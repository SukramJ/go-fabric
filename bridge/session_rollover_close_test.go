// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestSecureSessionClosedWhenItsCounterNearsExhaustion — a send that takes
// the session's outbound counter to within 100 000 of the maximum ends the
// session through the registry, as matter.js winds it down
// (packages/protocol/src/session/NodeSession.ts:398-412); a session whose
// counter is already used up is ended by the send that fails.
func TestSecureSessionClosedWhenItsCounterNearsExhaustion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		initial uint32
		sendErr bool
	}{
		{name: "near exhaustion", initial: 0xFFFFFFFF - 100_000},
		{name: "used up", initial: 0xFFFFFFFF, sendErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			reg := &fakeSessionRegistry{}
			b.AttachSessionRegistry(reg)
			sess, err := channel.New(channel.Config{
				EncryptKey: make([]byte, 16), DecryptKey: make([]byte, 16),
				LocalNodeID: 0xB0B, PeerNodeID: 0xA11, PeerSessionID: 0x1234,
				InitialCounter: tc.initial,
			})
			if err != nil {
				t.Fatal(err)
			}
			const sid uint16 = 7
			_, err = b.encryptSecureOutbound(sess, sid, &message.Header{SessionID: 0x1234}, []byte{0x01})
			if (err != nil) != tc.sendErr {
				t.Fatalf("encryptSecureOutbound err = %v, want error %v", err, tc.sendErr)
			}
			b.rolloverCloses.Wait()
			if got := reg.closedSessions(); !slices.Equal(got, []uint16{sid}) {
				t.Fatalf("registry Close calls = %v, want [%d]", got, sid)
			}
			// A second send does not close it again.
			_, _ = b.encryptSecureOutbound(sess, sid, &message.Header{SessionID: 0x1234}, []byte{0x01})
			b.rolloverCloses.Wait()
			if got := reg.closedSessions(); len(got) != 1 {
				t.Fatalf("registry Close calls after a second send = %v, want one", got)
			}
		})
	}
}
