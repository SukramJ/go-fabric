# Phase 1 — the dispositions

The commit ids below are those of the phase 1 pull request's branch
(`review2/p1`, the code at `c6f8325f`, then this change); if the pull
request merges squashed, as phase 0's did, they stay retrievable through
the pull request's reference. Items that are still open after this branch
and describe a weakness a party on the network could use are held back
with their text until the pull request that fixes them (owner decision
25); the table carries their id, severity and phase only, and "Counts"
says how many.

One row per finding of the three review runs of phase 1 — the first pass
(ten units, 64 findings), the second pass over the request-path files the
first left unread (`node`, `req2`; 21) and the diff review of the fix
campaign (`dr-req`, `dr-sec`, `dr-rest`; 22) — one per item the fix agents
noticed and did not fix, and one per unit for what its reviewer did not
read. Sources: the three result files of the review workflow, the thirteen
fix notes (`phase-1-fix-*.md`), `decisions.md` (owner decisions 28–33) and
`git log e1f18a2e..HEAD`. The fix notes are not part of this change; one
that mentions an item held back follows with the pull request that fixes
that item (owner decision 25).

How a row was placed — by rule:

- **fixed `<commit>`** — the commit is in `e1f18a2e..HEAD` and its subject
  names the ID (or the item). "corrected by" names the finding of the diff
  review that found the fix wanting and the commit that repaired it; the
  row gives the state at HEAD. A register row the fix brought is named
  after it.
- **by_design `<row>`**, **Ahead `<row>`** — the row is in
  `notes/parity/by_design.md` at HEAD; **open finding `<id>`** — the entry
  is in `notes/parity/matter_behaviour_findings.md` at HEAD.
- **struck: `<reason>`** — the reason is the fix note's.
- **phase N: `<what>`** — placed by the plan's cut (`README.md`: 2 flows,
  3 adversarial, 4 parity, 5 architecture, tests, docs and the API bundle,
  6 the full pass). The placement is the register editor's; the
  coordinator may move it.

Severity is the verifier's where a finding was verified — every high and
medium, R2-P1-req2-01 and -02 separately — with the reviewer's in
brackets where the verifier narrowed it; a low or note carries the
reviewer's severity and was not verified before its fix.

## The findings

### First pass

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P1-cam-a-01 | low (medium) | fixed `d2912a8c` | `controller/camera` — the key check and the registration are one step; with R2-P1-cam-b-03 |
| R2-P1-cam-a-02 | low | fixed `ae71d1b1` | `controller/camera`; by_design `BD-matterjs-server-CameraStreamManagement` (amended). Phase 4: what matterjs-server does with a removed node's leases — not exported at the pins |
| R2-P1-cam-a-03 | low | fixed `8260b3fa` | `controller/camera/policy_stream.go` |
| R2-P1-cam-b-01 | medium | fixed `04ba5f0d` | `controller/camera/session.go` |
| R2-P1-cam-b-02 | medium | fixed `6b2b5b0b` | `controller/camera` — ends and vanished callbacks act by identity |
| R2-P1-cam-b-03 | low | fixed `d2912a8c` | the same defect as R2-P1-cam-a-01 |
| R2-P1-cam-b-04 | low | fixed `7bd692be`, `486bc332` | `controller/camera`; by_design `BD-matterjs-server-CameraStreamManagement` (amended) |
| R2-P1-cam-b-05 | low | fixed `481a58a3` | `controller/camera/reports.go` |
| R2-P1-cam-b-06 | low | fixed `a6db875c`, corrected by R2-P1-dr-rest-05 `defcabbf`; by_design `BD-matterjs-server-CameraStreamManagement` (amended) | the bound is this module's own; matterjs-server not exported at the pins |
| R2-P1-net-01 | high | fixed `8c19b9d4`; Ahead `BD-Matter-MdnsTruncatedQueryBounds` | `mdns/answer.go`; `docs/guides/limits.md` |
| R2-P1-net-02 | medium | fixed `df13e913`, corrected by R2-P1-dr-rest-07 `d46dc9c2`; by_design `BD-Matter-MdnsOnLinkSource` | `mdns/answer.go` `onLink`; two residuals the fix left open are a held-back row below (N-net-1) |
| R2-P1-net-03 | low | by_design `BD-Matter-MdnsLegacyQuerierNoSpecialCase` — owner decision 32 | parity with matter.js; connectedhomeip answers such a query by unicast; no code change |
| R2-P1-net-04 | low | fixed `87ca4e28`, `9ef620ff` | `client/interaction/handler.go` as matter.js's `onNewExchange`; `controller` Close order |
| R2-P1-net-05 | low | fixed `728556d1`; by_design `BD-Matter-PeerBusyDeadline` (amended) | `client/peer` |
| R2-P1-misc-01 | medium | fixed `d291d22a`, `587175d9`, corrected by R2-P1-dr-rest-02 `d2f9dab4`; Ahead `BD-Matter-StoreEraseScrub` | `store/erase.go`, `node/factory_reset.go` |
| R2-P1-misc-02 | low | fixed `6146dbc4` (the table-list guard) | `store`. Phase 5: the `storetest` erase case over every table, and `memstore.Erase`'s hand-kept field list (N-clumisc-2) |
| R2-P1-misc-03 | low | fixed `0f22e384`, corrected by R2-P1-dr-rest-06 `b2cd2920` | `device/lifecycle.go` |
| R2-P1-misc-04 | low | fixed `5b72346d` | `examples/reference-bridge/camera.go` |
| R2-P1-misc-05 | low | fixed `20207c02` | `script/coverfloor`. Phase 5: "a floor of 0 only for a package that cannot be measured" needs a field in `floors.go` |
| R2-P1-misc-06 | low | fixed `2db5bfcd` | `examples/reference-bridge/fifo_unix.go` |
| R2-P1-misc-07 | low | fixed `118f2e85` (the contract sentence) | `bdx`; the hooks stay on the receive goroutine |
| R2-P1-clu-01 | medium | fixed `72817a25` | `cluster/closure/closurecontrol_server.go` |
| R2-P1-clu-02 | medium | fixed `d2b27cdd`, corrected by R2-P1-dr-rest-03 `7b23f86d` | `cluster/thermo`, `cluster/energy`. Phase 5: the guard "a server with a timer is a `ClusterQuiescer`"; phase 6: the other timer users (N-clumisc-4) |
| R2-P1-clu-03 | low | fixed `a044341f`, corrected by R2-P1-dr-rest-01 `c53b3030` | `cluster/sensing/occupancy.go` |
| R2-P1-clu-04 | low | fixed `734b7444`, corrected by R2-P1-dr-rest-01 `c53b3030` | `cluster/sensing/occupancy.go` |
| R2-P1-clu-05 | low | fixed `49351bf9`, corrected by R2-P1-dr-rest-04 `04e462b7` | `cluster/jointfabric/datastore_ops.go`; by_design `BD-chip-JointFabricDatastore-RulesInServer` (amended) |
| R2-P1-clu-06 | low | fixed `4a43de34` | `cluster/closure/closurecontrol_server.go` |
| R2-P1-ctl-01 | medium | fixed `023c4557`; Ahead `BD-Matter-ReservedLocalIDs` | `controller/client` |
| R2-P1-ctl-02 | medium | fixed `73f093b9` | `controller/erase.go` |
| R2-P1-ctl-03 | low | fixed `02ba0265`, `896816cc` | `controller`; the first round refuted the half about a failed Forget, the second fixed what it had rested on (`Node.Erase` returned no store error) |
| R2-P1-ctl-04 | low | fixed `b064e348` | `controller/client/cache.go`, as matter.js's `#applyUpdates` |
| R2-P1-ctl-05 | low | fixed `c9e8c4eb` | `controller`, `client.Hooks.Deleted`. Phase 2: the camera service still forgets a node only on `Decommissioned` (N-ctl2-1) |
| R2-P1-ctl-06 | low | fixed `b20b1c5f` | `controller/groups.go` |
| R2-P1-drv-01 | low (medium) | fixed `3ee49773` | `ble/esphome` |
| R2-P1-drv-02 | medium | fixed `2708b64f` | `wifi/wpasupplicant` |
| R2-P1-drv-03 | low (medium) | fixed `9797dfed` | `ble/bluez`; bluetoothd's side stays unverified (not in the pinned sources). Phase 6: `StopAdvertising` unexports when the unregister failed (N-secdrv-4) |
| R2-P1-drv-04 | low | fixed `db9e6b27` (the close) | `ble/bluez`. Phase 6: telling an empty C1 packet from the peer's shutdown — the trigger is unverified |
| R2-P1-drv-05 | low | fixed `b74f20a9` | `ble/bluez` |
| R2-P1-drv-06 | low | fixed `e1ad37fc`, `447459f9` | `wifi/wpasupplicant`; the reviewer's fix was refuted, the run loop no longer waits on a join |
| R2-P1-drv-07 | note | fixed `9797dfed`, `87c6dd0a` | `ble/bluez`, both roles |
| R2-P1-req-01 | high | fixed `8c0fa135`; by_design `BD-chip-SessionRouteByNewCounter` | `bridge/receive.go`; the citation that had justified the old binding: `12770f10`, `5b12fb30` |
| R2-P1-req-02 | high | fixed `98591308`, corrected by R2-P1-dr-req-08 `01e70588` | `transport/tcp`. Phase 5: the 32-bit CI leg builds and vets only |
| R2-P1-req-03 | low | fixed `2ed3720a`, corrected by R2-P1-dr-req-02 `b6a201f7`; Ahead `BD-chip-ChunkedWritesBounded` — owner decision 30 | `bridge/exchange_routing.go`; matter.js's order of hold and ACL check kept |
| R2-P1-req-04 | low | fixed `b2797c58` | `im/group.go` |
| R2-P1-req-05 | low | fixed `9766e4e6`, corrected by R2-P1-dr-req-08 `01e70588` | `bridge/ble.go` |
| R2-P1-req-06 | low | fixed `3fc93d1c` | `bridge/ble.go`. Phase 5: `StartBLE` still returns nil for a service the platform refuses — the `BLEPeripheral` contract |
| R2-P1-req-07 | note | fixed `1ee72fe2`, corrected by R2-P1-dr-req-03 `c59c7f9e`; Ahead `BD-Matter-BTP-HandshakeTimerFromAccept` — owner decision 31 | `transport/btp/accept.go` |
| R2-P1-core-01 | medium | fixed `b2175afa`, `d9755d79`, `5816f243`, corrected by R2-P1-dr-sec-01 `8bfe279b`, -02 `8e421ae4`, -04 `462fe649`; by_design `BD-Matter-PendingNOCMarker` | `cluster/core` |
| R2-P1-core-02 | low (medium) | fixed `367e81e3` | `cluster/core/general_commissioning.go` |
| R2-P1-core-03 | low | fixed `04117a74` | `cluster/core`; its test by R2-P1-dr-sec-05 `3a0315e4` |
| R2-P1-core-04 | low | fixed `994361a7` | `cluster/core/general_commissioning_tc.go` |
| R2-P1-core-05 | low | fixed `448c0a17` | `cluster/core/general_commissioning.go` |
| R2-P1-core-06 | low | fixed `3b77e4e8`; open finding `F-db021da0-10` | `cluster/core/network_commissioning_wifi.go`; what the late command answers is the finding |
| R2-P1-core-07 | low | fixed `c86a4a3e` | `cluster/core`; its test by R2-P1-dr-sec-05 `3a0315e4` |
| R2-P1-core-08 | low | fixed `453f2825` | `cluster/core/identify.go` |
| R2-P1-core-09 | low | fixed `be34f648` | `cluster/core/general_diagnostics.go` |
| R2-P1-core-10 | low | struck: refuted for the tests it names — every notification of the fault tests runs on the test goroutine before `settle` | `phase-1-fix-core.md` |
| R2-P1-core-11 | low | fixed `7b819233` (the log); open finding `F-db021da0-11` | `cluster/core/operational_credentials.go`; the wire answer is the finding |
| R2-P1-core-12 | note | fixed `cc29df65` | `cluster/core/operational_credentials.go` |
| R2-P1-sec-01 | medium | fixed `063c072c` | `secure/mattercert/der.go` |
| R2-P1-sec-02 | low | fixed `019f5ff9` (the comment), `0a1d4e6d` — owner decision 33 | `controller/attestation`, `secure/attestation`; rows `BD-Matter-AttestationDERAsReceived` and `BD-Matter-Attestation-ProfileOnCryptoX509` (amended). Phase 5: two copies of one rule |
| R2-P1-sec-03 | low | fixed `533f7427` | `secure/mattercert/der.go` |
| R2-P1-sec-04 | low | fixed `0b01d7e8`, `55ea85d8` | `secure/mattercert` |
| R2-P1-sec-05 | low | fixed `2a78b68f` | `controller/ble/channel.go` |

### Second pass

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P1-node-01 | low (medium) | fixed `eb90cf95` | `node/node.go` |
| R2-P1-node-02 | medium | fixed `db25f90e` | `node/factory_reset.go` |
| R2-P1-node-03 | low (medium) | fixed `41b0b315` | `node/aggregator.go`. Phase 2: the same shape at `node/surface_stability.go` (N-node2-2) |
| R2-P1-node-04 | low | fixed `afb19810`, corrected by R2-P1-dr-req-04 `a6a28806` | `node/node.go` |
| R2-P1-node-05 | low | fixed `afb19810`, corrected by R2-P1-dr-req-04 `a6a28806` | `node/node.go` |
| R2-P1-node-06 | low | fixed `e1f34cbe` | `node/root.go` `checkProduct` |
| R2-P1-node-07 | low | fixed `df19778f` | `node/session_events.go` |
| R2-P1-node-08 | low | fixed `2e971493`, `c6f8325f` | `node/new_goroutines_test.go` |
| R2-P1-req2-01 | high | fixed `1dc38afd`, `a41fbc87`, corrected by R2-P1-dr-req-07 `6ccf0978` and -10 `e95523e1`; Ahead `BD-Matter-ChunkWaitsBounded` | `bridge/chunk_waits.go`, `im/subscription` |
| R2-P1-req2-02 | medium | fixed `6e9c03f7` | `bridge/subscribe_dispatch.go` |
| R2-P1-req2-03 | low | fixed `6ed6a65c`, corrected by R2-P1-dr-req-06 `9ce8beed` | `im/subscription`, `bridge` |
| R2-P1-req2-04 | low | fixed `bb0d2cbb` | `bridge`; its citation corrected in `5b12fb30` |
| R2-P1-req2-05 | low | fixed `c498ddcc`, corrected by R2-P1-dr-req-01 `d243c193` | `bridge/bridge.go` |
| R2-P1-req2-06 | low | fixed `c498ddcc`, corrected by R2-P1-dr-req-05 `1bb81bab` | `bridge/bridge.go` |
| R2-P1-req2-07 | low | fixed `d2282868` | `endpoint/devicetype_conformance.go` |
| R2-P1-req2-08 | low | fixed `d2c26980` | `endpoint/types.go` |
| R2-P1-req2-09 | low | fixed `ee7bfd24` | `im` client decoders |
| R2-P1-req2-10 | low | fixed `b7622f31` | `bridge/subscribe.go` |
| R2-P1-req2-11 | low | held back until its fix merges (owner decision 25) — phase 3 | — |
| R2-P1-req2-12 | low | fixed `e931c2d7`; open finding `F-db021da0-13` | `bridge/subscribe.go`; the peer's interval is the finding |
| R2-P1-req2-13 | low | fixed `0c660d53` | `bridge/bridge.go` |

### The diff review of the fix campaign

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P1-dr-req-01 | medium | fixed `d243c193` | `bridge`, `node` |
| R2-P1-dr-req-02 | low (medium) | fixed `b6a201f7` | `bridge/exchange_routing.go`; row `BD-chip-ChunkedWritesBounded` |
| R2-P1-dr-req-03 | low | fixed `c59c7f9e` | `transport/btp/accept.go`; row `BD-Matter-BTP-HandshakeTimerFromAccept`; its comment said as the lines say it in `5b12fb30` |
| R2-P1-dr-req-04 | low | fixed `a6a28806` | `node/node.go` |
| R2-P1-dr-req-05 | low | fixed `1bb81bab` | `bridge/bridge.go` |
| R2-P1-dr-req-06 | low | fixed `9ce8beed` | `im/subscription/persistence.go`, `bridge` |
| R2-P1-dr-req-07 | low | fixed `6ccf0978` | `bridge` |
| R2-P1-dr-req-08 | low | fixed `01e70588` | `bridge/ble.go` |
| R2-P1-dr-req-09 | note | held back until its fix merges (owner decision 25) — phase 3 | — |
| R2-P1-dr-req-10 | note | fixed `e95523e1` (the comment) | `bridge/chunk_waits.go` |
| R2-P1-dr-sec-01 | low (medium) | fixed `8bfe279b` | `cluster/core`; row `BD-Matter-PendingNOCMarker`. Phase 2: whether AddNOC fails before the marker when the fabric list cannot be read (the reviewer's first proposal, left to the coordinator) |
| R2-P1-dr-sec-02 | medium | fixed `8e421ae4` | `cluster/core` |
| R2-P1-dr-sec-03 | low | fixed `709ba999`; by_design `BD-Matter-ArmOverDueWindowEndedSession` | `cluster/core`; the module's own rule, no reference read |
| R2-P1-dr-sec-04 | low | fixed `462fe649`, `7f440444` | `cluster/core` |
| R2-P1-dr-sec-05 | low | fixed `3a0315e4` | `cluster/core` tests |
| R2-P1-dr-rest-01 | medium | fixed `c53b3030` | `cluster/sensing/occupancy.go` |
| R2-P1-dr-rest-02 | medium | fixed `d2f9dab4` | `store`, `node`; row `BD-Matter-StoreEraseScrub` |
| R2-P1-dr-rest-03 | medium | fixed `7b23f86d` | `cluster/energy`, `cluster/thermo`. Phase 6: the same class in other quiescers (N-rest3-1) |
| R2-P1-dr-rest-04 | low | fixed `04e462b7`; by_design `BD-chip-JointFabricDatastore-RulesInServer` (amended) | `cluster/jointfabric`; chip's delegate not exported at the pin |
| R2-P1-dr-rest-05 | low | fixed `defcabbf` | `controller/camera` |
| R2-P1-dr-rest-06 | low | fixed `b2cd2920` | `device/lifecycle.go` |
| R2-P1-dr-rest-07 | low | by_design `BD-Matter-MdnsOnLinkSource` — as matter.js; stated in `d46dc9c2` | `mdns/answer.go`, `docs/guides/operations.md`; no behaviour change |

## Noticed by the fix agents and by this change, not fixed where it was noticed

No severity: none of these was reviewed or verified as a finding.

| ID | noticed in | what | disposition | where |
| --- | --- | --- | --- | --- |
| N-req-1 | `phase-1-fix-req.md` | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-req-2 | `phase-1-fix-req.md` | `StartBLE` returns nil although the GATT service was refused: `Serve` fails on its own goroutine | phase 5: the `BLEPeripheral` contract splits registration from serving (API bundle) | `bridge/ble.go` |
| N-net-1 | `phase-1-fix-net.md` | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-net-2 | `phase-1-fix-net.md` | an interface whose only IPv4 address is a /32 answers no IPv4 query but its own; matter.js's netmask test has the same effect (`Ip.ts:108-130` at `db021da0`) | struck: parity with the pin — by_design `BD-Matter-MdnsOnLinkSource`; whether a deployment has multicast on such an interface is unverified | `mdns/answer.go` |
| N-net-3 | `phase-1-fix-net.md` | the OTA provider is stopped before the subscriptions close | fixed `9ef620ff` | `controller/controller.go` |
| N-core-1 | `phase-1-fix-core.md` | an arm in the gap between a window's expiry and its rollback drops the pending AddNOC instead of reverting it | fixed `5816f243` | `cluster/core/general_commissioning_rollback.go` |
| N-core-2 | `phase-1-fix-core.md` | a revert whose store write fails is forgotten; the next arm clears its marker | fixed `8e421ae4` | `cluster/core` |
| N-core-3 | `phase-1-fix-core.md` | every RemoveFabric error answers InvalidFabricIndex | open finding `F-db021da0-11` | `cluster/core/operational_credentials.go` |
| N-core2-1 | `phase-1-fix-core2.md` | — | held back until its fix merges (owner decision 25) — phase 2 | — |
| N-core2-2 | `phase-1-fix-core2.md` | `controller/dcl/cert.go` cites the vendor/product id rule at the old pin's lines | fixed `5b12fb30` for that citation; phase 4: the comment's other one (`Certificate.ts:397-401`, `:549-551`), not re-read | `controller/dcl/cert.go` |
| N-core2-3 | `phase-1-fix-core2.md` | every BSS's PropertiesChanged crosses the Wi-Fi driver's run loop (the match is the whole namespace) | phase 6: `wifi/wpasupplicant` | `wifi/wpasupplicant/bus_linux.go` |
| N-clumisc-1 | `phase-1-fix-clumisc.md` | the endpoint records are erased after the store's scrub | fixed `587175d9` | `node/factory_reset.go` |
| N-clumisc-2 | `phase-1-fix-clumisc.md` | `memstore.Erase` resets a hand-kept field list no test holds to the struct | phase 5: tests | `store/memstore/memstore.go` |
| N-clumisc-3 | `phase-1-fix-clumisc.md` | the generated Calibrator doc says the server sets MainState Calibrating on success; since `72817a25` only when the drive reported none during the call | phase 5: generator text and docs | `cluster/spec/closurecontrol/server_gen.go` |
| N-clumisc-4 | `phase-1-fix-clumisc.md` | cluster servers with a timer and no `MatterQuiesce` — listed, not read | phase 6: `cluster/` | `phase-1-fix-clumisc.md` |
| N-camctl-1 | `phase-1-fix-camctl.md` | a Forget that fails with `context.Canceled` is not logged | fixed `896816cc` | `controller/ota_provider.go` |
| N-camctl-2 | `phase-1-fix-camctl.md` | `Node.Erase` logs a failing store erase and returns nil | fixed `896816cc` | `controller/client/node.go` |
| N-bridge2-1 | `phase-1-fix-bridge2.md` | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-bridge2-2 | `phase-1-fix-bridge2.md` | the KeepSubscriptions=false teardown on a PASE session, where matter.js tears nothing down | open finding `F-db021da0-12` | `bridge/subscribe_dispatch.go` |
| N-bridge2-3 | `phase-1-fix-bridge2.md` | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-bridge2-4 | `phase-1-fix-bridge2.md` | `AttachSubscriptionManager` wires the host's listeners on a stopped bridge that still holds a topology | phase 2: session and subscription life cycle | `bridge/bridge.go` |
| N-bridge2-5 | `phase-1-fix-bridge2.md` | the bound's peer term does not switch from the active to the idle interval | open finding `F-db021da0-13` | `bridge/subscribe.go` |
| N-node2-1 | `phase-1-fix-node2.md` | a fabric removed while its operational record is being published leaves a stale per-fabric instance entry (inferred from reading, not measured) | phase 2: fabric life cycle | `bridge/bridge.go` `AnnounceFabric` |
| N-node2-2 | `phase-1-fix-node2.md` | the surface-change ConfigurationVersion write runs under Start's context — the shape of R2-P1-node-03 | phase 2: persistence | `node/surface_stability.go` |
| N-node2-3 | `phase-1-fix-node2.md` | a host's `Store.Endpoints` override on its own database is erased without a scrub, and no page says so | phase 5: docs (`docs/guides/persistence.md`, `docs/security.md`) | `node/factory_reset.go` |
| N-ctl2-1 | `phase-1-fix-ctl2.md` | the camera service forgets a node only on `Hooks.Decommissioned`, not on Forget or Erase | phase 2: fabric life cycle (`Hooks.Deleted` is the hook to chain) | `controller/camera/reports.go` |
| N-ctl2-2 | `phase-1-fix-ctl2.md` | two subscription reports applied at once can enqueue their writes in the opposite order of their apply | phase 2: persistence | `controller/client/node.go` `applyWith` |
| N-ctl2-3 | `phase-1-fix-ctl2.md` | a node whose store erase failed stays in the registry erased in memory, and the cull skips it | phase 2: persistence | `controller/client/registry.go` `deleteNode` |
| N-ctl2-4 | `phase-1-fix-ctl2.md` | a raw ProvideOffer that passed the closed check just before Close can open a session after it | phase 2: shutdown | `controller/camera/provider_route.go` |
| N-sec3-1 | `phase-1-fix-sec3.md` | two AddNOCs in one fail-safe: one that fails after AddFabric and whose revert fails is rolled back neither by the expiry nor at start | phase 2: commissioning and fail-safe | `cluster/core/operational_credentials.go` |
| N-sec3-2 | `phase-1-fix-sec3.md` | UpdateNOC records the VID data to restore through a read that swallows its error; the rollback then erases the fabric's VID statement and VVSC | phase 2: commissioning and fail-safe | `cluster/core/vid_verification.go` |
| N-sec3-3 | `phase-1-fix-sec3.md` | the expiry announces a fabric as removed even when its removal failed; the retry that lands announces nothing | phase 2: fabric life cycle | `cluster/core/operational_credentials.go` |
| N-sec3-4 | `phase-1-fix-sec3.md` | `TestNewStartsNoGoroutine` fails intermittently | fixed `c6f8325f` | `node/new_goroutines_test.go` |
| N-req3-1 | `phase-1-fix-req3.md` | — | held back until its fix merges (owner decision 25) — phase 3 | — |
| N-req3-2 | `phase-1-fix-req3.md` | a window transition after Stop began logs an announce failure (context canceled) | phase 2: shutdown | `node` |
| N-rest3-1 | `phase-1-fix-rest3.md` | `contract.ClusterQuiescer` says nothing of a server the topology takes back; Occupancy put back while its hold ran stays occupied until the next report; other quiescers unverified | phase 5: the contract sentence; phase 6: the other quiescers | `contract/matter.go`, `cluster/sensing/occupancy.go` |
| N-secdrv-1 | `phase-1-fix-secdrv.md` | the TLV decoder cuts a pathLen to its low byte | fixed `55ea85d8` | `secure/mattercert/decode.go` |
| N-secdrv-2 | `phase-1-fix-secdrv.md` | a negative DER pathLenConstraint other than -1 reads as absent | fixed `55ea85d8` | `secure/mattercert/der.go` |
| N-secdrv-3 | `phase-1-fix-secdrv.md` | `NewCentral` reads BlueZ's objects without a bound | fixed `87c6dd0a` | `ble/bluez/central_linux.go` |
| N-secdrv-4 | `phase-1-fix-secdrv.md` | `StopAdvertising` unexports even when the unregister failed or timed out | phase 6: `ble/bluez` | `ble/bluez/peripheral_linux.go` |
| N-reg-1 | this change (the citation pass) | a SubscribeRequest refused for its paths leaves the peer's subscriptions standing where matter.js has cleared them | open finding `F-db021da0-14` | `bridge/subscribe.go` |
| N-reg-2 | this change (the citation pass) | citations in the diff whose reference is not exported at the pins: matterjs-server `0bd06f2` (`controller/camera/registry.go`, `service.go`, `session.go`, three), connectedhomeip `examples/jf-admin-app` (`cluster/jointfabric/datastore_ops.go`, the lines of one comment) | phase 4: not performable here (not exported); unverified | — |
| N-reg-3 | this change (the citation pass) | two statements about a reference that carry no line: "which the session's force-close ends" (`bridge/exchange_routing.go`, matter.js) and the order of connectedhomeip's access check relative to its write handler | phase 4: unverified | `bridge/exchange_routing.go`, row `BD-chip-ChunkedWritesBounded` |

## Unread

What each reviewer said it did not read, summarised per unit; all of it is
phase 6 (the full pass reads every hand-written package again). The list
is the reviewers', not re-measured here.

| Unit | area | not read | phase |
| --- | --- | --- | --- |
| cam-a | `controller/camera` (first half) | its tests skimmed; `registry.go`, `service.go`, `session.go`, `policy_stream_search.go`, `state.go` only where a change depended on them | 6 |
| cam-b | `controller/camera` (second half) | its tests by grep only; `manager.go` not read, `manager_session.go` and `client_io.go` in slices; the stream search not judged against matterjs-server (not exported) | 6 |
| net | `mdns`, `thread/otbr`, `client/peer`, `client/discovery` | `mdns/doc.go`, `thread/otbr/doc.go` not opened; `thread/otbr/translate.go`, `probe.go` diff lines only; `client/peer/connection.go` not traced for every ordering of the ranking; `client/discovery/session_kinds.go` stop paths; the unit's tests. One more unread part of this unit is a held-back row (U-net-1) | 6 |
| misc | `examples/`, `device`, `script/`, `schema`, `surface`, `contract`, `fabrictest`, `internal` | most of `examples/reference-bridge` (the fleet files, `bridge_sim.go`, `media.go`, `ota.go`, `otbr.go`, `surface.go`, `wifi.go`, parts of `camera.go`); `examples/reference-controller/cli/interact.go`, `ota.go`; `device/defaults*.go`, `options.go`, `surface.go`, `type.go`, `fromspec.go`; the generators (`script/generate_matter_schema.go`, `clustergen`, `devicegen`, `testparity/render.go`); `schema/devicetype_requirements.go`; `surface/*.go`; `contract/ble.go`, `metrics.go`, `wifi.go`; the small `fabrictest` files; nearly all tests | 6 |
| clu | `cluster/` | read in full: nine files (suggestions, occupancy, valve, closure control, concentration selection, `wire/appcodec.go`, `field_bounds.go`, `spec/bound_expr.go`, `ids.go`); every other cluster file at the diff hunk only; the new chip and parity test files not read for what they assert | 6 |
| ctl | `controller/` | `controller/topology/doc.go`; `client/structure.go`, `icd.go`, `fabricops.go`, `softwareupdate/manager.go`, `dcl/certstore.go` diff hunks only; no test file in full | 6 |
| drv | `ble/`, `wifi/` | `ble/bluez/central_linux.go`, `ble/esphome/discover.go`, `gatt.go` changed hunks only; the Linux-only tests not run (darwin) and four test files not read | 6 |
| req | `bridge`, `im`, `endpoint`, `node`, `transport` | 30 entries in the first pass: most of `node/`, `endpoint/devicetype_conformance.go`, `devicetype_validation.go`, `types.go`, `im/client.go`, `invoke_client.go`, `event_report.go`, `bridge/value_compare.go`, `bridge/bridgetest`, the tail of `bridge/subscribe.go`, the comments of six files (so their citations), every test for what it asserts. The second pass (units node and req2) read `node/` and the `bridge`, `endpoint` and `im` leftovers and names what it still left; which of the 30 are unread after it, file by file, the result files do not record (they carry a count of files read) | 6 |
| core | `cluster/core` | the diff of every file, three new files whole; not read at HEAD: the unchanged bodies of `general_diagnostics.go`, `time_synchronization_features.go`, the OTA requestor and provider, `access_control.go`'s entry validation, `group_key_management.go`'s handlers, the read paths of `operational_credentials.go`, `network_commissioning_thread.go`; 67 test files but for three; most citations at the older pins. One more unread part of this unit is a held-back row (U-core-1) | 6 |
| sec | `secure`, `controller/ble`, `controller/commissioner`, `controller/ca` | `controller/ble/bletest`; `controller/commissioner/flow.go` (network steps, the BLE re-arm timer), `commissioner.go`, `controller/ca/ca.go`, `encode.go`, `secure/operational/manager.go`, `secure/sigma/protocol.go` as diff and surroundings; `client/peer` hint consumer; `client/interaction` Read; `transport/btp` beyond the session; `controller/dcl` `CDSigner`; most tests; the citations it lists as unchecked | 6 |
| node | `node` (second pass) | `node/doc.go`; `surface.go`, `config.go`, `commissioning.go` as diff; about 90 changed test files; the bridge behaviour the facade relies on | 6 |
| req2 | `bridge`, `endpoint`, `im/subscription` (second pass) | `bridge/bridge.go` lines 1-376, 1326-1395, 2500-2560; `endpoint/devicetype_conformance.go` 300-498 and 720-1469; parts of `devicetype_validation.go` and `types.go`; the rest of `bridge/bridgetest`; no test file; matter.js's handling of a failed subscription establishment | 6 |
| dr-req | the request path's diff | 25 of 31 test files beyond a grep; the rest of `bridge/subscribe.go`; `node/attestation.go` `resolveAttestation`; `store/erase.go`; the session table's close order; the ordered listener; no `-race` run | 6 |
| dr-sec | the security paths' diff | the test files skimmed; four fuzz corpus files; `cluster/core/operational_credentials.go` beyond the handlers named; whether two invokes of one session run concurrently; `crypto/x509`'s refusals against a certificate corpus | 6 |
| dr-rest | the rest of the diff | seven docs diffs, two READMEs, `notes/parity/matterjs_tests.md`, `script/testparity/register.json`; `controller/camera` beyond the hunks; the Linux-only `ble/bluez` tests; most test files of the list; the truncated-query path not run | 6 |
| U-core-1 | unit core | — | held back until its fix merges (owner decision 25) — phase 3 |
| U-net-1 | unit net | — | held back until its fix merges (owner decision 25) — phase 3 |

## The citation pass (this change)

Read at the pins — matter.js `db021da0`, connectedhomeip `6170af84`, the
exported trees — by the register editor:

- The citations the campaign added: the 125 added comment lines of
  `git diff e1f18a2e..c6f8325f -- '*.go'` that carry a `file:line`, with
  the continuation lines that carry only a `:line`. Every range was read
  except those of two references that are not exported (N-reg-2).
- The 12 citations the reviewers reported as failing (11 in the two
  passes, 1 in the diff review). Four had been re-cited by the campaign
  (`bridge/receive.go`, `bridge/im_gate.go`,
  `controller/attestation/cert.go`, the `MRP.ts` citation of
  `bridge/subscribe.go`) and hold at the pin; eight were corrected in
  `5b12fb30`. None was removed: every claim has lines at the pin, three
  of them other lines than it named and saying something else (below).
- Corrected in `5b12fb30`, comment text only: 41 edits in 29 files. Line
  numbers at the pin for the eight failing citations and their twins in
  the same files (`bridge/subscribe.go`, `subscribe_dispatch.go`,
  `receive_dispatch.go`, `bridge.go`, `conn.go`,
  `im/subscription/manager.go`, `fabric_leaving_flush_test.go`), for four
  pre-existing citations the campaign's lines had moved
  (`controller/client/node.go`, `registry.go` twice, `controller/dcl/cert.go`),
  and the pin stated where a new citation named none.
- Three claims did not say what their lines say, and each had been the
  reason given for the code's behaviour:
  1. "the newest authenticated source address wins (matter.js
     ExchangeManager.ts:425-431)" — `bridge/conn.go`, `bridge/bridge.go`,
     `bridge/subscribe.go`, the twins of the `bridge/receive.go` citation
     that justified binding a session's route on every datagram. At the
     pin matter.js adopts the address only of a non-duplicate initiator
     message that opens a new exchange (`:462-486`). Phase 1 changed the
     binding (R2-P1-req-01); the comments now say whose rule it is
     (`BD-chip-SessionRouteByNewCounter`).
  2. "InteractionServer.ts:549-566 which matches on same-session for
     pre-fabric (PASE) subscriptions" — `bridge/subscribe_dispatch.go`. At
     the pin there is no teardown on a session without a fabric (`:647`).
     The code is unchanged; the difference is open finding
     `F-db021da0-12`.
  3. "the MAX_SUBSCRIBE_PATHS check that follows it (:600-604)" —
     `bridge/subscribe.go`. At the pin the count check (`:635-641`)
     precedes `validateReadPaths` (`:699`), with the KeepSubscriptions
     teardown between them; open finding `F-db021da0-14`.
- One claim was narrower than its comment: `transport/btp/accept.go` said
  matter.js keeps the connection of "a central whose request names no
  version". At the pin a request that names no *supported* version is
  disconnected (`BtpSessionHandler.ts:106-108`); the connection stays
  only for one whose version nibbles are all zero (`BtpCodec.ts:219-222`).
  The code's behaviour stands on its own rule (owner decision 31).
- `secure/attestation` reads a malformed vendor or product id as not
  stated. Re-read at the pin: matter.js's device does the same for its
  own chain (`DeviceCertification.ts:116-120`) and pins it with a test
  (`AttestationCertificateProfileTest.ts:610-624`). Parity; no finding
  was opened, and `BD-Matter-AttestationDERAsReceived` says so.

## Counts

The 107 findings, by their first disposition:

| disposition | count |
| --- | --- |
| fixed | 102 |
| by_design row, no code change | 2 |
| struck | 1 |
| held back | 2 |

Of the 102 fixed: 19 were corrected by a finding of the diff review,
6 brought an Ahead row, 7 a `by_design` row or an amended one, and
3 left an open finding (`F-db021da0-10`, `-11`, `-13`).

The 43 noticed items: 11 fixed, 4 open findings, 1 struck,
11 phase 2, 2 phase 4, 5 phase 5, 3 phase 6, 6 held back.

Unread: 15 units, phase 6, and 2 held-back rows.

Rows written in `by_design.md`: 6 Ahead rows with their entries, 5
`by_design` entries, 5 entries amended. Findings opened: 5
(`F-db021da0-10` to `-14`).

**Held back: 10 rows** — 2 findings (R2-P1-req2-11, R2-P1-dr-req-09), 6
noticed items and 2 unread parts; 9 in phase 3, 1 in phase 2.

## Not placed, not verified

- The three items phase 0 placed in phase 1 (S-16, the unexplained
  `-race` report in `controller/client`; rework-flow questions 1 and 2):
  no finding and no fix note of phase 1 names them. `phase-1-fix-bridge2.md`
  records one `-count=20` run of `controller/client`'s
  `TestTypedReadsConsumer` (20 of 20 after its fix, failures before);
  whether that is S-16 is not stated there. Unplaced.
- Every statement of the rows and findings of this change that is marked
  "unverified" or "not read" in its text; the two references that are not
  exported at the pins (N-reg-2).
- Whether the sibling checkouts hold what the exported pins hold was not
  compared; every line cited here was read in the exported trees, and the
  Ahead markers were also checked by `script/aheadcheck` against the
  `db021da0` commit of the local matter.js clone.

## The review of the rework (coordinator, 2026-11-14)

One reviewer read the 24 commits of the rework after the diff review as a
whole (ids `R2-P1-rw-NN`). Seven of its eight findings are fixed in this
branch; the dispositions above do not count them.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P1-rw-01 | medium | fixed `634112e7` | AddNOC and UpdateNOC retry an owed rollback before they read the fabric table; reproduced by the reviewer's test |
| R2-P1-rw-02 | low | fixed `44affd25` | an owed UpdateNOC rollback whose fabric was removed meanwhile is settled; reproduced |
| R2-P1-rw-03 | low | fixed `da8f0961` | a `FactoryReset` whose stop did not stop the bridge erases and closes nothing |
| R2-P1-rw-04 | low | fixed `5b55ff86` | the quiesce and re-arm of a reassembly follow the topology the bridge serves |
| R2-P1-rw-05 | low | fixed `8d041204` | an arm that waited for an expired fail-safe's rollback arms nothing when that rollback ended its own session; phase 2: a PASE session established between the rollback's PASE close and its end is refused although it survived |
| R2-P1-rw-06 | low | fixed `36f70a1f` in part; phase 2 | CommissioningComplete reads both markers before it clears either and writes back a cleared one; the settings port has no call that writes two keys at once, so one in-between state remains and is pinned by a test |
| R2-P1-rw-07 | note | phase 4 | a restored subscription is admitted priming and gets no priming abort; what the reference does was not read |
| R2-P1-rw-08 | low | fixed `3be12e66` | the re-entry deadlock tests are bounded by the test's own deadline |

Two states of the pending-NOC marker that its last author found by reading
and did not exercise are held back until their fix merges (owner decision
25; phase 2): 12 rows are held back in all.

**For phase 2.** Three rounds changed the pending-NOC marker in turn
(R2-P1-core-01, its residuals, R2-P1-dr-sec-01 to -04, R2-P1-rw-01, -02,
-06), and each change opened something. Phase 2
(flows: commissioning and fail-safe, persistence and crash consistency)
reads the mechanism as a whole against `BD-Matter-PendingNOCMarker` and
puts the alternative to the owner: keep an uncommitted fabric in memory
until CommissioningComplete, as both references do, instead of writing it
at once and rolling it back from a marker.

