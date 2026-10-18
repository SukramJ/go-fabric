// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// ChainFiles names the files of a vendor attestation chain on disk: the
// Device Attestation Certificate, its private key, the Product
// Attestation Intermediate and the Certification Declaration. Every
// file may be PEM or DER; the key may be PKCS#8 or SEC1 (an EC private
// key block). Each path is required.
//
// matter.js takes attestation material as bytes (its DeviceCertification
// is built from the certificates and the key, not from files), and
// chip's example apps read a provider file of their own format; a Go
// host usually has the four files a vendor's PKI handed over, so the
// loader is this module's convenience rather than a port.
type ChainFiles struct {
	DACPath    string
	DACKeyPath string
	PAIPath    string
	CDPath     string
}

// ErrNoChainConfigured is returned by [LoadChainFiles] when every path
// is empty: a host that offers the chain as an optional setting can
// fall back to the CSA test chain on this error without matching
// strings.
var ErrNoChainConfigured = errors.New("attestation: no vendor chain configured")

// ErrKeyMismatch is returned by [LoadChainFiles] when the private key
// does not belong to the DAC's public key.
var ErrKeyMismatch = errors.New("attestation: the DAC key does not match the DAC's public key")

// ErrCertificateTooLarge is returned by [LoadChainFiles] when the DAC or
// the PAI exceeds the 600 bytes a device can serve.
var ErrCertificateTooLarge = errors.New("attestation: certificate too large to serve")

// maxServableCertificateSize is the largest DAC or PAI a node serves in a
// CertificateChainResponse without the segmented responses of PQC Phase 1
// — matter.js MAX_DER_CERTIFICATE_SIZE (certificate/kinds/common.ts:13,
// Matter §6.1.3).
const maxServableCertificateSize = 600

// checkServable refuses a DAC or PAI over 600 bytes, which a node cannot serve.
func checkServable(name string, der []byte) error {
	// Mirrors matter.js packages/protocol/src/certificate/DeviceCertification.ts:87-105 (#assertServable).
	if len(der) > maxServableCertificateSize {
		return fmt.Errorf("%w: %s of %d bytes exceeds the %d bytes a device can serve without segmented certificate chain responses",
			ErrCertificateTooLarge, name, len(der), maxServableCertificateSize)
	}
	return nil
}

// LoadChainFiles reads a vendor attestation chain from f and returns the
// [Chain] a node serves (DAC key, DAC, PAI in DER) and the Certification
// Declaration bytes (the CMS structure, DER). Every file accepts PEM
// (the first block of the expected kind) or DER. It fails on a missing
// or unreadable file, a certificate that does not parse, a key that is
// not a P-256 EC key, a key that does not match the DAC
// ([ErrKeyMismatch]) and a DAC or PAI larger than a device can serve
// ([ErrCertificateTooLarge]). With every path empty it returns
// [ErrNoChainConfigured].
//
// The chain itself is not validated against a PAA here: whether the
// PAI is rooted at a PAA a commissioner trusts is the commissioner's
// judgement (matter.js DeviceAttestationValidator.ts), and a vendor's
// development chain may deliberately use a test PAA.
func LoadChainFiles(f ChainFiles) (chain *Chain, cd []byte, err error) {
	if f.DACPath == "" && f.DACKeyPath == "" && f.PAIPath == "" && f.CDPath == "" {
		return nil, nil, ErrNoChainConfigured
	}
	dac, err := readDER(f.DACPath, "CERTIFICATE", "DAC")
	if err != nil {
		return nil, nil, err
	}
	dacCert, err := x509.ParseCertificate(dac)
	if err != nil {
		return nil, nil, fmt.Errorf("attestation: DAC %s: %w", f.DACPath, err)
	}
	pai, err := readDER(f.PAIPath, "CERTIFICATE", "PAI")
	if err != nil {
		return nil, nil, err
	}
	if err := checkServable("DAC", dac); err != nil {
		return nil, nil, err
	}
	if err := checkServable("PAI", pai); err != nil {
		return nil, nil, err
	}
	if _, err := x509.ParseCertificate(pai); err != nil {
		return nil, nil, fmt.Errorf("attestation: PAI %s: %w", f.PAIPath, err)
	}
	keyDER, err := readDER(f.DACKeyPath, "PRIVATE KEY", "DAC key")
	if err != nil {
		return nil, nil, err
	}
	key, err := parseECPrivateKey(keyDER)
	if err != nil {
		return nil, nil, fmt.Errorf("attestation: DAC key %s: %w", f.DACKeyPath, err)
	}
	pub, ok := dacCert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&key.PublicKey) {
		return nil, nil, fmt.Errorf("%w (%s, %s)", ErrKeyMismatch, f.DACKeyPath, f.DACPath)
	}
	cdBytes, err := readDER(f.CDPath, "CMS", "CD")
	if err != nil {
		return nil, nil, err
	}
	return &Chain{DACKey: key, DAC: dac, PAI: pai}, cdBytes, nil
}

// readDER reads path and returns DER bytes: a PEM file yields its first
// block whose type is kind, or — for a key — any "* PRIVATE KEY" block,
// or for a CD any block (vendors label the CMS "CMS", "PKCS7" or
// "CERTIFICATION DECLARATION"); a file without a PEM block is DER.
func readDER(path, kind, what string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("attestation: %s: no path", what)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // the host names its own attestation files
	if err != nil {
		return nil, fmt.Errorf("attestation: %s: %w", what, err)
	}
	rest := raw
	for {
		block, more := pem.Decode(rest)
		if block == nil {
			break
		}
		switch {
		case block.Type == kind,
			kind == "PRIVATE KEY" && (block.Type == "EC PRIVATE KEY" || block.Type == "PRIVATE KEY"),
			kind == "CMS":
			return block.Bytes, nil
		}
		rest = more
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("attestation: %s %s: empty file", what, path)
	}
	if raw[0] != 0x30 {
		return nil, fmt.Errorf("attestation: %s %s: neither a %s PEM block nor DER", what, path, kind)
	}
	return raw, nil
}

// parseECPrivateKey accepts PKCS#8 and SEC1 encodings of a P-256 key.
func parseECPrivateKey(der []byte) (*ecdsa.PrivateKey, error) {
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("not an EC key")
		}
		return checkP256(ec)
	}
	k, err := x509.ParseECPrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("neither PKCS#8 nor SEC1: %w", err)
	}
	return checkP256(k)
}

func checkP256(k *ecdsa.PrivateKey) (*ecdsa.PrivateKey, error) {
	if k.Curve.Params().Name != "P-256" {
		return nil, fmt.Errorf("curve %s, want P-256 (Matter §6.2.2)", k.Curve.Params().Name)
	}
	return k, nil
}
