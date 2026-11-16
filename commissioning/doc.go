// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package commissioning is a second, standalone implementation of the
// commissionee's side of Matter commissioning: a PASE responder, the
// AttestationResponse and CSRResponse payloads and a DAC chain check.
// Nothing in this module uses it — the node serves commissioning through
// its commissioning window (bridge, node) and the OperationalCredentials
// server of cluster/core — and it differs from that served path: its
// PASE responder takes the trivial passcodes secure/setup refuses, its
// attestation timestamp is the Unix time where the node writes 0, and
// its CSR has an empty subject where the node's says O=CSR (review II,
// R2-P6-sec-0104, R2-P6-par-0105).
//
// Deprecated: the node's commissioning window and
// cluster/core.OperationalCredentials serve commissioning, and
// controller/attestation checks a device's attestation; each exported
// identifier names its replacement. Removable in the next minor release.
//
// The phases of commissioning (Matter Core Spec §5); the package covers
// the first three:
//
//  1. PASE establishment — Spake2+ handshake using the operator-
//     supplied passcode. Produces a PASE [secure/channel.Session]
//     good for the commissioning window only.
//  2. Attestation — commissioner sends an AttestationRequest with a
//     32-byte nonce; the bridge returns the AttestationElements TLV
//     blob plus an ECDSA signature over (elements ‖ attestation
//     challenge) using the bridge's DAC private key.
//  3. CSR — commissioner sends CSRRequest with a 32-byte nonce; the
//     bridge generates a fresh P-256 keypair and returns a PKCS#10
//     CSR wrapped in NOCSR Elements TLV plus another DAC signature.
//  4. NOC install — commissioner sends AddTrustedRootCertificate +
//     AddNOC; the bridge persists both via the OperationalCredentials
//     cluster (Stufe 4) and binds the new fabric.
//  5. CASE handover — commissioner re-establishes a session via
//     Sigma using the freshly installed NOC; the bridge tears down
//     the PASE session.
//
// The DAC/PAI/PAA chain is X.509 DER (Matter §6.2/§6.3/§6.4) — not
// Matter-TLV. The Matter NOC chain is TLV-encoded; that decoder lives
// in [github.com/SukramJ/go-fabric/secure/mattercert].
package commissioning
