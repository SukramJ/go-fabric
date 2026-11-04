// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/node"
)

// The reboot-armer case of matter.js's OtaTest
// (packages/node/test/behaviors/ota/OtaTest.ts at 862ad75f) that runs a
// device and listens on the report notifications the armer is fed;
// controller.ObserveArmer is that listener here (export_ota_test.go).

// reportLog records the sessions the armer is told the device's reports
// arrived over.
type reportLog struct {
	mu       sync.Mutex
	sessions []*exchange.Session
}

func observeReports(r *otaRig, p exchange.PeerAddress) *reportLog {
	l := &reportLog{}
	controller.ObserveArmer(r.c, &controller.ArmerObserver{
		ReportStarted: func(s peer.ArmerSession) {
			if s.Peer() == p {
				l.mu.Lock()
				l.sessions = append(l.sessions, controller.ArmerSessionOf(s))
				l.mu.Unlock()
			}
		},
	})
	return l
}

func (l *reportLog) reports() []*exchange.Session {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.sessions)
}

// TestOTAReportAnnouncesItsSessionParity ports OtaTest.ts:892-952: each
// report the device pushes over the sustained subscription announces to
// the armer the session it arrived over — the peer's one live session
// (ClientSubscriptions.reportStarted; the Go controller's reportStarted,
// RebootResubscribeArmer.ts:165-172 at 862ad75f).
func TestOTAReportAnnouncesItsSessionParity(t *testing.T) {
	t.Parallel()
	r := newOTARigWith(t, nil, nil)
	p := r.peer(t)
	log := observeReports(r, p)
	set := func(v uint32) {
		t.Helper()
		if err := r.dev.Act(func(n *node.Node) error {
			return n.Root().BasicInformation.SetSoftwareVersion(v, "v")
		}); err != nil {
			t.Fatal(err)
		}
	}
	set(99)
	r.eventually(t, "the first report", func() bool { return len(log.reports()) >= 1 })
	first := len(log.reports())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := r.site.Step(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	set(100)
	r.eventually(t, "the second report", func() bool { return len(log.reports()) > first })
	live := controller.PeerSessions(r.c, p)
	if len(live) != 1 {
		t.Fatalf("%d live sessions with the device, want 1", len(live))
	}
	for i, s := range log.reports() {
		if s != live[0] {
			t.Fatalf("report %d announced session %v, want the live %v", i, s, live[0])
		}
	}
}
