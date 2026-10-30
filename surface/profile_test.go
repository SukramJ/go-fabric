// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/surface/surfacetest"
)

// msch is the Thermostat's MatterScheduleConfiguration feature as a
// candidate: the register's one row names it.
var msch = surface.Element{Endpoint: "tstat", Cluster: 0x0201, Kind: surface.KindFeature, ID: 0x80, Origin: surface.OriginHostDeclared}

func TestProfilesFromTheRegister(t *testing.T) {
	ps := surface.Profiles()
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
		for _, r := range p.Rules {
			// The register's rule, enforced on the generated table too.
			if r.Effect != surface.EffectWarn && r.Confidence != surface.Reproduced {
				t.Errorf("profile %s: a %s rule with confidence %s", p.Name, r.Effect, r.Confidence)
			}
		}
	}
	if want := []string{"alexa", "apple-home", "google-home", "home-assistant", "smartthings"}; !slices.Equal(names, want) {
		t.Fatalf("profiles %v, want %v", names, want)
	}
	apple, ok := surface.ProfileByName("apple-home")
	if !ok || apple.Level != "" || len(apple.Rules) != 1 {
		t.Fatalf("apple-home: %+v %v", apple, ok)
	}
	r := apple.Rules[0]
	if r.Element != (surface.ElementPattern{Kind: surface.KindFeature, Cluster: 0x0201, ID: 0x80}) ||
		r.Effect != surface.EffectWarn || r.Confidence != surface.Reported || r.Source == "" || r.Reason == "" {
		t.Fatalf("apple-home rule: %+v", r)
	}
	apple.Rules[0].Reason = "changed"
	if again, _ := surface.ProfileByName("apple-home"); again.Rules[0].Reason == "changed" {
		t.Fatal("ProfileByName shares the table's rules")
	}
	ps[1].Rules[0].Reason = "changed"
	if again := surface.Profiles(); again[1].Rules[0].Reason == "changed" {
		t.Fatal("Profiles shares the table's rules")
	}
	if _, ok := surface.ProfileByName("nope"); ok {
		t.Fatal("an unknown profile resolved")
	}
}

func TestElementPattern(t *testing.T) {
	for _, c := range []struct {
		p    surface.ElementPattern
		want string
	}{
		{surface.ElementPattern{}, "level"},
		{surface.ElementPattern{Kind: surface.KindDeviceType, ID: 0x0301}, "device-type 0x0301"},
		{surface.ElementPattern{Kind: surface.KindFeature, Cluster: 0x0201, ID: 0x80}, "cluster 0x0201 feature MSCH"},
		{surface.ElementPattern{Kind: surface.KindFeature, Cluster: 0x0201, ID: 1 << 30}, "cluster 0x0201 feature 0x40000000"},
		{surface.ElementPattern{Kind: surface.KindAttribute, Cluster: 0x0201, ID: 0x0023}, "cluster 0x0201 attribute 0x0023"},
		{surface.ElementPattern{Kind: surface.KindCommand, Cluster: 6, ID: 2}, "cluster 0x0006 command 0x0002"},
		{surface.ElementPattern{Kind: surface.KindEvent, Cluster: 0x0201, ID: 0}, "cluster 0x0201 event 0x0000"},
	} {
		if got := c.p.String(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.p, got, c.want)
		}
	}
	p := surface.ElementPattern{Kind: surface.KindFeature, Cluster: 0x0201, ID: 0x80}
	if !p.Matches(msch) || p.Matches(surface.Element{Cluster: 0x0201, Kind: surface.KindAttribute, ID: 0x80}) ||
		p.Matches(surface.Element{Cluster: 0x0202, Kind: surface.KindFeature, ID: 0x80}) {
		t.Fatal("Matches")
	}
	if (surface.ElementPattern{}).Matches(surface.Element{}) {
		t.Fatal("the zero pattern matched")
	}
	if surface.Reproduced.String() != "Reproduced" || surface.Inherited.String() != "Inherited" || surface.Reported.String() != "Reported" ||
		surface.EffectRemove.String() != "remove" || surface.EffectWarn.String() != "warn" || surface.EffectLevel.String() != "level" {
		t.Fatal("confidence and effect names")
	}
}

// TestProfileWarnRow: the shipped register's one row keeps MSCH with a
// warning decision under apple-home, and removes nothing.
func TestProfileWarnRow(t *testing.T) {
	cands := append(candidates(), msch)
	kept, ds, err := surface.Evaluate(cands, surface.Policies{Profiles: []string{"apple-home"}})
	if err != nil || len(kept) != len(cands) {
		t.Fatalf("kept %d of %d: %v", len(kept), len(cands), err)
	}
	coversEveryCandidate(t, cands, ds)
	apple, _ := surface.ProfileByName("apple-home")
	want := "warning (Reported) profile apple-home: " + apple.Rules[0].Reason + " (" + apple.Rules[0].Source + ")"
	var rows []surface.Decision
	for _, d := range ds {
		if d.Policy != "" {
			rows = append(rows, d)
		}
	}
	if len(rows) != 1 || rows[0].Element != msch || rows[0].Policy != "profile:apple-home" || rows[0].Verdict != surface.Warn || rows[0].Reason != want {
		t.Fatalf("decisions %+v", rows)
	}
}

// TestProfileRemoveRule runs a Reproduced remove rule the register does
// not carry, through the test seam.
func TestProfileRemoveRule(t *testing.T) {
	onOffLT := surface.ElementPattern{Kind: surface.KindFeature, Cluster: 6, ID: 0x1}
	onOffDF := surface.ElementPattern{Kind: surface.KindFeature, Cluster: 6, ID: 0x4}
	attr := surface.ElementPattern{Kind: surface.KindAttribute, Cluster: 6, ID: 0x4001}
	prof := surface.Profile{Name: "synthetic", Rules: []surface.ProfileRule{
		{Element: onOffLT, Effect: surface.EffectRemove, Confidence: surface.Reproduced, Source: "s1", Reason: "r1"},
		{Element: onOffDF, Effect: surface.EffectRemove, Confidence: surface.Reproduced, Source: "s2", Reason: "r2"},
		// Only a Reproduced rule removes: a Reported remove warns.
		{Element: attr, Effect: surface.EffectRemove, Confidence: surface.Reported, Source: "s3", Reason: "r3"},
	}}
	p := surfacetest.WithProfiles(surface.Policies{}, prof)
	if !slices.Equal(p.Profiles, []string{"synthetic"}) {
		t.Fatalf("selected %v", p.Profiles)
	}
	cands := candidates()
	kept, ds, err := surface.Evaluate(cands, p)
	if err != nil {
		t.Fatal(err)
	}
	coversEveryCandidate(t, cands, ds)
	if len(kept) != len(cands)-1 || slices.Contains(kept, cands[3]) {
		t.Fatalf("kept %v", kept)
	}
	byElement := map[surface.Element]surface.Decision{}
	for _, d := range ds {
		if d.Policy == "profile:synthetic" {
			byElement[d.Element] = d
		}
	}
	if d := byElement[cands[3]]; d.Verdict != surface.Remove || d.Reason != "removed by profile synthetic: r2 (s2)" {
		t.Errorf("optional feature: %+v", d)
	}
	if d := byElement[cands[2]]; d.Verdict != surface.Keep || d.Reason != "kept: mandatory at pin (removed by profile synthetic: r1 (s1))" {
		t.Errorf("mandatory feature: %+v", d)
	}
	if d := byElement[cands[4]]; d.Verdict != surface.Warn || d.Reason != "warning (Reported) profile synthetic: r3 (s3)" {
		t.Errorf("Reported remove: %+v", d)
	}
}

func TestProfileLevel(t *testing.T) {
	levels := schema.Levels()
	low, mid, pin := levels[0], levels[1], surface.PinLevel()
	a := surface.Profile{Name: "a", Level: low, Rules: []surface.ProfileRule{{Effect: surface.EffectLevel, Level: low, Confidence: surface.Reproduced, Source: "s", Reason: "r"}}}
	a2 := surface.Profile{Name: "a2", Level: low}
	b := surface.Profile{Name: "b", Level: mid}
	plain := surface.Profile{Name: "plain"}

	if p := (surface.Policies{}); p.EffectiveLevel() != pin || p.LevelOrigin() != "pin" {
		t.Fatalf("zero: %s %s", p.EffectiveLevel(), p.LevelOrigin())
	}
	p := surfacetest.WithProfiles(surface.Policies{}, a, plain)
	if p.Validate() != nil || p.EffectiveLevel() != low || p.LevelOrigin() != "profile a" {
		t.Fatalf("one profile: %v %s %s", p.Validate(), p.EffectiveLevel(), p.LevelOrigin())
	}
	p = surfacetest.WithProfiles(surface.Policies{}, a, a2)
	if p.Validate() != nil || p.EffectiveLevel() != low || p.LevelOrigin() != "profile a, a2" {
		t.Fatalf("two agreeing: %v %s %s", p.Validate(), p.EffectiveLevel(), p.LevelOrigin())
	}
	p = surfacetest.WithProfiles(surface.Policies{}, a, b)
	err := p.Validate()
	if !errors.Is(err, surface.ErrProfileLevelConflict) || !strings.Contains(err.Error(), `"a"`) || !strings.Contains(err.Error(), `"b"`) {
		t.Fatalf("conflict: %v", err)
	}
	if _, _, err := surface.Evaluate(candidates(), p); !errors.Is(err, surface.ErrProfileLevelConflict) {
		t.Fatalf("Evaluate under a conflict: %v", err)
	}
	if p.EffectiveLevel() != pin || p.LevelOrigin() != "pin" {
		t.Fatalf("conflict level: %s %s", p.EffectiveLevel(), p.LevelOrigin())
	}
	p = surfacetest.WithProfiles(surface.Policies{Level: pin}, a, b)
	if p.Validate() != nil || p.EffectiveLevel() != pin || p.LevelOrigin() != "explicit" {
		t.Fatalf("explicit wins: %v %s %s", p.Validate(), p.EffectiveLevel(), p.LevelOrigin())
	}
	bad := surfacetest.WithProfiles(surface.Policies{}, surface.Profile{Name: "bad", Level: "0.9"})
	if err := bad.Validate(); !errors.Is(err, surface.ErrUnknownLevel) {
		t.Fatalf("a profile's unknown level: %v", err)
	}
	rep := surfacetest.WithProfiles(surface.Policies{}, a).Report()
	if rep.Level != low || rep.LevelOrigin != "profile a" || !slices.Equal(rep.Profiles, []string{"a"}) {
		t.Fatalf("report head: %+v", rep)
	}
}

func TestUnknownProfile(t *testing.T) {
	err := surface.Policies{Profiles: []string{"apple-home", "nope"}}.Validate()
	if !errors.Is(err, surface.ErrUnknownProfile) || !strings.Contains(err.Error(), `"nope"`) || !strings.Contains(err.Error(), "apple-home, google-home") {
		t.Fatalf("%v", err)
	}
	if err := (surface.Policies{Level: "0.9", Profiles: []string{"nope"}}).Validate(); !errors.Is(err, surface.ErrUnknownLevel) {
		t.Fatalf("the level is checked first: %v", err)
	}
	if err := (surface.Policies{Profiles: []string{"alexa", "apple-home"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProfilesDigest(t *testing.T) {
	zero := surface.Policies{}.Digest()
	if (surface.Policies{Profiles: []string{}}).Digest() != zero {
		t.Fatal("an empty selection changed the digest")
	}
	apple := surface.Policies{Profiles: []string{"apple-home"}}.Digest()
	both := surface.Policies{Profiles: []string{"apple-home", "alexa"}}.Digest()
	if apple == zero || both == apple {
		t.Fatal("a selection shares a digest")
	}
	if (surface.Policies{Profiles: []string{"alexa", "apple-home", "alexa"}}).Digest() != both {
		t.Fatal("the digest depends on the selection's order or repetition")
	}
	r1 := surface.Profile{Name: "apple-home", Rules: []surface.ProfileRule{{Element: surface.ElementPattern{Kind: surface.KindFeature, Cluster: 6, ID: 1}, Effect: surface.EffectRemove, Confidence: surface.Reproduced, Source: "s", Reason: "r"}}}
	r2 := r1
	r2.Rules = []surface.ProfileRule{{Element: surface.ElementPattern{Kind: surface.KindFeature, Cluster: 6, ID: 2}, Effect: surface.EffectRemove, Confidence: surface.Reproduced, Source: "s", Reason: "r"}}
	d1 := surfacetest.WithProfiles(surface.Policies{}, r1).Digest()
	if d1 == apple || d1 == surfacetest.WithProfiles(surface.Policies{}, r2).Digest() {
		t.Fatal("an injected profile is identified by its name alone")
	}
	if d1 != surfacetest.WithProfiles(surface.Policies{}, r1).Digest() {
		t.Fatal("the same injected profile has two digests")
	}
	// A profile's level enters the digest as the effective level.
	low := schema.Levels()[0]
	if surfacetest.WithProfiles(surface.Policies{}, surface.Profile{Name: "x", Level: low}).Digest() ==
		surfacetest.WithProfiles(surface.Policies{}, surface.Profile{Name: "x"}).Digest() {
		t.Fatal("a profile's level is not in the digest")
	}
}

func TestReportProfilesJSON(t *testing.T) {
	r := surface.Policies{Profiles: []string{"apple-home"}}.Report()
	if r.LevelOrigin != "pin" || !slices.Equal(r.Profiles, []string{"apple-home"}) {
		t.Fatalf("head %+v", r)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"level_origin":"pin"`) || !strings.Contains(string(b), `"profiles":["apple-home"]`) {
		t.Fatalf("%s", b)
	}
	var back surface.Report
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back.Profiles, r.Profiles) || back.LevelOrigin != "pin" {
		t.Fatalf("round trip %+v %v", back, err)
	}
	c := r.Clone()
	c.Profiles[0] = "changed"
	if r.Profiles[0] == "changed" {
		t.Fatal("Clone shares the profiles")
	}
	b, _ = json.Marshal(surface.Policies{}.Report())
	if !strings.Contains(string(b), `"profiles":[]`) {
		t.Fatalf("no selection: %s", b)
	}
	if err := json.Unmarshal(b, &back); err != nil || back.Profiles != nil {
		t.Fatalf("no selection decodes to nil: %v %v", back.Profiles, err)
	}
}
