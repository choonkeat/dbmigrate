FROM golang:1.21-bookworm AS builder

ARG VERSION=dev
WORKDIR /src
COPY . /src
RUN go build \
      -ldflags "-linkmode external -extldflags -static -X main.Version=${VERSION}" \
      -o /bin/dbmigrate \
      /src/cmd/dbmigrate/*.go

FROM scratch
COPY --from=builder /bin/dbmigrate /bin/dbmigrate
ENTRYPOINT ["/bin/dbmigrate"]
