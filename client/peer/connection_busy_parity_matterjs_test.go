// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/PeerConnectionBusyTest.ts
// at db91886c: a BUSY answer holds every new Sigma1 until the minimum wait
// the peer asked for has passed (PeerConnection.ts:176, :523-545, :558,
// :580, :683).
//
// matter.js's fixture starts two addresses one second apart with no
// jitter (delayBeforeNextAddress 1 s, delayAfterNetworkError 0); the rig
// runs with DelayAfterNetworkError 4 s and keeps the harness's random 0.5,
// so every wait ends 2 s (busyJitter) after matter.js's. A Busy holds every address
// here only when it came from the address a session proved to be the
// node's (BD-Matter-PeerBusyDeadline), so the cases whose BUSY holds
// another address take it from the operational address fd00::1; one from
// another address holds the attempt that received it (decision 743).

import (
	"context"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/secure/sessionparams"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// busyJitter is the waiters' jitter of the busy rig: DelayAfterNetworkError
// 4 s × the harness's random 0.5.
const busyJitter = 2 * time.Second

// addrThird is the busy rig's third address of the node host.
const addrThird = "[fd00::3]:5540"

// busyRig is the rig of PeerConnectionBusyTest.ts's fixture (:91-198): the
// node host's addresses fd00::1 — the proven operational address — and
// fd00::2 (or ips), attempts staggered by 1 s.
func busyRig(t *testing.T, ips []string, edits ...func(*Context)) (*rig507, *Peer) {
	t.Helper()
	r := newRig507(t, append([]func(*Context){func(c *Context) {
		c.Timing = &Timing{DelayBeforeNextAddress: time.Second, DelayAfterNetworkError: 4 * time.Second}
	}}, edits...)...)
	r.nodeIPs = ips
	p := r.peer(i507Host)
	r.nodeOnly()
	return r, p
}

// heldSigma1 records the first Sigma1 of each address, unanswered, for the
// case to answer later.
type heldSigma1 struct {
	mu  sync.Mutex
	got map[string]unsecured
}

func (h *heldSigma1) hold(u unsecured, to string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.got == nil {
		h.got = map[string]unsecured{}
	}
	if _, ok := h.got[to]; !ok {
		h.got[to] = u
	}
}

func (h *heldSigma1) of(t *testing.T, to string) unsecured {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	u, ok := h.got[to]
	if !ok {
		t.Fatalf("no Sigma1 to %s held", to)
	}
	return u
}

// busy answers u, sent to addr, with a Busy of d.
func (r *rig507) busy(u unsecured, addr string, d time.Duration) {
	r.status(u, addr, generalStatusBusy, secureChannelStatusBusy, busyData(uint16(d/time.Millisecond)))
	synctest.Wait()
}

func TestPeerConnectionBusyParity(t *testing.T) {
	t.Parallel()
	ips := []string{"fd00::1", "fd00::2"}

	// :200 — the BUSY from fd00::1 (in Go: the proven address).
	t.Run("holds the socket and Sigma1 of another address until the minimum wait has passed", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r, p := busyRig(t, ips)
			var busyAt time.Duration = -1
			r.respond = func(u unsecured, to string) {
				if to == addr1 && busyAt < 0 {
					busyAt = r.now()
					r.busy(u, to, 10*time.Second)
				}
			}
			r.connect(p)
			r.advance(9 * time.Second)
			if busyAt != 0 {
				t.Fatalf("Busy at %v, want at once", busyAt)
			}
			if got := r.fresh(addr2, 0, time.Hour); len(got) > 0 {
				t.Fatalf("Sigma1 to %s at %v, inside the minimum wait; order: %s", addr2, got, r.order())
			}
			r.advance(5 * time.Second)
			want := 10*time.Second + busyJitter
			if got := r.fresh(addr2, 0, time.Hour); len(got) != 1 || got[0] != want {
				t.Fatalf("Sigma1 to %s at %v, want one at %v; order: %s", addr2, got, want, r.order())
			}
		})
	})

	// :220 — the re-check once the socket is open (:580). In Go only a
	// TCP dial blocks between the attempt's first wait and its Sigma1.
	t.Run("holds the Sigma1 of a socket that opened while the BUSY arrived", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			var (
				mu       sync.Mutex
				channels = map[string]*harnessTCP{}
			)
			opened := make(chan struct{})
			dialing := make(chan struct{}, 1)
			var r *rig507
			r = newRig507(t, func(c *Context) {
				c.Timing = &Timing{DelayBeforeNextAddress: time.Second, DelayAfterNetworkError: 4 * time.Second}
				c.TransportPreference = exchange.TransportTCP
				c.DialTCP = func(ctx context.Context, addr *net.UDPAddr) (exchange.Channel, error) {
					ch := &harnessTCP{fakeTCP: newFakeTCP(addr.Port), h: r.harness}
					ch.remote.IP = addr.IP
					to := (&net.UDPAddr{IP: addr.IP, Port: addr.Port}).String()
					if to == addr2 {
						dialing <- struct{}{}
						select {
						case <-opened:
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}
					mu.Lock()
					channels[to] = ch
					mu.Unlock()
					return ch, nil
				}
			})
			r.nodeIPs = ips
			p, err := r.set.AddKnownPeer(Descriptor{
				Address: testAddr, OperationalAddress: udpAddr(addr1), OperationalHost: i507Host,
				ReportedSessionParameters: &sessionparams.Parameters{SpecificationVersion: spec15, SupportedTransports: sessionparams.TransportTCPServer},
			})
			if err != nil {
				t.Fatal(err)
			}
			r.nodeOnly()
			var held heldSigma1
			r.respond = held.hold
			r.connect(p)
			r.advance(time.Second + i507Step)
			select {
			case <-dialing:
			default:
				t.Fatalf("%s is not dialing; order: %s; %s", addr2, r.order(), r.state(p))
			}
			mu.Lock()
			ch1 := channels[addr1]
			mu.Unlock()
			busy := exchange.StatusReport{
				GeneralCode: generalStatusBusy, ProtocolID: uint32(mrp.SecureChannelProtocolID),
				ProtocolCode: secureChannelStatusBusy, ProtocolData: busyData(10000),
			}
			r.replyOn(ch1, held.of(t, addr1), mrp.SCOpcodeStatusReport, busy.Encode())
			synctest.Wait()
			busyAt := r.now()
			close(opened)
			r.collect()
			r.advance(9 * time.Second)
			if got := r.fresh(addr2, 0, time.Hour); len(got) > 0 {
				t.Fatalf("Sigma1 on the socket opened during the BUSY at %v; order: %s", got, r.order())
			}
			r.advance(4 * time.Second)
			// The address's TCP and UDP attempts both send at the end.
			want := busyAt + 10*time.Second + busyJitter
			if got := r.fresh(addr2, 0, time.Hour); len(got) == 0 || got[0] != want {
				t.Fatalf("Sigma1 to %s at %v, want the first at %v; order: %s", addr2, got, want, r.order())
			}
		})
	})

	// :259 — random 0.5, delayAfterNetworkError 4 s: the end of the wait
	// moves by 2 s.
	t.Run("adds a random delay to the end of the wait of another address", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r, p := busyRig(t, ips)
			r.respond = func(u unsecured, to string) {
				if to == addr1 && len(r.fresh(addr1, 0, time.Hour)) == 1 {
					r.busy(u, to, 10*time.Second)
				}
			}
			r.connect(p)
			r.advance(12*time.Second - i507Step)
			if got := r.fresh(addr2, 0, time.Hour); len(got) > 0 {
				t.Fatalf("Sigma1 to %s at %v, before the jitter passed; order: %s", addr2, got, r.order())
			}
			r.advance(i507Step)
			if got := r.fresh(addr2, 0, time.Hour); len(got) != 1 || got[0] != 12*time.Second {
				t.Fatalf("Sigma1 to %s at %v, want one at 12s; order: %s", addr2, got, r.order())
			}
		})
	})

	// :288 — existing behaviour.
	t.Run("lets a Sigma1 already sent on another address continue", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r, p := busyRig(t, ips)
			var held heldSigma1
			r.respond = held.hold
			r.connect(p)
			r.advance(time.Second)
			if len(r.fresh(addr1, 0, time.Hour)) != 1 || len(r.fresh(addr2, 0, time.Hour)) != 1 {
				t.Fatalf("want one Sigma1 to each address; order: %s", r.order())
			}
			second := attemptOf(p, addr2)
			x := second.exchange.Load()
			r.busy(held.of(t, addr1), addr1, 10*time.Second)
			r.advance(time.Second)
			if attemptOf(p, addr2) != second || second.ctx.Err() != nil || second.exchange.Load() != x {
				t.Fatalf("the BUSY ended the Sigma1 in flight on %s; %s", addr2, r.state(p))
			}
		})
	})

	// :309-313 — the later of two ends, in both orders. The second BUSY
	// answers fd00::2, which holds its own attempt (decision 743) beside
	// the peer's hold of the proven address's BUSY: fd00::2 waits for the
	// later end. That the first BUSY's end does not hold fd00::1 longer
	// when the second asked for more is BD-Matter-PeerBusyDeadline.
	for _, c := range []struct {
		name          string
		first, second time.Duration
	}{
		{"waits for the later end when BUSY requests 10s and then 20s", 10 * time.Second, 20 * time.Second},
		{"waits for the later end when BUSY requests 20s and then 10s", 20 * time.Second, 10 * time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				r, p := busyRig(t, ips)
				var held heldSigma1
				r.respond = held.hold
				r.connect(p)
				r.advance(2 * time.Second)
				firstEnd := r.now() + c.first
				r.busy(held.of(t, addr1), addr1, c.first)
				if c.first > c.second {
					// A later Busy with an earlier end does not move the
					// deadline (PeerConnection.ts:542).
					before := p.busyRemaining()
					p.holdBusy(c.second)
					if got := p.busyRemaining(); got != before {
						t.Fatalf("a %v hold moved the %v deadline: %v remaining, want %v", c.second, c.first, got, before)
					}
				}
				r.advance(2 * time.Second)
				secondEnd := r.now() + c.second
				r.busy(held.of(t, addr2), addr2, c.second)
				end := max(firstEnd, secondEnd) + busyJitter
				r.advance(end - r.now() - i507Step)
				if got := r.fresh(addr2, 2*time.Second, time.Hour); len(got) > 0 {
					t.Fatalf("Sigma1 to %s at %v, before the later end %v; order: %s", addr2, got, end, r.order())
				}
				r.advance(i507Step)
				if got := r.fresh(addr2, 2*time.Second, time.Hour); len(got) != 1 || got[0] != end {
					t.Fatalf("Sigma1 to %s at %v, want at the later end %v; order: %s", addr2, got, end, r.order())
				}
			})
		})
	}

	// :350 — three addresses, the handler's 100 ms. matter.js's two
	// 10 s BUSYs come from fd00::1 and fd00::2; here both must come from
	// the proven fd00::1, which has one Sigma1 in flight, so the second
	// is the hold its handling records (Peer.holdBusy, what holdBusyAt
	// does for a Busy at the proven address).
	t.Run("extends the wait of a waiting attempt for each BUSY that arrives during it", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r, p := busyRig(t, []string{"fd00::1", "fd00::2", "fd00::3"}, func(c *Context) {
				c.HandleError = func(error) (time.Duration, bool, error) { return 100 * time.Millisecond, true, nil }
			})
			var held heldSigma1
			r.respond = func(u unsecured, to string) {
				held.hold(u, to)
				if to == addrThird {
					r.busy(u, to, 5*time.Second)
				}
			}
			r.connect(p)
			r.advance(3 * time.Second)
			if n := countFresh(r); n != 3 {
				t.Fatalf("%d Sigma1 messages, want 3; order: %s", n, r.order())
			}
			r.busy(held.of(t, addr1), addr1, 10*time.Second)
			r.advance(9 * time.Second)
			p.holdBusy(10 * time.Second)
			end := r.now() + 10*time.Second + busyJitter
			r.advance(end - r.now() - i507Step)
			if n := countFresh(r); n != 3 {
				t.Fatalf("%d Sigma1 messages before the second end %v, want 3; order: %s", n, end, r.order())
			}
			r.advance(i507Step)
			if n := countFresh(r); n <= 3 {
				t.Fatalf("no Sigma1 at the second end %v; order: %s", end, r.order())
			}
		})
	})

	// :386 — the handler sees the BUSY and returns 100 ms; the minimum
	// wait holds. In Go the unproven variant holds the attempt that
	// received it (decision 743).
	for _, c := range []struct{ name, at string }{
		{"calls handleError for BUSY and keeps the minimum wait when it returns a shorter delay", addr1},
		{"calls handleError for BUSY and keeps the minimum wait when it returns a shorter delay (unproven address)", addr2},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				var (
					mu      sync.Mutex
					handled []time.Duration
				)
				r, p := busyRig(t, ips, func(ctx *Context) {
					ctx.HandleError = func(err error) (time.Duration, bool, error) {
						d, _ := busyDelayOf(err)
						mu.Lock()
						handled = append(handled, d)
						mu.Unlock()
						return 100 * time.Millisecond, true, nil
					}
				})
				var busyAt time.Duration = -1
				r.respond = func(u unsecured, to string) {
					if to == c.at && busyAt < 0 {
						busyAt = r.now()
						r.busy(u, to, 10*time.Second)
					}
				}
				r.connect(p)
				r.advance(time.Second)
				end := busyAt + 10*time.Second + busyJitter
				r.advance(end - r.now() - i507Step)
				mu.Lock()
				got := append([]time.Duration(nil), handled...)
				mu.Unlock()
				if len(got) != 1 || got[0] != 10*time.Second {
					t.Fatalf("handleError saw busy delays %v, want [10s]", got)
				}
				if n := len(r.fresh(c.at, 0, time.Hour)); n != 1 {
					t.Fatalf("%d Sigma1 messages to %s inside the minimum wait; order: %s", n, c.at, r.order())
				}
				r.advance(i507Step)
				if got := r.fresh(c.at, 0, time.Hour); len(got) != 2 || got[1] != end {
					t.Fatalf("Sigma1 to %s at %v, want the second at %v; order: %s", c.at, got, end, r.order())
				}
			})
		})
	}

	// :414 — fd00::2's BUSY holds its attempt (in Go only its own); the
	// session fd00::1 establishes ends it. The session is handed to the set
	// with r.set.Adopt, not by completing the CASE in flight at fd00::1:
	// the harness answers Sigma1 with status reports only and has no CASE
	// responder (no client/peer test completes a handshake), and adopting a
	// session is what ends the process either way (as in
	// TestIssue507_BusyDeadline's "a session clears it").
	t.Run("ends waiting attempts when the attempt in progress establishes a session", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r, p := busyRig(t, ips, func(c *Context) {
				c.HandleError = func(error) (time.Duration, bool, error) { return 100 * time.Millisecond, true, nil }
			})
			r.respond = func(u unsecured, to string) {
				if to == addr2 {
					r.busy(u, to, 10*time.Second)
				}
			}
			r.connect(p)
			r.advance(3 * time.Second)
			if n := len(r.fresh(addr2, 0, time.Hour)); n != 1 {
				t.Fatalf("%d Sigma1 messages to %s, want 1; order: %s", n, addr2, r.order())
			}
			r.set.Adopt(r.secureSession(t, 0x5b, udpAddr(addr1)))
			eventually(t, "the process to end", func() bool { return process(p) == nil })
			r.advance(15 * time.Second)
			if n := len(r.fresh(addr2, 0, time.Hour)); n != 1 {
				t.Fatalf("%d Sigma1 messages to %s after the session, want 1; order: %s", n, addr2, r.order())
			}
		})
	})
}

// countFresh is the number of fresh Sigma1 transmissions the rig saw.
func countFresh(r *rig507) int {
	n := 0
	for _, s := range r.sent {
		if s.first {
			n++
		}
	}
	return n
}
