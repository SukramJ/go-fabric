// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package filter contains one server for the Matter ResourceMonitoring
// cluster family, instantiated for the two derivations the AirPurifier and
// ExtractorHood device types offer: HepaFilterMonitoring (0x0071) and
// ActivatedCarbonFilterMonitoring (0x0072).
//
// It is the first server built on a generated cluster definition from the
// start (cluster/spec/hepafiltermonitoring,
// cluster/spec/activatedcarbonfiltermonitoring; ADR 0013): ids, enums, the
// ReplacementProductStruct and its codec, the feature-dependent attribute
// list, FeatureMap, ClusterRevision, the privileges and the
// LastChangedTime write check all come from the definition. What is left
// here is what matter.js writes by hand — and for this family matter.js
// writes nothing: HepaFilterMonitoringServer and
// ActivatedCarbonFilterMonitoringServer
// (packages/node/src/behaviors/<name>/*Server.ts) add no logic to the
// generated behavior. So this server holds the state as matter.js's
// behavior state does, checks what the host sets against the model
// (ChangeIndication Warning needs WRN, Condition is a percent), and carries
// out ResetCondition as the specification text matter.js ships with it
// describes (resource-monitoring.resource.ts, cluster §2.8.7.1): "the
// device shall reset the Condition and ChangeIndicator attributes,
// indicating full resource availability and readiness for use".
package filter

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	carbon "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Cluster ids.
const (
	ClusterIDHepaFilterMonitoring            = hepa.ClusterID
	ClusterIDActivatedCarbonFilterMonitoring = carbon.ClusterID
)

// The ResourceMonitoring datatypes. Both derivations inherit them unchanged
// from ResourceMonitoring, so the server speaks HepaFilterMonitoring's
// generated types for both; the encodings are the same.
type (
	// Feature is a FeatureMap bit.
	Feature = hepa.Feature
	// ChangeIndication is the ChangeIndicationEnum.
	ChangeIndication = hepa.ChangeIndicationEnum
	// DegradationDirection is the DegradationDirectionEnum.
	DegradationDirection = hepa.DegradationDirectionEnum
	// ReplacementProduct is one ReplacementProductList entry.
	ReplacementProduct = hepa.ReplacementProductStruct
)

// Features (CON, WRN, REP; every one optional).
const (
	FeatureCondition              = hepa.FeatureCondition
	FeatureWarning                = hepa.FeatureWarning
	FeatureReplacementProductList = hepa.FeatureReplacementProductList
)

// ChangeIndication values; Warning needs FeatureWarning.
const (
	ChangeIndicationOk       = hepa.ChangeIndicationOk
	ChangeIndicationWarning  = hepa.ChangeIndicationWarning
	ChangeIndicationCritical = hepa.ChangeIndicationCritical
)

// Optional names the optional elements a host serves.
type Optional uint32

// Optional elements (all conformance "O").
const (
	// OptionalInPlaceIndicator serves InPlaceIndicator.
	OptionalInPlaceIndicator Optional = 1 << iota
	// OptionalLastChangedTime serves LastChangedTime, writable "RW VO".
	OptionalLastChangedTime
	// OptionalResetCondition accepts ResetCondition; it needs
	// Config.Resetter.
	OptionalResetCondition
)

// State is the cluster's changing state.
type State struct {
	// Condition is the remaining resource in percent (CON).
	Condition uint8
	// ChangeIndication says whether the resource needs changing.
	ChangeIndication ChangeIndication
	// InPlaceIndicator says whether a resource is installed.
	InPlaceIndicator bool
	// LastChangedTime is when the resource was last changed, in seconds
	// since the Matter epoch; nil is null ("never set or unknown").
	LastChangedTime *uint32
}

// Resetter is the host port for ResetCondition: the device resets its
// resource tracking (a filter-hours counter, say). On success the server
// resets Condition to 100 and ChangeIndication to Ok.
type Resetter interface {
	ResetCondition(ctx context.Context) error
}

// LastChangedTimeWriter is told when a controller writes LastChangedTime,
// so the host can persist it (quality "N"); an error refuses the write.
type LastChangedTimeWriter interface {
	WriteLastChangedTime(ctx context.Context, t *uint32) error
}

// Config carries the construction parameters.
type Config struct {
	Features Feature
	Optional Optional
	// DegradationDirection is fixed (quality "F"); served with CON.
	DegradationDirection DegradationDirection
	// ReplacementProducts is fixed; served with REP, at most five.
	ReplacementProducts []ReplacementProduct
	// Initial is the state the server starts with.
	Initial State
	// Resetter carries out ResetCondition; required with
	// OptionalResetCondition.
	Resetter Resetter
	// LastChangedTimeWriter, optional, is told about a controller's write.
	LastChangedTimeWriter LastChangedTimeWriter
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Configuration errors.
var (
	ErrNoResetter   = errors.New("filter: OptionalResetCondition needs a Resetter")
	ErrInvalidValue = errors.New("filter: value outside the cluster's model")
)

// Server implements [contract.ClusterServer] for one ResourceMonitoring
// derivation. The lists, globals and privileges are the embedded
// [spec.Instance]'s.
type Server struct {
	*spec.Instance
	cluster.AttributeChanges

	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	cfg      Config

	mu    sync.Mutex
	state State
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Server)(nil)
	_ contract.ClusterDataVersion             = (*Server)(nil)
	_ contract.ClusterAttributeLister         = (*Server)(nil)
	_ contract.ClusterCommandLister           = (*Server)(nil)
	_ contract.ClusterEventLister             = (*Server)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Server)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Server)(nil)
	_ contract.AttributeChangeNotifier        = (*Server)(nil)
)

// NewHepaFilterMonitoring builds a HepaFilterMonitoring (0x0071) server.
func NewHepaFilterMonitoring(cfg Config) (*Server, error) { return newServer(hepa.Definition, cfg) }

// NewActivatedCarbonFilterMonitoring builds an
// ActivatedCarbonFilterMonitoring (0x0072) server.
func NewActivatedCarbonFilterMonitoring(cfg Config) (*Server, error) {
	return newServer(carbon.Definition, cfg)
}

func newServer(def *spec.Cluster, cfg Config) (*Server, error) {
	opts := spec.Options{Features: uint32(cfg.Features)}
	if cfg.Optional&OptionalInPlaceIndicator != 0 {
		opts.Attributes = append(opts.Attributes, hepa.AttrInPlaceIndicator)
	}
	if cfg.Optional&OptionalLastChangedTime != 0 {
		opts.Attributes = append(opts.Attributes, hepa.AttrLastChangedTime)
	}
	if cfg.Optional&OptionalResetCondition != 0 {
		if cfg.Resetter == nil {
			return nil, ErrNoResetter
		}
		opts.Commands = []uint32{hepa.CmdResetCondition}
	}
	inst, err := spec.New(def, opts)
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	s := &Server{Instance: inst, ext: cfg.DataVersion, cfg: cfg}
	if !inst.EnumSupported(hepa.DegradationDirectionEnumDef, uint64(cfg.DegradationDirection)) {
		return nil, fmt.Errorf("%w: DegradationDirection %d", ErrInvalidValue, cfg.DegradationDirection)
	}
	if err := s.checkProducts(); err != nil {
		return nil, err
	}
	s.cfg.ReplacementProducts = slices.Clone(cfg.ReplacementProducts)
	if err := s.check(cfg.Initial); err != nil {
		return nil, err
	}
	s.state = cfg.Initial
	return s, nil
}

// checkProducts holds ReplacementProductList to its constraints, read from
// the definition: at most five entries ("max 5"), a known identifier type,
// an identifier of at most 20 characters ("max 20").
func (s *Server) checkProducts() error {
	list := s.Definition().Attribute(hepa.AttrReplacementProductList)
	if n := int64(len(s.cfg.ReplacementProducts)); n > list.Constraint.Max.Int {
		return fmt.Errorf("%w: %d replacement products, at most %d", ErrInvalidValue, n, list.Constraint.Max.Int)
	}
	value := hepa.ReplacementProductStructDef.Fields[1].Constraint.Max.Int
	for _, p := range s.cfg.ReplacementProducts {
		if !s.EnumSupported(hepa.ProductIdentifierTypeEnumDef, uint64(p.ProductIdentifierType)) ||
			int64(spec.StringLength(p.ProductIdentifierValue)) > value {
			return fmt.Errorf("%w: replacement product %+v", ErrInvalidValue, p)
		}
	}
	return nil
}

// check holds a state to the model: Condition is a percent, and
// ChangeIndication a value the feature selection allows (Warning needs
// WRN), as matter.js's state validation holds a behavior's state.
func (s *Server) check(st State) error {
	switch {
	case st.Condition > 100:
		return fmt.Errorf("%w: Condition %d%% > 100", ErrInvalidValue, st.Condition)
	case !s.EnumSupported(hepa.ChangeIndicationEnumDef, uint64(st.ChangeIndication)):
		return fmt.Errorf("%w: ChangeIndication %d", ErrInvalidValue, st.ChangeIndication)
	case st.LastChangedTime != nil && *st.LastChangedTime == math.MaxUint32: // null on the wire
		return fmt.Errorf("%w: LastChangedTime is the null value", ErrInvalidValue)
	}
	return nil
}

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// State returns the current state.
func (s *Server) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetState records a change the device made — the resource wearing down,
// a filter swapped. It is refused when the state breaks the model.
func (s *Server) SetState(st State) error {
	if err := s.check(st); err != nil {
		return err
	}
	s.apply(st)
	return nil
}

// apply stores st and reports the served attributes that changed.
func (s *Server) apply(st State) {
	s.mu.Lock()
	old := s.state
	s.state = st
	s.mu.Unlock()
	var changed []uint32
	for id, differs := range map[uint32]bool{
		hepa.AttrCondition:        old.Condition != st.Condition,
		hepa.AttrChangeIndication: old.ChangeIndication != st.ChangeIndication,
		hepa.AttrInPlaceIndicator: old.InPlaceIndicator != st.InPlaceIndicator,
		hepa.AttrLastChangedTime:  !equalTime(old.LastChangedTime, st.LastChangedTime),
	} {
		if differs && s.Serves(id) {
			changed = append(changed, id)
		}
	}
	if len(changed) == 0 {
		return
	}
	slices.Sort(changed)
	s.tracker().Bump()
	s.Notify(changed...)
}

func equalTime(a, b *uint32) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.Serves(attrID) {
		return nil, false
	}
	st := s.State()
	switch attrID {
	case hepa.AttrCondition:
		return st.Condition, true
	case hepa.AttrDegradationDirection:
		return uint8(s.cfg.DegradationDirection), true
	case hepa.AttrChangeIndication:
		return uint8(st.ChangeIndication), true
	case hepa.AttrInPlaceIndicator:
		return st.InPlaceIndicator, true
	case hepa.AttrLastChangedTime:
		if st.LastChangedTime == nil {
			return nil, true
		}
		return *st.LastChangedTime, true
	}
	// ReplacementProductList, the last attribute Serves admits.
	return spec.List[ReplacementProduct](slices.Clone(s.cfg.ReplacementProducts)), true
}

// MatterWrite applies a LastChangedTime write, the one writable attribute;
// the definition answers every other write and every value outside the
// nullable epoch-s range.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	v, err := s.ValidateWrite(attrID, value, nil)
	if err != nil {
		return err
	}
	var t *uint32
	if n, ok := v.(uint64); ok { // within the nullable epoch-s range per ValidateWrite; nil for null
		at := uint32(n) //nolint:gosec // ≤ 0xFFFFFFFE per ValidateWrite
		t = &at
	}
	if w := s.cfg.LastChangedTimeWriter; w != nil {
		if err := w.WriteLastChangedTime(ctx, t); err != nil {
			return fmt.Errorf("filter: LastChangedTime write: %w", err)
		}
	}
	st := s.State()
	st.LastChangedTime = t
	s.apply(st)
	return nil
}

// MatterInvoke carries out ResetCondition: the host resets the resource,
// then Condition shows full availability and ChangeIndication Ok. Full
// availability is 100 % for a condition that degrades downwards and 0 % for
// one that degrades upwards (DegradationDirectionEnum Up: "the degradation
// of the resource is indicated by an upwards moving/increasing value").
// The command answers with a status only.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, _ any) (any, error) {
	if !s.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("filter: command 0x%02X is not supported", cmdID)
	}
	if err := s.cfg.Resetter.ResetCondition(ctx); err != nil {
		return nil, fmt.Errorf("filter: ResetCondition: %w", err)
	}
	st := s.State()
	if s.HasFeature("CON") {
		st.Condition = 100
		if s.cfg.DegradationDirection == hepa.DegradationDirectionUp {
			st.Condition = 0
		}
	}
	st.ChangeIndication = ChangeIndicationOk
	s.apply(st)
	return nil, nil
}
