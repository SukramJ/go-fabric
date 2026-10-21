// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tc "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
)

// The typed server's optional interfaces over [spec.Hooks]; SetAttributes
// is the setters'.
var (
	_ spec.AttributeSetter   = (*TemperatureControlServer)(nil)
	_ spec.CommandRegistrar  = (*TemperatureControlServer)(nil)
	_ spec.EventEmitting     = (*TemperatureControlServer)(nil)
	_ spec.BeforeWriteSetter = (*TemperatureControlServer)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the device's own
// change, all or nothing — TemperatureSetpoint
// ([TemperatureControlServer.SetTemperatureSetpoint], TN) and
// SelectedTemperatureLevel
// ([TemperatureControlServer.SetSelectedTemperatureLevel], TL), each
// checked against the cluster's rules before either is applied. The
// limits, Step and SupportedTemperatureLevels are the configuration's
// ([spec.ErrNotHeld]).
func (s *TemperatureControlServer) SetAttributes(values map[uint32]any) error {
	var apply []func() error
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case tc.AttrTemperatureSetpoint:
			t, _ := v.(int16)
			if err := s.checkTarget(t); err != nil {
				return fmt.Errorf("%w: %w", ErrTemperatureControlValue, err)
			}
			apply = append(apply, func() error { return s.SetTemperatureSetpoint(t) })
		case tc.AttrSelectedTemperatureLevel:
			l, _ := v.(uint8)
			if err := s.checkLevel(l); err != nil {
				return fmt.Errorf("%w: %w", ErrTemperatureControlValue, err)
			}
			apply = append(apply, func() error { return s.SetSelectedTemperatureLevel(l) })
		default:
			return fmt.Errorf("%w: thermo: TemperatureControl attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
		}
	}
	for _, fn := range apply {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

// Handle implements [spec.CommandRegistrar] ([spec.Hooks]).
func (s *TemperatureControlServer) Handle(cmdID uint32, fn spec.CommandHandler) error {
	return s.hooks.Handle(cmdID, fn)
}

// Emit implements [spec.EventEmitting]: TemperatureControl defines no
// event, so every one is [spec.ErrNotEmitted].
func (s *TemperatureControlServer) Emit(endpoint uint16, eventID uint32, data any) error {
	return s.hooks.Emit(endpoint, eventID, data)
}

// SetBeforeWrite implements [spec.BeforeWriteSetter] ([spec.Hooks]).
func (s *TemperatureControlServer) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.hooks.SetBeforeWrite(fn)
}
