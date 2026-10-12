# Report S — the shared AddNOC test commissioner

Branch `wave7/commissioner` on `planning/briefs-wave-7` (8cbfcfa). No PR.
`make reachability` after every commit that changed Go code, inventory in the
same commit (unreached stays 5).

| Commit | Item |
| --- | --- |
| d397636 | `bridge/bridgetest.Commissioner`: test CA, PASE / CASE initiator, CASE responder, IM client, its own tests |
| 511e6e6 | `node/`: five over-the-wire tests after AddNOC, the PASE test moved onto the commissioner, `node/client_test.go` deleted, floor 59 → 72 |
| ef71435 | CHANGELOG → Added |

Sources read: the files the brief names; chip at the harness pin
`6170af8461b10b1766044122ac83332c6d00ab20` (`src/controller/AutoCommissioner.cpp`,
`CHIPDeviceController.cpp`, `CommissioningDelegate.h`,
`examples/chip-tool/commands/common/CHIPCommand.h`); matter.js `4633bfd7`
`packages/protocol/src/certificate/CertificateAuthority.ts` for the shape of
the certificates the test CA mints.

## What was built

### `bridge/bridgetest` (public test support)

- `Dial(tb, addr, Options) *Commissioner`. `Options{Passcode, Discriminator,
  FabricID, NodeID, ControllerNodeID, CAT, AdminSubject, AdminVendorID,
  FailSafeSeconds, Seed, Advertiser}`. Passcode, FabricID and NodeID are
  required (a missing one fails the test). Defaults:
  - `ControllerNodeID` 112233 = chip-tool's kTestControllerNodeId
    (CHIPCommand.h:82).
  - `AdminSubject` = the controller node id.
  - `AdminVendorID` 0xFFF1.
  - `FailSafeSeconds` 60 = kDefaultFailsafeTimeout (CommissioningDelegate.h:208).
- `Commission(ctx) (*Session, error)` runs the stages in AutoCommissioner
  order (AutoCommissioner.cpp:472-545, :672-675):
  1. PASE.
  2. ArmFailSafe, breadcrumb = stage ordinal 3 (CHIPDeviceController.cpp:1708, :3744).
  3. A read of LocationCapability, then SetRegulatoryConfig (country "XX",
     breadcrumb 4).
  4. CertificateChainRequest for the PAI, then for the DAC.
  5. AttestationRequest. The signature is checked against the DAC over
     elements ‖ PASE attestation challenge.
  6. CSRRequest. Signature, nonce echo and CSR self-signature are checked.
  7. NOC minted over the CSR key, no ICAC.
  8. AddTrustedRootCertificate.
  9. AddNOC.
  10. CASE.
  11. CommissioningComplete **over CASE**.

  It returns that CASE session.
- `PASE(ctx)`, `CASE(ctx)`, and `CASEAs(ctx, nodeID, cats...)`, which opens a
  session as another controller of the same fabric with a NOC the CA mints.
- `Accept(ctx)`: the next CASE session the device opened to the commissioner.
  The commissioner answers Sigma1 / Sigma3 as the responder.
- `ResolveOperational`: implements `bridge.OperationalResolver`.
- `RemoveFabric(ctx, s)`, `FabricIndex()`, `CompressedFabricID()`, `Addr()`,
  `Close()`.
- `Session`:
  - `Read`, `Write`, `Invoke`, `Subscribe` (→ `Subscription{ID, MaxInterval,
    Priming}`), `NextReport` (the reports the device initiates; they are
    answered and chunks merged), `Close` (sends CloseSession), `PeerNodeID`,
    `LocalNodeID`.
  - Decoded results: `Report{SubscriptionID, Attributes, Statuses}` with
    `Value` / `Status`, `InvokeResult`, and `TLV` with `Child` / `DecodeTLV`.
  - Errors: `StatusError` (the interaction ended with a StatusResponse),
    `ErrSessionClosed`, `ErrHandshake`.
- Test CA (`ca.go`): the RCAC and NOCs as Matter TLV, in the shape of matter.js
  `CertificateAuthority.ts:225-249` (`#generateRootCert`) and `:287-320`
  (`generateNoc`). Details:
  - rcacId 0 (`:52`).
  - SHA-1 key identifiers (`:170`, `:314`).
  - EKU [2, 1].
  - Signed over `mattercert.TBSToDER` with deterministic ECDSA (RFC 6979).
  - Keys, IPK and nonces derive from `Options.Seed` (HKDF).
  - The compressed fabric id is derived as the device does
    (operational_credentials.go:1507-1535).
- Transport: one socket, one reader goroutine. Every reliable message is
  acknowledged at once with a standalone ack. The commissioner never
  retransmits; over loopback nothing is lost.
- `Options.Advertiser`: PASE takes the port of the commissionable record with
  TXT `D` = Discriminator from `Advertiser.Active()`. CASE takes the port of
  the operational record `mdns.BuildOperationalService(...).InstanceName`.

### `node/`

- `node/commission_test.go` (new), all over the wire on CASE:
  - `TestAddNOCCommissionsFreshNode`: Fabrics() lists fabric id, node id,
    index and the compressed id the commissioner derived. The window closes
    after CommissioningComplete. CASE answers as the assigned node id. PASE
    and CASE find the node through its `mdns.Noop` records.
  - `TestCASEReadsAdministratorACL`: one entry, Administer, CASE, subjects
    [112233], targets null, the fabric index.
  - `TestWriteWithoutPrivilegeIsUnsupportedAccess`: the administrator writes
    the ACL (itself Administer, node 0x2222 View). The viewer's read succeeds,
    its NodeLabel write is UNSUPPORTED_ACCESS, and the administrator's write
    is SUCCESS.
  - `TestSubscriptionSurvivesRestart`:
    1. Subscribe to Aggregator PartsList, then Stop.
    2. `node.New` on the same database, the commissioner attached as the
       operational resolver, Start.
    3. The node opens CASE to the commissioner, and the priming report
       carries the old SubscriptionID.
    4. A later Aggregator Add is reported on the same subscription id.
  - `TestRemoveFabricDropsSession`: NOCResponse OK, Fabrics() empty, a read
    on the session gets no answer within 1.5 s, and the node reopens its
    window.
- Floor `node` 59 → 72 (measured 72.7–72.8 %). The `why` is rewritten.

## Which guard proves it

| Must | Guard |
| --- | --- |
| 1 Commissioner API, CA, determinism | `bridge/bridgetest` tests: `TestCommissionerSessions`, `TestCommissionerRefusals`, `TestCommissionerAcceptsReestablishment`, `TestDialRefusesIncompleteOptions`, `TestDecodeTLV`; package floor 90 kept (measured 90.9 %) |
| 2 AddNOC → Fabrics() | `TestAddNOCCommissionsFreshNode` |
| 2 ACL entry over CASE | `TestCASEReadsAdministratorACL` |
| 2 UNSUPPORTED_ACCESS | `TestWriteWithoutPrivilegeIsUnsupportedAccess` |
| 2 subscription across Stop + New + Start | `TestSubscriptionSurvivesRestart` |
| 2 RemoveFabric drops the session | `TestRemoveFabricDropsSession` |
| 3 floor | `make cover-check` |

Mutation checks:
- `n.br.AttachACLLister` removed: all five node tests fail at
  CommissioningComplete with UNSUPPORTED_ACCESS. Report Q's open gap is now
  detected.
- `br.AttachCaseInitiatorProvider` removed:
  `TestSubscriptionSurvivesRestart` fails ("the restarted node opened no CASE
  session").

## Guards run (Linux, local)

- `go test ./node/... ./bridge/...`: green.
- `go test -race` on `./node/` and `./bridge/bridgetest/`: green at
  `-count=3`, `-count=5`, and `-count=10` on the network tests.
- `make lint`: 0 issues.
- `make cover-check`: green, 173 packages.
- `make reachability` / `make reachability-check`: clean, unreached 5.
- Not run: the module-wide `make race` (brief scope: node, bridgetest) and the
  chip-tool jobs (nothing they drive changed).

## Left out, and why

- **CASE resumption.** The commissioner offers no resumption record as
  initiator and holds no store as responder, so a resuming Sigma1 from the
  node gets the full handshake. `node/security.go` `resumptionLookup.GetByID`
  stays at 0 %.
- **Attestation verification beyond signatures.** No PAA chain, CD,
  revocation or JCM check (chip kAttestationVerification,
  kAttestationRevocationCheck).
- **ICAC.** The NOC is signed by the root directly.
- **MRP retransmission by the commissioner.** Not built. Over loopback it is
  not needed; a lost datagram shows up as a test timeout.
- **Real DNS-SD.** The advertiser route reads the records the device
  published to an `mdns.Noop`. The node itself resolves controllers with the
  real `mdns.NewOperationalResolver` (node/security.go:333), so the restart
  test attaches the commissioner through `Bridge().AttachOperationalResolver`
  before Start.
- **Not built:** events in reports, TimedRequest, network commissioning, time
  sync, T&C acknowledgements and ICD stages.
- `kReadCommissioningInfo` is reduced to the one attribute the regulatory
  stage needs.
- **UpdateNOC, the enhanced commissioning window and the OTA applier** in
  `node/` still have no package test (named in the floor's `why`).

## Open questions for the owner

1. **Order of CommissioningComplete and CASE.** The brief orders "…AddNOC →
   CommissioningComplete, then CASE(ctx)". The device refuses
   CommissioningComplete over PASE: InvalidAuthentication,
   cluster/core/general_commissioning.go:737-744. chip sends it over CASE:
   CHIPDeviceController.cpp:4574, AutoCommissioner.cpp:672-675. So
   `Commission` opens CASE first and returns that session, and `CASE(ctx)`
   opens further sessions. Confirm.
2. **`Options.NodeID`.** I read it as the node id assigned to the device
   (chip-tool's pairing argument) and added `ControllerNodeID` for the
   commissioner's own. Confirm the naming.
3. **Determinism.** Keys, certificates and nonces follow the seed. The
   unsecured session's ephemeral node id and message counter are random. With
   seeded values, two commissioners in one test collided: the device dropped
   the second one's Sigma1 as a duplicate (`matter.rx.duplicate_acked`).
   Confirm that this exception is acceptable.
4. **Certificate validity.** matter.js stamps now − 1 year / now + 10 years
   (CertificateAuthority.ts:233-234, :304-305). The test CA uses a fixed
   NotBefore (2026-01-01) and NotAfter 0, so the bytes depend on the seed
   alone. Accept, or prefer matter.js's window?
5. **A facade port for the operational resolver.** The node hard-wires
   `mdns.NewOperationalResolver`. Should `node.Config` take an
   `OperationalResolver`, so a test (or a host) need not reach through
   `Bridge()`?
6. **Regulatory location for an IndoorOutdoor device.** The commissioner
   sends Outdoor, chip's last fallback. chip's commissioner override and
   default location (CHIPDeviceController.cpp:3892-3910) are not modelled.
7. **Persisting a subscription races Stop.** The bridge records a
   subscription after it has sent the SubscribeResponse. A Stop right after
   `Subscribe` returns can lose it, and re-establishment then finds nothing.
   This was seen as a `-race` flake. Both restart tests now wait on
   `n.Store().LoadServerSubscriptions` before Stop. I have not checked what
   matter.js does in the same window.
8. **RemoveFabric closes the fabric's sessions silently.**
   `operational.Manager.CloseFabric` (secure/operational/manager.go:940-952)
   sends no CloseSession StatusReport, so the test can only assert "no
   answer". I have not checked whether matter.js notifies the peer here.
9. **SetRegulatoryConfig CountryCode.** The decoder reads `el.Octets`
   (bridge/fields_reader.go:463-476), so the UTF-8 "XX" a commissioner sends
   arrives as "". This is a known, commented revert. It is not touched; noted
   because the commissioner now sends exactly what chip-tool sends.

## Test edits, with reasons

- `node/node_test.go` `TestNodeServesPASEReadAndSubscription`: rewritten
  onto `bridgetest.Dial(...).PASE`, `Session.Read / Subscribe / NextReport`.
  The assertions are unchanged; `uintList` now takes `bridgetest.TLV`. The
  reason is the brief: move the PASE client, do not copy it.
- `node/client_test.go`: deleted. It moved into `bridge/bridgetest`
  (`handshake.go` PASE, `session.go`, `report.go`).
- No existing assertion was weakened.

## by_design / findings candidates for the owner

- by_design: **the test CA's fixed validity window** (question 4), as a
  divergence from CertificateAuthority.ts in the test support.
- by_design: **the commissioner does not retransmit**, and **the random
  ephemeral id / counter** (question 3).
- findings candidate: **a subscription is persisted after the
  SubscribeResponse**, so a Stop can lose it (question 7). This needs a
  matter.js read before it is a finding.
- findings candidate: **RemoveFabric drops sessions without CloseSession**
  (question 8). This also needs a matter.js read.
- findings (known): the **SetRegulatoryConfig CountryCode** decode
  (question 9).
- coverage gap, not a finding: **CASE resumption as responder** has no Go
  test yet. The commissioner could offer resumption in a follow-up.
