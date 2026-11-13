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
	"strings"
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
// db021da0), which runs after the servable-size check of the chain
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
	if _, _, err := productIDsOf(c.Issuer); err != nil {
		return nil, nil, err
	}
	return productIDsOf(c.Subject)
}

// maxFallbackCommonNameBytes is MAX_FALLBACK_COMMON_NAME_BYTES
// (DistinguishedName.ts:345-346 at db021da0).
const maxFallbackCommonNameBytes = 64

var (
	hexIDPattern       = regexp.MustCompile(`^[0-9A-F]{4}$`)
	fallbackIDPatterns = map[string]*regexp.Regexp{
		"Mvid:": regexp.MustCompile(`Mvid:([0-9A-F]{4})`),
		"Mpid:": regexp.MustCompile(`Mpid:([0-9A-F]{4})`),
	}
)

// productIDsOf reads a DN's vendor and product id as matter.js's DER parse
// does with acceptMalformedVidPid (Certificate.ts:302-382,
// DistinguishedName.ts:252-328 at db021da0):
//
//   - a matter-oid-vid or matter-oid-pid attribute that is not exactly 4
//     uppercase hex digits (parseMatterRdnHex, Certificate.ts:242-249,
//     :341-351), or that occurs twice (DistinguishedName.ts:261-270), is
//     read as absent, whatever the other attribute of its type says
//     (:318-320);
//   - the commonName fallback is read only when the DN carries neither
//     attribute, a malformed one counting (Certificate.ts:343, :377-381):
//     the first commonName, in DN order, whose first 64 bytes carry a
//     VendorID or ProductID supplies both (DistinguishedName.ts:345-374);
//     a prefix without 4 uppercase hex digits in a commonName read is an
//     error, which acceptMalformedVidPid does not cover (:63-72).
//
// controller/attestation reads the same rule without
// acceptMalformedVidPid, where a malformed or repeated id is a profile
// violation.
func productIDsOf(name pkix.Name) (vid, pid *uint16, err error) {
	hasAttribute, vidUnusable, pidUnusable := false, false, false
	var commonNames []string
	for _, n := range name.Names {
		var out **uint16
		var unusable *bool
		switch {
		case n.Type.Equal(oidMatterVendorID):
			out, unusable = &vid, &vidUnusable
		case n.Type.Equal(oidMatterProductID):
			out, unusable = &pid, &pidUnusable
		case n.Type.Equal(oidCommonName):
			if s, ok := n.Value.(string); ok {
				commonNames = append(commonNames, s)
			}
			continue
		default:
			continue
		}
		hasAttribute = true
		s, _ := n.Value.(string)
		switch {
		case !hexIDPattern.MatchString(s), *out != nil:
			*unusable = true
		default:
			v, _ := strconv.ParseUint(s, 16, 16)
			id := uint16(v)
			*out = &id
		}
	}
	if hasAttribute {
		if vidUnusable {
			vid = nil
		}
		if pidUnusable {
			pid = nil
		}
		return vid, pid, nil
	}
	for _, cn := range commonNames {
		if len(cn) > maxFallbackCommonNameBytes {
			cn = cn[:maxFallbackCommonNameBytes]
		}
		// VendorID first, as fallbackVidPid reads them (:367-368).
		for _, f := range []struct {
			prefix string
			out    **uint16
		}{{"Mvid:", &vid}, {"Mpid:", &pid}} {
			if m := fallbackIDPatterns[f.prefix].FindStringSubmatch(cn); m != nil {
				v, _ := strconv.ParseUint(m[1], 16, 16)
				id := uint16(v)
				*f.out = &id
			} else if strings.Contains(cn, f.prefix) {
				return nil, nil, fmt.Errorf("attestation: commonName contains %s not followed by 4 uppercase hex digits", f.prefix)
			}
		}
		if vid != nil || pid != nil {
			return vid, pid, nil
		}
	}
	return nil, nil, nil
}
