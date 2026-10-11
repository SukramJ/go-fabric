# Brief Q — the node facade `node/` and the OnOff server `cluster/onoff.Server` (phase 4, ADR 0018)

Owner: Fable. Implementer: Opus. Branch `wave6/node` from
`planning/briefs-wave-6`, pushed, **no PR**. Report in
`notes/briefs/reports/Q.md`. Two deliverables; commit the OnOff server
first (brief P's default table and brief R wait on it).

Read first: [ADR 0018](../../docs/adr/0018-device-layer-and-node-facade.md)
(decisions 2 and 5 are binding), `bridge/bridge.go`'s package doc (:1-146),
`notes/briefs/README.md`.

## Part 1 — `cluster/onoff.Server`

### Sources
matter.js `packages/node/src/behaviors/on-off/OnOffServer.ts` (345 lines,
HEAD `4633bfd7`; cite the lines you read):
- `OnOffBaseServer extends OnOffBehavior.with(Lighting)` (:17, :30); the
  OffOnly and Lighting features are "automatically supported because the
  commands are disabled by conformance" (:26).
- `initialize` (:33-68): StartUpOnOff applied at boot unless the boot
  reason is SoftwareUpdateCompleted (:36-47: Toggle inverts, On / Off set);
  `onTime$Changed` / `offWaitTime$Changed` stop the held timers (:56-57,
  `#stopHeldTimer` :71-83: a timer stops when its attribute is 0 or
  0xFFFF); `onOff$Changed` clears pending scene data (:62).
- `on` (:97-113): sets OnOff true, GlobalSceneControl true; with OnTime 0
  stops the delayed-off timer and sets OffWaitTime 0 (:104-110).
- `off` (:119-139): OnOff false; stops the timed-on timer, OnTime 0; with
  OffWaitTime in 1..0xFFFE starts the delayed-off timer (:131-137).
- `toggle` (:146-156) calls `on` / `off`; `offWithEffect` (:158-169) and
  `onWithRecallGlobalScene` (:171-192) follow GlobalSceneControl;
  `onWithTimedOff` (:198-225): with OffWaitTime > 0 and OnOff false, only
  OffWaitTime is lowered (:203-212); else OnTime = max, OffWaitTime set,
  timer started unless OnTime is 0 or 0xFFFF (:216-223).
- Timers: periodic 100 ms (:227-254: OnTime counts down; at 0 → `off`
  and OffWaitTime 0; 0xFFFF holds), delayed-off (:255-325: OffWaitTime
  counts down; 0xFFFF holds). Scene apply with transition time (:268-305)
  may be left to ScenesManagement's existing extension points — say so.
- The TLV shapes are generated: `cluster/spec/onoff/definition_gen.go`
  (`Definition`, `Feature*`, `Attr*`, `Cmd*`, `OnWithTimedOffRequest`).
  The reference daemon's hand-written `onOffServer`
  (`examples/reference-bridge/fleet.go:191-260`) is **not** a source; it is
  what brief R replaces.

### Must
1. `cluster/onoff.Server` on `spec.Server` (as `cluster/boolcfg` and
   `cluster/filter` are): `onoff.New(cfg Config) (*Server, error)` with
   `Config{Features onoff.Feature; DataVersion; Sink onoff.Sink (optional host hook: SetOnOff(ctx, bool) error, refuses the change); Initial map[uint32]any; BootReason func() uint8 (optional); Clock (for tests)}`;
   commands through `Handle`; the timers as goroutine-free tickers driven
   by a `clock` seam so tests are deterministic; `Set(on bool)` for the
   host; `OnOff() bool`. Implements `contract.ClusterQuiescer` (stop
   timers). Attribute, command and feature lists come from the definition.
2. Tests: every rule above as a table (`onoff_server_test.go`), a parity
   test `onoff_parity_matterjs_test.go` with the matter.js lines, race.
   Keep `cluster/onoff`'s existing constants and functions as they are
   (public API).
3. CHANGELOG → Added.

## Part 2 — `node/`

### Sources
- matter.js `packages/node/src/node/ServerNode.ts:106-126` (`create(type?, options?)` / `create(config)`),
  `:264-298` (the root endpoint composition: the generated Root plus
  Commissioning, Network, ProductDescription, Subscriptions, Sessions,
  Events, AccessControl with Extension / Auxiliary, Groupcast);
  `Node.ts:48-55` (environment, generated id), `:113-131` (`start`:
  idempotent, runtime created, network startup), `:178-181` (`run`),
  `:188` (`stop`), `:223` (`close`); configuration keys are behaviour ids:
  `network` (`NetworkServer.ts:202-222`: `port`, `listeningAddressIpv4/Ipv6`,
  `ipv4`, `tcp`, `transportPreference`, `subscriptionOptions`),
  `commissioning` (`CommissioningServer.ts:370-380`: `passcode`,
  `discriminator`, `flowType`, `pairingCodes`), `productDescription`
  (`ProductDescriptionServer.ts:70-90`: `name`, `deviceType`, `vendorId`,
  `productId`), `basicInformation` (the example
  `examples/device-bridge-onoff/src/BridgedDevicesNode.ts:29-65`:
  vendorName, vendorId, nodeLabel, productName, productLabel, productId,
  serialNumber, uniqueId), `:129` (`server.start()`), `:77-78`
  (`new Endpoint(AggregatorEndpoint, {id:"aggregator"}); server.add(aggregator)`).
- This repo — the specification the facade is tested against:
  `bridge/bridge.go:1-146` (every port in call order, what a skip costs),
  `examples/reference-bridge/main.go:84-497` (the bring-up: flags,
  identity, store, groups, assembler config, mDNS, `bridge.New`, ACL,
  group messaging, auxiliary ACL, diagnostic events, root clusters,
  aggregator clusters, `wireSecurity`, commissioning window, fabric hooks,
  event counter, `Start`, enable key, diag logs provider, StartUp /
  BootReason events, operational time, parts-list providers, node
  providers, fabrics loaded and CASE identities restored,
  `ReestablishFormerSubscriptions`, OTA requestor start, `AnnounceFabric`,
  own window when uncommissioned), `wiring.go:93-360` (`buildRootClusters`,
  `buildAggregatorClusters`, `attachPartsListProviders`), `:583-700`
  (`wireSecurity`: operational manager, session lookup, ack tracker,
  subscription manager, session registry, CASE provider, subscription
  store, operational resolver, CASE initiator provider), `commissioning.go:54-170`
  (window, PASE provider), `persist.go`, `ota.go:183` (`buildOtaRequestor`),
  `tcp.go:19` (`parseTCPFlag`). Brief M / N / O reports in
  `notes/briefs/reports/` explain the newer ports (TCP, OTA, session
  parameters).
- `endpoint/assembler.go` (`Config`, `New`, `Assemble`), `endpoint/types.go`
  `Snapshot`, `bridge.Snapshotter` (`bridge/bridge.go:218`).
- Brief P defines `device.Device` with `Spec() endpoint.Spec`, `Servers()`,
  `AddPart`. Until P's branch is merged, define the seam in `node/` as a
  small interface `node.Endpoint interface{ Spec() endpoint.Spec }` and
  test the aggregator with a stub; do not import `device/`.

### Must
1. `node.Config` (code-first, as ADR 0018 decision 5): `Network{Listen string; PreferIPv4 bool; TCP bridge.TCPConfig}`,
   `Commissioning{Passcode uint32; Discriminator uint16; PBKDFIterations int; Salt []byte}`,
   `ProductDescription{Name string; DeviceType uint16; VendorID, ProductID uint16}`,
   `BasicInformation{VendorName, ProductName, ProductLabel, NodeLabel, SerialNumber, SoftwareVersionString string; SoftwareVersion, HardwareVersion …}`
   (take the field set `buildRootClusters` configures; cite),
   `Store{DB *sql.DB}` or the two store interfaces the daemon opens
   (`store.New(db)`, `sqlitestore.New(db)`) — prefer the `*sql.DB` form
   with the interfaces as an override, `Advertiser mdns.Advertiser` (nil →
   noop, as `bridge.New` does), `DeviceTypeValidation endpoint.DeviceTypeValidationMode`,
   `Attestation` (the test chain by default; the daemon's
   `attestation.BuildTestChain`), `OTA *OtaConfig` (optional: dir,
   harness flag, applier), `Logger`. `Validate()` with the daemon's checks
   (passcode, discriminator, PBKDF range).
2. `node.New(cfg) (*Node, error)` builds everything `main.go` builds
   before `Start` and wires every port in the documented order; nothing
   left as a noop unless the config omits it on purpose (document which).
   `Start(ctx)`: the daemon's post-start sequence; `Stop(ctx)`;
   `Run(ctx)` = Start then wait, as `Node.run`. Accessors for a host that
   needs a port: `Bridge() *bridge.Bridge`, `Root() *RootClusters` (the
   struct of root servers `buildRootClusters` returns), `Aggregator() *Aggregator`,
   `CommissioningWindow()`, `Fabrics()`.
3. `node.Aggregator`: `Add(ctx, node.Endpoint) error`, `Remove(ctx, key endpoint.SourceKey) error`,
   `Endpoints()`; holds the set, builds the `endpoint.Snapshot{Scope, Endpoints, ModelComplete: true}`
   for the assembler, and triggers `bridge.Reassemble` when the bridge is
   started (before `Start`, the first assembly is `Start`'s). NodeLabel
   writes and ConfigurationVersion persist as the daemon's `labels` /
   `configVersions` do (`persist.go`; `endpoint.Config.OnNodeLabelWritten`).
   Multiple scopes are not needed; one scope `"node"`.
4. Tests (`node/`): `New` with a minimal config wires the ports the
   package doc lists — assert through the bridge's observable behaviour,
   not reflection: a PASE handshake answered, a read through the
   dispatcher, a subscription report; `Start` / `Stop` idempotence; `Add`
   before and after `Start` (Reassemble path); `Remove` drops the endpoint
   and persists the id for a re-add (`BD-Matter-EndpointID-Persistent`);
   `Run` stops with the context. Use the in-package test helpers the
   bridge and daemon tests already have (`bridge/bridgetest`,
   `endpoint/endpointtest`, the daemon's `wiring_test.go` as a pattern).
   Race on `node/`.
5. CHANGELOG → Added (public API). Reachability after every commit;
   coverage floor for `node/` and `cluster/onoff`; `make lint`.

Not in scope: `device/` (brief P), the daemon's migration (brief R; do
not edit `examples/reference-bridge` except to extract a helper you move
into `node/` — then leave the daemon calling the moved helper so it still
builds and its tests pass), `secure/`, `commissioning/` internals (wire
them, do not change them).

## Guards
`go test ./node/... ./cluster/onoff/... ./bridge/... ./examples/...`,
`make race` for `node/`, `cluster/onoff/`, `make lint`, `make cover-check`,
`make reachability`, `go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'`.
Report: what was built, which guard proves it, what was left out and why,
every open question, by_design / findings candidates.
