# Report BW — archcheck (ADR 0026 principles 1 and 11)

Branch `m4/archcheck`. `script/archcheck/` (layers.json, archcheck_test.go, doc.go), `make archcheck` (in `ci` after `vet`), one sentence in `docs/architecture.md`, a CHANGELOG line. The test loads in about 5 s.

## The real graph's upward edges

Measured with an empty exception list, before any exception. Besides the four expected `binding` edges (servers -> roles) the real graph has three more. None of them is excepted; **`TestLayering` fails on them in this PR**, as the brief asks.

| from | to | layers | what it is |
| --- | --- | --- | --- |
| `cluster/spec` | `cluster` | model-and-protocol -> servers | `spec/server.go` uses `cluster.DataVersionTracker` and `cluster.AttributeChanges` |
| `cluster/wire` | `cluster` | model-and-protocol -> servers | `scenes_management.go` uses `cluster.AttrGlobalFeatureMap` / `AttrGlobalClusterRevision` |
| `cluster/wire` | `cluster/switch` | model-and-protocol -> servers | `genericswitch.go` keeps the deprecated `NewGenericSwitch` wrapper over `switchserver.Server` |

The root package `cluster` holds the shared server machinery (data versions, attribute change sets, globals, coercion), not a server. The first two edges say that it is model-level code placed in the servers layer. Moving it (or splitting it) to model-and-protocol would remove both; the third goes with the removal of the deprecated wrapper. A decision for the owner; nothing was moved here.

The four `binding` -> `controller/{exchange,handshake,interaction,peer}` edges are in the exception list as the brief gave it, and all four occur (no stale exception).

## Layer choices not named in the brief's JSON

- `.` (the module root, `doc.go` only, imports nothing): support. It is documentation above everything.
- `conformance` (golden vectors, load tests, chip-tool smoke; no non-test code): support.
- `cluster/spec/spectest` (parity assertions for generated definitions): support. It imports `internal/paritytest` (support), so under `cluster/spec/...` in model-and-protocol it was an upward edge; it is test support in the same sense as `bridge/bridgetest`, so it joins them. Not an exception, a layer choice.
- `ble/...` stays in support as given; `ble/bluez` is a separate module and is not loaded by `./...`, the pattern simply matches nothing. `script/...` files with `//go:build ignore` are not loaded either.
- Everything else matched a pattern of the brief (checked by the "unassigned package" failure: only `.` and `conformance` were unmatched).

## Edits outside the brief's path list

- `script/coverfloor/floors.go`: one line, `script/archcheck` with floor 0 ("doc and test only"). `cover-check` fails on a package without a floor entry; the package has no non-test statements.
- `script/reachability/inventory.json` and `summary.md`: regenerated as the brief anticipated (+1 test package, +3 entry points; unreachable stays 5).

## Guards

See the PR body for the exit lines with HEAD.

## Questions

1. The three further upward edges: move `cluster` (root) down, or put it in the exception list for now? Left failing until answered.
2. Is `cluster/spec/spectest` in support what you want, or should the test helper packages of the generated definitions get their own pattern?
