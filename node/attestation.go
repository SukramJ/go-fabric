// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/secure/attestation"
)

// AttestationFiles names a vendor attestation chain on disk — the fields
// of attestation.ChainFiles: the Device Attestation Certificate, its
// private key, the Product Attestation Intermediate and the Certification
// Declaration. Each file may be PEM or DER, the key PKCS#8 or SEC1.
type AttestationFiles struct {
	DACPath    string
	DACKeyPath string
	PAIPath    string
	CDPath     string
}

// ErrNoChainConfigured is [LoadAttestation]'s answer when every path is
// empty (attestation.ErrNoChainConfigured, the same value).
var ErrNoChainConfigured = attestation.ErrNoChainConfigured

// LoadAttestation reads the chain f names (attestation.LoadChainFiles)
// into the [Attestation] a node serves. With every path empty it returns
// [ErrNoChainConfigured]; a file that is missing or does not parse, a key
// that is not the DAC's and a certificate too large to serve are errors.
// [Attestation].Files has [New] call it; a host that keeps running on a
// chain that does not load — blocking commissioning
// ([Node.SetCommissioningBlocked]) instead of failing — calls it itself
// and passes the zero Attestation on an error.
func LoadAttestation(f AttestationFiles) (Attestation, error) {
	chain, cd, err := attestation.LoadChainFiles(attestation.ChainFiles{
		DACPath: f.DACPath, DACKeyPath: f.DACKeyPath, PAIPath: f.PAIPath, CDPath: f.CDPath,
	})
	if err != nil {
		return Attestation{}, err
	}
	return Attestation{Chain: chain, CertificationDeclaration: cd}, nil
}

// loadAttestationFiles resolves Attestation.Files: the loaded chain and
// CD, or the zero value — the CSA test chain — when the files name
// nothing. A chain that does not load fails New: a node told to serve a
// vendor identity must not pair as the test vendor.
func (c *Config) loadAttestationFiles() error {
	f := c.Attestation.Files
	if f == nil {
		return nil
	}
	a, err := LoadAttestation(*f)
	switch {
	case errors.Is(err, ErrNoChainConfigured):
		c.Attestation = Attestation{Files: f}
		return nil
	case err != nil:
		return fmt.Errorf("node: attestation files: %w", err)
	}
	a.Files = f
	c.Attestation = a
	return nil
}
