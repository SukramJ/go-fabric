// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// TestNewCaseAdapterDefaultsSessionParameters pins what Sigma2 advertises
// when the host set nothing: matter.js SessionParameters.defaults, with
// MaxPathsPerInvoke, the revisions and SpecificationVersion equal to what
// BasicInformation reports. A responder the host configured keeps its own.
// Found by the CHIP Python harness (TC-IDM-1.4), whose controller reads
// MaxPathsPerInvoke from the CASE session parameters.
func TestNewCaseAdapterDefaultsSessionParameters(t *testing.T) {
	t.Parallel()
	r := sigma.NewResponder(nil, nil, 1)
	NewCaseAdapter(r)
	p := r.SessionParameters()
	if p == nil {
		t.Fatal("NewCaseAdapter left the responder without session parameters")
	}
	bi, err := mattercore.NewBasicInformation(mattercore.Config{VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "n", VendorName: "v", ProductName: "p", SerialNumber: "s"})
	if err != nil {
		t.Fatal(err)
	}
	read := func(id uint32) uint64 {
		v, _ := bi.MatterRead(id)
		switch x := v.(type) {
		case uint16:
			return uint64(x)
		case uint32:
			return uint64(x)
		}
		t.Fatalf("BasicInformation 0x%04X = %T", id, v)
		return 0
	}
	if uint64(p.MaxPathsPerInvoke) != read(0x0016) || p.MaxPathsPerInvoke != im.DefaultMaxPathsPerInvoke {
		t.Errorf("MaxPathsPerInvoke %d, BasicInformation says %d", p.MaxPathsPerInvoke, read(0x0016))
	}
	if uint64(p.DataModelRevision) != read(0x0000) {
		t.Errorf("DataModelRevision %d, BasicInformation says %d", p.DataModelRevision, read(0x0000))
	}
	if p.SpecificationVersion != cluster.SpecificationVersion || uint64(p.SpecificationVersion) != read(0x0015) {
		t.Errorf("SpecificationVersion 0x%08X", p.SpecificationVersion)
	}
	if p.InteractionModelRevision != uint16(im.MatterInteractionModelRevision) {
		t.Errorf("InteractionModelRevision %d", p.InteractionModelRevision)
	}
	if p.SessionIdleInterval != 500 || p.SessionActiveInterval != 300 || p.SessionActiveThreshold != 4000 {
		t.Errorf("MRP intervals %d/%d/%d, want matter.js SessionIntervals.defaults 500/300/4000",
			p.SessionIdleInterval, p.SessionActiveInterval, p.SessionActiveThreshold)
	}

	own := &sigma.SessionParameters{MaxPathsPerInvoke: 3}
	r2 := sigma.NewResponder(nil, nil, 2)
	r2.SetSessionParameters(own)
	NewCaseAdapter(r2)
	if r2.SessionParameters() != own {
		t.Error("NewCaseAdapter replaced the host's own session parameters")
	}
}
