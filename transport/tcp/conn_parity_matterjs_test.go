// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

// A port of matter.js packages/protocol/test/transport/tcp/TcpChannelTest.ts
// at 862ad75f: the TCP channel's send-after-close, properties, name,
// size limits, zero-length frames, close and message delivery.
//
// The Go surface differs: TcpChannel is *Conn, its onMessage listener is
// the Handler, its onClose listener is Closed(), and the async iterator
// has no Go counterpart beyond the same Handler-then-Closed stream. The
// MockTcpConnection pair is net.Pipe, its remote address overridden.

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport"
)

// tcpChannelPipe is one end of net.Pipe that reports the given remote.
type tcpChannelPipe struct {
	net.Conn
	remote net.Addr
}

func (p tcpChannelPipe) RemoteAddr() net.Addr { return p.remote }

// tcpChannelPair mirrors MockTcpConnection.createPair: client is the
// raw peer end, c the served channel on the other end, its remote
// ip:port. Every delivered message lands on msgs.
func tcpChannelPair(t *testing.T, ip string, port int) (client net.Conn, c *Conn, msgs chan []byte) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	c = newConn(tcpChannelPipe{b, &net.TCPAddr{IP: net.ParseIP(ip), Port: port}}, 0, false, nil, nil)
	msgs = make(chan []byte, 8)
	go c.serve(func(_ *Conn, msg []byte, _ func()) { msgs <- msg })
	t.Cleanup(func() { _ = c.Close() })
	return a, c, msgs
}

func tcpChannelLengthHeader(n int) []byte {
	h := make([]byte, 4)
	binary.LittleEndian.PutUint32(h, uint32(n)) //nolint:gosec // test sizes are small
	return h
}

func tcpChannelWrite(t *testing.T, w net.Conn, b []byte) {
	t.Helper()
	if _, err := w.Write(b); err != nil {
		t.Fatalf("peer write: %v", err)
	}
}

func tcpChannelAwaitClosed(t *testing.T, c *Conn) {
	t.Helper()
	select {
	case <-c.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("channel not closed")
	}
}

func tcpChannelAwaitMsg(t *testing.T, msgs <-chan []byte) []byte {
	t.Helper()
	select {
	case m := <-msgs:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no message delivered")
		return nil
	}
}

func tcpChannelNoMsg(t *testing.T, msgs <-chan []byte) {
	t.Helper()
	select {
	case m := <-msgs:
		t.Fatalf("message %x delivered, want none", m)
	default:
	}
}

func TestTcpChannelSendAfterClose(t *testing.T) {
	t.Run("rejects with a typed error rather than an untyped one", func(t *testing.T) {
		_, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		_ = c.Close()
		// TransportClosedError is the typed ErrConnClosed (TcpChannel.ts:120-122).
		err := c.Send([]byte{0xde, 0xad, 0xbe, 0xef})
		if !errors.Is(err, ErrConnClosed) {
			t.Fatalf("send after close: %v, want ErrConnClosed", err)
		}
	})
}

func TestTcpChannelChannelProperties(t *testing.T) {
	t.Run("reports correct channel type properties", func(t *testing.T) {
		_, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		var tc transport.Conn = c
		if _, ok := tc.(*Conn); !ok || !tc.Reliable() {
			t.Fatalf("type %T reliable=%v, want a reliable *tcp.Conn", tc, tc.Reliable())
		}
		// supportsLargeMessages: the TCP frame carries far more than a
		// UDP datagram's 1280 bytes.
		if got := c.MaxMessageSize(); got != DefaultMaxPayloadSize-FramingHeaderSize {
			t.Fatalf("MaxMessageSize = %d, want %d", got, DefaultMaxPayloadSize-FramingHeaderSize)
		}
	})
	t.Run("is recognized as ConnectedChannel", func(t *testing.T) {
		_, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		// isConnectedChannel: reliable and observable close (Channel.ts:87-89).
		var tc transport.Conn = c
		cc, ok := tc.(interface{ Closed() <-chan struct{} })
		if !ok || !tc.Reliable() || cc.Closed() == nil {
			t.Fatal("tcp.Conn is not a connected channel")
		}
	})
}

func TestTcpChannelNetworkAddress(t *testing.T) {
	t.Run("returns a ServerAddressTcp with correct values", func(t *testing.T) {
		_, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		addr, ok := c.Remote().(*net.TCPAddr)
		if !ok || !addr.IP.Equal(net.ParseIP("1.2.3.4")) || addr.Port != 5000 {
			t.Fatalf("Remote = %#v, want tcp 1.2.3.4:5000", c.Remote())
		}
	})
}

func TestTcpChannelName(t *testing.T) {
	t.Run("returns tcp:// formatted name", func(t *testing.T) {
		_, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		if got := c.String(); got != "tcp://1.2.3.4:5000" {
			t.Fatalf("name = %q", got)
		}
	})
	t.Run("wraps IPv6 addresses in brackets", func(t *testing.T) {
		_, c, _ := tcpChannelPair(t, "::1", 5000)
		if got := c.String(); got != "tcp://[::1]:5000" {
			t.Fatalf("name = %q", got)
		}
	})
}

func TestTcpChannelOversizedMessageHandling(t *testing.T) {
	t.Run("rejects message at the size limit", func(t *testing.T) {
		client, c, msgs := tcpChannelPair(t, "1.2.3.4", 5000)
		// 64000 exceeds the 63996-byte message limit (TcpChannelTest.ts:238).
		tcpChannelWrite(t, client, tcpChannelLengthHeader(DefaultMaxPayloadSize))
		tcpChannelAwaitClosed(t, c)
		tcpChannelNoMsg(t, msgs)
		if !errors.Is(c.Err(), ErrFrameTooLarge) {
			t.Fatalf("close reason %v", c.Err())
		}
	})
	t.Run("accepts message just under the size limit", func(t *testing.T) {
		client, c, msgs := tcpChannelPair(t, "1.2.3.4", 5000)
		size := DefaultMaxPayloadSize - 4 - 1 // 63995, TcpChannelTest.ts:258
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i)
		}
		tcpChannelWrite(t, client, append(tcpChannelLengthHeader(size), payload...))
		if got := tcpChannelAwaitMsg(t, msgs); len(got) != size || got[size-1] != byte(size-1) {
			t.Fatalf("delivered %d bytes, want %d", len(got), size)
		}
		select {
		case <-c.Closed():
			t.Fatalf("closed: %v", c.Err())
		default:
		}
	})
	t.Run("rejects way-oversized message", func(t *testing.T) {
		client, c, msgs := tcpChannelPair(t, "1.2.3.4", 5000)
		tcpChannelWrite(t, client, tcpChannelLengthHeader(100_000))
		tcpChannelAwaitClosed(t, c)
		tcpChannelNoMsg(t, msgs)
	})
}

func TestTcpChannelZeroLengthFrameHandling(t *testing.T) {
	t.Run("rejects and closes on a zero-length frame", func(t *testing.T) {
		client, c, msgs := tcpChannelPair(t, "1.2.3.4", 5000)
		tcpChannelWrite(t, client, tcpChannelLengthHeader(0))
		tcpChannelAwaitClosed(t, c)
		tcpChannelNoMsg(t, msgs)
		if !errors.Is(c.Err(), ErrZeroLengthFrame) {
			t.Fatalf("close reason %v", c.Err())
		}
	})
	t.Run("closes without processing a valid message that follows a zero-length frame", func(t *testing.T) {
		client, c, msgs := tcpChannelPair(t, "1.2.3.4", 5000)
		tcpChannelWrite(t, client, append(tcpChannelLengthHeader(0), frameOf([]byte{1, 2, 3})...))
		tcpChannelAwaitClosed(t, c)
		tcpChannelNoMsg(t, msgs)
	})
}

func TestTcpChannelSendSideSizeCheck(t *testing.T) {
	t.Run("rejects messages above the size limit", func(t *testing.T) {
		_, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		// matter.js matches the text "exceeds TCP limit"; Go matches the sentinel.
		if err := c.Send(make([]byte, c.MaxMessageSize()+1)); !errors.Is(err, ErrMessageTooLarge) {
			t.Fatalf("oversized send: %v", err)
		}
	})
	t.Run("accepts messages exactly at the size limit", func(t *testing.T) {
		client, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		drained := make(chan error, 1)
		go func() {
			_, err := io.ReadFull(client, make([]byte, FramingHeaderSize+c.MaxMessageSize()))
			drained <- err
		}()
		if err := c.Send(make([]byte, c.MaxMessageSize())); err != nil {
			t.Fatalf("send at limit: %v", err)
		}
		if err := <-drained; err != nil {
			t.Fatalf("peer read: %v", err)
		}
	})
}

func TestTcpChannelClose(t *testing.T) {
	t.Run("closes the underlying socket", func(t *testing.T) {
		client, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		_ = c.Close()
		_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Fatalf("peer read after close: %v, want EOF", err)
		}
	})
	t.Run("fires close listeners", func(t *testing.T) {
		_, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		_ = c.Close()
		select {
		case <-c.Closed():
		default:
			t.Fatal("Closed not signalled by Close")
		}
	})
}

// The async iterator maps to the Go stream: messages through the
// Handler in order, the end through Closed().
func TestTcpChannelAsyncIteration(t *testing.T) {
	t.Run("yields messages via for-await", func(t *testing.T) {
		client, _, msgs := tcpChannelPair(t, "1.2.3.4", 5000)
		tcpChannelWrite(t, client, frameOf([]byte{0xaa, 0xbb}))
		tcpChannelWrite(t, client, frameOf([]byte{0xcc, 0xdd}))
		if m := tcpChannelAwaitMsg(t, msgs); string(m) != "\xaa\xbb" {
			t.Fatalf("first %x", m)
		}
		if m := tcpChannelAwaitMsg(t, msgs); string(m) != "\xcc\xdd" {
			t.Fatalf("second %x", m)
		}
	})
	t.Run("terminates iterator on close", func(t *testing.T) {
		_, c, msgs := tcpChannelPair(t, "1.2.3.4", 5000)
		_ = c.Close()
		tcpChannelAwaitClosed(t, c)
		tcpChannelNoMsg(t, msgs)
	})
	t.Run("terminates iterator on remote disconnect", func(t *testing.T) {
		client, c, _ := tcpChannelPair(t, "1.2.3.4", 5000)
		_ = client.Close()
		tcpChannelAwaitClosed(t, c)
	})
	t.Run("delivers queued messages then terminates on close", func(t *testing.T) {
		client, c, msgs := tcpChannelPair(t, "1.2.3.4", 5000)
		tcpChannelWrite(t, client, frameOf([]byte{0x12, 0x34}))
		_ = client.Close()
		tcpChannelAwaitClosed(t, c)
		// The message completed before the remote end reached the
		// handler before Closed fired (TcpChannel.ts:85-98, :183-185).
		select {
		case m := <-msgs:
			if string(m) != "\x12\x34" {
				t.Fatalf("delivered %x", m)
			}
		default:
			t.Fatal("message completed before the remote close was dropped")
		}
	})
}
