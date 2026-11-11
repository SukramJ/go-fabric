// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package wpasupplicant is a node's Wi-Fi driver on Linux: a
// contract.WiFiDriver ([Driver]) over wpa_supplicant's D-Bus interface
// (fi.w1.wpa_supplicant1 on the system bus), without cgo. Set it as
// node.Network.WiFi's Driver: the node then serves NetworkCommissioning
// with the Wi-Fi feature, and the network a commissioner hands it —
// typically over BLE (node.Network.BLE, ble/bluez) — is joined through
// it. It is a module of its own, so the main module keeps its
// dependencies and a wired host never pulls godbus.
//
// matter.js ships no Wi-Fi driver (its NetworkCommissioningServer leaves
// the radio to the platform), so the driver follows connectedhomeip's
// Linux platform (src/platform/Linux/ConnectivityManagerImpl_NetworkManagementWpaSupplicant.cpp
// and WpaSupplicantClient.cpp at 6170af84) call for call: the bring-up,
// the active scan and its result mapping, the join and its verdict from
// the interface's State. The network list, its rules, the fail-safe
// revert and the store stay in the cluster (cluster/core).
//
// What it does not do, on purpose: it saves nothing into wpa_supplicant's
// configuration (the node's store holds the committed network and rejoins
// it at start, so the credential lives in one place); it runs no DHCP
// client (addressing is the host's network stack's; Matter needs the IPv6
// link-local address the kernel configures); no per-device credentials
// (PDC), no access-point mode, no Wi-Fi PAF.
//
// The Wi-Fi credential is never logged and never part of a debug text or
// an error: it travels in AddNetwork alone.
//
// The host runs wpa_supplicant with its D-Bus interface (-u), keeps the
// interface out of NetworkManager's hands, and grants the process the
// system bus's permission for fi.w1.wpa_supplicant1.
package wpasupplicant
