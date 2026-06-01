package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"proxy-gateway/internal/netutils"
	"proxy-gateway/internal/pool"
)

// Manager управляет проксированием запросов
type Manager struct {
	pool           *pool.Pool
	bypassChecker  *netutils.BypassChecker
	dialTimeout    time.Duration
	enableLogs     bool
	failover       bool
	requestCounter atomic.Int64
}

// Config конфигурация менеджера прокси
type Config struct {
	Pool          *pool.Pool
	BypassChecker *netutils.BypassChecker
	DialTimeout   time.Duration
	EnableLogs    bool
	Failover      bool
}

// New создаёт новый менеджер прокси
func New(cfg Config) *Manager {
	return &Manager{
		pool:          cfg.Pool,
		bypassChecker: cfg.BypassChecker,
		dialTimeout:   cfg.DialTimeout,
		enableLogs:    cfg.EnableLogs,
		failover:      cfg.Failover,
	}
}

// ProxyError представляет ошибку проксирования
type ProxyError struct {
	Err       error
	ProxyHost string
	Target    string
}

func (e *ProxyError) Error() string {
	return fmt.Sprintf("proxy error (proxy=%s, target=%s): %v", e.ProxyHost, e.Target, e.Err)
}

func (e *ProxyError) Unwrap() error {
	return e.Err
}

// DialContext создаёт соединение с целевым хостом
// Если хост в списке bypass, соединяется напрямую, иначе через прокси
func (m *Manager) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// Проверяем, нужно ли обходить прокси
	if m.bypassChecker.ShouldBypass(addr) {
		return m.dialDirect(ctx, network, addr)
	}

	return m.dialViaProxy(ctx, network, addr)
}

// dialDirect создаёт прямое соединение в обход прокси
func (m *Manager) dialDirect(ctx context.Context, network, addr string) (net.Conn, error) {
	if m.enableLogs {
		logf("[PROXY] Bypassing proxy for %s", addr)
	}

	dialer := &net.Dialer{
		Timeout:   m.dialTimeout,
		KeepAlive: 30 * time.Second,
	}

	return dialer.DialContext(ctx, network, addr)
}

// dialViaProxy создаёт соединение через upstream прокси
// Если пул пустой (нет upstream прокси), соединяется напрямую
func (m *Manager) dialViaProxy(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported network: %s", network)
	}

	// Получаем следующий прокси из пула
	proxy := m.pool.GetNextProxy()
	if proxy == nil {
		// Если пул пустой, работаем как прямой прокси (без upstream)
		if m.enableLogs {
			logf("[PROXY] No upstream proxies configured, connecting directly to %s", addr)
		}
		return m.dialDirect(ctx, network, addr)
	}

	// Пытаемся подключиться через прокси (с failover)
	return m.dialWithFailover(ctx, network, addr, proxy)
}

// dialWithFailover пытается подключиться через прокси с переключением на другие при ошибке
func (m *Manager) dialWithFailover(ctx context.Context, network, target string, initialProxy *pool.Proxy) (net.Conn, error) {
	var lastErr error

	// Получаем список прокси для попытки подключения
	proxies := m.getProxyList(initialProxy)

	for _, proxy := range proxies {
		conn, err := m.dialThroughProxy(ctx, network, target, proxy)
		if err == nil {
			// Успешное подключение
			m.pool.MarkHealthy(proxy)
			if m.enableLogs {
				logf("[PROXY] Connected to %s via %s", target, proxy.Host)
			}
			return conn, nil
		}

		lastErr = err
		m.pool.MarkUnhealthy(proxy)

		if m.enableLogs {
			logf("[PROXY] Failed to connect via %s: %v", proxy.Host, err)
		}

		// Если failover отключён, не пробуем другие прокси
		if !m.failover {
			return nil, &ProxyError{ProxyHost: proxy.Host, Target: target, Err: err}
		}
	}

	return nil, &ProxyError{ProxyHost: initialProxy.Host, Target: target, Err: lastErr}
}

// getProxyList возвращает список прокси для попытки подключения
func (m *Manager) getProxyList(initial *pool.Proxy) []*pool.Proxy {
	if !m.failover {
		return []*pool.Proxy{initial}
	}

	// Возвращаем все здоровые прокси
	healthy := m.pool.GetHealthyProxies()
	if len(healthy) == 0 {
		return []*pool.Proxy{initial}
	}

	return healthy
}

// dialThroughProxy создаёт соединение через один прокси
func (m *Manager) dialThroughProxy(ctx context.Context, network, target string, proxy *pool.Proxy) (net.Conn, error) {
	// Создаём соединение с прокси-сервером
	dialer := &net.Dialer{
		Timeout:   m.dialTimeout,
		KeepAlive: 30 * time.Second,
	}

	proxyConn, err := dialer.DialContext(ctx, network, proxy.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to proxy: %w", err)
	}

	// Для HTTP/HTTPS прокси используем CONNECT метод
	if proxy.Scheme == "http" || proxy.Scheme == "https" {
		return m.httpConnect(proxyConn, target)
	}

	// Для SOCKS5 нужна отдельная реализация
	if proxy.Scheme == "socks5" || proxy.Scheme == "socks5h" {
		return m.socks5Connect(proxyConn, target)
	}

	proxyConn.Close()
	return nil, fmt.Errorf("unsupported proxy scheme: %s", proxy.Scheme)
}

// httpConnect выполняет HTTP CONNECT запрос к прокси
func (m *Manager) httpConnect(conn net.Conn, target string) (net.Conn, error) {
	// Отправляем CONNECT запрос
	req := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Host: target},
		Host:   target,
		Header: make(http.Header),
	}

	// Добавляем Proxy-Authorization если есть учётные данные
	if proxyURL := m.getProxyURL(); proxyURL != nil {
		if username := proxyURL.User.Username(); username != "" {
			password, _ := proxyURL.User.Password()
			req.SetBasicAuth(username, password)
		}
	}

	err := req.Write(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send CONNECT: %w", err)
	}

	// Читаем ответ с помощью bufio.Reader
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read CONNECT response: %w", err)
	}
	defer resp.Body.Close()

	// Проверяем статус ответа (200 OK или 204 No Content)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != 204 {
		conn.Close()
		return nil, fmt.Errorf("proxy returned status: %s", resp.Status)
	}

	// Возвращаем обёрнутое соединение с буферизированным читателем
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

// getProxyURL возвращает URL текущего прокси (для аутентификации)
func (m *Manager) getProxyURL() *url.URL {
	if m.pool == nil {
		return nil
	}
	proxy := m.pool.GetProxy()
	if proxy == nil {
		return nil
	}
	return proxy.URL
}

// socks5Connect выполняет SOCKS5 handshake
func (m *Manager) socks5Connect(conn net.Conn, target string) (net.Conn, error) {
	// SOCKS5 handshake: версия, количество методов аутентификации, методы
	// Версия 5, один метод аутентификации (без аутентификации = 0)
	handshake := []byte{0x05, 0x01, 0x00}

	_, err := conn.Write(handshake)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send SOCKS5 handshake: %w", err)
	}

	// Читаем ответ (2 байта: версия, выбранный метод)
	resp := make([]byte, 2)
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read SOCKS5 handshake response: %w", err)
	}

	if resp[0] != 0x05 {
		conn.Close()
		return nil, fmt.Errorf("unsupported SOCKS version: %d", resp[0])
	}

	if resp[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 authentication required (method=%d)", resp[1])
	}

	// Формируем запрос на подключение
	// Версия (1) + команда CONNECT (1) + резерв (1) + тип адреса (1) + адрес + порт
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("invalid target address: %w", err)
	}

	var req []byte
	req = append(req, 0x05) // Версия SOCKS5
	req = append(req, 0x01) // Команда CONNECT
	req = append(req, 0x00) // Резервный байт

	// Определяем тип адреса
	ip := net.ParseIP(host)
	if ip != nil {
		// IPv4 или IPv6
		if ip.To4() != nil {
			req = append(req, 0x01) // IPv4
			req = append(req, ip.To4()...)
		} else {
			req = append(req, 0x04) // IPv6
			req = append(req, ip.To16()...)
		}
	} else {
		// Доменное имя
		req = append(req, 0x03) // Domain name
		req = append(req, byte(len(host)))
		req = append(req, []byte(host)...)
	}

	// Добавляем порт (2 байта, big-endian)
	var portNum int
	fmt.Sscanf(port, "%d", &portNum)
	req = append(req, byte(portNum>>8), byte(portNum&0xff))

	_, err = conn.Write(req)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send SOCKS5 connect request: %w", err)
	}

	// Читаем ответ (минимум 4 байта: версия, статус, резерв, тип адреса)
	header := make([]byte, 4)
	_, err = io.ReadFull(conn, header)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read SOCKS5 connect response: %w", err)
	}

	if header[0] != 0x05 {
		conn.Close()
		return nil, fmt.Errorf("invalid SOCKS5 response version: %d", header[0])
	}

	if header[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 connection failed (status=%d)", header[1])
	}

	// Пропускаем оставшуюся часть ответа (адрес + порт)
	var addrLen int
	switch header[3] {
	case 0x01: // IPv4
		addrLen = 4 + 2 // 4 байта IP + 2 байта порт
	case 0x04: // IPv6
		addrLen = 16 + 2 // 16 байт IP + 2 байта порт
	case 0x03: // Domain name
		// Первый байт - длина домена
		length := make([]byte, 1)
		_, err := io.ReadFull(conn, length)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to read domain length: %w", err)
		}
		addrLen = int(length[0]) + 2
	default:
		conn.Close()
		return nil, fmt.Errorf("unknown address type in SOCKS5 response: %d", header[3])
	}

	// Читаем и игнорируем оставшуюся часть ответа
	_, err = io.CopyN(io.Discard, conn, int64(addrLen))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read SOCKS5 response address: %w", err)
	}

	return conn, nil
}

// bufferedConn обёртка для соединения с буферизированным читателем
// Нужно для чтения данных после HTTP CONNECT ответа
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (bc *bufferedConn) Read(p []byte) (int, error) {
	return bc.reader.Read(p)
}

// logf выводит лог-сообщение
func logf(format string, args ...interface{}) {
	// Простая реализация логирования
}
