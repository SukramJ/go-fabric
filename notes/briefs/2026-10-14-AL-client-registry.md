# Brief AL — `controller/client.Registry`: the registry of client nodes — local ids, restore on start, the address book and its expiry, commission and decommission through the node, the BasicInformation reactions, the fabric-label reconciliation, a SQLite store (ADR 0020, stage 4, second half)

Owner: Fable. Implementer: Opus (one agent; nothing else runs on the repo
while you do). Branch `wave18/registry` from `planning/briefs-wave-18`,
pushed, **no PR**. Report in `notes/briefs/reports/AL.md`. Read first:
`notes/briefs/README.md`, [ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md),
concept v2 §B.1, brief AK and report AK (the owner's decisions at its end
are rules; `controller/client` is what you extend), and the packages this
one builds on: `controller/client` (`Node`, `Config`, `Hooks`, `Store`,
`MemoryStore`, the settings under cluster `0xFFFF_FFFF`,
`Node.SubscriptionActive`, `Node.Seeded`), `controller/commissioner`
(`Commissioner.Commission`, `Options`, `Result`, `Fabric`,
`Commissioner.Interaction`), `controller/peer` (`Set.For/Get/AddKnownPeer`,
`Peer.Observe`, `Peer.Descriptor`, `Peer.NewestSession`, `Peer.Disconnect`,
`Peer.Delete`), `controller/exchange` (`Manager.HandlePeerLoss`,
`Manager.SessionsFor`, `Session`), `controller/discovery` (`Device`,
`Address`, `Scanner`), `store/` (the migration pattern: `Migration`,
`Migrations`, `Apply`, `SchemaFingerprint`, `migrations/*.sql`, and its
tests' in-memory database). The live test of brief AK,
`controller/client/live_test.go` (`newLive`: the commissioner over a
`node.Node` in-process), is the pattern for the proofs here.

matter.js at **`0ceb202d`**; the `../matter.js` checkout stands later —
read with `git -C ../matter.js show 0ceb202d:<path> | cat -n`. Paths
below are relative to `packages/`; P = `node/src/node/client/Peers.ts`
(966 lines), CC = `node/src/behavior/system/commissioning/CommissioningClient.ts`
(1339 lines; **not** under `node/client/`), NC =
`node/src/behavior/system/network/NetworkClient.ts`, CNS =
`node/src/storage/client/ClientNodeStores.ts`, CN =
`node/src/node/ClientNode.ts`, CNL = `node/src/node/ClientNodeLifecycle.ts`,
RD = `node/src/behavior/system/commissioning/RemoteDescriptor.ts`, SM =
`protocol/src/session/SessionManager.ts`, PEER = `protocol/src/peer/Peer.ts`.
Read the lines a fact cites before you mirror them; a brief line that
differs from the file is a line to correct in the report. One commit per
§, reachability in each, CHANGELOG under `[Unreleased]` → Added (one entry
for the registry, one for `sqlstore`), Changed (`client.Store` gains
`NodeIDs`; `exchange.Session.CreatedAt`). Guards: `make ci`, `make race`,
`make reachability-check`, and `make chiptool-test` once at the end
(export `PATH=$HOME/go/bin:$PATH` first; if `make ci` fails only on
golangci-lint's own timeout after it printed "0 issues", rerun
`make lint` alone and say so in the report). You may change
`controller/client` (new files; `store.go` for §1; `node.go`/`config.go`
only where a § below says), add `controller/client/sqlstore`, add
`exchange.Session.CreatedAt()` (§5 — one getter, nothing else in
`controller/exchange`), and add to `script/coverfloor/floors.go`;
`controller/commissioner`, `controller/peer`, `controller/interaction`,
`controller/discovery`, `secure/`, `transport/`, `bridge/`, `node/`,
`store/`, `schema/`, `device/` and the generated files are read, not
changed — a change you need there is a question in the report.
`notes/parity/by_design.md` and the findings register are the owner's.

Out of this brief: groups (`ClientGroup`, P:912-917, `group${N}` ids —
`PeerAddress.isGroup` never matches here), ICD (`IcdClient`, P:777-783),
the bridged `ConfigurationVersion` lifecycle event (P:765-775), split
commissioning (`Peers.completeCommissioning`, P:226-346, and
`finalizeCommissioning`; the Go `commissioner.Options.FinalizeCommissioning`
exists and stays as it is — a later brief), the discovery runners
(`locate`/`discover`/`commission`/`pase` as `InstanceDiscovery`,
`ContinuousDiscovery`, `CommissioningDiscovery`, `PaseDiscovery`, P:182-213
— the host drives `discovery.Scanner` itself and hands the registry a
`discovery.Device`), the commissioning windows on a node (CN:188-205),
the `PeerAddressMonitor` (already in `controller/peer`), TCP and the
transport preference (NC:69-75), and the `device/` types as the client
view (after AL).

## What the second half of stage 4 is

matter.js's `Peers` (P:88) is the `ServerNode`'s container of
`ClientNode`s: it restores every stored node at initialization (P:123-152),
starts the commissioned ones when the server goes online (P:154-169) and
stops them when it goes offline (P:171-176), allocates the local ids
(CNS:23, :77-81), finds or creates a node for a discovered device
(P:358-377) or for a peer address (P:445-486), serializes commission and
decommission per node and shields such a node from the cull (P:573-610),
culls expired commissionable nodes and expired addresses once a minute
(P:73-74, :615-712), and reacts to the peer's BasicInformation events —
`leave` deletes the node, `shutDown` and `startUp` drop its stale sessions
(P:727-729, :785-893). `CommissioningClient` (CC) is the per-node
behaviour that holds the record (CC:931-1090: `peerAddress`, `addresses`,
`discoveredAt`, `commissionedAt`, `fabricIndexOnPeer`, `ttl`, …), binds
the protocol `Peer` (CC:685-763), runs `commission` (CC:170-330) and
`decommission` (CC:340-446). `NetworkClient` reconciles our fabric label
onto the peer once per start (NC:259-307).

In Go the per-node half of all this is `client.Node` (brief AK); the
registry is new: `client.Registry` in the same package (owner's D1), the
node's commissioning record beside its settings in the same `Store`
(D2), the address book as part of that record with matter.js's expiry
(§3), and `controller/client/sqlstore` as the first durable `Store`
(§7). matter.js's discovery runners and groups stay out; the host runs
`discovery.Scanner` and hands the registry a `discovery.Device`.

## Owner's decisions

- **D1 — the registry lives in `controller/client`** as `Registry`
  (files `registry.go`, `record.go`, `addresses.go`, `fabricops.go`,
  `reactions.go`, `label.go`). It needs the node's unexported state (the
  settings, the store writer, the hooks' wrapping); a second package
  would export all of it. `RegistryConfig` is the dependency bundle.
- **D2 — one store, one record.** The node's commissioning record
  (CC's nonvolatile state) is persisted like the settings: endpoint 0,
  reserved cluster id `0xFFFF_FFFE` (`recordCluster`), one attribute id
  per field, TLV-encoded. `client.Store` gains `NodeIDs() ([]string, error)`
  (matter.js `clientStores.knownIds`, CNS:117-121 — the storage
  contexts). `MemoryStore` implements it. The `peer.Store` of
  `controller/peer` (the protocol layer's descriptor store) stays as it
  is; the record duplicates the operational address and the CATs as CC
  duplicates `Peer.descriptor` (CC:721-741).
- **D3 — ids are `peerN`**, N from 1, the counter kept past every stored
  `peerN` (CNS:44-54, :77-81). An id is not an identity: the doc of
  `Registry.Get` says what CNS:71-75 and P:402-411 say. A caller may pass
  its own id (P:921).
- **D4 — no groups, no ICD, no discovery runners** (see "Out").
- **D5 — the fabric is the commissioner's.** `RegistryConfig.Fabrics
  []*commissioner.Fabric` is matter.js's `FabricManager` for the
  registry: `forAddress`'s "a fabric we do not belong to" (P:446-448) is
  "no fabric with the address's `FabricIndex`"; `FabricManager.forDescriptor`
  (P:807) is "the fabric whose `RootPublicKey` and `FabricID` equal the
  peer's `FabricDescriptorStruct`"; `maybeFor(fabricIndex)?.label` (NC:285)
  is `Fabric.Label`.
- **D6 — the reactions hang on `Hooks.Event`.** The registry wraps each
  node's `Hooks` (the host's hooks are called after the registry's
  work for `Event`; the other hooks pass through). No `clusterInstalled`
  observable exists in Go; matter.js's "once per node" guard
  (P:719-725) is moot.
- **D7 — the SQLite store is its own package**, `controller/client/sqlstore`,
  with its own migration list in `store/`'s style (`Migration`,
  `Migrations()`, `Apply`, `SchemaFingerprint`, `migrations/*.sql`,
  tables `matter_client_*`), so a device-side host's pin on
  `len(store.Migrations())` does not move for a controller table. It
  imports `store` for the `Migration` type only if that type is
  reusable without the device tables; otherwise it declares its own.
  `modernc.org/sqlite` is already a dependency; no new one.
- **D8 — a mutex, no promise machinery.** `Registry.mu` serializes what
  P's `#mutex` serializes (fabric-operation registration, `ForAddress`
  creation, the cull, `leave`, `Erase`); the fabric-operation body runs
  outside it (P:604-609) with the entry in `fabricOps` (P:93).

## §0 — `RegistryConfig`, `Registry`, lifecycle (`registry.go`)

```go
type RegistryConfig struct {
    Store         Store                    // nil: a MemoryStore
    Commissioner  *commissioner.Commissioner
    Peers         *peer.Set
    Exchanges     *exchange.Manager
    Fabrics       []*commissioner.Fabric   // D5; FabricByIndex/FabricForDescriptor are helpers over it
    Subscriptions *interaction.Subscriptions
    AutoStart     bool                     // NetworkServer.autoStartCommissionedPeers (P:155)
    Node          Config                   // the template: Hooks, Logger, Clock, Rand, FlushInterval; ID/Peer/Interaction/Store are the registry's
    Logger        *slog.Logger
    Clock         exchange.Clock
}
```

- `NewRegistry(cfg) (*Registry, error)`: validates (Commissioner, Peers,
  Exchanges, at least one fabric); loads `Store.NodeIDs()`; for each id
  builds the node — `client.New` with the record loaded first (§2) so the
  `Peer` is bound (`peers.AddKnownPeer` from the record, CC:139-142,
  :685-714 — the operational address, the discovery data, the CATs, the
  reported session parameters) and `Config.Peer`/`Interaction`
  (`Commissioner.Interaction(peer)`) set — and adds it; a node that fails
  to load is logged at Warn `matter.controller.client.registry.load_failed`
  with its id and skipped (P:141-147). The counter moves past every
  `peerN` (CNS:44-54). Synchronous, nothing started (P:123-152).
- `Start(ctx)`: when `AutoStart`, every node that is commissioned
  (record has a peer address) and not disabled is started **sequentially**,
  an error logged at Warn `matter.controller.client.registry.start_failed`
  and the loop continuing (P:154-167, `MatterError.accept` — a Go error
  is accepted); then the expiry timer is managed (§3). `Stop(ctx)`:
  cancel the timer, stop every node sequentially (P:171-176). `Close(ctx)`:
  closed latch, timer cancelled, mutex drained, nodes closed (P:516-527).
- `Nodes() []*Node` (join order), `Commissioned() []*Node` (P:397-400),
  `Get(id string) *Node`, `GetByAddress(addr handshake.PeerAddress) *Node`
  (the linear scan of P:412-427; `exchange.PeerAddress` and
  `handshake.PeerAddress` — use the one `Peer.Address()` returns), `Size()`.
- `Add` is internal: the node's owner is the registry; the expiry timer
  is managed on add and delete (P:536-541, :107).
- `Erase(ctx)`: under the mutex, every node deleted; one that fails to
  delete is closed and dropped, failures logged at Warn (P:495-514).
- `ErrFabricOperationInProgress` (P:81, the message of P:597-601),
  `ErrNotCommissioned` (CC:343-345), `ErrAlreadyCommissioned`
  (CC:173-175), `ErrNotLocated` (CC:220-223), `ErrForeignFabric`
  (P:446-448).

Tests: restore from a `MemoryStore` seeded with two nodes (`peer1`,
`peer3` → next id `peer4`); a node that fails to load is skipped;
`AutoStart` starts commissioned-and-enabled nodes only; `Stop` stops
them; `Erase` empties the store.

## §1 — `Store.NodeIDs` (`store.go`, `sqlstore` later)

`NodeIDs() ([]string, error)` on `Store`, sorted; `MemoryStore` keeps the
ids it has saved for (an `EraseNode` removes one). CHANGELOG Changed: an
unreleased interface, no deprecation window.

## §2 — the record (`record.go`)

The persisted commissioning record of a node, CC:931-1090's nonvolatile
fields, as one struct with TLV codecs under `recordCluster = 0xFFFF_FFFE`:

| id | field | CC line |
| --- | --- | --- |
| 0 | `PeerAddress *handshake.PeerAddress` (nil: uncommissioned) | :931-934 |
| 1 | `CaseAuthenticatedTags []uint32` | :936-940 |
| 2 | `Addresses []NetworkAddress` (nil: never located) | :942-947 |
| 3 | `Hostname string` | :949-959 |
| 4 | `DiscoveredAt time.Time` (ms) | :961-965 |
| 5 | `CommissionedAt time.Time` (ms; zero: not set) | :979-983 |
| 6 | `FabricIndexOnPeer uint8` (0: unknown) | :985-993 |
| 7 | `TTL time.Duration` (seconds on the wire; 0: unknown) | :995-999 |
| 8 | `DeviceIdentifier string` (the commissionable instance id) | the descriptor fields of CC:1001-1090 |
| 9 | `Discriminator *uint16` | " |
| 10 | `CommissioningMode uint8` | " |
| 11 | `VendorID`, 12 `ProductID` (`*uint16`) | " |
| 13 | `DeviceType *uint32` | " |
| 14 | `DeviceName string` | " |
| 15 | `ReportedSessionParameters *sessionparams.Parameters` (its TLV struct) | :1050-1060 |
| 16 | `TCPUnsupported bool` | :1062-1070 |

Read CC:1001-1090 and keep the fields the Go `discovery.TXTData` /
`discovery.Device` can fill (pairing hint, pairing instructions,
rotating id, ICD/LIT fields too if `TXTData` has them); list in the
report what you left out and why. `onlineAt`/`offlineAt` are not
persisted (CC:967-977, no `nonvolatile`) — not ported.

```go
type NetworkAddress struct {
    IP           netip.Addr
    Port         uint16
    TTL          time.Duration // 0: unknown → DefaultTTL at expiry (P:959-966)
    DiscoveredAt time.Time     // zero: never expires (P:960)
}
```

The record is loaded in `New` before the structure (so `NewRegistry` can
bind the peer); `Node.Record() Record` returns a copy; writes go through
`saveRecordLocked` like the settings. `Node.PeerAddress() (handshake.PeerAddress, bool)`
and `Node.IsCommissioned() bool` (`peerAddress !== undefined`, CNL's
`isCommissioned`).

Address learning (CC:721-741, `#update`): on every `Peer.Observe`
`SessionAdded` and on commission, the record takes the peer's
`Descriptor()`: `ReportedSessionParameters` when non-nil,
`OperationalAddress` as **the one address** (`addresses = [operationalAddress]`,
:736-739, the TODO included — mirror it), `CaseAuthenticatedTags`, and
the discovery data's fields that `RemoteDescriptor.toLongForm`
(RD:167-245) copies: `discoveredAt`/`ttl` only when defined (:191-197),
addresses only when non-empty (:199-201). `DiscoveredAt` is set at the
node's creation when zero (CC:125-127). The address-change log lines of
CC:600-635 at Info (`set to`, `changed … from … to`, `cleared`).

Tests: round trip of every field; a node restored from the record binds
its peer with the record's operational address and CATs; a session added
replaces the addresses with the one operational address.

## §3 — the address book and the cull (`addresses.go`)

Constants: `DefaultTTL = 15 * time.Minute` (P:73), `ExpirationInterval =
time.Minute` (P:74). `expirationOf(discoveredAt, ttl)` (P:959-966): zero
`discoveredAt` → no expiry; `ttl` 0 → `DefaultTTL` (`Seconds(undefined)
?? DEFAULT_TTL`); the record's own `DiscoveredAt`/`TTL` are the node-level
lifespan (P:685).

The timer (P:615-649): a one-shot `AfterFunc(ExpirationInterval)` armed
when the registry has nodes and no timer, re-armed after each cull;
cancelled when the registry empties or closes; the cull runs under the
mutex, an error logged at Warn `matter.controller.client.registry.cull_failed`.

The cull (P:651-712), per node: skip while not ready (Go: closing) or
with a fabric operation in flight; `isCommissioned = record.PeerAddress != nil`;
skip when `Addresses == nil` or (commissioned and exactly one address);
drop expired addresses (an address without `DiscoveredAt` is kept);
**uncommissioned**: delete the node when no address is left or the
node-level expiry passed (`?? 0`: a zero `DiscoveredAt` deletes);
**commissioned**: when every address expired, keep the freshest-expiry
ones (P:692-702); write the list back when it shrank (P:704-707).

`Registry.ForDevice(ctx, d discovery.Device) (*Node, error)` is
`forDescriptor` (P:358-377) with `Factory.find` (P:942-952) and
`RemoteDescriptor.is` (RD:37-50): match by peer address when both have
one, else by non-empty `DeviceIdentifier` (= `d.InstanceID`); a node
under decommission is waited for and the search repeated; a found node's
record is refreshed from the device (addresses only when non-empty,
`DiscoveredAt` now, `TTL` 0 — the Go scanner carries no TTL: say so in a
comment, it is `DefaultTTL` at expiry); otherwise a new node (id
allocated, record from the device, `DiscoveredAt` now).

Tests with a fake clock: an uncommissioned node whose only address
expired is deleted after the tick; one with a fresh address survives a
tick and dies at the node-level expiry; a commissioned node with two
addresses keeps the freshest when both expired and is never deleted;
one with one address is untouched; the timer is not armed on an empty
registry; a node under commission is skipped.

## §4 — commission and decommission (`fabricops.go`)

`runFabricOperation(node, kind, fn)` (P:592-610): under the mutex,
`ErrClosed` when the node is closing, `ErrFabricOperationInProgress`
when an entry exists, else register; `fn` outside the mutex; the entry
removed and its `done` channel closed in a defer. `ForDevice` and the
cull consult it; `leave` ignores a node under decommission.

`Registry.Commission(ctx, node, opts CommissionOptions) error` —
`ClientNode.commission` (CN:162-166) over CC:170-330:

```go
type CommissionOptions struct {
    commissioner.Options                 // Fabric (nil: Fabrics[0]), Passcode, NodeID, Addresses (nil: the record's), DiscoveryData (nil: from the record), …
    AutoSubscribe       *bool            // nil: true (CC:311 "unless disabled explicitly")
    DefaultSubscription *SubscribeOptions
    NoInitialRead       bool             // autoStateInitialize false
}
```

Order, inside `runFabricOperation(…, "commission", …)`: `ErrAlreadyCommissioned`
when the record has a peer address (CC:173-175); the fabric (CC:198-218:
the option, else `Fabrics[0]`; a fabric not in `Fabrics` is an error);
`ErrNotLocated` when neither the options nor the record have an address
(CC:220-223); `commissioner.Commission` with the record's addresses as
`[]*net.UDPAddr` and its discovery data (CC:252-269); on success the
record takes `Result.Address`, `CommissionedAt = now`, `FabricIndexOnPeer`
(CC:291-293) and `#update` from `Peers.Get(address)` (CC:296, §2); the
node's `Config.Peer`/`Interaction` are set now (the node was created
without them); the settings take `DefaultSubscription`, `autoSubscribe =
AutoSubscribe == nil || *AutoSubscribe` (CC:308-314), the node is marked
newly commissioned (`NoInitialRead` respected); record and settings
saved (CC:316 — the commit *before* `start`); Info
`matter.controller.client.registry.commissioned` with id and address
(CC:318-323); `node.Start(ctx)` — its error (the initial read's) returns
to the caller and the node **stays commissioned** (CC:327; report AK:
`Start` returns the read's error, the node stays started). On a
`commissioner.Commission` error the node stays as it was (uncommissioned,
its addresses kept; `commissioner` deletes the protocol peer itself) —
the error returns.

`Registry.Decommission(ctx, node) error` — `ClientNode.decommission`
(CN:176-186) over CC:340-446, inside `runFabricOperation(…, "decommission", …)`:
`ErrNotCommissioned` without a peer address; when the image has no
OperationalCredentials cluster on endpoint 0, or `FabricIndexOnPeer` is 0
and the image's `CurrentFabricIndex` (attr 5) is absent or 0, read
`OperationalCredentials.CurrentFabricIndex` through `node.Read` with the
default subscription's fabric filter (CC:353-371); `fabricIndex =
FabricIndexOnPeer`, else `CurrentFabricIndex`; arm the leave latch
(§6: a `leave` of that fabric index while the subscription is active sets
`leaveSeen`, CC:377-391) *before* sending; Debug
`matter.controller.client.registry.removing_fabric`; `node.Invoke`
`OperationalCredentials.RemoveFabric` (cluster `0x3E`, command `0x0A`,
field 0 `FabricIndex`; response `NOCResponse` command `0x08`: field 0
`StatusCode`, 1 `FabricIndex`, 2 `DebugText`; `0` is Ok) — the generated
definition does not exist (named cluster): encode and decode by hand in
`record.go`'s neighbour `opcreds.go` with the ids cited from the
snapshot (`parity/schema.json`, OperationalCredentials); a non-Ok status
is an error that no leave event overrides (CC:396-404, :418-420); an
invoke error is "confirmed" when `leaveSeen` or the error is a peer-lost
class (`errors.Is` against `exchange`'s / `peer`'s peer-lost,
peer-left, fabric-removed, peer-initiated-close errors — find the Go
sentinels in `controller/exchange` and `controller/peer` and cite them;
list in the report which of matter.js's four each maps to), logged at
Info `removal confirmed without response` with `leave event` or
`session closed` (CC:405-417), else returned; then `Peer.Disconnect(ctx,
ErrPeerLeft)` (CC:422-427; a Go sentinel `ErrPeerLeft` in `client` if
`peer` has none), the record's `PeerAddress`, `CommissionedAt`,
`FabricIndexOnPeer`, `ReportedSessionParameters`, `TCPUnsupported`
cleared (not the addresses, CC:429-433), saved, Info
`matter.controller.client.registry.decommissioned` with the former
address (CC:437-442); the leave latch disarmed in a defer. Then, as
CN:184, `deleteNode` (§0's delete: `node.Erase`, `Peer.Delete`, removed
from the registry — CN:213-225's `finally` deletes the protocol peer even
when the teardown failed).

Tests (scripted peer, as `network_test.go`): commission sets record and
settings and starts the node; a second commission is
`ErrAlreadyCommissioned`; a concurrent operation is
`ErrFabricOperationInProgress`; decommission reads `CurrentFabricIndex`
when unknown, sends RemoveFabric with the peer-side index, treats
non-Ok as an error, treats a lost session with `leaveSeen` as confirmed,
clears the record and deletes the node.

## §5 — `exchange.Session.CreatedAt()` 

One exported getter on `Session` returning `createdAt` (used by §6's
`startUp`, SM:690-692's cutoff). Doc: "CreatedAt is when the session was
established — matter.js Session.createdAt, the cutoff
SessionManager.#handlePeerLoss compares (SessionManager.ts:690-692 at
0ceb202d)". CHANGELOG Changed.

## §6 — the BasicInformation reactions (`reactions.go`)

Through the wrapped `Hooks.Event` (D6), endpoint 0, cluster `0x28`
(`basicinformation.ClusterID`), events `EventStartUp` 0, `EventShutDown`
1, `EventLeave` 2 (`basicinformation.LeaveEvent.FabricIndex` decodes the
payload — the generated definition exists). All three ignore an event
delivered while `node.SubscriptionActive()` is false, logging as
matter.js does: `leave` at Info
`matter.controller.client.registry.leave_ignored`, the other two at Debug
(P:812-824, :842-851, :872-881).

- **`leave`** (P:785-835): outside the mutex nothing; under the mutex:
  skip a closing node or one under decommission (:791-793); the peer's
  own fabric entry — the image's `OperationalCredentials.Fabrics` (attr
  1, a list of `FabricDescriptorStruct`: 1 `RootPublicKey` octstr, 2
  `VendorID`, 3 `FabricID`, 4 `NodeID`, 5 `Label`, 0xFE `FabricIndex` —
  decode by hand in `opcreds.go`, ids from the snapshot) whose
  `FabricIndex` equals the event's; none → return (:795-799); no peer
  address → return (:801-804); our fabric for that entry (D5:
  `RootPublicKey` and `FabricID` equal) must exist and its `FabricIndex`
  equal the peer address's (:806-810), else return; the subscription
  guard; Notice (Go: Info) `matter.controller.client.registry.peer_left`
  (:826); `Hooks.Decommissioned` (new hook, `lifecycle.decommissioned`,
  :827 — add it to `Hooks` in `config.go`); `Peer.Disconnect(ctx,
  ErrPeerLeft)`, an error logged at Warn (:828-832); `deleteNode`
  (:833). The decommission latch of §4 sees the same event (CC:383-391).
- **`shutDown`** (P:837-858): skip when closing or not started; the
  guard; with a peer address `Exchanges.HandlePeerLoss(addr,
  ErrPeerShutdown, time.Time{})` — every session with the peer (SM:624-631
  with `asOf` now: `handlePeerShutdown(address)`; the Go `HandlePeerLoss`
  with a zero `asOf` is "all of them"). matter.js passes
  `keepSubscriptions: true` (SM:629): the client subscriptions survive
  the session loss and the sustained one reconnects — **verify** what
  `exchange.Session.handlePeerLoss` and `interaction.Subscriptions` do
  with a lost session in Go (does the sustained default subscription
  re-subscribe after `HandlePeerLoss`? the AK live test's
  `CloseForPeer` path suggests the registry closes them); if a Go
  session loss would close the subscription for good, say so in the
  report with the lines — it is a question, not a fix.
- **`startUp`** (P:860-893): skip when closing or not started; the
  guard; no peer address → return; `asOf = Peer.NewestSession().CreatedAt()`
  (SM's `maybeSessionFor`: the newest session), zero when there is none
  (then `HandlePeerLoss` with `asOf = now`: pass `Clock.Now()`); sessions
  created at or after `asOf` are kept (SM:690-692).

Tests (scripted peer + a fake exchange manager or the real one with two
sessions): each reaction ignored without an active subscription; `leave`
with a foreign fabric index ignored, with ours deletes the node and
calls `Decommissioned`; `shutDown` loses every session; `startUp` keeps
the newest.

## §7 — the fabric-label reconciliation (`label.go`)

NC:259-307, once per `Start` (the latch set before anything else,
NC:269-273, reset by `Stop`): on the first `SubscriptionAlive` after
`Start` — wrap the hook as D6 wraps `Event` — return without a peer
address or without an OperationalCredentials cluster in the image
(:275-278); `label` = the `Fabrics` entry with the peer address's
`FabricIndex`, its `Label`; empty → return (:285-288); `ownFabric` = the
image's `Fabrics` entry whose `FabricIndex` equals the image's
`CurrentFabricIndex` (**the device-side index**, :290-294 — never our
`peerAddress.FabricIndex`); none or same label → return (:295-297);
`node.Invoke` `UpdateFabricLabel` (command `0x09`, field 0 `Label`;
`NOCResponse` as §4); non-Ok status → Info (matter.js notice)
`matter.controller.client.registry.label_not_reconciled` with the status
(:301-303); an error → Info `…label_reconcile_failed` (:304-306). Never
retried in the same start.

Tests: label equal → no invoke; label differs → one UpdateFabricLabel
with our label; non-Ok logged, no error; a second `SubscriptionAlive`
does nothing; `Stop`+`Start` reconciles again.

## §8 — `controller/client/sqlstore`

`sqlstore.New(db *sql.DB) *Store` implementing `client.Store` (`Load`,
`Save`, `EraseCluster`, `EraseEndpoint`, `EraseNode`, `NodeIDs`) over
`modernc.org/sqlite`; `Apply(ctx, db)`, `Upgrade`, `Migrations()`,
`SchemaFingerprint` in `store/`'s style (read `store/schema.go`,
`store/migrations.go`, `store/migrations/v1.up.sql` and the tests that
hold `Migrations` against `Apply`; mirror that test). Tables:
`matter_client_values(node_id TEXT, endpoint INTEGER, cluster INTEGER,
attribute INTEGER, value BLOB, PRIMARY KEY(node_id, endpoint, cluster,
attribute))` and `matter_client_versions(node_id, endpoint, cluster,
version INTEGER, PRIMARY KEY(node_id, endpoint, cluster))`; `Save` is
one transaction (delete the cluster's rows it replaces? — no:
`Save(values, version)` upserts the given attributes and, when `version`
is non-nil, the version; an attribute whose value is nil is deleted,
as `EndpointStore.ts:104-105` deletes an `undefined` key — check
`client.MemoryStore.Save`'s contract and keep to it). `NodeIDs` is
`SELECT DISTINCT node_id` of both tables, sorted. Safe for concurrent
use (`database/sql` is; one writer at a time is SQLite's). Tests like
`store/`'s: an in-memory database per test (`file::memory:?cache=shared`
or a temp file — look at `store/testhelper_test.go`), round trip, erase
levels, `NodeIDs`, concurrent saves under `-race`, `Migrations` ==
`Apply`. Floor for `sqlstore` 85.

## §9 — the live proof (`live_test.go` of `controller/client`, extended)

`TestLiveRegistry`, on the AK harness with a SQLite `sqlstore` on a temp
file for the controller side:

1. `NewRegistry` (empty store) → `ForDevice` with the node's address as a
   `discovery.Device{InstanceID: "live", Addresses: …}` → `peer1`,
   uncommissioned, `Addresses` one entry, `DiscoveredAt` set →
   `Commission` → the record has the address, `CommissionedAt`,
   `FabricIndexOnPeer`; the node is started, `Seeded`, `Connected`;
   `Commissioned()` is `[peer1]`; the store's `NodeIDs()` is `["peer1"]`.
2. Fabric label: the `commissioner.Fabric.Label` is "live"; after the
   first `SubscriptionAlive` the device's own fabric entry (read
   `OperationalCredentials.Fabrics` through `node.Read`) carries "live"
   (the commissioner's step 98.1 already set it) — then set
   `Fabric.Label = "renamed"`, `Stop`+`Start` the node → after the next
   `SubscriptionAlive` the entry reads "renamed".
3. `Close` the registry; `NewRegistry` on the same database → `peer1`
   restored (record intact, peer bound with the record's operational
   address, nothing read); `Start` with `AutoStart` → the subscription
   resumes and delivers a toggle.
4. `startUp`: restart the `node.Node` on its database → the StartUp event
   arrives over the restored subscription; assert the session that
   existed before the restart is lost (`Exchanges.SessionsFor` no longer
   lists it) and the subscription delivers again.
5. `Decommission` → RemoveFabric reaches the device (its
   `CommissionedFabrics` drops / the fabric is gone from its store);
   `Commissioned()` empty; `NodeIDs()` empty; the protocol peer gone
   (`Peers.Get` nil).
6. The cull: a second `ForDevice` with `TTL = 1s` on a fake clock
   (`RegistryConfig.Clock`) — advance past the tick → the node is gone.

Say in the report which of 2 and 4 the harness could not prove and why.
`make chiptool-test` once at the end: nothing on the device side moved.

## Report

As report AK: per § what was built and what proves it; the guards with
HEAD before and after; the brief lines that differ from the file; the
decisions where the brief left a choice; what was left out and why; the
open questions (the §6 `keepSubscriptions` one first); `by_design`
candidates (not written). Coverage floor `controller/client` stays 85;
`sqlstore` 85.
