# Architecture Decision Records

Decisions that shaped this module and are expensive to re-litigate. An ADR is
written when a choice is architectural, contested, or would otherwise look like
an oversight to the next reader — including choices to *not* do something.

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](./0001-matter-commissioning-bring-up.md) | Matter wire-protocol design rules from chip-tool bring-up | accepted |
| [0002](./0002-im-opcode-dispatch-seam.md) | A testable gate + per-opcode seam in `handleIMOpcode` | accepted |
| [0003](./0003-sigma-resume-extraction.md) | Sigma resumption extraction: already satisfied (finding corrected) | accepted, no code change |
| [0004](./0004-groups-cluster-stays-stub.md) | Groups stays a stub — a deliberate, matter.js-conformant divergence | superseded by 0009 |
| [0005](./0005-bridge-decomposition.md) | Defer the `Bridge` CommissioningSession / IMEngine facade split | accepted, deferred with a plan |
| [0006](./0006-subscribe-dispatch-seam.md) | Cohesive sub-helpers out of `handleSubscribeRequest` | accepted |
| [0007](./0007-chiptool-send-receive-matrix.md) | Hermetic per-type send/receive suite against chip-tool | accepted |
| [0008](./0008-subscription-resumption.md) | Persist server subscriptions and re-establish them after restart, with a CASE initiator scoped to that alone | accepted |
| [0009](./0009-groups-and-group-messaging.md) | A real Groups server and group message reception, with membership as stack state; Groupcast deferred to 0010 | accepted |
| [0010](./0010-groupcast-and-auxiliary-acl.md) | Groupcast (Listener, PerGroup) and the AccessControl Auxiliary ACL on the root, on the ADR 0009 group state; no Sender | accepted |
| [0011](./0011-certifiability-is-a-goal.md) | Certifiability is a goal, certification is not pursued; certification families are the yardstick, every exclusion is classified, PICS are honest, tests replace manual device testing | accepted |
| [0012](./0012-scenesmanagement-server.md) | A real ScenesManagement server on the bridged lights, as matter.js builds it; RemainingCapacity bounded by the shared table as chip does | accepted |
| [0013](./0013-generated-cluster-definitions.md) | Cluster definitions generated from the matter.js model (one package per cluster under `cluster/spec/`), a small runtime that derives lists, privileges, write checks and codecs from them; servers keep only the host port and the rules | accepted |
| [0014](./0014-transitions-optional-per-endpoint.md) | Attribute transitions run in the module (`cluster/transition`, matter.js `Transitions.ts`), optional per endpoint; a host whose device ramps natively keeps the hand-off path | accepted |
| [0015](./0015-chip-data-model-read-at-run-time.md) | The CHIP data model cross-check reads connectedhomeip's `data_model/<version>` from a checkout at test time and commits nothing derived from it (the CSA notice forbids publishing or deriving); CI provides the checkout | accepted |
| [0016](./0016-device-type-validation.md) | Assembled topologies are validated against their device types — a port of matter.js's `DeviceTypeConformance` and its service's modes (warn by default, strict, off) — on every bridge assembly, with a structured verdict for the host | accepted |
| [0017](./0017-generated-default-cluster-server.md) | A generated default cluster server in `cluster/spec`, so a cluster without matter.js logic costs its definition and a host port; the hand-written schema tables are generated too | accepted, implemented in #40 |
| [0018](./0018-device-layer-and-node-facade.md) | A device layer `device/` generated from the device-type model and a `node/` facade — the port of matter.js `devices/`, `endpoints/` and `ServerNode`; the existing `contract/` level stays beneath it, foreign-source projection stays outside | proposed (concept phase 4) |
| [0019](./0019-tcp-bdx-and-the-ota-requestor.md) | TCP (full large-payload path), BDX and the complete OTA Software Update Requestor enter the scope in wave 5; DiagnosticLogs stays without BDX (matter.js has none); the OTA Provider stays out — both exclusions superseded by [0021](./0021-ota-provider-and-diagnosticlogs-over-bdx.md). | Accepted |
| [0020](./0020-commissioner-and-controller-role.md) | The commissioner and controller role enters the scope as `controller/`, a sibling of `bridge/`, ported from matter.js in six stages; on-network only; the responder side and `contract/` are unchanged; supersedes the no-controller non-goal (CLAUDE.md, comparison §Non-goals 1, ADR 0008's exception, concept v1 §F.1) | accepted 2026-10-13 (concept v2 phase 7) |
| [0021](./0021-ota-provider-and-diagnosticlogs-over-bdx.md) | The OTA Software Update Provider (own OtaProvider endpoint, host ports for image source, consent and apply policy, the BDX sender roles) and DiagnosticLogs over BDX (chip as the reference: matter.js has no DiagnosticLogs server) enter the scope; supersedes ADR 0019's two exclusions; in-repo device-to-device proofs; the harness image pin stays | accepted 2026-10-14 (concept v2 phase 8) |
| [0022](./0022-media-and-content-clusters.md) | The fifteen media and content clusters (fourteen through the standard server behind typed host ports in `cluster/media`, Messages core-held for its fabric-scoped list) and the three media device types enter the scope; the reference daemon's player and content-app endpoints carry chip's media families; CastingVideoClient stays out; the ecosystem proof waits for phase 5 | accepted 2026-10-14 (concept v2 phase 9) |
| [0023](./0023-apache-2-and-the-cla.md) | Apache License 2.0 from the next release (matter.js's and connectedhomeip's licence, with the patent grant); the released MIT versions stay MIT; a contributor licence agreement (`CLA.md`) so the module can be licensed as a whole later; dependencies stay permissive | accepted 2026-10-15 |
| [0024](./0024-ble-commissioning-in-the-controller.md) | The commissioner commissions over BLE (BTP in `transport/btp`, the `controller/ble` seam, BlueZ over D-Bus in the separate module `ble/bluez`, Linux only, no cgo) and provisions Wi-Fi or Thread credentials; the device side stays without Bluetooth | accepted 2026-10-16 (concept v3) |
| [0025](./0025-facade-design-principles.md) | Design principles of the facade: the facade is the product, one shape, no leaked types, seams are not the facade, spec vocabulary, semantics from matter.js, errors as values, callbacks inline with an async option, every break announced (apidiff), the same bar inside, provable without hardware, few dependencies; two review gates in phase 20 | accepted |
| [0026](./0026-architecture-principles.md) | Architecture principles of the module, for every layer: layers and direction (held by `script/archcheck`), one composition root per role, generated code from one source, time and randomness injected, goroutine ownership, few named ordered locks, persistence as one seam, errors as values in one style, `slog` with injected loggers, tests as architecture, structural rules as guards, role-neutral shared packages; the phase-20 gates audit the whole tree | accepted |
| [0030](./0030-the-served-surface-is-a-value-under-policies.md) | The served surface is an explicit value (`surface`: canonical, hashable, diffable) under node-level policies with a decision log: every policy judges every candidate element with its origin, Refuse over Remove over Keep, code against code refuses, code against an operator setting removes, an element mandatory at the pin is never removed; a device under other policies is refused at `Add`; whole clusters are not gated | accepted 2026-10-30 (concept v11) |

## Provenance

ADRs 0001–0007 were recorded in
[OpenCCU-Loom](https://github.com/SukramJ/openccu-loom/tree/main/docs/adr) while
the Matter stack still lived there as `internal/north/matter/`, and moved here
with the stack. They were renumbered into this module's own sequence; each file
names the OpenCCU-Loom number it carried. ADR 0008 onwards were written here. Decisions that are about *projecting a
device model onto Matter* — which HomeMatic device becomes which device type,
how many endpoints a physical device gets — stayed there, because that is the
host's decision, not this module's.

## Related

- [`../matter-parity-contract.md`](../matter-parity-contract.md) — what parity
  means and which guards enforce it. Read this before your first change.
- [`../../notes/parity/by_design.md`](../../notes/parity/by_design.md) — the
  divergence catalogue. A non-trivial entry there also gets an ADR here.
