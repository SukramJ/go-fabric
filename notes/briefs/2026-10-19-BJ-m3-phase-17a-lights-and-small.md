# Brief BJ — milestone 3, phase 17a: OnOff, LevelControl, ColorControl, Identify, WindowCovering, ServiceArea

Owner: Fable. Implementer: Opus, one agent on one branch; a later agent
resumes from the report. Concept: `docs/concept-v5-behaviour-depth.md`
(owner decisions 0.1–0.4, §A.2 what a port is, §A.4, Teil B). Base:
`origin/main` at or after `v0.8.0`. Branch `m3/phase-17a-lights`, one PR
against `main` at the end carrying the label `needs-chiptool`, report
`notes/briefs/reports/BJ.md`. `CHANGELOG.md` under `[Unreleased]` is yours
for fixes; `notes/parity/` (except the generated `matterjs_tests.md`),
`docs/adr/`, `CLAUDE.md` are the owner's. Brief BK runs in parallel on
`m3/phase-17b-switch` and owns `cluster/switch`, `cluster/wire/genericswitch.go`,
`contract/`, `cluster/measurement/materializers.go`, `endpoint/materialize.go`,
`device/`, `examples/reference-bridge/fleet_sensors.go`,
`internal/chiptool/` and `docs/certifiability.md`: do not touch those; BK
alone runs the chip-tool harness in this pair — you rely on the labelled
PR's CI jobs and say so.

Read `CLAUDE.md`, `notes/briefs/README.md`, the concept, the register page
`notes/parity/matterjs_tests.md` (the `node/test/behaviors` tables of the
six directories), the Sonnet map `notes/briefs/reads/read-m3-phase17.md`
§1.4–1.9, §2.4–2.9 and §3.4–3.9 (a map, never a citation),
`docs/guides/testing-in-process.md`, `fabrictest/doc.go`,
`node/attribute_read_response_parity_matterjs_test.go` (a BI port as the
pattern on `fabrictest`) and one existing parity file of each package you
touch (`cluster/onoff/onoff_parity_matterjs_test.go`,
`cluster/levelcontrol/native_parity_matterjs_test.go`,
`cluster/light/colorcontrol_parity_matterjs_test.go`,
`cluster/cover/windowcovering_parity_matterjs_test.go`,
`cluster/servicearea/servicearea_parity_matterjs_test.go`,
`cluster/core/identify_commands_test.go`). Every matter.js file is read at
the pin via `git -C ../matter.js show 862ad75f:<path>` — never the working
tree (your worktree has no `../matter.js`: use
`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js` for `make` and the
absolute path for `git -C`). The matter.js helpers are
`packages/node/test/endpoint/mock-endpoint.ts` (`MockEndpoint`, local
server calls) and `packages/node/src/testing/mock-server-node.ts`,
`node-helpers.ts` (`MockServerNode`, `interaction`); `fabrictest` is their
Go counterpart; what it lacks for a test is added to it with a test of
its own and named in the report.

## Scope

The eleven files under `packages/node/test/behaviors/` in `on-off`,
`level-control`, `color-control`, `identify`, `window-covering`,
`service-area` — 76 `it` cases at the pin:

| File | Cases | Register today | Go subject |
| --- | ---: | --- | --- |
| `on-off/OnOffServerTest.ts` | 13 | planned | `cluster/onoff` (`server.go`) |
| `level-control/LevelControlServerTest.ts` | 6 (+1 `it.only` to port as a case) | partial, 2 missing | `cluster/levelcontrol`, `cluster/transition` |
| `level-control/OnOffCouplingTest.ts` | 6 | partial, 2 missing | `cluster/levelcontrol` |
| `level-control/ColorTemperatureCouplingTest.ts` | 13 | partial, 8 missing | `cluster/levelcontrol`, `cluster/light` |
| `level-control/RemainingTimeTest.ts` | 7 | partial, 5 missing | `cluster/transition`, `cluster/levelcontrol` |
| `color-control/ColorControlServerTest.ts` | 9 | partial, 3 missing | `cluster/light` |
| `color-control/ColorConversionUtilsTest.ts` | 3 | ported | `cluster/light/colorconversion.go` |
| `color-control/RemainingTimeTest.ts` | 1 | ported | `cluster/light` |
| `identify/IdentifyServerTest.ts` | 8 | planned | `cluster/core/identify.go` |
| `window-covering/WindowCoveringServerTest.ts` | 5 | planned | `cluster/cover` |
| `service-area/ServiceAreaServerTest.ts` | 5 | planned | `cluster/servicearea` |

The two `ported` files are re-checked against the register's `go` list
(every named test exists and covers its `it`); nothing else.

## How a port is built (concept v5 §A.2 and v4 §A.5, binding)

1. **One Go file per matter.js file**, `<subject>_parity_matterjs_test.go`,
   opening with the licence header and
   `// A port of matter.js packages/node/test/behaviors/<dir>/<File>Test.ts at 862ad75f: …`.
   One Go test per `describe` group with subtests carrying the `it` names,
   or one test per `it`; the register's `go` list names every test
   function.
2. **Harness.** A matter.js test on `MockEndpoint` (`WindowCoveringServerTest`)
   calls the server locally: its port calls the Go server in its package,
   time on `clocktest.Fake`, events through a test `contract.EventEmitter`.
   A test on `MockServerNode` is a node test: its port runs on
   `fabrictest` (`NewSite`, `AddDevice` with the cluster on a bridged
   endpoint, `Device.Act` for local state, the wire helpers for
   `interaction`, `Site.Eventually` for effects), as an external test
   package of the cluster (`package onoff_test`, …) or under `node/` when
   it needs a commissioned pair. No `time.Sleep`.
3. **Vectors and expectations come from the matter.js file at the pin**,
   never from the Go code under test. An expectation our code does not
   meet is a fix (4) or a `missing` with its reason — never an adjusted
   number. Where matter.js asserts on behavior names or TypeScript error
   classes, the Go assertion is the IM status, the attribute value or the
   Go error — say which in the test.
4. **Fixes (owner decision v4 0.11).** A behaviour gap with an unambiguous
   matter.js source is fixed in this wave: the production change cites the
   file and lines (`// Mirrors matter.js packages/node/src/behaviors/…:lines`),
   the ported test proves it, `CHANGELOG.md` names it under `### Fixed`,
   the report lists it under **Fixes**. The known gaps are the `missing`
   lines of the partial entries (LevelControl: the transaction-sharing and
   per-command override cases of the couplings, timer teardown on destroy,
   the application-supplied remaining time and end time; ColorControl: the
   primaries case, the application end time on StopMoveStep, a running
   color loop) — each is a fix or stays `missing` with a sharper reason.
   Anything that changes a wire shape, a store schema, `contract/` or
   another package's API, and anything over a few dozen lines, stays
   `missing` with a reason and goes to **Register candidates**.
5. **Partial entries:** the existing `go` tests stay; add the `missing`
   cases and empty `missing`.
6. **The register after every file:** `status`, `go`, `missing`,
   `snapshotSha`; `make testparity` regenerates the page;
   `testparityPlannedRatchet` moves in the same commit.
7. **Guards at the end (and after any fix that moved device behaviour):**
   `make ci`, `make race`, `make testparity-check`, `make cite-check`,
   `make reachability-check`, `make cover-check`; background runs with a
   summary log (HEAD before and after, one exit line per guard). No
   `make chiptool-test` here (BK owns the harness): a fix that moved
   device behaviour is named in the report with the families it touches
   (OO, LVL, CC, I, WNCV, SEAR), and the labelled PR runs them in CI.
8. **Commits** per file or per small group, message
   `m3/phase-17a BJ: <File>Test.ts → <package> (<n> cases, <m> missing)`.

## Report shape (`notes/briefs/reports/BJ.md`)

The files with their Go test file and the case count ported and missing;
**Fixes** (production file, matter.js lines, one sentence); **fabrictest
additions**; **Register candidates**; **Guards** with HEAD and one exit
line each; **Differences** to this brief; **Decisions** the owner should
check; **Left out**; **Questions**; `by_design` candidates (one paragraph
each with citations — the owner writes the entry).

## Out of scope

Switch (brief BK), Thermostat and DoorLock (briefs BL and BM, the second
pair); every other `packages/node/test/behaviors` directory (phases 18 and
19); the families table and `docs/certifiability.md` (the owner, from the
CI results).
