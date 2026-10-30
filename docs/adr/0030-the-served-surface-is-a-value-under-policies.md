# ADR 0030 — The served surface is a value under policies

- **Status**: Accepted (owner, 2026-10-30)
- **Date**: 2026-10-30
- **Related**:
  [`../history/concept-v11-surface.md`](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v11-surface.md)
  (the concept, German working paper; issues #331 and #332),
  [ADR 0015 — the CHIP data model is read at test time](./0015-chip-data-model-read-at-run-time.md),
  [ADR 0018 — device layer and node facade](./0018-device-layer-and-node-facade.md),
  [ADR 0026 — architecture principles](./0026-architecture-principles.md)

## Context

What a controller sees of a node — the device types and their revisions,
the clusters and their `ClusterRevision`, `FeatureMap`, `AttributeList`,
`AcceptedCommandList`, `GeneratedCommandList`, `EventList` — decides
whether Apple Home, Google Home or Alexa pair, load and keep working. This
ADR calls that the **surface**.

Until this decision the surface was a by-product of three sources nobody
saw together: the device type's requirements (`device/type.go`), the
host's declaration (`cluster/spec/declaration.go`) and the kit server's
defaults (`device/device.go`, `device/defaults*.go`). A server refused
only a feature bit it does not serve at all. Whether matter.js implements
an element, whether a chosen specification revision knows it, whether a
given controller tolerates it — nothing at runtime knew, and nothing
recorded why an element was present or absent. A host that enabled an
ahead-of-matter.js feature late (openccu-loom 0.94.0, Thermostat
MatterScheduleConfiguration) saw Apple Home stop loading data; which
element broke it was not measured.

Two issues asked for two switches: a matter.js compatibility mode (#331)
and a specification-level ceiling (#332). Both are rules over the same
value.

## Decision

1. **The surface is an explicit value.** Package `surface` holds it:
   `surface.Surface` is the node's surface, canonically ordered, hashable
   (`Digest`), diffable and JSON-encodable — what a wildcard read of
   Descriptor and the global attributes returns.

2. **Every rule over the surface is a policy with a decision log.** A
   `surface.Policy` judges candidate elements that carry their provenance
   (`Origin`: cluster-mandatory, device type, host-declared, kit default;
   `Reference`: matter.js baseline or an *Ahead of matter.js* register
   row; the specification levels the element exists in; whether it is
   mandatory at the pin). The chain evaluates every policy for every
   element; precedence is `Refuse` over `Remove` over `Warn` over `Keep`
   (`Warn` keeps the element and flags it — a profile's row below
   *Reproduced*; amended 2026-10-30, release 0.15.3); the
   `Report` names every decision. `surface.Policies` is the node-level
   set: `Extensions` (#331), `Level` (#332), `Profiles` (controller
   profiles), `Custom` (host rules). Its zero value is the matter.js
   baseline at the pin's revision with no profile.

3. **Code against code refuses; code against an operator setting removes.**
   A host declaration that asks for an ahead element the host did not
   enable is a bug: the build fails with `ErrExtensionNotEnabled` naming
   the register ID. A declaration that an operator-chosen level or profile
   does not know is removed and logged; a node never dies of an operator
   choice. An element mandatory at the pin is never removed by an
   operator-side policy, and `ClusterRevision` always stays the pin's:
   the module keeps its conformance at the newest revision.

4. **Policies are node-level only.** `node.Config.Surface` holds them; a
   device built under a different `Policies.Digest()` is refused at `Add`
   (`ErrPolicyMismatch`). Per-device differentiation is a `Custom` policy
   over `Element.Endpoint`.

5. **Specification levels come from matter.js's own release history**, not
   from connectedhomeip's `data_model/<version>` XML. The matter.js tags
   carry the model at the revision their `Specification.REVISION` names
   (measured: v0.13.0 = 1.4, v0.14.0 = 1.4.1, v0.16.0 = 1.4.2, v0.17.0 =
   1.5.1, v0.17.9 = 1.6.0, v0.18.0 = 1.6.1); the same extractor reads them
   under Apache-2.0. ADR 0015 stands: the CSA XML is read at test time as
   the cross-check of the generated level table and nothing of it is
   committed. The selectable levels are exactly the revisions matter.js
   shipped.

6. **A surface change under a paired controller is announced, never
   silent.** Each endpoint's effective surface and its digest are persisted
   in the node's settings next to the ConfigurationVersion (amended
   2026-10-30, wave S4: no endpoint-record or store change); a drift raises
   `ConfigurationVersion` and the Descriptor's data version and logs the
   diff. No mode re-allocates endpoint numbers; a
   measured controller profile may ask for one later.

7. **Whole clusters are not gated.** Which clusters reach an endpoint is
   the host's decision through the device types it declares (owner,
   2026-10-29).

8. **Controller profiles ship empty.** A profile row removes an element
   only with the confidence *Reproduced* (observed against a real
   controller in this project's own bring-up); *Reported* and *Inherited*
   rows stand in the register and appear in the log as warnings.

## Consequences

- Every pull request that changes what a controller sees shows up as a
  diff of the committed surface snapshot (`script/surfacecheck`, a
  ratchet like the reachability inventory). The guard catches the next
  accidental exposure, not only today's.
- Hosts that set the Thermostat MSCH or TEVT bit today must add the
  register ID to `Policies.Extensions`; the CHANGELOG announces this as a
  break under `### Changed`.
- `make ahead-check` gains the register column `surface` (measured by
  building each cluster with and without the row's elements) and the
  two-direction completeness check between code declarations and register
  rows.
- The host's operator-facing settings (which extensions, which level,
  which profile) stay host-side; this module exposes the mechanism and
  the report (`Node.Surface()`), never a UI.

## Alternatives considered

- **Per-feature host toggles (status quo).** Rejected: nothing distinguishes
  baseline from ahead at runtime; nothing records why; nothing announces a
  change.
- **Strip silently instead of refusing.** Rejected for code-against-code:
  a host bug would hide behind a quietly smaller surface.
- **A committed level table from the CSA XML (#332 as filed).** Rejected:
  a publication and a derivative work under the files' notice (ADR 0015).
- **Gating whole clusters.** Rejected by the owner: the device types are
  the host's cluster decision.
- **The node owns composition (devices built at `Add`, as matter.js
  initialises behaviors at `node.add`).** The cleaner long-term model and
  an API break; deferred to v1.0. The composition is already one function
  so the move stays possible.
