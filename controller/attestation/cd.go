// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package attestation

// The Certification Declaration as the validator reads it: the CMS
// SignedData walk of matter.js packages/protocol/src/certificate/kinds/
// CertificationDeclaration.ts:68-159 (CertificationDeclaration.parse) and
// the TLV content of kinds/definitions/certification-declaration.ts:31-48
// (TlvDc) at 0ceb202d. secure/attestation/cd.go:62-135 is the device's
// encoder of the same structure.

import (
	"encoding/asn1"
	"errors"
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

// parseDeclaration is CertificationDeclaration.parse (:68-159): the
// ContentInfo, the [0] SignedData, its encapContentInfo's [0] OCTET STRING
// eContent, the first SignerInfo's [0] subjectKeyIdentifier and signature,
// then the TLV content.
func parseDeclaration(b []byte) (*declaration, error) {
	contentInfo, err := decodeDER(b)
	if err != nil {
		return nil, err
	}
	if len(contentInfo.elements) < 2 {
		return nil, errors.New("invalid CMS ContentInfo structure")
	}
	wrapper := contentInfo.elements[1]
	if wrapper.tag != 0xa0 || len(wrapper.elements) < 1 {
		return nil, errors.New("invalid CMS SignedData wrapper")
	}
	signedData := wrapper.elements[0].elements
	if len(signedData) < 4 {
		return nil, errors.New("invalid CMS SignedData structure")
	}
	encap := signedData[2].elements
	if len(encap) < 2 {
		return nil, errors.New("invalid encapContentInfo structure")
	}
	eContentWrapper := encap[1]
	if eContentWrapper.tag != 0xa0 || len(eContentWrapper.elements) < 1 {
		return nil, errors.New("invalid eContent wrapper in encapContentInfo")
	}
	eContent := eContentWrapper.elements[0].bytes
	signerInfos := signedData[3].elements
	if len(signerInfos) < 1 {
		return nil, errors.New("no SignerInfo found in SignedData")
	}
	signerInfo := signerInfos[0].elements
	if len(signerInfo) < 5 {
		return nil, errors.New("invalid SignerInfo structure")
	}
	if signerInfo[1].tag != 0x80 {
		return nil, errors.New("expected SubjectKeyIdentifier context tag [0] in SignerInfo")
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
