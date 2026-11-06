// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/node"
)

// TestTermsAndConditionsFlag: --tc-required enables the register row and
// states version 1 with acknowledgement bit 1, and the root's
// GeneralCommissioning then serves the TC feature with those values;
// without the flag neither the row nor the feature is there.
func TestTermsAndConditionsFlag(t *testing.T) {
	t.Parallel()
	if tc := termsAndConditions(flags{}); tc != nil {
		t.Fatalf("no flag: terms %+v, want nil", tc)
	}
	p, err := surfacePolicies(flags{})
	if err != nil || p.Extensions.Has(termsAndConditionsRow) {
		t.Fatalf("no flag: extensions %v, %v; want no %s", p.Extensions.IDs(), err, termsAndConditionsRow)
	}

	f := flags{tcRequired: true}
	tc := termsAndConditions(f)
	if tc == nil || tc.Version != 1 || tc.Acknowledgements != 1 || tc.UpdateDeadline != nil {
		t.Fatalf("--tc-required: terms %+v, want version 1, acknowledgements 1, no deadline", tc)
	}
	p, err = surfacePolicies(f)
	if err != nil || !p.Extensions.Has(termsAndConditionsRow) {
		t.Fatalf("--tc-required: extensions %v, %v; want %s", p.Extensions.IDs(), err, termsAndConditionsRow)
	}

	read := func(n *node.Node, attr uint32) any {
		t.Helper()
		for _, s := range n.Root().Servers() {
			if s.MatterClusterID() != gencommdef.ClusterID {
				continue
			}
			v, ok := s.MatterRead(attr)
			if !ok {
				t.Fatalf("GeneralCommissioning serves no attribute 0x%04X", attr)
			}
			return v
		}
		t.Fatal("the root serves no GeneralCommissioning")
		return nil
	}
	off := testNode(t, newFleet(), filepath.Join(t.TempDir(), "off.db"))
	if fm := read(off, cluster.AttrGlobalFeatureMap); fm != uint32(0) {
		t.Errorf("without --tc-required: GeneralCommissioning FeatureMap = %v, want 0", fm)
	}
	// The devices are composed under the node's policies (compose's
	// devices.surface).
	devices := newFleet()
	devices.surface = p
	on := testNodeWith(t, devices, filepath.Join(t.TempDir(), "on.db"), func(cfg *node.Config) {
		cfg.Commissioning.TermsAndConditions = termsAndConditions(f)
		cfg.Surface = p
	})
	if fm := read(on, cluster.AttrGlobalFeatureMap); fm != uint32(gencommdef.FeatureTermsAndConditions) {
		t.Errorf("--tc-required: GeneralCommissioning FeatureMap = %v, want TC", fm)
	}
	if v := read(on, gencommdef.AttrTcMinRequiredVersion); v != uint16(1) {
		t.Errorf("TCMinRequiredVersion = %v (%T), want 1", v, v)
	}
	if v := read(on, gencommdef.AttrTcAcknowledgementsRequired); v != true {
		t.Errorf("TCAcknowledgementsRequired = %v, want true before any acceptance", v)
	}
}
