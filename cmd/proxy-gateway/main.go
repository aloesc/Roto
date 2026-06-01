package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"proxy-gateway/internal/config"
	"proxy-gateway/internal/handler"
	"proxy-gateway/internal/netutils"
	"proxy-gateway/internal/pool"
	"proxy-gateway/internal/proxy"
)

var version = "dev"

func main() {
	// Загружаем конфигурацию
	cfg := config.Get()

	log.Printf("========================================")
	log.Printf("  Proxy Gateway v%s", version)
	log.Printf("========================================")
	log.Printf("Listen address: %s", cfg.ListenAddr)
	log.Printf("Rotate every: %d requests", cfg.RotateEvery)
	log.Printf("Failover: %v", cfg.Failover)
	log.Printf("Logging: %v", cfg.EnableLogs)
	log.Printf("Proxies configured: %d", len(cfg.Proxies))
	log.Printf("NO_PROXY rules: %d", len(cfg.NoProxy))

	// Создаём пул прокси
	proxyPool, err := pool.New(cfg.Proxies, cfg.RotateEvery, cfg.Failover, cfg.EnableLogs)
	if err != nil {
		if len(cfg.Proxies) > 0 {
			log.Fatalf("Failed to initialize proxy pool: %v", err)
		}
		log.Printf("[WARN] No proxies configured, running in direct mode only")
	}

	// Создаём проверщик bypass
	bypassChecker := netutils.NewBypassChecker(cfg.NoProxy, cfg.EnableLogs)

	// Создаём менеджер прокси
	proxyMgr := proxy.New(proxy.Config{
		Pool:          proxyPool,
		BypassChecker: bypassChecker,
		DialTimeout:   cfg.DialTimeout,
		EnableLogs:    cfg.EnableLogs,
		Failover:      cfg.Failover,
	})

	// Создаём обработчик HTTP/HTTPS
	httpHandler := handler.New(handler.Config{
		ProxyManager: proxyMgr,
		EnableLogs:   cfg.EnableLogs,
		DialTimeout:  cfg.DialTimeout,
	})

	// Создаём HTTP сервер
	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      httpHandler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	// Канал для сигналов завершения
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Запускаем сервер в горутине
	go func() {
		log.Printf("Starting proxy server on %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	log.Printf("Proxy gateway is ready")
	log.Printf("Configure your clients with HTTP_PROXY=http://<host>:%s", cfg.ListenAddr)

	// Ждём сигнал завершения
	<-quit

	log.Printf("Shutting down server...")

	// Создаём контекст с таймаутом для graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Printf("Server stopped")
}
