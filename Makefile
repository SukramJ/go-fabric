# SPDX-License-Identifier: MIT
# Copyright (C) 2026 SukramJ.

GO              ?= go
GOFUMPT         ?= gofumpt
GOLANGCI_LINT   ?= golangci-lint
GOVULNCHECK     ?= govulncheck
MATTERJS_DIR    ?= ../matter.js

# Pinned on purpose, and to the same versions .github/workflows/ci.yml uses.
# openccu-loom's setup target installs @latest, which is how a local checkout
# drifted behind its own CI; do not copy that here. Bump deliberately: raise
# both, run the linter locally, fix what it finds, in one commit.
GOFUMPT_VERSION       ?= v0.10.0
GOLANGCI_LINT_VERSION ?= v2.13.0

# govulncheck is deliberately NOT pinned (setup installs @latest, and so does
# the nightly workflow). The inverse of the lint argument: a newly published
# advisory SHOULD turn the scan red without a commit, because the code it
# indicts has not changed but its risk has.

# Fuzz smoke budget. Iteration-based (`<n>x`) on purpose: a wall-clock budget
# makes go's fuzzing coordinator cancel mid-execution on a CPU-starved runner
# and report a spurious "context deadline exceeded". A fixed execution count
# is immune to runner load — it just takes longer — while still replaying the
# full seed corpus and exploring new inputs. The nightly raises both.
FUZZTIME     ?= 100000x
FUZZ_TIMEOUT ?= 120s

.DEFAULT_GOAL := help

.PHONY: help
help: ## show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup: ## install the developer tooling (lint pinned, govulncheck floating)
	$(GO) install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest

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

.PHONY: fuzz
fuzz: ## run every fuzz target for $(FUZZTIME) executions as a smoke test
	@# The package list is discovered, not hand-maintained: a checked-in
	@# pattern list turns into a no-op that still reports success the moment a
	@# target moves packages, and nothing announces it. grep only nominates the
	@# candidate directories; `go test -list` is the authority on the target
	@# names, so a file behind a build tag is honoured rather than guessed at.
	@dirs=$$(grep -rl '^func Fuzz' --include='*_test.go' . | xargs -n1 dirname | sort -u); \
	if [ -z "$$dirs" ]; then echo "fuzz: no fuzz targets in this module"; exit 1; fi; \
	ran=0; \
	for dir in $$dirs; do \
		for fn in $$($(GO) test -list 'Fuzz.*' $$dir 2>/dev/null | grep '^Fuzz'); do \
			echo "-> fuzz $$dir :: $$fn ($(FUZZTIME))"; \
			$(GO) test $$dir -fuzz=^$${fn}$$ -fuzztime=$(FUZZTIME) -timeout=$(FUZZ_TIMEOUT) -run=^$$ || exit 1; \
			ran=$$((ran+1)); \
		done; \
	done; \
	if [ "$$ran" -eq 0 ]; then echo "fuzz: fuzz files found but no target ran — check build tags"; exit 1; fi; \
	echo "fuzz: $$ran target(s) completed at $(FUZZTIME)"

.PHONY: vuln
vuln: ## report known vulnerabilities on code paths this module actually calls
	$(GOVULNCHECK) ./...

.PHONY: reachability
reachability: ## regenerate script/reachability/inventory.json — which exported API no test reaches
	@# The root set is this module's own tests, not a production main: go-fabric
	@# is a library and has none. See the package doc of script/reachability for
	@# why a production-only run here would measure nothing.
	$(GO) run ./script/reachability

.PHONY: reachability-check
reachability-check: reachability ## regenerate, then fail if the committed snapshot moved or is off its ratchet
	@git diff --quiet -- script/reachability/inventory.json script/reachability/summary.md || { \
		echo ""; \
		echo "The committed reachability snapshot is stale: regenerating it changed the file."; \
		echo "Run 'make reachability', read the diff, and commit it with the change that moved"; \
		echo "it — adjusting reachabilityUnreachedRatchet in the same commit if the count moved."; \
		echo ""; \
		git --no-pager diff --stat -- script/reachability/; \
		exit 1; \
	}
	$(GO) test -count=1 -run 'Reachab' ./script/reachability/

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
