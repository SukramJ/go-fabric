// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package jointfabric serves the two clusters of a Joint Fabric
// administrator (device type 0x0130): JointFabricDatastore (0x0752), the
// fabric's registry of nodes, administrators, groups, key sets,
// endpoints, bindings and ACLs that the administrator keeps in step with
// the nodes, and JointFabricAdministrator (0x0753), the administrator's
// joint commissioning window, its ICAC and its trust verification of a
// commissioner that announces itself (the JCMCommissionee).
//
// matter.js has no logic for either at its pin 862ad75f
// (JointFabricDatastoreServer.ts:14 and JointFabricAdministratorServer.ts:14
// are the empty subclasses); the rules are connectedhomeip's at 6170af84:
// src/app/server/JointFabricDatastore.cpp and
// src/app/clusters/joint-fabric-datastore-server, and
// src/app/clusters/joint-fabric-administrator-server with JCMCommissionee.
// The shared trust verification is client/jcm.
//
// The host's admin application stays host-side behind the ports of the
// generated definitions: [DatastoreSync] reaches the member nodes,
// [DatastoreStore] persists the tables, [AdministratorPort] holds the
// administrator key, [CommissionerReach] reads the announcing
// commissioner. The node hands the Administrator its fabric table, its
// commissioning window and its fail-safe as the topology is assembled.
package jointfabric
