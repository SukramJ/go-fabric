// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	bsc "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
	tm "thingfabric.dev/go-fabric/cluster/spec/temperaturemeasurement"
)

// TestSetJudgedAgainstTheStateItEnters holds a host Set to the state it is
// stored into: a Set whose bound names a sibling, judged against the stored
// sibling, is judged again when another Set stored a new sibling before it
// could store. Here Set(MaxMeasuredValue 40) is judged against
// MinMeasuredValue 0, and before it stores, Set(MinMeasuredValue 50)
// completes; MaxMeasuredValue's bound "min minMeasuredValue + 1" refuses 40
// in the state it would enter. Before, both were stored, a state the
// server's own constraint refuses (review II, R2-C2-conc-1302).
func TestSetJudgedAgainstTheStateItEnters(t *testing.T) {
	t.Parallel()
	s, err := spec.NewServer(tm.Definition, spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{
		tm.AttrMinMeasuredValue: int16(0), tm.AttrMaxMeasuredValue: int16(100), tm.AttrMeasuredValue: int16(20),
	}})
	if err != nil {
		t.Fatal(err)
	}
	judged, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	spec.SetJudgedHookForTests(s, func() {
		if first.CompareAndSwap(false, true) {
			close(judged)
			<-release
		}
	})
	done := make(chan error, 1)
	go func() { done <- s.Set(tm.AttrMaxMeasuredValue, int16(40)) }()
	<-judged
	if err := s.Set(tm.AttrMinMeasuredValue, int16(50)); err != nil {
		t.Fatalf("Set(MinMeasuredValue 50): %v", err)
	}
	close(release)
	if err := <-done; !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("Set(MaxMeasuredValue 40) after MinMeasuredValue 50 was stored: %v, want ErrInvalidValue", err)
	}
	if v, _ := s.Value(tm.AttrMaxMeasuredValue); v != int16(100) {
		t.Errorf("MaxMeasuredValue = %v, want the old 100", v)
	}
}

// TestWriteJudgedAgainstTheStateItEnters holds a controller write to the
// state it is stored into: a host Set that stores a sibling while the
// write's veto runs makes the write be judged again before its store.
// CurrentSensitivityLevel 4 passes against SupportedSensitivityLevels 10;
// while the veto runs, the host sets SupportedSensitivityLevels 3, and the
// bound "max supportedSensitivityLevels - 1" refuses 4 in the state the
// write would enter. Before, 4 was stored (review II, R2-C2-conc-1302).
func TestWriteJudgedAgainstTheStateItEnters(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var first atomic.Bool
	s, err := spec.NewServer(bsc.Definition, spec.Options{Features: uint32(bsc.FeatureSensitivityLevel)}, spec.ServerConfig{
		Initial: map[uint32]any{bsc.AttrSupportedSensitivityLevels: uint8(10), bsc.AttrCurrentSensitivityLevel: uint8(0)},
		BeforeWrite: func(context.Context, uint32, any) error {
			if first.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.MatterWrite(context.Background(), bsc.AttrCurrentSensitivityLevel, uint8(4)) }()
	<-entered
	if err := s.Set(bsc.AttrSupportedSensitivityLevels, uint8(3)); err != nil {
		t.Fatalf("Set(SupportedSensitivityLevels 3): %v", err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Error("write of CurrentSensitivityLevel 4 after SupportedSensitivityLevels 3 was stored: nil, want a refusal")
	}
	if v, _ := s.Value(bsc.AttrCurrentSensitivityLevel); v != uint8(0) {
		t.Errorf("CurrentSensitivityLevel = %v, want the old 0", v)
	}
}
