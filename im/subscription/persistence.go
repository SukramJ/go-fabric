// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// ReestablishTimeout bounds how long re-establishing the former
// subscriptions of one peer may spend reaching it — discovery plus CASE.
// Mirrors matter.js REESTABLISH_SUBSCRIPTIONS_TIMEOUT
// (packages/node/src/behavior/system/subscriptions/SubscriptionsServer.ts),
// passed there as `connectionTimeout` to `peer.connect`.
const ReestablishTimeout = 2 * time.Second

// PeerSubscription is the persisted form of one server subscription: what
// survives a restart so the subscription can be re-established under its
// old id. Mirrors matter.js `PeerSubscription`
// (packages/node/src/node/server/InteractionServer.ts) as
// SubscriptionsServer stores it — data-version and event filters are not
// kept, so the priming report of a re-established subscription carries
// full data.
type PeerSubscription struct {
	// SubscriptionID is the id the controller knows the subscription by.
	SubscriptionID uint32
	// FabricIndex and PeerNodeID are matter.js's `peerAddress`.
	FabricIndex uint8
	PeerNodeID  uint64
	// AttributeRequests and EventRequests are the subscribed paths.
	AttributeRequests []im.ConcreteAttributePath
	EventRequests     []im.ConcreteEventPath
	// IsFabricFiltered is the request's fabric-filtered flag.
	IsFabricFiltered bool
	// MinIntervalFloor and MaxIntervalCeiling are the subscription's
	// cadence bounds in seconds — as admitted by [Manager.Subscribe], i.e.
	// after its clamp to the manager limits.
	MinIntervalFloor   uint16
	MaxIntervalCeiling uint16
	// MaxInterval is the negotiated max interval (seconds) the
	// SubscribeResponse announced.
	MaxInterval uint16
	// SendInterval is the publisher heartbeat cadence.
	SendInterval time.Duration
}

// PeerSubscription captures s in its persisted form. isFabricFiltered
// comes from the request; the manager does not keep it.
func (s *Subscription) PeerSubscription(isFabricFiltered bool) PeerSubscription {
	return PeerSubscription{
		SubscriptionID:     s.ID,
		FabricIndex:        s.FabricIndex,
		PeerNodeID:         s.PeerNodeID,
		AttributeRequests:  append([]im.ConcreteAttributePath(nil), s.AttributePaths...),
		EventRequests:      append([]im.ConcreteEventPath(nil), s.EventPaths...),
		IsFabricFiltered:   isFabricFiltered,
		MinIntervalFloor:   s.MinIntervalFloor,
		MaxIntervalCeiling: s.MaxIntervalCeiling,
		MaxInterval:        s.MaxIntervalCeiling,
		SendInterval:       s.SendInterval(),
	}
}

// Restore re-creates a former subscription on sessionID under its
// persisted id, with the max interval and send interval it negotiated in
// its first run instead of negotiating them again.
//
// Mirrors matter.js InteractionServer.establishFormerSubscription
// (packages/node/src/node/server/InteractionServer.ts), which builds the
// ServerSubscription with `id: subscriptionId`, `useAsMaxInterval` and
// `useAsSendInterval`. As there, the caller sends the priming report next
// and unwinds with [Manager.Release] if it fails.
//
// Restore enforces the per-fabric quota and refuses an id that already
// names a live subscription ([ErrIDInUse]).
func (m *Manager) Restore(p PeerSubscription, sessionID uint16) (*Subscription, error) {
	if p.SubscriptionID == 0 {
		return nil, errors.New("subscription: restore: zero subscription id")
	}
	if len(p.AttributeRequests) == 0 && len(p.EventRequests) == 0 {
		return nil, errors.New("subscription: restore: no paths")
	}
	if err := m.validateCadence(p.MinIntervalFloor, p.MaxInterval); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if _, taken := m.byID[p.SubscriptionID]; taken {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: %d", ErrIDInUse, p.SubscriptionID)
	}
	if m.perFabric[p.FabricIndex] >= m.cfg.MaxSubscriptionsPerFabric {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: fabric=%d quota=%d", ErrFabricQuotaExceeded, p.FabricIndex, m.cfg.MaxSubscriptionsPerFabric)
	}
	sub := &Subscription{
		ID:                   p.SubscriptionID,
		FabricIndex:          p.FabricIndex,
		PeerNodeID:           p.PeerNodeID,
		SessionID:            sessionID,
		MinIntervalFloor:     p.MinIntervalFloor,
		MaxIntervalCeiling:   p.MaxInterval,
		KeepSubscriptions:    true,
		AttributePaths:       append([]im.ConcreteAttributePath(nil), p.AttributeRequests...),
		EventPaths:           append([]im.ConcreteEventPath(nil), p.EventRequests...),
		sendIntervalOverride: p.SendInterval,
		// As in Subscribe: the priming report is the first report, so the
		// engine must not fire a keep-alive into it.
		lastReport: time.Now(),
	}
	m.byID[sub.ID] = sub
	m.perFabric[sub.FabricIndex]++
	m.mu.Unlock()

	m.logger.Debug(
		"matter subscription restored",
		slog.Uint64("id", uint64(sub.ID)),
		slog.Int("fabric", int(sub.FabricIndex)),
		slog.Int("max", int(sub.MaxIntervalCeiling)),
		slog.Duration("send_interval", p.SendInterval),
	)
	return sub, nil
}

// peerSubscriptionWireVersion versions the persisted encoding.
const peerSubscriptionWireVersion = 1

// peerSubscriptionWire is the JSON shape of a persisted subscription. The
// field names follow matter.js SubscriptionsServer's state schema
// (subscriptionId, peerAddress{fabricIndex,nodeId}, attributeRequests,
// eventRequests, isFabricFiltered, minIntervalFloor, maxIntervalCeiling,
// maxInterval, sendInterval); durations are milliseconds, as matter.js
// stores its `duration` fields.
type peerSubscriptionWire struct {
	Version           int                 `json:"v"`
	SubscriptionID    uint32              `json:"subscriptionId"`
	PeerAddress       peerAddressWire     `json:"peerAddress"`
	AttributeRequests []attributePathWire `json:"attributeRequests,omitempty"`
	EventRequests     []eventPathWire     `json:"eventRequests,omitempty"`
	IsFabricFiltered  bool                `json:"isFabricFiltered"`
	MinIntervalFloor  int64               `json:"minIntervalFloor"`
	MaxIntervalCeil   int64               `json:"maxIntervalCeiling"`
	MaxInterval       int64               `json:"maxInterval"`
	SendInterval      int64               `json:"sendInterval"`
}

type peerAddressWire struct {
	FabricIndex uint8  `json:"fabricIndex"`
	NodeID      uint64 `json:"nodeId"`
}

type attributePathWire struct {
	NodeID      *uint64 `json:"nodeId,omitempty"`
	EndpointID  *uint16 `json:"endpointId,omitempty"`
	ClusterID   *uint32 `json:"clusterId,omitempty"`
	AttributeID *uint32 `json:"attributeId,omitempty"`
	ListIndex   *uint16 `json:"listIndex,omitempty"`
}

type eventPathWire struct {
	NodeID     *uint64 `json:"nodeId,omitempty"`
	EndpointID *uint16 `json:"endpointId,omitempty"`
	ClusterID  *uint32 `json:"clusterId,omitempty"`
	EventID    *uint32 `json:"eventId,omitempty"`
	IsUrgent   bool    `json:"isUrgent,omitempty"`
}

func ptrIf[T any](has bool, v T) *T {
	if !has {
		return nil
	}
	return &v
}

func valOf[T any](p *T) (T, bool) {
	var zero T
	if p == nil {
		return zero, false
	}
	return *p, true
}

const msPerSecond = int64(time.Second / time.Millisecond)

// MarshalPeerSubscription encodes p for a [PeerSubscription] store.
func MarshalPeerSubscription(p PeerSubscription) ([]byte, error) {
	w := peerSubscriptionWire{
		Version:          peerSubscriptionWireVersion,
		SubscriptionID:   p.SubscriptionID,
		PeerAddress:      peerAddressWire{FabricIndex: p.FabricIndex, NodeID: p.PeerNodeID},
		IsFabricFiltered: p.IsFabricFiltered,
		MinIntervalFloor: int64(p.MinIntervalFloor) * msPerSecond,
		MaxIntervalCeil:  int64(p.MaxIntervalCeiling) * msPerSecond,
		MaxInterval:      int64(p.MaxInterval) * msPerSecond,
		SendInterval:     p.SendInterval.Milliseconds(),
	}
	for _, a := range p.AttributeRequests {
		w.AttributeRequests = append(w.AttributeRequests, attributePathWire{
			NodeID:      ptrIf(a.HasNode, a.Node),
			EndpointID:  ptrIf(a.HasEndpoint, a.Endpoint),
			ClusterID:   ptrIf(a.HasCluster, a.Cluster),
			AttributeID: ptrIf(a.HasAttribute, a.Attribute),
			ListIndex:   ptrIf(a.HasListIndex, a.ListIndex),
		})
	}
	for _, e := range p.EventRequests {
		w.EventRequests = append(w.EventRequests, eventPathWire{
			NodeID:     ptrIf(e.HasNode, e.Node),
			EndpointID: ptrIf(e.HasEndpoint, e.Endpoint),
			ClusterID:  ptrIf(e.HasCluster, e.Cluster),
			EventID:    ptrIf(e.HasEvent, e.Event),
			IsUrgent:   e.IsUrgent,
		})
	}
	return json.Marshal(w)
}

// secondsOf converts a persisted millisecond duration back to the uint16
// seconds the manager works in, rejecting values it cannot represent.
func secondsOf(field string, ms int64) (uint16, error) {
	if ms < 0 || ms/msPerSecond > 0xFFFF {
		return 0, fmt.Errorf("subscription: persisted %s out of range: %d ms", field, ms)
	}
	return uint16(ms / msPerSecond), nil
}

// UnmarshalPeerSubscription decodes what [MarshalPeerSubscription]
// produced.
func UnmarshalPeerSubscription(b []byte) (PeerSubscription, error) {
	var w peerSubscriptionWire
	if err := json.Unmarshal(b, &w); err != nil {
		return PeerSubscription{}, fmt.Errorf("subscription: decode persisted subscription: %w", err)
	}
	if w.Version != peerSubscriptionWireVersion {
		return PeerSubscription{}, fmt.Errorf("subscription: persisted subscription version %d unsupported", w.Version)
	}
	p := PeerSubscription{
		SubscriptionID:   w.SubscriptionID,
		FabricIndex:      w.PeerAddress.FabricIndex,
		PeerNodeID:       w.PeerAddress.NodeID,
		IsFabricFiltered: w.IsFabricFiltered,
		SendInterval:     time.Duration(w.SendInterval) * time.Millisecond,
	}
	var err error
	if p.MinIntervalFloor, err = secondsOf("minIntervalFloor", w.MinIntervalFloor); err != nil {
		return PeerSubscription{}, err
	}
	if p.MaxIntervalCeiling, err = secondsOf("maxIntervalCeiling", w.MaxIntervalCeil); err != nil {
		return PeerSubscription{}, err
	}
	if p.MaxInterval, err = secondsOf("maxInterval", w.MaxInterval); err != nil {
		return PeerSubscription{}, err
	}
	for _, a := range w.AttributeRequests {
		var path im.ConcreteAttributePath
		path.Node, path.HasNode = valOf(a.NodeID)
		path.Endpoint, path.HasEndpoint = valOf(a.EndpointID)
		path.Cluster, path.HasCluster = valOf(a.ClusterID)
		path.Attribute, path.HasAttribute = valOf(a.AttributeID)
		path.ListIndex, path.HasListIndex = valOf(a.ListIndex)
		p.AttributeRequests = append(p.AttributeRequests, path)
	}
	for _, e := range w.EventRequests {
		var path im.ConcreteEventPath
		path.Node, path.HasNode = valOf(e.NodeID)
		path.Endpoint, path.HasEndpoint = valOf(e.EndpointID)
		path.Cluster, path.HasCluster = valOf(e.ClusterID)
		path.Event, path.HasEvent = valOf(e.EventID)
		path.IsUrgent = e.IsUrgent
		p.EventRequests = append(p.EventRequests, path)
	}
	return p, nil
}
