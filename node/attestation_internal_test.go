// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/secure/attestation"
)

// writeChainFiles writes a fresh test chain for 0xFFF1/0x8002 as DER files.
func writeChainFiles(t *testing.T) (AttestationFiles, *attestation.Chain, []byte) {
	t.Helper()
	chain, err := attestation.BuildTestChain(0xFFF1, 0x8002)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := attestation.BuildTestCertificationDeclaration(0xFFF1, 0x8002)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(chain.DACKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return AttestationFiles{
		DACPath: write("dac.der", chain.DAC), DACKeyPath: write("dac-key.der", key),
		PAIPath: write("pai.der", chain.PAI), CDPath: write("cd.der", cd),
	}, chain, cd
}

// Attestation.Files loads the chain into Chain and the CD; files that
// name nothing keep the test chain; a chain that does not load is an
// error.
func TestAttestationFilesLoad(t *testing.T) {
	files, chain, cd := writeChainFiles(t)
	c := Config{Attestation: Attestation{Files: &files}}
	if err := c.loadAttestationFiles(); err != nil {
		t.Fatal(err)
	}
	a := c.Attestation
	if a.Chain == nil || !bytes.Equal(a.Chain.DAC, chain.DAC) || !bytes.Equal(a.Chain.PAI, chain.PAI) ||
		!a.Chain.DACKey.Equal(chain.DACKey) || !bytes.Equal(a.CertificationDeclaration, cd) || a.Files != &files {
		t.Fatal("Attestation.Files did not load the chain and the CD")
	}

	c = Config{Attestation: Attestation{Files: &AttestationFiles{}}}
	if err := c.loadAttestationFiles(); err != nil || c.Attestation.Chain != nil || c.Attestation.CertificationDeclaration != nil {
		t.Fatalf("empty files: %v, chain %v — want the test chain", err, c.Attestation.Chain)
	}
	if _, err := LoadAttestation(AttestationFiles{}); !errors.Is(err, ErrNoChainConfigured) || !errors.Is(err, attestation.ErrNoChainConfigured) {
		t.Fatalf("LoadAttestation of nothing = %v, want ErrNoChainConfigured", err)
	}

	broken := files
	broken.PAIPath = filepath.Join(t.TempDir(), "missing.der")
	c = Config{Attestation: Attestation{Files: &broken}}
	if err := c.loadAttestationFiles(); err == nil || !strings.Contains(err.Error(), "attestation files") {
		t.Fatalf("missing PAI: %v, want an error", err)
	}

	c = Config{Attestation: Attestation{Files: &files, Chain: chain}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "Attestation.Files excludes") {
		t.Fatalf("Files with Chain: %v, want refused", err)
	}
}
