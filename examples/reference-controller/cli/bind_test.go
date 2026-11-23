// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"testing"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// TestReadListRefusesAReadWithoutTheList holds R2-C2-err-2652: a read that
// carries neither the list nor a status for its path — an empty report, an
// entry the assembler dropped, a value that is not a list — is an error,
// so appendToList never writes its one entry as the whole list (for the
// ACL: this fabric's every entry, the controller's Administer included,
// replaced by one Operate entry).
func TestReadListRefusesAReadWithoutTheList(t *testing.T) {
	path := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x001F, Attribute: 0x0000}
	if _, err := readList(&interaction.Report{}, path); err == nil {
		t.Fatal("an empty report read as an empty list")
	}
	scalar := &interaction.Report{Attributes: []interaction.AttributeData{{Path: path, Value: spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1}}}}}
	if _, err := readList(scalar, path); err == nil {
		t.Fatal("a value that is not a list read as an empty list")
	}
	entry := spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}}
	list := &interaction.Report{Attributes: []interaction.AttributeData{{Path: path, Value: spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}, Children: []spec.Node{entry}}}}}
	if got, err := readList(list, path); err != nil || len(got.Children) != 1 {
		t.Fatalf("a list of one entry: %d entries, %v", len(got.Children), err)
	}
	empty := &interaction.Report{Attributes: []interaction.AttributeData{{Path: path, Value: spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}}}}
	if got, err := readList(empty, path); err != nil || len(got.Children) != 0 {
		t.Fatalf("an empty list: %d entries, %v", len(got.Children), err)
	}
}
