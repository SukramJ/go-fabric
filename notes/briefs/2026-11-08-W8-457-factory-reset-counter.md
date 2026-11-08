
# Brief: issue 457 — FactoryReset restarts the endpoint numbering (2026-11-08)

Owner decisions: own small PR directly after W7 (#458) merges; not bundled
with #420 (bound to phase 40). Implementer: Opus, one worktree
(`../go-fabric-457`, branch `fix/457-reset-counter`, base = main after #458).
Author `SukramJ <sukramj@icloud.com>`, no attribution lines.

## Why (verified 2026-11-08)

`node/factory_reset.go:117-131` `eraseEndpointRecords` removes every
endpoint record through `endpoint.Store.RemoveEndpoint`, which by contract
never returns a number to the pool (`sqlitestore.go:376-385`); the
allocation row `matter_endpoint_allocation.next_endpoint_id` (`allocate`,
`sqlitestore.go:265-275`) keeps its value, so the node that comes back on
the erased store numbers from the old high-water mark (host measured 2–23
→ 24–45). matter.js `ServerNodeStore.erase`
(`packages/node/src/storage/server/ServerNodeStore.ts:134-139` at
db021da0) erases the endpoint stores, and `ServerEndpointStores.erase`
(`ServerEndpointStores.ts:63-77`) does `storage.clearAll()`, a fresh holder
map, `#nextNumber = 1`, `#persistedNextNumber = undefined`, then `load`.
No `by_design` row: parity, the current behaviour is the gap. The
never-rewind rule of `endpoint/identity.go:80-91` stays for removals; the
reset is the one case the counter restarts (say so in that comment).

## Scope

1. `endpoint.Store` gains `EraseEndpoints(ctx) error`: every record and
   the allocation counter back to the first bridged number, in one
   transaction (`sqlitestore`: `DELETE FROM matter_endpoints`; set
   `next_endpoint_id = allocMin` rather than deleting the row — `allocate`
   treats a missing row as "Apply not run", `sqlitestore.go:269-271`).
   Name check against `RemoveEndpoint`/`ListEndpoints`: `EraseEndpoints`
   is the verb `store.Eraser.Erase` already uses for the reset
   (docs/guides/persistence.md:32).
2. `node/factory_reset.go`: `eraseEndpointRecords` calls `EraseEndpoints`;
   remove the list-and-remove loop.
3. Every in-repo implementation and fake of `endpoint.Store` implements
   it: `endpoint/sqlitestore`, the fakes in
   `node/endpoint_number_conflict_parity_matterjs_test.go`,
   `endpoint/identity_parity_matterjs_test.go`, `endpoint/dispatcher_test.go`,
   `endpoint/groups_mount_test.go`,
   `endpoint/descriptor_server_parity_matterjs_test.go` (grep
   `UpsertEndpointAssigning(` for the full list).
4. **API break** (adding a method to a public interface): one `### Changed`
   **Break** line in `CHANGELOG.md` naming `endpoint.Store` (apidiff accepts
   the named symbol), plus a section in
   `docs/guides/migrating-to-the-facade.md` "Changed after v0.18.0: the
   endpoint store erases". openccu-loom implements the port
   (`internal/store/matterendpoint`, `internal/north/matteradapter/fake_store_test.go`)
   — the PR body names it; a loom follow-up issue is the owner's.
   go-fabric-server and go-ha2fabric do not implement it.
5. Docs: `docs/guides/operations.md:31` (FactoryReset paragraph) and
   `docs/guides/persistence.md` row: the node comes back numbering from 2 as
   a fresh install. `CHANGELOG.md` `### Fixed` for the behaviour.

## Tests

- `endpoint/sqlitestore`: unit test — allocate up to N, `EraseEndpoints`,
  next allocation is 2; a removal without reset still does not reissue.
- `node`: fabrictest — commission, read PartsList, `FactoryReset`, start
  again on the same store, assign the same sources: numbers start at 2.
- matter.js has no erase-then-assign test at the pin
  (`EndpointNumberConflictTest.ts` covers conflicts only); the parity case
  is ours, cite `ServerEndpointStores.ts:63-77` in it.
- Cover floors / testparity ratchets in the same commit if they move.

## Guards

Full bundle under `flock ~/.cache/go-fabric-scratch/guards.lock`
(`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`), incl.
`apidiff-check`, `mkdocs build --strict`. No chip-tool run: composition and
FeatureMap do not move (say so in the report).

## Report shape

Per §: files, guards with HEAD, differences from this brief, decisions,
left out, questions, by_design candidates (expected: none).

