// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package fabric is a Matter v1.1 bridge stack. Subpackages:
//
//   - bootid/        process-lifetime UniqueID salt (rotation off by default)
//   - bridge/        the composition unit: topology assembly, IM dispatcher
//     wiring, UDP listener, mDNS advertisement — what a host starts
//   - cluster/       cluster protocol impls (wire format only)
//   - commissioning/ on-network discovery, attestation, fabric join
//   - conformance/   golden-vector regression + load + chip-tool smoke tests
//   - contract/      the port contracts a host implements to expose a device
//   - diagevent/     bounded in-memory trace of events explaining a failed pairing
//   - eligibility/   candidate list for a host's bridging allowlist UI
//   - endpoint/      endpoint topology assembler
//   - im/            Interaction Model
//   - mdns/          DNS-SD operational + commissionable advertisement
//   - parity/        embedded matter.js HEAD schema snapshot for parity tests
//   - schema/        typed Go lookups over the parity/ snapshot (generated)
//   - secure/        Spake2+ (PASE), Sigma (CASE), session keys, AES-CCM,
//     DAC/PAI/PAA chain validation (secure/attestation/)
//   - store/         fabric / NOC / shared-secrets persistence
//   - tlv/           TLV codec (Matter Core Spec §A.7)
//   - transport/     UDP/IPv6, MRP, message framing
//
// Architecture: rich model, dumb bridge — this module owns the Matter
// wire format only. Per-device cluster projection lives on the host's
// own types, which reach the bridge through the source-surface
// interfaces in [github.com/SukramJ/go-fabric/contract].
package fabric
