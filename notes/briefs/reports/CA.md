# Report CA — M4 phase 21a: the persistence port and its second implementation

Brief: `notes/briefs/2026-10-23-CA-m4-phase-21a-store-port.md`. Concept v7
A.2, decision 0.1 (variant a). Branch `m4/phase-21a` from `origin/main`
`c1cf2257` (BV-b, #245, merged). matter.js pin `862ad75f`.

## Ports

`store/port.go`: one interface per area, the methods the consumers call;
`store.Port` embeds all eleven; `var _ Port = (*Store)(nil)`.

| Interface | Methods | Consumers (what they hold) |
| --- | --- | --- |
| `Fabrics` | AddFabric, GetFabric, ListFabrics, UpdateFabricLabel, UpdateFabricVendorID, UpdateFabricNodeID, RemoveFabric (the next-fabric-index counter is AddFabric's, kept as metadata `next_fabric_index`) | `cluster/core.StoreFacade` (subset), node commissioning window (`store.Fabrics`), node CASE identities (`fabricIdentities`), `groups.Store` (reads) |
| `Identity` | UpsertIdentity, GetIdentity | `cluster/core.StoreFacade`, node `fabricIdentities` |
| `ACL` | ListACL, ReplaceACL | `cluster/core.ACLStoreFacade`, `endpoint.ACLLister` via `bridge.AttachACLLister` |
| `GroupKeys` | UpsertGroupKeySet, GetGroupKeySet, ListGroupKeySets, RemoveGroupKeySet, RemoveGroupKeysByFabric, SetGroupKeyMapping, RemoveGroupKeyMapping, ListGroupKeyMappings | `cluster/core.GroupStoreFacade`, `StoreFacade` (IPK set, rollback), `groups.Store` (reads) |
| `Groups` | Upsert/Remove/ListGroupTable(Entry), Upsert/Remove/ListGroupcastGroup(s) | `groups.Store` now embeds `store.Groups` (reused, not duplicated) |
| `Resumption` | UpsertResumption, GetResumptionByID, GetResumptionByPeer, RemoveResumption, RemoveResumptionsByFabric | `secure/operational.ResumptionStore` (subset, unchanged), node `caseInitiatorProvider` (`store.Resumption`) |
| `ServerSubscriptions` | Save/Delete/DeleteByFabric/Load/ClearServerSubscriptions | `bridge.SubscriptionStore` (same method set, unchanged) |
| `Settings` | GetSetting, SetSetting, GetMetadataCounter, SetMetadataCounter | node `persisted`, `countBoot`, operational time, labels (`store.Settings`); `NewEndpointPersistence(store.Settings, …)`; `groups.CounterStore`; `ACLExtensionPersistence`; VID verification; `controller/ca`/`dcl` `Settings` |
| `OTA` | ListDefaultOTAProviders, ReplaceDefaultOTAProviders, Load/SaveOTARequestorState | `cluster/core.OtaRequestorStore` (same set) |
| `ThermostatPresets` | LoadThermostatPresets, SaveThermostatPresets | `store.NewThermostatPresetStore(st, key)` → `thermo.PresetStore` |
| `DoorLocks` | LoadDoorLockAuth, SaveDoorLockAuth | `doorlockdef.AuthStore` (with `Settings`) |

`node.Config.Store.Credentials` and `Node.Store()` are `store.Port`;
`fabrictest.Device.Store()` too. The node (composition root) holds the
`Port`; every node helper names an area. Security paths, type names only:
`node/security.go:250-259` (`caseIdentities.load` takes the local
`fabricIdentities`), `:365` (`wireSecurity`'s `st store.Port`), `:496`
(`caseInitiatorProvider(st store.Resumption, …)`). `secure/operational` and
`cluster/core/operational_credentials.go` are unchanged; `cluster/core`
changes are comments only (`group_key_management.go:388`,
`ota_software_update_requestor.go:112`, `:194-195`).

matter.js (cited in `store/port.go:34-44`): `StorageService`
(`packages/general/src/storage/StorageService.ts:37`, `registerDriver` :64,
`open` :161) over the abstract `StorageDriver` (`StorageDriver.ts:26-42`),
`MemoryStorageDriver` (`MemoryStorageDriver.ts:13`); the server node gets
it from its environment (`packages/node/src/node/server/ServerEnvironment.ts:158`,
`:171-174`); per-fabric context `fabric-<index>`
(`packages/protocol/src/fabric/FabricManager.ts:251`) cleared on deletion
(`:262-271`); the counter `FabricManager.ts:178-179`.

## memstore

`store/memstore` (maps under one mutex, copies in and out; 99.5 %
coverage). `endpoint/memstore`: the in-memory `endpoint.Store` with
`endpoint/sqlitestore`'s allocation (counter never rewinds, explicit number
lifts it, wrap to the first free from 2, `ErrEndpointsExhausted`; 100 %).
Chosen over `node.MemoryStores()`: the in-memory endpoint store sits beside
`sqlitestore` in `endpoint/`, and the node keeps one shape
(`node.Store{Credentials, Endpoints}`).

Cascade, read from `store/schema.sql` (FOREIGN KEY … ON DELETE CASCADE):

| Row | SQLite on RemoveFabric | memstore |
| --- | --- | --- |
| node identity, group key sets, group key map, group table, groupcast groups, ACL, resumption, default OTA providers | cascades (FK) | removed |
| server subscriptions | stays (no FK, `schema.sql:257-263`); the bridge calls `DeleteServerSubscriptionsByFabric` | stays |
| thermostat presets, door-lock users/credentials/schedules | stays (keyed by endpoint) | stays |
| settings, metadata counters | stay | stay |

Also mirrored: rows naming an unknown fabric are refused (the FK), ACL
privilege/auth-mode CHECKs, the unique resumption id, `(fabric_id,
root_public_key)` unique, the door-lock primary keys, `next_fabric_index`
starting at 1, nil CATs read back empty, epoch starts without a key
dropped, `ota_requestor.state` and `door_lock.saved:<key>` as settings.

`store/storetest.Run(t, open)`: 22 cases (round trips, sentinels,
counter no-reuse / explicit index / exhaustion and wrap / duplicate,
ordering incl. `ListGroupKeySets`, subscription load order, cascade,
unknown-fabric refusal, constraints, copies, concurrency). `store`
(`store/contract_test.go`) and `store/memstore` run it; both pass, with
`-race` too.

## fabrictest

Devices run on `store/memstore` + `endpoint/memstore` by default;
`fabrictest.Options.StoreOnDisk` keeps the SQLite file. `Device.Restart`
reuses the same store objects, so no test needs a file re-opened; on
SQLite stay `TestLiveSustainedSurvivesRestart` (client/interaction, the
subscriptions across the SQLite round trip) and `TestDeviceRestart`
(fabrictest, now both variants). The OTA restart tests
(`node/ota_live_test.go`) open their own database, not fabrictest's.

Runtime `go test -count=1 ./node/ ./controller/client/` (same machine,
warm build cache):

| | node | controller/client | wall |
| --- | --- | --- | --- |
| before (`c1cf2257`) | 62.8 s | 62.4 s | 66 s |
| after | 62.4 s | 49.9 s | 66 s |

The node package is bound by its clock waits, not by SQLite; the wall time
is the node package's.

## Guards

Run at `68b2d5c8` (HEAD before and after the same; one sequential chain,
summary log):

| Guard | Exit |
| --- | --- |
| `make ci` | 0 |
| `make race` (1) | 0 |
| `make race` (2) | 0 |
| `make archcheck` | 0 |
| `make reachability-check` | 0 (unreached stays 3; every area reached by `TestStoreImplementsEveryArea`) |
| `make cite-check` | 2 at `68b2d5c8` (stale artefacts, drift 262 → 264), 0 at `b1904f4e` after rewording two citations and committing the artefacts (drift 262, ratchet unchanged) |
| `make testparity-check` | 0 |
| `mkdocs build --strict` | 0 |

Earlier runs failed and were fixed before this one: `apidiff-check` (the
removed method named as apidiff names it), `errprefix` (memstore's door-lock
errors), lint (context-first contract cases, named results, preallocation).
After `68b2d5c8` only comments and the citation artefacts changed; `make
cite-check` and `go test ./store/...` re-ran, `make ci`/`race` did not
(comment-only).

## Differences

- `RemoveFabric` does not cascade subscriptions, presets or lock records:
  the brief listed them, the SQL does not cascade them (the brief said to
  follow the SQL).
- Subscription load order: SQLite orders by `updated_at` (one-second
  `CURRENT_TIMESTAMP`) then id; memstore by save sequence. Saves within one
  second come back by id from SQLite, by save order from memstore; the
  contract table saves in id order and the port documents "least recently
  saved first".
- memstore does not enforce the NOT NULL of blob columns (a nil NOC, root
  key or payload); SQLite may refuse those.
- `endpoint/memstore.ListEndpoints` returns the caller's `SourceKey`;
  sqlitestore decodes the rendered string (`WithKeyDecoder`).

## Decisions

- Area `DoorLocks`, not `DoorLockAuth`: the struct `store.DoorLockAuth`
  holds that name.
- `(*store.Store).ThermostatPresets(key)` removed (rule of 2026-10-21) for
  `store.NewThermostatPresetStore(st, key)` over any implementation;
  migration-guide row and CHANGELOG `### Removed`.
- `groups.Store` embeds `store.Groups`; the other consumer interfaces
  (`StoreFacade`, `ResumptionStore`, `SubscriptionStore`, …) stay local:
  each names its subset (ADR 0026 P1).
- `node.NewEndpointPersistence` takes `store.Settings` (a `*store.Store`
  still fits).
- Coverage floors: `store` 83 → 85, `store/memstore` 99,
  `endpoint/memstore` 99, `store/storetest` 0 with the reason; archcheck:
  `store/memstore` in security-and-storage, `store/storetest` in support.
- Doc citations shifted by the edit, re-pointed: `docs/guides/concurrency.md`,
  `persistence.md`, `migrating-to-the-facade.md`, `notes/parity/by_design.md:2271`
  area (`node/security.go:333`), `notes/audits/matter-threat-model.md:303`.
- Versioned as the `[Unreleased]` v0.10.4 lane beside BV-b; the break is in
  CHANGELOG `### Changed` and the migration guide.

## Left out

- ADR 0026 P7 not edited (owner's ADR). Proposed text: "7. **Persistence
  is one seam.** `store.Port` (credentials, fabrics, ACL, groups,
  subscriptions, settings: one interface per area, `store.Fabrics` …
  `store.DoorLocks`), `endpoint.Store` (endpoint numbers) and the
  controller's client store are the three ports; SQLite is the shipped
  implementation, and `store/memstore` with `endpoint/memstore` is the
  second, held to the same contract table, `store/storetest`."
- No chip-tool run (no composition change).
- `notes/parity/matter_behaviour_findings.md:419` cites
  `groups/manager.go:271`, already stale before this change (the line is
  not the key-set-0 skip; the IPK finding looks closed by 18c) — left for
  Fable.

## Questions

- Version: v0.10.4 together with BV-b, or BV-b as v0.10.4 and CA as
  v0.10.5?
- Should memstore also refuse nil blobs where SQLite's NOT NULL would?

## by_design candidates

- The port is typed by area (records), not key-value contexts: matter.js's
  `StorageContext`/`StorageDriver` get/set/values
  (`packages/general/src/storage/StorageDriver.ts:26-42`,
  `StorageContext.ts:15`, per-fabric `FabricManager.ts:251`, cleared at
  `:262-271`); go-fabric's `store.Port` (`store/port.go`), the cascade
  being the schema's rather than a context's `clearAll`. Reason: a Go
  backend keeps typed records and its schema's constraints.
