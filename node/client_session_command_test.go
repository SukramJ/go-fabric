// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"net/netip"
	"runtime"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/peer"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// controllerOnClientSession commissions a device with a client stack, ends
// the device's server-stack sessions with the controller and has the
// device open a session to the controller from its client stack: the
// controller's commands then arrive on the session the device opened.
func controllerOnClientSession(t *testing.T) (*fabrictest.Pair, handshake.PeerAddress) {
	t.Helper()
	ctx := parityCtx(t)
	site := tcpSite(t)
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("l:1"), Name: "l"})
	if err != nil {
		t.Fatal(err)
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}, Configure: func(cfg *node.Config) {
		cfg.Client = &node.ClientConfig{}
	}}})
	fabric := oneFabric(t, p.Device)
	ctlAddr := handshake.PeerAddress{FabricIndex: fabric.FabricIndex, NodeID: p.Controller.Fabric().RootNodeID}
	if err := site.Run(ctx, func(ctx context.Context) error {
		p.Device.Node().Sessions().CloseAllGraceful(time.Now().Add(time.Second))
		pr, err := p.Device.Node().Peers().For(ctlAddr)
		if err != nil {
			return err
		}
		// The site answers no operational record for its controller: the
		// connection process is handed the controller's address.
		done := make(chan error, 1)
		go func() {
			_, err := pr.Connect(ctx, peer.ConnectOptions{})
			done <- err
		}()
		at := p.Controller.Addr()
		hint := netip.AddrPortFrom(netip.MustParseAddr(at.IP.String()), uint16(at.Port)) //nolint:gosec // a socket's port
		for pr.HintAddresses(hint) == 0 {
			select {
			case err := <-done:
				return err
			default:
				runtime.Gosched()
			}
		}
		return <-done
	}); err != nil {
		t.Fatalf("the device's own session to the controller: %v", err)
	}
	if n := len(p.Device.Node().Sessions().Sessions()); n != 0 {
		t.Fatalf("the device still holds %d server-stack sessions", n)
	}
	return p, ctlAddr
}

// clientSessionsWith is the device's client-stack sessions with addr.
func clientSessionsWith(d *fabrictest.Device, addr handshake.PeerAddress) int {
	pr := d.Node().Peers().Get(addr)
	if pr == nil {
		return 0
	}
	return len(pr.Sessions())
}

// eventuallyNoClientSession waits until the device holds no client-stack
// session with addr.
func eventuallyNoClientSession(t *testing.T, d *fabrictest.Device, addr handshake.PeerAddress) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); clientSessionsWith(d, addr) != 0; {
		if time.Now().After(deadline) {
			t.Fatal("the invoking client-stack session outlived its command's response")
		}
		runtime.Gosched()
	}
}

// TestCommandOnAClientSessionGetsItsResponse: a RemoveFabric and an
// UpdateNOC the controller sends on the session the device's client stack
// opened run command → hook → sweep on the device and are answered there;
// the session closes once the response is out (review II, Fable R2-1 and
// R3-2; matter.js closes the invoking session after its exchange).
func TestCommandOnAClientSessionGetsItsResponse(t *testing.T) {
	t.Run("RemoveFabric", func(t *testing.T) {
		p, ctlAddr := controllerOnClientSession(t)
		fabric := oneFabric(t, p.Device)
		res, err := fabrictest.Invoke(parityCtx(t), p, im.ConcreteCommandPath{Endpoint: 0, Cluster: ocCluster, Command: opcreds.CmdRemoveFabric},
			opcreds.RemoveFabricRequest{FabricIndex: fabric.FabricIndex})
		if status := nocStatus(t, res, err); status != 0 {
			t.Fatalf("RemoveFabric StatusCode %d, want Ok", status)
		}
		if n := len(p.Device.Node().Sessions().Sessions()); n != 0 {
			t.Fatalf("the RemoveFabric arrived on the server stack (%d sessions)", n)
		}
		eventuallyNoClientSession(t, p.Device, ctlAddr)
	})
	t.Run("UpdateNOC", func(t *testing.T) {
		p, ctlAddr := controllerOnClientSession(t)
		fabric := oneFabric(t, p.Device)
		if status := updateNOC(t, p, p.Controller.Fabric().FabricID, fabric.NodeID); status != 0 {
			t.Fatalf("UpdateNOC StatusCode %d, want Ok", status)
		}
		if n := len(p.Device.Node().Sessions().Sessions()); n != 0 {
			t.Fatalf("the UpdateNOC arrived on the server stack (%d sessions)", n)
		}
		eventuallyNoClientSession(t, p.Device, ctlAddr)
	})
}
