// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"fmt"
	"net/http"
	"regexp"
	"time"

	"thingfabric.dev/go-fabric/dcl"
	"thingfabric.dev/go-fabric/internal/dclseam"
)

// The ledger client lives in package dcl (review II, R3-api-3780, decision
// 553). What the commissioner's machinery here needs of its internals comes
// through internal/dclseam (decision 577) — the destination guard stays in
// dcl, once — or is a copy of a helper with no state of its own.

// ledgerURLClient is the client of the download of a URL a ledger record
// names, for the ledger client c: dcl's guarded download client
// (dclseam.LedgerURLClient).
func ledgerURLClient(c *dcl.Client) *http.Client { return dclseam.LedgerURLClient(c) }

// httpClient is the client of a request to a URL the host configured, for
// the ledger client c (dclseam.HTTPClient).
func httpClient(c *dcl.Client) *http.Client { return dclseam.HTTPClient(c) }

// maxResponseBody is the bound on an answer the ledger client reads, which
// the GitHub fallback's answers share (dclseam.MaxResponseBody).
func maxResponseBody() int64 { return dclseam.MaxResponseBody() }

// withTimeout is the client with another request timeout — the per-call
// DclClient.Options of matter.js (DclClient.ts:424-428): a copy of c, its
// unexported fields (the guard's test seam among them) carried over.
func withTimeout(c *dcl.Client, d time.Duration) *dcl.Client {
	cc := *c
	cc.Timeout = d
	return &cc
}

// dclErrorf is a ledger answer the store cannot use, as a *dcl.Error.
func dclErrorf(format string, args ...any) error {
	return &dcl.Error{Message: fmt.Sprintf(format, args...)}
}

// hexPairs splits a key identifier into the pairs of the ledger's form, as
// dcl's own does.
var hexPairs = regexp.MustCompile(`.{1,2}`)
