// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/surface"
)

func TestReportChangedAndAssembledJSON(t *testing.T) {
	t.Parallel()
	before := sample().Endpoints[0]
	after := before.Clone()
	after.Clusters[0].FeatureMap |= 0x2
	at := time.Date(2026, 10, 30, 12, 0, 0, 5, time.UTC)
	r := surface.Report{
		Assembled: true,
		Changed: []surface.SurfaceChange{{
			Endpoint: before.Key, Before: before.Digest(), After: after.Digest(),
			Changes: surface.Diff(surface.Surface{Endpoints: []surface.Endpoint{before}}, surface.Surface{Endpoints: []surface.Endpoint{after}}),
			At:      at,
		}},
	}
	if len(r.Changed[0].Changes) == 0 {
		t.Fatal("the sample change is no difference")
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), `{"assembled":true,"changed":[{"after":"`) || !strings.Contains(string(b), `"kind":"feature"`) {
		t.Fatalf("report JSON: %s", b)
	}
	var back surface.Report
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Assembled || !reflect.DeepEqual(back.Changed, r.Changed) {
		t.Fatalf("round trip: %+v", back.Changed)
	}
	c := r.Clone()
	c.Changed[0].Changes[0].After = "changed"
	if r.Changed[0].Changes[0].After == "changed" || !c.Assembled {
		t.Fatal("Report.Clone shares the changes or drops Assembled")
	}
	for _, bad := range []string{
		`{"policies":"` + strings.Repeat("0", 64) + `","changed":[{"before":"zz","after":""}]}`,
		`{"policies":"` + strings.Repeat("0", 64) + `","changed":[{"before":"` + strings.Repeat("0", 64) + `","after":"00"}]}`,
	} {
		if err := json.Unmarshal([]byte(bad), &back); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}

// TestServerInternalKeepsTheSurfaceChangeSignal: below 1.4.2 the level
// does not know BridgedDeviceBasicInformation's ConfigurationVersion; the
// assembler mounts it on every bridged endpoint and the ledger keeps it
// with the D.9 reason, every other unknown element with the server-internal
// one.
func TestServerInternalKeepsTheSurfaceChangeSignal(t *testing.T) {
	t.Parallel()
	ep := surface.Endpoint{Key: "b", Clusters: []surface.Cluster{{ID: 0x0039, Attributes: []uint32{0x0005, 0x0011, 0x0018}}}}
	got := (surface.Policies{Level: "1.4.1"}).ServerInternal(ep, nil)
	reasons := map[uint32]string{}
	for _, d := range got {
		if d.Kind == surface.KindAttribute {
			reasons[d.ID] = d.Reason
		}
	}
	if reasons[0x0018] != surface.ReasonSurfaceChangeSignal {
		t.Fatalf("ConfigurationVersion at 1.4.1: %q (decisions %v)", reasons[0x0018], got)
	}
	if _, ok := reasons[0x0005]; ok {
		t.Error("NodeLabel is known at 1.4.1 and has no decision")
	}
	if got := (surface.Policies{Level: "1.4.2"}).ServerInternal(ep, nil); len(got) != 0 {
		t.Errorf("at 1.4.2 the level knows ConfigurationVersion: %v", got)
	}
}
