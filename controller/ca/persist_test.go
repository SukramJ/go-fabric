// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
)

// mapSettings is a ca.Settings in memory.
type mapSettings struct {
	m   map[string]string
	err error
}

func (s *mapSettings) GetSetting(_ context.Context, key string) (value string, ok bool, err error) {
	if s.err != nil {
		return "", false, s.err
	}
	value, ok = s.m[key]
	return value, ok, nil
}

func (s *mapSettings) SetSetting(_ context.Context, key, value string) error {
	if s.err != nil {
		return s.err
	}
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[key] = value
	return nil
}

// TestSettingStoreOpenCloseOpen: a CA created over a SettingStore and
// opened again from the same settings is the same root — certificate,
// public key, key identifier — and continues its certificate counter;
// with an ICAC the intermediate survives too.
func TestSettingStoreOpenCloseOpen(t *testing.T) {
	t.Parallel()
	for _, icac := range []bool{false, true} {
		settings := &mapSettings{}
		st := ca.SettingStore{Settings: settings}
		first, err := ca.New(ca.Config{Store: st, IntermediateCA: icac})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := first.GenerateNOC(first.RootPublicKey(), 1, 1, nil); err != nil {
			t.Fatal(err)
		}
		if _, ok := settings.m[ca.DefaultSettingKey]; !ok {
			t.Fatalf("nothing under %q: %v", ca.DefaultSettingKey, settings.m)
		}
		second, err := ca.New(ca.Config{Store: st, IntermediateCA: icac})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.RootCert(), second.RootCert()) || !bytes.Equal(first.RootPublicKey(), second.RootPublicKey()) {
			t.Fatalf("icac=%v: the root changed across open-close-open", icac)
		}
		if !bytes.Equal(first.ICACCert(), second.ICACCert()) {
			t.Fatalf("icac=%v: the ICAC changed", icac)
		}
		a, _, _ := st.Load()
		if a.NextCertificateID < 2 {
			t.Fatalf("icac=%v: counter %d not persisted", icac, a.NextCertificateID)
		}
		// A different IntermediateCA setting refuses the stored CA.
		if _, err := ca.New(ca.Config{Store: st, IntermediateCA: !icac}); !errors.Is(err, ca.ErrConfig) {
			t.Fatalf("icac=%v: mismatched config: %v", icac, err)
		}
	}
}

func TestSettingStoreFabricRecord(t *testing.T) {
	t.Parallel()
	st := ca.SettingStore{Settings: &mapSettings{}, Key: "ctl"}
	if st.FabricKey() != "ctl.fabric" {
		t.Fatalf("FabricKey %q", st.FabricKey())
	}
	if _, ok, err := st.LoadFabric(); ok || err != nil {
		t.Fatalf("empty store: %v %v", ok, err)
	}
	rec := ca.FabricRecord{
		FabricID: 0xF0B, RootNodeID: 112233, RootVendorID: 0xFFF1, IPK: bytes.Repeat([]byte{7}, 16),
		FabricIndex: 1, Label: "lab", KeyPair: &ca.KeyPair{PublicKey: []byte{4, 1}, PrivateKey: []byte{9}},
		NOC: []byte{1, 2, 3}, RootPublicKey: []byte{4, 2},
	}
	if err := st.SaveFabric(rec); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.LoadFabric()
	if err != nil || !ok {
		t.Fatalf("load: %v %v", ok, err)
	}
	if got.FabricID != rec.FabricID || got.RootNodeID != rec.RootNodeID || got.Label != rec.Label ||
		!bytes.Equal(got.IPK, rec.IPK) || !bytes.Equal(got.KeyPair.PrivateKey, rec.KeyPair.PrivateKey) || !bytes.Equal(got.NOC, rec.NOC) {
		t.Fatalf("record %+v, want %+v", got, rec)
	}
	var m ca.MemoryStore
	if _, ok, _ := m.LoadFabric(); ok {
		t.Fatal("empty memory store has a record")
	}
	_ = m.SaveFabric(rec)
	if got, ok, _ := m.LoadFabric(); !ok || got.FabricID != rec.FabricID || !bytes.Equal(got.KeyPair.PublicKey, rec.KeyPair.PublicKey) {
		t.Fatalf("memory record %+v", got)
	}
}

func TestSettingStoreErrors(t *testing.T) {
	t.Parallel()
	if _, _, err := (ca.SettingStore{}).Load(); err == nil {
		t.Fatal("Load without Settings")
	}
	if err := (ca.SettingStore{}).SaveFabric(ca.FabricRecord{}); err == nil {
		t.Fatal("SaveFabric without Settings")
	}
	bad := &mapSettings{m: map[string]string{ca.DefaultSettingKey: "{", ca.DefaultSettingKey + ".fabric": "[]"}}
	if _, _, err := (ca.SettingStore{Settings: bad}).Load(); err == nil {
		t.Fatal("Load of bad JSON")
	}
	if _, _, err := (ca.SettingStore{Settings: bad}).LoadFabric(); err == nil {
		t.Fatal("LoadFabric of bad JSON")
	}
	failing := &mapSettings{err: errors.New("disk")}
	if _, err := ca.New(ca.Config{Store: ca.SettingStore{Settings: failing}}); err == nil {
		t.Fatal("New over a failing store")
	}
}
