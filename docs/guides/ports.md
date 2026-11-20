# Host ports

A host reaches the module through ports: interfaces it implements,
configuration fields it fills, and calls that attach a collaborator. This
page lists them by where they live. For each port it says what the port
does and what happens when you leave it out.

Two failure modes recur, and they are not interchangeable:

- **Fails closed**: the request is refused. A controller at least sees a
  denial.
- **Silent**: the reply looks well-formed, and the missing behaviour never
  shows up as an error anywhere.

A port's error is the answer of the request that called it: a
`contract.StatusError` answers its status (and cluster status), as does an
error carrying an IM status (`im.StatusCodeError`); any other error answers
`FAILURE`. Where a row below says "an error answers with its status", this
is what it means.

What stays public below the facade, and what going around it costs, is
on [Advanced seams](advanced-seams.md).

*Source: `bridge/bridge.go` (package doc).*

## `contract/`: what a device and a server implement

`contract/` imports the standard library only. A host type that implements
these interfaces does not drag the host's dependencies into the module.

### Endpoints and cluster servers

| Interface | Implemented by | What it does | When absent |
| --- | --- | --- | --- |
| `EndpointSource` | a host device | device type and cluster servers of one bridged endpoint | the source is not an endpoint |
| `ClusterServer` | every server | read, write, invoke; the reportable attributes | required |
| `FabricScopedReader` | a server whose value depends on the reader beyond the fabric scope, or a fabric-scoped list whose entries do not encode themselves and that takes a list-append write | read with the FabricFiltered flag and the reading fabric; a filtered read returns that fabric's entries alone | `MatterRead` answers; either way the dispatcher scopes every fabric-scoped attribute of the model on a request — another fabric's entries filtered, their fabric-sensitive fields withheld — whatever the server returns. A list-append write appends to the writing fabric's entries: the dispatcher keeps those of an entry that encodes itself by its `FabricIndex`, and an append to a list whose entries do not encode themselves answers `FAILURE` without a filtered read |
| `FabricScopeDeclarer` | a server of a cluster outside the schema snapshot with fabric-scoped lists (`spec.Instance` from its definition) | declares an attribute's fabric scope: fabric-sensitive, and its entry's fabric-sensitive fields | the cluster's lists are not scoped; for a cluster of the snapshot the generated table applies and the declaration is not asked |
| `ClusterAttributeLister` | a server | the attribute set, for wildcard reads and AttributeList | a wildcard read returns only the global attributes |
| `ClusterCommandLister` | a server | AcceptedCommandList and GeneratedCommandList | both lists are empty |
| `ClusterEventLister` | a server | EventList, and which event paths exist on the cluster; an event the server emits is recorded as emitted, so it emits only events it lists | EventList is empty and no event path exists on the cluster: a read of an event on it is answered `UNSUPPORTED_EVENT`, and an event path on it selects nothing for a subscription (`endpoint/dispatcher.go`, `EventPathStatus`) |
| `ClusterDataVersion` | a server | the cluster's DataVersion on the root and the Aggregator | the endpoint keeps the cluster's DataVersion, on the root and the Aggregator too, and advances it on every change the stack sees; a bridged endpoint's DataVersion is the endpoint's own, kept per cluster, whatever the server implements (`endpoint/dispatcher.go`, `clusterDataVersionFor`) |
| `ChangeReportingWriter` | a server | `MatterWriteChanged` applies a controller's write as `MatterWrite` does and says whether it changed any attribute of the cluster, the written one or another the write moves with it; the dispatcher calls it instead of `MatterWrite`. A write that changed nothing advances no DataVersion and is reported to no subscriber. Unchanged means the write stored nothing and the host heard nothing of it; a write that stored, or one the server cannot judge, answers changed. Every server of the module implements it, except OperationalCredentials, which refuses every write. A type that embeds a server implementing it and declares its own `MatterWrite` declares `MatterWriteChanged` as well: the promoted method would apply the write without the type's `MatterWrite` | every successful write counts as a change: the cluster's DataVersion advances and the written attribute is reported (`endpoint/dispatcher.go`, `writeOne`; `bridge/receive_dispatch.go`, `reportWrittenAttributes`) |
| `ChangeNamingWriter` | a server | `MatterWriteCommit` applies a controller's write and names the attributes of the cluster it changed — the written one only when its value moved, and any other the write moves with it; the dispatcher prefers it to `MatterWriteChanged` and `MatterWrite`. A non-empty list advances the cluster's DataVersion once and only the named attributes are reported; an empty list is a write that changed nothing. The list reaches the transport in the write's result (`im.WriteResult.Commit`, `im.HandleWrite`). `spec.Server` implements it. A type that embeds a server implementing it and declares its own `MatterWrite` or `MatterWriteChanged` declares `MatterWriteCommit` as well | `ChangeReportingWriter`'s verdict, recorded under the written attribute (`endpoint/dispatcher.go`, `writeOne`) |
| `ClusterAttributeReadPrivilege` | a server | a read privilege above the data model's; it can only raise it | the data model's read privilege (`schema.AttributeReadPrivilege`, from parity/schema.json) |
| `ClusterAttributeWritePrivilege` | a server | a write privilege above the data model's; it can only raise it | the data model's write privilege (`schema.AttributeWritePrivilege`, from parity/schema.json) |
| `ClusterCommandInvokePrivilege` | a server | an invoke privilege above Operate | Operate |
| `TimedCommander` | a server | which commands need a timed interaction | the generated table (`schema.IsTimedInvoke`) decides |
| `ClusterQuiescer` | a server with its own timers | stops the timers when the server leaves the topology or the bridge stops | timers outlive the server's use |
| `EventReceiver` | a server that emits events | receives the bridge's `EventEmitter` at assembly | the server cannot emit events |
| `IdentifyHandler` | a host device (an `EndpointSource`); the device layer's `Device` implements it for `HandleIdentify` and `HandleTriggerEffect` | `MatterIdentify` hears the Identify command, an IdentifyTime write and — with 0 — the end of the countdown; `MatterTriggerEffect` the TriggerEffect command. An error refuses the command or the write with its status and leaves IdentifyTime as it was. `MatterIdentify` runs with the server's IdentifyTime queue held (a request and the countdown's tick run one after the other): it must not set IdentifyTime on the same server from inside the call — a write or the Identify command there waits for the queue the call holds and never returns — and it should return promptly, because the countdown does not tick while it runs. Removing the endpoint does not wait for it: the server closes while a `MatterIdentify` is still running, so the handler may take a lock held across the removal | the assembler's Identify server counts IdentifyTime down and accepts both commands without telling the host |

### Change reporting

| Interface | Implemented by | What it does | When absent |
| --- | --- | --- | --- |
| `AttributeChangeNotifier` | a server that holds its state | names exactly the attributes that changed; the bridge advances the DataVersion and reports only those | see `ChangeNotifier` |
| `SelfReportedAttributeLister` | an `AttributeChangeNotifier` | attributes it reports only itself (quieter "Q" attributes) | the before/after comparison after a command includes them |
| `ChangeNotifier` | a host source, or a cluster server that forwards its port's notifier | "something changed", without naming what: the bridge reads the reportable attributes of the notifier's own clusters on the endpoint again and compares each with the value it read last. Only the attributes whose value moved are reported, and each of their clusters advances its DataVersion once per notification; a quieter ("Q") attribute's change advances the version at once and its report goes through its throttle; a notification that moved nothing advances and reports nothing. A notification that arrives while a command runs against the cluster, or while a controller's write is applied to the endpoint, is held and counted with that command or write. A root server's notifier is not compared, and is held for a command only: its fire advances the cluster's version and marks every attribute the server lists as reportable (`bridge/subscribe.go`, `wireMeasurementNotifier`, `wireRootNotifiers`; `bridge/write_hold.go`) | **silent**: subscriptions carry only empty keep-alives, and Apple Home shows the sensor as not responding |

### Measurements and classification

| Interface | Implemented by | What it does | When absent |
| --- | --- | --- | --- |
| `MeasurementSource` | a host sensor | its measurement class; the assembler picks the cluster and the device type | the source is no measurement |
| `FloatMeasurementSource`, `BoolMeasurementSource` | a host sensor | the reading, and whether one was observed yet | — |
| `ElectricalReadings` | a host meter | the consolidated electrical readings | — |
| `MeasurementMaterializer` | a host (with `RegisterMeasurementKind`) | the servers of a measurement kind the module does not ship | — |
| `EligibilitySource` | a host model | whether and how fully a data point maps to Matter | the classifier treats it as unmappable |
| `SwitchEventEmitter` | the module (`cluster/switch.Server`) | the press gestures a host narrates (InitialPress, ShortRelease, LongPress, LongRelease); handed to a source with `WireMatterSwitchHandler` | — |
| `SwitchPositionSink` | the module (`cluster/switch.Server`) | the positions a host reports (`SetRawPosition` debounced, `SetCurrentPosition`); the server derives every Switch event, multi press included, as matter.js `SwitchServer.ts` does; handed to a source with `WireMatterSwitchPositions` | a source without either method reports no press |

### Radios

A device-side host that commissions over Bluetooth LE and joins Wi-Fi
supplies its radios; what goes on the air — the BTP session, the
advertisement's bytes and schedule, when the node is on the radio — stays
in the module.

| Interface | Implemented by | What it does | When absent |
| --- | --- | --- | --- |
| `GATTPeripheral` (`GATTService`, `BLEAdvertisement`, `BLEConnection`) | the host's BLE stack: `bletest.Peripheral.Radio` in tests; `ble/bluez.NewPeripheral` on Linux implements the deprecated `BLEPeripheral` until its next release | registers the Matter GATT service (C1, C2, C3) before the node advertises — a refusal is `StartBLE`'s error —, advertises the payload the node gives and hands each central's connection to the node | no BLE: the node is commissionable over IP only and its QR code says so |
| `WiFiDriver` | the host's Wi-Fi stack: `wifi/wpasupplicant.New` on Linux, the reference daemon's `--wifi-sim` in tests | scans and joins the network a commissioner provisions | NetworkCommissioning serves the Ethernet feature |
| `WiFiInterfaceSwitcher` (optional on a `WiFiDriver`) | a host Wi-Fi stack that can switch its interface on and off | switches the interface a controller's write of `InterfaceEnabled` names; the attribute takes the value once the driver switched | `InterfaceEnabled` stays true: a write of false answers `INVALID_ACTION` |

*Source: `contract/doc.go` (package doc), `contract/ble.go`, `contract/wifi.go`, `contract/matter.go` (`EndpointSource`, `ClusterServer`, `MeasurementMaterializer`, `RegisterMeasurementKind`, `MeasurementSource`, `ChangeNotifier`, `AttributeChangeNotifier`), `contract/switchevents.go` (`SwitchEventEmitter`, `SwitchPositionSink`).*

## `node.Config`: the facade's ports

| Field | What it does | When nil or zero |
| --- | --- | --- |
| `Store` | `Path` (a SQLite file the node opens and closes), `DB`, or both `Credentials` and `Endpoints` | `New` refuses the configuration |
| `Advertiser` | publishes the node over DNS-SD | the node's own `mdns.Responder`, shaped by `Network.MDNS`; with `Network.MDNS.Off` it is `mdns.NewNoop()`: nothing is discoverable, and the bridge logs `matter.mdns.commissioning_not_advertised` |
| `Attestation` | DAC, PAI and Certification Declaration; `Files` names them on disk and `New` loads them | the CSA test chain and test CD: pairable everywhere, not shippable |
| `Health` | the listener probe on the node's clock; `Node.Health` returns the last verdict | no probe |
| `OTA` (`OTAConfig`) | mounts the OTA Software Update Requestor; `Applier` carries out an update; `TransferProtocols` adds the HTTPS download, whose GET goes through `HTTPClient` | no requestor. A nil `Applier` is the facade's own, which validates the image and keeps it under `Dir/applied`. Empty `TransferProtocols` announces BDX alone, and a list without `OTADownloadBDXSynchronous` fails the build; a nil `HTTPClient` is the module's client, which follows a redirect only to an `https://` target; a host's client keeps its own redirect policy |
| `OTAProvider` | serves OTA images over BDX from an OtaProvider endpoint | no provider (also with a nil `ImageSource`). A nil `Consent` declines an image that needs consent; a nil `ApplyPolicy` proceeds at once |
| `CommissionerControl` | mounts CommissionerControl on the Aggregator; `Approver` and `Commissioner` are required | no server |
| `Network.GATT` | the node's Bluetooth LE radio (`contract.GATTPeripheral`; the deprecated `Network.BLE` takes a `contract.BLEPeripheral`): while the node has no fabric its own window is advertised over BLE and PASE is served over BTP; the QR code carries the BLE discovery bit ([Operations](operations.md#ble-commissioning)) | no BLE; the node never touches a radio |
| `Network.WiFi.Driver` | the node's Wi-Fi radio (`contract.WiFiDriver`): mounts NetworkCommissioning with the Wi-Fi feature in place of the Ethernet one; the committed network is kept in the node's store and rejoined at `Start` and, with a backoff, whenever a join left the node off it, and a successful join re-announces the node's DNS-SD records at once ([Operations](operations.md#wi-fi-provisioning)) | NetworkCommissioning serves the Ethernet feature |
| `Network.WiFi.Reporter` | mounts WiFiNetworkDiagnostics and states the RootNode condition WiFi | no Wi-Fi diagnostics. EthernetNetworkDiagnostics is always mounted, with every link value null. A `Network.WiFi` with neither `Driver` nor `Reporter` is refused |
| `Client` | the node's client stack, which the Binding entries reach other nodes through | no client stack. A Binding entry that names a node is dropped |
| `TimeSynchronization.Config` (`PlatformTime`, `NTPClient`, `PeerReader`) | the host clock as a time source, the NTP query the time client falls back to, the read of the trusted time source's node | the host clock is the source (MixedNTP, milliseconds) while past the Matter epoch; no NTP fallback (TimeFailure); the node's own reader over `Client`, none without it ([Time synchronization](../getting-started/device.md#time-synchronization)) |
| `EndpointState` | scene tables and bridged NodeLabels, kept by the host; a refused save answers the controller FAILURE | the facade's own, in the settings table (`EndpointPersistence` is deprecated) |
| `Groups` | the group state, for a host with its own assembler | the facade builds one over the credential store |
| `Snapshotter` | the host's own `bridge.Snapshotter`. The topology it returns comes from an `endpoint.Assembler` over the host's `endpoint.Spec`s; `Endpoint` and `Topology` are read-only to a host | the facade's Aggregator under one scope |
| `AggregatorServers` | extra servers on the Aggregator (an Actions server, say) | none |
| `Observers` | the host's callbacks on fabric, commissioning and window events; `Node.Observe` (inline) and `Node.ObserveAsync` (queued) register more on a running node, each after these, in registration order, until its unsubscribe | nothing observes. An observer's panic is logged, never returned |
| `Metrics` | the node's named counters and gauges ([Observability](observability.md)); `node/metrics/expvar` publishes them on expvar | nothing recorded |
| `Diagnostics` | the capacity of the diagnostic trace (`Node.DiagnosticEvents`) | 256 events |
| `Logger` | the node's logger; the attribute keys and event names are in [Observability](observability.md) | `slog.Default()` |

Ports the facade leaves to the host are reached through `Node.Root()` and
`Node.Bridge()`:

- the GeneralDiagnostics TestEventTrigger enable key;
- the PowerSource endpoints of PowerSourceConfiguration;
- the bridge's `SetOnReassembled` hook.

A fabric's installation (AddNOC) reaches the host through
`Observers.OnFabricAdded`; the bridge has no hook for it.

Setting a cluster hook that the facade wires itself, such as
OperationalCredentials `SetOnFabricRemoved`, replaces the facade's wiring.
Use `Observers` instead.

*Source: `node/config.go` (`Config`, `Network`, `Store`, `Attestation`, `OTAConfig`), `node/client.go` (`Client`), `node/ota_provider.go` (`OTAProviderConfig`), `node/commissioner_control.go` (`CommissionerControlConfig`), `node/observers.go` (`Observers`), `node/persist.go` (`EndpointPersistence`), `node/root.go` (`buildRootOptional`, `unknownEthernet`), `node/doc.go` (package doc), `bridge/bridge.go` (`Bridge.Stop`).*

## `controller.Config`: the controller's ports added in v0.13.0

| Field | What it does | When nil or zero |
| --- | --- | --- |
| `OTA` (`controller.OTAConfig`) | the OTA push: the update manager (`Controller.SoftwareUpdates`) over an image store (`Images`, nil is an in-memory one without ledgers), with the OTA provider served at `controller.OTAProviderEndpoint` of the controller's own tree and its BDX responder (`Controller.BDX`) | no OTA push, no provider endpoint |
| `Camera` (`controller.CameraConfig`) | the controller side of Matter cameras: the Camera Controller endpoint (`controller.CameraControllerEndpoint`) with a WebRtcTransportRequestor the fabric's cameras reach (`Controller.CameraRequestor`, `Controller.OnCameraSignal`), and the operational record of RootNodeID | no camera endpoint |
| `GroupTransport` (`groups.MulticastTransport`) | the transport group messages leave on (`Controller.Groups`, `Controller.SetGroupKey`) | the controller's own UDP socket |
| `TCP` (`controller.TCPConfig{Incoming, Outgoing, MaxInboundConnections}`) | a TCP listener (`Controller.TCPAddr`), holding at most `MaxInboundConnections` connections (zero is 256), and the TCP initiator | UDP only |
| `TransportPreference` | the transport each peer prefers | UDP |
| `PeerTiming` (`*client.Timing`) | the peer set's connection timing, overriding the defaults field-wise (a zero field keeps its default; a negative `ProvenAddressRetention` retains nothing) | `client/peer.DefaultTiming` |
| `Networks`, `OwnNetworkProfile` | the network profiles' defaults and the profile our own MRP margin comes from | matter.js's defaults (`peer.DefaultTemplates`); `fast` |
| `Advertise`, `Advertiser` | the `_matterd._udp` commissioner record and the advertiser publishing it | no record; `mdns.NewResponder()` when `Advertise` is set |
| `Diagnostics` (`diag.Diagnostics{Capacity}`) | the size of the diagnostic trace (`Controller.DiagnosticEvents`, `Controller.OnDiagnosticEvent`) | 256 events |
| `Metrics` (`contract.Metrics`) | the named counters and gauges (`controller.Metric…`) | nothing recorded |
| `MaxReportBytesPerSession` | the bound on the report chunks a session's interactions with a paired node (the commissioning steps' included) hold while the rest arrive (`interaction.ErrReportTooLarge` past it; [limits](limits.md)); negative is `controller.ErrConfiguration` | `interaction.DefaultMaxReportBytesPerSession` (16 MiB) |
| `MaxImageBytesPerNode` | the bound on what one paired node's reports leave in the controller's image of it and in `DB` — the image is charged its values' stored bytes and what each endpoint, cluster, attribute and TLV element holds in memory: a report that would raise the image above it is not applied, an image stored above it is cut back to what the node's structure names when it loads ([limits](limits.md)); negative is `controller.ErrConfiguration` | `client.DefaultMaxImageBytesPerNode` (32 MiB) |

`controller/commissioner.Context.Scanners` (`CommissionedScanner`) are the
scanners told to forget a device once it is commissioned; nil is none.

## `bridge`: the `Attach…` and `Set…` ports

The facade attaches all of these. A host that drives `bridge/` directly
attaches them itself, in this order. `bridge.New` installs a no-op for each
port that is not attached.

### Before `Start`

These are read once at `Start`, or feed a component `Start` builds.

| Port | Failure mode | What a skip costs |
| --- | --- | --- |
| `AttachACLLister` | fails closed | every operational (CASE) request is denied with UnsupportedAccess. `endpoint.UnenforcedACL` is the explicit way to run without an ACL |
| `AttachSessionLookup` | silent | every encrypted datagram is dropped. CASE completes and carries no traffic |
| `AttachPaseHandlerProvider` (or `AttachPaseHandler`) | fails closed | commissioning cannot begin. Prefer the provider: the singleton serialises commissioners |
| `AttachCaseHandlerProvider` (or `AttachCaseHandler`) | fails closed | no CASE. A singleton (the deprecated `AttachCaseHandler`) rejects the parallel CASE sessions Apple Home opens, and holds one awaited resumption: another exchange's resumption, between a peer's Sigma2_Resume and its Success, force-closes that peer's awaiting resumed session. The provider gives each exchange its own handler |
| `AttachAckTracker` | silent | no standalone ACKs and no retransmission of the bridge's own messages; every reply is best-effort. A peer's acknowledgement never discharges an acknowledgement the bridge owes; `AttachAckHandler` is a separate port that is only told of the peer's acknowledgements |
| `AttachSubscriptionManager` | silent | a Subscribe returns its initial report with no subscription id, and no report follows. The attach is the manager's whole report wiring: it installs the bridge's report sender (`subscription.Manager.SetReportSender`), and a manager with neither a sender nor a deprecated hook refuses every subscription (`subscription.ErrNoReportSender`), which the bridge answers FAILURE |
| `AttachSessionRegistry` | silent | CloseSession closes nothing, `Stop` sends no farewell, the re-announce trigger is not installed, and the PASE sessions are not cleared when a commissioning ends. The bridge clears them itself: after a CommissioningComplete that `core.GeneralCommissioning` answered OK, when that server's fail-safe expires (`AttachRootClusters` wires `Bridge.FailSafeExpired` into it, beside the host's `SetOnFailSafeExpired` hook) and when the attached commissioning window is revoked. A host with a GeneralCommissioning server of its own calls `Bridge.FailSafeExpired(ctx)` when its fail-safe expires |
| a session table without `AttestationChallengeFor(sessionID uint16) ([]byte, bool)` | fails closed | AttestationRequest, CSRRequest and SignVIDVerificationRequest answer FAILURE: the bridge reads the invoking session's attestation challenge from the registry or, failing that, the session lookup, and the server signs over nothing else. `operational.Manager` has the method |
| `AttachSubscriptionStore` | silent | nothing survives a restart; controllers wait out their liveness timeout. `SetSubscriptionPersistence(false)` switches persistence off on purpose |

### Cluster surface

Attaching republishes onto the live topology immediately, as well as on
the next `Reassemble`.

| Port | Failure mode | What a skip costs |
| --- | --- | --- |
| `AttachRootClusters`, then `AttachAggregatorClusters` | silent for wildcards | a wildcard read skips the empty endpoint, so the bridge looks present and empty |
| `AttachRootPartsListProvider`, `AttachAggregatorPartsListProvider` | silent | must follow their cluster call. They return false when no Descriptor is mounted, and PartsList stays frozen |
| `AttachCommissioningWindow` | silent; fails open on ArmFailSafe | wired into the root's GeneralCommissioning whichever of it and `AttachRootClusters` comes second. Without it the window stays open after a commissioning completes, until its timeout; ArmFailSafe over CASE is not answered BusyWithOtherAdmin while a window is open; a revoke closes the PASE sessions only through a host-wired `PaseSessionCloser`; WindowStatus, AdminFabricIndex and AdminVendorId changes reach no subscriber |

### Group communication

| Port | Failure mode | What a skip costs |
| --- | --- | --- |
| `AttachGroupMessaging` | fails closed, silently | every group message is dropped and no multicast group is joined |
| `AttachAuxiliaryACL` | fails closed, silently | auxiliary ACL entries are listed but not enforced, and group commands relying on them are dropped |

### Optional

| Port | What a skip costs |
| --- | --- |
| `AttachCaseInitiatorProvider`, `AttachOperationalResolver` (+ `ReestablishFormerSubscriptions`) | former subscriptions are dropped at restart (silent) |
| `AttachBDXResponder` | BDX exchanges are dropped; an OTA download fails (fails closed) |
| `AttachDiagnosticEvents` | `Bridge.DiagnosticEvents` stays empty |
| `AttachMetrics` | silent: the named counters and gauges are recorded nowhere |
| `SetSubscriptionPersistence` | on by default; off, no subscription is recorded or re-established after a restart |
| `SetICDAdvertisement`, `SetLocalFixedMRPBackoff`, `SetOnInboundActivity` | an ICD only, silent: the ICD's record and MRP padding stay those of a non-ICD node, and inbound traffic does not extend its active window |
| `SetOnReassembled`, `SetOnFabricRemoved` | nil-safe observer hooks. A fabric's installation is observed on the node (`node.Observers.OnFabricAdded`) |
| `Config.BLE`, then `StartBLE` / `WithdrawBLE` / `StopBLE` | no BLE: StartBLE refuses and the bridge never touches a radio. The facade starts BLE with the own window of a node without a fabric, withdraws the advertisement when the window closes and stops BLE when the node is commissioned |
| `SetDeviceTypeValidation` | warn mode: each new violation is logged once; strict refuses what an addition causes; off judges only singleton placement |

Strict refuses only what an addition causes. Each `Start` and `Reassemble`
is judged as a set of changes to the topology validated before: an endpoint
number the last validation did not hold is an addition, and strict refuses a
new violation on the added endpoint, its descendants, its ancestors or its
siblings. A new violation that a change of an installed endpoint left behind —
a changed device type, a removed part — is logged and refuses nothing, as
matter.js refuses only at construction. A misplaced singleton is refused in
every mode, the first misplacing endpoint in tree order alone. A mandatory
command a server leaves unimplemented is `endpoint.ViolationUnimplemented`:
logged in warn and off, refused in strict. A mode that is none of the three
fails `node.New`. A node that commissions over BLE (`node.Network.BLE`,
`endpoint.Topology.CommissionsOverBLE`) does not hold the CustomNetworkConfig
condition, so its RootNode requires NetworkCommissioning, as matter.js
derives the condition (`ServerEndpointFacts.ts:135-144` at `db021da0`).

*Source: the `bridge` package doc (`bridge/bridge.go`), `bridge/bridge.go` (`AttachCommissioningWindow`, `wireCommissioned`), `bridge/securechannel.go` (`AttachSessionRegistry`), `bridge/ackpump.go`, `endpoint/devicetype_validation.go` (`assembledNodeConditions`).*

### Capabilities the bridge looks for on what is attached

Beside the ports, the bridge asks the session registry, a cluster server
and an endpoint's measurement source for optional methods. It finds them
by type assertion — against the contract's switch-source ports, and for
the rest against interfaces that are not exported: a host with its own
registry or source implements the method with exactly this signature, or
the bridge silently does without.
`secure/operational.Manager` has every registry method.

| On | Method | Without it |
| --- | --- | --- |
| the `AttachSessionRegistry` registry | `SetGracefulCloseNotifier(fn func(sessionID uint16, sess *channel.Session))` | the registry is not handed the bridge's CloseSession sender; a session it closes gracefully ends without the report to its peer |
| the registry | `SetReannounceTrigger(fn func(fabricIndex uint8))` | the registry cannot ask for the operational record to be announced again when a peer's last session is gone |
| the registry | `SetOnPaseSessionClose(fn func(sessionID uint16))` | the commissionable record is not announced again when a PASE session closes |
| the registry | `Active() int` | the `sessions.open` gauge of `AttachMetrics` is not recorded |
| the registry | `SessionIDsForPeer(fabricIndex uint8, peerNodeID uint64) []uint16` | the bridge finds no live session to a peer it wants to reach and establishes a new one each time |
| the registry | `Sessions() []operational.SessionInfo` | the bridge cannot tell which sessions are PASE: none is cleared after CommissioningComplete, when the fail-safe expires or when the window is revoked |
| the registry | `CloseFabric(fabricIndex uint8)` | a removed fabric's sessions are not closed by the bridge |
| the registry | `CloseFabricExcept(fabricIndex uint8, exceptSessionID uint16)` | a fabric's other sessions are not closed while the session that carries the command stays open to answer it |
| the registry | `PASESessionIDs() []uint16` | the bridge sees no established PASE session, so a new PASE is accepted while one is established (fails open) |
| the registry | `SetReapExemption(fn func(sessionID uint16) bool)` | a session that carries a live subscription can be reaped as idle (silent) |
| the registry, or the `AttachSessionLookup` table | `AdoptFabricIndex(sessionID uint16, fabricIndex uint8) error` | the PASE session an AddNOC succeeded on stays without a fabric, so a CommissioningComplete sent over it is refused with UnsupportedAccess (fails closed) |
| the registry | `ClosePeerLostSince(fabricIndex uint8, peerNodeID uint64, sinceMono time.Duration) int` | when the bridge gives up on a message of an exchange it opened, only that session is closed, not the lost peer's other sessions (silent) |
| a host's own PASE or CASE handler | `PeerSessionParams() *sessionparams.Parameters` | the handshake's replies retransmit at the default MRP intervals, not at the ones the initiator stated |
| a PASE or CASE handler | `establishedSession() (sessionID uint16, ok bool)` — unexported, so only the module's `PaseAdapter` and `CaseAdapter` have it | the session the handshake established is bound to the connection or address it came over only with its first message, and the `sessions.open` gauge is not updated when the handshake ends |
| a cluster server that is a `contract.EventReceiver` | `SetEndpoint(uint16)` | the server is not told the endpoint it is mounted on, which its events carry |
| an endpoint's `Measurement` | `contract.SwitchGestureSource` (`WireMatterSwitchHandler(contract.SwitchEventEmitter) func()`) | the source's press gestures do not reach the Switch server |
| an endpoint's `Measurement` | `contract.SwitchPositionSource` (`WireMatterSwitchPositions(contract.SwitchPositionSink) func()`) | the source's positions do not reach the Switch server |

A cluster server's state of a removed fabric goes through one port,
`contract.FabricScopedState`: the node calls it on every server of every
endpoint; a host that runs the bridge without the node has the bridge's
fabric removal call it on the servers of the bridged endpoints only — the
endpoints present at the removal. The root's and the Aggregator's servers
(AccessControl, ICDManagement, TimeSynchronization,
OTASoftwareUpdateRequestor, Messages, Binding among them), an endpoint
absent at the removal, and the drop before a reused fabric index is
announced again are the node's (`node/fabric_state.go`); without the node
they are the host's.

*Source: `bridge/securechannel.go` (`gracefulCloseNotifierSetter`, `reannounceTriggerSetter`, `paseCloseNotifierSetter`, `AttachSessionRegistry`, `paseSessionIDLister`, `peerSessionParamsSource`), `bridge/conn.go` (`reapExemptionSetter`, `establishedSessionReporter`), `bridge/receive_dispatch.go` (`sessionFabricAdopter`), `bridge/ota_provider_link.go` (`peerLossCloser`), `bridge/metrics.go` (`sessionCounter`), `bridge/case_initiator.go` (`peerSessionLister`), `bridge/pase_close.go` (`sessionLister`), `bridge/bridge.go` (`fabricSessionCloser`, `fabricSessionExceptCloser`, `matterEndpointReceiver`, `matterSwitchSubscribable`, `matterSwitchPositionReporter`).*

## `<cluster>def.Host`: the kit server's host methods

A port is what the kit's server calls on the host. `Host` is the other
direction: the methods of the kit's server that the host calls and the
typed server's `Set`, `Get`, `OnChanged`, `OnWrite`, `MarkChanged`,
`Handle<Command>` and `Emit<Event>` do not reach — a state machine's
input, a change with its own rule, state no attribute carries. The
cluster's typed server returns them: `d.EnergyEvse().Host()`. `Host()` is
nil when the mounted server does not implement the interface, as a host's
own server (`Declare.HostServer`) normally does not; the kit's server
does, also when the host mounts it itself. A cluster whose kit server has
no such method has no `Host`. A `Host` may embed a `contract` interface
the kit server implements for host inputs: the switch's.

| Cluster | `Host` |
| --- | --- |
| AmbientContextSensing | `AddDetection`, `SetObjectCount`, `Close` (stops the hold-time timer) — [Ambient context sensing](sensing.md) |
| Binding | `Entries`, `OnChange` (per fabric, the entries gained and lost), in `bindingdef.TargetStruct` |
| BooleanStateConfiguration | `SetSensorFault` (the event on every call, the attribute when served) |
| CameraAvSettingsUserLevelManagement | `AddMoveCapableVideoStream`, `UpdateMoveCapableVideoStream`, `UpdateMoveCapableVideoStreams`, `RemoveMoveCapableVideoStream`, `PhysicalMovementComplete` |
| CameraAvStreamManagement | `UpdateVideoStreamRefCount`, `UpdateAudioStreamRefCount`, `UpdateSnapshotStreamRefCount`, `ReusableVideoStreamID`, `ReusableSnapshotStreamID`, `ResourceAvailable` |
| Chime | `SoundsChanged` |
| CommodityTariff | `SyncTime` (the poll the server runs itself with a `SyncInterval`) |
| DeviceEnergyManagement | `Close` (stops its timers) |
| JointFabricAdministrator | `PeerAdministratorEndpoint`, `VIDVerifiedFabric` — [Joint Fabric](joint-fabric.md) |
| JointFabricDatastore | the anchor and status setters, `AnchorFabricIndex`, the node, key set, admin, group, endpoint, binding and ACL operations (each pushing through the `DatastoreSync` before it returns), `RefreshNode`, `ForceAddGroup`, `ForceAddNodeKeySetEntry`, `Wait` — [Joint Fabric](joint-fabric.md) |
| DishwasherAlarm, RefrigeratorAlarm, TemperatureAlarm | `ResetLatchedAlarms` |
| DoorLock | `SetDoorState` (DoorStateChange and the door counters), `Users` |
| EcosystemInformation | `DirectoryChanged` |
| EnergyEvse | `SetHardwareState`, `SetFault`, `SetMaxHardwareChargeCurrent`, `SetMaxHardwareDischargeCurrent`, `SetCableAssemblyLimit`, `SetNominalMainsVoltage`, `ReportRFID`, `DiagnosticsComplete`, their getters, `IsPluggedIn`, `Targets`, `Close` |
| PushAvStreamTransport | `TransportStarted`, `TransportStopped` (PushTransportBegin and PushTransportEnd) |
| ServiceArea | `RemoveSupportedAreasEntry`, `RemoveSupportedMapsEntry` (with what names the area or map) |
| SmokeCoAlarm | `Refresh` (for a `StateSource` that is no `contract.ChangeNotifier`) |
| Switch | `contract.SwitchPositionSink` (`SetRawPosition`, `SetCurrentPosition`) and `contract.SwitchEventEmitter` (`FireInitialPress` …), `RawPosition`, `SetDebounceDelay`, `SetLongPressDelay`, `SetMultiPressDelay`, `ResetState` |
| Thermostat | `SetMeasuredTemperature` (the host's measured temperature; LocalTemperatureCalibration applies before it is served as LocalTemperature), `SetOccupied` (OCC: the Occupancy attribute, and the setpoints in effect) |
| ThreadBorderRouterManagement, ThreadNetworkDiagnostics | `Refresh` (the host schedules it) |
| ThreadNetworkDirectory | `ReloadNetworks` |
| TlsCertificateManagement | `RootCertificate`, `ClientCertificate`, `SetDependencyChecker` |
| TlsClientManagement | `ProvisionedEndpoint`, `MutateEndpointReferenceCount` (an endpoint in use is not removed: EndpointInUse), `RootCertificateInUse`, `ClientCertificateInUse` |
| WebRtcTransportProvider | `UpsertSession`, `RemoveSession` (by the session id) |
| WebRtcTransportRequestor | `UpsertSession`, `RemoveSession` (by `webrtctransportrequestordef.SessionKey`) |
| WiFiNetworkManagement | `SetNetworkCredentials`, `ClearNetworkCredentials`, `Passphrase` |
| ZoneManagement | `ZoneTriggered`, `ZoneStopped` |

*Source: `script/clustergen/hosts.go`; the decision on every method of
every kit server is `device/host_methods_test.go`.*

## Optional interfaces of cluster ports

Some cluster servers take a host port and look for more capabilities on it
by type assertion.

| Interface | On | What it does | When absent |
| --- | --- | --- | --- |
| `levelcontroldef.NativeRamp` | the device declared as `levelcontroldef.Declare{Ramp: …}` (or behind `levelcontrol.NewNativeSource`) | ramps its level by itself; the source keeps Options and OnLevel and turns each command into one target and one duration | — (it is the port) |
| `levelcontroldef.NativeMover` | a `NativeRamp` | replaces the Move logic; a device for which a jump to the bound means nothing (a speaker's volume) keeps its level | the source ramps to the bound |
| `levelcontroldef.NativeSwitcher` | a `NativeRamp` | writes On/Off and level together (`SwitchOn(level, d)`, `SwitchOff(d)`); the source then leaves the OnOff attribute to the host | matter.js's order: switch on, then ramp; ramp down, then switch off |
| `levelcontroldef.NativePersister` | a `NativeRamp` | receives Options and OnLevel before a write takes effect (`PersistLevel`); an error refuses the write | the values live in memory |
| `camera.RequestorPort` (`webrtctransportrequestordef.RequestorPort`) | `webrtctransportrequestordef.Declare{RequestorPort: …}`, `camera.RequestorConfig.Port` | receives the Offer, Answer, IceCandidates and End of a session the peer holds; `RequestorRefusals` hears of each NOT_FOUND ([Camera](camera.md#the-signalling-model)) | the commands are checked and answered, the signalling dropped |
| `camera.ChimePort` (`chimedef.ChimePort`) | `chimedef.Declare{ChimePort: …}`, `camera.ChimeConfig.Port` | lists the installed sounds and plays one; an error answers with its status, FAILURE without one ([Camera](camera.md#chime)) | the generated server |
| `camera.MechanicalPTZ`, `camera.MechanicalPresets`, `camera.DigitalPTZ` | `cameraavsettingsuserlevelmanagementdef.Declare{MechanicalPTZ: …, DigitalPTZ: …}`, `camera.AvSettingsConfig` | start a validated movement (the host reports its end), store and remove presets, change a stream's viewport; an error answers with its status ([Camera](camera.md#pan-tilt-and-zoom)) | the generated server |
| `camera.VideoStreamPort`, `camera.AudioStreamPort`, `camera.SnapshotStreamPort`, `camera.SnapshotCapturePort` | `cameraavstreammanagementdef.Declare{VideoStreamPort: …, AudioStreamPort: …, SnapshotStreamPort: …, SnapshotCapturePort: …}`, `camera.AvStreamConfig` | allocate, modify and release a validated stream and return its id, start a stream as allocated, take a snapshot; an error answers with its status ([Camera](camera.md#the-stream-model)) | the generated server |
| `jointfabric.DatastoreSync` (`jointfabricdatastoredef.DatastoreSync`) | `jointfabricdatastoredef.Declare{Sync: …}`, `jointfabric.DatastoreConfig.Sync` | pushes an entry, or a node's binding or ACL list, to the member node and fetches its lists for a refresh; an error leaves the entry Pending ([Joint Fabric](joint-fabric.md)) | the generated server |
| `jointfabric.DatastoreStore` (`jointfabricdatastoredef.DatastoreStore`) | `jointfabricdatastoredef.Declare{Store: …}` | persists the tables as one settings record under the StableKey; the node's store is one | the tables stay in memory |
| `jointfabric.AdministratorPort` (`jointfabricadministratordef.AdministratorPort`) | `jointfabricadministratordef.Declare{AdministratorPort: …}`, `jointfabric.AdministratorConfig.Port` | the administrator key's ICAC CSR | the generated server |
| `jointfabric.CommissionerReach` (`jointfabricadministratordef.CommissionerReach`) | `jointfabricadministratordef.Declare{CommissionerReach: …}` | reads the announcing commissioner's administrator data and has it sign the VID verification; every method takes the `jfadef.Peer` (the invoking session's fabric index and node id) | the node's own over its client stack with `node.Config.Client`; without one AnnounceJointFabricAdministrator fails |
| `camera.ZonePort` (`zonemanagementdef.ZonePort`) | `zonemanagementdef.Declare{ZonePort: …}`, `camera.ZonesConfig.Port` | store a validated zone (and return its id) or trigger, remove one; an error answers with its status ([Camera](camera.md#the-zone-model)) | the generated server |
| `camera.ProviderPort` (`webrtctransportproviderdef.ProviderPort`) | `webrtctransportproviderdef.Declare{ProviderPort: …}`, `camera.ProviderConfig.Port` | answer a validated SolicitOffer or ProvideOffer with the session to store, take an answer, ICE candidates and the end of a session; an error answers with its status ([Camera](camera.md#the-transport-model)) | the generated server |
| `camera.PushTransportPort` (`pushavstreamtransportdef.PushTransportPort`) | `pushavstreamtransportdef.Declare{PushTransportPort: …}`, `camera.PushTransportConfig.Port` | allocate, modify, activate, trigger and release a validated push transport, report a busy one, the bandwidth and the CMAF session number ([Camera](camera.md#the-transport-model)) | the generated server |
| `pushavstreamtransportdef.TLSEndpoints`, `TLSCertificates` | `pushavstreamtransportdef.Declare{TLSEndpoints: …, TLSCertificates: …}` (the TLS servers' typed `Host`) | look up the TLS endpoint and its certificates a transport names | every allocation is InvalidTLSEndpoint; without certificates the transport has none |
| `tls.CertificatePort` (`tlscertificatemanagementdef.CertificatePort`) | `tls.CertificateManagementConfig.Port` | make a client key and its CSR, judge a certificate's validity and key, drop a removed key ([Camera](camera.md#the-transport-model)) | — (required) |
| `tls.Store` (`tlscertificatemanagementdef.CertificateStore`, `tlsclientmanagementdef.EndpointStore`) | `tls.CertificateManagementConfig.Store`, `tls.ClientManagementConfig.Store` (any `store.Settings`) | persist the certificate and endpoint tables | the tables stay in memory |
| `closurecontroldef.StopObserver` | a `closurecontroldef.Mover` | hears of a Stop the server ignores because the closure is not in motion (`StopIgnored`); an error answers with its status, FAILURE without one | the idle Stop is a SUCCESS no-op |
| `TriggerReannounce(ctx)` | the `Advertiser` (`mdns.Responder` has it) | `Node.TriggerReannounce`: one publication of every service per interface now, with the goodbyes of the records that left | `TriggerReannounce` is a no-op |
| `ServiceConnected`, `ServiceDisconnected`, `ServiceRetransmission` | the `Advertiser` (`mdns.Responder` has them) | matter.js's broadcast events: a CASE session ends the operational broadcast; a session teardown, a terminated subscription and an MRP retransmission resume it, for the session's fabric's operational service only | the advertiser's own announcements only |
| `mdns.Enabled` | the `Advertiser` | says whether it publishes at all | `mdns.IsEnabled` takes the advertiser at its word |
| `onoff.Sink` | `onoff.Config.Sink` | consents to every OnOff change before it is stored; an error answers with its status, FAILURE without one | changes are stored without asking |
| `thermo.SchedulePort` (`thermostatdef.SchedulePort`) | `thermostatdef.Declare{SchedulePort: …}`, `thermo.ThermostatConfig.SchedulePort` | with MatterScheduleConfiguration: switches the device to the schedule a SetActiveScheduleRequest named, and stores the schedules an atomic write committed; an error answers with its status, FAILURE without one ([Thermostat](thermostat.md#week-programs-msch)) | schedules live in memory |
| `threadbr.BorderRouter` (`threadborderroutermanagementdef.BorderRouter`) | `threadborderroutermanagementdef.Declare{BorderRouter: …}`, `threadbr.ManagementConfig.BorderRouter` | the Thread Border Router the ThreadBorderRouterManagement and ThreadNetworkDiagnostics servers manage and read: its name, agent id, Thread version and interface, the datasets, the activation that returns once attached, its commit and revert, the pending dataset, the diagnostics; `otbr.BorderRouter` adapts an OTBR's REST API ([Thread Border Router](thread-border-router.md)) | both clusters keep the generated server |
| `netinfra.ThreadNetworkStore` (`threadnetworkdirectorydef.ThreadNetworkStore`) | `threadnetworkdirectorydef.Declare{ThreadNetworkStore: …}`, `netinfra.ThreadDirectoryConfig.Store` | persists the ThreadNetworkDirectory's operational datasets and its PreferredExtendedPanID: its capacity, the stored datasets, a save after AddNetwork and a removal after RemoveNetwork passed chip's checks, the preferred network loaded at start and saved on every change; an error answers FAILURE ([Network infrastructure](network-infrastructure.md#threadnetworkdirectory-the-datasets)) | datasets and the preferred network live in memory, 32 networks |
| `thermo.Sink` (`thermostatdef.Sink`) | `thermostatdef.Declare{Sink: …}`, `thermo.ThermostatConfig.Sink` | consents to every SystemMode, setpoint and — when the hold is served — TemperatureSetpointHold change a controller asks for, before it is stored; an error answers with its status, FAILURE without one ([Thermostat](thermostat.md)) | changes are stored without asking |

### What a port hears of a controller's write

A controller's write reaches the host only when it changes the value. The
setters of a FanControl, PumpConfigurationAndControl and SmokeCoAlarm
(SmokeSensitivityLevel) port and LevelControl's `SetOptions` and
`SetOnLevel` are called only for a value that differs from the one the
host's state reads (`FanState`, `PumpState`, `SmokeCOState`, `Options`,
`OnLevel`); a generated server's `Sink` (`spec.Sink`) only for a value that
differs from the one the server holds; BridgedDeviceBasicInformation's
`OnNodeLabelWrite` only for a label that differs from the held one, in the
order the labels are stored. A write of the held value is answered SUCCESS:
it calls nothing, the server stores nothing, no DataVersion advances and
no subscriber is told (`contract.ChangeReportingWriter`, above). A write
that reaches a setter counts as a change whether or not the host's state
shows the value when the setter returns, so a host that applies a write
later is judged by what was written. A generated server's own write rule
(`spec.ServerConfig.WriteRule`) runs on every write, before the comparison
with the held value; the host's veto (`Server.SetBeforeWrite`, the typed
server's `OnWrite`) runs after the comparison, so a write of the held value
reaches no veto.

*Source: `contract/matter.go` (`ChangeReportingWriter`), `cluster/spec/server.go` (`MatterWriteCommit`, `holds`), `cluster/fan/fancontrol_server.go`, `cluster/pump/pump_server.go` and `cluster/alarm/smokecoalarm_server.go` (`MatterWriteChanged`), `cluster/levelcontrol/levelcontrol_server.go` (`writeOptions`, `writeOnLevel`), `cluster/core/bridged_device_basic_information.go` (`MatterWriteChanged`).*

Host ports of the root servers, reached through `node.Config` or
`Node.Root()`:

| Port | What it does |
| --- | --- |
| `core.WiFiReporter` | the Wi-Fi link values; the module ships no reporter |
| `core.OtaApplier` | the host's part of an OTA update. Before `ApplyUpdate` the requestor has checked the image's header syntax, that its payload has the size the header states, and its version (the offered one, or newer than the running one) — not the payload digest, not the header's vendor and product IDs, and no signature. Those are the applier's: the facade's own applier verifies the digest and compares vendor and product with the node's (`node/ota.go`) |
| `core.OtaHTTPClient` | the GET of an HTTPS OTA download; `*http.Client` implements it, so a host restricts egress there |
| `core.OTAImageSource`, `core.OTAConsent`, `core.OTAApplyPolicy` | the provider's catalogue, user consent, and apply decision |
| `core.CommissioningApprover`, `core.ReverseCommissioner` | CommissionerControl's approval, and the commissioning of the approved node |
| `core.ActionsHost` | an Actions server's lists and commands |
| `core.LogProvider` | DiagnosticLogs content. The facade serves its diagnostic event ring |

Clusters the generated server serves with no hand-written logic — among
them WaterTankLevelMonitoring, as matter.js serves it with its generated
behaviour stub — have no port beyond their `Declare`: its command handler fields (`<cluster>def.Declare{…}`, or the
typed server's setters at run time) are the host's part, and a mandatory
command without a handler is unimplemented, absent from
AcceptedCommandList. AccessControl, OperationalCredentials and
GroupKeyManagement have a definition but no `Declare`: their servers are
the hand-written `cluster/core` ones the node mounts.

*Source: `cluster/levelcontrol/native.go` (`NativeRamp`, `NativeMover`, `NativeSwitcher`), `mdns/responder.go` (`TriggerReannounce`, `ServiceConnected`), `node/reannounce.go`, `cluster/onoff/server.go` (`Sink`), `cluster/thermo/thermostat_server.go` (`Sink`), `cluster/core/wifi_network_diagnostics.go` (`WiFiReporter`), `cluster/core/ota_software_update_requestor.go` (`OtaApplier`), `cluster/core/ota_provider.go` (`OTAImageSource`, `OTAConsent`, `OTAApplyPolicy`), `cluster/core/commissioner_control.go` (`CommissioningApprover`, `ReverseCommissioner`), `cluster/core/actions.go` (`ActionsHost`), `cluster/core/diagnostic_logs.go` (`LogProvider`), `node/node.go` (`ringLogs`).*
