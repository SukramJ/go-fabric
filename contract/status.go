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
// (packages/types/src/common/StatusResponseError.ts:18-25 at 862ad75f,
// code and clusterCode).
//
// Status is the IM status code (Matter Core §8.10.1; the module's
// im.StatusCode values): [StatusTimeout] for an answer the host could not
// give in time, 0x87 ConstraintError, 0x9C Busy, 0x01 Failure with
// ClusterStatus for a cluster-specific status. A StatusError with status
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
