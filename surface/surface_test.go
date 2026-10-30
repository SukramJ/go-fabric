// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/surface"
)

func sample() surface.Surface {
	return surface.Surface{
		Root: surface.Endpoint{
			DeviceTypes: []surface.DeviceTypeRef{{ID: 0x0016, Revision: 3}},
			Clusters:    []surface.Cluster{{ID: 0x001D, Revision: 3, Attributes: []uint32{0, 1, 0xFFFD}}},
		},
		Endpoints: []surface.Endpoint{
			{
				Key:         "light",
				DeviceTypes: []surface.DeviceTypeRef{{ID: 0x0100, Revision: 3}},
				Clusters: []surface.Cluster{{
					ID: 0x0006, Revision: 6, FeatureMap: 0x1,
					Attributes: []uint32{0, 0x4000, 0xFFFD}, Accepted: []uint32{0, 1, 2}, Events: []uint32{},
				}},
			},
			{Key: "aggregator", DeviceTypes: []surface.DeviceTypeRef{{ID: 0x000E, Revision: 2}}},
		},
	}
}

func TestDigestIgnoresNumbersAndOrder(t *testing.T) {
	a := sample()
	b := sample()
	b.Endpoints[0].Number = 3
	b.Root.Number = 0
	b.Endpoints[0], b.Endpoints[1] = b.Endpoints[1], b.Endpoints[0]
	b.Endpoints[1].Clusters[0].Accepted = []uint32{2, 0, 1}
	if a.Digest() != b.Digest() {
		t.Fatal("the digest depends on endpoint numbers or list order")
	}
	if a.Endpoints[0].Digest() != b.Endpoints[1].Digest() {
		t.Fatal("the endpoint digest depends on its number")
	}
	b.Endpoints[1].Clusters[0].FeatureMap = 0x3
	if a.Digest() == b.Digest() {
		t.Fatal("the digest does not see a FeatureMap")
	}
}

func TestMarshalJSONCanonical(t *testing.T) {
	s := sample()
	s.Endpoints[0].Number = 3
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	// Keys sorted, endpoints by key, every list present, the number where
	// set.
	for _, want := range []string{
		`{"endpoints":[{"clusters":[],"deviceTypes":[{"id":"0x000e","revision":2}],"key":"aggregator"}`,
		`"key":"light","number":3}`,
		`"generated":[]`,
		`"root":{"clusters":[{"accepted":[],"attributes":["0x00000000","0x00000001","0x0000fffd"],"events":[],"featureMap":"0x00000000","generated":[],"id":"0x001d","revision":3}]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("canonical JSON lacks %s:\n%s", want, got)
		}
	}
	var back surface.Surface
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	again, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != got {
		t.Fatalf("round trip changed the encoding:\n%s\n%s", got, again)
	}
	if back.Digest() != s.Digest() {
		t.Fatal("round trip changed the digest")
	}
	if err := json.Unmarshal([]byte(`[`), &back); err == nil {
		t.Fatal("malformed JSON decoded")
	}
}

func TestCloneIsDeep(t *testing.T) {
	s := sample()
	c := s.Clone()
	c.Endpoints[0].Clusters[0].Attributes[0] = 99
	c.Endpoints[0].DeviceTypes[0].ID = 99
	if s.Endpoints[0].Clusters[0].Attributes[0] == 99 || s.Endpoints[0].DeviceTypes[0].ID == 99 {
		t.Fatal("Clone shares the lists")
	}
	if got := (surface.Surface{}).Clone(); got.Endpoints != nil {
		t.Fatal("Clone of the zero value grew endpoints")
	}
}

func TestDiff(t *testing.T) {
	a := sample()
	if d := surface.Diff(a, sample()); d == nil || len(d) != 0 {
		t.Fatalf("Diff of equal surfaces = %v, want an empty slice", d)
	}
	b := sample()
	b.Endpoints[0].Number = 7
	if d := surface.Diff(a, b); len(d) != 0 {
		t.Fatalf("a number is a difference: %v", d)
	}
	light := &b.Endpoints[0].Clusters[0]
	light.Revision = 7
	light.FeatureMap = 0x2
	light.Attributes = []uint32{0, 0xFFFD, 0x4001}
	light.Accepted = []uint32{0, 1}
	light.Generated = []uint32{5}
	light.Events = []uint32{1}
	b.Endpoints[0].DeviceTypes[0].Revision = 4
	b.Endpoints[0].Clusters = append(b.Endpoints[0].Clusters, surface.Cluster{ID: 0x0008, Revision: 5})
	b.Endpoints = append(b.Endpoints[:1], surface.Endpoint{Key: "plug", DeviceTypes: []surface.DeviceTypeRef{{ID: 0x010A, Revision: 4}}})
	b.Root.Clusters = nil
	got := surface.Diff(a, b)
	want := []surface.Change{
		{Endpoint: "", Cluster: 0x001D, Kind: surface.KindCluster, ID: 0x001D, Before: "revision 3"},
		{Endpoint: "aggregator", Kind: surface.KindDeviceType, ID: 0x000E, Before: "revision 2"},
		{Endpoint: "light", Kind: surface.KindDeviceType, ID: 0x0100, Before: "revision 3", After: "revision 4"},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindCluster, ID: 6, Before: "revision 6", After: "revision 7"},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindFeature, ID: 0x1, Before: "set"},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindFeature, ID: 0x2, After: "set"},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindAttribute, ID: 0x4000, Before: "served"},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindAttribute, ID: 0x4001, After: "served"},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindCommand, ID: 2, Before: "accepted"},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindCommand, ID: 5, After: "generated"},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindEvent, ID: 1, After: "served"},
		{Endpoint: "light", Cluster: 8, Kind: surface.KindCluster, ID: 8, After: "revision 5"},
		{Endpoint: "plug", Kind: surface.KindDeviceType, ID: 0x010A, After: "revision 4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff:\n got %v\nwant %v", got, want)
	}
	if s := want[4].String(); !strings.Contains(s, `feature 0x1 of cluster 0x0006 on endpoint "light"`) || !strings.Contains(s, `"set" → ""`) {
		t.Errorf("Change.String = %s", s)
	}
}
