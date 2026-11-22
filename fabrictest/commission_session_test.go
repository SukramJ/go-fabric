// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"

	"thingfabric.dev/go-fabric/fabrictest"
)

// Controller.Commission returns only once the device lists the CASE
// session the commissioning established: the device completes its side
// of CASE after the Success that lets the controller go on, and lists no
// session still establishing (operational.Manager.Sessions), so a test
// reading the device's sessions right after CommissionedPair found none
// now and then.
func TestCommissionReturnsOnceTheDeviceListsTheCASESession(t *testing.T) {
	t.Parallel()
	for name, opts := range map[string]fabrictest.Options{
		"in-memory": {},
		"loopback":  {Loopback: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			site := fabrictest.NewSite(t, opts)
			p := site.CommissionedPair(fabrictest.PairOptions{})
			for _, s := range p.Device.Node().Sessions().Sessions() {
				if !s.IsPASE && s.LocalNodeID == p.Address.NodeID {
					return
				}
			}
			t.Fatalf("device lists no CASE session for node %#x at Commission's return: %+v", p.Address.NodeID, p.Device.Node().Sessions().Sessions())
		})
	}
}
