// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package attestation is the commissioner's device attestation check: the
// port of matter.js
// packages/protocol/src/certificate/DeviceAttestationValidator.ts
// (DeviceAttestationValidator.validate, :170-537) at 0ceb202d, ADR 0020
// stage 2, with a static trust store ([PAAStore], [StaticStore],
// [TestStore]) where matter.js consults its DclCertificateService.
//
// [Validate] runs the checks in matter.js's order: the DAC and PAI parse,
// the local checks (vendor id, nonce, attestation signature), the trust
// store checks when a store is given (PAA, chain, validity at the DAC's
// notBefore, PAA vendor id, revocation), and the Certification
// Declaration (signature, then content). A hard failure is an [*Error];
// warnings and information are [Finding]s of the [Result]. Whether a
// commissioning proceeds on them is the commissioner's [Policy].
//
// Like matter.js, the validator never compares a certificate's validity
// with the current time: the chain is checked at the DAC's notBefore.
package attestation
