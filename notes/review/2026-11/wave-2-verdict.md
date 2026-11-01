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
| W2-api-04, W2-wire-02, W2-wire-03, W2-sec-03 | medium/low | reviewer-confirmed, not re-read yet (re-read before the fix wave) |

## Owner decisions 2026-11-01

- W2-wire-01: **no hotfix**; fixed in the bundle with the other findings.
- All W1/W2 fixes deferred: review W3–W6 first, one fix bundle at the end.
