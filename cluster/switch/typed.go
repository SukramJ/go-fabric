// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package switchserver

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// are the embedded store's; SetAttributes runs the press logic.
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: CurrentPosition is the
// host's write of the position it debounced itself
// ([Server.SetCurrentPosition] — matter.js's write of
// state.currentPosition, which the press logic and its events follow,
// SwitchServer.ts:177-190); NumberOfPositions and MultiPressMax are the
// configuration's ([spec.ErrNotHeld]).
func (s *Server) SetAttributes(values map[uint32]any) error {
	var position uint8
	set := false
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		if id != swdef.AttrCurrentPosition {
			return fmt.Errorf("%w: switch: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
		}
		position, _ = v.(uint8)
		set = true
	}
	if !set {
		return nil
	}
	return s.SetCurrentPosition(position)
}
