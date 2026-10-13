// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package commissioner

// The OperationalCredentials commands of the flow. ADR 0013 generates no
// definition for the cluster, so its ids and payloads are written here
// from the matter.js model at 0ceb202d as parity/schema.json carries it
// (OperationalCredentials, cluster 0x003E: the commands 0x00-0x0B and
// NodeOperationalCertStatusEnum); cluster/core/operational_credentials.go
// is the device's side of the same wire shapes.

import (
	"math"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// OperationalCredentials ids.
const (
	opCredsCluster                 uint32 = 0x003E
	opCredsAttrSupportedFabrics    uint32 = 0x0002
	opCredsAttrCommissionedFabrics uint32 = 0x0003
	cmdAttestationRequest          uint32 = 0x00
	cmdCertificateChainRequest     uint32 = 0x02
	cmdCSRRequest                  uint32 = 0x04
	cmdAddNOC                      uint32 = 0x06
	cmdUpdateFabricLabel           uint32 = 0x09
	cmdAddTrustedRootCertificate   uint32 = 0x0B
)

// CertificateChainTypeEnum (DacCertificate 1, PaiCertificate 2).
const (
	certificateTypeDAC uint8 = 1
	certificateTypePAI uint8 = 2
)

// NodeOperationalCertStatusEnum.
const (
	nocStatusOk             uint8 = 0
	nocStatusTableFull      uint8 = 5
	nocStatusFabricConflict uint8 = 9
	nocStatusLabelConflict  uint8 = 10
)

// nocStatusNames are the NodeOperationalCertStatusEnum names matter.js
// prints (ControllerCommissioningFlow.ts:679, :705).
var nocStatusNames = map[uint8]string{
	0: "Ok", 1: "InvalidPublicKey", 2: "InvalidNodeOpId", 3: "InvalidNoc", 4: "MissingCsr",
	5: "TableFull", 6: "InvalidAdminSubject", 9: "FabricConflict", 10: "LabelConflict", 11: "InvalidFabricIndex",
}

// octetsRequest is a request whose only field is the octet string [0]
// (AttestationRequest, CsrRequest, AddTrustedRootCertificate).
type octetsRequest []byte

func (r octetsRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutOctets(tlv.ContextTag(0), r)
	_ = enc.EndContainer()
}

// certificateChainRequest is CertificateChainRequest {[0] CertificateType}.
type certificateChainRequest uint8

func (r certificateChainRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUint(tlv.ContextTag(0), uint64(r))
	_ = enc.EndContainer()
}

// addNOCRequest is AddNoc {[0] NocValue, [1] IcacValue, [2] IpkValue, [3]
// CaseAdminSubject, [4] AdminVendorId}. IcacValue is always sent, empty
// without an ICAC (ControllerCommissioningFlow.ts:1448).
type addNOCRequest struct {
	noc, icac, ipk   []byte
	caseAdminSubject uint64
	adminVendorID    uint16
}

func (r addNOCRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutOctets(tlv.ContextTag(0), r.noc)
	enc.PutOctets(tlv.ContextTag(1), r.icac)
	enc.PutOctets(tlv.ContextTag(2), r.ipk)
	enc.PutUint(tlv.ContextTag(3), r.caseAdminSubject)
	enc.PutUint(tlv.ContextTag(4), uint64(r.adminVendorID))
	_ = enc.EndContainer()
}

// updateFabricLabelRequest is UpdateFabricLabel {[0] Label}.
type updateFabricLabelRequest string

func (r updateFabricLabelRequest) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUTF8(tlv.ContextTag(0), string(r))
	_ = enc.EndContainer()
}

// certificateChainResponse is CertificateChainResponse {[0] Certificate,
// max 600}.
func decodeCertificateChainResponse(n spec.Node) ([]byte, error) {
	var cert []byte
	err := spec.DecodeFields(n, "CertificateChainResponse",
		spec.Bind(0, "Certificate", true, &cert, spec.DecodeBytesIn(0, 600)))
	return cert, err
}

// signedElements is AttestationResponse {[0] AttestationElements, [1]
// AttestationSignature} or CsrResponse {[0] NocsrElements, [1]
// AttestationSignature}: elements of at most 900 bytes and a 64-byte
// signature.
type signedElements struct {
	elements, signature []byte
}

func decodeSignedElements(n spec.Node, what string) (signedElements, error) {
	var s signedElements
	err := spec.DecodeFields(n, what,
		spec.Bind(0, "Elements", true, &s.elements, spec.DecodeBytesIn(0, 900)),
		spec.Bind(1, "AttestationSignature", true, &s.signature, spec.DecodeBytesIn(0, 64)))
	return s, err
}

// nocResponse is NocResponse {[0] StatusCode, [1] FabricIndex, [2]
// DebugText}.
type nocResponse struct {
	status      uint8
	fabricIndex *uint8
	debugText   *string
}

func decodeNOCResponse(n spec.Node) (nocResponse, error) {
	var r nocResponse
	err := spec.DecodeFields(n, "NocResponse",
		spec.Bind(0, "StatusCode", true, &r.status, spec.DecodeUint[uint8](8)),
		spec.Bind(1, "FabricIndex", false, &r.fabricIndex, spec.DecodeOptional(spec.DecodeUintIn[uint8](8, 1, 254))),
		spec.Bind(2, "DebugText", false, &r.debugText, spec.DecodeOptional(spec.DecodeStringIn(0, 128))))
	return r, err
}

// certSigningRequest is TlvCertSigningRequest
// (common/OperationalCredentialsTypes.ts:18-24): [1] certSigningRequest,
// [2] csrNonce (32 bytes), [3]-[5] vendor reserved.
func decodeCertSigningRequest(b []byte) ([]byte, error) {
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, err
	}
	var csr, nonce []byte
	err = spec.DecodeFields(n, "CertSigningRequest",
		spec.Bind(1, "certSigningRequest", true, &csr, spec.DecodeBytesIn(0, math.MaxInt)),
		spec.Bind(2, "csrNonce", true, &nonce, spec.DecodeBytesIn(32, 32)))
	return csr, err
}
