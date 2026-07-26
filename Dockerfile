# Prefer `make build` — it uses named Docker volumes for the Go module and build caches.
# This file is kept for reference and matches the same Go version as the Makefile.

ARG GO_VERSION=1.22
FROM golang:${GO_VERSION}-bookworm
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -o /pelican-steam-updater ./cmd/pelican-steam-updater
