package pool

import (
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// Proxy представляет прокси-сервер в пуле
type Proxy struct {
	URL      *url.URL
	Scheme   string // http или socks5
	Host     string
	Healthy  atomic.Bool // Статус здоровья (для failover)
}

// Pool управляет пулом прокси и ротацией
type Pool struct {
	proxies     []*Proxy
	count       int64
	rotateEvery int64
	currentIdx  atomic.Int64
	failover    bool
	enableLogs  bool
	mu          sync.RWMutex
}

// New создаёт новый пул прокси
func New(proxies []string, rotateEvery int64, failover, enableLogs bool) (*Pool, error) {
	p := &Pool{
		proxies:     make([]*Proxy, 0, len(proxies)),
		rotateEvery: rotateEvery,
		failover:    failover,
		enableLogs:  enableLogs,
	}

	for _, proxyURL := range proxies {
		proxy, err := parseProxy(proxyURL)
		if err != nil {
			if enableLogs {
				logf("[POOL] Invalid proxy URL %s: %v", proxyURL, err)
			}
			continue
		}
		p.proxies = append(p.proxies, proxy)
		proxy.Healthy.Store(true)
	}

	if len(p.proxies) == 0 {
		return nil, ErrNoProxies
	}

	p.count = int64(len(p.proxies))

	if enableLogs {
		logf("[POOL] Initialized with %d proxies", len(p.proxies))
	}

	return p, nil
}

// parseProxy парсит URL прокси и определяет тип
func parseProxy(proxyURL string) (*Proxy, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		// Если схема не указана, предполагаем HTTP
		scheme = "http"
		u.Scheme = "http"
	}

	if scheme != "http" && scheme != "https" && scheme != "socks5" && scheme != "socks5h" {
		return nil, ErrInvalidScheme
	}

	return &Proxy{
		URL:    u,
		Scheme: scheme,
		Host:   u.Host,
	}, nil
}

// GetProxy возвращает текущий прокси на основе ротационного счётчика
func (p *Pool) GetProxy() *Proxy {
	if len(p.proxies) == 0 {
		return nil
	}

	idx := p.currentIdx.Load() % p.count
	return p.proxies[idx]
}

// Next увеличивает счётчик запросов и возвращает следующий прокси
// Возвращает новый индекс для атомарного подсчёта
func (p *Pool) Next() *Proxy {
	if len(p.proxies) == 0 {
		return nil
	}

	// Атомарно увеличиваем счётчик
	count := atomic.AddInt64(&p.count, 1)

	// Вычисляем индекс на основе общего счётчика запросов
	idx := (count / p.rotateEvery) % int64(len(p.proxies))
	p.currentIdx.Store(idx)

	proxy := p.proxies[idx]

	if p.enableLogs {
		logf("[POOL] Rotated to proxy %s (request #%d)", proxy.Host, count)
	}

	return proxy
}

// GetNextProxy атомарно получает следующий прокси для запроса
func (p *Pool) GetNextProxy() *Proxy {
	return p.Next()
}

// MarkUnhealthy помечает прокси как нездоровый (для failover)
func (p *Pool) MarkUnhealthy(proxy *Proxy) {
	if !p.failover {
		return
	}

	proxy.Healthy.Store(false)

	if p.enableLogs {
		logf("[POOL] Marked proxy %s as unhealthy", proxy.Host)
	}
}

// MarkHealthy помечает прокси как здоровый
func (p *Pool) MarkHealthy(proxy *Proxy) {
	proxy.Healthy.Store(true)

	if p.enableLogs {
		logf("[POOL] Marked proxy %s as healthy", proxy.Host)
	}
}

// GetHealthyProxies возвращает список здоровых прокси
func (p *Pool) GetHealthyProxies() []*Proxy {
	if !p.failover {
		return p.proxies
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	healthy := make([]*Proxy, 0, len(p.proxies))
	for _, proxy := range p.proxies {
		if proxy.Healthy.Load() {
			healthy = append(healthy, proxy)
		}
	}

	if len(healthy) == 0 {
		// Если все прокси нездоровы, возвращаем все
		return p.proxies
	}

	return healthy
}

// Len возвращает количество прокси в пуле
func (p *Pool) Len() int {
	return len(p.proxies)
}

// GetAll возвращает все прокси в пуле
func (p *Pool) GetAll() []*Proxy {
	return p.proxies
}

// Ошибки пула
var (
	ErrNoProxies     = &ProxyError{Msg: "no proxies available"}
	ErrInvalidScheme = &ProxyError{Msg: "invalid proxy scheme"}
)

// ProxyError представляет ошибку прокси
type ProxyError struct {
	Msg string
}

func (e *ProxyError) Error() string {
	return e.Msg
}

// logf выводит лог-сообщение
func logf(format string, args ...interface{}) {
	// Простая реализация логирования
}
