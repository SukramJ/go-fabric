# Brief AO — the binding manager, the node's client stack, the 11 controller device types, the BIND proof (ADR 0020 stage 5, part 2)

Owner: Fable. Implementer: Opus. A second Opus agent works on brief AN
(the group sender: `groups/`, `secure/channel`, `transport/`,
`cluster/core/groupcast.go`, `node.Node.Groups()`) at the same time: do
not touch those; `CHANGELOG.md` is the one shared file (append your
entries). **Never run `make chiptool-test` or any chip-tool binary**
(the other agent runs it; the owner runs it on the tranche). Branch
`wave21/binding` from `planning/briefs-wave-21`, pushed, **no PR**.
Report `notes/briefs/reports/AO.md`. Read first: `notes/briefs/README.md`,
[ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md)
(stage 5 row, decision 6), concept v2 §B.1, report AL (the owner's
decisions are rules), `cluster/core/binding.go`, `contract/` (the
topology the host hands in), `endpoint/` (`assembler.go`,
`devicetype_validation.go`, the `unbindable` list in
`schema/devicetype_requirements_gen.go:2616`), `script/devicegen`,
`device/` (one generated file), `node/` (`security.go`'s
`caseIdentities`, `node.go`, `config.go`), `controller/peer` (`Set`,
`Context`, `Peer.Connect`, `Peer.Observe`), `controller/exchange`
(`NewManager`, `Config`), `controller/handshake` (`Fabric`),
`controller/interaction` (`New`, `Invoke`), `controller/client/live_test.go`
(`newLive`: the harness pattern), `controller/client.Registry`
(commissioning several nodes into one fabric).

matter.js at **`0ceb202d`** (`git -C ../matter.js show 0ceb202d:<path> | cat -n`),
paths under `packages/node/src/`: BS = `behaviors/binding/BindingServer.ts`,
BM = `behaviors/binding/BindingManager.ts` (737 lines — read it whole),
ME = `endpoint/type/MutableEndpoint.ts` (`withClientClusters`, :39,
:123, :166), the device files `devices/on-off-light-switch.ts` and
siblings, `node/ClientGroup.ts`, `node/client/ClientGroupInteraction.ts`;
model: `packages/model/src/logic/device-types/ResolvedEndpoint.ts:120-139`
and `ConditionAssertions.ts:600-602` (the `Client` condition = a
bindable application client is declared). Guards: `make ci`, `make race`,
`make reachability-check`, `make chipdm-check` (`device/` changes), no
chip-tool (export `PATH=$HOME/go/bin:$PATH`; a `make ci` failing only on
golangci-lint's own timeout after "0 issues" is rerun as `make lint`).
One commit per §, reachability in each. Allowed paths: `binding/` (new),
`cluster/core/binding.go` (+ tests), `contract/` (§1, stdlib only),
`endpoint/` (§1), `script/devicegen` and `device/` (§2), `node/` (§4),
`script/coverfloor/floors.go`, `CHANGELOG.md`. `groups/`, `secure/`,
`transport/`, `bridge/`, `controller/`, `store/` schema, `cluster/spec`
and the other generated files: read, not changed — a change you need
is a question in the report. `notes/parity/` and ADRs are the owner's.

Out: group sending itself (AN: this brief defines the `GroupSender`
interface the binding manager calls and proves the group kind up to
the point of sending with a fake), ICD, TCP, the controller-side
`ClientGroup` registry node, scenes, the `refcounts` of BM (bookkeeping
without a consumer, :656-677 — port as nothing).

## What matter.js does (Sonnet's read, the load-bearing lines verified by the owner)

- **BindingServer** (BS:120-158): registers every entry of `state.binding`
  with the manager at start; after each interaction diffs the list by
  `entryKey` (BM:714-718: `fabricIndex/node/group/endpoint/cluster`,
  missing fields empty) — added entries registered, removed ones
  unregistered; a changed entry is remove+add. Events `established`
  and `removed` (BS:166-213); `removed` fires for every live binding on
  dispose. An endpoint for which Base mandates Binding (`Simple & Client`)
  gets the server without declaring it (BS:54-60).
- **BindingManager** (BM:255-353, `#resolveAndEmit`), each failure a
  Warn and the entry dropped (the write already succeeded): exactly one
  of node/group (:258-263); the source endpoint's *declared* client
  clusters filtered by the entry's cluster, split bindable/unbindable;
  none bindable → warn with the reason of :724-737 (three strings) and
  drop (:265-272, :563-577); group entry: fabric known (:276-279); node
  entry to our own node id = "self-binding": the endpoint must exist
  and, with a cluster, hold that server (:284-297); a remote node entry
  needs an endpoint (:298-301); **dormant until something observes
  `established`** (:391-438, info once per entry, wakes on the first
  observer); group: waits until the fabric holds a key for the group
  (:482-490, :309-316, info; membership of the source endpoint not
  needed, :477-480), rechecked on key-map and key-set changes with one
  deferred recheck (:492-541); `#resolveGroup` (:453-474): the group
  peer address `{fabricIndex, NodeId.fromGroupId(group)}`, the group
  "endpoint" is the *source* endpoint's number; server kind: local, no
  wire (:322-328); client kind: `peers.forAddress(addr)` (warn on
  failure, :330-337), the target endpoint, the declared clients
  installed on it, then `#establishClientKind` (:585-627): start the
  node, `PeerSet.for(addr).connect()` in the background (info
  "Initiating CASE session for bound peer"), `established` emitted once
  the node is online (edge-triggered, immediately when already online).
  `#emitEstablished` (:359-383): handler errors only warn. `unregister`
  (:147-157): `removed` emitted for an established one. An established
  group entry is not torn down when its key vanishes (:522-525). Nothing
  closes a CASE session when a binding goes (the refcounts have no
  consumer).
- **Device types** (`devices/on-off-light-switch.ts:75-95`): the generated
  type mounts only the Identify and Binding *servers*; the client
  clusters are `requirements.client.{mandatory,optional}` and are
  declared on an endpoint with `withClientClusters` (ME) — the
  declaration is what the manager's `#selectClientClusters` reads.
- **Group peer** (`ClientGroupInteraction.ts:33-82`): read/subscribe refused,
  invoke only without an endpoint, timed refused, `suppressResponse`
  forced — AN's `Sender` has that shape.

## Owner's decisions

- **D1 — a host declares client clusters on an endpoint.** `contract`'s
  endpoint description gains `ClientClusters []uint32` (stdlib only;
  read `contract/` for the type the `Snapshotter` returns), the
  assembler carries it into the assembled endpoint (`Endpoint.ClientClusters`),
  the Descriptor's `ClientList` reports it (check what the Descriptor
  server does with client lists today and cite matter.js's
  `DescriptorServer` for `clientList`), and device-type validation
  evaluates the `Client` condition from it as ResolvedEndpoint.ts:120-139
  does (a *bindable* application client — the generated `unbindable`
  list), so Base's `Simple & Client` mandates the Binding server (the
  assembler mounts `core.NewBinding()` on such an endpoint when the host
  did not, BS:54-60).
- **D2 — `script/devicegen` emits client requirements.** The generated
  `device/` type carries `ClientClusters{Mandatory, Optional}` beside
  the server requirements; the 11 controller types leave `excluded`
  and are generated (ids/revisions in Sonnet's table: OnOffLightSwitch
  0x0103 r4, DimmerSwitch 0x0104 r4, ColorDimmerSwitch 0x0105 r4,
  ControlBridge 0x0840 r4, OnOffSensor 0x0850 r4, DoorLockController
  0x000B r4, WindowCoveringController 0x0203 r5, ThermostatController
  0x030A r2, PumpController 0x0304 r5, ClosureController 0x023E r2,
  VideoRemoteControl 0x002A r2 — verify each against the snapshot in
  the generator's test). A client cluster needs no `cluster/spec`
  definition (ids only); say in the report which of the eleven's
  client clusters have none.
- **D3 — the binding manager is `binding.Manager`** (new package
  `binding/`), device-side, fed by `cluster/core.Binding`'s change hook
  (§3) and resolving through interfaces it declares: `Peers` (the
  `For(addr) (*peer.Peer, error)` + connect of `controller/peer.Set`),
  `GroupSender` (`Sender(ctx, fabricIndex, groupID) (GroupTarget, error)`
  — AN's `groups.Manager.Sender` will satisfy it; until then a fake in
  tests), `Topology` (our node id per fabric, endpoints and their
  servers for the self-binding checks, each endpoint's declared client
  clusters). Resolutions: `Resolution{Kind Client|Group|Server, Entry,
  SourceEndpoint uint16, Peer *peer.Peer (client), Interaction
  *interaction.Interaction (client), Endpoint uint16 (target),
  Group GroupTarget}`. Callbacks: `Manager.OnEstablished(func(Resolution))
  (stop func())`, `OnRemoved(...)`; **dormant until the first
  `OnEstablished` handler**, as BM:391-438. Every Warn/Info string of
  BM ported verbatim as the log message's `reason` attribute.
- **D4 — the node's client stack.** `node.Config.Client{Enabled bool,
  Listen string}` (default off; on: a second `udp.Listener`, a
  `controller/exchange.Manager`, a `peer.Set` whose `Fabrics` lookup
  builds `handshake.Fabric{Identity, RootPublicKey, Verifier}` from the
  node's `caseIdentities` (`node/security.go`) and the fabric record's
  root public key, `Names` from the node's mDNS cache or a fresh
  `discovery.Open`, `Resumption` the node's store if `peer.ResumptionStore`
  can be backed by it, else memory). `Node.Peers() *peer.Set`,
  `Node.Bindings() *binding.Manager` (nil when the client stack is off;
  bindings then resolve to Warn "client stack disabled" — a Go-only
  string, say so). A second socket is matter.js-equivalent: the
  initiator's source port is free, the responder answers the source.
- **D5 — the BIND proof is in-repo** (the chip cases are manual YAML).
- **D6 — no new dependency.**

## §1 — declared client clusters (`contract/`, `endpoint/`)

D1. Tests: an endpoint declaring OnOff as client reports it in the
Descriptor's ClientList, satisfies `Client`, and gets the Binding server
mounted; one declaring only an unbindable client (find one in the list)
does not; the existing validation tests hold.

## §2 — devicegen and the 11 types (`script/devicegen`, `device/`)

D2. `go test ./script/devicegen/... ./device/... ./schema/...` and
`make chipdm-check`. A `device/` test that each of the eleven names its
client set as the snapshot does.

## §3 — the Binding server's change hook (`cluster/core/binding.go`)

`Binding.OnChange(func(fabricIndex uint8, added, removed []TargetStruct))`
called after a write commits, the diff by `entryKey` (BM:714-718), per
fabric (the list is fabric-scoped: the write sees one fabric's view —
check how `MatterWrite` receives the fabric index today and cite). At
start, `Entries()` for the manager's initial registration (BS:120-126).
Tests: add/remove/change (= remove+add) diffs; a write that fails
produces no callback.

## §4 — `binding.Manager` and the node's client stack (`binding/`, `node/`)

D3, D4. Resolution order and every drop reason as BM:255-353; the
client kind connects in the background and emits `established` once a
session exists (`Peer.Observe` `SessionAdded`, or immediately when
`HasSession()`); unregister emits `removed` for an established entry;
group entries wait for a key (`Topology.HasGroupKey(fabricIndex,
groupID)` fed by the node's `groups.Manager` key map — read it; a
recheck when it changes, deferred once) and resolve through `GroupSender`;
the server kind resolves locally. `Close` emits `removed` for all.
Tests (fakes for `Peers`, `GroupSender`, `Topology`): each drop reason
with its string; dormant then woken; client kind established on
session; removed on unregister; group waits then resolves; self-binding
server kind; handler panic logged, not raised.

## §5 — the BIND proof (`node/binding_live_test.go` or `binding/`)

`TestLiveBinding`, mirroring chip's TC-BIND-2.1 steps (read
`../connectedhomeip/src/app/tests/suites/certification/Test_TC_BIND_2_1.yaml`
and cite the steps): three in-process `node.Node`s on one fabric,
commissioned by the module's commissioner through `client.Registry`
(the AL live test shows one; three is the same loop) — the DUT with an
`OnOffLightSwitch` endpoint declaring OnOff as client and the client
stack on, two targets each with an OnOff plug; the controller writes
two Binding entries (both targets' node id + endpoint) on the DUT's
switch endpoint; the DUT's test host registered `OnEstablished` and,
per resolution, invokes OnOff `On` then `Off` through
`Resolution.Interaction`; assert both targets' OnOff servers; then the
controller rewrites Binding with one entry → `removed` for the other.
A group entry (TC-BIND-2.2/2.3) is proven only up to the manager's
`GroupSender` call with a fake until AN lands — say so.

## Report

As AK–AM: per §, guards with HEAD, brief lines that differ from the
files, decisions, left out, questions, `by_design` candidates (not
written). Floors: `binding/` 85.
