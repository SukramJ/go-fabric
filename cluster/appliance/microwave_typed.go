// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package appliance

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	mwo "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	"github.com/SukramJ/go-fabric/im"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.SelfHeld]; SetAttributes holds the
// setters' rules.
var (
	_ spec.AttributeSetter   = (*MicrowaveServer)(nil)
	_ spec.CommandRegistrar  = (*MicrowaveServer)(nil)
	_ spec.EventEmitting     = (*MicrowaveServer)(nil)
	_ spec.BeforeWriteSetter = (*MicrowaveServer)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the device's own
// change, all or nothing, each value held to its setter's rule first —
// CookTime 1 to MaxCookTime ([MicrowaveServer.SetCookTime]), PowerSetting
// within the power limits (PWRNUM, [MicrowaveServer.SetPowerSetting]),
// SelectedWattIndex an index into SupportedWatts (WATTS,
// [MicrowaveServer.SetSelectedWattIndex]). The other attributes are the
// configuration's ([spec.ErrNotHeld]).
func (s *MicrowaveServer) SetAttributes(values map[uint32]any) error {
	checked := make(map[uint32]any, len(values))
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case mwo.AttrCookTime:
			n, _ := v.(uint32)
			if n < 1 || n > s.MaxCookTime() {
				return spec.Errorf(im.StatusConstraintError, "appliance: CookTime %d outside 1..%d", n, s.MaxCookTime())
			}
		case mwo.AttrPowerSetting:
			n, _ := v.(uint8)
			if err := s.checkPower(n); err != nil {
				return err
			}
		case mwo.AttrSelectedWattIndex:
			if n, _ := v.(uint8); n > s.maxWattIndex() {
				return spec.Errorf(im.StatusConstraintError, "appliance: watt index %d above %d", n, s.maxWattIndex())
			}
		default:
			return fmt.Errorf("%w: appliance: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
		}
		checked[id] = v
	}
	return s.srv.SetAttributes(checked)
}
