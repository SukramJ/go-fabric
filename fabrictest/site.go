// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/metrics"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// DefaultStart is the instant a site's [clocktest.Fake] starts at when the
// test supplies no clock.
var DefaultStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// DefaultSettle is the quiet period of [Site.Settle], and of [Site.Run]
// before it moves a fake clock to its next deadline.
const DefaultSettle = 10 * time.Millisecond

// holdBudget is how long, on the wall clock, a host's counted work holds
// a fake site clock while nothing on the site moves — no datagram sent,
// no counter changed. Past it the work no longer holds the clock (the
// site logs it): work that waits on the clock while counted would
// otherwise stall the site. matter.js MockTime abandons a host operation
// after MAX_HOST_ASYNC_MS (packages/testing/src/mocks/time.ts:63-70 and
// :169-180 at 862ad75f), measured on the host's clock for the same
// reason: counted in turns it would shrink when the machine is loaded.
const holdBudget = time.Second

// busyGrace bounds the process-wide goroutine check on an in-memory site
// with a fake clock: a runnable goroutine delays a step by at most this
// long after the site's last change. The check stands in for the work no
// counter names (the goroutine a fired channel timer woke, a handshake's
// first message being computed); bounding it keeps the goroutines of the
// tests running beside this one from starving the site's clock.
const busyGrace = 50 * time.Millisecond

// DefaultWait is how long [Site.Eventually] and [Site.Settle] wait, on the
// site's wall clock ([Options].Wall), when their context has no deadline
// of its own: a condition that never holds ends the test with an error
// instead of hanging it until go test's timeout.
const DefaultWait = 30 * time.Second

// ErrWaited is the error of a wait without a context deadline that ran
// out of the site's own ([Options].Wait).
var ErrWaited = errors.New("fabrictest: the site's wait ran out (Options.Wait; the context had no deadline)")

// closeTimeout bounds the site's teardown of one host.
const closeTimeout = 10 * time.Second

// Options configures a [Site].
type Options struct {
	// Clock is the time source of every device and controller of the site;
	// nil is a new [clocktest.Fake] at [DefaultStart]. A test that drives
	// parts of its own on the system clock against a device of the site
	// passes clock.System{}.
	Clock clock.Clock
	// Net shapes the in-memory network's delivery; [Network.Set] changes
	// it later. Ignored with Loopback.
	Net NetOptions
	// Loopback runs the site's hosts on real UDP sockets on the loopback
	// address ([::1], an ephemeral port each) instead of the in-memory
	// network, for a test that needs sockets. The site still answers its
	// controllers' DNS-SD queries from what the devices publish.
	Loopback bool
	// Settle is the quiet period of [Site.Settle]; 0 is [DefaultSettle].
	Settle time.Duration
	// Wait bounds [Site.Eventually] and [Site.Settle] on the Wall clock
	// when their context has no deadline; 0 is [DefaultWait]. A context
	// with a deadline is waited on as it is.
	Wait time.Duration
	// Wall is the clock the quiet period is measured on and the quiet
	// check paces itself by — the machine's time, whatever Clock the hosts
	// run on, since "nothing ran for 10 ms" is a statement about the
	// process; nil is [clock.System].
	Wall clock.Clock
	// StoreOnDisk keeps each device's state in a SQLite file in the test's
	// temporary directory, as a deployment does, instead of in memory
	// (store/memstore and endpoint/memstore, the default). Either way the
	// state survives [Device.Restart], which starts the new node on the
	// same store; a test sets it to exercise the SQLite encoding across a
	// restart.
	StoreOnDisk bool
	// Logger receives the logs of the hosts the site builds — the nodes,
	// the controllers and their components; nil discards those. A
	// component that logs to slog.Default() rather than to the logger it
	// is given (OperationalCredentials' matter.opcreds.cmd lines, gate 1
	// G1-18) still reaches the process's default logger.
	Logger *slog.Logger
}

// Site is a set of in-process Matter hosts — devices ([Site.AddDevice])
// and controllers ([Site.AddController]) — on one network and one clock.
// It mirrors matter.js's MockSite (packages/node/src/testing/mock-site.ts):
// every host closes with the site, and the site closes when the test
// ends. It is safe for concurrent use.
type Site struct {
	tb   testing.TB
	opts Options
	// dir holds the devices' SQLite files (Options.StoreOnDisk). It is created before Close is
	// registered, so the test's cleanup removes it only after Close has
	// stopped every node (cleanups run last-registered first).
	dir  string
	clk  clock.Clock
	wall clock.Clock // Options.Wall
	fake *clocktest.Fake
	net  *Network
	log  *slog.Logger

	// groups are the in-flight counters of the site's hosts, one per
	// device and controller (internal/inflight); guarded by mu.
	groups []*inflight.Group
	// abandoned is, per group, the work that held the clock past
	// holdBudget and no longer holds it: a counter holds the site only
	// above its abandoned level, and the level follows the counter down
	// (matter.js marks the operation abandoned, time.ts:169-180). Guarded
	// by mu.
	abandoned map[*inflight.Group]map[string]int64

	// inSyscall is the number of the process's standing goroutines in a
	// system call (os/signal's loop, once a test of the binary called
	// signal.Notify), which never leave it and are no work of the site:
	// the fewest seen since the site was built. A count taken once would
	// include another parallel test's passing system calls and hide this
	// site's work once those end (the G2-16 flakes); the minimum only ever
	// errs towards waiting longer.
	inSyscall atomic.Uint64

	// mu guards closers and nextIndex.
	mu          sync.Mutex
	nextIndex   int
	devices     []*Device
	controllers []*Controller
	closers     []func()
	closeOnce   sync.Once
}

// NewSite returns an empty site, closed when tb ends.
func NewSite(tb testing.TB, opts Options) *Site {
	tb.Helper()
	s := &Site{tb: tb, opts: opts, clk: opts.Clock, wall: clock.Or(opts.Wall), log: opts.Logger, nextIndex: 1}
	if s.clk == nil {
		s.fake = clocktest.New(DefaultStart)
		s.clk = s.fake
	} else if f, ok := s.clk.(*clocktest.Fake); ok {
		s.fake = f
	}
	if s.log == nil {
		// A test site is quiet by design: its hosts log only to a logger
		// the test hands it (Options.Logger), never to slog.Default() as a
		// library component's nil logger does (docs/guides/observability.md).
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.opts.Wait <= 0 {
		s.opts.Wait = DefaultWait
	}
	if s.opts.Settle <= 0 {
		s.opts.Settle = DefaultSettle
	}
	if !opts.Loopback {
		s.net = newNetwork(s.clk, opts.Net)
	}
	s.dir = tb.TempDir()
	s.inSyscall.Store(goroutinesInSyscall())
	tb.Cleanup(s.Close)
	return s
}

// goroutinesInSyscall is the process's count of goroutines in a system
// call.
func goroutinesInSyscall() uint64 {
	sample := []metrics.Sample{{Name: "/sched/goroutines/not-in-go:goroutines"}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample[0].Value.Uint64()
}

// Clock is the site's clock.
func (s *Site) Clock() clock.Clock { return s.clk }

// Fake is the site's clock as a [clocktest.Fake]; nil when the test gave
// the site another clock.
func (s *Site) Fake() *clocktest.Fake { return s.fake }

// Net is the in-memory network; nil with [Options].Loopback.
func (s *Site) Net() *Network { return s.net }

// Loopback reports whether the site runs on real UDP sockets.
func (s *Site) Loopback() bool { return s.opts.Loopback }

// Logger is the logger the site hands its hosts.
func (s *Site) Logger() *slog.Logger { return s.log }

// Devices are the site's devices in the order they were added.
func (s *Site) Devices() []*Device { return s.devicesSnapshot() }

// Controllers are the site's controllers in the order they were added.
func (s *Site) Controllers() []*Controller {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Controller(nil), s.controllers...)
}

func (s *Site) devicesSnapshot() []*Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Device(nil), s.devices...)
}

// index hands out the next host index: a host's address on the in-memory
// network and its DNS-SD host name.
func (s *Site) index() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.nextIndex
	s.nextIndex++
	return i
}

// onClose runs fn when the site closes, the last registered first.
func (s *Site) onClose(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closers = append(s.closers, fn)
}

// Close closes every host of the site, the last added first: controllers
// and devices stop, their sockets close. It is idempotent and runs when
// the test ends — the matter.js site's close (mock-site.ts:182).
func (s *Site) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		closers := s.closers
		s.closers = nil
		s.mu.Unlock()
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	})
}

// listen opens a host's UDP listener: on the in-memory network at the
// host's address, or on a loopback socket.
func (s *Site) listen(index int, g *inflight.Group) (*udp.Listener, error) {
	if s.net != nil {
		return s.net.listen(hostAddr(index), g)
	}
	return udp.New(udp.Config{LocalAddr: "[::1]:0"})
}

// Settle waits until the site is quiet for the settle period
// ([Options].Settle): no datagram sent and none queued unread on the
// in-memory network, no work in hand on any host — a datagram being
// handled, a message an exchange took and its reader has not finished
// with, a report being sent, a host callback queued, a node's start or
// stop (internal/inflight) — and no goroutine of the test process
// runnable, running (but the one waiting) or in a system call. On an
// in-memory site with a fake clock the last check delays it by at most a
// short grace after the site's last change, so the goroutines of other
// tests in the binary do not starve the site; on loopback sockets and on
// any other clock it holds as long as a goroutine works. It moves no
// clock. A ctx without a deadline is bounded by [Options].Wait
// ([ErrWaited]).
func (s *Site) Settle(ctx context.Context) error { return s.quiet(ctx, nil, s.waitUntil(ctx)) }

// Step moves a fake site clock by d under the quiet protocol: it waits
// until the site is quiet ([Site.Settle]), advances the clock by d —
// firing every timer due in it, in deadline order, each callback awaited
// ([clocktest.Fake.Advance]) — and waits until the site is quiet again. A
// test that drives the clock by hand steps it with Step, never with
// [Site.Fake]().Advance while the stack may be working: the clock would
// move under a host still handling a message. It is matter.js's
// `await MockTime.advance(ms)` (packages/testing/src/mocks/time.ts:638-663
// at 862ad75f) between two waits for the task queue to drain. Without a
// fake clock it fails.
func (s *Site) Step(ctx context.Context, d time.Duration) error {
	if s.fake == nil {
		return errors.New("fabrictest: Step needs a fake site clock (Options.Clock is not a clocktest.Fake)")
	}
	if err := s.quiet(ctx, nil, s.waitUntil(ctx)); err != nil {
		return fmt.Errorf("fabrictest: step: %w", err)
	}
	s.fake.Advance(d)
	if err := s.quiet(ctx, nil, s.waitUntil(ctx)); err != nil {
		return fmt.Errorf("fabrictest: step: %w", err)
	}
	return nil
}

// drain waits until the network and every host's counted work are quiet
// for the settle period, holding the clock meanwhile: it counts a
// node.stop in own, which an outer [Site.Run] or [Site.Eventually] sees
// as work, and does not count it itself. It reads no goroutine states
// but on loopback sockets, whose kernel work no counter sees. Counted work
// that stands for holdBudget with nothing moving ends the wait, as it ends
// a quiet period's hold.
func (s *Site) drain(ctx context.Context, own *inflight.Group) error {
	own.Enter(inflight.NodeStop)
	defer own.Leave(inflight.NodeStop)
	until := s.waitUntil(ctx)
	wall := s.wall
	samples := make([]metrics.Sample, len(busyMetrics))
	for i, name := range busyMetrics {
		samples[i].Name = name
	}
	busyWork := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, g := range s.groups {
			for c, v := range g.Snapshot() {
				if g == own && c == inflight.NodeStop {
					v--
				}
				if v > 0 {
					return true
				}
			}
		}
		return false
	}
	last, _ := s.state()
	since := wall.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		now := wall.Now()
		if !until.IsZero() && !now.Before(until) {
			return ErrWaited
		}
		cur, _ := s.state()
		switch {
		case cur != last || s.pending() > 0:
			last, since = cur, now
		case s.opts.Loopback && busy(samples, &s.inSyscall):
			since = now
		default:
			stable := now.Sub(since)
			if stable >= s.opts.Settle && (!busyWork() || stable >= holdBudget) {
				return nil
			}
		}
		clock.Sleep(wall, time.Millisecond, nil)
	}
}

// newGroup registers the in-flight counters of a new host named name.
func (s *Site) newGroup(name string) *inflight.Group {
	g := inflight.New(name)
	s.mu.Lock()
	s.groups = append(s.groups, g)
	s.mu.Unlock()
	return g
}

// hostClock is the clock a host of the site is built with: on a fake
// site clock the fake carrying the host's counters, so every component
// the host builds on it counts its work there (internal/inflight); the
// site's clock as it is otherwise.
func (s *Site) hostClock(g *inflight.Group) clock.Clock {
	if s.fake == nil {
		return s.clk
	}
	return inflight.WithGroup(s.clk, g)
}

// siteState is what the quiet check compares between two looks: a
// change of any field is the site moving.
type siteState struct {
	sent    uint64
	changes uint64
}

func (s *Site) state() (st siteState, held bool) {
	st.sent = s.sent()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range s.groups {
		st.changes += g.Changes()
		snap := g.Snapshot()
		ab := s.abandoned[g]
		for c, v := range snap {
			if v > ab[c] {
				held = true
			}
		}
		for c, a := range ab {
			if v := snap[c]; v < a {
				if v <= 0 {
					delete(ab, c)
				} else {
					ab[c] = v
				}
			}
		}
	}
	return st, held
}

// abandon stops the work the site's hosts hold now from holding the
// clock; work counted later holds it again.
func (s *Site) abandon() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.abandoned == nil {
		s.abandoned = map[*inflight.Group]map[string]int64{}
	}
	for _, g := range s.groups {
		snap := g.Snapshot()
		if len(snap) == 0 {
			continue
		}
		if s.abandoned[g] == nil {
			s.abandoned[g] = map[string]int64{}
		}
		for c, v := range snap {
			s.abandoned[g][c] = max(s.abandoned[g][c], v)
		}
	}
}

// inflightString names the work the site's hosts hold, for a wait's
// error; empty when they hold none.
func (s *Site) inflightString() string {
	s.mu.Lock()
	groups := s.groups
	s.mu.Unlock()
	var out string
	for _, g := range groups {
		snap := g.Snapshot()
		if len(snap) == 0 {
			continue
		}
		if out != "" {
			out += "; "
		}
		out += fmt.Sprintf("%s %v", g.Name(), snap)
	}
	return out
}

// strictBusy reports whether the process-wide goroutine check holds the
// site as long as a goroutine works: on loopback sockets, whose kernel
// work no counter sees, and on a clock that is not a fake, which nothing
// starves.
func (s *Site) strictBusy() bool { return s.opts.Loopback || s.fake == nil }

// waitUntil is the wall-clock instant a wait on ctx gives up at: the zero
// time for a ctx with a deadline (ctx bounds it), now plus [Options].Wait
// otherwise.
func (s *Site) waitUntil(ctx context.Context) time.Time {
	if _, ok := ctx.Deadline(); ok {
		return time.Time{}
	}
	return s.wall.Now().Add(s.opts.Wait)
}

// quiet is Settle that also returns, with nil, once done is closed, and
// with [ErrWaited] once the wall clock passes until (none when zero).
//
// The site is quiet once, for the settle period on the wall clock, nothing
// moved — no datagram sent, no in-flight counter changed — and no datagram
// waits unread, no host holds counted work (or the work held the clock
// past holdBudget with nothing moving), and the process-wide goroutine
// check passed (strictBusy) or its grace ran out.
func (s *Site) quiet(ctx context.Context, done <-chan struct{}, until time.Time) error {
	wall := s.wall
	since := wall.Now()
	last, _ := s.state()
	samples := make([]metrics.Sample, len(busyMetrics))
	for i, name := range busyMetrics {
		samples[i].Name = name
	}
	for {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		now := wall.Now()
		if !until.IsZero() && !now.Before(until) {
			if held := s.inflightString(); held != "" {
				return fmt.Errorf("%w (in flight: %s)", ErrWaited, held)
			}
			return ErrWaited
		}
		cur, held := s.state()
		strict := s.strictBusy()
		switch {
		case cur != last || s.pending() > 0:
			last, since = cur, now
		case strict && busy(samples, &s.inSyscall):
			since = now
		default:
			stable := now.Sub(since)
			if stable < s.opts.Settle {
				break
			}
			if held && stable < holdBudget {
				break
			}
			if !strict && stable < busyGrace && busy(samples, &s.inSyscall) {
				break
			}
			if held {
				s.tb.Logf("fabrictest: counted work held the clock %v with nothing moving; the site moves on without it (in flight: %s)", holdBudget, s.inflightString())
				s.abandon()
			}
			return nil
		}
		clock.Sleep(wall, time.Millisecond, done)
	}
}

// busyMetrics are the goroutine states of work in progress.
var busyMetrics = []string{
	"/sched/goroutines/runnable:goroutines",
	"/sched/goroutines/running:goroutines",
	"/sched/goroutines/not-in-go:goroutines",
}

// busy reports whether a goroutine other than the caller is runnable,
// running or in a system call: a host still working on a message, a
// handshake's cryptography, a store write. It is the Go reading of
// MockTime's "the task queue is empty" — the process-wide count, so the
// work of a parallel test in the same binary reads as busy too, which is
// why an in-memory site with a fake clock bounds it (busyGrace) and
// relies on its hosts' counters (internal/inflight) for the work they
// name. The standing goroutines in a system call (standing, the fewest
// seen) are not work; a reading below it lowers it.
func busy(samples []metrics.Sample, standing *atomic.Uint64) bool {
	metrics.Read(samples)
	var n, inSyscall uint64
	for i := range samples {
		if samples[i].Value.Kind() == metrics.KindUint64 {
			n += samples[i].Value.Uint64()
			if samples[i].Name == "/sched/goroutines/not-in-go:goroutines" {
				inSyscall = samples[i].Value.Uint64()
			}
		}
	}
	for {
		was := standing.Load()
		if inSyscall >= was || standing.CompareAndSwap(was, inSyscall) {
			break
		}
	}
	return n > 1+standing.Load()
}

func (s *Site) sent() uint64 {
	if s.net == nil {
		return 0
	}
	return s.net.Sent()
}

func (s *Site) pending() int {
	if s.net == nil {
		return 0
	}
	return s.net.pending()
}

// Run runs fn and, while it has not returned, moves a fake site clock
// forward: each time the site has been quiet for the settle period
// ([Site.Settle]) it advances the clock to its next deadline, so a timer
// fn waits on — a standalone acknowledgement, a retransmission, a
// report's interval — fires. It returns fn's error, or ctx's when ctx ends
// first. With a clock that is not a [clocktest.Fake] it only runs fn.
//
// This is MockTime.resolve (packages/testing/src/mocks/time.ts:537-622 at
// 862ad75f), under which the matter.js site commissions (mock-site.ts:156):
// time moves only while nothing else can — no host operation pending
// (:603, withholdVirtualTime) — read in Go as "no datagram in the
// network, no counted work on a host, no goroutine working" ([Site.Settle]).
func (s *Site) Run(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.fake == nil {
		return fn(ctx)
	}
	finished := make(chan struct{})
	var result error
	go func() {
		defer close(finished)
		result = fn(ctx)
	}()
	for {
		if err := s.quiet(ctx, finished, time.Time{}); err != nil {
			return err
		}
		select {
		case <-finished:
			return result
		default:
		}
		if at, ok := s.fake.Next(); ok {
			s.fake.Advance(max(at.Sub(s.fake.Now()), 0))
		}
	}
}

// Eventually runs [Site.Run] until cond holds and returns an error naming
// what when ctx ends first — or, for a ctx without a deadline, once
// [Options].Wait has passed on the wall clock ([ErrWaited]). A test waits
// for the effect of a change — a report that reached the controller, a
// session that closed — with it.
func (s *Site) Eventually(ctx context.Context, what string, cond func() bool) error {
	until := s.waitUntil(ctx)
	for !cond() {
		if err := s.quiet(ctx, nil, until); err != nil {
			return fmt.Errorf("fabrictest: waiting for %s: %w", what, err)
		}
		if cond() {
			return nil
		}
		if s.fake == nil {
			continue // the wall clock moves by itself; quiet paced the loop
		}
		if at, ok := s.fake.Next(); ok {
			s.fake.Advance(max(at.Sub(s.fake.Now()), 0))
		}
	}
	return nil
}
