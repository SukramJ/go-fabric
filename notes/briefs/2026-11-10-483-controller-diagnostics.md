# Brief W13 — issue 483: the controller serves a diagnostics snapshot, a trace and mDNS findings

Branch `w13/483-controller-diagnostics` in `../go-fabric-w13`, base `main` after #426 merges (read HEAD
then). One PR, "Closes #483". Pins: matter.js **db021da0**, matterjs-server **0bd06f2** (local
`../matterjs-server`), connectedhomeip 6170af84 (not needed here). Read matter.js with
`git -C ../matter.js show db021da0:<path> | sed -n a,bp`. Read map (Sonnet, facts only, spot-checked
by Fable): `~/.cache/go-fabric-scratch/483/read-483.md` — every file:line below comes from it or from
Fable's own reads at 5c993e69; re-read each against the base before relying on it. Copy this brief to
`notes/briefs/2026-11-10-483-controller-diagnostics.md` as the first commit. Owner decisions
2026-11-10 (issue comment 6757735719) are binding: shared package `diag` with `node` aliases;
discovery through the snapshot only (no `Controller.Names()`); **one trace** incl. `ota` and `camera`.

## Better than, and the right cut (architecture standard)

- **matter.js** (db021da0) exposes the controller's state as live observable sets — `PeerSet.peers`
  (`packages/protocol/src/peer/PeerSet.ts:218`), `Peer.sessions` (`Peer.ts:259-265`),
  `SessionManager.sessions` (`SessionManager.ts:376`) — rendered by its `Diagnostic` presentation; a
  host builds its own view by subscribing. Ours is a consistent snapshot taken without holding
  `peer.Set`'s lock across the peers, plus a bounded trace with operator sentences, the same shapes
  the node already serves — one page model for both roles.
- **matterjs-server** (0bd06f2) serves the controller's client only Thread diagnostics
  (`get_thread_diagnostics`, `get_thread_border_routers`, `WebSocketControllerHandler.ts:144`);
  session loss reaches a client only as a `node_event` state change. Ours names the cause (lost
  session, failed probe, missed check-in, failed pairing step) in a trace a server forwards unchanged.
- **Before:** nothing controller-side; `diagevent.Record` is called only in `bridge/` (securechannel.go
  :392/:469/:600/:1165/:1401, conn.go:568); the controller has no metrics, no ring, no finding.
- **Cut:** the module owns the shapes (`diag`), the snapshot, the trace and the findings; go-fabric-server
  wires them into go-fabric-ui's `ControllerSource` and, if wanted, `/ws`; go-fabric-ui owns the page;
  loom and go-ha2fabric host device nodes and change nothing.

## §1 Package `diag` — the shapes both roles serve

New top-level package `diag` (stdlib + `diagevent` + `mdns` imports only; no `node`, no `controller`):

- Move from `node/inspect.go:58-113`: `SessionDiagnostics`, `SessionInfo`, `Occupancy`; from
  `node/diagnostics.go:10-22`: `Diagnostics{Capacity}`, `DefaultDiagnosticsCapacity`, `capacity()`
  (exported as `Capacity()` or kept unexported with a `diag`-internal helper both roles call — keep the
  method unexported and add `func (d Diagnostics) Effective() int`). `node` keeps every exported name
  as a type alias (`type SessionDiagnostics = diag.SessionDiagnostics`, …, `DefaultDiagnosticsCapacity =
  diag.DefaultDiagnosticsCapacity`) — **no break**; `node.DiagnosticEvent = diagevent.Event` stays
  (node/facade.go:63-64). `MDNSFinding = mdns.Finding` stays in `node`; the controller returns
  `[]mdns.Finding` directly.
- `SessionInfo` gains fields the controller fills and the node fills where it can: `CreatedAt time.Time`,
  `Resumed bool`, `Transport string` (`"udp"`, `"tcp"`, `"ble"`, "" unknown), `PeerLost bool`. Adding
  fields is apidiff-compatible; `LastActivity` stays zero for controller sessions (no exported
  any-direction timestamp, client/exchange/session.go:293 unexported — say so in the field doc).
- `SessionDiagnostics` gains `Peers []PeerInfo` — nil on the node, one per peer on the controller;
  the sessions stay flat in `Sessions` (one copy), a `PeerInfo` references its sessions by
  `SessionIDs []uint16`. `PeerInfo{FabricIndex uint8; NodeID uint64; Registered bool (a client.Node
  exists); State string (client.ConnectionState.String(): "connected", "disconnected", "reconnecting",
  "waiting-for-device-discovery" — take the exact strings from controller/client/config.go:181);
  LoadError string; TimeOffline time.Duration; OperationalAddress string; OperationalHost string;
  TCPUnsupported bool; Reachable bool (discovery.Service.Reachable, service.go:252); DiscoveredNames
  []string (the operational instance names the peer's Service holds — Service.Name() qname; empty when
  none); LastCheckInAt, NextExpectedCheckIn time.Time (ICD, zero when not a LIT peer; controller/client/icd.go:320/:350);
  SubscriptionActive bool (network.go:458); SessionIDs []uint16}`. `diag` must not import
  `controller/client` → `State` is a string.
- `Occupancy` on the controller: zero value with `Capacity 0` = "this role does not track id
  occupancy" (client/exchange has no table figure, manager.go; the node's is secure/operational
  manager.go:1570). Document on the field.

## §2 `Controller.SessionDiagnostics()`

`func (c *Controller) SessionDiagnostics() diag.SessionDiagnostics`: `AsOf` on `c.clock`; iterate
`c.peers.Peers()` (set.go:215, a clone) and per peer `Sessions()` (peer.go:476, clone), `Descriptor()`
(:462), `TimeOffline()` (connection.go:338), `Service()` (:483; may be nil — check) → `Reachable()`,
`Name()`; the `client.Node` for the peer from `c.reg`/`c.Nodes()` (controller.go:588) →
`ConnectionState()` (node.go:651), `LoadError()` (:608), `SubscriptionActive()`, the ICD accessors.
Per session: `ID`, `FabricIndex`, `PeerNodeID`, `NodeID()` as LocalNodeID, `Resumed`, `CreatedAt`,
`ActiveTimestamp` → LastPeerActivity + PeerIdle, `IsPeerLost`, `Transport().String()` (define the
string mapping where `Transport` lives if it has no String), `ReportedParameters()` → the three
intervals, subscriptions: count/IDs from `c.subs` (`interaction.Subscriptions`) **if** it lists per
session — read its API; if not, 0/nil and a sentence in the report. Never hold `c.peers`' lock across
peers (the node's rule, node/inspect.go:115-119); no call that blocks on the receive path.

## §3 The trace

- `diagevent`: add `KindReachability = "reachability"`, `KindICD = "icd"`, `KindOTA = "ota"`,
  `KindCamera = "camera"` with doc lines in the style of ring.go:37-43.
- `Config.Diagnostics diag.Diagnostics` on the controller (controller.go:83-208); the ring created in
  `New` as the node does (node/node.go:301-303: `NewRing`, `SetClock`); `Controller.DiagnosticEvents()
  []diag.Event` (`diag.Event = diagevent.Event` alias, newest first) and `OnDiagnosticEvent(fn)
  (unsubscribe func())` (inline, as node/diagnostics.go:40 documents). No Async variant (the controller
  has no ordered callback queue like `n.callbacks`; say so).
- Record sites (message = one operator sentence; Detail = identifiers as strings; severity as given):
  1. **pairing** — wrap `PairOptions.Progress` (controller.go:671, `PairingStep` :688, phases :692-695):
     `PairingStarted` of the first step → Info "Pairing started." {code_kind?, discriminator if known};
     every `PairingFailed` → Error "Pairing failed at step <Name>: <Err>." {step, err} (covers
     `TermsRequiredError`, attestation refusals, `ErrNoDevice`); `Pair` returning nil error → Info
     "Node <id> was paired." {node_id, fabric_index}. The host's Progress still runs after ours.
  2. **session** — per peer, `Peer.Observe` (observer.go:16-33): `SessionAdded` → Info "A secure session
     with node <id> was established." {node_id, fabric_index, session_id, transport, resumed};
     `SessionDeleted` → Warning when `IsPeerLost()` ("…was lost; the controller reconnects.") else Info
     ("…was closed."); `EstablishmentUnresponsive` → Warning "CASE with node <id> got no answer within
     the MRP budget." (fires once per exhausted budget — acceptance: one event, not one per
     retransmission). To observe every peer incl. future ones add **`peer.Context.PeerAdded func(*Peer)`**
     (set.go, called after a peer joins the set, outside the set's lock; doc it; the controller registers
     the observer there, controller.go:330-334 builds the Context). Connection-state transitions from
     `client.Hooks.ConnectionStateChanged` (config.go:100-161): the controller **chains** the host's
     `HooksFor` (controller.go:115-121; find where the registry gets it) with its own: Reconnecting /
     WaitingForDeviceDiscovery → Warning, Connected / Disconnected → Info, message naming the state.
  3. **reachability** — add **`peer.Observer.ReachabilityVerified func(reason ReachabilityReason, ok bool, address string)`**
     fired once at the end of a monitor run (monitor.go: where `all_probes_failed` :264 /
     `session_migrated` :247 / the current-address success are decided; `address` = the session's
     address after the run, "" when none). Record Info on success ("Node <id> answered a <reason>
     probe at <address>."), Warning on failure ("Node <id> answered no <reason> probe; the session is
     given up." / for address-change the message the code path implies). One event per run.
  4. **discovery** — `c.names.Watch([]string{"_matter._tcp.local."}, fn)` (watch.go:48; fn on the cache
     goroutine, must not call back into the cache → record only, the ring is lock-free for it): for an
     instance of **this** fabric (qname prefix = the compressed fabric id of `c.fabric`, hex upper, the
     operational instance format `<CFID>-<NODEID>`; look at `mdns.BuildOperationalService` for the
     format) → Info "Node <id>'s operational record appeared/disappeared." {qname, srv_target}. Border
     routers: `c.brs.OnChange` (brdiscovery/registry.go:246) → Info added/removed only (not updated)
     {ext_address, network_name, host_name}.
  5. **icd** — per registered node's ICD client: `OnCheckIn` → Info "Node <id> checked in (counter N)."
     {node_id, counter}; `OnCheckInMissed` → Warning "Node <id> missed its expected check-in." (icd.go:376,
     :392; find the accessor from `client.Node` to its `ICDClient` and register when the node is created —
     the same place the hooks chain).
  6. **ota** — chain `cfg.OTA.OnStatus` (controller/ota.go:51-60): Info "Node <id>'s software update is
     <status> (version V)." {node_id, status, version}; Warning for a failed status (map the
     `OTAUpdateStatus` values; read `mattercore.OTAUpdateStatus`).
  7. **camera** — `cameraPort.Refused` (serve.go:273-290): Warning for Offer/Answer, Info otherwise
     (mirror the log levels), "Node <id> refused a WebRTC <kind> for session S." {node_id, webrtc_session_id, kind}.
  No log key changes; the trace is beside the logs.

## §4 `Controller.DiagnoseMDNS() []mdns.Finding`

- When `cfg.Advertise != nil`: `mdns.Diagnose(c.adv.Active())` of the commissioner advertisement
  (mdns/diagnose.go:44; read what `Active()` returns for the controller's advertiser, serve.go:373).
- Browse-side finding, new code `no_operational_names` (Warning, `Service` empty): emitted when at least
  one registered node is in `WaitingForDeviceDiscovery` and none of the registered nodes' operational
  names is in `c.names.DiscoveredNames()` (names.go:254). No timer, no grace: the state is the signal.
  Message: "None of the N commissioned nodes has an operational record in the DNS-SD cache; the
  controller's browse gets no answer on this network." Add the code to `mdns/diagnose.go`'s code list
  with its doc (it is a `mdns.Finding`, so the UI renders it as the node's).
- No interface allow-list on the controller (no `Config.MDNS`; #450 is node-side) → `DiagnoseInterfaces`
  is not called; say so in the method doc. Stable sort errors first as node/mdns.go:118-135.

## §5 `contract.Metrics` on the controller

`Config.Metrics contract.Metrics` (nil records nothing); `sessions.open` gauge from the peer
observers (count across peers), `mrp.retransmits` by setting `exchange.Config.Retransmitted` where
`c.mgr` is created (manager.go:77-81; the node does it at node/client.go:95), `subscriptions.active`
if `c.subs` can count. New names in `contract/metrics.go` (:36-63 style, with docs): `pair.failures`
(counter), `case.unresponsive` (counter), `reachability.probes` (counter, with the verdict in no label —
keep it one counter) , `icd.checkins_missed` (counter). `node/metrics.go` re-exports only its own;
the controller gets `controller/metrics.go` with the same `count`/`gauge` helpers.

## §6 Tests

- `diag`: zero-value docs tested by compile; `node` aliases: `go vet` + apidiff.
- `controller/`: unit tests on the existing fakes (look at `controller/*_test.go` for how `New` runs
  with a fake listener / memory DB): snapshot shape (a registered node with one session → one PeerInfo
  referencing it; `Occupancy.Capacity == 0`), trace events for each record site that a unit test can
  reach (pairing failed step via a fake Progress path, connection state via the chained hook, OTA
  status via the chained hook, camera refused via `cameraPort.Refused`), `OnDiagnosticEvent` fires
  after the ring holds the event, newest-first order, `DiagnoseMDNS` with a WaitingForDeviceDiscovery
  node and an empty cache → `no_operational_names`.
- `client/peer`: `Context.PeerAdded` called once per new peer outside the lock; `Observer.ReachabilityVerified`
  once per run for both reasons (extend monitor_test.go / suspect_walk_test.go fixtures).
- `fabrictest` (in-process pair, as W11's leave test): commission, stop the device node → the
  controller's trace holds a `session` Warning (lost) for the peer and the state transition to
  Reconnecting; restart → Connected. No chip-tool run (no device-side or wire change; the Go-controller
  CI legs run as always) — say so.

## §7 by_design, CHANGELOG, docs

- `notes/parity/by_design.md`, register row (Fable text, verbatim, after the `BD-Matter-ProvenOperationalHost` row):
  `| BD-Matter-ControllerDiagnostics | `PeerSet.ts:218` at `db021da0` exposes the peers as a live set (`get peers()`) and `SessionManager.ts:376` the sessions (`get sessions()`); no snapshot, trace or finding type serves a host's diagnostics view (presence) | `packages/protocol/src/peer/PeerSet.ts` | `"    get peers() {"` | this module's node-side facade (`node.SessionDiagnostics`, `diagevent.Ring`, `mdns.Diagnose`) applied to the controller role; matterjs-server at `0bd06f2` serves only Thread diagnostics (`packages/ws-controller/src/server/WebSocketControllerHandler.ts:144`) | no |`
- Entry (Fable text, verbatim, after `BD-Matter-SuspectProbeWalksAlternates`):
  `### BD-Matter-ControllerDiagnostics — the controller serves a diagnostics snapshot, a trace and mDNS findings`
  "matter.js exposes the controller's state as live observable sets — `PeerSet.peers` (`packages/protocol/src/peer/PeerSet.ts:218` at db021da0), `Peer.sessions` (`Peer.ts:259-265`), `SessionManager.sessions` (`SessionManager.ts:376`) — rendered by its `Diagnostic` presentation; no snapshot, trace or finding type is served to a host. matterjs-server (`0bd06f2`) offers the controller's client only Thread diagnostics (`get_thread_diagnostics`, `get_thread_border_routers`; `packages/ws-controller/src/server/WebSocketControllerHandler.ts:144`) and session loss only as a node state event. This module serves the controller the facade the node has: `Controller.SessionDiagnostics()` (`diag.SessionDiagnostics`: the sessions flat, the peers with their state), `Controller.DiagnosticEvents()` and `OnDiagnosticEvent` on a `diagevent.Ring` (pairing, session, discovery, reachability, icd, ota, camera), `Controller.DiagnoseMDNS()` and `contract.Metrics`. Reason: a host running the controller half alone shows its operator the paired nodes but nothing about why one is reconnecting (issue 483); the shapes are shared with the node so one page model serves both roles. The row closes when matter.js serves a diagnostics snapshot or trace."
- `make ahead-check` must pass with the new row (the matcher string must occur at the cited line).
- CHANGELOG `[Unreleased]`: **Added** `diag` package; `Controller.SessionDiagnostics`, `DiagnosticEvents`,
  `OnDiagnosticEvent`, `DiagnoseMDNS`, `Config.Diagnostics`, `Config.Metrics`; `diagevent` kinds;
  `peer.Context.PeerAdded`, `peer.Observer.ReachabilityVerified`; `diag.SessionInfo` fields;
  `mdns` code `no_operational_names`; metric names. **Changed** `node`'s diagnostic types are aliases
  of `diag`'s (no source change for consumers).
- Docs: `docs/guides/observability.md` gets the controller section (snapshot, trace kinds with one
  example sentence each, findings, metrics); any controller guide listing the API gets the methods;
  mkdocs strict.

## Decisions (taken)
- One trace for everything the controller observes (owner); kinds as §3.
- No `Controller.Names()`; discovery visible through `PeerInfo.DiscoveredNames`, the `discovery` events
  and the `no_operational_names` finding (owner).
- `Occupancy` zero on the controller (no cheap figure; not tracked).
- Pairing recorded via the Progress chain, not via log keys (there are none, `matter.controller.commissioning.*` are the flow's).
- New seams in `client/peer` (`PeerAdded`, `ReachabilityVerified`) are additive; no change to the
  matter.js-mirrored behaviour; cite nothing from matter.js for them (this module's).

## Guards
`make ci` (`PATH=$HOME/go/bin:$PATH GOTMPDIR=$HOME/.cache/gotmp`), `go test -race -count=3` on
`./diag/... ./controller/... ./client/peer/... ./node/... ./fabrictest/...`, `make cite-check`,
`make reachability-check`, `make ahead-check`, `make apidiff-check` (aliases + added fields must be
compatible; a new package is fine), `make surface-check` (regenerate the surface snapshot if the
target exists — read the Makefile), `mkdocs build --strict` if docs changed (see the Makefile/pages
workflow for the command). No chip-tool run. Logs under `~/.cache/go-fabric-scratch/483/` with a summary
log (HEAD + one exit line per guard), under `flock ~/.cache/go-fabric-scratch/guards.lock` from a script
file. Author ident `SukramJ <sukramj@icloud.com>`; commit with
`git -c user.name=SukramJ -c user.email=sukramj@icloud.com`; no attribution lines; never `git add -A`;
no `time.Sleep`/`time.After` in production code. PR title "Wave W13: the controller serves a diagnostics
snapshot, a trace and mDNS findings (#483)", auto-merge off, report in the fixed shape (per §, guards
with HEAD, differences, decisions, left out, questions, by_design candidates).
