// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	"github.com/SukramJ/go-fabric/im"
)

// TestBridgedBasicInfoServesNodeLabelAndSerialNumberOnlyWhenStated (review
// II, F-db021da0-28, R2-P5-apidev-02, decision 143): SerialNumber and
// NodeLabel are optional and served only when the host stated them — no
// SerialNumber is derived from UniqueID; a NodeLabel the host did not
// state is not listed, not read and not writable; an empty label the host
// stated is served.
func TestBridgedBasicInfoServesNodeLabelAndSerialNumberOnlyWhenStated(t *testing.T) {
	t.Parallel()
	b, err := core.NewBridgedDeviceBasicInformation(core.BridgedConfig{UniqueID: "0123456789abcdef0123456789abcdef", Reachable: true})
	if err != nil {
		t.Fatal(err)
	}
	attrs := b.MatterAttributes()
	for _, a := range []uint32{bdbidef.AttrSerialNumber, bdbidef.AttrNodeLabel} {
		if slices.Contains(attrs, a) {
			t.Errorf("attribute 0x%04X listed although the host stated none", a)
		}
		if _, ok := b.MatterRead(a); ok {
			t.Errorf("attribute 0x%04X read although the host stated none", a)
		}
	}
	if slices.Contains(b.MatterReportable(), bdbidef.AttrNodeLabel) {
		t.Error("NodeLabel reportable although not served")
	}
	err = b.MatterWrite(im.WithFabricFilter(context.Background(), false, 1), bdbidef.AttrNodeLabel, "kitchen")
	if statusOf(err) != im.StatusUnsupportedAttribute {
		t.Errorf("NodeLabel write when not served: %v, want UNSUPPORTED_ATTRIBUTE", err)
	}

	stated, err := core.NewBridgedDeviceBasicInformation(core.BridgedConfig{UniqueID: "0123456789abcdef0123456789abcdef", NodeLabelStated: true, SerialNumber: "SN-1"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := stated.MatterRead(bdbidef.AttrNodeLabel); !ok || v != "" {
		t.Errorf("stated empty NodeLabel = %v, %v; want served as empty", v, ok)
	}
	if v, ok := stated.MatterRead(bdbidef.AttrSerialNumber); !ok || v != "SN-1" {
		t.Errorf("stated SerialNumber = %v, %v", v, ok)
	}
}
