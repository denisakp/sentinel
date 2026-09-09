# PRD I11 — Storage backends + config schema drift

**Severity:** Medium (Azure reported unreachable when it works; verify can cross backends)
**Status:** Open
**Issues:** #161, #184.1, #184.3, #184.4, #145, #144
**Area:** `internal/cli/{storage_cmd,backup_verify}.go`, `internal/config/{types,loader}.go`,
`internal/adapters/restore/runtime/staging.go`, `internal/adapters/storage/`

All settled statically. No live infrastructure was needed to confirm any of them.

## Confirmed defects

### #161 — `storage status` reports every Azure named storage as unreachable

`internal/cli/storage_cmd.go:137-141` builds an `azure.Config{AccountName, Container}` by hand —
**no `Auth`** — then calls `azure.NewAzureBlobBackend`, which dispatches on `cfg.Auth.Type`
(`azure/auth.go:15-54`). The empty `Type` falls through to the default and errors
`unsupported auth type ""` (auth.go:54).

The backup path does it correctly: `registry.go:122` calls
`NewBlobBackendFromKey(account, container, key)`. **Two constructors, one wired right.**

### #184.1 — `storage status` row order is nondeterministic

`storage_cmd.go:56` ranges over `cfg.Storages` (a Go map) with no sort anywhere in the file.

### #184.2 — see I06

Counted there, with the manifest sidecar work.

### #184.3 — `defaults.storage` overwrites a partial job storage

`internal/config/loader.go:97-99`:

```go
if job.Storage.Name=="" && job.Storage.Type=="" && cfg.Defaults.Storage.Type!="" {
    job.Storage = cfg.Defaults.Storage
}
```

Whole-struct assignment, not a field merge. A job that sets only `storage.local_path` loses it.

**The correct implementation already exists in the same file**: `resolveNamedStorages`
(loader.go:591-598) field-merges via `overlayStorage`. The defaults path just never calls it.

### #184.4 — `backup verify` combines the recorded backend type with current credentials

`internal/cli/backup_verify.go:198-211` dispatches on `exec.StorageBackend` — the type **recorded at
backup time** — but fetches connection parameters from `cfg.Databases[exec.BackupName].Storage`, the
**current** config. `verifyBackendParamsAndObject` (591-643) switches again on the recorded type
using current credentials.

Change a job's `storage.type` from s3 to gcs and verify will apply GCS credentials against an
S3-recorded reference. Not in the issue title; the most dangerous item here.

### #145 — `backup_source` accepts only local, s3 and gcs

`runtime/staging.go:233-251` (`listSourceObjects`) and :268-289 (`downloadSourceObject`) switch on
`source.Type` with cases for `local`/`s3`/`gcs` only; default returns
`ErrUnsupportedRestoreSource`. Meanwhile `config/restore_types.go:150-158` defines
`GDriveFolderID`, `GDriveSAFile`, `AzureStorageAccount`, `AzureContainer` on `RestoreBackupSource`,
and **no validator rejects those types**. The registry already has both backends.

### #144 — `AzureConfig` and `AzureAuthConfig` are unreachable

`config/types.go:178-191` defines both (with an `auth:` sub-block and `tier`). **No field of
`Configuration` (types.go:194-244) or any nested struct has that type.**
`grep -rn "AzureConfig\b" --include=*.go .` excluding tests finds only the declaration itself,
a doc comment in `azure/config.go`, and `validator.go:481` — which does not use the struct, it calls
`ValidateAzureConfig` with primitive strings.

The reachable Azure schema is the flat `azure_storage_account` / `_key` / `_container` on
`StorageConfig` (types.go:419-423). Also: `backup --help`'s advertised storage types disagree with
`storage.ValidateStorageType`, which does accept `azure`.

## Root causes

**(A)** `storage_cmd.go` and the `--help` text were hand-rolled independently of
`internal/adapters/storage/registry.go` and have drifted from it — #161, #184.1, #184.2, and #144's
help/validator mismatch.

**(B)** Config schema drift — the YAML advertises fields the runtime never implements (#145) while
carrying an orphaned dead duplicate of the real schema (#144).

#184.3 and #184.4 are independent of both: separate `loader.go` and `backup_verify.go` bugs.

## Fix order

1. **#144** — delete the dead `AzureConfig`/`AzureAuthConfig` structs; reconcile `backup --help` with
   `ValidateStorageType`. Cheap, and it clears the Azure story before touching #161.
2. **#161** — have `storage_cmd.go`'s azure case call `azure.NewBlobBackendFromKey` like
   `registry.go` does, instead of hand-building a `Config`. Small diff.
3. **#184.1** — sort entries by name before printing or encoding. Same file as #161, do together.
4. **#145** — implement the gdrive/azure cases in `staging.go` mirroring s3/gcs, **or** reject those
   `source.Type` values at config validation. See the decision below.
5. **#184.3** — replace the whole-struct overwrite with `overlayStorage` semantics.
6. **#184.4** — persist the full backend params at backup time, **or** refuse/downgrade verify when
   the current `storage.type` no longer matches `exec.StorageBackend`.

## DECISION REQUIRED

- **#145** — implement gdrive/azure as restore sources, or reject them at validation. The config
  struct already advertises the fields, so rejecting means removing them. Note this pairs with
  **#169** in I05: gdrive is half-implemented on both the retention and restore axes.
- **#184.4** — persisting backend params at backup time is the correct fix but changes the history
  schema (a migration). Refusing on mismatch is cheap and safe. Pick one.

## Definition of done

- **Code:** the six fixes.
- **e2e:**
  - `storage status` against Azurite configured with `azure_storage_key` — `reachable: true` and a
    correct `backup_count`.
  - `storage status --output json` run twice against ≥3 named storages — identical key order.
  - A job setting only `storage.local_path` with `defaults.storage.type` present — the load retains
    `local_path` merged with the default type.
  - A restore job with `backup_source.type: azure` — either it works, or it is rejected at config
    validation. Not `ErrUnsupportedRestoreSource` at run time.
  - Backup on s3, change the job's `storage.type` to gcs, `backup verify <id>` — does not silently
    apply GCS credentials against the S3-recorded reference.
  - `go vet` / dead-code check passes after the #144 deletions.
- **Docs:** `reference/cli/storage.md`, `guides/check-storage-backend.md`,
  `guides/migrate-storage-backend.md`, `concepts/storage-backends.md`. **#184 is not indexed in
  `website/docs/` at all** — add a pointer once it lands.
