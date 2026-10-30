// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

const (
	rowA = "BD-Test-RowA"
	rowB = "BD-Test-RowB"
)

// aheadCandidates are a baseline feature, two ahead features of rowA and
// rowB, and an ahead attribute of rowA that is mandatory.
func aheadCandidates() []surface.Element {
	return []surface.Element{
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindFeature, ID: 1, Origin: surface.OriginHostDeclared},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindFeature, ID: 0x80, Origin: surface.OriginHostDeclared, Reference: surface.Reference{Ahead: rowA}},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindFeature, ID: 0x200, Origin: surface.OriginHostDeclared, Reference: surface.Reference{Ahead: rowB}},
		{Endpoint: "t", Cluster: 0x0201, Kind: surface.KindAttribute, ID: 0x51, Origin: surface.OriginDeviceType, Mandatory: true, Reference: surface.Reference{Ahead: rowA}},
	}
}

func TestReferencePolicyRefusesWhatIsNotEnabled(t *testing.T) {
	cands := aheadCandidates()
	kept, ds, err := surface.Evaluate(cands, surface.Policies{})
	if !errors.Is(err, surface.ErrRefused) || !errors.Is(err, surface.ErrExtensionNotEnabled) {
		t.Fatalf("err = %v, want both ErrRefused and ErrExtensionNotEnabled", err)
	}
	if kept != nil {
		t.Errorf("kept %v beside a refusal", kept)
	}
	coversEveryCandidate(t, cands, ds)
	var re *surface.RefusedError
	if !errors.As(err, &re) || len(re.Decisions) != 3 {
		t.Fatalf("refusals %+v, want three (the mandatory attribute's included)", re)
	}
	for _, d := range re.Decisions {
		if d.Policy != "reference" || d.Verdict != surface.Refuse ||
			d.Reason != "ahead-of-matter.js row "+d.Reference.Ahead+" is not enabled (surface.Policies.Extensions)" {
			t.Errorf("decision %+v", d)
		}
	}
	msg := err.Error()
	for _, want := range []string{rowA, rowB, surface.ErrExtensionNotEnabled.Error() + ": " + rowA + ", " + rowB} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
}

func TestReferencePolicyKeepsWhatIsEnabled(t *testing.T) {
	cands := aheadCandidates()
	_, _, err := surface.Evaluate(cands, surface.Policies{Extensions: surface.NewExtensions(rowA)})
	if !errors.Is(err, surface.ErrExtensionNotEnabled) || strings.Contains(err.Error(), rowA+" is not") {
		t.Fatalf("err = %v, want rowB alone refused", err)
	}
	// An unknown ID beside the two is allowed: a host may enable a row
	// before the module ships it.
	kept, ds, err := surface.Evaluate(cands, surface.Policies{Extensions: surface.NewExtensions(rowA, rowB, "BD-Not-Yet-Shipped")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(kept, cands) {
		t.Errorf("kept %v, want every candidate", kept)
	}
	for _, d := range ds {
		if d.Policy != "" || d.Verdict != surface.Keep {
			t.Errorf("decision %+v, want the silent keep", d)
		}
	}
}

func TestCustomRefusalIsNoExtensionError(t *testing.T) {
	cands := aheadCandidates()[:1]
	_, _, err := surface.Evaluate(cands, surface.Policies{Custom: []surface.Policy{surfacetest.Refuse(surface.KindFeature, surfacetest.Match{})}})
	if !errors.Is(err, surface.ErrRefused) || errors.Is(err, surface.ErrExtensionNotEnabled) {
		t.Fatalf("err = %v, want ErrRefused alone", err)
	}
	if strings.Contains(err.Error(), surface.ErrExtensionNotEnabled.Error()) {
		t.Errorf("error %q names extensions", err)
	}
}

func TestReferencePolicyRefusesAMandatoryElement(t *testing.T) {
	// A Remove of a mandatory element is kept; a Refuse stays a Refuse.
	cands := aheadCandidates()[3:]
	p := surface.Policies{Extensions: surface.NewExtensions(rowB)}
	if _, _, err := surface.Evaluate(cands, p); !errors.Is(err, surface.ErrExtensionNotEnabled) {
		t.Fatalf("err = %v, want the mandatory ahead attribute refused", err)
	}
}

func TestExtensions(t *testing.T) {
	e := surface.NewExtensions("b", "a", "c", "a")
	if !slices.Equal(e.IDs(), []string{"a", "b", "c"}) {
		t.Errorf("IDs() = %v, want sorted and unique", e.IDs())
	}
	if !e.Has("a") || e.Has("d") || surface.Extensions(nil).Has("a") || len(surface.Extensions(nil).IDs()) != 0 {
		t.Error("Has")
	}
}

func TestExtensionsDigest(t *testing.T) {
	zero := surface.Policies{}.Digest()
	if zero != (surface.Policies{Extensions: surface.NewExtensions()}).Digest() {
		t.Error("an empty extension set changes the digest")
	}
	// The zero value's digest is pinned: the encoding is v2 since the
	// extensions joined it, and a host's persisted digest moves with it.
	if got := hex.EncodeToString(zero[:]); got != zeroPoliciesDigest {
		t.Errorf("zero-value digest %s, want %s", got, zeroPoliciesDigest)
	}
	one := surface.Policies{Extensions: surface.NewExtensions(rowA)}.Digest()
	two := surface.Policies{Extensions: surface.NewExtensions(rowA, rowB)}.Digest()
	if one == zero || two == one {
		t.Error("different extensions share a digest")
	}
	if two != (surface.Policies{Extensions: surface.NewExtensions(rowB, rowA)}).Digest() {
		t.Error("the digest depends on the order the IDs were given in")
	}
	custom := surface.Policies{Custom: []surface.Policy{surfacetest.KeepAll()}}
	if (surface.Policies{Extensions: surface.NewExtensions(rowA), Custom: custom.Custom}).Digest() == custom.Digest() {
		t.Error("extensions beside a custom policy do not change the digest")
	}
}

// zeroPoliciesDigest is surface.Policies{}.Digest() in hex.
const zeroPoliciesDigest = "fc6822f08d6c5d98a1632f05ea480c2666bc0701ab657f56500011c73e050c24"

func TestAheadSet(t *testing.T) {
	s := surface.AheadSet{
		rowB: {Features: 0x200, Events: []uint32{3}},
		rowA: {Features: 0x80, Attributes: []uint32{0x51}, Commands: []uint32{4}},
	}
	if !slices.Equal(s.IDs(), []string{rowA, rowB}) {
		t.Errorf("IDs() = %v", s.IDs())
	}
	for _, c := range []struct {
		kind surface.Kind
		id   uint32
		want string
	}{
		{surface.KindFeature, 0x80, rowA},
		{surface.KindFeature, 0x200, rowB},
		{surface.KindFeature, 0x1, ""},
		{surface.KindFeature, 0, ""},
		{surface.KindAttribute, 0x51, rowA},
		{surface.KindCommand, 4, rowA},
		{surface.KindEvent, 3, rowB},
		{surface.KindEvent, 4, ""},
		{surface.KindCluster, 0x0201, ""},
	} {
		if got := s.Row(c.kind, c.id); got != c.want {
			t.Errorf("Row(%s, 0x%X) = %q, want %q", c.kind, c.id, got, c.want)
		}
	}
}

func TestIDsDecodeBothForms(t *testing.T) {
	hexForm := `{"endpoints":[],"root":{"clusters":[{"accepted":["0x00000001"],"attributes":["0x0000FFFD"],"events":[],"featureMap":"0x00000080","generated":[],"id":"0x0201","revision":9}],"deviceTypes":[{"id":"0x0301","revision":4}],"key":""}}`
	decimal := `{"endpoints":[],"root":{"clusters":[{"accepted":[1],"attributes":[65533],"events":[],"featureMap":128,"generated":[],"id":513,"revision":9}],"deviceTypes":[{"id":769,"revision":4}],"key":""}}`
	var a, b surface.Surface
	if err := json.Unmarshal([]byte(hexForm), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(decimal), &b); err != nil {
		t.Fatal(err)
	}
	if len(surface.Diff(a, b)) != 0 || a.Root.Clusters[0].FeatureMap != 0x80 || a.Root.DeviceTypes[0].ID != 0x0301 {
		t.Fatalf("the two forms differ: %+v / %+v", a, b)
	}
	for _, bad := range []string{
		`{"root":{"clusters":[{"id":"0201"}]}}`,
		`{"root":{"clusters":[{"id":"0xzz"}]}}`,
		`{"root":{"clusters":[{"id":-1}]}}`,
		`{"root":{"clusters":[{"id":"0x"}]}}`,
		`{"root":{"clusters":[{"featureMap":"0x100000000"}]}}`,
		`{"root":{"clusters":[{"id":"\x"}]}}`,
	} {
		var s surface.Surface
		if err := json.Unmarshal([]byte(bad), &s); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}
