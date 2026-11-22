// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // the SHA-1 key identifier of a test CD signer
	"crypto/sha256"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/node"
	secattestation "thingfabric.dev/go-fabric/secure/attestation"
	"thingfabric.dev/go-fabric/tlv"
)

// TestRecordKeepsTheAttestedProduct: a commission whose attestation
// validates keeps the BasicInformation vendor and product id the
// Certification Declaration was checked against (review II decision 105);
// a decommission clears them; a commission whose attestation failed, ran
// without a trust store, had an error finding the policy accepted, ended
// in a test root or carried a declaration of the Matter test CD signer
// keeps none (decisions 119, 192, 214). The attested cases use a root the
// store holds as production and a CD signed by a store signer.
func TestRecordKeepsTheAttestedProduct(t *testing.T) {
	t.Run("attested, then decommissioned", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{Attestation: productionStore()},
			configure:  productionCD(t),
		})
		rec := cp.node.Record()
		// fabrictest's device is VendorID 0xFFF1, ProductID 0x8001
		// (fabrictest/device.go's ProductDescription).
		if rec.AttestedVendorID == nil || rec.AttestedProductID == nil || *rec.AttestedVendorID != 0xFFF1 || *rec.AttestedProductID != 0x8001 {
			t.Fatalf("attested %v/%v, want 0xFFF1/0x8001", rec.AttestedVendorID, rec.AttestedProductID)
		}
		if err := cp.decommission(t); err != nil {
			t.Fatal(err)
		}
		if rec := cp.node.Record(); rec.AttestedVendorID != nil || rec.AttestedProductID != nil {
			t.Fatalf("after decommission attested %v/%v, want none", rec.AttestedVendorID, rec.AttestedProductID)
		}
	})
	t.Run("a decommission stores the cleared pair before it erases", func(t *testing.T) {
		// The erase that follows a decommission fails, so the record the
		// decommission saved stays in the store — the one a restarted
		// registry would load.
		store := &eraseRefusingStore{}
		cp := newClientPair(t, clientPairOptions{
			rootOnly: true,
			controller: fabrictest.ControllerOptions{
				Attestation: productionStore(),
				Registry:    func(rc *client.RegistryConfig) { rc.Store = store },
			},
			configure: productionCD(t),
		})
		if v, ok := storedAttested(t, store, cp.node.ID()); !ok {
			t.Fatalf("no attested pair stored after the commission (%x)", v)
		}
		store.refuse.Store(true)
		if err := cp.decommission(t); err == nil {
			t.Fatal("the decommission succeeded although the erase was refused")
		}
		if v, ok := storedAttested(t, store, cp.node.ID()); ok {
			t.Fatalf("the stored record still carries the attested pair %x", v)
		}
	})
	// The test root and the test CD signer raise no finding under a store
	// that accepts test certificates, and their keys are public (review
	// II, R2-P6-sec-1803).
	t.Run("a test root is not attested", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{Attestation: storeSignerStore(false)},
			configure:  productionCD(t),
		})
		if rec := cp.node.Record(); rec.AttestedVendorID != nil || rec.AttestedProductID != nil {
			t.Fatalf("attested %v/%v under a test root, want none", rec.AttestedVendorID, rec.AttestedProductID)
		}
	})
	t.Run("a test-signed declaration is not attested", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{Attestation: productionStore()},
		})
		if rec := cp.node.Record(); rec.AttestedVendorID != nil || rec.AttestedProductID != nil {
			t.Fatalf("attested %v/%v with the test CD signer, want none", rec.AttestedVendorID, rec.AttestedProductID)
		}
	})
	t.Run("without a trust store the pair is not attested", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{NoAttestationStore: true},
			commission: client.CommissionOptions{OnAttestationFailure: attestation.Accept()},
		})
		if rec := cp.node.Record(); rec.AttestedVendorID != nil || rec.AttestedProductID != nil {
			t.Fatalf("attested %v/%v without a trust store, want none", rec.AttestedVendorID, rec.AttestedProductID)
		}
	})
	t.Run("an accepted error finding is not attested", func(t *testing.T) {
		// A test PAA under a store that accepts no test certificates:
		// Validate returns no error and a LevelError
		// TrustedAsTestCertificate finding, which the policy accepts.
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{Attestation: attestationStore(false, secattestation.TestPAANoVIDCert)},
			commission: client.CommissionOptions{OnAttestationFailure: attestation.Accept()},
		})
		if rec := cp.node.Record(); rec.AttestedVendorID != nil || rec.AttestedProductID != nil {
			t.Fatalf("attested %v/%v after an accepted error finding, want none", rec.AttestedVendorID, rec.AttestedProductID)
		}
	})
	t.Run("a failure the policy accepted is not attested", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{Attestation: attestationStore(true, secattestation.TestPAAFFF1Cert)},
			commission: client.CommissionOptions{OnAttestationFailure: attestation.Accept()},
		})
		if rec := cp.node.Record(); rec.AttestedVendorID != nil || rec.AttestedProductID != nil {
			t.Fatalf("attested %v/%v after an accepted failure, want none", rec.AttestedVendorID, rec.AttestedProductID)
		}
	})
}

// eraseRefusingStore is a MemoryStore whose EraseNode fails once armed.
type eraseRefusingStore struct {
	client.MemoryStore
	refuse atomic.Bool
}

func (s *eraseRefusingStore) EraseNode(id string) error {
	if s.refuse.Load() {
		return errors.New("erase refused")
	}
	return s.MemoryStore.EraseNode(id)
}

// storedAttested is the stored record's attested-pair value: the record is
// cluster 0xFFFF_FFFE of endpoint 0, the pair its attribute 24
// (controller/client/record.go, recordCluster and recAttestedProduct).
func storedAttested(t *testing.T, s client.Store, id string) ([]byte, bool) {
	t.Helper()
	eps, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range eps {
		if ep.Number != 0 {
			continue
		}
		for _, c := range ep.Clusters {
			if c.ID == 0xFFFF_FFFE {
				v, ok := c.Values[24]
				return v, ok && v != nil
			}
		}
	}
	return nil, false
}

// cdStoreKey stands for a CSA-issued CD signer a DCL answer vouches for:
// neither built in nor the Matter test CD signer.
var cdStoreKey, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

func cdStoreSKID() []byte {
	b, _ := cdStoreKey.PublicKey.Bytes()
	sum := sha1.Sum(b) //nolint:gosec // a key identifier, not a security hash
	return sum[:]
}

// storeSignerStore trusts the device's test PAA — held as production when
// production is set — and knows cdStoreKey as a CD signer.
func storeSignerStore(production bool) *attestation.StaticStore {
	return &attestation.StaticStore{
		PAAs:       []attestation.PAA{{DER: secattestation.TestPAANoVIDCert, Production: production}},
		CDSigners:  map[string]*ecdsa.PublicKey{strings.ToUpper(hex.EncodeToString(cdStoreSKID())): &cdStoreKey.PublicKey},
		AcceptTest: true,
	}
}

// productionStore is storeSignerStore with the root held as production.
func productionStore() *attestation.StaticStore { return storeSignerStore(true) }

// productionCD gives the device a Certification Declaration of type
// official for fabrictest's VendorID 0xFFF1, ProductID 0x8001, signed by
// cdStoreKey.
func productionCD(t *testing.T) func(*node.Config) {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutUint(tlv.ContextTag(0), 1)
	e.PutUint(tlv.ContextTag(1), 0xFFF1)
	e.StartArray(tlv.ContextTag(2))
	e.PutUint(tlv.AnonymousTag(), 0x8001)
	_ = e.EndContainer()
	e.PutUint(tlv.ContextTag(3), 22)
	e.PutUTF8(tlv.ContextTag(4), "CSA00000SWC00000-00")
	e.PutUint(tlv.ContextTag(5), 0)
	e.PutUint(tlv.ContextTag(6), 0)
	e.PutUint(tlv.ContextTag(7), 1)
	e.PutUint(tlv.ContextTag(8), 2)
	_ = e.EndContainer()
	content, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	sig, err := ecdsa.SignASN1(rand.Reader, cdStoreKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	type algo struct {
		Algorithm  asn1.ObjectIdentifier
		Parameters asn1.RawValue `asn1:"optional"`
	}
	type signerInfo struct {
		Version            int
		SID                asn1.RawValue
		DigestAlgorithm    algo
		SignatureAlgorithm algo
		Signature          []byte
	}
	type encap struct {
		EContentType asn1.ObjectIdentifier
		EContent     asn1.RawValue `asn1:"explicit,tag:0,optional"`
	}
	type signedData struct {
		Version          int
		DigestAlgorithms []algo `asn1:"set"`
		EncapContentInfo encap
		SignerInfos      []signerInfo `asn1:"set"`
	}
	type contentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	sha := asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	ecdsaSHA := asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	octets, _ := asn1.Marshal(content)
	sd, err := asn1.Marshal(signedData{
		Version:          3,
		DigestAlgorithms: []algo{{Algorithm: sha}},
		EncapContentInfo: encap{EContentType: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}, EContent: asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: octets}},
		SignerInfos: []signerInfo{{
			Version: 3, SID: asn1.RawValue{Class: 2, Tag: 0, Bytes: cdStoreSKID()},
			DigestAlgorithm: algo{Algorithm: sha}, SignatureAlgorithm: algo{Algorithm: ecdsaSHA}, Signature: sig,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cd, err := asn1.Marshal(contentInfo{ContentType: asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}, Content: asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: sd}})
	if err != nil {
		t.Fatal(err)
	}
	return func(cfg *node.Config) { cfg.Attestation.CertificationDeclaration = cd }
}
