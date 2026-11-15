// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"errors"
	"testing"
)

// TestConnectTargetRefusesAnUnmappedHost: a host net/http could not map
// to ASCII reaches the CONNECT hook as it was written and is refused
// there, whatever a proxy would make of it (Fable R6, wave DCL round 4).
func TestConnectTargetRefusesAnUnmappedHost(t *testing.T) {
	t.Parallel()
	g := &destinationGuard{}
	for _, target := range []string{
		"127.0.0.1‍:443",
		"10.0.0.1‌:443",
		"１２７.０.０.１‍:443",
		"ｌｏｃａｌｈｏｓｔ‍:443",
		"169.254.169.254‍:443",
	} {
		if _, err := g.connectTarget(context.Background(), nil, target); !errors.Is(err, errDestination) {
			t.Errorf("connectTarget(%q) = %v, want errDestination", target, err)
		}
	}
	if _, err := g.connectTarget(context.Background(), nil, "xn--bcher-kva.example:443"); err != nil {
		t.Errorf("connectTarget(xn-- name) = %v, want nil", err)
	}
}
