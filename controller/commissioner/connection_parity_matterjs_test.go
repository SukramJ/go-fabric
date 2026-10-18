// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/protocol/test/peer/CommissioningConnectionTest.ts at
// 862ad75f: the PASE connection over a device's candidate addresses — the
// per-address stagger, the responder hold, the credential drop, the
// session closes of late and losing attempts and the abort paths.
//
// The cases already ported by TestConnection* in connection_test.go are not
// repeated here. connectPASE is CommissioningConnection; an establish
// script is establishSession, its ctx the attempt's signal, a session's
// close cause is what the manager's PeerLost hook receives
// (initiateForceClose's cause). matter.js's errors map to the classes
// isRetryable names: NoResponseTimeoutError is exchange.ErrTransient,
// AddressUnreachableError and NetworkUnreachableError are
// exchange.ErrNetwork, UnexpectedDataError is handshake.ErrUnexpectedData,
// an AbortController's reason is the ctx's cancel cause.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/secure/channel"
)

// ccAttempt is one establishSession call.
type ccAttempt struct {
	ctx     context.Context
	ip      string
	device  string
	onPBKDF func()
}

// ccScript is a test's establishSession.
type ccScript func(h *ccHarness, a *ccAttempt) (*exchange.Session, error)

// ccHarness runs one connectPASE on a clocktest.Fake. running counts the
// scripts that are not blocked on the fake, a timer, a gate or their ctx,
// so settle can wait for every script to reach its next wait — MockTime's
// yield, which Go replaces by waiting for the effect.
type ccHarness struct {
	t       *testing.T
	clock   *clocktest.Fake
	mgr     *exchange.Manager
	running atomic.Int64
	done    chan struct{}

	pos map[string]int // queue position by label

	mu     sync.Mutex
	labels []string
	ctxs   []context.Context
	lost   map[*exchange.Session]error
	nextID uint16
	res    connectionResult
	err    error
}

// ccOptions is CommissioningConnection.Options without establishSession.
type ccOptions struct {
	ctx     context.Context // externalAbort
	devices []*candidate
	timeout time.Duration
	delay   *time.Duration // nil: the default
}

func ccDelay(d time.Duration) *time.Duration { return &d }

// ccDevice is CommissioningConnectionTest.ts:27-34 (device) with udp's
// port 5540 (:23-25).
func ccDevice(id string, ips ...string) *candidate {
	c := &candidate{identifier: id}
	for _, ip := range ips {
		c.addrs = append(c.addrs, &net.UDPAddr{IP: net.ParseIP(ip), Port: 5540})
	}
	return c
}

func ccStart(t *testing.T, o ccOptions, script ccScript) *ccHarness {
	t.Helper()
	h := &ccHarness{t: t, clock: clocktest.New(time.Unix(1_700_000_000, 0)), done: make(chan struct{}), lost: make(map[*exchange.Session]error), nextID: 0x300}
	mgr, err := exchange.NewManager(nullConn{}, exchange.Config{PeerLost: func(s *exchange.Session, cause error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.lost[s] = cause
	}})
	if err != nil {
		t.Fatal(err)
	}
	h.mgr = mgr
	t.Cleanup(mgr.Close)
	h.pos = make(map[string]int)
	for _, d := range o.devices {
		for _, a := range d.addrs {
			h.pos[d.identifier+":"+a.IP.String()] = len(h.pos)
		}
	}
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer close(h.done)
		res, err := connectPASE(ctx, connectionConfig{
			devices: o.devices, timeout: o.timeout, delay: o.delay, clock: h.clock, log: discard(),
			establish: func(actx context.Context, addr *net.UDPAddr, c *candidate, onPBKDF func()) (*exchange.Session, error) {
				h.running.Add(1)
				defer h.running.Add(-1)
				a := &ccAttempt{ctx: actx, ip: addr.IP.String(), device: c.identifier, onPBKDF: onPBKDF}
				h.mu.Lock()
				h.labels = append(h.labels, a.device+":"+a.ip)
				h.ctxs = append(h.ctxs, actx)
				h.mu.Unlock()
				return script(h, a)
			},
		})
		h.mu.Lock()
		h.res, h.err = res, err
		h.mu.Unlock()
	}()
	h.until("the first attempt", func() bool { return len(h.order()) > 0 })
	h.settle()
	return h
}

// until waits for cond, yielding the fake's way; it fails after about 5 s.
func (h *ccHarness) until(what string, cond func() bool) {
	h.t.Helper()
	for range 5000 {
		if cond() {
			return
		}
		h.clock.Yield()
	}
	h.t.Fatalf("timed out waiting for %s", what)
}

// settle waits until every launched script waits again, then yields for
// the connection's own bookkeeping after a script returned.
func (h *ccHarness) settle() {
	h.t.Helper()
	h.until("the scripts to wait", func() bool { return h.running.Load() == 0 })
	for range 3 {
		h.clock.Yield()
	}
}

// at is the scripted describe's at (:676-680).
func (h *ccHarness) at(d time.Duration) {
	h.t.Helper()
	h.clock.Advance(d)
	h.settle()
}

// armed waits until the fake holds n timers: the initial schedule arms
// the next-address timer after it launched the first attempt.
func (h *ccHarness) armed(n int) {
	h.t.Helper()
	h.until(fmt.Sprintf("%d armed timers", n), func() bool { return h.clock.Pending() >= n })
}

// order is the attempts' addresses in launch order. The scheduler
// launches from the queue's head, so launch order is queue order; the
// scripts record from their own goroutines, so the record is sorted by
// queue position — what is checked is which attempts started by when.
func (h *ccHarness) order() []string {
	l := h.labelled()
	for i, s := range l {
		l[i] = s[strings.IndexByte(s, ':')+1:]
	}
	return l
}

// labelled is order as "device:ip".
func (h *ccHarness) labelled() []string {
	h.mu.Lock()
	l := slices.Clone(h.labels)
	h.mu.Unlock()
	slices.SortStableFunc(l, func(a, b string) int { return h.pos[a] - h.pos[b] })
	return l
}

// expectOrder waits for the order to reach want and checks it is exactly want.
func (h *ccHarness) expectOrder(get func() []string, want ...string) {
	h.t.Helper()
	h.until(fmt.Sprintf("order %v", want), func() bool { return len(get()) >= len(want) })
	h.settle()
	if got := get(); !slices.Equal(got, want) {
		h.t.Fatalf("order %v, want %v", got, want)
	}
}

// result waits for connectPASE to return.
func (h *ccHarness) result() (connectionResult, error) {
	h.t.Helper()
	h.until("the connection's result", func() bool {
		select {
		case <-h.done:
			return true
		default:
			return false
		}
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.res, h.err
}

// session is a PASE-like session for a script to return.
func (h *ccHarness) session() *exchange.Session {
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	h.mu.Unlock()
	key := make([]byte, 16)
	s, err := h.mgr.NewSecureSession(exchange.SecureSessionConfig{ID: id, Addr: deviceAddr, Channel: channel.Config{EncryptKey: key, DecryptKey: key, PeerSessionID: id}, PeerParameters: exchange.PeerParameters(nil)})
	if err != nil {
		panic(err)
	}
	return s
}

// closedWith waits until s was force-closed and returns the cause.
func (h *ccHarness) closedWith(s *exchange.Session) error {
	h.t.Helper()
	var cause error
	h.until("the session close", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		c, ok := h.lost[s]
		cause = c
		return ok
	})
	return cause
}

func (h *ccHarness) isClosed(s *exchange.Session) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.lost[s]
	return ok
}

// block waits, not counted as running, for ch or ctx (a nil ch: ctx
// only); it reports whether ch fired.
func (h *ccHarness) block(ctx context.Context, ch <-chan struct{}) bool {
	h.running.Add(-1)
	defer h.running.Add(1)
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

// sleep is Time.sleep on the fake: the timer's callback marks the script
// running before it wakes, so settle sees it at once.
func (h *ccHarness) sleep(d time.Duration) {
	ch := make(chan struct{})
	h.clock.AfterFunc(d, func() {
		h.running.Add(1)
		close(ch)
	})
	h.running.Add(-1)
	<-ch
}

// gate is the test's deferred.
type gate chan struct{}

func newGate() gate { return make(gate) }

func (g gate) open() { close(g) }

var (
	errTransientNet = fmt.Errorf("%w: temporary network error", exchange.ErrTransient)
	errPasscode     = fmt.Errorf("%w: invalid credentials", handshake.ErrPasscodeMismatch)
)

// TestCommissioningConnection ports the top-level describe("CommissioningConnection").
func TestCommissioningConnection(t *testing.T) {
	t.Parallel()

	t.Run("does not accept a late success on a device already dropped for invalid credentials", func(t *testing.T) {
		t.Parallel()
		late, lateSession := newGate(), make(chan *exchange.Session, 1)
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2")}, timeout: 2 * time.Second, delay: ccDelay(0)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if a.ip == "fd00::1" {
					return nil, errPasscode
				}
				h.block(context.Background(), late) // ignores its signal
				s := h.session()
				lateSession <- s
				return s, nil
			})
		// Let fd00::1 fail and mark the device invalid, then let fd00::2 complete.
		h.until("the device drop", func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return len(h.ctxs) == 2 && h.ctxs[1].Err() != nil
		})
		late.open()
		if _, err := h.result(); !errors.Is(err, handshake.ErrPasscodeMismatch) {
			t.Fatalf("err = %v, want PasscodeMismatch", err)
		}
		// The late session is closed with the real credential error, not the generic race fallback.
		if cause := h.closedWith(<-lateSession); !errors.Is(cause, handshake.ErrPasscodeMismatch) {
			t.Fatalf("close cause = %v", cause)
		}
	})

	t.Run("credential failure skips later entries with the same deviceIdentifier", func(t *testing.T) {
		t.Parallel()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1"), ccDevice("b", "fd00::2"), ccDevice("a", "fd00::3")}, timeout: 2 * time.Second, delay: ccDelay(time.Second)},
			func(_ *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if a.device == "a" {
					return nil, errPasscode
				}
				return nil, errTransientNet
			})
		if _, err := h.result(); !errors.Is(err, handshake.ErrPasscodeMismatch) {
			t.Fatalf("err = %v", err)
		}
		if got := h.labelled(); !slices.Equal(got, []string{"a:fd00::1", "b:fd00::2"}) {
			t.Fatalf("attempts %v", got)
		}
	})

	t.Run("keeps trying a device's other addresses after invalid PASE data on one", func(t *testing.T) {
		t.Parallel()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2")}, timeout: 2 * time.Second, delay: ccDelay(time.Second)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if a.ip == "fd00::1" {
					return nil, fmt.Errorf("%w: Missing requested PbkdfParameters", handshake.ErrUnexpectedData)
				}
				return h.session(), nil
			})
		res, err := h.result()
		if err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
		if got := h.order(); !slices.Equal(got, []string{"fd00::1", "fd00::2"}) {
			t.Fatalf("attempts %v", got)
		}
	})

	t.Run("closes session if winner is found concurrently with another establishment completing", func(t *testing.T) {
		t.Parallel()
		first, second := newGate(), newGate()
		loser := make(chan *exchange.Session, 1)
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1"), ccDevice("b", "fd00::2")}, timeout: 2 * time.Second, delay: ccDelay(0)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if a.ip == "fd00::1" {
					h.block(context.Background(), first)
					return h.session(), nil // a wins
				}
				h.block(context.Background(), second)
				s := h.session()
				loser <- s
				return s, nil // b loses
			})
		// Both attempts start; with no stagger their scripts record in either order.
		h.until("both attempts", func() bool { return len(h.order()) == 2 })
		h.settle()
		// Let a finish first, then b (the JS test yields between the two).
		first.open()
		res, err := h.result()
		if err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
		second.open()
		h.closedWith(<-loser)
	})

	t.Run("returns the won session without waiting for an abort-unresponsive loser to settle", func(t *testing.T) {
		t.Parallel()
		releaseLoser := newGate()
		loser := make(chan *exchange.Session, 1)
		var winner atomic.Pointer[exchange.Session]
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("winner", "abcd::2"), ccDevice("loser", "10.10.10.2")}, timeout: 90 * time.Second, delay: ccDelay(0)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if a.ip == "abcd::2" {
					s := h.session()
					winner.Store(s)
					return s, nil
				}
				// Deliberately ignores the abort signal.
				h.block(context.Background(), releaseLoser)
				s := h.session()
				loser <- s
				return s, nil
			})
		// Without releasing the loser.
		res, err := h.result()
		if err != nil || res.device.identifier != "winner" || res.session != winner.Load() {
			t.Fatalf("result %+v, %v", res, err)
		}
		if h.isClosed(res.session) {
			t.Fatal("the won session was closed")
		}
		// Release the loser so its cleanup runs and nothing dangles into the next test.
		releaseLoser.open()
		h.closedWith(<-loser)
		if h.isClosed(res.session) {
			t.Fatal("the won session was closed")
		}
	})

	t.Run("closes session if timeout fires while establishment is pending", func(t *testing.T) {
		t.Parallel()
		later := newGate()
		late := make(chan *exchange.Session, 1)
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1")}, timeout: 50 * time.Millisecond, delay: ccDelay(0)},
			func(h *ccHarness, _ *ccAttempt) (*exchange.Session, error) {
				// Much longer than the timeout, so the abort fires before we return.
				h.block(context.Background(), later)
				s := h.session()
				late <- s
				return s, nil
			})
		h.at(50 * time.Millisecond)
		later.open()
		var pr *PairRetransmissionLimitReachedError
		if _, err := h.result(); !errors.As(err, &pr) {
			t.Fatalf("err = %v", err)
		}
		h.closedWith(<-late)
	})

	t.Run("external abort cancels in-flight connection and rejects with abort reason", func(t *testing.T) {
		t.Parallel()
		ctx, abort := context.WithCancelCause(context.Background())
		h := ccStart(t, ccOptions{ctx: ctx, devices: []*candidate{ccDevice("loser", "fd00::1")}, timeout: 500 * time.Millisecond, delay: ccDelay(0)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				h.block(a.ctx, nil)
				return nil, context.Cause(a.ctx)
			})
		reason := errors.New("another device won PASE")
		abort(reason)
		if _, err := h.result(); !errors.Is(err, reason) {
			t.Fatalf("err = %v, want the abort reason", err)
		}
	})

	t.Run("passes abort signal to establishSession and aborts early when timeout fires", func(t *testing.T) {
		t.Parallel()
		var received atomic.Pointer[context.Context]
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1")}, timeout: 50 * time.Millisecond, delay: ccDelay(0)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				received.Store(&a.ctx)
				timer := newGate()
				h.clock.AfterFunc(time.Second, timer.open)
				if h.block(a.ctx, timer) {
					return h.session(), nil
				}
				return nil, context.Cause(a.ctx)
			})
		h.at(50 * time.Millisecond)
		var pr *PairRetransmissionLimitReachedError
		if _, err := h.result(); !errors.As(err, &pr) {
			t.Fatalf("err = %v", err)
		}
		if c := received.Load(); c == nil || (*c).Err() == nil {
			t.Fatal("the attempt's signal was not aborted")
		}
	})
}

// TestCommissioningConnectionStagger ports describe("per-address stagger").
func TestCommissioningConnectionStagger(t *testing.T) {
	t.Parallel()

	t.Run("first candidate fires immediately, subsequent candidates wait delayBeforeNextAddress each", func(t *testing.T) {
		t.Parallel()
		g := newGate()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2", "fd00::3")}, timeout: time.Minute, delay: ccDelay(5 * time.Second)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if !h.block(a.ctx, g) {
					return nil, context.Cause(a.ctx)
				}
				if a.ip == "fd00::1" {
					return h.session(), nil
				}
				return nil, fmt.Errorf("%w: losing", exchange.ErrTransient)
			})
		h.armed(2)
		h.expectOrder(h.order, "fd00::1")
		h.at(5 * time.Second)
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		h.at(5 * time.Second)
		h.expectOrder(h.order, "fd00::1", "fd00::2", "fd00::3")
		g.open()
		if res, err := h.result(); err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
	})

	t.Run("cancels pending stagger when a winner is established", func(t *testing.T) {
		t.Parallel()
		winnerGate := newGate()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2", "fd00::3")}, timeout: time.Minute, delay: ccDelay(5 * time.Second)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if a.ip == "fd00::1" {
					h.block(context.Background(), winnerGate)
					return h.session(), nil
				}
				h.block(a.ctx, nil)
				return nil, context.Cause(a.ctx)
			})
		h.armed(2)
		h.expectOrder(h.order, "fd00::1")
		winnerGate.open()
		if res, err := h.result(); err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
		h.at(15 * time.Second)
		h.expectOrder(h.order, "fd00::1")
	})

	// b's rejection follows a's win in the JS test's microtask order (both
	// await one gate, a's continuation first); here b waits for that win.
	t.Run("stagger slot is global across distinct devices (contract doc)", func(t *testing.T) {
		t.Parallel()
		g := newGate()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1"), ccDevice("b", "fd00::2")}, timeout: time.Minute, delay: ccDelay(5 * time.Second)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if !h.block(a.ctx, g) {
					return nil, context.Cause(a.ctx)
				}
				if a.device == "a" {
					return h.session(), nil
				}
				h.block(a.ctx, nil)
				return nil, errors.New("b")
			})
		h.armed(2)
		h.expectOrder(h.labelled, "a:fd00::1")
		h.at(4900 * time.Millisecond)
		h.expectOrder(h.labelled, "a:fd00::1")
		h.at(200 * time.Millisecond)
		h.expectOrder(h.labelled, "a:fd00::1", "b:fd00::2")
		g.open()
		if res, err := h.result(); err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
	})

	t.Run("a fast failure starts the next attempt at once, and that attempt still wins", func(t *testing.T) {
		t.Parallel()
		winnerGate := newGate()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2")}, timeout: time.Minute, delay: ccDelay(5 * time.Second)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if a.ip == "fd00::1" {
					return nil, fmt.Errorf("%w: fe80-like unreachable", exchange.ErrNetwork)
				}
				if !h.block(a.ctx, winnerGate) {
					return nil, context.Cause(a.ctx)
				}
				return h.session(), nil
			})
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		winnerGate.open()
		if res, err := h.result(); err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
	})

	t.Run("gives a third address an attempt within the default 30s timeout", func(t *testing.T) {
		t.Parallel()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2", "fd00::3")}, timeout: 30 * time.Second},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				h.block(a.ctx, nil)
				return nil, context.Cause(a.ctx)
			})
		h.armed(2)
		h.at(20100 * time.Millisecond)
		h.expectOrder(h.order, "fd00::1", "fd00::2", "fd00::3")
		h.at(10 * time.Second)
		var pr *PairRetransmissionLimitReachedError
		if _, err := h.result(); !errors.As(err, &pr) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("holds the next address while an attempt has reached its responder", func(t *testing.T) {
		t.Parallel()
		firstFails := newGate()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2")}, timeout: time.Minute, delay: ccDelay(5 * time.Second)},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if a.ip == "fd00::1" {
					a.onPBKDF()
					h.block(context.Background(), firstFails)
					return nil, fmt.Errorf("%w: responder went silent", exchange.ErrNetwork)
				}
				return h.session(), nil
			})
		h.armed(2)
		h.at(15 * time.Second)
		h.expectOrder(h.order, "fd00::1")
		firstFails.open()
		res, err := h.result()
		if err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
		if got := h.order(); !slices.Equal(got, []string{"fd00::1", "fd00::2"}) {
			t.Fatalf("order %v", got)
		}
	})

	t.Run("skips the queued addresses of a device dropped for invalid credentials", func(t *testing.T) {
		t.Parallel()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2")}, timeout: time.Minute, delay: ccDelay(5 * time.Second)},
			func(_ *ccHarness, _ *ccAttempt) (*exchange.Session, error) {
				return nil, fmt.Errorf("%w: wrong passcode", handshake.ErrPasscodeMismatch)
			})
		h.at(10 * time.Second)
		if _, err := h.result(); !errors.Is(err, handshake.ErrPasscodeMismatch) {
			t.Fatalf("err = %v", err)
		}
		if got := h.order(); !slices.Equal(got, []string{"fd00::1"}) {
			t.Fatalf("order %v", got)
		}
	})

	t.Run("default delayBeforeNextAddress is 10s", func(t *testing.T) {
		t.Parallel()
		g := newGate()
		h := ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", "fd00::1", "fd00::2")}, timeout: time.Minute},
			func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
				if !h.block(a.ctx, g) {
					return nil, context.Cause(a.ctx)
				}
				if a.ip == "fd00::1" {
					return h.session(), nil
				}
				h.block(a.ctx, nil) // after fd00::1's win, as in the JS microtask order
				return nil, errors.New("fd00::2")
			})
		h.armed(2)
		h.expectOrder(h.order, "fd00::1")
		h.at(9900 * time.Millisecond)
		h.expectOrder(h.order, "fd00::1")
		h.at(200 * time.Millisecond)
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		g.open()
		if _, err := h.result(); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

// ccStep is the scripted describe's Script (:642-649), in milliseconds.
type ccStep struct {
	reach, fail int // 0: absent
	win         bool
}

// ccRun is the scripted describe's run (:651-674): one device "a" with an
// address per script, in order.
func ccRun(t *testing.T, ips []string, scripts map[string]ccStep) *ccHarness {
	t.Helper()
	return ccStart(t, ccOptions{devices: []*candidate{ccDevice("a", ips...)}, timeout: time.Minute, delay: ccDelay(5 * time.Second)},
		func(h *ccHarness, a *ccAttempt) (*exchange.Session, error) {
			s := scripts[a.ip]
			elapsed := 0
			if s.reach != 0 {
				h.sleep(time.Duration(s.reach) * time.Millisecond)
				elapsed = s.reach
				a.onPBKDF()
			}
			if s.win {
				return h.session(), nil
			}
			if s.fail != 0 {
				h.sleep(time.Duration(s.fail-elapsed) * time.Millisecond)
				return nil, fmt.Errorf("%w: %s gave up", exchange.ErrNetwork, a.ip)
			}
			h.block(a.ctx, nil)
			return nil, context.Cause(a.ctx)
		})
}

// TestCommissioningConnectionScripted ports describe("scripted addresses").
func TestCommissioningConnectionScripted(t *testing.T) {
	t.Parallel()
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	end := func(t *testing.T, h *ccHarness) {
		t.Helper()
		h.at(time.Minute)
		h.result()
	}

	t.Run("restarts the stagger from the last launch", func(t *testing.T) {
		t.Parallel()
		h := ccRun(t, []string{"fd00::1", "fd00::2", "fd00::3"}, map[string]ccStep{"fd00::1": {fail: 2000}})
		h.armed(3)
		h.at(ms(2000))
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		h.at(ms(4900))
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		h.at(ms(200))
		h.expectOrder(h.order, "fd00::1", "fd00::2", "fd00::3")
		end(t, h)
	})

	t.Run("lets an older failure leave the newer attempt's stagger running", func(t *testing.T) {
		t.Parallel()
		h := ccRun(t, []string{"fd00::1", "fd00::2", "fd00::3"}, map[string]ccStep{"fd00::1": {fail: 7000}})
		h.armed(3)
		h.at(ms(5000))
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		h.at(ms(2100))
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		h.at(ms(3000))
		h.expectOrder(h.order, "fd00::1", "fd00::2", "fd00::3")
		end(t, h)
	})

	t.Run("keeps the newer attempt's delay when a held attempt fails while it is unanswered", func(t *testing.T) {
		t.Parallel()
		h := ccRun(t, []string{"fd00::1", "fd00::2", "fd00::3"}, map[string]ccStep{"fd00::1": {reach: 6000, fail: 8000}})
		h.armed(3)
		h.at(ms(5000))
		h.at(ms(1000)) // fd00::1 holds its responder
		h.at(ms(2100)) // fd00::1 fails; fd00::2, started at 5s, is still unanswered
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		h.at(ms(2000)) // 5s after fd00::2 started
		h.expectOrder(h.order, "fd00::1", "fd00::2", "fd00::3")
		end(t, h)
	})

	t.Run("launches the next address once a hold ends after the latest attempt failed", func(t *testing.T) {
		t.Parallel()
		h := ccRun(t, []string{"fd00::1", "fd00::2", "fd00::3", "fd00::4"}, map[string]ccStep{
			"fd00::2": {reach: 6000, fail: 10000},
			"fd00::3": {fail: 2000},
			"fd00::4": {win: true},
		})
		h.armed(2)
		h.at(ms(5000)) // fd00::2 starts
		h.at(ms(5000)) // fd00::3 starts
		h.at(ms(1000)) // fd00::2 holds its responder
		h.at(ms(1000)) // fd00::3, the latest, fails while fd00::2 holds
		h.expectOrder(h.order, "fd00::1", "fd00::2", "fd00::3")
		h.at(ms(3100)) // fd00::2 fails; nothing unanswered is newer, so fd00::4 starts at once
		h.expectOrder(h.order, "fd00::1", "fd00::2", "fd00::3", "fd00::4")
		if res, err := h.result(); err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
	})

	t.Run("launches the next address when a held attempt fails with nothing else pending", func(t *testing.T) {
		t.Parallel()
		h := ccRun(t, []string{"fd00::1", "fd00::2", "fd00::3"}, map[string]ccStep{
			"fd00::1": {reach: 6000, fail: 8000},
			"fd00::2": {fail: 2000},
			"fd00::3": {win: true},
		})
		h.armed(3)
		h.at(ms(5000))
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		h.at(ms(1000)) // fd00::1 reaches its responder
		h.at(ms(1000)) // fd00::2 fails while fd00::1 holds the responder
		h.expectOrder(h.order, "fd00::1", "fd00::2")
		h.at(ms(1100)) // fd00::1 fails
		h.expectOrder(h.order, "fd00::1", "fd00::2", "fd00::3")
		if res, err := h.result(); err != nil || res.device.identifier != "a" {
			t.Fatalf("result %+v, %v", res, err)
		}
	})
}
