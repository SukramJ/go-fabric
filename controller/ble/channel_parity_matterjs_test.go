// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble_test

// A port of matter.js packages/nodejs-ble/test/NobleBleChannelTest.ts at
// 862ad75f: the cases of the ATT_MTU settle wait (:635-671, :1099-1136)
// and of the BTP renegotiation after a stall (:673-1045). MockTime is a
// clocktest.Fake inside a synctest bubble, whose Wait stands for
// MockTime.resolve; FakePeripheral and handshakeOnlyMatterService
// (:208-346) are a bletest.Peripheral with Stall and Faults; a send
// parked on the renegotiation is a goroutine. Each case names its
// matter.js title.

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// stallPeer is handshakeOnlyMatterService (NobleBleChannelTest.ts:208-346)
// over a bletest peripheral: a session offered a segment size above
// served is stalled — the handshake completed at that size, every BTP
// packet withheld and counted — and one at or below it is served, its
// Matter messages landing on got. served 0 stalls every session.
type stallPeer struct {
	p        *bletest.Peripheral
	withheld atomic.Int32
	got      chan []byte
}

func newStallPeer(served int) *stallPeer {
	s := &stallPeer{got: make(chan []byte, 8)}
	s.p = &bletest.Peripheral{
		Address: "11:22:33:44:55:66", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001,
		Stall: func(offered int) func([]byte) {
			if offered <= served {
				return nil
			}
			return func([]byte) { s.withheld.Add(1) }
		},
		Serve: func(*btp.Conn) (func([]byte), func()) {
			return func(m []byte) { s.got <- m }, nil
		},
	}
	return s
}

func (s *stallPeer) dial(t *testing.T, clock *clocktest.Fake) *ble.Channel {
	t.Helper()
	ch, err := ble.Dial(t.Context(), bletest.NewCentral(s.p), ble.Peripheral{Address: s.p.Address}, ble.DialOptions{Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

// sizes are the segment sizes the peripheral's handshakes offered.
func (s *stallPeer) sizes() []int { return s.p.Observed().HandshakeSegmentSizes }

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// parkSend issues a send that waits for the renegotiation and reports
// its error — the parked channel.send of the matter.js cases.
func parkSend(ch *ble.Channel, payload []byte) <-chan error {
	errc := make(chan error, 1)
	go func() { errc <- ch.Send(nil, payload) }()
	return errc
}

// stall sends msg over ch into a stalled session and lets the
// acknowledgement timeout report the stall, which starts the
// renegotiation; the renegotiation has begun when stall returns.
func stall(t *testing.T, clock *clocktest.Fake, ch *ble.Channel, peer *stallPeer, msgs ...[]byte) {
	t.Helper()
	for _, m := range msgs {
		if err := ch.Send(nil, m); err != nil {
			t.Fatal(err)
		}
	}
	synctest.Wait()
	if got := peer.withheld.Load(); int(got) != len(msgs) {
		t.Fatalf("withheld %d packets before the stall, want %d", got, len(msgs))
	}
	clock.Advance(btp.AckTimeout)
}

var noHandshakeTimer = "the handshake timer is still armed after the renegotiation was abandoned"

// TestNobleBleChannelAttMtuSettle covers NobleBleChannelTest.ts:635-671
// and :1099-1136: the ATT_MTU exchange awaited before the segment size
// is derived.
func TestNobleBleChannelAttMtuSettle(t *testing.T) {
	t.Run("derives the segment size from an ATT_MTU that arrives after the interview", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MaximumBTPMTU)
			ready := make(chan struct{})
			peer.p.ATTMTUReady = ready
			chc := make(chan *ble.Channel, 1)
			go func() {
				ch, err := ble.Dial(t.Context(), bletest.NewCentral(peer.p), ble.Peripheral{Address: peer.p.Address}, ble.DialOptions{Clock: clock})
				if err != nil {
					t.Error(err)
				}
				chc <- ch
			}()
			synctest.Wait()
			if !slices.Contains(clock.Armed(), ble.ATTMTUSettleTimeout) {
				t.Fatalf("no %v settle timer armed (armed %v)", ble.ATTMTUSettleTimeout, clock.Armed())
			}
			close(ready) // the exchange lands only once someone waits for it
			ch := <-chc
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MaximumBTPMTU}) {
				t.Fatalf("handshake segment sizes %v", got)
			}
			_ = ch.Close()
		})
	})
	t.Run("falls back to the minimum segment size when the ATT_MTU exchange never completes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MaximumBTPMTU)
			peer.p.ATTMTUReady = make(chan struct{})
			chc := make(chan *ble.Channel, 1)
			go func() {
				ch, err := ble.Dial(t.Context(), bletest.NewCentral(peer.p), ble.Peripheral{Address: peer.p.Address}, ble.DialOptions{Clock: clock})
				if err != nil {
					t.Error(err)
				}
				chc <- ch
			}()
			synctest.Wait()
			clock.Advance(ble.ATTMTUSettleTimeout)
			ch := <-chc
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MinimumATTMTU}) {
				t.Fatalf("handshake segment sizes %v", got)
			}
			_ = ch.Close()
		})
	})
	t.Run("raises an abort while the ATT_MTU exchange is still pending", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MaximumBTPMTU)
			peer.p.ATTMTUReady = make(chan struct{})
			ctx, cancel := context.WithCancel(t.Context())
			errc := make(chan error, 1)
			go func() {
				_, err := ble.Dial(ctx, bletest.NewCentral(peer.p), ble.Peripheral{Address: peer.p.Address}, ble.DialOptions{Clock: clock})
				errc <- err
			}()
			synctest.Wait()
			cancel()
			if err := <-errc; !errors.Is(err, context.Canceled) {
				t.Fatalf("Dial = %v", err)
			}
			if len(peer.sizes()) != 0 {
				t.Fatalf("a handshake was written after the abort: %v", peer.sizes())
			}
		})
	})
	t.Run("raises an abort that happened before the ATT_MTU wait started", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			peer := newStallPeer(btp.MaximumBTPMTU)
			peer.p.ATTMTUReady = make(chan struct{})
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err := ble.Dial(ctx, bletest.NewCentral(peer.p), ble.Peripheral{Address: peer.p.Address}, ble.DialOptions{Clock: clocktest.New(time.Unix(0, 0))})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Dial = %v", err)
			}
		})
	})
}

// TestNobleBleChannelRenegotiation covers NobleBleChannelTest.ts:673-746
// and :1021-1045: the renegotiation at the minimum segment size, the
// replay, the sends it holds, and the two ways the peer is given up on.
func TestNobleBleChannelRenegotiation(t *testing.T) {
	message := bytes.Repeat([]byte{0xa1}, 50) // one segment at 244 bytes, three at 20
	t.Run("renegotiates the BTP session with the minimum segment size when the peer answers no data packet", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			ch := peer.dial(t, clock)
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MaximumBTPMTU}) {
				t.Fatalf("handshake segment sizes %v", got)
			}
			stall(t, clock, ch, peer, message)
			synctest.Wait()
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MaximumBTPMTU, btp.MinimumATTMTU}) {
				t.Fatalf("handshake segment sizes %v", got)
			}
			if n := peer.p.Observed().Unsubscribes; n != 1 {
				t.Fatalf("unsubscribes %d, want 1", n)
			}
			if isClosed(ch.Closed()) {
				t.Fatal("the channel closed")
			}
			if got := ch.Conn().Session().FragmentSize(); got != btp.MinimumATTMTU {
				t.Fatalf("renegotiated fragment size %d", got)
			}
			// The replay only reassembles if the size actually dropped.
			select {
			case got := <-peer.got:
				if !bytes.Equal(got, message) {
					t.Fatalf("replayed message %x", got)
				}
			default:
				t.Fatal("the replayed message did not arrive")
			}
			if err := ch.Close(); err != nil {
				t.Fatal(err)
			}
		})
	})
	t.Run("holds a send issued while the BTP session is being renegotiated", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			ch := peer.dial(t, clock)
			second := bytes.Repeat([]byte{0xb2}, 30)
			during := []byte{0xaa, 0xbb, 0xcc, 0xdd}
			stall(t, clock, ch, peer, message, second)
			parked := parkSend(ch, during)
			synctest.Wait()
			if err := <-parked; err != nil {
				t.Fatalf("the held send = %v", err)
			}
			// The replay goes out first, in order, then the message queued
			// while the session was gone.
			var got [][]byte
			for len(peer.got) > 0 {
				got = append(got, <-peer.got)
			}
			want := [][]byte{message, second, during}
			if len(got) != len(want) {
				t.Fatalf("%d messages arrived, want %d", len(got), len(want))
			}
			for i := range want {
				if !bytes.Equal(got[i], want[i]) {
					t.Fatalf("message %d = %x, want %x", i, got[i], want[i])
				}
			}
			_ = ch.Close()
		})
	})
	t.Run("closes the channel when the renegotiation cannot close the peer's BTP session", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.UnsubscribeC2 = errors.New("unsubscribe failed")
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			synctest.Wait()
			if !isClosed(ch.Closed()) {
				t.Fatal("the channel stayed open")
			}
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MaximumBTPMTU}) {
				t.Fatalf("handshake segment sizes %v: a new handshake was attempted", got)
			}
			if n := peer.withheld.Load(); n != 1 {
				t.Fatalf("%d data writes", n)
			}
		})
	})
	t.Run("closes the channel when the renegotiated session is not answered either", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(0)
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			synctest.Wait()
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MaximumBTPMTU, btp.MinimumATTMTU}) {
				t.Fatalf("handshake segment sizes %v", got)
			}
			if isClosed(ch.Closed()) {
				t.Fatal("the channel closed before the renegotiated session timed out")
			}
			// The renegotiated session's acknowledgement timeout: one
			// renegotiation, then the peer is given up on rather than
			// retried forever.
			clock.Advance(btp.AckTimeout)
			synctest.Wait()
			if !isClosed(ch.Closed()) {
				t.Fatal("the channel stayed open")
			}
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MaximumBTPMTU, btp.MinimumATTMTU}) {
				t.Fatalf("handshake segment sizes %v", got)
			}
		})
	})
}

// TestNobleBleChannelRenegotiationLoss covers NobleBleChannelTest.ts:748-996:
// a renegotiation that fails, is abandoned or loses the connection, and
// what a send parked on it learns.
func TestNobleBleChannelRenegotiationLoss(t *testing.T) {
	message := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99}
	small := []byte{0xaa, 0xbb}
	t.Run("reports channel loss to a send parked on a renegotiation that fails", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.UnsubscribeC2 = errors.New("unsubscribe failed")
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			parked := parkSend(ch, small)
			synctest.Wait()
			// A flow error from the suspended session would not read as
			// channel loss downstream.
			if err := <-parked; !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("the parked send = %v", err)
			}
			if !isClosed(ch.Closed()) {
				t.Fatal("the channel stayed open")
			}
		})
	})
	t.Run("abandons a renegotiation when the channel is closed while it waits for the handshake", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.WithholdHandshakeFrom = 2 // the peer never answers the second handshake
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			synctest.Wait()
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MaximumBTPMTU, btp.MinimumATTMTU}) {
				t.Fatalf("handshake segment sizes %v", got)
			}
			if err := ch.Close(); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			// Without the abort the handshake timer would still be armed
			// for its full ConnRspTimeout.
			if slices.Contains(clock.Armed(), btp.ConnRspTimeout) {
				t.Fatal(noHandshakeTimer)
			}
		})
	})
	t.Run("abandons a renegotiation when the peripheral disconnects while it waits for the handshake", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.WithholdHandshakeFrom = 2
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			parked := parkSend(ch, small)
			synctest.Wait()
			peer.p.DropConnections()
			synctest.Wait()
			// No further time passes: the disconnect settles this, not the
			// handshake's own timeout.
			if err := <-parked; !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("the parked send = %v", err)
			}
			if !isClosed(ch.Closed()) {
				t.Fatal("the channel stayed open")
			}
			if slices.Contains(clock.Armed(), btp.ConnRspTimeout) {
				t.Fatal(noHandshakeTimer)
			}
		})
	})
	t.Run("settles a send parked on a renegotiation whose handshake write never completes once the channel closes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.HangHandshakeWriteFrom = 2
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			parked := parkSend(ch, small)
			synctest.Wait()
			if err := ch.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-parked; !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("the parked send = %v", err)
			}
			synctest.Wait()
			if slices.Contains(clock.Armed(), btp.ConnRspTimeout) {
				t.Fatal(noHandshakeTimer)
			}
		})
	})
	t.Run("abandons a renegotiation whose handshake write fails after the peripheral disconnected", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.HangHandshakeWriteFrom = 2
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			parked := parkSend(ch, small)
			synctest.Wait()
			peer.p.DropConnections() // the hung write fails after the disconnect
			if err := <-parked; !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("the parked send = %v", err)
			}
			synctest.Wait()
			if !isClosed(ch.Closed()) {
				t.Fatal("the channel stayed open")
			}
			if slices.Contains(clock.Armed(), btp.ConnRspTimeout) {
				t.Fatal(noHandshakeTimer)
			}
		})
	})
	t.Run("closes the channel when the renegotiation handshake is never answered", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.WithholdHandshakeFrom = 2
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			synctest.Wait()
			clock.Advance(btp.ConnRspTimeout)
			synctest.Wait()
			if !isClosed(ch.Closed()) {
				t.Fatal("the channel stayed open")
			}
			// One unsubscribe ends the stalled session, the second abandons
			// the unanswered handshake.
			if n := peer.p.Observed().Unsubscribes; n != 2 {
				t.Fatalf("unsubscribes %d, want 2", n)
			}
			if got := peer.sizes(); !slices.Equal(got, []int{btp.MaximumBTPMTU, btp.MinimumATTMTU}) {
				t.Fatalf("handshake segment sizes %v", got)
			}
		})
	})
	t.Run("reports channel loss to a send parked on a renegotiation whose replay lost the connection", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.FailDataWrite = 2 // write 1 is the stalled original, write 2 the replay
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			parked := parkSend(ch, small)
			synctest.Wait()
			if err := <-parked; !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("the parked send = %v", err)
			}
			if !isClosed(ch.Closed()) {
				t.Fatal("the channel stayed open")
			}
		})
	})
	t.Run("reports channel loss once its BTP session closed and still disconnects the peripheral on close", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.FailDataWrite = 1
			ch := peer.dial(t, clock)
			if err := ch.Send(nil, message); err != nil {
				t.Fatalf("the send whose write lost the connection = %v", err)
			}
			synctest.Wait()
			if err := ch.Send(nil, small); !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("send after the loss = %v", err)
			}
			if !isClosed(ch.Closed()) {
				t.Fatal("the channel stayed open")
			}
			if err := ch.Close(); err != nil {
				t.Fatal(err)
			}
		})
	})
	t.Run("settles a send parked on a replay whose write never completes once the channel closes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.HangDataWriteFrom = 2 // write 1 is the stalled original, write 2 the first replayed segment
			ch := peer.dial(t, clock)
			stall(t, clock, ch, peer, message)
			parked := parkSend(ch, small)
			synctest.Wait()
			if err := ch.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-parked; !errors.Is(err, btp.ErrDisconnected) {
				t.Fatalf("the parked send = %v", err)
			}
		})
	})
	t.Run("abandons opening a channel when aborted while the handshake write is pending", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			clock := clocktest.New(time.Unix(0, 0))
			peer := newStallPeer(btp.MinimumATTMTU)
			peer.p.Faults.HangHandshakeWriteFrom = 1
			ctx, cancel := context.WithCancel(t.Context())
			errc := make(chan error, 1)
			go func() {
				_, err := ble.Dial(ctx, bletest.NewCentral(peer.p), ble.Peripheral{Address: peer.p.Address}, ble.DialOptions{Clock: clock})
				errc <- err
			}()
			synctest.Wait()
			cancel()
			if err := <-errc; !errors.Is(err, context.Canceled) {
				t.Fatalf("Dial = %v", err)
			}
			// No virtual time passes, so only the abort can have ended the
			// handshake.
			if slices.Contains(clock.Armed(), btp.ConnRspTimeout) {
				t.Fatal(noHandshakeTimer)
			}
		})
	})
}
