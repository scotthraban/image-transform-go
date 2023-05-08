FROM golang:1.20.3-alpine3.17 AS mod_download

WORKDIR /go/src

COPY src/go.mod ./

RUN apk add musl-dev vips-dev gcc && go mod download

FROM mod_download AS builder

WORKDIR /go/src

COPY src/ ./

RUN go install .

FROM alpine:3.17

WORKDIR /app

RUN apk add vips

COPY --from=builder /go/bin/image-transform-go ./

ENTRYPOINT ["./image-transform-go"]