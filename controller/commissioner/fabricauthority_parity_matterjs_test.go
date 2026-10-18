// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/protocol/test/fabric/FabricAuthorityTest.ts at
// 862ad75f: the controller's own fabric — admin vendor checks, persistence, no rotation.
//
// matter.js's FabricAuthority.createFabric / defaultFabric over a
// FabricManager is NewFabric over a ca.FabricStore here: the controller
// owns one fabric, NewFabric mints it (createFabric) or loads it
// (defaultFabric), and it never rotates the NOC key (rotateNoc=false).
// The rotation cases, the fabricAdded event and FabricBuilder have no Go
// counterpart and stay in the register's missing list.

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
)

func faCA(t *testing.T) *ca.CA {
	t.Helper()
	c, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func faPublicKey(t *testing.T, f *Fabric) []byte {
	t.Helper()
	b, err := f.Identity.PrivateKey.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// faGatedStore holds SaveFabric until release closes (the GatedDriver, :73-85).
type faGatedStore struct {
	ca.MemoryStore
	writing chan struct{}
	release chan struct{}
	saves   atomic.Int32
}

func (g *faGatedStore) SaveFabric(r ca.FabricRecord) error {
	if g.saves.Add(1) == 1 {
		close(g.writing)
	}
	<-g.release
	return g.MemoryStore.SaveFabric(r)
}

// faFailingStore refuses every SaveFabric (the FailingDriver, :108-119).
type faFailingStore struct {
	ca.MemoryStore
	err error
}

func (f *faFailingStore) SaveFabric(ca.FabricRecord) error { return f.err }

// TestFabricAuthorityCreateFabric: describe "createFabric" (:57).
func TestFabricAuthorityCreateFabric(t *testing.T) {
	t.Parallel()
	t.Run("refuses a vendor ID above 0xFFF4 as admin vendor ID", func(t *testing.T) {
		t.Parallel()
		_, err := NewFabric(faCA(t), FabricConfig{FabricID: 1, RootNodeID: 100, Label: "test-fabric", VendorID: 0xfff5})
		if err == nil || !regexp.MustCompile(`Admin vendor ID 0xfff5 is reserved`).MatchString(err.Error()) {
			t.Fatalf("NewFabric = %v, want the reserved-vendor refusal (:67)", err)
		}
	})
	t.Run("completes once the fabric list is persisted and reports the fabric only as added", func(t *testing.T) {
		t.Parallel()
		st := &faGatedStore{writing: make(chan struct{}), release: make(chan struct{})}
		type result struct {
			f   *Fabric
			err error
		}
		done := make(chan result, 1)
		c := faCA(t)
		go func() {
			f, err := NewFabric(c, FabricConfig{FabricID: 1, RootNodeID: 100, Label: "test-fabric", Store: st})
			done <- result{f, err}
		}()
		<-st.writing
		select {
		case r := <-done:
			t.Fatalf("NewFabric completed before the record was persisted: %v", r.err)
		default:
		}
		close(st.release)
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		// Added only: one save, nothing written again (no "replaced").
		if n := st.saves.Load(); n != 1 {
			t.Fatalf("%d saves, want 1", n)
		}
		if rec, ok, _ := st.LoadFabric(); !ok || rec.FabricIndex != r.f.FabricIndex {
			t.Fatalf("stored %+v (%v)", rec, ok)
		}
	})
	t.Run("rejects when the fabric list cannot be persisted", func(t *testing.T) {
		t.Parallel()
		diskFull := errors.New("Disk full") //nolint:staticcheck // ST1005: the matter.js StorageError text, :115
		_, err := NewFabric(faCA(t), FabricConfig{FabricID: 1, RootNodeID: 100, Label: "test-fabric", Store: &faFailingStore{err: diskFull}})
		if !errors.Is(err, diskFull) || !strings.Contains(err.Error(), "Disk full") {
			t.Fatalf("NewFabric = %v, want the store's Disk full", err)
		}
	})
}

// TestFabricAuthorityDefaultAdminVendorID: no it of its own — the
// adminVendorId left undefined of every createFabric without one
// (FabricAuthority.ts:140-142) is the zero VendorID.
func TestFabricAuthorityDefaultAdminVendorID(t *testing.T) {
	t.Parallel()
	f, err := NewFabric(faCA(t), FabricConfig{FabricID: 1, RootNodeID: 100})
	if err != nil {
		t.Fatal(err)
	}
	if f.RootVendorID != 0xFFF1 {
		t.Fatalf("root vendor id %#x, want DEFAULT_ADMIN_VENDOR_ID 0xfff1", f.RootVendorID)
	}
}

// TestFabricAuthorityKeypairRotation: describe "keypair rotation" (:129) —
// the one case Go has, as NewFabric never rotates.
func TestFabricAuthorityKeypairRotation(t *testing.T) {
	t.Parallel()
	t.Run("does not rotate when rotateNoc=false", func(t *testing.T) {
		t.Parallel()
		c, st := faCA(t), &ca.MemoryStore{}
		cfg := FabricConfig{Label: "test-fabric", VendorID: 0xfff1, RootNodeID: 100, FabricID: 1, Store: st} // :292-297
		initial, err := NewFabric(c, cfg)
		if err != nil {
			t.Fatal(err)
		}
		again, err := NewFabric(c, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(faPublicKey(t, again), faPublicKey(t, initial)) || !bytes.Equal(again.Identity.NOC, initial.Identity.NOC) {
			t.Fatal("the operational key pair was rotated")
		}
	})
}

// TestFabricAuthorityFabricCreation: describe "fabric creation" (:423).
func TestFabricAuthorityFabricCreation(t *testing.T) {
	t.Parallel()
	t.Run("creates fabric with specified configuration", func(t *testing.T) {
		t.Parallel()
		f, err := NewFabric(faCA(t), FabricConfig{Label: "test-fabric", VendorID: 0xfff1, RootNodeID: 100, FabricID: 1}) // :425-430
		if err != nil {
			t.Fatal(err)
		}
		if f.Label != "test-fabric" || f.RootNodeID != 100 {
			t.Fatalf("label %q root node id %d", f.Label, f.RootNodeID)
		}
	})
}
