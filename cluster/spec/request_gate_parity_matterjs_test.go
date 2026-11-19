// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// collectorRequest is the record of
// packages/node/test/behavior/state/validation/ValueValidatorTest.ts:51-80 at
// b25ed960 ("with an error collector"): Num int8 "max 5", Mode enum8 of
// A and B whose conformance names the feature F, Modes a list "max 1" of
// that enum.
type collectorRequest struct {
	Num   int8
	Mode  uint8
	Modes []uint8
}

// collectorCluster states the record as the fields of a request command
// (0), as the request gate judges them; the feature F is not selected.
func collectorCluster() *spec.Cluster {
	ab := &spec.Enum{Name: "AB", Bits: 8, Values: []spec.EnumValue{{Value: 0, Name: "A"}, {Value: 1, Name: "B"}}}
	return &spec.Cluster{
		ID: 0xFFF1_FC0D, Name: "Test", Revision: 1,
		Features: []spec.Feature{{Name: "F", Title: "Feature", Bit: 0, Conformance: op(spec.ConfOptional)}},
		Commands: []spec.Command{{
			ID: 0, Name: "Request", Direction: spec.Request, Response: "status", Conformance: op(spec.ConfMandatory),
			Access: spec.Access{Write: spec.PrivilegeOperate},
			Fields: []spec.Field{
				{ID: 0, Name: "Num", Type: spec.Type{Kind: spec.KindInt, Bits: 8}, Conformance: op(spec.ConfMandatory), Constraint: spec.Constraint{Text: "max 5", Max: &spec.Bound{Int: 5}}},
				{ID: 1, Name: "Mode", Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: ab}, Conformance: spec.Conformance{Text: "F", Op: spec.ConfName, Name: "F"}},
				{
					ID: 2, Name: "Modes", Type: spec.Type{Kind: spec.KindList, Entry: &spec.Field{Name: "entry", Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: ab}}},
					Conformance: op(spec.ConfMandatory), Constraint: spec.Constraint{Text: "max 1", Max: &spec.Bound{Int: 1}},
				},
			},
		}},
	}
}

// errorClass is the matter.js error class a Go request error stands for.
func errorClass(err error) string {
	switch {
	case errors.Is(err, spec.ErrUnknownEnumValue):
		return "UnknownEnumValueError"
	case errors.Is(err, spec.ErrEnumValueConformance):
		return "EnumValueConformanceError"
	case errors.Is(err, spec.ErrConformance):
		return "ConformanceError"
	case spec.StatusOf(err) == im.StatusConstraintError:
		return "ConstraintError"
	default:
		return err.Error()
	}
}

// TestValueValidatorErrorCollectorParityMatterJS ports
// packages/node/test/behavior/state/validation/ValueValidatorTest.ts:51-99 at
// b25ed960 (describe "with an error collector"): the request gate's
// collector records the errors of every field, a field's conformance
// error and its value's alike
// (packages/node/src/behavior/state/validation/location.ts:64-80,
// conformance.ts:44-56 at b25ed960), while the
// public check stops at the first, as the validator throws it without a
// collector.
func TestValueValidatorErrorCollectorParityMatterJS(t *testing.T) {
	t.Parallel()
	inst := mustInstance(t, collectorCluster(), 0)
	record := collectorRequest{Num: 9, Mode: 3, Modes: []uint8{3, 0}}
	t.Run("with an error collector", func(t *testing.T) {
		t.Parallel()
		t.Run("records the errors of every member instead of stopping at the first", func(t *testing.T) { // :82-94
			t.Parallel()
			errs := spec.RequestErrorsForTests(inst, 0, record)
			got := make([]string, 0, len(errs))
			for _, err := range errs {
				got = append(got, errorClass(err))
			}
			want := []string{"ConstraintError", "ConformanceError", "UnknownEnumValueError", "ConstraintError", "UnknownEnumValueError"}
			if !slices.Equal(got, want) {
				t.Errorf("errors %q, want %q", got, want)
			}
		})
		t.Run("throws the first error without a collector", func(t *testing.T) { // :96-98
			t.Parallel()
			if got := errorClass(inst.CheckRequest(0, record, nil)); got != "ConstraintError" {
				t.Errorf("first error %s, want ConstraintError", got)
			}
		})
	})
}

// gateRequest is a request of the gate cluster: Flags a map8 of one bit,
// Mode an enum8 of A and B, Gated an enum8 whose member B the unselected
// feature F gates, Free an enum8 without members.
type gateRequest struct {
	Flags uint8
	Mode  uint8
	Gated uint8
	Free  uint8
}

// gateCluster serves the gate request as command 0 without a refusal and
// as command 1 with INVALID_COMMAND.
func gateCluster() *spec.Cluster {
	ab := &spec.Enum{Name: "AB", Bits: 8, Values: []spec.EnumValue{{Value: 0, Name: "A"}, {Value: 1, Name: "B"}}}
	gated := &spec.Enum{Name: "Gated", Bits: 8, Values: []spec.EnumValue{{Value: 0, Name: "A"}, {Value: 1, Name: "B", Conformance: spec.Conformance{Text: "F", Op: spec.ConfName, Name: "F"}}}}
	free := &spec.Enum{Name: "Free", Bits: 8}
	flags := &spec.Bitmap{Name: "Flags", Bits: 8, Members: []spec.BitmapMember{{Name: "One", Bit: 0, Width: 1}}}
	fields := []spec.Field{
		{ID: 0, Name: "Flags", Type: spec.Type{Kind: spec.KindBitmap, Bits: 8, Bitmap: flags}, Conformance: op(spec.ConfMandatory)},
		{ID: 1, Name: "Mode", Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: ab}, Conformance: op(spec.ConfMandatory)},
		{ID: 2, Name: "Gated", Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: gated}, Conformance: op(spec.ConfMandatory)},
		{ID: 3, Name: "Free", Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: free}, Conformance: op(spec.ConfMandatory)},
	}
	return &spec.Cluster{
		ID: 0xFFF1_FC0E, Name: "Gate", Revision: 1,
		Features: []spec.Feature{{Name: "F", Title: "Feature", Bit: 0, Conformance: op(spec.ConfOptional)}},
		Commands: []spec.Command{
			{ID: 0, Name: "Plain", Direction: spec.Request, Response: "status", Conformance: op(spec.ConfMandatory), Access: spec.Access{Write: spec.PrivilegeOperate}, Fields: fields},
			{
				ID: 1, Name: "Refused", Direction: spec.Request, Response: "status", Conformance: op(spec.ConfMandatory), Access: spec.Access{Write: spec.PrivilegeOperate}, Fields: fields,
				Check: spec.RequestCheck{Refusal: im.StatusInvalidCommand},
			},
		},
	}
}

// TestRequestGateParityMatterJS holds the request gate to matter.js's
// decision over the collected errors (ProtocolService.ts:671-704 at
// b25ed960): a command with its own refusal answers with it; else a
// value an enum does not define, wherever it sits among the errors, is
// CONSTRAINT_ERROR and the handler does not run; else the errors are
// logged and the handler runs. An enum without members accepts every
// value (packages/node/src/behavior/state/validation/ValueValidator.ts:171-172),
// and a request without fields is judged as {} (ProtocolService.ts:624-625).
func TestRequestGateParityMatterJS(t *testing.T) {
	t.Parallel()
	valid := gateRequest{Flags: 1, Mode: 1, Gated: 0, Free: 0}
	for _, c := range []struct {
		name   string
		cmd    uint32
		req    any
		status im.StatusCode
	}{
		{"a valid request runs", 0, valid, im.StatusSuccess},
		{"an undefined enum value is CONSTRAINT_ERROR", 0, gateRequest{Flags: 1, Mode: 2}, im.StatusConstraintError},
		{"an undefined enum value after an error that only gets a warning is CONSTRAINT_ERROR", 0, gateRequest{Flags: 0x81, Mode: 2}, im.StatusConstraintError},
		{"a reserved bit alone only gets a warning", 0, gateRequest{Flags: 0x81, Mode: 1}, im.StatusSuccess},
		{"a member its conformance disallows only gets a warning", 0, gateRequest{Mode: 1, Gated: 1}, im.StatusSuccess},
		{"an enum without members accepts every value", 0, gateRequest{Mode: 1, Free: 7}, im.StatusSuccess},
		{"a request without fields only gets a warning", 0, nil, im.StatusSuccess},
		{"a refusal answers an undefined enum value", 1, gateRequest{Mode: 2}, im.StatusInvalidCommand},
		{"a refusal answers an error that only gets a warning", 1, gateRequest{Flags: 0x81, Mode: 1}, im.StatusInvalidCommand},
		{"a refusal answers a request without fields", 1, nil, im.StatusInvalidCommand},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ran := false
			handler := func(context.Context, any) (any, error) { ran = true; return nil, nil }
			srv, err := spec.NewServer(gateCluster(), spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{0: handler, 1: handler}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = srv.MatterInvoke(context.Background(), c.cmd, c.req)
			if got := spec.StatusOf(err); got != c.status {
				t.Errorf("status %v (%v), want %v", got, err, c.status)
			}
			if ran != (c.status == im.StatusSuccess) {
				t.Errorf("handler ran %v, want %v", ran, c.status == im.StatusSuccess)
			}
		})
	}
}

// TestEnumWithoutMembersAcceptsEveryValue: a written value of an enum
// without members is held to its width only, as the request is
// (packages/node/src/behavior/state/validation/ValueValidator.ts:171-172
// at b25ed960, definesValues).
func TestEnumWithoutMembersAcceptsEveryValue(t *testing.T) {
	t.Parallel()
	def := &spec.Cluster{
		ID: 0xFFF1_FC0F, Name: "Free", Revision: 1,
		Attributes: []spec.Attribute{rw(0, "Free", spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: &spec.Enum{Name: "Free", Bits: 8}}, spec.Quality{}, spec.Constraint{})},
	}
	inst := mustInstance(t, def, 0)
	if _, err := inst.ValidateWrite(0, uint64(7), nil); err != nil {
		t.Errorf("Free 7: %v, want accepted", err)
	}
	if err := inst.CheckConformance(0, uint8(7), nil); err != nil {
		t.Errorf("Free 7 (conformance): %v, want accepted", err)
	}
	if _, err := inst.ValidateWrite(0, uint64(256), nil); spec.StatusOf(err) != im.StatusConstraintError {
		t.Errorf("Free 256: %v, want CONSTRAINT_ERROR", err)
	}
}
