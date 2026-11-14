// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
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
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// paseAnswerer is a device that answers a PBKDFParamRequest with its
// PBKDFParamResponse and then says nothing more; it reports the Pake1 that
// follows on pake1. It serves as the socket of a manager and as a
// commissioning channel.
type paseAnswerer struct {
	mgr    *exchange.Manager
	addr   *net.UDPAddr
	pake1  chan struct{}
	closed chan struct{}
	once   sync.Once
}

// Send is the device receiving a datagram; one it cannot read it drops.
func (p *paseAnswerer) Send(_ *net.UDPAddr, b []byte) error {
	p.receive(b)
	return nil
}

func (p *paseAnswerer) receive(b []byte) {
	hdr, hl, err := message.UnmarshalHeader(b)
	if err != nil {
		return
	}
	proto, pl, err := message.UnmarshalProtocolHeader(b[hl:])
	if err != nil {
		return
	}
	switch proto.Opcode {
	case mrp.SCOpcodePBKDFParamRequest:
		req, err := spake2.DecodePBKDFParamRequest(b[hl+pl:])
		if err != nil {
			return
		}
		resp := spake2.PBKDFParamResponse{
			InitiatorRandom: req.InitiatorRandom, ResponderRandom: bytes.Repeat([]byte{0x5a}, spake2.PBKDFRandomSize),
			ResponderSessionID: 0x4242, Parameters: &spake2.PBKDFParameters{Iterations: 1000, Salt: bytes.Repeat([]byte{0x33}, 16)},
		}
		rh := message.Header{MessageCounter: 7, DestSize: message.DestNodeID, DestNodeID: hdr.SourceNodeID}
		rp := message.ProtocolHeader{HasAck: true, AckCounter: hdr.MessageCounter, Opcode: mrp.SCOpcodePBKDFParamResponse, ExchangeID: proto.ExchangeID, ProtocolID: proto.ProtocolID}
		out := append(append(rh.Marshal(), rp.Marshal()...), resp.Marshal()...)
		go p.mgr.HandleDatagram(out, p.addr)
	case mrp.SCOpcodePake1:
		select {
		case p.pake1 <- struct{}{}:
		default:
		}
	}
}

func (p *paseAnswerer) Exchanges() *exchange.Manager { return p.mgr }
func (p *paseAnswerer) PeerAddress() *net.UDPAddr    { return p.addr }
func (p *paseAnswerer) Closed() <-chan struct{}      { return p.closed }
func (p *paseAnswerer) ClosedCause() error           { return errors.New("gone") }
func (p *paseAnswerer) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

// feedTransport reports each device sent on it.
type feedTransport chan discovery.Device

func (feedTransport) Kind() discovery.TransportKind { return discovery.TransportIP }

func (f feedTransport) FindContinuously(ctx context.Context, _ discovery.Identifier, found func(discovery.Device)) error {
	for {
		select {
		case d := <-f:
			found(d)
		case <-ctx.Done():
			return nil
		}
	}
}

// TestCommissionTellsTheRaceItsDeviceAnswered: a commissioning that runs
// as a race attempt marks the attempt answered once the device's
// PBKDFParamResponse has arrived — over a commissioning channel and over
// the device's addresses — so candidates found later, however many, never
// end it for their place (review II, owner decision 55).
func TestCommissionTellsTheRaceItsDeviceAnswered(t *testing.T) {
	t.Parallel()
	for _, overChannel := range []bool{true, false} {
		t.Run(fmt.Sprintf("channel=%v", overChannel), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) { commissionAsAnsweredAttempt(t, overChannel) })
		})
	}
}

// commissionAsAnsweredAttempt is one case of
// TestCommissionTellsTheRaceItsDeviceAnswered, inside a synctest bubble.
func commissionAsAnsweredAttempt(t *testing.T, overChannel bool) {
	t.Helper()
	ctx := t.Context()
	dev := &paseAnswerer{addr: &net.UDPAddr{IP: net.ParseIP("fd00::2"), Port: 5540}, pake1: make(chan struct{}, 1), closed: make(chan struct{})}
	mgr, err := exchange.NewManager(dev, exchange.Config{Clock: newFakeClock(), Reliable: overChannel})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	dev.mgr = mgr
	authority, err := ca.New(ca.Config{Clock: clocktest.New(time.Unix(1_700_000_000, 0))})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := NewFabric(authority, FabricConfig{FabricID: 7, RootNodeID: 1, VendorID: 0xFFF1})
	if err != nil {
		t.Fatal(err)
	}
	c := &Commissioner{ctx: Context{Exchanges: mgr, Clock: newFakeClock()}, log: discard()}
	opts := Options{Fabric: fabric, Passcode: 20202021, Timeout: time.Hour}
	if overChannel {
		opts.Channel = dev
	} else {
		opts.Addresses = []*net.UDPAddr{dev.addr}
	}

	raceClock := clocktest.New(time.Unix(1_000_000, 0))
	feed := make(feedTransport)
	started := make(chan string, 1)
	var mu sync.Mutex
	deviceEnded := false
	s := discovery.Race(ctx, []discovery.Transport{feed}, discovery.SessionOptions{Clock: raceClock, Timeout: 1000 * time.Hour},
		func(actx context.Context, cand discovery.Candidate, _ func() bool) (string, error) {
			if cand.Device.InstanceID != "device" {
				started <- cand.Device.InstanceID
				<-actx.Done()
				return "", actx.Err()
			}
			_, err := c.Commission(actx, opts)
			mu.Lock()
			deviceEnded = true
			mu.Unlock()
			return "", err
		})
	defer s.Cancel(nil)
	send := func(id string) {
		t.Helper()
		feed <- discovery.Device{InstanceID: id}
		synctest.Wait()
	}
	send("device")
	select {
	case <-dev.pake1:
	default:
		t.Fatal("the device's attempt sent no Pake1")
	}
	// Far more candidates than the race has places, each given
	// past its ceiling before the next is found.
	for i := range 64 {
		id := fmt.Sprintf("other-%02d", i)
		send(id)
		raceClock.Advance(time.Hour)
		synctest.Wait()
		select {
		case got := <-started:
			if got != id {
				t.Fatalf("started %s, want %s", got, id)
			}
		default:
			t.Fatalf("%s did not start past its ceiling", id)
		}
		mu.Lock()
		ended := deviceEnded
		mu.Unlock()
		if ended {
			t.Fatalf("the device's attempt, answered, ended for a later candidate (%s)", id)
		}
	}
}
