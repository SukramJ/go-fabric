// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package onoff_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/paritytest"
)

// TestParityMatterJS_OnOffServers builds every feature selection and holds
// each admitted server against the matter.js conformance of its definition
// (spectest.CheckServer): FeatureMap, ClusterRevision, the attribute and
// command lists, the privileges and the read-only writes. OFFONLY with LT
// or DF breaks "[!(LT | DF)]" and is refused.
func TestParityMatterJS_OnOffServers(t *testing.T) {
	t.Parallel()
	for features := range onoff.Feature(1 << 3) {
		srv, err := onoff.New(onoff.Config{Features: features, Clock: &fakeClock{}})
		conflict := features&onoff.FeatureOFFONLY != 0 && features&(onoff.FeatureLT|onoff.FeatureDF) != 0
		if conflict {
			if !errors.Is(err, onoff.ErrInvalidConfig) {
				t.Errorf("0b%03b: OFFONLY with LT / DF accepted (%v)", features, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("0b%03b: %v", features, err)
		}
		spectest.CheckServer(t, srv, onoffdef.Definition, uint32(features))
	}
}

// TestParityMatterJS_OnOffSnapshot pins the snapshot facts the server
// rests on: the four LT attributes and their access, the OnWithTimedOff
// bounds (0xFFFF is never a valid request value, only a held state), and
// the command conformances that make OffOnly and Lighting "automatically
// supported" (OnOffServer.ts:26).
func TestParityMatterJS_OnOffSnapshot(t *testing.T) {
	t.Parallel()
	js := paritytest.ClusterSnapshot(t, onoff.ClusterID)
	if js.Revision != onoffdef.Revision {
		t.Errorf("revision %d, generated %d", js.Revision, onoffdef.Revision)
	}
	for _, name := range []string{"GlobalSceneControl", "OnTime", "OffWaitTime", "StartUpOnOff"} {
		if a := js.Attribute(t, name); a.Conformance != "LT" {
			t.Errorf("%s conformance %q", name, a.Conformance)
		}
	}
	for _, name := range []string{"On", "Toggle"} {
		if c := js.Command(t, name); c.Conformance != "!OFFONLY" {
			t.Errorf("%s conformance %q", name, c.Conformance)
		}
	}
	for _, name := range []string{"OffWithEffect", "OnWithRecallGlobalScene", "OnWithTimedOff"} {
		if c := js.Command(t, name); c.Conformance != "LT" || c.Response != "status" {
			t.Errorf("%s %+v", name, c)
		}
	}
	spectest.RoundTripCommand(t, onoffdef.Definition, onoff.CmdOnWithTimedOff, spec.Request, onoffdef.OnWithTimedOffRequest{OnOffControl: onoffdef.OnOffControlAcceptOnlyWhenOn, OnTime: 65534, OffWaitTime: 1})
}

// TestParityMatterJS_OnOffServerBehaviour walks the matter.js
// OnOffBaseServer sequence of a timed
// on with a guard period, the countdown, the delayed off and a second
// OnWithTimedOff inside the guard period that only lowers OffWaitTime.
//
// Mirrors matter.js packages/node/src/behaviors/on-off/OnOffServer.ts
// onWithTimedOff (:198-225), #timedOnTick (:239-253), off (:119-139) and
// #delayedOffTick (:312-324).
func TestParityMatterJS_OnOffServerBehaviour(t *testing.T) {
	t.Parallel()
	f := newFixture(t, onoff.FeatureLT, nil)
	f.mustInvoke(onoff.CmdOnWithTimedOff, timedOff(0, 10, 20)) // :216-224
	f.want(lt{OnOff: true, GSC: true, OnTime: 10, OffWaitTime: 20})

	f.clock.Advance(time.Second) // :245-251: OnTime reaches 0 → off, OffWaitTime 0
	f.want(lt{GSC: true})

	// A plain Off with OffWaitTime enters the guard period (:131-137).
	if err := f.srv.MatterWrite(context.Background(), onoff.AttrOffWaitTime, uint64(20)); err != nil {
		t.Fatal(err)
	}
	f.mustInvoke(onoff.CmdOn, nil)
	// On with OnTime 0 clears OffWaitTime (:105-110).
	f.want(lt{OnOff: true, GSC: true})
	if err := f.srv.MatterWrite(context.Background(), onoff.AttrOffWaitTime, uint64(20)); err != nil {
		t.Fatal(err)
	}
	f.mustInvoke(onoff.CmdOff, nil)
	f.clock.Advance(500 * time.Millisecond)
	f.want(lt{GSC: true, OffWaitTime: 15})

	// Inside the guard period OnWithTimedOff only lowers OffWaitTime (:203-213).
	f.mustInvoke(onoff.CmdOnWithTimedOff, timedOff(0, 50, 5))
	f.want(lt{GSC: true, OffWaitTime: 5})
	f.clock.Advance(time.Second)
	f.want(lt{GSC: true})
}

// recScenes records the scenes-port calls; recall, when set, runs inside
// RecallGlobalScene (the ScenesManagement applying the stored values).
type recScenes struct {
	calls  []string
	recall func(ctx context.Context)
}

func (r *recScenes) MakeAllFabricSceneInfoEntriesInvalid() { r.calls = append(r.calls, "invalidate") }

func (r *recScenes) StoreGlobalScene(_ context.Context, fabric uint8) {
	r.calls = append(r.calls, fmt.Sprintf("store:%d", fabric))
}

func (r *recScenes) RecallGlobalScene(ctx context.Context, fabric uint8) {
	r.calls = append(r.calls, fmt.Sprintf("recall:%d", fabric))
	if r.recall != nil {
		r.recall(ctx)
	}
}

// TestParityMatterJS_OnOffScenePaths holds one row per scene path of
// matter.js OnOffServer.ts against the scenes port: the invalidation only
// on a change of OnOff (:97-100, :114-117, :119-122), the global-scene
// store of OffWithEffect while GlobalSceneControl is true and only for an
// accessing fabric (:159-165), the recall of OnWithRecallGlobalScene while
// it is false (:171-184), and the scene apply with and without a
// transition time (:267-310, the pending value dropped by a change of
// OnOff, :62).
func TestParityMatterJS_OnOffScenePaths(t *testing.T) {
	t.Parallel()
	fabric1 := im.WithFabricFilter(context.Background(), true, 1)
	type step struct {
		ctx    context.Context //nolint:containedctx // a row's accessing fabric
		cmd    uint32
		fields any
	}
	for _, tc := range []struct {
		name  string
		start lt
		steps []step
		calls []string
		end   lt
	}{
		{"On from off invalidates", lt{}, []step{{fabric1, onoff.CmdOn, nil}}, []string{"invalidate"}, lt{OnOff: true, GSC: true}},
		{"On while on keeps the scene", lt{OnOff: true, GSC: true}, []step{{fabric1, onoff.CmdOn, nil}}, nil, lt{OnOff: true, GSC: true}},
		{"Off from on invalidates", lt{OnOff: true, GSC: true}, []step{{fabric1, onoff.CmdOff, nil}}, []string{"invalidate"}, lt{GSC: true}},
		{"Off while off keeps the scene", lt{}, []step{{fabric1, onoff.CmdOff, nil}}, nil, lt{}},
		{"Toggle invalidates", lt{}, []step{{fabric1, onoff.CmdToggle, nil}}, []string{"invalidate"}, lt{OnOff: true, GSC: true}},
		{"OffWithEffect stores the global scene", lt{OnOff: true, GSC: true}, []step{{fabric1, onoff.CmdOffWithEffect, onoffdef.OffWithEffectRequest{}}}, []string{"store:1", "invalidate"}, lt{}},
		{"OffWithEffect without GSC stores nothing", lt{OnOff: true}, []step{{fabric1, onoff.CmdOffWithEffect, onoffdef.OffWithEffectRequest{}}}, []string{"invalidate"}, lt{}},
		{"OffWithEffect without a fabric stores nothing", lt{OnOff: true, GSC: true}, []step{{context.Background(), onoff.CmdOffWithEffect, onoffdef.OffWithEffectRequest{}}}, []string{"invalidate"}, lt{}},
		{"OnWithRecallGlobalScene recalls", lt{}, []step{{fabric1, onoff.CmdOnWithRecallGlobalScene, nil}}, []string{"recall:1", "invalidate"}, lt{OnOff: true, GSC: true}},
		{"OnWithRecallGlobalScene with GSC does nothing", lt{GSC: true}, []step{{fabric1, onoff.CmdOnWithRecallGlobalScene, nil}}, nil, lt{GSC: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, onoff.FeatureLT, map[uint32]any{onoff.AttrOnOff: tc.start.OnOff, onoff.AttrGlobalSceneControl: tc.start.GSC})
			sc := &recScenes{}
			f.srv.SetScenes(sc)
			for _, s := range tc.steps {
				if _, err := f.srv.MatterInvoke(s.ctx, s.cmd, s.fields); err != nil {
					t.Fatalf("invoke 0x%02X: %v", s.cmd, err)
				}
			}
			if !slices.Equal(sc.calls, tc.calls) {
				t.Errorf("scenes calls %v, want %v", sc.calls, tc.calls)
			}
			f.want(tc.end)
		})
	}

	t.Run("recall applies the scene inside the command", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, onoff.FeatureLT, nil)
		sc := &recScenes{}
		sc.recall = func(ctx context.Context) {
			// The stored scene is on: applied at once to the command's state.
			f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{onoff.AttrOnOff: 1}, 0)
		}
		f.srv.SetScenes(sc)
		f.mustInvokeCtx(fabric1, onoff.CmdOnWithRecallGlobalScene, nil)
		if want := []string{"recall:1", "invalidate"}; !slices.Equal(sc.calls, want) {
			t.Errorf("scenes calls %v, want %v (on() after the recall changes nothing)", sc.calls, want)
		}
		f.want(lt{OnOff: true, GSC: true})
	})

	t.Run("scene apply", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, onoff.FeatureLT, nil)
		sc := &recScenes{}
		f.srv.SetScenes(sc)
		ctx := context.Background()
		f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{onoff.AttrOnOff: 2}, 0) // not a boolean (:273)
		f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{}, 0)                   // no OnOff value
		f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{onoff.AttrOnOff: 0}, 0) // equals OnOff
		f.want(lt{})
		f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{onoff.AttrOnOff: 1}, 0) // :277-281 at once
		f.want(lt{OnOff: true, GSC: true})

		f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{onoff.AttrOnOff: 0}, 1500) // :283-288 delayed
		f.clock.Advance(1400 * time.Millisecond)
		f.want(lt{OnOff: true, GSC: true})
		f.clock.Advance(100 * time.Millisecond) // :299-309
		f.want(lt{GSC: true})

		f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{onoff.AttrOnOff: 1}, 1000)
		f.mustInvoke(onoff.CmdOn, nil) // OnOff changes: the pending value goes (:62)
		f.mustInvoke(onoff.CmdOff, nil)
		f.clock.Advance(2 * time.Second)
		f.want(lt{GSC: true})

		f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{onoff.AttrOnOff: 1}, 200) // delayed on (:305-306)
		f.clock.Advance(200 * time.Millisecond)
		f.want(lt{OnOff: true, GSC: true})
		f.srv.MatterApplySceneValues(ctx, map[uint32]uint64{onoff.AttrOnOff: 0}, 0) // off at once (:281)
		f.want(lt{GSC: true})
		if want := slices.Repeat([]string{"invalidate"}, 6); !slices.Equal(sc.calls, want) {
			t.Errorf("scenes calls %v, want %v", sc.calls, want)
		}
	})

	t.Run("without a port", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, onoff.FeatureLT, map[uint32]any{onoff.AttrOnOff: true, onoff.AttrGlobalSceneControl: true})
		if f.srv.MatterInvalidatesScenes() {
			t.Fatal("a server without a scenes port claims the invalidation")
		}
		f.mustInvokeCtx(fabric1, onoff.CmdOffWithEffect, onoffdef.OffWithEffectRequest{})
		f.mustInvokeCtx(fabric1, onoff.CmdOnWithRecallGlobalScene, nil)
		f.want(lt{OnOff: true, GSC: true})
		f.srv.SetScenes(&recScenes{})
		if !f.srv.MatterInvalidatesScenes() {
			t.Fatal("a server with a scenes port leaves the invalidation to the endpoint")
		}
	})
}
