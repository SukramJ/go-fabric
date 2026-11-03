# Brief: M9 topology sources — `Service.AddSource` after matterjs-server's `addNodeSource`

Date 2026-11-03. Owner decision 2026-11-03 (wizard): go-fabric-server's
imported test nodes (`import_test_node`) stay in the server's topology
graph the way matterjs-server keeps them — as a second node source of the
topology service (`WebSocketControllerHandler.ts:305-309` `#testNodeSource`
`{listNodes, nodeAdded, nodeRemoved}`, `:2224`
`networkTopology.addNodeSource(this.#testNodeSource)` at 0bd06f22) — so
`controller/topology` gains the source seam #384 left out (its hand-back:
"addNodeSource (3)" parity cases not ported). One wave, one pull request,
one hand-back; branch `m9/topology-sources`, worktree `../go-fabric-topo2`,
on main c91e5b7f. **Model:** Opus. Paths: `controller/topology/` only (plus
CHANGELOG, `docs/` page of the package, snapshots). Phase 30 runs beside you
in `../go-fabric-p30` on disjoint paths; guards under
`flock ~/.cache/go-fabric-scratch/guards.lock`; no chip-tool run.

## Source at the pin (matterjs-server 0bd06f22, `git -C ../matterjs-server show 0bd06f22:<path>`)

`packages/ws-controller/src/controller/NetworkTopologyService.ts`:
- `:72-81` `TopologyNodeSource {listNodes: () => TopologyNode[]; nodeAdded?; nodeRemoved?}` — "Its nodes are derivation inputs only: refresh never issues attribute reads to them."
- `:143` `#auxSources: TopologyNodeSource[]`.
- `:188-194` `addNodeSource`: no-op when stopped or already registered (idempotent per source), subscribes `nodeAdded`/`nodeRemoved` to `#scheduleRebuild`, schedules a rebuild at registration.
- `:307` `#build`: `allNodes = [...listNodes(), ...auxSources.flatMap(s => s.listNodes())]`.
- `:196-205` refresh doc: "aux-source nodes are never read".

`packages/ws-controller/test/NetworkTopologyServiceTest.ts:583-633` `describe("addNodeSource")`, the three cases: `:584-602` merges aux nodes, a neighbour of a primary node matched by rloc16 across sources gives a `strong` link 1→2; `:604-618` a `nodeAdded` emit rebuilds (emitted 1 → 2, second snapshot has 2 nodes); `:620-633` refresh never reads aux nodes (`readCalls` = [1]) and a duplicate registration is a no-op (2 nodes, not 3).

## Deliverable (one commit, plus snapshots)

`controller/topology/service.go`: a `Source` — the Go form of
`TopologyNodeSource`:

```go
// Source is an additional provider of graph nodes — matterjs-server's
// TopologyNodeSource at 0bd06f22 (NetworkTopologyService.ts:72-81): its
// images are derivation inputs only, Refresh never reads them.
type Source interface {
    // Images lists the source's nodes as they stand.
    Images() []Image
}
// AddSource registers src; it returns the function that removes it
// again. Registering schedules a rebuild; a source already registered
// is not added twice (NetworkTopologyService.ts:188-194). The host calls
// Changed when its source's node set changes — the rebuild is debounced
// like any other (nodeAdded/nodeRemoved → #scheduleRebuild).
func (s *Service) AddSource(src Source) (remove func())
// Changed schedules a debounced rebuild — what a Source's host calls
// when a node of its source was added or removed.
func (s *Service) Changed()
```

`images()` (`service.go:200-212`) appends every source's `Images()` after
the members' (`:307` order: primary first, then aux in registration
order). `runRefresh` (`:261-295`) iterates members only — sources are not
read; say so in `Refresh`'s doc (`:240`). `Close` (`:389`) drops the
sources. `Changed()` is `s.changed()` (`:334`) made public with the doc
above; keep the unexported one if the Changes wiring uses it. A closed
service ignores `AddSource` (returns a no-op remove) and `Changed`.

Tests in `service_internal_test.go`, the three cases ported by meaning on
the harness (`newHarness`, `:17`): "merges source images into the graph,
with edges resolving across sources" (a primary Thread image with a
neighbour at rloc16 1025, LQI 3, RSSI -40 → source image node 2 at rloc16
1025: nodes ["1","2"], link 1→2 strong, one emit after registration);
"rebuilds when the source reports a change" (`Changed()` → second emit
with two nodes); "never reads a source's nodes on Refresh and is
idempotent per source" (the fake members' read log names node 1 only;
the same `Source` value registered twice → 2 nodes). One case for the
remove function (removing emits a snapshot without the node) and one for
a closed service. The `fabrictest` site test (`site_test.go`) gains
nothing; this is pure.

`docs/` (the guide section #384 added for `controller/topology` — find it
with `grep -rn "controller/topology" docs/*.md docs/guides/*.md`): one
paragraph "Further sources" with the host's wiring (go-fabric-server's test
nodes). CHANGELOG `### Added` one line under `[Unreleased]`. by_design: the
`BD-matterjs-server-NetworkTopology` row's "left out" sentence in
`notes/parity/by_design.md` (find `addNodeSource` or "aux") drops
`addNodeSource` if it names it; otherwise unchanged — read it before you
touch it. `make cite-report` and `make reachability` regenerate the
snapshots in the same commit as the change that moved them.

## Rules

Read `CLAUDE.md` §"Token use" first. Every citation above is re-read at the
pin before you write it into a comment. Commits: conventional, English, no
attribution or co-author lines; before every commit `git var GIT_AUTHOR_IDENT`
must be `SukramJ <sukramj@icloud.com>`, commit with
`-c user.name=SukramJ -c user.email=sukramj@icloud.com`. Guards in the
background under the flock with a summary log
`~/.cache/go-fabric-scratch/topo2/summary.log` (HEAD before and after, one
`exit <name> <code>` line per guard): `make ci` whole (cover-check: the
package floor is 95, measured 95.5 at #384 — keep it), `make race` on
`./controller/topology/...`, `make cite-check` (needs `../matterjs-server`
at 0bd06f22), `make reachability-check`, `make apidiff-check`, `mkdocs
build --strict`. Read a guard's log only on failure, with grep. Then push,
open the pull request (no auto-merge, no labels, the body is the report),
hand back: per deliverable, guards table (guard, HEAD, exit), differences
from the brief with reasons, decisions, left out, open questions,
citations written.
