# ADR 0019 — TCP, BDX and the OTA Software Update Requestor enter the scope; DiagnosticLogs stays without BDX

- **Status**: Accepted (2026-10-11; wave 5 of
  [`../concept-matter-1.6.1-and-device-layer.md`](../history/concept-matter-1.6.1-and-device-layer.md), phase 3)
- **Date**: 2026-10-11
- **Superseded in part** by [ADR 0021](./0021-ota-provider-and-diagnosticlogs-over-bdx.md) (2026-10-14): the OTA provider enters the scope and DiagnosticLogs serves BDX; the rest stands.
- **Related**:
  [ADR 0008 — subscription resumption (the narrow CASE initiator)](./0008-subscription-resumption.md),
  [ADR 0011 — certifiability is a goal](./0011-certifiability-is-a-goal.md),
  `notes/parity/by_design.md` `BD-chip-DiagLogs-NoBDX`, `BD-Matter-OTAProvider-NotExposed`,
  `BD-Matter-PASE-SessionParametersLegacy`

## Context

The concept's phase 3 planned TCP (matter.js `protocol/src/transport/tcp`) and
BDX (`protocol/src/bdx`, 2 127 lines) "as a responder for
DiagnosticLogs.RetrieveLogsRequest", and asked for the scope to be
re-confirmed before the phase starts because, without OTA, the transfer
would serve the DLOG family alone. Measured on 2026-10-11 against matter.js
HEAD and the harness pin 6170af84:

- matter.js's `DiagnosticLogsServer.ts` is 14 lines and does no BDX; the
  only BDX users in matter.js are `OtaSoftwareUpdateRequestorServer.ts`
  (1 436 lines) and `OtaSoftwareUpdateProviderServer.ts` (668 lines).
- chip's DLOG family has one case, `Test_TC_DLOG_2_1.yaml`, a manual case
  (class (c) in `docs/certifiability.md`). A BDX responder for logs would
  follow chip's `DiagnosticLogsServer.cpp`, not the gold standard, and no
  automated case would prove it.
- The SU family has 13 cases (10 YAML, 5 Python at the pin) and the BDX
  family 11 YAML cases; matter.js's chip-testing runs both against its own
  requestor and chip's `chip-ota-provider-app` (`support/chip-testing/test/cert/AGENTS.md:61`,
  `OtaRequestorTestInstance.ts`), so the harness image carries the provider.
- TCP is Matter's large-payload path: `SessionParameters` carries
  `supportedTransports` (tag 8) and `maxTcpMessageSize` (tag 9, default
  64000; `PaseMessages.ts:28-58`), the mDNS `T` key advertises TCP client
  and server support (`DeviceAdvertiser.ts:376`, `ServerNetworkRuntime.ts:337`),
  frames carry a 4-byte little-endian length (`TcpChannel.ts:37-41`), and
  a dropped connection evicts every session bound to it
  (`ExchangeManager.ts:633-660`, "mimics CHIP SDK").

## Decision

Wizard of 2026-10-11 (owner):

1. **Wave 5 ports TCP, BDX and the complete OTA Software Update Requestor.**
   The requestor is the consumer that makes BDX a matter.js-mirrored path:
   QueryImage, BDX download, ApplyUpdateRequest / NotifyUpdateApplied,
   AnnounceOTAProvider, fabric-scoped persistent DefaultOTAProviders, with
   the host applying the image through a port
   (`OtaSoftwareUpdateRequestorServer.ts` `applyUpdate`, `requestUserConsent`).
   Appendix F.4's OTA row moves into scope; the OTA **Provider** stays out
   (`BD-Matter-OTAProvider-NotExposed`: a distribution role).
2. **TCP is served fully**, not only as BDX's carrier: the length-framed
   listener and dialer, the large-payload session parameters negotiated on
   CASE and PASE (and PASE unified onto `sigma.SessionParameters`, retiring
   `BD-Matter-PASE-SessionParametersLegacy`), large Read / Subscribe /
   Invoke payloads over TCP, the mDNS `T` flag with a TCP listener in the
   reference daemon, session eviction on connection drop, and session
   establishment (CASE and PASE) arriving over TCP.
3. **DiagnosticLogs keeps answering without BDX.** `BD-chip-DiagLogs-NoBDX`
   is re-read as mirroring matter.js; DLOG/2.1 stays a manual case.
4. The wave-4 harness follow-ups (per-case endpoint selection with matching
   PICS, the second ESA, the fleet-wide wire-shape guard, EGC, TagList,
   the CommodityTariff revision cross-check) run in parallel as a fourth
   brief, and the harness is wired for the SU and BDX families with chip's
   `chip-ota-provider-app` from the image.

## Consequences

- The module gains `transport/tcp`, a transport-neutral connection
  abstraction in `bridge/` (today every reply is addressed by
  `*net.UDPAddr`), `bdx/`, and a real `cluster/core` OTA requestor that
  initiates CASE to a provider through the ADR 0008 initiator generalised
  to one Invoke and one BDX transfer. The controller-role boundary is
  unchanged: the requestor talks to a provider the commissioner named, as
  matter.js's does.
- `secure/`, `commissioning/` and the session-parameter paths stay owner
  (Fable) work per `CLAUDE.md`; the briefs M, N, O leave them out.
- Certifiability: SU and BDX become declared families; DLOG stays (c).
- Scope statements updated: concept §0 sentence 1, F.1 (TCP row), F.4 (OTA
  row), `docs/matterjs-comparison.md` rows for TCP/BDX and the OTA
  requestor, `docs/feature-scope.md` (since replaced by `docs/architecture.md`).
