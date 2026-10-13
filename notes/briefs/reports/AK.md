# Report AK — `controller/client`: the client node model (ADR 0020, stage 4, first half)

Branch `wave17/client` from `planning/briefs-wave-17` (b3565cd), pushed,
no PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`);
every `// Mirrors`/citation comment added cites lines read at the pin. Each
commit carries its regenerated reachability inventory (unreached stays
**5**). Touched outside `controller/client`: `controller/interaction` (§7
only), `controller/peer` (§5a only), `cluster/spec/decode_value.go` (+ its
test), `script/coverfloor/floors.go`, `CHANGELOG.md`. `notes/parity/`,
ADRs and the findings register are untouched.

| Commit | Item |
| --- | --- |
| 34a381b `feat(controller/client): §0 the package doc, Config and the node's lifecycle` | §0 |
| 1d70422 `feat(controller/client): §1 the read scope` | §1 |
| 6aeb092 `feat(controller/client): §4 the per-cluster cache and the store` | §4 |
| 2653382 `feat(controller/client, cluster/spec): §2 the cluster shape and spec.DecodeValue` | §2 |
| f718b9c `feat(controller/client): §3 the structure — endpoints, clusters, the Descriptor-driven tree` | §3 |
| 8aef599 `feat(controller/client): §6 physical properties and the interval bounds` | §6 |
| 561f83c `feat(controller/interaction): §7 the refresh hook of a sustained subscription` | §7 |
| aa46365 `feat(controller/client, controller/peer): §5 the node …; §5a per-peer observers` | §5, §5a |
| 77a90b3 `test(controller/client): §8 the live proof and the coverage floor` | §8 |
| faa6aa9 `fix(controller/client): §3 changes-omitted attributes of a named cluster` | §3 follow-up |
| (this commit) `notes: report AK` | — |

Order: §4 before §2 and §3 (the cluster and the structure read and write
through the cache); §6 and §7 before §5 (the node's subscriptions take
their bounds from §6 and their refresh from §7). §0 is a skeleton the
later sections complete (the cache load joins `New` in §3/§4, the peer
hooks and the default subscription join `Start` in §5).

## Per section — what was built, what proves it

- **§0** `doc.go`, `config.go` (`Config`, `Hooks`, `SubscribeOptions`,
  `ConnectionState` with matter.js's values, `ErrDisabled`, `ErrClosed`,
  `ErrConfig`), `node.go` (`New`, `Start`, `Stop`, `Close`, `Disable`,
  `Enable`, `Erase`). `TestLifecycle`, `TestSettingsPersisted`.
- **§1** `scope.go`: RS:38-106. `TestScope` (every branch).
- **§2** `cluster.go` (shape inputs CS:762-808, analysis PB:422-467,
  `Supports`/`SupportsCommand`/`Extra`, `Value`, `Decoded`), `known.go`
  (links all 97 generated definitions), `cluster/spec/decode_value.go`.
  `TestClusterNames`, `TestClusterShape`, `TestClusterDecoded`,
  `TestEveryGeneratedClusterIsKnown`, `TestDecodeValueRoundTrip` (OnOff,
  Descriptor, PowerSource through `spectest`), `TestDecodeValueKinds`.
- **§3** `structure.go`, `endpoint.go`: the literal port of CS (mutate,
  updateCluster, synchronizeCluster/Descriptor, the claims, erase,
  rebuild, install, loadCache, injectVersionFilters). Tests: bridge tree
  and owners, tree composition, several ordinary claimants (warning),
  unknown composition waits, root list erases (and keeps a holder), server
  list deletes unless data arrives (before or after the list), AttributeList
  prune + FeatureMap rebuild, version only from a wildcard scope,
  fabric-filter mismatch, reload installs the same tree with events once,
  `checkUnlistedSeedValues`, event ordering, hook panics, version filters
  (incl. changes-omitted, named clusters), retracted claims.
- **§4** `cache.go`, `store.go`, `settings.go`. `TestCacheWriteThrough`,
  `TestCacheBuffered` (fake clock, failing store retried, stop flushes),
  `TestMemoryStore`, `TestMemoryStoreConcurrent` (`-race`),
  `TestSettingsRoundTrip`.
- **§5** `interact.go` (Read, Subscribe, Write, Invoke), `network.go`
  (default subscription, bootstrap read, connection state, events,
  seeded), lifecycle completed in `node.go`. Scripted tests in
  `network_test.go`: initial read of a newly commissioned node, the
  default subscription's filters + event minimum, the refreshed
  re-subscription, the next node's bootstrap read, `IncludeKnownVersions`,
  the connection-state table, Write/Invoke forwarding.
- **§5a** `controller/peer/observer.go`: `Peer.Observe` /
  `Observer{SessionAdded, SessionDeleted, EstablishmentUnresponsive}`,
  called after the peer's lock, in registration order, after the set-level
  hook. `TestObserve`, `TestObserveEstablishmentUnresponsive`.
- **§6** `properties.go`: NPP:30-194, PDP:64-164 with the constants
  :13-30. `TestIntervalBounds` (17 rows, fixed Rand), `TestProperties`
  (incl. frozen while stopped), `TestPropertiesFromDiagnostics`.
- **§7** `SubscribeRequest.RefreshRequest` in `runSustained`.
  `TestSustainedRefreshRequest`, `TestSustainedRefreshAfterBootstrap`.
- **§8** `live_test.go` (`TestLiveClientNode`, see below); floor
  `controller/client` 85 (measured 93.6 %).

## Guards

All four at `faa6aa9` (HEAD logged before and after each run; unchanged).

- `make ci`: exit 0 — build, vet, `go test -count=1 ./...` (180 `ok`, no
  failure), gofumpt + golangci-lint (0 issues), `go mod tidy -diff`,
  `go mod verify`, cover-check (182 packages at or above their floor;
  `controller/client` 93.6 % against 85, `controller/interaction` 92.5 %,
  `controller/peer` 94.5 %, `cluster/spec` 97.5 %).
- `make race`: exit 0 on the first run (180 `ok`, no race reported).
- `make reachability-check`: exit 0 (unreached stays **5**, none in
  `controller/` or `cluster/spec`).
- `make chiptool-test` (once): exit 0, `ok internal/chiptool 58.1s`, 313
  PASS, 0 FAIL, 1 SKIP (`TestMatterJSController`: no built matter.js at
  `.claude/worktrees/matter.js`, its environment reason) — as AI and AJ.
  `TestChipToolCommissionsReferenceBridge`, `TestChipToolSuite`,
  `TestGoControllerCommissionsReferenceBridge`,
  `TestGoControllerSubscribesReferenceBridge` passed: nothing on the
  device side moved.
- Coverage floor: the cover-check advises raising `controller/client` to
  93; it is set at 85 as the brief says.

## Live test (`TestLiveClientNode`)

Proves: endpoint 0 RootNode (`0x16`), the aggregator (`0x0E`) under it,
the plug (`0x010A`, "OnOffPlugInUnit") under the aggregator; the plug's
OnOff known, supported, `Decoded` a bool; `Seeded`; `Connected`; the
default subscription's first request carried the initial read's
DataVersionFilters and an event minimum; a toggle on the node (via the
`onoff.Server`) reaches the image; after the node's stop/start on its
database the subscription delivers again; a re-subscription carries the
refreshed filters and event minimum; the plug removed from the node
(`Aggregator().Remove`) is erased — no `EndpointInstalled`, no
`ClusterDeleted`; a second `client.Node` on the same store after `Close`
has the same tree without a read and stays `Disconnected`.

How the request is asserted: **a hook on the request** —
`export_test.go`'s `ObserveRequests(n, f)` sees every subscription request
the node hands to its interaction (the first, and each one its
`RefreshRequest` produces).

What the restart actually did: the restarted node **resumed its persisted
subscription itself** (CASE to the controller, reports under the same id)
— the controller sent no new SubscribeRequest (1 request after the
restart). The refreshed request was therefore proven by closing the
underlying subscription in the registry (`Subscriptions.CloseForPeer`),
which makes the sustained subscription re-subscribe.

Named clusters (D3) the live test met: **AccessControl,
GroupKeyManagement, Groupcast, OperationalCredentials** (all on endpoint
0). The snapshot has 135 clusters, 38 of them named, as D3 says.

## Lines of the brief that differ from the file (followed the file)

1. **Root revision (§3, §8).** CS:876-880 takes only the revision when the
   root's type is *not* RootNode; a ClientNode's root type is RootNode
   (`ClientNode.ts:401-404` over `Node.ts:388-392`, `endpoints/root.ts:314-319`),
   so that branch never runs, and the root's RootNode entry (utility
   classification "node") is skipped by CS:886-892. The root keeps
   `0x16`, "RootNode", "node" and an **unknown** revision. The brief's §8
   "revision from the Descriptor" does not hold; the live test asserts the
   unknown revision.
2. `ClientRead.ts:12` is `packages/protocol/src/action/client/ClientRead.ts`,
   not under `node/client/`.
3. The element file is `thread-network-diagnostics.element.ts` (kebab case):
   the brief's `grep -i ThreadNetworkDiagnostics` over the file names finds
   nothing. Cited `:20, :31-38, :288` and `wi-fi-network-diagnostics.element.ts:20, :28`.
4. NPP:154-167: in the not-wired branch the condition `battery || !wired ||
   batChargeLevel` always holds, so BatChargeLevel never decides; ported
   as "not wired and Active/Unspecified is battery".
5. `EndpointNumber.isValid` is `EndpointNumber.ts:26-28`
   (`validatorOf(EndpointNumber)`, the range at :19-24).
6. CS:1512-1519 emits every cluster of an installed endpoint again after
   `emitEndpoint`, which itself emits the clusters and recurses into parts
   (`ClientStructureEvents.ts:88-107`). Per the brief, Go emits
   `EndpointInstalled` once per installed endpoint and no
   `ClusterInstalled` for its clusters.
7. `spec.Lookup` knows only definitions whose package is linked
   (`registry.go`). The brief assumes it knows the 97; `known.go` links
   them all and a test fails when the generator adds one the list lacks.
8. DeviceTypeList: decoded **per entry** with
   `spec.DecodeStruct[descriptor.DeviceTypeStruct]`, skipping an entry
   that does not decode (CS:866-868's `typeof dt?.deviceType` check), not
   through `descriptor.Definition.Attribute(AttrDeviceTypeList).Decode`,
   which rejects an empty list (min 1) and fails the whole list on one bad
   entry.
9. `hasChangesOmittedAttribute` (CS:1674-1692) needs the model's
   changes-omitted attributes; a named cluster has no definition to
   enumerate (AccessControl's AuxiliaryAcl is "C"). With the peer's
   AttributeList the listed ids are judged by `schema.AttributeChangesOmitted`
   (exact); without it, for a named cluster, the ids received stand in for
   the model's (approximation).

## Decisions where the brief left a choice

- **Test seam (§5):** the stage-3 `scripted` harness is copied into
  `controller/client/helpers_test.go` (it is `package interaction_test`);
  `controller/client/export_test.go` exposes `ConnectionStateFor` and
  `ObserveRequests`. Nothing exported in `interaction`.
- **Endpoint and Cluster are read through methods** (`Number()`,
  `DeviceType()`, `DeviceRevision() (uint16, bool)`, `Owner()`,
  `Installed()`, `Clusters()`, `Cluster(id)`, `Parts()`; `ID()`, `Name()`,
  `Definition()`, `Revision()`, `FeatureMap()`, `Features()`,
  `AttributeList()`, `AttributeListReported()`, `AcceptedCommands()`,
  `GeneratedCommands()`, `Version()`, `Ready()`), not fields: the
  subscription mutates the image concurrently with the host's reads.
  `DeviceRevision` has an `ok` for EndpointType.UNKNOWN_DEVICE_REVISION.
- **Config vs. stored settings:** `AutoSubscribe` true sets the stored
  setting, false keeps it; a non-nil `DefaultSubscription` replaces it.
  `Config.NewlyCommissioned` / `NoInitialRead` are not persisted.
- **Hooks added beyond §3's list:** `SubscriptionStatusChanged`,
  `SubscriptionAlive` (NC events §5 names). Structure hooks recover a
  panic and log it at Error (the Go reading of "errors are logged").
- **`Start`** returns an error of the initial read and the node stays
  started (matter.js's startup rejects). **`Stop`** waits (within ctx) for
  the subscription to end, so a hook must not call it. `Erase` resets the
  in-memory settings with the store.
- `Node.IntervalBounds(floor, ceiling)` is the exported face of §6's
  bounds; `Node.SubscriptionActive()` of NC:203-209.
- The unknown-cluster/event notice (D4) is remembered per node, not
  module-wide as CEE:39's set.
- The event cursor is saved with the settings each time it rises.
- The store flushes when the default subscription becomes active
  (NC:48, :315-322).

## Left out, and why

- The brief's "Out of this brief" list (AL: Peers registry, address book,
  BasicInformation reactions, fabric-label reconciliation, commissioning
  through the node, SQLite Store; ICD, groups, TCP, applyWireChanges,
  RemoteWriter, generated behaviour types).
- `ChangeNotificationService.broadcastEvent` (CEE:79-90): no change
  notification service on the controller.
- `lifecycle.isReadable` in the seeded check (P:752): the Go node has no
  such state.
- The fingerprint cache of PB (D2).
- A read that fails part-way applies nothing (the report is merged, D7);
  matter.js has applied the chunks it got.

## Open questions

1. **Root revision:** keep matter.js's literal unknown revision for
   endpoint 0 (as ported), or take it from the DeviceTypeList as the brief
   expected? The latter is a `by_design` entry.
2. Should `controller/client` link every generated definition
   (`known.go`), or should the known set stay the host binary's choice?
3. Hosts learn of an erased endpoint only through `Changed` + `Endpoints()`
   (matter.js has no structure event for it). Add an `EndpointErased`
   hook?
4. Should a named cluster carry `Features` (schema.ClusterFeatureNames
   names them for all 135) — the brief says nil without a definition.
5. `RefreshRequest` keeps the subscription's callbacks and interval
   bounds whatever the hook returns; matter.js's hook can replace anything.
   Fine?

## `by_design` candidates (not written)

Besides the owner's three (`BD-Matter-ClientNode-NoGeneratedTypes`,
`-UnknownEventsDelivered`, `-WriteNotThroughCache`):

- `EndpointInstalled` once per endpoint, no re-emission of its clusters
  or parts (difference 6).
- The node's settings stored with the image under cluster id 0xFFFFFFFF
  on endpoint 0.
- A panic in a structure hook recovered and logged.
- The unknown-event notice per node.
- (If the owner decides so) the root's revision from its DeviceTypeList.
