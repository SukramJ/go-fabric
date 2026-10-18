// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/PeerConnectionErrorHandlerTest.ts at
// 862ad75f: the error handler's precedence and delay contract and the
// network-error rediscovery gate of handleConnectionError.
//
// matter.js tests isolated copies of the expressions; Go drives the
// handler through the connection process (a refused Sigma1 and the pause
// before the next one) and the gate through flagsUnreachable, which the
// process calls. A thrown handler error is Go's returned fatal error.

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// handlerReturning is an error handler that replaces the delay with d.
func handlerReturning(d time.Duration) ErrorHandler {
	return func(error) (time.Duration, bool, error) { return d, true, nil }
}

// refusedThenPause refuses the first Sigma1 with a peer error and checks
// that the next Sigma1 follows exactly d later.
func refusedThenPause(t *testing.T, h *harness, d time.Duration) {
	t.Helper()
	h.status(h.sigma1(t, addr1), addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolInvalidParameter, nil)
	h.clock.waitTimer(t, d)
	h.clock.Advance(d - time.Millisecond)
	if !h.clock.hasTimer(time.Millisecond) {
		t.Fatalf("the %v pause ended early", d)
	}
	h.clock.Advance(time.Millisecond)
	h.sigma1(t, addr1)
}

// startRefused starts a process at addr1 with the set's handler ctxHandler
// and the connection's handler optHandler.
func startRefused(t *testing.T, ctxHandler, optHandler ErrorHandler) (*harness, <-chan connectResult) {
	t.Helper()
	h := newHarness(t, func(c *Context) { c.HandleError = ctxHandler })
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	return h, connectAsync(p, ConnectOptions{ConnectionTimeout: -1, HandleError: optHandler})
}

// TestPeerConnectionErrorHandlerSelection is the describe "handler
// selection precedence".
func TestPeerConnectionErrorHandlerSelection(t *testing.T) {
	t.Parallel()
	t.Run("prefers per-connection handler over context handler", func(t *testing.T) {
		t.Parallel()
		h, _ := startRefused(t, handlerReturning(99*time.Second), handlerReturning(time.Second)) // :63-64
		refusedThenPause(t, h, time.Second)
	})
	t.Run("falls back to context handler when no per-connection handler", func(t *testing.T) {
		t.Parallel()
		h, _ := startRefused(t, handlerReturning(99*time.Second), nil) // :70
		refusedThenPause(t, h, 99*time.Second)
	})
	t.Run("returns undefined when neither handler is set", func(t *testing.T) {
		t.Parallel()
		// No handler: the delay table's peer-error pause stands.
		h, _ := startRefused(t, nil, nil)
		refusedThenPause(t, h, DefaultTiming().DelayAfterPeerError)
	})
}

// TestPeerConnectionErrorHandlerDelayContract is the describe "delay
// override contract" (the override and the keep cases are
// TestHandleErrorOverrideAndFatal).
func TestPeerConnectionErrorHandlerDelayContract(t *testing.T) {
	t.Parallel()
	defaultDelay := 5 * time.Minute // :82, DelayAfterPeerError
	t.Run("signals abort when handler throws", func(t *testing.T) {
		t.Parallel()
		fatal := errors.New("fatal")
		h, res := startRefused(t, nil, func(error) (time.Duration, bool, error) { return 0, false, fatal })
		h.status(h.sigma1(t, addr1), addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolInvalidParameter, nil)
		if r := waitResult(t, res); !errors.Is(r.err, fatal) {
			t.Fatalf("Connect = %v, want the handler's error", r.err)
		}
	})
	t.Run("keeps default delay when no handler is set", func(t *testing.T) {
		t.Parallel()
		h, _ := startRefused(t, nil, nil)
		refusedThenPause(t, h, defaultDelay)
	})
}

// socketError is a send failure as controller/exchange returns it
// (exchange.go: ErrNetwork wrapping the socket's error).
func socketError(errno syscall.Errno) error {
	op := &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendto", errno)}
	return fmt.Errorf("%w: %w", exchange.ErrNetwork, op)
}

// TestPeerConnectionErrorHandlerRediscoveryGate is the describe "network
// error rediscovery gate".
func TestPeerConnectionErrorHandlerRediscoveryGate(t *testing.T) {
	t.Parallel()
	t.Run("flags unreachable for NetworkUnreachableError (ENETUNREACH)", func(t *testing.T) {
		t.Parallel()
		if !flagsUnreachable(socketError(syscall.ENETUNREACH)) {
			t.Fatal("ENETUNREACH not flagged")
		}
		// And through the process: the failing send marks the service.
		h := newHarness(t, nil)
		failure := socketError(syscall.ENETUNREACH)
		h.udp.fail.Store(&failure)
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
		if !p.Service().Reachable() {
			t.Fatal("unreachable before the attempt")
		}
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		eventually(t, "the service to be marked unreachable", func() bool { return !p.Service().Reachable() })
	})
	t.Run("flags unreachable for AddressUnreachableError (EHOSTUNREACH)", func(t *testing.T) {
		t.Parallel()
		if !flagsUnreachable(socketError(syscall.EHOSTUNREACH)) {
			t.Fatal("EHOSTUNREACH not flagged")
		}
	})
	t.Run("flags unreachable when a network error is wrapped as a cause", func(t *testing.T) {
		t.Parallel()
		if !flagsUnreachable(fmt.Errorf("connect failed: %w", socketError(syscall.ENETUNREACH))) {
			t.Fatal("a wrapped network error not flagged")
		}
	})
	t.Run("does not flag unreachable for transient peer errors (handled by MRP retransmission path)", func(t *testing.T) {
		t.Parallel()
		if flagsUnreachable(&exchange.PeerUnresponsiveError{}) {
			t.Fatal("a transient peer error flagged")
		}
	})
	t.Run("does not flag unreachable for generic errors", func(t *testing.T) {
		t.Parallel()
		if flagsUnreachable(errors.New("something else")) {
			t.Fatal("a generic error flagged")
		}
	})
}
