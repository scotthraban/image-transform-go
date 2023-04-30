docker run \
  --rm \
  -v "$PWD/src":/usr/app/src \
  -w /usr/app/src \
  --user `id -u`:`id -g` \
  --env GOCACHE=/tmp/go \
  golang:1.20.3-alpine3.17 go $1 $2 $3 $4 $5 $6 $7