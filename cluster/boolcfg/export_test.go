// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg

import (
	"context"
	"sync/atomic"

	def "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
)

// WriteSensitivityThroughSink hands level to the server's Sink as the
// generated server does after its checks — the path a write takes when the
// stored CurrentSensitivityLevel and the level the Sink holds part, which
// no sequential write produces.
func WriteSensitivityThroughSink(ctx context.Context, s *Server, level uint8) error {
	return sensitivitySink{s}.MatterWriteAttribute(ctx, def.AttrCurrentSensitivityLevel, level)
}

// HoldNextPublish makes the first publish of s that names attrID signal
// entered and wait until release closes.
func HoldNextPublish(s *Server, attrID uint32, entered chan<- struct{}, release <-chan struct{}) {
	var held atomic.Bool
	s.publishing = func(values map[uint32]any) {
		if _, ok := values[attrID]; !ok || !held.CompareAndSwap(false, true) {
			return
		}
		close(entered)
		<-release
	}
}

// HoldAfterSensitivitySink makes the next controller write of
// CurrentSensitivityLevel signal entered once its Sink set the field, and
// wait there until release closes, before the generated server stores it.
func HoldAfterSensitivitySink(s *Server, entered chan<- struct{}, release <-chan struct{}) {
	var held atomic.Bool
	s.sinkSet = func() {
		if !held.CompareAndSwap(false, true) {
			return
		}
		close(entered)
		<-release
	}
}
