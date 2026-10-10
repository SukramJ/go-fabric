// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/binding"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	"github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

type sink struct {
	writes []any
	err    error
}

func (s *sink) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	s.writes = append(s.writes, value)
	return s.err
}

type source map[uint32]any

func (s source) MatterAttribute(attrID uint32) (any, bool) {
	v, ok := s[attrID]
	return v, ok
}

type emitted struct {
	endpoint uint16
	cluster  uint32
	event    uint32
	data     any
	priority contract.EventPriority
}

type emitter struct{ events []emitted }

func (e *emitter) MatterEmitEvent(endpoint uint16, clusterID, event uint32, data any, priority contract.EventPriority) {
	e.events = append(e.events, emitted{endpoint, clusterID, event, data, priority})
}

var products = spec.List[hepa.ReplacementProductStruct]{
	{ProductIdentifierType: hepa.ProductIdentifierTypeUpc, ProductIdentifierValue: "012345678905"},
}

// hepaServer is a server over a definition with a list attribute, a
// writable nullable one and a status-only command.
func hepaServer(t *testing.T, cfg spec.ServerConfig) *spec.Server {
	t.Helper()
	if cfg.Initial == nil {
		cfg.Initial = map[uint32]any{
			hepa.AttrCondition:              uint8(40),
			hepa.AttrChangeIndication:       hepa.ChangeIndicationCritical, // a generated enum type
			hepa.AttrReplacementProductList: products,
		}
	}
	srv, err := spec.NewServer(hepa.Definition, spec.Options{
		Features:   uint32(hepa.FeatureCondition | hepa.FeatureReplacementProductList),
		Attributes: []uint32{hepa.AttrLastChangedTime},
		Commands:   []uint32{hepa.CmdResetCondition},
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestNewServerRefuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		def  *spec.Cluster
		opts spec.Options
		cfg  spec.ServerConfig
		want error
	}{
		{"no definition", nil, spec.Options{}, spec.ServerConfig{}, spec.ErrNoDefinition},
		{"unknown feature", hepa.Definition, spec.Options{Features: 1 << 3}, spec.ServerConfig{}, spec.ErrUnknownFeature},
		{"fabric-scoped", binding.Definition, spec.Options{}, spec.ServerConfig{}, spec.ErrFabricScoped},
		{"initial not served", hepa.Definition, spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{hepa.AttrCondition: uint8(1)}}, spec.ErrNotServed},
		{"initial out of constraint", hepa.Definition, spec.Options{Features: uint32(hepa.FeatureCondition)}, spec.ServerConfig{Initial: map[uint32]any{hepa.AttrCondition: uint8(101)}}, spec.ErrInvalidValue},
		{"initial enum without its feature", hepa.Definition, spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{hepa.AttrChangeIndication: hepa.ChangeIndicationWarning}}, spec.ErrInvalidValue},
	}
	for _, tc := range cases {
		if _, err := spec.NewServer(tc.def, tc.opts, tc.cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestServerRead(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{Source: source{hepa.AttrCondition: uint8(7)}})
	want := map[uint32]any{
		spec.AttrFeatureMap:       uint32(hepa.FeatureCondition | hepa.FeatureReplacementProductList),
		spec.AttrClusterRevision:  hepa.Revision,
		hepa.AttrCondition:        uint8(7), // the Source ahead of the stored 40
		hepa.AttrChangeIndication: uint8(hepa.ChangeIndicationCritical),
	}
	for id, w := range want {
		if got, ok := srv.MatterRead(id); !ok || got != w {
			t.Errorf("read 0x%04X = %#v (%v), want %#v", id, got, ok, w)
		}
	}
	if v, ok := srv.Value(hepa.AttrCondition); !ok || v != uint8(40) {
		t.Errorf("stored Condition %#v", v)
	}
	if l, ok := srv.MatterRead(hepa.AttrReplacementProductList); !ok || !slices.Equal(l.(spec.List[hepa.ReplacementProductStruct]), products) {
		t.Errorf("ReplacementProductList %#v", l)
	}
	if v, ok := srv.MatterRead(hepa.AttrLastChangedTime); ok {
		t.Errorf("a served attribute with no value is absent, got %#v", v)
	}
	if v, ok := srv.MatterRead(hepa.AttrInPlaceIndicator); ok {
		t.Errorf("an attribute not served is absent, got %#v", v)
	}
	if !slices.Equal(srv.MatterAttributes(), []uint32{0, 1, 2, 4, 5}) {
		t.Errorf("attributes %v", srv.MatterAttributes())
	}
}

func TestServerWrite(t *testing.T) {
	t.Parallel()
	sk := &sink{}
	srv := hepaServer(t, spec.ServerConfig{Sink: sk})
	var seen [][]uint32
	srv.OnMatterAttributesChanged(func(ids []uint32) { seen = append(seen, ids) })
	ctx := context.Background()

	v0 := srv.MatterDataVersion()
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(1000)); err != nil {
		t.Fatal(err)
	}
	v1 := srv.MatterDataVersion()
	if got, _ := srv.MatterRead(hepa.AttrLastChangedTime); got != uint32(1000) || v1 == v0 {
		t.Errorf("after write: %#v, version %d → %d", got, v0, v1)
	}
	if len(seen) != 1 || !slices.Equal(seen[0], []uint32{hepa.AttrLastChangedTime}) {
		t.Errorf("notified %v", seen)
	}
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint32(1000)); err != nil || srv.MatterDataVersion() != v1 || len(seen) != 1 {
		t.Errorf("an equal write changes nothing: %v, %d notifications", err, len(seen))
	}
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok := srv.MatterRead(hepa.AttrLastChangedTime); !ok || got != nil {
		t.Errorf("null write: %#v %v", got, ok)
	}
	if !slices.Equal(sk.writes, []any{uint32(1000), uint32(1000), nil}) {
		t.Errorf("sink saw %#v", sk.writes)
	}

	sk.err = errors.New("flash")
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(5)); !errors.Is(err, sk.err) {
		t.Errorf("sink refusal: %v", err)
	}
	if got, _ := srv.MatterRead(hepa.AttrLastChangedTime); got != nil {
		t.Errorf("a refused write is stored: %#v", got)
	}
	for _, c := range []struct {
		attr   uint32
		value  any
		status im.StatusCode
	}{
		{hepa.AttrLastChangedTime, uint64(0xFFFFFFFF), im.StatusConstraintError},
		{hepa.AttrLastChangedTime, "x", im.StatusConstraintError},
		{hepa.AttrCondition, uint8(1), im.StatusUnsupportedWrite},
		{hepa.AttrInPlaceIndicator, true, im.StatusUnsupportedAttribute},
	} {
		if err := srv.MatterWrite(ctx, c.attr, c.value); statusOf(err) != c.status {
			t.Errorf("write 0x%04X = %v: %v", c.attr, c.value, err)
		}
	}

	var tracker cluster.DataVersionTracker
	ext := hepaServer(t, spec.ServerConfig{DataVersion: &tracker})
	before := tracker.Current()
	if err := ext.MatterWrite(ctx, hepa.AttrLastChangedTime, uint32(9)); err != nil || tracker.Current() == before || ext.MatterDataVersion() != tracker.Current() {
		t.Errorf("host tracker: %v", err)
	}
}

func TestServerSet(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	var seen [][]uint32
	srv.OnMatterAttributesChanged(func(ids []uint32) { seen = append(seen, ids) })
	v0 := srv.MatterDataVersion()

	// The device changes a read-only attribute: Set checks the value, not
	// the access.
	if err := srv.Set(hepa.AttrCondition, 30); err != nil {
		t.Fatal(err)
	}
	if got, _ := srv.MatterRead(hepa.AttrCondition); got != uint8(30) || srv.MatterDataVersion() == v0 {
		t.Errorf("after Set: %#v", got)
	}
	v1 := srv.MatterDataVersion()
	if err := srv.Set(hepa.AttrCondition, uint8(30)); err != nil || srv.MatterDataVersion() != v1 {
		t.Errorf("an equal value changes nothing: %v", err)
	}
	if err := srv.SetAttributes(map[uint32]any{
		hepa.AttrChangeIndication: hepa.ChangeIndicationOk,
		hepa.AttrCondition:        uint8(100),
		hepa.AttrLastChangedTime:  uint32(3),
	}); err != nil {
		t.Fatal(err)
	}
	if want := [][]uint32{{hepa.AttrCondition}, {hepa.AttrCondition, hepa.AttrChangeIndication, hepa.AttrLastChangedTime}}; len(seen) != 2 || !slices.Equal(seen[0], want[0]) || !slices.Equal(seen[1], want[1]) {
		t.Errorf("notified %v", seen)
	}
	if srv.MatterDataVersion() != v1+1 {
		t.Errorf("one bump per SetAttributes: %d → %d", v1, srv.MatterDataVersion())
	}

	for _, c := range []struct {
		name  string
		attr  uint32
		value any
		want  error
	}{
		{"constraint", hepa.AttrCondition, uint8(101), spec.ErrInvalidValue},
		{"type", hepa.AttrCondition, "full", spec.ErrInvalidValue},
		{"not nullable", hepa.AttrCondition, nil, spec.ErrInvalidValue},
		{"enum without its feature", hepa.AttrChangeIndication, hepa.ChangeIndicationWarning, spec.ErrInvalidValue},
		{"not served", hepa.AttrInPlaceIndicator, true, spec.ErrNotServed},
		{"not defined", 0x30, true, spec.ErrNotServed},
	} {
		if err := srv.Set(c.attr, c.value); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// A refused value in a batch stores none of the batch.
	if err := srv.SetAttributes(map[uint32]any{hepa.AttrCondition: uint8(1), hepa.AttrChangeIndication: uint8(9)}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("batch: %v", err)
	}
	if got, _ := srv.MatterRead(hepa.AttrCondition); got != uint8(100) || len(seen) != 2 {
		t.Errorf("a refused batch changed Condition to %#v", got)
	}
}

// TestServerSetPeers holds a constraint naming sibling attributes
// ("minMeasuredValue to maxMeasuredValue") to their current values, and a
// signed value to the width of its type.
func TestServerSetPeers(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(temperaturemeasurement.Definition, spec.Options{}, spec.ServerConfig{
		Initial: map[uint32]any{
			temperaturemeasurement.AttrMinMeasuredValue: int16(-1000),
			temperaturemeasurement.AttrMaxMeasuredValue: int16(4000),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Set(temperaturemeasurement.AttrMeasuredValue, 2150); err != nil {
		t.Fatal(err)
	}
	if got, _ := srv.MatterRead(temperaturemeasurement.AttrMeasuredValue); got != int16(2150) {
		t.Errorf("MeasuredValue %#v", got)
	}
	if err := srv.Set(temperaturemeasurement.AttrMeasuredValue, int64(4001)); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("above maxMeasuredValue: %v", err)
	}
	if err := srv.Set(temperaturemeasurement.AttrMeasuredValue, nil); err != nil {
		t.Errorf("null: %v", err)
	}
}

func TestServerInvoke(t *testing.T) {
	t.Parallel()
	srv := hepaServer(t, spec.ServerConfig{})
	ctx := context.Background()
	if _, err := srv.MatterInvoke(ctx, hepa.CmdResetCondition, nil); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("accepted, no handler: %v", err)
	}
	var got any
	srv.Handle(hepa.CmdResetCondition, func(_ context.Context, fields any) (any, error) {
		got = fields
		return nil, srv.Set(hepa.AttrCondition, uint8(100))
	})
	if resp, err := srv.MatterInvoke(ctx, hepa.CmdResetCondition, hepa.ResetConditionRequest{}); err != nil || resp != nil {
		t.Fatalf("ResetCondition: %v %v", resp, err)
	}
	if v, _ := srv.MatterRead(hepa.AttrCondition); got != (hepa.ResetConditionRequest{}) || v != uint8(100) {
		t.Errorf("handler saw %#v; Condition %#v", got, v)
	}
	srv.Handle(0x01, func(context.Context, any) (any, error) { return "never", nil })
	if _, err := srv.MatterInvoke(ctx, 0x01, nil); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("not accepted, handler registered: %v", err)
	}

	minimal, err := spec.NewServer(hepa.Definition, spec.Options{}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	minimal.Handle(hepa.CmdResetCondition, func(context.Context, any) (any, error) { return nil, nil })
	if _, err := minimal.MatterInvoke(ctx, hepa.CmdResetCondition, nil); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("optional command not declared: %v", err)
	}
}

// TestServerEvents runs a definition with a command and events: an event
// carries the definition's priority, one outside EventList is refused.
func TestServerEvents(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(smokecoalarm.Definition, spec.Options{
		Features: uint32(smokecoalarm.FeatureSmokeAlarm),
		Commands: []uint32{smokecoalarm.CmdSelfTestRequest},
	}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Emit(1, smokecoalarm.EventLowBattery, smokecoalarm.LowBatteryEvent{}); err != nil {
		t.Errorf("without an emitter the event is dropped: %v", err)
	}
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	ev := smokecoalarm.SmokeAlarmEvent{AlarmSeverityLevel: smokecoalarm.AlarmStateCritical}
	if err := srv.Emit(3, smokecoalarm.EventSmokeAlarm, ev); err != nil {
		t.Fatal(err)
	}
	if err := srv.Emit(3, smokecoalarm.EventAllClear, smokecoalarm.AllClearEvent{}); err != nil {
		t.Fatal(err)
	}
	want := []emitted{
		{3, smokecoalarm.ClusterID, smokecoalarm.EventSmokeAlarm, ev, contract.EventPriorityCritical},
		{3, smokecoalarm.ClusterID, smokecoalarm.EventAllClear, smokecoalarm.AllClearEvent{}, contract.EventPriorityInfo},
	}
	if !slices.Equal(em.events, want) {
		t.Errorf("emitted %+v", em.events)
	}
	for _, id := range []uint32{smokecoalarm.EventCoAlarm, smokecoalarm.EventAlarmMuted, 0x20} {
		if err := srv.Emit(3, id, nil); !errors.Is(err, spec.ErrNotEmitted) {
			t.Errorf("event 0x%02X: %v", id, err)
		}
	}
	if len(em.events) != 2 {
		t.Errorf("a refused event reached the emitter: %+v", em.events)
	}

	srv.Handle(smokecoalarm.CmdSelfTestRequest, func(context.Context, any) (any, error) {
		return nil, srv.Set(smokecoalarm.AttrTestInProgress, true)
	})
	if _, err := srv.MatterInvoke(context.Background(), smokecoalarm.CmdSelfTestRequest, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(smokecoalarm.AttrTestInProgress); v != true {
		t.Errorf("TestInProgress %#v", v)
	}
	if !slices.Equal(srv.MatterEvents(), []uint32{0, 2, 3, 4, 5, 10}) {
		t.Errorf("EventList %v", srv.MatterEvents())
	}
}
