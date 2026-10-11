# Report M — TCP transport: length-framed listener and dialer, transport-neutral connection, session eviction, mDNS `T` flag

Branch `wave5/tcp` on `origin/main` (07d9622). No PR. `make reachability`
after every commit that changed Go code, inventory in the same commit
(unreached stays 5).

| Commit | Item |
| --- | --- |
| c833c26 | Must 1, 2: `transport.Conn`, `transport/tcp`, UDP adapter |
| b764756 | Must 3: `mrp.UsesMRP` |
| ed645bd | Must 2, 3, 4, 5 (bridge side): receive / reply paths on a Conn, TCP listener on the UDP port, eviction, `T` key |
| 041f8bb, 4a53dc2 | Must 5: daemon `--tcp`, harness started with `--tcp false` |
| 247838e | CHANGELOG |
| a02cb78, a7d35b2 | lint fixes, coverage floor |

matter.js read at `9840bb92` (HEAD, 2026-10-11).

## What was built

### `transport.Conn` and the UDP adapter

- `transport/conn.go`: `Conn { Send([]byte) error; Remote() net.Addr; Reliable() bool }`,
  as the brief specifies. It is in `transport`, not `contract`: no host needs it.
- `transport/udp/conn.go`: `(*Listener).Conn(dst) *udp.Conn`, `Reliable() == false`.

### `transport/tcp` (new)

- `frame.go`: `FramingHeaderSize = 4` (TcpChannel.ts:28),
  `DefaultMaxPayloadSize = 64_000` (read in
  `packages/general/src/net/tcp/TcpConnection.ts:11`,
  `DEFAULT_MAX_TCP_MESSAGE_SIZE = 64_000`), `maxReceiveBufferFactor = 2`
  (:32). The assembler ports `#handleData` / `#extractMessages`
  (:202-220, :248-298): the buffer cap is checked before extraction; a zero
  length or a length above `maxPayloadSize-4` closes the connection; frames
  completed before the bad header are still delivered.
- `conn.go`: `Conn` with `Send` (refuses above `MaxMessageSize`, :125-126),
  `Remote`, `Reliable() == true`, `Close`, `Closed() <-chan struct{}`, `Err()`
  (why it closed). Messages of one connection go to the handler in arrival
  order, one at a time; a handler can `release()` its turn before it waits
  for the peer — the same contract as `udp.ServeOrdered`, needed because a
  chunked report waits for the StatusResponse that arrives on the same
  connection. Backpressure instead of drop: at most 64 messages in flight per
  connection, then the reader stops reading.
- `listener.go`: `Listen` (IPv6 dual-stack `[::]:5540` default, `tcp4` with
  `PreferIPv4`), `Serve(ctx, handler, onClose)`, `Close` (closes every
  connection), per-peer-IP cap 3 (TcpTransport.ts:27-28, :81-85), idle timeout
  10 s that stops at the first complete message (:30-31, :89-104).
- `dialer.go`: `Dialer.Dial(ctx, addr, handler)` — interface and one test, for
  brief N.

### MRP over a reliable connection

`mrp.UsesMRP(supportsMRP, reliable)` is Session.ts:355-356. The bridge applies
it per route: over TCP no R flag on replies or reports
(MessageExchange.ts:660), no piggybacked ack (:680-686), no ack obligation for
an inbound R flag (:448-452), so no standalone ack (:666-668, :867-869) and no
retransmission tracking (:752). Message counters and the secure-channel
processing are unchanged.

### Bridge

- `bridge/conn.go`: `receive(ctx, buf, transport.Conn)` is the receive entry.
  `handleDatagram(buf, src)` and the ordered UDP handler are thin wrappers that
  build the UDP adapter. Inside, the route is a `net.Addr`: the UDP path keeps
  its `*net.UDPAddr` and does not change; a TCP connection is a `*connRoute`
  (one per connection, so it has the connection's identity). Every send site
  (`reply.go`, `subscribe.go` reports, the CloseSession farewell, the ack pump)
  goes through `sendVia`, which sends a `*connRoute` on its connection and a
  UDP address through the listener as before.
- Binding: the session a frame decrypts under is bound to the route
  (`bindSessionRoute`, which replaces the plain `sessionPeerAddrs.Store`).
  The UDP rule stays (newest address wins). A session never moves between
  transports, as in matter.js `MessageChannel.ts:140-148` (the channel is
  replaced only UDP-for-UDP). A reused session id binds again because it has a
  new session object.
- Eviction: `handleTCPClose` → `evictConn` → for each session bound to the
  connection: release its status-response waits, drop its exchange reply
  routes and timed deadlines, forget the route, then `SessionRegistry.Close`.
  That is the close for a lost peer: no CloseSession echo, and the
  subscriptions close through the manager's hook. This mirrors
  ExchangeManager.ts:650-665 and Session.initiateForceClose (Session.ts:310-331).
- Over TCP the bridge drops unsecured messages (`ErrUnsecuredOverReliable`)
  and group messages (`ErrGroupOverReliable`) — see "Left out".
- `Config.TCP TCPConfig{Incoming, Outgoing}`. The zero value is off, and the
  bridge then behaves as before. `Start` binds UDP, then TCP on the UDP
  socket's bound port (ServerNetworkRuntime.ts:203-226). If the port was
  auto-assigned (`:0`) and TCP gets `EADDRINUSE`, it closes both and retries,
  up to 10 times (`MAX_PORT_ASSIGNMENT_RETRIES`, :54, :164). `Stop` closes
  the TCP listener with the UDP one. The log lines are `matter.tcp.listening`,
  `matter.tcp.evict`, `matter.tcp.frame` and `matter.tcp.reject`. The package
  doc gained a "Transports" paragraph.
- mDNS: `AnnounceFabric` / `WithdrawFabric` set
  `OperationalServiceConfig.TCPClient = Outgoing`, `TCPServer = Incoming`
  (ServerNetworkRuntime.ts:334-338).

### Reference daemon

`--tcp` (default `true`) is resolved like `NetworkServer.resolveTcpConfig`
(NetworkServer.ts:261-272): `true` → both, `false` / empty → none, otherwise
`incoming` / `outgoing`, comma-separated. A smoke run printed
`matter.tcp.listening addr=127.0.0.1:49364 incoming=true outgoing=true` on the
UDP port.

## Which guard proves it

| Claim | Guard |
| --- | --- |
| framing, malformed lengths, buffer bound | `transport/tcp` `TestEncodeFrame*`, `TestAssembler*` (one-byte chunks, zero length, 0xFFFFFFFF, cap at 2× exactly and +1) |
| constants equal matter.js | `TestFramingConstantsMirrorMatterJS` |
| idle timeout, per-peer cap, ordering + release, close notification, panic recovery, dialer | `transport/tcp/listener_test.go` |
| Conn implementations / reliability | `transport` `TestConnImplementations`, `transport/udp` `TestConnAdapterSendsThroughListener` |
| usesMrp truth table | `transport/mrp` `TestUsesMRP` |
| request over TCP answered on the same connection, no R / A flag, no ack owed, session bound | `bridge` `TestTCPRequestRepliedOnSameConnWithoutMRP` (real listener, established CASE keys from the secure harness) |
| eviction on drop | `TestTCPConnectionDropEvictsBoundSessions` |
| same port + `T=6` | `TestTCPListenerSharesUDPPortAndAdvertisesT` |
| ongoing report over TCP: no R / A, untracked | `TestUnsolicitedReportOverTCPHasNoMRP` (drives `sendReportChunks`) |
| no transport switch, reused id | `TestBindSessionRouteNeverSwitchesTransport` |
| unsecured / group over TCP dropped | `TestReceiveOverReliableDropsUnsecuredAndGroup` |
| `--tcp` semantics | `examples/reference-bridge` `TestParseTCPFlag` |
| UDP path unchanged | full `go test ./bridge/...` unchanged and green (only the four `Tick` send closures in the tests changed type, `*net.UDPAddr` → `net.Addr`) |

Runs (darwin, local): `go test ./transport/... ./bridge/... ./mdns/... ./examples/...` is
green except the known `mdns` `TestBuildOperationalService_EmptyHostName_UsesDefault`.
`make race` for `transport/...` and `bridge/...` is green, and `transport/tcp`
passed `-race -count=10`. `make lint` reports 0 issues; the pinned gofumpt
v0.12.0 lists nothing. Coverage floors: the local `make cover-check` stops at
the known mdns FAIL, so I ran the same report with that one test skipped, and
`coverfloor` passes: `transport/tcp` 90.9 % (new floor 89), `bridge` 83.5 %
(floor 83), `transport` has no statements and is exempt like `secure`.
`make reachability` leaves 5 unreached. `go test ./internal/chiptool/ -run
'TestWorkflow|TestFamil'` passes. The chip-tool suite itself is Linux/CI only
and was not run.

## Left out, and why

- **Session establishment over TCP.** It is owner work (ADR 0019, brief
  not-in-scope). An unsecured message that arrives over TCP is dropped with a
  debug log, so a controller that tries PASE / CASE over TCP sees a handshake
  that never answers. matter.js binds unsecured sessions to the channel too
  (`#sessionsOnChannel` checks `unsecuredSessions`, ExchangeManager.ts:645).
  Nothing here creates one over TCP, so eviction has no unsecured session to
  close yet.
- **Large payloads.** Replies are still chunked to the UDP budget
  (`reportChunkPayloadBudget`). Using the TCP limit needs the session
  parameter `maxTcpMessageSize`, which belongs to the owner.
- **Closing the connection when its last session goes** (ExchangeManager.ts:668-681
  `#closeTcpChannelIfLastSession`, TcpTransport.ts:47-48). The bridge does not
  see every session close (fabric removal, reaper and NOC rotation bypass it).
  The idle timer covers only a connection's first message. A connection whose
  sessions were closed by those other paths stays open until the peer closes
  it.
- **MESSAGE_TOO_LARGE status before closing** — matter.js has it as a TODO too
  (TcpChannel.ts:268).
- **Dialer registry and dedupe** (TcpTransport.ts:149-169, 197-210): the brief
  asked for the interface and one test.
- **`T` on the commissionable record.** The brief says the operational and
  commissionable records carry `supportedTransports`. matter.js emits `T`
  only for operational (`MdnsAdvertisement.ts:205`
  `if (this.description.tcp !== undefined && this.isOperational())`), so it
  stays operational-only here.
- **Subscription re-establishment (ADR 0008) over a TCP-bound session.**
  `case_initiator.go` `peerLink` accepts only a `*net.UDPAddr`, so it opens
  CASE over UDP instead. This is unchanged behaviour.
- **A write deadline on TCP sends.** None is set, and matter.js has no
  timeout I could cite. A peer that stops reading blocks the sender until the
  connection is closed.

## Open questions for the owner

1. **The daemon default contradicts the harness PICS.** The brief asks for
   `--tcp` default on, so `T=6` is advertised. The PICS have
   `MCORE.SC.S.TCP=0` (`internal/chiptool/testdata/reference-bridge.pics:43`),
   and TC-SC-4.3 (golden: 14 steps, 0 skipped) fails when the `T` key and the
   PICS disagree (chip 6170af84
   `src/python_testing/mdns_discovery/utils/asserts.py:1134-1135`). Setting
   the PICS to 1 would turn on SC/8.1–8.7, which need CASE over TCP. I kept CI
   unchanged by starting the daemon with `--tcp false` in
   `internal/chiptool/bridge_test.go` (launcher code, not a table). Should the
   default flip, or the PICS move with the owner's CASE-over-TCP work (brief
   O)?
2. **Brief vs matter.js on binding.** The brief says a session is bound to the
   connection its frame arrives on. matter.js binds a session to the channel
   it was established on and never swaps UDP for TCP (MessageChannel.ts:140-148).
   I implemented "first authenticated route wins, no transport switch". This
   meets the brief for the tested case (a session first seen over TCP). A
   UDP-established session whose later frames arrive over TCP is answered on
   TCP but stays bound to UDP, so it is not evicted and its reports go over
   UDP. Confirm.
3. **Brief vs matter.js on the idle timeout.** The brief calls it a
   "first-byte" timeout. matter.js stops the timer on the first complete
   message (`onMessage`, TcpTransport.ts:98-103), so a peer that sends a
   partial frame is still closed after 10 s. I mirrored matter.js.
4. **The seam.** The brief asked to inject an established session's frames
   "through the bridge seam (`internal/bridgeseam`)". I used the in-package
   secure harness (`secure_harness_test.go` keys and ids) with a real TCP
   connection to the started bridge. That covers the real listener, and it
   needs no new exported test affordance. If an external `bridgetest` path is
   wanted, it is a new seam function.
5. **The `T` key when TCP is off.** matter.js omits `T` when no TCP is
   configured (MdnsAdvertisement.ts:205). We keep emitting `T=0`, which the
   CSA assert accepts. The mdns package is unchanged; is this a findings
   entry?

## by_design / findings entries for the owner

- by_design candidate — **per-connection backpressure bound (64 in-flight
  messages)** and the 16 KiB read chunk: Go-side mechanics of async
  iteration over the socket (TcpChannel.ts:81-82); no matter.js constant.
- by_design candidate — **the bridge-internal route is a `net.Addr`**
  (`*net.UDPAddr` for UDP, `*connRoute` for a connection) rather than threading
  `transport.Conn` through every signature, so the UDP path and its tests keep
  their exact shape; the receive entry itself takes a `transport.Conn`.
- findings candidate — **unsecured messages over TCP are dropped** (until CASE /
  PASE over TCP lands; closes when the owner's work does).
- findings candidate — **no connection close when the last session on it goes**
  (ExchangeManager.ts:668-681).
- findings candidate — **`T=0` emitted when TCP is off** where matter.js omits
  the key (MdnsAdvertisement.ts:205-206).
