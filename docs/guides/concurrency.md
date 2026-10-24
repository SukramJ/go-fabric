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
| Datagram handlers | the read loop | one per datagram, or per session (below) | returns; `bridge.Stop` waits for them, bounded by its context |
| TCP accept loop and one reader per connection | `bridge.Start` with `Network.TCP` | per message | `bridge.Stop` (readers and their handler calls waited for, bounded by its context) |
| ACK pump | `bridge.Start`, when an ACK tracker is attached | every 50 ms | `bridge.Stop` |
| Subscription engine tick | `node.New` | every 250 ms | `Node.Stop` |
| One report per due subscription | the engine tick | at most one at a time per subscription | returns |
| Session reaper | `node.New` | sweep every minute; evicts sessions idle for 5 minutes | `Node.Stop` (and waited for) |
| PASE and CASE handler reapers | `node.New` | sweep every minute; evict handlers idle for 30 s | `Node.Stop` |
| Operational-time keeper | `Node.Start` | every minute | `Node.Stop` (and waited for) |
| DNS-SD re-announce loop | `Node.Start`, when the advertiser is an `mdns.Reannouncer` | 30 minutes by default | `Node.Stop` |
| Session-triggered re-announce | a session event | once, bounded by the advertise timeout | `bridge.Stop` (cancelled and waited for) |
| BDX transfer | a peer's BDX init on a secure session | one per exchange | `bridge.Stop` (cancelled and waited for, bounded by its context) |
| BDX log transfer | a RetrieveLogsRequest answered over BDX | one at a time | `bridge.Stop` (DiagnosticLogs' `MatterQuiesce` cancels it and waits) |
| Binding manager | `node.New` | on change | `Node.Stop` |
| Client stack socket | `node.New` with `Config.Client.Enabled` | per datagram | `Node.Stop` |

`node.New` starts the engine and the reapers before `Start`. Call `Stop`
on every node `New` returned, even one you never started.

Every cadence in the table runs on one clock: `node.Config.Clock`
(`bridge.Config.Clock` for a host that builds the bridge itself), a
[`clock.Clock`](https://github.com/SukramJ/go-fabric/blob/main/clock/clock.go)
— the module's one time port, `clock.System` when unset. No production
code outside the `clock` package reads the time package's clock
(`clock/noraw_test.go` fails the build on one), so a test that passes a
[`clocktest.Fake`](https://github.com/SukramJ/go-fabric/blob/main/clock/clocktest/fake.go)
moves the ACK pump, the engine tick, the reapers, the commissioning window
and the cluster timers only when it calls `Advance`. `AfterFunc` callbacks
run in their own goroutine on either clock. The node's assembler hands the
same clock to the Identify server of every bridged endpoint
(`endpoint.Config.Clock`) and to the Aggregator's; a device the host builds
with the device layer takes it when the Aggregator adds the device.

[`fabrictest`](https://github.com/SukramJ/go-fabric/tree/main/fabrictest)
runs whole nodes and the module's controller on one `clocktest.Fake` and an
in-memory network, and moves the clock only while no datagram is in flight,
no host holds counted work in hand and no goroutine of the process is
working — the
[Testing in process](testing-in-process.md) guide.

*Source: `transport/udp/listener.go:158-229`, `bridge/bridge.go:808-864`, `bridge/ackpump.go:18-24`, `bridge/ackpump.go:291-314`, `im/subscription/manager.go:185-186`, `im/subscription/manager.go:230-236`, `im/subscription/engine.go:74-95`, `secure/operational/manager.go:231-235`, `secure/operational/manager.go:1157-1172`, `node/security.go:421-468`, `node/node.go:37-39`, `node/node.go:567-575`, `node/reannounce.go:15-31`, `node/config.go:231-235`, `bridge/securechannel.go:1265-1299`, `bridge/bdx_responder.go:141-158`, `binding/manager.go:255-261`, `node/client.go:115-125`, `node/node.go:111-125`, `clock/clock.go`, `node/config.go` (`Config.Clock`), `endpoint/assembler.go` (`Config.Clock`), `node/root.go` (`buildAggregatorClusters`), `device/options.go` (`Options.Clock`), `fabrictest/site.go` (`Run`).*

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

*Source: `bridge/session_order.go:14-69`, `transport/udp/ordered.go:13-126`, `transport/udp/listener.go:38-47`, `transport/udp/listener.go:233-249`, `transport/tcp/doc.go:24-35`.*

## What a host callback may do

The module calls host code from its handler goroutines.

**The rule, for every facade callback** — `OnChanged` (typed and
`device.Device.OnChanged`), `OnWrite`, `Handle<Command>` and a
declaration's host ports: it runs inline, on the goroutine of the event
and before the response leaves; the `…Async` variant (`OnChangedAsync`)
queues it on the device's own goroutine instead, in order, drained when
the node removes the device or stops. A veto, a handler and a port answer
the request, so they have no queued form: one that may block hands its
work to the host's own goroutine and answers.

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

**The other host ports** — DeviceEnergyManagement's `DemManager` hooks,
the media servers' ports, Messages' `MessagePresenter`, SoilMeasurement's
source — run the same way: with the server's state lock released, so they
may call that server's setters and reads. Commands of the server wait
behind a port that runs, so a port must not issue one; what the command
records after the port returns stands over what the port set meanwhile.
WindowCovering's `Mover` and MicrowaveOvenControl's `Oven` run behind the
command queue alike.

**`node.Config.Observers`** run synchronously on the event's goroutine. For
fabric events, that is inside the command that caused the event, before
its response is sent, so a blocking observer delays the response. Hand
network work to your own goroutine. An observer's error or panic is logged
and never returned.

**`device.Device.OnChanged` callbacks** run on the server's own change
notification, with the change's origin (`contract.Change.Origin`: the
host's `Set`, a controller's write, a command). Keep them short — the
quickstart only logs — or use `OnChangedAsync`.

**Controller `SubscribeRequest.Updated`** runs on the goroutine that reads
the report, inside the report's exchange. The priming report is delivered
inside `Subscribe`, before it returns. The reference controller hands each
report to a channel.

**`client/discovery`** runs one goroutine per `Names` that holds all
cache, service and scanner state. User callbacks never run on that loop.

*Source: `endpoint/dispatcher.go:364-389`, `bridge/session_order.go:55-69`, `bridge/receive.go:185`, `bridge/ackpump.go:95-105`, `bridge/ackpump.go:291-295`, `transport/mrp/ack.go:50-55`, `node/security.go:314`, `im/subscription/engine.go:74-95`, `cluster/onoff/server.go:54-72`, `cluster/onoff/server.go:734-753`, `node/observers.go:4-21`, `device/lifecycle.go` (`OnChanged`, `OnChangedAsync`), `device/queue.go`, `client/interaction/handler.go:126-138`, `client/interaction/subscribe.go:180-200`, `examples/reference-controller/cli/interact.go:289-330`, `client/discovery/doc.go:21-30`.*

## Changing the topology

`Aggregator.Add` and `Remove` reassemble a running node. Each reassembly
takes a fresh snapshot of the set and swaps the new topology in
atomically, so an in-flight request always sees one consistent topology.

Two reassemblies that run at the same time are not ordered: the last one to
finish wins, even when it took its snapshot first. Call `Add`, `Remove`
and `MarkComplete` from one goroutine, or serialise them yourself.

The `Snapshotter` of a host with its own topology is called at `Start` and
on every `Reassemble`, and its result is never cached.

*Source: `node/aggregator.go:95-262`, `node/aggregator.go:345-353`, `bridge/bridge.go:908-933`, `bridge/bridge.go:218-232`.*

## Contexts and shutdown

| Call | Context use |
| --- | --- |
| `node.New` | takes none. Its goroutines run under the node's lifetime, which `Stop` cancels |
| `Node.Start(ctx)` | used for the start-up work (assembly, loading the stored fabrics). A cancelled `ctx` fails `Start` |
| `Node.Run(ctx)` | starts, waits for `ctx` to end, then stops with a fresh 5-second budget |
| `Node.Stop(ctx)` | bounds the bridge's wait for its read loop. It closes the sessions with a CloseSession each, stops the engine and reapers, withdraws the mDNS records and closes the advertiser |

`Stop` waits for the read loop and the datagram handlers it dispatched,
the TCP loop with its connection readers and their handler calls, the ACK
pump, the session re-announce, the BDX responders, the reapers and the
operational-time keeper. The waits for handlers and responders are bounded
by `ctx`: their context is cancelled first, and one that does not return
— host code that ignores its context — is waited for only until `ctx`
ends, so a server call can still return after `Stop` has. Close the
database only after your own servers have returned, or make them tolerate
a closed handle.

A deadline the stack sets for its own work — a store write, an mDNS
publish, a CASE connect, a subscription's re-establishment, `Node.Run`'s
stop budget — runs on the node's clock (`clock.WithTimeout`): on the system
clock it is `context.WithTimeout`, under a test's fake clock it expires when
the fake is advanced.

*Source: `node/node.go:131-148`, `node/node.go:616-712`, `node/node.go:723-788`, `bridge/bridge.go:1695-1801`, `bridge/bridge.go:1995-2002`, `transport/udp/listener.go:178-251`.*
