// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// Check names a check of the validation — matter.js DeviceAttestationCheck
// (DeviceAttestationValidator.ts:32-93 at db021da0). The string values are
// matter.js's.
type Check string

// The checks, DeviceAttestationValidator.ts:32-93 at db021da0, without
// CdSignerVerificationSkipped (:81-82 at db021da0), which matter.js keeps
// for compatibility but no longer emits.
const (
	PaaNotTrusted                            Check = "PaaNotTrusted"
	CertificateChainInvalid                  Check = "CertificateChainInvalid"
	CertificateExpired                       Check = "CertificateExpired"
	VendorIDMismatch                         Check = "VendorIdMismatch"
	AttestationSignatureInvalid              Check = "AttestationSignatureInvalid"
	AttestationNonceMismatch                 Check = "AttestationNonceMismatch"
	CertificationDeclarationSignatureInvalid Check = "CertificationDeclarationSignatureInvalid"
	// CertificationDeclarationFieldMismatch: a Certification Declaration
	// field does not match the device, Matter Core §6.2.3.1. Collected as
	// a recoverable error finding for the BasicInformation VendorID and
	// ProductID, which the device reports about itself, also when it did
	// not report them; a hard failure for a mismatch with the DAC or PAI
	// (DeviceAttestationValidator.ts:41-48 at db91886c).
	CertificationDeclarationFieldMismatch Check = "CertificationDeclarationFieldMismatch"
	CertificateRevoked                    Check = "CertificateRevoked"
	FirmwareInfoMismatch                  Check = "FirmwareInfoMismatch"
	CertificationTypeProvisional          Check = "CertificationTypeProvisional"
	CertificationTypeTest                 Check = "CertificationTypeTest"
	CertificateIDNotVerified              Check = "CertificateIdNotVerified"
	CertificateUnparseable                Check = "CertificateUnparseable"
	// DacFormatInvalid: the DAC parses but violates the DAC certificate
	// profile, Matter Core §6.2.2.3 (DeviceAttestationValidator.ts:49-54 at
	// db021da0).
	DacFormatInvalid Check = "DacFormatInvalid"
	// PaiFormatInvalid: the PAI parses but violates the PAI certificate
	// profile, Matter Core §6.2.2.4 (DeviceAttestationValidator.ts:56-61 at
	// db021da0).
	PaiFormatInvalid               Check = "PaiFormatInvalid"
	AttestationElementsUnparseable Check = "AttestationElementsUnparseable"
	// AttestationElementsTooLong: AttestationElements is longer than
	// RESP_MAX, Matter Core §11.18.4.1 (DeviceAttestationValidator.ts:71-77
	// at db91886c).
	AttestationElementsTooLong          Check = "AttestationElementsTooLong"
	CertificationDeclarationUnparseable Check = "CertificationDeclarationUnparseable"
	// CertificationDeclarationFormatInvalid: the CD breaks an encoding rule
	// of Matter Core §6.3.1 — its CMS envelope, format_version — or has a
	// reserved certification_type, which CHIP refuses
	// (DeviceAttestationValidator.ts:66-70 at db021da0).
	CertificationDeclarationFormatInvalid Check = "CertificationDeclarationFormatInvalid"
	// CdSignerUnknown: the CD's signer is neither a built-in CSA key, the
	// Matter test CD signer, nor a CSA-issued certificate the trust store
	// knows (DeviceAttestationValidator.ts:72-76 at db021da0).
	CdSignerUnknown Check = "CdSignerUnknown"
	// OfficialCdSignedByTestSigner: a CD of certification_type official is
	// signed by the Matter test CD signer (DeviceAttestationValidator.ts:
	// 78-79 at db021da0).
	OfficialCdSignedByTestSigner Check = "OfficialCdSignedByTestSigner"
	PaaTrustStoreTimeMismatch    Check = "PaaTrustStoreTimeMismatch"
	DclServiceUnavailable        Check = "DclServiceUnavailable"
	// TrustedAsTestCertificate: a test PAA or the Matter test CD signer
	// while the trust store does not accept test certificates, collected as
	// a recoverable error so the policy decides
	// (DeviceAttestationValidator.ts:85-89 at db021da0).
	TrustedAsTestCertificate Check = "TrustedAsTestCertificate"
	// ValidationError is an unexpected error during validation (an
	// unrecoverably corrupt trust-store certificate, say), DAV:105-106.
	ValidationError Check = "ValidationError"
)

// Level is a finding's severity — matter.js AttestationFinding.level
// (DAV:111-112).
type Level string

// The levels.
const (
	LevelError   Level = "error"
	LevelWarning Level = "warning"
	LevelInfo    Level = "info"
)

// Finding is one finding of the validation — matter.js AttestationFinding
// (DAV:110-119).
type Finding struct {
	Level   Level
	Check   Check
	Message string
}

// Error is a hard failure of the validation — matter.js
// DeviceAttestationError (DeviceAttestationValidator.ts:121-136 at
// db91886c), a CommissioningError there.
type Error struct {
	Check   Check
	Message string
	// Err is the cause, when the failure wraps one (a decoder's error).
	Err error
	// PrecedingFindings are the findings [Validate] collected before this
	// failure stopped it, in the order they arose, so a policy can see
	// them next to the failure — matter.js precedingFindings
	// (DeviceAttestationValidator.ts:123-127 at db91886c).
	PrecedingFindings []Finding
}

func (e *Error) Error() string { return "attestation: " + string(e.Check) + ": " + e.Message }

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

// fail is `throw new DeviceAttestationError(check, message)`.
func fail(check Check, message string) error { return &Error{Check: check, Message: message} }
