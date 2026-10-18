// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

// A port of matter.js packages/protocol/test/ble/BtpSessionHandlerTest.ts
// at 0ceb202d, case by case and in its order. MockTime is fakeClock; a
// matter.js write callback that stays pending is a Go write that blocks on
// a channel, with the call that issued it on its own goroutine.

import (
	"bytes"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

const handshake185w6 = "656c04000000b90006" // versions [4], ATT_MTU 185, window 6

// --- Test Handshake ---

func TestBtpSessionHandshake(t *testing.T) {
	cases := []struct {
		name    string
		maxData int
		req     string
		want    string
	}{
		{"handles a correct Handshake", 100, handshake185w6, "656c04640006"},
		{"handles a zero attMtu in Handshake", 100, "656c04000000000006", "656c04640006"},
		{"handles a undefined maxDataSize in Handshake", 0, "656c04000000000006", "656c04140006"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []byte
			allowClose := false
			s, err := NewPeripheral(c.maxData, unhex(t, c.req), Callbacks{
				Write: func(b []byte) error { got = b; return nil },
				Disconnect: func() error {
					if !allowClose {
						t.Error("Should not be called")
					}
					return nil
				},
				Message: noMessage(t),
			}, Options{TimeSource: newFakeClock()})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, unhex(t, c.want)) {
				t.Fatalf("response %x, want %s", got, c.want)
			}
			allowClose = true
			_ = s.Close()
		})
	}
}

func TestBtpSessionNoSharedVersion(t *testing.T) {
	disconnected := false
	_, err := NewPeripheral(100, unhex(t, "656c05000000000006"), Callbacks{
		Write:      func([]byte) error { t.Error("Should not be called"); return nil },
		Disconnect: func() error { disconnected = true; return nil },
		Message:    noMessage(t),
	}, Options{TimeSource: newFakeClock()})
	wantProtocolError(t, err, "No supported BTP version found in 5")
	if !disconnected {
		t.Fatal("not disconnected")
	}
}

// --- Test Matter Message handling ---

// mmh is the "Test Matter Message handling" fixture: a peripheral after a
// handshake with maxDataSize 20, whose callbacks a case overrides.
type mmh struct {
	clock      *fakeClock
	s          *Session
	write      func([]byte) error
	disconnect func() error
	message    func([]byte) error
}

func newMMH(t *testing.T) *mmh {
	t.Helper()
	f := &mmh{clock: newFakeClock()}
	f.disconnect = noDisconnect(t)
	f.message = noMessage(t)
	var hs []byte
	f.write = func(b []byte) error { hs = b; return nil }
	s, err := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{
		Write:      func(b []byte) error { return f.write(b) },
		Disconnect: func() error { return f.disconnect() },
		Message:    func(b []byte) error { return f.message(b) },
	}, Options{TimeSource: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hs, unhex(t, "656c04140006")) {
		t.Fatalf("handshake response %x", hs)
	}
	f.s = s
	t.Cleanup(func() {
		f.disconnect = func() error { return nil }
		_ = s.Close()
	})
	return f
}

func singleSegment(t *testing.T, ack *uint8, seq uint8, payload []byte) []byte {
	t.Helper()
	return mustEncode(t, Packet{
		Header:  Header{HasAckNumber: ack != nil, IsEndingSegment: true, IsBeginningSegment: true},
		Payload: Payload{AckNumber: ack, SequenceNumber: seq, MessageLength: u16(uint16(len(payload))), SegmentPayload: payload}, //nolint:gosec // test payloads
	})
}

func (f *mmh) expectDisconnectOn(t *testing.T, packets ...[]byte) {
	t.Helper()
	disconnected := false
	f.disconnect = func() error { disconnected = true; return nil }
	for _, p := range packets {
		_ = f.s.HandleIncoming(p)
	}
	if !disconnected {
		t.Fatal("session not disconnected")
	}
}

func TestBtpSessionDisconnectOnAnotherHandshake(t *testing.T) {
	f := newMMH(t)
	f.expectDisconnectOn(t, unhex(t, handshake185w6))
}

func TestBtpSessionDisconnectOnManagementOpcode(t *testing.T) {
	f := newMMH(t)
	f.expectDisconnectOn(t, unhex(t, "0d6c04000000b90006"))
}

func TestBtpSessionOneSegmentMessage(t *testing.T) {
	f := newMMH(t)
	payload := unhex(t, "010203040506070809")
	var handled, written []byte
	f.message = func(b []byte) error {
		handled = b
		return f.s.Send(unhex(t, "090807060504030201"))
	}
	f.write = func(b []byte) error { written = b; return nil }
	if err := f.s.HandleIncoming(singleSegment(t, u8(0), 0, payload)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(handled, payload) {
		t.Fatalf("handled %x", handled)
	}
	if want := unhex(t, "0d00010900090807060504030201"); !bytes.Equal(written, want) {
		t.Fatalf("written %x, want %x", written, want)
	}
}

func TestBtpSessionReceivedMoreThanFragmentSize(t *testing.T) {
	f := newMMH(t)
	f.expectDisconnectOn(t, singleSegment(t, u8(0), 0, unhex(t, "01020304050607080901020304050607080901")))
}

func TestBtpSessionEmptyMessage(t *testing.T) {
	f := newMMH(t)
	var fe *FlowError
	if err := f.s.Send(nil); !errors.As(err, &fe) || fe.Message != "BTP packet must not be empty" {
		t.Fatalf("got %v", err)
	}
}

func TestBtpSessionTwoSegmentMessage(t *testing.T) {
	f := newMMH(t)
	whole := unhex(t, "01020304050607080901020304050607")
	first := mustEncode(t, Packet{
		Header:  Header{HasAckNumber: true, IsBeginningSegment: true},
		Payload: Payload{AckNumber: u8(0), SequenceNumber: 0, MessageLength: u16(uint16(len(whole))), SegmentPayload: unhex(t, "010203040506070809010203040506")},
	})
	second := mustEncode(t, Packet{
		Header:  Header{IsEndingSegment: true, IsContinuingSegment: true},
		Payload: Payload{SequenceNumber: 1, SegmentPayload: unhex(t, "07")},
	})
	var handled []byte
	rec := &recorder{}
	f.message = func(b []byte) error {
		handled = b
		return f.s.Send(unhex(t, "030201090807060504030201090807060504"))
	}
	f.write = func(b []byte) error { rec.add(b); return nil }
	if err := f.s.HandleIncoming(first); err != nil {
		t.Fatal(err)
	}
	if err := f.s.HandleIncoming(second); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(handled, whole) {
		t.Fatalf("handled %x", handled)
	}
	if want := unhex(t, "0901011200030201090807060504030201090807"); !bytes.Equal(rec.at(0), want) {
		t.Fatalf("write 0 %x, want %x", rec.at(0), want)
	}
	if want := unhex(t, "0602060504"); !bytes.Equal(rec.at(1), want) {
		t.Fatalf("write 1 %x, want %x", rec.at(1), want)
	}
}

func TestBtpSessionSendAckTimeout(t *testing.T) {
	f := newMMH(t)
	payload := unhex(t, "010203040506070809")
	var handled, written []byte
	f.message = func(b []byte) error {
		handled = b
		// matter.js arms a 5 s mock timer that sends a reply; the send-ack
		// timer (5 s) fires first, at the same instant.
		f.clock.AfterFunc(SendAckTimeout, func() { _ = f.s.Send(unhex(t, "090807060504030201")) })
		return nil
	}
	f.write = func(b []byte) error {
		if written == nil {
			written = b
		}
		return nil
	}
	if err := f.s.HandleIncoming(singleSegment(t, u8(0), 0, payload)); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(SendAckTimeout)
	if want := unhex(t, "080001"); !bytes.Equal(written, want) {
		t.Fatalf("written %x, want %x", written, want)
	}
	if !bytes.Equal(handled, payload) {
		t.Fatalf("handled %x", handled)
	}
}

func TestBtpSessionAckReceiveTimeout(t *testing.T) {
	f := newMMH(t)
	f.message = func([]byte) error { return f.s.Send(unhex(t, "090807060504030201")) }
	f.write = func([]byte) error { return nil }
	disconnected := false
	f.disconnect = func() error { disconnected = true; return nil }
	if err := f.s.HandleIncoming(singleSegment(t, nil, 0, unhex(t, "010203040506070809"))); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(AckTimeout)
	if !disconnected {
		t.Fatal("not disconnected after the ack timeout")
	}
}

func TestBtpSessionLengthMismatch(t *testing.T) {
	f := newMMH(t)
	p := mustEncode(t, Packet{
		Header:  Header{HasAckNumber: true, IsEndingSegment: true, IsBeginningSegment: true},
		Payload: Payload{AckNumber: u8(0), SequenceNumber: 0, MessageLength: u16(8), SegmentPayload: unhex(t, "010203040506070809")},
	})
	f.expectDisconnectOn(t, p)
}

// getSequenceNumber is the helper of BtpSessionHandlerTest.ts:1520-1526.
func getSequenceNumber(n int) uint8 {
	n++
	if n > 255 {
		n = 0
	}
	return uint8(n) //nolint:gosec // wrapped above
}

func TestBtpSessionRoundsOffSequenceNumbers(t *testing.T) {
	f := newMMH(t)
	payload := unhex(t, "010203040506070809")
	var first []byte
	f.write = func(b []byte) error {
		if first == nil {
			first = b
		}
		return nil
	}
	for i := range 257 {
		handled := false
		f.message = func(b []byte) error {
			handled = bytes.Equal(b, payload)
			return f.s.Send(unhex(t, "090807060504030201"))
		}
		n := getSequenceNumber(i - 1)
		if err := f.s.HandleIncoming(singleSegment(t, u8(n), n, payload)); err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if !handled {
			t.Fatalf("packet %d not handled", i)
		}
	}
	if want := unhex(t, "0d00010900090807060504030201"); !bytes.Equal(first, want) {
		t.Fatalf("first write %x, want %x", first, want)
	}
}

func TestBtpSessionDataWriteDisconnected(t *testing.T) {
	f := newMMH(t)
	writes, disconnects := 0, 0
	f.write = func([]byte) error { writes++; return errors.Join(ErrDisconnected, errors.New("Disconnected 19")) }
	f.disconnect = func() error { disconnects++; return nil }
	if err := f.s.Send(unhex(t, "090807060504030201")); err != nil {
		t.Fatalf("send raised %v", err)
	}
	select {
	case <-f.s.Closed():
	default:
		t.Fatal("not closed")
	}
	if disconnects != 1 {
		t.Fatalf("disconnects %d", disconnects)
	}
	var fe *FlowError
	if err := f.s.Send(unhex(t, "0102030405060708")); !errors.As(err, &fe) {
		t.Fatalf("second send %v, want FlowError", err)
	}
	if writes != 1 {
		t.Fatalf("writes %d", writes)
	}
}

func TestBtpSessionStandaloneAckWriteDisconnected(t *testing.T) {
	f := newMMH(t)
	handled := false
	f.message = func([]byte) error { handled = true; return nil }
	writes := 0
	f.write = func([]byte) error { writes++; return errors.Join(ErrDisconnected, errors.New("Disconnected 19")) }
	f.disconnect = func() error { return nil }
	if err := f.s.HandleIncoming(singleSegment(t, u8(0), 0, unhex(t, "010203040506070809"))); err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("not handled")
	}
	f.clock.Advance(SendAckTimeout)
	select {
	case <-f.s.Closed():
	default:
		t.Fatal("not closed")
	}
	if writes != 1 {
		t.Fatalf("writes %d", writes)
	}
	var fe *FlowError
	if err := f.s.Send(unhex(t, "090807060504030201")); !errors.As(err, &fe) {
		t.Fatalf("send %v, want FlowError", err)
	}
}

// --- Test abort paths ---

func TestBtpSessionAbortPaths(t *testing.T) {
	begin := func(seq uint8, msgLen uint16, payload string) []byte {
		return mustEncode(t, Packet{
			Header:  Header{HasAckNumber: true, IsBeginningSegment: true},
			Payload: Payload{AckNumber: u8(0), SequenceNumber: seq, MessageLength: u16(msgLen), SegmentPayload: unhex(t, payload)},
		})
	}
	cases := []struct {
		name    string
		packets [][]byte
	}{
		// §4.19.4.6: a received sequence number must be the previous + 1.
		{"sequence-number increment violation", [][]byte{mustEncode(t, Packet{
			Header:  Header{HasAckNumber: true, IsEndingSegment: true, IsBeginningSegment: true},
			Payload: Payload{AckNumber: u8(0), SequenceNumber: 5, MessageLength: u16(3), SegmentPayload: unhex(t, "010203")},
		})}},
		// §4.19.4.8: the peripheral sent only sequence 0; an ack for 5 is outside.
		{"out-of-window ack number", [][]byte{mustEncode(t, Packet{
			Header:  Header{HasAckNumber: true, IsEndingSegment: true, IsBeginningSegment: true},
			Payload: Payload{AckNumber: u8(5), SequenceNumber: 0, MessageLength: u16(3), SegmentPayload: unhex(t, "010203")},
		})}},
		// §4.19.4.5: a beginning segment while reassembling.
		{"beginning segment while reassembly is in progress", [][]byte{begin(0, 100, "0102"), begin(1, 100, "0102")}},
		// §4.19.4.5: a continuing segment without a beginning.
		{"continuing segment without a beginning segment", [][]byte{mustEncode(t, Packet{
			Header:  Header{HasAckNumber: true, IsContinuingSegment: true},
			Payload: Payload{AckNumber: u8(0), SequenceNumber: 0, SegmentPayload: unhex(t, "0102")},
		})}},
		// §4.19.4.5: 2 + 3 bytes exceed the declared 3.
		{"continuing segments exceed the declared message length", [][]byte{begin(0, 3, "0102"), mustEncode(t, Packet{
			Header:  Header{IsContinuingSegment: true},
			Payload: Payload{SequenceNumber: 1, SegmentPayload: unhex(t, "030405")},
		})}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			disconnected := false
			s, err := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{
				Write:      func([]byte) error { return nil },
				Disconnect: func() error { disconnected = true; return nil },
				Message:    noMessage(t),
			}, Options{TimeSource: newFakeClock()})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range c.packets {
				_ = s.HandleIncoming(p)
			}
			if !disconnected {
				t.Fatal("not disconnected")
			}
		})
	}
}

// --- Test send-side flow control ---

func TestBtpSessionNoPiggybackBeforeData(t *testing.T) {
	rec := &recorder{}
	ready := false
	s, err := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{
		Write: func(b []byte) error {
			if ready {
				rec.add(b)
			}
			return nil
		},
		Disconnect: func() error { return nil },
		Message:    noMessage(t),
	}, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	ready = true
	if err := s.Send(unhex(t, "0102030405")); err != nil {
		t.Fatal(err)
	}
	if rec.len() != 1 || mustDecode(t, rec.at(0)).Header.HasAckNumber {
		t.Fatalf("writes %d, ack %v", rec.len(), rec.len() > 0 && mustDecode(t, rec.at(0)).Header.HasAckNumber)
	}
	_ = s.Close()
}

func seqs(t *testing.T, ws [][]byte) []uint8 {
	t.Helper()
	out := make([]uint8, len(ws))
	for i, w := range ws {
		out[i] = mustDecode(t, w).Payload.SequenceNumber
	}
	return out
}

func TestBtpSessionReservesLastWindowSlot(t *testing.T) {
	rec := &recorder{}
	c, err := NewCentral(unhex(t, "656c04f40004"), Callbacks{
		Write: func(b []byte) error { rec.add(b); return nil }, Disconnect: func() error { return nil }, Message: noMessage(t),
	}, MaximumBTPMTU, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if err := c.Send(unhex(t, "0102030405")); err != nil {
			t.Fatal(err)
		}
	}
	// Window 4: sequence 0, 1, 2 go out; the fourth slot is reserved.
	if got := seqs(t, rec.writes); !bytes.Equal(got, []uint8{0, 1, 2}) {
		t.Fatalf("sent %v", got)
	}
	if err := c.HandleIncoming(mustEncode(t, Packet{Header: Header{HasAckNumber: true}, Payload: Payload{AckNumber: u8(2), SequenceNumber: 1}})); err != nil {
		t.Fatal(err)
	}
	if got := seqs(t, rec.writes[3:]); !bytes.Equal(got, []uint8{3, 4, 5}) {
		t.Fatalf("after ack %v", got)
	}
	_ = c.Close()
}

// continuing is the "continuing" builder of BtpSessionHandlerTest.ts:821-836.
func continuing(t *testing.T, seq uint8) []byte {
	t.Helper()
	p := Packet{Header: Header{IsContinuingSegment: seq != 0, IsBeginningSegment: seq == 0}, Payload: Payload{SequenceNumber: seq, SegmentPayload: unhex(t, "0102")}}
	if seq == 0 {
		p.Payload.MessageLength = u16(100)
	}
	return mustEncode(t, p)
}

func TestBtpSessionImmediateAckOnLowWindow(t *testing.T) {
	rec := &recorder{}
	ready := false
	s, err := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{
		Write: func(b []byte) error {
			if ready {
				rec.add(b)
			}
			return nil
		},
		Disconnect: func() error { return nil }, Message: noMessage(t),
	}, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	ready = true
	for seq := range uint8(3) {
		_ = s.HandleIncoming(continuing(t, seq))
	}
	if rec.len() != 0 {
		t.Fatalf("writes %d before the threshold", rec.len())
	}
	_ = s.HandleIncoming(continuing(t, 3))
	if rec.len() != 1 {
		t.Fatalf("writes %d", rec.len())
	}
	ack := mustDecode(t, rec.at(0))
	if !ack.Header.HasAckNumber || *ack.Payload.AckNumber != 3 || len(ack.Payload.SegmentPayload) != 0 {
		t.Fatalf("ack %+v", ack)
	}
	_ = s.Close()
}

func TestBtpSessionNonDisconnectWriteError(t *testing.T) {
	writes, disconnects := 0, 0
	c, err := NewCentral(unhex(t, "656c04f40005"), Callbacks{
		Write:      func([]byte) error { writes++; return errors.New("boom") },
		Disconnect: func() error { disconnects++; return nil },
		Message:    noMessage(t),
	}, MaximumBTPMTU, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(unhex(t, "0102")); err == nil || err.Error() != "boom" {
		t.Fatalf("send %v, want boom", err)
	}
	if disconnects != 1 {
		t.Fatalf("disconnects %d", disconnects)
	}
	var fe *FlowError
	if err := c.Send(unhex(t, "0304")); !errors.As(err, &fe) {
		t.Fatalf("second send %v", err)
	}
	if writes != 1 {
		t.Fatalf("writes %d", writes)
	}
}

func TestBtpSessionWriteErrorWhenCloseFailsToo(t *testing.T) {
	c, err := NewCentral(unhex(t, "656c04f40005"), Callbacks{
		Write:      func([]byte) error { return errors.New("boom") },
		Disconnect: func() error { return errors.New("disconnect failed") },
		Message:    noMessage(t),
	}, MaximumBTPMTU, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(unhex(t, "0102")); err == nil || err.Error() != "boom" {
		t.Fatalf("send %v, want boom", err)
	}
}

func TestBtpSessionRejectsSmallSegmentSize(t *testing.T) {
	// matter.js also refuses NaN; Go's int has none.
	_, err := NewCentral(unhex(t, "656c04f40005"), Callbacks{}, MinimumATTMTU-1, Options{})
	if err == nil || !strings.Contains(err.Error(), "at least 20") {
		t.Fatalf("got %v", err)
	}
}

func TestBtpSessionNoDuplicateAckWhenSendInterleavesStandaloneAck(t *testing.T) {
	rec := &recorder{}
	var hang atomic.Bool
	release := make(chan struct{})
	ready := false
	s, err := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{
		Write: func(b []byte) error {
			if !ready {
				return nil
			}
			rec.add(b)
			if hang.Load() && len(mustDecode(t, b).Payload.SegmentPayload) == 0 {
				<-release
			}
			return nil
		},
		Disconnect: func() error { return nil }, Message: noMessage(t),
	}, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	ready = true
	for seq := range uint8(3) {
		_ = s.HandleIncoming(continuing(t, seq))
	}
	if rec.len() != 0 {
		t.Fatalf("writes %d", rec.len())
	}
	hang.Store(true)
	incoming := make(chan error, 1)
	go func() { incoming <- s.HandleIncoming(continuing(t, 3)) }()
	waitFor(t, func() bool { return rec.len() == 1 })
	if a := mustDecode(t, rec.at(0)).Payload.AckNumber; a == nil || *a != 3 {
		t.Fatalf("ack %v", a)
	}
	// The ack holds the send lock: the data is queued, not written.
	if err := s.Send(unhex(t, "0a0b0c")); err != nil {
		t.Fatal(err)
	}
	if rec.len() != 1 {
		t.Fatalf("writes %d while the ack is in flight", rec.len())
	}
	close(release)
	if err := <-incoming; err != nil {
		t.Fatal(err)
	}
	if rec.len() != 2 {
		t.Fatalf("writes %d", rec.len())
	}
	data := mustDecode(t, rec.at(1))
	if len(data.Payload.SegmentPayload) == 0 || data.Header.HasAckNumber {
		t.Fatalf("data packet %+v re-acks", data)
	}
	_ = s.Close()
}

func TestBtpSessionNoStandaloneAckWhileDataWriteInFlight(t *testing.T) {
	rec := &recorder{}
	clock := newFakeClock()
	var hang atomic.Bool
	release := make(chan struct{})
	ready := false
	s, err := NewPeripheral(20, unhex(t, handshake185w6), Callbacks{
		Write: func(b []byte) error {
			if !ready {
				return nil
			}
			rec.add(b)
			if hang.Load() && len(mustDecode(t, b).Payload.SegmentPayload) > 0 {
				<-release
			}
			return nil
		},
		Disconnect: func() error { return nil },
		Message:    func([]byte) error { return nil },
	}, Options{TimeSource: clock})
	if err != nil {
		t.Fatal(err)
	}
	ready = true
	hang.Store(true)
	sending := make(chan error, 1)
	go func() { sending <- s.Send(unhex(t, "0a0b0c")) }()
	waitFor(t, func() bool { return rec.len() == 1 })
	if mustDecode(t, rec.at(0)).Header.HasAckNumber {
		t.Fatal("first data packet carries an ack")
	}
	if err := s.HandleIncoming(singleSegment(t, nil, 0, unhex(t, "0102"))); err != nil {
		t.Fatal(err)
	}
	if rec.len() != 1 {
		t.Fatalf("writes %d after the incoming packet", rec.len())
	}
	clock.Advance(SendAckTimeout)
	if rec.len() != 1 {
		t.Fatalf("writes %d after the send-ack timer", rec.len())
	}
	close(release)
	if err := <-sending; err != nil {
		t.Fatal(err)
	}
	if rec.len() != 2 {
		t.Fatalf("writes %d", rec.len())
	}
	ack := mustDecode(t, rec.at(1))
	if !ack.Header.HasAckNumber || *ack.Payload.AckNumber != 0 || len(ack.Payload.SegmentPayload) != 0 {
		t.Fatalf("flushed ack %+v", ack)
	}
	_ = s.Close()
}

// pair wires a peripheral and a central back to back, each write the
// other's HandleIncoming — the in-memory GATT pair of the matter.js
// cases at :1051-1110 and :1112-1155.
type pair struct {
	peripheral, central *Session
	received            [][]byte
	pClosed, cClosed    bool
}

func newPair(t *testing.T, maxData int, req HandshakeRequest, clock *fakeClock) *pair {
	t.Helper()
	p := &pair{}
	var resp []byte
	ready := false
	var err error
	p.peripheral, err = NewPeripheral(maxData, EncodeHandshakeRequest(req), Callbacks{
		Write: func(b []byte) error {
			if !ready {
				resp = b
				return nil
			}
			return p.central.HandleIncoming(b)
		},
		Disconnect: func() error { p.pClosed = true; return nil },
		Message:    func(m []byte) error { p.received = append(p.received, m); return nil },
	}, Options{TimeSource: clock})
	if err != nil {
		t.Fatal(err)
	}
	ready = true
	p.central, err = NewCentral(resp, Callbacks{
		Write:      func(b []byte) error { return p.peripheral.HandleIncoming(b) },
		Disconnect: func() error { p.cClosed = true; return nil },
		Message:    noMessage(t),
	}, MaximumBTPMTU, Options{TimeSource: clock})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBtpSessionSequenceNumbersWrap(t *testing.T) {
	p := newPair(t, 244, HandshakeRequest{Versions: []int{4}, ATTMTU: 247, ClientWindowSize: 8}, newFakeClock())
	msgs := make([][]byte, 0, 270)
	for i := range 270 {
		m := []byte{0xaa, byte(i >> 8), byte(i)}
		msgs = append(msgs, m)
		if err := p.central.Send(m); err != nil {
			t.Fatal(err)
		}
	}
	if p.cClosed || p.pClosed {
		t.Fatal("a side closed")
	}
	if len(p.received) != len(msgs) {
		t.Fatalf("received %d of %d", len(p.received), len(msgs))
	}
	for i := range msgs {
		if !bytes.Equal(p.received[i], msgs[i]) {
			t.Fatalf("message %d: %x, want %x", i, p.received[i], msgs[i])
		}
	}
	_ = p.central.Close()
	_ = p.peripheral.Close()
}

func TestBtpSessionResumesStalledMultiFragmentMessage(t *testing.T) {
	p := newPair(t, 23, HandshakeRequest{Versions: []int{4}, ATTMTU: 23, ClientWindowSize: 4}, newFakeClock())
	msg := bytes.Repeat(unhex(t, "aabbccdd"), 50)
	if err := p.central.Send(msg); err != nil {
		t.Fatal(err)
	}
	if len(p.received) != 1 || !bytes.Equal(p.received[0], msg) {
		t.Fatalf("received %d", len(p.received))
	}
	_ = p.central.Close()
	_ = p.peripheral.Close()
}

// --- Test stalled peer detection ---

type stallFixture struct {
	clock        *fakeClock
	s            *Session
	rec          *recorder
	disconnected atomic.Bool
}

func centralFor(t *testing.T, attMTU uint16, requested int) *stallFixture {
	t.Helper()
	f := &stallFixture{clock: newFakeClock(), rec: &recorder{}}
	s, err := NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: attMTU, WindowSize: 8}), Callbacks{
		Write:      func(b []byte) error { f.rec.add(b); return nil },
		Disconnect: func() error { f.disconnected.Store(true); return nil },
		Message:    noMessage(t),
	}, requested, Options{TimeSource: f.clock})
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
	return f
}

func isClosed(s *Session) bool {
	select {
	case <-s.Closed():
		return true
	default:
		return false
	}
}

func TestBtpSessionReportsStall(t *testing.T) {
	f := centralFor(t, 244, MaximumBTPMTU)
	stalled := 0
	var replayed [][]byte
	f.s.OnStall(func(r [][]byte) { stalled++; replayed = r })
	first, second := unhex(t, "0102030405"), unhex(t, "060708")
	_ = f.s.Send(first)
	_ = f.s.Send(second)
	if f.rec.len() != 2 {
		t.Fatalf("writes %d", f.rec.len())
	}
	f.clock.Advance(AckTimeout)
	if stalled != 1 || isClosed(f.s) || f.disconnected.Load() {
		t.Fatalf("stalled %d closed %v disconnected %v", stalled, isClosed(f.s), f.disconnected.Load())
	}
	if len(replayed) != 2 || !bytes.Equal(replayed[0], first) || !bytes.Equal(replayed[1], second) {
		t.Fatalf("replayed %x", replayed)
	}
}

func TestBtpSessionClosesWhenNobodyObservesStall(t *testing.T) {
	f := centralFor(t, 244, MaximumBTPMTU)
	_ = f.s.Send(unhex(t, "0102030405"))
	f.clock.Advance(AckTimeout)
	if !isClosed(f.s) || !f.disconnected.Load() {
		t.Fatal("not closed")
	}
}

func TestBtpSessionClosesAtMinimumSegmentSize(t *testing.T) {
	f := centralFor(t, MinimumATTMTU, MaximumBTPMTU)
	stalled := 0
	f.s.OnStall(func([][]byte) { stalled++ })
	_ = f.s.Send(unhex(t, "0102030405"))
	f.clock.Advance(AckTimeout)
	if !isClosed(f.s) || stalled != 0 || !f.disconnected.Load() {
		t.Fatalf("closed %v stalled %d", isClosed(f.s), stalled)
	}
}

func TestBtpSessionFailedWriteNeverReplays(t *testing.T) {
	clock := newFakeClock()
	writes := 0
	c, err := NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 8}), Callbacks{
		Write: func([]byte) error {
			writes++
			if writes > 1 {
				return errors.New("boom")
			}
			return nil
		},
		Disconnect: func() error { return nil },
		Message:    noMessage(t),
	}, MaximumBTPMTU, Options{TimeSource: clock})
	if err != nil {
		t.Fatal(err)
	}
	stalled := 0
	c.OnStall(func([][]byte) { stalled++ })
	_ = c.Send(unhex(t, "0102030405"))
	if err := c.Send(unhex(t, "060708")); err == nil || err.Error() != "boom" {
		t.Fatalf("send %v", err)
	}
	if !isClosed(c) {
		t.Fatal("not closed")
	}
	clock.Advance(AckTimeout)
	if stalled != 0 {
		t.Fatalf("stalled %d", stalled)
	}
}

func TestBtpSessionNoAckForMessageWhoseHandlerClosed(t *testing.T) {
	rec := &recorder{}
	var c *Session
	var err error
	// A window of 3 runs low with the first received packet.
	c, err = NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 3}), Callbacks{
		Write:      func(b []byte) error { rec.add(b); return nil },
		Disconnect: func() error { return nil },
		Message:    func([]byte) error { return c.Close() },
	}, MaximumBTPMTU, Options{TimeSource: newFakeClock()})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.HandleIncoming(singleSegment(t, nil, 1, unhex(t, "0102")))
	if rec.len() != 0 {
		t.Fatalf("writes %d", rec.len())
	}
}

func TestBtpSessionNothingWrittenAfterCloseDuringStandaloneAck(t *testing.T) {
	clock := newFakeClock()
	rec := &recorder{}
	writing := make(chan struct{})
	written := make(chan struct{})
	c, err := NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 8}), Callbacks{
		Write: func(b []byte) error {
			if rec.add(b) == 1 {
				close(writing)
				<-written
			}
			return nil
		},
		Disconnect: func() error { return nil },
		Message:    func([]byte) error { return nil },
	}, MaximumBTPMTU, Options{TimeSource: clock})
	if err != nil {
		t.Fatal(err)
	}
	advanced := make(chan struct{})
	go func() { clock.Advance(SendAckTimeout); close(advanced) }()
	<-writing
	if err := c.Send(unhex(t, "0102030405")); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	close(written)
	<-advanced
	if rec.len() != 1 {
		t.Fatalf("writes %d", rec.len())
	}
	c.mu.Lock()
	running := c.ackReceiveTimer.running
	c.mu.Unlock()
	if running {
		t.Fatal("ack timer still running")
	}
}

func TestBtpSessionClosesWhenOnlyStandaloneAckUnanswered(t *testing.T) {
	f := centralFor(t, 244, MaximumBTPMTU)
	stalled := 0
	f.s.OnStall(func([][]byte) { stalled++ })
	f.clock.Advance(SendAckTimeout)
	if f.rec.len() != 1 {
		t.Fatalf("writes %d", f.rec.len())
	}
	f.clock.Advance(AckTimeout)
	if !isClosed(f.s) || stalled != 0 || !f.disconnected.Load() {
		t.Fatalf("closed %v stalled %d", isClosed(f.s), stalled)
	}
}

func TestBtpSessionClosesOncePeerSentAnything(t *testing.T) {
	clock := newFakeClock()
	disconnected := false
	c, err := NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 8}), Callbacks{
		Write:      func([]byte) error { return nil },
		Disconnect: func() error { disconnected = true; return nil },
		Message:    func([]byte) error { return nil },
	}, MaximumBTPMTU, Options{TimeSource: clock})
	if err != nil {
		t.Fatal(err)
	}
	stalled := 0
	c.OnStall(func([][]byte) { stalled++ })
	_ = c.Send(unhex(t, "0102030405"))
	if err := c.HandleIncoming(singleSegment(t, nil, 1, unhex(t, "aabb"))); err != nil {
		t.Fatal(err)
	}
	clock.Advance(AckTimeout)
	if !isClosed(c) || stalled != 0 || !disconnected {
		t.Fatalf("closed %v stalled %d", isClosed(c), stalled)
	}
}

func TestBtpSessionStopsWritingSuspendedFragments(t *testing.T) {
	clock := newFakeClock()
	rec := &recorder{}
	release := make(chan struct{})
	c, err := NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 8}), Callbacks{
		Write: func(b []byte) error {
			if rec.add(b) == 2 {
				<-release
			}
			return nil
		},
		Disconnect: func() error { return nil },
		Message:    func([]byte) error { return nil },
	}, MaximumBTPMTU, Options{TimeSource: clock})
	if err != nil {
		t.Fatal(err)
	}
	stalled := 0
	c.OnStall(func([][]byte) { stalled++ })
	_ = c.Send(unhex(t, "0102030405"))
	sending := make(chan error, 1)
	go func() { sending <- c.Send(bytes.Repeat([]byte{0xbb}, 500)) }()
	waitFor(t, func() bool { return rec.len() == 2 })
	clock.Advance(AckTimeout)
	if stalled != 1 {
		t.Fatalf("stalled %d", stalled)
	}
	close(release)
	if err := <-sending; err != nil {
		t.Fatal(err)
	}
	if rec.len() != 2 {
		t.Fatalf("writes %d", rec.len())
	}
}

func TestBtpSessionLateWriteFailureAfterStall(t *testing.T) {
	clock := newFakeClock()
	rec := &recorder{}
	fail := make(chan error)
	c, err := NewCentral(EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 244, WindowSize: 8}), Callbacks{
		Write: func(b []byte) error {
			if rec.add(b) == 2 {
				return <-fail
			}
			return nil
		},
		Disconnect: func() error { return nil },
		Message:    func([]byte) error { return nil },
	}, MaximumBTPMTU, Options{TimeSource: clock})
	if err != nil {
		t.Fatal(err)
	}
	var replayed [][]byte
	c.OnStall(func(r [][]byte) { replayed = r })
	pending := bytes.Repeat([]byte{0xbb}, 500)
	_ = c.Send(unhex(t, "0102030405"))
	sending := make(chan error, 1)
	go func() { sending <- c.Send(pending) }()
	waitFor(t, func() bool { return rec.len() == 2 })
	clock.Advance(AckTimeout)
	if len(replayed) != 2 || !bytes.Equal(replayed[1], pending) {
		t.Fatalf("replayed %d", len(replayed))
	}
	fail <- errors.New("late failure")
	if err := <-sending; err != nil {
		t.Fatalf("send failed: %v", err)
	}
}

func TestBtpSessionKeepsOfferedSegmentSize(t *testing.T) {
	f := centralFor(t, 244, MinimumATTMTU)
	_ = f.s.Send(bytes.Repeat([]byte{0xaa}, 60))
	if f.rec.len() <= 1 {
		t.Fatalf("writes %d", f.rec.len())
	}
	for i := range f.rec.len() {
		if len(f.rec.at(i)) > MinimumATTMTU {
			t.Fatalf("packet %d is %d bytes", i, len(f.rec.at(i)))
		}
	}
	_ = f.s.Close()
}
