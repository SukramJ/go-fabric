// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package paritytest is the shared support of the cluster packages'
// *_parity_matterjs_test.go files: the cluster entries of
// parity/schema.json, the matter.js HEAD pin, and one evaluator for the
// matter.js conformance expressions those tests check a server's
// attribute, command and event lists against.
//
// It is imported by tests only. Keeping one copy means a conformance form
// the evaluator learns for one cluster is learnt for all of them.
package paritytest

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	matterparity "github.com/SukramJ/go-fabric/parity"
)

// Element is an attribute, command or event of a cluster entry in the
// snapshot. A field the extractor did not emit stays zero: a derived
// cluster (one with a "type" base in matter.js) carries only what it
// overrides.
type Element struct {
	ID          uint32 `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Conformance string `json:"conformance"`
	Access      string `json:"access"`
	Constraint  string `json:"constraint"`
	Quality     string `json:"quality"`
	Priority    string `json:"priority"`
	Direction   string `json:"direction"`
	Response    string `json:"response"`
	Default     any    `json:"default"`
	// Effective is the element as matter.js's operational model resolves
	// it, inherited parts included (the snapshot's resolved layer).
	Effective *Effective `json:"effective"`
}

// Effective is the resolved description of an element or a feature: the
// parts of the snapshot's "effective" object the parity checks compare.
type Effective struct {
	Type        string          `json:"type"`
	Metatype    string          `json:"metatype"`
	Primitive   string          `json:"primitive"`
	Conformance *Text           `json:"conformance"`
	Access      *Access         `json:"access"`
	Quality     map[string]bool `json:"quality"`
	Direction   string          `json:"direction"`
	Response    string          `json:"response"`
	Priority    string          `json:"priority"`
	Title       string          `json:"title"`
}

// Text is a resolved aspect's printed form.
type Text struct {
	Text string `json:"text"`
}

// Access is a resolved access aspect.
type Access struct {
	RW        string `json:"rw"`
	ReadPriv  string `json:"readPriv"`
	WritePriv string `json:"writePriv"`
	Fabric    string `json:"fabric"`
	Timed     bool   `json:"timed"`
}

// Feature is a FeatureMap bit of a cluster entry.
type Feature struct {
	Name        string     `json:"name"`
	Conformance string     `json:"conformance"`
	Bit         uint32     `json:"bit"`
	Effective   *Effective `json:"effective"`
}

// Cluster is one cluster entry of the snapshot. The element lists hold
// pointers, so a range over them does not copy each element.
type Cluster struct {
	ID         uint32     `json:"id"`
	Name       string     `json:"name"`
	Revision   uint16     `json:"revision"`
	Attributes []*Element `json:"attributes"`
	Commands   []*Element `json:"commands"`
	Events     []*Element `json:"events"`
	Features   []Feature  `json:"features"`
	// Base names the cluster this one derives from, empty for none.
	Base string `json:"base"`
}

// Attribute returns the attribute named name, or fails tb.
func (c *Cluster) Attribute(tb testing.TB, name string) *Element {
	tb.Helper()
	return find(tb, c, "attribute", c.Attributes, name)
}

// Command returns the command named name, or fails tb.
func (c *Cluster) Command(tb testing.TB, name string) *Element {
	tb.Helper()
	return find(tb, c, "command", c.Commands, name)
}

// Event returns the event named name, or fails tb.
func (c *Cluster) Event(tb testing.TB, name string) *Element {
	tb.Helper()
	return find(tb, c, "event", c.Events, name)
}

func find(tb testing.TB, c *Cluster, kind string, list []*Element, name string) *Element {
	tb.Helper()
	for _, e := range list {
		if e.Name == name {
			return e
		}
	}
	tb.Fatalf("matter.js %s (0x%04X) has no %s %s", c.Name, c.ID, kind, name)
	return &Element{}
}

// ClusterSnapshot returns the snapshot entry of cluster id, or fails tb.
func ClusterSnapshot(tb testing.TB, id uint32) *Cluster {
	tb.Helper()
	// Each call decodes its own copy, so a test that changes what it was
	// handed cannot change what the next one reads.
	c := &Cluster{}
	raw, ok := clusterIndex()[id]
	if !ok || json.Unmarshal(raw, c) != nil {
		tb.Fatalf("the matter.js schema snapshot has no cluster 0x%04X", id)
	}
	return c
}

// clusterIndex splits the snapshot into its cluster entries once: the
// resolved layer makes the whole snapshot several megabytes, too much to
// decode again for every check. A snapshot that does not parse yields an
// empty index, and every lookup fails (package parity's own test names
// the parse error).
var clusterIndex = sync.OnceValue(func() map[uint32]json.RawMessage {
	var s struct {
		Clusters []json.RawMessage `json:"clusters"`
	}
	_ = json.Unmarshal(matterparity.SchemaJSON(), &s)
	index := make(map[uint32]json.RawMessage, len(s.Clusters))
	for _, raw := range s.Clusters {
		var head struct {
			ID uint32 `json:"id"`
		}
		_ = json.Unmarshal(raw, &head)
		index[head.ID] = raw
	}
	return index
})

// Conformance decides whether a matter.js conformance expression makes an
// element mandatory (required) or permitted (allowed), given which names
// hold — feature names, and for a command's conformance the names of the
// other commands the server supports.
//
// It models the forms the clusters checked here use, as matter.js's
// Conformance evaluates them: comma-separated terms are alternatives tried
// in order ("Resume, O", "PRSCONST, [AUTO]"); a term in brackets is
// optional when it holds; "M" is mandatory, "O" optional; "X" and "D"
// (disallowed, deprecated) never hold; a name holds when names says so;
// "A | B" holds when either side does; a choice suffix ("O.a+") is
// dropped; "Rev >= vN" holds as optional at the shipped revision.
func Conformance(expr string, names map[string]bool) (required, allowed bool) {
	for term := range strings.SplitSeq(expr, ",") {
		term = strings.TrimSpace(term)
		optional := strings.HasPrefix(term, "[") && strings.HasSuffix(term, "]")
		if optional {
			term = strings.TrimSpace(term[1 : len(term)-1])
		}
		if i := strings.Index(term, "."); i >= 0 {
			term = term[:i]
		}
		switch {
		case term == "M":
			return !optional, true
		case term == "O":
			return false, true
		case strings.HasPrefix(term, "Rev"):
			return false, true
		case holds(term, names):
			return !optional, true
		}
	}
	return false, false
}

// holds evaluates a name or a disjunction of names.
func holds(term string, names map[string]bool) bool {
	for alt := range strings.SplitSeq(term, "|") {
		if names[strings.TrimSpace(alt)] {
			return true
		}
	}
	return false
}
