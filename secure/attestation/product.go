// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"

	"thingfabric.dev/go-fabric/internal/attestids"
)

// ErrProductMismatch is returned by [ValidateProduct] when the DAC's or
// the PAI's vendor or product id contradicts the node's product.
var ErrProductMismatch = errors.New("attestation: the chain's vendor or product id does not match the product")

// ValidateProduct checks a loaded chain against the product the node
// serves as: a vendor id the DAC carries must be the product's, a product
// id the DAC carries must be the product's, a vendor id the PAI carries
// must be the product's; an id a certificate does not carry is not
// judged. Mirrors matter.js DeviceCertification.#validateCertification
// (packages/protocol/src/certificate/DeviceCertification.ts:107-145 at
// db91886c), which runs after the servable-size check of the chain
// (#assertServable — [LoadChainFiles] here). The ids are read as that
// method's Dac.fromAsn1 and Pai.fromAsn1 read them, with
// acceptMalformedVidPid (:116-120): see [productIDsOf]; the issuer of
// each is read too (Certificate.ts:729 at db021da0), so a malformed
// `Mvid:`/`Mpid:` prefix there refuses the chain.
func ValidateProduct(chain *Chain, vendorID, productID uint16) error {
	if chain == nil {
		return errors.New("attestation: ValidateProduct: nil chain")
	}
	dac, err := x509.ParseCertificate(chain.DAC)
	if err != nil {
		return fmt.Errorf("attestation: DAC: %w", err)
	}
	dacVID, dacPID, err := certificateIDs(dac)
	if err != nil {
		return fmt.Errorf("attestation: DAC: %w", err)
	}
	pai, err := x509.ParseCertificate(chain.PAI)
	if err != nil {
		return fmt.Errorf("attestation: PAI: %w", err)
	}
	paiVID, _, err := certificateIDs(pai)
	if err != nil {
		return fmt.Errorf("attestation: PAI: %w", err)
	}
	if dacVID != nil && *dacVID != vendorID {
		return fmt.Errorf("%w: DAC VendorID %#04x does not match product VendorID %#04x", ErrProductMismatch, *dacVID, vendorID)
	}
	if dacPID != nil && *dacPID != productID {
		return fmt.Errorf("%w: DAC ProductID %#04x does not match product ProductID %#04x", ErrProductMismatch, *dacPID, productID)
	}
	if paiVID != nil && *paiVID != vendorID {
		return fmt.Errorf("%w: PAI VendorID %#04x does not match product VendorID %#04x", ErrProductMismatch, *paiVID, vendorID)
	}
	return nil
}

// certificateIDs reads the issuer's ids, then the subject's, as the DER
// parse does (Certificate.ts:729, :740 at db021da0), and returns the
// subject's.
func certificateIDs(c *x509.Certificate) (vid, pid *uint16, err error) {
	if _, _, err := attestids.FromRawName(c.RawIssuer, true); err != nil {
		return nil, nil, err
	}
	// The values are read from their content bytes, whatever their string
	// tag ([attestids.FromRawName]; review II, R2-P6-par-2904).
	return attestids.FromRawName(c.RawSubject, true)
}

// productIDsOf reads a DN's vendor and product id as matter.js's DER parse
// does with acceptMalformedVidPid: the rule of [attestids.FromName] with
// acceptMalformed, where a malformed or repeated matter-oid-vid or
// matter-oid-pid attribute is read as absent and a malformed commonName
// fallback is still an error. controller/attestation reads the same rule
// without acceptMalformedVidPid, where a malformed or repeated id is a
// profile violation.
func productIDsOf(name pkix.Name) (vid, pid *uint16, err error) {
	return attestids.FromName(name, true)
}
