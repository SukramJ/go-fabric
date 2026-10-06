// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package filter_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/filter"
	"github.com/SukramJ/go-fabric/cluster/spec"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/im"
)

type host struct {
	resets   int
	resetErr error
	written  []*uint32
	writeErr error
}

func (h *host) ResetCondition(context.Context) error {
	h.resets++
	return h.resetErr
}

func (h *host) WriteLastChangedTime(_ context.Context, t *uint32) error {
	h.written = append(h.written, t)
	return h.writeErr
}

func statusOf(err error) im.StatusCode {
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return sce.MatterStatusCode()
	}
	return im.StatusSuccess
}

func u32(v uint32) *uint32 { return &v }

var products = []filter.ReplacementProduct{
	{ProductIdentifierType: hepa.ProductIdentifierTypeUpc, ProductIdentifierValue: "012345678905"},
	{ProductIdentifierType: hepa.ProductIdentifierTypeOem, ProductIdentifierValue: "HEPA-H13"},
}

func full(t *testing.T, h *host) *filter.Server {
	t.Helper()
	srv, err := filter.NewHepaFilterMonitoring(filter.Config{
		Features:              filter.FeatureCondition | filter.FeatureWarning | filter.FeatureReplacementProductList,
		Optional:              filter.OptionalInPlaceIndicator | filter.OptionalLastChangedTime | filter.OptionalResetCondition,
		DegradationDirection:  hepa.DegradationDirectionDown,
		ReplacementProducts:   products,
		Initial:               filter.State{Condition: 40, ChangeIndication: filter.ChangeIndicationWarning, InPlaceIndicator: true},
		Resetter:              h,
		LastChangedTimeWriter: h,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestNewRejects(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 21)
	cases := []struct {
		name string
		cfg  filter.Config
		want error
	}{
		{"reset without resetter", filter.Config{Optional: filter.OptionalResetCondition}, filter.ErrNoResetter},
		{"unknown feature", filter.Config{Features: 1 << 3}, spec.ErrUnknownFeature},
		{"direction", filter.Config{DegradationDirection: 2}, filter.ErrInvalidValue},
		{"too many products", filter.Config{ReplacementProducts: slices.Repeat(products[:1], 6)}, filter.ErrInvalidValue},
		{"identifier type", filter.Config{ReplacementProducts: []filter.ReplacementProduct{{ProductIdentifierType: 5}}}, filter.ErrInvalidValue},
		{"identifier length", filter.Config{ReplacementProducts: []filter.ReplacementProduct{{ProductIdentifierValue: long}}}, filter.ErrInvalidValue},
		{"condition", filter.Config{Initial: filter.State{Condition: 101}}, filter.ErrInvalidValue},
		{"warning without WRN", filter.Config{Initial: filter.State{ChangeIndication: filter.ChangeIndicationWarning}}, filter.ErrInvalidValue},
		{"null time", filter.Config{Initial: filter.State{LastChangedTime: u32(0xFFFFFFFF)}}, filter.ErrInvalidValue},
	}
	for _, tc := range cases {
		if _, err := filter.NewHepaFilterMonitoring(tc.cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestListsAndReads(t *testing.T) {
	t.Parallel()
	minimal, err := filter.NewActivatedCarbonFilterMonitoring(filter.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := minimal.MatterAttributes(); !slices.Equal(got, []uint32{hepa.AttrChangeIndication}) {
		t.Errorf("minimal attributes %v", got)
	}
	if minimal.MatterClusterID() != filter.ClusterIDActivatedCarbonFilterMonitoring || len(minimal.MatterAcceptedCommands()) != 0 {
		t.Error("minimal identity")
	}
	srv := full(t, &host{})
	if got := srv.MatterAttributes(); !slices.Equal(got, []uint32{0, 1, 2, 3, 4, 5}) {
		t.Errorf("attributes %v", got)
	}
	if got := srv.MatterReportable(); !slices.Equal(got, []uint32{0, 2, 3, 4}) {
		t.Errorf("reportable %v: DegradationDirection and the product list are fixed", got)
	}
	if got := srv.MatterAcceptedCommands(); !slices.Equal(got, []uint32{hepa.CmdResetCondition}) {
		t.Errorf("accepted %v", got)
	}
	if got := srv.MatterGeneratedCommands(); len(got) != 0 {
		t.Errorf("ResetCondition answers with a status: %v", got)
	}
	want := map[uint32]any{
		spec.AttrFeatureMap: uint32(7), spec.AttrClusterRevision: hepa.Revision,
		hepa.AttrCondition: uint8(40), hepa.AttrDegradationDirection: uint8(1), hepa.AttrChangeIndication: uint8(1),
		hepa.AttrInPlaceIndicator: true, hepa.AttrLastChangedTime: nil,
	}
	for id, w := range want {
		if got, ok := srv.MatterRead(id); !ok || got != w {
			t.Errorf("read 0x%04X = %v (%v), want %v", id, got, ok, w)
		}
	}
	list, ok := srv.MatterRead(hepa.AttrReplacementProductList)
	if l, isList := list.(spec.List[filter.ReplacementProduct]); !ok || !isList || !slices.Equal(l, products) {
		t.Errorf("ReplacementProductList %#v", list)
	}
	if _, ok := minimal.MatterRead(hepa.AttrCondition); ok {
		t.Error("Condition served without CON")
	}
	if err := srv.SetState(filter.State{LastChangedTime: u32(7)}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(hepa.AttrLastChangedTime); v != uint32(7) {
		t.Errorf("LastChangedTime %v", v)
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()
	h := &host{}
	var tracker cluster.DataVersionTracker
	srv := full(t, h)
	changes := 0
	srv.OnMatterAttributesChanged(func([]uint32) { changes++ })
	ctx := context.Background()
	before := srv.MatterDataVersion()
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(1000)); err != nil {
		t.Fatal(err)
	}
	if st := srv.State(); st.LastChangedTime == nil || *st.LastChangedTime != 1000 || srv.MatterDataVersion() == before || changes != 1 {
		t.Errorf("after write: %+v, version %d, %d changes", st, srv.MatterDataVersion(), changes)
	}
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, nil); err != nil || srv.State().LastChangedTime != nil {
		t.Errorf("null write: %v", err)
	}
	if len(h.written) != 2 {
		t.Errorf("host saw %d writes", len(h.written))
	}
	for _, c := range []struct {
		attr   uint32
		value  any
		status im.StatusCode
	}{
		{hepa.AttrLastChangedTime, uint64(0xFFFFFFFF), im.StatusConstraintError},
		{hepa.AttrLastChangedTime, "x", im.StatusConstraintError},
		{hepa.AttrCondition, uint8(1), im.StatusUnsupportedWrite},
		{0x30, uint8(1), im.StatusUnsupportedAttribute},
	} {
		if err := srv.MatterWrite(ctx, c.attr, c.value); statusOf(err) != c.status {
			t.Errorf("write 0x%04X = %v: %v", c.attr, c.value, err)
		}
	}
	h.writeErr = errors.New("flash")
	if err := srv.MatterWrite(ctx, hepa.AttrLastChangedTime, uint64(5)); !errors.Is(err, h.writeErr) || srv.State().LastChangedTime != nil {
		t.Errorf("a refused write: %v", err)
	}
	ext, err := filter.NewHepaFilterMonitoring(filter.Config{Optional: filter.OptionalLastChangedTime, DataVersion: &tracker})
	if err != nil {
		t.Fatal(err)
	}
	v := tracker.Current()
	if err := ext.MatterWrite(ctx, hepa.AttrLastChangedTime, uint32(9)); err != nil || tracker.Current() == v || ext.MatterDataVersion() != tracker.Current() {
		t.Errorf("host tracker: %v", err)
	}
}

func TestResetCondition(t *testing.T) {
	t.Parallel()
	h := &host{}
	srv := full(t, h)
	ctx := context.Background()
	if resp, err := srv.MatterInvoke(ctx, hepa.CmdResetCondition, hepa.ResetConditionRequest{}); err != nil || resp != nil {
		t.Fatalf("ResetCondition: %v %v", resp, err)
	}
	if st := srv.State(); st.Condition != 100 || st.ChangeIndication != filter.ChangeIndicationOk || h.resets != 1 {
		t.Errorf("after reset %+v", st)
	}
	h.resetErr = errors.New("busy")
	if _, err := srv.MatterInvoke(ctx, hepa.CmdResetCondition, nil); !errors.Is(err, h.resetErr) {
		t.Errorf("host failure: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, 1, nil); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("unknown command: %v", err)
	}
	up, err := filter.NewHepaFilterMonitoring(filter.Config{
		Features: filter.FeatureCondition, Optional: filter.OptionalResetCondition, Resetter: &host{},
		DegradationDirection: hepa.DegradationDirectionUp, Initial: filter.State{Condition: 80, ChangeIndication: filter.ChangeIndicationCritical},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := up.MatterInvoke(ctx, hepa.CmdResetCondition, nil); err != nil || up.State().Condition != 0 {
		t.Errorf("an upward-degrading condition resets to 0: %+v %v", up.State(), err)
	}
	noCon, err := filter.NewHepaFilterMonitoring(filter.Config{Optional: filter.OptionalResetCondition, Resetter: &host{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noCon.MatterInvoke(ctx, hepa.CmdResetCondition, nil); err != nil {
		t.Errorf("reset without CON: %v", err)
	}
	if _, err := noCon.MatterInvoke(ctx, hepa.CmdResetCondition, nil); err != nil {
		t.Errorf("a reset that changes nothing: %v", err)
	}
}

func TestSetState(t *testing.T) {
	t.Parallel()
	srv := full(t, &host{})
	var seen [][]uint32
	srv.OnMatterAttributesChanged(func(ids []uint32) { seen = append(seen, ids) })
	if err := srv.SetState(filter.State{Condition: 120}); !errors.Is(err, filter.ErrInvalidValue) {
		t.Errorf("Condition 120: %v", err)
	}
	if err := srv.SetState(filter.State{Condition: 10, ChangeIndication: filter.ChangeIndicationCritical, InPlaceIndicator: true}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || !slices.Equal(seen[0], []uint32{hepa.AttrCondition, hepa.AttrChangeIndication}) {
		t.Errorf("changes %v", seen)
	}
}
