// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarmbase

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.SelfHeld]; SetAttributes holds chip's rules.
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing, checked against each attribute's type first. Mask and
// State follow [Server.SetMask] and [Server.SetState] — chip's SetMask and
// SetState (AlarmBaseCluster.cpp:34-82): an alarm outside Supported, or a
// State outside the Mask, is refused ([ErrNotSupported]), a latched alarm
// stays active, and a changed State emits Notify. The TemperatureAlarm
// thresholds are [Server.SetThresholds]'. Supported and Latch are the
// configuration's: a different value is refused ([spec.ErrNotHeld]). No
// Delegate is asked.
func (s *Server) SetAttributes(values map[uint32]any) error {
	ids := slices.Sorted(maps.Keys(values))
	mask, state := s.Mask(), s.State()
	var setMask, setState bool
	thresholds := map[uint32]int16{}
	for _, id := range ids {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case AttrSupported, AttrLatch:
			fixed := s.supported
			if id == AttrLatch {
				fixed = s.latch
			}
			if n, _ := v.(uint32); n != fixed {
				return fmt.Errorf("%w: alarmbase: attribute 0x%04X is fixed at 0x%X", spec.ErrNotHeld, id, fixed)
			}
		case AttrMask:
			mask, _ = v.(uint32)
			setMask = true
		case AttrState:
			state, _ = v.(uint32)
			setState = true
		default: // a TemperatureAlarm threshold, the only other served attribute
			t, _ := v.(int16)
			thresholds[id] = t
		}
	}
	if mask&^s.supported != 0 || (setState && state&^mask != 0) {
		return fmt.Errorf("%w: Mask 0x%X, State 0x%X, Supported 0x%X", ErrNotSupported, mask, state, s.supported)
	}
	if len(thresholds) > 0 {
		if err := s.SetThresholds(thresholds); err != nil {
			return err
		}
	}
	s.op.Lock()
	defer s.op.Unlock()
	if setMask {
		if err := s.setMask(mask); err != nil {
			return err
		}
	}
	if setState {
		return s.setState(state, false)
	}
	return nil
}
