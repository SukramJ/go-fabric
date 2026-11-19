// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import "fmt"

// StatusError is a host handler's refusal with a Matter Interaction Model
// status: an OnWrite veto, a Handle<Command> handler, a cluster server's
// MatterWrite or MatterInvoke returns it (or wraps it) and the controller
// receives Status — and ClusterStatus when set — in place of FAILURE,
// which any other error answers. matter.js's StatusResponseError, which a
// behaviour throws to answer with a status of its own choice
// (packages/types/src/common/StatusResponseError.ts:18-25 at b25ed960,
// code and clusterCode).
//
// Status is the IM status code (Matter Core §8.10.1; the module's
// im.StatusCode values): [StatusTimeout] for an answer the host could not
// give in time, [StatusConstraintError], [StatusBusy], [StatusFailure]
// with ClusterStatus for a cluster-specific status, and the others
// declared below. A StatusError with status
// 0 (Success) answers FAILURE: an error never answers Success.
type StatusError struct {
	// Status is the IM status code.
	Status uint8
	// ClusterStatus is the cluster-specific status; nil sends none.
	ClusterStatus *uint8
	// Message is the error text for the host's logs; it is not sent.
	Message string
}

// StatusTimeout is the IM status TIMEOUT (0x94, Matter Core §8.10.1): a
// handler that could not answer in time returns
// contract.StatusError{Status: contract.StatusTimeout}.
const StatusTimeout uint8 = 0x94

// The IM statuses a host's veto or handler returns most ([StatusError]),
// the values of the module's im.StatusCode (Matter Core §8.10.1).
const (
	// StatusFailure is FAILURE (0x01); with a ClusterStatus it carries a
	// cluster-specific status.
	StatusFailure = 0x01
	// StatusInvalidCommand is INVALID_COMMAND (0x85).
	StatusInvalidCommand = 0x85
	// StatusConstraintError is CONSTRAINT_ERROR (0x87): a value outside
	// the attribute's or field's constraint.
	StatusConstraintError = 0x87
	// StatusUnsupportedWrite is UNSUPPORTED_WRITE (0x88).
	StatusUnsupportedWrite = 0x88
	// StatusResourceExhausted is RESOURCE_EXHAUSTED (0x89).
	StatusResourceExhausted = 0x89
	// StatusNotFound is NOT_FOUND (0x8B).
	StatusNotFound = 0x8B
	// StatusBusy is BUSY (0x9C).
	StatusBusy = 0x9C
	// StatusInvalidInState is INVALID_IN_STATE (0xCB).
	StatusInvalidInState = 0xCB
)

// Error renders the status and the message.
func (e StatusError) Error() string {
	s := fmt.Sprintf("matter status 0x%02X", e.Status)
	if e.ClusterStatus != nil {
		s += fmt.Sprintf(", cluster status 0x%02X", *e.ClusterStatus)
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}
