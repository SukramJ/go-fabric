# Gate 1 inventory (architecture audit) - go-fabric, branch m4/phase-20b (a71223c2)

Map only: file:line per fact, no judgement. Standards read: docs/adr/0025-facade-design-principles.md (12 principles), docs/adr/0026-architecture-principles.md (12 principles). Non-test code means *.go excluding *_test.go; script/ and examples/ are excluded unless a line says otherwise. Heuristics are named where used; anything not verified says so.

## 1 Import graph

`make archcheck` output (exit 0): `go test -count=1 ./script/archcheck/...` -> `ok  github.com/SukramJ/go-fabric/script/archcheck 3.771s`. With -v: TestLayering PASS, TestSpecificity PASS, TestCheckReportsUpwardStaleAndUnassigned PASS (Makefile:65-67). Layers and exceptions are data in script/archcheck/layers.json. Exceptions on the ratchet (layers.json:15-19): binding -> controller/exchange, controller/handshake, controller/interaction, controller/peer (reason: 'resolved at gate 1'); cluster/wire -> cluster/switch (deprecated NewGenericSwitch wrapper, 'may be removed in v0.10.0').

### 1a cluster/* -> im (non-test files; only packages that import `github.com/SukramJ/go-fabric/im`; no cluster package imports an im/* subpackage)

FILES importing im: 75; 25 cluster packages import im (alarm alarmbase appliance boolcfg closure core cover energy fan levelcontrol light lock media modebase modeselect onoff opstate pump servicearea spec spec/spectest switch thermo valve wire); the cluster packages that do not: cluster (root), filter, measurement, transition.
Identifiers used, by count (uses/files and first site, format at the end of the block):
- StatusInvalidCommand 121/41 cluster/servicearea/servicearea.go:535; StatusConstraintError 103/36 cluster/valve/valve_server.go:512; StatusCode 86/39 cluster/valve/valve_server.go:512
- StatusFailure 53/26 cluster/boolcfg/boolcfg.go:356; UnsupportedCommandf 40/37 cluster/valve/valve_server.go:347; StatusCodeError 39/25 cluster/valve/valve_server.go:515
- FabricFilterFromContext 30/16 cluster/core/general_commissioning.go:515; StatusInvalidInState 20/9 cluster/boolcfg/boolcfg.go:463; StatusResourceExhausted 17/9 cluster/core/groupcast.go:586
- StatusNotFound 16/7 cluster/core/groupcast.go:748; StatusUnsupportedAccess 15/11 cluster/core/groupcast.go:479; StatusSuccess 12/2 cluster/core/groups.go:291; LookupFabricFilter 9/7 cluster/core/operational_credentials.go:983
- SubjectFromContext 8/6 cluster/core/access_control_auxiliary.go:163; StatusUnsupportedAttribute 7/4 cluster/spec/write.go:61; IsPASEFromContext 5/5 cluster/core/access_control_auxiliary.go:160
- StatusFailsafeRequired 4/2 cluster/core/operational_credentials.go:721; StatusBusy 4/4 cluster/core/network_commissioning_wifi.go:276; MatterClusterStatusError 4/2 cluster/wire/admincommissioning.go:568
- StatusUnsupportedWrite 3/3 cluster/spec/write.go:64; StatusUnsupportedCommand 3/2 cluster/cover/windowcovering_server.go:545; AuthorityAt 2/2 cluster/core/groupcast.go:527
- GroupSubjectFromContext 2/2 cluster/core/commissioner_control.go:265; StatusInvalidAction 2/1 cluster/core/group_key_management.go:550; FieldsContainerConsumed 1/1 cluster/spec/codec.go:95
- DefaultMaxPathsPerInvoke 1/1 cluster/core/basic_information.go:276; StatusUnsupportedEndpoint 1/1 cluster/core/groupcast.go:640; StatusAlreadyExists 1/1 cluster/core/groupcast.go:743
- DeferAfterResponse 1/1 cluster/core/diagnostic_logs.go:559; IsListAppendWrite 1/1 cluster/core/access_control.go:855; WithFabricFilter 1/1 cluster/core/scenes_management.go:442
- StatusInvalidDataType 1/1 cluster/energy/evse.go:1328; CommandPathFromContext 1/1 cluster/thermo/atomic.go:350
(format: identifier uses/files first-site)
Per package (identifier set):
- cluster/alarm: S.Busy,S.Code,S.CodeError,UnsupportedCommandf | cluster/alarmbase: S.Code,S.Failure,S.InvalidCommand
- cluster/appliance: S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidInState,S.UnsupportedAttribute | cluster/boolcfg: S.Code,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidInState
- cluster/closure: S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidInState,S.UnsupportedCommand,UnsupportedCommandf
- cluster/core: AuthorityAt,DefaultMaxPathsPerInvoke,DeferAfterResponse,FabricFilterFromContext,GroupSubjectFromContext,IsListAppendWrite,IsPASEFromContext,LookupFabricFilter,S.AlreadyExists,S.Busy,S.Code,S.CodeError,S.ConstraintError,S.FailsafeRequired,S.Failure,S.InvalidAction,S.InvalidCommand,S.NotFound,S.ResourceExhausted,S.Success,S.UnsupportedAccess,S.UnsupportedEndpoint,S.UnsupportedWrite,SubjectFromContext,UnsupportedCommandf,WithFabricFilter
- cluster/cover: S.Busy,S.Code,S.CodeError,S.ConstraintError,S.Failure,S.UnsupportedCommand | cluster/energy: S.Code,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidDataType,S.InvalidInState,S.NotFound,S.ResourceExhausted
- cluster/fan: S.Code,S.CodeError,S.ConstraintError,S.InvalidCommand,S.InvalidInState,UnsupportedCommandf | cluster/levelcontrol: S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,UnsupportedCommandf
- cluster/light: S.Code,S.ConstraintError,S.InvalidCommand,UnsupportedCommandf
- cluster/lock: FabricFilterFromContext,MatterClusterStatusError,S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.UnsupportedAccess,SubjectFromContext | cluster/media: S.Code,S.CodeError,S.Failure,S.InvalidCommand
- cluster/modebase: S.Code,S.CodeError,S.Failure,S.InvalidCommand,UnsupportedCommandf | cluster/modeselect: S.Code,S.CodeError,S.InvalidCommand,UnsupportedCommandf | cluster/onoff: FabricFilterFromContext,S.Failure,S.InvalidCommand
- cluster/opstate: S.Code,S.CodeError,S.Failure,UnsupportedCommandf | cluster/pump: UnsupportedCommandf | cluster/servicearea: S.InvalidCommand,UnsupportedCommandf
- cluster/spec: FieldsContainerConsumed,S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.UnsupportedAttribute,S.UnsupportedWrite,UnsupportedCommandf | cluster/spec/spectest: S.CodeError,S.UnsupportedWrite
- cluster/switch: S.Code,S.ConstraintError
- cluster/thermo: AuthorityAt,CommandPathFromContext,GroupSubjectFromContext,LookupFabricFilter,S.Busy,S.Code,S.CodeError,S.ConstraintError,S.Failure,S.InvalidCommand,S.InvalidInState,S.NotFound,S.ResourceExhausted,S.UnsupportedAttribute,SubjectFromContext,UnsupportedCommandf
- cluster/valve: S.Code,S.CodeError,S.ConstraintError,UnsupportedCommandf | cluster/wire: FabricFilterFromContext,MatterClusterStatusError,S.Code,S.CodeError,S.Failure,S.InvalidCommand,UnsupportedCommandf
(S. abbreviates Status in the identifier names)

### 1b other named edges (identifier, uses, first site)

- cluster/core -> groups
  - import github.com/SukramJ/go-fabric/groups
    - AddEndpointForGroup x2 e.g. cluster/core/groupcast.go:700
    - ErrResourceExhausted x2 e.g. cluster/core/groupcast.go:727
    - Fabrics x1 e.g. cluster/core/group_key_management.go:433
    - GroupMessageEvent x1 e.g. cluster/core/groupcast.go:976
    - GroupTable x4 e.g. cluster/core/groupcast.go:575
    - GroupcastMembership x8 e.g. cluster/core/groupcast.go:411
    - GroupcastMemberships x7 e.g. cluster/core/groupcast.go:365
    - HasKeyMapping x1 e.g. cluster/core/groups.go:278
    - IANAGroupcastAddress x1 e.g. cluster/core/groupcast.go:987
    - Manager x6 e.g. cluster/core/groupcast.go:174
    - MaxGroupsPerFabric x2 e.g. cluster/core/groupcast.go:585
    - MulticastAddressFor x1 e.g. cluster/core/groupcast.go:995
    - OnGroupMessage x1 e.g. cluster/core/groupcast.go:953
    - OnGroupTableChanged x1 e.g. cluster/core/group_key_management.go:163
    - PolicyIanaAddr x3 e.g. cluster/core/groupcast.go:418
    - PolicyPerGroup x2 e.g. cluster/core/groupcast.go:416
    - Reload x1 e.g. cluster/core/group_key_management.go:177
    - RemoveEndpoint x4 e.g. cluster/core/groupcast.go:716
    - RemoveEndpointRetainingSender x2 e.g. cluster/core/groupcast.go:714
    - RemoveGroupProperties x1 e.g. cluster/core/groupcast.go:847
    - SetGroupProperties x4 e.g. cluster/core/groupcast.go:599
    - SetMaxGroupsPerFabric x1 e.g. cluster/core/group_key_management.go:162
- groups -> cluster/spec
  - import github.com/SukramJ/go-fabric/cluster/spec
    - Encodable x3 e.g. groups/request.go:44
    - ListElements x1 e.g. groups/request.go:110
    - Node x1 e.g. groups/request.go:115
- controller/client -> device
  - import github.com/SukramJ/go-fabric/device
    - Lookup x1 e.g. controller/client/endpoint.go:79
    - Type x1 e.g. controller/client/endpoint.go:78
- controller/client -> endpoint
  - import github.com/SukramJ/go-fabric/endpoint
    - ClusterShape x4 e.g. controller/client/conformance.go:177
    - DeviceTypeTree x1 e.g. controller/client/conformance.go:124
    - DeviceTypeViolation x2 e.g. controller/client/conformance.go:66
- binding -> controller
  - import github.com/SukramJ/go-fabric/controller/handshake
    - PeerAddress x2 e.g. binding/manager.go:85
  - import github.com/SukramJ/go-fabric/controller/interaction
    - Config x1 e.g. binding/manager.go:143
    - ForPeer x1 e.g. binding/resolve.go:110
    - Interaction x1 e.g. binding/manager.go:67
  - import github.com/SukramJ/go-fabric/controller/peer
    - ConnectOptions x1 e.g. binding/resolve.go:485
    - Observer x1 e.g. binding/resolve.go:492
    - Peer x8 e.g. binding/manager.go:64
  - import github.com/SukramJ/go-fabric/controller/exchange
    - Session x1 e.g. binding/resolve.go:492
  (binding rows: the identifiers `connect_failed`/`connecting` the regex also caught are text in log messages, dropped.)
Import sites: cluster/core imports groups at cluster/core/groupcast.go:18, groups.go:17, group_key_management.go:15; groups imports cluster/spec at groups/request.go:11; controller/client imports device at controller/client/endpoint.go:12 (alias fabricdevice) and endpoint at controller/client/conformance.go:12; binding imports controller/handshake, interaction, peer at binding/manager.go:15-17 and controller/exchange, handshake, interaction, peer at binding/resolve.go:13-16.

## 2 Goroutines and timers

Method: `grep -rn -E '(^\s*|[{;)] *)go (func|[A-Za-z0-9_.]+\()'` over non-test code (the briefed pattern lacks digits and so misses `go r.serveV4(`; this one adds mdns/subtype_responder.go:292,296, bletest.go:263 and the two `go` inside clock callbacks in cluster/lock), plus `.Go(func` (sync.WaitGroup.Go): 5 sites. 74 `go` statements + 5 `.Go` in library code (incl. test-support packages) + 7 in examples/. Columns: site | owner | how it stops | does the owner's Stop/Close wait (read at the cited line).

- bridge/bridge.go:820 | Bridge.Start, UDP serve loop | serveCtx cancel (Stop, bridge.go:1743) | yes: serveDone, waitServeStopped bridge.go:1809-1816, bounded by Stop's ctx
- bridge/bridge.go:836 | Bridge.Start, TCP serve | serveCtx | yes: tcpDone, bridge.go:1822-1827, bounded by ctx
- bridge/bridge.go:863 | Bridge.Start, ack pump | pumpCtx (pumpCancel, bridge.go:1746) | yes: pumpDone with own pumpStopGrace budget, bridge.go:1756-1771
- bridge/securechannel.go:1279 | Bridge.triggerSessionReannounce | context.WithTimeout(Background) at :1280 | no wait
- bridge/securechannel.go:1329 | closeSecureSessionsForShutdown, CloseAllGraceful | self-terminating, buffered chan | Stop waits via select with clock.After(stopSessionCloseBudget) :1335; the goroutine outlives a timeout
- bridge/health_probe.go:93 | StartHealthProbeWithClock | pctx cancel + done chan | yes: returned func does cancel(); <-done (:104-107)
- bridge/bdx_responder.go:108 | deliverRespondedBDX, one per BDX exchange | ctx parameter; ends when responder.HandleExchange returns | no wait visible
- bridge/pase_provider.go:169 (.Go) | PerExchangePaseProvider.StartReaper | ctx / Stop | yes: reaperWG.Wait in Stop (:183-196)
- bridge/case_provider.go:162 (.Go) | PerExchangeCaseProvider.StartReaper | ctx / StopReaper | yes: reaperWG.Wait
- bridge/subscription_reestablish.go:135 (.Go) | reestablish per peer, local WaitGroup | self-terminating | yes: wg.Wait() in the same function (:141)
- secure/operational/manager.go:1183 | Manager.StartReaper | ctx only | no handle, no Stop; Manager has no wait
- mdns/zeroconf.go:653 | Zeroconf.StartReannounceLoop | stopCh (returned cancel) or ctx | cancel is sync.OnceFunc(release; close(stopCh)) at :670-673 and does not wait
- mdns/subtype_responder.go:292,296 | SubtypeResponder.Start serveV4/V6 | cctx cancel in Close | yes: r.wg.Wait in Close (:323)
- mdns/operational_resolver.go:285,290 (.Go) | multicastQueryConn read4/read6 | socket close | yes: c.wg.Wait (:369)
- node/client.go:121 | clientStack Serve | ctx derived from n.life | yes: cs.close waits <-cs.served (client.go:144-145)
- node/ota.go:104 | otaApplier.ApplyUpdate -> restartedWhenIdle | context.WithoutCancel(ctx); 10 s clock deadline (ota.go:162) | no wait
- node/node.go:498 | Node.reopenWhenIdle | n.life, ticker | yes: keeperWG, Wait at node.go:757
- node/node.go:651 | Node.Start keepOperationalTime | n.life | yes: keeperWG (node.go:757)
- controller/client/reactions.go:55 | Registry.goWork | r.cancel | yes: r.work.Wait in Close (registry.go:405)
- controller/exchange/manager.go:513 | cleanupSessionExchanges `go lru.Close()` | self-terminating | no
- controller/exchange/manager.go:757 | Manager.dispatch, one per inbound exchange | handler returns | no WaitGroup; Manager.Close (:444-457) closes sessions only
- controller/interaction/sustained.go:209 | Interaction.sustain -> runSustained | sustained.finished closed at :248 | Subscription.Close waits finished in a further goroutine (subscriptions.go:369-371); no WaitGroup
- controller/interaction/subscriptions.go:369 | Subscription.Close waiter | waits finished | n/a (exits after finish())
- controller/dcl/revocation.go:259 | fetchAndValidateRevocation | self | yes: local wg.Wait :265
- controller/discovery/status.go:104 | Status.Connecting watcher | result chan or names.loop.done | no
- controller/discovery/status.go:167 | Service.maybeStartResolving | ctx cancel (resolveCancel) | done chan stored in status.resolving (:166); read at service.go:255
- controller/discovery/status.go:218 | Service.Changes watcher | ctx or loop.done | no
- controller/discovery/service.go:287 | Service.AddressChanges | ctx / loop closing | no
- controller/discovery/names.go:173 | Names.run loop | loop.closing | yes: Names.Close cancels, closes loop.closing, <-loop.done (names.go:213-217)
- controller/peer/peer.go:93 | Peer.goWork | peer ctx | yes: p.workers.Wait (peer.go:428)
- controller/peer/peer.go:219 | Peer.changedLocked save | workers | yes (peer.go:428)
- controller/peer/probe.go:100 | readEmpty final StatusResponse | context.WithoutCancel(ctx); self-terminating | no
- controller/peer/monitor.go:145 | monitor.verify run | done chan | yes: monitor.close waits <-r.done (monitor.go:124-135)
- controller/peer/connection.go:140 | initiateConnection `go c.kick` | self-terminating | no
- controller/peer/connection.go:171 | initiateConnection c.run | cancel / c.done | yes: p.workers (peer.go:428)
- controller/peer/connection.go:337 | connection.goWork | c.workers | yes: c.workers.Wait (connection.go:332)
- controller/commissioner/commissioner.go:330 | establishOverChannel watcher | ch.Closed() or s.Closing() | no
- controller/commissioner/connection.go:268 | connection.launch, one per candidate | actx/abortCtx | counter c.pending; drain loops wait for pending==0 (connection.go:163-185)
- controller/ble/scanner.go:373 | Scanner.transition | ctx cancel deferred (:371); buffered chan | no (returns on ScanTransitionTimeout while call may still run)
- controller/ble/channel.go:151 | Channel.watch | session.Closed / conn.Disconnected | no (Channel.Close :224-229 does not wait)
- ble/bluez/central_linux.go:115 | central.dispatch | c.done closed in Close (:119-139) | no wait
- ble/bluez/central_linux.go:698 | connection.deliver | ends when queue empty | no
- cluster/core/general_commissioning.go:648, :899 | watchFailSafeExpiry | request ctx or timer (:677-684) | no Close/Wait on GeneralCommissioning
- cluster/core/ota_software_update_requestor.go:843 | goRun | r.runCtx | yes: Stop does r.wg.Wait (:826-832)
- cluster/core/diagnostic_logs.go:547 | BDX log transfer | tctx = context.WithoutCancel(ctx) (:521) | no wait visible
- cluster/core/identify.go:264 | Identify countdown ticker | i.done, closed by Identify.Close (:141-142) | no wait
- cluster/core/commissioner_control.go:314 | requestCommissioningApproval -> approve | c.ctx | yes: c.wg.Wait in Close (:251-256)
- cluster/core/commissioner_control.go:332 | approve, Approver.Approve call | ctx with ApprovalTimeout; buffered chan | no
- cluster/core/commissioner_control.go:409 | commissionNode -> commission | c.ctx with CommissioningTimeout (:493) | yes: c.wg
- cluster/lock/users.go:362, cluster/lock/doorlock_server.go:970 | `go` inside a clock AfterFunc callback (runExpireUser, runAutoRelock) | timer Stop | no wait
- binding/manager.go:259 | Manager.run worker | m.life cancel | yes: Close waits m.wg (manager.go:529-534)
- binding/resolve.go:323 | establishClientKind connect | m.life | yes: m.wg
- im/subscription/engine.go:87 | tick report workers | m.wg | yes: Stop waits m.wg (manager.go:243-248)
- im/subscription/manager.go:238 | Manager.run | stopCh / ctx | yes: Stop waits m.wg
- transport/tcp/conn.go:194 | Conn.serve, one per message in an ordered chain | handler returns; skips when closed | no WaitGroup in package transport/tcp; Conn.Close (:137-147) only closes
- transport/tcp/dialer.go:49 | Dialer.Dial c.serve read loop | conn close | no
- transport/tcp/listener.go:156 | Listener.Serve ctx watcher | done chan, deferred close (:155) | exits with Serve
- transport/tcp/listener.go:213 | Listener.accept closed-watcher | <-c.Closed() | no
- transport/tcp/listener.go:223 | Listener.accept c.serve | conn close | no; Listener.Close (:228-245) closes conns without waiting
- transport/udp/listener.go:176 | Listener.Serve ctx watcher | done chan | exits with Serve
- transport/udp/listener.go:225 | Listener.Serve, one per datagram | inflight WaitGroup | Listener.Close (:306-315) does not wait; WaitIdle(ctx) does (:270-281), called from bridge.go:1818
- transport/udp/listener.go:272 | WaitIdle waiter | inflight.Wait | stays blocked until inflight drains even when ctx expired first
- transport/udp/ordered.go:90 | orderedDispatch.enqueue -> drain | queue empty | tracked by l.inflight (ordered.go:86)
- transport/udp/ordered.go:113 | drain, per datagram | l.inflight | WaitIdle
Test-support packages (same grep): fabrictest/site.go:280 (Site.Run runs fn; Run returns on ctx error without waiting `finished`, :285-287), fabrictest/controller.go:150 (Controller.Close waits <-served, :263), bridge/bridgetest/conn.go:64 (wg, ConnServer.Wait :84), bridge/bridgetest/commissioner.go:225 (waits <-c.served :293), clock/clocktest/fake.go:216 (Advance waits <-done :220), :249 (Resolve), controller/ble/bletest/bletest.go:162 (pipe, stop chan), :263, :275, :300, :343 (`go c.drop()`/`go s.Close()`, no wait).
examples/ (7, not audited): quickstart-controller/main.go:133, reference-bridge/control.go:88,99, ota.go:101, fleet_appliances.go:250, fleet_closure.go:104, reference-controller/cli/stack.go:172.

### 2b timers and tickers outside the clock port
- Non-test code outside clock/, examples/, script/ calls raw `time.Now/Sleep/After/Tick/NewTimer/NewTicker/AfterFunc/Since/Until` only at mdns/subtype_responder.go:358 and :384 (`SetReadDeadline(time.Now().Add(time.Second))`); the guard clock/noraw_test.go lists it as the one exception (rawTimeExceptions, 2 uses, reason: kernel judges socket deadlines against the wall clock). No raw time.AfterFunc/NewTimer/NewTicker/After/Tick/Sleep in library code. Mentions in comments only: transport/btp/session.go:47,69, transport/mrp/retransmit.go:141, secure/mattercert/verify.go:45, store/diagnostics.go:28, cluster/core/groupcast.go:185, cluster/core/time_synchronization.go:91, cluster/onoff/server.go:95, cluster/transition/transition.go:48.
- clock/clock.go:74,78,82,85 and clock/clocktest/fake.go:238 (`time.Sleep(time.Millisecond)`, 'the one real pause') are the port itself.
- Real-time `context.WithTimeout/WithDeadline` in non-test code (not a clock-port wait): bridge/bridge.go 6, ble/bluez/central_linux.go 5, bridge/subscription_persistence.go 4, fabrictest/device.go 2, fabrictest/controller.go 2, cluster/core/commissioner_control.go 2, bridge/ota_provider_link.go 2, bridge/bridgetest/commissioner.go 2, 1 each in node/node.go, internal/chipdm/source.go, controller/dcl/{revocation,ota,github,client}.go, controller/commissioner/commissioner.go, bridge/{subscription_reestablish,securechannel,group_messaging}.go, bridge/bridgetest/session.go, bdx/messenger.go (total 37).
- examples/ and script/ use raw time freely (examples/reference-bridge: fleet.go 7, fleet_energy_market.go 8, ota.go 4, fleet_closure.go 3, fleet_appliances.go 2, fleet_evse.go 2, 1 each in fleet_energy/esa/lighting/safety/sensors/wifisim; script/dclsnapshot/main.go:37).

## 3 Locks

Method: struct-field scan of `sync.Mutex`/`sync.RWMutex` (type, file:line); 'comment' = a // line directly above the field or a trailing // on it (a comment further up the struct, or on the type, does not count). Total 199 mutex declarations: 194 struct fields found by the scan (33 of them RWMutex), plus 5 the scan missed (bootid/bootid.go:34 rotationGuard, no comment; contract/matter.go:375 measurementKindsMu, package var in a `var (` block whose comment says it is guarded because registration may come from several host init functions; bridge/bridgetest/session.go:127 and bridge/subscription_reestablish.go:130 and controller/client/interact.go:74, function-local, no comment). With a naming comment: 49 (+1 contract/matter.go:375); no comment: 145 (+4). Types with 2 or more mutex fields: 26.

### 3a mutex with a comment (quoted, cut at 80 chars; some comments name the field the mutex guards rather than the mutex itself)
- binding/manager.go:215 Manager.mu (Mu): "mu guards the queue, the handlers, the attachments, the registered group entries..."
- binding/manager.go:236 Manager.stateMu (Mu): "state is the worker's; stateMu lets Established read it."
- bridge/bridge.go:330 Bridge.groupRx (Mu): "groupRx serialises the group-message receive path: decode, replay check, dispatc..."
- bridge/bridge.go:398 Bridge.groupNetMu (Mu): "groupNetMu serialises the multicast-membership reconciler over groupMember (the ..."
- bridge/bridge.go:669 Bridge.routeBindMu (Mu): "routeBindMu serialises the session-route bindings with the per-connection sessio..."
- bridge/handlers.go:82 PaseAdapter.mu (Mu): "mu serialises all PASE-state transitions (PBKDF capture → Pake1 verifier allocat..."
- bridge/subscription_persistence.go:66 resumptionState.mu (Mu): "mu guards every field below and serialises store writes, so a subscription's sav..."
- clock/clocktest/fake.go:32 Fake.advancing (Mu): "advancing serialises Advance (and Resolve's steps): two goroutines moving one cl..."
- cluster/alarmbase/alarmbase.go:199 Server.op (Mu): "op serialises the read-modify-write of Mask and State and the Notify that report..."
- cluster/appliance/microwave.go:132 MicrowaveServer.op (Mu): "op serialises a command's checks and its CookTime update."
- cluster/core/binding.go:37 Binding.writeMu (Mu): "writeMu serialises the list's changes with their persistence, so the saves land ..."
- cluster/core/descriptor.go:36 Descriptor.mu (RW): "mu guards the fields the topology assembler installs after the endpoint is alrea..."
- cluster/core/descriptor.go:49 Descriptor.versionMu (Mu): "version is the cluster's DataVersion, bumped whenever an observed ServerList / P..."
- cluster/core/general_diagnostics.go:132 GeneralDiagnostics.opMu (Mu): "opSession is the operational time this process counted up to the checkpoint opLa..."
- cluster/core/groupcast.go:220 Groupcast.cmdMu (Mu): "cmdMu serialises the commands: each one reads Membership, checks it and then cha..."
- cluster/core/identify.go:78 Identify.timerMu (Mu): "Countdown-Timer state. Spec §1.2.5.1: "The IdentifyTime attribute … SHALL decrem..."
- cluster/cover/windowcovering_server.go:187 WindowCoveringServer.op (Mu): "op serialises the commands while a Mover runs, so a command restoring its target..."
- cluster/energy/commodityprice.go:94 CommodityPriceServer.mu (Mu): "mu guards the full prices — the attributes are served without their descriptions..."
- cluster/energy/devicemanagement.go:234 DeviceEnergyManagementServer.mu (Mu): "mu serialises the delegate state, as chip's single-threaded stack does; it is he..."
- cluster/energy/evse.go:192 EvseServer.mu (Mu): "mu serialises the state machine, as chip's single-threaded stack does. pending h..."
- cluster/energy/waterheater.go:121 WaterHeaterManagementServer.mu (Mu): "mu serialises the BoostState transitions and their events; it is never held acro..."
- cluster/levelcontrol/levelcontrol_server.go:381 Server.cmd (Mu): "cmd serialises the commands the engine runs."
- cluster/levelcontrol/native.go:102 NativeSource.wmu (Mu): "wmu serialises the attribute writes, so the values handed to Persist are the one..."
- cluster/light/colorcontrol_server.go:261 ColorControlServer.cmd (Mu): "cmd serialises the commands."
- cluster/light/colorcontrol_server.go:264 ColorControlServer.commit (Mu): "commit serialises the changes of the colour state, which reach it from commands ..."
- cluster/light/colorcontrol_server.go:272 ColorControlServer.mu (Mu): "mu guards everything below: commands, the transition's steps and subscription re..."
- cluster/onoff/server.go:195 Server.op (Mu): "op is the queue: commands, writes, timer ticks, scene applies and the timers' ru..."
- cluster/onoff/server.go:199 Server.mu (Mu): "mu guards the stored state's read-modify-write ([Server.load], [Server.commit], ..."
- cluster/onoff/server.go:204 Server.scenesMu (RW): "scenes is the ScenesManagement port, nil without one. It has its own lock so [Se..."
- cluster/thermo/thermostat_server.go:276 ThermostatServer.op (Mu): "op serialises the controller's changes (writes, commands): the queue, held acros..."
- cluster/transition/transition.go:178 Engine.op (Mu): "op serialises the operations, callbacks included."
- cluster/transition/transition.go:181 Engine.mu (Mu): "mu guards everything below; it is never held across a callback."
- controller/client/registry.go:102 Registry.op (Mu): "op is matter.js Peers.#mutex (Peers.ts:91): it serializes the registration of a ..."
- controller/commissioner/flow.go:132 flow.mu (Mu): "mu guards what the BLE re-arm timer shares with the steps: the failsafe end, the..."
- controller/dcl/certstore.go:123 CertificateStore.fetchMu (Mu): "the in-flight update (#fetchPromise, :300)"
- endpoint/types.go:244 Endpoint.stateMu (Mu): "state holds everything a BRIDGED endpoint must keep BETWEEN dispatches: the per-..."
- fabrictest/controller.go:98 Controller.mu (RW): "mu guards the stack's fields against [Controller.Restart] for the site's resolve..."
- fabrictest/device.go:86 Device.actMu (Mu): "actMu serialises Act with Start, Stop and Restart."
- groups/manager.go:129 Manager.receptionMu (Mu): "reception is matter.js MessagingState: the replay-protection window per operatio..."
- groups/manager.go:139 Manager.listenersMu (Mu): "messageListeners receive the outcome of every group message (matter.js SessionMa..."
- groups/manager.go:145 Manager.counterMu (Mu): "counter is the node-global group data message counter (counter.go), created on t..."
- groups/manager.go:152 Manager.senderMu (Mu): "The send side (sender.go): the outbound group sessions, the socket they leave th..."
- mdns/subtype_responder.go:74 SubtypeResponder.lifecycleMu (Mu): "lifecycleMu serialises Start and Close and guards cancel/closed plus the pc4 / p..."
- node/aggregator.go:68 Aggregator.reassembleMu (Mu): "reassembleMu serialises reassembly: each snapshot is taken after the previous to..."
- node/client.go:220 bindingSync.persistMu (Mu): "persistMu guards persisted: the Binding servers whose list the store keeps (pers..."
- node/node.go:77 Node.healthMu (Mu): "health is the probe Start began (Config.Health); nil when none runs (health.go)."
- secure/channel/session.go:82 Session.privacyMu (Mu): "privacyMu guards lazy derivation of the privacy keys. The keys are derived on fi..."
- secure/operational/manager.go:335 Entry.mu (Mu): "mu guards the mutable fields above for concurrent Rx/Tx updates."
- secure/sigma/protocol.go:186 Responder.mu (Mu): "mu serialises every state-mutating method on Responder. Apple iOS sends Sigma1 a..."

### 3b mutex with no comment (file: line Type.field)
- bdx/responder.go: 76 Responder.mu; 354 ServerSession.mu | bdx/sink.go: 37 FileSink.mu | ble/bluez/central_linux.go: 77 central.mu; 550 connection.mu | bridge/bdx_responder.go: 33 bdxRouting.mu(RW); 134 responderTurn.mu | bridge/bridge.go: 355 Bridge.mu(RW)
- bridge/bridgetest/commissioner.go: 150 Commissioner.mu | bridge/bridgetest/conn.go: 28 ConnServer.mu | bridge/bridgetest/seed.go: 26 seededStream.mu | bridge/case_provider.go: 42 PerExchangeCaseProvider.mu
- bridge/commissioning_window.go: 109 CommissioningWindow.mu(RW); 804 CommissioningWindowOpener.mu | bridge/handlers.go: 558 CaseAdapter.mu(RW) | bridge/outbound_reliable.go: 52 outboundReliableTracker.mu
- bridge/pase_provider.go: 32 PerExchangePaseProvider.mu | bridge/session_miss_burst.go: 26 sessionMissBurst.mu | bridge/subscribe.go: 1571 booleanStateEvents.mu; 1622 pathValues.mu | bridge/subscription_reestablish.go: 30 peerBlockList.mu
- clock/clocktest/fake.go: 34 Fake.mu | cluster/alarm/smokecoalarm_server.go: 333 Server.mu | cluster/alarmbase/alarmbase.go: 200 Server.mu | cluster/attribute_changes.go: 20 AttributeChanges.mu; 105 Quieter.mu | cluster/boolcfg/boolcfg.go: 156 Server.mu
- cluster/closure/closurecontrol_server.go: 127 ControlServer.mu(RW) | cluster/closure/closuredimension_server.go: 183 DimensionServer.mu | cluster/core/access_control.go: 41 AccessControl.mu(RW) | cluster/core/actions.go: 112 Actions.mu
- cluster/core/basic_information.go: 41 BasicInformation.mu(RW) | cluster/core/binding.go: 38 Binding.mu(RW); 42 Binding.obsMu | cluster/core/bridged_device_basic_information.go: 37 BridgedDeviceBasicInformation.mu(RW)
- cluster/core/commissioner_control.go: 186 CommissionerControl.mu | cluster/core/diagnostic_logs.go: 40 DiagnosticLogs.mu(RW) | cluster/core/general_commissioning.go: 41 GeneralCommissioning.mu(RW)
- cluster/core/general_diagnostics.go: 80 GeneralDiagnostics.mu(RW) | cluster/core/group_key_management.go: 37 GroupKeyManagement.mu(RW) | cluster/core/groupcast.go: 222 Groupcast.mu(RW) | cluster/core/messages.go: 90 Messages.mu
- cluster/core/network_commissioning.go: 35 NetworkCommissioning.mu(RW) | cluster/core/network_commissioning_wifi.go: 38 WiFiNetworkCommissioning.mu | cluster/core/operational_credentials.go: 60 OperationalCredentials.mu(RW)
- cluster/core/ota_provider.go: 48 OTAProvider.mu | cluster/core/ota_software_update_requestor.go: 66 OTASoftwareUpdateRequestor.mu | cluster/core/power_source_configuration.go: 38 PowerSourceConfiguration.mu(RW)
- cluster/core/scenes_management.go: 112 ScenesState.mu | cluster/core/vid_verification.go: 59 vidVerificationMemory.mu | cluster/core/wifi_network_diagnostics.go: 109 WiFiNetworkDiagnostics.mu
- cluster/cover/windowcovering_server.go: 188 WindowCoveringServer.mu(RW) | cluster/energy/commoditymetering.go: 65 CommodityMeteringServer.mu | cluster/energy/commoditytariff.go: 215 CommodityTariffServer.mu
- cluster/energy/gridconditions.go: 57 GridConditionsServer.mu | cluster/filter/filter.go: 155 Server.mu | cluster/levelcontrol/levelcontrol_server.go: 390 Server.mu | cluster/levelcontrol/native.go: 104 NativeSource.mu
- cluster/lock/doorlock_server.go: 329 DoorLockServer.op; 331 DoorLockServer.mu | cluster/measurement/soil.go: 69 SoilMeasurementServer.mu | cluster/media/media.go: 304 stateful.mu; 373 events.epMu | cluster/modebase/modebase_server.go: 445 Server.mu
- cluster/opstate/opstate_server.go: 443 Server.mu | cluster/pump/pump_server.go: 289 Server.mu | cluster/servicearea/servicearea.go: 199 Server.mu | cluster/spec/server.go: 127 Server.mu | cluster/switch/server.go: 156 Server.mu
- cluster/thermo/atomic.go: 56 atomicWriteState.mu; 142 atomicWrites.mu | cluster/thermo/temperaturecontrol.go: 129 TemperatureControlServer.mu | cluster/thermo/thermostat_server.go: 277 ThermostatServer.mu(RW)
- cluster/wire/admincommissioning.go: 174 AdministratorCommissioning.mu(RW); 186 AdministratorCommissioning.versionMu | controller/ble/bletest/bletest.go: 30 Central.mu; 193 connection.mu | controller/ble/channel.go: 64 handshakeState.mu
- controller/ble/listening.go: 22 ListeningMeter.mu | controller/ble/scanner.go: 131 Scanner.mu; 145 scanQueue.mu | controller/ca/ca.go: 103 MemoryStore.mu; 243 CA.mu | controller/client/cache.go: 141 persister.mu | controller/client/node.go: 33 Node.mu(RW)
- controller/client/registry.go: 104 Registry.mu; 247 nodeHolder.mu | controller/client/store.go: 56 MemoryStore.mu | controller/commissioner/connection.go: 92 connection.mu | controller/dcl/certstore.go: 119 CertificateStore.mu
- controller/dcl/github.go: 93 githubDir.mu | controller/dcl/revocation.go: 101 revocationCache.mu | controller/dcl/store.go: 29 MemoryStore.mu | controller/discovery/scanner.go: 363 deliveryQueue.mu | controller/exchange/exchange.go: 166 Exchange.mu
- controller/exchange/manager.go: 100 Manager.mu | controller/exchange/session.go: 52 Session.mu | controller/handshake/resumption.go: 68 MemoryResumptionStore.mu | controller/handshake/server.go: 99 CASEServer.mu
- controller/interaction/subscriptions.go: 40 Subscriptions.mu; 282 Subscription.mu | controller/interaction/sustained.go: 80 sustainedState.mu | controller/peer/connection.go: 119 connection.mu | controller/peer/monitor.go: 47 monitor.mu
- controller/peer/peer.go: 39 Peer.mu | controller/peer/set.go: 89 Set.mu | controller/peer/store.go: 30 MemoryStore.mu | device/device.go: 61 Device.mu | diagevent/ring.go: 76 Ring.mu(RW) | endpoint/devicetype_validation.go: 393 DeviceTypeValidator.mu
- endpoint/types.go: 323 endpointState.mu; 454 endpointStateRegistry.mu | fabrictest/device.go: 88 Device.mu | fabrictest/net.go: 65 Network.mu | fabrictest/site.go: 75 Site.mu | groups/counter.go: 69 dataCounter.mu
- groups/manager.go: 121 Manager.mu(RW); 132 Manager.observersMu | im/after_response.go: 19 AfterResponse.mu | im/eventlog.go: 125 EventLog.mu(RW) | im/subscription/manager.go: 96 Manager.mu(RW) | im/subscription/subscription.go: 67 Subscription.mu
- mdns/advertiser.go: 44 Noop.mu(RW) | mdns/host_responder.go: 75 hostAnswerer.mu | mdns/service.go: 85 hostNameChooser.mu | mdns/subtype_responder.go: 77 SubtypeResponder.mu(RW) | mdns/zeroconf.go: 66 Zeroconf.mu(RW) | node/aggregator.go: 58 Aggregator.mu
- node/client.go: 214 bindingSync.mu | node/commissioning.go: 52 commissioningHost.installMu; 57 commissioningHost.basicMu; 60 commissioningHost.announcedMu | node/health.go: 47 healthProbe.mu | node/node.go: 107 Node.mu
- node/security.go: 66 caseIdentities.mu(RW) | secure/operational/manager.go: 40 Manager.mu(RW) | secure/sigma/initiator.go: 42 Initiator.mu | transport/btp/session.go: 124 Session.mu | transport/mrp/ack.go: 128 AckTracker.mu
- transport/mrp/retransmit.go: 111 Retransmitter.mu | transport/mrp/window.go: 47 Window.mu | transport/tcp/conn.go: 48 Conn.writeMu | transport/tcp/listener.go: 72 Listener.mu | transport/udp/listener.go: 97 Listener.mu
- transport/udp/ordered.go: 55 orderedDispatch.mu
- plus bootid/bootid.go:34 BootID.rotationGuard; bridge/bridgetest/session.go:127; bridge/subscription_reestablish.go:130; controller/client/interact.go:74

### 3c lock held across a host callback, `contract` method, interface/port call, or I/O
Method: AST walk (go/parser, no type info) of every function, tracking `X.Lock()/RLock()` until `X.Unlock()/RUnlock()` in straight-line order (`defer X.Unlock()` = held to function end; branches inspected with a copy of the held set; closures analysed with an empty set). Flagged call kinds: [funcvalue] call of a local/param function value, [funcfield] call of a func-typed struct field, [ifacefield] method call on a struct field whose type is an interface declared in the module (clock/clk/Clock excluded), [contract] any method named Matter*, [io-pkg] exported function of os/net/sql/http, [io?] method named Send/Write/Publish/Persist/Save/Put/Invoke/Subscribe/Connect/... . Limits: callbacks reached through a helper called under the lock are not seen; local closures (e.g. `fail`, `clampCool`, `visit`) and pure calls (net.IPv4/ParseIP, atomic Load, timer Stop/Reset, `Now` fields) are in the list as flagged, not verified as host code. Format: file | call-lines@lock-expression@L<line of the Lock()>: callee [kind].
- bdx/sink.go | 55@s.mu@L49: os.CreateTemp [io-pkg]; 61@s.mu@L49: s.tmp.Write [io?]; 86@s.mu@L75: os.CreateTemp [io-pkg]; 95,100,104+1@s.mu@L75: os.Remove [io-pkg]; 107@s.mu@L75: os.Rename [io-pkg]
- bridge/bridge.go | 1637@b.mu@L1634: s.MatterClusterID [contract]; 1679@b.mu@L1676: s.MatterClusterID [contract]
- bridge/case_provider.go | 85@p.mu@L76: p.factory [funcfield]
- bridge/commissioning_window.go | 421@w.mu@L414: w.closeTimer.Stop [ifacefield]
- bridge/group_messaging.go | 232@b.groupNetMu@L229: b.groupJoinRetry.Stop [ifacefield]; 237@b.groupNetMu@L229: b.groupMember.LeaveGroup [ifacefield]
- bridge/handlers.go | 330@a.mu@L305: source [funcvalue]; 393@a.mu@L384: a.verifierFactory [funcfield]
- bridge/outbound_reliable.go | 340@t.mu@L322: send [funcvalue]
- bridge/pase_provider.go | 91@p.mu@L82: p.factory [funcfield]
- cluster/appliance/microwave.go | 320@s.op@L317: s.oven.OperationalState [ifacefield]; 324@s.op@L317: s.oven.StartSupported [ifacefield]; 329@s.op@L317: s.oven.NormalMode [ifacefield]; 337@s.op@L317: s.oven.Supp
- cluster/closure/closuredimension_server.go | 647@s.mu@L631: s.cfg.Now [funcfield]
- cluster/core/binding.go | 161@b.writeMu@L157: cfg.Load [funcfield]
- cluster/core/commissioner_control.go | 354@c.mu@L352: c.now [funcfield]; 431@c.mu@L420: c.now [funcfield]
- cluster/core/general_commissioning.go | 525@g.mu@L501: g.isCommissioningWindowOpen [funcfield]
- cluster/core/groupcast.go | 915@g.mu@L913: g.stopTesting [funcfield]; 925@g.mu@L913: g.timer [funcfield]
- cluster/core/identify.go | 256@i.timerMu@L255: i.identifyTime.Load [funcfield]
- cluster/core/messages.go | 255@m.mu@L249: m.presenter.PresentMessage [ifacefield]; 277@m.mu@L269: m.presenter.CancelMessage [ifacefield]
- cluster/core/ota_provider.go | 541@p.mu@L537: off [funcvalue]; 672,677@p.mu@L655: check [funcvalue]
- cluster/cover/windowcovering_server.go | 647@s.op@L623: s.mover.Stop [ifacefield]; 649@s.op@L623: s.mover.GoTo [ifacefield]; 724@s.mu@L711: store [funcvalue]; 808@s.mu@L794: m.timer.Reset [ifacefield]
- cluster/energy/devicemanagement.go | 663@s.mu@L653: s.manager.PowerAdjustCompleted [ifacefield]; 743@s.mu@L711: s.manager.StartTimeAdjust [ifacefield]; 809@s.mu@L771: s.manager.Pause [ifacefield]; 848@s.mu@L839
- cluster/levelcontrol/native.go | 517@n.mu@L512: p.deadline.Stop [ifacefield]; 535@n.mu@L532: n.off.deadline.Stop [ifacefield]
- cluster/light/colorcontrol_modes.go | 41@s.commit@L36,s.mu@L38: change [funcvalue]
- cluster/light/colorcontrol_transitions.go | 161@s.commit@L156,s.mu@L158: change [funcvalue]
- cluster/lock/doorlock_server.go | 1325@s.mu@L1323: fn [funcvalue]
- cluster/lock/schedules.go | 136@s.op@L132,s.mu@L134: fn [funcvalue]
- cluster/measurement/soil.go | 115@s.mu@L113: s.src.MatterFloatValue [contract]
- cluster/media/media.go | 357@s.mu@L354: fn [funcvalue]
- cluster/onoff/server.go | 365@s.op@L362: fn [funcvalue]; 721@s.op@L719: s.srv.MatterWrite [contract]; 830@s.op@L825: tick [funcvalue]
- cluster/spec/server.go | 466@s.mu@L464: s.Instance.MatterAcceptedCommands [contract]; 479@s.mu@L476: s.Instance.MatterGeneratedCommands [contract]; 482@s.mu@L476: s.Instance.MatterAcceptedCommands [contract]
- cluster/switch/server.go | 779@s.mu@L773: fn [funcvalue]
- cluster/thermo/atomic.go | 103@a.mu@L96: a.timer.Stop [ifacefield]
- cluster/thermo/temperaturecontrol.go | 352@s.mu@L351: change [funcvalue]
- cluster/thermo/thermostat_server.go | 1092,1107,1126@s.op@L1040: clampCool [funcvalue]; 1094,1109,1112@s.op@L1040: clampHeat [funcvalue]
- cluster/transition/transition.go | 239@e.op@L228: e.cfg.Apply [funcfield]
- controller/ble/bletest/bletest.go | 281@c.mu@L253: c.p.Serve [funcfield]
- controller/ca/ca.go | 646@c.mu@L616: c.cfg.Store.Save [ifacefield]
- controller/client/conformance.go | 154@e.mu@L132: visit [funcvalue]
- controller/client/node.go | 436@n.mu@L424: n.store.EraseNode [ifacefield]
- controller/client/reactions.go | 134@r.op@L103: h [funcvalue]
- controller/client/record.go | 704@n.mu@L702: f [funcvalue]
- controller/client/registry.go | 280@r.mu@L278: old [funcvalue]
- controller/peer/set.go | 204@s.mu@L196: s.ctx.Fabrics [funcfield]
- fabrictest/device.go | 179@d.actMu@L144: d.topo.Prepare [funcfield]; 190@d.actMu@L144: net.ResolveUDPAddr [io-pkg]; 372@d.actMu@L366: fn [funcvalue]
- fabrictest/multicast.go | 200@n.mu@L196: net.IP [io-pkg]
- fabrictest/net.go | 161@n.mu@L160: n.filter [funcfield]
- groups/groupcast.go | 180@m.mu@L159: m.st.UpsertGroupcastGroup [ifacefield]; 213@m.mu@L202: m.st.RemoveGroupcastGroup [ifacefield]
- groups/manager.go | 595@m.mu@L575: m.st.UpsertGroupTableEntry [ifacefield]; 663@m.mu@L637: m.st.RemoveGroupTableEntry [ifacefield]; 677@m.mu@L637: m.st.RemoveGroupcastGroup [ifacefield]; 685@m.mu@L637: m.st.Ups
- im/eventlog.go | 183@l.mu@L174: l.persistFn [funcfield]; 211@l.mu@L207: l.persistFn [funcfield]
- mdns/subtype_responder.go | 216@r.lifecycleMu@L210: net.IPv4 [io-pkg]; 219@r.lifecycleMu@L210: net.ParseIP [io-pkg]
- node/client.go | 287@n.bindSync.persistMu@L273: b.Persist [io?]
- node/node.go | 755@n.mu@L720: n.advertiser.Close [ifacefield]
- secure/sigma/initiator.go | 266,271,275+15@i.mu@L256: fail [funcvalue]; 300@i.mu@L256: i.verifier.VerifyAndExtractPubKey [ifacefield]
- secure/sigma/protocol.go | 496@r.mu@L472: r.sessionIDRenewer [funcfield]
- transport/tcp/conn.go | 98@c.writeMu@L96: c.nc.Write [io?]
Documented in the cited source as deliberate: cluster/energy/devicemanagement.go:234 (mu 'held across the manager hooks'), cluster/thermo/thermostat_server.go:276 (op 'held across the Sink'); contrast cluster/energy/waterheater.go:121 ('never held across a host call'), cluster/transition/transition.go:181 ('never held across a callback'). docs/guides/concurrency.md:82-97 states the server rule.

## 4 Time and randomness

### 4a time
- Raw `time.Now/After/Sleep/NewTimer/Tick` (and AfterFunc/NewTicker/Since/Until) in non-test library code: 2 sites, both mdns/subtype_responder.go:358 and :384 (SetReadDeadline); guarded as the single exception in clock/noraw_test.go (rawTimeExceptions, `mdns/subtype_responder.go:Now`, 2). The ADR 0026 text says three remain; the tree has these two.
- Port implementation: clock/clock.go:74 (Now), :78 (AfterFunc), :82 (NewTimer), :85 (NewTicker); clock/clocktest/fake.go:238 (time.Sleep).
- `clock.System{}` constructed directly in non-test library code (default or fixed, bypassing an injected clock at that site): fabrictest/site.go:200, transport/btp/session.go:70,272, bridge/health_probe.go:72, secure/mattercert/verify.go:51,182, secure/attestation/builder.go:109, internal/clockvar/clockvar.go:29, groups/manager.go:185, store/diagnostics.go:67, controller/dcl/snapshot/snapshot.go:127, controller/discovery/names.go:161, controller/ca/ca.go:266, cluster/core/ota_software_update_requestor.go:351, cluster/core/diagnostic_logs.go:223 (bootEpoch and clock), cluster/core/general_diagnostics.go:251, cluster/core/ota_provider.go:268, cluster/core/commissioner_control.go:221, cluster/onoff/server.go:251, cluster/energy/{devicemanagement.go:283,commoditytariff.go:270,evse.go:257}, cluster/closure/closuredimension_server.go:216, cluster/attribute_changes.go:183,194.
- examples/ and script/ raw time: see 2b.

### 4b randomness (non-test, outside script/ and examples/; crypto/rand unless noted)
- transport/mrp: counter.go:40 Read(seed) - seam: none declared. retransmit.go:130 math/rand/v2 NewPCG(0xCAFEBABE,0xDEADBEEF) when rng nil - seam: NewRetransmitter(send, rng *rand.Rand) retransmit.go:128.
- bridge: handlers.go:368 Read - seam: field randomSource handlers.go:98, used :326, replaced in handlers_test.go:231,646. case_initiator.go:458 Read - none. commissioning_window.go:947,974,995 Read - none. receive_dispatch.go:948 math/rand/v2 IntN (report jitter) - none. outbound_reliable.go:133,349 math/rand/v2 Float64 handed to mrp.BackoffDuration - none.
- secure/sigma: initiator.go:186; protocol.go:590,826,854 Read; sigma.go:847 rand.Reader (ECDSA sign) - none declared.
- secure/operational/manager.go:516,1273 Read - none.
- secure/attestation: builder.go:145,175 rand.Int(rand.Reader); cd.go:90 ecdsa.Sign(rand.Reader) - none.
- secure/spake2/spake2.go:690 rand.Int(rand.Reader) - seam: package var randomScalarFn (:677), replaced in golden_test.go:106-115.
- mdns/operational_resolver.go:136 math/rand/v2 Float64 (retry jitter) - none.
- groups: sender.go:261 Read - none. counter.go:79 Read - seam: parameter random func() (uint32, error) newDataCounter counter.go:99, field counterRandom manager.go:147, tests pass fixedStart (counter_test.go:49,81,99).
- node: node.go:416 Read(instanceID); security.go:630 ecdsa.GenerateKey(rand.Reader) - none.
- contract/dataversion.go:53 Read - package var InitialDataVersion (func() uint32) is the seam.
- commissioning: attestation.go:110, csr.go:77,86,108 (rand.Reader) - none.
- bootid/bootid.go:39 cryptorand.Read(salt) - none.
- im/subscription/manager.go:219 Read - none.
- controller/handshake/pase.go:104 Read - none.
- controller/exchange/manager.go:121,256 Read - none; math/rand/v2 as mrand imported (:13), jitter via Config.Rand func() float64 (:67, field :90).
- controller/interaction/interaction.go:24 mrand; default mrand.Float64 :124; seam Config.Rand (:104), field :87; sustained.go:48-52.
- controller/client: node.go:85 default mrand.Float64; seam Config.Rand (config.go:59).
- controller/discovery/names.go:164 rand.Uint32 default; seam field random func() uint32 (:92,:122), solicitor.go:81.
- controller/peer/set.go:112 rand.Float64 default; seam Config.Rand (:58).
- controller/ca: ca.go:268-269 default rand.Reader; seam Config.Rand io.Reader (:184). controller/ca/keyfile/keyfile.go:193,201 Read - none.
- controller/commissioner: commissioner.go:197-198, fabric.go:121-122 default rand.Reader; seam Rand io.Reader (commissioner.go:62, fabric.go:44).
- cluster/core: operational_credentials.go:1389,1465,1516; vid_verification.go:214 (rand.Reader) - none. ota_software_update_requestor.go:354 default rand.IntN (math/rand/v2); seam Config.Rand func(n int) int (:138). ota_provider.go:270-271; commissioner_control.go:214-215 default rand.Reader; seam Config.Rand io.Reader (ota_provider.go:202, commissioner_control.go:127,183).
- cluster/lock: doorlock_server.go:590; auth.go:77 Read - none. cluster/thermo/presets.go:295 Read - none.
- fabrictest/net.go:85,106 math/rand/v2 PCG seeded from Options.Seed (test support).
- examples/: crypto/rand in quickstart-controller/main.go:259 and reference-controller/cli/{self,group,stack,window}.go; math/rand/v2 in reference-bridge/fleet_energy_market.go:248,301.
Imports of `crypto/rand` or `math/rand` in non-test library code: 33 files import crypto/rand, 11 import math/rand/v2 (transport/mrp/retransmit.go, bridge/{receive_dispatch,outbound_reliable}.go, mdns/operational_resolver.go, controller/{client/node,exchange/manager,interaction/interaction,discovery/names,peer/set}.go, cluster/core/ota_software_update_requestor.go, fabrictest/net.go).

## 5 Errors

### 5a `errors.New("<prefix>:` per package directory vs package name (596 literals; counts after the prefix; 'none' = literal without `prefix:`)
47 of 70 package dirs use only their package name (or the dir name) as prefix. The 23 that differ or have unprefixed literals:
- bdx/otaimage [otaimage] none:1
- bridge [bridge] bridge:40, receive:5, reply:3, privacy:3, securechannel:2
- cluster/appliance [appliance] microwaveovencontrol:2, appliance:1
- cluster/closure [closure] closurecontrol:3, closuredimension:2
- cluster/core [core] matter:38, none:10, core:10, identify:1
- cluster/cover [cover] windowcovering:3
- cluster/light [light] colorcontrol:1
- cluster/lock [lock] doorlock:3
- cluster/spec [spec] spec:16, none:1
- cluster/thermo [thermo] thermostat:3, temperaturecontrol:3
- cluster/wire [wire] wire:8, matter:1
- controller/attestation [attestation] none:14
- controller/commissioner [commissioner] none:14, commissioner:10
- controller/dcl [dcl] none:9, dcl:5
- im [im] im:11, none:4
- internal/chipdm [chipdm] none:4, conformance:3, chipdm:1
- mdns [mdns] mdns:4, none:2
- node [node] node:10, none:9, ota:1
- secure/attestation [attestation] attestation:3, none:1
- secure/sigma [sigma] sigma:17, none:1
- secure/spake2 [spake2] spake2:6, hex:1
- store [store] none:5
- transport/btp [btp] btp:2, none:1
(cluster/core uses `matter:` 38 times, e.g. cluster/core/access_control_auxiliary.go:24 'matter: AccessControl: '; cluster/appliance/closure/cover/light/lock/thermo servers use the cluster name as prefix.)

### 5b typed error types (`func (e T) Error() string`, non-test)
- fabrictest: eventStatusError wire.go:115
- transport/btp: ProtocolError codec.go:16, FlowError codec.go:24
- bridge/bridgetest: StatusError session.go:38
- bridge: fieldConstraintError fields_reader.go:146, invokeStatusError ota_provider_link.go:85, fieldInvalidCommandError fields_reader_groupkeys.go:33, commWindowDurationInvalidErr commissioning_window.go:1023, chunkStatusError subscription_reestablish.go:210
- groups: NoKeyError decode.go:52
- bdx: Error errors.go:31, StatusResponseError errors.go:83
- controller/client: peerShutdownError fabricops.go:50
- controller/exchange: UnexpectedMessageError errors.go:70, PeerUnresponsiveError errors.go:86, PeerMessageMissingError errors.go:101, PeerInitiatedCloseError errors.go:121, StatusReportError errors.go:169
- controller/interaction: StatusError interaction.go:66
- controller/dcl: Error client.go:42, ResponseError client.go:64, crlRejectedError revocation.go:65, OTAError ota.go:34, gitHubHTTPError github.go:72
- controller/attestation: Error check.go:66
- controller/peer: UnreachableError connection.go:44
- controller/commissioner: CommissioningError errors.go:58, PairRetransmissionLimitReachedError errors.go:86, PeerCommunicationError errors.go:104, CommissioningTransitionError errors.go:122
- ble/bluez: Error doc.go:62
- cluster/valve: constraintError valve_server.go:511
- cluster/modeselect: invalidCommandErr modeselect_server.go:344
- cluster/fan: statusError fancontrol_server.go:799
- cluster/spec: vetoError typed.go:267, StatusError write.go:25, FieldError codec.go:87
- cluster/core: aclStatusError access_control_auxiliary.go:24, refusedWrite definitions.go:35, opcredsInvalidArgErr operational_credentials.go:337, opcredsConstraintErr operational_credentials.go:354, opcredsFailsafeRequiredErr operational_credentials.go:717, opcredsInvalidCommandErr operational_credentials.go:2434, groupcastStatusError groupcast.go:159, opcredsUnsupportedAccessErr vid_verification.go:242, otaStatusError ota_software_update_requestor.go:452, diagLogsError diagnostic_logs.go:380, gendiagStatusErr general_diagnostics.go:780, groupsStatusError groups.go:113, timeSyncReadOnlyErr time_synchronization.go:139, netcommStatusError network_commissioning_wifi.go:263, bindingStatusError binding.go:386, scenesStatusError scenes_management.go:607, otaDownloadError ota_requestor_query.go:506, groupKeyNotFoundErr group_key_management.go:91, groupKeyExhaustedErr group_key_management.go:105, gkmStatusError group_key_management.go:525
- cluster/media: StatusError media.go:84
- cluster/measurement: writeRefusal definitions.go:100
- cluster/cover: windowCoveringConstraintErr windowcovering_server.go:451, maintenanceErr windowcovering_server.go:502
- cluster/wire: admCommBusyErr admincommissioning.go:510, admCommPakeErr admincommissioning.go:529, admCommWindowNotOpenErr admincommissioning.go:544, admCommInvalidFieldsErr admincommissioning.go:558
- cluster/modebase: statusError modebase_server.go:840
- cluster/alarmbase: statusError alarmbase.go:500
- cluster/alarm: statusError smokecoalarm_server.go:765
- cluster/light: colorControlConstraintErr colorcontrol_server.go:598, colorControlInvalidCommandErr colorcontrol_server.go:604
- cluster/levelcontrol: constraintErr levelcontrol_server.go:1222, invalidCommandErr levelcontrol_server.go:1229, failureErr transitions.go:430
- cluster/energy: statusError waterheater.go:390
- cluster/lock: occupiedError users.go:36, LockOperationFailedError doorlock_server.go:202
- cluster/closure: closureUnsupportedCommandErr closurecontrol_server.go:254, closureInvalidCommandErr closurecontrol_server.go:263, closureConstraintErr closurecontrol_server.go:271
- cluster/opstate: statusError opstate_server.go:983
- cluster/thermo: thermoConstraintErr thermostat_server.go:1455, thermoInvalidCommandErr thermostat_server.go:1464, tcStatusError temperaturecontrol.go:376
- cluster/switch: statusError server.go:249
- im: unsupportedCommandError status.go:111, invokeDecodeStatusError invoke.go:96
- endpoint: DeviceTypeConformanceError devicetype_validation.go:363
Total 87 types with Error(); 58 types implement `MatterStatusCode() StatusCode` (im.StatusCodeError, im/status.go:31-34) in 42 files - cluster/core 14 files, cluster/{alarm,alarmbase,closure,cover,energy,fan,levelcontrol,light,lock,media,modebase,modeselect,opstate,spec,switch,thermo,valve,wire}, bridge/{commissioning_window,fields_reader,fields_reader_groupkeys,ota_provider_link}.go, im/{invoke,status}.go; `im.MatterClusterStatusError` (im/status.go:36-55) is referenced at cluster/wire/admincommissioning.go:568-572 (3 uses) and in cluster/lock (1 use).

### 5c status carried by error text
- endpoint/dispatcher.go: writeErrorStatus :831, invokeErrorStatus :861, containsAny :888 (strings.Contains :890). After `errors.AsType[im.StatusCodeError]` (:840 and :870) the 'legacy string-heuristic fallback' maps err.Error(): write path :846-:855 'read-only'/'read only' -> UnsupportedWrite, 'unknown attribute' -> UnsupportedAttribute, 'resource exhausted' -> ResourceExhausted, 'constraint' -> ConstraintError (:852); invoke path :871-:882 'unknown command'/'no commands' -> UnsupportedCommand, 'constraint' -> ConstraintError (:874), 'invalid command argument' -> InvalidCommand (:878); else Failure. Other 'constraint' hits in the file (:1133,:1403-1419) are comments or direct im.StatusConstraintError returns.
- Error texts that reach that fallback by wording (plain errors.New/fmt.Errorf, no MatterStatusCode in the same expression): cluster/core/access_control.go:708-783 and :896-908 ('resource exhausted:' / 'constraint error:' in fmt.Errorf), cluster/core/group_key_management.go:749-755 ('constraint error:'), :759-783 and :884 ('invalid command argument:'), cluster/core/operational_credentials.go:1440 ('invalid command argument'), :2293,:2296 ('constraint error:'), :1068 ('read-only'), cluster/core/{groupcast.go:431,group_key_management.go:451} ('is read-only'), cluster/core/basic_information.go:184 and bridged_device_basic_information.go:134 ('unknown / read-only attribute'), cluster/core/descriptor.go:114, cluster/measurement/measurement.go:270 ('read-only'), :274 ('no commands'), cluster/wire/schedules.go:37, cluster/wire/scenes_management.go:41,83 ('read-only', 'no commands'), cluster/cover/windowcovering_server.go:562 ('unknown command'), cluster/closure/closurecontrol_server.go:300 ('unknown command'), cluster/lock/doorlock_server.go:113 ('unknown command'). Where `refusedWrite{sentinel:..., status: err}` is used (cluster/core/definitions.go:35-45; general_commissioning.go:386, icd_management.go:87, network_commissioning.go:229, diagnostic_logs.go:367, general_diagnostics.go:505, groups.go:198, scenes_management.go:486) the code comes from the type, not the text.
- `fmt.Errorf` containing 'status' outside the dispatcher (non-test): 27 hits, none builds an IM status code from text; they wrap sentinel errors (bridge/reply.go:658; bridge/receive_dispatch.go:321; bdx/messenger.go:101; controller/client/fabricops.go:338; controller/interaction/{report.go:496,write.go:152,226,interaction.go:191}; controller/exchange/errors.go:185; im/{status.go:216-227,timed.go:115-154,client.go:213,invoke_client.go:173}; bridge/bridgetest/{commissioner.go:446,session.go:188}; cluster/thermo/thermostatuserinterfaceconfiguration.go:165-216 where 'status' is part of the package prefix).
- `im.UnsupportedCommandf` (im/status.go:130) is called from 37 cluster files (40 uses); cluster servers import `im` only to spell statuses in 1a.

## 6 Logging

Method: first string literal of `.Info/.Warn/.Debug/.Error(` and the *Context variants (565 calls with a literal first argument; plus 7 where the literal sits on the next line). Dotted lower case `a.b.c` = regex `^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$`.

### 6a event names that are not dotted lower case
- bridge/subscribe.go:517 `"matter.tx.subscribe."+op` (prefix + variable)
- bridge/receive_dispatch.go:316 and :324 `"matter.tx."+op+".chunk_rejected"` (prefix + variable)
- cluster/core/general_diagnostics.go:364 logger.Info("The DataModelTest feature is enabled but is only required when maxPathsPerInvoke is greater than 1; ...") (sentence)
- tlv/encode.go:323 package-level slog.Warn("tlv: PutUTF8Bounded: string truncated") (prefix style, global logger)
- tlv/encode.go:239 package-level slog.Warn("tlv: UTF-8 string exceeds max length; trimming") (global logger; the call starts at :238)
- im/subscription/manager.go:340 "matter subscription added" (spaces)
- im/subscription/persistence.go:121 "matter subscription restored" (spaces)
- endpoint/assembler.go:378 "matter endpoint gc skipped: model incomplete" and :395 "matter endpoint gc" (spaces)
- node/commissioning.go:237 h.logger.Warn(event, ...) (variable name, not inspected)
- binding/resolve.go:61 m.log.Info(msg, ...) (variable name; callers pass `binding.peer.*` literals, e.g. resolve.go:321)
- cluster/cover/windowcovering_server.go:721 slog.Default().Warn(clampMsg, ...) (constant/variable name)
Count: 10 literal event names are not dotted lower case (of 565 single-line literal calls plus 7 multi-line ones; the first 8 rows), and 3 events are variable-named (last 3 rows, content not inspected). Everything else (about 560) matches the pattern.

### 6b printing and the global logger
- `fmt.Print*`, `fmt.Fprint*(os.Std...)`, `log.Print*`, `println` in non-test code outside examples/ and script/: ble/bluez/cmd/reference-controller/main.go:21 `fmt.Fprintln(os.Stderr, ...)` (a main package; line :19 passes os.Stdin/os.Stdout to cli.Run). No import of package `log` in non-test code. examples/ has 15 fmt.Print/Fprint lines.
- Structs with a `*slog.Logger` field: 69 (bdx 1, binding 2, bridge 1, cluster/core 7, controller/ble 5, controller/client 6, controller/commissioner 4, controller/dcl 4, controller/discovery 1, controller/exchange 2, controller/handshake 5, controller/interaction 7, controller/peer 3, endpoint 1, fabrictest 3, groups 2, im/subscription 1, mdns 2, node 6, transport/btp 2, transport/tcp 4). No cluster server outside cluster/core has a logger field.
- `slog.Default()` as a constructor fallback for a nil logger: transport/tcp/conn.go:68, tcp/listener.go:107, bridge/bridge.go:736, mdns/subtype_responder.go:103, mdns/operational_resolver.go:78, groups/manager.go:174, node/persist.go:83, node/node.go:141, bdx/responder.go:101, controller/discovery/names.go:144, cluster/core/ota_software_update_requestor.go:357, cluster/core/diagnostic_logs.go:223,262, cluster/core/ota_provider.go:275, im/subscription/manager.go:193, endpoint/assembler.go:120.
- `slog.Default().<Level>(...)` called inline (no logger field or parameter): 44 calls in 15 files: cluster/thermo/thermostat_server.go 9 (:414-1247), cluster/core/basic_information.go 7 (:463-978), cluster/core/operational_credentials.go 6 (:1175-2127), cluster/thermo/presets.go 3, cluster/thermo/atomic.go 3, cluster/core/general_commissioning.go 3 (:432-441), cluster/cover/windowcovering_server.go 2 (:721,741), cluster/core/general_diagnostics.go 2 (:919,923), cluster/core/bridged_device_basic_information.go 2 (:283,296), bridge/handlers.go 2 (:746,793), 1 each in transport/udp/listener.go:255, mdns/zeroconf.go:637, mdns/service.go:487, cluster/light/colorcontrol_set.go:47, cluster/core/access_control_auxiliary.go:102. Package-level `slog.Warn` in tlv/encode.go:238,323.

## 7 Persistence outside the seam
`os.Open/OpenFile/Create/CreateTemp/WriteFile/ReadFile/MkdirAll/Mkdir/Rename/Remove/RemoveAll/Stat/ReadDir` and `sql.Open`, non-test, outside store/, endpoint/sqlitestore/, controller/client/sqlstore/, node/store_path.go, bdx/, examples/, script/:
- fabrictest/device.go:116 sql.Open("sqlite", ...) (test support)
- bridge/subscribe_dispatch.go:425 os.WriteFile("/tmp/subscribe-init-sub%d-chunk%d.bin") gated by dumpSubscribeEnabled (env GOFABRIC_DUMP_SUBSCRIBE, comment :412-417)
- secure/attestation/files.go:129 os.ReadFile(path)
- node/ota.go:71 os.Remove; :75 os.MkdirAll(appliedDir); :79 os.Rename; :80 os.Remove; :127 os.Open; :178 os.MkdirAll(ota.Dir)
- controller/dcl/store.go:57 os.ReadFile; :66 os.CreateTemp; :72,:76 os.Remove; :79 os.Rename (FileStore.Path)
- controller/dcl/snapshot/fetch.go:91 os.RemoveAll; :94 os.MkdirAll; :99,:107 os.WriteFile
- controller/ca/keyfile/keyfile.go:125 os.ReadFile; :209 os.OpenFile(O_EXCL); :215,:219 os.Remove
- cluster/core/ota_requestor_query.go:185 os.Stat; :189,:388,:474 os.Remove; :621 os.Open (r.downloadPath())
Excluded as asked: bdx/sink.go:55,86,95-108 (os.CreateTemp/Remove/Rename). No other file access with the listed functions.

## 8 Facade surface
Method: `go doc -all ./node`, `./device`, `./contract` (1169, 5388, 853 lines); declaration lines (col 0 and tab-indented) scanned for `pkg.Ident` where pkg is not stdlib and not node/device/contract/schema/eligibility/cluster-spec (aliases ending `def` = cluster/spec/<cluster> packages, all verified to import cluster/spec/*). 'mattercore' = cluster/core. Type aliases are marked [alias]. Doc line = line in the `go doc` output.
- node doc:67 `const (` -> mattercore.RegulatoryIndoor, mattercore.RegulatoryIndoorOutdoor, mattercore.RegulatoryOutdoor, setup.MaxDiscriminator, spake2.IterationsMax, spake2.IterationsMin, spake2.PBKDFMaxSaltSize, spake2.PBKDFMinSaltSize
- node doc:90 `const DefaultProbeInterval = bridge.DefaultProbeInterval` -> bridge.DefaultProbeInterval
- node doc:123 `var (` -> bridge.ErrAdministratorWindowOpen, bridge.ErrCommissioningBlocked, bridge.ErrCommissioningWindowAlreadyOpen, bridge.ErrCommissioningWindowNotConfigured
- node doc:191 `func (a *Aggregator) IncreaseConfigurationVersion(key endpoint.SourceKey) error` -> endpoint.SourceKey
- node doc:216 `func (a *Aggregator) Remove(ctx context.Context, key endpoint.SourceKey) error` -> endpoint.SourceKey
- node doc:238 `type Attestation struct` -> attestation.Chain
- node doc:245 `type AttestationFiles = attestation.ChainFiles` [alias] -> attestation.ChainFiles
- node doc:310 `type Client struct` -> discovery.Names
- node doc:321 `type CommissionerControlConfig struct` -> mattercore.CommissioningApprover, mattercore.ReverseCommissioner
- node doc:381 `type CommissioningWindow = bridge.CommissioningWindow` [alias] -> bridge.CommissioningWindow
- node doc:405 `type Config struct` -> clock.Clock, endpoint.DeviceTypeValidationMode, mattercore.CapabilityMinimaStruct, mattercore.GeneralDiagnosticsFeatures, mdns.Advertiser
- node doc:549 `type DiagnosticEvent = diagevent.Event` [alias] -> diagevent.Event
- node doc:554 `type Endpoint interface` -> endpoint.Spec
- node doc:561 `type EndpointPersistence interface` -> endpoint.ScenesStore, endpoint.SourceKey
- node doc:580 `func NewEndpointPersistence(st *store.Store, logger *slog.Logger) EndpointPersistence` -> store.Store
- node doc:621 `type HealthSample = bridge.HealthSample` [alias] -> bridge.HealthSample
- node doc:638 `type MDNSFinding = mdns.Finding` [alias] -> mdns.Finding
- node doc:641 `type MDNSService = mdns.Service` [alias] -> mdns.Service
- node doc:735 `func (n *Node) Bridge() *bridge.Bridge` -> bridge.Bridge
- node doc:739 `func (n *Node) CommissionerControl() *mattercore.CommissionerControl` -> mattercore.CommissionerControl
- node doc:757 `func (n *Node) Fabrics(ctx context.Context) ([]store.FabricRecord, error)` -> store.FabricRecord
- node doc:787 `func (n *Node) OTAProvider() *mattercore.OTAProvider` -> mattercore.OTAProvider
- node doc:835 `func (n *Node) Peers() *peer.Set` -> peer.Set
- node doc:899 `func (n *Node) Store() *store.Store` -> store.Store
- node doc:922 `type OTAProviderConfig struct` -> mattercore.OTAApplyPolicy, mattercore.OTAConsent, mattercore.OTAImageSource, mattercore.OTAPeer, mattercore.OTAUpdateStatus
- node doc:1007 `type OtaConfig struct` -> mattercore.OtaApplier
- node doc:1025 `type RootClusters struct` -> mattercore.AccessControl, mattercore.BasicInformation, mattercore.Descriptor, mattercore.DiagnosticLogs, mattercore.GeneralCommissioning, mattercore.GeneralDiagnostics, mattercore.GroupKeyManagement, mattercore.Groupcast, mattercore.NetworkCommissioning, mattercore.OTASoftwareUpdateRequestor, mattercore.OperationalCredentials, mattercore.PowerSourceConfiguration, mattercore.TimeSynchronization, mattercore.WiFiNetworkCommissioning, mattercore.WiFiNetworkDiagnostics, wire.AdministratorCommissioning
- node doc:1063 `type SessionManager = operational.Manager` [alias] -> operational.Manager
- node doc:1068 `type Snapshotter = bridge.Snapshotter` [alias] -> bridge.Snapshotter
- node doc:1077 `type Store struct` -> endpoint.Store, sql.DB, store.Store
- node doc:1088 `type SubscriptionManager = subscription.Manager` [alias] -> subscription.Manager
- node doc:1093 `type TCPConfig = bridge.TCPConfig` [alias] -> bridge.TCPConfig
- node doc:1097 `type Topology = endpoint.Topology` [alias] -> endpoint.Topology
- node doc:1104 `type WiFi struct` -> mattercore.WiFiReporter
- node doc:1159 `type WindowSnapshot struct` -> wire.WindowStatusSnapshot
- device doc:3391 `type AvailabilityProbe = endpoint.AvailabilityProbe` [alias] -> endpoint.AvailabilityProbe
- device doc:3865 `func (d *Device) Spec() endpoint.Spec` -> endpoint.Spec
- device doc:4654 `type Options struct` -> clock.Clock
- device doc:5090 `type SourceKey = endpoint.SourceKey` [alias] -> endpoint.SourceKey
- contract: no declaration references a package outside the allowed set (the one scan hit, doc:22, is `err.Error`).
Names containing Behavior/Agent/Transaction/Backing/Hull among exported identifiers of node, device, contract: none. Prose mentions in the go doc output (lines containing the word, case-insensitive): 'behavio' node 4, device 11, contract 11; 'agent' 0/0/0; 'transaction' 0/0/0; 'backing' 0/0/2; 'hull' 0/0/0. One identifier-adjacent hit: node doc:482 comment `TimeSynchronizationBehavior (GeneralDiagnosticsServer.ts:168-182)` inside Config.

## 9 Tests

### 9a packages without test files
`go list` (229 packages). With the briefed template (`{{if not .TestGoFiles}}`) 43 packages match; 37 of those only have external `_test` package files (XTestGoFiles). Packages with no test file of any kind (6): examples/reference-controller, internal/bridgeseam, internal/channelseam, internal/nodeseam, internal/udpseam, script/dclsnapshot.
External-test-only (37): . (module root), bdx, bdx/otaimage, bridge/bridgetest, clock, clock/clocktest, cluster/{alarm,alarmbase,appliance,boolcfg,closure,cover,fan,filter,levelcontrol,light,lock,media,modebase,modeselect,pump,servicearea,transition,valve}, cluster/spec/spectest, commissioning, controller/ble/bletest, controller/ca, controller/client/sqlstore, endpoint/endpointtest, endpoint/sqlitestore, internal/clockvar, parity, secure, secure/sessionparams, store, transport.

### 9b one run of `go test -count=1 -json ./...` (this worktree, a71223c2)
Wall time 364 s (exit 0); 229 packages: 223 pass, 6 no test files, 0 fail; 52 individual tests skipped. Summed per-package elapsed 371 s. The 15 slowest (elapsed per package, parallel run):
- controller/client 108.8 s; node 73.2 s; script/clustergen 23.5 s; cluster/lock 18.8 s; examples/reference-bridge 17.2 s; fabrictest 12.8 s; examples/reference-controller/cli 12.1 s; cluster/thermo 8.4 s; . (module root) 7.1 s; bridge 6.3 s; internal/chipdm 5.7 s; store 4.2 s; mdns 4.0 s; cluster/onoff 3.8 s; transport/mrp 3.8 s.

### 9c the named flaky tests (file:line; what each waits on)
- TestRegistryDecommission - controller/client/registry_test.go:296 (t.Parallel). Registry on a clocktest.Fake (helpers_test.go:21-25, registry_test.go:102-113). Decommission runs in a goroutine (:304); the test waits with `sr.next(t)` (:305, :317; controller/client/helpers_test.go:86-111: select on s.conn.ch against `time.After(5 * time.Second)` at :107, then t.Fatal) and `<-done` (:321, no timeout). At :309 it asserts a second fabric operation is refused while the first is in flight (client.ErrFabricOperationInProgress).
- TestLiveSustainedSurvivesRestart - controller/interaction/live_test.go:305. waitOnOff (:126-141) = lv.site.Eventually under a 30 s context (:128); lv.wait("the subscription active", 10*time.Second, sub.Active) (:322; helper :147-154); waitPersisted (:375-388) Eventually under 30 s; restartCtx 30 s (:329). Site.Eventually (fabrictest/site.go:302-318) loops Site.quiet (settle 10 ms, DefaultSettle site.go:26) and advances the fake clock.
- TestNodeLifecycleSeededPairParity - controller/client/node_lifecycle_seeded_parity_matterjs_test.go:49 (two parallel subtests :52, :66). newClientPair (parity_matterjs_helpers_test.go:88-119): CommissionNode under parityCtx = 3 min (:30-32), then cp.eventually("the default subscription", n.SubscriptionActive) = Site.Eventually under parityCtx; second subtest Controller.Restart(parityCtx) (:73).
- TestNetworkLossAndDelay - fabrictest/site_test.go:187. testContext = 60 s (:61-66). Waits are inside fabrictest.Read/Connect (wire.go:64): a read over Loss:1 must fail (Loss:1 set at :192), then Delay 300 ms with Duplicate 1 (:198), then one dropped datagram via Net().Filter (:210); no explicit sleep or Eventually in the test body.
- TestDiagLogsBDX_Transfer - cluster/core/diagnostic_logs_bdx_test.go:275 (t.Parallel). Real time, channels only: `<-answered` (:300), opener.receive(t,...) (:299, :339), select on `<-done` against `time.After(50 * time.Millisecond)` (:316-319, a negative wait: the transfer must not have ended before the response), `<-done` (:323), `<-c.ended` (:329), `<-third` (:340), `<-done3` (:342); a gate channel (:281) releases the collector; no timeout on the unconditional receives.
- TestOtaParityMatterJS - node/ota_parity_matterjs_test.go:399. holdSite(t) (node/parity_matterjs_helpers_test.go:58-70) serialises sites with a sync.Cond; s.eventually (:343-351) = `site.Settle(parityCtx)` loop under 90 s (parity_matterjs_helpers_test.go:34-39); restartInto runs `site.Run(parityCtx, s.req.Stop/Start)` (:373, :377); subtests block on `select { <-release | <-ctx.Done() }` (:539-541) and `<-arrived` (:574-575).
- secure/operational reaper tests - secure/operational/manager_idle_reaper_test.go: TestStartReaper_EvictsIdleSessionGracefully :25 (wall-clock poll `for m.Active() > 0 && time.Now().Before(deadline)` with 2 s deadline and time.Sleep(5ms), :41-43; a second 2 s poll :53-58); TestStartReaper_ActiveSessionSurvivesSweeps :74 (time.Sleep(10ms) :98, 2 s poll :105-107); TestStartReaper_NeverMarkedSessionIsNotReaped :119 (time.Sleep(150ms) :133, 'several TTL windows'). The reaper ticks on m.clk() (secure/operational/manager.go:1184).

## Item counts per section
- §1: 71 (33 distinct im.* identifiers behind cluster/* -> im across 75 files / 25 packages; 38 identifier rows for the 5 named edges; 5 archcheck exceptions listed)
- §2: 64 table rows covering 79 goroutine sites (74 `go` + 5 `.Go`), plus 3 timer findings (2 raw time.Now sites, 1 exception entry)
- §3: 199 mutex declarations (50 with a naming comment, 149 without); 119 flagged calls under a held lock in 53 files
- §4: 27 (2 raw time sites + 25 randomness bullets covering 33 crypto/rand and 11 math/rand files)
- §5: 87 typed Error() types listed in 5b (plus 58 MatterStatusCode implementers, 23 package dirs flagged in 5a, 2 dispatcher fallback functions)
- §6: 11 rows in 6a (10 literal sites + 3 variable-named); 1 print site; 69 logger-field structs; 44 inline slog.Default() calls
- §7: 8 rows (8 packages) with file-system access outside the seam
- §8: 39 declarations in node/device with a foreign type (node 35, device 4, contract 0); 0 identifiers named Behavior/Agent/Transaction/Backing/Hull
- §9: 6 packages without any test file (43 by the briefed template), 15 slowest listed, 7 flaky entries
