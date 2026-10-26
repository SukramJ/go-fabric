// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// labels is labels of ClientChunkedWriteTest.ts:19-25: each label encodes
// to about 40 bytes, so 50 need more than one UDP message.
func labels(count int) []userlabeldef.LabelStruct {
	out := make([]userlabeldef.LabelStruct, count)
	for i := range out {
		out[i] = userlabeldef.LabelStruct{Label: fmt.Sprintf("label%011d", i), Value: fmt.Sprintf("value%011d", i)}
	}
	return out
}

// chunkedPair is withPair (:31-57): a light with a UserLabel, and a
// network that drops — and records — a datagram larger than one UDP
// message, which a real network or peer would drop.
func chunkedPair(t *testing.T) (cp *clientPair, oversized func() []int) {
	t.Helper()
	cp = newClientPair(t, clientPairOptions{lightDecls: []device.Declaration{userlabeldef.Declare{}}})
	var mu sync.Mutex
	var dropped []int
	cp.site.Net().Filter(func(d fabrictest.Datagram) bool {
		if len(d.Payload) > exchange.MaxPayloadSize+64 {
			mu.Lock()
			dropped = append(dropped, len(d.Payload))
			mu.Unlock()
			return false
		}
		return true
	})
	return cp, func() []int {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(dropped)
	}
}

// writeLabels is writeLabels (:59-90): the LabelList written, with the
// version of the image's UserLabel cluster version(current) when version
// is set; it returns the statuses and the device's LabelList.
func writeLabels(t *testing.T, value []userlabeldef.LabelStruct, opts client.WriteOptions, version func(uint32) uint32) (interaction.WriteStatuses, []userlabeldef.LabelStruct) {
	t.Helper()
	cp, oversized := chunkedPair(t)
	w := client.WriteRequest{Path: cp.lightPath(userlabeldef.ClusterID, userlabeldef.AttrLabelList), Value: spec.List[userlabeldef.LabelStruct](value)}
	if version != nil {
		v, ok := cp.node.Endpoint(cp.lightEP).Cluster(userlabeldef.ClusterID).Version()
		if !ok {
			t.Fatal("no version of UserLabel in the image")
		}
		v = version(v)
		w.DataVersion = &v
	}
	var statuses interaction.WriteStatuses
	cp.run(t, func(ctx context.Context) (err error) {
		statuses, err = cp.node.Write(ctx, []client.WriteRequest{w}, opts)
		return err
	})
	if o := oversized(); len(o) != 0 {
		t.Fatalf("oversized datagrams %v", o)
	}
	return statuses, cp.light.UserLabel().Get().LabelList
}

func expectWritten(t *testing.T, statuses interaction.WriteStatuses, got, want []userlabeldef.LabelStruct) {
	t.Helper()
	if len(statuses) == 0 {
		t.Fatal("no statuses")
	}
	for _, s := range statuses {
		if s.Status != im.StatusSuccess {
			t.Fatalf("status %v", s.Status)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("LabelList %d entries, want %d", len(got), len(want))
	}
}

// TestClientChunkedWriteParity ports matter.js's ClientChunkedWriteTest
// (packages/node/test/node/ClientChunkedWriteTest.ts at 862ad75f) on
// fabrictest: the write chunking of sendWriteCommand
// (InteractionMessenger.ts:1269-1450) against the module's device.
func TestClientChunkedWriteParity(t *testing.T) {
	t.Parallel()

	t.Run("writes a list larger than one message as an empty REPLACE and ADDs across chunks", func(t *testing.T) {
		t.Parallel()
		statuses, got := writeLabels(t, labels(50), client.WriteOptions{}, nil)
		expectWritten(t, statuses, got, labels(50))
	})

	t.Run("writes a whole list larger than one message as an empty REPLACE and ADDs across chunks", func(t *testing.T) {
		t.Parallel()
		statuses, got := writeLabels(t, labels(50), client.WriteOptions{DisableListChunking: true}, nil)
		expectWritten(t, statuses, got, labels(50))
	})

	t.Run("writes a list with a matching data version that fits one message", func(t *testing.T) {
		t.Parallel()
		statuses, got := writeLabels(t, labels(3), client.WriteOptions{}, func(v uint32) uint32 { return v })
		expectWritten(t, statuses, got, labels(3))
	})

	t.Run("writes a list with a matching data version across chunks", func(t *testing.T) {
		t.Parallel()
		statuses, got := writeLabels(t, labels(50), client.WriteOptions{}, func(v uint32) uint32 { return v })
		expectWritten(t, statuses, got, labels(50))
	})

	t.Run("rejects every entry of a list write with a stale data version", func(t *testing.T) {
		t.Parallel()
		statuses, got := writeLabels(t, labels(3), client.WriteOptions{}, func(v uint32) uint32 { return v - 1 })
		if len(statuses) != 4 {
			t.Fatalf("%d statuses, want 4", len(statuses))
		}
		for _, s := range statuses {
			if s.Status != im.StatusDataVersionMismatch {
				t.Fatalf("status %v, want DataVersionMismatch", s.Status)
			}
		}
		if len(got) != 0 {
			t.Fatalf("LabelList %v, want empty", got)
		}
	})

	t.Run("writes small values whose statuses do not fit one WriteResponse although the request does", func(t *testing.T) {
		t.Parallel()
		cp, oversized := chunkedPair(t)
		nodeLabel := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x28, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}
		requestSize := func(count int) int {
			req := im.WriteRequest{}
			for range count {
				req.Writes = append(req.Writes, im.AttributeWrite{Path: nodeLabel, Value: im.AttributeValue{Value: spec.String("")}})
			}
			e := tlv.NewEncoder()
			req.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) { v.Value.(spec.Encodable).EncodeTLV(e, tag) })
			b, _ := e.Bytes()
			return len(b)
		}
		count := 1
		for requestSize(count+1) <= exchange.MaxPayloadSize {
			count++
		}
		writes := make([]client.WriteRequest, count)
		for i := range writes {
			writes[i] = client.WriteRequest{Path: nodeLabel, Value: spec.String("")}
		}
		var statuses interaction.WriteStatuses
		cp.run(t, func(ctx context.Context) (err error) {
			statuses, err = cp.node.Write(ctx, writes, client.WriteOptions{})
			return err
		})
		if o := oversized(); len(o) != 0 {
			t.Fatalf("oversized datagrams %v", o)
		}
		if len(statuses) != count {
			t.Fatalf("%d statuses, want %d", len(statuses), count)
		}
		for _, s := range statuses {
			if s.Status != im.StatusSuccess {
				t.Fatalf("status %v", s.Status)
			}
		}
	})
}
