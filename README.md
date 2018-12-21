# dbmigrate

`rails migrate` inspired design

### create a new migration

```
$ dbmigrate -create describe your change
2018/12/21 16:33:13 writing db/migrations/20181221083313_describe-your-change.up.sql
2018/12/21 16:33:13 writing db/migrations/20181221083313_describe-your-change.down.sql
```

generate a pair of blank `.up.sql` and `.down.sql` files inside the directory `db/migrations`. configure the directory with `-dirname` command line flag.

the numeric prefix of the filename is the `version`. i.e. the version of the file above is `20181221083313`

### dbmigrate up

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

### dbmigrate down

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
