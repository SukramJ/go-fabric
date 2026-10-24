# SPDX-License-Identifier: Apache-2.0
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
GOFUMPT_VERSION       ?= v0.12.0
GOLANGCI_LINT_VERSION ?= v2.14.0

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

# Stress budget (make stress, make stress-flakes). -p 8 on a developer
# machine is the load of a CI runner that runs the race job beside its
# siblings: a test whose waits depend on the machine's speed fails here
# rather than in a pull request. STRESS_COUNT runs per test; every run is
# its own process, the race detector on.
STRESS_COUNT   ?= 50
STRESS_P       ?= 8
STRESS_TIMEOUT ?= 10m
# STRESS_INPROC instances of the test run in each process (-test.count): a
# parallel test runs them side by side, the load of its package siblings.
STRESS_INPROC  ?= 1

# The tests the flake sweep (brief CG) fixed at their root, one line each:
# <package>:<-run regexp>. make stress-flakes runs every line under make
# stress; the nightly workflow runs it, a red line is a signal, not a gate.
# Each line is quoted: the recipe's shell reads the list, and ( in a
# regexp is shell syntax.
STRESS_FLAKES = \
	'./fabrictest:^TestHostStatementName$$' \
	'./controller/client:^TestTypedReadsConsumer$$' \
	'./node:^TestLevelControlTransitionReachesHostAndSubscriber$$' \
	'./client/interaction:^TestLiveSustainedSurvivesRestart$$' \
	'./controller/client:^TestLiveClientNode$$' \
	'./controller/client:^TestRegistryDecommission$$' \
	'./node:^TestOtaParityMatterJS$$' \
	'./node:^TestSessionAndSubscriptionDiagnostics$$' \
	'./controller/commissioner:^Test(Commissioning)?Connection'

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

.PHONY: build-32bit
build-32bit: ## compile for linux/armv7 and vet for linux/386 — an int is 32 bits there
	@# -gcflags=-e reports every type error, not the first ten; vet on 386
	@# type-checks the test files as well.
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO) build -gcflags=-e ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=386 $(GO) vet ./...

.PHONY: archcheck
archcheck: ## layers and dependency direction (ADR 0026)
	$(GO) test -count=1 ./script/archcheck/...

.PHONY: apidiff
apidiff: ## the incompatible API changes of the public packages against the last tag
	$(GO) run ./script/apidiff

.PHONY: apidiff-check
apidiff-check: ## fail when an incompatible API change has no CHANGELOG line under [Unreleased]
	$(GO) run ./script/apidiff -check

.PHONY: test
test: ## run the test suite
	$(GO) test -count=1 ./...

.PHONY: race
race: ## run the test suite under the race detector (needs CGO)
	CGO_ENABLED=1 $(GO) test -race -count=1 ./...

.PHONY: stress
stress: ## run TESTS of PKG under x/tools/cmd/stress with -race, -p $(STRESS_P), -count $(STRESS_COUNT); fails on any failure
	@test -n "$(PKG)" -a -n "$(TESTS)" || { echo "usage: make stress PKG=./node TESTS='^TestFoo$$'"; exit 2; }
	@dir=$$(mktemp -d) && trap 'rm -rf "$$dir"' EXIT && \
	$(GO) build -o "$$dir/stress" golang.org/x/tools/cmd/stress && \
	CGO_ENABLED=1 $(GO) test -race -c -o "$$dir/pkg.test" $(PKG) && \
	echo "stress $(PKG) $(TESTS): -p $(STRESS_P) -count $(STRESS_COUNT)" && \
	cd $(PKG) && "$$dir/stress" -p $(STRESS_P) -count $(STRESS_COUNT) -timeout $(STRESS_TIMEOUT) \
		-o "$${STRESS_OUT:-$$dir/fail-}" "$$dir/pkg.test" -test.run '$(TESTS)' -test.count=$(STRESS_INPROC)

.PHONY: stress-flakes
stress-flakes: ## make stress for every line of STRESS_FLAKES (the tests brief CG fixed); fails on the first line with a failure
	@for line in $(STRESS_FLAKES); do \
		$(MAKE) --no-print-directory stress PKG="$${line%%:*}" TESTS="$${line#*:}" || exit 1; \
	done

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

.PHONY: testparity
testparity: ## render notes/parity/matterjs_tests.md from the matter.js test register (no matter.js clone needed)
	$(GO) run ./script/testparity -render

.PHONY: testparity-snapshot
testparity-snapshot: ## refresh script/testparity/matterjs_tests.json from $(MATTERJS_DIR) at the schema pin (exit 2 without the clone)
	@# A pin lift's step: the diff names the matter.js test files added, removed
	@# and changed; testparity-check then lists every mapping to re-check.
	MATTERJS_DIR=$(MATTERJS_DIR) $(GO) run ./script/testparity -snapshot

.PHONY: testparity-check
testparity-check: ## fail if the test register and the snapshot disagree, a Go test it names is gone, the page is stale or planned is off its ratchet
	$(GO) test -count=1 -run 'Testparity' ./script/testparity/

.PHONY: cite-report
cite-report: ## regenerate script/citecheck/drift.json and summary.md — the matter.js citations checked at the pin
	@# Reads MATTERJS_DIR through git plumbing only (the clone may stand at any
	@# commit that has the pin). Exit 2 when the clone or the pin is missing.
	$(GO) run ./script/citecheck -matterjs $(MATTERJS_DIR) -report

.PHONY: cite-check
cite-check: ## regenerate the citation artefacts, then fail on a hard error or when they moved off the committed ones
	@# Built, not `go run`: go run turns every non-zero exit into 1, and exit 2
	@# (no clone, no pin) must not read as "hard errors".
	@bin=$$(mktemp -d)/citecheck; $(GO) build -o $$bin ./script/citecheck || exit 1; \
	$$bin -matterjs $(MATTERJS_DIR); rc=$$?; rm -rf $$(dirname $$bin); \
	if [ $$rc -eq 2 ]; then echo "cite-check: no matter.js clone with the pin at $(MATTERJS_DIR)"; exit 2; fi; \
	git diff --quiet -- script/citecheck/drift.json script/citecheck/summary.md || { \
		echo ""; \
		echo "The committed citation artefacts are stale: regenerating them changed the files."; \
		echo "Run 'make cite-report', read the diff, and commit it with the change that moved"; \
		echo "it — adjusting citecheckDriftRatchet in the same commit if the drift count moved."; \
		echo ""; \
		git --no-pager diff --stat -- script/citecheck/; \
		exit 1; \
	}; \
	if [ $$rc -ne 0 ]; then echo "cite-check: hard errors (summary.md lists them)"; exit $$rc; fi
	$(GO) test -count=1 -run 'Citecheck' ./script/citecheck/

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

# The CHIP data model directory the snapshot is cross-checked against
# (internal/chipdm): data_model/<the snapshot's Matter revision>, read at
# CHIP_TEST_IMAGE_COMMIT from a connectedhomeip checkout at test time and
# never committed (ADR 0015). TestChipDataModelPins holds it to
# parity/schema.json's revision.
CHIP_DATA_MODEL_VERSION ?= 1.6.1

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
# for the connectedhomeip checkout, beside the main working tree — also when
# make runs in a linked git worktree, whose ../ is not that tree's
# (git rev-parse --git-common-dir names the main tree's .git). Outside git it
# is ../connectedhomeip.
GIT_COMMON_DIR := $(shell git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)
CHIP_ROOT ?= $(if $(GIT_COMMON_DIR),$(abspath $(dir $(GIT_COMMON_DIR))../connectedhomeip),../connectedhomeip)

.PHONY: chiptool-setup
chiptool-setup: ## sparse connectedhomeip checkout at the harness image's CHIP commit (source to read, nothing to build)
	script/chiptool-setup.sh "$(CHIP_TEST_IMAGE_COMMIT)" "$(CHIP_ROOT)" "data_model/$(CHIP_DATA_MODEL_VERSION)"

.PHONY: chipdm-check
chipdm-check: ## run the CHIP data model cross-check against CHIP_ROOT; fails instead of skipping without the checkout
	GOFABRIC_CHIP_ROOT="$(abspath $(CHIP_ROOT))" GOFABRIC_CHIP_REQUIRED=1 $(GO) test -count=1 -run 'TestChipDataModel|TestCompareSeededDifferences' -v ./internal/chipdm/

.PHONY: chipdm-report
chipdm-report: ## rewrite the generated block of docs/chip-datamodel-crosscheck.md from CHIP_ROOT
	GOFABRIC_CHIP_ROOT="$(abspath $(CHIP_ROOT))" GOFABRIC_CHIP_REQUIRED=1 $(GO) test -count=1 -run 'TestChipDataModelCrosscheck' ./internal/chipdm/ -args -update-chipdm-doc

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

.PHONY: chiptool-families-update
chiptool-families-update: chiptool-build ## the families run that rewrites its fixtures: PICS slices, chip-cases golden, docs/certifiability.md (read the diff before committing)
	$(GO) test -tags=chiptool -count=1 -timeout=$(CHIP_FAMILIES_TIMEOUT) -v \
		-run '^TestChipCertificationFamilies$$' ./internal/chiptool/... \
		-args -update-chip-cases -update-certifiability-doc

.PHONY: fmt
fmt: ## format with gofumpt
	$(GOFUMPT) -w .

.PHONY: lint
lint: ## gofumpt check + golangci-lint + module hygiene
	@# .claude/worktrees holds agent worktrees: other branches' files, not this tree's.
	@$(GOFUMPT) -l . | grep -v '^\.claude/' | tee /tmp/gofabric-gofumpt.out
	@test ! -s /tmp/gofabric-gofumpt.out || { echo "gofumpt: files need formatting (run 'make fmt')"; exit 1; }
	$(GOLANGCI_LINT) config verify
	$(GOLANGCI_LINT) run --timeout=5m ./...
	@# The chip-tool harness builds only with the chiptool tag; lint it under
	@# that tag so it is held to the module's standard.
	$(GOLANGCI_LINT) run --timeout=5m --build-tags chiptool ./internal/chiptool/...
	$(GO) mod tidy -diff
	$(GO) mod verify

.PHONY: tidy
tidy: ## tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: generate-matter-schema
generate-matter-schema: ## regenerate parity/schema.json, schema/ and the generated cluster definitions from a matter.js checkout
	# The extractor runs inside the matter.js tree so its bare @matter/model
	# import resolves; the copy is removed again even when node fails.
	cp script/extract-from-matter-js.ts $(MATTERJS_DIR)/.gofabric-extract.mts
	# The snapshot is written to a temporary file and moved into place only
	# on success: a failing extractor must not truncate the committed pin.
	# A node that cannot run TypeScript itself (a distribution build without
	# type stripping) gets it transpiled by the checkout's own esbuild first.
	cd $(MATTERJS_DIR) && { \
		if [ -x node_modules/.bin/esbuild ]; then \
			node_modules/.bin/esbuild --log-level=warning --format=esm --platform=node \
				.gofabric-extract.mts --outfile=.gofabric-extract.mjs && \
			node .gofabric-extract.mjs; \
		else node .gofabric-extract.mts; fi; \
	} > $(CURDIR)/parity/schema.json.tmp; \
		rc=$$?; rm -f .gofabric-extract.mts .gofabric-extract.mjs; \
		if [ $$rc -ne 0 ]; then rm -f $(CURDIR)/parity/schema.json.tmp; exit $$rc; fi
	mv parity/schema.json.tmp parity/schema.json
	$(GO) run ./script/generate_matter_schema.go
	$(GOFUMPT) -w schema/
	@# The cluster definitions under cluster/spec/ (ADR 0013) are generated
	@# from the same snapshot; script/clustergen/clusters.go lists which.
	$(GO) run ./script/clustergen
	@# The device types under device/ (ADR 0018) reference those definitions;
	@# script/devicegen/devicetypes.go lists which.
	$(GO) run ./script/devicegen

.PHONY: update-dcl-snapshot
update-dcl-snapshot: ## refetch controller/dcl/snapshot/data (PAAs, CD signers, SOURCE) from the production DCL — a review step, read the diff before committing
	@# Never automatic: like the schema refresh this changes what the
	@# controller trusts. The CRL check stays online (controller/dcl).
	$(GO) run ./script/dclsnapshot -out controller/dcl/snapshot/data

.PHONY: ble-virtual-test
ble-virtual-test: ## BLE end to end over btvirt: the BLE build pairs a matter.js node on bleno (Linux; sudo, bluez + bluez-test-tools, Node.js, make, g++)
	@# The same script .github/workflows/ble-virtual.yml runs. Exit 2 is the
	@# environment (btvirt, BlueZ, Node.js, the bleno build, the HCI claim),
	@# exit 1 a defect.
	script/ble-virtual-test.sh

# ble/bluez is a Go module of its own (ADR 0024 decision 3): the main
# module's ./... does not reach it, so it has its own leg. The !linux stub
# is built for darwin as well.
BLUEZ_DIR ?= ble/bluez

.PHONY: bluez-check
bluez-check: ## the ble/bluez sub-module: build (Linux and the stub), vet, test, golangci-lint, go mod tidy -diff
	cd $(BLUEZ_DIR) && $(GO) build ./...
	cd $(BLUEZ_DIR) && GOOS=darwin $(GO) build ./...
	cd $(BLUEZ_DIR) && $(GO) vet ./...
	cd $(BLUEZ_DIR) && $(GO) test -count=1 ./...
	cd $(BLUEZ_DIR) && $(GOLANGCI_LINT) run --timeout=5m ./...
	cd $(BLUEZ_DIR) && $(GO) mod tidy -diff
	cd $(BLUEZ_DIR) && $(GO) mod verify

.PHONY: ci
ci: build build-32bit vet archcheck apidiff-check test lint cover-check bluez-check ## everything the pipeline runs
