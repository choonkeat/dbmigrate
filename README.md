# dbmigrate [![Build Status](https://travis-ci.com/choonkeat/dbmigrate.svg?branch=master)](https://travis-ci.com/choonkeat/dbmigrate)

`rails migrate` inspired approach to database schema migrations but with plain sql files. and much faster.

### Create a new migration

```
$ dbmigrate -create describe your change
2018/12/21 16:33:13 writing db/migrations/20181221083313_describe-your-change.up.sql
2018/12/21 16:33:13 writing db/migrations/20181221083313_describe-your-change.down.sql
```

generate a pair of blank `.up.sql` and `.down.sql` files inside the directory `db/migrations`. configure the directory with `-dir` command line flag.

the numeric prefix of the filename is the `version`. i.e. the version of the file above is `20181221083313`

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

## Demo

[![asciicast](https://asciinema.org/a/11onajScFBZFAutBwB8K4RENl.svg)](https://asciinema.org/a/11onajScFBZFAutBwB8K4RENl)

Running integration tests with postgres, mariadb, and mysql

[![asciicast](https://asciinema.org/a/E8ifl4p5v6lL44f6lRd4r1bed.svg)](https://asciinema.org/a/E8ifl4p5v6lL44f6lRd4r1bed)
