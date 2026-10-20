// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/attestation"
)

// TestAttestationConfig: an empty flag keeps the test chain, a directory
// with the four files builds a node on the vendor chain, a directory
// missing one fails node.New (no silent fall-back to the test vendor).
func TestAttestationConfig(t *testing.T) {
	t.Parallel()
	if att := attestationConfig(""); att.Files != nil || att.Chain != nil || att.CertificationDeclaration != nil {
		t.Fatalf("empty flag: %+v", att)
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

	build := func(name string) error {
		cfg := nodeConfig(flags{
			dbPath: filepath.Join(t.TempDir(), name), listen: "127.0.0.1:0", label: "attestation-test",
			salt: "go-fabric-ref-salt", iterations: 1000,
		},
			20202021, 3840, node.TCPConfig{}, &otaApplier{dir: t.TempDir()}, nil, slog.New(slog.DiscardHandler))
		cfg.OTA, cfg.AggregatorServers = nil, nil
		cfg.Attestation = attestationConfig(dir)
		n, err := node.New(cfg)
		if err == nil {
			_ = n.Stop(context.Background())
		}
		return err
	}
	if err := build("good.db"); err != nil {
		t.Fatalf("the vendor chain: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "pai.pem")); err != nil {
		t.Fatal(err)
	}
	if err := build("broken.db"); err == nil {
		t.Fatal("a directory missing the PAI built a node")
	}
}
