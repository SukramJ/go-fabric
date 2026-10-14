# Report AP — the `device/` types as the client view, the Binding list persisted (ADR 0020 stage 4 close-out)

Branch `wave22/clientview`, pushed, no PR. matter.js read at `0ceb202d`
(`git -C ../matter.js show 0ceb202d:…`); every citation comment cites
lines read at the pin. Each commit carries its regenerated reachability
inventory (unreached stays **5**; the ratchet constant is untouched).

**Base.** The branch was created from `planning/briefs-wave-22` on top of
`main` (a3cc6dd), which does not hold wave 21: no `binding/`, no
`node/client.go`, no `TestLiveBinding`, no report AO — everything §3 and
D4 build on. I rebased the brief commit onto
`origin/wave/2026-10-14-tranche-21` (cd13fe2, the integrated wave 21);
the branch is that tranche plus the brief commit plus the commits below.
It integrates after tranche 21.

| Commit | Item |
| --- | --- |
| 06de01d `notes/briefs: brief AP …` (rebased) | — |
| 92d36fe `feat(device,endpoint): §1 device.Lookup and the validator's input seam` | §1 |
| a2c3867 `feat(controller/client): §2 the client view — Endpoint.Device and Endpoint.Conformance` | §2 |
| cf1567c `feat(cluster/core,node): §3 the Binding list persisted` | §3 |
| (this commit) `notes: report AP` | — |

## Per section — what was built, what proves it

- **§1** `device.Lookup(id uint32) (*Type, bool)` over the generated
  registry `device/types_gen.go` (`committedTypes`, indexed once); misses
  an id above 0xFFFF, unknown/manufacturer ids and the 16 excluded snapshot
  types (named in its doc). `endpoint.DeviceTypeSubject` (EndpointNumber,
  DeviceTypeIDs, ServerClusterIDs, ClientClusterIDs, ServerClusterShape,
  StatedConditions, PartNumbers — matter.js DeviceTypeFacts.ts:16-69 asked
  of one endpoint), `endpoint.ClusterShape` (FeatureMap, Attributes,
  AcceptedCommands, Events, each with a Known flag) and
  `endpoint.DeviceTypeTree{Endpoints, NodeConditions}` with `Validate()`
  and `Check(ep)`. One adapter (`subjectFacts`) answers the pass's
  internal `dtFacts`; `topologyFacts` is gone, the assembled topology is
  adapted (`topologyTree`/`assembledSubject`) and `ValidateDeviceTypes`
  runs through it. The pass gained one input, `judgeable`: a cluster whose
  FeatureMap is unknown is judged present but nothing inside it; an
  element whose list is unknown is not judged. Tests: every existing
  `endpoint` test unchanged in outcome (`TestTopologyFacts` included —
  `newTopologyFacts` now builds the subject facts);
  `TestDeviceTypeSubjectSameVerdicts` (the assembled fixture copied into
  plain subjects gives the same violations, keys, details, order;
  `Check`), `TestDeviceTypeSubjectUnknownShape`, `TestSubjectFactsTree`;
  `device.TestLookup` (OnOffPlugInUnit found; OtaProvider, Camera,
  CastingVideoPlayer, a manufacturer id, 0x0001_010A, 0xFFFFFFFF miss),
  `TestEveryGeneratedFileIsRegistered` (every `var <X>Type = Type{` in a
  `*_gen.go` is in the registry and nothing else).
- **§2** `client.Endpoint.Device()` (endpoint.go; CS:871-897 cited) and
  `client.Endpoint.Conformance() ConformanceReport` (conformance.go):
  `Status` (`ConformanceUnknown`/`Incomplete`/`Clean`/`Violated`),
  `Violations`, `Clients` (the `client:` requirements, informational),
  `Unjudged`. The image is snapshotted under the node's lock (root via
  owners, installed children), handed to `DeviceTypeTree.Check`; the
  Descriptor's DeviceTypeList/ServerList/ClientList are read from the
  cache, each cluster's shape from its built shape (`injected`),
  FeaturesKnown from the FeatureMap input, AttributesKnown =
  AttributeList reported, CommandsKnown = AcceptedCommandList received,
  events only from a reported EventList. Tests
  (`conformance_internal_test.go`, scripted reports as
  `structure_internal_test.go`): plug → OnOffPlugInUnit with the peer's
  and the snapshot's revision; clean plug; Identify dropped from the
  ServerList → `missing Identify`; OnOff FeatureMap 0 → `missing OnOff.LT`;
  Identify without TriggerEffect → `missing Identify.TriggerEffect`;
  OccupancySensing client → `missing Binding` (Base), clean with Binding;
  OnOffLightSwitch without a ClientList → `Clients` = Identify, OnOff and
  status clean; unknown/excluded type → nil `Device()`, unknown report;
  an endpoint off the image → unknown; OnOff listed but never delivered,
  and OnOff without its FeatureMap → incomplete, `Unjudged=[6]`, no
  violation; no ServerList → unknown; CustomNetworkConfig from the root's
  ServerList. `TestLiveClientNode` asserts the plug's `Device().Name ==
  "OnOffPlugInUnit"` and `Conformance().Status == ConformanceClean`
  against a real `node.Node` (passes).
- **§3** `core.BindingConfig{Load, Save}` and `(*Binding).Persist(cfg)`:
  restores a stored list (nil = nothing stored, kept; non-nil, empty
  included, replaces), notifies the OnChange observers if the list moved,
  installs Save; a failed Load returns its error and installs nothing.
  `MatterWrite` saves before it commits (a failed save refuses the write
  and changes nothing); `RemoveFabric` saves (the entries go whatever the
  save answers). A `writeMu` orders changes with their saves; readers are
  never blocked by the store. The node: `persistBindings` in the wrapped
  Snapshotter restores every Binding server it meets for the first time
  before the bridge serves the topology and before `startBindings`
  attaches it; the set is rebuilt per topology (a server that left is
  forgotten). Key `binding.<stable key>` (`binding.@<endpoint>` for an
  unkeyed endpoint), JSON of `{node, group, endpoint, cluster,
  fabricIndex}`; a list that does not decode is logged
  (`binding.restore`) and overwritten by the next change; a failed save
  logs `binding.persist`. Tests: `core.TestBindingPersist`,
  `TestBindingPersistFailures`; `node.TestNodeBindingPersisted` (write →
  Stop → `node.New` on the database restores the list; `RemoveFabric`
  through `node.RemoveFabricBindings` (export_test.go) persists across the
  next restart); `TestLiveBinding` extended: after the DUT's `Stop`, a new
  `node.New` on its database with a new switch of the same key restores
  `[TH2, group]`, `Bindings().OnEstablished` sees TH2 (client) and the
  group resolution again without a write, and On through the restored
  client resolution switches TH2 on.

## Guards

All at HEAD cf1567c, logged before and after each run (unchanged).

- `make ci`: green (both lint runs "0 issues", cover-check passes, no
  floor moved). History: the first run at c32e500 failed once in `node`
  `TestSessionAndSubscriptionDiagnostics` ("Could not connect to device",
  the brief's known load flake; passes alone and in a package run); the
  second stopped on three gocritic `rangeValCopy` in my new code, folded
  into the §1/§2 commits (autosquash; the rewrite moved the SHAs above).
- `make race`: green.
- `make reachability-check`: green (unreached 5).
- `make chipdm-check` (`CHIP_ROOT=/home/markus/Dokumente/GitHub/connectedhomeip`): green.
- `make chiptool-test` (once): green, 314 PASS. `TestMatterJSController`
  skipped: it looks for matter.js at `../matter.js` relative to the
  worktree (`.claude/worktrees/matter.js`), which does not exist —
  environment, as the worktree `CHIP_ROOT` case of AO (would need
  `GOFABRIC_MATTERJS_DIR`).

## Brief lines that differ from the files

1. **The base.** The brief's files (`node/client.go`, the wrapped
   Snapshotter, `TestLiveBinding`, report AO, `Bindings().OnEstablished`)
   exist only on `wave/2026-10-14-tranche-21`, not on
   `planning/briefs-wave-22`/`main`; rebased onto the tranche (above).
2. "The generator emits `device/registry_gen.go` with `Lookup`": the
   generator already emits the registry, `device/types_gen.go`
   (`committedTypes`, used by `Types()`/`TypeByID(uint16)`). `Lookup` is
   hand-written in `type.go` over it; no generator change, no second
   registry file. The registration test is in `device/registry_test.go`.
3. D4 "the node mounts each endpoint's Binding": the node mounts none.
   Binding servers are built by `device/device.go` (`newBindingServer`)
   and `endpoint/binding_mount.go` (Base's mandate), both outside the
   allowed paths; the node finds them in the topology (`bindingServers`).
   So the option is a method, `Binding.Persist(BindingConfig)`, which the
   node applies to the servers it finds, rather than a `NewBinding`
   option (D4 allowed either shape).
4. D4 key `binding:<endpoint key>` "the same key scheme the labels use":
   the labels use `user_label.root`/`.aggregator`, the per-endpoint keys
   `<prefix>.<stable key>` (`bridged_node_label.`, `scenes.`). Followed
   the files: `binding.<stable key>`.
5. D2 "`device.Lookup` … nil for … the excluded ones": since wave 21 the
   excluded list is 16 types (camera ×8 incl. VideoDoorbell,
   FloodlightCamera; media ×4 incl. CastingVideoClient; infrastructure ×4);
   the eleven controller types are generated and found.

## Decisions where the brief left a choice

- The validator's input is an **interface plus adapters**, not methods on
  `client.Endpoint` / `endpoint.Endpoint`: `endpoint.Endpoint` has fields
  of the names the interface needs (`DeviceType`, `DeviceConditions`) and
  `client.Endpoint` would grow a second public surface; both sides adapt
  (`assembledSubject`, `imageSubject`). One pass, no second
  implementation (D1).
- Four statuses instead of three: `Incomplete` separates "judged clean
  but a listed cluster has no shape yet" from `Unknown` (nothing judged)
  and `Clean`. A violation outranks unjudged clusters (`Violated`).
- **Events** of a remote cluster are judged only when the peer reports an
  EventList; without one they are unjudged and do *not* make the report
  incomplete (no client can learn them; our own bridge publishes none).
  Nine device type requirements are events (e.g. OperationalState
  StateChange).
- **CustomNetworkConfig** for a remote node: true when its root's
  ServerList lacks NetworkCommissioning (a commissioned node without one
  got its network some other way); otherwise false. matter.js derives it
  from the server node's own BLE configuration (ServerEndpointFacts),
  which a client cannot see. No condition is stated for a remote endpoint.
- `Device()` returns nil for an excluded type even though the schema
  judges it; `Conformance()` still judges such a type (the validator works
  on the schema, not on `device/`). Unknown = no listed device type in
  the schema.
- A Binding save failing in `MatterWrite` refuses the write (a plain error,
  classified by the dispatcher's `writeErrorStatus`); in `RemoveFabric` it is ignored by the server
  (the node's Save closure logs it), the fabric is gone either way.
- A stored list that does not decode is overwritten by the next change
  (logged), instead of leaving the server unpersisted.
- The node test seam `node.RemoveFabricBindings` lives in
  `node/export_test.go` (test-only).

## Left out

- The controller-side client node image of a controller endpoint, scenes,
  the `TargetStruct` sentinel (endpoint 0), group bindings beyond wave 21
  (as the brief says).
- A `NewBinding` constructor option (see difference 3): no constructor in
  the allowed paths would use it.
- No `store/` change: the settings key is enough.
- No `script/coverfloor/floors.go` change was needed.

## Open questions

1. `node.Start` calls `n.startGroupSender()` twice in a row
   (node/node.go, tranche 21). It is not idempotent: the second call
   opens a new socket and overwrites `n.groupTx`, so the first group send
   socket is never closed — one leaked UDP socket per `Start`. Likely a
   merge slip; outside my brief, not touched.
2. The stored Binding list of an endpoint whose device is removed for good
   stays in the settings table (as its NodeLabel and scene table do). A
   later device with the same stable key inherits it. Acceptable, or
   should `Aggregator().Remove` drop the endpoint's keyed settings?
3. Should `client.Endpoint.Conformance()` also be offered per node
   (`client.Node.Conformance()` over every endpoint), the way the server
   validator reports a topology? Not built; `DeviceTypeTree.Validate`
   would make it a few lines.

## `by_design` candidates (not written)

- The client view's device type validation: matter.js validates server
  nodes only (DeviceTypeConformanceService, ServerEndpointFacts.ts:25);
  the Go client judges a remote image with the same pass, unknown shapes
  unjudged, events only with an EventList, CustomNetworkConfig from the
  absence of NetworkCommissioning.
- `client.Endpoint.Device()`: matter.js installs no generated device type
  on a client endpoint (ClientStructure.ts:871-897 takes type and name
  from the model); the Go view answers the generated one beside them.
- Binding persistence through the settings table (`binding.<key>`, JSON)
  rather than a behaviour-state store; a write whose save fails is
  refused.

## Findings line this closes

"The Binding attribute is held in memory only" (findings register; the
owner closes it).

## Owner's decisions (Fable, 2026-10-14, tranche 22)

1. **The double `startGroupSender`/`stopGroupSender` on `main`** (question
   1) was the owner's own cherry-pick repair in tranche 21; fixed in this
   tranche's owner commit (one socket per Start again).
2. A stored Binding list inherited by a later device with the same
   stable key (question 2) is the labels' behaviour; accepted and named
   in `BD-Matter-Binding-PersistedInSettings`.
3. No node-level `Conformance()` now (question 3); a host iterates
   `Endpoints()`.
4. The differences 2–5 (the existing `types_gen.go` registry,
   `Binding.Persist` instead of a constructor option, the `binding.<key>`
   scheme, the 16 excluded types) are the files' truth; accepted.
5. `by_design`: `BD-Matter-ClientView-RemoteConformance` and
   `BD-Matter-Binding-PersistedInSettings` written; the findings line on
   Binding persistence closed.
