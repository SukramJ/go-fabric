// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// TimeSynchronization's features after connectedhomeip at 6170af84:
// src/app/clusters/time-synchronization-server/tests/
// TestTimeSynchronizationCluster.cpp (AttributeTest :52-178,
// ReadAttributeTest :180-340, SetTrustedTimeSourceTest :342-398), ported
// by meaning. The blocks with the optional TimeSource attribute are left
// out: the server does not serve it.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// tsEvents records the events with their payloads.
type tsEvents struct {
	mu     sync.Mutex
	ids    []uint32
	data   []any
	onRoot bool
}

func (r *tsEvents) MatterEmitEvent(endpoint uint16, _, event uint32, data any, _ contract.EventPriority) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onRoot = endpoint == 0
	r.ids = append(r.ids, event)
	r.data = append(r.data, data)
}

func (r *tsEvents) take() ([]uint32, []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids, data := r.ids, r.data
	r.ids, r.data = nil, nil
	return ids, data
}

// tsSettings is an in-memory store.Settings.
type tsSettings struct {
	mu   sync.Mutex
	m    map[string]string
	fail bool
}

func (s *tsSettings) GetSetting(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *tsSettings) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("storage full")
	}
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[key] = value
	return nil
}

func (s *tsSettings) GetMetadataCounter(context.Context, string) (uint64, bool, error) {
	return 0, false, nil
}
func (s *tsSettings) SetMetadataCounter(context.Context, string, uint64) error { return nil }

// tsNow is 2026-01-01, in Matter epoch microseconds below.
var tsNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func tsServer(t *testing.T, cfg core.TimeSynchronizationConfig) (*core.TimeSynchronization, *tsEvents, *clocktest.Fake) {
	t.Helper()
	srv, err := core.NewTimeSynchronizationWith(cfg)
	if err != nil {
		t.Fatal(err)
	}
	clk := clocktest.New(tsNow)
	srv.SetClock(clk)
	rec := &tsEvents{}
	srv.SetMatterEventEmitter(rec)
	return srv, rec, clk
}

func tsInvoke(ctx context.Context, srv *core.TimeSynchronization, cmd uint32, fields any) (any, error) {
	return srv.MatterInvoke(ctx, cmd, fields)
}

func tsStatus(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	var s interface{ MatterStatusCode() im.StatusCode }
	if errors.As(err, &s) {
		return s.MatterStatusCode()
	}
	return im.StatusFailure
}

// TestChipTimeSyncAttributeTest ports AttributeTest: each feature adds its
// attributes to UTCTime and Granularity.
func TestChipTimeSyncAttributeTest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		features tsdef.Feature
		want     []uint32
	}{
		{0, nil},
		{tsdef.FeatureTimeSyncClient, []uint32{tsdef.AttrTrustedTimeSource}},
		{tsdef.FeatureNtpClient, []uint32{tsdef.AttrDefaultNtp, tsdef.AttrSupportsDnsResolve}},
		{tsdef.FeatureNtpServer, []uint32{tsdef.AttrNtpServerAvailable}},
		{tsdef.FeatureTimeZone, []uint32{
			tsdef.AttrTimeZone, tsdef.AttrDstOffset, tsdef.AttrLocalTime, tsdef.AttrTimeZoneDatabase,
			tsdef.AttrTimeZoneListMaxSize, tsdef.AttrDstOffsetListMaxSize,
		}},
	} {
		srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tc.features})
		var got []uint32
		for _, id := range srv.MatterAttributes() {
			if id < 0xF000 {
				got = append(got, id)
			}
		}
		want := append([]uint32{tsdef.AttrUtcTime, tsdef.AttrGranularity}, tc.want...)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("features 0x%X: attributes %v, want %v", uint32(tc.features), got, want)
		}
	}
}

// TestChipTimeSyncReadAttributeTest ports ReadAttributeTest: every
// attribute of each selection reads, with chip's values where they are
// fixed.
func TestChipTimeSyncReadAttributeTest(t *testing.T) {
	t.Parallel()
	for _, f := range []tsdef.Feature{0, tsdef.FeatureTimeSyncClient, tsdef.FeatureNtpClient, tsdef.FeatureNtpServer, tsdef.FeatureTimeZone} {
		srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{Features: f})
		if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(f) {
			t.Errorf("features 0x%X: FeatureMap %v", uint32(f), fm)
		}
		for _, id := range srv.MatterAttributes() {
			if _, ok := srv.MatterRead(id); !ok {
				t.Errorf("features 0x%X: attribute 0x%04X does not read", uint32(f), id)
			}
		}
	}
	srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone})
	for id, want := range map[uint32]any{
		tsdef.AttrTimeZoneListMaxSize:  uint8(2),
		tsdef.AttrDstOffsetListMaxSize: uint8(2),
		tsdef.AttrTimeZoneDatabase:     tsdef.TimeZoneDatabaseNone,
	} {
		if got, _ := srv.MatterRead(id); got != want {
			t.Errorf("attribute 0x%04X = %v, want %v", id, got, want)
		}
	}
	// InitTimeZone's one default entry; no DST offset, so LocalTime null.
	if tz, _ := srv.MatterRead(tsdef.AttrTimeZone); len(tz.(spec.List[tsdef.TimeZoneStruct])) != 1 {
		t.Errorf("TimeZone %v, want the default entry", tz)
	}
	if lt, ok := srv.MatterRead(tsdef.AttrLocalTime); !ok || lt != nil {
		t.Errorf("LocalTime %v %v, want null", lt, ok)
	}
}

// TestChipTimeSyncSetTrustedTimeSource ports SetTrustedTimeSourceTest: a
// source is stored with the accessing fabric and emits nothing; null
// clears it and emits MissingTrustedTimeSource, one event.
func TestChipTimeSyncSetTrustedTimeSource(t *testing.T) {
	t.Parallel()
	srv, rec, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeSyncClient})
	ctx := im.WithFabricFilter(context.Background(), false, 2)
	req := tsdef.SetTrustedTimeSourceRequest{TrustedTimeSource: spec.ValueOf(tsdef.FabricScopedTrustedTimeSourceStruct{NodeId: 1234, Endpoint: 0})}
	if _, err := tsInvoke(ctx, srv, tsdef.CmdSetTrustedTimeSource, req); err != nil {
		t.Fatal(err)
	}
	if ids, _ := rec.take(); len(ids) != 0 {
		t.Errorf("events %v, want none", ids)
	}
	v, _ := srv.MatterRead(tsdef.AttrTrustedTimeSource)
	if tts, ok := v.(tsdef.TrustedTimeSourceStruct); !ok || tts.NodeId != 1234 || tts.Endpoint != 0 || tts.FabricIndex != 2 {
		t.Fatalf("TrustedTimeSource %+v", v)
	}
	if _, err := tsInvoke(ctx, srv, tsdef.CmdSetTrustedTimeSource, tsdef.SetTrustedTimeSourceRequest{TrustedTimeSource: spec.NullOf[tsdef.FabricScopedTrustedTimeSourceStruct]()}); err != nil {
		t.Fatal(err)
	}
	if ids, _ := rec.take(); !slices.Equal(ids, []uint32{tsdef.EventMissingTrustedTimeSource}) || !rec.onRoot {
		t.Errorf("events %v, want MissingTrustedTimeSource on the root", ids)
	}
	if v, ok := srv.MatterRead(tsdef.AttrTrustedTimeSource); !ok || v != nil {
		t.Errorf("TrustedTimeSource %v, want null", v)
	}
}
