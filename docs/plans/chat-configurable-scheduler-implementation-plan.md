# Chat-Configurable Scheduler — Technical Implementation Plan

## Status

Planning only. No implementation changes are included in this document.

The existing working-tree change to `configs/config.yaml` must remain untouched.

## Objective

Allow users to configure scheduled crypto alerts from Telegram or Slack chat, including:

- selecting symbols;
- selecting one or more intervals;
- enabling or pausing alerts;
- viewing the current configuration;
- testing the notification flow.

Only the creator of the Telegram group or Slack channel may change its configuration.

Each chat must have an independent configuration and alert history.

## Recommended architecture

Treat every Telegram group or Slack channel as an independent alert target with its own:

- selected symbols;
- selected intervals;
- enabled/paused state;
- platform identity;
- authorization owner;
- configuration version.

The current YAML configuration will define defaults and the allowed symbol/interval catalog. Runtime chat configuration will be persisted in PostgreSQL.

```text
Chat webhook
  → platform authentication
  → command/parser
  → creator authorization
  → configuration service
  → PostgreSQL
  → scheduler configuration loader
  → per-chat executor
  → Telegram/Slack alert delivery
```

The current service is a single-process Go application with fixed cron expressions, a global symbol list, and send-only Telegram/Slack integrations. The implementation therefore requires both inbound chat adapters and a scheduler refactor.

## Platform integration

### Telegram

Add a Telegram webhook endpoint:

```text
POST /webhooks/telegram
```

Configure the bot with `setWebhook`, using HTTPS and a secret token. The handler must verify the `X-Telegram-Bot-Api-Secret-Token` header before processing updates. Telegram supports webhook secret tokens and retries unsuccessful webhook requests. See the [Telegram Bot API](https://core.telegram.org/bots/api).

For every mutating command:

1. Read the group ID and sender user ID.
2. Call `getChatAdministrators`.
3. Locate the administrator whose status is `creator`.
4. Compare that Telegram user ID with the command sender.
5. Reject the request unless they match.

The bot must be added to the group with sufficient permissions to receive messages and respond.

### Slack

The current Slack incoming webhook is insufficient for configuration because it cannot receive commands or reliably identify the sender.

Add a Slack app with:

- a slash command, such as `/crypto-alert`;
- Events API or Socket Mode;
- an interactive message endpoint for buttons and menus;
- a bot token for sending messages;
- a signing secret for request verification.

Recommended endpoints:

```text
POST /webhooks/slack/commands
POST /webhooks/slack/events
POST /webhooks/slack/interactions
```

Slack signs incoming requests with `X-Slack-Signature`. The service must verify the raw request body and reject stale timestamps to prevent replay attacks. See [Slack request verification](https://api.slack.com/docs/verifying-requests-from-slack).

Slack requires an HTTP request URL or Socket Mode to deliver events. See the [Slack Events API](https://api.slack.com/apis/connections/events-api).

For authorization:

1. Read `team_id`, `channel_id`, and sender `user_id`.
2. Call Slack's conversation information API.
3. Read the channel's `creator`.
4. Allow configuration only when `sender_user_id == channel.creator`.

Slack channel metadata includes a `creator` field. See the [Slack conversations API](https://api.slack.com/methods/conversations.list).

If the channel creator is unavailable or the app lacks permission to inspect the channel, reject configuration rather than falling back to workspace-admin privileges.

## Chat command UX

Use one command namespace for both platforms.

### Command discoverability and quick selection

The command list should be available from the chat application's native command UI, while symbols and intervals should be selected through interactive controls rather than typed manually whenever possible.

There are two separate concerns:

1. discovering the available top-level commands;
2. selecting dynamic values such as symbols and intervals.

The platform command menu should contain stable top-level commands:

```text
help       Show available commands and current permissions
configure  Open the guided configuration flow
show       Show the current configuration
enable     Enable alerts for this chat
pause      Pause alerts for this chat
test       Send a test alert
```

The command menu must not be treated as an authorization boundary. A non-creator may still see a command, invoke it, or submit an old interactive message. Every mutating command and every button interaction must perform the creator check on the server.

#### Telegram command menu

Register the top-level commands using Telegram's `setMyCommands` API. Telegram supports command scopes including all group chats, all chat administrators, a specific chat, and a specific chat member. The bot command list can therefore be tailored to the group or creator when the target is known. See the [Telegram Bot API command scopes](https://core.telegram.org/bots/api).

Recommended registration:

- default scope: `help`, `configure`, `show`, `enable`, `pause`, `test`;
- group scope: the same commands, so users can discover the bot;
- creator-specific scope: the same commands with descriptions emphasizing configuration actions, if desired.

The server must still authorize `configure`, `enable`, and `pause` using the Telegram group creator check. Command visibility is only a usability feature.

Telegram's native command menu is not suitable for a long or dynamic symbol list. After `/crypto-alert configure`, show symbols as an inline keyboard with one toggle button per allowed symbol, followed by interval buttons and a final `Save` button.

#### Slack command menu

Slack slash commands are registered in the Slack app configuration. Register one distinctive command, for example:

```text
/crypto-alert
```

Configure its usage hint as:

```text
[help|configure|show|enable|pause|test]
```

Slack displays the slash command and usage hint while the user types in the message composer. The command handler should also respond to `/crypto-alert help` and unknown input with the complete command list. Slack recommends providing a help action for slash commands. See [Slack slash commands](https://api.slack.com/tutorials/your-first-slash-command).

After `/crypto-alert configure`, return an ephemeral Block Kit response containing:

- a multi-select menu for symbols;
- a select menu or buttons for intervals;
- a `Save` button;
- a `Cancel` button.

Interactive payloads must contain the target channel, initiating user, and configuration session ID. On `Save`, the service must re-check the Slack channel creator and the session version before committing the configuration.

#### Shared command response

The `/crypto-alert help` response should be equivalent on both platforms:

```text
/crypto-alert configure — Select symbols and intervals
/crypto-alert show      — Show current configuration
/crypto-alert enable    — Enable alerts
/crypto-alert pause     — Pause alerts
/crypto-alert test      — Send a test notification
/crypto-alert help      — Show this list
```

The command parser should accept both the platform-native command form and a plain bot mention where supported, but it should normalize both into the same internal command model.

Recommended MVP commands:

```text
/crypto-alert help
/crypto-alert show
/crypto-alert configure
/crypto-alert symbols
/crypto-alert symbols BTCUSDT ETHUSDT SOLUSDT
/crypto-alert intervals
/crypto-alert intervals 1h 4h
/crypto-alert enable
/crypto-alert pause
/crypto-alert test
```

For Slack, the equivalent is the `/crypto-alert` slash command.

The interactive experience should provide buttons or menus:

```text
Configure alerts
├── Select symbols
├── Select intervals
├── Review configuration
└── Save
```

Telegram can use inline keyboards. Slack can use Block Kit buttons and select menus.

Recommended flow:

1. The creator runs `/crypto-alert configure`.
2. The bot displays allowed symbols.
3. The creator selects one or more symbols.
4. The bot displays allowed intervals.
5. The creator selects one or more intervals.
6. The bot shows a confirmation summary.
7. The creator presses `Save`.
8. The complete update is committed atomically.

Text commands remain available for automation and recovery.

Example confirmation:

```text
Alert configuration

Symbols: BTCUSDT, ETHUSDT, SOLUSDT
Intervals: 1h, 4h
Status: enabled

Save this configuration?
[Save] [Cancel]
```

Non-creators should receive a short response:

```text
Only the creator of this chat can change alert configuration.
```

Do not reveal internal authorization details or secrets.

## Database model

Extend the current `notification_jobs` table and add runtime configuration tables.

### `alert_targets`

Represents a Telegram group or Slack channel.

Suggested fields:

```text
id                  UUID primary key
provider            telegram | slack
tenant_id           Telegram bot scope or Slack team_id
external_chat_id    Telegram chat_id or Slack channel_id
display_name        nullable
creator_user_id     platform user ID
enabled             boolean
created_at
updated_at
```

Unique constraint:

```text
(provider, tenant_id, external_chat_id)
```

### `alert_configs`

One current configuration per target.

```text
target_id           UUID primary key
enabled             boolean
version             bigint
updated_by_user_id
updated_at
```

### `alert_config_symbols`

```text
target_id
symbol
```

Unique constraint:

```text
(target_id, symbol)
```

### `alert_config_intervals`

```text
target_id
interval
```

Unique constraint:

```text
(target_id, interval)
```

The normalized structure makes updates and validation easier than storing arrays in a JSON column.

### `inbound_events`

Used for webhook idempotency.

```text
id
provider
external_event_id
received_at
processed_at
status
error_message
```

Unique constraint:

```text
(provider, external_event_id)
```

This prevents duplicate Telegram or Slack deliveries from applying a command twice.

### Modify `notification_jobs`

Add:

```text
target_id UUID NOT NULL
```

Change the uniqueness constraint from:

```text
(symbol, interval, period_start)
```

to:

```text
(target_id, symbol, interval, period_start)
```

Otherwise, one group's completed job would incorrectly suppress another group's notification.

## Go package structure

Suggested additions:

```text
internal/chat/
    command.go
    parser.go
    session.go
    service.go
    authorization.go

internal/chat/telegram/
    webhook.go
    client.go
    types.go

internal/chat/slack/
    webhook.go
    client.go
    signature.go
    types.go

internal/configuration/
    service.go
    validator.go

internal/repository/
    target_repository.go
    config_repository.go
    event_repository.go

internal/scheduler/
    coordinator.go
    target_executor.go
```

The domain layer should remain independent of Telegram, Slack, Echo, and GORM.

Core interfaces:

```go
type ChatTargetRepository interface {
    FindOrCreate(...)
    Get(...)
    ListEnabled(...)
}

type AlertConfigRepository interface {
    Get(...)
    Replace(...)
    SetEnabled(...)
}

type InboundEventRepository interface {
    Claim(...)
    MarkProcessed(...)
    MarkFailed(...)
}

type ChatMessenger interface {
    SendText(ctx context.Context, target ChatTarget, text string) error
    SendConfigurationUI(ctx context.Context, target ChatTarget, view ConfigurationView) error
}
```

The existing `notification.Notifier` interface should evolve from a global destination:

```go
Send(ctx context.Context, message domain.Message) error
```

to a target-aware operation:

```go
Send(ctx context.Context, target domain.AlertTarget, message domain.Message) error
```

This allows the same scheduler to send to different Telegram groups and Slack channels.

## Configuration changes

Keep the existing allowed-value catalog:

```yaml
market:
  symbols:
    - BTCUSDT
    - ETHUSDT
    - SOLUSDT

  intervals:
    - 1h
    - 4h
```

Add a chat-management section:

```yaml
chat:
  enabled: true
  max_symbols_per_target: 20
  max_targets: 100
  webhook_base_url: https://alerts.example.com
  telegram:
    enabled: true
    webhook_secret: "${TELEGRAM_WEBHOOK_SECRET}"
  slack:
    enabled: true
    signing_secret: "${SLACK_SIGNING_SECRET}"
    bot_token: "${SLACK_BOT_TOKEN}"
```

The existing Telegram bot token remains necessary for sending messages and Telegram API calls.

The existing static Telegram `chat_id` and Slack webhook should either:

1. be migrated into an initial `alert_target`; or
2. remain supported as a legacy single-target mode during rollout.

Recommended approach: bootstrap the existing configured Telegram destination into an `alert_target`, while retaining the legacy path for one release.

## Configuration service behavior

All updates must be validated before persistence.

Validation rules:

- normalize symbols to uppercase;
- require every symbol to exist in the configured allowed-symbol catalog;
- require every interval to be configured and supported by the domain;
- require at least one symbol;
- require at least one interval;
- remove or consistently reject duplicates;
- enforce the maximum symbol count;
- retain symbols and intervals when a target is paused;
- replace the complete configuration in one database transaction.

Use optimistic versioning:

```text
UPDATE alert_configs
SET version = version + 1
WHERE target_id = ? AND version = ?
```

This prevents two interactive configuration sessions from silently overwriting one another.

## Scheduler changes

The current scheduler has fixed cron expressions and one global symbol list. Replace that behavior with a scheduler coordinator.

Recommended operation:

1. Run a coordinator tick every minute.
2. Load enabled targets and their configurations.
3. Determine whether the current time closes a configured 1h or 4h period.
4. Create one execution task per target and interval.
5. Execute symbols configured for that target.
6. Build one aggregated message per target and period.
7. Send it to that target.
8. Mark target-specific jobs as sent or failed.

The existing period engine can remain mostly unchanged.

Example:

```text
10:00
├── target A: 1h, BTCUSDT + ETHUSDT
├── target B: 1h, BTCUSDT
└── target C: no 1h interval configured

11:00
├── target A: 1h + 4h
├── target B: 4h
└── target C: 1h only
```

The database uniqueness constraint remains the final idempotency boundary.

For the initial deployment, assume one application instance. If multiple replicas are expected later, add PostgreSQL advisory locks or a distributed scheduler lock before enabling horizontal scaling.

## Webhook processing

Webhook handlers should acknowledge quickly and process commands asynchronously.

Recommended flow:

```text
Receive request
  → verify signature/secret
  → decode event
  → claim inbound event ID
  → return HTTP 200
  → enqueue command
  → authorize creator
  → apply configuration
  → send response
```

This is particularly important for Slack, where delayed responses can cause retries. Slack recommends decoupling event ingestion from processing for higher-volume integrations. See [Slack request URLs](https://api.slack.com/apis/http).

Use:

- a bounded in-memory queue for MVP;
- a worker pool;
- event deduplication in PostgreSQL;
- structured logs containing provider, chat ID, event ID, and result;
- no bot tokens, signing secrets, or webhook URLs in logs.

## API and deployment changes

Add public HTTPS routing for:

```text
/webhooks/telegram
/webhooks/slack/commands
/webhooks/slack/events
/webhooks/slack/interactions
```

The existing Echo server can host these routes.

Update Docker and deployment documentation to include:

- public HTTPS URL;
- Telegram webhook registration;
- Slack app request URLs;
- Slack signing secret;
- Slack bot token;
- Telegram webhook secret;
- required Slack OAuth scopes;
- reverse proxy or TLS termination.

The current local Docker setup can still test the application, but real Telegram and Slack webhooks require a publicly reachable HTTPS endpoint or a development tunnel.

## Implementation phases

### Phase 1: domain and persistence

- Add `AlertTarget`, `AlertConfig`, and inbound event domain types.
- Add database models and migrations.
- Extend notification jobs with `target_id`.
- Add repositories and repository tests.
- Preserve existing scheduler behavior behind a compatibility path.

### Phase 2: configuration service

- Implement target discovery and creation.
- Implement configuration replacement and pause/resume.
- Add validation and optimistic versioning.
- Add service tests for authorization-independent configuration updates.

### Phase 3: Telegram integration

- Add webhook route.
- Add secret-header validation.
- Add Telegram update parsing.
- Add `getChatAdministrators` authorization.
- Add command responses.
- Add inline-keyboard configuration flow.
- Add Telegram integration tests using a fake HTTP server.

### Phase 4: Slack integration

- Add request signature verification.
- Add slash-command handling.
- Add Events API or Socket Mode support.
- Add channel creator lookup.
- Add Block Kit configuration flow.
- Add Slack message delivery using `chat.postMessage`.
- Add Slack integration tests.

### Phase 5: dynamic scheduler

- Replace global `symbols` usage with target configurations.
- Refactor the executor to operate per target.
- Add target-aware notification delivery.
- Add target-specific job idempotency.
- Add scheduler reload/tick behavior.
- Add tests for different configurations at the same timestamp.

### Phase 6: migration and rollout

- Add a feature flag such as `chat.enabled`.
- Bootstrap the existing configured Telegram destination into an `alert_target`.
- Keep static configuration as a fallback during one release.
- Enable chat configuration in dry-run mode first.
- Verify creator authorization and notification routing.
- Remove legacy static-destination behavior after successful migration.

## Test plan

### Unit tests

- command parsing;
- symbol normalization;
- interval validation;
- maximum symbol enforcement;
- creator authorization;
- Telegram webhook secret validation;
- Slack HMAC signature validation;
- replay timestamp rejection;
- configuration replacement;
- optimistic version conflict;
- inbound event deduplication.

### Repository tests

- target uniqueness;
- configuration replacement transaction;
- rollback on invalid update;
- target-specific job uniqueness;
- concurrent configuration updates;
- duplicate inbound events.

### Scheduler tests

- one target with one interval;
- one target with both intervals;
- multiple targets with different symbols;
- paused target;
- target added while the process is running;
- configuration changed between periods;
- duplicate scheduler ticks;
- one target notification failure not affecting another target.

### End-to-end tests

```text
Telegram creator → configure symbols → scheduler tick → Telegram alert
Telegram member → configure symbols → rejected
Slack channel creator → configure intervals → Slack alert
Slack non-creator → configure intervals → rejected
Duplicate webhook event → applied once
Invalid symbol → rejected without database change
```

## Acceptance criteria

The feature is ready when:

- Telegram group creators can configure symbols and intervals from chat;
- Slack channel creators can configure symbols and intervals from the Slack command UI;
- non-creators cannot change configuration;
- configuration survives application restart;
- different chats can have different symbols and intervals;
- pausing a chat stops alerts without deleting its configuration;
- scheduler changes take effect without restarting the process;
- duplicate webhook deliveries are harmless;
- a failed notification for one chat does not stop alerts for other chats;
- existing scheduled-job idempotency remains intact;
- no secrets appear in logs or chat responses;
- `go test ./...`, `go vet ./...`, and `go build ./...` pass.

## Decisions to confirm before implementation

1. Authorization should strictly mean Telegram group creator and Slack channel creator, not workspace or chat administrators.
2. Slack should use slash commands plus interactive Block Kit controls for the MVP.
3. Telegram should use webhooks rather than long polling in production.
4. YAML symbols and intervals should remain the allowed catalog; chat users should not be able to add arbitrary Binance symbols.
5. Existing static notification configuration should be migrated into a persisted default target during rollout.
