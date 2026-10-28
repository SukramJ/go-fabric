// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// noImages offers nothing; the test enables its BDX scope itself.
type noImages struct{}

func (noImages) QueryImage(context.Context, mattercore.OTAPeer, provdef.QueryImageRequest) (mattercore.OTAImage, bool, error) {
	return mattercore.OTAImage{}, false, nil
}

func (noImages) Open(context.Context, string) (bdx.Blob, error) { return nil, fs.ErrNotExist }

type oneFile []byte

func (f oneFile) Open(_ context.Context, name string) (bdx.Blob, error) {
	if name != "img" {
		return nil, fs.ErrNotExist
	}
	return bytes.NewReader(f), nil
}

// Limits.BDXAdditionalMrpDelay joins the base interval of the device's
// retransmissions on an inbound BDX exchange: a Block the controller never
// acknowledges is sent again after (base + delay) × MARGIN × (1 + jitter),
// where without the delay it is the bare base. Mirrors matter.js
// BdxProtocol.ts:139-142 (the exchange's peerAdditionalMrpDelay), the BDX
// margin of NetworkProfile.ts:156-159 and MRP.ts retransmissionIntervalOf
// at 862ad75f.
func TestBDXAdditionalMrpDelayOnTheWire(t *testing.T) {
	t.Parallel()
	const delay = 2 * time.Second
	for name, margin := range map[string]time.Duration{"without": 0, "with": delay} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gap := blockRetransmissionGap(t, margin)
			// The controller's session parameters are the defaults; it has
			// just sent, so the device counts it active: base 300 ms.
			const base = mrp.SessionActiveIntervalDefault
			lo := time.Duration(float64(base+margin) * mrp.MRPBackoffMargin)
			// The bridge's acknowledgement pump sends due retransmissions
			// every 50 ms, so the wait may run up to one tick over.
			hi := time.Duration(float64(mrp.SessionIdleIntervalDefault+margin)*mrp.MRPBackoffMargin*(1+mrp.MRPBackoffJitterFactor)) + 50*time.Millisecond
			if gap < lo || gap > hi {
				t.Fatalf("Block retransmitted after %v, want %v to %v", gap, lo, hi)
			}
		})
	}
}

// blockRetransmissionGap runs a ReceiveInit against a device whose limits
// carry margin, drops every datagram of the controller once the device
// sent its Block, and returns the time between the Block and its first
// retransmission.
func blockRetransmissionGap(t *testing.T, margin time.Duration) time.Duration {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Configure: func(cfg *node.Config) {
		cfg.OTAProvider = &node.OTAProviderConfig{ImageSource: noImages{}}
		cfg.Limits.BDXAdditionalMrpDelay = margin
	}}})
	ctl := pair.Controller
	if !pair.Device.Node().BDX().EnablePeerForScope(bdx.Peer{FabricIndex: 1, NodeID: ctl.Fabric().RootNodeID}, "t", oneFile(bytes.Repeat([]byte{0xA5}, 512)),
		bdx.ScopeConfig{PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}, MessageTimeout: time.Minute}) {
		t.Fatal("enable refused")
	}
	dev, ctlAddr := pair.Device.Addr().String(), ctl.Addr().String()
	var (
		mu       sync.Mutex
		querying bool
		sent     []fabrictest.Datagram
		at       []time.Time
		gap      time.Duration
	)
	site.Net().Filter(func(d fabrictest.Datagram) bool {
		mu.Lock()
		defer mu.Unlock()
		if !querying {
			return true
		}
		if d.From.String() == dev && d.To.String() == ctlAddr {
			now := site.Fake().Now()
			for i := range sent {
				if gap == 0 && bytes.Equal(sent[i].Payload, d.Payload) {
					gap = now.Sub(at[i])
				}
			}
			sent, at = append(sent, d), append(at, now)
			return true
		}
		// The controller's acknowledgements never reach the device once it
		// sent anything after the query.
		return len(sent) == 0
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := site.Run(ctx, func(ctx context.Context) error {
		s, err := pair.Peer.Connect(ctx, peer.ConnectOptions{})
		if err != nil {
			return err
		}
		x, err := ctl.Exchanges().Initiate(s, bdx.ProtocolID, exchange.Options{})
		if err != nil {
			return err
		}
		init, err := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("t/img")}.Encode()
		if err != nil {
			return err
		}
		if err := x.Send(ctx, uint8(bdx.MessageReceiveInit), init, exchange.SendOptions{}); err != nil {
			return err
		}
		m, err := x.Next(ctx, exchange.ReceiveOptions{Timeout: 30 * time.Second})
		if err != nil {
			return err
		}
		if bdx.MessageType(m.Opcode) != bdx.MessageReceiveAccept {
			return errors.New("no ReceiveAccept")
		}
		mu.Lock()
		querying = true
		mu.Unlock()
		if err := x.Send(ctx, uint8(bdx.MessageBlockQuery), bdx.CounterOnly{BlockCounter: 0}.Encode(), exchange.SendOptions{}); err != nil {
			return err
		}
		// Take the Block; its acknowledgement is what the filter drops.
		_, err = x.Next(ctx, exchange.ReceiveOptions{Timeout: 30 * time.Second})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := site.Eventually(ctx, "the Block's retransmission", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gap != 0
	}); err != nil {
		t.Fatal(err)
	}
	return gap
}
