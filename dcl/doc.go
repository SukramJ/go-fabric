// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package dcl is the client of the CSA Distributed Compliance Ledger: the
// port of matter.js packages/protocol/src/dcl/ at 0ceb202d (ADR 0020
// stage 6) — [Client] is DclClient.ts, [OTAVersions] the parts of
// DclOtaUpdateService.ts that answer whether a software version is in the
// ledger and with which checksum, [VendorInfoService] the vendor and
// product information, and [Store] the store a trust store is kept in
// between runs.
//
// The package is role-neutral (review II, R3-api-3780, decision 553): a
// device's OTA path (package node) and the commissioner (controller/dcl,
// which keeps the PAA and CD signer trust store, the CRL revocation check
// and the GitHub development-certificate fallback) use it alike, so a
// responder that asks the ledger does not link the controller role. It
// imports no role.
//
// Privacy: nothing in this package reaches the network unless the host
// constructs a [Client] and hands it over. With one, the ledger learns
// what is asked of it: the subject key identifiers of the PAAs, PAIs and
// CD signers of the devices a controller attests, and the vendor and
// product ids — and software versions — of the devices checked for
// updates. The embedded vendor names (eligibility/vendor_name.go) need no
// ledger at all.
//
// Only net/http and the standard library are used; the REST shapes are
// those of DclRestApiTypes.ts.
package dcl
