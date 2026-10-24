// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gendiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/identify"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	tcdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// The unit cases of brief CT's hooks beside their ports: MarkChanged's
// refusals and report, the unimplemented mandatory commands, the response
// check, the stored defaults and the conformance operators the ported
// tables do not reach.

func TestMarkChangedRefusalsAndReport(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(onoffdef.Definition, spec.Options{}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	var seen []contract.Change
	srv.OnAttributeChange(func(c contract.Change) { seen = append(seen, c) })
	before := srv.MatterDataVersion()
	for _, c := range []struct {
		ids  []uint32
		want error
	}{{nil, spec.ErrNotReportable}, {[]uint32{onoffdef.AttrOnTime}, spec.ErrNotServed}} {
		if err := srv.MarkChanged(c.ids...); !errors.Is(err, c.want) {
			t.Errorf("MarkChanged(%v) = %v, want %v", c.ids, err, c.want)
		}
	}
	if err := spec.MarkChangedOf(srv, onoffdef.AttrOnOff, onoffdef.AttrOnOff); err != nil {
		t.Fatal(err)
	}
	if srv.MatterDataVersion() != before+1 || len(seen) != 1 || !slices.Equal(seen[0].Attributes, []uint32{onoffdef.AttrOnOff}) || seen[0].Origin != contract.OriginHostSet {
		t.Errorf("version %d (from %d), changes %+v", srv.MatterDataVersion(), before, seen)
	}
	for _, c := range []struct {
		def *spec.Cluster
		id  uint32
	}{{identify.Definition, identify.AttrIdentifyTime}, {gendiagdef.Definition, gendiagdef.AttrUpTime}} {
		s, err := spec.NewServer(c.def, spec.Options{}, spec.ServerConfig{Unimplemented: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.MarkChanged(c.id); !errors.Is(err, spec.ErrNotReportable) {
			t.Errorf("%s: %v, want ErrNotReportable", c.def.Name, err)
		}
	}
	if err := spec.MarkChangedOf(nil, 0); !errors.Is(err, spec.ErrNoMarkChanged) {
		t.Errorf("MarkChangedOf(nil) = %v", err)
	}
}

func TestUnimplementedMandatoryCommands(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(tcdef.Definition, spec.Options{Features: uint32(tcdef.FeatureTemperatureNumber)}, spec.ServerConfig{Unimplemented: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.UnimplementedMandatoryCommands(); !slices.Equal(got, []uint32{tcdef.CmdSetTemperature}) {
		t.Fatalf("unimplemented %v", got)
	}
	if err := srv.Handle(tcdef.CmdSetTemperature, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if got := srv.UnimplementedMandatoryCommands(); len(got) != 0 {
		t.Errorf("unimplemented %v after Handle", got)
	}
}

type ctReq struct{ Enable *bool }

type ctResp struct{ Result *uint8 }

type ctOther struct{ Other uint8 }

func TestResponseFieldPresence(t *testing.T) {
	t.Parallel()
	def := &spec.Cluster{
		ID: 0xFFF1FC09, Name: "Resp", Revision: 1,
		Commands: []spec.Command{
			{
				ID: 1, Name: "Req", Direction: spec.Request, Response: "Resp", Conformance: op(spec.ConfMandatory),
				Fields: []spec.Field{{ID: 0, Name: "Enable", Type: spec.Type{Kind: spec.KindBool}, Conformance: op(spec.ConfMandatory)}},
			},
			{
				ID: 1, Name: "Resp", Direction: spec.Response, Conformance: op(spec.ConfMandatory),
				Fields: []spec.Field{{ID: 0, Name: "Result", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{
					Text: "Req.Enable", Op: spec.ConfDot, Args: []spec.Conformance{name("Req"), name("Enable")},
				}}},
			},
		},
	}
	var resp any
	srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{
		1: func(context.Context, any) (any, error) { return resp, nil },
	}})
	if err != nil {
		t.Fatal(err)
	}
	v, yes := uint8(1), true
	for _, c := range []struct {
		name    string
		req     any
		resp    any
		failure bool
	}{
		{"missing", ctReq{Enable: &yes}, ctResp{}, true},
		{"present", ctReq{Enable: &yes}, &ctResp{Result: &v}, false},
		{"disallowed", ctReq{}, ctResp{Result: &v}, true},
		{"absent payload", ctReq{Enable: &yes}, nil, false},
		{"nil pointer", ctReq{Enable: &yes}, (*ctResp)(nil), false},
		{"not a struct", ctReq{Enable: &yes}, 3, false},
		{"another type", ctReq{Enable: &yes}, ctOther{}, false},
	} {
		resp = c.resp
		_, err := srv.MatterInvoke(context.Background(), 1, c.req)
		var se *spec.StatusError
		if got := errors.As(err, &se) && se.Status == im.StatusFailure; got != c.failure {
			t.Errorf("%s: %v, want failure %v", c.name, err, c.failure)
		}
	}
}

type ctNullable struct {
	Mandatory *spec.Nullable[uint8]
	Stated    *spec.Nullable[uint8]
	List      *[]uint8
	Plain     []uint8
}

func TestStoredDefaultsOfNullsAndLists(t *testing.T) {
	t.Parallel()
	s := &spec.Struct{Name: "N", Fields: []spec.Field{
		{ID: 0, Name: "Mandatory", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory), Quality: spec.Quality{Nullable: true}},
		{ID: 1, Name: "Stated", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory), Quality: spec.Quality{Nullable: true}, Default: spec.NullDefault{}},
		{ID: 2, Name: "List", Type: spec.Type{Kind: spec.KindList}, Conformance: op(spec.ConfMandatory), Default: spec.EmptyListDefault{}},
		{ID: 3, Name: "Plain", Type: spec.Type{Kind: spec.KindList}, Conformance: op(spec.ConfOptional), Default: spec.EmptyListDefault{}},
	}}
	def := &spec.Cluster{ID: 0xFFF1FC0A, Name: "N", Revision: 1, Attributes: []spec.Attribute{{ID: 0, Name: "V", Conformance: op(spec.ConfMandatory), Type: spec.Type{Kind: spec.KindStruct, Struct: s}}}}
	inst, err := spec.New(def, spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := inst.PatchDefaults(0, &ctNullable{})
	if err != nil {
		t.Fatal(err)
	}
	got := out.(*ctNullable)
	if got.Mandatory == nil || !got.Mandatory.Null || got.Stated == nil || !got.Stated.Null || got.List == nil || len(*got.List) != 0 || got.Plain != nil {
		t.Errorf("patched %+v", got)
	}
	if v, err := inst.PatchDefaults(0, nil); v != nil || err != nil {
		t.Errorf("nil: %v, %v", v, err)
	}
	if v, err := inst.PatchDefaults(9, 1); v != 1 || err != nil {
		t.Errorf("undefined attribute: %v, %v", v, err)
	}
}

func TestConformanceOperators(t *testing.T) {
	t.Parallel()
	cases := []struct {
		conf   string
		record map[string]any
		ok     bool
	}{
		{"Value == true", map[string]any{"value": true, "test": 1.0}, true},
		{"Value == false", map[string]any{"value": true, "test": 1.0}, false},
		{"Value != null", map[string]any{"value": true, "test": 1.0}, false},
		{"Value == 1.5", map[string]any{"value": true}, true},
		{"Value ^ Other", map[string]any{"value": true, "test": 1.0}, true},
		{"Value ^ Other", map[string]any{"value": true, "other": true, "test": 1.0}, false},
		{"Value & Other", map[string]any{"value": true, "other": true, "test": 1.0}, true},
		{"Value | Other", map[string]any{}, true},
		{"!Value", map[string]any{"test": 1.0}, true},
		{"!Value", map[string]any{"value": true, "test": 1.0}, false},
		{"Value >= 1", map[string]any{"value": true}, true},
	}
	for _, c := range cases {
		fields := []confField{
			{Name: "Value", Type: "bool"},
			{Name: "Other", Type: "bool"},
			{Name: "Test", Type: "int16", Conformance: c.conf},
		}
		def := confCluster(t, nil, fields, nil)
		err := judge(t, def, 0, c.record, nil)
		if (err == nil) != c.ok {
			t.Errorf("%q over %v: %v, want ok %v", c.conf, c.record, err, c.ok)
		}
	}
}
