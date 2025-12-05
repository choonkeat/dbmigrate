# Upgrade Guide

## Upgrading to v3.0.0

This release introduces cross-process locking, configurable transaction modes, and support for `CREATE INDEX CONCURRENTLY`.

### Breaking Changes

#### SQLite and CQL (Cassandra) users must add `-no-lock` flag

dbmigrate now requires cross-process locking by default. Since SQLite and CQL do not support advisory locks, users must explicitly opt out:

```bash
# Before
dbmigrate -up

# After
dbmigrate -up -no-lock
```

**Why this change?** Cross-process locking prevents race conditions when multiple processes run migrations concurrently (e.g., multi-node deployments). SQLite and CQL do not support advisory locks, so you must acknowledge this limitation explicitly.

**When is `-no-lock` safe?**
- Local development
- Single-node production where migrations run before app starts
- CI pipelines with a single migration job

**Update your scripts:**

```bash
# Shell scripts
dbmigrate -up -no-lock

# Makefiles
migrate:
    dbmigrate -up -no-lock

# Docker
CMD ["dbmigrate", "-up", "-no-lock"]

# CI (GitHub Actions, etc.)
- run: dbmigrate -up -no-lock
```

### New Features

#### Cross-process locking (PostgreSQL, MySQL)

Migrations now acquire an advisory lock before running. This prevents concurrent migration processes from corrupting your database.

- **PostgreSQL**: Uses `pg_advisory_lock`
- **MySQL**: Uses `GET_LOCK`
- **Behavior**: Waits indefinitely until lock is acquired (prints "Waiting for migration lock..." every 2 seconds)
- **Auto-release**: Lock is released when connection ends (crash-safe)

No action required for PostgreSQL or MySQL users—locking is automatic.

To disable locking (not recommended for production):
```bash
dbmigrate -up -no-lock
```

#### Transaction modes (`-db-txn-mode`)

Control how migrations are wrapped in transactions:

| Mode | Behavior |
|------|----------|
| `all` | (Default) All pending migrations in a single transaction |
| `per-file` | Each migration file in its own transaction |
| `none` | No transaction wrapping |

```bash
# Default: all-in-one transaction (same as before)
dbmigrate -up

# Per-file transactions
dbmigrate -up -db-txn-mode=per-file

# No transactions
dbmigrate -up -db-txn-mode=none
```

#### Support for `CREATE INDEX CONCURRENTLY` (`.no-db-txn.` marker)

PostgreSQL's `CREATE INDEX CONCURRENTLY` cannot run inside a transaction. To use it, add `.no-db-txn.` to your migration filename:

```
db/migrations/
  20240101120000_create_users.up.sql
  20240101120000_create_users.down.sql
  20240101130000_add_email_index.no-db-txn.up.sql      # No transaction
  20240101130000_add_email_index.no-db-txn.down.sql    # No transaction
```

**Important:** When using `.no-db-txn.` files, you must use `-db-txn-mode=per-file`:

```bash
dbmigrate -up -db-txn-mode=per-file
```

If you forget, dbmigrate will error with instructions:

```
Error: Cannot apply migrations in -db-txn-mode=all (default)

The following migrations require -db-txn-mode=per-file:
  - 20240101130000_add_email_index.no-db-txn.up.sql

Run with: dbmigrate -up -db-txn-mode=per-file
```

#### MySQL DDL warning

MySQL DDL statements (CREATE, ALTER, DROP) cause implicit commits, breaking transaction atomicity. dbmigrate now prints a warning:

```
Warning: MySQL does not support transactional DDL.
         DDL statements (CREATE, ALTER, DROP) commit implicitly.
         Transaction mode has limited effect on DDL-heavy migrations.
```

No action required—this is informational only.

### What Happens When You Upgrade

#### By Database and Scenario

| Database | Scenario | Upgrade Result | Blocked? | Action Required |
|----------|----------|----------------|----------|-----------------|
| **PostgreSQL** | Dev workflow | ✅ Works | No | None |
| **PostgreSQL** | CI | ✅ Works | No | None |
| **PostgreSQL** | Deployment (single node) | ✅ Works | No | None |
| **PostgreSQL** | Deployment (multi-node) | ✅ Works (safer!) | No | None |
| **MySQL** | Dev workflow | ✅ Works + Warning | No | None |
| **MySQL** | CI | ✅ Works + Warning | No | None |
| **MySQL** | Deployment | ✅ Works + Warning | No | None |
| **SQLite** | Dev workflow | ❌ Blocked | **Yes** | Add `-no-lock` |
| **SQLite** | CI | ❌ Blocked | **Yes** | Add `-no-lock` |
| **SQLite** | Deployment | ❌ Blocked | **Yes** | Add `-no-lock` |
| **CQL** | Dev workflow | ❌ Blocked | **Yes** | Add `-no-lock` |
| **CQL** | CI | ❌ Blocked | **Yes** | Add `-no-lock` |
| **CQL** | Deployment | ❌ Blocked | **Yes** | Add `-no-lock` |

#### By Workflow Type

| Workflow | PostgreSQL | MySQL | SQLite | CQL |
|----------|------------|-------|--------|-----|
| **Dev (local)** | No change | Warning printed | Add `-no-lock` | Add `-no-lock` |
| **CI pipeline** | No change | Warning in logs | Update CI script | Update CI script |
| **Docker/K8s** | No change | Warning printed | Update Dockerfile/manifest | Update Dockerfile/manifest |
| **Multi-node boot** | Improved (locking!) | Improved (locking!) | N/A (use `-no-lock`) | N/A (use `-no-lock`) |

### Upgrade Checklist

| Database | Action Required |
|----------|-----------------|
| PostgreSQL | None |
| MySQL | None (read the new DDL warning) |
| SQLite | Add `-no-lock` to all dbmigrate commands |
| CQL | Add `-no-lock` to all dbmigrate commands |

### Error Recovery

#### "sqlite3 does not support cross-process locking" / "cql does not support cross-process locking"

Add `-no-lock` flag:
```bash
dbmigrate -up -no-lock
```

#### "Cannot apply migrations in -db-txn-mode=all (default)"

You have `.no-db-txn.` files. Use per-file transaction mode:
```bash
dbmigrate -up -db-txn-mode=per-file
```

#### "Waiting for migration lock..." (hangs)

Another migration process is running. Either:
1. Wait for it to complete
2. Kill the other process (lock auto-releases)
3. Check for stuck connections: `SELECT * FROM pg_locks WHERE locktype = 'advisory';`

### FAQ

**Q: Will my existing migrations still work?**

Yes. If you're using PostgreSQL or MySQL, everything works as before with the added benefit of locking. SQLite and CQL users need to add `-no-lock`.

**Q: Do I need to change my migration files?**

No. Existing migration files work unchanged. The `.no-db-txn.` marker is only needed for new migrations that use `CREATE INDEX CONCURRENTLY` or similar non-transactional statements.

**Q: What if I'm already running migrations from a single process?**

Locking has minimal overhead. You can keep it enabled (recommended) or disable with `-no-lock`.

**Q: Can I use `-db-txn-mode=per-file` by default?**

Yes. It's slightly less atomic (partial progress on failure) but allows `.no-db-txn.` files and reduces lock duration for long-running migrations.
