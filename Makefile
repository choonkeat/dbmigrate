DATABASE_DRIVERS=postgres mariadb mysql
BUILD_TARGET=./cmd/dbmigrate/main.go

test: build
	go build -o /dev/null ./examples # verify examples can compile
	for DATABASE_DRIVER in $(DATABASE_DRIVERS); do (\
		DATABASE_DRIVER=$$DATABASE_DRIVER bash tests/withdb.sh tests/scenario.sh || exit 1; \
	); done

build:
	go build -o dbmigrate $(BUILD_TARGET)
