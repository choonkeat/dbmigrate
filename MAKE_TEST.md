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

3. **Ensure port 65500 is free** - All tests use port 65500. Check with:
   ```bash
   lsof -i :65500
   ```

4. **Clean up stale containers** before running tests:
   ```bash
   docker ps -q | xargs -r docker stop
   docker ps -aq | xargs -r docker rm
   ```

## Database Drivers Tested

The Makefile tests these drivers in order:
1. `cql` (Cassandra)
2. `sqlite3`
3. `postgres`
4. `mariadb` (uses mysql driver)
5. `mysql`

## Running Tests

```bash
# Full test suite
make test

# Test a single driver
DATABASE_DRIVER=postgres bash -euxo pipefail tests/withdb.sh tests/scenario.sh

# Test without Docker (sqlite3 only)
DATABASE_DRIVER=sqlite3 bash -euxo pipefail tests/withdb.sh tests/scenario.sh
```

## Container Startup Times

Different databases take different amounts of time to start:

| Database   | Typical Startup Time | Notes |
|------------|---------------------|-------|
| Cassandra  | 45-60 seconds       | Slowest to start |
| PostgreSQL | 5-10 seconds        | Fast |
| MariaDB    | 10-20 seconds       | Medium |
| MySQL      | 10-20 seconds       | Medium |
| SQLite     | Instant             | No container needed |

The tests use `-server-ready 60s` to wait for database readiness.

## Known Issues

### Flaky Tests Due to Docker Timing

Tests may fail intermittently if Docker containers don't start in time. Common symptoms:
- "connection refused" errors
- Timeout after 60 seconds waiting for database

**Solution**: Re-run the tests. Ensure no other containers are using port 65500.

### Nil Pointer Panic on Timeout

If the database connection times out, you may see:
```
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x2 addr=0x18 pc=0x...]

goroutine 1 [running]:
main._main()
    .../cmd/dbmigrate/main.go:101 +0x6f8
```

This is a pre-existing bug where `errctx` is nil when `ReadyWait` times out. The root cause is a Docker container that failed to start, not a code bug in the migration logic.

### Container Cleanup Issues

The test script uses `--rm` flag, so containers auto-remove. If tests fail mid-run, you may need to manually clean up:
```bash
docker stop $(docker ps -q)
rm -f cid.txt
```

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

3. **Skip slow drivers** - Cassandra is the slowest. For quick iteration, test sqlite3 and postgres first.
