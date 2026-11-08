// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	gendiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/contract"
)

// staticFaults is a fault port whose lists never change.
type staticFaults contract.Faults

func (f staticFaults) MatterActiveFaults() contract.Faults { return contract.Faults(f) }
func (staticFaults) OnMatterValueChanged(func()) func()    { return func() {} }

// Config.Faults reaches the root GeneralDiagnostics: the three lists join
// its attribute list and serve the host's faults; without it they stay
// absent (issue 439).
func TestConfigFaultsServedByRootGeneralDiagnostics(t *testing.T) {
	served := func(n *Node) contract.ClusterServer {
		t.Helper()
		for _, s := range n.Root().Servers() {
			if s.MatterClusterID() == gendiagdef.ClusterID {
				return s
			}
		}
		t.Fatal("the root serves no GeneralDiagnostics")
		return nil
	}
	attrs := func(s contract.ClusterServer) []uint32 {
		return s.(contract.ClusterAttributeLister).MatterAttributes()
	}

	off := served(newMDNSTestNode(t, mdnsTestConfig(t)))
	if slices.Contains(attrs(off), gendiagdef.AttrActiveHardwareFaults) {
		t.Fatal("ActiveHardwareFaults listed without Config.Faults")
	}

	cfg := mdnsTestConfig(t)
	cfg.Faults = staticFaults{Hardware: []uint8{uint8(gendiagdef.HardwareFaultSensor)}}
	on := served(newMDNSTestNode(t, cfg))
	for _, a := range []uint32{gendiagdef.AttrActiveHardwareFaults, gendiagdef.AttrActiveRadioFaults, gendiagdef.AttrActiveNetworkFaults} {
		if !slices.Contains(attrs(on), a) {
			t.Fatalf("attribute 0x%04X not listed with Config.Faults", a)
		}
	}
	if _, ok := on.MatterRead(gendiagdef.AttrActiveHardwareFaults); !ok {
		t.Fatal("ActiveHardwareFaults not served")
	}
	if fm, _ := on.MatterRead(cluster.AttrGlobalFeatureMap); fm == nil {
		t.Fatal("no FeatureMap")
	}
}
