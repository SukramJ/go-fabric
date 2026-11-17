// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// The OperationalCredentials ids, enums and payloads of the flow come from
// the generated definition (cluster/spec/operationalcredentials, ADR 0013,
// generated from parity/schema.json). What stays here is the decoding the
// flow needs beyond it: the attestation and CSR responses, whose signature
// the flow hands to the attestation check at any length up to 64 bytes
// (the generated decoder requires exactly 64), and the CSR's
// TlvCertSigningRequest, which is not a cluster payload (review II,
// R2-P6-doc-1857).

import (
	"math"

	"github.com/SukramJ/go-fabric/cluster/spec"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/tlv"
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
// without an ICAC (ControllerCommissioningFlow.ts:1448).
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
// AttestationSignature} or CsrResponse {[0] NocsrElements, [1]
// AttestationSignature}: elements of at most 900 bytes and a signature
// of at most 64, whose length the attestation check judges.
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
	var g opcredsdef.NocResponse
	if err := g.DecodeTLV(n); err != nil {
		return nocResponse{}, err
	}
	return nocResponse{status: uint8(g.StatusCode), fabricIndex: g.FabricIndex, debugText: g.DebugText}, nil
}

// certSigningRequest is TlvCertSigningRequest
// (common/OperationalCredentialsTypes.ts:18-24): [1] certSigningRequest,
// [2] csrNonce (32 bytes), [3]-[5] vendor reserved. The nonce is returned
// so the flow can compare it with the one it sent (connectedhomeip
// DefaultDeviceAttestationVerifier.cpp:868-870 at 6170af84).
func decodeCertSigningRequest(b []byte) (csr, nonce []byte, err error) {
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, nil, err
	}
	err = spec.DecodeFields(n, "CertSigningRequest",
		spec.Bind(1, "certSigningRequest", true, &csr, spec.DecodeBytesIn(0, math.MaxInt)),
		spec.Bind(2, "csrNonce", true, &nonce, spec.DecodeBytesIn(32, 32)))
	if err != nil {
		return nil, nil, err
	}
	return csr, nonce, nil
}
