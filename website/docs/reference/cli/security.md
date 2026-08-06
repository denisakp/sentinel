---
title: sentinel security
description: Reference for sentinel security init-key, reencrypt, and encrypt-secrets-file, with every flag, exit codes, and one known defect.
sidebar_position: 9
---

Generates the master encryption key, re-encrypts existing backups into the current envelope or under a new key, and encrypts a database credentials file at rest.

## Synopsis

```text
sentinel security [command]
sentinel security init-key [flags]
sentinel security reencrypt [backup-id] [flags]
sentinel security encrypt-secrets-file <plaintext-path> --out <path> [flags]
```

`sentinel security` on its own prints help. All three subcommands work with the same 256-bit AES key material: a base64-encoded 32-byte value supplied through an environment variable or a key file, never on the command line.

## Subcommands

| Subcommand | Purpose |
|---|---|
| `init-key` | Generates a cryptographically strong 256-bit key and prints it with storage instructions. Writes nothing to disk and does not modify the configuration. |
| `reencrypt` | Re-wraps an encrypted backup into the current v2 envelope, or re-encrypts it under a new master key. Takes one `[backup-id]`, or `--all`. |
| `encrypt-secrets-file` | Encrypts a plaintext MySQL/MariaDB defaults file or MongoDB secrets file into Sentinel's self-contained `SSEC` container, written to a new path. |

## Flags

### `sentinel security`

| Flag | Type | Default | Description |
|---|---|---|---|
| `-h`, `--help` | bool | `false` | Print help for `security`. |

### `sentinel security init-key`

| Flag | Type | Default | Description |
|---|---|---|---|
| `--force` | bool | `false` | Generate a new key even when `SENTINEL_MASTER_KEY` is already set in the environment. |
| `-h`, `--help` | bool | `false` | Print help for `init-key`. |
| `--output` | string | `text` | Output format. `json` emits the key plus `algorithm`, `key_length_bits`, `generated_at`, and instruction fields. Any other value, including a misspelling, silently produces the text form. |

The key is printed to standard output and never persisted. Both output forms name `SENTINEL_MASTER_KEY` and `encryption_key_file` as the places to put it.

:::danger A new key orphans every existing encrypted backup
Encrypted artifacts can only be decrypted with the key that wrote them. Generating a replacement key does not re-encrypt anything. If you are rotating rather than starting fresh, keep the old key, put the new one in a second environment variable, and use `sentinel security reencrypt --mode rotate --new-key-env <var>` so existing backups are rewritten under the new key before the old one is retired.
:::

:::caution The overwrite guard only watches `SENTINEL_MASTER_KEY`
The check that refuses to generate a key over an existing one reads the hard-coded variable `SENTINEL_MASTER_KEY`, not the variable named by `encryption_key_env`, and it never inspects `encryption_key_file`. If your key lives in `PROD_BACKUP_KEY` or in a file, `sentinel security init-key` prints a fresh key with no warning at all, and an operator who follows the printed instructions replaces a live key. The guard also exits 0 when it does fire, so a script cannot detect the refusal from the exit code. Tracked as issue #160.
:::

### `sentinel security reencrypt [backup-id]`

Accepts at most one positional `backup-id`. Exactly one of `<backup-id>` or `--all` must be supplied.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--all` | bool | `false` | Process every recorded successful backup in scope. Mutually exclusive with a positional `backup-id`. |
| `--config` | string | `$HOME/.sentinel/config.yaml` | Path to the Sentinel YAML config. Supplies `history_db_path`, the current encryption key settings, and the storage credentials for remote artifacts. |
| `--dry-run` | bool | `false` | Classify and report only. Reads manifests, never artifact bytes, and mutates nothing. |
| `-h`, `--help` | bool | `false` | Print help for `reencrypt`. |
| `--job` | string | n/a | With `--all`: restrict the sweep to a single named backup job. |
| `--keep-original` | bool | `false` | Write the new artifact to a sibling path suffixed `.reencrypted` and leave the original in place. The history row is not updated, so cutover is manual. |
| `--mode` | string | `legacy` | `legacy` migrates a pre-v2 envelope to v2; `rotate` re-encrypts a current-envelope artifact under `--new-key-env`. Any other value is rejected. |
| `--new-key-env` | string | n/a | Name of the environment variable holding the new base64 32-byte master key. Required in `rotate` mode. Env-only: there is no flag that takes the key itself. |
| `--output` | string | inherits `log_format` from the config, which defaults to `json` | Output format: `text` or `json`. |
| `--since` | string | n/a | With `--all`: only process backups newer than this age. Accepts `30d`, `4w`, or any Go duration such as `720h`. |
| `--yes` | bool | `false` | Confirm a bulk (`--all`) mutation. Not required for a single `<backup-id>` or for `--dry-run`. |

Each backup is classified from its manifest, then acted on according to the mode:

| Classification | `--mode legacy` | `--mode rotate` |
|---|---|---|
| Encryption envelope v2 or later | `skipped_already_current` | re-encrypted, `rotated` |
| Pre-v2 envelope | migrated to v2, `migrated` | `skipped_legacy` |
| No `encryption` block in the manifest | `skipped_unencrypted` | `skipped_unencrypted` |
| No manifest sidecar found | `skipped_unmigratable` | `skipped_unmigratable` |

Under `--dry-run` the eligible outcomes are reported as `would_migrate` and `would_rotate`.

The pipeline is write-new, verify, then swap: the artifact is decrypted with the old key, re-encrypted to a temporary file, hash-checked and decrypted back to confirm the plaintext round-trips, and only then written over the original path. Nothing in storage is mutated before that local verification succeeds. On a remote backend the uploaded object is downloaded again and hash-compared before the manifest is uploaded.

:::danger Destructive without `--keep-original`
The default behaviour replaces the stored artifact and its manifest at their existing paths, and updates the history row. `--all --yes` does this to every recorded backup. Run `sentinel security reencrypt --all --dry-run --config <file>` first, and keep the old key available until `sentinel backup verify --all --config <file>` passes against the re-encrypted repository. Use `--keep-original` when you want to cut over by hand.
:::

:::warning Legacy migration does not repair the legacy weakness
In `legacy` mode Sentinel prints a caveat banner to stderr before the report. Re-wrapping changes the format so `--allow-legacy-envelope` is no longer needed; it does not undo the confidentiality weakness of the pre-v2 nonce scheme, because that ciphertext may already be compromised. Where the source database is still available, re-running the backup from source is the real remediation.
:::

Exit codes: `0` success, `4` a validation or operational failure with nothing mutated, `5` at least one backup failed during processing.

Three behaviours differ from what the help text implies:

- `--config` defaults to `$HOME/.sentinel/config.yaml`, unlike `retention`, `storage status`, and most other commands, which fall back to `./sentinel-config.yaml`. `reencrypt` also loads the file without running full schema validation, so a configuration that `sentinel config validate` rejects can still drive a re-encryption.
- `--new-key-env` is accepted in `legacy` mode as well, where it performs a combined migrate-and-rekey. The help text describes it as required in rotate mode and says nothing about this.
- On a remote backend the history row's manifest path column is updated with the artifact's object key rather than the `.manifest.json` key. Local backups record the manifest path correctly. Anything reading that column for a remote re-encrypted backup gets the wrong key.

### `sentinel security encrypt-secrets-file <plaintext-path>`

:::info Added in v1.4.0
At-rest encryption of database credential files requires Sentinel v1.4.0 or later. `sentinel security reencrypt` was added in the same release.
:::

Requires exactly one positional path: the plaintext file to encrypt.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--force` | bool | `false` | Overwrite `--out` when it already exists. Without it, an existing output path fails the command. |
| `-h`, `--help` | bool | `false` | Print help for `encrypt-secrets-file`. |
| `--key-env` | string | n/a | Name of the environment variable holding the base64-encoded 256-bit key. One of `--key-env` or `--key-file` is required. |
| `--key-file` | string | n/a | Path to a file holding the base64-encoded key. Used when `--key-env` is unset or the variable is empty. |
| `--out` | string | n/a | Destination path for the encrypted file. Required, and must differ from the input path. |

The output is written to a temporary file in the destination directory with mode `0600` and then renamed into place, so an interrupted run never leaves a partial file that looks valid. The plaintext input is never modified or deleted; remove it yourself once you have confirmed the encrypted file loads.

The result begins with the four-byte magic `SSEC`, which is how the configuration loader recognises it. At config load time an `SSEC` file referenced by `defaults_file` or `mongo_secrets_file` is decrypted in memory using `secrets_key_env` or `secrets_key_file`, falling back to `encryption_key_env` or `encryption_key_file`. A plaintext file at the same key is read as-is, so the two forms are interchangeable in the configuration.

## Examples

Generate a key and put it in the environment for the current shell:

```bash
sentinel security init-key
export SENTINEL_MASTER_KEY='<paste-the-printed-key>'
```

The printed key is the only copy. Store it in a secrets manager before you close the terminal.

Generate a key as JSON for a provisioning script:

```bash
sentinel security init-key --output json
```

Emits an object whose `key` field holds the base64 value. Pipe it into your secret store rather than to a file on disk.

Preview what a legacy migration would do across the whole repository:

```bash
sentinel security reencrypt --all --dry-run --config sentinel.yaml
```

One row per backup showing the envelope transition and the outcome, with a summary line. Nothing is read from the artifacts and nothing is written.

Migrate one pre-v2 backup into the current envelope:

```bash
sentinel security reencrypt 01HQ8Z3K4M5N6P7Q8R9S --config sentinel.yaml
```

Reports `migrated` on success. The backup no longer needs `--allow-legacy-envelope` to verify or restore.

Rotate the whole repository onto a new key:

```bash
export SENTINEL_MASTER_KEY='<current-key-from-your-secret-store>'
export SENTINEL_MASTER_KEY_NEXT='<new-key-from-sentinel-security-init-key>'
sentinel security reencrypt --all --mode rotate \
  --new-key-env SENTINEL_MASTER_KEY_NEXT --yes --config sentinel.yaml
```

Both keys must be resolvable for the duration of the run: the old one to decrypt, the new one to re-encrypt. Afterwards, point `encryption_key_env` at the new variable and retire the old key only once `sentinel backup verify --all` passes.

Encrypt a MySQL defaults file and switch the job over to it:

```bash
export SENTINEL_SECRETS_KEY='<key-from-sentinel-security-init-key>'
sentinel security encrypt-secrets-file /etc/sentinel/my.cnf \
  --out /etc/sentinel/my.cnf.enc --key-env SENTINEL_SECRETS_KEY
```

Then reference the encrypted file and its key:

```yaml
secrets_key_env: SENTINEL_SECRETS_KEY

databases:
  prod-mysql:
    type: mysql
    defaults_file: /etc/sentinel/my.cnf.enc
```

Run one backup to confirm the file decrypts, then destroy the plaintext original with `shred -u /etc/sentinel/my.cnf`.

## Related

- [Encryption and key management](../../concepts/security-encryption.md)
- [Credential sanitization](../../concepts/credential-sanitization.md)
- [Configuration reference](../configuration.md)
- [`sentinel backup`](./backup.md) for `verify` and `--allow-legacy-envelope`
- [`sentinel restore`](./restore.md)
- [`sentinel monitor`](./monitor.md) for the history rows `reencrypt` updates
- [CLI reference index](./index.md)

<!-- sources: internal/cli/security.go, internal/cli/security_reencrypt.go, internal/cli/security_secrets.go, internal/cli/exit_codes.go, internal/adapters/crypto/key.go, internal/adapters/crypto/secrets_envelope.go, internal/config/secrets_file_crypto.go, internal/config/types.go, release-notes.md -->
