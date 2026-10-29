// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package surfacetest has the policies the tests of a surface policy
// chain use: one that removes, one that refuses and one that keeps the
// elements it matches. They prove the chain ([surface.Evaluate]) on a real
// composition without a built-in policy.
package surfacetest

import (
	"fmt"

	"github.com/SukramJ/go-fabric/surface"
)

// Match selects elements by endpoint, cluster and id. A zero field matches
// every value; the zero Match matches every element of its kind.
type Match struct {
	Endpoint string
	Cluster  uint32
	ID       uint32
}

func (m Match) matches(e surface.Element) bool {
	return (m.Endpoint == "" || m.Endpoint == e.Endpoint) &&
		(m.Cluster == 0 || m.Cluster == e.Cluster) &&
		(m.ID == 0 || m.ID == e.ID)
}

func (m Match) String() string {
	return fmt.Sprintf("endpoint=%q cluster=0x%04X id=0x%X", m.Endpoint, m.Cluster, m.ID)
}

// policy judges the elements of one kind that match.
type policy struct {
	name    string
	kind    surface.Kind
	match   Match
	verdict surface.Verdict
}

// Remove returns a policy that removes every element of kind that m
// matches.
func Remove(kind surface.Kind, m Match) surface.Policy {
	return policy{name: "surfacetest.Remove", kind: kind, match: m, verdict: surface.Remove}
}

// Refuse returns a policy that refuses every element of kind that m
// matches.
func Refuse(kind surface.Kind, m Match) surface.Policy {
	return policy{name: "surfacetest.Refuse", kind: kind, match: m, verdict: surface.Refuse}
}

// KeepAll returns a policy that keeps every element without a word: the
// chain records no decision of it.
func KeepAll() surface.Policy {
	return policy{name: "surfacetest.KeepAll", verdict: surface.Keep}
}

// Name implements [surface.Policy].
func (p policy) Name() string { return p.name }

// Describe implements [surface.Describer].
func (p policy) Describe() string {
	if p.kind == 0 {
		return "all"
	}
	return p.kind.String() + " " + p.match.String()
}

// Judge implements [surface.Policy].
func (p policy) Judge(e surface.Element) (surface.Verdict, string) {
	if p.verdict == surface.Keep || e.Kind != p.kind || !p.match.matches(e) {
		return surface.Keep, ""
	}
	return p.verdict, fmt.Sprintf("%s %s", p.name, p.Describe())
}
