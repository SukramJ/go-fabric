// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/store"
)

// The wire helpers' vocabulary, owned here (ADR 0025 principle 3). The
// controller's types are the controller role's (controller/client), so a
// test that reads with [Read] and one that reads through [Pair.Node] name
// the same types; each alias adds no behaviour. Build paths with
// client.AttributeAt, client.ClusterAt, client.CommandAt and
// client.EventAt.
type (
	// AttributePath is a concrete attribute path (client.AttributePath).
	AttributePath = client.AttributePath
	// CommandPath is a concrete command path (client.CommandPath).
	CommandPath = client.CommandPath
	// EventPath is a concrete event path (client.EventPath).
	EventPath = client.EventPath
	// ReadRequest are the paths and filters of one read [ReadWith] sends
	// (client.ReadPaths): Attributes, Events, EventFilters,
	// DataVersionFilters, NoFabricFilter.
	ReadRequest = client.ReadPaths
	// SubscribeRequest is a subscription [Subscribe] establishes
	// (client.SubscribeRequest: the paths, the interval bounds, Updated
	// for every report, Closed).
	SubscribeRequest = client.SubscribeRequest
	// Subscription is an established subscription (client.Subscription);
	// the site closes it, or Close does.
	Subscription = client.Subscription
	// Report is a read's or a subscription report's data (client.Report);
	// Report.Values collects one cluster's for a generated
	// <cluster>def.StateFrom.
	Report = client.Report
	// AttributeData is one attribute value of a report (client.AttributeData).
	AttributeData = client.AttributeData
	// EventData is one event of a report (client.EventData).
	EventData = client.EventData
	// Result is a command's answer (client.Result).
	Result = client.Result
	// StatusError is an IM status other than Success (client.StatusError).
	StatusError = client.StatusError
	// PeerAddress is a node on a fabric (client.PeerAddress).
	PeerAddress = client.PeerAddress

	// Interaction is the controller's interaction with a pair's device
	// ([Connect], [Pair].Interaction): the client machinery's own,
	// client/interaction.
	Interaction = interaction.Interaction
	// Peer is the controller's peer of a pair's device, holding its CASE
	// session (client/peer).
	Peer = peer.Peer
	// Session is a secure session (client/exchange): a pair's PASE session.
	Session = exchange.Session
	// Message is one received message of [Exchange] (client/exchange).
	Message = exchange.Message

	// Privilege is an access privilege [Pair.Grant] gives the controller.
	Privilege = store.Privilege
)

// The privileges of [Pair.Grant], lowest first.
const (
	PrivilegeView       = store.PrivilegeView
	PrivilegeProxyView  = store.PrivilegeProxyView
	PrivilegeOperate    = store.PrivilegeOperate
	PrivilegeManage     = store.PrivilegeManage
	PrivilegeAdminister = store.PrivilegeAdminister
)
