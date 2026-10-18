# Brief BH — milestone 2, phase 16: the port of `packages/protocol/test`

Owner: Fable. Implementer: Opus, one agent at a time on one branch; a
later agent resumes from the report. Concept: `docs/concept-v4-test-foundation.md`
§A.3 (the register), §A.5 (owner decisions 0.9–0.12, the tranches, how a
port is built) and Teil B. Base: `origin/main` after the phase-16
preparation merged (register `until` on `planned`). Branch
`m2/phase-16-protocol`, one PR against `main` at the end, report
`notes/briefs/reports/BH.md` (one section per tranche, appended as you go).
`CHANGELOG.md` under `[Unreleased]` is yours for fixes; `notes/parity/`
(except the generated `matterjs_tests.md`), `docs/adr/`, `CLAUDE.md` are
the owner's. Brief BI runs in parallel on `m2/phase-16-node` (`node/`,
`controller/client`); do not touch those paths except for a fix that a
test proves, and name it in the report.

Read `CLAUDE.md`, `notes/briefs/README.md`, the concept sections, the
register page `notes/parity/matterjs_tests.md` (the `protocol/test/…`
tables: status, `go` references, `missing`), the Sonnet map
`notes/reference/read-matterjs-tests-862ad75f.md` lines 551–800 (one row
per file: lines, `describe` names, imports, candidate Go tests — a map,
never a citation), `docs/guides/testing-in-process.md` and `fabrictest/doc.go`.
Every matter.js file is read at the pin via
`git -C ../matter.js show 862ad75f:<path>` — never the working tree. The
helpers the tests import (`packages/protocol/src/protocol/ProtocolMocks.ts`,
`packages/testing/src/mocks/*.ts`, `packages/protocol/test/**/*Mock*.ts`)
are read the same way when a test needs them.

## Scope

Every `planned` and `partial` entry of `script/testparity/register.json`
under `packages/protocol/test/`, except the seven with `until` (the six
`icd/` files, `peer/PeerConnectionSocketOwnershipTest.ts`): 87 files,
about 1 500 `it` cases at the pin. `excluded` entries stay excluded; a port
that contradicts an entry's class is named in the report, the
reclassification is the owner's.

## Tranches (in this order, each ending green, pushed and reported)

| Tranche | Directories under `packages/protocol/test/` | Where the Go tests live |
| --- | --- | --- |
| BH-1 wire core | `codec`, `session`, `session/secure`, `securechannel`, `protocol`, `protocol/interaction`, `interaction`, `events`, `transport/tcp` | `transport/message`, `transport/mrp`, `transport/tcp`, `secure/channel`, `secure/operational`, `secure/pase`, `secure/case`, `bridge/`, `im/` |
| BH-2 credentials and transfer | `certificate`, `fabric`, `groups`, `bdx`, `ota`, `thread`, `advertisement`, `common` | `secure/mattercert`, `commissioning/`, `controller/ca`, `controller/attestation`, `groups/`, `bdx/`, `cluster/core` (OTA), `controller/thread`, `mdns/`, `controller/ble` (BleScanner, BleListeningClock), `bridge/` (fail-safe) |
| BH-3 controller side | `peer`, `mdns`, `action/client`, `action/client/subscription`, `action/request` | `controller/peer`, `controller/discovery`, `controller/interaction`, `controller/exchange`, `mdns/` |
| BH-4 DCL | `dcl` | `controller/dcl` (or where the DCL client lives — the register's `go` references say) |

A tranche that does not fit the agent's remaining budget is left whole for
the next agent: finish the current file, commit, push, report, and say in
the hand-back which tranche and which file is next.

## How a port is built (concept §A.5, binding)

1. **One Go file per matter.js file**, `<subject>_parity_matterjs_test.go`
   in the package of the subject, opening with the licence header and
   `// A port of matter.js packages/protocol/test/<dir>/<File>Test.ts at 862ad75f: …`.
   One Go test per `describe` group with subtests carrying the `it` names,
   or one test per `it` — the register's `go` list names every test
   function, so the guard resolves each.
2. **Vectors and expectations come from the matter.js file at the pin**,
   never from the Go code under test. An expectation our code does not
   meet is a fix (3) or a `missing` with its reason — never an adjusted
   number.
3. **Fixes (owner decision 0.11).** When the port shows a behaviour gap and
   the matter.js source is unambiguous, fix it in this wave: the
   production change cites the matter.js file and lines
   (`// Mirrors matter.js packages/protocol/src/…:lines`), the ported test
   proves it, `CHANGELOG.md` `[Unreleased]` names it under `### Fixed`,
   and the report lists it under **Fixes** with the lines. Anything
   unclear, anything that changes a wire shape, the store schema or
   `secure/` key material, and anything over a few dozen lines stays
   `missing` with a reason and goes to the report under **Register
   candidates**. A fix that lands in `node/`, `controller/client` or
   `fabrictest` (brief BI's paths) is named in the report.
4. **A test that needs a node and a controller** runs on `fabrictest`
   (`NewSite`, `CommissionedPair`, the wire helpers, `Site.Eventually`); a
   test of one component stays a unit test of its package. Time runs on
   `clocktest.Fake` (`clock/clocktest`), never on `time.Sleep`.
5. **Partial entries:** the existing `go` tests stay; add the `missing`
   cases beside them (same file when it is already a
   `*parity_matterjs_test.go`, else a new one) and empty `missing`. An
   `it` that cannot be ported keeps its `missing` line with a reason
   appended in the register's `reason`.
6. **The register after every file:** `status`, `go`, `missing`,
   `snapshotSha` (the blob id the snapshot records); `make testparity`
   regenerates the page; `testparityPlannedRatchet` in
   `script/testparity/register_test.go` moves in the same commit as the
   entries it counts.
7. **Guards per tranche:** `make ci`, `make race`, `make testparity-check`,
   `make cite-check` (every new citation in a `// A port of …` header or a
   `// Mirrors …` comment resolves at the pin; the drift ratchet may only
   fall), `make reachability-check`, `make cover-check` (a floor that rises
   is raised, never lowered). `make chiptool-test` only when a fix moved
   device behaviour; say when it was skipped and why. Background runs with
   a summary log; read the summary, grep a log on failure.
8. **Commits** per file or per small group, message
   `m2/phase-16 BH-n: <File>Test.ts → <package> (<n> cases, <m> missing)`.

## Report shape (`notes/briefs/reports/BH.md`)

Per tranche: the files with their Go test file and the case count ported
and missing; **Fixes** (production file, matter.js lines, one sentence);
**Register candidates** (gaps left as `missing`, with the matter.js lines
and why the fix did not fit the wave); **Guards** with HEAD and one exit
line each; **Differences** to this brief; **Decisions** the owner should
check; **Left out**; **Questions**; `by_design` candidates (one paragraph
each with citations — the owner writes the entry).

## Out of scope

The `until` entries; every `excluded` entry; `packages/node/test`
(brief BI and milestone 3); the CHIP image lift; the 261 citation drifts;
changes to `fabrictest`'s API beyond what a port strictly needs (name
them).
