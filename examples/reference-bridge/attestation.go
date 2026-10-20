// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/SukramJ/go-fabric/node"
)

// attestationFiles names the four files --attestation-dir holds. A
// vendor's PKI hands over exactly these; the loader accepts PEM or DER
// for each regardless of the suffix.
func attestationFiles(dir string) node.AttestationFiles {
	return node.AttestationFiles{
		DACPath:    filepath.Join(dir, "dac.pem"),
		DACKeyPath: filepath.Join(dir, "dac-key.pem"),
		PAIPath:    filepath.Join(dir, "pai.pem"),
		CDPath:     filepath.Join(dir, "cd.der"),
	}
}

// loadAttestation is the daemon's --attestation-dir: empty keeps the
// node's default, the CSA test chain (pairable everywhere, not
// shippable); a directory loads the vendor chain through
// node.LoadAttestation, and a chain that does not load is a start-up
// error naming the flag rather than a silent fall-back — a daemon told
// to serve a vendor identity must not pair as the test vendor.
func loadAttestation(dir string) (node.Attestation, error) {
	if dir == "" {
		return node.Attestation{}, nil
	}
	att, err := node.LoadAttestation(attestationFiles(dir))
	if err != nil {
		if errors.Is(err, node.ErrNoChainConfigured) {
			return node.Attestation{}, nil
		}
		return node.Attestation{}, fmt.Errorf("--attestation-dir %s: %w", dir, err)
	}
	return att, nil
}
