// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// sessionLog records the Observers' session hooks.
type sessionLog struct {
	mu     sync.Mutex
	events []string
	last   map[string]node.SessionInfo
}

func (l *sessionLog) add(kind string, s node.SessionInfo) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if s.PASE {
		kind += "(pase)"
	}
	l.events = append(l.events, kind)
	if l.last == nil {
		l.last = map[string]node.SessionInfo{}
	}
	l.last[kind] = s
}

func (l *sessionLog) count(kind string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if e == kind {
			n++
		}
	}
	return n
}

func (l *sessionLog) lastOf(kind string) node.SessionInfo {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.last[kind]
}

// TestSessionEventsParity ports the emission points of matter.js
// SessionsBehavior (packages/node/src/behavior/system/sessions/
// SessionsBehavior.ts at 862ad75f): `opened` for an established session
// but not a PASE one (:57-64), `subscriptionsChanged` when a subscription
// is added to it (:75-93), and `closed` for an announced session (:66-73)
// — here when the fabric is removed and its sessions go (W5-api-19).
func TestSessionEventsParity(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	log := &sessionLog{}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Observers.OnSessionOpened = func(s node.SessionInfo) { log.add("opened", s) }
		cfg.Observers.OnSessionClosed = func(s node.SessionInfo) { log.add("closed", s) }
		cfg.Observers.OnSubscriptionsChanged = func(s node.SessionInfo) { log.add("subscriptions", s) }
	}}})
	ctx := parityCtx(t)
	if err := site.Eventually(ctx, "a CASE session announced", func() bool { return log.count("opened") > 0 }); err != nil {
		t.Fatal(err)
	}
	if n := log.count("opened(pase)"); n != 0 {
		t.Errorf("%d PASE sessions announced, want none (SessionsBehavior.ts:58)", n)
	}
	opened := log.lastOf("opened")
	if opened.FabricIndex == 0 || opened.PeerNodeID == 0 {
		t.Errorf("opened %+v, want the CASE session's fabric and peer", opened)
	}

	path := im.ConcreteAttributePath{Endpoint: 0, Cluster: bidef.ClusterID, Attribute: bidef.AttrNodeLabel, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{path}})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if err := site.Eventually(ctx, "the subscription reported", func() bool {
		return log.count("subscriptions") > 0 && log.lastOf("subscriptions").Subscriptions == 1
	}); err != nil {
		t.Fatalf("%v; events %v", err, log.events)
	}

	idx := fabricIndexOf(t, p)
	if err := p.Device.Act(func(n *node.Node) error { return n.RemoveFabric(ctx, idx) }); err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the session closed", func() bool { return log.count("closed") > 0 }); err != nil {
		t.Fatalf("%v; events %v", err, log.events)
	}
	if closed := log.lastOf("closed"); closed.ID == 0 || closed.FabricIndex != idx {
		t.Errorf("closed %+v, want a session of fabric %d", closed, idx)
	}
}
