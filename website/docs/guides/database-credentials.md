---
title: Supplying database credentials
description: "Every channel Sentinel accepts a database password through: environment variables, password files, my.cnf, and MongoDB secrets files."
sidebar_position: 3
---

Give Sentinel the password it needs to reach your database, without writing that password into a
command line, a shell history file, or a process listing.

## When to use this

Read this when you are wiring up a new job, moving from an interactive drill to a scheduled one, or
migrating a deployment off the deprecated `--password` flag. It covers all six channels and the
precedence rules between them.

You do not need this page for MongoDB instances with no authentication, or for a PostgreSQL server
that trusts the connecting host. It also does not cover storage backend credentials, which are a
separate set of keys; see [Storage backends](../concepts/storage-backends.md).

## Before you start

- A configuration file, or the flags for a one-off run. See
  [Configuration reference](../reference/configuration.md).
- A place to keep the secret. A secrets manager injecting an environment variable is the target
  state for most deployments; a mode `0600` file is the alternative where an environment variable is
  awkward.
- For the encrypted secrets file in step 6, Sentinel v1.4.0 or later and a master key. See
  [Enabling backup encryption](./enable-encryption.md).

Two facts shape everything below. Sentinel resolves credentials at configuration load time, so a
missing variable or an unreadable file fails immediately and never partway through a dump. And a
password is never accepted as an inline YAML value; the schema has `password_env`, not `password`.

## Steps

Pick one channel per job. They are listed roughly in order of how often they are the right answer.

### 1. An environment variable, from the configuration

The default choice for scheduled and repeated runs. Name the variable in the job; Sentinel reads it
when the configuration loads.

```yaml
databases:
  primary:
    type: postgres
    host: db.internal
    username: app
    password_env: SENTINEL_DB_PASSWORD
    database: app
```

```bash
export SENTINEL_DB_PASSWORD="$(vault kv get -field=password secret/sentinel/primary)"
sentinel backup --config sentinel.yaml
```

If `SENTINEL_DB_PASSWORD` is unset or empty, the run stops with
`environment variable 'SENTINEL_DB_PASSWORD' is not set`.

### 2. An environment variable, from a flag

The same channel, chosen per invocation. Useful for a one-off drill against an account that is not
the one in the configuration.

```bash
export OPS_DRILL_PASSWORD='<from your secret store>'
sentinel backup --config sentinel.yaml --password-env OPS_DRILL_PASSWORD
```

The flag silently overrides `password_env` for every job in the run. There is no warning, by design,
so that a drill does not have to edit the configuration.

### 3. A password file

The right shape for Kubernetes secret mounts and any platform that presents secrets as files.

```bash
sentinel backup --type postgres --user app --database app \
  --password-file /var/run/secrets/db/password
```

Only the first line is used. It is right-trimmed of spaces, tabs, carriage returns, and newlines;
leading whitespace is preserved, because some systems permit a password that begins with a space.
Later lines are ignored. An empty first line is an error rather than an empty password.

If the file is group- or world-readable, Sentinel prints one warning to standard error and continues:

```
warning: password file '/var/run/secrets/db/password' has permissions 0o644 (group- or world-readable); recommend chmod 0600
```

### 4. A MySQL or MariaDB option file

For MySQL and MariaDB only, `defaults_file` points at a standard `my.cnf`. Its `[client]` section
seeds `host`, `username`, `password`, and `port` for the whole pipeline: the pre-backup connectivity
check, `database: "*"` auto-discovery, and the dump itself.

```yaml
databases:
  mydb:
    type: mysql
    database: "*"
    defaults_file: /etc/sentinel/db.cnf
```

```ini
# /etc/sentinel/db.cnf, mode 0600
[client]
host = 127.0.0.1
user = sentinel_backup
password = <the backup account's password>
port = 3306
```

Only `[client]` is read. Other sections such as `[mysqldump]`, and `!include` and `!includedir`
directives, are ignored rather than rejected. A file with no `[client]` section is not an error; it
simply contributes nothing. A missing, unreadable, or malformed file fails at configuration load.

The file fills gaps only. Any field you set explicitly wins, and `password_env` beats the file's
`password`. Sentinel applies `defaults_file` only to `mysql` and `mariadb` jobs; declaring it on any
other engine is a validation error.

This is unrelated to `additional_args: "--defaults-extra-file=..."`, which forwards a raw flag to the
dump binary. That passthrough still works, but it reaches only the dump subprocess, not Sentinel's
own connectivity check or discovery. See [Additional arguments](../reference/additional-args.md).

Where the mount path is only known at runtime, name a variable that holds it instead of a literal
path:

```yaml
    defaults_file_env: MYSQL_DEFAULTS_FILE
```

### 5. A MongoDB secrets file

For MongoDB only, `mongo_secrets_file` points at a small Sentinel-native YAML file that can supply a
password, a full connection URI, and a TLS private-key passphrase. Its values are composed into the
job's URI for the whole pipeline.

```yaml
databases:
  mongo-prod:
    type: mongodb
    uri: "mongodb://appuser@mongo:27017/?replicaSet=rs0"
    mongo_secrets_file: /run/secrets/mongo.yaml
```

```yaml
# /run/secrets/mongo.yaml, mode 0600
password: "<the backup account's password>"
# uri: "mongodb://appuser:<password>@mongo:27017/?replicaSet=rs0"
# ssl_pem_key_password: "<TLS key passphrase>"
```

Precedence is per field. An explicit `uri` or `uri_env` always wins, and the file's `uri` is then
simply unused rather than an error. The file's `password` only fills a URI that already carries a
username and no password. If the URI already has a password, or no username is known anywhere,
configuration loading fails with a clear message rather than guessing.

`mongo_secrets_file_env` names a variable holding the path, exactly as for `defaults_file_env`.

:::note The TLS passphrase is captured but not yet delivered
`ssl_pem_key_password` resolves through the same mechanism as `tls.client_key_password_env`, but
neither value currently reaches a live MongoDB TLS connection on the configuration-driven backup
path. That is a separate gap in how MongoDB TLS material is wired. The value is parsed and available
for when that wiring lands.
:::

### 6. Encrypt the secrets file at rest

:::info Added in v1.4.0
Encrypted secrets files, the `secrets_key_env` and `secrets_key_file` keys, and
`sentinel security encrypt-secrets-file` require Sentinel v1.4.0 or later.
:::

A plaintext `defaults_file` or `mongo_secrets_file` is a live password sitting on disk. Both can be
stored encrypted and decrypted in memory at load time. Encrypt to a new path; the command never
touches the input:

```bash
sentinel security encrypt-secrets-file /etc/sentinel/db.cnf \
  --out /etc/sentinel/db.cnf.enc \
  --key-env SENTINEL_SECRETS_KEY
```

Then point the job at the encrypted file and name the key at the top level of the configuration:

```yaml
secrets_key_env: SENTINEL_SECRETS_KEY   # falls back to encryption_key_env when unset

databases:
  mydb:
    type: mysql
    defaults_file: /etc/sentinel/db.cnf.enc
```

Detection is by content, not by configuration: Sentinel checks for the `SSEC` magic bytes. A
plaintext file keeps working with no key. The group- and world-readable warning is suppressed for
encrypted files, since ciphertext is not a credential exposure. The container format and the fallback
rules are described in [Security and encryption](../concepts/security-encryption.md).

## Verify

Confirm the configuration resolves. This parses the file, reads every `*_env` variable it references,
and decrypts any encrypted secrets file, all without connecting to a database:

```bash
sentinel config validate --config sentinel.yaml
```

Then run the job. A successful dump is the only proof that the credential is correct as well as
present:

```bash
sentinel backup --config sentinel.yaml
```

While a backup is running, confirm the password is not in the process listing:

```bash
ps -o args= -p "$(pgrep -f mysqldump)"
```

For PostgreSQL, MySQL, and MariaDB there must be no `--password` or `password=` token; those engines
receive the password through `PGPASSWORD` or `MYSQL_PWD` in the subprocess environment.

:::caution MongoDB is the exception
`mongodump` and `mongorestore` take the whole connection URI as a command-line argument, so a
password embedded in that URI is visible in `ps` and in `/proc/<pid>/cmdline` for the life of the
process. This is a property of the MongoDB tools, and it applies however you supply the password,
including through `mongo_secrets_file`. On a shared host, restrict who can read other users'
processes.
:::

## If it goes wrong

**`multiple password sources supplied on the command line`.** You passed more than one of
`--password`, `--password-env`, and `--password-file`. Use exactly one. The check runs before any
database I/O.

**`environment variable 'X' referenced by --password-env is unset or empty`.** The variable is
missing from the environment Sentinel actually runs in. Under systemd, that means the unit's
`EnvironmentFile`; under cron, it means the crontab's own environment, which does not inherit your
login shell.

**`password file '<path>' is empty after trimming the first line`.** The file exists but its first
line is blank. A common cause is `echo` adding a newline into an otherwise empty file; use
`printf '%s' "$password" > file`.

**`defaults_file is only valid for mysql or mariadb backup jobs`**, or the same message for
`mongo_secrets_file is only valid for mongodb backup jobs`. The key is on the wrong engine.

**`cannot decrypt secrets file '<path>': wrong key or corrupt data`.** The file is encrypted and the
resolved key does not open it. Check `secrets_key_env`, and remember it falls back to
`encryption_key_env` only when it is unset entirely.

**A restore job ignores `defaults_file`.** It is not ignored so much as unsupported: Sentinel applies
`defaults_file` and `mongo_secrets_file` to backup jobs under `databases:` only. Restore jobs take
`password_env`, `uri`, and `uri_env`. Unrecognised keys elsewhere in the file are parsed loosely and
dropped without a warning, so a misplaced key looks like it worked.

**A password appears in an error message.** Two MongoDB restore paths do not redact. A failed
connectivity check prints the URI verbatim, and a failed oplog replay embeds `mongorestore`'s own
standard error, which can echo the connection string. Treat MongoDB restore logs as credential
bearing until this is fixed. The dump paths do redact; see
[Credential sanitization](../concepts/credential-sanitization.md).

**You are still using `--password`.** The flag still functions but is deprecated, hidden from
`sentinel backup --help`, and prints a notice on use. It puts the password in your process listing
and your shell history. Move to one of the channels above before it is removed.

## Related

- [Credential sanitization](../concepts/credential-sanitization.md): how Sentinel keeps secrets out
  of arguments, logs, and captured tool output.
- [Security and encryption](../concepts/security-encryption.md): the `SSEC` secrets container and how
  keys are resolved.
- [Enabling backup encryption](./enable-encryption.md): generating the key this page's step 6 needs.
- [Setting up a practice environment](./environment-setup.md): a stack to try these channels against.
- [`sentinel backup` reference](../reference/cli/backup.md): every flag on the command.
- [Configuration reference](../reference/configuration.md): every YAML key named above.

{/* sources: internal/config/password_source.go, internal/config/types.go, internal/config/loader.go, internal/config/validator.go, internal/config/env.go, internal/config/defaults_file_resolve.go, internal/config/mongo_secrets_file.go, internal/config/secrets_file_crypto.go, internal/adapters/mysqlargs/defaults_file.go, internal/sanitize/sanitize.go, internal/cli/backup.go, internal/cli/security_secrets.go, internal/adapters/dump/mongo/args_builder.go, internal/adapters/restore/mongo/mongo_restore.go, internal/adapters/restore/mongo/oplog_replay.go, docs/runbooks/credentials.md */}
