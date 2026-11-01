// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestCarriesStatusAndStatusOf: the status a port's error answers, as the
// dispatcher classifies it (endpoint classifyError) — a host's
// contract.StatusError by value or pointer, wrapped or not, an
// im.StatusCodeError, FAILURE for any other error and for a host status
// naming Success, Success for nil.
func TestCarriesStatusAndStatusOf(t *testing.T) {
	cluster := uint8(0x42)
	for _, tc := range []struct {
		name    string
		err     error
		carries bool
		status  im.StatusCode
	}{
		{"nil", nil, false, im.StatusSuccess},
		{"plain", errors.New("radio timeout"), false, im.StatusFailure},
		{"spec.Errorf", spec.Errorf(im.StatusBusy, "busy"), true, im.StatusBusy},
		{"wrapped spec.Errorf", fmt.Errorf("outer: %w", spec.Errorf(im.StatusNotFound, "gone")), true, im.StatusNotFound},
		{"host status by value", contract.StatusError{Status: uint8(im.StatusInvalidInState), Message: "no offer of our own"}, true, im.StatusInvalidInState},
		{"host status by pointer", &contract.StatusError{Status: uint8(im.StatusConstraintError)}, true, im.StatusConstraintError},
		{"host status wrapped", fmt.Errorf("relay: %w", contract.StatusError{Status: contract.StatusTimeout}), true, im.StatusCode(contract.StatusTimeout)},
		{"host status with cluster status", contract.StatusError{Status: uint8(im.StatusFailure), ClusterStatus: &cluster}, true, im.StatusFailure},
		{"host status naming Success", contract.StatusError{Status: uint8(im.StatusSuccess)}, true, im.StatusFailure},
		{"nil host status pointer", (*contract.StatusError)(nil), false, im.StatusFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := spec.CarriesStatus(tc.err); got != tc.carries {
				t.Fatalf("CarriesStatus(%v) = %v, want %v", tc.err, got, tc.carries)
			}
			if got := spec.StatusOf(tc.err); got != tc.status {
				t.Fatalf("StatusOf(%v) = %s, want %s", tc.err, got, tc.status)
			}
		})
	}
}
