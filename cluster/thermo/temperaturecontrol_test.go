// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	tc "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/paritytest"
)

// setter is a host device: it records what SetTemperature asked for.
type setter struct {
	targets []int16
	levels  []uint8
	err     error
}

func (s *setter) SetTemperature(_ context.Context, target *int16, level *uint8) error {
	if s.err != nil {
		return s.err
	}
	if target != nil {
		s.targets = append(s.targets, *target)
	}
	if level != nil {
		s.levels = append(s.levels, *level)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

func numberConfig(s *setter) thermo.TemperatureControlConfig {
	return thermo.TemperatureControlConfig{
		Features: thermo.TemperatureControlFeatureNumber | thermo.TemperatureControlFeatureStep,
		Setter:   s, MinTemperature: -2000, MaxTemperature: 500, Step: 100, TemperatureSetpoint: -1800,
	}
}

func levelConfig(s *setter) thermo.TemperatureControlConfig {
	return thermo.TemperatureControlConfig{
		Features: thermo.TemperatureControlFeatureLevel, Setter: s,
		SupportedTemperatureLevels: []string{"Cold", "Colder", "Coldest"}, SelectedTemperatureLevel: 1,
	}
}

func newTC(t *testing.T, cfg thermo.TemperatureControlConfig) *thermo.TemperatureControlServer {
	t.Helper()
	srv, err := thermo.NewTemperatureControl(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func statusOf(err error) im.StatusCode {
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return sce.MatterStatusCode()
	}
	return 0xFF
}

func TestTemperatureControlConstruction(t *testing.T) {
	t.Parallel()
	s := &setter{}
	mod := func(base thermo.TemperatureControlConfig, f func(*thermo.TemperatureControlConfig)) thermo.TemperatureControlConfig {
		f(&base)
		return base
	}
	cases := []struct {
		name string
		cfg  thermo.TemperatureControlConfig
		want error
	}{
		{"no setter", mod(numberConfig(s), func(c *thermo.TemperatureControlConfig) { c.Setter = nil }), thermo.ErrNoTemperatureSetter},
		{"no feature", mod(numberConfig(s), func(c *thermo.TemperatureControlConfig) { c.Features = 0 }), nil},
		{"TN and TL", mod(numberConfig(s), func(c *thermo.TemperatureControlConfig) { c.Features |= thermo.TemperatureControlFeatureLevel }), nil},
		{"STEP without TN", mod(levelConfig(s), func(c *thermo.TemperatureControlConfig) { c.Features |= thermo.TemperatureControlFeatureStep }), nil},
		{"Min not below Max", mod(numberConfig(s), func(c *thermo.TemperatureControlConfig) { c.MinTemperature = c.MaxTemperature }), thermo.ErrTemperatureControlValue},
		{"Step zero", mod(numberConfig(s), func(c *thermo.TemperatureControlConfig) { c.Step = 0 }), thermo.ErrTemperatureControlValue},
		{"Step above range", mod(numberConfig(s), func(c *thermo.TemperatureControlConfig) { c.Step = 2501 }), thermo.ErrTemperatureControlValue},
		{"setpoint below Min", mod(numberConfig(s), func(c *thermo.TemperatureControlConfig) { c.TemperatureSetpoint = -2100 }), thermo.ErrTemperatureControlValue},
		{"setpoint off step", mod(numberConfig(s), func(c *thermo.TemperatureControlConfig) { c.TemperatureSetpoint = -1850 }), thermo.ErrTemperatureControlValue},
		{"33 levels", mod(levelConfig(s), func(c *thermo.TemperatureControlConfig) { c.SupportedTemperatureLevels = make([]string, 33) }), thermo.ErrTemperatureControlValue},
		{"long level", mod(levelConfig(s), func(c *thermo.TemperatureControlConfig) {
			c.SupportedTemperatureLevels = []string{strings.Repeat("x", 17)}
			c.SelectedTemperatureLevel = 0
		}), thermo.ErrTemperatureControlValue},
		{"selected level past the list", mod(levelConfig(s), func(c *thermo.TemperatureControlConfig) { c.SelectedTemperatureLevel = 3 }), thermo.ErrTemperatureControlValue},
	}
	for _, c := range cases {
		_, err := thermo.NewTemperatureControl(c.cfg)
		switch {
		case c.want == nil && err == nil:
			t.Errorf("%s: accepted", c.name) // a feature selection error has no sentinel here
		case c.want != nil && !errors.Is(err, c.want):
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	// Without STEP any setpoint within Min..Max is fine.
	cfg := numberConfig(s)
	cfg.Features, cfg.Step, cfg.TemperatureSetpoint = thermo.TemperatureControlFeatureNumber, 0, -1850
	newTC(t, cfg)
}

func TestTemperatureControlReadsAndLists(t *testing.T) {
	t.Parallel()
	num := newTC(t, numberConfig(&setter{}))
	if num.MatterClusterID() != thermo.ClusterIDTemperatureControl || num.Revision() != tc.Revision || num.MatterDataVersion() == 0 {
		t.Errorf("identity")
	}
	if !slices.Equal(num.MatterAttributes()[:4], []uint32{tc.AttrTemperatureSetpoint, tc.AttrMinTemperature, tc.AttrMaxTemperature, tc.AttrStep}) {
		t.Errorf("TN attributes %v", num.MatterAttributes())
	}
	for attr, want := range map[uint32]any{
		tc.AttrTemperatureSetpoint: int16(-1800), tc.AttrMinTemperature: int16(-2000), tc.AttrMaxTemperature: int16(500), tc.AttrStep: int16(100),
		cluster.AttrGlobalFeatureMap: uint32(tc.FeatureTemperatureNumber | tc.FeatureTemperatureStep),
	} {
		if v, ok := num.MatterRead(attr); !ok || v != want {
			t.Errorf("read 0x%04X = %v, want %v", attr, v, want)
		}
	}
	if _, ok := num.MatterRead(tc.AttrSelectedTemperatureLevel); ok {
		t.Error("TN server serves SelectedTemperatureLevel")
	}
	lvl := newTC(t, levelConfig(&setter{}))
	if v, _ := lvl.MatterRead(tc.AttrSelectedTemperatureLevel); v != uint8(1) {
		t.Errorf("SelectedTemperatureLevel %v", v)
	}
	if v, _ := lvl.MatterRead(tc.AttrSupportedTemperatureLevels); !slices.Equal(v.([]string), []string{"Cold", "Colder", "Coldest"}) {
		t.Errorf("SupportedTemperatureLevels %v", v)
	}
	if _, ok := lvl.MatterRead(tc.AttrTemperatureSetpoint); ok {
		t.Error("TL server serves TemperatureSetpoint")
	}
	if err := num.MatterWrite(context.Background(), tc.AttrTemperatureSetpoint, int64(0)); statusOf(err) != im.StatusUnsupportedWrite {
		t.Errorf("setpoint write: %v", err)
	}
}

func TestTemperatureControlSetTemperature(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := &setter{}
	num := newTC(t, numberConfig(s))
	var notes [][]uint32
	num.OnMatterAttributesChanged(func(ids []uint32) { notes = append(notes, ids) })
	for _, c := range []struct {
		name string
		req  any
		want im.StatusCode
	}{
		{"below Min", tc.SetTemperatureRequest{TargetTemperature: ptr[int16](-2100)}, im.StatusConstraintError},
		{"above Max", tc.SetTemperatureRequest{TargetTemperature: ptr[int16](600)}, im.StatusConstraintError},
		{"off step", tc.SetTemperatureRequest{TargetTemperature: ptr[int16](-1950)}, im.StatusConstraintError},
		{"no target", tc.SetTemperatureRequest{}, im.StatusInvalidCommand},
		{"untyped fields", map[uint8]any{0: int64(0)}, im.StatusInvalidCommand},
	} {
		if _, err := num.MatterInvoke(ctx, tc.CmdSetTemperature, c.req); statusOf(err) != c.want {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if len(s.targets) != 0 || len(notes) != 0 {
		t.Errorf("refused requests reached the device: %v", s.targets)
	}
	if _, err := num.MatterInvoke(ctx, tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr[int16](-1900)}); err != nil {
		t.Fatal(err)
	}
	if v, _ := num.MatterRead(tc.AttrTemperatureSetpoint); v != int16(-1900) || !slices.Equal(s.targets, []int16{-1900}) || len(notes) != 1 {
		t.Errorf("setpoint %v, asked %v, notes %v", v, s.targets, notes)
	}
	// Max is on a step from Min here; the bounds themselves are inside.
	for _, edge := range []int16{-2000, 500} {
		if _, err := num.MatterInvoke(ctx, tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr(edge)}); err != nil {
			t.Errorf("edge %d: %v", edge, err)
		}
	}
	// The TL field on a TN server is ignored, as chip's
	// HandleSetTemperature ignores it; the device never sees it.
	if _, err := num.MatterInvoke(ctx, tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr[int16](-1900), TargetTemperatureLevel: ptr[uint8](9)}); err != nil || len(s.levels) != 0 {
		t.Errorf("TN with a level field: %v, levels asked %v", err, s.levels)
	}
	// The device refusing the change now: INVALID_IN_STATE, nothing moves.
	s.err = fmt.Errorf("running: %w", thermo.ErrTemperatureRefused)
	if _, err := num.MatterInvoke(ctx, tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr[int16](0)}); statusOf(err) != im.StatusInvalidInState {
		t.Errorf("refused change: %v", err)
	}
	if v, _ := num.MatterRead(tc.AttrTemperatureSetpoint); v != int16(-1900) {
		t.Errorf("setpoint moved on a refused change: %v", v)
	}
	s.err = errors.New("offline")
	if _, err := num.MatterInvoke(ctx, tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr[int16](0)}); err == nil || statusOf(err) != 0xFF {
		t.Errorf("device error: %v", err)
	}
	if v, _ := num.MatterRead(tc.AttrTemperatureSetpoint); v != int16(-1900) {
		t.Errorf("setpoint moved on a device error: %v", v)
	}
	if _, err := num.MatterInvoke(ctx, 0x01, nil); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("unknown command: %v", err)
	}

	ls := &setter{}
	lvl := newTC(t, levelConfig(ls))
	for _, c := range []struct {
		name string
		req  tc.SetTemperatureRequest
		want im.StatusCode
	}{
		{"level past the list", tc.SetTemperatureRequest{TargetTemperatureLevel: ptr[uint8](3)}, im.StatusConstraintError},
		{"no level", tc.SetTemperatureRequest{}, im.StatusInvalidCommand},
	} {
		if _, err := lvl.MatterInvoke(ctx, tc.CmdSetTemperature, c.req); statusOf(err) != c.want {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// The TN field on a TL server is ignored.
	if _, err := lvl.MatterInvoke(ctx, tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr[int16](-30000), TargetTemperatureLevel: ptr[uint8](2)}); err != nil {
		t.Fatal(err)
	}
	if v, _ := lvl.MatterRead(tc.AttrSelectedTemperatureLevel); v != uint8(2) || !slices.Equal(ls.levels, []uint8{2}) || len(ls.targets) != 0 {
		t.Errorf("level %v, asked %v, targets %v", v, ls.levels, ls.targets)
	}
	ls.err = thermo.ErrTemperatureRefused
	if _, err := lvl.MatterInvoke(ctx, tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperatureLevel: ptr[uint8](0)}); statusOf(err) != im.StatusInvalidInState {
		t.Errorf("refused level change: %v", err)
	}
}

func TestTemperatureControlHostSetters(t *testing.T) {
	t.Parallel()
	ext := &cluster.DataVersionTracker{}
	cfg := numberConfig(&setter{})
	cfg.DataVersion = ext
	num := newTC(t, cfg)
	before := ext.Current()
	if err := num.SetTemperatureSetpoint(-1800); err != nil || ext.Current() != before {
		t.Errorf("same setpoint: %v", err)
	}
	if err := num.SetTemperatureSetpoint(-1700); err != nil || ext.Current() == before || num.MatterDataVersion() != ext.Current() {
		t.Errorf("new setpoint: %v", err)
	}
	if err := num.SetTemperatureSetpoint(-1750); !errors.Is(err, thermo.ErrTemperatureControlValue) {
		t.Errorf("off-step setpoint: %v", err)
	}
	if err := num.SetSelectedTemperatureLevel(0); !errors.Is(err, thermo.ErrTemperatureControlValue) {
		t.Errorf("level on TN: %v", err)
	}
	lvl := newTC(t, levelConfig(&setter{}))
	if err := lvl.SetSelectedTemperatureLevel(0); err != nil {
		t.Errorf("level 0: %v", err)
	}
	if err := lvl.SetSelectedTemperatureLevel(5); !errors.Is(err, thermo.ErrTemperatureControlValue) {
		t.Errorf("level 5: %v", err)
	}
	if err := lvl.SetTemperatureSetpoint(0); !errors.Is(err, thermo.ErrTemperatureControlValue) {
		t.Errorf("setpoint on TL: %v", err)
	}
}

// TestParityMatterJS_TemperatureControl holds the server to the snapshot:
// name, revision, feature bits, the attribute list per feature selection
// against each attribute's conformance, and the command.
func TestParityMatterJS_TemperatureControl(t *testing.T) {
	t.Parallel()
	js := paritytest.ClusterSnapshot(t, thermo.ClusterIDTemperatureControl)
	num := newTC(t, numberConfig(&setter{}))
	if js.Name != "TemperatureControl" || js.Revision != num.Revision() {
		t.Errorf("matter.js %s rev %d, server rev %d", js.Name, js.Revision, num.Revision())
	}
	bits := map[string]uint32{
		"TN": uint32(thermo.TemperatureControlFeatureNumber), "TL": uint32(thermo.TemperatureControlFeatureLevel),
		"STEP": uint32(thermo.TemperatureControlFeatureStep),
	}
	for _, f := range js.Features {
		if bits[f.Name] != 1<<f.Bit {
			t.Errorf("feature %s bit %d", f.Name, f.Bit)
		}
	}
	for _, sel := range []struct {
		srv      *thermo.TemperatureControlServer
		features map[string]bool
	}{
		{num, map[string]bool{"TN": true, "STEP": true}},
		{newTC(t, levelConfig(&setter{})), map[string]bool{"TL": true}},
	} {
		for _, a := range js.Attributes {
			if a.ID >= 0xFFF0 {
				continue
			}
			required, allowed := paritytest.Conformance(a.Conformance, sel.features)
			has := slices.Contains(sel.srv.MatterAttributes(), a.ID)
			if required && !has || has && !allowed {
				t.Errorf("%v: %s (%q) served=%v", sel.features, a.Name, a.Conformance, has)
			}
		}
		cmd := js.Command(t, "SetTemperature")
		if !slices.Equal(sel.srv.MatterAcceptedCommands(), []uint32{cmd.ID}) || len(sel.srv.MatterGeneratedCommands()) != 0 {
			t.Errorf("%v command lists", sel.features)
		}
	}
}
