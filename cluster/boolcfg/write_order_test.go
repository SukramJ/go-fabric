// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"thingfabric.dev/go-fabric/cluster/boolcfg"
	def "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
)

// TestWrittenSensitivityFollowsTheServersState (R3-conc-3203): a host's
// SetCurrentSensitivityLevel that lands between a controller write's Sink
// and the write's store leaves the served value and the server's state
// equal. Before the fix the Sink set the field to the written level, the
// host set and published its own, and the write then stored the written
// level: State() read the host's level while reads served the
// controller's.
func TestWrittenSensitivityFollowsTheServersState(t *testing.T) {
	t.Parallel()
	for _, write := range []string{"MatterWrite", "MatterWriteChanged"} {
		t.Run(write, func(t *testing.T) {
			t.Parallel()
			s, err := boolcfg.New(boolcfg.Config{Features: boolcfg.FeatureSensitivityLevel, SupportedSensitivityLevels: 4})
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			boolcfg.HoldAfterSensitivitySink(s, entered, release)
			done := make(chan error, 1)
			go func() {
				if write == "MatterWrite" {
					done <- s.MatterWrite(context.Background(), def.AttrCurrentSensitivityLevel, uint64(1))
					return
				}
				_, err := s.MatterWriteChanged(context.Background(), def.AttrCurrentSensitivityLevel, uint64(1))
				done <- err
			}()
			<-entered
			if err := s.SetCurrentSensitivityLevel(2); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			state := s.State().CurrentSensitivityLevel
			served, _ := s.MatterRead(def.AttrCurrentSensitivityLevel)
			if fmt.Sprintf("%d", served) != strconv.FormatUint(uint64(state), 10) {
				t.Errorf("CurrentSensitivityLevel serves %v while the server's state is %d", served, state)
			}
		})
	}
}
