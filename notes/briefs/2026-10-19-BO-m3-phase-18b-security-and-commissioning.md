# Brief BO — milestone 3, phase 18b: AccessControl, OperationalCredentials, GroupKeyManagement, the system commissioning tests, the node wiring of 18a

Owner: Fable. Implementer: Opus, one agent on one branch; a later agent
resumes from the report. Concept: `docs/concept-v5-behaviour-depth.md`
Teil E (owner decisions E.0 18.1–18.3, §E.2 what a port is here, §E.3,
§E.6). Base: `origin/main` at or after `v0.8.5` (BN, #182, merged).
Branch `m3/phase-18b-security`, one PR against `main` at the end carrying
the label `needs-chiptool`, report `notes/briefs/reports/BO.md`.
`CHANGELOG.md` under `[Unreleased]` is yours for fixes; `notes/parity/`
(except the generated `matterjs_tests.md`), `docs/adr/`, `CLAUDE.md` are
the owner's. No wave runs in parallel (E.0 18.3): you own every path you
touch, and you may run the chip-tool harness yourself.

**This wave is on the security path** (owner decision 18.1): AccessControl,
OperationalCredentials, GroupKeyManagement, `commissioning/`, `secure/`
and the commissioning-window logic of the bridge. The owner reads every
cited fix line against the pin before the pull request merges. The rule
for you: a fix in these packages is its own commit with the matter.js
file and lines in the message and in a `// Mirrors …` comment; a fix you
cannot cite line by line is not made — the case stays `missing` with the
reason "needs the owner's read". Never widen what a controller may do
(an ACL check, a certificate check, a fail-safe rule) to make a case
pass; a port that would need that is a question in the report.

Read `CLAUDE.md`, `notes/briefs/README.md`, the concept Teil E, the
register page `notes/parity/matterjs_tests.md` (the tables of
`access-control`, `operational-credentials`, `group-key-management` and
`behavior/system/commissioning`), the Sonnet map
`notes/briefs/reads/read-m3-phase-18-system-set.md` §1.3–§1.5, §1.10,
§1.11, §3.3–§3.5, §3.10, §3.11 and §4 (a map, never a citation), the BN
report `notes/briefs/reports/BN.md` (the last wave; its **Register
candidates** are four items of this wave), the BN block of
`notes/parity/matter_behaviour_findings.md`,
`docs/guides/testing-in-process.md`, `fabrictest/doc.go`, and one existing
parity file of each package you touch
(`cluster/core/access_control_parity_test.go`,
`cluster/core/operational_credentials_parity_test.go`,
`cluster/core/group_key_management_parity_matterjs_test.go`,
`bridge/commissioning_window_test.go`, `node/issue87_test.go` as a node
test on `fabrictest`). Every matter.js file is read at the pin via
`git -C ../matter.js show 862ad75f:<path>` — never the working tree (a
worktree has no `../matter.js`: use
`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js` for `make` and the
absolute path for `git -C`). The matter.js helpers are
`packages/node/src/testing/mock-server-node.ts`, `node-helpers.ts`
(`MockServerNode`, `MockSite`, `interaction`) and
`packages/protocol/src/testing/TestFabric.ts`; `fabrictest` is their Go
counterpart; what it lacks for a test is added to it with a test of its
own and named in the report.

## Scope

Six matter.js test files, 42 cases open at the pin, and the four node
wirings 18a left as register candidates:

| File | Harness | Cases open | Register today | Go subject |
| --- | --- | ---: | --- | --- |
| `access-control/AccessControlAuxiliaryTest.ts` | `MockServerNode` + `MockSite` | 9 | partial, 4 `go` | `cluster/core/access_control.go`, `access_control_auxiliary.go`, `groups/` (auxiliary entries), `endpoint/dispatcher.go` (the ACL check) |
| `operational-credentials/OperationalCredentialsServerTest.ts` | `MockServerNode` + `MockSite` + `TestFabric` | 12 | planned | `cluster/core/operational_credentials.go`, `vid_verification.go`, `secure/` (certificate checks), `store/fabric.go` |
| `group-key-management/GroupKeyManagementServerTest.ts` | `MockServerNode` + `MockSite` | 6 | partial, 2 `go` | `cluster/core/group_key_management.go`, `groups/manager.go`, `store/groupkeys.go` |
| `behavior/system/commissioning/CommissioningWindowLifetimeTest.ts` | `MockServerNode` + `MockSite` | 9 | partial, 7 `go` | `bridge/commissioning_window.go`, `node/commissioning.go`, `cluster/wire/admincommissioning.go` |
| `behavior/system/commissioning/CommissioningRestartTest.ts` | `MockSite` | 2 | planned | `node/` (stop and start on one store), `bridge/commissioning_window.go` |
| `behavior/system/commissioning/FabricBeforeStartTest.ts` | `MockServerNode` + `MockSite` | 4 | planned | `node/`, `store/fabric.go`, `mdns/` (operational advertisement) |

The case titles are the `missing` lists of the register entries (verbatim
`it` titles) and, for the three `planned` files, the `it` lines of the
map §1.4 and the files themselves; the expectations are read from the
matter.js file at the pin. `AccessControlCustomExtensionTest.ts` is out
(owner decision 18.2): its register entry stays `planned` and gets
`"until": "M4"`.

The node wirings (BN's register candidates; the facts are in BN's
findings block, the matter.js lines there):

| Wiring | What | matter.js |
| --- | --- | --- |
| DataModelTest at start | `node.Config` carries the GeneralDiagnostics feature selection and MaxPathsPerInvoke; `Node.Start` runs `core.GeneralDiagnostics.CheckDataModelTest` and fails on its error | `GeneralDiagnosticsServer.ts` `#online` :352, `#assertDataModelTest` :125-143 |
| Grouped ConfigurationVersion | `Aggregator` (the node facade) offers the grouped increase over several device keys and persists the versions once | `BasicInformationServer.ts:132-138`, `BridgedDeviceBasicInformationServer.ts:72-90` |
| CapabilityMinima | `node.Config` carries the caller's CapabilityMinima, defaults from the schema | `CapabilityMinimaTest.ts` ("caller-provided values win") |
| Key set 0 | the two `KeySetsTest.ts` cases: the only key of the default key set, the second-newest key of the IPK key set (§4.14.2.6) — if the manager can hold key set 0 without moving the CASE IPK out of `secure/`; otherwise a question | `packages/protocol/test/groups/KeySetsTest.ts` |

The fourth BN candidate (tags on `endpoint.Spec`) is milestone 4; leave it.

## Audit of the two servers without a matter.js test

GeneralCommissioning and AdministratorCommissioning have no test file at
the pin (map §1.10, §1.11). Their proof stays CGEN and CADMIN in CI and
the three commissioning files above. Your report carries an **Audit**
section: `GeneralCommissioningServer.ts` (285 lines) and
`ServerNodeFailsafeContext.ts` (135) against
`cluster/core/general_commissioning.go`, and
`AdministratorCommissioningServer.ts` (267) against
`bridge/commissioning_window.go` and `cluster/wire/admincommissioning.go`
— one table per server: matter.js member (file:line) → Go function → same
/ differs (one sentence). A difference with an unambiguous source and a
small fix is a fix (rule 4); everything else is a line in the table and
a **Register candidate**. No invented tests.

## How a port is built (concept v5 §A.2, §E.2 and v4 §A.5, binding)

1. **One Go file per matter.js file**, `<subject>_parity_matterjs_test.go`,
   opening with the licence header and
   `// A port of matter.js packages/node/test/behaviors/<dir>/<File>Test.ts at 862ad75f: …`
   (`packages/node/test/behavior/system/commissioning/<File>.ts` for the
   three commissioning files). One Go test per `describe` group with
   subtests carrying the `it` names, or one test per `it`; the register's
   `go` list names every test function.
2. **Harness.** Every file of this wave is a node test: its port runs on
   `fabrictest` (`NewSite`, `AddDevice`, `AddController`, `Device.Act`,
   the wire helpers for `interaction`, `Site.Eventually`), under `node/`
   when it needs a commissioned pair or a restart on one store, or as an
   external test package of `cluster/core` otherwise. `TestFabric` cases
   (OperationalCredentials) build their certificates with the site's
   certificate authority (`Controller.CA`), never with hand-written
   bytes. No `time.Sleep`.
3. **Vectors and expectations come from the matter.js file at the pin**,
   never from the Go code under test. An expectation our code does not
   meet is a fix (4) or a `missing` with its reason — never an adjusted
   number or a loosened check. Where matter.js asserts on TypeScript error
   classes, the Go assertion is the IM status (`InvalidNodeOpId`,
   `InvalidAdminSubject`, `InvalidNOC`, `Busy`, `Failure`) or the Go
   error — say which in the test.
4. **Fixes (owner decision v4 0.11, security rule above).** A behaviour
   gap with an unambiguous matter.js source is fixed in this wave as its
   own commit: the production change cites the file and lines
   (`// Mirrors matter.js packages/node/src/behaviors/…:lines`), the ported
   test proves it, `CHANGELOG.md` names it under `### Fixed`, the report
   lists it under **Fixes**. Likely candidates from the `missing` lists:
   the AddNOC admin-vendor-ID refusals (Matter Standard vendor ID and the
   reserved range), the UpdateNOC rules (group state kept, fail-safe
   expiry restoring the previous credentials, another fabric's NOC
   refused), the "final fabric change" rule for UpdateFabricLabel and
   SetVIDVerificationStatement under a bare fail-safe, the GKM key-count
   limit and the multicast-policy rules, the auxiliary-ACL rules
   (provider entries split to the per-entry limits, unchanged entries
   after a restart, AuxiliaryAccessUpdated only for changed fabrics), and
   the commissioning-window lifetime rules (restart of the node's own
   window, Busy while opening, the close callback). Anything that changes
   a wire shape, a store schema, `contract/` or another package's API, and
   anything over a few dozen lines, stays `missing` with a reason and goes
   to **Register candidates**.
5. **Runtime-only cases (concept §E.2).** `FabricBeforeStartTest.ts`
   "waits for another transaction that holds the commissioning state" is
   matter.js's transaction lock (register `FabricChangeLockTest.ts` is
   already `excluded` for it): port the other three, mark this one
   `missing` with "js-runtime-only: the commissioning-state transaction
   lock" in the reason. Everything a controller can observe is never
   excluded.
6. **Partial entries:** the existing `go` tests stay; add the `missing`
   cases and empty `missing`. The `planned` files become `ported` or
   `partial` with a `missing` list.
7. **The register after every file:** `status`, `go`, `missing`,
   `snapshotSha`; `make testparity` regenerates the page;
   `testparityPlannedRatchet` moves in the same commit.
8. **Guards at the end (and after any fix that moved device behaviour):**
   `make ci`, `make race`, `make testparity-check`, `make cite-check`,
   `make reachability-check`, `make cover-check`; background runs with a
   summary log (HEAD before and after, one exit line per guard). `make race`
   is mandatory here (sessions, fail-safe, windows are concurrent). A fix
   that moved device behaviour also runs the touched families locally
   (`GOFABRIC_CHIP_FAMILIES=ACL,ACE,OPCREDS,CGEN,CADMIN,GRPKEY,IDM make chiptool-families`;
   IDM/10.4 whenever an endpoint's PICS moved; `-update-chip-cases` only
   when the diff is read and explained in the report) and names them in
   the report; the labelled PR runs every group but `long` in CI.
9. **Commits** per file or per small group, message
   `m3/phase-18b BO: <File>Test.ts → <package> (<n> cases, <m> missing)`;
   a fix on the security path is its own commit,
   `m3/phase-18b BO fix: <one sentence> (<matter.js file>:<lines>)`.

## Report shape (`notes/briefs/reports/BO.md`)

The files with their Go test file and the case count ported, missing and
excluded; **Fixes** (production file, matter.js lines, one sentence, the
commit); **Node wirings** done; **Audit** (the two tables); **fabrictest
additions**; **Register candidates**; **Guards** with HEAD and one exit
line each, the families run and their result; **Differences** to this
brief; **Decisions** the owner should check; **Left out**; **Questions**;
`by_design` candidates (one paragraph each with citations — the owner
writes the entry).

## Out of scope

Groupcast (brief BP, wave 18c), Binding (BQ, 18d);
`AccessControlCustomExtensionTest.ts` (owner decision 18.2, milestone 4,
only its `until`); tags on `endpoint.Spec` (milestone 4); every
`packages/node/test/behaviors` directory of phase 19; the families table
rows and `docs/certifiability.md` beyond what the harness regenerates for
a fix you made (the owner, from the CI results).
