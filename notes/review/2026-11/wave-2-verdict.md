# W2 verdict (2026-11-01, HEAD 769ea3c2)

Theme files: `wave-2-api.md`, `wave-2-conc.md`, `wave-2-wire.md`, `wave-2-sec.md`.
Counts: high 1 (W2-sec-01 = W2-wire-01, one defect) · medium 9 · low 19 · note 21.

## Verified against the pins (Fable re-read, both sides)

| ID | Severity | Verdict |
| --- | --- | --- |
| W2-wire-01 | **high** | confirmed: `bridge/receive.go:268` routes IM on session 0 into the handlers; `resolveSessionFabric` (`subscribe.go:1511`) answers 0 and `resolveSessionPASE` (`:1581`) false for session 0, so the ACL term `hasACL && !pase && fabricIndex != 0` (`im/read.go:490`, write, invoke) is false and the request runs unauthenticated over cleartext UDP. `TestDispatch_IMReadRoutes` (`bridge/receive_test.go:311`) pins it. matter.js: InteractionServer `requiresSecureSession = true`; chip: empty SubjectDescriptor → ACL denies. Fix: reject IM on an unsecured session in `classifyIMOpcode` with StatusResponse(InvalidAction) and make the ACL terms fail closed. |
| W2-conc-01 | medium | confirmed: `bridge/subscribe.go:294-296, 461-463` drop drained dirty paths and events on a send error; matter.js re-queues for the first two failures (`ServerSubscription.ts:569-582`) |
| W2-conc-02 | medium | confirmed: `im/subscription/subscription.go:303-304` buffers full event payloads unbounded; matter.js keeps an event-number watermark (`ServerSubscription.ts:391-394, 541-542`) |
| W2-api-01 | medium | confirmed: `bridge.EmitFabricAdded` has no caller in the module; `node/node.go:606` feeds observers from the OpCreds hook directly; `docs/guides/ports.md:98,166` advertise the dead hook |
| W2-api-02 | medium | confirmed: `bridge.Config.{VendorID,ProductID,NodeLabel,Discriminator}` validated (`bridge.go:320-327`), never read; the advertisement is stamped from window params (`bridge.go:2200`) |
| W2-api-03 | medium | confirmed: `endpoint/doc.go:29` says 1..65534, `endpoint/types.go:63` says 2..65534; `IsAggregator` is `ID == 1` (`types.go:614`) |
| W2-sec-02 | medium | **confirmed, upgraded from plausible**: no caller of `operational.Manager.ClosePASESessions` exists in the module; the node facade's CommissioningComplete hook (`node/root.go:233-242`) and fail-safe-expiry hook (`:215-220`) never close PASE. matter.js `FailsafeContext.ts:153` `closePaseSession` on completeCommission. `notes/parity/matter_behaviour_findings.md:306` claims this FIXED via a daemon hook — that hook lived in the host daemon before the node facade took the wiring and was lost; the register entry is false and must be reopened. |
| W2-sec-01 | high | duplicate of W2-wire-01 (same defect, independent confirmation) |
| W2-api-04, W2-wire-02, W2-wire-03, W2-sec-03 | medium/low | re-read by the reviewer at both pins on 2026-11-01 before the fix PR (matter.js `InteractionMessenger.ts:415-419`, `TlvNumber.ts:151-155`, `FabricAccessControl.ts:189-191`, `bridge/bridge.go:229-240`; chip `InteractionModelEngine.cpp:1095-1099`, `AttributePathIB.cpp`); all four hold |

## Owner decisions 2026-11-01

- W2-wire-01: **no hotfix**; fixed in the bundle with the other findings.
- Fix order (superseding the earlier "one bundle at the end"): the security bundle first (PR #359), then one fix PR per wave; one tag only after the last wave PR.
- W2-api-01: remove the dead hook (`Bridge.SetOnFabricAdded` / `EmitFabricAdded`), with a migration line.
- W2-api-02: remove the unread fields (`bridge.Config.{VendorID,ProductID,NodeLabel,Discriminator}`, `endpoint.Config.NodeLabel`, `endpoint.Topology.NodeLabel`).
- W2-api-03: an additive Aggregator role field on `endpoint.Endpoint`.
- Scope of the W2 fix PR: the mediums plus the lows W2-wire-04, -05, -06, W2-sec-03, -04, -05, W2-api-05, -08 (brief `notes/briefs/review-2026-11-w2.md`); everything else stays open as recorded.

## Fixed in PR (review/w2)

| ID | Commit | Note |
| --- | --- | --- |
| W2-conc-02 | `7ccbaa4c` | watermark + drain-time read from the event log |
| W2-conc-03 | `7ccbaa4c` | closed by W2-conc-02 (log number order and timestamps; append and fan-out serialised) |
| W2-conc-01 | `acd09233` | failed report re-marks its paths and restores the event watermark |
| W2-wire-02 | `da3bbb91` | InvalidAction for a non-request opcode / unsolicited StatusResponse opening an exchange |
| W2-wire-03 | `6b0269c1` | `uintField` type/range checks in every path and header decoder |
| W2-wire-04 | `fbc720b5` | mandatory fields of Read, Subscribe, Timed, EventFilterIB, DataVersionFilterIB |
| W2-sec-03 | `696da663` | unresolvable fabric fails closed without an ACL lister; fabric 0 is no grant |
| W2-sec-04 | `341dff8c` | constant-time VerificationKey compare; ahead-of-matter.js row BD-Matter-ICDVerificationKeyConstantTime |
| W2-sec-05 | `07527d56` | eviction already held (Reload/ForgetFabric → forgetUnreferenced); pinned by a port of matter.js SessionManagerTest; the key stays the operational key as in matter.js MessagingState (not re-keyed) |
| W2-wire-05 | `e4da5159` | first EventFilterIB for this node applies |
| W2-wire-06 | `d53cfc23` | subtype PTR TTL 120; grandcat's PTR/SRV/TXT stay 3200 (no race-free setter), recorded in L7-D02 — **partly open** |
| W2-api-01 | `38b94b79` | hook removed |
| W2-api-02 | `0c622c13` | fields removed |
| W2-api-03 | `f118440c` | `Endpoint.Aggregator`, `Topology.Aggregator()` |
| W2-api-04 | `779010f5` | Snapshotter contract stated in code and docs |
| W2-api-05, W2-api-08 | `58644cd7` | port list completed; store citations fixed |
