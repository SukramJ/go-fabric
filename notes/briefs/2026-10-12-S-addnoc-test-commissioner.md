# Brief S — the shared AddNOC test commissioner (phase 5 preparation)

Owner: Fable. Implementer: Opus. Branch `wave7/commissioner` from
`planning/briefs-wave-7`, pushed, **no PR**. Report in
`notes/briefs/reports/S.md`. Read first: `notes/briefs/README.md`,
report Q (question 6), `bridge/bridgetest/bridgetest.go`.

## Why
`node/` can prove PASE, reads and subscriptions over the wire
(`node/client_test.go:6-9`, `TestNodeServesPASEReadAndSubscription`) but
nothing below AddNOC: the ACL, CASE, fabric and subscription-resumption
ports of the facade are exercised only by the chip-tool harness. A Go
commissioner that completes commissioning makes them provable in
`go test` and lets the `node/` floor rise.

## Sources (this repo; matter.js has the controller role, this module does not)
- `node/client_test.go` — the wire-level PASE commissioner (PBKDFParamRequest,
  Pake1-3, sealed IM requests). Start from it; move it, not copy it.
- `bridge/secure_harness_test.go:1-12` — the in-process CASE-equivalent
  harness (installed fabric, session resolving to the controller). The
  new commissioner replaces its "pre-installed fabric" shortcut with the
  real commands over the wire.
- The device side of commissioning, in order: GeneralCommissioning
  ArmFailSafe / SetRegulatoryConfig / CommissioningComplete
  (`cluster/core/general_commissioning*.go`), OperationalCredentials
  AttestationRequest, CertificateChainRequest, CSRRequest, AddTrustedRootCertificate,
  AddNOC (`cluster/core/operational_credentials.go`; the request structs
  there are what to encode), the fail-safe hooks `node/root.go` wires, the
  CASE responder `secure/sigma` (Sigma1-3; the initiator half needed here
  is the mirror of the responder, read `secure/sigma` and
  `bridge/case_handshake_test.go`, which already drives Sigma1 from the
  test side). The test CA: `secure/attestation` (`BuildTestChain`) and
  whatever the existing tests use to mint a NOC (`bridge/secure_harness_test.go`,
  `cluster/core/operational_credentials_test.go`).
- chip-tool's pairing stages, as the behaviour of the tool the harness
  runs (`git -C ../connectedhomeip show 6170af84…:src/controller/CHIPDeviceController.cpp`,
  the `CommissioningStage` sequence) — read to order the steps, cite the
  lines; the Go commissioner does the subset a Matter commissioner must
  (no network commissioning: Ethernet).

## Must
1. `bridge/bridgetest.Commissioner` (public test support, README §API
   stability applies): `Dial(t, addr, Options{Passcode, Discriminator?,
   FabricID, NodeID, CAT?})`, `Commission(ctx) (*Session, error)` = PASE →
   ArmFailSafe → attestation + CSR → AddTrustedRoot → AddNOC →
   CommissioningComplete, then `CASE(ctx)` against the operational
   advertisement (resolve by node id through the `mdns.Advertiser` the
   test passes, or dial the known address). The CASE session gives
   `Read / Write / Invoke / Subscribe` helpers sealed with its keys, and a
   `Close`. Keys and certificates from a test CA in the package (not the
   CSA test chain's PAA); everything deterministic under a seed.
2. Tests in `node/` that use it (new, assertions over the wire):
   AddNOC commissions a fresh node and `Fabrics()` lists it; a CASE read of
   AccessControl ACL shows the administrator entry AddNOC installed; a
   write without privilege is UNSUPPORTED_ACCESS; a subscription survives
   `Stop` + `New` + `Start` on the same database (ADR 0008,
   `ReestablishFormerSubscriptions`); RemoveFabric drops the session.
   Each is the facade's observable behaviour, not reflection.
3. `node/` floor raised to what the new tests give; `make race` on
   `node/`, `bridge/bridgetest`; `make lint`; reachability per commit.
4. CHANGELOG → Added (test support is public API).

Not in scope: `secure/`, `commissioning/`, `cluster/core` changes (read
only; a bug found there is a report item, not a fix); the daemon; the
chip-tool tables.

## Guards
`go test ./node/... ./bridge/...`, `make race` (node, bridgetest),
`make cover-check`, `make lint`, `make reachability-check`. Report: what
was built, which guard proves it, what was left out, every open question.
