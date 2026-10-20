# Report BW — archcheck (ADR 0026 principles 1 and 11)

Branch `m4/archcheck`. `script/archcheck/` (layers.json, archcheck_test.go, doc.go), `make archcheck` (in `ci` after `vet`), one sentence in `docs/architecture.md`, a CHANGELOG line. The test loads in about 5 s.

## The real graph's upward edges

Measured with an empty exception list, before any exception: the four expected `binding` edges and three more.

| from | to | layers | resolution (owner decisions, 2026-10-20) |
| --- | --- | --- | --- |
| `binding` | `controller/{exchange,handshake,interaction,peer}` | servers -> roles | exceptions as in the brief (ADR 0026 principle 12) |
| `cluster/spec` | `cluster` | model-and-protocol -> servers | the root `cluster` package is shared machinery (data versions, attribute changes, globals), not a server: the exact pattern `cluster` is in model-and-protocol, so the edge is same-layer |
| `cluster/wire` | `cluster` | model-and-protocol -> servers | same |
| `cluster/wire` | `cluster/switch` | model-and-protocol -> servers | exception: deprecated `NewGenericSwitch` wrapper over `switchserver.Server`; its `Deprecated:` comment says it may be removed in v0.10.0 |

An exact pattern wins over an `x/...` pattern of the same base (documented in `doc.go`, in `layers.json`'s `reading_order` and covered by `TestSpecificity`).

## Layer choices not named in the brief's JSON

- `.` (the module root, `doc.go` only, imports nothing): support. It is documentation above everything.
- `conformance` (golden vectors, load tests, chip-tool smoke; no non-test code): support.
- `cluster/spec/spectest` (parity assertions for generated definitions): support, as an explicit exact pattern (confirmed by the owner). It imports `internal/paritytest` (support), so under `cluster/spec/...` in model-and-protocol it was an upward edge; it is test support in the same sense as `bridge/bridgetest`, so it joins them. Not an exception, a layer choice.
- `ble/...` stays in support as given; `ble/bluez` is a separate module and is not loaded by `./...`, the pattern simply matches nothing. `script/...` files with `//go:build ignore` are not loaded either.
- Everything else matched a pattern of the brief (checked by the "unassigned package" failure: only `.` and `conformance` were unmatched).

## Edits outside the brief's path list

- `script/coverfloor/floors.go`: one line, `script/archcheck` with floor 0 ("doc and test only"). `cover-check` fails on a package without a floor entry; the package has no non-test statements.
- `script/reachability/inventory.json` and `summary.md`: regenerated as the brief anticipated (+1 test package, +3 entry points; unreachable stays 5).

## Guards

See the PR body for the exit lines with HEAD.

## Guards

Run on the tree of the final commit (only this report differs from what was measured): `make archcheck` 0, `make cover-check` 0, `make test` 0, `make reachability-check` 0, `make lint` 0 (with `GOTMPDIR` set; no timeout this time). The first `cover-check` run failed on `controller/client` `TestRegistryDecommission` ("stored [peer1]") while the machine was loaded by parallel guards; it passed alone three times and in `make test`, and the second `cover-check` run was green. The exit lines with HEAD are in the PR body.
