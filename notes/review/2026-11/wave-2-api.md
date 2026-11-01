# Wave 2 review — theme `api`

- **Scope:** `bridge/` as the composition root — the package doc (`bridge/bridge.go:4-177`), every `Attach…` / `Set…` port and the noop `New` installs (`bridge.go:764-798`, `receive.go:65-70`, `securechannel.go:98-135`, `group_messaging.go:55`, `subscription_persistence.go:43-58`), `Start` / `Reassemble` / `reassembleLocked` / `Stop` (`bridge.go:814-1240`, `:1740-1830`), `Snapshotter` and `Config` (`bridge.go:231-330`), `device_load.go`, `devicetype_validation.go`, `attribute_value_reader.go:1-140`, `:420-500`, `application_values.go`, `metrics.go`, `icd.go:1-100`, the BDX responder port (`bdx_responder.go:1-120`), the CASE initiator seam (`case_initiator.go:1-130`, `case_provider.go:1-80`), the session ports (`securechannel.go:960-1020`), `bridge/bridgetest` (package doc, exports); `endpoint/` as the assembler — `assembler.go` (whole), `identity.go` (whole), `spec.go` (whole), `types.go:40-64`, `:250-320`, `:619-672`, `doc.go`, `dispatcher.go:30-45`, `:108-120`, `:536-550`, `:650-700`, `:790-860`, `:1252-1264`, `materialize.go:50-70`, the id stores (`memstore`, `sqlitestore` outlines), `endpoint/endpointtest`; `store/` as a port — `doc.go`, `store.go`, `port.go` (whole); `contract/` as bridge and endpoint consume it — `status.go`, `matter.go:44-100`, the interface inventory. The consumer side was read only to check doc claims: `node/node.go:257-268`, `:300-340`, `:606-620`, `:748`, `:934-948`, `node/root.go:529-545`, `node/lifecycle.go:145-153`, `docs/guides/ports.md`, `docs/guides/advanced-seams.md:78-118`, `docs/getting-started/device.md:10-30`.
- **HEAD:** `769ea3c2`.
- **Pins:** matter.js `862ad75f` (`parity/schema.json` → `matter.sourceCommit`; read at `../matter.js`: `packages/node/src/storage/server/ServerEndpointStores.ts:85-190`, `packages/node/src/behaviors/descriptor/DescriptorServer.ts:216-254`, `packages/model/src/standard/elements/basic-information.element.ts:36-38`). connectedhomeip was not needed: matter.js models every behaviour this theme touches.
- **Method:** slices only; every claim below is from lines read on both sides. `notes/parity/by_design.md` (rows BD-Matter-EndpointGC-ModelCompleteGate, BD-Matter-EndpointNumber-Monotonic) and ADR 0016, 0018, 0025, 0026 were checked for each candidate; a recorded decision is not listed. Security (ACL evaluation, PASE/CASE), concurrency (lifecycle races, subscriptions, mdns) and wire encoding are other reviewers' themes and were read only as callers.

No high findings. Four medium, six low, three notes.

---

## W2-api-01 — medium — `SetOnFabricAdded` is a hook nothing fires

**Where:** `bridge/bridge.go:1286-1309` (`SetOnFabricAdded`, `EmitFabricAdded`), `node/node.go:606-620` (`observedFabricInstalled`), `docs/guides/ports.md` ("Ports the facade leaves to the host … the bridge's `SetOnReassembled` and `SetOnFabricAdded` hooks"), `docs/guides/advanced-seams.md:84-87`.

**Claim:** `EmitFabricAdded`'s doc says the OperationalCredentials `OnFabricInstalled` closure calls it on a successful AddNOC. No non-test file in the module calls it: the facade's `observedFabricInstalled` fans out to `node.Observers.OnFabricAdded` and never touches the bridge hook. Both docs pages advertise `SetOnFabricAdded` as a port the facade leaves to the host, so a host that wires `Node.Bridge().SetOnFabricAdded(fn)` gets a hook that is stored and never fires — silent. `SetOnFabricRemoved` is wired (`node/node.go:519` → `EmitFabricRemovedContext`), so the pair is asymmetric.

**Reference:** doc claim against code; no matter.js counterpart (matter.js's `FabricManager.events.added` is observed by the node, which is what `node.Observers` mirrors).

**Fix:** Either call `EmitFabricAdded` from `observedFabricInstalled` (one line, keeps the API) or remove `SetOnFabricAdded` / `EmitFabricAdded` and point both docs pages at `node.Observers.OnFabricAdded` (owner rule 2026-10-21: remove, do not deprecate, with a migration-guide line).

**API break:** none for the first option; yes for the removal.

**Confidence:** confirmed (grep over every non-test `.go` file; only the definitions and their comments mention the name).

## W2-api-02 — medium — four mandatory `bridge.Config` fields and `endpoint.Config.NodeLabel` are never read

**Where:** `bridge/bridge.go:264-279` (`VendorID`, `ProductID`, `NodeLabel`, `Discriminator`), `:318-330` (`validate`), `node/node.go:257-268`; `endpoint/assembler.go:19-27`, `:86-88`, `:161`; `endpoint/types.go:631-632` (`Topology.NodeLabel`).

**Claim:** `bridge.Config` validates `VendorID`, `ProductID` and `NodeLabel` as mandatory at `New`, and documents `NodeLabel` as "BasicInformation.NodeLabel" and `Discriminator` as "surfaces in the commissionable mDNS record". The bridge reads none of the four: the only `b.cfg.` consumers are `AdvertiseTimeout`, `Listen`, `MaxPathsPerInvoke`, `PreferIPv4`, `SessionIntervals` and `TCP`; the commissionable record takes its discriminator, VID, PID and device name from the `CommissioningAdvertisement` argument (`bridge.go:2160-2206`). `endpoint.Config.NodeLabel` is likewise mandatory ("the root BasicInformation cluster mandates them"), is copied into `Topology.NodeLabel` and read by nobody; the served label is `cluster/core.BasicInformation`'s own `Config.NodeLabel` (`basic_information.go:246-247`, `:346`). A host that drives `bridge.New` or `endpoint.New` must therefore invent three copies of a label and a discriminator that go nowhere, and the field docs tell it the wrong consumer.

**Reference:** matter.js `basic-information.element.ts:36-38` declares `NodeLabel` writable with no default — the label is cluster state, not a constructor requirement. The bridge's own doc at `bridge.go:241-247` says `Config` "carries nothing that governs topology assembly … holding a second copy of them here would be a setting that looks live and changes nothing" — which is what these four fields are.

**Fix:** Remove `VendorID`, `ProductID`, `NodeLabel`, `Discriminator` from `bridge.Config` and `NodeLabel` from `endpoint.Config` / `Topology` (the VID/PID in `endpoint.Config` stay: they are stamped onto bridged endpoints at `assembler.go:201-205`), with a migration-guide line; the facade already passes the served values to `core.BasicInformation`.

**API break:** yes (field removal on two exported structs; `apidiff` will name it).

**Confidence:** confirmed.

## W2-api-03 — medium — endpoint 1 is a magic number the docs describe three different ways

**Where:** `endpoint/types.go:40-64`, `:609-614` (`IsAggregator` = `ID == 1`), `endpoint/materialize.go:53-68` (`ClusterServers` serves only the attached set for ID 1), `endpoint/doc.go:26-29` ("Bridged endpoints occupy 1..65534"), `endpoint/identity.go:58-60` (`Record`: "EndpointID is in [1, 65534]; endpoint 0 … is never stored"), `endpoint/sqlitestore/sqlitestore.go:55` (`allocMin = 2`), `bridge/bridge.go:1003`, `:1708` (`FindByID(1)`), `node/node.go:334-339`, `docs/getting-started/device.md:17-20`.

**Claim:** The Aggregator role is keyed on the endpoint number, not on the endpoint: every bridge and dispatcher branch that treats the Aggregator specially tests `ID == 1`, and `ClusterServers` returns the published set for ID 1 whatever `Source` / `Measurement` the endpoint carries. The facade publishes its Aggregator servers onto `FindByID(1)` unconditionally, also with a host `Snapshotter` — although `device.md` says "the facade then builds no Aggregator". `endpoint/doc.go` and `Record` tell a host its bridged endpoints occupy `1..65534`; `types.go` and both stores say `2..65534`. A host that follows `doc.go` and numbers its first device 1 has that endpoint's clusters replaced by the facade's Descriptor, silently: a concrete read answers the Aggregator's Descriptor, a wildcard read never shows the device.

**Reference:** matter.js assigns the Aggregator a number like any endpoint (`ServerEndpointStores.ts:109-130`, `assignNumber`: the stored or next free number) and keys behaviour on the endpoint's type, never on its number; `DescriptorServer.ts:227-254` computes each endpoint's PartsList from the index. The fixed three-tier shape itself is recorded (`types.go:43-60`, by_design row BD-Matter-EndpointNumber-Monotonic "2 — 1 is the Aggregator"); the number-keyed role and the contradictory ranges are not.

**Fix:** Make the Aggregator role a property of the endpoint (an `Aggregator bool` the assembler sets, or `DeviceType == 0x000E` with no `Source`), used by `IsAggregator`, `ClusterServers` and the two `FindByID(1)` sites; fix `endpoint/doc.go:28` and `identity.go:58` to `2..65534`; have the facade skip `AttachAggregatorClusters` when the topology has no Aggregator endpoint.

**API break:** no for the doc fixes and the facade condition; the role field is an addition.

**Confidence:** confirmed.

## W2-api-04 — medium — the `Snapshotter` contract does not say the topology must come from `endpoint.Assembler`

**Where:** `bridge/bridge.go:231-239` (`Snapshotter` doc), `endpoint/types.go:276-300` (`endpointState`: "a bare endpoint gets a private state"), `endpoint/dispatcher.go:30-42` (`clusterDataVersionFor`), `contract/dataversion.go:26-27`, `:49-59` (random initial version), `endpoint/assembler.go:256-279` (`state`, `groups`, `onNodeLabelWritten`, `scenesStore` set only by `buildEndpoint`), `docs/getting-started/device.md:17-20` ("when you already have an endpoint assembler over your own device model"), `docs/guides/advanced-seams.md:113-116`.

**Claim:** `endpoint.Endpoint` and `Topology` are exported structs with exported fields, and the docs invite a host to put "its own assembler" behind `Config.Topology`. Everything that must survive a `Reassemble` lives in unexported fields only `Assembler.buildEndpoint` fills: the per-cluster DataVersion, the Identify server, the written NodeLabel, ConfigurationVersion, the scene table, the group manager and the label-persistence hook. A hand-built `*Endpoint` gets a fresh private state per instance, so every `Reassemble` hands each of its clusters a new random DataVersion (the controller's DataVersionFilter never matches and every subscription re-primes), the host's `Groups` / `Scenes` servers are never mounted, and a controller-written NodeLabel is lost on the next snapshot. None of this is an error; nothing in the `Snapshotter` doc or the two pages says the only valid producer is `endpoint.Assembler`.

**Reference:** matter.js `Datasource.ts:410` (cited at `dispatcher.go:37`): the version is set once per endpoint lifetime, not per access; the Go translation keeps it in `endpointStateRegistry`, which only the assembler reaches.

**Fix:** State the contract in the `Snapshotter` doc and both docs pages: a topology is produced by `endpoint.Assembler` over the host's `Spec`s ("own assembler" means own `Spec` producer, not own `Topology` producer), and `Endpoint` / `Topology` are read-only to a host. If hand-built topologies are meant to be supported, export a way to carry the state (an `endpoint.Topology` builder) instead.

**API break:** no (doc contract); a builder would be an addition.

**Confidence:** confirmed for the behaviour; plausible for the reading of the docs' "own assembler".

## W2-api-05 — low — the port list that claims to be complete misses five ports

**Where:** `bridge/bridge.go:39-45` ("The list below is therefore the actual requirement") and `:48-177`; `docs/guides/ports.md:120-166` ("The facade attaches all of these … in this order"); `bridge/icd.go:42`, `:61`, `:79`; `bridge/metrics.go:26`; `bridge/subscription_persistence.go:109`.

**Claim:** `SetLocalFixedMRPBackoff`, `SetOnInboundActivity` and `SetICDAdvertisement` appear in neither list (the last is reachable only through `Config.SessionIntervals`'s doc). `AttachMetrics` is in the package doc but not in `ports.md`'s table; `SetSubscriptionPersistence` is a sentence inside another row. A host wiring an ICD by the documented order never learns the three ICD ports exist.

**Reference:** doc claim against code.

**Fix:** Add the five to both lists (ICD group: "silent — the ICD's record and MRP padding stay those of a non-ICD node"; metrics: "silent — nothing is recorded").

**API break:** no.

**Confidence:** confirmed.

## W2-api-06 — low — exported Bridge surface that only tests call

**Where:** `bridge/securechannel.go:136-150` (`AttachPaseHandler`), `:244-254` (`AttachCaseHandler`), `bridge/subscription_persistence.go:118-124` (`SubscriptionPersistenceEnabled`), `:163-170` (`FormerSubscriptionCount`), `bridge/bridge.go:2328-2333` (`ControllerRTT`).

**Claim:** The two singleton handler ports have no production caller; the package doc itself (`bridge.go:65-73`) tells a host to prefer the providers because the singleton CASE adapter "sticks in Finished after the first Sigma3" — the port is kept as a footgun for tests. `SubscriptionPersistenceEnabled` and `FormerSubscriptionCount` are read only by tests; `ControllerRTT` has no caller at all (tests included) although `advanced-seams.md:85` presents it as a host diagnostic. `bridgetest`'s package doc (`bridgetest.go:4-19`) states the rule these violate: entry points that exist only to serve tests live in `bridgetest`, not on `Bridge`.

**Reference:** `bridge/bridgetest/bridgetest.go:14-19`; CLAUDE.md "everything outside `internal/` is public API".

**Fix:** Remove `AttachPaseHandler` / `AttachCaseHandler` (the providers cover the singleton case with a one-entry factory); move the two subscription readers behind `bridgetest` or keep them with a doc line naming their purpose; keep `ControllerRTT` only if a host is expected to call it.

**API break:** yes for each removal (migration-guide line).

**Confidence:** confirmed (call-site count per method over non-test files outside `bridge/`).

## W2-api-07 — low — two persistence ports in `endpoint.Config` have no context and no error

**Where:** `endpoint/assembler.go:40-47` (`OnNodeLabelWritten func(key SourceKey, label string)`), `:48-55`, `:73-78` (`ScenesStore`: `LoadScenes(key) []byte`, `SaveScenes(key, table)`); compare `endpoint/identity.go:88-103` (`Store`: every method `ctx`, every method `error`) and `store/port.go:179-183` (`ThermostatPresets`: `ctx`, `error`).

**Claim:** The two seams a host backs with a database carry neither cancellation nor an error path: a failed scene save is invisible, a slow one blocks the dispatch that triggered it, and the shapes differ from every other persistence port in the module. ADR 0026 principle 7 names three persistence interfaces (`store.Store`, `endpoint.Store`, the client store) and "no package … keeps state across a restart by any other path"; `ScenesStore` and `OnNodeLabelWritten` are a fourth path the principle does not name.

**Reference:** ADR 0026 principle 7 (`docs/adr/0026-architecture-principles.md:100-114`), principle 8 ("errors are values").

**Fix:** Give both `ctx` and an `error` return (the assembler logs a failure, as it logs a `Store` failure) and name them in principle 7, or fold them into `endpoint.Store`.

**API break:** yes (two signatures on `endpoint.Config`; the facade's `EndpointPersistence` adapts).

**Confidence:** confirmed.

## W2-api-08 — low — three stale citations in the store contract

**Where:** `store/store.go:10-20` ("migration 006", "[sqlite.Open] applied migration 006"), `store/port.go:11` ("ADR 0026 principle 1"), `notes/parity/by_design.md:691` (`store/endpoints.go::allocateEndpointID`, "`matter_metadata` (migration 035)").

**Claim:** `sqlite.Open` is the reference host's package, not this module's; `store/doc.go:9-14` gives the actual contract (`Schema`, `Apply`, `Migrations`). `port.go` cites principle 1 for "a consumer names the area it uses" — principle 1 is "Layers and direction"; the persistence rule is principle 7. The by_design row that pins endpoint-number monotonicity cites a file and a table that do not exist; the allocator is `endpoint/sqlitestore/sqlitestore.go:246-300` over `matter_endpoint_allocation`. A host reading the store contract is sent to the wrong places.

**Reference:** `docs/adr/0026-architecture-principles.md:43`, `:100`; `ls store/endpoints.go` (absent).

**Fix:** Rewrite `store.go:10-20` to point at `Apply` / `Migrations`; cite principle 7 in `port.go`; refresh the by_design row's citation.

**API break:** no.

**Confidence:** confirmed.

## W2-api-09 — low — PartsList is computed in two layers by two methods

**Where:** `endpoint/assembler.go:220-250` (`buildEndpointTree`: bridged endpoints' `PartIDs`, full-family rule), `node/root.go:529-545` (`attachPartsListProviders`: root and Aggregator lists by walking `br.Topology()` with `isRootChild`), `bridge/bridge.go:1674-1693`, `:1715-1735` (the two provider ports, `bool` return).

**Claim:** The assembler computes every bridged endpoint's PartsList from the topology it builds; the root's and the Aggregator's are computed by the consumer through a closure the bridge only installs. A host that mounts its own root Descriptor must re-implement the facade's walk (including the "the OTA provider's endpoint is the root's part, not the Aggregator's" rule) and check a `bool`, or serve a frozen list. The bridge already walks the root clusters in `reassembleLocked` and `AttachRootClusters` for other wiring (`wireDeviceLoad`, `wireCommissioned`), and holds the topology both lists derive from.

**Reference:** matter.js `DescriptorServer.ts:216-254` — one `#currentPartsListNumbers` computes the list for every endpoint, root and Aggregator included, from the node's index.

**Fix:** Compute both lists in the assembler (root: every endpoint; Aggregator: its full family) and let `reassembleLocked` install them on the published Descriptors through `PartsListProviderSetter`; the two `Attach…PartsListProvider` ports then go.

**API break:** yes if the ports are removed; none if they stay as overrides.

**Confidence:** confirmed.

## W2-api-10 — low — `AttachACLLister` after `Start` is the one cluster-surface-adjacent port that does not apply live

**Where:** `bridge/bridge.go:1540-1556` (`AttachACLLister`: stores the field), `:1004-1012` (applied only in `reassembleLocked`), `:1640-1662` (`AttachRootClusters` publishes onto the live topology at once), `endpoint/dispatcher.go:116-120` (`SetACLLister`: plain field write), `docs/guides/advanced-seams.md:106` ("`AttachACLLister` replaces").

**Claim:** `AttachRootClusters`, `AttachAggregatorClusters`, `AttachBDXResponder`, `AttachMetrics` and the ICD ports take effect on a running bridge; `AttachACLLister` and `AttachAuxiliaryACL` wait for the next `Reassemble`, which on a node whose devices do not change may never come. The package doc records the difference ("Before Start"), `advanced-seams.md` says only that the call "replaces". A host that rotates its lister on a running node keeps enforcing the old one indefinitely.

**Reference:** doc contract versus code; ACL evaluation itself is the `sec` theme's.

**Fix:** Apply the lister to the live dispatcher in `AttachACLLister` / `AttachAuxiliaryACL` (the dispatcher's setters are plain writes, so the swap belongs under `b.mu` or the dispatcher gets an atomic), or state in `advanced-seams.md` that the replacement takes effect at the next `Reassemble`.

**API break:** no.

**Confidence:** confirmed for the behaviour; the fix's locking is for the `conc` reviewer.

## W2-api-11 — note — `MatterRead` has no context while `MatterReadFiltered` does

**Where:** `contract/matter.go:49-53` versus `:91-100`.

**Claim:** A server whose read consults a backend cannot observe cancellation or the reading fabric unless it implements the optional `FabricScopedReader`; the two read methods of one interface family differ in shape for no stated reason. Not a defect (reads are expected to be in-memory), but a host author notices.

**Fix:** none now; worth a sentence in `contract.ClusterServer`'s doc that `MatterRead` is expected to answer from memory.

**API break:** a signature change would be.

**Confidence:** confirmed.

## W2-api-12 — note — optional port capabilities are unexported interfaces

**Where:** `bridge/securechannel.go:971-983` (`gracefulCloseNotifierSetter`, `reannounceTriggerSetter`), `bridge/metrics.go:62-66` (`sessionCounter`), `bridge/case_initiator.go:112-116` (`peerSessionLister`), `bridge/bridge.go:1322-1326` (`fabricSessionCloser`), `:1246-1290` (`matterEndpointReceiver`, `matterSwitchSubscribable`, `matterSwitchPositionReporter`).

**Claim:** Eight capabilities a `SessionRegistry` or a `MeasurementSource` may implement are discovered by type assertion against unexported interfaces; a host with its own registry or source cannot find them from the API (`ports.md` names the two switch methods by name, the session ones not at all). With `secure/operational.Manager` and the module's switch server as the only implementations today this costs nothing; it is the shape a second host stumbles on.

**Fix:** Export them in `contract/` (switch) and `bridge/` (session) when a second implementation appears; list them in `ports.md`.

**API break:** additions only.

**Confidence:** confirmed.

## W2-api-13 — note — `Bridge.Topology()` hands out the live tree

**Where:** `bridge/bridge.go:1930-1936`, `endpoint/types.go:619-623` (`Topology.Endpoints []*Endpoint`, exported mutable fields).

**Claim:** The pointer a host receives is the one the dispatcher serves; a host that appends to `Endpoints` or edits `PartIDs` races every dispatch. The facade wraps it (`node.Topology()` returns a value), and the examples only read it. Belongs with W2-api-04: the read-only contract of `Endpoint` / `Topology` is nowhere stated.

**Fix:** One sentence on `Topology()` and on the `Topology` type.

**API break:** no.

**Confidence:** confirmed.

---

## Checked and sound

- Endpoint numbers are stable across `Reassemble` for an unchanged source: `assignOrReuseID` (`assembler.go:296-333`) reuses the stored number by `StableKey`, refreshes only device type and scope, and both stores allocate from a persisted counter that never rewinds and skips holders (`sqlitestore.go:246-300`, `memstore.go:52-125`) — the Go form of `ServerEndpointStores.ts:109-183` `assignNumber` / `#nextFreeNumber`; a part is numbered by its own key (`buildEndpointTree`). Recorded in by_design BD-Matter-EndpointNumber-Monotonic.
- A `contract.StatusError` reaches the wire with its status and cluster status on write and invoke, by value or by pointer, status 0 answering FAILURE (`dispatcher.go:796-852`, `classifyError` at `:382`, `:457`, `:803`); no bridge path calls `MatterWrite` / `MatterInvoke` around the dispatcher.
- The noop defaults do what the package doc says: `noopSessionLookup` misses, `noopPaseHandler` / `noopCaseHandler` return the `…Missing` errors, `noopSubscriptionStore` persists nothing, the nil ACL lister denies (`dispatcher.go:112-115`), a nil BDX responder leaves the exchange to the unknown-protocol path. Fail-closed / silent labels match the code for every port I read.
- `New` validates and installs every default; `Start` is CAS-claimed and resets on failure; `Reassemble` before `Start` is `ErrNotStarted`; a refused topology (`DeviceTypeConformanceError`) leaves the old one serving (`reassembleLocked` returns before the swap); `Stop` is idempotent and safe after a failed `Start`; a nil topology from the snapshotter fails at the call.
- Order dependence that exists is enforced or stated: `Attach…PartsListProvider` after the cluster call returns `false`; `AttachCommissioningWindow` and `AttachRootClusters` wire the window in either order (`wireCommissioned` in both); `AttachSubscriptionStore` before `Start` is documented and `beginSubscriptionRun` runs after the last failable step.
- Context is threaded through `Snapshotter`, `endpoint.Store`, `SubscriptionStore`, `CaseInitiatorProvider`, `OperationalResolver`, `BDXResponder`, `MatterWrite` / `MatterInvoke`, and `announceAddedEndpoints`; the hooks that take none (`PartsList` provider, `AvailabilityProbe`, `SetOnInboundActivity`) are in-memory by contract.
- `store.Port` states its guarantees (atomic methods, fabric cascade, foreign-fabric refusal, monotonic fabric index, caller-owned slices) and `storetest` pins the foreign-fabric refusal (`storetest.go:715-730`); sentinel errors are documented per area.
- `bridgetest` and `endpointtest` reach the bridge only through `internal/bridgeseam` (`bridge/seam.go`), as their package docs claim; `endpointtest.NewFakeStore` starts at 2, consistent with the stores.
- `SetOnReassembled` reports `Topology.Bridged()` (root, Aggregator and parts excluded), as its doc says.
- `application_values.go` and `device_load.go` are internal; the structured-write limitation of `attribute_value_reader.go` is documented in the file and in `docs/matter-ecosystem-observations.md:62`.

## Not read

- `bridge/receive.go`, `receive_dispatch.go`, `reply.go`, `subscribe*.go`, `handlers.go`, `conn.go`, `outbound_reliable.go`, `ackpump.go`, `commissioning_window.go`, `group_messaging.go` beyond the port definitions (wire, conc and sec themes).
- `bridge/attribute_value_reader.go:140-420` (the per-attribute decoders) and `fields_reader.go`.
- `endpoint/dispatcher.go` outside the slices named in the scope (ACL evaluation is the `sec` theme's); `devicetype_conformance.go`, `devicetype_model.go`, `devicetype_subject.go`, `devicetype_validation.go` (ADR 0016; the mode contract was read from `bridge/devicetype_validation.go`); `materialize.go:70-577`; `binding_mount.go`; `surface.go`.
- `store/` implementations (`fabric.go`, `acl.go`, `migrations.go`, …) and `store/memstore`; `endpoint/sqlitestore/sqlitestore.go` bodies beyond the allocator outline.
- `contract/matter.go:100-914` beyond the interface inventory; `contract/change.go`, `switchevents.go`, `metrics.go` bodies.
- `node/` beyond the lines cited (W5).
