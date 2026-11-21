// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

// LocalTemperatureChangeThresholdOf reads the threshold the
// LocalTemperatureChange event measures against, for the tests of the
// package's external test package.
func LocalTemperatureChangeThresholdOf(s *ThermostatServer) int16 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ltEvent.threshold
}
