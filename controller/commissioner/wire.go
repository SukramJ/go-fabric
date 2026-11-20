// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// The OperationalCredentials ids, enums and payloads of the flow come from
// the generated definition (cluster/spec/operationalcredentials, ADR 0013,
// generated from parity/schema.json). What stays here is the decoding the
// flow needs beyond it: the attestation and CSR responses, whose signature
// is decoded at any length and judged downstream — the generated decoder
// requires exactly 64 at decode, which is the divergence these two avoid
// (decodeSignedElements) — and the CSR's TlvCertSigningRequest, which is
// not a cluster payload (review II, R2-P6-doc-1857).

import (
	"math"

	"github.com/SukramJ/go-fabric/cluster/spec"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/controller/attestation"
)

// opCredsCluster is the OperationalCredentials cluster id.
const opCredsCluster = opcredsdef.ClusterID

// The NodeOperationalCertStatusEnum values the flow acts on.
const (
	nocStatusOk             = uint8(opcredsdef.NodeOperationalCertStatusOk)
	nocStatusTableFull      = uint8(opcredsdef.NodeOperationalCertStatusTableFull)
	nocStatusFabricConflict = uint8(opcredsdef.NodeOperationalCertStatusFabricConflict)
	nocStatusLabelConflict  = uint8(opcredsdef.NodeOperationalCertStatusLabelConflict)
)

// nocStatusName is the NodeOperationalCertStatusEnum name of status the
// flow's messages print (ControllerCommissioningFlow.ts:679, :705), as
// [enumName] gives it: "undefined" for a value the enum does not name.
func nocStatusName(status uint8) string {
	return enumName(opcredsdef.NodeOperationalCertStatusEnumDef, uint64(status))
}

// addNOCRequest is the AddNoc payload; IcacValue is always sent, empty
// without an ICAC (ControllerCommissioningFlow.ts:1466).
func addNOCRequest(noc, icac, ipk []byte, caseAdminSubject uint64, adminVendorID uint16) opcredsdef.AddNocRequest {
	if icac == nil {
		icac = []byte{}
	}
	return opcredsdef.AddNocRequest{NocValue: noc, IcacValue: &icac, IpkValue: ipk, CaseAdminSubject: caseAdminSubject, AdminVendorId: adminVendorID}
}

// decodeCertificateChainResponse is the certificate of a
// CertificateChainResponse.
func decodeCertificateChainResponse(n spec.Node) ([]byte, error) {
	var r opcredsdef.CertificateChainResponse
	err := r.DecodeTLV(n)
	return r.Certificate, err
}

// signedElements is AttestationResponse {[0] AttestationElements, [1]
// AttestationSignature}: elements and a signature of any length, the
// message size their only bound. A command response is decoded without a
// check against its schema; the validator judges the lengths —
// AttestationElements over RESP_MAX is the attestation finding
// AttestationElementsTooLong
// (packages/protocol/src/certificate/DeviceAttestationValidator.ts:316-321
// at b25ed960).
type signedElements struct {
	elements, signature []byte
}

// decodeSignedElements enforces no signature length: a length other than
// 64 is judged downstream — in the AttestationResponse the error finding
// AttestationSignatureInvalid the policy judges, in the CSRResponse the
// CSR signature verification failure (an empty one stays unexpected
// data). Mirrors matter.js, whose client decodes the signature without
// validating its length
// (packages/protocol/src/action/client/ClientInteraction.ts:552, :561 at
// db021da0) and judges the length in EcdsaSignature
// (packages/general/src/crypto/EcdsaSignature.ts:39-40):
// DeviceAttestationValidator.ts:295-306 for the attestation,
// ControllerCommissioningFlow.ts:1419-1421 and :1424-1434 for the CSR.
// The generated opcredsdef.AttestationResponse and CsrResponse decode
// the signature at exactly 64 and so are not used here (review II,
// R2-P6-doc-1857 (b), owner decision 216).
func decodeSignedElements(n spec.Node, what string) (signedElements, error) {
	var s signedElements
	err := spec.DecodeFields(n, what,
		spec.Bind(0, "Elements", true, &s.elements, spec.DecodeBytesIn(0, math.MaxInt)),
		spec.Bind(1, "AttestationSignature", true, &s.signature, spec.DecodeBytesIn(0, math.MaxInt)))
	return s, err
}

// decodeCSRResponse decodes CsrResponse {[0] NocsrElements, [1]
// AttestationSignature} without a check against its schema: a field the
// device left out is marked absent and any length decodes, for
// attestation.ValidateNOCSR to judge (FieldMissing, ResponseEmpty,
// ElementsTooLong). Mirrors matter.js, which hands the decoded response to
// NocsrValidator.validate as a Partial<CsrResponse>
// (packages/protocol/src/certificate/NocsrValidator.ts:63-64, :82-98 at
// b25ed960).
func decodeCSRResponse(n spec.Node) (attestation.NOCSRResponse, error) {
	var elements, signature *[]byte
	err := spec.DecodeFields(n, "CsrResponse",
		spec.Bind(0, "NOCSRElements", false, &elements, spec.DecodeOptional(spec.DecodeBytes)),
		spec.Bind(1, "AttestationSignature", false, &signature, spec.DecodeOptional(spec.DecodeBytes)))
	if err != nil {
		return attestation.NOCSRResponse{}, err
	}
	r := attestation.NOCSRResponse{ElementsAbsent: elements == nil, SignatureAbsent: signature == nil}
	if elements != nil {
		r.Elements = *elements
	}
	if signature != nil {
		r.Signature = *signature
	}
	return r, nil
}

// nocResponse is NocResponse {[0] StatusCode, [1] FabricIndex, [2]
// DebugText}.
type nocResponse struct {
	status      uint8
	fabricIndex *uint8
	debugText   *string
}

func decodeNOCResponse(n spec.Node) (nocResponse, error) {
	var g opcredsdef.NocResponse
	if err := g.DecodeTLV(n); err != nil {
		return nocResponse{}, err
	}
	return nocResponse{status: uint8(g.StatusCode), fabricIndex: g.FabricIndex, debugText: g.DebugText}, nil
}
