// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/interaction"
)

// TestParityConstants: matter.js
// packages/protocol/src/action/client/ClientInteraction.ts:159-160 at
// 0ceb202d.
func TestParityConstants(t *testing.T) {
	t.Parallel()
	if interaction.DefaultTimedRequestTimeout != 10*time.Second {
		t.Errorf("DEFAULT_TIMED_REQUEST_TIMEOUT (:159) = %v, want 10s", interaction.DefaultTimedRequestTimeout)
	}
	if interaction.DefaultMinimumResponseTimeoutWithFailsafe != 30*time.Second {
		t.Errorf("DEFAULT_MINIMUM_RESPONSE_TIMEOUT_WITH_FAILSAFE (:160) = %v, want 30s", interaction.DefaultMinimumResponseTimeoutWithFailsafe)
	}
}
