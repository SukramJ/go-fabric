// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package fabric is Matter in pure Go: the device and bridge side and a
// full commissioner/controller, as an embeddable library. A semantic port
// of matter.js; not certified by the Connectivity Standards Alliance.
// Subpackages:
//
//   - node/          the facade: one node (root, aggregator, persistence,
//     commissioning, OTA, client stack) from a config — where a host starts
//   - device/        the generated Matter device types a host composes
//   - bridge/        the composition root under node/: topology assembly,
//     IM dispatcher wiring, UDP/TCP listener, mDNS — for hosts that wire it
//   - contract/      the port contracts a host implements to expose a device
//   - cluster/       cluster servers (core/ the system set, spec/ the
//     generated definitions, media/ the media and content clusters, …)
//   - endpoint/      endpoint topology assembler and device-type validation
//   - im/            Interaction Model
//   - controller/    the commissioner/controller role: discovery, PASE/CASE
//     client, attestation (DCL opt-in), CA, client interaction, the client
//     node model and registry
//   - binding/, groups/  bindings and group messaging (send and receive)
//   - bdx/           Bulk Data Exchange (OTA images, diagnostic logs)
//   - commissioning/ on-network discovery, attestation, fabric join
//   - secure/        PASE, CASE, session keys, AES-CCM, attestation, setup codes
//   - transport/     UDP/IPv6, TCP, MRP, message framing
//   - mdns/          DNS-SD advertisement and re-announce
//   - store/         SQLite persistence: fabrics, NOCs, settings, migrations
//   - tlv/           TLV codec (Matter Core Spec §A.7)
//   - schema/, parity/  the embedded matter.js schema and its typed lookups
//   - bootid/, diagevent/, eligibility/  UniqueID salt, pairing trace, host allowlist helpers
//   - conformance/   golden-vector regression, load and chip-tool smoke tests
//
// Architecture: rich model, dumb bridge — this module owns the Matter
// wire format only. Per-device cluster projection lives on the host's
// own types, which reach the bridge through the source-surface
// interfaces in [github.com/SukramJ/go-fabric/contract]. See
// docs/architecture.md.
package fabric
