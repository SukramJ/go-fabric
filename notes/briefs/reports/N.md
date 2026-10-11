# Report — brief N: BDX and the OTA Software Update Requestor

Branch `wave5/ota` (from `origin/main` 07d9622), pushed, no PR. Implementer:
Opus. matter.js HEAD 9840bb92 (2026-10-11); connectedhomeip read at the
harness pin 6170af84.

## What was built

| Commit | Content |
| --- | --- |
| 31e7a4e | `cluster/spec/otasoftwareupdateprovider` generated (clustergen list + comment): the provider's QueryImage / ApplyUpdateRequest / NotifyUpdateApplied payloads, used client-side only. |
| c185525 | `bdx/` — message codecs for all ten BDX messages and the BDX StatusReport (`messages.go`, `errors.go`), `Messenger` over a transport-neutral `Exchange` interface with the 5-minute idle timeout (`messenger.go`), `Receive` = BdxClient.asReceiver → BdxSession with the initiator/receiver negotiation and both inbound flows (`session.go`), `FileDesignator` + BDX URI (`designator.go`), `Sink` / `FileSink` (temp file renamed into place, `sink.go`). `bdx/otaimage` — OtaImageHeader / OtaImageReader (header, file, extractPayload) / OtaImageWriter. Fixture generator `notes/parity/matter/generate-bdx-wire-fixtures.ts` and `bdx/testdata/bdx-wire-fixtures.json`. |
| 185bd32 | `store`: table `matter_ota_default_providers` (fabric-scoped, FK cascade), `ListDefaultOTAProviders` / `ReplaceDefaultOTAProviders` (the ACL pattern), the requestor's other persisted state (activeOtaProviders, updateInProgressDetails) as a settings key. |
| c996e19 | `im`: `MarshalInvokeRequestTLV`, `UnmarshalInvokeResponseTLV` (client side). |
| 94ac52f | `cluster/core`: the requestor (`ota_software_update_requestor.go` surface/lifecycle, `ota_requestor_query.go` query/download/apply), ports `OtaProviderConnector` / `OtaProviderSession` / `OtaApplier` / `OtaRequestorStore`, `OtaRequestorDeviceType()`, `DecodeDefaultOTAProvidersWrite`. `bridge`: `ota_provider_link.go` — `(*Bridge).ConnectOtaProvider` (ADR 0008's `connectPeer`, bounded by matter.js's 90 s defaultConnectionTimeout), one Invoke per exchange, a secure initiated exchange implementing `bdx.Exchange`; the receive hook; the write decoder case. |
| 6f49131 | `BasicInformation.OtaNodeInformation` / `SetSoftwareVersion`; reference daemon: requestor on the root (DeviceTypeList gets OtaRequestor rev 1 from the schema), `ota.go` applier, `--ota-dir`, `--ota-harness`, commissioning-complete → `Commissioned()`, fabric removal → `FabricRemoved()`, `Start` after `ReestablishFormerSubscriptions`. |
| 7b673b5 | CHANGELOG (incl. the **breaking** `NewOTASoftwareUpdateRequestor` signature and removed `OTAUpdateState*` constants), daemon README. |

Every Go-changing commit carries its regenerated reachability inventory
(unreached stays 5).

## Rule inventory → where it is held

`OtaSoftwareUpdateRequestorServer.ts` lines as at HEAD (the brief's line
numbers are a few off; the code cites the actual ones):

| Rule (matter.js lines) | Test (`cluster/core/ota_software_update_requestor_test.go` unless noted) |
| --- | --- |
| initialize Unknown→Idle :175-178, device type :181 | `TestOTARequestor_Initialize` |
| #online :220-245 (defaults → active, 2-12 min / interval after update) | `TestOTARequestor_OnlineAddsDefaultsAndSchedules`, `…PreviousUpdateApplied` |
| #handlePreviousUpdateOnStart :255-313 | `…PreviousUpdateApplied`, `…PreviousUpdateFailed` |
| #enableOrDisableUpdates :316-336 | `…EnableDisable` |
| #assertDefaultOtaProviders :339-349, #handleUpdatedDefaultOtaProviders :352-368, fabric-scoped write | `…DefaultProvidersWrite`, `…DecodeDefaultProvidersWrite` |
| announceOtaProvider :376-420 (no fabric → UnsupportedAccess; Simple vs other; 1-600 s) | `…Announce` |
| #addActiveOtaProvider / #removeOtaProvider / #markActiveOtaProviderNoUpdate :423-476 | `…DownloadProtocolNotSupported`, `…NotAvailableAndBusy`, `…InvalidAnswers` |
| #scheduleUpdateQuery :485-522 | `…EnableDisable`, `…QueryTimerFires` |
| #chooseUpdateProvider :525-550 | `…ChooseProvider` |
| #performUpdateQuery / #queryForUpdate :568-642 (file already there, provider failure) | `…FileAlreadyDownloaded`, `…InvalidAnswers`, `…QueryTimerFires` |
| #updateAvailableFromProvider :650-762 (Busy ×3 floored to 120 s, NotAvailable, ProtocolNotSupported, mandatory fields, URI node, not newer, consent) | `…NotAvailableAndBusy`, `…InvalidAnswers`, `…Consent` |
| #validateApplyUpdate :767-837 (AwaitNextAction floor/cap, Proceed delay cap, Discontinue) | `…ApplyActions` |
| #queryOtaProvider :842-957 (QueryImage fields, consent, provider delay) | `…FullUpdate`, `…Consent` |
| #handleBdxDownload :959-1006 (3 attempts, ReceiverDrive, 1024) | `…DownloadFailure`, `bridge/ota_provider_e2e_test.go` |
| #updateProgress :1064-1070 | `…UpdateProgress` |
| #handleDownload / validateUpdateFile :1072-1158 | `…InvalidImage`, `…FullUpdate` |
| #updateState :1164-1204 | `…UpdateStateRules` |
| events :1210-1232 | `…PreviousUpdateApplied`, `…DownloadFailure` |
| #prepareApplyUpdate / #applyPreparedUpdate :1240-1285 | `…FullUpdate`, `…ApplyFailure` |
| state defaults :1312-1389 | `…Initialize`, `…Persistence` |

BDX (`bdx/session_test.go`): both driver modes, block-size cap
(bdx-session-initiator.ts:19-27, :155-165), BadBlockCounter with the
StatusReport back, LengthTooShort / LengthTooLarge, empty Block,
asynchronous refused, peer StatusReport, unexpected message, idle timeout,
readBlockQuery's ack check.

Two-process proof: `bridge/ota_provider_e2e_test.go`
`TestOtaRequestor_EndToEnd_QueryBDXApply` — a fake provider on its own UDP
socket (the ADR 0008 fake controller's CASE responder plus a new
`secureHook`), the bridge dialling it by CASE as initiator, QueryImage, the
BDX download on a second exchange of the same session, ApplyUpdateRequest,
the host's ApplyUpdate getting the exact bytes. Daemon:
`examples/reference-bridge/ota_test.go` (refusals; harness mode end to end
to VersionApplied + NotifyUpdateApplied).

## Guards run

| Guard | Result |
| --- | --- |
| `go test ./bdx/... ./cluster/core/... ./store/... ./bridge/... ./examples/... ./im/...` | pass |
| `go test -race` on the same packages (`make race` subset), e2e ×5 | pass |
| `make lint` (gofumpt v0.12.0, golangci-lint, chiptool tag, mod tidy/verify) | 0 issues |
| `make cover-check` | only the known darwin `mdns` failure (`TestBuildOperationalService_EmptyHostName_UsesDefault`); floors evaluated on the same run with mdns' measured 92.2 %: all 167 packages at or above floor. New floors: `bdx` 75, `bdx/otaimage` 76, `cluster/spec/otasoftwareupdateprovider` 100. |
| `make reachability` + ratchet test | pass, unreached 5 |
| `go test ./internal/chiptool/ -run 'TestWorkflow\|TestFamil'` | pass |
| bdx parity vs matter.js fixtures (messages, StatusReport, refused payloads, images incl. byte-identical `otaimage.Create`) | pass |

## Left out, and why

- **HTTPS download** (#handleHttpsDownload :1008-1062): out of brief scope.
  The requestor advertises BDX Synchronous only (matter.js's default
  `transferProtocolsSupported`, :1372); an `https://` URI is refused as an
  unsupported URI. `TransferProtocolsSupported` is not configurable.
- **TCP block size 8192** (:54): `OTA_BDX_MAX_BLOCK_SIZE_TCP` needs
  brief M's channel type; the requestor always asks for 1024 (:55). The
  bdx package already caps to the exchange's `MaxPayloadSize`, so a TCP
  exchange only has to report its size and the requestor one switch.
- **BDX's MRP specialisations in matter.js's MessageExchange**
  (`BDX_MAX_RETRANSMISSION_INTERVAL`, `#kickRetransmissionForLiveness`,
  `expectedProcessingTime` = message timeout, :70-77, :540-575, :1189-1220):
  MRP/session parameters are owner work. The bridge exchange uses the
  bridge's ordinary MRP tracker.
- `Send` on the bridge exchange returns after the datagram is out, not after
  its ack as matter.js's `exchange.send` does; a lost peer is noticed by the
  next receive's timeout (BDX: 5 min).
- The sender/responder transfer roles (BdxProtocol, outbound flows): an OTA
  Provider's, not exposed. Their message codecs and the messenger's sender
  helpers are present and tested.
- `updateReadyToApply` event (:1392-1395) and `downloadLocation` /
  `validateUpdateFile` overrides: host extension points of the TS class;
  Go hosts validate further in `OtaApplier.ApplyUpdate`.

## Deviations the owner should judge (by_design candidates)

1. **Server base**: the brief says "on `spec.Server`"; `spec.NewServer`
   refuses any fabric-scoped attribute (`spec.ErrFabricScoped`) and
   DefaultOTAProviders is one. Built on `spec.Instance` (+
   `FabricScopedReader`) like EcosystemInformation. *BD candidate*
   `BD-Matter-OTARequestor-OnSpecInstance`.
2. **Harness apply in place**: `--ota-harness` sets SoftwareVersion and runs
   the requestor's start-up path in process (`Restarted`) instead of
   restarting. matter.js's cert requestor leaves SoftwareVersion alone by
   default and restarts only with `--rebootAfterApply`
   (OtaRequestorTestInstance.ts:43-48, 151-164). A real product installs
   and restarts; the daemon outside harness mode keeps the image under
   `<ota-dir>/applied/` and keeps running the old version (reported as a
   failed update at the next start, as the rule says). Harness mode also
   sets `CanConsent` + consents and the 250 ms announced-query delay, as the
   cert requestor does (:65, :327-340). *BD candidate*
   `BD-Daemon-OTA-HarnessAppliesInPlace`.
3. **Requestor on endpoint 0**: the brief puts it on the root; matter.js's
   cert app uses an OtaRequestor endpoint 1. TC_SU_2_7 subscribes on
   endpoint 0, so the root fits the Python case.
4. **Validation split**: matter.js's `validateUpdateFile` checks only the
   header, payload size and version (`OtaImageReader.file` with the
   default full-checksum branch skips the payload digest,
   OtaImageReader.ts:82-86, :246-260) — no vendor/product. The digest,
   total size and the vendor/product check (brief item 4) are in the
   daemon's applier; a zero vendor/product is accepted as "applies to
   all" (OtaImageHeader.ts:26-41).
5. Error strings of matter.js's ImplementationErrors are lower-cased (lint).

## Findings for the register (matter.js against chip at the pin)

- **TC_SU_2_7 step 3**: expects Querying → Idle with reason *Failure* when
  the provider was killed; matter.js resets with *Success* after a failed
  provider connection (`#queryForUpdate` :632-641, `#resetStateToIdle()`
  default). Mirrored; the case will fail on that assert.
- **TC_SU_2_7 step 5**: expects DownloadError `bytesDownloaded > 0`,
  `progressPercent > 0`, `platformCode` null, and ≥ 300 s until Idle.
  matter.js reports bytesDownloaded 0 for a BDX failure (:995-1002 builds a
  fresh OtaDownloadError), platformCode = the BDX code or 0 (never null,
  :1228), and retries three times without waiting for the idle timeout
  when the session fails. Mirrored; three asserts likely fail.

## Open questions

1. **ClientList on the root**: the OtaRequestor device type requires client
   cluster OtaSoftwareUpdateProvider (ota-requestor.element.ts). matter.js
   derives ClientList from the endpoint type's declared client clusters
   (DescriptorServer.ts:288-297), which the root does not declare, so
   matter.js lists none; the daemon keeps ClientList empty. chip's
   device-type conformance cases may want 0x0029 there — owner decision.
2. **IDM golden**: the root now serves 0x002A and lists device type 0x0012;
   the chip-tool fixtures (IDM per endpoint) need a CI fixture run with
   `update_fixtures=true` (README rule), and the SU family rows are brief O's.
3. **Merge with brief M**: my bridge changes outside new files are three
   small edits: `bridge/receive.go` (the `deliverInitiatedSecure` hook just
   before the protocol switch, and a `//nolint:funlen` on `dispatch`),
   `bridge/attribute_value_reader.go` (two DefaultOtaProviders cases), and
   a `secureHook` field in `bridge/subscription_resume_e2e_test.go`'s fake
   controller. When M lands TCP, `initiatedSecureExchange` (addressed by
   `*net.UDPAddr`) should move onto M's connection abstraction, and
   `MaxPayloadSize` should come from the channel.
4. Should `otaProviderConnectTimeout` (90 s, matter.js
   PeerTimingParameters.ts:188) also bound the ADR 0008 path? Untouched.

## The OTA image brief O must generate for the SU cases

The daemon runs `SoftwareVersion` 1 / `SoftwareVersionString` "1" with
VendorID 0xFFF1, ProductID 0x8001 (`examples/reference-bridge/main.go`
testVendorID/testProductID, `wiring.go` BasicInformation config). Generate
with connectedhomeip's tool at the pin (`src/app/ota_image_tool.py`):

```sh
python3 src/app/ota_image_tool.py create \
    -v 0xFFF1 -p 0x8001 -vn 2 -vs "2.0" -da sha256 \
    <payload-file> <out>.ota
```

Format (both the tool and matter.js's OtaImageWriter, byte-identical for the
same inputs — `TestOtaImageParityMatterJS`): little-endian prefix `<IQI`
= file identifier 0x1BEEF11E, total size (prefix 16 + header + payload),
header size; then the anonymous TLV header {0: vendorId u16, 1: productId
u16, 2: softwareVersion u32, 3: softwareVersionString 1-64, 4: payloadSize
u64, [5: min, 6: max applicable u32, 7: release-notes URL], 8: digest type,
9: digest ≤ 64 bytes}; then the payload. Constraints the requestor and the
daemon enforce:

- version must be **greater than 1** and equal to what the provider's
  QueryImageResponse announces (the provider app reads it from the file);
- digest type must be one matter.js computes: `sha256` (1), `sha384` (7),
  `sha512` (8) or `sha3_256` (10) — the tool's truncated sha256 variants
  are refused;
- vendor/product 0xFFF1/0x8001 (or 0 for "any");
- a non-empty payload; any content.

The daemon must run with `--ota-harness` for TC_SU_2_7 step 7 (it reads
SoftwareVersion = 2 and the VersionApplied event afterwards). The applied
version lives only in BasicInformation's memory: a daemon restart runs
version 1 again, so the same image applies again in the next case.
