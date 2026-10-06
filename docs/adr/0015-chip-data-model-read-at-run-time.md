# ADR 0015 — The CHIP data model cross-check reads connectedhomeip at test time

- **Status**: Accepted
- **Date**: 2026-10-06
- **Related**:
  [`../chip-datamodel-crosscheck.md`](../chip-datamodel-crosscheck.md),
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  [ADR 0013 — cluster definitions generated from the matter.js model](./0013-generated-cluster-definitions.md)

## Context

matter.js stays the source of the schema snapshot and of the cluster
definitions generated from it. The CSA's Python certification cases judge a
device against connectedhomeip's data model XML (`data_model/<version>/`),
derived from the specification by a different scraper. `internal/chipdm`
compares the whole snapshot — every cluster and device type — with that XML.

The comparison needs the XML. Two ways to provide it:

1. **Commit a reduced extract** of the XML under `internal/chipdm/testdata/`,
   so the check runs in every `go test ./...` with no checkout.
2. **Read a connectedhomeip checkout at test time**, as matter.js's own
   comparison does (`support/codegen/src/chipdm/chip-data-model.ts` fetches
   the XML into a cache and commits nothing of it).

Every `data_model` XML file carries a Connectivity Standards Alliance notice.
It grants use "solely for your own internal purposes", and the licensee
warrants not to

> (b) post or publish this document;
>
> (c) modify, adapt, translate, or otherwise change this document in any
> manner or create any derivative work based on this document

A structured extract of those files, committed to a public MIT-licensed
repository, is both a publication and a derivative work.

## Decision

Read at test time (2); commit nothing derived from the XML.

- The cross-check reads `data_model/<CHIP_DATA_MODEL_VERSION>` at the
  Makefile's `CHIP_TEST_IMAGE_COMMIT` from a connectedhomeip checkout:
  `GOFABRIC_CHIP_ROOT` (the variable the chip-tool harness reads), else
  `../connectedhomeip` beside the main checkout (found through git's common
  directory, so a linked worktree finds it too). It reads the pinned commit
  with `git archive`, so the checkout's HEAD and sparse set do not matter.
  The model lives in memory only.
- Without such a checkout the tests that need it skip, naming
  `make chiptool-setup` (which adds `data_model/<version>` to its sparse
  set). With `GOFABRIC_CHIP_REQUIRED=1` the skip is a failure.
- CI runs it in its own job (`ci.yml`, "chip data model cross-check"): a
  blob-filtered, depth-1 partial clone of the pinned commit with only
  `data_model/<version>` checked out (about 8 MB), cached by pin, then
  `make chipdm-check`, which sets `GOFABRIC_CHIP_REQUIRED`. Every other job
  sees no checkout; there the cross-check skips and the parser and comparer
  are held by unit tests on fixtures written for them.
- What is committed is this module's own analysis: the comparison code, the
  acknowledged-differences table (per entry only the element path, the two
  differing values, the class, the reason and a citation), small synthetic
  XML fixtures authored for the unit tests, and the generated report's
  counts, provenance and classified entries — never the model.

## Consequences

- A contributor without the checkout gets a skip, not a false green; CI
  never skips. `make chipdm-check` / `make chipdm-report` run it locally.
- A harness image bump (`CHIP_TEST_IMAGE_COMMIT`) or a new Matter revision
  (`CHIP_DATA_MODEL_VERSION`, held to the snapshot by
  `TestChipDataModelPins`) re-runs the comparison on the next CI run; what the
  new CHIP commit says differently surfaces as unexplained or stale table
  entries and as a diff of the report's generated block.
- Coverage differs between a run with and without the checkout (the
  cross-check's own path); the floor is set to the checkout-less number,
  which is what the coverage job measures.
- Nothing derived from connectedhomeip's data model is distributed with this
  module (`THIRD-PARTY-NOTICES.md`).
