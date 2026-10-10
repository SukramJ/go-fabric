// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	swdiagdef "github.com/SukramJ/go-fabric/cluster/spec/softwarediagnostics"
)

// HeapReporter is the host port for SoftwareDiagnostics' heap attributes,
// read on every attribute read. A false ok — the host cannot tell —
// reads as 0, as connectedhomeip encodes a provider's
// CHIP_ERROR_UNSUPPORTED_CHIP_FEATURE
// (src/app/clusters/software-diagnostics-server/SoftwareDiagnosticsCluster.cpp
// EncodeValue :98-109, at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20).
type HeapReporter interface {
	// CurrentHeapFree is the heap memory, in bytes, free for allocation.
	CurrentHeapFree() (bytes uint64, ok bool)
	// CurrentHeapUsed is the heap memory, in bytes, in use.
	CurrentHeapUsed() (bytes uint64, ok bool)
}

// WatermarkReporter is the host port for the Watermarks (WTRMRK) feature:
// a server whose host provides it serves CurrentHeapHighWatermark and
// accepts ResetWatermarks, as connectedhomeip sets the feature only when
// its provider supports watermarks (SoftwareDiagnosticsCluster.h
// GetFeatureMap :82-88).
type WatermarkReporter interface {
	// CurrentHeapHighWatermark is the most heap memory, in bytes, used
	// since boot or the last ResetWatermarks; a false ok reads as 0.
	CurrentHeapHighWatermark() (bytes uint64, ok bool)
	// ResetWatermarks restarts the high watermark at the current use
	// (SoftwareDiagnosticsCluster.h ResetWatermarks :90). An error
	// answers FAILURE, or the status it carries.
	ResetWatermarks(ctx context.Context) error
}

// SoftwareDiagnosticsConfig carries the construction parameters of a
// SoftwareDiagnostics server.
type SoftwareDiagnosticsConfig struct {
	// Heap, when set, serves CurrentHeapFree and CurrentHeapUsed.
	Heap HeapReporter
	// Watermarks, when set, turns on the WTRMRK feature.
	Watermarks WatermarkReporter
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// NewSoftwareDiagnostics builds a SoftwareDiagnostics (0x0034) server on
// the generated definition (cluster/spec/softwarediagnostics, ADR 0013).
//
// Mirrors matter.js packages/node/src/behaviors/software-diagnostics/
// SoftwareDiagnosticsServer.ts, `export class SoftwareDiagnosticsServer
// extends SoftwareDiagnosticsBehavior {}` (:14): the generated behavior
// with no logic of its own. The values are the host's: its reporters
// answer every read (SoftwareDiagnosticsCluster.cpp ReadAttribute
// :54-83). ThreadMetrics and the SoftwareFault event are not served.
func NewSoftwareDiagnostics(cfg SoftwareDiagnosticsConfig) (*spec.Server, error) {
	var opts spec.Options
	if cfg.Heap != nil {
		opts.Attributes = []uint32{swdiagdef.AttrCurrentHeapFree, swdiagdef.AttrCurrentHeapUsed}
	}
	if cfg.Watermarks != nil {
		opts.Features = uint32(swdiagdef.FeatureWatermarks)
	}
	src := softwareDiagnosticsSource{heap: cfg.Heap, marks: cfg.Watermarks}
	srv, err := spec.NewServer(swdiagdef.Definition, opts, spec.ServerConfig{DataVersion: cfg.DataVersion, Source: src})
	if err != nil {
		return nil, fmt.Errorf("core: SoftwareDiagnostics: %w", err)
	}
	if cfg.Watermarks != nil {
		srv.Handle(swdiagdef.CmdResetWatermarks, func(ctx context.Context, _ any) (any, error) {
			if err := cfg.Watermarks.ResetWatermarks(ctx); err != nil {
				return nil, fmt.Errorf("core: SoftwareDiagnostics: ResetWatermarks: %w", err)
			}
			return nil, nil
		})
	}
	return srv, nil
}

// softwareDiagnosticsSource answers the heap attributes from the host.
type softwareDiagnosticsSource struct {
	heap  HeapReporter
	marks WatermarkReporter
}

// MatterAttribute implements [spec.Source]; the server asks only for the
// attributes it serves.
func (s softwareDiagnosticsSource) MatterAttribute(attrID uint32) (any, bool) {
	var (
		v  uint64
		ok bool
	)
	switch attrID {
	case swdiagdef.AttrCurrentHeapFree:
		v, ok = s.heap.CurrentHeapFree()
	case swdiagdef.AttrCurrentHeapUsed:
		v, ok = s.heap.CurrentHeapUsed()
	case swdiagdef.AttrCurrentHeapHighWatermark:
		v, ok = s.marks.CurrentHeapHighWatermark()
	default:
		return nil, false
	}
	if !ok {
		v = 0
	}
	return v, true
}
