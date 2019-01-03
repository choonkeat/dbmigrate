#!/bin/bash

# abort on any failure
set -e
source `dirname $0`/lib.sh

# setup
DBMIGRATE_CMD='./dbmigrate -server-ready 60s -create-db'
DB_MIGRATIONS_DIR=tests/db/migrations

trap finish EXIT
mkdir -p ${DB_MIGRATIONS_DIR}
echo "testing ${DATABASE_DRIVER}..."

# echo commands that we run
# set -x

# `-create` should work
assert "should create new migration" ${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -create finally 'do! nothing??' 2>/dev/null
assert "should create .up.sql"       test -f ${DB_MIGRATIONS_DIR}/*_finally-do-nothing.up.sql
assert "should create .down.sql"     test -f ${DB_MIGRATIONS_DIR}/*_finally-do-nothing.down.sql
PENDING_VERSIONS="
`${DBMIGRATE_CMD} -dir ${DB_MIGRATIONS_DIR} -versions-pending`"

# give a no-op migration
echo 'SELECT 1;' > ${DB_MIGRATIONS_DIR}/*_finally-do-nothing.up.sql
echo 'SELECT 1;' > ${DB_MIGRATIONS_DIR}/*_finally-do-nothing.down.sql

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
