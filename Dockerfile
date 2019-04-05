FROM golang:latest AS builder

COPY . /src

RUN GO111MODULE=on go build \
      -ldflags "-linkmode external -extldflags -static" \
      -o /bin/dbmigrate \
      /src/cmd/dbmigrate/*.go

FROM scratch
COPY --from=builder /bin/dbmigrate /bin/dbmigrate
ENTRYPOINT ["/bin/dbmigrate"]
