# syntax=docker/dockerfile:1
FROM docker.io/library/golang:1.25.7 AS builder

COPY go.mod go.sum *.go /go/src/

ENV CGO_ENABLED=0

WORKDIR /go/src

RUN go build -o simpleappcontroller .



FROM scratch

COPY --from=builder --chmod=0755 /go/src/simpleappcontroller /simpleappcontroller

CMD [ "/simpleappcontroller" ]
