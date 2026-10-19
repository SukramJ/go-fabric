# Brief BN — milestone 3, phase 18a: Descriptor, GeneralDiagnostics, BasicInformation, Groups, ScenesManagement, the protocol group tests

Owner: Fable. Implementer: Opus, one agent on one branch; a later agent
resumes from the report. Concept: `docs/concept-v5-behaviour-depth.md`
Teil E (owner decisions E.0 18.1–18.3, §E.2 what a port is here, §E.3,
§E.6). Base: `origin/main` at or after `v0.8.4` with #180 merged (the DD
fix: a labelled pull request can auto-merge again). Branch
`m3/phase-18a-system-small`, one PR against `main` at the end carrying the
label `needs-chiptool`, report `notes/briefs/reports/BN.md`. `CHANGELOG.md`
under `[Unreleased]` is yours for fixes; `notes/parity/` (except the
generated `matterjs_tests.md`), `docs/adr/`, `CLAUDE.md` are the owner's.
No wave runs in parallel (E.0 18.3): you own every path you touch, and you
may run the chip-tool harness yourself (it is single-user; nobody else
runs it during this wave).

Read `CLAUDE.md`, `notes/briefs/README.md`, the concept Teil E, the
register page `notes/parity/matterjs_tests.md` (the `node/test/behaviors`
tables of `descriptor`, `general-diagnostics`, `basic-information`,
`groups`, `scenes-management` and the `protocol/test/groups` table), the
Sonnet map `notes/briefs/reads/read-m3-phase-18-system-set.md` §1.1, §1.2,
§1.6, §1.9, §1.12, §3.1, §3.2, §3.6, §3.9, §3.12 and §4 (a map, never a
citation), `docs/guides/testing-in-process.md`, `fabrictest/doc.go`, the
BJ report `notes/briefs/reports/BJ.md` (the last wave on `fabrictest`) and
one existing parity file of each package you touch
(`cluster/core/descriptor_parity_test.go`,
`cluster/core/basic_information_parity_test.go`,
`cluster/core/groups_parity_matterjs_test.go`,
`cluster/core/general_diagnostics_test.go`,
`cluster/core/scenes_management_test.go`, `groups/manager_test.go` or the
file the register's `go` list names for `GroupsTest.ts`). Every matter.js
file is read at the pin via `git -C ../matter.js show 862ad75f:<path>` —
never the working tree (a worktree has no `../matter.js`: use
`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js` for `make` and the
absolute path for `git -C`). The matter.js helpers are
`packages/node/test/endpoint/mock-endpoint.ts` (`MockEndpoint`, local
server calls) and `packages/node/src/testing/mock-server-node.ts`,
`node-helpers.ts` (`MockServerNode`, `interaction`); `fabrictest` is their
Go counterpart; what it lacks for a test is added to it with a test of its
own and named in the report.

## Scope

Eleven matter.js test files, 47 cases open at the pin (register
`script/testparity/register.json`, `missing` lists and `planned` entries):

| File | Harness | Cases open | Register today | Go subject |
| --- | --- | ---: | --- | --- |
| `descriptor/DescriptorServerTest.ts` | `MockEndpoint` + `MockServerNode` | 16 | partial, 11 `go` | `cluster/core/descriptor.go`, `endpoint/` (parts, tags), `device/` |
| `descriptor/DescriptorServerClientListTest.ts` | `MockEndpoint` | 3 | partial, 2 `go` | `cluster/core/descriptor.go`, `endpoint/client_clusters_test.go` pattern |
| `general-diagnostics/GeneralDiagnosticsOperationalHoursTest.ts` | `MockServerNode` | 6 | partial, 3 `go` | `cluster/core/general_diagnostics.go` (UpTime, TotalOperationalHours on `clock.Clock`) |
| `general-diagnostics/GeneralDiagnosticsFeaturesTest.ts` | `MockServerNode` | 5 | planned | `cluster/core/general_diagnostics.go` (feature selection, DataModelTest against the node's paths-per-invoke limit) |
| `basic-information/BasicInformationValidatorsTest.ts` | `MockServerNode` | 1 | partial, 5 `go` | `cluster/core/basic_information.go`, `bridged_device_basic_information.go` |
| `basic-information/CapabilityMinimaTest.ts` | `MockServerNode` | 1 | partial, 2 `go` | `cluster/core/basic_information.go` |
| `basic-information/ConfigurationVersionTest.ts` | `MockServerNode` | 4 | partial, 2 `go` | `cluster/core/basic_information.go`, `bridge/` (the bridge's ConfigurationVersion bump) |
| `groups/GroupsServerSchemaTest.ts` | `MockServerNode` | 3 | planned | `cluster/core/groups.go` (AddGroup with out-of-range GroupId / over-long GroupName answered by the handler, not the schema) |
| `scenes-management/ScenesManagementServerTest.ts` | `MockSite` + `ServerNode` | 1 | partial, 1 `go` | `cluster/core/scenes_management.go`, `cluster/onoff` (boolean scene value) |
| `protocol/test/groups/KeySetsTest.ts` | protocol unit test | 2 | partial, 4 `go` | `groups/` (the manager's key sets: default key set's only key, IPK key set's second-newest key, §4.14.2.6) |
| `protocol/test/groups/MessagingStateTest.ts` | protocol unit test | 5 | partial, 1 `go` | `groups/` (reception window; the storage setter and the legacy `*-data` counter migration — see the rule below) |

The case titles are the `missing` lists of the register entries (verbatim
`it` titles) and, for the two `planned` files, the `it` lines of the map
§1.2 and §1.6; the expectations are read from the matter.js file at the
pin. The `ported` files of `protocol/test/groups` (`FabricGroupsManagerTest`,
`GroupsMulticastPolicyTest`, `GroupsTest`) are re-checked against the
register's `go` list (every named test exists and covers its `it`);
nothing else.

## How a port is built (concept v5 §A.2, §E.2 and v4 §A.5, binding)

1. **One Go file per matter.js file**, `<subject>_parity_matterjs_test.go`,
   opening with the licence header and
   `// A port of matter.js packages/node/test/behaviors/<dir>/<File>Test.ts at 862ad75f: …`
   (for the protocol files `packages/protocol/test/groups/<File>.ts`). One
   Go test per `describe` group with subtests carrying the `it` names, or
   one test per `it`; the register's `go` list names every test function.
   Where a partial entry's existing `go` tests live in a differently named
   file, the new cases go into a file of the pattern above and the register
   lists both.
2. **Harness.** `MockEndpoint` cases call the Go server in its package on
   `clocktest.Fake`. `MockServerNode` cases run on `fabrictest` (`NewSite`,
   `AddDevice`, `Device.Act`, the wire helpers for `interaction`,
   `Site.Eventually`), as an external test package of the cluster or under
   `node/` when a commissioned pair is needed. The Descriptor hierarchy
   cases ("when child is added before parent (auto ID)", the manual-ID
   orders, "orders PartsList numerically") build the endpoint tree through
   the assembler (`endpoint/`), as the BJ report did for parts, and read
   PartsList through the dispatcher; TagList edits write through the
   server's write path. No `time.Sleep`.
3. **Vectors and expectations come from the matter.js file at the pin**,
   never from the Go code under test. An expectation our code does not
   meet is a fix (4) or a `missing` with its reason — never an adjusted
   number. Where matter.js asserts on TypeScript error classes or
   `behaviors.require`, the Go assertion is the IM status, the attribute
   value or the Go error — say which in the test.
4. **Fixes (owner decision v4 0.11).** A behaviour gap with an unambiguous
   matter.js source is fixed in this wave: the production change cites the
   file and lines (`// Mirrors matter.js packages/node/src/behaviors/…:lines`),
   the ported test proves it, `CHANGELOG.md` names it under `### Fixed`,
   the report lists it under **Fixes**. Likely candidates from the
   `missing` lists: the Descriptor tag-edit rules (a listed tag added once,
   label replaced or kept), the ConfigurationVersion rules (callback before
   the increment, a decrease refused, one bump for a grouped change), the
   GeneralDiagnostics high-water reset across a restart, the integer
   counter and the non-negative hours on a backward step, the Groups
   schema relaxation (the handler answers, the schema does not refuse).
   Anything that changes a wire shape, a store schema, `contract/` or
   another package's API, and anything over a few dozen lines, stays
   `missing` with a reason and goes to **Register candidates**.
5. **Runtime-only cases (concept §E.2).** `MessagingStateTest.ts`'s storage
   setter ("allows setting storage once", "rejects setting storage a
   second time") and its legacy `*-data` entry migration are matter.js
   storage-shape cases: port what has a Go counterpart on the manager, and
   mark the rest `excluded` with class `js-runtime-only` and the reason
   verbatim-close to the register's current wording. Everything a
   controller can observe is never `excluded`.
6. **Partial entries:** the existing `go` tests stay; add the `missing`
   cases and empty `missing`. The `planned` files become `ported` or
   `partial` with a `missing` list.
7. **The register after every file:** `status`, `go`, `missing`,
   `snapshotSha`; `make testparity` regenerates the page;
   `testparityPlannedRatchet` moves in the same commit.
8. **Guards at the end (and after any fix that moved device behaviour):**
   `make ci`, `make race`, `make testparity-check`, `make cite-check`,
   `make reachability-check`, `make cover-check`; background runs with a
   summary log (HEAD before and after, one exit line per guard). A fix
   that moved device behaviour the reference daemon shows (Descriptor,
   GeneralDiagnostics, BasicInformation, Groups, ScenesManagement) also
   runs the touched families locally
   (`GOFABRIC_CHIP_FAMILIES=DESC,DGGEN,BINFO,BRBINFO,G,IDM make chiptool-families`;
   IDM/10.4 whenever an endpoint's PICS moved; `-update-chip-cases` only
   when the diff is read and explained in the report) and names them in
   the report; the labelled PR runs every group but `long` in CI.
9. **Commits** per file or per small group, message
   `m3/phase-18a BN: <File>Test.ts → <package> (<n> cases, <m> missing)`.

## Report shape (`notes/briefs/reports/BN.md`)

The files with their Go test file and the case count ported, missing and
excluded; **Fixes** (production file, matter.js lines, one sentence);
**fabrictest additions**; **Register candidates**; **Guards** with HEAD and
one exit line each, the families run and their result; **Differences** to
this brief; **Decisions** the owner should check; **Left out**;
**Questions**; `by_design` candidates (one paragraph each with citations —
the owner writes the entry).

## Out of scope

AccessControl, OperationalCredentials, GroupKeyManagement and the system
commissioning tests (brief BO, wave 18b), Groupcast (BP, 18c), Binding
(BQ, 18d); `AccessControlCustomExtensionTest.ts` (owner decision 18.2,
milestone 4); every `packages/node/test/behaviors` directory of phase 19;
the families table rows and `docs/certifiability.md` beyond what the
harness regenerates for a fix you made (the owner, from the CI results).
