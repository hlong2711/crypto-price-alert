# Changelog

## 2026-10-14

- FR-00: Established a green test/vet/build baseline and created feature records.
- FR-01: Verified public endpoint schemas live and funding semantics through
  official legacy documentation and the Binance funding FAQ. Confirmed the 8h
  default with per-symbol overrides; recorded reduced public examples.
- FR-02: Added exact decimal parsing/ordering, nonzero percentage formatting,
  funding-only DTOs, bounded symbols, limit validation, and freshness validation.
  `go test ./internal/domain` passed; candle intervals are unchanged.
- FR-03–FR-06: Added separate public futures transport, bounded retries/body size,
  shared 418/429 cooldown, eligibility/interval/snapshot parsing, and reduced live
  fixtures. `go test ./internal/market ./internal/domain` passed. Spot provider
  and its existing interface are unchanged.
- FR-07–FR-08: Added shared ranking, exact tie ordering, shortfall/invalid counts,
  bulk metadata/snapshot caching, independent bounded refreshes, concurrent waiter
  coalescing, settlement-boundary expiry, and shutdown cancellation. Service tests
  and race tests across domain/market/service passed.
- FR-09: Added backward-compatible funding YAML defaults and independent disable,
  positive-duration validation, and credential-free HTTP(S) origin validation.
  `go test ./internal/config` passed. No environment secrets changed.
- FR-10: Added GET funding route with strict limit validation, safe typed error
  mapping, exact-string JSON, and process-wide route-only throttling (2/s, burst
  10). `go test ./internal/api` passed; health/alert routes are unaffected.
- FR-11: Added read-only funding command parsing, Telegram-only menu definition,
  and provider-aware help. `go test ./internal/chat/...` passed, including existing
  Slack behavior and updated Telegram command-registration checks.
- FR-12: Added optional funding-reader injection, read-only Telegram dispatch,
  usage/disabled/temporary-error replies, UTC/percentage formatting, and bounded
  20-row messages. `go test ./internal/chat/...` passed. No alert configuration
  reads or mutations are needed for funding dispatch.
- FR-13: Wired one lazy funding service into HTTP and Telegram; disabled mode
  passes a genuinely nil reader and shutdown cancels shared work. Full tests and
  build passed. Startup test confirms no Binance requests during initialization.
- FR-14: Added authenticated Telegram adapter integration tests using real funding
  and chat services with fake upstream/Telegram transports. Verified non-creator
  access, bot suffix, invalid arguments, safe upstream errors, deduplication,
  invalid-secret rejection, and API/Telegram snapshot parity. Telegram tests passed.
- FR-15: Full test/race/vet/build/diff checks passed. Initial live API test exposed
  valid Unicode symbols rejected by ASCII validation; fixed this and Telegram's
  message-length accounting with regression tests. Live API rerun passed (top five
  from 467 positive pairs, zero invalid exclusions). Added user/config/rollback
  documentation and opt-in live smoke test. Real Telegram delivery remains a
  documented deployment check because no explicit test group was supplied.
