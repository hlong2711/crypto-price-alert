# Crypto Price Alert

Backend service for scheduled crypto price notifications.

The implementation plans are documented in `docs/plans/`.

## Positive futures funding rates

- API: `GET /api/v1/funding-rates?limit=5`
- Telegram: `/funding` or `/funding 10` (requires the existing Telegram chat adapter).

Returns positive Binance USDⓈ-M perpetual funding rates for USDT/USDC pairs,
ranked descending, including each pair's interval and next payment time (UTC).
Default limit is 5; allowed limits are 5–20. If fewer positive pairs exist, the
response explicitly reports the shortfall. Rates come directly from Binance;
next-payment rates are not guaranteed.

```bash
curl 'http://localhost:8080/api/v1/funding-rates?limit=5'
```

Funding is enabled by default; set `funding.enabled: false` to disable it without
changing price alerts. The read-only API does not send Telegram messages.
See [feature documentation and change tracking](docs/features/funding-rate/README.md).

## Local Docker setup

Copy `.env.example` to `.env`, fill notification secrets as needed, then run:

```bash
docker compose config
docker compose up --build
```

The application is exposed at `http://localhost:8080` and PostgreSQL data is stored in the `postgres_data` volume.

## Chat configuration

Chat configuration is disabled by default. To enable it, configure `chat.enabled` and one or more chat adapters in `configs/config.yaml`, then provide the corresponding secrets from `.env.example`.

Webhook endpoints are:

- Telegram: `POST /api/v1/chat/telegram/webhook`
- Slack slash command: `POST /api/v1/chat/slack/command`
- Slack Block Kit interactions: `POST /api/v1/chat/slack/interaction`

Expose these endpoints through HTTPS in deployments connected to Telegram or Slack. The application validates Telegram's secret token and Slack's signed request headers before processing requests. Health checks are available at `/api/health` and do not expose credentials.

When Telegram chat is enabled, startup calls Telegram `setWebhook` with `chat.webhook_base_url` plus `/api/v1/chat/telegram/webhook`; `WEBHOOK_URL` must therefore be the externally reachable HTTPS base URL.

For local development, set `chat.telegram.skip_webhook_registration: true` to serve the local route without changing Telegram's remote webhook configuration.
