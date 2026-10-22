# Brief BY-1 — gate-2 defects 1–3: the LevelControl change path, the thermostat's written setpoint

Date 2026-10-22. Source: the gate-2 consumer's notes
`notes/briefs/reports/G2-consumer/NOTES.md` on branch `docs/gate2-consumer`
(defects 1–3 with the program lines `main.go.txt`, `main_test.go.txt`,
`controller_test.go.txt` in the same directory), read against
`cluster/levelcontrol/{held.go,transitions.go,typed.go}` and
`cluster/thermo/thermostat_server.go:712-745` (owner's read, 2026-10-22).
ADR 0025 principles 1, 2, 7; ADR 0026 principles 5, 6, 10. go-fabric `main`
at `c9e7c473` or later. Branch `m4/by1`, commits `m4 BY-1: <step>`, one
pull request, no auto-merge, label `needs-chiptool` only if a FeatureMap
or composition moves (it should not). matter.js pin `862ad75f`, read only
via `git -C $MATTERJS_DIR show 862ad75f:<path>`; cite the lines you read.
Parallel to the gate-2 inventory (a read-only agent) — nothing else edits
code. The owner merges.

**Model:** Opus. No security path.

## 1 LevelControl: a change is reported, whoever made it (defects 1 and 2)

Observed: on a `device.NewDimmableLight` with a declared `LevelSource`
or the kit's held default, `LevelControl().OnChanged` never fires — not
for the host's `Set(Patch{CurrentLevel})`, not for a controller's
MoveToLevel — and the host's `Set` never reaches a subscriber (a Read
shows the new value; `OnOff` on the same device reports). What the owner
read: the server reports CurrentLevel itself
(`MatterSelfReportedAttributes`, `transitions.go:221`), so the bridge
relies on the server's notifier; `commit` notifies through
`s.changes.NotifyFrom` only on the transition path (`transitions.go:94`,
`:108`); the immediate (unmanaged) MoveToLevel and `SetAttributes` →
`heldLevel.setLevel` (`held.go:122`, `typed.go:39`) store without a
`NotifyFrom`; `OnAttributeChange` subscribes to the source's
`ChangeNotifier` only when `s.engine != nil` and the source implements
it (`transitions.go:183-215`) — the held source does not.
- Read matter.js: `LevelControlServer.ts` `moveToLevelLogic`/`setLevel`
  and `Transitions.ts` — a state write emits `currentLevel$Changed` on
  commit whoever wrote it (`Datasource.ts:1473-1476`), the quiet
  reporting only throttles the wire. Mirror: every path that changes
  CurrentLevel (immediate command, transition step, host `Set`, the
  source's own change, OnOff coupling, StartUpCurrentLevel at start)
  notifies `s.changes` with its origin — one place, the level setter —
  and the quieter stays in front of the wire report only.
- Tests, in `node/` over `fabrictest` (the consumer's programs are the
  template; copy what you need): for the kit's default DimmableLight and
  for a declared `LevelSource`: (a) host `Set` → typed `OnChanged` with
  `OriginHostSet`, `Device.OnChanged` too, and a subscriber's report after
  the quieter's window on the fake clock; (b) MoveToLevel from the
  controller → `OnChanged` with `OriginCommand` and the report; (c) a
  transition (`Transitions: true`) → steps reported, the final value once;
  (d) `OnChangedAsync` sees the same. Also a unit test in
  `cluster/levelcontrol` per path. Check ColorControl's held form for the
  same gap (`cluster/light`): same tests if it has it.

## 2 Thermostat: a controller's write of a setpoint reaches `OnChanged` (defect 3)

`thermostat_server.go:712-745` `MatterWrite`: the written setpoint goes to
the Sink and the store, `s.changes.NotifyFrom(OriginControllerWrite,
coupled...)` names only what the reconcile moved; the comment says "the
written attribute is reported by the write path" — true for the
subscriber (the dispatcher reports a controller write), false for the
host's `OnChanged`, which the docs promise (`docs/guides/host-view.md:57-61`,
`contract.OriginControllerWrite`). Read how `cluster.AttributeChanges`
feeds the host listeners and the bridge's report path
(`cluster/attribute_changes.go`, `contract.ChangeNotifier`,
`contract.AttributeChangeNotifier`, `bridge/` where a controller write is
reported) and make the written attribute reach the host listeners with
`OriginControllerWrite` **without** a second wire report — the clean
form is one `NotifyFrom` for the write that the report path de-duplicates
(or the dispatcher's own report goes through the same notifier), not a
special case in the thermostat. Check every self-held server's
`MatterWrite` for the same pattern (grep `NotifyFrom` near `MatterWrite`;
the BU-b origin audit `spectest.CheckOrigins` asserted the port, not the
listener — extend `CheckOrigins` so it asserts the listener sees the
written attribute with `OriginControllerWrite`, and run it over every
self-held server; fix what it finds).
- Tests: the consumer's thermostat case in `node/` (write from the
  controller → Sink, `Get`, `OnChanged` with the origin, one subscriber
  report); the extended `CheckOrigins` tables.

## 3 Docs lines the defects contradict

`docs/getting-started/device.md:163-170`, `:178-182`, `docs/guides/host-view.md:57-61`:
after the fix they are true — read them again and correct wording that
over-promises (e.g. the quieter's window). `go doc ./device Device.OnChanged`'s
"a cluster whose server notifies nothing, never calls fn" sentence goes
if no kit server is such a server any more; otherwise name them.

## Guards and report

`make ci`, `make race` (twice), `make reachability` then `-check`,
`make cite-check` (`MATTERJS_DIR=/home/markus/Dokumente/GitHub/matter.js`),
`make testparity-check`; `make chiptool-test` once (LevelControl's
reporting is on the wire; `GOFABRIC_CHIP_FAMILIES=LVL,OO,CC,TSTAT make
chiptool-families` if the report path changed). CHANGELOG `### Fixed`
(three lines, naming the consumer finding). Report
`notes/briefs/reports/BY-1.md`: **Causes** (file:line per defect),
**Fix** (the one notification place per server), **Audit** (the
extended CheckOrigins result per server), **Guards**, **Differences**,
**Decisions**, **Left out**, **Questions**, `by_design` candidates with
citations. Copy this brief to `notes/briefs/2026-10-22-BY1-gate2-defects.md`
in the first commit. Never add attribution lines to commits or the PR body.
