// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TimeFormatLocalizationConfig carries the construction parameters of a
// TimeFormatLocalization server.
type TimeFormatLocalizationConfig struct {
	// HourFormat is the hour format the node starts with: the host's
	// detected one, or the one it persisted.
	HourFormat tfldef.HourFormatEnum
	// ActiveCalendarType is the calendar the node starts with: the
	// host's detected one, or the one it persisted.
	ActiveCalendarType tfldef.CalendarTypeEnum
	// SupportedCalendarTypes lists the calendars a controller may choose;
	// nil is [ActiveCalendarType]. It must hold ActiveCalendarType.
	SupportedCalendarTypes []tfldef.CalendarTypeEnum
	// OnWrite, when set, is handed every controller write that passed the
	// checks — attrID is AttrHourFormat or AttrActiveCalendarType, value
	// the new HourFormatEnum or CalendarTypeEnum — before it is stored;
	// the host persists it here. An error refuses the write.
	OnWrite func(ctx context.Context, attrID uint32, value uint8) error
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// NewTimeFormatLocalization builds a TimeFormatLocalization (0x002C)
// server on the generated definition (cluster/spec/
// timeformatlocalization, ADR 0013), with the CalendarFormat feature.
//
// Mirrors matter.js packages/node/src/behaviors/time-format-localization/
// TimeFormatLocalizationServer.ts: the server is the behavior
// .with("CalendarFormat") (:16); initialize (:17-27) takes HourFormat and
// ActiveCalendarType from the detected values when unset — the host's
// here, as matter.js's detectedHourFormat / detectedCalendarType getters
// are override points (:29-81) — and SupportedCalendarTypes defaults to
// [ActiveCalendarType].
//
// A written ActiveCalendarType that SupportedCalendarTypes does not hold
// is CONSTRAINT_ERROR: the model constraint is "in
// supportedCalendarTypes", enforced by matter.js's validator
// (packages/node/src/behavior/state/validation/constraint.ts:153-163) and
// by connectedhomeip (src/app/clusters/time-format-localization-server/
// TimeFormatLocalizationCluster.cpp WriteImpl :133-141,
// IsSupportedCalendarType :30-52, at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20). HourFormat takes any value
// of its enum (:127-131).
func NewTimeFormatLocalization(cfg TimeFormatLocalizationConfig) (*spec.Server, error) {
	supported := slices.Clone(cfg.SupportedCalendarTypes)
	if supported == nil {
		supported = []tfldef.CalendarTypeEnum{cfg.ActiveCalendarType}
	}
	if !slices.Contains(supported, cfg.ActiveCalendarType) {
		return nil, fmt.Errorf("%w: TimeFormatLocalization: ActiveCalendarType %d not in SupportedCalendarTypes %v", ErrLocalizationConfig, cfg.ActiveCalendarType, supported)
	}
	srv, err := spec.NewServer(tfldef.Definition, spec.Options{Features: uint32(tfldef.FeatureCalendarFormat)}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Sink:        calendarSink{supported: supported, onWrite: cfg.OnWrite},
		Initial: map[uint32]any{
			tfldef.AttrHourFormat:             cfg.HourFormat,
			tfldef.AttrActiveCalendarType:     cfg.ActiveCalendarType,
			tfldef.AttrSupportedCalendarTypes: enumList[tfldef.CalendarTypeEnum](supported),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("core: TimeFormatLocalization: %w", err)
	}
	return srv, nil
}

// calendarSink holds a written ActiveCalendarType to the fixed
// SupportedCalendarTypes and hands every accepted write to the host.
type calendarSink struct {
	supported []tfldef.CalendarTypeEnum
	onWrite   func(ctx context.Context, attrID uint32, value uint8) error
}

// MatterWriteAttribute implements [spec.Sink]: value is the stored form,
// a uint8 for both writable enums.
func (s calendarSink) MatterWriteAttribute(ctx context.Context, attrID uint32, value any) error {
	v, _ := value.(uint8)
	if attrID == tfldef.AttrActiveCalendarType && !slices.Contains(s.supported, tfldef.CalendarTypeEnum(v)) {
		return spec.Errorf(im.StatusConstraintError, "TimeFormatLocalization: ActiveCalendarType %d is not in SupportedCalendarTypes", v)
	}
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
