---
name: runtime-simulation-debugger
description: Perform step-by-step runtime execution simulation and static code analysis to diagnose broken workflows, race conditions, edge-case failures, or silent drops.
---

# Runtime Simulation & Execution Debugger Skill

Use this skill when investigating a runtime bug, silent execution failure, race condition, data pipeline break, or unexpected state issue in a codebase.

## Diagnostic Workflow

Follow these steps sequentially to diagnose the issue with empirical evidence:

### 1. Entry Point Identification & Mapping
- Identify the exact entry point or trigger (e.g., cron tick, API endpoint, event handler, queue worker).
- Inspect all registration sites, configurations, and schedules associated with the trigger.
- Note any potential concurrent triggers or overlapping schedules.

### 2. End-to-End Code Tracing
- Trace the execution path sequentially through all architectural layers:
  - Controller / Handler / Entry Point
  - Concurrency Locks & Mutexes
  - Business Logic & Filtering Rules
  - Data Providers & External API Calls
  - Persistence & Notification Delivery
- Inspect actual source files using viewing tools before making any assumptions about types, signatures, or mutex scopes.

### 3. Step-by-Step Execution Simulation
- Perform a detailed step-by-step execution simulation matching the user's specific parameters (time, input values, timezone, flags, concurrency conditions).
- Test edge cases at boundaries (e.g., simultaneous cron firings, zero limits, lock contentions, null responses).

### 4. Root Cause Analysis
- Pinpoint the exact line numbers and logic conditions responsible for the break, silent skip, or unhandled exception.
- Determine if the failure is caused by a race condition, lock contention, schema mismatch, or logical flaw.

---

## Output Requirements

Format the final diagnostic response using the following structured report template:

### 1. Workflow Simulation Trace
Walk through each stage of execution with exact file references and line ranges formatted as markdown file links (`[file.ext:L10-L20](file:///path/to/file#L10-L20)`).

### 2. Failure Point & Root Cause
Explain clearly why and where the workflow fails or drops, detailing the exact conditions under which the failure occurs.

### 3. Execution Status Summary Table
Provide a summary table visualizing each stage in the pipeline:

| Stage / Component | File & Lines | Status | Observation |
| --- | --- | --- | --- |
| Entry Point / Trigger | `path/to/file.go#L10` | 🟢 Success | Triggered as expected |
| Concurrency / Locking | `path/to/file.go#L25` | 🔴 Broken | Lock contention causes silent drop |
| Data Retrieval | `path/to/file.go#L60` | 🟡 Skipped | Not reached due to upstream drop |

### 4. Recommended Fix & Action Plan
Provide clear, actionable recommendations or code diff snippets to resolve the root cause without side effects.
