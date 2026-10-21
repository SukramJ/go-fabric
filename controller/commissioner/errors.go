// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"errors"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/interaction"
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
)

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
