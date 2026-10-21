// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
)

// sameDevice reports where a and b — one device built twice, as data and
// by its constructor — differ: their type, their servers in order (cluster
// id, Go type, FeatureMap, attribute, command and event lists) and their
// endpoint Spec's client clusters and opt-ins.
func sameDevice(t *testing.T, name string, a, b *Device) {
	t.Helper()
	if a.Type() != b.Type() {
		t.Errorf("%s: types %s and %s", name, a.Type().Name, b.Type().Name)
		return
	}
	as, bs := a.Servers(), b.Servers()
	if len(as) != len(bs) {
		t.Errorf("%s: %d servers and %d", name, len(as), len(bs))
		return
	}
	for i := range as {
		x, y := as[i], bs[i]
		if x.MatterClusterID() != y.MatterClusterID() || reflect.TypeOf(x) != reflect.TypeOf(y) {
			t.Errorf("%s: server %d is 0x%04X %T and 0x%04X %T", name, i, x.MatterClusterID(), x, y.MatterClusterID(), y)
			continue
		}
		fx, _ := x.MatterRead(spec.AttrFeatureMap)
		fy, _ := y.MatterRead(spec.AttrFeatureMap)
		if fx != fy {
			t.Errorf("%s: cluster 0x%04X FeatureMap %v and %v", name, x.MatterClusterID(), fx, fy)
		}
		if l, ok := x.(contract.ClusterAttributeLister); ok {
			if !slices.Equal(l.MatterAttributes(), y.(contract.ClusterAttributeLister).MatterAttributes()) {
				t.Errorf("%s: cluster 0x%04X attribute lists differ", name, x.MatterClusterID())
			}
		}
		if l, ok := x.(contract.ClusterCommandLister); ok {
			if !slices.Equal(l.MatterAcceptedCommands(), y.(contract.ClusterCommandLister).MatterAcceptedCommands()) {
				t.Errorf("%s: cluster 0x%04X command lists differ", name, x.MatterClusterID())
			}
		}
	}
	sa, sb := a.Spec(), b.Spec()
	if !slices.Equal(sa.ClientClusters, sb.ClientClusters) || !slices.Equal(sa.OptIn, sb.OptIn) {
		t.Errorf("%s: client clusters %v / %v, opt-ins %v / %v", name, sa.ClientClusters, sb.ClientClusters, sa.OptIn, sb.OptIn)
	}
}

// TestFromSpecRefusesAnUnknownType: a Spec names a generated device type.
func TestFromSpecRefusesAnUnknownType(t *testing.T) {
	t.Parallel()
	if _, err := FromSpec(Spec{Type: 0xFFF0, Options: testOptions("x")}); !errors.Is(err, ErrConfiguration) {
		t.Errorf("FromSpec(0xFFF0): %v, want ErrConfiguration", err)
	}
}
