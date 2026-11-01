// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sensing

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	def "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// AmbientContextClusterID is the AmbientContextSensing cluster id.
const AmbientContextClusterID = def.ClusterID

// Generated types the server speaks.
type (
	// AmbientContextFeature is a FeatureMap bit.
	AmbientContextFeature = def.Feature
	// SemanticTag is a semantic tag (Globals SemanticTagStruct).
	SemanticTag = def.Semtag
	// Detection is one ambient context detected: up to two tags.
	Detection = def.AmbientContextTypeStruct
	// ObjectCountConfig is the object to count and its threshold.
	ObjectCountConfig = def.ObjectCountConfigStruct
	// HoldTimeLimits bounds HoldTime.
	HoldTimeLimits = def.HoldTimeLimitsStruct
	// PredictedActivity is one predicted activity window.
	PredictedActivity = def.PredictedActivityStruct
	// Namespace is a semantic tag namespace.
	Namespace = def.Namespace
)

// Features (HA, OC, OI and AUD: at least one; PRED optional).
const (
	FeatureHumanActivity        = def.FeatureHumanActivity
	FeatureObjectCounting       = def.FeatureObjectCounting
	FeatureObjectIdentification = def.FeatureObjectIdentification
	FeatureSoundIdentification  = def.FeatureSoundIdentification
	FeaturePredictedActivity    = def.FeaturePredictedActivity
)

// The namespaces the cluster detects in (chip
// ambient-context-sensing-namespace.h:27, :48, :78).
const (
	NamespaceIdentifiedObject        = def.NamespaceIdentifiedObject
	NamespaceIdentifiedSound         = def.NamespaceIdentifiedSound
	NamespaceIdentifiedHumanActivity = def.NamespaceIdentifiedHumanActivity
)

// chip's defaults and bounds (AmbientContextSensingDelegate.h:30-46).
const (
	// DefaultSimultaneousDetectionLimit, DefaultCountThreshold and the
	// hold-time defaults are chip's vendor defaults (:30-34).
	DefaultSimultaneousDetectionLimit uint8  = 5
	DefaultCountThreshold             uint16 = 5
	DefaultHoldTimeMin                uint16 = 10
	DefaultHoldTimeMax                uint16 = 300
	DefaultHoldTimeDefault            uint16 = 50

	maxACSensed                   = 2   // :37
	maxACTypeSupported            = 50  // :38
	maxSimultaneousDetectionLimit = 10  // :39
	minObjectCount                = 1   // :40
	maxSemanticTagLabelLength     = 64  // :41
	maxPredictedACType            = 100 // :42
	maxPredictedActivity          = 20  // :43
	minCrowdCount                 = 1   // :45
	maxCrowdCount                 = 254 // :46
)

// The tag counts of chip's namespace tables (namespace.h:29-45, :50-75,
// :80-93; AmbientContextSensingCluster.cpp:683-707): a tag beyond them
// counts as Unknown (tag 0) in the duplicate check, as TagToIndex returns
// 0 for a tag it does not find (:709-723).
var namespaceTagCount = map[Namespace]uint8{
	NamespaceIdentifiedObject:        13,
	NamespaceIdentifiedSound:         22,
	NamespaceIdentifiedHumanActivity: 10,
}

// Errors of the host's inputs.
var (
	// ErrAmbientContextConfig reports a configuration the cluster's model
	// refuses.
	ErrAmbientContextConfig = errors.New("sensing: AmbientContextSensing configuration outside the cluster's model")
	// ErrNotSupported is chip's CHIP_ERROR_INCORRECT_STATE: an input the
	// feature selection or the supported list does not cover.
	ErrNotSupported = errors.New("sensing: not supported by the AmbientContextSensing selection")
)

// AmbientContextConfig carries the construction parameters of an
// [AmbientContext]; the attribute-named fields are the values the host
// persisted or fixed (the declaration's Initial).
type AmbientContextConfig struct {
	// Features is the FeatureMap: at least one of HA, OC, OI and AUD
	// (chip AmbientContextSensingCluster.cpp:52-54).
	Features AmbientContextFeature
	// ServeObjectCount serves the optional ObjectCount attribute (OC & OI;
	// chip's OptionalAttributeSet, .h:76).
	ServeObjectCount bool
	// HoldTimeLimits is fixed; nil is chip's {10, 300, 50}
	// (.h:68-72). It is sanitised as chip's SetHoldTimeLimits does
	// (.cpp:438-466).
	HoldTimeLimits *HoldTimeLimits
	// HoldTime is the value the host persisted; nil starts at
	// HoldTimeLimits.HoldTimeDefault. A value outside the limits falls
	// back to the default, as chip's Startup replaces a stored value its
	// SetHoldTime refuses (.cpp:74-85).
	HoldTime *uint16
	// ObjectCountConfig is the value the host persisted (OC & OI); nil
	// is chip's {IdentifiedObject, Unknown, threshold 5} (.h:60-66).
	ObjectCountConfig *ObjectCountConfig
	// SimultaneousDetectionLimit is fixed; zero is chip's 5 (.h:153).
	SimultaneousDetectionLimit uint8
	// AmbientContextTypeSupported is the supported list, checked as
	// [AmbientContext.SetAttributes] checks it; nil starts empty, as chip
	// does until SetAmbientContextTypeSupported.
	AmbientContextTypeSupported []SemanticTag
	// Clock is the device's clock: the hold-time expiry and the event
	// timestamps; nil is the system clock.
	Clock clock.Clock
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// detection is one entry of chip's mAmbientContextTypeList
// (AmbientContextSensed, AmbientContextSensingDelegate.h:48-56).
type detection struct {
	info       Detection
	start, end time.Time
	startEpoch uint64 // posix-ms
}

// acState is the server's state; mutations work on a copy and commit it
// whole.
type acState struct {
	supported  []SemanticTag
	detections []detection // the newest first, as chip's PushFront
	predicted  []PredictedActivity
	sdl        uint8
	holdTime   uint16
	limits     [3]uint16 // min, max, default
	occ        ObjectCountConfig
	count      uint16
	countStart time.Time
	countEnd   time.Time
	countEpoch uint64
	// The detection attributes (UpdateDetectionAttributes).
	human, object, audio, reached bool
}

func (st acState) clone() acState {
	st.supported = slices.Clone(st.supported)
	st.detections = slices.Clone(st.detections)
	st.predicted = slices.Clone(st.predicted)
	return st
}

// event is an event a mutation emits once it is committed.
type event struct {
	id   uint32
	data any
}

// AmbientContext implements [contract.ClusterServer] for
// AmbientContextSensing: the detections with their hold time on the
// device's clock, the detection attributes derived from them, the object
// count against its threshold, the supported list and the predicted
// activity — chip's AmbientContextSensingCluster.cpp at 6170af84. HoldTime
// and ObjectCountConfig accept a controller's write.
type AmbientContext struct {
	*spec.Instance
	spec.SelfHeld

	srv   *spec.Server
	clock clock.Clock
	boot  time.Time // the systime epoch of EventStartTimeSys

	// mu guards endpoint, st, timer and gen.
	mu       sync.Mutex
	endpoint uint16
	st       acState
	timer    clock.Timer
	gen      uint64
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*AmbientContext)(nil)
	_ contract.ClusterQuiescer                = (*AmbientContext)(nil)
	_ contract.ClusterDataVersion             = (*AmbientContext)(nil)
	_ contract.ClusterAttributeLister         = (*AmbientContext)(nil)
	_ contract.ClusterEventLister             = (*AmbientContext)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*AmbientContext)(nil)
	_ contract.AttributeChangeNotifier        = (*AmbientContext)(nil)
	_ contract.EventReceiver                  = (*AmbientContext)(nil)
	_ spec.AttributeSetter                    = (*AmbientContext)(nil)
	_ spec.EventEmitting                      = (*AmbientContext)(nil)
	_ def.Host                                = (*AmbientContext)(nil)
)

// sourced are the attributes the server answers from its state; HoldTime
// and ObjectCountConfig, which a controller writes, are the generated
// server's stored values, kept equal to the state.
var sourced = []uint32{
	def.AttrHumanActivityDetected, def.AttrObjectIdentified, def.AttrAudioContextDetected,
	def.AttrAmbientContextType, def.AttrAmbientContextTypeSupported, def.AttrObjectCountThresholdReached,
	def.AttrObjectCount, def.AttrSimultaneousDetectionLimit, def.AttrHoldTimeLimits, def.AttrPredictedActivity,
}

// NewAmbientContext builds an AmbientContextSensing server. Mirrors chip
// AmbientContextSensingCluster.cpp:48-89 (the constructor and Startup) and
// :161-188 (the attribute list, which the generated definition's
// conformance yields).
func NewAmbientContext(cfg AmbientContextConfig) (*AmbientContext, error) {
	f := cfg.Features
	if f&(FeatureHumanActivity|FeatureObjectCounting|FeatureObjectIdentification|FeatureSoundIdentification) == 0 { // :52-54
		return nil, fmt.Errorf("%w: none of HA, OC, OI, AUD", ErrAmbientContextConfig)
	}
	opts := spec.Options{Features: uint32(f), Attributes: servedAttributes(f, cfg.ServeObjectCount)}
	counting := f&FeatureObjectCounting != 0 && f&FeatureObjectIdentification != 0
	if f&(FeatureHumanActivity|FeatureObjectIdentification|FeatureSoundIdentification) != 0 || counting {
		opts.Events = []uint32{def.EventAmbientContextDetectStarted, def.EventAmbientContextDetectEnded}
	}
	c := &AmbientContext{clock: clock.Or(cfg.Clock)}
	c.boot = c.clock.Now()
	st := acState{sdl: DefaultSimultaneousDetectionLimit, occ: defaultObjectCountConfig()}
	st.limits = [3]uint16{DefaultHoldTimeMin, DefaultHoldTimeMax, DefaultHoldTimeDefault}
	if cfg.HoldTimeLimits != nil {
		st.limits = sanitizeLimits(*cfg.HoldTimeLimits) // :55
	}
	st.holdTime = st.limits[2]
	if cfg.HoldTime != nil && *cfg.HoldTime >= st.limits[0] && *cfg.HoldTime <= st.limits[1] { // :56, :74-85
		st.holdTime = *cfg.HoldTime
	}
	if cfg.SimultaneousDetectionLimit != 0 {
		if err := checkSDL(cfg.SimultaneousDetectionLimit); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAmbientContextConfig, err)
		}
		st.sdl = cfg.SimultaneousDetectionLimit
	}
	c.st = st
	srv, err := spec.NewServer(def.Definition, opts, spec.ServerConfig{DataVersion: cfg.DataVersion, Source: acSource{c}, Sink: acSink{c}})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAmbientContextConfig, err)
	}
	c.srv, c.Instance, c.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	if cfg.AmbientContextTypeSupported != nil {
		if err := c.checkSupported(cfg.AmbientContextTypeSupported); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAmbientContextConfig, err)
		}
		c.st.supported = slices.Clone(cfg.AmbientContextTypeSupported)
	}
	if cfg.ObjectCountConfig != nil && counting {
		if _, err := srv.CheckSet(def.AttrObjectCountConfig, *cfg.ObjectCountConfig, nil); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAmbientContextConfig, err)
		}
		if err := checkLabel(*cfg.ObjectCountConfig); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrAmbientContextConfig, err)
		}
		c.st.occ = *cfg.ObjectCountConfig
	}
	if err := srv.SetAttributes(c.storedValues(c.st)); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAmbientContextConfig, err)
	}
	return c, nil
}

// servedAttributes is chip's attribute list (.cpp:161-188): every element
// of the cluster is provisional, so the model makes each optional ("P, M"
// is optional, matter.js Conformance.ts computeApplicability) and the
// server opts in what chip serves for the feature selection — the three
// mandatory ones, the detection attributes of HA, OI and AUD, the counting
// ones of OC & OI (ObjectCount when asked for), PredictedActivity of PRED.
func servedAttributes(f AmbientContextFeature, objectCount bool) []uint32 {
	ha, oi, aud := f&FeatureHumanActivity != 0, f&FeatureObjectIdentification != 0, f&FeatureSoundIdentification != 0
	counting := f&FeatureObjectCounting != 0 && oi
	attrs := []uint32{def.AttrSimultaneousDetectionLimit, def.AttrHoldTime, def.AttrHoldTimeLimits}
	for _, a := range []struct {
		on bool
		id uint32
	}{
		{ha, def.AttrHumanActivityDetected},
		{oi, def.AttrObjectIdentified},
		{aud, def.AttrAudioContextDetected},
		{ha || oi || aud, def.AttrAmbientContextType},
		{ha || oi || aud, def.AttrAmbientContextTypeSupported},
		{counting, def.AttrObjectCountThresholdReached},
		{counting, def.AttrObjectCountConfig},
		{counting && objectCount, def.AttrObjectCount},
		{f&FeaturePredictedActivity != 0, def.AttrPredictedActivity},
	} {
		if a.on {
			attrs = append(attrs, a.id)
		}
	}
	return attrs
}

// defaultObjectCountConfig is chip's mObjectCountConfig default (.h:60-66,
// :155-161): the Unknown identified object, threshold 5.
func defaultObjectCountConfig() ObjectCountConfig {
	threshold := DefaultCountThreshold
	return ObjectCountConfig{
		CountingObject:       &SemanticTag{MfgCode: spec.NullOf[uint16](), NamespaceId: NamespaceIdentifiedObject},
		ObjectCountThreshold: &threshold,
	}
}

// sanitizeLimits is chip's SetHoldTimeLimits sanitising (.cpp:443-449):
// min at least 1, max at least max(10, min), default clamped into them.
func sanitizeLimits(l HoldTimeLimits) [3]uint16 {
	lo, hi, dflt := deref(l.HoldTimeMin), deref(l.HoldTimeMax), deref(l.HoldTimeDefault)
	lo = max(1, lo)
	hi = max(10, lo, hi)
	dflt = min(max(dflt, lo), hi)
	return [3]uint16{lo, hi, dflt}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// storedValues are the attributes the generated server stores: HoldTime
// and, when served, ObjectCountConfig.
func (c *AmbientContext) storedValues(st acState) map[uint32]any {
	v := map[uint32]any{def.AttrHoldTime: st.holdTime}
	if c.Serves(def.AttrObjectCountConfig) {
		v[def.AttrObjectCountConfig] = st.occ
	}
	return v
}

// attr is the value of a sourced attribute in st. Mirrors chip
// ReadAttribute (.cpp:100-138) with ReadAmbientContextType (:543-556),
// ReadAmbientContextTypeSupported (:529-541) and ReadPredictedActivity
// (:894-904).
func attr(st *acState, id uint32) any {
	switch id {
	case def.AttrHumanActivityDetected:
		return st.human
	case def.AttrObjectIdentified:
		return st.object
	case def.AttrAudioContextDetected:
		return st.audio
	case def.AttrAmbientContextType:
		list := spec.List[Detection]{}
		if len(st.supported) > 0 { // :547
			for _, d := range st.detections {
				list = append(list, d.info)
			}
		}
		return list
	case def.AttrAmbientContextTypeSupported:
		return spec.List[SemanticTag](slices.Clone(st.supported))
	case def.AttrObjectCountThresholdReached:
		return st.reached
	case def.AttrObjectCount:
		return st.count
	case def.AttrSimultaneousDetectionLimit:
		return st.sdl
	case def.AttrHoldTimeLimits:
		lo, hi, dflt := st.limits[0], st.limits[1], st.limits[2]
		return HoldTimeLimits{HoldTimeMin: &lo, HoldTimeMax: &hi, HoldTimeDefault: &dflt}
	case def.AttrPredictedActivity:
		return spec.List[PredictedActivity](slices.Clone(st.predicted))
	}
	return nil
}

// acSource answers the sourced attributes from the state.
type acSource struct{ c *AmbientContext }

// MatterAttribute implements [spec.Source].
func (s acSource) MatterAttribute(attrID uint32) (any, bool) {
	if !slices.Contains(sourced, attrID) {
		return nil, false
	}
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	return attr(&s.c.st, attrID), true
}

// acSink carries out a controller's write of ObjectCountConfig or
// HoldTime after the definition's checks; the generated server stores the
// value and reports it. Mirrors chip WriteAttribute (.cpp:140-159).
type acSink struct{ c *AmbientContext }

// MatterWriteAttribute implements [spec.Sink].
func (k acSink) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	c := k.c
	c.mu.Lock()
	defer c.mu.Unlock()
	switch attrID {
	case def.AttrObjectCountConfig: // :145-149
		cfg, _ := value.(ObjectCountConfig)
		if err := c.checkObjectCountConfig(&c.st, cfg); err != nil {
			return err
		}
		c.st.occ = cfg
	case def.AttrHoldTime: // :150-155
		h, _ := value.(uint16)
		if h == c.st.holdTime { // :153: the same value is a success
			return nil
		}
		if err := checkHoldTime(&c.st, h); err != nil {
			return err
		}
		c.st.holdTime = h
	}
	return nil
}

// constraint answers a write, or a host input, with CONSTRAINT_ERROR.
func constraint(format string, args ...any) error {
	return spec.Errorf(im.StatusConstraintError, "AmbientContextSensing: "+format, args...)
}

// checkHoldTime is chip's SetHoldTime bound (.cpp:420-423).
func checkHoldTime(st *acState, h uint16) error {
	if h < st.limits[0] || h > st.limits[1] {
		return constraint("HoldTime %d outside %d..%d", h, st.limits[0], st.limits[1])
	}
	return nil
}

// checkObjectCountConfig is chip's SetObjectCountConfig (.cpp:303-339):
// the threshold at least 1; a standard tag in the IdentifiedObject
// namespace and on the supported list; a manufacturer tag with a label; a
// label of at most 64 characters.
func (c *AmbientContext) checkObjectCountConfig(st *acState, cfg ObjectCountConfig) error {
	if deref(cfg.ObjectCountThreshold) < minObjectCount { // :306
		return constraint("ObjectCountThreshold below %d", minObjectCount)
	}
	tag := deref(cfg.CountingObject)
	if tag.MfgCode.Null { // :309-325
		if tag.NamespaceId != NamespaceIdentifiedObject {
			return constraint("CountingObject namespace %d is not IdentifiedObject", tag.NamespaceId)
		}
		if !slices.ContainsFunc(st.supported, func(s SemanticTag) bool { return s.Tag == tag.Tag && s.NamespaceId == tag.NamespaceId }) {
			return constraint("CountingObject tag %d is not in AmbientContextTypeSupported", tag.Tag)
		}
	}
	return checkLabel(cfg)
}

// checkLabel is chip's label rule (.cpp:327-339): a manufacturer tag has
// a label, and a label is at most 64 characters.
func checkLabel(cfg ObjectCountConfig) error {
	tag := deref(cfg.CountingObject)
	if !tag.MfgCode.Null && tag.Label == nil { // :330
		return constraint("CountingObject with a MfgCode needs a Label")
	}
	if tag.Label != nil && !tag.Label.Null && len(tag.Label.Value) > maxSemanticTagLabelLength { // :338
		return constraint("CountingObject Label longer than %d", maxSemanticTagLabelLength)
	}
	return nil
}

// checkSDL is chip's SetSimultaneousDetectionLimit bound (.cpp:389) with
// the model's minimum ("1 to 10").
func checkSDL(n uint8) error {
	if n < 1 || n > maxSimultaneousDetectionLimit {
		return constraint("SimultaneousDetectionLimit %d outside 1..%d", n, maxSimultaneousDetectionLimit)
	}
	return nil
}

// invalid is chip's CHIP_ERROR_INVALID_ARGUMENT for a host input.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: sensing: "+format, append([]any{spec.ErrInvalidValue}, args...)...)
}

// checkSupported is chip's SetAmbientContextTypeSupported check
// (.cpp:190-194) with CheckInputSupportedType (:731-778): every tag in a
// namespace whose feature is selected, none twice, 1 to 50 of them.
func (c *AmbientContext) checkSupported(list []SemanticTag) error {
	seen := map[[2]uint8]bool{}
	for _, t := range list {
		enabled := t.NamespaceId == NamespaceIdentifiedHumanActivity && c.HasFeature("HA") ||
			t.NamespaceId == NamespaceIdentifiedObject && c.HasFeature("OI") ||
			t.NamespaceId == NamespaceIdentifiedSound && c.HasFeature("AUD")
		if !enabled { // :748
			return invalid("AmbientContextTypeSupported tag %d/%d outside the selected features", t.NamespaceId, t.Tag)
		}
		idx := t.Tag
		if idx >= namespaceTagCount[t.NamespaceId] { // TagToIndex :721-722
			idx = 0
		}
		key := [2]uint8{uint8(t.NamespaceId), idx}
		if seen[key] { // :775
			return invalid("AmbientContextTypeSupported tag %d/%d twice", t.NamespaceId, t.Tag)
		}
		seen[key] = true
	}
	if len(list) == 0 || len(list) > maxACTypeSupported { // :194
		return invalid("AmbientContextTypeSupported of %d tags, want 1..%d", len(list), maxACTypeSupported)
	}
	return nil
}

// checkPredicted is chip's SetPredictedActivity bound (.cpp:470) and
// CheckPredictedActivity (:844-892): windows in order, each start before
// its end and after the previous end; tags only in selected namespaces;
// a crowd count in 1..254 under OC.
func (c *AmbientContext) checkPredicted(list []PredictedActivity) error {
	if len(list) > maxPredictedActivity {
		return invalid("PredictedActivity of %d entries, want at most %d", len(list), maxPredictedActivity)
	}
	var last uint32
	for i, p := range list {
		start, end := deref(p.StartTimestamp), deref(p.EndTimestamp)
		if start >= end { // :853
			return invalid("PredictedActivity %d starts at %d, not before its end %d", i, start, end)
		}
		if last >= start { // :854
			return invalid("PredictedActivity %d starts at %d, not after the previous end %d", i, start, last)
		}
		last = end
		if p.AmbientContextType != nil {
			tags := *p.AmbientContextType
			if len(tags) > maxPredictedACType { // :860
				return invalid("PredictedActivity %d has %d tags", i, len(tags))
			}
			for _, t := range tags { // :861-877
				var ok bool
				switch t.NamespaceId {
				case NamespaceIdentifiedObject:
					ok = c.HasFeature("OI")
				case NamespaceIdentifiedSound:
					ok = c.HasFeature("AUD")
				case NamespaceIdentifiedHumanActivity:
					ok = c.HasFeature("HA")
				default: // :874-875
				}
				if !ok {
					return invalid("PredictedActivity %d tag %d/%d outside the selected features", i, t.NamespaceId, t.Tag)
				}
			}
		}
		if c.HasFeature("OC") && p.CrowdCount != nil { // :880-888
			if n := *p.CrowdCount; n < minCrowdCount || n > maxCrowdCount {
				return invalid("PredictedActivity %d CrowdCount %d outside %d..%d", i, n, minCrowdCount, maxCrowdCount)
			}
		}
	}
	return nil
}

// errStale ends a mutation that has nothing to do (a superseded timer).
var errStale = errors.New("sensing: stale")

// mutate runs fn on a copy of the state, commits it, re-arms the hold-time
// timer (chip's UpdateEventTimeout, :649-681) and then reports the
// attributes that changed and emits the events fn collected, in order.
func (c *AmbientContext) mutate(fn func(st *acState, events *[]event) error) error {
	c.mu.Lock()
	before := c.st
	next := c.st.clone()
	var events []event
	if err := fn(&next, &events); err != nil {
		c.mu.Unlock()
		if errors.Is(err, errStale) {
			return nil
		}
		return err
	}
	c.st = next
	c.rearmLocked()
	endpoint := c.endpoint
	var marked []uint32
	for _, id := range sourced {
		if c.Serves(id) && !reflect.DeepEqual(attr(&before, id), attr(&next, id)) {
			marked = append(marked, id)
		}
	}
	stored := c.storedValues(next)
	c.mu.Unlock()

	_ = c.srv.SetAttributes(stored) // equal values report nothing
	if len(marked) > 0 {
		_ = c.srv.MarkChanged(marked...)
	}
	for _, e := range events {
		if c.Emits(e.id) {
			_ = c.srv.Emit(endpoint, e.id, e.data)
		}
	}
	return nil
}

// rearmLocked arms the timer at the earliest end of a detection or of the
// object count, or stops it when there is none (UpdateEventTimeout,
// :649-681; FindEarliestEndTimestamp, :822-842).
func (c *AmbientContext) rearmLocked() {
	c.gen++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	var earliest time.Time
	for _, d := range c.st.detections {
		if earliest.IsZero() || d.end.Before(earliest) {
			earliest = d.end
		}
	}
	if c.st.count > 0 && (earliest.IsZero() || c.st.countEnd.Before(earliest)) {
		earliest = c.st.countEnd
	}
	if earliest.IsZero() {
		return
	}
	gen := c.gen
	c.timer = c.clock.AfterFunc(max(earliest.Sub(c.clock.Now()), 0), func() { c.timerFired(gen) })
}

// timerFired removes the expired detections and the expired object count
// and updates the detection attributes. Mirrors chip TimerFired
// (.cpp:498-509) and RemoveExpiredItems (:792-820).
func (c *AmbientContext) timerFired(gen uint64) {
	_ = c.mutate(func(st *acState, events *[]event) error {
		if gen != c.gen || (len(st.detections) == 0 && st.count == 0) { // :500
			return errStale
		}
		now := c.clock.Now()
		kept := st.detections[:0]
		for _, d := range st.detections {
			if d.end.After(now) {
				kept = append(kept, d)
				continue
			}
			*events = append(*events, c.endEvent(d.startEpoch, d.start)) // :807
		}
		st.detections = kept
		if st.count > 0 && !st.countEnd.After(now) { // :812-819
			st.count = 0
			*events = append(*events, c.endEvent(st.countEpoch, st.countStart))
			st.countStart, st.countEnd = time.Time{}, time.Time{}
		}
		updateDetections(st)
		return nil
	})
}

// endEvent is AmbientContextDetectEnded (chip SendDetectEndEvent,
// :584-591): the start as posix-ms and as systime-ms.
func (c *AmbientContext) endEvent(epoch uint64, start time.Time) event {
	sys := uint64(max(start.Sub(c.boot).Milliseconds(), 0)) //nolint:gosec // non-negative, systime-ms
	return event{def.EventAmbientContextDetectEnded, def.AmbientContextDetectEndedEvent{EventStartTimePos: &epoch, EventStartTimeSys: &sys}}
}

// epochMS is the posix-ms of t (chip's delegate GetEpochNow).
func epochMS(t time.Time) uint64 { return uint64(max(t.UnixMilli(), 0)) } //nolint:gosec // non-negative

// updateDetections derives the four detection attributes from the
// detections and the object count. Mirrors chip UpdateDetectionAttributes
// (.cpp:600-646).
func updateDetections(st *acState) {
	st.human, st.object, st.audio = false, false, false
	for _, d := range st.detections {
		for _, t := range deref(d.info.AmbientContextSensed) {
			switch t.NamespaceId {
			case NamespaceIdentifiedHumanActivity:
				st.human = true
			case NamespaceIdentifiedObject:
				st.object = true
			case NamespaceIdentifiedSound:
				st.audio = true
			default: // another namespace detects nothing here
			}
		}
	}
	st.reached = st.count >= deref(st.occ.ObjectCountThreshold) // :636-639
}

// sameDetection is chip's CompareAmbientContextSensed (.cpp:511-527): the
// same namespaces and tags in the same order.
func sameDetection(a, b Detection) bool {
	x, y := deref(a.AmbientContextSensed), deref(b.AmbientContextSensed)
	return slices.EqualFunc(x, y, func(p, q SemanticTag) bool { return p.NamespaceId == q.NamespaceId && p.Tag == q.Tag })
}

// AddDetection records a detection the device made: one or two tags, each
// on the supported list. A new detection is held for HoldTime and emits
// AmbientContextDetectStarted; the same detection again moves to the front
// and keeps its start, its end the start plus HoldTime or plus what is
// left of HoldTimeLimits.HoldTimeMax, whichever is less (chip adds the
// hold to the start, :284, so it does not outlast the first hold). At
// SimultaneousDetectionLimit the oldest detections make room. Mirrors chip
// AddDetection (.cpp:205-301) with IsSupportedEvent (:780-790) and
// SendDetectStartEvent (:558-564).
func (c *AmbientContext) AddDetection(d Detection) error {
	tags := deref(d.AmbientContextSensed)
	if len(tags) == 0 || len(tags) > maxACSensed { // :208
		return invalid("a detection of %d tags, want 1..%d", len(tags), maxACSensed)
	}
	return c.mutate(func(st *acState, events *[]event) error {
		for _, t := range tags { // :209, :780-790
			if !slices.ContainsFunc(st.supported, func(s SemanticTag) bool { return s.NamespaceId == t.NamespaceId && s.Tag == t.Tag }) {
				return fmt.Errorf("%w: tag %d/%d is not in AmbientContextTypeSupported", ErrNotSupported, t.NamespaceId, t.Tag)
			}
		}
		if n := len(st.detections); n >= int(st.sdl) { // :214-233: the oldest go, without an end event
			st.detections = st.detections[:int(st.sdl)-1]
		}
		var item detection
		existing := slices.IndexFunc(st.detections, func(x detection) bool { return sameDetection(x.info, d) })
		now := c.clock.Now()
		var hold time.Duration
		if existing < 0 { // :251-267
			info := d
			cl := slices.Clone(tags)
			info.AmbientContextSensed = &cl
			item = detection{info: info, start: now, startEpoch: epochMS(now)}
			hold = time.Duration(st.holdTime) * time.Second
		} else { // :268-283
			item = st.detections[existing]
			st.detections = slices.Delete(st.detections, existing, existing+1)
			elapsed := now.Sub(item.start).Truncate(time.Second)
			ht, limit := time.Duration(st.holdTime)*time.Second, time.Duration(st.limits[1])*time.Second
			switch {
			case elapsed+ht <= limit:
				hold = ht
			case elapsed < limit:
				hold = limit - elapsed
			}
		}
		item.end = item.start.Add(hold) // :284
		if existing < 0 {               // :285-288
			info := item.info
			*events = append(*events, event{def.EventAmbientContextDetectStarted, def.AmbientContextDetectStartedEvent{AmbientContextDetected: &info}})
		}
		st.detections = slices.Insert(st.detections, 0, item) // :289
		updateDetections(st)
		return nil
	})
}

// SetObjectCount records the device's object count (OC & OI), at least 1:
// it is held for HoldTime, ObjectCountThresholdReached follows the
// threshold, and AmbientContextDetectStarted carries the count; the same
// count again changes nothing. Mirrors chip SetObjectCount (.cpp:369-385)
// and SendDetectStartEvent (:566-582).
func (c *AmbientContext) SetObjectCount(count uint16) error {
	if !c.HasFeature("OC") || !c.HasFeature("OI") { // :371
		return fmt.Errorf("%w: ObjectCount needs OC and OI", ErrNotSupported)
	}
	if count < minObjectCount { // :373
		return invalid("ObjectCount %d below %d", count, minObjectCount)
	}
	return c.mutate(func(st *acState, events *[]event) error {
		if st.count == count { // :374
			return errStale
		}
		now := c.clock.Now()
		st.count, st.countStart, st.countEnd, st.countEpoch = count, now, now.Add(time.Duration(st.holdTime)*time.Second), epochMS(now)
		updateDetections(st)
		tag := deref(st.occ.CountingObject)
		tags := []SemanticTag{{MfgCode: spec.NullOf[uint16](), NamespaceId: tag.NamespaceId, Tag: tag.Tag}}
		reached := st.reached
		*events = append(*events, event{def.EventAmbientContextDetectStarted, def.AmbientContextDetectStartedEvent{
			AmbientContextDetected: &Detection{AmbientContextSensed: &tags}, ObjectCountThresholdReached: &reached, ObjectCount: &count,
		}})
		return nil
	})
}

// SetAttributes implements [spec.AttributeSetter]: the host's change of
// the configuration attributes, each under chip's rule — HoldTimeLimits
// sanitised and HoldTime reset to its default when outside them
// (SetHoldTimeLimits, .cpp:438-466), HoldTime within the limits
// (SetHoldTime, :420-436), AmbientContextTypeSupported (:190-203),
// ObjectCountConfig (:303-367), SimultaneousDetectionLimit with the
// newest detections kept (:387-418) and PredictedActivity
// (:468-478). The detections and the attributes derived from them are the
// server's ([spec.ErrNotHeld]); ObjectCount is [AmbientContext.SetObjectCount].
// All or nothing, in that order.
func (c *AmbientContext) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		if !c.Serves(id) {
			return fmt.Errorf("%w: AmbientContextSensing 0x%04X", spec.ErrNotServed, id)
		}
		switch id {
		case def.AttrHoldTimeLimits, def.AttrHoldTime, def.AttrAmbientContextTypeSupported,
			def.AttrObjectCountConfig, def.AttrSimultaneousDetectionLimit, def.AttrPredictedActivity:
		default:
			return fmt.Errorf("%w: sensing: AmbientContextSensing 0x%04X is the server's", spec.ErrNotHeld, id)
		}
	}
	return c.mutate(func(st *acState, _ *[]event) error {
		for _, id := range []uint32{
			def.AttrHoldTimeLimits, def.AttrHoldTime, def.AttrAmbientContextTypeSupported,
			def.AttrObjectCountConfig, def.AttrSimultaneousDetectionLimit, def.AttrPredictedActivity,
		} {
			v, ok := values[id]
			if !ok {
				continue
			}
			if err := c.set(st, id, v); err != nil {
				return err
			}
		}
		return nil
	})
}

// set applies one configuration attribute to st.
func (c *AmbientContext) set(st *acState, id uint32, v any) error {
	host := func(err error) error { return fmt.Errorf("%w: %w", spec.ErrInvalidValue, err) }
	switch id {
	case def.AttrHoldTimeLimits:
		l, ok := v.(HoldTimeLimits)
		if !ok {
			return invalid("HoldTimeLimits %T", v)
		}
		st.limits = sanitizeLimits(l)
		if st.holdTime < st.limits[0] || st.holdTime > st.limits[1] { // :459-465
			st.holdTime = st.limits[2]
		}
	case def.AttrHoldTime:
		h, ok := v.(uint16)
		if !ok {
			return invalid("HoldTime %T", v)
		}
		if err := checkHoldTime(st, h); err != nil {
			return host(err)
		}
		st.holdTime = h
	case def.AttrAmbientContextTypeSupported:
		list, ok := listOf[SemanticTag](v)
		if !ok {
			return invalid("AmbientContextTypeSupported %T", v)
		}
		if err := c.checkSupported(list); err != nil {
			return err
		}
		st.supported = slices.Clone(list)
	case def.AttrObjectCountConfig:
		cfg, ok := v.(ObjectCountConfig)
		if !ok {
			return invalid("ObjectCountConfig %T", v)
		}
		if err := c.checkObjectCountConfig(st, cfg); err != nil {
			return host(err)
		}
		st.occ = cfg
	case def.AttrSimultaneousDetectionLimit:
		n, ok := v.(uint8)
		if !ok {
			return invalid("SimultaneousDetectionLimit %T", v)
		}
		if err := checkSDL(n); err != nil {
			return host(err)
		}
		st.sdl = n
		if len(st.detections) > int(n) { // :396-416: the oldest go, without an end event
			st.detections = st.detections[:n]
			updateDetections(st)
		}
	case def.AttrPredictedActivity:
		list, ok := listOf[PredictedActivity](v)
		if !ok {
			return invalid("PredictedActivity %T", v)
		}
		if err := c.checkPredicted(list); err != nil {
			return err
		}
		st.predicted = slices.Clone(list)
	}
	return nil
}

// listOf reads a list in its stored form (spec.List) or as a slice.
func listOf[T spec.Encodable](v any) ([]T, bool) {
	switch l := v.(type) {
	case spec.List[T]:
		return l, true
	case []T:
		return l, true
	}
	return nil, false
}

// HoldTime returns the hold time in seconds.
func (c *AmbientContext) HoldTime() uint16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st.holdTime
}

// ObjectCountConfig returns the object count configuration.
func (c *AmbientContext) ObjectCountConfig() ObjectCountConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st.occ
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (c *AmbientContext) MatterDataVersion() uint32 { return c.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (c *AmbientContext) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return c.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute.
func (c *AmbientContext) MatterRead(attrID uint32) (any, bool) { return c.srv.MatterRead(attrID) }

// MatterWrite applies a write of ObjectCountConfig or HoldTime.
func (c *AmbientContext) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return c.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke answers every command: the cluster has none.
func (c *AmbientContext) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &c.SelfHeld, cmdID, fields)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (c *AmbientContext) SetMatterEventEmitter(emitter contract.EventEmitter) {
	c.srv.SetMatterEventEmitter(emitter)
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (c *AmbientContext) SetEndpoint(endpoint uint16) {
	c.mu.Lock()
	c.endpoint = endpoint
	c.mu.Unlock()
}

// Close stops the hold-time timer (chip Shutdown, .cpp:90-98); it is
// [AmbientContext.MatterQuiesce].
func (c *AmbientContext) Close() { c.MatterQuiesce() }

// MatterQuiesce implements [contract.ClusterQuiescer]: the bridge calls it
// when the server leaves the topology or stops, and it cancels the
// hold-time timer, so no AmbientContextDetectEnded is emitted after the
// bridge let the server go. Mirrors connectedhomeip
// src/app/clusters/ambient-context-sensing-server/AmbientContextSensingCluster.cpp:90-98
// at 6170af84 (Shutdown: mHoldTimeDelegate.CancelTimer); it used to have
// no caller (W3-club2-02).
func (c *AmbientContext) MatterQuiesce() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}
