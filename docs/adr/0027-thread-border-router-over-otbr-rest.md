# ADR 0027 — A Thread Border Router over OTBR REST: ThreadBorderRouterManagement enters the scope as a server with logic; the Thread radio stays out

- **Status**: Accepted (2026-10-25; roadmap milestone 6, phase 26a — the four
  open points decided by the owner on 2026-10-23, see "Decisions taken");
  implemented in wave 26a (brief `notes/briefs/2026-10-27-M6-26a-thread-border-router.md`,
  report `notes/briefs/reports/26a.md`)
- **Date**: 2026-10-23
- **Related**: [ADR 0024 — BLE commissioning in the controller role](./0024-ble-commissioning-in-the-controller.md)
  (decision 4: provisioning a Thread dataset needs no Thread radio),
  [ADR 0016 — device-type validation](./0016-device-type-validation.md),
  [ADR 0017 — the generated default cluster server](./0017-generated-default-cluster-server.md),
  [ADR 0026 — architecture principles](./0026-architecture-principles.md) (the layers),
  [roadmap](../roadmap.md) milestone 6 phase 26,
  `docs/matterjs-comparison.md` (the `thread-br-client` row and the
  "Thread / network infrastructure (4)" row), `docs/architecture.md`
  ("Boundaries that are decisions", the Thread radio row, concept v3)

## Context

"No Thread radio" has been a scope boundary since concept v3: a Thread
radio is hardware, and the stack that drives it (OpenThread: 802.15.4 MAC,
6LoWPAN, MLE, MeshCoP) runs on an MCU or, as a border router, on a Linux
host with a radio co-processor. Nothing of that is Go's place, and ADR 0024
kept the boundary when the commissioner learned to hand a device a dataset.

Phase 26 nevertheless names the four Thread and network-infrastructure
clusters, the `ThreadBorderRouter` device type (`0x91`) and "Thread network
diagnostics". Served as generated stubs (ADR 0017) they would be worse than
absent: an endpoint that declares `0x91` and answers `BorderAgentId` with a
default claims a border router that does not exist. A controller finds
border routers by that device type.

The host that does exist is a Linux machine running `ot-br-posix` — the
Raspberry Pi with an RCP dongle, the usual Home Assistant set-up. Its OTBR
exposes the Thread mesh as an IPv6 interface, so Matter over Thread is
already Matter over UDP/IPv6 to this module, and it exposes a REST API.
Nothing on this side needs to be a Thread node to manage that border router.

**What matter.js has at the pin.** `packages/thread-br-client` (added
2026-07-03, matter.js #4013; 10 730 lines of `src/`) is a *client*, not a
port of OpenThread. Its README names the scope: passive `_meshcop` mDNS
discovery, the Operational Dataset codec, the OTBR REST adapter, MeshCoP
diagnostics over CoAP/DTLS-EC-JPAKE, "read-only Thread diagnostics", with
the mutating OTBR operations exposed but never triggered by the library.
`OtbrRestClient.ts` (847 lines) wraps `/node`, `/node/state`,
`/node/network-name`, `/node/rloc`, `/node/ext-address`, `/node/ext-panid`,
`/node/ba-id`, `/node/leader-data`, `/node/dataset/active` and
`/node/dataset/pending` (GET, and PUT in `setActiveDataset` :424 /
`setPendingDataset` :440), `/diagnostics` (:246), `/api/actions` (:582),
`setState` (:410), `factoryReset` (:457), `setCommissionerState` (:467);
`OtbrRestCapability.ts:8-30` probes the key casing (`pascal` on older
OTBR builds, `camel` after 2024) and which diagnostics flavour the border
router serves. The dataset codec, `packages/protocol/src/thread/`
(`OperationalDataset.ts`, 327 lines), is already ported: `controller/thread`
(`doc.go`, at matter.js `862ad75f`), used by `controller/commissioner/network.go:360`.

matter.js has **no server behaviour** to mirror here:
`ThreadBorderRouterManagementServer.ts` is a 14-line generated stub, as are
`ThreadNetworkDiagnosticsServer.ts` and `ThreadNetworkDirectoryServer.ts`,
and nothing in `packages/*/src` wires `thread-br-client` into any of them
(a grep over `*.ts` outside `dist/` for `ThreadBorderRouterManagement`
finds only the model, the types and the device type).
`NetworkCommissioningServer.ts` carries no Thread branch either.

**What chip has at the harness pin (`6170af84`).** The CSA reference serves
the cluster with logic, and `CLAUDE.md` makes chip the authority where
matter.js models nothing.
`src/app/clusters/thread-border-router-management-server/ThreadBorderRouterManagementCluster.cpp`
(330 lines) over `ThreadBorderRouterManagementDelegate.h` (109 lines), with
`src/platform/OpenThread/GenericThreadBorderRouterDelegate.cpp` (233 lines)
as the in-process OpenThread delegate of `examples/thread-br-app`. The rules,
each a readable line:

| Rule | `ThreadBorderRouterManagementCluster.cpp` |
| --- | --- |
| commands only over CASE, else `UnsupportedAccess` | :151-153 |
| `SetActiveDatasetRequest` needs the fail-safe armed, else `FailsafeRequired` | :212-214 |
| undecodable dataset → `InvalidCommand` | :220 |
| an active dataset already configured → `InvalidInState` | :224-229 |
| an activation still pending → `Busy` | :234 |
| sequence number per activation, breadcrumb kept until completion | :238-242, :286-288 |
| `SetPendingDatasetRequest` only with feature `PC`, else `UnsupportedCommand` | :253-255 |
| fail-safe expiry reverts the dataset and answers `Timeout` | :301-316 |
| fail-safe disarm commits it | :321 |
| `ActiveDatasetTimestamp` / `PendingDatasetTimestamp` read from the dataset's Active Timestamp TLV | :61-75 |

The delegate (`ThreadBorderRouterManagementDelegate.h:63-106`): `Init`,
`GetPanChangeSupported`, `GetBorderRouterName`, `GetBorderAgentId`,
`GetThreadVersion`, `GetInterfaceEnabled`, `GetDataset`, `SetActiveDataset`
with an `ActivateDatasetCallback` that fires once the border router is
attached (:41-45), `CommitActiveDataset`, `RevertActiveDataset`,
`SetPendingDataset`. The certification cases are in the harness checkout:
`src/app/tests/suites/certification/Test_TC_TBRM_2_1.yaml` … `2_4` and
`3_1_Simulated`. chip's own CI builds an `otbr-agent` with the simulation
RCP for its tests (`.github/workflows/tests.yaml:345-346`).

**The device type couples two clusters.** `thread-border-router.ts:36-39`:
ThreadBorderRouterManagement and NetworkCommissioning "shall reflect the
same underlying network configuration", `MaxNetworks` is 1. The module's
NetworkCommissioning has an Ethernet base and a Wi-Fi variant
(`cluster/core/network_commissioning.go`, `network_commissioning_wifi.go`);
a Thread variant does not exist yet.

The cluster surface is small: six attributes (`BorderRouterName`,
`BorderAgentId`, `ThreadVersion`, `InterfaceEnabled`,
`ActiveDatasetTimestamp`, `PendingDatasetTimestamp`;
`thread-border-router-management.element.ts:25-32`), four commands, one
optional feature `PC` (:22). The REST contract needs `net/http` only, which
`controller/dcl` already uses; no sixth-plus dependency.

## Decision

1. **The Thread radio stays out; a border-router client enters.** The
   module never joins a Thread mesh. It manages an *existing* OpenThread
   Border Router over its REST API, as `@matter/thread-br-client` does.
   The host decides whether to attach one; a bridge without it is
   unchanged in composition, FeatureMap and chip-tool runs.

2. **The dataset codec moves down.** `controller/thread` becomes `thread/`
   (layer `foundation`, next to `tlv` and `schema`: the codec imports
   nothing of the module and holds no state) so that a server in
   `cluster/` may import it — `servers` sits below
   `roles` in `script/archcheck/layers.json`. `controller/thread` forwards
   until `v0.12.0`, the window the `controller/<name>` → `client/` moves
   use (`CLAUDE.md`, ADR 0026 principle 12). That window had passed when
   the move landed: wave 26a removed `controller/thread` at once, with a
   migration-guide line, instead of keeping forwarders (owner decision
   2026-10-27).

3. **Three servers, one delegate.** `cluster/threadbr/` holds
   - `ThreadBorderRouterManagement` with chip's rules from the table
     above, cited per line, against a `BorderRouter` interface that is
     `ThreadBorderRouterManagementDelegate.h:63-106` in Go: name, agent
     id, Thread version, interface state, dataset read, active-dataset
     activation with completion, commit, revert, pending-dataset write;
   - `ThreadNetworkDiagnostics` read-only from the same interface: the
     17 mandatory attributes (`thread-network-diagnostics.element.ts`:
     Channel … LeaderRouterId, SecurityPolicy, ChannelPage0Mask,
     ActiveNetworkFaultsList, the provisional ExtAddress and Rloc16) from
     `/node`, `/node/leader-data`, the active dataset and the border
     router's own `/diagnostics` record (Route64 and child table,
     `tlv/diag/Route64.ts`, `ChildTable.ts`), plus the features `MLECNT`
     and `MACCNT` (:27-28) from the MLE and MAC counter TLVs of the same
     record (`tlv/diag/MleCounters.ts`, `MacCounters.ts`); `PKTCNT` and
     `ERRCNT` stay off;
   - the Thread variant of NetworkCommissioning next to the Wi-Fi one
     (`cluster/core/network_commissioning_thread.go`), sharing the
     `BorderRouter` so both clusters reflect one network, as
     `thread-border-router.ts:36-39` demands; `MaxNetworks` 1.
     `ScanNetworks` is mandatory under `TH`
     (`network-commissioning.element.ts:77`, conformance `WI | TH`) and
     the OTBR REST surface has no network scan, so the server answers
     with what a border router knows without a radio scan: one
     `ThreadScanResult` (PanId, ExtendedPanId, NetworkName, Channel)
     from the active dataset, status `Success`; an empty list while no
     dataset is configured.
   The server is a hand-written server with logic matter.js's stub does
   not carry; `notes/parity/by_design.md` records it (candidate
   `BD-Thread-TBRM-ChipLogic`): mirrored from chip, not invented, with
   the lines above as provenance. `ThreadNetworkDirectory` stays a
   generated stub (ADR 0017); no device type of the snapshot mandates it
   on a border router.

4. **The adapter is `thread/otbr/`**, layer `support`: stdlib `net/http`,
   the endpoints of `OtbrRestClient.ts` named above, the key-casing probe
   of `OtbrRestCapability.ts`, `PUT /node/dataset/active|pending` for the
   writes. The REST surface has no attach callback, so the activation
   completion chip's delegate reports (`Delegate.h:41-45`) is polling of
   `/node/state` until the role leaves `disabled`/`detached`, bounded by
   the fail-safe. The adapter exposes the mutating operations only
   through the cluster's fail-safe-guarded path; `factoryReset` and
   `setCommissionerState` are not wrapped (the thread-br-client README's
   own warning: they "can silently partition or brick a network").
   The REST link is plaintext HTTP, as ot-br-posix serves it and as
   matter.js's `OtbrRestClient` builds its URLs: the operational dataset
   with its network key and PSKc crosses it in clear, so the border router
   belongs on the host's own trusted LAN segment.

5. **MeshCoP stays out of this ADR.** CoAP over DTLS with EC-JPAKE
   (`dtls/`, `coap/`, `commissioner/`, `diagnostic/MeshCopDiagnosticSource.ts`,
   together more than half of thread-br-client) has no stdlib DTLS, so it
   is a dependency decision of its own, and no cluster of the module
   needs it: everything the three servers read is on the REST surface.
   `_meshcop` discovery is not needed either — the host names its border
   router.

6. **The reference daemon** gains an optional flag (working name
   `--otbr http://[host]:8081`) that adds one `ThreadBorderRouter`
   endpoint; without it nothing moves, so the existing chip-tool guard
   runs are untouched (`CLAUDE.md`, token rules: no run when composition
   and FeatureMap did not move).

7. **Proof**, in the order ADR 0024 used: unit tests of every rule in the
   table, one per line; adapter tests against recorded OTBR REST fixtures
   in both key formats; the harness's TBRM family
   (`GOFABRIC_CHIP_FAMILIES=TBRM`) against the daemon with a virtual OTBR,
   built as chip's CI builds it (`otbr-agent` with the simulation RCP,
   `tests.yaml:345-346`), in a manually dispatched job first; a real OTBR
   in the manual test plan. *Unverified until tried:* that the simulation
   RCP runs on the `ubuntu-24.04` runner, and whether `TBRM_2_x` needs a
   second Thread node (`3_1_Simulated` suggests the harness simulates
   it).

## Consequences

- `docs/matterjs-comparison.md`: the `thread-br-client` row moves from
  "No" to the OTBR REST subset with this ADR; the "Thread / network
  infrastructure (4)" row splits — three served, `ThreadNetworkDirectory`
  stub. `docs/architecture.md` keeps the "Thread radio" row and gains a
  row for the border-router client. `docs/certifiability.md`: the TBRM
  family and the CNET Thread cases move from (d) out of scope to (c) not
  yet run. `CLAUDE.md`'s scope line names the ADR. The roadmap carries
  the work as its own phase 26a; phase 26 keeps the generated stubs and
  loses "Thread network diagnostics".
- `script/archcheck/layers.json` gains `thread`, `thread/otbr` and
  `cluster/threadbr`; the package move is a `CHANGELOG.md` entry and an
  `apidiff` finding with its forwarders.
- The host's obligation grows by one reachable port: the OTBR REST
  listener (default 8081) on the host or the LAN. The adapter's errors
  name the HTTP cause, and a border router that stops answering is an
  `InterfaceEnabled` false, not a crash.
- Nothing here is certified (ADR 0011); TBRM certifiability is tracked
  in `docs/certifiability.md` like every other family.

## Decisions taken (owner, 2026-10-23)

The four points the draft left open, answered before it was filed:

1. **Phase.** Its own phase 26a, after the stubs of 26: the `thread/` move,
   `cluster/threadbr`, the Thread NetworkCommissioning variant,
   `thread/otbr` and the virtual OTBR in CI, with their own proof. Phase 26
   keeps the generated stubs and the camera work.
2. **ThreadNetworkDiagnostics depth.** The mandatory set plus the MLE and
   MAC counter features, as written into decision 3; packet and error
   counts stay off.
3. **`ScanNetworks` under `TH`.** The border router's own network from the
   active dataset, as written into decision 3.
4. **Layer of `thread/`.** `foundation`.
