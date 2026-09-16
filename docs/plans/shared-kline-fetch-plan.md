# Shared Kline Fetch — Serve Multiple Alert-Targets With One API Call Per Symbol

## Status

Proposed — awaiting review. Do not implement until approved.

## Objective

In a fresh scheduler round, fetch each unique symbol from Binance **once** and share the
result across all alert-targets (Slack/Telegram channels) that configure it. This fixes
Binance rate-limit pressure as target count grows.

Approved decisions:

1. Remove the legacy run from `TargetExecutor` (delete the temporary dual-run).
2. Scope strictly to `TargetExecutor` scheduler path; leave `AlertService` manual-run path as-is.
3. Fetch unique symbols **sequentially** (no concurrency); determinism and simplicity over latency.

## Current Behavior (verified)

- `internal/scheduler/target_executor.go:87-101` loops targets sequentially;
  `executeTarget():111-128` loops `config.Symbols` and calls `e.market.GetKline()` per
  target per symbol. No cross-target dedupe.
- Job uniqueness is per-target: unique key
  `(target_id, symbol, interval, period_start)` — `internal/database/model.go:12-15`,
  `internal/repository/postgres.go:67-71`. Job carries `TargetID`
  (`internal/domain/types.go:73-81`, `target_executor.go:112-120`).
- Consequence: 3 targets x same symbol = 3 job rows = 3 Binance
  `GET /api/v3/klines?limit=1` calls on a fresh round, up to 9 with
  `retry.max_attempts: 3` (`internal/market/binance.go:64-80`,
  `configs/config.yaml:50-52`, wiring `cmd/server/main.go:63-69`).
- Extra multiplier: temporary legacy dual-run at `target_executor.go:74-80` fires
  `legacy.Execute()` in a goroutine alongside the per-target run.
- Same 1-call-per-symbol pattern exists in `internal/scheduler/executor.go:73` (legacy)
  and `internal/service/alert.go:97` (manual/API trigger); out of scope for this plan.

## Proposed Design

Restructure `TargetExecutor.Execute(ctx, now, interval)` into phases for a single
`(interval, period)` round:

```text
Phase 0: period + eligible targets+configs (unchanged: TryLock, ListEnabledTargets,
         GetCurrentPeriod, enabled + interval filter)
Phase 1: create/lookup per-target job rows first; collect neededSymbols = union of
         symbols where at least one job is new or needs retry (not already sent)
Phase 2: fetch each needed symbol ONCE, sequentially, in sorted order ->
         shared map[symbol]fetchOutcome (PriceResult or Unavailable; CalculateChange
         also runs once per symbol)
Phase 3: per target (sequential, current order): build results from shared map,
         BuildMessage, SendToTarget, MarkSent/MarkFailed per job row
```

Semantics preserved:

- Fetch failure for a symbol -> all targets sharing it get `Unavailable` for it,
  exactly as if each had fetched and failed.
- Fully-sent duplicate tick stays cheap: Phase 1 finds zero needed symbols (all jobs
  already `sent`) -> skip Phase 2 (0 Binance calls), send nothing. Matches
  `target_executor_test.go:48-54`.
- Per-target job rows untouched (audit/idempotency); per-target message content
  unchanged (different symbol subsets still render differently); send order sequential.

Concurrency/rate note:

- One goroutine-free sequential loop over unique symbols; existing
  `BinanceProvider` retry/backoff/semaphore path reused unchanged
  (`concurrency.market_requests: 5`).
- No in-memory cache across rounds: kline windows differ per period, so sharing is
  scoped to one `Execute()` call only. A TTL cache across 1h/4h overlaps is
  explicitly out of scope (staleness risk).

## File-Level Changes (not yet executed)

1. `internal/scheduler/target_executor.go`
   - Split `Execute` into `collectEligible()`, `fetchUniqueSymbols()`,
     `deliverToTarget()` helpers (or inline phases).
   - Replace `executeTarget()` per-symbol fetch with shared-map lookup; keep its job
     create/skip/send/mark logic.
   - Remove legacy run: delete the `else { go func() { legacy.Execute() }() }`
     dual-run block (`:74-80`).
   - Open assumption: also delete the `if len(targets)==0 { return legacy.Execute() }`
     fallback so `TargetExecutor` never calls legacy (zero targets = no-op). If the
     legacy global channel is still needed, keep the zero-target fallback only —
     confirm during review. Follow-up cleanup (removing the `legacy` field/ctor
     param) deferred.
2. `internal/scheduler/target_executor_test.go` (+ maybe `executor_test.go`)
   - Counting `fakeMarket` (`mu + map[string]int`).
   - New tests: 3 targets x same `BTCUSDT` -> 1 `GetKline`, 3 deliveries, 3 sent jobs;
     mixed symbols (`A:{BTC,ETH}`, `B:{ETH,SOL}`) -> unique-count calls with correct
     per-target messages; one symbol failing -> all sharers `Unavailable` but round
     still delivers; duplicate tick -> 0 new calls, 0 new deliveries; fully-sent
     target skipped while fresh target still triggers fetch.
3. Deferred / out of scope: legacy `Executor` path (single symbol list, already
   1/symbol); `AlertService.Run/RunForTarget` batch sharing; cross-round TTL cache;
   new config flags.

## Verification

```text
go test ./internal/scheduler/... -count=1 -v
go test ./... -count=1
```

Expected effect: N targets x M shared symbols on a fresh round -> M Binance calls
(vs N x M today).

## Risks / Notes

- Phase 1 still does `targets x symbols` DB `CreateIfNotExists` writes per round —
  cheap relative to Binance rate limit. Fetch-then-create alternative rejected
  (would change audit semantics).
- Per-target `GetAlertConfig` load errors keep current behavior: skip that target,
  continue others, return `firstErr`.
- `mu.TryLock` duplicate-tick guard unchanged; sequential shared fetch holds the lock
  for `U x p95` (e.g. ~20 symbols x ~300ms ~= 6s). Acceptable unless `U` grows
  large; parallelize later without changing the sharing design if needed.
- Observability: log `interval, period_start, targets, unique_symbols` per round;
  no schema/config changes.

## Review Checklist

- [ ] Confirm legacy removal scope (dual-run block only vs. zero-target fallback too).
- [ ] Confirm sequential-fetch latency trade-off acceptable.
- [ ] Approve implementation of `target_executor.go` + tests as scoped above.
