# read: matter.js inventory for the roadmap (2026-10-17)

Sonnet first read of `../matter.js`, **at the checkout's HEAD `82b00bd6`
(2026-10-17), 52 commits past the module's pin `0ceb202d`** — a map, not a
citation. Load-bearing numbers spot-checked by the owner-side agent with
`wc -l` / `find` on 2026-10-17 (marked ✓). Paths relative to the matter.js
root.

## 1. Behaviours: hand-written logic vs generated stub

143 directories under `packages/node/src/behaviors/` ✓. A generated stub
server is 14 lines (`/*** THIS FILE WILL BE REGENERATED … ***/` at :7, one
`export class XServer extends XBehavior {}`), 18 lines when it carries the
`.with(...)` note (CameraAvStreamManagement, CameraAvSettingsUserLevelManagement,
NetworkCommissioning).

Stubs (no logic): camera-av-stream-management (18 ✓), camera-av-settings-
user-level-management (18), web-rtc-transport-provider (14), push-av-stream-
transport (14), zone-management (14), chime (14 ✓), tls-certificate-management
(14), tls-client-management (14), thread-border-router-management (14),
thread-network-directory (14), wi-fi-network-management (14),
thread-network-diagnostics (14), joint-fabric-administrator (14),
joint-fabric-datastore (14), commissioner-control (14), ambient-context-sensing
(14), water-tank-level-monitoring (14), time-synchronization (14 ✓),
network-commissioning (18). `web-rtc-transport/` holds only `index.ts`.

Hand-written: web-rtc-transport-requestor/WebRtcTransportRequestorServer.ts
224 ✓ (class :42, Events :199; validates against the originating fabric,
tracks `currentSessions`, installs an Operate ACL entry on the first fabric,
:23-41); icd-management/IcdManagementServer.ts 838 ✓ + IcdMode.ts 152;
access-control/AccessControlServer.ts 775 — no ARL / managed-device logic
(`grep -i "restriction|Arl|managedDevice"` empty in node/src and protocol/src).

Servers over ~150 lines (25): color-control 2046, thermostat 1790 ✓,
door-lock 1628 ✓, ota-software-update-requestor 1436, scenes-management 1109,
groupcast 938, level-control 875, icd-management 838, access-control 775,
ota-software-update-provider 697, window-covering 654, operational-credentials
632, general-diagnostics 631, group-key-management 618, switch 486 ✓, on-off
343, descriptor 303, service-area 300, general-commissioning 285,
administrator-commissioning 267, groups 232, web-rtc-transport-requestor 224,
binding 214, basic-information 213, identify 156. Every energy, tariff and
appliance behaviour (device-energy-management*, energy-evse*, water-heater*,
commodity-*, oven-*, microwave-oven-*, laundry-*, dishwasher-*,
refrigerator-*, temperature-control, operational-state, rvc-*, service-area
excepted) is a stub or under 150 lines.

## 2. Protocol (`packages/protocol/src`)

- icd/ 1162 lines ✓: CheckInMessage 180, FabricIcd 307 (class :24,
  `processCheckIn` :207 — the controller-side receiver, trial decryption
  §4.22.4.2, `CheckInMessage.decodeIcd` :265), IcdCheckInBackOff 84,
  IcdCheckInSender 70 (class :38; used by IcdManagementServer.ts:356-360),
  IcdCheckInSuppression 53, IcdCounter 85, IcdPeerSchedule 97,
  IcdPeerWakefulness 272. SecureChannelProtocol.ts:149-150,164-172 routes
  `IcdCheckInMessage` to `fabric.icd.processCheckIn`. Controller client:
  packages/node/src/behavior/system/icd/IcdClient.ts 1140 ✓ (class :55),
  holdUntilIcdPeerAwake.ts 69, IcdMultiAdminError.ts 74, IcdPeerAsleepError.ts 25.
- ble/: BtpSessionHandler.ts 715, BleConsts.ts 80, Ble.ts 71 (abstract `Ble`,
  `BleChannel`, `BlePeripheralInterface`); common/BleScanner.ts; the node
  implementation (central and peripheral) is `packages/nodejs-ble`.
- bdx/ 1554: BdxMessenger 328, bdx-session-initiator 269, BdxSession 259,
  BdxProtocol 225, BdxClient 59; action/client/ClientBdx.ts.
- transport/tcp/: TcpTransport.ts 238 (class :50), TcpChannel.ts 325.
- dcl/ 4709: DclCertificateService 2178, DclOtaUpdateService 1319,
  DclVendorInfoService 497, DclClient 429 (class :55), DclRestApiTypes 188,
  DclConfig 53, SeedTypes 32.
- groups/ 735 + session/GroupSession.ts 457.
- CASE resumption, client side: session/case/CaseClient.ts:126-142 (resume1),
  :167 (Sigma2 | Sigma2Resume); SessionManager.ts:82-101, :227; stale records
  dropped at peer/Peer.ts:540, peer/PeerConnection.ts:675-731.
- Sustained subscription: action/client/subscription/SustainedSubscription.ts
  672 ✓ (:38-45 retry, re-subscribe, park on LIT peer wake); ClientSubscriptions
  284, ClientSubscription 117, ClientSubscriptionHandler 188, ClientSubscribe 66,
  PeerSubscription 61; peer/RebootResubscribeArmer.ts 214.
- thread/ OperationalDataset.ts 327 + TLV codecs; peer/ 7110 total
  (ControllerCommissioningFlow.ts 2142); mdns/ 1354.

## 3. Packages

`packages/`: cli-tool, create, general, main, matter.js, model, mqtt, node,
nodejs, nodejs-ble, nodejs-shell, nodejs-ws, protocol, react-native,
testing, thread-br-client ("Thread Border Router discovery, dataset codec,
and MeshCoP/OTBR diagnostics"; src/: coap, commissioner, credentials,
crypto, diagnostic, … ✓), types. `support/`: chip (image source for
ghcr.io/matter-js/chip, README.md:1-12), chip-testing, codegen, models,
tests (@matter/integration-tests), tools.

## 4. Tests

Convention: `*Test.ts` in packages/{node,protocol,types,model,general};
`*.test.ts` in support/chip-testing. Runner `matter-test` (Mocha + chai,
packages/testing/README.md:17-19).

### packages/node/test — 209 `*Test.ts` ✓
Tree (tests / lines): behavior 4/868, behavior/cluster 13/3895,
behavior/internal 2/534, behavior/state 1/61, behavior/supervision 1/600,
behaviors 54 ✓ (36 dirs, below), devices 1/62, endpoint 8/1721,
endpoint/properties 5/564, endpoint/server 3/3717, endpoint/type 4/480,
endpoint/validation 9/5568, endpoints 3/752, loader 1/29, node 49/23885 ✓,
node/client 11/1386, node/integration 2/625, node/remote 1/164, storage 2/791.

behaviors/ (file lines): access-control 380+239; basic-information 44+44+126;
binding 857+1375+223; boolean-state 34; color-control 407+131+72;
commodity-tariff 87; concentration-measurement 97; descriptor 37+648;
door-lock 1946; energy-evse-mode 79; fan-control 37; general-diagnostics
78+157; groupcast 2243; group-key-management 209; groups 100;
hepa-filter-monitoring 94; icd-management 1415+180; identify 187;
level-control 289+315+145+195; measurement 64; occupancy-sensing 55; on-off
352; operational-credentials 360; ota 2819 (+ ota-utils.ts 511);
power-source 38; rvc-operational-state 73; scenes-management 99;
service-area 234; switch 3527 ✓; thermostat 519+41+215+150+460+391;
thermostat-user-interface-configuration 68; thread-network-diagnostics 37;
time-format-localization 48; unit-localization 20; web-rtc-transport-
requestor 605; window-covering 145.

node/ (49): Attribute*/Command*/Event* response tests, Client* (TcpTest,
SubscribeTest, InvokeTest, AttestationTest …), commissioning
(FailsafeCommissioningTest, SplitCommissioningTest, DecommissionTest …),
MockServerNodeTest, MockSiteTest, ServerNodeTest, ServerSubscriptionTest,
PluginsTest; helpers icd-helpers.ts, read-helpers.ts.

How a behaviour test is written (behaviors/on-off/OnOffServerTest.ts):
`MockServerNode.createOnline()` (:28-52), `part.act(agent =>
agent.get(OnOffServer).toggle())`, `expect(endpoint.state.onOff.onOff)`,
`MockTime.enable/reset/advance/resolve` (:55-130), wire path via
`interaction.invoke(node, fabric, {commandPath}, …)` with `node.addFabric()`
(:331-352). Helpers: packages/node/src/testing/mock-server-node.ts 348
(class :102, createOnline :195, addFabric :297), node-helpers.ts 615
(`namespace interaction` :317 — connect :370, write :381, read :400, invoke
:427, subscribe :465; CommissioningHelper :181, seedPeerCache :520),
mock-site.ts 270 ✓ (MockSite :31, addController :112, addDevice :134,
addCommissionedPair :156, addUncommissionedPair :144), mock-exchange.ts 64;
packages/node/test/endpoint/mock-endpoint.ts:36.

### packages/protocol/test — 114 `*Test.ts` ✓ (lines)
action: InvokeLargeMessage 350; action/client: ClientInteractionSubscribe 352,
ClientInteractionInvoke 902, ReadScope 60, InputChunk 672,
LargeMessageTransport 106; action/client/subscription: SustainedSubscription
1712, ClientSubscriptions 150; action/request: Read 311, Invoke 182, Write
327, Subscribe 341, Specifier 163, ReadAttributePaths 62; action/response:
WriteResult 322, ResultStatus 35, ReadResult 261, InvokeResult 214;
advertisement: Advertisement 96, ServiceDescription 34; bdx:
PersistedFileDesignator 72, Bdx 1726; ble: BtpSessionHandlerClosedObservable
42, BtpSessionHandler 1526; certificate: Certificates 758,
DeviceAttestationValidator 887, PqcAttestationCertificates 1014,
CertificateAuthority 311, CertificationDeclaration 110; codec: MessagePrivacy
51, MessageCodec 295, BtpCodec 358; common: DiscoveryData 128, FailsafeTimer
84, FailsafeContext 117, BleListeningClock 56, ExpandedPath 134, PathError 53,
BleScanner 1342, ExpandedStatus 59; dcl (10 files, 7883); events:
NonvolatileEventStore 157, OccurrenceManager 159, VolatileEventStore 309;
fabric: FabricManager 281, Fabric 265, FabricAuthority 470; groups (5, 514);
icd (8, 1310): IcdCounter 60, IcdCheckInSender 97, IcdCheckInBackOff 63,
IcdCheckInSuppression 32, CheckInMessage 154, IcdPeerWakefulness 340,
FabricIcd 463, IcdPeerSchedule 101; interaction: InteractionClientMessenger
803; mdns: MdnsServer 1836, CommissionableMdnsScanner 1247, Mdns 1110; ota:
OtaImageReader 468, OtaImageWriter 524; peer (14 files, 3699):
RebootResubscribeArmer 529, CommissioningConnection 797, PeerAddressMonitor
465, PeerConnectionKick 441 …; protocol: DeviceAdvertiser 391,
MessageReceptionState 803, ProtocolStatusCode 63, MessageCounter 216,
TcpSessionBinding 542, MessageExchange 972, MRP 340, MessageChannel 81,
ExchangeManager 619; protocol/interaction: AttributeDataEncoder 362,
EventDataDecoder 362, AttributeDataDecoder 1234; securechannel:
SecureChannelMessenger 108, SecureChannelProtocol 99; session: SecureSession
495, SessionIntervals 99, SessionParameters 112, SessionManager 924;
session/secure: CasePairing 131, PasePairing 316; thread: BasicTlvCodec 97,
MeshCopTlvTypes 53, OperationalDataset 200; transport/tcp: TcpChannel 487,
TcpTransport 276.

Other: packages/types/test 33 files (4844 lines); packages/model/test 41
(11759); packages/general/test 97 (23714).

### support/chip-testing
test/core 27 files, test/app-fast 46, test/app-slow 6 (I, LVL, OO, PS, SWTCH,
WNCV), test/app-cc 2, test/icd 2 (ICDB, ICDM), test/cert 68 `*.test.ts` ✓
(TC-<CLUSTER>-<n>.<m>, 31801 lines), test/cert-framework 52.
PATCHED_TESTS_INFO.md:5-6: TestAccessControlCluster.yaml and
TestOperationalCredentialsCluster.yaml patched (OpCreds rev 3 PQC vs rev 2).

### packages/testing/src (2835 lines top level)
cli/runner/options (`matter-test`), mocha/chai setup, nodejs + web
(Playwright) runners, reporters, mocks/time.ts 831 ✓ (`MockTime` :324:
enable, reset, advance, resolve, yield), mocks/crypto, environment, logging,
blob, boot; chip/ (yaml-test, python-test, manual-test, accessory-server,
chip-bins, command-pipe, pics/, cert/); device/ (backchannel, subject, test);
docker/.

## 5. Device types (`packages/node/src/devices/`, 81 files)

Camera: camera.ts 174, snapshot-camera 108, floodlight-camera 38,
camera-controller 164. Doorbell/chime/intercom: doorbell 77, video-doorbell
35, audio-doorbell 159, chime 86, intercom 163. thread-border-router 181,
network-infrastructure-manager 205. Joint fabric: no device, endpoint type
packages/node/src/endpoints/joint-fabric-administrator.ts. Other endpoint
types in endpoints/: aggregator, base, bridged-node, device-energy-management,
electrical-sensor, joint-fabric-administrator, ota-provider, ota-requestor,
power-source, root, secondary-network-interface.

## 6. Energy and appliance device types — all present
solar-power 119, battery-storage 110, heat-pump 111, energy-evse 109,
water-heater 138, oven 67, cooktop 76, cook-surface 86, refrigerator 110,
dishwasher 132, laundry-washer 130, laundry-dryer 129, extractor-hood 106,
microwave-oven 167, robotic-vacuum-cleaner 195; electrical-sensor is an
endpoint type (endpoints/electrical-sensor.ts).
