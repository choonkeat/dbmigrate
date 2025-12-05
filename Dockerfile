FROM golang:1.21-bookworm AS builder

WORKDIR /src
COPY . /src
RUN go build \
      -ldflags "-linkmode external -extldflags -static" \
      -o /bin/dbmigrate \
      /src/cmd/dbmigrate/*.go

FROM scratch
COPY --from=builder /bin/dbmigrate /bin/dbmigrate
ENTRYPOINT ["/bin/dbmigrate"]
