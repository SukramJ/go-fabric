// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The node's client stack (review II wave B): it states the node's own
// session parameters (R2-P5-peer-02), dials TCP when the node takes the TCP
// client role (R2-P5-peer-01, by meaning matter.js
// packages/node/test/node/ClientNodeTcpTest.ts against a node's client
// stack), and resumes with a peer across a restart from the store's one
// resumption record per peer (R2-P5-port-06, F-db021da0-19).

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// clientPair is a node with a client stack (a) and a node it reaches (b),
// both commissioned onto one controller's fabric.
type clientPair struct {
	site     *fabrictest.Site
	a, b     *fabrictest.Device
	aFabric  node.FabricInfo
	bFabric  node.FabricInfo
	bAddress handshake.PeerAddress
}

func newClientPair(t *testing.T, site *fabrictest.Site, configureA, configureB func(*node.Config)) *clientPair {
	t.Helper()
	ctx := parityCtx(t)
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("b:1"), Name: "b"})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}, Configure: configureB}})
	a := site.AddDevice(fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Client = &node.ClientConfig{}
		if configureA != nil {
			configureA(cfg)
		}
	}})
	if _, err := pair.Controller.Commission(ctx, a, fabrictest.CommissionOptions{}); err != nil {
		t.Fatal(err)
	}
	cp := &clientPair{site: site, a: a, b: pair.Device, aFabric: oneFabric(t, a), bFabric: oneFabric(t, pair.Device)}
	cp.bAddress = handshake.PeerAddress{FabricIndex: cp.aFabric.FabricIndex, NodeID: cp.bFabric.NodeID}
	return cp
}

// connect has a's client stack connect to b with opts and returns the
// session.
func (cp *clientPair) connect(t *testing.T, opts peer.ConnectOptions) (*exchange.Session, error) {
	t.Helper()
	var s *exchange.Session
	err := cp.site.Run(parityCtx(t), func(ctx context.Context) error {
		pr, err := cp.a.Node().Peers().For(cp.bAddress)
		if err != nil {
			return err
		}
		s, err = pr.Connect(ctx, opts)
		return err
	})
	return s, err
}

// TestClientStackStatesTheNodesSessionParameters: a CASE the node opens
// from its client stack states the node's own MRP intervals,
// MaxPathsPerInvoke and TCP roles — the ones its responder states — as
// the peer reads them from Sigma1.
func TestClientStackStatesTheNodesSessionParameters(t *testing.T) {
	site := tcpSite(t)
	cp := newClientPair(t, site, func(cfg *node.Config) {
		cfg.Sessions = &node.SessionIntervals{Idle: 900 * time.Millisecond, Active: 350 * time.Millisecond, ActiveThreshold: 5 * time.Second}
		cfg.MaxPathsPerInvoke = 4
		cfg.Network.TCP = node.TCPConfig{Outgoing: true}
	}, withTCP)
	udp := exchange.TransportUDP
	if _, err := cp.connect(t, peer.ConnectOptions{RequiredTransport: &udp}); err != nil {
		t.Fatal(err)
	}
	// b completes its side of the establishment after the status report
	// that lets a return (Manager.Sessions lists no session still
	// establishing): wait for it.
	var opened []operational.SessionInfo
	waitFor(t, site, "b holds a session a opened", func() bool {
		opened = opened[:0]
		for _, info := range cp.b.Node().Sessions().Sessions() {
			if info.PeerNodeID == cp.aFabric.NodeID && !info.IsPASE {
				opened = append(opened, info)
			}
		}
		return len(opened) > 0
	})
	for _, info := range opened {
		if info.PeerIdleIntervalMs != 900 || info.PeerActiveIntervalMs != 350 || info.PeerActiveThresholdMs != 5000 {
			t.Errorf("b reads a's intervals %d/%d/%d, want 900/350/5000", info.PeerIdleIntervalMs, info.PeerActiveIntervalMs, info.PeerActiveThresholdMs)
		}
		e, err := cp.b.Node().Sessions().Get(info.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if transports, _ := e.PeerTransportParameters(); transports&sessionparams.TransportTCPClient == 0 {
			t.Errorf("b reads a's transports %#x, want the TCP client role", transports)
		}
	}
	// b's responder keeps a's parameters in its resumption record, the one
	// record per peer a CASE client resumes from too (review II, Fable P-5;
	// matter.js CaseServer.ts:339 at db021da0).
	rec, err := cp.b.Node().Store().GetResumptionByPeer(parityCtx(t), cp.bFabric.FabricIndex, cp.aFabric.NodeID)
	if err != nil {
		t.Fatalf("b's resumption record of a: %v", err)
	}
	if p := rec.SessionParameters; p.SessionIdleInterval != 900 || p.SessionActiveInterval != 350 || p.SessionActiveThreshold != 5000 {
		t.Errorf("b's record of a holds %+v, want a's 900/350/5000", p)
	}
}

// TestClientStackLargeMessageOverTCP: with the TCP client role a
// connection that requires TCP — every Large Message invoke — is
// established over TCP; without it the attempt fails at once
// (ClientNodeTcpTest.ts by meaning; peer.ErrNoTCP).
func TestClientStackLargeMessageOverTCP(t *testing.T) {
	tcp := exchange.TransportTCP
	t.Run("client role", func(t *testing.T) {
		cp := newClientPair(t, tcpSite(t), withTCP, withTCP)
		s, err := cp.connect(t, peer.ConnectOptions{RequiredTransport: &tcp})
		if err != nil {
			t.Fatal(err)
		}
		if s.Transport() != exchange.TransportTCP {
			t.Fatalf("session over %v, want TCP", s.Transport())
		}
	})
	t.Run("no client role", func(t *testing.T) {
		cp := newClientPair(t, tcpSite(t), func(cfg *node.Config) { cfg.Network.TCP = node.TCPConfig{Incoming: true} }, withTCP)
		_, err := cp.connect(t, peer.ConnectOptions{RequiredTransport: &tcp, ConnectionTimeout: 10 * time.Second})
		if err == nil {
			t.Fatal("a node without the TCP client role connected over TCP")
		}
	})
}

// TestClientStackResumesAcrossARestart: the node's client stack keeps its
// resumption record in the store's one row per peer, so after a restart it
// resumes with the peer instead of a full CASE.
func TestClientStackResumesAcrossARestart(t *testing.T) {
	site := quietSite(t)
	cp := newClientPair(t, site, nil, nil)
	first, err := cp.connect(t, peer.ConnectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Resumed() {
		t.Fatal("the first session resumed with no record")
	}
	if err := site.Run(parityCtx(t), cp.a.Restart); err != nil {
		t.Fatal(err)
	}
	again, err := cp.connect(t, peer.ConnectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Resumed() {
		t.Fatal("after the restart the node ran a full CASE, want a resumption")
	}
}

// TestResumedSessionKeepsTheRecordsParameters: a resumed session renews the
// record's id and keeps its session parameters — those of the full
// handshake — whatever the resuming Sigma1 states (review II, Fable P-5a;
// matter.js re-saves the old record with the new id, CaseServer.ts:213,
// :219 at db021da0).
func TestResumedSessionKeepsTheRecordsParameters(t *testing.T) {
	site := quietSite(t)
	idle := 900 * time.Millisecond
	cp := newClientPair(t, site, func(cfg *node.Config) {
		cfg.Sessions = &node.SessionIntervals{Idle: idle}
	}, nil)
	if _, err := cp.connect(t, peer.ConnectOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx := parityCtx(t)
	first, err := cp.b.Node().Store().GetResumptionByPeer(ctx, cp.bFabric.FabricIndex, cp.aFabric.NodeID)
	if err != nil || first.SessionParameters.SessionIdleInterval != 900 {
		t.Fatalf("b's record after the full handshake: %+v, %v", first.SessionParameters, err)
	}
	// a restarts stating other intervals and resumes.
	idle = 1200 * time.Millisecond
	if err := site.Run(ctx, cp.a.Restart); err != nil {
		t.Fatal(err)
	}
	again, err := cp.connect(t, peer.ConnectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !again.Resumed() {
		t.Fatal("the second session did not resume")
	}
	rec, err := cp.b.Node().Store().GetResumptionByPeer(ctx, cp.bFabric.FabricIndex, cp.aFabric.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(rec.ResumptionID, first.ResumptionID) {
		t.Error("the resumption id was not renewed")
	}
	if rec.SessionParameters != first.SessionParameters {
		t.Errorf("the resumed session rewrote the record's parameters: %+v, want %+v", rec.SessionParameters, first.SessionParameters)
	}
}
