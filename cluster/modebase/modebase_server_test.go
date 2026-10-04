// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package modebase_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// changer is a host device: it records the modes asked for and answers
// with status / text.
type changer struct {
	mu     sync.Mutex
	asked  []uint8
	status modebase.Status
	text   string
	err    error
}

func (c *changer) ChangeToMode(_ context.Context, mode uint8) (modebase.Status, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, mode)
	return c.status, c.text, c.err
}

func tags(values ...uint16) []modebase.ModeTag {
	out := make([]modebase.ModeTag, 0, len(values))
	for _, v := range values {
		out = append(out, modebase.ModeTag{Value: v})
	}
	return out
}

func mfg(code, value uint16) modebase.ModeTag { return modebase.ModeTag{MfgCode: &code, Value: value} }

var runModes = []modebase.ModeOption{
	{Label: "Idle", Mode: 0, Tags: tags(modebase.RvcRunTagIdle)},
	{Label: "Cleaning", Mode: 1, Tags: tags(modebase.RvcRunTagCleaning)},
	{Label: "Mapping", Mode: 2, Tags: tags(modebase.RvcRunTagMapping)},
}

var cleanModes = []modebase.ModeOption{
	{Label: "Vacuum", Mode: 1, Tags: tags(modebase.RvcCleanTagVacuum)},
	{Label: "Mop", Mode: 2, Tags: tags(modebase.RvcCleanTagMop)},
}

var laundryModes = []modebase.ModeOption{
	{Label: "Normal", Mode: 0, Tags: tags(modebase.LaundryTagNormal)},
	{Label: "Delicate", Mode: 1, Tags: tags(modebase.LaundryTagDelicate, modebase.TagLowEnergy)},
}

var dishModes = []modebase.ModeOption{
	{Label: "Normal", Mode: 0, Tags: tags(modebase.DishwasherTagNormal)},
	{Label: "Heavy", Mode: 1, Tags: tags(modebase.DishwasherTagHeavy, modebase.TagMax)},
}

func newRun(t *testing.T, c *changer) *modebase.Server {
	t.Helper()
	srv, err := modebase.NewRvcRunMode(modebase.Config{Changer: c, SupportedModes: runModes})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func change(t *testing.T, srv *modebase.Server, mode uint8) clusterwire.ChangeToModeResponse {
	t.Helper()
	resp, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, clusterwire.ChangeToModeRequest{NewMode: mode})
	if err != nil {
		t.Fatalf("ChangeToMode %d: %v", mode, err)
	}
	return resp.(clusterwire.ChangeToModeResponse)
}

func current(srv *modebase.Server) uint8 {
	v, _ := srv.MatterRead(modebase.AttrCurrentMode)
	return v.(uint8)
}

func TestConstructionRefusesWhatMatterJSRefuses(t *testing.T) {
	t.Parallel()
	c := &changer{}
	with := func(base []modebase.ModeOption, extra ...modebase.ModeOption) []modebase.ModeOption {
		return append(slices.Clone(base), extra...)
	}
	cases := []struct {
		name  string
		build func(modebase.Config) (*modebase.Server, error)
		cfg   modebase.Config
		want  error
	}{
		{"no changer", modebase.NewRvcRunMode, modebase.Config{SupportedModes: runModes}, modebase.ErrNoChanger},
		{"feature on LaundryWasherMode", modebase.NewLaundryWasherMode, modebase.Config{Changer: c, SupportedModes: laundryModes, Features: modebase.FeatureDirectModeChange}, modebase.ErrUnknownFeature},
		{"one mode", modebase.NewLaundryWasherMode, modebase.Config{Changer: c, SupportedModes: laundryModes[:1]}, modebase.ErrModeCount},
		{"duplicate label", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: "Idle", Mode: 9, Tags: tags(modebase.TagQuick)})}, modebase.ErrDuplicateLabel},
		{"duplicate mode", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: "X", Mode: 1, Tags: tags(modebase.TagQuick)})}, modebase.ErrDuplicateMode},
		{"long label", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: strings.Repeat("x", 65), Mode: 9, Tags: tags(modebase.TagQuick)})}, modebase.ErrLabel},
		{"no tag", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: "X", Mode: 9})}, modebase.ErrTagCount},
		{"nine tags", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: "X", Mode: 9, Tags: tags(0, 1, 2, 3, 4, 5, 6, 7, 8)})}, modebase.ErrTagCount},
		{"tag twice", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: "X", Mode: 9, Tags: tags(modebase.TagQuick, modebase.TagQuick)})}, modebase.ErrDuplicateTag},
		{"manufacturer tags only", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: "X", Mode: 9, Tags: []modebase.ModeTag{mfg(0xFFF1, 0x8000)}})}, modebase.ErrNoStandardTag},
		{"same tag set", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: "Again", Mode: 9, Tags: tags(modebase.RvcRunTagCleaning)})}, modebase.ErrDuplicateTagSet},
		{"no Idle", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: runModes[1:]}, modebase.ErrRequiredTag},
		{"no Cleaning", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: []modebase.ModeOption{runModes[0], runModes[2]}}, modebase.ErrRequiredTag},
		{"Idle and Cleaning together", modebase.NewRvcRunMode, modebase.Config{Changer: c, SupportedModes: with(runModes, modebase.ModeOption{Label: "Both", Mode: 9, Tags: tags(modebase.RvcRunTagIdle, modebase.RvcRunTagCleaning)})}, modebase.ErrExclusiveTags},
		{"no Vacuum or Mop", modebase.NewRvcCleanMode, modebase.Config{Changer: c, SupportedModes: []modebase.ModeOption{{Label: "A", Mode: 0, Tags: tags(modebase.RvcCleanTagDeepClean)}, {Label: "B", Mode: 1, Tags: tags(modebase.TagQuick)}}}, modebase.ErrRequiredTag},
		{"no Normal (laundry)", modebase.NewLaundryWasherMode, modebase.Config{Changer: c, SupportedModes: []modebase.ModeOption{laundryModes[1], {Label: "H", Mode: 2, Tags: tags(modebase.LaundryTagHeavy)}}}, modebase.ErrRequiredTag},
		{"no Normal (dishwasher)", modebase.NewDishwasherMode, modebase.Config{Changer: c, SupportedModes: []modebase.ModeOption{dishModes[1], {Label: "L", Mode: 2, Tags: tags(modebase.DishwasherTagLight)}}}, modebase.ErrRequiredTag},
		{"current mode not supported", modebase.NewDishwasherMode, modebase.Config{Changer: c, SupportedModes: dishModes, CurrentMode: 7}, modebase.ErrUnsupportedMode},
	}
	for _, tc := range cases {
		if _, err := tc.build(tc.cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	// The same tags in another order are the same set; a manufacturer tag
	// next to a standard one is fine.
	ok := with(runModes,
		modebase.ModeOption{Label: "Quiet A", Mode: 8, Tags: []modebase.ModeTag{{Value: modebase.TagQuiet}, mfg(0xFFF1, 0x8000)}},
		modebase.ModeOption{Label: "Quiet B", Mode: 9, Tags: []modebase.ModeTag{mfg(0xFFF1, 0x8000), {Value: modebase.TagQuiet}}})
	if _, err := modebase.NewRvcRunMode(modebase.Config{Changer: c, SupportedModes: ok}); !errors.Is(err, modebase.ErrDuplicateTagSet) {
		t.Errorf("reordered tag set: %v", err)
	}
	ok[len(ok)-1].Tags = []modebase.ModeTag{mfg(0xFFF2, 0x8000), {Value: modebase.TagQuiet}}
	if _, err := modebase.NewRvcRunMode(modebase.Config{Changer: c, SupportedModes: ok, Features: modebase.FeatureDirectModeChange}); err != nil {
		t.Errorf("distinct manufacturer tags: %v", err)
	}
}

func TestReadsAndLists(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		build func(modebase.Config) (*modebase.Server, error)
		modes []modebase.ModeOption
		id    uint32
	}{
		{modebase.NewLaundryWasherMode, laundryModes, modebase.ClusterIDLaundryWasherMode},
		{modebase.NewDishwasherMode, dishModes, modebase.ClusterIDDishwasherMode},
		{modebase.NewRvcRunMode, runModes, modebase.ClusterIDRvcRunMode},
		{modebase.NewRvcCleanMode, cleanModes, modebase.ClusterIDRvcCleanMode},
	} {
		srv, err := tc.build(modebase.Config{Changer: &changer{}, SupportedModes: tc.modes, CurrentMode: tc.modes[1].Mode})
		if err != nil {
			t.Fatal(err)
		}
		if srv.MatterClusterID() != tc.id || srv.Revision() == 0 || srv.MatterDataVersion() == 0 {
			t.Errorf("0x%04X: id / revision / version", tc.id)
		}
		if rev, _ := srv.MatterRead(cluster.AttrGlobalClusterRevision); rev != srv.Revision() {
			t.Errorf("0x%04X ClusterRevision %v", tc.id, rev)
		}
		if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(0) {
			t.Errorf("0x%04X FeatureMap %v", tc.id, fm)
		}
		if current(srv) != tc.modes[1].Mode {
			t.Errorf("0x%04X CurrentMode %d", tc.id, current(srv))
		}
		v, _ := srv.MatterRead(modebase.AttrSupportedModes)
		list := v.([]clusterwire.ModeOptionStruct)
		if len(list) != len(tc.modes) || list[0].Label != tc.modes[0].Label || list[0].ModeTags[0].Value != tc.modes[0].Tags[0].Value {
			t.Errorf("0x%04X SupportedModes %+v", tc.id, list)
		}
		if !slices.Equal(srv.MatterAttributes(), []uint32{0, 1}) || !slices.Equal(srv.MatterReportable(), []uint32{1}) ||
			!slices.Equal(srv.MatterAcceptedCommands(), []uint32{0}) || !slices.Equal(srv.MatterGeneratedCommands(), []uint32{1}) ||
			len(srv.MatterEvents()) != 0 {
			t.Errorf("0x%04X lists", tc.id)
		}
		for _, attr := range []uint32{0x0002, 0x0003} { // StartUpMode, OnMode: "X"
			if _, ok := srv.MatterRead(attr); ok {
				t.Errorf("0x%04X serves attribute 0x%04X", tc.id, attr)
			}
		}
	}
	srv, err := modebase.NewRvcCleanMode(modebase.Config{Changer: &changer{}, SupportedModes: cleanModes, CurrentMode: 1, Features: modebase.FeatureDirectModeChange})
	if err != nil {
		t.Fatal(err)
	}
	if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(1<<20) {
		t.Errorf("FeatureMap with DIRECTMODECH = %v", fm)
	}
}

// TestChangeToModeFollowsModeUtils walks ModeUtils.assertModeChange and
// the device's answer.
func TestChangeToModeFollowsModeUtils(t *testing.T) {
	t.Parallel()
	c := &changer{}
	srv := newRun(t, c)
	var notes [][]uint32
	srv.OnMatterAttributesChanged(func(ids []uint32) { notes = append(notes, ids) })

	// The current mode: Success, the device is not asked.
	if r := change(t, srv, 0); r != (clusterwire.ChangeToModeResponse{Status: 0}) || len(c.asked) != 0 {
		t.Errorf("current mode: %+v, asked %v", r, c.asked)
	}
	// An unsupported mode: UnsupportedMode with matter.js's text.
	if r := change(t, srv, 9); r != (clusterwire.ChangeToModeResponse{Status: 1, StatusText: "Unsupported mode: 9"}) || len(c.asked) != 0 {
		t.Errorf("unsupported mode: %+v", r)
	}
	// The device accepts: CurrentMode moves, a change is reported.
	version := srv.MatterDataVersion()
	if r := change(t, srv, 1); r.Status != 0 || current(srv) != 1 || srv.MatterDataVersion() == version {
		t.Errorf("accepted change: %+v, current %d", r, current(srv))
	}
	if len(notes) != 1 || !slices.Equal(notes[0], []uint32{modebase.AttrCurrentMode}) {
		t.Errorf("notifications %v", notes)
	}
	// The device refuses with a derivation status and text; nothing moves.
	c.status, c.text = modebase.StatusStuck, strings.Repeat("ü", 40)
	r := change(t, srv, 2)
	if r.Status != uint8(modebase.StatusStuck) || current(srv) != 1 || len(r.StatusText) > 64 || !strings.HasPrefix(r.StatusText, "ü") {
		t.Errorf("refused change: %+v, current %d", r, current(srv))
	}
	for _, st := range []modebase.Status{modebase.StatusGenericFailure, modebase.StatusInvalidInMode, 0x80, 0xBF} {
		c.status = st
		if r := change(t, srv, 2); r.Status != uint8(st) {
			t.Errorf("status 0x%02X answered %+v", uint8(st), r)
		}
	}
	// Reserved statuses from the device: FAILURE.
	var sce im.StatusCodeError
	for _, st := range []modebase.Status{modebase.StatusUnsupportedMode, modebase.StatusCleaningInProgress, 0x04} {
		c.status = st
		if _, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, clusterwire.ChangeToModeRequest{NewMode: 2}); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusFailure {
			t.Errorf("host status 0x%02X: %v", uint8(st), err)
		}
	}
	// A device error fails the invoke.
	c.status, c.err = modebase.StatusSuccess, errors.New("offline")
	if _, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, clusterwire.ChangeToModeRequest{NewMode: 2}); err == nil || current(srv) != 1 {
		t.Errorf("device error: %v", err)
	}
	// Malformed fields and other commands.
	if _, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, map[uint8]any{0: uint64(1)}); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("untyped fields: %v", err)
	}
	if _, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToModeResponse, nil); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedCommand {
		t.Errorf("ChangeToModeResponse invoked: %v", err)
	}
	// RvcCleanMode answers its own CleaningInProgress.
	cc := &changer{status: modebase.StatusCleaningInProgress}
	clean, err := modebase.NewRvcCleanMode(modebase.Config{Changer: cc, SupportedModes: cleanModes, CurrentMode: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r := change(t, clean, 2); r.Status != uint8(modebase.StatusCleaningInProgress) {
		t.Errorf("RvcCleanMode CleaningInProgress = %+v", r)
	}
}

func TestSetCurrentModeAndWrites(t *testing.T) {
	t.Parallel()
	srv := newRun(t, &changer{})
	var notes int
	srv.OnMatterAttributesChanged(func([]uint32) { notes++ })
	if err := srv.SetCurrentMode(7); !errors.Is(err, modebase.ErrUnsupportedMode) {
		t.Errorf("unsupported mode: %v", err)
	}
	if err := srv.SetCurrentMode(0); err != nil || notes != 0 {
		t.Errorf("same mode: %v, %d notes", err, notes)
	}
	if err := srv.SetCurrentMode(2); err != nil || notes != 1 || current(srv) != 2 {
		t.Errorf("out-of-band change: %v, %d notes", err, notes)
	}
	var sce im.StatusCodeError
	if err := srv.MatterWrite(context.Background(), modebase.AttrCurrentMode, uint64(1)); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedWrite {
		t.Errorf("CurrentMode write: %v", err)
	}
	if err := srv.MatterWrite(context.Background(), 0x0002, uint64(1)); !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedAttribute {
		t.Errorf("StartUpMode write: %v", err)
	}
	ext := &cluster.DataVersionTracker{}
	withExt, err := modebase.NewRvcRunMode(modebase.Config{Changer: &changer{}, SupportedModes: runModes, DataVersion: ext})
	if err != nil {
		t.Fatal(err)
	}
	before := ext.Current()
	if err := withExt.SetCurrentMode(1); err != nil || ext.Current() == before || withExt.MatterDataVersion() != ext.Current() {
		t.Errorf("host tracker not used: %v", err)
	}
}
