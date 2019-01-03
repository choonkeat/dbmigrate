#!/bin/bash

# abort on any failure
set -e

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
    env DATABASE_DRIVER=${DATABASE_DRIVER} DATABASE_URL="postgres://postgres:${DB_PASSWORD}@localhost:${PORT}/${DB_NAME}?sslmode=disable" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mysql)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME}dummy -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mysql --default-authentication-plugin=mysql_native_password
    env DATABASE_DRIVER=${DATABASE_DRIVER} DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mariadb)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME}dummy -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mariadb
    env DATABASE_DRIVER=mysql DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    sqlite3)
    rm -f "./tests/sqlite3.db"
    env DATABASE_DRIVER=sqlite3 DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    rm -f "./tests/sqlite3.db"
    ;;
    *)
    echo Unknown DATABASE_DRIVER=${DATABASE_DRIVER}
    exit 1
    ;;
esac
