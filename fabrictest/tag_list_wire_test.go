// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// TestTagListOverTheWire: a bridged plug built with device.Options.Tags
// serves its Descriptor TagList as a list of semtag structs (matter.js
// packages/model/src/standard/elements/descriptor.element.ts:48-51 at
// 862ad75f: list, entry semtag), and a
// controller's read stores the entries in its image — not the TLV null the
// bridge's value writer sent for the plain []Semtag slice before the
// general Encodable-slice rule (G-N4, issue #388). The FixedLabel
// LabelList on the same endpoint takes the same path.
func TestTagListOverTheWire(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	label := spec.Nullable[string]{Value: "Left"}
	fixed := []fixedlabeldef.LabelStruct{{Label: "room", Value: "hall"}}
	plug, err := device.NewOnOffPlugInUnit(device.Options{
		StableKey: device.StringKey("plug"), Name: "Plug",
		Tags: []device.SemanticTag{
			{MfgCode: spec.Nullable[uint16]{Null: true}, NamespaceId: 7, Tag: 1, Label: &label},
			{MfgCode: spec.Nullable[uint16]{Value: 0xFFF1}, NamespaceId: 8, Tag: 3},
		},
	}, fixedlabeldef.Declare{Initial: fixedlabeldef.Patch{LabelList: &fixed}})
	if err != nil {
		t.Fatal(err)
	}
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{plug}}})
	ep, _ := plug.EndpointNumber()
	n, err := p.Node()
	if err != nil {
		t.Fatal(err)
	}
	// A wildcard read: the image places an endpoint once the root's
	// PartsList names it.
	ctx := testContext(t)
	err = site.Run(ctx, func(ctx context.Context) error {
		_, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{{}}}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		cluster, attr uint32
		want          string
	}{
		{descdef.ClusterID, descdef.AttrTagList, `[{"label":"Left","mfgCode":null,"namespaceId":7,"tag":1},{"mfgCode":65521,"namespaceId":8,"tag":3}]`},
		{fixedlabeldef.ClusterID, fixedlabeldef.AttrLabelList, `[{"label":"room","value":"hall"}]`},
	} {
		v, ok := n.Endpoint(ep).Cluster(c.cluster).Value(c.attr)
		if !ok {
			t.Fatalf("0x%04X/%d: not in the image", c.cluster, c.attr)
		}
		got, err := client.DecodeJSON(client.AttributeField(c.cluster, c.attr), v, client.NameBased)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("0x%04X/%d renders %s, want %s", c.cluster, c.attr, got, c.want)
		}
	}
	decoded, err := n.Endpoint(ep).Cluster(descdef.ClusterID).Decoded(descdef.AttrTagList)
	if err != nil {
		t.Fatal(err)
	}
	if tags, ok := decoded.(spec.List[descdef.Semtag]); !ok || len(tags) != 2 || tags[1].Tag != 3 {
		t.Fatalf("Decoded TagList = %#v, want the two semtags", decoded)
	}
}
