// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/secure/attestation"
)

// TestLoadAttestation: an empty flag keeps the test chain, a directory
// with the four files serves the vendor chain, a directory missing one
// is an error (no silent fall-back to the test vendor).
func TestLoadAttestation(t *testing.T) {
	t.Parallel()
	if att, err := loadAttestation(""); err != nil || att.Chain != nil || att.CertificationDeclaration != nil {
		t.Fatalf("empty flag: %+v, %v", att, err)
	}
	chain, err := attestation.BuildTestChain(0xFFF1, 0x8002)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := attestation.BuildTestCertificationDeclaration(0xFFF1, 0x8002)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(chain.DACKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("dac.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: chain.DAC}))
	write("dac-key.pem", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	write("pai.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: chain.PAI}))
	write("cd.der", cd)

	att, err := loadAttestation(dir)
	if err != nil {
		t.Fatalf("loadAttestation: %v", err)
	}
	if att.Chain == nil || !bytes.Equal(att.Chain.DAC, chain.DAC) || !bytes.Equal(att.CertificationDeclaration, cd) {
		t.Fatal("the loaded chain is not the one written")
	}
	if err := os.Remove(filepath.Join(dir, "pai.pem")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAttestation(dir); err == nil {
		t.Fatal("a directory missing the PAI loaded")
	}
}
