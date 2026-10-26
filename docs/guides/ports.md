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

What stays public below the facade, and what going around it costs, is
on [Advanced seams](advanced-seams.md).

*Source: `bridge/bridge.go:38-47`.*

## `contract/`: what a device and a server implement

`contract/` imports the standard library only. A host type that implements
these interfaces does not drag the host's dependencies into the module.

### Endpoints and cluster servers

| Interface | Implemented by | What it does | When absent |
| --- | --- | --- | --- |
| `EndpointSource` | a host device | device type and cluster servers of one bridged endpoint | the source is not an endpoint |
| `ClusterServer` | every server | read, write, invoke; the reportable attributes | required |
| `FabricScopedReader` | a server with fabric-scoped attributes | read with the FabricFiltered flag and the reading fabric | `MatterRead` answers with the full value |
| `ClusterAttributeLister` | a server | the attribute set, for wildcard reads and AttributeList | a wildcard read returns only the global attributes |
| `ClusterCommandLister` | a server | AcceptedCommandList and GeneratedCommandList | both lists are empty |
| `ClusterEventLister` | a server | EventList | EventList is empty |
| `ClusterDataVersion` | a server | the cluster's DataVersion | the IM layer stamps a constant 1 |
| `ClusterAttributeReadPrivilege` | a server | a read privilege above View | View |
| `ClusterAttributeWritePrivilege` | a server | a write privilege above Operate | Operate |
| `ClusterCommandInvokePrivilege` | a server | an invoke privilege above Operate | Operate |
| `TimedCommander` | a server | which commands need a timed interaction | the generated table (`schema.IsTimedInvoke`) decides |
| `ClusterQuiescer` | a server with its own timers | stops the timers when the server leaves the topology or the bridge stops | timers outlive the server's use |
| `EventReceiver` | a server that emits events | receives the bridge's `EventEmitter` at assembly | the server cannot emit events |

### Change reporting

| Interface | Implemented by | What it does | When absent |
| --- | --- | --- | --- |
| `AttributeChangeNotifier` | a server that holds its state | names exactly the attributes that changed; the bridge advances the DataVersion and reports only those | see `ChangeNotifier` |
| `SelfReportedAttributeLister` | an `AttributeChangeNotifier` | attributes it reports only itself (quieter "Q" attributes) | the before/after comparison after a command includes them |
| `ChangeNotifier` | a host source | "something changed": every reportable attribute of the cluster is marked dirty | **silent**: subscriptions carry only empty keep-alives, and Apple Home shows the sensor as not responding |

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

*Source: `contract/doc.go:4-18`, `contract/matter.go:21-260`, `contract/matter.go:299-441`, `contract/matter.go:573-871`, `contract/switchevents.go:26`, `contract/switchevents.go:54`.*

## `node.Config`: the facade's ports

| Field | What it does | When nil or zero |
| --- | --- | --- |
| `Store` | `Path` (a SQLite file the node opens and closes), `DB`, or both `Credentials` and `Endpoints` | `New` refuses the configuration |
| `Advertiser` | publishes the node over DNS-SD | the node's own `mdns.Zeroconf` with the subtype responder, shaped by `Network.MDNS`; with `Network.MDNS.Disabled` it is `mdns.NewNoop()`: nothing is discoverable, and the bridge logs `matter.mdns.commissioning_not_advertised` |
| `Attestation` | DAC, PAI and Certification Declaration; `Files` names them on disk and `New` loads them | the CSA test chain and test CD: pairable everywhere, not shippable |
| `Health` | the listener probe on the node's clock; `Node.Health` returns the last verdict | no probe |
| `OTA` (`OtaConfig`) | mounts the OTA Software Update Requestor; `Applier` carries out an update | no requestor. A nil `Applier` is the facade's own, which validates the image and keeps it under `Dir/applied` |
| `OTAProvider` | serves OTA images over BDX from an OtaProvider endpoint | no provider (also with a nil `ImageSource`). A nil `Consent` declines an image that needs consent; a nil `ApplyPolicy` proceeds at once |
| `CommissionerControl` | mounts CommissionerControl on the Aggregator; `Approver` and `Commissioner` are required | no server |
| `Network.WiFi.Reporter` | mounts WiFiNetworkDiagnostics and states the RootNode condition WiFi | no Wi-Fi diagnostics. EthernetNetworkDiagnostics is always mounted, with every link value null |
| `Client` | the node's client stack, which the Binding entries reach other nodes through | no client stack. A Binding entry that names a node is dropped |
| `EndpointPersistence` | scene tables and bridged NodeLabels, kept by the host | the facade's own, in the settings table |
| `Groups` | the group state, for a host with its own assembler | the facade builds one over the credential store |
| `Topology` | the host's own `bridge.Snapshotter` | the facade's Aggregator under one scope |
| `AggregatorServers` | extra servers on the Aggregator (an Actions server, say) | none |
| `Observers` | the host's callbacks on fabric, commissioning and window events; `Node.Observe` registers more on a running node, each after these, in registration order, until its unsubscribe | nothing observes. An observer's error or panic is logged, never returned |
| `Metrics` | the node's named counters and gauges ([Observability](observability.md)); `node/metrics/expvar` publishes them on expvar | nothing recorded |
| `Diagnostics` | the capacity of the diagnostic trace (`Node.DiagnosticEvents`) | 256 events |
| `Logger` | the node's logger; the attribute keys and event names are in [Observability](observability.md) | `slog.Default()` |

Ports the facade leaves to the host are reached through `Node.Root()` and
`Node.Bridge()`:

- the GeneralDiagnostics TestEventTrigger enable key;
- the PowerSource endpoints of PowerSourceConfiguration;
- the bridge's `SetOnReassembled` and `SetOnFabricAdded` hooks.

Setting a cluster hook that the facade wires itself, such as
OperationalCredentials `SetOnFabricRemoved`, replaces the facade's wiring.
Use `Observers` instead.

*Source: `node/config.go:37-205`, `node/config.go:221-281`, `node/config.go:367-415`, `node/client.go:29-51`, `node/ota_provider.go:25-39`, `node/commissioner_control.go:23-34`, `node/observers.go:4-71`, `node/persist.go:57-77`, `node/root.go:392-403`, `node/root.go:536-540`, `node/doc.go:49-64`, `bridge/bridge.go:2168-2180`.*

## `controller.Config`: the controller's ports added in v0.13.0

| Field | What it does | When nil or zero |
| --- | --- | --- |
| `OTA` (`controller.OTAConfig`) | the OTA push: the update manager (`Controller.SoftwareUpdates`) over an image store (`Images`, nil is an in-memory one without ledgers) and the controller's own node with the OTA provider | no OTA push, no own node |
| `OTA.StartNode` | starts the own node from a `controller.OwnNodeSpec` and returns it as a `controller.OwnNode` with its address: `node/controllernode.Start`, or a test's starter that runs it on its site (`fabrictest.Site.ControllerConfig` sets one) | required with `OTA`: `New` refuses with `ErrConfiguration` |
| `TCP` (`controller.TCPConfig{Incoming, Outgoing}`) | a TCP listener (`Controller.TCPAddr`) and the TCP initiator | UDP only |
| `TransportPreference` | the transport each peer prefers | UDP |
| `Networks`, `OwnNetworkProfile` | the network profiles' defaults and the profile our own MRP margin comes from | matter.js's defaults (`peer.DefaultTemplates`); `fast` |
| `Advertise`, `Advertiser` | the `_matterd._udp` commissioner record and the advertiser publishing it | no record; `mdns.NewZeroconf()` when `Advertise` is set |

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
| `AttachACLLister` | fails closed | every fabric-scoped request is denied with UnsupportedAccess. `endpoint.UnenforcedACL` is the explicit way to run without an ACL |
| `AttachSessionLookup` | silent | every encrypted datagram is dropped. CASE completes and carries no traffic |
| `AttachPaseHandlerProvider` (or `AttachPaseHandler`) | fails closed | commissioning cannot begin. Prefer the provider: the singleton serialises commissioners |
| `AttachCaseHandlerProvider` (or `AttachCaseHandler`) | fails closed | no CASE. A singleton rejects the parallel CASE sessions Apple Home opens |
| `AttachAckTracker` | silent | no standalone ACKs and no retransmission of the bridge's own messages; every reply is best-effort. It wraps the lower-level `AttachAckHandler` |
| `AttachSubscriptionManager` | silent | a Subscribe returns its initial report with no subscription id, and no report follows |
| `AttachSessionRegistry` | silent | CloseSession closes nothing, `Stop` sends no farewell, and the re-announce trigger is not installed |
| `AttachSubscriptionStore` | silent | nothing survives a restart; controllers wait out their liveness timeout. `SetSubscriptionPersistence(false)` switches persistence off on purpose |

### Cluster surface

Attaching republishes onto the live topology immediately, as well as on
the next `Reassemble`.

| Port | Failure mode | What a skip costs |
| --- | --- | --- |
| `AttachRootClusters`, then `AttachAggregatorClusters` | silent for wildcards | a wildcard read skips the empty endpoint, so the bridge looks present and empty |
| `AttachRootPartsListProvider`, `AttachAggregatorPartsListProvider` | silent | must follow their cluster call. They return false when no Descriptor is mounted, and PartsList stays frozen |

### Group communication

| Port | Failure mode | What a skip costs |
| --- | --- | --- |
| `AttachGroupMessaging` | fails closed, silently | every group message is dropped and no multicast group is joined |
| `AttachAuxiliaryACL` | fails closed, silently | auxiliary ACL entries are listed but not enforced, and group commands relying on them are dropped |

### Optional

| Port | What a skip costs |
| --- | --- |
| `AttachCommissioningWindow` | only `Bridge.CommissioningWindow()` returns nothing |
| `AttachCaseInitiatorProvider`, `AttachOperationalResolver` (+ `ReestablishFormerSubscriptions`) | former subscriptions are dropped at restart (silent) |
| `AttachBDXResponder` | BDX exchanges are dropped; an OTA download fails (fails closed) |
| `AttachDiagnosticEvents` | `Bridge.DiagnosticEvents` stays empty |
| `SetOnReassembled`, `SetOnFabricAdded`, `SetOnFabricRemoved` | nil-safe observer hooks |
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
fails `node.New`.

*Source: `bridge/bridge.go:29-177`, `bridge/securechannel.go:242-262`, `bridge/ackpump.go:36-58`.*

## Optional interfaces of cluster ports

Some cluster servers take a host port and look for more capabilities on it
by type assertion.

| Interface | On | What it does | When absent |
| --- | --- | --- | --- |
| `levelcontroldef.NativeRamp` | the device declared as `levelcontroldef.Declare{Ramp: …}` (or behind `levelcontrol.NewNativeSource`) | ramps its level by itself; the source keeps Options and OnLevel and turns each command into one target and one duration | — (it is the port) |
| `levelcontroldef.NativeMover` | a `NativeRamp` | replaces the Move logic; a device for which a jump to the bound means nothing (a speaker's volume) keeps its level | the source ramps to the bound |
| `levelcontroldef.NativeSwitcher` | a `NativeRamp` | writes On/Off and level together (`SwitchOn(level, d)`, `SwitchOff(d)`); the source then leaves the OnOff attribute to the host | matter.js's order: switch on, then ramp; ramp down, then switch off |
| `levelcontroldef.NativePersister` | a `NativeRamp` | receives Options and OnLevel before a write takes effect (`PersistLevel`); an error refuses the write | the values live in memory |
| `closurecontroldef.StopObserver` | a `closurecontroldef.Mover` | hears of a Stop the server ignores because the closure is not in motion (`StopIgnored`); an error answers FAILURE | the idle Stop is a SUCCESS no-op |
| `mdns.Reannouncer` | the `Advertiser` | the periodic re-announce loop (30 min default) and `Node.TriggerReannounce` | no loop; `TriggerReannounce` is a no-op |
| `mdns.Enabled` | the `Advertiser` | says whether it publishes at all | `mdns.IsEnabled` takes the advertiser at its word |
| `onoff.Sink` | `onoff.Config.Sink` | consents to every OnOff change before it is stored; an error answers FAILURE | changes are stored without asking |

Host ports of the root servers, reached through `node.Config` or
`Node.Root()`:

| Port | What it does |
| --- | --- |
| `core.WiFiReporter` | the Wi-Fi link values; the module ships no reporter |
| `core.OtaApplier` | the host's part of an OTA update |
| `core.OTAImageSource`, `core.OTAConsent`, `core.OTAApplyPolicy` | the provider's catalogue, user consent, and apply decision |
| `core.CommissioningApprover`, `core.ReverseCommissioner` | CommissionerControl's approval, and the commissioning of the approved node |
| `core.ActionsHost` | an Actions server's lists and commands |
| `core.LogProvider` | DiagnosticLogs content. The facade serves its diagnostic event ring |

Clusters the generated server serves with no hand-written logic — among
them the camera, Thread, Joint Fabric and TLS clusters, as matter.js serves
them with its generated behaviour stubs — have no port beyond their
`Declare`: its command handler fields (`<cluster>def.Declare{…}`, or the
typed server's setters at run time) are the host's part, and a mandatory
command without a handler is unimplemented, absent from
AcceptedCommandList. AccessControl, OperationalCredentials and
GroupKeyManagement have a definition but no `Declare`: their servers are
the hand-written `cluster/core` ones the node mounts.

*Source: `cluster/levelcontrol/native.go:18-73`, `mdns/reannounce.go:15-55`, `node/reannounce.go:15-50`, `node/config.go:237-256`, `cluster/onoff/server.go:55-73`, `cluster/core/wifi_network_diagnostics.go:26-28`, `cluster/core/ota_software_update_requestor.go:180`, `cluster/core/ota_provider.go:96-138`, `cluster/core/commissioner_control.go:73-111`, `cluster/core/actions.go:63`, `cluster/core/diagnostic_logs.go:62-67`, `node/node.go:909-915`.*
