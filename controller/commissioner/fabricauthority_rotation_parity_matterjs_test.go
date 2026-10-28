// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// The rotation and fabricAdded cases of matter.js
// packages/protocol/test/fabric/FabricAuthorityTest.ts at 862ad75f
// (describe "keypair rotation" :129, "fabric creation" :423), on
// FabricAuthority over a ca.MemoryStore.

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

func faNOCSubject(t *testing.T, f *Fabric) mattercert.DistinguishedName {
	t.Helper()
	c, err := mattercert.Decode(f.Identity.NOC)
	if err != nil {
		t.Fatal(err)
	}
	return c.Subject
}

func TestFabricAuthorityRotation(t *testing.T) {
	t.Parallel()
	cfgOf := func(st ca.FabricStore, cats []uint32) FabricConfig {
		return FabricConfig{Label: "test-fabric", VendorID: 0xfff1, RootNodeID: 100, FabricID: 1, CATs: cats, Store: st}
	}
	t.Run("rotates NOC keypair when getting default fabric with rotateNoc=true", func(t *testing.T) {
		t.Parallel()
		// :130-163.
		a, st := NewFabricAuthority(faCA(t), nil), &ca.MemoryStore{}
		initial, err := a.CreateFabric(cfgOf(st, []uint32{0x00010001}))
		if err != nil {
			t.Fatal(err)
		}
		rotated, err := a.DefaultFabric(cfgOf(st, []uint32{0x00010001}), true)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(faPublicKey(t, rotated), faPublicKey(t, initial)) || bytes.Equal(rotated.Identity.NOC, initial.Identity.NOC) {
			t.Fatal("the key pair was not rotated")
		}
		s := faNOCSubject(t, rotated)
		if s.MatterFabricID != 1 || s.MatterNodeID != 100 || !slices.Equal(s.CASEAuthTags, []uint32{0x00010001}) {
			t.Fatalf("NOC subject %+v", s)
		}
		if rotated.FabricIndex != initial.FabricIndex || rotated.RootNodeID != initial.RootNodeID || !bytes.Equal(rotated.IPK, initial.IPK) {
			t.Fatal("the fabric's identity moved")
		}
	})
	t.Run("rotation occurs exactly once per runtime per fabric", func(t *testing.T) {
		t.Parallel()
		// :207-234 and "rotatedFabricIndices Set correctly prevents
		// duplicate rotations" (:236-259).
		a, st := NewFabricAuthority(faCA(t), nil), &ca.MemoryStore{}
		initial, err := a.CreateFabric(cfgOf(st, nil))
		if err != nil {
			t.Fatal(err)
		}
		first, err := a.DefaultFabric(cfgOf(st, nil), true)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(faPublicKey(t, first), faPublicKey(t, initial)) {
			t.Fatal("the first call did not rotate")
		}
		for range 2 {
			again, err := a.DefaultFabric(cfgOf(st, nil), true)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(faPublicKey(t, again), faPublicKey(t, first)) || again.FabricIndex != first.FabricIndex {
				t.Fatal("a second call rotated again")
			}
		}
		// Another authority is another runtime.
		next, err := NewFabricAuthority(a.CA(), nil).DefaultFabric(cfgOf(st, nil), true)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(faPublicKey(t, next), faPublicKey(t, first)) {
			t.Fatal("a new runtime did not rotate")
		}
	})
	t.Run("new fabric is properly persisted after rotation", func(t *testing.T) {
		t.Parallel()
		// :261-288: a later load sees the rotated key.
		c, st := faCA(t), &ca.MemoryStore{}
		a := NewFabricAuthority(c, nil)
		if _, err := a.CreateFabric(cfgOf(st, nil)); err != nil {
			t.Fatal(err)
		}
		rotated, err := a.DefaultFabric(cfgOf(st, nil), true)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := NewFabric(c, cfgOf(st, nil))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(faPublicKey(t, loaded), faPublicKey(t, rotated)) || !bytes.Equal(loaded.Identity.NOC, rotated.Identity.NOC) {
			t.Fatal("the rotated fabric was not persisted")
		}
	})
	t.Run("does not rotate when rotateNoc=false", func(t *testing.T) {
		t.Parallel()
		// :290-307.
		a, st := NewFabricAuthority(faCA(t), nil), &ca.MemoryStore{}
		initial, err := a.CreateFabric(cfgOf(st, nil))
		if err != nil {
			t.Fatal(err)
		}
		again, err := a.DefaultFabric(cfgOf(st, nil), false)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(faPublicKey(t, again), faPublicKey(t, initial)) {
			t.Fatal("rotated with rotateNOC false")
		}
	})
	for _, row := range []struct {
		name string
		cats []uint32
	}{
		// :309-380: the fabric id, node id and CATs of the old NOC, with
		// and without CATs.
		{"preserves fabricId, nodeId and caseAuthenticatedTags from operational certificate during rotation", []uint32{0x00010001, 0x00020001}},
		{"handles fabric without caseAuthenticatedTags during rotation", nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			a, st := NewFabricAuthority(faCA(t), nil), &ca.MemoryStore{}
			if _, err := a.CreateFabric(cfgOf(st, row.cats)); err != nil {
				t.Fatal(err)
			}
			rotated, err := a.DefaultFabric(cfgOf(st, nil), true)
			if err != nil {
				t.Fatal(err)
			}
			s := faNOCSubject(t, rotated)
			if s.MatterFabricID != 1 || s.MatterNodeID != 100 || !slices.Equal(s.CASEAuthTags, row.cats) {
				t.Fatalf("NOC subject %+v, want CATs %v", s, row.cats)
			}
		})
	}
	t.Run("rotation works with multiple fabrics", func(t *testing.T) {
		t.Parallel()
		// :382-421 by meaning: one authority, two stores (a Go controller
		// keeps one fabric per store), each rotated once.
		a := NewFabricAuthority(faCA(t), nil)
		st1, st2 := &ca.MemoryStore{}, &ca.MemoryStore{}
		cfg2 := cfgOf(st2, nil)
		cfg2.FabricID, cfg2.FabricIndex = 2, 2
		f1, err := a.CreateFabric(cfgOf(st1, nil))
		if err != nil {
			t.Fatal(err)
		}
		f2, err := a.CreateFabric(cfg2)
		if err != nil {
			t.Fatal(err)
		}
		r1, err := a.DefaultFabric(cfgOf(st1, nil), true)
		if err != nil {
			t.Fatal(err)
		}
		cfg2.FabricID = 0
		r2, err := a.DefaultFabric(cfg2, true)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(faPublicKey(t, r1), faPublicKey(t, f1)) || bytes.Equal(faPublicKey(t, r2), faPublicKey(t, f2)) {
			t.Fatal("a fabric was not rotated")
		}
		if r1.FabricIndex != 1 || r2.FabricIndex != 2 || r2.FabricID != 2 {
			t.Fatalf("fabrics %d/%d, %d", r1.FabricIndex, r2.FabricIndex, r2.FabricID)
		}
	})
	t.Run("refuses to rotate a NOC whose node id changed", func(t *testing.T) {
		t.Parallel()
		// FabricAuthority.ts:190-193.
		c := faCA(t)
		a := NewFabricAuthority(c, nil)
		f, err := a.CreateFabric(FabricConfig{FabricID: 1, RootNodeID: 100})
		if err != nil {
			t.Fatal(err)
		}
		f.RootNodeID = 101
		if _, err := a.rotate(f, FabricConfig{}); !errors.Is(err, ErrNodeIDChanged) {
			t.Fatalf("err %v, want ErrNodeIDChanged", err)
		}
	})
	t.Run("sets the configured label on the default fabric", func(t *testing.T) {
		t.Parallel()
		// :97-99.
		a, st := NewFabricAuthority(faCA(t), nil), &ca.MemoryStore{}
		if _, err := a.CreateFabric(cfgOf(st, nil)); err != nil {
			t.Fatal(err)
		}
		cfg := cfgOf(st, nil)
		cfg.Label = "renamed"
		f, err := a.DefaultFabric(cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		if f.CurrentLabel() != "renamed" {
			t.Fatalf("label %q", f.CurrentLabel())
		}
	})
}

// TestFabricAuthorityFabricAdded: "emits fabricAdded event when creating
// fabric" (:437-451): CreateFabric reports the fabric after persisting it;
// a loaded default fabric is not added.
func TestFabricAuthorityFabricAdded(t *testing.T) {
	t.Parallel()
	a, st := NewFabricAuthority(faCA(t), nil), &ca.MemoryStore{}
	var added []*Fabric
	stop := a.OnFabricAdded(func(f *Fabric) {
		if _, ok, _ := st.LoadFabric(); !ok {
			t.Error("fabricAdded before the fabric was persisted")
		}
		added = append(added, f)
	})
	f, err := a.CreateFabric(FabricConfig{FabricID: 1, RootNodeID: 100, Store: st})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DefaultFabric(FabricConfig{Store: st}, true); err != nil {
		t.Fatal(err)
	}
	stop()
	if _, err := a.CreateFabric(FabricConfig{FabricID: 2, RootNodeID: 100}); err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != f {
		t.Fatalf("added %v, want the created fabric once", added)
	}
}
