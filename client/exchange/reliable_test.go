// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestReliableManagerRunsNoMRP: on a reliable carrier (BTP) nothing asks
// for an acknowledgement, a send returns without waiting for one, no
// standalone ack is sent for a peer message that asks, and Next waits the
// reliable channel's time — matter.js Session.ts:355-356,
// MessageExchange.ts:449-453, :660, :666-668, MRP.ts:111-113.
func TestReliableManagerRunsNoMRP(t *testing.T) {
	t.Parallel()
	m, conn, clock := newTestManager(t, Config{Reliable: true})
	if !m.Reliable() {
		t.Fatal("Reliable false")
	}
	s, p := newUnsecured(t, m)
	if s.UsesMRP() {
		t.Fatal("UsesMRP on a reliable manager")
	}
	x, _ := m.Initiate(s, mrp.SecureChannelProtocolID, Options{})
	if err := x.Send(context.Background(), mrp.SCOpcodePBKDFParamRequest, []byte{1}, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	first := decodeUnsecured(t, conn.nextOn(t, x).bytes)
	if first.proto.NeedsAck || first.proto.HasAck {
		t.Fatalf("first message asks for or carries an ack: %+v", first.proto)
	}
	if x.HasUnackedMessage() {
		t.Fatal("a message waits for an ack")
	}
	// The peer's reply asks for an ack; none is sent, then or later.
	p.reply(x.ID(), 0, mrp.SCOpcodePBKDFParamResponse, true, nil, []byte{2})
	msg, err := x.Next(context.Background(), ReceiveOptions{})
	if err != nil || msg.RequiresAck {
		t.Fatalf("Next = %+v, %v", msg, err)
	}
	clock.Advance(StandaloneAckTimeout)
	conn.none(t)
	if err := x.Send(context.Background(), mrp.SCOpcodePake1, nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if second := decodeUnsecured(t, conn.nextOn(t, x).bytes); second.proto.HasAck || second.proto.NeedsAck {
		t.Fatalf("second message %+v", second.proto)
	}
	// Next waits 30 s plus the 5 s buffer, then gives up.
	errc := make(chan error, 1)
	go func() {
		_, err := x.Next(context.Background(), ReceiveOptions{})
		errc <- err
	}()
	waitTimers(t, clock)
	clock.Advance(35*time.Second - time.Millisecond)
	select {
	case err := <-errc:
		t.Fatalf("Next ended early: %v", err)
	default:
	}
	clock.Advance(time.Millisecond)
	var pm *PeerMessageMissingError
	if err := <-errc; !errors.As(err, &pm) || pm.Timeout != 35*time.Second {
		t.Fatalf("Next = %v", err)
	}
	if got := ReliableMaxPeerResponseTime(time.Minute); got != time.Minute+5*time.Second {
		t.Fatalf("ReliableMaxPeerResponseTime(1m) = %v", got)
	}
}

// waitTimers waits until the goroutine under test armed its timer.
func waitTimers(t *testing.T, c *fakeClock) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.pendingTimers() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no timer armed")
		}
		time.Sleep(time.Millisecond)
	}
}
