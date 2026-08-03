# SQL Migrations

This directory contains sequential, versioned SQL migration files embedded in the binary via Go embed.

## Naming Convention

Migrations follow the pattern: `NNN_description.sql`

Where:
- `NNN` is a zero-padded 3-digit sequential version number (001, 002, 003, ...)
- `description` is a brief snake_case summary of the migration

## Ordering

Migrations are applied in lexicographical order by filename. The version number ensures proper sequencing.

## Example

```
001_initial_schema.sql
002_add_cleanup_fields.sql
003_add_migration_tracking.sql
```

See [data-model.md](../../specs/001-v1-consolidation/data-model.md) for schema entity definitions.
