#!/bin/bash

set -euxo pipefail
source `dirname $0`/lib.sh

PORT=65500
DB_PASSWORD=password
DB_USER=dbuser
DB_NAME=dbmigrate_test
DB_MIGRATIONS_DIR=tests/db/migrations
TARGET_SCRIPT=$1
SERVER_READY=${SERVER_READY:-180s}

# Clean up any container using our port from a previous run
EXISTING=$(docker ps -q --filter "publish=${PORT}")
if [ -n "$EXISTING" ]; then
    echo "Stopping existing container on port ${PORT}..."
    docker stop $EXISTING 2>/dev/null || true
fi
rm -f cid.txt

function finish {
    local exit_code=$?  # Capture the exit code
    if [ -f cid.txt ]; then
        docker stop $(cat cid.txt) 2>/dev/null || true
        # No docker rm needed - container has --rm flag
        rm -f cid.txt
    fi
    exit $exit_code  # Explicitly exit with the captured code
}
trap finish EXIT

case $DATABASE_DRIVER in
    postgres)
    docker run --rm -e POSTGRES_DB=${DB_NAME}dummy -e POSTGRES_PASSWORD=${DB_PASSWORD} -p ${PORT}:5432 -d --cidfile cid.txt postgres
    env DATABASE_DRIVER=postgres DBMIGRATE_OPT="-server-ready ${SERVER_READY} -create-db" DATABASE_URL="postgres://postgres:${DB_PASSWORD}@localhost:${PORT}/${DB_NAME}?sslmode=disable" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mysql)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME}dummy -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mysql
    env DATABASE_DRIVER=mysql DBMIGRATE_OPT="-server-ready ${SERVER_READY} -create-db" DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mariadb)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME}dummy -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mariadb
    env DATABASE_DRIVER=mysql DBMIGRATE_OPT="-server-ready ${SERVER_READY} -create-db" DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    sqlite3)
    rm -f "./tests/sqlite3.db"
    if env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT="-server-ready ${SERVER_READY}" DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should not support -server-ready"
        exit 1
    else
        pass "should not support -server-ready"
        if env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT='-create-db' DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
            fail "should not support -create-db"
            exit 1
        else
            pass "should not support -create-db"
        fi
    fi
    # Test that locking is required (should fail without -no-lock)
    if env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT="" DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should require -no-lock flag"
        exit 1
    else
        pass "should require -no-lock flag"
    fi
    # Now run with -no-lock
    env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT="-no-lock" DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    rm -f "./tests/sqlite3.db"
    ;;
    cql)
    docker run --rm -p ${PORT}:9042 -d --cidfile cid.txt cassandra
    if env DATABASE_DRIVER=cql DBMIGRATE_OPT="-server-ready ${SERVER_READY} -create-db" DATABASE_URL="localhost:${PORT}?keyspace=${DB_NAME}" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should not support -create-db"
        exit 1
    else
        pass "should not support -create-db"
    fi
    docker logs --since 1m `cat cid.txt`
    until docker exec -t `cat cid.txt` cqlsh -e 'describe cluster' >/dev/null; do docker logs --since 1m `cat cid.txt`; echo waiting for cassandra; sleep 1; done
    until docker exec -t `cat cid.txt` cqlsh -e "CREATE KEYSPACE IF NOT EXISTS ${DB_NAME} WITH replication = {'class':'SimpleStrategy', 'replication_factor' : 1};"; do
        docker logs --since 1m `cat cid.txt`;
        fail "unexpected error pre-creating keyspace ${DB_NAME}; retrying..."
        sleep 1
    done
    # Test that locking is required (should fail without -no-lock)
    if env DATABASE_DRIVER=cql DBMIGRATE_OPT="-server-ready ${SERVER_READY}" DATABASE_URL="localhost:${PORT}?keyspace=${DB_NAME}&timeout=3m" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should require -no-lock flag"
        exit 1
    else
        pass "should require -no-lock flag"
    fi
    # Now run with -no-lock
    env DATABASE_DRIVER=cql DBMIGRATE_OPT="-server-ready ${SERVER_READY} -no-lock" DATABASE_URL="localhost:${PORT}?keyspace=${DB_NAME}&timeout=3m" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    *)
    echo Unknown DATABASE_DRIVER=${DATABASE_DRIVER}
    exit 1
    ;;
esac
