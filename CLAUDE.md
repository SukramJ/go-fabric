# CLAUDE.md — Matter side of OpenCCU-Loom

This file is loaded when you touch `internal/north/matter/` (and the
related `bridge/`, `endpoint/`, `im/`, `tlv/`, `secure/` trees). The
repo-wide rules live in the root [`CLAUDE.md`](../../../CLAUDE.md).

## matter.js is the gold standard

> **Hard rule for everything under `internal/north/matter/`,
> `internal/north/matter/cluster/`, `internal/north/matter/bridge/`,
> `internal/north/matter/endpoint/`, `internal/north/matter/im/`,
> `internal/north/matter/tlv/`, `internal/north/matter/secure/` and
> any other Matter-side code:** the gold standard is
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
The suite under `tests/chiptool/` and the CI control leg both drive the
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
Device Registry as data source) that do not translate to
OpenCCU-Loom. When in doubt, pull the pattern from matter.js itself,
not from ha-bridge.

**Goal:** OpenCCU-Loom's Matter side is a 100 % port of matter.js —
**semantically**, not syntactically. TypeScript idioms
(decorators, `Behavior.with(...)` mixins, `Promise<T>`) translate to
Go idioms (struct-with-methods, `context.Context`, goroutines). The
same defaults, the same constraints, the same wire shape, the same
order of attributes / commands / events. Where the Go translation
forces a different surface, the Go code calls out the matter.js
function it mirrors in a comment + the contract it enforces.

### Workflow

1. **Before writing any Matter-side fix or feature, read the
   corresponding matter.js source.** Likely paths:
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
3. **Every Matter-side change updates the parity tests** — the
   `*parity_matterjs_test.go` files across `cluster/`, `tlv/` and
   `schema/`. The schema snapshot at `parity/schema.json` is the
   matter.js HEAD pin (regen via `script/extract-from-matter-js.ts`);
   the wire-byte fixtures the TLV tests read lock the codec's wire
   shape. New cluster-server tests add a parity case; PRs without
   parity coverage are rejected.
4. **Deliberate divergences are documented in
   `notes/parity/by_design.md` (matter.js section)** — and the same
   divergence on a non-trivial scale gets an ADR. Examples of valid
   divergences: a TypeScript-only optimisation that would fight Go's
   GC, a Decorator pattern that has no Go equivalent. Examples of
   invalid divergences: hand-coding cluster revisions, attribute IDs,
   constraint defaults, Apple-Home-required tag patterns — those go
   verbatim from matter.js.
5. **Behavioral-parity contract + standing guards.** Ongoing Matter
   parity is held by the build- and test-time guards catalogued in
   [`docs/matter-parity-contract.md`](../../../docs/matter-parity-contract.md)
   — schema parity tests, the behavioural negative-write parity table,
   wire-codec fixtures, wiring-capability pins, and the `by_design.md`
   divergence catalogue — not by periodically regenerated audit reports.
   Every Matter change reads matter.js / chip first, mirrors behaviour
   (not just schema), cites the source, and extends the relevant guard.

### Lockstep with aiohomematic

aiohomematic remains the gold standard for the **CCU side** —
transports, devices, paramsets, custom-DP composition. matter.js is
the gold standard for the **Matter side**. The two reference layers
do not overlap; CCU wire knowledge stays in aiohomematic, Matter wire
knowledge stays in matter.js. When a single bridge feature spans
both (e.g. a HmIP DataPoint surface that has to map onto a Matter
cluster) the boundary is the `internal/model/custom/<dp>/matter.go`
file — left side mirrors aiohomematic, right side mirrors matter.js.

---


## Regenerate Matter schema from matter.js HEAD

The whole pipeline lives in this module: the extractor
(`script/extract-from-matter-js.ts`), the snapshot it produces
(`parity/schema.json`, the single copy — `package parity` embeds it and the
generator reads the same file), and the generator
(`script/generate_matter_schema.go`).

There is no `make` here, and the two steps are deliberately separate: step 1
changes what upstream says, which is a review decision; step 2 only propagates
it into Go.

1. **Refresh the snapshot** from a built matter.js checkout (`cd
   ../matter.js/packages/model && npm run build` first). The extractor imports
   `@matter/model` by bare specifier, so it has to run from inside that
   checkout, and its stdout must not be piped straight onto the embed — a
   throw would truncate it:

   ```sh
   cd ../matter.js
   cp ../go-fabric/script/extract-from-matter-js.ts .occu-extract.mts
   node .occu-extract.mts > /tmp/schema.json && \
       mv /tmp/schema.json ../go-fabric/parity/schema.json
   rm .occu-extract.mts
   ```

2. **Regenerate the typed maps** — `clusters.go`, `devicetypes.go` and the
   `SchemaSnapshotSHA256` provenance constant. The generator gofmts its own
   output, so no formatting step follows it:

   ```sh
   go generate ./schema/...
   ```

Then run `go test ./schema/...`. `TestMatterSchemaSnapshotHashMatchesEmbedded`
fails when step 2 was skipped, and `TestParityCodeMatchesGeneratedSchema` flags
every cluster whose hand-coded revision constant has drifted from the new
schema. Update those constants to match.

Callers that need a device-type revision at runtime should use
`schema.DeviceTypeRevision(id)` rather than hard-coding a switch — that way the
next regeneration propagates the update without a second manual edit.

A host application that pins its own copy of these bytes (the reference daemon
does, to keep a schema change from arriving unnoticed in a dependency bump)
updates that pin after the snapshot lands here, not before.

