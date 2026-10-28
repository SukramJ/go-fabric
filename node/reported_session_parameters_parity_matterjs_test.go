// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// A port of matter.js packages/node/test/node/ReportedSessionParametersTest.ts
// at 862ad75f: both ends keep what the peer sent in PASE and CASE
// establishment (:46-54) and in a resumed CASE session (:56-76). The
// controller's record is client/exchange.Session.ReportedParameters, the
// device's is node.SessionInfo.PeerIdleInterval (secure/operational.Entry
// .PeerMRPIntervals). The intervals are the test's (:16-17): the controller
// 2345 ms / 234 ms, the device 1234 ms / 321 ms.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

const (
	rspControllerIdle = 2345 * time.Millisecond
	rspDeviceIdle     = 1234 * time.Millisecond
)

func rspOptions() fabrictest.PairOptions {
	return fabrictest.PairOptions{
		Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
			cfg.Sessions = &node.SessionIntervals{Idle: rspDeviceIdle, Active: 321 * time.Millisecond}
		}},
		Controller: fabrictest.ControllerOptions{Sessions: &controller.SessionIntervals{Idle: rspControllerIdle, Active: 234 * time.Millisecond}},
	}
}

// deviceReportedIdle is the matter.js helper reportedIdle(sessions, isPase)
// on the device's side (:43): the PeerIdleInterval of every open session
// of the kind.
func deviceReportedIdle(n *node.Node, pase bool) []time.Duration {
	var out []time.Duration
	sessions := n.SessionDiagnostics().Sessions
	for i := range sessions {
		if sessions[i].PASE == pase {
			out = append(out, sessions[i].PeerIdleInterval)
		}
	}
	return out
}

func wantAll(t *testing.T, what string, got []time.Duration, want time.Duration) {
	t.Helper()
	if len(got) == 0 {
		t.Fatalf("%s: no session", what)
	}
	for _, g := range got {
		if g != want {
			t.Errorf("%s: reported idle %v, want %v (all: %v)", what, g, want, got)
		}
	}
}

// TestReportedSessionParametersParityMatterJS ports "holds what the peer
// sent in PASE and CASE establishment" (:46-54).
func TestReportedSessionParametersParityMatterJS(t *testing.T) {
	holdSite(t)
	site := quietSite(t)
	opts := rspOptions()
	ctrl := site.AddController(opts.Controller)
	ctx := context.Background()

	// PASE: the device's record of the controller, the controller's of the
	// device (:50-51). A device of its own: a device holds one PASE
	// session at a time, so the commissioning below pairs a second one.
	paseDev := site.AddDevice(opts.Device)
	pase, err := ctrl.PASE(ctx, paseDev)
	if err != nil {
		t.Fatal(err)
	}
	wantAll(t, "device PASE", deviceReportedIdle(paseDev.Node(), true), rspControllerIdle)
	if rp := pase.PASESession.ReportedParameters(); rp == nil || time.Duration(rp.SessionIdleInterval)*time.Millisecond != rspDeviceIdle {
		t.Errorf("controller PASE: reported %+v, want idle %v", rp, rspDeviceIdle)
	}

	// CASE (:52-53), on a second device commissioned to the end.
	dev := site.AddDevice(opts.Device)
	pair, err := ctrl.Commission(ctx, dev, opts.Commission)
	if err != nil {
		t.Fatal(err)
	}
	wantAll(t, "device CASE", deviceReportedIdle(dev.Node(), false), rspControllerIdle)
	var ctrlIdle []time.Duration
	for _, s := range pair.Peer.Sessions() {
		if rp := s.ReportedParameters(); rp != nil {
			ctrlIdle = append(ctrlIdle, time.Duration(rp.SessionIdleInterval)*time.Millisecond)
		}
	}
	wantAll(t, "controller CASE", ctrlIdle, rspDeviceIdle)
}

// TestReportedSessionParametersResumedParityMatterJS ports "holds what the
// peer sent in a resumed CASE session" (:56-76): after the controller
// drops its session, the next interaction resumes CASE (Sigma2_Resume) and
// both ends still hold the peer's intervals.
func TestReportedSessionParametersResumedParityMatterJS(t *testing.T) {
	holdSite(t)
	site := quietSite(t)
	p := site.CommissionedPair(rspOptions())
	ctx := context.Background()

	path := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0001} // BasicInformation.VendorName
	// Under Site.Run: the reconnect's timers run on the site's clock.
	err := site.Run(ctx, func(ctx context.Context) error {
		p.Peer.Disconnect(ctx, errors.New("test: drop the session for a resume"))
		_, err := p.Interaction.Read(ctx, path)
		return err
	})
	if err != nil {
		t.Fatalf("read after the drop: %v", err)
	}
	resumed := false
	for _, s := range p.Peer.Sessions() {
		if !s.Resumed() {
			continue
		}
		resumed = true
		if rp := s.ReportedParameters(); rp == nil || time.Duration(rp.SessionIdleInterval)*time.Millisecond != rspDeviceIdle {
			t.Errorf("controller resumed CASE: reported %+v, want idle %v", rp, rspDeviceIdle)
		}
	}
	if !resumed {
		t.Fatal("no resumed CASE session on the controller")
	}
	wantAll(t, "device resumed CASE", deviceReportedIdle(p.Device.Node(), false), rspControllerIdle)
}
