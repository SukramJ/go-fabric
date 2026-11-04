# Brief phase 31 — the server tranche of the matter.js test port

Date 2026-11-03. Milestone 8 phase 31 (`docs/roadmap.md`): the 332 `it`
cases of eleven partial register entries, ported by meaning, and the
behaviour they need. Owner decisions 2026-11-03 (wizard): (1) a case that
cannot be ported leaves `missing` into a new per-case register field
`left` with its class and reason; (2) **every one of the 29 feature gaps
the map names is built in this phase** (none deferred); (3) **two Opus
agents on one branch and one pull request**, on disjoint files, the guard
bundle run once at the end by the agent that finishes last. Also in this
phase, by owner decision 2026-11-03: `WiFiNetworkCommissioning.
OnCommissioningComplete` takes the request context (agent A, §A.6).

Map: `~/.cache/go-fabric-scratch/read-phase31.md` (Sonnet, 527 lines): one
row per missing case with the TS lines at 862ad75f, what it asserts and a
class (`port`, `seam`, `js-only`; `GAP` marks a feature Go lacks). **Every
line in the map is a map, not a citation**: read the TS at the pin before
porting, read the matter.js source before building a feature, and cite
both in the Go test and the Go code (`// Mirrors matter.js …`). The
map's TS line numbers were spot-checked and are exact; its register
field name is `go`, not `goTests`.

Pins: matter.js 862ad75f (`git -C ../matter.js show 862ad75f:<path>`),
connectedhomeip 6170af84 (`git -C ../connectedhomeip show 6170af84:<path>`).
Both checkouts are read-only.

## 0 Working rules for both agents

- **Branch and worktrees.** The shared branch is `origin/m8/phase-31`
  (this brief is its first commit). Agent A works in
  `../go-fabric-p31a` on local branch `m8/phase-31-a`, agent B in
  `../go-fabric-p31b` on `m8/phase-31-b`, both cut from this commit. To
  publish: `git fetch origin && git rebase origin/m8/phase-31 && git
  push origin HEAD:m8/phase-31` — fast-forward only, **never force**; a
  rebase conflict can only be in `CHANGELOG.md` or
  `notes/parity/by_design.md` (keep both sides) because the file sets
  below are disjoint. Push after every finished deliverable, not only at
  the end, so the other agent rebases on small steps.
- **Disjoint files.** Agent A owns `bridge/`, `node/`, `fabrictest/`,
  `im/`, `controller/`, `store/`, `cluster/core/network_commissioning_wifi.go`
  and, for the lazy attestation, the attestation resolution in
  `cluster/core/operational_credentials.go`; agent B owns
  `endpoint/devicetype_*`, `endpoint/binding_mount_test.go`,
  `cluster/spec/`, `device/`, `script/testparity/` and
  `notes/parity/matterjs_tests.md`. Agent A's two identity rows
  (ServerNodeTest map rows 30 and 38) go into a new file
  `endpoint/identity_parity_matterjs_test.go`; no other agent-A file under
  `endpoint/`. `schema/` and `parity/` are not touched by either.
- **The register and the ratchets are edited once, by the agent that
  finishes last** (§0 "finishing"); until then each agent keeps a list
  `~/.cache/go-fabric-scratch/p31/<a|b>/register.md` of the register
  edits its ports require: per TS file the `it` names that leave
  `missing`, the Go test (`package:TestName`, a top-level function —
  `register_test.go:116` resolves only those) that holds each, and the
  `left` rows (§B.1) with class and reason.
- **Guards while working:** `go test -race` and `golangci-lint run` on the
  packages you change, under `flock ~/.cache/go-fabric-scratch/guards.lock`,
  `export PATH=$HOME/go/bin:$PATH`, logs under
  `~/.cache/go-fabric-scratch/p31/<a|b>/`, read by `grep`. Not the whole
  `make ci` per agent.
- **Finishing.** When your deliverables are pushed, write
  `~/.cache/go-fabric-scratch/p31/<a|b>.done` (one line: your HEAD). If the
  other agent's `.done` does **not** exist yet, hand back now (report
  shape below) — you are first, no pull request. If it **does** exist,
  you are last: rebase on `origin/m8/phase-31`, apply both register lists
  (§B.1 field, `missing`, `go`, `left`, `reason`; `make testparity`;
  set `testparityPortedRatchet`, `testparityMissingRatchet`, the new
  `testparityLeftRatchet` in `script/testparity/register_test.go`), then
  run the whole bundle once for the branch under the flock, with a
  summary log `~/.cache/go-fabric-scratch/p31/summary.log` (HEAD before
  and after, one `exit` line per guard): `make ci`, `make race`,
  `make cover-check`, `make reachability-check`, `make cite-check`,
  `make apidiff-check`, `make testparity-check`, `make ahead-check`,
  `mkdocs build --strict`, `make chiptool-test` (the IM behaviour moves:
  WildcardPathFlags, BUSY; the chip-tool harness is yours alone), and
  `GOFABRIC_CHIP_FAMILIES=IDM make chiptool-families`. Fix what fails in
  your files; a failure in the other agent's files is reported, not
  fixed. Then open the pull request against `main` from `m8/phase-31`
  (title and body in English, body = your report plus a link to the
  first agent's report file, no labels, auto-merge off, no attribution
  lines) and hand back.
- **Commits.** `git var GIT_AUTHOR_IDENT` must say `SukramJ
  <sukramj@icloud.com>`; commit with `git -c user.name=SukramJ -c
  user.email=sukramj@icloud.com commit …`; no Co-Authored-By or other
  attribution lines; never commit CSA XML or spec text. One commit per
  deliverable, subject `<package>: <what> (phase 31)`.
- **Security paths.** `secure/`, `commissioning/`, the ACL code and the
  four hand-written core servers (AccessControl, OperationalCredentials,
  GroupKeyManagement, GeneralCommissioning) are Fable's. Agent A's single
  allowed edit in `cluster/core/operational_credentials.go` is §A.5's
  lazy resolution of the attestation material; anything beyond it, or
  any other change on those paths, is an **open question** in the
  report, not a patch. Fable reads §A.5 in full before the merge.
- **by_design and findings.** A GAP feature that follows matter.js is a
  `// Mirrors` citation and a parity test, nothing in `by_design.md`. A
  case whose Go behaviour deliberately differs is one paragraph in
  `notes/parity/by_design.md` with the citations and one sentence of
  reasoning, and that case's `left` row names the entry. The findings
  entries a feature closes are removed from
  `notes/parity/matter_behaviour_findings.md` (WildcardPathFlags `:504`,
  BUSY `:418` — agent A; `assertedAtRoot` and the per-bit bitmap gap if
  listed — agent B) and the `pin-lifts.md` rows #4768 / #4746 get the
  verdict "closed (phase 31)".
- **CHANGELOG.** `[Unreleased]`: `### Added` for each new feature and
  API, `### Changed` for the API breaks (§A.6, §A.4 if `store.Store`
  gains a method). Written by each agent for its own deliverables.
- **Report shape** (SubagentHandback, and the PR body for the last):
  per § — what landed, Go tests by name, the TS cases they port (count);
  guards table (guard, HEAD, exit); differences from the brief;
  decisions; left out; open questions; `by_design` entries written;
  every matter.js / chip citation written, re-read at the pin.

## A Agent A — wire and node

Files: `bridge/`, `node/`, `fabrictest/`, `im/` (incl. `im/subscription`),
`controller/`, `store/`, `cluster/core/network_commissioning_wifi.go`,
the §A.5 edit, `endpoint/identity_parity_matterjs_test.go` (new).
Map sections: ServerNodeTest (`read-phase31.md:8-86`), InteractionProtocolTest
(`:148-198`), ServerSubscriptionTest (`:290-326`), SuppressResponseTest
(`:426-448`). Harnesses: `node/parity_matterjs_helpers_test.go`
(`quietSite`, `parityCtx`), `fabrictest/{raw,wire,device,multicast}.go`,
`bridge/{receive_test,suppress_response_parity_matterjs_test,
write_chunked_dispatch_test,secure_harness_test,groups_harness_test}.go`,
`im/subscription/{manager_test,handler_parity_test,
server_subscription_parity_matterjs_test}.go`.

### A.1 Ports (`port` rows): ServerNodeTest 7, InteractionProtocolTest 27, ServerSubscriptionTest 8, SuppressResponseTest 9

Each a Go test by meaning, citing the TS case (`file:lines` at 862ad75f).
The map's "partly held" rows (SuppressResponseTest 2 and 4, ServerNodeTest
37 and 39): extend the existing Go test rather than add a twin, and name
it in the register list. ServerNodeTest row 36 is already held
(`node/endpoint_number_reservation_parity_matterjs_test.go:137`):
register bookkeeping only.

### A.2 Hook-only seams (`seam` rows without GAP)

Build the seam once, port every row that needs it:
- a multi-message raw exchange helper and a failing report sender in
  `fabrictest`/`bridge` (SuppressResponseTest row 6, InteractionProtocolTest
  rows 8 and 27, ServerSubscriptionTest rows 1, 3–8, 26, 27);
- a session without MRP (TCP-style) for ServerSubscriptionTest rows 4–5;
- an mDNS scanner on the in-memory multicast net (ServerNodeTest row 4);
- a store-level stale-row injection (ServerNodeTest row 56).
A seam is test support (`fabrictest` or `_test.go`), never a change of the
served behaviour.

### A.3 GAP: WildcardPathFlags (InteractionProtocolTest rows 4–6, `:1263-1322`)

AttributePathIB tag 7 is decoded in `im/` and applied on a wildcard read as
matter.js's `AttributeReadResponse.ts` does (`:150-170` at 862ad75f,
`WildcardPathFlagsCodec`, the WildcardFilterConfigurationVersion rule at
`:166`; the flag values in `packages/types/src/globals/WildcardPathFlags.ts`).
Parity test with the map's table (the seven flag cases and their counts),
the version-current and version-stale cases. Findings entry `:504` removed.

### A.4 GAP: BUSY for a write racing a chunked write (InteractionProtocolTest rows 12–16, `:1605-1877`)

A chunked write holds its attribute (`bridge/exchange_routing.go:62`
`writeTxs`); another exchange's concrete write of it is answered BUSY, a
wildcard write skips it, the hold moves with the chunk and lasts until the
chunk's writes are done — matter.js `InteractionServer.ts:505-528`
(`#isHeldByOther`, `Status.Busy`), chip `WriteHandler.cpp` as the second
authority where matter.js leaves a detail open. Five parity tests on two
exchanges (seam §A.2 helper). Findings `:418` removed, pin-lifts #4768
closed.

### A.5 GAP: lazy attestation provider (ServerNodeTest row 10, `:409-459`)

matter.js builds its `DeviceCertification` on first use
(`OperationalCredentialsServer.ts:560-572` `getCertification`, the
`certification?:` state at `:609/:622`). Go resolves the material at
construction (`node/attestation.go:26` `resolveAttestation`) and the server
takes the bytes (`cluster/core/operational_credentials.go:372-375`,
`:470`). Add `node.Attestation.Provider func(ctx) (…)` (shape yours; the
existing fields stay the default) resolved on the first AttestationRequest
or CSRRequest, never at `node.New`/`Start`; the server change is limited to
reading the material through a func on first use. Parity test: the
provider is not called at creation or start, once when the commissioner
asks. **Fable reads the server edit in full.**

### A.6 GAP: node erase / factory reset and controller erase (ServerNodeTest rows 15–18, 20, 26, 44, 53, 54; DeviceTypeConformanceServiceTest row 24 is agent B's test on your API)

matter.js `ServerNode.ts:159-259` (`erase`, `eraseWithMutex`, the
factory-reset list: peers, credentials, the store) and the controller's
erase in `ClientNode`/`Site` (find and cite). Go: `Node.FactoryReset(ctx)`
(name yours) — stop if running, wipe the node's store and every in-memory
state a reset erases, come back if it was running — and the controller
counterpart that erases peers and CA key material. The store port gets
what it needs (`store/port.go`; `sqlite.go` and `memstore` implement it;
`### Changed` if `store.Store` grows). Fabric removal runs through the
existing `node.RemoveFabric` fan-out (`node/node.go:1028-1032`), not
through the OperationalCredentials server directly. Parity tests for
every listed row. Expose a hook agent B can use for "every endpoint is
reported again after a factory reset" (the validator state reset is
`endpoint/devicetype_validation.go:388` `NewDeviceTypeValidator`): tell
agent B its name in your register list file.

### A.7 API: `OnCommissioningComplete(ctx)` (owner decision)

`cluster/core/network_commissioning_wifi.go` `OnCommissioningComplete`
takes the request context and passes it to `saveCommittedLocked`;
`node/root.go:299` hands the hook's ctx through, the `//nolint:contextcheck`
goes. `### Changed` line (API break). The persist tests in
`network_commissioning_wifi_persist_test.go` follow.

### A.8 js-only rows (ServerNodeTest 44, InteractionProtocolTest 2, ServerSubscriptionTest 10, SuppressResponseTest 3)

No Go test. Each becomes a `left` row in your register list with the
map's class and a one-line reason; a row the map ties to a `by_design`
entry names it. Verify the map's reason before copying it.

## B Agent B — validation and the register

Files: `endpoint/devicetype_*`, `endpoint/binding_mount_test.go`,
`endpoint/devicetype_validation.go` and `endpoint/devicetype_conformance.go`
(the feature code of §B.4/§B.6), `cluster/spec/`, `device/`,
`script/testparity/`, `notes/parity/matterjs_tests.md`. Map sections:
DeviceTypeConformanceTest (`read-phase31.md:88-146`),
DeviceTypeConformanceServiceTest (`:328-361`), RuntimeValidationTest
(`:251-288`), StandardDeviceTypeCorpusTest (`:397-424`), ValueValidatorTest
(`:200-249`), ValidatedElementsTest (`:363-395`), constraintTest
(`:450-470`). Harnesses: `endpoint/devicetype_fakes_test.go`,
`endpoint/devicetype_service_helpers_test.go`, `cluster/spec/{write_test,
server_handlers_test,instance_test,conformance_test,server_values_test}.go`.

### B.1 The register's `left` field (first deliverable, so both lists can use it)

`script/testparity/register.go`: `Entry.Left []Left` with `Left{It string
"it", Class string "class", Reason string "reason"}`; classes = the
register's existing exclusion classes plus `by-design` (the reason names
the `by_design.md` entry) and `skip` (an existing `t.Skip` subtest names
the case; RuntimeValidationTest's 27 rows, `endpoint/
runtime_validation_parity_matterjs_test.go:44-48`). Validation in
`register_test.go`: a `left` case is not also in `missing`; `left` needs a
class and a reason; `left` is allowed on `partial` and `ported` entries
(a `ported` entry may carry `left`, still not `missing`); a `left` row's
`it` must be a describe/it name of the snapshot where the snapshot holds
it (keep the existing free-text rule for `missing`). A new exact ratchet
`testparityLeftRatchet` beside the three at `register_test.go:41,60,61`.
`render.go`: a "Left: …" sentence after "Missing: …" with class in
parentheses; `make testparity` rerenders the page. The `-render`/`-check`
documentation in `script/testparity/main.go`'s head comment and
`docs/matter-parity-contract.md`'s testparity paragraph name the field.

### B.2 Ports (`port` rows): DeviceTypeConformanceTest 40, DeviceTypeConformanceServiceTest 12, ValueValidatorTest 19, ValidatedElementsTest 5, constraintTest 8

By meaning, each citing its TS case. "Partly held" rows
(DeviceTypeConformanceTest 17, 20, 21, 25): extend the existing Go test.
ValueValidatorTest's "accepts" bit rows (8–10, 14–15, 18, 20) pass today
because only defined bits are checked: port them as guards beside §B.5.

### B.3 Bookkeeping: StandardDeviceTypeCorpusTest

Twelve `judges <Name>` rows already run and pass
(`script/devicegen/devicetypes.go:139` `excluded` empty since 27-C2; the
Go test skips 5 subtests). Verify by running
`go test ./device/ -run StandardDeviceTypeCorpus -v`, then they leave
`missing` with the existing `device:TestStandardDeviceTypeCorpusParityMatterJS`
in `go`. The 5 skipped are `left` rows (class `skip`) unless a skip reason
no longer holds — then port.

### B.4 GAP: `assertedAtRoot` (DeviceTypeConformanceServiceTest rows 10–13, `:229-312`)

matter.js `packages/model/src/logic/device-types/DeviceTypeConformance.ts:65-76`
(a violation that goes away without the Root assertions is marked
`assertedAtRoot`; the unasserted re-check) and `ConditionAssertions.ts:63-68`
(`rootAssertedOf`). Go: `endpoint/devicetype_conformance.go:503-526`
(`assertedOn`) gains the root-asserted set; a violation only a Root
assertion explains is logged at debug and never refuses, even strict; the
others are reported apart. Four parity tests. pin-lifts #4746 closed; the
findings entry, if present, removed.

### B.5 GAP: per-bit bitmap conformance on write (ValueValidatorTest rows 11–13, 16, 17, `:205-257`)

`cluster/spec/write.go:262` refuses undefined bits only. matter.js
`ValueValidator.ts:198-240` (`createBitmapValidator`: per-member
conformance from `BitmapMember.Conformance`, multi-bit members by
Bit/Width, judged by whether the write sets any bit of the range). Go:
evaluate each set member's conformance against the instance's features
(`model.go:236` `BitmapMember.Conformance`, the existing conformance
evaluator of `cluster/spec`), with ConformanceError → CONSTRAINT_ERROR as
the existing negative-write table maps it (`cluster:TestNegativeWriteParity`
stays green). Five parity tests plus the WindowCovering wire twin check
(row 17 names `node:TestBitmapWriteValidationParity`).

### B.6 GAP: element-ID legality and strict-mode listing (ValidatedElementsTest rows 12, 17, 18, 23, `:528-668`)

Find the check matter.js's cases exercise (the `ValidatedElements.ts`
path that refuses `0xfff1_5000`-style attribute, command and event ids
and an illegal cluster id; cite it) and build it in `spec.New`
(`cluster/spec/instance.go:248`): illegal ids refused together, each named;
strict mode lists every mandatory command without a handler instead of
stopping at the first (`server.go:200`); an endpoint hosting a cluster
with an illegal id fails to initialise with the cause. Four parity tests.

### B.7 GAP: TagList required for duplicates (DeviceTypeConformanceTest rows 30, 33; StandardDeviceTypeCorpusTest row 1)

Phase 30 gave the kit `Options.Tags`/`Spec.Tags` and the Descriptor
`EnableTagList`. matter.js requires TagList of endpoints that duplicate a
sibling's device type and of the Closure corpus device; `cluster/core/
descriptor.go:250-253` keeps TagList off by default for Apple. Build the
check in the conformance judge as matter.js has it; if the Apple default
makes the judge refuse a composition the kit builds by default, the
refusal is a warning in `off`/`warn` mode and the difference is one
`by_design.md` paragraph (owner reason: Apple). Three parity tests.

### B.8 js-only rows (DeviceTypeConformanceTest 4, ServiceTest 4, RuntimeValidationTest 27, StandardDeviceTypeCorpusTest 4, ValueValidatorTest 14, ValidatedElementsTest 5, constraintTest 3)

`left` rows in your register list, class and reason verified against the
map's cited line (BLE scope rows: CLAUDE.md scope boundaries; ADR 0016
rows: name the ADR; `by_design.md:3363` rows: name the entry).

## Out of scope

The other tranches (phases 32–35), any `schema/` or snapshot change, the
border of the controller role beyond the erase rows, a release.

## Acceptance

`testparityMissingRatchet` 1130 → ≤ 810 (1130 − 332 + the rows that stay
`missing` with a named reason, if any — say which); `testparityLeftRatchet`
= the number of `left` rows; the eleven entries' `go` lists resolve; every
GAP feature has a `// Mirrors` citation and a parity test; the chip-tool
suite and the IDM family are green on the branch; one pull request.
