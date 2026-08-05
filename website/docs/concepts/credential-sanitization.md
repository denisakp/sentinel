---
title: Credential handling
description: How Sentinel keeps database passwords out of process arguments, what redaction protects, and the secrets-file options for each engine.
sidebar_position: 8
---

Sentinel drives your engine's own tools, so every backup and restore involves handing a database
password to a subprocess. It does that through the subprocess environment or a connection URI, never
through the command line, and it filters credential-shaped text out of the messages it writes about
those subprocesses. Configuration follows the same rule: there is no YAML key that holds a password
inline. A password is always named indirectly, by environment variable or by a file path.

## Why it exists

A process's argument vector is not private. On Linux, `/proc/<pid>/cmdline` is readable by any user
on the host for the duration of the process, and `ps -ef` renders the same data in a form anyone can
scroll. A password passed as `--password=hunter2` is therefore visible to every account on the
machine, to any sidecar container sharing the PID namespace, and to process accounting or audit
tooling that records command lines. The exposure is brief but it is a full disclosure while it lasts,
and a backup job that runs every night reopens the window every night.

The shell adds a second channel. An interactive command with a password in it lands in
`~/.bash_history` or `~/.zsh_history`, in plaintext, where it survives long after the process has
exited. A third channel is subtler: when a dump tool fails, its own error text can quote the
connection parameters it was given, and that text then gets embedded in an error message, printed to
stderr, and written into the execution history.

None of these are exotic attacks. They are the default behaviour of the operating system. The only
reliable fix is to never put the secret in argv at all, which is what the credential channels
described below exist to make convenient.

## How it works

Credential handling in Sentinel is three separate mechanisms that reinforce each other. It helps to
keep them distinct, because they protect against different things.

**Resolution happens before any database I/O.** Loading a configuration file resolves every `*_env`
field: `host_env`, `username_env`, `password_env`, `uri_env`, and the storage and notification
equivalents. An environment variable that is named but unset is a hard config-load error, not a
warning, so a missing secret fails immediately rather than partway through a dump. The same is true
of the secrets files: a `defaults_file` or `mongo_secrets_file` that is missing, unreadable, or
malformed fails at load time. Nothing connects to a database until the credential picture is
complete.

**The password reaches the engine tool out of band.** Sentinel builds the argument list for
`pg_dump`, `mysqldump`, `mariadb-dump`, or `mongodump` with host, port, user, and database, and then
supplies the password separately. For PostgreSQL it appends `PGPASSWORD` to the subprocess
environment; for MySQL and MariaDB it appends `MYSQL_PWD`. Neither variable is exported into
Sentinel's own environment, so it is scoped to the child process. MongoDB has no equivalent
environment channel, so the password travels inside the `--uri=` connection string, which is the
one case where a credential does appear in argv. The restore adapters use the same channels for
`pg_restore`, `psql`, `mysql`, `mariadb`, and `mongorestore`.

**Redaction filters what Sentinel writes.** The `internal/sanitize` package holds a small set of
regular expressions matching credential-shaped text: `--password=VALUE`, the `mysqldump`-style
`-pVALUE`, `PGPASSWORD=VALUE`, `MYSQL_PWD=VALUE`, `MONGO_INITDB_ROOT_PASSWORD=VALUE`, a libpq-style
`password = VALUE`, and the userinfo segment of a `scheme://user:secret@host` URI. Matched values are
replaced with `*****`. `RedactArgs` applies these to an argument slice and additionally handles the
two-token forms where the flag and its value are separate elements. `RedactStderr` applies them to a
subprocess's captured stderr and caps the result at 64 KiB.

### What redaction does not protect

This is the part worth internalising, because the guarantee is narrower than the name suggests.

Redaction operates on strings that Sentinel is about to emit. It is a last line of defence for text
that has already been captured, and it is pattern-based, which means it recognises the credential
shapes listed above and nothing else. A tool that invents a novel way to print a secret will not be
matched.

More importantly, redaction cannot reach anything Sentinel does not route through it. If `mysqldump`
writes to a terminal that Sentinel is not capturing, or an engine writes credentials into its own
server log, that output is entirely outside Sentinel's control. Redaction also cannot retroactively
protect an argv: once a command line exists, `/proc` has already published it, and no amount of
filtering afterwards helps. That is why the primary defence is the environment-variable channel and
redaction is only the secondary one.

Two consequences follow. First, MongoDB deserves extra care, because its credential lives inside a
URI rather than in a separate environment variable, so any message that echoes a connection string
is a potential disclosure. Second, encrypting a secrets file protects the file at rest and nothing
more: while a backup runs, the plaintext credential necessarily exists in the process's memory.

## Per-engine behaviour

Each engine hands its password to its tools differently, and the difference determines which
protections apply.

| Engine | Password channel | Appears in argv | Secrets file | Notes |
|---|---|---|---|---|
| PostgreSQL | `PGPASSWORD` in the subprocess environment | No | None; PostgreSQL has no Sentinel secrets file | Used for `pg_dump`, `pg_dumpall`, `pg_restore`, and the `psql` paths for plain-SQL restore and connectivity checks |
| MySQL | `MYSQL_PWD` in the subprocess environment | No | `defaults_file`, a standard `my.cnf` option file | Only the `[client]` section is read; `!include` directives and other sections are ignored |
| MariaDB | `MYSQL_PWD` in the subprocess environment | No | `defaults_file`, identical to MySQL | The two engines share one argument builder and differ only in the binary invoked |
| MongoDB | Userinfo inside the `--uri=` connection string | Yes, inside the URI | `mongo_secrets_file`, a Sentinel-native YAML file | The only engine whose credential reaches argv; redaction of the URI userinfo segment is the mitigation |

The MySQL and MariaDB `defaults_file` seeds host, user, password, and port for the whole pipeline,
not just the dump: the pre-backup connectivity check and `database: "*"` auto-discovery read the same
values. This distinguishes it from passing `--defaults-extra-file=...` through `additional_args`,
which forwards a raw flag to the dump binary alone and never reaches Sentinel's own Go-side
connection code.

## Configuration

There is no key that holds a password inline. Every channel is indirect.

```yaml
version: "1.0"

# Key for decrypting secrets files. Falls back to encryption_key_file
# or encryption_key_env when secrets_key_* is unset.
secrets_key_file: /etc/sentinel/master.key

databases:
  prod-postgres:
    type: postgres
    host_env: PROD_DB_HOST
    port: 5432
    username_env: PROD_DB_USER
    password_env: PROD_DB_PASSWORD
    database: myapp_prod
    output: prod-postgres.sql
    storage:
      type: local
      local_path: ./backups

  prod-mysql:
    type: mysql
    database: "*"
    defaults_file: /etc/sentinel/db.cnf
    storage:
      type: local
      local_path: ./backups

  prod-mongo:
    type: mongodb
    uri: "mongodb://appuser@mongo:27017/?replicaSet=rs0"
    mongo_secrets_file: /run/secrets/mongo.yaml
    storage:
      type: local
      local_path: ./backups
```

The keys involved:

- `password_env:` names an environment variable holding the password. `host_env:`, `username_env:`,
  and `uri_env:` follow the same pattern for the other connection fields.
- `defaults_file:` is valid only for `mysql` and `mariadb` jobs. Validation rejects it on any other
  engine. Explicit `host`, `username`, `port`, and `password_env` always win; the file only fills in
  what is unset.
- `mongo_secrets_file:` is valid only for `mongodb` jobs, and supplies `password`, `uri`, and
  `ssl_pem_key_password`. An explicit `uri` or `uri_env` always wins, and the file's `password` fills
  only a URI that has a username but no password.
- `defaults_file_env:` and `mongo_secrets_file_env:` name an environment variable holding the *path*
  to the file, for deployments where the mount location is known only at runtime.
- `secrets_key_env:` and `secrets_key_file:` name the key that decrypts an encrypted secrets file.
  When both are unset, the secrets-file path falls back to `encryption_key_env` and
  `encryption_key_file`, so a simple deployment needs only one key.

The complete key list, with types and defaults, is in the
[configuration reference](../reference/configuration.md).

### Command-line channels

For one-off runs without a configuration file, `sentinel backup` accepts two safe flags:

| Flag | Behaviour |
|---|---|
| `--password-env <VAR>` | Reads the password from the named environment variable. An unset or empty variable is an error |
| `--password-file <PATH>` | Reads the first line of the file, right-trimmed of spaces, tabs, and line endings. Leading whitespace is preserved |

Supplying more than one password flag is a hard error. A CLI flag overrides the job's
`password_env`, which in turn overrides a `defaults_file` password.

:::caution `--password` is deprecated
The legacy `--password` / `-p` flag still functions in v1.4.0 and is hidden from `--help`. Using it
prints a deprecation notice on stderr naming the exposure channels, and the flag will be removed in
the next minor release. Migrate to `--password-env`, `--password-file`, or `password_env` in the
configuration file.
:::

## Example

Run a job whose password comes from the environment, and confirm that the password never appears in
the dump tool's command line:

```bash
export PROD_DB_PASSWORD='placeholder-not-a-real-password'
sentinel backup --config sentinel.yaml
```

While the backup runs, inspect the child process from another shell:

```bash
ps -o args= -p "$(pgrep -f pg_dump)"
```

You should see the connection flags and no credential:

```
pg_dump --host=db --port=5432 --username=backup --dbname=myapp_prod --format=c
```

### Encrypting a secrets file at rest

:::info Added in v1.4.0
At-rest encryption of `defaults_file` and `mongo_secrets_file` requires Sentinel v1.4.0 or later.
:::

A plaintext `my.cnf` holds a live password on disk, which is a standing exposure if the file is ever
swept into a support bundle, a snapshot, or a configuration backup. Encrypt it to a new path:

```bash
sentinel security init-key --output text > /etc/sentinel/master.key
chmod 0600 /etc/sentinel/master.key

sentinel security encrypt-secrets-file /etc/sentinel/db.cnf \
  --out /etc/sentinel/db.cnf.enc \
  --key-file /etc/sentinel/master.key
```

The command writes a new file and never modifies or deletes the input. Point the job at the encrypted
form and declare the key globally:

```yaml
secrets_key_file: /etc/sentinel/master.key

databases:
  prod-mysql:
    type: mysql
    defaults_file: /etc/sentinel/db.cnf.enc
```

No configuration flag says "this file is encrypted"; the encrypted form is detected from the file's
own container header, so a plaintext file keeps working unchanged and needs no key. Confirm the
configuration still loads, then retire the plaintext original:

```bash
sentinel config validate --config sentinel.yaml
shred -u /etc/sentinel/db.cnf
```

The decrypted content exists only in memory. The encrypted file is self-contained and carries its own
decryption material, so copying or moving it as a single file never breaks decryption.

## Failure modes

**`environment variable 'PROD_DB_PASSWORD' referenced by --password-env is unset or empty`.** The
named variable does not exist in Sentinel's environment. Under systemd this usually means the
`EnvironmentFile` was not loaded; in Kubernetes it usually means the `secretRef` key name does not
match. The run stops before any connection attempt.

**`multiple password sources supplied on the command line`.** More than one of `--password`,
`--password-env`, and `--password-file` was given. Exactly one is permitted; there is no precedence
among them.

**`password file '/path' is empty after trimming the first line`.** Only the first line is read, and
it is right-trimmed. A file written with `echo` rather than `echo -n` still works, but a file whose
first line is blank does not.

**A permission warning on stderr.** When a `--password-file`, a `defaults_file`, or a
`mongo_secrets_file` is group- or world-readable, Sentinel prints a warning naming the file and its
mode and then proceeds. It is advisory, not fatal. The warning fires only on plaintext files; an
encrypted secrets file is ciphertext, so it is not reported.

**`encrypted secrets file ... but no decryption key configured`.** The file carries the encrypted
container header, but neither `secrets_key_env` / `secrets_key_file` nor the `encryption_key_*`
fallback is set.

**`cannot decrypt secrets file ...: wrong key or corrupt data`.** The key does not match, or the file
was truncated. The whole file is decrypted in memory before parsing, so a wrong key always produces
this single clear message rather than a garbled half-parse.

**`defaults_file is only valid for mysql or mariadb backup jobs`**, or
**`mongo_secrets_file is only valid for mongodb backup jobs`.** The secrets-file kind does not match
the job's engine. These are caught during validation.

**`already has a password in its uri; conflicting password sources`.** A MongoDB job supplied a
password both inside its `uri` and in its `mongo_secrets_file`. Sentinel refuses to guess which one
you meant. The mirror-image error, a password supplied with no username known anywhere, is rejected
for the same reason.

## Related

- [Backup](./backup.md): where credential resolution sits in the run sequence.
- [Restore](./restore.md): the same channels on the restore path.
- [Encryption](./security-encryption.md): encrypting backup artifacts, which shares a cipher and key
  format with secrets-file encryption but is otherwise a separate feature.
- [`sentinel backup` reference](../reference/cli/backup.md): the `--password-env` and
  `--password-file` flags in full.
- [`sentinel security` reference](../reference/cli/security.md): `init-key` and
  `encrypt-secrets-file`.
- [Configuration reference](../reference/configuration.md): every YAML key, including the `*_env`
  family.
- [How Sentinel fits together](../intro/architecture-overview.md): where the sanitize and crypto
  adapters sit.
- [Database credentials](../guides/database-credentials.md): choosing how Sentinel authenticates to each engine.
- [Enable encryption](../guides/enable-encryption.md): encrypting the secrets files themselves.

<!-- sources: internal/sanitize/sanitize.go, internal/adapters/dump/pg/pg_dump.go, internal/adapters/dump/mysql/mysql_dump.go, internal/adapters/dump/mariadb/mariadb_dump.go, internal/adapters/dump/mongo/mongo_dump.go, internal/adapters/dump/mongo/args_builder.go, internal/adapters/restore/pg/pg_restore.go, internal/adapters/restore/mysql/mysql_restore.go, internal/adapters/restore/mongo/mongo_restore.go, internal/adapters/restore/pg/args_builder.go, internal/adapters/mysqlargs/core.go, internal/adapters/mysqlargs/defaults_file.go, internal/config/types.go, internal/config/env.go, internal/config/loader.go, internal/config/validator.go, internal/config/password_source.go, internal/config/secrets_file_crypto.go, internal/config/defaults_file_resolve.go, internal/config/mongo_secrets_file.go, internal/adapters/crypto/secrets_envelope.go, internal/cli/backup.go, internal/cli/security.go, docs/runbooks/credentials.md -->
