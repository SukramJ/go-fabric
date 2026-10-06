# Changelog

All notable changes to go-fabric are recorded in this file.
The project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The module carries its own version lane, independent of any host that embeds
it. `v0.1.0` is its first tag; everything before it was consumed as a
pseudo-version of `main`.

## [Unreleased]

### Added

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

### Changed

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

### Fixed

- **Found by the CHIP Python certification harness** (`internal/chiptool`,
  run in matter.js's CHIP image against the reference daemon):
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
