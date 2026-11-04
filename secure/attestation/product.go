// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// ErrProductMismatch is returned by [ValidateProduct] when the DAC's or
// the PAI's vendor or product id contradicts the node's product.
var ErrProductMismatch = errors.New("attestation: the chain's vendor or product id does not match the product")

// ValidateProduct checks a loaded chain against the product the node
// serves as: a vendor id the DAC carries must be the product's, a product
// id the DAC carries must be the product's, a vendor id the PAI carries
// must be the product's; an id a certificate does not carry is not
// judged. Mirrors matter.js DeviceCertification.#validateCertification
// (packages/protocol/src/certificate/DeviceCertification.ts:107-140 at
// 862ad75f), which runs after the servable-size check of the chain
// (#assertServable, :87-105 — [LoadChainFiles] here). The ids are read as
// matter.js reads a subject: the Matter OIDs (Certificate.ts:346-347
// vendorId/productId), else the `Mvid:`/`Mpid:` fallback parsed from the
// commonName (Certificate.ts:380-385, parseMatterFallbackVidPid).
func ValidateProduct(chain *Chain, vendorID, productID uint16) error {
	if chain == nil {
		return errors.New("attestation: ValidateProduct: nil chain")
	}
	dac, err := x509.ParseCertificate(chain.DAC)
	if err != nil {
		return fmt.Errorf("attestation: DAC: %w", err)
	}
	pai, err := x509.ParseCertificate(chain.PAI)
	if err != nil {
		return fmt.Errorf("attestation: PAI: %w", err)
	}
	dacVID, dacPID := productIDsOf(dac.Subject)
	if dacVID != nil && *dacVID != vendorID {
		return fmt.Errorf("%w: DAC VendorID %#04x does not match product VendorID %#04x", ErrProductMismatch, *dacVID, vendorID)
	}
	if dacPID != nil && *dacPID != productID {
		return fmt.Errorf("%w: DAC ProductID %#04x does not match product ProductID %#04x", ErrProductMismatch, *dacPID, productID)
	}
	if paiVID, _ := productIDsOf(pai.Subject); paiVID != nil && *paiVID != vendorID {
		return fmt.Errorf("%w: PAI VendorID %#04x does not match product VendorID %#04x", ErrProductMismatch, *paiVID, vendorID)
	}
	return nil
}

var fallbackIDPatterns = map[string]*regexp.Regexp{
	"Mvid:": regexp.MustCompile(`Mvid:([0-9A-Fa-f]{4})`),
	"Mpid:": regexp.MustCompile(`Mpid:([0-9A-Fa-f]{4})`),
}

// productIDsOf reads a DN's vendor and product id: the Matter OID
// attributes as hex, else — when the DN carries neither — the `Mvid:` and
// `Mpid:` fallbacks of its commonName (Matter §6.2.2.2).
func productIDsOf(name pkix.Name) (vid, pid *uint16) {
	hexID := func(s string) *uint16 {
		end := 0
		for end < len(s) && ((s[end] >= '0' && s[end] <= '9') || (s[end] >= 'a' && s[end] <= 'f') || (s[end] >= 'A' && s[end] <= 'F')) {
			end++
		}
		v, err := strconv.ParseUint(s[:end], 16, 16)
		if end == 0 || err != nil {
			return nil
		}
		id := uint16(v)
		return &id
	}
	for _, n := range name.Names {
		s, ok := n.Value.(string)
		switch {
		case !ok:
		case n.Type.Equal(oidMatterVendorID):
			vid = hexID(s)
		case n.Type.Equal(oidMatterProductID):
			pid = hexID(s)
		}
	}
	if vid != nil || pid != nil {
		return vid, pid
	}
	for prefix, out := range map[string]**uint16{"Mvid:": &vid, "Mpid:": &pid} {
		if m := fallbackIDPatterns[prefix].FindStringSubmatch(name.CommonName); m != nil {
			v, _ := strconv.ParseUint(m[1], 16, 16)
			id := uint16(v)
			*out = &id
		}
	}
	return vid, pid
}
