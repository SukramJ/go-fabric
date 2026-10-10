// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package energy holds the servers of the Matter energy-management
// clusters a bridged appliance offers: WaterHeaterManagement (0x0094) for
// the WaterHeater device type, and EnergyPreference (0x009B), optional on
// the Thermostat device type.
//
// Both are built on a generated cluster definition (ADR 0013;
// cluster/spec/waterheatermanagement, cluster/spec/energypreference) and
// the generated server ([spec.Server]): ids, enums, structs and their
// codecs, the feature-dependent element lists, FeatureMap,
// ClusterRevision, the privileges and the write checks come from the
// definition. For both, matter.js writes no logic of its own — its
// WaterHeaterManagementServer and EnergyPreferenceServer
// (packages/node/src/behaviors/<name>/*Server.ts) are empty subclasses of
// the generated behavior; so is its MeterIdentificationServer, for the
// third server here ([NewMeterIdentification], for the
// ElectricalUtilityMeter device type). The few rules left here are
// connectedhomeip's,
// read at the harness pin (the Makefile's CHIP_TEST_IMAGE_COMMIT,
// 6170af8461b10b1766044122ac83332c6d00ab20), each cited where it is
// applied.
package energy
