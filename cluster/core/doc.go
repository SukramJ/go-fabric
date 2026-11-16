// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package core hosts the Matter cluster servers of the system and utility
// clusters: the ones a root node, a bridged endpoint or any device type
// mandates or offers whatever the device does, per Matter 1.6.1. They carry
// no device-specific semantics — they describe the node, its fabrics, its
// commissioning and the bridged-device metadata. Which endpoint mounts
// which server is the device type's decision (device/, node/, the endpoint
// assembler), not this package's.
//
// The servers, by constructor:
//
//   - Commissioning and security: [NewGeneralCommissioning],
//     [NewNetworkCommissioning], [NewWiFiNetworkCommissioning],
//     [NewThreadNetworkCommissioning], [NewOperationalCredentials],
//     [NewAccessControl], [NewGroupKeyManagement], [NewGroups],
//     [NewGroupcast], [NewBinding], [NewCommissionerControl],
//     [NewICDManagement].
//   - The node and its endpoints: [NewDescriptor], [NewBasicInformation],
//     [NewBridgedDeviceBasicInformation], [NewIdentify], [NewFixedLabel],
//     [NewUserLabel], [NewActions], [NewEcosystemInformation],
//     [NewPowerSourceConfiguration].
//   - Diagnostics: [NewGeneralDiagnostics], [NewGeneralDiagnosticsWith],
//     [NewSoftwareDiagnostics], [NewEthernetNetworkDiagnostics],
//     [NewWiFiNetworkDiagnostics], [NewDiagnosticLogs].
//   - Time and localisation: [NewTimeSynchronization],
//     [NewTimeSynchronizationWith], [NewLocalizationConfiguration],
//     [NewTimeFormatLocalization], [NewUnitLocalization].
//   - Scenes and messages: [NewScenesManagement] over a [ScenesState]
//     ([NewScenesState], [NewPersistedScenesState]), [NewMessages].
//   - Software update: [NewOTASoftwareUpdateRequestor] (0x002A, built by
//     node/ota.go) and [NewOTAProvider] (0x0029, built by
//     node/ota_provider.go for a node or a controller).
//
// PowerSource (0x002F) for bridged battery endpoints lives in the
// measurement package (measurement.PowerSourceServer), not here.
//
// Revisions and ids come from the generated definitions under
// cluster/spec, from the matter.js schema snapshot (parity/schema.json).
package core
