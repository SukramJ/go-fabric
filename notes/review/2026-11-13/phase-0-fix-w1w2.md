# Phase 0 — fixes, waves W1 and W2

- **Brief:** `phase-0-reg-w1w2.md` (dispositions FIX), baseline `95e03096`.
- **Worktree:** `/Users/markus/Documents/GitHub/go-fabric/.claude/worktrees/agent-a2f4a382f6a16968c`,
  branch `worktree-agent-a2f4a382f6a16968c`. 17 commits on `95e03096`, not pushed.
- **Where this file is:** in the worktree, uncommitted. The write to the
  main checkout was refused by the worktree isolation; it was not worked
  around.
- **Pins read:** matter.js `db021da0`, connectedhomeip `6170af84`, from the
  scratchpad export only. Every new citation line was read there; where a
  claim was not read it says "unverified".
- **Result:** 19 of the 21 items implemented (one of them, W2-wire-13, in
  part), 2 not implemented (W2-wire-10, W2-conc-04) plus the DN half of
  W2-wire-13 — reasons in §2.

## 1. Implemented

`ID | commit | files | test (red → green) | CHANGELOG line | by_design / doc follow-up`

**W1-sec-06** | `6788ae54` | `secure/operational/manager.go`, `manager_idle_reaper_test.go`, `manager_close_parity_test.go`
- Test: `TestReapIdleTakesANeverUsedSessionAfterTheTimeout` (direct `reapIdle`
  on the fake clock). Red: `Active() = 1 one idle timeout after creation,
  want 0 — a never-used session must be reaped`. Green after `reapIdle`
  falls back to `CreatedAt()` for a zero `LastActivity()`.
- It replaces `TestStartReaper_NeverMarkedSessionIsNotReaped`, which pinned
  the exemption. `TestReapIdle_ReannounceSkippedWhilePeerKeepsAnotherSession`
  relied on the exemption for its survivor (failed with `graceful notifier
  calls = [52142 52143], want [52142]`); the survivor now has traffic inside
  the timeout.
- CHANGELOG (Changed): *secure/operational: the idle reaper takes an
  established session that never carried a message one idle timeout after
  its creation; before, such a session was exempt for good.*
- by_design: `BD-Matter-IdleSessionReaper` — last sentence ("Never-used
  sessions … stay exempt") is no longer true; `cmd/openccu-loom/daemon_matter.go`
  → `node/security.go:515` (`s.sessions.StartReaper(ctx,
  operational.SessionIdleTimeout, time.Minute)`, read).

**W1-conc-07** | `7133f01b` | `transport/tcp/conn.go`, `owner_test.go`
- Test: `TestConnWaitReturnsForAConnClosedBeforeItsReaderRan`. Red: `Wait on
  a connection closed before its reader ran = tcp: wait tcp://[::1]:5540
  (inbound): context deadline exceeded, want nil at once`.
- `close` closes `served` when `serve` has not started (`serveMu`,
  `servedOnce`); the doc sentence now says "closed before its reader ran".
- CHANGELOG: none (not reachable through the public surface). Follow-up: none.

**W1-conc-08** | `158b41d5` | `bridge/ackpump.go` (`takeOwedAck`), `subscribe.go`, `subscribe_dispatch.go`, `subscription_reestablish.go`, `ota_provider_link.go`, `ackpump_route_release_test.go`
- Tests: `TestPiggybackedAckReleasesTheReplyRoute` (refreshAckCounter,
  piggybackOwedAck). Red: `routes after the ack was taken for a piggyback =
  1, want 0`. `TestSubscribeExchangeLeavesNoReplyRoute` drives a whole
  Subscribe through `dispatch`. Red: `reply routes of the finished Subscribe
  exchange = 1, want 0` — the leak is measured: one route per Subscribe
  exchange (the SubscribeResponse site deliberately does not call
  `dischargeOwedAck`).
- Five `LookupAndDischarge` sites exist and use the helper (the report says
  six and lists five). The OTA and re-establish sites have no flow test of
  their own; they share the helper.
- CHANGELOG (Fixed): *bridge: the reply route of an exchange is released
  when its owed ack rides a reply; before, a Subscribe left one entry behind
  until its session closed.*
- Follow-up: `BD-Matter-CloseSession-PeerAddrHeuristic` source (3) now holds
  exchanges with a pending obligation only (its text already says "no
  pending reliable exchange"); a session whose only address source was a
  leaked route skips the farewell.

**W2-sec-06** | `4ea4fe82` | `cluster/core/operational_credentials.go`, new `operational_credentials_failsafe_port_test.go`, `failsafe_port_rig_test.go`, 11 test files
- Test: `TestOpCredsWithoutFailSafePortFailsClosed`, four commands. Red:
  `CSRRequest without a fail-safe port = <nil>, want FailsafeRequired
  (0xCA)` (same for AddNOC, UpdateNOC; AddTrustedRootCertificate reached its
  decode error instead).
- chip re-read at 6170af84: `OperationalCredentialsCluster.cpp:341`, `:472`,
  `:640`, `:760` — all four `VerifyOrExit(failSafeContext.IsFailSafeArmed(…))`.
- Rigs: 46 tests ran the commands on a nil port. `armedFailSafe(fabric)` is
  the rig's port; a probe run found the 28 `OpcredsConfig` literals whose
  port is actually called, only those got it. `mintUpdateNOC` arms the
  window for the updating fabric; four tests name their fabric.
  UpdateNOC keeps its armed-only check (matter.js parity, ConstraintError
  for another fabric's window).
- CHANGELOG (Changed, behaviour): *cluster/core: OperationalCredentials
  without `OpcredsConfig.FailSafe` (or after `SetFailSafe(nil)`) answers
  FailsafeRequired to CSRRequest, AddTrustedRootCertificate, AddNOC and
  UpdateNOC; before, the guard was skipped. `node` wires the port; a host
  that builds the server itself passes `GeneralCommissioning.FailSafeFabric`.*
- Follow-up: none in `docs/` (no page states the nil rule, grep).

**W2-wire-08** | `a63ac05c` | `bridge/receive.go`, `receive_dispatch.go`, `im_gate.go`, `im_gate_group_silent_test.go`
- Test: `TestGroupSessionArmOfTheIMGateSendsNothing`. Red: `3 datagram(s)
  answered a request on a group session, want none`.
- `rejectGroupSession` is gone; the arm logs and returns nil. Pin lines
  read: `InteractionMessenger.ts:354-358`, `:383-387`, `:401-405` (the three
  throws), `:458-460` (`if (!isGroupSession) sendStatus`);
  `InteractionModelEngine.cpp:1059-1066`. The `im_gate.go` citation
  `:240,269,287` was stale and is replaced.
- CHANGELOG: none (arm not reachable in production). Follow-up: the
  reachability inventory moves (function removed).

**W2-wire-13 (PH/PI half)** | `e6f7ce7c` | `mdns/service.go` (`CommissionableServiceConfig.Validate`), `bridge/bridge.go` (`AnnounceCommissioning`), `mdns/pairing_instructions_parity_matterjs_test.go`, `bridge/commissioning_pairing_hint_test.go`
- Test: `TestAnnounceCommissioningRefusesInconsistentPairingInstructions`.
  Red: `AnnounceCommissioning with a pairing instruction and no pairing hint
  = <nil>, want mdns.ErrInvalidService`. `TestPairingInstructionsParity`:
  one case per throw of `#validatePairingInstructions` (new method, so
  green only).
- Pin lines read: `CommissionableMdnsAdvertisement.ts:40`, `:106-135`;
  `MdnsConsts.ts:15-18`, `:20-32`; bit positions from `PairingHintBitmap.ts`
  (each bit read, not taken from the list order). The first throw (a hint
  object with every flag false) has no Go form: `PairingHint == 0` means
  "not given".
- CHANGELOG (Added): *mdns: `CommissionableServiceConfig.Validate`.*
  (Changed): *bridge: `AnnounceCommissioning` refuses a pairing hint and
  pairing instruction that do not belong together (`mdns.ErrInvalidService`)
  instead of advertising them.*
- The DN bound is **not** implemented — §2.

**W2-conc-06** | `366ba11d` | `im/subscription/engine.go`, `manager.go`, `bridge/receive_dispatch.go`, `subscribe.go`, `subscribe_stop_unanswered_chunk_test.go`
- Test: `TestManagerStopDoesNotWaitOutAnUnansweredReportChunk` (chunk bound
  set to 30 s). Red: `Manager.Stop still waits on a report parked on an
  unanswered chunk after 5 s`.
- Mechanism differs from the report's (no new `Bridge` method): the context
  a reporter receives is cancelled when the engine stops (`run` derives and
  cancels it), the ongoing reporters put its `Done` on the send target
  (`subTarget.abort`), and the chunk wait selects on it
  (`errChunkWaitAborted`, not counted as a send error).
- Not written: the test at `Node.Stop` level. `node/node.go:1025` calls
  `subs.Stop()` directly, so the engine-level test covers the mechanism.
- CHANGELOG (Fixed): *im/subscription, bridge: stopping the subscription
  engine ends an ongoing report's wait for a chunk answer; `Node.Stop` no
  longer stands still for the MRP bound (about 14 s) when a peer vanished
  mid-report. The context a `Reporter` / `EventReporter` receives ends with
  `Manager.Stop`.*

**W2-conc-07, W2-conc-10** | `e846ae09` | `bridge/subscribe.go` (`rewireMeasurementListeners`, `detachHostListeners`), `bridge/bridge.go`, `contract/matter.go`, `bridge/notifier_lock_test.go`
- Tests: `TestReassembleUnsubscribesOutsideTheBridgeMutex` (an unsubscribe
  that waits for a callback in flight, the callback emits a BooleanState
  event), `TestNotifierMayCallBackWhileItRegisters`,
  `TestStopUnsubscribesTheHostNotifiers`. Red: the first two deadlocked —
  the test binary ended with `panic: test timed out after 40s`; the third:
  `unsubscribe calls after Stop = 0, want 1`.
- The wiring no longer runs under `b.mu`: stale unsubscribers and
  registrations run with no bridge lock held; a generation counter makes
  the last wiring win. `Stop` runs both lists (value listeners, switch
  hooks) after the transports are down; `Start` reassembles and wires again
  (`bridge/bridge.go` Start → `reassembleLocked`, read).
- CHANGELOG (Fixed): *bridge: host notifiers are registered and
  unregistered outside the bridge's mutex — a notifier that replays while
  registering, or whose unsubscribe waits for a callback in flight, no
  longer stops the bridge.* (Changed): *bridge: `Stop` unsubscribes from the
  host's notifiers; `Start` subscribes again.*
- Doc: `contract.ChangeNotifier` states the locking rule (also for the two
  attribute notifiers).

**W2-conc-09** | `e81c613c` | `bridge/bridge.go`, `bridge_helpers_test.go`
- Test: `TestHandlerContext_AfterStop_IsCancelled`. Red: `handlerContext
  after Stop: Err() = <nil>, want context.Canceled`.
- `handlerCtx` is set before the serve goroutines start and is not reset by
  Stop. The Start-window half has no test (not deterministic); by reading.
- CHANGELOG: none (internal).

**W2-conc-11** | `758d6548` | `bridge/subscribe.go` (`releaseReportOfClosed`), `subscribe_closed_report_test.go`
- Test: `TestReportOfAClosedSubscriptionLeavesNoRecords` (attributes,
  events). Red: `records of a closed subscription's report:
  subReportExchange 1, reportExchangeOwner 1, reportCounterOwner 1 — want
  none`.
- The interleaving is laid out by hand (subscription closed, the target a
  reporter loaded before the close put back), not through a send hook.
- CHANGELOG: none (internal).

**W2-conc-12** | `e11eda11` | `bridge/exchange_routing.go` (`maxPendingTimedRequests = 8`, `pendingTimedRequests`), `receive_dispatch.go`, `timed_request_bound_test.go`
- Test: `TestTimedRequestsOfOneSessionAreBounded`. Red: `TimedRequest past
  the bound answered Success, want Busy` and `pending timed requests after
  the refused one = 9, want 8 — it must store nothing`.
- chip re-read: `InteractionModelEngine.cpp:963-973`, `CHIPConfig.h:1010-1011`.
  matter.js's bound, if any: unverified (not read).
- CHANGELOG (Changed): *bridge: a session may leave at most 8 timed
  interactions waiting for their Write or Invoke; the TimedRequest past that
  is answered Busy.*
- by_design: a row is needed — the bound is per session, chip's pool of 8 is
  node-wide, matter.js none found.

**W2-conc-13** | `e2775253` | `diagevent/ring.go`, `ring_test.go`
- Test: `TestRecordKeepsItsOwnDetail`. Red: `recorded Detail =
  map[added:after Record peer:changed after Record] after the caller changed
  its map, want map[peer:fd11::1 session_id:7]`.
- CHANGELOG: none.

**W2-conc-15** | `bad1f26d` | `cluster/core/general_commissioning.go` (`failSafeWatch`), `general_commissioning_watcher_test.go`, `failsafe_timer_parity_matterjs_test.go`
- Test: `TestGencomm_ReArmKeepsOneExpiryWatcher` (timers counted on the fake
  clock). Red: `59 expiry timers armed after 100 re-arms, want 1`. The
  expiry fires once at the last deadline; a disarm leaves none.
- The parity fixture counted two pending timers after a re-arm
  (`f.arm(ctx, t, 450, 2)`); now one.
- CHANGELOG: none (internal).

**W2-api-10** | `b411f007` | `endpoint/dispatcher.go` (sources behind `atomic.Pointer`), `bridge/bridge.go`, `docs/guides/advanced-seams.md`, `bridge/acl_lister_live_test.go`, `endpoint/dispatcher_acl_test.go`
- Test: `TestAttachACLListerAppliesToTheRunningBridge`, readers running
  during the swap. Red: `CheckACL after AttachACLLister(UnenforcedACL) on a
  running bridge = Status(0x7E), want Success without a Reassemble`. Green,
  also under `-race`.
- The reassemble applies the sources once more under the lock of the swap,
  so an attach during a reassemble is not lost.
- CHANGELOG (Changed): *bridge: `AttachACLLister` and `AttachAuxiliaryACL`
  apply to the dispatcher of a running bridge at once; before, at the next
  `Reassemble`.*
- Doc: package doc of `bridge` and `advanced-seams.md:105` corrected.

**W2-api-11, -12, -13** | `50bc69fa` | `contract/matter.go`, `bridge/bridge.go`, `docs/guides/ports.md`
- No test (docs). `MatterRead`: answers from memory, no context on purpose.
  `Bridge.Topology()`: the live tree, read-only. `ports.md`: a table of the
  capabilities the bridge finds by type assertion — the report's eight and
  three more read on the way (`SetOnPaseSessionClose`, `CloseFabricExcept`,
  `MatterFabricRemoved`), each with what its absence costs, read at the
  assertion site.
- `mkdocs build --strict` was **not run**.

**R2-P0-doc-A01, R2-P0-doc-A04 (code comments)** | `f1c6ad60`, `01d7730a` | `endpoint/dispatcher.go`, `im/read.go`, `bridge/receive.go`, `bridge/subscribe.go`, `docs/guides/advanced-seams.md`
- No test. The four A01 comments say what the code does: fabric 0 without
  PASE is no grant. `FabricAccessControl.ts:189-191` re-read at the pin.
  A fifth of the same kind was corrected (`reportSubscriptionEvents`: "PASE
  (fabricIndex==0) bypasses"). Left: the `subTarget` field doc
  "fabricIndex==0 means PASE" (`bridge/subscribe.go`).
- A04: the "v1.1" texts at `bridge/receive.go` (subscribe flow) and
  `im/read.go` (DataVersion) are rewritten; the docs citation of
  `AttachACLLister` follows the function (`bridge/bridge.go:1632-1655`). The
  two `by_design.md` rows of A04 are the coordinator's.

## 2. Not implemented

**W2-wire-10 — the proposed fix would make reads stale.** The fix gives a
root or Aggregator server without `contract.ClusterDataVersion` the
endpoint-held version and then honours DataVersionFilters against it.
Nothing advances that version for such a server: every bump site excludes
root and Aggregator — `endpoint/dispatcher.go` WriteAuthorized (`if
res.Status == im.StatusSuccess && !ep.IsRoot() && !ep.IsAggregator()`),
`bridge/subscribe.go` attribute notifiers (`if !persistent && …`),
`bridge/receive_dispatch.go` post-invoke diff (`!ep.IsRoot() &&
!ep.IsAggregator()`). A controller would be told its cache is fresh for
data that changed; today such a cluster is never filtered and always
fresh. A correct fix has to move those three sites and
`CurrentDataVersion` as well, changes the documented contract
(`contract/matter.go`: "Without this capability, the IM layer falls back
to a constant 1"), and dropping the 0 → 1 substitution in `im` puts 0 on
the wire for any other `im.Dispatcher` that reports no version. Done
instead: the stale rationale at `im/read.go` is corrected (A04).

**W2-conc-04 — needs an API-shape decision, and depends on W2-conc-05.**
Read at the pin: `ServerSubscription.ts:534-556` takes both sets into one
`#sendUpdateMessage`; the claim holds. One ReportData needs one reporter
for both. The engine has three host-wired hooks (`Reporter`,
`EventReporter`, `FlushReporter`, wired in `node/security.go:447-449`);
the flush reporter cannot be reused (it sets `target.flush`, sends
untracked and awaits 500 ms), and its doc states the present design:
"[Reporter] and [EventReporter] stay the ongoing paths". So the fix is a
fourth hook that fails open for a host that wires the bridge itself, or
folding the three into one (a break). Two reports in one tick are not an
option (`engine.go`: Apple's duplicate suppression). Which dirty paths may
ride an event report also depends on W2-conc-05: with the Critical bypass
in place, attributes would go out ahead of the MinIntervalFloor.

**W2-wire-13, DN over 32 bytes — refusal matches neither reference and
fails silently in `node`.** matter.js emits the name verbatim
(`CommissionableMdnsAdvertisement.ts:86`); chip copies it into
`char mDeviceName[kKeyDeviceNameMaxLength + 1]` (`Advertiser.h:229`,
`:313`, `TxtFields.h:47`) — it truncates (the body of
`Platform::CopyString` was not read). In `node` the name is
`ProductDescription.Name`, else the NodeLabel (`node/node.go`
`advertisement`), with no length rule in `node/config.go`; a refused
announcement is a warn log (`node/commissioning.go:279-282`) and a node
that cannot be discovered. Refusing therefore needs a place where it is
visible (config validation — a new failure of `node.New`), or the choice
of truncation.

## 3. Guards

Per commit: `gofumpt -l`, `go vet`, `go test -count=1` on the touched
packages, `-race` for the concurrency items. Once more at the end, on
everything touched, at one HEAD (summary log, all exit 0):

```
HEAD before: 01d7730a
gofumpt -l secure/operational transport/tcp bridge cluster/core mdns im endpoint diagevent contract: exit 0 (0 files listed)
go vet <touched>: exit 0
go test -count=1 <touched>: exit 0
CGO_ENABLED=1 go test -race -count=1 <touched>: exit 0
go test -count=1 ./node/... (dependent, not touched): exit 0
go test -count=1 ./script/errprefix/: exit 0
HEAD after: 01d7730a
```

`<touched>` = `./secure/operational/... ./transport/tcp/... ./bridge/...
./cluster/core/... ./cluster/spec/operationalcredentials/... ./mdns/...
./im/... ./endpoint/... ./diagevent/... ./contract/...`. Also `go build
./...`: exit 0.

Not run: `make` targets, repo-wide tests, `mkdocs build --strict`,
`-race` on `./node/...`, the chip-tool suite, reachability / cover /
citecheck ratchets, apidiff. `controller/`, `device/` and `examples/` tests
were not run; they build.

## 4. Noticed, not fixed

1. **`PairingHint` is 16 bits wide** (`mdns.CommissionableServiceConfig`,
   `bridge.CommissioningAdvertisement`). `PairingHintBitmap.ts` at db021da0
   has bits up to 21; four of the hints that require an instruction are bits
   16–19 and cannot be set. chip's field is 10 decimal digits
   (`TxtFields.h:50`). Widening is an API change.
2. **`dischargeOwedAck` drops the route while a displaced obligation may
   wait.** `transport/mrp/ack.go` keeps an obligation a newer one displaced
   for the next `Due`; `bridge/ackpump.go` `dischargeOwedAck` deletes the
   exchange's route unconditionally, so that standalone ack would be dropped
   (`matter.tx.ack_pump.no_src`). Whether it is reached in practice is
   unverified (the pump is woken on every `Owe`). `takeOwedAck` has the same
   non-atomic check-then-delete against `owedInboundAck` (route stored, then
   `Owe`); a retransmission stores the route again.
3. **`drainEvents` stamps `lastReport` before it knows it sends anything**
   (`im/subscription/subscription.go`: the stamp precedes `src == nil` and
   the empty result). Dirty paths then wait a full floor after an event
   drain that sent nothing. Read, no test.
4. **`Ring.Snapshot` hands out the ring's `Detail` maps**; a caller that
   changes one changes the entry. `Record` and the observers have copies.
5. **Citations not re-checked:** `docs/guides/advanced-seams.md:105` keeps
   `bridge/bridge.go:920`, `:1866`, `:1887`, `:2248`, `bridge/receive.go:567`;
   `docs/guides/ports.md` keeps `bridge/bridge.go:29-177` and two more;
   `mdns/service.go` cites `CommissionableMdnsAdvertisement.ts:90` for the
   default hint — at db021da0 the default is applied at `:81` (read).
6. **W2-api-03 residual** (`endpoint/dispatcher.go`, `FindByID(1)`) is not in
   this brief's list and was not touched.
7. **Ratchets that move:** the reachability inventory (removed
   `rejectGroupSession`, the three `wire…Locked` functions renamed; added
   `takeOwedAck`, `rewireMeasurementListeners`, `detachHostListeners`,
   `releaseReportOfClosed`, `pendingTimedRequests`, `failSafeWatch`, …) and,
   possibly, cover floors. Not regenerated here.

## 5. Open questions

1. W2-conc-04: a fourth reporter hook, or one reporter for all three — and
   after W2-conc-05 is decided?
2. W2-wire-13 DN: refuse at `node.Config` validation, truncate as chip, or
   keep matter.js's verbatim name with a `by_design` row?
3. W2-wire-10: record the sentinel as `by_design`, or the fuller fix (bumps
   for capability-less root servers, contract text changed)?
4. W2-sec-06 changes behaviour for a host that builds OperationalCredentials
   without the port. Announced as Changed — is a migration note wanted?
5. W2-conc-12: the bound is per session (8); chip's 8 is node-wide. Keep?
