# Chat-Configurable Scheduler — Technical Implementation Plan

## Status

Implementation is proceeding under the gated phase protocol. Phases 0–6 are complete; the next phase requires explicit approval.

The existing working-tree change to `configs/config.yaml` must remain untouched.

## Gated implementation protocol

Implementation must proceed one phase at a time.

For every phase:

1. Confirm the phase scope before editing code.
2. Implement only that phase.
3. Run the phase's required tests and verification commands.
4. Report changed files, test results, and any design decisions.
5. Stop and wait for explicit user approval.
6. Begin the next phase only after the previous phase passes and approval is received.

No phase may hide failing tests, weaken existing tests, or modify unrelated user changes. A phase that fails testing must be fixed before approval is requested again.

The first implementation turn should begin with Phase 0 only. The plan itself is not approval to implement all phases.

## Detailed code blueprint

### Phase 0 — Baseline and implementation scaffolding

**Status: DONE**

#### Goal

Establish a clean, measurable baseline before changing behavior.

#### Files to inspect or update

```text
README.md
docs/plans/crypto-price-alert-mvp-implementation-plan.md
docs/plans/chat-configurable-scheduler-implementation-plan.md
internal/config/config.go
internal/database/database.go
internal/scheduler/scheduler.go
internal/notification/*.go
cmd/server/main.go
```

#### Ordered steps

1. Inspect the current working tree and preserve unrelated modifications.
2. Run the current test suite:

   ```text
   go test ./...
   go vet ./...
   go build ./...
   ```

3. Record the current behavior of the fixed scheduler, global symbols, and configured notification destinations.
4. Define the feature flag and compatibility behavior before adding runtime configuration.
5. Add only empty package boundaries or interfaces if needed for later phases; do not change scheduler behavior in this phase.
6. Document any baseline failure as pre-existing before implementation begins.

#### Phase 0 tests and acceptance

- Existing tests pass, or known pre-existing failures are documented.
- The application still builds.
- No existing notification or scheduler behavior changes.
- The dirty `configs/config.yaml` change is preserved.
- The feature can be disabled without changing current runtime behavior.

#### Gate

Stop after baseline verification. Proceed only after the user approves the Phase 0 results.

---

### Phase 1 — Domain types, database models, and migrations

**Status: DONE**

The PostgreSQL/Docker integration check is intentionally deferred to the user's environment. Unit tests, static analysis, and build verification passed.

#### Goal

Introduce persistent alert targets and per-target configuration without connecting them to the scheduler yet.

#### Files to add or modify

```text
internal/domain/types.go
internal/domain/chat.go
internal/database/model.go
internal/database/database.go
internal/repository/repository.go
internal/repository/postgres.go
internal/repository/target_repository.go
internal/repository/config_repository.go
internal/repository/event_repository.go
internal/database/*_test.go
internal/repository/*_test.go
```

#### Domain types

Add validated types similar to:

```go
type ChatProvider string

const (
    ChatProviderTelegram ChatProvider = "telegram"
    ChatProviderSlack    ChatProvider = "slack"
)

type AlertTarget struct {
    ID              string
    Provider        ChatProvider
    TenantID        string
    ExternalChatID  string
    DisplayName     string
    CreatorUserID   string
    Enabled         bool
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type AlertConfig struct {
    TargetID       string
    Enabled        bool
    Symbols        []string
    Intervals      []Interval
    Version        int64
    UpdatedBy      string
    UpdatedAt      time.Time
}

type InboundEvent struct {
    Provider        ChatProvider
    ExternalEventID string
    ReceivedAt      time.Time
}
```

Validation must reject empty target identity, unsupported provider, empty creator identity, invalid intervals, empty symbols, and invalid version metadata.

#### Database models

Add GORM models for:

- `alert_targets`;
- `alert_configs`;
- `alert_config_symbols`;
- `alert_config_intervals`;
- `inbound_events`.

Extend `notification_jobs` with `target_id` and replace its uniqueness key with `(target_id, symbol, interval, period_start)`.

Do not make existing jobs impossible to migrate. The migration must define how old rows receive a legacy/default target ID.

#### Repository interfaces

Add repository methods for:

- finding a target by provider, tenant, and external chat ID;
- creating a target idempotently;
- loading an alert configuration;
- replacing symbols and intervals transactionally;
- enabling and pausing a target;
- listing enabled targets with their configurations;
- claiming an inbound event idempotently;
- marking an inbound event processed or failed.

The configuration replacement operation must update the parent version and child rows in one database transaction.

#### Tests

Add tests for:

- domain validation;
- target uniqueness;
- configuration child-row uniqueness;
- transaction rollback on invalid configuration;
- optimistic version conflict;
- inbound event uniqueness;
- target-specific notification job uniqueness;
- migration behavior for existing notification jobs.

#### Acceptance criteria

- `go test ./...` passes.
- `go vet ./...` passes.
- `go build ./...` passes.
- GORM migration creates all required tables and constraints.
- Repeated target creation returns the same target.
- Invalid configuration cannot partially persist.
- Two configurations for different targets can use the same symbol, interval, and period.
- Existing notification jobs remain readable after migration.
- No scheduler or notifier behavior changes yet.

#### Gate

Stop and report the schema, migration behavior, test output, and compatibility risks. Proceed only after user approval.

---

### Phase 2 — Configuration service and validation

**Status: DONE**

#### Goal

Create the application service that owns configuration changes independently of Telegram and Slack.

#### Files to add or modify

```text
internal/config/config.go
internal/config/config_test.go
internal/service/configuration/service.go
internal/service/configuration/validator.go
internal/service/configuration/service_test.go
internal/domain/chat.go
cmd/server/main.go
```

#### Configuration additions

Add validated configuration for:

```yaml
chat:
  enabled: false
  max_symbols_per_target: 20
  max_targets: 100
  webhook_base_url: ""
  telegram:
    enabled: false
    webhook_secret: "${TELEGRAM_WEBHOOK_SECRET}"
  slack:
    enabled: false
    signing_secret: "${SLACK_SIGNING_SECRET}"
    bot_token: "${SLACK_BOT_TOKEN}"
```

The initial default must be disabled unless explicitly enabled. Existing static notification configuration must continue to work while this feature is disabled.

#### Service operations

Implement application-level operations:

```text
GetTarget
GetConfig
ReplaceConfig
EnableTarget
PauseTarget
ListAllowedSymbols
ListAllowedIntervals
```

`ReplaceConfig` must:

1. verify that the target exists;
2. normalize symbols to uppercase;
3. validate symbols against `market.symbols`;
4. validate intervals against configured intervals and domain intervals;
5. enforce configured limits;
6. verify the expected version;
7. persist the complete configuration atomically;
8. return the new version.

Authorization is not implemented inside this service yet. The service receives an already-authorized actor or an authorization result from the chat layer.

#### Tests

Add tests for:

- valid configuration replacement;
- lowercase symbol normalization;
- unknown symbol rejection;
- unsupported interval rejection;
- duplicate handling;
- empty symbol and interval rejection;
- maximum symbol limit;
- pause and resume;
- version conflict;
- transaction failure;
- feature-disabled behavior;
- legacy static configuration compatibility.

#### Acceptance criteria

- Configuration rules are enforced in one service, not duplicated in chat adapters.
- Invalid updates leave the previous configuration unchanged.
- Successful updates increment the version exactly once.
- The service is testable with repository fakes.
- Chat remains disabled by default.
- Existing API and scheduler tests continue to pass.

#### Gate

Stop after configuration-service tests pass. Report the public service interfaces and validation rules. Proceed only after user approval.

---

### Phase 3 — Shared command model, sessions, and authorization

**Status: DONE**

Implemented in `internal/chat/` with an in-memory session store for the initial single-instance deployment. Docker/integration testing remains deferred to the user's environment.

#### Goal

Create platform-independent command processing and secure interactive configuration sessions.

#### Files to add

```text
internal/chat/command.go
internal/chat/parser.go
internal/chat/session.go
internal/chat/authorization.go
internal/chat/service.go
internal/chat/*_test.go
```

#### Command model

Define normalized commands:

```go
type CommandAction string

const (
    ActionHelp      CommandAction = "help"
    ActionConfigure CommandAction = "configure"
    ActionShow      CommandAction = "show"
    ActionEnable    CommandAction = "enable"
    ActionPause     CommandAction = "pause"
    ActionTest      CommandAction = "test"
)

type Command struct {
    Target       domain.AlertTarget
    ActorUserID  string
    Action       CommandAction
    Arguments    []string
    EventID      string
}
```

The parser must accept platform-specific input but return the same command model.

#### Authorization model

Define an adapter-independent authorization request:

```go
type AuthorizationRequest struct {
    Target      domain.AlertTarget
    ActorUserID string
    Action      CommandAction
}
```

Mutating actions require a positive creator check. Read-only actions may be available to all chat members according to product policy.

Every interaction must be authorized again when it is submitted. Never trust a button payload merely because it was generated by the application.

#### Interactive sessions

Add a short-lived configuration session containing:

```text
session_id
target_id
actor_user_id
base_config_version
selected_symbols
selected_intervals
expires_at
```

Sessions may be stored in PostgreSQL or an in-memory store for the first single-instance implementation. PostgreSQL is preferred if multiple instances are expected soon.

The final save must reject expired sessions, wrong actors, wrong targets, and stale configuration versions.

#### Tests

Add tests for:

- every supported command;
- unknown command;
- malformed arguments;
- actor identity propagation;
- creator authorization success and failure;
- read-only versus mutating action policy;
- session ownership;
- session expiration;
- stale version rejection;
- replayed interaction rejection.

#### Acceptance criteria

- Telegram and Slack adapters can share the same parser and service.
- No mutation occurs without a creator authorization result.
- An interaction cannot be reused by another user.
- A stale configuration session cannot overwrite a newer configuration.
- Command responses do not contain credentials or internal stack traces.

#### Gate

Stop after command, authorization, and session tests pass. Proceed only after user approval.

---

### Phase 4 — Telegram adapter

**Status: DONE**

Implemented in `internal/chat/telegram/`, registered conditionally from `cmd/server/main.go`, with an Echo route helper in `internal/api/routes.go`. Docker/integration testing remains deferred to the user's environment.

#### Goal

Receive Telegram commands and provide native command discovery plus inline configuration controls.

#### Files to add or modify

```text
internal/chat/telegram/types.go
internal/chat/telegram/client.go
internal/chat/telegram/webhook.go
internal/chat/telegram/commands.go
internal/chat/telegram/telegram_test.go
internal/api/routes.go
cmd/server/main.go
```

#### Ordered steps

1. Define only the Telegram payload types needed for messages, callback queries, chats, users, and administrators.
2. Implement the Telegram API client with timeout, retry classification, and response validation.
3. Implement webhook secret-header verification.
4. Implement update decoding and event ID extraction.
5. Implement `getChatAdministrators` lookup.
6. Implement creator comparison using numeric Telegram user IDs.
7. Register native top-level commands using `setMyCommands`.
8. Implement `/crypto-alert` parsing and shared command dispatch.
9. Implement inline-keyboard rendering for symbols, intervals, save, and cancel.
10. Implement callback payload validation and session lookup.
11. Send responses through the Telegram chat ID associated with the target.
12. Claim inbound events before enqueueing them.
13. Return HTTP 200 quickly after authentication and event claim.

#### Telegram command list

Register:

```text
help       Show available commands
configure  Configure symbols and intervals
show       Show current configuration
enable     Enable alerts
pause      Pause alerts
test       Send a test alert
```

The command menu is for discoverability only. Creator authorization must run for every mutation and callback.

#### Tests

- valid webhook secret;
- invalid webhook secret;
- malformed update;
- duplicate update ID;
- message command parsing;
- callback query parsing;
- creator returned by `getChatAdministrators`;
- non-creator rejection;
- Telegram API timeout and retry;
- Telegram API non-retryable error;
- command registration payload;
- inline keyboard payload;
- expired session;
- successful save;
- failed save leaves configuration unchanged.

#### Acceptance criteria

- A Telegram creator can open and complete the configuration flow.
- A Telegram non-creator cannot mutate configuration through text or buttons.
- The Telegram webhook rejects unauthenticated requests.
- Duplicate updates are processed once.
- The bot command menu exposes the supported commands.
- Symbols and intervals are selected from allowed values.
- Existing Telegram notifications remain functional when chat configuration is disabled.

#### Gate

Stop after Telegram adapter tests pass. Perform no Slack or scheduler work until user approval is received.

---

### Phase 5 — Slack adapter

**Status: DONE**

Implemented in `internal/chat/slack/`, with authenticated slash-command and Block Kit interaction routes registered conditionally from `cmd/server/main.go`. Docker/integration testing remains deferred to the user's environment.

#### Goal

Receive Slack commands and provide slash-command autocomplete plus Block Kit configuration controls.

#### Files to add or modify

```text
internal/chat/slack/types.go
internal/chat/slack/client.go
internal/chat/slack/signature.go
internal/chat/slack/webhook.go
internal/chat/slack/blocks.go
internal/chat/slack/slack_test.go
internal/api/routes.go
cmd/server/main.go
internal/config/config.go
configs/config.example.yaml
```

#### Ordered steps

1. Define URL-encoded slash-command payload types and JSON interaction payload types.
2. Preserve the raw request body before parsing.
3. Verify the timestamp and HMAC signature.
4. Reject requests outside the replay window.
5. Validate the expected Slack app ID/team context.
6. Implement the Slack Web API client with timeout and response validation.
7. Implement channel creator lookup.
8. Implement slash-command acknowledgment within Slack's deadline.
9. Dispatch the normalized command asynchronously.
10. Render ephemeral help and status responses.
11. Render Block Kit symbol and interval controls.
12. Validate callback action IDs, target channel, actor, and session ID.
13. Re-check channel creator on every mutation.
14. Save through the shared configuration service.
15. Respond through the interaction response URL or Slack Web API.

Implemented endpoints:

```text
POST /api/v1/chat/slack/command
POST /api/v1/chat/slack/interaction
```

The Slack app slash-command manifest should point `/crypto-alert` at the command endpoint and use `help|configure|show|enable|pause|test` as its usage hint. Interactive Block Kit actions use the interaction endpoint.

#### Slack command registration

Register one distinctive slash command:

```text
/crypto-alert
```

Usage hint:

```text
[help|configure|show|enable|pause|test]
```

The handler must respond to `/crypto-alert help` and unknown input with the full command list.

#### Tests

- valid Slack signature;
- invalid signature;
- stale timestamp;
- malformed form payload;
- wrong app ID or team ID;
- command acknowledgment;
- command parsing;
- channel creator success;
- non-creator rejection;
- Block Kit action parsing;
- wrong actor or channel in callback;
- expired session;
- stale config version;
- Slack API retryable and non-retryable errors;
- ephemeral response rendering;
- duplicate event handling.

#### Acceptance criteria

- A Slack channel creator can configure symbols and intervals from the slash-command UI.
- A Slack non-creator cannot mutate configuration.
- Slack requests are authenticated with the signing secret.
- Slash commands are acknowledged within the required response window.
- Symbol and interval controls use allowed values only.
- Interactive responses are scoped to the initiating user where appropriate.
- Existing Slack notifications remain functional when chat configuration is disabled.

#### Gate

Stop after Slack adapter tests pass. Proceed to scheduler integration only after explicit user approval.

---

### Phase 6 — Target-aware notification delivery

**Status: DONE**

Implemented target-aware delivery in `internal/notification/` and `internal/service/alert.go`. Existing static notifier constructors and compatibility behavior remain available. Docker/integration testing remains deferred to the user's environment.

#### Goal

Make notification delivery target-aware while preserving current notifier behavior in compatibility mode.

#### Files to modify

```text
internal/domain/types.go
internal/notification/notifier.go
internal/notification/telegram.go
internal/notification/slack.go
internal/notification/dryrun.go
internal/notification/notifiers_test.go
internal/service/alert.go
internal/service/alert_test.go
internal/scheduler/executor.go
internal/repository/repository.go
internal/repository/postgres.go
```

#### Ordered steps

1. Add an alert-target delivery abstraction.
2. Keep current constructors and compatibility behavior where possible.
3. Add Telegram target chat ID resolution.
4. Add Slack target channel posting through the bot token.
5. Keep the existing incoming webhook path only for legacy mode.
6. Ensure each target receives only its own message.
7. Preserve partial symbol-failure behavior.
8. Return per-target delivery results.
9. Mark target-specific jobs after the delivery policy completes.

#### Tests

- Telegram sends to the requested target chat;
- Slack sends to the requested target channel;
- target A cannot receive target B's message;
- one target failure does not prevent another target;
- dry-run target output;
- retry behavior;
- job status updates;
- compatibility-mode notifier behavior.

#### Acceptance criteria

- Notification delivery has no global mutable destination.
- Each message is routed to the correct target.
- Job uniqueness includes target ID.
- Existing single-target tests and behavior still pass.

#### Gate

Stop after target-aware notification tests pass. Proceed only after approval.

---

### Phase 7 — Dynamic scheduler integration

#### Goal

Run different alert configurations for different targets without restarting the service.

#### Files to modify or add

```text
internal/scheduler/scheduler.go
internal/scheduler/coordinator.go
internal/scheduler/executor.go
internal/scheduler/target_executor.go
internal/scheduler/*_test.go
cmd/server/main.go
```

#### Ordered steps

1. Keep the period engine as the source of truth for 1h and 4h periods.
2. Replace fixed global symbol execution with enabled-target loading.
3. Add a coordinator tick every minute.
4. Resolve due periods using the configured application timezone.
5. Create one target execution task per target and interval.
6. Load the target configuration immediately before execution.
7. Create target-specific jobs idempotently.
8. Skip already-sent target jobs.
9. Fetch all configured symbols for the target.
10. Build one aggregated message per target and period.
11. Send through the target-aware notifier.
12. Mark each target job sent or failed.
13. Preserve graceful scheduler shutdown.
14. Add a single-instance mutex around in-process target execution if required.
15. Do not add multi-replica support until the single-instance behavior is verified.

#### Tests

- one target and one interval;
- one target and both intervals;
- multiple targets with different symbols;
- multiple targets with different intervals;
- paused target skipped;
- target added without restart;
- configuration changed before the next period;
- duplicate coordinator ticks;
- provider failure for one symbol;
- notifier failure for one target;
- no execution during the inactive window;
- exact 1h and 4h period boundaries;
- graceful scheduler stop.

#### Acceptance criteria

- Runtime configuration takes effect without restart.
- Different chats receive different symbol sets and intervals.
- Paused targets produce no notifications.
- Duplicate ticks cannot duplicate sent jobs.
- Existing period and timezone semantics remain unchanged.
- One target's failure does not stop other targets.
- Scheduler tests pass deterministically without real waiting.

#### Gate

Stop after scheduler tests pass. Report concurrency and consistency behavior. Proceed only after explicit user approval.

---

### Phase 8 — HTTP wiring, startup, and deployment

#### Goal

Wire the feature into the application lifecycle and make webhook deployment configurable.

#### Files to modify

```text
internal/api/routes.go
internal/api/handler.go
internal/api/handler_test.go
cmd/server/main.go
configs/config.example.yaml
.env.example
docker-compose.yml
README.md
```

#### Ordered steps

1. Add webhook routes to Echo.
2. Inject chat services and platform clients through constructors.
3. Validate chat configuration at startup.
4. Initialize repositories before chat services.
5. Initialize chat adapters before webhook routes.
6. Start the scheduler only after database migration and service initialization succeed.
7. Preserve health-check behavior.
8. Add graceful shutdown for webhook workers and scheduler.
9. Document HTTPS requirements and platform setup.
10. Add environment variables without committing secrets.
11. Update Docker Compose health and port documentation if needed.

#### Tests

- route registration;
- health endpoint;
- startup with chat disabled;
- startup with Telegram enabled;
- startup with Slack enabled;
- invalid chat configuration;
- graceful shutdown;
- webhook HTTP status behavior;
- Docker Compose configuration validation.

#### Acceptance criteria

- Chat-disabled deployments behave as before.
- Chat-enabled deployments start only with valid secrets and configuration.
- Webhook routes are reachable and authenticated.
- Health checks do not expose credentials.
- `docker compose config` passes.

#### Gate

Stop after application wiring and deployment tests pass. Proceed only after approval.

---

### Phase 9 — Legacy migration and rollout

#### Goal

Migrate existing static notification configuration safely and enable the feature gradually.

#### Files to modify or add

```text
internal/database/database.go
internal/repository/migration.go
cmd/server/main.go
configs/config.example.yaml
README.md
docs/plans/chat-configurable-scheduler-implementation-plan.md
```

#### Ordered steps

1. Define the legacy target identity deterministically.
2. Create a legacy target for the existing Telegram chat if configured.
3. Define how Slack webhook-only configuration maps to a target, or require explicit Slack app setup.
4. Seed the legacy target's symbols and intervals from YAML.
5. Ensure the migration is idempotent.
6. Run the service with chat configuration disabled and verify unchanged behavior.
7. Enable chat configuration in dry-run mode.
8. Verify creator authorization in real test groups/channels.
9. Enable real chat-driven configuration.
10. Monitor webhook errors, authorization failures, scheduler failures, and notification failures.
11. Remove legacy behavior only in a separately approved cleanup phase.

#### Tests

- clean database migration;
- migration with existing jobs;
- repeated migration;
- legacy Telegram target seeding;
- incomplete Slack legacy configuration;
- chat-disabled compatibility;
- dry-run delivery;
- rollback or restart during migration.

#### Acceptance criteria

- Existing jobs and destinations are not duplicated.
- Restarting the service does not create duplicate targets or configuration rows.
- Chat can be enabled without losing existing schedules.
- Rollback to chat-disabled mode remains possible.
- Migration behavior is documented and observable.

#### Gate

Stop after migration and rollout verification. Proceed only after user approval to remove or deprecate compatibility behavior.

---

### Phase 10 — Full verification and handoff

#### Goal

Verify the complete feature and produce the final implementation report.

#### Required commands

```text
gofmt -w <changed Go files>
go test ./...
go vet ./...
go build ./...
docker compose config
```

Run integration and end-to-end tests using fake Binance, Telegram, Slack, and PostgreSQL dependencies where applicable.

#### Required end-to-end scenarios

```text
Telegram creator → configure symbols → configure intervals → scheduler tick → Telegram alert
Telegram member → configure symbols → rejected
Slack channel creator → configure symbols → configure intervals → Slack alert
Slack non-creator → configure intervals → rejected
Invalid symbol → rejected without database change
Duplicate webhook event → applied once
Duplicate scheduler tick → one notification
Paused target → no notification
One target failure → other target continues
Application restart → configuration survives
```

#### Final acceptance criteria

- All tests pass.
- No known regression exists in the original scheduler behavior.
- No secrets are logged or committed.
- Database migrations are repeatable.
- Authorization is enforced for every mutating path.
- Interactive sessions cannot be replayed or transferred between users.
- Runtime configuration is isolated per chat.
- Documentation includes setup, commands, permissions, webhook configuration, and rollback.

#### Final gate

Stop and provide the complete change summary, test evidence, migration notes, and operational risks. Do not perform additional cleanup or refactoring without separate approval.

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

internal/service/configuration/
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
