// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFixture writes a fresh test chain as files: PEM or DER, the key
// as PKCS#8 or SEC1.
func writeFixture(t *testing.T, asPEM, sec1 bool) (ChainFiles, *Chain, []byte) {
	t.Helper()
	chain, err := BuildTestChain(0xFFF1, 0x8001)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := BuildTestCertificationDeclaration(0xFFF1, 0x8001)
	if err != nil {
		t.Fatal(err)
	}
	var keyDER []byte
	keyType := "PRIVATE KEY"
	if sec1 {
		keyDER, err = x509.MarshalECPrivateKey(chain.DACKey)
		keyType = "EC PRIVATE KEY"
	} else {
		keyDER, err = x509.MarshalPKCS8PrivateKey(chain.DACKey)
	}
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, pemType string, der []byte) string {
		p := filepath.Join(dir, name)
		data := der
		if asPEM {
			data = pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: der})
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	f := ChainFiles{
		DACPath:    write("dac", "CERTIFICATE", chain.DAC),
		DACKeyPath: write("dac-key", keyType, keyDER),
		PAIPath:    write("pai", "CERTIFICATE", chain.PAI),
		CDPath:     write("cd", "CMS", cd),
	}
	return f, chain, cd
}

func TestLoadChainFiles_Formats(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		pem, sec1 bool
	}{
		{"pem-pkcs8", true, false},
		{"pem-sec1", true, true},
		{"der-pkcs8", false, false},
		{"der-sec1", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, want, wantCD := writeFixture(t, tc.pem, tc.sec1)
			got, cd, err := LoadChainFiles(f)
			if err != nil {
				t.Fatalf("LoadChainFiles: %v", err)
			}
			if string(got.DAC) != string(want.DAC) || string(got.PAI) != string(want.PAI) || string(cd) != string(wantCD) {
				t.Error("certificates or CD differ from the files written")
			}
			if !got.DACKey.Equal(want.DACKey) {
				t.Error("the DAC key differs from the one written")
			}
		})
	}
}

func TestLoadChainFiles_Errors(t *testing.T) {
	t.Parallel()
	if _, _, err := LoadChainFiles(ChainFiles{}); !errors.Is(err, ErrNoChainConfigured) {
		t.Errorf("empty paths: %v, want ErrNoChainConfigured", err)
	}
	f, _, _ := writeFixture(t, true, false)

	missing := f
	missing.PAIPath = filepath.Join(t.TempDir(), "absent")
	if _, _, err := LoadChainFiles(missing); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing PAI: %v, want a not-exist error", err)
	}

	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherDER, _ := x509.MarshalPKCS8PrivateKey(other)
	wrongKey := f
	wrongKey.DACKeyPath = filepath.Join(t.TempDir(), "other-key.pem")
	if err := os.WriteFile(wrongKey.DACKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: otherDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadChainFiles(wrongKey); !errors.Is(err, ErrKeyMismatch) {
		t.Errorf("mismatched key: %v, want ErrKeyMismatch", err)
	}

	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p384DER, _ := x509.MarshalECPrivateKey(p384)
	wrongCurve := f
	wrongCurve.DACKeyPath = filepath.Join(t.TempDir(), "p384.der")
	if err := os.WriteFile(wrongCurve.DACKeyPath, p384DER, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadChainFiles(wrongCurve); err == nil {
		t.Error("a P-384 key was accepted")
	}

	garbage := f
	garbage.DACPath = filepath.Join(t.TempDir(), "garbage")
	if err := os.WriteFile(garbage.DACPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadChainFiles(garbage); err == nil {
		t.Error("a non-PEM, non-DER DAC was accepted")
	}
}
