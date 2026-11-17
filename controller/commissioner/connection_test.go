// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/secure/channel"
)

// attemptCall is one call of the fake establish function.
type attemptCall struct {
	ctx     context.Context
	addr    string
	device  string
	onPBKDF func()
	result  chan attemptResult
}

type attemptResult struct {
	s   *exchange.Session
	err error
}

// connectionHarness drives connectPASE with a fake establish function.
type connectionHarness struct {
	clock *fakeClock
	mgr   *exchange.Manager
	calls chan *attemptCall
	done  chan struct{}
	// giveUp is connectionConfig.giveUp; set before start.
	giveUp func(*exchange.Session, error)

	mu     sync.Mutex
	result connectionResult
	err    error
	nextID uint16
}

type nullConn struct{}

func (nullConn) Send(*net.UDPAddr, []byte) error { return nil }

func newConnectionHarness(t *testing.T) *connectionHarness {
	t.Helper()
	clock := newFakeClock()
	mgr, err := exchange.NewManager(nullConn{}, exchange.Config{Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	return &connectionHarness{clock: clock, mgr: mgr, calls: make(chan *attemptCall, 64), done: make(chan struct{}), nextID: 0x200}
}

// start runs connectPASE with the devices in the background.
func (h *connectionHarness) start(ctx context.Context, timeout time.Duration, devices ...*candidate) {
	go func() {
		res, err := connectPASE(ctx, connectionConfig{
			devices: devices, timeout: timeout, clock: h.clock, log: discard(), giveUp: h.giveUp,
			establish: func(actx context.Context, addr *net.UDPAddr, c *candidate, onPBKDF func()) (*exchange.Session, error) {
				call := &attemptCall{ctx: actx, addr: addr.String(), device: c.identifier, onPBKDF: onPBKDF, result: make(chan attemptResult, 1)}
				h.calls <- call
				r := <-call.result
				return r.s, r.err
			},
		})
		h.mu.Lock()
		h.result, h.err = res, err
		h.mu.Unlock()
		close(h.done)
	}()
}

// next returns the next launched attempt.
func (h *connectionHarness) next(t *testing.T) *attemptCall {
	t.Helper()
	select {
	case c := <-h.calls:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no attempt launched")
		return nil
	}
}

// armed waits until connectPASE has armed its next-address timer beside
// the budget's: the attempt it launched reaches the test through
// establish before the goroutine arms the timer, so advancing the fake
// clock at once could pass the delay before the timer exists and no
// next attempt would ever launch.
func (h *connectionHarness) armed(t *testing.T) {
	t.Helper()
	synctest.Wait()
	if h.clock.live() < 2 {
		t.Fatal("connectPASE armed no next-address timer")
	}
}

// none checks that no attempt launched once the case's synctest bubble
// has settled.
func (h *connectionHarness) none(t *testing.T) {
	t.Helper()
	synctest.Wait()
	select {
	case c := <-h.calls:
		t.Fatalf("unexpected attempt on %s", c.addr)
	default:
	}
}

// session registers a PASE-like session to return from an attempt.
func (h *connectionHarness) session(t *testing.T) *exchange.Session {
	t.Helper()
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	h.mu.Unlock()
	key := make([]byte, 16)
	s, err := h.mgr.NewSecureSession(exchange.SecureSessionConfig{ID: id, Addr: deviceAddr, Channel: channel.Config{EncryptKey: key, DecryptKey: key, PeerSessionID: id}, PeerParameters: exchange.PeerParameters(nil)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (h *connectionHarness) wait(t *testing.T) (connectionResult, error) {
	t.Helper()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("connectPASE did not return")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.result, h.err
}

func device(id string, ports ...int) *candidate {
	c := &candidate{identifier: id}
	for _, p := range ports {
		c.addrs = append(c.addrs, &net.UDPAddr{IP: net.ParseIP("fd00::1"), Port: p})
	}
	return c
}

// cancelled waits for an attempt's context to end and answers with its
// error, as a cancelled PASE does.
func cancelled(t *testing.T, c *attemptCall) {
	t.Helper()
	select {
	case <-c.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("attempt on %s was not cancelled", c.addr)
	}
	c.result <- attemptResult{err: c.ctx.Err()}
}

// TestConnectionStaggerAndWinner: the second address starts only once the
// first attempt is DelayBeforeNextAddress old (:106-113); the winner
// returns at once and cancels the other attempt (:158-161).
func TestConnectionStaggerAndWinner(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newConnectionHarness(t)
		h.start(context.Background(), time.Minute, device("dev", 1, 2))
		first := h.next(t)
		h.none(t)
		h.armed(t)
		h.clock.Advance(DelayBeforeNextAddress - time.Second)
		h.none(t)
		h.clock.Advance(time.Second)
		second := h.next(t)
		if first.addr == second.addr {
			t.Fatal("the same address twice")
		}
		s := h.session(t)
		second.result <- attemptResult{s: s}
		res, err := h.wait(t)
		if err != nil || res.session != s || res.device.identifier != "dev" {
			t.Fatalf("result %+v, %v", res, err)
		}
		cancelled(t, first)
	})
}

// TestConnectionHoldingResponder: an attempt that received the
// PBKDFParamResponse holds back the next launch (:97, :127-133) — for at
// most DelayBeforeNextAddress from its hold; after it the next address
// starts while the holding attempt runs on, so a responder that answers
// PBKDFParamResponse and never Pake2 does not hold the device's other
// addresses for the whole budget (the module's bound, ahead of matter.js,
// whose hold has none: review II, R2-P6-fsm-1806). A late success after
// the winner is closed (:137-157).
func TestConnectionHoldingResponder(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newConnectionHarness(t)
		h.start(context.Background(), time.Minute, device("dev", 1, 2, 3))
		first := h.next(t)
		first.onPBKDF()
		first.onPBKDF() // once only
		h.armed(t)
		h.clock.Advance(DelayBeforeNextAddress - time.Second)
		h.none(t)
		h.clock.Advance(time.Second)
		second := h.next(t) // the first still holds
		won := h.session(t)
		second.result <- attemptResult{s: won}
		if res, err := h.wait(t); err != nil || res.session != won {
			t.Fatalf("result %v", err)
		}
		late := h.session(t)
		<-first.ctx.Done()
		first.result <- attemptResult{s: late}
		eventually(t, "the late session closed", late.IsPeerLost)
	})
}

// TestConnectionPasscodeMismatchEndsTheAddress: a wrong passcode ends that
// address only — the device's next address is tried, and the mismatch is
// the result only when no address is left (connectedhomeip's rule, ahead
// of matter.js, which drops the device: review II, R2-P6-fsm-1806). A
// responder at one address of the device — a spoofed one — therefore no
// longer decides for the device.
func TestConnectionPasscodeMismatchEndsTheAddress(t *testing.T) {
	t.Parallel()
	t.Run("the device's next address wins", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h := newConnectionHarness(t)
			h.start(context.Background(), time.Minute, device("a", 1, 2))
			first := h.next(t)
			first.onPBKDF()
			first.result <- attemptResult{err: handshake.ErrPasscodeMismatch}
			second := h.next(t)
			if second.device != "a" || second.addr == first.addr {
				t.Fatalf("after the mismatch, an attempt on %s (%s)", second.device, second.addr)
			}
			s := h.session(t)
			second.result <- attemptResult{s: s}
			if res, err := h.wait(t); err != nil || res.session != s {
				t.Fatalf("result %+v, %v", res, err)
			}
		})
	})
	t.Run("the mismatch when no address is left", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			h := newConnectionHarness(t)
			h.start(context.Background(), time.Minute, device("a", 1, 2), device("b", 3))
			for _, want := range []string{"a", "a", "b"} {
				c := h.next(t)
				if c.device != want {
					t.Fatalf("an attempt on %s (%s), want %s", c.device, c.addr, want)
				}
				err := handshake.ErrPasscodeMismatch
				if want == "b" {
					err = fmt.Errorf("%w: lost", exchange.ErrTransient)
				}
				c.result <- attemptResult{err: err}
			}
			if _, err := h.wait(t); !errors.Is(err, handshake.ErrPasscodeMismatch) {
				t.Fatalf("error %v, want the passcode mismatch", err)
			}
		})
	})
}

// TestConnectionLosingGiveUpHoldsTheRun (review II, R2-P3-rr-ctl-05): a
// session established after the run was decided — here by the budget —
// is given up (connectionConfig.giveUp, the commissioner's disarm), and a
// run without a winner returns only once the give-up has returned, so
// its devices are free when Commission returns. (The test's other half,
// the next address launched beside the give-up, needed a device dropped
// for a wrong passcode; no device is dropped since R2-P6-fsm-1806.)
func TestConnectionLosingGiveUpHoldsTheRun(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newConnectionHarness(t)
		givingUp, release := make(chan struct{}), make(chan struct{})
		h.giveUp = func(s *exchange.Session, cause error) {
			close(givingUp)
			<-release
			s.HandlePeerLoss(cause)
		}
		h.start(context.Background(), DelayBeforeNextAddress/2, device("a", 1))
		first := h.next(t)
		first.onPBKDF()
		synctest.Wait()
		h.clock.Advance(DelayBeforeNextAddress / 2)
		<-first.ctx.Done()
		late := h.session(t)
		first.result <- attemptResult{s: late}
		<-givingUp
		synctest.Wait()
		select {
		case <-h.done:
			t.Fatal("the run returned before the losing session's give-up did")
		default:
		}
		close(release)
		var pr *PairRetransmissionLimitReachedError
		if _, err := h.wait(t); !errors.As(err, &pr) {
			t.Fatalf("error %v, want the budget's", err)
		}
		if !late.IsPeerLost() {
			t.Fatal("the losing session was not given up")
		}
	})
}

// TestConnectionBudget: the budget's end without a retryable error is a
// PairRetransmissionLimitReachedError (:226-233); with one, that error
// around it (:234-238); an external cancellation is returned as is.
func TestConnectionBudget(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newConnectionHarness(t)
		h.start(context.Background(), 15*time.Second, device("dev", 1, 2))
		first := h.next(t)
		h.armed(t)
		h.clock.Advance(DelayBeforeNextAddress)
		second := h.next(t)
		h.clock.Advance(5 * time.Second)
		cancelled(t, first)
		cancelled(t, second)
		var pr *PairRetransmissionLimitReachedError
		if _, err := h.wait(t); !errors.As(err, &pr) || pr.Cause != nil {
			t.Fatalf("budget: %v", err)
		}

		h = newConnectionHarness(t)
		h.start(context.Background(), time.Minute, device("x", 1), device("y", 2))
		retry := fmt.Errorf("%w: no answer", exchange.ErrTransient)
		h.next(t).result <- attemptResult{err: retry}
		h.next(t).result <- attemptResult{err: retry}
		if _, err := h.wait(t); !errors.As(err, &pr) || !errors.Is(err, retry) {
			t.Fatalf("retryable: %v", err)
		}

		h = newConnectionHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		h.start(ctx, time.Minute, device("dev", 1))
		call := h.next(t)
		cancel()
		cancelled(t, call)
		if _, err := h.wait(t); !errors.Is(err, context.Canceled) {
			t.Fatalf("external cancellation: %v", err)
		}
	})
}

// TestConnectionErrorClasses: other unexpected data is the non-retryable
// error while the other addresses still run (:171-177); any other error
// aborts the whole connection (:183-187); no candidates at all is a
// PairRetransmissionLimitReachedError (:239).
func TestConnectionErrorClasses(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newConnectionHarness(t)
		h.start(context.Background(), time.Minute, device("dev", 1, 2))
		h.next(t).result <- attemptResult{err: fmt.Errorf("%w: garbage", handshake.ErrUnexpectedData)}
		won := h.session(t)
		h.next(t).result <- attemptResult{s: won}
		if res, err := h.wait(t); err != nil || res.session != won {
			t.Fatalf("after unexpected data: %v", err)
		}

		h = newConnectionHarness(t)
		h.start(context.Background(), time.Minute, device("dev", 1, 2))
		first := h.next(t)
		h.armed(t)
		h.clock.Advance(DelayBeforeNextAddress)
		second := h.next(t)
		fatal := errors.New("fatal")
		first.result <- attemptResult{err: fatal}
		cancelled(t, second)
		if _, err := h.wait(t); !errors.Is(err, fatal) {
			t.Fatalf("fatal: %v", err)
		}

		h = newConnectionHarness(t)
		h.start(context.Background(), time.Minute)
		var pr *PairRetransmissionLimitReachedError
		if _, err := h.wait(t); !errors.As(err, &pr) || pr.Error() == "" {
			t.Fatalf("no candidates: %v", err)
		}
	})
}

// TestAddressesToCandidates: the distinct addresses by desirability; one
// candidate with a device identifier, else one per address (:547-566).
func TestAddressesToCandidates(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v4 := &net.UDPAddr{IP: net.ParseIP("192.168.1.2"), Port: 5540}
		global := &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 5540}
		ula := &net.UDPAddr{IP: net.ParseIP("fd00::1"), Port: 5540}
		cands := addressesToCandidates([]*net.UDPAddr{v4, global, ula, v4}, nil)
		if len(cands) != 3 || cands[0].addrs[0] != ula || cands[1].addrs[0] != global || cands[2].addrs[0] != v4 ||
			cands[0].identifier != "known-address-0-[fd00::1]:5540" {
			t.Fatalf("candidates %+v", cands)
		}
		dd := &discoveryDevice
		one := addressesToCandidates([]*net.UDPAddr{v4, ula}, dd)
		if len(one) != 1 || one[0].identifier != "INST" || len(one[0].addrs) != 2 || one[0].addrs[0] != ula {
			t.Fatalf("one device %+v", one)
		}
	})
}

var discoveryDevice = discovery.Device{InstanceID: "INST"}
