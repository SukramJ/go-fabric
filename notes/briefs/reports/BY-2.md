# Report BY-2 — gate-2 fixes before v0.10.0

Brief `notes/briefs/2026-10-22-BY2-gate2-fixes.md`, branch `m4/by2`,
matter.js pin `862ad75f`. Code and snapshots at `cb86b3eb`; the report commit adds only this file.

## §1 Docs a consumer can read (G2-3)

- The programs are inline: `docs/getting-started/device.md:34` and
  `controller.md:34` carry a `<!-- program: <path> -->` marker and the
  whole file as a fenced block. `script/docscheck/docscheck_test.go:40`
  (`TestInlinePrograms`) holds each block to its file (`-update` rewrites
  them). The `--8<--` includes are gone (`mkdocs.yml`).
- Citations: every in-repo `file:line` in `docs/` and the package docs of
  `node`, `device`, `fabrictest` was regenerated from the commit that
  wrote it. `TestCitationsResolve` (`docscheck_test.go:116`) checks that
  each one names a file that exists and lines inside it.
- `fabrictest/doc.go:26` uses `onoffdef`.
- `docs/guides/migrating-to-the-facade.md`:
  - `v0.10.0` sections at :41 and :71.
  - The `OnMatterAttributesChanged` rows at :59-60, one per server that had it.
  - The one-shape rows at :79-84.
  - The alias note at :86.
- `docs/guides/advanced-seams.md:35-36`: the controller role's seams
  (`fabrictest.Controller.Exchanges/Names/Peers/Subscriptions`,
  `commissioner.Context`/`Channel`).
- G2-4, construction versus run time: said in `device.md:252` and in the
  generated setter docs (`script/clustergen/hull.go:262`, `:270`).
- G2-6: `controller.md:348` says when to use `AutoSubscribe` and when
  `Node.Subscribe`, and that `KeepSubscriptions` is the rule for both.
- P5 doc comments:
  - `IsLocked`'s `observed` (`cluster/lock/doorlock_server.go:115`).
  - `spec.Nullable`'s zero value (`cluster/spec/codec.go:283`).
  - `Cmd*` constants say `timed` (`script/clustergen/gen.go:558-560`).

## §2 One shape (G2-5, G2-6, G2-7)

- `OnChangedAsync` returns only `unsubscribe func()`, on the typed server
  (`cluster/spec/typed.go:192`, the generated `Server.OnChangedAsync`) and
  on `device.Device` (`device/lifecycle.go:174`). `spec.ErrNoQueue` is
  removed.
- `device.Options.NodeLabel` is removed. Its test moved to `Rename`
  (`device/lifecycle.go:137`).
- `node.Advertiser = mdns.Advertiser` (`node/config.go:29`) and
  `node.Groups = groups.Manager` (`:35`).
- `schema.DeviceTypeID` (`schema/devicetypes.go:15`) is written by
  `script/generate_matter_schema.go:323-329`. It types `device.Spec.Type`,
  `Type.ID`, the device-type constants, `TypeByID` and the `schema`
  lookups. `schema.DeviceTypeIDOf` (`schema/lookup.go:44`) converts a
  Descriptor's 32-bit devtype-id and is false above 0xFFFF.
- CHANGELOG: `### Changed` and `### Removed`.

## §3 Errors are values (G2-10, G2-11)

- The inventory's sites (`icd_management.go:87`,
  `network_commissioning.go:230`, …) already return `refusedWrite`. The
  FAILURE came from its fallback for a refusal the definition does not
  judge. That fallback now answers UNSUPPORTED_WRITE
  (`cluster/core/definitions.go:47-51`).
- The Schedules invoke answers UNSUPPORTED_COMMAND
  (`cluster/wire/schedules.go`, `spec.Errorf`).
- One dispatcher-level test covers every site
  (`cluster/core/status_typing_test.go:50`, `TestRefusalsAnswerTheirStatus`).
- The stale comment in `group_key_management.go` was rewritten (comment
  only).
- `fabrictest.Site.Eventually`/`Settle`: on a context without a deadline
  they end after `Options.Wait` (`DefaultWait` 30 s, on the wall clock)
  with `ErrWaited` (`fabrictest/site.go`, test
  `fabrictest/wall_internal_test.go`). `testing-in-process.md` sets a
  deadline anyway.

## §4 The same bar inside (G2-12)

- `script/clustergen/hull.go:390` writes `State.FromAttributeValues` per
  field, implementing `spec.StateReader` (`cluster/spec/typed.go:82`). It
  converts through `spec.Assign` (`:91`).
- `StateOf`, `OnChangedOf`, `OnChangedAsyncOf` and `OnWriteOf`
  (`typed.go:124-210`) have no struct walk by reflection any more.
- `SetPatch` takes a `spec.PatchValuer`. `spec.PatchValues` is removed.
  The generated types carry no `matter` tags.
- `cluster/spec`'s own test structs write their methods by hand
  (`cluster/spec/typed_internal_test.go`).

## §5 Locks and ownership (G2-13)

- `bridge/group_messaging.go:230` (`stopGroupNetworking`) takes the
  joined set under `groupNetMu` and leaves the groups after unlocking.
  Test: `bridge/group_messaging_e2e_test.go:566`.
- `client/peer/set.go:196` (`AddKnownPeer`) looks the fabric up before it
  takes `s.mu`. Test: `client/peer/set_lock_test.go:16`.
- `client/interaction/sustained.go:278` already called
  `VerifyReachability` after releasing `st.mu` (snapshot at :274-276), so
  nothing changed there.
- §3c rows: two hooks that run under a serialising lock now say so —
  the transition step hook (`cluster/transition/transition.go`) and the
  peer-leave hook (`controller/client/config.go`).
- Mutex comments: the BX3 script wrote 6 (`7a2a4fa3`) and 28 were written
  by hand (`95b18072`). A dry run of `bx3-mutex.go` now proposes only the
  five on the security paths, left for the owner:
  - `cluster/core/access_control.go:46`
  - `cluster/core/general_commissioning.go:42`
  - `cluster/core/group_key_management.go:38`
  - `cluster/core/operational_credentials.go:64`
  - `secure/sigma/initiator.go:42`

## §6 BY-1's two questions

- One DataVersion advance per controller write: the bridge's listener
  skips the advance for a change with `OriginControllerWrite`, because
  the dispatcher already advanced it (`bridge/subscribe.go`; matter.js
  `Datasource.ts:1440-1448`, `advanceVersion` once per commit). Test:
  `node/dataversion_write_test.go:41`.
- The OTA requestor reports DefaultOTAProviders only when the list
  changed, from `OriginControllerWrite`
  (`cluster/core/ota_software_update_requestor.go:510`, test
  `ota_software_update_requestor_test.go:1288-1301`).
- The EVSE's MaximumChargeCurrent that a UserMaximumChargeCurrent write
  moves reports `OriginControllerWrite` (`cluster/energy/evse.go`, test
  `cluster/energy/origin_test.go:162`).
- The Binding list reaches `OnChanged`: `OriginControllerWrite` for a
  write, `OriginHostSet` for a host's set or restore
  (`cluster/core/binding.go`, test `binding_change_test.go:250`).

## §7 Guards as code

- `.golangci.yaml:44` (`forbidigo`) refuses a method called on
  `slog.Default()` inline.
- `script/errprefix/errprefix_test.go:84` checks that every
  `errors.New`/`fmt.Errorf` literal starts with its package's prefix. The
  exceptions are rules, not a file list: a `%w` wrap, `package main`, and
  the owner's security paths on a ratchet. It found 46 texts, now
  prefixed (`2a156dc0`).

## Flakes

20 runs each, `go test -race`, while `make race` ran beside them.
"Before" is main (`98612ef3`); "after" is a snapshot of the tree at
`9f191249`.

| Test | Before | After | Cause, fix |
| --- | --- | --- | --- |
| `TestDelayReportDataParity` | 8 pass, 3 fail; run 12 hung until the 10 min package timeout | 20/20 (1076 s) | `fabrictest`: the standing system-call goroutines were counted once, when the site was built. Another parallel test's passing system calls were counted too, and once those ended they hid this site's own work, so the fake clock ran ahead of a sender. Now the count is the fewest seen (`fabrictest/site.go`, `busy`, `9f191249`). G2-16. |
| `TestOtaParityMatterJS` | 18/20 (`ota_parity_matterjs_test.go:491`, "the update in progress stayed stored") | 20/20 (701 s) | The test read the record before the requestor cleared it. matter.js clears `updateInProgressDetails` after `#handlePreviousUpdateOnStart` returned (`OtaSoftwareUpdateRequestorServer.ts:241-244`). The two restart cases now wait for the cleared record (`9f191249`). |
| `TestDiagLogsBDX_Transfer` | 20/20 (259 s) | hung on run 5 with the old test; fixed in `ac463ec5`, then 300 runs × `-cpu=1,2,8` pass | The transfer goroutine calls `EndLogCollection` before `done()` frees the slot (`cluster/core/diagnostic_logs.go:587-593`). The test sent its next request on `<-c.ended`; one that arrived between the two was answered Busy, opened no exchange, and `receive` waited forever. The test now waits with `MatterQuiesce` (the owner test's form). The 50 ms negative wait stays: it bounds nothing, a loaded machine only makes it more lenient. |
| `TestRegistryDecommission` | 20/20 (267 s) | 20/20 (22 s) | Not reproduced. Sound. |
| `TestNetworkLossAndDelay` | 20/20 (177 s) | 20/20 (81 s) | Not reproduced. Sound. |
| `TestLiveSustainedSurvivesRestart` | 20/20 (68 s) | 20/20 (22 s) | Not reproduced. The test's assumption is right; see below. |
| `controller/commissioner` (whole package) | ok, no `another PASE attempt won` line (342 s) | ok, none (86 s) | Not reproduced. The owner's hypothesis did not hold on reading, see below. |
| `TestLevelControlChangesReachHostAndSubscriber` (BY-1's) | — | 20/20 (78 s) | Control run. |

**`TestLiveSustainedSurvivesRestart`** (`client/interaction/live_test.go:322`):
the node restarts, not the controller. The test asserts that the
controller's sustained subscription keeps its id and stays active. That
is right:
- The restarted node re-establishes a persisted subscription under its
  stored id: `establishFormerSubscription` builds the `ServerSubscription`
  with `id: subscriptionId` (matter.js
  `packages/node/src/node/server/InteractionServer.ts:851-889`). It is
  called per former subscription from
  `SubscriptionsServer.ts:217`/`:302`, which `CommissioningServer.ts:260`
  starts.
- The client routes a report by (peer, id): `ClientSubscriptions.getPeer`
  (`packages/protocol/src/action/client/subscription/ClientSubscriptions.ts:148-150`).
  An unknown id is answered InvalidSubscription and dropped
  (`ClientSubscriptionHandler.ts:88-103`).
- A `SustainedSubscription`'s id changes only after its underlying
  subscription closed (`SustainedSubscription.ts:41-42`, `:150-153`). In
  the test the node returns inside the max interval, so nothing closes.

**Commissioner `errWon`**: the session that loses the race is the
attempt's own `s` (`controller/commissioner/connection.go:273-279`), and
`HandlePeerLoss` acts on that session's exchanges only
(`client/exchange/session.go:286-298`). `Config.PeerLost`, the
manager-wide hook (`client/exchange/manager.go:882-885`), has no caller
in the tree. So the loser's loss cannot reach the winner through this
path.

## Guards

| Guard | HEAD | Exit |
| --- | --- | --- |
| `make ci` (build, vet, test, lint) | `4a14791a` | 0 up to `cover-check`, which failed (schema 97.3 %, two floors missing) |
| `make cover-check` | `e9626e46` | 0 |
| `make ci` | `cb86b3eb` | 0 |
| `make race` | `ac463ec5` | 0 |
| `make race` (second; started at `ac463ec5`, `cb86b3eb` landed during it: a comment and snapshots) | `cb86b3eb` | 0 |
| `make archcheck` | `ac463ec5` | 0 |
| `make reachability` + `-check` (unreached 5, on its ratchet) | `cb86b3eb` | 0 |
| `make cite-check` (hard 0, drift 263, on its ratchet) | `cb86b3eb` | 0 |
| `make testparity-check` | `ac463ec5` | 0 |
| `go test ./script/...` | `cb86b3eb` | 0 |
| `mkdocs build --strict` | `ac463ec5` | 0 |

What the earlier runs found, all fixed in the commits named:
- The first `make ci` (`1e3372d7`) failed lint on four findings (gocritic,
  gosec G115, unconvert, perfsprint) → `700821f8`.
- The second (`700821f8`) failed lint's typecheck: the `chiptool`-tagged
  `internal/chiptool/datamodel_test.go` still used `uint32` device-type
  ids → `4a14791a`.
- The third (`4a14791a`) failed `cover-check` → `e9626e46`.
- The first `reachability-check` found `spec.StateReader` unreached. It is
  a constraint interface that RTA does not record, now annotated as
  `Assign` is → `cb86b3eb`.
- `cite-check` regenerated `drift.json`: line shifts only, drift unchanged
  at 263 → `cb86b3eb`.

chip-tool did not run: no FeatureMap or composition moved, so no
`needs-chiptool` label.

## Differences

- §3: the inventory named the sites. The fix sits in their shared
  fallback (`refusedWrite.MatterStatusCode`), not at each site, because
  each site already returned the typed value.
- §5: `sustained.go:278` needed no change; the call was already outside
  the lock.
- `TestDiagLogsBDX_Transfer` failed only after the change, but the race
  is older than this branch: no commit here touches DiagnosticLogs, and
  the after run, not the before run, happened to hit it.

## Decisions

- §1 form: the inline block plus a test that holds it to its file, not a
  link plus 40 lines. Both GitHub and the strict mkdocs build render a
  fenced block; a snippet include renders on neither GitHub nor `go doc`.
- `Eventually`/`Settle`'s default bound is on the wall clock
  (`Options.Wall`), not the site's fake clock: the bound is for a test
  that hangs, which the fake clock does not measure.
- `DiagnosticLogs` keeps its order (`EndLogCollection` before the slot is
  free); only the test waits. A host learns that the collection ended
  before a new request can start one, which is what `MatterQuiesce` is
  for.

## Left out

- §5 §3c: beyond the two hooks above, no per-row list of the 55 + 30
  funcvalue/funcfield rows was written into this report.
- The five security-path mutexes (§5), left for the owner.
- The commissioner `errWon` flake: not reproduced in 20 runs. The read
  above rules out the loser-hits-winner path; no other cause was found.

## Questions

- `make lint` runs golangci-lint with `--timeout=5m`. On the first
  `make ci` it hit that timeout while the flake runs loaded the machine;
  it finished well inside the limit without that load. Raise the
  timeout, or keep it and run lint on a quiet machine only?
- The two `make race` runs used only as background load for the flake
  reproductions exited 2. The after run's log shows "no space left on
  device" on the `/tmp` tmpfs; the before run's log was not examined.
  Neither is a result: the clean `make race` runs in the table are.
  Should a load run get its own `GOTMPDIR` on disk?

## `by_design` candidates

None.
