---
title: Setting up a practice environment
description: Bring up throwaway PostgreSQL, MySQL, MariaDB, and MongoDB containers and a host that has the client tools Sentinel drives.
sidebar_position: 2
---

Stand up a disposable set of databases and a host with the dump and restore tools installed, so you
can run every other guide against something you are allowed to break.

## When to use this

Use this when you are evaluating Sentinel, working through a tutorial, or rehearsing a procedure
that you do not want to try against production first. It is also the fastest way to reproduce a
problem for a bug report, because the resulting stack is identical for everyone.

Do not use it as a template for a production deployment. The containers below carry well-known
throwaway passwords, publish ports on the host, and store data in volumes that `make infra-down`
destroys. For a real install, follow [Installation](../intro/installation.md) and supply credentials
through one of the channels in [Supplying database credentials](./database-credentials.md).

## Before you start

- Docker with the Compose plugin, and `make`.
- A clone of the Sentinel repository. The compose files live in `infra/docker/` and are not shipped
  with the release binaries.
- These host ports free: `5432`, `3306`, `3307`, `27017`, and, if you want the extras, `8000`,
  `8001`, `9000`, `9001`.
- The client tools for the engines you intend to exercise, on the `PATH` of whatever runs
  `sentinel`. See the tool table in [Installation](../intro/installation.md). If you would rather not
  install them on your machine, step 3 gives you a container that already has them.

## Steps

### 1. Start the stack

From the repository root:

```bash
make infra-up
```

This one target does four things that are easy to get wrong by hand. It creates the external Docker
network named `sentinel` if it is missing, starts the `pgsql`, `mysql`, and `mariadb` services from
`infra/docker/docker-compose.yml`, waits for them to report healthy, then starts MongoDB as a
separate container named `sentinel-mongo` with the network alias `mongo`, and finally brings up the
Azurite and fake-gcs-server emulators used by the storage tests.

:::note MongoDB is not started by Compose
`docker-compose.yml` does define a `mongo` service, but it publishes no host port. The container you
actually connect to on `localhost:27017` is started by the Makefile with `docker run`. Running
`docker compose up` on its own will not give you a reachable MongoDB.
:::

You should see the network being created on a first run, then a readiness line per database:

```
[make] Creating network 'sentinel'...
[make] Starting DB stack (postgres, mysql, mariadb)...
[make] Waiting for DBs to be ready...
[make] Starting MongoDB...
[make] Starting Azurite + fake-gcs-server...
```

### 2. Note what is now listening

| Service | Image | Host port | Database | Username | Password source |
|---|---|---|---|---|---|
| PostgreSQL | `postgres:17-alpine` | `5432` | `sentinel` | `sentinel` | `POSTGRES_PASSWORD` in the compose file |
| MySQL | `mysql:lts` | `3307` | `sentinel` | `sentinel` | `MYSQL_PASSWORD` in the compose file |
| MariaDB | `mariadb:lts` | `3306` | `sentinel` | `sentinel` | `MYSQL_PASSWORD` in the compose file |
| MongoDB | `mongo:8.2` | `27017` | n/a | n/a | no authentication |

PostgreSQL is pinned to 17 deliberately. WAL-based incremental backup needs PostgreSQL 17 or later,
and the client tools in the dev image are new enough to emit settings that older servers reject.

Three optional services come with the compose file and are not started by `make infra-up`:
`phpmyadmin` on `8000`, `pgadmin` on `8001`, and `rustfs`, an S3-compatible object store, on `9000`
and `9001`. Start any of them with `docker compose -f infra/docker/docker-compose.yml up -d <name>`
once the network exists.

### 3. Get a host with the client tools

If `pg_dump`, `mysqldump`, `mariadb-dump`, and `mongodump` are already on your `PATH`, skip to step 4
and run `sentinel` directly against the ports above.

Otherwise build the development image, which bundles the Sentinel binary from your working tree with
all four client tool sets:

```bash
make build-image
```

Then open a shell on the same Docker network, so the databases are reachable by their service names
rather than by published ports:

```bash
docker run --rm -it \
  --network sentinel \
  -v "$PWD:/workspace" \
  -w /workspace \
  --entrypoint sh \
  sentinel-dev:local
```

Inside that container the hostnames are `pgdb`, `mysql`, `maria`, and `mongo`, and every database
listens on its own default port, so MySQL is `3306` there rather than the `3307` published on your
host.

### 4. Write a configuration and point it at the stack

Create `sentinel.yaml` somewhere in your working tree. Passwords are named, never inlined:

```yaml
version: "1.0"

databases:
  practice-postgres:
    type: postgres
    host: 127.0.0.1
    port: 5432
    username: sentinel
    password_env: PRACTICE_PG_PASSWORD
    database: sentinel
    output: practice-postgres.sql
    storage:
      type: local
      local_path: ./backups
```

Use the container hostnames instead of `127.0.0.1` if you are running from the development image.
Export the variable in the same shell that runs Sentinel:

```bash
export PRACTICE_PG_PASSWORD='<the value of POSTGRES_PASSWORD in infra/docker/docker-compose.yml>'
mkdir -p ./backups
```

## Verify

Confirm the binary can see the tools it needs. Anything you plan to back up must read `available`:

```bash
sentinel version --tools
```

```
Tools:
  pg_dump        available  pg_dump (PostgreSQL) 17.x
  mysqldump      available  mysqldump  Ver 8.x
  mariadb-dump   available  mariadb-dump from 11.x
  mongodump      available  mongodump version: 100.x
```

Confirm the configuration parses. This check is structural and makes no database connection:

```bash
sentinel config validate --config sentinel.yaml
```

Then take a real backup, which is the only check that proves connectivity, credentials, and the dump
tool all line up at once:

```bash
sentinel backup --config sentinel.yaml
ls -l ./backups
```

## If it goes wrong

**`network sentinel declared as external, but could not be found`.** You ran `docker compose` by
hand before the network existed. Run `make infra-up`, or create it once with
`docker network create sentinel`.

**A port is already allocated.** Something else on your machine holds `5432`, `3306`, `3307`, or
`27017`. Stop it, or edit the `ports:` mapping in `infra/docker/docker-compose.yml`. Note the
deliberate asymmetry: MySQL is published on `3307` precisely so that it can coexist with MariaDB on
`3306`.

**`connection refused` on MongoDB.** The `sentinel-mongo` container is not running.
`docker ps --filter name=sentinel-mongo` will tell you. Starting the compose `mongo` service instead
will not help, because it publishes no host port.

**A dump fails with `executable file not found`.** The client tool for that engine is missing on the
host running Sentinel, not in the database container. Check `sentinel version --tools` and use the
development image from step 3 if you would rather not install them.

**Tearing everything down.** `make infra-down` stops the database stack, the emulators, and the
MongoDB container. `make clean` does that and also removes the `.e2e` workspace.

:::danger Destructive
`make infra-down` removes the emulator volumes, and the compose volumes hold every database you
created here. Nothing in this environment is meant to survive; do not point these commands at a
compose project you care about.
:::

## Related

- [Installation](../intro/installation.md): installing the release binary and verifying it.
- [Quickstart](../intro/quickstart.md): the shortest path from an installed binary to a backup.
- [Supplying database credentials](./database-credentials.md): every channel Sentinel accepts a
  password through.
- [Your first PostgreSQL backup](../tutorials/postgres/first-backup.md): the guided walkthrough this
  environment is built for.
- [Configuration reference](../reference/configuration.md): every YAML key used above.

{/* sources: infra/docker/docker-compose.yml, infra/docker/Dockerfile.dev, Makefile, internal/config/types.go, internal/cli/version.go, internal/cli/config.go, docs/runbooks/environment-setup.md */}
