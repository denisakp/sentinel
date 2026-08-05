# Database credentials

Sentinel never accepts passwords on the command line in supported channels. Use one of three safe sources; conflicts among the CLI flags are hard errors before any database I/O.

## Three safe channels

### 1. Environment variable (recommended for shells, systemd units, CI)

```bash
export DB_PWD='hunter2'
sentinel backup --type postgres --user app --database app --password-env DB_PWD
```

### 2. File (recommended for Kubernetes / mounted secrets)

```bash
echo -n 'hunter2' > ~/.sentinel/db_pwd
chmod 0600 ~/.sentinel/db_pwd
sentinel backup --type postgres --user app --database app --password-file ~/.sentinel/db_pwd
```

The first line is read and right-trimmed of `" \t\r\n"`. Leading whitespace is preserved (some legacy systems accept passwords starting with a space). Subsequent lines are ignored.

If the file mode includes any group or world read bit (`mode & 0o044 != 0`), Sentinel emits a single stderr warning and proceeds:

```
warning: password file '/home/me/.sentinel/db_pwd' has permissions 0o644 (group- or world-readable); recommend chmod 0600
```

### 3. Config file (recommended for scheduled / repeated use)

```yaml
# sentinel.yaml
databases:
  primary:
    type: postgres
    host: db
    user: app
    database: app
    password_env: DB_PWD
```

```bash
export DB_PWD='hunter2'
sentinel backup --config sentinel.yaml
```

### 4. MySQL/MariaDB defaults file (my.cnf) — `defaults_file`

For MySQL/MariaDB jobs only, `defaults_file` points at a standard my.cnf-format option file and seeds `host`/`user`/`password`/`port` from its `[client]` section for **every** part of the pipeline that needs them — the pre-backup connectivity check, `database: "*"` auto-discovery, and the dump itself (spec 056 / PRD 44, closing GitHub issue #28).

```yaml
databases:
  mydb:
    type: mysql
    database: "*"
    defaults_file: /etc/sentinel/.my.cnf
```

```ini
# /etc/sentinel/.my.cnf  (chmod 0600)
[client]
host = 127.0.0.1
user = sentinel_backup
password = s3cr3t
port = 3306
```

Only the `[client]` section is read; other sections (e.g. `[mysqldump]`) and `!include`/`!includedir` directives are ignored. Any explicit config field (`host`, `username`, `password_env`) always overrides the file's corresponding value — the file only fills in what's left unset. A missing, unreadable, or malformed `defaults_file` fails **at config-load time**, not partway through a backup run. Same group/world-readable permission warning as the `--password-file` channel above.

This is unrelated to (and can be combined with) `additional_args: "--defaults-extra-file=..."`, which forwards a raw flag to `mysqldump`/`mariadb-dump` directly — that dump-only passthrough still works exactly as before, but on its own it does not reach the Go-side connectivity check or discovery, which is exactly the gap `defaults_file` closes.

### 5. MongoDB secrets file — `mongo_secrets_file`

For MongoDB jobs only, `mongo_secrets_file` points at a small Sentinel-native YAML file supplying a password, a full connection URI, and/or a TLS private-key passphrase, composed into the job's connection string for the **whole** pipeline — the pre-backup connectivity check, database discovery, and the dump itself (spec 057 / PRD 45, closing GitHub issue #27).

```yaml
databases:
  mongo-prod:
    type: mongodb
    uri: "mongodb://appuser@mongo:27017/?replicaSet=rs0"   # user, no password
    mongo_secrets_file: /run/secrets/mongo.yaml            # supplies the password
```

```yaml
# /run/secrets/mongo.yaml  (chmod 0600)
password: "s3cr3t"
# uri: "mongodb://appuser:s3cr3t@mongo:27017/?replicaSet=rs0"   # alternative: a full URI
# ssl_pem_key_password: "pem-pass"                               # optional TLS key passphrase
```

Precedence is evaluated **per field**: an explicit `uri`/`uri_env` always wins and the file's `uri` is simply unused (not an error) when present; the file's `password` only fills a URI that has a username but no password — if the URI already has one, or no username is known anywhere, config loading fails immediately with a clear error rather than guessing. A missing, unreadable, or malformed `mongo_secrets_file` fails **at config-load time**, never partway through a backup run. Same group/world-readable permission warning as the channels above.

> **Note on the TLS passphrase field**: `ssl_pem_key_password` is resolved through the same mechanism as the existing `tls.client_key_password_env` option, but as of this writing neither reaches a live MongoDB TLS connection on the config-driven backup path — that is a separate, pre-existing gap in how TLS material is wired for Mongo dumps, unrelated to and not fixed by this feature. The value is captured and available for the day that wiring lands.

## Precedence

CLI flag > config `password_env` > `defaults_file` (MySQL/MariaDB only). The CLI-flag override is silent (no warning), enabling one-off drills:

```bash
export OPS_PWD='different'
sentinel backup --config sentinel.yaml --password-env OPS_PWD
```

## Errors you may encounter

- Multiple CLI password flags supplied:

  ```
  error: multiple password sources supplied on the command line (--password-env, --password-file); use exactly one of --password, --password-env, --password-file
  ```

- `--password-env` references an unset or empty variable:

  ```
  error: environment variable 'DOES_NOT_EXIST' referenced by --password-env is unset or empty
  ```

- `--password-file` path missing or unreadable:

  ```
  error: cannot read password file '/tmp/nope': open /tmp/nope: no such file or directory
  ```

- `--password-file` first line is empty after trim:

  ```
  error: password file '/tmp/empty' is empty after trimming the first line
  ```

## Kubernetes recipe

```yaml
spec:
  containers:
    - name: sentinel
      image: sentinel:1.x
      args:
        - backup
        - --config=/etc/sentinel/sentinel.yaml
      envFrom:
        - secretRef:
            name: sentinel-db-secrets   # provides DB_PWD
```

```yaml
# sentinel.yaml (mounted ConfigMap)
databases:
  primary:
    password_env: DB_PWD
```

No file, no flag, no argv leak.

## systemd recipe

```ini
# /etc/systemd/system/sentinel-backup.service
[Service]
Type=oneshot
EnvironmentFile=/etc/sentinel/secrets.env   # chmod 0600, contains DB_PWD=...
ExecStart=/usr/local/bin/sentinel backup --config /etc/sentinel/sentinel.yaml
```

## Argv guarantee

After the v1.x deprecation release, no Sentinel backup adapter writes the database password to subprocess argv:

| Engine | Password channel | Source file |
|--------|-----------------|-------------|
| PostgreSQL | `PGPASSWORD` env | `pkg/backup/pg_dump/pg_dump.go` |
| MySQL | `MYSQL_PWD` env | `pkg/backup/mysql_dump/mysql_dump.go` |
| MariaDB | `MYSQL_PWD` env | `pkg/backup/mariadb_dump/mariadb_dump.go` |
| MongoDB | URI userinfo | `pkg/backup/mongo_dump/` |

You can verify with `ps -o args= -p $(pgrep -f mariadb-dump)` during a backup; it must contain no `--password` or `password=` token.

## Deprecation: `--password`

The legacy `--password` / `-p` flag still functions in this release but emits a deprecation notice on stderr and will be removed in the next minor release. Migrate to one of the three channels above.
