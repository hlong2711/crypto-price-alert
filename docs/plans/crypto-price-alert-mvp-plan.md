# Crypto Price Alert — MVP Plan

## 1. Mục tiêu

Xây dựng một backend service bằng Go để định kỳ lấy dữ liệu giá crypto từ exchange và gửi thông báo lên Telegram hoặc Slack.

MVP hỗ trợ:

- Chọn một hoặc nhiều crypto.
- Theo dõi interval 1H và 4H.
- Timezone mặc định: `Asia/Ho_Chi_Minh`.
- Chỉ gửi notification trong khung giờ `06:00 → 23:00`.
- Bỏ qua toàn bộ thời gian `23:00 → 06:00`.
- Với interval 4H, chỉ chạy các window:
  - `06:00 → 10:00`
  - `10:00 → 14:00`
  - `14:00 → 18:00`
  - `18:00 → 22:00`
- Bỏ qua window `22:00 → 23:00` vì chỉ có 1 giờ, không đủ 4H.
- Notification gộp nhiều coin vào một message.
- Có thể bật Telegram và/hoặc Slack.
- Ưu tiên đơn giản, dễ deploy, không over-engineer.

---

## 2. Kiến trúc tổng thể

```text
                    Exchange API
                Binance / Market Data
                         │
                         ▼
                ┌─────────────────┐
                │ Market Service  │
                │                 │
                │ GetKline()      │
                │ GetPrice()      │
                └────────┬────────┘
                         │
                         ▼
                ┌─────────────────┐
                │ Calculator      │
                │                 │
                │ Price change %  │
                └────────┬────────┘
                         │
              ┌──────────┴──────────┐
              │                     │
              ▼                     ▼
        1H Scheduler          4H Scheduler
              │                     │
              └──────────┬──────────┘
                         ▼
                ┌─────────────────┐
                │ Notification    │
                │ Service         │
                └────────┬────────┘
                         │
                  ┌──────┴──────┐
                  ▼             ▼
              Telegram        Slack
```

Kiến trúc: **modular monolith**, một Go process duy nhất.

Không cần microservices, Kafka, RabbitMQ, Redis hoặc Kubernetes cho MVP.

---

## 3. Tech stack

| Thành phần | Công nghệ |
|---|---|
| Language | Go 1.24+ |
| Market data | Binance API |
| Scheduler | Go scheduler hoặc `robfig/cron` |
| Config | YAML + Environment Variables |
| Database | SQLite |
| HTTP | Go `net/http` |
| Telegram | Telegram Bot API |
| Slack | Slack Webhook |
| Logging | `log/slog` |
| Testing | Go standard `testing` |
| Deployment | Docker + VPS |

---

## 4. Market data

### Provider

MVP sử dụng Binance public market data.

Ví dụ symbols:

```text
BTCUSDT
ETHUSDT
SOLUSDT
```

Thiết kế abstraction:

```go
type MarketDataProvider interface {
    GetKline(
        ctx context.Context,
        symbol string,
        interval string,
        start time.Time,
        end time.Time,
    ) (Candle, error)
}
```

Implementation đầu tiên:

```text
BinanceProvider
```

Sau này có thể thêm:

```text
BybitProvider
OKXProvider
CoinbaseProvider
```

mà không cần thay đổi business logic.

---

## 5. Tính price change

Công thức:

```text
change_percent = (close - open) / open * 100
```

Ví dụ:

```text
Open  = 100,000
Close = 102,000

Change = +2%
```

Domain model:

```go
type Candle struct {
    Symbol    string
    Open      float64
    High      float64
    Low       float64
    Close     float64
    OpenTime  time.Time
    CloseTime time.Time
}

type PriceChange struct {
    Symbol    string
    Interval  string
    Open      float64
    Close     float64
    ChangePct float64
    StartTime time.Time
    EndTime   time.Time
}
```

Ưu tiên sử dụng OHLC/Kline từ exchange thay vì tự polling spot price rồi tính.

---

## 6. Timezone và trading window

Timezone mặc định:

```yaml
timezone: Asia/Ho_Chi_Minh
```

Không hard-code `UTC+7`.

Load timezone bằng IANA timezone:

```go
loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
```

### Active hours

```text
06:00 ───────────────────────── 23:00
  ACTIVE                         ACTIVE END

23:00 ───────────────────────── 06:00
            SLEEP
```

### 1H

Các period hợp lệ:

```text
06:00 → 07:00
07:00 → 08:00
08:00 → 09:00
...
21:00 → 22:00
22:00 → 23:00
```

Tổng cộng 17 period/ngày.

### 4H

Các period hợp lệ:

```text
06:00 → 10:00
10:00 → 14:00
14:00 → 18:00
18:00 → 22:00
```

Bỏ qua:

```text
22:00 → 23:00
23:00 → 06:00
```

Do đó 4H có 4 notification periods/ngày.

---

## 7. Notification format

Một notification nên chứa nhiều coin thay vì gửi từng coin riêng lẻ.

Ví dụ:

```text
📊 Crypto 1H Update
Period: 09:00 → 10:00 GMT+7

BTCUSDT   $108,420   +1.82% 🟢
ETHUSDT     $4,120   +0.94% 🟢
SOLUSDT    $198.32   -2.13% 🔴
```

4H:

```text
📊 Crypto 4H Update
Period: 06:00 → 10:00 GMT+7

BTCUSDT   $108,420   +3.21% 🟢
ETHUSDT     $4,120   +1.82% 🟢
SOLUSDT    $198.32   -2.44% 🔴
```

---

## 8. Notification abstraction

Không để business logic phụ thuộc trực tiếp vào Telegram/Slack.

```go
type Notifier interface {
    Send(ctx context.Context, message Message) error
}
```

Implementations:

```text
TelegramNotifier
SlackNotifier
```

Service:

```text
NotificationService
    │
    ├── TelegramNotifier
    └── SlackNotifier
```

Sau này có thể thêm:

```text
DiscordNotifier
EmailNotifier
WebhookNotifier
```

---

## 9. Configuration

Ví dụ `config.yaml`:

```yaml
app:
  timezone: Asia/Ho_Chi_Minh

market:
  provider: binance

  symbols:
    - BTCUSDT
    - ETHUSDT
    - SOLUSDT

  intervals:
    - 1h
    - 4h

schedule:
  active_from: "06:00"
  active_until: "23:00"

notifications:
  telegram:
    enabled: true
    chat_id: "${TELEGRAM_CHAT_ID}"

  slack:
    enabled: false
    webhook_url: "${SLACK_WEBHOOK_URL}"
```

Secrets nên nằm trong environment variables:

```text
TELEGRAM_BOT_TOKEN
TELEGRAM_CHAT_ID
SLACK_WEBHOOK_URL
```

Không commit secret vào Git.

---

## 10. Database

MVP sử dụng SQLite.

Mục đích chính là lưu trạng thái notification và đảm bảo idempotency.

Ví dụ bảng:

```text
notification_jobs

id
symbol
interval
period_start
period_end
status
sent_at
created_at
```

Unique constraint:

```text
(symbol, interval, period_start)
```

Điều này tránh trường hợp service restart và gửi duplicate notification.

Ví dụ:

```text
BTCUSDT | 1h | 2026-09-01 09:00
```

chỉ được gửi một lần.

Repository abstraction:

```go
type JobRepository interface {
    Exists(ctx context.Context, key JobKey) (bool, error)
    Create(ctx context.Context, job Job) error
    MarkSent(ctx context.Context, id string) error
}
```

MVP implementation:

```text
SQLiteRepository
```

Sau này có thể thay bằng:

```text
PostgresRepository
```

---

## 11. Scheduler flow

### 1H

Scheduler trigger tại đầu mỗi hour trong active window:

```text
06:00
07:00
08:00
...
22:00
23:00
```

Tại `10:00`, service xử lý period:

```text
09:00 → 10:00
```

### 4H

Trigger:

```text
10:00
14:00
18:00
22:00
```

Các period:

```text
06:00 → 10:00
10:00 → 14:00
14:00 → 18:00
18:00 → 22:00
```

Không trigger 4H lúc 23:00.

---

## 12. Job execution

Một job:

```text
Scheduler
    │
    ▼
Check active window
    │
    ▼
Create/check job idempotency
    │
    ▼
Fetch candle/price data
    │
    ▼
Calculate percentage change
    │
    ▼
Build aggregated message
    │
    ▼
Send Telegram / Slack
    │
    ▼
Mark job as sent
```

Nếu một symbol lỗi:

```text
BTCUSDT   +1.82% 🟢
SOLUSDT   -2.13% 🔴
ETHUSDT   ⚠️ unavailable
```

Không fail toàn bộ batch chỉ vì một symbol.

---

## 13. Retry và error handling

Market API có thể timeout hoặc rate limit.

Recommendation:

```text
attempt 1
   ↓
500ms
   ↓
attempt 2
   ↓
1s
   ↓
attempt 3
```

Dùng exponential backoff.

Có thể retry các lỗi transient:

```text
timeout
connection reset
HTTP 5xx
rate limit nếu exchange yêu cầu retry-after
```

Không retry vô hạn.

---

## 14. Concurrency

MVP chỉ cần giới hạn concurrency.

Ví dụ:

```go
sem := make(chan struct{}, 5)
```

Tối đa 5 request đồng thời.

Với vài symbol thì workload rất nhỏ, không cần queue system.

---

## 15. Logging

Sử dụng `log/slog`.

Ví dụ:

```text
INFO job started interval=1h
INFO candle fetched symbol=BTCUSDT
INFO price calculated symbol=BTCUSDT change=1.82
INFO notification sent provider=telegram
ERROR candle fetch failed symbol=ETHUSDT error=timeout
```

Không log:

```text
TELEGRAM_BOT_TOKEN
SLACK_WEBHOOK_URL
```

---

## 16. Health check

Thêm HTTP endpoint:

```text
GET /health
```

Response:

```json
{
  "status": "ok"
}
```

Mục đích:

- Docker health check.
- VPS monitoring.
- Kiểm tra process còn sống.

MVP chưa cần Prometheus.

---

## 17. Project structure

```text
crypto-alert/
│
├── cmd/
│   └── server/
│       └── main.go
│
├── internal/
│   ├── config/
│   │   └── config.go
│   │
│   ├── domain/
│   │   ├── candle.go
│   │   ├── price_change.go
│   │   └── notification.go
│   │
│   ├── market/
│   │   ├── provider.go
│   │   └── binance.go
│   │
│   ├── scheduler/
│   │   └── scheduler.go
│   │
│   ├── calculator/
│   │   └── calculator.go
│   │
│   ├── notification/
│   │   ├── notifier.go
│   │   ├── telegram.go
│   │   └── slack.go
│   │
│   └── repository/
│       ├── repository.go
│       └── sqlite.go
│
├── migrations/
│   └── 001_init.sql
│
├── configs/
│   └── config.example.yaml
│
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── README.md
```

---

## 18. Testing

Tập trung test vào business logic.

### Calculator

```text
open=100
close=110
=> +10%
```

```text
open=100
close=90
=> -10%
```

### Active window

```text
05:59 => inactive
06:00 => active
22:59 => active
23:00 => inactive
```

### 4H windows

```text
06:00 → 10:00 => notify
10:00 → 14:00 => notify
14:00 → 18:00 => notify
18:00 → 22:00 => notify
22:00 → 23:00 => skip
```

### Idempotency

```text
same symbol
same interval
same period

=> only one notification
```

---

## 19. Docker deployment

MVP deployment:

```text
VPS
 │
 └── Docker
      │
      └── crypto-alert
           ├── Go binary
           ├── config
           └── SQLite
```

Không cần Kubernetes.

Nếu sau này chuyển PostgreSQL:

```text
docker-compose
├── crypto-alert
└── postgres
```

---

## 20. MVP scope

### Must have

- [x] Go backend
- [x] Binance market data
- [x] Configurable crypto symbols
- [x] 1H interval
- [x] 4H interval
- [x] Asia/Ho_Chi_Minh timezone
- [x] Active window 06:00–23:00
- [x] Skip 23:00–06:00
- [x] 4H windows 06–10, 10–14, 14–18, 18–22
- [x] Skip 22–23 for 4H
- [x] Telegram
- [x] Slack adapter
- [x] Aggregated notifications
- [x] SQLite idempotency
- [x] Retry
- [x] Structured logging
- [x] Health check
- [x] Docker

### Không làm trong MVP

- [ ] Web UI
- [ ] User authentication
- [ ] Multi-user configuration
- [ ] Kafka
- [ ] RabbitMQ
- [ ] Redis
- [ ] Kubernetes
- [ ] Microservices
- [ ] Prometheus/Grafana
- [ ] Complex trading signals
- [ ] Automated trading

---

## 21. Suggested implementation order

### Step 1 — Bootstrap

- Initialize Go module.
- Config loader.
- `.env` / environment variables.
- Structured logging.

### Step 2 — Market provider

- Binance client.
- Kline API.
- Error handling.
- Retry.
- Unit tests.

### Step 3 — Time/window engine

Implement:

```go
GetCurrentPeriod(now time.Time, interval Interval) Period
```

Đây là core business logic cần test kỹ.

### Step 4 — Calculator

Implement:

```go
CalculateChange(open, close float64) float64
```

### Step 5 — Notification

Implement:

```text
Notifier
├── TelegramNotifier
└── SlackNotifier
```

### Step 6 — Scheduler

Connect:

```text
scheduler
    ↓
period
    ↓
market
    ↓
calculator
    ↓
notification
```

### Step 7 — SQLite

- Migration.
- Job repository.
- Idempotency.

### Step 8 — Health check + Docker

- `/health`
- Dockerfile.
- docker-compose.
- Persistent SQLite volume.

### Step 9 — End-to-end test

Test scenario:

```text
06:00 → 07:00
```

and:

```text
06:00 → 10:00
```

with Telegram test channel.

---

## 22. Future extensions

Sau khi MVP ổn định có thể thêm:

### Threshold

```yaml
alerts:
  min_change_percent: 1.0
```

Ví dụ:

```text
BTC +2.1% → notify
ETH +0.3% → skip
SOL -1.8% → notify
```

### Per-symbol threshold

```yaml
alerts:
  BTCUSDT:
    min_change_percent: 0.5

  ETHUSDT:
    min_change_percent: 1.0
```

### Volume alerts

```text
BTC
Price: +2.1%
Volume: +85%
```

### Price threshold

```text
BTC > 110,000
BTC < 100,000
```

### More providers

```text
Binance
Bybit
OKX
Coinbase
```

### More notification channels

```text
Telegram
Slack
Discord
Email
Webhook
```

### Web UI

Cho phép configure:

```text
Symbols
Intervals
Timezone
Active hours
Threshold
Notification channels
```

---

## 23. Final recommendation

MVP nên giữ architecture:

```text
                    ┌─────────────┐
                    │  Scheduler  │
                    └──────┬──────┘
                           │
                           ▼
                    ┌─────────────┐
                    │ Market      │
                    │ Provider    │
                    └──────┬──────┘
                           │
                           ▼
                    ┌─────────────┐
                    │ Calculator  │
                    └──────┬──────┘
                           │
                           ▼
                    ┌─────────────┐
                    │ Repository  │
                    │ SQLite      │
                    └──────┬──────┘
                           │
                           ▼
                    ┌─────────────┐
                    │ Notification│
                    └──────┬──────┘
                           │
                    ┌──────┴──────┐
                    ▼             ▼
                Telegram        Slack
```

Các abstraction nên có ngay từ đầu:

```text
MarketDataProvider
Notifier
JobRepository
```

Nhưng chỉ implement:

```text
BinanceProvider
TelegramNotifier
SlackNotifier
SQLiteRepository
```

Điều này giữ codebase nhỏ nhưng vẫn có đường mở rộng rõ ràng.

### Definition of Done

MVP được xem là hoàn thành khi:

1. Service chạy ổn định trong Docker.
2. Có thể config danh sách crypto bằng YAML.
3. 1H notification chạy đúng theo `Asia/Ho_Chi_Minh`.
4. 4H notification chạy đúng `06–10 / 10–14 / 14–18 / 18–22`.
5. Không có notification trong `23–06`.
6. 4H `22–23` được bỏ qua.
7. Telegram nhận message đúng format.
8. Slack có thể bật bằng config.
9. Restart service không gây duplicate notification.
10. Exchange timeout không làm crash service.
11. `/health` trả về `status=ok`.
12. Có unit tests cho calculation, timezone/window và idempotency.
