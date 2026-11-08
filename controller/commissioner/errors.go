// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/client/interaction"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/attestation"
)

// The classes of commissioning failure — matter.js CommissioningError
// (packages/protocol/src/peer/CommissioningError.ts:14) and its subclasses
// of ControllerCommissioningFlow.ts:223-254 at 0ceb202d. A
// [*CommissioningError] carries one as its Kind; errors.Is matches the
// kind and, for every kind, [ErrCommissioning].
var (
	// ErrCommissioning is CommissioningError, the class of every kind.
	ErrCommissioning = errors.New("commissioner: commissioning failed")
	// ErrMaximumCommissionedFabricsReached: the device has no room for
	// another fabric (:224).
	ErrMaximumCommissionedFabricsReached = errors.New("commissioner: maximum commissioned fabrics reached")
	// ErrCommissioningTimeout: commissioning took longer than the
	// device's maximum cumulative failsafe (:227).
	ErrCommissioningTimeout = errors.New("commissioner: commissioning timeout")
	// ErrDeviceAlreadyCommissionedToThisFabric: AddNOC answered
	// FabricConflict (:230).
	ErrDeviceAlreadyCommissionedToThisFabric = errors.New("commissioner: device already commissioned to this fabric")
	// ErrFabricLabelConflict: UpdateFabricLabel answered LabelConflict
	// (:233).
	ErrFabricLabelConflict = errors.New("commissioner: fabric label conflict")
	// ErrNodeIDConflict: the node id is already a peer of the fabric
	// (:245).
	ErrNodeIDConflict = errors.New("commissioner: node id conflict")
	// ErrOperativeConnectionFailed: the operational CASE connection after
	// AddNOC failed (:248).
	ErrOperativeConnectionFailed = errors.New("commissioner: operative connection failed")
	// errRecoverable is RecoverableCommissioningError (:251, not
	// exported there either): the flow logs it and continues.
	errRecoverable = errors.New("commissioner: recoverable commissioning error")
	// ErrAttestationRejected: the device's attestation produced findings
	// and the policy (Options.OnAttestationFailure) rejected them, or a
	// chain the validator refused was not accepted — the two throws of
	// matter.js's #deviceAttestation after #resolveAttestationFindings
	// (packages/protocol/src/peer/ControllerCommissioningFlow.ts:1306-1315
	// at 862ad75f). matter.js names no class of its own for it (the error
	// is a CommissioningError or the validator's DeviceAttestationError);
	// errors.Is matches this sentinel besides the error matter.js throws,
	// whose message the error keeps. A policy function's own error is
	// returned verbatim, without it.
	ErrAttestationRejected = errors.New("commissioner: device attestation rejected")
)

// attestationRejected is a rejection of the device's attestation: err as
// matter.js throws it, matching [ErrAttestationRejected] as well.
type attestationRejected struct{ err error }

func (e attestationRejected) Error() string { return e.err.Error() }

// Unwrap makes ErrAttestationRejected and the rejection's own chain match
// errors.Is and errors.As.
func (e attestationRejected) Unwrap() []error { return []error{ErrAttestationRejected, e.err} }

// CommissioningError is a commissioning failure of one of the kinds above.
type CommissioningError struct {
	// Kind is the class, ErrCommissioning for the base class.
	Kind error
	// Message is matter.js's message.
	Message string
	// Cause is the underlying error, nil when there is none.
	Cause error
}

func newError(kind error, message string, cause error) *CommissioningError {
	return &CommissioningError{Kind: kind, Message: message, Cause: cause}
}

func (e *CommissioningError) Error() string {
	if e.Cause != nil {
		return "commissioner: " + e.Message + ": " + e.Cause.Error()
	}
	return "commissioner: " + e.Message
}

// Unwrap makes the kind, ErrCommissioning and the cause match errors.Is.
func (e *CommissioningError) Unwrap() []error {
	out := []error{e.Kind}
	if e.Kind != ErrCommissioning { //nolint:errorlint // the identity of the kind, not a wrapped error
		out = append(out, ErrCommissioning)
	}
	if e.Cause != nil {
		out = append(out, e.Cause)
	}
	return out
}

// GeneralCommissioningError is a GeneralCommissioning command the device
// refused: ArmFailSafe, SetRegulatoryConfig, SetTCAcknowledgements or
// CommissioningComplete answered with an ErrorCode other than OK. It is
// matter.js's CommissioningError of #ensureGeneralCommissioningSuccess
// (ControllerCommissioningFlow.ts:712-719 at 0ceb202d), its message
// unchanged, with the command and the code as data: errors.As finds it,
// and it unwraps to that [*CommissioningError] of kind
// [ErrCommissioning]. A device that requires terms and conditions answers
// CommissioningComplete with RequiredTCNotAccepted, TCAcknowledgementsNotReceived
// or TCMinVersionNotMet when they were not acknowledged — the flow
// returns a [*TermsRequiredError] earlier when the device says so.
type GeneralCommissioningError struct {
	// Command is the refused command in matter.js's naming:
	// "armFailSafe", "setRegulatoryConfig", "setTCAcknowledgements" or
	// "commissioningComplete".
	Command string
	// Code is the response's ErrorCode.
	Code gencomm.CommissioningErrorEnum
	// DebugText is the response's DebugText, "" when it carries none.
	DebugText string
}

func (e *GeneralCommissioningError) message() string {
	msg := fmt.Sprintf("Commission error for %q: %s (%d)", e.Command, enumName(gencomm.CommissioningErrorEnumDef, uint64(e.Code)), e.Code)
	if e.DebugText != "" {
		msg += ", " + e.DebugText
	}
	return msg
}

func (e *GeneralCommissioningError) Error() string { return "commissioner: " + e.message() }

// Unwrap is matter.js's CommissioningError with the same message, so
// errors.Is matches [ErrCommissioning] and errors.As a
// [*CommissioningError].
func (e *GeneralCommissioningError) Unwrap() error {
	return newError(ErrCommissioning, e.message(), nil)
}

// TermsRequiredError is a device that requires an acknowledgement of its
// terms and conditions the commissioning does not carry: its root
// GeneralCommissioning serves the TermsAndConditions feature,
// TCAcknowledgementsRequired is true, and [Options.TermsAndConditions] is
// nil or its Version below the device's TCMinRequiredVersion. The flow
// reads the requirement in step 0.1 and returns this right after it,
// before ArmFailSafe and AddNOC, so a caller can obtain the user's
// consent and commission again with the acknowledgement. Neither
// matter.js nor connectedhomeip reads the requirement (register row
// BD-Matter-Commissioner-TermsRequirementRead). It unwraps to a
// [*CommissioningError] of kind [ErrCommissioning].
//
// It names no acknowledgement bits: a device serves no attribute that
// names the bits it requires — TCAcknowledgements is the set it has
// accepted, and the required set is the vendor's Distributed Compliance
// Ledger entry, outside the commissioning. The caller takes the bits from
// the terms it shows the user; a missing bit is the device's
// RequiredTCNotAccepted at SetTCAcknowledgements, a
// [*GeneralCommissioningError].
type TermsRequiredError struct {
	// Version is the device's TCMinRequiredVersion, the lowest version an
	// acknowledgement has to name.
	Version uint16
	// Deadline is the device's TCUpdateDeadline in Matter epoch seconds,
	// the time by which a commissioned device expects an updated
	// acceptance; nil when the attribute is null.
	Deadline *uint32
}

func (e *TermsRequiredError) message() string {
	return fmt.Sprintf("Device requires acknowledgement of its terms and conditions, version %d or later", e.Version)
}

func (e *TermsRequiredError) Error() string { return "commissioner: " + e.message() }

// Unwrap is a CommissioningError with the same message, so errors.Is
// matches [ErrCommissioning] and errors.As a [*CommissioningError].
func (e *TermsRequiredError) Unwrap() error { return newError(ErrCommissioning, e.message(), nil) }

// PairRetransmissionLimitReachedError is a PASE connection that reached no
// candidate before its budget — matter.js PairRetransmissionLimitReachedError
// (peer/CommissioningError.ts:19).
type PairRetransmissionLimitReachedError struct {
	Message string
	// Cause is the last retryable error, nil when there was none.
	Cause error
}

func (e *PairRetransmissionLimitReachedError) Error() string {
	if e.Cause != nil {
		return "commissioner: " + e.Message + ": " + e.Cause.Error()
	}
	return "commissioner: " + e.Message
}

// Unwrap returns the cause.
func (e *PairRetransmissionLimitReachedError) Unwrap() error { return e.Cause }

// PeerCommunicationError is matter.js PeerCommunicationError
// (peer/PeerCommunicationError.ts:12): Commission's error when no
// candidate address could be reached (ControllerCommissioner.ts:402-407).
type PeerCommunicationError struct {
	Message string
	Cause   error
}

func (e *PeerCommunicationError) Error() string {
	// matter.js's PeerCommunicationError carries the message alone; the
	// cause is appended here so a failed connection names what the last
	// attempt saw (a retransmission limit, a refused address).
	if e.Cause != nil {
		return "commissioner: " + e.Message + ": " + e.Cause.Error()
	}
	return "commissioner: " + e.Message
}

// Unwrap returns the cause.
func (e *PeerCommunicationError) Unwrap() error { return e.Cause }

// CommissioningTransitionError is the cause a PASE session closes with
// once commissioning moved on — matter.js CommissioningTransitionError
// (peer/PeerCommunicationError.ts:117).
type CommissioningTransitionError struct{ Message string }

func (e *CommissioningTransitionError) Error() string { return "commissioner: " + e.Message }

// isCommissioningError is causedBy(error, CommissioningError): a
// CommissioningError anywhere in the chain, the attestation's
// DeviceAttestationError (DeviceAttestationValidator.ts:67, a
// CommissioningError there) included.
func isCommissioningError(err error) bool {
	var ce *CommissioningError
	var ae *attestation.Error
	return errors.As(err, &ce) || errors.As(err, &ae)
}

// isStatusError is causedBy(error, StatusResponseError).
func isStatusError(err error) bool {
	var se *interaction.StatusError
	return errors.As(err, &se)
}
