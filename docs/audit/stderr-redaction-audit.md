# Stderr Redaction Audit — Dump Adapters

**Feature**: [012-pg-dump-stderr-redaction](../../specs/012-pg-dump-stderr-redaction/)
**Date**: 2026-05-19
**Status**: Closed — all callsites route stderr through `sanitize.RedactStderr` before embedding it in returned errors.

This artifact satisfies FR-007 of the spec: a one-time enumeration of every
dump-adapter error path that previously embedded raw subprocess stderr in a
returned error. Each callsite is now wrapped with the helper introduced in
`internal/sanitize/sanitize.go` (`RedactStderr` + `RedactStats`), backed by
the extended `credentialPatterns` set (P1–P7).

## Reviewed callsites

| # | File | Line | Engine | Status | Covering test |
|---|------|------|--------|--------|---------------|
| 1 | `pkg/backup/pg_dump/pg_dump.go` | 56 | `pg_dump` | redacted | `pkg/backup/pg_dump/pg_dump_test.go::TestErrorRedaction` |
| 2 | `pkg/backup/pg_dump/pg_dump_all.go` | 58 | `pg_dumpall` | redacted | `pkg/backup/pg_dump/pg_dump_all_test.go::TestErrorRedaction_PgDumpAll` |
| 3 | `pkg/backup/mysql_dump/mysql_dump.go` | 40 | `mysqldump` | redacted | `pkg/backup/mysql_dump/mysql_dump_test.go::TestErrorRedaction` |
| 4 | `pkg/backup/mysql_dump/mysql_dump_all.go` | 54 | `mysqldump --all-databases` | redacted | `pkg/backup/mysql_dump/mysql_dump_all_test.go::TestErrorRedaction_MysqlDumpAll` |
| 5 | `pkg/backup/mariadb_dump/mariadb_dump.go` | 37 | `mariadb-dump` | redacted | `pkg/backup/mariadb_dump/mariadb_dump_test.go::TestErrorRedaction` |
| 6 | `pkg/backup/mariadb_dump/mariadb_dump_all.go` | 51 | `mariadb-dump --all-databases` | redacted | `pkg/backup/mariadb_dump/mariadb_dump_all_test.go::TestErrorRedaction_MariaDBDumpAll` |
| 7 | `pkg/backup/mongo_dump/mongo_dump.go` | 50–54 | `mongodump` | redacted (both branches) | `pkg/backup/mongo_dump/mongo_dump_test.go::TestErrorRedaction` |
| 8 | `pkg/backup/mongo_dump/oplog.go` | 70–74 | `mongodump --oplog` | redacted | `pkg/backup/mongo_dump/oplog_test.go::TestErrorRedaction_Oplog` |

## Sink-identity verification

The monitor and notifier sinks serialize the dump-adapter error string
verbatim; redaction MUST therefore happen upstream. Identity covered by:

- `internal/monitor/sink_identity_test.go::TestRecordExecution_ErrorMessageIsIdentity`
- `internal/notifier/webhook_sink_identity_test.go::TestWebhookPayload_ErrorFieldIsIdentity`

## CI gate

`make lint-redact-stderr` scans `pkg/backup/` (and `internal/adapters/dump/`
once it exists) for any future regression where a raw `stdErr.String()` is
passed into `fmt.Errorf` / `errors.New` / `fmt.Sprintf`. Invoked from
`.github/workflows/integration.yml` on every PR. Self-test:
`scripts/test/lint-redact-stderr-self-test.sh`.

## Re-audit trigger

ADR 0001 plans to migrate dump adapters from `pkg/backup/<engine>_dump/`
to `internal/adapters/dump/<engine>/`. When that move lands, re-run this
audit and update the table; the Makefile target already scans the new
location.
