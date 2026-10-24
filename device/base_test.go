// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"
	"strings"
	"testing"

	boolstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
)

// TestBaseOptionalServers: the Base device type's plain optional server
// clusters are FixedLabel and UserLabel — matter.js
// packages/model/src/standard/elements/base.element.ts:46-47 at 862ad75f —
// read from the snapshot; Binding (:45, `Simple & Client`) is not among
// them.
func TestBaseOptionalServers(t *testing.T) {
	t.Parallel()
	ids := make([]uint32, 0, len(baseOptionalServers()))
	for _, r := range baseOptionalServers() {
		ids = append(ids, r.ID)
		if r.Conformance != "O" || r.Definition == nil {
			t.Errorf("%s: conformance %q, definition %v", r.Name, r.Conformance, r.Definition != nil)
		}
	}
	if !slices.Equal(ids, []uint32{fixedlabeldef.ClusterID, userlabeldef.ClusterID}) {
		t.Errorf("Base optional servers %#04x, want FixedLabel and UserLabel", ids)
	}
}

// TestBaseOptionalOnEveryType: every device type that constructs with its
// defaults accepts FixedLabel and UserLabel by declaration and mounts the
// module's servers after its own clusters; the declared labels are served.
func TestBaseOptionalOnEveryType(t *testing.T) {
	t.Parallel()
	fixed := []fixedlabeldef.LabelStruct{{Label: "room", Value: "hall"}}
	user := []userlabeldef.LabelStruct{{Label: "owner", Value: "me"}}
	for _, ty := range Types() {
		if _, refused := refusedDefaults[ty.Name]; refused {
			continue
		}
		t.Run(ty.Name, func(t *testing.T) {
			t.Parallel()
			d, err := New(ty, testOptions(ty.Name),
				fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &fixed}},
				userlabeldef.Declare{Initial: userlabeldef.Patch{LabelList: &user}})
			if err != nil {
				t.Fatal(err)
			}
			if d.Type() != ty {
				t.Error("the device's type is not the type it was built from")
			}
			if _, listed := ty.Server(fixedlabeldef.ClusterID); listed {
				return // a type that lists the cluster itself
			}
			ids := serverIDs(d.Servers())
			if n := len(ids); n < 2 || ids[n-2] != fixedlabeldef.ClusterID || ids[n-1] != userlabeldef.ClusterID {
				t.Fatalf("servers %#04x: FixedLabel and UserLabel are not mounted after the type's", ids)
			}
			if got := fixedlabeldef.Wrap(d.Server(fixedlabeldef.ClusterID)).Get().LabelList; !slices.Equal(got, fixed) {
				t.Errorf("FixedLabel %+v", got)
			}
			if got := userlabeldef.Wrap(d.Server(userlabeldef.ClusterID)).Get().LabelList; !slices.Equal(got, user) {
				t.Errorf("UserLabel %+v", got)
			}
		})
	}
}

// TestBaseOptionalConformsAndRefuses: an OnOffLight with both labels has
// no device-type violation; an over-long label is refused at
// construction; a cluster neither the type nor Base allows stays refused.
func TestBaseOptionalConformsAndRefuses(t *testing.T) {
	t.Parallel()
	fixed := []fixedlabeldef.LabelStruct{{Label: "room", Value: "hall"}}
	d, err := NewOnOffLight(testOptions("l"),
		fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &fixed}}, userlabeldef.Declare{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violationsOf(assemble(t, d.Device)) {
		t.Errorf("device type violation: %s", v)
	}
	long := []fixedlabeldef.LabelStruct{{Label: strings.Repeat("x", 40), Value: "v"}}
	if _, err := NewOnOffLight(testOptions("l"), fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &long}}); err == nil {
		t.Error("an over-long label was accepted")
	}
	_, err = NewOnOffLight(testOptions("l"), boolstatedef.Declare{})
	if err == nil || !strings.Contains(err.Error(), "no server cluster requirement for cluster 0x0045") {
		t.Errorf("BooleanState on an OnOffLight: %v, want the refusal", err)
	}
}
