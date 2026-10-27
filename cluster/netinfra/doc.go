// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package netinfra holds the network-infrastructure servers of a Network
// Infrastructure Manager or a Thread Border Router: WiFiNetworkManagement
// (0x0451) and ThreadNetworkDirectory (0x0453).
//
// matter.js serves both as the generated behaviour stub
// (packages/node/src/behaviors/wi-fi-network-management/
// WiFiNetworkManagementServer.ts:14 and packages/node/src/behaviors/
// thread-network-directory/ThreadNetworkDirectoryServer.ts:14 at
// 862ad75f, `export class …Server extends …Behavior {}`), so the rules
// here follow connectedhomeip at the harness image's CHIP commit 6170af84:
// src/app/clusters/wifi-network-management-server/
// WiFiNetworkManagementCluster.cpp and src/app/clusters/
// thread-network-directory-server/ThreadNetworkDirectoryCluster.cpp, cited
// below by line. Both are entries of the "Ahead of matter.js" register
// (notes/parity/by_design.md).
//
// The host supplies the network's state: the Wi-Fi credentials through
// [WiFiManagementServer.SetNetworkCredentials], the Thread datasets
// through a [ThreadNetworkStore] it persists them in (an in-memory one by
// default). Neither server holds a radio.
package netinfra
