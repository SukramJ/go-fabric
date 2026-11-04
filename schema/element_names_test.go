// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

import (
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
)

// TestElementNamesMatchSnapshot holds the generated name tables against the
// model they are generated from: every attribute, request command,
// response command and event of every cluster of parity/schema.json is
// named, under its effective command direction, and the tables hold
// nothing else. A stale element_names_gen.go (the generator skipped after
// a snapshot refresh) fails here.
func TestElementNamesMatchSnapshot(t *testing.T) {
	t.Parallel()
	type elem struct {
		ID        uint32 `json:"id"`
		Name      string `json:"name"`
		Direction string `json:"direction"`
		Effective *struct {
			Direction string `json:"direction"`
			Title     string `json:"title"`
		} `json:"effective"`
		Bit *int `json:"bit"`
	}
	var snap struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Name       string `json:"name"`
			Attributes []elem `json:"attributes"`
			Commands   []elem `json:"commands"`
			Events     []elem `json:"events"`
			Features   []elem `json:"features"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	count := func(m map[uint32]map[uint32]string) int {
		n := 0
		for _, c := range m {
			n += len(c)
		}
		return n
	}
	var attrs, reqs, resps, events int
	for _, c := range snap.Clusters {
		for _, a := range c.Attributes {
			attrs++
			if got, ok := AttributeName(c.ID, a.ID); !ok || got != a.Name {
				t.Errorf("%s attribute 0x%04X: %q %v, want %q", c.Name, a.ID, got, ok, a.Name)
			}
		}
		for _, m := range c.Commands {
			dir := m.Direction
			if m.Effective != nil && m.Effective.Direction != "" {
				dir = m.Effective.Direction
			}
			lookup := CommandName
			switch dir {
			case "request":
				reqs++
			case "response":
				resps++
				lookup = ResponseCommandName
			default:
				t.Errorf("%s command %s has no direction", c.Name, m.Name)
				continue
			}
			if got, ok := lookup(c.ID, m.ID); !ok || got != m.Name {
				t.Errorf("%s %s command 0x%02X: %q %v, want %q", c.Name, dir, m.ID, got, ok, m.Name)
			}
		}
		for _, e := range c.Events {
			events++
			if got, ok := EventName(c.ID, e.ID); !ok || got != e.Name {
				t.Errorf("%s event 0x%02X: %q %v, want %q", c.Name, e.ID, got, ok, e.Name)
			}
		}
		for _, f := range c.Features {
			if f.Bit == nil || f.Effective == nil {
				continue
			}
			found := false
			for _, cf := range ClusterFeatures(c.ID) {
				if int(cf.Bit) == *f.Bit {
					found = true
					if cf.Name != f.Name || cf.Title != f.Effective.Title {
						t.Errorf("%s feature bit %d: %+v, want %s %q", c.Name, *f.Bit, cf, f.Name, f.Effective.Title)
					}
				}
			}
			if !found {
				t.Errorf("%s feature %s missing", c.Name, f.Name)
			}
		}
	}
	if count(attributeNames) != attrs || count(commandNames) != reqs || count(responseCommandNames) != resps || count(eventNames) != events {
		t.Errorf("tables hold %d/%d/%d/%d names, the snapshot %d/%d/%d/%d: regenerate with `go generate ./schema/...`",
			count(attributeNames), count(commandNames), count(responseCommandNames), count(eventNames), attrs, reqs, resps, events)
	}
}

// TestElementNameLookups spot-checks the lookups: a global attribute on any
// cluster, a request and a response sharing an id, an event, and the
// unknown cases.
func TestElementNameLookups(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		got  func() (string, bool)
		want string
	}{
		{func() (string, bool) { return AttributeName(0x0006, 0x0000) }, "OnOff"},
		{func() (string, bool) { return AttributeName(0x0006, 0xFFFB) }, "AttributeList"},
		{func() (string, bool) { return AttributeName(0xFFF1FC00, 0xFFF9) }, "AcceptedCommandList"},
		{func() (string, bool) { return AttributeName(0x001D, 0x0004) }, "TagList"},
		{func() (string, bool) { return CommandName(0x0006, 0x02) }, "Toggle"},
		// DoorLock GetWeekDaySchedule and its response share id 0x0C.
		{func() (string, bool) { return CommandName(0x0101, 0x0C) }, "GetWeekDaySchedule"},
		{func() (string, bool) { return ResponseCommandName(0x0101, 0x0C) }, "GetWeekDayScheduleResponse"},
		// A derived cluster's override inherits its base's direction.
		{func() (string, bool) { return ResponseCommandName(0x0061, 0x04) }, "OperationalCommandResponse"},
		{func() (string, bool) { return EventName(0x0028, 0x00) }, "StartUp"},
	} {
		if got, ok := c.got(); !ok || got != c.want {
			t.Errorf("got %q %v, want %q", got, ok, c.want)
		}
	}
	if _, ok := AttributeName(0x0006, 0x7777); ok {
		t.Error("an unknown attribute is named")
	}
	if _, ok := CommandName(0x0006, 0x77); ok {
		t.Error("an unknown command is named")
	}
	if _, ok := ResponseCommandName(0x0006, 0x00); ok {
		t.Error("OnOff's Off is named a response")
	}
	if _, ok := EventName(0xFFFF, 0); ok {
		t.Error("an event of an unknown cluster is named")
	}
	if fs := ClusterFeatures(0x0006); len(fs) == 0 || fs[0].Title != "Lighting" {
		t.Errorf("OnOff features = %v, want LT titled Lighting", fs)
	}
}
