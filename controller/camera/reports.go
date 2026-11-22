// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"slices"
	"sync"

	"thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	"thingfabric.dev/go-fabric/controller"
	"thingfabric.dev/go-fabric/controller/client"
)

// Reports brings the cameras' reports to a [Service]: each node's applied
// report batch (its subscription alive), its StartUp event, a new
// endpoint and its removal from the fabric. Install it on the controller's
// configuration before controller.New — the node hooks are fixed when a
// node is built — and pass it in [Config].Reports. matterjs-server feeds
// the same four from its controller's node events
// (packages/ws-controller/src/controller/WebRtcProviderSessions.ts:120-155
// at 0bd06f2).
//
//	var reports camera.Reports
//	reports.Install(&cfg)
//	ctrl, err := controller.New(ctx, cfg)
//	…
//	svc, err := camera.New(ctx, ctrl, camera.Config{Reports: &reports})
//
// One Service at a time holds a Reports: [New] refuses one a Service that
// is not closed holds.
type Reports struct {
	mu   sync.Mutex
	done bool
	// cfgs are the configurations Install wrapped, each once.
	cfgs []*controller.Config
	// held: a Service holds the reports, from New's claim to its Close.
	held bool
	svc  *Service
	// known is the operational node id of each client node seen, for a
	// decommission, whose hook runs once the peer address is cleared.
	known map[string]uint64
}

// Install wraps cfg's node hooks — cfg.HooksFor, else the template
// cfg.Node.Hooks; the host's hooks run first and keep their behaviour.
// Installing it again on the same configuration changes nothing (review
// II, R2-P6-api-2208).
func (r *Reports) Install(cfg *controller.Config) {
	r.mu.Lock()
	if slices.Contains(r.cfgs, cfg) {
		r.mu.Unlock()
		return
	}
	r.cfgs = append(r.cfgs, cfg)
	r.mu.Unlock()
	user := cfg.HooksFor
	template := cfg.Node.Hooks
	cfg.HooksFor = func(id string) client.Hooks {
		h := template
		if user != nil {
			h = user(id)
		}
		return r.wrap(id, h)
	}
	r.mu.Lock()
	r.done = true
	r.mu.Unlock()
}

func (r *Reports) installed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

// claim reserves the reports for the Service New builds; false while
// another Service holds them (review II, R2-P6-api-2208).
func (r *Reports) claim() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.held {
		return false
	}
	r.held = true
	return true
}

// attach hangs s on the reports and learns the operational node id of
// every node the registry holds, so a node decommissioned before its next
// report is still named.
func (r *Reports) attach(s *Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.svc = s
	if r.known == nil {
		r.known = map[string]uint64{}
	}
	// The registry is nil once the controller's Close has begun.
	reg := s.ctrl.Registry()
	if reg == nil {
		return
	}
	for _, n := range reg.Nodes() {
		if addr, ok := n.PeerAddress(); ok {
			r.known[n.ID()] = addr.NodeID
		}
	}
}

func (r *Reports) detach(s *Service) {
	r.mu.Lock()
	if r.svc == s {
		r.svc, r.held = nil, false
	}
	r.mu.Unlock()
}

// forget drops a node that left the registry from known (review II,
// R2-P6-bound-2209).
func (r *Reports) forget(id string) {
	r.mu.Lock()
	delete(r.known, id)
	r.mu.Unlock()
}

// with runs fn with the attached service and the operational node id of
// the client node id, if both exist; a node whose address is gone is
// named by the id it had.
func (r *Reports) with(id string, fn func(s *Service, nodeID uint64)) {
	r.mu.Lock()
	s := r.svc
	r.mu.Unlock()
	if s == nil {
		return
	}
	nodeID, ok := uint64(0), false
	// The registry is nil once the controller's Close has begun; the node
	// is then named by the id it had.
	if reg := s.ctrl.Registry(); reg != nil {
		if n := reg.Get(id); n != nil {
			var addr client.PeerAddress
			if addr, ok = n.PeerAddress(); ok {
				nodeID = addr.NodeID
			}
		}
	}
	r.mu.Lock()
	if ok {
		if r.known == nil {
			r.known = map[string]uint64{}
		}
		r.known[id] = nodeID
	} else {
		nodeID, ok = r.known[id]
	}
	r.mu.Unlock()
	if ok {
		fn(s, nodeID)
	}
}

// isStartUp reports whether an event is the root's BasicInformation
// StartUp, the one event that marks a restart. Mirrors matterjs-server
// packages/ws-controller/src/controller/WebRtcProviderSessions.ts:139-143
// at 0bd06f2.
func isStartUp(endpoint uint16, cluster, event uint32) bool {
	return endpoint == 0 && cluster == basicinformation.ClusterID && event == basicinformation.EventStartUp
}

func (r *Reports) wrap(id string, h client.Hooks) client.Hooks {
	event, alive, installed, decommissioned, deleted := h.Event, h.SubscriptionAlive, h.EndpointInstalled, h.Decommissioned, h.Deleted
	h.Event = func(endpoint uint16, cluster, ev uint32, e client.EventData, value any) {
		if event != nil {
			event(endpoint, cluster, ev, e, value)
		}
		if isStartUp(endpoint, cluster, ev) {
			r.with(id, func(s *Service, nodeID uint64) { s.w.startedUp(nodeID) })
		}
	}
	h.SubscriptionAlive = func() {
		if alive != nil {
			alive()
		}
		r.with(id, func(s *Service, nodeID uint64) { s.reportCompleted(nodeID) })
	}
	h.EndpointInstalled = func(ep *client.Endpoint) {
		if installed != nil {
			installed(ep)
		}
		r.with(id, func(s *Service, nodeID uint64) { s.w.endpointAdded(nodeID) })
	}
	h.Decommissioned = func() {
		if decommissioned != nil {
			decommissioned()
		}
		r.with(id, func(s *Service, nodeID uint64) { s.nodeRemoved(nodeID) })
	}
	// A node that leaves the registry without a decommission (Forget,
	// Erase, the cull) fires Deleted only; it is forgotten all the same.
	// After a decommission this runs a second time and finds nothing left
	// (review II, R2-P2-fabric-08). The node's operational id goes with
	// it: the node is gone from the registry (R2-P6-bound-2209).
	h.Deleted = func() {
		if deleted != nil {
			deleted()
		}
		r.with(id, func(s *Service, nodeID uint64) { s.nodeRemoved(nodeID) })
		r.forget(id)
	}
	return h
}
