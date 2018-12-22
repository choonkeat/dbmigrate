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
    docker run --rm -e POSTGRES_DB=${DB_NAME} -e POSTGRES_PASSWORD=${DB_PASSWORD} -p ${PORT}:5432 -d --cidfile cid.txt postgres
    until docker exec -it `cat cid.txt` pg_isready 2>&1 > /dev/null; do echo "waiting for ${DATABASE_DRIVER}..."; sleep 3; done
    # sleep 10 # no need to sleep
    env DATABASE_DRIVER=${DATABASE_DRIVER} DATABASE_URL="postgres://postgres:${DB_PASSWORD}@localhost:${PORT}/${DB_NAME}?sslmode=disable" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mysql)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME} -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -e MYSQL_ALLOW_EMPTY_PASSWORD=yes -p ${PORT}:3306 -d --cidfile cid.txt mysql --default-authentication-plugin=mysql_native_password
    until docker exec -it `cat cid.txt` mysql -e 'SELECT 1' 2>&1 > /dev/null; do echo "waiting for ${DATABASE_DRIVER}..."; sleep 3; done
    sleep 20
    env DATABASE_DRIVER=${DATABASE_DRIVER} DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mariadb)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME} -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mariadb
    until docker exec -it `cat cid.txt` mysql -e 'SELECT 1' 2>&1 > /dev/null; do echo "waiting for ${DATABASE_DRIVER}..."; sleep 3; done
    sleep 20
    env DATABASE_DRIVER=mysql DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    *)
    echo Unknown DATABASE_DRIVER=${DATABASE_DRIVER}
    exit 1
    ;;
esac
