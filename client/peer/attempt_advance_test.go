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
// OperationalSessionSetup.cpp:457-480); a Sigma1 the node never
// acknowledges advances when its contact schedule switches to fresh ones.
func TestFailedAttemptAdvances(t *testing.T) {
	t.Parallel()
	start := func(t *testing.T, edit ...func(*Context)) (*harness, *Peer) {
		t.Helper()
		var e func(*Context)
		if len(edit) > 0 {
			e = edit[0]
		}
		h := newHarness(t, e)
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
	t.Run("unacknowledged Sigma1", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			// A stagger of an hour: the second address can only start
			// through the switch of the first's contact schedule.
			h, p := start(t, func(c *Context) { c.Timing = &Timing{DelayBeforeNextAddress: time.Hour} })
			began := h.clock.Now()
			connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
			for {
				synctest.Wait()
				seen := false
				for drained := false; !drained && !seen; {
					select {
					case d := <-h.udp.ch:
						seen = d.to.String() == addr2 && decodeUnsecured(t, d.bytes).proto.Opcode == mrp.SCOpcodeSigma1
					default:
						drained = true
					}
				}
				if seen {
					break
				}
				if h.clock.Now().Sub(began) > 10*time.Minute {
					t.Fatal("no attempt at the second address within 10 minutes")
				}
				h.clock.Advance(time.Second)
			}
			if d := h.clock.Now().Sub(began); d < DefaultTiming().MaxDelayBetweenInitialContactRetries {
				t.Fatalf("the second address began after %v, before the first's Sigma1 could switch", d)
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
