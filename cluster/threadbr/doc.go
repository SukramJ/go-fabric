// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package threadbr holds the servers of a Thread Border Router endpoint
// (device type 0x0091, ADR 0027): ThreadBorderRouterManagement (0x0452)
// and ThreadNetworkDiagnostics (0x0035), both over one [BorderRouter] — the
// existing OpenThread Border Router the host manages, which thread/otbr
// reaches over its REST API. The module joins no Thread mesh.
//
// matter.js serves both clusters as the generated behaviour stub
// (packages/node/src/behaviors/thread-border-router-management/
// ThreadBorderRouterManagementServer.ts:14 and packages/node/src/behaviors/
// thread-network-diagnostics/ThreadNetworkDiagnosticsServer.ts:14 at
// 862ad75f, `export class …Server extends …Behavior {}`), so the rules here
// follow connectedhomeip at the harness image's CHIP commit 6170af84:
// src/app/clusters/thread-border-router-management-server/
// ThreadBorderRouterManagementCluster.cpp over
// ThreadBorderRouterManagementDelegate.h, and src/app/clusters/
// thread-network-diagnostics-server/DirectThreadNetworkDiagnosticsProvider.cpp
// for what each diagnostics attribute reads, cited below by line.
// ThreadBorderRouterManagement is an entry of the "Ahead of matter.js"
// register (notes/parity/by_design.md, BD-Thread-TBRM-ChipLogic).
//
// The servers cache what they read from the border router: they read it
// at construction, after each command and at every [ManagementServer.Refresh]
// / [DiagnosticsServer.Refresh] the host schedules, where chip's delegate
// is read at every attribute read — a border router on the LAN is an HTTP
// round trip away, and the cache keeps the data versions aware of every
// change a refresh sees. A border router that stops answering is
// InterfaceEnabled false, not an error of the read.
//
// The Thread variant of NetworkCommissioning that reflects the same
// network is cluster/core's ThreadNetworkCommissioning over the same
// [BorderRouter].
package threadbr
