// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
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
	// fabric's other sessions, and after an UpdateFabricLabel or a
	// SetVIDVerificationStatement changed a fabric under the same
	// identity (nothing for the facade to move). Mirrors matter.js
	// FabricManager `events.replaced` (FabricManager.ts:236-246, :377),
	// which a fabric's persist emits too, and CommissioningServer's
	// fabricsChanged(index, "updated") (CommissioningServer.ts:118-141).
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
	// OnSurfaceChanged runs after an assembly changed an endpoint's served
	// surface against the one persisted for its key — once the node has
	// raised the ConfigurationVersions, bumped the DataVersions, stored
	// the new surface and recorded the change ([Node.Surface] lists it
	// under Changed) — once per changed endpoint, with the change. The
	// announcement is this module's (concept v11 §D, by_design
	// BD-Matter-SurfaceChange-ConfigurationVersion); matter.js raises the
	// versions and has no event for it, so a host that mirrors a change
	// to its own pages (a stream event) observes it here.
	OnSurfaceChanged func(surface.SurfaceChange) error
}

// FabricInfo names one fabric, as OperationalCredentials' AddNOC hook
// carries it (cluster/core OpcredsConfig.OnFabricInstalled: fabric index,
// fabric id, node id, root public key) plus the compressed fabric id the
// operational record is named by, and the vendor and label the fabric's
// administrator set — the fields of the store's record a host shows
// ([Node.FabricInfos]).
type FabricInfo struct {
	FabricIndex        uint8
	FabricID           uint64
	NodeID             uint64
	RootPublicKey      []byte
	CompressedFabricID [8]byte
	// VendorID is the AdminVendorID AddNOC carried for the fabric.
	VendorID uint16
	// Label is the fabric's label (UpdateFabricLabel); empty until an
	// administrator sets one.
	Label string
}

// fabricInfoOf is the FabricInfo of one stored fabric.
func fabricInfoOf(rec store.FabricRecord) FabricInfo {
	return FabricInfo{
		FabricIndex:        rec.FabricIndex,
		FabricID:           rec.FabricID,
		NodeID:             rec.NodeID,
		RootPublicKey:      append([]byte(nil), rec.RootPublicKey...),
		CompressedFabricID: rec.CompressedID,
		VendorID:           rec.VendorID,
		Label:              rec.Label,
	}
}

// WindowSnapshot is the commissioning window as the node serves it: what
// OnWindowTransition receives after a transition and [Node.Window] returns
// at any time. matter.js keeps the same state in DeviceCommissioner's
// window — its status, byAdministrator and its timer
// (packages/protocol/src/protocol/DeviceCommissioner.ts:344-350,
// :236-266 #openWindow at 862ad75f) — and the advertised discriminator
// (`discriminator ?? commissioningConfig.discriminator`, :252).
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
	Administrator WindowStatus
	// CommissioningMode is the CM value of the commissionable record the
	// node publishes after the transition: 0 none, 1 a basic window, 2
	// an enhanced window with an administrator's verifier.
	CommissioningMode uint8
	// Discriminator is the published record's discriminator; 0 when
	// CommissioningMode is 0.
	Discriminator uint16
	// Kind is which window is open; [WindowNone] while none is.
	Kind WindowKind
	// OpenedAt is when the window opened (or the node restarted its own
	// window), and Deadline when its timer closes it, both on the node's
	// clock ([Node.Clock]); zero while no window is open.
	OpenedAt, Deadline time.Time
	// Timeout is the duration the window was opened for; 0 while no
	// window is open.
	Timeout time.Duration
}

// WindowKind names the open commissioning window by who opened it with
// which credentials.
type WindowKind uint8

const (
	// WindowNone is no window: none is open.
	WindowNone WindowKind = iota
	// WindowOwn is the node's own window with the configured credentials
	// ([Config].Commissioning): the one an uncommissioned node opens, or
	// one the host opened through CommissioningWindow().OpenOwnWindow —
	// matter.js's window with byAdministrator false
	// (DeviceCommissioner.ts:236-266 at 862ad75f).
	WindowOwn
	// WindowBasic is the node's own window [Node.OpenBasicWindow] opened
	// with the host's credentials (a host extension; see there).
	WindowBasic
	// WindowEnhanced is an administrator's OpenCommissioningWindow with
	// its PAKE verifier — matter.js's EnhancedWindowOpen with
	// byAdministrator true.
	WindowEnhanced
)

// String names the kind.
func (k WindowKind) String() string {
	switch k {
	case WindowNone:
		return "none"
	case WindowOwn:
		return "own"
	case WindowBasic:
		return "basic"
	case WindowEnhanced:
		return "enhanced"
	default:
		return fmt.Sprintf("WindowKind(%d)", uint8(k))
	}
}

// Window returns the commissioning window as the node serves it now —
// the value OnWindowTransition receives, readable at any time.
func (n *Node) Window() WindowSnapshot {
	win := n.br.CommissioningWindow()
	if n.window == nil || win == nil {
		return WindowSnapshot{}
	}
	return n.window.snapshot(win)
}

// observerSet holds the observers [Node.Observe] registered, in
// registration order.
type observerSet struct {
	mu   sync.Mutex
	list []*Observers
}

// observerList is every observer of the node: [Config].Observers first,
// then the registered ones in registration order. A copy: an observer
// registered or unsubscribed while an event is dispatched takes effect
// from the next event.
func (n *Node) observerList() []Observers {
	n.observers.mu.Lock()
	defer n.observers.mu.Unlock()
	out := make([]Observers, 0, 1+len(n.observers.list))
	out = append(out, n.cfg.Observers)
	for _, o := range n.observers.list {
		out = append(out, *o)
	}
	return out
}

// Observe registers o's hooks on a running (or not yet started) node:
// each hook o sets runs after [Config].Observers' hook for the same event
// and after the hooks registered before it, under the same rules — on
// the event's goroutine, after the facade's own handling, an error or a
// panic logged and never returned. The returned unsubscribe removes them;
// it is idempotent, and an event already being dispatched may still reach
// them. A host that mounts a handler after [Node.Start] (a web UI) learns
// of commissionings and fabric changes this way.
//
// matter.js's node events are Observables a handler attaches to and
// detaches from at any time (packages/general/src/util/Observable.ts:55
// `on`, :70 `off` at 862ad75f — the FabricManager events of
// packages/protocol/src/fabric/FabricManager.ts:41-48 among them);
// Config.Observers is the attach at construction, Observe the attach later.
func (n *Node) Observe(o Observers) (unsubscribe func()) {
	entry := &o
	n.observers.mu.Lock()
	n.observers.list = append(n.observers.list, entry)
	n.observers.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			n.observers.mu.Lock()
			n.observers.list = slices.DeleteFunc(n.observers.list, func(e *Observers) bool { return e == entry })
			n.observers.mu.Unlock()
		})
	}
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
		logger.Warn("node.observer.failed", slog.String("event", event), slog.Any("err", err))
	}
}

// fabricInfo reads a fabric's identity from the store; ok is false when
// the fabric is not (or no longer) there.
func (n *Node) fabricInfo(ctx context.Context, fabricIndex uint8) (FabricInfo, bool) {
	rec, err := n.st.GetFabric(ctx, fabricIndex)
	if err != nil {
		return FabricInfo{FabricIndex: fabricIndex}, false
	}
	info := fabricInfoOf(rec)
	info.FabricIndex = fabricIndex
	return info, true
}
