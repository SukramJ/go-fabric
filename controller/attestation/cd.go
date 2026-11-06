// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// The Certification Declaration as the validator reads it: the CMS
// SignedData walk with its encoding rules of matter.js
// packages/protocol/src/certificate/kinds/CertificationDeclaration.ts:
// 58-206 (CertificationDeclaration.parse) at db021da0 and the TLV content
// of kinds/definitions/certification-declaration.ts:31-48 (TlvDc) at
// db021da0. secure/attestation/cd.go:62-135 is the device's encoder of the
// same structure.

import (
	"bytes"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// CertificationType is the CD's certification_type —
// certification-declaration.ts:20-28.
type CertificationType uint8

// The certification types.
const (
	CertificationTest        CertificationType = 0
	CertificationProvisional CertificationType = 1
	CertificationOfficial    CertificationType = 2
)

// cdContent is TlvDc (certification-declaration.ts:32-48).
type cdContent struct {
	formatVersion       uint16
	vendorID            uint16
	productIDs          []uint16
	deviceTypeID        uint32
	certificateID       string
	securityLevel       uint8
	securityInformation uint16
	versionNumber       uint16
	certificationType   CertificationType
	dacOriginVendorID   *uint16
	dacOriginProductID  *uint16
	authorizedPAAList   [][]byte // nil when absent
}

// declaration is what CertificationDeclaration.parse returns.
type declaration struct {
	content            cdContent
	signerSubjectKeyID []byte
	signature          []byte // DER ECDSA-Sig-Value
	signedData         []byte // the eContent, the signed bytes
}

// derNode is one DER element, its content and its children when it is
// constructed — matter.js DerNode as DerCodec.decode yields it.
type derNode struct {
	tag      byte // the identifier octet (class | constructed | number)
	bytes    []byte
	elements []derNode
}

// decodeDER decodes one DER element and, for a constructed one, its
// children.
func decodeDER(b []byte) (derNode, error) {
	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(b, &raw); err != nil {
		return derNode{}, err
	}
	return nodeOf(raw)
}

func nodeOf(raw asn1.RawValue) (derNode, error) {
	// The identifier octet as matter.js's DerNode._tag holds it (class,
	// constructed bit, tag number).
	n := derNode{tag: raw.FullBytes[0], bytes: raw.Bytes}
	if raw.IsCompound {
		rest := raw.Bytes
		for len(rest) > 0 {
			var child asn1.RawValue
			var err error
			if rest, err = asn1.Unmarshal(rest, &child); err != nil {
				return derNode{}, err
			}
			c, err := nodeOf(child)
			if err != nil {
				return derNode{}, err
			}
			n.elements = append(n.elements, c)
		}
	}
	return n, nil
}

// cdFormatError is matter.js CertificationDeclarationFormatError
// (CertificationDeclaration.ts:58-59 at db021da0): the CD breaks an
// encoding rule of Matter Core §6.3.1. The validator reports it as
// CertificationDeclarationFormatInvalid, any other parse failure as
// CertificationDeclarationUnparseable (DeviceAttestationValidator.ts:
// 138-158 at db021da0).
type cdFormatError struct{ msg string }

func (e *cdFormatError) Error() string { return e.msg }

// The CMS constants of a CD (CertificationDeclaration.ts:61-64 at
// db021da0) and the algorithm OIDs it requires: sha256
// (2.16.840.1.101.3.4.2.1) and ecdsa-with-SHA256 (1.2.840.10045.4.3.2).
const (
	cmsVersion        = 3
	signerInfoVersion = 3
	// signerSubjectKeyIDLength is SIGNER_SUBJECT_KEY_ID_LENGTH, a key
	// identifier of 20 octets (:261-266 at db021da0).
	signerSubjectKeyIDLength = 20
)

var (
	oidPKCS7SignedData = []byte{0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x07, 0x02}
	oidPKCS7Data       = []byte{0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x07, 0x01}
	oidSHA256          = []byte{0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01}
	oidECDSAWithSHA256 = []byte{0x2a, 0x86, 0x48, 0xce, 0x3d, 0x04, 0x03, 0x02}
)

// The DER identifier octets of the CMS walk.
const (
	derOIDTag      = 0x06
	derSequenceTag = 0x30
	derSetTag      = 0x31
)

// requireOID is requireOid (CertificationDeclaration.ts:66-71 at db021da0).
func requireOID(n *derNode, want []byte, rule string) error {
	if n == nil || n.tag != derOIDTag || !bytes.Equal(n.bytes, want) {
		found := "none"
		if n != nil && n.tag == derOIDTag {
			found = hex.EncodeToString(n.bytes)
		}
		return &cdFormatError{msg: rule + ", found OID " + found}
	}
	return nil
}

// requireAlgorithm is requireAlgorithm (:73-76 at db021da0): an
// AlgorithmIdentifier SEQUENCE by its OID; the parameters are not
// inspected.
func requireAlgorithm(n *derNode, want []byte, rule string) error {
	var oid *derNode
	if n != nil && n.tag == derSequenceTag && len(n.elements) > 0 {
		oid = &n.elements[0]
	}
	return requireOID(oid, want, rule)
}

// requireVersion is requireVersion (:78-83 at db021da0): an INTEGER whose
// content is the one byte want.
func requireVersion(n *derNode, want byte, rule string) error {
	if n.tag != derIntegerTag || !bytes.Equal(n.bytes, []byte{want}) {
		found := "none"
		if n.tag == derIntegerTag {
			found = "0x" + hex.EncodeToString(n.bytes)
		}
		return &cdFormatError{msg: rule + ", found " + found}
	}
	return nil
}

// parseDeclaration is CertificationDeclaration.parse (:108-206 at
// db021da0): the ContentInfo, the [0] SignedData, its encapContentInfo's
// [0] OCTET STRING eContent, the first SignerInfo's [0]
// subjectKeyIdentifier and OCTET STRING signature, then the TLV content.
// A broken CMS rule — contentType pkcs7-signedData, CMS version 3, at
// least one digest algorithm and every one sha256, eContentType
// pkcs7-data, SignerInfo version 3, a 20-byte subjectKeyIdentifier,
// SignerInfo digest algorithm sha256, signature algorithm
// ecdsa-with-SHA256 — is a [*cdFormatError].
func parseDeclaration(b []byte) (*declaration, error) {
	contentInfo, err := decodeDER(b)
	if err != nil {
		return nil, err
	}
	if len(contentInfo.elements) < 2 {
		return nil, errors.New("attestation: invalid CMS ContentInfo structure")
	}
	if err := requireOID(&contentInfo.elements[0], oidPKCS7SignedData, "CMS contentType must be pkcs7-signedData"); err != nil {
		return nil, err
	}
	wrapper := contentInfo.elements[1]
	if wrapper.tag != 0xa0 || len(wrapper.elements) < 1 {
		return nil, errors.New("attestation: invalid CMS SignedData wrapper")
	}
	signedData := wrapper.elements[0].elements
	if len(signedData) < 4 {
		return nil, errors.New("attestation: invalid CMS SignedData structure")
	}
	if err := requireVersion(&signedData[0], cmsVersion, "CMS version must be 3"); err != nil {
		return nil, err
	}
	digestAlgorithms := signedData[1]
	if digestAlgorithms.tag != derSetTag || len(digestAlgorithms.elements) == 0 {
		return nil, &cdFormatError{msg: "CMS digestAlgorithms must hold sha256, found none"}
	}
	for i := range digestAlgorithms.elements {
		if err := requireAlgorithm(&digestAlgorithms.elements[i], oidSHA256, "CMS digestAlgorithm must be sha256"); err != nil {
			return nil, err
		}
	}
	encap := signedData[2].elements
	if len(encap) < 2 {
		return nil, errors.New("attestation: invalid encapContentInfo structure")
	}
	if err := requireOID(&encap[0], oidPKCS7Data, "CMS eContentType must be pkcs7-data"); err != nil {
		return nil, err
	}
	eContentWrapper := encap[1]
	if eContentWrapper.tag != 0xa0 || len(eContentWrapper.elements) < 1 {
		return nil, errors.New("attestation: invalid eContent wrapper in encapContentInfo")
	}
	if eContentWrapper.elements[0].tag != derOctetStringTag {
		return nil, errors.New("attestation: expected OCTET STRING eContent in encapContentInfo")
	}
	eContent := eContentWrapper.elements[0].bytes
	signerInfos := signedData[3].elements
	if len(signerInfos) < 1 {
		return nil, errors.New("attestation: no SignerInfo found in SignedData")
	}
	signerInfo := signerInfos[0].elements
	if len(signerInfo) < 5 {
		return nil, errors.New("attestation: invalid SignerInfo structure")
	}
	if err := requireVersion(&signerInfo[0], signerInfoVersion, "CMS SignerInfo version must be 3"); err != nil {
		return nil, err
	}
	if signerInfo[1].tag != 0x80 {
		return nil, errors.New("attestation: expected SubjectKeyIdentifier context tag [0] in SignerInfo")
	}
	if n := len(signerInfo[1].bytes); n != signerSubjectKeyIDLength {
		return nil, &cdFormatError{msg: fmt.Sprintf("CMS SignerInfo subjectKeyIdentifier must be %d bytes, found %d", signerSubjectKeyIDLength, n)}
	}
	if err := requireAlgorithm(&signerInfo[2], oidSHA256, "CMS SignerInfo digestAlgorithm must be sha256"); err != nil {
		return nil, err
	}
	if err := requireAlgorithm(&signerInfo[3], oidECDSAWithSHA256, "CMS SignerInfo signatureAlgorithm must be ecdsa-with-SHA256"); err != nil {
		return nil, err
	}
	if signerInfo[4].tag != derOctetStringTag {
		return nil, errors.New("attestation: expected OCTET STRING signature in SignerInfo")
	}
	content, err := decodeCDContent(eContent)
	if err != nil {
		return nil, err
	}
	return &declaration{
		content:            content,
		signerSubjectKeyID: signerInfo[1].bytes,
		signature:          signerInfo[4].bytes,
		signedData:         eContent,
	}, nil
}

// decodeCDContent is TlvDc.decode with its bounds (Schema.decode validates,
// packages/types/src/schema/Schema.ts:18-21).
func decodeCDContent(b []byte) (cdContent, error) {
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return cdContent{}, err
	}
	var c cdContent
	var origVID, origPID *uint16
	var paas *[][]byte
	var certType uint8
	err = spec.DecodeFields(n, "CertificationDeclaration",
		spec.Bind(0, "formatVersion", true, &c.formatVersion, spec.DecodeUint[uint16](16)),
		spec.Bind(1, "vendorId", true, &c.vendorID, spec.DecodeUint[uint16](16)),
		spec.Bind(2, "produceIdArray", true, &c.productIDs, spec.DecodeListIn(spec.DecodeUint[uint16](16), 1, 100)),
		spec.Bind(3, "deviceTypeId", true, &c.deviceTypeID, spec.DecodeUint[uint32](32)),
		spec.Bind(4, "certificateId", true, &c.certificateID, spec.DecodeStringIn(19, 19)),
		spec.Bind(5, "securityLevel", true, &c.securityLevel, spec.DecodeUint[uint8](8)),
		spec.Bind(6, "securityInformation", true, &c.securityInformation, spec.DecodeUint[uint16](16)),
		spec.Bind(7, "versionNumber", true, &c.versionNumber, spec.DecodeUint[uint16](16)),
		spec.Bind(8, "certificationType", true, &certType, spec.DecodeUint[uint8](8)),
		spec.Bind(9, "dacOriginVendorId", false, &origVID, spec.DecodeOptional(spec.DecodeUint[uint16](16))),
		spec.Bind(10, "dacOriginProductId", false, &origPID, spec.DecodeOptional(spec.DecodeUint[uint16](16))),
		spec.Bind(11, "authorizedPaaList", false, &paas, spec.DecodeOptional(spec.DecodeListIn(spec.DecodeBytesIn(20, 20), 1, 10))),
	)
	if err != nil {
		return cdContent{}, err
	}
	c.certificationType = CertificationType(certType)
	c.dacOriginVendorID, c.dacOriginProductID = origVID, origPID
	if paas != nil {
		c.authorizedPAAList = *paas
	}
	return c, nil
}

// attestationElements is TlvAttestation (common/OperationalCredentialsTypes.ts:
// 10-15): declaration [1], attestationNonce [2] (32 bytes), timestamp [3],
// firmwareInfo [4] optional.
type attestationElements struct {
	declaration  []byte
	nonce        []byte
	timestamp    uint32
	firmwareInfo *[]byte
}

func decodeAttestationElements(b []byte) (attestationElements, error) {
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return attestationElements{}, err
	}
	var a attestationElements
	err = spec.DecodeFields(n, "AttestationElements",
		spec.Bind(1, "declaration", true, &a.declaration, spec.DecodeBytesIn(0, math.MaxInt)),
		spec.Bind(2, "attestationNonce", true, &a.nonce, spec.DecodeBytesIn(32, 32)),
		spec.Bind(3, "timestamp", true, &a.timestamp, spec.DecodeUint[uint32](32)),
		spec.Bind(4, "firmwareInfo", false, &a.firmwareInfo, spec.DecodeOptional(spec.DecodeBytesIn(0, math.MaxInt))),
	)
	return a, err
}
