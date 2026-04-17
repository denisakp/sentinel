# Incremental Integration Test Notes

This directory hosts integration coverage for feature 006 incremental backup and restore.

Planned scenarios:

- PostgreSQL full + incremental chain restore using pg_combinebackup
- MySQL/MariaDB binary log archival and replay target validation
- MongoDB oplog archival and replay on replica set deployments
- Chain validation failures (missing baseline, broken lineage, hash mismatch)
- Fallback confirmation behavior for broken incremental plans
- Staging capacity and required-tool fail-fast checks
