# Pin lifts — what moved upstream, and what it means here

One section per lift of the matter.js pin (`parity/schema.json` →
`matter.sourceCommit`). Every upstream fix between the two commits is
classified against this module: **have it** (by construction or already
ported), **gap** (a register entry in `matter_behaviour_findings.md`, closed by
a later wave), **not applicable** (JS-runtime concern, harness, a role or
feature this module does not have yet — with the milestone that will have
it), **caught up** (matter.js now implements behaviour this module already
had ahead of it — a row of the *Ahead of matter.js* register in
[`by_design.md`](./by_design.md); the two implementations are reconciled onto
matter.js and the row closes, see `CLAUDE.md` workflow step 5), or **open**
(to be settled by the test port named). Independently of the commit list,
every lift re-reads each marker of that register at the new pin: an upstream
change can close a gap without a commit message that says so. The trigger
is the check over those markers: `script/aheadcheck` (`make ahead-check`)
reads each row's marker file and text at the pin and at a ref, and the
weekly `ahead-markers` job in `.github/workflows/matterjs-pin.yml` runs it
against matter.js `origin/main` and goes red with the finding "caught up?"
naming the row as soon as a marker text is gone there — between lifts, not
only at one. The manual re-read at the lift is the confirmation: a row gets
the verdict **caught up** only after the new pin has been read and the
behaviour compared, never on the check's finding alone. Owner's work (Fable
row); the reading map behind each section is a Sonnet `read-*.md` under
`notes/reference/`, verified line by line before anything landed here.

## 862ad75f → db021da0 (2026-11-06, milestone 10 after phase 39)

63 commits; 17 skipped (9 `[CHIP BUILD]`, 3 version bumps / the 0.18.0
release, 2 `chore`/`ci`, 3 Dockerfile-only harness images). Map:
`notes/reference/read-matterjs-delta-db021da0.md`. Model: no change under
`packages/model/src/standard/elements`, `packages/types/src/clusters` or
`packages/types/src/globals` — no cluster, attribute, command, event,
enum, revision or conformance moved; the snapshot's only change is
`matter.sourceCommit` (and the generators' source line in every generated
file). A behaviour lift, not the 1.7 pin (roadmap phase 40 waits for
chip's `data_model/1.7` in matter.js's snapshot). Owner decision
2026-11-06: the gaps are fixed before `v0.18.0`.

The register's markers at db021da0 (`make ahead-check` against
`origin/main`): **caught up** —
`BD-Matter-MessageCodec-PrivacyWithExtensions` (#4843: `decodeFixedHeader`
keeps the obfuscated region of a privacy packet and the MX flag,
`MessageCodec.ts:258-272`; the message extension is read length-checked
after the fixed header, `:328-340`; P on a unicast session refused "as in
the CHIP SDK", `:251-253`); our parser and the bridge already did the
same, the row closed, the entry kept as the record. A false alarm —
`BD-matterjs-server-NetworkTopology`: the absence marker is the body of
`class NetworkClient`, which #4808 edited (`WaitingForDeviceDiscovery` for
a peer with no address); no topology service appeared, the row stays and
its marker is re-read at the new pin. Every other marker holds. Also
caught up outside the register: `BD-Matter-CaseInitiatorPeerCATs` (#4884,
the initiator takes the responder's CATs from its NOC, `CaseClient.ts:282-290`).

Citations that resolve at the pin — the bare `path:line` form without an
`at <sha>` — now resolve at db021da0: 18 pointed beyond the end of a
shrunken file (hard errors) and were re-read and updated, and the drift
count of `make cite-check` rose from 253 to 419 (`citecheckDriftRatchet`
raised in the lift's commit: a citation whose line moved without changing
what it says drifts, and a lift moves many at once; the drift is worked
off citation by citation as the files are touched, never by a sweep).
The 18 hard errors: seven files; re-read and
updated (`ServerBehaviorBacking.ts` quiet subscription `:251-259` /
`#configureEventSuppression` `:223-262`, `PeerExchangeProvider.ts:19-159`
/ `:150-158`, `validateNocTagList` now `Noc.ts:116`, the CADMIN flows in
`tc-cadmin-support.ts`, `tc-dd-flow-support.ts:72-189`, `MdnsTest.ts`
`expectMessage` `:1001-1024`, `Rcac.ts:135`).

The matter.js test register (`script/testparity/register.json`) moved with
the snapshot: 56 entries re-checked against their new blobs (395 new `it`
cases listed as missing, 30 removed cases pruned, 11 ported files now
partial), 3 files gone (the WAL cleaner and snapshot tests, the peer
establishment progress test), 24 new files entered — 6 excluded as JS
runtime, 18 planned on the lift's waves P1–P4. Ratchets: planned 2 → 20,
ported 158 → 146, missing 583 → 977, left 593 → 592. The missing cases are
the waves' port lists.

### Security

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4884 | the CASE initiator takes the peer's CATs from its NOC in Sigma2 (`CaseClient.ts:282-290`); resumption records versioned | **caught up** at the sigma layer; **gap → lift wave P1**, closed 2026-11-06, at the client stack | `secure/sigma/initiator.go` `processSigma2` agreed; `client/handshake/case.go` gave an initiated session the configured or recorded CATs, which `client/interaction/server.go` takes as the peer's subject CATs — wave P1 makes them the peer's NOC CATs and removes `CASEOptions.CaseAuthenticatedTags` and `ResumptionRecord.PeerCATs` (BD-Matter-CaseInitiatorPeerCATs); the record version is a JS storage detail, the module's records live in memory |
| #4843 | group decode checks GroupKeyMap for the destination group (`GroupSession.ts:37-60`) | **have it** | `groups/decode.go:27-111` (a message belongs to the key set GroupKeyMap maps the group to) |
| #4843 | the fixed header refuses a group session message with a destination node id (MCSP, `MessageCodec.ts:303`, as chip), and an unsecured unicast message with both or neither of source and destination node id (`:305`) | **gap → lift wave P1**, closed 2026-11-06 (branch `pin/p1-security`) | `transport/message/message.go` refuses MCSP (`ErrGroupSessionHeader`) and an unsecured unicast message with both or neither node id (`ErrUnsecuredNodeIDs`); `MarshalPacket` the same; the bridge's unsecured replies and acknowledgements name one node id (`bridge/ackpump.go`, `bridge/reply.go`) |
| #4843 | the privacy flag on a unicast message refused (`:251-253`) | **have it** | `bridge/privacy.go` `maybeUnmaskPrivacy` |
| #4843 | AES-CCM input limits shared by the two crypto implementations | **not applicable** | Go's stdlib AEAD |
| #4865 | DAC/PAI certificate profile enforced in device attestation: version 2, basic constraints and key usage present and critical, no duplicate extension; PAI cA TRUE + pathLen 0 + keyUsage, DAC cA FALSE (`AttestationCertificates.ts:85-200`, `DacFormatInvalid`/`PaiFormatInvalid`) | **gap → lift wave P2**, closed 2026-11-06 | `controller/attestation/profile.go` (the DER encoding beside `crypto/x509`, `assertDacProfile`/`assertPaiProfile`), `cert.go` `parseAttCert` per kind, `validate.go` `parsePeerCertificate` (PAI first, `DacFormatInvalid`/`PaiFormatInvalid`); chip's commissioner DUT vectors in `testdata/chip-commissioner-dut`; `BD-Matter-Attestation-ProfileOnCryptoX509` |
| #4871 | Certification Declaration validated as chip does: CMS envelope, `format_version` 1, certification type, 20-byte signer SKID (`DeviceAttestationValidator.ts:456-515`); an unknown CD signer is `CdSignerUnknown`, an error, with a built-in trusted-signer set (`CdSigners.ts`) | **gap → lift wave P2**, closed 2026-11-06 | `controller/attestation/cd.go` `parseDeclaration` (`CertificationDeclarationFormatInvalid`), `validate.go` (format_version, reserved type, `OfficialCdSignedByTestSigner`, `CdSignerUnknown`, the test signer as `TrustedAsTestCertificate`), `cdsigners.go` (`KnownCDSigner`, `VerifyCDSignerCertificate`); `controller/dcl` `CertificateStore.CDSigner`/`AddCertificate` and `snapshot` trust CSA-issued signers only; `BD-Matter-Attestation-CDSignerTrust` |
| #4880 | operational certificates: a repeated single-valued DN field refused (`DistinguishedName.ts:242-246`, `:261-283`); matter-vvs-id (tag 23) decoded in NOC/ICAC/RCAC and refused there; a NOC must list serverAuth (`Noc.ts:148-154`); a VVSC with is-ca false accepted and its extensions checked (`Vvsc.ts`, `VendorIdVerification.ts`) | **gap → lift wave P1**, closed 2026-11-06 for the certificate rules; the VVSC chain check of the verifier (`VendorIdVerification.ts:231`) is #433's, closed by lift wave P4 2026-11-06 (`secure/mattercert/vvsc.go` `DecodeVVSC`, `ValidateVVSC`, `VerifyVVSCIssuedBy`; `client/jcm.Verify`'s statement path, `BD-Matter-VIDStatementVerification`) | `secure/mattercert/decode.go` and `der.go` refuse a repeated Matter-specific DN attribute and decode matter-vvs-id (`DistinguishedName.MatterVVSID`); `verify.go` `checkDNLimits` (five RDNs), serverAuth required of a NOC, matter-vvs-id refused in a NOC, ICAC or RCAC subject; the device keeps a set VVSC opaque, as `Fabric.ts:231` does |
| #4880 | a CAT with version 0 refused; at most three CATs; a CAT id twice refused | **have it** | `secure/mattercert/verify.go:457-470` |
| #4899 | message counter window edges (tests and comments; the "new" range ends at 2^32-1) | **have it** | `transport/mrp/window.go:12-62`; the edge tests are a parity port candidate (open, low) |
| #4879 | resumption records and server subscriptions stored per peer | **not applicable** | JS storage layout; `store` keys them by fabric and node already |

### Device side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4878 | AdministratorCommissioning: a discriminator above 0xFFF is INVALID_COMMAND in the handler, not CONSTRAINT_ERROR (`AdministratorCommissioningServer.ts:42-46, :91-97`); an open on a session without fabric answers PAKEParameterError; the check order Busy → window → parameters | **gap → lift wave P1**, closed 2026-11-06 | `cluster/wire/admincommissioning.go` (`assertNoCommissioningInProgress`, the fabric check first as PAKEParameterError, the discriminator last as INVALID_COMMAND); `bridge/fields_reader_admincomm.go` decodes any discriminator |
| #4881 | GeneralDiagnostics: `systemTime()` shared by UpTime and TimeSnapshot.SystemTimeMs, suspend-aware high-water mark (`GeneralDiagnosticsServer.ts:61-86`); TestEventTriggersEnabled derived from the enable key (true iff not all zero, `:144-156`); one gate for TestEventTrigger and PayloadTestRequest | **gap → lift wave P3** | `cluster/core/general_diagnostics.go:376-384, :529-536, :636-683` |
| #4844 | mDNS goodbyes only for records no remaining advertisement publishes; failed goodbyes retried with backoff (`InterfaceResponder.ts:46-67, :263-280`) | **gap → lift wave P3** | `mdns/announce.go`, `mdns/answer.go:77-111` |
| #4842 | the responder follows interface address changes (polled every 60 s while observed): rebuilds records, goodbyes removed addresses, announces twice 1 s apart; GeneralDiagnostics re-reads NetworkInterfaces on change (`MdnsServer.ts:288-348`, `GeneralDiagnosticsServer.ts:112-115, :389-401`) | **gap → lift wave P3** | `mdns/responder.go:295-300` has `TriggerReannounce` only; `general_diagnostics.go:53-56, :453` static NetworkInterfaces |
| #4841 | Thermostat Presets validated against the presets the transaction replaces (built-in and active-removal rules) | **open** — compare `cluster/thermo/atomic.go` with `ThermostatServer.ts` at db021da0 in wave P3 | `cluster/thermo/atomic.go`, `schedules_write.go:59-158` |
| #4900 | UserLabel and FixedLabel server tests (test-only; any server fix inside them is read in wave P3) | **open (low)** | `cluster/core` label servers |
| #4795 | a debug line when a node does not advertise because commissioning is disabled | **not applicable** | log only |
| #4825, #4811, #4841 (rest), #4902 | managed state references, decorator derivation, typings | **not applicable** | JS runtime |

### Controller side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4793 | the TCP send path is capped at min(channel, the peer's announced MAX_TCP_MESSAGE_SIZE); an oversized message is `MessageTooLargeError` (`MessageChannel.ts:87-112`, `Session.ts:230-241`) | **gap → lift wave P4**, closed 2026-11-06 | `client/exchange` `Session.PeerMaxTCPMessageSize` (the announced size, 64000 without one, never below 1232) caps `Session.MaxPayloadSize` under the channel's limit, and a larger message is `ErrMessageTooLarge` before it reaches the channel (`checkSize` in `transmitLocked`); `transport/tcp`'s limit excludes the length prefix and has no receive-buffer cap (`TcpChannel.ts:37-41`, `:204`); `bdx` offers at most 0xFFFF (`bdx-session-initiator.ts:155`); `client/exchange:TestMessageChannelTCPLimitParity`, `transport/tcp:TestTcpChannelOversizedMessageHandling` |
| #4794 | a Sigma1 the peer never acknowledges is refreshed on a new exchange with a fresh ephemeral key after the MRP transmissions, per address, with the backoff continued (`InitialContactSchedule.ts:1-246`) | **gap → lift wave P4**, closed 2026-11-06 | `client/peer/initial_contact.go` (`initialContactSchedule`, one per address; the attempt's Sigma1 options, the unresponsive signal and the kick blocker), `connection.go` `attemptOnce` retrying at once after an unacknowledged Sigma1; `exchange.SendOptions` `RetransmissionTimeLimit` and `BackoffOffset`, `handshake.CASEOptions` `InitialRetransmissionTimeLimit` and `InitialBackoffOffset`; `client/peer:TestInitialContactScheduleParity`, `client/peer:TestConnectionRefreshesUnacknowledgedSigma1Parity` |
| #4832 | delta event timestamps resolved per ReportData message against the previous event of the same clock (`InputChunk.ts:359-412`), `ReadResult.EventValue.resolvedTimestamp` | **gap → lift wave P4**, closed 2026-11-06 | `client/interaction/report.go` `eventTimestampChain` resolves per ReportData message, `EventData.ResolvedTimestamp` carries it to `Report.Events` and `controller/client` `Hooks.Event`; `client/interaction:TestInputChunkResolvedTimestampsParity`, `controller/client:TestClientEventNotificationResolvedTimestampsParity` |
| #4808 | a peer with no address at all reports WaitingForDeviceDiscovery (`NetworkClient.ts:541-546`) | **gap (small) → lift wave P4**, closed 2026-11-06 | `controller/client/network.go` `hasAnyAddress` (a session, the operational address or an address of the peer's operational service) feeds `recompute`, and `observePeer` recomputes on every change of that service (`NetworkClient.ts:87`, `:390-392`); `controller/client:TestConnectionStateWithoutAddressParity` |
| #4815 | the fail-safe re-arm between commissioning steps is skipped on a closed session (`ControllerCommissioningFlow.ts:835-849`) | **gap → lift wave P4**, closed 2026-11-06 (the periodic re-arm already skipped; the between-steps one did not) | `controller/commissioner/flow.go` `reArmFailsafeBetweenSteps`, called before a step with a re-arm and after a step that left less than half the failsafe (`ControllerCommissioningFlow.ts:385-393`, `:409-423`); `controller/commissioner:TestCommissioningClosedSessionReArmParity` |
| #4812 | legacy node enumeration skips unreadable peers | **not applicable** | the JS package's legacy API |
| #4809 | shell: legacy peer migration | **not applicable** | JS shell |

### Harness

#4792, #4814, #4819, #4820, #4823, #4828, #4845, #4850, #4851, #4852,
#4853, #4855, #4857, #4866, #4870, #4875, #4876, #4877: cert cases and
chip-testing adapters of matter.js's own harness — **not applicable**;
the cases they add (TC-CADMIN-1.1/1.2/1.18, TC-DA-1.3/1.4/1.8, TC-DD-1.9/
2.2/3.5, TC-CC-x.4/x.5, TC-TSTAT-3.2, TC-DRLK-3.2, TC-OO-3.2, TC-I-3.2,
TC-MOD-2.2, TC-LOWPOWER-2.2, TC-WNCV-6.1/7.1, TC-DGETH/DGSW/DGTHREAD/DGWIFI
3.x) are the Go-TH leg's next candidates (`internal/chiptool/gocert_*`),
tracked in `docs/certifiability.md`.

## Milestone 6 (2026-10-28, wave 27-C) — what the next lift re-reads

No lift; the pin stays `862ad75f`. Milestone 6 added 18 rows to the
*Ahead of matter.js* register (12 before wave 26-B, 30 after it; 39 at
the review of 2026-11, `make ahead-check` holding every marker at the pin
and at HEAD). The next lift
re-reads each of their markers first, because each names a matter.js
server that was the generated stub at the pin:

- phase 26, camera (waves 26-A1 … 26-A3): `BD-chip-Chime-RulesInServer`,
  `BD-chip-CameraAvSettings-RulesInServer`,
  `BD-chip-CameraAvStreamManagement-RulesInServer`,
  `BD-chip-ZoneManagement-RulesInServer`,
  `BD-chip-WebRtcTransportProvider-RulesInServer`,
  `BD-chip-PushAvStreamTransport-RulesInServer`,
  `BD-chip-TlsCertificateManagement-RulesInServer`,
  `BD-chip-TlsClientManagement-RulesInServer`;
- phase 26, network infrastructure (wave 26-B):
  `BD-chip-WiFiNetworkManagement-RulesInServer`,
  `BD-chip-ThreadNetworkDirectory-RulesInServer`;
- phase 26a, the Thread Border Router: `BD-Thread-TBRM-ChipLogic`,
  `BD-Thread-TND-FromBorderRouter`, `BD-Thread-NetComm-ThreadVariant`;
- phase 26, sensing (wave 26-C): `BD-chip-AmbientContextSensing-RulesInServer`;
- phase 26, Joint Fabric (wave 26-D):
  `BD-chip-JointFabricDatastore-RulesInServer`,
  `BD-chip-JointFabricAdministrator-RulesInServer`, `BD-chip-JCM-Commissioner`;
- phase 27 (wave 27-B): `BD-chip-TimeSynchronization-Features`.

The Thermostat events (TEVT, wave 27-B) have their own register row since
2026-10-29, `BD-chip-Thermostat-Events` (its marker: `ThermostatServer.ts:525`
is the server's only `events.` emit), so a lift re-reads them with the
other markers.
WebRtcTransportRequestor mirrors matter.js and is no register row.

## 0ceb202d → 862ad75f (2026-10-18, milestone 2 phase 14)

57 commits; 16 skipped (7 `[CHIP BUILD]`, 7 version bumps, `ci:` #4787,
`docs:` #4782). Map: `notes/reference/read-matterjs-delta-862ad75f.md`.
Model: one element change, `bridged-node.element.ts:17` — BridgedNode
`composition: "full-family"` (#4735); no cluster, attribute, command, event or
revision moved. Snapshot regenerated in commit `9a6c2fb` (brief BD's branch).

### Device side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4735 | BridgedNode PartsList lists every descendant (`DescriptorServer.ts:227-270`) | **gap → brief BD item 1** | `endpoint/assembler.go:230-238` lists direct parts |
| #4766 | keep-alive DataReport with `suppressResponse: false` so INVALID_SUBSCRIPTION is read on every transport (`ServerSubscription.ts:913-914`) | **gap → register** | `bridge/subscribe.go:447-453` sets `SuppressResponse = true` (the rule this commit reverses) |
| #4742 | a subscription that ends while the node runs is dropped from the store; only `closeForShutdown` keeps it (`Subscription.ts:21-40`, `ServerSubscription.ts:766-812`, SubscriptionsServerTest) | **gap → register** | `im/subscription/manager.go:128-149` keeps the former rule: closed on session close = re-established after restart |
| #4768 | server: another exchange's write of an attribute a chunked write holds is answered BUSY (`InteractionServer.ts:507,526`) | **closed (phase 31)** | `im.WriteHolds` (`im/write.go`), one table per bridge (`bridge/exchange_routing.go` `writeHolds`): BUSY for a concrete write, a wildcard or group write skips the attribute; `im:TestChunkedWriteHoldParityMatterJS` |
| #4775 | KeypadLockout stays as configured or stored; NoLockout only when undefined (`ThermostatUserInterfaceConfigurationServer.ts:30-32`) | **gap → register** | `cluster/thermo/thermostatuserinterfaceconfiguration.go:31-32` resets on every start |
| #4736 | OTA provider: `NewVersion 0` under a known token applies the token's version; another version than the token's is refused Cancelled (`OtaSoftwareUpdateProviderServer.ts:419-458`) | **gap → register** | `cluster/core/ota_provider.go:558-563` checks consent on the raw `NewVersion` only |
| #4745 | a utility component the composing endpoint lists itself counts that endpoint as a candidate (`DeviceTypeConformance.ts:596-614`, Core § 9.2.1) | **gap → register** | `endpoint/devicetype_conformance.go:842-850` iterates the composition scope only |
| #4746 | a condition true only by a `Root` assertion never refuses; the violation is marked `assertedAtRoot` and logged (`DeviceTypeConformance.ts:65-76`, `ConditionAssertions.ts:63-68`) | **closed (phase 31)** | `endpoint/devicetype_conformance.go` `check` re-checks the node endpoint without its Root-asserted conditions and flags `DeviceTypeViolation.AssertedAtRoot`; `DeviceTypeValidator` never refuses a flagged violation and reports it in `DeviceTypeVerdict.FreshAssertedAtRoot`; `endpoint:TestDeviceTypeConformanceServiceParityMatterJS` |
| #4779 | a replaced Fabric shares groups, ACL and ICD state with its replacement | **have it** (by construction) | `handleUpdateNOC` updates the NOC of the session's fabric index in place; ACL, groups and ICD state are keyed by fabric index and untouched |
| #4778 | only a change of the fabric the armed fail-safe owns is deferred; UpdateFabricLabel / SetVIDVerificationStatement under a fail-safe without AddNOC/UpdateNOC are final | **have it** — settled by the OperationalCredentialsServerTest port (ported): `node:TestOperationalCredentialsFinalFabricChange` treats UpdateFabricLabel and SetVIDVerificationStatement under a fail-safe without AddNOC/UpdateNOC as final fabric changes, reported to `Observers.OnFabricUpdated` (review 2026-11 W6-reg-11) |
| #4777 | client answers the priming report of a cancelled subscribe with INVALID_SUBSCRIPTION; the server drops it | **have it, untested** — the priming report's chunk loop waits for the peer's status and aborts the Subscribe on an error status (`bridge/subscribe_dispatch.go`, `awaitChunkStatusResponse`, as matter.js's waitForSuccess); the ServerSubscriptionTest port (partial) has no case that answers a priming report with INVALID_SUBSCRIPTION, so the server side is a test gap (review 2026-11 W6-reg-11) |
| #4765 | an out-of-range vendor ID in a manual code is an invalid payload | **have it** — `secure/setup/decode.go:96-107` validates through `assertValidPayloadIdentity`, which returns the invalid-payload error |
| #4759 | AtomicRequest: request-wide checks before per-attribute statuses | **not applicable** — no atomic-write handler here (thermostat presets, milestone 3; chipdm acknowledges the command) |
| #4737 | WebRTC requestor sessions keyed per peer | **not applicable** — no WebRTC server (milestone 6 stubs) |
| #4734 | ML-DSA attestation certificates skipped at debug | **not applicable** — ML-DSA is not admitted here (`controller/dcl/revocation.go:389-391`); no fetch or store path for such a PAA |
| #4758 | IPv4 multicast membership on the configured interface | **not applicable** — IPv6 only (`transport/udp/multicast.go`) |
| #4731 #4732 #4748 #4749 #4762 #4771 | reference lifetimes, migration hook, WebSocket, console format, transaction lock, cache lifetime | **not applicable** — JS runtime |
| #4738 #4772 #4774 #4776 #4786 #4717 #4788 (harness part) | matter.js test harness | **not applicable** |

### Controller side

| PR | Fix | Verdict | Where |
| --- | --- | --- | --- |
| #4741 | DNS-SD TXT keys match regardless of case, first wins (`DnssdParameters.ts:15-49`, RFC 6763 § 6.4) | **gap → register** | `controller/discovery/name.go:154-162` `Parameters` is a case-sensitive map |
| #4756 | known answers carry their remaining TTL and are left out past half TTL (`DnssdSolicitor.ts:280-291`, RFC 6762 § 7.1) | **gap → register** | `controller/discovery/solicitor.go:368-375` copies records unchanged |
| #4770 | a client node is seeded once BasicInformation and every endpoint of the root's PartsList are in; a root-only peer qualifies (`ClientNodeLifecycle.ts:52-56`) | **gap → register** | `controller/client/config.go:116-118`, `node.go` `evaluateSeeded` keep "more than the root endpoint" |
| #4747 | discovery error kinds (`DeviceNotDiscoveredError`, `DiscoveryIncompleteError`, `NoDiscoveryTransportError`) and an abort that stops every attempt | **gap → register** (API) | `controller/discovery`, `controller/commissioner`: one error, no abort option |
| #4750 | a status the peer sent is told from one produced locally (`ResultStatus.received`) | **gap → register** (API, low) | `controller/interaction/interaction.go:56` `StatusError` has no origin |
| #4752 | a closed sustained subscription never reports itself active | **have it** — settled by the SustainedSubscriptionTest port: `client/interaction:TestSustainedSubscriptionNonICD` "never reports active when closed while the subscribe is in flight and then succeeds" (review 2026-11 W6-reg-11) |
| #4751 | one owner for a client node's event number and data versions | **have it** for the event number — settled by the ClientEventNotificationTest port (partial): `controller/client:TestClientEventNotificationEventNumbersParity` holds one stored event number, never lowered, asked above by a replacement subscription and after a restart; the data versions are the client image's own (the version filters it injects); the per-subscription event number stays `missing` there (review 2026-11 W6-reg-11) |
| #4760 | every ClientNode write reaches the device, the cache follows the device | **have it** — no local cache write exists; the image changes when the peer reports (`controller/client/interact.go:100-110`) |
| #4767 | invoke batching per request options | **not ported** (documented, `controller/interaction/interaction.go:12`); unchanged |
| #4769 | ICD Check-In key refresh kept in step | **not applicable** until milestone 5 (no ICD client) |
| #4733 | controller-side OTA: an update applied without restart fails | **not applicable** until milestone 5 (no controller OTA manager) |
| #4788 | `caseConnectionTimeout` commissioning budget | **have it** — settled by the CaseConnectionTimeoutTest port (ported): `controller/client:TestCaseConnectionTimeoutParity` fails a commissioning whose CASE connection outlasts the budget and refuses a non-finite budget (review 2026-11 W6-reg-11) |
| #4743 | `PeerDescriptor.dataModelRevision` removed | **not applicable** (type only) |
