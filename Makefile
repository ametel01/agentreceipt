SHELL := /bin/bash

GO ?= go
BINARY ?= agentreceipt
TOOLS_BIN ?= $(CURDIR)/.tools/bin
GOLANGCI_LINT ?= $(TOOLS_BIN)/golangci-lint
STATICCHECK ?= $(TOOLS_BIN)/staticcheck
GOSEC ?= $(TOOLS_BIN)/gosec

export PATH := $(TOOLS_BIN):$(PATH)

.PHONY: help fmt fmt-check lint test test-race build verify security coverage smoke tools clean

help:
	@printf '%s\n' 'AgentReceipt Make targets:'
	@printf '  %-12s %s\n' 'help' 'Show this help.'
	@printf '  %-12s %s\n' 'fmt' 'Format Go files with gofmt.'
	@printf '  %-12s %s\n' 'fmt-check' 'Fail if Go files need formatting.'
	@printf '  %-12s %s\n' 'lint' 'Run golangci-lint, staticcheck, and go vet.'
	@printf '  %-12s %s\n' 'test' 'Run go test ./...'
	@printf '  %-12s %s\n' 'test-race' 'Run go test -race ./...'
	@printf '  %-12s %s\n' 'security' 'Run gosec ./...'
	@printf '  %-12s %s\n' 'coverage' 'Run tests with coverage and enforce the 80% threshold.'
	@printf '  %-12s %s\n' 'build' 'Build all Go packages and refresh ./agentreceipt.'
	@printf '  %-12s %s\n' 'smoke' 'Run the CLI smoke harness.'
	@printf '  %-12s %s\n' 'verify' 'Run format, lint, tests, race, security, coverage, build, and smoke.'
	@printf '  %-12s %s\n' 'tools' 'Install local lint/security tools into .tools/bin.'
	@printf '  %-12s %s\n' 'clean' 'Remove local build and tool artifacts.'

fmt:
	$(GO) fmt ./...
	gofmt -s -w .

fmt-check:
	test -z "$$(gofmt -s -l .)"

lint:
	$(GOLANGCI_LINT) run ./...
	$(STATICCHECK) ./...
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

build:
	rm -f "$(BINARY)"
	$(GO) build ./...
	tmp="$$(mktemp "$(CURDIR)/$(BINARY).XXXXXX")"; \
		trap 'rm -f "$$tmp"' EXIT; \
		$(GO) build -o "$$tmp" .; \
		mv "$$tmp" "$(BINARY)"

verify: fmt-check lint test test-race security coverage build smoke

security:
	$(GOSEC) ./...

coverage:
	$(GO) test ./... -run Test -count=1 -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out | awk '/total:/ { if ($$3+0 < 80.0) exit 1 }'

smoke:
	./scripts/smoke.sh

tools:
	mkdir -p "$(TOOLS_BIN)"
	GOBIN="$(TOOLS_BIN)" $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	GOBIN="$(TOOLS_BIN)" $(GO) install honnef.co/go/tools/cmd/staticcheck@latest
	GOBIN="$(TOOLS_BIN)" $(GO) install github.com/securego/gosec/v2/cmd/gosec@latest

clean:
	rm -rf .tools coverage.out dist "$(BINARY)"
