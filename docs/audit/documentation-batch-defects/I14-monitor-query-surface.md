# PRD I14 — Monitor query surface + reporting polish

**Severity:** Low to Medium (wrong query results presented as correct; several help-vs-behaviour lies)
**Status:** Open
**Issues:** #153, #166, #167.1, #167.2, #167.3, #167.5, #179.1, #179.2, #179.4, #181.2, #181.3 (= #179.5)
**Area:** `internal/cli/{monitor,monitor_doctor,repair}.go`, `internal/adapters/monitor/{export,init}.go`,
`internal/config/restore_types.go`
**Blocked by:** I01 (stdout routing must land before touching the CSV/limit logic — adjacent edits in
the same file), I02 (the read-only monitor open)

## Problem

The monitor surface answers questions wrongly and confidently. Time filters are silently dropped,
limits are ignored, and the doctor hides the error text that explains what it found.

Most of these are small. They are grouped because they share two files and because several would
conflict if fixed separately.

## Confirmed defects

| # | Defect | Cause | Evidence |
|---|---|---|---|
| #166 | `monitor stats --last 12h` silently means all time | `monitor.go:~278` `parseLastDays` does `int(duration.Hours()/24)` — 12h truncates to 0; `monitorStatsCmd` only bounds when `days > 0` | rows seeded at now and now-40d; `stats --last 12h` → "Period: all time / Executions: 2". `list --last 12h` correctly returned only the recent row |
| #167.2 | `list --format csv` ignores `--limit` and `--offset` | `adapters/monitor/export.go:17` hardcodes `ListExecutions(ctx, filter, 100000, 0)`; `printCSV` discards the limit it already read | `list --format csv --limit 1 --last 60d` returned both rows |
| #153 | `monitor stats` documents `--job` as optional and rejects the command without it | `monitor.go:205` help says "optional; omit for all jobs"; `:76-79` returns `--job is required`. `loadStatistics`'s all-jobs branch (`GetAggregateStatistics`, ~219) is **dead code, unreachable through the CLI** | ran it: `Error: --job is required`, exit 1 |
| #167.5 | `list` and `export` disagree on defaults and on unknown formats | `monitor.go:196` `list` defaults `--last` to `7d`; `:210` `export` defaults to `""` (whole history). `list`'s format switch falls through to table silently; `export`'s `normalizeFormat` errors | `list --format bogus` → exit 0, table. `export --format bogus` → exit 1. `export` without `--last` returned the 40d-old row, `list` did not |
| #167.3 | `monitor show <id>` takes a positional the help does not document | `monitor.go:117-120` reads `args[0]`; `Use: "show"` and `--help` omit it | `monitor show exec-1` worked; `show --help` lists no positional |
| #167.1 | `repair --fix` help claims it marks broken chains | `repair.go:1128` flag text; `detectBrokenChains` (355-431) always sets `Action = "manual action required"`, never gated on `mode.doRecoverable()` | source: no branch marks `chain_broken` applied under `--fix` |
| #179.4 | `monitor doctor` hides the error and hint in table output | `monitor_doctor.go` `renderDoctorTable` (111-142) never prints `r.Error`, and prints `r.Hint` only for `ForwardIncompatible` — never for `StalePending`, which is exactly what a failed repair leaves behind | forced a real migration failure (`duplicate column name: hash_algorithm`); the table showed status/pending/tables and no error text. `--json` on the same run carried both fields |
| #181.3 (=#179.5) | `monitor doctor` output has no separator between name and count | `monitor_doctor.go:134-138` `tabwriter.NewWriter(w,0,0,2,' ',tabwriter.AlignRight)` — `AlignRight` left-pads narrower cells, so the widest name in the block gets zero separator | reproduced: `backup_executions42 rows`, `schema_migrations5 rows` |
| #181.2 | An empty integrity sweep records nothing, so "ran and found nothing" is indistinguishable from "never ran" | `adapters/monitor/recorder.go:141-143` `RecordIntegrityCheck` is an explicit, **tested** no-op when `len(run.Results) == 0` (`TestRecordIntegrityCheck_EmptyRunNoOp`). Default `notify_on: failure` also stays silent (`integrity_scheduled.go:104-107`) | source; the doc comment states the behaviour |
| #179.1 | `schema_migrations.checksum` is declared and never written | `adapters/monitor/init.go:519` `INSERT INTO schema_migrations (version, name)` omits it; `schema.go:114` declares the column | `sqlite3 history.db "SELECT checksum FROM schema_migrations"` → all NULL after 5 real migrations |
| #179.2 | A restore job cannot be on-demand-only | `config/restore_types.go:265-267` — `enabled && job.Schedule == ""` is a hard error, no opt-out | config with `enabled: true`, no schedule, valid `backup_source` → "restore schedule (cron) is required" |

## Root cause

Two shared, plus a tail of independents.

**(a)** Pattern **B**, already covered in I01: `monitor`, `repair`, `security` and `storage` use
`cmd.Print*` and `return nil` inconsistently. #167.5's silent format fallback is the same habit.

**(b)** Pattern **C**: a value is read from the flags and then not threaded to the query — `--last`
(#166), `--limit`/`--offset` (#167.2). The plumbing stops one hop short of the SQL.

**(c)** Independents: #179.1, #179.2, #181.2, #167.1, #167.3.

## DECISION REQUIRED

- **#153 direction.** `GetAggregateStatistics` already exists and is unreachable. Either wire it up
  (drop the required-check, honour the documented "omit for all jobs") or fix the help string to say
  the flag is required. Wiring it is more useful and the code is already written.
- **#181.2 design.** How should "ran, found nothing" be marked? A zero-result run marker row, or a
  summary surfaced through `monitor doctor`. This changes the `IntegrityRun` shape and the existing
  test asserts the current no-op, so the test changes with it. Small decision, needs an answer
  before implementation.
- **#179.2** — allow an empty schedule for jobs never registered with the scheduler, or add an
  explicit `on_demand: true`. The explicit flag is clearer and does not weaken validation for
  scheduled jobs.

## Fix order

1. **#181.3** and **#167.1** — one-liners (`tabwriter` flag; help text). Do first, zero risk.
2. **#179.4** — print `r.Error` always, and `r.Hint` for `StalePending` too.
3. **#166** and **#167.2** — thread the time filter and the limit/offset into the queries. Both in
   `monitor.go`; land after I01's stdout work to avoid conflicting edits.
4. **#167.3**, **#167.5** — `Use: "show [id]"`, align the `list`/`export` defaults, make `list`
   reject unknown formats as `export` does.
5. **#153**, **#181.2**, **#179.2** — after the decisions above.
6. **#179.1** — compute and store a per-migration checksum, or drop the unused column.

## Definition of done

- **Code:** the items above.
- **e2e:**
  - `stats --last 12h` with a row older than 12h present — that row is excluded.
  - `list --format csv --limit 1` with 2 matching rows — returns 1.
  - `monitor stats` without `--job` and several jobs present — returns an aggregate, or errors with
    help text that says the flag is required. Assert whichever the decision picks.
  - `monitor doctor` against a DB with ≥2 tables of different name lengths — every row matches
    `\w+\s+\d+ rows` (the separator is present).
  - `monitor doctor --repair` against a DB that fails migration — the **table** output contains the
    error string, not just `--json`.
  - A sweep filter matching zero backups — a queryable run record exists afterwards.
  - `list --format bogus` — rejected, matching `export`.
- **Docs:** `guides/inspect-monitor-history.md`, `guides/monitoring-history.md`,
  `reference/cli/monitor.md`, `guides/first-backup.md`, `guides/restore.md` (all cite #153 or #166).
  #167, #179 and #181 sub-defects are largely unindexed — add pointers where a page describes the
  broken behaviour as working.
