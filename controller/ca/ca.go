// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca

import (
	"bytes"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // Matter key identifiers are SHA-1 of the public key (CertificateAuthority.ts:170, :254, :314)
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// Certificate ids. Mirrors matter.js
// packages/protocol/src/certificate/CertificateAuthority.ts:52
// (#rootCertId = 0) and :56 (#nextCertificateId = 1) at 0ceb202d.
const (
	RootCertID         uint64 = 0
	FirstCertificateID uint64 = 1
)

// Validity of every certificate the CA issues, in years around the
// issuing instant — CertificateAuthority.ts:233-234, :263-264, :304-305
// (jsToMatterDate(now, -1), jsToMatterDate(now, 10)).
const (
	NotBeforeYears = -1
	NotAfterYears  = 10
)

// ErrConfig is a stored CA that does not fit the configuration: its ICAC
// presence differs from Config.IntermediateCA, or it has neither an ICAC
// nor a root private key to sign with — matter.js ImplementationError
// (CertificateAuthority.ts:367-373, :382-387).
var ErrConfig = errors.New("ca: stored credentials do not match the configuration")

// KeyPair is a P-256 key pair as the store keeps it: the 65-byte
// uncompressed public key and the 32-byte private scalar — matter.js
// BinaryKeyPair.
type KeyPair struct {
	PublicKey  []byte
	PrivateKey []byte
}

// Stored is what a [Store] keeps of a CA — matter.js
// CertificateAuthority.Configuration (CertificateAuthority.ts:446-484),
// written by #buildStorageData (:391-409).
type Stored struct {
	// RootCertID is the RCAC id (rootCertId).
	RootCertID uint64
	// RootKeyIdentifier is the SHA-1 of the root public key.
	RootKeyIdentifier []byte
	// RootCertBytes is the RCAC as Matter TLV.
	RootCertBytes []byte
	// NextCertificateID is the id of the next certificate to issue.
	NextCertificateID uint64
	// RootKeyPair is the root key; optional with an ICAC (a 3-tier PKI
	// whose root key stays offline, :455-474).
	RootKeyPair *KeyPair
	// ICAC is the intermediate CA, nil for a 2-tier PKI.
	ICAC *StoredICAC
	// RootKeyRef names the root key when it is a [Config.RootSigner]: the
	// record then has no RootKeyPair, the key stays where the signer
	// keeps it. "" for a key in RootKeyPair.
	RootKeyRef string
}

// StoredICAC is the ICAC part of [Stored] (icacCertId, icacKeyPair,
// icacKeyIdentifier, icacCertBytes).
type StoredICAC struct {
	CertID        uint64
	KeyPair       KeyPair
	KeyIdentifier []byte
	CertBytes     []byte
	// KeyRef names the ICAC key when it is a [Config.ICACSigner]; KeyPair
	// then holds the public key only.
	KeyRef string
}

// Store keeps a CA's key material and certificate counter.
type Store interface {
	// Load returns what is stored; ok is false when nothing is.
	Load() (s Stored, ok bool, err error)
	// Save replaces what is stored.
	Save(s Stored) error
}

// MemoryStore is a [Store] and a [FabricStore] in memory. The zero value is empty and ready to
// use; it is safe for concurrent use.
type MemoryStore struct {
	// mu guards saves and stored.
	mu     sync.Mutex
	stored *Stored
	fabric *FabricRecord
	saves  int
}

// Load implements [Store].
func (m *MemoryStore) Load() (Stored, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stored == nil {
		return Stored{}, false, nil
	}
	return m.stored.clone(), true, nil
}

// Save implements [Store].
func (m *MemoryStore) Save(s Stored) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := s.clone()
	m.stored = &c
	m.saves++
	return nil
}

// Saves is the number of Save calls so far.
func (m *MemoryStore) Saves() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saves
}

func (s Stored) clone() Stored {
	s.RootKeyIdentifier = append([]byte(nil), s.RootKeyIdentifier...)
	s.RootCertBytes = append([]byte(nil), s.RootCertBytes...)
	if s.RootKeyPair != nil {
		kp := s.RootKeyPair.clone()
		s.RootKeyPair = &kp
	}
	if s.ICAC != nil {
		i := *s.ICAC
		i.KeyPair = i.KeyPair.clone()
		i.KeyIdentifier = append([]byte(nil), i.KeyIdentifier...)
		i.CertBytes = append([]byte(nil), i.CertBytes...)
		s.ICAC = &i
	}
	return s
}

func (k KeyPair) clone() KeyPair {
	return KeyPair{PublicKey: append([]byte(nil), k.PublicKey...), PrivateKey: append([]byte(nil), k.PrivateKey...)}
}

// Config configures [New].
type Config struct {
	// Store keeps the CA; nil keeps nothing (a fresh root every New, as
	// matter.js without a storage context).
	Store Store
	// Clock is the issuing instant of every certificate and the
	// verifier's time. Set, it wins over Now; neither set is
	// [clock.System].
	Clock clock.Clock
	// Now is the issuing instant of every certificate.
	//
	// Deprecated: set Clock; Now may be removed in v0.10.0.
	Now func() time.Time
	// IntermediateCA issues an ICAC that signs the NOCs (3-tier PKI). A
	// stored CA must match it.
	IntermediateCA bool
	// DetectIntermediateCA lets a stored CA decide IntermediateCA: a
	// stored ICAC is loaded and required, a stored CA without one stays
	// 2-tier. A new CA follows IntermediateCA. It is matter.js's
	// generateIntermediateCert left undefined (CertificateAuthority.ts:157-159).
	DetectIntermediateCA bool
	// Rand is the randomness of keys and signatures; nil is crypto/rand.
	// Since Go 1.26 crypto/ecdsa ignores a caller's reader for key
	// generation and signing (a secure source is always used; see
	// ecdsa.GenerateKey), so a seeded reader no longer makes the
	// certificates reproducible — a test that needs that uses
	// testing/cryptotest.SetGlobalRandom.
	Rand io.Reader
	// RootSigner, when set, is the root key: a P-256 crypto.Signer whose
	// private key the store never sees (a key file, an HSM, a KMS). A new
	// CA issues its RCAC for the signer's public key; a stored CA must
	// have been made under it. The stored record then holds the
	// certificates and RootKeyRef, no root private key; a record that
	// still holds one is rewritten without it. matter.js has no such seam
	// (its CertificateAuthority keeps the key pair in its storage).
	RootSigner crypto.Signer
	// ICACSigner is RootSigner for the ICAC key, with IntermediateCA.
	ICACSigner crypto.Signer
}

// KeyReferencer is a signer that names its key for the stored record —
// a path, a PKCS#11 URI, a KMS key name. A [Config.RootSigner] or
// [Config.ICACSigner] that is none is recorded as [ExternalKeyRef].
type KeyReferencer interface {
	KeyRef() string
}

// ExternalKeyRef is the KeyRef recorded for a signer that is no
// [KeyReferencer].
const ExternalKeyRef = "external"

// KeyRefOf is the KeyRef the record keeps for s.
func KeyRefOf(s crypto.Signer) string {
	if r, ok := s.(KeyReferencer); ok && r.KeyRef() != "" {
		return r.KeyRef()
	}
	return ExternalKeyRef
}

// P256PublicKey is the 65-byte uncompressed public key of s, an error
// when s is not a P-256 ECDSA key.
func P256PublicKey(s crypto.Signer) ([]byte, error) {
	pub, ok := s.Public().(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("ca: signer is not a P-256 ECDSA key")
	}
	return pub.Bytes()
}

// signer is the RCAC or ICAC side that signs.
type signer struct {
	id     uint64 // rcacId or icacId
	tag    uint8  // dnTagRCACID or dnTagICACID
	key    crypto.Signer
	keyRef string // set: key is a configured signer, not stored
	keyID  []byte
	cert   []byte
	public []byte
}

// CA is a fabric's certificate authority — matter.js CertificateAuthority
// (CertificateAuthority.ts:50-437) at 0ceb202d. It is safe for concurrent
// use.
type CA struct {
	cfg Config

	mu         sync.Mutex
	rootCertID uint64
	rootKey    crypto.Signer // nil: offline root with an ICAC
	rootKeyRef string        // set: rootKey is Config.RootSigner
	rootKeyID  []byte
	rootCert   []byte
	rootPub    []byte
	nextID     uint64
	icac       *signer
	verifier   *mattercert.Verifier
}

// New loads the CA cfg.Store holds, or creates one. Mirrors matter.js
// CertificateAuthority.ts:149-184 (the construction): a valid stored root
// (:336-346) is loaded (:357-389) and must agree with cfg.IntermediateCA
// (:367-373); otherwise a P-256 root key, its SHA-1 key identifier
// (:169-170), the RCAC (:225-249), the ICAC when configured (:251-285),
// and the result is saved (:181-183).
func New(cfg Config) (*CA, error) {
	switch {
	case cfg.Clock != nil:
		cfg.Now = cfg.Clock.Now
	case cfg.Now == nil:
		cfg.Now = clock.System{}.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	var st Stored
	haveStored := false
	if cfg.Store != nil {
		s, ok, err := cfg.Store.Load()
		if err != nil {
			return nil, fmt.Errorf("ca: load: %w", err)
		}
		st, haveStored = s, ok && validStoredRoot(s)
	}
	if haveStored && cfg.DetectIntermediateCA {
		// Mirrors matter.js packages/protocol/src/certificate/CertificateAuthority.ts:157-159
		// (requireIcac = generateIntermediateCert ?? #isValidStoredIcacCertificate).
		cfg.IntermediateCA = validStoredICAC(st)
	}
	if cfg.ICACSigner != nil && !cfg.IntermediateCA {
		return nil, fmt.Errorf("%w: an ICACSigner needs IntermediateCA", ErrConfig)
	}
	c := &CA{cfg: cfg, rootCertID: RootCertID, nextID: FirstCertificateID}
	if haveStored {
		if err := c.load(st); err != nil {
			return nil, err
		}
		if err := c.initVerifier(); err != nil {
			return nil, err
		}
		if storedKeyBehindSigner(st, c) {
			// A private key the configuration now keeps in a signer
			// leaves the record.
			if err := cfg.Store.Save(c.stored()); err != nil {
				return nil, fmt.Errorf("ca: save: %w", err)
			}
		}
		return c, nil
	}
	if cfg.RootSigner != nil {
		pub, err := P256PublicKey(cfg.RootSigner)
		if err != nil {
			return nil, err
		}
		c.rootKey, c.rootKeyRef, c.rootPub = cfg.RootSigner, KeyRefOf(cfg.RootSigner), pub
	} else {
		key, err := ecdsa.GenerateKey(elliptic.P256(), cfg.Rand)
		if err != nil {
			return nil, fmt.Errorf("ca: root key: %w", err)
		}
		c.rootKey = key
		c.rootPub = publicKeyBytes(&key.PublicKey)
	}
	c.rootKeyID = KeyIdentifier(c.rootPub)
	var err error
	if c.rootCert, err = c.generateRootCert(); err != nil {
		return nil, err
	}
	if cfg.IntermediateCA {
		if c.icac, err = c.generateICAC(); err != nil {
			return nil, err
		}
	}
	if cfg.Store != nil {
		if err := cfg.Store.Save(c.stored()); err != nil {
			return nil, fmt.Errorf("ca: save: %w", err)
		}
	}
	if err := c.initVerifier(); err != nil {
		return nil, err
	}
	return c, nil
}

// initVerifier builds the verifier over the root.
func (c *CA) initVerifier() error {
	v, err := mattercert.NewVerifier(c.rootPub, nowSource{c.cfg.Now})
	if err != nil {
		return fmt.Errorf("ca: root public key: %w", err)
	}
	c.verifier = v
	return nil
}

// validStoredRoot is #isValidStoredRootCertificate (:336-346): the key
// identifier and the root certificate are present.
func validStoredRoot(s Stored) bool {
	return len(s.RootKeyIdentifier) > 0 && len(s.RootCertBytes) > 0
}

// validStoredICAC is #isValidStoredIcacCertificate (:348-355); a key
// kept by a signer (KeyRef) stands in for the private key.
func validStoredICAC(s Stored) bool {
	return s.ICAC != nil && (len(s.ICAC.KeyPair.PrivateKey) > 0 || s.ICAC.KeyRef != "") &&
		len(s.ICAC.KeyIdentifier) > 0 && len(s.ICAC.CertBytes) > 0
}

// storedKeyBehindSigner reports whether s holds a private key that c now
// takes from a configured signer.
func storedKeyBehindSigner(s Stored, c *CA) bool {
	if c.cfg.RootSigner != nil && s.RootKeyPair != nil && len(s.RootKeyPair.PrivateKey) > 0 {
		return true
	}
	return c.cfg.ICACSigner != nil && s.ICAC != nil && len(s.ICAC.KeyPair.PrivateKey) > 0
}

// signerFor checks that a configured signer holds the key of a stored
// certificate's public key pub.
func signerFor(s crypto.Signer, pub []byte, what string) error {
	sp, err := P256PublicKey(s)
	if err != nil {
		return err
	}
	if !bytes.Equal(sp, pub) {
		return fmt.Errorf("%w: the %s signer's key is not the stored %s certificate's", ErrConfig, what, what)
	}
	return nil
}

// load is #loadFromStorage (:357-389).
func (c *CA) load(s Stored) error {
	c.rootCertID = s.RootCertID
	if s.RootKeyPair != nil && c.cfg.RootSigner == nil {
		k, err := privateKey(s.RootKeyPair.PrivateKey)
		if err != nil {
			return fmt.Errorf("ca: stored root key: %w", err)
		}
		c.rootKey = k
	}
	c.rootKeyID = append([]byte(nil), s.RootKeyIdentifier...)
	c.rootCert = append([]byte(nil), s.RootCertBytes...)
	c.nextID = s.NextCertificateID
	root, err := mattercert.Decode(c.rootCert)
	if err != nil {
		return fmt.Errorf("ca: stored root certificate: %w", err)
	}
	c.rootPub = root.PublicKey
	if c.cfg.RootSigner != nil {
		if err := signerFor(c.cfg.RootSigner, c.rootPub, "root"); err != nil {
			return err
		}
		c.rootKey, c.rootKeyRef = c.cfg.RootSigner, KeyRefOf(c.cfg.RootSigner)
	} else if c.rootKey == nil {
		// An offline root kept by a signer this run does not configure:
		// the record keeps naming it.
		c.rootKeyRef = s.RootKeyRef
	}
	hasICAC := validStoredICAC(s)
	if c.cfg.IntermediateCA != hasICAC {
		return fmt.Errorf("%w: stored credentials contain ICAC certificate: %v, but configuration expected it to be %v", ErrConfig, hasICAC, c.cfg.IntermediateCA)
	}
	if hasICAC {
		if c.cfg.ICACSigner != nil {
			icac, err := mattercert.Decode(s.ICAC.CertBytes)
			if err != nil {
				return fmt.Errorf("ca: stored ICAC certificate: %w", err)
			}
			if err := signerFor(c.cfg.ICACSigner, icac.PublicKey, "ICAC"); err != nil {
				return err
			}
			c.icac = &signer{
				id: s.ICAC.CertID, tag: dnTagICACID, key: c.cfg.ICACSigner, keyRef: KeyRefOf(c.cfg.ICACSigner),
				keyID:  append([]byte(nil), s.ICAC.KeyIdentifier...),
				cert:   append([]byte(nil), s.ICAC.CertBytes...),
				public: append([]byte(nil), icac.PublicKey...),
			}
			return nil
		}
		if len(s.ICAC.KeyPair.PrivateKey) == 0 {
			return fmt.Errorf("%w: the ICAC key %q is not configured (Config.ICACSigner)", ErrConfig, s.ICAC.KeyRef)
		}
		k, err := privateKey(s.ICAC.KeyPair.PrivateKey)
		if err != nil {
			return fmt.Errorf("ca: stored ICAC key: %w", err)
		}
		c.icac = &signer{
			id: s.ICAC.CertID, tag: dnTagICACID, key: k,
			keyID:  append([]byte(nil), s.ICAC.KeyIdentifier...),
			cert:   append([]byte(nil), s.ICAC.CertBytes...),
			public: publicKeyBytes(&k.PublicKey),
		}
		return nil
	}
	if c.rootKey == nil {
		if s.RootKeyRef != "" {
			return fmt.Errorf("%w: the root key %q is not configured (Config.RootSigner)", ErrConfig, s.RootKeyRef)
		}
		return fmt.Errorf("%w: a root key pair is required without an ICAC", ErrConfig)
	}
	return nil
}

// stored is #buildStorageData (:391-409).
func (c *CA) stored() Stored {
	s := Stored{
		RootCertID:        c.rootCertID,
		RootKeyIdentifier: append([]byte(nil), c.rootKeyID...),
		RootCertBytes:     append([]byte(nil), c.rootCert...),
		NextCertificateID: c.nextID,
	}
	switch k, ok := c.rootKey.(*ecdsa.PrivateKey); {
	case c.rootKeyRef != "":
		s.RootKeyRef = c.rootKeyRef
	case ok:
		s.RootKeyPair = &KeyPair{PublicKey: append([]byte(nil), c.rootPub...), PrivateKey: privateKeyBytes(k)}
	}
	if c.icac != nil {
		s.ICAC = &StoredICAC{
			CertID:        c.icac.id,
			KeyPair:       KeyPair{PublicKey: append([]byte(nil), c.icac.public...)},
			KeyIdentifier: append([]byte(nil), c.icac.keyID...),
			CertBytes:     append([]byte(nil), c.icac.cert...),
			KeyRef:        c.icac.keyRef,
		}
		if k, ok := c.icac.key.(*ecdsa.PrivateKey); ok && c.icac.keyRef == "" {
			s.ICAC.KeyPair.PrivateKey = privateKeyBytes(k)
		}
	}
	return s
}

// validity returns notBefore and notAfter around now.
func (c *CA) validity() (notBefore, notAfter uint32, err error) {
	now := c.cfg.Now()
	if notBefore, err = matterDate(now, NotBeforeYears); err != nil {
		return 0, 0, err
	}
	if notAfter, err = matterDate(now, NotAfterYears); err != nil {
		return 0, 0, err
	}
	return notBefore, notAfter, nil
}

// generateRootCert is #generateRootCert (:225-249): serial = issuer =
// subject = the root cert id, a CA with keyCertSign and cRLSign, both key
// identifiers the root's, self-signed.
func (c *CA) generateRootCert() ([]byte, error) {
	nb, na, err := c.validity()
	if err != nil {
		return nil, err
	}
	return signCert(&certFields{
		serial:    c.rootCertID,
		issuer:    []dnAttr{{dnTagRCACID, c.rootCertID}},
		notBefore: nb,
		notAfter:  na,
		subject:   []dnAttr{{dnTagRCACID, c.rootCertID}},
		pub:       c.rootPub,
		isCA:      true,
		subjectID: c.rootKeyID,
		authority: c.rootKeyID,
	}, c.rootKey, c.cfg.Rand)
}

// generateICAC is #generateIcacProps (:251-285): the next certificate id,
// a fresh key, subject {icacId}, issuer {rcacId}, a CA signed by the root.
func (c *CA) generateICAC() (*signer, error) {
	id := c.nextID
	c.nextID++
	var key crypto.Signer
	var keyRef string
	var pub []byte
	if c.cfg.ICACSigner != nil {
		p, err := P256PublicKey(c.cfg.ICACSigner)
		if err != nil {
			return nil, err
		}
		key, keyRef, pub = c.cfg.ICACSigner, KeyRefOf(c.cfg.ICACSigner), p
	} else {
		k, err := ecdsa.GenerateKey(elliptic.P256(), c.cfg.Rand)
		if err != nil {
			return nil, fmt.Errorf("ca: ICAC key: %w", err)
		}
		key, pub = k, publicKeyBytes(&k.PublicKey)
	}
	keyID := KeyIdentifier(pub)
	nb, na, err := c.validity()
	if err != nil {
		return nil, err
	}
	cert, err := signCert(&certFields{
		serial:    id,
		issuer:    []dnAttr{{dnTagRCACID, c.rootCertID}},
		notBefore: nb,
		notAfter:  na,
		subject:   []dnAttr{{dnTagICACID, id}},
		pub:       pub,
		isCA:      true,
		subjectID: keyID,
		authority: c.rootKeyID,
	}, c.rootKey, c.cfg.Rand)
	if err != nil {
		return nil, err
	}
	return &signer{id: id, tag: dnTagICACID, key: key, keyRef: keyRef, keyID: keyID, cert: cert, public: pub}, nil
}

// RootCert is the RCAC as Matter TLV — matter.js rootCert (:194-196).
func (c *CA) RootCert() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.rootCert...)
}

// RootPublicKey is the 65-byte uncompressed root public key.
func (c *CA) RootPublicKey() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.rootPub...)
}

// ICACCert is the ICAC as Matter TLV, nil without one — matter.js icacCert
// (:198-203).
func (c *CA) ICACCert() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.icac == nil {
		return nil
	}
	return append([]byte(nil), c.icac.cert...)
}

// Verifier is the verifier of NOC chains over the CA's root, with the
// CA's clock.
func (c *CA) Verifier() *mattercert.Verifier { return c.verifier }

// nowSource adapts the resolved issuing clock to the verifier's time
// source.
type nowSource struct{ now func() time.Time }

func (k nowSource) Now() time.Time { return k.now() }

// GenerateNOC issues a NOC for the subject key pub (65-byte uncompressed
// P-256), on fabricID for nodeID with the CASE Authenticated Tags cats.
// Mirrors matter.js CertificateAuthority.ts:287-320 (generateNoc) at
// 0ceb202d: the next certificate id; issuer and signer the ICAC when there
// is one, else the root (#getSigningParameters :411-429); subject
// {fabricId, nodeId, caseAuthenticatedTags} in that order (the DN list
// keeps the caller's order, kinds/definitions/operational.ts:109-110);
// validity −1 / +10 years; not a CA; keyUsage digitalSignature;
// extendedKeyUsage [clientAuth, serverAuth]; SHA-1 of pub as the subject
// key identifier and the signer's as the authority key identifier.
//
// Unlike matter.js, which writes the counter only when it creates the
// root (:181-183), the CA saves after each issue so serial numbers do not
// repeat after a restart; a save error fails the issue.
func (c *CA) GenerateNOC(pub []byte, fabricID, nodeID uint64, cats []uint32) ([]byte, error) {
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return nil, fmt.Errorf("ca: NOC subject key: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	s := c.signingParameters()
	if s.key == nil {
		return nil, errors.New("ca: no signing key: the root key is offline and there is no ICAC")
	}
	nb, na, err := c.validity()
	if err != nil {
		return nil, err
	}
	subject := []dnAttr{{dnTagFabricID, fabricID}, {dnTagNodeID, nodeID}}
	for _, cat := range cats {
		subject = append(subject, dnAttr{dnTagCAT, uint64(cat)})
	}
	noc, err := signCert(&certFields{
		serial:    id,
		issuer:    []dnAttr{{s.tag, s.id}},
		notBefore: nb,
		notAfter:  na,
		subject:   subject,
		pub:       pub,
		subjectID: KeyIdentifier(pub),
		authority: s.keyID,
	}, s.key, c.cfg.Rand)
	if err != nil {
		return nil, err
	}
	if c.cfg.Store != nil {
		if err := c.cfg.Store.Save(c.stored()); err != nil {
			return nil, fmt.Errorf("ca: save the certificate counter: %w", err)
		}
	}
	return noc, nil
}

// signingParameters is #getSigningParameters (:411-429).
func (c *CA) signingParameters() signer {
	if c.icac != nil {
		return *c.icac
	}
	return signer{id: c.rootCertID, tag: dnTagRCACID, key: c.rootKey, keyID: c.rootKeyID}
}

// KeyIdentifier is the SHA-1 key identifier of a public key — matter.js
// CertificateAuthority.ts:170, :254, :314 (computeHash(publicKey, "SHA-1")).
func KeyIdentifier(pub []byte) []byte {
	sum := sha1.Sum(pub) //nolint:gosec // see the import
	return sum[:]
}

// CompressedFabricID is the compressed fabric id of fabricID under the
// root public key rootPub (65-byte uncompressed): HKDF-SHA256 with the
// key without its 0x04 prefix as input, the fabric id big-endian as salt,
// "CompressedFabric" as info, 8 bytes. Mirrors matter.js
// packages/types/src/datatype/GlobalFabricId.ts:11, :36-47
// (GlobalFabricId.compute; DataWriter is big-endian by default,
// general/src/util/DataWriter.ts:12-18) at 0ceb202d.
func CompressedFabricID(rootPub []byte, fabricID uint64) ([8]byte, error) {
	var id [8]byte
	if len(rootPub) != 65 || rootPub[0] != 0x04 {
		return id, errors.New("ca: root public key is not a 65-byte uncompressed point")
	}
	var salt [8]byte
	binary.BigEndian.PutUint64(salt[:], fabricID)
	out, err := hkdf.Key(sha256.New, rootPub[1:], salt[:], "CompressedFabric", 8)
	if err != nil {
		return id, err
	}
	copy(id[:], out)
	return id, nil
}

// publicKeyBytes is the 65-byte uncompressed encoding of pub.
func publicKeyBytes(pub *ecdsa.PublicKey) []byte {
	b, err := pub.Bytes()
	if err != nil {
		// A key this package generated or parsed is always on P-256.
		panic("ca: " + err.Error())
	}
	return b
}

// privateKeyBytes is the 32-byte private scalar of k.
func privateKeyBytes(k *ecdsa.PrivateKey) []byte {
	b, err := k.Bytes()
	if err != nil {
		panic("ca: " + err.Error())
	}
	return b
}

// privateKey parses a 32-byte P-256 private scalar.
func privateKey(b []byte) (*ecdsa.PrivateKey, error) {
	return ecdsa.ParseRawPrivateKey(elliptic.P256(), b)
}
