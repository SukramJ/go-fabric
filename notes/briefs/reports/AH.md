# Report AH — `controller/ca`, `controller/attestation`, `controller/interaction`, `controller/commissioner`: the commissioner (ADR 0020, stage 2)

Branch `wave14/commissioner` from `planning/briefs-wave-14` (2d50846), pushed,
no PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`;
the checkout stands at `706a8463`); every `// Mirrors matter.js …` comment
added cites `0ceb202d` lines that were read. Each commit carries its
regenerated reachability inventory (unreached stays 5) and a CHANGELOG entry
under `[Unreleased]` → Added. `secure/`, `transport/`, `mdns/`, `bridge/`,
`node/`, `im/`, `cluster/`, `notes/parity/by_design.md` and the findings
register are untouched.

| Commit | Item |
| --- | --- |
| ca27d2e `feat(controller/handshake): CASEOptions.Exchange and OnExchange replace exchange.WithHooks` | §0 |
| 2173caa `feat(controller/ca): the certificate authority` | §1 |
| 3b187a8 `feat(controller/attestation): device attestation with a static PAA store` | §2 |
| e63792f `feat(controller/interaction): Invoke and Read for the commissioning flow` | §3 |
| 7f67394 `feat(controller/commissioner): the commissioner, the PASE connection and the commissioning flow` | §4a, §4b, §4c, the node proof |
| f73ddc5 `test(internal/chiptool): the Go commissioner commissions the reference daemon` | the reference-bridge proof |
| (this commit) `notes: report AH` | — |

Coverage floors (new): `controller/ca` 88 (measured 88.0 %),
`controller/attestation` 89 (90.3 %), `controller/interaction` 87 (88.3 %),
`controller/commissioner` 85 (86.3 %). The touched packages hold theirs
(`controller/exchange` 93, `controller/handshake` 86, `controller/peer` 93).

## Type map

| matter.js `0ceb202d` (`packages/protocol/src/` unless named) | Go |
| --- | --- |
| `certificate/CertificateAuthority.ts` `CertificateAuthority` | `ca.CA`, `ca.New`, `ca.Config` |
| `CertificateAuthority.Configuration` (:446-484), `#buildStorageData` | `ca.Stored`, `ca.StoredICAC`, `ca.KeyPair`, `ca.Store`, `ca.MemoryStore` |
| `generateNoc`, `rootCert`, `icacCert` | `CA.GenerateNOC`, `RootCert`, `ICACCert` (+ `RootPublicKey`, `Verifier`) |
| `ImplementationError` of `#loadFromStorage` | `ca.ErrConfig` |
| `types/src/datatype/GlobalFabricId.ts` `GlobalFabricId.compute` | `ca.CompressedFabricID` |
| `computeHash(publicKey, "SHA-1")` | `ca.KeyIdentifier` |
| `kinds/definitions/asn.ts` `jsToMatterDate` | unexported `matterDate` |
| `certificate/DeviceAttestationValidator.ts` `validate` | `attestation.Validate` |
| `DeviceAttestationValidator.Context` / `DeviceAttestationData` / `ValidationResult` | `attestation.Context`, `Data`, `Result` |
| `DeviceAttestationCheck`, `AttestationFinding`, `DeviceAttestationError` | `attestation.Check`, `Finding` (+ `Level`), `*attestation.Error` |
| `DclCertificateService` (the parts the validator uses) | `attestation.PAAStore`, `PAA`, `Revocations`, `StaticStore`, `TestStore()` |
| `OnAttestationFailure`, `#resolveAttestationFindings` (CF:1341-1368) | `attestation.Policy` (`Accept`, `Reject`, `Decide`), `Policy.Resolve` |
| `kinds/CertificationDeclaration.ts` `CertificationDeclaration.parse`, `definitions/certification-declaration.ts` `TlvDc`, `CertificationType` | unexported `parseDeclaration`, `decodeCDContent`; `attestation.CertificationType` |
| `common/OperationalCredentialsTypes.ts` `TlvAttestation`, `TlvCertSigningRequest` | unexported `decodeAttestationElements`; `commissioner` `decodeCertSigningRequest` |
| `action/client/ClientInteraction.ts` `ClientInteraction` (`read`, `#invokeSingle`, `maximumPeerResponseTime`) | `interaction.Interaction` (`New`, `Read`, `Invoke`, `MaximumPeerResponseTime`) |
| `interaction/InteractionMessenger.ts` `InteractionClientMessenger` | unexported `messenger` |
| `StatusResponseError` / `ReceivedStatusResponseError` | `*interaction.StatusError` |
| `peer/ControllerCommissioner.ts` `ControllerCommissioner` | `commissioner.Commissioner`, `New`, `Commission`, `EstablishPASE` |
| `ControllerCommissionerContext` | `commissioner.Context` |
| `LocatedNodeCommissioningOptions` / `CommissioningOptions` | `commissioner.Options` |
| `EstablishPaseOptions` | `commissioner.PASEOptions` |
| `CommissionResult` | `commissioner.Result` |
| `peer/CommissioningConnection.ts` `CommissioningConnection` | unexported `connectPASE` |
| `CommissionableDevice` (as `#addressesToCandidates` builds it) | unexported `candidate` |
| `peer/ControllerCommissioningFlow.ts` `ControllerCommissioningFlow` | unexported `flow` |
| `fabric/Fabric.ts` `Fabric` (the fields the flow uses), `addressOf` | `commissioner.Fabric` (embeds `handshake.Fabric`), `NewFabric`, `FabricConfig`, `AddressOf`, `Lookup` |
| `CommissioningError` and CF:223-254 subclasses | `*commissioner.CommissioningError` with `Kind` `ErrCommissioning`, `ErrMaximumCommissionedFabricsReached`, `ErrCommissioningTimeout`, `ErrDeviceAlreadyCommissionedToThisFabric`, `ErrFabricLabelConflict`, `ErrNodeIDConflict`, `ErrOperativeConnectionFailed` (`RecoverableCommissioningError` unexported, as upstream) |
| `peer/CommissioningError.ts` `PairRetransmissionLimitReachedError` | `*commissioner.PairRetransmissionLimitReachedError` |
| `peer/PeerCommunicationError.ts` `PeerCommunicationError`, `CommissioningTransitionError` | `*commissioner.PeerCommunicationError`, `*commissioner.CommissioningTransitionError` |
| `MessageExchange.Options` `onSend`/`onReceive` for `CaseClient.pair` (PeerConnection.ts:829-860) | `handshake.CASEOptions.Exchange`, `OnExchange` |

## Guards

- `make ci`: exit 0 — build, vet, `go test -count=1 ./...` (179 `ok`
  lines, no failure), gofumpt + golangci-lint (0 issues), `go mod tidy
  -diff`, `go mod verify`, cover-check (181 packages at or above their
  floor; the four new ones as listed above).
- `make race`: exit 0 on the second run (179 packages `ok`, no race
  reported). The first run failed in `bridge`
  `TestOngoingReportChunkPiggybacksTheStatusResponseAck` ("report went out
  in 0 chunk(s); the test needs >=2"), a timing test in an untouched
  package; it passed 5 of 5 runs alone under `-race`, and the rerun of
  `make race` was green.
- `make reachability-check`: exit 0 — 10 854 exported, 7 614 reached,
  3 235 whitelisted, unreached stays **5**, none in `controller/`.
- `make chiptool-test` (once): exit 0, `ok internal/chiptool 51.3s`, 312
  PASS, 0 FAIL, 1 SKIP. `TestChipToolCommissionsReferenceBridge`,
  `TestChipToolSuite` and the new `TestGoControllerCommissionsReferenceBridge`
  (3.2 s: found over multicast, commissioned, VendorID read over CASE)
  passed; `TestMatterJSController` skipped for its environment reason (no
  built matter.js at `.claude/worktrees/matter.js`).
- In-package proofs: `controller/commissioner` `TestCommissionTheNode`
  (injected DNS-SD records, runs under `-race`: wrong passcode, full
  commissioning — fabric table, label, failsafe closed, CASE session the
  peer's, a Read over CASE — then a second commissioning refused with
  FabricConflict and the failsafe disarmed, then a node id conflict),
  `TestFinalizeCommissioning`, `TestRejectedAttestation`,
  `TestAcceptedFailedAttestation`, `TestEstablishPASE`, and
  `TestCommissionLiveThroughMulticast` (`!race`; passed here, not skipped).
  The connection (stagger, holding, passcode drop, budget, winner, error
  classes) and the flow loop (re-arm below half, cumulative timeout,
  disarm on failure, recoverable continues, Stop, the 320 s CASE re-arm) on
  fake clocks.
- Parity: `controller/{ca,attestation,interaction,commissioner}/parity_matterjs_test.go`
  — CA ids and validity, the certificate extensions; the DAV check names;
  CI:159-160; CF:289-301, CC:123/:264/:603-604/:706, CN:35, the step table
  and order, the error classification, the NOCResponse and
  GeneralCommissioning result messages, the AddNoc fields, the operational
  node id range.

## Lines that differed from the brief (followed the file)

- **§0, a way to see the exchange.** `CASEOptions.Exchange` alone cannot hand
  the connection process the exchange its kick reads (AG asked for "plus a
  way to see the exchange"). `CASEOptions.OnExchange` is called with it right
  after `Initiate`, before Sigma1 — matter.js's process creates the exchange
  itself (PeerConnection.ts:829-860).
- **§0, `Scanner.Forget`.** Confirmed: only `common/BleScanner.ts:305`
  implements `forgetCommissionedDevice`; `CommissionableMdnsScanner` does not.
  `commissioner.Context` therefore has no `Scanner` field.
- **§1, the NOC subject order.** matter.js encodes the DN lists in the
  caller's property order (`TlvTaggedListPreservingOrder`,
  `kinds/definitions/operational.ts:109-110`, `TlvObject.ts:156-175`), and
  `generateNoc` writes `{ fabricId, nodeId, caseAuthenticatedTags }`
  (CA:306). The NOC subject is fabric id, node id, CATs — `bridgetest/ca.go`
  writes node id first. The extension order is the same in both.
- **§1, `IntermediateCA`.** matter.js requires the ICAC only when
  `generateIntermediateCert` is set and otherwise follows the store (CA:159);
  Go's `bool` has no undefined, so a stored CA must always agree with
  `Config.IntermediateCA`. `Verifier()` returns the verifier built once at
  `New`, on `Config.Now`.
- **§2, no `Context.Now`.** The validator never compares with the current
  time, so the field would be dead; it is left out.
- **§2, parsing.** `Dac/Pai/Paa.fromAsn1` include more than an X.509 parse:
  the 600-byte limit (`kinds/common.ts:13`, `Certificate.ts:617-622`), the
  required extensions (`Certificate.ts:397-401`, `:549-551`; the PAA needs no
  AKID) and, for the DAC, an EC P-256 key (`AttestationCertificates.ts:66-72`)
  — all `CertificateUnparseable`. The PAA key is read inside the chain
  check's `try` (DAV:272-279), so a PAA key the validator cannot use is
  `CertificateChainInvalid`, not a plain error. TLV decodes validate their
  bounds (`types/src/schema/Schema.ts:18-21`): a nonce other than 32 bytes is
  `AttestationElementsUnparseable`.
- **§2, the CN fallback** is used only when the DN carries neither OID
  (`Certificate.ts:379-392`); a prefix without four uppercase hex digits is a
  parse error (`:249-258`).
- **§2, `Policy.Resolve`** lives in `attestation` (the flow calls it). A
  `Decide` with an empty reason rejects with the underlying error; matter.js's
  empty *string* would reject with `CommissioningError("")`.
- **§3, the API.** `exchange.Session` does not expose its manager and §0
  forbids adding to `exchange`, so the client is a value bound to
  (manager, session): `interaction.New(mgr, s, log)`, then `Invoke(ctx, path,
  fields, opts)` / `Read(ctx, paths...)` — as matter.js's `ClientInteraction`
  is bound to its exchange provider. The flow swaps the PASE interaction for
  the CASE one (CF:2043-2044).
- **§3, `Read` is fabric-filtered.** `Read()` defaults `fabricFilter` to true
  (`action/request/Read.ts:40`); the brief's "FabricFiltered false" is the
  probe's explicit option (`Read({fabricFilter: false})`). The request carries
  no empty AttributeRequests array and the IM revision (`TlvReadRequest.ts:19-35`).
- **§3, `InvokeOptions.Timed bool` + `TimedTimeout`** (matter.js
  `timedRequest` + `timeout`, CI:438-440), not one duration; `Result.Fields`
  is a `spec.Node` (the generated `DecodeTLV` reads it), `ClusterStatus` a
  `*uint8`.
- **§4c, two more steps.** CF:563-568 adds 8.2
  `TimeSynchronization.SynchronizeTime` and CF:588-593 15.1 `AccessControl`;
  both return Skipped (CF:1199-1211, :1515-1522). They are in the step table.
- **§4c, 0.1's second read** is the wildcard read of CF:773-784 —
  NetworkCommissioning FeatureMap *and* Networks, and the OTA requestor's
  DefaultOtaProviders, on every endpoint — not concrete paths per parts-list
  endpoint; only the FeatureMaps are used (the others feed BLE and OTA steps).
- **§4c, the breadcrumb.** SetRegulatoryConfig sends the current breadcrumb
  and increments after (`this.lastBreadcrumb++`, CF:1141); the "XX" retry
  sends the incremented value (:1166).
- **§4c, AddNOC always carries `IcacValue`**, empty without an ICAC
  (`this.ca.icacCert ?? new Uint8Array(0)`, CF:1448); `bridgetest` omits it.
- **§4c, the CSR.** `Certificate.getPublicKeyFromCsr`
  (`kinds/Certificate.ts:782-843`) also checks version 0, an EC P-256 key,
  ecdsa-with-SHA256 and the CSR's own signature. Mirrored.
- **§4c, error classes.** `StatusResponseError.accept(error)` (CF:459,
  `general/src/MatterError.ts:87-92`) rethrows anything that is not a status
  error as is; only a top-level status error is repacked as a
  `CommissioningError` (`util/Error.ts:88-104`). The visible result is the
  brief's.
- **§4b, a wrong passcode** ends `Commission` with
  `handshake.ErrPasscodeMismatch`: `PasscodeMismatchError extends
  UnexpectedDataError` (`session/pase/PaseClient.ts:41`) is CN's
  `lastNonRetryableError` (CN:167-170, :223-225). Only a
  `PairRetransmissionLimitReachedError` becomes `PeerCommunicationError`
  (CC:402-407). The brief expected `PeerCommunicationError`.
- **§4b, closing the PASE session** is `initiateForceClose` (CC:313-319): the
  session is given up without a CloseSession on the wire —
  `exchange.Session.HandlePeerLoss`.
- **§4b, the peer.** The transition creates the peer with
  `Set.AddKnownPeer(Descriptor{discovery data, reported parameters})`; the
  peer is new (the address was checked), so the merge equals matter.js's
  assignment (CC:692-696).
- **§4a, retryable** includes `exchange.ErrRetransmissionLimit`:
  `RetransmissionLimitReachedError extends NoResponseTimeoutError`
  (`protocol/errors.ts:17`). A PASE StatusReport is `exchange.ErrTransient`,
  which is where matter.js's `ChannelStatusResponseError →
  NoResponseTimeoutError` (CC:481-485) lands it too.
- Line numbers otherwise as the brief states. `FabricConfig.IPK` nil draws a
  random key; `FabricIndex` 0 is 1 (Go surface).

## Left out, and why

- BLE, Wi-Fi and Thread network commissioning and their re-arm timer, the
  pessimistic failsafe, time synchronisation (a stub upstream too), T&C, the
  default OTA provider, the DCL, Joint Fabric — the brief's scope.
- `reArmFailsafe` on a step and the re-arm before it (CF:385-392): only the
  BLE network steps set it.
- `commissioningFlowImpl` (a subclass hook) and the retries of a failed flow
  (a TODO upstream, CF:443-451).
- `interaction` grows nothing beyond the flow: no Write, Subscribe, batching,
  chunked requests (stage 3).

## `secure/`, `im/`, `cluster/` gaps found

- `im/`: no client encoder for ReadRequest as matter.js sends it (the server
  type writes an empty AttributeRequests array and no revision), none for
  TimedRequest, no client ReportData decoder. `interaction` carries all three
  until stage 3 (the open item `docs/open-items.md` names).
- `cluster/spec`: OperationalCredentials has no generated definition (ADR 0013
  skips it); `commissioner/wire.go` writes its ids and payloads from
  `parity/schema.json`. `secure/mattercert` has no `KeyUsageDigitalSignature`
  constant (the CA uses the bit of `base.ts:11`).
- Observation, not a gap: while a PASE session of its window lives, the device
  answers a new PBKDFParamRequest with an MRP ack only (`bridge`
  `matter.rx.sc.pase_busy`) — matter.js's `PaseServer.onNewExchange` ignores
  it the same way (`session/pase/PaseServer.ts:82`). Since the commissioner
  closes a PASE session without CloseSession (CC:313-319), a second PASE to
  the same node right after one waits out its response timeout; the tests use
  a fresh node for that case.
- Nothing missing in `secure/`.

## What brief AI and stage 3 need from here

- **AI (bridgetest onto `controller/`):** `bridgetest/ca.go` → `controller/ca`
  (mind the NOC subject order above, which changes the minted bytes), the
  commissioning → `commissioner.Commission` (its fixed-seed determinism and
  the injected-address dialling have no counterpart: `ca.Config.Rand` and
  `commissioner.Context.Rand` take a seeded reader, and the node's
  operational record must be resolvable for the CASE step —
  `bridgetest.Options.Advertiser` would become records injected into a
  `discovery.Names`), its Session → `interaction.Interaction` plus Write and
  Subscribe (stage 3). `bridgetest.Commissioner.ControllerIdentity` /
  `RootPublicKey` map to `commissioner.Fabric.Identity` / `RootPublicKey`.
- **Stage 3:** the client encoders into `im/`; Write, Subscribe, batching and
  chunked requests on `interaction`; a persistent `peer.Store`,
  `peer.ResumptionStore` and `ca.Store` (ADR 0020's open persistence item).

## Open questions

1. Should the PASE session's close send CloseSession after all? matter.js
   does not (CC:313-319), and the device then ignores a new PASE until it
   drops the session itself (above).
2. `Policy.Decide`'s empty reason means "reject with the underlying error";
   is that Go surface fine, or should the reason be a `*string`?
3. Should `commissioner.Context` keep a `Scanner` for a future
   `forgetCommissionedDevice` (BLE only upstream)?
4. Is `FabricConfig.IPK` nil → random the right default (matter.js's fabric
   builder draws one too), or should it be required?

## `by_design` candidates (not written, per the rules)

- **The persisted certificate counter:** `ca.CA` saves after every issued NOC;
  matter.js writes its storage only at creation (CA:181-183), so its serials
  repeat after a restart.
- **The revocation default:** a `PAAStore` without `Revocations` reports
  nothing revoked (matter.js's DCL service always answers `isRevoked`).
- **The attestation policy default:** the zero `Policy` accepts every finding
  with a warning — matter.js's backward-compatible `undefined`
  (CF:149, :1355), marked there for removal; the one to turn off later.
- **The NOC subject order** differs from `bridgetest`'s (not from matter.js) —
  only relevant if `BD-Matter-Commissioner-TestCA` describes the bytes.
