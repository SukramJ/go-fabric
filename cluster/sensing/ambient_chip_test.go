// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// AmbientContextSensing: a port of connectedhomeip
// src/app/clusters/ambient-context-sensing-server/tests/
// TestAmbientContextSensingCluster.cpp at 6170af84, by meaning. chip's
// ClusterTester reads and writes the cluster directly; here the server's
// MatterRead and MatterWrite do. chip's public setters are the host's
// inputs here: SetAmbientContextTypeSupported, SetHoldTime,
// SetHoldTimeLimits, SetSimultaneousDetectionLimit, SetObjectCountConfig
// and SetPredictedActivity are the server's SetAttributes (the typed Set),
// AddDetection and SetObjectCount its own methods. TestACSDelegate
// (:113-231) is the server's own state, TimerDelegateMock the fake clock,
// and chip's attribute persistence the host's store fed from the change
// notification. chip's SensorFusion feature is not in the matter.js model
// (TestSensorFusionSupported, :1100-1151, becomes the check that the
// attribute is not served).
package sensing_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/sensing"
	"github.com/SukramJ/go-fabric/cluster/spec"
	def "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// allFeatures is kFeaturesAllForACSTest (:49-51) without SensorFusion.
const allFeatures = sensing.FeatureHumanActivity | sensing.FeatureObjectCounting | sensing.FeatureObjectIdentification |
	sensing.FeatureSoundIdentification | sensing.FeaturePredictedActivity

// tag is a standard semantic tag (no manufacturer code).
func tag(ns sensing.Namespace, t uint8) sensing.SemanticTag {
	return sensing.SemanticTag{MfgCode: spec.NullOf[uint16](), NamespaceId: ns, Tag: t}
}

// The namespace tags the cases use (ambient-context-sensing-namespace.h).
var (
	haFall     = tag(sensing.NamespaceIdentifiedHumanActivity, 0x01)
	haSleeping = tag(sensing.NamespaceIdentifiedHumanActivity, 0x02)
	oiAdult    = tag(sensing.NamespaceIdentifiedObject, 0x01)
	oiChild    = tag(sensing.NamespaceIdentifiedObject, 0x02)
	audFall    = tag(sensing.NamespaceIdentifiedSound, 0x01)
	audSnoring = tag(sensing.NamespaceIdentifiedSound, 0x02)
)

// supportedArray is g_kACTSupportedArray (:52-77).
func supportedArray() []sensing.SemanticTag {
	return []sensing.SemanticTag{haFall, haSleeping, oiAdult, oiChild, audFall, audSnoring}
}

// detect is a detection of the given tags (g_kACTDetectArray, :98-111).
func detect(tags ...sensing.SemanticTag) sensing.Detection {
	return sensing.Detection{AmbientContextSensed: &tags}
}

func ptr[T any](v T) *T { return &v }

func limits(lo, hi, dflt uint16) *sensing.HoldTimeLimits {
	return &sensing.HoldTimeLimits{HoldTimeMin: &lo, HoldTimeMax: &hi, HoldTimeDefault: &dflt}
}

func newFake() *clocktest.Fake { return clocktest.New(time.Unix(1_800_000_000, 0)) }

func newACS(t *testing.T, cfg sensing.AmbientContextConfig) *sensing.AmbientContext {
	t.Helper()
	c, err := sensing.NewAmbientContext(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func read[T any](t *testing.T, c *sensing.AmbientContext, id uint32) T {
	t.Helper()
	v, ok := c.MatterRead(id)
	if !ok {
		t.Fatalf("attribute 0x%04X not read", id)
	}
	out, ok := v.(T)
	if !ok {
		t.Fatalf("attribute 0x%04X is %T", id, v)
	}
	return out
}

func set(c *sensing.AmbientContext, p def.Patch) error { return def.Wrap(c).Set(p) }

// statusOf is the IM status an error answers, SUCCESS for none.
func statusOf(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return sc.MatterStatusCode()
	}
	return im.StatusFailure
}

// events records the emitted events.
type events struct {
	mu   sync.Mutex
	list []recorded
}

type recorded struct {
	id   uint32
	data any
}

func (e *events) MatterEmitEvent(_ uint16, _, id uint32, data any, _ contract.EventPriority) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, recorded{id, data})
}

func (e *events) all() []recorded {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.list)
}

// TestAttributes ports :241-323.
func TestAttributes(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, Clock: newFake()})
	if got := read[uint16](t, c, spec.AttrClusterRevision); got != def.Revision {
		t.Errorf("ClusterRevision %d", got)
	}
	if got := read[uint32](t, c, spec.AttrFeatureMap); got != uint32(allFeatures) {
		t.Errorf("FeatureMap 0x%X", got)
	}
	for _, id := range []uint32{def.AttrHumanActivityDetected, def.AttrObjectIdentified, def.AttrAudioContextDetected, def.AttrObjectCountThresholdReached} {
		if read[bool](t, c, id) {
			t.Errorf("0x%04X detected at start", id)
		}
	}
	want := []uint32{
		def.AttrHumanActivityDetected, def.AttrObjectIdentified, def.AttrAudioContextDetected, def.AttrAmbientContextType,
		def.AttrAmbientContextTypeSupported, def.AttrObjectCountThresholdReached, def.AttrObjectCountConfig,
		def.AttrSimultaneousDetectionLimit, def.AttrHoldTime, def.AttrHoldTimeLimits, def.AttrPredictedActivity,
	}
	var got []uint32
	for _, id := range c.MatterAttributes() {
		if id < 0xF000 {
			got = append(got, id)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("attributes %v, want %v", got, want)
	}
	if l := read[spec.List[sensing.SemanticTag]](t, c, def.AttrAmbientContextTypeSupported); len(l) != 0 {
		t.Errorf("AmbientContextTypeSupported %v", l)
	}
	if _, ok := c.MatterRead(def.AttrObjectCount); ok { // :306-307 UnsupportedAttribute
		t.Error("ObjectCount served without the optional attribute")
	}
	if got := read[uint8](t, c, def.AttrSimultaneousDetectionLimit); got != sensing.DefaultSimultaneousDetectionLimit {
		t.Errorf("SimultaneousDetectionLimit %d", got)
	}
	if got := read[uint16](t, c, def.AttrHoldTime); got != sensing.DefaultHoldTimeDefault {
		t.Errorf("HoldTime %d", got)
	}
	l := read[sensing.HoldTimeLimits](t, c, def.AttrHoldTimeLimits)
	if *l.HoldTimeMin != sensing.DefaultHoldTimeMin || *l.HoldTimeMax != sensing.DefaultHoldTimeMax || *l.HoldTimeDefault != sensing.DefaultHoldTimeDefault {
		t.Errorf("HoldTimeLimits %d %d %d", *l.HoldTimeMin, *l.HoldTimeMax, *l.HoldTimeDefault)
	}
}

// TestAmbientContextTypeSupported ports :325-434.
func TestAmbientContextTypeSupported(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{
		Features: sensing.FeatureHumanActivity | sensing.FeatureObjectCounting | sensing.FeatureObjectIdentification | sensing.FeaturePredictedActivity,
		Clock:    newFake(),
	})
	dup := []sensing.SemanticTag{haFall, haSleeping, oiAdult, oiChild, oiChild}
	if err := set(c, def.Patch{AmbientContextTypeSupported: &dup}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a duplicate: %v", err)
	}
	if l := read[spec.List[sensing.SemanticTag]](t, c, def.AttrAmbientContextTypeSupported); len(l) != 0 {
		t.Errorf("stored after a refusal: %v", l)
	}
	unsupported := []sensing.SemanticTag{haFall, haSleeping, oiAdult, oiChild, audFall, audSnoring}
	if err := set(c, def.Patch{AmbientContextTypeSupported: &unsupported}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a namespace without its feature: %v", err)
	}
	if l := read[spec.List[sensing.SemanticTag]](t, c, def.AttrAmbientContextTypeSupported); len(l) != 0 {
		t.Errorf("stored after a refusal: %v", l)
	}
	ok := []sensing.SemanticTag{haFall, haSleeping, oiAdult, oiChild}
	if err := set(c, def.Patch{AmbientContextTypeSupported: &ok}); err != nil {
		t.Fatal(err)
	}
	if l := read[spec.List[sensing.SemanticTag]](t, c, def.AttrAmbientContextTypeSupported); len(l) != 4 {
		t.Errorf("AmbientContextTypeSupported %v", l)
	}
	if err := c.AddDetection(detect(audFall)); !errors.Is(err, sensing.ErrNotSupported) { // :422-432
		t.Errorf("an unsupported detection: %v", err)
	}
}

// TestHoldTimeAttribute ports :436-489.
func TestHoldTimeAttribute(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, HoldTimeLimits: limits(10, 200, 100), HoldTime: ptr[uint16](100), Clock: newFake()})
	if err := set(c, def.Patch{HoldTime: ptr[uint16](150)}); err != nil {
		t.Fatal(err)
	}
	if got := read[uint16](t, c, def.AttrHoldTime); got != 150 || c.HoldTime() != 150 {
		t.Errorf("HoldTime %d / %d", got, c.HoldTime())
	}
	if err := set(c, def.Patch{HoldTime: ptr[uint16](150)}); err != nil { // :459 kWriteSuccessNoOp
		t.Errorf("the same HoldTime: %v", err)
	}
	for _, h := range []uint16{5, 250} { // :462, :465
		if err := set(c, def.Patch{HoldTime: &h}); statusOf(err) != im.StatusConstraintError {
			t.Errorf("HoldTime %d: %v", h, err)
		}
	}
	ctx := context.Background()
	if err := c.MatterWrite(ctx, def.AttrHoldTime, uint64(180)); err != nil {
		t.Fatal(err)
	}
	if got := read[uint16](t, c, def.AttrHoldTime); got != 180 || c.HoldTime() != 180 {
		t.Errorf("HoldTime %d / %d", got, c.HoldTime())
	}
	for _, h := range []uint64{5, 250} { // :474-479
		if err := c.MatterWrite(ctx, def.AttrHoldTime, h); statusOf(err) != im.StatusConstraintError {
			t.Errorf("write HoldTime %d: %v", h, err)
		}
	}
	if err := set(c, def.Patch{HoldTimeLimits: limits(201, 300, 250)}); err != nil { // :481-486
		t.Fatal(err)
	}
	if c.HoldTime() != 250 {
		t.Errorf("HoldTime %d after the limits moved, want their default 250", c.HoldTime())
	}
}

// persisted is the host's store of the attributes the server persists,
// fed from the change notification.
type persisted struct {
	mu       sync.Mutex
	holdTime *uint16
	occ      *sensing.ObjectCountConfig
}

func (p *persisted) watch(c *sensing.AmbientContext) {
	c.OnAttributeChange(func(ch contract.Change) {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, id := range ch.Attributes {
			switch id {
			case def.AttrHoldTime:
				h := c.HoldTime()
				p.holdTime = &h
			case def.AttrObjectCountConfig:
				o := c.ObjectCountConfig()
				p.occ = &o
			}
		}
	})
}

// TestHoldTimePersistence ports :491-550.
func TestHoldTimePersistence(t *testing.T) {
	store := &persisted{}
	build := func() *sensing.AmbientContext {
		store.mu.Lock()
		h := store.holdTime
		store.mu.Unlock()
		c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, HoldTimeLimits: limits(10, 200, 100), HoldTime: h, Clock: newFake()})
		store.watch(c)
		return c
	}
	if c := build(); c.HoldTime() != 100 { // 1.
		t.Errorf("HoldTime %d, want the default 100", c.HoldTime())
	}
	c := build() // 2.
	if err := c.MatterWrite(context.Background(), def.AttrHoldTime, uint64(150)); err != nil {
		t.Fatal(err)
	}
	if c.HoldTime() != 150 {
		t.Errorf("HoldTime %d", c.HoldTime())
	}
	if c := build(); c.HoldTime() != 150 || read[uint16](t, c, def.AttrHoldTime) != 150 { // 3.
		t.Errorf("HoldTime %d after the restart, want the persisted 150", c.HoldTime())
	}
}

// TestAmbientContextDetect ports :552-641.
func TestAmbientContextDetect(t *testing.T) {
	clk := newFake()
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, HoldTimeLimits: limits(10, 200, 100), HoldTime: ptr(sensing.DefaultHoldTimeDefault), Clock: clk})
	sup := supportedArray()
	if err := set(c, def.Patch{AmbientContextTypeSupported: &sup}); err != nil {
		t.Fatal(err)
	}
	if l := read[spec.List[sensing.SemanticTag]](t, c, def.AttrAmbientContextTypeSupported); len(l) != 6 {
		t.Errorf("AmbientContextTypeSupported %v", l)
	}
	for _, id := range []uint32{def.AttrHumanActivityDetected, def.AttrObjectIdentified, def.AttrAudioContextDetected} {
		if read[bool](t, c, id) {
			t.Errorf("0x%04X detected before a detection", id)
		}
	}
	if err := c.AddDetection(detect(haFall)); err != nil {
		t.Fatal(err)
	}
	if !read[bool](t, c, def.AttrHumanActivityDetected) || clk.Pending() == 0 {
		t.Error("not detected, or no timer armed")
	}
	list := read[spec.List[sensing.Detection]](t, c, def.AttrAmbientContextType)
	if len(list) != 1 || len(*list[0].AmbientContextSensed) != 1 {
		t.Fatalf("AmbientContextType %v", list)
	}
	if got := (*list[0].AmbientContextSensed)[0]; got.NamespaceId != haFall.NamespaceId || got.Tag != haFall.Tag {
		t.Errorf("detected %v", got)
	}
	clk.Advance(time.Duration(sensing.DefaultHoldTimeDefault+1) * time.Second)
	if read[bool](t, c, def.AttrHumanActivityDetected) {
		t.Error("still detected after the hold time")
	}
}

// TestSimultaneousDetectLimit ports :643-701.
func TestSimultaneousDetectLimit(t *testing.T) {
	clk := newFake()
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, HoldTimeLimits: limits(10, 200, 100), HoldTime: ptr(sensing.DefaultHoldTimeDefault), Clock: clk})
	sup := supportedArray()
	if err := set(c, def.Patch{AmbientContextTypeSupported: &sup}); err != nil {
		t.Fatal(err)
	}
	if got := read[uint8](t, c, def.AttrSimultaneousDetectionLimit); got != sensing.DefaultSimultaneousDetectionLimit {
		t.Errorf("SimultaneousDetectionLimit %d", got)
	}
	if err := set(c, def.Patch{SimultaneousDetectionLimit: ptr(sensing.DefaultSimultaneousDetectionLimit + 1)}); err != nil {
		t.Fatal(err)
	}
	for i, d := range []sensing.SemanticTag{haFall, oiAdult, oiChild} { // :673-677
		if i > 0 {
			clk.Advance(time.Second)
		}
		if err := c.AddDetection(detect(d)); err != nil {
			t.Fatal(err)
		}
	}
	if !read[bool](t, c, def.AttrHumanActivityDetected) || !read[bool](t, c, def.AttrObjectIdentified) {
		t.Fatal("not detected")
	}
	if err := set(c, def.Patch{SimultaneousDetectionLimit: ptr[uint8](2)}); err != nil {
		t.Fatal(err)
	}
	if err := set(c, def.Patch{SimultaneousDetectionLimit: ptr[uint8](2)}); err != nil { // :689-690 kWriteSuccessNoOp
		t.Errorf("the same limit: %v", err)
	}
	if got := read[uint8](t, c, def.AttrSimultaneousDetectionLimit); got != 2 {
		t.Errorf("SimultaneousDetectionLimit %d", got)
	}
	if read[bool](t, c, def.AttrHumanActivityDetected) { // :696-698: the oldest went
		t.Error("the oldest detection kept")
	}
}

// TestObjectCount ports :703-784.
func TestObjectCount(t *testing.T) {
	clk := newFake()
	c := newACS(t, sensing.AmbientContextConfig{
		Features: allFeatures, ServeObjectCount: true, HoldTimeLimits: limits(10, 200, sensing.DefaultHoldTimeDefault),
		HoldTime: ptr(sensing.DefaultHoldTimeDefault), Clock: clk,
	})
	cfg := sensing.ObjectCountConfig{CountingObject: ptr(oiAdult), ObjectCountThreshold: ptr[uint16](10)}
	if err := set(c, def.Patch{ObjectCountConfig: &cfg}); statusOf(err) != im.StatusConstraintError { // :727-728
		t.Errorf("before the supported list: %v", err)
	}
	sup := supportedArray()
	if err := set(c, def.Patch{AmbientContextTypeSupported: &sup}); err != nil {
		t.Fatal(err)
	}
	if err := c.MatterWrite(context.Background(), def.AttrObjectCountConfig, cfg); err != nil {
		t.Fatal(err)
	}
	got := read[sensing.ObjectCountConfig](t, c, def.AttrObjectCountConfig)
	if got.CountingObject.NamespaceId != oiAdult.NamespaceId || got.CountingObject.Tag != oiAdult.Tag || *got.ObjectCountThreshold != 10 {
		t.Errorf("ObjectCountConfig %+v", got)
	}
	if read[bool](t, c, def.AttrObjectCountThresholdReached) {
		t.Error("reached at start")
	}
	if err := c.SetObjectCount(1); err != nil {
		t.Fatal(err)
	}
	if read[bool](t, c, def.AttrObjectCountThresholdReached) {
		t.Error("reached below the threshold")
	}
	if err := c.SetObjectCount(11); err != nil {
		t.Fatal(err)
	}
	if clk.Pending() == 0 {
		t.Error("no timer armed")
	}
	if !read[bool](t, c, def.AttrObjectCountThresholdReached) || read[uint16](t, c, def.AttrObjectCount) != 11 {
		t.Error("not reached, or the count not 11")
	}
	clk.Advance(time.Duration(sensing.DefaultHoldTimeDefault+1) * time.Second)
	if read[bool](t, c, def.AttrObjectCountThresholdReached) || read[uint16](t, c, def.AttrObjectCount) != 0 {
		t.Error("still reached, or the count not reset, after the hold time")
	}
}

// TestObjCntThresholdPersistence ports :786-859.
func TestObjCntThresholdPersistence(t *testing.T) {
	store := &persisted{}
	build := func() *sensing.AmbientContext {
		store.mu.Lock()
		occ := store.occ
		store.mu.Unlock()
		sup := supportedArray()
		c := newACS(t, sensing.AmbientContextConfig{
			Features: allFeatures, HoldTimeLimits: limits(10, 200, sensing.DefaultHoldTimeDefault), HoldTime: ptr(sensing.DefaultHoldTimeDefault),
			ObjectCountConfig: occ, AmbientContextTypeSupported: sup, Clock: newFake(),
		})
		store.watch(c)
		return c
	}
	c := build()
	if got := read[sensing.ObjectCountConfig](t, c, def.AttrObjectCountConfig); *got.ObjectCountThreshold != sensing.DefaultCountThreshold {
		t.Errorf("threshold %d, want the default", *got.ObjectCountThreshold)
	}
	cfg := sensing.ObjectCountConfig{CountingObject: ptr(oiAdult), ObjectCountThreshold: ptr[uint16](10)}
	if err := c.MatterWrite(context.Background(), def.AttrObjectCountConfig, cfg); err != nil {
		t.Fatal(err)
	}
	if got := read[sensing.ObjectCountConfig](t, c, def.AttrObjectCountConfig); *got.ObjectCountThreshold != 10 {
		t.Errorf("threshold %d", *got.ObjectCountThreshold)
	}
	if got := read[sensing.ObjectCountConfig](t, build(), def.AttrObjectCountConfig); *got.ObjectCountThreshold != 10 {
		t.Errorf("threshold %d after the restart, want the persisted 10", *got.ObjectCountThreshold)
	}
}

// TestPredictActivity ports :861-918.
func TestPredictActivity(t *testing.T) {
	clk := newFake()
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, Clock: clk})
	now := uint32(clk.Now().Unix()) //nolint:gosec // a 2027 timestamp
	tags := []sensing.SemanticTag{haFall, oiAdult, oiChild, audFall}
	pa := []sensing.PredictedActivity{{
		StartTimestamp: ptr(now + 10), EndTimestamp: ptr(now + 20), AmbientContextType: &tags,
		CrowdDetected: ptr(false), CrowdCount: ptr[uint8](1), Confidence: ptr[uint8](100),
	}}
	if err := set(c, def.Patch{PredictedActivity: &pa}); err != nil {
		t.Fatal(err)
	}
	list := read[spec.List[sensing.PredictedActivity]](t, c, def.AttrPredictedActivity)
	if len(list) != 1 {
		t.Fatalf("PredictedActivity %v", list)
	}
	p := list[0]
	if !slices.EqualFunc(*p.AmbientContextType, tags, func(a, b sensing.SemanticTag) bool { return a.NamespaceId == b.NamespaceId && a.Tag == b.Tag }) {
		t.Errorf("tags %v", *p.AmbientContextType)
	}
	if *p.StartTimestamp != now+10 || *p.EndTimestamp != now+20 || *p.CrowdDetected || *p.CrowdCount != 1 || *p.Confidence != 100 {
		t.Errorf("PredictedActivity %+v", p)
	}
}

// mfgTag is a manufacturer tag with an optional label.
func mfgTag(vendor uint16, label *string) *sensing.SemanticTag {
	s := sensing.SemanticTag{MfgCode: spec.ValueOf(vendor), NamespaceId: sensing.NamespaceIdentifiedObject, Tag: oiAdult.Tag}
	if label != nil {
		l := spec.ValueOf(*label)
		s.Label = &l
	}
	return &s
}

// The TestVendor ids (chip VendorId::TestVendor1, TestVendor2).
const (
	testVendor1 uint16 = 0xFFF1
	testVendor2 uint16 = 0xFFF2
)

// TestObjectCountConfigLabelOutlivesRequestBuffer ports :922-984: the
// label a controller wrote reads back as written (chip's aliasing of the
// request buffer has no Go counterpart; the read back is the contract).
func TestObjectCountConfigLabelOutlivesRequestBuffer(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, Clock: newFake()})
	label := strings.Repeat("0123456789abcdef", 4)
	cfg := sensing.ObjectCountConfig{CountingObject: mfgTag(testVendor1, &label), ObjectCountThreshold: ptr[uint16](5)}
	if err := c.MatterWrite(context.Background(), def.AttrObjectCountConfig, cfg); err != nil {
		t.Fatal(err)
	}
	got := read[sensing.ObjectCountConfig](t, c, def.AttrObjectCountConfig)
	if got.CountingObject.Label == nil || got.CountingObject.Label.Null || got.CountingObject.Label.Value != label {
		t.Errorf("label %+v", got.CountingObject.Label)
	}
}

// TestObjectCountConfigRejectsOverlongLabel ports :987-1008.
func TestObjectCountConfigRejectsOverlongLabel(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, Clock: newFake()})
	label := strings.Repeat("a", 200)
	cfg := sensing.ObjectCountConfig{CountingObject: mfgTag(testVendor1, &label), ObjectCountThreshold: ptr[uint16](5)}
	if err := set(c, def.Patch{ObjectCountConfig: &cfg}); statusOf(err) != im.StatusConstraintError {
		t.Errorf("a 200-character label: %v", err)
	}
}

// TestObjectCountConfigRequiresLabelWithMfgCode ports :1011-1029.
func TestObjectCountConfigRequiresLabelWithMfgCode(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, Clock: newFake()})
	cfg := sensing.ObjectCountConfig{CountingObject: mfgTag(testVendor1, nil), ObjectCountThreshold: ptr[uint16](5)}
	if err := set(c, def.Patch{ObjectCountConfig: &cfg}); statusOf(err) != im.StatusConstraintError {
		t.Errorf("a MfgCode without a label: %v", err)
	}
}

// TestObjectCountConfigMfgCodeOnlyChangeIsStored ports :1033-1063.
func TestObjectCountConfigMfgCodeOnlyChangeIsStored(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, Clock: newFake()})
	label := "counted"
	cfg := sensing.ObjectCountConfig{CountingObject: mfgTag(testVendor1, &label), ObjectCountThreshold: ptr[uint16](5)}
	if err := set(c, def.Patch{ObjectCountConfig: &cfg}); err != nil {
		t.Fatal(err)
	}
	cfg.CountingObject = mfgTag(testVendor2, &label)
	if err := set(c, def.Patch{ObjectCountConfig: &cfg}); err != nil {
		t.Fatal(err)
	}
	got := read[sensing.ObjectCountConfig](t, c, def.AttrObjectCountConfig)
	if got.CountingObject.MfgCode.Null || got.CountingObject.MfgCode.Value != testVendor2 {
		t.Errorf("MfgCode %+v", got.CountingObject.MfgCode)
	}
}

// TestObjectCountConfigLabelOnlyChangeIsStored ports :1066-1098.
func TestObjectCountConfigLabelOnlyChangeIsStored(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, Clock: newFake()})
	first, second := "first", "second"
	cfg := sensing.ObjectCountConfig{CountingObject: mfgTag(testVendor1, &first), ObjectCountThreshold: ptr[uint16](5)}
	if err := set(c, def.Patch{ObjectCountConfig: &cfg}); err != nil {
		t.Fatal(err)
	}
	cfg.CountingObject = mfgTag(testVendor1, &second)
	if err := set(c, def.Patch{ObjectCountConfig: &cfg}); err != nil {
		t.Fatal(err)
	}
	got := read[sensing.ObjectCountConfig](t, c, def.AttrObjectCountConfig)
	if got.CountingObject.Label == nil || got.CountingObject.Label.Value != second {
		t.Errorf("label %+v", got.CountingObject.Label)
	}
}

// TestSensorFusionSupported stands for :1100-1151: chip's
// SensorFusionSupported (0x000C) and its feature are not in the matter.js
// model at the pin, so the attribute is not served.
func TestSensorFusionSupported(t *testing.T) {
	c := newACS(t, sensing.AmbientContextConfig{Features: allFeatures, Clock: newFake()})
	if _, ok := c.MatterRead(0x000C); ok {
		t.Error("SensorFusionSupported served")
	}
	if slices.Contains(c.MatterAttributes(), 0x000C) {
		t.Error("SensorFusionSupported listed")
	}
}
