DATABASE_DRIVERS=postgres mariadb mysql

test: build
	for DATABASE_DRIVER in $(DATABASE_DRIVERS); do (\
		DATABASE_DRIVER=$$DATABASE_DRIVER bash tests/withdb.sh tests/scenario.sh || exit 1; \
	); done

build:
	go build ./cmd/dbmigrate
