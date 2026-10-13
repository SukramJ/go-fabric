// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package ca is the controller's certificate authority: the root of trust
// of a fabric the controller owns (its RCAC, optionally an ICAC), which
// issues the controller's own NOC and the NOC of every node it
// commissions. It is the port of matter.js
// packages/protocol/src/certificate/CertificateAuthority.ts at 0ceb202d
// (ADR 0020, stage 2), with the Matter TLV certificate encoder the
// commissioner of bridge/bridgetest uses (ca.go there) grown into
// production code, and the compressed fabric id derivation of
// packages/types/src/datatype/GlobalFabricId.ts.
//
// A [Store] keeps the root (and ICAC) key material and the certificate
// counter across restarts; [MemoryStore] keeps them in memory. Unlike
// matter.js, which writes its storage only when it creates the root, the
// CA saves after every certificate it issues, so serial numbers do not
// repeat after a restart.
package ca
