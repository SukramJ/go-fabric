// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime/metrics"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// DefaultStart is the instant a site's [clocktest.Fake] starts at when the
// test supplies no clock.
var DefaultStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// DefaultSettle is the quiet period of [Site.Settle], and of [Site.Run]
// before it moves a fake clock to its next deadline.
const DefaultSettle = 10 * time.Millisecond

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
	// Logger receives the hosts' logs; nil discards them.
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
	clk  clock.Clock
	fake *clocktest.Fake
	net  *Network
	log  *slog.Logger

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
	s := &Site{tb: tb, opts: opts, clk: opts.Clock, log: opts.Logger, nextIndex: 1}
	if s.clk == nil {
		s.fake = clocktest.New(DefaultStart)
		s.clk = s.fake
	} else if f, ok := s.clk.(*clocktest.Fake); ok {
		s.fake = f
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if s.opts.Settle <= 0 {
		s.opts.Settle = DefaultSettle
	}
	if !opts.Loopback {
		s.net = newNetwork(s.clk, opts.Net)
	}
	tb.Cleanup(s.Close)
	return s
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
// the test ends — MockSite.close (mock-site.ts:182).
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
func (s *Site) listen(index int) (*udp.Listener, error) {
	if s.net != nil {
		return s.net.listen(hostAddr(index))
	}
	return udp.New(udp.Config{LocalAddr: "[::1]:0"})
}

// Settle waits until the site is quiet for the settle period
// ([Options].Settle): no datagram sent and none queued unread on the
// in-memory network, and no goroutine of the test process runnable,
// running (but the one waiting) or in a system call. It moves no clock.
func (s *Site) Settle(ctx context.Context) error { return s.quiet(ctx, nil) }

// quiet is Settle that also returns, with nil, once done is closed.
func (s *Site) quiet(ctx context.Context, done <-chan struct{}) error {
	wall := clock.System{}
	since := wall.Now()
	lastSent := s.sent()
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
		if sent := s.sent(); sent != lastSent || s.pending() > 0 || busy(samples) {
			lastSent, since = sent, wall.Now()
		} else if clock.Since(wall, since) >= s.opts.Settle {
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
// work of a parallel test in the same binary delays the step too.
func busy(samples []metrics.Sample) bool {
	metrics.Read(samples)
	var n uint64
	for i := range samples {
		if samples[i].Value.Kind() == metrics.KindUint64 {
			n += samples[i].Value.Uint64()
		}
	}
	return n > 1
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
// This is MockTime.resolve (packages/testing/src/mocks/time.ts), under
// which the matter.js site commissions (mock-site.ts:156): time moves only
// while nothing else can, read in Go as "no datagram in the network and no
// goroutine working".
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
		if err := s.quiet(ctx, finished); err != nil {
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
// what when ctx ends first. A test waits for the effect of a change — a
// report that reached the controller, a session that closed — with it.
func (s *Site) Eventually(ctx context.Context, what string, cond func() bool) error {
	for !cond() {
		if err := s.quiet(ctx, nil); err != nil {
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

// address is host index's address on the in-memory network; nil on
// loopback, where the kernel picks the port when the host listens.
func (s *Site) address(index int) *net.UDPAddr {
	if s.net != nil {
		return hostAddr(index)
	}
	return nil
}
