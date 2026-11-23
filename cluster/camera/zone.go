// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"thingfabric.dev/go-fabric/internal/deepcopy"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	zmdef "thingfabric.dev/go-fabric/cluster/spec/zonemanagement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// ZonesClusterID is the ZoneManagement cluster id.
const ZonesClusterID = zmdef.ClusterID

// Generated types the zone management speaks.
type (
	// ZoneFeature is a FeatureMap bit of ZoneManagement.
	ZoneFeature = zmdef.Feature
	// Zone is an entry of Zones.
	Zone = zmdef.ZoneInformationStruct
	// TwoDZone is a two-dimensional Cartesian zone: name, use, vertices
	// and colour.
	TwoDZone = zmdef.TwoDCartesianZoneStruct
	// Vertex is a point of a two-dimensional Cartesian zone.
	Vertex = zmdef.TwoDCartesianVertexStruct
	// ZoneTrigger is an entry of Triggers.
	ZoneTrigger = zmdef.ZoneTriggerControlStruct
	// ZonePort is the camera's zone and trigger store.
	ZonePort = zmdef.ZonePort
	// ZoneTriggeredReason is the reason of a ZoneTriggered event.
	ZoneTriggeredReason = zmdef.ZoneEventTriggeredReasonEnum
	// ZoneStoppedReason is the reason of a ZoneStopped event.
	ZoneStoppedReason = zmdef.ZoneEventStoppedReasonEnum
)

// The features.
const (
	FeatureTwoDimensionalCartesianZone = zmdef.FeatureTwoDimensionalCartesianZone
	FeaturePerZoneSensitivity          = zmdef.FeaturePerZoneSensitivity
	FeatureUserDefined                 = zmdef.FeatureUserDefined
	FeatureFocusZones                  = zmdef.FeatureFocusZones
)

// Configuration errors.
var (
	// ErrZonesConfig reports a configuration chip's Startup refuses or the
	// cluster's model does not hold.
	ErrZonesConfig = errors.New("camera: ZoneManagement configuration outside the cluster's model")
	// ErrNoZonePort reports a ZoneManagement built without its port.
	ErrNoZonePort = errors.New("camera: ZoneManagement needs a ZonePort")
)

// ZonesConfig carries the construction parameters of a [Zones]. The field
// names are the attributes' they set.
type ZonesConfig struct {
	Features ZoneFeature
	// MaxUserDefinedZones (USERDEFINED) is at least 5 and at most
	// MaxZones; MaxZones is at least 1; SensitivityMax is 2 to 10 — the
	// model's constraints (matter.js zone-management.element.ts:32-33,
	// :35, :54-55 at b25ed960), checked as chip's ValidateConfiguration
	// does (ZoneManagementCluster.cpp:85-106 at 6170af84).
	MaxUserDefinedZones uint8
	MaxZones            uint8
	SensitivityMax      uint8
	// TwoDCartesianMax (TWODCART) bounds every vertex.
	TwoDCartesianMax Vertex
	// Sensitivity is the value the host persisted, 1 to SensitivityMax
	// (the model's constraint, zone-management.element.ts:58-59 at
	// b25ed960); 0 starts at chip's 1 (ZoneManagementCluster.h:135).
	Sensitivity uint8
	// Zones and Triggers are the ones the host persisted, chip's
	// LoadZones and LoadTriggers (:113-172). A zone ID used twice is
	// refused (ErrZonesConfig).
	Zones    []Zone
	Triggers []ZoneTrigger
	// LastZoneID is the ID the server assigned last, as the host
	// persisted it with Zones: the server gives a new zone the ID after
	// it, skipping IDs in use, so that the ID of a removed zone is not
	// reused. nil asks the port ([LastZoneIDSource]) and, without an
	// answer, continues after the highest ID in use; a value set here wins
	// over the port's. The host learns each ID the server assigns through
	// [ZonePort]'s CreateTwoDCartesianZone and persists it there; matter.js
	// keeps it as the server's nonvolatile lastZoneId
	// (ZoneManagementServer.ts:253-260 at b25ed960).
	LastZoneID *uint16
	// Port is the zone and trigger store; required.
	Port ZonePort
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// Zones implements [contract.ClusterServer] for ZoneManagement over the
// generated server: the zones and triggers it keeps, the zone ID it
// assigns, the validation of a zone (its constraints, the vertex bounds,
// a duplicate, a self-intersecting polygon) and of a trigger before the
// host's [ZonePort] stores it, the zone limits, the Sensitivity write, and
// the ZoneTriggered and ZoneStopped events the host reports. The command
// rules are matter.js's ZoneManagementServer.ts and
// TwoDCartesianGeometry.ts at b25ed960; the configuration limits and the
// Sensitivity write are the model's constraints
// (zone-management.element.ts:32-33, :35, :54-55, :58-59 at b25ed960),
// checked as connectedhomeip's ValidateConfiguration and WriteAttribute
// do (ZoneManagementCluster.cpp at 6170af84).
type Zones struct {
	*spec.Instance
	spec.SelfHeld

	srv  *spec.Server
	port ZonePort
	cfg  ZonesConfig

	// op serialises the commands from their checks through the port to
	// their commit ([serial]); pub serialises each change of Zones or
	// Triggers with its publication; mu guards the state below.
	op          sync.Mutex
	pub         sync.Mutex
	mu          sync.Mutex
	endpoint    uint16
	zones       []Zone
	triggers    []ZoneTrigger
	lastZoneID  *uint16
	sensitivity uint8
	// refuseForTests, nil in production, stands for a table the schema
	// refuses: a non-nil error from it fails [Zones.change] before the
	// store, as a refused value does.
	refuseForTests func() error
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Zones)(nil)
	_ contract.ClusterDataVersion             = (*Zones)(nil)
	_ contract.ClusterAttributeLister         = (*Zones)(nil)
	_ contract.ClusterCommandLister           = (*Zones)(nil)
	_ contract.ClusterEventLister             = (*Zones)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Zones)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Zones)(nil)
	_ contract.AttributeChangeNotifier        = (*Zones)(nil)
	_ contract.EventReceiver                  = (*Zones)(nil)
	_ spec.AttributeSetter                    = (*Zones)(nil)
	_ spec.CommandRegistrar                   = (*Zones)(nil)
)

// NewZones builds a ZoneManagement server on cfg.Port. The configuration
// rules are the model's constraints, checked as chip's Startup does
// (ZoneManagementCluster.cpp:60-106 at 6170af84), and matter.js's
// initialize (ZoneManagementServer.ts:70-78 at b25ed960): MaxZones not
// below MaxUserDefinedZones with USERDEFINED, and no zone ID used twice.
// The last assigned zone ID is cfg.LastZoneID, else the port's
// [LastZoneIDSource] answer, read once here.
func NewZones(cfg ZonesConfig) (*Zones, error) {
	if cfg.Port == nil {
		return nil, ErrNoZonePort
	}
	if cfg.Features&FeatureUserDefined != 0 {
		if cfg.MaxUserDefinedZones < 5 || cfg.MaxZones < cfg.MaxUserDefinedZones { // :89-93
			return nil, fmt.Errorf("%w: MaxUserDefinedZones %d, MaxZones %d", ErrZonesConfig, cfg.MaxUserDefinedZones, cfg.MaxZones)
		}
	} else if cfg.MaxZones < 1 { // :97-98
		return nil, fmt.Errorf("%w: MaxZones %d", ErrZonesConfig, cfg.MaxZones)
	}
	if cfg.SensitivityMax < 2 || cfg.SensitivityMax > 10 { // :101-103
		return nil, fmt.Errorf("%w: SensitivityMax %d", ErrZonesConfig, cfg.SensitivityMax)
	}
	z := &Zones{port: cfg.Port, cfg: cfg, sensitivity: 1}
	if cfg.Sensitivity != 0 {
		if cfg.Sensitivity > cfg.SensitivityMax { // :63-68
			return nil, fmt.Errorf("%w: Sensitivity %d", ErrZonesConfig, cfg.Sensitivity)
		}
		z.sensitivity = cfg.Sensitivity
	}
	if id, ok := repeatedZoneID(cfg.Zones); ok { // ZoneManagementServer.ts:76, :178-186
		return nil, fmt.Errorf("%w: ZoneID %d is not unique", ErrZonesConfig, id)
	}
	z.zones = deepcopy.Each(cfg.Zones)
	z.triggers = deepcopy.Each(cfg.Triggers)
	switch src, ok := cfg.Port.(LastZoneIDSource); {
	case cfg.LastZoneID != nil:
		last := *cfg.LastZoneID
		z.lastZoneID = &last
	case ok:
		if last, known := src.LastZoneID(); known {
			z.lastZoneID = &last
		}
	}
	handlers := map[uint32]spec.CommandHandler{
		zmdef.CmdCreateTwoDCartesianZone: serial(&z.op, z.createTwoDCartesianZone),
		zmdef.CmdUpdateTwoDCartesianZone: serial(&z.op, z.updateTwoDCartesianZone),
		zmdef.CmdRemoveZone:              serial(&z.op, z.removeZone),
		zmdef.CmdCreateOrUpdateTrigger:   serial(&z.op, z.createOrUpdateTrigger),
		zmdef.CmdRemoveTrigger:           serial(&z.op, z.removeTrigger),
	}
	srv, err := spec.NewServer(zmdef.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{
		Handlers: handlers, DataVersion: cfg.DataVersion, WriteRule: z.writeRule, Sink: zonesSink{z}, //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrZonesConfig, err)
	}
	z.srv, z.Instance, z.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)
	values := map[uint32]any{
		zmdef.AttrMaxUserDefinedZones: cfg.MaxUserDefinedZones,
		zmdef.AttrMaxZones:            cfg.MaxZones,
		zmdef.AttrSensitivityMax:      cfg.SensitivityMax,
		zmdef.AttrTwoDCartesianMax:    cfg.TwoDCartesianMax,
		zmdef.AttrSensitivity:         z.sensitivity,
		zmdef.AttrZones:               spec.List[Zone](nonNil(z.zones)),
		zmdef.AttrTriggers:            spec.List[ZoneTrigger](nonNil(z.triggers)),
	}
	served := make(map[uint32]any, len(values))
	for id, v := range values {
		if z.Serves(id) {
			served[id] = v
		}
	}
	if err := srv.SetAttributes(served); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrZonesConfig, err)
	}
	return z, nil
}

// LastZoneIDSource is the optional interface of a [ZonePort] that keeps
// the last zone ID the server assigned — the ID its
// CreateTwoDCartesianZone received last, persisted with the zones. The
// server finds it on the port by type assertion and reads it once, when
// it is built ([NewZones]), where [ZonesConfig].LastZoneID is nil: the
// path of a server built through the device facade
// (zonemanagement.Declare.ZonePort), which carries no LastZoneID. known
// false continues after the highest ID in use. matter.js keeps the ID as
// the server's nonvolatile lastZoneId (ZoneManagementServer.ts:253-260 at
// b25ed960); here the host holds it (owner decisions O-1, O-7).
type LastZoneIDSource interface {
	LastZoneID() (zoneID uint16, known bool)
}

// repeatedZoneID reports the first zone ID zones use twice. Mirrors
// matter.js ZoneManagementServer.ts:178-186 at b25ed960
// (#assertUniqueZoneIds).
func repeatedZoneID(zones []Zone) (uint16, bool) {
	seen := make(map[uint16]bool, len(zones))
	for _, z := range zones {
		if seen[z.ZoneId] {
			return z.ZoneId, true
		}
		seen[z.ZoneId] = true
	}
	return 0, false
}

// maxZoneID is the highest zone ID the server assigns; the next ID after
// it is 0 (matter.js ZoneManagementServer.ts:24 at b25ed960, MAX_ZONE_ID).
const maxZoneID = 0xfffe

// nextZoneID is the ID of a new zone: the one after last, skipping IDs in
// use, so that a new zone does not take the ID of a zone just removed;
// without a last ID, the one after the highest ID in use, because
// manufacturer zones count toward the same sequence; 0 after maxZoneID.
// Mirrors matter.js ZoneManagementServer.ts:236-250 at b25ed960
// (#nextZoneId). The caller holds the zone limit, so an ID is free.
func nextZoneID(zones []Zone, last *uint16) uint16 {
	used := make(map[uint16]bool, len(zones))
	id := -1
	for _, z := range zones {
		used[z.ZoneId] = true
		id = max(id, int(z.ZoneId))
	}
	if last != nil {
		id = int(*last)
	}
	for {
		if id >= maxZoneID {
			id = 0
		} else {
			id++
		}
		if !used[uint16(id)] { //nolint:gosec // 0 to maxZoneID
			return uint16(id) //nolint:gosec // 0 to maxZoneID
		}
	}
}

func countUserZones(zones []Zone) int {
	n := 0
	for _, z := range zones {
		if z.ZoneSource == zmdef.ZoneSourceUser {
			n++
		}
	}
	return n
}

// ZoneList returns Zones: a deep copy, which the caller may change.
func (z *Zones) ZoneList() []Zone {
	z.mu.Lock()
	defer z.mu.Unlock()
	return deepcopy.Each(z.zones)
}

// MaxZones returns MaxZones.
func (z *Zones) MaxZones() uint8 { return z.cfg.MaxZones }

// TriggerList returns Triggers: a deep copy, which the caller may change.
func (z *Zones) TriggerList() []ZoneTrigger {
	z.mu.Lock()
	defer z.mu.Unlock()
	return deepcopy.Each(z.triggers)
}

// TriggerFor returns the trigger of a zone (chip GetTriggerForZone,
// :194-205).
func (z *Zones) TriggerFor(zoneID uint16) (ZoneTrigger, bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	i := slices.IndexFunc(z.triggers, func(t ZoneTrigger) bool { return t.ZoneId == zoneID })
	if i < 0 {
		return ZoneTrigger{}, false
	}
	return z.triggers[i].Clone(), true
}

// Sensitivity returns Sensitivity.
func (z *Zones) Sensitivity() uint8 {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.sensitivity
}

// writeRule holds a write of Sensitivity to 1..SensitivityMax
// (CONSTRAINT_ERROR). Mirrors chip WriteAttribute
// (ZoneManagementCluster.cpp:259-284); the definition's own constraint
// says the same.
func (z *Zones) writeRule(_ context.Context, attrID uint32, value any) error {
	if attrID != zmdef.AttrSensitivity {
		return nil
	}
	if v, _ := value.(uint8); v < 1 || v > z.cfg.SensitivityMax { // :262
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: Sensitivity %d outside 1 to %d", v, z.cfg.SensitivityMax)
	}
	return nil
}

// zonesSink takes a written Sensitivity; an unchanged value is stored
// without a report (chip :264, kWriteSuccessNoOp).
type zonesSink struct{ z *Zones }

// MatterWriteAttribute implements [spec.Sink].
func (k zonesSink) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	if attrID == zmdef.AttrSensitivity {
		v, _ := value.(uint8)
		k.z.mu.Lock()
		k.z.sensitivity = v
		k.z.mu.Unlock()
	}
	return nil
}

// SetSensitivity is chip's SetSensitivity (:207-224): the host's change,
// 1 to SensitivityMax ([spec.ErrInvalidValue] otherwise).
func (z *Zones) SetSensitivity(v uint8) error {
	return z.SetAttributes(map[uint32]any{zmdef.AttrSensitivity: v})
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change
// of Sensitivity (1 to SensitivityMax); the limits are the
// configuration's and Zones and Triggers the server's ([spec.ErrNotHeld]).
func (z *Zones) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		if id != zmdef.AttrSensitivity {
			return fmt.Errorf("%w: camera: ZoneManagement attribute 0x%04X is the server's", spec.ErrNotHeld, id)
		}
	}
	v, ok := values[zmdef.AttrSensitivity]
	if !ok {
		return nil
	}
	checked, err := z.CheckSet(zmdef.AttrSensitivity, v, nil)
	if err != nil {
		return err
	}
	s, _ := checked.(uint8)
	if s < 1 || s > z.cfg.SensitivityMax { // :209-212
		return fmt.Errorf("%w: camera: Sensitivity %d outside 1 to %d", spec.ErrInvalidValue, s, z.cfg.SensitivityMax)
	}
	z.mu.Lock()
	z.sensitivity = s
	z.mu.Unlock()
	return z.srv.SetAttributes(map[uint32]any{zmdef.AttrSensitivity: s})
}

// zoneTables is the state a command changes: Zones, Triggers and the
// last assigned zone ID.
type zoneTables struct {
	zones      []Zone
	triggers   []ZoneTrigger
	lastZoneID *uint16
}

// change applies edit to a copy of the tables and publishes the attribute
// attrID names (Zones or Triggers) and keeps the result — both under pub;
// a table the schema refuses is neither published nor kept, and the
// command answers FAILURE (review II, R2-P6-err-1611).
func (z *Zones) change(attrID uint32, edit func(*zoneTables)) error {
	z.pub.Lock()
	defer z.pub.Unlock()
	z.mu.Lock()
	t := zoneTables{zones: deepcopy.Each(z.zones), triggers: deepcopy.Each(z.triggers), lastZoneID: z.lastZoneID}
	z.mu.Unlock()
	edit(&t)
	if z.refuseForTests != nil {
		if err := z.refuseForTests(); err != nil {
			return commitError(fmt.Errorf("camera: ZoneManagement: %w", err))
		}
	}
	var v any = spec.List[Zone](nonNil(t.zones))
	if attrID == zmdef.AttrTriggers {
		v = spec.List[ZoneTrigger](nonNil(t.triggers))
	}
	if err := z.srv.SetAttributesFrom(contract.OriginCommand, map[uint32]any{attrID: v}); err != nil {
		return commitError(fmt.Errorf("camera: ZoneManagement: %w", err))
	}
	z.mu.Lock()
	z.zones, z.triggers, z.lastZoneID = t.zones, t.triggers, t.lastZoneID
	z.mu.Unlock()
	return nil
}

// validateTwoDZone is chip's ValidateTwoDCartesianZone (:445-460): a name
// of at most 32 characters, a known use, 3 to 12 vertices and a colour of
// 7 or 9 characters, each CONSTRAINT_ERROR otherwise.
func validateTwoDZone(zone TwoDZone) error {
	switch {
	case len(zone.Name) > 32: // :447
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: Name of %d characters", len(zone.Name))
	case zone.Use > zmdef.ZoneUseFocus: // :448
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: unknown Use %d", zone.Use)
	case len(zone.Vertices) < 3 || len(zone.Vertices) > 12: // :450-452
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: %d vertices", len(zone.Vertices))
	case zone.Color != nil && len(*zone.Color) != 7 && len(*zone.Color) != 9: // :454-457
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: Color %q", *zone.Color)
	}
	return nil
}

// checkVertices holds the vertices to TwoDCartesianMax
// (DYNAMIC_CONSTRAINT_ERROR), a duplicate zone of the same use and
// vertices other than exclude (ALREADY_EXISTS) and a self-intersecting
// polygon (DYNAMIC_CONSTRAINT_ERROR), in that order. Mirrors matter.js
// ZoneManagementServer.ts:205-234 at b25ed960 (#assertVertices).
func (z *Zones) checkVertices(zone TwoDZone, exclude *uint16) error {
	maxV := z.cfg.TwoDCartesianMax
	for _, v := range zone.Vertices {
		if v.X > maxV.X || v.Y > maxV.Y {
			return spec.Errorf(im.StatusDynamicConstraintError, "ZoneManagement: vertex (%d,%d) beyond TwoDCartesianMax (%d,%d)", v.X, v.Y, maxV.X, maxV.Y)
		}
	}
	if z.zoneExists(zone.Use, zone.Vertices, exclude) {
		return spec.Errorf(im.StatusAlreadyExists, "ZoneManagement: a zone of the use and vertices exists")
	}
	if ZoneSelfIntersecting(zone.Vertices) {
		return spec.Errorf(im.StatusDynamicConstraintError, "ZoneManagement: the zone intersects itself")
	}
	return nil
}

// zoneExists reports a zone other than exclude with the use and the same
// vertices in the same order (matter.js ZoneManagementServer.ts:218-224,
// TwoDCartesianGeometry.ts:17-22 at b25ed960, verticesEqual).
func (z *Zones) zoneExists(use zmdef.ZoneUseEnum, vertices []Vertex, exclude *uint16) bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	for _, zone := range z.zones {
		if exclude != nil && zone.ZoneId == *exclude {
			continue
		}
		if zone.TwoDCartesianZone != nil && zone.TwoDCartesianZone.Use == use && slices.Equal(zone.TwoDCartesianZone.Vertices, vertices) {
			return true
		}
	}
	return false
}

func zoneRequestOf[T any](fields any, name string) (T, error) {
	req, ok := spec.RequestOf[T](fields)
	if !ok {
		return req, spec.Errorf(im.StatusInvalidCommand, "ZoneManagement: %s payload %T", name, fields)
	}
	return req, nil
}

// assertFocusSupported refuses a Focus zone without FOCUSZONES
// (CONSTRAINT_ERROR). Mirrors matter.js ZoneManagementServer.ts:199-203 at
// b25ed960 (#assertFocusSupported).
func (z *Zones) assertFocusSupported(zone TwoDZone) error {
	if zone.Use == zmdef.ZoneUseFocus && !z.HasFeature("FOCUSZONES") {
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: a Focus zone without FOCUSZONES")
	}
	return nil
}

// createTwoDCartesianZone carries out CreateTwoDCartesianZone. Mirrors
// matter.js ZoneManagementServer.ts:80-107 at b25ed960
// (createTwoDCartesianZone): a Focus zone only with FOCUSZONES,
// RESOURCE_EXHAUSTED once MaxUserDefinedZones user-defined zones or
// MaxZones zones exist, the vertex checks, then the next zone ID
// ([nextZoneID]). The port stores the zone under that ID before the
// server keeps it: a refusal of the port leaves Zones and the last ID as
// they were, as matter.js discards the state of a command an override
// refuses after super (:61-63). The zone's constraints come first:
// matter.js refuses a Name over 32 bytes and fewer than 3 vertices in the
// interaction layer, before the command (ZoneManagementServerTest.ts:756-768
// at b25ed960); Use and Color keep chip's ValidateTwoDCartesianZone
// (ZoneManagementCluster.cpp:445-460 at 6170af84). A zone the port stored
// but the table refuses is removed at the port again before the command
// answers FAILURE ([undoAtPort], R2-C2-err-1602).
func (z *Zones) createTwoDCartesianZone(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.CreateTwoDCartesianZoneRequest](fields, "CreateTwoDCartesianZone")
	if err != nil {
		return nil, err
	}
	if err := validateTwoDZone(req.Zone); err != nil {
		return nil, err
	}
	if err := z.assertFocusSupported(req.Zone); err != nil { // :83
		return nil, err
	}
	z.mu.Lock()
	users, all := countUserZones(z.zones), len(z.zones)
	z.mu.Unlock()
	if users >= int(z.cfg.MaxUserDefinedZones) || all >= int(z.cfg.MaxZones) { // :85-91
		return nil, spec.Errorf(im.StatusResourceExhausted,
			"ZoneManagement: zone limit reached with %d of %d user-defined zones and %d of %d zones",
			users, z.cfg.MaxUserDefinedZones, all, z.cfg.MaxZones)
	}
	if err := z.checkVertices(req.Zone, nil); err != nil { // :93
		return nil, err
	}
	z.mu.Lock()
	id := nextZoneID(z.zones, z.lastZoneID) // :95
	z.mu.Unlock()
	if err := z.port.CreateTwoDCartesianZone(ctx, id, req.Zone); err != nil {
		return nil, portError("CreateTwoDCartesianZone", err)
	}
	zone := req.Zone.Clone()
	if err := z.change(zmdef.AttrZones, func(t *zoneTables) {
		t.zones = append(t.zones, Zone{ // :96-104
			ZoneId: id, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceUser,
			TwoDCartesianZone: &zone,
		})
		t.lastZoneID = &id // :105
	}); err != nil {
		return nil, undoAtPort(err, func() error { return z.port.RemoveZone(ctx, id) })
	}
	return zmdef.CreateTwoDCartesianZoneResponse{ZoneId: id}, nil
}

// requireUserZone is the zone of id: NOT_FOUND for an unknown zone,
// CONSTRAINT_ERROR for one that is not user-defined. Mirrors matter.js
// ZoneManagementServer.ts:188-197 at b25ed960 (#requireUserZone).
func (z *Zones) requireUserZone(id uint16) (Zone, error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	i := slices.IndexFunc(z.zones, func(zone Zone) bool { return zone.ZoneId == id })
	if i < 0 {
		return Zone{}, spec.Errorf(im.StatusNotFound, "ZoneManagement: zone %d not found", id)
	}
	if z.zones[i].ZoneSource != zmdef.ZoneSourceUser {
		return Zone{}, spec.Errorf(im.StatusConstraintError, "ZoneManagement: zone %d is not user-defined", id)
	}
	return z.zones[i], nil
}

// updateTwoDCartesianZone carries out UpdateTwoDCartesianZone. Mirrors
// matter.js ZoneManagementServer.ts:109-121 at b25ed960
// (updateTwoDCartesianZone): the zone's constraints, then an unknown zone
// NOT_FOUND, one not user-defined CONSTRAINT_ERROR, one of another type
// DYNAMIC_CONSTRAINT_ERROR, a Focus zone only with FOCUSZONES, the vertex
// checks; then the port, and the zone's area replaced.
func (z *Zones) updateTwoDCartesianZone(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.UpdateTwoDCartesianZoneRequest](fields, "UpdateTwoDCartesianZone")
	if err != nil {
		return nil, err
	}
	if err := validateTwoDZone(req.Zone); err != nil {
		return nil, err
	}
	found, err := z.requireUserZone(req.ZoneId) // :110
	if err != nil {
		return nil, err
	}
	if found.ZoneType != zmdef.ZoneTypeTwoDcartZone { // :111-113
		return nil, spec.Errorf(im.StatusDynamicConstraintError, "ZoneManagement: zone %d is no 2D Cartesian zone", req.ZoneId)
	}
	if err := z.assertFocusSupported(req.Zone); err != nil { // :115
		return nil, err
	}
	if err := z.checkVertices(req.Zone, &req.ZoneId); err != nil { // :116
		return nil, err
	}
	if err := z.port.UpdateTwoDCartesianZone(ctx, req.ZoneId, req.Zone); err != nil {
		return nil, portError("UpdateTwoDCartesianZone", err)
	}
	zone := req.Zone.Clone()
	if err := z.change(zmdef.AttrZones, func(t *zoneTables) {
		if i := slices.IndexFunc(t.zones, func(zone Zone) bool { return zone.ZoneId == req.ZoneId }); i >= 0 { // :118-120
			t.zones[i].TwoDCartesianZone = &zone
		}
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

// removeZone carries out RemoveZone. Mirrors matter.js
// ZoneManagementServer.ts:123-130 at b25ed960 (removeZone): an unknown
// zone is NOT_FOUND, one not user-defined CONSTRAINT_ERROR, one a trigger
// uses INVALID_IN_STATE; then the port.
func (z *Zones) removeZone(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.RemoveZoneRequest](fields, "RemoveZone")
	if err != nil {
		return nil, err
	}
	if _, err := z.requireUserZone(req.ZoneId); err != nil { // :124
		return nil, err
	}
	z.mu.Lock()
	triggered := slices.ContainsFunc(z.triggers, func(t ZoneTrigger) bool { return t.ZoneId == req.ZoneId })
	z.mu.Unlock()
	if triggered { // :125-127
		return nil, spec.Errorf(im.StatusInvalidInState, "ZoneManagement: zone %d has a trigger", req.ZoneId)
	}
	if err := z.port.RemoveZone(ctx, req.ZoneId); err != nil {
		return nil, portError("RemoveZone", err)
	}
	if err := z.change(zmdef.AttrZones, func(t *zoneTables) {
		t.zones = slices.DeleteFunc(t.zones, func(zone Zone) bool { return zone.ZoneId == req.ZoneId })
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

// validateTrigger holds InitialDuration at least 1, AugmentationDuration
// at most and MaxDuration at least InitialDuration (CONSTRAINT_ERROR); a
// Sensitivity exactly with PERZONESENS (INVALID_COMMAND), 1 to
// SensitivityMax (CONSTRAINT_ERROR). Mirrors matter.js
// ZoneManagementServer.ts:133-152 at b25ed960 (createOrUpdateTrigger).
func (z *Zones) validateTrigger(t ZoneTrigger) error {
	switch {
	case t.InitialDuration < 1, t.AugmentationDuration > t.InitialDuration, t.MaxDuration < t.InitialDuration: // :135-139
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: trigger durations %d/%d/%d", t.InitialDuration, t.AugmentationDuration, t.MaxDuration)
	case z.HasFeature("PERZONESENS") != (t.Sensitivity != nil): // :141-147
		return spec.Errorf(im.StatusInvalidCommand, "ZoneManagement: trigger Sensitivity does not match PERZONESENS")
	case t.Sensitivity != nil && (*t.Sensitivity < 1 || *t.Sensitivity > z.cfg.SensitivityMax): // :148-152
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: trigger Sensitivity %d outside 1 to %d", *t.Sensitivity, z.cfg.SensitivityMax)
	}
	return nil
}

// createOrUpdateTrigger carries out CreateOrUpdateTrigger. Mirrors
// matter.js ZoneManagementServer.ts:132-167 at b25ed960
// (createOrUpdateTrigger): the trigger's checks; an unknown zone is
// NOT_FOUND, a 2D zone of another use than Motion CONSTRAINT_ERROR; then
// the port creates or updates it, and the trigger replaces the zone's or
// is appended.
func (z *Zones) createOrUpdateTrigger(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.CreateOrUpdateTriggerRequest](fields, "CreateOrUpdateTrigger")
	if err != nil {
		return nil, err
	}
	t := req.Trigger
	if err := z.validateTrigger(t); err != nil {
		return nil, err
	}
	z.mu.Lock()
	i := slices.IndexFunc(z.zones, func(zone Zone) bool { return zone.ZoneId == t.ZoneId })
	var found Zone
	if i >= 0 {
		found = z.zones[i]
	}
	exists := slices.ContainsFunc(z.triggers, func(x ZoneTrigger) bool { return x.ZoneId == t.ZoneId })
	z.mu.Unlock()
	switch {
	case i < 0: // :154-157
		return nil, spec.Errorf(im.StatusNotFound, "ZoneManagement: zone %d not found", t.ZoneId)
	case found.TwoDCartesianZone != nil && found.TwoDCartesianZone.Use != zmdef.ZoneUseMotion: // :158-161
		return nil, spec.Errorf(im.StatusConstraintError, "ZoneManagement: zone %d is no Motion zone", t.ZoneId)
	}
	t = t.Clone()
	if exists {
		if err := z.port.UpdateTrigger(ctx, t); err != nil {
			return nil, portError("UpdateTrigger", err)
		}
	} else if err := z.port.CreateTrigger(ctx, t); err != nil {
		return nil, portError("CreateTrigger", err)
	}
	if err := z.change(zmdef.AttrTriggers, func(tt *zoneTables) { // :163-166
		if j := slices.IndexFunc(tt.triggers, func(x ZoneTrigger) bool { return x.ZoneId == t.ZoneId }); j >= 0 {
			tt.triggers[j] = t
		} else {
			tt.triggers = append(tt.triggers, t)
		}
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

// removeTrigger carries out RemoveTrigger. Mirrors matter.js
// ZoneManagementServer.ts:169-176 at b25ed960 (removeTrigger): a zone
// without a trigger is NOT_FOUND; then the port.
func (z *Zones) removeTrigger(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.RemoveTriggerRequest](fields, "RemoveTrigger")
	if err != nil {
		return nil, err
	}
	if _, ok := z.TriggerFor(req.ZoneId); !ok { // :171-173
		return nil, spec.Errorf(im.StatusNotFound, "ZoneManagement: no trigger for zone %d", req.ZoneId)
	}
	if err := z.port.RemoveTrigger(ctx, req.ZoneId); err != nil {
		return nil, portError("RemoveTrigger", err)
	}
	if err := z.change(zmdef.AttrTriggers, func(t *zoneTables) {
		t.triggers = slices.DeleteFunc(t.triggers, func(t ZoneTrigger) bool { return t.ZoneId == req.ZoneId })
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

// ZoneTriggered emits the ZoneTriggered event the host reports for a
// zone. Mirrors chip GenerateZoneTriggeredEvent (:701-716).
func (z *Zones) ZoneTriggered(zoneID uint16, reason ZoneTriggeredReason) error {
	return z.srv.EmitEvent(zmdef.EventZoneTriggered, zmdef.ZoneTriggeredEvent{Zone: zoneID, Reason: reason})
}

// ZoneStopped emits the ZoneStopped event the host reports for a zone.
// Mirrors chip GenerateZoneStoppedEvent (:718-733).
func (z *Zones) ZoneStopped(zoneID uint16, reason ZoneStoppedReason) error {
	return z.srv.EmitEvent(zmdef.EventZoneStopped, zmdef.ZoneStoppedEvent{Zone: zoneID, Reason: reason})
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (z *Zones) MatterDataVersion() uint32 { return z.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (z *Zones) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return z.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute.
func (z *Zones) MatterRead(attrID uint32) (any, bool) { return z.srv.MatterRead(attrID) }

// MatterWrite applies a write of Sensitivity.
func (z *Zones) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return z.srv.MatterWrite(ctx, attrID, value)
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// generated server's write and its verdict.
func (z *Zones) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return z.srv.MatterWriteChanged(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (z *Zones) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &z.SelfHeld, cmdID, fields)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (z *Zones) SetMatterEventEmitter(emitter contract.EventEmitter) {
	z.srv.SetMatterEventEmitter(emitter)
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (z *Zones) SetEndpoint(endpoint uint16) {
	z.mu.Lock()
	z.endpoint = endpoint
	z.mu.Unlock()
	z.SelfHeld.SetEndpoint(endpoint) // the store's EmitEvent, the typed Record<Event>
}
