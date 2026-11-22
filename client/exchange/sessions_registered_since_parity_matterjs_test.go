// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// A port of matter.js packages/protocol/test/session/SessionManagerTest.ts
// at b25ed960, the sessionsRegisteredSince rows (:1219-1240) of describe
// "peer loss by registration order": the sessions RebootResubscribeArmer
// counts as the peer's return (RebootResubscribeArmer.ts:149).

import (
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/secure/channel"
)

// newFabricSessionOfPeer is newFabricSession with another peer node id.
func newFabricSessionOfPeer(t *testing.T, m *Manager, id uint16, peer uint64) *Session {
	t.Helper()
	s, err := m.NewSecureSession(SecureSessionConfig{
		ID:   id,
		Addr: peerAddr,
		Channel: channel.Config{
			EncryptKey: keyI2R, DecryptKey: keyR2I,
			LocalNodeID: 0x1111, PeerNodeID: peer, PeerSessionID: 0x7000 + id,
		},
		PeerParameters: PeerParameters(nil),
		FabricIndex:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionManagerSessionsRegisteredSince(t *testing.T) {
	t.Parallel()
	t.Run("returns the sessions of the peer registered at or after a session", func(t *testing.T) {
		t.Parallel()
		// matter.js registers its establishing session after newer; a Go
		// session registers when it is created, so it is created last.
		m, _, _ := newTestManager(t, Config{})
		older := newFabricSession(t, m, 0x0100)
		since := newFabricSession(t, m, 0x0200)
		newer := newFabricSession(t, m, 0x0400)
		establishing, _ := newResponderSession(t, m, 0x0300, true)
		newFabricSessionOfPeer(t, m, 0x0500, 0x3333) // another peer's

		check := func(of *Session, want ...*Session) {
			t.Helper()
			got := m.SessionsRegisteredSince(of)
			if len(got) != len(want) {
				t.Fatalf("SessionsRegisteredSince(%#x) = %d sessions; want %d", of.ID(), len(got), len(want))
			}
			for _, w := range want {
				if !slices.Contains(got, w) {
					t.Fatalf("SessionsRegisteredSince(%#x) lacks %#x", of.ID(), w.ID())
				}
			}
		}
		check(since, since, newer, establishing)
		check(establishing, establishing)
		check(older, older, since, newer, establishing)
	})
	t.Run("treats a session that never registered as keeping nothing", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newTestManager(t, Config{})
		registered := newFabricSession(t, m, 0x0100)
		other, _, _ := newTestManager(t, Config{})
		unregistered := newFabricSession(t, other, 0x0200)
		if got := m.SessionsRegisteredSince(unregistered); len(got) != 0 {
			t.Fatalf("SessionsRegisteredSince of a session that never registered = %d sessions; want none", len(got))
		}
		m.HandlePeerLossBefore(responderPeer, errors.New("test: shutdown"), unregistered)
		if !registered.IsPeerLost() {
			t.Fatal("a session that never registered with the manager kept the peer's sessions")
		}
	})
}
