// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package servicearea contains the Matter ServiceArea cluster server
// (0x0150): the areas a robotic vacuum cleaner (or any device that serves
// areas) knows, the maps they lie on, which of them a controller selected,
// where the device is now and how far it got.
//
// The cluster's identity is its generated definition
// (cluster/spec/servicearea, ADR 0013): ids, revision, the structs and
// their codecs, the enums, the feature-dependent attribute list, the
// privileges and the write answers. The rules are matter.js's
// ServiceAreaBaseServer (packages/node/src/behaviors/service-area/
// ServiceAreaServer.ts), ported onto a hand-written server the way
// cluster/opstate ports OperationalStateServer: the server holds the
// state, and every setter enforces what that server's reactors enforce —
//
//   - SupportedMaps: MapID and Name unique (#assertSupportedMaps :75-92);
//   - SupportedAreas: AreaID unique; an area has LocationInfo or
//     LandmarkInfo, and a LocationInfo is not empty; no two areas of one
//     map with equal AreaInfo; with SupportedMaps non-empty no area has a
//     null MapID, otherwise every area has (#assertSupportedAreas
//     :94-147);
//   - SelectedAreas: supported and unique (#assertSelectedAreas :149-160);
//   - CurrentArea: null or supported; null makes EstimatedEndTime null
//     (#assertCurrentArea :162-172, initialize :53-56);
//   - Progress: AreaIDs unique and supported (#assertProgress :174-185).
//
// A setter that breaks one of these returns an error wrapping
// [ErrInvalidState] and changes nothing, where matter.js throws a
// ValidationError. Each value is also held to the definition's own type
// and constraint (list lengths, string lengths).
//
// SelectAreas answers as matter.js's selectAreas does (:196-213): every
// requested area must be supported (UnsupportedArea otherwise,
// assertSelectServiceArea :215-236), duplicates are dropped, and on
// Success SelectedAreas becomes the selection and — with Progress served
// and a non-empty selection — Progress lists every selected area as
// Pending. The device-side decision matter.js leaves to the
// assertSelectServiceArea extension point (InvalidInMode, InvalidSet) is
// the host's [Selector], asked after the default checks passed, as
// matter.js's own CHIP test device does (support/chip-testing/src/cluster/
// TestServiceAreaServer.ts selectAreas).
//
// SkipArea is optional ("[CurrentArea | Progress]"); matter.js implements
// only its default validation (assertSkipServiceArea :238-255:
// InvalidAreaList with nothing selected, InvalidSkippedArea for an area
// not selected) and leaves the rest to the device, here the host's
// [Skipper].
//
// EstimatedEndTime has quality Q; its reports follow the shouldEmit
// matter.js installs (initialize :57-71): at once when it changes to or
// from null or 0 or when it decreases, otherwise through the quiet
// throttle ([cluster.Quieter]).
package servicearea

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterID is the ServiceArea cluster id, from the generated definition.
const ClusterID = sa.ClusterID

// DeviceTypeRoboticVacuumCleaner offers ServiceArea (optional) —
// robotic-vacuum-cleaner.element.ts.
const DeviceTypeRoboticVacuumCleaner uint16 = 0x0074

// The ServiceArea datatypes, from the generated definition.
type (
	// Feature is a FeatureMap bit.
	Feature = sa.Feature
	// Area is one SupportedAreas entry.
	Area = sa.AreaStruct
	// AreaInfo is an area's description.
	AreaInfo = sa.AreaInfoStruct
	// LocationInfo is an AreaInfo's location description.
	LocationInfo = sa.Locationdesc
	// LandmarkInfo is an AreaInfo's landmark description.
	LandmarkInfo = sa.LandmarkInfoStruct
	// Map is one SupportedMaps entry.
	Map = sa.MapStruct
	// Progress is one Progress entry.
	Progress = sa.ProgressStruct
	// OperationalStatus is the OperationalStatusEnum of a Progress entry.
	OperationalStatus = sa.OperationalStatusEnum
	// SelectAreasStatus is the status a SelectAreasResponse carries.
	SelectAreasStatus = sa.SelectAreasStatus
	// SkipAreaStatus is the status a SkipAreaResponse carries.
	SkipAreaStatus = sa.SkipAreaStatus
)

// Features (every one optional).
const (
	FeatureSelectWhileRunning = sa.FeatureSelectWhileRunning // SELRUN
	FeatureProgressReporting  = sa.FeatureProgressReporting  // PROG
	FeatureMaps               = sa.FeatureMaps               // MAPS
)

// Statuses.
const (
	SelectAreasSuccess         = sa.SelectAreasStatusSuccess
	SelectAreasUnsupportedArea = sa.SelectAreasStatusUnsupportedArea
	SelectAreasInvalidInMode   = sa.SelectAreasStatusInvalidInMode
	SelectAreasInvalidSet      = sa.SelectAreasStatusInvalidSet

	SkipAreaSuccess            = sa.SkipAreaStatusSuccess
	SkipAreaInvalidAreaList    = sa.SkipAreaStatusInvalidAreaList
	SkipAreaInvalidInMode      = sa.SkipAreaStatusInvalidInMode
	SkipAreaInvalidSkippedArea = sa.SkipAreaStatusInvalidSkippedArea

	StatusPending   = sa.OperationalStatusPending
	StatusOperating = sa.OperationalStatusOperating
	StatusSkipped   = sa.OperationalStatusSkipped
	StatusCompleted = sa.OperationalStatusCompleted
)

// Selector is the device-side decision on a SelectAreas request whose
// areas are all supported (matter.js's assertSelectServiceArea extension
// point). areas is the request without duplicates. A non-Success status
// is answered as is and changes nothing; an error answers FAILURE.
type Selector interface {
	SelectAreas(ctx context.Context, areas []uint32) (SelectAreasStatus, string, error)
}

// Skipper carries out SkipArea for an area that is selected (matter.js's
// assertSkipServiceArea passed). It moves the device on and updates the
// Progress itself ([Server.SetProgress]); a non-Success status is
// answered as is, an error answers FAILURE.
type Skipper interface {
	SkipArea(ctx context.Context, area uint32) (SkipAreaStatus, string, error)
}

// Config carries the construction parameters.
type Config struct {
	// Features: any of SELRUN, PROG, MAPS.
	Features Feature
	// Selector decides SelectAreas; required.
	Selector Selector
	// Skipper, optional, accepts SkipArea; it needs CurrentArea served or
	// PROG ("[CurrentArea | Progress]").
	Skipper Skipper

	// SupportedAreas, SupportedMaps (MAPS), SelectedAreas and Progress
	// (PROG) are the initial state.
	SupportedAreas []Area
	SupportedMaps  []Map
	SelectedAreas  []uint32
	Progress       []Progress

	// CurrentArea serves the optional CurrentArea attribute; its initial
	// value is InitialCurrentArea (nil: null).
	CurrentArea        bool
	InitialCurrentArea *uint32
	// EstimatedEndTime serves the optional EstimatedEndTime attribute
	// ("[CurrentArea]"); it starts null.
	EstimatedEndTime bool

	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Errors.
var (
	// ErrNoSelector: Config.Selector is required.
	ErrNoSelector = errors.New("servicearea: a Selector is required")
	// ErrConfig: an optional element is declared without what its
	// conformance needs.
	ErrConfig = errors.New("servicearea: element not allowed by its conformance")
	// ErrInvalidState: a value that breaks one of matter.js's rules or the
	// definition's constraints.
	ErrInvalidState = errors.New("servicearea: value outside the cluster's rules")
)

// Server implements [contract.ClusterServer] for ServiceArea. The lists,
// globals and privileges are the embedded [spec.Instance]'s.
type Server struct {
	*spec.Instance
	cluster.AttributeChanges

	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	selector Selector
	skipper  Skipper
	quiet    *cluster.Quieter

	mu       sync.Mutex
	areas    []Area
	maps     []Map
	selected []uint32
	current  *uint32
	endTime  *uint32
	progress []Progress
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

// New builds a ServiceArea server and holds its initial state to the
// rules, in the order matter.js's initialize checks them (:37-52).
func New(cfg Config) (*Server, error) {
	if cfg.Selector == nil {
		return nil, ErrNoSelector
	}
	opts := spec.Options{Features: uint32(cfg.Features)}
	if cfg.CurrentArea {
		opts.Attributes = append(opts.Attributes, sa.AttrCurrentArea)
	}
	if cfg.EstimatedEndTime {
		// "[CurrentArea]": optional, and only next to CurrentArea.
		if !cfg.CurrentArea {
			return nil, fmt.Errorf("%w: EstimatedEndTime needs CurrentArea", ErrConfig)
		}
		opts.Attributes = append(opts.Attributes, sa.AttrEstimatedEndTime)
	}
	if cfg.Skipper != nil {
		// "[CurrentArea | Progress]".
		if !cfg.CurrentArea && cfg.Features&FeatureProgressReporting == 0 {
			return nil, fmt.Errorf("%w: SkipArea needs CurrentArea or Progress", ErrConfig)
		}
		opts.Commands = []uint32{sa.CmdSkipArea}
	}
	if cfg.InitialCurrentArea != nil && !cfg.CurrentArea {
		return nil, fmt.Errorf("%w: InitialCurrentArea without CurrentArea", ErrConfig)
	}
	inst, err := spec.New(sa.Definition, opts)
	if err != nil {
		return nil, fmt.Errorf("servicearea: %w", err)
	}
	s := &Server{Instance: inst, ext: cfg.DataVersion, selector: cfg.Selector, skipper: cfg.Skipper}
	s.quiet = &cluster.Quieter{Report: func() { s.changed(sa.AttrEstimatedEndTime) }}
	if len(cfg.SupportedMaps) > 0 && !s.Serves(sa.AttrSupportedMaps) {
		return nil, fmt.Errorf("%w: SupportedMaps without MAPS", ErrConfig)
	}
	if len(cfg.Progress) > 0 && !s.Serves(sa.AttrProgress) {
		return nil, fmt.Errorf("%w: Progress without PROG", ErrConfig)
	}
	// The rules read the maps and areas the server holds, as matter.js's
	// read this.state; the checks run in initialize's order.
	s.maps, s.areas = slices.Clone(cfg.SupportedMaps), slices.Clone(cfg.SupportedAreas)
	for _, check := range []func() error{
		func() error { return s.checkAreas(cfg.SupportedAreas) },
		func() error { return s.checkMaps(cfg.SupportedMaps) },
		func() error { return s.checkSelected(cfg.SelectedAreas) },
		func() error { return s.checkCurrent(cfg.InitialCurrentArea) },
		func() error { return s.checkProgress(cfg.Progress) },
	} {
		if err := check(); err != nil {
			return nil, err
		}
	}
	s.selected = slices.Clone(cfg.SelectedAreas)
	s.current = clonePtr(cfg.InitialCurrentArea)
	s.progress = slices.Clone(cfg.Progress)
	return s, nil
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidState}, args...)...)
}

// checkValue holds a value to the definition's type and constraint.
func (s *Server) checkValue(attrID uint32, v any) error {
	if _, err := s.CheckValue(attrID, v, nil); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidState, err)
	}
	return nil
}

// checkMaps is matter.js #assertSupportedMaps (:75-92).
func (s *Server) checkMaps(maps []Map) error {
	if err := s.checkValue(sa.AttrSupportedMaps, spec.List[Map](maps)); err != nil {
		return err
	}
	ids := map[uint32]bool{}
	names := map[string]bool{}
	for _, m := range maps {
		if ids[m.MapId] {
			return invalid("MapID %d is not unique", m.MapId)
		}
		if names[m.Name] {
			return invalid("MapName %q is not unique", m.Name)
		}
		ids[m.MapId], names[m.Name] = true, true
	}
	return nil
}

// checkAreas is matter.js #assertSupportedAreas (:94-147), against the
// maps the server holds.
func (s *Server) checkAreas(areas []Area) error {
	if err := s.checkValue(sa.AttrSupportedAreas, spec.List[Area](areas)); err != nil {
		return err
	}
	if len(areas) == 0 {
		return nil
	}
	ids := map[uint32]bool{}
	for _, a := range areas {
		if ids[a.AreaId] {
			return invalid("AreaID %d is not unique", a.AreaId)
		}
		loc, land := a.AreaInfo.LocationInfo, a.AreaInfo.LandmarkInfo
		if loc.Null && land.Null {
			return invalid("Area %d has no location or landmark info", a.AreaId)
		}
		if !loc.Null && loc.Value.LocationName == "" && loc.Value.FloorNumber.Null && loc.Value.AreaType.Null && land.Null {
			return invalid("Area %d has no location info", a.AreaId)
		}
		ids[a.AreaId] = true
	}
	nullMap, mapIDs := false, map[uint32]bool{}
	for i, a := range areas {
		if a.MapId.Null {
			nullMap = true
		} else {
			mapIDs[a.MapId.Value] = true
		}
		for _, other := range areas[i+1:] {
			if !sameMap(a.MapId, other.MapId) {
				continue
			}
			if sameAreaInfo(a.AreaInfo, other.AreaInfo) {
				return invalid("Areas must have a unique AreaInfo field, but area %d and area %d are equal", a.AreaId, other.AreaId)
			}
		}
	}
	distinct := len(mapIDs)
	if nullMap {
		distinct++
	}
	if len(s.maps) > 0 {
		if nullMap {
			return invalid("Areas must not have a null mapId when supportedMaps is defined")
		}
	} else if !nullMap || distinct > 1 {
		return invalid("Areas must have a null mapId when supportedMaps is empty")
	}
	return nil
}

// sameMap is matter.js's `otherArea.mapId !== mapId` negated: null equals
// null.
func sameMap(a, b spec.Nullable[uint32]) bool {
	if a.Null || b.Null {
		return a.Null == b.Null
	}
	return a.Value == b.Value
}

// sameAreaInfo is matter.js's isDeepEqual of two AreaInfo values: a null
// field equals only a null field.
func sameAreaInfo(a, b AreaInfo) bool {
	if a.LocationInfo.Null != b.LocationInfo.Null || a.LandmarkInfo.Null != b.LandmarkInfo.Null {
		return false
	}
	if !a.LocationInfo.Null {
		x, y := a.LocationInfo.Value, b.LocationInfo.Value
		if x.LocationName != y.LocationName || !sameNullable(x.FloorNumber, y.FloorNumber) || !sameNullable(x.AreaType, y.AreaType) {
			return false
		}
	}
	if !a.LandmarkInfo.Null {
		x, y := a.LandmarkInfo.Value, b.LandmarkInfo.Value
		if x.LandmarkTag != y.LandmarkTag || !sameNullable(x.RelativePositionTag, y.RelativePositionTag) {
			return false
		}
	}
	return true
}

func sameNullable[T comparable](a, b spec.Nullable[T]) bool {
	if a.Null || b.Null {
		return a.Null == b.Null
	}
	return a.Value == b.Value
}

// supported reports whether id is a SupportedAreas entry.
func (s *Server) supported(id uint32) bool {
	return slices.ContainsFunc(s.areas, func(a Area) bool { return a.AreaId == id })
}

// checkSelected is matter.js #assertSelectedAreas (:149-160).
// SelectedAreas is a list[uint32] without a length constraint ("desc"):
// the definition adds nothing to check.
func (s *Server) checkSelected(ids []uint32) error {
	seen := map[uint32]bool{}
	for _, id := range ids {
		if !s.supported(id) {
			return invalid("AreaID %d is not in the supported areas list", id)
		}
		if seen[id] {
			return invalid("AreaID %d is not unique", id)
		}
		seen[id] = true
	}
	return nil
}

// checkCurrent is matter.js #assertCurrentArea (:162-172), less the
// EstimatedEndTime reset the setter applies.
func (s *Server) checkCurrent(id *uint32) error {
	if id != nil && !s.supported(*id) {
		return invalid("AreaID %d is not in the supported areas list", *id)
	}
	return nil
}

// checkProgress is matter.js #assertProgress (:174-185).
func (s *Server) checkProgress(progress []Progress) error {
	if err := s.checkValue(sa.AttrProgress, spec.List[Progress](progress)); err != nil {
		return err
	}
	seen := map[uint32]bool{}
	for _, p := range progress {
		if seen[p.AreaId] {
			return invalid("AreaID %d is not unique", p.AreaId)
		}
		seen[p.AreaId] = true
		if !s.supported(p.AreaId) {
			return invalid("AreaID %d is not in the supported areas list", p.AreaId)
		}
	}
	return nil
}

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// changed bumps the data version and reports attrIDs.
func (s *Server) changed(attrIDs ...uint32) {
	s.tracker().Bump()
	s.Notify(attrIDs...)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterReportable lists the attributes whose change is reported at once:
// every served one but EstimatedEndTime, whose quality is Q and whose
// reports go through the quiet throttle.
func (s *Server) MatterReportable() []uint32 {
	out := make([]uint32, 0, 6)
	for _, id := range s.MatterAttributes() {
		if id != sa.AttrEstimatedEndTime {
			out = append(out, id)
		}
	}
	return out
}

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.Serves(attrID) {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch attrID {
	case sa.AttrSupportedAreas:
		return spec.List[Area](slices.Clone(s.areas)), true
	case sa.AttrSupportedMaps:
		return spec.List[Map](slices.Clone(s.maps)), true
	case sa.AttrSelectedAreas:
		return append([]uint32{}, s.selected...), true
	case sa.AttrCurrentArea:
		return nullable(s.current), true
	case sa.AttrEstimatedEndTime:
		return nullable(s.endTime), true
	}
	// Progress, the last attribute Serves admits.
	return spec.List[Progress](slices.Clone(s.progress)), true
}

// nullable is a nullable uint32 in its read form: nil for null.
func nullable(p *uint32) any {
	if p == nil {
		return nil
	}
	return *p
}

// MatterWrite refuses every write: every attribute is read-only, and the
// definition answers each with its status.
func (s *Server) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := s.ValidateWrite(attrID, value, nil)
	return err
}

// MatterInvoke carries out SelectAreas and SkipArea.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if !s.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("servicearea: command 0x%02X is not supported", cmdID)
	}
	if cmdID == sa.CmdSelectAreas {
		req, ok := request[sa.SelectAreasRequest](fields)
		if !ok {
			return nil, spec.Errorf(im.StatusInvalidCommand, "servicearea: SelectAreas fields %T", fields)
		}
		return s.selectAreas(ctx, req.NewAreas)
	}
	// SkipArea, the other command Accepts admits.
	req, ok := request[sa.SkipAreaRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "servicearea: SkipArea fields %T", fields)
	}
	return s.skipArea(ctx, req.SkippedArea)
}

// request takes a generated request by value or by pointer.
func request[T any](fields any) (T, bool) {
	switch f := fields.(type) {
	case T:
		return f, true
	case *T:
		if f != nil {
			return *f, true
		}
	}
	var zero T
	return zero, false
}

// selectAreas is matter.js's selectAreas (:196-213) with
// assertSelectServiceArea (:215-236) and the host's decision.
func (s *Server) selectAreas(ctx context.Context, newAreas []uint32) (any, error) {
	s.mu.Lock()
	var validated []uint32
	for _, id := range newAreas {
		if !s.supported(id) {
			s.mu.Unlock()
			return sa.SelectAreasResponse{
				Status:     SelectAreasUnsupportedArea,
				StatusText: fmt.Sprintf("AreaID %d is not in the supported areas list", id),
			}, nil
		}
		if !slices.Contains(validated, id) {
			validated = append(validated, id)
		}
	}
	s.mu.Unlock()
	status, text, err := s.selector.SelectAreas(ctx, slices.Clone(validated))
	if err != nil {
		return nil, fmt.Errorf("servicearea: SelectAreas: %w", err)
	}
	if status != SelectAreasSuccess {
		return sa.SelectAreasResponse{Status: status, StatusText: text}, nil
	}
	changed := []uint32{sa.AttrSelectedAreas}
	s.mu.Lock()
	s.selected = validated
	if s.Serves(sa.AttrProgress) && len(validated) > 0 {
		progress := make([]Progress, 0, len(validated))
		for _, id := range validated {
			progress = append(progress, Progress{AreaId: id, Status: StatusPending})
		}
		s.progress = progress
		changed = append(changed, sa.AttrProgress)
	}
	s.mu.Unlock()
	s.changed(changed...)
	return sa.SelectAreasResponse{Status: SelectAreasSuccess, StatusText: text}, nil
}

// skipArea is matter.js's assertSkipServiceArea (:238-255), then the
// host's Skipper.
func (s *Server) skipArea(ctx context.Context, area uint32) (any, error) {
	s.mu.Lock()
	none, selected := len(s.selected) == 0, slices.Contains(s.selected, area)
	s.mu.Unlock()
	switch {
	case none:
		return sa.SkipAreaResponse{Status: SkipAreaInvalidAreaList}, nil
	case !selected:
		return sa.SkipAreaResponse{
			Status:     SkipAreaInvalidSkippedArea,
			StatusText: fmt.Sprintf("AreaID %d is not in the selected areas list", area),
		}, nil
	}
	status, text, err := s.skipper.SkipArea(ctx, area)
	if err != nil {
		return nil, fmt.Errorf("servicearea: SkipArea: %w", err)
	}
	return sa.SkipAreaResponse{Status: status, StatusText: text}, nil
}

// SetSupportedAreas records the device's area list; it is refused when it
// breaks #assertSupportedAreas.
func (s *Server) SetSupportedAreas(areas []Area) error {
	s.mu.Lock()
	if err := s.checkAreas(areas); err != nil {
		s.mu.Unlock()
		return err
	}
	s.areas = slices.Clone(areas)
	s.mu.Unlock()
	s.changed(sa.AttrSupportedAreas)
	return nil
}

// SetSupportedMaps records the device's map list (MAPS); it is refused
// when it breaks #assertSupportedMaps.
func (s *Server) SetSupportedMaps(maps []Map) error {
	if !s.Serves(sa.AttrSupportedMaps) {
		return fmt.Errorf("%w: SupportedMaps needs MAPS", ErrConfig)
	}
	s.mu.Lock()
	if err := s.checkMaps(maps); err != nil {
		s.mu.Unlock()
		return err
	}
	s.maps = slices.Clone(maps)
	s.mu.Unlock()
	s.changed(sa.AttrSupportedMaps)
	return nil
}

// SetSelectedAreas records a selection the device made; it is refused
// when it breaks #assertSelectedAreas.
func (s *Server) SetSelectedAreas(ids []uint32) error {
	s.mu.Lock()
	if err := s.checkSelected(ids); err != nil {
		s.mu.Unlock()
		return err
	}
	s.selected = slices.Clone(ids)
	s.mu.Unlock()
	s.changed(sa.AttrSelectedAreas)
	return nil
}

// SetCurrentArea records where the device is; nil is null, which also
// makes EstimatedEndTime null (#assertCurrentArea).
func (s *Server) SetCurrentArea(id *uint32) error {
	if !s.Serves(sa.AttrCurrentArea) {
		return fmt.Errorf("%w: CurrentArea is not served", ErrConfig)
	}
	s.mu.Lock()
	if err := s.checkCurrent(id); err != nil {
		s.mu.Unlock()
		return err
	}
	s.current = clonePtr(id)
	s.mu.Unlock()
	s.changed(sa.AttrCurrentArea)
	if id == nil && s.Serves(sa.AttrEstimatedEndTime) {
		return s.SetEstimatedEndTime(nil)
	}
	return nil
}

// SetEstimatedEndTime records when the device expects to finish the
// current area, in seconds since the Unix epoch; nil is null. The change
// is reported per matter.js's shouldEmit: at once to or from null or 0 or
// when it decreases, otherwise through the quiet throttle.
func (s *Server) SetEstimatedEndTime(t *uint32) error {
	if !s.Serves(sa.AttrEstimatedEndTime) {
		return fmt.Errorf("%w: EstimatedEndTime is not served", ErrConfig)
	}
	s.mu.Lock()
	old := s.endTime
	if (old == nil) == (t == nil) && (old == nil || *old == *t) {
		s.mu.Unlock()
		return nil
	}
	s.endTime = clonePtr(t)
	s.mu.Unlock()
	now := old == nil || *old == 0 || t == nil || *t == 0 || *t < *old
	// Quieter reports at once for a change from or to null; "now" covers
	// 0 and a decrease as well.
	s.quiet.Changed(now, t == nil)
	return nil
}

// SetProgress records the device's progress (PROG); it is refused when it
// breaks #assertProgress.
func (s *Server) SetProgress(progress []Progress) error {
	if !s.Serves(sa.AttrProgress) {
		return fmt.Errorf("%w: Progress needs PROG", ErrConfig)
	}
	s.mu.Lock()
	if err := s.checkProgress(progress); err != nil {
		s.mu.Unlock()
		return err
	}
	s.progress = slices.Clone(progress)
	s.mu.Unlock()
	s.changed(sa.AttrProgress)
	return nil
}

// RemoveSupportedAreasEntry removes an area and everything that names it:
// it leaves SelectedAreas and Progress, and CurrentArea becomes null when
// it was the area — matter.js removeSupportedAreasEntry (:257-270).
func (s *Server) RemoveSupportedAreasEntry(areaID uint32) {
	s.mu.Lock()
	if len(s.areas) == 0 {
		s.mu.Unlock()
		return
	}
	changed := s.removeAreaLocked(areaID)
	s.mu.Unlock()
	s.report(changed)
}

func (s *Server) removeAreaLocked(areaID uint32) []uint32 {
	changed := []uint32{sa.AttrSupportedAreas, sa.AttrSelectedAreas}
	s.selected = slices.DeleteFunc(slices.Clone(s.selected), func(id uint32) bool { return id == areaID })
	if s.current != nil && *s.current == areaID {
		s.current = nil
		changed = append(changed, sa.AttrCurrentArea)
	}
	if s.Serves(sa.AttrProgress) {
		s.progress = slices.DeleteFunc(slices.Clone(s.progress), func(p Progress) bool { return p.AreaId == areaID })
		changed = append(changed, sa.AttrProgress)
	}
	s.areas = slices.DeleteFunc(slices.Clone(s.areas), func(a Area) bool { return a.AreaId == areaID })
	return changed
}

// report sends one report for the attributes a removal touched, and the
// EstimatedEndTime reset a null CurrentArea brings.
func (s *Server) report(changed []uint32) {
	slices.Sort(changed)
	s.changed(slices.Compact(changed)...)
	if slices.Contains(changed, sa.AttrCurrentArea) && s.Serves(sa.AttrEstimatedEndTime) {
		_ = s.SetEstimatedEndTime(nil) // served: checked above
	}
}

// RemoveSupportedMapsEntry removes a map and the first area that lies on
// it — matter.js removeSupportedMapsEntry (:276-289), which finds one
// affected area, not every one.
func (s *Server) RemoveSupportedMapsEntry(mapID uint32) {
	s.mu.Lock()
	idx := slices.IndexFunc(s.maps, func(m Map) bool { return m.MapId == mapID })
	if idx < 0 {
		s.mu.Unlock()
		return
	}
	var changed []uint32
	if a := slices.IndexFunc(s.areas, func(a Area) bool { return !a.MapId.Null && a.MapId.Value == mapID }); a >= 0 {
		changed = s.removeAreaLocked(s.areas[a].AreaId)
	}
	s.maps = slices.Delete(slices.Clone(s.maps), idx, idx+1)
	changed = append(changed, sa.AttrSupportedMaps)
	s.mu.Unlock()
	s.report(changed)
}
