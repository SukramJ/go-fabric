# Brief phase 36 — the parity features (M10)

Roadmap milestone 10, row 36 (`docs/roadmap.md`): "the parity features
(matter.js serves them, this module refuses them): DoorLock FACE,
AdministratorCommissioning BC, OccupancySensing beyond PIR with
`OccupancyChanged`, PowerSource WIRED/RECHG/REPLC — proof: comparison rows,
DRLK, CADMIN, OCC, PS families". The read map
(`~/.cache/go-fabric-scratch/read-phase36.md`, Sonnet 2026-11-05; the
load-bearing lines below re-read by Fable at the pins) corrected the row's
premise: the device-kit path (the generated servers under `cluster/spec`
behind `device/`) already accepts any conformant feature selection for
PowerSource and OccupancySensing; what is refused or missing is narrower —
the FACE bit of `cluster/lock`, the BC feature of the wire
AdministratorCommissioning, the OccupancyChanged reaction, the HoldTime
timer, and the fixed PIR / BAT selections of the contract-projected servers
in `cluster/measurement`.

Pins: matter.js `862ad75f` (read only with
`git -C ../matter.js show 862ad75f:<path>`), connectedhomeip `6170af84`
(read only with `git -C ../connectedhomeip show 6170af84:<path>`; the
working tree is at another commit).

## 0 Working rules

`export PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`; `/tmp` is a
small tmpfs. Commits as `SukramJ <sukramj@icloud.com>` (`git var
GIT_AUTHOR_IDENT` first), `<area>: <what> (phase 36)`, no attribution
lines; stage named paths (never `git add -A` after `go build` of a main
package); never `git reset --hard`, never force-push; push regularly.
Guards under `flock ~/.cache/go-fabric-scratch/guards.lock`: `make ci`,
`make race`, `make cover-check`, `make reachability-check`,
`make cite-check`, `make apidiff-check`, `make ahead-check`,
`make surface-check`, `make chiptool-test`, and the families through
`make chiptool-families-update GOFABRIC_CHIP_FAMILIES=DRLK,CADMIN,OCC,PS`
(the harness is one agent's at a time). A ratchet moves in the same
commit as its cause; every new test citation regenerates `make cite-report
&& make reachability` in the same commit. Before the pull request:
`GOFABRIC_CHIP_FAMILIES=IDM GOFABRIC_CHIP_CASES=IDM/10.5 make chiptool-families`
(a cluster or feature added to a default-composition endpoint moves the
device-type conformance check) and `make cover-check` read for the
packages touched (CI measures the same numbers). Security paths
(`secure/`, `commissioning/`, ACL, the four hand-written `cluster/core`
servers) are not touched by the Opus agent; the BC deliverable (§2.2) is
Fable's, on the same branch in disjoint files. Facade changes (`node/`,
`device/`, `contract/`) are judged as API design: one shape, the
`node.Config.TLS` precedent (node/config.go:84-88, node/tls.go:27-47).
Official text in English; Docker is available. Production code never
calls `time.Sleep`/`time.After`; a timer runs on the component's clock
(`TestNoRawTimeOutsideClock`).

## 1 Owner decisions 2026-11-05 (wizard)

1. **DoorLock FACE is served, as matter.js's base server serves it**
   (`DoorLockServer.ts:85-98` lists `FaceCredentials`). The model marks
   the feature `P, O` (provisional; `cluster/spec/doorlock/definition_gen.go:131`);
   the comparison row names that. No chip case is gated on `DRLK.S.F06`.
2. **AdministratorCommissioning BC is a node option, off by default**
   (matter.js serves it only through `.with("Basic")`;
   `AdministratorCommissioningServer.ts:105-117` opens the basic window
   with the default passcode); **the reference daemon turns it on** so
   `TC_CADMIN_1_4` (`pics CADMIN.S.F00`, `TC_CADMIN_1_3_4.py:288-289`) and
   `TC_CADMIN_1_11` steps 9a-9d (`:87-92`) run. Commissioning path: Fable
   implements it (§2.2).
3. **OccupancySensing**: the OccupancyChanged reaction (matter.js
   `OccupancySensingServer.ts:67-74` on `occupancy$Changed` when
   OccupancyEvent is set; chip emits always,
   `OccupancySensingCluster.cpp:312-320`) for the kit and the projection;
   the projected server gains detector-type and OccupancyEvent selection
   through the contract, default PIR + OccupancyEvent; **and the HoldTime
   timer after chip** (`OccupancySensingCluster.cpp:206-292`: `SetHoldTime`
   with the limits, `SetOccupancy` starting the hold timer on the monotonic
   clock, re-arming on a HoldTime change) — matter.js has no timer
   (`:93-104` only clears the dependents when HoldTime is unset), so this
   is an *Ahead of matter.js* register row. The stale `by_design.md` L02-D02
   (claims PIROccupiedToUnoccupiedDelay served; `definitions.go:50-54` says
   not) is corrected.
4. **PowerSource**: parity tests after `PowerSourceServerTest.ts`
   (Wired default `WiredCurrentType = Ac`, `PowerSourceServer.ts:57-62`; the
   base `with` list Battery/Rechargeable/Wired `:12-15`; Replaceable through
   `.with`, `PowerSourceServerTest.ts:19`), the comparison rows corrected,
   **and the HA projection opens WIRED through the contract** (the
   go-hamapping rows for mains-powered devices are a follow-up there).

## 2 Deliverables

### 2.1 DoorLock FACE (Opus) — `cluster/lock`

- `FeatureFACE = lockdef.FeatureFaceCredentials` in the exported feature
  set and in `servedFeatures` (`doorlock_server.go:62-80`); the `ErrConfig`
  refusal (`:432-435`) and its test `doorlock_auth_test.go:51-58` move to
  the features still refused (ALIRO, ALBU). USR's conformance
  (`PIN | RID | FGP | FACE | ALIRO`, definition `:133`) holds through
  `spectest.CheckServer`.
- The credential paths (`credentials.go:245`, `:345-358`, `:387`) take
  `CredentialTypeFace` (5) wherever they take Fingerprint/FingerVein; read
  each and assert, do not assume: SetCredential, GetCredentialStatus,
  ClearCredential, the user's credential list, the `NumberOfCredentialsSupportedPerUser`
  accounting, and the lock/unlock credential check.
- Parity: the DoorLockServerTest.ts cases that exercise face credentials
  (grep `Face` in `packages/node/test/behaviors/door-lock/DoorLockServerTest.ts`
  at the pin; if none names Face, port the fingerprint cases once more
  with the Face type, named for it) into
  `cluster/lock/doorlockserver_*_parity_matterjs_test.go`; a
  `cluster/lock` test that a FACE server's FeatureMap, attribute list and
  credential enum round trip; the register row of DoorLockServerTest.ts
  updated if a case moves.
- `examples/reference-bridge`: the lock gains FACE in its feature set if
  and only if the DRLK family stays green with it (TC-DRLK-2.x read the
  FeatureMap; run the family in update mode, read every changed case
  count); otherwise leave the daemon's lock as it is and say so.

### 2.2 AdministratorCommissioning BC (Fable) — `cluster/wire`, `node`, `bridge`, `examples/reference-bridge`

- `node.Config.Commissioning` gains the switch (name it with the config's
  existing vocabulary; one shape); off by default. With it on, the wire
  server advertises FeatureMap bit 0 (`cluster/wire/admincommissioning.go:327-331`)
  and `OpenBasicCommissioningWindow` (`:425-430`) opens a basic window
  through the existing path (`bridge/commissioning_window.go:137-141`,
  `:551-558`, `:637-638`; `Node.OpenBasicWindow`, `node/commissioning.go:487`)
  with the node's setup passcode and discriminator, the status
  BasicWindowOpen, the same checks and order as the enhanced command
  (the findings row "AdministratorCommissioning checks in another order",
  `matter_behaviour_findings.md:546-554`, is read before the change and
  closed or kept with a reason).
- The reference daemon turns it on (`examples/reference-bridge`), the
  harness PICS regenerate (`CADMIN.S.F00=1` on endpoint 0), TC_CADMIN_1_4
  and 1_11 run in update mode; the CommissioningWindowLifetimeTest.ts case
  "replaces a window the node opened itself with an administrator's basic
  window" ports out of `missing`; the findings row "No Basic commissioning
  window (BC)" (`:555-558`) closes.

### 2.3 OccupancySensing (Opus) — `cluster/sensing` (new file(s)), `cluster/measurement`, `contract`

- **The reaction.** A hand-written server over the generated one (the
  pattern of `cluster/sensing/ambient.go` over its definition): on an
  Occupancy change with `FeatureOccupancyEvent` set, `EmitOccupancyChanged`
  with the new bitmap (`cluster/spec/occupancysensing/server_gen.go:237`),
  through `OnChanged` (`:201`); never without the feature (matter.js
  `:67-74`; chip always emits, `:312-320` — matter.js wins, record the
  difference in the row's comment). The kit's OccupancySensor device
  (`device/occupancy_sensor_gen.go`) reaches it through its Declare; the
  projected server (§2.3 third bullet) through the same type.
- **HoldTime after chip** (Ahead of matter.js): `HoldTime` (0x3, RW VM),
  `HoldTimeLimits` (0x4, fixed), and the per-detector-type delays the
  definition gates on HoldTime (`occupancy-sensing.element.ts:47-70`);
  a write to HoldTime is refused outside the limits with CONSTRAINT_ERROR
  (`OccupancySensingCluster.cpp:206-214`), an occupancy clear starts the
  hold timer on the component's clock and the Occupancy attribute flips to
  unoccupied when it elapses, an occupancy set cancels it, a HoldTime
  change re-arms the running timer with the remaining time (`:235-260`,
  the elapsed/remaining arithmetic). Tests on `clocktest.Fake`.
  Register row at the head of `notes/parity/by_design.md`: the
  matter.js marker is `OccupancySensingServer.ts:93-104` (HoldTime
  dependents cleared, no timer), the source chip, the parity cases the
  OccupancySensing chip tests (`TestOccupancySensingCluster` or the
  TC_OCC_2_3 steps) — read what exists at the pin and name it.
- **The projection.** `cluster/measurement`'s OccupancySensing server
  (`definitions.go:50-54`, `measurement.go:785-807`) takes the detector
  type(s) and OccupancyEvent from the contract (one new optional field of
  the measurement class the host fills, `contract/matter.go:587-600`
  neighbourhood; stdlib only; default PIR + OccupancyEvent when unset),
  builds its instance per selection instead of the package-level
  `occupancyInst`, and emits the event through the §2.3 reaction. Parity:
  OccupancySensingServerTest.ts "emits OccupancyChanged …" out of
  `missing` (`cluster/measurement/occupancy_sensing_parity_matterjs_test.go`),
  the findings row "OccupancySensing has no feature selection"
  (`matter_behaviour_findings.md:570-577`) closes, by_design L02-D02
  (`by_design.md:980-984`) is rewritten to what is served.
- The reference daemon's occupancy sensor serves PIR + OccupancyEvent (and
  HoldTime with limits if the OCC family's cases read it — TC_OCC_2_3,
  3_1 steps 12/17); OCC family in update mode.

### 2.4 PowerSource (Opus) — `cluster/spec/powersource` tests, `cluster/measurement`, `contract`, docs

- Parity tests after `PowerSourceServerTest.ts` on the kit's server: the
  Wired default `WiredCurrentType = Ac` when unset (`PowerSourceServer.ts:57-62`;
  check the generated server's defaults and add the rule in the
  hand-written layer if the kit lacks it — name the file), the
  Battery/Rechargeable/Wired/Replaceable attribute sets as the definition's
  conformance holds them (`definition_gen.go:81` neighbourhood), each
  selection constructed and read; the "augments descriptor" case stays
  `by_design` (BD-Matter-PowerSource-DeviceTypeOnPart).
- The projection: `cluster/measurement`'s PowerSource server
  (`measurement.go:160-170`, `pwrFeatureBAT`) takes WIRED from the
  contract (a mains-powered device: feature WIRED, `WiredCurrentType`,
  the Wired attribute set; BAT as today; both when the host says so),
  stdlib-only field, default BAT.
- `docs/matterjs-comparison.md`: the Sensing row (line 121) and the
  Actuation row (117) get the feature facts (FACE served, BC a node
  option, OccupancySensing selection + event + HoldTime ahead, PowerSource
  WIRED/RECHG/REPLC served, projection selection); `docs/roadmap.md` row
  36's status line.

### 2.5 Harness and docs (the last agent, once)

- `make chiptool-families-update GOFABRIC_CHIP_FAMILIES=DRLK,CADMIN,OCC,PS`
  after both halves landed; every changed case count explained; the
  certifiability document regenerated.
- CHANGELOG `[Unreleased]`: one line per deliverable under `### Added`
  (FACE, BC option, OccupancySensing selection/event/HoldTime, PowerSource
  projection WIRED) and `### Changed` where a default moved; a facade
  addition is additive (`apidiff-check`).

## 3 Report shape

Per deliverable: what landed (files, options, rows, cases passed /
skipped with class); the register moves (ported/missing/left before →
after); guards table (guard, HEAD, exit) incl. each family run's summary;
differences from this brief; decisions; left out; open questions; every
chip/matter.js line cited, re-read at the pins. Pull request against main
(English, report as body, no labels, auto-merge off), hand back with
SubagentHandback.
