// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// TestTCPMaxMessageSizeResolves (review II, R2-P6-par-3209): the TCP
// message bound resolves as matter.js resolveTcpConfig does — the default
// for zero or without a TCP role, the configured value otherwise — and is
// what the session parameters state.
func TestTCPMaxMessageSizeResolves(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cfg  TCPConfig
		want int
	}{
		{TCPConfig{}, 64000},
		{TCPConfig{MaxMessageSize: 5000}, 64000},
		{TCPConfig{Incoming: true}, 64000},
		{TCPConfig{Outgoing: true, MaxMessageSize: 100_000}, 100_000},
		{TCPConfig{Incoming: true, MaxMessageSize: exchange.MaxUDPMessageSize}, exchange.MaxUDPMessageSize},
	} {
		if got := tc.cfg.maxMessageSize(); got != tc.want {
			t.Errorf("%+v resolves to %d, want %d", tc.cfg, got, tc.want)
		}
		if p := localParameters(tc.cfg, nil); (tc.cfg.Incoming || tc.cfg.Outgoing) && (p == nil || p.MaxTCPMessageSize != uint32(tc.want)) { //nolint:gosec // test values
			t.Errorf("%+v states %+v, want MaxTCPMessageSize %d", tc.cfg, p, tc.want)
		}
	}
	if p := localParameters(TCPConfig{}, nil); p != nil && p.MaxTCPMessageSize != 0 {
		t.Errorf("no TCP role states MaxTCPMessageSize %d", p.MaxTCPMessageSize)
	}
}

// TestTCPMaxMessageSizeRefusals (review II, R2-P6-par-3209): with a TCP
// role, a bound below the UDP message size or above the uint32 maximum is
// ErrConfiguration, as matter.js's assertMaxTcpMessageSize throws; without
// one the field is not used.
func TestTCPMaxMessageSizeRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tcp    TCPConfig
		refuse bool
	}{
		{TCPConfig{Incoming: true, MaxMessageSize: exchange.MaxUDPMessageSize - 1}, true},
		{TCPConfig{Outgoing: true, MaxMessageSize: -1}, true},
		{TCPConfig{Incoming: true, MaxMessageSize: exchange.MaxUDPMessageSize}, false},
		{TCPConfig{MaxMessageSize: 10}, false},
	} {
		if got := !tc.tcp.validMessageSize(); got != tc.refuse {
			t.Errorf("%+v refused %v, want %v", tc.tcp, got, tc.refuse)
		}
	}
	if math.MaxInt > math.MaxUint32 {
		big := TCPConfig{Incoming: true}
		above := int64(math.MaxUint32) + 1
		big.MaxMessageSize = int(above)
		if big.validMessageSize() {
			t.Error("a bound above the uint32 maximum was accepted")
		}
	}
}

// TestTCPListenerHoldsToMaxMessageSize (review II, R2-P6-par-3209): the
// controller's TCP listener closes a connection whose message is longer
// than the configured bound; under the default the same length is taken.
func TestTCPListenerHoldsToMaxMessageSize(t *testing.T) {
	t.Parallel()
	const length = 3000
	for _, tc := range []struct {
		name   string
		max    int
		closed bool
	}{
		{"bound 2000", 2000, true},
		{"default", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			db := openSealerDB(t, filepath.Join(t.TempDir(), "controller.db"))
			cfg := sealerConfig(db, nil)
			cfg.Listen = "[::1]:0"
			cfg.TCP = TCPConfig{Incoming: true, MaxMessageSize: tc.max}
			c, err := New(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close(ctx) }()
			conn, err := net.Dial("tcp", c.TCPAddr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			var prefix [4]byte
			binary.LittleEndian.PutUint32(prefix[:], length)
			if _, err := conn.Write(prefix[:]); err != nil {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, err = conn.Read(make([]byte, 1))
			timedOut := errors.Is(err, os.ErrDeadlineExceeded)
			if tc.closed && timedOut {
				t.Fatalf("a %d-byte message under a %d-byte bound left the connection open", length, tc.max)
			}
			if !tc.closed && !timedOut {
				t.Fatalf("a %d-byte message under the default bound closed the connection: %v", length, err)
			}
		})
	}
}
