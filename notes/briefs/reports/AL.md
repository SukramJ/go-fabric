# Report AL — `controller/client.Registry`: the registry of client nodes (ADR 0020, stage 4, second half)

Branch `wave18/registry` from `planning/briefs-wave-18` (f72901c), pushed,
no PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`);
every `// Mirrors`/citation comment added cites lines read at the pin.
Each commit carries its regenerated reachability inventory (unreached is
**5** at the end; it was 8/6 at the intermediate §0/§4/§3 commits, while
sentinels and helpers their later § uses were still unreached).
Touched outside `controller/client`: `controller/client/sqlstore` (new),
`controller/exchange` (`Session.CreatedAt` and its test only),
`script/coverfloor/floors.go`, `CHANGELOG.md`. `notes/parity/`, ADRs and
the findings register are untouched. Inside `controller/client` besides
the new files: `store.go` (§1), `node.go` (record load/erase, `bind`,
the label latch, an unbound node's interaction accessor), `config.go`
(`Hooks.Decommissioned`), `interact.go` and `network.go` (they read the
interaction/peer through the locked accessor so an unbound node returns
`ErrNotCommissioned` instead of a nil dereference — see question 4),
`doc.go` (the "Not here" list), `export_test.go`.

| Commit | Item |
| --- | --- |
| 0ea5f61 `feat(controller/client): §1 Store.NodeIDs …` | §1 |
| 1310d73 `feat(controller/client): §2 the commissioning record …` | §2 |
| 578d0d5 `feat(controller/client): §0 the Registry …` | §0 |
| 6b76f2e `feat(controller/client): §4 commission and decommission …` | §4 |
| 9e9b205 `feat(controller/client): §3 the address book, its expiry and ForDevice …` | §3 |
| 07d8777 `feat(controller/exchange): §5 Session.CreatedAt …` | §5 |
| c19da15 `feat(controller/client): §6 the BasicInformation reactions …` | §6 |
| eaf6c68 `feat(controller/client): §7 the fabric-label reconciliation …` | §7 |
| 83ba3c2 `feat(controller/client/sqlstore): §8 the SQLite client.Store …` | §8 |
| 2194d84 `test(controller/client): §9 the live proof …; package doc and CHANGELOG` | §9 |
| (this commit) `notes: report AL` | — |

Order: §1 (NewRegistry lists the store's ids) and §2 (the restore binds
the peer from the record) before §0; §4 before §3 (the cull and
ForDevice consult the fabric operations); §5 before §6 (startUp's
cutoff). §0 carried stubs for the expiry timer that §3 replaced.

## Per section — what was built, what proves it

- **§1** `Store.NodeIDs`, `MemoryStore.NodeIDs` (sorted keys; `EraseNode`
  removes). `TestMemoryStore` extended.
- **§2** `record.go`: `Record` (fields 0–16 of the brief plus 17
  RotatingIdentifier, 18 PairingHint, 19 PairingInstructions, 20
  AdvertisedIntervals, 21 TCPSupport, 22 LongIdleTimeOperatingMode — all
  CC:1043-1090 fields `discovery.TXTData` can fill), `NetworkAddress`,
  `AdvertisedIntervals`, TLV codecs under `recordCluster 0xFFFF_FFFE`,
  `applyTXT` (RD:214-234), `applyDevice` (RD:167-245), `applyPeer`
  (CC:721-741, `addresses = [operationalAddress]` with the TODO),
  `device()`/`txt()` (RD:52-165), `knownDescriptor()` (CC:699-706);
  `Node.Record`, `Node.PeerAddress`, `Node.IsCommissioned`; the
  address-change Info lines (CC:600-635). The record is loaded in `New`
  and kept out of the image. `TestRecordRoundTrip`,
  `TestRecordDecodeErrors`, `TestRecordApplyPeer`, `TestRecordDevice`,
  `TestNodeRecord`.
- **§0** `registry.go`: `RegistryConfig` (+ `HooksFor`, decision below),
  `NewRegistry` (restore, peer bound from the record, `load_failed`
  skip, counter past `peerN` by leading digits as `Number.parseInt`),
  `Start`/`Stop`/`Close`/`Erase`, `Nodes`, `Commissioned`, `Get` (doc of
  CNS:71-75 and P:402-411), `GetByAddress`, `Size`, `ForAddress(addr,
  id)` (P:445-486), the errors. `TestRegistryConfig`,
  `TestRegistryRestore` (peer1, peer3, `peer12x` → next `peer13`, a
  foreign-fabric and a broken record skipped), `TestRegistryAutoStart`,
  `TestRegistryErase`.
- **§4** `fabricops.go` + `opcreds.go`: `runFabricOperation`
  (P:592-610), `Commission` (CN:162-166, CC:170-330), `Decommission`
  (CN:176-186, CC:340-446), the leave latch, `removalConfirmedBy`,
  `ErrPeerLeft`, `ErrPeerShutdown`, the OperationalCredentials wire
  shapes from `parity/schema.json` (cluster 62; attrs 1, 5; commands 8,
  9, 10; FabricDescriptorStruct 1–5, 0xFE). Scripted:
  `TestRegistryDecommission` (CurrentFabricIndex read, RemoveFabric(3),
  record cleared, node/peer/store gone, `Decommissioned` called, a
  concurrent Commission refused), `TestRegistryDecommissionRefused`
  (status 11: error, node kept), `TestRegistryDecommissionConfirmed`
  (leave event + failed invoke; lost session with `PeerInitiatedClose`;
  unconfirmed failure kept), `TestRegistryCommissionGuards`. The
  commission success path is proven live (§9 step 1): a scripted peer
  cannot run PASE.
- **§3** `addresses.go`: `DefaultTTL`, `ExpirationInterval`,
  `expirationOf`, the timer (P:615-649), the cull (P:651-712),
  `ForDevice` (P:358-377 with Factory.find, RD:37-50). Fake clock:
  `TestCullUncommissioned` (expired-only address deleted at the tick;
  fresh address survives to the node-level expiry; timer not armed when
  empty, cancelled when emptied), `TestCullCommissioned` (keeps the
  freshest of three, never deletes, single address untouched, a node
  under a fabric operation skipped, Stop/Start/Close on the timer),
  `TestForDevice` (refresh on a second sighting, waits on a
  decommission, not on a commission, anonymous device never matches).
- **§5** `exchange.Session.CreatedAt()` with the brief's doc;
  `TestManagerHandlePeerLossAsOf` extended.
- **§6** `reactions.go`: `onLeave` (P:785-835), `onShutdown`
  (P:837-858), `onStartUp` (P:860-893). Scripted:
  `TestReactionsNeedSubscription` (the three ignored, logged),
  `TestReactionLeave` (unknown index and another fabric's entry ignored;
  ours deletes the node, calls `Decommissioned`, loses the session),
  `TestReactionStartUp` (two sessions; the StartUp arrives over the
  newer — the older is lost, the newer kept), `TestReactionShutDown`
  (both sessions lost, the subscription stays registered).
- **§7** `label.go`: once-per-start latch on the node (set by the first
  `SubscriptionAlive`, cleared by `Stop`), `reconcileLabel`
  (NC:259-307). `TestLabelReconciliation` (differs → one
  UpdateFabricLabel("ours"), non-Ok noted; second alive nothing;
  Stop+Start → again, an error noted; equal label → nothing).
- **§8** `controller/client/sqlstore`: `New`, `Load`, `Save` (one
  transaction), the three erases, `NodeIDs` (UNION of both tables,
  sorted), `Schema`, `Apply`, `Upgrade` (no step yet), `Migration`,
  `Migrations` (`v1`), `SchemaFingerprint`; `schema.sql`,
  `migrations/v1.{up,down}.sql`. `TestStoreRoundTrip` (the same op
  sequence on `MemoryStore` and `sqlstore` loads `reflect.DeepEqual`),
  `TestStoreErase`, `TestStoreConcurrent` (`-race`), `TestStoreErrors`,
  `TestMigrationsMatchApply` (list vs. Apply, Apply after the list, Downs,
  host table and rows ignored), `TestMigrationsPin`. Floor 85 (measured
  89.6 %).
- **§9** `registry_live_test.go`, `TestLiveRegistry` on the AK harness,
  controller side on a SQLite file — all six steps pass (see below).

## Live test (`TestLiveRegistry`)

1. `ForDevice` → `peer1`, uncommissioned, one address, discovered;
   `Commission` → record has the address, `CommissionedAt`,
   `FabricIndexOnPeer` 1; Connected, Seeded; `Commissioned()` =
   `[peer1]`; `NodeIDs()` = `["peer1"]`. **Proven.**
2. Label: after the first `SubscriptionAlive` the device's own entry
   (read through `node.Read`) is "live"; `Fabric.Label = "renamed"`,
   `Stop`+`Start` → after the next `SubscriptionAlive` the entry reads
   "renamed" (and the device's own store too, `node.Fabrics`).
   **Proven.**
3. `Close`, `NewRegistry` on the same database → `peer1` restored, peer
   bound with the record's operational address, the image from the cache
   (3 endpoints, OnOff known) without a read and unsubscribed; `Start`
   with `AutoStart` resumes the subscription; a toggle arrives.
   **Proven.**
4. The `node.Node` restarted on its database: the StartUp event arrives
   over the subscription the device resumes, no session from before the
   restart remains, and the subscription delivers again. **Not proven
   in isolation:** the device's `Stop` closes its sessions gracefully
   (CloseSession) — the test logs "session lost true closed true" right
   after the stop, before the restart — so the old session is gone before
   the startUp reaction runs. The cut itself is proven by the scripted
   `TestReactionStartUp`.
5. `Decommission` → the device's fabric list is empty
   (`node.Fabrics`), `Commissioned()` empty, `NodeIDs()` empty,
   `Peers.Get` nil. **Proven.** RemoveFabric was answered with
   NOCResponse Ok in this run (no "confirmed without response" path).
6. The cull: `ForDevice` of an unreachable device → `peer2`; the
   registry's fake clock advanced to just before `DefaultTTL` keeps it,
   past it the node is gone. **Proven** (see difference 8 for the TTL).

## Guards

- `make ci` at 2194d84 (HEAD logged before and after, unchanged): exit 0
  — build, vet, `go test -count=1 ./...` (181 `ok`, 0 FAIL), gofumpt +
  golangci-lint (0 issues, both runs), `go mod tidy -diff`, `go mod
  verify`, cover-check (`controller/client` 93.9 % against 85,
  `controller/client/sqlstore` 89.6 % against 85; the checker advises
  raising `controller/client` to 93 — left at 85 as the brief says).
- `make race` at 2194d84 (HEAD unchanged): exit 0 on the first run, 181
  `ok`, no FAIL, no race reported.
- `make reachability-check` at 2194d84 (HEAD unchanged): exit 0,
  snapshot not moved; unreached stays **5**, none in `controller/`.
- `make chiptool-test` (once) at 2194d84 (HEAD unchanged): exit 0,
  `ok internal/chiptool 66.8s`, 313 PASS, 0 FAIL, 1 SKIP
  (`TestMatterJSController`: no built matter.js at
  `.claude/worktrees/matter.js`, its environment reason — as AK).
  `TestChipToolCommissionsReferenceBridge`, `TestChipToolSuite`,
  `TestGoControllerCommissionsReferenceBridge`,
  `TestGoControllerSubscribesReferenceBridge` passed: nothing on the
  device side moved.
- `make ci` did not hit golangci-lint's own timeout; no separate `make
  lint` rerun was needed.

## Lines of the brief that differ from the file (followed the file)

1. **CC line numbers of the table (§2):** `ReportedSessionParameters` is
   CC:1061-1065 (not :1050-1060), `TCPUnsupported` CC:1073-1078 (not
   :1062-1070); the State class is CC:927-1091. FabricDescriptorStruct
   also has field 6 `VidVerificationStatement` (ignored).
2. **`isCommissioned` (§2)** is `node/NodeLifecycle.ts:94-96`, not in
   `ClientNodeLifecycle.ts` (no such member there); it is latched by
   `initialized` (CC:586) and by `commissioned`/`decommissioned` (CC:637-646).
3. **Decommission of an uncommissioned node (§0/§4):** CN:178 decommissions
   only `if (this.lifecycle.isCommissioned)`, then deletes the node
   (CN:184) — so `ClientNode.decommission` never reaches CC:343's throw.
   `Registry.Decommission` therefore deletes an uncommissioned node;
   `ErrNotCommissioned` is the inner check and the error of an unbound
   node's interactions.
4. **Not located (§4):** CC:220-223 checks only `state.addresses`. The Go
   `commissioner.Options.Addresses` stays an override (the brief's
   "neither the options nor the record"); without it the record's are
   used — decision, not a mirror.
5. **`toLongForm` (§2):** RD:223-234 *assigns* DT, DN, RI, PH, PI, T, ICD
   and the advertised intervals unconditionally (an absent key clears),
   VP only when present (RD:214-221, 0 meaning unspecified). The brief
   named only discoveredAt/ttl/addresses; `applyTXT` follows the file.
6. **Address learning (§2):** matter.js runs `#update` on `peer.updated`
   (CC:711), i.e. after every descriptor save. `peer.Observer` has no
   `Updated`, so — as the brief says — the record follows on
   `SessionAdded` and on commission; a TXT change between sessions is not
   copied until the next session (question 3).
7. **`lifecycle.decommissioned` (§6/§4):** CC:637-646 also emits it when
   a decommission clears the peer address, so `Hooks.Decommissioned` is
   called by `Decommission` too, not only by a leave (P:827).
8. **ForDevice TTL (§3, §9.6):** the brief's step 6 says "`TTL = 1s` on
   a fake clock", but `ForDevice(ctx, d)` takes no TTL and the Go scanner
   carries none (TTL 0 → `DefaultTTL`, as §3 says). Step 6 advances past
   `DefaultTTL` instead.
9. **startUp without a session (§6):** the brief says "pass `Clock.Now()`".
   The Go `HandlePeerLoss` reads a zero `asOf` as "every session", which
   is the same cut as "created before now"; passing the registry's clock
   would compare it against the exchange manager's clock (the live test
   runs the registry on a fake clock). `time.Time{}` is passed, as for
   shutDown.
10. **Leave disconnect error (§6):** P:828-832 logs a failing
    `disconnect`; Go `Peer.Disconnect` returns nothing, so there is
    nothing to log.
11. **RemoteDescriptor.is (§3):** its peer-address branch (RD:38-40)
    needs both sides to have one; a `discovery.Device` never has, so
    `find` matches the device identifier only (said in a comment).
12. **Restore failure (§0):** P:141-150 adds a node whose construction
    failed (it is "unavailable"); Go skips it, as the brief says.

## Decisions where the brief left a choice

- `RegistryConfig.HooksFor func(id string) Hooks` (new): the template
  `Node.Hooks` is one set for all nodes and its callbacks do not name the
  node; `HooksFor`, when set, gives each node its own. `Hooks.Decommissioned`
  is `func()` like the other per-node hooks.
- The reactions and the label reconciliation run as registry background
  work (`goWork`, waited for by `Close`, context cancelled by `Close`):
  matter.js does not await them (P:727-729, :790, NC:56 `offline`), and a
  reaction that deletes the node must wait for the subscription that
  delivered the event to end. The decommission latch is set synchronously
  in the hook. The host's `Event` runs after the registry's dispatch.
- `ForAddress(addr, id string)` (no ctx); `Registry.Erase(ctx)` returns
  nothing (failures are logged, P:495-514).
- `CommissionOptions.CaseAuthenticatedTags` (CC:304-306) and the default
  CASE connection timing after commissioning (CC:775-779: 15 s, 60 s,
  2 min) when the caller passes none.
- `deleteNode`: `Close` then `Erase`, the protocol peer deleted even when
  that fails (CN:213-225); a node whose teardown fails stays in the
  registry except under `Erase`, which drops it.
- The template's commissioning choices (AutoSubscribe, DefaultSubscription,
  NewlyCommissioned, NoInitialRead) are zeroed for every node; a commission
  sets them.
- `ErrClosed` for a fabric operation on a closing node, one no longer in
  the registry, or a closed registry (P:595 `assertNotGone`).
- `sqlstore`: `Save` always upserts the cluster's version row (NULL for a
  nil version), so the store keeps `MemoryStore`'s contract exactly — a
  nil version clears the stored one, and a cluster saved without values
  still loads (TestStoreRoundTrip compares both stores). Its own
  `Migration` type: `store.Migration` would be reusable, but its doc and
  pin are the device tables'.
- Record encoding: `NetworkAddress` as a struct {0 ip string, 1 port, 2
  ttl s, 3 discoveredAt ms}; absent fields are stored as nil (deleted).
- The leave and decommission read `OperationalCredentials` through the
  image's endpoint 0 cluster (`node.behaviors.has(...)`); the
  decommission's CurrentFabricIndex is taken from the read's report.

## Left out, and why

- The brief's "Out" list (groups, ICD, ConfigurationVersion, split
  commissioning, discovery runners, commissioning windows, TCP).
- `lifecycle.commissioned` (CC:325, :641) — no `Commissioned` hook;
  question 2.
- `IdentityService` node-id reservation (CC:225-250,
  `allocatePeerAddress`), the OTA provider location (CC:271-281),
  `finalizeCommissioning` (CC:283-287), the pairing-code option: not in
  the Go commissioner's surface / later briefs.
- `onlineAt`/`offlineAt` (CC:967-977, not persisted).
- `SessionIntervals.advertisable` (RD:119, :223): the Go `TXTData` is
  already bounded by the scanner's parser; not re-validated.
- `FabricRemovedError` in the confirmation classes: no Go layer raises it.
- The fabric-index backfill from `CurrentFabricIndex$Changed`
  (CC:573-598).

## Open questions

1. **`keepSubscriptions` (§6).** In Go a session loss never closes a
   client subscription: `exchange.Manager.HandlePeerLoss`
   (manager.go:864-875) → `Session.handlePeerLoss` (session.go:273-297)
   marks the session lost, reports it and aborts its exchanges; client
   subscriptions live in `interaction.Subscriptions`, keyed by peer
   address and subscription id (subscriptions.go:119-136), and nothing
   connects the two. So every Go peer loss behaves as
   `keepSubscriptions: true`. In matter.js the flag only decides whether
   the *session's* subscriptions close (Session.ts:318-322 →
   NodeSession.closeSubscriptions, NodeSession.ts:69, :293-299) — the
   server-side subscriptions a peer holds with us, which a pure
   controller has none of — so the shutDown/startUp mirror holds. The
   sustained default subscription survives the loss and continues when
   the device resumes it (live step 4: it delivers again) or, failing
   that, on its liveness timeout. The AK test's `CloseForPeer` is the
   address-monitor path (PeerAddressMonitor.ts:244-246), not this one.
   Is that reading accepted, or should a `keepSubscriptions: false` peer
   loss (e.g. the decommission's) close the peer's client subscriptions?
2. Add a `Hooks.Commissioned` for `lifecycle.commissioned` (CC:325)?
3. Add `peer.Observer.Updated` so the record follows every descriptor
   save as CC:711 does (a change in `controller/peer`)?
4. The unbound node's `Read`/`Subscribe`/`Write`/`Invoke` now go through a
   locked accessor (`interact.go`, `network.go` touched) and return
   `ErrNotCommissioned` — acceptable, or should the registry hand out a
   node only once bound?
5. `RegistryConfig.HooksFor` — keep, or a different shape (e.g. hooks
   taking the node)?
6. `ErrPeerLeft`/`ErrPeerShutdown` are declared in `client`; should they
   move to `controller/peer` or `controller/exchange` next to
   `PeerInitiatedCloseError`?

## `by_design` candidates (not written)

- The reactions and the label reconciliation as background work of the
  registry rather than in the delivering hook.
- A node that fails to restore is skipped, not kept unavailable
  (P:141-150).
- `ForDevice`'s TTL unknown (the scanner carries none) → `DefaultTTL`.
- The record follows the peer on `SessionAdded` and commission, not on
  every `peer.updated` (CC:711) — unless question 3 adds the observer.
- The commissioner's address option kept as an override of the record's
  addresses (difference 4).
