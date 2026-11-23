// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package dclseam carries the one-way seam through which the commissioner's
// ledger machinery in controller/dcl reaches the internals of the ledger
// client in package dcl: the guarded client of a ledger-named download, the
// client of a host-configured request, the bound on an answer's size, and,
// for the tests of the CRL and GitHub downloads, the test seam of the
// destination guard.
//
// Direction of the edge: package dcl installs the hooks below from its init;
// package controller/dcl reads them. It mirrors
// [thingfabric.dev/go-fabric/internal/channelseam] and exists for the same
// reason (review II, R3-api-3780, decision 577): the ledger client moved to
// the role-neutral package dcl (decision 553), while the CRL download of the
// revocation check and the GitHub development-certificate fallback stayed
// with the commissioner. Both download what a ledger record names and must do
// so through the one destination guard dcl owns. Exporting that client from
// dcl.Client would make it a production affordance every consumer sees, and
// copying the guard would leave two of a security check.
//
// The seam is module-internal, so it cannot become a consumer-visible
// surface.
package dclseam

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
)

// LedgerURLClient returns the client of the download of a URL a ledger
// record names, for the *dcl.Client c: the host's HTTPClient, else the
// destination-guarded client unless AllowPrivateDestinations, each refusing
// a redirect away from https. A nil *dcl.Client gets the module's guarded
// client.
//
// The client is threaded through as any because this package must not
// import dcl: dcl imports this one to install the hook, and the reverse edge
// would close an import cycle. Anything that is not a *dcl.Client is read as
// a nil one, so a wrong type gets the guarded client, never an unguarded one.
var LedgerURLClient func(c any) *http.Client

// HTTPClient returns the client of a request to a URL the host configured,
// for the *dcl.Client c: its HTTPClient, else http.DefaultClient. c must be a
// non-nil *dcl.Client.
var HTTPClient func(c any) *http.Client

// MaxResponseBody returns the bound on an answer the ledger client reads,
// which the GitHub fallback's answers share.
var MaxResponseBody func() int64

// IsDestinationRefused reports whether err is, or wraps, the refusal of a
// ledger-named download by the destination guard — for the tests of the CRL
// and GitHub downloads.
var IsDestinationRefused func(err error) bool

// GuardConfig configures the destination guard [SetTestGuard] installs. Its
// zero value is the module's guard; every field is a test seam.
type GuardConfig struct {
	// Resolver resolves the names; nil is net.DefaultResolver.
	Resolver *net.Resolver
	// Admit, when it reports true, admits an address the guard refuses.
	Admit func(netip.AddrPort) bool
	// Proxy is the transport's proxy; nil is http.ProxyFromEnvironment.
	Proxy func(*http.Request) (*url.URL, error)
	// TLS is the transport's TLS configuration; nil is net/http's.
	TLS *tls.Config
}

// SetTestGuard gives the *dcl.Client c a destination guard built from cfg in
// place of the module's, for a test of a ledger-named download. It reports
// false when c is not a non-nil *dcl.Client. Production code does not call
// it.
var SetTestGuard func(c any, cfg GuardConfig) bool
