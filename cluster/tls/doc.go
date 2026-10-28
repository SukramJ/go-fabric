// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package tls holds the kit's servers of the two TLS clusters a camera's
// push transport depends on: TlsCertificateManagement
// ([CertificateManagement]) keeps the root certificates and the client
// certificates of every fabric, TlsClientManagement ([ClientManagement])
// the TLS endpoints they provision. Both are connectedhomeip's server
// logic at 6170af84 (TLSCertificateManagementCluster.cpp,
// TLSClientManagementCluster.cpp and their example delegates' tables);
// matter.js's server for each is the generated behaviour
// (TlsCertificateManagementServer.ts:14, TlsClientManagementServer.ts:14
// at 862ad75f).
//
// The module keeps the tables and their rules — the size bounds, the
// per-fabric capacities, the node-wide ids, the duplicate and dependency
// checks, the fabric's entries leaving with the fabric — and persists them
// through a [Store]. The key material is the host's: a
// [CertificatePort] generates the client key pairs and their CSRs and
// judges a certificate's validity. No TLS library enters the module; a
// certificate's fingerprint is its SHA-256 (crypto/sha256), as chip's
// example delegate computes it.
//
// A ClientManagement checks the certificates an endpoint names against the
// node's CertificateManagement and is that server's [DependencyChecker]:
// a certificate a provisioned endpoint names is not removed.
package tls
