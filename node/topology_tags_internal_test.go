// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// TestTagsAndLabelsOfServers: a Descriptor with the TagList feature
// enabled gives BridgedDevice its tags, one without gives none; a
// FixedLabel server its entries. The endpoint publishes its servers as the
// root does, the one place a Descriptor is not built per read.
func TestTagsAndLabelsOfServers(t *testing.T) {
	tag := core.SemanticTag{NamespaceId: 0x07, Tag: 0x02}
	tagged, err := core.NewDescriptor([]core.DeviceTypeStruct{{DeviceType: 0x0100, Revision: 3}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tagged.EnableTagList(tag)
	plain, err := core.NewDescriptor([]core.DeviceTypeStruct{{DeviceType: 0x0100, Revision: 3}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := core.NewFixedLabel([]fixedlabeldef.LabelStruct{{Label: "room", Value: "hall"}})
	if err != nil {
		t.Fatal(err)
	}
	ep := &endpoint.Endpoint{ID: 0}
	ep.PublishClusterServers([]contract.ClusterServer{tagged, fixed})
	tags, labels := tagsAndLabels(ep)
	if len(tags) != 1 || tags[0].NamespaceId != tag.NamespaceId || tags[0].Tag != tag.Tag {
		t.Errorf("tags = %+v", tags)
	}
	if !slices.Equal(labels, []Label{{Label: "room", Value: "hall"}}) {
		t.Errorf("labels = %+v", labels)
	}
	ep.PublishClusterServers([]contract.ClusterServer{plain})
	if tags, labels := tagsAndLabels(ep); tags != nil || labels != nil {
		t.Errorf("without TagList and labels: %+v, %+v", tags, labels)
	}
}
