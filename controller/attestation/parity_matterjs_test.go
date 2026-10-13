// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
)

// TestParityChecks: the DeviceAttestationCheck values of matter.js
// packages/protocol/src/certificate/DeviceAttestationValidator.ts:28-52 at
// 0ceb202d, in declaration order.
func TestParityChecks(t *testing.T) {
	t.Parallel()
	got := []attestation.Check{
		attestation.PaaNotTrusted, attestation.CertificateChainInvalid, attestation.CertificateExpired,
		attestation.VendorIDMismatch, attestation.AttestationSignatureInvalid, attestation.AttestationNonceMismatch,
		attestation.CertificationDeclarationSignatureInvalid, attestation.CertificationDeclarationFieldMismatch,
		attestation.CertificateRevoked, attestation.FirmwareInfoMismatch, attestation.CertificationTypeProvisional,
		attestation.CertificationTypeTest, attestation.CertificateIDNotVerified, attestation.CertificateUnparseable,
		attestation.AttestationElementsUnparseable, attestation.CertificationDeclarationUnparseable,
		attestation.CdSignerVerificationSkipped, attestation.PaaTrustStoreTimeMismatch,
		attestation.DclServiceUnavailable, attestation.TrustedAsTestCertificate, attestation.ValidationError,
	}
	want := []string{
		"PaaNotTrusted", "CertificateChainInvalid", "CertificateExpired", "VendorIdMismatch",
		"AttestationSignatureInvalid", "AttestationNonceMismatch", "CertificationDeclarationSignatureInvalid",
		"CertificationDeclarationFieldMismatch", "CertificateRevoked", "FirmwareInfoMismatch",
		"CertificationTypeProvisional", "CertificationTypeTest", "CertificateIdNotVerified",
		"CertificateUnparseable", "AttestationElementsUnparseable", "CertificationDeclarationUnparseable",
		"CdSignerVerificationSkipped", "PaaTrustStoreTimeMismatch", "DclServiceUnavailable",
		"TrustedAsTestCertificate", "ValidationError",
	}
	if len(got) != len(want) {
		t.Fatalf("%d checks, want %d", len(got), len(want))
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("check %d = %q, want %q", i, got[i], want[i])
		}
	}
	// AttestationFinding.level (DAV:57) and CertificationType
	// (kinds/definitions/certification-declaration.ts:21-28).
	if attestation.LevelError != "error" || attestation.LevelWarning != "warning" || attestation.LevelInfo != "info" {
		t.Error("levels differ from DAV:57")
	}
	if attestation.CertificationTest != 0 || attestation.CertificationProvisional != 1 || attestation.CertificationOfficial != 2 {
		t.Error("certification types differ from certification-declaration.ts:21-28")
	}
}
