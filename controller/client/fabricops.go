// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync/atomic"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/controller/commissioner"
)

// Fabric operations: commission and decommission through the registry.

// Peer-loss causes of the registry — matter.js PeerLeftError and
// PeerShutdownError (protocol/src/peer/PeerCommunicationError.ts:90-94,
// :72-76 at db021da0), which client/peer and client/exchange do
// not declare. The cause an operational certificate update force-closes
// the peer's sessions with, matter.js FabricChangedError, is
// commissioner.ErrFabricChanged: the commissioner runs the update and
// cannot import this package.
var (
	// ErrPeerLeft: the peer left the fabric — the cause its sessions are
	// given up with after a decommission or a leave event.
	ErrPeerLeft = errors.New("client: peer has left the fabric")
	// ErrPeerShutdown: the peer reported a shutdown. It is transient
	// (errors.Is against exchange.ErrTransient), as matter.js's class
	// extends TransientPeerCommunicationError.
	ErrPeerShutdown error = peerShutdownError{}
	// ErrFabricRemoved: this node is no longer a member of the session's
	// fabric — matter.js FabricRemovedError
	// (protocol/src/peer/PeerCommunicationError.ts:96-103 at db021da0), the
	// cause Fabric.delete force-closes the fabric's sessions with
	// (protocol/src/fabric/Fabric.ts:484-488). A host that drops its fabric
	// closes the peers' sessions with it (peer.Peer.Disconnect); a
	// RemoveFabric that fails with it counts as delivered.
	ErrFabricRemoved = errors.New("client: this node is no longer a member of the fabric")
)

type peerShutdownError struct{}

func (peerShutdownError) Error() string { return "client: peer reports shutdown" }

// Is makes the error match exchange.ErrTransient.
func (peerShutdownError) Is(target error) bool { return target == exchange.ErrTransient }

// Operation kinds — matter.js FabricOperation
// (packages/node/src/node/client/Peers.ts:80 at b25ed960).
const (
	opCommission        = "commission"
	opDecommission      = "decommission"
	opCertificateUpdate = "certificate update"
)

// fabricOperationVerbs are the verbs of the in-progress refusal — matter.js
// FABRIC_OPERATION_VERBS (Peers.ts:82-86 at b25ed960).
var fabricOperationVerbs = map[string]string{
	opCommission:        "commission",
	opDecommission:      "decommission",
	opCertificateUpdate: "update the operational certificate of",
}

// fabricOp is a commission or decommission in flight — an entry of
// Peers.#fabricOperations (Peers.ts:93).
type fabricOp struct {
	kind string
	done chan struct{}
}

// runFabricOperation runs fn as the node's one fabric operation. Mirrors
// matter.js packages/node/src/node/client/Peers.ts:638-656 at b25ed960
// (Peers.#runFabricOperation): registered under the mutex, refused for a
// node that is gone (:641) or has one running (:642-647); fn runs outside
// the mutex (:651); the entry is removed and its waiters released however
// fn ends (:652-655).
func (r *Registry) runFabricOperation(n *Node, kind string, fn func() error) error {
	op, err := r.registerOperation(n, kind)
	if err != nil {
		return err
	}
	defer func() {
		r.mu.Lock()
		delete(r.ops, n)
		r.mu.Unlock()
		close(op.done)
	}()
	return fn()
}

func (r *Registry) registerOperation(n *Node, kind string) (*fabricOp, error) {
	r.op.Lock()
	defer r.op.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || n.isClosing() || !slices.Contains(r.nodes, n) {
		return nil, ErrClosed
	}
	// A node that failed to load is refused with its load error before
	// anything runs, so no operation erases its stored data as a side
	// effect of failing; Forget is the explicit way to drop it. Mirrors
	// matter.js Peers.ts:641 at b25ed960 (assertNotGone, one gate for every
	// fabric operation, :607-632), which throws CrashedDependencyError with
	// the construction error as its cause for a crashed node
	// (packages/node/src/endpoint/properties/EndpointLifecycle.ts:146-175).
	if err := n.LoadError(); err != nil {
		return nil, err
	}
	if running := r.ops[n]; running != nil {
		return nil, fmt.Errorf("%w: cannot %s %s because a %s attempt is already in progress", ErrFabricOperationInProgress, fabricOperationVerbs[kind], n.ID(), running.kind)
	}
	op := &fabricOp{kind: kind, done: make(chan struct{})}
	r.ops[n] = op
	return op, nil
}

// operationOf is the node's fabric operation in flight, nil when none.
func (r *Registry) operationOf(n *Node) *fabricOp {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ops[n]
}

func (n *Node) isClosing() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.closing
}

// CommissionOptions configure [Registry.Commission] — matter.js
// CommissioningClient.CommissioningOptions as far as the Go commissioner
// takes them.
type CommissionOptions struct {
	// Options are the commissioner's. Fabric nil is the registry's first
	// fabric; Addresses nil are the node's known addresses; DiscoveryData
	// nil is the node's record; CASEConnectionTiming nil is the tighter
	// timing matter.js applies after a commissioning
	// (defaultCaseConnectionTiming, CommissioningClient.ts:798-811 at
	// db021da0).
	commissioner.Options
	// AutoSubscribe nil or true subscribes the node — "unless disabled
	// explicitly" (CommissioningClient.ts:310-311).
	AutoSubscribe *bool
	// DefaultSubscription narrows the default subscription; nil is the
	// wildcard (:309).
	DefaultSubscription *SubscribeOptions
	// NoInitialRead skips the initial read of the newly commissioned node
	// (autoStateInitialize false, :312).
	NoInitialRead bool
	// OnboardingCode, when set, is the QR or manual pairing code the
	// passcode came from; it is kept with the node when the commissioning
	// succeeds ([Node.OnboardingCode], source [OnboardingPaired]) and the
	// device advertised Basic commissioning mode — not for an Enhanced
	// Commissioning Window, whose code ends with the window, and not
	// without an advertisement. It is kept before the node starts, so
	// also when Commission then returns the start's error. A plain
	// string, as the passcode in Options is: it is the caller's input,
	// and the stored value is a [SecretCode]. This module's; matter.js
	// keeps no pairing code.
	OnboardingCode string
}

// defaultCASEConnectionTiming is the timing of the operational CASE
// connection after commissioning unless the caller supplies one —
// CommissioningClient.ts:775-779.
var defaultCASEConnectionTiming = Timing{
	DelayBeforeNextAddress:               15 * time.Second,
	MaxDelayBetweenInitialContactRetries: 60 * time.Second,
	DelayAfterPeerError:                  2 * time.Minute,
}

// Commission commissions the node — ClientNode.commission
// (node/ClientNode.ts:162-166) over CommissioningClient.commission
// (CommissioningClient.ts:170-330), as the node's fabric operation: a node
// with a peer address is [ErrAlreadyCommissioned] (:173-175); the fabric
// is the option's, else the first (:197-218), one of ours (:214-218); a
// node without an address is [ErrNotLocated] (:220-223); the commissioner
// commissions it over the addresses and the record's discovery data
// (:252-269, :290); the record takes the peer address, the commissioning
// time and our fabric index on the peer (:291-293) and the peer's
// descriptor (:296); the node is bound to its peer; the settings take the
// default subscription and AutoSubscribe (:308-314) and the node is marked
// newly commissioned (:314); record and settings are saved (:316) and the
// node started (:327) — an error of its initial read is returned and the
// node stays commissioned. A commissioner error leaves the node as it was.
//
// The node's start is reported through o.Progress as the step
// commissioner.StepSubscribe 21.1 — started, then succeeded or failed with
// the start's error. It is never skipped: every commission that succeeds
// starts the node, and one that fails earlier reports nothing after its
// failed step, as matter.js reports nothing after a failure
// (ControllerCommissioningFlow.ts:437-465 at db021da0).
func (r *Registry) Commission(ctx context.Context, n *Node, o CommissionOptions) error {
	return r.runFabricOperation(n, opCommission, func() error { return r.commission(ctx, n, o) })
}

func (r *Registry) commission(ctx context.Context, n *Node, o CommissionOptions) error {
	if n.IsCommissioned() {
		return fmt.Errorf("%w: %s", ErrAlreadyCommissioned, n.ID())
	}
	opts := o.Options
	if opts.Fabric == nil {
		opts.Fabric = r.cfg.Fabrics[0]
	} else if !slices.Contains(r.cfg.Fabrics, opts.Fabric) {
		return fmt.Errorf("%w: cannot commission %s into fabric %d because we do not control it", ErrForeignFabric, n.ID(), opts.Fabric.FabricIndex)
	}
	rec := n.Record()
	if opts.Addresses == nil {
		opts.Addresses = udpAddrs(rec.Addresses)
	}
	// A node found over BLE is located by its commissioning channel. In
	// matter.js its addresses hold the peripheral as a NetworkAddress of
	// type "ble" (CommissioningClient.ts:910-945 at db021da0), which passes
	// the check above (:254-257); a Record's NetworkAddress is IP only, so the
	// channel stands for it.
	if len(opts.Addresses) == 0 && opts.Channel == nil {
		return fmt.Errorf("%w: cannot commission %s", ErrNotLocated, n.ID())
	}
	if opts.DiscoveryData == nil {
		opts.DiscoveryData = rec.device()
	}
	if opts.CASEConnectionTiming == nil {
		t := defaultCASEConnectionTiming
		opts.CASEConnectionTiming = &t
	}
	res, err := r.cfg.Commissioner.Commission(ctx, opts)
	if err != nil {
		return err
	}
	// The device carries our fabric now: the record takes its address
	// before the peer is looked up, so a lookup that fails leaves a node
	// that can still be reached and decommissioned. Mirrors matter.js
	// CommissioningClient.commission, which sets peerAddress and
	// commissionedAt (packages/node/src/behavior/system/commissioning/
	// CommissioningClient.ts:324-326 at db021da0) before PeerSet.for
	// (:330) (review II, R2-P6-err-2318).
	now := r.clock.Now()
	n.updateRecord(func(rec *Record) {
		a := res.Address
		rec.PeerAddress = &a
		rec.CommissionedAt = now
		rec.FabricIndexOnPeer = res.FabricIndexOnPeer
		rec.AttestedVendorID, rec.AttestedProductID = res.AttestedVendorID, res.AttestedProductID
	})
	p := r.cfg.Peers.Get(res.Address)
	if p == nil {
		//nolint:contextcheck // the peer's service resolves under the DNS-SD cache's context
		if p, err = r.cfg.Peers.AddKnownPeer(peer.Descriptor{Address: res.Address}); err != nil {
			return err
		}
	}
	d := p.Descriptor()
	n.updateRecord(func(rec *Record) { rec.applyPeer(d) })
	r.bindPeer(n, p)
	n.mu.Lock()
	n.settings.defaultSubscription = o.DefaultSubscription.clone()
	n.settings.autoSubscribe = o.AutoSubscribe == nil || *o.AutoSubscribe
	n.cfg.NoInitialRead = o.NoInitialRead
	n.newlyCommissioned = true
	n.saveSettingsLocked()
	n.mu.Unlock()
	if o.OnboardingCode != "" {
		n.keepPairedCode(o.OnboardingCode, rec.CommissioningMode)
	}
	r.log.Info("matter.controller.client.registry.commissioned", slog.String("node", n.ID()), slog.String("peer", peerAddressString(res.Address)))
	if h := n.cfg.Hooks.Commissioned; h != nil {
		h()
	}
	commissioner.ReportSubscribe(o.Progress, commissioner.StepStarted, nil)
	if err := n.Start(ctx); err != nil {
		commissioner.ReportSubscribe(o.Progress, commissioner.StepFailed, err)
		return err
	}
	commissioner.ReportSubscribe(o.Progress, commissioner.StepSucceeded, nil)
	return nil
}

// Decommission removes the node from its fabric and deletes it — matter.js
// ClientNode.decommission (packages/node/src/node/ClientNode.ts:200-210 at
// b25ed960) as the node's fabric operation: a commissioned node is
// decommissioned first (CommissioningClient.decommission,
// CommissioningClient.ts:340-446), then the node is deleted (:208). A
// removal the peer refused, or one that was not confirmed, returns its
// error and keeps the node. A node that failed to load is refused with its
// load error and nothing is erased (:175-178, assertNotGone); [Registry.Forget]
// drops it.
func (r *Registry) Decommission(ctx context.Context, n *Node) error {
	return r.runFabricOperation(n, opDecommission, func() error {
		var former *PeerAddress
		if n.IsCommissioned() {
			addr, err := r.decommission(ctx, n)
			if err != nil {
				return err
			}
			former = &addr
		}
		return r.deleteNode(ctx, n, former)
	})
}

// CertificateUpdateOptions configure [Registry.UpdateOperationalCertificate]
// — matter.js CommissioningClient.OperationalCertificateUpdateOptions
// (packages/node/src/behavior/system/commissioning/CommissioningClient.ts:931-941
// at b25ed960); the abort is the call's context.
type CertificateUpdateOptions struct {
	// Failsafe is the fail-safe armed for the whole update, including the
	// CASE reconnect with the new certificate: 0 is one minute
	// ([commissioner.DefaultCertificateUpdateFailsafe]); it must lie
	// between 30 s and 65535 s. The node ends the fail-safe early at its
	// MaxCumulativeFailsafeSeconds.
	Failsafe time.Duration
}

// UpdateOperationalCertificate replaces the node operational certificate
// of the commissioned node with one for a freshly generated key pair, as
// the node's fabric operation: its node id, fabric, CASE Authenticated
// Tags and fabric-scoped data stay. Mirrors matter.js ClientNode
// .updateOperationalCertificate (packages/node/src/node/ClientNode.ts:218-224
// at b25ed960) under Peers.runOperationalCertificateUpdate
// (Peers.ts:630-632), over CommissioningClient.updateOperationalCertificate
// (CommissioningClient.ts:519-570), which hands the update to
// [commissioner.Commissioner.UpdateOperationalCertificate] (the sequence,
// the failure table and what ctx ends are documented there).
//
// A node without a peer address is [ErrNotCommissioned] (:520-525). The
// update reads the node's CurrentFabricIndex first; once that read
// succeeded, the read index replaces [Record].FabricIndexOnPeer — with a
// warning when another one was stored (:562-570) — also when the update
// fails afterwards: a [*commissioner.CertificateUpdateError] that carries
// an index stores it and is returned unchanged; a failing record save is
// logged and does not replace the update's error (:533-537, :544-555).
// While the update runs, a commission, decommission or second update of
// the node is [ErrFabricOperationInProgress].
func (r *Registry) UpdateOperationalCertificate(ctx context.Context, n *Node, o CertificateUpdateOptions) error {
	return r.runFabricOperation(n, opCertificateUpdate, func() error {
		addr, ok := n.commissionedAddress()
		if !ok {
			return fmt.Errorf("%w: cannot update the operational certificate of %s because it is not commissioned", ErrNotCommissioned, n.ID())
		}
		var fabric *commissioner.Fabric
		for _, f := range r.cfg.Fabrics {
			if f.FabricIndex == addr.FabricIndex {
				fabric = f
				break
			}
		}
		if fabric == nil {
			return fmt.Errorf("%w: cannot update the operational certificate of %s in fabric %d because we do not control it", ErrForeignFabric, n.ID(), addr.FabricIndex)
		}
		res, err := r.cfg.Commissioner.UpdateOperationalCertificate(ctx, commissioner.CertificateUpdateOptions{Address: addr, Fabric: fabric, Failsafe: o.Failsafe})
		if err != nil {
			var ue *commissioner.CertificateUpdateError
			if errors.As(err, &ue) && ue.FabricIndexOnPeer != 0 {
				r.storeReportedFabricIndex(n, ue.FabricIndexOnPeer)
			}
			return err
		}
		r.storeReportedFabricIndex(n, res.FabricIndexOnPeer)
		return nil
	})
}

// storeReportedFabricIndex makes fabricIndex the node's FabricIndexOnPeer,
// with a warning when another one was stored; a save failure is logged by
// the record's save. Mirrors matter.js CommissioningClient
// .#storeReportedFabricIndex (CommissioningClient.ts:557-571 at b25ed960).
func (r *Registry) storeReportedFabricIndex(n *Node, fabricIndex uint8) {
	var known uint8
	n.updateRecord(func(rec *Record) {
		known = rec.FabricIndexOnPeer
		rec.FabricIndexOnPeer = fabricIndex
	})
	if known != 0 && known != fabricIndex {
		r.log.Warn("matter.controller.client.registry.fabric_index_replaced", slog.String("node", n.ID()),
			slog.Int("reported", int(fabricIndex)), slog.Int("stored", int(known)))
	}
}

// leaveLatch is the decommission's watch for the peer's leave event
// (CommissioningClient.ts:426-437 at b25ed960).
type leaveLatch struct {
	fabricIndex uint8
	seen        atomic.Bool
}

// noteLeave is the latch's listener: a leave of the fabric index being
// removed, while the subscription is active, confirms the removal
// (:430-436, the guard :433). matter.js's listener runs inside the
// report's awaited emit, so a Leave of the priming report finds the
// subscription inactive; live — whether it was active when the report
// arrived — gives the Go listener the same answer (onLeave).
func (r *Registry) noteLeave(n *Node, fabricIndex uint8, live bool) {
	r.mu.Lock()
	l := r.leaves[n]
	r.mu.Unlock()
	if l != nil && l.fabricIndex == fabricIndex && live && n.SubscriptionActive() {
		l.seen.Store(true)
	}
}

// decommission is CommissioningClient.decommission
// (CommissioningClient.ts:340-446): without an OperationalCredentials
// cluster in the image, or without our fabric index on the peer, the
// CurrentFabricIndex is read with the default subscription's fabric
// filter (:349-371); the leave latch is armed before RemoveFabric goes out
// (:377-391); a non-Ok status fails whatever the leave event says
// (:396-404, :418-420); an error is a confirmed removal when the leave was
// seen or the session was lost to the removal (:405-417); then the peer's
// sessions are given up (:422-427) and the record's commissioning fields
// cleared — not the addresses (:429-435). It returns the former address.
func (r *Registry) decommission(ctx context.Context, n *Node) (PeerAddress, error) {
	addr, ok := n.commissionedAddress()
	if !ok {
		return addr, ErrNotCommissioned
	}
	former := peerAddressString(addr)
	rec := n.Record()
	oc := n.opCreds()
	current := currentFabricIndex(oc)
	if oc == nil || (rec.FabricIndexOnPeer == 0 && current == 0) {
		// The read has the cache's fabric filter (CommissioningClient.ts:391-392
		// at db021da0).
		n.mu.RLock()
		noFilter := !n.s.cacheFabricFiltered()
		n.mu.RUnlock()
		rep, err := n.Read(ctx, ReadRequest{ReadRequest: ReadPaths{
			Attributes:     []AttributePath{{Endpoint: 0, Cluster: opCredsCluster, Attribute: opCredsAttrCurrentFabricIndex, HasEndpoint: true, HasCluster: true, HasAttribute: true}},
			NoFabricFilter: noFilter,
		}})
		if err != nil {
			return addr, err
		}
		if v, ok := rep.Value(0, opCredsCluster, opCredsAttrCurrentFabricIndex); ok {
			current, _ = decodeFabricIndex(v)
		}
	}
	fabricIndex := rec.FabricIndexOnPeer
	if fabricIndex == 0 {
		fabricIndex = current
	}
	if fabricIndex == 0 {
		return addr, fmt.Errorf("client: removing node %s: the peer's fabric index is unknown", former)
	}
	latch := &leaveLatch{fabricIndex: fabricIndex}
	r.mu.Lock()
	r.leaves[n] = latch
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.leaves, n)
		r.mu.Unlock()
	}()
	r.log.Debug("matter.controller.client.registry.removing_fabric", slog.String("node", n.ID()), slog.String("peer", former), slog.Int("fabric_index", int(fabricIndex)))
	invoke := func(fabricIndex uint8) (Result, error) {
		return n.Invoke(ctx, opCredsCommand(opCredsCmdRemoveFabric), removeFabricRequest(fabricIndex), InvokeOptions{})
	}
	r.mu.Lock()
	patch := r.removeFabric
	r.mu.Unlock()
	var res Result
	var err error
	if patch != nil {
		res, err = patch(fabricIndex, invoke)
	} else {
		res, err = invoke(fabricIndex)
	}
	if err == nil {
		nr, derr := nocResponseOf(res)
		if derr != nil {
			return addr, derr
		}
		if nr.status != nocStatusOk {
			var text string
			if nr.debugText != nil {
				text = *nr.debugText
			}
			return addr, fmt.Errorf("client: removing node %s failed with status %d %q", former, nr.status, text)
		}
	} else {
		seen := latch.seen.Load()
		if !seen && !removalConfirmedBy(err) {
			return addr, err
		}
		reason := "session closed"
		if seen {
			reason = "leave event"
		}
		r.log.Info("matter.controller.client.registry.removal_confirmed_without_response", slog.String("node", n.ID()), slog.String("peer", former), slog.String("by", reason))
	}
	if p := n.peer(); p != nil {
		p.Disconnect(ctx, ErrPeerLeft)
	}
	n.updateRecord(func(rec *Record) {
		rec.PeerAddress = nil
		rec.CommissionedAt = time.Time{}
		rec.FabricIndexOnPeer = 0
		rec.AttestedVendorID, rec.AttestedProductID = nil, nil
		rec.ReportedSessionParameters = nil
		rec.TCPUnsupported = false
	})
	r.log.Info("matter.controller.client.registry.decommissioned", slog.String("node", n.ID()), slog.String("formerly", former))
	// The cleared peer address is lifecycle.decommissioned
	// (CommissioningClient.ts:637-646).
	if n.icd != nil {
		n.icd.onDecommissioned()
	}
	if h := n.cfg.Hooks.Decommissioned; h != nil {
		h()
	}
	return addr, nil
}

// removalConfirmedBy reports whether an error of RemoveFabric means the
// command was delivered: matter.js causedBy(PeerMessageMissingError,
// PeerLeftError, FabricRemovedError, PeerInitiatedCloseError)
// (CommissioningClient.ts:437-440 at db021da0). PeerMessageMissingError and
// PeerInitiatedCloseError are client/exchange's
// [*exchange.PeerMessageMissingError] and [*exchange.PeerInitiatedCloseError];
// PeerLeftError is [ErrPeerLeft]; FabricRemovedError is [ErrFabricRemoved].
func removalConfirmedBy(err error) bool {
	var missing *exchange.PeerMessageMissingError
	var closed *exchange.PeerInitiatedCloseError
	return errors.As(err, &missing) || errors.As(err, &closed) || errors.Is(err, ErrPeerLeft) || errors.Is(err, ErrFabricRemoved)
}

// udpAddrs is a list of addresses as the commissioner takes them.
func udpAddrs(as []NetworkAddress) []*net.UDPAddr {
	out := make([]*net.UDPAddr, 0, len(as))
	for _, a := range as {
		out = append(out, a.UDPAddr())
	}
	return out
}
