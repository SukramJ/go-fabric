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
	"maps"
	"math"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	carbon "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	"github.com/SukramJ/go-fabric/contract"
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
// derivation. Reads, the controller's LastChangedTime write, the command
// dispatch, the data version and the change notifications are the
// generated server's ([spec.Server]); the lists, globals and privileges
// its embedded [spec.Instance]'s. What stays here are the cluster's rules:
// the values the host sets, and ResetCondition.
type Server struct {
	*spec.Instance

	srv *spec.Server
	cfg Config

	mu sync.Mutex
	// last is the state the host set last; it answers State for the
	// fields whose attribute the server does not serve.
	last State
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
	sc := spec.ServerConfig{DataVersion: cfg.DataVersion}
	if cfg.LastChangedTimeWriter != nil {
		sc.Sink = lastChangedSink{cfg.LastChangedTimeWriter}
	}
	srv, err := spec.NewServer(def, opts, sc)
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	s := &Server{Instance: srv.Instance, srv: srv, cfg: cfg}
	if !s.EnumSupported(hepa.DegradationDirectionEnumDef, uint64(cfg.DegradationDirection)) {
		return nil, fmt.Errorf("%w: DegradationDirection %d", ErrInvalidValue, cfg.DegradationDirection)
	}
	if err := s.checkProducts(); err != nil {
		return nil, err
	}
	s.cfg.ReplacementProducts = slices.Clone(cfg.ReplacementProducts)
	fixed := map[uint32]any{}
	if s.Serves(hepa.AttrDegradationDirection) {
		fixed[hepa.AttrDegradationDirection] = cfg.DegradationDirection
	}
	if s.Serves(hepa.AttrReplacementProductList) {
		fixed[hepa.AttrReplacementProductList] = spec.List[ReplacementProduct](s.cfg.ReplacementProducts)
	}
	// The initial values go in as a device-side change: the server holds
	// no value before, so this is the one data-version bump construction
	// makes.
	if err := s.setState(cfg.Initial, fixed); err != nil {
		return nil, err
	}
	if cfg.Optional&OptionalResetCondition != 0 {
		srv.Handle(hepa.CmdResetCondition, s.resetCondition)
	}
	return s, nil
}

// checkProducts holds ReplacementProductList to its constraints, read from
// the definition: at most five entries ("max 5"), a known identifier type,
// an identifier of at most 20 characters ("max 20"). The definition's
// value check ([spec.Instance.CheckValue]) holds all three, whether REP
// serves the list or not.
func (s *Server) checkProducts() error {
	if _, err := s.CheckValue(hepa.AttrReplacementProductList, spec.List[ReplacementProduct](s.cfg.ReplacementProducts), nil); err != nil {
		return fmt.Errorf("%w: replacement products: %w", ErrInvalidValue, err)
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

// values maps the served attributes of st to their values.
func (s *Server) values(st State) map[uint32]any {
	all := map[uint32]any{
		hepa.AttrCondition:        st.Condition,
		hepa.AttrChangeIndication: st.ChangeIndication,
		hepa.AttrInPlaceIndicator: st.InPlaceIndicator,
		hepa.AttrLastChangedTime:  nil,
	}
	if st.LastChangedTime != nil {
		all[hepa.AttrLastChangedTime] = *st.LastChangedTime
	}
	out := make(map[uint32]any, len(all))
	for id, v := range all {
		if s.Serves(id) {
			out[id] = v
		}
	}
	return out
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *Server) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// Notify tells the listeners that attrIDs changed.
func (s *Server) Notify(attrIDs ...uint32) { s.srv.Notify(attrIDs...) }

// State returns the current state.
func (s *Server) State() State {
	s.mu.Lock()
	st := s.last
	s.mu.Unlock()
	if v, ok := s.srv.Value(hepa.AttrCondition); ok {
		st.Condition, _ = v.(uint8)
	}
	if v, ok := s.srv.Value(hepa.AttrChangeIndication); ok {
		n, _ := v.(uint8)
		st.ChangeIndication = ChangeIndication(n)
	}
	if v, ok := s.srv.Value(hepa.AttrInPlaceIndicator); ok {
		st.InPlaceIndicator, _ = v.(bool)
	}
	if v, ok := s.srv.Value(hepa.AttrLastChangedTime); ok {
		st.LastChangedTime = nil
		if t, isTime := v.(uint32); isTime {
			st.LastChangedTime = &t
		}
	}
	return st
}

// SetState records a change the device made — the resource wearing down,
// a filter swapped. It is refused when the state breaks the model.
func (s *Server) SetState(st State) error { return s.setState(st, nil) }

// setState holds st to the model and stores it with the extra attribute
// values; the generated server reports the served attributes that
// changed. It cannot refuse what check admits: the definition's own
// checks of these attributes (the percent's "max 100", the enum's
// conformance, the nullable range) are the same ones.
func (s *Server) setState(st State, extra map[uint32]any) error {
	if err := s.check(st); err != nil {
		return err
	}
	values := s.values(st)
	maps.Copy(values, extra)
	s.mu.Lock()
	s.last = st
	s.mu.Unlock()
	return s.srv.SetAttributes(values)
}

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite applies a LastChangedTime write, the one writable attribute;
// the definition answers every other write and every value outside the
// nullable epoch-s range.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// lastChangedSink hands a controller's LastChangedTime write to the host.
type lastChangedSink struct{ w LastChangedTimeWriter }

// MatterWriteAttribute implements [spec.Sink]. The value is the stored
// form of the nullable epoch-s, a uint32 or nil.
func (l lastChangedSink) MatterWriteAttribute(ctx context.Context, _ uint32, value any) error {
	var t *uint32
	if n, ok := value.(uint32); ok {
		t = &n
	}
	if err := l.w.WriteLastChangedTime(ctx, t); err != nil {
		return fmt.Errorf("filter: LastChangedTime write: %w", err)
	}
	return nil
}

// MatterInvoke dispatches an accepted command; the generated server
// answers any other with UNSUPPORTED_COMMAND.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// resetCondition carries out ResetCondition: the host resets the
// resource, then Condition shows full availability and ChangeIndication
// Ok. Full availability is 100 % for a condition that degrades downwards
// and 0 % for one that degrades upwards (DegradationDirectionEnum Up: "the
// degradation of the resource is indicated by an upwards
// moving/increasing value"). The command answers with a status only.
func (s *Server) resetCondition(ctx context.Context, _ any) (any, error) {
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
	return nil, s.setState(st, nil)
}
