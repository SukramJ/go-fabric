# Brief CA — M4 phase 21a: the persistence port and its second implementation

Date 2026-10-23. Concept v7 (`docs/concept-v7-operation-and-packaging.md`
A.1, A.2, decision 0.1), the read map `notes/briefs/reads/read-m4-phase-21-22.md`
§2 (every `*store.Store` method with file:line — a map, verify before
acting), ADR 0026 P7 ("Persistence is one seam"), ADR 0025 P3/P4/P9,
`docs/guides/persistence.md`, `docs/guides/advanced-seams.md` (store
row). matter.js pin `862ad75f`: the storage port is
`packages/general/src/storage/` (`StorageService`, `StorageContext`,
the backends) — read how the node receives it
(`packages/node/src/node/server/ServerEnvironment.ts`, the `storage`
wiring) and cite the lines; the Go port is a port, not a copy. go-fabric
`main` after BV-b has merged (one implementing agent at a time — owner
2026-10-23; start only when the BV agent has handed back BV-b and its PR
is merged: poll `gh pr list --state open` for the BV-b PR, then its
state). Branch `m4/phase-21a`, commits `m4 CA: <step>`, one pull
request, no auto-merge, no chip-tool label (no composition change). The
removal rule of 2026-10-21 applies (a replaced form goes with a
migration-guide line). Version: a `v0.10.x` patch (owner 0.5) — the one
break (`node.Config.Store.Credentials` type) is named in the CHANGELOG
and the guide. The owner merges.

**Model:** Opus. `store/` is not a security path, but `secure/operational`'s
resumption and `cluster/core/operational_credentials.go`'s fabric use of
the store are: touch their call sites only where the type name changes
(`*store.Store` → the port), nothing else there; list every such line.

## 1 The ports (concept A.2 variant a)

- In `store/`: one interface per area, named after the Matter concept,
  methods exactly the ones the module's consumers call today (read map
  §2.1; `grep -rn 'st\.\|store\.' --include=*.go` per consumer):
  `Fabrics` (AddFabric, GetFabric, ListFabrics, UpdateFabricLabel,
  UpdateFabricVendorID, UpdateFabricNodeID, RemoveFabric, the
  next-fabric-index counter), `Identity` (UpsertIdentity, GetIdentity),
  `ACL` (ListACL, ReplaceACL), `GroupKeys` (the group key set and
  mapping methods), `GroupTable`/`Groupcast` (or fold into `Groups` to
  match `groups.Store`, which already exists — reuse it, do not
  duplicate), `Resumption`, `ServerSubscriptions`, `Settings`
  (GetSetting, SetSetting, GetMetadataCounter, SetMetadataCounter),
  `OTA`, `ThermostatPresets` (exists), `DoorLockAuth`. The sum is
  `store.Port` (an interface embedding them). Every consumer that holds
  `*store.Store` holds its area interface instead (`node`, `bridge`,
  `cluster/core/*`, `groups`, `secure/operational`, `im/subscription`,
  `node/persist.go`, `fabrictest`): a consumer names only what it uses
  (ADR 0026 P1 — no package needs the whole port).
- `*store.Store` implements every interface (compile-time asserts);
  `store.New(db)` stays; `Schema`, `Apply`, `Upgrade`, `Migrations` stay
  SQLite's.
- `node.Config.Store.Credentials` becomes `store.Port` (the type
  changes; `Store.Path` and `Store.DB` unchanged — the reference
  consumer uses `Path`); `Node.Store()` returns `store.Port`. Guide
  line, CHANGELOG `### Changed`.

## 2 The second implementation: `store/memstore`

- `memstore.New() *memstore.Store` implements `store.Port` in memory
  (maps under one mutex, fabric-scoped removal exactly as SQLite's
  `RemoveFabric` cascades — read the SQL to know what cascades:
  resumptions, ACL, group keys, subscriptions, presets, lock auth by
  fabric). Copies on read (no aliasing of slices the caller may mutate).
  Documented for tests and small daemons that accept losing state on
  restart; `node.Config.Store.Credentials: memstore.New()` works with
  `Store.Endpoints` an in-memory endpoint store (`endpointtest.FakeStore`
  is test support — add `endpoint/memstore` or a `node` helper
  `node.MemoryStores()` returning both, say which).
- Contract tests `store/storetest`: one table that runs against any
  `store.Port` (every method: round trip, not-found sentinels, fabric
  cascade, counters, the group-key ordering `ListGroupKeySets` promises,
  the subscription load order); `store` runs it against SQLite,
  `memstore` against itself. The existing SQLite tests stay.
- `fabrictest` devices: `memstore` + the in-memory endpoint store by
  default; `fabrictest.Options.StoreOnDisk` (or the existing option, if
  one names a directory) keeps the SQLite file for the tests that need
  a restart to re-open a file (`TestLiveSustainedSurvivesRestart`, the
  OTA restart tests — read which ones re-open; they keep SQLite). Report
  the test-suite runtime before and after (`go test -count=1 ./node/
  ./controller/client/` wall time).

## 3 Docs

`docs/guides/persistence.md`: the port, the two implementations, what a
host's own backend must guarantee (atomic per method, fabric cascade,
the counters monotonic, the settings' keys) and how `storetest` proves
it; `advanced-seams.md`'s store row says the seam became a port
(remove "milestone 4, phase 21" sentences at `:105` and wherever else);
`docs/matterjs-comparison.md:130` "Pluggable storage" → ✅ with the
sentence; ADR 0026 P7's text names `store.Port` (owner's ADR: propose
the sentence in the report, do not edit the ADR).

## Guards and report

`make ci`, `make race` (twice: the memstore's mutex is new),
`make archcheck`, `make reachability` then `-check`, `make cite-check`
(`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`),
`make testparity-check`, `mkdocs build --strict`. CHANGELOG `### Added`
(ports, memstore, storetest), `### Changed` (`Config.Store.Credentials`,
`Node.Store()`), guide rows. Report `notes/briefs/reports/CA.md`:
**Ports** (interface → methods → consumers), **memstore** (cascade
table), **fabrictest** (which tests keep SQLite, runtimes), **Guards**,
**Differences**, **Decisions**, **Left out**, **Questions**, `by_design`
candidates with citations. Copy this brief to
`notes/briefs/2026-10-23-CA-m4-phase-21a-store-port.md` in the first
commit. Never add attribution lines to commits or the PR body.
