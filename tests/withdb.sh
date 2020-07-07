#!/bin/bash

# abort on any failure
set -e
source `dirname $0`/lib.sh

PORT=65500
DB_PASSWORD=password
DB_USER=dbuser
DB_NAME=dbmigrate_test
DB_MIGRATIONS_DIR=tests/db/migrations
TARGET_SCRIPT=$1

function finish {
    test -f cid.txt && docker rm -f `cat cid.txt` >/dev/null || true
    rm -f cid.txt
}
trap finish EXIT

case $DATABASE_DRIVER in
    postgres)
    docker run --rm -e POSTGRES_DB=${DB_NAME}dummy -e POSTGRES_PASSWORD=${DB_PASSWORD} -p ${PORT}:5432 -d --cidfile cid.txt postgres
    env DATABASE_DRIVER=postgres DBMIGRATE_OPT='-server-ready 60s -create-db' DATABASE_URL="postgres://postgres:${DB_PASSWORD}@localhost:${PORT}/${DB_NAME}?sslmode=disable" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mysql)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME}dummy -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mysql --default-authentication-plugin=mysql_native_password
    env DATABASE_DRIVER=mysql DBMIGRATE_OPT='-server-ready 60s -create-db' DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mariadb)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME}dummy -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mariadb
    env DATABASE_DRIVER=mysql DBMIGRATE_OPT='-server-ready 60s -create-db' DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    sqlite3)
    rm -f "./tests/sqlite3.db"
    if env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT='-server-ready 60s' DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
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
    env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT="" DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    rm -f "./tests/sqlite3.db"
    ;;
    *)
    echo Unknown DATABASE_DRIVER=${DATABASE_DRIVER}
    exit 1
    ;;
esac
