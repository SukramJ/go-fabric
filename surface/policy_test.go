// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

func candidates() []surface.Element {
	return []surface.Element{
		{Endpoint: "light", Kind: surface.KindDeviceType, ID: 0x0100, Origin: surface.OriginDeviceType, Mandatory: true},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindCluster, ID: 6, Origin: surface.OriginDeviceType, Mandatory: true},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindFeature, ID: 0x1, Origin: surface.OriginDeviceType, Mandatory: true},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindFeature, ID: 0x4, Origin: surface.OriginHostDeclared},
		{Endpoint: "light", Cluster: 6, Kind: surface.KindAttribute, ID: 0x4001, Origin: surface.OriginHostDeclared},
		{Endpoint: "light", Cluster: 0x0300, Kind: surface.KindCluster, ID: 0x0300, Origin: surface.OriginHostDeclared},
	}
}

// coversEveryCandidate is the completeness rule: every candidate has at
// least one decision.
func coversEveryCandidate(t *testing.T, cands []surface.Element, ds []surface.Decision) {
	t.Helper()
	for _, c := range cands {
		found := false
		for _, d := range ds {
			if d.Element == c {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no decision for %s", c)
		}
	}
}

func TestEvaluateZeroPoliciesKeepsEverything(t *testing.T) {
	cands := candidates()
	kept, ds, err := surface.Evaluate(cands, surface.Policies{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kept, cands) {
		t.Fatalf("kept %v", kept)
	}
	if len(ds) != len(cands) {
		t.Fatalf("%d decisions for %d candidates", len(ds), len(cands))
	}
	for _, d := range ds {
		if d.Verdict != surface.Keep || d.Policy != "" || d.Reason != "" {
			t.Errorf("decision %+v, want a silent Keep", d)
		}
	}
	coversEveryCandidate(t, cands, ds)
}

func TestEvaluateRemoveAndMandatory(t *testing.T) {
	cands := candidates()
	p := surface.Policies{Custom: []surface.Policy{
		surfacetest.KeepAll(),
		surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: 6}),
		surfacetest.Remove(surface.KindCluster, surfacetest.Match{ID: 0x0300}),
	}}
	kept, ds, err := surface.Evaluate(cands, p)
	if err != nil {
		t.Fatal(err)
	}
	keptIDs := make([]uint32, 0, len(kept))
	for _, e := range kept {
		keptIDs = append(keptIDs, e.ID)
	}
	if want := []uint32{0x0100, 6, 0x1, 0x4001}; !reflect.DeepEqual(keptIDs, want) {
		t.Fatalf("kept %#v, want %#v", keptIDs, want)
	}
	coversEveryCandidate(t, cands, ds)
	for _, d := range ds {
		switch {
		case d.Kind == surface.KindFeature && d.ID == 0x1:
			if d.Verdict != surface.Keep || !strings.HasPrefix(d.Reason, "kept: mandatory at pin (surfacetest.Remove ") || d.Policy != "surfacetest.Remove" {
				t.Errorf("mandatory feature: %+v", d)
			}
		case d.Kind == surface.KindFeature && d.ID == 0x4, d.Kind == surface.KindCluster && d.ID == 0x0300:
			if d.Verdict != surface.Remove {
				t.Errorf("optional element: %+v", d)
			}
		default:
			if d.Verdict != surface.Keep || d.Policy != "" {
				t.Errorf("unjudged element: %+v", d)
			}
		}
	}
}

func TestEvaluateRefuse(t *testing.T) {
	cands := candidates()
	p := surface.Policies{Custom: []surface.Policy{
		surfacetest.Remove(surface.KindAttribute, surfacetest.Match{}),
		surfacetest.Refuse(surface.KindAttribute, surfacetest.Match{ID: 0x4001}),
		surfacetest.Refuse(surface.KindDeviceType, surfacetest.Match{}),
	}}
	kept, ds, err := surface.Evaluate(cands, p)
	if !errors.Is(err, surface.ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
	if kept != nil {
		t.Fatalf("kept %v beside a refusal", kept)
	}
	var re *surface.RefusedError
	if !errors.As(err, &re) || len(re.Decisions) != 2 {
		t.Fatalf("RefusedError %+v", re)
	}
	// A Refuse on a mandatory element stays a Refuse.
	if re.Decisions[0].Kind != surface.KindDeviceType || re.Decisions[0].Verdict != surface.Refuse {
		t.Errorf("first refusal %+v", re.Decisions[0])
	}
	for _, want := range []string{`device type 0x0100 on endpoint "light"`, `attribute 0x4001 of cluster 0x0006 on endpoint "light"`, `policy "surfacetest.Refuse"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	coversEveryCandidate(t, cands, ds)
}

type badPolicy struct{}

func (badPolicy) Name() string { return "bad" }
func (badPolicy) Judge(surface.Element) (v surface.Verdict, reason string) {
	return surface.Verdict(9), "?"
}

type explainedKeep struct{}

func (explainedKeep) Name() string { return "explained" }
func (explainedKeep) Judge(surface.Element) (v surface.Verdict, reason string) {
	return surface.Keep, "baseline"
}

func TestEvaluateRejectsBadChains(t *testing.T) {
	if _, _, err := surface.Evaluate(candidates(), surface.Policies{Custom: []surface.Policy{nil}}); err == nil {
		t.Fatal("a nil policy was judged")
	}
	if _, _, err := surface.Evaluate(candidates(), surface.Policies{Custom: []surface.Policy{badPolicy{}}}); err == nil || !strings.Contains(err.Error(), "verdict 9") {
		t.Fatalf("an unknown verdict: %v", err)
	}
	_, ds, err := surface.Evaluate(candidates(), surface.Policies{Custom: []surface.Policy{explainedKeep{}}})
	if err != nil || len(ds) != len(candidates()) || ds[0].Policy != "explained" || ds[0].Reason != "baseline" {
		t.Fatalf("a Keep with a reason is its decision: %v %+v", err, ds)
	}
}

func TestPoliciesDigest(t *testing.T) {
	zero := surface.Policies{}.Digest()
	if zero != (surface.Policies{Custom: []surface.Policy{}}).Digest() {
		t.Fatal("the zero value's digest is not fixed")
	}
	a := surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: 6, ID: 1})}}
	b := surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: 6, ID: 2})}}
	c := surface.Policies{Custom: []surface.Policy{badPolicy{}, nil}}
	if a.Digest() == zero || a.Digest() == b.Digest() || c.Digest() == zero {
		t.Fatal("different policies share a digest")
	}
	if a.Digest() != (surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindFeature, surfacetest.Match{Cluster: 6, ID: 1})}}).Digest() {
		t.Fatal("the same policies have different digests")
	}
}

func TestReportJSON(t *testing.T) {
	cands := candidates()
	p := surface.Policies{Custom: []surface.Policy{surfacetest.Remove(surface.KindFeature, surfacetest.Match{})}}
	_, ds, err := surface.Evaluate(cands, p)
	if err != nil {
		t.Fatal(err)
	}
	r := surface.Report{Policies: p.Digest(), Effective: sample(), Decisions: ds, Withheld: cands[:1]}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), `{"assembled":false,"changed":[],"decisions":[{"cluster":"0x00000000","endpoint":"light","id":"0x0100","kind":"device-type","mandatory":true,"origin":"device-type","policy":"","reason":"","reference":"","verdict":"keep"}`) {
		t.Fatalf("report JSON: %s", b)
	}
	var back surface.Report
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Decisions, r.Decisions) || back.Policies != r.Policies || !reflect.DeepEqual(back.Withheld, r.Withheld) ||
		back.Effective.Digest() != r.Effective.Digest() {
		t.Fatalf("round trip: %+v", back)
	}
	c := r.Clone()
	c.Decisions[0].Reason = "changed"
	if r.Decisions[0].Reason == "changed" {
		t.Fatal("Report.Clone shares the decisions")
	}
	for _, bad := range []string{`{"policies":"zz"}`, `{"policies":"00"}`, `{"decisions":[{"kind":"nope"}]}`, `[`} {
		if err := json.Unmarshal([]byte(bad), &back); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
	if _, err := json.Marshal(surface.Report{Withheld: []surface.Element{{}}}); err == nil {
		t.Error("an element without a kind encoded")
	}
}

func TestEnumNames(t *testing.T) {
	if surface.KindEvent.String() != "event" || surface.OriginKitDefault.String() != "kit-default" || surface.Refuse.String() != "refuse" {
		t.Fatal("enum names")
	}
	if surface.Kind(0).String() != "0" || surface.Origin(77).String() != "77" {
		t.Fatal("an unnamed value renders as its number")
	}
	var o surface.Origin
	if err := o.UnmarshalText([]byte("cluster-mandatory")); err != nil || o != surface.OriginClusterMandatory {
		t.Fatalf("origin %v %v", o, err)
	}
	var v surface.Verdict
	if err := v.UnmarshalText([]byte("remove")); err != nil || v != surface.Remove {
		t.Fatalf("verdict %v %v", v, err)
	}
	if _, err := surface.Verdict(5).MarshalText(); err == nil {
		t.Fatal("an unnamed verdict encoded")
	}
	root := surface.Element{Kind: surface.KindCluster, ID: 0x28}
	if root.String() != "cluster 0x0028 on the root endpoint" {
		t.Fatalf("root element: %s", root)
	}
}
