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
# CHIP_TEST_IMAGE (below) is the SINGLE SOURCE for the commissioner, the CSA
# certification harness and the CHIP reference apps this module is tested
# against: the suite, the certification families and the CI control leg
# (.github/workflows/chiptool.yml) all run inside it.
#
# CHIP_CERT_BINS_IMAGE is only the host fallback: `make chiptool-extract`
# copies chip-tool out of it for a run without Docker (arm64 hosts; the image
# publishes arm64 manifests only). Bump it deliberately, like the image pin.
CHIP_CERT_BINS_IMAGE ?= connectedhomeip/chip-cert-bins:6feac778f196483b6355d35fc529f183b293b71f
CHIPTOOL_BIN_DIR     ?= bin

# The CHIP test harness image the chip-tool suite runs by default: matter.js's
# own (../matter.js/support/chip), multi-arch, carrying chip-tool, the YAML
# and Python certification cases and the CHIP reference apps at one CHIP
# commit. Pinned by digest, never a floating tag; the CHIP commit it was
# built from is in the image's org.opencontainers.image.revision label and
# /etc/chip-version, recorded below; `make chiptool-setup` checks the CHIP
# source out at that commit. Bump both together, deliberately: a new digest is
# a new commissioner and a new case set.
CHIP_TEST_IMAGE        ?= ghcr.io/matter-js/chip@sha256:d6f1de89d714309beb621543d451a98996690eea82fa74d1563abd7d2b3cb326
CHIP_TEST_IMAGE_COMMIT ?= 6170af8461b10b1766044122ac83332c6d00ab20

.PHONY: chiptool-extract
chiptool-extract: ## copy chip-tool out of the pinned chip-cert-bins image into ./bin (arm64 hosts; ~2.5 GiB pull)
	@# The image publishes arm64 manifests only. On any other host the pull
	@# fails with Docker's "no matching manifest for linux/amd64", which names
	@# neither the cause nor a way out -- so say both before Docker does.
	@arch=$$(uname -m); case "$$arch" in aarch64|arm64) ;; *) \
		echo "chiptool-extract: $(CHIP_CERT_BINS_IMAGE) publishes linux/arm64 only; this host is $$arch."; \
		echo "  On amd64, use the chip-tool snap instead -- the suite finds it on PATH and"; \
		echo "  keeps its storage under ~/snap/chip-tool/common by itself:"; \
		echo "      sudo snap install chip-tool"; \
		echo "  or point GOFABRIC_CHIPTOOL_BIN at any other chip-tool build."; \
		echo "  See internal/chiptool/doc.go for what differs between the snap and the CI pin."; \
		exit 1;; esac
	@mkdir -p $(CHIPTOOL_BIN_DIR)
	@# /root/chip-tool in the image is a symlink; /root/apps/chip-tool is the file.
	docker create --name gofabric-chip-cert-bins $(CHIP_CERT_BINS_IMAGE)
	docker cp gofabric-chip-cert-bins:/root/apps/chip-tool $(CHIPTOOL_BIN_DIR)/chip-tool
	docker rm gofabric-chip-cert-bins
	chmod +x $(CHIPTOOL_BIN_DIR)/chip-tool
	@# The suite logs this sidecar as the binary's identity in every run.
	@echo "$(CHIP_CERT_BINS_IMAGE)" > $(CHIPTOOL_BIN_DIR)/chip-tool.source

# Where `make chiptool-setup` puts the CHIP source: the path CLAUDE.md names
# for the connectedhomeip checkout.
CHIP_ROOT ?= ../connectedhomeip

.PHONY: chiptool-setup
chiptool-setup: ## sparse connectedhomeip checkout at the harness image's CHIP commit (source to read, nothing to build)
	script/chiptool-setup.sh "$(CHIP_TEST_IMAGE_COMMIT)" "$(CHIP_ROOT)"

.PHONY: chiptool-image
chiptool-image: ## pull the pinned CHIP test harness image (the suite also pulls it on first use)
	docker pull $(CHIP_TEST_IMAGE)

.PHONY: chiptool-build
chiptool-build: ## build the reference daemon the chip-tool guard commissions
	$(GO) build -o $(CHIPTOOL_BIN_DIR)/reference-bridge ./examples/reference-bridge

# CHIPTOOL_TIMEOUT bounds the commissioning test and the chip-tool suite;
# CHIP_FAMILIES_TIMEOUT the certification families, which run for hours in
# full. Raise them rather than trimming tests.
CHIPTOOL_TIMEOUT      ?= 1500s
CHIP_FAMILIES_TIMEOUT ?= 10h

.PHONY: chiptool-test
chiptool-test: chiptool-build ## commission the reference daemon and run the chip-tool suite (Linux + Docker, or a host chip-tool)
	@# Skips itself, naming the missing prerequisite, without Docker or a
	@# chip-tool; fails loudly on a missing daemon binary, which
	@# chiptool-build has just produced.
	$(GO) test -tags=chiptool -count=1 -timeout=$(CHIPTOOL_TIMEOUT) -v \
		-skip '^TestChipCertificationFamilies$$' ./internal/chiptool/...

.PHONY: chiptool-families
chiptool-families: chiptool-build ## run the CSA certification families in the CHIP harness image (GOFABRIC_CHIP_FAMILIES=IDM,ACL narrows)
	$(GO) test -tags=chiptool -count=1 -timeout=$(CHIP_FAMILIES_TIMEOUT) -v \
		-run '^TestChipCertificationFamilies$$' ./internal/chiptool/...

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
	# The snapshot is written to a temporary file and moved into place only
	# on success: a failing extractor must not truncate the committed pin.
	cd $(MATTERJS_DIR) && node .gofabric-extract.mts \
		> $(CURDIR)/parity/schema.json.tmp; \
		rc=$$?; rm -f .gofabric-extract.mts; \
		if [ $$rc -ne 0 ]; then rm -f $(CURDIR)/parity/schema.json.tmp; exit $$rc; fi
	mv parity/schema.json.tmp parity/schema.json
	$(GO) run ./script/generate_matter_schema.go
	$(GOFUMPT) -w schema/

.PHONY: ci
ci: build vet test lint cover-check ## everything the pipeline runs
