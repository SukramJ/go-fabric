// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"path/filepath"

	"github.com/SukramJ/go-fabric/node"
)

// attestationConfig is the daemon's --attestation-dir: empty keeps the
// node's default, the CSA test chain (pairable everywhere, not
// shippable); a directory names the four files a vendor's PKI hands over
// — PEM or DER each, whatever the suffix — and node.New loads them. A
// chain that does not load fails New rather than falling back: a daemon
// told to serve a vendor identity must not pair as the test vendor.
func attestationConfig(dir string) node.Attestation {
	if dir == "" {
		return node.Attestation{}
	}
	return node.Attestation{Files: &node.AttestationFiles{
		DACPath:    filepath.Join(dir, "dac.pem"),
		DACKeyPath: filepath.Join(dir, "dac-key.pem"),
		PAIPath:    filepath.Join(dir, "pai.pem"),
		CDPath:     filepath.Join(dir, "cd.der"),
	}}
}
