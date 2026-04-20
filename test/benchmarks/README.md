# Loop detector benchmarks

14-task end-to-end benchmark used to validate agent-loop reliability work
(loop detector gate, force-stop synthesis, empty-result heuristic).

## Scripts

- `driver.sh` — 8 coding scenarios (grep, batch edit, audit analysis, codebase
  comparison, bulk test generation, tracing, tool selection, TODO scan).
  Modifying tasks (2, 5) run in throwaway worktrees under `/tmp/maxiter_tests/`.
- `driver_tob.sh` — 6 toB daily-task scenarios (calendar, inbox, drive, notion).
  Read-only / draft-free — no side effects on the user's accounts.
- `analyze.py` — post-run parser: reads a session JSON + audit log and emits
  per-task metrics (LLM calls, tool distribution, consecutive streaks, failures,
  max-iter synthesis detection, cost).

## Running

Requires the `shan` binary on `$PATH` and a configured Shannon Cloud endpoint.
Tasks that call Calendar / Gmail / Drive / Notion need MCP servers configured.

```bash
# From anywhere — scripts resolve repo root via BASH_SOURCE
./test/benchmarks/driver.sh         # coding scenarios
./test/benchmarks/driver_tob.sh     # toB scenarios

# Override where results land
BENCHMARK_RESULTS_DIR=/path/to/out ./test/benchmarks/driver.sh

# Analyze one task
./test/benchmarks/analyze.py <session_id> <task_num> <task_name>
```

Per-task artifacts (stdout, session_id, driver.log) land under
`$BENCHMARK_RESULTS_DIR` (defaults to `/tmp/maxiter_tests/results` and
`/tmp/maxiter_tests/results_tob`).

## Expected behavior after Phase 1 gate

The loop detector previously force-stopped Task 5 (coding, ~14 bash calls) and
Task 6 (toB, ~10 MCP calls) despite both being legitimate batch operations with
unique arguments. After the `batchTolerant` uniqueness gate lands:

- Task 5: completes all 19 tool calls without force-stop.
- Task 6: completes all 16 database queries; row counts returned.
- Tasks that don't hit the detector (1, 3, 4, 7, 8 / 1, 2, 3, 4, 5) must remain
  within ±1 tool call of their pre-fix trajectory — uniqueness gate must not
  accidentally relax generic `think` / `http` / `file_*` spin detection.
