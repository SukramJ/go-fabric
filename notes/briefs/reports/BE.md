# Report BE — milestone 2, phase 14b: the test-porting register

Implementer: Opus. Branch `m2/phase-14-testparity` (from `m2/phase-14-pin`
d8fe15d), PR #153. matter.js read only through `ls-tree` / `cat-file` /
`show` at `862ad75f` (the `sourceCommit` of `parity/schema.json`). Nothing
under `docs/adr/`, `CLAUDE.md`, `parity/schema.json` or `notes/parity/`
changed except the generated `matterjs_tests.md`. (Committed by the owner
from the agent's hand-back: the agent's tool set could not write under
`notes/briefs/reports/`.)

| Commit | Items |
| --- | --- |
| `bd4967f` `script/testparity`: snapshot, register with the initial fill, page, guard, coverage floor | 1–5 |
| `cf0b275` Makefile targets, CI job `testparity`, parity-contract row, CHANGELOG | 4, 6 |
| `1dbab21` reachability snapshot regenerated for the new test package | guard |

**Guards** (HEAD `cf0b275` before, `1dbab21` after): `make testparity-check`
exit 0; `make ci` exit 0; `make reachability-check` exit 2 on the first run
(entry points 5287 → 5295, test packages 214 → 215, unreached count
unchanged, ratchet stays 5), regenerated as `1dbab21`, rerun exit 0. No
chip-tool run: nothing under `node/`, `cluster/` or `endpoint/` moved.

## 1. The snapshot

Built: `script/testparity/matterjs_tests.json`, written by
`go run ./script/testparity -snapshot` (`make testparity-snapshot`;
`MATTERJS_DIR`, default `../matter.js`): commit, the two roots, every
`*Test.ts` under them sorted by path, with line count, blob SHA-1 from
`ls-tree` and the top-level `describe` names; one `ls-tree` and one
`cat-file --batch`. 324 files (210 + 114); regeneration reproduces the
committed file; exit 2 without the clone or the commit (no-clone case
checked). Guard: `TestTestparitySnapshotIsThePin` fails when the snapshot's
commit differs from `sourceCommit`; sorting, SHAs and roots checked; parsers
unit-tested. Decision: "top-level" is a `describe` at column 0; four files
have none (`ClusterBehaviorVarianceTest`, `loaderTest`, `PluginsTest`,
`lenientTest`).

## 2. The register

Built: `script/testparity/register.json`, Go types `Register`/`Entry`, JSON
schema in the package doc. `validate` refuses an unknown status or class;
`go`/`missing` on `planned`/`excluded`; `ported` with `missing` or `partial`
without; `ported`/`partial` without `go`; a malformed `pkg:TestFunc`; `class`
or `until` on a non-excluded entry; `controller-stage` without `until`, or
`until` on another class except `device-ble-thread`; a missing `reason` or a
bad `snapshotSha`; unsorted or duplicate paths; unknown JSON fields. Guard:
`TestTestparityEntryValidation`. Decision: `reason` and `snapshotSha` are
required on every entry.

## 3. The page

Built: `notes/parity/matterjs_tests.md` from `-render` (`make testparity`,
no clone): generated marker, pin, counts per status; a per-directory table
with anchors; one table per leaf directory in tree order; the `planned` list
by directory. Compared byte for byte by the guard. 212 KB, because each
`partial` note lists every missing `it` (kept complete, owner decision).

## 4. The guard

`register_test.go`: snapshot and register match both ways; every `go`
reference resolves through `go/parser` to a `Test…` function in that
package's `_test.go` files; a changed blob fails with "re-check the mapping"
(extended to `excluded` entries on review: "re-check the exclusion"); the
page is current; `testparityPlannedRatchet` equals the `planned` count
exactly, in both directions, documented like `reachabilityUnreachedRatchet`.
Makefile `testparity`, `testparity-snapshot`, `testparity-check`; CI job
`testparity` without a clone.

## 5. The initial fill

Method: every `describe`/`it` name of the 324 blobs against the 5 288 Go
`func Test` names; the best signal is a Go test doc comment citing a
`…Test.ts`; per candidate the doc comment and `t.Run` names; a scratch
builder computes `missing` as the uncovered `it` names (`constraintTest.ts`
is table-driven: its table groups).

Counts before review: ported 8, partial 94, planned 133, excluded 89
(js-runtime-only 71, controller-stage 16, test-infrastructure 2). Per
directory and the full excluded list: PR #153's description and the
generated page. Ratchet before review: 133. Owner review (2026-10-18): all
eight `ported` read; `SustainedSubscriptionTest` moved from controller-stage
to `partial` (the sustained subscription exists in `controller/interaction`),
`PeerConnectionSocketOwnershipTest` from controller-stage to `planned` (its
UDP describe applies now); `SecureChannelProtocolTest` stays
controller-stage (both `it` are ICD check-in routing);
`ScenesManagementServerTest` from `ported` to `partial` (its one `it` also
checks the 60 000 000 ms transition maximum and a 1 s recall transition).
Final counts (commits `e2c3953`, `ab6f0a0`): ported 7, partial 96, planned
134, excluded 87 (js-runtime-only 71, controller-stage 14,
test-infrastructure 2); `testparityPlannedRatchet = 134`.

What the seed got wrong: nested describes mixed with top-level ones; noisy
candidates from describe words; the real ports missed (`BtpSessionHandlerTest`
/ `BtpCodecTest` → `transport/btp/*_parity_matterjs_test.go`,
`BleScannerTest` → `controller/ble`, `DelayReportDataTest` →
`im/subscription:TestDeferReports`) — the reliable map is a grep of
`*_test.go` for `[A-Za-z]+Test\.ts`; table-driven files have no literal
`it(`; Go parity tests cite `0ceb202d` and quote `it` names that changed by
`862ad75f`, so matching goes by meaning.

by_design candidates: none.

## 6. Docs and CHANGELOG

`docs/matter-parity-contract.md`: guard row "Test-porting register", a step
4 in "Sweeping a pin bump" (refresh the snapshot, re-check the mappings the
guard names, move the ratchet in the same commit). One CHANGELOG entry.

## Left out

`script/coverfloor/floors.go` got one entry (`script/testparity`, floor 67,
measured 67.7 %): outside the brief's paths, required by `make cover-check`;
kept (owner). The PR shows the pin-lift commits until brief BD's PR merges;
not rebased by the agent.

## Questions (answered on review)

Floors entry stays; `FabricIcdTest` planned and the two controller-only ICD
files controller-stage; controller features without a roadmap stage are
`planned`; a changed blob fails `excluded` too; the page stays complete; the
remote-control API and TypeScript accessor tests are `js-runtime-only`.
