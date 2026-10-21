// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package handshake is the controller's side of session establishment:
// [PASE], the client of the passcode-authenticated handshake a
// commissioner opens to a node in its commissioning window, and [CASE],
// the client of the certificate-authenticated handshake a controller
// opens to a node on its fabric, with session resumption. Both run on
// the unsecured session of a client/exchange Manager and return the
// secure session they established, registered with that manager. They
// are the ports of matter.js packages/protocol/src/session/pase/PaseClient.ts
// and session/case/CaseClient.ts (ADR 0020, stage 1); the cryptography
// is secure/spake2's Prover and secure/sigma's Initiator.
package handshake
