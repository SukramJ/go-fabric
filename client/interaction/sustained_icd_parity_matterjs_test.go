// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// The ICD part of the port of matter.js
// packages/protocol/test/action/client/subscription/SustainedSubscriptionTest.ts
// at 862ad75f (:341-415, :458-1193): a sustained subscription with a
// wakefulness provider — the timed retry and probe of a peer that needs no
// awaiting, the parking of a sleeping LIT peer, the hold, the operating-mode
// recreate, the priority connection and the registration feed. matter.js
// builds SustainedSubscription with an injected subscribe and probe; the
// port does the same through the interaction's attempt seam and a provider
// that counts its probes, the subscription being Interaction.Subscribe with
// Sustain. matter.js's `inactive` observable is !Active; its "icdLit"
// network is ExchangeOptions.Priority; its flush() — macrotask drains — is a
// wait for the loop's effect, and "nothing happened" a wait for the loop to
// settle on the wall clock.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/im"
)

// litTimings are SustainedSubscriptionTest.ts:46-50.
var litTimings = icd.Timings{ActiveModeThreshold: 5 * time.Second, IdleModeDuration: 30 * time.Second}

// icdProvider is a peer at (1, 1) that counts its probes.
type icdProvider struct {
	mu     sync.Mutex
	probes int
}

func (p *icdProvider) InitiateExchange(context.Context, ExchangeOptions) (*exchange.Exchange, error) {
	return nil, errors.New("no exchanges")
}

func (p *icdProvider) PeerAddress() (exchange.PeerAddress, bool) {
	return exchange.PeerAddress{FabricIndex: 1, NodeID: 1}, true
}
func (p *icdProvider) MaximumPeerResponseTime(time.Duration, bool) time.Duration { return 0 }
func (p *icdProvider) VerifyReachability(context.Context) bool {
	p.mu.Lock()
	p.probes++
	p.mu.Unlock()
	return true
}
func (p *icdProvider) ReadPathsSupported() int      { return 9 }
func (p *icdProvider) SubscribePathsSupported() int { return 3 }
func (p *icdProvider) Session() *exchange.Session   { return nil }
func (p *icdProvider) Manager() *exchange.Manager   { return nil }

func (p *icdProvider) probeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.probes
}

// icdRig is build() (SustainedSubscriptionTest.ts:89-106) with the
// subscribe injected through the attempt seam.
type icdRig struct {
	t        *testing.T
	fake     *clocktest.Fake
	prov     *icdProvider
	c        *Interaction
	mu       sync.Mutex
	attempts int
	closes   int
	priority []bool
	last     *Subscription
	fail     func(n int) error
	// closeGate, when set for the first subscription, blocks its Close.
	closeGate chan struct{}
	sub       *Subscription
	states    chan bool
	emissions []bool
}

func newICDRig(t *testing.T, fail func(n int) error) *icdRig {
	t.Helper()
	fake := clocktest.New(time.Unix(3_000_000, 0))
	r := &icdRig{t: t, fake: fake, prov: &icdProvider{}, fail: fail, states: make(chan bool, 64)}
	r.c = Bind(r.prov, Config{Subscriptions: NewSubscriptions(SubscriptionsConfig{Clock: fake}), Clock: fake, Rand: func() float64 { return 0 }})
	r.c.attemptHook = func(_ context.Context, req SubscribeRequest, opts ExchangeOptions) (*Subscription, error) {
		r.mu.Lock()
		r.attempts++
		n := r.attempts
		r.priority = append(r.priority, opts.Priority)
		fail := r.fail
		r.mu.Unlock()
		if fail != nil {
			if err := fail(n); err != nil {
				return nil, err
			}
		}
		peer, _ := r.prov.PeerAddress()
		inner := r.c.subs.newSubscription(peer, req, r.c.log)
		inner.id, inner.maxInterval = uint32(n), 60
		gate := r.closeGate
		inner.closed = func() {
			if n == 1 && gate != nil {
				<-gate
			}
			r.mu.Lock()
			r.closes++
			r.mu.Unlock()
		}
		r.mu.Lock()
		r.last = inner
		r.mu.Unlock()
		return inner, nil
	}
	return r
}

func (r *icdRig) start(w func() *icd.PeerWakefulness, fed func(func(uint64)) func()) *Subscription {
	r.t.Helper()
	sub, err := r.c.Subscribe(context.Background(), SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{{}}, Sustain: true, Wakefulness: w, PeerFed: fed,
		ActiveChanged: func(v bool) {
			r.mu.Lock()
			r.emissions = append(r.emissions, v)
			r.mu.Unlock()
		},
	})
	if err != nil {
		r.t.Fatal(err)
	}
	r.sub = sub
	r.t.Cleanup(func() {
		sub.Close()
		<-sub.Done()
	})
	return sub
}

func (r *icdRig) count() (attempts, closes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts, r.closes
}

func (r *icdRig) networks() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.priority)
}

func (r *icdRig) dips() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.emissions, false)
}

func (r *icdRig) resetEmissions() {
	r.mu.Lock()
	r.emissions = nil
	r.mu.Unlock()
}

// lose is the peer's subscription closing (lastRequest.closed()).
func (r *icdRig) lose() {
	r.mu.Lock()
	last := r.last
	r.mu.Unlock()
	if last != nil {
		last.Close()
	}
}

// waitFor waits on the wall clock for the loop to reach cond.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// settle lets the loop run what it would run now.
func settle() { time.Sleep(20 * time.Millisecond) }

func (r *icdRig) wantAttempts(n int) {
	r.t.Helper()
	waitFor(r.t, "the subscribe attempts", func() bool { a, _ := r.count(); return a >= n })
	settle()
	if a, _ := r.count(); a != n {
		r.t.Fatalf("%d attempts, want %d", a, n)
	}
}

func (r *icdRig) wantActive(v bool) {
	r.t.Helper()
	waitFor(r.t, "the active state", func() bool { return r.sub.Active() == v })
}

// advance moves the fake clock once the loop has armed what it waits on.
func (r *icdRig) advance(d time.Duration) {
	settle()
	r.fake.Advance(d)
	settle()
}

// litWakefulness starts asleep and unavailable (SustainedSubscriptionTest.ts:71-76).
func litWakefulness(fake *clocktest.Fake) *icd.PeerWakefulness {
	w := icd.NewPeerWakefulness(fake)
	w.SetTimings(litTimings)
	w.SetRequiresAwait(true)
	return w
}

func sitWakefulness(fake *clocktest.Fake) *icd.PeerWakefulness {
	w := icd.NewPeerWakefulness(fake)
	w.SetTimings(litTimings)
	return w
}

func fixed(w *icd.PeerWakefulness) func() *icd.PeerWakefulness {
	return func() *icd.PeerWakefulness { return w }
}

// feed is an Observable<[NodeId]> for the peerFed provider.
type feed struct {
	mu  sync.Mutex
	fns map[int]func(uint64)
	n   int
}

func (f *feed) on(fn func(uint64)) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fns == nil {
		f.fns = map[int]func(uint64){}
	}
	id := f.n
	f.n++
	f.fns[id] = fn
	return func() { f.mu.Lock(); delete(f.fns, id); f.mu.Unlock() }
}

func (f *feed) emit(id uint64) {
	f.mu.Lock()
	fns := make([]func(uint64), 0, len(f.fns))
	for _, fn := range f.fns {
		fns = append(fns, fn)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn(id)
	}
}

// registration is a wakefulness provider whose value a test sets later.
type registration struct {
	mu sync.Mutex
	w  *icd.PeerWakefulness
}

func (g *registration) get() *icd.PeerWakefulness { g.mu.Lock(); defer g.mu.Unlock(); return g.w }
func (g *registration) set(w *icd.PeerWakefulness) { g.mu.Lock(); g.w = w; g.mu.Unlock() }

var errTransient = errors.New("transient send failure")

func TestSustainedSubscriptionICD(t *testing.T) {
	t.Parallel()

	t.Run("non-ICD behavior", func(t *testing.T) {
		t.Parallel()
		t.Run("treats a wakefulness with requiresAwait===false as non-ICD (timed retry, probe)", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := icd.NewPeerWakefulness(r.fake) // requiresAwait false: always awake
			r.start(fixed(w), nil)
			r.wantAttempts(1)
			r.wantActive(true)
			r.lose()
			r.wantAttempts(2)
			if r.prov.probeCount() != 1 {
				t.Fatalf("%d probes", r.prov.probeCount())
			}
		})
		t.Run("parks on the next loss once its wakefulness flips to requiresAwait===true", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := sitWakefulness(r.fake)
			r.start(fixed(w), nil)
			r.wantAttempts(1)
			// A runtime SIT→LIT flip: the subscription recreates and parks, the
			// peer being asleep; the loss then finds nothing to replace.
			w.SetRequiresAwait(true)
			settle()
			r.lose()
			r.advance(60 * time.Second)
			if r.prov.probeCount() != 0 {
				t.Fatalf("%d probes", r.prov.probeCount())
			}
			r.wantAttempts(1)
			w.NoteActive()
			r.wantAttempts(2)
		})
	})

	t.Run("ICD await-mode behavior", func(t *testing.T) {
		t.Parallel()
		t.Run("parks while asleep and subscribes only after a wake signal", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := litWakefulness(r.fake)
			sub := r.start(fixed(w), nil)
			r.advance(20 * time.Second)
			r.wantAttempts(0)
			if sub.Active() {
				t.Fatal("active while parked")
			}
			w.NoteActive()
			r.wantAttempts(1)
			r.wantActive(true)
		})
		t.Run("leaves a subscribed LIT peer's availability to the subscription", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := litWakefulness(r.fake)
			r.start(fixed(w), nil)
			w.NoteActive()
			r.wantActive(true)
			r.advance(2 * time.Hour)
			if !w.Available() || !r.sub.Active() {
				t.Fatalf("available %v active %v", w.Available(), r.sub.Active())
			}
		})
		t.Run("fires checkInMissed for a parked LIT peer that never subscribes and misses its Check-In", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, func(int) error { return errTransient })
			w := litWakefulness(r.fake)
			var mu sync.Mutex
			missed := 0
			w.OnCheckInMissed(func() { mu.Lock(); missed++; mu.Unlock() })
			r.start(fixed(w), nil)
			w.NoteActive() // arms the deadline; the subscribe fails, so no hold
			r.wantAttempts(1)
			r.advance(30*time.Second + 5*time.Second + icd.CheckInMargin + time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			if w.Available() || missed != 1 {
				t.Fatalf("available %v missed %d", w.Available(), missed)
			}
		})
		t.Run("goes inactive immediately on a subscription loss, regardless of the availability window", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := litWakefulness(r.fake)
			r.start(fixed(w), nil)
			w.NoteActive()
			r.wantActive(true)
			r.advance(10 * time.Second) // past the threshold: asleep, still available
			r.lose()
			r.wantActive(false)
			if !w.Available() {
				t.Fatal("unavailable")
			}
			w.NoteActive()
			r.wantActive(true)
		})
		t.Run("parks on subscribe failure instead of running a timed retry", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, func(n int) error {
				if n == 1 {
					return errTransient
				}
				return nil
			})
			w := litWakefulness(r.fake)
			r.start(fixed(w), nil)
			w.NoteActive()
			r.wantAttempts(1)
			r.advance(60 * time.Second)
			r.wantAttempts(1)
			w.NoteActive()
			r.wantAttempts(2)
			r.wantActive(true)
		})
		t.Run("retries a flipped SIT peer after a bounded delay instead of stranding on a wake edge", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, func(n int) error {
				if n == 1 {
					return errTransient
				}
				return nil
			})
			w := litWakefulness(r.fake)
			r.start(fixed(w), nil)
			settle()
			w.SetRequiresAwait(false) // a DSLS LIT→SIT flip releases the parked loop
			r.wantAttempts(1)
			r.advance(SITRetryInterval)
			r.wantAttempts(2)
			r.wantActive(true)
		})
		t.Run("close() resolves done cleanly while parked", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			sub := r.start(fixed(litWakefulness(r.fake)), nil)
			settle()
			sub.Close()
			select {
			case <-sub.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("done not closed")
			}
		})
		t.Run("closes the active peer subscription on abort", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := litWakefulness(r.fake)
			sub := r.start(fixed(w), nil)
			w.NoteActive()
			r.wantActive(true)
			sub.Close()
			<-sub.Done()
			if _, closes := r.count(); closes != 1 || sub.ID() != NoSubscription {
				t.Fatalf("%d closes, id %d", closes, sub.ID())
			}
		})
		t.Run("honors a wakefulness provider that returns undefined until the peer registers", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			reg := &registration{}
			r.start(reg.get, nil)
			r.wantAttempts(1) // no wakefulness: non-ICD, at once
			reg.set(litWakefulness(r.fake))
		})
		t.Run("recreates the subscription immediately on a LIT->SIT runtime flip", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := litWakefulness(r.fake)
			r.start(fixed(w), nil)
			w.NoteActive()
			r.wantAttempts(1)
			r.wantActive(true)
			w.SetRequiresAwait(false)
			r.wantAttempts(2)
			if _, closes := r.count(); closes != 1 || r.prov.probeCount() != 0 || !r.sub.Active() {
				t.Fatalf("%d closes, %d probes, active %v", closes, r.prov.probeCount(), r.sub.Active())
			}
		})
		t.Run("recreates after the next check-in on a SIT->LIT runtime flip", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := sitWakefulness(r.fake)
			r.start(fixed(w), nil)
			r.wantAttempts(1)
			r.wantActive(true)
			w.SetRequiresAwait(true)
			r.wantActive(false)
			r.wantAttempts(1)
			if _, closes := r.count(); closes != 1 {
				t.Fatalf("%d closes", closes)
			}
			w.NoteActive()
			r.wantAttempts(2)
		})
		t.Run("does not emit an active dip across an instant mode-flip recreate", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := litWakefulness(r.fake)
			r.start(fixed(w), nil)
			w.NoteActive()
			r.wantActive(true)
			r.resetEmissions()
			w.SetRequiresAwait(false)
			r.wantAttempts(2)
			if !r.sub.Active() || r.dips() {
				t.Fatal("an active dip")
			}
		})
		t.Run("reports not live when closed while a mode-flip recreate tears down the old subscription", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			r.closeGate = make(chan struct{})
			w := litWakefulness(r.fake)
			sub := r.start(fixed(w), nil)
			w.NoteActive()
			r.wantActive(true)
			w.SetRequiresAwait(false) // the recreate waits on the old subscription's close
			settle()
			if !sub.Active() {
				t.Fatal("not active during the recreate")
			}
			sub.Close()
			close(r.closeGate)
			<-sub.Done()
			if sub.Active() {
				t.Fatal("live after close")
			}
		})
		t.Run("recreates on the icdLit network profile, then reverts to the default on a later loss", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := litWakefulness(r.fake)
			r.start(fixed(w), nil)
			w.NoteActive()
			r.wantAttempts(1)
			w.SetRequiresAwait(false)
			r.wantAttempts(2)
			r.lose()
			r.wantAttempts(3)
			if got := r.networks(); !slices.Equal(got, []bool{true, true, false}) {
				t.Fatalf("priority %v", got)
			}
		})
		t.Run("keeps a runtime SIT→LIT peer on icdLit for a post-loss resubscribe, not just the flip recreate", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := sitWakefulness(r.fake)
			r.start(fixed(w), nil)
			r.wantAttempts(1)
			w.SetRequiresAwait(true)
			w.NoteActive()
			r.wantAttempts(2)
			r.lose()
			r.wantAttempts(3)
			if got := r.networks(); !slices.Equal(got, []bool{false, true, true}) {
				t.Fatalf("priority %v", got)
			}
		})
		t.Run("keeps active live across a report-driven SIT→LIT flip when the recreate lands in-window", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := sitWakefulness(r.fake)
			r.start(fixed(w), nil)
			r.wantAttempts(1)
			r.wantActive(true)
			r.resetEmissions()
			w.SetRequiresAwait(true)
			w.NoteActive()
			r.wantAttempts(2)
			if got := r.networks(); !got[1] || !r.sub.Active() || r.dips() {
				t.Fatalf("priority %v active %v dips %v", got, r.sub.Active(), r.dips())
			}
		})
		t.Run("drops to not-live when a mode-flip recreate parks for a peer that never wakes", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			w := sitWakefulness(r.fake)
			r.start(fixed(w), nil)
			r.wantAttempts(1)
			r.wantActive(true)
			w.SetRequiresAwait(true)
			r.advance(120 * time.Second)
			r.wantAttempts(1)
			r.wantActive(false)
			w.NoteActive()
			r.wantAttempts(2)
			r.wantActive(true)
		})
		t.Run("recreates on the first registration-induced feed while the subscription was established unfed", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			reg, f := &registration{}, &feed{}
			r.start(reg.get, f.on)
			r.wantAttempts(1)
			r.wantActive(true)
			r.resetEmissions()
			w := litWakefulness(r.fake)
			w.NoteActive()
			reg.set(w)
			f.emit(1)
			r.wantAttempts(2)
			if _, closes := r.count(); closes != 1 || !r.networks()[1] || !r.sub.Active() || r.dips() {
				t.Fatalf("%d closes, priority %v", closes, r.networks())
			}
		})
		t.Run("does not double-recreate: a stray feed signal after the peer is fed is a no-op, later flips use the observer", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			reg, f := &registration{}, &feed{}
			r.start(reg.get, f.on)
			r.wantAttempts(1)
			w := litWakefulness(r.fake)
			w.NoteActive()
			reg.set(w)
			f.emit(1)
			r.wantAttempts(2)
			f.emit(1)
			r.wantAttempts(2)
			w.SetRequiresAwait(false)
			r.wantAttempts(3)
		})
		t.Run("subscription hold", func(t *testing.T) {
			t.Parallel()
			// subscribedLitPeer is SustainedSubscriptionTest.ts:1069-1087: a
			// later subscribe fails.
			subscribedLitPeer := func(t *testing.T) (*icdRig, *icd.PeerWakefulness) {
				t.Helper()
				r := newICDRig(t, func(n int) error {
					if n > 1 {
						return errTransient
					}
					return nil
				})
				w := litWakefulness(r.fake)
				w.NoteActive()
				r.start(fixed(w), nil)
				r.wantActive(true)
				return r, w
			}
			checkInDue := func(r *icdRig) time.Time {
				return r.fake.Now().Add(litTimings.ActiveModeThreshold + litTimings.IdleModeDuration + icd.CheckInMargin)
			}
			wantDue := func(t *testing.T, r *icdRig, w *icd.PeerWakefulness) {
				t.Helper()
				w.NoteActive()
				if due, ok := w.NextCheckInDue(); !ok || !due.Equal(checkInDue(r)) {
					t.Fatalf("due %v (%v)", due, ok)
				}
			}
			t.Run("has no Check-In deadline while subscribed", func(t *testing.T) {
				t.Parallel()
				_, w := subscribedLitPeer(t)
				w.NoteActive()
				if _, ok := w.NextCheckInDue(); ok {
					t.Fatal("deadline while subscribed")
				}
			})
			t.Run("releases its hold when closed", func(t *testing.T) {
				t.Parallel()
				r, w := subscribedLitPeer(t)
				r.sub.Close()
				<-r.sub.Done()
				wantDue(t, r, w)
			})
			t.Run("releases its hold when the subscription is lost", func(t *testing.T) {
				t.Parallel()
				r, w := subscribedLitPeer(t)
				r.lose()
				r.wantActive(false)
				settle()
				wantDue(t, r, w)
			})
			t.Run("releases its hold when the recreate after a mode change fails", func(t *testing.T) {
				t.Parallel()
				r, w := subscribedLitPeer(t)
				w.SetRequiresAwait(false) // the recreate's resubscribe fails
				r.wantAttempts(2)
				w.SetRequiresAwait(true)
				wantDue(t, r, w)
			})
			t.Run("keeps its subscription when the peer's wakefulness is suspended", func(t *testing.T) {
				t.Parallel()
				r, w := subscribedLitPeer(t)
				id := r.sub.ID()
				w.Suspend()
				settle()
				if !r.sub.Active() || r.sub.ID() != id {
					t.Fatalf("active %v id %d", r.sub.Active(), r.sub.ID())
				}
			})
			t.Run("stops observing the peer's mode once closed", func(t *testing.T) {
				t.Parallel()
				r, w := subscribedLitPeer(t)
				r.sub.Close()
				<-r.sub.Done()
				w.SetRequiresAwait(false)
				r.wantAttempts(1)
			})
		})
		t.Run("ignores a feed signal for a different peer", func(t *testing.T) {
			t.Parallel()
			r := newICDRig(t, nil)
			reg, f := &registration{}, &feed{}
			r.start(reg.get, f.on)
			r.wantAttempts(1)
			w := litWakefulness(r.fake)
			w.NoteActive()
			reg.set(w)
			f.emit(2)
			r.wantAttempts(1)
		})
	})
}
