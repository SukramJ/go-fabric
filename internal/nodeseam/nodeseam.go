// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package nodeseam carries the one-way seams to a node's configuration
// that are not public API: fabrictest puts a node's client stack onto its
// in-memory network, and the reference daemon and the module's tests set
// the TESTING ONLY knobs — the OTA requestor's harness mode, the Wi-Fi
// NetworkCommissioning driver (review 2026-11 W5-api-11) and the
// SpecificationVersion a node reports (phase 32).
//
// Direction of the edge: package node installs [UseClientListener] from
// its init; fabrictest reads it. node.Client names a bind address and a
// DNS-SD cache, which a host supplies; the socket itself is the node's own
// (udp.New), a kernel socket a site cannot carry. The seam hands the
// client stack a transport/udp.Listener over a socket of the site instead,
// as bridgeseam.UseListener does for the server socket, without a second
// exported way to build the stack.
package nodeseam

import (
	"github.com/SukramJ/go-fabric/transport/udp"
)

// UseClientListener makes the node built from cfg (a *node.Config, as any:
// node imports this package to install the hook) open its client socket
// with open instead of udp.New. open is called once per client stack, at
// node.New; the client stack closes what it returns on Stop. Reports false
// when cfg is not a *node.Config or open is nil.
var UseClientListener func(cfg any, open func() (*udp.Listener, error)) bool

// UseOTAHarness puts the OTA requestor of cfg (a *node.Config whose OTA is
// set) into harness mode: an image is applied in place and its version
// reported as running without a restart, with the user consent the
// harness gives. TESTING ONLY. Reports false when cfg is not a
// *node.Config or has no OTA.
var UseOTAHarness func(cfg any) bool

// UseSpecificationVersion makes the node built from cfg (a *node.Config)
// report version as its BasicInformation SpecificationVersion instead of
// the release the module implements. TESTING ONLY: a test stands in for a
// node of an older release, as matter.js's IcdClientTest sets
// basicInformation.specificationVersion (IcdClientTest.ts:567-582 at
// 862ad75f). Reports false when cfg is not a *node.Config.
var UseSpecificationVersion func(cfg any, version uint32) bool
