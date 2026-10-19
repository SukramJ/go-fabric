// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"context"
	"fmt"

	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	switchserver "github.com/SukramJ/go-fabric/cluster/switch"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// The Switch cluster (0x003B) of the bridge's first releases. The server
// itself is now cluster/switch ([switchserver.Server]), the port of
// matter.js SwitchServer.ts; [GenericSwitch] stays as a thin wrapper over
// it for hosts that construct it directly, and the measurement materialiser
// builds the new server.
//
// The constants below name the cluster's identity from its generated
// definition (cluster/spec/switchcluster, ADR 0013, from matter.js
// switch.element.ts).
const (
	matterClusterGenericSwitch = swdef.ClusterID

	matterAttrSwitchNumberOfPositions = swdef.AttrNumberOfPositions
	matterAttrSwitchCurrentPosition   = swdef.AttrCurrentPosition
	matterAttrSwitchMultiPressMax     = swdef.AttrMultiPressMax

	// MatterEventSwitchLatched fires when a latching switch flips
	// position (LS feature).
	MatterEventSwitchLatched = swdef.EventSwitchLatched
	// MatterEventInitialPress fires on the first press (MS / AS).
	MatterEventInitialPress = swdef.EventInitialPress
	// MatterEventLongPress fires once the long-press threshold is
	// crossed (MSL feature).
	MatterEventLongPress = swdef.EventLongPress
	// MatterEventShortRelease fires when a short press ends (MSR).
	MatterEventShortRelease = swdef.EventShortRelease
	// MatterEventLongRelease fires when a long press ends (MSL).
	MatterEventLongRelease = swdef.EventLongRelease
	// MatterEventMultiPressOngoing fires during multi-press capture
	// (MSM feature).
	MatterEventMultiPressOngoing = swdef.EventMultiPressOngoing
	// MatterEventMultiPressComplete fires once the multi-press window
	// closes (MSM feature).
	MatterEventMultiPressComplete = swdef.EventMultiPressComplete

	// switchClusterRevision is the definition's (Switch bumped 1→2 in
	// Matter 1.4 with the action-button (AS) feature codification).
	switchClusterRevision = swdef.Revision
)

// GenericSwitchSource is the model-side surface a HM button source
// (the per-channel press group, or a lone Button / Action DP) exposes.
// The cluster server reads `NumberOfPositions` once at construction
// and supports long-press recognition opt-in via `SupportsLongPress`.
type GenericSwitchSource interface {
	// MatterSwitchPositions returns the static NumberOfPositions
	// (Matter §1.13.5.1). Typical: 2 for a single button (idle +
	// pressed). 0 → cluster falls back to 2.
	MatterSwitchPositions() uint8
	// MatterSwitchSupportsLongPress reports whether the source can
	// distinguish long press from short press. When true, the
	// FeatureMap exposes MSL and the cluster forwards LongPress /
	// LongRelease events when the source fires them.
	MatterSwitchSupportsLongPress() bool
}

// GenericSwitchPositionSource is the optional source capability for a
// live CurrentPosition (Matter §1.13.5.2). Sources that run a press-
// cycle state machine report 1 (pressed) while a hold is open and 0
// (idle) otherwise, mirroring matter.js
// packages/node/src/behaviors/switch/SwitchServer.ts, where
// `state.currentPosition` moves to the pressed position for the
// duration of the press and returns to `momentaryNeutralPosition`
// (default 0) on release. Sources without the capability read as the
// constant neutral position.
type GenericSwitchPositionSource interface {
	MatterSwitchCurrentPosition() uint8
}

// GenericSwitchMultiPressSource is the optional source capability for
// multi-press recognition: MatterSwitchMultiPressMax returns the
// MultiPressMax attribute (Matter §1.13.5.3, "min 2"). A value of 2 or more
// makes the materialised server advertise MSM and count presses up to it
// (matter.js SwitchServer.ts:328-340); 0 (or 1) is no MSM.
type GenericSwitchMultiPressSource interface {
	MatterSwitchMultiPressMax() uint8
}

// GenericSwitchActionSource is the optional source capability for an
// action switch (AS): true makes the materialised server advertise AS
// instead of MSR — one InitialPress per press cycle, no ShortRelease and no
// MultiPressOngoing (matter.js SwitchServer.ts:246-251, :337-339) — and
// MSM, which AS makes mandatory (switch.element.ts conformance "AS, [MS &
// MSR]").
type GenericSwitchActionSource interface {
	MatterSwitchActionSwitch() bool
}

// GenericSwitch is the cluster server of the bridge's first releases: MS
// and MSR, MSL when the source recognises long presses, the gestures a
// host narrates through [contract.SwitchEventEmitter]. It is a thin wrapper
// over [switchserver.Server] that keeps its reads as they were —
// NumberOfPositions and, with a [GenericSwitchPositionSource],
// CurrentPosition read live from the source; MultiPressMax answers 0.
//
// Deprecated: use cluster/switch ([switchserver.New]), which the
// measurement materialiser builds for a [GenericSwitchSource]: positions
// as well as gestures, and the LS, MSM and AS features. GenericSwitch may
// be removed in v0.10.0.
type GenericSwitch struct {
	src GenericSwitchSource
	srv *switchserver.Server
}

// NewGenericSwitch wires the cluster-server against a model-side
// source on the given endpoint. The endpoint is captured so events
// can be addressed without the source needing to know its own ID.
//
// Deprecated: use [switchserver.New]; NewGenericSwitch may be removed in
// v0.10.0.
func NewGenericSwitch(endpoint uint16, src GenericSwitchSource) *GenericSwitch {
	features := switchserver.FeatureMS | switchserver.FeatureMSR
	if src.MatterSwitchSupportsLongPress() {
		features |= switchserver.FeatureMSL
	}
	n := src.MatterSwitchPositions()
	if n < 2 {
		n = 2
	}
	srv, err := switchserver.New(switchserver.Config{Features: features, Endpoint: endpoint, NumberOfPositions: n})
	if err != nil {
		// MS + MSR (+ MSL) with two or more positions is in the model.
		panic(fmt.Sprintf("wire: Switch: %v", err))
	}
	return &GenericSwitch{src: src, srv: srv}
}

// Endpoint returns the endpoint this switch addresses its events to.
//
// Exported so a caller can verify the id it supplied actually arrived: the
// endpoint is captured at construction and used only when an event fires, so
// a materialiser that dropped it would go unnoticed until a press event was
// delivered to the wrong Matter path.
func (s *GenericSwitch) Endpoint() uint16 { return s.srv.Endpoint() }

// MatterClusterID identifies the Switch cluster (0x003B).
func (s *GenericSwitch) MatterClusterID() uint32 { return matterClusterGenericSwitch }

// SetMatterEventEmitter implements [contract.EventReceiver].
// Called by the bridge during topology assembly so the cluster can
// fire events outside the request/response cycle.
func (s *GenericSwitch) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.srv.SetMatterEventEmitter(emitter)
}

// MatterRead resolves attribute reads.
func (s *GenericSwitch) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case matterAttrSwitchNumberOfPositions:
		n := s.src.MatterSwitchPositions()
		if n == 0 {
			n = 2
		}
		return n, true
	case matterAttrSwitchCurrentPosition:
		// Live position when the source tracks its press cycle (see
		// [GenericSwitchPositionSource]); otherwise the position the
		// narrated gestures moved the server to.
		if pos, ok := s.src.(GenericSwitchPositionSource); ok {
			return pos.MatterSwitchCurrentPosition(), true
		}
		return s.srv.CurrentPosition(), true
	case matterAttrSwitchMultiPressMax:
		// MSM is off, so MultiPressMax is not served (MatterAttributes
		// omits it); the read answers 0 for controllers that probe
		// optional attributes. MultiPressMax carries "min 2" (matter.js
		// switch.element.ts), which is why the feature is dropped rather
		// than advertised with max=1.
		return uint8(0), true
	default:
		return s.srv.MatterRead(attrID)
	}
}

// MatterWrite rejects all attribute writes — every Switch attribute is
// read-only (access "R V"), so a served one answers UNSUPPORTED_WRITE and
// any other UNSUPPORTED_ATTRIBUTE, as the definition judges them (matter.js
// AttributeWriteResponse).
func (s *GenericSwitch) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke rejects all commands — Switch cluster has no commands.
func (s *GenericSwitch) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, im.UnsupportedCommandf("matter: GenericSwitch has no command 0x%02X", cmdID)
}

// MatterReportable lists the attributes that emit reports: CurrentPosition,
// the only value-bearing attribute.
func (s *GenericSwitch) MatterReportable() []uint32 { return s.srv.MatterReportable() }

// MatterAttributes lists the Switch (0x003B) attributes the feature
// selection makes mandatory: NumberOfPositions and CurrentPosition.
func (s *GenericSwitch) MatterAttributes() []uint32 { return s.srv.MatterAttributes() }

// MatterEvents returns the event IDs this cluster may emit, enabling the
// dispatcher to populate EventList (0xFFFA) for wildcard reads: the
// definition's for the feature selection — InitialPress (MS),
// ShortRelease (MSR), LongPress and LongRelease (MSL).
func (s *GenericSwitch) MatterEvents() []uint32 { return s.srv.MatterEvents() }

// FireInitialPress emits the Matter §1.13.6.1 InitialPress event (priority
// INFO, as every Switch event in matter.js switch.element.ts).
func (s *GenericSwitch) FireInitialPress(newPosition uint8) { s.srv.FireInitialPress(newPosition) }

// FireShortRelease emits the §1.13.6.3 ShortRelease event.
func (s *GenericSwitch) FireShortRelease(previousPosition uint8) {
	s.srv.FireShortRelease(previousPosition)
}

// FireLongPress emits the §1.13.6.2 LongPress event. No-op when the
// source does not advertise long-press support.
func (s *GenericSwitch) FireLongPress(newPosition uint8) { s.srv.FireLongPress(newPosition) }

// FireLongRelease emits the §1.13.6.4 LongRelease event; a no-op without
// long-press support.
func (s *GenericSwitch) FireLongRelease(previousPosition uint8) {
	s.srv.FireLongRelease(previousPosition)
}

// The Switch{event}Event types are the event payloads the Fire*
// methods emit: the generated payloads of switch.element.ts, a structure
// with its one uint8 field at context tag 0 — InitialPress / LongPress
// carry NewPosition, ShortRelease / LongRelease PreviousPosition. They
// encode themselves (spec.Encodable).
//
// MultiPressOngoing / MultiPressComplete have no alias here: their payloads
// are cluster/spec/switchcluster's, which cluster/switch emits.

// SwitchInitialPressEvent is the InitialPress (0x01) payload.
type SwitchInitialPressEvent = swdef.InitialPressEvent

// SwitchLongPressEvent is the LongPress (0x02) payload.
type SwitchLongPressEvent = swdef.LongPressEvent

// SwitchShortReleaseEvent is the ShortRelease (0x03) payload.
type SwitchShortReleaseEvent = swdef.ShortReleaseEvent

// SwitchLongReleaseEvent is the LongRelease (0x04) payload.
type SwitchLongReleaseEvent = swdef.LongReleaseEvent

// Compile-time assertions: GenericSwitch satisfies the bridge-side
// dispatch interfaces and the attribute-lister and event-lister capabilities.
var (
	_ contract.ClusterServer          = (*GenericSwitch)(nil)
	_ contract.EventReceiver          = (*GenericSwitch)(nil)
	_ contract.ClusterAttributeLister = (*GenericSwitch)(nil)
	_ contract.ClusterEventLister     = (*GenericSwitch)(nil)
)
