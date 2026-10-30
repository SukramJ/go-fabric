// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surfacetest_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

func TestPolicies(t *testing.T) {
	onoff := surface.Element{Endpoint: "light", Cluster: 6, Kind: surface.KindFeature, ID: 0x4}
	tests := []struct {
		p    surface.Policy
		e    surface.Element
		want surface.Verdict
	}{
		{surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: 6}), onoff, surface.Remove},
		{surfacetest.Remove(surface.KindFeature, surfacetest.Match{Endpoint: "plug"}), onoff, surface.Keep},
		{surfacetest.Remove(surface.KindFeature, surfacetest.Match{ID: 0x1}), onoff, surface.Keep},
		{surfacetest.Remove(surface.KindAttribute, surfacetest.Match{}), onoff, surface.Keep},
		{surfacetest.Refuse(surface.KindFeature, surfacetest.Match{Endpoint: "light", Cluster: 6, ID: 0x4}), onoff, surface.Refuse},
		{surfacetest.KeepAll(), onoff, surface.Keep},
	}
	for _, tc := range tests {
		v, reason := tc.p.Judge(tc.e)
		if v != tc.want {
			t.Errorf("%s %s: %v, want %v", tc.p.Name(), tc.p.(surface.Describer).Describe(), v, tc.want)
		}
		if (v == surface.Keep) != (reason == "") {
			t.Errorf("%s: verdict %v with reason %q", tc.p.Name(), v, reason)
		}
	}
	if d := surfacetest.KeepAll().(surface.Describer).Describe(); d != "all" {
		t.Errorf("KeepAll describes itself as %q", d)
	}
	d := surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: 6, ID: 2}).(surface.Describer).Describe()
	if !strings.Contains(d, "feature") || !strings.Contains(d, "cluster=0x0006") || !strings.Contains(d, "id=0x2") {
		t.Errorf("Describe = %q", d)
	}
}
