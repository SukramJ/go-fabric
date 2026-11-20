// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	"github.com/SukramJ/go-fabric/tlv"
)

// TimeFormatLocalizationConfig carries the construction parameters of a
// TimeFormatLocalization server.
type TimeFormatLocalizationConfig struct {
	// HourFormat is the hour format the node starts with: the host's
	// detected one, or the one it persisted. Required: nil is refused
	// with [ErrLocalizationConfig] — matter.js takes its detected hour
	// format there, and detection is the host's (decision 358).
	HourFormat *tfldef.HourFormatEnum
	// ActiveCalendarType is the calendar the node starts with: the
	// host's detected one, or the one it persisted. nil is unset: the
	// first of SupportedCalendarTypes; nil with an empty list is refused
	// with [ErrLocalizationConfig]. A set calendar SupportedCalendarTypes
	// does not hold is refused.
	ActiveCalendarType *tfldef.CalendarTypeEnum
	// SupportedCalendarTypes lists the calendars a controller may choose;
	// empty (nil or zero length) is [ActiveCalendarType].
	SupportedCalendarTypes []tfldef.CalendarTypeEnum
	// NoFeatures selects no feature: FeatureMap 0, the server without
	// CalendarFormat, serving HourFormat alone. The zero value serves
	// CalendarFormat. A calendar configured beside it (ActiveCalendarType
	// or SupportedCalendarTypes) is refused with [ErrLocalizationConfig];
	// HourFormat stays required.
	NoFeatures bool
	// OnWrite, when set, is handed every controller write that passed the
	// checks — attrID is AttrHourFormat or AttrActiveCalendarType, value
	// the new HourFormatEnum or CalendarTypeEnum — before it is stored;
	// the host persists it here. An error refuses the write.
	OnWrite func(ctx context.Context, attrID uint32, value uint8) error
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// NewTimeFormatLocalization builds a TimeFormatLocalization (0x002C)
// server on the generated definition (cluster/spec/
// timeformatlocalization, ADR 0013), with the CalendarFormat feature
// unless NoFeatures.
//
// Mirrors matter.js packages/node/src/behaviors/time-format-localization/
// TimeFormatLocalizationServer.ts at b25ed960: the server is the behavior
// .with("CalendarFormat") (:16) — NoFeatures is the behavior without it,
// whose initialize (:17-42) returns after the hour format (:21-23). It
// seeds an empty or unset SupportedCalendarTypes with [ActiveCalendarType]
// (:24-28), keeps an ActiveCalendarType that is set and supported
// (:29-32), and takes the list's first for an unset one (:34-35). The
// detected values are the host's here — matter.js's detectedHourFormat /
// detectedCalendarType getters are override points (:44-96) — and arrive
// as HourFormat and ActiveCalendarType. These rules depart, on the
// principle of LocalizationConfiguration's (decisions 289, 296, 358, 359;
// notes/parity/by_design.md
// BD-Matter-Localization-ConfiguredStartValueRefused): matter.js prefers
// its detected calendar for an unset one when the list holds it (:34-35),
// where the list's first is taken here; matter.js replaces a
// set calendar the list lacks, with a warning (:36-40), where a configured
// ActiveCalendarType is the host's explicit statement and one the list
// lacks is refused with [ErrLocalizationConfig]; matter.js takes its
// detected hour format for an unset one (:18-20) and its detected calendar
// for an unset one with an empty list (:25), where both are refused here;
// and without the feature matter.js does not look at calendar state
// (:21-23), where a calendar configured beside NoFeatures is refused.
//
// A written ActiveCalendarType that SupportedCalendarTypes does not hold
// is CONSTRAINT_ERROR: the model constraint is "in
// supportedCalendarTypes", enforced by matter.js's validator
// (packages/node/src/behavior/state/validation/constraint.ts:157-168 at
// b25ed960) — and
// by the generated runtime before the sink sees the value
// (spec.Instance.ValidateWrite) — and
// by connectedhomeip (src/app/clusters/time-format-localization-server/
// TimeFormatLocalizationCluster.cpp WriteImpl :133-141,
// IsSupportedCalendarType :30-52, at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20). HourFormat takes any value
// of its enum (:127-131).
func NewTimeFormatLocalization(cfg TimeFormatLocalizationConfig) (*spec.Server, error) {
	if cfg.HourFormat == nil {
		return nil, fmt.Errorf("%w: TimeFormatLocalization: no HourFormat", ErrLocalizationConfig)
	}
	initial := map[uint32]any{tfldef.AttrHourFormat: *cfg.HourFormat}
	var features uint32
	if cfg.NoFeatures {
		if cfg.ActiveCalendarType != nil || len(cfg.SupportedCalendarTypes) > 0 {
			return nil, fmt.Errorf("%w: TimeFormatLocalization: a calendar beside NoFeatures", ErrLocalizationConfig)
		}
	} else {
		supported := slices.Clone(cfg.SupportedCalendarTypes)
		var active tfldef.CalendarTypeEnum
		switch {
		case len(supported) == 0 && cfg.ActiveCalendarType == nil:
			return nil, fmt.Errorf("%w: TimeFormatLocalization: neither ActiveCalendarType nor SupportedCalendarTypes", ErrLocalizationConfig)
		case len(supported) == 0:
			active = *cfg.ActiveCalendarType
			supported = []tfldef.CalendarTypeEnum{active}
		case cfg.ActiveCalendarType == nil:
			active = supported[0]
		case !slices.Contains(supported, *cfg.ActiveCalendarType):
			return nil, fmt.Errorf("%w: TimeFormatLocalization: ActiveCalendarType %d not in SupportedCalendarTypes %v", ErrLocalizationConfig, *cfg.ActiveCalendarType, supported)
		default:
			active = *cfg.ActiveCalendarType
		}
		features = uint32(tfldef.FeatureCalendarFormat)
		initial[tfldef.AttrActiveCalendarType] = active
		initial[tfldef.AttrSupportedCalendarTypes] = enumList[tfldef.CalendarTypeEnum](supported)
	}
	srv, err := spec.NewServer(tfldef.Definition, spec.Options{Features: features}, spec.ServerConfig{
		DataVersion: cfg.DataVersion, //nolint:staticcheck // the deprecated tracker is passed on for its window (decision 165)
		Sink:        calendarSink{onWrite: cfg.OnWrite},
		Initial:     initial,
	})
	if err != nil {
		return nil, fmt.Errorf("core: TimeFormatLocalization: %w", err)
	}
	return srv, nil
}

// calendarSink hands every accepted write to the host. Membership of
// ActiveCalendarType in SupportedCalendarTypes is the runtime's "in"
// constraint, refused before the sink is called.
type calendarSink struct {
	onWrite func(ctx context.Context, attrID uint32, value uint8) error
}

// MatterWriteAttribute implements [spec.Sink]: value is the stored form,
// a uint8 for both writable enums.
func (s calendarSink) MatterWriteAttribute(ctx context.Context, attrID uint32, value any) error {
	v, _ := value.(uint8)
	if s.onWrite != nil {
		if err := s.onWrite(ctx, attrID, v); err != nil {
			return fmt.Errorf("core: TimeFormatLocalization: %w", err)
		}
	}
	return nil
}

// enumList is a list of an 8-bit enum that encodes itself as the
// generated codecs write a list[enum8] (spec.PutList over spec.PutUint):
// the reply encoder takes a [spec.Encodable], and a plain []uint8 would
// go out as an octet string.
type enumList[T ~uint8] []T

// EncodeTLV implements [spec.Encodable].
func (l enumList[T]) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	spec.PutList(spec.PutUint[T])(enc, tag, l)
}
