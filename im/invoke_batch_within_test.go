// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "testing"

// TestValidateInvokeBatchWithin: the batch limit is the node's own
// MaxPathsPerInvoke, 0 being DefaultMaxPathsPerInvoke (matter.js
// InteractionServer.ts:198, :1017).
func TestValidateInvokeBatchWithin(t *testing.T) {
	t.Parallel()
	batch := InvokeRequest{Invokes: []CommandInvocation{
		{Path: ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 0, HasEndpoint: true, HasCluster: true, HasCommand: true}, CommandRef: 1, HasCommandRef: true},
		{Path: ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 1, HasEndpoint: true, HasCluster: true, HasCommand: true}, CommandRef: 2, HasCommandRef: true},
	}}
	for _, tc := range []struct {
		maxPaths int
		want     StatusCode
	}{
		{1, StatusInvalidAction},
		{2, StatusSuccess},
		{0, StatusSuccess},
	} {
		if got := ValidateInvokeBatchWithin(batch, tc.maxPaths); got != tc.want {
			t.Errorf("two paths within %d: %v, want %v", tc.maxPaths, got, tc.want)
		}
	}
	if got := ValidateInvokeBatch(batch); got != StatusSuccess {
		t.Errorf("ValidateInvokeBatch: %v, want Success", got)
	}
}
