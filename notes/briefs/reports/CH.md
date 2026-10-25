# Report CH: ICD server and Check-In on the device, ICD client and parked subscription on the controller (M5 phases 23 and 24)

- **Branch:** `m5/phase-23-24`, from `origin/main` `cf772ba3`, with
  `origin/main` `019277ed` (the `v0.11.0` release) merged in at `9b5bd09f`.
- **matter.js:** read only at the pin `862ad75f`
  (`git -C ../matter.js show 862ad75f:<path>`). Line numbers below are
  that tree's.
- **Release:** `v0.12.0` for the bundle. Phase 25 becomes `v0.13.0`
  (roadmap row and status updated).
- **Labels:** none. The reference bridge's composition does not change:
  the ICD server mounts only when the host sets `node.Config.ICD`, and the
  reference bridge is mains-powered and sets none.
- **Owner review:** the Check-In codec `icd/checkin` went in early, in
  `fb9b9359`, for the owner's line-by-line review. It uses `secure/aesccm`
  as the AEAD. No owner path (`secure/*`, `client/handshake`, the four
  `cluster/core` security servers) was touched.

## Phase 23

### The `icd` package (`68044433`)

The package is a Go reading of `protocol/src/icd`, one file per matter.js
file:

- `counter.go`: `IcdCounter.ts`.
- `mode.go`: `IcdMode.ts`. The mode machine never cycles on its own.
- `backoff.go`: `IcdCheckInBackOff.ts`.
- `suppression.go`: `IcdCheckInSuppression.ts`.
- `registrations.go`: `FabricIcd.ts:45-60`, the device role.
- `sender.go`: `IcdCheckInSender.ts`.
- `icd/checkin`: the codec (`fb9b9359`, `CheckInMessage.ts:30-143`),
  with the CHIP vectors that matter.js uses.

`archcheck` places `icd` and `icd/...` in the security-and-storage layer.

### `store.ICD` (`d5d56335`)

- **The area:** `ListICDRegistrations`, `ReplaceICDRegistrations`,
  `LoadICDCounter` and `SaveICDCounter`.
- **The table:** migration v7 adds `matter_icd_registrations`, which
  cascades on fabric removal. The counter is the metadata counter
  `icd_management.counter`.
- **Implementations:** `store.Store`, `store/memstore`, and the
  `store/storetest` case with its cascade. Schema fingerprint `901125a0…`:
  7 migrations, 19 tables.

### `cluster/core.ICDManagement` (`86763d67`)

This is the port of `IcdManagementServer.ts:179-773`. It replaces the
stub.

- **Configuration:** `ICDManagementConfig` with `Validate`.
- **Commands:** `RegisterClient`, `UnregisterClient` and
  `StayActiveRequest`, with matter.js's statuses.
- **State:** a DataVersion tracker, `ICDFastPollingInterval` (200 ms) and
  the `Advertisement`.
- **Decoding:** the bridge decodes VerificationKey without the length
  bound (`bridge/fields_reader_icd.go`, `:58-75`).

### Bridge seams (`d4e475ba`)

The bridge gains the seams the server needs:

- `SendUnsecured`: the one-shot send of a Check-In.
- `PeerSessionAddress` and `ResolveOperationalAddress`.
- `ActiveSubscriptionSubjects`.
- `SetLocalFixedMRPBackoff`.
- `SetOnInboundActivity`.
- `SetICDAdvertisement`: the `ICD` and `SAT` keys, with
  `mdns.OperationalServiceConfig.OmitSessionIdleInterval` for LIT.

`transport/mrp` gains `SCOpcodeIcdCheckIn` (0x50).

### Facade (`3deb25c6`, `51e4c41a`)

- `node.Config.ICD` (`node.ICDConfig`).
- `Node.ICD()`, with `RequestActiveMode`, `EnterIdleMode`,
  `TriggerUserActiveMode`, `Active`, `Mode`, `SetOperatingMode`,
  `WithdrawForcedOperatingMode`, `OnModeChange` and `OnModeChangeAsync`.
- The server mounts on the root under `Sit | Lit`.
- Subscription persistence is off, the MRP pad is on, inbound activity
  feeds the mode machine, and an operating-mode change triggers a
  re-announcement.
- A LIT publisher grants its IdleModeDuration as MaxInterval
  (`ServerSubscription.ts:302-312`,
  `subscription.Manager.SetICDIdleModeDuration`).

## Phase 24

### Receive path

- `client/exchange.Manager.SetCheckInReceiver` (`022a0d01`,
  `SecureChannelProtocol.ts:149-180`).
- `Registry.receiveCheckIn` offers the payload to the ICD peer table of
  each fabric.

### The controller role in `icd` (`8d32a285`)

`PeerSchedule`, `PeerWakefulness` and `PeerTable`, after
`IcdPeerSchedule.ts`, `IcdPeerWakefulness.ts` and `FabricIcd.ts:62-272`.

### ICD client (`0810aa26`, `9ce97ab0`, `af622291`)

`controller/client.ICDClient`, after `IcdClient.ts:55-935`:

- **Operations:** `Register`, `Unregister`, `Forget` and `StayActive`.
- **Behaviour:** LIT auto-registration, Check-In receipt with the key
  refresh, and the decommission hooks.
- **Persistence:** the registration is stored in the client store under
  cluster `0xFFFF_FFFD`.
- **Errors:** `ICDMultiAdminError`, `ICDPeerAsleepError`,
  `ErrICDOperation`, `ErrICDAbandoned` and `TrustedEcosystemVendors`.
- **LIT support:** `Node.LITSupported` and `MinLITSpecificationVersion`.
  The `LongIdleTimeOperating` property now requires a specification
  version of 1.4 or later.

`client/peer`:

- `Peer.SetLongIdleTimeOperating` with the `LITMinIdleInterval` floor of
  5 s (`Peer.ts:59`, `:305-315`).
- `Peer.SetICDAwait` and the address monitor's ICD branch
  (`PeerAddressMonitor.ts:143-172`).

### Hold

- `HoldUntilICDPeerAwake` (`holdUntilIcdPeerAwake.ts:21-69`).
- `WithICDAwaitTimeout` (`ClientRequest.ts:50`).
- A Read, Write or Invoke to a sleeping registered LIT peer waits for its
  Check-In.

### Parking (`aa4ec565`)

`client/interaction/sustained.go` takes the ICD branches of
`SustainedSubscription.ts:116-337` and `:459-551`:

- Parking on the wake signal.
- The hold.
- Recreating the subscription on the priority connection when the
  operating mode flips (`peer.ConnectOptions.Priority`,
  `ExchangeOptions.Priority`).
- The registration feed (`SubscribeRequest.Wakefulness` and `PeerFed`).

### Armer (`c5fd6e2b`)

- `client/peer.RebootResubscribeArmer` (`RebootResubscribeArmer.ts:58-213`),
  with `RebootReturnTimeout`, a 30 s grace and `ArmerSession`.
- The context is injected, which keeps the package free of a
  session-manager dependency: `ClosePeerSessionsBefore`, `PeerLost` and
  `CloseSubscriptions`.
- The host wires `SessionAdded` and `ReportStarted`.
- `Arm` is public and has no caller yet. The OTA provider path of phase
  25 arms it.

## Ports

| matter.js file | Register | Cases | Go |
| --- | --- | --- | --- |
| `protocol/test/icd/CheckInMessageTest.ts` | ported | 16/16 | `icd/checkin` |
| `protocol/test/icd/FabricIcdTest.ts` | ported | 32/32 | `icd` (both roles) |
| `protocol/test/icd/IcdCheckInBackOffTest.ts` | ported | 5/5 | `icd` |
| `protocol/test/icd/IcdCheckInSenderTest.ts` | ported | 4/4 | `icd` |
| `protocol/test/icd/IcdCheckInSuppressionTest.ts` | ported | 4/4 | `icd` |
| `protocol/test/icd/IcdCounterTest.ts` | ported | 6/6 | `icd` |
| `protocol/test/icd/IcdPeerScheduleTest.ts` | ported | 11/11 | `icd` |
| `protocol/test/icd/IcdPeerWakefulnessTest.ts` | partial | 26/28 | `icd`; 2 missing: wall-clock steps |
| `node/test/behaviors/icd-management/IcdManagementServerTest.ts` | ported | 60/60 | `node` |
| `node/test/behaviors/icd-management/IcdModeTest.ts` | partial | 12/14 | `icd`; 2 missing: wall-clock steps |
| `node/test/node/ServerSubscriptionTest.ts` | partial (LIT cases added) | — | `node:TestServerSubscriptionLITParity` |
| `node/test/behavior/system/icd/IcdClientTest.ts` | partial | 48/100 | `controller/client`, `client/peer` |
| `node/test/behavior/system/icd/IcdMultiAdminErrorTest.ts` | ported | 8/8 | `controller/client` |
| `node/test/behavior/system/icd/IcdPeerAsleepErrorTest.ts` | ported | 2/2 | `controller/client` |
| `node/test/behavior/system/icd/holdUntilIcdPeerAwakeTest.ts` | ported | 4/4 | `controller/client` |
| `node/test/behavior/system/icd/litSupportedTest.ts` | ported | 6/6 | `controller/client` (internal) |
| `node/test/node/client/ClientNodeInteractionIcdTest.ts` | partial | 9/10 | `controller/client`; the icdLit network profile is missing |
| `node/test/node/client/ClientNodePhysicalPropertiesIcdTest.ts` | ported | 4/4 | `controller/client` |
| `protocol/test/action/client/subscription/SustainedSubscriptionTest.ts` | partial | 46/55 | `client/interaction`; the 9 missing cases are non-ICD (observer shapes, report-failure counting) |
| `protocol/test/peer/RebootResubscribeArmerTest.ts` | ported | 24/24 | `client/peer` |
| `protocol/test/securechannel/SecureChannelProtocolTest.ts` | ported | 2/2 | `controller/client` + `client/exchange` |

Notes on the ports:

- **Wall-clock steps.** `IcdModeTest` and `IcdPeerWakefulnessTest` each
  miss the two cases that use `MockTime.stepWallClock`: `clocktest.Fake`
  has one reading.
- **`IcdClientTest`, 52 missing:**
  - The transaction group (about 1054-1431) and most of the
    operation-interleaving cases (1432-2002). The Go client has no
    transaction model.
  - The storage-failure injection during a key refresh.
  - The routing cases of the sustained subscription, which are held by
    the `SustainedSubscriptionTest` port.
  - Three skips, each with its reason: a peer reporting specification
    1.3.0 (a node of this module always reports 1.6.1), a stale LIT seeded
    into the cache (no seam for it), and `stepWallClock`.
- **`PeerConnectionSocketOwnershipTest`:** `until` moves from "M5 phase
  24" to "M5 phase 25", which is where its TCP describe belongs. No
  `until: M5 phase 23/24` is left.

## E2E

`controller/client/icd_e2e_test.go` `TestICDEndToEnd` (`a407218d`) runs a
DSLS LIT device forced into LIT (idle 30 s, active 1 s, threshold 5 s) on
the fake clock, paced by `Site.Step`, `Settle` and `Eventually`:

1. The controller commissions the device with its sustained default
   subscription.
2. The client auto-registers on the LIT peer.
3. The device runs three idle cycles: the host calls `EnterIdleMode`, the
   clock advances 30 s, the host calls `RequestActiveMode`. The same
   subscription ID survives all three.
4. Re-registered under a subject the subscription does not cover, a read
   to the sleeping peer parks and completes on the Check-In.
5. `Unregister` clears the registration on both sides, and a read then
   passes at once.

It passes 5 of 5 runs under `-race`. It sits in `controller/client`, not
`node/`, because the helpers for the ICD pair live there.

## Guards

Every guard below ran at HEAD `241dd326`, and every one passed:

| Guard | Result |
| --- | --- |
| `make ci` (build, 32-bit build, vet, archcheck, apidiff-check, test, lint, cover-check, bluez-check) | exit 0 |
| `make race`, first run | exit 0 |
| `make race`, second run | exit 0 |
| `make stress-flakes` | exit 0 |
| `make reachability-check` (unreached 3, on the ratchet) | exit 0 |
| `make cite-check` (0 hard errors, drift 262, on the ratchet) | exit 0 |

These ran at `3d42c15a`. The only commit after it, `241dd326`, regenerates
the snapshots and changes no code:

- `make testparity` and `make testparity-check`: exit 0.
- `go test ./script/...`: exit 0.
- `mkdocs build --strict`: exit 0.

Skipped:

- `make chipdm-check`: the schema snapshot is unchanged.
- chip-tool: the reference daemon's composition and FeatureMap are
  unchanged.

The guard runs found five problems. Each is fixed in a commit of its own:

- **The ICD auto-registration flake (`6d08e563`).** The default
  subscription can go active before `client.Node` names it in `n.sub`.
  When that happens, the ICD client's LIT auto-registration trigger reads
  `SubscriptionActive` as false, and nothing triggers it again.
  `adoptDefaultSubscription` now gives the ICD client the edge a second
  time once the node holds the subscription. Under `make cover-check` this
  failed twice in three runs (`TestClientNodeInteractionIcdParity`, "LIT
  auto-registration" timed out after 180 s).
- **The hang in a controller's close (`cbaa3da0`).** The close waited on
  a CASE attempt whose last StatusReport needed the device's standalone
  acknowledgement, and that acknowledgement waits on the fake clock.
  `fabrictest.Controller.Close` now runs its shutdown under `Site.Run`, as
  `Restart` already does. Seen once: `TestIcdClientParity` timed out at
  10 minutes in its cleanup.
- **A racy precondition in two ports (`3d42c15a`).** The runtime
  SIT→LIT flip ports hold the DSLS device in SIT before they register.
  Otherwise the registration's own mode report races the SIT precondition
  that matter.js asserts on MockTime.
- **The bridge coverage floor (`cebf6bce`).** Bridge coverage was 82.3%
  against a floor of 83%. `bridge/icd_test.go` now tests the ICD seams in
  the bridge package, which brings it to 83.3%.
- **Lint (`332e4bd3`).** 33 findings. The ICD branches of `runSustained`,
  of the address monitor and of the default subscription are now helpers
  (`innerRequest`, `sustainGate`, `established`, `tearDownForRecreate`,
  `adoptICDAddress`, `icdSustainHooks`, `startICD`,
  `checkDataModelTest`). The fix also annotates the bounded conversions,
  uses `errors.New`, and clears the test findings.

Earlier runs on this branch, before the guards' fix commit `50d1ef1b`:

- `t1`: the node, bridge, cluster, store, mdns, im, icd and fabrictest
  tests. Green.
- `race1`: the ICD packages under `-race`. Green.
- `ci1` at `a407218d`: it stopped at `apidiff-check` because the two
  breaks had no CHANGELOG lines yet. Fixed in `604af11b`.
- `mkdocs build --strict`: green.

## Differences

- **The mode machine never cycles on its own.** This holds as in
  matter.js `IcdMode.ts`. The host drives sleep and wake through
  `Node.ICD()`, and the E2E test does the same.
- **No network profiles.** `ConnectOptions.Priority` only records the
  intent, because no connection is throttled. matter.js's icdLit profile
  arrives with phase 25 (findings register, "Network profiles"; the
  register's `NetworkProfileTest`). The brief cited
  "by_design `NetworkProfileTest`", but no such by_design entry exists.
  The code comments cite the findings register instead.
- **No transaction model in the ICD client.** Operations serialise on one
  operation slot with abandonment (`ErrICDAbandoned`), not on matter.js's
  transaction participant.
- **Go splits `FabricIcd` by role.** `icd.Registrations` holds the
  device's registrations and `icd.PeerTable` the controller's peers.
- **Where the keys live.** The device's ICD keys are in `store.ICD` (the
  table), not in a behaviour state. The controller's registration is in
  the client store under a pseudo-cluster.
- **Facade default.** A zero `MaximumCheckInBackoff` is IdleModeDuration.
- **Activity timing.** Inbound activity is noted when a message is
  received, not when the exchange ends.
- **`SecureChannelProtocolTest` runs on two layers.** The exchange manager
  sends nothing and closes. The registry routes the payload to a fabric.
- **Log keys.** The ICD code logs a peer's or a check-in client's node ID
  as `node_id`, so the guard `script/slogkeys` holds it to the existing
  core key.

## Decisions

- **The armer's call site is phase 25.** `Arm` is public and unused in the
  bundle.
- **Where the E2E test lives:** `controller/client`, beside the ICD pair
  helpers.
- **Log keys.** The ICD code logs a peer's or a check-in client's node ID
  as the core key `node_id`. Three area keys are added: `counter_start`,
  `last_counter` and `next_check_in`.
- **`BD-chip-ICD-Attrs-0x3-0x5`** is marked superseded (kept as a record),
  not deleted.
- **ICDM and ICDB in `docs/certifiability.md`:** one row, class (b), not
  applicable to the reference daemon and applicable to a host that sets
  `Config.ICD`. The generated block is untouched: its ICDM gaps already
  read "PICS false for the reference DUT".

- **The `fabrictest` change is outside the brief's list.**
  `Controller.Close` running under `Site.Run` is a harness change, needed
  for this bundle's tests to be stable. It changes nothing for a test that
  passed before.

## Left out

- The 52 `IcdClientTest` cases above, the icdLit routing case, and the 9
  non-ICD `SustainedSubscriptionTest` cases.
- No chip-tool run of ICDM or ICDB: the harness has no ICD DUT, and the
  reference daemon is unchanged.
- No `make chipdm-check`: the snapshot is unchanged.

## Questions

1. The roadmap now reads "`v0.12.0` (23–24), `v0.13.0` (25)" for
   milestone 5, but milestone 6 still says `v0.13.0`. Should milestones 6
   and 7 shift by one minor?
2. The ICD keys are stored in plain text in `matter_icd_registrations`, as
   matter.js stores them. The DoorLock credentials are encrypted at rest.
   Should the ICD keys be encrypted the same way?
3. The brief counts 25 `RebootResubscribeArmerTest` cases. The pin has
   24 `it` titles, and all 24 are ported.
4. ADR 0028 came in with the merge of `v0.11.0`, and this bundle adds no
   ADR. Is an ADR needed for the ICD split (device registrations versus
   controller peer table, no transaction model)?

## by_design candidates

1. **No network profiles; `Priority` records the intent.** Until phase 25.
   Cites `SustainedSubscription.ts:133-149` and `client/peer/connection.go`.
2. **The ICD client has no transaction model.** It uses one operation
   slot with abandonment. This explains the skipped `IcdClientTest`
   transaction group (`IcdClient.ts:980-991`).
3. **The device's registrations and keys sit in the store area
   `store.ICD`,** not in behaviour state (`IcdManagementServer.ts:777-791`).
4. **A zero `MaximumCheckInBackoff` in the facade is IdleModeDuration.**
5. **Inbound activity is noted on receipt** (`IcdManagementServer.ts:300-306`).
6. **`clocktest.Fake` has one clock reading,** so there are no wall-clock
   step cases.
