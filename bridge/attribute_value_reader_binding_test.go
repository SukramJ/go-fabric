// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"reflect"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestAttributeValueReader_BindingAppend pins the list-append write of
// Binding (0x001E/0x0000): the appended entry decodes into the element type
// of the server's own fabric-filtered list, which the dispatcher appends it
// to (endpoint appendListValue), and the server takes the merged list — as
// matter.js applies a ListIndex-null write to the current value. A
// controller that writes a Binding list as a replace and appends (the Go
// controller does, TC-BIND-2.1 in the Go-TH leg) was answered
// CONSTRAINT_ERROR for every entry while the item decoded to the generated
// list type.
func TestAttributeValueReader_BindingAppend(t *testing.T) {
	t.Parallel()
	item := im.ConcreteAttributePath{HasCluster: true, HasAttribute: true, Cluster: bindingdef.ClusterID, Attribute: bindingdef.AttrBinding, ListAppend: true}
	node, ep, cl := uint64(0x1122334455667788), uint16(2), uint32(6)
	enc := tlv.NewEncoder()
	bindingdef.TargetStruct{Node: &node, Endpoint: &ep, Cluster: &cl}.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(b)
	av, err := attributeValueReader(item, advanceToContent(t, dec), dec)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := av.Value.([]mattercore.TargetStruct)
	if !ok || len(got) != 1 || got[0].Node != node || got[0].Endpoint != ep || got[0].Cluster != cl {
		t.Fatalf("append item decoded %#v", av.Value)
	}
	srv := mattercore.NewBinding()
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	current, ok := srv.MatterReadFiltered(ctx, bindingdef.AttrBinding)
	if !ok || reflect.TypeOf(current) != reflect.TypeOf(av.Value) {
		t.Fatalf("the server's list is %T, the appended entry %T: the dispatcher cannot append it", current, av.Value)
	}
	if err := srv.MatterWrite(im.WithListAppendWrite(ctx), bindingdef.AttrBinding, got); err != nil {
		t.Fatalf("the server refused the appended entry: %v", err)
	}
	if e := srv.Entries(); len(e) != 1 || e[0].FabricIndex != 1 || *e[0].Node != node {
		t.Fatalf("entries after the append %+v", e)
	}
}
