// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// openBasicWindowRequest is OpenBasicCommissioningWindow with a 180 s
// timeout (CommissioningWindowLifetimeTest.ts:157-176 at 862ad75f).
type openBasicWindowRequest struct{ timeout uint16 }

func (r openBasicWindowRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(r.timeout))
	_ = enc.EndContainer()
}

// openBasicCommissioningWindowPath is AdministratorCommissioning
// OpenBasicCommissioningWindow on the root.
var openBasicCommissioningWindowPath = im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003C, Command: 0x01}

// TestCommissioningWindowLifetime_AdministratorBasicWindow ports
// CommissioningWindowLifetimeTest.ts "replaces a window the node opened
// itself with an administrator's basic window" (:157-176 at 862ad75f): on a
// node with the Basic feature (BasicCapableRootEndpoint there,
// node.Commissioning.BasicCommissioning here) the administrator's
// OpenBasicCommissioningWindow replaces the node's own window, the window
// reads BasicWindowOpen both as DeviceCommissioner reports it and on the
// cluster, and closes 181 s later.
func TestCommissioningWindowLifetime_AdministratorBasicWindow(t *testing.T) {
	holdSite(t)
	site := quietSite(t)
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.Commissioning.BasicCommissioning = true
	}}})
	openOwnWindow(t, p.Device)

	if _, err := fabrictest.InvokeTimed(parityCtx(t), p, openBasicCommissioningWindowPath, openBasicWindowRequest{timeout: 180}); err != nil {
		t.Fatalf("OpenBasicCommissioningWindow: %v", err)
	}
	if got := windowStatusOf(p.Device); got != wire.WindowStatusBasic {
		t.Errorf("window status %v, want BasicWindowOpen", got)
	}
	var cluster wire.WindowStatusSnapshot
	if err := p.Device.Act(func(n *node.Node) error {
		cluster = n.Bridge().CommissioningWindow().CurrentWindow()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if cluster.Status != wire.WindowStatusBasic || cluster.AdminFabricIsNull {
		t.Errorf("cluster window %+v, want BasicWindowOpen by the administrator's fabric", cluster)
	}

	site.Fake().Advance(181 * time.Second)
	if got := windowStatusOf(p.Device); got != wire.WindowStatusClosed {
		t.Errorf("181 s later: %v, want WindowNotOpen", got)
	}
}
