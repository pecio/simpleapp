# syntax=docker/dockerfile:1
FROM docker.io/library/golang:1.25.7 AS builder

COPY go.mod go.sum *.go /go/src/

WORKDIR /go/src

RUN /usr/bin/env CGO_ENABLED=0 go build -o app .



FROM scratch

COPY --from=builder --chmod=0755 /go/src/app /app

CMD [ "/app" ]
