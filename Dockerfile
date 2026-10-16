# syntax=docker/dockerfile:1
#
# Build and test without installing Go or Node.js on the host.
#
#   docker build --target dist --output dist .   # binaries for all OSes -> ./dist
#     --build-arg PWA_URL=https://...           # optional default PWA URL
#   docker build --target test .                 # go vet / go test / PWA tests
#
# The agent itself runs on the host (it needs the host's named pipe or
# socket); Docker is only used to build it.

ARG GO_VERSION=1.27
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-slim AS node

FROM golang:${GO_VERSION} AS base
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

FROM base AS build
COPY . .
ARG TARGETS="windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"
ARG VERSION=dev
ARG PWA_URL=""
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build <<'EOF'
set -eu
mkdir -p /out
for t in $TARGETS; do
  os=${t%/*}; arch=${t#*/}; ext=""
  [ "$os" = windows ] && ext=.exe
  GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.defaultWebURL=$PWA_URL" \
    -o "/out/pocket-ssh-agent-$os-$arch$ext" ./cmd/pocket-ssh-agent
done
cd /out && sha256sum * > SHA256SUMS
EOF

FROM scratch AS dist
COPY --from=build /out/ /

FROM base AS test
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build <<'EOF'
set -eu
test -z "$(gofmt -l .)"
go vet ./...
GOOS=windows go vet ./...
GOOS=darwin go vet ./...
go test ./...
node --test web/test/*.test.mjs
EOF
