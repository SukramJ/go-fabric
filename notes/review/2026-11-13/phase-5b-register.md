# Phase 5b — the dispositions

The commit ids below are those of the branch of this phase's second pull
request — "the phase 5b pull request" where a number would stand
(`review2/p5b`, `7c15bb7a..fa118739`, 262 commits). If that pull request
merges squashed, as the earlier phases' did, its single commits stay
retrievable through the pull request's reference. Items that describe a
weakness a party on the network could use are held back with their text until
the pull request that fixes them merges (owner decision 25); the table carries
their id and phase only, and "Counts" says how many. What this branch fixed is
public as what the code does now.

Phase 5 is cut in two halves with a pull request each (owner decision 91).
This is 5b: the API cut, the port extensions, the peer path, the generated
redaction and wave H. Seven review units read it — the node facade
(`apinode`), the device-side contract (`apidev`), `cluster/spec` (`apispec`),
the write seam (`write`), the store and driver ports (`port`), the peer path
(`peer`) and the generated redaction (`redact`). Two verifiers judged the
first five units' findings (`api`, `data`), and a confirming read judged the
reference halves the verifiers left and the design facts of the peer and
redaction notes. The owner answered ten of the units' 30 questions in a wizard
(decisions 107 to 115), the coordinator the other twenty. Nine waves carried
the fixes, with eleven fix agents: F, the store port and the controller's
attested pair (`fstore`, `fctl`); H, the wire (`wire`); DCL, the ledger
downloads (`dcltrust`); S, sealing at rest (`seal`); A, `spec.Server` and the
generated fabric scope (`aspec`, `aredact`); A2, the facade (`afacade`); B,
the peer path (`peer`); G-DD, the daemon's commissioning flows (`gdd`); C, the
write verdict (`write`). An independent review read the integrated diff in
four slices, two agents reworked it (`rwa`, `rwb`), and the rework had a read
of its own. Reference reads judged, before the code and after each wave's
rounds, the statements about matter.js and connectedhomeip the waves wrote and
the security paths they touched. Which model did which part is the
coordinator's account (CLAUDE.md, "Which model does which work"); the result
file names none.

One row per finding of four reads — the seven units (90 findings), the diff
review (29), the review of the rework (9) and the reference reads (112 items)
— one per item of earlier phases a unit was given or phase 5a's register
placed in 5b, one per question of the units, one per owner decision of this
phase, one per item the fix agents, the guard runs and the coordinator noticed
and did not fix where it was noticed, and one per review for what it did not
read. Sources: the result file of the review workflow, the units' and the
design notes, the coordinator's open list, the wizard's briefs, the thirteen
fix notes, the four reports of the diff review, the rework's read, the
reference reads, the owner's decisions and `git log 7c15bb7a..fa118739`. The
notes under `.git/` are not part of this change. The guards of the branch are
in the pull request's body, not here.

How a row was placed — by rule:

- **fixed `<commit>`** — the commit is in `7c15bb7a..fa118739`, and its
  message names the ID or the fix note gives it. The fix notes carry the ids
  the commits had in the agents' worktrees; the branch's commit was found by
  the ID or the subject. What a fixed row says holds now is the commit's
  subject or the fix note's line, written short, and was not checked against
  the code for this file. "corrected by" names the item of a later read that
  found the fix wanting and the commit that repaired it. **fixed in part**
  names what stays after a colon or a semicolon.
- **parity: a recorded divergence** — the behaviour is the module's by
  decision, and the row names the `by_design` entry that records it.
- **decided** — the owner's or the coordinator's answer, where no code change
  was asked.
- **struck** — refuted, judged equivalent or nothing to fix; **not a finding**
  — an item of a read that asks for no change; **closed** — measured settled
  by a later run. The reason is the reader's, the fix note's or the
  coordinator's, as the row says.
- **open: findings row `F-db021da0-NN`** — the gap is a row of
  `notes/parity/matter_behaviour_findings.md`, opened on this branch unless
  the row says otherwise.
- **phase 6** — the full pass, with what 5b leaves. The placement is the
  coordinator's list's or the fix note's where one of them names it, the
  register editor's otherwise, and then the row says so.
- **held back** — the item tells a party on the network what it could use, or
  it was held back in an earlier phase's table; held back by the coordinator's
  list or, where "Not placed, not verified" says so, by the register editor's
  judgement.

Severity is the confirming read's where it judged a finding, else the
verifier's, with the reviewer's in brackets where it differs (78 of the 90
unit findings were verified or confirmed; lowered 11 times, raised 0 times);
the findings of the peer and redaction units that the confirming read did not
judge, and the four notes no verifier took, carry the reviewer's. The diff
review, the rework's read and the reference reads had no verifier; their rows
carry the reader's severity, and "—" where a fact note gives none. The
reference reads' ids are this register's: `5b-<read> <item>`, the item as the
read numbers it. A reference statement in a row is its source's — a commit
subject's or a read's — and was not re-read for this file.

## The findings

### The seven review units

The units read the tree at `7c15bb7a`. Two findings of two units are one
defect (R2-P5-apidev-07 = R2-P5-apispec-07; R2-P5-apinode-11 = R2-P5-port-16);
each keeps its row and names the other.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| R2-P5-apinode-01 | medium | fixed `81ccebca` | `node/node.go` — WindowOnDemand, CommissioningFlow, the full pairing hint bitmap; window calls refused before Start; Start, FactoryReset and Run bounded |
| R2-P5-apinode-02 | medium | fixed `81ccebca` | `node/credentials.go` — WindowOnDemand, CommissioningFlow, the full pairing hint bitmap; window calls refused before Start; Start, FactoryReset and Run bounded |
| R2-P5-apinode-03 | low | fixed `81ccebca`, corrected by 5b-afacade F1 and F6 `6fdcf1f6`, `48b932c7`; what chip reads of bits 16 to 22 is `F-db021da0-53` | `mdns/service.go` — WindowOnDemand, CommissioningFlow, the full pairing hint bitmap; window calls refused before Start; Start, FactoryReset and Run bounded (carried: PairingHint width) |
| R2-P5-apinode-04 | low (medium) | fixed `81ccebca` | `node/node.go` — WindowOnDemand, CommissioningFlow, the full pairing hint bitmap; window calls refused before Start; Start, FactoryReset and Run bounded (carried: N-life-5) |
| R2-P5-apinode-05 | low | fixed `81ccebca` | `node/node.go` — WindowOnDemand, CommissioningFlow, the full pairing hint bitmap; window calls refused before Start; Start, FactoryReset and Run bounded |
| R2-P5-apinode-06 | low | fixed `81ccebca` | `node/commissioning.go` — WindowOnDemand, CommissioningFlow, the full pairing hint bitmap; window calls refused before Start; Start, FactoryReset and Run bounded |
| R2-P5-apinode-07 | low (medium) | fixed `b3cff6b6` | `node/doc.go` — the facade's hook handlers in a slot of their own; the bridge installs the live PartsList itself; five unused bridge methods deprecated |
| R2-P5-apinode-08 | low | fixed `0d483f5a` | `node/routes.go` — the store and ledger sentinels, the limits on Limits, Validate refuses pairs one of which would be dropped and a device name over 32 bytes, named reaper durations, the Endpoints override's erase (carried: S-17) |
| R2-P5-apinode-09 | low | fixed `0d483f5a` | `node/factory_reset.go` — the store and ledger sentinels, the limits on Limits, Validate refuses pairs one of which would be dropped and a device name over 32 bytes, named reaper durations, the Endpoints override's erase (carried: N-node2-3) |
| R2-P5-apinode-10 | low | fixed `81ccebca` | `node/config.go` — WindowOnDemand, CommissioningFlow, the full pairing hint bitmap; window calls refused before Start; Start, FactoryReset and Run bounded |
| R2-P5-apinode-11 | medium | fixed `c8c1f660`, `e522bce3`, `25bd26bf` (the coordinator's answer to apinode Q3 and Q-port-3), corrected by DR5b-c2-04 `bfc14b6a` and RW-03 `f27ef213` | `node/persist.go` — one store-error rule for controller-caused endpoint state — endpoint.Persistence and node.EndpointState (carried: W2-api-07) |
| R2-P5-apinode-12 | low | fixed `b3cff6b6` | `bridge/securechannel.go` — the facade's hook handlers in a slot of their own; the bridge installs the live PartsList itself; five unused bridge methods deprecated (carried: W2-api-06) |
| R2-P5-apinode-13 | low | fixed `b3cff6b6` | `bridge/bridge.go` — the facade's hook handlers in a slot of their own; the bridge installs the live PartsList itself; five unused bridge methods deprecated (carried: W2-api-09) |
| R2-P5-apinode-14 | low | fixed `fd8a5ef1` | `controller/controller.go` — a later Close waits for the nodes still stopping; camera Close bounded by its context (carried: Controller.Close second wait) |
| R2-P5-apinode-15 | low | fixed `f0e2ba7a` | `controller/client/network.go` — an error of SetDefaultSubscription always means not replaced (carried: SetDefaultSubscription gap) |
| R2-P5-apinode-16 | note | fixed `0d483f5a` | `node/security.go` — the reaper durations are named; the 30-second sweep and one-minute limit stand |
| R2-P5-apinode-17 | low | fixed `0d483f5a` | `node/config.go` — the store and ledger sentinels, the limits on Limits, Validate refuses pairs one of which would be dropped and a device name over 32 bytes, named reaper durations, the Endpoints override's erase |
| R2-P5-apinode-18 | low | fixed `81ccebca` | `node/node.go` — WindowOnDemand, CommissioningFlow, the full pairing hint bitmap; window calls refused before Start; Start, FactoryReset and Run bounded |
| R2-P5-apinode-19 | low | fixed `fd8a5ef1` | `controller/camera/service.go` — a later Close waits for the nodes still stopping; camera Close bounded by its context (carried: N-life-5) |
| R2-P5-apidev-01 | medium | fixed `dc0913dc` | `contract/matter.go` — the read contract and endpointtest.AssertClusterServerContract, AccessingFabric, the switch-source ports, privilege and status constants, the module's terms; the thermostat's atomic writes through FabricScopedState |
| R2-P5-apidev-02 | medium | phase 6: with `F-db021da0-28` (owner decision 92; wave A2's brief leaves it out) | `endpoint/materialize.go` — A bridged device's BridgedDeviceBasicInformation is not the host's to state |
| R2-P5-apidev-03 | low (medium) | fixed `62a4f36c`, `70f3f154` | `device/type.go` — the accessors return copies; a host-made Type is supported, and New copies it (the coordinator's answer to apidev Q3) (carried: W5-api-18) |
| R2-P5-apidev-04 | low | fixed `b358db05`, `80382f2b`, `91d1f1c2` | `script/clustergen/declare.go` — a typed Declare selects no feature with NoFeatures (the coordinator's answer to apidev Q1) (carried: N4-dec2b-2) |
| R2-P5-apidev-05 | low | fixed `8ef44b44`, `056e82c8` (owner decision 114) | `contract/ble.go` — the GATT peripheral port registers before it advertises; BLEPeripheral and the two BLE fields deprecated; bletest implements it (carried: N-req-2) |
| R2-P5-apidev-06 | low | fixed in part `056e82c8`, `83fd50ec`, `7e641699`: SetMeasuredTemperature applies LocalTemperatureCalibration; the sibling couplings stay `F-db021da0-21`, narrowed (the coordinator's answer to apidev Q4) | `cluster/thermo/thermostat_server.go` — SetMeasuredTemperature applies LocalTemperatureCalibration; docs: the GATT port (carried: F-db021da0-21) |
| R2-P5-apidev-07 | medium | fixed `b358db05`, `3c8a1dfa`, `663368b7` with R2-P5-apispec-07 (one fix) | `contract/matter.go` — a server emits on its own endpoint; the bridge withdraws a removed server's emitter |
| R2-P5-apidev-08 | low | fixed `dc0913dc` | `contract/matter.go` — the read contract and endpointtest.AssertClusterServerContract, AccessingFabric, the switch-source ports, privilege and status constants, the module's terms; the thermostat's atomic writes through FabricScopedState (carried: N-rest3-1) |
| R2-P5-apidev-09 | low | fixed `dc0913dc`, `e1405bbe`, `42b40e68` | `bridge/bridge.go` — the switch-source ports are in contract; a fabric removal drops FabricScopedState on the bridged endpoints' servers |
| R2-P5-apidev-10 | low | fixed `62a4f36c`, `f10c9d13`, `42b40e68`, `7e641699` as narrowed: New refuses what matter.js refuses at initialisation (decision 17, read again by 5b-d17) — 36 pinned entries, 21 bare types and the feature-selected ones, larger than the 19 decision 17 named | `node/served_revision_parity_matterjs_test.go` — the registry unreachable (copies), Spec.Parts, Type.RequiredInitial, the INTENTIONALLY_ENABLED parity, NewRootNode deprecated (carried: N-1) |
| R2-P5-apidev-11 | low (medium) | fixed `62a4f36c` | `device/fromspec.go` — Spec.Parts; FromSpec builds the parts and refuses a missing component as the generated constructor does |
| R2-P5-apidev-12 | low | fixed `62a4f36c`; the row `F-db021da0-23` is closed (`26a09ab6`) | `device/cluster_features_parity_matterjs_test.go` — the INTENTIONALLY_ENABLED parity test holds the default features (carried: F-db021da0-23) |
| R2-P5-apidev-13 | low | fixed `dc0913dc` | `contract/matter.go` — the read contract and endpointtest.AssertClusterServerContract, AccessingFabric, the switch-source ports, privilege and status constants, the module's terms; the thermostat's atomic writes through FabricScopedState |
| R2-P5-apidev-14 | low | fixed `dc0913dc` | `im/fabric_context.go` — the read contract and endpointtest.AssertClusterServerContract, AccessingFabric, the switch-source ports, privilege and status constants, the module's terms; the thermostat's atomic writes through FabricScopedState |
| R2-P5-apidev-15 | low | fixed `dc0913dc` | `contract/matter.go` — the read contract and endpointtest.AssertClusterServerContract, AccessingFabric, the switch-source ports, privilege and status constants, the module's terms; the thermostat's atomic writes through FabricScopedState |
| R2-P5-apidev-16 | note | fixed in part `62a4f36c`: NewRootNode is deprecated; phase 6: the errors of Device.Rename, SetReachable and Update, a signature change (wave A2's brief) | `device/device.go` — the generated device surface's seams |
| R2-P5-apispec-01 | medium (high) | fixed `8809cb84`, `80382f2b` | `cluster/spec/server.go` — the author's write rule in front of the held-value comparison, the host's veto behind it |
| R2-P5-apispec-02 | low (medium) | fixed `8809cb84`, `84158c57`; the row `F-db021da0-40` is closed (`26a09ab6`), the order on the seven self-held servers corrected by 5b-aspec F3 `84158c57` | `cluster/spec/server.go` — the author's write rule in front of the held-value comparison, the host's veto behind it (carried: F-db021da0-40) |
| R2-P5-apispec-03 | medium | fixed `290b7636`, corrected by 5b-aspec F1 `6e1d83b8`; the row `F-db021da0-22` is closed (`26a09ab6`) | `cluster/spec/server.go` — a quieter attribute reports through its throttle, its interval in the definition; a stored one advances its version at the change, a computed one at the emit (carried: F-db021da0-22) |
| R2-P5-apispec-04 | low (medium) | fixed `4634d3dd` | `cluster/spec/server.go` — an accepted command without a handler stays out of the command lists |
| R2-P5-apispec-05 | low | fixed `ec49ca6c` (owner decision 112); the row `F-db021da0-31` is closed (`26a09ab6`), the OnOff recall is `BD-Matter-OnOff-RecallBypassesHandle` | `cluster/levelcontrol/levelcontrol_server.go` — Handle is the whole command method; a host handler's response is checked on every server (carried: F-db021da0-31) |
| R2-P5-apispec-06 | low | fixed `ec49ca6c` | `cluster/spec/selfheld.go` — Handle is the whole command method; a host handler's response is checked on every server |
| R2-P5-apispec-07 | medium | fixed `b358db05`, `3c8a1dfa`, `663368b7` (the coordinator's answer to apispec Q3); one fix with R2-P5-apidev-07 | `cluster/spec/server.go` — a server emits on its own endpoint; the bridge withdraws a removed server's emitter; EmitEvent after the withdrawal answers ErrNotMounted |
| R2-P5-apispec-08 | low | fixed `13f1a70a`, `91d1f1c2` (owner decision 113) | `cluster/spec/model.go` — hold every request to its definition before any handler |
| R2-P5-apispec-09 | low | fixed `13f1a70a` as narrowed (owner decision 113): every request is held to its definition before any handler; whether the check refuses exactly what matter.js's validation refuses stays `F-db021da0-20`, narrowed | `cluster/lock/users.go` — hold every request to its definition before any handler (carried: F-db021da0-20) |
| R2-P5-apispec-10 | low | fixed `b358db05` | `cluster/spec/server.go` — a server emits on its own endpoint; the bridge withdraws a removed server's emitter |
| R2-P5-apispec-11 | low | fixed `bba778ba` | `cluster/core/general_diagnostics.go` — cut a NetworkInterfaces name to the field's max 32 bytes, never inside a rune (carried: S-3) |
| R2-P5-apispec-12 | low | fixed `0d483f5a` | `node/config.go` — Validate refuses an advertised device name over 32 bytes (decision 26) (carried: W2-wire-13) |
| R2-P5-apispec-13 | low | fixed `61fc2830`, `80382f2b` | `cluster/wire/scenes_management.go` — deprecate the ScenesManagement stub and the Schedules surface (carried: W3-clucore-07) |
| R2-P5-apispec-14 | low | fixed `61fc2830`, `80382f2b` | `cluster/wire/schedules.go` — deprecate the ScenesManagement stub and the Schedules surface (carried: W3-clugen-09) |
| R2-P5-apispec-15 | note | fixed in part `c1028573`: spec.Server says whose version the wire carries; `F-db021da0-46` stays open for LevelControl and ModeSelect (5b-notes C) | `cluster/spec/server.go` — say whose version the wire carries, that a Source wins over Set, and that the dispatcher scopes fabric-scoped reads (carried: F-db021da0-46) |
| R2-P5-apispec-16 | low | fixed `c25f319b`, `80382f2b` | `cluster/spec/server.go` — Server declares Notify, NotifyFrom, ChangedFrom and ReportFrom itself, deprecated |
| R2-P5-apispec-17 | note | fixed `b358db05`, `c1028573` | `cluster/spec/server.go` — a server emits on its own endpoint; the bridge withdraws a removed server's emitter |
| R2-P5-write-01 | low | fixed `a14b0074` | `endpoint/write_changes.go` — the write verdict in the result, by name; the hold settles against every overlapping write (carried: N4-d4ver-1) |
| R2-P5-write-02 | low | fixed `a14b0074`; the row `F-db021da0-39` is closed (`26a09ab6`); WindowCovering and FanControl are `F-db021da0-57` | `contract/matter.go` — the write verdict in the result, by name; the hold settles against every overlapping write (carried: F-db021da0-39) |
| R2-P5-write-03 | low | fixed `e793fd0a`, corrected by 5b-write F1 `00d68459`, R1 `c0865ad1`, S1 to S5 `13a55529` and T1 to T3 `60d1e358`; the row `F-db021da0-45` is closed (`26a09ab6`); what stays is `F-db021da0-56` and `F-db021da0-59` | `bridge/subscribe.go` — a host's change from inside a controller's write is held with the write (carried: F-db021da0-45) |
| R2-P5-write-04 | note (low) | fixed `1976e84a` as narrowed to a note | `node/nonvolatile.go` — PersistWrite stores nothing when the store already holds the bytes (carried: F-db021da0-47) |
| R2-P5-write-05 | low | fixed `a14b0074` | `bridge/write_hold.go` — the write verdict in the result, by name; the hold settles against every overlapping write |
| R2-P5-write-06 | low | fixed `eae4f21a`; the row `F-db021da0-32` is closed with a residue in its closing note (`6370b9fe`) | `im/read_expansion.go` — a concrete read path's status without a value read (carried: F-db021da0-32) |
| R2-P5-write-07 | low | fixed `3ab568e9`, `28b177e8` | `bridge/subscription_reestablish.go` — one chunk sender for the read, priming, ongoing, re-establish and invoke legs (carried: N-read-2) |
| R2-P5-write-08 | low | fixed `ab01d5e0` | `client/interaction/server.go` — one chunker, the served read generated chunk by chunk (carried: N3-ctl-1) |
| R2-P5-port-02 | medium | fixed `1976e84a`, `ee253205`, `e522bce3` | `node/nonvolatile.go` — Forget erases every setting kept under the key, by name and without reading them; an equal write stores nothing (carried: N-finp-2) |
| R2-P5-port-09 | low (medium) | fixed `dc22c2f5` | `contract/wifi.go` — the optional Wi-Fi link watcher and disconnector; the fail-safe's expiry takes the radio off a reverted network (carried: R2-P3-net-01) |
| R2-P5-port-10 | low (medium) | fixed `dc22c2f5` as narrowed | `cluster/core/network_commissioning_wifi.go` — the optional Wi-Fi link watcher and disconnector; the fail-safe's expiry takes the radio off a reverted network (carried: R2-P3-net-02) |
| R2-P5-port-16 | low | fixed `c8c1f660`, `25bd26bf` with R2-P5-apinode-11 (one port, the coordinator's answer to Q-port-3) | `node/persist.go` — one store-error rule for controller-caused endpoint state — endpoint.Persistence and node.EndpointState (carried: D-store-4) |
| R2-P5-port-04 | low | fixed `73236af5`, `e522bce3` | `cluster/core/group_key_management.go` — ReplaceGroupKeyMappings stores a GroupKeyMap write in one transaction (owner decision 107) (carried: F-db021da0-41) |
| R2-P5-port-05 | low | fixed `73236af5`, `e522bce3` | `cluster/core/vid_verification.go` — UpdateFabricVendorVerification writes the vendor id and the VID data in one write (owner decision 107) (carried: N-noc-2) |
| R2-P5-port-03 | low | fixed `73236af5`, `ee253205`, `e522bce3` | `store/port.go` — Settings has DeleteSettings and DeleteSettingsWithPrefix; Forget no longer reads every candidate (owner decision 107) (carried: N-finp-1) |
| R2-P5-port-01 | low | fixed `9df8d5a5`, `712980cd` (wave H) | `bridge/handlers.go` — the PASE adapter closes the verifiers it drops; a provider that drops an adapter closes its verifier (carried: W1-sec-05) |
| R2-P5-port-06 | low | fixed `73236af5`, `17073672`, `2b0692b3`, `98f9b141`, `a1c5ac4b`, corrected by 5b-peer P-5 `f70f050e` and P-5a `d7fc0718`; the row `F-db021da0-19` is closed (`26a09ab6`) | `store/resumption.go` — one resumption record per peer for both roles, with the session parameters (the coordinator's answer to Q-port-6 and peer Q4) (carried: F-db021da0-19) |
| R2-P5-port-07 | low | fixed `d928835a`, `bd16b5bf`, `91d1d44c`; the rows `F-db021da0-27` and `-34` are closed (`26a09ab6`) | `secure/operational/manager.go` — a peer's sessions by last peer activity (carried: F-db021da0-34) |
| R2-P5-port-08 | low | fixed `c45bf3b3`, `6fd9fde6`, corrected by 5b-seal F6 `a6ba30cc`; the row `F-db021da0-33` is closed (`26a09ab6`); `BD-chip-DoorLock-RecordSource` | `store/doorlock.go` — GetUser and GetCredentialStatus answer a record's fabrics only when Matter made it; the store keeps the source (schema v10) (carried: F-db021da0-33) |
| R2-P5-port-11 | low | fixed `ec8d183f`, `9b8fd3ae` | `cluster/core/general_commissioning_rollback.go` — the fail-safe expiry hook names the arming and the added fabric, the session closer the PASE sessions it closed, the PASE arm its session; the old hooks deprecated (the coordinator's answer to Q-port-4) (carried: R2-P2-comm-11) |
| R2-P5-port-17 | low | fixed `c45bf3b3`, `3a130dc5`, `4deb3507`, `08a88d4d`, `c5237d71`, `84261460`, `a86fad47`, `d3678b66`, `4a13d9cb`, `94d04980` (owner decision 108), corrected by 5b-seal F1, F2, R2-1 and R3-1 (`c5237d71`, `d3678b66`, `2766514a`) and `a70cf01b`; `BD-Matter-SealedAtRest`; the controller's store is owner decision 118 | `store/identity.go` — schema v10 — a host-supplied Sealer seals every secret column at rest, door-lock records keep their source (carried: N-store-2) |
| R2-P5-port-18 | low | fixed `73236af5`, `9f7d1b85`, `e522bce3`, `25bd26bf`, `fa0ec24c` (owner decision 109), corrected by DR5b-c2-02 `b07291b0` and DR5b-c3-04 `f58a414e`; `BD-Matter-OwedFabricEvents` | `node/node.go` — the fabric hooks a rollback owes are durable: Start delivers the store's owed events, then acknowledges them (carried: N-findev-3) |
| R2-P5-port-12 | low | fixed `37ea286f`, `a5d7c360` | `secure/attestation/product.go` — one implementation of the DAC/PAI vendor and product id rule, in internal/attestids (carried: R2-P1-sec-02) |
| R2-P5-port-13 | low | fixed `2169fe97` | `node/fabric_state.go` — a fabric index handed out again starts without the removed fabric's state (carried: D-fabric-2) |
| R2-P5-port-14 | low | fixed `194dc4b2`, `c047b49e`, `b21fd149`; `BD-Matter-OTAImage-HiddenUntilVerified` | `controller/softwareupdate/images.go` — stream a ledger image into a BlobStreamer and verify it in place (carried: OTA image streaming to store (Blobs)) |
| R2-P5-port-15 | low | fixed `73236af5`, `e522bce3` | `store/port.go` — the port states what a method does once the store is closed (carried: store-closed signal) |
| R2-P5-peer-01 | medium | fixed `76a721c3`, `2b0692b3` (owner decision 115, ADR 0032) | `node/client.go` — the node's client stack has TCP; one peer path over both stacks |
| R2-P5-peer-02 | low | fixed `2b0692b3` | `node/client.go` — one peer path over both stacks, the client stack with TCP, the node's parameters and the store's resumption |
| R2-P5-peer-03 | medium | fixed `bd16b5bf`, `2b0692b3` (owner decision 115); the rows `F-db021da0-29` and `-34` are closed (`26a09ab6`) | `bridge/case_initiator.go` — LivePeerSession with a transport requirement, newest by peer activity (carried: F-db021da0-29 (the sender half), F-db021da0-34 (the ordering)) |
| R2-P5-peer-04 | low | fixed `2b0692b3` | `bridge/case_initiator.go` — one peer path over both stacks, the client stack with TCP, the node's parameters and the store's resumption |
| R2-P5-peer-05 | low | fixed `1e75c43f` (wave H, owner decision 94); the row `F-db021da0-1` is closed (`26a09ab6`) | `notes/parity/matter_behaviour_findings.md` — the F-db021da0-1 row named a BDX half that was already closed; the server stack's sends are bounded by the peer's MAX_TCP_MESSAGE_SIZE (carried: F-db021da0-1) |
| R2-P5-peer-06 | low | fixed `911d585f` | `bridge/case_initiator.go` — the initiator's callers stated as they are |
| R2-P5-peer-07 | low | open: findings row `F-db021da0-50` (the coordinator's answer to peer Q3) | `bridge/subscription_reestablish.go` — Subscription re-establishment connects over UDP only |
| R2-P5-peer-08 | note | struck: resolved by the order of the waves, B after H; nothing to build (fix-peer) | the phase plan — waves B and H both listed bridge/ota_provider_link.go |
| R2-P5-redact-01 | high | fixed by construction `2b253a95`, `3cb9a8c6`, `40c1767e`, `9ccd46e2` (owner decision 110; fix-aredact), with N4-frsec-1 | `cluster/spec/server.go` — Disclosure class, held back until the release that carries its fix (owner decisions 25, 126). Same hole as N4-frsec-1, reached through a type of the module itself, so N4-frsec-1's 'no server of the module is affected' does not hold for it. Confirming read: High holds as a disclosure class: the module refuses an F attribute on its generated server and silently accepts an S one, which matter.js filters on every remote read. Limits, stated plainly: it needs a host that mounts the generated server of one of the six class-B clusters and stores several fabrics' entries in it; the module's own cluster/tls and cluster/camera servers filter (their lines not re-read by me). The path through the dispatcher to the wire was read, not run end to end. |
| R2-P5-redact-02 | low (medium) | fixed `2b253a95`, `e62fe4c6`, `a86a8379` (owner decision 110); the row `F-db021da0-30` is closed (`26a09ab6`) | `cluster/core/ecosystem_information.go` — the fabric scope of every attribute and event is generated from the snapshot; the hand-written redactions are gone, their Redacted fields deprecated (carried: F-db021da0-30) |
| R2-P5-redact-03 | low | fixed `2b253a95`, `40c1767e` | `im/read.go` — the event gate and the event read privilege come from the generated table (the coordinator's answer to redact-4) |
| R2-P5-redact-04 | low | fixed `d1026de0` | `contract/matter.go` — the dispatcher scopes every fabric-scoped attribute of the model |

### The diff review

An independent review read `7c15bb7a..de4365e2` in four slices: `dr5b-code-1`
the bridge, im, endpoint, internal and transport; `dr5b-code-2` node,
contract, device, cluster, schema, the scripts and examples; `dr5b-code-3`
store, controller, client, secure, bdx, ble and CI; `dr5b-fable` the security
paths and the reference statements at the integrated tip. Two agents reworked
it (`rwa`, `rwb`); the notes it left were placed by the coordinator.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| DR5b-c1-01 | low | fixed `704c98ac` | `endpoint/write_changes_guard_test.go` and every repository walk — the walks skip hidden directories, so a nested checkout under `.claude/` is not read as the module |
| DR5b-c1-02 | low | fixed `5d20e029` | `endpoint/endpointtest` — the contract check reads FeatureMap, ClusterRevision and vendor attributes |
| DR5b-c1-03 | low | fixed `fae26d98`, `5d836fd3` | `bridge/ble.go` — StartBLE registers the GATT service without the BLE lock |
| DR5b-c1-04 | note | phase 6: not taken into the rework (the coordinator's list) | `bridge/receive_dispatch.go`, `bridge/subscribe.go` — an error status to an ongoing report's last message closes the subscription twice: two log lines, the second Close answers ErrNotFound |
| DR5b-c1-05 | note | fixed `d752a2c7`; 5b-dr-questions c1-Q1: the flush's wait mirrors matter.js | `im/subscription` — FlushFabric's doc says an in-flight report is waited for |
| DR5b-c1-06 | note | phase 6: not taken into the rework (the coordinator's list) | `bridge/subscribe.go` — a session close counts as a failed update on the last message only; no wire effect (the subscription closes with its session) |
| DR5b-c1-07 | note | fixed `04d760d8` | `bridge` — a connection without a peer address is no TCP route |
| DR5b-c1-08 | note | fixed `2bc40dcd` | `secure/operational` — NewestSessionForPeer states the order it asks usable in |
| DR5b-c2-01 | low | fixed `2bd14dd3` | `cluster/spec/server.go` — a Set inside a write window queues its notification with its change |
| DR5b-c2-02 | low | fixed `b07291b0` | `node/node.go` — an owed fabric removal runs only when the store answers that the fabric is gone |
| DR5b-c2-03 | low | fixed `d5ffc2d4` | `device` — RequiredInitial answers an error when the defaults do not build |
| DR5b-c2-04 | low | fixed `bfc14b6a`, corrected by RW-03 `f27ef213` | `cluster/core/scenes_management.go` — a refused scene-table store restores the scene info too |
| DR5b-c2-05 | low | fixed `56885f6a`, `98f49b33`, `0a733e30`, `d23e4ad2` (the coordinator: device.New refuses too, the kit exempt) | `cluster/spec`, `device` — NewServer and device.New refuse an optional command selected or declared without a handler |
| DR5b-c2-06 | note | phase 6: not taken into the rework (the coordinator's list) | `cluster/spec/hooks.go` — the Hooks-based self-held servers skip the request check the others run; today only the mismatch warning differs |
| DR5b-c2-07 | note | fixed `d19b8513` | `cluster/spec/server.go` — ErrNotMounted's text names both causes |
| DR5b-c2-08 | note | struck: matter.js copies scene by scene too (5b-dr-questions c2-Q1) | `cluster/core/scenes_management.go` — CopyScene with CopyAllScenes stores scene by scene |
| DR5b-c3-01 | medium | fixed `11008c56` | `store/schema.go` — Apply upgrades a v7, v8 or v9 database that holds an ICD key in clear; measured by the reviewer, introduced on this branch |
| DR5b-c3-02 | low | fixed `548cb8fe` | `controller/softwareupdate` — a streamed download is tested under the checksum rule |
| DR5b-c3-03 | low | fixed with DR5b-F-02 | the controller's CASE resumption records at rest — the same as DR5b-F-02 |
| DR5b-c3-04 | low | fixed `f58a414e` | `store/memstore` — Erase keeps the last owed fabric event ID |
| DR5b-c3-05 | low | fixed `760b309f`; owner decision 119 `54c349aa` | `controller/commissioner` — the attested pair is recorded only for a verified chain without an error finding |
| DR5b-c3-06 | note | struck: no action; recorded so that the order is not changed into a deadlock later (the reviewer's) | `store/seal.go` — NewSealed's key check runs before the sealing transaction |
| DR5b-c3-07 | note | phase 6: not taken into the rework (the coordinator's list) | `controller/softwareupdate/images.go` — two concurrent streamed downloads of one name, one of them refused, can remove the other's verified file; the fix shape is a lock per name |
| DR5b-c3-08 | note | closed: the coordinator ran controller and mdns under emulated linux/386 — a commissioner test made deterministic (`de4365e2`), one mdns case fails under the emulation only (its mechanism not read) | `Makefile` — the 32-bit test run takes controller and mdns (owner decision 104, `f9974fa8`) |
| DR5b-F-01 | medium | fixed `6ede7e00` (fix-rwa item 1); 5b rework read: holds on every append path | the diff review's Fable read: a list-append write hands the server every fabric's entries (redaction × write), held-back class. endpoint/dispatcher.go:1840-1848 (appendListValue) read the current list from the server and appended the request's element; the read was narrowed to the writer's fabric only by a FabricScopedReader that honours filtered, any other server was read with MatterRead. Measured with an overlay test (vendor cluster 0xFFF1FC02, a FabricScopeDeclarer without FabricScopedReader, list [{1, fabric 1}, {2, fabric 2}], request stamped fabric 2, append {9, fabric 2}): MatterWrite was handed all three entries, status Success. Inferred: a server that applies a write as the accessing fabric's whole list stores or rewrites fabric 1's entry. The module's own writable scoped lists were not reached. matter.js: InteractionServer.ts:484-489 (always fabric filtered), ListManager.ts:609-620, AttributeWriteResponse.ts:437-449 |
| DR5b-F-02 | medium | fixed `261abd16` (the docs); owner decision 118: `1c4c465b`, `9adfdfbb`, `904ca733` | `controller/controller.go`, `docs/security.md` — the controller keeps its CASE resumption records in its database; Config.Sealer seals them with the CA, the fabric and the node registry's secrets |
| DR5b-F-03 | low | fixed `3880494a` | `client/handshake/store_resumption.go` — DeleteResumptionRecord removes a stored record that does not open |
| DR5b-F-04 | info | phase 6: not taken into the rework (the register editor's placement) | `cluster/core/network_commissioning_wifi.go` — the Wi-Fi list fails closed where the durable commit is wired; without that hook a refused save is logged only; one sentence at loadCommitted's doc would say so |
| DR5b-F-05 | info | phase 6: not taken into the rework (the register editor's placement) | `node/node.go`, `docs/guides/persistence.md` — a sealed store that does not open gives the host no node to call FactoryReset on; a sentence on erasing with store.New(db).Erase would close it (the guide not read for one) |

### The review of the rework

One read took the rework, `61d7bedb..c48cbb33`: F-01's fix on every append
path, the sealing of the controller's store, decision 119 and the rest of both
agents' items; a re-check read rwa's fourth round (`ea20cebf`, `43e2027e`),
closed RW-01 and found RW-04, which the coordinator fixed. The `RW-i` rows are
its info items.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| RW-01 | medium | fixed `ea20cebf`, `43e2027e` | `controller/client/sqlstore/sqlstore.go` — the registry's NewSealed records the scrub it owes in the sealing transaction and runs it until one finishes (the read's re-check: closed) |
| RW-02 | low | fixed `8531163c` | `bridge/subscribe.go` — the chunk wait's doc says matter.js leaves the peer term at 0 for this wait; `BD-Matter-Subscription-ReportWaitBound` holds |
| RW-03 | low | fixed `f27ef213`, `f88fba0a` | `cluster/core/scenes_management.go` — a refused first command leaves no scene info entry behind |
| RW-04 | low | fixed `d595a371` (by the coordinator) | `controller/client/sqlstore/migrations/v2.down.sql` — v2's Down refuses a database that still owes a scrub; found by the read's re-check of RW-01 |
| RW-i1 | info | not a finding: the contract now says one thing; the earlier wording was never released (the read's) | `contract/matter.go` — a non-encoding scoped list is trusted to the reader's filtered answer |
| RW-i2 | — | held back until its fix merges (owner decision 25) — phase 5b | — |
| RW-i3 | — | held back until its fix merges (owner decision 25) — phase 5b | — |
| RW-i4 | info | phase 6: not traced (the read's) | `controller/commissioner/flow.go` — the attested flag is not cleared on the withheld branch; a second run of the step in one flow was not traced |
| RW-i5 | note | phase 6 (the register editor's placement) | `controller/client/sqlstore/migrations/v2.up.sql` — the comment names the onboarding code only |

### The reference reads

Reference reads at the pins (matter.js `db021da0`, connectedhomeip
`6170af84`). Six are fact notes read before the code or the notes were written
— `5b-A`, `5b-F`, `5b-H` for the waves' briefs, `5b-d17` for decision 17's
rule, `5b-dcl-ledger` for the checksum rule, `5b-notes` for the notes pass;
the others read a wave's diff and each of its rework rounds (`5b-fctl`,
`-wire`, `-dcl`, `-seal`, `-aspec`, `-afacade`, `-peer`, `-aredact`, `-gdd`,
`-write`), the diff review's questions (`5b-dr-questions`) and the finished
notes (`5b-notes-text`). A row is an item that asked for a change or stays
open; the statements a read found to hold are not rows.

| ID | sev | disposition | where |
| --- | --- | --- | --- |
| 5b-A A2 | — | fixed `ec49ca6c` (owner decision 112 holds for commands); the recall is `BD-Matter-OnOff-RecallBypassesHandle` | `cluster/levelcontrol`, `cluster/onoff` — a scene recall reaches the server's own logic, never the host's Handle |
| 5b-A A3 | — | fixed in part `056e82c8`; the sibling coupling stays `F-db021da0-21` | `cluster/thermo` — SetMeasuredTemperature can be built; matter.js couples to a sibling endpoint once, at online |
| 5b-A A4 | — | parity: a recorded divergence, by_design `BD-Matter-FabricScopedEntryWithoutIndexRefused` (owner decision 111), `9ccd46e2` | `im` — an index-less entry of a fabric-scoped list answers FAILURE as connectedhomeip; matter.js serves it to every fabric |
| 5b-F Q1 | — | parity: a recorded divergence, by_design `BD-Matter-SealedAtRest` (Ahead of matter.js; owner decision 108) | `store/seal.go` — matter.js encrypts only the door lock's credential data at rest |
| 5b-F Q2 | — | parity: a recorded divergence, by_design `BD-Matter-OwedFabricEvents` (Ahead; owner decision 109) | `store`, `node` — matter.js reverts nothing at start-up; connectedhomeip's marker covers removal only |
| 5b-F Q3 | — | parity: a recorded divergence, by_design `BD-Matter-OTA-AttestedIdentityKept` (Ahead; owner decision 105) | `controller` — matter.js checks a QueryImage against the peer's self-reported BasicInformation only |
| 5b-F Q4 | — | parity: a recorded divergence, by_design `BD-chip-DoorLock-RecordSource`, `c45bf3b3`, `6fd9fde6` | `cluster/lock` — matter.js leaves it open; connectedhomeip answers null for a record not made through Matter |
| 5b-F Q5 | — | phase 6: the final status after a store refusal in commit phase one partly untraced (the notes pass left it out) | `node/nonvolatile.go` — matter.js rolls back on a refused store; `BD-Matter-NonVolatileWrite-StoredBeforeApplied`'s cites hold |
| 5b-H H1 | — | fixed `1f9960ad` | `controller/client/network.go` — one SubscriptionAlive per subscription going live, as matter.js |
| 5b-H H2 | — | fixed `728b3881`, `1789ab2b`, `c0ea5ada`, `edf78232` (wave H); the bitmap range is `BD-Matter-SessionParameters-BitmapRange` | fable-read-5b-H.md H2: the PASE session-parameters strictness on the matter.js side (PaseMessages.ts:30-54) — three gaps (type, range, non-context tag) and three as matter.js; the same item as 5a-2 F2, 5a-3 F2 and 5a-4 F2 |
| 5b-H H3 | — | fixed `abb0bc37`, `de9399ca` (owner decision 95); `BD-Matter-Subscription-ReportsOverlap` retired, the row `F-db021da0-36` closed | `bridge/subscribe.go` — one report in flight per subscription, delivered by its StatusResponse |
| 5b-H H4 | — | fixed `45767cff` (the order of authority; no owner question) | `im` — a wrong-typed integer field in a request answers FAILURE |
| 5b-H H5 | — | fixed `1e75c43f`; the row `F-db021da0-1` is closed (`26a09ab6`) | `bridge` — the peer's MAX_TCP_MESSAGE_SIZE bounds every send on the server stack |
| 5b-d17 | — | fixed `f10c9d13`, `42b40e68`, `8bafdc1e`, `cf274b7d` | `device`, `script/clustergen` — New refuses what matter.js's whole initial validation refuses; a struct attribute takes the default its fields state (OperationalError) |
| 5b-dcl-ledger | — | parity: a recorded divergence, by_design `BD-Matter-OTAImage-LedgerChecksumRequired`, `c33fccf6` | `controller/softwareupdate` — a divergence only for a missing checksum; matter.js checks one when it is present; type 0 is SHA-256 as matter.js |
| 5b-notes A1 | — | parity: a recorded divergence, by_design `BD-Matter-Subscription-ReportWaitBound`, `c33fccf6` | `bridge/subscribe.go` — matter.js bounds the wait too; Go differs in the clock's start and the peer term |
| 5b-notes A1c | — | fixed `182b9cbf`, corrected by RW-02 `8531163c` | `bridge/subscribe.go` — the comment's "never shorter than matter.js's" was unproven; it names its terms |
| 5b-notes A2 | — | open: findings row `F-db021da0-59` (`6370b9fe`) | the change-only notifiers (thermostat, cover, occupancy) mark before the version advance |
| 5b-notes B-C2 | — | parity: a recorded divergence, by_design `BD-Matter-OTAImage-HiddenUntilVerified`, `c33fccf6` | `controller/softwareupdate` — a streamed image is not listed or opened while it is checked; stricter than matter.js |
| 5b-notes B-dec | — | struck: the generated decoders hold the bounds the proposed row names; not written | the decoder-gap row the aspec agent proposed |
| 5b-notes B-cf | — | open: findings row `F-db021da0-57` | `cluster/cover`, `cluster/fan` — not on ChangeNamingWriter |
| 5b-notes C-46 | — | open: `F-db021da0-46` reopened (`6370b9fe`) | `cluster/levelcontrol`, `cluster/modeselect` — the doc commit covers spec.Server only |
| 5b-notes C-32 | — | fixed: the residue stated in the row's closing note (`6370b9fe`) | `endpoint/dispatcher.go` — an attribute the server does not list is still read twice |
| 5b-notes C-208 | — | fixed `a4a66b4f`, `04048195` | `notes/parity/pin-lifts.md` — awaitChunkStatusResponseWithin lives in bridge/receive_dispatch.go |
| 5b-fctl F1, F2 | low | fixed `a5d7c360` | `internal/attestids` — the citations of the lenient half and the attributes' names at the pin |
| 5b-fctl O2 | low | open: findings row `F-db021da0-49` | `internal/attestids` — the vendor and product id are read from a string-typed DN value only; matter.js reads the raw bytes (pre-existing, inferred) |
| 5b-wire F-H2-a | low | fixed `c0ea5ada` (wave H round 2); the round-2 read: closed | fable-read-5b-wire.md: the session-parameters member was not required to be a structure on Sigma1 / Sigma2 / Sigma2_Resume (secure/sigma/sigma.go:342, initiator.go:579, :634); matter.js TlvObject.ts:206-207 refuses |
| 5b-wire H2-n | — | fixed `c0ea5ada`, `0205380a` (wave H round 2) | fable-read-5b-wire.md, noticed outside H2: the Sigma messages' own uint16 session-id members were taken masked with no type check — sigma.go:325 (InitiatorSessionID), initiator.go:569, :631 (ResponderSessionID), against TlvField(2 or 3, TlvUInt16) (CaseMessages.ts:31, :43, :54) |
| 5b-wire F-H2-b | low | parity: a recorded divergence, by_design `BD-Matter-SessionParameters-BitmapRange`, `edf78232` | `secure/sessionparams` — a SUPPORTED_TRANSPORTS value above 16 bits is refused as connectedhomeip |
| 5b-wire F-H3-a | low | parity: a recorded divergence, by_design `BD-Matter-Subscription-ThirdFailureClosesEveryKind`, `13eb50a8` | `bridge/subscribe.go` — the third failed update closes for every kind; matter.js for the communication failures only |
| 5b-wire F-H3-b | low | fixed `de9399ca` | `bridge/subscribe.go` — a StatusResponse wait over a connection is bounded at 35 s |
| 5b-wire F-R2-a | low | fixed `3a653228` | `bridge` — a bridge's own chunk-bound override wins over the package test bound |
| 5b-wire F-R2-b | info | fixed `b475dfd3` (the two by_design wordings) | `notes/parity/by_design.md` — keeps the flags it knows; stays above the limit |
| 5b-dcl F1 | medium | fixed `ae8e501f`, `1fdd35f0` | `controller/softwareupdate` — stored ledger images keep their provenance; the provider offers only stored images the store serves (owner decision 117) |
| 5b-dcl F2 | medium | decided: owner decision 116a; fixed `06e2b7cc`, `4b524acf` | `controller/dcl` — a host is judged before a proxy and on each redirect |
| 5b-dcl F3, F4 | low | fixed `ae8e501f` | `controller/softwareupdate` — downloads follow the update's origin; a test-net record goes through the test client |
| 5b-dcl F5 | info | decided: owner decision 116a (CRL points http or https), `06e2b7cc` | `controller/dcl` — CRL distribution points |
| 5b-dcl F6 | — | held back until its fix merges (owner decision 25) — phase 5b | — |
| 5b-dcl F7 | info | decided: the refused set stays decision 116's five; the docs name the classes beyond them (`63c749c9`) | `controller/dcl`, `docs/security.md` — the address classes the guard refuses |
| 5b-dcl F8 | info | fixed `63c749c9` | `controller/dcl` — the URLs the GitHub API names are fetched through the guarded download client |
| 5b-dcl M8 | — | fixed: the comment takes the read's wording (DCL round 2) | `controller/dcl/client.go` — what matter.js's download does with redirects |
| 5b-dcl R1 | medium | fixed `4b524acf` | `controller/dcl` — a host is judged as the transport sends it before a proxy |
| 5b-dcl R2 | low | fixed `4b524acf` | `controller/dcl` — the legacy IPv4 spellings are read in both readings |
| 5b-dcl R3 | low | fixed `1fdd35f0` | `controller/softwareupdate` — UpdateExistsFor offers only servable stored images |
| 5b-dcl R4 | info | fixed `1fdd35f0` | `controller/softwareupdate` — a provenance record names its bytes |
| 5b-dcl R5 | info | fixed `4b524acf` (a doc sentence) | `controller/dcl` — the configured GitHub API is the host's choice and goes through the host's client |
| 5b-dcl R6 | low | fixed `831fe9fc`, `445ec296` | `controller/dcl` — a CONNECT target net/http could not map to ASCII is refused |
| 5b-dcl R7+R8 | — | held back until its fix merges (owner decision 25) — phase 5b | — |
| 5b-dcl R9 | — | held back until its fix merges (owner decision 25) — phase 5b | — |
| 5b-seal F1 | medium | fixed `c5237d71` | `store/seal.go` — a sealed store is strict: a clear secret row is refused once open; the read side checks lengths |
| 5b-seal F2 | medium | fixed `c5237d71` | `store/seal.go` — the joint fabric record is sealed |
| 5b-seal F3 | medium | fixed `84261460` | `docs/security.md` — sealing gives confidentiality of a copy, not integrity |
| 5b-seal F4 | low | fixed `84261460` (the journal modes stated); phase 6: PERSIST journal hardening (the coordinator's list) | `store/seal.go` — the residue after the sealing upgrade was measured in WAL mode only |
| 5b-seal F5 | low | fixed `def1ebff` | `cluster/core/network_commissioning_wifi.go` — an unreadable stored Wi-Fi list fails closed |
| 5b-seal F6 | low | fixed `a6ba30cc` | `cluster/lock/users.go` — each fabric index follows its own source |
| 5b-seal F7 | low | fixed `84261460` | `docs/guides/persistence.md`, `docs/security.md` — two wordings |
| 5b-seal F8 | low | fixed `c5237d71` | `store/seal.go` — an unexpected key type in the upgrade is an error, not a panic |
| 5b-seal R2-1 | medium | fixed `d3678b66`, `4a13d9cb` | `store` — a write refuses the key lengths a read refuses, in both backends; ListGroupKeySets leaves a malformed row out |
| 5b-seal R2-2 | low | fixed `4a13d9cb` (strict while open, stated); phase 6: a persistent sealed-database marker (the coordinator's list) | `store/seal.go` — strict until the next NewSealed |
| 5b-seal R2-3 | low | fixed `d3678b66` | `store/seal.go` — a stale comment; the ICD upgrade's error |
| 5b-seal R2-4 | low | fixed `4a13d9cb` | `store/seal.go` — the PERSIST limit in NewSealed's doc |
| 5b-seal R3-1, R3-2 | low | fixed `2766514a` | `store/groupkeys.go` — an empty optional epoch key reads as absent; the list's left-out row is documented |
| 5b-aspec F1 | medium | fixed `6e1d83b8` | `cluster/spec/quiet.go` — a stored quieter attribute advances its version at the change, only a computed one at the emit, as matter.js |
| 5b-aspec F2 | low | parity: a recorded divergence, by_design `BD-Matter-OnOff-RecallBypassesHandle` | `cluster/onoff` — the recall reaches OnOff's own logic |
| 5b-aspec F3 | low | fixed `84158c57` | `cluster` — every self-held server consults the host veto after its held-value comparison |
| 5b-aspec F4-F7 | low | fixed `6e1d83b8` | `script/clustergen/hull.go`, `cluster/spec/server.go` — citations and two doc sentences |
| 5b-aspec R2-1, R2-2 | low | fixed `d043a25e` | `script/clustergen` — the struct-default rule stated as this generator's narrowing of matter.js's |
| 5b-afacade F1 | medium | fixed `6fdcf1f6`, `48b932c7`; open: findings row `F-db021da0-53` | `mdns/service.go`, `node/config.go` — chip reads a PH above 65535 as no hint |
| 5b-afacade F2 | low | fixed `a55708d6` | `cluster/thermo` — the measured-temperature citations the right way round |
| 5b-afacade F3 | low | fixed `8bafdc1e` | `device/initial.go` — a struct child matter.js builds is not refused |
| 5b-afacade F4, F5 | low | fixed `8bafdc1e` | `device` — the corpus citations |
| 5b-afacade F6 | low | fixed `6fdcf1f6`; open: findings row `F-db021da0-54` | `mdns/service.go` — the instruction set as matter.js's, bits 20 to 22 beside it; Hints bits 23 to 31 refused |
| 5b-afacade F7 | low | fixed `a55708d6`, `48b932c7` | `docs/guides/ports.md` — without the node only the bridged endpoints drop a removed fabric's state |
| 5b-peer P-1 | low | fixed `7548edee` | `im/group.go` — a group invoke leaves a Large Message command out |
| 5b-peer P-2 | low | fixed `6779f0ee` | `node/peer_path.go` — the cross-stack order on the monotonic reading |
| 5b-peer P-3 | low | fixed `5a81934d` | `node` — the client stacks are swept after the fabric's identity changed |
| 5b-peer P-4 | medium | fixed `55993817`, corrected by P-4a `4c935b58` and `883a8b8d` | `controller` — a taken-over fabric row drops the former own node's resumption records |
| 5b-peer P-5 | low | fixed `f70f050e` | `secure/operational`, `node` — the responder's record carries the peer's session parameters |
| 5b-peer P-6, P-7, P-8 | low (P-7 medium) | fixed `10e7bfe4` | `client/handshake`, ADR 0032 — the resumed parameters' base; the comparison sections |
| 5b-peer P-9 | medium | fixed `c449c646`, `9e476bad`; what stays is `F-db021da0-51` | `node/client.go` — the client stack serves the node's endpoint tree on the sessions it opens, with the bridge's codec |
| 5b-peer R2-1 | low (medium as behaviour) | fixed `f4d4e3a7`, corrected by R3-1 `a64ae804` | `cluster/core/operational_credentials.go` — the CSR binds to the invoking session's identity on either stack |
| 5b-peer P-9b, P-9c | low | open: findings row `F-db021da0-51` | `client/interaction/server.go` — a write there ignores the bridge's holds; no per-session resource bounds |
| 5b-peer P-5a | low | fixed `d7fc0718` | `node` — a resumed session keeps the record's session parameters |
| 5b-peer R3-1 | medium | fixed `a64ae804` | `cluster/core` — UpdateNOC binds no CSR session, as matter.js and connectedhomeip |
| 5b-peer R3-1b, R3-2 | low | fixed `9e476bad` | `bridge`, `node` — SessionLookup's stable pointer stated; RemoveFabric and UpdateNOC on a client-stack session end to end |
| 5b-peer R3-1c | low | fixed `a64ae804` | `cluster/core` — a session identity is a pointer |
| 5b-peer R4-1a | low | fixed `31bbb5cc` | `cluster/core` — the session binding's doc names AddNOC only |
| 5b-peer R4-1b | low | phase 6: not taken (the register editor's placement) | `cluster/core` — WithInvokeSession drops a non-pointer key silently: a nil binding for a foreign stack without ids |
| 5b-peer R4-2a | low | open: findings row `F-db021da0-52` | `client/interaction/server.go` — a whole-request decode failure is answered differently by the two stacks |
| 5b-peer R4-2b | low | fixed `f573559c` | `CHANGELOG.md` — bridge.Codec announced |
| 5b-aredact F1 | low | fixed at integration: the wave's commit messages reworded (the coordinator's list) | the wave's commit messages |
| 5b-aredact F2 | low | parity: a recorded divergence, by_design `BD-Matter-FabricScopedEntryWithoutIndexRefused` (the coordinator: keep) | `im` — the encode-time FAILURE of an index-less entry, also on wildcard and change reports, as connectedhomeip |
| 5b-aredact F3 | info | open: findings row `F-db021da0-55` | `contract/fabric_scope.go` — a vendor attribute on a snapshot cluster cannot be declared |
| 5b-aredact F4 | low | fixed `9ccd46e2` | `im`, `contract` — owner and reader by HasAccessingFabric |
| 5b-gdd L1 | low | fixed `b639fe09` | `docs/certifiability.md` — the DD flow cases hold the daemon on every leg that runs |
| 5b-gdd L2 | low | struck: a line range in the fix note only, not in the repository | `fix-gdd.md` item 6 |
| 5b-write F1 | medium | fixed `00d68459`, corrected by R1 `c0865ad1` | `cluster/spec`, `bridge` — a Sink's sibling is part of the write's commit, one advance |
| 5b-write F2 | medium | fixed `00d68459` | `bridge`, `im` — group writes are reported to subscribers |
| 5b-write F3 | low | open: findings row `F-db021da0-56` | `bridge/write_hold.go` — a host's change during a later attribute data of a written cluster gets no advance of its own |
| 5b-write F4, F5 | low | fixed `00d68459`, `c742937e`, `dc6de94f` | `bridge`, `client/exchange` — re-establishment stops with the bridge; the piggyback citation |
| 5b-write F6 | low | fixed `00d68459`, corrected by R2 `c0865ad1` (the bridge's C filters were redundant and are removed) | `spec`, `bridge` — names a commit omits are not broadcast |
| 5b-write F8 | info | fixed `00d68459` | `im` — WriteCommit's doc on a sibling |
| 5b-write R1 | high | fixed `c0865ad1`, corrected by S1 to S5 `13a55529` and T1 to T3 `60d1e358` | `cluster/spec/server.go` — one commit window per generated server; overlapping writes each advance once (measured at the server; introduced on this branch, never released) |
| 5b-write R4 | info | fixed `2dc4a4ba` | `cluster/core/identify.go` — the IdentifyServer.ts citation at db021da0 |
| 5b-write S1-S5 | low | fixed `13a55529` | `cluster/spec`, `bridge` — the commit window never hangs; a report view's mark waits for the write |
| 5b-write T1-T3 | low | fixed `60d1e358` | `cluster/spec` — a panicking Sink settles its sibling's change; the window's limits stated |
| 5b-write R3w | low | open: findings row `F-db021da0-59` | `bridge` — the remaining window of the report view's mark |
| 5b-dr-questions c2-Q2 | low | fixed `77b95e2d` | `cluster/core/scenes_management.go` — a FabricSceneInfo read fails while the scene table does not load, as connectedhomeip |
| 5b-dr-questions WC | — | phase 6: a findings candidate (the coordinator's list) | `cluster/cover/windowcovering_server.go` — the veto gets the written Mode, not the Mode the server stores |
| 5b-dr-questions cite | — | fixed `527e77de` | `controller/attestation/validate.go` — the no-trust-store finding cites DeviceAttestationValidator.ts:312-318 at db021da0 |
| 5b-dr-questions c1-Q1d | — | phase 6: not compared (the read's) | `bridge/subscribe.go` — after a non-status failure of the in-flight update at close, matter.js does not send the flushed data |
| 5b-notes-text | — | fixed `61d7bedb` (10 wordings narrowed to what was read; 59 held) | `notes/parity/` — the Fable read of the 5b notes |

## What earlier phases handed to phase 5b

Each unit was given the rows of earlier registers and of the findings register
that the carry-in map of phase 5 places in its area for 5b, and reported their
status at `7c15bb7a`. The status is the unit's word for it and was not
re-measured for this file; "where it stands" is at `fa118739`. An item given
to two units has one row (F-db021da0-19 and -34: `port` and `peer`).

| ID | unit | status at the review | where it stands |
| --- | --- | --- | --- |
| N-life-5 | apinode | open | fixed `81ccebca`, `fd8a5ef1` with R2-P5-apinode-04 and -19 |
| N-node2-3 | apinode | open | fixed `0d483f5a` with R2-P5-apinode-09 |
| S-17 | apinode | open | fixed `0d483f5a` with R2-P5-apinode-08 |
| N4-rwcore-1 | apinode | open | decided: keep as documented — a grant only adds, the administrator removes (the coordinator's answer to apinode Q5); GrantFabric's doc already says so (fix-afacade) |
| W2-api-06 | apinode | open | fixed `b3cff6b6` with R2-P5-apinode-12: deprecated, removable in the next minor |
| W2-api-07 | apinode | open | fixed `c8c1f660` with R2-P5-apinode-11 |
| W2-api-09 | apinode | open | fixed `b3cff6b6` with R2-P5-apinode-13 |
| Controller.Close second wait | apinode | open | fixed `fd8a5ef1` with R2-P5-apinode-14 |
| SetDefaultSubscription gap | apinode | open | fixed `f0e2ba7a` with R2-P5-apinode-15 |
| PairingHint width | apinode | open | fixed `81ccebca`, `6fdcf1f6` with R2-P5-apinode-03; `F-db021da0-53`, `-54` |
| reaper arguments | apinode | already fixed | closed before this phase: not a defect in the call; the recommended ratio is R2-P5-apinode-16, fixed `0d483f5a` |
| F-db021da0-21 | apidev | open | fixed in part `056e82c8` with R2-P5-apidev-06; the row is narrowed to the sibling couplings (`26a09ab6`) |
| F-db021da0-23 | apidev | open | fixed `62a4f36c` with R2-P5-apidev-12; the row is closed (`26a09ab6`) |
| F-db021da0-28 | apidev | deferred to phase 6 | phase 6 (owner decision 92), with R2-P5-apidev-02 |
| N-rest3-1 | apidev | open | fixed `dc0913dc` with R2-P5-apidev-08 |
| N-req-2 | apidev | open | fixed `8ef44b44` with R2-P5-apidev-05 (owner decision 114) |
| W5-api-18 | apidev | open | fixed `62a4f36c` with R2-P5-apidev-03 |
| N-1 | apidev | open | fixed `f10c9d13`, `42b40e68` with R2-P5-apidev-10 (decision 17 as matter.js's whole initial validation) |
| N4-dec2b-2 | apidev | open | fixed `b358db05`, `91d1f1c2` with R2-P5-apidev-04 |
| F-db021da0-20 | apispec | open | fixed in part `13f1a70a` with R2-P5-apispec-09; the row is narrowed (`26a09ab6`) |
| F-db021da0-22 | apispec | open | fixed `290b7636`, `6e1d83b8` with R2-P5-apispec-03; the row is closed (`26a09ab6`) |
| F-db021da0-31 | apispec | open | fixed `ec49ca6c` with R2-P5-apispec-05 (owner decision 112); the row is closed (`26a09ab6`) |
| F-db021da0-40 | apispec | open | fixed `8809cb84`, `84158c57` with R2-P5-apispec-02; the row is closed (`26a09ab6`) |
| F-db021da0-46 | apispec | open | fixed in part `c1028573` with R2-P5-apispec-15; the row stays open (closed and reopened on the branch, `6370b9fe`) |
| W2-wire-13 | apispec | open | fixed `0d483f5a` with R2-P5-apispec-12 |
| S-3 | apispec | open | fixed `bba778ba` with R2-P5-apispec-11 |
| W3-clucore-07 | apispec | open | fixed `61fc2830` with R2-P5-apispec-13: deprecated |
| W3-clugen-09 | apispec | open | fixed `61fc2830` with R2-P5-apispec-14: deprecated |
| N4-d4ver-1 | write | open | fixed `a14b0074` with R2-P5-write-01 (the coordinator's answer to Q-write-1) |
| F-db021da0-39 | write | open | fixed `a14b0074` with R2-P5-write-02; the row is closed (`26a09ab6`) |
| F-db021da0-45 | write | open | fixed `e793fd0a` with R2-P5-write-03; the row is closed (`26a09ab6`) |
| F-db021da0-47 | write | open | fixed `1976e84a` with R2-P5-write-04, as narrowed; the row stands in the findings register at this file's head ("Not placed") |
| F-db021da0-32 | write | open | fixed `eae4f21a` with R2-P5-write-06; the row is closed with its residue (`6370b9fe`) |
| N-read-2 | write | open | fixed `3ab568e9` with R2-P5-write-07 |
| N3-ctl-1 | write | open | fixed `ab01d5e0` with R2-P5-write-08 |
| W2-conc-04 | write | open | phase 6: wave H's H7, the single reporter hook (fix-wire; the coordinator's list) |
| W1-sec-05 | port | open | fixed `9df8d5a5`, `712980cd` with R2-P5-port-01 |
| store-closed signal | port | open | fixed `73236af5` with R2-P5-port-15 (the port's doc) |
| OTA image streaming to store (Blobs) | port | open | fixed `194dc4b2`, `c047b49e`, `b21fd149` with R2-P5-port-14 |
| R2-P2-comm-11 | port | open | fixed `ec8d183f`, `9b8fd3ae` with R2-P5-port-11 |
| D-comm-4 (c) | port | open | fixed `ec8d183f` with R2-P5-port-11 |
| N-store-2 | port | open | fixed `c45bf3b3` with R2-P5-port-17 (owner decision 108) |
| N-noc-2 | port | open | fixed `73236af5` with R2-P5-port-05 |
| N-findev-3 | port | open | fixed `73236af5`, `9f7d1b85` with R2-P5-port-18 (owner decision 109) |
| N-finp-1 | port | open | fixed `73236af5`, `1976e84a` with R2-P5-port-03 |
| N-finp-2 | port | open | fixed `1976e84a` with R2-P5-port-02 |
| R2-P3-admin-04 | port | open | struck: the unit recommends closing without a change — the list is one stored value bounded at 64, and matter.js commits whole values; placed by the register editor, not re-read |
| R2-P3-net-01 | port | open | fixed `dc22c2f5` with R2-P5-port-09 |
| R2-P3-net-02 | port | open | fixed `dc22c2f5` with R2-P5-port-10 |
| R2-P3-dr-net-01 | port | open | fixed `dc22c2f5` with R2-P5-port-10 |
| F-db021da0-19 | port, peer | open | fixed `73236af5`, `17073672`, `2b0692b3`, `98f9b141` with R2-P5-port-06; the row is closed (`26a09ab6`) |
| F-db021da0-27 | port | open | fixed `d928835a`, `91d1d44c` with R2-P5-port-07; the row is closed (`26a09ab6`) |
| F-db021da0-33 | port | open | fixed `c45bf3b3`, `6fd9fde6` with R2-P5-port-08; the row is closed (`26a09ab6`) |
| F-db021da0-34 | port, peer | open | fixed `d928835a`, `bd16b5bf` with R2-P5-port-07 and R2-P5-peer-03; the row is closed (`26a09ab6`) |
| F-db021da0-41 | port | open | fixed `73236af5` with R2-P5-port-04; the row stands in the findings register at this file's head ("Not placed") |
| R2-P1-sec-02 | port | open | fixed `37ea286f` with R2-P5-port-12 |
| D-fabric-2 | port | open | fixed `2169fe97` with R2-P5-port-13 |
| TM R6.2 | port | open | fixed `c83a59e6`, `0f3cd37a`, `06e2b7cc`, `ae8e501f`, `4b524acf`, `1fdd35f0`, `831fe9fc` (owner decisions 116, 117, 116a; brief-dcl-trust.md) |
| N3-net-4 | port | open | fixed `c83a59e6`, `06e2b7cc`, `63c749c9`, `4b524acf`, `831fe9fc` (owner decisions 116, 116a) |
| D-store-4 | port | open | fixed `c8c1f660` with R2-P5-apinode-11 and R2-P5-port-16 (one rule: a refused store answers FAILURE and applies nothing) |
| R2-P4-imwrite-01 | peer | open | fixed `4fd600da`, `7548edee` (owner decision 115); the row `F-db021da0-29` is closed (`26a09ab6`) |
| F-db021da0-1 | peer | open | fixed `1e75c43f` (wave H, owner decision 94) with R2-P5-peer-05; the row is closed (`26a09ab6`) |
| F-db021da0-30 | redact | open | fixed `2b253a95`, `40c1767e`, `e62fe4c6` with R2-P5-redact-02 (owner decision 110); the row is closed (`26a09ab6`) |
| N4-frsec-1 | redact | open | fixed by construction `2b253a95`, `3cb9a8c6`, `40c1767e`, `9ccd46e2` (owner decision 110) with R2-P5-redact-01 |

Phase 5a's register placed these rows in 5b; the ones no 5b unit was given are
placed here.

| ID | where it stands |
| --- | --- |
| N-ctr-5 | phase 6: not given to a 5b unit (the register editor's placement) |
| N-rw2-3 | phase 6: not given to a 5b unit; the test seam it needs does not exist (fix-tqual; the register editor's placement) |
| R2-P3-ctl-dev-05 and TM R4 Q7 | fixed `a89b33a0`, `b14f66ed` (owner decision 105: keep the attested pair, log a mismatch; `BD-Matter-OTA-AttestedIdentityKept`); recorded only for a verified chain since owner decision 119 (`54c349aa`); the refusal rule decision 105 conditions on a read is not built — phase 6 (the register editor's) |
| N5-tqual-1 | fixed `13f1a70a` with R2-P5-apispec-08 (owner decision 113) |
| N5-docs-2 | fixed `0d483f5a` with R2-P5-apinode-16: the durations are named, the cadence kept |
| N5-race-2 | fixed `1f9960ad` (wave H, 5b-H H1) |
| N5-fxcode-1, 5a-2 D1, DR5a-code-06 | fixed `45767cff` (wave H, 5b-H H4) |
| 5a-2 A1-d | phase 6: the doubly malformed order was not traced for this file; `45767cff` answers a wrong type FAILURE (the register editor's placement) |
| owner decision 94 | fixed `1e75c43f` (wave H); the entry `BD-Matter-TCP-ExchangePayloadSize` is not retired on this branch (its heading stands) |
| owner decision 95 | fixed `abb0bc37`; `BD-Matter-Subscription-ReportsOverlap` retired (`b475dfd3`), the row `F-db021da0-36` closed (`26a09ab6`) |
| owner decision 96 | fixed `81ccebca` (the node option), `9722c8a0`, `414763b3` (the daemon's flow and TC-DD-3.12, 3.13 ported) |
| owner decision 97 | fixed `8e45fd78`, `48b98ae4` |
| 5a-2 F2 | fixed `728b3881`, `1789ab2b`, `edf78232` (wave H, H2) |
| 5a-3 F2 | fixed with 5a-2 F2 (wave H, H2) |
| 5a-4 F2 | fixed with 5a-2 F2 and `c0ea5ada` (wave H, H2 and its round 2) |
| N5-rw2-1 | fixed `cb9717bd`, `9df8d5a5`, `712980cd` (wave H, H6) |

## The units' questions

The units' 30 questions went to a decision brief (`wizard-2`): ten to the
owner, twenty answered by the coordinator under the decisions they follow
from. Where an answer differed from a unit's recommendation, the brief says
why.

| unit | question | answer | by | where |
| --- | --- | --- | --- | --- |
| apinode | Q1 | Commissioning.WindowOnDemand | the coordinator (wizard-2's list) | done `81ccebca` |
| apinode | Q2 | Flow and WindowOnDemand in wave A2, the pairing hints with R2-P5-apinode-03 | the coordinator (wizard-2's list) | done `81ccebca`, `6fdcf1f6` |
| apinode | Q3 | one store-error rule, one endpoint.Persistence port with ctx and error (with Q-port-3) | the coordinator (wizard-2's list) | done `c8c1f660` |
| apinode | Q4 | deprecate the five bridge methods now | the coordinator (wizard-2's list) | done `b3cff6b6` |
| apinode | Q5 | keep as documented | the coordinator (wizard-2's list) | no change (GrantFabric's doc says it) |
| apidev | Q-apidev-1 | NoFeatures beside Features | the coordinator (wizard-2's list) | done `b358db05`, `91d1f1c2` |
| apidev | Q-apidev-2 | a new BLE port, the old one deprecated (against the unit's optional extension) | owner decision 114 | done `8ef44b44` |
| apidev | Q-apidev-3 | a host-made device.Type is supported; the registry returns copies | the coordinator (wizard-2's list) | done `62a4f36c` |
| apidev | Q-apidev-4 | SetMeasuredTemperature now; the sibling couplings one findings row | the coordinator (wizard-2's list) | done `056e82c8`; `F-db021da0-21` |
| apispec | Q1 | the author's write rule and the host's veto as two hooks | the coordinator (wizard-2's list) | done `8809cb84`, after the 5b-A A1 read |
| apispec | Q2 | Handle is the command method everywhere | owner decision 112 | done `ec49ca6c` |
| apispec | Q3 | a bound emit; the endpoint-taking emits deprecated | the coordinator (wizard-2's list) | done `b358db05` |
| apispec | Q4 | the quiet interval in the generated definition; the version as matter.js | the coordinator (wizard-2's list) | done `290b7636`, `6e1d83b8` |
| apispec | Q5 | a hand-written definition is supported, with a request check | owner decision 113 | done `13f1a70a` |
| write | Q-write-1 | the verdict in the write result, by name; ChangeNamingWriter | the coordinator (wizard-2's list) | done `a14b0074` |
| port | Q-port-1 | methods on the area interfaces, one announced break | owner decision 107 | done `73236af5` |
| port | Q-port-2 | a host-supplied store.Sealer, opt-in, in its own wave | owner decision 108 | done `c45bf3b3` |
| port | Q-port-3 | matter.js's rule for every change a controller causes (with apinode Q3) | the coordinator (wizard-2's list) | done `c8c1f660` |
| port | Q-port-4 | additive, session-aware fail-safe hooks; the old three deprecated | the coordinator (wizard-2's list) | done `ec8d183f`, `9b8fd3ae` |
| port | Q-port-5 | durable owed-event marks in the store | owner decision 109 | done `73236af5`, `9f7d1b85` |
| port | Q-port-6 | one resumption record per peer for both roles (with peer Q4) | the coordinator (wizard-2's list) | done `73236af5`, `17073672` |
| peer | Q1 | shape 1: establish on client/, reuse where the session lives, composed in node | owner decision 115 | done `2b0692b3`, `f93dda75` (ADR 0032) |
| peer | Q2 | the client stack built on demand when Config.Client is nil | the coordinator (wizard-2's list) | done `2b0692b3`, `4c968557` |
| peer | Q3 | re-establishment stays on UDP; the TCP preference is a findings row | the coordinator (wizard-2's list) | `F-db021da0-50` |
| peer | Q4 | one record per peer, both roles (with Q-port-6) | the coordinator (wizard-2's list) | done `17073672` |
| peer | Q5 | wave H, with bridge/reply.go in its files | the coordinator (wizard-2's list) | done `1e75c43f` |
| redact | redact-1 | shape A: one generated scope table, enforced behind readOne | owner decision 110 | done `2b253a95`, `40c1767e`, `e62fe4c6` |
| redact | redact-2 | refuse: FAILURE for the path, as connectedhomeip | owner decision 111 | done `9ccd46e2`; `BD-Matter-FabricScopedEntryWithoutIndexRefused` |
| redact | redact-3 | a stdlib-only declarer port in contract/ (with apispec Q5) | owner decision 113 | done `3cb9a8c6` |
| redact | redact-4 | the event gate from the same generator, in wave A | the coordinator (wizard-2's list) | done `2b253a95`, `40c1767e` |

## The owner's decisions of this phase

Decisions 105 to 119 and 116a; 105 to 117 and 116a are rows of `decisions.md`
(`561f0170`). Decisions 116, 117 and 116a are worded as their rule; 118 and
119 were taken on the diff review's findings.

| # | on | decided | where |
| --- | --- | --- | --- |
| 105 | white-label OTA identity | keep the attested vendor and product pair, log a mismatch; the refusal rule only after a read | done `a89b33a0`; `BD-Matter-OTA-AttestedIdentityKept`; the refusal rule not built |
| 106 | branch protection | `test (linux/386)` and `workflow lint` required on main | set by the owner's choice through the API on 2026-11-16; `31fddb1a` |
| 107 | the store port's growth | methods on the area interfaces, one announced break | done `73236af5` |
| 108 | sealing secrets at rest | a host-supplied store.Sealer, opt-in, in its own wave | done `c45bf3b3` and wave S; `BD-Matter-SealedAtRest` |
| 109 | owed fabric hooks | durable owed-event marks in the store | done `73236af5`, `9f7d1b85`; `BD-Matter-OwedFabricEvents` |
| 110 | where the redaction runs | one generated scope table behind readOne | done `2b253a95`, `40c1767e`, `e62fe4c6`; `BD-Matter-FabricScope-OneMaskBelowEveryServer` |
| 111 | a fabric-scoped entry without a FabricIndex | FAILURE for the path, as connectedhomeip | done `9ccd46e2`; `BD-Matter-FabricScopedEntryWithoutIndexRefused` |
| 112 | the meaning of Handle | the command method everywhere | done `ec49ca6c`, after the 5b-A A2 read |
| 113 | hand-written spec definitions | supported, with a request check and a declarer port | done `13f1a70a`, `3cb9a8c6` |
| 114 | the BLE peripheral port | a new port, the old one deprecated | done `8ef44b44` |
| 115 | the peer path's shape | shape 1 (ADR 0032) | done `2b0692b3`, `f93dda75`; `BD-Matter-PeerPath-TwoStacksOnePath` |
| 116 | destinations of a ledger download | the rule for URLs the ledger names | done `c83a59e6`, `06e2b7cc`; `BD-Matter-Controller-DclDownloadDestination` |
| 116a | decision 116 refined | CRL points http or https; the proxy kept, the host judged before it | done `06e2b7cc`, `4b524acf` |
| 117 | an OTA record that states no checksum | refused unless an option admits it | done `0f3cd37a`, `ae8e501f`; `BD-Matter-OTAImage-LedgerChecksumRequired` |
| 118 | the controller's resumption records at rest | controller.Config takes the same store.Sealer | done `1c4c465b`, `9adfdfbb` |
| 119 | what the attested pair means | recorded only for a verified chain without an error finding | done `54c349aa` |

## Noticed by the fix agents, the guard runs and the coordinator, not fixed where it was noticed

No severity: none of these was reviewed or verified as a finding. What a read
took up is in the findings above and is not repeated here. The ids are this
register's (`N5b-<group>-<n>`).

| ID | noticed in | what | disposition | where |
| --- | --- | --- | --- | --- |
| N5b-seal-1 | `fix-seal.md`; the coordinator's list | re-sealing under a new key (rotation) and an unseal path for a downgrade | phase 6 (the coordinator's list) | `store/seal.go` |
| N5b-guard-1 | a guard run at `1a3b760f`; the coordinator's list | `fabrictest:TestControllerRecoversFromASilentRestart` failed once beside six agents, passed 35 times on a rerun | open: findings row `F-db021da0-58` — phase 6 (the coordinator's list) | `fabrictest` |
| N5b-guard-2 | the rework's guard runs; the coordinator's list | `mdns:TestSockets_ReceiveAfterJoinNew` failed under load, passes alone | phase 6 (the coordinator's list) | `mdns` |
| N5b-guard-3 | the rework's guard runs; the coordinator's list | `bridge:TestSingletonCompletedHandshakesHoldNoWindows` failed under load ("extension length exceeds buffer"), passes alone; the message is to be checked | phase 6 (the coordinator's list) | `bridge` |
| N5b-guard-4 | the rework's guard runs; the coordinator's list | `controller:TestControllerServesTheCameraRequestor` failed under load, passes alone | phase 6 (the coordinator's list) | `controller` |
| N5b-guard-5 | the guard runs from wave F on; the coordinator's list | the BLE Wi-Fi light example's lint reports SA4023 on darwin only (wpasupplicant's other-OS file always errors); a GOOS=linux lint is clean | phase 6: lint the nested example with GOOS=linux in its Makefile target (the coordinator's list) | `examples/ble-wifi-light`, `Makefile` |
| N5b-guard-6 | the guard run at `5ec7cba5`, at `3ed1a358` | the nested modules stay on the deprecated BLEPeripheral until their next release | fixed `883a8b8d`, `495e7975` (nolint, owner decision 114); the move is phase 6 with the nested release (the coordinator's list) | `ble/bluez`, `examples/ble-wifi-light` |
| N5b-guard-7 | the guard run at `5ec7cba5` | `TestControllerTakeOverDropsFormerResumptions` was flaky: the second controller may store a fresh record | fixed `883a8b8d`: the test asserts the former record is gone | `controller` |
| N5b-guard-8 | the guard run at `15f65912` | `TestSleepRatchet` counted 60 sleeps against 59 after wave G-DD's test | fixed `7c865eea`: the record is published synchronously | `examples/reference-bridge` |
| N5b-guard-9 | the guard run after wave F | the root deprecation and walk tests read nested checkouts under `.claude/worktrees/` | fixed `704c98ac` with DR5b-c1-01 | the repository walks |
| N5b-guard-10 | the guard run after wave F | the deprecation policy test refused a wording in `endpoint/assembler.go`; cite-check found an ambiguous Endpoint.ts citation | fixed `80364733`, `6422429c` | `endpoint/assembler.go`, `node` |
| N5b-notes-1 | the notes pass's writer; the coordinator's list | whether the scenes server answers FAILURE on a table that does not load, as the references do, was left untraced | fixed in part `77b95e2d` (FabricSceneInfo, 5b-dr-questions c2-Q2); whether matter.js models an unloaded table was not read — phase 6 | `cluster/core/scenes_management.go` |
| N5b-notes-2 | the notes pass; the coordinator's list | what a controller is answered when the store refuses in commit phase one was left untraced | phase 6 (5b-F Q5; the coordinator's list) | `node/nonvolatile.go` |
| N5b-fctl-1 | `fix-fctl.md` | UpdateInfoFromURL still downloads the whole image into memory to read its header — the class of R2-P5-port-14, not in the brief | phase 6 (the register editor's placement) | `controller/softwareupdate/images.go` |
| N5b-afacade-1 | `fix-afacade.md`; the coordinator's list | decision 17's refusal, read again at the pin, covers 36 entries and 21 bare types and the feature-selected ones — larger than the 19 the decision named | open: for the owner, in the phase summary (the coordinator's list) | `device` |
| N5b-386-1 | the coordinator's linux/386 run | under emulated i386 every multicast JoinGroup fails with "protocol not available"; CI runs 386 natively | struck: the environment (the probe's); the emulation's mechanism not read | `mdns:TestSockets_JoinNewInterfaces` |

## Unread

What each review said it did not compare, read or run, summarised per review;
all of it is phase 6 (the full pass reads every hand-written package again).
The lists are the reviewers', not re-measured here; where a later read closed
an entry, the row says so. `dr5b-code-2` states no such list beside its
findings.

| Unit | area | not read | phase |
| --- | --- | --- | --- |
| apinode | the node facade | ADR 0018 and 0030 by their headings only; most Config sub-structures by the go doc map; what a real advertiser publishes for a window opened before Start; a store failure on a bridged write, and a host replacing a facade hook, read and not run; the specification's PH width (not in the tree); how a matter.js host sets the pairing hint | 6 |
| apidev | the device-side contract | no connectedhomeip source; the other 11 quiescers; an event on an endpoint that does not host the cluster, downstream; FromSpec composites against the assembler; AccessControl's featureMap body; the doc comments' other citations; advertisement before registration on a radio; no race run | 6 |
| apispec | cluster/spec | matter.js's bitmap validation and the DoorLock requests' conformance (F-db021da0-20); Zones after a host OnWrite, run; the endpoint-0 event, run; findings 06, 16, 17 without a probe; event payload validation in matter.js; concurrent writes with a Sink; the self-held Q wrappers one by one; chip's CopyString | 6 |
| write | the write seam | the 85 MatterWriteChanged implementers beyond fan and WindowCovering; whether a reactor's change joins matter.js's transaction (F-db021da0-45); write-05's consequence end to end | 6 |
| port | the store and driver ports | no probe test; N-finp-4 and F-db021da0-47 of plan.md's row; Server.cpp's start-up cleanup and Tx.ts's rollback; whether matter.js seals a secret (read later by 5b-F); connectedhomeip's answer for a removed fabric's lock record; endpoint/sqlitestore; calls after Stop or Close | 6 |
| peer | the peer path | no probe test; whether a ServerNode's peers share the server's port and parameters (peer-fact-18, unverified); chip's CheckCommandFlags tests; by_design for UDP-only re-establishment; whether real controllers accept CASE over TCP as responders | 6 |
| redact | generated redaction | whether im's chunker works on encoded entries (left to wave A); removing the Redacted fields in this release line; matter.js's test bodies to port; connectedhomeip's per-cluster servers; the write path of fabric-scoped lists | 6 |
| verify, confirm | the verifiers and the confirming read | the reference halves of the needsReference findings (the confirming read took 30); the reviewers' probes not rerun; the PH width; the peer path read, not run; matter.js after ThermostatServer.ts:514; the rollback on a refused commit-phase store; matter.js's DoorLock answer for a seeded record; one fact line sampled out; peer-fact-18 and redact-fact-14 unverified | 6 |
| dr5b-code-1 | the diff review, bridge, im, endpoint | the BLE registration with a slow GATT fake (none in the tree); c1-05's effect inferred | 6 |
| dr5b-code-3 | the diff review, store, controller, client, secure | the 386 test run itself (closed later by the coordinator's run); the concurrent-download case read, not run | 6 |
| dr5b-fable | the diff review, security and reference | group invokes and the CSR binding; the controller's served camera Requestor against the mask; secure/sigma, sessionparams, attestation and the peer path taken as the rounds closed them; connectedhomeip; node/nonvolatile.go's and root.go's settings values | 6 |
| rework read | the review of the rework | bridge/write_hold.go's callers; the tests' bodies beyond the sealed test; docs beyond the cited lines; ci.yml, script/requiredchecks, the walk guards; Bridge.Stop's BLE takedown | 6 |
| Fable reads | the reference statements of the waves | the reads' own unverified lists — among them matter.js's unloaded scene table, its DoorLock answer for a seeded record, the sibling branch of the thermostat before :490 and after :514, what a proxy does with the legacy spellings — named in each read | 6 |

## Counts

Generated from the rows above, at `fa118739`: a row is counted by the head of
its disposition — `fixed`, `fixed in part`, `parity: a recorded divergence`,
`decided`, `closed`, `struck`, `not a finding`, `open: findings row`, `phase
6` — or as held back. A fixed row that names a `by_design` entry, a findings
row or a part left open is counted as fixed.

The 240 findings, by their first disposition:

| disposition | units | diff review | rework | reference reads | all |
| --- | --- | --- | --- | --- | --- |
| fixed in part | 3 | 0 | 0 | 1 | 4 |
| fixed | 84 | 20 | 4 | 78 | 186 |
| a recorded divergence | 0 | 0 | 0 | 12 | 12 |
| decided | 0 | 0 | 0 | 3 | 3 |
| closed by a later run | 0 | 1 | 0 | 0 | 1 |
| struck | 1 | 2 | 0 | 2 | 5 |
| not a finding | 0 | 0 | 1 | 0 | 1 |
| open: a findings row | 1 | 0 | 0 | 9 | 10 |
| open: phase 6 | 1 | 6 | 2 | 4 | 13 |
| held back | 0 | 0 | 2 | 3 | 5 |
| all | 90 | 29 | 9 | 112 | 240 |

By the severity their rows carry — units: 1 high, 12 medium, 71 low, 6 note;
diff review: 3 medium, 13 low, 11 note, 2 info; rework: 1
medium, 3 low, 1 note, 2 info, 2 held back; reference reads: 1 high, 14
medium, 55 low, 9 info, 30 without one, 3 held back.

Of the 190 fixed or fixed in part: 16 were corrected by an item of a later
read, 8 name a `by_design` entry and 23 a findings row.

What earlier phases handed to phase 5b: 64 rows for the units' carried items —
3 fixed in part, 56 fixed, 1 decided, 1 closed before this phase, 1 struck, 2
open: phase 6; and 16 rows phase 5a's register placed in 5b — 13
fixed, 3 open: phase 6.

The units' questions: 30 rows, 10 answered by the owner and 20 by the
coordinator. The owner's decisions of this phase: 16 rows (105 to 119 and
116a).

The 16 noticed items: 1 fixed in part, 5 fixed, 1 struck, 1 open: a findings
row, 1 open: other, 7 open: phase 6. Unread: 13 rows, all phase 6.

**Held back: 5 rows** — findings still open, for phase 6: RW-i2, RW-i3, 5b-dcl
F6, 5b-dcl R7+R8, 5b-dcl R9. The 3 rows of the disclosure class fixed by phase
5b's pull request (#534) — R2-P5-redact-01, DR5b-F-01 and the carried item
N4-frsec-1 — are in the tables since 2026-11-18, with the release that
carries their fix (owner decision 126); the counts above include them.
Nine rows held back until #534 merged are in the tables since 2026-11-16
(owner decision 126): 5b-H H2, 5b-wire F-H2-a, 5b-wire H2-n, TM R6.2,
N3-net-4, 5a-2 F2, 5a-3 F2, 5a-4 F2, N5-rw2-1; the counts above include them.
The rows phases 3 and 4 hold back for phase 6 stay in their files as they
were.

## Not placed, not verified

- Held back by the register editor's judgement, not by a list: 5b-H H2, RW-i2,
  RW-i3, 5b-dcl F6, 5b-dcl R7+R8, 5b-dcl R9. Judged public: the DCL read's
  fixed items, as what the code does now; 5b-dcl F7 (the class list, which the
  published docs state); 5b-seal R2-2 and F4 (limits `docs/security.md`
  states); R2-P5-redact-04 (the contract sentence, fixed).
- The findings register still lists F-db021da0-41 and F-db021da0-47 as open at
  `fa118739`, although R2-P5-port-04 (`73236af5`) and R2-P5-write-04
  (`1976e84a`) are fixed; the notes pass closed sixteen rows and named
  neither. Not this file's to change.
- RW-01: fixed in `ea20cebf`, `43e2027e`; the rework read's re-check closed it
  and found RW-04 (fixed `d595a371`).
- Owner decisions 118 and 119 are in `decisions.md`.
- The result file has 66 carried entries for 64 items: F-db021da0-19 and -34
  were given to `port` and `peer`.
- The fix notes name the commits their agents made in worktrees; after the
  rebases the branch's ids differ. A fixed row's commit was found by the ID in
  the branch's messages or by the subject; the waves whose messages name no ID
  (H, DCL, the redaction wave, the rework) were mapped by subject from their
  fix notes.
- 59 of the 262 commits are named by no row: `ee7982f4`, `9aa963c9`,
  `8d34502d`, `9615d0d2`, `80900135`, `b6e92d68`, `b0005d54`, `d4e9604c`,
  `4c4fae63`, `91af89ee`, `8d2353fc`, `ceeaec46`, `3e50609d`, `0d734312`,
  `ab99a347`, `de24e220`, `c27e5a30`, `38735a99`, `8e1cd6ac`, `01bdf24e`,
  `2b33c9de`, `f536fa35`, `e59d5c68`, `653a6b4f`, `3d025d88`, `36e66551`,
  `599cdfb8`, `4b593bdd`, `cbc769e5`, `cf9ef186`, `48104a44`, `18d9c8ab`,
  `69185f8a`, `519321eb`, `f1481731`, `c6f3f83d`, `015333d2`, `750a3e98`,
  `b9a320ac`, `b0fe065b`, `68e29207`, `143ae98e`, `0de26b66`, `acaf02c1`,
  `1402db69`, `1562741c`, `39d72429`, `ff716943`, `a9a952e9`, `ccc40303`,
  `c89f922f`, `a81c7536`, `1268df16`, `02698658`, `842cd846`, `c48cbb33`,
  `f6348c3a`, `2eb7d95f`, `5206fa30`. They were placed by their subjects —
  regenerated snapshots, the CHANGELOG and migration rows, coverage floors,
  lint follow-ups and the notes — not read one by one.
- The diff review and the rework's read have reports, not result files; their
  severities and verdicts are their reports'. The six fact notes of the
  reference reads are counted by the items that set a change or stay open, not
  by the facts they confirmed.
