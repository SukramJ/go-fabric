# BX4 report — the client tree (2026-10-21)

Brief `notes/briefs/2026-10-21-BX4-client-tree.md`; branch `m4/bx4` off `main` at `b5b1653e` (#204 merged). Gate 1,
G1-12; ADR 0026 principles 1, 3, 12. Five packages moved, not four (see Differences). No behaviour changed; no
chip-tool run (no composition or FeatureMap change).

## The move

`git mv` per package; every import and every module-relative path in a Go comment rewritten by script
(`BX4-scripts/rewrite_imports.py`: 374 import sites in 215 files; `rewrite_refs.py`: 39 comment sites), `gofumpt -w`
for the import order (136 files; the diff holds only changed import lines, checked).

| Old | New | Files (tests) | Lines |
| --- | --- | --- | --- |
| `controller/discovery` | `client/discovery` | 23 (12) | 6560 |
| `controller/exchange` | `client/exchange` | 27 (19) | 6366 |
| `controller/handshake` | `client/handshake` | 14 (8) | 2420 |
| `controller/interaction` | `client/interaction` | 34 (24) | 8367 |
| `controller/peer` | `client/peer` | 28 (18) | 5830 |

No package has `testdata`. Changes inside a moved file beyond imports — module paths in comments only, `controller/x`
→ `client/x`: `client/exchange/doc.go:9`, `client/exchange/messagechannel_parity_matterjs_test.go:9`,
`client/handshake/doc.go:9`, `client/peer/doc.go:11-13`, `client/peer/peer_connection_error_handler_parity_matterjs_test.go:100`,
`client/peer/peer_connection_kick_parity_matterjs_test.go:11`. The CASE initiator and sigma code of `client/handshake`
is byte-identical apart from imports. Package docs that say "the controller's …" (`client/discovery/doc.go:4`) are left
as written. `// Mirrors` paths are matter.js paths and none changed (`fabrictest/node.go:45`'s
`behavior/system/controller/discovery` is matter.js and is skipped by the script's lookbehind).

`script/archcheck/layers.json`: `client/...` joins model-and-protocol (the client half of the protocol, beside `im`;
its imports are `clock`, `tlv`, `mdns`, `transport/*`, `secure/*`, `im`, `cluster/spec` and each other); the four
`binding → controller/*` exceptions are removed; no new exception. `binding → client/*` and `node → client/*` are
downward edges; `node` and `binding` import no `controller/*` package any more (`go list`).

## Forwards

`script/bx4-forward` (generator plus test, in the tree; removable with the window) writes
`controller/<name>/forward.go` from the type-checked new package: SPDX header, `// Code generated … DO NOT EDIT.`,
the package doc with `Deprecated: moved to github.com/SukramJ/go-fabric/client/<name> (brief BX4, v0.10.0);
removable in v0.12.0.`, then a type alias per type, `const` per constant, `var` per variable and per function (no
moved identifier has type parameters; the generator fails on one instead of writing a wrapper). The value blocks carry
`fabric:reachable:reason=…` (tests reach the identifiers through the new path; aliases are whitelisted by
reachability itself).

| Package | Types | Consts | Vars | Funcs | Total |
| --- | --- | --- | --- | --- | --- |
| discovery | 19 | 15 | 2 | 17 | 53 |
| exchange | 22 | 12 | 8 | 10 | 52 |
| handshake | 11 | 3 | 2 | 3 | 19 |
| interaction | 22 | 8 | 8 | 9 | 47 |
| peer | 16 | 2 | 2 | 3 | 23 |

Test (`script/bx4-forward/main_test.go`): `TestForwardFilesAreCurrent` regenerates in memory and fails on a stale
file; `TestEveryExportHasItsForward` loads old and new through `go/packages` and holds each old package to exactly the
new one's exported identifiers — same count, a type is an alias, a constant has the same value, a variable or a
function is a variable, `types.Identical` types.

## The client view (§3)

`controller/client → device`: `Endpoint.Device() *device.Type` (`controller/client/endpoint.go:78-81`,
`fabricdevice.Lookup`) returns the generated device type with its requirements; `schema` has the id and revision, not
the requirement set, and the return type is public API. `controller/client → endpoint`: `Endpoint.Conformance`
(`conformance.go:93-120`) runs `endpoint.DeviceTypeTree.Check` over an `imageSubject` (`DeviceTypeViolation`,
`ClusterShape`, `DeviceTypeTree`) — the one device-type validator the server side runs, on purpose. Both are real
dependencies on the device layer, not a client view of ids; both are downward edges (roles → endpoints). Left for
gate 2: moving the validator below `endpoint` (or into a role-neutral package) is the change that would remove them.

## Paths

- Docs: `docs/architecture.md` (the controller-side listing, one paragraph on the client tree), `docs/matterjs-comparison.md`,
  `docs/getting-started/controller.md`, `docs/guides/concurrency.md`, `docs/guides/testing-in-process.md`. ADRs and
  `docs/history/` untouched.
- `README.md` (§API stability: `client/…` in the public list), `CLAUDE.md` (one paragraph in Architecture).
- `notes/parity/by_design.md` (11), `notes/parity/matter_behaviour_findings.md` (9): live registers, paths only, line
  numbers unchanged because the files are. Briefs, reports, `notes/handover.md`, `notes/parity/pin-lifts.md` and
  `notes/reference/` keep their paths (history).
- `script/testparity/register.json` (147 `go` refs) and `notes/parity/matterjs_tests.md` (`make testparity`).
- `script/reachability/inventory.json` + `summary.md` (`make reachability`): unreached 5 → 5, whitelisted 4230 → 4424
  (the forwards), ratchet unchanged.
- `script/coverfloor/floors.go`: the five floors moved with their packages (95, 93, 86, 91, 93); the forwards 0 with a
  reason (no statements), `script/bx4-forward` 74 (measured 74.1).
- `script/citecheck/drift.json`: **not regenerated** — see Questions.
- CHANGELOG `[Unreleased]`: Changed (one line per package with the new import path), Deprecated (the five old paths,
  removable in v0.12.0).

## Guards

HEAD before and after each run, from the summary logs.

| Guard | HEAD | Exit |
| --- | --- | --- |
| `make ci` (first run: lint of the generator, fixed in `cd89fedd`) | 5d1d3c55 | 2 |
| `make ci` | cd89fedd | 0 |
| `go test -race -count=1 ./client/... ./binding/... ./node/... ./controller/...` | 5d1d3c55 | 0 |
| `make reachability-check` | 5d1d3c55 | 0 |
| `make testparity-check` | 5d1d3c55 | 0 |
| `make cover-check` (in `make ci`) | cd89fedd | 0 |
| `mkdocs build --strict` | 5d1d3c55 | 0 |
| `make cite-report` / `make cite-check` | — | not run (refused, see Questions) |

`make race` (whole tree) not run; the race subset is the brief's list. `make ci` includes `archcheck` and
`bluez-check` (`ble/bluez`'s test now imports `client/discovery`; its `replace ../..` resolves it).

## Differences

1. **Five packages, not four.** `controller/discovery` moved too: `client/peer` imports it (`Names`, `Address`,
   `TXTData`, `Service`, `Status` throughout `connection.go`, `peer.go`, `set.go`, `descriptor.go`). Left in the roles
   layer it is a new upward edge from model-and-protocol, which the brief forbids; injecting it would edit the moved
   peer code, which the brief also forbids. Discovery is client machinery in the same sense (the peer set resolves
   operational addresses through it; `node` imports it); it has its forward like the other four.
2. Module-path comments inside six moved files changed (listed under The move).
3. `notes/parity/by_design.md` and `matter_behaviour_findings.md` updated (live registers; the brief names `docs/` only).

## Decisions

- `client/...` in model-and-protocol, not servers: nothing in it imports a server, and it is the client half of `im`.
- Functions forward as `var` (the brief's first option); a forwarded `var` copies the value at init — the one
  non-sentinel, `interaction.DefaultSustainRetries`, is a value nobody is meant to assign.
- The generator stays in `script/bx4-forward` with its test so a later exported identifier cannot slip past the
  forwards; it goes with them in v0.12.0.
- Edit scripts kept in `notes/briefs/reports/BX4-scripts/`.

## Left out

- ADR 0026 principle 1's layer list (`im/*, groups, cluster/spec/*, …`) and its sentence on the `binding → controller/*`
  exception are not edited (owner's ADR); they now describe the state before BX4.
- §3's two device-layer dependencies (gate 2).
- Removing the forwards (BV/gate 2 `apidiff`, v0.12.0).

## Questions

1. `make cite-report` was refused in this agent's sandbox (it runs git plumbing on `MATTERJS_DIR`, outside the
   worktree). `script/citecheck/drift.json` still names the old paths (37 entries), so the CI `cite-check` job should
   fail until someone runs `MATTERJS_DIR=… make cite-report` on this branch and commits the result.
2. ADR 0026 principle 1: add `client/*` to model and protocol and drop the "one upward edge of today" sentence?
3. `site/` (a mkdocs build output, committed in `9cdf4263`) is tracked and not ignored; its search index still names
   the old paths. Remove it and ignore it?
