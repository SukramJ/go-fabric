// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/secure/attestation"
)

// writeChainFiles writes a fresh test chain for 0xFFF1/0x8002 as DER files.
func writeChainFiles(t *testing.T) (files AttestationFiles, chain *attestation.Chain, cd []byte) {
	t.Helper()
	chain, err := attestation.BuildTestChain(0xFFF1, 0x8002)
	if err != nil {
		t.Fatal(err)
	}
	cd, err = attestation.BuildTestCertificationDeclaration(0xFFF1, 0x8002)
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
// error, or with BlockCommissioningOnError the error to block with.
func TestAttestationFilesLoad(t *testing.T) {
	files, chain, cd := writeChainFiles(t)
	c := Config{Attestation: Attestation{Files: &files}}
	if blocked, err := c.resolveAttestation(); err != nil || blocked != nil {
		t.Fatal(blocked, err)
	}
	a := c.Attestation
	if a.Chain == nil || !bytes.Equal(a.Chain.DAC, chain.DAC) || !bytes.Equal(a.Chain.PAI, chain.PAI) ||
		!a.Chain.DACKey.Equal(chain.DACKey) || !bytes.Equal(a.CertificationDeclaration, cd) {
		t.Fatal("Attestation.Files did not load the chain and the CD")
	}

	c = Config{Attestation: Attestation{Files: &AttestationFiles{}}}
	if blocked, err := c.resolveAttestation(); err != nil || blocked != nil || c.Attestation.Chain != nil || c.Attestation.CertificationDeclaration != nil {
		t.Fatalf("empty files: %v %v, chain %v — want the test chain", blocked, err, c.Attestation.Chain)
	}

	broken := files
	broken.PAIPath = filepath.Join(t.TempDir(), "missing.der")
	c = Config{Attestation: Attestation{Files: &broken}}
	if blocked, err := c.resolveAttestation(); err == nil || blocked != nil || !strings.Contains(err.Error(), "attestation files") {
		t.Fatalf("missing PAI: %v %v, want an error", blocked, err)
	}
	c = Config{Attestation: Attestation{Files: &broken, BlockCommissioningOnError: true}}
	if blocked, err := c.resolveAttestation(); err != nil || blocked == nil || c.Attestation.Chain != nil {
		t.Fatalf("missing PAI, blocking: %v %v, want the error to block with and the test chain", blocked, err)
	}

	c = Config{Attestation: Attestation{Files: &files, Chain: chain}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "Attestation.Files excludes") {
		t.Fatalf("Files with Chain: %v, want refused", err)
	}
	c = Config{Attestation: Attestation{BlockCommissioningOnError: true}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "needs Files") {
		t.Fatalf("BlockCommissioningOnError without Files: %v, want refused", err)
	}
}

// A node whose chain does not load under BlockCommissioningOnError builds,
// reports the error and refuses its own window.
func TestAttestationBlocksCommissioning(t *testing.T) {
	files, _, _ := writeChainFiles(t)
	files.CDPath = filepath.Join(t.TempDir(), "missing.der")
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	cfg.Attestation = Attestation{Files: &files, BlockCommissioningOnError: true}
	n := newMDNSTestNode(t, cfg)
	if n.AttestationError() == nil {
		t.Fatal("AttestationError is nil for a chain that did not load")
	}
	if err := n.Bridge().CommissioningWindow().OpenOwnWindow(context.Background(), 0); !errors.Is(err, ErrCommissioningBlocked) {
		t.Fatalf("own window: %v, want ErrCommissioningBlocked", err)
	}
}

// Attestation.Files are judged against the product (matter.js
// DeviceCertification.#validateCertification, DeviceCertification.ts:107-140
// at 862ad75f): a chain of another product id fails New, or blocks
// commissioning under BlockCommissioningOnError; a config without a
// product (the zero description) does not judge.
func TestAttestationFilesProduct(t *testing.T) {
	files, _, _ := writeChainFiles(t) // VID 0xFFF1, PID 0x8002
	c := Config{ProductDescription: ProductDescription{VendorID: 0xFFF1, ProductID: 0x8002}, Attestation: Attestation{Files: &files}}
	if blocked, err := c.resolveAttestation(); err != nil || blocked != nil || c.Attestation.Chain == nil {
		t.Fatalf("matching product: %v %v", blocked, err)
	}
	c = Config{ProductDescription: ProductDescription{VendorID: 0xFFF1, ProductID: 0x8003}, Attestation: Attestation{Files: &files}}
	if _, err := c.resolveAttestation(); !errors.Is(err, attestation.ErrProductMismatch) {
		t.Fatalf("other product id: err = %v, want ErrProductMismatch", err)
	}
	c = Config{ProductDescription: ProductDescription{VendorID: 0xFFF1, ProductID: 0x8003}, Attestation: Attestation{Files: &files, BlockCommissioningOnError: true}}
	if blocked, err := c.resolveAttestation(); err != nil || !errors.Is(blocked, attestation.ErrProductMismatch) || c.Attestation.Chain != nil {
		t.Fatalf("other product id, blocking: blocked %v err %v", blocked, err)
	}
}
