.PHONY: help test test-offline test-perf lint fmt vet build demo bench site clean verify tidy

GO ?= go
BIN ?= bin/itm

help:           ## show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-14s\033[0m %s\n", $$1, $$2}'

test:           ## run the full offline test suite (fast; excludes perf)
	$(GO) test -count=1 ./...

test-offline:   ## prove the module builds with no module proxy and no C toolchain
	GOPROXY=off CGO_ENABLED=0 $(GO) build ./...
	$(GO) test -count=1 ./internal/offline/

test-perf:      ## run the perf/benchmark measures (slow, machine-dependent)
	$(GO) test -run XXX -bench . -benchmem ./...

lint:           ## static checks (vet + gofmt, no external linters needed)
	$(GO) vet ./...
	@test -z "$$($(GO) fmt -l .)" || { echo "unformatted files:"; $(GO) fmt -l .; exit 1; }

fmt:            ## auto-format
	$(GO) fmt ./...

build:          ## build the static binary to bin/itm
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/itm

demo:           ## simulate an incident, analyze it, write the report to out/demo
	$(GO) run ./cmd/itm demo --out out/demo

bench:          ## run the held-out benchmark, rewrite BENCHMARK.md
	$(GO) run ./cmd/itm bench --seeds 1001-1020 --out BENCHMARK.md

site:           ## regenerate the static site (reports + benchmark table)
	$(GO) run ./cmd/itm demo --out site/report-1 --seed 11
	$(GO) run ./cmd/itm demo --out site/report-2 --seed 23
	$(GO) run ./cmd/itm demo --out site/report-3 --seed 37
	cp BENCHMARK.md site/BENCHMARK.md

verify:         ## judge-style check: clean build, tests with no network
	docker build -t itm .
	docker build -t itm-test --target test .
	docker run --rm --network none itm-test make test
	@echo "--- image size ---"
	docker images itm --format "{{.Repository}}:{{.Tag}} {{.Size}}"

clean:
	rm -rf bin out
	$(GO) clean -testcache
