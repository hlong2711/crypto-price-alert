# Funding rate implementation tracking

All changes are pending commit unless otherwise noted. See [verification](verification.md).

| Task | Description | Status | Changed files | Verification evidence | Commit/PR | Blocker |
| --- | --- | --- | --- | --- | --- | --- |
| FR-00 | Baseline and records | done | Feature docs | Baseline test/vet/build passed | pending | — |
| FR-01 | Verify upstream contract | done | Feature README; market testdata | Official references and reduced live fixtures | pending | — |
| FR-02 | Domain and exact decimals | done | `internal/domain/funding*` | Domain tests passed, including Unicode regression | pending | — |
| FR-03 | Futures transport | done | `internal/market/binance_futures*`, `funding.go` | Retries, cooldown, deadlines, body bounds tested | pending | — |
| FR-04 | Contract eligibility | done | Futures provider/tests; testdata | Metadata filters/validation tested | pending | — |
| FR-05 | Funding intervals | done | Futures provider/tests; testdata | Overrides, empty success, invalid responses tested | pending | — |
| FR-06 | Bulk rate snapshot | done | Futures provider/tests; testdata | Bulk schema, exact values, timestamps tested | pending | — |
| FR-07 | Ranking service | done | `internal/service/funding*` | Ordering, ties, scarcity, corruption tested | pending | — |
| FR-08 | Cache and freshness | done | Funding service/tests | Concurrent coalescing, cancellation, expiry, shutdown; race checks passed | pending | — |
| FR-09 | Configuration | done | Config implementation/tests; YAML | Defaults, explicit disable, invalid settings tested | pending | — |
| FR-10 | HTTP endpoint | done | `internal/api/funding_handler*` | Validation, statuses, serialization, scoped throttle tested | pending | — |
| FR-11 | Command definitions | done | Chat command/parser/service; Telegram menu/tests | Telegram menu/help updated; Slack unchanged | pending | — |
| FR-12 | Telegram dispatch/format | done | Chat funding/service/tests | Read-only non-creator lookup, tiny rates, Unicode message bound tested | pending | — |
| FR-13 | Startup wiring | done | `cmd/server/main*` | Full build/tests; lazy startup and disable tested | pending | — |
| FR-14 | Telegram integration tests | done | `internal/chat/telegram/funding_integration_test.go` | Real service, non-creator, dedupe, secret rejection, API parity | pending | — |
| FR-15 | Final verification | done | Live smoke test; README/spec links; feature records | Full tests/race/vet/build/diff checks passed; live API passed | pending | Real Telegram smoke deferred until test group supplied |

No commits were created automatically during execution. Real Telegram delivery is
a remaining deployment check, not a claimed successful live test.
