# Report CB — M4 phase 21b: metrics port, diagnostic trace and slog convention on the facade; the Base device type's optional clusters

Brief: `notes/briefs/2026-10-24-CB-m4-phase-21b-observability.md`. Branch
`m4/phase-21b` from `origin/main` `e1b2bff8` (CA, #247). matter.js pin
`862ad75f`.

## Metrics

`contract.Metrics` (`Counter(name, delta)`, `Gauge(name, value)`, stdlib
only, no attributes in this wave, said in the doc comment);
`node.Metrics = contract.Metrics`; `node.Config.Metrics` (nil records
nothing). The names are `contract.Metric…` constants, re-declared as
`node.Metric…`; `contract.MetricNames()` lists them. Below the facade
the seam is `bridge.Bridge.AttachMetrics` (atomic, like the trace).

| Name | Kind | Source counter | Site |
| --- | --- | --- | --- |
| `sessions.open` | gauge | `operational.Manager.Active()` through an optional `Active() int` on the session registry | `bridge/conn.go` `bindEstablishedSession` (every responder PASE/CASE/resumption), `bridge/case_initiator.go` (initiator), `Bridge.SessionClosed` (every close path) |
| `subscriptions.active` | gauge | `subscription.Manager.Active()` | `bridge/subscribe_dispatch.go` (beside `subscriptionsSucceeded`), `bridge/subscription_reestablish.go` (`Restore`), the manager's close hook (`bridge/subscribe.go`) |
| `im.received` | counter | `deviceLoadCounters.imReceived` | `bridge/receive.go` `handleIMOpcode` |
| `im.sent` | counter | `deviceLoadCounters.imSent` | `bridge/reply.go` `sendReplyOpts`, `bridge/subscribe.go` `sendUnsolicitedIM` |
| `pase.failures` | counter | `Bridge.paseFailures` | `bridge/securechannel.go` `recordPaseFailure`, every call |
| `pase.lockouts` | counter | `engagePaseLockout` | same function, the transition to the cap |
| `aggregator.reassemblies` | counter | `Aggregator.reassemblies` | `node/aggregator.go` `reassemble` |
| `mrp.retransmits` | counter | `outboundReliableTracker.Tick` results without error; `client/exchange` `retransmit` | `bridge/ackpump.go` `tickOutboundReliable`; `client/exchange.Config.Retransmitted` (new), wired by the node's client stack |
| `panics.recovered` | counter | `recoveredPanics` of `transport/udp.Listener`, `transport/tcp.Listener`/`Dialer` | new `OnPanicRecovered(fn)` on all three; the bridge sets it on its UDP and TCP listeners at `Start`, the node on its client socket |

Every existing reader stays (`Bridge.DeviceLoad`, `RecoveredPanics()`,
the lockout counter). `node/metrics/expvar` (package `expvarmetrics`):
`New(prefix)` publishes the set at zero, shares a variable a second `New`
published, `Value(name)` for a reader. The reference bridge:
`--metrics-expvar <addr>` serves `/debug/vars` (`examples/reference-bridge/metrics.go`).
Tests: `fabrictest.Metrics`/`NewMetrics()` (set through
`Topology.Configure`); `node/observability_test.go` — a pairing
(`sessions.open`, `im.*` against `DeviceLoad`), a subscribe and its close
(`subscriptions.active` 1 → 0), a wrong passcode (`pase.failures`), an
`Aggregator().Add` (`aggregator.reassemblies`), a dropped reply
(`mrp.retransmits`); `bridge` brute-force test (20 failures, 1 lockout),
`bridge/metrics_internal_test.go` (nil port, detach, panic hook, gauge),
the UDP/TCP panic tests (the hook runs once), `client/exchange`
`TestRetransmissionSchedule` (`Retransmitted` = transmissions − 1),
`expvarmetrics` round trip.

## Ring

`node.Config.Diagnostics{Capacity}` (0 → `node.DefaultDiagnosticsCapacity`
= 256, today's value); `Node.DiagnosticEvents()` (newest first, the ring's
`Snapshot`); `Node.OnDiagnosticEvent(fn)` inline and
`Node.OnDiagnosticEventAsync(fn)`, both returning the unsubscribe function
as `Device.OnChanged` does. `diagevent.Ring.Observe(fn)`: the observers
run after `Record` released the ring's lock (and the observer lock), in
registration order, each with its own copy of `Detail`; an observer may
read or record into the ring. The async variant queues on the node's
serial goroutine, started at `Start`, drained at `Stop` after the bridge
stopped — the device kit's queue, moved to `internal/serialqueue`
(foundation layer) so both owners use one type. `Bridge.DiagnosticEvents`
and `AttachDiagnosticEvents` stay. `advanced-seams.md` row: "Since
v0.10.5 the ring is read on the facade"; `matterjs-comparison.md:141`
unchanged. Migration guide: `diagevent` import → `node.DiagnosticEvent` /
`Node.DiagnosticEvents()`. Test: `TestDiagnosticEventsOnTheFacade`
(capacity 2, three events from a commissioning and the controller's
restart, inline = async, newest first, an unsubscribed observer silent);
`diagevent` `TestObserve`.

## slog

Renames in library code (scripted, anchors asserted; counts):

| From | To | Sites |
| --- | --- | --- |
| `slog.String("err", x.Error())` | `slog.Any("err", x)` | 231 (incl. examples/, script/) |
| `slog.String("err", <text>)` | `slog.Any("err", <error>)` | 2 (`binding/resolve.go`, two sentinel errors) |
| `session` | `session_id` | 10 |
| `exchange` | `exchange_id` | 14 |
| `fabric` | `fabric_index` | 5 |
| `fabric` (a fabric id) | `fabric_id` | 1 (`controller/commissioner/commissioner.go:501`) |
| `endpoint_id` | `endpoint` | 3 |
| `subscription`, `id` (im/subscription) | `subscription_id` | 6 + 2 |
| `group` | `group_id` | 2 |
| `peer_node`, `peer_node_id` | `node_id` | 10 + 1 |
| `peer` / `node` holding a node id (`slog.Uint64`) | `node_id` | 9 + 2 |
| `dst` | `peer` | 1 |
| `address` (the commissioner's peer) | `peer` | 4 |
| `dest` (a group's multicast address) | `address` | 1 |
| `addr` (a local socket) | `listen` | 2 |
| `addr` (a dial attempt's index) | `address_index` | 1 |
| `why` | `reason` | 3 |
| `vid`, `pid` | `vendor_id`, `product_id` | 2 calls |
| `cmd`, `cmdID`, `respType` | `command_name`, `command`, `response_type` | 6, 2, 2 |
| `current_fabric`, `resolved_idx` | `current_fabric_index`, `fabric_index` | 1 call |
| `idx` | `path_index` | 2 |
| `protocol` (an id); `general`/`protocol` (codes) | `protocol_id`; `general_code`/`protocol_code` | 1; 1 call |
| `device` (a commissioning candidate) | `candidate` | 4 |
| examples: `error` (an error-state id) | `error_state` | 2 |

Security-path lines touched (keys and the brief's logger fallback only):
`client/handshake/case.go:68,104,126,135,267`; `pase.go:46,80,94`;
`server.go:80,113,162,176,410-411,414`;
`cluster/core/access_control_auxiliary.go:102`;
`cluster/core/general_commissioning.go:441-442,446-447,450-451`;
`cluster/core/operational_credentials.go:1185-1186,1190-1191,1194-1195,1830,2138-2139`.
`secure/` untouched; `group_key_management.go` untouched.

Logger fallbacks: `client/handshake/{server,pase,case}.go`,
`client/exchange/manager.go`, `client/interaction/{interaction,subscriptions}.go`,
`client/peer/set.go` → `slog.Default()` (doc comments updated);
`fabrictest/site.go` keeps discard, with the reason in a comment.

Guard `script/slogkeys` (test package next to `script/errprefix`, run by
`go test ./script/...` in `make test`/`ci`, same walk and skips; package
main exempt): an attribute key outside the table, a non-`Any` `err`, a
key stated twice in one call; floor 1000 judged attributes (1152 today);
an area key nothing uses fails. The table is a Go slice: 15 core keys
with value and meaning, 240 area keys. `docs/guides/observability.md`'s
key section is generated from it (`-run TestGuideTable -update`) and
checked by `TestGuideTable`. `TestCheckFileReports` proves the checker.
No ratchet.

`docs/guides/observability.md` (nav next to Persistence): the logger
rule, event-name prefixes (kept as they are), the key table, the metric
table, expvar, the trace; ports.md rows for `Metrics`, `Diagnostics`.

## Base clusters

The rule sits in `device/base.go`: `baseOptionalServers` reads
`schema.BaseDeviceTypes()` and keeps the server-cluster requirements of
plain conformance `O` that have a committed definition — FixedLabel and
UserLabel, matter.js `packages/model/src/standard/elements/base.element.ts:46-47`
at `862ad75f`. `device.New` builds from `withBaseOptional(t, s)`: the type
plus those requirements the declarations name, so `checkOptedClusters`'
refusal (`device.go`, "has no server cluster requirement") keeps
applying to every other cluster; `d.Type()` stays `t`. Not a per-type
exception, not hand-coded ids. Self-held defaults: `core.NewFixedLabel`,
`core.NewUserLabel` (`device/defaults.go`); `Device.FixedLabel()` and
`Device.UserLabel()` typed accessors on the embedded `*Device`. UserLabel
persistence: `node/user_labels.go` `persistUserLabels`, beside
`persistBindings` in `observeTopology`, under
`bridged_user_label.<stable key>` in `store.Settings` (the root's and
the Aggregator's keep `user_label.root`/`.aggregator`); LabelList quality
N, `user-label.element.ts:21-22`. Binding (`base.element.ts:45`,
`Simple & Client`) out of scope. `device.FixedLabel` removed; the
reference bridge's light declares `fixedlabeldef.Declare{Initial: …}`
(same endpoint, same labels: no chip-tool run). Tests:
`TestBaseOptionalServers`, `TestBaseOptionalOnEveryType` (every type
that constructs with defaults), `TestBaseOptionalConformsAndRefuses`
(no violation, over-long label refused, BooleanState refused),
`TestSelfHeldDefaults` cases 0x0040/0x0041, `host_options_test.go` moved
to BooleanState (0x0045), `node/base_labels_test.go` (wire read of the
labels, a UserLabel write reaching `OnChanged` with
`OriginControllerWrite`, the list restored by a new node on the same
store — the last fails with `persistUserLabels` disabled).

## Guards

| Guard | HEAD | Result |
| --- | --- | --- |
| `make ci` (build, build-32bit, vet, archcheck, apidiff-check, test incl. slogkeys, lint, cover-check, bluez-check) | `f6453faf`; re-run at the final HEAD, see below | exit 0 |
| `make race` ×2 | `4fa2ec7a` | exit 0, exit 0 |
| `make reachability-check` | `bb2fc0cc` (snapshot committed in it; unreachable 3, unchanged) | exit 0 after the commit |
| `make cite-check` (`MATTERJS_DIR` set) | `bb2fc0cc` (artefacts committed; drift unchanged) | exit 0 |
| `make testparity-check` | `f6453faf` | exit 0 |
| `go test ./script/...` | `f6453faf` | exit 0 |
| `mkdocs build --strict` | `f6453faf` | exit 0 |
| `make ci`, `make reachability-check` at the final code HEAD | `331f6986` | exit 0, exit 0 |

Race history, for the record: at `f6453faf`/`bb2fc0cc` four of five
`make race` runs failed under full-suite load, each in a different
test (a DATA RACE in `controller/client` — a sustained subscription's
`SubscriptionAlive` hook, `registry.go:568`, still running after its
test ended; `TestRegistryDecommission`; `node`'s
`TestLevelControlTransitionReachesHostAndSubscriber` and
`TestThermostatControllerWriteReachesOnChanged`, 10 s loopback waits);
every one passed in isolation (`-race -count=3/10`), and `main` passed
three full runs. `script/slogkeys` walked the tree twice (23 s under
race, beside the other packages); after it walks once (`4fa2ec7a`) two
consecutive full runs passed. The `controller/client` goroutine that
outlives its test looks pre-existing; listed under Questions.

## Differences

- `to` and `from` kept: every site is the two sides of a change (an
  address list, a version), none only the peer's address; the brief
  listed `to` among the address aliases.
- `address` kept where it is not the session peer's address (a
  multicast group, a BLE advertiser, a dial attempt beside the logger's
  `peer` in `client/peer`, the host's `DeviceAddress`); `node` kept in
  `controller/client`, where it is the registry's string id, not a node
  id.
- examples' `error` → `error_state`, not `err`: the value is an
  error-state id (`slog.Int`), not an error.
- `err` count: 231 scripted + 2 rewritten, not 233.
- `mrp.retransmits` also counts the node's client exchanges (the brief
  named `transport/mrp`; the retransmit sites are the bridge's
  `ackpump.go` and `client/exchange`, `transport/mrp` only computes the
  backoff).
- The `fabrictest` fake is a type (`fabrictest.NewMetrics`) set through
  `Topology.Configure`, not `Site.Metrics()`.
- Migration line spells the real type: `fixedlabeldef.Declare{Initial:
  fixedlabeldef.Patch{LabelList: &labels}}` (Initial is a Patch).

## Decisions

- The names are constants in `contract` (the bridge records them and
  cannot import `node`) and re-declared in `node`.
- `sessions.open` from an optional `Active() int` capability of the
  registry; no hook in `secure/operational` (security path).
- The gauges read their source through atomics or the manager in hand —
  never the bridge's lock — since the close hooks may run under it.
- `internal/serialqueue` (foundation layer in `layers.json`) holds the
  device queue, now shared by the node; `node/metrics/...` in the facade
  layer.
- `Device.FixedLabel()`/`UserLabel()` added so a Base cluster has the
  typed accessor every mounted cluster has.
- Bridged UserLabel lists persist only for endpoints with a stable key.
- Coverage floors: `internal/serialqueue` 97, `node/metrics/expvar` 100,
  `script/slogkeys` 0 (test only); `fabrictest` held at 85 with a test of
  its metrics fake.

## Left out

- `transport/btp/session.go:236` keeps its discard fallback (not
  `client/*`, outside the brief's list); one line to align it.
- Binding on the Base rule (`Simple & Client`).
- Per-fabric/per-peer attributes on metrics (doc comment says later).
- The area-key list is accepted as found (240 keys); only the aliases
  named above were merged.
- No chip-tool run: composition and FeatureMap unchanged.

## Questions

- Should `peer` (address) and the `client/peer` logger's `address`
  (an attempt's address) merge, e.g. `peer` + `attempt_address`?
- Should `transport/btp` follow the `slog.Default()` fallback too?
- The `controller/client` sustained-subscription goroutine that ran past
  its test under race (see Guards): a follow-up for the flake sweep?

## by_design candidates

- None new. Possible: BD-Matter-BaseOptionalByDeclaration — matter.js
  attaches FixedLabel/UserLabel with `.with(...)` on any endpoint
  (`base.element.ts:46-47`); the kit accepts them as declarations
  instead, the Go form of the same allowance.
