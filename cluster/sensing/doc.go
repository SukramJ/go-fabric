// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package sensing contains the server of the AmbientContextSensing cluster
// (0x0431, milestone 6, concept v10 §C wave 26-C): a sensor that reports
// detected human activity, identified objects and sounds, an object count
// against a threshold, and predicted activity.
//
// [AmbientContext] is built on the generated definition
// (cluster/spec/ambientcontextsensing, ADR 0013) and the generated server
// ([spec.Server]): ids, the feature-dependent element lists, FeatureMap,
// ClusterRevision, privileges, the codecs and the write checks come from
// there. matter.js serves the cluster with the generated behaviour only
// (packages/node/src/behaviors/ambient-context-sensing/
// AmbientContextSensingServer.ts:14 at 862ad75f is the empty subclass), so
// the rules are connectedhomeip's at the harness commit 6170af84
// (src/app/clusters/ambient-context-sensing-server/
// AmbientContextSensingCluster.cpp), cited by line; the Ahead-of-matter.js
// register in notes/parity/by_design.md lists it.
//
// chip's delegate (AmbientContextSensingDelegate.h:78-106) holds buffers
// and a wall clock. Here they are the server's own state: the supported
// list, the predicted activity and the detections it keeps, the hold-time
// expiry on the module's clock ([clock.Clock]) and the event epoch from
// the same clock. No delegate method stays a port: the host's inputs — a
// detection, an object count, the supported list, the predicted activity —
// are the server's [AmbientContext.AddDetection] and
// [AmbientContext.SetObjectCount] and its SetAttributes (the typed Set).
//
// The schema is matter.js's: chip's SensorFusion feature and its
// SensorFusionSupported attribute are not in the matter.js model at the
// pin (packages/model/src/standard/elements/
// ambient-context-sensing.element.ts:22-29 lists HA, OC, OI, AUD and
// PRED), so this server has neither.
//
// Persistence: chip persists HoldTime and the ObjectCountConfig threshold
// (AmbientContextSensingCluster.cpp:59-89, :357-364, :427-433). As for the
// camera servers (cluster/camera) and BooleanStateConfiguration
// (cluster/boolcfg), the host persists them here: every change is
// reported through the server's change notification (the typed
// OnChanged), and [AmbientContextConfig] (the declaration's Initial)
// restores them.
package sensing
