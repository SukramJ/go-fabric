// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package attestation

// Check names a check of the validation — matter.js DeviceAttestationCheck
// (DeviceAttestationValidator.ts:28-52 at 0ceb202d). The string values are
// matter.js's.
type Check string

// The checks, DAV:28-52.
const (
	PaaNotTrusted                            Check = "PaaNotTrusted"
	CertificateChainInvalid                  Check = "CertificateChainInvalid"
	CertificateExpired                       Check = "CertificateExpired"
	VendorIDMismatch                         Check = "VendorIdMismatch"
	AttestationSignatureInvalid              Check = "AttestationSignatureInvalid"
	AttestationNonceMismatch                 Check = "AttestationNonceMismatch"
	CertificationDeclarationSignatureInvalid Check = "CertificationDeclarationSignatureInvalid"
	CertificationDeclarationFieldMismatch    Check = "CertificationDeclarationFieldMismatch"
	CertificateRevoked                       Check = "CertificateRevoked"
	FirmwareInfoMismatch                     Check = "FirmwareInfoMismatch"
	CertificationTypeProvisional             Check = "CertificationTypeProvisional"
	CertificationTypeTest                    Check = "CertificationTypeTest"
	CertificateIDNotVerified                 Check = "CertificateIdNotVerified"
	CertificateUnparseable                   Check = "CertificateUnparseable"
	AttestationElementsUnparseable           Check = "AttestationElementsUnparseable"
	CertificationDeclarationUnparseable      Check = "CertificationDeclarationUnparseable"
	CdSignerVerificationSkipped              Check = "CdSignerVerificationSkipped"
	PaaTrustStoreTimeMismatch                Check = "PaaTrustStoreTimeMismatch"
	DclServiceUnavailable                    Check = "DclServiceUnavailable"
	TrustedAsTestCertificate                 Check = "TrustedAsTestCertificate"
	// ValidationError is an unexpected error during validation (an
	// unrecoverably corrupt trust-store certificate, say), DAV:50-51.
	ValidationError Check = "ValidationError"
)

// Level is a finding's severity — matter.js AttestationFinding.level
// (DAV:56-57).
type Level string

// The levels.
const (
	LevelError   Level = "error"
	LevelWarning Level = "warning"
	LevelInfo    Level = "info"
)

// Finding is one finding of the validation — matter.js AttestationFinding
// (DAV:55-64).
type Finding struct {
	Level   Level
	Check   Check
	Message string
}

// Error is a hard failure of the validation — matter.js
// DeviceAttestationError (DAV:67-75), a CommissioningError there.
type Error struct {
	Check   Check
	Message string
	// Err is the cause, when the failure wraps one (a decoder's error).
	Err error
}

func (e *Error) Error() string { return "attestation: " + string(e.Check) + ": " + e.Message }

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

// fail is `throw new DeviceAttestationError(check, message)`.
func fail(check Check, message string) error { return &Error{Check: check, Message: message} }
