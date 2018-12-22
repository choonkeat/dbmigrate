build:
	go build .

test:
	for DATABASE_DRIVER in postgres mariadb mysql; do (\
 		DATABASE_DRIVER=$$DATABASE_DRIVER bash tests/withdb.sh tests/scenario.sh; \
	); done
