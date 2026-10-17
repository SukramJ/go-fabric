// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/cluster/wire"
)

// Observers are the host's callbacks on the node's fabric and
// commissioning events (issue #58). Each is optional, and each is chained
// after the facade's own handler for the same event: the facade's wiring
// runs first and unchanged, the observer runs once it has returned, and
// an observer's error (or panic) is logged, never returned to the
// handler — the event has already happened.
//
// The name follows matter.js, whose node events are Observables that
// observers attach to (packages/protocol/src/fabric/FabricManager.ts:
// 41-48 `#events`); an observer of an error-guarded event is logged and
// the remaining observers still run (FabricManager.ts:381-386
// `unhandled`, "Log unhandled errors but do not abort deletion").
//
// Observers run synchronously on the goroutine of the event — for the
// fabric events, inside the command that caused it and before its
// response is sent — so an observer that blocks delays the response. A
// host that fans an event out over the network hands it to its own
// goroutine.
type Observers struct {
	// OnFabricAdded runs after an AddNOC installed a fabric and the
	// facade rebuilt the fabric's CASE identity and announced its
	// operational record. Mirrors matter.js FabricManager `events.added`
	// (FabricManager.ts:209).
	OnFabricAdded func(FabricInfo) error
	// OnFabricRemoved runs after RemoveFabric (or the fail-safe rollback
	// of an AddNOC) deleted a fabric and the facade's cleanup ran: the
	// fabric, its ACL and its keys are gone from the store, its CASE
	// identity and operational record are withdrawn, its sessions are
	// told and closed (the invoking one after the response). When it was
	// the last fabric the node's own window reopens afterwards, once the
	// node is idle — no fail-safe armed and no session left (matter.js
	// #resetWhenIdle); OnWindowTransition reports that. Mirrors
	// matter.js FabricManager `events.deleted` (FabricManager.ts:253).
	OnFabricRemoved func(FabricInfo) error
	// OnFabricUpdated runs after an UpdateNOC (or the fail-safe rollback
	// of one) changed a fabric's operational identity and the facade
	// moved the CASE identity and the operational record and closed the
	// fabric's other sessions. Mirrors matter.js FabricManager
	// `events.replaced` (FabricManager.ts:236, :377).
	OnFabricUpdated func(FabricInfo) error
	// OnCommissioningComplete runs after a successful
	// CommissioningComplete and the facade's own handling of it (pending
	// OperationalCredentials state cleared, the OTA requestor's query
	// scheduled), with the fabric the commissioning completed for. It
	// fires on every CommissioningComplete, whereas matter.js's
	// `commissioned` event fires when the fabric count crosses zero
	// (CommissioningServer.ts:143-157 #syncCommissioned).
	OnCommissioningComplete func(fabricIndex uint8) error
	// OnWindowTransition runs on every commissioning-window transition
	// (open, close, an administrator's AdminFabricIndex cleared), after
	// the facade switched the PASE acceptor and published or withdrew
	// the commissionable record.
	OnWindowTransition func(WindowSnapshot) error
}

// FabricInfo names one fabric, as OperationalCredentials' AddNOC hook
// carries it (cluster/core OpcredsConfig.OnFabricInstalled: fabric index,
// fabric id, node id, root public key) plus the compressed fabric id the
// operational record is named by.
type FabricInfo struct {
	FabricIndex        uint8
	FabricID           uint64
	NodeID             uint64
	RootPublicKey      []byte
	CompressedFabricID [8]byte
}

// WindowSnapshot is the commissioning window after a transition.
type WindowSnapshot struct {
	// Open reports whether a window is open, whoever opened it.
	Open bool
	// Own reports whether it is the node's own window — the one an
	// uncommissioned node opens, [Node.OpenBasicWindow]'s, or one the
	// host opened through CommissioningWindow().OpenOwnWindow — rather
	// than an administrator's.
	Own bool
	// Administrator is the AdministratorCommissioning view of the
	// window: WindowStatus, AdminFabricIndex, AdminVendorId. The node's
	// own window reads WindowNotOpen there, as matter.js keeps those
	// attributes for administrator windows only.
	Administrator wire.WindowStatusSnapshot
	// CommissioningMode is the CM value of the commissionable record the
	// node publishes after the transition: 0 none, 1 a basic window, 2
	// an enhanced window with an administrator's verifier.
	CommissioningMode uint8
	// Discriminator is the published record's discriminator; 0 when
	// CommissioningMode is 0.
	Discriminator uint16
}

// observe runs one observer after the facade's handler: a nil observer is
// skipped, an error or a panic is logged under event.
func observe(logger *slog.Logger, event string, fn func() error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("node.observer.panic", slog.String("event", event), slog.String("panic", fmt.Sprint(r)))
		}
	}()
	if err := fn(); err != nil {
		logger.Warn("node.observer.failed", slog.String("event", event), slog.String("err", err.Error()))
	}
}

// fabricInfo reads a fabric's identity from the store; ok is false when
// the fabric is not (or no longer) there.
func (n *Node) fabricInfo(ctx context.Context, fabricIndex uint8) (FabricInfo, bool) {
	rec, err := n.st.GetFabric(ctx, fabricIndex)
	if err != nil {
		return FabricInfo{FabricIndex: fabricIndex}, false
	}
	return FabricInfo{
		FabricIndex:        fabricIndex,
		FabricID:           rec.FabricID,
		NodeID:             rec.NodeID,
		RootPublicKey:      append([]byte(nil), rec.RootPublicKey...),
		CompressedFabricID: rec.CompressedID,
	}, true
}
