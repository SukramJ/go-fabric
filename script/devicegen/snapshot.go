// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// The parts of parity/schema.json the device generator reads: every device
// type's resolved layer ("effective": composition, base, conditions and the
// requirements with their nested element requirements), the Base device
// type, and the clusters' features, elements and classification. It is
// the same snapshot script/clustergen and schema/ are generated from.

type snapshot struct {
	Matter struct {
		Revision     string `json:"revision"`
		SourceCommit string `json:"sourceCommit"`
	} `json:"matter"`
	DeviceTypes     []*deviceType `json:"deviceTypes"`
	BaseDeviceTypes []*deviceType `json:"baseDeviceTypes"`
	Clusters        []*cluster    `json:"clusters"`
}

type deviceType struct {
	ID             *uint32 `json:"id"`
	Name           string  `json:"name"`
	Classification string  `json:"classification"`
	Revision       uint16  `json:"revision"`
	Effective      struct {
		Composition  string         `json:"composition"`
		Base         string         `json:"base"`
		Conditions   []string       `json:"conditions"`
		Requirements []*requirement `json:"requirements"`
	} `json:"effective"`
}

type requirement struct {
	Element      string          `json:"element"`
	Name         string          `json:"name"`
	ID           *uint32         `json:"id"`
	Conformance  *conformance    `json:"conformance"`
	Constraint   *constraint     `json:"constraint"`
	Default      json.RawMessage `json:"default"`
	Referent     *referent       `json:"referent"`
	Requirements []*requirement  `json:"requirements"`
}

type referent struct {
	ID   *uint32 `json:"id"`
	Bit  *uint   `json:"bit"`
	Name string  `json:"name"`
}

type conformance struct {
	Text string `json:"text"`
	AST  *ast   `json:"ast"`
}

// ast is a matter.js Conformance.Ast node: a flag ("M", "O", …), a name, a
// revision, an otherwise list, an optionalIf, an operator, a choice.
type ast struct {
	Type  string          `json:"type"`
	Param json.RawMessage `json:"param"`
}

type constraint struct {
	Text  string          `json:"text"`
	Value json.RawMessage `json:"value"`
	Min   json.RawMessage `json:"min"`
	Max   json.RawMessage `json:"max"`
}

type cluster struct {
	ID             uint32     `json:"id"`
	Name           string     `json:"name"`
	Classification string     `json:"classification"`
	Bindable       *bool      `json:"bindable"`
	Features       []*feature `json:"features"`
	Attributes     []*element `json:"attributes"`
	Commands       []*element `json:"commands"`
	Events         []*element `json:"events"`
}

type feature struct {
	Name      string `json:"name"`
	Bit       *uint  `json:"bit"`
	Effective struct {
		Title       string       `json:"title"`
		Conformance *conformance `json:"conformance"`
	} `json:"effective"`
}

type element struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Effective struct {
		Conformance *conformance `json:"conformance"`
	} `json:"effective"`
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
	if len(s.DeviceTypes) == 0 {
		return nil, errors.New("snapshot has no device types")
	}
	if err := s.check(); err != nil {
		return nil, err
	}
	return &s, nil
}

// check refuses what the generator would otherwise read silently wrong: a
// feature without a bit (it would decode as bit 0) or with one the uint32
// FeatureMap cannot carry (R2-P6-par-2714), and a conformance with a node
// the evaluators do not know or a parameter that does not decode
// (checkAST, R2-P6-par-2707).
func (s *snapshot) check() error {
	for _, c := range s.Clusters {
		for _, f := range c.Features {
			if f.Bit == nil {
				return fmt.Errorf("%s.%s: feature without a bit", c.Name, f.Name)
			}
			if *f.Bit > 31 {
				return fmt.Errorf("%s.%s: feature bit %d outside the 32-bit FeatureMap", c.Name, f.Name, *f.Bit)
			}
			if err := checkConformance(f.Effective.Conformance); err != nil {
				return fmt.Errorf("%s.%s: %w", c.Name, f.Name, err)
			}
		}
		for _, list := range [][]*element{c.Attributes, c.Commands, c.Events} {
			for _, e := range list {
				if err := checkConformance(e.Effective.Conformance); err != nil {
					return fmt.Errorf("%s.%s: %w", c.Name, e.Name, err)
				}
			}
		}
	}
	for _, dts := range [][]*deviceType{s.DeviceTypes, s.BaseDeviceTypes} {
		for _, dt := range dts {
			if err := checkRequirements(dt.Effective.Requirements); err != nil {
				return fmt.Errorf("device type %s: %w", dt.Name, err)
			}
		}
	}
	return nil
}

func checkRequirements(reqs []*requirement) error {
	for _, r := range reqs {
		if err := checkConformance(r.Conformance); err != nil {
			return fmt.Errorf("%s %s: %w", r.Element, r.Name, err)
		}
		if err := checkRequirements(r.Requirements); err != nil {
			return fmt.Errorf("%s %s: %w", r.Element, r.Name, err)
		}
	}
	return nil
}

func checkConformance(c *conformance) error {
	if c == nil || c.AST == nil {
		return nil
	}
	if err := checkAST(c.AST); err != nil {
		return fmt.Errorf("conformance %q: %w", c.Text, err)
	}
	return nil
}

// deviceType returns the device type named name, or nil.
func (s *snapshot) deviceType(name string) *deviceType {
	for _, d := range s.DeviceTypes {
		if d.Name == name {
			return d
		}
	}
	return nil
}

// clusterByID returns the cluster with id, or nil.
func (s *snapshot) clusterByID(id uint32) *cluster {
	for _, c := range s.Clusters {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// feature returns the cluster's feature named name, or nil.
func (c *cluster) feature(name string) *feature {
	for _, f := range c.Features {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// element returns the cluster's attribute, command or event a requirement
// names — matter.js RequirementResolver.elementOf
// (packages/model/src/logic/RequirementResolver.ts:223-229): the member of
// that kind with the requirement's name.
func (c *cluster) element(kind, name string) *element {
	var list []*element
	switch kind {
	case "attribute":
		list = c.Attributes
	case "command":
		list = c.Commands
	case "event":
		list = c.Events
	default:
		return nil
	}
	for _, e := range list {
		if e.Name == name {
			return e
		}
	}
	return nil
}
