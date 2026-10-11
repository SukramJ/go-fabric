# Brief M — TCP transport: length-framed listener and dialer, a transport-neutral connection in the bridge, session eviction, mDNS `T` flag (phase 3, ADR 0019)

Owner: Fable. Implementer: Opus. Branch `wave5/tcp`, pushed, **no PR**.

## Sources (read, transcribe, cite file:line; derive nothing)

- matter.js `packages/protocol/src/transport/tcp/TcpChannel.ts` (325 lines):
  each message is prefixed with a 4-byte little-endian uint32 length
  (:37-41); `FRAMING_HEADER_SIZE = 4` (:28), `MAX_RECEIVE_BUFFER_FACTOR = 2`
  (:32, the receive buffer may hold twice `maxPayloadSize` before the
  connection is closed), `maxMessageSize = maxPayloadSize - 4` (:114-116), a
  send above it is a `NetworkError` (:125-126), `DEFAULT_MAX_TCP_MESSAGE_SIZE`
  comes from `@matter/general` (`packages/general/src/…`; read the value
  there and cite it).
- `TcpTransport.ts` (238 lines): `MAX_CONNECTIONS_PER_PEER_IP = 3` (:27-28),
  `NEW_CONNECTION_IDLE_TIMEOUT = Seconds(10)` for the first byte of an
  inbound connection (:30-31), server and client roles in one transport
  with a connection registry (:45-50), "each connection is 1:1 with a
  session; when the session on a connection is closed, the connection is
  closed by that layer" (:47-48).
- `packages/protocol/src/protocol/ExchangeManager.ts:628-655`: a dropped TCP
  connection evicts every session whose channel is that connection and
  closes their exchanges ("Mimics CHIP SDK behavior"); `#sessionsOnChannel`
  (:633-645) matches by channel identity over secure and unsecured sessions.
  Read the `isReliable`/reliable-channel handling in the same file and in
  `packages/protocol/src/protocol/MessageExchange.ts`: over a reliable
  channel MRP retransmission and acknowledgement are not used (cite the
  lines you find; do not assume).
- mDNS: `packages/protocol/src/protocol/DeviceAdvertiser.ts:183-184, :376`
  and `packages/node/src/behavior/system/network/ServerNetworkRuntime.ts:337,
  :361`: the operational and commissionable records carry
  `supportedTransports = { tcpClient: outgoing, tcpServer: incoming }`;
  `NetworkServer.resolveTcpConfig` (`NetworkServer.ts:261-270`): `true` →
  both, `false` → none, else the object. Our `mdns/service.go:205-215`
  already derives the `T` bitmap (bit1 client, bit2 server) from
  `cfg.TCPClient` / `cfg.TCPServer`, citing chip `Advertiser.h:63-64`.
- The TCP listener runs on the same port as UDP (`ServerNetworkRuntime.ts:203-214`,
  `listeningPort: ipv6Interface.port`, retrying another port when taken).
- This repo: `transport/udp/listener.go` (`New`, `Serve(ctx, handler)`,
  `Send(dst, payload)`, `Close`), `transport/message` (header codec),
  `transport/mrp`, `bridge/bridge.go:1587 handleDatagram(buf, src *net.UDPAddr)`,
  every reply path sends with `listener.Send(dest *net.UDPAddr, …)`
  (`bridge/ackpump.go:354, :479`, `bridge/case_initiator.go:361-419`,
  `bridge/reply*.go`). `secure/channel.Session` has no channel binding
  today. `bridge/bridge.go`'s package doc lists every port in call order.

## Must
1. `transport/tcp` (new): `Listener` (accept loop, per-connection reader
   with the 4-byte LE length framing, the receive-buffer bound, the
   first-byte timeout, the per-peer-IP connection cap), `Dialer`
   (outgoing connection with the same framing; used by brief N's OTA path
   later — interface and one test only), `Conn` with `Send`, `Remote`,
   `Close`, `Closed()` notification. IPv6 first like `transport/udp`.
   Default max message size from `DEFAULT_MAX_TCP_MESSAGE_SIZE`.
2. A transport-neutral connection in the bridge: a small interface in
   `transport` (or `contract` if a host must see it — then stdlib only):
   `Conn { Send([]byte) error; Remote() net.Addr; Reliable() bool }`; a UDP
   adapter (listener + `*net.UDPAddr`, `Reliable() == false`) and the TCP
   `Conn` (`Reliable() == true`). The bridge's receive entry takes a `Conn`
   instead of `*net.UDPAddr`; every reply goes to the `Conn` the request
   arrived on. Keep `handleDatagram(buf, src)` as a thin wrapper for the
   UDP path so existing tests stay valid.
3. MRP over a reliable connection: no retransmission, no standalone acks,
   as matter.js does for a reliable channel (cite). Message counters and
   the secure-channel processing are unchanged.
4. Session ↔ connection binding and eviction: when a frame arrives on a TCP
   `Conn`, the session it decrypts under is bound to that `Conn`; when the
   `Conn` closes, every bound session (secure and unsecured) is closed with
   its subscriptions and exchanges, as `ExchangeManager.ts:628-655`.
5. Reference daemon: TCP listener on the Matter port next to UDP,
   configuration `incoming` / `outgoing` with matter.js's `resolveTcpConfig`
   semantics (default both on, as `true` resolves), and the mDNS `T` flag
   set from it (`mdns.Config.TCPServer/TCPClient`). Logging as the UDP path.
6. Tests: framing round trip and malformed lengths, the buffer bound and
   first-byte timeout, per-peer cap, a CASE-established session over UDP
   whose subscription is then driven over TCP? — no: session establishment
   over TCP is the owner's (secure/). Test with an already-established
   session's frames injected over a TCP `Conn` through the bridge seam
   (`internal/bridgeseam`), eviction on close, and `make race` for
   `transport/tcp`, `bridge`, `transport/mrp`. CHANGELOG; reachability after
   every commit; floors for the new package.

Not in scope: `secure/`, `commissioning/`, session parameters (`supportedTransports`,
`maxTcpMessageSize` — owner), CASE/PASE over TCP (owner), BDX and OTA (brief N),
`cluster/`, the harness tables (brief O). Where the bridge's wiring order
matters, read `bridge/bridge.go`'s package doc first and keep the noop-port
pattern.

## Guards
`go test ./transport/... ./bridge/... ./mdns/... ./examples/...`, `make race`
for the packages touched, `make lint` (CI pins gofumpt v0.12.0:
`go run mvdan.cc/gofumpt@v0.12.0 -l .`), `make cover-check`,
`make reachability`, `go test ./internal/chiptool/ -run 'TestWorkflow|TestFamil'`.
Report in `notes/briefs/reports/M.md` (what was built, which guard proves it,
what was left out and why, every open question, by_design / findings entries
for the owner).
