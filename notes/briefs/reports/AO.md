# Report AO — the binding manager, the node's client stack, the 11 controller device types, the BIND proof (ADR 0020, stage 5, part 2)

Branch `wave21/binding` from `planning/briefs-wave-21` (8d7ec13), pushed,
no PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`),
BindingManager.ts read whole; chip's Test_TC_BIND_2_1.yaml read at the
harness pin 6170af84. Every `// Mirrors`/citation comment cites lines read
at the pin. Each commit carries its regenerated reachability inventory
(unreached stays **5**). No chip-tool was run. Touched: `endpoint/` (§1),
`script/devicegen`, `device/` (§2), `cluster/core/binding.go` + tests (§3,
§4), `binding/` (new), `node/` (§4, §5), `script/coverfloor/floors.go`
(`binding` 85), `CHANGELOG.md`. Not touched: `contract/`, `groups/`,
`secure/`, `transport/`, `bridge/`, `controller/`, `store/`, ADRs,
`notes/parity/`.

| Commit | Item |
| --- | --- |
| 1987981 `feat(endpoint): §1 …` | §1 |
| 74b33ac `feat(device): §2 …` | §2 |
| 3638d95 `feat(cluster/core): §3 …` | §3 |
| 14dc9a4 `feat(binding,node): §4 …` | §4 |
| 1b852b5 `test(node): §5 the BIND proof …` | §5 |
| (this commit) `notes: report AO` | — |

## Per section — what was built, what proves it

- **§1** Most of D1 already existed (wave 6, b4ea1f9): `endpoint.Spec.ClientClusters`,
  `Endpoint.ClientClusters`, the Descriptor's ClientList
  (`materialize.go:313`, `partClusterServers`), the Client condition in the
  validation (`devicetype_conformance.go:141-150, :342-343`). New:
  `endpoint/binding_mount.go` — `baseMandatesBinding` evaluates Base's
  actual Binding requirement ("Simple & Client") against the endpoint's
  own conditions through the validation's lookups (not hand-coded), and
  `mountBinding` mounts the endpoint identity's own `core.Binding`
  (`endpointState.binding`, survives the per-dispatch rebuild) when the
  source supplies none (BS:54-56). Tests `TestDeclaredClientMountsBinding`
  (OnOff client → ClientList, ServerList has Binding, same instance, no
  Binding violation; only WebRtcTransportProvider client → none; none →
  none; verified the validation reports `missing Base Binding` without the
  mount), `TestBaseMandatesBinding`.
- **§2** The eleven move from `excluded` to `committed`; the generator
  already handled client sides. IDs/revisions match the brief's table.
  `device.WithClientClusters` (ME:123-129) added. `TestControllerDeviceTypesParity`
  pins each type's servers and `client.mandatory/optional` against its
  matter.js device file and its client set against the snapshot;
  `TestControllerDeviceDeclaresClients`. `make chipdm-check` green (needs
  `CHIP_ROOT=<absolute>` from a worktree, see question 6).
  Client clusters without a `cluster/spec` definition: WakeOnLan,
  Channel, TargetNavigator, MediaPlayback, MediaInput, LowPower,
  KeypadInput, ContentLauncher, AudioOutput, ApplicationLauncher,
  AccountLogin, ContentControl (all VideoRemoteControl's media clients);
  every other client cluster of the eleven has one.
- **§3** `core.Binding`: `OnChange(fn) (stop func())` after each committed
  write, per fabric, diffed by `TargetStruct.Key` (BM:714-718; change =
  remove+add, BS:127-157); `Entries()`; `MatterReadFiltered`; `HasNode/
  HasGroup/HasEndpoint/HasCluster`. The write was fabric-unaware (its
  comment claimed the dispatcher preserves other fabrics' entries — it
  does not); it now reads the accessing fabric from
  `im.LookupFabricFilter` (stamped by `bridge/receive_dispatch.go:374-376`
  `writeFabricIndex`), replaces only that fabric's entries stamped with
  it (AttributeWriteResponse.ts:461-467), refuses a fabric-less session
  UNSUPPORTED_ACCESS (:246-249); a local write replaces all. The generated
  `[]bindingdef.TargetStruct` is accepted and judged by the definition.
  In §4: `RemoveFabric` (FabricScopedDataHandler.ts:109-122). Tests
  `TestBindingKey`, `TestBindingOnChange` (add/unchanged/remove+change,
  refused writes report nothing, stop), `TestBindingFabricScopedWrite`,
  `TestBindingGeneratedValue`, `TestBindingRemoveFabric`.
- **§4** `binding/`: `Manager` (New, Attach, Detach, Attached, Register,
  Unregister, OnEstablished, OnRemoved, Established, GroupKeysChanged,
  Close), `Resolution{Kind, Entry, SourceEndpoint, Peer, Interaction,
  Endpoint, Group, Clients}`, ports `Topology`, `Peers`, `GroupSender`
  (+`GroupSenderFunc`), `GroupTarget` (AN's `Sender.Invoke` shape). The
  resolution order and every drop as BM:255-353 (`admit` = :258-301),
  every Warn/Info string verbatim as the `reason` attribute
  (`reason.go`, line of each). `node/client.go`: `Config.Client{Enabled,
  Listen, Names}`, second socket + `exchange.Manager` + `peer.Set` whose
  `Fabrics` is built from `caseIdentities`; `Node.Peers()`,
  `Node.Bindings()`; the node wraps its Snapshotter to attach/detach every
  Binding server of the served topology once started (after the CASE
  identities load, BM:65-78), rechecks group entries on
  `groups.Manager.OnGroupcastChanged`, drops a removed fabric's entries,
  closes the manager before the client stack on Stop. Tests (fakes):
  `TestDropReasons` (16 cases, every string), `TestDormantThenWoken`
  (info once per entry, self-binding server kind), `TestParkedAgainWithoutObserver`,
  `TestClientEstablishedOnSession` (background connect, SessionAdded, and
  immediate when a session exists), `TestUnregister`, `TestGroupWaitsForKey`
  (waits, other fabric's change ignored, coalesced recheck, stays when
  the key goes), `TestHandlerPanic`, `TestAttachDetachClose`,
  `TestReplaceAttachedServer`; node: `TestNodeBindingsFollowTheTopology`,
  `TestNodeClientStack`. Coverage `binding` 92.0 % (floor 85).
- **§5** `node/binding_live_test.go` `TestLiveBinding`: TC-BIND-2.1 steps
  2-12, three `node.Node`s commissioned by `client.Registry`, DNS-SD
  injected on both sides (the DUT finds TH2/TH3 through its own `Names`);
  established after the host's late `OnEstablished` (dormancy wake), ACL
  writes (step 7), On through both resolutions' interactions, removal of
  TH3's entry → `removed`, Off → TH2 off, TH3 on; Stop removes the rest.
  Group entry (TC-BIND-2.2/2.3) proven up to the `GroupSender` call: real
  KeySetWrite + GroupKeyMap on the DUT move it from "Group binding waits
  …" to the sender lookup, which the node has none of yet → "Group binding
  peer registration failed". Unit-level with a fake sender it is
  established (`TestGroupWaitsForKey`).

## Guards (HEAD 1b852b5 before and after each)

- `make ci`: green (both lint runs "0 issues", cover-check passes).
- `make race`: green on the second run; the first failed once in `controller/commissioner` `TestConnectionErrorClasses` ("no attempt launched" after its 5 s wait, under full-suite load; untouched package, passes `-race -count=5` alone)
- `make reachability-check`: green (unreached 5)
- `make chipdm-check CHIP_ROOT=/home/markus/Dokumente/GitHub/connectedhomeip`: green

## Brief lines that differ from the files

1. "the reason of :724-737 (three strings)": `ignoredEntryReason`
   (BM:721-737) has **four** messages (one unbindable, several unbindable,
   no client at all, none for the cluster). All four ported.
2. D1 "`contract`'s endpoint description gains `ClientClusters`": the
   Snapshotter returns `*endpoint.Topology` built from `endpoint.Spec`;
   contract has no endpoint description. `Spec.ClientClusters`,
   `Endpoint.ClientClusters`, ClientList and the Client condition existed;
   `contract/` untouched.
3. D2 "`ClientClusters{Mandatory, Optional}`": `device.Type.Clients
   []ClusterRequirement` with per-entry `Mandatory` already carried that
   (generated since wave 6); kept the existing shape.
4. BS:54-60 for the mount: the text is the doc comment at BS:54-56; there is
   no code there (cited :54-56).
5. §5 "invokes OnOff On then Off per resolution": the YAML sends On to
   both (step 7), removes TH3's entry (step 9), then Off (step 10) with TH3
   staying on (step 12). Followed the YAML.
6. "Close emits removed for all": matter.js `close()` (:679-703) emits
   nothing; `disposeServer` (:207-244) does, and BS:52 says removed fires
   on shutdown. `Close` = dispose every server, as the brief says.
7. `Binding.OnChange` returns `stop func()` (needed for Detach).
8. `cluster/core/binding.go:87-90` claimed the dispatcher preserves other
   fabrics' entries before the write; nothing does (see §3).

## Decisions where the brief left a choice

- `Node.Bindings()` is always non-nil; `Node.Peers()` is nil without the
  client stack, and then a node entry drops with "client stack disabled"
  (Go-only string) — group and self-bindings still resolve.
- One worker goroutine serialises all manager state and runs the handlers
  (matter.js's single event loop); a blocking handler delays the manager.
  `Close` must not be called from a handler.
- Dormancy is manager-wide (any `OnEstablished` handler), matter.js's is
  per endpoint's `established` observable — the Go manager has one event
  surface with `SourceEndpoint` in the resolution.
- `Register` ignores a key it already tracks (guards the Attach/OnChange
  window); matter.js's BindingServer never double-registers.
- A client entry always records a pending entry, also when the peer
  already has a session (then established at once through the queue).
- `Resolution.Clients` added (matter.js installs these behaviours on the
  materialised endpoint, :339, :472).
- Client resumption records in memory: the store's `matter_resumption`
  row is the responder's (one per peer) and holds neither session
  parameters nor our CATs.
- `discovery.Open` failing fails `New`; `Client.Names` lets a host/test
  supply the cache.
- Client socket default `[::]:0` / `0.0.0.0:0` (PreferIPv4).
- `TargetStruct` keeps its sentinels: Node/Group/Endpoint 0 absent,
  Cluster absent at 0 or 0xFFFFFFFF (the read encoder omits 0); a target
  endpoint 0 is not representable.
- A wrong-typed Binding write is now CONSTRAINT_ERROR (was FAILURE).
- Not ported: the refcounts (:656-677, as the brief says), "PeerSet lookup
  failed" (:604 — the Go peer comes from the set itself), the
  per-fabric `fabricReplaced/fabricDeleted` key watches (:91-109, the node
  rechecks on every group reload and drops removed fabrics' entries),
  `#installClientBehaviors`/`node.start()` (no client node image here).
- Log message keys: `binding.entry.ignored` (Warn), `binding.entry.dormant`,
  `binding.group.waiting`, `binding.peer.connecting` (Info),
  `binding.established`/`binding.removed` (Debug),
  `binding.handler.failed` (Warn; Error from dispose, as :237),
  `binding.peer.connect_failed` (Debug, Go-only).

## Left out

- Group sending (AN); node does not wire a `GroupSender` yet (question 2).
- ICD, TCP, scenes, the controller-side `ClientGroup` registry node.
- Persistence of the Binding list across restarts (question 3).
- Binding writes over the wire (question 1) — the live test writes
  locally on the session's fabric when the wire answers CONSTRAINT_ERROR.

## Open questions

1. **The bridge decodes no Binding write.** `bridge/attribute_value_reader.go`
   has no case for 0x001E (neither `attributeValueReader` nor
   `listItemValue`), so a controller's Binding write reaches the server as
   nil (CONSTRAINT_ERROR now, FAILURE before) — TC-BIND over the wire
   cannot pass. The fix is two cases using `generatedAttributeValue(
   bindingdef.Definition, …)` (whole list and list item); `core.Binding`
   already accepts the generated type. Also `bridge/reply.go:1034-1053`
   encodes `[]TargetStruct` by sentinels (omits endpoint 0). `bridge/` was
   out of my paths. Once fixed, `TestLiveBinding` takes the wire path by
   itself.
2. After AN: wire `groups.Manager.Sender` into `node.newBindings` as
   `binding.GroupSenderFunc(func(ctx, f, g) (binding.GroupTarget, error) {
   s, err := m.Sender(ctx, f, g); if err != nil { return nil, err }; return s, nil })`.
   And `Topology.HasGroupKey` checks the key map only
   (`groups.Manager.HasKeyMapping`); matter.js also requires the key set
   to exist (BM:488-489) — groups exposes no per-group key-set check.
3. Binding is `N` (nonvolatile); `core.Binding` is memory-only, so entries
   vanish on restart (pre-existing). Persist through the endpoint store?
4. Key-change signal: `groups.Manager.OnGroupcastChanged` fires on every
   `Reload`; acceptable, or a dedicated key-change observer in groups/?
5. The DUT's peer set resolves targets through mDNS only (as matter.js);
   a host may seed `Node.Peers().AddKnownPeer`. OK?
6. `make chipdm-check` from a worktree resolves `CHIP_ROOT ?= ../connectedhomeip`
   relative to the worktree; it needs an absolute `CHIP_ROOT`.

## `by_design` candidates (not written)

- Resolution names the target endpoint and clients instead of a
  materialised client endpoint.
- Handlers run on the manager's one worker goroutine.
- Dormancy is manager-wide, not per endpoint.
- The client stack's second UDP socket (initiator source port free).
- Client CASE resumption records in memory.
- "client stack disabled" (Go-only reason).
- `TargetStruct` sentinel encoding (endpoint 0 not a target).
