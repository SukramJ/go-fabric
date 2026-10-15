// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	pscfgdef "github.com/SukramJ/go-fabric/cluster/spec/powersourceconfiguration"
)

// TestPowerSourceConfigurationSources pins the list rules: the host's
// order, at most six entries (power-source-configuration-server.cpp
// :41, :72-73), and the read-time provider replacing the constructed list.
func TestPowerSourceConfigurationSources(t *testing.T) {
	t.Parallel()
	read := func(p *PowerSourceConfiguration) []uint16 {
		t.Helper()
		v, ok := p.MatterRead(pscfgdef.AttrSources)
		l, isList := v.([]uint16)
		if !ok || !isList {
			t.Fatalf("Sources = %#v (%v)", v, ok)
		}
		return l
	}
	p, err := NewPowerSourceConfiguration([]uint16{9, 2, 7, 4, 5, 6, 8})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(p); !slices.Equal(got, []uint16{9, 2, 7, 4, 5, 6}) {
		t.Errorf("Sources = %v, want the first six in the given order", got)
	}
	empty, err := NewPowerSourceConfiguration(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(empty); got == nil || len(got) != 0 {
		t.Errorf("empty Sources = %#v, want an empty list", got)
	}
	ids := []uint16{12}
	p.SetSourcesProvider(func() []uint16 { return ids })
	if got := read(p); !slices.Equal(got, []uint16{12}) {
		t.Errorf("provided Sources = %v", got)
	}
	ids = []uint16{1, 2, 3, 4, 5, 6, 7, 8}
	if got := read(p); len(got) != PowerSourceConfigurationMaxSources {
		t.Errorf("provided Sources = %v, want six", got)
	}
	p.SetSourcesProvider(nil)
	if got := read(p); len(got) != 6 || got[0] != 9 {
		t.Errorf("Sources after the provider is removed = %v", got)
	}
	if _, ok := (pscfgSource{p}).MatterAttribute(pscfgdef.AttrSources + 1); ok {
		t.Error("the source answered an attribute other than Sources")
	}
	if err := p.MatterWrite(context.Background(), pscfgdef.AttrSources, []uint16{1}); err == nil {
		t.Error("Sources write accepted")
	}
}
