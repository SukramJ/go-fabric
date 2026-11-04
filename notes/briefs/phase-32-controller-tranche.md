# Brief phase 32 — the controller and protocol tranche of the matter.js test port

Date 2026-11-03. Milestone 8 phase 32 (`docs/roadmap.md`): the 312 `it`
cases of nine partial register entries. Owner decisions 2026-11-03
(wizard): (1) the three security-path files (SessionManagerTest,
CertificatesTest, PqcAttestationCertificatesTest) were implemented by
Fable — they are on this branch already (commits e2fd61fa, 9135ea43,
bc87623d, fbbd18e2; register list
`~/.cache/go-fabric-scratch/p32/register-fable.md`), **nothing of them is
yours**; (2) **every one of the 72 feature gaps the map names is built**
(none deferred); (3) the controller's node-id allocation **stays random**
— by design, one `by_design.md` paragraph (several administrators on one
fabric collide with sequential ids from 1; host records stay stable), the
four cases `left: by-design`; (4) **two Opus agents on one branch and one
pull request**, disjoint files, the guard bundle once at the end by the
agent that finishes last, js-only cases into the register's `left` field
(phase 31's `script/testparity` change).

Map: `~/.cache/go-fabric-scratch/read-phase32.md` (Sonnet, 471 lines):
one row per missing case with the TS lines at 862ad75f, what it asserts
and a class (`port`, `seam`, `js-only`; `GAP` marks a feature Go lacks).
**Every line in the map is a map, not a citation**: read the TS at the
pin before porting, the matter.js source before building a feature, and
cite both. Map corrections known: the register's field is `go`; `Node.
SetAutoSubscribe` exists (`controller/client/node.go:513`) — only the
`defaultSubscription` and fabric-filter change is absent;
`TestHostReadDoesNotEmitEvents` already holds ClientEventNotificationTest
#11; the "no TLV certificate encoder" note is wrong and moot (Fable's
tranche).

Pins: matter.js 862ad75f (`git -C ../matter.js show 862ad75f:<path>`),
connectedhomeip 6170af84. Read-only checkouts.

## 0 Working rules for both agents

Exactly phase 31's (`notes/briefs/phase-31-server-tranche.md` §0), with
these names: shared branch `origin/m8/phase-32` (this brief's commit is
its head; it contains Fable's tranche); agent A in `../go-fabric-p32a` on
`m8/phase-32-a`, agent B in `../go-fabric-p32b` on `m8/phase-32-b`; publish
by `git fetch origin && git rebase origin/m8/phase-32 && git push origin
HEAD:m8/phase-32` (fast-forward only, never force; a CHANGELOG or
by_design conflict keeps both sides); handshake files
`~/.cache/go-fabric-scratch/p32/<a|b>.done`; register lists
`~/.cache/go-fabric-scratch/p32/<a|b>/register.md` (plus Fable's
`register-fable.md`, applied by the last agent too); logs under
`~/.cache/go-fabric-scratch/p32/<a|b>/`; `export GOTMPDIR=$HOME/.cache/gotmp`
(mkdir it) before every go/make run. The last agent: applies the three
register lists (`missing`, `go`, `left`, `reason`), `make testparity`,
the ratchets (`testparityPortedRatchet`, `testparityMissingRatchet`,
`testparityLeftRatchet`), the whole bundle under the flock with
`~/.cache/go-fabric-scratch/p32/summary.log` (`make ci`, `make race`,
`make cover-check`, `make reachability-check`, `make cite-check`,
`make apidiff-check`, `make testparity-check`, `make ahead-check`,
`mkdocs build --strict`, `make chiptool-test` — the subscription and DCL
paths move), then the pull request against main (title and body in
English, body = report, link to the first agent's report file, no labels,
auto-merge off, no attribution lines). Commits as `SukramJ
<sukramj@icloud.com>` (check `git var GIT_AUTHOR_IDENT`), `<package>:
<what> (phase 32)`. Security paths (`secure/`, `commissioning/`, ACL, the
four hand-written core servers) are not touched by either agent — a
needed change there is an open question for Fable. Report shape as
phase 31.

## A Agent A — DCL, OTA and the protocol remainder

Files: `controller/dcl/`, `controller/softwareupdate/`, `controller/ota*.go`
and the OTA rig under `controller/` (not `controller/client/`), `bdx/`,
`cluster/core/ota_*.go` only if a port needs a device-side seam (say so),
plus `fabrictest/` files no agent-B deliverable names. Map sections:
DclOtaUpdateServiceTest (`read-phase32.md:193-239`), OtaTest (`:408-428`).

### A.1 Ports: DclOtaUpdateServiceTest 16 `port` rows
By meaning, each citing its TS case.

### A.2 GAP: the image store (DclOtaUpdateServiceTest 15 rows)
`controller/dcl/images.go`: a force flag on `Images.DownloadUpdate`
(`:467`; rows #9, #18, #30, #35), delete by vendor/product/mode pattern
with a count (`:348`; #2, #3, #6, #8, #20, #21), the spec version recorded
on reuse (`:469-475`; #15), unreadable meta removed (`:298-303`; #17), an
unreadable blob removed online (`:469-476`; #34), the checksum check of a
stored image (`:245-275`; #31), `createUpdateInfoFromFile` (#36) — each
after matterjs's `DclOtaUpdateService.ts` (find and cite the function),
with the parity case.

### A.3 OtaTest 11 `seam` rows
The hooks the map names (`:408-428`): build the seam once in the OTA rig,
port every row that needs it. A seam is test support, never a change of
the served behaviour.

### A.4 js-only rows (DclOtaUpdateServiceTest 5)
`left` rows with class and reason, verified against the map's cited line.

### A.5 Two phase-31 follow-ups (owner decisions 2026-11-04)

- **UniqueID rotates after a factory reset**, as matter.js draws a fresh one
  (`packages/node/src/testing/node-helpers.ts:164-165` at 862ad75f; the
  reset case in `node/server_node_factory_reset_parity_matterjs_test.go`).
  `Node.FactoryReset` (`node/factory_reset.go`, `eraseStores`) writes a
  fresh random pin (16 random bytes, hex, as `DeriveUniqueID`'s shape) under
  `store.SettingUniqueID` into the erased credential store, so the node that
  comes back serves it (`node/persist.go:218-236` `persisted.uniqueID` reads
  a stored pin first). Not with UniqueID rotation on (`bootid.Salt` non-zero:
  the pin is never read). Extend the `online` reset case: the UniqueID served
  after the reset differs from the one before. Shorten the "re-pinned
  UniqueID is derived again" sentence of `BD-Matter-FactoryResetEndsTheNode`
  in `notes/parity/by_design.md` accordingly. CHANGELOG Changed line.
- **`FreshAssertedAtRoot` at debug** in `bridge/devicetype_validation.go`:
  the warn loop logs the Root-asserted violations of the node endpoint at
  debug, as matter.js `DeviceTypeConformanceService.ts:438-441` does
  (verdict field added by phase 31, B.4). One test on the existing warn-mode
  harness.

## B Agent B — the controller/client tranche

Files: `controller/client/` (all four files' tests and the feature code),
`client/interaction/sustained.go` (the `Updated` error, #24/#25),
`controller/controller.go` and `controller/fabricops.go` only for the
registry rows below, `node/root.go:87-89` (PowerSource on the root, row
#86) and `node/node.go` for the Leave-event flush (row #82), `fabrictest/`
(multi-address device, restart with changed shape, store hook). New
seams in per-file `export_*_test.go` files, not in the shared
`export_test.go`. Map sections: ClientNodeTest (`:11-127`), IcdClientTest
(`:129-191`), ClientSubscribeTest (`:241-276`), ClientEventNotificationTest
(`:278-311`).

### B.1 Ports: ClientNodeTest 6, IcdClientTest 21, ClientSubscribeTest 1, ClientEventNotificationTest 3
By meaning. ClientEventNotificationTest #11 is already held by
`TestHostReadDoesNotEmitEvents`: register bookkeeping.

### B.2 Hook-only seams (`seam` rows without GAP)
The store wrapper through `fabrictest.ControllerOptions.Registry`
(`fabrictest/controller.go:63`; ClientSubscribeTest #10, #12-#14,
IcdClientTest #3, #34-#36, ClientNodeTest #69, #104), the held-datagram
and restart seams the map names; the IcdClientTest rows marked
HoldICDState are `left: js-runtime-only` if, after reading the TS, they
assert matter.js's transaction model and nothing a Go seam can hold —
say which in the report.

### B.3 GAP: default subscription at runtime and the cache's fabric filter (ClientSubscribeTest 21)
matter.js: the runtime change of `defaultSubscription` and the recorded
fabric filter with adopt (`controller/client/structure.go:72-74`,
`node.go:164-169` are the Go anchors); `Updated` returns an error
(`client/interaction/sustained.go:139`; #24, #25). Cite `ClientNode.ts` /
`ClientStructure.ts` at the pin.

### B.4 GAP: the client registry (ClientNodeTest 13)
An explicit local id on commission (`fabricops.go:116-136`; #8), the
scanner-forget across scanners (#13-#17), the `fabricIndexOnPeer` backfill
on reload (#81), PowerSource on the root (`node/root.go:87-89`; #86), the
device's Leave event flushed by `node.RemoveFabric` (`node/node.go:1038`,
`EmitLeave` `:520`; #82). **Not** the sequential node-id allocator (#9,
#83, #97, #98): owner decision — write the `by_design.md` paragraph
`BD-Matter-ControllerNodeIDRandom` (matter.js's allocator cited;
reason: several administrators on one fabric, host records) and the four
`left: by-design` rows.

### B.5 GAP: ICD key refresh and the 1.3.0 node (IcdClientTest 4)
The key stays offered when the store write fails
(`controller/client/icd.go:603-606, 1090-1135`; #1, #2, #16); a node that
reports spec 1.3.0 (#50, `cluster.SpecificationVersion`).

### B.6 js-only rows (ClientNodeTest 70, IcdClientTest 4, ClientEventNotificationTest 17)
`left` rows: the write-through family names `BD-Matter-ClientNode-WriteNotThroughCache`
(`notes/parity/by_design.md:2902-2918`), the observer/emit-queue family
the single `Hooks.Event` (`controller/client/config.go:123`); each row's
class and reason verified against the map's cited line.

## Acceptance
`testparityMissingRatchet`: 312 cases leave `missing` (ported or `left`)
but the ones a report names as still missing with a reason;
`testparityLeftRatchet` counts them; every GAP feature cites its source and
has a parity test; the node-id by_design paragraph exists; one pull
request; guards green at the final head.
