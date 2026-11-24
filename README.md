# go-fabric

`go-fabric` is a pure-Go implementation of the Matter protocol, packaged as
a library a host application embeds. On the **device side** it brings the
TLV codec, the Interaction Model, PASE and CASE session establishment, MRP
over IPv6 UDP and TCP, DNS-SD advertisement, the cluster servers a bridge
needs (including the media clusters, the OTA requestor and provider, and
DiagnosticLogs over BDX), and the endpoint assembler that turns a host's own
device list into a Matter topology. The host keeps its device model; this
module owns the wire format and hands back the port contracts (`contract/`)
a device implements to appear as a bridged endpoint. On the **controller
side** (`controller/`, [ADR 0020](./docs/adr/0020-commissioner-and-controller-role.md))
it is a full commissioner: discovery, attestation (with the CSA Distributed
Compliance Ledger, opt-in), NOC issuance, the client interaction and a typed
image of a remote node, bindings and the group sender —
`examples/reference-controller` drives it from the command line. It
implements the Matter specification (currently 1.6.1) and follows
[matter.js](https://github.com/matter-js/matter.js) as its reference
implementation — not a translation of its TypeScript, but its behaviour.
matter.js is our executable specification: where its production-hardened
behaviour departs from the spec text, we follow matter.js and record the
departure; the spec still defines what is mandatory. Cluster IDs, revisions,
attribute IDs, constraints and wire shape are mirrored from matter.js HEAD
rather than transcribed from the specification, and the extracted matter.js
element model ships embedded so parity is a test, not a claim.

## Status

As of `v0.24.0` (2026-11-22); the starting point of the milestones is [the roadmap's baseline](./docs/roadmap.md#baseline-2026-10-17-v060) of 2026-10-17.

| Area | State |
| --- | --- |
| Device side | all 135 clusters of the matter.js snapshot served, all 91 device types generated ([comparison](./docs/matterjs-comparison.md), [roadmap](./docs/roadmap.md)) |
| Controller side | full commissioner (BLE, Wi-Fi and Thread network steps, keys behind `crypto.Signer`, the embedded DCL snapshot), controller role stages 1–6c, Joint Commissioning after connectedhomeip ([comparison](./docs/matterjs-comparison.md)) |
| BLE | controller: done with [milestone 1](./docs/roadmap.md#milestone-1-the-commissioner-in-the-field) — `ble/bluez` on Linux, `ble/esphome` through ESPHome Bluetooth proxies, `pair --ble`, proven against a matter.js node over virtual controllers; device: `node.Network.BLE` with `ble/bluez` as the GATT peripheral on Linux, commissioned over BLE onto Wi-Fi ([guide](./docs/guides/first-boot-without-network.md), [milestone 7](./docs/roadmap.md#milestone-7-ble-on-the-device-side)); proven by chip-tool under chip's BlueZ and wpa_supplicant mocks (`make chiptool-ble-wifi`) and by the weekly btvirt job in both directions, the hardware test follows |
| Wi-Fi | device: `node.Network.WiFi.Driver` joins the network a commissioner provisions; `wifi/wpasupplicant` is the driver on Linux (wpa_supplicant over D-Bus, a nested module), `examples/ble-wifi-light` the example node with BLE and Wi-Fi ([operations](./docs/guides/operations.md#wi-fi-provisioning), [milestone 7](./docs/roadmap.md#milestone-7-ble-on-the-device-side)) |
| Thread | a Thread Border Router over an OTBR and ThreadNetworkDirectory on the device side ([guide](./docs/guides/thread-border-router.md)); the commissioner provisions datasets; no Thread radio ([comparison](./docs/matterjs-comparison.md)) |
| ICD | shipped with [milestone 5](./docs/roadmap.md#milestone-5-icd-and-the-controller-remainder): the device side (`node.Config.ICD`, Check-In) and the controller's ICD client ([guide](./docs/guides/icd.md)) |
| Certification | not pursued; certifiability is a goal ([certifiability](./docs/certifiability.md)) |
| Verified ecosystems | Apple Home, Home Assistant; Google Home and Alexa pending ([roadmap](./docs/roadmap.md#baseline-2026-10-17-v060)) |

## Not certified

This is an independent open-source implementation. It is **not certified by
the Connectivity Standards Alliance**, and it is not a project of the
Alliance. No warranty or assurance is made about rights that may be required
to implement it. Using it does not certify anything: it does not assure
compliance with the Matter specification, and it does not convey any right to
describe a device, product or service built with it as Matter compliant,
certified or similar. Certification requires membership in the Alliance and
compliance with Alliance policy; so does any use of Alliance trademarks and
logos. See [`licenses/NOTICE-matter.js.txt`](./licenses/NOTICE-matter.js.txt),
reproduced from matter.js, which states the same terms for the project this
one is ported from.

### Why "Matter" is not in the name

"Matter" and the Matter logo are trademarks of the Connectivity Standards
Alliance. Their use — in a product, project or repository name as much as
on a device — is reserved to Alliance members and certified products under
the *Connectivity Standards Alliance Trademark and Logo Usage Guidelines
and Terms* (<https://csa-iot.org/resources/governing-documents/>; the
terms are restated in [`licenses/NOTICE-matter.js.txt`](./licenses/NOTICE-matter.js.txt):
"Only the Alliance and its members may use Alliance trademarks and logos,
including, without limitation, the Matter trademarks and logos"). This
project is neither, so the mark is not part of its name or of the names of
the repositories built on it (`go-ha2fabric`, `go-hamapping`); the word
appears only descriptively — a library that implements the Matter
specification, a bridge for Matter controllers — and never to suggest
certification or endorsement. The name `go-fabric` is the specification's
own term for a commissioned network of nodes.

Certification is not pursued by this project. Certifiability is a goal: the
module is meant to let a product built on it pass certification, and the CSA
certification test families run against the reference daemon to hold it
there. What passes, what does not and why, and what a product owner must
supply that a library cannot is in
[`docs/certifiability.md`](./docs/certifiability.md)
([ADR 0011](./docs/adr/0011-certifiability-is-a-goal.md)).

## Documentation

The site (rendered from `docs/`) is at
<https://docs.thingfabric.dev/>; the API reference is
[pkg.go.dev](https://pkg.go.dev/thingfabric.dev/go-fabric).

| | |
| --- | --- |
| [`docs/index.md`](./docs/index.md) | Where to start, by role: embedding the device side, driving the controller, evaluating the architecture. |
| [`docs/architecture.md`](./docs/architecture.md) | The two roles, the layers, the request path, the package map and the scope boundaries that are decisions rather than backlog. |
| [`docs/getting-started/`](./docs/getting-started) | A minimal device host and a minimal controller, each a compiling example. |
| [`docs/guides/host-view.md`](./docs/guides/host-view.md) | A bridge as a host writes it — two declared devices, the change's origin, the device's lifecycle — every line explained. |
| [`docs/guides/`](./docs/guides) | Host ports, concurrency, persistence. |
| [`docs/security.md`](./docs/security.md) | The security model of both roles. |
| [`docs/matterjs-comparison.md`](./docs/matterjs-comparison.md) | Feature-by-feature against matter.js, with a verdict on whether each gap is worth closing here. |
| [`docs/certifiability.md`](./docs/certifiability.md) | What the CSA certification families say about this module, family by family. |
| [`docs/matter-parity-contract.md`](./docs/matter-parity-contract.md) | What parity means and which standing guards enforce it. Read before your first change. |
| [`docs/matter-ecosystem-observations.md`](./docs/matter-ecosystem-observations.md) | What Apple Home, Google Home and Alexa were actually observed to do. |
| [`docs/adr/`](./docs/adr) | Architecture decision records. |
| [`docs/history/`](./docs/history) | Earlier planning concepts (German), kept for their reasoning. |
| [`notes/parity/by_design.md`](./notes/parity/by_design.md) | The catalogue of intentional divergences from matter.js. |
| [`notes/parity/matter_behaviour_findings.md`](./notes/parity/matter_behaviour_findings.md) | Known behaviour gaps that are *not* intentional, as scoped fix packages. |

## Examples

[`examples/reference-bridge`](./examples/reference-bridge) is the reference
daemon: a bridge on this module, the binary the chip-tool suite runs
against. [`examples/reference-controller`](./examples/reference-controller)
is the controller role as a command-line tool — pair a node by its QR or
manual code, read, write, invoke, subscribe, provision groups and bindings,
serve an OTA image — with its fabric kept in one SQLite file; its
[README](./examples/reference-controller/README.md) lists every command.

## API stability

The module carries its own SemVer lane, independent of any host that embeds
it.

### What "public API" means here

**Every exported identifier outside `internal/` is public API.** The module
already draws that line for itself: `internal/` holds three kinds of
package, and Go makes all of them unimportable from outside — the one-way
seams through which the facade, the test-support packages, the reference
programs and the module's own tests reach package internals (`bridgeseam`,
`channelseam`, `coreseam`, `dclseam`, `deviceseam`, `exchangeseam`, `facadeseam`, `fabrictestseam`,
`nodeseam`, `sealseam`, `sessionseam`, `surfaceseam`, `udpseam`, `inflight`, `clockvar`); shared
internals behind them (`serialqueue`, `forward`, `netwatch`, `valuejson`, `attestids`, `reportchunk`, `caseauth`, `deepcopy`);
and test support (`paritytest`, `chipdm`, the chip-tool suite `chiptool`). Nothing else is private by convention, so nothing
else may be changed as if it were.

Depend freely on `bdx/`, `binding/`, `bootid/`, `bridge/`, `client/…`, `clock/`, `cluster/…`,
`commissioning/`, `contract/`, `controller/…`, `dcl/`, `device/`, `diag/`, `diagevent/`,
`eligibility/`, `endpoint/` (including `endpoint/sqlitestore`), `groups/`,
`icd/`, `im/`, `im/subscription`, `logring/`, `mdns/`, `node/`,
`parity/`, `schema/`, `secure/…`, `store/`, `surface/`, `thread/`, `tlv/` and `transport/…`. The
device layer `device/` and the node facade `node/` are public API since
`v0.3.0`; that release freezes nothing, and the `v1.0.0` freeze is still
ahead.

`bridge/bridgetest`, `endpoint/endpointtest`, `controller/ble/bletest`,
`clock/clocktest`, `surface/surfacetest`, `store/storetest` and `fabrictest` are public API on the same terms as the
rest, not a lower tier. They exist for the reason
`net/http/httptest` exists — a consumer's tests need to place a bridge in a
state the production API is not shaped to reach — and a consumer's test suite
is as expensive to break as its production code.

Three things outside `internal/` are still not API. `conformance/` is this
module's own suite, shaped for the tests in this repository rather than for a
consumer -- one of its three exported identifiers takes a `*testing.T`, and the
other two exist to serve it. `examples/reference-bridge`,
`examples/reference-controller` and everything under `script/` are programs
and the packages that serve them (`examples/reference-controller/cli`, the
guards and generators' own packages under `script/`) — runnable, not a
library. Copy from them; do not build against them.

One value-level caveat on `parity/` and `schema/`: their Go surface is
covered by everything below, but the data behind it is a matter.js HEAD
extract. Cluster revisions, attribute IDs and constraint values move when
upstream corrects them. That is a behaviour change, not a signature change,
and it is announced the same way — see the exception under
[Pre-1.0](#pre-10).

### The deprecation window

An identifier on its way out is marked, kept working, and only then removed.

1. **Marked.** Its doc comment gains a paragraph beginning `Deprecated: `,
   naming the replacement and the release the removal becomes permissible in.
   That exact prefix is what `gopls`, `staticcheck` (SA1019) and the Go
   documentation renderer key on, so a consumer sees it in an editor without
   reading a changelog.
2. **Kept.** A deprecated identifier keeps its exact behaviour for the whole
   window. It is not degraded, does not start logging, and does not become a
   no-op. A rename lands the new name first and leaves the old one delegating
   to it.
3. **Removed** no earlier than the **second minor release after the one that
   marked it**. Deprecated in `v0.4.0` means removable in `v0.6.0`, not
   before. Consumers who upgrade minor-by-minor therefore see at least one
   release in which their code still compiles and the warning is already
   visible.

**In force again since 2026-11-18** (owner decision 259 of review II). From
2026-10-21 the window was suspended while the reference consumer was the
module's only one and migrated in one step: a replaced identifier was
removed in the next minor release, once
[`docs/guides/migrating-to-the-facade.md`](./docs/guides/migrating-to-the-facade.md)
named its replacement. The module has four consumers now, so the window
applies as written above: an identifier `v0.23.0` marks is removable in
`v0.25.0`. A removal still has its line in the migration guide, and is
announced under `### Removed` as below.

Currently deprecated, each with its replacement and the release its removal
becomes permissible in:

| Deprecated | Replacement | Removable in |
| --- | --- | --- |
| `im.EventLog.SetCounterPersistence` | `EventLog.SetCeilingPersistence`, whose persist function returns the store's error | `v0.25.0` |
| `secure/operational.Manager.ReleaseID` | `Manager.AllocateStake` and `Manager.ReleaseStake`, which free the caller's own stake only | `v0.25.0` |
| `secure/operational.Manager.ClosePeer` | none with the CloseSession farewell; `Manager.Close` on each id `Manager.SessionIDsForPeer` returns closes without it | `v0.25.0` |
| `transport/mrp.NewWindowNoRollover` | `transport/mrp.NewWindowNoRolloverAt(0)` | `v0.25.0` |
| `bridge.FailSafeArmer` | none; the fail-safe comes with the commissioner's ArmFailSafe or the arm of its PASE session | `v0.25.0` |
| `bridge.CommissioningWindow.SetFailSafeArmer` | `CommissioningWindow.SetFailSafeExpirer` | `v0.25.0` |
| `cluster/core.GeneralCommissioning.AutoArmOnPaseEstablished` | `GeneralCommissioning.ArmOnPaseSession`, which returns the refusal of the arm and takes the session's id | `v0.25.0` |
| `im/subscription.Manager.CloseEndpoint` | `Manager.Close` on each subscription to end; a Reassemble that removes an endpoint ends none | `v0.25.0` |
| `bridge.MRPAckAdapter`, `bridge.NewMRPAckAdapter` | none; `Bridge.AttachAckTracker` wires the tracker, which a peer's acknowledgement does not discharge | `v0.25.0` |
| `groups.NoKeyError` | `groups.DecodeError`, the outcome of a group message no candidate key passed; `Manager.Decode` no longer returns `NoKeyError` | `v0.25.0` |
| `store.Store.DeleteSetting` | `Store.DeleteSettings`, which every `store.Settings` backend implements | `v0.25.0` |
| `endpoint.Config.OnNodeLabelWritten`, `endpoint.Config.Scenes`, `endpoint.ScenesStore` | `endpoint.Config.Persistence` (`endpoint.Persistence`), whose saves and loads report the store's error | `v0.25.0` |
| `node.EndpointPersistence`, `node.Config.EndpointPersistence`, `node.NewEndpointPersistence` | `node.EndpointState`, `node.Config.EndpointState`, `node.NewEndpointState` | `v0.25.0` |
| `cluster/core.AccessControlEntryStruct.Redacted`, `cluster/core.AccessControlAuxiliaryEntryStruct.Redacted`, `cluster/core.AccessControlExtensionEntry.Redacted`, `cluster/core.SceneInfoStruct.Redacted` | none: return the entry whole; the dispatcher scopes every fabric-scoped attribute of the model | `v0.25.0` |
| `node.Node.Persistence` | `Node.EndpointState`, which returns the new port | `v0.25.0` |
| `cluster/spec.ServerConfig.BeforeWrite` | `ServerConfig.WriteRule` for a server's own rule (no host veto replaces it); `Server.SetBeforeWrite` / the typed `OnWrite` for the host's veto | `v0.25.0` |
| `cluster/spec.Server.Notify`, `cluster/spec.Server.NotifyFrom`, `cluster/spec.Server.ChangedFrom`, `cluster/spec.Server.ReportFrom` | `Server.Set` for a held value, `Server.MarkChanged` (or `MarkQuietChanged` for a quieter attribute) for one the host computes on read | `v0.25.0` |
| `cluster/spec.Server.Emit`, `cluster/spec.EmitOn`, `cluster/spec/<cluster>.Server.Emit<Event>` (the generated typed emits) | `Server.EmitEvent`, `<cluster>def.Server.Record<Event>`: the event goes out on the endpoint the server is mounted on (`spec.ErrNotMounted` before) | `v0.25.0` |
| `cluster/spec/<cluster>.Feature` of the 39 generated clusters without features (Binding, Identify, …; the type's doc says so) | none: the cluster has no FeatureMap bit, so no value has the type | `v0.28.0` |
| `im.InvokeResponse.HasCommandData` | none; range over `InvokeResponse.Responses` and test `IsStatus` | `v0.26.0` |
| `client/exchange.Manager.HandlePeerLoss` | `Manager.HandlePeerLossBefore`, which cuts by registration order | `v0.26.0` |
| `cluster/spec.RequestCheck.Unchecked` | none; a command with its own refusal states `RequestCheck.Refusal` | `v0.26.0` |
| `im/subscription.Subscription.SessionID` (reading the field) | `Subscription.CurrentSessionID`, read under the subscription's lock: the session changes when a newer one supersedes it | `v0.26.0` |
| `client/peer.RebootArmerContext.ClosePeerSessionsBefore` | `RebootArmerContext.ClosePeerSessionsRegisteredBefore`, which cuts by registration order | `v0.26.0` |
| `secure/operational.Manager.ClosePeerLost`, `secure/operational.Manager.ClosePeerLostSince` | `Manager.ClosePeerLostBefore` with the `Manager.RegistrationCount` read when the exchange began, which cuts by registration order | `v0.26.0` |
| `cluster/core.OTAApplyPolicy`, `cluster/core.OTAProviderConfig.ApplyPolicy`, `node.OTAApplyPolicy`, `node.OTAProviderConfig.ApplyPolicy`, `controller/softwareupdate.ProviderConfig.ApplyPolicy` | `OTAProviderConfig.ApplyDelay` for the delay; `OTAProviderConfig.Manager` (`OTAUpdateManager.ApplyUpdate`) for the decision | `v0.26.0` |
| `cluster/core.OTAProviderConfig.OnStatusChange` | `OTAProviderConfig.OnAttemptStatus` (every manager) | `v0.26.0` |
| `cluster/core.OTAProviderConfig.RebootQuery`, `cluster/core.OTAProviderConfig.TransferBlockSize`, `cluster/core.OTAProviderConfig.SessionActivity`, `node.OTAProviderConfig.RebootQuery`, `node.OTAProviderConfig.TransferBlockSize`, `controller/softwareupdate.ProviderConfig.OnStatusChange`, `controller/softwareupdate.ProviderConfig.RebootQuery`, `controller/softwareupdate.ProviderConfig.TransferBlockSize` | `OTAProviderConfig.Manager` (`OTAUpdateManager`): admission and transfer options are the manager's | `v0.26.0` |
| `controller/softwareupdate.Manager.OnStatusChange`, `controller/softwareupdate.Manager.SuppressNextStartUp` | `ProviderConfig.Manager`, through which the provider reports to the manager's attempts and the manager judges a restart query | `v0.26.0` |
| `cluster/wire.ScenesManagement` | `cluster/core.ScenesManagement` (`core.NewScenesManagement`) | `v0.25.0` |
| `cluster/wire.Schedules*` (the server and source types, the attribute, cluster and revision constants), `cluster/wire.NewSchedulesServer`, `cluster/wire.ScheduleEntry`, `cluster/wire.ScheduleStruct`, `cluster/wire.ErrSchedulesReadOnly` | the Thermostat server's schedules (`cluster/thermo`, `docs/guides/thermostat.md`) | `v0.25.0` |
| `bridge.Bridge.ConnectPeer`, `bridge.Bridge.ConnectOtaProvider` | none to call: a node reaches its OTA provider and its camera's requestor over its own peer path (ADR 0032); `Bridge.LivePeerSession` reuses a live session without a handshake | `v0.25.0` |
| `node.ErrCommissioningWindowAlreadyOpen`, `node.ErrCommissioningWindowNotConfigured` | none; `bridge.CommissioningWindowOpener` returns the `bridge` values | `v0.25.0` |
| `node.Config.SupportedFabrics`, `node.Config.MaxPathsPerInvoke` | `Config.Limits.SupportedFabrics`, `Config.Limits.MaxPathsPerInvoke` | `v0.25.0` |
| `mdns.CommissionableServiceConfig.PairingHint`, `bridge.CommissioningAdvertisement.PairingHint` | `Hints` (`mdns.PairingHints`) | `v0.25.0` |
| `bridge.Bridge.AttachPaseHandler`, `bridge.Bridge.AttachCaseHandler` | `AttachPaseHandlerProvider`, `AttachCaseHandlerProvider` | `v0.25.0` |
| `bridge.Bridge.SubscriptionPersistenceEnabled`, `bridge.Bridge.FormerSubscriptionCount` | `bridgetest.SubscriptionPersistence` | `v0.25.0` |
| `contract.BLEPeripheral`, `node.Network.BLE`, `bridge.Config.BLE` | `contract.GATTPeripheral`, `node.Network.GATT`, `bridge.Config.GATT` | `v0.25.0` |
| `cluster/core.GeneralCommissioning.SetOnFailSafeExpired` | `GeneralCommissioning.SetOnFailSafeExpiry` | `v0.25.0` |
| `cluster/core.GeneralCommissioning.SetOnFailSafeRolledBack` | `GeneralCommissioning.SetFailSafeSessionCloser` | `v0.25.0` |
| `cluster/core.GeneralCommissioning.ArmOnPaseEstablished` | `GeneralCommissioning.ArmOnPaseSession` | `v0.25.0` |
| `device.NewRootNode` | none; the root endpoint is the node's (`node.Node.Root`) | `v0.25.0` |
| `endpoint.WriteChanges`, `endpoint.WithWriteChanges`, `endpoint.WriteChanges.Unchanged`, `endpoint.WriteChanges.Changed`, `endpoint.WriteChanges.ChangedAttributes` | `im.HandleWrite`, whose results carry each write's verdict in `im.WriteResult.Commit` | `v0.25.0` |
| `commissioning.PASEConfig`, `commissioning.PASEResponder`, `commissioning.NewPASEResponder`, `commissioning.ErrPASEInvalidIterations`, `commissioning.ErrPASEInvalidPasscode`, `commissioning.ErrPASEStateMismatch`, `commissioning.BuildAttestation`, `commissioning.AttestationInput`, `commissioning.AttestationResult`, `commissioning.ErrAttestationChallenge`, `commissioning.ErrAttestationNonce`, `commissioning.BuildCSR`, `commissioning.CSRInput`, `commissioning.CSRResult`, `commissioning.ErrCSRNonce`, `commissioning.DACChain`, `commissioning.VerifyChain`, `commissioning.LoadPAAPoolFromPEM`, `commissioning.ErrDACChainBroken`, `commissioning.ErrDACExpired`, `commissioning.ErrDACMalformed` | the node's commissioning window (`node.Node.OpenBasicWindow`, `OpenOwnWindow`) and `cluster/core.OperationalCredentials`; `secure/spake2` and `secure/mattercert.CreateCSR` for the pieces; `controller/attestation.Validate` for the DAC chain | `v0.25.0` |
| `mdns.DeriveUniqueIDFromIdentity` | `node.Commissioning.RotatingDeviceID`, or 16 random bytes drawn once and persisted, passed to `mdns.GenerateRotatingID` | `v0.25.0` |
| `cluster/core.OperationalCredentials.SetCurrentFabric`, `cluster/core.GroupKeyManagement.SetCurrentFabric`, `cluster/core.AccessControl.SetCurrentFabric` | none: a fabric-scoped command or write takes its fabric from the request (`im.WithFabricFilter`) | `v0.25.0` |
| `secure/operational.Manager.OpenFromSigmaWithID`, `secure/operational.Manager.OpenFromSigmaAsInitiatorWithID`, `secure/operational.Manager.OpenFromPaseWithID` | `Manager.AllocateStake` with `OpenFromSigmaWithStake`, `OpenFromSigmaAsInitiatorWithStake`, `OpenFromPaseWithStake`, which install only while the handshake still holds its id (`ErrStakeLost` otherwise) | `v0.25.0` |
| `secure/operational.Manager.AllocateID` | `Manager.AllocateStake` (`Stake.ID` where the id is announced) | `v0.25.0` |
| `cluster/spec.Hooks` | a `spec.Server` over the server's own state, MatterInvoke routed through `spec.InvokeHeld` over `spec.NewSelfHeld`; events `spec.Events`, veto `spec.WriteVeto` | `v0.25.0` |
| `cluster/closure.ControlServer.Hooks` (the embedded field) | `ControlServer.Handle`, `Emit`, `EmitEvent`, `SetBeforeWrite` | `v0.25.0` |
| `cluster/modebase.Server.Hooks`, `cluster/modebase.Server.Init`, `cluster/modebase.Server.Invoke`, `cluster/modebase.Server.CheckWrite` | `Server.Handle` for a host handler, `Server.MatterInvoke` for the checked command path; `Init` needs none, `CheckWrite` none (no controller write reaches a veto) | `v0.25.0` |
| `cluster/wire.Color*`, `cluster/wire.FanControl*`, `cluster/wire.MatterEvent*`, `cluster/wire.OperationalState*`, `cluster/wire.RvcOperationalState*`, `cluster/wire.ThermostatCmdSetpointRaiseLower`, `cluster/wire.ThermostatSetpointMode*`, `cluster/wire.WindowCoveringAttr*`, `cluster/wire.WindowCoveringClusterID`, `cluster/wire.WindowCoveringCmdDownOrClose`, `cluster/wire.WindowCoveringCmdGoToLiftPercentage`, `cluster/wire.WindowCoveringCmdGoToTiltPercentage`, `cluster/wire.WindowCoveringCmdStopMotion`, `cluster/wire.WindowCoveringCmdUpOrOpen` (ids that duplicate a generated constant; CHANGELOG names each) | the `ClusterID` / `Attr…` / `Cmd…` / `Event…` / enum constant of `cluster/spec/<cluster>` | `v0.25.0` |
| `cluster/wire.ThermostatCmdSetWeeklySchedule`, `cluster/wire.ThermostatCmdGetWeeklySchedule`, `cluster/wire.ThermostatCmdClearWeeklySchedule`, `cluster/wire.WindowCoveringCmdGoToLiftValue`, `cluster/wire.WindowCoveringCmdGoToTiltValue`, `cluster/wire.GoToLiftValueRequest`, `cluster/wire.GoToTiltValueRequest`, `cluster/wire.DecodeGoToLiftValue`, `cluster/wire.DecodeGoToTiltValue`, `cluster/wire.ErrWindowCoveringMalformed` | none: the commands are not in the model the module serves | `v0.25.0` |
| `cluster/<package>.<Config>.DataVersion` (the field of every server config, the ServerConfig of cluster/spec and the HeldConfig of levelcontrol among them), `cluster/<package>.Server.MatterDataVersion` (of levelcontrol, modeselect and modebase) | none: leave the field nil; the wire carries the endpoint's DataVersion for the cluster, or the server's own where it serves one | `v0.25.0` |
| `im/subscription.Reporter`, `im/subscription.EventReporter`, `im/subscription.FlushReporter`, `im/subscription.Manager.SetEventReporter`, `im/subscription.Manager.SetFlushReporter` | `ReportSender` set with `Manager.SetReportSender`; a flush is a `Report` with `Flush` set | `v0.25.0` |
| `im/subscription.Subscription.TakeDuePaths`, `im/subscription.Subscription.NothingSent` | `Report.Paths`; a `ReportSender` with nothing for the subscriber returns `NothingForSubscriber` | `v0.25.0` |
| `bridge.Bridge.SubscriptionReporter`, `bridge.Bridge.SubscriptionEventReporter`, `bridge.Bridge.SubscriptionFlushReporter` | none needed: `Bridge.AttachSubscriptionManager` installs the bridge's sender; pass nil as `subscription.NewManager`'s reporter | `v0.25.0` |
| `bridge/bridgetest.EstablishSubscriptionTarget`, `bridge/bridgetest.SubscriptionTarget`, `bridge/bridgetest.SubscriptionTarget.PeerCounter`, `bridge/bridgetest.ErrNoPeerCounter`, `bridge/bridgetest.ErrNoSubscriptionID`, `bridge/bridgetest.ErrNoPeer`, `bridge/bridgetest.ErrPeerCounterNotAnchored`, `bridge/bridgetest.ErrNotEstablished` | `bridgetest.Session.Subscribe` of the test `Commissioner` | `v0.25.0` |
| `bridge.ErrAckWaitTimeout` | none: no function returns it | `v0.25.0` |
| `client/peer.Descriptor.DataModelRevision` | `Descriptor.ReportedSessionParameters.DataModelRevision`, the revision the peer's session parameters report | `v0.25.0` |
| `schema.ClusterCommandLevels` | `schema.ClusterRequestCommandLevels` for an accepted command, `schema.ClusterResponseCommandLevels` for a generated one | `v0.25.0` |
| `client/interaction.DefaultSustainRetries` | `SubscribeRequest.Retries`, a schedule of the subscription's own; while the variable is kept, the field's zero fields read it | `v0.28.0` |
| `controller/dcl.*` (every name of the package, each forwarding unchanged) | the same name in `dcl`, the role-neutral package the ledger client moved to | `v0.28.0` |
| `cluster/core.WiFiNetworkCommissioningConfig.IsFailSafeArmed`, `cluster/core.ThreadNetworkCommissioningConfig.IsFailSafeArmed` | `FailSafe`, the fabric-aware port (`GeneralCommissioning.FailSafeFabric`); on Thread also `ThreadNetworkCommissioning.AttachFailSafe` | `v0.28.0` |
| `cluster/core.ScenesConfig.Fabrics` | `ScenesConfig.ListFabrics`, which reports the error | `v0.28.0` |
| `bridge.CaseSessionEstablished`, `bridge.CaseAdapter.SetOnSessionEstablished` | `bridge.CaseEstablished`, `CaseAdapter.SetOnEstablished`, whose callback receives the session the handshake step established (`sigma.Established`) | `v0.28.0` |
| `bridge.CaseAdapter.SnapshotResponder` | the `sigma.Established` value `CaseAdapter.SetOnEstablished`'s callback receives | `v0.28.0` |
| `cluster/core.NewAccessControl`, `cluster/core.AccessControl.SetLimits` | `cluster/core.NewAccessControlWith` with `AccessControlConfig{Store, Limits}`, which refuses a limit outside its range at construction | `v0.29.0` |
| `cluster/core.ARLClusterID` | none: ARL is AccessControl's MNGD feature, `cluster/spec/accesscontrol.ClusterID` (0x001F); 0x002B is LocalizationConfiguration | `v0.29.0` |
| `cluster/camera.Provider.PeerGone` | `Provider.EndLostPeer`, the same call, which also returns how many of the pair's sessions it ended | `v0.29.0` |

A patch release (`v0.x.y`) never removes or renames a public identifier and
never changes a signature. Deprecations may be *marked* in a patch release;
they are only ever *acted on* in a minor one.

### Pre-1.0

The module is at `v0`; `v0.1.0` is its first tag. Before it, `main` was the
only consumable version and could break in any commit, which is why consumers
tracked a pseudo-version. `main` is still consumable the same way — see
`CONTRIBUTING.md` — but a tagged version is what a consumer should pin.

SemVer permits a `v0` module to break on any minor bump. This module does not use that permission as a default. The window
above applies at `v0` exactly as it would at `v1`: a breaking change is a
minor bump, never a patch, and wherever the old identifier can be kept
working it is kept working for the full window.

`v0.3.0` is the first release to act on the window: it removes the
identifiers `v0.2.0` marked for removal in it, each listed with its
replacement under `### Removed` in [`CHANGELOG.md`](./CHANGELOG.md#030--2026-10-12).

Two changes bypass the window, and both are named as such in their entry:

- **A matter.js parity correction.** matter.js HEAD is the gold standard for
  every cluster ID, revision, attribute ID and wire shape in this module.
  When upstream corrects one, this module follows it in the next release
  without a window. Holding a value stable *because it is stable* would mean
  knowingly shipping a wrong one, and the failure it produces is a silent
  pairing abort in a controller nobody can attach a debugger to.
- **A security fix that cannot be made without an API change.** It ships in
  the next release, marked `### Security`.

What `v0` does *not* excuse: an unannounced break. Every one of these lands
in the changelog before a consumer meets it.

### How a consumer learns about a break

[`CHANGELOG.md`](./CHANGELOG.md) is the single place. It follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), so the headings
already carry the meaning:

- `### Deprecated` — the release that marks an identifier. The entry gives
  the full import path and name, the replacement, and the earliest release
  the removal may happen in.
- `### Removed` — the release that removes it. The entry repeats the
  replacement, so a consumer who skipped the deprecation release still finds
  it here rather than in a diff.
- `### Changed` — behaviour that moved under an unchanged signature. A parity
  correction lands here and says which matter.js element changed.
- `### Security` — a fix that forced an API change out of band.

A release note carries, for the version it names: every entry from those four
headings, and an explicit statement of whether upgrading requires a source
change. "No source change required" is a claim worth making when it is true,
because it is the only sentence most consumers need to read.

The rule behind all of the above: **a break that appears only in a commit
message, a doc comment or a diff has not been announced.** If it is not in
the changelog, it does not ship.

A release pull request (head branch `release/*`) is also the gate of the
certification run: it runs every chip-tool job and certification family the
nightly run does — the `long` group, whose one case waits 3 h 10 min, by its
result on the base commit when the pull request changes documentation only —
checks that the nested modules build against the versions
they require, and cannot merge with one of them red
([Certifiability](https://github.com/thingfabric/go-fabric/blob/main/docs/certifiability.md#how-the-status-is-produced)).

A guard holds the rule: `make apidiff` compares every public package with
the last release tag (`golang.org/x/exp/cmd/apidiff`, a tool dependency of
the module) and prints the incompatible changes, and `make apidiff-check`,
a CI job, fails when one of them is not named under `[Unreleased]` in
`CHANGELOG.md` (`### Changed`, `### Removed` or `### Deprecated`).

## Licence

Apache License 2.0 — see [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE)
([ADR 0023](./docs/adr/0023-apache-2-and-the-cla.md): the same licence as
matter.js and connectedhomeip, with their patent grant; `v0.4.0` and earlier
were released under MIT and stay so). Upstream notices and the dependency
licences are recorded in
[`THIRD-PARTY-NOTICES.md`](./THIRD-PARTY-NOTICES.md). Contributions need
the contributor licence agreement in [`CLA.md`](./CLA.md), agreed once per
account in the first pull request.
