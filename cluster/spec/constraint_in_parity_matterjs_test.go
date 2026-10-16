// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	lcfg "github.com/SukramJ/go-fabric/cluster/spec/localizationconfiguration"
	tfl "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	"github.com/SukramJ/go-fabric/im"
)

// countingSink counts the writes that reach it.
type countingSink struct{ writes int }

func (s *countingSink) MatterWriteAttribute(context.Context, uint32, any) error {
	s.writes++
	return nil
}

// TestConstraintInParityMatterJS: a controller write of an attribute whose
// constraint is "in <peer>" is refused with CONSTRAINT_ERROR unless the
// value is an entry of the peer attribute's current list — before the sink
// sees it — and an unresolved peer admits nothing. Mirrors matter.js
// packages/node/src/behavior/state/validation/constraint.ts:153-163 and
// packages/model/src/aspects/Constraint.ts:164-170. The cases are found
// from the definitions, not listed: every attribute carrying an "in"
// constraint in the two generated definitions that state one.
func TestConstraintInParityMatterJS(t *testing.T) {
	t.Parallel()
	type fixture struct {
		opts     spec.Options
		set      any // the peer list
		member   any // a written value the peer holds
		outsider any // a written value it does not
	}
	fixtures := map[*spec.Cluster]fixture{
		lcfg.Definition: {
			set:    []string{"en-US", "de-DE"},
			member: "de-DE", outsider: "fr-FR",
		},
		tfl.Definition: {
			opts:   spec.Options{Features: uint32(tfl.FeatureCalendarFormat)},
			set:    []tfl.CalendarTypeEnum{tfl.CalendarTypeGregorian, tfl.CalendarTypeBuddhist},
			member: uint64(tfl.CalendarTypeBuddhist), outsider: uint64(tfl.CalendarTypeChinese),
		},
	}
	found := 0
	for def, fx := range fixtures {
		for _, a := range def.Attributes {
			if a.Constraint.In == "" {
				continue
			}
			found++
			peer := def.AttributeByName(a.Constraint.In)
			if peer == nil {
				t.Fatalf("%s.%s: \"in\" names %q, which the definition lacks", def.Name, a.Name, a.Constraint.In)
			}
			t.Run(def.Name+"."+a.Name, func(t *testing.T) {
				t.Parallel()
				sink := &countingSink{}
				srv, err := spec.NewServer(def, fx.opts, spec.ServerConfig{
					Sink:    sink,
					Initial: map[uint32]any{peer.ID: fx.set},
				})
				if err != nil {
					t.Fatalf("NewServer: %v", err)
				}
				if err := srv.MatterWrite(context.Background(), a.ID, fx.member); err != nil {
					t.Fatalf("write of a member %v: %v", fx.member, err)
				}
				err = srv.MatterWrite(context.Background(), a.ID, fx.outsider)
				var se *spec.StatusError
				if !errors.As(err, &se) || se.Status != im.StatusConstraintError {
					t.Fatalf("write of %v outside %s: %v, want CONSTRAINT_ERROR", fx.outsider, a.Constraint.In, err)
				}
				if sink.writes != 1 {
					t.Errorf("sink saw %d writes, want 1 (the refused value must not reach it)", sink.writes)
				}
				// An unresolved peer is an empty set (Constraint.ts:166-168).
				inst, err := spec.New(def, fx.opts)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				if _, err := inst.ValidateWrite(a.ID, fx.member, nil); !errors.As(err, &se) || se.Status != im.StatusConstraintError {
					t.Errorf("write with no peers: %v, want CONSTRAINT_ERROR", err)
				}
			})
		}
	}
	if found != len(fixtures) {
		t.Errorf("found %d \"in\" constraints over %d definitions, want one each", found, len(fixtures))
	}
}

// TestConstraintInStartUpAndSetParityMatterJS: an "in" constraint holds
// the initial state — judged after every initial value is applied, so the
// id order of the two attributes does not matter — and a host Set, judged
// against the state with the change applied. Mirrors matter.js
// packages/node/src/behavior/internal/ServerBehaviorBacking.ts:60-63 (the
// whole state validated after the initializer) and
// packages/node/src/behavior/state/managed/values/StructManager.ts:273-287
// (an assigned property validated against its siblings).
func TestConstraintInStartUpAndSetParityMatterJS(t *testing.T) {
	t.Parallel()
	type startUp struct {
		def          *spec.Cluster
		opts         spec.Options
		active, list uint32
		set          any
		member       any
		outsider     any
	}
	cases := map[string]startUp{
		"LocalizationConfiguration": {
			def: lcfg.Definition, active: lcfg.AttrActiveLocale, list: lcfg.AttrSupportedLocales,
			set: []string{"en-US", "de-DE"}, member: "de-DE", outsider: "fr-FR",
		},
		"TimeFormatLocalization": {
			def: tfl.Definition, opts: spec.Options{Features: uint32(tfl.FeatureCalendarFormat)},
			active: tfl.AttrActiveCalendarType, list: tfl.AttrSupportedCalendarTypes,
			set:    []tfl.CalendarTypeEnum{tfl.CalendarTypeGregorian, tfl.CalendarTypeBuddhist},
			member: tfl.CalendarTypeBuddhist, outsider: tfl.CalendarTypeChinese,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if c.active >= c.list {
				t.Fatalf("fixture: the active attribute 0x%X must precede its list 0x%X to test the order", c.active, c.list)
			}
			srv, err := spec.NewServer(c.def, c.opts, spec.ServerConfig{
				Initial: map[uint32]any{c.active: c.member, c.list: c.set},
			})
			if err != nil {
				t.Fatalf("NewServer with a member: %v", err)
			}
			_, err = spec.NewServer(c.def, c.opts, spec.ServerConfig{
				Initial: map[uint32]any{c.active: c.outsider, c.list: c.set},
			})
			var se *spec.StatusError
			if !errors.Is(err, spec.ErrInvalidValue) || !errors.As(err, &se) || se.Status != im.StatusConstraintError {
				t.Fatalf("NewServer with an outsider: %v, want ErrInvalidValue / CONSTRAINT_ERROR", err)
			}
			if err := srv.Set(c.active, c.outsider); !errors.Is(err, spec.ErrInvalidValue) {
				t.Errorf("Set of an outsider: %v, want ErrInvalidValue", err)
			}
			if v, _ := srv.Value(c.active); v == nil {
				t.Errorf("refused Set cleared the value")
			}
			if err := srv.SetAttributes(map[uint32]any{c.active: c.outsider, c.list: []any{c.outsider}}); err != nil {
				t.Errorf("SetAttributes of an outsider with a list holding it: %v", err)
			}
		})
	}
}
