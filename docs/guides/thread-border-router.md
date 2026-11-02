# Thread Border Router

A Thread Border Router endpoint (device type `0x0091`) lets a controller
form or join a Thread network through a border router the host already
runs: an OpenThread Border Router (`ot-br-posix`), typically a Raspberry
Pi with a radio co-processor. The module never joins the Thread mesh
itself ([ADR 0027](https://github.com/SukramJ/go-fabric/blob/main/docs/adr/0027-thread-border-router-over-otbr-rest.md)).
The REST link is plaintext HTTP, as ot-br-posix serves it: the operational
dataset, network key included, crosses it in clear, so keep the border
router on the host's own trusted LAN segment.
It manages the border router over its REST API, by default on port 8081.

Three servers serve the endpoint, all over one border router:

| Cluster | Server | Rules from |
| --- | --- | --- |
| ThreadBorderRouterManagement (`0x0452`) | `threadbr.ManagementServer` | connectedhomeip `ThreadBorderRouterManagementCluster.cpp` at `6170af84` |
| ThreadNetworkDiagnostics (`0x0035`) | `threadbr.DiagnosticsServer` | connectedhomeip `DirectThreadNetworkDiagnosticsProvider.cpp` |
| NetworkCommissioning, Thread feature (`0x0031`) | `core.ThreadNetworkCommissioning` | connectedhomeip `GenericNetworkCommissioningThreadDriver.cpp` |

matter.js serves the first two as generated stubs with no logic, so both
follow connectedhomeip. ThreadBorderRouterManagement is an entry of the
*Ahead of matter.js* register.

## The pieces

| Package | Layer | Role |
| --- | --- | --- |
| [`thread`](https://github.com/SukramJ/go-fabric/tree/main/thread) | foundation | the operational dataset codec after matter.js (`DecodeDataset`), chip's dataset view (`DatasetView`), the diagnostics records |
| [`thread/otbr`](https://github.com/SukramJ/go-fabric/tree/main/thread/otbr) | support | the REST client after matter.js `OtbrRestClient.ts`, and `otbr.BorderRouter`, which adapts it to the servers' port |
| [`thread/otbr/otbrtest`](https://github.com/SukramJ/go-fabric/tree/main/thread/otbr/otbrtest) | support | an in-process fake border router for tests |
| [`cluster/threadbr`](https://github.com/SukramJ/go-fabric/tree/main/cluster/threadbr) | servers | the management and diagnostics servers and the `BorderRouter` port |

The port is `threadbr.BorderRouter`, generated as
`threadborderroutermanagementdef.BorderRouter`. It is chip's
`ThreadBorderRouterManagementDelegate` in Go. A host with its own border
router (another REST API, an in-process stack) implements it directly.

## Wiring it

```go
c, err := otbr.NewClient(otbr.Config{Host: "fd00::1"})
br, err := otbr.NewBorderRouter(c, otbr.BorderRouterConfig{
	Name:          "Living room border router", // BorderRouterName, 1 to 63 bytes
	ThreadVersion: 5,                           // Thread 1.4, when the border router reports none
})
d, err := device.NewThreadBorderRouter(device.Options{StableKey: device.StringKey("otbr"), Name: "Thread Border Router"},
	threadborderroutermanagementdef.Declare{BorderRouter: br, Features: threadborderroutermanagementdef.FeaturePanChange},
	threadnetworkdiagnosticsdef.Declare{Features: threadbr.DiagnosticsFeatures},
)
```

The kit mounts the management server on the declared border router and the
diagnostics server on the same one. Without a `BorderRouter`, both clusters
keep the generated server. The border router must answer when the device is
built, because the server reads its agent id and Thread version then.

The node wires the management server to its fail-safe as the endpoint joins
the topology: whether the fail-safe is armed, the Breadcrumb a completed
activation writes, the revert at the fail-safe's expiry and the commit at
CommissioningComplete. The Thread NetworkCommissioning server is wired the
same way.

The servers cache what they read from the border router. They read it at
construction, after each command and at every `Refresh(ctx)`. The host
schedules `Refresh` on both servers, as the reference daemon does every
30 s. A border router that stops answering reads `InterfaceEnabled` false
and null timestamps. It is not an error of the read.

## ThreadBorderRouterManagement

| Command | Rule |
| --- | --- |
| any | a session that is not CASE (PASE, a group message) is UNSUPPORTED_ACCESS |
| `GetActiveDatasetRequest`, `GetPendingDatasetRequest` | the border router's dataset, the empty one when it has none |
| `SetActiveDatasetRequest` (timed) | FAILSAFE_REQUIRED without an armed fail-safe; INVALID_COMMAND for a blob that does not frame as a dataset; INVALID_IN_STATE when an active dataset with an Active Timestamp is configured; BUSY while an earlier activation waits |
| | otherwise the border router activates the dataset, and the request is answered when it is attached (SUCCESS, and the breadcrumb is written) or when the fail-safe expires (TIMEOUT, and the dataset is reverted) |
| `SetPendingDatasetRequest` (timed, `PanChange` only) | INVALID_COMMAND for a blob that does not frame; the border router's answer otherwise |

`BorderRouterName` and `ThreadVersion` come from the `otbr.BorderRouter`
configuration (the version from the border router's own diagnostics record
when it carries one). `BorderAgentId` comes from `/node/ba-id`,
`InterfaceEnabled` from `/node/state`, and the two timestamps from the
datasets' Active Timestamp sub-TLV.

Over REST, the activation is `PUT /node/dataset/active`, then
`PUT /node/state "enable"`. The adapter then polls `/node/state` until the
role is `child`, `router` or `leader`. The REST surface has no attach
callback, so polling stands in for chip's `ActivateDatasetCallback`.
`factoryReset`, the commissioner state and the joiners are not wrapped.

## ThreadNetworkDiagnostics

The server serves the mandatory attributes, the provisional `ExtAddress`
and `Rloc16`, and, with the declared features, the MLE counts (`MLECNT`)
and the MAC counts the border router's record carries (`MACCNT`: the
unicast and broadcast frames sent and received). `PKTCNT` and `ERRCNT` are
refused. The values come from:

| Attributes | Source |
| --- | --- |
| Channel, NetworkName, PanId, ExtendedPanId, MeshLocalPrefix, SecurityPolicy, ChannelPage0Mask, OperationalDatasetComponents | the active dataset |
| RoutingRole, PartitionId, Weighting, DataVersion, StableDataVersion, LeaderRouterId, ExtAddress, Rloc16 | `/node` |
| RouteTable, NeighborTable, the MLE and MAC counts | the border router's own `/diagnostics` record |

Without a configured dataset the nullable attributes read null, as chip
serves them.

## NetworkCommissioning with the Thread feature

`core.NewThreadNetworkCommissioning` serves the same border router's network
as NetworkCommissioning: `MaxNetworks` is 1, the staged network is the
active dataset, and `ConnectNetwork` attaches the border router to it through
the port. `ScanNetworks` has no radio scan to run. It answers the border
router's own network from the active dataset, or an empty list without one.

The Thread Border Router device type couples the two clusters only on an
endpoint that also carries SecondaryNetworkInterface
(`thread-border-router.ts:34-39`). The device kit composes one device type
per endpoint, so the kit does not mount this server. Neither does the reference
daemon: connectedhomeip's own border-router app at the harness commit
`6170af84` composes endpoint 1 of ThreadNetworkDiagnostics and
ThreadBorderRouterManagement only, with NetworkCommissioning on endpoint 0 for
the primary interface (`examples/thread-br-app/thread-br-common/thread-br-app.matter:1759-1979`).
`core.ThreadNetworkCommissioning` is for a host whose device is itself a Thread
participant.

## The reference daemon

`reference-bridge --otbr http://[fd00::1]:8081` adds one ThreadBorderRouter
endpoint under the Aggregator, with `PanChange` and the MLE and MAC counts.
Without the flag the daemon's composition does not change.

## Testing

`otbrtest.New` starts a fake border router in process. It serves matter.js's
recorded OTBR responses in either key casing, and it behaves as an agent does
on a write: an interface that attaches after an enable, and a 409 for a
dataset replaced or deleted while the interface is up. The servers' wire test
(`cluster/threadbr/threadbr_wire_test.go`) runs all three on it on a
commissioned pair.
