// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// The role's vocabulary: the types [Node.Read], [Node.Subscribe],
// [Node.Write] and [Node.Invoke] take and return, owned here so a
// controller program names them through this package (ADR 0025
// principle 3). Each is the client machinery's own type
// (client/interaction, client/discovery, client/peer, im); the alias adds
// no behaviour.
type (
	// AttributePath is a concrete attribute path. Build one with
	// [AttributeAt], or [ClusterAt] for every attribute of a cluster; a
	// path's Has… flags say which of its fields are set, a field left
	// unset being a wildcard.
	AttributePath = im.ConcreteAttributePath
	// CommandPath is a concrete command path ([CommandAt]).
	CommandPath = im.ConcreteCommandPath
	// EventPath is a concrete event path ([EventAt]).
	EventPath = im.ConcreteEventPath
	// DataVersionFilter skips a cluster whose DataVersion the caller
	// holds. [Node.Read] and [Node.Subscribe] add the image's own.
	DataVersionFilter = im.DataVersionFilter
	// EventMinimumNumber asks for the events from a number on.
	EventMinimumNumber = im.EventMinimumNumber

	// ReadPaths are the paths and filters of a read, the part of
	// [ReadRequest] the read sends: Attributes (the attribute paths,
	// wildcards allowed), Events (the event paths), EventFilters,
	// DataVersionFilters, and NoFabricFilter (true reads fabric-scoped
	// lists unfiltered). A ReadRequest's fields are promoted:
	//
	//	var req client.ReadRequest
	//	req.Attributes = []client.AttributePath{client.ClusterAt(1, onoffdef.ClusterID)}
	ReadPaths = interaction.ReadRequest
	// SubscribePaths are a subscription as it is sent, the part of
	// [SubscribeRequest] the subscription sends: the paths and filters of a read
	// ([ReadPaths]); MinIntervalFloor and MaxIntervalCeiling ([Interval];
	// nil for the defaults, the node's bounds applied over them);
	// ReplaceSubscriptions (false, the default, keeps the other
	// subscriptions with the peer — matter.js keepSubscriptions true);
	// Updated, called with every report that carries data, the priming
	// report too, on the report's exchange (keep it short); Closed,
	// called once the subscription ended; Sustain, which re-establishes a
	// lost subscription. The [Subscription] it returns ends with
	// [Subscription.Close] or with the node; its Done channel closes then.
	// A SubscribeRequest's fields are promoted.
	SubscribePaths = interaction.SubscribeRequest
	// Subscription is an established subscription: ID, MaxInterval, Done
	// (closed when it ended) and Close (idempotent; the peer learns of it
	// from the next report it sends).
	Subscription = interaction.Subscription
	// Report is a read's data, or one subscription report's, its chunks
	// merged: Attributes (values), Statuses (attribute paths answered with
	// a status), Events, EventStatuses. [Report.Values] collects one
	// cluster's values for a generated <cluster>def.StateFrom.
	Report = interaction.Report
	// AttributeData is one attribute value of a report: its Path, the
	// cluster's DataVersion, and the Value as a TLV node — decode it with
	// the generated <cluster>def.StateFrom over [Report.Values], or
	// spec.DecodeAttribute.
	AttributeData = interaction.AttributeData
	// AttributeStatus is an attribute path a report answers with a status.
	AttributeStatus = interaction.AttributeStatus
	// EventData is one event of a report: Path, Number, Priority, the
	// timestamps, and the event's data as a TLV node (HasValue false when
	// it carried none).
	EventData = interaction.EventData
	// EventStatus is an event path a report answers with a status.
	EventStatus = interaction.EventStatus

	// WriteRequest is one attribute write: Path, Value (a spec.Encodable:
	// a generated struct or request, a spec.List, or a scalar spec.Value
	// such as spec.Uint8(20) and spec.Null[uint16]()) and DataVersion (nil
	// for an unconditional write).
	WriteRequest = interaction.WriteRequest
	// WriteOptions are a write's options: Timed (a TimedRequest first,
	// for an attribute that needs one), TimedTimeout, SuppressResponse,
	// DisableListChunking.
	WriteOptions = interaction.WriteOptions
	// WriteStatus is one path's answer to a write.
	WriteStatus = interaction.WriteStatus
	// WriteStatuses are a write's answers, one per path; Err is the first
	// that is not Success, as a *StatusError.
	WriteStatuses = interaction.WriteStatuses
	// CommandRequest is one command of [Node.InvokeBatch]: its Path and
	// its request Fields (a generated request's value, nil for none).
	CommandRequest = interaction.CommandRequest
	// InvokeOptions are an invoke's options: Timed (a TimedRequest first;
	// a command schema.IsTimedInvoke names is timed without it), TimedTimeout,
	// ExpectedProcessingTime, ExtendedFailSafeTimeout, SuppressResponse.
	InvokeOptions = interaction.InvokeOptions
	// Result is a command's answer: the response Command and its Fields
	// (HasFields false for a status answer), or the Status and
	// ClusterStatus.
	Result = interaction.Result
	// StatusError is an IM status other than Success: Status, the
	// ClusterStatus when the peer sent one, and the attribute Path for an
	// attribute's status.
	StatusError = interaction.StatusError

	// PeerAddress is a node on a fabric: the fabric's index and the
	// node's id.
	PeerAddress = handshake.PeerAddress
	// Device is a commissionable node as DNS-SD reports it: its TXT data,
	// discriminator, commissioning mode and addresses ([Registry.ForDevice]).
	Device = discovery.Device
	// SupportedTransports are the transports a node announces (its `T`
	// TXT key).
	SupportedTransports = discovery.SupportedTransports
	// Timing are the peer's connection timing parameters (a zero field is
	// the default).
	Timing = peer.Timing
	// SessionParameters are the session parameters a node reported.
	SessionParameters = sessionparams.Parameters
	// DeviceTypeViolation is one finding of a [ConformanceReport].
	DeviceTypeViolation = endpoint.DeviceTypeViolation
)

// AttributeAt is the path of one attribute of a cluster of an endpoint.
func AttributeAt(ep uint16, cluster, attribute uint32) AttributePath {
	return AttributePath{Endpoint: ep, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// ClusterAt is the path of every attribute of a cluster of an endpoint —
// the read a generated <cluster>def.StateFrom takes.
func ClusterAt(ep uint16, cluster uint32) AttributePath {
	return AttributePath{Endpoint: ep, Cluster: cluster, HasEndpoint: true, HasCluster: true}
}

// CommandAt is the path of a command of a cluster of an endpoint.
func CommandAt(ep uint16, cluster, command uint32) CommandPath {
	return CommandPath{Endpoint: ep, Cluster: cluster, Command: command, HasEndpoint: true, HasCluster: true, HasCommand: true}
}

// EventAt is the path of an event of a cluster of an endpoint.
func EventAt(ep uint16, cluster, event uint32) EventPath {
	return EventPath{Endpoint: ep, Cluster: cluster, Event: event, HasEndpoint: true, HasCluster: true, HasEvent: true}
}

// Interval is d as the pointer a [SubscribeRequest]'s interval bounds take.
func Interval(d time.Duration) *time.Duration { return interaction.Interval(d) }
