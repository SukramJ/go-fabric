// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package fabrictestseam carries the one-way seams into a fabrictest site
// that are not public API: the client machinery a site's controller runs
// on, the layers below a site's device, and a pair's transport. The
// module's own tests drive those layers; a host's test drives the facade —
// fabrictest's wire helpers, a pair's client node, a device's node —
// and reaches none of this (review 2026-11 W5-ex-12, owner decision
// 2026-11-02).
//
// Direction of the edge: package fabrictest installs every hook from its
// init; the module's tests call them with fabrictest's own values (a
// *fabrictest.Device, *fabrictest.Controller or *fabrictest.Pair, as any:
// fabrictest imports this package to install the hooks). A hook called
// with any other value panics.
package fabrictestseam

import (
	"context"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/mdns"
)

// The device seams: d is a *fabrictest.Device.
var (
	// Advertiser is the device's DNS-SD advertiser — the records the site
	// answers queries with.
	Advertiser func(d any) mdns.Advertiser
	// Endpoints are the endpoints the device's node serves: the root, the
	// Aggregator and the bridged ones.
	Endpoints func(d any) []*endpoint.Endpoint
)

// The controller seams: c is a *fabrictest.Controller.
var (
	// Exchanges is the controller's exchange manager.
	Exchanges func(c any) *exchange.Manager
	// Names is the controller's DNS-SD cache, answered by the site.
	Names func(c any) *discovery.Names
	// Peers is the controller's peer set.
	Peers func(c any) *peer.Set
	// Resumption is the peer set's CASE resumption store.
	Resumption func(c any) *peer.MemoryResumptionStore
	// Subscriptions is the controller's subscription registry.
	Subscriptions func(c any) *interaction.Subscriptions
)

// The pair seams: p is a *fabrictest.Pair.
var (
	// Peer is the controller's peer of the pair's device, holding the
	// CASE session; nil before commissioning.
	Peer func(p any) *peer.Peer
	// Interaction is the controller's interaction with the device over
	// Peer; nil before commissioning. A pair of Controller.PASE speaks
	// over PASESession instead.
	Interaction func(p any) *interaction.Interaction
	// PASESession is the PASE session of a pair of Controller.PASE; nil
	// otherwise.
	PASESession func(p any) *exchange.Session
	// Connect returns the controller's interaction with the pair's
	// device, opening a CASE session first when the peer holds none —
	// node-helpers.ts interaction.connect (:370).
	Connect func(ctx context.Context, p any) (*interaction.Interaction, error)
	// Exchange sends one Interaction Model message on a new exchange over
	// the pair's session and returns the device's first answer: the raw
	// request the controller's own formulation refuses to send
	// (AttributeWriteResponseTest.ts writeAttrRaw).
	Exchange func(ctx context.Context, p any, opcode uint8, payload []byte) (*exchange.Message, error)
)
