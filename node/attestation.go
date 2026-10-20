// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/secure/attestation"
)

// AttestationFiles names a vendor attestation chain on disk
// ([attestation.ChainFiles]): the Device Attestation Certificate, its
// private key, the Product Attestation Intermediate and the Certification
// Declaration. Each file may be PEM or DER, the key PKCS#8 or SEC1.
type AttestationFiles = attestation.ChainFiles

// resolveAttestation loads Attestation.Files (attestation.LoadChainFiles)
// into Chain and CertificationDeclaration; files that name nothing keep
// the CSA test chain. A chain that does not load fails New — a node told
// to serve a vendor identity must not pair as the test vendor — unless
// BlockCommissioningOnError is set: then the test chain stays, which no
// commissioner reaches, and blocked is the load error New blocks
// commissioning with.
func (c *Config) resolveAttestation() (blocked, err error) {
	a := &c.Attestation
	if a.Files == nil {
		return nil, nil
	}
	chain, cd, err := attestation.LoadChainFiles(*a.Files)
	switch {
	case errors.Is(err, attestation.ErrNoChainConfigured):
		return nil, nil
	case err != nil && a.BlockCommissioningOnError:
		return fmt.Errorf("node: attestation files: %w", err), nil
	case err != nil:
		return nil, fmt.Errorf("node: attestation files: %w", err)
	}
	a.Chain, a.CertificationDeclaration = chain, cd
	return nil, nil
}

// AttestationError is the load error of [Attestation].Files under
// [Attestation].BlockCommissioningOnError: the node runs on the test chain
// and refuses every commissioning until the files are fixed and a new
// node is built. nil when the files loaded, named nothing, or are unset.
func (n *Node) AttestationError() error { return n.attestationErr }
