// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"testing"

	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
)

// TestUnconvertibleInitialIsRefused (R2-P6-err-2506): a declared Initial
// value the kit's server cannot take — a negative int, a value beyond the
// attribute's width — is refused by New, not dropped: the self-held
// server's constructor leaves it for the definition's check.
func TestUnconvertibleInitialIsRefused(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func() error{
		"LevelControl OnLevel int -1": func() error {
			_, err := NewDimmableLight(testOptions("on-level"), withInitial(lvldef.ClusterID, map[uint32]any{lvldef.AttrOnLevel: -1}))
			return err
		},
		"LevelControl Options 300": func() error {
			_, err := NewDimmableLight(testOptions("options"), withInitial(lvldef.ClusterID, map[uint32]any{lvldef.AttrOptions: uint64(300)}))
			return err
		},
		"ColorControl ColorTempPhysicalMinMireds 70000": func() error {
			_, err := NewColorTemperatureLight(testOptions("mireds"), withInitial(ccdef.ClusterID, map[uint32]any{ccdef.AttrColorTempPhysicalMinMireds: uint64(70000)}))
			return err
		},
	} {
		if err := build(); !errors.Is(err, ErrConfiguration) {
			t.Errorf("%s: New = %v, want ErrConfiguration", name, err)
		}
	}
}

// TestConsumeKeepsWhatItDoesNotTake (R2-P6-err-2506): consume removes an
// attribute from the Initial only when it takes the value.
func TestConsumeKeepsWhatItDoesNotTake(t *testing.T) {
	t.Parallel()
	initial := map[uint32]any{1: int64(-1), 2: "x", 3: uint64(300), 4: uint64(7)}
	for _, id := range []uint32{1, 2} {
		if _, ok := consume(initial, id); ok {
			t.Errorf("consume(%d) took %v", id, initial[id])
		}
	}
	if _, ok := consume8(initial, 3); ok {
		t.Error("consume8 took 300")
	}
	if v, ok := consume8(initial, 4); !ok || v != 7 {
		t.Errorf("consume8(7) = %d, %v", v, ok)
	}
	for _, id := range []uint32{1, 2, 3} {
		if _, kept := initial[id]; !kept {
			t.Errorf("attribute %d was dropped without being taken", id)
		}
	}
	if _, kept := initial[4]; kept {
		t.Error("attribute 4 was taken but kept")
	}
}
