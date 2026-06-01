# Proxy Gateway

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat&logo=docker)](https://www.docker.com)

**Production-ready HTTP/HTTPS Proxy Gateway in Go**

A high-performance proxy gateway that accepts traffic from Docker containers and applications, rotates upstream proxies, and provides automatic failover.

---

## 🇬🇧 English Version

### 📋 Features

- ✅ **HTTP/HTTPS Proxy** - Full support for HTTP proxy protocol and HTTPS CONNECT tunneling
- ✅ **Upstream Proxy Pool** - Support for HTTP, HTTPS, and SOCKS5 upstream proxies
- ✅ **Automatic Rotation** - Rotate upstream proxies every N requests (configurable)
- ✅ **Failover** - Automatic retry with next proxy on connection failure
- ✅ **Bypass Rules** - Direct connections for localhost, private networks, and custom NO_PROXY rules
- ✅ **Docker Ready** - Pre-configured Dockerfile and docker-compose.yml
- ✅ **High Performance** - Atomic counters, connection pooling, efficient memory usage
- ✅ **No External Dependencies** - Built with Go standard library only

### 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Client Container                            │
│              HTTP_PROXY=http://gateway:8080                     │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Proxy Gateway (Go)                           │
│                                                                 │
│  ┌─────────────┐    ┌──────────────┐    ┌─────────────────┐   │
│  │   handler/  │───▶│   proxy/     │───▶│   pool/         │   │
│  │  HTTP/HTTPS │    │  Rotation    │    │   Management    │   │
│  │  CONNECT    │    │  Failover    │    │                 │   │
│  └─────────────┘    └──────────────┘    └─────────────────┘   │
│         │                                       ▲              │
│         ▼                                       │              │
│  ┌─────────────┐                        ┌─────────────────┐   │
│  │  netutils/  │───────────────────────▶│   config/       │   │
│  │  Bypass     │                        │   Env Loader    │   │
│  │  LAN Check  │                        │                 │   │
│  └─────────────┘                        └─────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         │
         ├──────────────┬─────────────────────────────────────┐
         ▼              ▼                                     ▼
   ┌──────────┐   ┌──────────┐                         ┌──────────┐
   │ Direct   │   │ Upstream │                         │ Upstream │
   │ (bypass) │   │ Proxy 1  │                         │ Proxy 2  │
   └──────────┘   └──────────┘                         └──────────┘
```

### 📦 Installation

#### Build from Source

```bash
# Clone repository
git clone https://github.com/yourusername/proxy-gateway.git
cd proxy-gateway

# Build
go build -o proxy-gateway ./cmd/proxy-gateway

# Run
./proxy-gateway
```

#### Run with Docker

```bash
# Build image
docker build -t proxy-gateway .

# Run container
docker run -d \
  -p 8080:8080 \
  -e PROXIES="http://proxy1.com:8080,http://proxy2.com:8080" \
  -e ROTATE_EVERY=100 \
  --name proxy-gateway \
  proxy-gateway
```

#### Run with docker-compose

```bash
# Copy and edit configuration
cp .env.example .env
nano .env

# Start service
docker-compose up -d

# Check status
docker-compose ps
```

### ⚙️ Configuration

#### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LISTEN_ADDR` | Listen address (host:port) | `0.0.0.0:8080` |
| `ROTATE_EVERY` | Rotate proxy every N requests | `100` |
| `PROXIES` | Comma-separated upstream proxy list | (empty) |
| `NO_PROXY` | Bypass rules (hosts, CIDRs) | localhost, private nets |
| `ENABLE_LOGS` | Enable request logging | `true` |
| `FAILOVER` | Enable automatic failover | `true` |
| `DIAL_TIMEOUT` | Connection timeout | `10s` |
| `READ_TIMEOUT` | Read timeout | `30s` |
| `WRITE_TIMEOUT` | Write timeout | `30s` |

#### .env File Example

```bash
# Server
LISTEN_ADDR=0.0.0.0:8080

# Rotation
ROTATE_EVERY=100

# Upstream proxies
PROXIES=http://1.2.3.4:8080,http://5.6.7.8:8080,socks5://9.10.11.12:1080

# Bypass rules
NO_PROXY=localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16

# Features
ENABLE_LOGS=true
FAILOVER=true
```

### 🚀 Usage Examples

#### Configure Container to Use Proxy

```bash
docker run --rm \
  -e HTTP_PROXY=http://proxy-gateway:8080 \
  -e HTTPS_PROXY=http://proxy-gateway:8080 \
  -e NO_PROXY=localhost,127.0.0.1 \
  alpine/curl curl https://api.ipify.org
```

#### Configure Docker Daemon

Edit `/etc/docker/daemon.json`:

```json
{
  "proxies": {
    "http-proxy": "http://proxy-gateway:8080",
    "https-proxy": "http://proxy-gateway:8080",
    "no-proxy": "localhost,127.0.0.1,10.0.0.0/8"
  }
}
```

#### curl Examples

```bash
# Direct request
curl https://example.com

# Via proxy gateway
curl -x http://localhost:8080 https://example.com

# With authentication (if upstream proxy requires)
curl -x http://user:pass@localhost:8080 https://example.com
```

### 🔄 How Rotation Works

1. **Global Counter**: All requests increment a shared atomic counter
2. **Rotation**: Every N requests, the gateway switches to the next upstream proxy
3. **Round-Robin**: Proxies are used in circular order (proxy1 → proxy2 → proxy3 → proxy1...)

**Example with 3 proxies and ROTATE_EVERY=100:**
- Requests 1-100 → Proxy 1
- Requests 101-200 → Proxy 2
- Requests 201-300 → Proxy 3
- Requests 301-400 → Proxy 1 (cycle repeats)

### 🛡️ Failover

When `FAILOVER=true`:

1. If connection through current proxy fails, gateway automatically tries next proxy
2. Failed proxy is marked as unhealthy
3. Gateway continues with healthy proxies
4. Unhealthy proxy is retried after rotation cycle

### 🐛 Troubleshooting

#### Container Cannot Connect

**Check NO_PROXY rules:**
```bash
# Ensure localhost and Docker networks are bypassed
NO_PROXY=localhost,127.0.0.1,host.docker.internal,172.17.0.0/16
```

#### DNS Resolution Issues

**Use SOCKS5H for DNS-over-proxy:**
```bash
PROXIES=socks5h://proxy.example.com:1080
```

#### Connection Timeout

**Increase timeouts in .env:**
```bash
DIAL_TIMEOUT=30s
READ_TIMEOUT=60s
```

#### Proxy Loop / No Internet

**Verify bypass rules are correct:**
- Ensure `host.docker.internal` is in NO_PROXY
- Check Docker network CIDR matches your setup
- Test direct connection: `curl --noproxy '*' http://example.com`

---

## 🇷🇺 Русская Версия

### 📋 Возможности

- ✅ **HTTP/HTTPS Прокси** - Полная поддержка протокола HTTP proxy и HTTPS CONNECT туннелирования
- ✅ **Пул прокси** - Поддержка HTTP, HTTPS и SOCKS5 upstream прокси
- ✅ **Автоматическая ротация** - Переключение прокси каждые N запросов (настраивается)
- ✅ **Failover** - Автоматическая повторная попытка со следующим прокси при ошибке
- ✅ **Правила обхода** - Прямые соединения для localhost, частных сетей и пользовательских NO_PROXY
- ✅ **Docker Ready** - Готовые Dockerfile и docker-compose.yml
- ✅ **Высокая производительность** - Атомарные счётчики, пул соединений, эффективное использование памяти
- ✅ **Без внешних зависимостей** - Только стандартная библиотека Go

### 🏗️ Архитектура

```
┌─────────────────────────────────────────────────────────────────┐
│                    Клиентский контейнер                         │
│              HTTP_PROXY=http://gateway:8080                     │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                   Proxy Gateway (Go)                            │
│                                                                 │
│  ┌─────────────┐    ┌──────────────┐    ┌─────────────────┐   │
│  │   handler/  │───▶│   proxy/     │───▶│   pool/         │   │
│  │  HTTP/HTTPS │    │  Ротация     │    │   Управление    │   │
│  │  CONNECT    │    │  Failover    │    │   прокси        │   │
│  └─────────────┘    └──────────────┘    └─────────────────┘   │
│         │                                       ▲              │
│         ▼                                       │              │
│  ┌─────────────┐                        ┌─────────────────┐   │
│  │  netutils/  │───────────────────────▶│   config/       │   │
│  │  Обход      │                        │   Загрузка env  │   │
│  │  LAN Check  │                        │                 │   │
│  └─────────────┘                        └─────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         │
         ├──────────────┬─────────────────────────────────────┐
         ▼              ▼                                     ▼
   ┌──────────┐   ┌──────────┐                         ┌──────────┐
   │ Прямое   │   │ Upstream │                         │ Upstream │
   │ (обход)  │   │ Proxy 1  │                         │ Proxy 2  │
   └──────────┘   └──────────┘                         └──────────┘
```

### 📦 Установка

#### Сборка из исходников

```bash
# Клонировать репозиторий
git clone https://github.com/yourusername/proxy-gateway.git
cd proxy-gateway

# Собрать
go build -o proxy-gateway ./cmd/proxy-gateway

# Запустить
./proxy-gateway
```

#### Запуск в Docker

```bash
# Построить образ
docker build -t proxy-gateway .

# Запустить контейнер
docker run -d \
  -p 8080:8080 \
  -e PROXIES="http://proxy1.com:8080,http://proxy2.com:8080" \
  -e ROTATE_EVERY=100 \
  --name proxy-gateway \
  proxy-gateway
```

#### Запуск через docker-compose

```bash
# Скопировать и настроить конфигурацию
cp .env.example .env
nano .env

# Запустить сервис
docker-compose up -d

# Проверить статус
docker-compose ps
```

### ⚙️ Конфигурация

#### Переменные окружения

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `LISTEN_ADDR` | Адрес прослушивания (хост:порт) | `0.0.0.0:8080` |
| `ROTATE_EVERY` | Ротация прокси каждые N запросов | `100` |
| `PROXIES` | Список upstream прокси через запятую | (пусто) |
| `NO_PROXY` | Правила обхода (хосты, CIDR) | localhost, частные сети |
| `ENABLE_LOGS` | Включить логирование запросов | `true` |
| `FAILOVER` | Включить автоматический failover | `true` |
| `DIAL_TIMEOUT` | Таймаут соединения | `10s` |
| `READ_TIMEOUT` | Таймаут чтения | `30s` |
| `WRITE_TIMEOUT` | Таймаут записи | `30s` |

#### Пример .env файла

```bash
# Сервер
LISTEN_ADDR=0.0.0.0:8080

# Ротация
ROTATE_EVERY=100

# Upstream прокси
PROXIES=http://1.2.3.4:8080,http://5.6.7.8:8080,socks5://9.10.11.12:1080

# Правила обхода
NO_PROXY=localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16

# Функции
ENABLE_LOGS=true
FAILOVER=true
```

### 🚀 Примеры использования

#### Настройка контейнера для работы с прокси

```bash
docker run --rm \
  -e HTTP_PROXY=http://proxy-gateway:8080 \
  -e HTTPS_PROXY=http://proxy-gateway:8080 \
  -e NO_PROXY=localhost,127.0.0.1 \
  alpine/curl curl https://api.ipify.org
```

#### Настройка Docker Daemon

Отредактируйте `/etc/docker/daemon.json`:

```json
{
  "proxies": {
    "http-proxy": "http://proxy-gateway:8080",
    "https-proxy": "http://proxy-gateway:8080",
    "no-proxy": "localhost,127.0.0.1,10.0.0.0/8"
  }
}
```

#### Примеры curl

```bash
# Прямой запрос
curl https://example.com

# Через proxy gateway
curl -x http://localhost:8080 https://example.com

# С аутентификацией (если upstream прокси требует)
curl -x http://user:pass@localhost:8080 https://example.com
```

### 🔄 Как работает ротация

1. **Глобальный счётчик**: Все запросы увеличивают общий атомарный счётчик
2. **Ротация**: Каждые N запросов шлюз переключается на следующий upstream прокси
3. **Циклически**: Прокси используются по кругу (proxy1 → proxy2 → proxy3 → proxy1...)

**Пример с 3 прокси и ROTATE_EVERY=100:**
- Запросы 1-100 → Proxy 1
- Запросы 101-200 → Proxy 2
- Запросы 201-300 → Proxy 3
- Запросы 301-400 → Proxy 1 (цикл повторяется)

### 🛡️ Failover (Аварийное переключение)

Когда `FAILOVER=true`:

1. Если соединение через текущий прокси не удалось, шлюз автоматически пробует следующий
2. Нерабочий прокси помечается как unhealthy
3. Шлюз продолжает работу со здоровыми прокси
4. Нездоровый прокси повторяется после цикла ротации

### 🐛 Решение проблем

#### Контейнер не может подключиться

**Проверьте правила NO_PROXY:**
```bash
# Убедитесь, что localhost и Docker сети обходятся
NO_PROXY=localhost,127.0.0.1,host.docker.internal,172.17.0.0/16
```

#### Проблемы с DNS

**Используйте SOCKS5H для DNS через прокси:**
```bash
PROXIES=socks5h://proxy.example.com:1080
```

#### Таймаут соединения

**Увеличьте таймауты в .env:**
```bash
DIAL_TIMEOUT=30s
READ_TIMEOUT=60s
```

#### Цикл прокси / Нет интернета

**Проверьте правильность правил обхода:**
- Убедитесь, что `host.docker.internal` в NO_PROXY
- Проверьте, что CIDR Docker сети соответствует вашей настройке
- Протестируйте прямое соединение: `curl --noproxy '*' http://example.com`

---

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

---

**Built with ❤️ using Go**
