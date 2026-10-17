// SPDX-License-Identifier: Apache-2.0
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
//
// [Config.RootSigner] and [Config.ICACSigner] take the keys out of the
// store: any P-256 crypto.Signer — the passphrase-encrypted file of
// package keyfile, or a host's PKCS#11 or KMS signer (keyfile's package
// documentation says how to wire one). The record then holds the
// certificates and a KeyRef, no private key; without a signer the store
// keeps the key as matter.js does. commissioner.FabricConfig.OperationalSigner
// is the same seam for the controller's operational key. matter.js has no
// such seam (packages/protocol/src/fabric/Fabric.ts:50-76 at 0ceb202d
// keeps #keyPair).
package ca
