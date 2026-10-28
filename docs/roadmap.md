# Roadmap

Set by the owner on 2026-10-17, at `v0.6.0`, after the module's node had
been paired and driven by Apple Home and by Home Assistant's Matter server
(both on the reference bridge and OpenCCU-Loom). This page is the one place
where the order of the remaining work is written down: what each milestone
contains, roughly how it is phased, what it is worth and to whom, and when
it counts as closed. The detail of a phase lives in its own document,
written when the phase is next (see [How a phase is planned](#how-a-phase-is-planned)).

## The goal

**Complete Matter 1.6.1, wherever matter.js implements it.** matter.js is
the gold standard ([`CLAUDE.md`](https://github.com/SukramJ/go-fabric/blob/main/CLAUDE.md));
the measure of "complete" is therefore its HEAD, not the specification
text: every cluster, device type, protocol feature and behaviour matter.js
has, this module has — semantically, with the same defaults, the same
constraints and the same wire shape — and nothing it lacks is invented here
unless a connectedhomeip reference exists, as the parity contract already
allows (DiagnosticLogs, FanControl coupling). A generated matter.js stub
counts as an implementation: it is served here by the generated default
server ([ADR 0017](./adr/0017-generated-default-cluster-server.md)), which
is exactly as much as matter.js offers for it.

**Beyond the standard: the embedding layer.** matter.js is the gold
standard; this module is meant to be the diamond one — not a larger
protocol surface, but a smaller distance between a host's device model and
a paired, working Matter node. The consumers that set that bar:

| Consumer | State | What it needs from this module |
| --- | --- | --- |
| [OpenCCU-Loom](https://github.com/SukramJ/openccu-loom) | in production; imports 32 packages of this module in its product code (45 with tests) at its `v0.6.0` pin; the target after milestone 4 is the facade plus the cluster packages of the servers it customises ([concept v6](./history/concept-v6-embedding-layer.md), decision 0.4) | a complete `node/` + `device/` + `contract/` surface so that a bridge needs nothing below it; depth in the clusters HomeMatic devices map to (Switch, Thermostat, DoorLock, WindowCovering) |
| [go-ha2fabric](https://github.com/SukramJ/go-ha2fabric) (Home Assistant entities as a bridge for Matter controllers) | concept, not started: its `CONCEPT.md` was written 2026-10-24 (wave CF) on the owner's decisions of that day — a thin client between the Home Assistant WebSocket API and `go-fabric-server`, the stand-alone node with a remote protocol planned after milestone 4 | `go-fabric-server` and its protocol; the mapping table of go-hamapping |
| the `go-*2mqtt` family (daikin, homeconnect, mtec, unifi, zendure) via [go-hamapping](https://github.com/SukramJ/go-hamapping) (`go-hamqtt`'s Home-Assistant vocabulary → the device kit's declarations; lives outside this module, so `contract/` stays stdlib-only) | concept, not started: `CONCEPT.md` written 2026-10-24 (wave CF); go-zendure2mqtt embeds the kit directly in the phase-22 cookbook (wave CE) | the declarative device kit, the in-process test pair; energy and appliance device types, which already exist |
| `examples/reference-controller` and a host acting as commissioner | shipped, controller stages 1–6b | the field features (milestone 1), ICD peers and the rest of OTA push (milestone 5) |

Priority follows from that table: a feature a consumer uses comes before
one none uses, and both come before completeness for its own sake.

## How to read this page

- A **milestone** closes a topic area completely; it is not a sprint. One
  minor version per milestone (`v0.7.0` is milestone 1), patch versions for
  the waves inside it. The `v1.0.0` cut is the owner's call and is not
  scheduled here; the `0.x` series continues until then, and the freeze of
  `device/`, `node/` and `controller/` stays bound to that cut.
- **Phases** continue the numbering of the concepts (concept v3 ends at
  phase 13). A phase is one to three implementation waves, one pull request
  per wave ([one PR per wave](https://github.com/SukramJ/go-fabric/blob/main/CLAUDE.md)).
- **Definition of done** is the same three pillars for every milestone, and
  a milestone is not closed while one is missing:
    1. the chip certification families the milestone names run green in
       the harness (`make chiptool-families`), or are classified in
       [`certifiability.md`](./certifiability.md);
    2. the matter.js test files the milestone names are ported as Go tests
       and recorded in the test-porting register with its ratchet
       (milestone 2 creates both);
    3. the rows of [`matterjs-comparison.md`](./matterjs-comparison.md)
       the milestone names move to ✅, or the gap moves to
       [`by_design.md`](https://github.com/SukramJ/go-fabric/blob/main/notes/parity/by_design.md)
       with its reasoning.
- **Every milestone opens with a pin check.** matter.js HEAD moves while a
  milestone runs (the checkout is 52 commits past the pin `0ceb202d`
  today); the first wave of each milestone lifts the pin, classifies the
  snapshot diff and regenerates, as concept v2 phase 6 did. A fact about
  matter.js quoted in this page is at the pin named in the milestone.

## Baseline (2026-10-17, `v0.6.0`)

- 118 of the snapshot's 135 clusters served, 79 of its 91 device types
  generated; the matter.js servers with hand-written logic number 25
  (≥150 lines) and their depth here varies from a full port (ColorControl,
  LevelControl, WindowCovering, OnOff, OTA) to none (Switch: the generated
  definition and the wire type only, against `SwitchServer.ts` 486 lines
  and a 3 527-line test).
- Controller role stages 1–6b landed; Joint Fabric (6c) deferred by
  decision ([ADR 0020](./adr/0020-commissioner-and-controller-role.md)).
- Concept v3 phases 11–13 open: `crypto.Signer`, window-open from the
  reference controller, the DCL snapshot and BLE commissioning
  ([ADR 0024](./adr/0024-ble-commissioning-in-the-controller.md)) — none
  of it in the tree yet.
- Verified ecosystems: Apple Home, Home Assistant (owner, 2026-10-17);
  Google Home and Alexa await the owner at the device
  ([ecosystem observations](./matter-ecosystem-observations.md)).
- matter.js's own tests, the proof for devices nobody has on the bench:
  209 `*Test.ts` under `packages/node/test` (54 of them behaviour tests
  in 36 cluster directories, 49 node-level), 114 under
  `packages/protocol/test`, 68 certification cases under
  `support/chip-testing/test/cert`. None of them is ported as such; the
  module's parity tests were written from the servers, not from these
  files.

## Milestones

| # | Milestone | Version | Phases | Value, in one line |
| --- | --- | --- | --- | --- |
| 1 | [The commissioner in the field](#milestone-1-the-commissioner-in-the-field) | `v0.7.0` | 11–13 | a boxed Wi-Fi or Thread product is commissioned by this module and handed on to Apple Home |
| 2 | [The test foundation](#milestone-2-the-test-foundation) | `v0.8.0` | 14–16 | every later milestone is proved against matter.js's own tests; consumers test their bridge without hardware |
| 3 | [Behaviour depth](#milestone-3-behaviour-depth) | `v0.9.0` | 17–19 | the 25 matter.js servers with logic are ported line for line — remotes, thermostats, locks behave as the ecosystems expect |
| 4 | [The embedding layer](#milestone-4-the-embedding-layer) | `v0.11.0` | 20–22 | a bridge is `node/` + `device/` + `contract/`; a new consumer is a day's work, not a port |
| 5 | [ICD and the controller remainder](#milestone-5-icd-and-the-controller-remainder) | `v0.12.0` (23–24), `v0.13.0` (25) | 23–25 | battery devices stay reachable; the controller pushes firmware and uses TCP |
| 6 | [Completeness](#milestone-6-completeness) | `v0.14.0` | 26–27 | 135/135 clusters, 91/91 device types, every matter.js row of the comparison closed |
| 7 | [BLE on the device side](#milestone-7-ble-on-the-device-side) | `v0.15.0` | 28 | a node of this module without a network is commissioned over BLE, as matter.js's `nodejs-ble` node is |

Milestones 2 and 3 are the owner's order of 2026-10-17: the test
foundation first, because milestones 3–7 are proved on it; the behaviour
depth before the embedding layer, so that the API frozen in milestone 4 has
seen what Switch, Thermostat and ICD need from it.

### Milestone 1: The commissioner in the field

**Detail:** [concept v3](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v3-commissioner-in-the-field.md) (German, in the history since the milestone closed) and
[ADR 0024](./adr/0024-ble-commissioning-in-the-controller.md).

| Phase | Content | Proof |
| --- | --- | --- |
| 11 | `crypto.Signer` behind the controller CA; window-open from the reference controller (multi-admin from our side); the embedded DCL snapshot with modes dev / prod / none; the status table in the README | in-repo tests; the hand-over to Apple Home in the manual test plan |
| 12 | BTP (`transport/btp`), `controller/ble`, the network steps of the commissioning flow (Wi-Fi credentials, Thread operational dataset, `ConnectNetwork`), a simulated Wi-Fi driver in the reference daemon; **the decision where the Thread dataset comes from** (operator-supplied, or the Thread border-router client of milestone 5) | BTP vectors against `BtpSessionHandlerTest.ts` (1 526 lines) and `BtpCodecTest.ts`; network steps against the daemon |
| 13 | the sub-module `ble/bluez`, `pair --ble`, the `btvirt` job with chip's all-clusters-app | BLE end to end, virtual; a real device in the test plan |

**Value.** A commissioner that only reaches devices already on the network
is a laboratory tool. After this milestone a host built on this module
commissions a boxed product and hands it on. Consumers: the reference
controller, a loom installation acting as commissioner.

**Done when.** The comparison rows BLE/BTP and DCL move to ✅ for the
controller role; `TC-DD` and `TC-SC` BLE cases are classified; the
`btvirt` job passes.

**Status 2026-10-18.** Done, released as `v0.7.0`: phases 11–13 landed
(pull requests 143, 144, 147, 149); the `ble-virtual` job is green against a
matter.js node (ADR 0024 §5 as amended); the BLE and Thread cases of DD and
SC are classified out of scope for the device side
([certifiability](./certifiability.md)). The field proof with a boxed
product is the owner's manual test plan.

### Milestone 2: The test foundation

**Detail:** [concept v4](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v4-test-foundation.md) (German), written 2026-10-18 when the milestone opened, in the history since it closed.

**Status 2026-10-18.** Phase 14 done, released as `v0.7.1` (pull requests 151–154): pin `862ad75f`, the upstream fixes classified, the test-porting register (134 `planned` on its ratchet) and the citation guard (266 drifts on its ratchet) in CI. Phase 15 done, released as `v0.7.2` (pull requests 157, 158, 160): every time source of the module behind `clock.Clock`, `clocktest.Fake` after `MockTime` and the no-raw-time guard; the public `fabrictest` package — in-process devices and controllers on an in-memory network and one clock, after `MockSite` — with the controller's own tests on it and the testing-in-process guide. Phase 16, first wave (brief BH, pull request 164), released as `v0.7.3`: the port of `packages/protocol/test` — 987 cases in Go, every missing case with its reason, 13 files `planned` with an `until`, the fixes the ports found and the X.520 DN gap registered first among eighteen findings; the second wave (brief BI, pull request 168), released as `v0.8.0`: the port of `packages/node/test/node` onto `fabrictest` — device side and controller side, every missing case with its reason, 69 files `planned` on the ratchet (the four node files left with `until M5 phase 25`), 22 fixes the ports found, the controller's `Restart`/`CommissionNode`/`Discover` in `fabrictest`. **Milestone 2 is closed with `v0.8.0` (2026-10-19).**

| Phase | Content | Proof |
| --- | --- | --- |
| 14 | pin lift to matter.js HEAD (snapshot diff classified, generators, citations re-checked, `chipdm-check` green); the **test-porting register** `notes/parity/matterjs_tests.md`: every `*Test.ts` under `packages/node/test` and `packages/protocol/test` mapped to the Go test(s) that port it or to a classified exclusion (controller-only at this stage, JS-runtime-only, Joint Fabric), with a ratchet guard like `reachability-check` | the guard fails when a matter.js test file is unmapped |
| 15 | the public test package (working name `fabrictest`, next to `bridgetest` and `endpointtest`): an in-process node, an in-process controller, the commissioned pair (`MockSite.addCommissionedPair`), a fake clock injected through one `Clock` port instead of the five ad-hoc fakes in the tree today (`MockTime`: `advance`, `resolve`, `yield`), the wire-path helpers (`interaction.read/write/invoke/subscribe` of `node-helpers.ts`) | the module's own suites run on it; the concurrency guide names it |
| 16 | the protocol ports: `packages/protocol/test` (114 files — MRP, MessageExchange, MessageReceptionState, SessionManager, SecureSession, PASE/CASE pairing, MessageCodec, Certificates, DeviceAttestationValidator, Bdx, TcpChannel, Mdns*, FailsafeContext …) and the node-level suite `packages/node/test/node` (49 files — the Attribute/Command/Event response tests, the commissioning tests, ServerSubscription) as Go tests against this stack | register at 100 % for the two trees, behaviour tests excepted (milestone 3) |

**Value.** The owner's premise: the bench will never hold every device, so
the proof has to come from matter.js's tests — which encode the ecosystem
fixes matter.js collected. The package of phase 15 is also the first piece
of the embedding layer: a consumer (loom, the HA bridge, a 2mqtt daemon)
tests its own device mapping against an in-process controller, with no
chip-tool and no radio. Consumers: every one.

**Done when.** The register exists, the guard holds, phases 15–16 are at
100 %, and the per-package coverage floors did not drop.

### Milestone 3: Behaviour depth

The 25 matter.js servers with hand-written logic, each audited against its
Go counterpart with the matter.js test file ported as the proof. Grouped
by the surface they serve; the figures are matter.js lines of server / of
test.

**Detail:** [concept v5](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v5-behaviour-depth.md) (German), written 2026-10-19 when the milestone opened, in the history since it closed.

| Phase | Content | Proof |
| --- | --- | --- |
| 17 | **the device surface loom and the HA bridge map to:** Switch — a real server (486 / 3 527: the multi-press and long-press state machine, `SwitchServer.ts`), Thermostat — presets, schedules, atomic writes (1 790 / 1 776 in six files), DoorLock (1 628 / 1 946), WindowCovering (654 / 145), OnOff (343 / 352), LevelControl (875 / 944), ColorControl (2 046 / 610), Identify (156 / 187), ServiceArea (300 / 234) | SWTCH, TSTAT, DRLK, WNCV, OO, LVL, CC, I families green; the nine test files ported |
| 18 | **the system set:** Descriptor (303 / 685), GeneralDiagnostics (631 / 235), AccessControl (775 / 619), OperationalCredentials (632 / 360), GroupKeyManagement (618 / 209), Groups (232 / 100), Groupcast (938 / 2 243), Binding (214 / 2 455), BasicInformation (213 / 214), GeneralCommissioning (285), AdministratorCommissioning (267), ScenesManagement (1 109 / 99) | ACL, ACE, DESC, DGGEN, OPCREDS, GRPKEY, G, S, BIND, BINFO, CGEN, CADMIN green; the test files ported |
| 19 | **OTA and the remaining behaviour tests:** OTA requestor and provider (1 436 + 697 / 2 819 + utils), and the behaviour tests of the generated servers (boolean-state, fan-control, measurement, occupancy-sensing, power-source, hepa-filter-monitoring, concentration-measurement, energy-evse-mode, rvc-operational-state, commodity-tariff, thermostat-user-interface-configuration, time-format-localization, unit-localization, window-covering) | SU green; the register at 100 % for `packages/node/test/behaviors`: no `planned` left there, every `missing` with its reason (js-runtime-only, a `by_design` entry, or `until` M4/M5 with a findings entry) — owner decision 19.4, [concept v5](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v5-behaviour-depth.md) Teil F |

**Value.** This is where the ecosystems judge a bridge: a HomeMatic remote
that double-clicks, a thermostat whose presets Apple Home writes
atomically, a KeyMatic that reports its lock state the way `DoorLockServer`
does. Consumers: loom (switches, thermostats, locks, covers), the HA bridge
(the same surface from the Home Assistant side).

**Done when.** The behaviour-test register is at 100 %, the named
families are green, and no ◐ remains in the comparison's cluster section
for a cluster with a hand-written matter.js server.

**Status 2026-10-19.** Phase 17 planned: [concept v5](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v5-behaviour-depth.md) (German) with the owner's decisions 0.1–0.4 — a real Switch server after `SwitchServer.ts` beside the host-gesture path, the full Thermostat (presets, atomic writes, occupancy) and DoorLock (users, credentials, schedules) scope as selectable features, four waves in two parallel pairs (BJ ∥ BK, BL ∥ BM) as `v0.8.1`–`v0.8.4`, starting after `v0.8.0`. Phase 17a (brief BJ, pull request 170) released as `v0.8.1`: the behaviour-test ports of OnOff, LevelControl, ColorControl, Identify, WindowCovering and ServiceArea — 66 of 76 cases, three fixes the ports found (window-covering enum conformance at construction, the level-control quiesce, the colour-temperature coupling floor), `fabrictest.Endpoint`; Phase 17b (brief BK, pull request 171) released as `v0.8.2`: `cluster/switch`, the port of `SwitchServer.ts` with matter.js's position input beside the kept gesture path, the reference daemon's button on positions, SwitchServerTest.ts 69 of 69 cases, TC-SWTCH-2.5 passing; Phase 17c (brief BL, pull request 175) released as `v0.8.3`: the Thermostat's Presets, atomic writes and Occupancy after `ThermostatServer.ts`, `AtomicWriteHandler.ts` and `AtomicWriteState.ts`, the seven thermostat test files 70 of 82 cases, presets persisted on the store (migration v5), KeypadLockout persistence, the reference daemon's thermostat in the shape of matter.js's CHIP test instance with TC-TSTAT-4.2 passing; Phase 17d (brief BM, pull request 176) released as `v0.8.4`: the DoorLock server after `DoorLockServer.ts`, `LockAuth.ts` and `LockSchedule.ts` with every feature selectable (PIN, RID, FGP, COTA, USR, WDSCH, YDSCH, HDSCH, DPS, UBOLT), users, credentials and schedules persisted on the store (migration v6, credential data encrypted at rest), the DRLK families against the reference daemon's lock. Phase 17 is complete. Phase 18 planned: [concept v5](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v5-behaviour-depth.md) Teil E with the owner's decisions 18.1–18.3 — Opus implements all four waves, Fable reads every cited fix line of the security servers against the pin before the merge; `AccessControlCustomExtensionTest.ts` deferred to milestone 4; four sequential waves `v0.8.5`–`v0.8.8` (18a Descriptor, GeneralDiagnostics, BasicInformation, Groups, ScenesManagement and the protocol group tests; 18b AccessControl, OperationalCredentials, GroupKeyManagement and the system commissioning tests; 18c Groupcast; 18d Binding); Phase 18a (brief BN, pull request 182) released as `v0.8.5`: the ports of Descriptor, GeneralDiagnostics, BasicInformation, Groups, ScenesManagement and the protocol group tests, 38 of 47 open cases, four fixes (the bridge raises the node's ConfigurationVersion once for a grouped change, the operational time no longer wraps on a backward clock step, GeneralDiagnostics feature selection with the DataModelTest check, Descriptor TagList opt-in with matter.js's tag rules); Phase 18b (brief BO, pull request 184) released as `v0.8.6`: the ports of AccessControl Auxiliary, OperationalCredentials, GroupKeyManagement and the system commissioning tests, 30 of 42 open cases, two fixes (SetVIDVerificationStatement refuses vendor ID 0, ArmFailSafe(0) leaves the Breadcrumb to the fail-safe's expiry), the node's MaxPathsPerInvoke, CapabilityMinima, GeneralDiagnostics feature selection and grouped ConfigurationVersion, the GeneralCommissioning and AdministratorCommissioning audit (seven findings); the rotated-IPK gap (key set 0) is the owner's `secure/` change in 18c; Phase 18c (brief BP, pull request 186) released as `v0.8.7`: the Groupcast port, 49 of 50 open cases, on a multicast model of `fabrictest` (memberships, delivery, failure injection, the join seam in `transport/udp`), two fixes (JoinGroup binds the group's own address first, one log line per join-failure streak), `MaxMcastAddrCount` as an option, and the Identity Protection Key set held as key set 0 so a rotated IPK reaches CASE (the responder under every key of key set 0, the initiator on the second-newest); Phase 18d (brief BQ, pull request 189) released as `v0.8.8`: the Binding port, 41 of 47 open cases (the three default-subscription cases wait for a node client stack on `fabrictest`, a phase-19 harness commit), group sends on the multicast model (`SendMulticast` through the socket seam, `Topology.Prepare`), two fixes (a group entry unregistered while it resolves is not established, a group invoke logs as it leaves). Phase 18 is complete. Phase 19 planned: [concept v5](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v5-behaviour-depth.md) Teil F with the owner's decisions 19.1–19.4 (2026-10-20) — one wave (brief BR), ICD to milestone 5 phase 23, WebRTC and Thread diagnostics to milestone 6 phase 26 (`until`, not `excluded`: phase 26 builds both; corrected at the `v0.8.8` cut), the 23 runtime test files classified (40 cases into phase 19, the validation and endpoint-model files to milestone 4, discovery to milestone 5, DCL to milestone 6), "100 %" defined as no `planned` left under `packages/node/test/behaviors` with every `missing` reasoned. Phase 19 (brief BR, pull request 191) released as `v0.9.0`: 75 of 159 open cases ported (OTA on two `fabrictest` devices of one site with the node client stack, the small servers, the open partial cases, the four controller files against `controller/client` and the reference controller's window helpers), the other 84 `missing` with their class, the planned ratchet 52 → 37, five fixes; 29 `OtaTest.ts` cases wait for the controller-side SoftwareUpdateManager (milestone 5 phase 25). Phase 19 is complete. **Milestone 3 closed 2026-10-20 with `v0.9.0`** (the last wave carries the minor, as `v0.8.0` did): no `planned` is left under `packages/node/test/behaviors` except the `until` entries, the families are green, and the ◐ rows left in the comparison's cluster section are ICDManagement (milestone 5 phase 23, decision 19.2), Thread diagnostics (milestone 6 phase 26) and Joint Fabric (excluded).

### Milestone 4: The embedding layer

**Detail:** [concept v6](./history/concept-v6-embedding-layer.md) (German), written
2026-10-20 when the milestone opened, with the owner's decisions 0.1–0.4 and
the drafts of the go-ha-matter-bridge and adapter-module concepts (Teil B/C),
because the three shape each other; the two drafts move to their own
repositories in phase 22.

| Phase | Content | Proof |
| --- | --- | --- |
| 20 | the `node/` facade complete: everything loom reaches below it today (`bridge`, `store`, `secure/*`, `mdns`, `im`) is reachable through `node/`, `device/` and `contract/`, or is documented as an advanced seam; the **declarative device kit** — a bridged device from a device type, its values and its command handlers in one declaration, with attribute change propagation, transitions and the device-type validator behind it; an `apidiff` guard from this milestone on, so that a break is announced before it ships | loom's import list shrinks to the facade; the reference bridge is rewritten on the kit |
| 21 | observability and persistence as ports: `slog` attribute conventions, a metrics hook, the diagnostic event ring; the `Store` seam proved by a second implementation (in-memory, for tests and small daemons); hardening a daemon in the field needs (session and subscription limits documented, PASE attempt limit after §5.5, fuzz targets for the controller decoders) | the persistence and concurrency guides describe the ports; `make fuzz` covers the new targets |
| 22 | the cookbook and the operations page: the "embed in a daemon" guide written against a `go-*2mqtt` project, `operations.md` (what a host daemon minds when it runs a node); the concept documents of go-ha2fabric and go-hamapping in their own repositories. Container image, systemd unit and arm64 build left this milestone on 2026-10-24 (concept v7 decision 0.9): they belong to `go-fabric-server`, the stand-alone daemon with a remote protocol planned after milestone 4, which also carries the item "a bridge operable out of the box" | a 2mqtt daemon exposes one device type end to end on the kit |

**Value.** The diamond-standard claim lives here, and it is a claim about
embedding and operation, not about Matter semantics: **ahead of matter.js
in embedding and operation, equal in semantics** (owner, 2026-10-19).
matter.js asks a host to learn behaviours, agents and transactions; this
module should ask for a device type and a handful of callbacks, and ship
as one static binary with a small footprint. On the wire it is a port of
matter.js and can be as correct as its source, not more; the ecosystem
exceptions stay matter.js's. Consumers: every one; the HA bridge and the
2mqtt family cannot start before it.

**Done when.** The comparison's "device model" and "runtime" sections have
no ◐ left except the behaviour-layer idiom row; loom and one 2mqtt daemon
build on the facade alone.

**Status 2026-10-20.** Phase 20 planned: concept v6 Teil A — a facade route or a documented advanced seam for every symbol loom reaches below `node/` today, the generated typed cluster layer (`State`, `Patch`, typed `OnChanged` and command handlers per cluster) and the declaration form of the device kit, the reference bridge rewritten on it, the apidiff guard on `golang.org/x/exp/cmd/apidiff` against the last tag; four waves (BS ∥ BT, then BU, BV) as `v0.9.1`–`v0.9.4`, the twelve `until: M4` test files ported where their subject exists. Phase 20a (brief BS, pull request 197) released as `v0.9.1`: a facade route or a documented advanced seam (`docs/guides/advanced-seams.md`) for every symbol loom reaches below `node/`, the migration guide (`docs/guides/migrating-to-the-facade.md`), the reference daemon on the routes; with it the archcheck guard (brief BW, pull request 196) and ADR 0025 / ADR 0026. Versions (owner, 2026-10-20): phase 20b (the typed cluster layer, an accessor break) and 20c land together as `v0.10.0`, milestone 4 closes with `v0.11.0`, and the later milestones shift by one minor in the table above.

**Status 2026-10-22.** Phase 20 complete, released as `v0.10.0`: phase 20b (brief BT, pull request 198) — the typed cluster layer, a typed server over every cluster server (`State`, `Patch`, `Set`, `Get`, `OnChanged`, `OnWrite`, `Handle<Command>`, `Emit<Event>`), every device accessor returning it; the first architecture gate (report G1, pull requests 200/201, ADR 0026 amended) with its fix waves BX1–BX4 (pull requests 202–206, 205: the kit's defaults are the self-held servers, one request decoder, one shape over every server, goroutine ownership and locks, the sweeps, the client machinery under `client/`); phase 20c (brief BU, pull requests 208/209, 213) — the declaration `<cluster>def.Declare` with its typed host ports and `device.FromSpec`, the lifecycle (`SetReachable`, `Rename`, `Replace`, `EndpointNumber`, `Node.Topology`), callbacks inline with the `…Async` variant on one goroutine per device, `contract.Change` with its origin, the reference bridge and `examples/host-view` on the declaration; the deprecated surface removed under the suspended window (brief BZ, pull requests 211/212; owner decision 2026-10-21, README §API stability); the second gate over the whole facade including `controller/` (report G2, pull request 215: 16 findings, a context-free consumer's bridge and controller) with its fix waves BY-1 (pull request 216: LevelControl's change path and the written setpoint reach the host; the host view and the wire view of a change) and BY-2 (pull request 218: docs a consumer can read with a citation guard, one shape for the Async hook, `schema.DeviceTypeID`, typed refusal statuses, `State` from the generator, the lock sites, one DataVersion per write, the `forbidigo` and error-prefix guards, the flake sweep). Left for BV after the tag: the typed controller reads and writes with owned aliases in `controller/client` and `fabrictest`, the node-feature routes the reference bridge still imports, `apidiff`. Phases 21 and 22 next; milestone 4 closes with `v0.11.0`. The reference consumer's migration against this tag: openccu-loom issue 995. `v0.10.1` (2026-10-22, brief BV-0, pull request 226) answers the six findings that migration filed (issues 220–225): the 32-bit build with a CI cross-compile job, `device.HostServer(srv)` and `device.Measurement(src)` for a data-driven host, `NativeMover` on the generated request, the validator's route through `secure/setup` and `secure/spake2`, three guide gaps. `v0.10.2` (2026-10-23, brief BV-1, pull request 233) closes the gaps that kept the reference consumer off a declaration-only device model (issues 228–232): `levelcontroldef.Declare.Ramp` for a device that ramps by itself, the value helpers moved into the generated `<cluster>def` packages (a source change for a host that imports them), `closurecontroldef.StopObserver`, and the Aggregator's partitions with `RetirePartition` and `Batch`. `v0.10.3` (2026-10-23, phase 20d, briefs BV-a and BV-b1, pull requests 240 and 243): the controller's typed surface (`spec.Value`, `<cluster>def.StateFrom`, `fabrictest.ReadCluster`, owned aliases in `controller/client` and `fabrictest`), `controller.New` as the role's composition root, the `apidiff` guard as a tool dependency in `make ci`, the report-exchange ack fix, and the host statements of a bound device (`Device.Update`, `device.ComposeNodeLabel`; issues 238, 239). `v0.10.4` (2026-10-24, brief BV-b, pull request 245): the node-feature routes the reference bridge still imported (OTA, DCL, Actions, EcosystemInformation, the root servers a host drives after Start), `device.FixedLabel`, ContentControl by declaration, a declared PowerSource's EndpointList and defaults, and the `until: M4` test ports; two owner decisions follow it (2026-10-23): the typed declaration accepts the Base device type's optional clusters (phase 21, wave CB, `device.FixedLabel` goes with it), and the eight test ports whose subject is M5's are still ported in milestone 4, as a wave of their own before `v0.11.0`. Phase 21a (brief CA, pull request 247) released as `v0.10.5` (2026-10-24): the persistence port `store.Port` (one interface per area), `store/memstore` and `endpoint/memstore` as the second implementation, the contract table `store/storetest`, `fabrictest` on the in-memory stores by default, `node.Config.Store.Credentials` on the port (a break under the suspended window, migration guide); the comparison row "Pluggable storage" is ✅. Phase 21b (brief CB, pull request 250) released as `v0.10.6` (2026-10-24): the metrics port `contract.Metrics` with its named set and `node/metrics/expvar`, the diagnostic trace on the facade (`node.Config.Diagnostics`, `Node.DiagnosticEvents`, `OnDiagnosticEvent`), one log-key table with the guard `script/slogkeys` and the observability guide, and the Base device type's optional clusters (FixedLabel, UserLabel) by typed declaration on every device type, `device.FixedLabel` removed. Phase 21c (brief CC, pull request 252) released as `v0.10.7` (2026-10-24): the limits page with its value guard, `node.Config.Limits` for the capacities matter.js keeps configurable, 16 fuzz targets for the controller side's decoders and the `otaimage.ReadHeader` allocation fix they found; the hardening item of phase 21 is done. Phase 22 narrowed the same day (concept v7 decision 0.9): the container image and systemd unit leave this milestone for `go-fabric-server`. Phase 21d (brief CT, pull request 254, owner decision 0.8) released as `v0.10.8` (2026-10-24): the eight test ports whose subject exists in part (`MarkChangedTest`, `ValuePatcherTest`, `conformanceTest`, `EndpointNumberConflictTest`, `ProtocolServiceTest`, `ConstructionValidationTest`, `RuntimeValidationTest`, `StandardDeviceTypeCorpusTest`; the planned count 35 → 27) and the hooks they needed: `spec.Server.MarkChanged`, defaults per feature set, field conformance on struct and command fields, a strict validator that refuses only what an addition causes, and the unimplemented mandatory command as a validator verdict. What stays open is in the findings register (field conformance on the host's own values, request constraints on the wire). Phase 22 (briefs CE and CF, pull requests 258 and 256) released as `v0.10.9` (2026-10-24): the cookbook [Embed in a daemon](https://github.com/SukramJ/go-fabric/blob/main/docs/guides/embed-in-a-daemon.md) written against the go-zendure2mqtt pull request that exposes each Zendure unit as a bridged Electrical Sensor with its battery (the phase-22 proof), the operations page, the two facade fixes the cookbook found (PowerSource by declaration on every bridged device; the PowerSource server forwards its source's changes), and the concept repositories go-ha2fabric and go-hamapping. Built with Go 1.27.3 (pull request 257). The flake sweep (brief CG, pull request 262, [ADR 0028](./adr/0028-site-paces-by-counted-work.md)) released as `v0.11.0` (2026-10-25): paced code holds no wall-clock deadline (`clock.WithTimeout`, a guard), the stack counts its in-flight work (`internal/inflight`) and `fabrictest` judges quiet by it, `Site.Step`, `make stress` / `make stress-flakes` with a nightly job, nine named flakes at 0/50 under stress twice. **Milestone 4 is closed with `v0.11.0`:** a bridge is `node/` + `device/` + `contract/`, the persistence and metrics ports exist with second implementations, the limits and operations pages hold, the cookbook embeds the kit in a real daemon, the concept repositories go-ha2fabric and go-hamapping exist, and `go-fabric-server` is concept v8's product. What left the milestone: the container image and systemd unit (decision 0.9, now `go-fabric-server`'s).

### Milestone 5: ICD and the controller remainder

| Phase | Content | Proof |
| --- | --- | --- |
| 23 | the ICD server (`IcdManagementServer.ts` 838 + `IcdMode.ts` 152: SIT / LIT, the check-in sender, `UserActiveModeTrigger`, the registration table), mounted where the device type asks for it; the check-in protocol (`protocol/src/icd`, 1 162) | ICDM and ICDB families; `IcdManagementServerTest` (1 415) and the eight `protocol/test/icd` files ported |
| 24 | the controller side: `IcdClient` (1 140), the check-in receiver (`FabricIcd.processCheckIn`), peer wakefulness, the sustained subscription parking on a LIT peer and re-subscribing when it wakes (`SustainedSubscription.ts` 672 / test 1 712), `RebootResubscribeArmer` | the module's controller holds a subscription to the module's own LIT node across its idle cycles |
| 25 | the controller remainder: the rest of OTA push from the controller (`ota serve --announce` sends `AnnounceOTAProvider` today; writing `DefaultOTAProviders` and the provider's per-node consent record do not exist), the TCP initiator in `controller/`, the Thread border-router client (`packages/thread-br-client`: discovery, dataset codec, MeshCoP / OTBR diagnostics) if phase 12 chose it as the dataset source | `ClientNodeTcpTest`, the `peer/` tests ported; the reference controller updates the reference daemon over the air |

**Value.** The battery sensor, lock or button commissioned over BLE in
milestone 1 is an ICD; without this milestone the controller loses it at
its first idle period. Consumers: the controller role; the device side of
loom and the HA bridge is mains-powered and gains the server only for
completeness.

**Done when.** The comparison's ICD and CASE-initiator rows are ✅; the
ICDM / ICDB families are green or classified; issues #273 and #274
closed.

**Status 2026-10-25.** Phases 23 and 24 are one bundle, released as
`v0.12.0` (owner decision 2026-10-24: one release per bundle; phase 25
follows as `v0.13.0`). Phase 23 (brief CH): `cluster/core.ICDManagement`
after `IcdManagementServer.ts` and `IcdMode.ts`, the `icd` package after
`protocol/src/icd` (counter, back-off, suppression, registrations, sender,
the Check-In codec in `icd/checkin`), `store.ICD` (migration v7), the
facade's `node.Config.ICD` and `Node.ICD()`. Phase 24: the Check-In
receive path, `controller/client.ICDClient` after `IcdClient.ts`, the hold
of a sleeping LIT peer, the sustained subscription's ICD branches,
`client/peer.RebootResubscribeArmer`; the end-to-end proof holds a
subscription to the module's own LIT node across three idle cycles
(`controller/client/icd_e2e_test.go`). The comparison's ICD rows are ✅;
ICDM and ICDB are classified as not applicable to the reference daemon,
which is mains-powered and sets no `Config.ICD`, and applicable to a host
that sets it. The [ICD guide](./guides/icd.md) is the host's view.
Milestone 5 also closes issues #274 (the Ahead-of-matter.js marker check,
wave CN) and #273 (wave CO, one pull request after phase 25; owner decisions
2026-10-25 and 2026-10-26).

**Status 2026-10-26.** Phase 25 (brief CM) is one bundle for `v0.13.0`:
the OTA push from the controller (`controller/softwareupdate` after
`SoftwareUpdateManager.ts`, `OtaAnnouncements.ts` and the
`DclOtaUpdateService` store; the provider on the controller's own node,
composed by `node/controllernode`; the reference controller's `ota serve`
updates the reference daemon over the air), the TCP initiator
(`client/peer`, `client/exchange`; `ClientNodeTcpTest`), the network
profiles and MRP margins (`client/peer.NetworkProfiles`;
`NetworkProfileTest`, `MRPTest`), the `_matterd._udp` commissioner
advertisement, the parked register items (`Interaction.Close` and the
abandoned subscribe, write chunking across messages, `includeKnownVersions`
on a subscription, `Cluster.Supports` by conformance, nodes kept when they
fail to load, `Registry.CompleteCommissioning`, the payload identity
check) and the `MarkComplete` break. The four controller-discovery test
files and `ReportedSessionParametersTest` move to milestone 6 phase 27
with their reasons. The Thread border-router client is milestone 6 phase
26a ([ADR 0027](./adr/0027-thread-border-router-over-otbr-rest.md)).
Wave CO (brief CO, pull request 287) released as `v0.13.3` (2026-10-26): the Thermostat serves `TemperatureSetpointHold` (with its duration) and `ThermostatRunningState`, the hold reaching the host through `Sink.SetTemperatureSetpointHold`; MatterScheduleConfiguration ahead of matter.js after connectedhomeip (`SchedulePort`, `SetActiveScheduleRequest`, the Schedules atomic write, the first row of the Ahead-of-matter.js register with a chip source), the five multi-attribute `AtomicWriteHandlerTest` cases ported; hosts serve MSCH behind a flag until the owner's live pairing with Apple Home is verified. TEVT (the seven Thermostat events) is a milestone 6 candidate. **Milestone 5 is closed with `v0.13.3`.** Owner decisions 2026-10-26 at the CM review: the invoke batcher is decided with the milestone 6 concept; the snapshot extractor gains the "L" (large message) quality in a small pull request after `v0.13.0`; the controller's own node stays behind the `controller.OwnNode` port with no layering exception.

### Milestone 6: Completeness

| Phase | Content | Proof |
| --- | --- | --- |
| 26 | the 15 clusters matter.js serves as generated stubs, through the generated default server: Camera (7), TLS (2), Thread and network infrastructure (4), AmbientContextSensing, WaterTankLevelMonitoring; `WebRtcTransportRequestor` with its logic (224 / 605) as the one camera-side server matter.js hand-writes; the 12 device types (Camera, SnapshotCamera, FloodlightCamera, CameraController, VideoDoorbell, AudioDoorbell, Intercom, Chime, CastingVideoClient, ThreadBorderRouter, NetworkInfrastructureManager, …) | `chipdm-check` green; every snapshot device type in a devicegen list |
| 26a | a Thread Border Router over OTBR REST ([ADR 0027](./adr/0027-thread-border-router-over-otbr-rest.md), accepted 2026-10-25): the dataset codec moves to `thread/` (layer `foundation`, `controller/thread` forwards until `v0.12.0`); `cluster/threadbr` with ThreadBorderRouterManagement (chip's rules, `ThreadBorderRouterManagementCluster.cpp` at `6170af84`, line-cited; a `by_design` entry for logic matter.js's stub does not carry) and ThreadNetworkDiagnostics (the mandatory set plus `MLECNT` / `MACCNT`); the Thread variant of NetworkCommissioning next to the Wi-Fi one, `ScanNetworks` answering the border router's own network from the active dataset; the adapter `thread/otbr` over stdlib `net/http` after `@matter/thread-br-client`'s `OtbrRestClient.ts`, no new dependency, MeshCoP out; the reference daemon's optional `--otbr` flag adds one `ThreadBorderRouter` endpoint | unit tests one per cited chip rule; adapter tests against recorded OTBR REST fixtures in both key formats; the harness's `TBRM` family (`Test_TC_TBRM_2_1` … `2_4`, `3_1_Simulated`) against the daemon with a virtual OTBR built as chip's CI builds it (`otbr-agent` with the simulation RCP), manually dispatched first; a real OTBR in the manual test plan |
| 27 | the comparison swept: every remaining ◐ / ○ row is ✅ or a `by_design` entry; the findings register empty (the controller remainder of phase 25 included: the invoke batcher, the controller's `Discovery` shape, reported session parameters on the device, the device-side BDX MRP delay); the test-porting register at 100 % for `packages/node/test` (endpoint, validation, storage) | the three registers agree; one spot-check run of the CSA Test Harness (`project-chip/certification-tool`) against the reference daemon, report under `notes/` (owner, 2026-10-21; the harness stays outside the guards, see `docs/certifiability.md`) |

Excluded by decision, restated so they are not re-opened by accident:
**ARL / managed aggregator** (no
matter.js logic exists, `BD-Matter-ARL-NotMounted`), the **Thread radio**
(hardware; the border-router *client* over OTBR REST is phase 26a,
[ADR 0027](./adr/0027-thread-border-router-over-otbr-rest.md)), **certification itself** ([ADR 0011](./adr/0011-certifiability-is-a-goal.md)).

**Value.** The claim in the first sentence of this page becomes checkable:
135/135, 91/91, no row of the comparison older than its milestone. No
consumer asked for a camera, which is why this is milestone 6 and not 2.

**Status 2026-10-27.** [Concept v10](https://github.com/SukramJ/go-fabric/blob/main/docs/history/concept-v10-completeness.md) (German, in the history since the milestone closed) plans the milestone from the read map of 2026-10-26: the 17 clusters of phase 26 already have generated servers (brief CL, 2026-10-25); matter.js carries behaviour for `WebRtcTransportRequestor` only, connectedhomeip for 15 of the 17, none for WaterTankLevelMonitoring. Waves: 26-A1 camera signalling (`WebRtcTransportRequestor` after matter.js, Chime, CameraAvSettingsUserLevelManagement) → 26-A2 streams (CameraAvStreamManagement, ZoneManagement) → 26-A3 transport (TLS, WebRtcTransportProvider, PushAvStreamTransport), in parallel 26-B network infrastructure (WiFiNetworkManagement, ThreadNetworkDirectory, the provisional ThreadNetworkDiagnostics attributes) → 26a Thread Border Router (ADR 0027) → 26-C AmbientContextSensing (WaterTankLevelMonitoring stays generated, a `by_design` entry) → 26-D Joint Fabric after connectedhomeip (owner decision 2026-10-27: stage 6c of ADR 0020 is pulled into this milestone) → 27-A the controller remainder (the invoke batcher — owner decision 2026-10-27: implemented after matter.js —, the `Discovery` shape, the DCL vendor information service, NOC rotation with the PQC phase 1 sizes) → 27-B the device remainder (reported session parameters, the BDX MRP delay, TEVT, composite device types in `devicegen`, TimeSynchronization's optional features after connectedhomeip, AirQuality's feature-gated values) → 27-C the sweep. An audit of every generated server against connectedhomeip (2026-10-27) found no further rules to port: `BD-Matter-GeneratedServerSuffices` lists the clusters that are complete by the model alone. Every chip-sourced server is a row of the Ahead-of-matter.js register with a host port and the chip unit tests as parity cases; camera clusters stay out of host devices until a live pairing with Apple Home passes. One release per wave, `v0.13.4` onwards; **milestone 6 closes with `v0.14.0`.**

**Status 2026-10-28.** Wave 26-D implements Joint Fabric after connectedhomeip, ahead of matter.js (ADR 0020 stage 6c): the JointFabricDatastore and JointFabricAdministrator servers in `cluster/jointfabric` behind the host ports `DatastoreSync`, `DatastoreStore`, `AdministratorPort` and `CommissionerReach`, the shared trust verification in `client/jcm`, and the commissioner's Joint Commissioning step with the host's consent ([Joint Fabric guide](guides/joint-fabric.md)); chip's 62 unit tests ported, 58 by meaning and 4 C++ lifetime cases replaced by a context test.

**Status 2026-10-28: closed.** Every wave landed with its release:

| Phase | Wave | Content | Release |
| --- | --- | --- | --- |
| 26 | 26-B | WiFiNetworkManagement, ThreadNetworkDirectory, the provisional ThreadNetworkDiagnostics attributes | `v0.13.4` |
| 26 | 26-A1 | camera signalling: WebRtcTransportRequestor, Chime, CameraAvSettingsUserLevelManagement | `v0.13.6` |
| 26 | 26-A2 | camera streams: CameraAvStreamManagement, ZoneManagement | `v0.13.7` |
| 26a | 26a | the Thread Border Router over OTBR REST (ADR 0027) | `v0.13.7` |
| 26 | 26-C | AmbientContextSensing; WaterTankLevelMonitoring stays generated | `v0.13.8` |
| 26 | 26-A3 | camera transport: the TLS clusters, WebRtcTransportProvider, PushAvStreamTransport | `v0.13.9` |
| 26 | 26-D | Joint Fabric (ADR 0020 stage 6c) | `v0.13.10` |
| 27 | 27-A | the controller remainder: the invoke batcher, discovery sessions, DCL vendor information, NOC rotation, PQC phase 1 | `v0.13.11` |
| 27 | 27-B | the device remainder: session intervals, the BDX MRP delay, TEVT, the composite device types, TimeSynchronization's features, AirQuality's feature-gated values | `v0.13.12` |
| 27 | #326 | the peer's reported session parameters on both ends | `v0.14.0` |
| 27 | 27-C | the sweep: the comparison, the findings, test and Ahead-of-matter.js registers, this page and the certifiability page | `v0.14.0` |

The snapshot's 135 clusters each have a server; 90 of its 91 device types
are generated (CastingVideoClient stays excluded,
`script/devicegen/devicetypes.go`). What the sweep left open is in
[report 27-C](https://github.com/SukramJ/go-fabric/blob/main/notes/briefs/reports/27C.md):
the rows the comparison keeps with their decisions, the findings that
outlived their milestone tags, and the owner's hand tests (group multicast
against a real controller, the ESPHome proxy live test, the Apple Home
camera pairing) and the CSA Test Harness spot check of phase 27, which
stay open. **Milestone 6 is closed with `v0.14.0`.**

### Milestone 7: BLE on the device side

| Phase | Content | Proof |
| --- | --- | --- |
| 28 | the peripheral role of BTP (shared with milestone 1's central), `ble/bluez` as GATT peripheral, the commissionable advertisement over BLE, `NetworkCommissioning` with the Wi-Fi feature on a Linux host (the `wpa_supplicant` driver behind a host port) | the reference daemon on a Pi without a cable is commissioned by the reference controller and by Apple Home over BLE |

**Value.** matter.js's `nodejs-ble` node does it, so by the premise this
module does too; the owner's decision of 2026-10-17 reopens ADR 0024's
device-side exclusion as a late milestone. Consumers: a future Go device
without a network at first boot — none of the current bridges.

**Done when.** The comparison's BLE row is ✅ for both roles; the DD and
SC BLE cases run.

## Standing work, outside the milestones

- **Pin lifts** open every milestone (above).
- **The chip harness** keeps running on every wave that moves the
  daemon's composition or a FeatureMap; a wave that does not is said so in
  its report.
- **The findings register** takes every gap an audit finds; closing one
  removes it. Ecosystem findings go to the observations page.
- **Google Home and Alexa** runs happen when the owner is at the device;
  their findings enter the register and may re-order a milestone's phases,
  not the milestones.
- **Security paths** (`secure/`, `commissioning/`, ACL, the four
  hand-written core servers) are reviewed by the strongest model whatever
  the milestone, as `CLAUDE.md` assigns them.

## How a phase is planned

A phase gets its detail document when it is next, not before: German,
under `docs/` while it is current (as concept v3), moved to
`docs/history/` when its milestone closes. It names the matter.js files
at the pin with line ranges, the Go packages it touches, the decisions it
needs from the owner (asked as a wizard with a recommendation), the waves
with one agent each, and the three pillars it has to show. An architecture
decision or a deliberate divergence inside it becomes an ADR or a
`by_design` entry in the same pull request. The adapter module and the
Home Assistant bridge got their own concept documents in their own
repositories on 2026-10-24 (wave CF): [go-hamapping](https://github.com/SukramJ/go-hamapping)
and [go-ha2fabric](https://github.com/SukramJ/go-ha2fabric).
