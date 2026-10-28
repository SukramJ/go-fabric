// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The rules of chip's AmbientContextSensingCluster.cpp at 6170af84 that
// TestAmbientContextSensingCluster.cpp does not reach: the configuration,
// the events, the hold extension, the eviction at the limit, the predicted
// activity checks and the attributes that are the server's.
package sensing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	"github.com/SukramJ/go-fabric/cluster/spec"
	def "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestAmbientContextConfigRefusals: no detection feature (:52-54), a
// SimultaneousDetectionLimit outside 1..10 (:389, the model's "1 to 10"),
// a supported list chip's check refuses, and a persisted ObjectCountConfig
// without the label its MfgCode needs.
func TestAmbientContextConfigRefusals(t *testing.T) {
	label := "x"
	for name, cfg := range map[string]sensing.AmbientContextConfig{
		"no feature":    {Features: sensing.FeaturePredictedActivity},
		"limit 11":      {Features: sensing.FeatureHumanActivity, SimultaneousDetectionLimit: 11},
		"supported AUD": {Features: sensing.FeatureHumanActivity, AmbientContextTypeSupported: []sensing.SemanticTag{audFall}},
		"config label": {
			Features:          sensing.FeatureObjectCounting | sensing.FeatureObjectIdentification,
			ObjectCountConfig: &sensing.ObjectCountConfig{CountingObject: mfgTag(testVendor1, nil), ObjectCountThreshold: ptr[uint16](5)},
		},
		"config threshold": {
			Features:          sensing.FeatureObjectCounting | sensing.FeatureObjectIdentification,
			ObjectCountConfig: &sensing.ObjectCountConfig{CountingObject: mfgTag(testVendor1, &label), ObjectCountThreshold: ptr[uint16](0)},
		},
	} {
		if _, err := sensing.NewAmbientContext(cfg); !errors.Is(err, sensing.ErrAmbientContextConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestAmbientContextConfigValues: the sanitised limits (:443-449), a
// persisted HoldTime outside them falls back to their default (:74-85),
// the configured limit and supported list are served.
func TestAmbientContextConfigValues(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{
		Features: sensing.FeatureHumanActivity, HoldTimeLimits: limits(0, 5, 900), HoldTime: ptr[uint16](500),
		SimultaneousDetectionLimit: 3, AmbientContextTypeSupported: []sensing.SemanticTag{haFall}, Clock: newFake(),
	})
	l := read[sensing.HoldTimeLimits](t, c, def.AttrHoldTimeLimits)
	if *l.HoldTimeMin != 1 || *l.HoldTimeMax != 10 || *l.HoldTimeDefault != 10 {
		t.Errorf("limits %d %d %d, want 1 10 10", *l.HoldTimeMin, *l.HoldTimeMax, *l.HoldTimeDefault)
	}
	if c.HoldTime() != 10 || read[uint8](t, c, def.AttrSimultaneousDetectionLimit) != 3 {
		t.Errorf("HoldTime %d", c.HoldTime())
	}
	if len(read[spec.List[sensing.SemanticTag]](t, c, def.AttrAmbientContextTypeSupported)) != 1 {
		t.Error("supported list not served")
	}
	// No counting: neither the counting attributes nor ObjectCount.
	for _, id := range []uint32{def.AttrObjectCountConfig, def.AttrObjectCount, def.AttrObjectIdentified, def.AttrPredictedActivity} {
		if _, ok := c.MatterRead(id); ok {
			t.Errorf("0x%04X served", id)
		}
	}
}

// TestAmbientContextEvents: a new detection emits DetectStarted with its
// tags (:285-288, :558-564), the same one again does not, its expiry emits
// DetectEnded with the start as posix-ms and systime-ms (:584-591, :807);
// an object count emits DetectStarted with the counting tag, the reached
// flag and the count (:566-582) and DetectEnded when it expires (:812-819).
func TestAmbientContextEvents(t *testing.T) {
	clk := newFake()
	start := clk.Now()
	c := newACS(t, sensing.AmbientContextConfig{
		Features: allFeatures, HoldTimeLimits: limits(10, 300, 20), AmbientContextTypeSupported: supportedArray(), Clock: clk,
	})
	em := &events{}
	c.SetMatterEventEmitter(em)
	c.SetEndpoint(3)
	clk.Advance(2 * time.Second)
	if err := c.AddDetection(detect(haFall)); err != nil {
		t.Fatal(err)
	}
	clk.Advance(5 * time.Second)
	if err := c.AddDetection(detect(haFall)); err != nil {
		t.Fatal(err)
	}
	got := em.all()
	if len(got) != 1 || got[0].id != def.EventAmbientContextDetectStarted {
		t.Fatalf("events %v", got)
	}
	ev, _ := got[0].data.(def.AmbientContextDetectStartedEvent)
	if ev.AmbientContextDetected == nil || (*ev.AmbientContextDetected.AmbientContextSensed)[0].Tag != haFall.Tag || ev.ObjectCount != nil {
		t.Errorf("DetectStarted %+v", ev)
	}
	clk.Advance(30 * time.Second)
	got = em.all()
	if len(got) != 2 || got[1].id != def.EventAmbientContextDetectEnded {
		t.Fatalf("events %v", got)
	}
	end, _ := got[1].data.(def.AmbientContextDetectEndedEvent)
	if *end.EventStartTimePos != uint64(start.Add(2*time.Second).UnixMilli()) || *end.EventStartTimeSys != 2000 { //nolint:gosec // positive
		t.Errorf("DetectEnded %d / %d", *end.EventStartTimePos, *end.EventStartTimeSys)
	}

	if err := c.MatterWrite(context.Background(), def.AttrObjectCountConfig,
		sensing.ObjectCountConfig{CountingObject: ptr(oiChild), ObjectCountThreshold: ptr[uint16](3)}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetObjectCount(4); err != nil {
		t.Fatal(err)
	}
	if err := c.SetObjectCount(4); err != nil { // :374: the same count changes nothing
		t.Fatal(err)
	}
	got = em.all()
	if len(got) != 3 {
		t.Fatalf("events %v", got)
	}
	cnt, _ := got[2].data.(def.AmbientContextDetectStartedEvent)
	if (*cnt.AmbientContextDetected.AmbientContextSensed)[0].Tag != oiChild.Tag || !*cnt.ObjectCountThresholdReached || *cnt.ObjectCount != 4 {
		t.Errorf("count DetectStarted %+v", cnt)
	}
	clk.Advance(21 * time.Second)
	if got = em.all(); len(got) != 4 || got[3].id != def.EventAmbientContextDetectEnded {
		t.Errorf("events %v", got)
	}
	if clk.Pending() != 0 {
		t.Errorf("%d timers armed with nothing held", clk.Pending())
	}
}

// TestAmbientContextHoldExtension: the same detection again keeps its
// start, and its end is the start plus HoldTime, or plus what is left of
// HoldTimeMax when that is less (:270-284) — chip adds the hold to the
// start, not to now, so a repeated detection does not outlast its first
// hold.
func TestAmbientContextHoldExtension(t *testing.T) {
	clk := newFake()
	c := newACS(t, sensing.AmbientContextConfig{
		Features: sensing.FeatureHumanActivity, HoldTimeLimits: limits(10, 300, 20), AmbientContextTypeSupported: []sensing.SemanticTag{haFall}, Clock: clk,
	})
	if err := c.AddDetection(detect(haFall)); err != nil {
		t.Fatal(err)
	}
	clk.Advance(5 * time.Second)
	if err := c.AddDetection(detect(haFall)); err != nil { // 5 + 20 <= 300: end = start + 20
		t.Fatal(err)
	}
	clk.Advance(14 * time.Second)
	if !read[bool](t, c, def.AttrHumanActivityDetected) {
		t.Fatal("expired before start + HoldTime")
	}
	clk.Advance(time.Second)
	if read[bool](t, c, def.AttrHumanActivityDetected) {
		t.Error("held past start + HoldTime")
	}

	c = newACS(t, sensing.AmbientContextConfig{
		Features: sensing.FeatureHumanActivity, HoldTimeLimits: limits(10, 30, 20), AmbientContextTypeSupported: []sensing.SemanticTag{haFall}, Clock: clk,
	})
	if err := c.AddDetection(detect(haFall)); err != nil {
		t.Fatal(err)
	}
	clk.Advance(15 * time.Second)
	if err := c.AddDetection(detect(haFall)); err != nil { // 15 + 20 > 30: hold 30 - 15, end = start + 15 = now
		t.Fatal(err)
	}
	clk.Advance(0)
	if read[bool](t, c, def.AttrHumanActivityDetected) {
		t.Error("held although the capped hold has passed")
	}
}

// TestAmbientContextEviction: at SimultaneousDetectionLimit the oldest
// detection makes room, without an end event (:214-233); the list reads
// newest first (:289).
func TestAmbientContextEviction(t *testing.T) {
	clk := newFake()
	c := newACS(t, sensing.AmbientContextConfig{
		Features: allFeatures, SimultaneousDetectionLimit: 2, AmbientContextTypeSupported: supportedArray(), Clock: clk,
	})
	em := &events{}
	c.SetMatterEventEmitter(em)
	for _, d := range []sensing.SemanticTag{haFall, oiAdult, audFall} {
		if err := c.AddDetection(detect(d)); err != nil {
			t.Fatal(err)
		}
	}
	list := read[spec.List[sensing.Detection]](t, c, def.AttrAmbientContextType)
	if len(list) != 2 || (*list[0].AmbientContextSensed)[0].NamespaceId != sensing.NamespaceIdentifiedSound {
		t.Fatalf("AmbientContextType %v", list)
	}
	if read[bool](t, c, def.AttrHumanActivityDetected) || !read[bool](t, c, def.AttrAudioContextDetected) {
		t.Error("detection attributes after the eviction")
	}
	for _, e := range em.all() {
		if e.id == def.EventAmbientContextDetectEnded {
			t.Error("an evicted detection emitted DetectEnded")
		}
	}
	two := detect(haFall, oiAdult)
	if err := c.AddDetection(two); err != nil {
		t.Fatal(err)
	}
	three := []sensing.SemanticTag{haFall, oiAdult, audFall}
	if err := c.AddDetection(sensing.Detection{AmbientContextSensed: &three}); !errors.Is(err, spec.ErrInvalidValue) { // :208
		t.Errorf("three tags: %v", err)
	}
	if err := c.AddDetection(sensing.Detection{}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("no tags: %v", err)
	}
}

// TestAmbientContextObjectCountRefusals: SetObjectCount needs OC and OI
// (:371) and a count of at least 1 (:373).
func TestAmbientContextObjectCountRefusals(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: sensing.FeatureObjectCounting, Clock: newFake()})
	if err := c.SetObjectCount(3); !errors.Is(err, sensing.ErrNotSupported) {
		t.Errorf("without OI: %v", err)
	}
	c = newACS(t, sensing.AmbientContextConfig{Features: sensing.FeatureObjectCounting | sensing.FeatureObjectIdentification, Clock: newFake()})
	if err := c.SetObjectCount(0); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("count 0: %v", err)
	}
}

// TestAmbientContextPredictedActivityRules: CheckPredictedActivity
// (:844-892) and the list bound (:470); PredictedActivity needs PRED.
func TestAmbientContextPredictedActivityRules(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: sensing.FeatureHumanActivity | sensing.FeatureObjectCounting | sensing.FeaturePredictedActivity, Clock: newFake()})
	win := func(start, end uint32, tags []sensing.SemanticTag, crowd *uint8) sensing.PredictedActivity {
		p := sensing.PredictedActivity{StartTimestamp: &start, EndTimestamp: &end, CrowdCount: crowd, Confidence: ptr[uint8](50)}
		if tags != nil {
			p.AmbientContextType = &tags
		}
		return p
	}
	for name, list := range map[string][]sensing.PredictedActivity{
		"start at end":       {win(10, 10, nil, nil)},
		"overlap":            {win(10, 20, nil, nil), win(20, 30, nil, nil)},
		"namespace without":  {win(10, 20, []sensing.SemanticTag{oiAdult}, nil)},
		"unknown namespace":  {win(10, 20, []sensing.SemanticTag{tag(sensing.Namespace(1), 0)}, nil)},
		"crowd count 0":      {win(10, 20, nil, ptr[uint8](0))},
		"crowd count 255":    {win(10, 20, nil, ptr[uint8](255))},
		"twenty-one windows": make21(),
	} {
		if err := set(c, def.Patch{PredictedActivity: &list}); !errors.Is(err, spec.ErrInvalidValue) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := []sensing.PredictedActivity{win(10, 20, []sensing.SemanticTag{haFall}, ptr[uint8](254)), win(21, 30, nil, nil)}
	if err := set(c, def.Patch{PredictedActivity: &ok}); err != nil {
		t.Fatal(err)
	}
	c = newACS(t, sensing.AmbientContextConfig{Features: sensing.FeatureHumanActivity, Clock: newFake()})
	if err := set(c, def.Patch{PredictedActivity: &ok}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("without PRED: %v", err)
	}
}

func make21() []sensing.PredictedActivity {
	out := make([]sensing.PredictedActivity, 21)
	for i := range out {
		s, e := uint32(10*i+1), uint32(10*i+5) //nolint:gosec // small
		out[i] = sensing.PredictedActivity{StartTimestamp: &s, EndTimestamp: &e, Confidence: ptr[uint8](1)}
	}
	return out
}

// TestAmbientContextServerHeld: the detection attributes and the detections
// are the server's, ObjectCount is SetObjectCount's; nothing is stored
// when one value of a set is refused; a controller cannot write the fixed
// or derived attributes; the cluster has no command.
func TestAmbientContextServerHeld(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, ServeObjectCount: true, Clock: newFake()})
	for name, p := range map[string]def.Patch{
		"HumanActivityDetected": {HumanActivityDetected: ptr(true)},
		"AmbientContextType":    {AmbientContextType: &[]sensing.Detection{}},
		"ObjectCount":           {ObjectCount: ptr[uint16](2)},
		"Threshold reached":     {ObjectCountThresholdReached: ptr(true)},
	} {
		if err := set(c, p); !errors.Is(err, spec.ErrNotHeld) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := set(c, def.Patch{HoldTime: ptr[uint16](20), SimultaneousDetectionLimit: ptr[uint8](0)}); err == nil {
		t.Fatal("a limit of 0 stored")
	}
	if c.HoldTime() != sensing.DefaultHoldTimeDefault {
		t.Errorf("HoldTime %d stored beside a refused value", c.HoldTime())
	}
	ctx := context.Background()
	for _, id := range []uint32{def.AttrSimultaneousDetectionLimit, def.AttrHumanActivityDetected, def.AttrHoldTimeLimits} {
		if err := c.MatterWrite(ctx, id, uint64(1)); statusOf(err) != im.StatusUnsupportedWrite {
			t.Errorf("write 0x%04X: %v", id, err)
		}
	}
	if _, err := c.MatterInvoke(ctx, 0x00, nil); statusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("a command: %v", err)
	}
	if err := c.MatterWrite(ctx, def.AttrHoldTime, uint64(sensing.DefaultHoldTimeDefault)); err != nil { // :153
		t.Errorf("the same HoldTime: %v", err)
	}
}

// TestAmbientContextSupportedDuplicates: a tag beyond chip's table counts
// as Unknown (TagToIndex, :709-723), so it collides with tag 0; a tag in
// the table does not; the list holds 1 to 50 tags (:194).
func TestAmbientContextSupportedDuplicates(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: sensing.FeatureObjectIdentification, Clock: newFake()})
	dup := []sensing.SemanticTag{tag(sensing.NamespaceIdentifiedObject, 0), tag(sensing.NamespaceIdentifiedObject, 200)}
	if err := set(c, def.Patch{AmbientContextTypeSupported: &dup}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("an unknown tag beside Unknown: %v", err)
	}
	if err := set(c, def.Patch{AmbientContextTypeSupported: &[]sensing.SemanticTag{}}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("an empty list: %v", err)
	}
	ok := []sensing.SemanticTag{tag(sensing.NamespaceIdentifiedObject, 0), tag(sensing.NamespaceIdentifiedObject, 12)}
	if err := set(c, def.Patch{AmbientContextTypeSupported: &ok}); err != nil {
		t.Errorf("two known tags: %v", err)
	}
	if def.Wrap(c).Host() == nil {
		t.Error("no Host")
	}
}

// TestAmbientContextChangeReports: a detection and its expiry report the
// derived attributes; Close stops the timer.
func TestAmbientContextChangeReports(t *testing.T) {
	clk := newFake()
	c := newACS(t, sensing.AmbientContextConfig{Features: sensing.FeatureHumanActivity, AmbientContextTypeSupported: []sensing.SemanticTag{haFall}, Clock: clk})
	var seen []uint32
	c.OnAttributeChange(func(ch contract.Change) { seen = append(seen, ch.Attributes...) })
	v0 := c.MatterDataVersion()
	if err := c.AddDetection(detect(haFall)); err != nil {
		t.Fatal(err)
	}
	if c.MatterDataVersion() == v0 || len(seen) != 2 || seen[0] != def.AttrHumanActivityDetected || seen[1] != def.AttrAmbientContextType {
		t.Errorf("reported %v", seen)
	}
	c.Close()
	if clk.Pending() != 0 {
		t.Error("timer armed after Close")
	}
}
