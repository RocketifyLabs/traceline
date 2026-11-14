# syntax=docker/dockerfile:1

# Traceline rebuilds from this file on a clean Linux machine with no network at run
# time. The exact commands a reviewer runs:
#   docker build -t traceline .
#   docker run --rm --network none traceline make test
#
# Go produces a self-contained static binary, so the shipped image carries no
# interpreter, no package manager and no toolchain: only the binary.

# ---------- build stage ----------
FROM golang:1.27-alpine AS build

WORKDIR /src

# No external dependencies: go.mod has no require block. If that ever changes,
# copying go.sum and running `go mod download` here would be the place.
COPY go.mod ./
COPY . .

ENV CGO_ENABLED=0 \
    GOOS=linux

RUN go build -trimpath -ldflags "-s -w" -o /out/traceline ./cmd/traceline

# ---------- test stage ----------
# Kept separate so `make test` can run with the network switched off even though
# the final image has no compiler.
FROM golang:1.27-alpine AS test

WORKDIR /src
COPY go.mod ./
COPY . .

ENV CGO_ENABLED=0 \
    GOFLAGS=-mod=mod \
    GOPROXY=off

RUN apk add --no-cache make

CMD ["make", "test"]

# ---------- runtime stage ----------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/traceline /traceline

USER nonroot:nonroot
ENTRYPOINT ["/traceline"]
