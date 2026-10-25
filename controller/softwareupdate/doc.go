// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package softwareupdate is the controller's OTA push: matter.js's
// SoftwareUpdateManager and OtaAnnouncements
// (packages/node/src/behavior/system/software-update/ at 862ad75f) and the
// storage half of its DclOtaUpdateService
// (packages/protocol/src/dcl/DclOtaUpdateService.ts).
//
// A [Manager] keeps the update queue of the controller's commissioned
// nodes: it checks the ledgers and the stored images ([Images]) for
// updates, asks the host for consent ([Config].OnUpdateAvailable) or
// queues an update it holds a consent for ([Manager.AddUpdateConsent],
// [Manager.ForceUpdate]), announces the provider to one node at a time
// (AnnounceOTAProvider, reason UpdateAvailable), follows the update's
// status through the provider, and detects the outcome — the new version,
// a reboot into the old one, a resubscription after a restart that did not
// apply — arming the reboot re-subscription of client/peer
// (RebootResubscribeArmer) while the node applies. With
// [Config].AnnounceAsDefaultProvider it also writes itself as each node's
// default provider once a day.
//
// The provider itself is node/'s: matter.js serves it from the
// controller's own ServerNode, and the controller of this module mounts
// the same server (cluster/core.OTAProvider) on a node of its own on the
// fabric, the manager as its image source, consent and apply policy and
// the observer of its statuses (controller.Config.OTA). There is no second
// provider implementation.
package softwareupdate
