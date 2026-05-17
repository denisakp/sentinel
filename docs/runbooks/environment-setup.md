# Runbook — Environment setup

- **Audience**: ops / SRE
- **Last reviewed**: 2026-05-17
- **Related**: [run-backup-from-config](./run-backup-from-config.md), [troubleshooting](./troubleshooting.md)

## When to use

Bootstrapping a fresh host (bare-metal, VM, or container) that will run `sentinel` against existing databases. Pick Option A for clean per-test isolation, Option B for a pre-baked image.

## Preconditions

- Docker available on the host.
- Target databases reachable from where Sentinel will run.
- Sentinel binary built (`sentinel-linux-amd64`) or pulled.
- Default DB stack ports free: 5432 (Postgres), 3307 (MySQL), 3306 (MariaDB), 27017 (MongoDB).

## Steps

### Start the database stack

```bash
docker compose -f infra/docker/docker-compose.yml up -d pgsql mysql mariadb
docker run -d --name sentinel-mongo -p 27017:27017 mongo:8.2
```

Exposed ports / creds:

| Service        | Host port | DB         | Credentials              |
|----------------|-----------|------------|--------------------------|
| PostgreSQL 16  | `5432`    | `sentinel` | `sentinel / sentinel`    |
| MySQL 8 LTS    | `3307`    | `sentinel` | `sentinel / sentinel`    |
| MariaDB 11     | `3306`    | `sentinel` | `sentinel / sentinel`    |
| MongoDB 8.2    | `27017`   | —          | no auth (dev)            |

### Option A — Ubuntu container (recommended)

1. Start an Ubuntu shell on the same Docker network.

```bash
docker run -it --rm \
  --name sentinel-tester \
  --add-host=host.docker.internal:host-gateway \
  -v "$PWD:/workspace" \
  ubuntu:24.04 bash
```

2. Install dump client tools inside the container.

```bash
apt-get update && apt-get install -y \
  ca-certificates curl gnupg wget \
  postgresql-client-18 \
  mysql-client

MARIADB_VERSION="11.8.6"
wget -q "https://downloads.mariadb.org/rest-api/mariadb/${MARIADB_VERSION}/mariadb-${MARIADB_VERSION}-linux-systemd-x86_64.tar.gz" \
    -O mariadb.tar.gz
tar -xzf mariadb.tar.gz \
    "mariadb-${MARIADB_VERSION}-linux-systemd-x86_64/bin/mariadb-dump" \
    --strip-components=2
mv mariadb-dump /usr/local/bin/
chmod +x /usr/local/bin/mariadb-dump
rm mariadb.tar.gz

wget -qO mongodb-database-tools.deb \
    https://fastdl.mongodb.org/tools/db/mongodb-database-tools-ubuntu2204-x86_64-100.14.1.deb
dpkg -i mongodb-database-tools.deb
rm mongodb-database-tools.deb
```

3. Install the Sentinel binary.

```bash
cp /workspace/sentinel-linux-amd64 /usr/local/bin/sentinel
chmod +x /usr/local/bin/sentinel
sentinel --help
```

4. Prepare working directories.

```bash
mkdir -p /workspace/backups /workspace/.sentinel
```

### Option B — Dev Docker image (Sentinel + clients bundled)

```bash
docker build -f infra/docker/Dockerfile.dev -t sentinel-dev:local .

docker run --rm \
  --add-host=host.docker.internal:host-gateway \
  -e DEV_POSTGRES_PASSWORD=sentinel \
  -e DEV_MYSQL_PASSWORD=sentinel \
  -e DEV_MARIADB_PASSWORD=sentinel \
  -v "$PWD:/workspace" \
  sentinel-dev:local \
  sh -lc 'sentinel backup --config /workspace/infra/dataset/local.yaml'
```

## Verification

```bash
pg_dump --version
mysqldump --version
mariadb-dump --version
mongodump --version
sentinel --help
```

Connectivity matrix (from inside container, `host.docker.internal` resolves to the host):

| Database   | host                   | port  | user       | password   | database   |
|------------|------------------------|-------|------------|------------|------------|
| PostgreSQL | `host.docker.internal` | 5432  | `sentinel` | `sentinel` | `sentinel` |
| MySQL      | `host.docker.internal` | 3307  | `sentinel` | `sentinel` | `sentinel` |
| MariaDB    | `host.docker.internal` | 3306  | `sentinel` | `sentinel` | `sentinel` |
| MongoDB    | `host.docker.internal` | 27017 | —          | —          | `sentinel` |

## Rollback / recovery

```bash
docker rm -f sentinel-mongo
docker compose -f infra/docker/docker-compose.yml down
```

## References

- `infra/docker/docker-compose.yml`, `infra/docker/Dockerfile.dev`
- [troubleshooting](./troubleshooting.md) for client-tool and connectivity errors
