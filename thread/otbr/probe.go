// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"context"
	"errors"
	"net/http"
	"time"
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

// Probe probes host:port for an OTBR REST surface (OtbrRestProbe.probe,
// OtbrRestProbe.ts:18-51): GET /node confirms the endpoint is an OTBR and
// reveals its key casing, then the diagnostics endpoints' statuses its
// flavour. A port of 0 is [DefaultPort], a timeout of 0
// [DefaultProbeTimeout]. An endpoint that is not an OTBR or does not
// answer is (nil, nil); an error that is not the REST surface's is
// returned. httpClient may be nil.
func Probe(ctx context.Context, host string, port int, timeout time.Duration, httpClient *http.Client) (*Capability, error) {
	if timeout == 0 {
		timeout = DefaultProbeTimeout
	}
	c, err := NewClient(Config{Host: host, Port: port, Timeout: timeout, HTTPClient: httpClient})
	if err != nil {
		return nil, err
	}
	format, name, xp, err := c.ProbeNode(ctx)
	if err == nil {
		var api DiagnosticsAPI
		if api, err = c.DetectDiagnosticsAPI(ctx); err == nil {
			return &Capability{BaseURL: c.BaseURL(), KeyFormat: format, ProbedAt: time.Now(), NetworkName: name, ExtPANID: xp, DiagnosticsAPI: api}, nil
		}
	}
	if _, ok := errors.AsType[*Error](err); ok {
		return nil, nil //nolint:nilnil // OtbrRestProbe.ts:42-46: a miss is null, not an error
	}
	return nil, err
}
