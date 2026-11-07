// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// A port of the TCP cases matter.js #4793 added to
// packages/protocol/test/protocol/MessageChannelTest.ts at db021da0
// (:96-215, "MessageChannel maxPayloadSize" and "MessageChannel send over
// TCP"): a session on a TCP channel sends at most the MAX_TCP_MESSAGE_SIZE
// the peer announced under the channel's own limit, the spec default when
// it announced none, never below the UDP message size, and refuses a
// larger message instead of sending it (MessageChannel.ts:77-112,
// Session.ts:229-239). matter.js reads MessageChannel.maxPayloadSize, the
// whole message; the Go session exposes the payload an exchange may send,
// MaxPayloadSize, which is that less the Matter message overhead.

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// sizedChannel is a fake TCP channel with its own message limit.
type sizedChannel struct {
	*fakeChannel
	limit int
}

func (c sizedChannel) MaxMessageSize() int { return c.limit }

const matterMessageOverhead = 26 + 12 + 16

// tcpSessionWith registers a secure session on ch whose peer reported
// reported (nil: nothing).
func tcpSessionWith(t *testing.T, m *Manager, ch Channel, reported *sessionparams.Parameters) *Session {
	t.Helper()
	s, err := m.NewSecureSession(SecureSessionConfig{
		ID: 1, Addr: m.AttachChannel(ch), FabricIndex: 1,
		Channel:        channel.Config{EncryptKey: keyI2R, DecryptKey: keyR2I, LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7001},
		PeerParameters: PeerParameters(reported),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func tcpPeer(maxTCPMessageSize uint32) *sessionparams.Parameters {
	return &sessionparams.Parameters{
		SpecificationVersion: sessionparams.MinTCPSpecificationVersion,
		SupportedTransports:  sessionparams.TransportTCPServer,
		MaxTCPMessageSize:    maxTCPMessageSize,
	}
}

func TestMessageChannelTCPLimitParity(t *testing.T) {
	t.Parallel()
	limitOf := func(t *testing.T, channelLimit int, reported *sessionparams.Parameters) int {
		t.Helper()
		m := bindingManager(t)
		return tcpSessionWith(t, m, sizedChannel{newFakeChannel(5540), channelLimit}, reported).MaxPayloadSize() + matterMessageOverhead
	}
	cases := []struct {
		name         string
		channelLimit int
		reported     *sessionparams.Parameters
		want         int
	}{
		{"caps a TCP channel at the MAX_TCP_MESSAGE_SIZE the peer announced", 64000, tcpPeer(1526), 1526},
		{"keeps the own TCP limit when the peer announces a larger one", 64000, tcpPeer(1_000_000), 64000},
		{"uses the spec default for a TCP peer that announces no limit", 64000, tcpPeer(0), 64000},
		{"uses the spec default for a peer that announces no TCP support", 100_000, nil, 64000},
		{"does not cap a TCP channel below the UDP message size", 64000, tcpPeer(100), MaxUDPMessageSize},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := limitOf(t, c.channelLimit, c.reported); got != c.want {
				t.Fatalf("largest message %d, want %d", got, c.want)
			}
		})
	}
	// matter.js's two non-TCP cases: a channel with large-message support
	// that is not TCP (BTP) and one without it ignore the peer's TCP
	// limit. A Go session without a channel sends UDP or BLE sized.
	t.Run("ignores the peer's TCP limit on a channel without large-message support", func(t *testing.T) {
		t.Parallel()
		m := bindingManager(t)
		s, err := m.NewSecureSession(SecureSessionConfig{
			ID: 1, Addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, FabricIndex: 1,
			Channel:        channel.Config{EncryptKey: keyI2R, DecryptKey: keyR2I, LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7001},
			PeerParameters: PeerParameters(tcpPeer(1000)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := s.MaxPayloadSize(); got != MaxPayloadSize || MaxPayloadSize+matterMessageOverhead != MaxUDPMessageSize {
			t.Fatalf("payload %d, want %d", got, MaxPayloadSize)
		}
	})
	t.Run("refuses a message above the peer's MAX_TCP_MESSAGE_SIZE instead of sending it", func(t *testing.T) {
		t.Parallel()
		m := bindingManager(t)
		ch := newFakeChannel(5540)
		s := tcpSessionWith(t, m, ch, tcpPeer(1526))
		x, err := m.Initiate(s, imProtocolID, Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer x.Close()
		// A payload of the peer's whole limit: with its headers the
		// message is above it (matter.js encodes a 1527-byte message).
		err = x.Send(context.Background(), 1, make([]byte, 1526), SendOptions{SuppressAck: true})
		if !errors.Is(err, ErrMessageTooLarge) || errors.Is(err, ErrTransient) || errors.Is(err, ErrNetwork) {
			t.Fatalf("oversized send: %v, want ErrMessageTooLarge", err)
		}
		ch.mu.Lock()
		sent := len(ch.sent)
		ch.mu.Unlock()
		if sent != 0 {
			t.Fatalf("%d messages reached the channel", sent)
		}
		// The largest payload the session names goes out.
		if err := x.Send(context.Background(), 1, make([]byte, s.MaxPayloadSize()), SendOptions{SuppressAck: true}); err != nil {
			t.Fatalf("send at the limit: %v", err)
		}
	})
}

// TestSessionParametersMaxTCPMessageSizeParity ports the two cases
// matter.js #4793 added to
// packages/protocol/test/session/SessionParametersTest.ts at db021da0
// (:89-95): the SessionParameters factory (here PeerParameters) keeps a
// TCP peer's limit below the default and takes the default for 0
// (SessionParameters.ts:66-73).
func TestSessionParametersMaxTCPMessageSizeParity(t *testing.T) {
	t.Parallel()
	t.Run("honors a peer-reported limit below the default", func(t *testing.T) {
		t.Parallel()
		if got := PeerParameters(tcpPeer(1526)).MaxTCPMessageSize; got != 1526 {
			t.Fatalf("MaxTCPMessageSize %d, want 1526", got)
		}
	})
	t.Run("uses the spec default when the peer reports zero", func(t *testing.T) {
		t.Parallel()
		if got := PeerParameters(tcpPeer(0)).MaxTCPMessageSize; got != sessionparams.DefaultMaxTCPMessageSize {
			t.Fatalf("MaxTCPMessageSize %d, want %d", got, sessionparams.DefaultMaxTCPMessageSize)
		}
	})
}
