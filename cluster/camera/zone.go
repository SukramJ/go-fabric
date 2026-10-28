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

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
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
	// MaxZones; MaxZones is at least 1; SensitivityMax is 2 to 10 (chip
	// ValidateConfiguration, ZoneManagementCluster.cpp:85-106).
	MaxUserDefinedZones uint8
	MaxZones            uint8
	SensitivityMax      uint8
	// TwoDCartesianMax (TWODCART) bounds every vertex.
	TwoDCartesianMax Vertex
	// Sensitivity is the value the host persisted, 1 to SensitivityMax;
	// 0 starts at chip's 1 (ZoneManagementCluster.h:135).
	Sensitivity uint8
	// Zones and Triggers are the ones the host persisted, chip's
	// LoadZones and LoadTriggers (:113-172).
	Zones    []Zone
	Triggers []ZoneTrigger
	// Port is the zone and trigger store; required.
	Port ZonePort
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Zones implements [contract.ClusterServer] for ZoneManagement: chip's
// server logic over the generated server — the zones and triggers it
// keeps, the validation of a zone (its constraints, the vertex bounds, a
// duplicate, a self-intersecting polygon) and of a trigger before the
// host's [ZonePort] stores it, the user-defined zone budget, the
// Sensitivity write, and the ZoneTriggered and ZoneStopped events the host
// reports. The rules are connectedhomeip's ZoneManagementCluster.cpp and
// zone-geometry.h at 6170af84 (matter.js's server is the generated
// behaviour).
type Zones struct {
	*spec.Instance
	spec.SelfHeld

	srv  *spec.Server
	port ZonePort
	cfg  ZonesConfig

	// mu guards the state below.
	mu          sync.Mutex
	endpoint    uint16
	zones       []Zone
	triggers    []ZoneTrigger
	userDefined int
	sensitivity uint8
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
// rules are chip's Startup (ZoneManagementCluster.cpp:60-106).
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
	z.zones = slices.Clone(cfg.Zones)
	z.triggers = slices.Clone(cfg.Triggers)
	z.userDefined = min(countUserZones(z.zones), int(cfg.MaxUserDefinedZones)) // :135-136
	handlers := map[uint32]spec.CommandHandler{
		zmdef.CmdCreateTwoDCartesianZone: z.createTwoDCartesianZone,
		zmdef.CmdUpdateTwoDCartesianZone: z.updateTwoDCartesianZone,
		zmdef.CmdRemoveZone:              z.removeZone,
		zmdef.CmdCreateOrUpdateTrigger:   z.createOrUpdateTrigger,
		zmdef.CmdRemoveTrigger:           z.removeTrigger,
	}
	srv, err := spec.NewServer(zmdef.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{
		Handlers: handlers, DataVersion: cfg.DataVersion, BeforeWrite: z.beforeWrite, Sink: zonesSink{z},
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

func countUserZones(zones []Zone) int {
	n := 0
	for _, z := range zones {
		if z.ZoneSource == zmdef.ZoneSourceUser {
			n++
		}
	}
	return n
}

// ZoneList returns Zones.
func (z *Zones) ZoneList() []Zone {
	z.mu.Lock()
	defer z.mu.Unlock()
	return slices.Clone(z.zones)
}

// MaxZones returns MaxZones.
func (z *Zones) MaxZones() uint8 { return z.cfg.MaxZones }

// TriggerList returns Triggers.
func (z *Zones) TriggerList() []ZoneTrigger {
	z.mu.Lock()
	defer z.mu.Unlock()
	return slices.Clone(z.triggers)
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
	return z.triggers[i], true
}

// Sensitivity returns Sensitivity.
func (z *Zones) Sensitivity() uint8 {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.sensitivity
}

// beforeWrite holds a write of Sensitivity to 1..SensitivityMax
// (CONSTRAINT_ERROR). Mirrors chip WriteAttribute
// (ZoneManagementCluster.cpp:259-284); the definition's own constraint
// says the same.
func (z *Zones) beforeWrite(_ context.Context, attrID uint32, value any) error {
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

// publish stores Zones or Triggers at their current value.
func (z *Zones) publish(ids ...uint32) {
	z.mu.Lock()
	values := map[uint32]any{}
	for _, id := range ids {
		switch id {
		case zmdef.AttrZones:
			values[id] = spec.List[Zone](nonNil(z.zones))
		case zmdef.AttrTriggers:
			values[id] = spec.List[ZoneTrigger](nonNil(z.triggers))
		}
	}
	z.mu.Unlock()
	_ = z.srv.SetAttributesFrom(contract.OriginCommand, values)
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
// (DYNAMIC_CONSTRAINT_ERROR, :477-486, :542-551), a duplicate zone of the
// same use and vertices other than exclude (ALREADY_EXISTS, :495-498,
// :560-563) and a self-intersecting polygon (DYNAMIC_CONSTRAINT_ERROR,
// :500-504, :565-569), in chip's order.
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

// zoneExists is chip's ZoneAlreadyExists (:661-699): a zone other than
// exclude with the use and the same vertices in the same order.
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

func cloneTwoDZone(zone TwoDZone) *TwoDZone {
	c := zone
	c.Vertices = slices.Clone(zone.Vertices)
	if zone.Color != nil {
		color := *zone.Color
		c.Color = &color
	}
	return &c
}

// createTwoDCartesianZone carries out CreateTwoDCartesianZone. Mirrors
// chip HandleCreateTwoDCartesianZone (ZoneManagementCluster.cpp:462-521):
// the zone's checks, a Focus zone only with FOCUSZONES (CONSTRAINT_ERROR),
// the user-defined budget (RESOURCE_EXHAUSTED), the vertex checks, then
// the port's id and the zone added as a user-defined 2D Cartesian zone.
func (z *Zones) createTwoDCartesianZone(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.CreateTwoDCartesianZoneRequest](fields, "CreateTwoDCartesianZone")
	if err != nil {
		return nil, err
	}
	if err := validateTwoDZone(req.Zone); err != nil {
		return nil, err
	}
	if req.Zone.Use == zmdef.ZoneUseFocus && !z.HasFeature("FOCUSZONES") { // :472
		return nil, spec.Errorf(im.StatusConstraintError, "ZoneManagement: a Focus zone without FOCUSZONES")
	}
	z.mu.Lock()
	full := z.userDefined >= int(z.cfg.MaxUserDefinedZones)
	z.mu.Unlock()
	if full { // :473
		return nil, spec.Errorf(im.StatusResourceExhausted, "ZoneManagement: %d user-defined zones", z.cfg.MaxUserDefinedZones)
	}
	if err := z.checkVertices(req.Zone, nil); err != nil {
		return nil, err
	}
	id, err := z.port.CreateTwoDCartesianZone(ctx, req.Zone) // :509
	if err != nil {
		return nil, portError("CreateTwoDCartesianZone", err)
	}
	z.mu.Lock()
	z.zones = append(z.zones, Zone{ // :512-515
		ZoneId: id, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceUser,
		TwoDCartesianZone: cloneTwoDZone(req.Zone),
	})
	z.userDefined++
	z.mu.Unlock()
	z.publish(zmdef.AttrZones)
	return zmdef.CreateTwoDCartesianZoneResponse{ZoneId: id}, nil
}

// updateTwoDCartesianZone carries out UpdateTwoDCartesianZone. Mirrors
// chip HandleUpdateTwoDCartesianZone (:523-582): the zone's checks; an
// unknown zone is NOT_FOUND, a manufacturer's CONSTRAINT_ERROR, one of
// another type DYNAMIC_CONSTRAINT_ERROR; the vertex checks; then the
// port, and the zone replaced as user-defined.
func (z *Zones) updateTwoDCartesianZone(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.UpdateTwoDCartesianZoneRequest](fields, "UpdateTwoDCartesianZone")
	if err != nil {
		return nil, err
	}
	if err := validateTwoDZone(req.Zone); err != nil {
		return nil, err
	}
	z.mu.Lock()
	i := slices.IndexFunc(z.zones, func(zone Zone) bool { return zone.ZoneId == req.ZoneId })
	var found Zone
	if i >= 0 {
		found = z.zones[i]
	}
	z.mu.Unlock()
	switch {
	case i < 0: // :534
		return nil, spec.Errorf(im.StatusNotFound, "ZoneManagement: zone %d not found", req.ZoneId)
	case found.ZoneSource == zmdef.ZoneSourceMfg: // :537
		return nil, spec.Errorf(im.StatusConstraintError, "ZoneManagement: zone %d is the manufacturer's", req.ZoneId)
	case found.ZoneType != zmdef.ZoneTypeTwoDcartZone: // :538
		return nil, spec.Errorf(im.StatusDynamicConstraintError, "ZoneManagement: zone %d is no 2D Cartesian zone", req.ZoneId)
	}
	if err := z.checkVertices(req.Zone, &req.ZoneId); err != nil {
		return nil, err
	}
	if err := z.port.UpdateTwoDCartesianZone(ctx, req.ZoneId, req.Zone); err != nil { // :574
		return nil, portError("UpdateTwoDCartesianZone", err)
	}
	z.mu.Lock()
	if i := slices.IndexFunc(z.zones, func(zone Zone) bool { return zone.ZoneId == req.ZoneId }); i >= 0 { // :577-579
		z.zones[i] = Zone{
			ZoneId: req.ZoneId, ZoneType: zmdef.ZoneTypeTwoDcartZone, ZoneSource: zmdef.ZoneSourceUser,
			TwoDCartesianZone: cloneTwoDZone(req.Zone),
		}
	}
	z.mu.Unlock()
	z.publish(zmdef.AttrZones)
	return nil, nil
}

// removeZone carries out RemoveZone. Mirrors chip HandleRemoveZone
// (:584-610): an unknown zone is NOT_FOUND, one not user-defined
// CONSTRAINT_ERROR, one with a trigger INVALID_IN_STATE; then the port.
func (z *Zones) removeZone(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.RemoveZoneRequest](fields, "RemoveZone")
	if err != nil {
		return nil, err
	}
	z.mu.Lock()
	i := slices.IndexFunc(z.zones, func(zone Zone) bool { return zone.ZoneId == req.ZoneId })
	var found Zone
	if i >= 0 {
		found = z.zones[i]
	}
	triggered := slices.ContainsFunc(z.triggers, func(t ZoneTrigger) bool { return t.ZoneId == req.ZoneId })
	z.mu.Unlock()
	switch {
	case i < 0: // :590
		return nil, spec.Errorf(im.StatusNotFound, "ZoneManagement: zone %d not found", req.ZoneId)
	case found.ZoneSource != zmdef.ZoneSourceUser: // :593
		return nil, spec.Errorf(im.StatusConstraintError, "ZoneManagement: zone %d is not user-defined", req.ZoneId)
	case triggered: // :595-598
		return nil, spec.Errorf(im.StatusInvalidInState, "ZoneManagement: zone %d has a trigger", req.ZoneId)
	}
	if err := z.port.RemoveZone(ctx, req.ZoneId); err != nil { // :600
		return nil, portError("RemoveZone", err)
	}
	z.mu.Lock()
	z.zones = slices.DeleteFunc(z.zones, func(zone Zone) bool { return zone.ZoneId == req.ZoneId })
	if z.userDefined > 0 { // :604-607
		z.userDefined--
	}
	z.mu.Unlock()
	z.publish(zmdef.AttrZones)
	return nil, nil
}

// validateTrigger is chip's ValidateTrigger (:612-627): InitialDuration at
// least 1, AugmentationDuration at most and MaxDuration at least
// InitialDuration (CONSTRAINT_ERROR); a Sensitivity exactly with
// PERZONESENS (INVALID_COMMAND), 1 to SensitivityMax (CONSTRAINT_ERROR).
func (z *Zones) validateTrigger(t ZoneTrigger) error {
	switch {
	case t.InitialDuration < 1, t.AugmentationDuration > t.InitialDuration, t.MaxDuration < t.InitialDuration: // :614-616
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: trigger durations %d/%d/%d", t.InitialDuration, t.AugmentationDuration, t.MaxDuration)
	case z.HasFeature("PERZONESENS") != (t.Sensitivity != nil): // :618
		return spec.Errorf(im.StatusInvalidCommand, "ZoneManagement: trigger Sensitivity does not match PERZONESENS")
	case t.Sensitivity != nil && (*t.Sensitivity < 1 || *t.Sensitivity > z.cfg.SensitivityMax): // :619-624
		return spec.Errorf(im.StatusConstraintError, "ZoneManagement: trigger Sensitivity %d outside 1 to %d", *t.Sensitivity, z.cfg.SensitivityMax)
	}
	return nil
}

// createOrUpdateTrigger carries out CreateOrUpdateTrigger. Mirrors chip
// HandleCreateOrUpdateTrigger (:629-647) and AddOrUpdateTrigger
// (:402-429): the trigger's checks; an unknown zone is NOT_FOUND, a 2D
// zone of another use than Motion CONSTRAINT_ERROR; then the port creates
// or updates it.
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
	case i < 0: // :639
		return nil, spec.Errorf(im.StatusNotFound, "ZoneManagement: zone %d not found", t.ZoneId)
	case found.TwoDCartesianZone != nil && found.TwoDCartesianZone.Use != zmdef.ZoneUseMotion: // :642-644
		return nil, spec.Errorf(im.StatusConstraintError, "ZoneManagement: zone %d is no Motion zone", t.ZoneId)
	}
	if t.Sensitivity != nil {
		s := *t.Sensitivity
		t.Sensitivity = &s
	}
	if exists { // :417-424
		if err := z.port.UpdateTrigger(ctx, t); err != nil {
			return nil, portError("UpdateTrigger", err)
		}
	} else if err := z.port.CreateTrigger(ctx, t); err != nil { // :409-416
		return nil, portError("CreateTrigger", err)
	}
	z.mu.Lock()
	if j := slices.IndexFunc(z.triggers, func(x ZoneTrigger) bool { return x.ZoneId == t.ZoneId }); j >= 0 {
		z.triggers[j] = t
	} else {
		z.triggers = append(z.triggers, t)
	}
	z.mu.Unlock()
	z.publish(zmdef.AttrTriggers)
	return nil, nil
}

// removeTrigger carries out RemoveTrigger. Mirrors chip
// HandleRemoveTrigger (:649-659) and RemoveTrigger (:431-443): an unknown
// trigger is NOT_FOUND; then the port.
func (z *Zones) removeTrigger(ctx context.Context, fields any) (any, error) {
	req, err := zoneRequestOf[zmdef.RemoveTriggerRequest](fields, "RemoveTrigger")
	if err != nil {
		return nil, err
	}
	if _, ok := z.TriggerFor(req.ZoneId); !ok { // :653-656
		return nil, spec.Errorf(im.StatusNotFound, "ZoneManagement: no trigger for zone %d", req.ZoneId)
	}
	if err := z.port.RemoveTrigger(ctx, req.ZoneId); err != nil { // :433
		return nil, portError("RemoveTrigger", err)
	}
	z.mu.Lock()
	z.triggers = slices.DeleteFunc(z.triggers, func(t ZoneTrigger) bool { return t.ZoneId == req.ZoneId })
	z.mu.Unlock()
	z.publish(zmdef.AttrTriggers)
	return nil, nil
}

// ZoneTriggered emits the ZoneTriggered event the host reports for a
// zone. Mirrors chip GenerateZoneTriggeredEvent (:701-716).
func (z *Zones) ZoneTriggered(zoneID uint16, reason ZoneTriggeredReason) error {
	z.mu.Lock()
	endpoint := z.endpoint
	z.mu.Unlock()
	return z.srv.Emit(endpoint, zmdef.EventZoneTriggered, zmdef.ZoneTriggeredEvent{Zone: zoneID, Reason: reason})
}

// ZoneStopped emits the ZoneStopped event the host reports for a zone.
// Mirrors chip GenerateZoneStoppedEvent (:718-733).
func (z *Zones) ZoneStopped(zoneID uint16, reason ZoneStoppedReason) error {
	z.mu.Lock()
	endpoint := z.endpoint
	z.mu.Unlock()
	return z.srv.Emit(endpoint, zmdef.EventZoneStopped, zmdef.ZoneStoppedEvent{Zone: zoneID, Reason: reason})
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
}
