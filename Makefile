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

# Benchmark budgets. BENCHTIME is what a contributor measuring a change
# should use; BENCH_SMOKE_TIME is the CI leg's, which only proves every
# benchmark still builds, still finds its fixture and still completes — a
# benchmark nobody runs rots exactly like a test nobody runs, except that it
# fails silently at the moment somebody finally needs a number from it.
BENCHTIME        ?= 1s
BENCH_SMOKE_TIME ?= 10x

# Where `make cover` / `make cover-check` leave the per-package coverage
# report the floor checker reads. The name ends in .out so .gitignore's
# existing `*.out` rule keeps it out of the tree — the floors themselves are
# committed (script/coverfloor/floors.go), the measurement never is.
COVER_REPORT ?= coverage.out

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

.PHONY: cover
cover: ## run the suite with per-package statement coverage
	$(GO) test -count=1 -cover ./... 2>&1 | tee $(COVER_REPORT)

.PHONY: cover-check
cover-check: ## regenerate the coverage report, then fail on a package below its floor
	@# Two steps rather than one pipeline: `sh` has no pipefail, so a failing
	@# `go test` on the left of a pipe would be masked by the checker's own
	@# exit status. The checker refuses a report carrying FAIL lines for the
	@# same reason — coverage from a run that did not finish is not a
	@# measurement, and ratcheting against it would bake in a partial number.
	$(GO) test -count=1 -cover ./... > $(COVER_REPORT) 2>&1 || { cat $(COVER_REPORT); exit 1; }
	$(GO) run ./script/coverfloor -report $(COVER_REPORT)

.PHONY: bench
bench: ## run every benchmark at $(BENCHTIME)
	$(GO) test -run '^$$' -bench . -benchtime $(BENCHTIME) ./...

.PHONY: bench-smoke
bench-smoke: ## run every benchmark for $(BENCH_SMOKE_TIME) iterations — a build-and-run check, not a measurement
	$(GO) test -run '^$$' -bench . -benchtime $(BENCH_SMOKE_TIME) ./...

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

# --- the real-commissioner guard ----------------------------------------
#
# The line below is the SINGLE SOURCE for the chip-tool build this module is
# tested against: .github/workflows/chiptool.yml seds the 40-hex tag out of
# this Makefile for both of its jobs, so the suite and its control leg can
# never end up validating two different chip-tool builds, and a local
# extraction gets the same one a CI run does.
#
# The tag is a connectedhomeip commit. Bump it deliberately — a new tag is a
# new commissioner, which is exactly the thing this guard measures against.
# Recent chip-cert-bins tags publish arm64-only manifests, which is why the
# workflow runs on ubuntu-24.04-arm; an amd64 runner cannot pull the image.
CHIP_CERT_BINS_IMAGE ?= connectedhomeip/chip-cert-bins:6feac778f196483b6355d35fc529f183b293b71f
CHIPTOOL_BIN_DIR     ?= bin

.PHONY: chiptool-extract
chiptool-extract: ## copy chip-tool out of the pinned chip-cert-bins image into ./bin (~2.5 GiB pull)
	@mkdir -p $(CHIPTOOL_BIN_DIR)
	@# /root/chip-tool in the image is a symlink; /root/apps/chip-tool is the file.
	docker create --name gofabric-chip-cert-bins $(CHIP_CERT_BINS_IMAGE)
	docker cp gofabric-chip-cert-bins:/root/apps/chip-tool $(CHIPTOOL_BIN_DIR)/chip-tool
	docker rm gofabric-chip-cert-bins
	chmod +x $(CHIPTOOL_BIN_DIR)/chip-tool

.PHONY: chiptool-build
chiptool-build: ## build the reference daemon the chip-tool guard commissions
	$(GO) build -o $(CHIPTOOL_BIN_DIR)/reference-bridge ./examples/reference-bridge

.PHONY: chiptool-test
chiptool-test: chiptool-build ## commission the reference daemon with chip-tool (Linux host + chip-tool required)
	@# The suite skips itself when chip-tool is missing, which is the whole
	@# story on macOS: chip-tool has no macOS host build. It fails loudly on a
	@# missing daemon binary instead, which chiptool-build has just produced.
	$(GO) test -tags=chiptool -count=1 -timeout=900s -v ./internal/chiptool/...

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
ci: build vet test lint cover-check ## everything the pipeline runs
