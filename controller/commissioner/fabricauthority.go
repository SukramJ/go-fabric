// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// FabricAuthority is the authority over the fabrics one CA controls —
// matter.js FabricAuthority (packages/protocol/src/fabric/FabricAuthority.ts:49-207
// at 862ad75f): it creates a fabric ([FabricAuthority.CreateFabric]),
// returns the default one ([FabricAuthority.DefaultFabric]), which rotates
// a stored fabric's operational key once per authority — once per runtime
// — and reports every fabric it creates ([FabricAuthority.OnFabricAdded]).
// It is safe for concurrent use.
//
// matter.js's FabricManager holds several fabrics under one authority; a Go
// controller keeps one in its ca.FabricStore, so the authority works on
// the fabric of FabricConfig.Store. FabricBuilder (the builder the
// rotation's new fabric comes from, FabricBuilder.updateOperationalCert)
// is [FabricAuthority.DefaultFabric]'s own step here.
type FabricAuthority struct {
	ca  *ca.CA
	log *slog.Logger

	mu      sync.Mutex
	rotated map[uint8]bool // #rotatedFabricIndices (:57)
	added   map[int]func(*Fabric)
	nextObs int
}

// NewFabricAuthority returns the authority of c; a nil logger discards.
func NewFabricAuthority(c *ca.CA, log *slog.Logger) *FabricAuthority {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &FabricAuthority{ca: c, log: log, rotated: map[uint8]bool{}, added: map[int]func(*Fabric){}}
}

// CA is the authority's certificate authority (:72-75).
func (a *FabricAuthority) CA() *ca.CA { return a.ca }

// OnFabricAdded calls fn with every fabric [FabricAuthority.CreateFabric]
// creates, after it was persisted — the fabricAdded observable
// (:108-110, :170-171) — until the returned function is called.
func (a *FabricAuthority) OnFabricAdded(fn func(*Fabric)) (unsubscribe func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := a.nextObs
	a.nextObs++
	a.added[id] = fn
	return func() {
		a.mu.Lock()
		delete(a.added, id)
		a.mu.Unlock()
	}
}

// CreateFabric mints a new fabric under the authority's CA and saves it in
// cfg.Store, whatever the store held — createFabric (:119-173): our node
// id, the IPK, the admin vendor id (reserved ones refused), the label, the
// NOC; then fabricAdded.
func (a *FabricAuthority) CreateFabric(cfg FabricConfig) (*Fabric, error) {
	store := cfg.Store
	cfg.Store = nil
	f, err := newFabric(a.ca, cfg)
	if err != nil {
		return nil, err
	}
	f.store = store
	if store != nil {
		if err := store.SaveFabric(fabricRecord(f, a.ca)); err != nil {
			return nil, fmt.Errorf("commissioner: save fabric: %w", err)
		}
	}
	a.log.Debug("matter.controller.fabric.created", slog.Int("fabric_index", int(f.FabricIndex)))
	a.mu.Lock()
	fns := make([]func(*Fabric), 0, len(a.added))
	for _, fn := range a.added {
		fns = append(fns, fn)
	}
	a.mu.Unlock()
	for _, fn := range fns {
		fn(f)
	}
	return f, nil
}

// DefaultFabric is the authority's fabric — defaultFabric (:84-104): the
// fabric cfg.Store holds, its label set to cfg.Label, and, with rotateNOC,
// its operational key rotated the first time this authority returns it
// (a new key pair and a new NOC from the CA under the stored NOC's fabric
// id, node id and CASE Authenticated Tags, saved over the old record,
// :181-205); without a stored fabric a new one is created. matter.js
// rotates by default (rotateNoc = true, :86-88): each start of a
// controller that keeps its own fabric derives a fresh key for the same
// logical fabric, which the peers trust under the same CA
// (packages/matter.js/src/MatterController.ts:402-415).
//
// A fabric whose key a FabricConfig.OperationalSigner keeps is not
// rotated: the key is the signer's, not the authority's to replace.
func (a *FabricAuthority) DefaultFabric(cfg FabricConfig, rotateNOC bool) (*Fabric, error) {
	if cfg.Store == nil {
		return a.CreateFabric(cfg)
	}
	_, found, err := cfg.Store.LoadFabric()
	if err != nil {
		return nil, fmt.Errorf("commissioner: load fabric: %w", err)
	}
	if !found {
		return a.CreateFabric(cfg)
	}
	label := cfg.Label
	f, err := NewFabric(a.ca, cfg)
	if err != nil {
		return nil, err
	}
	if rotateNOC && cfg.OperationalSigner == nil {
		if f, err = a.rotate(f, cfg); err != nil {
			return nil, err
		}
	}
	if label != "" && f.CurrentLabel() != label {
		if err := f.SetLabel(label); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// ErrNodeIDChanged is a stored fabric whose NOC names another node than
// the fabric's root node id: its key cannot be rotated (:190-193).
var ErrNodeIDChanged = errors.New("commissioner: cannot rotate the NOC because the node id changed")

// rotate is #rotateFabricNocKey (:181-205): once per fabric index and
// authority.
func (a *FabricAuthority) rotate(f *Fabric, cfg FabricConfig) (*Fabric, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rotated[f.FabricIndex] {
		return f, nil
	}
	noc, err := mattercert.Decode(f.Identity.NOC)
	if err != nil {
		return nil, fmt.Errorf("commissioner: rotate NOC: %w", err)
	}
	subject := noc.Subject
	if subject.MatterNodeID != f.RootNodeID {
		return nil, fmt.Errorf("%w: fabric %d", ErrNodeIDChanged, f.FabricIndex)
	}
	random := cfg.Rand
	if random == nil {
		random = rand.Reader
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), random)
	if err != nil {
		return nil, fmt.Errorf("commissioner: key: %w", err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	newNOC, err := a.ca.GenerateNOC(pub, subject.MatterFabricID, subject.MatterNodeID, slices.Clone(subject.CASEAuthTags))
	if err != nil {
		return nil, err
	}
	bcfg := FabricConfig{
		FabricID: subject.MatterFabricID, RootNodeID: f.RootNodeID, VendorID: f.RootVendorID,
		Label: f.CurrentLabel(), FabricIndex: f.FabricIndex,
	}
	rotated, err := buildFabric(a.ca, bcfg, f.IPK, key, newNOC)
	if err != nil {
		return nil, err
	}
	rotated.store = f.store
	if f.store != nil {
		if err := f.store.SaveFabric(fabricRecord(rotated, a.ca)); err != nil {
			return nil, fmt.Errorf("commissioner: save fabric: %w", err)
		}
	}
	a.log.Info("matter.controller.fabric.noc_rotated", slog.Int("fabric_index", int(f.FabricIndex)))
	a.rotated[f.FabricIndex] = true
	return rotated, nil
}
