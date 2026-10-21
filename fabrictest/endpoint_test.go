// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestEndpointServesItsServers: an endpoint of fabrictest.Endpoint is
// found by its key, serves the test's server over the wire, and a command
// the server's feature selection leaves out is answered as such.
func TestEndpointServesItsServers(t *testing.T) {
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureOFFONLY})
	if err != nil {
		t.Fatal(err)
	}
	site := fabrictest.NewSite(t, fabrictest.Options{})
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{
		Bridged: []node.Endpoint{fabrictest.Endpoint("offonly:1", onoff.DeviceTypeOnOffPlugInUnit, srv)},
	}})
	ep, ok := p.Device.EndpointOf("offonly:1")
	if !ok {
		t.Fatal("the endpoint has no number")
	}
	ctx := testContext(t)
	cmd := func(id uint32) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: ep, Cluster: onoff.ClusterID, Command: id}
	}
	srv.Set(true)
	if _, err := fabrictest.Invoke(ctx, p, cmd(onoff.CmdOff), nil); err != nil {
		t.Fatalf("Off: %v", err)
	}
	if srv.OnOff() {
		t.Fatal("OnOff still true after Off")
	}
	_, err = fabrictest.Invoke(ctx, p, cmd(onoff.CmdOn), nil)
	var se *interaction.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusUnsupportedCommand {
		t.Fatalf("On on an OffOnly server: %v, want UNSUPPORTED_COMMAND", err)
	}
	got, err := fabrictest.Read(ctx, p, onOffPath(ep))
	if err != nil || got.Value.El.Bool {
		t.Fatalf("OnOff %+v, %v", got, err)
	}
}
