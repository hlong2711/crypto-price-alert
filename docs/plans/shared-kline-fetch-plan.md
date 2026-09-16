# Shared Kline Fetch — Serve Multiple Alert-Targets With One API Call Per Symbol

## Status

Implemented — all steps executed and verified (`go test ./...`, `go vet ./...`, `go build ./...` green).

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

## Implementation Steps (ordered todo list — follow top-down to avoid conflicts)

> Rule: finish + verify each step before starting the next. Steps are scoped to
> non-overlapping code regions except where noted; Step 3 is the single behavior
> change and must be applied atomically.

- [x] Step 0 — Baseline (no code change)
  - Files: none.
  - Run: `go test ./internal/scheduler/... -count=1 -v` and `go build ./...`; record green baseline.

- [x] Step 1 — REMOVE legacy run from `TargetExecutor.Execute` (isolated delete)
  - File MODIFY: `internal/scheduler/target_executor.go:68-80`.
  - REMOVE: the `if len(targets) == 0 { ... legacy.Execute ... } else { // TODO: REMOVE ... go func() { legacy.Execute }() }` block.
  - ADD: `if len(targets) == 0 { return nil }` (zero targets = no-op).
  - Do NOT touch: struct field `legacy`, ctor `NewTargetExecutor` signature, `cmd/server/main.go` call sites (keeps this step conflict-free; field cleanup is deferred).
  - Done when: `grep -n "legacy.Execute" internal/scheduler/target_executor.go` returns nothing; `go build ./...` passes.

- [x] Step 2 — ADD shared-fetch scaffolding (purely additive, no behavior change)
  - File MODIFY (append-only): `internal/scheduler/target_executor.go` (bottom, near `containsInterval`).
  - ADD types only, nothing calls them yet:
    - `eligibleTarget{target domain.AlertTarget, config domain.AlertConfig}`
    - `pendingJob{job domain.Job, symbol string}`
    - `fetchOutcome{result notification.PriceResult}` (Unavailable encoded as today via `notification.PriceResult{Change: ..., Unavailable: true}`).
  - Done when: `go build ./...` + `go vet ./internal/scheduler/` pass; `Execute` behavior byte-identical.

- [x] Step 3 — MODIFY `Execute` to collect → fetch-once → deliver (the behavior change; atomic)
  - File MODIFY: `internal/scheduler/target_executor.go:59-103` (`Execute`) and `:105-175` (`executeTarget`).
  - MODIFY `Execute`: keep `mu.TryLock`, `ListEnabledTargets`, `GetCurrentPeriod` as-is; REPLACE the per-target `GetAlertConfig` + `executeTarget` loop with:
    1. collect `eligible` list (same skip rules: config-load error -> record `firstErr`, continue; `!Enabled`/interval mismatch -> skip);
    2. Phase 1 job pre-pass: per eligible x per `config.Symbols` call `CreateIfNotExists`; on error record `firstErr` and drop that target; `!isNew && sent` -> exclude from send set; else add symbol to `neededSet` + stash job handle per target. If `neededSet` empty -> return `firstErr` (0 Binance calls);
    3. Phase 2 shared fetch: `sort` `neededSet`, then sequential `GetKline` + `CalculateChange` once per symbol into `outcomes` map (fetch/validation error -> shared `Unavailable` outcome; same `service.CalculateChange` call as today);
    4. Phase 3 deliver: per eligible target in original order, assemble items from stashed unsent jobs + `outcomes[symbol]`, skip if empty, then existing `BuildMessage` -> `SendToTarget` per notifier -> `MarkSent`/`MarkFailed` block unchanged.
  - MODIFY `executeTarget`: change signature to accept the shared map (e.g. `executeTargetWithShared(ctx, target, config, period, jobs []domain.Job, outcomes map[string]fetchOutcome)`) and REMOVE its internal `GetKline`/`CalculateChange` calls, replacing them with map lookups. Alternatively inline it into `Execute`; either way the old per-symbol fetch loop (`:128-144`) is REMOVED.
  - Do NOT touch in this step: test files, `Executor`, `AlertService`, provider, repository.
  - Done when: `go test ./internal/scheduler/... -count=1 -v` passes (old tests; new sharing tests come in Step 4).

- [x] Step 4 — MODIFY test harness + ADD sharing tests (test-only step)
  - File MODIFY: `internal/scheduler/target_executor_test.go:115-119` (`fakeMarket`).
  - MODIFY `fakeMarket`: ADD `mu sync.Mutex` + `calls map[string]int` (+ `fail map[string]error` for the error case); `GetKline` records `calls[symbol]++` and returns injected error when set. Existing tests keep passing (they ignore counts; value receiver -> switch to pointer receiver + update `NewTargetExecutor(..., &fakeMarket{...} or newCountingMarket())` call sites `:33,63` accordingly in the same edit to avoid a half-broken state).
  - ADD tests (same file, no changes to production code):
    1. 3 targets x same `BTCUSDT` -> `calls["BTCUSDT"] == 1`, 3 deliveries, 3 sent jobs;
    2. mixed `A:{BTC,ETH}`, `B:{ETH,SOL}` -> 3 total calls, correct per-target messages;
    3. failing symbol -> all sharers `Unavailable`, round still delivers;
    4. duplicate tick -> 0 new calls, 0 new deliveries;
    5. fully-sent target skipped while fresh target still triggers fetch.
  - Done when: `go test ./internal/scheduler/... -count=1 -v` passes including the 5 new tests.

- [x] Step 5 — Full verification (no code change)
  - Run: `go test ./... -count=1`, `go vet ./...`, `go build ./...`.
  - Done when: all green; expected effect spot-checked via Step 4 counters (N targets x M shared symbols -> M Binance calls).

## Deferred / Explicitly Out Of Scope

- Removing the now-unused `legacy *Executor` field + ctor param + `cmd/server/main.go` wiring (deferred cleanup; Step 1 keeps them to avoid cross-file conflicts).
- Legacy `Executor` path, `AlertService.Run/RunForTarget` sharing, cross-round TTL cache, new config flags.

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
