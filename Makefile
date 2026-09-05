# SPDX-License-Identifier: MIT
# Copyright (C) 2026 SukramJ.

GO              ?= go
GOFUMPT         ?= gofumpt
GOLANGCI_LINT   ?= golangci-lint
MATTERJS_DIR    ?= ../matter.js

# Pinned on purpose, and to the same versions .github/workflows/ci.yml uses.
# openccu-loom's setup target installs @latest, which is how a local checkout
# drifted behind its own CI; do not copy that here. Bump deliberately: raise
# both, run the linter locally, fix what it finds, in one commit.
GOFUMPT_VERSION       ?= v0.10.0
GOLANGCI_LINT_VERSION ?= v2.13.0

.DEFAULT_GOAL := help

.PHONY: help
help: ## show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup: ## install the pinned developer tooling
	$(GO) install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: build
build: ## compile every package (library module — no binaries)
	$(GO) build ./...

.PHONY: vet
vet: ## run go vet
	$(GO) vet ./...

.PHONY: test
test: ## run the test suite
	$(GO) test -count=1 ./...

.PHONY: race
race: ## run the test suite under the race detector (needs CGO)
	CGO_ENABLED=1 $(GO) test -race -count=1 ./...

.PHONY: fmt
fmt: ## format with gofumpt
	$(GOFUMPT) -w .

.PHONY: lint
lint: ## gofumpt check + golangci-lint + module hygiene
	@$(GOFUMPT) -l . | tee /tmp/gofabric-gofumpt.out
	@test ! -s /tmp/gofabric-gofumpt.out || { echo "gofumpt: files need formatting (run 'make fmt')"; exit 1; }
	$(GOLANGCI_LINT) config verify
	$(GOLANGCI_LINT) run --timeout=5m ./...
	$(GO) mod tidy -diff
	$(GO) mod verify

.PHONY: tidy
tidy: ## tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: generate-matter-schema
generate-matter-schema: ## regenerate parity/schema.json + schema/ from a matter.js checkout
	# The extractor runs inside the matter.js tree so its bare @matter/model
	# import resolves; the copy is removed again even when node fails.
	cp script/extract-from-matter-js.ts $(MATTERJS_DIR)/.gofabric-extract.mts
	cd $(MATTERJS_DIR) && node .gofabric-extract.mts \
		> $(CURDIR)/parity/schema.json; \
		rc=$$?; rm -f .gofabric-extract.mts; exit $$rc
	$(GO) run ./script/generate_matter_schema.go
	$(GOFUMPT) -w schema/

.PHONY: ci
ci: build vet test lint ## everything the pipeline runs
