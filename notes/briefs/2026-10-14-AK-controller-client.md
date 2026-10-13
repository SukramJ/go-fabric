# Brief AK — `controller/client`: the client node model — the typed image of a remote node, its cache, the structure a peer's Descriptor describes, the default sustained subscription that keeps it live (ADR 0020, stage 4, first half)

Owner: Fable. Implementer: Opus (one agent; nothing else runs on the repo
while you do). Branch `wave17/client` from `planning/briefs-wave-17`,
pushed, **no PR**. Report in `notes/briefs/reports/AK.md`. Read first:
`notes/briefs/README.md`, [ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md)
(decision 6: *no device model of its own in the controller*), concept v2
§B.1, the reports AH–AJ (the owner's decisions at their end are rules),
and the packages this one builds on: `controller/interaction`
(`Interaction`, `ReadRequest`, `Report`, `SubscribeRequest`,
`Subscription`, `runSustained`), `controller/peer` (`Peer`, `Set`,
`Descriptor`), `controller/commissioner` (`Commissioner.Interaction`,
`Result`), `cluster/spec` (`Cluster`, `Attribute`, `Type`, `Kind`,
`Node`, `Lookup`, the `Decode*` codecs), `schema/` (`ClusterName`,
`ClusterFeatureNames`, `AttributeChangesOmitted`, `DeviceTypeDefinitionOf`,
`Composition`), `cluster/spec/descriptor` (`DeviceTypeStruct`,
`AttrDeviceTypeList`, `AttrServerList`, `AttrPartsList`). The live test
of stage 3, `controller/interaction/live_test.go` (`liveNodeAt`, the
commissioner over a `node.Node` in-process), is the pattern for the
proofs here.

matter.js at **`0ceb202d`**; the `../matter.js` checkout stands later —
read with `git -C ../matter.js show 0ceb202d:<path> | cat -n`. Paths
below are relative to `packages/`; CS = `node/src/node/client/ClientStructure.ts`,
PB = `node/src/node/client/PeerBehavior.ts`, CNI =
`node/src/node/client/ClientNodeInteraction.ts`, CN =
`node/src/node/ClientNode.ts`, CNL = `node/src/node/ClientNodeLifecycle.ts`,
CEE = `node/src/node/client/ClientEventEmitter.ts`, NC =
`node/src/behavior/system/network/NetworkClient.ts`, DC =
`node/src/storage/client/DatasourceCache.ts`, CBB =
`node/src/behavior/internal/ClientBehaviorBacking.ts`, NPP =
`node/src/node/NodePhysicalProperties.ts`, RS =
`protocol/src/action/client/ReadScope.ts`, PDP =
`protocol/src/peer/PhysicalDeviceProperties.ts`, SS =
`protocol/src/action/client/subscription/SustainedSubscription.ts`, P =
`node/src/node/client/Peers.ts`. Read the lines a fact cites before you
mirror them; a brief line that differs from the file is a line to correct
in the report. One commit per § (the sections below), reachability in
each, CHANGELOG under `[Unreleased]` → Added (one entry for the package;
Changed for the `interaction` hook of §7). Guards: `make ci`, `make race`,
`make reachability-check`, and `make chiptool-test` once at the end
(export `PATH=$HOME/go/bin:$PATH` first: the lint tools live there). You
may add `controller/client`, change `controller/interaction` (§7 only)
and `controller/peer` (§5a only),
add one hand-written file to `cluster/spec` (§2, `decode_value.go`), and
add to `script/coverfloor/floors.go`;
`controller/commissioner`, `secure/`, `transport/`, `bridge/`, `node/`,
`store/`, `schema/`, `device/` and the generated files are read, not
changed — a change you need there is a question in the report
(`controller/peer` beyond §5a included).
`notes/parity/by_design.md` and the findings register are the owner's.

Out of this brief (the second half of stage 4 is brief AL): the `Peers`
registry — local ids `peerN` (`storage/client/ClientNodeStores.ts:23,
77-81`), restore on start (P:123-152), the address book and its expiry
(P:73-74, :615-712, `CommissioningClient.State`), `forAddress` (P:445-486),
the BasicInformation `leave`/`shutDown`/`startUp` reactions (P:727-729,
:785-893), the fabric-label reconciliation (NC:268-307), `commission` /
`decommission` / the commissioning windows on the node (CN:162-205), a
SQLite `Store`; also ICD (`IcdClient`, LIT routing, `holdUntilAwake`,
CNI:92-101, NC:90-97), groups (`ClientGroup.ts`,
`ClientGroupInteraction.ts`, NC:39-43), TCP and the transport preference
(NC:69-75), matter.js's own remote API (`applyWireChanges`, CS:389-432,
`node/src/node/remote/`), the local-write-through-cache of
`RemoteWriter.ts` and the compensation of DC:73-130 (owner's decision D6),
and the generated behaviour *types* of PB (owner's decision D2).

## What stage 4 is, after the first read

matter.js's `ClientNode` (CN:55) holds no hand-written model of the
remote node. Everything it knows comes from what the peer reports: the
`ClientStructure` (CS:115) turns the attribute values of reads and
subscription reports into endpoints and clusters — the Descriptor's
`deviceTypeList`, `serverList` and `partsList` arriving *in the same
report as the data* decide the tree (CS:851-1009), a cluster's own
global attributes decide its shape (CS:754-837, PB:422-467). The values
sit in one cache per (endpoint, cluster), keyed by attribute id, with the
cluster's DataVersion beside them (DC:47-48, :151-154); the cache is
persisted and reloaded at construction (CN:118, CS:183-232). One sustained
wildcard subscription keeps the image live (NC:113-196); every read and
every re-subscribe carries the cached DataVersions as filters so the peer
sends only what changed (CS:252-290, CNI:103-108, :148-155).

The Go port keeps that division. `controller/client` is the node image
over the stage-3 interaction and the stage-1 peer; its "typed" side is the
generated `cluster/spec` definitions and the `schema/` tables — the
module's one data model, as ADR 0020 decision 6 requires — not generated
behaviour types.

## What exists (reuse, do not duplicate)

- `interaction.Interaction.ReadWith(ctx, ReadRequest) (*Report, error)`
  (`read.go:56`) returns one merged `Report` whose `Attributes` are in
  wire order, each with `DataVersion` (0 when absent, `report.go:19-26`)
  and a `spec.Node` value; `Subscribe(ctx, SubscribeRequest)` with
  `Sustain`, `BootstrapRead`, `Updated` (one merged report per
  ReportData, the priming report too), `KeepaliveReceived`, `Closed`,
  `ActiveChanged` (`subscribe.go:47-86`); `Subscription.Active()`,
  `ID()`, `Close()`; `runSustained` (`sustained.go:144-229`) with
  `DefaultSustainRetries`; `Write`, `Invoke`.
- `peer.Peer`: `Address()`, `Descriptor()`, `HasSession()`,
  `NewestSession()`, `VerifyReachability`; `peer.Set.Context` has the
  `Disconnected`, `Updated`, `EstablishmentUnresponsive` hooks
  (`set.go:60-76`).
- `commissioner.Commissioner.Interaction(p *peer.Peer)` binds an
  interaction to the commissioner's `Subscriptions` (`commissioner.go:207`);
  `Result{Address, FabricIndexOnPeer}`.
- `spec.Lookup(clusterID) *spec.Cluster` for the 97 generated clusters;
  `Cluster.Attribute(id)`, `Command(id, dir)`, `Event(id)`, `Features`
  with `Feature.Mask()`; `Attribute.Type` (`Kind`, `Bits`, `Enum`,
  `Bitmap`, `Struct`, `Entry`), `Attribute.Conformance`,
  `Attribute.Quality.ChangesOmitted`, `Attribute.Decode` for struct and
  list-of-struct attributes; the global attribute ids `spec.AttrAttributeList`
  (0xFFFB), `AttrFeatureMap` (0xFFFC), `AttrClusterRevision` (0xFFFD),
  `AttrAcceptedCommandList` (0xFFF9), `AttrGeneratedCommandList` (0xFFF8),
  `AttrEventList` (0xFFFA) (`instance.go:17-22`); the codecs
  `spec.DecodeUint/Int/Bool/Float32/Float64/String/Bytes/List`
  (`codec.go:123-272`).
- `schema.ClusterName(id)` for all 135 snapshot clusters,
  `ClusterFeatureNames(clusterID, featureMap)`, `AttributeChangesOmitted(cluster, attr)`,
  `DeviceTypeName(id)`, `DeviceTypeDefinitionOf(id)` with
  `Classification` and `Composition` (`"tree"`, `"full-family"`),
  `ClusterClassification`.
- `cluster/spec/descriptor`: `AttrDeviceTypeList` 0x0000 (decodes to
  `[]DeviceTypeStruct{DeviceType uint32, Revision uint16}` through its
  `Decode`), `AttrServerList` 0x0001, `AttrPartsList` 0x0003.
- `im.ConcreteAttributePath` with its `Has*` wildcard flags,
  `im.DataVersionFilter{Endpoint, Cluster, DataVersion}`,
  `im.EventMinimumNumber`.

## Owner's decisions (rules for this brief)

- **D1 — package and names.** `controller/client`, package `client`:
  `Node` (the image of one remote node; CN), `Endpoint`, `Cluster` (the
  structure's entries), `Store` + `MemoryStore` (the persistence seam),
  `Config`, `ConnectionState` (CNL:16-40), `Properties` (PDP:32-61). The
  `ClientStructure` is the unexported `structure` inside `Node`; its
  public face is `Node.Endpoints()`, `Node.Endpoint(n)`,
  `Endpoint.Clusters()`, `Endpoint.Cluster(id)`, `Cluster.Value(attr)`
  and the structure callbacks of §3.
- **D2 — no generated client types.** PB builds a TypeScript class per
  discovered shape (PB:105-139, :169-285) and memoises it by a fingerprint
  (PB:290-378). Go has no run-time types: a `Cluster` carries the
  *analysis* of PB:422-467 (definition, revision, features, supported
  attributes and commands, the extra ids) and the values stay `spec.Node`,
  decoded on access through the definition's `Attribute.Decode` or the
  new `spec.DecodeValue` (§2). The fingerprint cache is not ported. The
  owner writes `BD-Matter-ClientNode-NoGeneratedTypes`.
- **D3 — known, named and unknown clusters.** matter.js knows every
  cluster of its model and names an unknown one `Cluster$<hex>`
  (PB:399-401, :426-427). Here a cluster is *known* when `spec.Lookup`
  has its definition, *named* when only `schema.ClusterName` has it
  (38 clusters), *unknown* otherwise. A named cluster gets its name and no
  attribute types (its values are untyped `spec.Node`, its support
  overrides are not computed — PB:484-486 applies as if unknown); an
  unknown one is named `Cluster$<hex>` and an unknown attribute
  `attr$<hex>` (PB:253, :399-401). The owner notes the 38 in the
  findings register (they are generator scope, not this brief's).
- **D4 — events.** CEE drops an event of an unknown cluster or event with
  a one-time notice (CEE:156-175) because it has no observable to emit
  on. Here events reach the host through one callback with the path and
  the decoded-or-raw value, for every cluster; the one-time notice is kept
  as a log line at `Info` for an unknown cluster or event (the owner
  writes `BD-Matter-ClientNode-UnknownEventsDelivered`). The event
  cursor `maxEventNumber` is kept and persisted as CEE:70-76 does.
- **D5 — persistence.** The `Store` interface of §4 replaces
  `ClientNodeStore`/`ClientEndpointStore`/`LocalWriter`
  (`storage/client/`), with the same layout
  (node → endpoint number → cluster id → attribute id, plus the version)
  and the same batching rule as `ClientCacheBuffer` (a flush interval; none
  means write-through). `MemoryStore` is the only implementation in this
  brief; the SQLite one is AL's.
- **D6 — writes go to the peer, not through the cache.** `RemoteWriter.ts`
  and DC:73-130 implement "set state locally, write remotely, compensate
  on refusal". Here `Node.Write` forwards to `Interaction.Write`; the
  image changes when the peer reports the new value (the subscription
  does that). The owner writes `BD-Matter-ClientNode-WriteNotThroughCache`.
- **D7 — report granularity.** `structure.mutate` consumes chunks
  (CS:325-355) and batches per consecutive (endpoint, cluster)
  (CS:479-492). `interaction.Report` is the merged ReportData; the batching
  per consecutive (endpoint, cluster) is done over `Report.Attributes` in
  order, and the structural changes are applied once per report
  (CS:375-379) — the same points, one level coarser.
- **D8 — concurrency.** matter.js serialises the deferred persist/emit
  jobs in a FIFO mutex with back-pressure at 100 pending jobs
  (CS:299-320, :349-351, :57). Here `mutate` runs under the node's mutex
  on the caller's goroutine (the subscription's `Updated` or the `Read`
  caller) and the store write is synchronous or buffered (§4); there is no
  job queue. Callbacks (§3, §5) are invoked after the mutex is released,
  in the order of §3.

## §0 `controller/client`: package doc and `Config`

`doc.go`: the package is the port of CN, CNL, CS, PB, CNI, CEE, NC
(the subscription and connection-state half), DC and NPP/PDP at
0ceb202d; what it leaves to AL and later (the "Out of this brief" list,
condensed); the data model statement of D2/D3.

```go
type Config struct {
    // ID is the node's local id, the store key ("peer1"; AL allocates them).
    ID string
    // Peer is the remote node (its address, sessions and connection process).
    Peer *peer.Peer
    // Interaction is the interaction bound to Peer — commissioner.Interaction(peer).
    Interaction *interaction.Interaction
    // Store persists the image; nil is a MemoryStore of this node's own.
    Store Store
    // FlushInterval batches store writes (ClientCacheBuffer.ts:41-48); 0 writes through.
    FlushInterval time.Duration
    // AutoSubscribe starts the default subscription in Start (NC:432-439; a
    // newly commissioned node sets it, CommissioningClient.ts:308-327).
    AutoSubscribe bool
    // DefaultSubscription narrows the default subscription (NC:412-421); nil is the wildcard.
    DefaultSubscription *SubscribeOptions
    // Callbacks: §3 (structure), §5 (connection state, events, seeded).
    Hooks Hooks
    Logger *slog.Logger
    Clock  exchange.Clock
    Rand   func() float64
}
```

`New(cfg Config) (*Node, error)` loads the cache from the store
(§4, CS:183-232 — synchronous, CN:105-133) and installs nothing else;
`Start(ctx)` (CN `start` → NC:60-102) binds the peer hooks, runs the
default subscription when `AutoSubscribe` and not disabled, and
recomputes the connection state; `Stop(ctx)` closes the subscription and
flushes the store; `Close(ctx)` stops and releases. `Disable(ctx)` /
`Enable(ctx)` (CN:236-264): disabled is persisted (NC:344-349), a disabled
node drops its subscription first (CN:244-249) and `Start` of a disabled
node is a no-op (ClientNetworkRuntime.ts:38-43 throws; here `ErrDisabled`).
`Erase(ctx)` (CN:227-229, :266-271): stop, clear the image and the store.

## §1 `scope.go`: the read scope (RS, 114 lines)

Port RS:38-106 over `[]im.ConcreteAttributePath`: `scope{isRelevant,
isWildcard func(ep uint16, cluster uint32) bool; fabricFiltered bool}`
built from a request's attribute paths. `isRelevant` is the tester for
"any" (RS:41), `isWildcard` for "all" (RS:46): a path naming an attribute
is ignored for "all" (RS:63-66); a path with wildcard endpoint and
wildcard cluster short-circuits to always (RS:69-73); a wildcard-endpoint
path with a cluster adds the cluster to the "all endpoints" set (RS:76);
an endpoint with wildcard cluster marks the endpoint whole (RS:81-84);
endpoint + cluster without attribute adds the cluster to the endpoint's
set (RS:86-96); no attribute paths → never (RS:55-57). `fabricFiltered`
is `!NoFabricFilter` (RS:50: `isFabricFiltered ?? true`). Table test
over the branches.

## §2 `cluster.go` and `cluster/spec/decode_value.go`: the cluster shape (PB, CS:754-837)

```go
type Cluster struct {
    ID         uint32
    Name       string        // D3: definition's name, schema's name, or "Cluster$<hex>"
    Definition *spec.Cluster // nil for a named or unknown cluster
    Revision   uint16        // the peer's ClusterRevision, 0 when not reported
    FeatureMap uint32        // the peer's FeatureMap
    Features   []string      // schema.ClusterFeatureNames(ID, FeatureMap); nil for a non-generated cluster
    // AttributeList, AcceptedCommands, GeneratedCommands: the peer's lists, sorted
    // (CS:782-784, :798-800, :804-806); AttributeListReported tells whether
    // AttributeList is the peer's list or the ids received (CS:779-795).
    ...
    // Version is the cluster's DataVersion; HasVersion false is UNKNOWN_VERSION (DC:47-48).
}
```

Rules:

- The shape is (re)computed from the cache's global attributes in
  `synchronizeCluster` (CS:762-808): revision only when a number
  (CS:771-773); features (CS:775-777 — the TS value is a decoded bitmap
  object; here the uint32 of the report); a non-empty AttributeList is
  authoritative, an empty or absent one falls back to the attribute ids
  received (CS:779-795, `authoritativeAttributeList` CS:101-108: an empty
  list is ignored); command lists when arrays (CS:797-807). The shape is
  "built" once something useful exists (CS:810-817: any attributes,
  commands or generated commands) — before that `Cluster.Ready()` is
  false and the cluster is not announced (§3).
- `Supports(attrID) bool` / `SupportsCommand(cmdID) bool`: PB:478-507 over
  the definition — an element the peer lists is supported; a global
  attribute is always supported (PB:490-492); for a named or unknown
  cluster the lists alone decide (PB:484-486). The "override" bookkeeping
  (mandatory-but-absent, optional-but-present, deprecated-but-present
  PB:386-397) is not materialised: `Supports` is the function those
  overrides feed; `Extra() (attrs, cmds []uint32)` returns the ids the
  definition does not know (PB:444-455).
- Revision override rule PB:429-434: a reported revision below 1 is not
  a revision (`Revision` stays 0).
- `Value(attrID) (spec.Node, bool)` from the cache; `Decoded(attrID)
  (any, error)`: `Definition.Attribute(id).Decode` when set, else
  `spec.DecodeValue(attr.Type, node)`; a named/unknown cluster returns
  `ErrUntyped` with the node.
- `spec.DecodeValue(t Type, n Node) (any, error)` — new, hand-written
  `cluster/spec/decode_value.go`: by `Kind` — `KindBool` → bool,
  `KindUint`/`KindEnum`/`KindBitmap` → uint64 (through `DecodeUint` at
  `t.Bits`, 64 when 0), `KindInt` → int64, `KindFloat32/64`,
  `KindString`, `KindBytes`, `KindList` → `[]any` of `DecodeValue(t.Entry.Type)`,
  `KindStruct` → `map[uint32]any` by field id over `t.Struct.Fields`,
  `KindAny` → the `Node` itself; TLV null → `nil, nil`. It is the
  attribute-level counterpart of `TlvOfModel` for values the generated
  decoders do not cover (InputChunk.ts:89-124 decodes every attribute by
  its model; here the typed decode is on access). Round-trip test against
  the generated encoders of three clusters (onoff, descriptor,
  powersource) through `spectest`.

## §3 `structure.go`: endpoints, clusters, the Descriptor-driven tree (CS)

State (CS:116-146): `endpoints map[uint16]*Endpoint` (root created at
construction, CS:159-162), per endpoint `clusters map[uint32]*Cluster`
and `pendingOwner`; `pendingChanges map[*Endpoint]pendingChange{erase,
install, rebuild}` (CS:1634-1649); `partClaims map[*Endpoint]set[*Endpoint]`,
`partsListOf map[*Endpoint]set[uint16]`, `composition map[*Endpoint]schema.Composition`
(CS:127-146); `delayedEvents []interaction.EventData` (CS:125);
`subscribedFabricFiltered bool` (CS:437-460: true unless the default
subscription says `NoFabricFilter`).

`mutate(req, report *interaction.Report)` (CS:295-380, D7/D8):

1. For each attribute value in order, `mutateAttribute` (CS:462-504):
   skip the value when `subscribedFabricFiltered != scope.fabricFiltered`
   (CS:468-471 — the cache holds one fabric view); record the endpoint in
   `endpointsWithData` (CS:477); when the (endpoint, cluster) changes,
   flush the batch through `updateCluster` (CS:479-484); start a batch
   with the value and, **only when `scope.isWildcard(ep, cluster)`**, the
   report's DataVersion (CS:494-497).
2. For each event, `emitEvent` (CS:506-528): delayed to the end of the
   report when the endpoint is unknown or not installed, received
   attribute data in this report, or has a pending structural change
   (CS:518-524); else delivered after the data (CS:526 — here: appended
   to the callbacks list, D8).
3. Attribute statuses and event statuses are logged at `Debug`
   (CS:338-346).
4. Flush the last batch (CS:357-361); `applyPendingChanges` (CS:1269-1288);
   emit structure events (CS:1503-1551) then the delayed events
   (CS:1553-1563).

`updateCluster(batch, clustersWithData)` (CS:572-587): record the cluster
in `clustersWithData` and cancel a pending delete ("Schrödinger's
cluster", CS:555-565, :576-580, log at `Info`);
`pruneUnlistedAttributes` (CS:668-681, :690-707: with an authoritative
AttributeList in the batch, every *stored* attribute id not in the list
and not global is deleted — the batch's own values are kept);
`invalidateOnDefinitionChange` (CS:632-659: a changed sorted AttributeList,
ClusterRevision, FeatureMap or AcceptedCommandList resets the shape so it
is recomputed); `cache.externalSet(values)` (§4); `synchronizeCluster`.

`synchronizeCluster` (CS:754-849): recompute the shape when reset (§2);
when it becomes ready: if the endpoint is installed, queue it as the
cluster's pending shape and schedule `rebuild` when the cluster was
already announced, else `install` (CS:825-831); if not installed, set it
directly (CS:832-835). Then, for the Descriptor cluster (CS:839-848),
`synchronizeDescriptor` over the cluster's current values.

`synchronizeDescriptor` (CS:851-1009), decoded with
`descriptor.Definition.Attribute(AttrDeviceTypeList).Decode` and
`spec.DecodeList(DecodeUint[uint32](32))` / `DecodeUint[uint16](16)` for
the id lists:

- `deviceTypeList` (CS:858-905): `noteComposition` first (CS:1457-1478: a
  list with any numeric entry makes the composition known; full-family
  when any entry's `schema.DeviceTypeDefinitionOf(id).Composition ==
  "full-family"`, else tree; an unknown device type counts as tree); then
  per entry in order: `isApp` when the definition's `Classification` is
  not a utility one (`DeviceClassification.isApplication` =
  `!isUtility`, utility being `"utility"` or `"node"`,
  `model/src/common/DeviceClassification.ts:52-65`; an unknown device
  type is not an application type, CS:870-874); the
  root endpoint takes only the revision (CS:876-880); a non-app type is
  skipped once a type other than the unknown sentinel is set
  (CS:882-892); set `DeviceType`, `DeviceRevision`, `Classification`
  (default `"simple"`, CS:896), `Name` = `schema.DeviceTypeName` or
  `Unknown#<hex4>` (CS:897); stop at the first app type (CS:901-903).
  `Endpoint` fields: `Number`, `DeviceType uint32` (`UnknownDeviceType`
  sentinel = `math.MaxUint32`, matter.js `DeviceTypeId(-1)`,
  `EndpointType.ts:43-44`), `DeviceRevision`, `Classification`, `Name`
  (`"Unknown"` at creation, CS:1026-1030), `Owner *Endpoint`, `Installed`.
- `serverList` (CS:907-939): `clusterFor` each listed id (CS:915-920);
  every *announced* cluster not listed is marked `pendingDelete` unless
  it has a pending shape or data arrived for it in this report
  (CS:922-934); any marked → schedule `rebuild`.
- `partsList` (CS:941-1008): invalid entries skipped (CS:953-955;
  `EndpointNumber.isValid`: 0 to 0xFFFE,
  `types/src/datatype/EndpointNumber.ts:19-24`); each named part gets
  this endpoint as a claimant (CS:957-966); `retractClaimsOf` for parts
  no longer named (CS:1426-1447); record the list (CS:969). For the
  **root** list: every endpoint of the image the root no longer names is
  scheduled `erase`, unless it holds a descendant the root still names
  (kept, warning, CS:971-1008).

`resolvePartClaims` (CS:1300-1331) with `ownerFor` (CS:1360-1403),
`isPlaced` (CS:1337-1339), `isOnTheNode` (CS:1349-1357),
`partsAccountedFor` (CS:1405-1417), `isFullFamily` (CS:1481-1483):
literal port, including the warning for several ordinary claimants
(CS:1382-1390) and "nothing is reparented" (CS:1305-1310).

`applyPendingChanges` (CS:1269-1288): claims first, then per endpoint
`erase` (CS:1130-1161: remove from every map, delete descendants too),
`rebuild` (CS:1171-1222: a `pendingDelete` cluster's structure is
discarded entirely and its store erased, event `ClusterDeleted`; a pending
shape replaces or adds, event `ClusterReplaced`/`ClusterInstalled`),
`install` (CS:1227-1263: set `Owner` from `pendingOwner`, event
`EndpointInstalled`; pending shapes become the shape, `ClusterInstalled`
only when the endpoint was already installed — an endpoint's event
carries its clusters, `ClientStructureEvents.ts:88-107`).

`loadCache` (CS:183-232): endpoints from the store in any order; per
endpoint the Descriptor cluster first (CS:199-205); per cluster
`checkUnlistedSeedValues` (CS:721-744: a cached cluster holding id-keyed
values its cached AttributeList omits forgets its version, so the next
wildcard read refetches the cluster; the name-keyed legacy branch
CS:734-737 does not apply — Go keys by id only), then
`synchronizeCluster`; then `resolvePartClaims`; only `install` may be
pending (else `errCorruptCache`, CS:217-229); install all; emit structure
events.

Structure callbacks (`Hooks`): `EndpointInstalled(*Endpoint)`,
`ClusterInstalled(*Endpoint, *Cluster)`, `ClusterReplaced`,
`ClusterDeleted`, `Changed()` (CS:166-168, :1550). Errors in hooks are
logged, never raised (`ClientStructureEvents.ts:138-144`).

Tests (table-driven, no network): a report of a bridge (root with
Aggregator 0x000E on endpoint 1 full-family, two bridged endpoints under
it, each with BridgedDeviceBasicInformation and OnOff) installs the tree
with the right owners; a tree-composed endpoint; a part claimed by both a
full-family and an ordinary list goes to the ordinary one with the
warning; a part whose claimant's composition is unknown waits; a root
list dropping an endpoint erases it; a server list dropping a cluster
deletes it unless data arrives in the same report; an AttributeList
shrinking prunes the stored values; a changed FeatureMap recomputes the
shape; the version is stored only from a wildcard scope; a fabric-filter
mismatch stores nothing; the cache reload installs the same tree and
emits the events once; `checkUnlistedSeedValues` forgets the version.

## §4 `cache.go`, `store.go`: the per-cluster cache and the store (DC, `storage/client/`)

```go
// Store persists a node's image: endpoint → cluster → attribute values and the
// cluster's DataVersion (storage/client/ClientNodeStore.ts, ClientEndpointStore.ts,
// LocalWriter.ts; layout nodes/<id>/endpoints/<ep>/<cluster>/<attr>, version "__version__").
type Store interface {
    Load(nodeID string) ([]StoredEndpoint, error)                    // every endpoint with every cluster's values
    Save(nodeID string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error // nil deletes a key (EndpointStore.ts:58-131); version nil = unknown
    EraseCluster(nodeID string, ep uint16, cluster uint32) error     // DC erase (CS:1189)
    EraseEndpoint(nodeID string, ep uint16) error                    // ClientEndpointStore.erase
    EraseNode(nodeID string) error                                   // ClientNodeStore.erase
}
```

Values are the attribute's TLV bytes (the `spec.Node` re-encoded with
`tlv`; a value is decoded on load through `spec.ReadElement`). The
node's own settings (`disabled`, `autoSubscribe`, `maxEventNumber`,
`defaultSubscription` — NC:331-372, all quality N) are stored under a
reserved cluster id of endpoint 0: `settingsCluster = 0xFFFF_FFFF`
(not a cluster id) with attribute ids 0..3 — the owner's choice for the
`network` behaviour context of `ClientNodeStores`.

`cache` (one per `Cluster`): `values map[uint32]spec.Node`, `version
uint32`, `hasVersion bool` (DC:47-48), `dirty set[uint32]` and
`versionDirty`. `externalSet(values map[uint32]*spec.Node, version
*uint32)` (DC:132-174): a nil value deletes the key (EndpointStore
semantics); the version is updated when present (DC:151-154); with a
flush interval the keys are marked dirty and the node's flusher is armed
(DC:156-160), else the store is written now (DC:162-163).
`invalidateVersion()` (DC:199-201). The flusher (`ClientCacheBuffer.ts`):
one timer per node at `FlushInterval`, `flush` writes every dirty cache
in one pass (`#doFlush` 103-153: a failed cache keeps its dirty keys and
is retried next cycle, DC:291-303), `Stop`/`Close` flush first
(`ClientCacheBuffer.ts:94-101`). A `Store` error is logged at `Warn`,
never raised (DC:297-303).

Tests: write-through and buffered modes with a fake clock; a failing
store retries; `Load` round trip of values and version; erase paths;
`MemoryStore` concurrency under `-race`.

## §5 `node.go`: the node (CN, CNI, NC, CNL, CEE)

- `Read(ctx, req ReadRequest) (*interaction.Report, error)` (CNI:91-112):
  `ReadRequest` embeds `interaction.ReadRequest` plus `IncludeKnownVersions
  bool` (`ClientRead.ts:12`); unless set, and when `!NoFabricFilter ==
  subscribedFabricFiltered` (CNI:103-106), inject the version filters
  with `refreshChangesOmitted` (CS:252-290: for every endpoint/cluster of
  the image that `scope.isRelevant` covers and whose version is known,
  one `im.DataVersionFilter`; skipped when the cluster has a
  changes-omitted attribute — CS:1668-1692: without the peer's
  AttributeList every such attribute of the definition counts, with it
  only listed ones; `schema.AttributeChangesOmitted` is the table); then
  `Interaction.ReadWith` and `mutate` over the report. The caller gets
  the report as the peer sent it (chunks merged).
- `Subscribe(ctx, req SubscribeRequest) (*interaction.Subscription, error)`
  (CNI:125-169): inject the version filters (no changes-omitted refresh,
  CNI:128); interval bounds from §6 unless the request sets them
  (CNI:129-133); `Updated` wraps the caller's: `mutate` first, then the
  caller (CNI:139-146); `RefreshRequest` (§7) clears `DataVersionFilters`,
  sets `EventFilters` to `maxEventNumber+1` and re-injects the versions
  (CNI:148-155).
- `Write(ctx, ...)`, `Invoke(ctx, ...)` forward (CNI:175-186, :197-210; D6).
- The default subscription (NC:113-196): `Subscribe{NoFabricFilter:false,
  ReplaceSubscriptions:true, Attributes:[wildcard], Events:[wildcard
  urgent]}` overridden by `DefaultSubscription` (NC:121-128; the wildcard event
  path with `im.ConcreteEventPath.IsUrgent`, `im/event_report.go:76-78`).
  A node marked `NewlyCommissioned`
  (`Config.NewlyCommissioned bool`, NC:137-143, set by whoever
  commissioned it) first performs a plain `Read` of the same request
  (NC:140) unless `Config.NoInitialRead` (`autoStateInitialize === false`,
  NC:139); then, when a subscription is desired and none is active,
  `Subscribe` with `Sustain:true`, `EventFilters:[maxEventNumber+1]`,
  `BootstrapRead` set unless the initial read ran (NC:149-156), `Updated`
  draining into `mutate` and emitting `SubscriptionAlive` when the
  subscription id is not `NoSubscription` (NC:157-166),
  `KeepaliveReceived` likewise (NC:167-172), `Closed` clearing the active
  subscription (NC:173-178), `ActiveChanged` → `SubscriptionStatusChanged`
  and, when active, `SubscriptionAlive` (NC:181-188).
  `subscriptionActive` (NC:203-209) is `Subscription.Active()`.
- Connection state (CNL:16-40, NC:215-238): `Connected` iff the
  subscription is active and the peer has a session (NC:223); else
  `Disconnected` when disabled or stopped (NC:226-230, clearing the
  likely-offline latch); else `WaitingForDeviceDiscovery` when the latch
  is set (NC:231-232); else `Reconnecting`. The latch is set by the peer
  set's `EstablishmentUnresponsive` (NC:81, :244-247) and cleared by a
  new session (NC:82, :254-257); the state is recomputed on session add
  and delete (NC:82-83), subscription status, disable, stop. `Hooks.
  ConnectionStateChanged(ConnectionState)` fires on real transitions
  only (CNL:101-107). The peer signals come from §5a.
- **§5a `controller/peer`: per-peer observers.** matter.js's node
  layer observes `peer.sessions.added` / `.deleted` (a `BasicSet`,
  Peer.ts:68, :258-260) and `peer.establishmentUnresponsive`
  (Peer.ts:90-91, :202-204) directly (NC:81-83). The Go `Peer` only has
  the set-wide `Set.Context` hooks. Add `Peer.Observe(o Observer) (stop
  func())` with `Observer{SessionAdded, SessionDeleted func(*exchange.Session);
  EstablishmentUnresponsive func()}`, called from `addSession`,
  `removeSession` and the unresponsive path (`connection.go:650`) after
  the peer's mutex is released, in registration order; `stop` removes
  the observer. The set-level hooks stay as they are. This is the one
  change to `controller/peer` this brief allows; CHANGELOG → Added.
- Seeded (CNL:51-75, P:747-759): `Seeded()` true once the root has a
  BasicInformation cluster (0x0028) with values and the image has more
  than one endpoint; `Hooks.Seeded()` once.
- Events (CEE:41-91, D4): `Hooks.Event(ep uint16, cluster, event uint32,
  e interaction.EventData, value any)` — `value` decoded through
  `Definition.Event(id).Decode` when known, else the raw node; dropped
  while the node is closing (CEE:47-53); `maxEventNumber` raised and
  persisted when the number is higher (CEE:70-76).
- `Properties()` (§6) recomputed lazily after every `Changed` and frozen
  while stopped (`ClientNodePhysicalProperties.ts:19-120`).

Tests: scripted (the stage-3 `scripted` harness in
`controller/interaction/helpers_test.go` is package-private — copy what
you need into `controller/client` test helpers or add an exported test
seam in `interaction` through an `export_test.go`; say which in the
report): the default subscription's request carries the filters and the
event minimum; a re-subscribe after loss carries the refreshed filters;
the connection-state table over (active, session, disabled, latch); the
initial read of a newly commissioned node; `IncludeKnownVersions`.

## §6 `properties.go`: physical properties and the interval bounds (NPP, PDP)

`Properties` (PDP:32-61) computed from the image (NPP:30-194): root
server list (NPP:31); `IntermittentlyConnected` = IcdManagement 0x0046 in
the root server list (NPP:44); `IdleModeDuration` from IcdManagement
attr 0x0000 (NPP:35, :46, seconds); `LongIdleTimeOperating` = LITS feature
(bit 2) of IcdManagement's FeatureMap and `OperatingMode` 0x0008 == Lit (1)
(NPP:33-34, :45 — `IcdClient.litSupported` not read: the owner's reading
is the LITS feature bit; cite NPP only); `SpecificationVersion` from
BasicInformation 0x0028 attr 0x0015 (NPP:48); device types of every
endpoint (NPP:110-113); per endpoint not behind an aggregator
(NPP:115-185, the aggregator rule NPP:189-192: a part other than 0 whose
`DeviceType` is Aggregator 0x000E puts its subtree behind one): the
NetworkCommissioning 0x0031 feature bits WI 0 / TH 1 / ET 2 →
`SupportsWifi/Thread/Ethernet` (NPP:117-128); GeneralDiagnostics 0x0033
`NetworkInterfaces` 0x0000 (decode through its definition) → `WifiActive`
/ `EthernetActive` from entries of type WiFi (1) / Ethernet (2) that are
operational (NPP:132-141); PowerSource 0x002F: `Wired` feature (bit 0)
with `Status` 0x0000 == Active (1) → `MainsPowered` (NPP:148-153); else
`Battery` feature (bit 1), or not wired, or `BatChargeLevel` 0x000E in
the AttributeList, with status Active (1) or Unspecified (0) →
`BatteryPowered` (NPP:154-167); ThreadNetworkDiagnostics 0x0035 (named,
no definition — raw ids: `RoutingRole` 0x0001 enum8 SleepyEndDevice = 2,
`ExtendedPanId` 0x0004 uint64 nullable, `Channel` 0x0000 uint16
nullable; cite `parity/schema.json` by cluster name and the matter.js
element file at the pin — find it with
`git ls-tree -r --name-only 0ceb202d packages/model/src | grep -i ThreadNetworkDiagnostics`
and cite the lines): `ThreadSleepyEndDevice` (NPP:174-176),
`ThreadActive`/`ThreadPan`/`ThreadChannel` (NPP:177-183). When no medium
was found, `inferNetworkMediumFromDiagnostics` (NPP:70-99): WiFi
diagnostics 0x0036 `Bssid` 0x0000 present and non-null → wifi; Thread
diagnostics with any of ExtendedPanId/PanId 0x0003/Channel/NetworkName
0x0002 present → thread, and thread active when it is the only medium
(NPP:90-97).

`IntervalBounds(req floor, ceiling *time.Duration) (floor, ceiling
time.Duration)` (PDP:64-164): ICD floor 0 (PDP:94-102, log at `Info` only
when the caller supplied a floor); default floor 1 s only for a
pre-1.3 Thread node (`SpecificationVersion < 0x0103_0000`, PDP:19)
without a Generic Switch 0x000F endpoint (PDP:103-115), else 0; default
ceiling: ICD → `min(IdleModeDuration, 65535 s)`, battery and not mains →
10 min, Thread sleepy → 3 min, Thread → 1 min, else 1 min (PDP:117-126);
a caller's ceiling below the default is logged at `Debug` (PDP:131-135);
ICD or explicit ceiling → `max(floor, ceiling)` without jitter
(PDP:137-144); else jitter added of up to `max(10 % of ceiling, 10 s)`,
whole seconds, `max(floor, …)` (PDP:146-157); clamp to 65535 s
(PDP:158). Constants verbatim from PDP:13-30. Tests: the table of PDP
branches with a fixed `Rand`.

## §7 `controller/interaction`: the refresh hook

`SubscribeRequest.RefreshRequest func(SubscribeRequest) SubscribeRequest`
(`ClientSubscribe.ts:30-34`): applied in `runSustained` before every
attempt except the first (SS:182-186: `needToRefreshRequest` is false
only for the very first attempt) and again right after a successful
bootstrap read (SS:200-203). `sustainAttempt` takes the request by value
already; thread the refreshed request through. Scripted test: the second
attempt's request differs as the hook says. CHANGELOG → Changed.

## §8 wiring and proofs

- `controller/client/live_test.go` (package `client_test`), after
  `controller/interaction/live_test.go`: a `node.Node` with one bridged
  plug (the `liveNodeAt` pattern), commissioned by `commissioner`; then
  `client.New` over the commissioner's peer and interaction,
  `NewlyCommissioned`, `AutoSubscribe`, `MemoryStore`, hooks collecting
  events; `Start`. Proves: the image has endpoint 0 (root device type
  0x0016, revision from the Descriptor), the aggregator and the plug with
  their device types, names and owners; the plug's OnOff cluster is known
  (`Definition != nil`), `Supports(OnOff.OnOff)`, `Decoded` gives a bool;
  `Seeded`; `Connected`. Toggle the plug on the node side → the
  subscription report updates the cached value and `Changed` fires.
  Stop the node, start it on the same database → the sustained
  subscription re-establishes; its request carried the version filters
  and `eventMin` (assert through a hook on the request or the
  `interaction` log — say which). `Reassemble` without the plug → the
  endpoint is erased, `EndpointInstalled`/`ClusterDeleted` as expected.
  Build a second `client.Node` on the same `Store` after `Close` →
  the tree loads from the cache without a read.
- `make chiptool-test` once at the end: unchanged behaviour of the
  responder is the proof that nothing on the device side moved.
- `script/coverfloor/floors.go`: `controller/client` at 85.

## Report

`notes/briefs/reports/AK.md`: per §, what was built and the guard that
proves it; every line of this brief you found to differ from the file
(with the file's line); the decisions you took where the brief left a
choice (the test seam); what was left
out and why; the open questions for the owner (candidates for
`by_design` go here, you do not write them); the list of named clusters
(D3) the live test met. Then the owner's decisions are appended by the
owner.
