// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// The rules of the TimeSynchronization features beyond chip's unit tests,
// read from TimeSynchronizationCluster.cpp at 6170af84: the four command
// handlers' statuses and events (:1138-1275), the list rules
// (:619-816), LocalTime (:874-1007), the fabric removal (:1020-1028), the
// command lists (:1066-1107) and the stored state (:333-353).

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/im"
)

const tzAll = tsdef.FeatureTimeZone | tsdef.FeatureNtpClient | tsdef.FeatureNtpServer | tsdef.FeatureTimeSyncClient

// epochUs is t in Matter epoch microseconds.
func epochUs(at time.Time) uint64 { return uint64(at.UnixMicro() - 946684800*1_000_000) } //nolint:gosec // after 2000

func name(s string) *string { return &s }

func TestTimeSyncCommandLists(t *testing.T) {
	t.Parallel()
	srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{})
	if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{tsdef.CmdSetUtcTime}) {
		t.Errorf("accepted %v without features", got)
	}
	if _, err := srv.MatterInvoke(context.Background(), tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{}); tsStatus(err) != im.StatusUnsupportedCommand {
		t.Errorf("SetTimeZone without TZ: %v, want UnsupportedCommand", err)
	}
	all, _, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tzAll})
	got := all.MatterAcceptedCommands()
	slices.Sort(got)
	if want := []uint32{tsdef.CmdSetUtcTime, tsdef.CmdSetTrustedTimeSource, tsdef.CmdSetTimeZone, tsdef.CmdSetDstOffset, tsdef.CmdSetDefaultNtp}; !slices.Equal(got, want) {
		t.Errorf("accepted %v, want %v", got, want)
	}
	if got := all.MatterGeneratedCommands(); !slices.Equal(got, []uint32{tsdef.CmdSetTimeZoneResponse}) {
		t.Errorf("generated %v", got)
	}
	ev := all.MatterEvents()
	slices.Sort(ev)
	if want := []uint32{0, 1, 2, 3, 4}; !slices.Equal(ev, want) {
		t.Errorf("events %v, want %v", ev, want)
	}
}

func TestTimeSyncSetTimeZoneRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := epochUs(tsNow.Add(time.Hour))
	for label, tc := range map[string]struct {
		list []tsdef.TimeZoneStruct
		want im.StatusCode
	}{
		"three entries":         {[]tsdef.TimeZoneStruct{{}, {ValidAt: at}, {ValidAt: at + 1}}, im.StatusResourceExhausted},
		"offset below":          {[]tsdef.TimeZoneStruct{{Offset: -43201}}, im.StatusConstraintError},
		"offset above":          {[]tsdef.TimeZoneStruct{{Offset: 50401}}, im.StatusConstraintError},
		"first valid later":     {[]tsdef.TimeZoneStruct{{ValidAt: at}}, im.StatusConstraintError},
		"second valid at 0":     {[]tsdef.TimeZoneStruct{{}, {}}, im.StatusConstraintError},
		"name of 65 bytes":      {[]tsdef.TimeZoneStruct{{Name: name(strings.Repeat("x", 65))}}, im.StatusConstraintError},
		"one entry":             {[]tsdef.TimeZoneStruct{{Offset: 3600, Name: name("Europe/Berlin")}}, im.StatusSuccess},
		"two entries":           {[]tsdef.TimeZoneStruct{{Offset: 3600}, {Offset: 7200, ValidAt: at}}, im.StatusSuccess},
		"bounds and name of 64": {[]tsdef.TimeZoneStruct{{Offset: -43200, Name: name(strings.Repeat("x", 64))}, {Offset: 50400, ValidAt: at}}, im.StatusSuccess},
	} {
		srv, rec, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone})
		resp, err := srv.MatterInvoke(ctx, tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{TimeZone: tc.list})
		if got := tsStatus(err); got != tc.want {
			t.Errorf("%s: %v, want %v", label, err, tc.want)
			continue
		}
		tz, _ := srv.MatterRead(tsdef.AttrTimeZone)
		list := tz.(spec.List[tsdef.TimeZoneStruct])
		if tc.want != im.StatusSuccess {
			if len(list) != 1 || list[0].Offset != 0 {
				t.Errorf("%s: a refused list changed TimeZone to %+v", label, list)
			}
			continue
		}
		if len(list) != len(tc.list) || list[0].Offset != tc.list[0].Offset {
			t.Errorf("%s: TimeZone %+v", label, list)
		}
		if r, ok := resp.(tsdef.SetTimeZoneResponse); !ok || !r.DstOffsetRequired {
			t.Errorf("%s: response %+v, want DSTOffsetRequired", label, resp)
		}
		// The DST list is cleared for the new zone (:1203-1208).
		if ids, _ := rec.take(); !slices.Contains(ids, tsdef.EventDstTableEmpty) {
			t.Errorf("%s: events %v, want DSTTableEmpty", label, ids)
		}
	}
}

// A changed active offset emits TimeZoneStatus (:661-723, :1180-1184); an
// empty list restores the default entry.
func TestTimeSyncTimeZoneStatusAndDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, rec, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone})
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{Offset: 3600, Name: name("A")}}}); err != nil {
		t.Fatal(err)
	}
	ids, data := rec.take()
	i := slices.Index(ids, tsdef.EventTimeZoneStatus)
	if i < 0 || data[i].(tsdef.TimeZoneStatusEvent).Offset != 3600 || *data[i].(tsdef.TimeZoneStatusEvent).Name != "A" {
		t.Fatalf("events %v %v, want TimeZoneStatus 3600 A", ids, data)
	}
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{Offset: 3600, Name: name("A")}}}); err != nil {
		t.Fatal(err)
	}
	if ids, _ := rec.take(); slices.Contains(ids, tsdef.EventTimeZoneStatus) {
		t.Errorf("the same zone emitted TimeZoneStatus: %v", ids)
	}
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{}); err != nil {
		t.Fatal(err)
	}
	if tz, _ := srv.MatterRead(tsdef.AttrTimeZone); len(tz.(spec.List[tsdef.TimeZoneStruct])) != 1 || tz.(spec.List[tsdef.TimeZoneStruct])[0].Offset != 0 {
		t.Errorf("TimeZone %v after an empty list, want the default entry", tz)
	}
}

// With a database that knows the zone, SetTimeZone takes its DST offsets
// and answers DSTOffsetRequired false with DSTStatus(true) (:1189-1200).
func TestTimeSyncTimeZoneDatabase(t *testing.T) {
	t.Parallel()
	full := tsdef.TimeZoneDatabaseFull
	offsets := []tsdef.DSTOffsetStruct{{Offset: 3600, ValidStarting: 0, ValidUntil: spec.NullOf[uint64]()}}
	srv, rec, _ := tsServer(t, core.TimeSynchronizationConfig{
		Features: tsdef.FeatureTimeZone, TimeZoneDatabase: &full,
		DSTOffsetsFor: func(n string) ([]tsdef.DSTOffsetStruct, bool) { return offsets, n == "Europe/Berlin" },
	})
	resp, err := srv.MatterInvoke(context.Background(), tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{Offset: 3600, Name: name("Europe/Berlin")}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.(tsdef.SetTimeZoneResponse).DstOffsetRequired {
		t.Error("DSTOffsetRequired with a database that knows the zone")
	}
	ids, data := rec.take()
	if i := slices.Index(ids, tsdef.EventDstStatus); i < 0 || !data[i].(tsdef.DstStatusEvent).DstOffsetActive {
		t.Errorf("events %v, want DSTStatus(true)", ids)
	}
	if lt, _ := srv.MatterRead(tsdef.AttrLocalTime); lt != epochUs(tsNow)+7200*1_000_000 {
		t.Errorf("LocalTime %v, want UTC + 2 h", lt)
	}
}

func TestTimeSyncSetDSTOffsetRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := epochUs(tsNow)
	h := uint64(time.Hour / time.Microsecond)
	for label, tc := range map[string]struct {
		list []tsdef.DSTOffsetStruct
		want im.StatusCode
	}{
		"three":                 {[]tsdef.DSTOffsetStruct{{ValidUntil: spec.ValueOf(h)}, {ValidStarting: h, ValidUntil: spec.ValueOf(2 * h)}, {ValidStarting: 2 * h, ValidUntil: spec.NullOf[uint64]()}}, im.StatusResourceExhausted},
		"ends before it starts": {[]tsdef.DSTOffsetStruct{{ValidStarting: 2 * h, ValidUntil: spec.ValueOf(h)}}, im.StatusConstraintError},
		"overlap":               {[]tsdef.DSTOffsetStruct{{ValidUntil: spec.ValueOf(2 * h)}, {ValidStarting: h, ValidUntil: spec.NullOf[uint64]()}}, im.StatusConstraintError},
		"open-ended first":      {[]tsdef.DSTOffsetStruct{{ValidUntil: spec.NullOf[uint64]()}, {ValidStarting: h, ValidUntil: spec.NullOf[uint64]()}}, im.StatusConstraintError},
		"valid":                 {[]tsdef.DSTOffsetStruct{{Offset: 3600, ValidUntil: spec.ValueOf(now + h)}, {ValidStarting: now + h, ValidUntil: spec.NullOf[uint64]()}}, im.StatusSuccess},
	} {
		srv, rec, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone})
		_, err := srv.MatterInvoke(ctx, tsdef.CmdSetDstOffset, tsdef.SetDstOffsetRequest{DstOffset: tc.list})
		if got := tsStatus(err); got != tc.want {
			t.Errorf("%s: %v, want %v", label, err, tc.want)
			continue
		}
		dst, _ := srv.MatterRead(tsdef.AttrDstOffset)
		if tc.want != im.StatusSuccess {
			if len(dst.(spec.List[tsdef.DSTOffsetStruct])) != 0 {
				t.Errorf("%s: a refused list was stored", label)
			}
			continue
		}
		// From no list (Invalid) to an active offset: DSTStatus(true).
		ids, data := rec.take()
		if i := slices.Index(ids, tsdef.EventDstStatus); i < 0 || !data[i].(tsdef.DstStatusEvent).DstOffsetActive {
			t.Errorf("%s: events %v, want DSTStatus(true)", label, ids)
		}
	}
}

// LocalTime adds the zone's offset and the DST offset in effect; the last
// DST offset expiring empties the list (DSTTableEmpty) and LocalTime turns
// null (:951-1007).
func TestTimeSyncLocalTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, rec, clk := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone})
	now := epochUs(tsNow)
	h := uint64(time.Hour / time.Microsecond)
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{Offset: 3600}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetDstOffset, tsdef.SetDstOffsetRequest{DstOffset: []tsdef.DSTOffsetStruct{{Offset: 3600, ValidUntil: spec.ValueOf(now + h)}}}); err != nil {
		t.Fatal(err)
	}
	_, _ = rec.take()
	if lt, _ := srv.MatterRead(tsdef.AttrLocalTime); lt != now+2*h {
		t.Fatalf("LocalTime %v, want UTC + 2 h", lt)
	}
	clk.Advance(2 * time.Hour)
	if lt, ok := srv.MatterRead(tsdef.AttrLocalTime); !ok || lt != nil {
		t.Errorf("LocalTime %v after the last DST offset ended, want null", lt)
	}
	if ids, _ := rec.take(); !slices.Contains(ids, tsdef.EventDstTableEmpty) {
		t.Errorf("events %v, want DSTTableEmpty", ids)
	}
	// A second zone becomes the active one once its time has come.
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{Offset: 3600}, {Offset: 7200, ValidAt: epochUs(clk.Now()) + h}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetDstOffset, tsdef.SetDstOffsetRequest{DstOffset: []tsdef.DSTOffsetStruct{{ValidUntil: spec.NullOf[uint64](), Offset: 0}}}); err != nil {
		t.Fatal(err)
	}
	_, _ = rec.take()
	clk.Advance(2 * time.Hour)
	if lt, _ := srv.MatterRead(tsdef.AttrLocalTime); lt != epochUs(clk.Now())+2*h {
		t.Errorf("LocalTime %v, want UTC + 2 h under the second zone", lt)
	}
	if ids, _ := rec.take(); !slices.Contains(ids, tsdef.EventTimeZoneStatus) {
		t.Errorf("events %v, want TimeZoneStatus", ids)
	}
	if tz, _ := srv.MatterRead(tsdef.AttrTimeZone); len(tz.(spec.List[tsdef.TimeZoneStruct])) != 1 {
		t.Errorf("TimeZone %v, want the passed zone dropped", tz)
	}
}

func TestTimeSyncSetDefaultNTPRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for label, tc := range map[string]struct {
		dns  bool
		ntp  spec.Nullable[string]
		want im.StatusCode
	}{
		"ipv6":              {false, spec.ValueOf("fd00::1"), im.StatusSuccess},
		"ipv4, no dns":      {false, spec.ValueOf("192.168.1.1"), im.StatusInvalidCommand},
		"domain, no dns":    {false, spec.ValueOf("pool.ntp.org"), im.StatusInvalidCommand},
		"domain, dns":       {true, spec.ValueOf("pool.ntp.org"), im.StatusSuccess},
		"no dot":            {true, spec.ValueOf("ntp"), im.StatusInvalidCommand},
		"too long":          {true, spec.ValueOf(strings.Repeat("a", 125) + ".org"), im.StatusConstraintError},
		"null":              {false, spec.NullOf[string](), im.StatusSuccess},
		"empty is accepted": {false, spec.ValueOf(""), im.StatusSuccess},
	} {
		srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureNtpClient, SupportsDNSResolve: tc.dns})
		_, err := srv.MatterInvoke(ctx, tsdef.CmdSetDefaultNtp, tsdef.SetDefaultNtpRequest{DefaultNtp: tc.ntp})
		if got := tsStatus(err); got != tc.want {
			t.Errorf("%s: %v, want %v", label, err, tc.want)
			continue
		}
		v, _ := srv.MatterRead(tsdef.AttrDefaultNtp)
		switch {
		case tc.want != im.StatusSuccess && v != nil:
			t.Errorf("%s: refused, DefaultNTP %v", label, v)
		case tc.want == im.StatusSuccess && tc.ntp.Null && v != nil:
			t.Errorf("%s: DefaultNTP %v, want null", label, v)
		case tc.want == im.StatusSuccess && !tc.ntp.Null && v != tc.ntp.Value:
			t.Errorf("%s: DefaultNTP %v", label, v)
		}
	}
	failing := &tsSettings{fail: true}
	srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureNtpClient, Store: failing})
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetDefaultNtp, tsdef.SetDefaultNtpRequest{DefaultNtp: spec.ValueOf("fd00::1")}); tsStatus(err) != im.StatusFailure {
		t.Errorf("a failed store: %v, want Failure", err)
	}
}

// A removed fabric's trusted time source is cleared with
// MissingTrustedTimeSource; another fabric's stays.
func TestTimeSyncFabricRemoved(t *testing.T) {
	t.Parallel()
	srv, rec, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeSyncClient})
	ctx := im.WithFabricFilter(context.Background(), false, 3)
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTrustedTimeSource, tsdef.SetTrustedTimeSourceRequest{TrustedTimeSource: spec.ValueOf(tsdef.FabricScopedTrustedTimeSourceStruct{NodeId: 5, Endpoint: 0})}); err != nil {
		t.Fatal(err)
	}
	srv.FabricRemoved(context.Background(), 1)
	if _, ok := srv.TrustedTimeSource(); !ok {
		t.Fatal("another fabric's removal cleared the source")
	}
	srv.FabricRemoved(context.Background(), 3)
	if _, ok := srv.TrustedTimeSource(); ok {
		t.Fatal("the source's fabric was removed and it stayed")
	}
	if ids, _ := rec.take(); !slices.Equal(ids, []uint32{tsdef.EventMissingTrustedTimeSource}) {
		t.Errorf("events %v", ids)
	}
}

// The state survives a restart on the same store (chip's Startup, :333-353),
// and NTPServerAvailable follows its setter.
func TestTimeSyncPersistsAcrossRestart(t *testing.T) {
	t.Parallel()
	st := &tsSettings{}
	cfg := core.TimeSynchronizationConfig{Features: tzAll, Store: st, NTPServerAvailable: true}
	srv, _, _ := tsServer(t, cfg)
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	for cmd, fields := range map[uint32]any{
		tsdef.CmdSetTrustedTimeSource: tsdef.SetTrustedTimeSourceRequest{TrustedTimeSource: spec.ValueOf(tsdef.FabricScopedTrustedTimeSourceStruct{NodeId: 9, Endpoint: 1})},
		tsdef.CmdSetDefaultNtp:        tsdef.SetDefaultNtpRequest{DefaultNtp: spec.ValueOf("fd00::7")},
	} {
		if _, err := srv.MatterInvoke(ctx, cmd, fields); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{Offset: -18000, Name: name("America/New_York")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetDstOffset, tsdef.SetDstOffsetRequest{DstOffset: []tsdef.DSTOffsetStruct{{Offset: 3600, ValidUntil: spec.NullOf[uint64]()}}}); err != nil {
		t.Fatal(err)
	}
	again, _, _ := tsServer(t, cfg)
	if tts, ok := again.TrustedTimeSource(); !ok || tts.NodeId != 9 || tts.Endpoint != 1 {
		t.Errorf("TrustedTimeSource %+v %v after the restart", tts, ok)
	}
	if ntp, ok := again.DefaultNTP(); !ok || ntp != "fd00::7" {
		t.Errorf("DefaultNTP %q %v after the restart", ntp, ok)
	}
	if tz, _ := again.MatterRead(tsdef.AttrTimeZone); tz.(spec.List[tsdef.TimeZoneStruct])[0].Offset != -18000 {
		t.Errorf("TimeZone %v after the restart", tz)
	}
	if dst, _ := again.MatterRead(tsdef.AttrDstOffset); len(dst.(spec.List[tsdef.DSTOffsetStruct])) != 1 {
		t.Errorf("DSTOffset %v after the restart", dst)
	}
	if v, _ := again.MatterRead(tsdef.AttrNtpServerAvailable); v != true {
		t.Errorf("NTPServerAvailable %v", v)
	}
	again.SetNTPServerAvailable(false)
	if v, _ := again.MatterRead(tsdef.AttrNtpServerAvailable); v != false {
		t.Errorf("NTPServerAvailable %v after the setter", v)
	}
}

func TestTimeSyncRefusesAnUnknownFeature(t *testing.T) {
	t.Parallel()
	if _, err := core.NewTimeSynchronizationWith(core.TimeSynchronizationConfig{Features: 1 << 7}); err == nil {
		t.Fatal("an unknown feature bit was accepted")
	}
}

// SetUTCTime answers chip's statuses (TimeSynchronizationCluster.cpp:
// 1110-1136): INVALID_COMMAND for a granularity or a time source outside
// its enum, TimeNotAccepted for NoTimeGranularity and for one coarser than
// the node's MillisecondsGranularity, SUCCESS otherwise; the clock stays.
func TestTimeSyncSetUTCTimeStatuses(t *testing.T) {
	t.Parallel()
	bad := tsdef.TimeSourceEnum(17)
	gnss := tsdef.TimeSourceGnss
	for label, tc := range map[string]struct {
		req         tsdef.SetUtcTimeRequest
		want        im.StatusCode
		notAccepted bool
	}{
		"milliseconds":        {tsdef.SetUtcTimeRequest{Granularity: tsdef.GranularityMillisecondsGranularity}, im.StatusSuccess, false},
		"microseconds, gnss":  {tsdef.SetUtcTimeRequest{Granularity: tsdef.GranularityMicrosecondsGranularity, TimeSource: &gnss}, im.StatusSuccess, false},
		"granularity 5":       {tsdef.SetUtcTimeRequest{Granularity: 5}, im.StatusInvalidCommand, false},
		"time source 17":      {tsdef.SetUtcTimeRequest{Granularity: tsdef.GranularityMillisecondsGranularity, TimeSource: &bad}, im.StatusInvalidCommand, false},
		"seconds is coarser":  {tsdef.SetUtcTimeRequest{Granularity: tsdef.GranularitySecondsGranularity}, im.StatusFailure, true},
		"minutes is coarser":  {tsdef.SetUtcTimeRequest{Granularity: tsdef.GranularityMinutesGranularity}, im.StatusFailure, true},
		"no time granularity": {tsdef.SetUtcTimeRequest{Granularity: tsdef.GranularityNoTimeGranularity}, im.StatusFailure, true},
	} {
		srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{})
		before, _ := srv.UTC()
		_, err := srv.MatterInvoke(context.Background(), tsdef.CmdSetUtcTime, tc.req)
		if got := tsStatus(err); got != tc.want {
			t.Errorf("%s: %v, want %v", label, err, tc.want)
			continue
		}
		var cs interface{ MatterClusterStatus() uint8 }
		if gotCS := errors.As(err, &cs) && cs.MatterClusterStatus() == uint8(tsdef.StatusCodeTimeNotAccepted); gotCS != tc.notAccepted {
			t.Errorf("%s: %v, TimeNotAccepted %v", label, err, tc.notAccepted)
		}
		if after, _ := srv.UTC(); !after.Equal(before) {
			t.Errorf("%s: the clock moved", label)
		}
	}
}
