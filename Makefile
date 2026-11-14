.PHONY: help test test-all test-offline test-perf lint fmt build demo simulate bench site clean verify

GO ?= go
BIN ?= bin/traceline

help:           ## show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-14s\033[0m %s\n", $$1, $$2}'

test:           ## run the offline test suite (fast; excludes perf)
	CGO_ENABLED=0 $(GO) test -count=1 -short ./...

test-all:       ## run every test including the slow perf budget
	CGO_ENABLED=0 $(GO) test -count=1 ./...

test-offline:   ## prove the module builds with no module proxy and no C toolchain
	GOPROXY=off CGO_ENABLED=0 $(GO) build ./...
	GOPROXY=off CGO_ENABLED=0 $(GO) test -count=1 ./internal/offline/

test-perf:      ## run the perf budget and scaling checks (slow, machine-dependent)
	$(GO) test -count=1 -v -run "TestAnalysis" ./internal/perf/
	$(GO) test -run XXX -bench . -benchmem ./internal/perf/

lint:           ## static checks (vet + gofmt, no external linters needed)
	$(GO) vet ./internal/... ./cmd/...
	@test -z "$$($(GO) fmt -l ./internal ./cmd)" || \
		{ echo "unformatted files:"; $(GO) fmt -l ./internal ./cmd; exit 1; }

fmt:            ## auto-format
	$(GO) fmt ./internal/... ./cmd/... ./scripts/...

build:          ## build the static binary to bin/traceline
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/traceline

demo:           ## simulate an incident, analyze it, write the report to out/demo
	$(GO) run ./cmd/traceline demo --out out/demo

simulate:       ## write a scenario's logs and spans to out/scenario
	$(GO) run ./cmd/traceline simulate --seed 7 --out out/scenario

bench:          ## run the held-out benchmark, rewrite BENCHMARK.md
	$(GO) run ./cmd/traceline bench --seeds 1001-1020 --out BENCHMARK.md

site:           ## regenerate the static site (3 reports + benchmark table)
	$(GO) run scripts/build_site.go

verify:         ## judge-style check: clean build, tests with no network
	docker build -t traceline .
	docker build -t traceline-test --target test .
	docker run --rm --network none traceline-test make test
	@echo "--- image size ---"
	docker images traceline --format "{{.Repository}}:{{.Tag}} {{.Size}}"

clean:
	rm -rf bin out
	$(GO) clean -testcache
