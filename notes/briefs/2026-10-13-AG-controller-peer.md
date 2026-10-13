# Brief AG — `controller/peer`: peers, the connection process and the address monitor (ADR 0020, stage 1, part 3)

Owner: Fable. Implementer: Opus (one agent; nothing else runs on the repo
while you do). Branch `wave13/peer` from `planning/briefs-wave-13`, pushed,
**no PR**. Report in `notes/briefs/reports/AG.md`. Read first:
`notes/briefs/README.md`, [ADR 0020](../../docs/adr/0020-commissioner-and-controller-role.md),
concept v2 §B.1, the reports `notes/briefs/reports/AE.md` and `AF.md`
(the owner's decisions at their end are rules here), and the two packages
this one composes: `controller/discovery` (`doc.go`, `service.go`,
`operational.go`) and `controller/exchange` + `controller/handshake`
(`doc.go`, `manager.go`, `exchange.go`, `session.go`, `case.go`,
`resumption.go`). matter.js at **`0ceb202d`**; the `../matter.js` checkout
may stand later — read with `git -C ../matter.js show 0ceb202d:<path>`.
Paths below are relative to `packages/protocol/src/peer` unless named.
Read the lines a fact cites before you mirror them; a brief line that
differs from the file is a line to correct in the report, as AE and AF did.
One commit per item, reachability in each, CHANGELOG under `[Unreleased]`
→ Added. Guards: `make ci`, `make race`, `make reachability-check`, and
`make chiptool-test` once at the end (the responder is untouched; the
guard proves it). You may change `controller/discovery` and
`controller/exchange` where §0 says so and nowhere else outside
`controller/peer`; `secure/`, `transport/`, `mdns/`, `bridge/`, `node/`,
`im/` are read, not changed — a change you need there is a question in
the report. `notes/parity/by_design.md` and the findings register are the
owner's.

Out of this brief, by ADR 0020 and the stage split: TCP and
`transportPreference`/`resolveTransports`/`tcpUnsupported` (the module has
no TCP client; `OperationalAddress` is UDP only), group sessions
(`Peer.connect` for a group address, `PeerSet` group lookups),
`NetworkProfiles` (the semaphore slot, `additionalMrpDelay`, `mrpMargins`,
`probeAddress`: our one network is the LAN; margins are 0 as in AF),
`PhysicalDeviceProperties` and the LIT ICD floor (`Peer.ts:59`,
`:307-314` — a stage-4 reader's input; note it), `BasicInformation` in
`limits`/`sessionParameters` (`Peer.ts:238-248`, `:282-302` — stage 3/4;
the fallbacks of `exchange.PeerParameters` stand in), the ICD
`wakefulness` branch of the monitor (`PeerAddressMonitor.ts:149-172`),
`RebootResubscribeArmer.ts` (OTA; a later brief), `PeerExchangeProvider.ts`
and `ClientInteraction` (stage 3 — except the one probe below).

## What exists (reuse, do not duplicate)

- `controller/discovery`: `Names` (`Open`, `NewNames`), `Names.Service(cfid,
  nodeID)` and `OperationalQName` (`operational.go:20-37`), `Service`
  (`Addresses`, `Parameters`, `HasTXTRecord`, `Reachable`, `AddressChanges(ctx)`,
  `Resolve(ctx)`, `Close`; `service.go:160-262`), `Address`
  (`service.go:21-36`, `UDPAddr()`), `CompareDesirability`
  (`service.go:76`), `TXTData`/`ParseTXTData` (`txt_data.go`).
- `controller/exchange`: `Manager` (`NewManager(conn, Config)`,
  `NewUnsecuredSession(addr, peerParams)`, `NewSecureSession`,
  `Initiate(session, protocol, Options)`, `CloseSession`, `Close`,
  `HandleDatagram`; `manager.go:52-460`), `Config.PeerLost(s, cause)`
  (`manager.go:68-72`), `Exchange` (`Send`, `Next`, `Close`, `Abort`,
  `Done`, `HasUnackedMessage`; `exchange.go:124-640`), `SendOptions`
  (`MaxRetransmissions` negative = unlimited, `MaxRetransmissionTime`,
  `InitialRetransmissionTime`, `SuppressPeerLoss`; `exchange.go:56-83`),
  `Session` (`Addr`, `IsSecure`, `PeerNodeID`, `Parameters`,
  `ReportedParameters`, `CaseAuthenticatedTags`, `IsPeerActive`,
  `IsClosed`, `Close`; `session.go:54-220`), `PeerParameters`/`Overlay`
  (`params.go:56-113`), the error classes (`errors.go`: `ErrTransient`,
  `ErrNetwork`, `PeerUnresponsiveError`, `StatusReportError` with the
  general and protocol status, `errors.go:102-170`; a Busy status carries
  its delay as a little-endian uint16 of milliseconds in `ProtocolData`,
  `securechannel/SecureChannelMessenger.ts:33`, `:49`).
- `controller/handshake`: `CASE(ctx, mgr, addr, Fabric, peerNodeID,
  CASEOptions)` (`case.go:34-110`), `CASEOptions` (`PeerParameters`,
  `CaseAuthenticatedTags`, `Resumption ResumptionStore`,
  `MaxInitialRetransmissions`, `MaxInitialRetransmissionTime`,
  `InitialRetransmissionTime`), `PeerAddress{FabricIndex, NodeID}`,
  `ResumptionStore` (`resumption.go:12-60`).
- `im.ReadRequest{}.MarshalTLV` (`im/read.go:82`) and `im.ReportData`
  (`im/read.go:270`, decode it as `bridgetest/session.go` `reports()` does)
  for the liveness probe; `im.OpcodeReadRequest`/`OpcodeReportData`,
  the IM protocol id, and `im.StatusResponse` for the final ack.
- Tests: the in-process node and commissioned fabric of
  `controller/handshake/helpers_test.go` (`commissionedFabric`,
  `newManager`); the fake clock pattern of `controller/exchange` (`Clock`,
  `clock.go`) and `controller/discovery` (`clk.advance`, `inject`,
  `helpers_test.go`) — a connection process with 45 s staggers and a
  monitor with a 10-minute cooldown are tested on a fake clock, never on
  wall time.

## 0. Additions to `controller/exchange` and `controller/discovery`

Each a small commit before §1, each with its unit test.

- `exchange.Exchange.RetransmissionCount() int` — the count since the last
  send or receive (`protocol/MessageExchange.ts:263`, `:411-413`, reset at
  `:508`, `:611`, `:786`, incremented `:886`).
  `Exchange.RetransmissionRestartSaving() time.Duration` — the live
  retransmission timer's remaining interval minus the base backoff for
  count 0, floored at 0, and 0 with no timer pending (`:1227-1235`).
- `exchange.Options.OnSend func(retransmission int)` and
  `OnReceive func()` — matter.js `MessageExchange.Options.onSend/onReceive`
  (`:1243-1248`), called as `ExchangeManager.ts` does for an exchange's
  sends and receipts (find the call sites; cite them). `PeerConnection.ts:847-859`
  uses them: a send with retransmission > 0 marks the service unreachable
  and reports to an `onRetransmit`; a receipt stamps `lastReceiptAt`.
- `exchange.SendOptions.AddressOverride *net.UDPAddr` — one exchange's
  messages go to another address while the session's address stays
  (`MessageExchange.ts:228`, set `:297`, used for the ack `:488` and every
  send `:771`; AF's report lists it as left out). `Session.SetAddr(*net.UDPAddr)` —
  the in-place migration `session.channel.networkAddress = address`
  (`PeerAddressMonitor.ts:243`, `:170`); later sends go there.
- `exchange.Session.ActiveTimestamp() time.Time` — the last receipt from
  the peer, what `Peer.newestSession` orders by (`Peer.ts:581-601`; the
  session's `activeTimestamp` in `session/Session.ts` — cite). Zero
  before the first receipt.
- `exchange.Session.HandlePeerLoss(cause error)` and `IsPeerLost() bool`
  — `NodeSession.ts:311-314`, `:320-322`, `:349-352`: the session is
  marked lost, every exchange on it is aborted with the cause, and it is
  closed without a CloseSession on the wire (the peer is gone). The
  `Config.PeerLost` hook is called once, as the exchange-level loss calls
  it today (do not call it twice for one loss; read `manager.go:624-640`).
  `Manager.HandlePeerLoss(peer handshake.PeerAddress, cause, asOf
  time.Time)` — `SessionManager.ts:636-638`, `:677-696`: every secure
  session of that fabric and node id created before `asOf` (zero = all).
  For this the secure session needs to know its fabric index: add it to
  `SecureSessionConfig` (`FabricIndex uint8`) and `Session.FabricIndex()`;
  `handshake.CASE` fills it from `Fabric.Identity.FabricIndex` (that one
  line in `handshake` is allowed).
- Peer-initiated Secure Channel exchanges on a secure session
  (`securechannel/SecureChannelProtocol.ts:30-88`, the
  `StatusReportOnlySecureChannelProtocol`): an initial StatusReport with
  general Success and protocol CloseSession means the peer closed the
  session — `NodeSession.handlePeerClose` (`NodeSession.ts:305-309`):
  mark lost, `HandlePeerLoss` with a `PeerInitiatedCloseError`
  (`PeerCommunicationError.ts:71-78`, transient), and the exchange that
  carried it is closed gracefully (acked). Any other initial StatusReport
  is logged and the exchange closed (`:69-77`); a standalone ack alone
  opens nothing (`:45`); any other message type on the Secure Channel
  protocol on a secure session is a flow error (`:59-64`), the exchange
  closed. Today the manager ignores a message that opens an exchange
  (AF's report, "Inbound CloseSession"); this item is where it stops
  being ignored. Other protocols' peer-initiated exchanges (IM
  subscriptions' reports) stay ignored until stage 3 — but they must be
  acked when they ask for an ack, so the peer does not retransmit five
  times; read `ExchangeManager.ts` `#receiveMessage` (`:220-475`) for
  what it does with an unknown protocol and mirror that (cite).
- `discovery.Service.Status()` — the rest of `IpServiceStatus`
  (`packages/general/src/net/dns-sd/IpServiceStatus.ts:23-200`) beyond the
  flag AE built: `Connecting(result <-chan bool)` (`:114-150`: one
  outstanding attempt per channel; while any is outstanding
  `IsConnecting` is true; `ConnectionInitiatedAt` is set at the call and
  cleared when the last settles; `true` → reachable, `false` → not
  reachable), `SetReachable(bool)` (`:78-90`), `IsResolving`
  (`:74-76`), `LastReceiptAt`/`SetLastReceiptAt` (`:92-98`), and the
  rule that drives discovery (`:152-194`): not reachable **and**
  connecting **and** not already resolving → run `Service.Resolve` (the
  active solicitation, `IpServiceResolution`) until it returns or the
  status becomes reachable while no attempt is outstanding
  (`#maybeStopResolving` `:188-194`), when it is cancelled. `Close`
  cancels and waits (`:36-41`). A new address still sets reachable
  (`IpService.ts:324-325`, AE's existing flag). Keep the loop-ownership
  pattern of `controller/discovery` (`loop.go`).

## 1. `Descriptor`, `Timing`, the store (`PeerDescriptor.ts`, `PeerTimingParameters.ts`)

- `peer.Descriptor` (`PeerDescriptor.ts:39-80`): `Address
  handshake.PeerAddress`, `DataModelRevision uint16` (0 = unknown),
  `OperationalAddress *net.UDPAddr` (nil = unknown; `OperationalAddress.from`
  `:22-31` — UDP only here), `DiscoveryData *discovery.TXTData`,
  `ReportedSessionParameters *sessionparams.Parameters` (a session's
  report replaces the previous as a whole, `:60-65`),
  `CaseAuthenticatedTags []uint32` (`:73-80`: our CATs for CASE to this
  node). `Store` interface: `LoadPeers(fabricIndex) ([]Descriptor, error)`,
  `SavePeer(Descriptor) error`, `DeletePeer(PeerAddress) error`;
  `MemoryStore`. The peer saves on any descriptor change, coalesced: one
  save per change burst (`Peer.ts:99-106`, `#save` `:575-579` — matter.js
  emits `updated`; Go calls the store, and a `Peer.Updated` hook if a
  host wants the event). A store error is logged, never fatal (cite
  what matter.js does with a rejected `updated`).
- `peer.Timing` (`PeerTimingParameters.ts:21-130` fields, `defaults`
  `:187-209`): `DefaultConnectionTimeout` 90 s,
  `MaxDelayBetweenInitialContactRetries` 2 min, `DelayBeforeNextAddress`
  45 s, `DelayAfterNetworkError` 15 s, `DelayAfterPeerError` 5 min,
  `DelayAfterUnhandledError` 2 min, `KickThrottleInterval` 3 s,
  `KickMinRetransmissions` 2, `KickMinRestartSaving` 60 s (half of 2 min,
  `:199`), `KickRestartCooldown{AddressChange 30 min, Connect 10 min}`,
  `AddressChangeStabilizationDelay` 10 s, `AddressChangeProbeCooldown{Min
  10 min, Max 24 h}`. `DefaultTiming()`; `Merge(base, over)` field-wise
  with zero = unset (`:153-181`).

## 2. `Set` and `Peer` (`PeerSet.ts`, `Peer.ts`)

- `peer.Set` (`PeerSet.ts:43-339`): `NewSet(Context)` with `Context{
  Exchanges *exchange.Manager; Names *discovery.Names; Fabrics
  func(handshake.PeerAddress) (handshake.Fabric, bool) (SessionManager.fabricFor,
  `session/SessionManager.ts:407`); Resumption handshake.ResumptionStore;
  Store; Timing *Timing (nil = defaults); HandleError func(error)
  (time.Duration, bool); Logger}`. `For(addr) *Peer` (get-or-create,
  `:164-175`), `Get(addr) *Peer` (`:241-246`), `Has`, `Peers() []*Peer`,
  `Size`, `AddKnownPeer(Descriptor) *Peer` with the merge rules for a
  known peer (`:306-338`: operational address and CATs replace when
  given, discovery data merges with the existing winning,
  reported parameters only when unknown), `Adopt(session)` (the
  `sessions.added` adoption `:104-125`, `operationalAddressOf` `:341-350`:
  a secure session of a fabric becomes its peer's, creating the peer with
  the session's address as operational address; stage 2's commissioning
  CASE and `Peer.Connect` both go through it), `Disconnected` hook (a
  peer whose last session went, `:95-102`), `Disconnect(ctx)` (`:253-262`:
  no further connects — `connect` fails — and every peer's `Disconnect`),
  `Close(ctx)` (`:264-272`: every peer closed in order), `Timing`/`SetTiming`
  (`:226-232`), `SetHandleError` (`:234-236`). `openSocket` (`:274-304`)
  has no Go counterpart: the `exchange.Manager` is the one socket;
  `NewUnsecuredSession(addr, params)` is the "open".
- `peer.Peer` (`Peer.ts:64-674`): built by the set from a descriptor
  (`:95-188`): its `Service` is `names.Service(cfid, nodeID)` of the
  fabric's compressed fabric id (`:108-117`;
  `handshake.Fabric.Identity.CompressedFabricID`); reachable at once when an
  operational address is known (`:120-122`); on a service change the TXT
  keys SII/SAI/SAT/T/ICD are cleared when a TXT record is present and
  then overlaid by the record's parsed data (`:126-135`), and the address
  check is scheduled (`:138`, §4); on a session added the operational
  address follows the session's address (`:141-166`, UDP branch only),
  the session's reported parameters replace the descriptor's
  (`:176-178`), and the session is dropped from the peer when it closes
  (`:169-174`). `Address()`, `Descriptor()` (a copy), `Sessions()`,
  `Service()`, `HasSession()` (`:571-573`: one neither closing nor
  peer-lost), `NewestSession()` (`:581-601`: highest `ActiveTimestamp`
  among live sessions), `SessionParameters()` (`:281-317` without the
  BasicInformation and LIT branches: reported overlaid; idle/active/
  threshold from the reported else the discovery data; the `exchange.PeerParameters`
  fallbacks below), `TimeOffline()` (`:340-346`: since the status's
  `ConnectionInitiatedAt`, 0 when none).
- `Peer.Connect(ctx, ConnectOptions) (*exchange.Session, error)`
  (`:417-491`): loop — a live newest session is returned; else the shared
  connection process starts if none runs (`#initiateConnection`
  `:628-673`: one per peer, aborted when any session is added
  `:639`, its kicker throttled to `KickThrottleInterval` with suppressed
  emits skipped `:645-648`) or is kicked when `Kick` is set (`:630-633`);
  the wait is bounded by `ConnectionTimeout` (default
  `DefaultConnectionTimeout`; negative = unbounded; ≤0 = at once; unless
  `Kick`, the remaining time is the timeout minus `TimeOffline`,
  `:466-475`) and ends in `PeerUnreachableError{TimeOffline}`
  (`PeerCommunicationError.ts:31-38`, transient `:19`) — then the loop reads
  the session the process produced. `Kick()` (`:500-502`).
  `Disconnect(ctx, cause error)` (`:511-527`: abort the process and wait;
  with a cause force-close every session of the address through the
  manager's `HandlePeerLoss`; without, leave them). `Delete(ctx)`
  (`:532-541`: close, then delete the resumption record and the stored
  descriptor). `Close(ctx)` (`:546-565`: stop observing, abort, wait for
  the monitor's in-flight run, graceful close of each session
  (`CloseSession` on the wire via `Manager.CloseSession`), wait for
  workers, close the service, leave the set).

## 3. The connection process (`PeerConnection.ts:93-753`)

One goroutine group per process; state as `:129-177`. Mirror the rules
in the doc block `:57-92` and the code:

- Start: `service.Status().Connecting(result)` (`:124`), the scheduler
  (`:220-262`), the fallback retirer (`:275-303`), `maybeAttemptFallback`
  (`:363-379`); then consume `service.AddressChanges(ctx)` (`:189-200`):
  add → `addAddress` (`:341-358`: skip an address already attempted —
  and if it was the fallback, it is now real: clear the fallback flag and
  kick with origin discover `:348-352`), delete → `deleteAddress`
  (`:417-440`) then `maybeAttemptFallback`. When the stream ends: mark
  reachable if a session came (`:202-205`), cancel everything, wait, and
  return the session, the fatal error, or nil when aborted (`:207-215`).
- Pending addresses are a heap ordered by `CompareDesirability` (`:145-157`;
  the transport tie-break does not apply). The scheduler waits for a
  pending address, then — if the last attempt started less than
  `DelayBeforeNextAddress` ago — sleeps the remainder, woken early by an
  add, a delete or a reschedule, and re-evaluates (`:233-254`); else
  pops the best and `initiateAttempt`s it (`:256-260`). Attempts run in
  parallel; a new one does not cancel the old (`:85-86`).
- `maybeAttemptFallback` (`:363-379`): only when no attempt, no pending
  and no discovered address; the descriptor's operational address becomes
  the fallback. `retireStaleFallback` (`:275-303`): when discovered
  addresses exist and none equals the fallback (`fallbackSuperseded`
  `:310-320`), wait `AddressChangeStabilizationDelay`, re-check, then drop
  the fallback with force, clear `lastAttemptAt` (no stagger for the
  replacement) and reschedule. `deleteAddress` (`:417-440`): the
  operational address stays alive as fallback when it is the last attempt
  and not forced; otherwise its attempt is aborted and it leaves the
  queue.
- `connect(address)` (`:469-493`): a non-fallback attempt starting while
  the fallback is attempted aborts the fallback (`:476-484`); then
  `attemptOnce` in a loop until the address is aborted, each failure
  through `handleConnectionError`.
- `attemptOnce` (`:498-651`): `isFallback` = attempting the fallback and
  not the first address (`:520`); a fresh unsecured session with the
  peer's `SessionParameters()` (`:522-526`); `handshake.CASE` with the
  descriptor's CATs, `MaxInitialRetransmissions` unlimited,
  `MaxInitialRetransmissionTime` = `MaxDelayBetweenInitialContactRetries`,
  `InitialRetransmissionTime` = the same when `isFallback` (`:597-603`;
  the TCP validator does not apply); the exchange's `OnSend` with a
  retransmission marks the service unreachable (`:847-855`) and feeds the
  unresponsive latch (`establishmentUnresponsiveDetector` `:815-826`:
  fires once when the count exceeds `mrp.MaxTransmissions` 5, reset at 0
  → `Peer.EstablishmentUnresponsive` hook); `OnReceive` stamps
  `LastReceiptAt` (`:857-859`). On success the session is the output
  and the whole process is cancelled (`:623-625`); a cancellation returns
  quietly (`:627-629`). The kick handler (`:568-594`): ignored below
  `KickMinRetransmissions` or `KickMinRestartSaving`; otherwise, if the
  last restart is older than the cooldown of the origin (`AddressChange`
  for discover, `Connect` for connect, measured from one `lastRestartAt`
  shared by the process), the attempt's local context is cancelled so
  `connect` opens a fresh exchange with a fresh backoff to the same
  address.
- `handleConnectionError` (`:656-752`, TCP branch excluded): a
  `StatusReportError` with general Busy and a delay → that delay plus a
  random 0..`DelayAfterNetworkError` (`:678-680`); protocol
  NoSharedTrustRoots **and** a resumption record was deleted → retry at
  once (`:681-685`); any other status report → `DelayAfterPeerError`
  (`:686-689`); `ErrTransient` → `DelayAfterNetworkError` (`:690-692`);
  else `DelayAfterUnhandledError` (`:693-696`). `ErrNetwork` → service
  unreachable (`:700-702`). `HandleError` (option over set) is consulted
  only when a delay was set, may replace it, and an error it returns is
  fatal for the process (`:704-717`). Then sleep the delay under the
  address's context (`:745-751`). Log at the levels of `:720-743`.

## 4. The address monitor (`PeerAddressMonitor.ts:43-293`)

`Peer` owns one, created on first use (`Peer.ts:603-614`). `Schedule()`
(`:77-84`: nothing without a live session; restart the stabilisation
timer, `AddressChangeStabilizationDelay`); `Stop()`; `Close()` (`:97-100`,
waits for an in-flight run); `VerifyReachability(ctx, reason) (bool,
error)` with reasons `address-change` and `session-suspect` (`:110-127`:
concurrent callers join one run; only an address-change run re-arms the
timer afterwards; a joiner's ctx ending returns false without cancelling
the run). The run (`:129-267`, ICD branch excluded): no live session →
false; session-suspect resets the backoff (`:177-180`); address-change
returns true when the set is empty or still contains the session's
address (`:182-188`), or when the last probe or the session's
`ActiveTimestamp` is younger than the current cooldown (`:189-192`), else
stamps `lastProbeAt`; probe the current address (§5) — success advances
the backoff on address-change (`:213-216`) and returns true; on
address-change walk the other discovered addresses with an address
override, the first that answers becomes the session's address
(`Session.SetAddr`, `:243`; the subscription close at `:246` is stage 3 —
note it) and resets the backoff (`:220-252`); all failed → the session's
`HandlePeerLoss(PeerUnresponsiveError)` (`:258-266`) and false.
Cooldown: `fibCurr` or `Min` (`:270-272`); advance: first → (Min, Min),
then prev+curr capped at `Max` (`:275-286`); reset (`:288-292`).
`Peer.VerifyReachability` (`Peer.ts:621-626`).

## 5. The probe

`ClientInteraction.probe` (`action/client/ClientInteraction.ts:300-360`)
is stage 3's, but the monitor needs it: a `probe(ctx, session,
addressOverride)` in `controller/peer` (unexported, or exported as
`Probe` if the tests want it) that opens an IM exchange on the existing
session — never a new connection (`requireExistingSession` `:321`) —
sends a ReadRequest with no paths and `FabricFiltered` false
(`Read({ fabricFilter: false })` `:331`) with `SuppressPeerLoss` and
`MaxRetransmissions` 2 (`PROBE_MAX_RETRANSMISSIONS`
`PeerAddressMonitor.ts:27`), reads the ReportData (one, no data; answer
its ack as the protocol asks — read how `bridgetest/session.go`
`reports()` completes a read and what the device expects, and what
`ClientInteraction` sends after the last report), and returns true on a
report, false on any error or ctx end (`:345-355`), closing the
exchange either way (`:356-359`).

## Tests and proof

- Unit tests on a fake clock for §1–§4: descriptor save coalescing and
  the `AddKnownPeer` merge table; `Connect` reuse, shared process, timeout
  minus time offline, `PeerUnreachableError`; the process against a fake
  `discovery.Names` fed by injected packets (AE's helpers): discovered
  address connects; fallback-only connects with the max initial interval
  when not first; fallback retired 10 s after discovery supersedes it and
  the replacement starts without stagger; two discovered addresses are
  45 s apart; the operational address stays as fallback when every
  discovered address expires; Busy delay, NoSharedTrustRoots clears the
  record and retries at once, transient → 15 s, other → 2 min,
  `HandleError` override and fatal; kick suppressed below the thresholds
  and within the cooldown, restart otherwise; the monitor: still
  discovered → no probe; cooldown respected and the Fibonacci sequence
  10, 10, 20, 30, 50 … min capped at 24 h; migration to an alternate; all
  failed → peer loss; session-suspect probes the current address only.
- Against the in-process node (AF's helpers): `Set.For(addr).Connect`
  establishes CASE through the process with the node's real operational
  record (the node advertises on `mdns.Zeroconf`; a live-multicast test
  with AE's `!race` tag, plus a fake-`Names` test that injects the node's
  SRV/TXT/AAAA so the path runs under `-race`); a second `Connect` reuses
  the session; the node's CloseSession (a `RemoveFabric` or a node stop —
  find what makes the reference node send one) drops the session from the
  peer and fires `Disconnected`; the probe succeeds on a live session and
  fails on a closed one; `Delete` removes the resumption record.
- `controller/peer/parity_matterjs_test.go`: the timing defaults, the
  error-to-delay table, the kick thresholds, the Fibonacci sequence, the
  merge rules — each row cites its lines.
- `make chiptool-test` at the end (unchanged responder). Coverage floor
  for `controller/peer`; floors of the two packages you touched hold;
  reachability regenerated.

## Report

`notes/briefs/reports/AG.md`: the type map, the lines that differed from
the brief, what you left out and why, the `secure/`/`im/`/`transport/`
gaps found, what stage 2 needs from here, open questions. The owner's
decisions follow in the report file.
