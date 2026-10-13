# Report AG — `controller/peer`: peers, the connection process and the address monitor (ADR 0020, stage 1, part 3)

Branch `wave13/peer` from `planning/briefs-wave-13` (c438293), pushed, no
PR. matter.js read at `0ceb202d` (`git -C ../matter.js show 0ceb202d:…`);
every `// Mirrors matter.js …` comment added cites `0ceb202d` lines that were
read. Each commit carries its regenerated reachability inventory (unreached
stays 5) and a CHANGELOG entry under `[Unreleased]` → Added.
`notes/parity/by_design.md`, the findings register, `secure/`, `transport/`,
`mdns/`, `bridge/`, `node/` and `im/` are untouched.

| Commit | Item |
| --- | --- |
| 79c2c21 `feat(controller/exchange): RetransmissionCount and RetransmissionRestartSaving` | §0 |
| 9075d44 `feat(controller/exchange): OnSend/OnReceive notifiers and WithHooks` | §0 |
| eac8faa `feat(controller/exchange): AddressOverride, Session.SetAddr, Session.ActiveTimestamp` | §0 |
| 1ff195b `feat(controller/exchange): session peer loss and Manager.HandlePeerLoss` | §0 (+ the `handshake` fabric-index fill) |
| 48804d7 `feat(controller/exchange): peer-initiated Secure Channel exchanges, CloseSession from the peer` | §0 |
| 9a0bf1f `feat(controller/discovery): Service.Status (IpServiceStatus) and Service.Changes` | §0 |
| 8ffb183 `chore(controller): lint findings of the §0 additions` | §0 (lint only) |
| 458b356 `feat(controller/peer): Descriptor, Store, ResumptionStore, Timing` | §1 |
| 5bc8d66 `feat(controller/peer): Set and Peer` | §2 |
| 0b9af6d `feat(controller/peer): the connection process, Peer.Connect and Kick` | §3 |
| b251771 `feat(controller/peer): the liveness probe` | §5 (before §4, which uses it) |
| 401541b `feat(controller/peer): the address monitor and VerifyReachability` | §4 |
| (this commit) `notes: report AG` | — |

Coverage floors: `controller/peer` 93 (measured 94.7 %); `controller/exchange`
93 holds (94.7 %), `controller/discovery` 95 holds (95.5 %),
`controller/handshake` 86 holds (86.6 %).

## Type map

| matter.js `0ceb202d` (`packages/protocol/src/` unless named) | Go |
| --- | --- |
| `peer/PeerDescriptor.ts` `PeerDescriptor`, `ObservablePeerDescriptor` | `peer.Descriptor` (the observable half is `Peer`'s locked descriptor and its save) |
| `peer/PeerTimingParameters.ts` `PeerTimingParameters`, `.defaults`, `.merge` | `peer.Timing` (+ `KickRestartCooldown`, `ProbeCooldown`), `DefaultTiming`, `Merge` |
| `Peer.updated` → node layer persistence | `peer.Store`, `MemoryStore`, `Context.Updated` |
| `SessionManager` resumption records (`deleteResumptionRecord` :468-480) | `peer.ResumptionStore` (= `handshake.ResumptionStore` + `DeleteResumptionRecord`), `MemoryResumptionStore` |
| `peer/PeerSet.ts` `PeerSet`, `PeerSetContext` | `peer.Set`, `peer.Context`, `NewSet` |
| `PeerSet.#adoptSession` + `operationalAddressOf` | `Set.Adopt` |
| `PeerSet.disconnected` | `Context.Disconnected` |
| `peer/Peer.ts` `Peer` | `peer.Peer` |
| `Peer.ConnectOptions` | `peer.ConnectOptions` |
| `Peer.establishmentUnresponsive` | `Context.EstablishmentUnresponsive` |
| `PeerConnection.Context.handleError` | `peer.ErrorHandler` (`Context.HandleError`, `Set.SetHandleError`, `ConnectOptions.HandleError`) |
| `peer/PeerConnection.ts` `PeerConnection` | unexported `connection` (`Peer.initiateConnection`, `run`, `scheduleAttempts`, `retireStaleFallback`, …) |
| `PeerConnection.establishmentUnresponsiveDetector` | `unresponsiveDetector` |
| `KickOrigin` | unexported `kickOrigin` |
| `peer/PeerAddressMonitor.ts` `PeerAddressMonitor` | unexported `monitor`; `Peer.VerifyReachability` |
| `protocol/ExchangeProvider.ts` `ReachabilityReason` | `peer.ReachabilityReason` (`ReasonAddressChange`, `ReasonSessionSuspect`) |
| `action/client/ClientInteraction.ts` `probe` | unexported `probe` |
| `peer/PeerCommunicationError.ts` `PeerUnreachableError` | `peer.UnreachableError` (revive's stutter rule, as `TXTData` in AE) |
| `PeerCommunicationError.ts` `PeerInitiatedCloseError` | `exchange.PeerInitiatedCloseError` |
| `protocol/MessageExchange.ts` `retransmissionCount`, `retransmissionRestartSaving` | `exchange.Exchange.RetransmissionCount`, `RetransmissionRestartSaving` |
| `MessageExchange.Options` `onSend`, `onReceive`, `addressOverride` and the `onSend`/`onReceive` setters (:351-369) | `exchange.Options.OnSend`, `OnReceive`, `AddressOverride`; `exchange.Hooks` / `exchange.WithHooks` |
| `session/Session.ts` `activeTimestamp`, `closing`; `session.channel.networkAddress = …` | `exchange.Session.ActiveTimestamp`, `Closing`, `SetAddr` |
| `session/NodeSession.ts` `handlePeerLoss`, `isPeerLost`, `handlePeerClose`, `peerAddress` | `exchange.Session.HandlePeerLoss`, `IsPeerLost`, `FabricIndex`; the CloseSession handler |
| `session/SessionManager.ts` `handlePeerLoss`, `sessionsFor` | `exchange.Manager.HandlePeerLoss`, `SessionsFor`, `exchange.PeerAddress` |
| `ExchangeManager.ts` `peerLost` context (:556-579) | `Manager.exchangePeerLost` (unexported) |
| `securechannel/SecureChannelProtocol.ts` `StatusReportOnlySecureChannelProtocol` | `Manager.handleSecureChannel` (unexported) |
| `general/src/net/dns-sd/IpServiceStatus.ts` | `discovery.Status` (`Service.Status`) |
| `IpService.changed` | `discovery.Service.Changes` |

## Guards

- `make ci`: exit 0 — build, vet, `go test -count=1 ./...` (every package
  `ok`, 175 `ok` lines), gofumpt + golangci-lint (0 issues), `go mod tidy
  -diff`, `go mod verify`, cover-check (177 packages at or above their
  floor).
- `make race`: exit 0 on the second run (175 packages `ok`, no race
  reported). The first run failed in `bridge`
  `TestOngoingReportWaitsForEachChunksStatusResponse` ("an unanswered
  chunked report sent 0 chunks"), a timing test in an untouched package;
  it passed 10 of 10 runs alone under `-race`, and the rerun of `make race`
  was green.
- `make reachability-check`: exit 0 — 10 729 exported, 7 517 reached,
  3 207 whitelisted, unreached stays **5**, none in `controller/`.
- `make chiptool-test` (once): exit 0, `ok internal/chiptool 37.2s`, 311
  PASS and 0 FAIL; `TestChipToolSuite` passed (287 subtests) and so did
  `TestChipToolCommissionsReferenceBridge`. `TestMatterJSController`
  skipped for its environment reason (no built matter.js at
  `.claude/worktrees/matter.js`, the sibling path of the worktree).
- Parity: `controller/peer/parity_matterjs_test.go` — the timing defaults
  and `merge` (PeerTimingParameters.ts:153-209), the error-to-delay table
  (PeerConnection.ts:674-696 with SecureChannelMessenger.ts:44-50), the
  unresponsive latch (:815-826), the Fibonacci probe cooldown
  (PeerAddressMonitor.ts:269-292), the known-peer merge rules
  (PeerSet.ts:315-337). Kick thresholds are rows of the defaults table and
  are exercised by `TestKickRestartsTheExchange` / `TestDiscoverKick`.
- Against the in-process node: `TestConnectToANodeThroughTheProcess`
  (records injected into a real `discovery.Names`, runs under `-race`):
  `Set.For(addr).Connect` establishes CASE through the process, a second
  `Connect` reuses the session, the probe succeeds on the live session, the
  node's CloseSession on `Stop` loses the session, drops it from the peer
  and fires `Disconnected`, the probe then fails, `Delete` forgets the
  resumption record. `TestConnectLiveThroughMulticast` (`!race`) does the
  same over the host's multicast with the node advertising on
  `mdns.Zeroconf`; it passed here (not skipped).

## Lines that differed from the brief (followed the file)

- **`AddressOverride` is an exchange option, not a send option.** matter.js
  keeps it in `MessageExchange.Options` (:1266-1270), stored at :297 and
  used for every transmission of the exchange (:488, :771-774, :953). It
  is `exchange.Options.AddressOverride`.
- **The notifiers' call sites are in `MessageExchange.ts`**, not
  `ExchangeManager.ts`: `onSend(message, 0)` at :768 (every first
  transmission, standalone acks included), `onSend(message, n)` at :943,
  `onReceive` at :469 after the protocol check (duplicates and acks
  included).
- **`retransmissionRestartSaving` reads the last timer whether or not it
  runs** (:1229 `this.#retransmissionTimer?.interval`); it is 0 only before
  any timer was armed. Mirrored; the brief's "0 with no timer pending" is
  that case.
- **`activeTimestamp` of a secure session starts at its creation**
  (`setActiveTimestamp: true`, NodeSession.ts:119, Session.ts:119-121); only
  the unsecured session is zero until a receipt.
- **Peer loss from an exchange now closes sessions.** ExchangeManager.ts:556-579
  sends an exchange-observed loss to `SessionManager.handlePeerLoss` (every
  session of the peer created before the exchange's context, `asOf` :549),
  or to the session alone without a peer address, sparing the observing
  exchange (`currentExchange`, Session.ts:324-327; the session closes when
  that exchange does, NodeSession.ts:354-362). Before, the Go manager only
  called the hook. The `PeerLost` hook is now called once per session that
  becomes lost.
- **The Secure Channel handler:** an initial message other than StatusReport
  or standalone ack is refused at SecureChannelProtocol.ts:43-50
  (`StatusResponseError`), not :59-64 (that check in
  `handleInitialStatusReport` is unreachable from `onNewExchange`). A
  standalone ack with R set is not acknowledged — no standalone ack ever is
  (MessageExchange.ts:519-522); the brief's ":45" ignore holds. A new
  exchange on a closing session is declined without an ack
  (ExchangeManager.ts:386-389).
- **`handshake.CASE` has two `NewSecureSession` literals** (resume and
  full); both got `FabricIndex: c.fabric.Identity.FabricIndex`, the one
  line the brief allows, twice.
- **Busy needs the protocol code Busy too.** `busyDelay` is set only for
  general Busy (8) *and* protocol Busy (4) *and* exactly two bytes of
  protocol data (SecureChannelMessenger.ts:44-50); any other Busy is a peer
  error (5 min).
- **A non-fallback attempt does not always abort the fallback.** `connect`
  deletes the fallback (:476-484) *before* `initiateAttempt` registers the
  new attempt (:396 runs `connect` synchronously, :407 sets it after). When
  the fallback's attempt is then the only one, `deleteAddress` keeps it
  (:422-429) and :483 only unmarks it. Mirrored (the order is kept in
  `initiateAttemptLocked`); the stale fallback is normally retired by
  `retireStaleFallback` first, as the brief's test describes.
- **The kick thresholds are the set's timing**, not the per-call merged
  timing: :569, :573 and :578-581 read `context.timing`; only the throttle
  uses the merged timing (Peer.ts:641-648).
- **`connectionTimeout`:** matter.js uses `Infinity` for unbounded and
  `≤ 0` for "at once" (Peer.ts:466-475). In Go 0 is "unset" (the default),
  so a negative value is unbounded and there is no explicit "at once"; a
  timeout already spent by the time offline returns `UnreachableError` at
  once.
- **After `Set.Disconnect`,** matter.js's `exchanges` getter throws
  "Client networking is not initialized" inside the attempt, which
  `handleConnectionError` treats as unhandled (2 min) — `Connect` ends at
  its timeout, not at once. Mirrored with `peer.ErrNotConnectable`.
- **Save coalescing:** matter.js's `#save` runs synchronously up to the
  `updated` emit (an async function runs to its first `await`), so its
  `#isSaving` guard coalesces nothing in practice — one emit per change. Go
  starts one save worker per burst of changes made before it runs (one
  `AddKnownPeer`, one session adoption), as the brief asks. A save error is
  logged at error level, as `BasicMultiplex.add` logs a rejected worker
  (general/src/util/Multiplex.ts:41-47).
- Line numbers otherwise as the brief states, with these small corrections:
  `PeerUnreachableError` is :28-35; `SessionManager.fabricFor` :404-409;
  `Peer.delete` :529-541; `PeerConnection.Context.handleError` :768-775.

## Where the Go surface differs from the brief's wording

- **`exchange.WithHooks`.** `handshake.CASE` opens its own unsecured session
  and exchange, so the process cannot hand `Options.OnSend`/`OnReceive` to
  `Initiate`, nor see the exchange the kick reads. Without a `handshake`
  change the hooks travel in the CASE context: the exchange adopts them at
  its first `Send`/`Next` (the setter semantics of MessageExchange.ts:351-369)
  and hands itself to `Hooks.Exchange`. See open question 1.
- **`ErrorHandler` returns `(delay, replace, fatal)`** — the brief's
  `(time.Duration, bool)` cannot carry the fatal error.
- **`Set.For` / `AddKnownPeer` return an error** for an unknown fabric
  (matter.js `fabricFor` throws); `Adopt` returns nil then.
- **`Peer.TimeOffline` uses the peer's clock.** matter.js reads
  `service.status.connectionInitiatedAt`; `discovery.Status` stamps it on
  the DNS-SD cache's clock, which a peer test cannot drive, so the peer
  stamps the same instant on its own clock (set and cleared with the
  process). `discovery.Status.ConnectionInitiatedAt` exists as the brief
  asks.
- **Additions outside the brief's list, all in the packages §0 opens:**
  `exchange.Session.Closing` (the peer drops a session when it begins to
  close, Peer.ts:169-174), `exchange.Manager.SessionsFor` and
  `exchange.PeerAddress` (`exchange` cannot import `handshake`, which
  imports it; `handshake.PeerAddress` converts to it), and
  `discovery.Service.Changes` (the service's `changed` observable,
  Peer.ts:126 and PeerConnection.ts:281). `discovery.Names` now owns a
  context its status resolutions derive from (cancelled on `Close`), and
  `scanner.go` carries one `nolint:contextcheck` for the call chain into it.
- **`Peer.Delete` also deletes the stored descriptor**, as the brief asks;
  in matter.js that is the node layer's.
- **The probe's request** is `im.ReadRequest{FabricFiltered: false}` as the
  brief says. Its encoding differs from matter.js's `Read({fabricFilter:
  false})`: `im` always writes the AttributeRequests array (empty here) and
  no InteractionModelRevision, where matter.js omits the array and sends the
  revision (action/request/Read.ts:40-77). The node answers it.

## Left out, and why

- TCP, transport preference, group peers, network profiles, the
  BasicInformation / LIT ICD inputs of `sessionParameters`, the ICD branch
  of the monitor, `RebootResubscribeArmer`, `PeerExchangeProvider`,
  `ClientInteraction` beyond the probe — the brief's scope.
- The subscription close after a migration (PeerAddressMonitor.ts:244-246)
  — stage 3's client interaction; noted at the call site.
- Following `Session.SetAddr` from elsewhere: matter.js observes
  `networkAddressChanged`; here the monitor, the only caller, updates the
  descriptor itself.
- `peer.Store.LoadPeers` has no caller in the package (matter.js loads
  peers in the node layer); a host loads and calls `AddKnownPeer`.

## `secure/`, `im/`, `transport/` gaps found

- `im/`: no ReportData decoder for a client (the probe reads the two flags
  itself, as `bridgetest` does), and the ReadRequest encoding difference
  above. Both are stage 3 material.
- `bridge/` (observation, not changed): the node sends CloseSession on stop
  only for sessions it has routed a message of (`notifyFabricSessionsClosing`
  / the shutdown path keyed by `sessionPeerAddrs`); a CASE session that
  never carried a message closes silently. The integration test sends one
  probe first.
- Nothing missing in `secure/` or `transport/`.

## What stage 2 needs from here

- Commissioning's operational CASE goes through `Set.Adopt` (or
  `Peer.Connect` once the node advertises) so the peer owns the session;
  its PASE session must stay out of the set (no fabric index — `Adopt`
  ignores it) and keep `SuppressPeerLoss`.
- A persistent `peer.Store` and `peer.ResumptionStore` (the ADR's open
  persistence item), and the CATs a commissioner assigns, recorded in the
  descriptor with `AddKnownPeer`.
- An invoke path over a session (AF's note still stands).
- `Peer.Disconnect(ctx, cause)` after `RemoveFabric` on the peer.

## Open questions

1. **Hooks through the context or through `CASEOptions`?** `exchange.WithHooks`
   avoids touching `handshake`. The alternative is a `CASEOptions.Exchange
   exchange.Options` (plus a way to see the exchange) — a small `handshake`
   change. Keep the context, or move it?
2. **`DeleteResumptionRecord` into `handshake.ResumptionStore`?** It lives
   in `peer.ResumptionStore` because `handshake` was closed to this brief;
   `handshake.MemoryResumptionStore` cannot delete.
3. **`handshake.PeerAddress` as an alias of `exchange.PeerAddress`?** The
   two identical structs convert, but one type would read better.
4. **Exchange-level peer loss now closes sessions** (matter.js's rule). A
   host that relied on the hook alone keeping sessions open sees them
   close. Fine for an `[Unreleased]` package, or worth a CHANGELOG
   "Changed" line?
5. **The probe's wire shape** — should `im.ReadRequest` omit an empty
   AttributeRequests array and send the IM revision, as matter.js does?
6. **`by_design` candidates** (not written, per the rules): `WithHooks` as
   the Go form of the notifier setters; the save coalescing per burst
   versus matter.js's per-change emit; `TimeOffline` on the peer's clock;
   no explicit "at once" connection timeout.
