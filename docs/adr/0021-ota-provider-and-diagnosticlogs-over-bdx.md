# ADR 0021 — The OTA Software Update Provider and DiagnosticLogs over BDX enter the scope

- **Status**: Accepted (2026-10-14; concept v2 phase 8,
  [`../concept-v2-scope-extension.md`](../history/concept-v2-scope-extension.md) §B.2, decision 0.3)
- **Date**: 2026-10-14
- **Related**:
  [ADR 0019 — TCP, BDX and the OTA Software Update Requestor](./0019-tcp-bdx-and-the-ota-requestor.md)
  (superseded in two points: the provider is no longer out, DiagnosticLogs
  no longer stays without BDX),
  [ADR 0020 — the commissioner and controller role](./0020-commissioner-and-controller-role.md)
  (the proof's counterpart),
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  `notes/parity/by_design.md` `BD-Matter-OTAProvider-NotExposed`,
  `BD-chip-DiagLogs-NoBDX` (both retire with this ADR's waves)

## Context

ADR 0019 brought BDX and the OTA *requestor* into the module and left the
provider out ("another product") and DiagnosticLogs without BDX ("matter.js
has none"). Concept v2 (2026-10-12, decision 0.3) reverses both for
`v1.0.0`: a bridge that updates its bridged devices' firmware needs to
serve images, and a bridge in a certification lab must hand its logs over
BDX. The first read at the pin (`0ceb202d`, 2026-10-14) measured:

- **OTA provider in matter.js:** `OtaSoftwareUpdateProviderServer.ts`
  (668 lines) is a full server: QueryImage with a software-update manager
  that validates the peer's BasicInformation and asks a consent and image
  source, Busy with a 2-minute retry (`:218`, `:235`), the BDX transfer
  offered as `BdxSynchronous` with a 5-minute idle timeout (`:289-304`),
  ApplyUpdateRequest answering Proceed or, without consent after 120 s,
  Discontinue (`:248`, `:434-435`), NotifyUpdateApplied. The BDX
  **sender** side exists in `packages/protocol/src/bdx/` (`OutboundFlow`,
  `DrivenSendingFlow`, `FollowingReceivingFlow` — the receiver-driven
  transfer a requestor's `ReceiveInit` + `BlockQuery` opens; concept v2
  B.2 named `FollowingSendingFlow`, which is the receiver's flow `bdx/`
  already ports). The URI is `bdx://<root node id, 16 hex>/ota/<file>`,
  which `bdx.FileDesignator.BdxURI` already produces. The generated
  definition `cluster/spec/otasoftwareupdateprovider` and the device type
  OtaProvider (0x0014) exist; the type needs its own endpoint
  (`BD-Matter-OTAProvider-NotExposed`: on the root, Apple Home rejects the
  node).
- **DiagnosticLogs in matter.js:** `DiagnosticLogsServer.ts` is an empty
  class — matter.js answers nothing. The premise of `BD-chip-DiagLogs-NoBDX`
  ("matter.js has no BDX transfer") was true but incomplete: it has no
  server at all, so chip is the only behaviour reference, as CLAUDE.md
  provides for what matter.js does not model. chip's
  `diagnostic-logs-server` (`DiagnosticLogsCluster.cpp`,
  `BDXDiagnosticLogsProvider.cpp` at the harness pin) is the BDX
  **initiator and sender**: on `RetrieveLogsRequest(BDX, designator)` the
  device opens an exchange on the command's session, sends `SendInit`,
  answers the command with Success after `SendAccept`, then streams
  1024-byte blocks; Exhausted when the logs fit the response, Denied when
  the provider fails, InvalidCommand without a designator, ConstraintError
  above 32 characters, the oldest entry's timestamps in a Success
  response. The one DLOG case stays manual at the pinned image (an
  automated `TC_DLOG_2_1.py` exists only after the pin).
- **Two BDX roles are missing** in `bdx/`: the responder-and-sender (the
  provider) and the initiator-and-sender (DiagnosticLogs); the messenger
  and the receiver flows exist. The bridge routes no BDX protocol
  messages inbound (`bridge/receive.go`: IM and SecureChannel only).

## Decision

1. **The OTA Software Update Provider (0x0029) enters `cluster/core`** as a
   port of `OtaSoftwareUpdateProviderServer.ts` with matter.js's defaults
   and statuses, and host ports for what matter.js's software-update
   manager decides: the image source (which image for a vendor/product/
   version, as a file designator), user consent, and the apply policy.
   The BDX sender side is ported into `bdx/` from matter.js's flows; the
   bridge routes inbound BDX to the registered responder. The provider
   lives on its **own endpoint** of device type OtaProvider (0x0014),
   assembled by the host; the reference daemon exposes one behind a
   TESTING-ONLY flag with a test image. `BD-Matter-OTAProvider-NotExposed`
   retires.
2. **DiagnosticLogs serves BDX** as chip does: the server becomes a BDX
   initiator and sender on the requesting session, with chip's statuses
   and limits, cited line by line from `diagnostic-logs-server`;
   `BD-chip-DiagLogs-NoBDX` retires. The harness image pin does not move
   for this ADR; DLOG/2.1 stays a manual case.
3. **The proofs are in-repo and device-to-device**, as ADR 0020's are:
   the module's own requestor downloads from the module's own provider
   (two `node.Node`s on one fabric, commissioned by `controller/client`),
   and a DiagnosticLogs transfer is received by a BDX receiver on the
   controller side. The SU provider cases of chip (SU-3.x) are manual
   YAML; nothing built on this is described as certified (ADR 0011).
4. **Order:** one wave (brief AQ): the BDX sender roles first, then the
   provider, then DiagnosticLogs; the reference daemon's provider endpoint
   last. Phase 9 (media) and ADR 0020 stage 6 are unaffected.

## Consequences

- `docs/matterjs-comparison.md` (the OTA and DiagnosticLogs rows),
  concept v1 §B.2's "provider out" line, ADR 0019's two points and
  `docs/certifiability.md` (the SU and DLOG families' notes) are updated
  by the wave's docs PR; `CLAUDE.md`'s scope boundaries name neither and
  do not change.
- The host's obligation grows by three ports (image source, consent,
  apply policy) and one endpoint; a host that wires none of them has no
  provider, as before.
- Two `by_design` entries retire; the differences the port keeps (a Go
  file-backed image store instead of matter.js's DCL-aware manager, the
  pinned image's manual DLOG case) are recorded in `notes/parity/` by the
  owner with the wave.
