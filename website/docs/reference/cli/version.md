---
title: sentinel version
description: Reference for sentinel version, its JSON envelope field by field, external tool probing, and the dev/unknown fallback.
sidebar_position: 12
---

Prints the build metadata stamped into the binary, and optionally probes the external database client binaries Sentinel shells out to.

## Synopsis

```text
sentinel version [flags]
sentinel --version
```

`sentinel version` has no subcommands. It accepts, and silently ignores, positional arguments: `sentinel version foo` behaves exactly like `sentinel version`.

## Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--format` | string | `text` | Output format: `text` or `json`. Any other value is rejected before anything is printed. |
| `-h`, `--help` | bool | `false` | Print help for `version`. |
| `--tools` | bool | `false` | Additionally probe `pg_dump`, `mysqldump`, `mariadb-dump`, and `mongodump` on `PATH` and report each one's version. |

The root command's `-v`, `--version` flag is a separate, much smaller thing: it prints one line, `sentinel <version>`, and nothing else. Use the `version` subcommand when you want commit, build date, or Go runtime.

## Text output

```bash
sentinel version
```

```text
Version:    dev
Commit:     unknown
Build Date: unknown
Go Version: go1.26.4
```

Four fixed labels, in this order, one per line, each padded to a stable column. The block is deterministic and safe to diff between runs.

With `--tools`, a blank line and a `Tools:` section follow, one line per tool in a fixed registry order that never changes across releases:

```bash
sentinel version --tools
```

```text
Version:    dev
Commit:     unknown
Build Date: unknown
Go Version: go1.26.4

Tools:
  pg_dump        missing
  mysqldump      missing
  mariadb-dump   missing
  mongodump      available  mongodump version: 100.16.1
```

## JSON output

```bash
sentinel version --format json
```

```json
{
  "sentinel": {
    "version": "dev",
    "commit": "unknown",
    "build_date": "unknown",
    "go_version": "go1.26.4"
  }
}
```

### The `sentinel` object

Always present, always with all four keys.

| Field | Type | Source | Notes |
|---|---|---|---|
| `version` | string | `-ldflags` stamp of `internal/version.Version` | The release tag on an official build, for example `v1.4.0`. Falls back to `dev`. |
| `commit` | string | `-ldflags` stamp of `internal/version.Commit` | Full commit SHA on an official build. Falls back to `unknown`. |
| `build_date` | string | `-ldflags` stamp of `internal/version.BuildDate` | The commit date of the tagged build. Falls back to `unknown`. |
| `go_version` | string | `runtime.Version()` | Never stamped, never empty. Read from the Go runtime the binary was compiled with, so it is trustworthy on every build. |

### The `tools` array

Present only with `--tools`. Omitted entirely, not emitted as `null` or `[]`, when tool probing was not requested.

```bash
sentinel version --tools --format json
```

```json
{
  "sentinel": {
    "version": "dev",
    "commit": "unknown",
    "build_date": "unknown",
    "go_version": "go1.26.4"
  },
  "tools": [
    { "name": "pg_dump", "state": "missing" },
    { "name": "mysqldump", "state": "missing" },
    { "name": "mariadb-dump", "state": "missing" },
    { "name": "mongodump", "state": "available", "version_line": "mongodump version: 100.16.1" }
  ]
}
```

| Field | Type | Present when | Description |
|---|---|---|---|
| `name` | string | Always | The binary name, exactly as it is looked up on `PATH`. |
| `state` | string | Always | `available`, `missing`, or `error`. |
| `version_line` | string | `state` is `available` | The first non-empty line of the tool's `--version` output, trimmed. |
| `error` | string | `state` is `error` | The failure detail, with the tool's own first output line appended when it produced one. |

The array always has exactly four entries, in registry order: `pg_dump`, `mysqldump`, `mariadb-dump`, `mongodump`. Order is guaranteed stable, so an index-based consumer will not break across releases.

| State | Meaning |
|---|---|
| `available` | Found on `PATH`, `--version` exited 0, and produced a usable first line. |
| `missing` | Not found on `PATH`. This is not an error and does not change the exit code; you only need the client for the engines you actually back up. |
| `error` | Found on `PATH` but `--version` exited non-zero, timed out, or produced no output. Usually a broken install or an architecture mismatch. |

Each tool is probed independently in its own subprocess with a 5-second timeout, so one hung or broken client never prevents the other three from being reported.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Metadata printed, whatever the tool states are. `missing` and `error` tools do not affect it. |
| `1` | `--format` was given a value other than `text` or `json`. |

```bash
sentinel version --format yaml
```

```text
Error: unsupported format "yaml": must be one of: text, json
```

The format is validated before any metadata is gathered, so a bad format never produces partial output.

## Why an installed binary reports `dev / unknown / unknown`

`version`, `commit`, and `build_date` are not computed at runtime. They are package-level variables in `internal/version` that a release build overwrites with `-ldflags -X`. When nothing overwrites them, they keep their compiled-in defaults: `dev`, `unknown`, `unknown`.

That is the expected output for:

- `go install github.com/denisakp/sentinel@latest`, which passes no `-ldflags`.
- `go build ./...` or `make` in a local checkout.
- Any build from source that does not reproduce the release linker flags.

It is **not** a sign of a corrupt or partial installation, and it does not mean you are running a pre-release. A `go install` of the `v1.4.0` tag is byte-for-byte the `v1.4.0` source and still prints `dev`.

`go_version` is stamped by nobody and read from `runtime.Version()`, which is why it shows a real value even in the fallback case.

:::note Reporting a bug from a `go install` build
`sentinel version` cannot tell maintainers which commit you are on when it prints `unknown`. Include the module version you installed, or the output of `go version -m $(which sentinel)`, which reads the Go module metadata the linker embeds regardless of `-ldflags`.
:::

To get real metadata, use an official release binary or container image, where the release pipeline stamps the tag, the full commit SHA, and the commit date. See [installing Sentinel](../../intro/installation.md).

## Examples

Human-readable metadata:

```bash
sentinel version
```

Machine-readable metadata for a CI step:

```bash
sentinel version --format json | jq -r '.sentinel.version'
```

Prints the version string alone. On a `go install` build this prints `dev`.

Check that the database clients Sentinel needs are present before the first scheduled run:

```bash
sentinel version --tools --format json | jq -r '.tools[] | select(.state != "available") | .name'
```

Prints the name of every client that is missing or broken. An empty result means all four are usable. Sentinel only invokes the client matching each job's engine, so a non-empty result matters only if it names a client you actually use.

Fail a container image build when the PostgreSQL client did not make it into the final layer:

```bash
sentinel version --tools --format json \
  | jq -e '.tools[] | select(.name == "pg_dump") | .state == "available"' > /dev/null
```

## Related

- [Installing Sentinel](../../intro/installation.md)
- [`sentinel config`](./config.md)
- [`sentinel db`](./db.md)
- [CLI reference index](./index.md)
- [Architecture overview](../../intro/architecture-overview.md)

{/* sources: internal/cli/version.go, internal/cli/root.go, internal/version/metadata.go, internal/version/format.go, internal/version/tools.go, .goreleaser.yaml */}
