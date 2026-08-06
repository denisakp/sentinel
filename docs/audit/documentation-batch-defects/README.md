# Documentation-batch defects — remediation plan

**Origin:** 54 open `bug` issues (#136–#197), all labelled `found-by-docs`, filed while writing the
documentation site (spec 060). Tracking issue: #176. Root-cause issue for the whole batch: #175.

**Confirmation pass:** 2026-08-06, eleven parallel read-only agents against `develop` @ `b9fd891`,
using a reference binary built with `go build -o sentinel .` at that commit.

## Headline result

| | |
|---|---|
| Issues examined | 54 |
| Distinct defects after expanding umbrella issues | **77** |
| Duplicates found between umbrellas | 4 |
| Net distinct defects | **73** |
| CONFIRMED | 70 |
| PARTIAL (symptom real, stated cause wrong or narrower than reality) | 3 |
| NOT_REPRODUCED | 0 |
| ALREADY_FIXED by specs 042–059 | 0 |

Nothing in the batch was a false alarm. Three issues named the wrong cause, and correcting them
changed the fix in each case (see "Corrections to the issue text" below).

Five issues are umbrellas hiding 28 defects between them: #167 (5), #170 (7), #179 (7), #181 (5),
#184 (4).

## Why the e2e suite reported success

Three independent mechanisms, none of them a one-off oversight. Full analysis in
[`I00-e2e-harness-foundation.md`](I00-e2e-harness-foundation.md).

1. **Part of the test suite never runs.** `test_unit` in `scripts/e2e.sh` runs `go test ./...`,
   which silently skips every `//go:build integration` file. Those run only in a separate workflow.
2. **The incremental coverage is scenery.** Every scenario in `tests/integration/incremental/` is a
   `t.Skip("...scaffold...")`. A `grep incremental` finds 22 hits and zero executed assertions.
3. **The one PITR test cannot see the PITR bug.** `postgres_pitr_restore_test.go` hand-writes its
   manifest via `WritePITRManifest` and exercises only the planner. It never traverses the
   backup-time manifest-writing path, which is exactly where #148 lives.

On top of that, the harness's default assertion is `assert_exit_ok` (28 uses). A command that
validates a config key, ignores it, and exits 0 is indistinguishable from success under that helper.
That is the dominant shape of this entire defect batch.

## Cross-cutting root causes

Six patterns explain most of the 73 defects. They are the reason this plan groups by cause rather
than by issue number.

| # | Pattern | Defects |
|---|---|---|
| A | **Commands bypass `config_resolver.go`** and reinvent their own config path / validation | #160, #171, #173, #179.3, #181.4 |
| B | **Success and failure are indistinguishable** — exit 0 on failure, output on stderr | #165, #168, #170.1–.4, #179.7, #181.1 |
| C | **Config plumbing stops one hop short** — parsed, validated, never threaded to a consumer | #141, #142, #143, #154, #155, #157, #172, #194, #196 |
| D | **Wired on one axis, forgotten on the other** — dump but not restore, `--all` but not single-job, list but not status | #139, #140, #156, #161, #181.1, #184.3, #189 |
| E | **The validator accepts what the runtime rejects** | #145, #183, #185, #186 |
| F | **Artifact identity is lost between dump and manifest** | #151, #164, #184.2, #191, #193 |

## Corrections to the issue text

These three would have led to a wrong or wasted fix if taken at face value.

- **#157** — the hardcoded `dryRun=false` is at `internal/cli/backup.go:617`, not at the cited
  `backup_factory.go:313`. The cited site builds `djob.Retention`, and **that field is read nowhere
  in the domain**. Fixing the cited line changes nothing.
- **#164** — "every dump adapter" is wrong for MongoDB. `mongodump` streams to disk via
  `--archive`/`--out`; only pg, mysql and mariadb buffer the dump in a `bytes.Buffer`. Mongo's real
  defect is different and is #191.
- **#195** — the claim that `repair --fix` carries the same hazard is **false**. `repair.go:437-509`
  does not call `ReconcileStaleExecutions`; it has its own lock-aware reconciliation that checks
  `ReadLock` + `EvaluateLockState` + hostname. The fix is to make `schedule start` call the logic
  repair already has, not to change repair.

Two defects were found that no issue covers:

- **#171 extra** — `swapArtifact`'s remote branch (`security_reencrypt.go:503-534`) returns
  `destObject` (the artifact key) where `RecordSecurityInfo` expects `manifestPath`. The local
  branch at line 552 is correct. Remote-only.
- **#151 extra** — the same `Job.OutName` staleness makes `res.ArtifactPath` resolve to `"unknown"`.
  **The execution history is broken, not just the manifest.**

## Duplicates between umbrella issues

Fix once, close both.

| Duplicate pair | Subject |
|---|---|
| #167.4 = #170.3 | `repair --job <unknown>` exits 0 reporting "no drift" |
| #170.4 = #168 | `retention apply` without `--job` swallows errors and exits 0 |
| #179.5 = #181.3 | `tabwriter.AlignRight` collapses the separator: `backup_executions42 rows` |
| #179.6 = #177 (secondary) | `--parallel` has no upper bound, unlike `max_concurrent_restores` |

## Execution order

The ordering is driven by observability first, then by load-bearing structural fixes, then by
independent work. **I00 and I01 come first because without them no later fix is verifiable** — the
harness cannot currently tell a fix from a no-op.

```
I00  e2e harness foundation          ── blocks everything, land the cheap half first
 │
I01  CLI honesty (exit codes/stdout) ── makes every later failure visible
 │
 ├─ I02  config-loading unification ──┐
 ├─ I03  secret redaction + Mongo TLS │  (security, land early, independent)
 ├─ I04  backup locking ──────────────┤  #163 is load-bearing for I05 and I10
 ├─ I06  artifact identity ───────────┼─→ I07  dump streaming + Mongo artifact
 ├─ I08  compression + validation order
 ├─ I09  restore planner (PITR/incremental)
 ├─ I10  restore/schedule command state
 ├─ I11  storage backends + schema drift
 ├─ I12  dead config + notification isolation
 └─ I13  staging disk budget
        │
        └─ I05  retention correctness  (after I01: #168 is what makes the rest visible)
```

| PRD | Title | Issues | Defects | Blocked by |
|---|---|---|---|---|
| [I00](I00-e2e-harness-foundation.md) | e2e harness foundation | #175 | 7 tasks | — |
| [I01](I01-cli-honesty-exit-codes.md) | CLI honesty: exit codes, stdout, usage noise | #165, #168, #170.1–.6, #179.7, #181.1, #181.5 | 10 | I00 (partial) |
| [I02](I02-config-loading-unification.md) | Config-loading unification + read-only monitor | #160, #171, #173, #179.3, #181.4 | 5 | — |
| [I03](I03-secret-redaction-mongo-tls.md) | Secret redaction + MongoDB TLS wiring | #156, #189 | 2 | — |
| [I04](I04-backup-locking.md) | Backup file locking + lock plumbing + timeouts | #142, #154, #163, #194, #195, #196, #197 | 7 | — |
| [I05](I05-retention-correctness.md) | Retention correctness | #157, #158, #159, #169, #170.7, #182 | 6 | I01 |
| [I06](I06-artifact-identity.md) | Artifact identity: output naming, manifest, history | #151, #184.2, #193 | 3 | — |
| [I07](I07-dump-streaming.md) | Dump streaming + Mongo local artifact | #164, #191 | 2 | I06 |
| [I08](I08-compression-validation-order.md) | Compression wiring + validation ordering | #183, #188 | 2 | — |
| [I09](I09-restore-planner.md) | Restore planner: PITR, incremental, dry-run, verify | #148, #149, #150, #152, #185, #186, #190 | 7 | — |
| [I10](I10-command-state-registration.md) | Restore/schedule command state + registration | #136, #137, #138, #139, #140 | 5 | — |
| [I11](I11-storage-schema-drift.md) | Storage backends + config schema drift | #144, #145, #161, #184.1, #184.3, #184.4 | 6 | — |
| [I12](I12-dead-config-notification.md) | Dead config keys + notification isolation | #141, #143, #146, #155, #172, #187 | 6 | — |
| [I13](I13-staging-disk-budget.md) | Concurrent staging disk budget | #177, #179.6 | 2 | — |
| [I14](I14-monitor-query-surface.md) | Monitor query surface + reporting polish | #153, #166, #167.1–.3, #167.5, #179.1, #179.2, #179.4, #181.2, #181.3 | 11 | I01, I02 |

**Coverage check.** All 54 open `bug` issues appear in exactly one PRD, verified mechanically. The
five umbrella issues are split across PRDs by sub-defect, so #167, #170, #179, #181 and #184 each
appear in more than one row above — every individual sub-defect still has exactly one owner. #175
and #176 are the e2e root cause and the tracking issue, both owned by I00.

## Standing rules for every PRD in this directory

Each of these is a required task in the PRD's own definition of done, not a nice-to-have.

1. **The e2e assertion ships with the fix.** Every PRD names the assertion that would have caught
   its defect, and that assertion lands in the same PR. Deferring the coverage to #175 is how this
   batch happened.
2. **The documentation site is updated in the same PR.** Every affected page carries a
   `:::caution` / `:::warning` naming the issue number. `grep -rl "<issue>" website/docs/` finds
   every page that has to change. Per PRD, the affected pages are listed explicitly.
3. **Runbook maintenance is a Polish-phase task**, per the standing process rule. `docs/runbooks/`
   is the preserved corpus; where a runbook and the code disagree after the fix, the runbook is
   updated, not the site.
4. **Decisions marked "DECISION REQUIRED" are not the implementer's to make.** They are product or
   security calls: implement-vs-remove, or a behaviour change with a blast radius on existing
   configs. Raise them, do not default them.

## Live reproduction status

Every verdict in this plan was settled **statically or against the reference binary, without
Docker**. The following need live infrastructure to prove the *fix*, not the defect:

#149, #154, #155, #163, #164, #169 (gdrive half), #177, #182, #185, #189 (TLS half), #190, #194,
#195, #196, #197, #171 (remote half).

Parallel agents cannot share the Docker infra naively: fixed ports, shared DB state, a common
`lock_dir` and colliding output directories. Live verification must either be serialised or
namespaced per agent (distinct job names, databases and output paths).
