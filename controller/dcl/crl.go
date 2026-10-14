// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The DER reading of a CRL as matter.js does it: its DerCodec
// (packages/general/src/codec/DerCodec.ts at 0ceb202d) and
// DclCertificateService.parseCrl (DclCertificateService.ts:1785-1998).
// crypto/x509's ParseRevocationList reads differently — it neither reports
// an unknown critical extension nor an Issuing Distribution Point URI —
// so the CRL is walked here node by node, as matter.js walks it.

// DER tags as DerCodec reports them (DerCodec.ts:18-45).
const (
	derBoolean         = 0x01
	derInteger         = 0x02
	derBitString       = 0x03
	derOctetString     = 0x04
	derObjectID        = 0x06
	derUTF8String      = 0x0c
	derPrintableString = 0x13
	derT16String       = 0x14
	derIA5String       = 0x16
	derUTCTime         = 0x17
	derGeneralizedTime = 0x18
	derSequence        = 0x30
	derConstructed     = 0x20
	// derContext0 is [0] IMPLICIT/EXPLICIT constructed: the crlExtensions
	// of a TBSCertList, distributionPoint and its fullName in an Issuing
	// Distribution Point (DclCertificateService.ts:264-265).
	derContext0 = 0xa0
	// derDirectoryName is [4] EXPLICIT Name, GeneralName's directoryName
	// (:261-262).
	derDirectoryName = 0xa4
	// derURIGeneralName is [6] IMPLICIT IA5String, GeneralName's
	// uniformResourceIdentifier (:267-268).
	derURIGeneralName = 0x86
	// derKeyIdentifier is [0] IMPLICIT, AuthorityKeyIdentifier's
	// keyIdentifier.
	derKeyIdentifier = 0x80
	// derMaxLengthBytes is MAX_LENGTH_BYTES (DerCodec.ts:16).
	derMaxLengthBytes = 6
)

// The extensions parseCrl reads, by OID content as hex — KNOWN_CRL_EXTENSIONS
// and KNOWN_CRL_ENTRY_EXTENSIONS (DclCertificateService.ts:235-241).
var (
	knownCRLExtensions      = []string{"551d23", "551d1c", "551d14"}
	knownCRLEntryExtensions = []string{"551d15", "551d18", "551d1d"}
)

const (
	oidAuthorityKeyID          = "551d23"
	oidIssuingDistributionPt   = "551d1c"
	oidCertificateIssuerEntry  = "551d1d"
	errDERTruncatedDescription = "DER data is truncated"
)

// derNode is DerCodec's DerNode: the tag byte, the content, the elements
// of a constructed node, the unused-bits count of a BIT STRING.
type derNode struct {
	tag      byte
	bytes    []byte
	elements []derNode
	padding  byte
}

var errDERTruncated = errors.New(errDERTruncatedDescription)

// derDecode is DerCodec.decode (DerCodec.ts:276-283, #decodeRec :429-442,
// #decodeAsn1 :444-463): the first TLV of data, a constructed one decoded
// element by element, trailing bytes ignored.
func derDecode(data []byte) (derNode, error) {
	n, _, err := derDecodeRec(data)
	return n, err
}

func derDecodeRec(data []byte) (derNode, []byte, error) {
	if len(data) < 2 {
		return derNode{}, nil, errDERTruncated
	}
	tag, length := data[0], int(data[1])
	rest := data[2:]
	if length&0x80 != 0 {
		ll := length & 0x7f
		if ll == 0 {
			return derNode{}, nil, errors.New("DER forbids the indefinite-length encoding")
		}
		if ll > derMaxLengthBytes {
			return derNode{}, nil, errors.New("DER length exceeds the maximum")
		}
		if len(rest) < ll {
			return derNode{}, nil, errDERTruncated
		}
		length = 0
		for _, b := range rest[:ll] {
			length = length*0x100 + int(b)
		}
		rest = rest[ll:]
	}
	if length < 0 || len(rest) < length {
		return derNode{}, nil, errDERTruncated
	}
	content, rest := rest[:length], rest[length:]
	if tag == derBitString {
		if len(content) == 0 {
			return derNode{tag: tag}, rest, nil
		}
		return derNode{tag: tag, bytes: content[1:], padding: content[0]}, rest, nil
	}
	if tag&derConstructed == 0 {
		return derNode{tag: tag, bytes: content}, rest, nil
	}
	n := derNode{tag: tag, bytes: content, elements: []derNode{}}
	for r := content; len(r) > 0; {
		el, next, err := derDecodeRec(r)
		if err != nil {
			return derNode{}, nil, err
		}
		n.elements = append(n.elements, el)
		r = next
	}
	return n, rest, nil
}

// derEncode is DerCodec.encode of a decoded node: its tag, the minimal
// length and its content (a BIT STRING's unused-bits count first).
func derEncode(n derNode) []byte {
	content := n.bytes
	if n.tag == derBitString {
		content = append([]byte{n.padding}, n.bytes...)
	}
	out := []byte{n.tag}
	switch l := len(content); {
	case l < 0x80:
		out = append(out, byte(l)) //nolint:gosec // below 0x80
	default:
		var lb []byte
		for v := l; v > 0; v >>= 8 {
			lb = append([]byte{byte(v & 0xff)}, lb...)
		}
		out = append(out, 0x80|byte(len(lb)&0x7f))
		out = append(out, lb...)
	}
	return append(out, content...)
}

// isCritical is DclCertificateService.ts:243-246.
func isCritical(ext derNode) bool {
	return len(ext.elements) > 1 && ext.elements[1].tag == derBoolean && len(ext.elements[1].bytes) > 0 && ext.elements[1].bytes[0] != 0
}

// issuingDistributionPointURIOf is :127-150: the URI of a critical Issuing
// Distribution Point whose distributionPoint is a fullName of exactly one
// uniformResourceIdentifier, as Matter Core §6.2.6.1 step 7.2 requires;
// "" otherwise.
func issuingDistributionPointURIOf(ext derNode) string {
	if len(ext.elements) < 3 {
		return ""
	}
	critical, value := ext.elements[1], ext.elements[2]
	if critical.tag != derBoolean || len(critical.bytes) == 0 || critical.bytes[0] == 0 || value.tag != derOctetString {
		return ""
	}
	idp, err := derDecode(value.bytes)
	if err != nil || len(idp.elements) == 0 || idp.elements[0].tag != derContext0 {
		return ""
	}
	name, err := derDecode(idp.elements[0].bytes)
	if err != nil || name.tag != derContext0 {
		return ""
	}
	uri, err := derDecode(name.bytes)
	if err != nil || uri.tag != derURIGeneralName || len(derEncode(uri)) != len(name.bytes) {
		return ""
	}
	return string(uri.bytes)
}

// certificateIssuerOf is :249-259: the single directoryName of a
// certificateIssuer entry extension as Name DER, nil unless it names
// exactly one.
func certificateIssuerOf(ext derNode) []byte {
	if len(ext.elements) == 0 {
		return nil
	}
	value := ext.elements[len(ext.elements)-1]
	if value.tag != derOctetString {
		return nil
	}
	names, err := derDecode(value.bytes)
	if err != nil || len(names.elements) != 1 || names.elements[0].tag != derDirectoryName {
		return nil
	}
	return names.elements[0].bytes
}

// directoryStringTags are DIRECTORY_STRING_TAGS (:177-182).
var directoryStringTags = []byte{derUTF8String, derPrintableString, derT16String, derIA5String}

// sameName is :188-212: two DER Names are the same name under RFC 5280
// §7.1 — string attributes compare ignoring case, surrounding and repeated
// whitespace, and their string type.
func sameName(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	left, err := canonicalName(a)
	if err != nil {
		return false
	}
	right, err := canonicalName(b)
	if err != nil {
		return false
	}
	return slices.Equal(left, right)
}

func canonicalName(der []byte) ([]string, error) {
	n, err := derDecode(der)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(n.elements))
	for _, rdn := range n.elements {
		attrs := make([]string, 0, len(rdn.elements))
		for _, attr := range rdn.elements {
			var typ, value *derNode
			if len(attr.elements) > 0 {
				typ = &attr.elements[0]
			}
			if len(attr.elements) > 1 {
				value = &attr.elements[1]
			}
			var text string
			if value != nil && slices.Contains(directoryStringTags, value.tag) {
				text = strings.ToLower(strings.Join(strings.Fields(string(value.bytes)), " "))
			} else {
				tag, content := "undefined", []byte{}
				if value != nil {
					tag, content = strconv.Itoa(int(value.tag)), value.bytes
				}
				text = tag + ":" + strings.ToLower(hexUpper(content))
			}
			var typeHex string
			if typ != nil {
				typeHex = strings.ToLower(hexUpper(typ.bytes))
			}
			attrs = append(attrs, typeHex+"="+text)
		}
		slices.Sort(attrs)
		out = append(out, strings.Join(attrs, "+"))
	}
	return out, nil
}

// sameNameHex is :215-218: sameName for Names as hex; a value that is not
// hex names nothing.
func sameNameHex(a, b string) bool {
	da, ok := decodeHex(a)
	if !ok {
		return false
	}
	db, ok := decodeHex(b)
	if !ok {
		return false
	}
	return sameName(da, db)
}

func decodeHex(s string) ([]byte, bool) {
	if len(s)%2 != 0 {
		return nil, false
	}
	out := make([]byte, len(s)/2)
	for i := range out {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, false
		}
		out[i] = byte(v)
	}
	return out, true
}

// crlParseResult is DclCertificateService.CrlParseResult (:2123-2159).
type crlParseResult struct {
	// serials are the revoked serial numbers, uppercase hex of the DER
	// INTEGER content.
	serials map[string]struct{}
	// issuerDN is the hex of the issuer Name DER, "" when absent.
	issuerDN string
	// authorityKeyID is uppercase hex, "" when absent.
	authorityKeyID string
	// tbs is the re-encoded tbsCertList the signature is over.
	tbs []byte
	// signatureAlgorithm is the AlgorithmIdentifier DER, nil when it
	// differs from the one inside tbsCertList.
	signatureAlgorithm []byte
	// signatureValue is nil unless a BIT STRING without unused bits.
	signatureValue []byte
	// issuingDistributionPointURI is "" unless a critical IDP names one URI.
	issuingDistributionPointURI string
	// unsupportedCriticalExtension is the OID hex of the first critical
	// extension the parser does not support, "" when none.
	unsupportedCriticalExtension string
	// nextUpdate is the CRL's nextUpdate, zero when absent; matter.js
	// parses it and does not use it.
	nextUpdate time.Time
}

// parseCRL is DclCertificateService.parseCrl (:1785-1998). authorityName,
// when set, drops the entries an indirect CRL names for another issuer
// (§6.2.6.1 step 10.1).
func parseCRL(crlDER, authorityName []byte) (crlParseResult, error) {
	r := crlParseResult{serials: map[string]struct{}{}}
	decoded, err := derDecode(crlDER)
	if err != nil {
		return r, err
	}
	list := decoded.elements
	if len(list) < 2 {
		return r, nil
	}
	tbsList := list[0]
	tbs := tbsList.elements
	if tbs == nil {
		return r, nil
	}
	r.tbs = derEncode(tbsList)
	if len(list) >= 3 && list[2].tag == derBitString && list[2].padding == 0 {
		r.signatureValue = list[2].bytes
	}
	// RFC 5280 §5.1.1.2: the outer algorithm equals the inner (:1817-1824).
	outer := derEncode(list[1])
	if i := slices.IndexFunc(tbs, func(n derNode) bool { return n.tag == derSequence }); i >= 0 && bytes.Equal(outer, derEncode(tbs[i])) {
		r.signatureAlgorithm = outer
	}
	idx := 0
	at := func(tag ...byte) bool { return idx < len(tbs) && slices.Contains(tag, tbs[idx].tag) }
	if at(derInteger) {
		idx++
	}
	if at(derSequence) {
		idx++
	}
	if at(derSequence) {
		r.issuerDN = hexUpper(derEncode(tbs[idx]))
		idx++
	}
	if at(derUTCTime, derGeneralizedTime) {
		idx++
	}
	if at(derUTCTime, derGeneralizedTime) {
		r.nextUpdate = parseCRLTime(tbs[idx])
	}
	if err := r.readExtensions(tbs); err != nil {
		return r, err
	}
	// revokedCertificates: the first SEQUENCE whose first element is a
	// SEQUENCE starting with an INTEGER (:1921-1936).
	var revoked *derNode
	for i := range tbs {
		el := &tbs[i]
		if el.tag == derSequence && len(el.elements) > 0 {
			first := el.elements[0]
			if first.tag == derSequence && len(first.elements) > 0 && first.elements[0].tag == derInteger {
				revoked = el
				break
			}
		}
	}
	if revoked != nil {
		r.readEntries(revoked.elements, authorityName)
	}
	return r, nil
}

// readExtensions reads the crlExtensions, [0] EXPLICIT (:1876-1919): the
// first unsupported critical one, the Issuing Distribution Point URI and
// the authority key identifier.
func (r *crlParseResult) readExtensions(tbs []derNode) error {
	for _, el := range tbs {
		if el.tag != derContext0 {
			continue
		}
		extSeq, err := derDecode(el.bytes)
		if err != nil {
			return err
		}
		for _, ext := range extSeq.elements {
			if len(ext.elements) < 2 {
				continue
			}
			oid := strings.ToLower(hexUpper(ext.elements[0].bytes))
			if isCritical(ext) && !slices.Contains(knownCRLExtensions, oid) {
				r.unsupported(oid)
			}
			switch oid {
			case oidIssuingDistributionPt:
				r.issuingDistributionPointURI = issuingDistributionPointURIOf(ext)
			case oidAuthorityKeyID:
				if err := r.readAuthorityKeyID(ext); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// readAuthorityKeyID is :1897-1915: the keyIdentifier, [0], of the value
// after the critical flag.
func (r *crlParseResult) readAuthorityKeyID(ext derNode) error {
	vi := 1
	if len(ext.elements) > 2 {
		vi = 2
	}
	aki, err := derDecode(ext.elements[vi].bytes)
	if err != nil {
		return err
	}
	for _, f := range aki.elements {
		if f.tag == derKeyIdentifier {
			r.authorityKeyID = hexUpper(f.bytes)
			break
		}
	}
	return nil
}

// unsupported records the first unsupported critical extension
// (unsupportedCriticalExtension ??= oid).
func (r *crlParseResult) unsupported(oid string) {
	if r.unsupportedCriticalExtension == "" {
		r.unsupportedCriticalExtension = oid
	}
}

// readEntries reads the revoked serials (:1952-1985). An indirect CRL's
// certificateIssuer applies to the entries after it until another (RFC
// 5280 §5.3.3); an entry of another issuer than authorityName is not the
// authority's (§6.2.6.1 step 10.1).
func (r *crlParseResult) readEntries(entries []derNode, authorityName []byte) {
	var certificateIssuer []byte
	for _, entry := range entries {
		if entry.tag != derSequence || len(entry.elements) < 1 || entry.elements[0].tag != derInteger {
			continue
		}
		if len(entry.elements) > 2 && entry.elements[2].tag == derSequence {
			if named := r.readEntryExtensions(entry.elements[2].elements); named != nil {
				certificateIssuer = named
			}
		}
		if certificateIssuer != nil && authorityName != nil && !sameName(certificateIssuer, authorityName) {
			continue
		}
		r.serials[hexUpper(entry.elements[0].bytes)] = struct{}{}
	}
}

// readEntryExtensions reads one entry's extensions: the certificateIssuer
// it names (critical, one directoryName, else unsupported), and the first
// unsupported critical one.
func (r *crlParseResult) readEntryExtensions(exts []derNode) (certificateIssuer []byte) {
	for _, ext := range exts {
		var oid string
		if len(ext.elements) > 0 {
			oid = strings.ToLower(hexUpper(ext.elements[0].bytes))
		}
		switch {
		case oid == oidCertificateIssuerEntry:
			var named []byte
			if isCritical(ext) {
				named = certificateIssuerOf(ext)
			}
			if named == nil {
				r.unsupported(oid)
			} else {
				certificateIssuer = named
			}
		case isCritical(ext) && !slices.Contains(knownCRLEntryExtensions, oid):
			r.unsupported(oid)
		}
	}
	return certificateIssuer
}

// parseCRLTime reads a UTCTime (YYMMDDHHMMSSZ, a year of 50 and above in
// the 1900s) or GeneralizedTime (YYYYMMDDHHMMSSZ), :1849-1874; a value
// that does not read is the zero time.
func parseCRLTime(n derNode) time.Time {
	s := string(n.bytes)
	var year int
	var rest string
	if n.tag == derUTCTime {
		if len(s) < 2 {
			return time.Time{}
		}
		yy, err := strconv.Atoi(s[:2])
		if err != nil {
			return time.Time{}
		}
		year = 2000 + yy
		if yy >= 50 {
			year = 1900 + yy
		}
		rest = s[2:]
	} else {
		if len(s) < 4 {
			return time.Time{}
		}
		y, err := strconv.Atoi(s[:4])
		if err != nil {
			return time.Time{}
		}
		year, rest = y, s[4:]
	}
	if len(rest) < 10 {
		return time.Time{}
	}
	f := make([]int, 5)
	for i := range f {
		v, err := strconv.Atoi(rest[2*i : 2*i+2])
		if err != nil {
			return time.Time{}
		}
		f[i] = v
	}
	return time.Date(year, time.Month(f[0]), f[1], f[2], f[3], f[4], 0, time.UTC)
}
