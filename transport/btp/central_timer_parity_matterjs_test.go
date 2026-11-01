// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// immediateClock fires every timer at once, on its own goroutine: the
// send-ack timer the central starts in its constructor then races the
// constructor itself.
type immediateClock struct{ clock.System }

func (immediateClock) AfterFunc(_ time.Duration, fn func()) clock.Timer {
	return clock.System{}.AfterFunc(0, fn)
}

// The central owes the acknowledgement of the handshake response
// (sequence 0) and starts its send-ack timer in the constructor
// (BtpSessionHandler.ts:221-226 at 862ad75f). The timer's callback reads
// the sequence state the constructor sets, so even a timer that fires at
// once acknowledges sequence 0 with the central's first sequence number 0
// — and the race detector sees no unsynchronised write.
func TestBtpCentralSendAckTimerSeesTheConstructedState(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	done := make(chan struct{}, 1)
	c, err := NewCentral(unhex(t, "656c04f40004"), Callbacks{
		Write: func(b []byte) error {
			rec.add(b)
			select {
			case done <- struct{}{}:
			default:
			}
			return nil
		},
		Disconnect: func() error { return nil }, Message: noMessage(t),
	}, MaximumBTPMTU, Options{TimeSource: immediateClock{}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no standalone ack")
	}
	rec.mu.Lock()
	p := mustDecode(t, rec.writes[0])
	rec.mu.Unlock()
	if !p.Header.HasAckNumber || p.Payload.AckNumber == nil || *p.Payload.AckNumber != 0 || p.Payload.SequenceNumber != 0 {
		t.Fatalf("standalone ack = %+v, want ack 0 at sequence 0", p)
	}
}
