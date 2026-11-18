// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package attestids reads the Matter vendor and product id of an
// attestation certificate's distinguished name — the one implementation of
// that rule. Two packages read it under different policies:
// controller/attestation, the commissioner's attestation check, refuses a
// malformed or repeated id attribute ([FromName] without acceptMalformed);
// secure/attestation, the device's own chain check, reads such an
// attribute as absent (with acceptMalformed). Every other part of the rule
// — which attribute counts, the commonName fallback and its refusals — is
// the same for both, and lives here once (review II, R2-P1-sec-02 /
// R2-P5-port-12).
//
// The package imports the standard library only.
package attestids

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The Matter attestation DN attributes: matter-oid-vid and matter-oid-pid
// (matter.js names them VendorID and ProductID, DistinguishedName.ts:244
// at db021da0), and X.520 commonName (2.5.4.3), whose value carries
// the `Mvid:`/`Mpid:` fallback.
var (
	OIDVendorID   = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 1}
	OIDProductID  = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 2}
	OIDCommonName = asn1.ObjectIdentifier{2, 5, 4, 3}
)

// ErrMalformedAttribute is wrapped by the error [FromName] returns, without
// acceptMalformed, for a matter-oid-vid or matter-oid-pid attribute that is
// not exactly 4 uppercase hex digits or that occurs more than once. The
// commonName fallback's refusal does not wrap it.
var ErrMalformedAttribute = errors.New("attestids: malformed Matter id attribute")

// attributeError is a malformed or repeated id attribute; its text is the
// whole message, so a caller that re-types it keeps the wording.
type attributeError struct{ msg string }

func (e *attributeError) Error() string { return e.msg }

func (e *attributeError) Is(target error) bool { return target == ErrMalformedAttribute }

// FromName is the vendor and product id of one DN, as matter.js's
// parseSubjectOrIssuer and decodeDistinguishedName read them
// (Certificate.ts:302-382, DistinguishedName.ts:252-328 at db021da0):
//
//   - a matter-oid-vid or matter-oid-pid attribute must be exactly 4
//     uppercase hex digits (parseMatterRdnHex, Certificate.ts:242-249,
//     :341-353) and occur once (DistinguishedName.ts:261-270, :277-281).
//     Without acceptMalformed either violation is an error wrapping
//     [ErrMalformedAttribute], the first in DN order, an invalid value
//     reported before a repetition; with acceptMalformed the id is read as
//     absent, whatever the other attribute of its type says (:258, :318-320);
//   - the commonName fallback is read only when the DN carries neither
//     attribute, a malformed one counting (Certificate.ts:343, :377-381):
//     the first commonName, in DN order, that carries a VendorID or
//     ProductID supplies both, searched in its first 64 bytes
//     (DistinguishedName.ts:345-374); a prefix without 4 uppercase hex
//     digits in a commonName read is an error under either policy, which
//     acceptMalformedVidPid does not cover (:63-72).
//
// A value that is not a string counts as malformed; a commonName that is
// not a string is not read. crypto/x509 has decoded the values by their
// string tags; [FromRawName] reads them as matter.js does.
func FromName(name pkix.Name, acceptMalformed bool) (vid, pid *uint16, err error) {
	attrs := make([]attribute, 0, len(name.Names))
	for _, n := range name.Names {
		s, ok := n.Value.(string)
		attrs = append(attrs, attribute{oid: n.Type, text: s, isString: ok})
	}
	return fromAttributes(attrs, acceptMalformed)
}

// FromRawName is [FromName] over the DER of a distinguished name — an
// x509.Certificate's RawSubject or RawIssuer — reading every value from its
// content bytes as text, whatever its string tag, as matter.js's
// parseSubjectOrIssuer does (`Bytes.toString(Bytes.of(valueNode._bytes))`,
// packages/protocol/src/certificate/kinds/Certificate.ts:321-326 at
// db021da0): "FFF1" in an OCTET STRING is 0xFFF1, in a BMPString it is
// malformed. An attribute of other than two elements is skipped (:319); one
// whose first element is not an OBJECT IDENTIFIER is skipped too, a rule of
// this module (matter.js looks its bytes up by hex without a tag test,
// :322-323). Review II, R2-P6-par-2904.
func FromRawName(raw []byte, acceptMalformed bool) (vid, pid *uint16, err error) {
	attrs, err := rawAttributes(raw)
	if err != nil {
		return nil, nil, err
	}
	return fromAttributes(attrs, acceptMalformed)
}

// attribute is one attribute of a distinguished name: its type and its
// value as text, isString false when the value is not text.
type attribute struct {
	oid      asn1.ObjectIdentifier
	text     string
	isString bool
}

// rawAttributes lists the attributes of the DER RDNSequence raw in order,
// each value's content bytes as its text ([FromRawName]).
func rawAttributes(raw []byte) ([]attribute, error) {
	var seq asn1.RawValue
	if rest, err := asn1.Unmarshal(raw, &seq); err != nil || len(rest) != 0 || seq.Tag != asn1.TagSequence || !seq.IsCompound {
		return nil, errors.New("attestids: invalid subject/issuer structure")
	}
	var out []attribute
	for rdns := seq.Bytes; len(rdns) > 0; {
		var set asn1.RawValue
		var err error
		if rdns, err = asn1.Unmarshal(rdns, &set); err != nil {
			return nil, errors.New("attestids: invalid subject/issuer structure")
		}
		if !set.IsCompound {
			continue
		}
		for atvs := set.Bytes; len(atvs) > 0; {
			var atv asn1.RawValue
			if atvs, err = asn1.Unmarshal(atvs, &atv); err != nil {
				return nil, errors.New("attestids: invalid subject/issuer structure")
			}
			elems, ok := children(atv)
			if !ok || len(elems) != 2 {
				continue
			}
			var oid asn1.ObjectIdentifier
			if _, err := asn1.Unmarshal(elems[0].FullBytes, &oid); err != nil {
				continue
			}
			out = append(out, attribute{oid: oid, text: string(elems[1].Bytes), isString: true})
		}
	}
	return out, nil
}

// children is the elements of the constructed value v.
func children(v asn1.RawValue) ([]asn1.RawValue, bool) {
	if !v.IsCompound {
		return nil, false
	}
	var out []asn1.RawValue
	for b := v.Bytes; len(b) > 0; {
		var e asn1.RawValue
		var err error
		if b, err = asn1.Unmarshal(b, &e); err != nil {
			return nil, false
		}
		out = append(out, e)
	}
	return out, true
}

// fromAttributes is the rule of [FromName] over attrs, in DN order.
func fromAttributes(attrs []attribute, acceptMalformed bool) (vid, pid *uint16, err error) {
	hasAttribute, vidUnusable, pidUnusable := false, false, false
	var commonNames []string
	for _, n := range attrs {
		var field string
		var out **uint16
		var unusable *bool
		switch {
		case n.oid.Equal(OIDVendorID):
			field, out, unusable = "vendorId", &vid, &vidUnusable
		case n.oid.Equal(OIDProductID):
			field, out, unusable = "productId", &pid, &pidUnusable
		case n.oid.Equal(OIDCommonName):
			if n.isString {
				commonNames = append(commonNames, n.text)
			}
			continue
		default:
			continue
		}
		hasAttribute = true
		s := n.text
		id, ok := ParseHexID(s)
		switch {
		case !ok:
			if !acceptMalformed {
				return nil, nil, &attributeError{msg: fmt.Sprintf("Invalid %s %q: expected exactly 4 uppercase hexadecimal digits", field, s)}
			}
			*unusable = true
		case *out != nil:
			if !acceptMalformed {
				return nil, nil, &attributeError{msg: fmt.Sprintf("Distinguished name contains %s more than once", field)}
			}
			*unusable = true
		default:
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
	return fallbackIDs(commonNames)
}

// maxFallbackCommonNameBytes is MAX_FALLBACK_COMMON_NAME_BYTES
// (DistinguishedName.ts:345-346 at db021da0).
const maxFallbackCommonNameBytes = 64

// fallbackIDs is fallbackVidPid (DistinguishedName.ts:354-374 at
// db021da0): the first commonName whose first 64 bytes carry a VendorID
// or ProductID supplies both, the VendorID read first (:367-368); a later
// one is not read, an earlier one with a malformed prefix refuses the DN.
func fallbackIDs(commonNames []string) (vid, pid *uint16, err error) {
	for _, cn := range commonNames {
		if len(cn) > maxFallbackCommonNameBytes {
			cn = cn[:maxFallbackCommonNameBytes]
		}
		if vid, err = FallbackID(cn, "Mvid:"); err != nil {
			return nil, nil, err
		}
		if pid, err = FallbackID(cn, "Mpid:"); err != nil {
			return nil, nil, err
		}
		if vid != nil || pid != nil {
			return vid, pid, nil
		}
	}
	return nil, nil, nil
}

// ParseHexID is parseMatterRdnHex(field, text, 2) (Certificate.ts:242-249
// at db021da0): exactly 4 uppercase hex digits, nothing else.
func ParseHexID(s string) (uint16, bool) {
	if !hexIDPattern.MatchString(s) {
		return 0, false
	}
	v, _ := strconv.ParseUint(s, 16, 16)
	return uint16(v), true
}

var hexIDPattern = regexp.MustCompile(`^[0-9A-F]{4}$`)

var fallbackPatterns = map[string]*regexp.Regexp{
	"Mvid:": regexp.MustCompile(`Mvid:([0-9A-F]{4})`),
	"Mpid:": regexp.MustCompile(`Mpid:([0-9A-F]{4})`),
}

// FallbackID is parseMatterFallbackVidPid (DistinguishedName.ts:63-72 at
// db021da0): the leftmost prefix followed by 4 uppercase hex digits; the
// prefix without them is an error. prefix is "Mvid:" or "Mpid:".
func FallbackID(cn, prefix string) (*uint16, error) {
	if m := fallbackPatterns[prefix].FindStringSubmatch(cn); m != nil {
		v, _ := strconv.ParseUint(m[1], 16, 16)
		id := uint16(v)
		return &id, nil
	}
	if strings.Contains(cn, prefix) {
		return nil, fmt.Errorf("attestation: commonName contains %s not followed by 4 uppercase hex digits", prefix)
	}
	return nil, nil
}
