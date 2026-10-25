# Report CK: the facade gaps of go-fabric-ui U1 and go-ha2fabric H3, endpoint-number reservation, the reference CLI on `controller.New`

Brief: `notes/briefs/2026-10-25-CK-facade-gaps-ui-and-numbers.md`. Branch
`m5/facade-ui-gaps` from `v0.12.1` (09ed2507). Version: **v0.12.2**
(additions, one behaviour change of `Aggregator.Remove` without a store
migration, no CLI flag moved; `make apidiff-check` finds no incompatible
change). Owner paths untouched: nothing under `secure/`, `commissioning/`,
`client/handshake`, ACL or the four `cluster/core` servers changed; the new
functions call `setup.IsValidPasscode`, `spake2`/`bridge.Random*` and the
session and subscription managers' exported readers.

## §1 The six U1 gaps — 0cb70676

- **1.1** `Node.Observe(node.Observers) (unsubscribe func())`
  (`node/observers.go`): hooks run after `Config.Observers`, in
  registration order, on the event's goroutine; the unsubscribe is
  idempotent (`sync.Once`). Every call site (fabric added, removed,
  updated, changed; commissioning complete; window transition) walks one
  list. Cites matter.js `Observable.ts:55`/`:70` (`on`/`off`). Test:
  `TestObserveAfterStart` — a set registered after `Start` sees a
  commissioning and a fabric removal, after the config hook, in order; an
  unsubscribed set sees nothing.
- **1.2** `Node.Window() node.WindowSnapshot`: the observer's value at any
  time. `WindowSnapshot` gains `Kind` (`WindowNone`, `WindowOwn`,
  `WindowBasic`, `WindowEnhanced`, with `String`), `OpenedAt`, `Deadline`,
  `Timeout`; the discriminator is the advertised one, so the own and basic
  windows report theirs. Below it `bridge.CommissioningWindow.Timing()`
  (opened-at, expires-at, unclamped timeout on the window's clock). Cites
  `DeviceCommissioner.ts:344-350`, `:236-266`, `:252`. Test:
  `TestWindowSnapshot`.
- **1.3** `Node.SetupPayload() (node.SetupPayload, error)`: discriminator,
  passcode (by design, said in the doc comment), vendor and product id, QR
  and manual code. Cites `CommissioningServer.ts:405-414`, `:417-450`.
  Test: `TestSetupPayload`.
- **1.4** `BridgedDevice.Clusters []node.ClusterInfo{ID, Name, Revision}`
  and `BridgedDevice.DeviceTypeRevision`, for every endpoint including
  parts and a host Snapshotter's: `Topology` walks the mounted servers
  (`endpoint.ClusterServers`, the set the Descriptor's ServerList comes
  from), names and revisions from the generated `schema.ClusterName`,
  `ClusterRevision`, `DeviceTypeRevision`. `contract/` untouched. Cites
  `DescriptorServer.ts:275` (`#serverList`). Test: `TestTopologyClusters`
  (a BridgedNode with an OnOffLight part).
- **1.5** `node.NewWindowCredentials(timeout)`; the setup-PIN rule is one
  function: `bridge.RandomPasscode` now re-rolls on `setup.IsValidPasscode`
  instead of its own list (the same twelve values). Cites
  `PaseClient.ts:55-71`, `PairingCodeSchema.ts:160-162`, `:171-178`,
  `CommissioningClient.ts:522-525`. Test: `TestNewWindowCredentials`.
- **1.6** `Node.SessionDiagnostics() node.SessionDiagnostics` (`AsOf`,
  `Sessions []SessionInfo{ID, FabricIndex, PeerNodeID, LocalNodeID, PASE,
  Subscriptions, LastActivity, LastPeerActivity, Idle, PeerIdle}`,
  `Occupancy{Live, Reserved, Capacity, Free}`), and `Node.Clock()`. Cites
  `NodeSession.ts:183/238/250/270`, `Session.ts:56/65/142-146`. Test:
  `TestSessionDiagnostics`. `advanced-seams.md` no longer lists the
  `SessionManager`/`SubscriptionManager` aliases as facade field types;
  the `secure/…` and `im/subscription` seam sections name
  `SessionDiagnostics` as the reading route.

## §2 Endpoint-number reservation per stable key — 333e5d42

- `Aggregator.Remove` (and `Partition.Remove`, `Batch.Remove`) reserves the
  removed key's number and its parts' numbers; `Add` of the key hands them
  back (its record was never deleted) and releases the reservation;
  `Aggregator.Forget` / `Partition.Forget` / `Batch.Forget` frees them
  (the record and the reservation go); `RetirePartition` drops a retired
  scope's reservations with its numbers.
- Mechanism: the reservation is a settings row
  `endpoint_number_reserved.<key>` holding the JSON list of the removed
  tree's keys (no SQL migration; the endpoint store's table has no free
  column and its `endpoint.Store` port is host-implementable). The
  assembler gets `endpoint.Snapshot.Retain []SourceKey`: a model-complete
  snapshot keeps those records although absent. Batch reservation changes
  are applied in call order after `fn`, before the reassembly, outside the
  Aggregator's lock (`node/reserve.go`).
- Parity: `node/endpoint_number_reservation_parity_matterjs_test.go` ports
  `BridgeTest.ts:231-269`, `:271-341`, `:343-425` and
  `ServerNodeTest.ts:1114-1142` (close/re-add, multiple close and re-add
  across a restart, delete/re-add across a restart, a deleted subtree),
  plus a Go-only `RetirePartition` case;
  `endpoint_number_conflict_parity_matterjs_test.go` gains "keeps a closed
  endpoint's number for its return" (`Snapshot.Retain` on the assembler).
  `TestAggregatorAcrossRestart` now pins Remove keeps / Forget frees across
  a restart and `MarkComplete`.
- Docs: `docs/getting-started/device.md`, `docs/guides/host-view.md`,
  `advanced-seams.md`, the Aggregator row of `docs/matterjs-comparison.md`.
- The reference bridge does not call `Remove`; its composition and
  FeatureMap did not move — no chip-tool run.

## §3 go-fabric-server note

The JSON route of CJ §1.1 lets go-fabric-server drop its `internal/wire`
exception; that is S3's work. Nothing changed here.

## §4 The reference CLI on `controller.New` — 40823f2b (lint fix 10e8a766, snapshots e47998dc)

- New on the facade: `controller.Config.CAKey crypto.Signer`
  (`ca.Config.RootSigner`; matter.js has no such seam,
  `CertificateAuthority.ts:53`, `:169`) and `Controller.Peers() *peer.Set`
  (`Peers.ts:93`), a seam beside `Registry`, `Commissioner`, `Fabric`. A
  failed `controller.New` now closes the `Config.Listener` it was given
  (before, a failure ahead of the network step leaked it). Test:
  `controller/cakey_test.go` `TestNewCAKey`.
- `examples/reference-controller/cli/stack.go`: `openStack` composes the
  trust set (`attestation.Trust` over snapshot and online DCL) into
  `Config.Attestation`, opens `--ca-key` into `CAKey`, opens the socket
  with the `--listen` IPv4 preference into `Listener`, and calls
  `controller.New` with `ManualStart` and the test hook's `Names`. The
  CLI's own exchange manager, DNS-SD browser, peer set, subscriptions,
  commissioner and registry construction are gone.
- `pair.go`: `cmdPair` is `Controller.Pair` with `PairOptions{BLE, WiFi,
  Thread, AutoSubscribe, OnAttestationFailure, Edit (NodeID)}`;
  `findCommissionable` and `dialBLE` are gone. `--passcode` with
  `--discriminator` becomes a QR code (`codeOf`, no vendor/product, on-IP)
  that `Pair` decodes. Flags and output unchanged; `ErrNoDevice` keeps the
  old "no commissionable device with … answered" message.
- `args_test.go` (codeOf round trip through `controller.ParsePairingCode`)
  and `pair_ble_test.go` (unchanged, now through `PairOptions.BLE`) pass on
  the facade route; `cakey_test.go` reads `ca.DefaultSettingKey`.
- Docs: `docs/getting-started/controller.md` (`CAKey`, the seams, the
  reference controller on `controller.New`), `advanced-seams.md`,
  `persistence.md` line reference.

## Guards

`df -h /` before the runs: 117 GB free (52 %); `/tmp` (tmpfs) 3.8 GB free.

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` (build, build-32bit, vet, archcheck, apidiff-check, test, lint, cover-check, bluez-check) | e47998dc | pass (the first run at 40823f2b passed every test and stopped at lint on one revive comment, fixed in 10e8a766) |
| `make race` | 40823f2b | pass |
| `make race` | e47998dc | pass (the second green full run; two other full runs, at 40823f2b and e47998dc, each failed one test in a package this bundle does not touch, both green alone under `-race`: `client/interaction` `TestSustainedSubscriptionICD` (`-count=5`) and `controller/client` `TestIcdClientParity/key_refresh_outcome/…pending_key_again…` (a 180 s wait under load; 4.6 s alone, `-count=2`) — the ICD-client flake family the launcher names) |
| `make reachability-check` | e47998dc | pass (3 unreached, on the ratchet; snapshot regenerated in e47998dc) |
| `make cite-check MATTERJS_DIR=…/matter.js` | e47998dc | pass (0 hard errors, drift 259, ratchet unchanged; the first run at 40823f2b found one new drift, a test comment whose citation sentence also named `BD-Matter-EndpointNumber-Monotonic`, split into two sentences in e47998dc) |
| `make apidiff-check` | 40823f2b | pass (no incompatible change) |
| `go test ./script/...` | e47998dc | pass |
| `mkdocs build --strict` | 40823f2b | pass |
| `make stress-flakes` | 40823f2b | pass |
| chip-tool | — | not run: the reference bridge's composition and FeatureMap did not move |

## Differences from the brief

- §1.2: `Kind` has a fourth value, `WindowNone`, as the zero value for a
  closed window. An administrator's basic window does not exist here (the
  bridge never opens one: no BC feature), so the three open kinds cover
  every window. The timing needed one accessor below the facade,
  `bridge.CommissioningWindow.Timing()` (`bridge` is not an owner path).
- §1.5: `node.NewWindowCredentials` is `NewCommissioningCredentials()
  .WindowCredentials(timeout)`; `node.ValidateSetupPIN` already existed as
  the exported rule. The rule moved from `bridge.RandomPasscode`'s own map
  to `setup.IsValidPasscode`, which the node's checks already used.
- §1.6: `SessionInfo` adds `Idle` and `PeerIdle` (ages against `AsOf`),
  so a host does not compute them on another clock.
- §2: no migration and no change to `endpoint.Store`: the reservation is a
  settings row, the assembler's gate is `Snapshot.Retain`. The name of the
  free call is `Forget`, on the Aggregator, the partition and the batch.
- §4: `Controller.Pair`, not `Controller.Discover`: `Discover` returns
  every device found once its timeout passes (15 min default), `Pair`
  takes the first that answers, which is what `pair` does. No
  `Config.Trust` field: `Config.Attestation` already takes the composed
  trust store. One new seam the brief did not name, `Controller.Peers()`:
  the CLI's own node is reached on a known address through the peer set.

## Decisions

- v0.12.2: the `Remove` change is behaviour, not API, needs no host
  migration (a stored record simply stays) and moves no flag; the
  CHANGELOG lists it under Changed with the `Forget` route.
- A stored key a restarted host neither re-adds nor removes is still
  collected at `MarkComplete` (the existing ModelComplete gate); only
  keys a `Remove` reserved survive it. This keeps `MarkComplete`
  meaningful for the hosts that call it (go-ha2fabric, go-fabric-server).
- A reservation that cannot be read keeps the record (the safe
  direction: a number survives rather than goes to another device).
- Revisions in `ClusterInfo` are the schema's, as the brief says, not a
  read of each server's ClusterRevision attribute.

## Left out

- A listing of the reserved keys (`Aggregator.Reserved`) for a host that
  wants to sweep them; nothing asked for it.
- chip-tool: the reference bridge's composition and FeatureMap did not
  move.

## Questions

- **Q1 (owner).** Should a stored key that a restarted host neither
  re-adds nor removes keep its number too, as matter.js does (no
  collection at `MarkComplete` at all)? Then `MarkComplete` has no effect
  left and goes (a Break). Symbol needed: none new; `Aggregator.MarkComplete`
  removed or documented as a no-op.
- **Q2 (go-fabric-ui).** The UI can now drop its `Added.Spec()` walk and
  its `bridge.Random*` passcode rule: `BridgedDevice.Clusters`,
  `node.NewWindowCredentials`, `Node.Window`, `Node.SetupPayload`,
  `Node.SessionDiagnostics`, `Node.Observe`. No symbol needed.

## by_design candidates (for the owner to write)

- `BD-Matter-EndpointNumber-Monotonic`: its "Honest consequence of
  monotonic allocation: a source key removed and re-added later gets a
  fresh number" no longer holds for the facade's Aggregator — a removed
  key keeps its number (`ServerEndpointStores.ts:20-22`, `:117-123`;
  `ServerEndpointInitializer.ts:67-69` at 862ad75f); only `Forget` gives a
  re-add a fresh one (`ServerEndpointInitializer.ts:59-65`).
- `BD-Matter-EndpointGC-ModelCompleteGate`: the Aggregator now passes
  `Snapshot.Retain`; the remaining divergence is a stored key neither
  re-added nor removed after a restart, collected at `MarkComplete`, where
  matter.js keeps every stored number (`ServerEndpointStores.ts:33-45`
  load, `:109-130` assignNumber).
- The reservation lives in the settings table, not in the endpoint store
  (matter.js keeps the number in the endpoint's own store,
  `ServerEndpointStores.ts:13`, `__number__` per store).
- `controller.Config.CAKey`: matter.js keeps the CA key pair in its
  storage (`CertificateAuthority.ts:53`, `:169`); already stated at
  `ca.Config.RootSigner`, now a facade field.
- `node.WindowBasic`: a node's own window with host credentials has no
  matter.js counterpart (`DeviceCommissioner.ts:252` advertises the
  configured discriminator unless an administrator supplies one); the
  existing `OpenBasicWindow` divergence, now named in the snapshot.
