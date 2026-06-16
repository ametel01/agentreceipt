SHELL := /bin/bash

GO ?= go
TOOLS_BIN ?= $(CURDIR)/.tools/bin
GOLANGCI_LINT ?= $(TOOLS_BIN)/golangci-lint
STATICCHECK ?= $(TOOLS_BIN)/staticcheck
GOSEC ?= $(TOOLS_BIN)/gosec

export PATH := $(TOOLS_BIN):$(PATH)

.PHONY: fmt fmt-check lint test test-race build verify security coverage smoke tools clean

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
	$(GO) build ./...

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
	rm -rf .tools coverage.out dist agentreceipt
