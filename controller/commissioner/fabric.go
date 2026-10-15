// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// FabricConfig configures [NewFabric].
type FabricConfig struct {
	// FabricID is the fabric id; required, not 0.
	FabricID uint64
	// RootNodeID is our node id on the fabric, the CaseAdminSubject AddNOC
	// grants Administer to; required, an operational node id.
	RootNodeID uint64
	// VendorID is the AdminVendorId of AddNOC (the fabric's root vendor).
	VendorID uint16
	// IPK is the 16-byte identity protection key (the epoch key AddNOC
	// carries); nil draws one from Rand.
	IPK []byte
	// Label is the fabric label UpdateFabricLabel sets.
	Label string
	// FabricIndex is our index of the fabric; 0 is 1.
	FabricIndex uint8
	// CATs are the CASE Authenticated Tags of our own NOC.
	CATs []uint32
	// Rand is the randomness of our key and the IPK; nil is crypto/rand.
	// The key's generation ignores it since Go 1.26 (crypto/ecdsa; see
	// ca.Config.Rand); the IPK is drawn from it.
	Rand io.Reader
	// Store keeps the fabric across restarts; nil keeps nothing (a new
	// key, NOC and IPK every NewFabric). With a Store, NewFabric loads the
	// stored record when there is one — the same fabric id, root node id,
	// IPK, index, label, key and NOC — and otherwise mints them and saves
	// the record. A stored record wins over the fields above; a non-zero
	// FabricID or RootNodeID that differs from it is [ErrFabricMismatch].
	Store ca.FabricStore
}

// ErrFabricMismatch is a stored fabric record that does not fit: its
// fabric id or root node id differs from a non-zero FabricConfig value, or
// it was made under another root than the CA's.
var ErrFabricMismatch = errors.New("commissioner: stored fabric does not match")

// Fabric is our side of a fabric the commissioner commissions into:
// [handshake.Fabric] (our operational identity, the root public key, the
// verifier) and what the flow needs of matter.js Fabric
// (packages/protocol/src/fabric/Fabric.ts:60-73 at 0ceb202d): the fabric
// id, the root node id (caseAdminSubject, ControllerCommissioningFlow.ts:1451),
// the root vendor id (adminVendorId, :1450, Fabric.ts:256), the identity
// protection key (:1449), the label (:1494, Fabric.ts:188) and the fabric
// index (Fabric.ts:522).
type Fabric struct {
	handshake.Fabric
	FabricID     uint64
	RootNodeID   uint64
	RootVendorID uint16
	IPK          []byte
	Label        string
	FabricIndex  uint8
	// CA is the authority that issues the fabric's NOCs.
	CA *ca.CA
}

// NewFabric loads the fabric cfg.Store keeps, or mints our own key and NOC
// through c and builds the fabric's operational identity — the key, the NOC, the ICAC, the operational IPK
// (sigma.DeriveOperationalIPK over the compressed fabric id), the ids —
// as bridge/bridgetest's Commissioner.identity does for its test CA.
func NewFabric(c *ca.CA, cfg FabricConfig) (*Fabric, error) {
	if c == nil {
		return nil, errors.New("commissioner: NewFabric needs a CA")
	}
	if cfg.Store != nil {
		rec, ok, err := cfg.Store.LoadFabric()
		if err != nil {
			return nil, fmt.Errorf("commissioner: load fabric: %w", err)
		}
		if ok {
			return loadFabric(c, cfg, rec)
		}
	}
	if cfg.FabricID == 0 {
		return nil, errors.New("commissioner: fabric id 0")
	}
	if !isOperationalNodeID(cfg.RootNodeID) {
		return nil, fmt.Errorf("commissioner: root node id 0x%016X is not an operational node id", cfg.RootNodeID)
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	if cfg.FabricIndex == 0 {
		cfg.FabricIndex = 1
	}
	ipk := cfg.IPK
	if ipk == nil {
		ipk = make([]byte, 16)
		if _, err := io.ReadFull(cfg.Rand, ipk); err != nil {
			return nil, fmt.Errorf("commissioner: IPK: %w", err)
		}
	}
	if len(ipk) != 16 {
		return nil, fmt.Errorf("commissioner: IPK of %d bytes, want 16", len(ipk))
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), cfg.Rand)
	if err != nil {
		return nil, fmt.Errorf("commissioner: key: %w", err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	noc, err := c.GenerateNOC(pub, cfg.FabricID, cfg.RootNodeID, cfg.CATs)
	if err != nil {
		return nil, err
	}
	f, err := buildFabric(c, cfg, ipk, key, noc)
	if err != nil {
		return nil, err
	}
	if cfg.Store != nil {
		if err := cfg.Store.SaveFabric(fabricRecord(f, c)); err != nil {
			return nil, fmt.Errorf("commissioner: save fabric: %w", err)
		}
	}
	return f, nil
}

// loadFabric rebuilds a fabric from its stored record — the load half of
// matter.js FabricManager's construction (FabricManager.ts:66-69,
// Fabric.create over each stored Fabric.Config), for the one fabric a
// controller owns.
func loadFabric(c *ca.CA, cfg FabricConfig, rec ca.FabricRecord) (*Fabric, error) {
	if cfg.FabricID != 0 && cfg.FabricID != rec.FabricID {
		return nil, fmt.Errorf("%w: fabric id 0x%016X, stored 0x%016X", ErrFabricMismatch, cfg.FabricID, rec.FabricID)
	}
	if cfg.RootNodeID != 0 && cfg.RootNodeID != rec.RootNodeID {
		return nil, fmt.Errorf("%w: root node id 0x%016X, stored 0x%016X", ErrFabricMismatch, cfg.RootNodeID, rec.RootNodeID)
	}
	if len(rec.RootPublicKey) > 0 && !bytes.Equal(rec.RootPublicKey, c.RootPublicKey()) {
		return nil, fmt.Errorf("%w: the record was made under another root", ErrFabricMismatch)
	}
	if rec.FabricID == 0 || !isOperationalNodeID(rec.RootNodeID) || len(rec.IPK) != 16 {
		return nil, fmt.Errorf("%w: stored record is incomplete", ErrFabricMismatch)
	}
	if rec.FabricIndex == 0 {
		rec.FabricIndex = 1
	}
	cfg.FabricID, cfg.RootNodeID, cfg.VendorID = rec.FabricID, rec.RootNodeID, rec.RootVendorID
	cfg.Label, cfg.FabricIndex = rec.Label, rec.FabricIndex
	if rec.KeyPair == nil || len(rec.NOC) == 0 {
		// No identity kept: mint one under the stored ids and save it.
		cfg.IPK = rec.IPK
		store := cfg.Store
		cfg.Store = nil
		f, err := NewFabric(c, cfg)
		if err != nil {
			return nil, err
		}
		if err := store.SaveFabric(fabricRecord(f, c)); err != nil {
			return nil, fmt.Errorf("commissioner: save fabric: %w", err)
		}
		return f, nil
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), rec.KeyPair.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("%w: operational key: %w", ErrFabricMismatch, err)
	}
	return buildFabric(c, cfg, rec.IPK, key, rec.NOC)
}

// fabricRecord is the record of f, its key and NOC included.
func fabricRecord(f *Fabric, c *ca.CA) ca.FabricRecord {
	r := ca.FabricRecord{
		FabricID: f.FabricID, RootNodeID: f.RootNodeID, RootVendorID: f.RootVendorID,
		IPK: f.IPK, FabricIndex: f.FabricIndex, Label: f.Label, RootPublicKey: c.RootPublicKey(),
	}
	if id := f.Identity; id != nil && id.PrivateKey != nil {
		pub, _ := id.PrivateKey.PublicKey.Bytes() // a P-256 key this package generated or parsed
		priv, _ := id.PrivateKey.Bytes()
		r.KeyPair, r.NOC = &ca.KeyPair{PublicKey: pub, PrivateKey: priv}, id.NOC
	}
	return r
}

// buildFabric is the operational identity over a key and its NOC: the
// compressed fabric id, the operational IPK (sigma.DeriveOperationalIPK),
// the root public key and the CA's verifier.
func buildFabric(c *ca.CA, cfg FabricConfig, ipk []byte, key *ecdsa.PrivateKey, noc []byte) (*Fabric, error) {
	root := c.RootPublicKey()
	cfid, err := ca.CompressedFabricID(root, cfg.FabricID)
	if err != nil {
		return nil, err
	}
	opIPK, err := sigma.DeriveOperationalIPK(ipk, cfid)
	if err != nil {
		return nil, err
	}
	return &Fabric{
		Fabric: handshake.Fabric{
			Identity: &sigma.Identity{
				NOC: noc, ICAC: c.ICACCert(), PrivateKey: key,
				NodeID: cfg.RootNodeID, FabricID: cfg.FabricID, CompressedFabricID: cfid,
				IPK: opIPK, FabricIndex: cfg.FabricIndex,
			},
			RootPublicKey: root,
			Verifier:      c.Verifier(),
		},
		FabricID:     cfg.FabricID,
		RootNodeID:   cfg.RootNodeID,
		RootVendorID: cfg.VendorID,
		IPK:          append([]byte(nil), ipk...),
		Label:        cfg.Label,
		FabricIndex:  cfg.FabricIndex,
		CA:           c,
	}, nil
}

// AddressOf is the peer address of nodeID on the fabric — matter.js
// Fabric.addressOf (Fabric.ts:482-484).
func (f *Fabric) AddressOf(nodeID uint64) handshake.PeerAddress {
	return handshake.PeerAddress{FabricIndex: f.FabricIndex, NodeID: nodeID}
}

// Lookup is the fabric of a peer address when it is this fabric's — a
// peer.Context.Fabrics for a set of one fabric.
func (f *Fabric) Lookup(a handshake.PeerAddress) (handshake.Fabric, bool) {
	if a.FabricIndex != f.FabricIndex {
		return handshake.Fabric{}, false
	}
	return f.Fabric, true
}

// Operational node id range — matter.js packages/types/src/datatype/
// NodeId.ts:27-28 (OPERATIONAL_NODE_MIN, OPERATIONAL_NODE_MAX) at 0ceb202d.
const (
	operationalNodeMin uint64 = 0x0000_0000_0000_0001
	operationalNodeMax uint64 = 0xFFFF_FFEF_FFFF_FFFF
)

func isOperationalNodeID(id uint64) bool { return id >= operationalNodeMin && id <= operationalNodeMax }
