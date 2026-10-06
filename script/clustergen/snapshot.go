// SPDX-License-Identifier: MIT
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
	Entry *constraint     `json:"entry"`
	Parts []*constraint   `json:"parts"`
}

type feature struct {
	Name      string `json:"name"`
	Bit       uint   `json:"bit"`
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
		for _, list := range [][]*element{c.Attributes, c.Commands, c.Events} {
			for _, e := range list {
				if e.Effective == nil {
					return nil, fmt.Errorf("%s.%s has no resolved layer: regenerate the snapshot with script/extract-from-matter-js.ts", c.Name, e.Name)
				}
			}
		}
	}
	return &s, nil
}

func (s *snapshot) cluster(name string) *cluster {
	for _, c := range s.Clusters {
		if c.Name == name {
			return c
		}
	}
	return nil
}
