# Concurrency

The module is concurrent by construction: a host's cluster servers are
called from many goroutines at once, and the session, subscription and MRP
paths run side by side. This page lists the goroutines a running node
owns, what a host callback may do, the ordering the module guarantees, and
how shutdown ends it all. The race detector run (`make race`) is a release
gate for these paths.

## The goroutines of a running node

| Goroutine | Started by | Cadence | Stopped by |
| --- | --- | --- | --- |
| UDP read loop (one) | `bridge.Start` | per datagram | `bridge.Stop` (context cancel, socket close) |
| Datagram handlers | the read loop | one per datagram, or per session (below) | returns; not awaited by `Stop` |
| TCP accept loop and one reader per connection | `bridge.Start` with `Network.TCP` | per message | `bridge.Stop` |
| ACK pump | `bridge.Start`, when an ACK tracker is attached | every 50 ms | `bridge.Stop` |
| Subscription engine tick | `node.New` | every 250 ms | `Node.Stop` |
| One report per due subscription | the engine tick | at most one at a time per subscription | returns |
| Session reaper | `node.New` | sweep every minute; evicts sessions idle for 5 minutes | `Node.Stop` |
| PASE and CASE handler reapers | `node.New` | sweep every minute; evict handlers idle for 30 s | `Node.Stop` |
| Operational-time keeper | `Node.Start` | every minute | `Node.Stop` (and waited for) |
| DNS-SD re-announce loop | `Node.Start`, when the advertiser is an `mdns.Reannouncer` | 30 minutes by default | `Node.Stop` |
| Session-triggered re-announce | a session event | once, bounded by the advertise timeout | returns |
| BDX transfer | a peer's BDX init on a secure session | one per exchange | returns |
| Binding manager | `node.New` | on change | `Node.Stop` |
| Client stack socket | `node.New` with `Config.Client.Enabled` | per datagram | `Node.Stop` |

`node.New` starts the engine and the reapers before `Start`. Call `Stop`
on every node `New` returned, even one you never started.

*Source: `transport/udp/listener.go:147-212`, `bridge/bridge.go:784-840`, `bridge/ackpump.go:18-24`, `bridge/ackpump.go:291-314`, `im/subscription/manager.go:184-185`, `im/subscription/manager.go:229-235`, `im/subscription/engine.go:74-95`, `secure/operational/manager.go:223-227`, `secure/operational/manager.go:1145-1160`, `node/security.go:308-355`, `node/node.go:38-40`, `node/node.go:486-494`, `node/reannounce.go:15-31`, `node/config.go:175-179`, `bridge/securechannel.go:1268-1290`, `bridge/bdx_responder.go:93-110`, `binding/manager.go:238-244`, `node/client.go:96-106`, `node/node.go:95-108`.*

## Dispatch and ordering

The read loop copies each datagram and hands it on without blocking.

**Messages of one secure unicast session are handled one at a time, in
arrival order.** The listener keys them by their session id and queues
them, so a Read sent right behind an Invoke sees the Invoke's effect. A
handler that sends and then waits for the peer's answer on the same session
releases its turn first, so the answer is not queued behind it. Examples
are a chunked report waiting for its StatusResponse and the subscription
priming loop. The TCP transport keeps the same contract per connection.

**Everything else runs concurrently:** unsecured messages (the PASE and
CASE handshakes, session id 0) and group messages. Concurrent unordered
handlers are bounded at 1024. A datagram that arrives when every slot is
taken is dropped, and the peer's MRP retransmits it. Per session, at most
256 datagrams queue; beyond that a datagram is dropped too.

Between sessions there is no ordering. Two controllers act on the same
server concurrently.

*Source: `bridge/session_order.go:14-69`, `transport/udp/ordered.go:13-110`, `transport/udp/listener.go:35-44`, `transport/udp/listener.go:196-210`, `transport/tcp/doc.go:24-35`.*

## What a host callback may do

The module calls host code from its handler goroutines.

**Cluster servers** (`MatterRead`, `MatterWrite`, `MatterInvoke`). No
dispatcher lock is held while a server is called. A call runs on its
session's turn, so a slow `MatterInvoke` delays that session's next
message, and other sessions go on. While it runs, the ACK pump sends a
standalone acknowledgement for the request once the standalone-ACK delay
(200 ms) has passed, so the controller does not retransmit. Reads also run on the
subscription engine's report goroutines, at the same time as commands on
other sessions. A server must therefore be safe for concurrent use. The
`ctx` passed to the server ends when the bridge stops.

**OnOff's `Sink`** runs with no lock held that `Server.Set` or a read
takes. It may confirm through `Set`, from its own goroutine or another.
Commands, writes and countdown ticks of that server wait behind it.

**`node.Config.Observers`** run synchronously on the event's goroutine. For
fabric events, that is inside the command that caused the event, before
its response is sent, so a blocking observer delays the response. Hand
network work to your own goroutine. An observer's error or panic is logged
and never returned.

**`device.Device.OnChanged` callbacks** run on the server's own change
notification. Keep them short. The quickstart only logs.

**Controller `SubscribeRequest.Updated`** runs on the goroutine that reads
the report, inside the report's exchange. The priming report is delivered
inside `Subscribe`, before it returns. The reference controller hands each
report to a channel.

**`controller/discovery`** runs one goroutine per `Names` that holds all
cache, service and scanner state. User callbacks never run on that loop.

*Source: `endpoint/dispatcher.go:364-389`, `bridge/session_order.go:55-69`, `bridge/receive.go:185`, `bridge/ackpump.go:95-105`, `bridge/ackpump.go:291-295`, `transport/mrp/ack.go:50-55`, `node/security.go:308`, `im/subscription/engine.go:74-95`, `cluster/onoff/server.go:54-72`, `cluster/onoff/server.go:749-768`, `node/observers.go:4-21`, `device/device.go:643-661`, `controller/interaction/handler.go:126-138`, `controller/interaction/subscribe.go:180-200`, `examples/reference-controller/interact.go:289-330`, `controller/discovery/doc.go:21-30`.*

## Changing the topology

`Aggregator.Add` and `Remove` reassemble a running node. Each reassembly
takes a fresh snapshot of the set and swaps the new topology in
atomically, so an in-flight request always sees one consistent topology.

Two reassemblies that run at the same time are not ordered: the last one to
finish wins, even when it took its snapshot first. Call `Add`, `Remove`
and `MarkComplete` from one goroutine, or serialise them yourself.

The `Snapshotter` of a host with its own topology is called at `Start` and
on every `Reassemble`, and its result is never cached.

*Source: `node/aggregator.go:70-132`, `node/aggregator.go:172-178`, `bridge/bridge.go:868-893`, `bridge/bridge.go:217-231`.*

## Contexts and shutdown

| Call | Context use |
| --- | --- |
| `node.New` | takes none. Its goroutines run under the node's lifetime, which `Stop` cancels |
| `Node.Start(ctx)` | used for the start-up work (assembly, loading the stored fabrics). A cancelled `ctx` fails `Start` |
| `Node.Run(ctx)` | starts, waits for `ctx` to end, then stops with a fresh 5-second budget |
| `Node.Stop(ctx)` | bounds the bridge's wait for its read loop. It closes the sessions with a CloseSession each, stops the engine and reapers, withdraws the mDNS records and closes the advertiser |

`Stop` waits for the read loop, the TCP loop, the ACK pump and the
operational-time keeper. It does not wait for datagram handlers that are
already running. Their context is cancelled, and a server call can return
after `Stop` has. Close the database only after your own servers have
returned, or make them tolerate a closed handle.

*Source: `node/node.go:109-120`, `node/node.go:465-535`, `node/node.go:545-599`, `bridge/bridge.go:1541-1650`, `bridge/bridge.go:1805-1812`, `transport/udp/listener.go:147-212`.*
