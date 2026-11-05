// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
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
type Reports struct {
	mu   sync.Mutex
	done bool
	svc  *Service
	// known is the operational node id of each client node seen, for a
	// decommission, whose hook runs once the peer address is cleared.
	known map[string]uint64
}

// Install wraps cfg's node hooks — cfg.HooksFor, else the template
// cfg.Node.Hooks; the host's hooks run first and keep their behaviour.
func (r *Reports) Install(cfg *controller.Config) {
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

func (r *Reports) attach(s *Service) {
	r.mu.Lock()
	r.svc = s
	r.mu.Unlock()
}

func (r *Reports) detach(s *Service) {
	r.mu.Lock()
	if r.svc == s {
		r.svc = nil
	}
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
	if n := s.ctrl.Registry().Get(id); n != nil {
		var addr client.PeerAddress
		if addr, ok = n.PeerAddress(); ok {
			nodeID = addr.NodeID
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
	event, alive, installed, decommissioned := h.Event, h.SubscriptionAlive, h.EndpointInstalled, h.Decommissioned
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
	return h
}
