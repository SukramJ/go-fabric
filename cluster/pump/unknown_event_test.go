// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/pump"
)

// TestUnknownEventNamesTheEvent (R2-P6-err-1590): ErrUnknownEvent names
// the event id the definition lacks.
func TestUnknownEventNamesTheEvent(t *testing.T) {
	t.Parallel()
	_, err := pump.NewServer(pump.Config{Source: &device{}, Features: pump.FeatureConstantFlow, Events: []uint32{0x11}})
	if !errors.Is(err, pump.ErrUnknownEvent) || err.Error() != "pump: not a PumpConfigurationAndControl event: 0x11" {
		t.Errorf("unknown event: %v", err)
	}
}
