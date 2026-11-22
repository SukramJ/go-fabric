// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package caseauth_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/caseauth"
)

// TestIsCASE (R2-P6-sec-1111): a group message has a fabric and is not
// PASE, and is not CASE either; neither is a request without a node
// subject or without a fabric.
func TestIsCASE(t *testing.T) {
	base := im.WithFabricFilter(context.Background(), true, 3)
	for name, tc := range map[string]struct {
		ctx  context.Context
		want bool
	}{
		"CASE":       {im.WithSubject(base, 0x77, nil), true},
		"PASE":       {im.WithAuthModePASE(im.WithSubject(base, 0x77, nil)), false},
		"group":      {im.WithGroupSubject(base, im.GroupSubject{GroupID: 1}), false},
		"no subject": {base, false},
		"no fabric":  {im.WithSubject(context.Background(), 0x77, nil), false},
	} {
		if got := caseauth.IsCASE(tc.ctx); got != tc.want {
			t.Errorf("%s: IsCASE %v", name, got)
		}
	}
}
