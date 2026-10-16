// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"
)

// Context is what [Validate] works with — matter.js
// DeviceAttestationValidator.Context (DAV:113-124).
type Context struct {
	// Challenge is the PASE session's attestation challenge.
	Challenge []byte
	// Store is the trust store; nil skips the PAA, chain, revocation and
	// CD signature checks (dclCertificateService undefined).
	Store PAAStore
}

// Data is the device's attestation data — matter.js DeviceAttestationData
// (DAV:126-134).
type Data struct {
	// DAC and PAI are the X.509 DER certificates of CertificateChainRequest.
	DAC, PAI []byte
	// Elements and Signature are the AttestationResponse fields.
	Elements, Signature []byte
	// Nonce is the AttestationRequest nonce we sent.
	Nonce []byte
	// VendorID and ProductID are the node's BasicInformation values.
	VendorID, ProductID uint16
}

// Result is a successful validation — matter.js ValidationResult
// (DAV:137-140).
type Result struct {
	// DACPublicKey is the DAC's key, which the CSR signature is checked with.
	DACPublicKey *ecdsa.PublicKey
	// Findings are the non-fatal findings, in the order they arose.
	Findings []Finding
}

// Validate validates the device's attestation. Mirrors matter.js
// packages/protocol/src/certificate/DeviceAttestationValidator.ts:170-537
// (validate) at 0ceb202d, check for check and in its order; each step
// cites the DAV lines it ports. A hard failure is an [*Error]; any other
// error is the trust store's (a stored PAA that does not parse, a
// revocation lookup that fails), which the commissioner reports as a
// ValidationError finding.
func Validate(ctx Context, data Data) (Result, error) {
	var findings []Finding

	// Step 1: parse DAC and PAI from DER (DAV:174-176).
	dac, err := parsePeerCertificate("DAC", data.DAC, true)
	if err != nil {
		return Result{}, err
	}
	// The DAC key must be EC P-256 (Dac constructor,
	// kinds/AttestationCertificates.ts:66-72).
	dacPublicKey, err := P256Key(dac.x509)
	if err != nil {
		return Result{}, &Error{Check: CertificateUnparseable, Message: "Device returned a DAC certificate that cannot be parsed: " + err.Error(), Err: err}
	}
	pai, err := parsePeerCertificate("PAI", data.PAI, true)
	if err != nil {
		return Result{}, err
	}

	// === Local checks (always run, no DCL needed), DAV:178 ===

	// Step 4 (local part): DAC vendorId = PAI vendorId (DAV:180-186).
	if !sameID(dac.vendorID, pai.vendorID) {
		return Result{}, fail(VendorIDMismatch, fmt.Sprintf("DAC vendorId %s does not match PAI vendorId %s", idHex(dac.vendorID), idHex(pai.vendorID)))
	}

	// Step 6: the attestation elements decode and carry our nonce
	// (DAV:188-199).
	elements, err := decodeAttestationElements(data.Elements)
	if err != nil {
		return Result{}, &Error{Check: AttestationElementsUnparseable, Message: "Device returned attestation elements that cannot be decoded: " + err.Error(), Err: err}
	}
	if !bytes.Equal(elements.nonce, data.Nonce) {
		return Result{}, fail(AttestationNonceMismatch, "AttestationNonce in response does not match the nonce sent to the device")
	}

	// Step 7: the attestation signature over elements ‖ challenge verifies
	// with the DAC key (DAV:201-218); the signature is the raw r‖s.
	if !verifyRaw(dacPublicKey, slices.Concat(data.Elements, ctx.Challenge), data.Signature) {
		return Result{}, fail(AttestationSignatureInvalid, "Attestation signature verification failed against DAC public key")
	}

	// === DCL-dependent checks (only with a store), DAV:220 ===

	var paa *attCert
	if ctx.Store == nil {
		// DAV:224-231.
		findings = append(findings, Finding{
			Level: LevelWarning, Check: DclServiceUnavailable,
			Message: "DclCertificateService not available; skipping PAA trust store, chain, and revocation checks. " +
				"Register DclCertificateService in the environment for full attestation.",
		})
	} else {
		if paa, err = checkChain(ctx.Store, dac, pai, &findings); err != nil {
			return Result{}, err
		}
	}

	// === Remaining local checks (CD validation), DAV:366 ===

	// Step 8: the Certification Declaration parses (DAV:368-374) and its
	// signer verifies it (DAV:376-400).
	cd, err := parseDeclaration(elements.declaration)
	if err != nil {
		return Result{}, &Error{Check: CertificationDeclarationUnparseable, Message: "Device returned a Certification Declaration that cannot be parsed: " + err.Error(), Err: err}
	}
	cdSignerSkidHex := hex.EncodeToString(cd.signerSubjectKeyID)
	if ctx.Store != nil {
		if signer, ok := ctx.Store.CDSigner(cd.signerSubjectKeyID); ok {
			digest := sha256.Sum256(cd.signedData)
			if !ecdsa.VerifyASN1(signer, digest[:], cd.signature) {
				return Result{}, fail(CertificationDeclarationSignatureInvalid, "Certification Declaration signature verification failed")
			}
		} else {
			findings = append(findings, Finding{
				Level: LevelWarning, Check: CdSignerVerificationSkipped,
				Message: "CD signer SKID " + cdSignerSkidHex + " not found in trust store or DCL; skipping CD signature verification",
			})
		}
	} else {
		findings = append(findings, Finding{
			Level: LevelWarning, Check: CdSignerVerificationSkipped,
			Message: "No DclCertificateService provided; skipping CD signature verification",
		})
	}

	// Step 9: the CD's fields (DAV:402-526).
	if err := checkDeclaration(cd.content, data, dac, pai, paa, &findings); err != nil {
		return Result{}, err
	}

	// Step 10, firmware information: a TODO upstream (DAV:528-534), as is
	// the certificate_id check (DAV:421-422).
	return Result{DACPublicKey: dacPublicKey, Findings: findings}, nil
}

// checkChain is the store branch, DAV:232-364: the PAA, the chain, the
// validity at the DAC's notBefore, the PAA vendor id, revocation. It
// returns the PAA.
func checkChain(store PAAStore, dac, pai *attCert, findings *[]Finding) (*attCert, error) {
	// Step 2: the PAA by the PAI's authority key identifier, test roots
	// considered (DAV:233-241).
	paiAkid := pai.x509.AuthorityKeyId
	meta, ok := store.PAA(paiAkid)
	if !ok {
		return nil, fail(PaaNotTrusted, "PAA not found in trust store for authority key identifier "+hex.EncodeToString(paiAkid))
	}
	// A test root under a production policy (DAV:243-251).
	if !meta.Production && !store.AcceptsTestCertificates() {
		*findings = append(*findings, Finding{
			Level: LevelError, Check: TrustedAsTestCertificate,
			Message: "PAA " + hex.EncodeToString(paiAkid) + " is a test/development certificate; " +
				"device presented a valid test attestation but production trust policy is in effect",
		})
	}
	// Step 2b: the root fetched after the DAC's notBefore (DAV:253-266).
	if !meta.FetchedAt.IsZero() {
		dacNotBefore := time.Unix(dac.notBefore+matterEpochOffset, 0)
		if meta.FetchedAt.After(dacNotBefore) {
			*findings = append(*findings, Finding{
				Level: LevelInfo, Check: PaaTrustStoreTimeMismatch,
				Message: "PAA was fetched at " + meta.FetchedAt.UTC().Format(time.RFC3339) +
					" which is after DAC notBefore " + dacNotBefore.UTC().Format(time.RFC3339) +
					"; cannot confirm PAA was in DCL when DAC was issued",
			})
		}
	}

	// Step 3: the chain's signatures (DAV:268-303). A trust-store PAA that
	// does not parse is not the peer's fault: a plain error (DAV:270).
	paa, err := parseAttCert(meta.DER, false)
	if err != nil {
		return nil, fmt.Errorf("attestation: trust store PAA: %w", err)
	}
	// The PAA key is read inside matter.js's try (DAV:272-279), so a key
	// it cannot use fails the chain as the signature does.
	paaKey, err := P256Key(paa.x509)
	if err == nil {
		err = VerifySignedBy(pai.x509, paaKey)
	}
	if err != nil {
		return nil, fail(CertificateChainInvalid, "PAI signature verification failed: "+err.Error())
	}
	// Reading the PAI key apart from the DAC check (DAV:281-286).
	paiKey, err := P256Key(pai.x509)
	if err != nil {
		return nil, &Error{Check: CertificateUnparseable, Message: "Device returned a PAI whose public key cannot be read: " + err.Error(), Err: err}
	}
	// mayIssue (DAV:288-294; kinds/CertificateSignature.ts:46-54): a PAI
	// key no stronger than its PAA's. Both are EC P-256 here — the one
	// algorithm this validator admits — so the strengths are equal.
	// The DAC's signature by the PAI (DAV:296-303).
	if err := VerifySignedBy(dac.x509, paiKey); err != nil {
		return nil, fail(CertificateChainInvalid, "DAC signature verification failed: "+err.Error())
	}

	// Step 3b: validity at the DAC's notBefore; notAfter 0 is none
	// (DAV:305-331).
	nb := dac.notBefore
	switch {
	case nb < pai.notBefore:
		return nil, fail(CertificateExpired, fmt.Sprintf("DAC notBefore (%d) is before PAI notBefore (%d)", nb, pai.notBefore))
	case pai.notAfter != 0 && nb > pai.notAfter:
		return nil, fail(CertificateExpired, fmt.Sprintf("DAC notBefore (%d) is after PAI notAfter (%d)", nb, pai.notAfter))
	case nb < paa.notBefore:
		return nil, fail(CertificateExpired, fmt.Sprintf("DAC notBefore (%d) is before PAA notBefore (%d)", nb, paa.notBefore))
	case paa.notAfter != 0 && nb > paa.notAfter:
		return nil, fail(CertificateExpired, fmt.Sprintf("DAC notBefore (%d) is after PAA notAfter (%d)", nb, paa.notAfter))
	}

	// Step 4 (DCL part): a PAA vendor id, when present, is the PAI's
	// (DAV:333-340).
	if paa.vendorID != nil && !sameID(paa.vendorID, pai.vendorID) {
		return nil, fail(VendorIDMismatch, fmt.Sprintf("PAA vendorId %s does not match PAI vendorId %s", idHex(paa.vendorID), idHex(pai.vendorID)))
	}

	// Step 5: revocation of the DAC, then the PAI, keyed by (AKID, serial,
	// issuer DN) (DAV:342-363). A store without Revocations revokes
	// nothing.
	if r, ok := store.(Revocations); ok {
		revoked, err := r.IsRevoked(dac.x509.AuthorityKeyId, serialBytes(dac.x509.SerialNumber), strings.ToUpper(hex.EncodeToString(dac.x509.RawIssuer)))
		if err != nil {
			return nil, fmt.Errorf("attestation: revocation check: %w", err)
		}
		if revoked {
			return nil, fail(CertificateRevoked, "Device Attestation Certificate has been revoked")
		}
		revoked, err = r.IsRevoked(paiAkid, serialBytes(pai.x509.SerialNumber), strings.ToUpper(hex.EncodeToString(pai.x509.RawIssuer)))
		if err != nil {
			return nil, fmt.Errorf("attestation: revocation check: %w", err)
		}
		if revoked {
			return nil, fail(CertificateRevoked, "Product Attestation Intermediate certificate has been revoked")
		}
	}
	return paa, nil
}

// checkDeclaration is Step 9, the CD's fields (DAV:402-526).
func checkDeclaration(cd cdContent, data Data, dac, pai, paa *attCert, findings *[]Finding) error {
	// certification_type: provisional is a warning, test an info
	// (DAV:405-419).
	switch cd.certificationType {
	case CertificationProvisional:
		*findings = append(*findings, Finding{Level: LevelWarning, Check: CertificationTypeProvisional, Message: "Certification Declaration has certification_type=1 (provisional)"})
	case CertificationTest:
		*findings = append(*findings, Finding{Level: LevelInfo, Check: CertificationTypeTest, Message: "Certification Declaration has certification_type=0 (test)"})
	case CertificationOfficial:
	}

	// vendor_id SHALL match BasicInformation VendorID (DAV:424-430).
	if cd.vendorID != data.VendorID {
		return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("CD vendor_id %s does not match BasicInformation VendorID %s", idHex(&cd.vendorID), idHex(&data.VendorID)))
	}
	// product_id_array SHALL contain BasicInformation ProductID (DAV:432-438).
	if !slices.Contains(cd.productIDs, data.ProductID) {
		return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("CD product_id_array %s does not contain BasicInformation ProductID %s", idList(cd.productIDs), idHex(&data.ProductID)))
	}
	// Both or neither of dac_origin_vendor_id and dac_origin_product_id
	// (DAV:440-448).
	hasOriginVID, hasOriginPID := cd.dacOriginVendorID != nil, cd.dacOriginProductID != nil
	if hasOriginVID != hasOriginPID {
		return fail(CertificationDeclarationFieldMismatch, "CD has only one of dac_origin_vendor_id and dac_origin_product_id")
	}
	if hasOriginVID {
		// Both: against the dac_origin values (DAV:450-477).
		switch {
		case !sameID(dac.vendorID, cd.dacOriginVendorID):
			return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("DAC vendorId %s does not match CD dac_origin_vendor_id %s", idHex(dac.vendorID), idHex(cd.dacOriginVendorID)))
		case !sameID(pai.vendorID, cd.dacOriginVendorID):
			return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("PAI vendorId %s does not match CD dac_origin_vendor_id %s", idHex(pai.vendorID), idHex(cd.dacOriginVendorID)))
		case dac.productID != nil && *dac.productID != *cd.dacOriginProductID:
			return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("DAC productId %s does not match CD dac_origin_product_id %s", idHex(dac.productID), idHex(cd.dacOriginProductID)))
		case pai.productID != nil && *pai.productID != *cd.dacOriginProductID:
			return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("PAI productId %s does not match CD dac_origin_product_id %s", idHex(pai.productID), idHex(cd.dacOriginProductID)))
		}
	} else {
		// Neither: against vendor_id and product_id_array (DAV:478-505).
		switch {
		case !sameID(dac.vendorID, &cd.vendorID):
			return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("DAC vendorId %s does not match CD vendor_id %s", idHex(dac.vendorID), idHex(&cd.vendorID)))
		case !sameID(pai.vendorID, &cd.vendorID):
			return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("PAI vendorId %s does not match CD vendor_id %s", idHex(pai.vendorID), idHex(&cd.vendorID)))
		case dac.productID != nil && !slices.Contains(cd.productIDs, *dac.productID):
			return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("DAC productId %s not found in CD product_id_array %s", idHex(dac.productID), idList(cd.productIDs)))
		case pai.productID != nil && !slices.Contains(cd.productIDs, *pai.productID):
			return fail(CertificationDeclarationFieldMismatch, fmt.Sprintf("PAI productId %s not found in CD product_id_array %s", idHex(pai.productID), idList(cd.productIDs)))
		}
	}

	// authorized_paa_list, which needs the PAA (DAV:508-526).
	if cd.authorizedPAAList != nil {
		if paa != nil {
			ski := paa.x509.SubjectKeyId
			if !slices.ContainsFunc(cd.authorizedPAAList, func(e []byte) bool { return bytes.Equal(e, ski) }) {
				return fail(CertificationDeclarationFieldMismatch, "PAA subject key identifier not in CD authorized_paa_list")
			}
		} else {
			*findings = append(*findings, Finding{Level: LevelWarning, Check: DclServiceUnavailable, Message: "CD has authorized_paa_list but cannot verify without DCL service"})
		}
	}
	return nil
}

// parsePeerCertificate is DAV:93-106: an empty certificate, or one that
// does not parse, is CertificateUnparseable.
func parsePeerCertificate(kind string, der []byte, authorityKeyID bool) (*attCert, error) {
	if len(der) == 0 {
		return nil, fail(CertificateUnparseable, "Device returned an empty "+kind+" certificate")
	}
	c, err := parseAttCert(der, authorityKeyID)
	if err != nil {
		return nil, &Error{Check: CertificateUnparseable, Message: "Device returned a " + kind + " certificate that cannot be parsed: " + err.Error(), Err: err}
	}
	return c, nil
}

// verifyRaw verifies a raw 64-byte r‖s ECDSA signature over SHA-256 of
// msg — matter.js crypto.verifyEcdsa with an EcdsaSignature in its
// default (IEEE P1363) form.
func verifyRaw(key *ecdsa.PublicKey, msg, sig []byte) bool {
	if len(sig) != 64 {
		return false
	}
	digest := sha256.Sum256(msg)
	return ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}

// sameID compares two optional ids as matter.js's !== on numbers or
// undefined does.
func sameID(a, b *uint16) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// idHex is DAV:108-110.
func idHex(id *uint16) string {
	if id == nil {
		return "undefined"
	}
	return fmt.Sprintf("0x%04X", *id)
}

func idList(ids []uint16) string {
	parts := make([]string, len(ids))
	for i := range ids {
		parts[i] = idHex(&ids[i])
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// serialBytes is the certificate's serialNumber as matter.js keeps it: the
// DER INTEGER content (Certificate.ts:650).
func serialBytes(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) == 0 || b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return b
}
