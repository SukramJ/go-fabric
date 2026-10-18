# Read map: test helpers, clocks and timers for phase 15 (`fabrictest`)

Map only, no recommendations. go-fabric HEAD ed4fdbe (checkout branch release/v0.7.1); matter.js read at 862ad75f via `git show`. A Sonnet read: spot-check before citing.

## 1. Clocks and timers

### 1a. Injectable time sources in production code (interface or field, owner, default)

| Seam | Declared | Owner pkg | Default (`time.Now` / `time.AfterFunc`) | Methods |
|---|---|---|---|---|
| `exchange.Clock` iface | controller/exchange/clock.go:11 | controller/exchange | `SystemClock` clock.go:25 (Now :28, AfterFunc :31); nil-defaulted manager.go:148-149 (`Config.Clock` manager.go:64-65) | `Now()`, `AfterFunc(d,f) Timer` |
| same iface reused as `Config.Clock` | controller/client/config.go:55-56; controller/client/registry.go:57 (default :140-141); controller/client/node.go:27 (default :81-82); controller/interaction/interaction.go:101 (default :120-121); controller/interaction/subscriptions.go:18 (default :62-63); controller/peer/set.go:55 (default :108-109); controller/commissioner/commissioner.go:59 (default :194-195); controller/dcl/certstore.go:62 (default :137-138); controller/ble/channel.go:37 (default :101-102); controller/ble/scanner.go:94 (default :123-124) | controller/* | `exchange.SystemClock{}` | as above |
| `btp.Clock` iface | transport/btp/session.go:46 | transport/btp | `SystemClock` session.go:56 (AfterFunc :59); `Options.Clock` :80, default :246-247 | `AfterFunc` only (no `Now`) |
| `btpClock` adapter | controller/ble/channel.go:233 | controller/ble | wraps `exchange.Clock` as `btp.Clock` | `AfterFunc` :235 |
| `discovery.clock` iface (unexported) | controller/discovery/loop.go:16 | controller/discovery | `wallClock` loop.go:23 (Now :25, AfterFunc :27); set at names.go:159-160; field names.go:90,120; config field `cfg.clock` names.go:149 (unexported: same-package tests only) | `Now()`, `AfterFunc(...) stopper` |
| `onoff.Clock` iface | cluster/onoff/server.go:96 | cluster/onoff | `wallClock` server.go:100 (AfterFunc :102); `Config.Clock` :126, default :236-237 | `AfterFunc(d,f) (stop func() bool)` only |
| `core.OtaClock` iface | cluster/core/ota_software_update_requestor.go:194 | cluster/core | `wallClock` :200 (Now :202, AfterFunc :203); `Config.Clock` :127, default :333-334 | `Now()`, `AfterFunc(...) (stop func() bool)` |
| `mattercert.TimeSource` iface | secure/mattercert/verify.go:37 | secure/mattercert | `SystemTime` verify.go:42; nil-defaulted `NewVerifier` :175-176; `FixedTime(t)` :47 ships in non-test code | `Now()` |
| `ca.Config.Now` func field | controller/ca/ca.go:162 | controller/ca | `time.Now` ca.go:250-251; adapter `clock` ca.go:570 -> `mattercert.TimeSource` | `func() time.Time` |
| `cluster.Quieter.Now` / `.AfterFunc` | cluster/attribute_changes.go:92-93 (doc :91 "default to time.Now and time.AfterFunc"); `now()` :170 | cluster | `time.Now` / `time.AfterFunc` (nil check in `now()` :170-172) | `func() time.Time`, `func(d,f) *time.Timer` |
| `core` OTA provider `Config.Now` | cluster/core/ota_provider.go:194 | cluster/core | `time.Now` ota_provider.go:257-258 | func field |
| `core` CommissionerControl `Config.Now` | cluster/core/commissioner_control.go:128 (internal `now` :178) | cluster/core | `time.Now` :211-212 | func field |
| `closure` Dimension `Config.Now` | cluster/closure/closuredimension_server.go:155 | cluster/closure | `time.Now` :206-207 | func field |
| `energy` EVSE `Config.Now` | cluster/energy/evse.go:161 (internal :181) | cluster/energy | `time.Now` :246-247 | func field |
| `energy` DeviceEnergyManagement `Config.Now` | cluster/energy/devicemanagement.go:205 (internal :223) | cluster/energy | `time.Now` :272-273 | func field |
| `energy` CommodityTariff `Config.Now` | cluster/energy/commoditytariff.go:194 (internal :207) | cluster/energy | `time.Now` :260-261 | func field |
| `mdns.hostAnswerer.now` (unexported) | mdns/host_responder.go:85 | mdns | `time.Now` host_responder.go:106 | func field |
| `groups.Manager.now` (unexported) | groups/manager.go:152 | groups | `time.Now` groups/manager.go:172 | func field |
| `commissioning` attestation `Now time.Time` (value, not func) | commissioning/attestation.go:55 | commissioning | `time.Now()` :81 | value |
| `ble.ListeningClock` iface (reports listened time, not wall time) | controller/ble/central.go:49 | controller/ble | none (optional Central capability) | `ListeningTime()` |
| `tariffTestClock` (example app, chip-style TestTimeManager) | examples/reference-bridge/fleet_energy_market.go:70 (now :76, enable :86, advance :105) | examples/reference-bridge | enabled/disabled; `d.clock.now` fed to `Now:` :178 | `enable`, `advance` |
| `demoDem.now` / `newDemoDem(name, now func() time.Time)` | examples/reference-bridge/fleet_evse.go:134,466; fleet_esa.go:47 | examples/reference-bridge | caller-supplied | func field |

No clock seam exists in: `bridge/` (grep `Clock|Now` in bridge/*.go non-test: none), `node/` (only node/root.go:229 `SetUTCClock` wiring), `im/`, `transport/mrp`, `secure/operational`, `bdx/`, `diagevent/`. Those read `time.Now()` directly (e.g. bridge/securechannel.go:553,643,1326; bridge/case_provider.go:71,181; bridge/pase_provider.go:76; bridge/subscribe.go:825; bridge/receive_dispatch.go:619; bridge/commissioning_window.go:415,566; secure/operational/manager.go:333,408,1176; im/subscription/manager.go:596; cluster/core/general_commissioning.go:586,864; cluster/core/time_synchronization.go:63; cluster/transition/transition.go:283,569,594,599).

### 1b. Time passed as an argument (the "tick(now)" style; no clock object)

| Function | file:line |
|---|---|
| `mrp.Retransmitter.Track(..., now)` / `.Tick(now)` | transport/mrp/retransmit.go:134 / :180 |
| `mrp.AckTracker.Owe(..., now)` / `.Due(now)` | transport/mrp/ack.go:150 / :247 |
| `subscription.Manager.Tick(ctx, now)` | im/subscription/engine.go:166 (ticker owner `NewTicker(m.cfg.TickInterval)` :39) |
| `Subscription.EndPriming / TouchLastReport / DeferReports (now)` | im/subscription/subscription.go:246 / :265 / :344 |
| `outboundReliableTracker.Track/Tick(now)` | bridge/outbound_reliable.go:121 / :316 (comment on wall vs injected `now` :84-86) |
| `Bridge.RunAckPumpOnce(now)` | bridge/ackpump.go:388 |
| `Bridge.ackPumpTick(tracker, outbound, now)` | bridge/ackpump.go:322 |
| `bridgeseam.AckPumpTick` hook installed | bridge/seam.go:105 |
| `bridgetest.AckPumpTick(b, now)` (due StandaloneAcks + outbound retransmits + timed-deadline sweep) | bridge/bridgetest/bridgetest.go:207 |
| `exchangeRouting.sweepExpiredTimedDeadlines(now)` | bridge/exchange_routing.go:157 (maybe-variant :135) |
| `OperationalSessionLookup.RetransmitBaseInterval(sessionID, now)` | bridge/handlers.go:1107; `Entry.RetransmitBaseInterval(now)` secure/operational/manager.go:380 |

### 1c. Test-side fakes (every one found)

| # | Type | file:line | Implements | How a test advances it |
|---|---|---|---|---|
| 1 | `fakeClock` | transport/btp/helpers_test.go:16 | `btp.Clock` (AfterFunc :30) | `Advance(d)` :47 fires due timers in order |
| 2 | `fakeClock` | controller/exchange/helpers_test.go:21 | `exchange.Clock` (Now :38, AfterFunc :44) | `Advance(d)` :69; `durations()` :62, `pendingTimers()` :95 |
| 3 | `fakeClock` | controller/interaction/helpers_test.go:21 | `exchange.Clock` | `Advance(d)` :91; `pending()` :61, `waitTimer(t,d)` :74 |
| 4 | `fakeClock` | controller/client/helpers_test.go:24 | `exchange.Clock` | `Advance(d)` :64 |
| 5 | `manualClock` | controller/client/cache_internal_test.go:22 | `exchange.Clock` (Now :36, AfterFunc :42) | `Advance(d)` :62; `armed()` :50 |
| 6 | `fakeClock` | controller/peer/helpers_test.go:31 | `exchange.Clock` | `Advance(d)` :70; `waitTimers` (doc :30) |
| 7 | `fakeClock` | controller/commissioner/helpers_test.go:23 | `exchange.Clock` | `Advance(d)` :76; `live()` :63 |
| 8 | `fakeClock` | controller/discovery/helpers_test.go:73 | unexported `clock` (Now :91, AfterFunc :97) | `advance(n *Names, d)` :115 (runs on the Names loop) |
| 9 | `manualClock` | controller/dcl/helpers_test.go:360 | `exchange.Clock` | `advance(d)` :401; `pending()` :422 |
| 10 | `stepClock` | controller/ble/channel_test.go:164 | `exchange.Clock` (Now fixed `time.Unix(0,0)` :168) | `fire()` :180 runs the one armed callback; `waitArmed` :178 (no-op) |
| 11 | `fakeClock` | controller/ble/scanner_parity_matterjs_test.go:37 | `exchange.Clock` | `Advance(d)` :75 |
| 12 | `fakeClock` | cluster/attribute_changes_test.go:38 | `Quieter.Now`/`AfterFunc` (AfterFunc :51 returns real `*time.Timer`) | `advance(d)` :59 runs recorded callbacks |
| 13 | `fakeClock` | cluster/onoff/onoff_server_test.go:21 | `onoff.Clock` (AfterFunc :33) | `Advance(d)` :48 |
| 14 | `manualClock` | cluster/onoff/server_internal_test.go:16 | `onoff.Clock` (stores one `f`) | test calls stored `f` directly |
| 15 | `otaClock` | cluster/core/ota_provider_test.go:46 | `Config.Now` (Now :51) | `advance(d)` :57 (time only, no timers) |
| 16 | `fakeClock` | cluster/core/ota_software_update_requestor_test.go:42 (`newFakeClock` :48, base `time.Unix(1_700_000_000,0)`) | `core.OtaClock` | `advance(d)` :84 fires due timers; `pending()` used :492 |
| 17 | `manualClock` | cluster/core/scenes_onoff_test.go:19 | `onoff.Clock`; holds every timer, runs none | never advances |
| 18 | `fakeClock{now}` | cluster/closure/closuredimension_test.go:383 | `cfg.Now = clk.Now` (:385) | test assigns `clk.now` |
| 19 | `tariffClock` | cluster/energy/commodity_test.go:307 | `Config.Now` (Now :312) | `advance(d)` :318 |
| 20 | `fakeClock{t}` | mdns/host_responder_parity_matterjs_test.go:24 ("matter.js MockTime", comment) | `h.now = clock.now` | `advance(d)` :27 |

Inline closures (no named type): groups/sender_test.go:80,121,125,140,144 (`m.now = func()...`); cluster/core/commissioner_control_test.go:306 (`c.Now`); cluster/energy/commodity_days_test.go:140; cluster/opstate/countdown_internal_test.go:29-30 (`srv.quiet.Now` + `srv.quiet.AfterFunc`); controller/ca/ca_test.go:247; examples/reference-bridge/fleet_evse_test.go:146; bridge/bridgetest/commissioner.go:230 (`ca.Config.Now` pinned to `certificateInstant`, doc :2-5 of that const); `mattercert.FixedTime` verify.go:47.

Controller tests that pass no clock (system clock): controller/handshake/helpers_test.go:144 `exchange.Config{}`; bridgetest.Commissioner.start passes no `Clock` to `exchange.NewManager`, `peer.NewSet`, `commissioner.New`, `interaction.NewSubscriptions` (bridge/bridgetest/commissioner.go:200-240; grep `Clock` in bridge/bridgetest/*.go: no hits). controller/interaction/live_test.go:123,129 `newLive(t, regClock exchange.Clock)` is the only live-node test taking a clock (registry only).

### 1d. Timer call sites in non-test production code (`time.AfterFunc`, `NewTimer`, `NewTicker`, `Sleep`, `time.After(`)

| Package | Raw time.* count | Per file (up to 3 lines) |
|---|---|---|
| bridge/ | 11 | commissioning_window.go 2 (:418, :583); pase_provider.go 2 (:88 AfterFunc 60s, :161 NewTicker); case_provider.go 1 (:155); ackpump.go 1 (:304 `NewTicker(ackPumpInterval)`); health_probe.go 1 (:81); group_messaging.go 1 (:185 AfterFunc groupJoinRetryInterval); bdx_responder.go 1 (:181 NewTimer); subscription_reestablish.go 1 (:326); receive_dispatch.go 1 (:308); securechannel.go 1 (:1334 `time.After(stopSessionCloseBudget)`) |
| im/ | 1 | subscription/engine.go 1 (:39 `NewTicker(m.cfg.TickInterval)`) |
| transport/mrp/ | 0 | (pure `now`-argument API, see 1b) |
| secure/ | 1 | operational/manager.go 1 (:1159 `NewTicker(poll)`) |
| commissioning/ | 0 | - |
| node/ | 4 | ota.go 2 (:114 NewTimer, :164 `time.Sleep(10ms)`); persist.go 1 (:310 NewTicker); node.go 1 (:409 `NewTicker(reopenPollInterval)`) |
| mdns/ | 4 | zeroconf.go 2 (:531 `AfterFunc(time.Second, r.Announce)`, :634 NewTicker); subtype_responder.go 1 (:426 `time.After(readErrorBackoff)`); operational_resolver.go 1 (:96 NewTimer) |
| controller/ | 0 raw (2 definitions of the system clock: exchange/clock.go:31, discovery/loop.go:27) | 22 `clock.AfterFunc` call sites through the seam: exchange/exchange.go 4 (:472, :617, :673, :739); client/cache.go 2 (:187, :280); commissioner/connection.go 2 (:130, :233; :251 is `context.AfterFunc`, not the seam); peer/connection.go 2 (:226, :355); discovery/names.go 2 (:552, :584); client/addresses.go 1 (:55); interaction/subscriptions.go 1 (:254); interaction/sustained.go 1 (:267); discovery/scanner.go 1 (:350); discovery/solicitor.go 1 (:298); peer/monitor.go 1 (:108); dcl/certstore.go 1 (:181); commissioner/network.go 1 (:601); ble/channel.go 1 (:160); ble/scanner.go 1 (:385) |
| (extra, not requested) cluster/ | 14 raw | attribute_changes.go 2; core/groupcast.go 2; energy/devicemanagement.go 2; transition/transition.go 2 (:569 `time.AfterFunc(time.Until(at),...)`); onoff/server.go 2; core/general_commissioning.go 1; core/identify.go 1; core/ota_software_update_requestor.go 1; cover/windowcovering_server.go 1; energy/evse.go 1; levelcontrol/native.go 1 |

## 2. Existing test harnesses

### 2a. bridge/bridgetest (public)

| Symbol | file:line | Description (from doc comment) |
|---|---|---|
| package doc | bridge/bridgetest/bridgetest.go:4-22 | affordances to put a `bridge.Bridge` into a state without wire traffic; `Commissioner` = controller reaching it over the wire; reaches bridge via `internal/bridgeseam` |
| `SubscriptionTarget` | bridgetest.go:69 | peer a subscription's ReportData travels to (the values a SubscribeRequest carried) |
| `EstablishSubscriptionTarget(b, subID, target)` | bridgetest.go:152 | installs subscription route + anchors peer's inbound counter window (`PeerCounter`) without wire |
| `AckPumpTick(b, now)` | bridgetest.go:207 | one full pump tick: due StandaloneAcks, outbound retransmits, timed-deadline sweep, at caller-supplied `now` |
| Errors `ErrNoBridge`, `ErrNoPeer`, `ErrNoSubscriptionID`, `ErrNotEstablished` | bridgetest.go:~34-65 | |
| `ServeConn(b, conn transport.Conn) (*ConnServer, error)` | bridge/bridgetest/conn.go:35 | serves one non-UDP connection (BTP peripheral) through the bridge's transport-neutral receive path |
| `ConnServer.Deliver / Close / Wait` | conn.go:52 / :76 / :84 | ordered message delivery; seam calls `bridgeseam.ReceiveOnConn` :70, `ConnClosed` :80 |
| `ErrNoConn` | conn.go:16 | |
| `Options` | bridge/bridgetest/commissioner.go:68 | Passcode, Discriminator, FabricID, NodeID, ControllerNodeID, CAT, AdminSubject (no effect), AdminVendorID, FailSafeSeconds (no effect), Seed, Advertiser (:52) |
| `Dial(tb, addr, opts) *Commissioner` | commissioner.go:162 | in-process controller on loopback UDP; closed via `tb.Cleanup`; builds udp listener, `exchange.Manager`, `discovery.Names` over silent `queryConn`, `ca`, `commissioner.Fabric`, `peer.Set`, `interaction.Subscriptions`, `commissioner.Commissioner`, `handshake.CASEServer` (`start()` :200-262) |
| `Commissioner.Commission(ctx) *Session` | commissioner.go:345 | full on-network commissioning flow (PASE ... CommissioningComplete over CASE) |
| `.PASE` / `.CASE` / `.CASEAs(nodeID, cats...)` / `.Accept` | commissioner.go:382 / :396 / :404 / :422 | sessions; CASEAs mints another controller identity |
| `.RemoveFabric(ctx, s)` | commissioner.go:435 | |
| `.ControllerIdentity(nodeID, cats...)` | commissioner.go:457 | sigma.Identity for CASE initiators outside the package |
| `.ResolveSigma1Destination` | commissioner.go:493 | |
| `.Close / Addr / FabricIndex / CompressedFabricID / RootPublicKey / ResolveOperational` | commissioner.go:271 / :304 / :307 / :314 / :318 / :324 | `ResolveOperational` implements `bridge.OperationalResolver` |
| `Session` + `.PeerNodeID .LocalNodeID .Close .Read .Subscribe .NextReport .Write .Invoke` | bridge/bridgetest/session.go:47; :55 :59 :63 :92 :122 :160 :179 :212 | IM client over a session; `StatusError` :34, `ErrSessionClosed` :26, `ErrHandshake` :30, `Subscription` :104, `InvokeResult` :195 |
| `TLV`, `DecodeTLV`, `AttributeData`, `AttributeStatus`, `Report` (`.Value`, `.Status`) | bridge/bridgetest/report.go:18, :38, :81, :88, :94 (:115, :125) | report decoding |
| names.go (unexported) `queryConn`, `feedOperational` | bridge/bridgetest/names.go:26, :56 | DNS-SD cache without multicast; device's operational record injected as answer |
| seed.go (unexported) `seededStream` | bridge/bridgetest/seed.go:22 | seeded randomness (`newSeededStream` :31) |
| consts `DefaultControllerNodeID`, `DefaultAdminVendorID`, `DefaultFailSafeSeconds` | used commissioner.go:171-180 (declared commissioner.go:34-45 region, DefaultControllerNodeID :36) | |

### 2b. endpoint/endpointtest (public)

| Symbol | file:line | Description |
|---|---|---|
| `AssertDeviceTypeConformance(tb, topology, tolerate...)` | endpoint/endpointtest/endpointtest.go:42 | fails tb on device-type violations unless tolerated; returns violations |
| `FakeStore` / `NewFakeStore()` | :66 / :74 | in-memory `endpoint.Store`, next id starts at 2; methods GetEndpoint :82, UpsertEndpointAssigning :91, ListEndpoints :102, RemoveEndpoint :113 |
| `AssemblerConfig()` | :137 | `endpoint.Config` the empty snapshotter assembles under |
| `NewEmptySnapshotter()` | :158 | `bridge.Snapshotter` over an empty fleet (one assembler, one store) |

### 2c. internal seams and parity support

| Symbol | file:line | Description |
|---|---|---|
| `bridgeseam.CaptureSubTarget` | internal/bridgeseam/bridgeseam.go:41 | hook var installed by package bridge init |
| `bridgeseam.AnchorPeerCounter` | :66 | anchor peer counter window |
| `bridgeseam.AckPumpTick` | :75 | installed bridge/seam.go:105 |
| `bridgeseam.ReceiveOnConn` / `ConnClosed` | :83 / :87 | used by `ConnServer` conn.go:70,80 |
| `channelseam.AnchorPeerCounter` | internal/channelseam/channelseam.go:40 | secure/channel-side counterpart |
| `paritytest.ClusterSnapshot(tb, id)` | internal/paritytest/paritytest.go:125 | cluster entry of parity/schema.json |
| `paritytest.Cluster.Attribute/Command/Event` | :96 / :102 / :108 | lookups, fail tb if absent |
| `paritytest.Conformance(expr, names)` | :170 | evaluates matter.js conformance expressions |
| types `Element`, `Effective`, `Text`, `Access`, `Feature`, `Cluster` | :27, :46, :60, :65, :74, :83 | |

### 2d. controller/ test helpers and test-only sub-packages

| Symbol | file:line | Description |
|---|---|---|
| `bletest.Central` / `NewCentral(ps...)` / `.Add .SetDiscoveryCallback .StartScanning .StopScanning .Scanning .Advertise .Connect` | controller/ble/bletest/bletest.go:29 / :42 / :51 :65 :72 :85 :95 :103 :115 | in-memory `ble.Central` |
| `bletest.Peripheral{Address, Discriminator, VendorID, ProductID, ATTMTU, Serve}` / `.ServiceData()` | bletest.go:127 / :143 | GATT server of the Matter service; `Serve` returns per-session message handler |
| `attestation.TestStore()` | controller/attestation/store.go:93 | CSA test PAA store (used by Dial, commissioner tests) |
| `startNode(t[, adv, listen])` | controller/handshake/helpers_test.go:32; controller/interaction/node_test.go:33; controller/peer/node_test.go:91; controller/commissioner/node_test.go:45 | four copies: `sql.Open("sqlite", temp db)` + `node.New(node.Config{Network 127.0.0.1:0, Commissioning, ProductDescription, BasicInformation, Store{DB}})` + `Start` + `Cleanup(Stop)` -> addr from `n.Bridge().LocalAddr()` |
| `newManager(t)` + `recorder` | controller/handshake/helpers_test.go:144 | loopback udp.Listener + `exchange.Manager` (`exchange.Config{}`) + Serve goroutine |
| `commissionedFabric(t)` | controller/handshake/case_test.go:31 | startNode + `bridgetest.Dial` + `Commission` + `ControllerIdentity` -> `handshake.Fabric` |
| `commissionedFabricOffline(t)` | controller/handshake/scripted_test.go:211 | `bridgetest.Dial(t,"127.0.0.1:9",...)`, no node |
| `commissioned(t, adv, listen)` + `newLiveSet(t, names, fabric, listen, edit)` + `liveSet` | controller/peer/node_test.go:129 / :159 | returns node, addr, fabric; "a set on a real loopback socket and the system clock" |
| `paseTo(t, addr)` | controller/interaction/node_test.go:66 | PASE session on loopback manager |
| `liveNodeAt(t, path, controllerAddr)` + `newLive(t)` | controller/client/live_test.go:86 / :137; controller/interaction/live_test.go:63 / :123 (`newLive(t, regClock)`, `newLiveWith` :129) | node with OnOff plug on Aggregator + `AttachOperationalResolver` + hand-assembled controller (udp, `exchange.Manager`, `discovery.Names` over `fakeMDNS`, `ca.New`, `commissioner.NewFabric`, `peer.NewSet`, `commissioner.New`), operational record injected as DNS answer |
| `harnessNode` / `harnessCommissioner` / `commission` | controller/dcl/commission_test.go:47 / :83 / :130 | commissioner-with-DCL harness |
| `silentMDNS` / `fakeMDNS` / `resolverFunc` | controller/dcl/commission_test.go:41; controller/client/live_test.go:72,78 | no-op `mdns.QueryConn`; resolver adapter |
| node-side: `newNode`, `commissioned(t, cfg)`, `commissionerOptions()`, `liveNode(t,name,edit)` | node/node_test.go:59; node/commission_test.go:52,47; node/ota_live_test.go:149 | `commissioned` = newNode + Start + `bridgetest.Dial` + `Commission` |
| bridgetest-internal: `newDevice` / `startNode` / `options()` | bridge/bridgetest/commissioner_test.go:37 / :64 / :77 | |
| bridge white-box: `newBridgeWithAdvertiser` | bridge/bridge_helpers_test.go:341 | |

`node.New(` is called in tests of 14 files (examples/reference-controller/cli/main_test.go; bridge/bridgetest/commissioner_test.go; node/{node,client,commission,groupsend_live,ota_live}_test.go; controller/{client/live,handshake/helpers,interaction/live,interaction/node,dcl/commission,peer/node,commissioner/node}_test.go; device/subscriber_test.go).

### 2e. Who uses `bridgetest` (test files outside bridgetest, call counts of `bridgetest.`)

controller/ca/ca_test.go 1; controller/handshake/{case 4, scripted 3, server 4}; controller/interaction/provider_test.go 1; controller/peer/node_test.go 2; examples/reference-controller/cli/pair_ble_test.go 1; node/{basic_window 3, ble_commissioning_live 2, commission 11, issue87 1, knobs 2, node 2, observers 2}_test.go; inside bridgetest: bridgetest_test.go 8, commissioner_test.go 30, conn_test.go 3, peer_counter_anchor_test.go 12.

`bridgetest.ServeConn` callers: node/ble_commissioning_live_test.go:71 (in `bletest.Peripheral.Serve`, returns `srv.Deliver, srv.Close` :76); examples/reference-controller/cli/pair_ble_test.go:75 (`h.n.Bridge()`); bridge/bridgetest/conn_test.go:33,37,41.

### 2f. How an in-process controller meets an in-process node today

Path A (bridgetest, loopback UDP, real clock): `startNode` (node.New + Start, sqlite temp db) -> `bridgetest.Dial(t, n.Bridge().LocalAddr(), Options{Passcode, Discriminator, FabricID, NodeID, Seed, Advertiser?})` -> `c.Commission(ctx)` -> `*Session` (`Read/Write/Invoke/Subscribe`) or `c.ControllerIdentity(...)` for handshake-level tests. Examples: controller/handshake/case_test.go:31-50; controller/peer/node_test.go:129-150; node/commission_test.go:52-64. Operational discovery: `Options.Advertiser` (an `mdns.NewNoop()` given to the node) lets the commissioner read the device's records (commissioner.go:46-51, names.go:56); node-initiated resolution via `c.ResolveOperational` (commissioner.go:324) attached with `Bridge().AttachOperationalResolver`.

Path B (hand-assembled controller, loopback UDP, optional clock): controller/client/live_test.go:137-200 and controller/interaction/live_test.go:123- build udp + `exchange.Manager` + `discovery.Names` + `ca` + `commissioner` + `peer.Set` themselves, inject DNS answers for the operational record, node gets `AttachOperationalResolver(resolverFunc)` to reach the controller (live_test.go:114-116).

Path C (BLE/BTP, in-memory): `bletest.Central` + `bletest.Peripheral.Serve` -> `bridgetest.ServeConn(n.Bridge(), conn)` (node/ble_commissioning_live_test.go:68-77; examples/reference-controller/cli/pair_ble_test.go:75).

Bridge-only (no controller, no network): `bridgetest.EstablishSubscriptionTarget` + `AckPumpTick` (bridge/bridgetest/bridgetest.go:152,207; used openccu-loom tests/scenario/scenario_harness_test.go:362).

## 3. matter.js counterparts (read at 862ad75f)

### 3a. packages/node/src/testing/mock-server-node.ts

| Symbol | line | Note |
|---|---|---|
| `beforeEach` / `afterEach` / `after` hooks tracking `nodesOfCurrentTest`, fail run on leaked open nodes | 50 / 55 / 89 | module-level leak guard |
| `class MockServerNode<T> extends ServerNode<T>` | 102 (doc 96-101: mock time, mock crypto, in-memory storage, simulated network host) | |
| constructor overloads / impl | 111, 112, 113 | initializes MockTime, `MockCrypto(config.index)` :131, `NetworkSimulator` :140, default index 0x80 :123 |
| `get matter()` | 159 | |
| `get simulator()` | 166 | |
| `online(options, actor)` | 173 | run actor with a `RemoteActorContext` over a `MockExchange` |
| `static createOnline(...)` | 195 (impl :203) | create + add device (On/Off Light default) + start; installs exchange capture :241 |
| `handleExchange()` | 260 | next queued outgoing `MockExchange` |
| `createExchange(options?)` | 267 | `ProtocolMocks.NodeSession.create` :268 |
| `stop()` / `close(stepMs?)` | 283 / 290 | |
| `addFabric()` | 297 | uses `TestFabric` :299 |
| `namespace MockServerNode`: `RootEndpoint` (= `ServerNode.RootEndpoint.with(ControllerBehavior)`), `MockOptions` {online, device, index, simulator, matter}, `Options`, `Configuration` | 306, 310, 316, 342, 347 | |

### 3b. packages/node/src/testing/node-helpers.ts

| Symbol | line |
|---|---|
| `settled(...nodes)` (yields until `NodeActivity` idle; never advances mock time) | 69 (doc 60-68) |
| `FAILSAFE_LENGTH_S = 60` | 104 |
| `testFactoryReset(...)` | 112 |
| `CommissioningHelper()` returns `{fabricNumber, almostCommission(node?, index=1, exchange?), commission(existingNode?, index=1, exchange?)}` | 181; fabricNumber 183; almostCommission 191 (ArmFailSafe, SetRegulatoryConfig, CSR, AddTrustedRootCertificate, AddNOC; fail-safe left armed); commission 278 (almostCommission + CommissioningComplete on new session) |
| `namespace interaction`: `BarelyMockedMessenger` 318, `createInvokeMessenger` 337, `BarelyMockedMessage` 356, `BarelyMockedGroupMessage` 363, `connect(node, fabric)` 370, `write` 381, `read` 400, `invoke` 427, `subscribe` 465, `receiveDataReport(node)` 478, `receiveData(node, minAttrs, minEvents)` 493 | 317 |
| `seedPeerCache` 520, `clientStructureOf(peer)` 540, `subscribedPeer(controller, id)` 548, `causeMessagesOf(promise)` 570, `captureLog(level)` 599 | |

Other files in the folder: mock-exchange.ts, index.ts (`git ls-tree 862ad75f packages/node/src/testing`).

### 3c. packages/node/src/testing/mock-site.ts

| Member | line |
|---|---|
| `class MockSite` (private `#simulator`, `#nodes`, `#environments`, `#nextNetworkIndex`, `#storage`, `#createStorageDriver`) | 31-37 |
| constructor(options?: `MockSite.Options`) | 42 |
| `addNode(type?/definition, options?)` plain `ServerNode` + mock crypto + simulated host (`env.set(Network, simulator.addHost(index))` :82) + site storage; starts unless `online:false` | 51, 55, 59 |
| `addController(options?)` (offline, commissioning disabled, admin fabric id 1) | 112 |
| `addDevice(options?)` (On/Off Light, started) | 134 |
| `addUncommissionedPair(options?: PairOptions)` | 144 |
| `addCommissionedPair(options?)` (starts controller, commissions with mock time, 90 s timeout; entropy enabled only while pairing) | 156 |
| `close()` | 182 |
| `storageFor(id)` (in-memory store persists across restarts within the site) | 212 |
| `[Symbol.asyncDispose]()` | 225 |
| `namespace MockSite`: `Options` {createStorageDriver} 234, `PairOptions` {controller, device config} 244 | 230 |

### 3d. MockTime (packages/testing/src/mocks/time.ts, 831 lines)

Also listed by `git grep -l MockTime`: packages/testing/src/{global-declarations.d.ts, mocks/blob.ts, mocks/crypto.ts, util/heap.ts}, packages/general/src/storage/MemoryBlobStorageDriver.ts.

| Member | line |
|---|---|
| `class MockTimer` (Timer iface mirror: `name`, `interval` get/set, `start`, `stop`, `#close`) | 211 (interval 249/258, start 262, stop 274) |
| `export const MockTime = {` | 324 |
| `activeImplementation` get | 327 |
| `disable()` | 334 |
| `enable()` ("Reverts to disabled for each test file") | 342 |
| `reset(time = epoch)` (sets time, enables) | 350 |
| `init()` (enable + reset if not enabled) | 363 |
| `forceMacrotasks` get/set (default microtasks) | 375 / 379 |
| `requireHostAsync(dependent)` / `requireMacrotasks(dependent)` | 388 / 392 |
| `hostTurnInterval`, `abandonedHostAsyncOps`, `dependentCount`, `pendingHostAsyncOps` (getters) | 399, 406, 413, 422 |
| `atTime(time, actor)` (run with wall clock pinned) | 436 |
| `now` (Date) / `nowMs` / `nowUs` (monotonic; timers run on this) | 456 / 460 / 468 |
| `stepWallClock(offsetMs)` (NTP-step style, may be negative) | 476 |
| `getTimer(name, duration, cb)` / `getPeriodicTimer(name, interval, cb)` | 480 / 484 |
| `sleep(name, duration)` | 493 |
| `macrotask` get / `macrotasks` get | 506 / 517 |
| `resolve(promise, {stepMs, macrotasks})` (moves time forward until promise resolves) | 537 |
| `advance(ms)` (runs tasks scheduled in the interval) | 638 |
| `yield()` / `yield3()` | 669 / 679 |
| `interceptOnce(obj, method, responseInterceptor, callInterceptor?)` | 694 |
| `timerCountFor(name)` | 751 |
| `callbackAtTime(atMs, cb)` / `removeCallback(cb)` | 758 / 769 |
| interception of the production clock: `timeSetup(Time)` swaps `Time.default = MockTime.activeImplementation` (via `installActiveImplementation`); `registry` {timers, register, unregister} | 778-787; 205-207 |
| `instrumentImplementation` wraps `macrotask` getter with `requireMacrotasks` | 789-817 |
| `Object.assign(globalThis, { MockTime })` | 819 |
| `Boot.init` hook: per test, `MockTime.reset(); MockTime.disable()` (kind "state" resets `wallClockOffsetMs`) | 821-831 |

## 4. Consumers' test needs

Existing under /home/markus/Dokumente/GitHub: `go-daikin2mqtt`, `go-homeconnect2mqtt`, `go-mtec2mqtt`, `go-unifi2mqtt`, `go-zendure2mqtt`, `openccu-loom` (also `go-hamqtt`, `go-ha-catalog`, `go-mqtt`: checked). No go-*2mqtt repo references go-fabric (grep of go.mod and `*.go`: no hits). Only openccu-loom does (go.mod pins `github.com/SukramJ/go-fabric v0.6.0`).

| file:line | Import / call |
|---|---|
| openccu-loom/cmd/openccu-loom/daemon_matter_groups_test.go:13 | imports `bridge/bridgetest` |
| ...daemon_matter_groups_test.go:51 | `bootCommissionedBridge(t) (ctx, *matterBridgeBundle, *bridgetest.Session, uint16)` |
| ...daemon_matter_groups_test.go:119 | `bridgetest.Dial(t, bundle.bridge.LocalAddr(), bridgetest.Options{...})` |
| ...daemon_matter_groups_test.go:126 | `cmr.Commission(ctx)` |
| ...daemon_matter_groups_test.go:152,185,203 | `sess.Invoke(...)`; :170 `sess.Write(...)`; :236 `sess.Subscribe(ctx, 0, 60, ...)`; :220 `*bridgetest.InvokeResult` |
| openccu-loom/cmd/openccu-loom/matter_window_adapter_test.go:14, :198, :205 | import; `bridgetest.Dial(...)`; `cmr.Commission(ctx)` |
| openccu-loom/tests/scenario/scenario_harness_test.go:19, :21 | imports `bridgetest`, `endpoint/endpointtest` |
| ...scenario_harness_test.go:151 | `endpointtest.NewEmptySnapshotter()` |
| ...scenario_harness_test.go:362 | `bridgetest.EstablishSubscriptionTarget(h.br, sub.ID, bridgetest.SubscriptionTarget{...})` (comment :310) |
| openccu-loom/tests/scenario/scenario_topologies_test.go:17, :41-42 | imports `endpointtest`; `endpointtest.NewFakeStore()` returned with `bridge.Snapshotter` |

loom `bridgetest.`/`endpointtest.` occurrences total: 9. No loom test calls `AckPumpTick`, `RunAckPumpOnce`, `ServeConn`.

## Counts

- Named test fakes/manual clocks: 20 (types, 1c) plus 6 inline-closure sites; production time seams: 22 rows (1a), `now`-argument APIs: 11 (1b).
- Raw timer call sites in production code: bridge 11, im 1, transport/mrp 0, secure 1, commissioning 0, node 4, mdns 4, controller 0 raw (22 via Clock seam; 2 SystemClock definitions); cluster (extra) 14.
- Helpers listed: bridgetest 30+ symbols, endpointtest 6, seams 7, paritytest 9, controller/test helpers 14 rows (2d), matter.js: MockServerNode 12, node-helpers 14, MockSite 11, MockTime 33.
