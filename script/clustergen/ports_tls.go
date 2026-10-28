// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// The host ports of the TLS servers (brief 26-A3, concept v10 §D): the
// kit's servers in cluster/tls keep the certificate and endpoint tables
// and their rules, after connectedhomeip's TLSCertificateManagementCluster
// and TLSClientManagementCluster; the key material, the CSR and the
// certificate's validity are the host's, and the tables persist through a
// settings store. No TLS stack enters the module.
var tlsPorts = map[string]portSet{
	"TlsCertificateManagement": {ports: []portDecl{{
		name: "CertificatePort",
		doc: []string{
			"CertificatePort is the node's TLS key store (cluster/tls",
			"CertificateManagement, after connectedhomeip's",
			"TLSCertificateManagementDelegate and CertificateTableImpl).",
			"GenerateCSR makes the client key pair of ccdid — a new one when",
			"newKey is set, the one it made before otherwise — and returns its",
			"DER CSR (at most 3000 bytes) and its signature of the 32-byte",
			"nonce (at most 128 bytes). CertificateValid reports whether a DER",
			"certificate is valid at the current time (chip's",
			"Crypto::IsCertificateValidAtCurrentTime); ClientKeyMatches whether",
			"a client certificate carries ccdid's public key. RemoveClientKey",
			"drops a key whose entry left the table. The server keeps the",
			"tables and calls the port after its own checks; a GenerateCSR",
			"error answers its status, FAILURE without one.",
		},
		methods: []string{
			"GenerateCSR(ctx context.Context, fabricIndex uint8, ccdid uint16, newKey bool, nonce []byte) (csr, nonceSignature []byte, err error)",
			"CertificateValid(ctx context.Context, certificate []byte) bool",
			"ClientKeyMatches(ctx context.Context, fabricIndex uint8, ccdid uint16, certificate []byte) bool",
			"RemoveClientKey(ctx context.Context, fabricIndex uint8, ccdid uint16)",
		},
	}, {
		name: "CertificateStore",
		doc: []string{
			"CertificateStore persists the certificate tables under the",
			"device's StableKey; the node's store (node.Node.Store) is one.",
			"Without one they stay in memory (cluster/tls Store).",
		},
		methods: []string{
			"GetSetting(ctx context.Context, key string) (value string, ok bool, err error)",
			"SetSetting(ctx context.Context, key, value string) error",
		},
	}}},
	"TlsClientManagement": {ports: []portDecl{{
		name: "EndpointStore",
		doc: []string{
			"EndpointStore persists the provisioned TLS endpoints under the",
			"device's StableKey; the node's store (node.Node.Store) is one.",
			"Without one they stay in memory (cluster/tls Store).",
		},
		methods: []string{
			"GetSetting(ctx context.Context, key string) (value string, ok bool, err error)",
			"SetSetting(ctx context.Context, key, value string) error",
		},
	}}},
}

func init() {
	for name, ps := range tlsPorts {
		ports[name] = ps
	}
}
