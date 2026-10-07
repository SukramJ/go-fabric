// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
)

// The order key is the local session id of a unicast secure message; the
// unsecured session (id 0, the PASE / CASE handshakes) and group messages
// are not ordered by the listener.
func TestSessionOrderKey(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		buf  []byte
		key  uint32
		want bool
	}{
		"unicast secure": {[]byte{0x00, 0x34, 0x12, 0x00}, 0x1234, true},
		"unsecured":      {[]byte{0x00, 0x00, 0x00, 0x00}, 0, false},
		"group":          {[]byte{0x00, 0x34, 0x12, 0x01}, 0, false},
		"short":          {[]byte{0x00, 0x34}, 0, false},
	} {
		key, ok := sessionOrderKey(tc.buf)
		if ok != tc.want || (ok && key != tc.key) {
			t.Errorf("%s: (%#x, %v), want (%#x, %v)", name, key, ok, tc.key, tc.want)
		}
	}
	released := false
	ctx := context.WithValue(context.Background(), sessionTurnKey{}, func() { released = true })
	leaveSessionOrder(ctx)
	leaveSessionOrder(context.Background())
	if !released {
		t.Error("leaveSessionOrder did not release the turn")
	}
}
