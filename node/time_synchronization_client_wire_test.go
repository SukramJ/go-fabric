// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
	"github.com/SukramJ/go-fabric/tlv"
)

// wireNTP is a host NTPClient answering at, or failing while at is zero.
type wireNTP struct {
	mu      sync.Mutex
	at      time.Time
	servers []string
}

func (f *wireNTP) QueryTime(_ context.Context, server string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers = append(f.servers, server)
	if f.at.IsZero() {
		return time.Time{}, errors.New("no answer")
	}
	return f.at, nil
}

// The time client on the wire (brief 39 §2.3, after connectedhomeip's
// TimeSynchronizationCluster.cpp:379-593 at 6170af84): a controller sets
// the source node's UTC with SetUTCTime, which moves its offset (the host
// clock stays); a node whose host clock is no time source, told the source
// as its trusted time source, reads the source's UTCTime and Granularity
// over its own client stack and adopts the time with TimeSource
// NodeTimeCluster and seconds for the source's milliseconds. A second such
// node the source does not let read falls back to its host's NTP client
// asking DefaultNTP (NonMatterSNTP); both emitted TimeFailure at start,
// when they had no source.
func TestTimeSynchronizationClientOnTheWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	timeClient := func(ntp node.NTPClient) func(cfg *node.Config) {
		return func(cfg *node.Config) {
			cfg.Client = &node.ClientConfig{}
			cfg.TimeSynchronization.Config = &node.TimeSynchronizationConfig{
				Features: tsdef.FeatureTimeSyncClient | tsdef.FeatureNtpClient,
				PlatformTime: func() (tsdef.TimeSourceEnum, tsdef.GranularityEnum) {
					return tsdef.TimeSourceNone, tsdef.GranularityNoTimeGranularity
				},
				NTPClient: ntp,
			}
			cfg.Surface.Extensions = surface.NewExtensions("BD-chip-TimeSynchronization-Features")
		}
	}
	ntpAt := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	ntp := &wireNTP{at: ntpAt}
	source := site.AddDevice(fabrictest.Topology{})
	reader := site.AddDevice(fabrictest.Topology{Configure: timeClient(nil)})
	fallback := site.AddDevice(fabrictest.Topology{Configure: timeClient(ntp)})
	c := site.AddController(fabrictest.ControllerOptions{})
	pairS, err := c.Commission(ctx, source, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatalf("commission the source: %v", err)
	}
	pairR, err := c.Commission(ctx, reader, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatalf("commission the reader: %v", err)
	}
	pairF, err := c.Commission(ctx, fallback, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatalf("commission the fallback: %v", err)
	}
	cmd := func(id uint32) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: 0, Cluster: tsdef.ClusterID, Command: id}
	}
	attr := func(id uint32) fabrictest.AttributePath { return client.AttributeAt(0, tsdef.ClusterID, id) }
	invoke := func(p *fabrictest.Pair, id uint32, fields spec.Encodable) {
		t.Helper()
		res, err := fabrictest.Invoke(ctx, p, cmd(id), fields)
		if err != nil || res.Status != im.StatusSuccess {
			t.Fatalf("command 0x%02X: %+v %v", id, res, err)
		}
	}

	// Both clients started without a time and without a source.
	for label, p := range map[string]*fabrictest.Pair{"reader": pairR, "fallback": pairF} {
		evs, err := fabrictest.Events(ctx, p, client.EventAt(0, tsdef.ClusterID, tsdef.EventTimeFailure))
		if err != nil || len(evs) != 1 {
			t.Errorf("%s: TimeFailure events %d %v, want one", label, len(evs), err)
		}
		if v, err := fabrictest.Read(ctx, p, attr(tsdef.AttrUtcTime)); err != nil || v.Value.El.Type != tlv.TypeNull {
			t.Errorf("%s: UTCTime %+v %v, want null", label, v.Value, err)
		}
	}

	// SetUTCTime moves the source's UTC, not its host clock.
	sourceAt := time.Now().Add(30 * 24 * time.Hour)
	invoke(pairS, tsdef.CmdSetUtcTime, tsdef.SetUtcTimeRequest{
		UtcTime: uint64(sourceAt.UnixMicro() - 946684800_000_000), Granularity: tsdef.GranularityMillisecondsGranularity, //nolint:gosec // after the epoch
	})
	utc, ok := source.Node().Root().TimeSynchronization.UTC()
	if !ok || utc.Sub(sourceAt).Abs() > time.Minute {
		t.Fatalf("source UTC %v %v, want about %v", utc, ok, sourceAt)
	}

	// The reader may read the source; the fallback may not.
	if err := source.Node().Store().ReplaceACL(ctx, 1, []store.ACLEntry{{
		FabricIndex: 1, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{c.Fabric().RootNodeID},
	}, {
		FabricIndex: 1, Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE,
		Subjects: []uint64{pairR.Address.NodeID},
	}}); err != nil {
		t.Fatal(err)
	}
	trusted := tsdef.SetTrustedTimeSourceRequest{TrustedTimeSource: spec.ValueOf(tsdef.FabricScopedTrustedTimeSourceStruct{NodeId: pairS.Address.NodeID, Endpoint: 0})}
	invoke(pairF, tsdef.CmdSetDefaultNtp, tsdef.SetDefaultNtpRequest{DefaultNtp: spec.ValueOf("fd00::123")})
	invoke(pairR, tsdef.CmdSetTrustedTimeSource, trusted)
	invoke(pairF, tsdef.CmdSetTrustedTimeSource, trusted)

	quality := func(d *fabrictest.Device) (any, any) {
		srv := d.Node().Root().TimeSynchronization
		g, _ := srv.MatterRead(tsdef.AttrGranularity)
		s, _ := srv.MatterRead(tsdef.AttrTimeSource)
		return g, s
	}
	if err := site.Eventually(ctx, "the reader adopts the source's time", func() bool {
		_, s := quality(reader)
		return s == uint8(tsdef.TimeSourceNodeTimeCluster)
	}); err != nil {
		t.Fatal(err)
	}
	if g, _ := quality(reader); g != uint8(tsdef.GranularitySecondsGranularity) {
		t.Errorf("reader Granularity %v, want Seconds for the source's milliseconds", g)
	}
	utc, ok = reader.Node().Root().TimeSynchronization.UTC()
	if !ok || utc.Sub(sourceAt).Abs() > time.Minute {
		t.Errorf("reader UTC %v %v, want about the source's %v", utc, ok, sourceAt)
	}
	if v, err := fabrictest.Read(ctx, pairR, attr(tsdef.AttrTimeSource)); err != nil || v.Value.El.Uint != uint64(tsdef.TimeSourceNodeTimeCluster) {
		t.Errorf("reader TimeSource on the wire %+v %v", v.Value, err)
	}

	if err := site.Eventually(ctx, "the fallback takes the NTP time", func() bool {
		_, s := quality(fallback)
		return s == uint8(tsdef.TimeSourceNonMatterSntp)
	}); err != nil {
		t.Fatal(err)
	}
	if g, _ := quality(fallback); g != uint8(tsdef.GranularityMillisecondsGranularity) {
		t.Errorf("fallback Granularity %v, want Milliseconds", g)
	}
	utc, ok = fallback.Node().Root().TimeSynchronization.UTC()
	if !ok || utc.Sub(ntpAt).Abs() > time.Minute {
		t.Errorf("fallback UTC %v %v, want about the NTP time %v", utc, ok, ntpAt)
	}
	ntp.mu.Lock()
	servers := slices.Clone(ntp.servers)
	ntp.mu.Unlock()
	if !slices.Equal(servers, []string{"fd00::123"}) {
		t.Errorf("NTP asked %v, want DefaultNTP once", servers)
	}
}
