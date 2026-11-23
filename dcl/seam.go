// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"errors"
	"net/http"

	"thingfabric.dev/go-fabric/internal/dclseam"
)

// init installs the hooks of internal/dclseam, through which the
// commissioner's CRL download and GitHub fallback in controller/dcl reach
// this client's guarded download client (review II, R3-api-3780,
// decision 577).
func init() {
	dclseam.LedgerURLClient = func(v any) *http.Client {
		c, _ := v.(*Client)
		return c.ledgerURLClient()
	}
	dclseam.HTTPClient = func(v any) *http.Client {
		c, _ := v.(*Client)
		return c.httpClient()
	}
	dclseam.MaxResponseBody = func() int64 { return maxResponseBody }
	dclseam.IsDestinationRefused = func(err error) bool { return errors.Is(err, errDestination) }
	dclseam.SetTestGuard = func(v any, cfg dclseam.GuardConfig) bool {
		c, ok := v.(*Client)
		if !ok || c == nil {
			return false
		}
		c.guard = newGuardedClient(guardConfig{resolver: cfg.Resolver, admit: cfg.Admit, proxy: cfg.Proxy, tls: cfg.TLS})
		return true
	}
}
