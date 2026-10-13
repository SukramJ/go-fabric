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

// TestParityProviderPathFloors: ExchangeProvider.readPathsSupported /
// subscribePathsSupported (protocol/ExchangeProvider.ts:85-90): 9 and 3.
func TestParityProviderPathFloors(t *testing.T) {
	t.Parallel()
	if interaction.DefaultReadPathsSupported != 9 || interaction.DefaultSubscribePathsSupported != 3 {
		t.Errorf("path floors %d/%d, want 9/3", interaction.DefaultReadPathsSupported, interaction.DefaultSubscribePathsSupported)
	}
}

// TestParityWriteExemption: the paths whose list values are never chunked
// — AccessControl (0x001F) acl (0x0000) and extension (0x0001),
// action/request/Write.ts:14-21 with the ids of
// packages/model/src/standard/elements/access-control.element.ts:20, :31, :35.
func TestParityWriteExemption(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cluster, attribute uint32
		chunked            bool
	}{{0x001F, 0x0000, false}, {0x001F, 0x0001, false}, {0x001F, 0x0002, true}, {0x001E, 0x0000, true}} {
		if got := interaction.ListChunked(c.cluster, c.attribute); got != c.chunked {
			t.Errorf("0x%04X/0x%04X chunked %v, want %v", c.cluster, c.attribute, got, c.chunked)
		}
	}
}
