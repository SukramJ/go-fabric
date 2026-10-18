# Report BH — milestone 2, phase 16: the port of `packages/protocol/test`

Branch `m2/phase-16-protocol` from `origin/main` at d401168. matter.js read
only at the pin (`git -C ../matter.js archive|show 862ad75f`). The
implementing agent split each tranche over parallel porting agents on
disjoint production packages (each fix only in a package its agent owned) and
committed per matter.js file, with the register entry, the regenerated page
and the ratchet in the same commit. "ported" in the tables counts every `it`
of the file the Go tests now cover (the cases ported before this wave
included); "missing" is what the register still lists.

## BH-1 — wire core

26 files, 345 open `it` cases before the tranche: 215 ported, 130 left as
`missing` with a reason in the register. 4 files `ported`, 23 `partial`;
`testparityPlannedRatchet` 134 → 125.

| matter.js file | status | Go test files | ported | missing |
| --- | --- | --- | ---: | ---: |
| `codec/MessageCodecTest.ts` | partial | `transport/message/messagecodec_parity_matterjs_test.go`, `groups/messagecodec_parity_matterjs_test.go` | 17 | 1 |
| `codec/MessagePrivacyTest.ts` | ported | `secure/channel/privacy_parity_matterjs_test.go` | 5 | 0 |
| `events/NonvolatileEventStoreTest.ts` | partial | `im/nonvolatile_event_store_parity_matterjs_test.go` | 3 | 8 |
| `events/VolatileEventStoreTest.ts` | partial | `im/volatile_event_store_parity_matterjs_test.go` | 11 | 9 |
| `interaction/InteractionClientMessengerTest.ts` | partial | `controller/interaction/interaction_client_messenger_parity_matterjs_test.go` | 10 | 21 |
| `protocol/DeviceAdvertiserTest.ts` | partial | `secure/operational/reannounce_parity_matterjs_test.go`, `bridge/advertiser_parity_matterjs_test.go` | 7 | 5 |
| `protocol/ExchangeManagerTest.ts` | partial | `controller/exchange/exchangemanager_parity_matterjs_test.go` | 6 | 4 |
| `protocol/MRPTest.ts` | partial | `controller/exchange/mrp_parity_matterjs_test.go` | 16 | 9 |
| `protocol/MessageChannelTest.ts` | partial | `controller/exchange/messagechannel_parity_matterjs_test.go` | 2 | 2 |
| `protocol/MessageCounterTest.ts` | partial | `groups/messagecounter_parity_matterjs_test.go` | 14 | 5 |
| `protocol/MessageExchangeTest.ts` | partial | `controller/exchange/messageexchange_parity_matterjs_test.go` | 30 | 14 |
| `protocol/MessageReceptionStateTest.ts` | partial | `transport/mrp/messagereceptionstate_parity_matterjs_test.go` | 32 | 1 |
| `protocol/ProtocolStatusCodeTest.ts` | ported | `controller/exchange/protocolstatuscode_parity_matterjs_test.go` | 3 | 0 |
| `protocol/TcpSessionBindingTest.ts` | partial | `bridge/conn_tcp_parity_matterjs_test.go` | 14 | 7 |
| `protocol/interaction/AttributeDataDecoderTest.ts` | partial | `controller/interaction/attribute_data_decoder_parity_matterjs_test.go` | 16 | 1 |
| `protocol/interaction/AttributeDataEncoderTest.ts` | partial | `im/attribute_data_encoder_parity_matterjs_test.go`, `bridge/attribute_data_encoder_parity_matterjs_test.go` | 6 | 1 |
| `protocol/interaction/EventDataDecoderTest.ts` | partial | `controller/interaction/event_data_decoder_parity_matterjs_test.go` | 3 | 2 |
| `securechannel/SecureChannelMessengerTest.ts` | partial | `controller/exchange/securechannelmessenger_parity_matterjs_test.go` | 1 | 4 |
| `session/SecureSessionTest.ts` | partial | `secure/channel/session_parity_matterjs_test.go`, `groups/session_parity_matterjs_test.go` | 9 | 2 |
| `session/SessionIntervalsTest.ts` | partial | `controller/exchange/intervals_parity_matterjs_test.go` | 9 | 4 |
| `session/SessionManagerTest.ts` | partial | `secure/operational/sessionmanager_parity_matterjs_test.go`, `groups/sessionmanager_parity_matterjs_test.go`, `controller/exchange/sessionmanager_parity_matterjs_test.go` | 17 | 19 |
| `session/SessionParametersTest.ts` | partial | `controller/exchange/params_parity_matterjs_test.go` | 13 | 2 |
| `session/secure/CasePairingTest.ts` | ported | `secure/sigma/parity_matterjs_test.go` | 3 | 0 |
| `session/secure/PasePairingTest.ts` | partial | `secure/spake2/parity_matterjs_test.go`, `controller/handshake/pase_parity_matterjs_test.go` | 8 | 2 |
| `transport/tcp/TcpChannelTest.ts` | partial | `transport/tcp/conn_parity_matterjs_test.go` | 24 | 1 |
| `transport/tcp/TcpTransportTest.ts` | partial | `transport/tcp/listener_parity_matterjs_test.go` | 5 | 6 |
| **total** | | | **305** | **130** |

The reason of every remaining `missing` is in the register entry's `reason`.
The large groups: the network-profile MRP margins (20 cases over MRPTest and
MessageExchangeTest), `abandonSubscription` and write chunking (19 cases of
InteractionClientMessengerTest), the event store's persisted events and
delete/close (17), the wall-clock-step cases (9 over SessionManagerTest and
ExchangeManagerTest), and values a Go type cannot hold (out-of-range
counters, a uint8 status 0x7777, a uint16 SAT above 65535, JS value shapes).

### Fixes

| Production file | matter.js (pin lines) | What |
| --- | --- | --- |
| `transport/message/packet.go` | `codec/MessageCodec.ts:429-437` | New `ProtocolHeader.MarshalPayload` (+ `ErrEncodeSecuredExtensions`), the checked encoder that refuses Secured Extensions as `encodePayload` does. |
| `transport/tcp/conn.go` | `transport/tcp/TcpChannel.ts:85-98,183-185,281-296` | A connection the peer closes hands every message completed before the close to the handler before it reports closed (it could drop them); proven by "delivers queued messages then terminates on close". |
| `bridge/conn.go` | `protocol/ExchangeManager.ts:655` | A dropped TCP connection logs one debug line per evicted session. |
| `secure/channel/group.go` | `session/GroupSession.ts:260-269` | `EncryptGroup` builds the nonce from the header's Source Node ID and refuses a disagreeing sender id (`ErrSourceNodeMismatch`). No key material changes. |
| `secure/operational/manager.go` | `session/SessionManager.ts:272-277` | Only a session the peer established runs the per-peer cap check; a session we initiate no longer evicts one of the peer's. |
| `im/eventlog.go` | `events/BaseEventStore.ts:62-67,141-147` | `DropBuffered` persists the current numbering as the ceiling first, so a restart continues at last+1. Same store key, a smaller value. |
| `controller/exchange/exchange.go` | `protocol/MessageExchange.ts:63-77,1234-1251`; `:125-130,794-797,812-825,987-997`; `:485-495,564-617` | BDX retransmission cap 7.4 s; new `SendOptions.AckWaitAbort` (ends only the ack wait); a duplicate of the message a pending BDX reply answers retransmits the reply at once instead of a standalone ack. |
| `controller/interaction/report.go` | `types/src/tlv/TlvArray.ts:146-150`, `interaction/AttributeDataDecoder.ts:310-344`; `interaction/InteractionMessenger.ts:985-1000` | A list chunk at the list's length index grows the list, a null chunk past the end is a no-op, repeated non-list values resolve to the highest data version; a StatusResponse Success in place of a report is a `*StatusError`. |
| `controller/interaction/write.go` | `interaction/InteractionMessenger.ts:103-106,1335-1354,1388-1392` | A write that does not fit one message, or a timed write whose WriteResponse might not, is refused with `ErrRequestTooLarge` before anything is sent. |

All in `CHANGELOG.md` `[Unreleased]` (### Added for the three new exported
names, ### Fixed). None lands in `node/`, `controller/client` or `fabrictest`.

### Register candidates

1. **Group sender node id after UpdateNOC** — `groups/sender.go:208,225`
   captures the node id at creation; `Reload` keeps it and nothing reloads on
   UpdateNOC. matter.js reads it per message (`session/GroupSession.ts:180-205`,
   `ExchangeManagerTest.ts:345-368`). Found by two agents independently;
   outside the owned packages of both.
2. **Empty ICAC in the CASE signed data** — `secure/sigma` `signedDataBytes`
   (`sigma.go:811-822`) drops an ICAC field that is present but empty; the
   CasePairingTest vector carries `300200` and its rebuild does not match.
   matter.js re-encodes the field as present (`session/case/CaseServer.ts:294-299`).
   Signature path of `secure/`: not touched.
3. **PASE failure path** — `bridge/securechannel.go:756-770` always answers
   InvalidParam and has no "still unacked" guard (`PaseServer.cancelPairing`).
4. **Network-profile MRP margins** — `MRP.ts:16-27,79-120,162-190`,
   `MessageExchange.ts:1214-1222` (`marginFor`, `additionalDelay`,
   `localFixedBackoff`): a feature across `transport/mrp` and
   `controller/exchange`, 20 missing cases.
5. **Write chunking across messages** — `InteractionMessenger.ts:1269-1460`
   (about 200 lines); Go now refuses where matter.js chunks.
6. **abandonSubscription / reportAwaitsStatus** —
   `InteractionMessenger.ts:1056-1137`, used at `ClientInteraction.ts:1096`;
   `AckWaitAbort` now exists in `controller/exchange` to build it on.
7. **Exchange closing/closed observables** — `MessageExchange.ts:1095-1115,1160-1185`.
8. **Device advertiser** — the operational record carries the mdns default
   SII/SAI/SAT, not the node's session parameters
   (`DeviceAdvertiser.ts:363-364`, needs a bridge API); no ICD advertisement
   provider (`DeviceAdvertiser.ts:195-197,356-380`); no operational-mode gate
   on refresh (`DeviceAdvertiser.ts:206-209`).
9. **TCP dialer pool** — no per-address connection reuse (`TcpTransport.ts:149-169`).
10. **Event store** — epoch 0 selects chip's 0x10000 where matter.js counts
    block size 0 as 1 (`BaseEventStore.ts:43-51`; `node/node.go` passes 0,
    brief BI's path); no delete-by-number and no plain clear.
11. **Session parameter checks** — local `MaxPathsPerInvoke` 0 not rejected
    (`SessionManager.ts:368-374`); `sessionparams.DecodeFields` masks an
    oversized TLV value (0x10009 → 9) instead of rejecting it.

### Guards (HEAD 5ca3de5, clean tree)

| Guard | Exit |
| --- | --- |
| `make ci` | 0 |
| `make race` | 0 |
| `make testparity-check` | 0 |
| `make cite-check` | 0 (drift 266 → 265, ratchet lowered) |
| `make reachability-check` | 0 (regenerated; unreached 5, unchanged) |
| `make cover-check` | 0 on the rerun; the first run hit the 10-minute timeout in `controller/interaction` `TestLiveSustainedSurvivesRestart` (see Questions) |
| `make chiptool-test` | skipped: the harness is brief BI's (owner decision 0.12); the device-side fixes (session cap, group nonce source, event-number ceiling, TCP close order) are covered by the chip-tool jobs of the PR |

### Differences to the brief

- `SecureSessionTest`, `SessionManagerTest`, `MessageCodecTest`,
  `DeviceAdvertiserTest`, `AttributeDataEncoderTest`, `PasePairingTest` are
  split over two or three Go files, one per package whose subject they test;
  each header names the matter.js file, the register's `go` list names every
  function.
- The `controller/exchange` fix is three behaviours in about 75 logic lines
  (diff +122/−11 with a funlen split) — above "a few dozen"; kept because
  each is one unambiguous matter.js passage proven by its ported case. The
  owner may prefer to split it out.

### Decisions for the owner

- `controller/interaction.Write` refuses every oversized write, also the
  non-timed ones matter.js would chunk (before: an oversized message went out).
- AttributeDataEncoder "chunk array": the bridge splitter fuses
  `chunkAttributePayload` with the messenger's first-element packing
  (`InteractionMessenger.ts:657-664`), so the expectation is REPLACE [29] +
  ADD 40 where matter.js shows [] + ADD 29 + ADD 40.
- §8.9 "rejects" cases are asserted as "exactly one of data/status goes on
  the wire": Go cannot construct both or neither.
- MessageCounter runs on the group data counter's fixed configuration
  (reserve 1000, difference 100 000), the rollover vectors taken at the same
  distance.
- SLEEPY's activeThreshold 0 means "not stated" in Go (4 s fallback); the
  port advances 5 s first.
- "supportedTransports setter updates context" is ported as `Config.TCP`
  at `New` (no setter after construction).
- New exported names: `message.ProtocolHeader.MarshalPayload`,
  `message.ErrEncodeSecuredExtensions`, `exchange.SendOptions.AckWaitAbort`,
  `channel.ErrSourceNodeMismatch`.

### Left out

The `until` entries; nothing else of the tranche's scope.

### Questions

1. *(Fixed in BH-2, 48effe5.)* `TestLiveSustainedSurvivesRestart` (brief BG's migration onto
   `fabrictest`) hung once under the full `-cover ./...` load:
   `Device.Restart` → `ReestablishFormerSubscriptions` waits for the
   priming report's StatusResponse on the site's Fake, which nobody advances
   while `Restart` blocks, so a lost or late answer never times out. 120
   race runs of the test in isolation (8 in parallel) passed. Pre-existing
   by its shape; whether a BH-1 change made it likelier is not proven
   either way.
2. Should the network-profile MRP margins become a planned feature (20 cases)?
3. The wall-clock-step cases need a clock whose wall reading can step apart
   from the monotonic one — a `clocktest` addition, or a by_design entry?

### by_design candidates

- **Zero means unset in session parameters.** A zero field of
  `sessionparams.Parameters` means "not stated" (`secure/sessionparams/sessionparams.go:44-48`),
  so zero, fractional or untyped maxPathsPerInvoke, an SAT above 65535 and
  the object form of supportedTransports (`session/SessionParameters.ts:65-80`,
  `session/SessionManager.ts:368-379`) are unrepresentable rather than
  checked at run time.
- **Monotonic time instead of wall-clock tests.** Go's `time.Time`
  comparisons use the monotonic reading, so a wall-clock step does not reach
  session activity, eviction or peer-loss comparisons that matter.js guards
  separately (`SessionManagerTest.ts:794-924`, `ExchangeManagerTest.ts:178-186`;
  `Session.ts:58-59`, `SessionManager.ts:627-628,696`).
- **Flat client report.** `controller/interaction/report.go` mirrors the
  streaming `action/client/InputChunk.ts:174-245,336-354`; the deprecated
  per-path grouping of `interaction/AttributeDataDecoder.ts:162-181` and
  `EventDataDecoder` (3 missing cases) is not ported.
- **Event number epoch.** `EventLog` epoch 0 is
  `CHIP_DEVICE_CONFIG_EVENT_ID_COUNTER_EPOCH` (0x10000) with an inclusive
  ceiling; matter.js `events/BaseEventStore.ts:43-51,153-162` counts block
  size 0 as 1, defaults to 1000 and stores the exclusive next number. Neither
  reuses a number.

## BH-2 — credentials and transfer

origin/main 9eee86e (PR #163, the owner's `secure/mattercert` TBS fix) was
merged in before the tranche (4aae782; CHANGELOG kept both sides, the
generated reachability and citation artefacts regenerated). Its three
`tbs_der_parity_matterjs_test.go` tests are folded into the
`CertificatesTest.ts` entry's `go` list.

27 files, 390 open `it` cases before the tranche: 295 ported, 95 left as
`missing`. 16 files `ported`, 11 `partial`; `testparityPlannedRatchet`
125 → 108.

| matter.js file | status | Go test files | ported | missing |
| --- | --- | --- | ---: | ---: |
| `advertisement/AdvertisementTest.ts` | partial | `mdns/advertisement_parity_matterjs_test.go` | 1 | 3 |
| `advertisement/ServiceDescriptionTest.ts` | ported | `mdns/service_description_parity_matterjs_test.go` | 2 | 0 |
| `bdx/BdxTest.ts` | partial | `bdx/transfer_parity_matterjs_test.go` | 27 | 3 |
| `bdx/PersistedFileDesignatorTest.ts` | ported | `bdx/sink_parity_matterjs_test.go` | 4 | 0 |
| `certificate/CertificateAuthorityTest.ts` | ported | `controller/ca/certificateauthority_parity_matterjs_test.go` | 20 | 0 |
| `certificate/CertificatesTest.ts` | partial | `secure/mattercert/certificates_parity_matterjs_test.go`, `controller/attestation/certificates_parity_matterjs_test.go`, `controller/commissioner/certificates_parity_matterjs_test.go` | 21 | 27 |
| `certificate/CertificationDeclarationTest.ts` | partial | `controller/attestation/certificationdeclaration_parity_matterjs_test.go` | 5 | 1 |
| `certificate/DeviceAttestationValidatorTest.ts` | ported | `controller/attestation/deviceattestationvalidator_parity_matterjs_test.go` | 43 | 0 |
| `certificate/PqcAttestationCertificatesTest.ts` | partial | `controller/attestation/pqcattestationcertificates_parity_matterjs_test.go`, `secure/attestation/pqcattestationcertificates_parity_matterjs_test.go` | 9 | 32 |
| `common/BleListeningClockTest.ts` | ported | `controller/ble/listening_parity_matterjs_test.go` | 5 | 0 |
| `common/BleScannerTest.ts` | partial | `controller/ble/scanner_parity_matterjs_test.go` | 66 | 1 |
| `common/DiscoveryDataTest.ts` | ported | `controller/discovery/txt_data_parity_matterjs_test.go` | 17 | 0 |
| `common/FailsafeContextTest.ts` | partial | `cluster/core/failsafe_context_parity_matterjs_test.go` | 3 | 1 |
| `common/FailsafeTimerTest.ts` | ported | `cluster/core/failsafe_timer_parity_matterjs_test.go` | 5 | 0 |
| `fabric/FabricAuthorityTest.ts` | partial | `controller/commissioner/fabricauthority_parity_matterjs_test.go` | 5 | 14 |
| `fabric/FabricManagerTest.ts` | partial | `cluster/core/fabric_manager_parity_matterjs_test.go`, `store/fabric_manager_parity_matterjs_test.go` | 16 | 4 |
| `fabric/FabricTest.ts` | partial | `secure/sigma/fabric_parity_matterjs_test.go`, `cluster/core/fabric_parity_matterjs_test.go`, `bridge/fabric_parity_matterjs_test.go` | 8 | 2 |
| `groups/FabricGroupsManagerTest.ts` | ported | `groups/fabric_groups_manager_parity_matterjs_test.go` | 1 | 0 |
| `groups/GroupsMulticastPolicyTest.ts` | ported | `groups/groups_multicast_policy_parity_matterjs_test.go` | 4 | 0 |
| `groups/GroupsTest.ts` | ported | `groups/groups_parity_matterjs_test.go` | 10 | 0 |
| `groups/KeySetsTest.ts` | partial | `groups/keysets_parity_matterjs_test.go` | 12 | 2 |
| `groups/MessagingStateTest.ts` | partial | `groups/messaging_state_parity_matterjs_test.go` | 1 | 5 |
| `ota/OtaImageReaderTest.ts` | ported | `bdx/otaimage/otaimage_reader_parity_matterjs_test.go` | 16 | 0 |
| `ota/OtaImageWriterTest.ts` | ported | `bdx/otaimage/otaimage_writer_parity_matterjs_test.go` | 17 | 0 |
| `thread/BasicTlvCodecTest.ts` | ported | `controller/thread/tlv_parity_matterjs_test.go` | 11 | 0 |
| `thread/MeshCopTlvTypesTest.ts` | ported | `controller/thread/meshcop_parity_matterjs_test.go` | 6 | 0 |
| `thread/OperationalDatasetTest.ts` | ported | `controller/thread/dataset_parity_matterjs_test.go`, `controller/thread/dataset_test.go` | 18 | 0 |
| **total** | | | **353** | **95** |

The large groups of the 95: PQC Phase 1 (32 of PqcAttestationCertificatesTest),
the operational-certificate TLV encoder / X.509-to-TLV parser and the
X.520-DN verification gap (27 of CertificatesTest), the controller's NOC key
rotation (11 of FabricAuthorityTest), the legacy and storage-setter cases of
MessagingState (5).

### Fixes

| Production file | matter.js (pin lines) | What |
| --- | --- | --- |
| `controller/discovery/name.go` | `general/src/net/dns-sd/DnssdParameters.ts:27-46` | TXT keys match regardless of case, the first occurrence winning: a node announcing `sii`/`Sat` is no longer read with MRP defaults. |
| `cluster/core/general_commissioning.go` | `common/FailsafeTimer.ts:33-35` | A re-arm can no longer carry the fail-safe past the cumulative limit; the limit expires it. Device behaviour. |
| `controller/ca/ca.go` | `certificate/CertificateAuthority.ts:157-159,367-372` | New `Config.DetectIntermediateCA` (a stored CA's ICAC decides the tier); the ICAC-mismatch error carries matter.js's text. |
| `controller/commissioner/fabric.go` | `fabric/FabricAuthority.ts:47,139-147`, `types/src/datatype/VendorId.ts:22,44` | `NewFabric` gives an unset admin vendor id 0xFFF1 and refuses one above 0xFFF4 instead of sending it in AddNOC. |
| `groups/groupcast.go` | `groups/Groups.ts:98-99` | `MulticastAddressFor` refuses group id 0 (`ErrInvalidGroupID`). |
| `secure/mattercert/decode.go` | `certificate/kinds/OperationalBase.ts:19-28` | `Decode` refuses a Matter-TLV operational certificate over 400 bytes (also on AddNOC/AddTrustedRoot). Pure refusal; no encoding change. |
| `secure/attestation/files.go` | `certificate/DeviceCertification.ts:87-105` | `LoadChainFiles` refuses a DAC or PAI over 600 bytes (`ErrCertificateTooLarge`). Device behaviour at load time. |
| `bdx/otaimage/otaimage.go` | `ota/OtaImageReader.ts:65-110,246-278`; `ota/OtaImageWriter.ts:13-20,107-115` | `FileOptions.ChecksumType`/`SkipFullChecksum`; an expected checksum with none computed mismatches; `CreateWithChecksum` returns the base64 whole-file checksum. |
| `bdx/send.go`, `serve.go`, `session.go` | `bdx/bdx-session-initiator.ts:128-135,167-178,224-257` | `SendOptions.StartOffset` stated in the SendInit; `Result.StartOffset`. No BDX encoding change. |
| `controller/ble/listening.go` (new) | `common/BleListeningClock.ts:9-37` | `ListeningMeter`, a Central's scan-time meter. |
| `controller/ble/scanner.go` | `common/BleScanner.ts:110-114,116-122,160-225,298-320,654-673` | `ForgetCommissioned`; `ErrScanTransitionTimeout` (10 s); scan transitions in queue order (Close's stop last); a discovery on a closed scanner returns the stored devices (`ErrScannerClosed` deprecated, README row added). |
| `controller/thread` (new package) | `thread/BasicTlvCodec.ts`, `meshcopTlvTypes.ts`, `OperationalDataset.ts`, `SecurityPolicy.ts`, `errors.ts` | The Thread operational dataset and MeshCoP TLV codec, about 550 lines TS ported, 97.3 % covered (floor 97 added). |

### Register candidates

1. **Standard X.520 DN attributes dropped** — `secure/mattercert` `Decode`
   and `buildDN` carry only DN tags 17-22, so a chain whose DN carries
   countryName, organizationName or commonName fails ECDSA verification (the
   Google set of `TestCertificates.ts`: RCAC self-signature, ICAC and NOC).
   matter.js `certificate/kinds/definitions/operational.ts:69-110`,
   `Certificate.ts:899-1058`. Changes the TBS DER; interop-relevant.
2. **NOC issuer not chosen by authorityKeyIdentifier** (`Noc.ts:170-186`):
   a root-signed NOC presented with an ICAC is refused.
3. **Root fabric id not checked against the NOC's** (`Noc.ts:119-125`,
   `Icac.ts:107`): the `Verifier` holds only the root key (API change).
4. **No Matter-TLV encoder and no X.509-to-TLV parser** for operational
   certificates (recode, fromAsn1, external, extensions, TLV-order cases).
5. **Device CSR subject** — matter.js O=CSR as UTF8String
   (`Certificate.ts:264-277`); `cluster/core/operational_credentials.go:1480-1486`
   uses CN=CSR, `commissioning/csr.go:84-86` an empty subject. A NOCSR wire
   difference.
6. **Attestation certificates kept as received** — issuer DN vendor id not
   read, never re-encoded (`Certificate.ts:379-392`).
7. **One 600-byte attestation limit** — matter.js allows 10 240 for PQC
   (`common.ts:19`, `Certificate.ts:617`).
8. **No provisional Certification Declaration** generator
   (`CertificationDeclaration.ts:165-181`).
9. **PQC Phase 1** (`AttestationCertificates.ts:22-44`, `Certificate.ts:596-760`):
   even off (matter.js's default) matter.js classifies ML-DSA certificates
   as `MlDsaAttestationDisabledError` at DEBUG; Go refuses by size with a
   generic error. Go 1.27's standard library has ML-DSA (`crypto/mldsa`).
10. **No product description in `LoadChainFiles`** (`DeviceCertification.ts` `#validateCertification`).
11. **Controller NOC key rotation** (`FabricAuthority.ts:88-99,181-206`;
    11 cases) and the `updateOperationalCert` fabric-id guard
    (`Fabric.ts:579-585`); `buildFabric`/`loadFabric` do not check a stored
    NOC's fabric id against the record.
12. **PASE close does not expire the fail-safe** (`common/FailsafeContext.ts:76-86`;
    `ExpireFailSafe` is reached only from RevokeCommissioning,
    `bridge/commissioning_window.go:727`). Crosses `secure/channel` and `bridge`.
13. **Group sender node id after UpdateNOC** (BH-1 candidate 1) — still
    open: no BH-2 test proves it either.
14. **Per-message BDX debug log context** (`BdxMessenger.ts:140-200`,
    `BdxSession.ts:207-217`): the Go messenger has no logger.
15. **`ForgetCommissioned` not wired into the commissioner** —
    `controller/commissioner/commissioner.go:516` says matter.js has no
    forgetCommissionedDevice; its BLE scanner does
    (`ControllerCommissioner.ts:325-340,723`).
16. **Thread dataset parsing elsewhere** — `controller/commissioner/network.go`
    (`threadNetworkNameOf`) reads NETWORK_NAME with its own walk and does
    not reject an undecodable dataset as matter.js's
    `resolveThreadNetworkName` does; `examples/reference-controller/cli/pair.go:65-70`
    hex-decodes `--thread-dataset` without matter.js's whitespace tolerance.
    Both could use `controller/thread`.

### Guards (HEAD 06c4dc4, clean tree; cover-check rerun at 48effe5)

| Guard | Exit |
| --- | --- |
| `make ci` | 0 |
| `make race` | 0 |
| `make testparity-check` | 0 |
| `make cite-check` | 0 (drift 265, two new drifts reworded) |
| `make reachability-check` | 0 (unreached 5; the deprecated `ErrScannerClosed` whitelisted) |
| `make cover-check` | 0 at 48effe5. At 06c4dc4 it hit `TestLiveSustainedSurvivesRestart`'s hang again (BH-1 Question 1); fixed in 48effe5 (the node restarts under `Site.Run`, so the Fake moves while `Start` waits for the priming report's answer; 80 runs on one CPU each, 16 in parallel, without a hang — the same stress hangs at d401168). One rerun hit the wall-clock `secure/operational` `TestStartReaper_ActiveSessionSurvivesSweeps` (untouched by BH, `time.Sleep`-paced); the next run was green. |
| `make chiptool-test` | skipped: the harness is brief BI's (decision 0.12); the device-side fixes (cumulative fail-safe limit, 400-byte NOC refusal, 600-byte DAC/PAI refusal at load) are covered by the PR's chip-tool jobs |

### Differences to the brief

- `controller/thread` is a new package (the brief names it as the place of
  the thread tests, the Go subject did not exist): about 550 lines of
  matter.js ported, not a "few dozen lines" fix. Decision for the owner.
- `controller/interaction/live_test.go` (brief BG's test) changed to stop
  the hang that blocked `cover-check`; no production change.
- `README.md` deprecation table gains the `ErrScannerClosed` row.

### Decisions for the owner

- **Expectations bent to Go's surface, counted as ported:** the PAI
  "public key unreadable" case asserts the same check
  (CertificateUnparseable) with Go's step-1 message, because `crypto/x509`
  reads the key while parsing; FailsafeTimer's cumulative vector is scaled
  ×450 because the server floors window and limit at 900 s; the TLV type
  range and SecurityPolicy range cases become the `uint8`/`uint16` field
  types; "encode intermediate certificate" counts though no set carries an
  ICAC ASN.1 at the pin; "rejects ML-DSA certificates where only
  traditional…" counts though its `Noc.fromAsn1` half has no Go subject.
  Move any of them to `missing` if that reads as an adjusted expectation.
- Gap subtests of `CertificatesTest` are `t.Skip("register gap: …")` from
  a `certificatesGaps` map; their `it` names stay in `missing`.
- KeySets `forId`/`allKeysForId` run on key set 1 (the manager never loads
  key set 0, the IPK); a slot is identified by its start time.
- FabricManager "shares groups, access control and ICD state…" is ported
  with a vacuous ICD part.
- The existing `TestParityFabricManager_Allocate*` (cluster/core) test a
  fake store and `AllocateTableFull` is a `t.Skip`; the new
  `store:TestFabricManagerDetermineNextFabricIndex` covers them on SQLite.
- The existing `TestParityMatterJS_CompressedFabricID_HKDF` asserts
  `d559af361549a9a2` (ROOT_3, fabric 1), not FabricTest's
  `0x6cf78388a7e78e3d` — yet the register counted "generates the correct
  compressed Fabric ID" as covered before this wave. Please check.
- New exported API (additive): see `CHANGELOG.md` ### Added.
- `controller/thread` `Redact` keeps `Raw` (the source bytes, secrets
  included) as matter.js does (`OperationalDataset.ts:100-102`), although
  matter.js documents the result "safe to log" — an upstream report.
- `ErrScannerClosed` is no longer returned (deprecated, removable in
  `v0.10.0` at the earliest).

### Left out

Nothing of the tranche's scope.

### Questions

1. Should PQC Phase 1 (at least the off-mode classification) and the
   controller's NOC rotation become planned work?
2. Candidate 1 (X.520 DN attributes) can block real controllers' chains;
   prioritise?

### by_design candidates

- **No TLV re-encoding or ASN.1 parsing of operational certificates.** The
  device keeps the TLV it received (`Certificate.Raw`), `controller/ca`
  encodes from fields; matter.js `Certificate.ts` `asSignedTlv`/`fromAsn1`,
  `Noc.ts:24-35`. Costs the cases of candidate 4.
- **Attestation certificates kept as received DER** (`Certificate.ts:379-392,899-1058`).
- **IPK as a single key.** `store/identity.go` holds one IPK, Group Key
  Management refuses key set 0 (`cluster/core/group_key_management.go:883`),
  `groups/manager.go` skips it; matter.js's epoch selection
  (`groups/KeySets.ts:113-120`, §4.14.2.6.1) cannot occur.
- **No legacy group counters** to migrate (`groups/MessagingState.ts:41-60`,
  `SessionManager.ts:984-985`; `groups/counter.go`).
- **PAI key read at parse time** (`DeviceAttestationValidatorTest.ts:236-274`):
  same check, earlier step, other message.
- **The BDX initiator takes an Exchange** (`BdxTest.ts:1691-1724`;
  `bdx/doc.go`): the session stays with the caller.
- **DELAY_TIMER as uint32** where JavaScript's `<<` makes it a signed int32
  (`thread/OperationalDataset.ts:167-171`); the wire bytes are identical.
- **Wall-clock steps** (`BleScannerTest.ts:775-796`, and BH-1's
  SessionManager/ExchangeManager cases): `clocktest.Fake` has one reading;
  either a by_design entry or a `clocktest` wall-only step.

## BH-3 — controller side

24 files, 382 open `it` cases before the tranche: 236 ported, 146 left
(`missing` or, for four files, the whole file still `planned`). 8 files
`ported`, 12 `partial`, 4 stay `planned`; `testparityPlannedRatchet`
108 → 101.

| matter.js file | status | Go test files | ported | missing |
| --- | --- | --- | ---: | ---: |
| `action/client/ClientInteractionInvokeTest.ts` | partial | `controller/interaction/client_interaction_invoke_parity_matterjs_test.go` | 14 | 11 |
| `action/client/ClientInteractionSubscribeTest.ts` | planned |  | 0 | 7 |
| `action/client/InputChunkTest.ts` | ported | `controller/interaction/input_chunk_parity_matterjs_test.go` | 18 | 0 |
| `action/client/ReadScopeTest.ts` | planned |  | 0 | 6 |
| `action/client/subscription/ClientSubscriptionsTest.ts` | partial | `controller/interaction/client_subscriptions_parity_matterjs_test.go` | 4 | 2 |
| `action/client/subscription/SustainedSubscriptionTest.ts` | partial | `controller/interaction/sustained_eventnumber_parity_matterjs_test.go`, `controller/interaction/sustained_parity_matterjs_test.go` | 18 | 37 |
| `action/request/InvokeTest.ts` | partial | `controller/interaction/invoke_request_parity_matterjs_test.go` | 10 | 5 |
| `action/request/ReadAttributePathsTest.ts` | ported | `im/read_attribute_paths_parity_matterjs_test.go` | 6 | 0 |
| `action/request/ReadTest.ts` | partial | `controller/interaction/read_request_parity_matterjs_test.go` | 16 | 4 |
| `action/request/SubscribeTest.ts` | ported | `controller/interaction/subscribe_request_parity_matterjs_test.go` | 21 | 0 |
| `action/request/WriteTest.ts` | partial | `controller/interaction/write_request_parity_matterjs_test.go` | 16 | 2 |
| `mdns/CommissionableMdnsScannerTest.ts` | ported | `controller/discovery/scanner_parity_matterjs_test.go` | 17 | 0 |
| `mdns/MdnsServerTest.ts` | partial | `mdns/host_responder_parity_matterjs_test.go` | 35 | 10 |
| `mdns/MdnsTest.ts` | partial |  | 1 | 6 |
| `peer/CommissioningConnectionTest.ts` | ported | `controller/commissioner/connection_parity_matterjs_test.go` | 27 | 0 |
| `peer/NetworkProfileTest.ts` | planned |  | 0 | 12 |
| `peer/PeerAddressMonitorTest.ts` | partial | `controller/peer/peer_address_monitor_parity_matterjs_test.go` | 13 | 8 |
| `peer/PeerConnectTimeoutTest.ts` | partial | `controller/peer/peer_connect_timeout_parity_matterjs_test.go` | 4 | 1 |
| `peer/PeerConnectionErrorHandlerTest.ts` | partial | `controller/peer/peer_connection_error_handler_parity_matterjs_test.go`, `controller/peer/helpers_test.go` | 12 | 5 |
| `peer/PeerConnectionFallbackRetirementTest.ts` | ported | `controller/peer/peer_connection_fallback_retirement_parity_matterjs_test.go` | 14 | 0 |
| `peer/PeerConnectionKickTest.ts` | partial | `controller/peer/peer_connection_kick_parity_matterjs_test.go` | 27 | 1 |
| `peer/PeerEstablishmentProgressTest.ts` | ported | `controller/peer/peer_establishment_progress_parity_matterjs_test.go` | 5 | 0 |
| `peer/PhysicalDevicePropertiesTest.ts` | planned |  | 0 | 29 |
| `peer/ThreadNetworkNameResolutionTest.ts` | ported | `controller/commissioner/threadnetworkname_parity_matterjs_test.go` | 7 | 0 |
| **total** | | | **285** | **146** |

**Four files stay `planned`** (no Go test could be written inside BH's
paths; each register `reason` says why):

- `peer/PhysicalDevicePropertiesTest.ts` (29) and `action/client/ReadScopeTest.ts`
  (6): the only Go subjects are `controller/client/properties.go` and
  `controller/client/scope.go`, brief BI's path. Test-only ports there
  would need no production change.
- `peer/NetworkProfileTest.ts` (12): matter.js `NetworkProfiles`
  (throttle semaphores, per-medium MRP margins, connect/probe
  sub-profiles; 372 lines) has no Go counterpart — `controller/peer/doc.go`
  and `controller/exchange/mrp.go:62-65` scope the controller to one LAN.
  Together with BH-1's 20 margin cases this is one feature.
- `action/client/ClientInteractionSubscribeTest.ts` (7): every case needs
  `ClientInteraction.close` over the in-flight set, the abandoned-subscribe
  termination or the client BDX (`ClientInteraction.ts:264-297,1096,1185-1215,1260-1300`).

### Fixes

| Production file | matter.js (pin lines) | What |
| --- | --- | --- |
| `transport/mrp/retransmit.go` | `protocol/MRP.ts:162-190` | `BackoffDuration` saturates instead of overflowing to `MinInt64` from transmission 52 on; `controller/exchange`'s `min()` against the cap then gave a negative wait and an unbounded attempt retransmitted in a tight loop after about 100 minutes. Found by the PeerConnectTimeoutTest port; `TestBackoffDurationSaturates`. Device side: `bridge` uses the same function, bounded by MaxRetransmissions. |
| `controller/interaction/sustained.go`, `subscribe.go` | `action/client/subscription/SustainedSubscription.ts:105-126,339-384` | A sustained subscription counts the highest delivered event number (`Subscription.MaxEventNumber`, new) and resubscribes above it, replacing the request's and its RefreshRequest's filters; new `SubscribeRequest.MaxEventNumberChanged`, called after Updated and before the report's StatusResponse, its error logged (warn once, then debug, `#logLocalFailure` :366-377). The hook half was added by the implementing agent after the porting agent named it a candidate. |
| `controller/interaction/invoke.go` | `action/request/Invoke.ts:114`; `interaction/InteractionMessenger.ts:1210-1218,1454-1478`, `action/client/ClientInteraction.ts:528` | A `TimedTimeout` alone marks the invoke timed; new `InvokeOptions.SuppressResponse` (ack-only invoke). |
| `controller/interaction/handler.go` | `action/client/subscription/ClientSubscriptions.ts:156-158` | A panic in the `ReportStarted` callback is logged and contained instead of crashing the inbound report handler. |
| `mdns/service.go` | `mdns/MdnsServer.ts:328-351` | The MAC-derived default host name stays while its interface still carries the MAC, skips a zero MAC, and is kept when the interfaces cannot be listed (about 50 lines). Device behaviour only when interfaces reorder or come and go. |
| `controller/commissioner/connection.go` | `peer/CommissioningConnection.ts:64` | `connectionConfig.delay` is a pointer (nil = the 10 s default, 0 = no stagger), so the ported stagger cases can set 0. Production never sets it; no CHANGELOG line. |
| `controller/peer/connection.go` | `peer/PeerConnection.ts:568-584,698-702` | Pure refactors (`flagsUnreachable`, `kickGate`, `restartAllowedLocked`) so the cases reach the gates; no behaviour change. |

### Register candidates

1. **`ClientInteraction.close`, in-flight tracking, abandoned-subscribe
   termination, client BDX** (`ClientInteraction.ts:264-297,1096,1185-1215,1260-1370`)
   — ClientInteractionSubscribeTest (7) and two invoke cases.
2. **Multi-command batched invoke** with commandRef reconciliation,
   NoCommandResponse synthesis and splitting by maxPathsPerInvoke
   (`ClientInteraction.ts:520-640,745-1000`) — 8 cases.
3. **Custom interaction model revision** on Read/Write/Invoke
   (`Read.ts:40`, `Write.ts:67`, `Invoke.ts:102`).
4. **DelayReportData sending** (forward feature; 2 cases).
5. **Network profiles** (`peer/NetworkProfile.ts:78-245,347-371`) — 12 + BH-1's 20 cases.
6. **Sleeping-LIT-ICD branches** — `PeerAddressMonitor.ts:143-172` (4),
   SustainedSubscription's ICD await mode (milestone 5 phase 24).
7. **Commissioning NoSharedTrustRoots handler** — an inline closure in
   `controller/commissioner/commissioner.go:549-557`; a named function
   would make its 5 PeerConnectionErrorHandler cases portable.
8. **Thread dataset rejection in `threadNetworkNameOf`** — matter.js
   `ControllerCommissioningFlow.ts:264-288` falls back to the supplied name
   when `OperationalDataset.decode` throws; Go walks without validating
   (dataset `0101ff03054d794e6574` with "OtherNet": matter.js "OtherNet",
   Go a mismatch error). About ten lines onto `controller/thread`; no
   ported case shows it.
9. **Late-session close cause race** in `controller/commissioner/connection.go`
   `launch` (`CommissioningConnection.ts:140-151`): a three-line fix, no
   deterministic test.
10. **Bootstrap read events** (`SustainedSubscription.ts:187-204`): the
    host hook hands no reports back.
11. **mDNS** — TC continuation merge (`MdnsServer.ts:446-503`); subtype
    PTR answers without SRV/TXT additionals and unsplit oversized
    announcements (grandcat/zeroconf builds those).
12. **`ForgetCommissioned`** wiring (BH-2 candidate 15) stays open.

### Guards (HEAD df01098, clean tree)

| Guard | Exit |
| --- | --- |
| `make ci` | 0 |
| `make race` | 0 |
| `make testparity-check` | 0 |
| `make cite-check` | 0 (drift 265) |
| `make reachability-check` | 0 (unreached 5) |
| `make cover-check` | 0 |
| `make chiptool-test` | skipped: harness is BI's (decision 0.12); the device-side moves (mDNS host-name choice, MRP backoff saturation past transmission 52) are covered by the PR's chip-tool jobs |

### Differences to the brief

- Four entries stay `planned` (above); the protocol tree therefore ends
  BH-3 above the brief's "ported or until" goal for these files.
- `transport/mrp` and `controller/interaction/subscribe.go` were changed by
  the implementing agent after their porting agents finished (the backoff
  overflow, the hook field).

### Decisions for the owner

- `controller/interaction` uses the scripted peer, not `fabrictest`, for
  the sustained and request-builder ports (unit tests of the loop and the
  wire request, as the package's existing tests are).
- `RefreshRequest`'s node-wide `EventMin` (from `controller/client`) is
  replaced by the subscription's own max+1 once it delivered an event, as
  matter.js does; it can drop below the node's number if the host read
  higher events another way.
- Invoke sends interaction model revision 12; matter.js's `Invoke()`
  defaults to `SessionParameters.fallbacks.interactionModelRevision` = 11
  (`Invoke.ts:102`, `SessionParameters.ts:131`; Read/Write use 12,
  `Specification.ts:124`). No matter.js test pins it; not changed.
- `ConnectOptions.ConnectionTimeout`: 0 = default, negative = forever; no
  Instant (`Peer.ts:466-475`, one case missing).
- The status origin "local error" maps to `ErrNoResponse`; the two abort
  cases share Go's single context.
- The "settles the delivery … after maxEventNumberChanged settled" case
  asserts that no StatusResponse leaves while the hook runs.
- `PeerAddressMonitor` coalescing cases count goroutines blocked in
  `(*monitor).verify` through `runtime.Stack` (non-parallel test).
- Eight existing `TestParityMdnsServer_*` tests in the MdnsServerTest
  entry's `go` list are `t.Skip` anchors that cover nothing.

### Left out

The `until` entry `PeerConnectionSocketOwnershipTest.ts`; the four
`planned` files above.

### Questions

1. May the PhysicalDeviceProperties (29) and ReadScope (6) ports go into
   `controller/client` as test-only files (this wave or BI's), or do they
   get an `until`?
2. Network profiles: a feature brief, `until`, or `excluded`?

### by_design candidates

- **One network on the controller** (`peer/NetworkProfile.ts:78-245`;
  `controller/peer/doc.go:15-17`, `controller/exchange/mrp.go:62-65`):
  additive MRP and BDX margins are 0, no per-medium exchange semaphore.
- **Connection timeout without Instant** (`PeerConnectTimeoutTest.ts:58-63`,
  `Peer.ts:466-475`): Go's zero value means "unset".
- **Single-command, numeric request builders** (`InvokeTest.ts:178`,
  `ReadTest.ts:286`, `WriteTest.ts:284`): no zero-argument or name-based forms.
- **Report as one value per kind** (`action/client/InputChunk.ts:166-432`)
  and **one ReportStarted / ActiveChanged callback** instead of observables
  (`ClientSubscriptions.ts:156-158`).
- **No `_matterd._udp` commissioner advertisement** (`MdnsTest.ts:546-713`).

## BH-4 — DCL

10 files, 331 open `it` cases before the tranche: 238 ported, 93 left (45
of them the OTA image store, 39 the vendor information service). 4 files
`ported`, 4 `partial`, 2 stay `planned`; `testparityPlannedRatchet`
101 → 99.

| matter.js file | status | Go test files | ported | missing |
| --- | --- | --- | ---: | ---: |
| `dcl/DclCertificateServiceRevocationTest.ts` | ported | `controller/dcl/revocation_parity_matterjs_test.go` | 32 | 0 |
| `dcl/DclCertificateServiceSeedTest.ts` | partial | `controller/dcl/snapshot/seed_parity_matterjs_test.go` | 11 | 2 |
| `dcl/DclCertificateServiceTest.ts` | partial | `controller/dcl/certstore_parity_matterjs_test.go` | 82 | 2 |
| `dcl/DclClientRevocationTest.ts` | ported | `controller/dcl/client_revocation_parity_matterjs_test.go` | 13 | 0 |
| `dcl/DclClientTest.ts` | ported | `controller/dcl/client_parity_matterjs_test.go` | 25 | 0 |
| `dcl/DclConfigTest.ts` | ported | `controller/dcl/config_parity_matterjs_test.go` | 9 | 0 |
| `dcl/DclCrlAuthenticationTest.ts` | partial | `controller/dcl/crlauthentication_parity_matterjs_test.go` | 53 | 5 |
| `dcl/DclOtaUpdateServiceTest.ts` | partial | `controller/dcl/ota_parity_matterjs_test.go` | 30 | 45 |
| `dcl/DclVendorInfoServiceSeedTest.ts` | planned |  | 0 | 10 |
| `dcl/DclVendorInfoServiceTest.ts` | planned |  | 0 | 29 |
| **total** | | | **255** | **93** |

Two vendor-information files stay `planned`: matter.js
`dcl/DclVendorInfoService.ts` (497 lines) with `SeedTypes.ts` has no Go
counterpart (`eligibility/vendor_name.go` is an embedded name table, not a
port). The ledger read it would build on, `Client.Vendors`, now exists.

**Late addition to BH-3** (cab4f5c): the commissioner's CASE error handler
is the method `handleNoSharedTrustRoots` (`ControllerCommissioner.ts:701-708`),
so three of PeerConnectionErrorHandlerTest's five commissioning cases are
ported (`controller/commissioner:TestPeerConnectionErrorHandlerNoSharedTrustRoots`);
the entry is 15 ported / 2 missing (the full-flow pair cannot reach
`controller/peer`'s harness without an import cycle).

### Fixes and added API

| Production file | matter.js (pin lines) | What |
| --- | --- | --- |
| `controller/dcl/revocation.go` | `dcl/DclCertificateService.ts:706-710` | `IsRevoked` refuses with `ErrStoreClosed` while and after the store closes, instead of answering "not revoked" for a lookup it never ran. Added by the implementing agent after the porting agent named it. |
| `controller/dcl/revocation.go`, `certstore.go` | `DclCertificateService.ts:328-330,640-687,713-724,1767-1800` | Revocation information from outside the DCL: `InstallRevocations`, `InstalledRevocations`, `ParseRevocationSet`, `CertificateOptions.Revocations`, `ErrInvalidRevocationSet`; consulted by `IsRevoked` before the ledger and in an offline store; serials matched without leading zeros. About 230 lines, in memory only. |
| `controller/dcl/certstore.go` | `DclCertificateService.ts:387-394,400-407,433-480,556-559,604-617,747-812` | `Certificate`, `CertificatePEM`, `CertificateDER` (with the self-heal `PAA` now uses), `GetOrFetchCertificate`, `DeleteCertificate`, `GetCertificateOptions.ConsiderTestCertificates`, `ErrCertificateNotFound`. About 180 lines. |
| `controller/dcl/client.go`, `types.go` | `dcl/DclClient.ts:229-265,305-321,360-381` | `Client.ComplianceInfo` (vid/pid/version/program checked, schema 0 and 1) and `Client.Vendors` (paginated, schema 0) with their records. About 90 lines. |
| `controller/dcl/ota.go` | `dcl/DclOtaUpdateService.ts:747-752,771-781` | An applicable update carries the specification version of its "matter" compliance record (the version record's when it states none); one more ledger GET per applicable version, a failed lookup logged at debug. Goes through `Client.ComplianceInfo` (979b837 removed the porting agent's second parser). |

No change in `crl.go`: every CRL-authentication expectation already held.

### Register candidates

1. **DCL vendor information service** (`DclVendorInfoService.ts`, 497
   lines; 39 cases).
2. **OTA image store** (`DclOtaUpdateService.ts`: blob storage "ota", file
   names, find/delete, force download, `PersistedFileDesignator`, stored
   specification version, migration, offline; 45 cases) — or a by_design
   entry, below.
3. **PQC Phase 1 for CRLs** (5 ML-DSA cases; BH-2 candidate 9).
4. **Asynchronous store construction** — `OpenCertificateStore` returns
   after its first update, so "closed during the startup fetch" has no handle.
5. **Seed test kind** — the snapshot is production-only; no
   `fetchTestCertificates` for seeded test entries (2 cases).

### Guards

HEAD 32d54ed: `make race`, `make testparity-check`, `make cite-check` (drift 265), `make reachability-check` (unreached 5) and `make cover-check` exit 0; `make ci` exit 2 on `TestLiveLivenessTimeout`'s "TempDir RemoveAll cleanup: directory not empty" — a `fabrictest` cleanup-order race (about 1 in 30 runs, the same at d401168): the device stores lived in a `t.TempDir()` made after `NewSite` registered `Close`, so the removal ran while a node still wrote. Fixed in 800fc6b (one store directory per site, made before `Close` is registered; 100 runs clean); then at HEAD 800fc6b (report and CHANGELOG uncommitted):

| Guard | Exit |
| --- | --- |
| `make ci` | 0 |
| `make race` | 0 |
| `make cover-check` | 0 |
| `make testparity-check`, `cite-check`, `reachability-check` | 0 at 32d54ed; 800fc6b changes no register entry, citation or exported identifier |
| `make chiptool-test` | skipped: harness is BI's (decision 0.12); BH-4 moved no device behaviour |

The `fabrictest` change is in brief BI's path; it changes no API (named here as the brief asks).

### Differences to the brief

- New API in `controller/dcl` (about 500 lines over three files) rather
  than "a few dozen lines": each piece is a public matter.js method its test
  file exercises, with no wire, store-schema or `secure/` change. The owner
  may prefer to split it out or to keep the cases `missing`.

### Decisions for the owner

- Offline is `Client == nil`; "asks the DCL nothing" holds by construction.
- A whitespace-only issuer name in an installed set decodes to no issuer
  and so matches every issuer (revokes more); matter.js keeps issuer `""`,
  which matches nothing.
- "Bytes" key-id cases go through lowercase hex strings (the Go getters
  take strings; the attestation surface takes bytes).
- Where matter.js's GitHub default is on, the tests set
  `FetchGitHubCertificates: true` (by_design BD-Matter-DCL-OptIn).
- The two OTA download cases that also check the stored file name are
  counted as ported without that check (the store is the host's).
- ML-DSA CRL cases with pqc-phase-1 on stay missing even where Go refuses
  as matter.js does, since Go refuses all ML-DSA.

### Left out

Nothing beyond the two `planned` vendor files.

### Questions

1. The vendor information service and the OTA image store: port as
   features, or by_design?

### by_design candidates

- **BD-Matter-DclOta-HostStore**: matter.js `DclOtaUpdateService` keeps its
  own image store on blob storage "ota"; Go's `dcl.OTAVersions` answers the
  ledger questions only, the catalogue stays with the host behind
  `cluster/core.OTAImageSource` (as `BD-Matter-OTAProvider-HostPorts`).
- **Production-only seed**: the snapshot is a separate reviewed PAA set
  layered by `attestation.Trust`, not seeded into the online store
  (`DclCertificateService.ts:1370-1393`).
- **ML-DSA CRL signer refusal reason**: matter.js
  `MlDsaAttestationDisabledError` while pqc-phase-1 is off
  (`AttestationCertificates.ts:30-38`); Go refuses through the 600-byte
  `parseCert` limit and the ecdsa-only `verifyCRLSignature`.

## Summary of the wave

| Tranche | Files | Open before | Ported | Left | Planned after |
| --- | ---: | ---: | ---: | ---: | ---: |
| BH-1 wire core | 26 | 345 | 215 | 130 | 0 |
| BH-2 credentials and transfer | 27 | 390 | 295 | 95 | 0 |
| BH-3 controller side | 24 | 382 | 239 | 143 | 4 |
| BH-4 DCL | 10 | 331 | 238 | 93 | 2 |
| **total** | **87** | **1 448** | **987** | **461** | **6** |

Register for `packages/protocol/test` after the wave: 32 `ported`, 51
`partial`, 18 `excluded`, 13 `planned` (the 7 with `until` plus the 6
above); `testparityPlannedRatchet` 134 → 99 overall (the node trees are
untouched). The brief's target of 7 `planned` in this tree is missed by
the six files named in BH-3 and BH-4, each waiting on an owner decision.
