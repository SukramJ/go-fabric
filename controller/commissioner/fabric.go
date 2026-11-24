// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"unicode/utf8"

	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/controller/ca"
	"thingfabric.dev/go-fabric/secure/mattercert"
	"thingfabric.dev/go-fabric/secure/sigma"
)

// FabricConfig configures [NewFabric].
type FabricConfig struct {
	// FabricID is the fabric id; required, not 0.
	FabricID uint64
	// RootNodeID is our node id on the fabric, the CaseAdminSubject AddNOC
	// grants Administer to; required, an operational node id.
	RootNodeID uint64
	// VendorID is the AdminVendorId of AddNOC (the fabric's root vendor);
	// 0 is the test vendor 0xFFF1, and one above 0xFFF4 is refused.
	VendorID uint16
	// IPK is the 16-byte identity protection key (the epoch key AddNOC
	// carries); nil draws one from Rand.
	IPK []byte
	// Label is the fabric label UpdateFabricLabel sets: at most
	// [MaxLabelLength] bytes, else [ErrLabel].
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
	// OperationalSigner, when set, is our operational key: a P-256
	// crypto.Signer whose private key the store never sees (a key file,
	// an HSM, a KMS); CASE signs Sigma3 through it. The NOC is issued for
	// its public key; the stored record holds the NOC, the public key and
	// a KeyRef ([ca.KeyRefOf]), no private key. A stored NOC for another
	// key is refused with [ca.ErrConfig] (with [ErrFabricMismatch]) and the
	// record kept; a stored private key of the signer's key leaves the
	// record. matter.js has no such seam
	// (Fabric.ts:50-76 keeps #keyPair).
	OperationalSigner crypto.Signer
	// Logger receives the fabric's load warnings — a stored label cut to
	// [MaxLabelLength] bytes, a failed save of the cut; nil discards them, as [Config.Logger] and
	// [NewFabricAuthority]. In [FabricAuthority.DefaultFabric] nil is the
	// authority's logger.
	Logger *slog.Logger
}

// ErrFabricMismatch is a stored fabric record that does not fit: its
// fabric id or root node id differs from a non-zero FabricConfig value, or
// it was made under another root than the CA's.
var ErrFabricMismatch = errors.New("commissioner: stored fabric does not match")

// Fabric is our side of a fabric the commissioner commissions into:
// [handshake.Fabric] (our operational identity, the root public key, the
// verifier) and what the flow needs of matter.js Fabric
// (packages/protocol/src/fabric/Fabric.ts:61-74 at db021da0): the fabric
// id, the root node id (caseAdminSubject, ControllerCommissioningFlow.ts:1469),
// the root vendor id (adminVendorId, :1468, Fabric.ts:257), the identity
// protection key (:1467), the label (:1512, Fabric.ts:189) and the fabric
// index (Fabric.ts:545).
//
// A Fabric must not be copied after first use: it holds the label's
// locks. The zero value is usable for [Fabric.SetLabel] and
// [Fabric.CurrentLabel] (review II, R2-P6-conc-1858).
type Fabric struct {
	handshake.Fabric
	FabricID     uint64
	RootNodeID   uint64
	RootVendorID uint16
	IPK          []byte
	// Label is the fabric label the fabric was built with; read the
	// current one with [Fabric.CurrentLabel] once [Fabric.SetLabel] may
	// run.
	Label       string
	FabricIndex uint8
	// CA is the authority that issues the fabric's NOCs.
	CA *ca.CA

	keyRef string // set: Identity.Signer is FabricConfig.OperationalSigner
	// labelMu guards Label against SetLabel; setLabelMu serialises
	// SetLabel across its save, so CurrentLabel never waits for the store
	// (review II, R2-P6-conc-1858). store is FabricConfig.Store, where
	// SetLabel saves.
	labelMu    sync.RWMutex
	setLabelMu sync.Mutex
	store      ca.FabricStore
}

// MaxLabelLength is the longest fabric label, in bytes of its UTF-8
// encoding — the bound a device enforces, connectedhomeip's
// kFabricLabelMaxLengthInBytes (src/credentials/FabricTable.h:48 at
// 6170af84) and this module's device side. matter.js's Fabric.setLabel
// counts the label's UTF-8 bytes against the same 32 since #4995
// (packages/protocol/src/fabric/Fabric.ts:194, Fabric.MAX_LABEL_BYTES
// :727 at b25ed960): both count bytes. The module's choice (review II,
// R2-P6-par-1854, decision 155).
const MaxLabelLength = 32

// ErrLabel is a fabric label longer than [MaxLabelLength] bytes: refused
// by [NewFabric], [FabricAuthority.CreateFabric] and [Fabric.SetLabel]
// for the label they are given. A stored record's longer label is not
// refused: the load cuts it to the bound (see [NewFabric]).
var ErrLabel = errors.New("commissioner: fabric label must be at most 32 bytes long")

// validLabel is the one check of a fabric label given at creation and in
// SetLabel; a stored label is cut at load instead (storedLabel).
func validLabel(label string) error {
	if len(label) > MaxLabelLength {
		return ErrLabel
	}
	return nil
}

// storedLabel is a stored fabric label held to [MaxLabelLength] bytes: a
// longer one — stored by v0.22.x, whose SetLabel bounded the label in
// runes — is cut to the longest prefix of at most MaxLabelLength bytes
// that ends between two characters, with a warning through log (nil
// discards it), as
// cluster/core BasicInformation.RestoreNodeLabel cuts a stored NodeLabel
// (cluster/core/basic_information.go:969, shortenStored :990, the cut
// truncateUTF8 in cluster/core/utf8.go) (go-fabric #587, decision 557).
// shortened reports whether it was cut; loadFabric then saves the
// record once with the cut label (saveCutLabel, decision 597).
//
// Mirrors matter.js packages/protocol/src/fabric/FabricManager.ts:39-48
// (shortenStoredLabel): the stored label truncated to
// Fabric.MAX_LABEL_BYTES in UTF-8 and the cut warned naming both values.
func storedLabel(label string, log *slog.Logger) (_ string, shortened bool) {
	if len(label) <= MaxLabelLength {
		return label, false
	}
	cut := MaxLabelLength
	for cut > 0 && !utf8.RuneStart(label[cut]) {
		cut--
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log.Warn("matter.controller.fabric.stored_label_shortened",
		slog.Int("max_bytes", MaxLabelLength), slog.String("stored", label), slog.String("label", label[:cut]))
	return label[:cut], true
}

// saveCutLabel saves the stored record rec with its label replaced by the
// cut one, every other field as stored, so the next load neither cuts nor
// warns again (R4-par-4403, decision 597). A failed save is warned
// through log (nil discards it) and the load goes on with the cut label;
// the record keeps the stored label, cut again at the next load.
//
// Mirrors matter.js packages/protocol/src/fabric/FabricManager.ts:93-101:
// the shortened configs are written back at once when any changed, and a
// failed write is warned — "they are shortened again at the next start".
func saveCutLabel(store ca.FabricStore, rec ca.FabricRecord, label string, log *slog.Logger) {
	rec.Label = label
	if err := store.SaveFabric(rec); err != nil {
		if log == nil {
			log = slog.New(slog.DiscardHandler)
		}
		log.Warn("matter.controller.fabric.stored_label_save_failed",
			slog.String("label", label), slog.Any("err", err))
	}
}

// CurrentLabel is the fabric's label: the one it was built with, or the
// one [Fabric.SetLabel] set last.
func (f *Fabric) CurrentLabel() string {
	f.labelMu.RLock()
	defer f.labelMu.RUnlock()
	return f.Label
}

// SetLabel sets the fabric's label and saves the fabric record when it
// changed — after matter.js Fabric.setLabel (Fabric.ts:193-201 at
// db021da0), which sets it before it persists, and bounds the label at 32
// UTF-8 bytes since #4995 (Fabric.ts:194 at b25ed960); here the bound is
// [MaxLabelLength] bytes, the same, and a
// changed label is persisted before it takes effect (the module's own
// rule, decision 155): a longer one is [ErrLabel], an unchanged one is
// nothing. The
// commissioner gives the new
// label to the nodes it commissions from then on; a client.Registry
// reconciles it to the nodes commissioned before (Registry.SetFabricLabel).
func (f *Fabric) SetLabel(label string) error {
	if err := validLabel(label); err != nil {
		return err
	}
	// setLabelMu, not labelMu, is held across the save: CurrentLabel —
	// which the flow reads at step 98.1 — never waits for the store.
	f.setLabelMu.Lock()
	defer f.setLabelMu.Unlock()
	if f.CurrentLabel() == label {
		return nil
	}
	if f.store != nil {
		rec := fabricRecord(f, f.CA)
		rec.Label = label
		if err := f.store.SaveFabric(rec); err != nil {
			return fmt.Errorf("commissioner: save fabric: %w", err)
		}
	}
	f.labelMu.Lock()
	f.Label = label
	f.labelMu.Unlock()
	return nil
}

// NewFabric loads the fabric cfg.Store keeps, or mints our own key and NOC
// through c and builds the fabric's operational identity — the key, the NOC, the ICAC, the operational IPK
// (sigma.DeriveOperationalIPK over the compressed fabric id), the ids —
// as bridge/bridgetest's Commissioner.identity does for its test CA.
//
// A stored label longer than [MaxLabelLength] bytes is not refused: the
// load cuts it to the bound between two characters and warns through
// cfg.Logger (nil discards the warning), and the record is saved once with
// the cut label; a failed save is warned and the load goes on, the cut
// repeated at the next load. cfg.Label itself is still [ErrLabel] when
// longer.
func NewFabric(c *ca.CA, cfg FabricConfig) (*Fabric, error) {
	f, err := newFabric(c, cfg)
	if err != nil {
		return nil, err
	}
	f.store = cfg.Store
	return f, nil
}

// newFabric is [NewFabric] without the store kept for SetLabel.
func newFabric(c *ca.CA, cfg FabricConfig) (*Fabric, error) {
	if c == nil {
		return nil, errors.New("commissioner: NewFabric needs a CA")
	}
	if err := validLabel(cfg.Label); err != nil {
		return nil, err
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
	// Mirrors matter.js packages/protocol/src/fabric/FabricAuthority.ts:47, :139-147
	// (createFabric): an unset admin vendor id is DEFAULT_ADMIN_VENDOR_ID,
	// a reserved one is refused before anything is minted.
	switch {
	case cfg.VendorID == 0:
		cfg.VendorID = defaultAdminVendorID
	case cfg.VendorID > maxOperationalVendorID:
		return nil, fmt.Errorf("commissioner: Admin vendor ID %#x is reserved (0x0000 or above 0xFFF4); devices refuse it in AddNOC", cfg.VendorID)
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
	var key *ecdsa.PrivateKey
	var pub []byte
	var err error
	if cfg.OperationalSigner != nil {
		if pub, err = ca.P256PublicKey(cfg.OperationalSigner); err != nil {
			return nil, fmt.Errorf("commissioner: operational signer: %w", err)
		}
	} else {
		if key, err = ecdsa.GenerateKey(elliptic.P256(), cfg.Rand); err != nil {
			return nil, fmt.Errorf("commissioner: key: %w", err)
		}
		if pub, err = key.PublicKey.Bytes(); err != nil {
			return nil, err
		}
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
// controller owns. A stored label over [MaxLabelLength] bytes is cut
// (storedLabel), warned through cfg.Logger, and the record saved once
// with the cut label after the fabric is built: by the save a path does
// anyway (a minted identity, a signer record re-saved), else by
// saveCutLabel (decision 597).
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
	stored := rec
	var cut bool
	rec.Label, cut = storedLabel(rec.Label, cfg.Logger)
	if rec.FabricIndex == 0 {
		rec.FabricIndex = 1
	}
	cfg.FabricID, cfg.RootNodeID, cfg.VendorID = rec.FabricID, rec.RootNodeID, rec.RootVendorID
	cfg.Label, cfg.FabricIndex = rec.Label, rec.FabricIndex
	// A stored operational key is never dropped (decision 191, Fable read
	// F3): a record with a key pair but no NOC, or a NOC but no key pair,
	// is refused instead of a new identity saved over it. A record that
	// keeps neither gets one minted below.
	hasKey := rec.KeyPair != nil && (len(rec.KeyPair.PrivateKey) > 0 || len(rec.KeyPair.PublicKey) > 0)
	if hasNOC := len(rec.NOC) > 0; hasKey != hasNOC {
		return nil, fmt.Errorf("%w: %w: the stored fabric holds its operational key pair or its NOC without the other", ErrFabricMismatch, ca.ErrConfig)
	}
	if cfg.OperationalSigner != nil {
		f, saved, err := loadSignerFabric(c, cfg, rec)
		if err == nil && cut && !saved {
			saveCutLabel(cfg.Store, stored, rec.Label, cfg.Logger)
		}
		return f, err
	}
	if rec.KeyRef != "" && (rec.KeyPair == nil || len(rec.KeyPair.PrivateKey) == 0) {
		return nil, fmt.Errorf("%w: the operational key %q is not configured (FabricConfig.OperationalSigner)", ErrFabricMismatch, rec.KeyRef)
	}
	if rec.KeyPair == nil || len(rec.NOC) == 0 {
		// No identity kept: mint one under the stored ids and save it.
		cfg.IPK = rec.IPK
		store := cfg.Store
		cfg.Store = nil
		f, err := newFabric(c, cfg)
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
	if err := checkStoredNOC(c, rec, &key.PublicKey); err != nil {
		return nil, err
	}
	f, err := buildFabric(c, cfg, rec.IPK, key, rec.NOC)
	if err != nil {
		return nil, err
	}
	if cut {
		saveCutLabel(cfg.Store, stored, rec.Label, cfg.Logger)
	}
	return f, nil
}

// checkStoredNOC holds a stored NOC to the record it is stored in: it
// verifies under the CA (with the CA's ICAC), it is for the stored key,
// and it names the record's node and fabric. A NOC that does not would
// fail at the first CASE or AddNOC with an error that names nothing
// (review II, R2-P6-store-1853 (c)).
func checkStoredNOC(c *ca.CA, rec ca.FabricRecord, key *ecdsa.PublicKey) error {
	nocKey, err := c.Verifier().VerifyAndExtractPubKey(rec.NOC, c.ICACCert())
	if err != nil {
		return fmt.Errorf("%w: the stored NOC does not verify under the CA: %w", ErrFabricMismatch, err)
	}
	if !nocKey.Equal(key) {
		return fmt.Errorf("%w: the stored NOC is not for the stored operational key", ErrFabricMismatch)
	}
	noc, err := mattercert.Decode(rec.NOC)
	if err != nil {
		return fmt.Errorf("%w: the stored NOC: %w", ErrFabricMismatch, err)
	}
	if noc.Subject.MatterNodeID != rec.RootNodeID || noc.Subject.MatterFabricID != rec.FabricID {
		return fmt.Errorf("%w: the stored NOC names node 0x%016X on fabric 0x%016X, the record node 0x%016X on fabric 0x%016X",
			ErrFabricMismatch, noc.Subject.MatterNodeID, noc.Subject.MatterFabricID, rec.RootNodeID, rec.FabricID)
	}
	return nil
}

// loadSignerFabric is loadFabric with an operational signer: the stored
// NOC is kept when it is for the signer's key and passes checkStoredNOC
// (verifies under the CA, names the record's node and fabric), and a
// stored NOC for another key — or one that does not decode or does not
// pass that check — is refused with
// ca.ErrConfig, the record left as it is (decision 191, Fable read F3):
// the signer is held to the stored certificate as the CA holds its
// signers (ca.Config.RootSigner). A record without a NOC gets one minted
// under the stored ids; a record that still holds the private key the
// signer now keeps is saved again without it. saved reports whether the
// record was saved.
func loadSignerFabric(c *ca.CA, cfg FabricConfig, rec ca.FabricRecord) (_ *Fabric, saved bool, _ error) {
	pub, err := ca.P256PublicKey(cfg.OperationalSigner)
	if err != nil {
		return nil, false, fmt.Errorf("commissioner: operational signer: %w", err)
	}
	store := cfg.Store
	keep := false
	if len(rec.NOC) > 0 {
		noc, err := mattercert.Decode(rec.NOC)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %w: the stored NOC: %w", ErrFabricMismatch, ca.ErrConfig, err)
		}
		if !bytes.Equal(noc.PublicKey, pub) {
			return nil, false, fmt.Errorf("%w: %w: the operational signer's key is not the stored NOC's", ErrFabricMismatch, ca.ErrConfig)
		}
		// The key path's rule holds the signer path too (R2-C2-store-1801):
		// the NOC verifies under the CA and names the record's node and
		// fabric, or the record is refused and left as it is.
		signerKey, _ := cfg.OperationalSigner.Public().(*ecdsa.PublicKey) // P-256 ECDSA: ca.P256PublicKey above
		if err := checkStoredNOC(c, rec, signerKey); err != nil {
			return nil, false, fmt.Errorf("%w: %w", ca.ErrConfig, err)
		}
		keep = true
	}
	var f *Fabric
	if keep {
		if f, err = buildFabric(c, cfg, rec.IPK, nil, rec.NOC); err != nil {
			return nil, false, err
		}
	} else {
		cfg.IPK = rec.IPK
		cfg.Store = nil
		if f, err = newFabric(c, cfg); err != nil {
			return nil, false, err
		}
	}
	if fresh := fabricRecord(f, c); !keep || rec.KeyRef != fresh.KeyRef || (rec.KeyPair != nil && len(rec.KeyPair.PrivateKey) > 0) {
		if err := store.SaveFabric(fresh); err != nil {
			return nil, false, fmt.Errorf("commissioner: save fabric: %w", err)
		}
		return f, true, nil
	}
	return f, false, nil
}

// fabricRecord is the record of f, its key and NOC included — the public
// key and the KeyRef only when a signer keeps the key.
func fabricRecord(f *Fabric, c *ca.CA) ca.FabricRecord {
	r := ca.FabricRecord{
		FabricID: f.FabricID, RootNodeID: f.RootNodeID, RootVendorID: f.RootVendorID,
		IPK: f.IPK, FabricIndex: f.FabricIndex, Label: f.Label, RootPublicKey: c.RootPublicKey(),
	}
	if id := f.Identity; id != nil && id.PrivateKey == nil && id.Signer != nil {
		pub, _ := ca.P256PublicKey(id.Signer) // checked when the fabric was built
		r.KeyPair, r.NOC, r.KeyRef = &ca.KeyPair{PublicKey: pub}, id.NOC, f.keyRef
		return r
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
// the root public key and the CA's verifier. key is nil when
// cfg.OperationalSigner keeps the key.
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
	id := &sigma.Identity{
		NOC: noc, ICAC: c.ICACCert(), PrivateKey: key,
		NodeID: cfg.RootNodeID, FabricID: cfg.FabricID, CompressedFabricID: cfid,
		IPK: opIPK, FabricIndex: cfg.FabricIndex,
	}
	var keyRef string
	if key == nil {
		if cfg.OperationalSigner == nil {
			return nil, errors.New("commissioner: no operational key")
		}
		id.Signer, keyRef = cfg.OperationalSigner, ca.KeyRefOf(cfg.OperationalSigner)
	}
	return &Fabric{
		Fabric: handshake.Fabric{
			Identity:      id,
			RootPublicKey: root,
			Verifier:      c.Verifier(),
		},
		keyRef:       keyRef,
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
// Fabric.addressOf (Fabric.ts:505-507).
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

// Admin vendor ids of a new fabric.
const (
	// defaultAdminVendorID is matter.js DEFAULT_ADMIN_VENDOR_ID
	// (FabricAuthority.ts:47), the test vendor.
	defaultAdminVendorID uint16 = 0xFFF1
	// maxOperationalVendorID is the upper bound of matter.js's VendorId
	// constructor (packages/types/src/datatype/VendorId.ts:22). Its
	// isOperational applies the bound (VendorId.ts:44).
	maxOperationalVendorID uint16 = 0xFFF4
)

// Operational node id range — matter.js packages/types/src/datatype/
// NodeId.ts:27-28 (OPERATIONAL_NODE_MIN, OPERATIONAL_NODE_MAX) at b25ed960.
const (
	operationalNodeMin uint64 = 0x0000_0000_0000_0001
	operationalNodeMax uint64 = 0xFFFF_FFEF_FFFF_FFFF
)

func isOperationalNodeID(id uint64) bool { return id >= operationalNodeMin && id <= operationalNodeMax }
