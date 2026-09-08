# Matter Behavioural-Parity Contract

**Audience:** every contributor and AI agent that touches code in this module.
This is a standing contract, not an audit you run once. Read it before your
first change here and treat it as binding.

`go-fabric` is a deliberate, behaviour-level port of
[matter.js](https://github.com/matter-js/matter.js) HEAD. This page defines
what "parity" means in practice and which standing guards keep the port
honest, so that a change which merely *looks* right but drifts from the gold
standard cannot land unnoticed.

It complements the
[`matter.js is the gold standard`](../CLAUDE.md) section of this repository's
`CLAUDE.md`: that one tells you matter.js HEAD is the gold standard and how to
regenerate the schema; this one tells you what *parity* means, why
**behaviour** parity (not just schema parity) is the bar, and which standing
guards enforce it. What the ecosystems themselves do with the result is
collected in
[Matter Ecosystem Observations](./matter-ecosystem-observations.md).

---

## 1. The principle

> **Think for yourself, and always verify against matter.js / connectedhomeip.
> Mirror behaviour, not just shape.**

Two halves, both mandatory:

1. **Reason about the change.** Understand the cluster, the command, the state
   machine, the failure modes. Do not cargo-cult.
2. **Verify against the gold standard.** Before you write a fix or feature,
   read the corresponding matter.js source (and connectedhomeip for
   wire-truth). Your implementation mirrors theirs — same defaults, same
   constraints, same status codes, same order, same wire shape — unless a
   divergence is deliberate and recorded.

A change that "looks right" but was never checked against matter.js is not
acceptable, no matter how reasonable it seems. The protocol's edge cases were
encoded into matter.js through real interop testing against Apple Home, Google
Home, and Alexa; we mirror that hard-won behaviour rather than re-deriving it.

## 2. Why matter.js / chip, and why *behaviour*

matter.js HEAD is a production-tested, continuously-evolving Matter stack (it
is not CSA-certified, and neither is this module). connectedhomeip (chip) is
the CSA reference. Together they are the authority for:

- cluster IDs, revisions, attribute / command / event IDs (schema), **and**
- defaults, constraint enforcement on writes, command semantics, status codes,
  conformance gating, subscribe / report cadence, commissioning and CASE state
  machines (**behaviour**), **and**
- the byte-level wire shape (TLV, IM messages, sigma).

**Schema parity is necessary but not sufficient.** It is easy to advertise the
right attribute IDs and revisions and still get the behaviour wrong — accept a
write matter.js rejects, return the wrong status, skip a constraint, drop a
session that must survive. Those defects pass every schema test and surface as
silent Apple / Google pair-aborts or mis-behaving devices that take days to
attribute back. Behaviour parity is the bar.

This is not hypothetical: a parity sweep found a whole class of write-constraint
and command-semantics defects (wrong setpoint limits, an unenforced
percent-max, a `SupportedOperatingModes` bitmap that marked a mandatory mode
unsupported, a `MoveToColorTemperature` that never moved, an `UpdateNOC` that
tore down its own response session) — every one of which a schema test would
have waved through.

## 3. The workflow for every change

1. **Read the matter.js source first.** Likely paths:
   - schema constant / revision / id → `../matter.js/packages/model/src/standard/elements/<name>.element.ts`
   - cluster behaviour (defaults, mandatory attrs, conformance, write
     constraints, command logic) → `../matter.js/packages/node/src/behaviors/<name>/<Name>Server.ts`
   - device type → `../matter.js/packages/node/src/devices/<name>.ts`
   - wire codec / IM / sigma → `../matter.js/packages/types/src/tlv/`, `../matter.js/packages/protocol/src/`
   - wire-truth cross-check → `../connectedhomeip/src/app/...`, `../connectedhomeip/src/messaging/...`
2. **Mirror the behaviour** in Go idiom (struct-with-methods, `context.Context`,
   goroutines for TS decorators / mixins / `Promise<T>`). Keep the same
   defaults, constraints, status codes, and order.
3. **Cite the source in the Go code:**
   `// Mirrors matter.js packages/node/src/behaviors/.../FooServer.ts:bar`
   (or the chip path). Provenance must survive drift.
4. **Add a parity test — behaviour, not just schema.** A new constraint adds a
   row to the negative-write parity table (§4). A new cluster server adds a
   schema parity case. A wire change adds / updates a TLV fixture. PRs without
   parity coverage are rejected.
5. **Record deliberate divergences** in
   [`notes/parity/by_design.md`](../notes/parity/by_design.md) (the living
   catalogue). A non-trivial divergence also gets an ADR under
   [`docs/adr/`](./adr/). Valid divergences are TypeScript-only optimisations
   that fight Go's GC, or decorator patterns with no Go equivalent. Invalid
   divergences are hand-coding cluster revisions, attribute IDs, constraint
   defaults, status codes, or Apple-required tag patterns — those go verbatim
   from matter.js.

## 4. The standing guards (enforcement)

Parity is held by build- and test-time guards, **not** by periodically
regenerated audit reports. These are the mechanism; keep them green and extend
them with every change:

| Guard | Location | Locks |
| --- | --- | --- |
| **Schema parity** | [`parity/`](../parity) (the embedded matter.js HEAD extract) + [`schema/parity_test.go`](../schema/parity_test.go), `schema/provenance_test.go`, `schema/staleness_test.go`, and the `*parity_matterjs_test.go` files across the tree | cluster / device-type IDs, revisions, attribute / command / event IDs vs matter.js HEAD; that the generated Go maps match the snapshot they were generated from |
| **Behavioural negative-write parity** | [`cluster/matter_negative_write_parity_test.go`](../cluster/matter_negative_write_parity_test.go) | a write/invoke matter.js *rejects* is rejected with the matching IM status (ConstraintError 0x87 / InvalidCommand 0x85), plus boundary positive controls against over-rejection. Add one row per new constraint. |
| **Wire-codec parity** | [`tlv/parity_matterjs_test.go`](../tlv/parity_matterjs_test.go) and [`im/wire_fixtures_parity_test.go`](../im/wire_fixtures_parity_test.go), each reading its own `testdata/` copy of the fixtures; the generators live at [`notes/parity/matter/`](../notes/parity/matter) | byte-level TLV / IM shape |
| **Reference-controller validation** | [`conformance/`](../conformance) and `internal/chiptool/` under the `chiptool` build tag, driven by `.github/workflows/chiptool.yml` | end-to-end behaviour against the real `chip-tool` commissioner, plus borrowed CSA `Test_TC_*` cases |
| **matter.js pin freshness** | `.github/workflows/matterjs-pin.yml` + `schema/staleness_test.go` | the embedded extract does not silently fall behind matter.js HEAD |
| **Divergence catalogue** | [`notes/parity/by_design.md`](../notes/parity/by_design.md) | every intentional deviation, with rationale and what pins it |
| **Open-gap register** | [`notes/parity/matter_behaviour_findings.md`](../notes/parity/matter_behaviour_findings.md) | known behaviour gaps that are *not* by design, as scoped fix packages |

When a parity sweep is genuinely warranted, prefer extending these guards over
producing a throw-away report: a guard catches the *next* regression, a report
catches only today's.

## 5. Where the host's own parity obligation begins

This module owns the wire format. A bridge is this module *plus* a host that
projects its own devices onto clusters, and the host half has its own gold
standard — for OpenCCU-Loom, the reference consumer, that is `aiohomematic` on
the CCU side. The two reference layers do not overlap: CCU wire knowledge stays
in aiohomematic, Matter wire knowledge stays in matter.js.

The boundary is the [`contract/`](../contract) package. Everything on this side
of it mirrors matter.js and is covered by §4. Everything on the far side —
which data point becomes which cluster, which device type a product maps to —
is the host's decision and the host's parity contract. A host that gets the
projection wrong produces a device that pairs and then misbehaves, which is
exactly the class of defect §2 is about; it is simply not caught by the guards
listed here.

One asymmetry worth stating: a port of a CCU stack converges, because the
source stands still. The Matter mirror never "finishes" — matter.js HEAD bumps
cluster and device-type revisions, and the wire shape is interop-critical.
Parity here is a permanent discipline, re-verified on every change and whenever
the matter.js pin is advanced.

## 6. The non-negotiable checklist

Before you open a PR:

- [ ] I read the matter.js (and, for wire-truth, chip) source for this
      cluster / behaviour / device-type.
- [ ] My implementation mirrors its defaults, constraints, status codes, and
      order — or the divergence is recorded in `by_design.md` (+ ADR if
      non-trivial).
- [ ] I cited the matter.js / chip `path:line` in the Go code.
- [ ] I added or extended a **behaviour** parity test (a constraint row, a
      command-semantics case, a wire fixture) — not only a schema assertion.
- [ ] The standing guards in §4 are green.

If you cannot check every box, the change is not ready.
