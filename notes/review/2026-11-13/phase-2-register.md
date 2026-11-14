# Phase 2 — the dispositions

The commit ids below are those of the phase 2 pull request's branch (#526)
(`review2/p2`, `a9134e81..540cb0eb`, 154 commits). If that pull request
merges squashed, as the two before it did, its single commits stay
retrievable through the pull request's reference. Items that are still open
after this branch and describe a weakness a party on the network could use
are held back with their text until the pull request that fixes them (owner
decision 25); the table carries their id, severity and phase only, and
"Counts" says how many.

One row per finding of the three review runs of phase 2 — the review of the
seven flows (`comm`, `fabric`, `sess`, `ctr`, `store`, `stop`, `alloc`; 69
findings), the diff review of the fix campaign (`dr-noc`, `dr-req`,
`dr-ctl`, `dr-node`; 22) and the review of the rework after it (`rr-dev`,
`rr-ctl`; 14) — one per item phases 0 and 1 handed to this phase, one per
design note of the flow reviewers, one per item the fix agents, the citation
passes and the coordinator noticed and did not fix where it was noticed, and
one per unit for what its reviewer did not read. Sources: the two result
files of the review workflow, the two reports of the rework's review, the
twenty fix notes (`phase-2-fix-*.md`), the three citation passes, the
coordinator's open list, `decisions.md` (owner decisions 34–49) and `git log
a9134e81..540cb0eb`. The notes are not part of this change; one that
mentions an item held back follows with the pull request that fixes that
item (owner decision 25).

How a row was placed — by rule:

- **fixed `<commit>`** — the commit is in `a9134e81..540cb0eb`, and its
  subject or its fix note's table names the ID. For every finding of high or
  medium severity, the reviewer's or the verifier's, the files the commit
  touches were read off `git show --stat`; where it does not touch the file
  the finding names, the row says what it touches instead. "corrected by"
  names the finding of a later review that found the fix wanting and the
  commit that repaired it. Which fix a later finding corrects is the
  register editor's pairing: by the finding it names, or by the mechanism it
  names and the commit that introduced it (`git log -S`). The row gives the
  state at `540cb0eb`. A register row the fix brought is named after it.
- **by_design `<row>`**, **Ahead `<row>`** — the row is in
  `notes/parity/by_design.md` at `540cb0eb`; **open finding `<id>`** — the
  entry is in `notes/parity/matter_behaviour_findings.md` at `540cb0eb`.
- **parity, no change** — the behaviour is matter.js's at the pin; the lines
  were re-read for this file.
- **struck: `<reason>`** — the reason is the fix note's or the
  coordinator's.
- **phase N: `<what>`** — placed by the plan's cut (`README.md`: 3
  adversarial, 4 parity, 5 architecture, tests, docs and the API bundle, 6
  the full pass). The placement is the coordinator's where his open list
  names the item, the register editor's otherwise; the coordinator may move
  it.

Severity is the verifier's where a finding was verified — every high and
medium of the flows' review (21) and of the diff review (2) — with the
reviewer's in brackets where the verifier narrowed it (10); a low or note
carries the reviewer's severity and was not verified before its fix. The
review of the rework had one reviewer per unit and no verifier; "PLAUSIBLE"
in a row is that reviewer's own word. "Re-read" after a reference's lines
means read for this file in the exported tree of the pin (matter.js
`db021da0`, connectedhomeip `6170af84`); a reference statement without it is
its source's.

## The findings

### The seven flows

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P2-comm-01 | medium | fixed `a398a07f`; by_design `BD-Matter-PendingNOCMarker` (rewritten) | `cluster/core/operational_credentials.go` — the removal of another fabric leaves an armed fail-safe's pending row and its CSR; owner decision 36. The same defect as R2-P2-fabric-01; held back in phase 1 as a coordinator's addition |
| R2-P2-comm-02 | low (medium) | fixed `620c86f3` | `cluster/core/access_control.go` — a fabric's Extension entry goes with the fabric (owner decision 37). The same defect as R2-P2-fabric-03 and R2-P2-store-08 |
| R2-P2-comm-03 | low (medium) | fixed `a398a07f` (the rollback at start keeps what it rolled back), `b8a5c0a6` (the node runs the consequences), corrected by R2-P2-dr-noc-05 `c9d5a5a8` | `cluster/core/operational_credentials.go`, `node/node.go` — a fabric a start rolled back runs its removal consequences and the host's `OnFabricRemoved` before the node serves |
| R2-P2-comm-04 | low | fixed `a398a07f` | `cluster/core/operational_credentials.go` `handleUpdateNOC` — UpdateNOC is one store write; a failed one writes nothing. Held back in phase 1 as a coordinator's addition |
| R2-P2-comm-05 | low | fixed `a398a07f`, `991febc6`, corrected by R2-P2-dr-noc-04 `4e6de287` | `cluster/core/operational_credentials.go` — a failed AddNOC's row stays pending until a fail-safe's end, a fresh arm or a start removes it; the host hears no removal of a fabric it never heard added. N-sec3-1 of phase 1 |
| R2-P2-comm-06 | low | fixed `a398a07f`, corrected by R2-P2-dr-noc-02 `30a1e142` | `cluster/core/vid_verification.go` — the VID data is the fabric row's, and the store keeps a pending update's prior values. N-sec3-2 of phase 1 |
| R2-P2-comm-07 | low | fixed `a398a07f` | `cluster/core/general_commissioning.go` — terms, the network list, then the fabric's commit as the last write; a failed step writes the earlier ones back |
| R2-P2-comm-08 | low | fixed `7a21594b`, corrected by R2-P2-dr-noc-01 `1e194ad4` and -03 `abf68c39`; by_design `BD-Matter-RevokeCommissioning-PASEArmedFailSafeOnly` | `bridge/commissioning_window.go`, `cluster/core/general_commissioning.go` — opening a window arms nothing (owner decision 47), RevokeCommissioning expires only a fail-safe armed over PASE (owner decision 42); `bridge.FailSafeArmer` deprecated in `d4d65978`. The two observations of phase 1's U-core-1 |
| R2-P2-comm-09 | low | fixed `f0720417` | `cluster/core/general_commissioning.go` `handleCommissioningComplete`. N-core2-1 of phase 1 |
| R2-P2-comm-10 | low | fixed `a398a07f` | `cluster/core/operational_credentials.go` `handleAddNOC` — an unreadable fabric list answers FAILURE before any write |
| R2-P2-comm-11 | low | phase 5: the API bundle — the fix changes an exported hook | `cluster/core/general_commissioning_rollback.go` — a PASE session established while a rollback closes the PASE sessions is refused its arm although it survived; confirmed by the fix agent's throwaway test at the level of `cluster/core`, not run on a node. The arm has to know which sessions the rollback closed, and `SetOnFailSafeRolledBack`'s hook names none. Costs the commissioner a retry |
| R2-P2-comm-12 | note | fixed `a398a07f` | the commit is one `CommitFabric`. The same defect as R2-P2-store-05 |
| R2-P2-comm-13 | note | parity, no change | `controller/commissioner/commissioner.go` — the commissioner deletes the peer when the flow fails, a lost CommissioningComplete answer included; matter.js deletes it on any error of `executeCommissioning` (`packages/protocol/src/peer/ControllerCommissioner.ts:719-733` at `db021da0`, re-read: the delete is `:727-730`). A probe of the device's fabric table after a lost answer would be ahead of matter.js; it was not put to the owner |
| R2-P2-comm-14 | low | fixed `4b6538b9` (the row); by_design `BD-Matter-PendingNOCMarker` (rewritten) | `notes/parity/by_design.md` — the row said matter.js persists a new fabric only in `completeCommission`; it now says when matter.js writes its fabric table, and describes the fabric row |
| R2-P2-fabric-01 | medium | fixed `a398a07f` | the same defect as R2-P2-comm-01 |
| R2-P2-fabric-02 | low (medium) | fixed `ea109bc5` | `node/client.go`, `node/node.go` — a removed fabric's client-stack peers are deleted, and UpdateNOC ends their sessions and resumption records. The tests drive the two helpers; the hook wiring was read, not run end to end |
| R2-P2-fabric-03 | low (medium) | fixed `620c86f3`; by_design `BD-Matter-FabricState-ReconciledWithoutFabrics` | `contract/fabric.go`, `node/fabric_state.go`, the servers of `cluster/core` and `cluster/tls` — one removal contract, and a reconcile against the fabric table at start and when an endpoint joins (owner decision 37); guard `script/fabricscopecheck` (`81172497`) |
| R2-P2-fabric-04 | low (medium) | fixed `8865c5ce`; Ahead `BD-Matter-Camera-FabricRemovalEndsStreams`; by_design `BD-Matter-WebRtcRequestor-ACLThroughNode` (amended) | `cluster/core/messages.go`, `cluster/camera` |
| R2-P2-fabric-05 | low | fixed `a880e593` | `node/node.go`, `node/icd.go`. N-node2-1 of phase 1 |
| R2-P2-fabric-06 | low | fixed `620c86f3`, corrected by R2-P2-dr-node-03 `24c27eb8`, `9de6338f` | `node/client.go`, `node/fabric_state.go` — the reconcile |
| R2-P2-fabric-07 | low | fixed `a398a07f` | `cluster/core/operational_credentials.go` — a rollback is reported once, when it lands. N-sec3-3 of phase 1 |
| R2-P2-fabric-08 | low | fixed `2428589b` (camera), `fd556658` (topology) | `controller/camera/reports.go`, `controller/topology/service.go` — `Hooks.Deleted` chained in both. N-ctl2-1 of phase 1; the topology half is a row below (N-cite-4) |
| R2-P2-fabric-09 | low | fixed `d802d9a7`; Ahead `BD-Matter-CASE-RemovedFabricRefusedAtCompletion` | `node/security.go`. The same defect as R2-P2-sess-05 |
| R2-P2-sess-01 | medium | fixed `6c4d9b07`, `8d79e692` | `bridge/subscription_reestablish.go`; `store` — `save_seq`, migration v8, so the SQL store's order is the save order |
| R2-P2-sess-02 | low (medium) | fixed `1d265303`; by_design `BD-Matter-IdleSessionReaper` (amended) | `secure/operational/manager.go`, `bridge/conn.go` — the reaper spares a session with a live subscription (owner decision 43). The commit does not touch the file the finding names (`im/subscription/subscription.go`, the cadence, unchanged); it touches that package's test |
| R2-P2-sess-03 | low | fixed `615d7d37` (the flush at Stop), `edfbe40c` (the comment), corrected by R2-P2-dr-req-01 `41c939a5`; by_design `BD-Matter-Shutdown-CloseSessionKept` | `bridge/bridge.go`, `secure/operational/manager.go` — half fixed, half decided: the CloseSession at shutdown stays (owner decision 49). The controller's half of that decision is `d6fcddf6`, row `BD-Matter-PeerLoss-OwnSessionAtTheSameInstant` |
| R2-P2-sess-04 | low | fixed `a3fe0892`; closes open finding `F-db021da0-14` | `bridge/subscribe.go` |
| R2-P2-sess-05 | low | fixed `d802d9a7` | the same defect as R2-P2-fabric-09 |
| R2-P2-sess-06 | low | fixed `a792ed5a` | `bridge/conn.go`, `bridge/outbound_reliable.go` — one purge on every close path |
| R2-P2-sess-07 | low | fixed `ae3917ad` | `bridge/subscribe_dispatch.go`, `bridge/subscription_reestablish.go` |
| R2-P2-sess-08 | low | fixed `4eb52929` | `im/subscription/persistence.go` |
| R2-P2-sess-09 | low | fixed `ae1ac9cc`, corrected by R2-P2-dr-req-05 `cbceef0d` | `bridge/subscribe.go`. N-bridge2-4 of phase 1 |
| R2-P2-sess-10 | note | fixed `a792ed5a` | `bridge/conn.go`. S-21 of phase 0 |
| R2-P2-ctr-01 | medium | fixed `85788456` | `client/exchange/manager.go` — an exchange is keyed by its session and index |
| R2-P2-ctr-02 | low (medium) | fixed `da8c16ef` | `transport/mrp/window.go`, `secure/channel/session.go`; `NewWindowNoRollover` deprecated |
| R2-P2-ctr-03 | low | fixed `e8c26628`; phase 6: one part | `bridge` — the unsecured counter and the exchange ids start at random values, the ids use 16 bits. Open, low: an exchange id that a live exchange of the session holds is not skipped on the device (it keeps no table of the exchanges it opened), where matter.js skips it (`packages/protocol/src/protocol/ExchangeManager.ts:189-198` at `db021da0`, re-read) |
| R2-P2-ctr-04 | low | fixed `18e138b1`, corrected by R2-P2-dr-req-02 `76d49fde` | `bridge/receive.go` — an unsecured source's duplicate window lives as long as the handshake it serves. One part the fix left is a held-back row below (N-ctr-1) |
| R2-P2-ctr-05 | low | fixed `f09f79ac`, `67b562e4`, `d23227f4`, corrected by R2-P2-dr-req-03 `a0772988` | `endpoint`, `im`, `bridge`, `contract/matter.go` — a DataVersion for every cluster instance (owner decision 46). W2-wire-10 of phase 0; the tracker's own zero is a row below (N-ctr-3) |
| R2-P2-ctr-06 | low | fixed `f09f79ac` | `im/write.go` |
| R2-P2-ctr-07 | low | fixed `2ceede0c` | `node/node.go`; with R2-P2-store-02 |
| R2-P2-ctr-08 | low | fixed `d2f57963` (device), `78f284a4` (controller), corrected by R2-P2-dr-req-07 `36351820` and R2-P2-rr-ctl-02 `14085d3d`; by_design `BD-Matter-SessionRolloverClose` | `secure/channel/session.go`, `bridge/reply.go`, `client/exchange/manager.go` |
| R2-P2-ctr-09 | low | fixed `121b589c` | `bridge/receive.go` |
| R2-P2-store-01 | medium (high) | fixed `a3df68d6` | `cluster/core/network_commissioning_wifi.go`, `node/root.go` — the network list is stored before the fabric's commit |
| R2-P2-store-02 | medium | fixed `2ceede0c` | `im/eventlog.go`, `node/node.go`; `EventLog.SetCounterPersistence` deprecated |
| R2-P2-store-03 | low (medium) | fixed `7da60194` | `node/surface_stability.go`. N-node2-2 of phase 1 |
| R2-P2-store-04 | medium | fixed `d17f858d`, `09360722`, `b6ac305a`, corrected by R2-P2-dr-node-01 and -04 `12d01be8` and -05 `d0deb7d5`; by_design `BD-Matter-NonVolatileWrite-StoredBeforeApplied` | `node/nonvolatile.go`, `endpoint/dispatcher.go`, `device/held.go` — one persister at the node (owner decisions 15 and 38). The commit does not touch the file the finding names (`device/defaults.go`); it touches `device/held.go` and its test. R2-P0-store-B06; what the cut leaves out is a row below (N-persist-1) |
| R2-P2-store-05 | low | fixed `a398a07f` | the same defect as R2-P2-comm-12 |
| R2-P2-store-06 | low | fixed `33f77c05` | `node/persist.go` — one `SetSettings` (owner decision 39, `af18e97e`) |
| R2-P2-store-07 | low | fixed `d17f858d` | `node/nonvolatile.go` |
| R2-P2-store-08 | low | fixed `620c86f3` | the same defect as R2-P2-comm-02 |
| R2-P2-store-09 | low | fixed `fdaf4012`, corrected by R2-P2-dr-ctl-02 `d05fcb4d` | `controller/client/node.go`, `jobs.go`. N-ctl2-2 of phase 1 |
| R2-P2-store-10 | low | struck: it mirrors matter.js | `controller/client/registry.go` — matter.js's cull awaits `node.delete()` inside its loop with no catch of its own, so an error ends the pass and is logged (`packages/node/src/node/client/Peers.ts:713-717`, `:680-739`, `:668-672` at `db021da0`, re-read), and it skips a node that is not ready (`:684-686`). That a node whose delete failed is then not ready in matter.js is the fix note's inference, not traced. N-ctl2-3 of phase 1 |
| R2-P2-store-11 | low | fixed `1f26dce5` | `cluster/core/icd_management.go` |
| R2-P2-stop-01 | low (medium) | fixed `ce084c74`, corrected by R2-P2-dr-ctl-01 `e3c062d4` | `controller` — one closed gate on every public method (owner decision 41) |
| R2-P2-stop-02 | low | fixed `9c42703d`, `97a87e89` (docs) | `node/commissioning.go`, `bridge/commissioning_window.go` |
| R2-P2-stop-03 | low | fixed `08f34381`, `13d687e1` | `controller/camera`. N-ctl2-4 of phase 1 |
| R2-P2-stop-04 | low | fixed `9c42703d` | `node/commissioning.go`. N-req3-2 of phase 1 |
| R2-P2-stop-05 | low | fixed `91172715`, `97a87e89` (docs); by_design `BD-Matter-Bridge-StartDuringStopRefused` (amended) | `bridge/bridge.go` — a Bridge is single-run (owner decision 40) |
| R2-P2-stop-06 | low | fixed `4d7bf619` | `cluster/core/general_commissioning.go`, `node/node.go` |
| R2-P2-stop-07 | low | fixed `08f34381` | `controller/camera/service.go` |
| R2-P2-stop-08 | note | fixed `d56761b2` | `node/node.go` |
| R2-P2-alloc-01 | high | fixed `bb35c313` | `controller/client/structure.go` |
| R2-P2-alloc-02 | high | fixed `55def8a4`, corrected by R2-P2-dr-ctl-04 `9580db1b`; Ahead `BD-Matter-Controller-UnsecuredSessionsBounded`, `BD-Matter-Controller-UnsecuredAckWaitsBounded`, `BD-Matter-Controller-UnsecuredSessionLifetime`, `BD-Matter-UnauthenticatedIntervalsClamped` | `client/exchange`, `secure/sessionparams` — owner decision 45. The commit does not touch the file the finding names (`client/handshake/server.go`) or its tests: the clamp is in `Session.SetTimingParameters` (`client/exchange/session.go`), which that file calls at the line the finding names (read at `540cb0eb`). The device's half of the clamp is `e261d8c7` |
| R2-P2-alloc-03 | high | fixed `6c69d070`, `314166cf`; Ahead `BD-Matter-Controller-DnssdCacheBounded`, `BD-Matter-Controller-DnssdTTLCeiling` | `client/discovery/names.go` — owner decision 45. The observation of phase 1's U-net-1 |
| R2-P2-alloc-04 | high | fixed `30292398`, `980d755d`, `4386de3a`, corrected by R2-P2-dr-req-04 `4eef4527`; by_design `BD-Matter-Read-RepeatedPathsDropped` (rewritten); open finding `F-db021da0-16` | `im/read_expansion.go`, `bridge` — read and priming responses are generated chunk by chunk (owner decision 44). The ongoing subscription reports and the event reports of a read stay one slice each; the fix note's reason: they are bounded by the node's attributes and by the event log |
| R2-P2-alloc-05 | medium | fixed `a6950779`, `30ffe7ba`; Ahead `BD-Matter-Controller-ReportAssemblyBounded` | `client/interaction` — owner decisions 45 and 48; `6d4c721e` and `b0c95399` extend the bound to the commissioning steps and to a pushed report's first chunk |
| R2-P2-alloc-06 | low | fixed `64f838d0`, `54e8fb24`; Ahead `BD-Matter-BDX-BlockPastLengthRefusedBeforeWrite`, `BD-Matter-OTARequestor-BDXSizeCap` | `bdx/session.go`, `cluster/core/ota_requestor_query.go` |
| R2-P2-alloc-07 | low | fixed `660aba79`, corrected by R2-P2-dr-req-06 `ee95970e` | `im/read_expansion.go`, `bridge` — the commit does not touch the file the finding names (`im/subscription/manager.go`) |
| R2-P2-alloc-08 | note | fixed `4c6d4b4e`, corrected by R2-P2-dr-ctl-03 `d6e47900`; Ahead `BD-Matter-Controller-DclListingBounded` | `controller/dcl/client.go` |

### The diff review of the fix campaign

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P2-dr-noc-01 | medium | fixed `1e194ad4` | `bridge/securechannel.go` — the PASE lockout ends the window as the end of commissioning does. The commit changes the caller and its test, not the file the finding names (`bridge/commissioning_window.go` `RevokeWindow`). Row `BD-Matter-RevokeCommissioning-PASEArmedFailSafeOnly` |
| R2-P2-dr-noc-02 | low | fixed `30a1e142`, `e6586924` | `store`, `cluster/core` — `UpdateFabricCredentials` writes the NOC, ICAC, private key and node id only |
| R2-P2-dr-noc-03 | low | fixed `abf68c39`, corrected by R2-P2-rr-dev-07 `41c9c961` | `cluster/core/general_commissioning.go` |
| R2-P2-dr-noc-04 | low | fixed `4e6de287`, `74345572` (docs), corrected by R2-P2-rr-dev-01 `7ec977ba`, -03 `e07b6f14` and -06 `37cf15c8`; by_design `BD-Matter-PendingNOCMarker` (amended) | `cluster/core` — a fresh fail-safe context starts before its arm is recorded |
| R2-P2-dr-noc-05 | low | fixed `c9d5a5a8`, corrected by R2-P2-rr-dev-09 `540cb0eb` | `cluster/core/operational_credentials.go`, `node/node.go`. The same defect as R2-P2-dr-node-02 |
| R2-P2-dr-noc-06 | low | fixed `c6ebb2e1` (tests) | `store/fabric_pending_test.go`, `store/memstore` |
| R2-P2-dr-req-01 | low | fixed `41c939a5`, corrected by R2-P2-rr-dev-05 `2a96c989` | `bridge/subscribe.go` |
| R2-P2-dr-req-02 | low | fixed `76d49fde`, corrected by R2-P2-rr-dev-02 `44879cd3` | `bridge/receive.go`. One residual is a held-back row below (N-rw3req-3) |
| R2-P2-dr-req-03 | low | fixed `a0772988`, `4464d1c5` (the citation), corrected by R2-P2-rr-dev-04 `8db0e4c2` | `bridge/receive_dispatch.go` — one advance per command; what follows for a host's change during a command is a row below (N-rw3req-1) |
| R2-P2-dr-req-04 | low | fixed `4eef4527` | `im/read_expansion.go` |
| R2-P2-dr-req-05 | low | fixed `cbceef0d`, `c6aa1fa6` | `bridge/bridge.go`, `bridge/subscribe.go` |
| R2-P2-dr-req-06 | low | fixed `ee95970e` | `bridge/subscribe_dispatch.go`; see N-rw3req-2 |
| R2-P2-dr-req-07 | note | fixed `36351820`, `93a5a10c`; by_design `BD-Matter-SessionRolloverClose` | `im/subscription/manager.go`, `bridge/reply.go` |
| R2-P2-dr-ctl-01 | low | fixed `e3c062d4` | `controller/diagnostics.go` |
| R2-P2-dr-ctl-02 | low | fixed `d05fcb4d` | `controller/client` |
| R2-P2-dr-ctl-03 | low | fixed `d6e47900`; Ahead `BD-Matter-Controller-DclListingBounded` (amended) | `controller/dcl/client.go` |
| R2-P2-dr-ctl-04 | low | fixed `9580db1b`; Ahead `BD-Matter-Controller-UnsecuredSessionOneHandshake` | `client/exchange` — PLAUSIBLE in the review; settled by the fix's test at the manager's level, with no run against the production CASE responder |
| R2-P2-dr-node-01 | medium | fixed `12d01be8`, `bebaed81`, corrected by R2-P2-rr-ctl-01 `4e9d88d2`, -04 `349c7695` and -05 `577d1e60`; by_design `BD-Matter-NonVolatileWrite-StoredBeforeApplied` (amended) | `node/nonvolatile.go` — every change of a nonvolatile attribute the module holds is stored, whoever made it |
| R2-P2-dr-node-02 | low | fixed `c9d5a5a8` | the same defect as R2-P2-dr-noc-05 |
| R2-P2-dr-node-03 | low | fixed `24c27eb8`, `9de6338f` | `node/client.go`, `node/fabric_state.go` |
| R2-P2-dr-node-04 | low | fixed `12d01be8` | `node/nonvolatile.go` — one lock per key |
| R2-P2-dr-node-05 | note | fixed `d0deb7d5` | `node/nonvolatile.go` |

### The review of the rework

One reviewer per unit read the rework after the diff review as a whole
(`114dee6d..9580db1b`).

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P2-rr-dev-01 | medium | fixed `7ec977ba` | `cluster/core/general_commissioning_rollback.go`, `operational_credentials.go` — no fail-safe is armed over a row its start left pending, and CommissioningComplete commits its fabric's row or fails; measured by the reviewer's throwaway test |
| R2-P2-rr-dev-02 | low | fixed `44879cd3` | `bridge/receive.go` — a window is bound to at most four exchanges; the number is the fix agent's, with no reference line (`docs/guides/limits.md`) |
| R2-P2-rr-dev-03 | low | fixed `e07b6f14` | `cluster/core/general_commissioning.go`, `general_commissioning_rollback.go` |
| R2-P2-rr-dev-04 | low | fixed `8db0e4c2` | `bridge/receive_dispatch.go`; the branch without a manager is a row below (N-findev-2) |
| R2-P2-rr-dev-05 | low | fixed `2a96c989` | `bridge/subscribe.go` — traced by the reviewer, not run before the fix |
| R2-P2-rr-dev-06 | low | fixed `37cf15c8` | `cluster/core/operational_credentials.go` — traced by the reviewer, not run before the fix |
| R2-P2-rr-dev-07 | low | fixed `41c9c961` | `cluster/core/general_commissioning.go`; see N-findev-4 |
| R2-P2-rr-dev-08 | note | held back until its fix merges (owner decision 25) — phase 3 | — |
| R2-P2-rr-dev-09 | note | fixed `540cb0eb` | `node/node.go` — PLAUSIBLE in the review, settled by the fix's test; what the fix cannot reach is a row below (N-findev-3) |
| R2-P2-rr-ctl-01 | medium | fixed `4e9d88d2`, `49331c94` (docs) | `node/nonvolatile.go` — measured by the reviewer's throwaway test |
| R2-P2-rr-ctl-02 | low | fixed `14085d3d`; by_design `BD-Matter-SessionRolloverClose` (amended) | `client/exchange`, `client/peer` |
| R2-P2-rr-ctl-03 | low | fixed `91a95f08` (the row); by_design `BD-Matter-NonVolatileWrite-StoredBeforeApplied` (amended) | `notes/parity/by_design.md` — the row states both orders and no longer says the stored form is the written value |
| R2-P2-rr-ctl-04 | note | fixed `349c7695` | `node/nonvolatile.go` — PLAUSIBLE in the review, confirmed by the fix's test |
| R2-P2-rr-ctl-05 | note | fixed `577d1e60`, `49331c94` (docs), `d97d5bb4` | `node/nonvolatile.go` |

## What phases 0 and 1 handed to phase 2

Each flow's reviewer was given the open items of its flow and reported their
status at `940c99b7`; one item was given to two flows.

| ID | flow | status at the review | where it went |
| --- | --- | --- | --- |
| N-sec3-1 | comm | holds, reproduced | R2-P2-comm-05, fixed `a398a07f` |
| N-sec3-2 | comm | holds, traced | R2-P2-comm-06, fixed `a398a07f` |
| N-core2-1 | comm | holds, traced | R2-P2-comm-09, fixed `f0720417` — held back in phase 1's table; its text is that finding's |
| R2-P1-dr-sec-01 (the phase 2 part) | comm | holds | R2-P2-comm-10, fixed `a398a07f` |
| R2-P1-rw-05 (the phase 2 part) | comm | holds by trace, not reproduced | R2-P2-comm-11, phase 5 |
| R2-P1-rw-06 (the phase 2 part) | comm | holds | R2-P2-comm-12, fixed `a398a07f` |
| N-node2-1 | fabric | holds, measured; wider than the row said (the published record, not only the bridge's entry) | R2-P2-fabric-05, fixed `a880e593` |
| N-sec3-3 | fabric | holds, read | R2-P2-fabric-07, fixed `a398a07f` |
| N-ctl2-1 | fabric | holds, read | R2-P2-fabric-08, fixed `2428589b`, `fd556658` |
| R2-P1-ctl-05 (the phase 2 part) | fabric | the same item as N-ctl2-1; the ICD watch part phase 1 fixed was not tested again | R2-P2-fabric-08, fixed `2428589b` |
| N-bridge2-4 | sess | holds, read, not run | R2-P2-sess-09, fixed `ae1ac9cc` |
| S-21 | sess | one of its two sites remains; no failure reachable | R2-P2-sess-10, fixed `a792ed5a` |
| F-db021da0-14 | sess | holds as recorded, and the Busy refusal precedes the teardown too | R2-P2-sess-04, fixed `a3fe0892`; the finding is closed |
| W2-wire-10 | ctr | holds, measured on a default node | R2-P2-ctr-05 and -06, fixed `f09f79ac`; owner decision 46 |
| R2-P0-store-B06 | store | holds, and larger than the one attribute named | R2-P2-store-04, fixed `d17f858d`; owner decision 38 |
| N-node2-2 | store | the cancelled context is harmless; the defect on that path is another | R2-P2-store-03, fixed `7da60194` |
| N-ctl2-2 | store | holds | R2-P2-store-09, fixed `fdaf4012` |
| N-ctl2-3 | store | holds, and the cull ends its pass at the first failed delete | R2-P2-store-10, struck: it mirrors matter.js |
| R2-P1-rw-06 (the phase 2 part) | store | holds | R2-P2-store-05, fixed `a398a07f` — the same item as in the commissioning flow |
| N-ctl2-4 | stop | holds, traced | R2-P2-stop-03, fixed `08f34381` |
| N-req3-2 | stop | holds, traced | R2-P2-stop-04, fixed `9c42703d` |

Held back in phase 1 and public with this branch, beside N-core2-1 above:

| row | what it said | where it went |
| --- | --- | --- |
| phase 1, the coordinator's first addition (phase 2, unrated) | an UpdateNOC that fails after its identity write leaves the half-applied identity, and a later CommissioningComplete in that fail-safe commits it | R2-P2-comm-04, fixed `a398a07f` |
| phase 1, the coordinator's second addition (phase 2, unrated) | `notifyFabricRemoved` clears every pending state in memory when the removed fabric is `currentFabric` and leaves the markers on disk, so a pending AddNOC or UpdateNOC can be committed without CommissioningComplete | R2-P2-comm-01, fixed `a398a07f` |
| U-core-1 (phase 1; placed in phase 3 after a read in phase 2) | unread there: `bridge/commissioning_window.go`. Two things seen and not followed up: OpenCommissioningWindow arms the fail-safe for fabric 0 without an ownership check, and `AutoArmOnPaseEstablished` checks "already armed" and arms in two lock holds | read by the commissioning flow; both hold; R2-P2-comm-08, fixed `7a21594b` (owner decision 47) |
| U-net-1 (phase 1; phase 3) | unread there: `client/discovery/names.go` `handleMessage`, the scanner's ingest; seen: the cache has no cap on names or records and takes TTLs up to 2^32 s | traced by the allocations flow; R2-P2-alloc-03, fixed `6c69d070` |

Phase 1 also handed over the pending-NOC marker as a whole ("For phase 2"):
read as a whole by the commissioning flow (D-comm-1 to -3) and replaced by
the fabric row (owner decision 36). W6-test-17 of phase 0 is under "Not
placed, not verified".

## The reviewers' design notes

No severity: a design note is a question or a recommendation, not a finding.

| ID | the note | where it went |
| --- | --- | --- |
| D-comm-1 | nine states the pending-NOC marker's two keys and the memory fields cannot represent or handle wrongly | owner decision 36; fixed `a398a07f` on the store's `b07ccf63`, `56391460`. State (h), a marker that does not decode fails the constructor for good, was not read again for the one start-up path that still reads the old keys |
| D-comm-2 | what the two references do with a fabric between AddNOC and CommissioningComplete | by_design `BD-Matter-PendingNOCMarker` (rewritten); what matter.js serves after a restart inside the window is N-cite-1 |
| D-comm-3 | keeping the uncommitted fabric in memory, its cost, and a middle path | owner decision 36 took the middle path: the pending mark on the row |
| D-comm-4 | four questions for the owner | (a) owner decision 42; (b) owner decision 47; (c) the expiry hook's fabric argument has two meanings for a host that wires `SetOnFailSafeExpired` itself — phase 5: the API bundle; (d) a commissioning that Stop aborts is rolled back by the next process and the host is not told before the node ends — phase 6. (c) and (d) were not put to the owner |
| D-fabric-1 | one removal contract in place of a hand-kept list of calls | owner decision 37; fixed `620c86f3`, guard `81172497` |
| D-fabric-2 | a fabric index handed out again inherits what the removal does not cascade | owner decision 37 reconciles at start and when an endpoint joins; a purge when an index is handed out again was not decided — phase 5; what the reconcile does not sweep is N-fabric-2 |
| D-fabric-3 | DoorLock keeps a removed fabric's index in its creator and last-modified fields, as matter.js does, where connectedhomeip clears them | by_design `BD-Matter-DoorLock-CreatorFabricIndexKept`; behaviour unchanged |
| D-fabric-4 | RemoveFabric is not serialised with AddNOC, UpdateNOC and the rollback | fixed `a398a07f` — the store removal takes the lock of the NOC commands (`removeFabricRow`, read at `540cb0eb`); the two interleavings the note names were read by the reviewer, not run, and were not run after the fix either |
| D-fabric-5 | a session established by a handshake in flight across its fabric's removal, shared with matter.js | fixed `d802d9a7`; Ahead `BD-Matter-CASE-RemovedFabricRefusedAtCompletion` |
| D-sess-1 | ADR 0008's table is behind the pin in three places that want one decision | fixed `f9d0ac5f` (the table), `6c4d9b07`, `615d7d37`; owner decision 49 |
| D-sess-2 | which side yields, the idle reaper or the subscription | owner decision 43; fixed `1d265303` |
| D-sess-3 | one per-session purge in place of three partial copies | fixed `a792ed5a` |
| D-sess-4 | `secure/operational.Manager.ClosePeer` has no caller | fixed `edfbe40c` (deprecated) |
| D-sess-5 | bare `path:line` citations rot; a pin per citation or a guard that resolves symbols | phase 4: owner decision 13 |
| D-ctr-1 | a version for every cluster instance, its absence not encoded in the value | owner decision 46; fixed `f09f79ac` |
| D-ctr-2 | the controller's exchange table and the device's exchange ids fail together | fixed `85788456`, `e8c26628` |
| D-ctr-3 | the secure session's reception window anchors on the first counter seen | fixed `da8c16ef` |
| D-ctr-4 | the unsecured reception state lives with the commissioning window | fixed `18e138b1` |
| D-ctr-5 | citations on the counter path at three matter.js commits, three of them hiding a drift | the three drifts are R2-P2-ctr-01, -02 and -03, fixed; the citations are phase 4: owner decision 13 |
| D-ctr-6 | DataVersions are random per process start, so a restart primes every controller's cache again; the reviewer calls it the owner's call, not a finding | phase 4: open finding `F-db021da0-17`, which names the decision |
| D-store-1 | the scope and cut of owner decision 15 | owner decision 38; fixed `d17f858d`; outside the cut: N-persist-1 |
| D-store-2 | one persister at the node, keyed by the generated quality flag | owner decision 38 |
| D-store-3 | an atomic multi-key write in the settings port | owner decision 39; fixed `af18e97e` |
| D-store-4 | three error rules coexist for controller-written state; one stated rule would close the class | owner decision 38 sets the rule for the nonvolatile attributes; the sites that still commit first and log a refused store (scenes, a bridged NodeLabel, the OTA requestor's state, the controller's cache, by the note's list — not read again) are phase 5: architecture |
| D-store-5 | no reconcile at start of the fabric state kept in settings against the fabric table | owner decision 37; fixed `620c86f3` |
| D-stop-1 | is a Bridge single-run or restartable | owner decision 40; fixed `91172715` |
| D-stop-2 | the Controller has no one closed gate | owner decision 41; fixed `ce084c74` |
| D-stop-3 | the node's window calls and the transition hook share no stopping gate | fixed `9c42703d` |
| D-alloc-1 | the controller lacks the bounds the device has against hostile input | owner decision 45; fixed `55def8a4`, `6c69d070`, `a6950779` |
| D-alloc-2 | `BD-Matter-Read-RepeatedPathsDropped` rests on a bound that does not hold | owner decision 44; fixed `30292398`; the row rewritten |
| D-alloc-3 | MRP intervals a peer states before it authenticated are taken unclamped | owner decision 45; fixed `55def8a4`, `e261d8c7` |

## Noticed by the fix agents, the citation passes and the coordinator, not fixed where it was noticed

No severity: none of these was reviewed or verified as a finding.

| ID | noticed in | what | disposition | where |
| --- | --- | --- | --- | --- |
| N-store-1 | `phase-2-fix-store.md`, `-persist.md` | `script/docscheck`'s settings-key guard judged `SetSetting` calls only, so a key written through `SetSettings` escaped it | fixed `b66a9867`, `ac9c5fd8` | `script/docscheck/lists_test.go` |
| N-store-2 | `phase-2-fix-store.md`; the coordinator's list | a pending update's prior private key is stored as the key itself is, unsealed, and its erasure at commit or rollback leaves it in the database's free pages and write-ahead log, as every overwrite does | phase 5: sealing at rest (owner decision 10) | `store/fabric.go` |
| N-noc-1 | `phase-2-fix-noc.md`, `-fabric.md`, `-persist.md` | `golangci-lint` not clean: 17 `rangeValCopy` reports over `store.FabricRecord`, 160 bytes since schema v8 | fixed `7a6feeb0`, `afb5aaac` | `node`, `cluster/core`, `groups`, `cluster/jointfabric` |
| N-noc-2 | `phase-2-fix-noc.md` | SetVIDVerificationStatement writes the vendor id and the VID data in two store writes; the port has no one call for both | phase 5: the store port, with the API bundle | `cluster/core/vid_verification.go` (`:99`, `:104` at `540cb0eb`) |
| N-noc-3 | `phase-2-fix-noc.md` | a failed AddNOC whose rollback failed was never announced, yet its later rollback fired the host's `OnFabricRemoved` | fixed `991febc6` | `node/node.go` |
| N-noc-4 | `phase-2-fix-rw3noc.md` | a host's `SetBlocked` while a window is open still revokes as RevokeCommissioning does: the PASE sessions close and a fail-safe armed over PASE expires with its rollback | struck: kept by the coordinator — the host's own act | `bridge/commissioning_window.go` |
| N-noc-5 | `phase-2-fix-rw3noc.md` | the three failure paths of `OpenWindow` revoke right after its Busy check; no lock excludes a PASE auto-arm between the check and the revoke (inferred) | phase 6 | `bridge/commissioning_window.go` |
| N-noc-6 | — | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-noc-7 | `phase-2-fix-rw3noc.md` | for an UpdateNOC rolled back at start only the store, client and host parts of the fabric-updated path run — no identity reload, no announce | struck: accepted by the coordinator — the bridge has not started then | `node/node.go` |
| N-noc-8 | `phase-2-fix-rw3noc.md` | a comment of the wiring test says the PASE auto-arm clears nothing by itself | fixed `ae8e262f` | `examples/reference-bridge/wiring_test.go` |
| N-fabric-1 | `phase-2-fix-fabric.md` | before the removal contract, a refused save during a fabric's removal still removed the TLS client keys and published while the tables were restored | fixed `620c86f3` — read at `540cb0eb`: the drop returns on a refused save before it removes a key or publishes; not run | `cluster/tls/certificates.go` |
| N-fabric-2 | `phase-2-fix-fabric.md` | the reconcile does not sweep the Thermostat's atomic-write state nor the stored server subscriptions of a fabric that is gone | phase 6 | `node/fabric_state.go` |
| N-ctl-1 | — | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-ctl-2 | `phase-2-fix-ctl.md` | `topology.Changes` rebuilt on a decommission only | fixed `fd556658` | `controller/topology/service.go` |
| N-ctl-3 | `phase-2-fix-ctl.md` | an OTA download of indefinite length over BDX had no size cap | fixed `54e8fb24`; Ahead `BD-Matter-OTARequestor-BDXSizeCap` | `cluster/core/ota_requestor_query.go` |
| N-ctr-1 | — | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-ctr-2 | `phase-2-fix-ctr.md` | two comments still described the constant 1 (`cluster/measurement/measurement.go`, `contract.ClusterDataVersion` "MUST return ≥ 1") | fixed `67b562e4` | — |
| N-ctr-3 | `phase-2-fix-ctr.md`; the third citation pass | `contract.DataVersionTracker` never holds 0: it starts non-zero, `Bump` skips 0 with a comment that names the specification (not read), and `Current` uses 0 for "not installed"; matter.js starts at a random uint32 and wraps to 0 (`packages/node/src/behavior/state/managed/Datasource.ts:597`, `:695-698` at `db021da0`, re-read) | phase 4: open finding `F-db021da0-17` | `contract/dataversion.go` |
| N-flake-1 | the coordinator (a test of this branch that failed once under load) | `BasicInformation.SetSoftwareVersion` advances the DataVersion and reports nothing to a subscription that is already running; its one caller at run time (`node/ota.go`) restarts the node afterwards. Whether matter.js would report such a change is unverified. The three Erase tests that relied on a priming report carrying the change now change NodeLabel (`10818538`) | phase 4: parity, low | `cluster/core/basic_information.go` |
| N-ctr-4 | `phase-2-fix-ctr.md` | ADR 0001 records the old default of 1 | struck: an ADR, left as history | `docs/adr/0001-matter-commissioning-bring-up.md` |
| N-ctr-5 | the coordinator's list | `bridgetest.SubscriptionTarget.PeerCounter` and `ErrNoPeerCounter` are kept; whether anything still needs them | phase 5: the API bundle | `bridge/bridgetest` |
| N-ctr-6 | the coordinator's list | a parity case of the controller's default subscription failed once every cluster had a version: its priming report came back empty | fixed `d23227f4` | `controller/client` |
| N-ctr-7 | the coordinator's list | the CHANGELOG's Deprecated section has to name `transport/mrp.NewWindowNoRollover`, `im.EventLog.SetCounterPersistence` and `secure/operational.Manager.ClosePeer` | fixed `24c2c6c5`, `c9e41825` — read at `540cb0eb`: all three are named there and in the README's table | `CHANGELOG.md`, `README.md` |
| N-life-1 | `phase-2-fix-life.md` | a camera service that outlives its controller's Close dereferences the cleared registry | fixed `618463ee` | `controller/camera` |
| N-life-2 | `phase-2-fix-life.md`, `-rework1.md` | two concurrent `Controller.Erase` calls race on the stopping registry; an Erase whose own Close could not wait erased under a Close still running | fixed `688abf99`, `ea5e36b9` | `controller/erase.go` |
| N-life-3 | `phase-2-fix-life.md` | a window open at Stop still reads open afterwards (`Node.Window().Open`) — state only, no timer | phase 6 | `bridge/commissioning_window.go` |
| N-life-4 | the coordinator's list | `DiagnosticEvents` returns nil after Close | struck: kept under owner decision 41 | `controller` |
| N-life-5 | the coordinator's list | `FactoryReset`'s wait for the lifecycle slot and the camera `Close`'s wait have no bound | phase 5 | `node/factory_reset.go`, `controller/camera` |
| N-persist-1 | `phase-2-fix-persist.md` | module-held nonvolatile attributes outside the cut, which have no setter and are not stored: the root's `BasicInformation.LocalConfigDisabled`, and `Location` and `LocalConfigDisabled` of the assembler's BridgedDeviceBasicInformation | phase 6 | `cluster/core/basic_information.go`, `endpoint` |
| N-persist-2 | `phase-2-fix-persist.md`; the coordinator's list | `NetworkCommissioning.InterfaceEnabled` had no setter, and the stored form was the written value where a server commits another (WindowCovering's Mode) | fixed `09360722`, `b6ac305a` | `cluster/core`, `node/nonvolatile.go` |
| N-read-1 | `phase-2-fix-read.md`; the first citation pass | the priming report is sorted ascending, a rule of this module that its comment presented as matter.js's; since the chunk-by-chunk generation a report of several paths sorts each path's results and keeps the paths in the request's order. The fix note asks the owner about the multi-path order; it was not asked | open finding `F-db021da0-16` (phase 4) | `bridge/subscribe_dispatch.go`, `im/read_expansion.go` |
| N-read-2 | `phase-2-fix-read.md` | two copies of the per-chunk arm, send and wait loop, with different timeout and abort handling | phase 5: architecture | `bridge/subscribe_dispatch.go`, `bridge/subscription_reestablish.go` |
| N-read-3 | `phase-2-fix-read.md` | a DataVersionFilter that names another node was applied | fixed `f09f79ac` | `im` |
| N-sess-1 | `phase-2-fix-sess.md` | the SQL store ordered server subscriptions by a one-second timestamp, then by id | fixed `8d79e692` | `store/server_subscriptions.go` |
| N-sess-2 | `phase-2-fix-sess.md` | the two CloseSession paths still drop the timed deadlines themselves, redundant with the purge | phase 6 | `bridge/securechannel.go` |
| N-sess-3 | `phase-2-fix-sess.md` | stale matter.js line numbers in the comments of `evictSession` and `handleTCPClose` | phase 4: still there at `540cb0eb` (`:571`, `:619-620`); owner decision 13 | `bridge/conn.go` |
| N-shut-1 | `phase-2-fix-shut.md`; the second citation pass | two comments cited matter.js's shutdown at lines that hold something else, and Stop's comment claimed a graceful close matter.js's shutdown does not make | fixed `67c7e99b`, `5c938296` | `bridge/bridge.go`, `secure/operational/manager.go` |
| N-shut-2 | `phase-2-fix-shut.md` | matter.js's factory reset goes offline first (`packages/node/src/node/ServerNode.ts:165-173` at `db021da0`, re-read), where `node.FactoryReset` removes every fabric first, with a CloseSession per session and the Leave event flushed | phase 4: a parity difference to classify | `node/factory_reset.go` |
| N-sweep-1 | `phase-2-fix-sweep.md` | the wiring test's AddNOC check no longer isolates what CommissioningComplete clears: the auto-arm's starting hook clears the pending state too | phase 5: test quality | `examples/reference-bridge/wiring_test.go` |
| N-sweep-2 | `phase-2-fix-sweep.md` | `Aggregator.Forget` kept the forgotten keys' stored values, so a later Add of the key was restored from them | fixed `9d46d53e` | `node/nonvolatile.go`, `node/reserve.go` |
| N-sweep-3 | `phase-2-fix-sweep.md` | a CHANGELOG line still listed the vendor id and the VID data among what `UpdateFabricCredentials` writes | fixed `a40b27d2` | `CHANGELOG.md` |
| N-rw1-1 | `phase-2-fix-rework1.md` | a re-offer answered between the closed check and the start of Close counts as answered before Close; the camera's session is not ended | phase 6 | `controller/camera/provider_sessions.go` |
| N-rw1-2 | `phase-2-fix-rework1.md` | the first chunk of a pushed report was not counted against the report bound | fixed `b0c95399` | `client/interaction/report.go` |
| N-rw2-1 | `phase-2-fix-rework2.md` | the ported decommission case drives its failure through a network filter where matter.js's case patches `removeFabric`, so its second attempt depends on session state the original does not touch | phase 5: test quality | `controller/client/decommission_parity_matterjs_test.go` |
| N-rw2-2 | `phase-2-fix-rework2.md` | one `-race` run of `./client/interaction` printed FAIL with nothing captured; three reruns passed | phase 5: test quality — unexplained | `client/interaction` |
| N-rw2-3 | `phase-2-fix-rework2.md` | no test drives a full CASE re-establishment after a rollover close | phase 5: test quality | `client/exchange` |
| N-rw2-4 | `phase-2-fix-rework2.md` | `script/docscheck` failed at `114dee6d`: the one start-up read of the old pending-NOC keys cleared them through `SetSettings` with a key the call did not state | fixed `ac9c5fd8` | `script/docscheck/lists_test.go` |
| N-rw3ctl-1 | `phase-2-fix-rw3ctl.md` | `SetGroupKey` holds the closed gate across store writes and `groups.Reload`; not traced to a wait on a goroutine that takes the gate | phase 6 | `controller/groups.go` |
| N-rw3ctl-2 | the review of the rework (`rr-ctl`); the coordinator's list | `BD-Matter-Controller-UnsecuredSessionOneHandshake` says connectedhomeip was not read. It was, after the row: its CASE initiator takes a fresh random ephemeral node id for every session (`src/app/CASEClient.cpp:40-41`, `src/transport/SessionManager.h:495-504` at `6170af84`, re-read), so its retry opens another session and does not meet the bound. matter.js's initiator was not read | phase 4: the row | `notes/parity/by_design.md` |
| N-rw3req-1 | `phase-2-fix-rw3req.md`; the coordinator's list | while a command runs, a host's change on the same cluster is held and reported when the command ends — accepted by the coordinator; no `by_design` row, and matter.js was not read for it | phase 4 | `bridge/receive_dispatch.go` |
| N-rw3req-2 | `phase-2-fix-rw3req.md` | a re-established subscription's priming report no longer answers a concrete path that names another node with UnsupportedNode: the kept paths leave it out | phase 6 | `bridge/subscribe_dispatch.go`, `im/subscription/persistence.go` |
| N-rw3req-3 | — | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-rw3req-4 | `phase-2-fix-rw3req.md` | a citation for "one advance per commit" named the lines before it, and `wireRootNotifiers`' doc said nothing is bumped there | fixed `4464d1c5`, `ae8e262f`, and the third citation pass | `bridge/subscribe.go`, `node/dataversion_write_test.go` |
| N-rw3persist-1 | `phase-2-fix-rw3persist.md` | a Device that leaves the topology stays referenced by the persister until Stop unless its key is taken again; `Aggregator.Forget` could release it | fixed `bebaed81` — a forgotten Device is released; a removed one is kept, so that a Device added again keeps its state (`12d01be8`) | `node/nonvolatile.go` |
| N-finctl-1 | `phase-2-fix-finctl.md`; the coordinator's list | the device's rollover close flushes, sends its CloseSession and closes at once; the controller now waits for its exchanges in flight, as matter.js does (`packages/protocol/src/session/NodeSession.ts:367-391` at `db021da0`, re-read). Whether the device drops a reliable reply that still waits for its acknowledgement: unverified | phase 4: a parity finding to open | `bridge/reply.go` |
| N-finctl-2 | `phase-2-fix-finctl.md` | the controller's `Manager.CloseSession` closes at once with exchanges active — `Peer.Close` and the node's fabric close abort exchanges in flight where matter.js lets them end | phase 4 | `client/exchange/manager.go` |
| N-finctl-3 | `phase-2-fix-finctl.md` | the commissioner's BLE channel-loss watcher stops on `Closing()`, which now fires at a rollover before the close; reachable only after about 4 × 10⁹ messages on a PASE session | phase 6 | `controller/commissioner/commissioner.go` |
| N-findev-1 | `phase-2-fix-findev.md` | the log text of the refused arm ("its rollback ended the arming session") now also answers an arm refused over a row left pending | phase 6 | `cluster/core/general_commissioning_rollback.go` |
| N-findev-2 | `phase-2-fix-findev.md`; the review of the rework | with the subscription manager detached during a command, the commits end and what was held is dropped without advancing the DataVersion; read, not run | phase 6 | `bridge/receive_dispatch.go` |
| N-findev-3 | `phase-2-fix-findev.md` | the host's `OnFabricUpdated` and `OnFabricRemoved` owed for a row rolled back in `New` are lost when the process ends before `Start` (measured for the update); so is the store's part when it crashes inside `New`. It needs a durable mark | phase 5: the store port | `node/node.go` |
| N-findev-4 | `phase-2-fix-findev.md` | a fresh arm through `ArmFailSafeFor` still starts the Breadcrumb at 0 | phase 6 | `cluster/core/general_commissioning.go` |
| N-finp-1 | `phase-2-fix-finpersist.md`; the coordinator's list | `Forget`'s erasure reads every key the persister could have written — 108 attributes per key at this head, by the fix agent's count — because the settings port lists no keys by prefix; a host-registered cluster not met under the forgotten key in this run keeps its values | phase 5: the settings port (a prefix delete) | `node/nonvolatile.go` |
| N-finp-2 | `phase-2-fix-finpersist.md` | `bridged_node_label.<key>` is not erased by `Forget` | phase 5, with N-finp-1 | `node/persist.go` |
| N-finp-3 | `phase-2-fix-finpersist.md` | whether the unsubscribe of `spec.ChangesOf` waits for a callback that is running is unverified; a host's change at the moment of `Forget` could store again | phase 6 | `node/user_labels.go` |
| N-finp-4 | `phase-2-fix-finpersist.md` | `endpoint.WritePersister`'s doc gives one meaning of a nil done; there are two now | phase 5: docs | `endpoint/dispatcher.go` |
| N-cite-1 | the second citation pass; R2-P2-comm-14's verifier | whether matter.js keeps an uncommitted fabric across a restart: its table write stores every fabric it holds whenever another path writes inside the window; what its start then does with that fabric was not traced | phase 4 | `notes/parity/by_design.md` `BD-Matter-PendingNOCMarker` |
| N-cite-2 | the second citation pass | three comments say connectedhomeip "does the same" at start. Its start deletes the fabric that a commit marker names (`src/credentials/FabricTable.cpp:1129-1141` at `6170af84`, re-read); that the marker is written only inside the commit, so that a fabric whose AddNOC was never committed is not stored at all, is the pass's reading and was not re-read here. The comments do not state the difference in trigger | phase 4 | `cluster/core/operational_credentials.go` |
| N-cite-3 | the third citation pass | `Datasource.ts:1473-1476` is cited on 19 Go lines for "a commit emits `$Changed`" (counted at `540cb0eb`); at the pin those lines are the doc comment and the signature of `#noteIfChanged` (re-read). `bridge/securechannel_close_parity_test.go:308` cites `ServerNetworkRuntime.ts:410-447` for `stop()`, which starts at `:427` (re-read) | phase 4: owner decision 13 | `cluster/`, `contract/`, `bridge/` |
| N-cite-4 | the second citation pass; `phase-2-fix-rework1.md` | `controller/topology/service.go` cites matterjs-server at `0bd06f22`, which is not exported; and the rebuild on `Hooks.Deleted` (R2-P2-fabric-08) is ahead of what that comment cites, which `BD-matterjs-server-NetworkTopology` does not say | phase 4: not performable here | `controller/topology/service.go` |
| N-cite-5 | the second citation pass | stale line numbers on lines outside the wave's diff (`Datasource.ts:1035`, `:410`, the OTA block sizes, a quoted comment of `ServerNetworkRuntime.ts`), and a sentence on the shutdown's CloseSession that predated owner decision 49 | fixed `365266fd`, `2dc3b2f5`, `b19b153e`, `5c938296` (the third pass), `ae8e262f` | `bridge`, `endpoint`, `cluster/core`, `secure/operational` |
| N-reg-1 | this change | `client/exchange/manager.go:984` reads "(ExchangeManager.ts:386-389; :441-444 at db021da0)": `:441-444` declines a new exchange on a closing session at the pin; `:386-389` are fields of a Groupcast test event there — the older range was kept when `14085d3d` added the new one. `client/exchange/handler_test.go:271` carries the older range alone | phase 4: owner decision 13; comments only, not changed here | `client/exchange` |

## Unread

What each reviewer said it did not read or trace, summarised per unit; all
of it is phase 6 (the full pass reads every hand-written package again). The
list is the reviewers', not re-measured here.

| Unit | area | not read | phase |
| --- | --- | --- | --- |
| comm | commissioning and fail-safe | the Thread NetworkCommissioning server and `threadbr` as fail-safe participants; BLE start and withdraw against the window and a rolled-back first commissioning; the enhanced window's verifier install and restore, the window reopen logic; the mDNS record content at each step; `jointfabric`, `cluster/tls` and commissioner control as participants; GroupKeyManagement and Groupcast state after a rollback; a host that wires the bridge without the node facade; `controller/commissioner/network.go` step bodies, `reconnect_probe.go`, `jcm.go`, `fabric.go`, `controller/client/window.go`, `complete.go`; SetVIDVerificationStatement inside a fail-safe against connectedhomeip's pending handling; matter.js's start-up handling of fabric state without a fabric. No `-race` and no chip-tool run; seven findings traced in code, not exercised | 6 |
| fabric | the fabric life cycle | fabric-filtered reads, writes and events on the request path; CommissioningComplete's commit, taken as phase 1 left it; `groups.Manager` on ForgetFabric and the group servers beyond the entry; the bodies of the ICD, TimeSynchronization, TLS and JointFabric removals; the OTA requestor's state of a removed fabric; whether a composed device's parts are among the endpoints the Scenes removal walks; on the controller: the Check-In table for a forgotten node, software update, diagnostics and topology state after Forget, a rotation of the controller's own fabric; `memstore.Erase`; the two interleavings of its fourth design note, read, not run; the matterjs-server citations (not exported); the specification on DoorLock's creator index | 6 |
| sess | sessions and subscriptions | CASE and PASE establishment on the device beyond the dispatch switch (`bridge/securechannel.go`, the providers, the Sigma1 dedupe); CASE resumption; the device as CASE initiator; `secure/channel`; `transport/mrp` and the ordered dispatch of `transport/udp`; `bridge/reply.go` beyond its call sites; the event side of reports; the read's chunk loop and the write path; ICD; OTA and BDX exchanges on a lost session; on the controller: `client/peer`, the MRP of `client/exchange`, `client/handshake/server.go`, `controller/client`; BLE and TCP session routes; the citations at `862ad75f` (not exported). No `-race` run, no end-to-end run of the reaper; four findings read, not exercised | 6 |
| ctr | counters, replay, rollover | privacy header obfuscation of group messages beyond the nonce inputs; timed-interaction deadlines and the report-counter map across a reused session id; whether every root server with its own tracker bumps before each notify (a grep count per file, not a trace); the controller's `PeerTable.ProcessCheckIn` against matter.js line by line; matter.js's event store numbering and connectedhomeip's event counter epoch; connectedhomeip's group peer table citations; the citations at `0ceb202d` (not exported); in-memory counters across a Stop without a process exit; the store layer's atomicity of the counter writes. Four findings traced, not reproduced. More unread parts of this unit are a held-back row (U-ctr-1) | 6 |
| store | persistence and crash consistency | the controller's client cache saved in several jobs under one data version; the OTA requestor's `persist()` under concurrent callers; `groups.Manager.removeEndpoint` on a store error in mid-loop; the scenes tables, DoorLock's save order, the Thermostat's PresetStore, ThreadNetworkDirectory, the TLS tables, the JointFabric datastore, TimeSynchronization's writes; resumption records, and the CASE identity load of a fabric without an identity row; the controller's own fabric and CA persistence and `Controller.Erase`; memstore against SQLite under errors, migrations on a crash; host backends of `store.Port`; commissioning-window state and ICD registrations at start. No citation checked | 6 |
| stop | shutdown and goroutine leaks | the shutdown of the `client/discovery` scanner and browser; `ble/bluez` and `ble/esphome` life cycles; `wifi/wpasupplicant` under a Close racing a join; BDX transfers and OTA downloads on Stop and Close; the reconnect loops of `client/peer` and sustained subscriptions after `Controller.Close`; `controller/client` node Close and the ICD tables; the camera's session-watch sweeps; a report send in flight when the subscription engine stops; `Controller.Erase` racing Close; a reassembly that ends after `Node.Stop`; time synchronization, the ICD manager, the group sender, the health probe; the topology service and DCL's background refresh. No `-race` run for any finding | 6 |
| alloc | wire-driven allocations | certificates and CSRs beyond "bounded by the message size"; group message reception (owner decision 22), not re-read; the controller's camera paths; `thread/brdiscovery` and `thread/otbr` beyond the body cap; the ESPHome BLE proxy's wire and the BlueZ drivers; `jointfabric/datastore_ops`, `endpoint/devicetype_conformance`, `node/config`. No raw-datagram reproduction of R2-P2-alloc-02. More unread parts of this unit are a held-back row (U-alloc-1) | 6 |
| dr-noc | the diff of the fail-safe and the fabric row | `operational_credentials.go`'s attribute reads, attestation and CSRRequest through their diff only; `general_commissioning.go`'s constructor and reads; a dozen files as diff plus the functions around it; the callers of the removal contract; `node/node.go` beyond four functions; most tests by name and diff stat; the lock order between the hooks run under the NOC lock and the node's fan-out reasoned from the hook list, the fan-out's callees in `bridge/` not read; the watcher's context; docs and CHANGELOG of the second wave | 6 |
| dr-req | the request path's diff | the unit's tests but for four; fourteen files as diff plus surroundings; the node-side wiring the unit depends on (the registry's close hook, the persister beyond `PersistWrite` and the restore, the order of `stopParts`); no `-race` and no package-wide run; whether the always-compare gate refuses a legitimate chunked list write — how connectedhomeip's write client stamps the follow-up items was not read, so not performable; whether every CASE session carries a non-zero local node id | 6 |
| dr-ctl | the controller's diff | the camera service beyond the changed hunks; `controller.go` `New`, `openRegistry`, `openTransports`; non-public callbacks that read fields Close clears without the gate (`controller/ota.go`, `ota_provider.go`), looked at, not traced to a failing interleaving; `client/discovery`'s staged-address path, solicitor and `watch.go`; `client/exchange/exchange.go` `Close`, `abort` and the retransmission timer; 13 test files grepped only; `docs/guides/limits.md` and the CHANGELOG lines against the code; R2-P2-dr-ctl-01 traced, not run. More unread parts of this unit are a held-back row (U-dr-ctl-1) | 6 |
| dr-node | the node's and the docs' diff | `script/coverfloor/floors.go`; four docs pages by their diff only (limits, the controller's getting-started, observability, ports); `node/commissioning.go` and `node/root.go` outside the unit; the tests skimmed for wall-clock waits and weakened assertions only; the fabric drop of Messages and ScenesManagement | 6 |
| rr-dev | the rework, device side | three new or changed test files and one changed test; `node/nonvolatile.go` `stop`; of the citation passes' commits only the citations its report lists were read against the pins; `transport/` did not change in the range | 6 |
| rr-ctl | the rework, controller side and persister | four test files of `node` and `controller/client`, the second test of `apply_order_internal_test.go`, `report_bound_config_test.go` beyond its diff; the CHANGELOG and docs changes | 6 |
| U-ctr-1 | unit ctr | — | held back until its fix merges (owner decision 25) — phase 3 |
| U-alloc-1 | unit alloc | — | held back until its fix merges (owner decision 25) — phase 3 |
| U-dr-ctl-1 | unit dr-ctl | — | held back until its fix merges (owner decision 25) — phase 3 |

## The citation passes

Three passes read, in the exported trees of the pins, every citation of
matter.js or connectedhomeip that a wave of the campaign added or changed in
a Go file or a docs page — `a9134e81..edfbe40c`, `edfbe40c..114dee6d`,
`114dee6d..9580db1b` — and the third also the citations the fix agents had
reported as stale or unread. Their corrections are the 19 commits "the
citation says what the lines say": comment text and one docs page, 151 lines
added and 96 removed in 31 files.

| pass | rows | OK | LINES | WRONG | not performable | not in the tree |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 54 | 39 | 11 | 4 | 0 | 0 |
| 2 | 89 | 73 | 6 | 9 | 1 | 0 |
| 3 | 60 | 36 | 23 | 0 | 0 | 1 |
| all | 203 | 148 | 40 | 13 | 1 | 1 |

LINES: the claim holds at the pin, at other lines than it named. WRONG: the
lines do not say it. The counts are the passes' own tables, added up by
script; the verdicts are theirs.

- One WRONG claim had been the reason given for the code's behaviour: the
  ascending order of the priming report, presented as matter.js's. The
  comment now says what the pin shows (`61319601`); the order is open
  finding `F-db021da0-16` (N-read-1).
- The other WRONG rows are sentences corrected in place, with no code
  resting on them: a matter.js function named at three sites that does not
  exist at the pin; the shutdown described as a graceful close (the
  CloseSession is owner decision 49); "both references come back from such a
  restart without the uncommitted fabric" (N-cite-1); two connectedhomeip
  names for CommissioningComplete's success path that do not exist at the
  pin; three statements that "neither reference bounds" what the controller
  now bounds, at six sites — by the pass's reading connectedhomeip holds a
  pool of four unauthenticated sessions and reads a DNS-SD interval above
  one hour as not stated; and a comment that still called an Ahead row a
  candidate.
- NOT PERFORMABLE: a citation of matterjs-server, which is not exported
  (N-cite-4).
- Two things the third pass turned up that are not line corrections: the
  DataVersion tracker's zero (N-ctr-3), and that the one handshake per
  inbound unsecured session is this module's bound, not matter.js's — now
  the Ahead row `BD-Matter-Controller-UnsecuredSessionOneHandshake`
  (`91a95f08`).
- After the third pass's range two commits added citations: `14085d3d` (13
  comment lines) and `41c9c961` (1). Re-read by the register editor:
  `NodeSession.ts:367-376`, `:384-391`, `:388`, `:419-424`,
  `Session.ts:190-191`, `:304-320`, `:311`, `ExchangeManager.ts:441-444`,
  `Peer.ts:572-573`, `:587-590` and `GeneralCommissioningServer.ts:65` say
  what their comments say; one older range on a line `14085d3d` extended
  does not (N-reg-1).
- In no pass's scope: the citations of the rows this phase wrote in
  `notes/parity/` (`script/citecheck` holds a register's citation to a file
  and lines that exist, not to its sentence); and the citations the
  reviewers reported as failing outside the campaign's diff — 7 + 12 + 18 +
  7 + 1 + 1 entries in the units comm, fabric, sess, ctr, dr-noc, dr-ctl;
  how many of them a pass corrected was not counted. Those still stale are
  phase 4 (owner decision 13); N-sess-3 and N-cite-3 are measured instances.

## Counts

Generated from the rows above (`fixed`, `parity`, `struck`, `phase N` and
held back are read off the head of each disposition).

The 105 findings, by their first disposition:

| disposition | the seven flows | the diff review | the review of the rework | all |
| --- | --- | --- | --- | --- |
| fixed | 66 | 22 | 13 | 101 |
| parity with matter.js, no change | 1 | 0 | 0 | 1 |
| struck | 1 | 0 | 0 | 1 |
| open: phase 5 | 1 | 0 | 0 | 1 |
| held back | 0 | 0 | 1 | 1 |
| all | 69 | 22 | 14 | 105 |

By the severity their rows carry: 4 high, 12 medium, 78 low, 11 note. 10
rows name another row as the same defect.

Of the 101 fixed: 24 were corrected by a finding of a later review, 9
brought an Ahead row, 16 a `by_design` row or an amended one, 2 name an open
finding (`F-db021da0-14` closed, `F-db021da0-16` opened), and 1 leaves one
part open in a later phase (R2-P2-ctr-03).

What phases 0 and 1 handed over: 21 rows in the flows' lists — 19 fixed, 1
struck, 1 open (phase 5) — and 4 rows held back in phase 1 whose fix is on
this branch.

The 31 design notes: 25 settled by a decision, a fix or a register row (20
of the 31 name an owner decision), 5 with a part placed in a later phase, 1
placed in phase 4 as a whole (D-ctr-6, `F-db021da0-17`).

The 71 noticed items: 23 fixed, 2 open findings, 4 struck, 12 phase 4, 13
phase 5, 13 phase 6, 4 held back.

Unread: 13 units, phase 6, and 3 held-back rows.

Rows written in `by_design.md` (`git diff a9134e81..540cb0eb`): 13 Ahead
rows with their entries, 7 `by_design` entries, 6 entries or rows amended;
after `540cb0eb` one more Ahead row (`BD-Matter-UnsecuredWindowExchangesBounded`)
and `BD-Matter-SessionRolloverClose` amended again. Findings: 1 closed
(`F-db021da0-14`), 2 opened (`F-db021da0-16`, `-17`).

**Held back: 8 rows** — 1 finding (R2-P2-rr-dev-08), 4 noticed items and 3
unread parts; all in phase 3. Of the 12 rows phase 1 held back, 5 are public
with this branch (N-core2-1 and the 4 rows of the second table of "What
phases 0 and 1 handed to phase 2"); the other 7 stay held back.

## Not placed, not verified

- The 115 bare refusal assertions on `script/refusalcheck`'s ratchet
  (W6-test-17, placed in phase 2 by phase 0) were not taken up in phase 2;
  they move to phase 5 (test quality), the count unchanged.
- The mutation sampling (phase 0's mechanical evidence) ran on `940c99b7`
  for `cluster/core`'s ACL and OpCreds files, `commissioning`, `im` and
  `endpoint` — the log's "SURVIVED SURVIVED" column adds up to 128 in `core`
  (1 block), 20 in `commissioning` (1 block), 420 in `im` (6 blocks), 176 in
  `endpoint` (4 blocks); what the log's labels mean it does not say — and
  still runs for `bridge`; its survivors are phase 5 material.
- Not put to the owner, though a source asks for it: D-ctr-6; D-comm-4 (c)
  and (d); the order of a priming report of several paths (N-read-1); a
  probe after a lost CommissioningComplete answer (R2-P2-comm-13). Chosen by
  a fix agent without a reference line and recorded as the module's own in
  `docs/guides/limits.md` or a register row, not as an owner decision: the
  DNS-SD cache's 8192 records, the DCL listing's 64 MiB and 65536 items, the
  four exchanges of an unsecured window, the end reason a removed fabric's
  camera session ends with. The report bound was put to the owner (decision
  48).
- What the verifiers could not perform (21 entries in the result files), in
  short: no run of the client stack across a fabric's removal
  (R2-P2-fabric-02); no crash injected between the two writes of
  R2-P2-store-01; no `-race` run for R2-P2-stop-01; R2-P2-alloc-02's 561
  days are arithmetic, and no raw datagrams were sent; the per-peer total of
  R2-P2-alloc-04 and the figures of R2-P2-alloc-05 are the reviewers'; no
  end-to-end run of the reaper against a subscription (R2-P2-sess-02); the
  unsecured and BDX variants of R2-P2-ctr-01 are inferred; which values real
  controllers request was not read; the specification's text is not
  available.
- Every statement of a row that is marked unverified, inferred, "not run" or
  "not re-read"; the reference that is not exported at the pins
  (matterjs-server, N-cite-4).
- Phase 0's held-back row "rework-bridge, one item on the unsecured session"
  is what R2-P2-ctr-04 names as known. Whether every part of it is closed by
  `18e138b1` was not compared here; it stays held back with phase 0's text.
- The map from finding to commit came from a script over the commit subjects
  and the fix notes' tables. Two of its attributions were wrong and are not
  in the rows: a commit a fix note names as the state at which its test was
  red (`12585e70`, a notes commit, under six findings), and the two commits
  of R2-P2-dr-req-07 under R2-P2-dr-req-01. One is left out of its row:
  R2-P2-ctr-09's commit `121b589c` also moves a helper of R2-P2-ctr-04's
  fix, by the fix note. 21 of the 154 commits are named by no row: 1
  CHANGELOG commit, 3 notes commits, 1 docs commit, 14 of the 19
  citation-pass commits, and two code commits — `8a88e8a9`, which put the
  deprecation of `bridge.FailSafeArmer` off until the CHANGELOG announced it
  (`d4d65978` then made it), and `7e23102c`, the text of one refusal in
  owner decision 36's store change. They were placed by their subjects, not
  read one by one.
- Whether the sibling checkouts hold what the exported pins hold was not
  compared; the exported trees carry no `.git`, so that they are the pins is
  taken from how they were made. `script/citecheck`'s drift artefacts did
  not move on this branch (`git log a9134e81..540cb0eb -- script/citecheck`
  is empty).