test: build
	for DATABASE_DRIVER in postgres mariadb mysql; do (\
		DATABASE_DRIVER=$$DATABASE_DRIVER bash tests/withdb.sh tests/scenario.sh || exit 1; \
	); done

build:
	go build .
