// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// "refreshes a Sigma1 that is never acknowledged", the case matter.js
// #4794 added to packages/node/test/node/ClientConnectivityTest.ts at
// db021da0 (:333-409), ported by meaning onto the connection process: a
// peer whose Sigma1 messages are all lost is first sent one Sigma1 for at
// least the MRP budget, then fresh ones — a new exchange and ephemeral key
// each — three transmissions apiece with the backoff continued instead of
// restarting at its base interval (PeerConnection.ts:639-653,
// InitialContactSchedule.ts). matter.js loses every Sigma1 of a running
// device for 20 minutes and then lets one through; here the socket
// answers nothing, and the 20 minutes are read the same way.

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

func TestConnectionRefreshesUnacknowledgedSigma1Parity(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr9)})
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		start := h.clock.Now()
		type sigma1 struct {
			first time.Duration
			at    []time.Duration
		}
		byExchange := map[uint16]*sigma1{}
		var order []uint16
		drain := func() {
			for {
				select {
				case d := <-h.udp.ch:
					if d.to.String() != addr9 {
						continue
					}
					u := decodeUnsecured(t, d.bytes)
					if u.proto.Opcode != mrp.SCOpcodeSigma1 {
						continue
					}
					elapsed := h.clock.Now().Sub(start)
					s, ok := byExchange[u.proto.ExchangeID]
					if !ok {
						s = &sigma1{first: elapsed}
						byExchange[u.proto.ExchangeID] = s
						order = append(order, u.proto.ExchangeID)
					}
					s.at = append(s.at, elapsed)
				default:
					return
				}
			}
		}
		const lossWindow = 20 * time.Minute
		for h.clock.Now().Sub(start) < lossWindow {
			synctest.Wait()
			drain()
			h.clock.Advance(time.Second)
		}
		synctest.Wait()
		drain()

		timing := DefaultTiming()
		if len(order) < 2 {
			t.Fatalf("%d Sigma1 messages in %v, want a fresh one after the first", len(order), lossWindow)
		}
		if first := byExchange[order[0]]; len(first.at) < exchange.MaxTransmissions {
			t.Fatalf("the first Sigma1 went out %d times, want at least the MRP budget %d", len(first.at), exchange.MaxTransmissions)
		}
		// Each fresh Sigma1 whose three transmissions and final wait fit
		// into the window goes out three times, continuing the backoff.
		refreshCycle := 4 * timing.MaxDelayBetweenInitialContactRetries
		refreshed := 0
		for _, id := range order[1:] {
			s := byExchange[id]
			if s.first >= lossWindow-refreshCycle {
				continue
			}
			refreshed++
			if len(s.at) != transmissionsPerRefresh {
				t.Fatalf("a fresh Sigma1 went out %d times, want %d", len(s.at), transmissionsPerRefresh)
			}
			if gap := s.at[1] - s.at[0]; gap <= 2*time.Second {
				t.Fatalf("a fresh Sigma1's first retransmission after %v, want the continued backoff", gap)
			}
		}
		t.Logf("%d Sigma1 messages, the first sent %d times, %d fresh ones inside the window", len(order), len(byExchange[order[0]].at), refreshed)
		if refreshed == 0 {
			t.Fatal("no fresh Sigma1 inside the window")
		}
		p.Close(closingCtx())
	})
}
