// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestTimeSyncRefusedStoreKeepsTheValue (review II, R2-P6-store-1207): a
// store that refuses leaves DefaultNTP and DSTOffset as they were, reports
// no change and emits no DSTStatus; DefaultNTP answers FAILURE and
// SetDSTOffset CONSTRAINT_ERROR (chip's statuses,
// TimeSynchronizationCluster.cpp:1268-1271, :1225-1235 at 6170af84).
func TestTimeSyncRefusedStoreKeepsTheValue(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	srv, rec, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tzAll, Store: &tsSettings{fail: true}})
	var changed []uint32
	srv.OnAttributeChange(func(c contract.Change) { changed = append(changed, c.Attributes...) })
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetDefaultNtp, tsdef.SetDefaultNtpRequest{DefaultNtp: spec.ValueOf("fd00::1")}); tsStatus(err) != im.StatusFailure {
		t.Errorf("SetDefaultNTP: %v, want FAILURE", err)
	}
	if v, _ := srv.MatterRead(tsdef.AttrDefaultNtp); v != nil {
		t.Errorf("DefaultNTP %v served after the refused store, want null", v)
	}
	h := uint64(time.Hour / time.Microsecond)
	list := []tsdef.DSTOffsetStruct{{Offset: 3600, ValidUntil: spec.ValueOf(epochUs(tsNow) + h)}}
	if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetDstOffset, tsdef.SetDstOffsetRequest{DstOffset: list}); tsStatus(err) != im.StatusConstraintError {
		t.Errorf("SetDSTOffset: %v, want CONSTRAINT_ERROR", err)
	}
	if dst, _ := srv.MatterRead(tsdef.AttrDstOffset); len(dst.(spec.List[tsdef.DSTOffsetStruct])) != 0 {
		t.Errorf("DSTOffset %v served after the refused store", dst)
	}
	if slices.Contains(changed, tsdef.AttrDefaultNtp) || slices.Contains(changed, tsdef.AttrDstOffset) {
		t.Errorf("changes reported %v", changed)
	}
	if ids, _ := rec.take(); slices.Contains(ids, tsdef.EventDstStatus) {
		t.Errorf("events %v after the refused store", ids)
	}
}

// readFailingSettings is a settings store whose reads fail.
type readFailingSettings struct{ tsSettings }

func (*readFailingSettings) GetSetting(context.Context, string) (value string, ok bool, err error) {
	return "", false, errors.New("the store cannot read")
}

// TestTimeSyncLoadIsHeldToTheRules (review II, R2-P6-store-1267): stored
// TimeZone and DSTOffset lists are held to the setters' capacity and
// rules when they load, and a read error is logged, not taken for "none
// stored" in silence.
func TestTimeSyncLoadIsHeldToTheRules(t *testing.T) {
	t.Parallel()
	later := epochUs(tsNow) + uint64(time.Hour/time.Microsecond)
	st := &tsSettings{m: map[string]string{
		"timesync.time_zone":  fmt.Sprintf(`[{"Offset":3600,"ValidAt":0},{"Offset":7200,"ValidAt":%d},{"Offset":0,"ValidAt":%d}]`, later, later+1),
		"timesync.dst_offset": `[{"Offset":3600,"ValidStarting":200,"ValidUntil":{"Value":100}}]`,
	}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone, Store: st, Logger: logger})
	if tz, _ := srv.MatterRead(tsdef.AttrTimeZone); len(tz.(spec.List[tsdef.TimeZoneStruct])) != 2 {
		t.Errorf("TimeZone %v, want the first two stored entries", tz)
	}
	if dst, _ := srv.MatterRead(tsdef.AttrDstOffset); len(dst.(spec.List[tsdef.DSTOffsetStruct])) != 0 {
		t.Errorf("DSTOffset %v, want the invalid stored list dropped", dst)
	}
	if !strings.Contains(logs.String(), "TimeZone") || !strings.Contains(logs.String(), "DSTOffset") {
		t.Errorf("log %q, want the cut TimeZone and the dropped DSTOffset", logs.String())
	}

	logs.Reset()
	tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone, Store: &readFailingSettings{}, Logger: logger})
	if !strings.Contains(logs.String(), "the store cannot read") {
		t.Errorf("log %q, want the read error", logs.String())
	}
}
