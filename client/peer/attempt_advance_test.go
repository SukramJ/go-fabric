// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestFailedAttemptAdvances: an attempt that fails without reaching the
// node starts the next pending address at once, not
// DelayBeforeNextAddress after the first began — a send failure at once,
// a CASE timeout (Sigma1 acknowledged, Sigma2 missing) when it expires;
// a status report from the node keeps the stagger (issue 478, chip's
// OperationalSessionSetup.cpp:457-480).
func TestFailedAttemptAdvances(t *testing.T) {
	t.Parallel()
	start := func(t *testing.T) (*harness, *Peer) {
		t.Helper()
		h := newHarness(t, nil)
		p, _ := h.set.For(testAddr)
		h.announceAddrs(t, "fd00::1", "fd00::2")
		eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
		return h, p
	}
	t.Run("send failure", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h, p := start(t)
			h.udp.failFor.Store(&sendFailure{to: addr1, err: socketError(syscall.EHOSTUNREACH)})
			began := h.clock.Now()
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			h.sigma1(t, addr2)
			if d := h.clock.Now().Sub(began); d != 0 {
				t.Fatalf("the second address began after %v", d)
			}
		})
	})
	t.Run("CASE timeout", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h, p := start(t)
			began := h.clock.Now()
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			h.reply(h.sigma1(t, addr1), addr1, mrp.StandaloneAckOpcode, nil)
			timeout := caseResponseTimeout()
			if timeout >= 45*time.Second {
				t.Fatalf("the CASE timeout %v is not below the stagger: the case shows nothing", timeout)
			}
			h.clock.waitTimer(t, timeout)
			h.clock.Advance(timeout)
			h.sigma1(t, addr2)
			if d := h.clock.Now().Sub(began); d >= 45*time.Second {
				t.Fatalf("the second address began after %v", d)
			}
		})
	})
	t.Run("status report keeps the stagger", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h, p := start(t)
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			h.status(h.sigma1(t, addr1), addr1, mrp.SCStatusGeneralFailure, mrp.SCStatusProtocolInvalidParameter, nil)
			h.clock.waitTimer(t, 45*time.Second)
			h.noSigma1(t, addr2)
			h.clock.Advance(45*time.Second - time.Millisecond)
			h.noSigma1(t, addr2)
			h.clock.Advance(time.Millisecond)
			h.sigma1(t, addr2)
		})
	})
}
