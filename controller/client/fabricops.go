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

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// Fabric operations: commission and decommission through the registry.

// Peer-loss causes of the registry — matter.js PeerLeftError and
// PeerShutdownError (protocol/src/peer/PeerCommunicationError.ts:80-84,
// :62-66 at 0ceb202d), which client/peer and client/exchange do
// not declare.
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
	// (protocol/src/peer/PeerCommunicationError.ts:86-93 at 0ceb202d), the
	// cause Fabric.delete force-closes the fabric's sessions with
	// (protocol/src/fabric/Fabric.ts:461-465). A host that drops its fabric
	// closes the peers' sessions with it (peer.Peer.Disconnect); a
	// RemoveFabric that fails with it counts as delivered.
	ErrFabricRemoved = errors.New("client: this node is no longer a member of the fabric")
)

type peerShutdownError struct{}

func (peerShutdownError) Error() string { return "client: peer reports shutdown" }

// Is makes the error match exchange.ErrTransient.
func (peerShutdownError) Is(target error) bool { return target == exchange.ErrTransient }

// Operation kinds — matter.js FabricOperation (Peers.ts:76).
const (
	opCommission   = "commission"
	opDecommission = "decommission"
)

// fabricOp is a commission or decommission in flight — an entry of
// Peers.#fabricOperations (Peers.ts:93).
type fabricOp struct {
	kind string
	done chan struct{}
}

// runFabricOperation runs fn as the node's one fabric operation —
// Peers.#runFabricOperation (Peers.ts:592-610): registered under the
// mutex, refused for a node that is gone (:595) or has one running
// (:596-601); fn runs outside the mutex; the entry is removed and its
// waiters released however fn ends (:606-609).
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
	if running := r.ops[n]; running != nil {
		return nil, fmt.Errorf("%w: cannot %s %s because a %s attempt is already in progress", ErrFabricOperationInProgress, kind, n.ID(), running.kind)
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
	// (CommissioningClient.ts:775-779).
	commissioner.Options
	// CaseAuthenticatedTags are our CATs for CASE sessions to the node
	// (CommissioningClient.ts:304-306); nil keeps the peer's.
	CaseAuthenticatedTags []uint32
	// AutoSubscribe nil or true subscribes the node — "unless disabled
	// explicitly" (CommissioningClient.ts:310-311).
	AutoSubscribe *bool
	// DefaultSubscription narrows the default subscription; nil is the
	// wildcard (:309).
	DefaultSubscription *SubscribeOptions
	// NoInitialRead skips the initial read of the newly commissioned node
	// (autoStateInitialize false, :312).
	NoInitialRead bool
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
// the start's error; a Commission that returns before the node starts
// reports it skipped.
func (r *Registry) Commission(ctx context.Context, n *Node, o CommissionOptions) error {
	starting := false
	err := r.runFabricOperation(n, opCommission, func() error { return r.commission(ctx, n, o, &starting) })
	if !starting {
		commissioner.ReportSubscribe(o.Progress, commissioner.StepSkipped, nil)
	}
	return err
}

func (r *Registry) commission(ctx context.Context, n *Node, o CommissionOptions, starting *bool) error {
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
	// type "ble" (CommissioningClient.ts:883-895), which passes the check
	// above (:220-223); a Record's NetworkAddress is IP only, so the
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
	p := r.cfg.Peers.Get(res.Address)
	if o.CaseAuthenticatedTags != nil || p == nil {
		// The CATs reach the peer as #catsChanged gives them
		// (CommissioningClient.ts:664-680).
		//nolint:contextcheck // the peer's service resolves under the DNS-SD cache's context
		if p, err = r.cfg.Peers.AddKnownPeer(peer.Descriptor{Address: res.Address, CaseAuthenticatedTags: o.CaseAuthenticatedTags}); err != nil {
			return err
		}
	}
	now := r.clock.Now()
	d := p.Descriptor()
	n.updateRecord(func(rec *Record) {
		a := res.Address
		rec.PeerAddress = &a
		rec.CommissionedAt = now
		rec.FabricIndexOnPeer = res.FabricIndexOnPeer
		rec.applyPeer(d)
		if o.CaseAuthenticatedTags != nil {
			rec.CaseAuthenticatedTags = slices.Clone(o.CaseAuthenticatedTags)
		}
	})
	r.bindPeer(n, p)
	n.mu.Lock()
	n.settings.defaultSubscription = o.DefaultSubscription.clone()
	n.settings.autoSubscribe = o.AutoSubscribe == nil || *o.AutoSubscribe
	n.s.subscribedFabricFiltered = n.settings.defaultSubscription == nil || !n.settings.defaultSubscription.NoFabricFilter
	n.cfg.NoInitialRead = o.NoInitialRead
	n.newlyCommissioned = true
	n.saveSettingsLocked()
	n.mu.Unlock()
	r.log.Info("matter.controller.client.registry.commissioned", slog.String("node", n.ID()), slog.String("peer", peerAddressString(res.Address)))
	if h := n.cfg.Hooks.Commissioned; h != nil {
		h()
	}
	*starting = true
	commissioner.ReportSubscribe(o.Progress, commissioner.StepStarted, nil)
	if err := n.Start(ctx); err != nil {
		commissioner.ReportSubscribe(o.Progress, commissioner.StepFailed, err)
		return err
	}
	commissioner.ReportSubscribe(o.Progress, commissioner.StepSucceeded, nil)
	return nil
}

// Decommission removes the node from its fabric and deletes it —
// ClientNode.decommission (node/ClientNode.ts:176-186) as the node's
// fabric operation: a commissioned node is decommissioned first
// (CommissioningClient.decommission, CommissioningClient.ts:340-446), then
// the node is deleted (:184). A removal the peer refused, or one that was
// not confirmed, returns its error and keeps the node.
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

// leaveLatch is the decommission's watch for the peer's leave event
// (CommissioningClient.ts:380-391).
type leaveLatch struct {
	fabricIndex uint8
	seen        atomic.Bool
}

// noteLeave is the latch's listener: a leave of the fabric index being
// removed, while the subscription is active, confirms the removal
// (:384-390).
func (r *Registry) noteLeave(n *Node, fabricIndex uint8) {
	r.mu.Lock()
	l := r.leaves[n]
	r.mu.Unlock()
	if l != nil && l.fabricIndex == fabricIndex && n.SubscriptionActive() {
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
	addr, ok := n.PeerAddress()
	if !ok {
		return addr, ErrNotCommissioned
	}
	former := peerAddressString(addr)
	rec := n.Record()
	oc := n.opCreds()
	current := currentFabricIndex(oc)
	if oc == nil || (rec.FabricIndexOnPeer == 0 && current == 0) {
		n.mu.RLock()
		noFilter := n.settings.defaultSubscription != nil && n.settings.defaultSubscription.NoFabricFilter
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
	res, err := n.Invoke(ctx, opCredsCommand(opCredsCmdRemoveFabric), removeFabricRequest(fabricIndex), InvokeOptions{})
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
// (CommissioningClient.ts:406-408). PeerMessageMissingError and
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
