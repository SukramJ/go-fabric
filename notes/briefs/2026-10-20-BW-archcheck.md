# Brief BW — the archcheck guard (ADR 0026, principle 11)

Date 2026-10-20. go-fabric `main` after ADR 0026
(`docs/adr/0026-architecture-principles.md`, principle 1 for the layers,
principle 11 for the guard). A small wave: one Go test package and its
rule file, a Makefile target, a docs line. Model: Sonnet (the work is
checked by the test it builds; no fact about matter.js or chip is
written).

## What to build

`script/archcheck/`:

- `layers.json` — the rule file, data not code:
  ```json
  {
    "layers": [
      {"name": "foundation", "packages": ["clock", "clock/clocktest", "tlv", "schema", "parity", "internal/clockvar"]},
      {"name": "transport", "packages": ["transport/...", "mdns", "internal/udpseam", "internal/channelseam"]},
      {"name": "security-and-storage", "packages": ["secure/...", "commissioning", "store", "bdx/...", "diagevent", "bootid"]},
      {"name": "model-and-protocol", "packages": ["im", "im/...", "groups", "cluster/spec", "cluster/spec/...", "cluster/wire", "contract", "eligibility"]},
      {"name": "servers", "packages": ["cluster", "cluster/...", "binding"]},
      {"name": "endpoints", "packages": ["endpoint", "endpoint/...", "device", "device/..."]},
      {"name": "roles", "packages": ["bridge", "controller/...", "internal/bridgeseam", "internal/nodeseam"]},
      {"name": "facade", "packages": ["node"]},
      {"name": "support", "packages": ["fabrictest", "bridge/bridgetest", "endpoint/endpointtest", "controller/ble/bletest", "internal/paritytest", "internal/chiptool", "internal/chipdm", "examples/...", "script/...", "ble/..."]}
    ],
    "exceptions": [
      {"from": "binding", "to": "controller/exchange", "reason": "the node's client stack lives under controller/ since phase 18d; ADR 0026 principle 12, resolved at gate 1"},
      {"from": "binding", "to": "controller/handshake", "reason": "as above"},
      {"from": "binding", "to": "controller/interaction", "reason": "as above"},
      {"from": "binding", "to": "controller/peer", "reason": "as above"}
    ]
  }
  ```
  A pattern `x/...` matches `x` and everything below it; a package is in
  the first layer whose pattern matches it, **most specific pattern
  wins** regardless of order (so `cluster/spec/...` in model-and-protocol
  beats `cluster/...` in servers, `bridge/bridgetest` in support beats
  `bridge` in roles, `clock/clocktest` in foundation; implement "most
  specific" as the longest matching pattern prefix). Every package of the
  module must match a layer, or the test fails naming it.
- `archcheck_test.go` — `TestLayering`: loads the module's packages with
  `golang.org/x/tools/go/packages` (already a dependency; `script/reachability/main.go:100-102`
  shows the loading pattern) in `packages.NeedName | NeedImports | NeedFiles`
  mode, non-test imports only (the `Imports` of each package; do not
  load test variants), reduces each import to the module-relative path,
  assigns both ends to layers and fails on every edge whose target layer
  is above the source layer and that is not in `exceptions`. It also
  fails on an exception that no longer occurs ("stale exception: remove
  it") and on a package no layer matches. Output: one line per
  violation, `from -> to (layer A -> layer B)`, sorted. The test must run
  under `go test ./...` from the module root (CI's test job) and take
  well under ten seconds.
- `doc.go` — package doc: what the guard holds (ADR 0026, principle 1 and
  11), how to read a failure, how to add an exception (only with a
  reason and only in the commit that explains it; the list may only
  shrink otherwise), how to move a package between layers (edit
  `layers.json`, explain in the commit).
- Makefile: `archcheck: ## layers and dependency direction (ADR 0026)` →
  `$(GO) test ./script/archcheck/...`; add `archcheck` to the `ci`
  target's chain after `vet`. `make help` lists it (the `##` comment).
- `docs/architecture.md`: one sentence after the package map: "The
  layers and their allowed direction are held by `script/archcheck`
  ([ADR 0026](adr/0026-architecture-principles.md))." — a link out of
  `docs/` is an absolute GitHub URL; this one is inside `docs/`.
- `CHANGELOG.md` `[Unreleased]`, `### Added`: one line.

## Expectations and what to report

Run the test before adding any exception: the brief's exception list
is my reading of today's graph (`go list` reduced to directories: the
only upward edge is `binding` → the four `controller/*` packages). If the
real graph shows more upward edges, **do not add exceptions for them**:
list them in the hand-back with `from -> to` and the layers, and leave
the test failing on them in your last commit only if I have not answered
within the wave — otherwise push with the test failing and say so in the
PR body. The layer assignment of a package you were unsure about
(anything not named in the JSON above, e.g. `cluster/transition`,
`groups/...` subpackages, `controller/ble/...`, `ble/...` as a separate
module, `script/...` with `//go:build ignore` files) is reported, with
the layer you chose and why.

Guards: `make ci` (now including archcheck), `make race` is not needed
(no runtime code), `make reachability-check` (a new test package seeds
no new exported API; if the inventory moves, regenerate and commit),
`make lint`. Branch `m4/archcheck`, commits `m4 BW: <step>`, one pull
request against main, no auto-merge, no label. Copy this brief to
`notes/briefs/2026-10-20-BW-archcheck.md` in the first commit; write
`notes/briefs/reports/BW.md` (short: the real graph's upward edges, the
layer choices, the guard exit lines with HEAD, questions). Never add
attribution lines to commits or the PR body.
