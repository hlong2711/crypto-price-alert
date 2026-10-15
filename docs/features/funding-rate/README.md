# Positive funding rates

Implemented; real Telegram delivery still requires a deployment smoke test.
[Specification](../../funding-rate.md) ·
[Plan and flowcharts](implementation-plan.md) · [Tracking](tracking.md) ·
[Verification](verification.md) · [Changelog](CHANGELOG.md).

## Verified Binance contract (2026-10-14)

The modern documentation URLs returned empty responses; Binance's official
`/legacy-docs/` URLs are accessible and retain the relevant contracts:

- [Exchange information](https://developers.binance.com/legacy-docs/derivatives/usds-margined-futures/market-data/rest-api/Exchange-Information):
  `/fapi/v1/exchangeInfo`, weight 1. Use trading perpetual contract metadata,
  not symbol suffix matching. Live request-weight limit was 2400/min/IP.
- [Mark price and funding](https://developers.binance.com/legacy-docs/derivatives/usds-margined-futures/market-data/rest-api/Mark-Price):
  `/fapi/v1/premiumIndex`; weight 10 without symbol (1 with symbol).
  `lastFundingRate` is explicitly described as the **latest funding rate**;
  `nextFundingTime` and `time` are Unix milliseconds. No funding calculation is needed.
- [Funding info](https://developers.binance.com/legacy-docs/derivatives/usds-margined-futures/market-data/rest-api/Get-Funding-Rate-Info):
  `/fapi/v1/fundingInfo` lists contracts with cap/floor/interval adjustments.
  Weight 0; shares 500 requests/5min/IP with funding history. A successful empty
  list permits defaults; a failed request does not.
- [Binance funding FAQ](https://www.binance.com/en/support/faq/detail/360033525031),
  section 3: default funding interval **8h** at 00:00/08:00/16:00 UTC; section 8
  allows contract-specific adjustments. Retrieved through the Jina text proxy
  when direct access returned empty. Override using `fundingIntervalHours`.
- [Futures HTTP/rate limits](https://developers.binance.com/legacy-docs/derivatives/usds-margined-futures/general-info):
  429 requires backing off; repeated violations lead to 418 bans lasting 2m–3d.
  Futures docs do not guarantee Retry-After on every response. Honor it when
  supplied (HTTP seconds/date); otherwise use conservative cooldowns of 1m for
  429 and 3d for 418. Never repeatedly probe a known ban.

Rates are exchange-reported latest values, **not guaranteed next-payment rates**.
Rank raw rates descending across USDT/USDC; do not annualize or normalize by hours.
Return fewer than five only when fewer valid positive candidates exist, with an
explicit shortfall. All timestamps are UTC.

## Reduced public examples

Captured from public endpoints on 2026-10-14; see provider test fixtures for JSON.

- BTCUSDT/BTCUSDC: trading perpetuals quoted in USDT/USDC respectively.
- BTCUSDT latest rate `0.00003362`; BTCUSDC `0.00006146`.
- ORDIUSDC: explicit funding interval 4h; latest rate `0.00005000`.
- Example snapshot observation: `1791978947000` Unix milliseconds.

No database migration, API key, trading operation, or scheduled funding alert is
part of this feature. Existing Telegram group target-resolution prerequisites
remain unchanged; private chats are not newly supported by this feature.

## Usage and response

```bash
curl 'http://localhost:8080/api/v1/funding-rates?limit=5'
```

Telegram: `/funding`, `/funding 10`, or `/funding@YourBot 10`. The existing
authenticated Telegram adapter must be enabled. Ordinary group members can query
rates; no saved alert configuration is required. HTTP GET does not send messages.

Limits are integers 5–20 (default 5). Rows are sorted by exact raw rate descending,
then symbol ascending. Example item:

```json
{
  "symbol": "BTCUSDT",
  "base_asset": "BTC",
  "quote_asset": "USDT",
  "funding_rate": "0.00050000",
  "funding_interval_hours": 8,
  "next_funding_time": "2026-10-14T16:00:00Z",
  "observed_at": "2026-10-14T12:00:00Z"
}
```

The envelope contains `source`, `rate_source_field`, `requested_limit`,
`returned_count`, `eligible_positive_count`, `shortfall`, `generated_at`,
`observed_at`, `excluded_invalid_count`, and `data`. `observed_at` is the oldest
returned observation (null when empty); `generated_at` is the cached snapshot's
generation time. Raw rates stay decimal strings; Telegram converts to percentages
(0.0005 → +0.05%). Small positive values never display as zero.

- 200: valid results, including an empty array or genuine shortfall.
- 400: malformed, repeated, empty, or out-of-range limit.
- 429: local endpoint budget exceeded; Retry-After 1 second.
- 502: invalid/unavailable upstream data.
- 503: disabled service or Binance throttling/ban; Retry-After when applicable.
- 504: upstream deadline exceeded.

Errors include safe `error` text and a stable `code`, never raw upstream bodies.
The API has no new authentication layer: retain deployment access controls.
Route-wide limiting is process-local (2 requests/second, burst 10), not a shared
multi-instance quota. Slack does not advertise or execute this command.

## Configuration and operations

The funding section defaults even when omitted from an existing YAML config:

```yaml
funding:
  enabled: true
  base_url: https://fapi.binance.com
  request_timeout: 10s
  refresh_timeout: 15s
  snapshot_ttl: 15s
  exchange_ttl: 5m
  interval_ttl: 1m
```

The HTTP and Telegram paths use one in-process service and snapshot cache.
Cold refresh normally makes three bulk requests; warm requests make none;
snapshot-only refresh makes one. Concurrent requests share bounded refresh work;
canceling one caller does not cancel other waiters. Existing retry/backoff
configuration is reused. Refresh is lazy, not part of startup/health checks.

Snapshot expiry is capped by metadata expiry and the earliest positive candidate's
funding time. Observations older than 2 minutes or more than 5 seconds in the
future are invalid. Unknown/malformed rows are counted; entirely unusable eligible
data fails rather than pretending no positive pairs exist. Failed refreshes do
not serve expired data. Unicode Binance asset symbols are supported.

Monitor `funding refresh` / `funding refresh failed` logs, candidate/invalid counts,
duration, upstream status, cooldown, and cache-hit debug logs. These controls are
per process; deployments sharing an egress IP must account for their combined
Binance usage. This is on-demand latest-data reporting, not a streaming guarantee.

Disable/rollback: set `funding.enabled: false` and restart. API returns 503 and
the command explains it is disabled. Price alerts, health, saved configurations,
and Slack remain unchanged; no database rollback is needed.

## Validation

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
FUNDING_LIVE_TEST=1 go test ./internal/api -run '^TestFundingLive$' -count=1 -v
```

The last command is opt-in, requires public Binance access, and sends no Telegram
messages. Before rollout, verify `/funding` in an explicitly selected supported
group, confirm bot-menu registration, and compare symbol order with the API.
See [verification evidence](verification.md) for completed and skipped checks.
