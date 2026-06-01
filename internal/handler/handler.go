package handler

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"proxy-gateway/internal/proxy"
)

// Handler обрабатывает HTTP и HTTPS CONNECT запросы
type Handler struct {
	proxyManager *proxy.Manager
	enableLogs   bool
	dialTimeout  time.Duration
	bufferPool   sync.Pool
}

// Config конфигурация обработчика
type Config struct {
	ProxyManager *proxy.Manager
	EnableLogs   bool
	DialTimeout  time.Duration
}

// New создаёт новый обработчик
func New(cfg Config) *Handler {
	h := &Handler{
		proxyManager: cfg.ProxyManager,
		enableLogs:   cfg.EnableLogs,
		dialTimeout:  cfg.DialTimeout,
	}

	// Инициализируем пул буферов для эффективного копирования данных
	h.bufferPool = sync.Pool{
		New: func() interface{} {
			// Буфер 32KB для копирования данных
			return make([]byte, 32*1024)
		},
	}

	return h
}

// ServeHTTP реализует http.Handler интерфейс
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		h.handleHTTPS(w, r)
		return
	}

	if r.Method == http.MethodGet || r.Method == http.MethodPost || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		h.handleHTTP(w, r)
		return
	}

	// Поддерживаем и другие методы для HTTP проксирования
	h.handleHTTP(w, r)
}

// handleHTTPS обрабатывает HTTPS CONNECT туннель
func (h *Handler) handleHTTPS(w http.ResponseWriter, r *http.Request) {
	target := r.Host

	if h.enableLogs {
		log.Printf("[HTTPS] CONNECT request for %s from %s", target, h.getClientIP(r))
	}

	// Создаём соединение с целевым хостом
	conn, err := h.proxyManager.DialContext(r.Context(), "tcp", target)
	if err != nil {
		if h.enableLogs {
			log.Printf("[ERROR] CONNECT failed for %s: %v", target, err)
		}
		http.Error(w, fmt.Sprintf("failed to establish connection: %v", err), http.StatusBadGateway)
		return
	}
	defer conn.Close()

	// Отправляем 200 OK клиенту
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		h.logError("Hijacker not supported", target, nil)
		http.Error(w, "Hijacker not supported", http.StatusInternalServerError)
		return
	}

	clientConn, bufrw, err := hijacker.Hijack()
	if err != nil {
		h.logError("Hijack failed", target, err)
		http.Error(w, fmt.Sprintf("hijack failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Отправляем успешный ответ на CONNECT
	_, err = bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err != nil {
		h.logError("Failed to send CONNECT response", target, err)
		return
	}
	bufrw.Flush()

	// Двустороннее копирование данных между клиентом и сервером
	h.copyBidirectional(clientConn, conn, target)
}

// handleHTTP обрабатывает обычные HTTP запросы через прокси
func (h *Handler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Для HTTP прокси нужно определить целевой хост
	target := r.Host
	if target == "" {
		// Если Host не указан, пробуем извлечь из URL
		if r.URL != nil && r.URL.Host != "" {
			target = r.URL.Host
		} else {
			http.Error(w, "no target host specified", http.StatusBadRequest)
			return
		}
	}

	// Проверяем, есть ли схема в URL (абсолютный URL)
	if r.URL != nil && r.URL.Scheme != "" && r.URL.Host != "" {
		// Это абсолютный URL, используем его напрямую
		target = r.URL.Host
	}

	if h.enableLogs {
		log.Printf("[HTTP] %s %s from %s", r.Method, target, h.getClientIP(r))
	}

	// Создаём транспорт для проксирования запроса
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Для HTTP прокси используем target из запроса
			return h.proxyManager.DialContext(ctx, network, target)
		},
		DisableKeepAlives:   true,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	defer transport.CloseIdleConnections()

	// Создаём новый запрос к целевому серверу
	targetURL := r.URL
	if targetURL.Scheme == "" {
		targetURL.Scheme = "http"
	}
	if targetURL.Host == "" {
		targetURL.Host = target
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
	if err != nil {
		h.logError("Failed to create proxy request", target, err)
		http.Error(w, fmt.Sprintf("failed to create request: %v", err), http.StatusInternalServerError)
		return
	}

	// Копируем заголовки
	proxyReq.Header = make(http.Header)
	for k, vv := range r.Header {
		// Пропускаем hop-by-hop заголовки
		if !h.isHopByHop(k) {
			for _, v := range vv {
				proxyReq.Header.Add(k, v)
			}
		}
	}

	// Добавляем X-Forwarded-For
	if clientIP := h.getClientIP(r); clientIP != "" {
		proxyReq.Header.Set("X-Forwarded-For", clientIP)
	}

	// Отправляем запрос через транспорт
	resp, err := transport.RoundTrip(proxyReq)
	if err != nil {
		h.logError("Proxy request failed", target, err)
		http.Error(w, fmt.Sprintf("proxy request failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Копируем ответ клиенту
	for k, vv := range resp.Header {
		if !h.isHopByHop(k) {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}

	w.WriteHeader(resp.StatusCode)

	// Копируем тело ответа
	buf := h.bufferPool.Get().([]byte)
	defer h.bufferPool.Put(buf)

	_, err = io.CopyBuffer(w, resp.Body, buf)
	if err != nil {
		h.logError("Failed to copy response body", target, err)
	}
}

// copyBidirectional выполняет двустороннее копирование между соединениями
func (h *Handler) copyBidirectional(client, server net.Conn, target string) {
	var wg sync.WaitGroup
	wg.Add(2)

	errChan := make(chan error, 2)

	go func() {
		defer wg.Done()
		buf := h.bufferPool.Get().([]byte)
		defer h.bufferPool.Put(buf)

		_, err := io.CopyBuffer(client, server, buf)
		if err != nil && err != io.EOF {
			errChan <- fmt.Errorf("server->client: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		buf := h.bufferPool.Get().([]byte)
		defer h.bufferPool.Put(buf)

		_, err := io.CopyBuffer(server, client, buf)
		if err != nil && err != io.EOF {
			errChan <- fmt.Errorf("client->server: %w", err)
		}
	}()

	// Ждём завершения копирования
	wg.Wait()
	close(errChan)

	// Логируем ошибки если есть
	for err := range errChan {
		if h.enableLogs {
			log.Printf("[HTTPS] Error copying data for %s: %v", target, err)
		}
	}
}

// isHopByHop проверяет, является ли заголовок hop-by-hop
func (h *Handler) isHopByHop(header string) bool {
	hopByHop := map[string]bool{
		"Connection":          true,
		"Proxy-Connection":    true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailers":            true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}

	return hopByHop[header]
}

// getClientIP извлекает IP адрес клиента из запроса
func (h *Handler) getClientIP(r *http.Request) string {
	// Проверяем X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Берём первый IP из списка
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Проверяем X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Используем RemoteAddr
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// logError выводит сообщение об ошибке
func (h *Handler) logError(msg, target string, err error) {
	if !h.enableLogs {
		return
	}

	if err != nil {
		log.Printf("[ERROR] %s for %s: %v", msg, target, err)
	} else {
		log.Printf("[ERROR] %s for %s", msg, target)
	}
}
