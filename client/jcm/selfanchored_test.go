// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jcm_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/client/jcm"
)

// TestSelfAnchored: the self-anchored lookup answers with the
// administrator's own RCAC inside a verification — the device vouching for
// itself, an opt-in for tests and the example controller — and outside one
// it finds no anchor (review II, R2-P6-sec-1850, owner decision 190).
func TestSelfAnchored(t *testing.T) {
	f := newJFFabric(t)
	if err := verify(t, f, f.info(), jcm.SelfAnchored); err != nil {
		t.Fatalf("VerifyVendorID with SelfAnchored = %v", err)
	}
	if _, err := jcm.SelfAnchored(context.Background(), 0xFFF1, nil); !errors.Is(err, jcm.ErrNoTrustAnchor) {
		t.Fatalf("SelfAnchored outside a verification = %v, want ErrNoTrustAnchor", err)
	}
}
