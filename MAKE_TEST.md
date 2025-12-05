# Running `make test`

## Prerequisites

### Docker Setup

1. **Docker must be running** - The tests use Docker containers for each database driver.

2. **Fix Docker credential helper** (if needed) - If you see this error:
   ```
   docker: error getting credentials - err: exec: "docker-credential-osxkeychain": executable file not found in $PATH
   ```

   Edit `~/.docker/config.json` and remove the `credsStore` line:
   ```json
   {
     "auths": {},
     "currentContext": "colima"
   }
   ```

3. **Ensure port 65500 is free** - All tests use port 65500. The test script automatically stops any container using this port before starting, but you can check manually with:
   ```bash
   lsof -i :65500
   ```

## Database Drivers Tested

The Makefile tests these drivers in order:
1. `cql` (Cassandra)
2. `sqlite3`
3. `postgres`
4. `mariadb` (uses mysql driver)
5. `mysql` (MySQL 9.x with `caching_sha2_password` authentication)

## Running Tests

```bash
# Full test suite (default 180s timeout)
make test

# Test a single driver
DATABASE_DRIVER=postgres bash -euxo pipefail tests/withdb.sh tests/scenario.sh

# Test with shorter timeout for faster iteration
SERVER_READY=60s DATABASE_DRIVER=mariadb bash -euxo pipefail tests/withdb.sh tests/scenario.sh

# Test without Docker (sqlite3 only)
DATABASE_DRIVER=sqlite3 bash -euxo pipefail tests/withdb.sh tests/scenario.sh
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_DRIVER` | (required) | Which driver to test: `cql`, `sqlite3`, `postgres`, `mariadb`, `mysql` |
| `SERVER_READY` | `180s` | How long to wait for database to be ready |

## Container Startup Times

Different databases take different amounts of time to start:

| Database   | Typical Startup Time | Notes |
|------------|---------------------|-------|
| Cassandra  | 45-60 seconds       | Slowest to start |
| PostgreSQL | 5-10 seconds        | Fast |
| MariaDB    | 5-10 seconds        | Fast |
| MySQL 9    | 5-10 seconds        | Fast |
| SQLite     | Instant             | No container needed |

The tests use `-server-ready ${SERVER_READY}` (default 180s) to wait for database readiness. This gives plenty of headroom for slow container starts.

## Automatic Cleanup

The test script automatically:
1. **Stops any container using port 65500** before starting a new one
2. **Cleans up containers on exit** using the `--rm` flag

You generally don't need to manually clean up between test runs.

## Verifying Test Results

Look for `[PASS]` markers in the output. A successful run shows:
```
[PASS] should create new migration
[PASS] should create .up.sql
[PASS] should create .down.sql
[PASS] match tests/db/<driver>/VERSIONS-01.before-fail
[PASS] should fail with bad migration
[PASS] match tests/db/<driver>/VERSIONS-02.after-fail
[PASS] should retry fixed migration and succeed
[PASS] match tests/db/<driver>/VERSIONS-03.after-fix-retry
[PASS] match tests/db/<driver>/VERSIONS-04.before-missing
[PASS] should run missing, older migration
[PASS] match tests/db/<driver>/VERSIONS-05.after-missing
[PASS] should migrate down by 1
[PASS] match tests/db/<driver>/VERSIONS-06.after-down-1
[PASS] should migrate down until nothing
[PASS] match tests/db/<driver>/VERSIONS-07.after-down-999
```

## Tips for Faster Iteration

1. **Pull images first** to avoid download delays during tests:
   ```bash
   docker pull cassandra
   docker pull postgres
   docker pull mariadb
   docker pull mysql
   ```

2. **Test one driver at a time** during development:
   ```bash
   DATABASE_DRIVER=sqlite3 bash -euxo pipefail tests/withdb.sh tests/scenario.sh
   ```

3. **Use shorter timeout** for databases that start quickly:
   ```bash
   SERVER_READY=60s DATABASE_DRIVER=postgres bash -euxo pipefail tests/withdb.sh tests/scenario.sh
   ```

4. **Skip slow drivers** - Cassandra is the slowest. For quick iteration, test sqlite3 and postgres first.

## Troubleshooting

### Container won't start

If a container fails to start, check Docker logs:
```bash
docker logs $(cat cid.txt)
```

### Manual cleanup

If tests fail mid-run and leave containers running:
```bash
docker ps -q --filter "publish=65500" | xargs -r docker stop
rm -f cid.txt
```
