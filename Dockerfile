FROM golang:1.20.3-alpine3.17 AS mod_download

WORKDIR /src

COPY src/go.mod ./

RUN go mod download

FROM mod_download AS builder

WORKDIR /src

COPY src/ ./

RUN go build .

FROM alpine:3.17

WORKDIR /app

COPY --from=builder /src/image-transform ./

ENTRYPOINT ["./image-transform"]