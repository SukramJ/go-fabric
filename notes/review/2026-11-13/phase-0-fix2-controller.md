# Phase 0, second wave — controller fixes (fix2-controller)

Worktree `.claude/worktrees/fix2-controller`, branch `review2/p0-fix2-controller`,
base `7bce1e25`, HEAD `1c3e585e`. Pins read: matter.js `db021da0`,
connectedhomeip `6170af84` (the scratchpad `pin/` trees; neither is a git
checkout, so the commit is the brief's statement, not measured).

Six commits for five items: item 3 has a follow-up commit (`310c5064`) because
`script/slogkeys` refused two attribute keys after `0a450ba0` was already
under item 5's commit, and a rebase was outside the git operations allowed.

## Per item

### 1. W4-ctlsec-08 — `0763f61b`

- Files: `controller/commissioner/flow.go` (`execute` :302-306,
  `disarmAfterAbort` :797, `abortDisarmTimeout` :55), `flow_test.go`,
  `docs/getting-started/controller.md`.
- What is true now: when `execute`'s context has ended (`ctx.Err() != nil`)
  and the failing step's error is neither a CommissioningError nor a status
  error, the flow stops the BLE re-arm timer, and — a fail-safe armed, the
  current session (PASE, or CASE after Reconnect) not closed — sends
  `#resetFailsafeTimer`'s ArmFailSafe(0) on
  `context.WithTimeout(context.WithoutCancel(ctx), FailSafeMaxSendTime)` and
  returns the step's error unchanged. A CommissioningError caused by the
  ended context still takes the existing branch (unchanged).
- Read at the pins: `ControllerCommissioningFlow.ts:379-468`
  (executeCommissioning; :457 reset, :463-465 rethrow), `:333-351`
  (constructor, no signal), `:1092-1108` (#resetFailsafeTimer), `:302`
  (FAILSAFE_MAX_SEND_TIME); `ControllerCommissioner.ts:194-195`, `:254-298`
  (abort goes to `#establishPaseFromCandidates` only; grep `abort` finds no
  use at :596-725 where the flow is built and run).
  `CHIPDeviceController.cpp:1204-1233` (StopPairing), `:2164-2170`
  (DisarmFailsafeRequest), `:2180-2295` (CleanupCommissioning; the
  CANCELLED branch :2219-2247, failure ignored :2233-2236).
- Red → green: `TestFlowDisarmsFailsafeWhenCallerAborts`. Red (field stubbed,
  no branch): "after the fail-safe is armed": `arms [60], want … [60 0]`;
  "a deadline that passes": same; "a disarm that fails": `ArmFailSafe(0) sent
  0 times`. The three nothing-sent cases (before arm, after
  CommissioningComplete, closed session) pass before and after — guards, not
  reproductions. Green: all six subtests.
- The bound (10 s) is this module's choice, taken from `FailSafeMaxSendTime`;
  neither reference has such a wait (chip sends in the background).
- BLE: `controller/ble/channel.go:223` keeps the GATT context apart from the
  caller's and `pair_race.go:103` closes the channel after the commission
  returns, so the disarm should leave over BLE too — **inferred from those
  two lines, not exercised**.
- CHANGELOG (`### Fixed`): `controller/commissioner`: a `Pair`/`Commission`
  whose context is cancelled or passes its deadline no longer leaves the
  device's fail-safe armed — the commissioner sends ArmFailSafe with expiry 0
  over the session it still holds, on a context of its own (at most 10 s),
  and returns the context's error; a failed disarm is logged
  (`matter.controller.commissioning.reset_failsafe_failed`). New log lines
  `matter.controller.commissioning.aborted`, `…abort_disarm_skipped`.
- Register row (Ahead of matter.js; the docs page already names the id
  `BD-chip-Commissioner-AbortDisarmsFailSafe` — rename both if you rename):

  `| BD-chip-Commissioner-AbortDisarmsFailSafe | `ControllerCommissioningFlow.ts:463-465` at `db021da0`: `executeCommissioning` rethrows an error that is neither a CommissioningError nor a StatusResponseError without `#resetFailsafeTimer` (presence), and the flow takes no abort signal — the commissioner's `abort` option reaches the PASE attempts only (`ControllerCommissioner.ts:194`, presence) | `packages/protocol/src/peer/ControllerCommissioningFlow.ts`<br>`packages/protocol/src/peer/ControllerCommissioner.ts` | `"                } else {\n                    throw error;\n                }"`<br>`"    /** External abort signal that, when fired, cancels all in-flight PASE attempts. */"` | connectedhomeip `src/controller/CHIPDeviceController.cpp` at `6170af84` (`CleanupCommissioning`, the `CHIP_ERROR_CANCELLED` branch :2219-2247; `StopPairing` :1204-1233; `DisarmFailsafeRequest` :2164-2170) | no |`

  Both marker strings checked as substrings of the pinned files, each once
  (lines 463 and 194).
- Entry paragraph: *### BD-chip-Commissioner-AbortDisarmsFailSafe — a
  commissioning whose context ends disarms the fail-safe it armed.*
  matter.js's flow resets the fail-safe for a CommissioningError or a
  StatusResponseError and rethrows anything else
  (`ControllerCommissioningFlow.ts:437-465` at db021da0); it takes no abort
  signal (`:333-351`), and the commissioner's `abort` cancels the PASE
  attempts only (`ControllerCommissioner.ts:194-195`, `:277`). A Go caller
  ends a commissioning through its context, and a step that returned the
  context's error left the device armed until expiry, refusing a new
  commissioning. This module follows connectedhomeip: on `StopPairing`
  (`CHIPDeviceController.cpp:1225-1226` at 6170af84) `CleanupCommissioning`
  sends ArmFailSafe with expiry 0 over the session the commissioning holds
  and ignores a failure (`:2219-2247`, `:2164-2170`). Two differences from
  chip: the send is awaited (at most `FailSafeMaxSendTime`, 10 s) because
  `Commission` closes the PASE session when the flow returns, and the request
  is `#resetFailsafeTimer`'s, with the current breadcrumb where chip sends 0.
  Owner decision 5 (review II). Closes when matter.js's flow takes an abort
  signal and resets on it.

### 2. R2-P0-bound-C01 / W4-ctlint-02 — `b7aaf1bb`

- Files: `controller/dcl/ota.go` (`OTAVersions.MaxImageSize` :85,
  `DefaultMaxImageSize` :47, `ErrOTAImageTooLarge` :51, `Download` :271),
  `ota_test.go`, `controller/softwareupdate/images.go` (doc of
  `ImagesConfig.Ledger`), `images_bound_test.go` (new),
  `docs/getting-started/controller.md`.
- Where the field sits and why: `manager.go:941` →
  `Images.DownloadUpdate` → `i.ledger.Download` (`images.go:671`); the ledger
  is the `*dcl.OTAVersions` the host builds and hands in through
  `ImagesConfig.Ledger` → `controller.OTAConfig.Images`. `OTAVersions` is the
  one struct every download goes through (also `UpdateInfoFromURL`,
  `images.go:745`, and the device side's `node.DCLOTAVersions`, an alias), so
  the bound is a field there rather than on `ImagesConfig`, which would have
  needed a second parameter path into `Download`. `int64`; 0 or less is the
  default.
- Behaviour: announced > bound → `*OTAError` wrapping `ErrOTAImageTooLarge`
  before the request; no announcement → copy cut at bound+1 bytes, same
  error; announced ≤ bound → unchanged (cut at the announcement, the
  existing "exceeds its announced size" error). The former
  `math.MaxInt64-1` check is subsumed.
- Red → green: `TestOTADownloadBoundedByMaxImageSize` (dcl). Red (field
  stubbed): `exceeds its announced size of 2049 bytes, want
  ErrOTAImageTooLarge`, `requested 1 times and 2050 bytes copied`,
  `unannounced … <nil>`, `copied 6146 bytes`, default: `<nil>, 1 requests`.
  Green after. `TestImagesHonourTheLedgersMaxImageSize` (softwareupdate) was
  written after the fix; with `maxImageSize()` forced to `1<<62` it fails
  (`DownloadUpdate … <nil>`), restored, green.
- Not tested: the default bound cutting an unannounced 256 MiB stream (same
  code path as the host-set bound; a 256 MiB test body was not worth it).
- Buffering (not changed): `images.go:664-677` — `bytes.Buffer`, then
  `dcl.VerifyImage` over the bytes, then `Images.Store(ctx, []byte, …)` →
  `Blobs.Put(ctx, name, []byte)`; `:744-748` the same for a URL. `Blobs`
  (`images.go:104-110`) is whole-slice `Put`/`Get`. Streaming to the store
  as matter.js does (`DclOtaUpdateService.ts:571`, `:703`) would be the
  better cut — with the default bound a download can hold roughly twice the
  image during buffer growth plus the store's copy — but it changes the
  public `Blobs` interface hosts implement: a phase 5 API item, not this fix.
- CHANGELOG (`### Added`): `controller/dcl`: `OTAVersions.MaxImageSize`
  (0 is `DefaultMaxImageSize`, 256 MiB) bounds every OTA image download; a
  ledger record that announces more is refused before the request and an
  answer without an announced size is cut at the bound, both a `*OTAError`
  matching the new `ErrOTAImageTooLarge`. (`### Changed`/behaviour: an image
  larger than 256 MiB now needs the field set.)
- by_design candidate (a divergence, not an Ahead row — chip has no DCL
  image download to follow): *BD-Matter-OTAImage-DownloadBound*: matter.js
  fetches with a timeout only and streams to storage
  (`DclOtaUpdateService.ts:690-693`, `:703`, `:571` at db021da0); this module
  buffers and bounds at `MaxImageSize`, default 256 MiB (owner decision 11, a
  judgement, no measured image sizes). `docs/security.md`: the OTA download
  row can now name the bound.
- Coordinator: three new exported names in `controller/dcl`
  (`script/reachability`, apidiff — not run here). `go test
  ./script/surfacecheck/` passes.

### 3. R2-P0-bound-E01 — `0a450ba0`, follow-up `310c5064`

- Files: `client/peer/hint.go` (`MaxHintedAddresses` :98,
  `unicastDestination` :106, the cap :63), `connection.go`
  (`hintLimitLogged`), `hint_test.go`,
  `controller/commissioner/reconnect_probe.go` (`maxProbedInterfaces` :390,
  `addInterfaces` :422, `routableAddr` :168), `reconnect_probe_test.go`,
  `docs/getting-started/controller.md`.
- What was read first: the generated `NetworkInterface` decoder already holds
  the inner lists (`definition_gen.go:260-261`: IPv4 `0..4` of 4 bytes, IPv6
  `0..8` of 16 bytes), but the probe decoded the outer list with
  `spec.DecodeList` (unbounded), and `c.hinted` grew over probes without a
  cap. `routableAddr` already dropped invalid, link-local, loopback,
  unspecified and multicast; `HintAddresses` itself (public API) dropped only
  invalid, port 0 and unzoned IPv6 link-local.
- Source of the bound: `packages/model/src/standard/elements/
  general-diagnostics.element.ts:27` (NetworkInterfaces "max 8"), `:195`
  (IPv6Addresses "max 8"), `:191` (IPv4Addresses "max 4") at db021da0 →
  8 × (8 + 4) = **96**. The probe reads the 8 from the generated definition at
  run time (`gendiag.Definition.Attribute(…).Constraint.Max`, no bound → takes
  none); `peer.MaxHintedAddresses` states 96 and
  `TestReconnectProbeBoundsReportedAddresses` holds it against the product
  computed from the generated definition.
- Decision inside the item: more than 8 interfaces are truncated (first 8
  operational, the rest counted and logged), not rejected, because
  connectedhomeip encodes its platform's whole interface list
  (`GeneralDiagnosticsCluster.cpp:437-446` at 6170af84) while it sizes the
  address arrays to 4 and 8 (`DiagnosticDataProvider.h:42-43`). An interface
  with more addresses than its lists allow still fails the decode and the
  attribute is left out — the behaviour before, now pinned.
- Filter added: IPv4 `0.0.0.0/8` and `240.0.0.0/4` (with 255.255.255.255) in
  both places; unspecified and multicast in `HintAddresses`. The two IPv4
  ranges are named from memory of the IANA special-purpose registry —
  **not re-read from a source here**. A directed broadcast cannot be told
  from a host address and is not looked for (said in the comment).
- Red → green: `TestHintAddressesBounded` (`one call with 106 addresses
  queued 106, want … 96`), `TestHintAddressesUnicastOnly` (9 addresses
  queued), `TestReconnectProbeRoutableIsUnicast` (`routable [fd11::5
  255.255.255.255 240.0.0.1 0.1.2.3 192.0.2.7]`),
  `TestReconnectProbeBoundsReportedAddresses` (with the cap disabled: `132
  addresses, 132 routable, 0 interfaces passed over; want 96, 96 and 3`).
  Green after.
- Not addressed: the probe still re-hands the same addresses every 10 s
  (deduplicated in the peer); the rate is unchanged.
- CHANGELOG (`### Fixed`): `controller/commissioner`, `client/peer`: the
  Reconnect probe takes at most 8 operational interfaces from a device's
  NetworkInterfaces and a connection process at most
  `peer.MaxHintedAddresses` (96) hinted addresses; `Peer.HintAddresses` and
  the probe pass over addresses that name no single host (unspecified,
  multicast, IPv4 0.0.0.0/8 and 240.0.0.0/4). New log lines
  `matter.controller.peer.address_hint_limit`,
  `matter.controller.commissioning.reconnect_probe_interfaces_dropped`.
- Register follow-up: `BD-Matter-Commissioner-ReconnectProbe`
  (`by_design.md:908`) gains the bound: "…what is taken is bounded by what
  NetworkInterfaces may hold — 8 operational interfaces, 96 addresses per
  connection process (`general-diagnostics.element.ts:27`, `:195`, `:191` at
  db021da0) — and filtered to unicast destinations."

### 4. R2-P0-conc-C03 — `1c3e585e`

- Decision by reading: a hang is possible. The queue's jobs are
  `persist.write` (host store, no ctx) and `deliverApplied`
  (`node.go:245-248`), i.e. host hooks, which `Hooks`' doc allows to block
  (`config.go:94-97`); `queueAlive` (`network.go:393`) runs a hook too. The
  module's own job code does not block on the network: ICD registration is
  started on its own goroutine (`icd.go:1219-1223`),
  `backfillFabricIndexOnPeer` reads the image only. So the blocking party is
  the host's hook (or store) — including a hook that waits for a lock the
  caller of `Stop` holds, which was a deadlock no deadline broke.
- Files: `controller/client/jobs.go` (`waitContext` :82; the `sync.Cond`
  became a `drained` channel), `node.go` (`Stop` :512), `config.go` (Hooks
  doc), `stop_context_test.go` (new), `docs/getting-started/controller.md`.
- Behaviour: when ctx ends with jobs still queued, `Stop` returns ctx's
  error after `recompute()` and **skips the final flush** (`persist.stop()`),
  which must follow the drain; a second `Stop` completes it. `Erase` and
  `Registry.deleteNode` already do nothing after a failed `Stop`/`Close`
  (`node.go:635-637`, `registry.go:468-479`), so no queued write lands behind
  an erase.
- Red → green: `TestStopReturnsWithItsContextWhileAHookBlocks`. Red: `Stop
  still waited for the node's jobs a second after its 50 ms context ended`
  (this reproduces the hang the reviewer had only read). Green after.
- CHANGELOG (`### Fixed`): `controller/client`: `Node.Stop`/`Close` (and
  `Registry.Stop`, `Close`, `Decommission`, `Forget`) no longer wait past
  their context for a report hook that blocks; they return the context's
  error, the queue finishes by itself, and a second `Stop` runs the store's
  final flush.
- by_design: a sentence under the existing drain citation is enough —
  matter.js drains on every exit path without a deadline
  (`packages/node/src/node/client/ClientStructure.ts:482-486` at db021da0,
  re-read: the `finally` block's comment and `await queue;`). The code keeps
  the existing citation line and adds no new one.

### 5. Reference controller documentation — `5e4bfd01`

- Files: `examples/reference-controller/README.md` (flag row, `group
  create`/`group send`, the section now "The controller's own identity", the
  database table, the ledger sentence), `cli/cli.go` (help text, comment),
  `cli/stack.go`, `cli/group.go`, `cli/args_test.go`.
- The retirement: `git log -S"nodeListen"` → `7e895c1d` (#416, phase 37)
  removed the one reader (`Listen: s.e.opts.nodeListen`); `git describe
  --contains` → `v0.18.0~14`; CHANGELOG `[0.18.0]` lists the removal. Help
  text: "deprecated: no effect since v0.18.0, which retired the controller's
  second node (the group sender and the OTA provider use --listen); will be
  removed".
- Database table: the old "other `matter_*` tables = the own node's store"
  row is now limited to what I read (`retireOwnNode`,
  `controller/ota_provider.go:253-276`, removes the record and the registry
  node only). What those tables hold in a fresh database today was **not
  checked** and the README no longer says.
- Red → green: `TestNodeListenFlagIsDeprecatedNotRemoved`, against the old
  `cli.go`: `help "the UDP address of the controller's own node …" lacks
  "deprecated"` (and the two other phrases). Green after.
- CHANGELOG (`### Deprecated`): `examples/reference-controller`:
  `--node-listen` is deprecated — without effect since v0.18.0 — and will be
  removed.
- No finding id of its own: "Noticed 1" of `phase-0-fix-w4w5.md`.

## Not implemented

None of the five. Within them, deliberately left:

- Item 2: streaming the download to the store (a `Blobs` API break).
- Item 5: removing `--node-listen` (a CLI break, bundled later, per brief).

## Guards (worktree, HEAD `1c3e585e` unless stated)

| Command | Exit |
| --- | --- |
| `gofumpt -l client/peer controller/commissioner controller/dcl controller/softwareupdate controller/client examples/reference-controller` | 0, no output |
| `go vet` on each touched package (per item, before its commit) | 0 |
| `go build ./controller/... ./client/... ./node/... ./examples/...` | 0 |
| `go test -count=1 -p 4 ./controller/commissioner/... ./client/peer/...` | 0 |
| `CGO_ENABLED=1 go test -race -count=1 -p 4 ./client/peer/... ./controller/commissioner/...` | 0 |
| `go test -count=1 -p 4 ./controller/dcl/... ./controller/softwareupdate/... ./examples/reference-controller/...` | 0 |
| `go test -count=1 -p 4 ./controller/client/...` (item 4 in the tree, before its commit) | 0 |
| `CGO_ENABLED=1 go test -race -count=1 -p 4 ./controller/client/...` | **1 of 6 runs failed**, see below |
| `CGO_ENABLED=1 go test -race -count=25 … -run 'TestSubscriptionAlive\|TestClientAttestation\|TestStopReturns\|TestStopDuring\|TestPersister'` | 0 |
| `go test -count=1 ./script/errprefix/` | 0 |
| `go test -count=1 ./script/slogkeys/ ./script/docscheck/ ./script/sqlitedsn/ ./script/archcheck/ ./script/surfacecheck/ ./script/citecheck/` | 0 (slogkeys failed once before `310c5064`, which is why that commit exists) |

Not run: `make` targets, `./...`, `./controller/` root package,
`reachability`, `cover-check`, `cite-check` against the pins, apidiff, mkdocs.

## New defects and observations

1. **One unexplained race report in `controller/client`.** The first
   `-race` run of the package (items 1–3 committed, item 4 in the tree)
   ended `--- FAIL: TestClientAttestationTrustStoreParity (0.00s) …
   testing.go:1867: race detected during execution of test`. The detector's
   own report was lost — my output filter (`grep -v`, `tail`) dropped it.
   Five further full runs and 25 targeted iterations passed, with full logs
   kept and no `DATA RACE` in them. The test named is where the testing
   package noticed the counter, not necessarily where the race is. **I
   cannot say whether it is from this branch or was there before; it was not
   run on `7bce1e25`.** Re-reading `jobs.go` after the change found every
   access to `drained` under `q.mu`. Worth a `-race -count=20` on the base
   and on the integrated branch.
2. `docs/getting-started/controller.md:66` still says the reference
   controller's "own node is reached through `Peers` and
   `Commissioner().Interaction`" — the same stale class as item 5, outside
   its listed scope; not changed.
3. A facade host (`node.DCLOTAVersions`) gets `MaxImageSize` through the
   alias, but `node` routes neither `dcl.ErrOTAImageTooLarge` nor
   `dcl.DefaultMaxImageSize` (`node/routes.go:70-92`), and
   `docs/guides/advanced-seams.md:46` does not mention the bound. Not added:
   a `node` API decision.
4. The existing error branch of `execute` (`flow.go:288-301`) resets the
   fail-safe without stopping the BLE re-arm timer first, so a re-arm in
   flight can land behind the reset; matter.js has the same order (the
   interval stops at dispose, after `executeCommissioning` threw). The new
   abort path stops the timer first; the old branch is unchanged. A re-arm
   already in flight can still land behind either.
5. After a `Stop` that timed out, the persister is not stopped, so its timer
   can still flush later — into a database the host may have closed, which is
   a `store_write_failed` warning, not a crash (read, not exercised).

## Open questions

1. Item 1: is 10 s (`FailSafeMaxSendTime`) the wait the owner wants for an
   aborted `Pair` against a device that no longer answers, or shorter?
2. Item 1: the abort path triggers on `ctx.Err() != nil` whatever the step's
   error is. Should a non-context error on a live context (the "other error"
   row of `TestFlowErrorClasses`) disarm too, as chip does for a failure in
   the early stages (`CHIPDeviceController.cpp`, the last `else` of
   `CleanupCommissioning`, comment at :2273-2275)? Left as matter.js has it.
3. Item 3: 96 is the legitimate maximum, far above the reviewer's "single
   digits". If the owner wants the tighter number it is a policy bound, not
   one the cluster gives.
4. Item 4: on a timed-out `Stop` the final flush is skipped rather than run
   beside the still-running queue. The alternative (flush anyway) breaks the
   documented order "jobs drain before the final flush"; confirm the choice.
