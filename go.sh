docker run \
  --rm \
  -v "$PWD/src":/go/src/image-transform-go \
  -v "$PWD/bin":/go/bin \
  -w /go/src/image-transform-go \
  --user `id -u`:`id -g` \
  --env GOCACHE=/tmp/go/build/cache \
  golang:1.20.3-alpine3.17 go $1 $2 $3 $4 $5 $6 $7