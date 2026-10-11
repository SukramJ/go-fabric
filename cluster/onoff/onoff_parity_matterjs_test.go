// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package onoff_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
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
