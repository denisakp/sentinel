# SENTINEL Runbooks

Operational procedures for running, recovering, and maintaining Sentinel in production.

> **Most of this material now lives on the [documentation site](https://denisakp.github.io/sentinel/),
> rewritten and expanded.** The site's [guides](https://denisakp.github.io/sentinel/guides/) cover
> setup and daily operations, and its
> [operations section](https://denisakp.github.io/sentinel/operations/) covers incidents.
>
> **Prefer the site.** Porting these runbooks to it involved checking every command, flag and
> configuration key against the source, and fifteen of the runbooks below were found to describe
> behaviour the code does not have. Some are stale; others document intended behaviour that was never
> wired up. Where the two disagree, the site describes what the code actually does today and links to
> the open issue.
>
> These files are kept because they are the source material the site was built from, and because a
> few of them are correct where the code is not. They are not maintained in parallel.

## Daily operations
- [Run backup from config](./run-backup-from-config.md)
- [Start the scheduler](./start-scheduler.md)
- [Inspect monitor history](./inspect-monitor-history.md)
- [Apply retention](./apply-retention.md)
- [GFS retention (grandfather-father-son)](./retention-gfs.md)
- [Parallel multi-job restore](./parallel-restore.md)
- [Check storage backend](./check-storage-backend.md)

## Setup
- [Environment setup](./environment-setup.md)
- [Enable encryption](./enable-encryption.md)
- [Backup compression (gzip/zstd)](./backup-compression.md)
- [Database credentials](./credentials.md)
- [Alerting setup](./alerting-setup.md)
- [DB migration status](./db-migration-status.md)
- [Monitor schema migration](./monitor-schema-migration.md)

## Restore
- [Restore from backup](./restore-from-backup.md)
- [PITR and incremental restore](./restore-pitr-and-incremental.md)
- [Restore from GCS](./restore-from-gcs.md)
- [Restore rehearsal (DR drill)](./restore-rehearsal.md)
- [Verify backup integrity](./verify-backup-integrity.md)
- [Repository-wide integrity sweep (`backup verify --all`)](./integrity-sweep.md)

## Incidents
- [Repository state repair (`sentinel repair`)](./state-repair.md)
- [Stale lock recovery](./stale-lock-recovery.md)
- [Scheduler crash recovery](./scheduler-crash-recovery.md)
- [Failed backup triage](./failed-backup-triage.md)
- [Chain corruption recovery](./chain-corruption-recovery.md)
- [Key loss incident](./key-loss-incident.md)
- [Recover legacy envelope](./recover-legacy-envelope.md)
- [Troubleshooting](./troubleshooting.md)

## Maintenance
- [Upgrade Sentinel binary](./upgrade-sentinel-binary.md)
- [Verify release artifacts (cosign + SLSA)](./verify-release-artifacts.md)
- [Migrate storage backend](./migrate-storage-backend.md)
- [Key rotation](./key-rotation.md)

## Reference
- [`additional_args` quoting reference](./additional-args.md)
- [Mongo remote-backup `.staging/` directory](./mongo-staging.md)
