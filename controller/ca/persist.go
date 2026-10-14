// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package ca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// FabricRecord is what a controller keeps of the one fabric it owns, beside
// the CA that roots it: enough to rebuild the same operational identity on
// the next start, so every node it commissioned still trusts it.
//
// It is matter.js Fabric.Config (packages/protocol/src/fabric/Fabric.ts:666-685
// at 0ceb202d), which FabricManager persists as a list under "fabrics" with
// "nextFabricIndex" beside it (packages/protocol/src/fabric/FabricManager.ts:66-71),
// narrowed to one fabric per store. Kept: fabricIndex, fabricId, rootNodeId
// (which is the controller's nodeId on its own fabric), rootVendorId,
// identityProtectionKey, label, keyPair and operationalCert, and the derived
// rootPublicKey as a consistency check against the CA. Dropped: rootCert and
// intermediateCACert (the CA's [Stored] holds them), vidVerificationStatement
// and vvsc (no VID verification on the controller side), globalId and
// operationalIdentityProtectionKey (derived again on load), and
// nextFabricIndex (no fabric list).
type FabricRecord struct {
	FabricID     uint64 `json:"fabricId"`
	RootNodeID   uint64 `json:"rootNodeId"`
	RootVendorID uint16 `json:"rootVendorId"`
	// IPK is the 16-byte epoch identity protection key.
	IPK         []byte `json:"identityProtectionKey"`
	FabricIndex uint8  `json:"fabricIndex"`
	Label       string `json:"label"`
	// KeyPair is the controller's operational key; NOC its operational
	// certificate. Both nil makes the loader mint a new NOC from the CA.
	KeyPair *KeyPair `json:"keyPair,omitempty"`
	NOC     []byte   `json:"operationalCert,omitempty"`
	// RootPublicKey is the RCAC public key the record was made under.
	RootPublicKey []byte `json:"rootPublicKey,omitempty"`
}

// FabricStore keeps a [FabricRecord].
type FabricStore interface {
	// LoadFabric returns what is stored; ok is false when nothing is.
	LoadFabric() (r FabricRecord, ok bool, err error)
	// SaveFabric replaces what is stored.
	SaveFabric(r FabricRecord) error
}

// LoadFabric implements [FabricStore].
func (m *MemoryStore) LoadFabric() (FabricRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fabric == nil {
		return FabricRecord{}, false, nil
	}
	return m.fabric.clone(), true, nil
}

// SaveFabric implements [FabricStore].
func (m *MemoryStore) SaveFabric(r FabricRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := r.clone()
	m.fabric = &c
	return nil
}

func (r FabricRecord) clone() FabricRecord {
	r.IPK = append([]byte(nil), r.IPK...)
	r.NOC = append([]byte(nil), r.NOC...)
	r.RootPublicKey = append([]byte(nil), r.RootPublicKey...)
	if r.KeyPair != nil {
		kp := r.KeyPair.clone()
		r.KeyPair = &kp
	}
	return r
}

// Settings is a string key-value store — *store.Store's GetSetting and
// SetSetting, the table the node keeps its own settings in.
type Settings interface {
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
}

// SettingStore is a [Store] and a [FabricStore] under keys of a [Settings]
// store: the CA as JSON under Key, the fabric record under Key+".fabric".
// The JSON field names are matter.js's (CertificateAuthority.Configuration,
// CertificateAuthority.ts:448-474; Fabric.Config, Fabric.ts:666-685), byte
// strings in base64.
type SettingStore struct {
	Settings Settings
	// Key is the settings key of the CA; "" is "controller.ca".
	Key string
}

// DefaultSettingKey is the key a [SettingStore] with no Key uses.
const DefaultSettingKey = "controller.ca"

func (s SettingStore) key() string {
	if s.Key == "" {
		return DefaultSettingKey
	}
	return s.Key
}

// FabricKey is the settings key the fabric record is kept under.
func (s SettingStore) FabricKey() string { return s.key() + ".fabric" }

// storedJSON is [Stored] with matter.js's field names
// (CertificateAuthority.ts:448-474).
type storedJSON struct {
	RootCertID        uint64   `json:"rootCertId"`
	RootKeyIdentifier []byte   `json:"rootKeyIdentifier"`
	RootCertBytes     []byte   `json:"rootCertBytes"`
	NextCertificateID uint64   `json:"nextCertificateId"`
	RootKeyPair       *KeyPair `json:"rootKeyPair,omitempty"`
	ICACCertID        *uint64  `json:"icacCertId,omitempty"`
	ICACKeyPair       *KeyPair `json:"icacKeyPair,omitempty"`
	ICACKeyIdentifier []byte   `json:"icacKeyIdentifier,omitempty"`
	ICACCertBytes     []byte   `json:"icacCertBytes,omitempty"`
}

// MarshalJSON gives [KeyPair] matter.js BinaryKeyPair's field names.
func (k KeyPair) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		PublicKey  []byte `json:"publicKey"`
		PrivateKey []byte `json:"privateKey"`
	}{k.PublicKey, k.PrivateKey})
}

// UnmarshalJSON is the inverse of [KeyPair.MarshalJSON].
func (k *KeyPair) UnmarshalJSON(b []byte) error {
	var v struct {
		PublicKey  []byte `json:"publicKey"`
		PrivateKey []byte `json:"privateKey"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	k.PublicKey, k.PrivateKey = v.PublicKey, v.PrivateKey
	return nil
}

func (s SettingStore) load(key string, into any) (bool, error) {
	if s.Settings == nil {
		return false, errors.New("ca: SettingStore without Settings")
	}
	v, ok, err := s.Settings.GetSetting(context.Background(), key)
	if err != nil || !ok {
		return false, err
	}
	if err := json.Unmarshal([]byte(v), into); err != nil {
		return false, fmt.Errorf("ca: setting %q: %w", key, err)
	}
	return true, nil
}

func (s SettingStore) save(key string, v any) error {
	if s.Settings == nil {
		return errors.New("ca: SettingStore without Settings")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Settings.SetSetting(context.Background(), key, string(b))
}

// Load implements [Store].
func (s SettingStore) Load() (Stored, bool, error) {
	var j storedJSON
	ok, err := s.load(s.key(), &j)
	if err != nil || !ok {
		return Stored{}, false, err
	}
	st := Stored{
		RootCertID:        j.RootCertID,
		RootKeyIdentifier: j.RootKeyIdentifier,
		RootCertBytes:     j.RootCertBytes,
		NextCertificateID: j.NextCertificateID,
		RootKeyPair:       j.RootKeyPair,
	}
	if j.ICACCertID != nil && j.ICACKeyPair != nil {
		st.ICAC = &StoredICAC{CertID: *j.ICACCertID, KeyPair: *j.ICACKeyPair, KeyIdentifier: j.ICACKeyIdentifier, CertBytes: j.ICACCertBytes}
	}
	return st, true, nil
}

// Save implements [Store].
func (s SettingStore) Save(st Stored) error {
	j := storedJSON{
		RootCertID:        st.RootCertID,
		RootKeyIdentifier: st.RootKeyIdentifier,
		RootCertBytes:     st.RootCertBytes,
		NextCertificateID: st.NextCertificateID,
		RootKeyPair:       st.RootKeyPair,
	}
	if st.ICAC != nil {
		id := st.ICAC.CertID
		kp := st.ICAC.KeyPair
		j.ICACCertID, j.ICACKeyPair, j.ICACKeyIdentifier, j.ICACCertBytes = &id, &kp, st.ICAC.KeyIdentifier, st.ICAC.CertBytes
	}
	return s.save(s.key(), j)
}

// LoadFabric implements [FabricStore].
func (s SettingStore) LoadFabric() (FabricRecord, bool, error) {
	var r FabricRecord
	ok, err := s.load(s.FabricKey(), &r)
	if err != nil || !ok {
		return FabricRecord{}, false, err
	}
	return r, true, nil
}

// SaveFabric implements [FabricStore].
func (s SettingStore) SaveFabric(r FabricRecord) error { return s.save(s.FabricKey(), r) }
