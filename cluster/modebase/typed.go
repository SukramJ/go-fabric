// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modebase

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.Hooks]; SetAttributes is
// [Server.SetCurrentMode]'s.
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing. CurrentMode follows [Server.SetCurrentMode] (a supported
// mode, [ErrUnsupportedMode] otherwise); SupportedModes is the
// configuration's ([spec.ErrNotHeld]).
func (s *Server) SetAttributes(values map[uint32]any) error {
	var mode uint8
	set := false
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.inst.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		if id != AttrCurrentMode {
			return fmt.Errorf("%w: modebase: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
		}
		mode, _ = v.(uint8)
		set = true
	}
	if !set {
		return nil
	}
	return s.SetCurrentMode(mode)
}
