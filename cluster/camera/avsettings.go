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
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// AvSettingsClusterID is the CameraAvSettingsUserLevelManagement cluster id.
const AvSettingsClusterID = avdef.ClusterID

// Generated types the camera settings speak.
type (
	// AvFeature is a FeatureMap bit of CameraAvSettingsUserLevelManagement.
	AvFeature = avdef.Feature
	// MPTZ is a mechanical pan, tilt and zoom position.
	MPTZ = avdef.MPTZStruct
	// MPTZPreset is a stored position.
	MPTZPreset = avdef.MPTZPresetStruct
	// DPTZStream is a video stream with its digital viewport.
	DPTZStream = avdef.DPTZStruct
	// Viewport is a stream's viewport.
	Viewport = avdef.ViewportStruct
	// PhysicalMovement is the MovementState value.
	PhysicalMovement = avdef.PhysicalMovementEnum
	// MechanicalPTZ moves the camera.
	MechanicalPTZ = avdef.MechanicalPTZ
	// MechanicalPresets is the optional preset interface of a
	// MechanicalPTZ.
	MechanicalPresets = avdef.MechanicalPresets
	// DigitalPTZ changes a stream's viewport.
	DigitalPTZ = avdef.DigitalPTZ
)

// The features.
const (
	FeatureDigitalPTZ        = avdef.FeatureDigitalPtz
	FeatureMechanicalPan     = avdef.FeatureMechanicalPan
	FeatureMechanicalTilt    = avdef.FeatureMechanicalTilt
	FeatureMechanicalZoom    = avdef.FeatureMechanicalZoom
	FeatureMechanicalPresets = avdef.FeatureMechanicalPresets
)

// The limits and defaults of chip's
// CameraAvSettingsUserLevelManagementConstants.h:30-45 — the definition's
// constraints of PanMin, PanMax, TiltMin, TiltMax and ZoomMax too.
const (
	PanMinMinValue  int16 = -180
	PanMinMaxValue  int16 = 0
	PanMaxMinValue  int16 = 1
	PanMaxMaxValue  int16 = 180
	TiltMinMinValue int16 = -180
	TiltMinMaxValue int16 = 0
	TiltMaxMinValue int16 = 1
	TiltMaxMaxValue int16 = 180
	ZoomMinValue    uint8 = 1
	ZoomMaxMinValue uint8 = 2
	ZoomMaxMaxValue uint8 = 100

	DefaultPan  int16 = 0
	DefaultTilt int16 = 0
	DefaultZoom uint8 = 1

	// DefaultMaxPresets is chip's mMaxPresets default
	// (CameraAvSettingsUserLevelManagementLogic.h, `mMaxPresets = 5`).
	DefaultMaxPresets uint8 = 5
)

// The movement states.
const (
	MovementIdle   = avdef.PhysicalMovementIdle
	MovementMoving = avdef.PhysicalMovementMoving
)

// Configuration errors.
var (
	// ErrAvSettingsConfig reports a configuration the cluster's model or
	// chip's start-up refuses.
	ErrAvSettingsConfig = errors.New("camera: CameraAvSettingsUserLevelManagement configuration outside the cluster's model")
	// ErrNoMechanicalPTZ: MPAN, MTILT or MZOOM without a MechanicalPTZ, or
	// MPRESETS with one that is no MechanicalPresets.
	ErrNoMechanicalPTZ = errors.New("camera: the mechanical features need a MechanicalPTZ (MPRESETS a MechanicalPresets)")
	// ErrNoDigitalPTZ: DPTZ without a DigitalPTZ.
	ErrNoDigitalPTZ = errors.New("camera: DPTZ needs a DigitalPTZ")
)

// AvSettingsConfig carries the construction parameters of an [AvSettings].
// The limits, the position, the presets and the streams are the values the
// host persisted; a nil limit starts at chip's default (the widest range,
// CameraAvSettingsUserLevelManagementLogic.h).
type AvSettingsConfig struct {
	Features AvFeature
	// MaxPresets is fixed (MPRESETS); 0 is [DefaultMaxPresets].
	MaxPresets uint8
	PanMin     *int16
	PanMax     *int16
	TiltMin    *int16
	TiltMax    *int16
	ZoomMax    *uint8
	// MptzPosition is the persisted position; nil starts at pan 0, tilt
	// 0, zoom 1, each with its feature (chip LoadPersistentAttributes,
	// CameraAvSettingsUserLevelManagementLogic.cpp:680-687).
	MptzPosition *MPTZ
	// MptzPresets are the persisted presets (MPRESETS).
	MptzPresets []MPTZPreset
	// DptzStreams are the persisted streams (DPTZ).
	DptzStreams []DPTZStream
	// Mechanical moves the camera; MPAN, MTILT and MZOOM require it, and
	// MPRESETS requires it to be a [MechanicalPresets].
	Mechanical MechanicalPTZ
	// Digital changes the streams' viewports; DPTZ requires it.
	Digital DigitalPTZ
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// AvSettings implements [contract.ClusterServer] for
// CameraAvSettingsUserLevelManagement: chip's server logic over the
// generated server — the feature-gated MPTZ commands, which validate a
// target against the limits and hand it to the [MechanicalPTZ], the
// MovementState of a movement the host reports complete
// ([AvSettings.PhysicalMovementComplete]), the presets, and the DPTZ
// streams the host announces as it allocates them
// ([AvSettings.AddMoveCapableVideoStream]). The rules are connectedhomeip's
// CameraAvSettingsUserLevelManagementLogic.cpp at 6170af84 (matter.js's
// server is the generated behaviour).
type AvSettings struct {
	*spec.Instance
	spec.SelfHeld

	srv     *spec.Server
	mech    MechanicalPTZ
	presets MechanicalPresets
	digital DigitalPTZ

	// mu guards the state below.
	mu                     sync.Mutex
	maxPresets             uint8
	panMin, panMax         int16
	tiltMin, tiltMax       int16
	zoomMax                uint8
	position               MPTZ
	movement               PhysicalMovement
	targetPan, targetTilt  *int16
	targetZoom             *uint8
	mptzPresets            []MPTZPreset
	dptzStreams            []DPTZStream
	currentPresetID        uint8
	mechanical, hasPresets bool
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*AvSettings)(nil)
	_ contract.ClusterDataVersion             = (*AvSettings)(nil)
	_ contract.ClusterAttributeLister         = (*AvSettings)(nil)
	_ contract.ClusterCommandLister           = (*AvSettings)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*AvSettings)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*AvSettings)(nil)
	_ contract.AttributeChangeNotifier        = (*AvSettings)(nil)
	_ spec.AttributeSetter                    = (*AvSettings)(nil)
	_ spec.CommandRegistrar                   = (*AvSettings)(nil)
)

const mechanicalFeatures = FeatureMechanicalPan | FeatureMechanicalTilt | FeatureMechanicalZoom

// NewAvSettings builds a CameraAvSettingsUserLevelManagement server. At
// least one of MPAN, MTILT, MZOOM and DPTZ is required (chip Startup,
// CameraAvSettingsUserLevelManagementLogic.cpp:57-64); the accepted
// commands follow the features (:83-106), DPTZRelativeMove with DPTZ.
func NewAvSettings(cfg AvSettingsConfig) (*AvSettings, error) {
	if cfg.Features&(mechanicalFeatures|FeatureDigitalPTZ) == 0 {
		return nil, fmt.Errorf("%w: none of MPAN, MTILT, MZOOM and DPTZ", ErrAvSettingsConfig)
	}
	a := &AvSettings{
		mech: cfg.Mechanical, digital: cfg.Digital,
		maxPresets: cfg.MaxPresets,
		panMin:     PanMinMinValue, panMax: PanMaxMaxValue,
		tiltMin: TiltMinMinValue, tiltMax: TiltMaxMaxValue,
		zoomMax:         ZoomMaxMaxValue,
		currentPresetID: 1,
		mechanical:      cfg.Features&mechanicalFeatures != 0,
		hasPresets:      cfg.Features&FeatureMechanicalPresets != 0,
	}
	if a.mechanical && a.mech == nil {
		return nil, ErrNoMechanicalPTZ
	}
	if a.hasPresets {
		p, ok := a.mech.(MechanicalPresets)
		if !ok {
			return nil, ErrNoMechanicalPTZ
		}
		a.presets = p
	}
	if cfg.Features&FeatureDigitalPTZ != 0 && a.digital == nil {
		return nil, ErrNoDigitalPTZ
	}
	if a.maxPresets == 0 {
		a.maxPresets = DefaultMaxPresets
	}
	setIf(&a.panMin, cfg.PanMin)
	setIf(&a.panMax, cfg.PanMax)
	setIf(&a.tiltMin, cfg.TiltMin)
	setIf(&a.tiltMax, cfg.TiltMax)
	setIf(&a.zoomMax, cfg.ZoomMax)

	opts := spec.Options{Features: uint32(cfg.Features)}
	if cfg.Features&FeatureDigitalPTZ != 0 {
		opts.Commands = append(opts.Commands, avdef.CmdDptzRelativeMove) // :99-103
	}
	handlers := map[uint32]spec.CommandHandler{
		avdef.CmdMptzSetPosition:  a.mptzSetPosition,
		avdef.CmdMptzRelativeMove: a.mptzRelativeMove,
		avdef.CmdMptzMoveToPreset: a.mptzMoveToPreset,
		avdef.CmdMptzSavePreset:   a.mptzSavePreset,
		avdef.CmdMptzRemovePreset: a.mptzRemovePreset,
		avdef.CmdDptzSetViewport:  a.dptzSetViewport,
		avdef.CmdDptzRelativeMove: a.dptzRelativeMove,
	}
	srv, err := spec.NewServer(avdef.Definition, opts, spec.ServerConfig{Handlers: handlers, DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAvSettingsConfig, err)
	}
	a.srv, a.Instance, a.SelfHeld = srv, srv.Instance, spec.NewSelfHeld(srv)

	// The persisted position, or the defaults (:668-687).
	if cfg.MptzPosition != nil {
		a.position = *cfg.MptzPosition
	} else {
		a.setPosition(ptr(DefaultPan), ptr(DefaultTilt), ptr(DefaultZoom))
	}
	a.mptzPresets = slices.Clone(cfg.MptzPresets)
	a.dptzStreams = slices.Clone(cfg.DptzStreams)
	a.skipTakenPresetID()
	a.movement = MovementIdle // :68
	if err := srv.SetAttributes(a.served(a.snapshot())); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAvSettingsConfig, err)
	}
	return a, nil
}

func setIf[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

func ptr[T any](v T) *T { return &v }

// snapshot is every attribute's value; the caller holds a.mu or owns a.
func (a *AvSettings) snapshot() map[uint32]any {
	return map[uint32]any{
		avdef.AttrMptzPosition:  a.position,
		avdef.AttrMaxPresets:    a.maxPresets,
		avdef.AttrMptzPresets:   spec.List[MPTZPreset](nonNil(a.mptzPresets)),
		avdef.AttrDptzStreams:   spec.List[DPTZStream](nonNil(a.dptzStreams)),
		avdef.AttrZoomMax:       a.zoomMax,
		avdef.AttrTiltMin:       a.tiltMin,
		avdef.AttrTiltMax:       a.tiltMax,
		avdef.AttrPanMin:        a.panMin,
		avdef.AttrPanMax:        a.panMax,
		avdef.AttrMovementState: a.movement,
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return slices.Clone(s)
}

// served keeps the values of the attributes the instance serves.
func (a *AvSettings) served(values map[uint32]any) map[uint32]any {
	out := make(map[uint32]any, len(values))
	for id, v := range values {
		if a.Serves(id) {
			out[id] = v
		}
	}
	return out
}

// publish stores the served attributes among ids at their current value;
// the generated server reports the ones that changed.
func (a *AvSettings) publish(origin contract.Origin, ids ...uint32) {
	a.mu.Lock()
	all := a.snapshot()
	a.mu.Unlock()
	values := make(map[uint32]any, len(ids))
	for _, id := range ids {
		values[id] = all[id]
	}
	_ = a.srv.SetAttributesFrom(origin, a.served(values))
}

// setPosition sets the pan, tilt and zoom each value and its feature
// carry; nil leaves one as it is. Mirrors chip SetPan, SetTilt, SetZoom
// (CameraAvSettingsUserLevelManagementLogic.cpp:409-454).
func (a *AvSettings) setPosition(pan, tilt *int16, zoom *uint8) bool {
	changed := false
	if pan != nil && a.HasFeature("MPAN") {
		a.position.Pan, changed = ptr(*pan), true
	}
	if tilt != nil && a.HasFeature("MTILT") {
		a.position.Tilt, changed = ptr(*tilt), true
	}
	if zoom != nil && a.HasFeature("MZOOM") {
		a.position.Zoom, changed = ptr(*zoom), true
	}
	return changed
}

// SetPosition records a position the device reached by itself (a local
// change, chip's SetPan / SetTilt / SetZoom): each value with its feature
// is set, nil leaves one as it is.
func (a *AvSettings) SetPosition(pan, tilt *int16, zoom *uint8) {
	a.mu.Lock()
	changed := a.setPosition(pan, tilt, zoom)
	a.mu.Unlock()
	if changed {
		a.publish(contract.OriginHostSet, avdef.AttrMptzPosition)
	}
}

// Position returns MPTZPosition.
func (a *AvSettings) Position() MPTZ {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.position
}

// MovementState returns MovementState.
func (a *AvSettings) MovementState() PhysicalMovement {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.movement
}

// Presets returns MPTZPresets.
func (a *AvSettings) Presets() []MPTZPreset {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.mptzPresets)
}

// Streams returns DPTZStreams.
func (a *AvSettings) Streams() []DPTZStream {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.dptzStreams)
}

// NextPresetID is the preset id an MPTZSavePreset without one uses
// (chip's mCurrentPresetID).
func (a *AvSettings) NextPresetID() uint8 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.currentPresetID
}

// The limit setters, chip's SetTiltMin … SetZoomMax
// (CameraAvSettingsUserLevelManagementLogic.cpp:334-407): the attribute
// needs its feature ([spec.ErrNotServed]) and a value within its
// constraint ([spec.ErrInvalidValue]); an unchanged value is no change.

// SetPanMin sets PanMin (MPAN, -180 to 0).
func (a *AvSettings) SetPanMin(v int16) error { return a.setLimit(avdef.AttrPanMin, v) }

// SetPanMax sets PanMax (MPAN, 1 to 180).
func (a *AvSettings) SetPanMax(v int16) error { return a.setLimit(avdef.AttrPanMax, v) }

// SetTiltMin sets TiltMin (MTILT, -180 to 0).
func (a *AvSettings) SetTiltMin(v int16) error { return a.setLimit(avdef.AttrTiltMin, v) }

// SetTiltMax sets TiltMax (MTILT, 1 to 180).
func (a *AvSettings) SetTiltMax(v int16) error { return a.setLimit(avdef.AttrTiltMax, v) }

// SetZoomMax sets ZoomMax (MZOOM, 2 to 100).
func (a *AvSettings) SetZoomMax(v uint8) error { return a.setLimit(avdef.AttrZoomMax, v) }

func (a *AvSettings) setLimit(attrID uint32, v any) error {
	checked, err := a.CheckSet(attrID, v, nil)
	if err != nil {
		return err
	}
	a.mu.Lock()
	switch attrID {
	case avdef.AttrPanMin:
		a.panMin, _ = checked.(int16)
	case avdef.AttrPanMax:
		a.panMax, _ = checked.(int16)
	case avdef.AttrTiltMin:
		a.tiltMin, _ = checked.(int16)
	case avdef.AttrTiltMax:
		a.tiltMax, _ = checked.(int16)
	case avdef.AttrZoomMax:
		a.zoomMax, _ = checked.(uint8)
	}
	a.mu.Unlock()
	a.publish(contract.OriginHostSet, attrID)
	return nil
}

// The DPTZ streams the host announces, chip's AddMoveCapableVideoStream …
// RemoveMoveCapableVideoStream (CameraAvSettingsUserLevelManagementLogic.cpp:
// 466-591): the streams that can move digitally, each with its viewport.

// AddMoveCapableVideoStream adds an allocated stream with its default
// viewport (:469-489).
func (a *AvSettings) AddMoveCapableVideoStream(videoStreamID uint16, viewport Viewport) {
	a.mu.Lock()
	a.dptzStreams = append(a.dptzStreams, DPTZStream{VideoStreamId: videoStreamID, Viewport: viewport})
	a.mu.Unlock()
	a.publish(contract.OriginHostSet, avdef.AttrDptzStreams)
}

// UpdateMoveCapableVideoStream sets a known stream's viewport; an unknown
// stream changes nothing (:491-523).
func (a *AvSettings) UpdateMoveCapableVideoStream(videoStreamID uint16, viewport Viewport) {
	a.updateStream(contract.OriginHostSet, videoStreamID, viewport)
}

func (a *AvSettings) updateStream(origin contract.Origin, videoStreamID uint16, viewport Viewport) {
	a.mu.Lock()
	i := slices.IndexFunc(a.dptzStreams, func(s DPTZStream) bool { return s.VideoStreamId == videoStreamID })
	if i >= 0 {
		a.dptzStreams[i].Viewport = viewport
	}
	a.mu.Unlock()
	if i >= 0 {
		a.publish(origin, avdef.AttrDptzStreams)
	}
}

// UpdateMoveCapableVideoStreams sets every stream's viewport, after the
// device's default viewport changed; without a stream nothing changes
// (:525-556).
func (a *AvSettings) UpdateMoveCapableVideoStreams(viewport Viewport) {
	a.mu.Lock()
	n := len(a.dptzStreams)
	for i := range a.dptzStreams {
		a.dptzStreams[i].Viewport = viewport
	}
	a.mu.Unlock()
	if n > 0 {
		a.publish(contract.OriginHostSet, avdef.AttrDptzStreams)
	}
}

// RemoveMoveCapableVideoStream removes a deallocated stream; an unknown one
// changes nothing (:558-580).
func (a *AvSettings) RemoveMoveCapableVideoStream(videoStreamID uint16) {
	a.mu.Lock()
	n := len(a.dptzStreams)
	a.dptzStreams = slices.DeleteFunc(a.dptzStreams, func(s DPTZStream) bool { return s.VideoStreamId == videoStreamID })
	removed := len(a.dptzStreams) != n
	a.mu.Unlock()
	if removed {
		a.publish(contract.OriginHostSet, avdef.AttrDptzStreams)
	}
}

// knownVideoStreamID reports a stream of DPTZStreams (:585-591).
func (a *AvSettings) knownVideoStreamID(id uint16) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.ContainsFunc(a.dptzStreams, func(s DPTZStream) bool { return s.VideoStreamId == id })
}

// updatePresetID moves the id an MPTZSavePreset without one uses to the
// next free one after it, wrapping to 1; with every slot taken it stays.
// The caller holds a.mu or owns a. Mirrors chip UpdatePresetID
// (CameraAvSettingsUserLevelManagementLogic.cpp:593-629).
func (a *AvSettings) updatePresetID() {
	next := a.currentPresetID
	for {
		next = uint8(int(next)%int(a.maxPresets) + 1)
		if next == a.currentPresetID {
			return
		}
		if !slices.ContainsFunc(a.mptzPresets, func(p MPTZPreset) bool { return p.PresetId == next }) {
			a.currentPresetID = next
			return
		}
	}
}

// skipTakenPresetID moves the next preset id on when a preset the host
// restored holds it. chip starts at 1 whatever its storage holds
// (CameraAvSettingsUserLevelManagementLogic.h, `mCurrentPresetID = 1`),
// so an MPTZSavePreset without an id after a restart with preset 1 stored
// appends a second preset 1 (:1062, :1105-1110); here it takes the next
// free id. The caller holds a.mu or owns a.
func (a *AvSettings) skipTakenPresetID() {
	if slices.ContainsFunc(a.mptzPresets, func(p MPTZPreset) bool { return p.PresetId == a.currentPresetID }) {
		a.updatePresetID()
	}
}

// PhysicalMovementComplete reports the end of the movement a command
// started: on success (nil) the target becomes MPTZPosition; either way
// MovementState returns to Idle. Mirrors chip OnPhysicalMovementComplete
// (CameraAvSettingsUserLevelManagementLogic.cpp:1263-1284).
func (a *AvSettings) PhysicalMovementComplete(err error) {
	a.mu.Lock()
	moved := false
	if err == nil {
		moved = a.setPosition(a.targetPan, a.targetTilt, a.targetZoom)
	}
	a.movement = MovementIdle
	a.mu.Unlock()
	ids := []uint32{avdef.AttrMovementState}
	if moved {
		ids = append(ids, avdef.AttrMptzPosition)
	}
	a.publish(contract.OriginHostSet, ids...)
}

// startMovement records the target of a movement the port started and
// sets MovementState Moving (:803-813).
func (a *AvSettings) startMovement(pan, tilt *int16, zoom *uint8) {
	a.mu.Lock()
	a.targetPan, a.targetTilt, a.targetZoom = pan, tilt, zoom
	a.movement = MovementMoving
	a.mu.Unlock()
	a.publish(contract.OriginCommand, avdef.AttrMovementState)
}

// avStatus answers a command with an IM status.
func avStatus(status im.StatusCode, format string, args ...any) error {
	return spec.Errorf(status, "CameraAvSettingsUserLevelManagement: "+format, args...)
}

// mptzSetPosition carries out MPTZSetPosition: a value needs its feature
// (INVALID_COMMAND) and must lie within its limits (CONSTRAINT_ERROR); at
// least one is required (INVALID_COMMAND); a camera that moves is BUSY;
// then the port starts the movement. Mirrors chip HandleMPTZSetPosition
// (CameraAvSettingsUserLevelManagementLogic.cpp:721-814); chip's
// CanChangeMPTZ (:793-795, BUSY) is the port's own refusal here.
func (a *AvSettings) mptzSetPosition(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[avdef.MptzSetPositionRequest](fields)
	if !ok {
		return nil, avStatus(im.StatusInvalidCommand, "MPTZSetPosition payload %T", fields)
	}
	a.mu.Lock()
	panMin, panMax, tiltMin, tiltMax, zoomMax, moving := a.panMin, a.panMax, a.tiltMin, a.tiltMax, a.zoomMax, a.movement == MovementMoving
	a.mu.Unlock()
	if req.Pan != nil { // :733-746
		if !a.HasFeature("MPAN") {
			return nil, avStatus(im.StatusInvalidCommand, "Pan without MechanicalPan")
		}
		if *req.Pan > panMax || *req.Pan < panMin {
			return nil, avStatus(im.StatusConstraintError, "Pan %d outside %d to %d", *req.Pan, panMin, panMax)
		}
	}
	if req.Tilt != nil { // :748-761
		if !a.HasFeature("MTILT") {
			return nil, avStatus(im.StatusInvalidCommand, "Tilt without MechanicalTilt")
		}
		if *req.Tilt > tiltMax || *req.Tilt < tiltMin {
			return nil, avStatus(im.StatusConstraintError, "Tilt %d outside %d to %d", *req.Tilt, tiltMin, tiltMax)
		}
	}
	if req.Zoom != nil { // :763-777
		if !a.HasFeature("MZOOM") {
			return nil, avStatus(im.StatusInvalidCommand, "Zoom without MechanicalZoom")
		}
		if *req.Zoom > zoomMax || *req.Zoom < ZoomMinValue {
			return nil, avStatus(im.StatusConstraintError, "Zoom %d outside %d to %d", *req.Zoom, ZoomMinValue, zoomMax)
		}
	}
	if req.Pan == nil && req.Tilt == nil && req.Zoom == nil { // :780
		return nil, avStatus(im.StatusInvalidCommand, "MPTZSetPosition names no value")
	}
	if moving { // :788
		return nil, avStatus(im.StatusBusy, "the camera is moving")
	}
	if err := a.mech.MptzSetPosition(ctx, MPTZ{Pan: req.Pan, Tilt: req.Tilt, Zoom: req.Zoom}); err != nil { // :797-802
		return nil, portError("MPTZSetPosition", err)
	}
	a.startMovement(req.Pan, req.Tilt, req.Zoom)
	return nil, nil
}

// mptzRelativeMove carries out MPTZRelativeMove: a delta needs its
// feature (INVALID_COMMAND) and may not exceed its axis' range
// (CONSTRAINT_ERROR); the target is the current position moved by the
// delta, held to the limits; at least one delta is required
// (INVALID_COMMAND); a camera that moves is BUSY. Mirrors chip
// HandleMPTZRelativeMove (CameraAvSettingsUserLevelManagementLogic.cpp:
// 816-965).
func (a *AvSettings) mptzRelativeMove(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[avdef.MptzRelativeMoveRequest](fields)
	if !ok {
		return nil, avStatus(im.StatusInvalidCommand, "MPTZRelativeMove payload %T", fields)
	}
	a.mu.Lock()
	panMin, panMax, tiltMin, tiltMax, zoomMax, moving := int(a.panMin), int(a.panMax), int(a.tiltMin), int(a.tiltMax), int(a.zoomMax), a.movement == MovementMoving
	pos := a.position
	a.mu.Unlock()
	var newPan, newTilt *int16
	var newZoom *uint8
	if req.PanDelta != nil { // :833-862
		if !a.HasFeature("MPAN") {
			return nil, avStatus(im.StatusInvalidCommand, "PanDelta without MechanicalPan")
		}
		d, span := int(*req.PanDelta), panMax-panMin
		if d > span || d < -span {
			return nil, avStatus(im.StatusConstraintError, "PanDelta %d outside ±%d", d, span)
		}
		newPan = ptr(int16(min(max(int(deref(pos.Pan))+d, panMin), panMax)))
	}
	if req.TiltDelta != nil { // :864-894
		if !a.HasFeature("MTILT") {
			return nil, avStatus(im.StatusInvalidCommand, "TiltDelta without MechanicalTilt")
		}
		d, span := int(*req.TiltDelta), tiltMax-tiltMin
		if d > span || d < -span {
			return nil, avStatus(im.StatusConstraintError, "TiltDelta %d outside ±%d", d, span)
		}
		newTilt = ptr(int16(min(max(int(deref(pos.Tilt))+d, tiltMin), tiltMax)))
	}
	if req.ZoomDelta != nil { // :896-928
		if !a.HasFeature("MZOOM") {
			return nil, avStatus(im.StatusInvalidCommand, "ZoomDelta without MechanicalZoom")
		}
		d, span := int(*req.ZoomDelta), zoomMax-1
		if d > span || d < -span {
			return nil, avStatus(im.StatusConstraintError, "ZoomDelta %d outside ±%d", d, span)
		}
		newZoom = ptr(uint8(min(max(int(deref(pos.Zoom))+d, 1), zoomMax)))
	}
	if req.PanDelta == nil && req.TiltDelta == nil && req.ZoomDelta == nil { // :931
		return nil, avStatus(im.StatusInvalidCommand, "MPTZRelativeMove names no delta")
	}
	if moving { // :939
		return nil, avStatus(im.StatusBusy, "the camera is moving")
	}
	if err := a.mech.MptzRelativeMove(ctx, MPTZ{Pan: newPan, Tilt: newTilt, Zoom: newZoom}); err != nil { // :949-954
		return nil, portError("MPTZRelativeMove", err)
	}
	a.startMovement(newPan, newTilt, newZoom)
	return nil, nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// checkPresetID holds a preset id to 1 to MaxPresets (CONSTRAINT_ERROR).
func (a *AvSettings) checkPresetID(id uint8) error {
	if id < 1 || id > a.maxPresets {
		return avStatus(im.StatusConstraintError, "preset %d outside 1 to %d", id, a.maxPresets)
	}
	return nil
}

// mptzMoveToPreset carries out MPTZMoveToPreset: the id must lie in 1 to
// MaxPresets (CONSTRAINT_ERROR) and name a stored preset (NOT_FOUND); a
// camera that moves is BUSY; the port moves to the preset's settings.
// Mirrors chip HandleMPTZMoveToPreset
// (CameraAvSettingsUserLevelManagementLogic.cpp:967-1028).
func (a *AvSettings) mptzMoveToPreset(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[avdef.MptzMoveToPresetRequest](fields)
	if !ok {
		return nil, avStatus(im.StatusInvalidCommand, "MPTZMoveToPreset payload %T", fields)
	}
	if err := a.checkPresetID(req.PresetId); err != nil { // :975
		return nil, err
	}
	a.mu.Lock()
	i := slices.IndexFunc(a.mptzPresets, func(p MPTZPreset) bool { return p.PresetId == req.PresetId })
	var settings MPTZ
	if i >= 0 {
		settings = a.mptzPresets[i].Settings
	}
	moving := a.movement == MovementMoving
	a.mu.Unlock()
	if i < 0 { // :981-996
		return nil, avStatus(im.StatusNotFound, "no preset %d", req.PresetId)
	}
	if moving { // :1000
		return nil, avStatus(im.StatusBusy, "the camera is moving")
	}
	if err := a.presets.MptzMoveToPreset(ctx, req.PresetId, settings); err != nil { // :1012-1017
		return nil, portError("MPTZMoveToPreset", err)
	}
	a.startMovement(settings.Pan, settings.Tilt, settings.Zoom)
	return nil, nil
}

// mptzSavePreset carries out MPTZSavePreset: the current position is
// stored under the given id (1 to MaxPresets, CONSTRAINT_ERROR), or under
// the next free one; a new preset needs a free slot (RESOURCE_EXHAUSTED);
// the port may refuse it. Saving under the next free id moves it on.
// Mirrors chip HandleMPTZSavePreset
// (CameraAvSettingsUserLevelManagementLogic.cpp:1030-1141).
func (a *AvSettings) mptzSavePreset(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[avdef.MptzSavePresetRequest](fields)
	if !ok {
		return nil, avStatus(im.StatusInvalidCommand, "MPTZSavePreset payload %T", fields)
	}
	a.mu.Lock()
	id := a.currentPresetID
	a.mu.Unlock()
	if req.PresetId != nil { // :1042-1049
		if err := a.checkPresetID(*req.PresetId); err != nil {
			return nil, err
		}
		id = *req.PresetId
	}
	a.mu.Lock()
	i := slices.IndexFunc(a.mptzPresets, func(p MPTZPreset) bool { return p.PresetId == id })
	updating := i >= 0 && req.PresetId != nil // :1062
	full := len(a.mptzPresets) == int(a.maxPresets)
	a.mu.Unlock()
	if !updating && full { // :1064-1073
		return nil, avStatus(im.StatusResourceExhausted, "every one of %d presets is taken", a.maxPresets)
	}
	if err := a.presets.MptzSavePreset(ctx, id); err != nil { // :1078-1083
		return nil, portError("MPTZSavePreset", err)
	}
	a.mu.Lock()
	preset := MPTZPreset{PresetId: id, Name: req.Name, Settings: a.position} // :1087-1091
	if j := slices.IndexFunc(a.mptzPresets, func(p MPTZPreset) bool { return p.PresetId == id }); updating && j >= 0 {
		a.mptzPresets[j] = preset // :1098-1104
	} else {
		a.mptzPresets = append(a.mptzPresets, preset) // :1105-1110
	}
	if id == a.currentPresetID { // :1133-1136
		a.updatePresetID()
	}
	a.mu.Unlock()
	a.publish(contract.OriginCommand, avdef.AttrMptzPresets)
	return nil, nil
}

// mptzRemovePreset carries out MPTZRemovePreset: the id must lie in 1 to
// MaxPresets (CONSTRAINT_ERROR) and name a stored preset (NOT_FOUND); the
// port may refuse; the preset is erased. Mirrors chip
// HandleMPTZRemovePreset (CameraAvSettingsUserLevelManagementLogic.cpp:
// 1143-1195).
func (a *AvSettings) mptzRemovePreset(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[avdef.MptzRemovePresetRequest](fields)
	if !ok {
		return nil, avStatus(im.StatusInvalidCommand, "MPTZRemovePreset payload %T", fields)
	}
	if err := a.checkPresetID(req.PresetId); err != nil { // :1151
		return nil, err
	}
	a.mu.Lock()
	found := slices.ContainsFunc(a.mptzPresets, func(p MPTZPreset) bool { return p.PresetId == req.PresetId })
	a.mu.Unlock()
	if !found { // :1162-1166
		return nil, avStatus(im.StatusNotFound, "no preset %d", req.PresetId)
	}
	if err := a.presets.MptzRemovePreset(ctx, req.PresetId); err != nil { // :1170-1175
		return nil, portError("MPTZRemovePreset", err)
	}
	a.mu.Lock()
	a.mptzPresets = slices.DeleteFunc(a.mptzPresets, func(p MPTZPreset) bool { return p.PresetId == req.PresetId }) // :1191
	a.mu.Unlock()
	a.publish(contract.OriginCommand, avdef.AttrMptzPresets)
	return nil, nil
}

// dptzSetViewport carries out DPTZSetViewport: the stream must be one of
// DPTZStreams (NOT_FOUND); the port applies the viewport, which the
// stream then reports. Mirrors chip HandleDPTZSetViewport
// (CameraAvSettingsUserLevelManagementLogic.cpp:1197-1222).
func (a *AvSettings) dptzSetViewport(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[avdef.DptzSetViewportRequest](fields)
	if !ok {
		return nil, avStatus(im.StatusInvalidCommand, "DPTZSetViewport payload %T", fields)
	}
	if !a.knownVideoStreamID(req.VideoStreamId) { // :1207
		return nil, avStatus(im.StatusNotFound, "no DPTZ stream %d", req.VideoStreamId)
	}
	if err := a.digital.DptzSetViewport(ctx, req.VideoStreamId, req.Viewport); err != nil { // :1212
		return nil, portError("DPTZSetViewport", err)
	}
	a.updateStream(contract.OriginCommand, req.VideoStreamId, req.Viewport) // :1214-1219
	return nil, nil
}

// dptzRelativeMove carries out DPTZRelativeMove: a ZoomDelta lies in
// -100 to 100 (CONSTRAINT_ERROR), checked first; the stream must be one of
// DPTZStreams (NOT_FOUND); the port moves the viewport and returns the one
// it arrived at, which the stream then reports. Mirrors chip
// HandleDPTZRelativeMove (CameraAvSettingsUserLevelManagementLogic.cpp:
// 1224-1261).
func (a *AvSettings) dptzRelativeMove(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[avdef.DptzRelativeMoveRequest](fields)
	if !ok {
		return nil, avStatus(im.StatusInvalidCommand, "DPTZRelativeMove payload %T", fields)
	}
	if req.ZoomDelta != nil && (*req.ZoomDelta < -100 || *req.ZoomDelta > 100) { // :1235-1243
		return nil, avStatus(im.StatusConstraintError, "ZoomDelta %d outside -100 to 100", *req.ZoomDelta)
	}
	if !a.knownVideoStreamID(req.VideoStreamId) { // :1247
		return nil, avStatus(im.StatusNotFound, "no DPTZ stream %d", req.VideoStreamId)
	}
	viewport, err := a.digital.DptzRelativeMove(ctx, req) // :1253
	if err != nil {
		return nil, portError("DPTZRelativeMove", err)
	}
	a.updateStream(contract.OriginCommand, req.VideoStreamId, viewport) // :1255-1258
	return nil, nil
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change.
// The limits follow their setters, MPTZPosition [AvSettings.SetPosition],
// MPTZPresets and DPTZStreams replace the lists; MaxPresets is fixed and
// MovementState follows the movement ([spec.ErrNotHeld]). All or nothing.
func (a *AvSettings) SetAttributes(values map[uint32]any) error {
	ids := slices.Sorted(maps.Keys(values))
	checked := make(map[uint32]any, len(ids))
	for _, id := range ids {
		if id == avdef.AttrMaxPresets || id == avdef.AttrMovementState {
			return fmt.Errorf("%w: camera: CameraAvSettingsUserLevelManagement attribute 0x%04X", spec.ErrNotHeld, id)
		}
		v, err := a.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		checked[id] = v
	}
	a.mu.Lock()
	for id, v := range checked {
		switch id {
		case avdef.AttrPanMin:
			a.panMin, _ = v.(int16)
		case avdef.AttrPanMax:
			a.panMax, _ = v.(int16)
		case avdef.AttrTiltMin:
			a.tiltMin, _ = v.(int16)
		case avdef.AttrTiltMax:
			a.tiltMax, _ = v.(int16)
		case avdef.AttrZoomMax:
			a.zoomMax, _ = v.(uint8)
		case avdef.AttrMptzPosition:
			if p, ok := v.(MPTZ); ok {
				a.setPosition(p.Pan, p.Tilt, p.Zoom)
			}
		case avdef.AttrMptzPresets:
			a.mptzPresets = listOf[MPTZPreset](v)
			a.skipTakenPresetID()
		case avdef.AttrDptzStreams:
			a.dptzStreams = listOf[DPTZStream](v)
		}
	}
	a.mu.Unlock()
	a.publish(contract.OriginHostSet, ids...)
	return nil
}

// listOf is a stored list value as a slice.
func listOf[T spec.Encodable](v any) []T {
	switch l := v.(type) {
	case spec.List[T]:
		return slices.Clone([]T(l))
	case []T:
		return slices.Clone(l)
	}
	return nil
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (a *AvSettings) MatterDataVersion() uint32 { return a.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (a *AvSettings) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return a.srv.OnAttributeChange(cb)
}

// MatterRead resolves an attribute.
func (a *AvSettings) MatterRead(attrID uint32) (any, bool) { return a.srv.MatterRead(attrID) }

// MatterWrite answers every write by the definition: no attribute is
// writable.
func (a *AvSettings) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return a.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches an accepted command; a host's handler runs in
// its place ([spec.SelfHeld.Handle]).
func (a *AvSettings) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &a.SelfHeld, cmdID, fields)
}
