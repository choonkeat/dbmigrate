DATABASE_DRIVERS=cql sqlite3 postgres mariadb mysql
BUILD_TARGET=./cmd/dbmigrate/*.go

test: build
	go build -o /dev/null ./examples # verify examples can compile
	for DATABASE_DRIVER in $(DATABASE_DRIVERS); do (\
		DATABASE_DRIVER=$$DATABASE_DRIVER bash tests/withdb.sh tests/scenario.sh || exit 1; \
	); done

build:
	go build -o dbmigrate $(BUILD_TARGET)

build-docker:
	tar -c Dockerfile go.* *.go cmd | gzip -9 | docker build -f Dockerfile - -t dbmigrate

#

publish-docker: build-docker
	docker tag dbmigrate:latest choonkeat/dbmigrate
	docker push choonkeat/dbmigrate
