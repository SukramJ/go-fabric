# Changelog

All notable changes to go-fabric are recorded in this file.
The project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The module carries its own version lane, independent of any host that embeds
it. `v0.1.0` is its first tag; everything before it was consumed as a
pseudo-version of `main`.

## [Unreleased]

### Added

- `controller/discovery`, the controller's DNS-SD browser (ADR 0020,
  stage 1): `Names` / `Name`, the record cache of matter.js `DnssdNames` /
  `DnssdName` at `0ceb202d` — records keyed as matter.js keys them, TTLs
  with the 1.05 grace factor and the 15 s floor, goodbyes and cache-flush
  supersession after the 1 s eviction delay, unavailable SRVs ignored, TXT
  parameters by matter.js's rules, the Matter service-type ingress filter,
  and A/AAAA staging for SRV targets from relevant packets only.
- `discovery.Service`, the live view of one DNS-SD service name (matter.js
  `IpService`): SRV targets keyed target:port, `{ip, port}` addresses with
  fe80:: zoned to the receiving interface, coalesced change notification,
  TXT changes detected by parameter signature, `AddressChanges` (deletions
  first, then additions in desirability order), and
  `discovery.CompareDesirability` / `discovery.SelectionPreference`
  (link-local, ULA, global IPv6, IPv4; then health; then SRV priority).
- `discovery.Solicitor`, the query loop of matter.js `DnssdSolicitor`:
  solicitations of one loop task go out as one packet whose known answers
  are the names' cached records without the cache-flush bit; `Discover`
  sends the first query at once and repeats on `discovery.DefaultRetries`
  (1 s, ×2, jitter up to 20 % added before the 1 h cap), the first caller
  per name owning the schedule and later callers joining it.
- `discovery.TXTData` / `discovery.ParseTXTData`, the TXT parser of
  matter.js `DiscoveryData` (`Scanner.ts`): `ParseTxtDecimal`
  (`^(0|[1-9]\d*)$` up to a maximum, else absent), DN/RI/PI verbatim, VP
  with vendor and product parsed independently, DT, PH (0 illegal), ICD,
  the `T` transports bitmap, SII/SAI up to one hour and SAT 1..65535 ms —
  an invalid value is absent so the MRP defaults apply.
  `Service.TXTData` serves operational TXT records the same way.
- `discovery.Scanner`, matter.js `CommissionableMdnsScanner`:
  `FindCommissionable(ctx, Identifier, found)` reports cached matches
  first and then queries `_matterc._udp.local` plus the identifier's
  `_L`/`_S`/`_V`/`_T`/`_CM` subtype or instance name, retrying at most
  every 30 s, until ctx ends or `Cancel` cancels it; `Discovered` reads
  the cache only. PTR targets without SRV/TXT are solicited speculatively
  (at most 50, forgotten after an hour); an instance becomes a
  `discovery.Device` once its TXT carries a valid D and CM, and is
  reported once it has an address. Identifiers: `ByInstanceID`,
  `ByLongDiscriminator`, `ByShortDiscriminator`, `ByVendor`,
  `ByVendorProduct`, `ByDeviceType`, `ByProduct`, `AnyCommissionable`.
- `discovery.Names.Service(compressedFabricID, nodeID)` gives a node's
  operational service (`discovery.OperationalQName`, matter.js
  `getOperationalDeviceQname`), and `Service.Resolve` runs matter.js
  `IpServiceResolution`: SRV and TXT for the instance with its SRV targets
  as known answers, A/AAAA for each target without an address, on the
  default schedule, until an address not known before appears or ctx
  ends. `mdns.OperationalResolver` stays for the bridge (ADR 0008).
- `mdns.QueryConn` / `mdns.OpenQueryConn` export the multicast query
  transport `mdns.OperationalResolver` already used, and
  `mdns.ParseDNSMessage` / `mdns.PackDNSQuery` with `mdns.DNSRecord`,
  `mdns.DNSQuestion`, `mdns.DNSMessage`, `mdns.DNSSRV` and `mdns.DNSType`
  give the browser a typed record codec. `OperationalResolver` behaves as
  before.
- `controller/exchange`, the controller's message layer (ADR 0020, stage 1):
  a `Manager` over one UDP socket with the session table (the unsecured
  session of a handshake, secure PASE / CASE sessions over
  `secure/channel`), the exchange table and the receive path, and
  `Exchange` with MRP — reliable `Send` with retransmission on the
  §4.12.2.1 schedule and the final wait, piggybacked and standalone acks,
  duplicate detection, `Next` with the maximum peer response time,
  graceful `Close` and `Abort`, the 30-exchange cap per session, session
  id allocation with reuse of the least recently used session's id, and
  `CloseSession`. Ported from matter.js `ExchangeManager.ts` and
  `MessageExchange.ts` at `0ceb202d`.
- `controller/handshake.PASE`, the PASE client (ADR 0020, stage 1): the
  PBKDF parameter exchange, SPAKE2+ Pake1–Pake3 over `secure/spake2`'s
  prover and the PASE session on a `controller/exchange.Manager`; a wrong
  passcode is `ErrPasscodeMismatch`, a failure the device can be told of
  is reported with StatusReport InvalidParam (also when the context was
  cancelled), and the context no longer cancels once Pake3 is sent.
  Ported from matter.js `PaseClient.ts` at `0ceb202d`.
- `controller/handshake.CASE`, the CASE client (ADR 0020, stage 1): Sigma1
  with or without a resumption offer from a `ResumptionStore`
  (`MemoryResumptionStore` in memory), Sigma2Resume with the resume MIC
  and the record moved to the new resumption id, Sigma2 with the
  responder's NOC bound to the expected node and fabric and its chain
  verified, Sigma3, the CASE session on a `controller/exchange.Manager`,
  the record saved after the session exists, and the InvalidParam report
  on a failure the peer can be told of. Ported from matter.js
  `CaseClient.ts` at `0ceb202d`.
- `bridgetest.Commissioner.ControllerIdentity` and `RootPublicKey`: the
  commissioner's controller identity and fabric root, for a CASE
  initiator outside the package.

### Changed

- The Matter data model snapshot (`parity/schema.json`) moves from
  matter.js `85cf6647` (behaviour cited at the code pin `4633bfd7`) to
  `0ceb202d`, and every matter.js line citation follows it. No cluster or
  device-type revision moved.
- Enum manufacturer ranges are accepted. The snapshot carries the reserved
  ranges of Matter 1.6.1 as enum range members (`ModeTag` MfgTags
  32768–49151 and `ModeChangeStatus` MfgCodes 128–191 in the ten mode
  clusters, `OperationalStateEnum` ManufacturerStates and `ErrorStateEnum`
  ManufacturerError 128–191 in OperationalState, RvcOperationalState and
  OvenCavityOperationalState, `ClosureErrorEnum` ManufacturerError
  128–191, `LightSensorTypeEnum` Ms 64–254). `spec.Enum` gains `Ranges`
  (`spec.EnumRange`), and `spec.EnumSupported` and every write and host
  setter that checks an enum value against the definition accept a value
  inside a range, as matter.js validates it. A value past the range is
  still CONSTRAINT_ERROR. A mode server's `ModeChanger` status is checked
  against the derivation's ModeChangeStatus as the definition allows: 0x80–0xBF
  (MfgCodes) still pass, 0xC0–0xFF now fail the invoke with
  `ErrInvalidHostStatus` (they were accepted before).
- `IlluminanceMeasurement.LightSensorType` is typed `LightSensorTypeEnum`
  in the generated definition (it was `uint8`).
- `mdns` caps the advertised session intervals as matter.js `0ceb202d`'s
  `SessionIntervals.forAdvertisement` does: SII and SAI above one hour are
  advertised as 3 600 000 ms, SAT above 65 535 ms as 65 535, each cap
  logged as `matter.mdns.interval_capped`, on the operational and the
  commissionable record; zero still means the default.

### Fixed

- AddNOC refuses a reserved AdminVendorId (0x0000, 0xFFF5..0xFFFF) with
  the IM status InvalidCommand before every other check, the fail-safe
  included, as matter.js `0ceb202d` and chip do; it used to answer a
  NOCResponse InvalidAdminSubject after the CSR and trust-root checks. The
  test vendor IDs 0xFFF1..0xFFF4 stay accepted.
- AddNOC and UpdateNOC answer a NOC whose matter-node-id lies outside the
  operational node ID range with NOCResponse InvalidNodeOpId instead of
  InvalidNOC (`mattercert.ErrNonOperationalNodeID`, wrapped alongside
  `ErrMalformed`).
- DoorLock follows matter.js `0ceb202d`'s lock-operation rules:
  UnboltDoor reports LockOperationType Unlock (was Unlatch); a lock
  command without an accessing fabric (a PASE session before AddNOC) is
  refused with UNSUPPORTED_ACCESS before the `StateSource` is called; with
  OperatingMode NoRemoteLockUnlock every lock command emits
  LockOperationError (OperationError Unspecified) and answers FAILURE; a
  failing `LockInvoke` emits LockOperationError and answers the error's IM
  status if it carries one, FAILURE otherwise (it used to return the
  error unchanged with no event).
- `core.NewDescriptor` lists a device type once, at the revision of its
  first entry, as matter.js `0ceb202d`'s `addDeviceTypes` does: a bridged
  endpoint whose host device type is BridgedNode no longer reports
  BridgedNode twice in its DeviceTypeList.
- `thermo.New` checks the configured setpoint limits and initial setpoints
  against the setpoint rules, as matter.js `0ceb202d`'s
  `#reconcileInitialSetpoints` does: a violation is repaired preferring the
  heating side (an initial heating setpoint above its maximum is cropped,
  the cooling setpoint and limits move to keep the AUTO deadband), logged
  as `matter.thermostat.initial_setpoints_adjusted`, and a configuration
  that cannot be repaired (unordered absolute limits, no room for the
  deadband) is refused with the new `thermo.ErrSetpoints`;
  `NewThermostatServer` panics on it. It used to be accepted unchecked,
  and every later setpoint write then failed.

## [0.3.1] — 2026-10-13

**What a host must do.** Nothing. Every change in this release is additive
or a behavioural fix behind an unchanged signature: no identifier is
removed, renamed or deprecated, the store schema is the one `v0.3.0`
created (`store.Migrations()` ends at `v4`; an existing database opens
unchanged), and a host that does not set the new ports and knobs sees the
same wire behaviour as before. The one visible timing change is
intentional: a host `Sink` of `onoff.Server` that confirms through `Set`
from inside its own call no longer deadlocks (issue #63); it used to block
the command until the host's write timeout and leave the confirming
goroutine stuck for good.

### Added

- `light.ColorControlServer.Set(light.Color)`: records a colour the device
  took on itself, the twin of `onoff.Server.Set`. `Color.Mode` is the
  EnhancedColorMode the device reports; that mode's fields become the
  current colour (the colour temperature cropped to the physical range,
  the other fields to their attributes' ranges), a mode change converts as
  a command's mode switch does, and every changed attribute is reported
  through the change notifier. The writers are not called. As in matter.js,
  a running transition or colour loop is not stopped: its next step
  overwrites the device's value, and RemainingTime is unchanged. Set takes
  only the state lock, so a `ColorWriter` or `ColorTemperatureWriter` may
  call it from inside its own call; the device's colour then stays stored
  and the command answers SUCCESS, and a Set of the colour being written
  is a confirmation. A colour of a mode the server has no feature for is
  logged and ignored (issue #66).
- `thermo.ThermostatConfig.Sink` (`thermo.Sink`, `thermo.SetpointChange`):
  an optional host port for a bridged thermostat. A controller's SystemMode
  write, OccupiedHeating/CoolingSetpoint write (with the companions the
  reconcile would move) and the setpoints a SetpointRaiseLower moves (one
  call; in Both mode the cooling setpoint with the heating setpoint as its
  companion) reach the host after validation and before anything is stored;
  an error refuses the change (its IM status if it carries one, FAILURE
  otherwise) and nothing is stored. A value equal to the stored one is no
  change and does not call the Sink. The limit writes do not call it. New inbound setters
  `SetSystemMode`, `SetOccupiedHeatingSetpoint` and
  `SetOccupiedCoolingSetpoint` record the device's own state without the
  Sink and report through the change notifier (issue #60).
- `cover.Config.Mover` (`cover.Mover`): an optional host port for a bridged
  window covering. UpOrOpen, DownOrClose and GoToLiftPercentage store the
  target the server infers and call `GoTo` once; StopMotion sets the target
  to the current position and calls `Stop`. The server then neither moves
  the lift nor changes OperationalStatus; a Mover error answers with its IM
  status if it carries one, FAILURE otherwise, and restores the previous
  target. The Mover runs inside the command, so it
  returns once the device accepted the target. `New` refuses a Mover
  together with a `MoveStep` (`cover.ErrMoverWithMoveStep`). New inbound
  setters `SetCurrentPosition(lift, tilt)` (tilt unused: the server is
  lift-only) and `SetOperationalStatus` record the device's state without
  the Mover and report through the change notifier (issue #61).
- `bridge.Bridge.CloseFabricSessionsExcept(fabricIndex, exceptSessionID)`:
  ends every session of a fabric but one, telling each peer with a
  CloseSession first — the UpdateNOC path (matter.js `Fabric.replaced`).
  `node` calls it after UpdateNOC instead of closing the sessions silently.
- `store.Migrations()` and `store.Migration{Version, Up, Down}` (#62): the
  matter_* schema as ordered, versioned steps for a host that runs its own
  migration tool and cannot call `store.Apply` — `v1` the DDL of v0.1.0
  (`matter_group_key_map` with its foreign key into `matter_group_keys`),
  `v2` the three tables of v0.2.0 (`matter_group_table`,
  `matter_groupcast_groups`, `matter_server_subscriptions`), `v3` the
  v0.2.0 `matter_group_key_map` rebuild without that foreign key (the
  `store.Upgrade` step, which now runs the same text), `v4` the v0.3.0
  `matter_ota_default_providers`. Each step's `Down` drops what it added,
  `v3`'s rebuilds the foreign key and drops the GroupKeyMap entries that
  name a key set that does not exist. A host embeds the steps in order and
  pins `len(Migrations())` (4) and the last version (`v4`); a test applies
  the list to one database and `store.Apply` to another and compares
  their `sqlite_schema`, so the list and `store.Schema()` cannot drift.
  `store.Schema`, `store.Apply` and `store.Upgrade` are unchanged.
- `store.SchemaFingerprint(ctx, db)`: SHA-256 of the database's matter_*
  schema (the tables `store.Schema()` creates, the indexes its DDL declares
  and those tables' automatic indexes, statements normalised; a host's own
  index on one of these tables does not count), so a host that carries the schema itself can compare it with
  the release's at column and constraint level. This release, after
  `store.Apply`:
  `924e777a63a110753483a2dd08fb61c19bc5b9f96f09271b1bd265b9212bd924`.
- `node.Config.Observers` (issue #58): `OnFabricAdded`, `OnFabricRemoved`,
  `OnFabricUpdated` (a `node.FabricInfo`: index, fabric id, node id, root
  public key, compressed fabric id), `OnCommissioningComplete(fabricIndex)`
  and `OnWindowTransition(node.WindowSnapshot)`. Each runs after the
  facade's own handler for the event, which stays wired; an observer's
  error or panic is logged, not returned. A host no longer replaces the
  facade's OperationalCredentials, GeneralCommissioning or window hooks to
  follow these events.
- `node.Node.OpenBasicWindow(ctx, node.WindowCredentials)` (issue #58): the
  node's own commissioning window with per-window credentials
  (discriminator, passcode, PBKDF iterations and salt, timeout) — the
  ephemeral-window pattern. PASE runs against a verifier computed from them,
  the node is advertised CM=1 under the window's discriminator, and once the
  window closes the configured credentials are back (a node without a fabric
  reopens its own window under the configured discriminator). matter.js has
  no per-window basic credentials; this is a facade extension, not a cluster
  path.
- `node.Config.SupportedFabrics` (OperationalCredentials SupportedFabrics;
  0 keeps the facade's 5, above 254 is refused) and
  `node.Config.DisableTimeSynchronization` (the zero value keeps
  TimeSynchronization on the root; set, the root's ServerList lacks 0x0038,
  `RootClusters.TimeSynchronization` is nil and GeneralDiagnostics
  TimeSnapshot answers with a null PosixTimeMs, as matter.js does without
  the behaviour) (issue #58).

### Changed

- `onoff.Server`: the host `Sink` no longer runs under a lock `Set` takes.
  Commands, writes, the countdown ticks and scene applies stay serialised
  in one queue the host never takes; the stored state has a lock of its
  own that `Set` takes alone. A Sink may now confirm the change through
  `Set` before it returns — on its own goroutine or from another — where
  it used to block the command for the host's write timeout and the
  confirming goroutine for good (issue #63). A `Set` that lands while a
  command's Sink runs is stored and reported at once; once the Sink
  consents, the command compares with it (the scene invalidation) and
  stores its own OnOff over it.
- `thermo.ThermostatServer`: the inbound setters (`SetSystemMode`,
  `SetOccupiedHeatingSetpoint`, `SetOccupiedCoolingSetpoint`) no longer
  wait for a write or SetpointRaiseLower whose `Sink` is in flight, so a
  Sink may confirm through them before it returns (the cycle of issue #63,
  introduced with the Sink). Writes and the command stay serialised behind
  the Sink. Once it consented, a write stores its value on the state as it
  is then and reruns the reconcile there; when the companions it computes
  differ from the ones the Sink was told about, the recomputed ones are
  stored and `matter.thermostat.sink_companions_moved` is logged. The
  Sink is told the written setpoint as the reconcile leaves it — the value
  the server stores — rather than the value the controller wrote (the two
  are equal for every write the limits admit). A SetpointRaiseLower stores
  what it moved as the Sink saw it and leaves a setpoint it did not move
  as the host stored it.
- RemoveFabric tells every other session of the removed fabric with a
  Secure-Channel CloseSession before they close (matter.js `Fabric.leave`);
  the session that carried the command still closes after its response,
  without one, as in matter.js. A host on `EmitFabricRemovedContext` gets
  this without a change; the peer address comes from the bridge's session
  routes and the fabric from the `SessionLookup`'s `SessionFabricResolver`.

### Fixed

- A PASE or CASE handshake in flight on a TCP connection now holds the
  connection open: the connection's last secure session closing mid-handshake
  no longer closes it under the handshake (matter.js counts the unsecured
  session on the channel). The session the handshake establishes is bound to
  the connection when the handshake completes, not at its first message.
- The first chunk of a chunked ongoing report that the peer never
  acknowledges is peer loss, as an unchunked report's message already was:
  its counter is registered with the report's exchange as soon as it is sent,
  not once the whole report is out, so the peer's earlier sessions close and
  their subscriptions with them (matter.js `MessageExchange.send` →
  `peerLost`). A later chunk's loss is unchanged.
- `contract.DeviceTypeName` labels every device type of the matter.js
  snapshot, not only the 31 it had a hand-kept entry for (#57):
  `DeviceTypeName(0x0045)` — SoilSensor, which `MeasurementSoilMoisture`
  advertises — returned the hex fallback `"0x0045"`. The table
  (`contract/devicetype_names_gen.go`) is now generated by
  `script/devicegen` from `parity/schema.json`; the 31 existing labels are
  unchanged, the 60 new ones are the matter.js names as
  `schema.DeviceTypeName` gives them (`"SoilSensor"`, `"RootNode"`, …).

## [0.3.0] — 2026-10-12

**What a host must do.** Upgrading from `v0.2.x` needs a source change only
where a host still names what `v0.2.0` announced for removal: `wire.Groups`
(configure `endpoint.Config.Groups`; the assembler mounts the real server),
`im.StatusUnreportableAttr` / `im.StatusNoUpstreamSubscription` (no
replacement; the codes left the spec), and the `PersistentSubscription*`
store API (server-side subscription resumption, ADR 0008, needs none). The
`matter_persistent_subscriptions` table stays in the schema, so an existing
database opens unchanged. Everything else in this release is additive: the
device layer `device/`, the node facade `node/`, `cluster/onoff.Server`,
TCP, BDX and the OTA requestor, `bridgetest.Commissioner`. A host on
`contract.EndpointSource` keeps working; the reference daemon shows the
move to `node.New` and `device.New<Type>`.

**ColorControl serves every feature matter.js implements; ExtendedColorLight
is servable.** A CT-only host keeps working unchanged: a
`light.ColorControlServerConfig` without `Features` is CT only, serves the
same attribute and command lists, reports the same attributes and drives
the same `ColorTemperatureWriter` as before.

### Added

- `bridge/bridgetest.Commissioner`, **public test support** (README.md §API
  stability): a commissioner and controller for tests. `Dial(t, addr,
  Options{Passcode, Discriminator, FabricID, NodeID, CAT, Seed, …})`;
  `Commission(ctx)` runs PASE, ArmFailSafe, SetRegulatoryConfig, the
  certificate-chain and attestation requests (signatures checked against
  the DAC), CSRRequest, AddTrustedRootCertificate, AddNOC, CASE and
  CommissioningComplete over CASE, in chip-tool's AutoCommissioner order,
  and returns the CASE `Session`; `PASE`, `CASE`, `CASEAs` (another
  controller of the fabric) and `Accept` (a CASE session the device opens)
  give further sessions. A `Session` reads, writes, invokes, subscribes,
  receives reports (`NextReport`) and closes. Keys and certificates come
  from a test CA in the package, determined by `Options.Seed`; with
  `Options.Advertiser` the commissioner finds the device through its
  DNS-SD records. It implements `bridge.OperationalResolver`, so a device
  re-establishing a former subscription after a restart reaches it.
- `device/` mounts default servers for two fabric-scoped clusters whose
  generated server refuses them: Binding (`core.NewBinding`, as matter.js
  mounts BindingServer where the device type or Base mandates it) and
  EcosystemInformation (`core.NewEcosystemInformation` over empty
  directories). `Doorbell.Binding()` returns `*core.Binding` and
  `BridgedNode.EcosystemInformation()` `*core.EcosystemInformation`. A
  Doorbell no longer needs `WithServer(core.NewBinding())`.
  OtaSoftwareUpdateRequestor stays the host's (`ErrNoDefaultServer`): its
  server needs the node's connector.
- `endpoint.Spec.OptIn []uint32`: an endpoint opts into an optional Groups
  (0x0004) or ScenesManagement (0x0062) and the assembler mounts the stack's
  server where the device type allows the cluster, as matter.js's
  `MutableEndpoint.with(GroupsServer)` extends any endpoint type.
  `device.WithOptional` sets it for those two clusters instead of refusing
  them (AirPurifier, Pump, RoomAirConditioner, SmokeCoAlarm, Thermostat,
  WindowCovering list Groups as optional; Pump and RoomAirConditioner
  ScenesManagement); every other option for them stays refused.
- `device.WithMeasurement(contract.MeasurementSource)`: a device's
  measurement clusters are served from the host's live reading — the
  assembler materialises them from the reading's measurement class
  (`endpoint.Spec.Measurement`, `cluster/measurement FromMeasurementClass`)
  on every dispatch, the bridge subscribes to the reading's notifier and
  wires a GenericSwitch's presses to it. The clusters the reading
  materialises are the assembler's for that device: no default server, no
  other option for them. A device without the option is unchanged.
  `endpoint.ClusterServers` now serves a Source's clusters followed by the
  Measurement's when an endpoint carries both. The reference daemon's
  sensors, the smoke alarm's battery and the electrical sensors are
  `device.New<Type>(…, WithMeasurement(src))` calls, parts through
  `AddPart`; endpoint ids, cluster sets and attribute values are unchanged.
- `onoff.Server` carries matter.js's scene paths (`OnOffServer.ts`) through
  an optional ScenesManagement port, `onoff.Scenes` (`Config.Scenes`,
  `Server.SetScenes`), whose three operations are named after
  `ScenesManagementServer`'s: `MakeAllFabricSceneInfoEntriesInvalid`,
  `StoreGlobalScene`, `RecallGlobalScene`. `core.ScenesManagement`
  implements them, and the endpoint layer hands an endpoint's
  ScenesManagement to its `onoff.Server` when it mounts one. With the port,
  a command that changes OnOff invalidates the current scene (one that
  leaves it unchanged does not; the endpoint's blanket invalidation after
  every OnOff command is skipped for such a server,
  `core.SceneInvalidator`); OffWithEffect stores the accessing fabric's
  global scene while GlobalSceneControl is true; OnWithRecallGlobalScene
  recalls it; and a recalled scene's OnOff value is applied by the server
  (`MatterApplySceneValues`) at once, or after the scene's transition time
  on the server's clock, dropped when OnOff changes in between.

- `Bridge.SessionClosed(sessionID)`: the host calls it from the session
  registry's close hook, next to the subscription manager's
  `CloseSession` (`node/` does). The bridge forgets the session's route
  and closes a TCP connection when its last session goes — by
  CloseSession, fabric removal, NOC rotation or the idle reaper — as
  matter.js's `#closeTcpChannelIfLastSession` does
  (`ExchangeManager.ts:669-676`). Before, such a connection stayed open
  until the peer closed it. An inbound or outbound CloseSession closes the
  connection without the hook. A host that wires the registry itself
  should add the call.

- The device layer `device/` (ADR 0018, wave 6), **public API from this
  release on** (README.md §API stability): one generated Go type per device
  type in scope — 63 of the snapshot's 91, the rest named with their concept
  Appendix F.3 group — with its id, revision, classification, composition
  and the server and client cluster requirements matter.js's generator
  derives (`RequirementGenerator.ts`, `ClusterRequirements.ts`: mandatory
  and optional clusters, mandated features, element alterations, Base's
  Binding where it mandates it). `device.New<Type>(Options, ...Option)`
  mounts every mandatory server cluster and every optional one the host
  opts in with a default `spec.Server` (features, mandated elements, stated
  defaults; the default selection adds the features the cluster itself
  marks mandatory), takes a host server instead (`WithServer` — also for a
  cluster the type does not list, mounted after the type's own, as
  matter.js's `MutableEndpoint.with` adds any behavior), the host model's
  change notifier as the endpoint source's (`WithChangeNotifier`, for
  servers that read their state through a host port), a feature
  selection (`WithFeatures`, held to `spec.CheckFeatures` and to the
  features the type mandates), an optional cluster (`WithOptional`),
  optional elements (`WithElements`) and initial values (`WithInitial`). `Device.Spec()` turns a device and its
  parts (`AddPart`, refused for a device type the owner does not compose)
  into the `endpoint.Spec` the assembler consumes, with the type's
  mandatory client clusters as `Spec.ClientClusters`; `Device.OnChanged`
  delivers the server's attribute changes. Descriptor, Identify,
  BridgedDeviceBasicInformation, Groups and ScenesManagement stay the
  assembler's. `script/devicegen` generates the package from
  `parity/schema.json` and runs in `make generate-matter-schema` after
  `script/clustergen`.
- `endpoint.Spec.ClientClusters`: the client clusters an endpoint declares,
  served as its Descriptor ClientList (bridged endpoints and parts), as
  matter.js `MutableEndpoint.ts:53-61` registers a type's mandatory client
  clusters and `DescriptorServer.ts:284-290` lists them. Unset, the
  ClientList stays empty as before.
- `node/`: the node facade (ADR 0018 decision 5), mirroring matter.js
  `ServerNode` / `Node`. `node.Config` groups the configuration by
  matter.js behaviour id — `Network` (listen address, IPv4, TCP),
  `Commissioning` (passcode, discriminator, PBKDF parameters),
  `ProductDescription`, `BasicInformation` — plus the store (`*sql.DB`, or
  the two stores), the advertiser, the device-type validation mode, the
  attestation material (the CSA test chain by default), an optional OTA
  requestor, the root's FixedLabel list and supported locales (`Locales`,
  en-US alone by default) and the logger; `Validate` applies the reference daemon's
  checks. `node.New` performs the daemon's bring-up with every bridge port
  wired in the order `bridge/bridge.go` documents; `Start`, `Stop` (both
  idempotent) and `Run` the post-start sequence and the shutdown.
  `node.Aggregator` (`Add`, `Remove`, `Endpoints`, `MarkComplete`,
  `IncreaseConfigurationVersion`) holds the bridged endpoints — anything
  with `Spec() endpoint.Spec` — and reassembles a running node; stored
  endpoint numbers are retained until the host calls `MarkComplete`; NodeLabel
  writes, ConfigurationVersion and scene tables persist under the daemon's
  settings keys. `Bridge()`, `Root()` (the root servers), `Store()`,
  `CommissioningWindow()`, `Fabrics()`, `PairingCodes()` and
  `BootReason()` reach the ports the facade does not cover.
- `cluster/onoff.Server`: the default OnOff server, a port of matter.js
  `OnOffBaseServer` (`packages/node/src/behaviors/on-off/OnOffServer.ts`)
  on the generated definition. `onoff.New(Config)` takes the feature
  selection, an optional host `Sink` (`SetOnOff(ctx, bool) error`, which
  may refuse a change: the command answers FAILURE and nothing is stored),
  the initial values, an optional `BootReason` and a `Clock` seam. It
  carries the command logic (On, Off, Toggle, OffWithEffect,
  OnWithRecallGlobalScene, OnWithTimedOff), StartUpOnOff at construction
  (not after a software update), the 100 ms timed-on and delayed-off
  countdowns with the 0xFFFF hold, `Set` / `OnOff` for the host and
  `MatterQuiesce`; `Config.Bridged` skips StartUpOnOff on an endpoint under
  the Aggregator, as matter.js does. The existing constants and functions of `cluster/onoff`
  are unchanged.
- Session establishment over TCP: a PASE or CASE message that arrives on a
  TCP connection is served on it, as matter.js creates the unsecured
  session from whichever channel carried the message
  (`ExchangeManager.ts:241-264`); replies go back on the connection without
  MRP. Reply chunking is route-aware: over a reliable connection the
  ReportData and InvokeResponse chunkers fill the connection's frame,
  bounded by the peer's stated MaxTcpMessageSize
  (`bridge.SessionTransportResolver`, wired by the reference daemon from the
  operational entry), and keep the UDP datagram budget otherwise.
  `bridge.EncodesAttributeValue` tells a fleet guard whether the attribute
  writer knows a value's type (the reference daemon's wire-shape guard uses
  it instead of a copy of the writer's type switch). The harness runs the
  daemon with TCP on (`MCORE.SC.S.TCP=1`, TC-SC-4.3's `T` key check) and
  with `--ota-harness`.
- Matter TCP transport (ADR 0019, phase 3). `transport.Conn`
  (`Send`, `Remote`, `Reliable`) is the connection a message arrived on;
  `transport/tcp` is the length-framed listener and dialer ported from
  matter.js `TcpChannel.ts` / `TcpTransport.ts` (4-byte little-endian
  length, 64 000-byte default frame, receive buffer bounded at twice that,
  three connections per peer IP, 10 s for an inbound connection's first
  message); `udp.Listener.Conn` is the unreliable adapter;
  `mrp.UsesMRP` is matter.js's `usesMrp`. `bridge.Config.TCP`
  (`Incoming`, `Outgoing`; zero value off) opens a TCP listener on the UDP
  socket's port and sets the operational record's `T` key. A request over
  TCP is answered on its connection without MRP, and a dropped connection
  closes every session bound to it. Session establishment (PASE / CASE)
  over TCP is served too (above). The reference daemon's `--tcp` flag (`true`, `false`, `incoming`,
  `outgoing`) defaults to on.
- `secure/sessionparams` (new): the SessionParameters structure both
  handshakes exchange, tags 1–9 as matter.js `PaseMessages.ts:28-58`
  declares them, including SupportedTransports (tag 8) and
  MaxTCPMessageSize (tag 9, written only when the bitmap names TCP) with
  the Matter 1.5 gate on a peer's TCP bits (`SessionParameters.ts:50-62`).
  `sigma.SessionParameters` is this type; Sigma1/Sigma2/Sigma2_Resume carry
  the two new fields. `bridge.SessionParametersFor` states the node's
  transports for a host that listens on TCP; `operational.Entry` keeps the
  peer's effective transports and TCP message bound
  (`SetPeerTransportParameters`), which the reference daemon sets on every
  CASE and PASE session.
- **OTA Software Update Requestor and BDX** (ADR 0019, brief N).
  `cluster/core.OTASoftwareUpdateRequestor` replaces the static stub: it
  queries the providers a controller names (DefaultOTAProviders, persisted
  per fabric in the new `matter_ota_default_providers` table, or
  AnnounceOTAProvider), downloads the image over BDX, asks the provider
  whether to apply it, hands it to the host's `OtaApplier` and reports
  the outcome (StateTransition, VersionApplied, DownloadError,
  NotifyUpdateApplied) — matter.js's OtaSoftwareUpdateRequestorServer rule
  for rule. **Breaking:** `NewOTASoftwareUpdateRequestor` takes an
  `OTARequestorConfig` and returns an error; the `OTAUpdateState*`
  constants are gone (use `otasoftwareupdaterequestor.UpdateState*`).
  New package `bdx` (message codecs, messenger, the initiating receiver in
  both driver modes, `FileSink`) and `bdx/otaimage` (OTA image header,
  validation, writer), both byte-checked against matter.js fixtures.
  `*bridge.Bridge` implements `core.OtaProviderConnector`: the ADR 0008
  CASE initiator, generalised to one Invoke and one BDX exchange on a
  session with the provider. `im` gains the client-side InvokeRequest
  encoder and InvokeResponse decoder; `store` gains
  `ListDefaultOTAProviders`, `ReplaceDefaultOTAProviders` and the
  requestor's state; `BasicInformation` gains `OtaNodeInformation` and
  `SetSoftwareVersion`. HTTPS downloads are not ported.
- Reference daemon: the requestor on the root (device type OtaRequestor),
  with an applier that validates the image against the node's vendor,
  product and the offered version and keeps it; `--ota-dir` and the
  test-only `--ota-harness`, which applies in place.
- Reference daemon: a second DeviceEnergyManagement device ("Charger ESA",
  `fleet_esa.go`) with StateForecastReporting, StartTimeAdjustment,
  Pausable, ForecastAdjustment and ConstraintBasedAdjustment (FeatureMap
  0x7C, as TC-DEM-2.6/2.8 start chip's app), appended after the grid
  meter. The DEM test event triggers reach both ESAs. A test,
  `TestFleetWireShapes`, reads every attribute of the fleet through the
  bridge's dispatcher and fails on a value the bridge writer would encode
  as null.
- Certification harness: a per-case endpoint override in the family table
  (`endpoints`, formerly `caseDeviceType`, taking a pseudo key too) runs
  DEM/2.6 and 2.8 on the new ESA with its PICS slice; the EGC family runs
  on the energy tariff, BDX next to SU; the SU requestor cases get chip's
  OTA provider app and a staged image built with `ota_image_tool.py` for
  the daemon's vendor, product and next software version.
- `deprecation_policy_test.go` checks a `Deprecated:` paragraph on a
  `const (` / `var (` group, and one inside it, against CHANGELOG.md.

- `cluster/spec.CheckFeatures` judges a feature's otherwise-conformance by
  its first applicable entry, as matter.js's FeatureSelectionErrors does
  (`IllegalFeatureCombinations.ts` addOtherwiseRules): DeviceEnergyManagement
  with StateForecastReporting and the adjustment features but without
  PowerAdjustment (TC-DEM-2.6's `--featureSet 0x7C`) is accepted; before,
  PowerForecastReporting was wrongly mandated. Attribute, command and event
  conformance is unchanged (`Conformance.ts` computeApplicability).
- Reference daemon, for the certification harness (wave-4 integration): the
  app pipe knows `SetSimulatedSoilMoisture` (TC-SOIL-2.2) and the eleven
  energy-market test event triggers of the tariff and meter devices reach
  the TestEventTrigger dispatcher. The daemon binary that `go build` drops
  into `examples/reference-bridge/` is no longer tracked (it had been
  committed since the wave-1 refrigerator PR) and is ignored from now on.

- `thermo.ThermostatUIConfig.UnitLocalization` (optional `*spec.Server`):
  with no configured TemperatureDisplayMode, the same endpoint's
  UnitLocalization decides it at construction — Fahrenheit when its
  TemperatureUnit is Fahrenheit, else Celsius — as matter.js's
  ThermostatUserInterfaceConfigurationServer initialize does.

- `cluster/measurement`: SoilMeasurement (0x0430) as the SoilSensor kind,
  `contract.MeasurementSoilMoisture` (SoilSensor 0x0045). A source
  implements `measurement.SoilMoistureSource`: the reading in percent plus
  the fixed `SoilMoistureMeasurementLimits` struct; a source without
  limits mounts nothing. The measured value starts null and a reading
  outside the limits is refused, keeping the previous value, as
  connectedhomeip's `SoilMeasurementCluster.cpp` does.
- `cluster/core`: `NewPowerSourceConfiguration` (0x002E; Sources in
  priority order, at most six, optionally read through
  `SetSourcesProvider`), `NewEcosystemInformation` (0x0750; host port
  `EcosystemDirectory`, fabric-scoped lists filtered by the reading fabric,
  another fabric's entries redacted on an unfiltered read) and
  `NewActions` (0x0025; host port `ActionsHost`, all twelve commands
  accepted, NOT_FOUND for an unknown action, INVALID_COMMAND for a command
  the action does not support, StateChanged / ActionFailed through the
  event emitter).
- Generated definitions for SoilMeasurement, PowerSourceConfiguration,
  EcosystemInformation and Actions under `cluster/spec/`.
- Reference daemon: a SoilSensor ("Garden Soil"), PowerSourceConfiguration
  on the root listing the EV charger's PowerSource endpoint, Actions on the
  Aggregator with two demo scenes over the desk lamp, EcosystemInformation
  on the air quality sensor. The SOIL, ACT, PSCFG and ECOINFO families are
  declared (ECOINFO's two cases excluded: they start their own fabric-sync
  DUT).
- `cluster/energy`: CommodityPrice (0x0095), ElectricalGridConditions
  (0x00A0), CommodityMetering (0x0B07) and CommodityTariff (0x0700) servers
  on the generated definitions (`NewCommodityPrice`,
  `NewElectricalGridConditions`, `NewCommodityMetering`,
  `NewCommodityTariff`), with connectedhomeip's rules at the harness pin:
  the price validation, the detail filter of the CurrentPrice and
  PriceForecast attributes and of GetDetailedPriceRequest /
  GetDetailedForecastRequest, the PriceChange and CurrentConditionsChanged
  events, the metering setter rules, and for CommodityTariff the whole
  update validation and cross validation (`UpdateTariff` takes a `Tariff`;
  a refused update returns which rule refused it), the delayed activation
  at StartDate, `ClearTariff`, the derived CurrentDay / NextDay /
  Current- and NextDayEntry(Date) / Current- and NextTariffComponents
  (`SyncTime`), GetTariffComponent and GetDayEntry.
- Reference daemon: an ElectricalEnergyTariff (0x0513) with CommodityPrice,
  ElectricalGridConditions and CommodityTariff, and an ElectricalMeter
  (0x0514) with ElectricalPowerMeasurement, ElectricalEnergyMeasurement,
  CommodityMetering and an ElectricalSensor part, carrying the
  energy-gateway app's demo data, tariff presets and test event triggers.
  chip-tool families SETRF and COMMTR (workflow group `app-energy`).
- Reference daemon, for the certification harness: FixedLabel and UserLabel
  on the Aggregator endpoint and FixedLabel on the first bridged light (the
  CSA cases written against the CHIP all-clusters app address endpoint 1);
  the BooleanStateConfiguration and MeterIdentification test event triggers
  as chip's handlers define them; the demo meter starts with MeterType null,
  as chip's `meter-identification-server.cpp` does. DGSW/3.2 and DGETH/3.2
  "Simulated" are declared not applicable (`DGSW.C=0`, `DGETH.C=0`: the
  daemon is no client).
- `cluster/measurement` serves seven more concentration clusters on the
  existing kind pattern, each with its `contract.MeasurementClass`
  (`MeasurementCO`, `MeasurementNO2`, `MeasurementOzone`,
  `MeasurementFormaldehyde`, `MeasurementPM1`, `MeasurementTVOC`,
  `MeasurementRadon`; AirQualitySensor 0x002C) and constructor
  (`NewCOConcentrationServer`, …, `NewRadonConcentrationServer`). Model
  units: ppm for CO, NO2, ozone, formaldehyde and TVOC, µg/m³ for PM1,
  Bq/m³ for radon. The materialiser mounts AirQuality next to each, which
  reads Unknown for these seven (no grading guideline).
- `cluster/boolcfg`: a BooleanStateConfiguration (0x0080) server on the
  generated definition — sensitivity level, visual / audible alarms with
  EnableDisableAlarm and SuppressAlarm, sensor faults, the
  AlarmsStateChanged and SensorFault events — with the command rules of
  connectedhomeip's BooleanStateConfigurationCluster and a `Delegate` host
  port.
- The reference daemon has an AirQualitySensor carrying AirQuality and all
  ten concentration clusters, and its contact sensor carries
  BooleanStateConfiguration (SENSLVL, VIS, SPRS); the certification table
  declares BOOLCFG and the concentration families for them.
- `cluster/core` serves six more root (or any-endpoint) clusters, each on
  its generated definition: `NewUserLabel` (LabelList longer than
  `MaxLabels`, default 255, is RESOURCE_EXHAUSTED; `OnWrite` persists),
  `NewLocalizationConfiguration`, `NewTimeFormatLocalization` (with
  CalendarFormat) and `NewUnitLocalization` (with TemperatureUnit) —
  each takes the host's detected values, defaults its supported list as
  matter.js does and answers a written active value outside that list
  CONSTRAINT_ERROR — plus `NewSoftwareDiagnostics` (host port
  `HeapReporter`; WTRMRK and ResetWatermarks with a `WatermarkReporter`)
  and `NewEthernetNetworkDiagnostics` (host port `EthernetReporter`;
  PHYRate, FullDuplex, CarrierDetect null and TimeSinceReset 0 where the
  host cannot tell; no PKTCNT / ERRCNT). `ErrLocalizationConfig`,
  `ErrUserLabelConfig` and `ErrNilReporter` reject a configuration.
- The bridge decodes a UserLabel LabelList write — whole list or
  list-append entry — with the generated decoder; a label over "max 16"
  is answered CONSTRAINT_ERROR without failing the rest of the request.
- The reference daemon mounts FixedLabel (two labels), UserLabel
  (persisted in the settings table), the three localization clusters
  (en-US with de-DE, 24-hour, Gregorian, Celsius), SoftwareDiagnostics
  (Go runtime heap) and EthernetNetworkDiagnostics on the root; the
  chip-tool families FLABEL, ULABEL, LCFG, LTIME, LUNIT, DGSW and DGETH
  are declared and run in the core group.
- `cluster/energy` (new package), on generated definitions:
  - `NewWaterHeaterManagement`: a WaterHeaterManagement (0x0094) server
    with the EnergyManagement and TankPercent features, the host port
    `Booster` (`Boost`, `CancelBoost`), `SetHeatDemand` (held inside
    HeaterTypes), `SetTankPercentage`, `SetTankVolume`,
    `SetEstimatedHeatRequired`, and `EndBoost` for a boost that ends on
    its own. Boost answers INVALID_COMMAND to the target fields
    connectedhomeip's server refuses; a successful Boost sets BoostState
    Active and emits BoostStarted, CancelBoost and `EndBoost` set it
    Inactive and emit BoostEnded, and a CancelBoost without a running
    boost succeeds without an event, as connectedhomeip's water-heater app
    and TC-EWATERHTR-2.2 expect.
  - `NewEnergyPreference`: an EnergyPreference (0x009B) server with the
    EnergyBalance and LowPowerModeSensitivity features; a write of
    CurrentEnergyBalance or CurrentLowPowerModeSensitivity at or beyond
    its list's length is CONSTRAINT_ERROR, an accepted one reaches the
    optional `PreferenceChanger`.
  - `NewMeterIdentification`: the MeterIdentification (0x0B06) server
    without PowerThreshold.
- Generated definitions under `cluster/spec/` for WaterHeaterManagement,
  EnergyPreference and MeterIdentification.
- The reference daemon bridges a WaterHeater (WaterHeaterManagement EM +
  TP, WaterHeaterMode, a HEAT Thermostat) whose tank answers the
  WaterHeaterManagement test event triggers as connectedhomeip's
  water-heater app does, an ElectricalUtilityMeter with
  MeterIdentification, and serves EnergyPreference (EnergyBalance) on its
  thermostat. The chip-tool family table runs EWATERHTR, WHM and EPREF.
- `cluster/alarmbase` (new): one server for the AlarmBase family on the
  generated definitions — `NewDishwasherAlarm`, `NewRefrigeratorAlarm`,
  `NewTemperatureAlarm`. Supported and Latch are fixed, Mask and State
  move through `SetMask`, `SetState` and `ResetLatchedAlarms`, Reset and
  ModifyEnabledAlarms ask an optional `Delegate`, and every State change
  emits Notify — connectedhomeip's AlarmBase rules, as matter.js's servers
  add none. TemperatureAlarm's adjustable-threshold features
  (`SetTemperatureAlarmThresholds`) are refused
  (`ErrAdjustableThresholds`): no source gives that command a rule.
- `cluster/appliance` (new): `NewMicrowaveOvenControl` (SetCookingParameters,
  AddMoreTime, the host port `Oven`), `NewLaundryWasherControls` and
  `NewLaundryDryerControls` (controller writes checked against the
  supported spin speeds, rinses and dryness levels, an optional listener),
  each on its generated definition with connectedhomeip's rules.
- `cluster/opstate.NewOvenCavityServer`: OvenCavityOperationalState
  (0x0048), with matter.js's `#assertPhaseList` (`OvenCavityPhases`,
  `ErrPhaseNotAllowed`). `DeviceTypeMicrowaveOven` and
  `DeviceTypeTemperatureControlledCabinet` are accepted device types; a
  MicrowaveOven OperationalState serves CountdownTime as the device type
  mandates.
- The generated definitions of DishwasherAlarm, RefrigeratorAlarm,
  TemperatureAlarm, LaundryWasherControls, LaundryDryerControls,
  MicrowaveOvenControl and OvenCavityOperationalState under `cluster/spec/`.
- The reference daemon serves a dishwasher (DishwasherAlarm), an Oven with
  a Heater cavity part (OvenMode, OvenCavityOperationalState,
  TemperatureControl), a MicrowaveOven, and a laundry dryer
  (LaundryDryerControls); the washer gains LaundryWasherControls and the
  fridge RefrigeratorAlarm, driven by CHIP's `SetRefrigeratorDoorStatus`
  app-pipe command. The chip-tool families DISHALM, REFALM, OVENOPSTATE,
  OTCCM, MWOCTRL and MWOM are declared.
- `cluster/energy.NewDeviceEnergyManagement`: a DeviceEnergyManagement
  (0x0098) server on the generated definition, any feature selection the
  definition allows (PA, PFR, SFR, STA, PAU, FA, CON). The eight requests
  carry connectedhomeip's checks (opt-out, ESAState, the capability and
  forecast bounds, its post-conditions); ESAState, the PowerAdjustStart /
  PowerAdjustEnd / Paused / Resumed events, the adjustment and pause
  timers and the opt-out cancellation follow chip's shared DEM delegate.
  The host port `DemManager` is chip's DEMManufacturerDelegate
  (`NopDemManager` for its defaults); the device side sets ESAState, the
  power bounds, PowerAdjustmentCapability, Forecast and OptOutState.
- `cluster/energy.NewEnergyEvse`: an EnergyEvse (0x0099) server on the
  generated definition (PREF, SOC, PNC, RFID, V2X; the optional
  UserMaximumChargeCurrent, RandomizationDelayWindow,
  ApproximateEvEfficiency and StartDiagnostics). Disable, EnableCharging,
  EnableDischarging, StartDiagnostics, SetTargets, GetTargets and
  ClearTargets carry chip's checks; chip's EVSE state machine drives
  State, SupplyState, FaultState, the current limits, the session
  attributes, the ChargingEnabledUntil timer and the EVConnected,
  EVNotDetected, EnergyTransferStarted / Stopped, Fault and RFID events
  from the device-side inputs (`SetHardwareState`, `SetFault`,
  `ReportRFID`, `SetVehicleID`, `DiagnosticsComplete`, the hardware,
  circuit and cable limits). The host port `EvseHost` reads the energy
  meter and is told of state, limit and charging-preference changes.
- The reference daemon serves an EV charger: EnergyEvse (0x050C) with
  EnergyEvse and EnergyEvseMode, and PowerSource (wired),
  DeviceEnergyManagement (with DeviceEnergyManagementMode, condition
  ControllableEsa) and ElectricalSensor parts; it answers the EnergyEvse
  (0x0099…) and DeviceEnergyManagement (0x0098…) test event triggers as
  connectedhomeip's evse-app does. The chip-tool suite runs the DEM, DEMM,
  EEVSE and EEVSEM families against it (workflow group `app-energy`).
- Reference daemon, for the certification harness (wave-3 fix-up): the
  app-pipe `OperationalStateChange` routes Device "Oven" to the oven's
  cavity as CHIP's all-clusters app does (`OnOvenOperationalStateChange`),
  and the cavity's OperationalStateList holds Paused as that app's does
  (TC-OVENOPSTATE-2.1, 2.2, 2.4). TC-EEVSE-2.2 runs with its
  UserMaximumChargeCurrent write pointed at the case's own endpoint (the
  case hard-codes endpoint 1); TC-EEVSE-2.3 is recorded as a class (a) gap
  (step 19: CONSTRAINT_ERROR from the generated decoder where the case
  expects RESOURCE_EXHAUSTED; matter.js answers the same).
- `cluster/servicearea` (new): a ServiceArea (0x0150) server on the
  generated definition with the rules of matter.js's
  `ServiceAreaBaseServer` — the SupportedAreas / SupportedMaps /
  SelectedAreas / CurrentArea / Progress checks on construction and on
  every setter (`ErrInvalidState`), SelectAreas (UnsupportedArea, duplicate
  removal, Progress reset to Pending) with the device's decision through
  the `Selector` host port, SkipArea's default checks with the `Skipper`
  host port, `RemoveSupportedAreasEntry` / `RemoveSupportedMapsEntry`, and
  EstimatedEndTime reported per matter.js's quiet rule.
- `cluster/closure.NewDimension`: a ClosureDimension (0x0105) server on
  the generated definition with connectedhomeip's rules (SetTarget and
  Step checks, LimitRange clamp, Resolution rounding, the latch rules,
  CurrentState's reporting rule), the `DimensionHandler` host port and
  `DeviceTypeClosurePanel`.
- `cluster/thermo.NewThermostatUserInterfaceConfiguration`: the
  ThermostatUserInterfaceConfiguration (0x0204) server on the generated
  default server, starting at matter.js's values (Celsius unless the host
  says otherwise, KeypadLockout NoLockout).
- Reference daemon: the vacuum serves ServiceArea (one map, two areas,
  matter.js's RVC test-node decisions; the app pipe's `Reset` clears the
  selection), the thermostat serves ThermostatUserInterfaceConfiguration,
  and a ClosurePanel (0x0231) serves ClosureDimension as connectedhomeip's
  closure-app panel does, with its simulated motion. The chip-tool table
  declares the SEAR, CLDIM and TSUIC families.
- `cluster/modebase` serves six more ModeBase derivations, each on its
  generated definition: `NewOvenMode`,
  `NewRefrigeratorAndTemperatureControlledCabinetMode`,
  `NewMicrowaveOvenMode`, `NewEnergyEvseMode`, `NewWaterHeaterMode` and
  `NewDeviceEnergyManagementMode`, with their cluster ids
  (`ClusterIDOvenMode`, …) and tag constants (`OvenTag…`,
  `RefrigeratorTag…`, `MicrowaveTag…`, `EvseTag…`, `WaterHeaterTag…`,
  `DemTag…`). Each refuses the SupportedModes its matter.js server's
  `#assertSupportedModes` refuses (`ErrRequiredTag`, and the new
  `ErrTagCombination` for tags a derivation forbids in one mode).
  MicrowaveOvenMode's definition disallows ChangeToMode: that server
  accepts no command and needs no `ModeChanger`. The ModeTags count of a
  mode is held to each definition's constraint: 1 to 8, and 0 to 8 on
  MicrowaveOvenMode ("max 8"), where a mode without tags is accepted.
- `modebase.Server.MatterInvoke` takes the generated
  `ChangeToModeRequest` of every derivation's definition as well as
  `cluster/wire.ChangeToModeRequest`.
- `cluster/thermo.NewTemperatureControl`: a TemperatureControl (0x0056)
  server on the generated definition — TemperatureNumber (with optional
  TemperatureStep) or TemperatureLevel, the host port `TemperatureSetter`,
  `SetTemperatureSetpoint` / `SetSelectedTemperatureLevel` for changes the
  device makes. SetTemperature answers CONSTRAINT_ERROR for a target
  outside MinTemperature..MaxTemperature or off a Step, or a level past
  SupportedTemperatureLevels, INVALID_COMMAND for a missing field of its
  own feature, and INVALID_IN_STATE when the host refuses with
  `thermo.ErrTemperatureRefused`; the field of the other feature is
  ignored — all as connectedhomeip's TemperatureControl server answers.
- Generated definitions under `cluster/spec/` for EnergyEvseMode,
  WaterHeaterMode, DeviceEnergyManagementMode, MicrowaveOvenMode,
  OvenMode, RefrigeratorAndTemperatureControlledCabinetMode and
  TemperatureControl.
- The reference daemon bridges a Refrigerator with a
  TemperatureControlledCabinet (Cooler) part: RefrigeratorAndTemperature-
  ControlledCabinetMode on both, TemperatureControl (TN + STEP) on the
  cabinet.

- `cluster/light.ColorControlServer` serves XY (CurrentX / CurrentY,
  MoveToColor, MoveColor, StepColor), HS (CurrentHue / CurrentSaturation and
  the seven hue and saturation commands), EHUE (EnhancedCurrentHue and the
  Enhanced* commands) and CL (ColorLoopSet and the colour loop attributes)
  besides CT, as matter.js `ColorControlServer` does: every command on the
  transition engine with `ManageTransitions`, the colour-mode switching with
  matter.js's colour-space conversions, StopMoveStep over every mode, the
  feature-dependent attribute and command lists of the element file. It
  carries ExtendedColorLight (0x010D, XY and CT; matter.js
  `devices/extended-color-light.ts`) as well as ColorTemperatureLight.
- New host-facing API in `cluster/light`: `ColorControlServerConfig.Features`
  (`ColorFeature`: `ColorFeatureHueSaturation`, `ColorFeatureEnhancedHue`,
  `ColorFeatureColorLoop`, `ColorFeatureXY`, `ColorFeatureColorTemperature`),
  `InitialColorMode` (`ColorMode`), `InitialX` / `InitialY`, `InitialHue`,
  `InitialSaturation`, `InitialEnhancedHue`; `NewColorControl` (returns
  `ErrColorFeatures` for a selection the conformance forbids — CL without
  EHUE, EHUE without HS; `NewColorControlServer` panics on one, never on a
  configuration without `Features`); the colour sink `ColorWriter` /
  `Color` with `SetColorWriter`, and `ColorControlServer.Color`; the ported
  conversions `HSVToXY`, `XYToHSV`, `HSVToMireds`, `MiredsToHSV`,
  `HSVToRGB`, `RGBToHSV`, `RGBToXY`, `XYToRGB`, `XYToMireds`, `MiredsToXY`,
  `MiredsToKelvin`, `KelvinToMireds`; `ColorControlServer.MatterApplySceneValues`.
- `cluster/spec/colorcontrol`: the generated ColorControl definition. Linking
  it (any binary that imports `cluster/light` does) makes the bridge decode
  ColorControl requests through it, with matter.js's statuses.
- `cluster/core.SceneValuesApplier`: a scene-able server that recalls a
  scene's values itself (matter.js `implementScenes`); the module's
  ColorControl server does, so a recall moves x/y, the enhanced hue and the
  colour loop too (`BD-Matter-ColorControl-SceneRecall`).
- `cluster/transition.AddWithOverflow` (matter.js `addValueWithOverflow`).
- The reference daemon has an ExtendedColorLight ("Color Bulb") with every
  ColorControl feature, and the CHIP ColorControl family runs against it.
- `cluster/core.GeneralDiagnostics.SetPersistedOperationalTime` and
  `TotalOperationalTime`: the operational time at full resolution, which a
  host persists and seeds back instead of the TotalOperationalHours
  attribute, as matter.js persists its millisecond
  `totalOperationalHoursCounter`. `SetPersistedCounters` keeps its whole-hour
  seed (and now saturates instead of overflowing on a huge one).
- **The node's own commissioning window** (matter.js `DeviceCommissioner`,
  9397828d): `bridge.CommissioningWindow.OpenOwnWindow` opens the window an
  uncommissioned node opens at start (or the application opens) — 48 h
  without a fabric, 15 min with one (`OwnWindowTimeoutUncommissioned` /
  `OwnWindowTimeoutCommissioned`, from `SetFabricCounter`); restarted by a
  second call, closed by CommissioningComplete, RevokeCommissioning and its
  timer; `IsOwnWindow` tells it apart for a host's transition hook,
  `ErrAdministratorWindowOpen` refuses it over an administrator's window.
  `cluster/wire.AnyWindowReporter` is the optional controller capability
  RevokeCommissioning consults.
- **Parts of a bridged endpoint** (matter.js Endpoint `parts`):
  `endpoint.Spec.Parts` declares child endpoints of a bridged endpoint, each
  with its own persisted number from its `StableKey`, listed in the parent's
  Descriptor PartsList (`Endpoint.PartIDs`) and in the Aggregator's
  full-family one, with the parent as its ParentEndpoint. A part
  (`Endpoint.Part`) is a component, not a bridged node: its DeviceTypeList is
  its own device type alone, it serves no BridgedDeviceBasicInformation, and
  Identify only where its device type mandates it. `ValidateDeviceTypes`
  counts it, so a SmokeCoAlarm with a PowerSource (0x0011) part meets its
  component requirement. The reference daemon's smoke alarm carries its
  battery as such a part, as matter.js's `device-smoke-co-alarm` example
  does.

- Thirty-one more generated definitions under `cluster/spec` (ADR 0013): the
  fifteen measurement clusters (`temperaturemeasurement` …
  `electricalenergymeasurement`), `switchcluster`,
  `administratorcommissioning`, `groups`, `scenesmanagement`, and the system
  clusters `identify`, `descriptor`, `basicinformation`,
  `bridgeddevicebasicinformation`, `generaldiagnostics`,
  `generalcommissioning`, `networkcommissioning`, `timesynchronization`,
  `diagnosticlogs`, `binding`, `icdmanagement` and
  `otasoftwareupdaterequestor`.
- `measurement.AccuracyList` and `core.NetworkInterfaceList` (the attribute
  values the bridge encodes through the generated codecs);
  `measurement.AccuracyStruct`, `core.CapabilityMinimaStruct`,
  `core.ProductAppearanceStruct`, `core.BootReasonEvent`,
  `core.TimeSnapshotResponse` and `core.RetrieveLogsResponse` gain
  `EncodeTLV` (and the two responses `ResponseCommand`).
- `cluster/spec/closurecontrol`, `windowcovering`, `doorlock` and
  `thermostat`: the generated definitions of four more servers (ADR 0013),
  the AtomicRequest / AtomicResponse codecs of Thermostat — anonymous
  attribute-status entry struct included — among them.
- Error-returning constructors for the servers that now refuse a feature
  selection: `closure.New` (with `closure.DerivedFeatureMap` and
  `closure.ErrFeatureMap`), `cover.New` (with `cover.ServedFeatureMap` and
  `cover.ErrFeatureMap`) and `thermo.New` (with `thermo.ErrFeatures`).
  `closure.ControlServer`, `cover.WindowCoveringServer`,
  `lock.DoorLockServer` and `thermo.ThermostatServer` answer
  `MinInvokePrivilege` (and, where an attribute is writable,
  `MinWritePrivilege`) from their definitions.
- `cluster/spec.Server` (`spec.NewServer(def, opts, spec.ServerConfig)`):
  the generated default cluster server (ADR 0017, phase 0). A generated
  definition plus a feature selection becomes a complete
  `contract.ClusterServer` — attribute values starting at the model's
  defaults, an optional read port (`spec.Source`) and write port
  (`spec.Sink`), controller writes checked by `ValidateWrite`, device-side
  `Set` / `SetAttributes` checked against the same type and constraint
  checks, data-version bumps and change notifications, command dispatch
  through `Handle`, events through `SetMatterEventEmitter` / `Emit` with
  the definition's priority. New errors `spec.ErrFabricScoped` (a
  selection that serves a fabric-scoped attribute is refused),
  `spec.ErrNotServed`, `spec.ErrInvalidValue`, `spec.ErrNotEmitted`.
- `spec.Instance.CheckValue`: an attribute value against its type and
  constraint, without the access check.
- `cluster/core.NewFixedLabel(labels)`: a FixedLabel (0x0040) server on the
  generated server, with the new generated definition
  `cluster/spec/fixedlabel`. Not mounted by the reference bridge.

### Changed

- The bridge subscribes every mounted cluster server of a bridged endpoint
  that implements `contract.ChangeNotifier`, whether or not the endpoint's
  source or measurement notifies (before, the servers were wired only
  behind a notifying source); a server that is the source itself is
  subscribed once. A server that notifies its own changes reaches a
  subscriber without `device.WithChangeNotifier`, which stays as the relay
  for a host model whose servers do not forward its notifications —
  matter.js reports each behavior's change through its own Datasource.
- `cluster/spec` decides a revision condition ("Rev >= vN") against the
  definition's own revision instead of leaving it conditional, as matter.js's
  runtime conformance compiler does (`conformance-compiler.ts:250-254`): an
  element "Rev >= vN" or "Rev >= vN, O" is served by default at or above
  revision N, and refused when declared below it. `spec.Context` returns a
  `spec.RevisionContext`; a `FeatureContext` without a revision keeps the
  condition conditional, and `CheckFeatures` still ignores the revision
  (`IllegalFeatureCombinations.ts`). A default LevelControl from `device/`
  now serves MinLevel and MaxLevel with the device type's defaults
  (DimmableLight: 1 and 254).
- The OTA Software Update Requestor asks for 8192-byte BDX blocks when the
  provider's session runs over TCP and 1024 otherwise, as matter.js does
  (`OtaSoftwareUpdateRequestorServer.ts:54-55`, `:962-963`). An exchange
  `OtaProviderSession.OpenExchange` returns states its channel with an
  optional `Reliable() bool`; the bridge's exchanges do. The bridge's
  initiated exchanges follow the session's route: over a TCP connection
  they run without MRP and carry the connection's message size, and
  `ConnectOtaProvider` (and subscription re-establishment) reuse a live
  session bound to a TCP connection as they reuse one on UDP (matter.js
  `Peer.ts:418-438`).

- The operational DNS-SD record carries the `T` key only when a TCP mode
  is advertised (`T=2`, `4` or `6`); with TCP off the key is absent instead
  of `T=0`, as in matter.js (`MdnsAdvertisement.ts:204-206`,
  `ServerNetworkRuntime.ts:334-338`) and chip
  (`Advertiser_ImplMinimalMdns.cpp:279-285`). The commissionable record
  never carries it, as before.

- `examples/reference-bridge` runs on the device layer and the node facade
  (ADR 0018 decision 6), the shape the README points a host at:
  `node.New(cfg)`, one `device.New<Type>` call per device added to
  `Aggregator()`, `MarkComplete`, `Run`. Every flag keeps its name and
  meaning. Servers whose rules need a host port go in through
  `device.WithServer`, the host models' change notifiers through
  `device.WithChangeNotifier`, parts through `AddPart`; measurement-backed
  sensors keep `endpoint.Spec.Measurement`. The hand-written OnOff server is
  gone: the lights, the speaker and the pump use `cluster/onoff.Server`.
  The bring-up the facade performs left the daemon (`wiring.go`,
  `commissioning.go`, `root_optional.go`). Endpoint numbers, device types,
  cluster sets and attribute values are unchanged; the Descriptor
  ServerList of the vacuum, the air quality sensor and the oven cavity
  lists its clusters in the device type's order, as matter.js does. The
  snapshot scope is the node's (`"node"`): a database written by an
  earlier release keeps every endpoint number (the store looks numbers up
  by key), but its rows stay under the old scope `"demo"`, which the
  model-complete garbage collection no longer visits.

- PASE: PBKDFParamRequest and PBKDFParamResponse carry the whole
  SessionParameters structure (`InitiatorSessionParams`,
  `ResponderSessionParams`), and the response always states the node's
  parameters — the same ones Sigma2 advertises — as matter.js
  `PaseServer.ts:151` does; before, only a host-configured MRP triplet was
  sent, and a 1.3+ commissioner's revisions, MaxPathsPerInvoke and
  transports were dropped on decode (`BD-Matter-PASE-SessionParametersLegacy`,
  retired).
- The pinned gofumpt is v0.12.0 (was v0.10.0; `GOFUMPT_VERSION` in the
  Makefile and `ci.yml`). The two releases format a multi-line `append(`
  differently; v0.12.0 reformats nothing on `main` at the bump.
- ModeSelect SupportedModes is encoded by the generated codec
  (`cluster/spec/modeselect`): `modeselect.Server` reads it as a
  `spec.List[ModeOptionStruct]` and the bridge's hand-written case is gone.
  On the wire, SemanticTag MfgCode and Value now take the smallest integer
  width, as matter.js's `TlvOfModel` writes them (they were always two
  bytes). The list, its SemanticTags and Label are no longer trimmed at
  encode time: matter.js trims nothing, and the bounds
  (`SupportedModesMaxEntries`, `SemanticTagsMaxEntries`, `LabelMaxBytes`)
  are now documented on `ModeSource.SupportedModes` as the host's part.

- `cluster/spec` evaluates an "in" constraint on a controller write
  (`Instance.ValidateWrite`): the value must be an entry of the named peer
  attribute's current list, else CONSTRAINT_ERROR before the sink sees it;
  an unresolved peer admits nothing, as matter.js's `Constraint.test`.
  LocalizationConfiguration ActiveLocale and TimeFormatLocalization
  ActiveCalendarType answer as before; their hand-written membership checks
  are gone.

- `contract.MeasurementClass` gains seven built-in classes after
  `MeasurementFlow`. The built-in values keep their numbers; the classes
  `RegisterMeasurementKind` hands out start seven higher, so a host that
  persisted a registered class value must re-register rather than reuse it.
- The reference daemon's contact sensor is assembled from a Source instead
  of a Measurement (same StableKey, device type and BooleanState surface).
- `endpoint`: a bridged endpoint whose `Source` serves no cluster of its
  own — a composed device such as an Oven, whose device type mandates none
  and whose cavity is a part — now mounts the bridged node's Identify,
  Descriptor and BridgedDeviceBasicInformation; before, it mounted
  nothing. An endpoint with no source at all still mounts nothing.
- The bridge encodes the OperationalState, RvcOperationalState, ModeBase,
  SmokeCoAlarm, PumpConfigurationAndControl and ClosureControl values
  through the generated codecs (ADR 0013): the hand-written cases in
  `bridge/application_values.go` and the three ClosureControl cases in
  `defaultAttributeValueWriter` are gone, the wire bytes are unchanged.
  `cluster/wire`'s `ErrorStateStruct`, `OperationalStateStruct`,
  `OperationalCommandResponse`, `OperationalErrorEvent`,
  `OperationCompletionEvent`, `ModeOptionStruct`, `ChangeToModeResponse`,
  `FieldlessEvent`, `ClosureErrorList`, `*ClosureOverallCurrentState` and
  `*ClosureOverallTargetState` implement `spec.Encodable`; new list types
  `wire.OperationalStateList` and `wire.ModeOptionList`.
  `cluster/alarm.AlarmSeverityEvent` is now an alias of the generated
  `cluster/spec/smokecoalarm.SmokeAlarmEvent` (same field, same use).
- `cluster/filter` is built on `spec.Server`; its exported API and
  behaviour are unchanged, except that constructing a server now bumps its
  data version once (the initial values are stored as a device-side
  change), which is visible only on a host-owned `Config.DataVersion`.
- `spec.Instance.ValidateWrite` checks list and struct values too: a
  list's length against its constraint ("max 5"), each entry against the
  entry type and entry constraint, a struct's fields against their types
  and constraints (matter.js `ValueValidator.ts`, `constraint.ts`). A
  write of such a value that was accepted before can now be answered
  CONSTRAINT_ERROR.

- **The measurement servers, GenericSwitch, AdministratorCommissioning and
  fourteen system servers are built on their generated definitions** (ADR
  0013): `cluster/measurement` (every server), `cluster/wire`'s
  GenericSwitch and AdministratorCommissioning, and `cluster/core`'s
  Identify, Descriptor, BasicInformation, BridgedDeviceBasicInformation,
  GeneralDiagnostics, GeneralCommissioning, NetworkCommissioning,
  TimeSynchronization, DiagnosticLogs, Binding, ICDManagement,
  OTASoftwareUpdateRequestor, Groups and ScenesManagement read their ids,
  revisions, FeatureMaps, lists, event priorities, privileges and write
  statuses from the packages above. Exported names keep compiling; these
  are now aliases of the generated types: `measurement.EnergyMeasurementStruct`,
  `measurement.BooleanStateChangeEvent`, `wire.SwitchInitialPressEvent` /
  `SwitchLongPressEvent` / `SwitchShortReleaseEvent` /
  `SwitchLongReleaseEvent`, `core.DeviceTypeStruct`, `core.StartUpEvent` /
  `ShutDownEvent` / `LeaveEvent`, `core.ReachableChangedEvent`,
  `core.DeviceLoadStruct`, `core.TestEventTriggerRequest` and
  `core.PayloadTestResponse` (same fields). The exported cluster, feature
  and enum constants of these packages read the definitions' values
  (unchanged). PowerSource's, BasicInformation's and
  BridgedDeviceBasicInformation's AttributeLists are in id order; the
  AdministratorCommissioning AttributeList no longer names FeatureMap and
  ClusterRevision itself (the dispatcher adds every global, so the wire is
  unchanged).
- **Host servers of these clusters receive typed requests.** Linking the
  packages makes the bridge decode their requests through the generated
  definitions, so a host's own server of one of them now receives the
  generated request struct instead of a generic `map[uint8]any`, and a
  request missing a mandatory field is answered INVALID_COMMAND (a value
  outside its bounds CONSTRAINT_ERROR) before the server runs:
  - Identify: `identify.IdentifyRequest`, `TriggerEffectRequest` (a missing
    IdentifyTime is INVALID_COMMAND, where the server read it as 0).
  - GeneralDiagnostics: `generaldiagnostics.TestEventTriggerRequest`
    (= `core.TestEventTriggerRequest`), `TimeSnapshotRequest`,
    `PayloadTestRequestRequest` (a Value above 255 is CONSTRAINT_ERROR,
    where the server answered INVALID_COMMAND).
  - TimeSynchronization: `timesynchronization.SetUtcTimeRequest` and the
    unserved feature requests.
  - DiagnosticLogs: `diagnosticlogs.RetrieveLogsRequestRequest` (an Intent
    outside IntentEnum or a TransferFileDesignator over 32 bytes is
    CONSTRAINT_ERROR).
  - OTASoftwareUpdateRequestor: `otasoftwareupdaterequestor.AnnounceOtaProviderRequest`.
  - ICDManagement, AdministratorCommissioning's RevokeCommissioning and
    OpenBasicCommissioningWindow, and the unserved NetworkCommissioning
    requests decode through their definitions too.
  - OpenCommissioningWindow, the GeneralCommissioning requests and the core
    Groups and ScenesManagement requests keep their hand-written decoders
    and `cluster/wire` / `cluster/core` structs
    (`BD-Matter-AdminCommissioning-OpenWindowParams`,
    `BD-Matter-Commissioning-HandDecoders`).
- Writes answered from the definitions: every migrated server answers a
  write to a served read-only attribute with UNSUPPORTED_WRITE and one to
  an attribute it does not serve with UNSUPPORTED_ATTRIBUTE (was a plain
  error, read as FAILURE; TimeSynchronization answered UNSUPPORTED_WRITE for
  both). The servers' own sentinel errors stay in the chain
  (`errors.Is` / `errors.Unwrap` as before).
- **ClosureControl, WindowCovering and Thermostat refuse a feature
  selection they do not serve** (owner decision recorded in ADR 0013: a
  server derives its FeatureMap from what it serves). A host whose
  FeatureMap matches — or that leaves it zero — is unaffected. Otherwise
  `NewControlServer`, `NewWindowCoveringServer` and `NewThermostatServer`
  now **panic**, and the new `New` constructors return the error:
  - `closure.Config.FeatureMap`: Positioning plus any of Ventilation,
    Pedestrian, Protection and ManuallyOperable (zero still means
    Positioning + Ventilation). MotionLatching, Speed, Calibration,
    Instantaneous or a selection without Positioning advertised elements the
    server never served (LatchControlModes, the Speed fields, Calibrate, no
    Stop) and are refused.
  - `cover.Config.FeatureMap`: Lift + PositionAwareLift (0x05) only; zero
    now means that, where it advertised 0. A Tilt or Lift-only FeatureMap
    listed attributes the server has no state for, or disallowed the
    position attributes it serves.
  - `thermo.ThermostatConfig.Features`: HEAT and / or COOL with optional
    AUTO (still cleared without both) and LTNE. Neither HEAT nor COOL —
    which matter.js refuses, both being "AUTO, O.a+" — and Occupancy,
    Setback, MatterScheduleConfiguration, Presets, Events and
    ThermostatSuggestions are refused; such a server used to start in
    SystemMode Off, or advertise features whose attributes it never served.
- **Four more servers are built on their generated definitions** (ADR 0013):
  `cluster/closure`, `cluster/cover`, `cluster/lock` and `cluster/thermo`
  read their ids, revisions, attribute / command / event lists, event
  priorities, write checks and privileges from the four packages above.
  ClosureControl's AttributeList no longer names FeatureMap and
  ClusterRevision from the server (the dispatcher adds every global, so
  the wire is unchanged); `closure.MovementCompletedEvent`,
  `EngageStateChangedEvent` and `SecureStateChangedEvent` are aliases of
  the generated payloads; `cover.ClusterRevision`, `closure.ClusterRevision`,
  `thermo.ThermostatClusterID` / `ThermostatClusterRevision` and the
  `thermo.ThermostatFeature*` constants read the definitions' values
  (unchanged). The ClosureControl, WindowCovering and Thermostat
  MatterReportable lists name every served attribute that moves.
- **Host servers of these clusters receive typed requests.** Linking one of
  the four packages (any binary that imports the server package does) makes
  the bridge decode that cluster's requests through the generated
  definition, so a host's own server now receives the generated request
  struct instead of a generic `map[uint8]any`, and a request missing a
  mandatory field or a field of the wrong TLV type is answered
  INVALID_COMMAND (a value outside its bounds CONSTRAINT_ERROR) before the
  server runs:
  - ClosureControl: `closurecontrol.StopRequest`, `MoveToRequest`,
    `CalibrateRequest` — a null MoveTo field is now INVALID_COMMAND, as
    matter.js's request schema answers it, where the tag map read it as
    absent. `closure.ControlServer` carries MoveTo over to the
    `cluster/wire.MoveToRequest` it took before.
  - WindowCovering: `windowcovering.UpOrOpenRequest`, `DownOrCloseRequest`,
    `StopMotionRequest`, `GoToLiftPercentageRequest` (a value above 10000
    CONSTRAINT_ERROR), `GoToTiltPercentageRequest`.
  - DoorLock: `doorlock.LockDoorRequest`, `UnlockDoorRequest`,
    `UnboltDoorRequest` and the rest of the cluster's requests;
    `lock.DoorLockServer` reads none of their fields, as before.
    `cluster/wire.DoorLockRequest` / `DecodeDoorLockRequest` stay.
  - Thermostat: `thermostat.SetpointRaiseLowerRequest` and the schedule,
    preset and atomic requests.
- Writes answered from the definitions: `cover.WindowCoveringServer`,
  `lock.DoorLockServer` and `thermo.ThermostatServer` answer a write to a
  served read-only attribute with UNSUPPORTED_WRITE and one to an attribute
  they do not serve with UNSUPPORTED_ATTRIBUTE (was a plain error, read as
  FAILURE); a value of the wrong type is CONSTRAINT_ERROR. A Thermostat
  setpoint of an absent feature is UNSUPPORTED_ATTRIBUTE.
- **Seven more servers are built on their generated definitions** (ADR 0013):
  `cluster/onoff` (identity), `cluster/valve`, `cluster/modeselect`,
  `cluster/alarm`, `cluster/fan`, `cluster/opstate` and
  `cluster/levelcontrol` read their ids, revisions, enums, attribute /
  command / event lists, write checks and privileges from the new
  `cluster/spec/onoff`, `valveconfigurationandcontrol`, `modeselect`,
  `smokecoalarm`, `fancontrol`, `operationalstate`, `rvcoperationalstate`
  and `levelcontrol` packages. Every exported name keeps compiling: the
  enum and struct types are now aliases of the generated ones
  (`valve.State`, `modeselect.ModeOptionStruct` / `SemanticTagStruct` /
  `ChangeToModeRequest`, `alarm.AlarmState` / `Sensitivity` /
  `ExpressedState` / `MuteState` / `EndOfService` / `ContaminationState` /
  `Feature`, `fan.Feature` / `FanMode` / `Sequence` / `RockBitmap` /
  `WindBitmap` / `AirflowDirection`). `fan.Server` gains
  `MinWritePrivilege`.
- **Host servers of these clusters receive typed requests.** Linking one of
  the packages above makes the bridge decode that cluster's requests through
  the generated definition, so a host's own server now receives the
  generated request struct instead of a generic `map[uint8]any`, and a
  request missing a mandatory field is answered INVALID_COMMAND before the
  server runs:
  - OnOff: `onoff.OffRequest`, `OnRequest`, `ToggleRequest`,
    `OffWithEffectRequest`, `OnWithRecallGlobalSceneRequest`,
    `OnWithTimedOffRequest` (`cluster/spec/onoff`). OnWithTimedOff's
    OnTime / OffWaitTime "max 65534" is CONSTRAINT_ERROR there. The
    reference daemon's OnOff server takes `OnWithTimedOffRequest`.
  - ValveConfigurationAndControl: `OpenRequest`, `CloseRequest`
    (`cluster/spec/valveconfigurationandcontrol`); `valve.Server` carries
    Open over to the host's `valve.OpenRequest` unchanged.
  - ModeSelect: `ChangeToModeRequest` (= `modeselect.ChangeToModeRequest`).
  - OperationalState / RvcOperationalState: `PauseRequest`, `StopRequest`,
    `StartRequest`, `ResumeRequest`, `GoHomeRequest`.
  - SmokeCoAlarm: `SelfTestRequestRequest`.
  - FanControl Step and every LevelControl request keep their previous
    shapes (`cluster/wire.FanStepRequest`, `wire.MoveToLevelRequest`, the
    tag map): the bridge decodes them ahead of the generated definition
    (`BD-Matter-LevelControl-LenientDecoders` — Google Home omits
    LevelControl's TransitionTime).
- Writes answered from the definitions: `levelcontrol.Server` answers a
  write to MinLevel or MaxLevel with UNSUPPORTED_WRITE (was
  UNSUPPORTED_ATTRIBUTE; both are served read-only attributes);
  `valve.Server` refuses a DefaultOpenDuration of 0xFFFFFFFF (the null of a
  nullable elapsed-s) with CONSTRAINT_ERROR.
- **Host ColorControl servers:** the ColorControl requests other than
  MoveToHue, MoveToSaturation, MoveToHueAndSaturation and
  MoveToColorTemperature now reach `MatterInvoke` as the generated
  `cluster/spec/colorcontrol` request structs (for example
  `colorcontrol.MoveColorTemperatureRequest`) instead of a generic
  `map[uint8]any` whenever that package is linked, and a request missing a
  mandatory field is answered INVALID_COMMAND before the server runs. The
  four keep their `cluster/wire` structs
  (`BD-Matter-ColorControl-LegacyDecoders`); MoveToHue, MoveToSaturation and
  MoveToHueAndSaturation now carry their OptionsMask / OptionsOverride,
  which the bridge dropped.
- `light.ColorControlServer` answers a write to a read-only attribute with
  UNSUPPORTED_WRITE and one to an attribute it does not serve with
  UNSUPPORTED_ATTRIBUTE (was a plain error), and a command its features do
  not serve with UNSUPPORTED_COMMAND.
- `light.ColorControlServer.SyncColorTemperatureWithLevel` couples the colour
  temperature to the level only in the colour temperature mode, as matter.js
  `syncColorTemperatureWithLevel` does (a CT-only server is always in it).
- `light.ColorControlServer` deliberately departs from matter.js in one
  conversion: a switch from the XY mode to hue and saturation converts the
  colour with `xyToHsv` (`XYToHSV`), where matter.js calls `hsvToXy` with x
  and y and starts from a hue near 0 (`BD-Matter-ColorControl-XYToHS`, an
  upstream matter.js issue candidate).
- **Subscriptions honour the MinIntervalFloor the subscriber requests.**
  `subscription.Config.MinIntervalFloorSeconds` defaulted to 1 s and lifted
  every request to it; the default is now 0, as matter.js
  `ServerSubscription` keeps the requested floor. A subscriber that asks for
  0 gets a change on the next engine tick (250 ms) even right after another
  report; before, it waited out a second, and a value read at sending time
  — RemainingTime — went out up to 12 tenths short (TC-CC-2.2). A host that
  wants the old floor sets `MinIntervalFloorSeconds: 1`.
- **An InvokeRequest's DelayReportData now defers subscription reports**:
  every subscription that selects an endpoint the commands target holds its
  next report by DelayMinMs plus a jitter below DelayJitterWindowMs, as
  matter.js does with its `delay-report-data` forward feature on
  (`BD-Matter-DelayReportData`; `subscription.Manager.DeferReports`,
  `Subscription.DeferReports`). TC-IDM-1.5 had passed only through the 1 s
  floor above.
- `light.ColorControlClusterRevision` is the generated definition's
  `colorcontrol.Revision` (still 9).
- **Commissionability follows the node's own window.** An administrator's
  OpenCommissioningWindow replaces a window the node opened itself instead of
  answering Busy, and RevokeCommissioning closes it instead of answering
  WindowNotOpen; WindowStatus keeps reading WindowNotOpen for it, as matter.js
  keeps it for administrator windows only. `CommissioningWindow.IsOpen` — the
  ArmFailSafe-over-CASE guard — counts the node's own window too, as matter.js
  `DeviceCommissioner.windowStatus` does.
- **The reference daemon is commissionable only while uncommissioned.** It
  opens its own window at start when it has no fabric, attaches its
  configured-passcode PASE acceptor and announces CM=1 only while that window
  is open, and withdraws both when the window closes (commissioning
  complete, revoked, timed out); losing its last fabric reopens it. Before, a
  commissioned daemon stayed commissionable with its passcode for its whole
  lifetime. A host that embedded the same pattern (baseline acceptor
  attached once, CM=1 announced at every boot) should move to
  `OpenOwnWindow`.
- `endpoint.Topology.Bridged` leaves out parts (`Endpoint.Part`): they are
  components of a bridged device, not bridged devices.

### Deprecated

- `github.com/SukramJ/go-fabric/secure/spake2`: `MRPParameters`,
  `PBKDFParamRequest.InitiatorMRPParams`,
  `PBKDFParamResponse.ResponderMRPParams`; and in `bridge`,
  `PaseAdapter.SetResponderMRPParams` and `PaseAdapter.PeerMRPParams`.
  Replacement: `sessionparams.Parameters` through
  `InitiatorSessionParams` / `ResponderSessionParams`,
  `SetResponderSessionParams` and `PeerSessionParams`. The old fields keep
  their behaviour (decoded as the first three fields, encoded when the new
  field is nil). Marked in v0.3.0; removal permissible from v0.5.0.
- `github.com/SukramJ/go-fabric/cluster/modeselect`: the field-tag
  constants `ModeOptionFieldLabel`, `ModeOptionFieldMode`,
  `ModeOptionFieldSemanticTags`, `SemanticTagFieldMfgCode` and
  `SemanticTagFieldValue`. Replacement: the generated codec
  (`cluster/spec/modeselect` `ModeOptionStruct.EncodeTLV`,
  `SemanticTagStruct.EncodeTLV`), which carries the field ids itself.
  Removal permissible in the second minor release after the one that ships
  this entry.

### Removed

Marked in v0.2.0, removal announced there. A host that still uses one of
them moves to the replacement named here; none has a behaviour of its own
left to keep.

- `github.com/SukramJ/go-fabric/cluster/wire`: `Groups`, the read-only
  Groups stub. Replacement: none to wire — group membership is stack state;
  configure `endpoint.Config.Groups` and the assembler mounts the real
  `cluster/core.Groups` server wherever a device type mandates Groups
  (ADR 0009).
- `github.com/SukramJ/go-fabric/im`: `StatusUnreportableAttr` (0x8c) and
  `StatusNoUpstreamSubscription` (0xc5). Matter 1.6.1 removed both codes and
  matter.js dropped them from its status table; no replacement.
- `github.com/SukramJ/go-fabric/store`: the never-wired subscription API —
  `PersistentSubscriptionRecord`, `ErrPersistentSubscriptionNotFound`,
  `Store.SavePersistentSubscription`, `Store.LoadPersistentSubscriptions`,
  `Store.DeletePersistentSubscription`,
  `Store.DeletePersistentSubscriptionsByFabric`,
  `Store.GetPersistentSubscription`, `PersistentSubscriptionIntervals`,
  `MarshalIntervals` and `UnmarshalIntervals`. Replacement:
  `Store.SaveServerSubscription` and its siblings on the
  `matter_server_subscriptions` table (ADR 0008). The
  `matter_persistent_subscriptions` table itself stays in `store.Schema()`:
  every database a v0.2.x store applied already carries it, nothing reads
  or writes it, and keeping the CREATE leaves old and new databases with the
  same schema.

### Fixed

- An ongoing subscription report the controller never acknowledges is peer
  loss, as matter.js treats an exhausted first message on any exchange
  (`MessageExchange.ts:616-621`, `SessionManager.ts:677-695`): every
  session with that peer established before the report's exchange is
  force-closed without a CloseSession, and their other subscriptions close
  with them. Before, only the one subscription was closed and the peer's
  sessions stayed in the table. A later chunk of a chunked report, sent
  after the peer answered the previous one, still closes only its
  subscription.

- **A peer that stops answering an exchange this node opened is lost, and
  its sessions go.** When the outbound-reliable tracker gave up on a
  message of an exchange the bridge itself opened (the OTA requestor's
  QueryImage or a BDX block), nothing happened to the session: the next
  query reused it, and a provider restarted under the same node id, which
  knows nothing of the old keys, was never reached again (TC-SU-2.2 step 2
  announced 24 times in vain). Max retransmissions on such an exchange now
  force-close every session with that peer established before the
  exchange, without a CloseSession to a peer that is gone, so the next
  connect runs a fresh CASE. `operational.Manager.ClosePeerLost` is the
  new primitive; `Entry.CreatedAt` bounds the sweep. Mirrors matter.js
  `MessageExchange.ts:620` → `ExchangeManager.ts:556-580` →
  `SessionManager.ts:677-695`. A subscription report that runs out of
  retransmissions still closes only the subscription, as before.
- **Harness: the OTA provider's store is removed after the provider is
  gone, before and after each SU case.** TC-SU-2.2's own teardown removes
  `/tmp/chip_kvs_provider` and then terminates the provider, whose
  shutdown writes the store back (chip's Linux store rewrites the whole
  file on commit), so TC-SU-2.4's provider came up already commissioned
  and never advertised for commissioning. The harness now stops the
  provider, waits until it is gone and removes the store, at the start of
  an SU case as well as after it.
- **A new CASE no longer closes the peer's previous sessions.** The session
  manager used to evict every session of the same (fabric, peer node) when
  that peer established another CASE session. A controller that runs two
  clients under one node identity (the CSA Python harness and chip-tool do)
  saw each client's new session close the other's, with a CloseSession on
  the wire every time, and every subscription on the closed session died
  with it: the DEM certification cases 2.2, 2.4, 2.6 and 2.10 lost their
  event and attribute reports that way. The manager now keeps a peer's
  sessions and caps them at `operational.MaxSessionsPerPeer` (5); beyond
  the cap the least recently active responder session is force-closed
  without a CloseSession, sessions this node initiated never claim a slot,
  and a PASE session never triggers the check. Mirrors matter.js
  `SessionManager.ts:185-189` (`MAX_SESSIONS_PER_PEER`) and `:647-680`
  (`#evictExcessSessionsFor`). The retired divergence
  `BD-Matter-CASE-StalePeerEviction` is removed from
  `notes/parity/by_design.md`; the Apple-Home stale-key replay it was
  introduced for is now handled the way matter.js handles it, by the cap.
- **Root cluster servers that name their changed attributes are
  reported.** The bridge wired `contract.AttributeChangeNotifier` only on
  bridged endpoints, so a root server keeping its own state — the OTA
  Software Update Requestor's UpdateState, UpdatePossible and
  UpdateStateProgress — changed without a subscription report (TC-SU-2.2
  waited for DelayedOnQuery in vain). Root and aggregator servers are now
  wired as well; their own DataVersion stays authoritative.
- **Timed commands come from the model.** An untimed invoke of a command
  matter.js marks "T" answers NEEDS_TIMED_INTERACTION for every cluster,
  not only for the hand-pinned AdministratorCommissioning, DoorLock and
  ClosureControl commands: an untimed EnergyEvse EnableCharging or
  ClosureDimension SetTarget used to be accepted. The IM asks the resolved
  cluster server first through the new optional `contract.TimedCommander`
  (implemented by `spec.Instance`, so by `spec.Server` and every server
  embedding an Instance); `schema.IsTimedInvoke` is now generated from
  `parity/schema.json` (`schema/timed_gen.go`, `make
  generate-matter-schema`) and remains the fallback for hand-written
  servers. New optional dispatcher capability `im.TimedCommandProvider`,
  implemented by `endpoint.TopologyDispatcher`.
- **A transition's rate and its RemainingTime came from two reads of the
  current value.** `cluster/light` (MoveToSaturation, MoveToColorTemperature)
  and `cluster/levelcontrol` read the current value, computed the rate, and
  then called `transition.Engine.Start`, which read the value again under
  the engine's lock; a step of the running transition landing in between
  gave a rate from the old value and a RemainingTime from the new one, one
  tenth short (149 for 150), visible as `TestRemainingTimeReportsAtCommandTime`
  failing in about a third of `-race` runs. `transition.Transition.RateFrom`
  computes the rate inside `Start` from the engine's own read, as matter.js
  reads and starts in one synchronous handler; the three callers use it.
- **ElectricalEnergyMeasurement.Accuracy is one MeasurementAccuracyStruct,**
  not a list (matter.js electrical-energy-measurement.element.ts and
  connectedhomeip's cluster XML agree); the server reported a one-entry
  list, which a typed controller fails to decode. `MatterRead` now returns
  a `measurement.AccuracyStruct` for it.
- **The electrical Accuracy entries name the right MeasurementType:**
  ActivePower (0x05) and ElectricalEnergy (0x0E) per
  measurement-type-enum.element.ts, where they named RmsVoltage (0x08) and
  RmsCurrent (0x09).
- **RetrieveLogsResponse is encoded.** The bridge had no encoder for
  `core.RetrieveLogsResponse` and answered RetrieveLogsRequest with an
  empty structure; it now carries Status, LogContent, UTCTimeStamp and
  TimeSinceBoot under RetrieveLogsResponse (0x01).
- **Thermostat SystemMode follows SystemModeEnum.** A heating-only
  thermostat refused SystemMode 7 as "Precooling" and accepted Precooling
  (6); 7 is FanOnly (conformance O), which matter.js accepts, and Precooling
  is refused. SystemMode Auto without the AUTO feature is CONSTRAINT_ERROR,
  as the enum's conformance makes it.
- **Thermostat LocalTemperatureCalibration is writable** ("RW VM", plain
  state in matter.js); the server listed it and refused every write.
- **TotalOperationalHours keeps the part-hours of every run.** The reference
  daemon persisted the attribute in whole hours, so each restart dropped its
  part-hour and a bridge restarting more often than hourly never gained one.
  It now persists milliseconds under the settings key `gendiag.operational_ms`
  (matter.js `totalOperationalHoursCounter`); a database of an earlier
  release is migrated on first start — its `gendiag.operational_hours` count
  is read as hours × 3,600,000 ms and the next store writes the new key. A
  host that persisted the attribute itself should switch to
  `TotalOperationalTime` the same way.

### Documentation

- `notes/parity/by_design.md`: eleven entries for the decisions of waves
  2–4 (BooleanStateConfiguration, UnitLocalization, WaterHeaterManagement,
  EnergyPreference, AlarmBase, the appliance controls and the oven cavity,
  DeviceEnergyManagement / EnergyEvse, ClosureDimension, SoilMeasurement,
  PowerSourceConfiguration, EcosystemInformation, the commodity servers);
  `BD-Matter-TimedAndQuotaDeferred` records the command half as closed and
  the quota half as mirroring matter.js. The findings register gains the
  host-side `Constraint.In` gap. `docs/open-items.md` drops the ModeSelect
  encoder item and notes `schema/timed.go` as generated;
  `docs/matterjs-comparison.md` counts 99 of 135 clusters served.
- The target of the module is now stated: Matter 1.6.1 complete for the
  home-automation scope, measured against the snapshot and phased in
  [`docs/concept-matter-1.6.1-and-device-layer.md`](./docs/concept-matter-1.6.1-and-device-layer.md)
  (Appendix F lists every exclusion with its reason). Two planning ADRs
  record the decisions it rests on:
  [ADR 0017](./docs/adr/0017-generated-default-cluster-server.md), a
  generated default cluster server, and
  [ADR 0018](./docs/adr/0018-device-layer-and-node-facade.md), a generated
  `device/` layer with a `node/` facade. `docs/matterjs-comparison.md`,
  `docs/open-items.md` and `docs/feature-scope.md` point at them; TCP and
  BDX leave the non-goal list. No code changes.

### Security

- Built with Go 1.27.2, which fixes Go standard-library vulnerabilities
  (net/http and HTTP/2, crypto/tls, net/textproto, html/template).
  `golang.org/x/net` moves to v0.60.0 and `golangci-lint` to v2.14.0 (reads
  Go 1.27.2's export data).

## [0.2.0] — 2026-10-07

Six pull requests since `v0.1.0` (#22–#28): the matter.js pin at Matter
1.6.1, subscription resumption, real groups and Groupcast, a transition
engine, fourteen more cluster servers, cluster definitions generated from
the matter.js model, device-type validation of every topology, and the CSA
certification families as the certifiability gate (ADR 0011: certification
is not pursued, certifiability is). The full entries follow this summary.

**What a host must do when moving from `v0.1.0`:**

- **Re-pin the schema snapshot** if the host pins `parity/schema.json` by
  hash: `schema.SchemaSnapshotSHA256` changed three times (Matter 1.6.1, the
  resolved model, the device-type requirements). Pin once, after upgrading.
- **Expect the Interaction Model revision 12** (was 13) on every IM message,
  as matter.js sends it.
- **Supply `endpoint.Config.VendorName`** (or a per-endpoint
  `Spec.VendorName`); the module no longer fills in a vendor name of its own,
  and a host that sets neither serves no VendorName on bridged endpoints.
- **Renumber host-registered measurement kinds**: the built-in
  `contract.MeasurementFlow` shifts every value `RegisterMeasurementKind`
  hands out by one.
- **Drop any host-supplied Groups server**: the assembler mounts the module's
  Groups server wherever a device type mandates it, replacing a host's.
- **Watch for `matter.devicetype.violation` warnings** at Start and
  Reassemble, and for a refused topology when a singleton cluster is
  misplaced (matter.js's warn mode; `Bridge.SetDeviceTypeValidation` selects
  strict or off).
- **New fabrics start with an empty Label** (`OpcredsConfig.InitialFabricLabel`
  to override).
- **Subscription resumption is on by default** (`Bridge.SetSubscriptionPersistence`
  to disable); a host that wants it must attach the store, the operational
  resolver and the CASE-initiator provider (ADR 0008).
- **Deprecated, removable in v0.3.0:** `wire.Groups`, the two removed status
  codes, and the never-wired `PersistentSubscription*` store API.

What is still open is tracked in [`docs/open-items.md`](./docs/open-items.md).

### Added

- The reference daemon mounts TimeSynchronization and DiagnosticLogs on its
  root (the logs are its diagnostic event ring, inline, no BDX), an
  AirPurifier endpoint with HEPA and activated-carbon filter monitoring
  (`cluster/filter`) in the state matter.js's all-clusters test app starts
  them, and a garage door (Closure: ClosureControl with Positioning and
  Ventilation, a simulated drive, the CHIP ClosureControl test event
  triggers). The certification families TIMESYNC, DLOG, ICDM, SU, BIND,
  HEPAFREMON, ACFREMON and CLCTRL run; TC-DGGEN-2.4 runs now that
  TimeSynchronization is there.

- `core.GeneralDiagnostics.SetUTCClock`: TimeSnapshot carries
  PosixTimeMs when the node's TimeSynchronization holds a UTC time, as
  matter.js `GeneralDiagnosticsServer.timeSnapshot` couples them (and as
  TC-DGGEN-2.4 requires). `core.TimeSynchronization.UTC` is that clock.
- `core.TimeSynchronization` lists SetUTCTime in AcceptedCommandList
  (`MatterAcceptedCommands`); the list was empty, although the command is
  mandatory.

- `closure.ControlServer` emits the ClosureControl events —
  OperationalError (`ReportError`, which also enters the Error state),
  MovementCompleted (MainState Moving → Stopped), SecureStateChanged (a
  change of OverallCurrentState.SecureState) and, with ManuallyOperable,
  EngageStateChanged (`SetEngaged`) — lists them and its commands in
  EventList / AcceptedCommandList (`MatterEvents`, `MatterAcceptedCommands`,
  `SetMatterEventEmitter`, `SetEndpoint`), and tells the bridge which
  attributes a state change moved (`OnMatterAttributesChanged`), so an
  arrival or an error between commands reaches subscribers.

- **Attribute transitions** — `cluster/transition`, a port of matter.js's
  `Transitions.ts`: `transition.New` / `Engine` (`Start`, `Stop`,
  `StopAll`, `Finish`, `FinishAll`, `Cancel`, `CancelAll`,
  `RemainingTime`, `Active`), `Config`, `Property`, `Transition`, `Change`,
  `HueDistance` / `HueDirection`, `DirectionalDistance`,
  `DefaultStepInterval`, `ExternalTimeUnit`. Values step every 100 ms and
  round half up; RemainingTime is reported by matter.js's
  `#updateRemainingTime` rules; an idle engine owns no goroutine.
- `levelcontrol.Config.Lighting` serves the LT feature — MinLevel 1,
  RemainingTime, StartUpCurrentLevel, the CoupleColorTempToLevel option
  (`AttrRemainingTime`, `AttrStartUpCurrentLevel`, `LightingLevelMin`,
  `Server.MinWritePrivilege`). `levelcontrol.Config.Transitions`
  (`Transitions`, `OnOff`, `ColorTemperatureCoupling`) runs the eight
  commands on the engine for a device that cannot ramp — matter.js
  LevelControlServer with managedTransitionTimeHandling — and hands the
  host each level as an immediate MoveToLevel. Left nil, the commands are
  forwarded as before (`BD-Matter-LevelControl-NativeRamp`).
- `light.ColorControlServerConfig.ManageTransitions`,
  `TransitionStepInterval` and `OnOff` (`light.OnOffState`):
  gradual colour temperature transitions with a live RemainingTime, and the
  ExecuteIfOff gate. `ColorControlServer.SyncColorTemperatureWithLevel`
  couples the colour temperature to the level.
- `contract.SelfReportedAttributeLister`: attributes a server reports by
  itself, which the bridge leaves out of a command's before/after
  comparison — a quieter ("Q") attribute such as CurrentLevel is reported
  by its own rules. `contract.ClusterQuiescer`: the bridge stops a server's
  timers when the server leaves the topology and on `Stop`.
- `cluster.Quieter.EmitNow` (matter.js `QuietObservable.emitNow`).

- **The schema snapshot is cross-checked against connectedhomeip's data
  model** (`internal/chipdm`, ADR 0015): every cluster (135) and device type
  (91) of `parity/schema.json` is compared with CHIP's `data_model/1.6.1`
  XML — the model the CSA's Python certification cases judge a device by —
  at the harness image's CHIP commit. Every difference is classified
  (matter.js right, CHIP right, representation, or excused by the harness)
  in a reviewed table that fails when it goes stale;
  [`docs/chip-datamodel-crosscheck.md`](./docs/chip-datamodel-crosscheck.md)
  is the generated report. The XML is read at test time from a
  connectedhomeip checkout and never committed (its CSA notice forbids
  publishing or derivative works); without one the cross-check skips.
  `make chiptool-setup` adds the data model directory to the sparse CHIP
  checkout; `make chipdm-check` runs the check (failing instead of
  skipping), `make chipdm-report` rewrites the report, and CI runs it in a
  new job, "chip data model cross-check".

- **ScenesManagement (0x0062) is a real server** on the bridged lights
  (ADR 0012), a port of matter.js's `ScenesManagementServer`:
  `cluster/core.ScenesManagement`, `NewScenesManagement`, `ScenesConfig`,
  `ScenesState` / `NewScenesState` / `LoadScenesState`, the request and
  response types (`AddSceneRequest`, `SceneRef`, `ViewSceneResponse`, …) and
  `SceneInfoStruct`. Scenes capture and recall the endpoint's OnOff,
  LevelControl and ColorControl state; FabricSceneInfo reports every change;
  a removed group or fabric takes its scenes along. The host persists the
  table through `endpoint.Config.Scenes` (`endpoint.ScenesStore`);
  `groups.Manager.StoredFabrics` lists the fabrics FabricSceneInfo covers.
  Replaces the stub that rejected every command (`BD-Matter-P2-D18`, retired).

- The reference daemon answers CHIP's `SimulateConfigurationVersionChange`
  on its app pipe and persists the raised ConfigurationVersions.

- `cluster/core` GeneralDiagnostics: `EnableTestEventTriggers(key, handler)`
  arms TestEventTrigger with a 16-byte test enable key and a host handler
  (`TestEventTriggerHandler`, `TestEventTriggerRequest`,
  `TestEnableKeySize`), as matter.js's `deviceTestEnableKey` does; the
  DataModelTest (DMTEST) feature with PayloadTestRequest / Response
  (`PayloadTestResponse`), mandatory above a MaxPathsPerInvoke of one; and
  DeviceLoadStatus (0x000A, `DeviceLoadStruct`, `SetDeviceLoadProvider`),
  mandatory at cluster revision 3 — `bridge.AttachRootClusters` wires the
  bridge's own counters into it (`Bridge.DeviceLoad`).
- `bridge.DefaultSessionParameters`: what Sigma2 advertises when the host
  sets nothing — matter.js `SessionParameters.defaults`, with the module's
  revisions, SpecificationVersion and MaxPathsPerInvoke.
  `sigma.Responder.SessionParameters` reads a responder's own.
- `schema.IsFabricScopedInvoke`: the commands matter.js marks fabric-scoped.
- `im.WithTimedInteraction` / `im.TimedInteractionFromContext`.

- `contract.AttributeChangeNotifier`: a bridged cluster server that keeps
  its own attribute state names the attributes that moved, and the bridge
  advances the cluster's DataVersion and marks just those dirty for
  subscribers — independently of the host source's `ChangeNotifier`. It
  lets a server hold a "Q" (quieter) attribute out of change reporting
  until its own rule reports it, as matter.js's `QuietEvent` does.
- **Appliance cluster servers for laundry washers, laundry dryers,
  dishwashers and robotic vacuum cleaners.** Each server holds its
  cluster state the way matter.js's behavior does; the host moves it
  through setters that enforce matter.js's reactors and answers the
  commands the server could not decide itself. Changes are reported per
  attribute through `contract.AttributeChangeNotifier`. Every command,
  response, event and structured attribute crosses the real wire path in
  `bridge` tests and is pinned against matter.js encodings
  (`bridge/testdata/application-wire-fixtures.json`).
  - `cluster/opstate` — OperationalState (0x0060) and RvcOperationalState
    (0x0061): `Server`, `NewServer`, `NewRvcServer`, `Config`,
    `CommandHandler`, `Command*`, `State*`, `Error*`, `StateEntry`,
    `ErrorState`, `OperationCompletion`, the setters
    (`SetOperationalState`, `SetOperationalError`, `SetPhaseList`,
    `SetCurrentPhase`, `SetCountdownTime`), `EmitOperationCompletion`,
    `Revision` / `RvcRevision` and the device-type constants. Pause /
    Resume / GoHome follow matter.js's OperationalStateUtils; the
    "already in that state" answers and the command-state list rule are
    specification text (`BD-Matter-OperationalStateRulesInServer`).
    CountdownTime is reported as matter.js reports a quieter attribute.
  - `cluster/modebase` — `NewLaundryWasherMode`, `NewRvcRunMode`,
    `NewRvcCleanMode`, `NewDishwasherMode` on one `Server`: `Config`,
    `ModeChanger`, `ModeOption`, `ModeTag`, `Status*`, the tag constants,
    `FeatureDirectModeChange`, `SetCurrentMode`. SupportedModes is checked
    as matter.js's mode servers check it, plus the ModeBase tag rules of
    the specification (`BD-Matter-ModeBaseRulesInServer`); a ChangeToMode
    other than UnsupportedMode / the current mode is the device's.
  - `cluster` — `AttributeChanges` (the listener set behind
    `contract.AttributeChangeNotifier`) and `Quieter` (matter.js's
    QuietEvent throttle for a "Q" attribute).
  - `cluster/wire` — the OperationalState and ModeBase wire types
    (`OperationalStateStruct`, `ErrorStateStruct`,
    `OperationalCommandResponse`, `OperationalErrorEvent`,
    `OperationCompletionEvent`, `ElapsedS`, `ModeOptionStruct`,
    `ModeTagStruct`, `ChangeToModeRequest`, `ChangeToModeResponse`) and
    their ids.
  - `contract.DeviceTypeName` names Laundry Washer, Robotic Vacuum
    Cleaner, Dishwasher and Laundry Dryer; `schema` knows the read-only
    attributes of OperationalState and RvcOperationalState.
  - Not built (none is mandatory for the four device types): the
    microwave-oven clusters, OvenCavityOperationalState, ServiceArea,
    LaundryWasherControls, LaundryDryerControls, DishwasherAlarm,
    TemperatureControl.
- **Application cluster servers for smoke / CO alarms, fans, air purifiers,
  extractor hoods, pumps and flow sensors.** Each owns no device state: the
  host reports a snapshot through a port of the server's package and
  receives validated writes; the Matter-side rules — conformance, enum and
  range checks, coupling, status codes, events, FeatureMap / AttributeList /
  AcceptedCommandList / EventList, revisions from the generated schema —
  live in the server. Every new command, write and event crosses the real
  wire path in `bridge` tests, and the Step request and event payloads are
  pinned against matter.js encodings
  (`bridge/testdata/application-wire-fixtures.json`,
  `notes/parity/matter/generate-application-fixtures.ts`).
  - `cluster/alarm` — SmokeCoAlarm (0x005C) for device type 0x0076:
    `Server`, `NewServer`, `Config`, `State`, `StateSource`, `SelfTester`,
    `SensitivitySetter`, `Feature*`, `Optional*`, the enum types,
    `DefaultExpressedStatePriority`, `Server.ExpressedStateOf`,
    `Server.Refresh` (emits the specified events between two snapshots),
    `AlarmSeverityEvent`. SelfTestRequest answers BUSY while alarming or
    testing (`BD-Matter-SmokeCoAlarmRulesInServer`).
  - `cluster/fan` — FanControl (0x0202) for Fan 0x002B, AirPurifier 0x002D
    and ExtractorHood 0x007A: `Server`, `NewServer`, `Config`, `State`,
    `Settings`, `Setting`, `StateSource`, `RockSetter`, `WindSetter`,
    `AirflowDirectionSetter`, `Stepper`, `ErrInvalidInState`, `Feature*`
    and the enum / bitmap types. FanMode, PercentSetting and SpeedSetting
    writes resolve into one coupled `Settings`; Step has a default when the
    host has no `Stepper` (`BD-Matter-FanControlCouplingInServer`).
  - `cluster/pump` — PumpConfigurationAndControl (0x0200) for Pump 0x0303:
    `Server`, `NewServer`, `Config`, `Limits`, `State`, `StateSource`,
    `ControlModeSetter`, `RunningHoursSetter`, `EnergyConsumedSetter`,
    `Server.Emit` for the seventeen alarm events, `Feature*`, `Optional*`
    and the enum types. Pump's mandatory OnOff stays the host's.
  - `cluster/measurement` — `FlowServer` / `NewFlowServer` / `FlowRevision`
    for FlowMeasurement (0x0404); `contract.MeasurementFlow` (FlowSensor
    0x0306) is a new built-in measurement class. It is appended after
    `MeasurementElectrical`, so a host-registered class now starts one
    value higher.
  - `cluster/wire` — `FanControlClusterID`, `FanControlCmdStep`,
    `FanStepRequest` and its field tags, `FieldlessEvent`; `cluster` —
    `AsUintMax`, a narrowing that rejects instead of wrapping.
  - `contract.DeviceTypeName` names Fan, Air Purifier, Extractor Hood, Pump
    and Flow Sensor; `schema` knows the read-only attributes of FanControl,
    PumpConfigurationAndControl and FlowMeasurement.
  - Not built, all optional: HEPA / activated-carbon filter monitoring, and
    the optional measurement / level / scene servers of these device types.
    Open items (unverified connectedhomeip-derived formulas and the
    PowerSource device-type entry SmokeCoAlarm requires) are in
    `notes/parity/matter_behaviour_findings.md`.
- **Groupcast (0x0065) and the AccessControl Auxiliary ACL on the root**, as
  matter.js's default `ServerNode.RootEndpoint` installs them and Matter
  1.6.1 requires them of a node with lights or plugs
  ([ADR 0010](docs/adr/0010-groupcast-and-auxiliary-acl.md)).
  `core.Groupcast` answers JoinGroup (key creation through
  GroupKeyManagement, Administer for a key or UseAuxiliaryAcl, the
  per-fabric and total membership limits, ReplaceEndpoints, IanaAddr /
  PerGroup multicast policy), LeaveGroup (GroupID 0 applies its Endpoints
  to every group of the fabric), UpdateGroupKey, ConfigureAuxiliaryAcl and
  GroupcastTesting (60 s by default; every received group message's outcome
  becomes a GroupcastTesting event meanwhile), with Membership derived from
  the shared group state. It advertises Listener and PerGroup, not Sender:
  this node sends no group message (`BD-Matter-GroupcastNoSender`).
  `NewGroupcast` turns on AccessControl's Auxiliary feature — the
  AuxiliaryAcl attribute, the AuxiliaryAccessUpdated event, and an ACL
  write carrying AuxiliaryType refused with Failure. An IanaAddr group is
  received on FF05::FA. The reference bridge mounts it.
  - `cluster/core`: `Groupcast`, `NewGroupcast`, `GroupcastConfig`,
    `GroupcastClusterID`, `JoinGroupRequest`, `LeaveGroupRequest`,
    `LeaveGroupResponse`, `UpdateGroupKeyRequest`,
    `ConfigureAuxiliaryACLRequest`, `GroupcastTestingRequest`,
    `GroupcastMembershipStruct`, `GroupcastTestingEvent`,
    `AuxiliaryACLSource`, `AccessControlAuxiliaryEntryStruct`,
    `AuxiliaryAccessUpdatedEvent`, `AccessControlAuxiliaryTypeSystem`,
    `AccessControlAuxiliaryTypeGroupcast`, and
    `AccessControlEntryStruct.AuxiliaryType`.
  - `bridge`: `AttachAuxiliaryACL` (nil default: the auxiliary grants are
    not enforced — fails closed); `GroupMessaging.ReportGroupMessage`; a
    root cluster server implementing `contract.ChangeNotifier` now reaches
    subscribers.
  - `endpoint`: `AuxiliaryACLLister`, `TopologyDispatcher.SetAuxiliaryACL`.
  - `im`: `AuthorityAt`, `WithAuthority`, `AuthorityFunc` (the in-command
    access check matter.js calls `session.authorityAt`),
    `HandleGroupInvoke`, `GroupInvokeReport`.
  - `groups`: `GroupProperties`, `GroupcastMembership`,
    `Manager.GroupcastMemberships`, `SetGroupProperties`,
    `RemoveGroupProperties`, `MulticastAddressFor`, `AuxiliaryACL`,
    `OnGroupcastChanged`, `OnGroupMessage`, `ReportGroupMessage`,
    `GroupMessageEvent`, `NoKeyError`, `IANAGroupcastAddress`,
    `PolicyIanaAddr`, `PolicyPerGroup`, `DefaultMcastAddrPolicy`,
    `UnmappedKeySetID`, the `TestResult*` values; `Store` gains the three
    Groupcast-group methods.
  - `store`: the `matter_groupcast_groups` table and `GroupcastGroup`,
    `UpsertGroupcastGroup`, `RemoveGroupcastGroup`, `ListGroupcastGroups`.
- **Package `groups`: the node's operational group state**, the Go
  counterpart of matter.js `packages/protocol/src/groups` and the receive
  half of `GroupSession`. `groups.Manager` holds, per fabric, the key sets
  in derived form, the GroupKeyMap, the group table and the per-sender
  replay windows; `Decode` authenticates a group message (privacy
  deobfuscation, AES-CCM, the encrypted-with-rollover counter window) and
  returns it with its Group subject; `Memberships` names the multicast
  addresses to join. `OperationalKey`, `SessionID`, `PrivacyKey` and
  `MulticastAddress` are the derivations, pinned against matter.js by
  fixtures in `groups/testdata/group-crypto-fixtures.json`.
- **Group messages are received and routed**, as matter.js does it
  (`SessionManager.groupSessionFromPacket`, `GroupSession.decode`,
  `InteractionServer` on a group session, `ServerGroupNetworking`). A group
  message is authenticated through the node's group state, checked against
  the per-key, per-sender counter window, and a group Invoke or a
  SuppressResponse Write runs on every member endpoint of the group where a
  Group access control entry grants the privilege — never answered, never
  acknowledged; anything that fails a check is dropped silently. The bridge
  joins the IPv6 multicast address of every group with a member endpoint
  (`FF35:0040:FD<FabricID>00:<GroupID>`) on all multicast-capable
  interfaces, follows membership changes, retries a failed join every 30 s
  and leaves the addresses on Stop; GroupTable changes reach subscribers,
  and a removed fabric's groups are forgotten.
  - `bridge`: `GroupMessaging`, `AttachGroupMessaging` (noop default: every
    group message dropped; see the package doc).
  - `im`: `GroupSubject`, `WithGroupSubject`, `GroupSubjectFromContext`,
    `HandleGroupInvokeRequest`, `HandleGroupWriteRequest`,
    `AuthorizingInvoker`, `CommandAuthorizer`.
  - `endpoint`: `TopologyDispatcher.InvokeAuthorized`; `CheckACL` evaluates
    a request carrying a Group subject under the Group auth mode.
  - `transport/udp`: `Listener.JoinGroup`, `Listener.LeaveGroup`,
    `ErrNoMulticastInterface`.
- **A real Groups server (0x0004)**, mirroring matter.js `GroupsServer`
  ([ADR 0009](docs/adr/0009-groups-and-group-messaging.md), superseding
  ADR 0004). `core.Groups` answers AddGroup, ViewGroup, GetGroupMembership,
  RemoveGroup, RemoveAllGroups and AddGroupIfIdentifying with matter.js's
  statuses (ConstraintError for GroupId 0 or a name over 16 characters,
  UnsupportedAccess for a group without a GroupKeyMap entry,
  ResourceExhausted beyond MaxGroupsPerFabric, NotFound), advertises the
  GroupNames feature, and keeps membership in the shared `groups.Manager`,
  persisted in the store. `GroupKeyManagement.GroupTable` now serves that
  membership. Group membership is stack state: with
  `endpoint.Config.Groups` set, the assembler mounts the server on every
  bridged endpoint whose device type mandates Groups and replaces a Groups
  server a source supplies itself.
  - `cluster/core`: `Groups`, `NewGroups`, `GroupsClusterID`, the request
    and response types `AddGroupRequest` / `AddGroupResponse`,
    `ViewGroupRequest` / `ViewGroupResponse`, `GetGroupMembershipRequest` /
    `GetGroupMembershipResponse`, `RemoveGroupRequest` /
    `RemoveGroupResponse`, `AddGroupIfIdentifyingRequest`, and
    `GroupKeyMgmtConfig.Groups`.
  - `endpoint`: `Config.Groups`.
  - `schema`: `DeviceTypeMandatoryServerClusters` (generated) and
    `DeviceTypeRequiresServerCluster`.
  - `groups`: `Manager.OnGroupTableChanged`.
- `store`: the `matter_group_table` table and `GroupTableEntry`,
  `UpsertGroupTableEntry`, `RemoveGroupTableEntry`, `ListGroupTable`. A host
  that migrates the schema itself picks the table up from `store.Schema()`.
- `transport/mrp`: `NewWindowEncryptedRollover`, matter.js
  `MessageReceptionStateEncryptedWithRollover`.

- **Subscriptions survive a restart**, as in matter.js
  (`SubscriptionsServer`; [ADR 0008](docs/adr/0008-subscription-resumption.md)).
  Each subscription of a CASE session is recorded while it is active and
  forgotten when it is terminated; after a restart the bridge resolves the
  controller, opens a CASE session to it as the initiator, and re-sends the
  priming report under the old subscription id, so the controller carries on
  instead of waiting out its liveness timeout. On by default.
  - `bridge`: `SubscriptionStore`, `AttachSubscriptionStore`,
    `SetSubscriptionPersistence`, `SubscriptionPersistenceEnabled`,
    `FormerSubscriptionCount`, `ReestablishFormerSubscriptions` /
    `ReestablishResult`, `CaseInitiation`, `CaseInitiatorProvider`,
    `AttachCaseInitiatorProvider`, `OperationalResolver`,
    `AttachOperationalResolver`, and the errors `ErrCaseInitiatorMissing`,
    `ErrOperationalResolverMissing`, `ErrCaseRejected`. Each port defaults to
    a noop; the package doc lists what a skip costs.
  - `im/subscription`: `PeerSubscription`, `MarshalPeerSubscription`,
    `UnmarshalPeerSubscription`, `Manager.Restore`, `Manager.Release`,
    `Manager.SetOnSubscriptionTerminated`, `Subscription.PeerSubscription`,
    `Subscription.SendInterval`, `ReestablishTimeout`, `ErrIDInUse`.
  - `secure/sigma`: a CASE initiator towards a peer on the node's own fabric —
    `NewPeerInitiator` / `InitiatorConfig`, `Initiator.ProcessSigma2Bytes`,
    `Initiator.ProcessSigma2Resume`, `Initiator.Result` / `InitiatorResult`,
    `UnmarshalSigma2`, `UnmarshalSigma2Resume`, `ErrPeerIdentityMismatch`,
    `ErrUnexpectedSigma2Resume`. It exists for re-establishing subscriptions
    only; the module still has no controller role.
  - `secure/operational`: `Manager.OpenFromSigmaAsInitiatorWithID`.
  - `mdns`: `OperationalResolver` / `NewOperationalResolver`,
    `OperationalInstanceQName`, `SelectionPreference`,
    `ErrOperationalNotResolved` — resolution of one peer's operational
    instance, not a browser.
  - `store`: the `matter_server_subscriptions` table and
    `SaveServerSubscription`, `DeleteServerSubscription`,
    `DeleteServerSubscriptionsByFabric`, `LoadServerSubscriptions`,
    `ClearServerSubscriptions`; `*store.Store` satisfies
    `bridge.SubscriptionStore`.
  - `im`: `EventLog.DropBuffered`.
- **Cluster definitions generated from the matter.js model** (ADR 0013).
  `script/clustergen` writes one package per cluster under `cluster/spec/`
  (`pumpconfigurationandcontrol`, `laundrywashermode`, `rvcrunmode`,
  `rvccleanmode`, `dishwashermode`, `hepafiltermonitoring`,
  `activatedcarbonfiltermonitoring`): ids, typed enums and bitmaps, structs
  and command / event payloads with TLV codecs, and a `Definition`. The
  runtime `cluster/spec` derives from a definition plus the host's features
  and optional elements what matter.js derives from its model —
  `spec.New` / `spec.Instance` (attribute, accepted / generated command and
  event lists, FeatureMap, ClusterRevision, privileges, event priorities,
  enum membership), `spec.CheckFeatures`, `Instance.ValidateWrite`,
  `Conformance.Applicability`, the codec helpers, `spec.Register` /
  `spec.Lookup` / `spec.DecodeRequest`. `cluster/spec/spectest` holds the
  parity assertions (`CheckDefinition`, `CheckServer`, the round trips).
  The bridge decodes a generated cluster's request payloads through the
  registry and encodes any `spec.Encodable` value or response.
- **`cluster/filter`: HepaFilterMonitoring (0x0071) and
  ActivatedCarbonFilterMonitoring (0x0072)** for the AirPurifier and
  ExtractorHood device types — `NewHepaFilterMonitoring`,
  `NewActivatedCarbonFilterMonitoring`, `Config`, `State`, `Resetter`,
  `LastChangedTimeWriter`; all three features (Condition, Warning,
  ReplacementProductList), InPlaceIndicator, the writable LastChangedTime and
  ResetCondition. The first server built on a generated definition.
- **Device type validation** ([ADR 0016](./docs/adr/0016-device-type-validation.md)):
  `endpoint.ValidateDeviceTypes` judges every endpoint of a topology
  against the device types its Descriptor lists and the Base device type —
  a port of matter.js's `DeviceTypeConformance`: mandatory and disallowed
  server and client clusters, the feature / attribute / command / event
  requirements nested in a served cluster, component device types,
  Descendant condition counts, singleton placement and unknown stated
  conditions, with matter.js's condition derivation (structural, asserted at
  Root / Self / Descendant, CustomNetworkConfig, network interfaces) and its
  rule that a condition never disallows. Each `endpoint.DeviceTypeViolation`
  names the endpoint, device type, requirement path, kind, cluster, element,
  conformance and the conditions that applied.
  `endpoint.DeviceTypeValidator` (`NewDeviceTypeValidator`, `Validate`,
  `Violations`, `DeviceTypeVerdict`, `DeviceTypeConformanceError`) adds
  matter.js's modes `DeviceTypeValidationWarn` (default),
  `DeviceTypeValidationStrict` and `DeviceTypeValidationOff`. **The bridge
  validates every assembly**: in the default mode it logs each new violation
  once (`matter.devicetype.violation`) and refuses only a root singleton
  misplaced on another endpoint — Start / Reassemble then return the error
  and the previous topology stays live; `Bridge.SetDeviceTypeValidation`
  selects the mode, `Bridge.DeviceTypeViolations` returns what the installed
  topology violates. `Spec.DeviceConditions`, `Config.RootDeviceConditions`
  and `Endpoint.DeviceConditions` let a host state the conditions no
  topology shows (LanguageLocale, PhysicalInputs, …).
  `endpointtest.AssertDeviceTypeConformance` asserts a host's topology in
  its tests, with a list of tolerated violations that fails when stale.
- **Typed device-type requirement lookups** in `schema`, generated from the
  snapshot's new device-type layer: `DeviceTypeDefinitionOf`,
  `BaseDeviceTypes`, `DeviceTypeConditions`, `DeviceTypeConditionScope`,
  `ConditionScopeOf`, `DeviceTypeClusterRequirement`,
  `DeviceTypeElementRequirements`, `ClusterFeatures`, `ClusterFeatureNames`,
  `ClusterClassification`, `ClusterBindable`, with the types
  `DeviceTypeDefinition`, `DeviceTypeRequirement`, `RequirementElement`,
  `ConditionLocation`, `Composition`, `Conformance`, `ConformanceChoice`,
  `CountRange`, `Referent`, `ClusterFeature`. `spec.ConformanceFromSchema`
  and `spec.Conformance.Names` evaluate them.
- **The CHIP data model cross-check covers device-type element requirements
  and conditions**: 153 feature / attribute / command / event requirements,
  46 conditions, 57 condition requirements, the Base device type and every
  cluster's classification are compared with CHIP's XML; none differs (the
  Base device type's CHIP name is normalized). The "Not compared" rows for
  them are gone.

### Changed

- IcdManagement and the OTA Software Update Requestor stay unmounted in the
  reference daemon, and the documentation no longer says the former is
  "mounted for conformance": RootNode requires IcdManagement only for an
  intermittently connected node ("Sit | Lit"), matter.js mounts it on no
  node that is not an ICD, and a controller reads its presence as "this
  node sleeps"; the requestor cannot take an update without BDX and an
  update agent. Their families are not applicable through the device's
  own PICS.

- `closure.ControlServer` MoveTo and Stop follow the specification text
  matter.js carries (closure-control.resource.ts): a MoveTo with none of its
  fields is INVALID_COMMAND; a field of a feature the server does not
  advertise (Latch, Speed) is ignored instead of refused with
  ConstraintError; with Positioning an absent Position falls back to
  OverallTargetState.Position, and with that null nothing moves. Stop acts
  only on a closure that is Moving, WaitingForMotion or Calibrating and
  answers SUCCESS in every state; before, it set MainState Stopped from any
  state, SetupRequired included.

- `light.ColorControlServer` serves MoveColorTemperature,
  StepColorTemperature and StopMoveStep as matter.js does instead of
  accepting and ignoring them — at once without `ManageTransitions`, as a
  transition with it; MoveColorTemperature with MoveMode Stop leaves a
  colour temperature move running, as in matter.js. It reports
  ColorTemperatureMireds and RemainingTime through
  `OnMatterAttributesChanged` by the Q rules, and `MatterReportable` is
  empty, so an endpoint source's change notification no longer reports the
  colour temperature.
- The reference daemon's ceiling light runs its LevelControl (now
  `Config.Lighting`, replacing the daemon's own LT wrapper) and its
  ColorControl on the transition engine; the speaker keeps the hand-off
  path. The bridge decodes MoveToColorTemperature's OptionsMask and
  OptionsOverride.

- **A bridged endpoint's VendorName comes from the host.** The module
  served `"eQ-3"` as the BridgedDeviceBasicInformation VendorName of every
  bridged endpoint — one host's manufacturer inside the module. It now
  serves `endpoint.Spec.VendorName`, and when the host leaves that empty the
  node's own VendorName, `endpoint.Config.VendorName` (both new; also
  `Topology.VendorName`, `Endpoint.VendorName` and
  `Endpoint.BridgeVendorName`). A host that set neither now serves no
  VendorName on its bridged endpoints, matter.js's default for a bridged
  device; set `Config.VendorName` (or a per-device `Spec.VendorName`) to
  keep the four vendor fields Apple Home relies on filled
  (`BD-Matter-BridgedVendorNameFallback`).

- **A fabric AddNOC installs starts with an empty Label**, as matter.js's
  `FabricBuilder` starts one and as TC-OPCREDS-3.7 reads it right after
  commissioning; it was `"go-fabric"`. A host that wants a label from the
  first read sets `core.OpcredsConfig.InitialFabricLabel` (at most 32
  bytes). Retires `L3-PFAD-1` in `notes/parity/by_design.md`.

- **Certifiability is a goal; certification is still not pursued**
  ([ADR 0011](docs/adr/0011-certifiability-is-a-goal.md)). The scope
  statement "no CSA certification" no longer reads as "certification
  conformance does not matter here": a product built on the module should
  be able to pass certification. The chip-tool suite became the measure:
  - `internal/chiptool` runs whole CHIP certification families — the core
    families and the application family of every server the reference
    daemon mounts, YAML and Python `TC_*` cases — in matter.js's CHIP image
    (`ghcr.io/matter-js/chip`, pinned by digest with the CHIP commit it was
    built from), declared the way matter.js declares its own. Every case
    not run is a gap of one class — (a) defect, (b) not supported,
    (c) harness, (d) out of scope — with its reason, and the steps a
    passing case executed are pinned.
  - The PICS are generated from the commissioned daemon per endpoint
    (`testdata/gen_pics.py`, CHIP's own derivation helpers) and checked
    with TC-IDM-10.4 per endpoint; only what a device cannot report is
    declared by hand (`testdata/reference-bridge.pics`).
  - A matter.js controller leg commissions the daemon and checks read,
    write, invoke, reporting, a second fabric and subscription resumption
    across a restart.
  - [`docs/certifiability.md`](docs/certifiability.md) is the status page,
    generated from the family table and the last run and held to them by
    `TestCertifiabilityDocument` in every `go test ./...`; README,
    `docs/feature-scope.md`, `docs/matterjs-comparison.md` and CLAUDE.md are
    reworded accordingly.
  - Make targets: `chiptool-test` (the quick suite), `chiptool-families`
    (every family, hours); `chiptool-setup` checks out
    `../connectedhomeip` at the image's commit. The CI workflow runs the
    families in four groups next to the chip-tool control leg.

- `examples/reference-bridge` exposes one simulated device per surface the
  module serves (colour-temperature light, fan, smoke/CO alarm, pump, flow
  sensor, laundry washer, robot vacuum, thermostat, blind, door lock,
  humidity / occupancy / contact sensors, wall button), takes CHIP-style
  test control through `--app-pipe` and `--enable-key` (off by default),
  wires AdministratorCommissioning and the enhanced (multi-admin)
  commissioning window, serves the test Certification Declaration — a
  commissioner that verifies attestation no longer fails the pairing — and
  emits StartUp / BootReason / ShutDown / Leave. It prints its endpoint
  topology after the banner.

- **Matter 1.6.1.** `parity/schema.json` is re-extracted from matter.js
  `85cf6647` (Matter 1.6.1; previously `f07365a8`, 1.6.0) and `schema/` is
  regenerated from it. This is a matter.js parity correction and bypasses the
  deprecation window for the values below:
  - `cluster.SpecificationVersion` is `0x01060100` (was `0x01050100`), and
    `core.BasicInformation` defaults `DataModelRevision` to 21 (was 19) —
    matter.js `Specification.SPECIFICATION_VERSION` / `DATA_MODEL_REVISION`.
  - `im.MatterInteractionModelRevision` and `im.InteractionModelRevision` are
    12 (were 13), the revision matter.js stamps on every IM message
    (`Specification.INTERACTION_MODEL_REVISION`, capped below 13 because
    revision 13's only delta is provisional). Every IM response's tag `0xFF`
    changes byte; the IM wire fixtures were regenerated with the generator
    reading the value from matter.js instead of a literal.
  - GroupKeyManagement advertises ClusterRevision 4 (was 3). Its GroupKeyMap
    carries quality `C` (changes omitted), so `MatterReportable` no longer
    lists it.
  - Advertised device-type revisions follow the snapshot through
    `schema.DeviceTypeRevision`: RootNode 5, OnOffLight 4, DimmableLight 4,
    OnOffPlugInUnit 5, DimmablePlugInUnit 6, ColorTemperatureLight 5,
    ExtendedColorLight 5, WindowCovering 7, Thermostat 7, SmokeCoAlarm 2,
    among others.
  - A write to the deprecated Thermostat attributes PiCoolingDemand,
    PiHeatingDemand and Occupied/UnoccupiedSetbackMin/Max is answered
    UNSUPPORTED_WRITE: the 1.6.1 snapshot gives them access `R V`.

  Matter 1.6.1 also requires a Groupcast server and the AccessControl
  Auxiliary ACL on the root of a node with lights or plugs — see the
  Groupcast entry above.

- `Bridge.Stop` drops the buffered events (the numbering continues), as a
  process restart does: the priming report of a subscription re-established
  after an in-process Stop/Start no longer replays the former run's events
  (matter.js #4594).
- A `KeepSubscriptions=false` subscribe over CASE now cancels the peer's
  subscriptions on all of its sessions, matching on the session's peer node —
  matter.js matches `session.peerAddress`. It used to fall back to the
  request's own session because a secure header carries no source node id.
- `subscription.Manager.CloseSession` and `CloseFabricExcept` close
  subscriptions without terminating them; `Close`, `ClosePeer`,
  `CloseEndpoint`, `CloseFabric` and a replace-on-resubscribe terminate them.
  Only the latter fire the new terminated hook.
- `sigma.Initiator` is safe for concurrent use and refuses further input after
  a failed Sigma2.
- **The matter.js extract carries the resolved model** (`parity/schema.json`,
  same matter.js commit `85cf664`, Matter 1.6.1). Every attribute, command,
  event and feature gains an `effective` object read through matter.js's
  operational model — type, metatype and primitive, conformance as matter.js's
  AST, access, quality, constraint, default, command and event fields, the
  response linkage, event priority, feature titles — with what a derived
  cluster inherits from its base resolved; each cluster gains its `datatypes`
  (enums with their values and conformance, bitmaps with their bits, structs
  with their fields) and `base`, the snapshot a `globalDatatypes` list. Every
  key the extract carried before is byte-identical in meaning and position.
  `schema.SchemaSnapshotSHA256` changes: **a host pinning the snapshot's bytes
  must re-pin.** `make generate-matter-schema` also runs on a node without
  TypeScript support, transpiling the extractor with the matter.js checkout's
  esbuild.
- `cluster/pump` and `cluster/modebase` are built on their generated
  definitions; their API is unchanged. `pump.Feature`, `OperationMode`,
  `ControlMode` and `Status` are now aliases of the generated types (same
  values, same underlying types), and the servers' lists, globals, write
  checks and privileges come from the definition.
- **The matter.js extract carries device-type element requirements and
  conditions** (`parity/schema.json`, same matter.js commit `85cf664`).
  Each device type gains an `effective` object — its composition
  (`tree` / `full-family`), the device type it derives from, the 46
  conditions the device types declare, and its full requirement tree as
  matter.js's device type validation reads it: server and client clusters
  with their nested feature / attribute / command / event requirements
  (conformance as matter.js's AST, constraint, quality — the singleton
  `I` — access, default), component device types with their instance
  numbers and the clusters they require, and the condition requirements
  with their location; every requirement names what matter.js resolves it
  to. The snapshot gains `baseDeviceTypes` (the Base device type), and
  each cluster its `classification` and, where a binding never directs
  it, `bindable: false`. Every key the extract carried before is
  byte-identical in content and position. `schema.SchemaSnapshotSHA256`
  changes: **a host pinning the snapshot's bytes must re-pin.**

### Fixed

- The reference daemon's robot vacuum emits OperationCompletion when a
  cleaning run ends; the RoboticVacuumCleaner device type mandates the event
  and the RvcOperationalState server already listed it.

- A reliable message is registered for retransmission before it is sent,
  not after: an acknowledgement the peer sent at once could be handled
  before the message was registered, was lost, and the message went out
  again as a spurious retransmission — in a chunked read that ended with a
  chunk carrying a stale piggybacked acknowledgement, which chip-tool drops,
  and a stalled read (TC-BINFO-2.1, the PICS generation's wildcard read,
  intermittently).

- **A session's messages are handled in the order they arrive.** The UDP
  listener gave every datagram its own goroutine, so two messages of one
  session could run in either order: a Read sent right behind an Invoke
  reported state from before the command (TC-IDM-1.2 step 7,
  intermittently), and a report chunk could go out before the
  StatusResponse acking its predecessor had been processed, carrying a stale
  piggybacked acknowledgement that chip-tool drops — the chunked read then
  stalled until the client timed out (TC-BINFO-2.1 and the PICS
  generation's wildcard read, intermittently). The bridge now serves with
  `udp.Listener.ServeOrdered`: the datagrams of one unicast session are
  queued in arrival order and handled one at a time; a handler that waits
  for the peer's StatusResponse hands its turn on first. matter.js and chip
  handle a session's messages in order on their event loops.

- A write to a read-only ClosureControl or TimeSynchronization attribute
  answers UNSUPPORTED_WRITE instead of FAILURE (TC-ACE-2.2's write-access
  checker accepts nothing else).

- The Auxiliary access-control rule (a Group entry without targets does not
  reach endpoint 0) follows the root AccessControl's advertised FeatureMap,
  not only `Bridge.AttachAuxiliaryACL`: a host that mounted Groupcast —
  which advertises the feature — without attaching the lister let such an
  entry grant endpoint 0. It now fails closed, as matter.js
  `AccessControlServer.#applyFabricAcl` derives the rule from the cluster's
  own feature.

- A group invoke whose command fields do not decode is reported per member
  endpoint the group may reach — GroupcastTesting Success with
  AccessAllowed true and the endpoint, the command failing there with the
  decode status — instead of as one access-denied outcome without an
  endpoint. Access is checked before the fields are decoded, as matter.js
  `CommandInvokeResponse` does (#4526, Core §11.27.7.6.3); the command
  still runs nowhere.

- ArmFailSafe over CASE answers BusyWithOtherAdmin while an administrator's
  commissioning window is open and the fail-safe is not armed: the bridge
  now hands the window's state to GeneralCommissioning
  (`CommissioningWindow.IsOpen`), whose check was never wired outside its
  unit tests (matter.js `GeneralCommissioningServer.#armFailSafe`, #4602).

- **SuppressResponse suppresses error statuses too.** A Write or Invoke
  request with SuppressResponse set that failed before dispatch — a timed
  mismatch or timeout, a chunked or batch InvalidAction, a request that does
  not decode — was still answered with a StatusResponse. Such a request now
  ends silently (only the MRP acknowledgement goes out), as matter.js
  (`InteractionServerMessenger`, #4570) and the CHIP SDK 1.6.1 do; for a
  request that does not decode, SuppressResponse is read on its own
  (`im.SuppressResponseOf`). Read, Subscribe and Timed requests are
  unaffected.
- An InvokeRequest's DelayReportData (tag 3) is decoded and validated
  instead of skipped: a field of the wrong type fails the request with
  InvalidAction, one above uint16 with ConstraintError
  (`im.InvokeRequest.DelayReportData`, `im.DelayReportData`; matter.js
  `TlvInvokeRequest`, #4569). Report deferral itself stays off, as it is in
  matter.js behind its `delay-report-data` forward feature
  (`BD-Matter-DelayReportData`).

- GeneralDiagnostics UpTime counts the time the host was suspended and is
  never lowered by a backward clock step: the larger of the monotonic and
  the wall-clock elapsed time, held to the highest value reported (matter.js
  `GeneralDiagnosticsServer` upTime, #4614). Go's monotonic clock, like
  CLOCK_MONOTONIC, stops during a suspend.

- **mDNS: the node answers address queries for its own host name.** A
  query for the A / AAAA (or ANY) records of the SRV target
  (`<MAC>0000.local.`) went unanswered: grandcat/zeroconf's
  `Server.handleQuestion` answers only service-type, service and instance
  questions and sends the host's addresses solely as additionals of an SRV
  answer, so a resolver that follows the SRV target with its own address
  query found nothing (TC-SC-4.1 / TC-SC-4.3: "No AAAA addresses were
  resolved for hostname"). The `SubtypeResponder` side-car now answers
  them (`SubtypeResponder.SetHost`, set by `Zeroconf.Publish`), from the
  addresses the advertise policy publishes for the interface the query
  arrived on — none, and no answer, on an excluded interface — with the
  120 s TTL and the cache-flush bit, known-answer suppression, the
  unicast-response rule, the 900 ms multicast rate limit and RFC 6762 §7.3
  duplicate-question suppression, as matter.js `MdnsServer` does
  (`#registerResponderNames`, `#handleMessage`, `sendable`). The OS host
  name (`Zeroconf.HostName`, macOS) stays the OS responder's.
- **Group messages are received one at a time.** Each datagram runs on its
  own goroutine, so a controller that sends a group message on several
  interfaces (chip's multicast homing) had both copies decoded at once; the
  copy that lost the replay check reported MessageReplay before the other
  copy's Success, and the GroupcastTesting event queue came out reordered
  (TC-ACE-1.6, intermittently on a host with two IPv6 interfaces). The
  bridge now finishes one group datagram — decode, replay check, dispatch,
  outcome event — before the next, as matter.js and chip do on their single
  loops. The second copy is still a replay and still reported as one.

- **BooleanState lists its StateChange event** (`measurement.BooleanStateServer.MatterEvents`,
  `measurement.BooleanStateEventStateChange`). The server advertises CHGEVENT,
  which makes the event mandatory, and the bridge emits it on every change,
  but the server did not list it — so a Water Leak Detector, Water Freeze
  Detector or Rain Sensor built on it lacked the event its device type
  requires. Found by the new device type validation. `cluster/light` no
  longer claims ExtendedColorLight, whose XY feature its ColorControl server
  does not serve.
- **The schema snapshot carries inherited features.**
  `script/extract-from-matter-js.ts` read a cluster's features from its own
  FeatureMap, which in a derived cluster replaces the base's, so a feature
  inherited without being restated was lost: TemperatureAlarm (0x0064)
  lacked Alarm Base's RESET (bit 0, O). The extractor now emits them from
  matter.js's operational model; re-extracted at the same matter.js pin,
  that feature is the only change. The CHIP data model cross-check found it.
  `schema.SchemaSnapshotSHA256` changes: **a host pinning the snapshot's
  bytes must re-pin.** No server or generated cluster definition is
  affected.
- **Certification:** TC-LVL-2.3, TC-LVL-3.1, TC-LVL-4.1, TC-LVL-5.1,
  TC-LVL-6.1, TC-CC-2.2, TC-CC-6.2 and TC-CC-6.3 run and pass — they were
  excluded as class (a) gaps because no level or colour temperature was
  ever in transition (RemainingTime always 0, nothing readable part-way).

- **Found by the CHIP Python certification harness** (`internal/chiptool`,
  run in matter.js's CHIP image against the reference daemon):
  - An attribute with matter.js's "C" (changesOmitted) quality — GroupKeyMap,
    Nocs, TrustedRootCertificates, the diagnostics counters and the rest —
    is no longer reported to subscribers when it changes, however the
    change was made; it still reads and appears in priming reports.
    A write of GroupKeyMap was reported back (TC-ACE-1.6 counts none). New:
    `schema.AttributeChangesOmitted`, generated from the quality strings
    in `parity/schema.json`.
  - `mdns.Zeroconf` publishes an interface's link-local IPv6 address when
    the interface has no routable IPv6, as chip and matter.js publish every
    IPv6 address. On a host whose LAN interface is link-local-only for
    IPv6 (GitHub's runners) the operational host name carried IPv4 records
    only, and an IPv6-only resolver such as chip-tool never found the
    commissioned node. Where a routable IPv6 exists, the link-local one
    stays out as before.
  - `mdns.Zeroconf` answers only on the interfaces its address records come
    from. Answering on every interface sent the records over container
    bridges too, and a local IPv6 resolver scoped the link-local address
    to the bridge, an unreachable route (TC-BINFO-2.2 in CI).
  - `cluster/thermo.ThermostatServer` accepts writes of
    Min/MaxHeatSetpointLimit and Min/MaxCoolSetpointLimit (ConstraintError
    outside the absolute range) and reconciles the setpoints and limits a
    write leaves inconsistent — including the AutoMode deadband between
    heating and cooling — as matter.js's `#reconcileSetpoints` (chip
    `FixUserLimits` / `FixUserLimitDeadband` / `FixRange`) does; the
    values it moves besides the written one are reported through
    `contract.AttributeChangeNotifier` (TC-TSTAT-2.2). Writes of
    ControlSequenceOfOperation and MinSetpointDeadBand are accepted and
    ignored, as the specification ("optionally writeable … silently
    ignored") and matter.js have it, instead of being refused.
  - `cluster/lock.DoorLockServer` serves OperatingMode ("RW VM") as
    writable state, as matter.js does: a supported mode (Normal,
    NoRemoteLockUnlock) is stored, anything else is a ConstraintError. The
    reference daemon's lock builds its server once, so the written mode
    survives to the next request (TC-DRLK-2.1).
  - `cluster/cover.Config.MoveStep` lets the WindowCovering server's lift
    travel — six steps, OperationalStatus Opening or Closing until it
    arrives, every step reported through `contract.AttributeChangeNotifier`,
    StopMotion halting it where it is — as matter.js's CHIP test node
    moves it; zero keeps the instant movement. The reference daemon's
    blind travels with 950 ms steps (TC-WNCV-3.1 to 3.3).
  - The reference daemon's blind keeps its lift position across a restart,
    as matter.js keeps WindowCovering state non-volatile (TC-WNCV-4.5).
  - `mdns.Zeroconf.HostName` replaces the SRV target of every published
    record; the reference daemon sets it to the OS host name with
    `--mdns-os-hostname` (testing only), which the chip-tool harness passes
    on a host whose LAN interface has no IPv6 — there the MAC-derived name
    carries only IPv4 records, and the image's chip-tool resolves
    operational nodes over IPv6 only.
  - The reference daemon's smoke/CO alarm builds its SmokeCoAlarm server
    once, so the alarm events go out through the instance the bridge wired
    its emitter into (TC-SMOKECO-2.2 to 2.5), and reports a future
    ExpiryDate (TC-SMOKECO-2.1); its valve closes itself when a timed
    opening ends and reports it (TC-VALCC-4.5).
  - The reference daemon's washer counts its CountdownTime down while a
    cycle runs (TC-OPSTATE-2.2), and its robot vacuum behaves as matter.js's
    RVC test node: it starts Stopped, its run and clean modes refuse with
    InvalidInMode where that node does, Resume is refused on the dock and
    GoHome in Error, and the rvc-app pipe's Reset, ChargerFound, Charging and
    Charged are understood (TC-RVCOPSTATE-*, TC-RVCRUNM-*, TC-RVCCLEANM-*).
  - The reference daemon's light reports what its OnWithTimedOff countdowns
    change on their own: OnTime and OffWaitTime as they run down, and OnOff
    when the timed-on phase switches the light off. Before, a subscriber
    learned of none of it (TC-OO-2.8).
  - The reference daemon's ceiling light no longer hangs on an OnOff
    command: switching it fired the device's change notification while the
    command still held the light's state, and the bridge's read-back of the
    changed paths waited on it forever (TC-CC-*, TC-LVL-*).
  - The reference daemon's on/off light hands the bridge the same cluster
    servers on every dispatch, so a written OnTime or OffWaitTime and a
    running countdown survive to the next request (TC-OO-2.1 to 2.3).
  - The reference daemon's valve travels: Open and Close set TargetState and
    report CurrentState Transitioning, and on arrival TargetState returns to
    null — the pair of reports TC-VALCC-3.1 waits for; OpenDuration and
    RemainingDuration hold from the Open command on (TC-VALCC-4.1, 4.2,
    4.5); a command for the position the valve already holds moves nothing.
  - The reference daemon's washer refuses Start and Resume with
    UnableToStartOrResume after the app pipe reported a fault, and a
    cleared fault resumes the cycle, as matter.js's test node does
    (TC-OPSTATE-2.2).
  - The reference daemon's smoke/CO alarm ends a self-test on its own after
    five seconds (SelfTestComplete, then AllClear), and its app pipe takes
    CHIP's smoke-co-alarm-app `LongPress` (start a self-test) and
    `SetUnmounted` (TC-SMOKECO-2.4, TC-SMOKECO-2.7). The test button starts
    no self-test while the alarm sounds, the rule SelfTestRequest answers
    with BUSY (TC-SMOKECO-2.2, 2.3), an unmounted alarm expresses
    Inoperative, and a critical alarm cannot be muted (TC-SMOKECO-2.5).
  - The reference daemon persists GeneralDiagnostics TotalOperationalHours
    (every minute and on shutdown) and RebootCount — 0 on a
    database's first boot, one more on each boot after, as matter.js counts
    it — where it reported a fixed placeholder — and accepts the generic
    test event trigger 0x3 the DGGEN cases send (TC-DGGEN-2.1).
  - The reference daemon's washer counts a 30-second cycle down instead of
    a 30-minute one, and the Stop that ends a cycle emits
    OperationCompletion with the seconds it lasted, pauses included, and
    the seconds it was paused
    (TC-OPSTATE-2.5).
  - A change notification of a host source marked every reportable
    attribute of its endpoint dirty; the bridge now reports only the
    attributes whose value moved, and advances only their clusters'
    DataVersions, as matter.js's Datasource broadcasts the changed
    properties (TC-FAN-3.2 counts the FanMode reports).
  - `cluster/light.ColorControlServer` serves Options ("RW VO") as a
    writable bitmap (ExecuteIfOff), as matter.js does (TC-CC-6.5); the
    read-only divergence `BD-Matter-P1-D8` is retired.
  - The reference daemon's CASE identity table resolves a resumed
    session by the fabric its resumption record names
    (`sigma.FabricIndexResolver`); a controller resuming on its fabric after
    another fabric was installed got a session on the newest fabric and was
    refused everything (TC-ACL-2.10).
  - SetRegulatoryConfig accepted a configuration the LocationCapability
    rules out; an Indoor-only or Outdoor-only node now answers
    ValueOutsideRange for anything else, as matter.js does (TC-CGEN-2.4).
  - A rolled-back UpdateNOC now also drops a VID verification statement,
    VVSC or vendor id set under it, and OperationalCredentials reports
    changes made outside its own commands — a fail-safe expiry or disarm
    rolling back AddNOC or UpdateNOC — to subscribers
    (`contract.ChangeNotifier`; TC-OPCREDS-3.8). The reference daemon drops
    a removed or rolled-back fabric's CASE identity and operational record,
    so a recommissioned fabric does not land its sessions on the stale
    index (TC-CGEN-2.4).
  - NetworkCommissioning reported LastNetworkingStatus and LastNetworkID
    as null on a node that is on its Ethernet network; they are now Success
    and the interface's network id, as TC-CNET-4.3 reads them and as
    matter.js configures its bridge test node.
  - An UpdateNOC whose fail-safe expired or was disarmed without
    CommissioningComplete stayed in force. The fail-safe's expiry now
    restores the replaced NOC, key and node id and runs the
    fabric-updated hook, as matter.js's FailsafeContext rollback does
    (TC-OPCREDS-3.5). The reference daemon follows an UpdateNOC — and its
    rollback — with the CASE identity, the operational record under the
    new instance name and the fabric's other sessions (TC-OPCREDS-3.8).
  - RevokeCommissioning only disarmed the fail-safe, so a fabric an
    aborted commissioning had added over PASE survived the revoke and the
    next commissioning of the same fabric failed FabricConflict. The revoke
    now expires the fail-safe with its timeout cleanup
    (`core.GeneralCommissioning.ExpireFailSafe`, used by
    `bridge.CommissioningWindow.RevokeWindow`), as matter.js's
    `failsafeContext.close()` does (TC-CGEN-2.4).
  - A list-append ACL write reported every entry before the appended one
    as Changed; it now reports only the appended entry as Added, as chip's
    list append does (`im.WithListAppendWrite`, `im.IsListAppendWrite`;
    TC-ACL-2.6; `BD-Matter-ACLAppendEvents`).
  - Arming the fail-safe from the disarmed state inside the stack — the
    PASE auto-arm, the window-open arm — did not start a new fail-safe
    context, so a trusted root an aborted attempt left pending made the
    next attempt's AddTrustedRootCertificate fail (TC-CGEN-2.4).
  - SetVIDVerificationStatement and SignVIDVerificationRequest, mandatory
    OperationalCredentials commands, answered InvalidCommand. They are now
    served as matter.js serves them: the statement and VVSC are stored per
    fabric (through a store with `GetSetting`/`SetSetting`, in memory
    otherwise) and reported in Fabrics and NOCs, a VendorID updates the
    fabric (`store.Store.UpdateFabricVendorID`), and the signature covers
    `VendorIdVerification.dataToSign` with the invoking session's
    attestation challenge (`core.WithInvokeAttestationChallenge`,
    `operational.Manager.AttestationChallengeFor`). AttestationRequest and
    CSRRequest sign with the invoking session's challenge too
    (`SetVidVerificationStatementRequest` gains `HasVendorID`,
    `HasVidVerificationStatement`, `HasVvsc`; TC-OPCREDS-3.8, TC-RR-1.1).
  - A second AddNOC — or an UpdateNOC — after AddNOC in the same
    fail-safe context was processed instead of failing with ConstraintError,
    as matter.js's `addNoc` / `updateNoc` do (TC-OPCREDS-3.1).
  - A PASE session an AddNOC succeeded on kept no accessing fabric, so a
    fabric-scoped command sent on it afterwards — CommissioningComplete
    over PASE — was refused UnsupportedAccess instead of being answered
    InvalidAuthentication. The bridge now moves the session onto the
    installed fabric (through a session registry with `AdoptFabricIndex`,
    as `secure/operational.Manager` has), as matter.js sets
    `session.fabric` and chip calls `AdoptFabricIndex`; GeneralCommissioning
    rejects CommissioningComplete on any PASE session (TC-CGEN-2.4).
  - ArmFailSafe while the fail-safe was already armed reset the pending
    credentials, dropping a trusted root added under it. A re-arm now only
    extends the fail-safe in force; the armed hook runs for an arm from the
    disarmed state, as matter.js's `failsafeContext.extend` (TC-CGEN-2.2).
  - Removing the fabric that opened a commissioning window left
    AdminFabricIndex naming it. `bridge.CommissioningWindow.FabricRemoved`
    (run by `EmitFabricRemoved`) now clears it and reports the change, as
    matter.js's AdministratorCommissioningServer does (TC-CADMIN-1.25).
  - A controller that removed its own fabric kept being answered on the
    removed fabric's sessions. `bridge.Bridge.EmitFabricRemovedContext`
    (new; call it from `OperationalCredentials.SetOnFabricRemoved` with the
    command's context) also closes every session of the removed fabric once
    the NOCResponse is out, as matter.js's `Fabric.remove` does
    (TC-CADMIN-1.15). `EmitFabricRemoved` is unchanged.
  - A RevokeCommissioning sent over the PASE session it revokes closed that
    session before answering, so the command timed out at the controller.
    The close now waits for the response, as matter.js defers a session
    close until its exchanges end (`im.AfterResponse`,
    `im.WithAfterResponse`, `im.DeferAfterResponse`; TC-CADMIN-1.10).
  - A commissioner that aborted a PASE handshake with a failure
    StatusReport — chip's answer to a wrong passcode — left the handshake
    holding the single-active-PASE slot for its one-minute timeout; every
    retry in that minute was dropped as busy and the failure was not
    counted. The report now ends the handshake and counts toward the
    20-failure window revocation, as matter.js's PaseServer does
    (TC-CADMIN-1.9).
  - A chunked ACL write whose REPLACE-ALL drops the writer's own Administer
    entry lost every following append to UnsupportedAccess. The elements of
    one Write interaction that continue writing the attribute just written
    successfully are no longer re-authorized, as chip's WriteHandler does —
    across the messages of a chunked write too (`im.WriteTransaction`,
    `im.WithWriteTransaction`; TC-ACL-2.6, 2.8;
    `BD-Matter-ChunkedWriteAuthorizedOnce`).
  - A list attribute too large for one message — the NOCs of three
    fabrics, a long ACL or PartsList — went out as a single oversized
    ReportData that a chip controller cannot authenticate and discards,
    stalling the read or subscription. Such a list is now split into a
    REPLACE-ALL with the leading members and ListIndex=null appends, as
    matter.js's `chunkAttributePayload` and chip's report engine do
    (`tlv.SplitArrayMembers`, `tlv.Encoder.PutRawElement`; TC-S-2.6).
  - One subscription whose peer was slow to answer a report chunk held up
    every other subscription's reports until it timed out. Each
    subscription now reports on its own, and a subscription whose report is
    still on the wire is not re-entered — its changes wait for the next
    one — as matter.js's ServerSubscription does (TC-S-2.6).
  - A command a cluster changed state with was never reported to
    subscribers unless the server fired a change notification of its own,
    and none of the root servers does: Breadcrumb after ArmFailSafe,
    Fabrics after AddNOC. The bridge now compares the invoked cluster's
    attributes before and after the command and reports what moved, as
    matter.js's Datasource does (TC-IDM-1.5).
  - A commissioning window that opened, timed out, was revoked or ended with
    a commissioning never reported WindowStatus, AdminFabricIndex and
    AdminVendorId to subscribers; the bridge now marks them changed on every
    window transition (TC-CADMIN-1.3).
  - OpenCommissioningWindow answered the IM-level BUSY where matter.js and
    the spec give FAILURE with the cluster-specific status Busy (0x02)
    (TC-CADMIN-1.5).
  - A list-append write (null ListIndex) — how a controller writes a list too
    large for one message, a replace followed by appends — was not
    understood: the path lost the null, the element failed to decode as a
    list, and the whole WriteRequest went unanswered. Appends now add the
    element to the list as the writer sees it (`im.ConcreteAttributePath.ListAppend`),
    as matter.js AttributeWriteResponse does (TC-ACL-2.3, 2.5).
  - A Read, Write, Invoke, Subscribe or Timed request that does not decode is
    answered with a StatusResponse (the error's status, FAILURE otherwise)
    instead of being left to time out, as matter.js's InteractionMessenger
    does.
  - Writing an ACL or Extension list identical to the stored one emitted
    change events; matter.js reports only an actual change, and the empty
    replace that opens a chunked list write now reports nothing (TC-ACL-2.5).
  - A concrete event path the subject may not read was left out of the
    report; it is answered with UNSUPPORTED_ACCESS, a wildcard still skips
    it silently, as matter.js EventReadResponse does (TC-ACL-2.9).
  - The DNS-SD SRV target was the OS host name, which fails the Matter
    host-name rule (12 or 16 uppercase hexadecimal characters from the MAC
    address). The default host name is now the first multicast interface's
    MAC plus "0000", as matter.js MdnsAdvertisement.ts names its host; on
    macOS the OS host name is kept, where a separately published name lost
    its address records (TC-SC-4.3).
  - BooleanState served FeatureMap 0 and never emitted StateChange. The
    ChangeEvent feature is on, and the bridge emits StateChange whenever a
    host's notification changes StateValue, as matter.js BooleanStateServer
    does by default (ported from matter.js BooleanStateServerTest).
  - The CASE initiator that re-establishes former subscriptions after a
    restart never acknowledged the responder's final StatusReport, so the
    controller retransmitted it until its MRP budget ran out. It is
    acknowledged when the handshake ends, as matter.js's MessageExchange
    does on destroy. Found by the matter.js controller leg of the chip-tool
    suite, which also confirms the resumption itself: after a daemon
    restart matter.js's controller receives changes on its subscription
    without subscribing again.
  - AccessControlEntryChanged was one event per ACL write with no
    LatestValue and no actor, and the entry AddNOC installs emitted none.
    The events now follow matter.js AccessControlServer.ts: one per entry
    position (Added / Changed with the new entry, Removed with the old one),
    AdminNodeID for a CASE actor and AdminPasscodeID 0 for PASE, and the
    AddNOC entry reported as Added by passcode 0
    (`OperationalCredentials.SetOnAdminEntryInstalled`, wired by the bridge)
    (TC-ACL-2.5, 2.6, 2.9). AccessControlExtensionChanged names the actor
    too.
  - An AccessControl Extension whose list member carries no tag was accepted;
    matter.js decodes the extension as a tagged list and rejects it with
    CONSTRAINT_ERROR (TC-ACL-2.3).
  - The commissioning window stayed open after a successful
    CommissioningComplete through it, so the next OpenCommissioningWindow was
    answered BUSY until the window timed out. GeneralCommissioning now ends
    it (`SetOnCommissioned`, wired by the bridge to its CommissioningWindow,
    `CommissioningWindow.EndCommissioning`), as matter.js's DeviceCommissioner
    does on `commissioned` (TC-CADMIN-1.3, TC-ACL-2.8).
  - BasicInformation.ConfigurationVersion, mandatory from cluster revision
    6, was not served on the root, and the bridged endpoints' was a constant
    1. Both now start at 1 as in matter.js and can be raised:
    `BasicInformation.IncreaseConfigurationVersion` /
    `RestoreConfigurationVersion`, `endpoint.Endpoint.IncreaseConfigurationVersion`,
    `endpoint.Spec.ConfigurationVersion` for the persisted value, and
    `bridge.Bridge.IncreaseConfigurationVersion(scope, address)`, which raises
    the device's endpoints and the node's version and reports both to
    subscribers, as matter.js's increaseConfigurationVersion does
    (TC-BINFO-3.2, TC-BRBINFO-3.2).
  - The AccessControl Extension attribute lived in memory only and was gone
    after a restart. It is persisted through the store's settings when the
    store offers them (`core.ACLExtensionPersistence`, which `store.Store`
    implements) (TC-ACL-2.10).
  - Command and attribute privileges came from the cluster servers alone,
    with Operate as the fallback, so every server that did not declare one
    let an Operate subject through: Identify and TriggerEffect (Manage),
    PayloadTestRequest (Manage), a write to
    BridgedDeviceBasicInformation.NodeLabel, OnOff.StartUpOnOff or the
    Thermostat setpoint limits (Manage). The dispatcher now takes matter.js's
    privilege for every command (`schema.InvokePrivilege`, held against the
    element files by a test) and every attribute
    (`schema.AttributeWritePrivilege`, generated from the access strings of
    `parity/schema.json`, inherited ones included); a server can only raise
    it (TC-ACE-2.2, TC-ACE-2.3).
  - A write to a global attribute (AttributeList, FeatureMap, …) reached the
    cluster server and answered FAILURE or UNSUPPORTED_ATTRIBUTE; it answers
    UNSUPPORTED_WRITE now, as matter.js models them read-only (TC-ACE-2.2).
  - An event-only subscription established even when the subject may not
    read any of the requested events (AccessControlEntryChanged without
    Administer) or the path names no cluster the node has. Such a
    subscription is now rejected with INVALID_ACTION, as matter.js counts
    only readable, existent event paths (EventReadResponse.ts,
    ServerSubscription.ts) (TC-ACE-1.2).
  - An InvokeResponse too large for one datagram was dropped by the
    listener. It is now chunked with MoreChunkedMessages, each chunk but
    the last waiting for the controller's StatusResponse, as matter.js
    InteractionMessenger.ts sendInvokeResponseChunk does; a single entry
    that cannot fit answers RESOURCE_EXHAUSTED (TC-IDM-1.4).
  - An ongoing subscription report that needed several chunks sent them
    back to back without the per-chunk StatusResponse handshake and without
    piggybacking the ack of the controller's StatusResponse; chip drops
    such a chunk ("Dropping message without piggyback ack when we are
    waiting for an ack") and with it every change it carried. Ongoing
    reports now follow the same handshake as the priming report
    (InteractionMessenger.ts sendDataReportMessage, MessageExchange.ts
    send) (TC-IDM-4.3).
  - The subscription engine sent keep-alives and change reports for a
    subscription whose priming report was still streaming; a controller
    answers those with INVALID_SUBSCRIPTION and tears the subscription
    down. A subscription stays silent until its SubscribeResponse is sent,
    as matter.js's ServerSubscription only starts its timers after
    activation (TC-IDM-4.3).
  - A write to a bridged endpoint's BridgedDeviceBasicInformation.NodeLabel
    was accepted but not kept: the next read returned the host label again,
    and a reassembly lost it. The written label now lives on the endpoint,
    survives Reassemble, and reaches the host through
    `endpoint.Config.OnNodeLabelWritten` for persistence (TC-IDM-4.3).
  - A successful write was not reported to subscribers either: writing
    BasicInformation.NodeLabel never reached a subscription to it. Written
    attributes are now marked dirty (TC-IDM-2.3).
  - A write to an attribute the cluster does not implement answered
    UNSUPPORTED_WRITE (or reached the server); it now answers
    UNSUPPORTED_ATTRIBUTE before writability is considered, as matter.js
    AttributeWriteResponse.ts does (TC-IDM-3.2).
  - AdministratorCommissioning.OpenCommissioningWindow had no field decoder,
    so every controller's enhanced commissioning window — the multi-admin
    "share" — answered INVALID_COMMAND. It is decoded now (TC-IDM-1.2).
  - A fabric-scoped command on a session without an accessing fabric (PASE
    before AddNOC) reached the server; it now answers UNSUPPORTED_ACCESS
    (matter.js CommandInvokeResponse.ts:287, TC-IDM-1.2).
  - An InvokeRequest or WriteRequest whose Timed flag is set without a
    preceding TimedRequest answered NEEDS_TIMED_INTERACTION; matter.js
    answers TIMED_REQUEST_MISMATCH. A timed-required command outside a timed
    interaction now answers NEEDS_TIMED_INTERACTION for its own path in the
    InvokeResponse instead of failing the whole interaction (TC-IDM-1.2).
  - Sigma2 carried no session parameters, so a controller assumed
    MaxPathsPerInvoke 1 against a device whose BasicInformation says 10
    (TC-IDM-1.4).
  - The root Descriptor and AdministratorCommissioning had no DataVersion
    and answered every read with the sentinel, which a DataVersionFilter
    never matches. Both now derive one from their content (TC-IDM-2.2).
  - BasicInformation.CapabilityMinima lacked the four fields revision 6
    makes mandatory; they default to matter.js's 20 (TC-IDM-2.3).
  - GeneralDiagnostics TimeSnapshot answered under its request's command id
    with an empty struct; the response is encoded now, with PosixTimeMs
    null as matter.js reports it without TimeSynchronization.
  - LevelControl lacked MinLevel / MaxLevel (conformance "Rev >= v7"), and
    the CT ColorControl lacked RemainingTime, which ColorTemperatureLight
    requires (TC-IDM-10.2).

- **Found by the chip-tool data-model sweep** (`internal/chiptool`,
  which holds every cluster a real controller reads against its own global
  lists and the matter.js schema):
  - `cluster/thermo`: AcceptedCommandList was empty — the server handled
    SetpointRaiseLower, the cluster's one mandatory command, but
    implemented no command lister, so the dispatcher synthesised `[]`. It
    now lists SetpointRaiseLower.
  - `cluster/cover`: AcceptedCommandList was empty for the same reason. It
    now lists UpOrOpen, DownOrClose, StopMotion and, with LF,
    GoToLiftPercentage — the commands the server handles and matter.js
    mandates.
  - `cluster/light`: the CT-only ColorControl server lacked the two
    attributes conformance "CT" makes mandatory,
    CoupleColorTempToLevelMinMireds (0x400D, the physical minimum, as
    matter.js falls back to) and StartUpColorTemperatureMireds (0x4010,
    nullable, writable at Manage, constraint 1 to 65279). New wire
    constants `ColorCtrlAttrCoupleColorTempToLevelMinMireds` and
    `ColorCtrlAttrStartUpColorTemperatureMireds`.
  - `cluster/core`: OperationalCredentials, GroupKeyManagement and Groups
    served EventList (0xFFFA) and named it in AttributeList, while the
    dispatcher leaves it out of every other cluster and matter.js marks it
    deprecated (conformance "D"). Groups even named it in AttributeList
    without returning it from a wildcard read. All three now leave it out.

- GroupKeyManagement GroupTable and GroupKeyMap write GroupId,
  GroupKeySetId and the endpoint ids at their smallest TLV width, as
  matter.js's `TlvUInt16` does, instead of always two bytes (both are valid
  TLV; pinned against matter.js in `bridge/testdata/groupcast-wire-fixtures.json`).
- **AccessControl Acl and Extension answered a non-fabric-filtered read with
  the accessing fabric's entries only.** They now return every fabric's
  entries, as matter.js does (`ListManager` filters a fabric-scoped list
  only on a fabric-filtered read): the accessing fabric's whole, another
  fabric's with its fabric-sensitive fields — Privilege, AuthMode,
  Subjects, Targets; Data — withheld, so such an entry carries FabricIndex
  alone (`StructManager` / `AccessControl.mayRead`). Reading either still
  needs Administer. `core.AccessControlEntryStruct.Redacted` and
  `core.AccessControlExtensionEntry.Redacted` mark such an entry; matter.js
  encodings are pinned in `bridge/testdata/groupcast-wire-fixtures.json`.
- **Switch press events went out without their position.** InitialPress,
  LongPress, ShortRelease and LongRelease carried TLV null in the EventDataIB
  Data slot: their payload types were unexported, so the bridge's value
  writer had no case for them. They are now `wire.SwitchInitialPressEvent`,
  `wire.SwitchLongPressEvent`, `wire.SwitchShortReleaseEvent` and
  `wire.SwitchLongReleaseEvent`, encoded as matter.js encodes them
  (`{0: position}`, pinned in `bridge/testdata/application-wire-fixtures.json`
  and checked through the event-read path). MultiPressOngoing /
  MultiPressComplete are unaffected: the server does not advertise MSM.
- **An AccessControl Group entry could not be written.** The validator
  accepted only Group Node IDs (0xFFFF_FFFF_FFFF_FFxx) as Group subjects;
  the subject of a Group entry is a Group ID, 0x0001..0xFFFF, as matter.js
  validates it and as a group message's subject carries it.
- **GroupKeyManagement worked only through typed Go calls.** The bridge had
  no wire codec for KeySetWrite / KeySetRead / KeySetRemove /
  KeySetReadAllIndices, so a controller's command reached the server as a
  generic tag map and answered Failure, and KeySetReadResponse /
  KeySetReadAllIndicesResponse had no encoder. Both directions now go
  through the schema matter.js uses (`TlvOfModel`, pinned by wire fixtures
  from matter.js in `bridge/testdata/group-wire-fixtures.json`); a payload
  that does not match the schema answers InvalidCommand as in matter.js.
  `im.FieldsContainerConsumed` lets a fields reader report a reject it found
  at the container's end.
- GroupKeyManagement follows matter.js HEAD (`452d6f5c`,
  `GroupKeyManagementServer.ts`): KeySetWrite accepts any
  GroupKeyMulticastPolicy and does not store it, and KeySetRead reports
  PerGroupID (`core.GroupKeySetStruct.GroupKeyMulticastPolicy`,
  `core.GroupKeyMulticastPolicyPerGroupID`); KeySetReadAllIndices always
  lists key set 0 first; the MaxGroupKeysPerFabric budget counts the
  implicit IPK once whether or not the store holds a row for it; a
  GroupKeyMap write is refused with InvalidAction for a non-application
  GroupId, ConstraintError for a duplicate and ResourceExhausted beyond
  MaxGroupsPerFabric.

- **A GroupKeyMap entry naming a key set not written yet failed with
  Failure.** `matter_group_key_map` carried a foreign key into
  `matter_group_keys`; matter.js accepts the write
  (`GroupKeyManagementServer.ts #validateGroupKeyMap`) and the entry
  authenticates nothing until the set exists. The constraint is gone:
  `store.Upgrade` (run by `store.Apply`) rebuilds an existing table without
  it, and `RemoveGroupKeySet` / `RemoveGroupKeysByFabric` now drop the
  GroupKeyMap entries naming the removed sets explicitly. A host that feeds
  `store.Schema()` through its own migration tool calls `store.Upgrade`
  once after it.

- **GroupKeyMap and GroupTable answered an unfiltered read with the
  accessing fabric's entries only.** Both are fabric-scoped lists without a
  fabric-sensitive field, so a read with `isFabricFiltered=false` now
  returns every fabric's entries, as matter.js does (`ListManager`
  filters only fabric-filtered reads and fabric-sensitive lists) and as
  `OperationalCredentials.Fabrics` / `NOCs` already did. Adds
  `groups.Manager.Fabrics`.

- **A fail-safe revert of AddNOC left the fabric's group state loaded.**
  The revert removes a fabric the way RemoveFabric does (core§11.9.7.2
  step 6, matter.js `FailsafeContext.rollback`), so it now runs
  `OperationalCredentials.NotifyFabricRemoved` and the `OnFabricRemoved`
  hook as well — a host that hands that hook to `Bridge.EmitFabricRemoved`
  forgets the reverted fabric's keys, group table, subscriptions and
  multicast memberships.

### Deprecated

- `wire.Groups`, the read-only Groups stub: group membership is stack state
  now, and the assembler mounts the real server where a device type
  mandates Groups (replacing the stub where a source still supplies it).
  Removal permissible in v0.3.0.
- `im.StatusUnreportableAttr` (0x8c) and `im.StatusNoUpstreamSubscription`
  (0xc5): Matter 1.6.1 removed both codes and matter.js dropped them from its
  status table. No replacement; removal permissible in v0.3.0.
- The never-wired `store` subscription API — `PersistentSubscriptionRecord`,
  `ErrPersistentSubscriptionNotFound`, `SavePersistentSubscription`,
  `LoadPersistentSubscriptions`, `DeletePersistentSubscription`,
  `DeletePersistentSubscriptionsByFabric`, `GetPersistentSubscription`,
  `PersistentSubscriptionIntervals`, `MarshalIntervals`, `UnmarshalIntervals`
  — and its `matter_persistent_subscriptions` table. Nothing ever wrote or
  read it. Replaced by `Store.SaveServerSubscription` and its siblings on the
  new `matter_server_subscriptions` table; removal permissible in v0.3.0.

### Security

- **A session with no fabric no longer reads another fabric's
  fabric-scoped data.** A read on a PASE session before AddNOC (FabricIndex
  0) — a second commissioner, say, while a fabric already exists — was
  answered as a read without a request behind it: AccessControl `Acl` and
  `Extension` returned the entries of the fabric the last ACL write
  targeted, whole; a fabric-filtered read of OperationalCredentials `NOCs` /
  `Fabrics` returned every fabric's entries, GroupKeyManagement `GroupKeyMap`
  / `GroupTable` the fabric last set with `SetCurrentFabric`, and
  `CurrentFabricIndex` the fabric of the last AddNOC. Now, as in matter.js
  (ListManager's FabricFilteredListProxyHandler, `AccessControl.ts`
  `mayRead`, `OperationalCredentialsServer` `currentFabricIndex`), such a
  session's fabric-filtered read of a fabric-scoped list is empty, its
  unfiltered read of `Acl` / `Extension` carries every entry redacted to its
  FabricIndex, and `CurrentFabricIndex` reads 0; unfiltered `NOCs`,
  `Fabrics`, `GroupKeyMap` and `GroupTable`, which carry no fabric-sensitive
  field, still list every fabric. A read with no Interaction Model request
  behind it keeps its previous scope; `im.LookupFabricFilter` (new) tells
  the two apart. A host whose session table cannot adopt a PASE session
  onto the fabric its AddNOC installed (`AdoptFabricIndex`) now reads
  `CurrentFabricIndex` 0 on that session after AddNOC as well.

## [0.1.0] — 2026-10-02

The first tagged release. It is the state of `main` the reference daemon
already consumes as a pseudo-version, so moving from that pseudo-version to
`v0.1.0` requires no source change. The entries below record what reached
`main` before the first tag, including the deprecations and removals a
consumer of an older pseudo-version should read.

### Changed

- **Requires Go 1.27.1.** `go.mod` targets Go 1.27.1 and every CI workflow
  builds with it; a consumer module has to be on Go 1.27 or newer.

### Fixed

- **A commissioner lost its implicit Administer grant the moment AddNOC
  succeeded.** The IM ACL gates keyed the PASE bypass on `FabricIndex == 0`,
  but AddNOC adopts the commissioner's PASE session onto the new fabric, so
  every follow-up over that channel — the ACL write Apple sends next,
  GroupKeySetWrite, AccessControl event reads, an ongoing subscription — was
  evaluated as fabric N with subject node-id 0 and answered
  UnsupportedAccess. The grant is now keyed on the session's auth mode as
  matter.js does (`FabricAccessControl.ts:189-191`): `im.WithAuthModePASE`
  / `im.IsPASEFromContext`, `EventReadAuthorizer.PASE`,
  `operational.Entry.IsPASE`, and the bridge stamps it from a
  `SessionPASEResolver` — wire it with
  `OperationalSessionLookup.WithPASEResolver` (the reference bridge does).
  Unwired, behaviour stays fail-safe: no grant, as before.

- **Ongoing event reports were sent on the commissioner's closed Subscribe
  exchange.** Attribute reports had moved to a bridge-initiated exchange for
  exactly this failure; event reports still went out with `Initiator=false`
  on the peer's Subscribe exchange, which a matter.js controller acks and
  discards (`ExchangeManager.ts:411-418`). Reproduced live with chip-tool.
  Event reports now take the same bridge-initiated path.
- **A Sigma1 addressed at a fabric this node does not hold is refused with
  NoSharedTrustRoots** before any key material is generated
  (`sigma.ErrNoSharedTrustRoots`; matter.js `CaseServer.ts:88-90,239`).
  The responder used to fall back to its constructor-time identity and
  answer a full ECDH/ECDSA/AES round with a Sigma2 the peer could never open.
- **The CASE adapter's once-only gate armed wrong in both directions:** an
  idempotent Sigma1 replay re-armed it (a Sigma3 retransmit then displaced
  the live session), and a second resume handshake on a live adapter never
  installed its keys. The responder takes a fresh session id for a resume
  after a completed handshake, and the adapter fires its callback for every
  resume that yields a new id.
- **Operational-certificate validity is no longer enforced against the raw
  wall clock.** matter.js only warns on a NotBefore in the future and never
  checks NotAfter (`OperationalBase.ts:82-89`, LKGT pending); a bridge whose
  clock is behind the commissioner's refused AddNOC and every later CASE.
- **The NOC/ICAC chain verifier enforces the structural predicates of
  matter.js `Noc.ts` / `Icac.ts`:** NOC not a CA, keyUsage digitalSignature,
  EKU serverAuth/clientAuth, 20-byte SKID, operational node id, non-zero
  fabric id, ≤3 CATs with non-zero version; ICAC a CA with keyCertSign|cRLSign,
  no EKU, no CATs; ICAC fabric id equal to the NOC's; 400-byte TLV cap on the
  Sigma path. An ICAC minted for fabric A could previously issue a NOC for
  fabric B under the same root.
- **The test attestation chain is rooted at the VID-less CSA test PAA**, as
  matter.js's own generator does (`AttestationCertificateManager.ts:38-44`).
  Rooted at the FFF1 PAA, every `vendor_id != 0xFFF1` failed the PAA-VID vs
  PAI-VID check (`DeviceAttestationValidator.ts:328-334`).
- **`ClosureControl.MoveTo` accepted only a typed struct** and refused the
  tag map the bridge decodes every command into — no controller could drive
  a bridged garage. The server accepts the wire shape like valve/modeselect.
- WindowCovering: ConfigStatus advertised LiftMovementReversed instead of
  LiftPositionAware and FeatureMap/ClusterRevision were unserved. Thermostat
  SetpointRaiseLower inverted the Mode enum and read a payload shape the
  bridge never produces. Identify stored IdentifyTime=0 for every Identify.
  GroupKeyMap writes stamp the accessing fabric instead of rejecting.
  DiagnosticLogs timestamps are Matter-epoch / system-time microseconds.
  `timedInvokePaths` lists ClosureControl MoveTo/Calibrate as timed.
- Subscriptions report to the peer's current source address instead of the
  one frozen at Subscribe time; a StatusResponse of InvalidSubscription or
  Failure ends the subscription; Read/Subscribe responses are bounded (path
  count, materialised size, duplicate paths coalesced) and an unanswered
  chunk aborts the read after matter.js's `maxPeerResponseTime`; over-wide
  integers in command fields are rejected with ConstraintError instead of
  truncated.
- reference-bridge: a stale pending fabric index survived
  CommissioningComplete and reverted the committed fabric on the next
  fail-safe expiry; the root PartsList named only the aggregator; the light
  endpoints mount Groups/ScenesManagement and OnOff advertises LT with a
  timed-on/delayed-off port of `OnOffServer.ts`.
- The exported commissioning package derived AttestationChallenge from a
  second HKDF and swapped I2R/R2I; it now runs the one key schedule of
  §4.13.2.5 like `secure/operational`.
- `mdns.Diagnose` no longer flags every 172.16.0.0/12 LAN as
  container-internal (only Docker's 172.17.0.0/16 default); the subtype
  announcer's `AfterFunc` no longer races `Close`.
- `parity/schema.json` is re-extracted from the matter.js commit it records
  (previously it carried the pre-discriminator (tag,id) collapse — RootNode,
  the switch/controller device types and Pump/WaterValve lost mandatory
  server-cluster requirements).

### Deprecated

- `mrp.Retransmitter` / `mrp.NewRetransmitter`: no consumer exists;
  production reliability is `bridge/outbound_reliable.go`. Removed after one
  deprecation window.

### Fixed

- **The per-fabric ACL cap counted the FabricIndex the client sent, then
  stored every entry on the writer's fabric.** `AccessControl.ACL` writes
  filtered the count to entries whose FabricIndex matched the writer's (or
  was zero) before checking `AccessControlEntriesPerFabric`, and then stamped
  the writer's fabric on every entry it persisted — so a list of twenty
  entries tagged with a foreign index passed the limit of four and all twenty
  landed on the writer's fabric, while the attribute kept reporting four. The
  count is now the list as stored. matter.js reaches the same number because
  its fabric-scoped write machinery stamps the accessing fabric before
  `AccessControlServer.ts` filters on it.
- **Privacy masking covered only the first AES block of the protected
  header; the region is 20 bytes when both node ids are present.** Counter,
  Source Node ID and a 64-bit Destination Node ID are 4 + 8 + 8 bytes, and
  matter.js runs the CTR keystream over all of them
  (`MessagePrivacy.ts:53-56`). Capping the mask at 16 bytes left the tail of
  the destination id in the clear on send and obfuscated on receive, and the
  AEAD tag then failed on whichever side had unmasked the wrong bytes.
  `channel.PrivacyKeystream` now yields as many blocks as the region needs.
  Alongside it, a unicast frame carrying the P bit is dropped rather than
  unmasked — privacy enhancements are defined for group messages, and
  matter.js (`ExchangeManager.ts:220-224`) and the chip SDK both drop the
  unicast case.
- **The AEAD nonce used a Security Flags byte rebuilt from typed fields.**
  Reserved bits 4-2 of a received frame were dropped on decode and read back
  as zero, so a peer that set one would fail the tag check on every frame
  while matter.js decoded it (it keeps the raw byte for exactly this use,
  `MessageCodec.ts:45`). `Header.NonceSecurityFlags` returns the byte as
  received for a decoded header.
- **The nonce node id came from the header's Source Node ID when present,
  not from the session.** matter.js and chip build the nonce from the peer id
  the session was established with (`NodeSession.ts:187`); reading it off the
  header made a mismatched label fail the tag and a matching one partly
  sender-chosen. `Session.Decrypt` uses the session's peer id unconditionally.
- **PASE session parameters decoded the two retransmit timeouts into 16
  bits.** `idleInterval` and `activeInterval` are `TlvUInt32` in matter.js
  `PaseMessages.ts` — and `uint32` on this module's own CASE path — so a
  commissioner advertising 100 000 ms was paced at 34 464 ms. Both fields of
  `spake2.MRPParameters` are `uint32` now; `ActiveThresholdTimeMs` stays
  `uint16` as the schema declares it.
- **`sigma.Responder.SetResumptionStore` and `SetSessionParameters` wrote
  without the responder lock** that every handshake path reads under, unlike
  the two sibling setters. Both take it now.
- **`subscription.Manager.Start` said "Idempotent" and was not** — every call
  launched another engine goroutine, doubling every tick and heartbeat. A
  `sync.Once` makes the doc true.

### Added

- **`MeasurementContext` — a host-registered measurement kind can now express
  every shape the library can.** The materialiser signature carried only the
  source, so the two built-ins that need the endpoint id — the generic switch,
  which takes it at construction because event delivery addresses a Matter
  path, and PowerSource, which must name the endpoint it feeds (§11.7.6.20) —
  had to stay hard-coded in `endpoint/materialize.go`. Both are ordinary
  registered materialisers now, and that file names no measurement class at
  all. The context is a struct rather than a bare id on purpose: adding a field
  later is source-compatible, where adding a parameter after `v0.1.0` would
  cost a deprecation cycle.

- **The reference daemon mounts the three cluster servers that had no host.**
  `cluster/valve`, `cluster/modeselect` and `cluster/levelcontrol` shipped with
  no endpoint anywhere mounting them, so their subscription paths were
  exercised only through collaborations a test constructed itself — a
  bracketing test by this project's own definition, recorded rather than
  hidden when they landed. A WaterValve, a ModeSelect device and a Speaker now
  sit in the example fleet behind fake devices that hold real state, and the
  guard that could not be written before exists: a device-side change reaching
  a subscriber through a **mounted** endpoint.

- **A stated API-stability and deprecation policy, with a guard behind it.**
  The module is on its own SemVer lane and nothing is tagged, so a consumer had
  no way to know what it may depend on or how much warning a rename carries.
  README.md now says which packages are public (measured against the real
  `internal/` tree, not asserted), how a deprecation is marked, how long it
  stays, and what pre-1.0 changes about that. Prose enforces nothing, so two
  tests hold the policy to its word: a marker that does not open its paragraph
  with `Deprecated: ` produces no SA1019 warning at any consumer and is
  refused, and a deprecated identifier that appears nowhere in `CHANGELOG.md`
  is refused too — the window only means something to someone told the clock
  is running. Both pass vacuously today, because the module has deprecated
  nothing, and say so out loud rather than looking asleep.
- **One chip `Test_TC_` conformance case runs against the reference daemon.**
  `Test_TC_OO_2_2` drives Off/On/Toggle with read-back, including both
  idempotency cases, gated in CI like the other chip-tool jobs. Certification
  stays a non-goal: the PICS file says in its own text that it is written only
  as far as this case needs and is not a certification artefact. Running it
  needs chip's Python runner, because chip-tool at the pinned commit registers
  no `tests` command at all — that is recorded rather than worked around.

- **Eleven new fuzz targets, over the five packages that parse bytes.** The
  module fuzzed only its four Interaction-Model decoders; everything else that
  turns wire bytes into structures was unfuzzed, including the two packages a
  commissioner reaches *before* anything is trusted — DER certificate parsing
  in `secure/mattercert` and the setup-payload surface in `secure/setup`. Seeds
  come from each package's own tests rather than being invented, so they are
  known-good, and each target carries deliberately malformed variants. None
  found a crash. Where an encoder exists the target asserts a round-trip; none
  asserts a specific error for malformed input, which is not the property.
- **Ten benchmarks and a per-package coverage floor.** Both were absent
  entirely. The benchmarks cover the paths a running bridge repeats — TLV
  encode/decode and validate, inbound datagram decode, initial-report
  construction, endpoint assembly — and each says in its doc comment why that
  path was chosen. `script/coverfloor` compares `go test -cover` against a
  table with a reason per row; floors sit below each package's measured
  coverage so the gate ratchets against regression rather than failing on day
  one.

- **`cluster/levelcontrol` — a host-agnostic LevelControl server (0x0008).** Speaker
  0x0022 mandates OnOff and LevelControl servers, and this module had neither
  a LevelControl server nor a way to build one: `cluster/wire` carried command
  decoders only. Serves exactly the three conformance-M attributes
  (CurrentLevel 0x0, Options 0xf, OnLevel 0x11) and all eight M commands. The
  four `WithOnOff` variants get their own port methods rather than a flag on a
  shared request — a bool a host forgets to read compiles fine and turns "turn
  it on and set the level" into "set the level while it stays off".
- **The measurement-kind set is open: a host can register one without editing
  this module.** It was a closed enum answered by switches, so every new
  measurement meant a library change. `RegisterMeasurementKind` now takes a
  descriptor carrying the device type, the cluster id and the materialiser
  that builds the servers, and refuses one without a materialiser — a registry
  able to advertise what it cannot build is the defect, so it is made
  impossible rather than documented. The sixteen built-ins keep their exact
  values and behaviour; their materialisers moved verbatim into
  `cluster/measurement` and install themselves through the same seam idiom the
  module already uses, so `contract` gains no dependency. What a registered
  kind cannot yet express is stated in `endpoint/materialize.go`: the two
  shapes that need the endpoint id at construction — the generic switch and
  the battery re-entry — stay library-only.

- **`cluster/valve` — ValveConfigurationAndControl (0x0081), the server behind
  WaterValve 0x0042.** Serves the five attributes matter.js marks conformance M
  (OpenDuration 0x0, DefaultOpenDuration 0x1, RemainingDuration 0x3,
  CurrentState 0x4, TargetState 0x5) and handles Open 0x0 / Close 0x1. Every id
  and conformance string is cited to
  `valve-configuration-and-control.element.ts`; AutoCloseTime 0x2 is `"TS"` and
  the level attributes are `"LVL"`, so neither is served while FeatureMap is 0
  — an `Open` carrying TargetLevel is refused with ConstraintError rather than
  silently ignored. It reaches a host through a narrow port instead of mutating
  internal state: a host refusal does not become Success.
- **`cluster/modeselect` — ModeSelect (0x0050), the server behind device type
  0x0027.** Description 0x0, StandardNamespace 0x1, SupportedModes 0x2 and
  CurrentMode 0x3 served from a host-supplied list, ChangeToMode 0x0 forwarded
  to the host, and an unsupported mode answered with InvalidCommand. StartUpMode
  and OnMode are absent and FeatureMap is 0, because DEPONOFF would make OnMode
  mandatory. The wire writer gained the one case it was missing — a list of
  structs — so SupportedModes can reach a controller at all, with the constraint
  bounds (255 modes, 64 tags) applied at encode time rather than trusted from
  the host.

- **`endpoint/sqlitestore`, a production `endpoint.Store`.** The port shipped
  with only `endpointtest.NewFakeStore` behind it, so every consumer had to
  write the real one — and endpoint identity is the piece of bridge state
  that must survive a restart, because a controller keys its accessory list
  on the endpoint number and is never told to re-read it. The package takes
  an already-open `*sql.DB` and imports no driver, and it is a separate
  package on purpose: a host that brings its own store links none of it and
  creates none of its tables. Its `WithKeyDecoder` option exists for a
  failure the port made possible and nothing announced — the assembler
  garbage-collects by comparing the keys a store *lists* against the keys the
  live snapshot carried, as interface values, so a store that hands back
  `endpoint.StringKey` to a host with a composite key type makes every live
  row look vanished and deletes every endpoint number on the first
  model-complete assembly. Both directions are pinned by a test through the
  real assembler.
- **`store.Schema()` / `store.Apply`, and the same pair on
  `endpoint/sqlitestore`.** The DDL the store's queries are written against
  lived under `store/testdata/`, where a host can read it and never import
  it; the module's own example had transcribed 140 lines of it, and two
  copies of one schema drift with nothing to catch it. The text is now
  embedded in the package it belongs to and handed out, so a column added
  here arrives with the dependency bump. Both scripts are idempotent, and
  the store package's own tests apply them through the exported entry point
  rather than through a fixture beside it.
- **`sigma.DeriveOperationalIPK`.** Turning the raw `AddNOC.IPKValue` into
  the operational IPK the CASE handshake keys on was documented in a doc
  comment and implemented nowhere in the module, so each host reconstructed
  a security-relevant HKDF derivation from prose. It is now exported next to
  `ComputeDestinationID`, which consumes it, with every input cited to
  matter.js and pinned by matter.js's own group-key test vector. The doc
  comments that had described the raw value as `Identity.IPK` were wrong and
  say the opposite now.

- **Test-support packages, so a consumer's tests stop reimplementing the
  module's fakes and stop sending traffic to set up state.** The module's
  first external consumer — a scenario harness driving the bridge from
  another repository — hit four places where the public API was too narrow,
  and one of its workarounds cost that repository a flaky CI job: with no way
  to tell the bridge where a subscription's reports go, the harness issued a
  real `SubscribeRequest` over the wire and read the id back, and on a loaded
  machine that setup traffic landed inside the window the scenario then
  measured. `bridge/bridgetest` closes it — `EstablishSubscriptionTarget`
  registers the reply route directly, and `AckPumpTick` runs one whole pump
  iteration (the outbound retransmits included, which `RunAckPumpOnce` does
  not cover) rather than waiting out the pump goroutine's ticker.
  `endpoint/endpointtest` carries the in-memory `endpoint.Store` and the
  empty-fleet snapshotter that were stranded in `_test.go` files and so were
  invisible outside the package. None of this widened the production API:
  the affordances reach the bridge through a module-internal seam, so an
  external module sees `bridgetest` and cannot reach what it stands on.
  `EstablishSubscriptionTarget` leaves the *whole* post-handshake state: the
  `SubscribeRequest` it stands in for also anchored the peer's inbound MRP
  duplicate-detection window, and a secure session's window anchors with an
  all-ones bitmap — so without the anchor the peer's next two messages had
  to arrive in order or lose the earlier one to the duplicate path, which is
  how the same consumer's StandaloneAck raced its own StatusResponse.
  `SubscriptionTarget.PeerCounter` carries the counter that request would
  have used, and a secure target must name it: nothing on the bridge side
  can derive a counter from a handshake that never happened.
- **`message.Header.SecurityFlags`.** The Security Flags byte is an input
  every caller of `channel.Session.Encrypt` / `Decrypt` has to supply
  alongside the header, and it was derivable only inside this module. It now
  sits next to the encoder that writes it, so the byte fed to the AEAD nonce
  and the byte written to the wire come from one derivation — the bridge's
  private copy had already drifted in its session-type mask, harmlessly only
  because decode rejects the values where the two disagreed.
- **The Matter bridge stack is a standalone module.** The subtree and its
  port contracts left the host daemon they grew in and became
  `github.com/SukramJ/go-fabric`, with its own dependency set: TLV codec,
  Interaction Model, PASE/CASE session establishment, MRP over IPv6 UDP,
  DNS-SD advertisement, the cluster servers a bridge needs, and the endpoint
  assembler. The `contract` package is the seam: a host implements those
  interfaces to expose its own devices as bridged endpoints and keeps
  ownership of its device model, while this module owns the wire format.
  Tests that reached into the host's device model, or read fixtures outside
  the module, did not travel; the ones whose subject is the wire format did.
- **Four commissioning security guards are back.** The NOC length cap, the
  root-certificate subject check, the ACL cleanup on revert, and the
  commissioning window's rejection of a PASE caller were lost in the
  extraction — their subject moved and they did not, so they existed in
  neither repository. They return as behavioural tests rather than the source
  scans they were: the subject now sits in the same module as its test, so
  the real handler is driven and the effect observed, where a scan could not
  tell a check that runs from one that was moved after the action it
  protects. Each carries a control (the 400-byte NOC, the untouched fabric
  indices, the CASE leg), and each was observed failing before it was kept.
- **A licence-header guard over the whole module.** Every `.go` file must
  carry the SPDX identifier and the copyright line ahead of its package
  clause. It reads the comment block before `package` rather than lines 1
  and 2, because a build-constrained file opens with its build tag.
- **CI, a pinned lint gate and dependency tracking.** Build, vet, race-enabled
  tests and lint run on every push and pull request, with the Go, `gofumpt`
  and `golangci-lint` versions pinned to the same values the reference daemon
  uses. Dependabot tracks the module's Go dependencies and the SHA-pinned
  actions.
- **The Matter schema pipeline arrived with the code it feeds.** The matter.js
  extractor (`script/extract-from-matter-js.ts`) and the schema generator
  (`script/generate_matter_schema.go`) followed `parity/schema.json` and
  `schema/` out of the reference daemon, where the two halves had been sitting
  in different repositories with a `cp` between them. The generator now reads
  the embedded snapshot itself rather than a second copy of the extract, so
  the constants it emits cannot describe bytes no parity test validates;
  `go generate ./schema/...` drives it, and it gofmts its own output instead
  of leaning on a formatting step in a `Makefile` this module does not have.
  Regenerating from the unchanged snapshot reproduces `clusters.go`,
  `devicetypes.go` and the `SchemaSnapshotSHA256` constant byte for byte.

### Removed

- **`bridge.New` no longer takes an `endpoint.Store`.** The signature is now
  `New(snap Snapshotter, advertiser mdns.Advertiser, cfg Config, logger
  *slog.Logger)`. The store was stored on the `Bridge` and read by nothing —
  the bridge consumes a fully assembled topology through the `Snapshotter` and
  never looks an endpoint record up itself, so the parameter asked every host
  for a collaborator the package had no use for and made the constructor read
  as though the bridge owned endpoint persistence. There is no replacement,
  because there was nothing to replace: a host still needs a store, and still
  passes it to the `endpoint` assembler behind its snapshotter, exactly as
  before.

  **Upgrading requires a source change**: delete the first argument at every
  `bridge.New` call site. `bridge.New(store, snap, adv, cfg, logger)` becomes
  `bridge.New(snap, adv, cfg, logger)`; the store variable stays where it is,
  feeding the assembler. Nothing else moves, and a missed call site is a
  compile error rather than a behaviour change. The error string
  `"bridge: store is required"` is gone with the nil check that produced it.

  This ships without a deprecation window. A parameter cannot carry a
  `Deprecated:` marker that `staticcheck` would ever warn on, so honouring the
  window would mean shipping a second constructor for two minor releases; the
  module is at `v0` with nothing tagged, where README.md's API-stability
  section says `main` is the only consumable version and may break in any
  commit. What that state does not excuse is an unannounced break, which is
  what this entry is.

### Removed

- **`bridge.New`'s `store` parameter.** Nothing read it: the field was
  assigned and never used, which a compiler probe confirmed rather than a
  grep. Removing it is a breaking change to a v0 API, so it is announced here
  with the call-site edit rather than left for a consumer to discover at build
  time — the module's stated policy allows the break before `v0.1.0` but not
  an unannounced one. A parameter cannot carry a `Deprecated:` marker, so the
  window the policy describes does not apply and this entry stands in for it.

### Fixed

- **A certificate whose `NotBefore` overflows the epoch conversion was
  accepted.** `NotBefore` and `NotAfter` are Matter-epoch seconds and reaching
  Unix time adds the epoch; a field close to 2^64 wraps that addition to a
  small number, and a small number is one every real clock is already past — so
  the validity window passed. With `NotAfter == 0`, the long-lived RCAC
  convention, `decode.go`'s ordering check does not fire either, leaving nothing
  else to catch it. Measured: `NotBefore = 2^64-1001` becomes 946683799, and
  the certificate verified on an ordinary clock — no exotic device state
  required. Both conversions are checked now. Found by writing down what a
  `//nolint` was actually suppressing.

- **A fail-safe armed over PASE could be completed by any already-commissioned
  fabric.** A PASE arm stamps fabric index 0 because PASE has no fabric, and
  the ownership check read `failSafeFabricIndex != 0 && …` — so that 0 meant
  "anyone may complete this". An already-authorised controller on another
  fabric could send `CommissioningComplete` into a window it had not opened,
  aborting the admin who did and committing half-installed state without the
  expiry rollback running. Both references keep plain equality here and stay
  correct because AddNOC re-stamps the context onto the fabric it installed
  (Matter §11.18.6.16); this module now does the same, so the check is
  equality and a first pairing still completes. Guarded in both directions:
  the hijack is refused, and the legitimate PASE-arm-then-CASE-complete
  sequence still works.

- **A bridge that attaches an ACL it cannot enforce now refuses to start.**
  `CheckACL` answers Success for fabric index 0, which means "PASE, no fabric
  yet" and is correct while commissioning. But `resolveSessionFabric` returns
  that same 0 when the session lookup does not implement
  `SessionFabricResolver` — so a host that wired an ACL and a lookup without
  that capability had *every* CASE session resolve to 0, and every operational
  request passed the access check as though the device were still being
  commissioned. Nothing failed and nothing logged; the AccessControl entries
  were simply never applied. The neighbouring branch in `CheckACL` already
  fails closed when there is no ACL source at all, on the same reasoning, so
  this is that answer one layer out — at start-up, where it is a wiring
  mistake rather than a silent runtime state. A host that attaches no ACL is
  unaffected. Found by writing the threat model, not by review.

- **A first pairing of the reference daemon timed out in operational
  discovery.** The post-AddNOC hook rebuilt the CASE identity and published
  nothing. Boot advertised an operational record for every fabric already in
  the store, so restart-then-reconnect worked and hid the gap -- but a first
  pairing never goes through that path. The commissioner finished PASE,
  installed the fabric, then resolved
  `<compressed>-<node>._matter._tcp` against a record that had never been
  published, and spent its retry budget on a lookup that could not succeed.
  The hook now advertises the fabric, keyed on the identity it just loaded so
  the record and the CASE responder cannot name different nodes. Found by the
  chip-tool guard on its first run, with its control leg green in the same
  run.
- **The reachability snapshot stamped itself with a revision it could not
  know.** `inventory.json` carried `head` and `generated`, both the git
  revision at generation time -- necessarily the parent of the commit that
  carries the file. Against a check that compares the regenerated snapshot
  byte-for-byte, that field goes stale on the next run no matter what the
  code does, and it tells a reader the snapshot was measured one commit
  earlier than it was. Both fields are gone; the commit holding the file is
  its provenance.

[Unreleased]: https://github.com/SukramJ/go-fabric/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/SukramJ/go-fabric/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/SukramJ/go-fabric/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/SukramJ/go-fabric/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/SukramJ/go-fabric/releases/tag/v0.1.0
