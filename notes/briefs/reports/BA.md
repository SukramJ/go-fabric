# Report BA — milestone 1, phase 12: BTP, the BLE central seam, the network steps

Branch `m1/phase-12` from `docs/roadmap` (d74f5d6), rebased onto
`origin/main` (b4ffec3: `docs/roadmap` is merged as #141 and gone from the
remote; one conflict in `docs/matterjs-comparison.md`, both rows kept).
matter.js read at `0ceb202d` (`git show`), chip at `6170af84`. Every item
commit carries its reachability inventory (unreached unchanged at 5) and a
CHANGELOG entry. AZ's files are untouched (in `examples/reference-controller`
only `pair.go` changed, plus a new `pair_test.go`).

| Commit | Item |
| --- | --- |
| 04b7ecc `feat(transport/btp)` | 1 |
| 7d1902e `feat(controller/ble)` | 2 |
| 8702e3b `feat(controller/commissioner): the Wi-Fi and Thread network steps` | 3 |
| d926601 `feat(cluster/core): a simulated Wi-Fi NetworkCommissioning` | 4 |
| 55baa5b `test(bridgetest): ServeConn's own coverage` | guard |

## 1. `transport/btp`

**Built.** `consts.go` (BleConsts.ts), `codec.go` (BtpCodec.ts: handshake
request/response, data packet, advertisement and service data),
`session.go` (BtpSessionHandler.ts, both roles), `conn.go` (`btp.Conn`, a
`transport.Conn`, `Reliable()` true). The session holds its state under one
mutex released exactly at matter.js's awaits (write, disconnect, message
hand-over), so the interleavings are matter.js's.
**Guard.** `codec_parity_matterjs_test.go` (all 21 cases of BtpCodecTest.ts
plus the flag-consistency refusals and truncation), `session_parity_matterjs_test.go`
(all 43 cases of BtpSessionHandlerTest.ts; matter.js's pending writes are
writes blocked on a channel), `conn_test.go` (an asynchronous in-memory GATT
pair, 20 messages up to 1000 bytes each way), `FuzzDecodePacket`.
Coverage 94 % (floor 93).
**Differences.** Timers and windows verbatim: ConnRspTimeout 15 s, AckTimeout
15 s, SendAckTimeout 5 s, ConnIdleTimeout 60 s, window 255, no-ack threshold 1,
immediate-ack threshold 2, segment 20–244 (BleConsts.ts:32-79). MRP is not
used: BleChannel `isReliable = true` (Ble.ts:38), `usesMrp = supportsMRP &&
!channel?.isReliable` (Session.ts:355-356). Go additions that matter.js does
not have: `Send` refuses a message above 0xFFFF bytes (16-bit length field);
a segment size leaving no room for payload is a FlowError instead of a
negative read; a truncated packet is `ErrTruncated` (matter.js's RangeError,
also not a protocol error); `NewCentral` refuses `requested < 20` (Go has no NaN).
**Decisions.** `maxDataSize` 0 is matter.js's `undefined` (identical result).
**Left out.** BtpSessionHandlerClosedObservableTest.ts (not named; the
`closed` latch is covered by `Closed()` in the ported cases).
**Questions.** None.

## 2. `controller/ble`

**Built.** `Central`/`Connection` (+ optional `Reachability`,
`ListeningClock`), `Scanner` (BleScanner.ts: records, rotation by service
data after 60 s, staleness in listening time, 15 min aging, reachability,
`Find`, `FindContinuously`, `Cancel`, `Close`), `Dial`/`Channel`
(NobleBleChannel.ts:592-860: segment size from ATT_MTU, minimum while
unknown; handshake on C1, C2 subscribed, response within 15 s; session on
C1/C2; disconnect = unsubscribe C2 then disconnect). `controller/ble/bletest`:
in-memory central and peripheral (handshake on C1, session on C2 subscribe,
BlenoBleServer order). `exchange.Config.Reliable` (no MRP; Next waits
max(30 s, ept) + 5 s, MRP.ts:111-113). `commissioner.Options.Channel`: PASE
on the channel's manager, closed with the PASE session; a lost channel
force-closes it (`ErrChannelClosed` wraps `btp.ErrDisconnected`).
`bridgetest.ServeConn` serves a node over a test's BTP peripheral through a
new `internal/bridgeseam` hook pair (receive on a conn, evict a conn).
`discovery.Identifier.SupportedOverBLE`/`MatchesBLE`.
**Guard.** `scanner_parity_matterjs_test.go` (parser vectors
SERVICE_DATA_A/B; 24 Go tests covering 26 of BleScannerTest.ts's 67 cases),
`channel_test.go` (no NeedsAck over BTP, multi-segment round trip, handshake
timeout, drop), `exchange/reliable_test.go`,
`node/ble_commissioning_live_test.go` (commission the module's node over the
in-memory BTP pair, read NodeLabel over CASE; race-clean ×3).
**Differences.** One exchange manager per BLE channel (matter.js: one
ExchangeManager, a channel per session); the manager names the peer by a
synthetic `[::%ble-<address>]:0`. Scan transitions are serialized but
without SCAN_TRANSITION_TIMEOUT.
**Left out.** BlueZ, `pair --ble` (phase 13); the stall renegotiation with a
smaller segment size (`NobleBleChannel.#startRenegotiation`; the session
reports the stall via `OnStall`); `forgetCommissionedDevice`; the scanner
cases on scan-transition gating, wall-clock steps and forgetting (41).
**Questions.** README §API stability (AZ's file) does not yet list
`controller/ble/bletest` among the public test-support packages.

## 3. Network steps

**Built.** `network.go`: steps 16.1 Validate, 16.2 Wifi, 16.3 Thread
(ControllerCommissioningFlow.ts:1537, :1577, :1758; AddOrUpdateWiFiNetwork
:1677, AddOrUpdateThreadNetwork :1874), scan with hints (:1959), network read
back by index, ConnectNetwork (:1049), breadcrumbs (`lastBreadcrumb++`),
`connectNetworkFailsafeTime` (:963), channel-drop fallback (:977-1036),
re-arm before a reArmFailsafe step (:385-393), BLE re-arm every 25 s
(:905-938), Networks collected in GetInitialData, Thread name resolved from
the dataset (:256-286) by a MeshCoP TLV walk (Network Name only). Error kinds
`ErrWifiOrThreadNetworkCredentialsNotConfigured`, `ErrWifiNetworkSetupFailed`,
`ErrThreadNetworkSetupFailed`, `ErrThreadNetworkNameMismatch`.
`Options.WiFiNetwork`/`ThreadNetwork` (dataset as bytes); `pair --wifi-ssid
--wifi-credentials --thread-dataset <hex>`.
**Guard.** `network_test.go` (Wi-Fi and Thread happy paths with breadcrumb
order, scan rejected → hint then add failure message, connect failure,
missing index, skips, name mismatch, registration and reArm flags, channel
drop, BLE re-arm timer), `channel_test.go`, `pair_test.go`. Coverage 85.8 %
(floor 85).
**Differences.** matter.js registers the network steps **only on BLE**
(:595-620; `ScanNetworksRejectionCommissioningTest.ts` proxies channelType to
BLE to test them). The brief's "as matter.js does" over IP does not hold at
the pin; here they also run over IP when Wi-Fi or Thread credentials are
given (without credentials the IP flow is matter.js's).
**Left out.** Thread TLV validation beyond the Network Name (matter.js's
decoder also refuses malformed PANID/channel/key TLVs and then keeps the
given name).
**Questions.** The `pair` usage line in `examples/reference-controller/main.go`
and its README (AZ's) should name the three flags.

## 4. Simulated Wi-Fi on the device side

**Built.** `contract.WiFiDriver` (scan, connect; stdlib only),
`cluster/core.WiFiNetworkCommissioning` (FeatureMap WI; MaxNetworks 1,
ScanMax 10, ConnectMax 20 — chip Linux driver; SupportedWiFiBands [2G4]),
`node.Network.WiFiCommissioning` (mounted instead of the Ethernet server;
commit on CommissioningComplete, revert on failsafe expiry; breadcrumb via
GeneralCommissioning's Breadcrumb write), reference daemon `--wifi-sim`
(`wifisim.go`: connect after 200 ms, stays on Ethernet).
**Guard.** `network_commissioning_wifi_test.go` (FailsafeRequired,
ConstraintError for SSID/credential lengths, BoundsExceeded, NetworkIDNotFound,
OutOfRange, error value, revert/commit, Busy, directed scan NetworkNotFound),
`node/wifi_commissioning_live_test.go` (over BTP and over IP: the network
steps run, the driver connects "lab", Networks reads connected over CASE),
`wifisim_test.go`. Manual: daemon `--wifi-sim` + `reference-controller pair
--passcode 20202021 --discriminator 3002 --wifi-ssid lab --wifi-credentials
secret12` paired over DNS-SD/IP; `read peer1 0/NetworkCommissioning/Networks`
→ `[{"networkId":"hex:6c6162","connected":true}]`.
**Differences.** Semantics from chip at the pin; the brief's
`network-commissioning.cpp` does not exist there — it is
`src/app/clusters/network-commissioning/NetworkCommissioningCluster.cpp`,
with list rules in `src/platform/Linux/NetworkCommissioningWiFiDriver.cpp`
(generalized to MaxNetworks). The failsafe check uses
`GeneralCommissioning.FailSafeArmed()`, not chip's fabric-matched
`IsFailSafeArmed(fabricIndex)` (as OperationalCredentials does here).
Connect is synchronous (chip's concurrent-connection path); the
non-concurrent early-response path is not modelled.
**Decisions.** A separate server type, so the Ethernet server and the
default composition do not move; `make chiptool-test` skipped for that reason
(the root's servers, order and FeatureMap are unchanged without the flag).
**Left out.** PDC (QueryIdentity), Thread on the device side, CNET harness
cases (phase 13).
**Questions.** The brief's `--wifi-credentials secret` (6 bytes) is refused
with ConstraintError by chip's credential rule (0, 5, 8–63 bytes or 64 hex,
NetworkCommissioningCluster.cpp:417-445) — the proof uses `secret12`.

## by_design candidates

- The network steps over IP when credentials are given (matter.js: BLE only,
  ControllerCommissioningFlow.ts:595-620).
- The device-side Wi-Fi server from chip, not matter.js (a generated stub).
- One reliable exchange manager per BLE channel with a synthetic peer
  address, instead of a per-session channel in one manager.
- BTP's Go-only refusals (message > 0xFFFF, no payload room, truncation as
  `ErrTruncated`); `btp.Conn` refuses > 1232 bytes at the channel.
- The Thread dataset read for the Network Name only.
- The Wi-Fi server's failsafe check not fabric-scoped.

## Ported matter.js test files

- `packages/protocol/test/codec/BtpCodecTest.ts` — all cases.
- `packages/protocol/test/ble/BtpSessionHandlerTest.ts` — all cases.
- `packages/protocol/test/common/BleScannerTest.ts` — parser vectors and 26
  of 67 cases (deduplication/aging, reachability, discoveries for one
  identifier, a scan that cannot start, close).
- `packages/node/test/node/ScanNetworksRejectionCommissioningTest.ts` — its
  device expectations (rejected scan is a hint, failed add fails) in
  `network_test.go`, not a case-by-case port.

## Guards

All at HEAD 55baa5b (the report commit after it is notes-only).

- `make ci` — exit 0 (build, vet, test, lint, cover-check). A first run at
  d926601 failed cover-check only: `bridge/bridgetest` 85.4 % < 92 %;
  fixed by 55baa5b (92.8 %). New floors: `transport/btp` 93,
  `controller/ble` 90, `controller/ble/bletest` 90.
- `make race` (CGO_ENABLED=1) — exit 0, whole module.
- `make fuzz` — exit 2 at `./tlv FuzzEncodeDecodeRoundTrip` ("utf8
  round-trip: got \"\ufffd\", want \"\xca\"", fuzz_codec_test.go:154),
  reproducible, in a package this branch does not touch — a pre-existing
  finding for the owner (invalid UTF-8 in a TLV string; which side is
  right is a parity question). The targets after it, run by hand at
  100000x: `transport/btp FuzzDecodePacket` 0,
  `transport/message` (3 targets) 0. The generated corpus was removed.
- `make reachability` — after every commit; unreached 5 throughout.
- Skipped: `make chiptool-test` (the root's composition and FeatureMap do
  not move without `--wifi-sim`), `make chiptool-families` (brief).
- Flake seen once under the parallel race run, passing on rerun ×3:
  `bridge TestOngoingReportChunkPiggybacksTheStatusResponseAck`
  ("report went out in 0 chunk(s)", 14.6 s).
