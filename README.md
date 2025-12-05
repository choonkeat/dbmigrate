# dbmigrate [![Build Status](https://travis-ci.com/choonkeat/dbmigrate.svg?branch=master)](https://travis-ci.com/choonkeat/dbmigrate)

`rails migrate` [inspired](https://blog.choonkeat.com/weblog/2019/05/database-schema-migration.html) approach to database schema migrations but with plain sql files. and much faster.

## Getting started with docker-compose

### Given a working app

Let's say we have a simple docker-compose project setup with only a `docker-compose.yml` file

```
$ tree .
.
└── docker-compose.yml

0 directories, 1 file
```

That declares a postgres database (`mydb`) and an app (`myapp`) that uses the database somehow (in our contrived example, we're just listing the tables in our database with `\dt`)


``` yaml
version: '3'
services:
    mydb:
        image: "postgres"
        environment:
            - POSTGRES_DB=myapp_development
            - POSTGRES_USER=myuser
            - POSTGRES_PASSWORD=topsecret

    myapp:
        image: "postgres"
        command: >-
            sh -c '
                until pg_isready --host=mydb --port=5432 --timeout=30; do sleep 1; done;
                psql postgres://myuser:topsecret@mydb:5432/myapp_development -c \\dt
            '
        depends_on:
            - mydb
```

Let's see how our app runs

```
$ docker-compose up myapp
Creating dbmigrate-example_mydb_1 ... done
Creating dbmigrate-example_myapp_1 ... done
Attaching to dbmigrate-example_myapp_1
myapp_1  | mydb:5432 - no response
myapp_1  | mydb:5432 - no response
myapp_1  | mydb:5432 - accepting connections
myapp_1  | Did not find any relations.
dbmigrate-example_myapp_1 exited with code 0
```

The output simply shows that
- our `myapp` waited until `mydb:5432` was ready to accept connections
- then it listed the tables inside the database and got `Did not find any relations` (which means no tables)

### Add `dbmigrate`

Now let's add `dbmigrate` to our docker-compose.yml (take special note that `myapp` has a new entry under `depends_on` also)

``` yaml
version: '3'
services:
    mydb:
        image: "postgres"
        environment:
            - POSTGRES_DB=myapp_development
            - POSTGRES_USER=myuser
            - POSTGRES_PASSWORD=topsecret

    myapp:
        image: "postgres"
        command: >-
            sh -c '
                until pg_isready --host=mydb --port=5432 --timeout=30; do sleep 1; done;
                psql postgres://myuser:topsecret@mydb:5432/myapp_development -c \\dt
            '
        depends_on:
            - mydb
            - dbmigrate

    # by default apply migrations with `-up` flag
    # we can run different commands by overwriting `DBMIGRATE_CMD` env
    # try DBMIGRATE_CMD="-h" to see what other flags dbmigrate can offer
    dbmigrate:
        image: "choonkeat/dbmigrate"
        environment:
            - DATABASE_URL=postgres://myuser:topsecret@mydb:5432/myapp_development?sslmode=disable
        volumes:
            - .:/app
        working_dir: /app
        command: ${DBMIGRATE_CMD:--up -server-ready 60s -create-db}
        depends_on:
            - mydb
```

And we can start creating a few migration scripts by running `docker-compose up dbmigrate` with a custom `DBMIGRATE_CMD` env variable

```
$ DBMIGRATE_CMD="-create users" docker-compose up dbmigrate
dbmigrate-example_mydb_1 is up-to-date
Creating dbmigrate-example_dbmigrate_1 ... done
Attaching to dbmigrate-example_dbmigrate_1
dbmigrate_1  | 2019/06/01 08:58:05 writing db/migrations/20190601085805_users.up.sql
dbmigrate_1  | 2019/06/01 08:58:05 writing db/migrations/20190601085805_users.down.sql
dbmigrate-example_dbmigrate_1 exited with code 0
```

```
$ DBMIGRATE_CMD="-create blogs" docker-compose up dbmigrate
dbmigrate-example_mydb_1 is up-to-date
Recreating dbmigrate-example_dbmigrate_1 ... done
Attaching to dbmigrate-example_dbmigrate_1
dbmigrate_1  | 2019/06/01 08:58:14 writing db/migrations/20190601085814_blogs.up.sql
dbmigrate_1  | 2019/06/01 08:58:14 writing db/migrations/20190601085814_blogs.down.sql
dbmigrate-example_dbmigrate_1 exited with code 0
```

After running those 2 commands, we see that we've generated 2 pairs of _empty_ `*.up.sql` and `*.down.sql` files.

```
$ tree .
.
├── db
│   └── migrations
│       ├── 20190601085805_users.down.sql
│       ├── 20190601085805_users.up.sql
│       ├── 20190601085814_blogs.down.sql
│       └── 20190601085814_blogs.up.sql
└── docker-compose.yml

2 directories, 5 files
```

We add our SQLs into our respective files

``` sql
-- db/migrations/20190601085805_users.up.sql
CREATE TABLE IF NOT EXISTS users (
    "id" SERIAL primary key
);
```

``` sql
-- db/migrations/20190601085805_users.down.sql
DROP TABLE IF EXISTS users;
```

``` sql
-- db/migrations/20190601085814_blogs.up.sql
CREATE TABLE IF NOT EXISTS blogs (
    "id" SERIAL primary key
);
```

``` sql
-- db/migrations/20190601085814_blogs.down.sql
DROP TABLE IF EXISTS blogs;
```

### Database schema migrations is now managed

Let's see how our app runs after those changes

```
$ docker-compose up myapp
dbmigrate-example_mydb_1 is up-to-date
Recreating dbmigrate-example_dbmigrate_1 ... done
Recreating dbmigrate-example_myapp_1     ... done
Attaching to dbmigrate-example_myapp_1
myapp_1      | mydb:5432 - accepting connections
myapp_1      |               List of relations
myapp_1      |  Schema |        Name        | Type  | Owner  
myapp_1      | --------+--------------------+-------+--------
myapp_1      |  public | blogs              | table | myuser
myapp_1      |  public | dbmigrate_versions | table | myuser
myapp_1      |  public | users              | table | myuser
myapp_1      | (3 rows)
myapp_1      |
dbmigrate-example_myapp_1 exited with code 0
```

Hey, looks like we have 3 tables now:

1. `blogs` created by our `db/migrations/20190601085814_blogs.up.sql`
1. `users` created by our `db/migrations/20190601085805_users.up.sql`
1. `dbmigrate_versions` created by `dbmigrate` for itself to track migration history.
    - every time `dbmigrate` runs, it checks `dbmigrate_versions` table to know which files in `db/migrations` had been applied and skip them; which files have not been seen before and apply them

We can look at the logs of the `dbmigrate` container to see what had happened when `myapp` booted up just now

```
$ docker-compose logs dbmigrate
Attaching to dbmigrate-example_dbmigrate_1
dbmigrate_1  | 2019/06/01 08:59:32 [up] 20190601085805_users.up.sql
dbmigrate_1  | 2019/06/01 08:59:32 [up] 20190601085814_blogs.up.sql
```

### That's it

Now everytime `myapp` starts up, since it declares `depends_on: dbmigrate`, our `dbmigrate` container will be run automatically to apply any new migration files in `db/migrations`. To add new migration files there, just run `docker-compose up dbmigrate` with a custom `DBMIGRATE_CMD` env variable (see above)

---

## Basic operations

### Create a new migration

```
$ dbmigrate -create describe your change
2018/12/21 16:33:13 writing db/migrations/20181221083313_describe-your-change.up.sql
2018/12/21 16:33:13 writing db/migrations/20181221083313_describe-your-change.down.sql
```

generate a pair of blank `.up.sql` and `.down.sql` files inside the directory `db/migrations`. configure the directory with `-dir` command line flag.

the numeric prefix of the filename is the `version`. i.e. the version of the file above is `20181221083313`

### Create a non-transactional migration

For operations that cannot run inside a transaction (e.g., `CREATE INDEX CONCURRENTLY` in PostgreSQL), use the `-create-no-db-txn` flag:

```
$ dbmigrate -create-no-db-txn add index concurrently
2024/01/15 10:30:00 writing db/migrations/20240115103000_add-index-concurrently.no-db-txn.up.sql
2024/01/15 10:30:00 writing db/migrations/20240115103000_add-index-concurrently.no-db-txn.down.sql
```

Files with `.no-db-txn.` in the filename will be executed outside of any transaction, regardless of the `-db-txn-mode` setting.

### Enforce CLI version

To ensure all team members use the same dbmigrate version, add `-wanted-cli-version` to your migration commands:

```
$ dbmigrate -wanted-cli-version 3.0.0 -up
2024/01/15 10:30:00 [up] 20240115103000_add-users.up.sql
```

If someone has a different version installed, the command fails immediately:

```
$ dbmigrate -wanted-cli-version 3.0.0 -up
2024/01/15 10:30:00 version mismatch: wanted "3.0.0" but binary is "2.2.2"
```

This is useful in CI/CD pipelines or team environments where version consistency matters.

### Migrate up

```
$ dbmigrate -up
2018/12/21 16:37:40 [up] 20181221083313_describe-your-change.up.sql
2018/12/21 16:37:40 [up] 20181221083727_more-changes.up.sql
```

1. Connect to database (defaults to the value of `DATABASE_URL` env; configure with `-url`)
1. Start a db transaction
1. Pick up each `.up.sql` file in `db/migrations` and iterate through them in ascending order
    - if the file `version` is found in `dbmigrate_versions` table, skip it
    - otherwise, execute the sql statements in the file
    - if it succeeds, insert an entry into `dbmigrate_versions` table
      ``` sql
      CREATE TABLE dbmigrate_versions (
        version text NOT NULL PRIMARY KEY
      );
      ```
    - if fail, rollback the entire transaction and exit 1
1. Commit db transaction and exit 0

### Migrate down

```
$ dbmigrate -down 1
2018/12/21 16:42:24 [down] 20181221083727_more-changes.down.sql
```

1. Connect to database (defaults to the value of `DATABASE_URL` env; configure with `-url`)
1. Start a db transaction
1. Pick up each `.down.sql` file in `db/migrations` and iterate through them in descending order
    - if the file `version` is NOT found in `dbmigrate_versions` table, skip it
    - otherwise, execute the sql statements in the file
    - if succeeds, remove the entry `WHERE version = ?` from `dbmigrate_versions` table
    - if fail, rollback the entire transaction and exit 1
1. Commit db transaction and exit 0

You can migrate "down" more files by using a different number

```
$ dbmigrate -down 3
2018/12/21 16:46:45 [down] 20181221083313_describe-your-change.down.sql
2018/12/21 16:46:45 [down] 20181221055307_create-users.down.sql
2018/12/21 16:46:45 [down] 20181221055304_create-projects.down.sql
```

### Show versions pending

Prints a sorted list of versions found in `-dir` but does not have a record in `dbmigrate_versions` table.

```
$ dbmigrate -versions-pending
20181222073750
20181222073900
20181222073901
```

### Configuring `DATABASE_URL`

**PostgreSQL**

We're using [github.com/lib/pq](https://godoc.org/github.com/lib/pq) so environment variable look like this

```
DATABASE_URL=postgres://user:password@localhost:5432/dbmigrate_test?sslmode=disable
```

or

```
DATABASE_DRIVER=postgres
DATABASE_URL='user=pqgotest dbname=pqgotest sslmode=verify-full'
```

> NOTE: out of the box, this driver supports having multiple statements in one `.sql` file.

**MySQL**

We're using [github.com/go-sql-driver/mysql](https://github.com/go-sql-driver/mysql#examples) so environment variables look like

```
DATABASE_DRIVER=mysql
DATABASE_URL='user:password@tcp(127.0.0.1:3306)/dbmigrate_test'
```

> NOTE: to have multiple SQL statements in each `.sql` file, you'd need to add `multiStatements=true` to the CGI query string of your `DATABASE_URL`. i.e.
>
> ```
> DATABASE_URL='user:password@tcp(127.0.0.1:3306)/dbmigrate_test?multiStatements=true'
> ```
>
> See the [driver documentation](https://github.com/go-sql-driver/mysql#multistatements) for details and other available options.

## Handling failure

When there's an error, we rollback the entire transaction. So you can edit your faulty `.sql` file and simply re-run

```
$ dbmigrate -up
2018/12/21 16:55:41 20181221083313_describe-your-change.up.sql: pq: relation "users" already exists
exit status 1
$ vim db/migrations/20181221083313_describe-your-change.up.sql
$ dbmigrate -up
2018/12/21 16:56:05 [up] 20181221083313_describe-your-change.up.sql
2018/12/21 16:56:05 [up] 20181221083727_more-changes.up.sql
$ dbmigrate -up
$
```

**PostgreSQL** supports rollback for most data definition language (DDL)

> one of the more advanced features of PostgreSQL is its ability to perform transactional DDL via its Write-Ahead Log design. This design supports backing out even large changes to DDL, such as table creation. You can't recover from an add/drop on a database or tablespace, but all other catalog operations are reversible.
> https://wiki.postgresql.org/wiki/Transactional_DDL_in_PostgreSQL:_A_Competitive_Analysis

**MySQL** does not support rollback for DDL

> Some statements cannot be rolled back. In general, these include data definition language (DDL) statements, such as those that create or drop databases, those that create, drop, or alter tables or stored routines.
> https://dev.mysql.com/doc/refman/8.0/en/cannot-roll-back.html

If you're using MySQL, make sure to have DDL (e.g. `CREATE TABLE ...`) in their individual `*.sql` files.

### Caveat: `-create-db` and database names

The SQL command `CREATE DATABASE <dbname>` does not work well (at least in postgres) if `<dbname>` contains dashes `-`. The proper way would've been to [quote](https://godoc.org/github.com/lib/pq#QuoteIdentifier) the value [when using it](https://github.com/choonkeat/dbmigrate/blob/5397b58246f8dfbfaf97897520eb8a9fdc5f129f/cmd/dbmigrate/main.go#L101) but alas there doesn't seem to be a driver agnostic way to quote that string [in Go](https://godoc.org/database/sql).

The workaround is, if `-create-db` is needed, use underscore `_` for your dbname instead of dashes `-`

---

## Upgrading from v2.x to v3.0.0

v3.0.0 introduces cross-process locking, configurable transaction modes, and support for `CREATE INDEX CONCURRENTLY`.

### What's Changed

**PostgreSQL and MySQL users**: No changes required—everything works as before, with automatic locking.

**SQLite and CQL users** need a small update:
- CLI: Add `-no-lock` flag (e.g., `dbmigrate -up -no-lock`)
- Library: Use `MigrateUpWithMode`/`MigrateDownWithMode` with `noLock=true`

**Custom adapter authors**: Add `SupportsLocking`, `AcquireLock`, `ReleaseLock` fields.

### New Features

- **Cross-process locking** (PostgreSQL, MySQL): Prevents race conditions when multiple processes run migrations
- **Transaction modes** (`-db-txn-mode`): Control transaction wrapping (`all`, `per-file`, `none`)
- **`.no-db-txn.` marker**: Support for `CREATE INDEX CONCURRENTLY` and other non-transactional DDL
- **MySQL DDL warning**: Informational warning about MySQL's implicit commits

**⚠️ Please read [UPGRADE.md](UPGRADE.md) for complete upgrade instructions, migration scenarios, error recovery, and FAQ.**

