// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// TestDeviceTypeComponentInstances drives instance matching and the
// disallowed component: a Composer whose two mandatory Light instances need
// OnOff with LT, and one whose Light is X (DeviceTypeConformance.ts
// checkInstances, matchInstances, checkComposition).
func TestDeviceTypeComponentInstances(t *testing.T) {
	t.Parallel()
	lighting := func() schema.DeviceTypeRequirement {
		return clusterReq("OnOff", 6, confM(), featureReq("LT", 0, confM()))
	}
	l := syntheticModel(
		nil,
		&schema.DeviceTypeDefinition{ID: rootID, Name: "RootNode", Classification: "node"},
		&schema.DeviceTypeDefinition{ID: lightID, Name: "Light", Classification: "simple"},
		&schema.DeviceTypeDefinition{
			ID: composerID, Name: "Composer", Classification: "simple",
			Requirements: []schema.DeviceTypeRequirement{
				componentReq("Light", lightID, confM(), schema.CountRange{}, 0, lighting()),
				componentReq("Light", lightID, confM(), schema.CountRange{}, 0, lighting()),
			},
		},
		&schema.DeviceTypeDefinition{
			ID: composer2ID, Name: "Forbidding", Classification: "simple",
			Requirements: []schema.DeviceTypeRequirement{componentReq("Light", lightID, confX(), schema.CountRange{}, 0)},
		},
	)
	f := newFakeTree()
	root := f.add("root", nil, rootID)

	// One candidate satisfies an instance; the other offers no LT.
	c1 := f.add("c1", root, composerID)
	f.add("good", c1, lightID).server(6, "LT")
	f.add("dim", c1, lightID).server(6)
	got := f.check(l, c1)
	wantKinds(t, got, "instanceCount device:Light#2")
	if d := detailsOf(got); len(d) == 1 && d[0] != "No endpoint fills component device type Light: every endpoint that satisfies it fills another instance" {
		t.Errorf("detail = %q", d[0])
	}

	// No candidate offers LT.
	c2 := f.add("c2", root, composerID)
	f.add("dim1", c2, lightID).server(6)
	f.add("dim2", c2, lightID).server(6)
	if d := detailsOf(f.check(l, c2)); !slices.Equal(d, []string{
		"No endpoint fills component device type Light: no candidate offers OnOff.LT",
		"No endpoint fills component device type Light: no candidate offers OnOff.LT",
	}) {
		t.Errorf("c2 = %v", d)
	}

	// The candidates fail differently.
	c3 := f.add("c3", root, composerID)
	f.add("dim3", c3, lightID).server(6)
	f.add("dark", c3, lightID)
	if d := detailsOf(f.check(l, c3)); len(d) != 2 || d[0] != "No endpoint fills component device type Light: no candidate satisfies it" {
		t.Errorf("c3 = %v", d)
	}

	forbidding := f.add("forbidding", root, composer2ID)
	f.add("intruder", forbidding, lightID)
	got = f.check(l, forbidding)
	wantKinds(t, got, "disallowed device:Light")
	if d := detailsOf(got); len(d) == 1 && d[0] != "Disallowed component device type Light is present on 1 endpoint(s)" {
		t.Errorf("detail = %q", d[0])
	}
	wantKinds(t, f.check(l, f.add("empty", root, composer2ID)))
}

func TestDeviceTypeRanges(t *testing.T) {
	t.Parallel()
	for want, r := range map[string]schema.CountRange{
		"exactly 1": {Set: true, Min: 1, HasMin: true, Max: 1, HasMax: true},
		"1 to 254":  {Set: true, Min: 1, HasMin: true, Max: 254, HasMax: true},
		"min 2":     minCount(2),
		"max 1":     maxCount(1),
	} {
		if got := describeRange(r); got != want {
			t.Errorf("describeRange(%+v) = %q, want %q", r, got, want)
		}
	}
	comp := &dtComponent{deviceType: &schema.DeviceTypeDefinition{Name: "PowerSource"}}
	numbered := &schema.DeviceTypeRequirement{Instance: 2}
	if instancePath(comp, numbered) != "device:PowerSource#2" || describeInstance(comp, numbered) != "instance 2 of component device type PowerSource" {
		t.Error("a numbered instance renders wrongly")
	}
}

// TestDeviceTypeOutsideNodeScope: an endpoint below no node endpoint takes
// the conditions of its whole tree, and one below a nested node endpoint
// belongs to that node's scope only.
func TestDeviceTypeOutsideNodeScope(t *testing.T) {
	t.Parallel()
	l := deviceTypeLookups()
	f := newFakeTree()
	top := f.add("top", nil, dtAggregator)
	lamp := light(f, top, "lamp")
	wantKinds(t, f.check(l, lamp))
	if newDTPass(f, l).treeRootOf(lamp.id) != top.id {
		t.Error("tree root wrong")
	}

	g, root := standardTree()
	inner := g.add("inner", root, dtRootNode)
	deep := light(g, inner, "deep")
	p := newDTPass(g, l)
	if p.isInScope(deep.id, root.id) || !p.isInScope(deep.id, inner.id) {
		t.Error("node scope boundaries wrong")
	}
	if slices.Contains(p.nodeScopeOf(root.id), deep.id) || slices.Contains(p.of(root.id).compositionScope(), inner.id) {
		t.Error("the outer node scope or composition enters the inner node")
	}
	if got := ValidateDeviceTypes(&Topology{Endpoints: []*Endpoint{nil}}); got != nil {
		t.Errorf("a topology of nil endpoints has violations %v", got)
	}
}
