// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/im"
)

// Config configures a [Node].
type Config struct {
	// ID is the node's local id, the store key ("peer1"; the registry of
	// nodes allocates them).
	ID string
	// Peer is the remote node: its address, sessions and connection
	// process. Nil leaves the connection state to the subscription alone
	// (NetworkClient.ts:223, a node without a peer).
	Peer *peer.Peer
	// Interaction is the interaction bound to Peer —
	// commissioner.Commissioner.Interaction(Peer).
	Interaction *interaction.Interaction
	// Store persists the image; nil is a [MemoryStore] of this node's own.
	Store Store
	// FlushInterval batches store writes (storage/client/ClientCacheBuffer.ts:41-48);
	// 0 writes through.
	FlushInterval time.Duration
	// AutoSubscribe starts the default subscription in Start
	// (NetworkClient.ts:432-439; a newly commissioned node sets it,
	// CommissioningClient.ts:308-327). It is persisted: true sets the
	// node's stored setting, false keeps what the store holds.
	AutoSubscribe bool
	// DefaultSubscription narrows the default subscription
	// (NetworkClient.ts:412-421); nil keeps the stored one, which is the
	// wildcard when none was stored.
	DefaultSubscription *SubscribeOptions
	// NewlyCommissioned makes Start read the node once before it subscribes
	// (NetworkClient.ts:137-143, Internal.isNewlyCommissioned); whoever
	// commissioned the node sets it.
	NewlyCommissioned bool
	// NoInitialRead skips that read — autoStateInitialize false
	// (NetworkClient.ts:139, :441-445).
	NoInitialRead bool
	// Hooks are the node's callbacks.
	Hooks Hooks
	// Logger receives the diagnostics; nil discards them.
	Logger *slog.Logger
	// Clock paces the store's flush; nil is the system clock.
	Clock exchange.Clock
	// Rand returns the jitter draw in [0, 1) of the subscription ceiling;
	// nil is math/rand/v2.Float64.
	Rand func() float64
}

// SubscribeOptions narrows the default subscription — the fields of
// matter.js Subscribe a NetworkClient.State.defaultSubscription overrides
// (NetworkClient.ts:121-128). A nil list keeps the default (every
// attribute, every event urgent); a non-nil one, empty included, replaces
// it.
type SubscribeOptions struct {
	Attributes []im.ConcreteAttributePath
	Events     []im.ConcreteEventPath
	// NoFabricFilter subscribes unfiltered (fabricFilter false). The cache
	// keeps the values of one fabric view: those of interactions with the
	// default subscription's filter (ClientStructure.ts:468-471).
	NoFabricFilter bool
	// KeepSubscriptions keeps our other subscriptions with the peer
	// (keepSubscriptions true); the default subscription replaces them.
	KeepSubscriptions bool
	// MinIntervalFloor and MaxIntervalCeiling bound the reporting interval;
	// nil takes the node's physical defaults ([Node.IntervalBounds]).
	MinIntervalFloor   *time.Duration
	MaxIntervalCeiling *time.Duration
}

// Hooks are a node's callbacks. Each is optional; they run after the
// node's lock is released, in the order the structure produced them
// (events delivered at once, the structure events, Changed, then the
// delayed events — ClientStructure.ts:375-379). A panic in a structure
// hook is logged, never raised (ClientStructureEvents.ts:138-144).
type Hooks struct {
	// EndpointInstalled: an endpoint joined the image under its owner. Its
	// clusters come with it; no ClusterInstalled follows for them
	// (ClientStructureEvents.ts:88-107).
	EndpointInstalled func(ep *Endpoint)
	// ClusterInstalled: a cluster joined an installed endpoint.
	ClusterInstalled func(ep *Endpoint, c *Cluster)
	// ClusterReplaced: an installed cluster's shape changed (its revision,
	// features, attribute or command lists).
	ClusterReplaced func(ep *Endpoint, c *Cluster)
	// ClusterDeleted: the peer's ServerList dropped the cluster.
	ClusterDeleted func(ep *Endpoint, c *Cluster)
	// Changed: a read or report was applied (ClientStructure.ts:166-168,
	// :1550).
	Changed func()
	// ConnectionStateChanged: the connection state changed
	// (ClientNodeLifecycle.ts:101-107).
	ConnectionStateChanged func(s ConnectionState)
	// SubscriptionStatusChanged: the default subscription gained or lost
	// its underlying subscription (NetworkClient.ts:181-188).
	SubscriptionStatusChanged func(active bool)
	// SubscriptionAlive: the default subscription delivered a report or a
	// keepalive, or was established (NetworkClient.ts:157-172, :185-187).
	SubscriptionAlive func()
	// Event: an event the peer reported, for every cluster. value is the
	// payload decoded through the event's definition, the raw [spec.Node]
	// when there is none, nil without a payload.
	Event func(endpoint uint16, cluster, event uint32, e interaction.EventData, value any)
	// Seeded: the structure is known, once — BasicInformation is present
	// and every endpoint the root's PartsList names is installed, more than
	// the root when no PartsList was reported (Peers.ts:760-797,
	// ClientNodeLifecycle.ts:51-75).
	Seeded func()
	// Decommissioned: a [Registry] removed the node from its fabric, or the
	// peer left it — matter.js lifecycle.decommissioned
	// (node/NodeLifecycle.ts:105-110), emitted when the peer address is
	// cleared (CommissioningClient.ts:637-646) and on a leave event
	// (node/client/Peers.ts:827). On a leave it runs inside the
	// [Registry]'s fabric operation, before the node's sessions go and it
	// is deleted (Peers.ts:826-833): it must not start a fabric operation
	// of the same registry (decommission, a fabric change), which waits for
	// that one to end.
	Decommissioned func()
	// Commissioned: the registry commissioned the node — matter.js
	// lifecycle.commissioned (CommissioningClient.ts:325 at 0ceb202d),
	// emitted after the record and settings are saved and before the node
	// starts.
	Commissioned func()
	// ConfigurationVersionChanged: a report changed the ConfigurationVersion
	// of the node's BasicInformation (ep the root) or of a bridged
	// endpoint's BridgedDeviceBasicInformation — matter.js
	// lifecycle.configurationVersionChanged (node/client/Peers.ts:757,
	// :803-814). The first value the image takes is no change.
	ConfigurationVersionChanged func(ep *Endpoint)
}

// ConnectionState is a node's connection state — matter.js
// NodeConnectionState (ClientNodeLifecycle.ts:16-40), the same values.
type ConnectionState uint8

// Connection states.
const (
	// Connected: the subscription is established and the peer has a
	// session; the data is current.
	Connected ConnectionState = 0
	// Disconnected: disabled, stopped or not started.
	Disconnected ConnectionState = 1
	// Reconnecting: a connection was lost and is being re-established.
	Reconnecting ConnectionState = 2
	// WaitingForDeviceDiscovery: the peer looks offline; the node waits for
	// it to announce itself again.
	WaitingForDeviceDiscovery ConnectionState = 3
)

func (s ConnectionState) String() string {
	switch s {
	case Connected:
		return "Connected"
	case Disconnected:
		return "Disconnected"
	case Reconnecting:
		return "Reconnecting"
	case WaitingForDeviceDiscovery:
		return "WaitingForDeviceDiscovery"
	}
	return "ConnectionState(" + strconv.Itoa(int(s)) + ")"
}

// Errors of a [Node].
var (
	// ErrDisabled: Start of a disabled node — matter.js
	// ClientNetworkRuntime throws (ClientNetworkRuntime.ts:38-43).
	ErrDisabled = errors.New("client: node is disabled")
	// ErrClosed: the node was closed.
	ErrClosed = errors.New("client: node is closed")
	// ErrConfig: an invalid Config.
	ErrConfig = errors.New("client: invalid config")
)
