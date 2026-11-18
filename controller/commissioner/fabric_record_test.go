// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
)

// TestStoredFabricIdentityIsChecked: a stored fabric record whose NOC is
// not for its key, names another node or fabric, or does not verify under
// the CA is ErrFabricMismatch, not an identity that fails at the first
// CASE or AddNOC (review II, R2-P6-store-1853 (c)).
func TestStoredFabricIdentityIsChecked(t *testing.T) {
	t.Parallel()
	otherPub := func(t *testing.T) []byte {
		t.Helper()
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		b, _ := k.PublicKey.Bytes()
		return b
	}
	for _, c := range []struct {
		name   string
		damage func(t *testing.T, c *ca.CA, own []byte, r *ca.FabricRecord)
	}{
		{"a NOC for another key", func(t *testing.T, c *ca.CA, _ []byte, r *ca.FabricRecord) {
			t.Helper()
			r.NOC = mustNOC(t, c, otherPub(t), r.FabricID, r.RootNodeID)
		}},
		{"a NOC for another node", func(t *testing.T, c *ca.CA, own []byte, r *ca.FabricRecord) {
			t.Helper()
			r.NOC = mustNOC(t, c, own, r.FabricID, 0x77)
		}},
		{"a NOC for another fabric", func(t *testing.T, c *ca.CA, own []byte, r *ca.FabricRecord) {
			t.Helper()
			r.NOC = mustNOC(t, c, own, 0x99, r.RootNodeID)
		}},
		{"a NOC of another CA, the record's root key cleared", func(t *testing.T, _ *ca.CA, own []byte, r *ca.FabricRecord) {
			t.Helper()
			other, err := ca.New(ca.Config{})
			if err != nil {
				t.Fatal(err)
			}
			r.NOC, r.RootPublicKey = mustNOC(t, other, own, r.FabricID, r.RootNodeID), nil
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			st := &ca.MemoryStore{}
			authority, err := ca.New(ca.Config{Store: st})
			if err != nil {
				t.Fatal(err)
			}
			cfg := FabricConfig{FabricID: 1, RootNodeID: 1, Store: st}
			if _, err := NewFabric(authority, cfg); err != nil {
				t.Fatal(err)
			}
			rec, _, _ := st.LoadFabric()
			c.damage(t, authority, rec.KeyPair.PublicKey, &rec)
			if err := st.SaveFabric(rec); err != nil {
				t.Fatal(err)
			}
			if _, err := NewFabric(authority, cfg); !errors.Is(err, ErrFabricMismatch) {
				t.Fatalf("NewFabric = %v, want ErrFabricMismatch", err)
			}
		})
	}
}

func mustNOC(t *testing.T, c *ca.CA, pub []byte, fabricID, nodeID uint64) []byte {
	t.Helper()
	noc, err := c.GenerateNOC(pub, fabricID, nodeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return noc
}

// TestFabricLabelIsBoundInBytes: the controller's fabric label is at most
// 32 bytes of UTF-8 — the bound a device enforces — at creation, at load
// and in SetLabel (review II, R2-P6-par-1854, decision 155). Before, only
// SetLabel checked it, in code points.
func TestFabricLabelIsBoundInBytes(t *testing.T) {
	t.Parallel()
	tooLong := []string{strings.Repeat("x", 33), strings.Repeat("ä", 17), strings.Repeat("😀", 9)}
	fits := []string{strings.Repeat("x", 32), strings.Repeat("ä", 16), strings.Repeat("😀", 8), ""}
	for _, l := range tooLong {
		if _, err := NewFabric(faCA(t), FabricConfig{FabricID: 1, RootNodeID: 1, Label: l}); !errors.Is(err, ErrLabel) {
			t.Errorf("NewFabric with %d bytes: %v, want ErrLabel", len(l), err)
		}
		if _, err := NewFabricAuthority(faCA(t), nil).CreateFabric(FabricConfig{FabricID: 1, RootNodeID: 1, Label: l}); !errors.Is(err, ErrLabel) {
			t.Errorf("CreateFabric with %d bytes: %v, want ErrLabel", len(l), err)
		}
		f, err := NewFabric(faCA(t), FabricConfig{FabricID: 1, RootNodeID: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetLabel(l); !errors.Is(err, ErrLabel) {
			t.Errorf("SetLabel with %d bytes: %v, want ErrLabel", len(l), err)
		}
		st := &ca.MemoryStore{}
		c, err := ca.New(ca.Config{Store: st})
		if err != nil {
			t.Fatal(err)
		}
		cfg := FabricConfig{FabricID: 1, RootNodeID: 1, Store: st}
		if _, err := NewFabric(c, cfg); err != nil {
			t.Fatal(err)
		}
		rec, _, _ := st.LoadFabric()
		rec.Label = l
		_ = st.SaveFabric(rec)
		if _, err := NewFabric(c, cfg); !errors.Is(err, ErrLabel) {
			t.Errorf("a stored label of %d bytes: %v, want ErrLabel", len(l), err)
		}
	}
	for _, l := range fits {
		f, err := NewFabric(faCA(t), FabricConfig{FabricID: 1, RootNodeID: 1, Label: l})
		if err != nil {
			t.Fatalf("NewFabric with %d bytes: %v", len(l), err)
		}
		if err := f.SetLabel(l + ""); err != nil {
			t.Fatalf("SetLabel with %d bytes: %v", len(l), err)
		}
	}
}

// TestZeroFabricLabelIsSafe: SetLabel and CurrentLabel on a Fabric the
// package did not build — the zero value — do not race, and CurrentLabel
// does not wait for SetLabel's store (review II, R2-P6-conc-1858 (a)(b)).
// Run under -race.
func TestZeroFabricLabelIsSafe(t *testing.T) {
	t.Parallel()
	f := &Fabric{}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); _ = f.SetLabel(strings.Repeat("x", i)) }()
		go func() { defer wg.Done(); _ = f.CurrentLabel() }()
	}
	wg.Wait()

	block := make(chan struct{})
	entered := make(chan struct{})
	f2 := &Fabric{CA: faCA(t), store: &blockingFabricStore{entered: entered, release: block}}
	done := make(chan error, 1)
	go func() { done <- f2.SetLabel("new") }()
	<-entered
	if got := f2.CurrentLabel(); got != "" {
		t.Fatalf("CurrentLabel %q while the save runs, want the old label", got)
	}
	close(block)
	if err := <-done; err != nil || f2.CurrentLabel() != "new" {
		t.Fatalf("SetLabel = %v, label %q", err, f2.CurrentLabel())
	}
}

type blockingFabricStore struct {
	entered, release chan struct{}
}

func (s *blockingFabricStore) LoadFabric() (ca.FabricRecord, bool, error) {
	return ca.FabricRecord{}, false, nil
}

func (s *blockingFabricStore) SaveFabric(ca.FabricRecord) error {
	close(s.entered)
	<-s.release
	return nil
}

// TestCreateFabricNeverReplacesAStoredFabric: CreateFabric on a store that
// holds a fabric fails and leaves the record — its key, NOC and IPK — as
// it was; replacing the fabric would orphan every node commissioned under
// the old IPK (review II, R2-P6-api-1859, owner decision 191).
func TestCreateFabricNeverReplacesAStoredFabric(t *testing.T) {
	t.Parallel()
	st := &ca.MemoryStore{}
	c, err := ca.New(ca.Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	a := NewFabricAuthority(c, nil)
	cfg := FabricConfig{FabricID: 1, RootNodeID: 1, Store: st}
	if _, err := a.CreateFabric(cfg); err != nil {
		t.Fatal(err)
	}
	before, _, _ := st.LoadFabric()
	if _, err := a.CreateFabric(cfg); err == nil {
		t.Fatal("the second CreateFabric succeeded")
	}
	if after, _, _ := st.LoadFabric(); !reflect.DeepEqual(after, before) {
		t.Fatal("the stored fabric changed")
	}
}

// TestRotationDoesNotHoldTheObserverLock: while DefaultFabric's rotation
// waits for the store, OnFabricAdded and its unsubscribe return — the
// authority's lock is not held across the CA's signer or the store
// (review II, R2-P6-conc-1858 (c), ADR 0026 P6).
func TestRotationDoesNotHoldTheObserverLock(t *testing.T) {
	t.Parallel()
	mem := &ca.MemoryStore{}
	c, err := ca.New(ca.Config{Store: mem})
	if err != nil {
		t.Fatal(err)
	}
	cfg := FabricConfig{FabricID: 1, RootNodeID: 1, Store: mem}
	if _, err := NewFabric(c, cfg); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	cfg.Store = &onceBlockingStore{FabricStore: mem, entered: entered, release: release}
	a := NewFabricAuthority(c, nil)
	done := make(chan error, 1)
	go func() { _, err := a.DefaultFabric(cfg, true); done <- err }()
	<-entered
	observed := make(chan struct{})
	go func() { a.OnFabricAdded(func(*Fabric) {})(); close(observed) }()
	receive(t, "OnFabricAdded to return while the rotation's save waits", observed)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// onceBlockingStore blocks its first SaveFabric until release.
type onceBlockingStore struct {
	ca.FabricStore
	entered, release chan struct{}
	once             sync.Once
}

func (s *onceBlockingStore) SaveFabric(r ca.FabricRecord) error {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.FabricStore.SaveFabric(r)
}

// TestStoredOperationalKeyIsNeverDropped: a stored fabric record whose
// key pair or NOC is missing, and an OperationalSigner whose key is not
// the stored NOC's, are refused with ca.ErrConfig and the record stays as
// it was — before, a new identity or NOC was minted and saved over it,
// the stored private key gone (review II, R2-P6-store-1851 class, owner
// decision 191, Fable read F3).
func TestStoredOperationalKeyIsNeverDropped(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*ca.CA, *ca.MemoryStore, FabricConfig) {
		t.Helper()
		st := &ca.MemoryStore{}
		c, err := ca.New(ca.Config{Store: st})
		if err != nil {
			t.Fatal(err)
		}
		cfg := FabricConfig{FabricID: 1, RootNodeID: 1, Store: st}
		if _, err := NewFabric(c, cfg); err != nil {
			t.Fatal(err)
		}
		return c, st, cfg
	}
	for _, d := range []struct {
		name   string
		damage func(*ca.FabricRecord)
		signer bool
	}{
		{"a key pair without its NOC", func(r *ca.FabricRecord) { r.NOC = nil }, false},
		{"a NOC without its key pair", func(r *ca.FabricRecord) { r.KeyPair = nil }, false},
		{"a signer whose key is not the stored NOC's", func(*ca.FabricRecord) {}, true},
	} {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			c, st, cfg := setup(t)
			rec, _, _ := st.LoadFabric()
			d.damage(&rec)
			if err := st.SaveFabric(rec); err != nil {
				t.Fatal(err)
			}
			if d.signer {
				cfg.OperationalSigner = newOpSigner(t)
			}
			if _, err := NewFabric(c, cfg); !errors.Is(err, ca.ErrConfig) {
				t.Fatalf("NewFabric = %v, want ca.ErrConfig", err)
			}
			if after, _, _ := st.LoadFabric(); !reflect.DeepEqual(after, rec) {
				t.Fatal("the stored record changed")
			}
		})
	}
}
