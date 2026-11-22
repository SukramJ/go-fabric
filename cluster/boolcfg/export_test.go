// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg

import (
	"context"

	def "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
)

// WriteSensitivityThroughSink hands level to the server's Sink as the
// generated server does after its checks — the path a write takes when the
// stored CurrentSensitivityLevel and the level the Sink holds part, which
// no sequential write produces.
func WriteSensitivityThroughSink(ctx context.Context, s *Server, level uint8) error {
	return sensitivitySink{s}.MatterWriteAttribute(ctx, def.AttrCurrentSensitivityLevel, level)
}
