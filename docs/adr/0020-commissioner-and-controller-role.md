# ADR 0020 — The commissioner and controller role enters the scope as `controller/`; the responder side stays as it is

- **Status**: Accepted (owner, 2026-10-13; proposed the same day; concept v2
  [`../concept-v2-scope-extension.md`](../concept-v2-scope-extension.md)
  decision 0.1, phase 7). The non-goal it supersedes is retired; phase 7
  starts with stage 1.
- **Date**: 2026-10-13
- **Related**:
  [ADR 0008 — subscription resumption (the narrow CASE initiator)](./0008-subscription-resumption.md),
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  [ADR 0018 — device layer and node facade](./0018-device-layer-and-node-facade.md),
  [ADR 0019 — TCP, BDX and the OTA requestor](./0019-tcp-bdx-and-the-ota-requestor.md),
  [`../matterjs-comparison.md`](../matterjs-comparison.md) §1–§3 and §Non-goals,
  concept v1 [`../concept-matter-1.6.1-and-device-layer.md`](../concept-matter-1.6.1-and-device-layer.md) §F.1,
  `notes/parity/by_design.md` `BD-Matter-GroupcastNoSender`,
  `BD-Matter-Commissioner-TestCA` (the test commissioner's fixed issuing instant)

## Context

Four places record "no controller / commissioner role" as a decision, not
backlog: `CLAUDE.md` §"Scope boundaries", `docs/matterjs-comparison.md`
§Non-goals item 1, ADR 0008 (which carved out the one bounded exception —
the device re-establishing its own former subscriptions opens a CASE
session as the initiator and does nothing else), and concept v1 §F.1
("a different product, not an extension"). The reasoning was that a bridge
answers; a second state machine, a certificate-issuer role and a device
cache serve no bridge.

Concept v2 (2026-10-12, decision 0.1) reverses that for two consumers
that did not exist when the non-goal was written:

1. **A controller daemon as a product** outside this module (working title
   `go-matter-controller`, the same relationship the reference daemon has
   to `node/`): it commissions nodes, keeps their sessions, subscribes and
   offers the devices to a host.
2. **The certification harness.** `internal/chiptool` commissions the
   reference daemon with chip-tool and with matter.js's controller; the
   BIND family and every "controller" case need a third party that the
   harness can drive. A commissioner of our own replaces both for those
   cases and proves the role the way the responder is proved.

What exists today is a responder with the initiator's wire pieces already
in place: `secure/sigma.NewPeerInitiator` and `bridge/case_initiator.go`
(ADR 0008), `bridge/bridgetest.Commissioner` (#53: PASE, ArmFailSafe,
attestation and CSR requests, AddTrustedRootCertificate, AddNOC, CASE,
CommissioningComplete in chip-tool's AutoCommissioner order, over a test
CA), `secure/mattercert` (the certificate codec), `secure/attestation`
(the device side), `mdns/` (an advertiser and an operational resolver, no
browser), `commissioning/` (the responder side of PASE). matter.js's
controller is `packages/protocol/src/peer/` (`PeerSet`, `Peer`,
`PeerConnection`, `ControllerCommissioner`, `ControllerCommissioningFlow`,
`CommissioningConnection`), `packages/protocol/src/mdns/CommissionableMdnsScanner.ts`
and `common/Scanner.ts`, `session/pase/PaseClient.ts`,
`session/case/CaseClient.ts`, `packages/node/src/node/ClientNode.ts` and
`node/client/`, `behaviors/binding/`, the sender half of
`behaviors/groupcast/GroupcastServer.ts`, the Joint Fabric behaviours and
`protocol/src/dcl/` — about 22 000 lines at `0ceb202d`, measured in
concept v2 §B.1.

## Decision

**The controller and commissioner role is in scope, as a package of its
own, ported from matter.js in the six stages of concept v2 §B.1.** The
boundary:

1. **`controller/` is a sibling of `bridge/`, not part of it.** It
   imports `secure/`, `tlv/`, `im/` (the codecs), `mdns/`, `transport/`
   and `schema/`, and owns its state machine, its peer table, its
   certificate authority and its client-side interaction. `bridge/`,
   `node/`, `device/` and `endpoint/` do not import it and are not changed
   for it. A host that embeds the responder keeps a module whose
   controller code is dead weight the linker drops.
2. **`contract/` stays stdlib-only** and stays the responder's host
   boundary. The controller's host-facing surface is its own
   (`controller/` types; a later ADR if it needs a contract package).
3. **The shared packages are now used by two roles.** `secure/`, `tlv/`,
   `im/`, `mdns/`, `transport/` carry the responder's parity guards and
   the chip-tool suite; every change made for the controller runs through
   those guards first, and a divergence a role needs is a `by_design`
   entry naming the role. The initiator paths of `secure/sigma` and the
   mDNS browser are the first such shared code; ADR 0008's initiator
   becomes the first user of `controller/`'s session client rather than a
   special case in `bridge/`.
4. **On-network only.** Nodes are discovered and commissioned over IP
   (DNS-SD `_matterc._udp`, the way chip-tool `pairing onnetwork` and
   matter.js's `CommissionableMdnsScanner` do). No BLE, no Thread, no
   Wi-Fi commissioning over BLE — those non-goals stand (concept v2
   decisions 0.4, 0.6).
5. **The certificate authority is the package's default CA**, grown from
   `bridgetest`'s test CA (stage 2; since wave 16 `bridgetest` mints
   through it, its fixed issuing instant staying `by_design`). Attestation is verified against a PAA
   store the host supplies, with the CSA test PAAs as the default for the
   harness. The DCL client (stage 6) is an HTTP client against the CSA
   ledger over `net/http`; it is the last stage and separable.
6. **No device model of its own in the controller.** The client view of a
   remote node comes from `schema/` and the `device/` types (stage 4, the
   port of `ClientNode` / `ClientStructure`); the controller daemon
   projects it to its host, as the reference daemon projects the other
   way.
7. **matter.js stays the single gold standard** for the controller as for
   the responder; chip-tool is the authority on tool behaviour the
   harness compares against (its argument handling, its output), not a
   second reference for the protocol. Every `// Mirrors matter.js …`
   comment, parity test and `by_design` rule applies unchanged.

The stages and their acceptance (from concept v2 §B.1):

| Stage | Ports | Done when |
| --- | --- | --- |
| 1 Transport client | `CommissionableMdnsScanner.ts`, `common/Scanner.ts`, `PaseClient.ts`, `CaseClient.ts`, `peer/PeerConnection.ts`, `PeerSet.ts`, `Peer.ts`, `PeerAddressMonitor.ts` | the module browses DNS-SD, opens PASE and CASE sessions to any node and follows its addresses |
| 2 Commissioning | `ControllerCommissioningFlow.ts`, `ControllerCommissioner.ts`, `CommissioningConnection.ts`, `DeviceCommissioner.ts`, the CA, attestation with a PAA store | the harness commissions the reference daemon with `controller/`; `bridgetest.Commissioner` is a thin wrapper or gone — done: #83 (wave 14) and #93 (wave 16, the wrapper) |
| 3 Client interaction | `action/client/` (`ClientInteraction.ts`, subscriptions, `InputChunk.ts`) | Read, Write, Invoke, Subscribe with chunking and resumption, proved against the responder |
| 4 Client node model | `ClientNode.ts`, `node/client/` | a typed image of a remote node from the schema; `device/` types as the client view — done: #95 (wave 17, brief AK: `controller/client`, the Descriptor-driven image, cache and store, the shape analysis instead of generated types, the default sustained subscription, connection state, physical properties) and #97 (wave 18, brief AL: `client.Registry` with `peerN` ids, restore on start, the commissioning record and address book with matter.js's expiry, commission/decommission, the leave/shutDown/startUp reactions, the fabric-label reconciliation, `controller/client/sqlstore`); the `device/` types as the client view: #105 (wave 22, brief AP: `client.Endpoint.Device()`/`Conformance()` over the one device-type validator; the Binding list persisted) — stage 4 closed |
| 5 Binding, Groupcast sender, controller device types | `behaviors/binding/`, the sender half of `GroupcastServer.ts`, the 11 controller device types in `script/devicegen` | the BIND family runs green with the own commissioner; `BD-Matter-GroupcastNoSender` retires — part 1 done: #101 (wave 20, brief AN: the group sender in `groups/`, the Groupcast Sender feature; both no-sender entries retired); part 2 done: #103 (wave 21, brief AO: `binding.Manager`, `node.Config.Client`, the 11 controller types, `TestLiveBinding` after TC-BIND-2.1 — chip's BIND cases are manual YAML, so the proof is in-repo); stage 5 closed |
| 6 Joint Fabric, DCL | `joint-fabric-*`, `commissioner-control`, `protocol/src/dcl/` | cross-fabric administration; DCL checks of attestation and OTA URLs — split 2026-10-14 by the owner: **6a done:** #109 (wave 25, brief AS: `controller/dcl` — `DclClient.ts` and the used parts of the certificate and OTA services, `dcl.CertificateStore` as `attestation.PAAStore`/`Revocations`, `dcl.OTAVersions`; opt-in, nothing contacts the ledger unless the host constructs a client); **6b done:** #109 (`core.CommissionerControl` after chip's server and the fabric-sync delegate — matter.js has an empty class — `node.Config.CommissionerControl`, `TestLiveCommissionerControl`: the node's own commissioner commissions the client through the reverse window); **6c Joint Fabric deferred past `v1.0.0`** (no matter.js behaviour at the pin, no proof without chip's JF apps; the stage is separable, as below) |

One brief per stage (the stage table is the brief list), each with its
harness proof; stage 6 is separable and may be deferred without touching
the others.

## Consequences

- `CLAUDE.md` §"Scope boundaries", `docs/matterjs-comparison.md`
  §Non-goals item 1, ADR 0008 and concept v1 §F.1 carry a note that this
  ADR supersedes the non-goal; the text they keep is history. The
  comparison's §1–§3 "Controller" rows move from **No** to the stage that
  ports them when the stage lands, not before.
- `v1.0.0` moves behind phases 7–9 (concept v2 decision 0.11): the freeze
  then covers `device/`, `node/` and `controller/` together; consumers stay
  on `v0.x` guarantees meanwhile (README §API stability).
- The reachability inventory and the coverage floors gain a package; the
  chip-tool CI gains a leg in which `controller/` is the commissioner
  (`chiptool.yml`), beside the chip-tool and matter.js-controller legs,
  which stay — the control leg's purpose (separating our defects from the
  environment's) needs a commissioner that is not ours.
- `secure/sigma` and `mdns/` get initiator- and browser-side parity tests
  of their own (`PaseClient.ts`, `CaseClient.ts`,
  `CommissionableMdnsScanner.ts` fixtures); the responder fixtures are not
  touched.
- Not decided here: the controller daemon's own host contract, the
  persistence of peers and the CA key (a `store/` extension or the
  daemon's own), and whether the DCL client lives in `controller/` or
  beside it. Each is a later ADR when its stage starts.
