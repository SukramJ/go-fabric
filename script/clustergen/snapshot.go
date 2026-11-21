// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// The resolved layer of parity/schema.json, as script/extract-from-matter-js.ts
// emits it: every element's "effective" object, the clusters' "datatypes"
// and "base", and the snapshot's "globalDatatypes". The raw layer (the
// element files' text) is not read here.

type snapshot struct {
	Matter struct {
		Revision     string `json:"revision"`
		SourceCommit string `json:"sourceCommit"`
	} `json:"matter"`
	Clusters        []*cluster  `json:"clusters"`
	GlobalDatatypes []*datatype `json:"globalDatatypes"`
	// Definitions are the definition-only clusters (no id, datatypes only)
	// other clusters reference by "Owner.Name": WebRtcTransportDefinitions
	// at the pin.
	Definitions []*cluster `json:"definitions"`
}

type cluster struct {
	ID         uint32      `json:"id"`
	Name       string      `json:"name"`
	Revision   uint16      `json:"revision"`
	Base       string      `json:"base"`
	Attributes []*element  `json:"attributes"`
	Commands   []*element  `json:"commands"`
	Events     []*element  `json:"events"`
	Features   []*feature  `json:"features"`
	Datatypes  []*datatype `json:"datatypes"`
}

type element struct {
	ID        uint32     `json:"id"`
	Name      string     `json:"name"`
	Effective *effective `json:"effective"`
}

// effective is an element's resolved description. Attributes use the value
// half; commands and events the direction, response, priority and fields.
type effective struct {
	value
	Direction string   `json:"direction"`
	Response  string   `json:"response"`
	Priority  string   `json:"priority"`
	Fields    []*value `json:"fields"`
}

// value describes an attribute, a field, a list entry, an enum value or a
// bitmap member.
type value struct {
	ID          *uint32         `json:"id"`
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Type        string          `json:"type"`
	Metatype    string          `json:"metatype"`
	Primitive   string          `json:"primitive"`
	Scope       string          `json:"scope"`
	Entry       *value          `json:"entry"`
	Fields      []*value        `json:"fields"`
	Conformance *conformance    `json:"conformance"`
	Access      *access         `json:"access"`
	Quality     map[string]bool `json:"quality"`
	Constraint  *constraint     `json:"constraint"`
	Default     json.RawMessage `json:"default"`
	// IsSecret is matter.js's mark for a key, passcode or credential
	// whose value its logs redact, read through the base chain at
	// extraction (effectiveIsSecret) and present only when true.
	IsSecret bool `json:"isSecret"`
}

type conformance struct {
	Text string          `json:"text"`
	AST  json.RawMessage `json:"ast"`
}

type access struct {
	RW        string `json:"rw"`
	ReadPriv  string `json:"readPriv"`
	WritePriv string `json:"writePriv"`
	Fabric    string `json:"fabric"`
	Timed     bool   `json:"timed"`
}

type constraint struct {
	Text  string          `json:"text"`
	Desc  bool            `json:"desc"`
	Value json.RawMessage `json:"value"`
	Min   json.RawMessage `json:"min"`
	Max   json.RawMessage `json:"max"`
	In    json.RawMessage `json:"in"`
	CpMax *int            `json:"cpMax"`
	Entry *constraint     `json:"entry"`
	Parts []*constraint   `json:"parts"`
}

type feature struct {
	Name      string `json:"name"`
	Bit       *uint  `json:"bit"`
	Effective struct {
		Title       string       `json:"title"`
		Conformance *conformance `json:"conformance"`
	} `json:"effective"`
}

type datatype struct {
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Metatype    string       `json:"metatype"`
	Primitive   string       `json:"primitive"`
	Conformance *conformance `json:"conformance"`
	Constraint  *constraint  `json:"constraint"`
	Fields      []*value     `json:"fields"`
	// IsSecret is the mark on a datatype itself. spec.Struct, spec.Enum
	// and spec.Bitmap carry no mark, so [parseSnapshot] refuses a marked
	// datatype rather than drop it (none at the pin: every mark sits on a
	// value).
	IsSecret bool `json:"isSecret"`
}

func loadSnapshot(path string) (*snapshot, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // the module's own snapshot, or a test's
	if err != nil {
		return nil, err
	}
	return parseSnapshot(raw)
}

func parseSnapshot(raw []byte) (*snapshot, error) {
	var s snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	if len(s.Clusters) == 0 {
		return nil, errors.New("snapshot has no clusters")
	}
	for _, c := range s.Clusters {
		// An attribute's inline struct fields decode into the effective
		// object's own "fields" (which commands and events use); move them
		// to the value they describe.
		for _, a := range c.Attributes {
			if a.Effective != nil && len(a.Effective.value.Fields) == 0 {
				a.Effective.value.Fields = a.Effective.Fields
			}
		}
		for _, f := range c.Features {
			// Without a bit the feature would decode as bit 0
			// (R2-P6-par-2714); one above 31 the generated Feature
			// constant's uint32 cannot hold, which its compile refuses.
			if f.Bit == nil {
				return nil, fmt.Errorf("%s.%s: feature without a bit", c.Name, f.Name)
			}
		}
		for _, list := range [][]*element{c.Attributes, c.Commands, c.Events} {
			for _, e := range list {
				if e.Effective == nil {
					return nil, fmt.Errorf("%s.%s has no resolved layer: regenerate the snapshot with script/extract-from-matter-js.ts", c.Name, e.Name)
				}
			}
		}
	}
	if err := refuseSecretDatatypes(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// refuseSecretDatatypes fails on a datatype that carries isSecret itself:
// the generated definitions carry the mark on a field or an attribute only,
// and a dropped mark would let the secret reach the module's logs.
func refuseSecretDatatypes(s *snapshot) error {
	check := func(owner string, dts []*datatype) error {
		for _, d := range dts {
			if d.IsSecret {
				return fmt.Errorf("%s.%s: a datatype marked isSecret is not carried by the generated definitions", owner, d.Name)
			}
		}
		return nil
	}
	if err := check("globalDatatypes", s.GlobalDatatypes); err != nil {
		return err
	}
	for _, c := range append(append([]*cluster{}, s.Clusters...), s.Definitions...) {
		if err := check(c.Name, c.Datatypes); err != nil {
			return err
		}
	}
	return nil
}

func (s *snapshot) cluster(name string) *cluster {
	for _, c := range s.Clusters {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// definition finds a definition-only cluster by name, nil when there is
// none.
func (s *snapshot) definition(name string) *cluster {
	for _, c := range s.Definitions {
		if c.Name == name {
			return c
		}
	}
	return nil
}
