# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`go-fabric` is the **device side** of Matter as an embeddable Go library: TLV
codec, Interaction Model, PASE/CASE, MRP over IPv6 UDP, DNS-SD, the cluster
servers a bridge needs, and the endpoint assembler. A host application keeps
its own device model and reaches the wire through `contract/`. It is a
semantic port of matter.js, and that is the single most important thing to
know before changing anything here.

## Commands

`make help` lists everything. The ones that matter:

```sh
make build          # go build ./...
make test           # go test -count=1 ./...
make race           # go test -race (needs CGO_ENABLED=1) — not optional, see below
make lint           # gofumpt check + golangci-lint + go mod tidy -diff + go mod verify
make fmt            # gofumpt -w .
make ci             # build vet test lint cover-check — everything the pipeline runs
make setup          # install the pinned gofumpt / golangci-lint, plus govulncheck
```

A single test or package:

```sh
go test ./im/... -run TestSubscribeChunking -v
go test -race -count=1 ./secure/...
```

`make race` is a release gate, not a nicety: the session, subscription and MRP
paths are concurrent by construction, and a race there surfaces as an
intermittent pairing failure in a controller nobody can attach a debugger to.

Guards that fail the build for reasons that are not obvious from the error:

```sh
make cover-check      # per-package statement floors in script/coverfloor/floors.go
make reachability     # regenerates script/reachability/inventory.json …
make reachability-check   # … and fails if the committed snapshot moved off its ratchet
make bench-smoke      # every benchmark still builds, finds its fixture and completes
make fuzz             # every fuzz target, iteration-budgeted (FUZZTIME=100000x)
```

`cover-check` and `reachability-check` are ratchets: when a change moves the
number legitimately, regenerate, read the diff, and commit the new snapshot —
adjusting the ratchet constant in the *same* commit.

The chip-tool guard needs Linux and a ~2.5 GiB image pull, so it normally runs
in CI (`.github/workflows/chiptool.yml`, three jobs including a control leg
that separates our defects from environment failures):

```sh
make chiptool-extract   # pull chip-tool out of the pinned chip-cert-bins image
make chiptool-test      # build examples/reference-bridge, commission it for real
```

## Architecture

Two rules explain most of the layout.

**Rich model, dumb bridge.** This module owns the Matter wire format and
nothing about devices. Which data point becomes which cluster attribute, which
product maps to which device type — that lives host-side, behind `contract/`.
`contract/` therefore imports **stdlib only**: a port contract that names a
host type drags that host's release cadence and dependency graph into this
module, and the bridge is the one seam where that coupling is expensive.

**`bridge/` is the composition root.** It depends on every other package
(`endpoint`, `transport/udp`, `mdns`, `im`) and on no host type. The host
supplies its topology through a `Snapshotter` callback, so the bridge never
imports a device model. Lifecycle is `New → Start → (Reassemble) → Stop`;
every collaborator beyond the three constructor arguments arrives through an
`Attach…`/`Set…` call afterwards, because the host's wiring has real
chicken-and-egg cycles (the session manager needs the listener, the bridge
needs the manager). The cost is that `New` installs a **noop** for each
unattached port: a half-wired bridge still binds its socket, publishes mDNS
and answers datagrams — it just answers them wrongly. `bridge/bridge.go`'s
package doc lists every port, in call order, with what a skip costs and
whether it fails closed (visible) or silently (not). Read it before wiring
anything.

The request path, top to bottom:

```
transport/udp → transport/message (framing) → transport/mrp (reliability)
  → secure/channel (session keys, privacy)
  → bridge/receive.go → bridge/im_gate.go (the §8.5.7-style gates)
  → im/ (Read | Write | Invoke | Subscribe | Timed)
  → endpoint/dispatcher.go (path → endpoint → cluster, ACL + privilege checks)
  → cluster/… (a contract.ClusterServer)
```

Where the data model comes from:

- `parity/schema.json` — the embedded matter.js HEAD extract. The single copy;
  `package parity` embeds it and the generator reads the same file.
- `schema/clusters.go`, `schema/devicetypes.go` — **generated** from it. Never
  hand-edit. Use `schema.DeviceTypeRevision(id)` at runtime rather than a
  hand-written switch, so the next regeneration propagates without a second
  edit.
- `cluster/` — servers, hand-written, grouped by the device surface they serve
  (`onoff/`, `cover/`, `measurement/`, …) rather than one package per cluster.
  `cluster/core/` is the system set the root or every bridged endpoint
  mandates. `cluster/wire/` holds wire types and command payloads.

Everything outside `internal/` is public API, including `bridge/bridgetest`
and `endpoint/endpointtest` — see `README.md` §API stability for the
deprecation window before you rename anything. `internal/` holds only the
one-way test seams (`bridgeseam`, `channelseam`) and the chip-tool suite.

Scope boundaries that are decisions, not backlog — do not "fix" them:
no controller/commissioner role, no Bluetooth, no Thread, no CSA
certification. `docs/matterjs-comparison.md` records each with its reasoning.

## matter.js is the gold standard

> **Hard rule for every package in this module — `cluster/`, `bridge/`,
> `endpoint/`, `im/`, `tlv/`, `secure/`, `transport/`, `mdns/`,
> `commissioning/`, `store/` and the rest:** the gold standard is
> [`matter.js`](https://github.com/matter-js/matter.js) HEAD.
> Apache-2.0 — MIT-compatible — and the most production-tested
> open reference stack. It is **not** certified: its own README states
> that matter.js is not certified by the Connectivity Standards
> Alliance, and devices built on it show up in the ecosystems as
> uncertified test devices. The platform-specific exceptions Apple
> Home / Google Home / Alexa apply have already been encoded into
> matter.js's behavior + protocol layers through real interop
> testing; we do not re-derive them, we mirror them.

| Repo | Local path | Role |
| --- | --- | --- |
| matter.js | `../matter.js/` | Matter Core implementation: schema (`packages/model`), wire codec (`packages/types`), behavior layer (`packages/node/src/behaviors`), device types (`packages/node/src/devices`), protocol engine (`packages/protocol`). The single Matter-side gold standard. |
| connectedhomeip ("chip") | `../connectedhomeip/` | The CSA reference implementation, checked out locally. Not a second gold standard — matter.js remains the one. It is the **authority on tool and controller behaviour that matter.js does not model**: chip-tool's own argument handling (`examples/chip-tool/`), the access-control and read-client semantics already cited across `bridge/` and `im/` (`src/access/AccessControl.cpp`, `src/app/ReadClient.cpp`), and the source of every error string a chip-tool run prints. |

**When chip-tool behaves unexpectedly, read its source, not its output.**
The suite under `internal/chiptool/` and the CI control leg both drive the
chip-tool binary from `connectedhomeip/chip-cert-bins`, whose sources are
the local checkout above. Its argument parsing, its address handling and
its storage initialisation each rejected a plausible-looking invocation
during the control leg's bring-up, and every one of those rejections is a
readable line in `examples/chip-tool/` — the messages
(`Unknown cluster or command set`, `Unsupported address`,
`ExamplePersistentStorage.cpp` init failures) are grep-able strings there.
Reading the tree beats inferring the contract from a CI log, and beats
assuming a convention.

[`home-assistant-matter-bridge`](https://github.com/Nabu-Casa/home-assistant-matter-bridge)
(Apache-2.0, local at `../home-assistant-matter-bridge/`) is one
specific consumer of matter.js. Useful as an occasional helper
reference for "how does a real bridge wire its Aggregator + bridged
devices end-to-end?", but **not** a gold standard — it carries
Home-Assistant-specific shims (Entity-Domain → Cluster mapping, HA
Device Registry as data source) that do not translate here. When in
doubt, pull the pattern from matter.js itself, not from ha-bridge.

**Goal:** this module is a 100 % port of matter.js — **semantically**, not
syntactically. TypeScript idioms (decorators, `Behavior.with(...)` mixins,
`Promise<T>`) translate to Go idioms (struct-with-methods,
`context.Context`, goroutines). The same defaults, the same constraints, the
same wire shape, the same order of attributes / commands / events. Where the
Go translation forces a different surface, the Go code calls out the matter.js
function it mirrors in a comment + the contract it enforces.

### Workflow

1. **Before writing any fix or feature, read the corresponding matter.js
   source.** Likely paths:
   - schema constant / cluster revision / attribute id →
     `../matter.js/packages/model/src/standard/elements/<name>.element.ts`
   - cluster behavior (defaults, mandatory attributes, conformance
     checks) → `../matter.js/packages/node/src/behaviors/<name>/`
   - device type (DeviceTypeList revision, mandatory cluster set) →
     `../matter.js/packages/node/src/devices/<name>.ts`
   - bridge composition pattern → `../matter.js/packages/node/src/devices/aggregator.ts`
     and `../matter.js/packages/node/src/devices/bridged-device.ts`
     (ha-bridge's `packages/backend/src/matter/` is a useful
     supplementary read but is not the gold standard)
   - wire codec, IM messages, sigma → `../matter.js/packages/types/src/tlv/`
     and `../matter.js/packages/protocol/src/`
2. **Cite the matter.js path + function in the Go code**
   (`// Mirrors matter.js packages/node/src/behaviors/.../FooBehavior.ts:bar`)
   so the provenance survives drift. PR descriptions quote it too.
3. **Every change updates the parity tests** — the `*parity_matterjs_test.go`
   files across `cluster/`, `tlv/`, `im/`, `mdns/` and `schema/`. The schema
   snapshot at `parity/schema.json` is the matter.js HEAD pin; the wire-byte
   fixtures under `tlv/testdata/` and `im/testdata/` lock the codec's wire
   shape (regenerate with the scripts in `notes/parity/matter/`). New
   cluster-server tests add a parity case; PRs without parity coverage are
   rejected.
4. **Deliberate divergences are documented in `notes/parity/by_design.md`** —
   and the same divergence on a non-trivial scale gets an ADR under
   `docs/adr/`. Examples of valid divergences: a TypeScript-only optimisation
   that would fight Go's GC, a Decorator pattern that has no Go equivalent.
   Examples of invalid divergences: hand-coding cluster revisions, attribute
   IDs, constraint defaults, Apple-Home-required tag patterns — those go
   verbatim from matter.js. A gap that is *not* deliberate belongs in
   `notes/parity/matter_behaviour_findings.md` instead; closing one removes it
   from that list.
5. **Behavioral-parity contract + standing guards.** Ongoing parity is held by
   the build- and test-time guards catalogued in
   [`docs/matter-parity-contract.md`](./docs/matter-parity-contract.md)
   — schema parity tests, the behavioural negative-write parity table,
   wire-codec fixtures, and the `by_design.md` divergence catalogue — not by
   periodically regenerated audit reports. Every change reads matter.js / chip
   first, mirrors behaviour (not just schema), cites the source, and extends
   the relevant guard.

### Where the host's obligation begins

`contract/` is the boundary. Everything on this side of it mirrors matter.js
and is covered by the guards above. Everything on the far side — the device
projection — is the host's decision and the host's own gold standard.
[OpenCCU-Loom](https://github.com/SukramJ/openccu-loom), the reference
consumer, mirrors `aiohomematic` there; the two reference layers do not
overlap, and CCU wire knowledge never enters this module.

---

## Regenerate the Matter schema from matter.js HEAD

The whole pipeline lives here: the extractor
(`script/extract-from-matter-js.ts`), the snapshot it produces
(`parity/schema.json`) and the generator (`script/generate_matter_schema.go`).

```sh
cd ../matter.js/packages/model && npm run build   # once, per matter.js checkout
cd -
make generate-matter-schema                       # extract + generate + gofumpt
```

`make generate-matter-schema` does both halves: it copies the extractor into
the matter.js tree (so its bare `@matter/model` import resolves), runs it,
removes the copy even on failure, then regenerates `schema/clusters.go`,
`schema/devicetypes.go` and the `SchemaSnapshotSHA256` provenance constant.
Override the checkout with `MATTERJS_DIR=…` if it is not at `../matter.js`.

Refreshing the snapshot is a **review decision** — it changes what upstream
says. Propagating it into Go is mechanical. Run the two consciously, not as
one habit.

Then `go test ./schema/...`. `TestMatterSchemaSnapshotHashMatchesEmbedded`
fails when the generation half was skipped, and
`TestParityCodeMatchesGeneratedSchema` flags every cluster whose hand-coded
revision constant has drifted from the new schema. Update those constants to
match.

A host application that pins its own copy of these bytes (the reference daemon
does, to keep a schema change from arriving unnoticed in a dependency bump)
updates that pin after the snapshot lands here, not before.

## Conventions worth knowing before the linter tells you

- Every new `.go` file opens with the MIT header, or
  `TestLicenseHeaderOnEveryGoFile` fails naming it:

  ```go
  // SPDX-License-Identifier: MIT
  // Copyright (C) 2026 SukramJ.
  ```
- **No cgo.** The module builds with `CGO_ENABLED=0`; only the race-enabled
  test run turns it on.
- Five direct dependencies. A sixth is a decision, not a convenience: MIT /
  Apache-2.0 / BSD only, and it updates `THIRD-PARTY-NOTICES.md` (plus
  `licenses/` when the licence text must travel).
- User-visible changes get a `CHANGELOG.md` entry under `[Unreleased]`. A
  break that appears only in a commit message has not been announced — see
  `README.md` §"How a consumer learns about a break".
