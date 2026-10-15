// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package dcl is the controller's client of the CSA Distributed Compliance
// Ledger: the port of matter.js packages/protocol/src/dcl/ at 0ceb202d
// (ADR 0020 stage 6) — [Client] is DclClient.ts, [CertificateStore] the
// parts of DclCertificateService.ts the attestation check consults (the
// PAA and CD signer trust store, refreshed daily, and the CRL revocation
// check of Matter Core §6.2.6.1), [OTAVersions] the parts of
// DclOtaUpdateService.ts that answer whether a software version is in
// the ledger and with which checksum.
//
// Privacy: nothing in this package reaches the network unless the host
// constructs a [Client] and hands it over. With one, the ledger learns
// what the controller asks it: the subject key identifiers of the PAAs,
// PAIs and CD signers of the devices it attests (CD signers and revocation
// points are looked up by key identifier, on demand), and the vendor and
// product ids — and software versions — of the devices it checks for
// updates. The GitHub development-certificate fallback, which asks
// api.github.com, is off unless [CertificateOptions.FetchGitHubCertificates]
// turns it on. The embedded vendor names (eligibility/vendor_name.go) need
// no ledger at all.
//
// Only net/http and the standard library are used; the REST shapes are
// those of DclRestApiTypes.ts.
package dcl
