// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jcm

import (
	"fmt"
	"sync"
)

// InvalidEndpointID is chip's kInvalidEndpointId: no administrator
// endpoint known.
const InvalidEndpointID uint16 = 0xFFFF

// Info is what a trust verification collects about the other side's
// administrator: its JointFabricAdministrator endpoint and
// AdministratorFabricIndex, and that fabric's vendor id, fabric id, root
// public key and certificate chain. Mirrors connectedhomeip
// TrustVerification.h:39-63 (TrustVerificationInfo) at 6170af84.
type Info struct {
	AdminEndpointID  uint16
	AdminFabricIndex uint8
	AdminVendorID    uint16
	AdminFabricID    uint64
	RootPublicKey    []byte
	AdminRCAC        []byte
	AdminICAC        []byte
	AdminNOC         []byte
}

// NewInfo is an Info with nothing known: the invalid endpoint, the
// undefined fabric index (TrustVerification.h:41-42).
func NewInfo() Info { return Info{AdminEndpointID: InvalidEndpointID} }

// Cleanup forgets everything collected: the endpoint invalid, the fabric
// index, vendor id (VendorId::Common, 0) and fabric id undefined, the keys
// and certificates dropped. Mirrors TrustVerification.h:52-62.
func (i *Info) Cleanup() { *i = NewInfo() }

// Stage is a stage of the trust verification. Mirrors
// TrustVerification.h:82-95 (TrustVerificationStage); the commissioner
// runs VerifyingAdministratorInformation, PerformingVendorIDVerification
// and AskingUserForConsent, the administrator StoringEndpointID,
// ReadingCommissionerAdminFabricIndex, PerformingVendorIDVerification and
// CrossCheckingAdministratorIDs.
type Stage uint8

// The stages.
const (
	StageIdle Stage = iota
	StageVerifyingAdministratorInformation
	StagePerformingVendorIDVerification
	StageAskingUserForConsent
	StageStoringEndpointID
	StageReadingCommissionerAdminFabricIndex
	StageCrossCheckingAdministratorIDs
	StageComplete
	StageError
)

// String is chip's EnumToString of the stage (TrustVerification.h:143-169).
func (s Stage) String() string {
	switch s {
	case StageIdle:
		return "IDLE"
	case StageVerifyingAdministratorInformation:
		return "VERIFYING_ADMINISTRATOR_INFORMATION"
	case StagePerformingVendorIDVerification:
		return "PERFORMING_VENDOR_ID_VERIFICATION_PROCEDURE"
	case StageAskingUserForConsent:
		return "ASKING_USER_FOR_CONSENT"
	case StageStoringEndpointID:
		return "STORING_ENDPOINT_ID"
	case StageReadingCommissionerAdminFabricIndex:
		return "READING_COMMISSIONER_ADMIN_FABRIC_INDEX"
	case StageCrossCheckingAdministratorIDs:
		return "CROSS_CHECKING_ADMINISTRATOR_IDS"
	case StageComplete:
		return "COMPLETE"
	case StageError:
		return "ERROR"
	}
	return "UNKNOWN"
}

// Result is the outcome of a stage. Mirrors TrustVerification.h:65-80
// (TrustVerificationError); Async is a stage that finishes later through
// [Machine.Finished].
type Result uint16

// The results, chip's values.
const (
	Success                         Result = 0
	Async                           Result = 1
	InvalidAdministratorEndpointID  Result = 100
	InvalidAdministratorFabricIndex Result = 101
	InvalidAdministratorCAT         Result = 102
	TrustVerificationDelegateNotSet Result = 103
	UserDeniedConsent               Result = 104
	VendorIDVerificationFailed      Result = 105
	ReadAdminAttributeFailed        Result = 106
	AdministratorIDMismatched       Result = 107
	InternalError                   Result = 200
)

// String is chip's EnumToString of the result (TrustVerification.h:104-134).
func (r Result) String() string {
	switch r {
	case Success:
		return "SUCCESS"
	case Async:
		return "ASYNC_OPERATION"
	case InvalidAdministratorEndpointID:
		return "INVALID_ADMINISTRATOR_ENDPOINT_ID"
	case InvalidAdministratorFabricIndex:
		return "INVALID_ADMINISTRATOR_FABRIC_INDEX"
	case InvalidAdministratorCAT:
		return "INVALID_ADMINISTRATOR_CAT"
	case TrustVerificationDelegateNotSet:
		return "TRUST_VERIFICATION_DELEGATE_NOT_SET"
	case UserDeniedConsent:
		return "USER_DENIED_CONSENT"
	case VendorIDVerificationFailed:
		return "VENDOR_ID_VERIFICATION_FAILED"
	case ReadAdminAttributeFailed:
		return "READ_ADMIN_ATTRIBUTE_FAILED"
	case AdministratorIDMismatched:
		return "ADMINISTRATOR_ID_MISMATCHED"
	case InternalError:
		return "INTERNAL_ERROR"
	}
	return "UNKNOWN_ERROR"
}

// Failure is a trust verification that ended at a stage with a result
// other than Success.
type Failure struct {
	Stage  Stage
	Result Result
}

// Error implements error.
func (f Failure) Error() string {
	return fmt.Sprintf("jcm: trust verification failed at %s: %s", f.Stage, f.Result)
}

// Machine is the trust verification's stage machine: Next names the stage
// after a completed one, Perform runs a stage — finishing it at once
// through [Machine.Finished], or later for a stage that waits —, Progress
// hears every finished stage and Complete the end. Mirrors
// TrustVerification.h:173-217 (TrustVerificationStateMachine,
// TrustVerificationDelegate::OnProgressUpdate) and TrustVerification.cpp
// :34-76. Next and Perform are required; a Machine is not reused once
// complete.
type Machine struct {
	Next     func(completed Stage) Stage
	Perform  func(stage Stage)
	Progress func(stage Stage, result Result)
	Complete func(result Result)

	mu   sync.Mutex
	done bool
}

// Start performs the stage after Idle. Mirrors TrustVerification.cpp:40-43.
func (m *Machine) Start() { m.Perform(m.Next(StageIdle)) }

// Finished reports a stage's result: Progress hears it; a result other
// than Success, or the Complete or Error stage, completes the machine;
// otherwise the next stage runs, and a next stage of Error completes it
// with InternalError. A Finished after the machine completed is ignored.
// Mirrors TrustVerification.cpp:45-76.
func (m *Machine) Finished(completed Stage, result Result) {
	m.mu.Lock()
	if m.done {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	if m.Progress != nil {
		m.Progress(completed, result)
	}
	if result != Success || completed == StageComplete || completed == StageError { // :55-66
		m.complete(result)
		return
	}
	next := m.Next(completed)
	if next == StageError { // :68-73
		m.complete(InternalError)
		return
	}
	m.Perform(next)
}

func (m *Machine) complete(result Result) {
	m.mu.Lock()
	if m.done {
		m.mu.Unlock()
		return
	}
	m.done = true
	m.mu.Unlock()
	if m.Complete != nil {
		m.Complete(result)
	}
}

// Run performs stage with fn and finishes it with fn's result unless it
// is Async — chip's PerformTrustVerificationStage pattern
// (JCMCommissionee.cpp:147-150, jcm/DeviceCommissioner.cpp:470-473).
func (m *Machine) Run(stage Stage, fn func() Result) {
	if r := fn(); r != Async {
		m.Finished(stage, r)
	}
}
