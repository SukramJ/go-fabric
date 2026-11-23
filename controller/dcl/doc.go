// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package dcl is the controller's use of the CSA Distributed Compliance
// Ledger: the port of matter.js packages/protocol/src/dcl/ at 0ceb202d
// (ADR 0020 stage 6) — [CertificateStore] the parts of
// DclCertificateService.ts the attestation check consults (the PAA and CD
// signer trust store, refreshed daily, and the CRL revocation check of
// Matter Core §6.2.6.1).
//
// The ledger client itself — [dcl.Client], the OTA answers [dcl.OTAVersions],
// the vendor information and the trust store's [dcl.Store] — moved to the
// role-neutral package thingfabric.dev/go-fabric/dcl (review II,
// R3-api-3780, decision 553). Its names still resolve here, deprecated, for
// the deprecation window (moved.go); the CRL download and the GitHub
// fallback reach its guarded download client through internal/dclseam
// (decision 577).
//
// Privacy: nothing in this package reaches the network unless the host
// constructs a [dcl.Client] and hands it over. With one, the ledger learns
// what the controller asks it: the subject key identifiers of the PAAs,
// PAIs and CD signers of the devices it attests (CD signers and revocation
// points are looked up by key identifier, on demand), and the vendor and
// product ids — and software versions — of the devices it checks for
// updates. The GitHub development-certificate fallback, which asks
// api.github.com, is off unless [CertificateOptions.FetchGitHubCertificates]
// turns it on. The embedded vendor names (eligibility/vendor_name.go) need
// no ledger at all.
//
// Offline trust: package snapshot embeds the production PAAs and CD
// signers of the ledger, fetched with a [dcl.Client] by
// `make update-dcl-snapshot` — a review step like the schema refresh, run
// by hand and committed after reading the diff, never automatic; its
// SOURCE file names the ledger, the fetch time, the count per kind and
// the SHA-256 of every file. attestation.Trust layers it under a
// [CertificateStore]. Revocation stays online: the snapshot carries no
// CRL, and the CRL check is the [CertificateStore]'s alone.
package dcl
