// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"context"
	"errors"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// DefaultProbeTimeout is the probe's per-request timeout
// (OtbrRestProbe.ts:15).
const DefaultProbeTimeout = time.Second

// Capability is a border router's REST surface as [Probe] found it,
// matter.js's OtbrRestCapability (OtbrRestCapability.ts:16-29).
type Capability struct {
	BaseURL        string
	KeyFormat      KeyFormat
	ProbedAt       time.Time
	NetworkName    string
	ExtPANID       []byte
	DiagnosticsAPI DiagnosticsAPI
}

// Probe probes the border router cfg names for an OTBR REST surface
// (OtbrRestProbe.probe, OtbrRestProbe.ts:18-51): GET /node confirms the
// endpoint is an OTBR and reveals its key casing, then the diagnostics
// endpoints' statuses its flavour. A Port of 0 is [DefaultPort], a
// Timeout of 0 [DefaultProbeTimeout] (the probe's, not the client's
// default); cfg.Clock stamps ProbedAt. An endpoint that is not an OTBR or
// does not answer is (nil, nil); an error that is not the REST surface's
// is returned.
func Probe(ctx context.Context, cfg Config) (*Capability, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultProbeTimeout
	}
	c, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	format, name, xp, err := c.ProbeNode(ctx)
	if err == nil {
		var api DiagnosticsAPI
		if api, err = c.DetectDiagnosticsAPI(ctx); err == nil {
			return &Capability{BaseURL: c.BaseURL(), KeyFormat: format, ProbedAt: clock.Or(cfg.Clock).Now(), NetworkName: name, ExtPANID: xp, DiagnosticsAPI: api}, nil
		}
	}
	if _, ok := errors.AsType[*Error](err); ok {
		return nil, nil //nolint:nilnil // OtbrRestProbe.ts:42-46: a miss is null, not an error
	}
	return nil, err
}
