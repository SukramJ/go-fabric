// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/internal/surfaceseam"
	"github.com/SukramJ/go-fabric/schema"
)

// Confidence is how a row of the controller-profiles register
// (notes/parity/controller_profiles.md) knows what it states: the levels
// of docs/matter-ecosystem-observations.md.
type Confidence uint8

// Confidences. The zero value is no confidence.
const (
	// Reproduced: observed against a real controller during this
	// project's own bring-up, with the evidence named in the row. Only a
	// Reproduced rule removes an element (concept v11 E.5).
	Reproduced Confidence = iota + 1
	// Inherited: taken from matter.js, not independently reproduced here.
	Inherited
	// Reported: a third-party report acted on but not reproduced.
	Reported
)

var confidenceNames = map[Confidence]string{Reproduced: "Reproduced", Inherited: "Inherited", Reported: "Reported"}

// String returns the confidence's name as the register spells it
// ("Reproduced", "Inherited", "Reported").
func (c Confidence) String() string { return enumName(confidenceNames, c) }

// Effect is what a [ProfileRule] does.
type Effect uint8

// Effects. The zero value is no effect.
const (
	// EffectRemove removes the element the rule names — only when the
	// rule's confidence is [Reproduced]; any other confidence warns.
	EffectRemove Effect = iota + 1
	// EffectWarn keeps the element with a warning decision.
	EffectWarn
	// EffectLevel sets the profile's specification level ([Profile].Level).
	EffectLevel
)

var effectNames = map[Effect]string{EffectRemove: "remove", EffectWarn: "warn", EffectLevel: "level"}

// String returns the effect's name as the register spells it ("remove",
// "warn", "level").
func (e Effect) String() string { return enumName(effectNames, e) }

// ElementPattern names the elements a [ProfileRule] is about, on every
// endpoint: a feature of a cluster (ID is its one-bit mask), an attribute,
// a command or an event of a cluster, or a device type (Cluster 0). The
// zero value — a level rule's — matches nothing.
type ElementPattern struct {
	Kind    Kind
	Cluster uint32
	ID      uint32
}

// Matches reports whether e is an element the pattern names.
func (p ElementPattern) Matches(e Element) bool {
	return p.Kind != 0 && e.Kind == p.Kind && e.Cluster == p.Cluster && e.ID == p.ID
}

// String renders the pattern in the register's form: "cluster 0x0201
// feature MSCH" (the feature's code from the pin's schema, its mask when
// the schema names none), "cluster 0x0006 attribute 0x4000", "device-type
// 0x0301", and "level" for the zero value.
func (p ElementPattern) String() string {
	switch p.Kind {
	case 0:
		return "level"
	case KindDeviceType:
		return fmt.Sprintf("device-type 0x%04X", p.ID)
	case KindFeature:
		for _, f := range schema.ClusterFeatures(p.Cluster) {
			if f.Bit < 32 && uint32(1)<<f.Bit == p.ID {
				return fmt.Sprintf("cluster 0x%04X feature %s", p.Cluster, f.Name)
			}
		}
		return fmt.Sprintf("cluster 0x%04X feature 0x%X", p.Cluster, p.ID)
	default:
		return fmt.Sprintf("cluster 0x%04X %s 0x%04X", p.Cluster, p.Kind, p.ID)
	}
}

// ProfileRule is one row of the controller-profiles register.
type ProfileRule struct {
	// Element is the register row's Element cell; the zero value for a
	// level row.
	Element ElementPattern
	Effect  Effect
	// Level is the level an [EffectLevel] row sets.
	Level      Level
	Confidence Confidence
	// Source names the evidence; Reason is the row's one sentence.
	Source, Reason string
}

// Profile is a named controller ecosystem's tolerances (concept v11 §C.6,
// ADR 0030 decision 8): rules that remove or warn about elements, and
// optionally the level the profile implies. It is the device-side twin of
// the client side's eligibility.Compat. [Profiles] are generated from the
// evidence register notes/parity/controller_profiles.md
// (script/profilegen); a host selects them by name in [Policies].Profiles.
type Profile struct {
	Name string
	// Level is the level a register row sets for the profile; "" when no
	// row sets one.
	Level Level
	Rules []ProfileRule
}

// Profiles returns every controller profile of the register, sorted by
// name. A profile without a row is a name with no effect.
func Profiles() []Profile {
	out := make([]Profile, len(registerProfiles))
	for i, p := range registerProfiles {
		out[i] = p.clone()
	}
	return out
}

// ProfileByName returns the register's profile called name.
func ProfileByName(name string) (Profile, bool) {
	for _, p := range registerProfiles {
		if p.Name == name {
			return p.clone(), true
		}
	}
	return Profile{}, false
}

func (p Profile) clone() Profile {
	p.Rules = slices.Clone(p.Rules)
	return p
}

// ErrUnknownProfile is returned by [Policies.Validate] for a profile name
// the register does not list.
var ErrUnknownProfile = errors.New("surface: unknown controller profile")

// ErrProfileLevelConflict is returned by [Policies.Validate] when two
// selected profiles imply different levels and no explicit
// [Policies].Level decides.
var ErrProfileLevelConflict = errors.New("surface: selected controller profiles imply different levels")

// profileNames returns the selected profile names, sorted, each once.
func (p Policies) profileNames() []string {
	names := slices.Clone(p.Profiles)
	slices.Sort(names)
	return slices.Compact(names)
}

// lookupProfile resolves name: a profile the test seam injected first,
// then the register's.
func (p Policies) lookupProfile(name string) (Profile, bool) {
	for _, q := range p.injected {
		if q.Name == name {
			return q, true
		}
	}
	return ProfileByName(name)
}

// selectedProfiles resolves the selected names, sorted; an unknown name is
// skipped ([Policies.Validate] rejects it).
func (p Policies) selectedProfiles() []Profile {
	var out []Profile
	for _, n := range p.profileNames() {
		if q, ok := p.lookupProfile(n); ok {
			out = append(out, q)
		}
	}
	return out
}

// profileLevel returns the level the selected profiles agree on and the
// names of the profiles that set it; "" when none sets one. ok is false
// when two set different levels.
func (p Policies) profileLevel() (level Level, from []string, ok bool) {
	for _, q := range p.selectedProfiles() {
		if q.Level == "" {
			continue
		}
		if level != "" && q.Level != level {
			return "", []string{from[0], q.Name}, false
		}
		level = q.Level
		from = append(from, q.Name)
	}
	return level, from, true
}

// LevelOrigin names where [Policies.EffectiveLevel] comes from:
// "explicit" for [Policies].Level, "profile <name>[, <name>…]" for the
// selected profiles that set it, "pin" otherwise.
func (p Policies) LevelOrigin() string {
	if p.Level != "" {
		return "explicit"
	}
	if l, from, ok := p.profileLevel(); ok && l != "" {
		return "profile " + strings.Join(from, ", ")
	}
	return "pin"
}

// validateProfiles rejects an unknown profile name ([ErrUnknownProfile],
// naming the known ones) and, without an explicit level, two selected
// profiles that imply different levels ([ErrProfileLevelConflict], naming
// both).
func (p Policies) validateProfiles() error {
	for _, n := range p.profileNames() {
		if _, ok := p.lookupProfile(n); !ok {
			known := make([]string, 0, len(registerProfiles))
			for _, q := range registerProfiles {
				known = append(known, q.Name)
			}
			return fmt.Errorf("%w: %q (profiles: %s)", ErrUnknownProfile, n, strings.Join(known, ", "))
		}
	}
	if p.Level != "" {
		return nil
	}
	if _, from, ok := p.profileLevel(); !ok {
		a, _ := p.lookupProfile(from[0])
		b, _ := p.lookupProfile(from[1])
		return fmt.Errorf("%w: %q implies %s, %q implies %s; set the level explicitly",
			ErrProfileLevelConflict, a.Name, a.Level, b.Name, b.Level)
	}
	for _, q := range p.selectedProfiles() {
		if q.Level != "" && !slices.Contains(schema.Levels(), q.Level) {
			return fmt.Errorf("%w: %q, the level of profile %q", ErrUnknownLevel, q.Level, q.Name)
		}
	}
	return nil
}

// profilePolicyPrefix opens a built-in profile policy's name.
const profilePolicyPrefix = "profile:"

// profilePolicy is the built-in policy of one selected profile, judged
// after the level policy: a Reproduced remove rule that matches removes
// the element (the chain keeps a mandatory one); a warn rule — or a
// remove rule of any other confidence — keeps it with a warning decision.
type profilePolicy struct{ profile Profile }

func (p profilePolicy) Name() string { return profilePolicyPrefix + p.profile.Name }

func (p profilePolicy) Judge(el Element) (v Verdict, reason string) {
	var warns []string
	for _, r := range p.profile.Rules {
		if r.Effect == EffectLevel || !r.Element.Matches(el) {
			continue
		}
		if r.Effect == EffectRemove && r.Confidence == Reproduced {
			return Remove, fmt.Sprintf("removed by profile %s: %s (%s)", p.profile.Name, r.Reason, r.Source)
		}
		warns = append(warns, fmt.Sprintf("warning (%s) profile %s: %s (%s)", r.Confidence, p.profile.Name, r.Reason, r.Source))
	}
	return Keep, strings.Join(warns, "; ")
}

// describe is the profile's rules in the digest's form, for a profile the
// test seam injected: the register's are identified by their name.
func (p Profile) describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "level=%s", p.Level)
	for _, r := range p.Rules {
		fmt.Fprintf(&b, ";%s|%s|%s|%s|%s|%s", r.Element, r.Effect, r.Level, r.Confidence, r.Source, r.Reason)
	}
	return b.String()
}

func init() {
	surfaceseam.WithProfiles = func(policies, profiles any) (any, bool) {
		p, ok := policies.(Policies)
		qs, ok2 := profiles.([]Profile)
		if !ok || !ok2 {
			return nil, false
		}
		p.injected = slices.Clone(p.injected)
		p.Profiles = slices.Clone(p.Profiles)
		for _, q := range qs {
			p.injected = append(p.injected, q.clone())
			p.Profiles = append(p.Profiles, q.Name)
		}
		return p, true
	}
}

// Report returns the head of a report composed under p: the policies'
// digest, the effective level and its origin, the selected profiles and
// the enabled extensions, each sorted and each once; no surface and no
// decision. device.Device and the
// node start their reports with it.
func (p Policies) Report() Report {
	r := Report{Policies: p.Digest(), Level: p.EffectiveLevel(), LevelOrigin: p.LevelOrigin()}
	if names := p.profileNames(); len(names) > 0 {
		r.Profiles = names
	}
	if ids := p.Extensions.IDs(); len(ids) > 0 {
		r.Extensions = ids
	}
	return r
}
