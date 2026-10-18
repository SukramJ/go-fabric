// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/tlv"
)

// shaped builds a cluster over values and analyses it, as the structure
// does for an endpoint that is not installed.
func shaped(id uint32, values map[uint32]spec.Node) *Cluster {
	var mu sync.RWMutex
	p, _ := newTestPersister(&MemoryStore{}, 0, newManualClock())
	c := newCluster(&mu, id, newCache(p, 1, id, values, ptr(uint32(4))))
	c.readInput()
	if c.buildable() {
		c.current = c.analyse()
		c.injected = c.current
	}
	return c
}

// TestClusterNames: known, named and unknown clusters (D3;
// PeerBehavior.ts:399-401, :426-427).
func TestClusterNames(t *testing.T) {
	t.Parallel()
	for id, want := range map[uint32]string{0x0006: "OnOff", 0x0035: "ThreadNetworkDiagnostics", 0xFFF1FC01: "Cluster$fff1fc01"} {
		c := shaped(id, nil)
		if c.Name() != want || c.ID() != id || (c.Definition() != nil) != (id == 0x0006) {
			t.Errorf("0x%X: %q definition %v", id, c.Name(), c.Definition() != nil)
		}
	}
	if unknownName("attr", 0x7001) != "attr$7001" {
		t.Fatal(unknownName("attr", 0x7001))
	}
}

// TestClusterShape: the inputs of ClientStructure.ts:762-808 and the
// analysis of PeerBehavior.ts:422-467 and :478-507.
func TestClusterShape(t *testing.T) {
	t.Parallel()
	c := shaped(onoff.ClusterID, map[uint32]spec.Node{
		onoff.AttrOnOff:               boolNode(true),
		0x7001:                        uintNode(1),
		spec.AttrClusterRevision:      uintNode(6),
		spec.AttrFeatureMap:           uintNode(uint64(onoff.FeatureLighting)),
		spec.AttrAttributeList:        listNode(0, 0x7001, 0xFFFD, 0xFFFC, 0xFFFB),
		spec.AttrAcceptedCommandList:  listNode(2, 1, 0, 0x80),
		spec.AttrGeneratedCommandList: listNode(),
	})
	if !c.Ready() || c.Revision() != 6 || c.FeatureMap() != uint32(onoff.FeatureLighting) || !reflect.DeepEqual(c.Features(), []string{"LT"}) {
		t.Fatalf("ready %v revision %d features %#x %v", c.Ready(), c.Revision(), c.FeatureMap(), c.Features())
	}
	if !c.AttributeListReported() || !reflect.DeepEqual(c.AttributeList(), []uint32{0, 0x7001, 0xFFFB, 0xFFFC, 0xFFFD}) {
		t.Fatalf("attribute list %v reported %v", c.AttributeList(), c.AttributeListReported())
	}
	if !reflect.DeepEqual(c.AcceptedCommands(), []uint32{0, 1, 2, 0x80}) || len(c.GeneratedCommands()) != 0 {
		t.Fatalf("commands %v %v", c.AcceptedCommands(), c.GeneratedCommands())
	}
	attrs, cmds := c.Extra()
	if !reflect.DeepEqual(attrs, []uint32{0x7001}) || !reflect.DeepEqual(cmds, []uint32{0x80}) {
		t.Fatalf("extra %v %v", attrs, cmds)
	}
	if !c.Supports(onoff.AttrOnOff) || c.Supports(onoff.AttrOnTime) || !c.Supports(spec.AttrEventList) || !c.SupportsCommand(onoff.CmdToggle) || c.SupportsCommand(onoff.CmdOnWithTimedOff) {
		t.Fatal("support")
	}
	if v, ok := c.Version(); !ok || v != 4 {
		t.Fatalf("version %d %v", v, ok)
	}

	// An empty AttributeList falls back to the ids received; a revision
	// below 1 is none (:779-795, PeerBehavior.ts:429-434).
	e := shaped(onoff.ClusterID, map[uint32]spec.Node{onoff.AttrOnOff: boolNode(false), spec.AttrAttributeList: listNode(), spec.AttrClusterRevision: uintNode(0)})
	if e.AttributeListReported() || !reflect.DeepEqual(e.AttributeList(), []uint32{0, 0xFFFB, 0xFFFD}) || e.Revision() != 0 {
		t.Fatalf("fallback %v revision %d", e.AttributeList(), e.Revision())
	}

	// Nothing useful: no shape (:810-817).
	n := shaped(onoff.ClusterID, nil)
	attrs, cmds = n.Extra()
	if n.Ready() || n.Revision() != 0 || n.FeatureMap() != 0 || n.Features() != nil || n.AttributeList() != nil || n.AttributeListReported() ||
		n.AcceptedCommands() != nil || n.GeneratedCommands() != nil || attrs != nil || cmds != nil || n.Supports(0) || n.SupportsCommand(0) {
		t.Fatal("an empty cluster has a shape")
	}

	// A named cluster: the schema names its features, every non-global
	// listed id is extra (no definition to subtract).
	a := shaped(0x001F, map[uint32]spec.Node{spec.AttrFeatureMap: uintNode(0b101), spec.AttrAttributeList: listNode(0, 0xFFFC)})
	attrs, cmds = a.Extra()
	if !reflect.DeepEqual(a.Features(), []string{"EXTS", "AUX"}) || !reflect.DeepEqual(attrs, []uint32{0}) || cmds != nil {
		t.Fatalf("named cluster features %v extra %v %v", a.Features(), attrs, cmds)
	}
	// An unknown cluster: no features, every listed id extra.
	u := shaped(0xFFF1FC01, map[uint32]spec.Node{spec.AttrFeatureMap: uintNode(1), spec.AttrAttributeList: listNode(0, 0xFFFC), spec.AttrAcceptedCommandList: listNode(3)})
	attrs, cmds = u.Extra()
	if u.Features() != nil || u.FeatureMap() != 1 || !reflect.DeepEqual(attrs, []uint32{0}) || !reflect.DeepEqual(cmds, []uint32{3}) || !u.Supports(0) {
		t.Fatalf("unknown cluster features %v extra %v %v", u.Features(), attrs, cmds)
	}
}

// TestClusterDecoded: by definition, by global type, untyped, null, absent.
func TestClusterDecoded(t *testing.T) {
	t.Parallel()
	null := spec.Node{El: tlv.Element{Type: tlv.TypeNull, IsNull: true}}
	c := shaped(onoff.ClusterID, map[uint32]spec.Node{
		onoff.AttrOnOff:          boolNode(true),
		onoff.AttrStartUpOnOff:   null,
		0x7001:                   uintNode(1),
		spec.AttrAttributeList:   listNode(0, 0x7001),
		spec.AttrClusterRevision: uintNode(6),
	})
	if v, err := c.Decoded(onoff.AttrOnOff); err != nil || v != true {
		t.Fatalf("OnOff %v %v", v, err)
	}
	if v, err := c.Decoded(onoff.AttrStartUpOnOff); err != nil || v != nil {
		t.Fatalf("null %v %v", v, err)
	}
	if v, err := c.Decoded(spec.AttrAttributeList); err != nil || !reflect.DeepEqual(v, []any{uint64(0), uint64(0x7001)}) {
		t.Fatalf("AttributeList %v %v", v, err)
	}
	if v, err := c.Decoded(0x7001); !errors.Is(err, ErrUntyped) || v.(spec.Node).El.Uint != 1 {
		t.Fatalf("extra attribute %v %v", v, err)
	}
	if _, err := c.Decoded(onoff.AttrOnTime); !errors.Is(err, ErrNoValue) {
		t.Fatalf("absent %v", err)
	}
	u := shaped(0x0035, map[uint32]spec.Node{0: uintNode(15), spec.AttrClusterRevision: uintNode(3)})
	if _, err := u.Decoded(0); !errors.Is(err, ErrUntyped) {
		t.Fatalf("named cluster %v", err)
	}
	if v, err := u.Decoded(spec.AttrClusterRevision); err != nil || v != uint64(3) {
		t.Fatalf("global of a named cluster %v %v", v, err)
	}
	if _, ok := globalType(0); ok {
		t.Fatal("attribute 0 is no global")
	}
}

// TestEveryGeneratedClusterIsKnown: known.go links every generated
// definition, so spec.Lookup knows each cluster the generator emits (D3).
func TestEveryGeneratedClusterIsKnown(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../cluster/spec/*/definition_gen.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no generated definitions (%v)", err)
	}
	re := regexp.MustCompile(`ClusterID\s+uint32\s*=\s*0x([0-9A-Fa-f]+)`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m := re.FindSubmatch(b)
		if m == nil {
			t.Fatalf("%s states no ClusterID", f)
		}
		id, _ := strconv.ParseUint(string(m[1]), 16, 32)
		if spec.Lookup(uint32(id)) == nil {
			t.Errorf("%s (0x%04X) is not linked: add it to known.go", filepath.Dir(f), id)
		}
	}
}
