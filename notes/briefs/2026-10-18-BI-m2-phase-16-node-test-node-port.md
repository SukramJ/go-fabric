# Brief BI — milestone 2, phase 16: the port of `packages/node/test/node`

Owner: Fable. Implementer: Opus, one agent at a time on one branch; a
later agent resumes from the report. Concept: `docs/concept-v4-test-foundation.md`
§A.3 (the register), §A.5 (owner decisions 0.9–0.12, the tranches, how a
port is built) and Teil B. Base: `origin/main` after the phase-16
preparation merged. Branch `m2/phase-16-node`, one PR against `main` at
the end, report `notes/briefs/reports/BI.md` (one section per tranche,
appended as you go). `CHANGELOG.md` under `[Unreleased]` is yours for
fixes; `notes/parity/` (except the generated `matterjs_tests.md`),
`docs/adr/`, `CLAUDE.md` are the owner's. Brief BH runs in parallel on
`m2/phase-16-protocol` (`transport/`, `secure/`, `mdns/`, `bdx/`, `groups/`,
`controller/{peer,discovery,…}`); do not touch those paths except for a
fix that a test proves, and name it in the report. The chip-tool harness
is yours alone in this phase.

Read `CLAUDE.md`, `notes/briefs/README.md`, the concept sections, the
register page `notes/parity/matterjs_tests.md` (the `node/test/node`
tables), the Sonnet map `notes/reference/read-matterjs-tests-862ad75f.md`
lines 454–537 (a map, never a citation), `docs/guides/testing-in-process.md`,
`fabrictest/doc.go`, `fabrictest/guide_test.go`, `fabrictest/site_test.go`
and `controller/interaction/live_test.go` (how a test commissions a pair,
acts on the device, steps the clock, restarts). Every matter.js file is
read at the pin via `git -C ../matter.js show 862ad75f:<path>` — never the
working tree. The helpers the tests use are
`packages/node/src/testing/mock-site.ts`, `mock-server-node.ts`,
`node-helpers.ts` (`CommissioningHelper` :181, `namespace interaction` :317,
`seedPeerCache` :520, `subscribedPeer` :548) and
`packages/testing/src/mocks/time.ts`; `fabrictest` is their Go counterpart
(§A.4), and what it lacks for a test is added to it with a test of its own
and named in the report.

## Scope

Every `planned` and `partial` entry of `script/testparity/register.json`
under `packages/node/test/node/`: 44 files, about 690 `it` cases at the
pin. `excluded` entries stay excluded (the four helper tests
`MockSiteTest`, `MockServerNodeTest`, `SettledTest`, `LifetimeReleaseTest`
among them); a port that contradicts an entry's class is named in the
report, the reclassification is the owner's.

## Tranches (in this order, each ending green, pushed and reported)

| Tranche | Files under `packages/node/test/node/` | Where the Go tests live |
| --- | --- | --- |
| BI-1 device side | `AttributeReadResponseTest`, `AttributeWriteResponseTest`, `AttributeSubscriptionResponseTest`, `CommandInvokeResponseTest`, `EventReadResponseTest`, `BitmapWriteValidationTest`, `DotQualifiedConstraintTest`, `DelayReportDataTest`, `ServerNodeTest`, `ServerSubscriptionTest`, `FailsafeCommissioningTest`, `CommissioningPaseLifetimeTest`, `RegulatoryConfigCommissioningTest`, `ScanNetworksRejectionCommissioningTest`, `SetRegulatoryConfigSideEffectTest`, `SplitCommissioningTest`, `ReportedSessionParametersTest` | `node/*_parity_matterjs_test.go` (package `node_test`) on `fabrictest`; a pure IM case may sit in `im/` beside `imrevision_absent_parity_matterjs_test.go` |
| BI-2 controller side | `Client*Test` (all), `CaseConnectionTimeoutTest`, `DecommissionTest`, `NodeLifecycleSeededTest`, `NodePhysicalPropertiesTest`, `PeerBehaviorTest`, `PeerDescriptorTest`, `ReceivedStatusTest`, `client/ClientNodeSemanticTagTest`, `client/Peers*Test` | `controller/client/*_parity_matterjs_test.go` on `fabrictest` |

A file whose subject the register's reason places elsewhere (device or
controller) moves to the other tranche; say so in the report. A tranche
that does not fit the agent's remaining budget is left whole for the next
agent: finish the current file, commit, push, report, and say in the
hand-back which tranche and which file is next.

## How a port is built (concept §A.5, binding)

1. **One Go file per matter.js file**, `<subject>_parity_matterjs_test.go`,
   opening with the licence header and
   `// A port of matter.js packages/node/test/node/<File>Test.ts at 862ad75f: …`.
   One Go test per `describe` group with subtests carrying the `it` names,
   or one test per `it` — the register's `go` list names every test
   function.
2. **Vectors and expectations come from the matter.js file at the pin**,
   never from the Go code under test. An expectation our code does not
   meet is a fix (3) or a `missing` with its reason — never an adjusted
   number. Where matter.js asserts on its own behavior names or TypeScript
   error classes, the Go assertion is the IM status, the wire payload or
   the Go error the module defines — say which in the test.
3. **Fixes (owner decision 0.11).** When the port shows a behaviour gap and
   the matter.js source is unambiguous, fix it in this wave: the
   production change cites the matter.js file and lines
   (`// Mirrors matter.js packages/…:lines`), the ported test proves it,
   `CHANGELOG.md` `[Unreleased]` names it under `### Fixed`, and the report
   lists it under **Fixes** with the lines. The first known one: a read of
   a concrete event path on an unknown endpoint, cluster or event answers
   no EventStatusIB (`im/read.go:598-640`; matter.js
   `packages/protocol/src/action/server/EventReadResponse.ts:204-218`:
   View-privilege pass, then UnsupportedEndpoint / UnsupportedCluster /
   UnsupportedEvent) — `EventReadResponseTest.ts` proves it; the
   register entry in `notes/parity/matter_behaviour_findings.md` is the
   owner's to close. Anything unclear, anything that changes a wire shape,
   the store schema or `secure/`, and anything over a few dozen lines
   stays `missing` with a reason and goes to the report under **Register
   candidates**. A fix in brief BH's paths is named in the report.
4. **Every test runs on `fabrictest`** unless it needs no node: `NewSite`
   with the default `clocktest.Fake`, `CommissionedPair` or
   `UncommissionedPair`, the wire helpers, `Device.Act` for local state,
   `Device.Restart`, `Site.Eventually` for effects, `Net().Set` / `Filter`
   for loss. No `time.Sleep`. A test that needs sockets uses
   `Options.Loopback`.
5. **Partial entries:** the existing `go` tests stay; add the `missing`
   cases and empty `missing`. An `it` that cannot be ported keeps its
   `missing` line with a reason appended to the register's `reason`.
6. **The register after every file:** `status`, `go`, `missing`,
   `snapshotSha`; `make testparity` regenerates the page;
   `testparityPlannedRatchet` moves in the same commit.
7. **Guards per tranche:** `make ci`, `make race`, `make testparity-check`,
   `make cite-check`, `make reachability-check`, `make cover-check`
   (`fabrictest`'s floor of 85 may only rise). `make chiptool-test` after
   every fix that moved device behaviour (the event-status fix among
   them); say when it was skipped and why. Background runs with a summary
   log; read the summary, grep a log on failure.
8. **Commits** per file or per small group, message
   `m2/phase-16 BI-n: <File>Test.ts → <package> (<n> cases, <m> missing)`.

## Report shape (`notes/briefs/reports/BI.md`)

Per tranche: the files with their Go test file and the case count ported
and missing; **Fixes** (production file, matter.js lines, one sentence);
**fabrictest additions** (symbol, why, its test); **Register candidates**;
**Guards** with HEAD and one exit line each; **Differences** to this
brief; **Decisions** the owner should check; **Left out**; **Questions**;
`by_design` candidates (one paragraph each with citations — the owner
writes the entry).

## Out of scope

Every `excluded` entry (the ICD and TCP controller files carry `until`
M5); `packages/node/test/behavior*` (milestone 3); `packages/protocol/test`
(brief BH); the `controller/commissioner` `startNode` (report BG,
decision 1); the registry live test's own wiring may move onto
`fabrictest` in BI-2 if a `Peers*` port touches it (name it).
