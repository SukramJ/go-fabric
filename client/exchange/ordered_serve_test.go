// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// TestOrderedServeKeepsASessionsDatagramsInOrder: the manager on a UDP
// socket served in order per secure session (transport/udp ServeOrdered
// keyed by message.SessionOrderKey, as controller.New serves its socket)
// takes a session's messages in the order they arrived, so a later message
// never overtakes an earlier one on the receive path — matter.js handles
// each datagram's non-I/O continuation before the next (ExchangeManager.ts:661
// at db021da0, a runtime property), chip on its one thread
// (ExchangeMgr.cpp:295-302 at 6170af84). Without the order the listener
// gives every datagram a goroutine of its own and a burst arrives shuffled.
func TestOrderedServeKeepsASessionsDatagramsInOrder(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, Config{})
	s, p := newSecurePair(t, m, 0x0314, false)
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.ServeOrdered(ctx, message.SessionOrderKey, func(buf []byte, src *net.UDPAddr, _ func()) { m.HandleDatagram(buf, src) })
	}()
	t.Cleanup(func() { cancel(); <-served; _ = l.Close() })

	const burst = 200
	var mu sync.Mutex
	var counters []uint32
	all := make(chan struct{})
	x, err := m.Initiate(s, im.InteractionModelProtocolID, Options{OnReceive: func(msg *Message, _ bool) {
		mu.Lock()
		defer mu.Unlock()
		counters = append(counters, msg.Counter)
		if len(counters) == burst {
			close(all)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := net.DialUDP("udp4", nil, l.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	for range burst {
		hdr := message.Header{SessionID: s.ID()}
		proto := message.ProtocolHeader{Opcode: im.OpcodeReportData, ExchangeID: x.ID(), ProtocolID: im.InteractionModelProtocolID}
		sealed, err := p.ch.Encrypt(&hdr, hdr.SecurityFlags(), append(proto.Marshal(), 0x15, 0x18))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peer.Write(append(hdr.Marshal(), sealed.Ciphertext...)); err != nil {
			t.Fatal(err)
		}
	}
	wait, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	select {
	case <-all:
	case <-wait.Done():
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("%d of %d messages taken", len(counters), burst)
	}
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(counters); i++ {
		if counters[i] <= counters[i-1] {
			t.Fatalf("message %d (counter %d) taken after counter %d: out of arrival order", i, counters[i], counters[i-1])
		}
	}
}
