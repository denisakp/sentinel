# SENTINEL Runbooks

Operational procedures for running, recovering, and maintaining Sentinel in production.

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

## Incidents
- [Stale lock recovery](./stale-lock-recovery.md)
- [Scheduler crash recovery](./scheduler-crash-recovery.md)
- [Failed backup triage](./failed-backup-triage.md)
- [Chain corruption recovery](./chain-corruption-recovery.md)
- [Key loss incident](./key-loss-incident.md)
- [Recover legacy envelope](./recover-legacy-envelope.md)
- [Troubleshooting](./troubleshooting.md)

## Maintenance
- [Upgrade Sentinel binary](./upgrade-sentinel-binary.md)
- [Migrate storage backend](./migrate-storage-backend.md)
- [Key rotation](./key-rotation.md)

## Reference
- [`additional_args` quoting reference](./additional-args.md)
- [Mongo remote-backup `.staging/` directory](./mongo-staging.md)
