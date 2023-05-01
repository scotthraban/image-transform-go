FROM golang:1.20.3-alpine3.17 AS mod_download

WORKDIR /go/src

COPY src/go.mod ./

RUN go mod download

FROM mod_download AS builder

WORKDIR /go/src

COPY src/ ./

RUN go install .

FROM alpine:3.17

WORKDIR /app

COPY --from=builder /go/bin/image-transform-go ./

ENTRYPOINT ["./image-transform-go"]