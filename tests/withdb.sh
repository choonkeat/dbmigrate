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
    echo "waiting for ${DATABASE_DRIVER}...";
    until docker exec -it `cat cid.txt` pg_isready; do sleep 3; done
    env DATABASE_DRIVER=${DATABASE_DRIVER} DATABASE_URL="postgres://postgres:${DB_PASSWORD}@localhost:${PORT}/${DB_NAME}?sslmode=disable" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mysql)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME} -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mysql --default-authentication-plugin=mysql_native_password
    echo "waiting for ${DATABASE_DRIVER}...";
    until docker exec -it `cat cid.txt` mysql -u root -p${DB_PASSWORD} --protocol TCP -e 'SELECT 1'; do sleep 3; done
    env DATABASE_DRIVER=${DATABASE_DRIVER} DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    mariadb)
    docker run --rm -e MYSQL_DATABASE=${DB_NAME} -e MYSQL_ROOT_PASSWORD=${DB_PASSWORD} -p ${PORT}:3306 -d --cidfile cid.txt mariadb
    echo "waiting for ${DATABASE_DRIVER}...";
    until docker exec -it `cat cid.txt` mysql -u root -p${DB_PASSWORD} --protocol TCP -e 'SELECT 1'; do sleep 3; done
    env DATABASE_DRIVER=mysql DATABASE_URL="root:${DB_PASSWORD}@tcp(127.0.0.1:${PORT})/${DB_NAME}?multiStatements=true" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
    *)
    echo Unknown DATABASE_DRIVER=${DATABASE_DRIVER}
    exit 1
    ;;
esac
