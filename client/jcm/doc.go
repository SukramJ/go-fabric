// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package jcm is the trust verification of Joint Commissioning (JCM), the
// part both roles share: the stage machine a commissioner runs before it
// commissions a Joint Fabric administrator, and the one that
// administrator runs when a commissioner announces itself; the
// verification information each collects; and the Vendor ID
// Verification procedure each performs on the other's administrator
// fabric. It is the port of connectedhomeip's src/credentials/jcm
// (TrustVerification.{h,cpp}, VendorIdVerificationClient.{h,cpp}) at
// 6170af84; matter.js has no Joint Fabric logic at its pin 862ad75f
// (JointFabricAdministratorServer.ts:14 is the empty subclass).
//
// The roles bring their own stages: controller/commissioner (the JCM
// step of the commissioning flow, chip's src/controller/jcm) and
// cluster/jointfabric (the administrator's JCMCommissionee, chip's
// joint-fabric-administrator-server/JCMCommissionee.cpp).
//
// chip's stage functions answer kAsync and finish later through a
// callback guarded by a liveness token (VendorIdVerificationClient.h:79-90,
// :102); here a stage that waits on the network runs under a context that
// its owner cancels, so a late answer has nothing left to finish.
package jcm
