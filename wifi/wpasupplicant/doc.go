// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package wpasupplicant is a node's Wi-Fi driver on Linux: a
// contract.WiFiDriver ([Driver]) over wpa_supplicant's D-Bus interface
// (fi.w1.wpa_supplicant1 on the system bus), without cgo. Set it as
// node.Network.WiFi's Driver: the node then serves NetworkCommissioning
// with the Wi-Fi feature, and the network a commissioner hands it —
// typically over BLE (node.Network.GATT, ble/bluez) — is joined through
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
// A join keeps the network that was current until it has ended: a join
// that fails removes the network it added and selects the one it was on
// again, a join that succeeds removes the one it was on. A removal that
// fails is retried before the next join and at Close while wpa_supplicant
// still lists the network. An AddNetwork that ends without
// wpa_supplicant's answer (the join's bound or context ended in the call,
// or the bus daemon gave up) may still have been carried out: every
// network that appeared in the interface's Networks meanwhile — another
// client's included — is removed the same way, at once when
// wpa_supplicant answers the read that follows the call, else before the
// next join or at Close; until then the window stays open, and a Close
// whose read fails leaves the network behind. A join whose read of the
// interface's Networks before AddNetwork fails ends there: the credential
// is not sent, as nothing could tell the network it added apart.
//
// What it does not do, on purpose: it saves nothing into wpa_supplicant's
// configuration (the node's store holds the committed network and the
// cluster rejoins it, so the credential lives in one place); it runs no DHCP
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
