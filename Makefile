# Copyright 2026 ALTESSA SOLUTIONS INC.
# Use of this source code is governed by the MIT license that can be found in
# the LICENSE file.

# Tools are built inside the worktree, never installed into the user's bin.
GO ?= go
TOOLBIN := $(CURDIR)/bin/tools
LINTER := $(TOOLBIN)/golangci-lint
GOSEC := $(TOOLBIN)/gosec
VULN := $(TOOLBIN)/govulncheck
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD)
LDFLAGS := -X github.com/altessa-s/go-atlas-core/runtime/appinfo.Name=snctl -X github.com/altessa-s/go-atlas-core/runtime/appinfo.Project=snctl -X github.com/altessa-s/go-atlas-core/runtime/appinfo.Version=$(VERSION) -X github.com/altessa-s/go-atlas-core/runtime/appinfo.Commit=$(COMMIT)

.PHONY: all help tidy fmt lint golangci vet build snctl test test-race test-integration coverage coverage-gate security-scan gosec govulncheck ci clean
all: help
help:
	@echo 'snctl build lint vet test test-integration coverage-gate security-scan ci'
$(LINTER): devtools/go.mod devtools/go.sum
	$(GO) -C devtools build -o $(LINTER) github.com/golangci/golangci-lint/v2/cmd/golangci-lint
$(GOSEC): devtools/go.mod devtools/go.sum
	$(GO) -C devtools build -o $(GOSEC) github.com/securego/gosec/v2/cmd/gosec
$(VULN): devtools/go.mod devtools/go.sum
	$(GO) -C devtools build -o $(VULN) golang.org/x/vuln/cmd/govulncheck

tidy:
	$(GO) mod tidy
	$(GO) -C devtools mod tidy
fmt: $(LINTER)
	$(LINTER) fmt ./...
lint: tidy fmt golangci
golangci: $(LINTER)
	$(LINTER) run ./...
vet:
	$(GO) vet ./...
	$(GO) fix -diff ./...
build:
	$(GO) build ./...
snctl:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/snctl ./cmd/snctl
test:
	$(GO) test -race ./...
test-race: test
test-integration:
	$(GO) test -race -shuffle=on -tags=integration ./...
# CI measures coverage on the integration run itself (COVERAGE_TAGS=integration)
# instead of running the race suite a second time.
COVERAGE_TAGS ?=
coverage:
	$(GO) test -tags="$(COVERAGE_TAGS)" -race -shuffle=on -coverpkg=./... -covermode=atomic -coverprofile=coverage.out ./...
coverage-gate: coverage
	bash .github/scripts/coverage-gate.sh coverage.out
	$(GO) tool cover -func=coverage.out > coverage-summary.txt
	awk '/^total:/ { if ($$3 + 0 < 93) exit 1; print }' coverage-summary.txt
gosec: $(GOSEC)
	$(GOSEC) -quiet -exclude-generated -exclude=G104 ./...
govulncheck: $(VULN)
	$(VULN) ./...
security-scan: govulncheck gosec
ci: COVERAGE_TAGS = integration
ci: lint vet coverage-gate security-scan dupl-check
clean:
	rm -rf bin coverage.out coverage-summary.txt

.PHONY: test-all test-shuffle test-verbose test-coverage build-nocgo bench fuzz-seed fuzz copyright
build-nocgo:
	CGO_ENABLED=0 $(GO) build ./...
	CGO_ENABLED=0 $(GO) vet ./...
test-all: coverage-gate test-integration
test-shuffle:
	$(GO) test -race -shuffle=on ./...
test-verbose:
	$(GO) test -race -v ./...
test-coverage: coverage
bench:
	$(GO) test -run '^$$' -bench . -benchmem ./...
fuzz-seed:
	$(GO) test -run '^Fuzz' ./...
fuzz:
	$(GO) test ./cmd/snctl/commands/cliutil -run '^$$' -fuzz '^FuzzMaskJSON$$' -fuzztime=30s
copyright:
	$(GO) -C devtools build -o $(TOOLBIN)/go-copyright-checker github.com/altessa-s/go-copyright-checker
	$(TOOLBIN)/go-copyright-checker check --fix .

$(TOOLBIN)/dupl: devtools/go.mod devtools/go.sum
	$(GO) -C devtools build -o $(TOOLBIN)/dupl github.com/mibk/dupl
.PHONY: dupl dupl-check
dupl: $(TOOLBIN)/dupl
	bash .github/scripts/dupl.sh
dupl-check: $(TOOLBIN)/dupl
	bash .github/scripts/dupl.sh check

# Lite builds omit the renderer and retain the native notebook reader.
.PHONY: snctl-lite build-lite test-lite golangci-lite
snctl-lite:
	$(GO) build -trimpath -tags norender -ldflags '$(LDFLAGS)' -o bin/snctl ./cmd/snctl
build-lite:
	$(GO) build -tags norender ./...
	$(GO) vet -tags norender ./...
test-lite:
	$(GO) test -race -shuffle=on -tags=norender,integration ./...
golangci-lite: $(LINTER)
	$(LINTER) run --build-tags integration,norender ./...

# Explicit credentials and a build tag keep real-cloud writes opt-in.
.PHONY: test-bench
test-bench:
	$(GO) test -count=1 -timeout=15m -tags=bench ./tests/bench

.PHONY: release-macos
release-macos: ## Build, sign, notarize and upload macOS archives of a published release (TAG=v1.2.3)
	@if [ -z "$(TAG)" ]; then echo "Usage: make release-macos TAG=v1.2.3"; exit 1; fi
	@bash .github/scripts/release-macos.sh "$(TAG)"
