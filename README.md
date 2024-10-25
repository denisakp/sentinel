# Sentinel

**Status: Project in Development 🚧**

Sentinel is an open-source, cloud-native database backup and restoration tool designed for seamless management of SQL
and NoSQL databases in Docker, Kubernetes, and local environments. Currently, Sentinel is under development, so certain
features are incomplete, and documentation will be continuously updated.

## Project Purpose

Sentinel aims to simplify the backup, restoration, and management of databases with support for local storage,
scheduled backups, secure encryption, and multiple notification channels. It's designed to provide database
administrators and developers with a flexible, reliable solution for database continuity.

## Key Features

- **Backup and Restoration** for SQL and NoSQL databases (PostgreSQL, MySQL, MariaDB, MongoDB).
- **Storage Support** for multiple environments, including local storage and upcoming support for cloud storage
  solutions.
- **Notification System** for real-time backup alerts (Slack, Google Chat, SMTP).
- **Scheduling and Automation** through cron jobs for regular, automated backups (upcoming).
- **Enhanced Security** with backup file encryption (AES 256) and integrity verification using hash checks (upcoming).
- **Cross-Platform Compatibility**: Built with Golang, Sentinel works seamlessly in Docker, Kubernetes, and other
  cloud-native environments.

## Features Overview

**Implemented Features**:

- [x] Backup functionality for PostgreSQL, MySQL, MariaDB, and MongoDB databases.
- [x] Local storage support for backups.

**Upcoming Features**:

- [ ] External Storage Options: S3 (including MinIO), Google Drive, Dropbox.
- [ ] Scheduled Backups: Automate backups on a defined schedule.
- [ ] Notifications: Real-time alerts for scheduled backup statuses.
- [ ] Security Enhancements: Hash verification and AES-256 encryption.
- [ ] Restoration: Easy restoration from existing backups.
- [ ] Scheduled Backup Monitoring: Track and manage automated backups effectively.

## Installation

At this stage, Sentinel has not yet reached an initial release, so the only way to use it is by cloning the repository
and building the project locally. A Go development environment (v1.18+) is required for building Sentinel.

1. **Clone the Repository**:
   ```bash
   git clone https://github.com/denisakp/sentinel.git
   cd sentinel
   ```

2. **Build the Project**:
   ```bash
   go mod download 
   go build -o sentinel
   ```

3. **Run Sentinel**:
   Sentinel's current focus is on the backup functionality. To create a backup of your PostgresSQL database, for
   example,
   run:
   ```bash
   ./sentinel backup --type postgres --host mydb.host.tld --port 5432 --user my-user --password 1234 --database sample
   ```
   Use `sentinel backup -h` or `--help` for more options.

> **Note**: When the initial release (v1.0) is available, this README will be updated with more user-friendly
> installation options and instructions.

## Usage

Currently, only the `backup` command is functional. It supports backup operations for:

- **PostgresSQL**
- **MySQL**
- **MariaDB**
- **MongoDB**

Example usage:

```bash
./sentinel backup --type mysql --host mydb.host.tld --port 3307 --user my-user --password 1234 --database sample
```

For additional options, run:

```bash
./sentinel backup -h
```

## Contributions

Sentinel is under active development, and we welcome contributions from the community! To get started, please review the
following resources:

- [CONTRIBUTING.md](CONTRIBUTING.md): Guidelines for contributing, including setup instructions, coding standards, and
  our pull request process.
- [SECURITY.md](SECURITY.md): Important information on reporting security vulnerabilities responsibly.
- **Issue Templates**:
    - [Feature Request](.github/ISSUE_TEMPLATE/2-feature-request.md): To suggest new features.
    - [Bug Report](.github/ISSUE_TEMPLATE/1-bug.md): To report bugs or issues.
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md): Community standards for respectful and inclusive collaboration.

Thank you for helping improve Sentinel!

Stay tuned for more updates, and thank you for your interest in making Sentinel a reliable tool for database continuity!

---

