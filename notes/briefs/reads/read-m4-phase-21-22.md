# Read map: milestone 4, phases 21 and 22 (observability and persistence ports and hardening; packaging and cookbook)

An inventory, not an analysis. Sonnet read; a map, never a citation. Verify every matter.js line before it enters a brief/ADR/notes.
go-fabric read in the worktree of `main` @ bd168c7b (v0.10.2), clean tree; paths are repo-relative, `file:line`. matter.js read only via `git show 862ad75f:<path>` / `git grep ... 862ad75f` (cited `mjs:<path>:line`, at the pin). Siblings read in the checkouts next to this repo (`../go-*`), working trees as they stand today, not pinned to a tag.
Counting scripts live in the scratchpad of this run (`.scratch/`, not committed): counts are grep counts of non-test `.go` files, so they include `examples/` unless a line says otherwise.
Roadmap rows: docs/roadmap.md:197 (phase 21), :198 (phase 22); Done when :210-212; status :216.

## §1 Observability today

### 1.1 `*slog.Logger` fields per package (non-test files; count of lines containing `*slog.Logger`, first site)

| Package | Count | First site |
| --- | --- | --- |
| cluster/core | 27 | cluster/core/general_commissioning.go:48 |
| node | 15 | node/root.go:98 (the node's own field: node/node.go:51 `logger *slog.Logger`) |
| client/interaction | 11 | client/interaction/sustained.go:91 |
| examples/reference-bridge | 9 | examples/reference-bridge/control.go:87 |
| mdns | 9 | mdns/subtype_responder.go:55 |
| controller/client | 8 | controller/client/cache.go:137 |
| controller/dcl | 7 | controller/dcl/client.go:88 |
| client/handshake | 5 | client/handshake/server.go:81 |
| cluster/thermo | 5 | cluster/thermo/atomic.go:194 |
| controller/ble | 5 | controller/ble/channel.go:38 |
| controller/commissioner | 5 | controller/commissioner/commissioner.go:56 |
| transport/tcp | 5 | transport/tcp/listener.go:55 |
| client/discovery | 4 | client/discovery/names.go:88 |
| client/peer | 4 | client/peer/peer.go:31 |
| groups | 4 | groups/counter.go:67 |
| fabrictest | 4 | fabrictest/site.go:74 |
| bridge | 3 | bridge/reply.go:1336 (the bridge's own logger is a constructor argument, bridge/bridge.go:747) |
| cluster/cover | 3 | cluster/cover/windowcovering_server.go:172 |
| examples/reference-controller/cli | 3 | examples/reference-controller/cli/cli.go:122 |
| script/reachability | 3 | script/reachability/main.go:221 |
| transport/udp | 3 | transport/udp/listener.go:72 |
| binding, bdx, client/exchange, cluster/light, endpoint, im/subscription, transport/btp | 2 each | binding/manager.go:145, bdx/responder.go:74, client/exchange/manager.go:69, cluster/light/colorcontrol_server.go:152, endpoint/assembler.go:98, im/subscription/manager.go:92, transport/btp/session.go:67 |
| controller/attestation, examples/quickstart-controller | 1 each | controller/attestation/policy.go:42, examples/quickstart-controller/main.go:95 |

`logger()` / `log()` / `Logger()` accessors (grep `func (…) (logger|log|Logger)()`; 12): transport/udp/listener.go:346, mdns/zeroconf.go:771, controller/dcl/client.go:94 and controller/dcl/ota.go:74 (`log()`), cluster/core/{general_commissioning.go:1001, basic_information.go:1082, operational_credentials.go:2479, general_diagnostics.go:1072, access_control.go:1243}, cluster/cover/windowcovering_server.go:939, cluster/light/colorcontrol_server.go:653, cluster/thermo/thermostat_server.go:1490; exported: fabrictest/site.go:166 `Site.Logger()`.
Packages with a `slog` import in non-test code: see 1.5 for the fallback (nil logger) behaviour.

How a logger arrives (the same grep, `Config.Logger` style): `node.Config.Logger` nil becomes `slog.Default()` at node/node.go:144-145 and is handed on (node/node.go:283 `DiagnosticLogs.SetLogger`); `transport/udp.Config.Logger` nil is `slog.Default()` (transport/udp/listener.go:71, :145); `bridge.New` argument nil is `slog.Default()` (bridge/bridge.go:746-748); `im/subscription.NewManager` nil is `slog.Default()` (im/subscription/manager.go:191-193). Docs: docs/guides/ports.md:83 row `Logger` — "the node's logger" — default `slog.Default()`. No docs page states attribute-key conventions (grep `slog|attribute` in docs/ outside history/adr hits docs/guides/ports.md, docs/getting-started/{controller,device}.md, docs/guides/host-view.md only as usage).

### 1.2 slog attribute keys in use

Method: `grep -rnoE 'slog\.(String|Int|Int64|Uint64|Uint32|Uint|Bool|Any|Duration|Float64|Time|Group)\("<key>"'` over non-test `.go`: 1393 attribute sites, 309 distinct keys. Call style: 727 log calls found by `(log|Log|logger()|logger|slog)….(Debug|Info|Warn|Error)(Context)?(`; 709 with a literal message. `slog.LogAttrs`: 0 uses. Positional key/value style (`log.Info("msg", "k", v)`, not `slog.Attr`): used in `examples/reference-controller/cli` (examples/reference-controller/cli/self.go:157 `"node", rec.NodeID`; cakey.go:42 `"ref"`; cakey.go:64; cakey.go:66; pair.go:182 `"err", err`); the library packages use `slog.X("key", …)` attributes.
Counts include `examples/` (241 of the 1393 attribute sites are under examples/, 225 of the 263 `err` sites are outside examples/ — the 38 are in examples/reference-bridge, e.g. examples/reference-bridge/fleet_sensors.go:87).

Top keys (count, first site):

| Key | Count | First site |
| --- | --- | --- |
| err | 263 | examples/reference-bridge/fleet_sensors.go:87 (outside examples/: 225) |
| device | 106 | examples/reference-bridge/fleet_sensors.go:84 (102 in examples/reference-bridge; the other 4 in controller/commissioner) |
| reason | 57 | client/exchange/exchange.go:663 |
| src | 50 | transport/udp/listener.go:274 |
| session_id | 29 | bridge/conn.go:514 |
| endpoint | 28 | client/interaction/report.go:279 (27 sites outside examples/; the 1 in examples/ is examples/host-view/main.go:111; others bridge/subscribe.go:1413, bridge/receive_dispatch.go:154, binding/resolve.go) |
| fabric_index | 24 | client/handshake/server.go:411 |
| cluster | 23 | client/interaction/report.go:279 |
| address | 23 | client/peer/monitor.go:179 |
| hint | 22 | node/node.go:360 (bridge 11, node 5, controller 2, mdns 2, groups 1, examples 1; e.g. bridge/securechannel.go:341, node/mdns.go:54) |
| message | 21 | controller/dcl/client.go:468 |
| node | 20 | controller/client/reactions.go:129 |
| peer | 19 | client/peer/peer.go:61 |
| exchange_id | 18 | bridge/bdx_responder.go:131 |
| value | 16 | examples/reference-bridge/fleet_sensors.go:84 |
| mode | 15 | examples/reference-bridge/fleet_energy.go:139 |
| status | 14 | bridge/subscribe.go:631 |
| exchange | 14 | client/exchange/exchange.go:606 |
| attribute | 14 | examples/reference-bridge/fleet_climate.go:247 |
| subscription_id | 13 | bridge/subscription_persistence.go:216 |
| skid | 13 | controller/dcl/certstore.go:350 |
| instance | 13 | bridge/bridge.go:1806 |
| version | 12 | examples/reference-bridge/ota.go:82 |
| opcode | 12 | client/interaction/handler.go:55 |
| session | 10 | client/handshake/server.go:410 |
| peer_node | 10 | client/handshake/server.go:411 |
| stage | 9 | bridge/reply.go:1341 |
| conn | 9 | transport/tcp/listener.go:207 |
| kind | 8 | bridge/devicetype_validation.go:75 |
| fabric | 6 | bridge/receive_dispatch.go:442 |

The full 309-key list is in the scratch file; a long tail of 1-count keys (`hue`, `mireds`, `cook_time`, `rinses`, …) is all in examples/reference-bridge/fleet_*.go and cluster servers.

### 1.3 The same thing under two (or more) keys (grep counts; each line cites a site of each)

- Error: `slog.String("err", err.Error())` 233 sites (e.g. node/node.go:738) versus `slog.Any("err", err)` 30 sites; key `error` 2 sites (examples/reference-bridge/fleet_appliances.go:332 first). Positional style uses `"err", err` (examples/reference-controller/cli/pair.go:182).
- Fabric: `fabric_index` 24 (client/handshake/server.go:411; node/security.go:177; node/ota_provider.go:186) versus `fabric` 6 (bridge/receive_dispatch.go:442, bridge/ota_provider_link.go:355, bridge/subscribe_dispatch.go:231, controller/commissioner/commissioner.go:501, im/subscription/manager.go:343, im/subscription/persistence.go:123) versus `ctx_fabric_index`, `invoke_fabric_index`, `current_fabric`, `fabric_id` (node/security.go:301, a fabric id, not an index).
- Session: `session_id` 29 (bridge/conn.go:514; node/security.go:700) versus `session` 10 (client/exchange/manager.go 9 sites, client/handshake/server.go:410 1 site).
- Peer / node identity: `peer_node` 10 (client/handshake/server.go:411), `peer_node_id` 1 (node/security.go:702), `peer` 19 (client/peer/peer.go:61), `node` 20 (controller/client/reactions.go:129), `node_id` 2 (node/security.go:302), `source_node_id`, `client_node_id`, `noc_subject_node_id`.
- Address: `src` 50 (transport/udp/listener.go:274; bridge/receive.go, bridge/securechannel.go), `address` 23 (client/peer/monitor.go:179), `addr` 3 (client/peer/connection.go:683), `dst` 1 (bridge/securechannel.go:1250), `dest` 1 (groups/request.go:82), `host` 2 (bridge/bridge.go:2188), `to` 4 (client/peer/monitor.go:224).
- Exchange: `exchange_id` 18 (bridge/bdx_responder.go:131) versus `exchange` 14 (bridge/receive_dispatch.go:61 4 sites, client/exchange/exchange.go 5 sites incl. :606, client/exchange/manager.go 5 sites).
- Endpoint: `endpoint` 28 (client/interaction/report.go:279; bridge/subscribe.go:1413), `endpoint_id` 3 (bridge/bridge.go:1102), `endpoints` 1, `source_endpoint` 1.
- Subscription: `subscription_id` 13 (bridge/subscription_persistence.go:216), `subscription` 6 (client/interaction/handler.go:97), `id` 2 (im/subscription/manager.go:342, persistence.go:122).
- Group: `group_id` 5 (bridge/group_messaging.go:296), `group` 2 (groups/request.go:82).
- Status: `status` 14 (bridge/subscribe.go:631), `status_code` 4 (bridge/receive_dispatch.go:220), `general_code`/`protocol_code`/`status_name`.
- Why: `reason` 57 (client/exchange/exchange.go:663), `hint` 22 (node/node.go:360), `why` 3 (client/discovery/status.go:162), `what` 3 (client/interaction/sustained.go:152), `message` 21 (controller/dcl/client.go:468).
- Device: `device` is examples-only (102 of 106) except controller/commissioner (4 sites); library packages use `endpoint`/`node`/`peer`.
- Message sizes: `bytes` 7 (transport/btp/session.go:326), `payload_bytes` 2 (bridge/securechannel.go:437), `datagram_bytes`, `message_bytes`, `noc_bytes`, `icac_bytes`, `original_bytes`/`trimmed_bytes`/`max_bytes` (tlv/encode.go:245-247).
- Duration units: `interval` (Duration, mdns/zeroconf.go:642), `send_interval` 2 (bridge/subscription_reestablish.go:292), `timeout_ms` 1 (bridge/receive_dispatch.go:645), `timeout` 3 (client/interaction/subscribe.go:225), `configured_ms`/`advertised_ms` (mdns/service.go:494-496, Uint64 ms), `seconds` 3 (examples/reference-bridge/fleet_energy.go:153), `min_interval`/`max_interval`.

### 1.4 Event-name (message) prefixes

Method: the literal first argument of the 709 log calls above (`.scratch/msgsite.txt`). Messages are dotted lower-case event names (`matter.controller.case.invalid_param_failed`, client/handshake/case.go:126); a few are free text.

| Prefix | Count | First site |
| --- | --- | --- |
| `matter.` (all) | 452 | see sub-prefixes |
| `matter.controller.*` | 161 | client/handshake/case.go:126 |
| `matter.rx.*` | 59 | bridge/securechannel.go:322 |
| `matter.mdns.*` | 36 | bridge/securechannel.go:1300 |
| `matter.ota.*` | 35 | cluster/core/ota_software_update_requestor.go:521 |
| `matter.tx.*` | 22 | bridge/securechannel.go:1208 |
| `matter.thermostat.*` | 15 | cluster/thermo/atomic.go:200 |
| `matter.subscription.*` | 15 | bridge/subscription_persistence.go:142 |
| `matter.tcp.*` | 14 | transport/tcp/listener.go:206 |
| `matter.bridge.*` | 13 | bridge/securechannel.go:1346 |
| `matter.btp.*` | 11 | transport/btp/session.go:249 |
| `matter.groups.*` | 9 | bridge/group_messaging.go:156 |
| `matter.discovery.*` | 9 | client/discovery/status.go:97 |
| `matter.ble.*` | 9 | controller/ble/scanner.go:195 |
| `matter.commissioner_control.*` | 8 | cluster/core/commissioner_control.go:284 |
| `matter.basic_information.*` | 7 | cluster/core/basic_information.go:473 |
| `matter.opcreds.*` | 6 | cluster/core/operational_credentials.go:1184 |
| `matter.case.*` | 4 | bridge/case_initiator.go:304 |
| `matter.subscribe.*`, `matter.general_diagnostics.*`, `matter.gencomm.*`, `matter.udp.*`, `matter.exchange.*` | 3, 3, 3, 2, 2 | bridge/subscribe.go:534, cluster/core/general_diagnostics.go:370, cluster/core/general_commissioning.go:440, transport/udp/listener.go:272, bridge/bdx_responder.go:130 |
| `dcl.*` (`dcl.certificates` 23, `dcl.ota` 15, `dcl.revocation` 5) | 45 | controller/dcl/certstore.go:161 |
| `ota.*` (`ota.provider` 13, `ota.image_stored`, `ota.applied_in_place`) | 18 | node/ota_provider.go:163 |
| `case.identity` | 7 | node/security.go:127 |
| `controller.*` (`controller.ca_key` 3, `controller.pair` 2, `controller.dcl_snapshot` 2, `controller.self`) | 8 | examples/reference-controller/cli/cakey.go:42 |
| `binding.*` | 8 | node/client.go:290 (`binding.restore`) |
| `commissioning.*` (`commissioning.own_window` 3, `commissioning.window` 2) | 6 | node/node.go:705, node/commissioning.go:154 |
| `pase.window` | 3 | node/commissioning.go:325 |
| `node.*` (`node.observer` 2) | 4 | node/observers.go:133 |
| `diaglogs.*`, `bdx.*`, `apppipe.*` | 4 each | cluster/core/diagnostic_logs.go:561, (bdx first site not recorded), examples/reference-bridge/control.go:95 |
| `groups.*`, `mdns.*`, `gendiag.*`, `label`, `scenes` | 3, 2, 2, 2, 1 | first sites not recorded; likely node/groupsend.go:32, node/mdns.go:53, node/node.go:738, node/persist.go:148, node/persist.go:205 (same lines carry log calls; the message text was not read) |
| `tlv.*` | 1 | tlv/encode.go:328 (`tlv.utf8_truncated`) |
| `device.*` | 1 | device/device.go:380 (`device.default_server.generated`) |
| `im.*`, `endpoint.*` | 0 | no message in the 709 starts with either |
| per-device names in the reference daemon (`vacuum.`, `waterheater.`, `sensor.`, `evse.`, `washer.`, `tariff.`, …) | ~170 | examples/reference-bridge/fleet_servicearea.go:128, fleet_energy.go:139, fleet_sensors.go:84 |

Files carrying most log calls (directory counts of the 709): bridge 152, examples/reference-bridge 115, cluster/core 77, node 46, controller/client 43, controller/commissioner 42, controller/dcl 40, client/interaction 24, mdns 21, client/exchange 21, client/peer 20, cluster/thermo 15, transport/btp 11.

### 1.5 Fallback when no logger is given (not uniform)

`slog.Default()`: bridge/bridge.go:747, node/node.go:145, transport/udp/listener.go:145 (+ four more `slog.Default()` sites in that file), transport/tcp/listener.go:113, transport/tcp/conn.go:74, mdns/subtype_responder.go:104, mdns/service.go (4 sites), mdns/zeroconf.go (3), client/discovery/names.go:144, im/subscription/manager.go:191-193, cluster/core/{general_commissioning,operational_credentials,basic_information}.go (5 each), groups/manager.go.
`slog.New(slog.DiscardHandler)`: client/handshake/{server.go:113, pase.go:80, case.go:104}, client/exchange/manager.go:163, client/interaction/interaction.go:118, client/interaction/subscriptions.go:68, client/peer/set.go:107, transport/btp/session.go:236, fabrictest/site.go:124.
Handlers are built only in executables and scripts: `slog.NewTextHandler` at examples/reference-bridge/main.go:155 (+ `slog.SetDefault` :156), examples/reference-controller/cli/cli.go:169, script/reachability/main.go:208, script/dclsnapshot/main.go:36.

### 1.6 `diagevent/` (the diagnostic event ring)

One non-test file: diagevent/ring.go (160 lines) + ring_test.go (140).
- API: `Kind` (ring.go:29) with `KindPairing` (:35), `KindSession` (:38), `KindDiscovery` (:41); `Severity` (:46) `SeverityInfo/Warning/Error` (:50-52); `Event{At time.Time; Kind; Severity; Message string; Detail map[string]string}` (:56-72); `Ring` (:75); `(*Ring).SetClock(clock.Clock)` (:86); `NewRing(capacity int)` (:101); `(*Ring).Record(Event)` (:117); `(*Ring).Snapshot() []Event` newest first (:143). Methods are nil-receiver safe (ring.go:69-73 doc, :87, :118, :144).
- Size: `NewRing(n)` with n < 1 becomes `defaultCapacity = 200` (ring.go:110); the node builds it with 256 (node/node.go:249). Drop policy: oldest overwritten (ring.go:117-137). In-memory only; no persistence (package doc ring.go:4-21: "bounded, in-memory"; the doc says the alarm journal and audit log are the durable records).
- Writers: bridge/securechannel.go (15 `diagevent.` references; first `b.diagRing().Record` at :388 session, :465 pairing warning, :594 pairing error, :1115 session, :1312 discovery) and bridge/conn.go:566 (session). `Bridge.AttachDiagnosticEvents(*diagevent.Ring)` bridge/bridge.go:2259; `Bridge.DiagnosticEvents() []diagevent.Event` :2268; `atomic.Pointer[diagevent.Ring]` field bridge/bridge.go:395.
- Readers: `Bridge.DiagnosticEvents` (above); DiagnosticLogs RetrieveLogs through `ringLogs` (node/node.go:912-934; attached at node/node.go:649 `DiagnosticLogs.AttachProvider(ringLogs{n.diagRing})`; one line per event `time kind message k=v…`, CrashLogs answers none, :917-919); facade alias `node.DiagnosticEvent = diagevent.Event` (node/facade.go:57). No `Node` method returns the ring or its snapshot (grep `DiagnosticEvents` in node/*.go non-test: none; docs/guides/advanced-seams.md:203-207 says "Reading the node's ring from the facade is milestone 4, phase 21").
- Loom: reads `diagevent.Event` in its REST handler (docs/history/concept-v6-embedding-layer.md:109).
- Comparison row: docs/matterjs-comparison.md:141 "Diagnostics for a failed pairing | diagevent/ | ✅ | No matter.js equivalent".

### 1.7 Metrics and counters today

No metrics package, hook, `expvar`, Prometheus or OpenTelemetry import in non-test code (grep `expvar|prometheus|otel|MetricsSnapshot` finds nothing outside generated cluster ThreadMetrics: cluster/spec/softwarediagnostics/definition_gen.go:66, schema/attribute_access_gen.go:194; `store.MetricsSnapshot` does not exist). What exists:
- `bridge.HealthSample{Healthy bool; Note string}` (bridge/health_probe.go:41-47), `HealthRecorder` interface `Record` / `RecordUnhealthy` (:51-56), `StartHealthProbe` (:59ff; the probe checks the operational listener's `LocalAddr()`, :30-33), `DefaultProbeInterval = 30 * time.Second` (:23). Used by loom (daemon_matter.go; read map phase 20 §1).
- `node.Health{Interval; OnSample func(HealthSample)}` (node/health.go:27-36), `node.HealthSample = bridge.HealthSample` (:16), `Node.Health() (HealthSample, bool)` (:96), `node.DefaultProbeInterval` alias (:20 area); wired in examples/reference-bridge/main.go:196 (`cfg.Health.OnSample = …`). The probe runs from `Node.Start` to `Node.Stop`.
- GeneralDiagnostics DeviceLoadStatus counters: `deviceLoadCounters{imReceived, imSent, subscriptionsSucceeded atomic.Uint64}` (bridge/device_load.go:20-24), `Bridge.DeviceLoad(fabricIndex)` (:42) clamps to field widths, served by `SetDeviceLoadProvider` (:33). `upTimeHighWater atomic.Int64` cluster/core/general_diagnostics.go:127.
- Recovered-panic counters: `recoveredPanics atomic.Uint64` transport/udp/listener.go:96 with `Listener.RecoveredPanics()` :304; transport/tcp/listener.go:70 with `RecoveredPanics()` :137; transport/tcp/conn.go:64, transport/tcp/dialer.go:27. Logged with `recovered_total` (transport/udp/listener.go:276, transport/tcp/conn.go:285).
- Other counters (protocol state, not metrics): `paseFailures atomic.Int32` bridge/bridge.go:518; `unsecuredWindowCount atomic.Int64` :570; `reassemblies atomic.Int64` node/aggregator.go:111; `written atomic.Int64` bdx/sink.go:45; session-miss burst table bridge/session_miss_burst.go (logged, not exported). `Saves()` counters on in-memory test stores (client/peer/store.go:79, controller/ca/ca.go:130).
- `fabrictest` runtime/metrics use is for goroutine quiescence, not product metrics (fabrictest/site.go:248-290).

## §2 Persistence today

### 2.1 `store.Store` — a concrete type, not an interface

store/store.go:14 `type Store struct { db *sql.DB }`; `store.New(db *sql.DB) *Store` store/store.go:21. Doc: "this package never opens a database" (store/store.go:11-13). ADR 0026 principle 7 (docs/adr/0026*.md:100) calls it "`store.Store` … `endpoint.Store` … and the controller's client store are the three interfaces"; in code only `endpoint.Store` (endpoint/identity.go:92) and `controller/client.Store` (controller/client/store.go:18) are interfaces; the `*store.Store` is the struct above (node.Config takes `Store.Credentials *store.Store`, node/config.go:338).
Methods (every exported method of `*Store`, `file:line`; ctx is the first parameter unless noted):
- acl.go: `ListACL` :59, `ReplaceACL` :86. Types Privilege :14, AuthMode :26, ACLTarget :38, ACLEntry :47.
- fabric.go: `AddFabric` :71, `GetFabric` :140, `ListFabrics` :155, `UpdateFabricLabel` :180, `UpdateFabricVendorID` :201, `UpdateFabricNodeID` :225, `RemoveFabric` :245. Type FabricRecord :17.
- identity.go: `UpsertIdentity` :36, `GetIdentity` :56 (IdentityRecord :16).
- groupkeys.go: `UpsertGroupKeySet` :42, `GetGroupKeySet` :71, `ListGroupKeySets` :91, `RemoveGroupKeySet` :124, `RemoveGroupKeysByFabric` :142, `SetGroupKeyMapping` :181, `RemoveGroupKeyMapping` :194, `ListGroupKeyMappings` :205.
- grouptable.go: `UpsertGroupTableEntry` :27, `RemoveGroupTableEntry` :49, `ListGroupTable` :60.
- groupcast.go: `UpsertGroupcastGroup` :27, `RemoveGroupcastGroup` :46, `ListGroupcastGroups` :56.
- resumption.go: `UpsertResumption` :40, `GetResumptionByID` :75, `GetResumptionByPeer` :105, `RemoveResumption` :133, `RemoveResumptionsByFabric` :148.
- server_subscriptions.go: `SaveServerSubscription` :20, `DeleteServerSubscription` :37, `DeleteServerSubscriptionsByFabric` :46, `LoadServerSubscriptions` :54, `ClearServerSubscriptions` :79.
- settings.go: `GetSetting` :46, `SetSetting` :58, `GetMetadataCounter` :69, `SetMetadataCounter` :86. metadata.go: unexported next-fabric-index helpers (:20, :44; key `next_fabric_index` :15).
- ota.go: `ListDefaultOTAProviders` :29, `ReplaceDefaultOTAProviders` :63, `LoadOTARequestorState` :88, `SaveOTARequestorState` :97.
- thermostat_presets.go: `LoadThermostatPresets` :17, `SaveThermostatPresets` :35, `ThermostatPresets(endpointKey)` :65 (type ThermostatPresetStore :58).
- doorlock.go: `LoadDoorLockAuth` :114, `SaveDoorLockAuth` :235 (types DoorLockUser :35, DoorLockCredential :55, schedules :65/:77/:86, DoorLockAuth :96).
- Free functions: `Schema()` store/schema.go:39, `Apply(ctx, db)` :52, `Upgrade(ctx, db)` :76; `Migrations()` store/migrations.go:64, `SchemaFingerprint(ctx, db)` :116; `Migration{Version, Up, Down}` :29.
- Sentinel errors: store/store.go:26 onward (`ErrFabricNotFound` :28 …).
Interfaces that sit in front of parts of it: `groups.Store` (groups/manager.go:39; methods GetFabric, ListFabrics, ListGroupKeySets, ListGroupKeyMappings, ListGroupTable, Upsert/RemoveGroupTableEntry, List/Upsert/RemoveGroupcastGroup); `endpoint.dispatcher` ACL readers (endpoint/dispatcher.go:68 `AuxiliaryACL`, :77 `ListACL`, taking `store.ACLEntry`); `controller/ca.SettingStore` writes the CA through `GetSetting`/`SetSetting` (controller/ca/persist.go:224, :234).

### 2.2 SQLite: the one implementation

- Driver: `modernc.org/sqlite v1.60.1` (go.mod:11), pure Go. `store` imports only `database/sql`; the driver is imported by `node` (node/store_path.go:11, `_ "modernc.org/sqlite"`) and by examples/fabrictest through `sql.Open("sqlite", …)`.
- Files: one file per node: `node.Config.Store.Path` (opened by `openStorePath`, node/store_path.go:28-35, DSN `file:<path>?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)`, :24) or a handle `Config.Store.DB`; the credentials tables, the endpoint tables (2.4) and the settings live in that one database (node/node.go:381 `sqlitestore.Apply(ctx, sc.DB)`, :387-388 `sqlitestore.New(sc.DB)`). The reference controller opens its own file with WAL (examples/reference-controller/cli/stack.go:91).
- Schema version: no `PRAGMA user_version` and no version table in `store/*.go` (grep `user_version|schema_version` non-test: none). Idempotent DDL (`CREATE TABLE IF NOT EXISTS`, `INSERT OR IGNORE`) re-run at every boot by `store.Apply` (store/schema.go:52-58, :45-47 doc) then `Upgrade` (:76); the version is the migration list and `SchemaFingerprint` (store/migrations.go:116ff; SHA-256 over the tables/indexes the DDL declares, CHANGELOG records the value per release).
- Migrations (`migrationVersions` store/migrations.go:57 `{"v1".."v6"}`, files store/migrations/vN.{up,down}.sql, doc at :36-55): v1 v0.1.0 the DDL v0.1.0 shipped, incl. matter_group_key_map with FK; v2 v0.2.0 matter_group_table, matter_groupcast_groups, matter_server_subscriptions (+ fabric index); v3 v0.2.0 matter_group_key_map rebuilt without the FK (also `Upgrade`'s step, store/schema.go:77-90); v4 v0.3.0 matter_ota_default_providers; v5 v0.8.3 matter_thermostat_presets; v6 brief BM matter_doorlock_users, matter_doorlock_credentials, matter_doorlock_schedules.
- Tables in store/schema.sql (CREATE lines): matter_fabrics :30, matter_node_identities :57, matter_group_keys :71, matter_group_key_map :97, matter_group_table :113, matter_groupcast_groups :132, matter_acl_entries :147, matter_resumption :171, matter_diagnostics :196, matter_metadata :211, matter_settings :223, matter_persistent_subscriptions :235, matter_server_subscriptions :257, matter_ota_default_providers :274, matter_thermostat_presets :291, matter_doorlock_users :310, matter_doorlock_credentials :325, matter_doorlock_schedules :335 (18 tables). Note: `matter_diagnostics` and `matter_persistent_subscriptions` have no `Store` method in the list above (grep not exhaustive; both are in the DDL only).
- Size of package: store/*.go 6723 lines including tests.

### 2.3 Other `Store` interfaces and their implementations

| Interface | Methods | In-memory impl | SQL/file impl |
| --- | --- | --- | --- |
| `endpoint.Store` endpoint/identity.go:92 | `GetEndpoint` :94, `UpsertEndpointAssigning` :97, `ListEndpoints` :100, `RemoveEndpoint` :104 | `endpointtest.FakeStore` endpoint/endpointtest/endpointtest.go:63-66 | `endpoint/sqlitestore` (2.4) |
| `endpoint` scenes store (assembler) | `LoadScenes(key)` / `SaveScenes(key, table)` endpoint/assembler.go:74-75 (interface `ScenesStore`, :72) | not checked | not checked |
| `controller/client.Store` controller/client/store.go:18 | `Load(nodeID)`, `Save(nodeID, ep, cluster, values, version)`, `EraseCluster`, `EraseEndpoint`, `EraseNode`, `NodeIDs` | `client.MemoryStore` :54-61 (methods :65-148) | `controller/client/sqlstore` |
| `client/peer.Store` client/peer/store.go:18 | `LoadPeers(fabricIndex)`, `SavePeer`, `DeletePeer` | `peer.MemoryStore` :27-79; `peer.MemoryResumptionStore` (used at fabrictest/controller.go:124) | none found (grep `LoadPeers` implementers: only `MemoryStore`) |
| `controller/dcl.Store` controller/dcl/store.go:19 | `Load(ctx)`, `Save(ctx, data)` | `dcl.MemoryStore` :26-42 (the default, certstore.go:150) | file store `FileStore` (controller/dcl/store.go:58-80, temp file + rename) |
| `controller/ca.Store` controller/ca/ca.go:92 | `Load() (Stored, ok, err)`, `Save(Stored)`; `FabricStore` (`LoadFabric`/`SaveFabric`) controller/ca/persist.go:57-67 | `ca.MemoryStore` controller/ca/ca.go:99-130 | `ca.SettingStore` controller/ca/persist.go:224 (over `store` settings); key file controller/ca/keyfile/keyfile.go |

### 2.4 `endpoint/sqlitestore`, `controller/client/sqlstore`, `node/store_path.go`

- endpoint/sqlitestore: files sqlitestore.go, migrations.go, schema.sql, migrations/v1.{up,down}.sql; tables `matter_endpoints` (schema.sql:25), `matter_endpoint_allocation` (:53); `migrationVersions = {"v1"}` migrations.go:39; `Migrations()` :47; doc at sqlitestore.go:22 shows `sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")`; `node` applies it at node/node.go:381. Tests: allocation_test.go, race_on_test.go/race_off_test.go.
- controller/client/sqlstore: sqlstore.go, schema.go, schema.sql, migrations/v1.{up,down}.sql; tables `matter_client_values` (schema.sql:13), `matter_client_versions` (:25); `migrationVersions = {"v1"}` schema.go:66; `Migrations()` :71; header comment at sqlstore.go:53 and :131 states it matches `client.MemoryStore` semantics.
- node/store_path.go: `sqliteDriver = "sqlite"` :17, `sqlitePragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"` :24, `openStorePath(path)` :28 (node owns the handle and closes it on `Node.Stop`). `node.Config.Store` selection rule at node/config.go:494 (Path, DB, or both Credentials and Endpoints must be set); doc node/config.go:368.

### 2.5 Everything else the module opens (non-test library code, outside script/ and examples/)

The four file seams of ADR 0026 P7 (docs/adr/0026*.md:104-110) and where they open files:
1. OTA download and applied-image directories: node/ota.go (`os.MkdirAll` :113 and :223, `os.Rename` :117, `os.Remove` :109/:118, `os.Open` :166); cluster/core/ota_requestor_query.go (`os.Stat` :185, `os.Remove` :189/:388/:474, `os.Open(r.downloadPath())` :621); bdx/sink.go (`os.CreateTemp` :61/:88, `os.Rename` :109, `os.Remove`).
2. DCL file store: controller/dcl/store.go:58 (`os.ReadFile`), :67 (`os.CreateTemp`), :80 (`os.Rename`).
3. Controller CA key file: controller/ca/keyfile/keyfile.go:125 (`os.ReadFile`), :209 (`os.OpenFile` O_EXCL 0600), :215/:219 (`os.Remove`).
4. Attestation chain files: secure/attestation/files.go:129 (`os.ReadFile`).
The two residues named at gate 2 (ADR 0026 P7, lines 111-114): the OTA requestor's downloaded image (cluster/core/ota_requestor_query.go, the `os.*` sites in item 1) and the DCL snapshot fetch (controller/dcl/snapshot/fetch.go:91 `os.RemoveAll`, :94 `os.MkdirAll`, :99 and :107 `os.WriteFile`).
Not library paths: examples/reference-bridge/control.go:93 (`os.OpenFile` of the app pipe, read-only), examples/reference-bridge/ota.go:68-124 (the daemon's own copy of the node/ota.go logic), examples/reference-controller/cli/ota.go:69 (`os.ReadFile` of an image), the `script/` generators.
`sql.Open` call sites: node/store_path.go:30, fabrictest/device.go:117, examples/quickstart-controller/main.go:102, examples/quickstart-device/main.go:72, examples/reference-controller/cli/stack.go:91. `database/sql` is imported by store/*, endpoint/sqlitestore, controller/client/sqlstore, node/{config,node,store_path}.go (grep `"database/sql"`).

### 2.6 `fabrictest`'s store and in-memory stores that exist

- fabrictest device: a SQLite *file* per device in `t.TempDir()`: fabrictest/site.go:135 `s.dir = tb.TempDir()`; fabrictest/device.go:116-117 `path := filepath.Join(s.dir, "device<N>.db")`, `sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")`; not `:memory:` (grep `:memory:|mode=memory` in non-test code: none). `Device.Store() *store.Store` fabrictest/device.go:305.
- fabrictest controller: in-memory throughout: `ca.MemoryStore`, `peer.MemoryResumptionStore`, `client.MemoryStore` (fabrictest/controller.go:124; doc :52).
- In-memory store types today (grep `MemoryStore|FakeStore`): `client/peer.MemoryStore`, `peer.MemoryResumptionStore`, `controller/client.MemoryStore`, `controller/dcl.MemoryStore`, `controller/ca.MemoryStore`, `endpoint/endpointtest.FakeStore`. There is none for `*store.Store` (credentials, fabrics, ACL, groups, subscriptions, settings, presets, locks, OTA state): `store.Store` is bound to `*sql.DB` (store/store.go:14).
- Docs: docs/guides/advanced-seams.md:105 "A store of the host's own design behind a port is milestone 4, phase 21."; docs/guides/persistence.md exists; docs/matterjs-comparison.md:130 row "Pluggable storage" ◐ Low: "The `Store` interfaces are already host-implementable; SQLite is the shipped implementation".

## §3 Hardening

### 3.1 Limits and caps today (constant, file:line, docs)

| What | Value | Site | Documented in docs/ |
| --- | --- | --- | --- |
| Sessions per peer (CASE cap; LRU eviction) | `MaxSessionsPerPeer = 5` | secure/operational/manager.go:1104 (doc :1098-1103; eviction `evictExcessPeerSessionsLocked` :1123; callers :576, :697; cites `mjs:packages/protocol/src/session/SessionManager.ts:185-189 MAX_SESSIONS_PER_PEER` in the comment at :1102) | not found by grep `MaxSessionsPerPeer` in docs/ outside roadmap |
| Subscriptions per fabric | `Config.MaxSubscriptionsPerFabric`, default 16 | im/subscription/manager.go:63 (field), :183-185 (default), :268-270 (check, `ErrFabricQuotaExceeded`), im/subscription/persistence.go:97-99 | not found in docs/ |
| Subscription refusal status | `ResourceExhausted` | bridge/subscribe_dispatch.go:318-324 | not found |
| Interval ceiling | `MaxIntervalCeilingSeconds` default 3600; `TickInterval` 250 ms | im/subscription/manager.go:186-190 | not found |
| Session-id-miss burst table | `sessionMissBurstThreshold = 8`, window 15 s, cooldown 5 min, sweep 1 min, `sessionMissBurstMaxEntries = 4096` | bridge/session_miss_burst.go:55-77 | not found |
| Unsecured duplicate-detection windows | `maxUnsecuredWindows = 256` | bridge/securechannel.go:511 (doc :504-510); use bridge/receive.go:296 | not found |
| PASE failures per window / lockout | `paseMaxErrors = 20`; `paseLockoutCooldown = 15 min`, `paseLockoutMaxCooldown = 4 h` (doubling); `pasePairingTimeout = 60 s` | bridge/securechannel.go:497-502, :520-543, :518 area (see 3.2) | docs/security.md:24 (and :25 for the single in-flight PASE) |
| Fabrics (SupportedFabrics) | max 254 (`maxSupportedFabrics`), facade default 5 (`defaultSupportedFabrics`) | node/config.go:428-431 | docs/getting-started/device.md:176 and :200 |
| ACL entries per fabric | `accessControlEntriesPerFabricLimit uint16 = 4` | cluster/core/access_control.go:169 | not found |
| Groups / group keys per fabric | `defaultMaxGroupsPerFabric = 22`, `defaultMaxGroupKeysPerFabric = 20`; `groups.DefaultMaxGroupsPerFabric = 22` | cluster/core/group_key_management.go:136-137 (doc :118-134), groups/manager.go:35 | not found |
| Commands per InvokeRequest | `im.DefaultMaxPathsPerInvoke = 10`; `bridge.Config.MaxPathsPerInvoke` | im/invoke.go:44; bridge/bridge.go:289-296 | not found |
| TCP connections per peer IP; in-flight per connection | `MaxConnectionsPerPeerIP = 3`; `maxInFlightPerConn = 64` | transport/tcp/listener.go:28; transport/tcp/conn.go:33 | not found |
| Controller exchanges per session | `MaxConcurrentExchangesPerSession = 30` | client/exchange/manager.go:31 | not found |
| Controller subscription ceiling | `SubscriptionMaxIntervalPublisherLimit = 60 min` | client/interaction/subscribe.go:26 | not found |
| Commissioner fabric cap | `ErrMaximumCommissionedFabricsReached` | controller/commissioner/errors.go:23 | not found |

Grep for the doc column: `grep -rn 'MaxSessionsPerPeer|MaxSubscriptionsPerFabric|sessionMissBurst|maxUnsecuredWindows|MaxConnectionsPerPeerIP|paseMaxErrors|SupportedFabrics|MaxPathsPerInvoke' docs/` (excluding docs/history, docs/adr): hits only docs/security.md:24, docs/getting-started/device.md:176/:200 and docs/roadmap.md (status prose). The CASE-handshake count in flight has no cap constant found (grep `maxCase|caseInFlight`: none).

### 3.2 PASE attempt handling in the bridge today

- Counter: `paseFailures atomic.Int32` bridge/bridge.go:518; lockout state `paseLockoutUntil time.Time`, `paseLockoutStreak int` bridge/bridge.go:530-531; `preserveLockoutOnReset` :535 (comment :513-536).
- Constants: bridge/securechannel.go:497-502 `paseMaxErrors = 20` ("Mirrors matter.js PaseServer.ts PASE_COMMISSIONING_MAX_ERRORS (= 20)"), :541-543 `paseLockoutCooldown = 15 * time.Minute`, `paseLockoutMaxCooldown = 4 * time.Hour`, :518 `pasePairingTimeout = 60 * time.Second`; the comment at :520-540 states matter.js and chip end the refusal with the commissioning window and that this bridge needs its own expiry because an uncommissioned daemon answers PASE with no window.
- Functions: `claimPaseInFlight` :545 (single active PASE slot), `recordPaseFailure` :580 (increments; at `paseMaxErrors` engages the lockout, logs `slog.Int("max_errors", paseMaxErrors)` :593 area with hint "too many PASE pairing failures; revoking commissioning window and refusing PASE for the lockout period", records a `diagevent` KindPairing/SeverityError at :594-, then `win.RevokeWindow` :616), `engagePaseLockout` :621 (returns the cooldown, doubling per streak), `paseLockedOut` :658, `resetPaseFailures` :677. Callers: failure counted at bridge/securechannel.go:775 and :1040; lockout check on receive at :328 (log hint :341); resets at :143 and :175 (success paths) and when a window opens.
- bridge/handlers.go:343-345 and :412-470 describe the PASE brute-force cap ("matter.js counts every PASE pairing failure — a malformed Pake1 included").
- bridge/pase_provider.go: no match for `attempt|fail|lockout|retry` (grep). bridge/commissioning_window.go: `RevokeWindow` mentions :75, :96, :188, :228, :366, :634-649 (the window teardown; no counter). secure/spake2/spake2.go: only `ErrConfirmationFailed` :56 and its uses :413, :547 (no attempt state); it returns the error to the caller.
- docs: docs/security.md:24 states 20 attempts, lockout 15 min doubling to 4 h, new window resets.
- Roadmap row 21 says "PASE attempt limit after §5.5" (docs/roadmap.md:197); `grep -rn '§5.5\|5\.5' docs/history/concept-v6-embedding-layer.md` finds no section so named; I did not determine what "§5.5" refers to.

### 3.3 matter.js at 862ad75f: PASE attempt handling (file:line and the literal only)

- mjs:packages/protocol/src/session/pase/PaseServer.ts:33 `const PASE_PAIRING_TIMEOUT = Seconds(60);`
- mjs:packages/protocol/src/session/pase/PaseServer.ts:34 `const PASE_COMMISSIONING_MAX_ERRORS = 20;`
- mjs:packages/protocol/src/session/pase/PaseServer.ts:36 `export class MaximumPasePairingErrorsReachedError extends MatterFlowError {}`
- mjs:packages/protocol/src/session/pase/PaseServer.ts:44 `#pairingErrors = 0;`
- mjs:packages/protocol/src/session/pase/PaseServer.ts:95 `this.#pairingErrors++;`
- mjs:packages/protocol/src/session/pase/PaseServer.ts:97 `` `An error occurred during PASE commissioning (${this.#pairingErrors}/${PASE_COMMISSIONING_MAX_ERRORS}):` ``
- mjs:packages/protocol/src/session/pase/PaseServer.ts:106-110 `if (this.#pairingErrors >= PASE_COMMISSIONING_MAX_ERRORS) { throw new MaximumPasePairingErrorsReachedError(` … `` `Too many errors during PASE commissioning, aborting commissioning window` ``
- mjs:packages/protocol/src/session/pase/PaseServer.ts:127 `this.#pairingTimer = Time.getTimer("PASE pairing timeout", PASE_PAIRING_TIMEOUT, …`
- mjs:packages/protocol/src/securechannel/SecureChannelProtocol.ts:24 import of `MaximumPasePairingErrorsReachedError`; :94 `readonly #tooManyPaseErrors = AsyncObservable<[]>();`; :112-113 getter `tooManyPaseErrors`; :141 `MaximumPasePairingErrorsReachedError.accept(error);`; :143 `await this.#tooManyPaseErrors.emit();`
- mjs:packages/protocol/src/protocol/DeviceCommissioner.ts:79 `this.#observers.on(this.#context.secureChannelProtocol.tooManyPaseErrors, async () => {`; :80 `logger.info("Maximum number of PASE pairing errors reached, canceling commissioning");`; :81 `await this.endCommissioning();`
- mjs:packages/node/src/behavior/system/commissioning/ (CommissioningClient.ts, CommissioningServer.ts, RemoteDescriptor.ts, index.ts): `git grep -i -E 'attempt|MaximumPase|lockout'` there and in packages/node/src/node/server and packages/node/src/behaviors/administrator-commissioning finds only CommissioningClient.ts:1166 ("Abort signal for cancellation. With {@link ClientNode#commission} it cancels the PASE attempt.") and :1299 ("to bound commissioning to a single handshake attempt, so a handshake the device fails is a commissioning"), :1307 — controller-side text, not device-side attempt counting.
- `git grep` for `maximumPasePairing|MaxPairingAttempts|maxPairingAttempts|PairingAttempt|failedAttempt|FailedAttempt` over packages/{protocol,node,types}/src and packages/model/src/standard at 862ad75f: no match. The file `DeviceCommissioner.ts` is at `packages/protocol/src/protocol/`, not under packages/node.

### 3.4 Fuzz targets (grep `func Fuzz`; 24 targets, 10 packages)

| Package | Targets (first site) |
| --- | --- |
| transport/message | FuzzUnmarshalHeader transport/message/fuzz_message_test.go:27; FuzzUnmarshalProtocolHeader :80; FuzzMarshalPacket :115 |
| transport/btp | FuzzDecodePacket transport/btp/fuzz_codec_test.go:16 |
| bridge | FuzzFanStepFieldsReader bridge/fuzz_fan_fields_test.go:20; FuzzGroupcastFieldsReader bridge/fuzz_groupcast_fields_test.go:20; FuzzChangeToModeFieldsReader bridge/fuzz_modebase_fields_test.go:19 |
| secure/mattercert | FuzzDecode secure/mattercert/fuzz_decode_test.go:45 |
| secure/setup | FuzzQRCode secure/setup/fuzz_setup_test.go:34; FuzzManualCode :88; FuzzIsValidSetupPIN :131; FuzzDecodeQRCode :154; FuzzDecodeManualCode :180 |
| bdx | FuzzDecode bdx/fuzz_messages_test.go:17 |
| groups | FuzzDecode groups/fuzz_decode_test.go:16 |
| secure/channel | FuzzSessionDecrypt secure/channel/fuzz_session_test.go:43; FuzzSessionEncryptDecryptRoundTrip :89; FuzzPrivacyMask :130 |
| im | FuzzUnmarshalInvokeRequest im/fuzz_invoke_test.go:18; FuzzUnmarshalReadRequest :33; FuzzUnmarshalWriteRequest :45; FuzzUnmarshalSubscribeRequest :56 |
| tlv | FuzzDecoderNext tlv/fuzz_codec_test.go:30; FuzzEncodeDecodeRoundTrip :91 |

`make fuzz` (Makefile:105-124) discovers every package with `^func Fuzz` in a `_test.go`, lists targets with `go test -list`, runs each for `FUZZTIME ?= 100000x` (Makefile:27); `nightly.yml` job "fuzz (extended)" (.github/workflows/nightly.yml:21-35).
Packages with decoders of wire or file input and no fuzz target (candidates by `func (Decode|Parse|decode|parse)…` in non-test files; zero `Fuzz` in these directories):
- client/exchange: `DecodeStatusReport` client/exchange/errors.go:142.
- client/discovery: `ParseTxtDecimal` client/discovery/txt_data.go:71, `ParseTXTData` :101, `decodeSupportedTransports` :30.
- client/interaction: `decodeReport` client/interaction/report.go:427, `decodeGroup` :286, `readNode` :418, `readAttributeValue` :409.
- client/handshake, client/peer: no parse/decode function by that naming (handshake messages go through `secure/sigma` / `secure/spake2` / `im`; grep `Unmarshal|Decode` not run per message type).
- controller/attestation: `decodeDER` controller/attestation/cd.go:67, `parseDeclaration` :101, `decodeCDContent` :151, `decodeAttestationElements` :195, `parseAttCert` cert.go:77, `parsePeerCertificate` validate.go:323, `ParseTrustMode` trust.go:42.
- controller/commissioner: `decodeCertificateChainResponse` controller/commissioner/wire.go:103, `decodeSignedElements` :118, `decodeNOCResponse` :134, `decodeCertSigningRequest` :146, `decodeNetworks` network.go:129.
- controller/client: `decodeValue` controller/client/cache.go:124, `decodeRecord` record.go:303, `decodeNetworkAddress` :424, `decodeSessionParameters` :448, `decodeSettings` settings.go:65, `decodeFabricDescriptor` opcreds.go:50.
- controller/dcl: `parseCert` controller/dcl/cert.go:52, `parseTrustStoreCertificate` :73, `parseCRL` crl.go:324, `decodeHex` :280, `ParseRevocationSet` revocation.go:812.
- controller/ble: no parse/decode function by that naming (transport/btp has FuzzDecodePacket).
- mdns: `ParseDNSMessage` mdns/query_conn.go:125 (no Fuzz target in mdns/).
- Also without Fuzz: secure/sigma, secure/spake2, endpoint, cluster/*, node (not checked for decoders).

## §4 Packaging today

### 4.1 Executables in examples/

| Entry | `func main` | Flags / env |
| --- | --- | --- |
| examples/reference-bridge | examples/reference-bridge/main.go:78 | `--db` (default `reference-bridge.db`) :99, `--listen` `:5540` :100, `--tcp` `true` :101, `--label` :102, `--passcode` 20202021 :103, `--discriminator` 3840 :104, `--pbkdf-iterations` 1000 :105, `--pbkdf-salt` :106, `--advertise` true :107, `--log-level` info :108, `--ota-dir` :112, `--attestation-dir` :115, `--dcl` (off) :116; TESTING ONLY: `--app-pipe` :109, `--enable-key` :110, `--mdns-os-hostname` :111, `--ota-harness` :113, `--ota-provider` :114, `--wifi` :117, `--wifi-sim` :118, `--media` :119. Env: none read (`os.Getenv` not found in these files). README: examples/reference-bridge/README.md ("None of these flags belongs in a real deployment", :190-200). Logger: text handler on stderr + `slog.SetDefault` main.go:155-156. |
| examples/reference-controller | examples/reference-controller/main.go:22 (calls `cli.Run`) | global flags in examples/reference-controller/cli/cli.go:70-79: `--db`, `--dcl`, `--trust`, `--trust-dir`, `--attestation`, `--log-level`, `--listen`, `--node-listen`, `--timeout`, `--ca-key` (passphrase from env var `GOFABRIC_KEY_PASSPHRASE`, examples/reference-controller/cli/cakey.go:22; flag cli.go:79); subcommands in the `commands` map (cli.go:~140-: pair, unpair, nodes, node, read, write, invoke, subscribe, events, …). README examples/reference-controller/README.md. |
| examples/host-view | examples/host-view/main.go:125 | no flags; signal handling imports `os/signal`; explained by docs/guides/host-view.md |
| examples/quickstart-device | examples/quickstart-device/main.go:30 | no flags found |
| examples/quickstart-controller | examples/quickstart-controller/main.go:62 | `--db` (default `quickstart-controller.db`) :63, `--code` :64 |

### 4.2 Build, CI, release

- Module: `module github.com/SukramJ/go-fabric`, `go 1.27.2` (go.mod:1,3); CI `GO_VERSION: "1.27.2"` (.github/workflows/ci.yml:18); `CGO_ENABLED: "0"` (ci.yml:17, nightly.yml:16, release.yml:27).
- Makefile: `build` compiles every package, "library module — no binaries" (Makefile:58); `build-32bit` (Makefile:65-70): `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -gcflags=-e ./...` and `GOARCH=386 go vet ./...`; `archcheck` :72-74. No Makefile target builds an example binary or an image (grep `^(image|docker|install|release|cross)` in Makefile: only `chiptool-image`, `chiptool-extract` :221, which pull the CHIP image).
- ci.yml jobs (names): build, `build (32-bit)` (ci.yml:47-61, runs `make build-32bit`, added for issue 225), vet, `test (linux, race)`, coverage floor, `bench (smoke)`, reachability, testparity, `matter.js citations`, `chip data model cross-check`, lint, `ble/bluez` (ci.yml:34-305). All `runs-on: ubuntu-latest`; no `GOARCH` matrix.
- Other workflows: chiptool.yml (ubuntu-24.04; pulls the CHIP image; header says the image is multi-arch and jobs run on amd64 runners, chiptool.yml:9), ble-virtual.yml (an arm64 ubuntu-24.04 runner, ble-virtual.yml:34-36), nightly.yml (fuzz extended, macOS race, govulncheck, snapshot staleness), codeql.yml, dependabot-auto-merge.yml, lock.yml, matterjs-pin.yml, pages.yml.
- release.yml: triggered by a tag; verifies the module version, builds and tests the tagged tree (:90), extracts the CHANGELOG section into RELEASE_NOTES.md (:101-115), `gh release create` with `--notes-file` (:124-134), warms the module proxy (:141). It uploads no binary and builds no image.
- Packaging files in this repo: none (`find . -name 'Dockerfile*' -o -name '*.service' -o -name '.goreleaser*'` finds nothing; no docker-compose). The arm64-only CHIP image is the only container reference (Makefile:197-198, :221-226).
- Arm64 story: library builds as pure Go (`CGO_ENABLED=0`); the 32-bit build and vet are CI jobs; no arm64 build or test job for the examples; ble-virtual runs on an arm64 runner for a kernel reason (ble-virtual.yml:34-36).

### 4.3 What docs/ says about deployment

grep `deploy|container|systemd|docker|arm64|raspberry|cross-compile|GOARCH|armv7` over docs/ (excluding history, adr, reference) and the example READMEs: docs/roadmap.md:198 (phase 22 row), docs/history/concept-v6-embedding-layer.md:244 ("kein Container, kein Cookbook (Phase 22)"), docs/security.md:45 (an unrelated hit), examples/reference-bridge/README.md:200, examples/reference-controller/README.md:215 (ble-virtual on arm64). There is no deployment page. docs/getting-started/device.md:221 heading "What the reference daemon adds". Existing guides: docs/guides/{advanced-seams, concurrency, host-view, migrating-to-the-facade, persistence, ports, testing-in-process}.md; getting-started/{controller, device}.md.

## §5 The `go-*2mqtt` siblings as cookbook candidates

All paths relative to the parent directory of this repo. LOC = non-test `.go` lines / test lines. No sibling imports go-fabric (`grep -c go-fabric go.mod` = 0 in all nine).

| Repo | Module | go | Direct deps | LOC (src / test) | Build and packaging |
| --- | --- | --- | --- | --- | --- |
| go-daikin2mqtt | github.com/SukramJ/go-daikin2mqtt | 1.27.2 | go-ha-catalog, go-hamqtt, go-mqtt, x/sync, yaml.v3 | 14739 / 18755 | Dockerfile (golang:1.27.2-alpine builder, `CGO_ENABLED=0`, `gcr.io/distroless/static-debian12:nonroot`, EXPOSE 8080, ENTRYPOINT `/app/daikin2mqtt`), `addon/` (HA add-on: Dockerfile, config.yaml with `arch:` incl. aarch64, build.yaml), workflows addon-image, ci, codeql, dependabot-auto-merge, docker-build-push, release-on-tag; no goreleaser, no systemd unit |
| go-homeconnect2mqtt | github.com/SukramJ/go-homeconnect2mqtt | 1.27.2 | go-ha-catalog, go-hamqtt, go-mqtt, coder/websocket, x/sync, yaml.v3 | 13391 / 18788 | same pattern (Dockerfile distroless, `addon/`, addon-image.yml pushes `ghcr.io/sukramj/go-homeconnect2mqtt-addon-{arch}`); no docker-build-push.yml |
| go-mtec2mqtt | github.com/SukramJ/go-mtec2mqtt | 1.27.2 | go-ha-catalog, go-hamqtt, go-mqtt, x/sync, yaml.v3 | 9453 / 16367 | same; Dockerfile has a `--healthcheck` flag in exec form (Dockerfile:63-67) |
| go-unifi2mqtt | github.com/SukramJ/go-unifi2mqtt | 1.27.2 | same five as daikin | 13681 / 17065 | same; `addon/translations` |
| go-zendure2mqtt | github.com/SukramJ/go-zendure2mqtt | 1.27.2 | same five as daikin | 5789 / 8587 | same |
| go-hamqtt | github.com/SukramJ/go-hamqtt | 1.27.2 | go-ha-catalog, go-mqtt | 16133 / 20097 | library; cmd/hadoctor, cmd/hacheck; no Dockerfile |
| go-mqtt | github.com/SukramJ/go-mqtt | 1.27.2 | none | 8374 / 14179 | library (MQTT client; protocol/ subpackage) |
| go-ha-catalog | github.com/SukramJ/go-ha-catalog | 1.27.2 | none | 1907 / 543 | data artifact (see below) |
| go-openccu-data | github.com/SukramJ/go-openccu-data | 1.27.2 | none | 107 / 103 | data artifact (see below) |

No sibling has a systemd unit, goreleaser file or docker-compose file (`find` for `*.service|.goreleaser*|docker-compose*`).

### go-daikin2mqtt
- Device shape: `model.Device{ID, Model, ManagementPoints []ManagementPoint, Raw}` internal/daikin/model/model.go:25; `ManagementPoint{EmbeddedID, Type, Category, Characteristics map[string]Characteristic}` :35; `Characteristic{Value json.RawMessage, Settable, MinValue/MaxValue/StepValue *float64, Values []string}` :45; `DataPoint` :273.
- Registry/entity abstraction: `coordinator.Coordinator` internal/coordinator/coordinator.go:94 (+ `CloudClient` interface :46, `Deps` :52); entity catalog in internal/catalog (3 files), HA publishing in internal/hass (5 files). Entities come from `go-hamqtt`'s model (below).
- Layout: 12 files in coordinator, 5 hass, 4 web, 4 schedule, 4 daikin/auth; `characteristics.yaml` at the root; two commands (`cmd/daikin2mqtt`, `cmd/daikin2mqtt-util`).
- Matter hook: management point is the natural endpoint unit, characteristic is the attribute-like unit (not verified beyond the types).

### go-homeconnect2mqtt
- Device shape: `homeconnect.Appliance` internal/homeconnect/appliance.go:19; `homeconnect.Entity{Desc *profile.Entry, value/min/max/step state under a mutex}` internal/homeconnect/entities.go:17; `profile.DeviceProfile` internal/profile/archive.go:73, `profile.Description` description.go:77, `profile.Entry` :33, `profile.DeviceConfig` devices.go:24.
- Registry: `bridge.Bridge` internal/bridge/bridge.go:55 over `DeviceSpec{Config, Description}` :30; `bridge.Device` internal/bridge/device.go:53 ("one appliance worker: appliance + reconnect manager + topics").
- HA side: internal/haplane/plane.go (`Plane` :125, `Config` :58), internal/hass.
- Layout: internal/{homeconnect 13 files, profile 8, bridge 8, hass 4, config 4, web 3, haplane 3, state 2, i18n 2, slug, pincatalog}; `mapping.yaml`, `devices-template.yaml`.

### go-mtec2mqtt
- Device shape: register-driven: `registers.Register{Key, Address uint16, Name, NameDE, …}` internal/registers/register.go:91, `Cluster` internal/registers/cluster.go:30, `Map` register.go:218; `DataType` :16, `Group` :41; data in `registers.yaml`.
- Registry: `coordinator.Coordinator` internal/coordinator/coordinator.go:267 over `ModbusClient`/`MQTTPublisher`/`MQTTSubscriber`/`Reader` interfaces :58-80; `state.Store` internal/state/store.go:18 with `Snapshot`, `GroupView`, `Health` (types.go:22-56).
- One inverter per daemon (a single device; no device list seen in the type names).
- Layout: coordinator 9 files, registers 4, modbus 2 (+ protocol), hass 2.

### go-unifi2mqtt
- Device shape: `model.Site` internal/model/model.go:28, `model.Device{MAC, ID, ClassicID, Name, Model, Type DeviceType, IP, State, Supported, Firmware, …}` :115, `DeviceDetail` :167, `DeviceStats` :176, `Port` :202, `PoEState` :214, `Radio` :228, `Client` :261, `Network` :318.
- Registry: `state.Store` internal/state/store.go:29 with `DeviceView` :55, `ClientView` :62, `Snapshot` :199; `coordinator.Coordinator` internal/coordinator/coordinator.go:135 with `Source` interface :63 and `Capabilities` :50; `Publisher` internal/coordinator/publish.go:37; `Subscriber` discovery.go:28.
- Layout: coordinator 13 files, hass 8, unifi/{integration, classic} 3+3, model 3; CONCEPT.md at the root.

### go-zendure2mqtt
- Device shape: `source.Device{SN, DeviceName, DeviceID, ProductKey, Model, Address}` internal/source/source.go:24, `source.Reading{Device, Report *model.Report}` :44, `model.Report` internal/zendure/model/model.go:22, `model.WriteRequest` :51; `state.Device`/`Entry`/`Snapshot`/`Store` internal/state/state.go:20-45.
- Registry: `source.Source` interface :55 (`Devices()`, `Run(ctx, Handler)`, `Read(ctx, dev)`), `Controller` :66, `Backend` :71, `Observer` :83, `Observable` :98; `coordinator.Coordinator` internal/coordinator/coordinator.go:73; `virtual.Switch` internal/virtual/virtual.go:13.
- Smallest of the five (5789 source lines); local and cloud backends behind one interface.
- Layout: config 4, zendure/{local, cloud, model}, catalog 2, hass 2, harender.

### go-hamqtt (the shared HA vocabulary layer)
- Entity shape: `model.Entity` interface (`Key()`, `Platform() hacatalog.Platform`, `Desc() *Description`, `Bindings() []Binding`) go-hamqtt/model/entity.go:27-43; `model.Basic` :45; `model.Command` :80; capability interfaces `Commander` :107, `Invoker` :129, `Suppressor` :142, `Deriver` :152, `EntitySource` :162, `Enricher` :179; `model.Device` model/device.go:169, `Identity` :81, `Identifier` :31, `Connection` :69; `model.Description` model/description.go:180, `Availability` :113; `model.Slot` model/slot.go:78, `Binding` :217; `model.State` model/state.go:23.
- Publisher side: `publisher.Instance` publisher/instance.go:111 (+ `InstanceConfig` :53), `CommandRouter` publisher/command.go:508, `Transport` publisher/publisher.go:79, `AvailabilityPublisher` publisher/availability.go:136.
- Consumer shape: all five 2mqtt daemons depend on it; its `Entity` + `Device` + `Platform` are the common vocabulary a Matter projection would read (this is the repo docs/history/concept-v6-embedding-layer.md:273-296 calls the source of the `go-hamatter` vocabulary).

### go-mqtt
- A zero-dependency MQTT client (client.go, session.go, publish.go, pump.go, breaker.go, adapter_tcp.go; protocol/ subpackage). No device model.

### go-ha-catalog, go-openccu-data
- go-ha-catalog: "the Home Assistant MQTT-discovery vocabulary as a versioned Go artifact" (README.md:3-9): device classes, state classes, units, entity features, icons, abbreviation tables, per-platform discovery schema, as typed constants and tables from `data/*.json`; a data artifact, not a lookup framework; `script/gen`, `script/hadiff`.
- go-openccu-data: "openccu-data as a versioned Go artifact" (README.md:3-10): OCCU/OpenCCU metadata extracts (translations, easymodes, MASTER-paramset profiles, curated overlays, device semantics, WebUI device images) embedded in a Go module with thin typed accessors; regenerated per upstream release; 107 source lines.

## §6 The comparison and roadmap

### 6.1 docs/matterjs-comparison.md rows still ◐ or ○ (sections "6. Device model and composition" :120-130 and "7. Runtime and tooling" :132-141)

State legend :16-22. Rows with ◐/○ (awk over lines 120-144):
- :129 "Cluster servers **generated** from the model" — ◐ Low. Text: cluster definitions generated per cluster under `cluster/spec/` by `script/clustergen`; "`cluster/pump`, `cluster/modebase`, … `cluster/thermo` and `cluster/switch` are built on it; every other cluster of the snapshot generates, compiles and round-trips matter.js's wire fixtures in a test. The remaining servers are listed in the ADR with what each migration needs."
- :130 "Pluggable storage (`general/src/storage`)" — ◐ **Low**: "SQLite (`store/`, `endpoint/sqlitestore`) | ◐ | **Low** | The `Store` interfaces are already host-implementable; SQLite is the shipped implementation, not the only possible one."
- :139 "`packages/cli-tool`, `nodejs-shell` — interactive shells" — ○ Low: "`examples/reference-bridge` covers the "run it and pair it" need."
- :140 "`packages/mqtt`, `react-native`, `nodejs-ws` — runtime bindings" — ○ **No**: "Host concerns. This module is a library; the host owns its own surfaces."
- Row :128 "Behaviour layer: state, transactions, events, `Behavior.with(...)` mixins" is now ✅ (it was the ◐ the roadmap's "Done when" exempts as "the behaviour-layer idiom row").
All other rows in those two sections are ✅. Concept v6 said the same in advance: docs/history/concept-v6-embedding-layer.md:61-64 ("Drei ◐ … das Behaviour-Idiom (bleibt per Roadmap), die generierten Cluster-Server (ADR 0013/0017) und Pluggable Storage (Phase 21)") and :229-235 (row 129 becomes ✅ after phase 20, row 130 stays ◐ until phase 21, row 128 stays per roadmap) — the roadmap status at docs/roadmap.md:216 does not say row 129 moved; the file as read still shows ◐ at :129.
The comparison cites phase 21 by name only in the "Pluggable storage" row's neighbours: docs/guides/advanced-seams.md:105 and :207.

### 6.2 docs/roadmap.md, milestone 4

- Heading :186 "### Milestone 4: The embedding layer".
- Row 21 (:197), verbatim: "| 21 | observability and persistence as ports: `slog` attribute conventions, a metrics hook, the diagnostic event ring; the `Store` seam proved by a second implementation (in-memory, for tests and small daemons); hardening a daemon in the field needs (session and subscription limits documented, PASE attempt limit after §5.5, fuzz targets for the controller decoders) | the persistence and concurrency guides describe the ports; `make fuzz` covers the new targets |"
- Row 22 (:198), verbatim: "| 22 | packaging and the cookbook: container image and systemd unit for the reference apps, arm64, the "embed in a daemon" guide written against a `go-*2mqtt` project; the concept documents of the go-ha-matter-bridge and the adapter module | a 2mqtt daemon exposes one device type end to end on the kit |"
- Value (:200-208): "The diamond-standard claim lives here, and it is a claim about embedding and operation, not about Matter semantics: **ahead of matter.js in embedding and operation, equal in semantics** (owner, 2026-10-19). matter.js asks a host to learn behaviours, agents and transactions; this module should ask for a device type and a handful of callbacks, and ship as one static binary with a small footprint. On the wire it is a port of matter.js and can be as correct as its source, not more; the ecosystem exceptions stay matter.js's. Consumers: every one; the HA bridge and the 2mqtt family cannot start before it."
- Done when (:210-212), verbatim: "**Done when.** The comparison's "device model" and "runtime" sections have no ◐ left except the behaviour-layer idiom row; loom and one 2mqtt daemon build on the facade alone."
- Status 2026-10-22 (:216) ends: "Phases 21 and 22 next; milestone 4 closes with `v0.11.0`." and records `v0.10.1` (brief BV-0, issues 220-225: 32-bit build with a CI cross-compile job, `device.HostServer`, `device.Measurement`, …) and `v0.10.2` (BV-1, issues 228-232). The line "Left for BV after the tag: the typed controller reads and writes … `apidiff`" is in the same paragraph.
- Consumer table rows: docs/roadmap.md:32 loom ("imports 32 packages of this module in its product code (45 with tests) at its `v0.6.0` pin"), :33 go-ha-matter-bridge ("not started; its concept is written when milestone 4 is due"), :34 the `go-*2mqtt` family via the shared adapter module ("working name `go-hamatter` … lives outside this module, so `contract/` stays stdlib-only"; "the same device kit; energy and appliance device types, …").
- Roadmap :190-192: concept v6 holds "the drafts of the go-ha-matter-bridge and adapter-module concepts (Teil B/C) … the two drafts move to their own repositories in phase 22."

### 6.3 docs/history/concept-v6-embedding-layer.md (376 lines, German)

Headings: :1 title; :21 "## 0. Entscheidungen des Owners (2026-10-20)"; :36 "## Teil A: Phase 20"; :38 A.1; :83 A.2; :117 A.3; :202 A.4; :229 A.5 "Vergleichstabelle und Roadmap"; :238 A.6 "Was Phase 20 nicht ist"; :248 "## Teil B: go-ha-matter-bridge (Entwurf)"; :273 "## Teil C: Das Adapter-Modul `go-hamatter` (Entwurf)"; :298 "## Teil D: Wellen"; :314 "## Teil E: Was sich an Grenzen ändert"; :328 "## Teil F: Risiken"; :349 "## Ergänzungen (Owner, 2026-10-20, nach dem Review des Konzepts)".
Sentences naming phase 21 / 22 (grep `Phase 2[12]`): :27 (decision 0.3: "Phase 22 verschiebt sie in ihre Repositories."), :64 ("Pluggable Storage (Phase 21)"), :109 ("der Ring selbst ist Phase 21 („diagnostic event ring")"), :199 ("`until` (Phase 21: Session-Limits; der Extension-Hook ist ein Sicherheitspfad und mein Lesen)"), :233 ("Zeile 130 (Storage) bleibt ◐ bis Phase 21"), :243-244 ("Kein zweiter Store, keine Metriken, keine Limits (Phase 21); kein Container, kein Cookbook (Phase 22)."), :251 (Teil B moves "mit Phase 22 in das eigene Repository"), :277 (Teil C "Zieht mit Phase 22 um."), :288 (Teil C quotes the roadmap "a 2mqtt daemon exposes one device type end to end on the kit", Phase 22), :312 ("`v0.10.0` schließt Meilenstein 4 mit Phase 22." — superseded by :373-376, "Meilenstein 4 schließt mit `v0.11.0`"), :347 (Teil F: "ACL-Anwendung … bleiben mein Lesen (Phase 21 oder später)").

Teil B (:248-271), the bullets: Zweck (HA entities as Matter bridge for Apple Home, Google Home and Alexa "als ein statisches Go-Binary auf diesem Modul", :254-257); Datenquelle (HA WebSocket API: entity registry, device registry, `state_changed`, `call_service`; "eine Entität ist eine Gerätedeklaration des Kits (A.3): Domäne und Attribute → Gerätetyp und Cluster-Patch, `call_service` → Command-Handler", :258-262); the mapping HA vocabulary → Matter device type "liegt nicht in der Bridge, sondern im Adapter-Modul (Teil C), damit die `go-*2mqtt`-Familie dieselbe benutzt" (:263-264); Tests on `fabrictest` (:266-268: commissioned, an HA entity appears as an endpoint, a controller invoke becomes `call_service`); "Was Phase 20 festlegt" (:270-271: the declaration form A.3, the facade scope A.2 "keine Imports unterhalb von `node`", the kit errors (ConformanceError), the apidiff guard as a promise to a consumer outside this repo).

Teil C (:273-296): Zweck (:278-283: "Eine Tabelle und ihre Laufzeitform: HA-Komponente (`light`, `switch`, `cover`, `climate`, `lock`, `fan`, `sensor`, `binary_sensor`, `number`, `select`, …) mit ihren Attributen und Fähigkeiten (`supported_color_modes`, `hvac_modes`, `device_class`) → Gerätetyp des Kits mit Cluster-Deklaration (A.3), und zurück: Matter-Command → HA-Service oder MQTT-Kommando"); Konsumenten (:284-289: the five `go-*2mqtt` daemons speak HA MQTT discovery; "mit dem Adapter wird jeder mit demselben Vokabular eine Matter-Bridge"; the HA bridge is the second consumer); Grenze (:290-293: the adapter imports `node`, `device`, `contract`, `schema`, `eligibility` and the `<cluster>def` packages — "genau die Liste aus A.2"; first consumer where measurement 0.4 applies without legacy); Eligibility (:294-296: which device types an ecosystem accepts is `eligibility`'s knowledge (`Classify`, `EcosystemForVendor`); the adapter picks the type by it). Working name `go-hamatter` is stated at :273 and docs/roadmap.md:34.

## Not determined / caveats

- "PASE attempt limit after §5.5" (docs/roadmap.md:197): I could not find what §5.5 refers to in docs/ or the concept; the device-side limit already exists (3.2), so what the row still asks for is not clear from the repo.
- Whether `matter_diagnostics` and `matter_persistent_subscriptions` (store/schema.sql:196, :235) are read or written by any code: not checked (no `Store` method by those names).
- Whether `endpoint/sqlitestore` also serves `LoadScenes`/`SaveScenes` (endpoint/assembler.go:74-75): not checked.
- Attribute-key counts include `examples/`; the reference-bridge-only keys (`device`, `hue`, …) are an example convention, not a library one.
- The 709-message prefix table counts calls whose first literal argument is a string; `slog.LevelX`-parameterised helpers and calls that build the message dynamically are missed.
- Sibling repos were read as checked out (working tree), not at a tag; LOC are `wc -l` of non-test `.go` files; their runtime behaviour was not read.
- No loom checkout was read for this map.
