package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config хранит конфигурацию прокси-шлюза
type Config struct {
	ListenAddr   string        // Адрес прослушивания
	RotateEvery  int64         // Ротация каждые N запросов
	Proxies      []string      // Список прокси
	NoProxy      []string      // Список исключений
	EnableLogs   bool          // Включить логирование
	Failover     bool          // Включить failover
	ReadTimeout  time.Duration // Таймаут чтения
	WriteTimeout time.Duration // Таймаут записи
	DialTimeout  time.Duration // Таймаут соединения
}

// Load загружает конфигурацию из переменных окружения
func Load() *Config {
	cfg := &Config{
		ListenAddr:   getEnv("LISTEN_ADDR", "0.0.0.0:8080"),
		RotateEvery:  getEnvInt64("ROTATE_EVERY", 100),
		Proxies:      parseProxyList(getEnv("PROXIES", "")),
		NoProxy:      parseNoProxy(getEnv("NO_PROXY", "")),
		EnableLogs:   getEnvBool("ENABLE_LOGS", true),
		Failover:     getEnvBool("FAILOVER", true),
		ReadTimeout:  getEnvDuration("READ_TIMEOUT", 30*time.Second),
		WriteTimeout: getEnvDuration("WRITE_TIMEOUT", 30*time.Second),
		DialTimeout:  getEnvDuration("DIAL_TIMEOUT", 10*time.Second),
	}

	// Если прокси не указаны, пытаемся загрузить из файла proxies.txt
	if len(cfg.Proxies) == 0 {
		cfg.Proxies = loadProxiesFromFile("proxies.txt")
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt64(key string, defaultVal int64) int64 {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

func parseProxyList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func parseNoProxy(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func loadProxiesFromFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var proxies []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		// Пропускаем комментарии и пустые строки
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proxies = append(proxies, line)
	}
	return proxies
}

// ProxyPoolConfig хранит конфигурацию пула прокси
type ProxyPoolConfig struct {
	Proxies      []string
	RotateEvery  int64
	Failover     bool
	EnableLogs   bool
}

// NewProxyPoolConfig создаёт конфигурацию пула из основной конфигурации
func (c *Config) NewProxyPoolConfig() *ProxyPoolConfig {
	return &ProxyPoolConfig{
		Proxies:     c.Proxies,
		RotateEvery: c.RotateEvery,
		Failover:    c.Failover,
		EnableLogs:  c.EnableLogs,
	}
}

// BypassCheckerConfig хранит конфигурацию для проверки bypass
type BypassCheckerConfig struct {
	NoProxy    []string
	EnableLogs bool
}

// NewBypassCheckerConfig создаёт конфигурацию для проверки bypass
func (c *Config) NewBypassCheckerConfig() *BypassCheckerConfig {
	return &BypassCheckerConfig{
		NoProxy:    c.NoProxy,
		EnableLogs: c.EnableLogs,
	}
}

// once используется для инициализации конфигурации в singleton-паттерне
var (
	once   sync.Once
	config *Config
)

// Get возвращает глобальный экземпляр конфигурации (singleton)
func Get() *Config {
	once.Do(func() {
		config = Load()
	})
	return config
}
