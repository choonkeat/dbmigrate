DATABASE_DRIVERS=cql sqlite3 postgres mariadb mysql
BUILD_TARGET=./cmd/dbmigrate/*.go
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

test-clean:
	rm -rf tests/db/migrations
	docker stop $$(docker ps -q --filter "publish=65500") 2>/dev/null || true

test: test-clean build
	go build -o /dev/null ./examples # verify examples can compile
	for DATABASE_DRIVER in $(DATABASE_DRIVERS); do \
		DATABASE_DRIVER=$$DATABASE_DRIVER bash -euxo pipefail tests/withdb.sh tests/scenario.sh || exit 1; \
	done

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o dbmigrate $(BUILD_TARGET)

build-docker:
	tar -c Dockerfile go.* *.go cmd | gzip -9 | docker build -f Dockerfile - -t dbmigrate

#

publish-docker: build-docker
	docker tag dbmigrate:latest choonkeat/dbmigrate
	docker push choonkeat/dbmigrate

release:
ifndef RELEASE_VERSION
	$(error RELEASE_VERSION is required. Usage: make release RELEASE_VERSION=3.0.1)
endif
	git diff --quiet || (echo "Error: uncommitted changes. Commit or stash first." && exit 1)
	git push origin master
	git tag v$(RELEASE_VERSION)
	git push origin v$(RELEASE_VERSION)
	@echo ""
	@echo "Released v$(RELEASE_VERSION)"
	@echo "Docker image will be built by GitHub Actions: choonkeat/dbmigrate:$(RELEASE_VERSION)"
