// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package fabrictest runs Matter devices and controllers of this module in
// one test process, on an in-memory network and one test clock, so a test
// commissions a device and talks to it over the wire with no chip-tool, no
// radio and no socket.
//
// It follows matter.js's testing support at the pinned commit
// (packages/node/src/testing): [Site] is MockSite (mock-site.ts) — devices
// and controllers on one simulated network, closed with the site; [Device]
// exposes what MockServerNode does (mock-server-node.ts) — the node, its
// store, its endpoints, local server access ([Device.Act]); the wire
// helpers [Read], [Write], [Invoke], [Subscribe] and [Connect] follow the
// `interaction` namespace of node-helpers.ts; and the site's default clock
// is a [github.com/SukramJ/go-fabric/clock/clocktest.Fake], matter.js's
// MockTime (packages/testing/src/mocks/time.ts).
//
// A test of a device mapping:
//
//	site := fabrictest.NewSite(t, fabrictest.Options{})
//	pair := site.CommissionedPair(fabrictest.PairOptions{
//		Device: fabrictest.Topology{Bridged: []node.Endpoint{myPlug}},
//	})
//	ep, _ := pair.Device.EndpointOf("plug:1")
//	// onoffdef is github.com/SukramJ/go-fabric/cluster/spec/onoff
//	res, err := fabrictest.Invoke(ctx, pair, im.ConcreteCommandPath{Endpoint: ep, Cluster: onoffdef.ClusterID, Command: onoffdef.CmdToggle}, nil)
//
// What runs:
//
//   - A device is a node.Node ([Site.AddDevice]) with its own SQLite
//     stores in the test's temporary directory, an mdns.Noop advertiser and
//     the site's clock (node.Config.Clock). Its bridge serves the
//     production transport/udp.Listener over a socket of the in-memory
//     network, so every datagram it receives, answers or initiates takes
//     the production UDP path. [Device.Restart] starts a new node on the
//     same store and address.
//   - A controller ([Site.AddController]) is the module's controller stack
//     (controller/, ADR 0020): exchange manager, DNS-SD cache, certificate
//     authority, fabric, peer set, subscription registry, commissioner with
//     its CASE responder and client node registry, all on the site's clock.
//     [Controller.CommissionNode] commissions a device through the registry
//     as matter.js controller.peers.commission does, and
//     [Controller.Restart] starts the stack again on its stores.
//   - The in-memory network ([Network]) addresses every host like UDP, by
//     an IPv6 link-local address the site hands out (fe80::<index>, port
//     5540), and can lose, delay (on the site's clock) and duplicate
//     datagrams ([NetOptions]). The site answers the controllers' DNS-SD
//     queries from what the devices publish: the commissionable record
//     while a window is open, the operational record per fabric.
//   - Multicast ([Network.Members], [Device.MulticastGroups],
//     [Network.WatchMemberships], [Network.FailJoins]) follows matter.js's
//     MockNetwork (packages/general/src/net/mock/MockNetwork.ts): a host's
//     production listener joins and leaves IPv6 multicast groups on the
//     in-memory network, which keeps them per host address — past the
//     socket's close, as MockNetwork keeps them on the host — and delivers a
//     datagram sent to a group to every host holding it. A test asserts the
//     groups a node joined, the joins and leaves it made, and injects join
//     failures; the site has one interface per host and routes nothing. A
//     node's group messages (node.Node.Groups' senders) leave through its
//     bridge's socket on the network, as MockUdpSocket.send sends them, so
//     a [Network.Filter] sees each as one datagram to the group and every
//     member receives it.
//   - [Options].Loopback puts every host on a real UDP socket on [::1]
//     instead, for a test that needs sockets.
//
// Time. A fake clock moves only when the test moves it — [clocktest.Fake]
// Advance — or inside [Site.Run] and [Site.Eventually], which every helper
// here uses: while the work has not finished, they advance the clock to
// its next deadline each time the site has been quiet for the settle
// period — no datagram sent or queued, no goroutine of the process
// runnable, running or in a system call — so a timer the work waits on
// fires. That is MockTime.resolve, which moves time only while no task
// is pending. A site given clock.System runs on the wall clock, and Run
// only runs the work.
//
// The package is test support: it is imported from _test.go files, its
// hosts accept the CSA test attestation chain and their credentials are
// fixed and public.
package fabrictest
