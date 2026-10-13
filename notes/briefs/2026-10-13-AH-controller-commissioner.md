# Brief AH — `controller/ca`, `controller/attestation`, `controller/interaction`, `controller/commissioner`: the commissioner (ADR 0020, stage 2)

Owner: Fable. Implementer: Opus (one agent; nothing else runs on the repo
while you do). Branch `wave14/commissioner` from `planning/briefs-wave-14`,
pushed, **no PR**. Report in `notes/briefs/reports/AH.md`. Read first:
`notes/briefs/README.md`, [ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md),
concept v2 §B.1, the reports AE–AG (the owner's decisions at their end are
rules), and the packages this one composes: `controller/discovery`
(`Scanner`, `Device`), `controller/exchange`, `controller/handshake`
(`PASE`, `PASEOptions`, `CASE`, `Fabric`), `controller/peer` (`Set`,
`Peer.Connect`, `Descriptor`, `Store`). The working reference on the Go
side is `bridge/bridgetest`: `commissioner.go:268-450` runs the flow by
hand today (ArmFailSafe → SetRegulatoryConfig → attestation → CSR →
AddTrustedRootCertificate → AddNOC → CASE → CommissioningComplete),
`ca.go` mints RCAC and NOC as Matter TLV, `session.go:184-370` encodes a
Read and an Invoke and decodes their replies. Read them; this brief turns
them into production packages. `bridgetest` itself stays untouched (its
move onto `controller/` is the next brief, AI). matter.js at
**`0ceb202d`**; the `../matter.js` checkout may stand later — read with
`git -C ../matter.js show 0ceb202d:<path> | cat -n`. Paths below are
relative to `packages/protocol/src`; CF = `peer/ControllerCommissioningFlow.ts`,
CC = `peer/ControllerCommissioner.ts`, CN = `peer/CommissioningConnection.ts`,
CA = `certificate/CertificateAuthority.ts`, DAV = `certificate/DeviceAttestationValidator.ts`,
CI = `action/client/ClientInteraction.ts`. Read the lines a fact cites
before you mirror them; a brief line that differs from the file is a line
to correct in the report. One commit per item, reachability in each,
CHANGELOG under `[Unreleased]` → Added. Guards: `make ci`, `make race`,
`make reachability-check`, and `make chiptool-test` once at the end.
You may create the four packages and change `controller/discovery`,
`controller/exchange`, `controller/handshake`, `controller/peer` where an
item says so; `secure/`, `transport/`, `mdns/`, `bridge/`, `node/`, `im/`,
`cluster/` are read, not changed — a change you need there is a question
in the report. `notes/parity/by_design.md` and the findings register are
the owner's. **`controller/attestation` is trust logic: the owner reads
every line of it against DAV; keep it small, literal and commented.**

Out of this brief, by ADR 0020 and the stage split: BLE, Wi-Fi and Thread
network commissioning (CF:596-625 runs those steps only on a BLE channel;
on an IP channel it logs "Skipping NetworkCommissioning steps"), the
BLE re-arm timer (CF:905-939) and the pessimistic failsafe (CF:963-974),
time synchronisation (CF:1199-1211, a stub there too), T&C
acknowledgements (not implemented upstream, CF:546, :570), the default
OTA provider step (CF:2083-2136), the DCL (`dcl/DclCertificateService.ts`
— the PAA store below is static), Joint Fabric, and the subscriptions and
general client interaction of stage 3 (`controller/interaction` carries
only what the flow needs).

## What exists (reuse, do not duplicate)

- `controller/handshake.PASE(ctx, mgr, addr, passcode, PASEOptions)` with
  `SuppressPeerLoss` and `OnPBKDFParamResponse` (`pase.go:33-45`);
  `handshake.CASE`, `handshake.Fabric{Identity *sigma.Identity,
  RootPublicKey, Verifier}` (`case.go:20-34`); `exchange.Session.AttestationChallenge()`
  (the PASE challenge key, `session.go:127`), `ReportedParameters()`.
- `controller/peer.Set.For(addr)`, `AddKnownPeer`, `Peer.Connect(ctx,
  ConnectOptions{ConnectionTimeout, Timing, HandleError})`, `Peer.Delete`,
  `Descriptor{DiscoveryData, ReportedSessionParameters, CaseAuthenticatedTags}`
  (`connection.go:78-90`, `:193`; `store.go`).
- `controller/discovery.Scanner.FindCommissionable`, `Device{TXTData,
  InstanceID, D, CM, Hostname, Addresses}` (`scanner.go:132-145`),
  `discovery.CompareDesirability`.
- `secure/mattercert`: `NewVerifier(rootPub, clock)`, `ValidateRCAC`,
  `Certificate` decoding (`decode.go`); `secure/sigma.Identity`
  (`sigma.go:96-141`: key, NOC, ICAC, IPK, node id, fabric id, compressed
  fabric id, fabric index); `secure/attestation`: the CSA test PAAs
  `TestPAAFFF1Cert`, `TestPAANoVIDCert` (`testpaa.go:45-...`), the CD
  signer's SKID `TestCMSSignerSKID` and key (`:129-138`), `BuildTestChain`
  (what the reference node presents).
- `im`: `MarshalInvokeRequestTLV`, `UnmarshalInvokeResponseTLV`
  (`invoke_client.go:41`), `ReadRequest.MarshalTLV`, `ReportData` decoding
  as `bridgetest/session.go:160-182` `reports()` does it, `im.StatusIB`,
  `OpcodeInvokeRequest`/`OpcodeInvokeResponse`/`OpcodeReadRequest`/`OpcodeReportData`/`OpcodeStatusResponse`
  (`protocol.go`), `im.InteractionModelProtocolID`.
- Generated request/response types: `cluster/spec/generalcommissioning`
  (`ArmFailSafeRequest`, `ArmFailSafeResponse`, `SetRegulatoryConfigRequest`,
  `CommissioningCompleteRequest`, `BasicCommissioningInfo`, the attribute
  and command ids), `cluster/spec/basicinformation`, `cluster/spec/descriptor`,
  `cluster/spec/networkcommissioning` (FeatureMap bits),
  `cluster/spec/operationalcredentials` does not exist (ADR 0013 skips the
  cluster): the ids are `bridgetest/commissioner.go:56-73` and the wire
  shapes `cluster/core/operational_credentials.go` (`encodeNOCSRElements`
  `:1493`, `encodeAttestationElements` `:1309-1340`, `NOCResponse`,
  `AttestationResponse`, `CertificateChainResponse` encoders — read how the
  device writes them, and decode them in `controller/commissioner/wire.go`).
- Tests: the in-process node of `controller/handshake/helpers_test.go`
  (`node.New` with its passcode and discriminator), `controller/peer`'s
  node test (records injected into a real `discovery.Names` so the path
  runs under `-race`), the fake clocks of `exchange` and `peer`.

## 0. Additions to the stage-1 packages

- No `Scanner.Forget`: matter.js's `Scanner.forgetCommissionedDevice`
  (`common/Scanner.ts:291-299`) is optional, and `CommissionableMdnsScanner`
  at `0ceb202d` does not implement it (CC:325-342 then does nothing for
  mDNS). A commissioned node withdraws its `_matterc` record itself; the
  cache follows the goodbye. Say so in the report if you find otherwise.
- `handshake.CASEOptions.Exchange exchange.Options` — the hooks AG
  carried in the context move here (report AG, owner decision 1):
  `CASE` opens its exchange with them; `exchange.WithHooks` and
  `Hooks` go. `peer`'s connection process passes its `OnSend`/`OnReceive`
  through the option.
- `exchange.Manager.SessionsFor` already exists; nothing else.

## 1. `controller/ca` — the certificate authority (CA)

The port of `certificate/CertificateAuthority.ts` (CA:50-437): the fabric
root of trust that issues the controller's own NOC and every
commissioned node's NOC.

- `ca.New(cfg Config) (*CA, error)` with `Config{Store Store; Now func()
  time.Time; IntermediateCA bool; Rand io.Reader}`: a stored root is loaded
  (`CA:149-167`: rootCertId, rootKeyIdentifier, rootCertBytes,
  nextCertificateId, rootKeyPair, and the ICAC fields when one exists;
  a store whose ICAC presence disagrees with `IntermediateCA` is an
  error `:367-373`); otherwise a P-256 key pair, key id = SHA-1 of the
  public key (`:169-170`), the RCAC (`#generateRootCert` `:225-249`:
  serial = root cert id 0, ECDSA-SHA256, EC P-256, issuer = subject =
  {rcacId}, notBefore = now − 1 year, notAfter = now + 10 years
  (`jsToMatterDate`, `kinds/definitions/asn.ts:18-22`: Unix seconds
  minus the Matter epoch offset plus the years; the X.520
  non-well-defined date encodes as 0), basicConstraints isCa, keyUsage
  keyCertSign + cRLSign, SKID = AKID = the root key id), the ICAC when
  configured (`:251-285`: cert id = next id, subject {icacId}, issuer
  {rcacId}, same validity, signed by the root), then `Store.Save`
  (`:181-183`). `Store` interface + `MemoryStore`; the stored fields are
  `CA:391-409`, `:448-474`.
- `CA.RootCert() []byte` (Matter TLV), `RootPublicKey() []byte` (65
  bytes), `ICACCert() []byte` (nil without), `Verifier() *mattercert.Verifier`
  (over the root), `GenerateNOC(pub []byte, fabricID, nodeID uint64, cats
  []uint32) ([]byte, error)` (`CA:287-320`: cert id = next id — **and
  persist it**: matter.js writes the counter only at creation (`:181-183`),
  so serials repeat after a restart; the Go CA saves after each issue and
  the report names it as a by_design candidate), issuer and signer the
  ICAC when present else the root (`#getSigningParameters` `:411-429`),
  subject {fabricId, nodeId, caseAuthenticatedTags}, validity −1 y / +10 y,
  basicConstraints isCa false, keyUsage digitalSignature,
  extendedKeyUsage [clientAuth, serverAuth] ([2, 1]), SKID = SHA-1 of
  the subject key, AKID = the signer's key id.
- The Matter TLV certificate encoder: port `bridgetest/ca.go` `certFields`
  / `sign` / `encode` (`:141-222`) into `controller/ca/encode.go` with
  the DN tags, the extension tags and the signature shape it uses (they
  cite `secure/mattercert` and the spec; keep the citations); a
  round-trip test decodes every minted certificate with
  `mattercert` and `ValidateRCAC` accepts the root. `bridgetest` keeps
  its copy for now (brief AI replaces it).
- The validity instants come from `Config.Now`; the test CA of
  `bridgetest` uses a fixed instant for deterministic bytes — that stays
  there. The compressed fabric id derivation (`bridgetest/ca.go:106-120`,
  HKDF over the root key) is `ca.CompressedFabricID(rootPub, fabricID)`.

## 2. `controller/attestation` — device attestation (DAV)

The port of `DeviceAttestationValidator.validate` (DAV:170-537) with a
static trust store in place of the DCL.

- `attestation.Validate(ctx Context, data Data) (Result, error)` with
  `Context{Challenge []byte; Store PAAStore (nil = none); Now func()
  time.Time}` and `Data{DAC, PAI, Elements, Signature, Nonce []byte;
  VendorID, ProductID uint16}`; `Result{DACPublicKey *ecdsa.PublicKey;
  Findings []Finding}`; `Finding{Level, Check, Message}`, `Check` the
  enum of DAV:28-52, `Error{Check, Message}` (DAV:67-75, the hard
  failure). Order and rules exactly DAV's:
  1. parse DAC and PAI as X.509 DER (`crypto/x509`; empty or undecodable
     → `CertificateUnparseable`, DAV:81-106, :175-176);
  2. DAC vendor id = PAI vendor id (`VendorIdMismatch`, :181-186) — the
     Matter attestation DN attributes (vid, pid) are the OIDs
     `1.3.6.1.4.1.37244.2.1/2` or the `Mvid:`/`Mpid:` CN fallback; read
     `kinds/AttestationCertificates.ts` and `secure/attestation/builder.go:39-66`
     for both forms and cite;
  3. decode the attestation elements (`TlvAttestation`,
     `common/OperationalCredentialsTypes.ts:10-16`: declaration [1],
     nonce [2], timestamp [3]; `AttestationElementsUnparseable`) and
     compare the nonce (`AttestationNonceMismatch`, :188-199);
  4. verify the ECDSA signature over elements ‖ challenge with the DAC
     key (`AttestationSignatureInvalid`, :201-218; the device signs
     SHA-256 of that concatenation, `cluster/core/operational_credentials.go:1347-1365`,
     the signature r‖s 64 bytes);
  5. without a store: a warning finding `DclServiceUnavailable` and no
     PKI check (:224-231). With one (:232-364): the PAA by the PAI's
     authority key identifier (`PaaNotTrusted`); a non-production PAA
     under a store that does not accept test certificates → error
     finding `TrustedAsTestCertificate` (:243-251); PAI signature by the
     PAA key and DAC signature by the PAI key (`CertificateChainInvalid`,
     :272-303; `mayIssue` :288-294: read `kinds/CertificateSignature.ts`
     and cite — P-256 only here, so it is the one algorithm); validity
     at the DAC's notBefore against PAI and PAA notBefore/notAfter
     (`CertificateExpired`, :305-331; notAfter 0 = none; the validator
     never compares with `Now` — mirror that and name it in the report);
     a PAA vendor id, when present, must equal the PAI's (:333-340);
     revocation through `PAAStore.IsRevoked(akid, serial, issuerDN)`
     when the store implements `Revocations` (:342-363; a store without
     it reports not revoked — by_design candidate);
  6. the Certification Declaration: parse the CMS SignedData (`kinds/CertificationDeclaration.ts`,
     read and cite; `secure/attestation/cd.go:62-135` is the device's
     encoder of the same structure — the inner TLV with format version,
     vendor id, product id array, device type id, certificate id,
     security level and information, version number, certification
     type, dac origin vid/pid, authorized PAA list), `CertificationDeclarationUnparseable`;
     the signer by SKID from `PAAStore.CDSigner(skid)` — found: verify
     (`CertificationDeclarationSignatureInvalid`), not found or no
     store: warning `CdSignerVerificationSkipped` (:369-400);
  7. CD content (:402-506): certification type Provisional → warning,
     Test → info; vendor id must equal the node's BasicInformation vendor
     id; product id in the product id array; dac origin vid/pid both or
     neither, and the DAC/PAI vid (and pid when present) against them,
     else against the CD's vendor id and product id array — every
     mismatch `CertificationDeclarationFieldMismatch`; authorized PAA
     list, when present and the PAA known: the PAA SKID must be listed
     (:508-526; unknown PAA → warning `DclServiceUnavailable`).
  8. returns the DAC key and the findings (:536). `certificate_id` and
     firmware information are TODO upstream (:421-422, :528-534).
- `PAAStore` interface: `PAA(akid []byte) (PAA, bool)` with
  `PAA{DER []byte; Production bool; FetchedAt time.Time}` (zero
  FetchedAt skips the `PaaTrustStoreTimeMismatch` info finding
  :253-266), `AcceptsTestCertificates() bool`, `CDSigner(skid []byte)
  (*ecdsa.PublicKey, bool)`; optional `Revocations` interface.
  `StaticStore{PAAs []PAA; CDSigners map; AcceptTest bool}`;
  `TestStore()` holds the two CSA test PAAs of `secure/attestation` and
  the test CD signer's public key (derive it from `TestCMSSignerPrivateKey`),
  `AcceptTest` true — what commissions the reference node and what
  chip-tool's test PAAs are.
- `Policy` = `OnFailure`: `bool` or `func(findings) (proceed bool,
  reason string, err error)` (DAV:156, CF:129-151, `#resolveAttestationFindings`
  CF:1341-1368: a function's decision; `true` proceeds with an info log,
  `false` rejects, nil (matter.js `undefined`) proceeds with a warning —
  the backward-compatible default the report names as the one to turn
  off later). Applied by the flow (§4), not by `Validate`.

## 3. `controller/interaction` — what the flow sends

The minimum of `ClientInteraction` (CI) the flow needs, on an
`*exchange.Session`; stage 3 grows it.

- `Invoke(ctx, session, path im.ConcreteCommandPath, fields Encodable,
  InvokeOptions{ExpectedProcessingTime; ExtendedFailSafeTimeout bool;
  Timed time.Duration}) (Result, error)`: one exchange on the IM protocol;
  a timed request first when `Timed` > 0 (CI:438-440, default 10 s
  `:159`); the single command without a commandRef (CI:457-464); the
  response timeout is `ExpectedProcessingTime`, else 30 s when
  `ExtendedFailSafeTimeout` (`DEFAULT_MINIMUM_RESPONSE_TIMEOUT_WITH_FAILSAFE`
  CI:160, :466-474), else the exchange's default; `Result{Command
  uint32; Fields tlv bytes; Status im.StatusCode; ClusterStatus}` from
  `im.UnmarshalInvokeResponseTLV`; a non-Success status is
  `StatusError{Status, ClusterStatus}` (CF:506-530 `#invokeCommand`:
  `cmd-status` ≠ Success throws `StatusResponseError`). Chunked
  InvokeResponses (`interaction/InteractionMessenger.ts:1082-1105`) are
  acknowledged with a Success StatusResponse between chunks — cite.
- `Read(ctx, session, paths ...im.ConcreteAttributePath) (Report,
  error)`: ReadRequest with `FabricFiltered` false and the IM revision
  (fix `im.ReadRequest`? — no, `im/` is closed: encode the request in
  this package until stage 3 moves the client encoders into `im`; the
  open item in `docs/open-items.md` names it), ReportData decoded into
  `Report` with `Value(endpoint, cluster, attr)` (the
  `bridgetest/session.go` shape), multi-chunk reports followed by the
  StatusResponse the device expects (CI:277-285, read `readDataReports`).
- Both run under `exchange.Options`/`SendOptions` with the session's
  parameters; errors of the exchange layer pass through unchanged.

## 4. `controller/commissioner` — the commissioner (CC, CN, CF)

- `commissioner.Fabric`: `handshake.Fabric` plus what the flow needs
  (`fabric/Fabric.ts:55-70`, `:188`, `:256`, `:482`, `:522`, `:625`):
  `FabricID`, `RootNodeID` (`caseAdminSubject`, CF:1451), `RootVendorID`
  (`adminVendorId`, CF:1450), `IPK` (`identityProtectionKey`, CF:1449),
  `Label` (UpdateFabricLabel, CF:1494), `FabricIndex`, and
  `AddressOf(nodeID) handshake.PeerAddress` (`:482`). `NewFabric(ca *ca.CA,
  FabricConfig{FabricID, RootNodeID, VendorID, IPK, Label, FabricIndex,
  CATs}) (*Fabric, error)`: mints the controller's own NOC through the
  CA and builds the `sigma.Identity` (key, NOC, ICAC, IPK, ids, the
  compressed fabric id) — what `bridgetest.Commissioner.identity`
  (`handshake.go:124-166`) does for its test CA.
- `Commissioner` (CC:230-737): `New(Context{Exchanges *exchange.Manager;
  Peers *peer.Set; Scanner *discovery.Scanner (optional, for Forget);
  CA *ca.CA; Attestation attestation.PAAStore (nil = none); Logger})`.
  `Commission(ctx, Options) (Result, error)` with `Options` =
  `LocatedNodeCommissioningOptions` (CC:134-170) minus BLE/Wi-Fi/Thread:
  `Fabric *Fabric`, `Passcode uint32`, `NodeID uint64` (0 = random
  operational node id, CC:569-582), `Addresses []*net.UDPAddr`,
  `DiscoveryData *discovery.Device` (its TXT parameters seed the
  unsecured session, CC:454-463; `DeviceIdentifier` = its instance id),
  `Timeout time.Duration` (the PASE budget, default 30 s CC:264),
  `ClaimNodeIDAfterPASE func() (uint64, bool)` (CC:283-292),
  `RegulatoryLocation` (default Outdoor CC:603), `RegulatoryCountryCode`
  (default "XX" CC:604), `OnAttestationFailure attestation.Policy`,
  `FinalizeCommissioning func(ctx, addr handshake.PeerAddress, dd
  *discovery.Device, params *sessionparams.Parameters) error` (replaces
  the built-in CASE, the flow then stops before CommissioningComplete,
  CC:687-690, CF:2035-2041), `CASEConnectionTimeout` (default 255 s
  CC:123), `CASEConnectionTiming *peer.Timing`. `Result{Address
  handshake.PeerAddress; FabricIndexOnPeer uint8}` (0 = unknown, CC:724-726).
  `EstablishPASE(ctx, PASEOptions)` (CC:352-376) for a caller that only
  wants the session (`ContinueAfterPASE func() bool`).
- `Commission` (CC:255-308): the requested node id must not be a known
  peer (`NodeIdConflictError`, CC:495-505); options validated; the
  addresses deduplicated and sorted by desirability into candidates
  (CC:547-566: one candidate carrying every address when a device
  identifier is known — a wrong passcode on one address then drops the
  whole device — else one candidate per address with a synthetic
  identifier); PASE through the connection (§4a) with `SuppressPeerLoss`;
  then the node id claim, the connected-node flow (§4b), and the PASE
  session closed on every path — `CommissioningTransitionError` on
  success (`PeerCommunicationError.ts:117`), the error as cause on
  failure (CC:299-307, `#closePaseSession` `:313-319` swallowing close
  errors). A `PairRetransmissionLimitReachedError` from the connection
  becomes `PeerCommunicationError("Could not connect to device")`
  (CC:402-407).
- **§4a the PASE connection** (CN:60-250): candidates = devices ×
  addresses in order (`:86`); one scheduler (`:93-117`) launches the
  next attempt while no winner, not aborted and no attempt holds the
  responder; a candidate of a dropped device is skipped (`:98-100`); a
  new attempt waits until the latest unsettled one is 10 s old
  (`DELAY_BEFORE_NEXT_ADDRESS` `:35`, `:106-113`). An attempt (`:119-201`)
  runs `handshake.PASE` with the device's TXT parameters and
  `OnPBKDFParamResponse` marking it as holding the responder (`:127-133`;
  chip binds one PASE to the first PBKDFParamRequest exchange, so no
  second address of that device may be raced); on success it is the
  winner unless one exists or its device was dropped — then the session
  is force-closed (`:137-157`) — and the winner cancels every other
  attempt (`:158-161`; a cancelled PASE sends InvalidParam so the device
  does not lock up for 60 s). Errors (`:163-190`, skipped for a
  cancelled attempt): `ErrPasscodeMismatch` drops the device (every
  address of it), remembered as the non-retryable error; another
  "unexpected data" error is remembered and the other addresses still
  run; a timeout, transient or network error is the retryable last
  error; anything else aborts the whole connection. The result
  (`:205-239`): the winner at once without waiting for the losers; else
  the non-retryable error, else on the budget's end
  `PairRetransmissionLimitReachedError` (an external cancellation as
  is), else the last error wrapped. Hold the budget as one `Abort`
  (`:63`): `Timeout` plus the caller's ctx.
- **§4b the connected node** (CC:588-736): defaults; the operational
  address = a random operational node id not in the set, or the
  requested one (`:633`, `:569-582`); the flow (§4c) over the PASE
  session with the attestation context (challenge =
  `session.AttestationChallenge()`, the store, the policy; `:653-670`)
  and the CASE transition (`:671-716`): a non-concurrent device closes
  the PASE session first (`:672-685`; `supportsConcurrentConnection`
  from GeneralCommissioning); `FinalizeCommissioning` when given
  (`:687-690`); else `peers.For(addr)`, the descriptor's discovery data
  and reported parameters from the PASE session (`:692-696`),
  `Peer.Connect` with the CASE timeout, timing and an error handler
  that retries a NoSharedTrustRoots status after 15 s (`:697-709`; the
  device may not know the fabric yet); the CASE session is the
  interaction's from then on. After the flow: `Scanner.Forget`
  (`:721-723`, a no-op for mDNS, see §0); on any error
  `peers.Get(addr).Delete` (`:727-730`).
- **§4c the flow** (CF:312-2141), the steps that run on an IP channel,
  sorted by (step, sub-step) (`:536-663`): 0.1 GetInitialData, 7.1
  ArmFailSafe, 8.1 SetRegulatoryConfig, 10.1 DeviceAttestation, 11.1
  Certificates, 18.1 Reconnect, 20.1 CommissioningComplete, 98.1
  UpdateFabricLabel. Every command and read targets endpoint 0
  (`RootEndpointNumber` `:307`); every invoke of a step but ArmFailSafe
  and UpdateFabricLabel uses the extended failsafe timeout (30 s);
  `lastBreadcrumb` starts at 1 (`:327`) and only SetRegulatoryConfig
  increments it (`:1138`, `:1162`).
  - The loop (`:378-468`): run the step; while a failsafe is armed, a
    total past `maxCumulativeFailsafeSeconds` since the first arm is
    `CommissioningTimeoutError` (`:398-407`), and less than half the
    default failsafe time left re-arms (`:414-423`); `Stop` ends the
    loop (`:426-428`, only Reconnect returns it). Errors: a
    `RecoverableCommissioningError` (only UpdateFabricLabel) is logged
    and the loop continues (`:431-436`); a `CommissioningError` or an IM
    status error disarms the failsafe (`#resetFailsafeTimer` `:1077-1093`:
    ArmFailSafe with 0, a failure only logged) and is returned as a
    `CommissioningError` (`:437-462`); any other error is returned as is
    without the disarm (`:463-465`). No retries (`:443-451`, a TODO
    upstream; name it in the report).
  - 0.1 (`:724-833`): one Read of OperationalCredentials
    SupportedFabrics/CommissionedFabrics, Descriptor PartsList/ServerList,
    BasicInformation VendorID/ProductID/ProductName, GeneralCommissioning
    SupportsConcurrentConnection; `commissionedFabrics >= supportedFabrics`
    → `MaximumCommissionedFabricsReachedError` (`:759-763`); then
    NetworkCommissioning FeatureMap on endpoint 0 and every endpoint of
    the parts list (matter.js reads the wildcard endpoint `:773-784`; the
    concrete paths give the same set) — `networkFeatures` decides 8.1.
  - 7.1 (`:844-886`): on the first arm read `BasicCommissioningInfo`,
    take `failSafeExpiryLengthSeconds` as the default failsafe time and
    start the cumulative budget (`:845-861`); the length = min(requested
    or default, `maxCumulativeFailsafeSeconds`) (`:863-868`); ArmFailSafe
    {breadcrumb, expiryLengthSeconds}; `ErrorCode` ≠ Ok →
    `CommissioningError` with the code's name and debug text
    (`#ensureGeneralCommissioningSuccess` `:712-719`); remember the end
    time (`:881`). `#failSafeTimeLeft` (`:888-893`),
    `#ensureFailsafeTimerFor(maxProcessingTime)` (`:895-951` without the
    BLE timer: re-arm when less than response wait + 10 s is left,
    `FAILSAFE_PROCESSING_MARGIN` `:298`, `FAILSAFE_MAX_SEND_TIME` `:301`).
  - 8.1 (`:1102-1188`): only when some NetworkCommissioning instance has
    the Wi-Fi or Thread feature (`:1109-1112`), else Skipped — the
    reference node is Ethernet, so this step is Skipped there; read
    LocationCapability, IndoorOutdoor takes the option else the device's
    value (`:1115-1133`); SetRegulatoryConfig {breadcrumb++, config,
    country}; `ValueOutsideRange` with a country other than "XX" retries
    once with "XX" (`:1150-1175`).
  - 10.1 (`:1218-1338`): CertificateChainRequest DAC (type 1) then PAI
    (type 2), a 32-byte random nonce, AttestationRequest; `attestation.Validate`
    (§2); an `attestation.Error` becomes one error finding, any other
    error a `ValidationError` finding (`:1286-1303`); the policy decides
    (`:1305-1315`); the DAC key is kept for 11.1, parsed from the DAC
    when the validator threw but the policy accepted (`:1321-1331`).
  - 11.1 (`:1388-1468`): CSRRequest with a 32-byte nonce; empty elements
    or signature → error (`:1401-1403`); the CSR signature over
    nocsrElements ‖ challenge against the DAC key (`:1405-1416`); the
    CSR (`TlvCertSigningRequest` `:18-21`: certSigningRequest [1], nonce
    [2]) decoded, its public key the NOC subject key (`:1418-1419`;
    matter.js does not compare the echoed nonce — `bridgetest` does;
    mirror matter.js and name it); AddTrustedRootCertificate with the
    CA's root (response ignored, `:1421-1433`); `ca.GenerateNOC(key,
    fabricID, nodeID, nil)` (`:1435-1439`, no CATs); AddNOC {noc, icac
    or empty, ipk, adminVendorId = RootVendorID, caseAdminSubject =
    RootNodeID} (`:1441-1457`); NOCResponse status: Ok; FabricConflict →
    `DeviceAlreadyCommissionedToThisFabricError`; TableFull →
    `MaximumCommissionedFabricsReachedError`; else `CommissioningError`
    with the status name, debug text and fabric index
    (`#ensureOperationalCredentialsSuccess` `:674-709`); the fabric index
    kept (`:1461-1462`).
  - 18.1 (`:2000-2052`): concurrent flow → `#ensureFailsafeTimerFor`(5 min,
    `CASE_RECONNECT_TIMEOUT` `:295`), a failure only logged; the
    transition (§4b); its error → `OperativeConnectionFailedError`
    (`:2024-2030`); nil interaction → Stop (`:2035-2041`); else the PASE
    interaction closed and the CASE one used (`:2043-2044`).
  - 20.1 (`:2061-2081`): CommissioningComplete over CASE, extended
    timeout, `ErrorCode` checked, the failsafe end time cleared (`:2075`).
  - 98.1 (`:1477-1506`): UpdateFabricLabel {label} over CASE when the
    fabric index is known; any error → `RecoverableCommissioningError`
    (`:1498-1500`; `LabelConflict` → `FabricLabelConflictError` inside it,
    `:696-702`).
  - Errors: `CommissioningError` and the classes of CF:223-254 as Go
    error types (`errors.Is`/`As`-able), `PairRetransmissionLimitReachedError`
    (`peer/CommissioningError.ts:19`), `exchange.PeerCommunicationError`
    where AF/AG put it.
- Logging as the flow logs (step start, failsafe re-arm, findings
  accepted) through the context's logger.

## Tests and proof

- `controller/ca`: round trips of RCAC, ICAC and NOC through
  `mattercert` (`ValidateRCAC`, `NewVerifier(root).VerifyAndExtractPubKey(noc,
  icac)`), the validity instants from `Config.Now`, store save/load
  (fields `CA:391-409`), the counter persisted per issue, the compressed
  fabric id against `bridgetest`'s derivation for the same root.
- `controller/attestation`: the reference node's chain
  (`secure/attestation.BuildTestChain` + `BuildTestCertificationDeclaration`)
  validates against `TestStore()` with the findings matter.js produces
  for it (which? run it and compare the list with DAV's rules — a test
  CD gives `CertificationTypeTest` info; a missing CD signer a warning);
  every hard failure on a tampered input (wrong nonce, wrong signature,
  DAC not signed by the PAI, vendor id mismatch, PAI from an unknown PAA,
  DAC notBefore before the PAI's, CD vendor id mismatch, product id not
  listed); without a store only the local checks and the warning; the
  policy table (CF:1341-1368).
- `controller/interaction`: Invoke and Read against the in-process node
  (a status reply, a response with fields, a two-chunk ReportData);
  the timeouts on a fake clock.
- `controller/commissioner`: unit tests on fake clocks for the
  connection (§4a: the 10 s stagger, the holding rule, the passcode
  drop of a whole device, the budget, the winner cancelling the losers)
  and the flow's loop (re-arm thresholds, the cumulative timeout, the
  disarm on failure, Recoverable continues, Stop); the full
  commissioning of the in-process node: `Scanner.FindCommissionable` with
  injected records → `Commission` → the node's fabric table shows the
  fabric, the CASE session is the peer's (`peers.Get(addr).NewestSession()`),
  CommissioningComplete answered, UpdateFabricLabel set, a Read over
  the CASE session; a second `Commission` with the same fabric →
  `DeviceAlreadyCommissionedToThisFabricError` and the failsafe
  disarmed; a wrong passcode → the device dropped and
  `PeerCommunicationError`; `FinalizeCommissioning` stopping the flow;
  a live-multicast variant (`!race`). Then the reference bridge binary
  (`examples/reference-bridge`, started as `internal/chiptool` starts
  it — read `internal/chiptool/doc.go` and the harness helpers for the
  passcode and discriminator) commissioned by the Go commissioner over
  the host's multicast, in `internal/chiptool` as a test that skips
  without the binary's prerequisites, as `TestMatterJSController` does.
- `controller/*/parity_matterjs_test.go`: the constants (CF:289-305,
  CC:123, :264, CN:35, CI:159-160), the step table and order, the error
  classification tables, the NOC fields.
- `make chiptool-test` at the end (unchanged responder). Coverage floors
  for the four packages; the floors of the packages you touched hold;
  reachability regenerated.

## Report

`notes/briefs/reports/AH.md`: the type map, the lines that differed from
the brief, what you left out and why, the `secure/`/`im/`/`cluster/`
gaps found, what brief AI (bridgetest onto `controller/`) and stage 3
need from here, open questions, the by_design candidates (the persisted
certificate counter, the revocation default, the attestation policy
default). The owner's decisions follow in the report file.
