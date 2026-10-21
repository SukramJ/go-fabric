// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// A port of matter.js packages/protocol/test/protocol/MessageChannelTest.ts at
// 862ad75f: the socket swap. The Go MessageChannel is folded into the
// Session: Session.SetAddr is the swap, and there is no networkAddressChanged
// observable — client/peer reads Session.Addr after a migration
// (peer.go followAddress). So "emits" is held as the session reporting the
// new address and sending there, and "does not emit" as nothing changing.

import (
	"context"
	"net"
	"testing"
)

func TestMessageChannelSocketSwap(t *testing.T) {
	t.Parallel()
	one := &net.UDPAddr{IP: net.ParseIP("::1"), Port: 5540}
	t.Run("emits networkAddressChanged when the socket swaps to a different address", func(t *testing.T) {
		t.Parallel()
		m, conn, _ := newTestManager(t, Config{})
		s, err := m.NewUnsecuredSession(one, nil)
		if err != nil {
			t.Fatal(err)
		}
		s.SetAddr(&net.UDPAddr{IP: net.ParseIP("::2"), Port: 5540})
		if got := s.Addr().IP.String(); got != "::2" {
			t.Fatalf("address = %s, want ::2", got)
		}
		x, _ := m.Initiate(s, 0, Options{})
		if err := x.Send(context.Background(), 0x20, nil, SendOptions{SuppressAck: true, DisableMRP: true}); err != nil {
			t.Fatal(err)
		}
		if got := conn.next(t).to.IP.String(); got != "::2" {
			t.Errorf("sent to %s, want ::2", got)
		}
	})
	t.Run("does not emit when the socket swaps to the same address", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		s, err := m.NewUnsecuredSession(one, nil)
		if err != nil {
			t.Fatal(err)
		}
		s.SetAddr(&net.UDPAddr{IP: net.ParseIP("::1"), Port: 5540})
		if got := s.Addr().IP.String(); got != "::1" {
			t.Errorf("address = %s, want ::1", got)
		}
	})
}
