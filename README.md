# Crypto Price Alert

Backend service for scheduled crypto price notifications.

The implementation plans are documented in `docs/plans/`.

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
