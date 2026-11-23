// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

// Benchmarks for the write of a persisted server subscription: the
// encoding of its record ([subscription.MarshalPeerSubscription]) and the
// row upsert ([store.Store.SaveServerSubscription]) the bridge runs each
// time a subscription becomes active. The cost grows with the path set the
// subscription carries, so each size is a run of its own:
//
//   - a few concrete paths, the shape a controller subscribing to the
//     attributes it shows sends;
//   - the most concrete paths one subscribe request carries in a TCP
//     message of [tcp.DefaultMaxPayloadSize] (an upper bound: the count is
//     taken against the encoded request alone, without the message and
//     protocol headers);
//   - [im.MaxReadPaths] paths, the acceptance ceiling of a subscribe
//     request, once with the concrete shape and once with every field of
//     the path present at its widest value — the largest record the
//     encoding produces at the ceiling.
//
// A last run loads a fabric's quota of rows at the ceiling, the read a
// start performs before it re-establishes them.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/tcp"
)

// openBenchStore opens a file-backed store with the schema applied, the
// way openTestDB does for the tests.
func openBenchStore(b *testing.B) *store.Store {
	b.Helper()
	db, err := store.OpenSQLite(filepath.Join(b.TempDir(), "matter.db"), walMode)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(context.Background(), db); err != nil {
		b.Fatal(err)
	}
	return store.New(db)
}

// concretePaths are n attribute paths naming endpoint, cluster and
// attribute, as a controller that lists what it shows sends them.
func concretePaths(n int) []im.ConcreteAttributePath {
	out := make([]im.ConcreteAttributePath, n)
	for i := range out {
		out[i] = im.ConcreteAttributePath{
			Endpoint: uint16(1 + i%254), Cluster: 0x0006 + uint32(i/254), Attribute: uint32(i % 7), //nolint:gosec // G115: i < im.MaxReadPaths
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		}
	}
	return out
}

// widestPaths are n attribute paths with every persisted field present
// at the value with the most decimal digits.
func widestPaths(n int) []im.ConcreteAttributePath {
	out := make([]im.ConcreteAttributePath, n)
	for i := range out {
		out[i] = im.ConcreteAttributePath{
			Node: ^uint64(0), Endpoint: 0xFFFE, Cluster: 0xFFFFFFFE, Attribute: 0xFFFFFFFE, ListIndex: 0xFFFE,
			HasNode: true, HasEndpoint: true, HasCluster: true, HasAttribute: true, HasListIndex: true,
			WildcardPathFlags: 0xFFFFFFFF,
		}
	}
	return out
}

// subscribeRequestSize is the encoded size of a subscribe request that
// carries paths.
func subscribeRequestSize(tb testing.TB, paths []im.ConcreteAttributePath) int {
	tb.Helper()
	enc := tlv.NewEncoder()
	im.SubscribeRequest{KeepSubscriptions: true, MinIntervalFloor: 1, MaxIntervalCeiling: 60, AttributeRequests: paths, FabricFiltered: true}.MarshalTLV(enc)
	raw, err := enc.Bytes()
	if err != nil {
		tb.Fatal(err)
	}
	return len(raw)
}

// pathsInOneTCPMessage is the largest count of concrete paths whose
// subscribe request encodes within limit bytes.
func pathsInOneTCPMessage(tb testing.TB, limit int) int {
	tb.Helper()
	lo, hi := 1, im.MaxReadPaths
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if subscribeRequestSize(tb, concretePaths(mid)) <= limit {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func peerSubscriptionOf(id uint32, paths []im.ConcreteAttributePath) subscription.PeerSubscription {
	return subscription.PeerSubscription{
		SubscriptionID: id, FabricIndex: 1, PeerNodeID: 0x0102030405060708,
		AttributeRequests: paths, IsFabricFiltered: true,
		MinIntervalFloor: 1, MaxIntervalCeiling: 3600, MaxInterval: 3600, SendInterval: 50 * time.Minute,
	}
}

func BenchmarkServerSubscriptionSave(b *testing.B) {
	tcpPaths := pathsInOneTCPMessage(b, tcp.DefaultMaxPayloadSize)
	cases := []struct {
		name  string
		paths []im.ConcreteAttributePath
	}{
		{"concrete-8", concretePaths(8)},
		{fmt.Sprintf("concrete-%d-one-tcp-message", tcpPaths), concretePaths(tcpPaths)},
		{fmt.Sprintf("concrete-%d-ceiling", im.MaxReadPaths), concretePaths(im.MaxReadPaths)},
		{fmt.Sprintf("widest-%d-ceiling", im.MaxReadPaths), widestPaths(im.MaxReadPaths)},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			s := openBenchStore(b)
			ctx := context.Background()
			rec := peerSubscriptionOf(1, c.paths)
			payload, err := subscription.MarshalPeerSubscription(rec)
			if err != nil {
				b.Fatal(err)
			}
			requestBytes := subscribeRequestSize(b, c.paths)
			b.ReportAllocs()
			for b.Loop() {
				payload, err := subscription.MarshalPeerSubscription(rec)
				if err != nil {
					b.Fatal(err)
				}
				if err := s.SaveServerSubscription(ctx, rec.SubscriptionID, rec.FabricIndex, rec.PeerNodeID, payload); err != nil {
					b.Fatal(err)
				}
			}
			// Reported after the loop: the timer reset at its start clears
			// the metrics reported before it.
			b.ReportMetric(float64(len(c.paths)), "paths")
			b.ReportMetric(float64(len(payload)), "row-bytes")
			b.ReportMetric(float64(requestBytes), "request-bytes")
		})
	}
}

// BenchmarkServerSubscriptionLoadFabricQuota loads
// [subscription.DefaultMaxSubscriptionsPerFabric] rows of one fabric, each
// at the path ceiling, and decodes every one.
func BenchmarkServerSubscriptionLoadFabricQuota(b *testing.B) {
	s := openBenchStore(b)
	ctx := context.Background()
	total := 0
	for i := range subscription.DefaultMaxSubscriptionsPerFabric {
		rec := peerSubscriptionOf(uint32(i+1), widestPaths(im.MaxReadPaths)) //nolint:gosec // G115: i < 16
		payload, err := subscription.MarshalPeerSubscription(rec)
		if err != nil {
			b.Fatal(err)
		}
		total += len(payload)
		if err := s.SaveServerSubscription(ctx, rec.SubscriptionID, rec.FabricIndex, rec.PeerNodeID, payload); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		rows, err := s.LoadServerSubscriptions(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) != subscription.DefaultMaxSubscriptionsPerFabric {
			b.Fatalf("loaded %d rows, want %d", len(rows), subscription.DefaultMaxSubscriptionsPerFabric)
		}
		for _, row := range rows {
			if _, err := subscription.UnmarshalPeerSubscription(row); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(total), "stored-bytes")
}
