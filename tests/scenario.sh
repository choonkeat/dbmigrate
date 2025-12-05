#!/bin/bash

# abort on any failure
set -euxo pipefail
source `dirname $0`/lib.sh

# setup
DBMIGRATE_CMD="./dbmigrate ${DBMIGRATE_OPT}"
DB_MIGRATIONS_DIR=tests/db/migrations

trap finish EXIT
mkdir -p ${DB_MIGRATIONS_DIR}
echo "testing ${DATABASE_DRIVER}..."

# `-create` should work
assert "should create new migration" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -create finally 'do! nothing??' 2>/dev/null
assert "should create .up.sql"       test -f ${DB_MIGRATIONS_DIR}/*_finally-do-nothing.up.sql
assert "should create .down.sql"     test -f ${DB_MIGRATIONS_DIR}/*_finally-do-nothing.down.sql
PENDING_VERSIONS="
`${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending`"

# give a no-op migration
printf "  \t\r\n \n\n \t" > ${DB_MIGRATIONS_DIR}/*_finally-do-nothing.up.sql
printf "  \t\r\n \n\n \t" > ${DB_MIGRATIONS_DIR}/*_finally-do-nothing.down.sql

# `-up` should fail; but recoverable later
cp tests/db/${DATABASE_DRIVER}/{20181222073546,20181222073750,20181222073901}_* ${DB_MIGRATIONS_DIR}
sed -i.original -e 's/price/xpricex/g' ${DB_MIGRATIONS_DIR}/20181222073901_change-product-price-to-int.*.sql
assert_equal "tests/db/${DATABASE_DRIVER}/VERSIONS-01.before-fail" "${PENDING_VERSIONS}" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending
assert_fail "should fail with bad migration" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up 2>/dev/null
assert_equal "tests/db/${DATABASE_DRIVER}/VERSIONS-02.after-fail" "${PENDING_VERSIONS}" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending

# retrying a fixed migration should work
sed -i.original -e 's/xpricex/price/g' ${DB_MIGRATIONS_DIR}/20181222073901_change-product-price-to-int.*.sql
assert "should retry fixed migration and succeed" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up 2>/dev/null
assert_equal "tests/db/${DATABASE_DRIVER}/VERSIONS-03.after-fix-retry" "" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending

# putting an old, missed migration; running `-up` should apply it
cp tests/db/${DATABASE_DRIVER}/20181222073900_* ${DB_MIGRATIONS_DIR}
assert_equal "tests/db/${DATABASE_DRIVER}/VERSIONS-04.before-missing" "" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending
assert "should run missing, older migration" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up 2>/dev/null
assert_equal "tests/db/${DATABASE_DRIVER}/VERSIONS-05.after-missing" "" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending

# migrating down should work
assert "should migrate down by 1" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -down 1 2>/dev/null
assert_equal "tests/db/${DATABASE_DRIVER}/VERSIONS-06.after-down-1" $PENDING_VERSIONS ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending

# should assert against a db dump here
assert "should migrate down until nothing" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -down 999 2>/dev/null
assert_equal "tests/db/${DATABASE_DRIVER}/VERSIONS-07.after-down-999" "${PENDING_VERSIONS}" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending

# Test -db-txn-mode behaviors
# First, apply all regular migrations
assert "apply all migrations for txn-mode tests" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up 2>/dev/null

# Create a .no-db-txn. migration (empty files work across all drivers)
touch ${DB_MIGRATIONS_DIR}/20991231235959_test-no-txn.no-db-txn.up.sql
touch ${DB_MIGRATIONS_DIR}/20991231235959_test-no-txn.no-db-txn.down.sql

# Should fail with default mode (all) when .no-db-txn. files exist
assert_fail "should fail with -db-txn-mode=all when .no-db-txn. files pending" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up 2>&1

# Should succeed with -db-txn-mode=per-file
assert "should succeed with -db-txn-mode=per-file" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=per-file -up 2>/dev/null

# Migrate down and test -db-txn-mode=none
assert "migrate down the no-txn migration" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=per-file -down 1 2>/dev/null

# Should also succeed with -db-txn-mode=none
assert "should succeed with -db-txn-mode=none" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=none -up 2>/dev/null

# Clean up: migrate everything down
assert "final cleanup - migrate down all" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=per-file -down 999 2>/dev/null

# Test -db-txn-mode transaction behavior differences
# Same setup, three different outcomes based on mode
# Note: CQL (Cassandra) doesn't support transactions, so skip these tests
#
# Expected behavior:
#   -db-txn-mode=all:      txn-first=NO,  txn-second=NO  (all rolled back) - sqlite3 postgres mariadb mysql
#   -db-txn-mode=per-file: txn-first=YES, txn-second=NO  (file 2 rolled back) - sqlite3 postgres mariadb mysql
#   -db-txn-mode=none:     txn-first=YES, txn-second=??? (driver-dependent partial state)
#     - sqlite3:  txn-second=YES (executes statements independently)
#     - postgres: txn-second=NO  (executes multi-statement SQL atomically)
#     - mysql:    txn-second=YES (with multiStatements=true, executes independently)
#     - mariadb:  txn-second=YES (same as mysql)

if [ "$DATABASE_DRIVER" = "cql" ]; then
    pass "skipping txn-mode behavior tests for cql (no transaction support)"
else

# Helper to check if a row exists in products table (returns count)
check_row_count() {
    local name=$1
    case $DATABASE_DRIVER in
        sqlite3)
            sqlite3 "${DATABASE_URL}" "SELECT COUNT(*) FROM products WHERE name='${name}';" 2>/dev/null || echo "0"
            ;;
        postgres)
            docker exec $(cat cid.txt) psql -U postgres -d dbmigrate_test -t -c "SELECT COUNT(*) FROM products WHERE name='${name}';" 2>/dev/null | tr -d ' \t\r\n' || echo "0"
            ;;
        mysql)
            # Try mysql first (mysql image), then mariadb (mariadb image)
            # Use 2>&1 and grep to filter only numeric results (ignores docker error messages)
            (docker exec $(cat cid.txt) /usr/bin/mysql -u root -ppassword dbmigrate_test -N -e "SELECT COUNT(*) FROM products WHERE name='${name}';" 2>&1 || \
             docker exec $(cat cid.txt) /usr/bin/mariadb -u root -ppassword dbmigrate_test -N -e "SELECT COUNT(*) FROM products WHERE name='${name}';" 2>&1) | grep -E '^[0-9]+$' | tail -1 || echo "0"
            ;;
    esac
}

# Helper to setup txn-mode test migrations
setup_txn_test() {
    rm -f ${DB_MIGRATIONS_DIR}/2099*
    # Migration 1: single successful INSERT
    cat > ${DB_MIGRATIONS_DIR}/20990101000001_txn-test-first.up.sql << 'SQLEOF'
INSERT INTO products (name, price) VALUES ('txn-first', 100);
SQLEOF
    cat > ${DB_MIGRATIONS_DIR}/20990101000001_txn-test-first.down.sql << 'SQLEOF'
DELETE FROM products WHERE name = 'txn-first';
SQLEOF
    # Migration 2: successful INSERT then fails on nonexistent table
    cat > ${DB_MIGRATIONS_DIR}/20990101000002_txn-test-second.up.sql << 'SQLEOF'
INSERT INTO products (name, price) VALUES ('txn-second', 200);
INSERT INTO nonexistent_table VALUES (1);
SQLEOF
    cat > ${DB_MIGRATIONS_DIR}/20990101000002_txn-test-second.down.sql << 'SQLEOF'
DELETE FROM products WHERE name = 'txn-second';
SQLEOF
}

# Setup: base table
rm -f ${DB_MIGRATIONS_DIR}/2099*
cp tests/db/${DATABASE_DRIVER}/20181222073546_create-products.* ${DB_MIGRATIONS_DIR}/
assert "txn-test: create base table" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up 2>/dev/null

# Test 1: -db-txn-mode=all - both migrations should be rolled back
setup_txn_test
assert_fail "all: should fail on bad migration" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=all -up 2>&1
ALL_PENDING=$(${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending 2>/dev/null)
if echo "$ALL_PENDING" | grep -q "20990101000001" && echo "$ALL_PENDING" | grep -q "20990101000002"; then
    pass "all: both migrations rolled back (both pending)"
else
    fail "all: expected both migrations to be pending after rollback"
    echo "Pending: $ALL_PENDING"
    exit 1
fi
# Verify actual data: txn-first should NOT exist (rolled back)
ALL_FIRST_COUNT=$(check_row_count "txn-first")
if [ "$ALL_FIRST_COUNT" = "0" ]; then
    pass "all: txn-first row does NOT exist (rolled back)"
else
    fail "all: txn-first row should NOT exist, but count=$ALL_FIRST_COUNT"
    exit 1
fi

# Test 2: -db-txn-mode=per-file - migration 1 applied, migration 2 rolled back
setup_txn_test
assert_fail "per-file: should fail on bad migration" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=per-file -up 2>&1
PERFILE_PENDING=$(${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending 2>/dev/null)
if echo "$PERFILE_PENDING" | grep -q "20990101000002" && ! echo "$PERFILE_PENDING" | grep -q "20990101000001"; then
    pass "per-file: migration 1 applied, migration 2 pending"
else
    fail "per-file: expected only migration 2 to be pending"
    echo "Pending: $PERFILE_PENDING"
    exit 1
fi
# Verify actual data: txn-first EXISTS, txn-second does NOT exist
PERFILE_FIRST_COUNT=$(check_row_count "txn-first")
PERFILE_SECOND_COUNT=$(check_row_count "txn-second")
if [ "$PERFILE_FIRST_COUNT" = "1" ] && [ "$PERFILE_SECOND_COUNT" = "0" ]; then
    pass "per-file: txn-first=1, txn-second=0 (file 2 rolled back)"
else
    fail "per-file: expected txn-first=1, txn-second=0, but got txn-first=$PERFILE_FIRST_COUNT, txn-second=$PERFILE_SECOND_COUNT"
    exit 1
fi
# Clean up migration 1 for next test
${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=per-file -down 999 2>/dev/null || true

# Test 3: -db-txn-mode=none - migration 1 applied, migration 2 partial (first INSERT applied)
setup_txn_test
assert_fail "none: should fail on bad migration" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=none -up 2>&1
NONE_PENDING=$(${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending 2>/dev/null)
if echo "$NONE_PENDING" | grep -q "20990101000002" && ! echo "$NONE_PENDING" | grep -q "20990101000001"; then
    pass "none: migration 1 applied, migration 2 pending (partial state)"
else
    fail "none: expected only migration 2 to be pending"
    echo "Pending: $NONE_PENDING"
    exit 1
fi
# Verify actual data: txn-first EXISTS, txn-second depends on driver behavior
NONE_FIRST_COUNT=$(check_row_count "txn-first")
NONE_SECOND_COUNT=$(check_row_count "txn-second")
# postgres executes multi-statement SQL atomically, so txn-second=0
# sqlite3/mysql execute statements independently, so txn-second=1
case $DATABASE_DRIVER in
    postgres)
        EXPECTED_SECOND=0
        ;;
    *)
        EXPECTED_SECOND=1
        ;;
esac
if [ "$NONE_FIRST_COUNT" = "1" ] && [ "$NONE_SECOND_COUNT" = "$EXPECTED_SECOND" ]; then
    pass "none: txn-first=1, txn-second=$EXPECTED_SECOND (driver-specific behavior)"
else
    fail "none: expected txn-first=1, txn-second=$EXPECTED_SECOND, but got txn-first=$NONE_FIRST_COUNT, txn-second=$NONE_SECOND_COUNT"
    exit 1
fi

# Final cleanup
${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -db-txn-mode=none -down 999 2>/dev/null || true

fi # end of if [ "$DATABASE_DRIVER" != "cql" ]

# Test concurrent locking (only for databases that support locking)
# sqlite3 and cql don't support locking, so skip
if [ "$DATABASE_DRIVER" = "postgres" ] || [ "$DATABASE_DRIVER" = "mysql" ]; then

# Number of concurrent processes (default 5, configurable via env)
CONCURRENT_PROCESSES=${CONCURRENT_PROCESSES:-5}
echo "Testing concurrent locking for ${DATABASE_DRIVER} with ${CONCURRENT_PROCESSES} processes..."

# Setup: clean state with base table
rm -f ${DB_MIGRATIONS_DIR}/2099*
cp tests/db/${DATABASE_DRIVER}/20181222073546_create-products.* ${DB_MIGRATIONS_DIR}/
${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -down 999 2>/dev/null || true
${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up 2>/dev/null

# Create test migrations for concurrent test with slow operations to ensure contention
# Use driver-specific sleep to increase the window for lock contention
case $DATABASE_DRIVER in
    postgres)
        SLEEP_SQL="SELECT pg_sleep(1);"
        ;;
    mysql)
        SLEEP_SQL="SELECT SLEEP(1);"
        ;;
esac
cat > ${DB_MIGRATIONS_DIR}/20990201000001_concurrent-test.up.sql << SQLEOF
INSERT INTO products (name, price) VALUES ('concurrent-test-1', 100);
${SLEEP_SQL}
SQLEOF
cat > ${DB_MIGRATIONS_DIR}/20990201000001_concurrent-test.down.sql << 'SQLEOF'
DELETE FROM products WHERE name = 'concurrent-test-1';
SQLEOF
cat > ${DB_MIGRATIONS_DIR}/20990201000002_concurrent-test.up.sql << SQLEOF
INSERT INTO products (name, price) VALUES ('concurrent-test-2', 200);
${SLEEP_SQL}
SQLEOF
cat > ${DB_MIGRATIONS_DIR}/20990201000002_concurrent-test.down.sql << 'SQLEOF'
DELETE FROM products WHERE name = 'concurrent-test-2';
SQLEOF

# Run N migration processes concurrently
# All should succeed - one applies migrations, others wait then find nothing to do
echo "Starting ${CONCURRENT_PROCESSES} concurrent migration processes..."
PIDS=()
for i in $(seq 1 $CONCURRENT_PROCESSES); do
    ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up > /tmp/migrate${i}.log 2>&1 &
    PIDS+=($!)
done

# Wait for all processes and collect exit codes
ALL_SUCCESS=true
for i in $(seq 1 $CONCURRENT_PROCESSES); do
    wait ${PIDS[$i-1]}
    EXIT_CODE=$?
    if [ "$EXIT_CODE" != "0" ]; then
        ALL_SUCCESS=false
        echo "Process $i failed with exit code $EXIT_CODE"
    fi
done

# All should succeed
if [ "$ALL_SUCCESS" = "true" ]; then
    pass "concurrent: all ${CONCURRENT_PROCESSES} migration processes completed successfully"
else
    fail "concurrent: not all processes succeeded"
    for i in $(seq 1 $CONCURRENT_PROCESSES); do
        echo "=== Process $i log ===" && cat /tmp/migrate${i}.log
    done
    exit 1
fi

# Verify migrations applied exactly once (not zero, not twice)
CONCURRENT_PENDING=$(${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending 2>/dev/null)
if [ -z "$CONCURRENT_PENDING" ]; then
    pass "concurrent: no pending migrations (all applied)"
else
    fail "concurrent: expected no pending migrations, but got: $CONCURRENT_PENDING"
    exit 1
fi

# Verify data: each row should exist exactly once
CONCURRENT_COUNT1=$(check_row_count "concurrent-test-1")
CONCURRENT_COUNT2=$(check_row_count "concurrent-test-2")
if [ "$CONCURRENT_COUNT1" = "1" ] && [ "$CONCURRENT_COUNT2" = "1" ]; then
    pass "concurrent: each migration applied exactly once (count=1)"
else
    fail "concurrent: expected count=1 for each, got concurrent-test-1=$CONCURRENT_COUNT1, concurrent-test-2=$CONCURRENT_COUNT2"
    exit 1
fi

# Verify that lock contention actually occurred (at least one process waited)
LOCK_WAIT_FOUND=false
for i in $(seq 1 $CONCURRENT_PROCESSES); do
    if grep -q "Waiting for migration lock" /tmp/migrate${i}.log; then
        LOCK_WAIT_FOUND=true
        break
    fi
done
if [ "$LOCK_WAIT_FOUND" = "true" ]; then
    pass "concurrent: lock contention verified (process waited for lock)"
else
    fail "concurrent: no lock contention detected - test inconclusive"
    for i in $(seq 1 $CONCURRENT_PROCESSES); do
        echo "=== Process $i log ===" && cat /tmp/migrate${i}.log
    done
    exit 1
fi

# Show which process did the work (informational)
for i in $(seq 1 $CONCURRENT_PROCESSES); do
    echo "=== Process $i log ===" && cat /tmp/migrate${i}.log
done

# Cleanup
${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -down 999 2>/dev/null || true
rm -f /tmp/migrate*.log

fi # end of concurrent locking test

# Test MySQL DDL warning
# MySQL should show the warning, other drivers should not
if [ "$DATABASE_DRIVER" = "mysql" ]; then
    # Setup: clean state
    rm -f ${DB_MIGRATIONS_DIR}/2099*
    cp tests/db/${DATABASE_DRIVER}/20181222073546_create-products.* ${DB_MIGRATIONS_DIR}/
    ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -down 999 2>/dev/null || true

    # Run migration and capture output
    ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up > /tmp/mysql_ddl_test.log 2>&1

    # Verify warning appears
    if grep -q "MySQL does not support transactional DDL" /tmp/mysql_ddl_test.log; then
        pass "mysql: DDL warning shown"
    else
        fail "mysql: DDL warning NOT shown (expected warning)"
        cat /tmp/mysql_ddl_test.log
        exit 1
    fi

    # Cleanup
    ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -down 999 2>/dev/null || true
    rm -f /tmp/mysql_ddl_test.log
fi

# For postgres, verify warning does NOT appear
if [ "$DATABASE_DRIVER" = "postgres" ]; then
    # Setup: clean state
    rm -f ${DB_MIGRATIONS_DIR}/2099*
    cp tests/db/${DATABASE_DRIVER}/20181222073546_create-products.* ${DB_MIGRATIONS_DIR}/
    ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -down 999 2>/dev/null || true

    # Run migration and capture output
    ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -up > /tmp/postgres_ddl_test.log 2>&1

    # Verify warning does NOT appear
    if grep -q "MySQL does not support transactional DDL" /tmp/postgres_ddl_test.log; then
        fail "postgres: DDL warning shown (should NOT appear for postgres)"
        cat /tmp/postgres_ddl_test.log
        exit 1
    else
        pass "postgres: DDL warning correctly NOT shown"
    fi

    # Cleanup
    ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -down 999 2>/dev/null || true
    rm -f /tmp/postgres_ddl_test.log
fi
