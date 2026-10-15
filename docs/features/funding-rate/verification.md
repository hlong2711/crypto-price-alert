# Verification evidence

## Baseline (2026-10-14, FR-00)

- `git status --short`: clean before implementation.
- `go test ./...`: passed.
- `go vet ./...`: passed.
- `go build ./...`: passed.

## Upstream verification (FR-01)

Public read-only requests to exchangeInfo, fundingInfo, and bulk premiumIndex
returned HTTP 200 without credentials. Reduced fixtures preserve relevant fields
only; they are examples, not assertions about current market prices.
See README for authoritative semantics and rate-limit references.

No Telegram messages have been sent during verification.

## Step-level checks

- FR-02: `go test ./internal/domain` passed.
- FR-03–FR-06: `go test ./internal/market ./internal/domain` passed.
- FR-07: `go test ./internal/service -count=1` passed.
- FR-08: `go test -race ./internal/service ./internal/market ./internal/domain` passed.
- FR-09: `go test ./internal/config` passed.
- FR-10: `go test ./internal/api` passed.
- FR-11–FR-12: `go test ./internal/chat/...` passed.
- FR-13: `go test ./...` and `go build ./...` passed.
- FR-14: `go test ./internal/chat/telegram -count=1` passed, including
  API/Telegram ranking parity from one shared snapshot.

## Live smoke test (FR-15)

`FUNDING_LIVE_TEST=1 go test ./internal/api -run '^TestFundingLive$' -count=1 -v`

The first run failed with HTTP 502. Investigation found valid Unicode Binance
symbols (`币安人生USDT`, `我踏马来了USDT`, etc.) rejected by the initial
ASCII-only validation. Corrected validation to bounded Unicode letters/numbers,
added a live-derived Unicode fixture, and tested Unicode Telegram message size
in UTF-16 units instead of UTF-8 bytes.

The rerun **passed**, returning five rows from 467 eligible positive pairs with
zero invalid exclusions. Observed ordered rates:

| Symbol | Raw rate | Interval | Next payment (UTC) |
| --- | --- | --- | --- |
| SPORTFUNUSDT | 0.00029020 | 4h | 2026-10-14 16:00 |
| TRUTHUSDT | 0.00027395 | 4h | 2026-10-14 16:00 |
| JELLYJELLYUSDT | 0.00024003 | 4h | 2026-10-14 16:00 |
| ARXUSDT | 0.00020045 | 4h | 2026-10-14 16:00 |
| GRVTUSDT | 0.00015642 | 4h | 2026-10-14 16:00 |

These are historical test observations, not current trading recommendations.

Real Telegram delivery was **not run**: no explicit test group was supplied.
Authenticated adapter behavior and outgoing message payloads were verified with
fake Telegram transport; no secrets or live chat destinations were used.

## Final automated checks (after Unicode correction)

- `gofmt -w` on all touched Go files: completed.
- `go test ./... -count=1`: passed, all packages.
- `go test -race ./... -count=1`: passed, all packages.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `git diff --check`: passed.

The live smoke test is skipped by default in the normal test suite. It was run
explicitly and passed as recorded above. No database, Telegram credentials, or
live alert scheduler was needed for these checks.
