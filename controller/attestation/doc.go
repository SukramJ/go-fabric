// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package attestation is the commissioner's device attestation check: the
// port of matter.js
// packages/protocol/src/certificate/DeviceAttestationValidator.ts
// (DeviceAttestationValidator.validate, :258-645) at db021da0, ADR 0020
// stage 2, with a static trust store ([PAAStore], [StaticStore],
// [TestStore]) where matter.js consults its DclCertificateService.
//
// The package's comments cite that file as DAV: DAV:<n> is line n of
// DeviceAttestationValidator.ts at db021da0, and script/citecheck checks
// each as a citation of that file.
//
// [Validate] runs the checks in matter.js's order: the DAC and PAI parse,
// the local checks (vendor id, nonce, attestation signature), the trust
// store checks when a store is given (PAA, chain, validity at the DAC's
// notBefore, PAA vendor id, revocation), and the Certification
// Declaration (signature, then content). A hard failure is an [*Error];
// warnings and information are [Finding]s of the [Result]. Whether a
// commissioning proceeds on them is the commissioner's [Policy].
//
// Like matter.js, the validator checks the chain at the DAC's notBefore.
// Beyond matter.js it raises three error findings on the device's own
// certificates, so the [Policy] decides and the commissioner records no
// attested pair: a DAC without a ProductID, a PAI ProductID that is not
// the DAC's, and — when [Context].Clock is set, as the commissioner sets
// it — a DAC past its notAfter. connectedhomeip refuses all three
// (src/credentials/attestation_verifier/DefaultDeviceAttestationVerifier.cpp
// :553-558, :638-641 at 6170af84); here they are findings (review II,
// R2-P6-par-1804, R2-P6-par-1805, owner decision 193).
package attestation
