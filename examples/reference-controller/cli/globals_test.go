// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// The global attributes resolve by name for any cluster, generated
// definition or not, and a report names them (matter.js Global.ts:17-23).
func TestGlobalAttributeNames(t *testing.T) {
	for _, tc := range []struct {
		path string
		id   uint32
		name string
	}{
		{"0/OnOff/FeatureMap", spec.AttrFeatureMap, "FeatureMap"},
		{"0/OnOff/clusterrevision", spec.AttrClusterRevision, "ClusterRevision"},
		{"0/0xFFF1FC00/AttributeList", spec.AttrAttributeList, "AttributeList"},
		{"1/OnOff/EventList", spec.AttrEventList, "EventList"},
		{"1/OnOff/AcceptedCommandList", spec.AttrAcceptedCommandList, "AcceptedCommandList"},
		{"1/OnOff/GeneratedCommandList", spec.AttrGeneratedCommandList, "GeneratedCommandList"},
	} {
		p, err := parsePath(tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		ap, a, err := p.attributePath()
		if err != nil || !ap.HasAttribute || ap.Attribute != tc.id || a == nil || a.Name != tc.name {
			t.Fatalf("%s: %+v %v %v", tc.path, ap, a, err)
		}
		if name, _ := attributeName(spec.Lookup(ap.Cluster), tc.id); name != tc.name {
			t.Fatalf("%s: report name %q", tc.path, name)
		}
	}
	if _, a, err := mustPath(t, "0/OnOff/Bogus").attributePath(); err == nil || a != nil {
		t.Fatalf("Bogus resolved: %v %v", a, err)
	}
}

func mustPath(t *testing.T, s string) path {
	t.Helper()
	p, err := parsePath(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
