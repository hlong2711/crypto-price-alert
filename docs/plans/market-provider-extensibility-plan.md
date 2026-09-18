# Multi-provider market-data plan

## Goal and decisions

Add a market-provider registry that supports Binance and CoinMarketCap (CMC). Each alert target selects **exactly one** market provider as part of its alert configuration. Symbols and intervals remain shared, global allow-lists, so the same choices are presented across Telegram, Slack, the API, and scheduled alerts.

This plan uses CMC's DEX K-line endpoint, `GET /v1/k-line/candles`, rather than CMC's separate cryptocurrency OHLCV endpoint. It supports the shared interval set: `1m`, `3m`, `5m`, `15m`, `30m`, `1h`, `2h`, `4h`, `6h`, `8h`, `12h`, `1d`, `3d`, and `1w`. Remove `10m` from the domain entirely; neither Binance nor CMC supports it in this application. The endpoint requires a chain/platform and token or pool address, returns positional OHLCV data, and is available through CMC's keyless `/public-api` path.

The first release remains one provider per target—not one provider per symbol or interval. A target changing provider creates new provider-specific notification jobs; it must not reuse a job created from another provider's candle.

## Current code that must change

| Area | Current behavior | Required change |
| --- | --- | --- |
| `internal/market/provider.go` | One provider interface, no identity/registry. | Add a `MarketProvider` domain type and registry that selects the correct adapter per target. |
| `internal/market/binance.go` | Expects an exchange-pair symbol, e.g. `BTCUSDT`. | Keep adapter behavior but resolve shared logical symbols through its mapping. |
| `internal/config/config.go` | Accepts only `market.provider: binance`. | Replace the single provider setting with enabled provider definitions, a legacy/API default provider, shared allow-lists, and provider-specific symbol mappings. |
| `cmd/server/main.go` | Constructs one hard-coded `BinanceProvider`. | Construct all configured adapters once, validate the registry, and inject it into executors/services. |
| `internal/domain/types.go` | `AlertConfig` has symbols and intervals only; `Job` has no market source. | Add one required `MarketProvider` to both types and their validation. |
| `internal/database/chat_model.go` and `internal/database/model.go` | Alert configurations and notification-job unique keys do not include provider. | Persist `market_provider`; include it in notification-job uniqueness. |
| `internal/repository/*` | Reads/writes configs and jobs without provider. | Round-trip provider, update optimistic replacements, and query conflicts with provider included. |
| `internal/scheduler/target_executor.go` | Dedupe cache key is symbol only and fetches from one provider. | Fetch by `(provider, symbol)` and group/dedupe only within the same provider. |
| `internal/service/alert.go`, API handler | One globally injected provider. | Resolve a requested/default provider for non-target API runs; target runs use the provider saved in the target config. |
| Chat session, Telegram, Slack | Configuration flow selects symbols then intervals. | Add provider selection; filter selectable symbols/intervals to the selected provider's compatible subset while retaining global allow-lists as the source of truth. |

## Configuration model

Replace `market.provider` with a provider registry. `symbols` and `intervals` are shared logical allow-lists; provider mappings translate each logical symbol into the provider request it needs.

```yaml
market:
  # Used by the legacy fallback and a non-target API run when no provider is specified.
  default_provider: binance

  # Shared, user-facing logical IDs. They are not assumed to be HTTP request symbols.
  symbols: [BTC, ETH, SOL]
  intervals: [1m, 3m, 5m, 15m, 30m, 1h, 2h, 4h, 6h, 8h, 12h, 1d, 3d, 1w]

  providers:
    binance:
      enabled: true
      base_url: https://api.binance.com
      symbols:
        BTC: { symbol: BTCUSDT }
        ETH: { symbol: ETHUSDT }
        SOL: { symbol: SOLUSDT }

    coinmarketcap:
      enabled: true
      # Empty selects the keyless public path. Set api_key to use Pro API instead.
      base_url: https://pro-api.coinmarketcap.com
      api_key: "${COINMARKETCAP_API_KEY}"
      unit: usd
      symbols:
        BTC:
          platform: ethereum
          # Token address or an explicitly chosen pool address; choose and document one source.
          address: "0x..."
        ETH:
          platform: ethereum
          address: "0x..."
        SOL:
          platform: solana
          address: "..."
```

### Validation rules

1. Require at least one enabled provider and require `default_provider` to name an enabled provider.
2. Require non-empty, unique shared symbols and valid, unique shared intervals.
3. Require every enabled provider mapping key to be in `market.symbols`; reject unknown and duplicate keys.
4. At startup, create each adapter and expose its supported intervals. A provider may serve only a subset of the shared interval list.
5. CMC mappings require `platform` and `address`; Binance mappings require an exchange symbol.
6. A target configuration is valid only when its one selected provider is enabled and every selected shared symbol and interval is available through that provider.
7. An API caller may optionally choose a provider; absent one, use `default_provider`. Do not infer provider from the symbol.

The shared allow-lists stay shared. Provider mappings/capabilities are a second eligibility layer, so chat displays only the valid intersection for the provider that the user selected.

## Implementation blueprint

Implement the following slices in order. Each slice should compile and pass its focused tests before beginning the next; do not merge the database migration and the scheduler rewrite as one unverified change.

### 1. Domain types and compile-driven call-site discovery

Edit internal/domain/types.go.

- Add MarketProvider as a new type, with binance and coinmarketcap constants and a Validate method. Keep it separate from ChatProvider.
- Add MarketProvider to AlertConfig and Job.
- Require a valid non-empty market provider in AlertConfig.Validate and Job.Validate.
- Do not add provider to Candle or PriceChange: those values remain provider-neutral, while the selected provider is configuration and job provenance.

Update every config/job fixture and constructor to set MarketProviderBinance initially. Add domain tests for valid Binance/CMC values and empty/unknown values. This intentional compile break exposes every code path that creates an alert config or job.

### 2. Configuration structures and validation

Edit internal/config/config.go. Replace the single Provider field with:

    type MarketConfig struct {
        DefaultProvider domain.MarketProvider
        Symbols         []string
        Intervals       []string
        Providers       map[domain.MarketProvider]ProviderConfig
    }

    type ProviderConfig struct {
        Enabled bool
        BaseURL string
        APIKey  string
        Unit    string
        Symbols map[string]ProviderSymbolConfig
    }

    type ProviderSymbolConfig struct {
        Symbol   string // Binance request symbol
        Platform string // CMC platform slug or ID
        Address  string // CMC token or selected pool address
    }

Add the YAML tags matching the configuration example in this document. Config validation must normalize provider names and shared symbols, ensure the default provider exists and is enabled, reject empty/duplicate global symbols and intervals, and reject mapping keys outside the global symbol allow-list. Binance mappings require Symbol. CMC mappings require Platform and Address. Empty CMC Unit defaults to usd; only usd is accepted in this first release. An empty CMC API key is valid and means keyless public access.

Convert checked-in config files in the same slice: global symbols become BTC/ETH/SOL, while BTCUSDT/ETHUSDT/SOLUSDT move under the Binance mappings. Change the checked-in production schedule tick interval from 10m to 5m, then remove the Interval10M domain constant/duration case and all associated test cases. Do not enable CMC in a committed runtime config until real platform/address mappings are supplied.

Tests: old market.provider rejected; missing/disabled default rejected; duplicate global values rejected after normalization; invalid interval rejected; 10m rejected for both market intervals and schedule tick intervals; unknown mapped symbol rejected; missing provider-specific mapping fields rejected; API-key environment expansion covered.

### 3. Immutable provider registry

Add internal/market/registry.go. Keep the current MarketDataProvider GetKline signature unchanged; it is widely used by fakes and should not require unrelated callers to add support methods.

    type ProviderRegistry struct {
        providers       map[domain.MarketProvider]MarketDataProvider
        capabilities    map[domain.MarketProvider]providerCapabilities
        defaultProvider domain.MarketProvider
    }

    func NewProviderRegistry(config.MarketConfig, *http.Client, int, time.Duration, int) (*ProviderRegistry, error)
    func (r *ProviderRegistry) Get(domain.MarketProvider) (MarketDataProvider, error)
    func (r *ProviderRegistry) Default() (domain.MarketProvider, MarketDataProvider, error)
    func (r *ProviderRegistry) Enabled() []domain.MarketProvider
    func (r *ProviderRegistry) Supports(domain.MarketProvider, string, domain.Interval) bool

The constructor loops over enabled provider configurations, creates concrete adapters, records configured symbols plus provider interval support, and fails with a provider-qualified startup error. Supports is pure metadata and never makes an HTTP request. Return sorted provider names for deterministic chat output and tests.

Tests: default lookup, disabled/unknown provider error, stable Enabled order, compatible/incompatible symbol and interval checks, and malformed enabled CMC config preventing startup.

## Domain and market package changes

### Add provider identity and registry

Create a separate provider type; do not reuse `domain.ChatProvider`.

```go
type MarketProvider string

const (
    MarketProviderBinance       MarketProvider = "binance"
    MarketProviderCoinMarketCap MarketProvider = "coinmarketcap"
)

type MarketDataProvider interface {
    GetKline(ctx context.Context, symbol string, interval Interval, start, end time.Time) (Candle, error)
}

type ProviderResolver interface {
    Get(name MarketProvider) (MarketDataProvider, error)
    Supports(name MarketProvider, symbol string, interval Interval) bool
    Enabled() []MarketProvider
}
```

Keep upper layers independent of HTTP and CMC/Binance data formats. The registry is created at startup from config and is immutable thereafter. It owns no network discovery and therefore cannot make startup non-deterministic.

### Add `CoinMarketCapProvider`

Add `internal/market/coinmarketcap.go` and a constructor that accepts:

- base URL, HTTP client, retry/backoff/concurrency settings;
- optional API key;
- unit (`usd` initially);
- a map from shared logical symbol to `{platform, address}`.

For each `GetKline` call, the adapter must:

1. Validate the logical symbol, selected interval, and `start < end`.
2. Resolve the configured CMC mapping, then send `platform`, `address`, CMC interval, `from`, `to`, `unit`, and a bounded `limit` to `/v1/k-line/candles`.
3. Use `/public-api/v1/k-line/candles` when no key is configured; use `/v1/k-line/candles` and `X-CMC_PRO_API_KEY` when a key is configured. Never put the key in the URL or an error message.
4. Decode the positional candle format `[open, high, low, close, volume, timestamp, traders]` into `domain.Candle`. Ignore `traders` until the alert domain gains a use for it.
5. Parse the returned timestamp strictly as a Unix timestamp in seconds and normalize it to UTC. Select only a candle aligned with the requested period; reject timestamps outside the requested range.
6. Derive `CloseTime` as `OpenTime + interval duration`, then validate with `Candle.Validate` and the existing period checks.
7. Apply the same bounded-body, context cancellation, semaphore, and exponential-backoff behavior as Binance. Retry transport failures, 429, and 5xx; do not retry malformed data, invalid mappings, authentication errors, or other client errors.

Map the app's interval values to CMC K-line values explicitly: `1m → 1min`, `3m → 3min`, `5m → 5min`, `15m → 15min`, `30m → 30min`; hour/day/week values already match. Do not add the sub-minute CMC intervals to `domain.Interval` in this work, because the scheduler's current minute-based cadence cannot execute them correctly.

### Adapt Binance to logical symbols

Move its direct use of `BTCUSDT` behind a `map[logicalSymbol]binanceSymbol` resolver. The request and returned `domain.Candle.Symbol` should use the shared logical symbol (`BTC`), so notifications, chat configuration, persistence, and cross-provider job keys have stable names.

### 4. Market adapters

Modify internal/market/binance.go and its tests. Change the constructor to receive a logical-to-exchange-symbol map. GetKline resolves the logical ID before constructing the Binance request, but it restores the logical ID in Candle.Symbol before returning. An unmapped symbol is a non-retryable error. Existing retry, body limit, validation, and semaphore behavior stay unchanged.

Add internal/market/coinmarketcap.go and coinmarketcap_test.go. Its constructor takes base URL, optional API key, unit, a logical-symbol-to-(platform,address) map, HTTP client, retry settings, and concurrency.

The CMC GetKline algorithm is:

1. Validate symbol, interval, and bounds; resolve its platform/address; acquire the semaphore or return the cancelled context error.
2. Map domain intervals exactly: 1m to 1min, 3m to 3min, 5m to 5min, 15m to 15min, 30m to 30min; hour/day/week values pass through. There is no 10m domain interval or provider mapping.
3. Build the URL through net/url, with platform, address, interval, from, to, unit, and limit=2 query parameters. Send bounds as UTC Unix seconds.
4. If API key is empty, call public-api/v1/k-line/candles. Otherwise call v1/k-line/candles and set X-CMC_PRO_API_KEY; never include a key in an URL or error.
5. Read at most 1 MiB. Retry only transport failures, 429, and 5xx. For other non-2xx responses, return a non-retryable status-only error.
6. Decode each row as positional OHLCV: open, high, low, close, volume, timestamp, traders. Ignore traders.
7. Parse the returned timestamp strictly as a UTC Unix timestamp in seconds, using time.Unix(value, 0). Reject rows that cannot align with the requested period. Select only the row whose open time equals period start. Set close time to open time plus the interval duration and require it to equal period end.
8. Validate the returned Candle.

Tests: Binance request-symbol translation and logical return symbol; CMC public/keyed paths; query/header shape; all mappings; valid/empty/malformed rows; invalid OHLC; Unix-seconds timestamp parsing; boundary selection; 429/5xx retries; 400/401 no retry; cancellation; retry exhaustion; API-key absence from errors.

### 5. Database migration before runtime consumers

Edit internal/database/chat_model.go and model.go to add MarketProvider fields tagged as varchar(32), not null. Change the notification job unique-index tag so its ordered fields are target_id, market_provider, symbol, interval, period_start.

Edit database.Migrate in internal/database/database.go with this exact idempotent sequence:

1. AutoMigrate alert config models only after adding the alert_configs column safely, or explicitly add the nullable column, backfill binance, then require non-null.
2. Add nullable market_provider to notification_jobs if missing.
3. Backfill null provider values in both tables to binance.
4. Alter both columns to SET NOT NULL.
5. Drop idx_notification_jobs_target_key, the actual current unique-index name.
6. Create idx_notification_jobs_target_key as UNIQUE on target_id, market_provider, symbol, interval, period_start.
7. Run AutoMigrate NotificationJob after the explicit index replacement, or create the same index directly and prevent GORM from trying to change it.

Never rely on AutoMigrate alone to replace an existing unique index. Update repository conversion functions and insert/update maps in the same slice, so read and write paths round-trip provider. Existing stored configs/jobs backfill as Binance.

Tests: fresh schema, simulated upgrade with old rows, non-null backfill, exact new unique index, create/get/replace config provider preservation, pause/enable preservation, same job idempotent for one provider, and distinct same-period jobs for Binance versus CMC.

### 6. Provider-aware execution and API

Change Executor, TargetExecutor, and AlertService constructors to take the registry. The no-target legacy executor and API runs without provider use registry.Default.

In TargetExecutor:

- Load config.MarketProvider for every eligible target and resolve it before job creation.
- Write job.MarketProvider.
- Replace the Phase 2 map key from symbol with a struct containing provider and symbol.
- Fetch once for every distinct pair; use the matching provider for the fetch.
- Keep delivery output keyed by that same pair, so a Binance candle cannot be delivered to a CMC target.

In internal/api/handler.go, add Provider to runAlertRequest. Bind it from JSON and the provider query parameter. Pass it to AlertService.Run; AlertService resolves default when empty and rejects an unknown/unsupported provider-symbol-interval combination before requests begin.

Tests: two targets on different providers fetch twice and receive the correct source; two targets on same provider/symbol fetch once; provider switches create a new job; legacy fallback/default API run uses default provider; explicit unknown provider returns 400; provider unsupported symbol/interval returns 400.

### 7. Target configuration service and chat state machine

Add MarketProvider to ConfigSession, cloneSession, and NewConfigSession. Change ConfigurationService ReplaceConfig and UpdateSession signatures to include provider. configuration.Service validates registry.Supports for every selected symbol and interval before persistence.

Change the interaction sequence to provider -> symbols -> intervals -> save. On provider change, retain only selected symbols/intervals supported by the new provider; do not carry incompatible choices to Save.

Telegram changes: add a provider keyboard and provider callback, change Next/Back actions to provider-to-symbols-to-intervals, and include provider in the final text. Slack changes: add a provider selector/action value, regenerate blocks using the provider-compatible subsets, and include provider in saved/status output.

Tests: new session defaults to default provider; provider toggle changes eligible lists; incompatible selections are removed; back/cancel preserves valid state; save persists provider; stale session version still fails; Telegram and Slack callbacks cannot select a disabled provider or unsupported symbol/interval.

### 8. Documentation, staging verification, and release

Update README, environment example, and config examples with the canonical-symbol and CMC platform/address requirements. Add an opt-in CMC smoke test that requires a deliberate environment flag and test address; all normal tests use httptest fixtures only.

Deploy schema migration first, then the registry/adapters binary with CMC disabled or dry-run, then enable a test target with a known address. Compare selected candle open time, close time, OHLC, and volume to the CMC response before enabling notifications.

## Target configuration, chat, and API changes

### Persist one provider on each target config

Add `MarketProvider` to `domain.AlertConfig`, `database.AlertConfig`, repository conversions, and config validation. It is one non-null column, not a child table; the existing one-row-per-target design already enforces one provider per target.

Update `configuration.Service` signatures so `CreateConfig` and `ReplaceConfig` accept a provider. Its first-time default uses `market.default_provider`. Preserve the selected provider through pauses/enables and optimistic version updates.

Update `chat.ConfigSession` with `SelectedMarketProvider`. The interactive flow becomes:

1. Choose provider.
2. Choose compatible symbols from the shared allow-list.
3. Choose compatible intervals from the shared allow-list.
4. Review, save, enable/pause.

Add back/cancel behavior that preserves selections. Telegram keyboard and Slack Block Kit values must include the provider selection action. The status/readback message must display the selected market provider.

For `POST /api/v1/alerts/run`, add an optional `provider` request field. It selects a configured provider; omit it to use `default_provider`. Keep the API's `symbols` field expressed in shared logical IDs.

## Scheduler and job identity changes

Replace the single provider dependency in `Executor`, `TargetExecutor`, and `AlertService` with the registry.

For every eligible target, `TargetExecutor` reads `config.MarketProvider`, resolves it once, and records jobs with that provider. The Phase 2 fetch cache key becomes:

```go
type fetchKey struct {
    Provider domain.MarketProvider
    Symbol   string
}
```

This preserves current one-fetch-per-shared-symbol efficiency for targets using the same provider, but prevents a Binance candle from being delivered to a CMC target. Fetch groups may run concurrently within the existing request controls; preserve deterministic delivery and existing target-executor locking semantics.

Add `MarketProvider` to `domain.Job`, `database.NotificationJob`, conversion functions, and the unique job key. The uniqueness condition becomes `(target_id, market_provider, symbol, interval, period_start)`. This is required because a provider change during a period must not revive or suppress a job from the previous provider.

Keep `default_provider` only for non-target API runs and the existing no-enabled-target legacy fallback. Do not add any global runtime `market.provider` branch. If legacy execution is no longer required, remove the fallback in a separately approved cleanup after production migration.

## Database migration and compatibility

Use an explicit, safe PostgreSQL migration in `database.InitDatabase` (consistent with the existing `notification_jobs.target_id` migration):

1. Add `market_provider varchar(32)` to `alert_configs` and `notification_jobs` if absent.
2. Backfill existing rows with `binance`.
3. Set both columns `NOT NULL` and add a database check/list constraint if project migration conventions permit it.
4. Drop the old notification-job unique index only after confirming its exact name and columns.
5. Create the new unique index containing `market_provider`.
6. Deploy this schema migration before binaries that write provider-bearing configurations/jobs.

GORM `AutoMigrate` alone is insufficient for safely replacing an existing unique index, so index migration must be explicit and idempotent. Add migration tests or an integration check against a fresh database and an upgraded database. Existing targets continue using Binance after backfill.

## Files to add, modify, and remove

### Add

- `internal/market/coinmarketcap.go`
- `internal/market/coinmarketcap_test.go`
- `internal/market/registry.go` and `internal/market/registry_test.go`
- Provider mapping/config types and tests as needed under `internal/config/`
- Database migration tests or a dedicated upgrade test helper
- CMC example configuration and `COINMARKETCAP_API_KEY` in `.env.example`

### Modify

- `internal/domain/types.go` and tests
- `internal/market/provider.go`, `binance.go`, and Binance tests
- `internal/config/config.go` and tests; all checked-in config examples
- `cmd/server/main.go`
- `internal/database/model.go`, `chat_model.go`, `database.go`, and model tests
- `internal/repository/postgres.go`, `chat_repository.go`, repository interfaces, and tests
- `internal/scheduler/executor.go`, `target_executor.go`, and scheduler tests
- `internal/service/alert.go`, configuration service/validator, API handler/contracts, chat session/service, Telegram adapter, Slack adapter, and their tests
- README and this plan's CMC references

### Remove/replace

- Remove the `market.provider` configuration field and its Binance-only validation.
- Remove direct `NewBinanceProvider` construction from `main`.
- Remove single-provider assumptions from the executors, alert service, API request flow, and target fetch cache.
- Do not remove `BinanceProvider`, existing notification tables, or shared symbol/interval allow-lists.

## Test coverage

### Configuration and registry

- Valid multi-provider config, disabled provider, missing/unknown default provider, duplicate providers, empty shared lists, and malformed mappings.
- Global list rejects duplicates/invalid intervals; provider mappings reject symbols outside the shared list.
- CMC requires platform/address; Binance requires exchange symbol.
- Registry returns configured adapters, rejects disabled/unknown providers, and reports compatible symbols/intervals.

### Market adapters

- Binance resolves shared `BTC` to configured `BTCUSDT` but returns `Candle.Symbol == "BTC"`.
- CMC asserts public-versus-keyed path, required query parameters, CMC header behavior, interval mapping, address/platform encoding, and absence of secrets in errors.
- CMC decoding covers valid positional OHLCV, numeric/string values if accepted, missing rows/columns, invalid OHLC, malformed JSON, empty response, status errors, and epoch-unit normalization.
- Both adapters cover retryable transport/429/5xx, non-retryable 4xx, retry exhaustion, cancellation during backoff, body-size limit, and semaphore/context behavior.

### Domain, repository, and migration

- `AlertConfig` and `Job` reject missing/unknown market provider.
- Create/get/replace config round-trips its provider and preserves it through enable/pause.
- Old database rows backfill to Binance; upgraded schema has non-null provider columns and the new provider-inclusive unique index.
- Same target/symbol/interval/period can create independent Binance and CMC jobs; an identical provider job remains idempotent.

### Scheduler, service, API, and chat

- Two targets selecting different providers fetch from their own adapter and receive no cross-provider data.
- Two targets selecting the same provider and symbol share one fetch; same symbol under different providers results in two fetches.
- Missing/disabled provider or unsupported selected option yields an unavailable result/error without delivering a mismatched candle.
- Legacy/no-target execution and API omitted-provider runs use `default_provider`; explicit valid/invalid provider API cases are covered.
- Telegram and Slack: provider selector renders first, provider selection filters available shared symbols/intervals, back/cancel preserve state, saved configuration stores provider, and status output includes it.
- Existing notification formatting and delivery tests continue to pass with provider-neutral `PriceChange` values.

### Verification commands

Run `go test ./...` after each implementation slice. Add only opt-in live CMC smoke tests, requiring an explicit environment flag; normal tests must use `httptest.Server` and fixtures, never real CMC credentials or network calls.

## Delivery order

1. Add `MarketProvider`, provider fields on config/job domain types, and shared/provider mapping config validation.
2. Add database migration, repository round-tripping, and migration tests; retain Binance defaults for existing data.
3. Build registry and refactor Binance to logical symbols; convert startup, legacy executor, and API to registry use.
4. Implement and contract-test `CoinMarketCapProvider` against `/v1/k-line/candles`.
5. Make `TargetExecutor` provider-aware and change job/cache keys; cover cross-provider scheduling.
6. Add provider selection to configuration service, sessions, Telegram, Slack, and API contracts.
7. Update examples, `.env.example`, README, and deploy with CMC disabled or dry-run first.
8. Validate a known configured DEX address manually in staging, compare timestamp/candle alignment, then enable target notifications.

## Acceptance criteria

- An enabled target has exactly one persisted market provider and can select only shared symbols/intervals that provider supports.
- Binance behavior remains compatible after moving to shared logical symbols and backfilled provider data.
- The shared domain contains no `10m` interval; both Binance and CMC use the same remaining supported interval set.
- CMC requests always contain provider-specific chain/address configuration; no attempt is made to guess a DEX address from a ticker.
- Notification-job idempotency and in-memory fetch sharing are provider-aware.
- Database upgrade preserves existing targets/jobs as Binance data.
- Full unit/integration test coverage listed above passes; live tests are opt-in and do not expose credentials.

## References

- [CMC K-line / OHLCV API reference](https://coinmarketcap.com/api/documentation/pro-api-reference/ohlcv)
- [CMC keyless public API reference](https://coinmarketcap.com/api/documentation/pro-api-reference/keyless-public-api)
- [Current market interface](../../internal/market/provider.go)
- [Current target scheduler](../../internal/scheduler/target_executor.go)
- [Current configuration model](../../internal/domain/types.go)
