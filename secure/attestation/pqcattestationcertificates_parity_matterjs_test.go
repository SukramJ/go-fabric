// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// A port of matter.js packages/protocol/test/certificate/PqcAttestationCertificatesTest.ts at
// 862ad75f: the describe "device certification" — DeviceCertification's
// servability check is LoadChainFiles here. The parsing cases are in
// controller/attestation/pqcattestationcertificates_parity_matterjs_test.go.

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// pqcChain is buildChain({ paa: "ML-DSA-65", pai: "ML-DSA-44" })
// (PqcAttestationCertificatesTest.ts:899-961): an ML-DSA-65 PAA, an
// ML-DSA-44 PAI it signs and a P-256 DAC the PAI signs, valid from a year
// before now for ten years.
func pqcChain(t *testing.T) (dac, pai []byte, dacKey *ecdsa.PrivateKey) {
	t.Helper()
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	paaKey, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatalf("PAA key: %v", err)
	}
	paiKey, err := mldsa.GenerateKey(mldsa.MLDSA44())
	if err != nil {
		t.Fatalf("PAI key: %v", err)
	}
	if dacKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		t.Fatalf("DAC key: %v", err)
	}
	tmpl := func(serial int64, cn string, ca bool, pathLen int) *x509.Certificate {
		c := &x509.Certificate{
			SerialNumber:          big.NewInt(serial),
			Subject:               pkix.Name{CommonName: cn},
			NotBefore:             now.AddDate(-1, 0, 0),
			NotAfter:              now.AddDate(10, 0, 0),
			BasicConstraintsValid: true,
			IsCA:                  ca,
			KeyUsage:              x509.KeyUsageDigitalSignature,
			SubjectKeyId:          []byte{byte(serial), 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19},
		}
		if ca {
			c.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
			c.MaxPathLen, c.MaxPathLenZero = pathLen, pathLen == 0
		}
		return c
	}
	create := func(c, parent *x509.Certificate, pub crypto.PublicKey, signer crypto.Signer) ([]byte, *x509.Certificate) {
		der, err := x509.CreateCertificate(rand.Reader, c, parent, pub, signer)
		if err != nil {
			t.Fatalf("create %s: %v", c.Subject.CommonName, err)
		}
		parsed, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatalf("parse %s: %v", c.Subject.CommonName, err)
		}
		return der, parsed
	}
	paaTmpl := tmpl(1, "Matter PQC Test PAA", true, 1)
	_, paa := create(paaTmpl, paaTmpl, paaKey.Public(), paaKey)
	pai, paiCert := create(tmpl(2, "Matter PQC Test PAI", true, 0), paa, paiKey.Public(), paaKey)
	dac, _ = create(tmpl(3, "Matter PQC Test DAC", false, 0), paiCert, &dacKey.PublicKey, paiKey)
	return dac, pai, dacKey
}

// TestPqcAttestationCertificatesDeviceCertification is the describe "device
// certification" (PqcAttestationCertificatesTest.ts:491). LoadChainFiles
// takes no product description, so it is the case without one.
func TestPqcAttestationCertificatesDeviceCertification(t *testing.T) {
	t.Run("refuses an unservable chain even without product information", func(t *testing.T) {
		dac, pai, dacKey := pqcChain(t)
		key, err := x509.MarshalPKCS8PrivateKey(dacKey)
		if err != nil {
			t.Fatalf("DAC key: %v", err)
		}
		cd, err := BuildTestCertificationDeclaration(0xfff1, 0x8000)
		if err != nil {
			t.Fatalf("CD: %v", err)
		}
		dir := t.TempDir()
		files := ChainFiles{
			DACPath:    filepath.Join(dir, "dac.der"),
			DACKeyPath: filepath.Join(dir, "dac.key"),
			PAIPath:    filepath.Join(dir, "pai.der"),
			CDPath:     filepath.Join(dir, "cd.der"),
		}
		for path, data := range map[string][]byte{files.DACPath: dac, files.DACKeyPath: key, files.PAIPath: pai, files.CDPath: cd} {
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		_, _, err = LoadChainFiles(files)
		if !errors.Is(err, ErrCertificateTooLarge) || !regexp.MustCompile(`DAC of \d+ bytes exceeds the 600 bytes`).MatchString(err.Error()) {
			t.Fatalf("LoadChainFiles = %v, want the DAC refused as unservable", err)
		}
	})
}
