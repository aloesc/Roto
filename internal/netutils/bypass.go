package netutils

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

// BypassChecker проверяет, должен ли запрос идти в обход прокси
type BypassChecker struct {
	noProxyRules []noProxyRule
	enableLogs   bool
	mu           sync.RWMutex
}

type noProxyRule struct {
	host    string
	isCIDR  bool
	network *net.IPNet
}

// NewBypassChecker создаёт новый экземпляр BypassChecker
func NewBypassChecker(noProxyList []string, enableLogs bool) *BypassChecker {
	bc := &BypassChecker{
		noProxyRules: make([]noProxyRule, 0, len(noProxyList)+7),
		enableLogs:   enableLogs,
	}

	// Добавляем стандартные правила для localhost и частных сетей
	bc.addDefaultRules()

	// Добавляем пользовательские правила из NO_PROXY
	for _, rule := range noProxyList {
		bc.addRule(rule)
	}

	return bc
}

// addDefaultRules добавляет стандартные правила для localhost и частных сетей
func (bc *BypassChecker) addDefaultRules() {
	// Localhost
	bc.addRule("127.0.0.1")
	bc.addRule("localhost")
	bc.addRule("::1")

	// Docker internal
	bc.addRule("host.docker.internal")

	// Docker bridge network 172.17.0.0/16
	bc.addCIDR("172.17.0.0/16")

	// Docker networks 172.16.0.0/12
	bc.addCIDR("172.16.0.0/12")

	// Private networks 10.0.0.0/8
	bc.addCIDR("10.0.0.0/8")

	// Private networks 192.168.0.0/16
	bc.addCIDR("192.168.0.0/16")
}

// addRule добавляет одно правило NO_PROXY
func (bc *BypassChecker) addRule(rule string) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return
	}

	// Проверяем, это CIDR или хост
	if strings.Contains(rule, "/") {
		bc.addCIDR(rule)
		return
	}

	// Удаляем ведущую точку для доменов вида .example.com
	if strings.HasPrefix(rule, ".") {
		rule = rule[1:]
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.noProxyRules = append(bc.noProxyRules, noProxyRule{
		host:   strings.ToLower(rule),
		isCIDR: false,
	})
}

// addCIDR добавляет правило CIDR
func (bc *BypassChecker) addCIDR(cidr string) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		if bc.enableLogs {
			logf("[BYPASS] Invalid CIDR: %s: %v", cidr, err)
		}
		return
	}

	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.noProxyRules = append(bc.noProxyRules, noProxyRule{
		host:    cidr,
		isCIDR:  true,
		network: network,
	})
}

// ShouldBypass проверяет, должен ли запрос идти в обход прокси
func (bc *BypassChecker) ShouldBypass(host string) bool {
	if host == "" {
		return false
	}

	// Нормализуем хост (удаляем порт)
	host, _, err := net.SplitHostPort(host)
	if err != nil {
		// Если нет порта, используем как есть
		host = strings.Trim(host, "[]")
	}

	host = strings.ToLower(host)

	bc.mu.RLock()
	defer bc.mu.RUnlock()

	for _, rule := range bc.noProxyRules {
		if rule.isCIDR {
			// Проверяем CIDR правило
			ip := net.ParseIP(host)
			if ip != nil && rule.network.Contains(ip) {
				if bc.enableLogs {
					logf("[BYPASS] %s matches CIDR %s", host, rule.host)
				}
				return true
			}
		} else {
			// Проверяем точное совпадение или суффикс домена
			if rule.host == host {
				if bc.enableLogs {
					logf("[BYPASS] %s matches exact rule %s", host, rule.host)
				}
				return true
			}
			// Проверяем суффикс для доменов (example.com匹配 sub.example.com)
			if strings.HasSuffix(host, "."+rule.host) {
				if bc.enableLogs {
					logf("[BYPASS] %s matches domain suffix %s", host, rule.host)
				}
				return true
			}
		}
	}

	return false
}

// ShouldBypassRequest проверяет, должен ли HTTP-запрос идти в обход прокси
func (bc *BypassChecker) ShouldBypassRequest(req *http.Request) bool {
	if req == nil {
		return false
	}
	return bc.ShouldBypass(req.Host)
}

// logf выводит лог-сообщение (внутренняя функция)
func logf(format string, args ...interface{}) {
	// Простая реализация логирования
	// В production можно заменить на logger из стандартной библиотеки
}
