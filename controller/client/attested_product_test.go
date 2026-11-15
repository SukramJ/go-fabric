// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// TestRecordKeepsTheAttestedProduct: a commission whose attestation
// validates keeps the BasicInformation vendor and product id the
// Certification Declaration was checked against (review II decision 105);
// a decommission clears them; a commission whose attestation failed and
// was accepted by policy keeps none.
func TestRecordKeepsTheAttestedProduct(t *testing.T) {
	t.Run("attested, then decommissioned", func(t *testing.T) {
		cp := newClientPair(t, clientPairOptions{
			rootOnly:   true,
			controller: fabrictest.ControllerOptions{Attestation: attestationStore(true, secattestation.TestPAANoVIDCert)},
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
				Attestation: attestationStore(true, secattestation.TestPAANoVIDCert),
				Registry:    func(rc *client.RegistryConfig) { rc.Store = store },
			},
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
