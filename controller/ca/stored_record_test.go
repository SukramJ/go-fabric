// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"thingfabric.dev/go-fabric/controller/ca"
)

// TestNewNeverReplacesAStoredRecord: a CA record the store holds but that
// is incomplete or inconsistent — a field lost, a certificate counter that
// would repeat a serial, a stored key that is not its certificate's — is
// ErrConfig, and the record stays as it was: the constructor never writes
// a new root over a record that still holds a key. Before, an incomplete
// root was treated as nothing stored and replaced, and the other cases
// loaded and issued certificates that verified nowhere or repeated a
// serial (review II, R2-P6-store-1851, R2-P6-store-1852,
// R2-P6-store-1853 (a)(b), owner decision 191; ahead of matter.js, whose
// constructor replaces an invalid stored root).
func TestNewNeverReplacesAStoredRecord(t *testing.T) {
	t.Parallel()
	otherKey := func(t *testing.T) []byte {
		t.Helper()
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		b, err := k.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, c := range []struct {
		name   string
		icac   bool
		damage func(t *testing.T, s *ca.Stored)
	}{
		{"the root key identifier lost", false, func(_ *testing.T, s *ca.Stored) { s.RootKeyIdentifier = nil }},
		{"the root certificate lost", false, func(_ *testing.T, s *ca.Stored) { s.RootCertBytes = nil }},
		{"the root certificate lost beside an ICAC", true, func(_ *testing.T, s *ca.Stored) { s.RootCertBytes = nil }},
		{"the certificate counter 0", false, func(_ *testing.T, s *ca.Stored) { s.NextCertificateID = 0 }},
		{"the certificate counter at the ICAC's id", true, func(_ *testing.T, s *ca.Stored) { s.NextCertificateID = s.ICAC.CertID }},
		{"a root key that is not the root certificate's", false, func(t *testing.T, s *ca.Stored) { t.Helper(); s.RootKeyPair.PrivateKey = otherKey(t) }},
		{"an ICAC key that is not the ICAC certificate's", true, func(t *testing.T, s *ca.Stored) { t.Helper(); s.ICAC.KeyPair.PrivateKey = otherKey(t) }},
		{"the ICAC key identifier lost", true, func(_ *testing.T, s *ca.Stored) { s.ICAC.KeyIdentifier = nil }},
		{"the ICAC certificate lost", true, func(_ *testing.T, s *ca.Stored) { s.ICAC.CertBytes = nil }},
		{"the ICAC key lost", true, func(_ *testing.T, s *ca.Stored) { s.ICAC.KeyPair.PrivateKey = nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			st := &ca.MemoryStore{}
			if _, err := ca.New(ca.Config{Store: st, IntermediateCA: c.icac}); err != nil {
				t.Fatal(err)
			}
			rec, _, _ := st.Load()
			c.damage(t, &rec)
			if err := st.Save(rec); err != nil {
				t.Fatal(err)
			}
			for _, cfg := range []ca.Config{{Store: st, IntermediateCA: c.icac}, {Store: st, DetectIntermediateCA: true}} {
				if _, err := ca.New(cfg); !errors.Is(err, ca.ErrConfig) {
					t.Fatalf("New (detect %v) = %v, want ErrConfig", cfg.DetectIntermediateCA, err)
				}
			}
			if after, _, _ := st.Load(); !reflect.DeepEqual(after, rec) {
				t.Fatal("the stored record changed")
			}
		})
	}
	t.Run("control: the record as written loads", func(t *testing.T) {
		t.Parallel()
		for _, icac := range []bool{false, true} {
			st := &ca.MemoryStore{}
			if _, err := ca.New(ca.Config{Store: st, IntermediateCA: icac}); err != nil {
				t.Fatal(err)
			}
			if _, err := ca.New(ca.Config{Store: st, DetectIntermediateCA: true}); err != nil {
				t.Fatalf("icac %v: %v", icac, err)
			}
		}
	})
}

// TestSettingStoreKeepsAPartialICAC: a settings record with some but not
// all ICAC fields is not read as a CA without an ICAC — whose next save
// would drop the fields still there — but as an incomplete record,
// ErrConfig (review II, R2-P6-store-1851, second leg).
func TestSettingStoreKeepsAPartialICAC(t *testing.T) {
	t.Parallel()
	settings := &mapSettings{}
	st := ca.SettingStore{Settings: settings}
	if _, err := ca.New(ca.Config{Store: st, IntermediateCA: true}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"icacCertId", "icacKeyPair"} {
		var j map[string]any
		if err := json.Unmarshal([]byte(settings.m[ca.DefaultSettingKey]), &j); err != nil {
			t.Fatal(err)
		}
		delete(j, field)
		b, _ := json.Marshal(j)
		damaged := string(b)
		settings.m[ca.DefaultSettingKey] = damaged
		if _, err := ca.New(ca.Config{Store: st, DetectIntermediateCA: true}); !errors.Is(err, ca.ErrConfig) {
			t.Fatalf("%s lost: New = %v, want ErrConfig", field, err)
		}
		if settings.m[ca.DefaultSettingKey] != damaged {
			t.Fatalf("%s lost: the stored record changed", field)
		}
	}
}

// TestUnparseableStoredRecordIsErrConfig: a stored record that does not
// parse — its JSON, the root key, the root certificate, the ICAC key or
// certificate — is refused as ErrConfig, like an incomplete one, and the
// record stays (review II, decision 191, Fable read F4).
func TestUnparseableStoredRecordIsErrConfig(t *testing.T) {
	t.Parallel()
	t.Run("JSON", func(t *testing.T) {
		t.Parallel()
		settings := &mapSettings{m: map[string]string{ca.DefaultSettingKey: "{not json"}}
		if _, err := ca.New(ca.Config{Store: ca.SettingStore{Settings: settings}}); !errors.Is(err, ca.ErrConfig) {
			t.Fatalf("New = %v, want ErrConfig", err)
		}
		if settings.m[ca.DefaultSettingKey] != "{not json" {
			t.Fatal("the stored record changed")
		}
	})
	for _, c := range []struct {
		name   string
		icac   bool
		damage func(s *ca.Stored)
	}{
		{"the root key", false, func(s *ca.Stored) { s.RootKeyPair.PrivateKey = []byte{1, 2, 3} }},
		{"the root certificate", false, func(s *ca.Stored) { s.RootCertBytes = []byte{0x15, 0x01} }},
		{"the ICAC key", true, func(s *ca.Stored) { s.ICAC.KeyPair.PrivateKey = []byte{1, 2, 3} }},
		{"the ICAC certificate", true, func(s *ca.Stored) { s.ICAC.CertBytes = []byte{0x15, 0x01} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			st := &ca.MemoryStore{}
			if _, err := ca.New(ca.Config{Store: st, IntermediateCA: c.icac}); err != nil {
				t.Fatal(err)
			}
			rec, _, _ := st.Load()
			c.damage(&rec)
			_ = st.Save(rec)
			if _, err := ca.New(ca.Config{Store: st, IntermediateCA: c.icac}); !errors.Is(err, ca.ErrConfig) {
				t.Fatalf("New = %v, want ErrConfig", err)
			}
			if after, _, _ := st.Load(); !reflect.DeepEqual(after, rec) {
				t.Fatal("the stored record changed")
			}
		})
	}
}
