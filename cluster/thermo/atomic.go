// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/im"
)

// The atomic writes of the Thermostat server: the port of matter.js
// packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts and
// AtomicWriteState.ts at 862ad75f for this server (matter.js runs one
// handler per node environment; the Thermostat is its only user, so here
// each server holds the states of its own endpoint).

// The AtomicRequest request types (the RequestTypeEnum of the global
// AtomicRequest command, thermostat AtomicRequest field 0).
const (
	atomicBeginWrite    uint8 = 0
	atomicCommitWrite   uint8 = 1
	atomicRollbackWrite uint8 = 2
)

// maxAtomicTimeout bounds the hold a BeginWrite asks for. Mirrors matter.js
// AtomicWriteState.ts:15-16 (MAXIMUM_ALLOWED_TIMEOUT = Seconds(9): "SDK uses
// 3s for Presets and 9s for Schedules, so just use 9s in general") and
// :51 (`Math.min(timeout, MAXIMUM_ALLOWED_TIMEOUT)`).
const maxAtomicTimeout = 9 * time.Second

// atomicPeer is the Atomic Writer of a state: matter.js's PeerAddress of
// fabric index and operational node id (AtomicWriteHandler.ts:474-482).
type atomicPeer struct {
	fabricIndex uint8
	nodeID      uint64
}

// atomicWriteState is one atomic write in progress — matter.js
// AtomicWriteState (AtomicWriteState.ts:24-97): its peer, the attributes
// it holds, the timeout (clamped), the values staged so far, and the
// timer that closes it.
type atomicWriteState struct {
	peer     atomicPeer
	attrs    []uint32
	timeout  time.Duration
	deadline time.Time

	mu       sync.Mutex
	pending  map[uint32]any
	timer    clock.Timer
	closed   bool
	onClosed []func()
}

// newAtomicWriteState builds a state; timeout is the request's, in
// milliseconds (AtomicWriteState.ts:51, :55 `Millis(this.timeout)`).
func newAtomicWriteState(peer atomicPeer, attrs []uint32, timeoutMs uint16) *atomicWriteState {
	return &atomicWriteState{
		peer:    peer,
		attrs:   slices.Clone(attrs),
		timeout: min(time.Duration(timeoutMs)*time.Millisecond, maxAtomicTimeout),
		pending: map[uint32]any{},
	}
}

// start arms the timeout on clk: once it passes, the state closes
// (AtomicWriteState.ts:70-82). The deadline is kept as well, so a lookup
// after the timeout sees the state closed even before the timer's
// goroutine ran.
func (a *atomicWriteState) start(clk clock.Clock) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deadline = clk.Now().Add(a.timeout)
	a.timer = clk.AfterFunc(a.timeout, a.close)
}

// whenClosed registers fn to run once, when the state closes — matter.js's
// `closed` Observable (AtomicWriteState.ts:33-34).
func (a *atomicWriteState) whenClosed(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onClosed = append(a.onClosed, fn)
}

// close ends the state; a second close does nothing, so its observers run
// exactly once (AtomicWriteState.ts:84-96).
func (a *atomicWriteState) close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	if a.timer != nil {
		a.timer.Stop()
	}
	observers := a.onClosed
	a.onClosed = nil
	a.mu.Unlock()
	for _, fn := range observers {
		fn()
	}
}

// live reports whether the state still holds its attributes at now.
func (a *atomicWriteState) live(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.closed && (a.deadline.IsZero() || now.Before(a.deadline))
}

// stage records a value written inside the write
// (AtomicWriteHandler.ts:264).
func (a *atomicWriteState) stage(attrID uint32, v any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending[attrID] = v
}

// staged is the value staged for attrID, if any.
func (a *atomicWriteState) staged(attrID uint32) (any, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.pending[attrID]
	return v, ok
}

// holds reports whether the state holds attrID.
func (a *atomicWriteState) holds(attrID uint32) bool { return slices.Contains(a.attrs, attrID) }

// atomicWrites is the set of a server's atomic writes in progress —
// matter.js AtomicWriteHandler's #pendingWrites (AtomicWriteHandler.ts:72).
type atomicWrites struct {
	mu     sync.Mutex
	states []*atomicWriteState
}

// add keeps st until it closes (AtomicWriteHandler.ts:227-228).
func (w *atomicWrites) add(st *atomicWriteState) {
	w.mu.Lock()
	w.states = append(w.states, st)
	w.mu.Unlock()
	st.whenClosed(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.states = slices.DeleteFunc(w.states, func(s *atomicWriteState) bool { return s == st })
	})
}

// live lists the states still open at now.
func (w *atomicWrites) live(now time.Time) []*atomicWriteState {
	w.mu.Lock()
	all := slices.Clone(w.states)
	w.mu.Unlock()
	return slices.DeleteFunc(all, func(s *atomicWriteState) bool { return !s.live(now) })
}

// forPeer is the state peer holds on this endpoint and cluster
// (AtomicWriteHandler.ts:393-400).
func (w *atomicWrites) forPeer(now time.Time, peer atomicPeer) *atomicWriteState {
	for _, s := range w.live(now) {
		if s.peer == peer {
			return s
		}
	}
	return nil
}

// forAttribute is the state that holds attrID, whoever its peer
// (AtomicWriteHandler.ts:405-420; BeginWrite answers BUSY for an attribute
// a state holds, so there is at most one).
func (w *atomicWrites) forAttribute(now time.Time, attrID uint32) *atomicWriteState {
	for _, s := range w.live(now) {
		if s.holds(attrID) {
			return s
		}
	}
	return nil
}

// closeAll closes every state (AtomicWriteHandler.ts:84-89).
func (w *atomicWrites) closeAll() {
	w.mu.Lock()
	all := slices.Clone(w.states)
	w.mu.Unlock()
	for _, s := range all {
		s.close()
	}
}

// closeFabric closes the states of fabricIndex's peers — matter.js
// #handleFabricRemoval (AtomicWriteHandler.ts:378-388).
func (w *atomicWrites) closeFabric(fabricIndex uint8) {
	w.mu.Lock()
	all := slices.Clone(w.states)
	w.mu.Unlock()
	for _, s := range all {
		if s.peer.fabricIndex == fabricIndex {
			slog.Default().Debug("matter.thermostat.atomic_write_closed",
				slog.Int("fabric_index", int(fabricIndex)), slog.Uint64("node_id", s.peer.nodeID),
				slog.String("reason", "fabric removed"))
			s.close()
		}
	}
}

// isOperationalNodeID reports whether id is an operational node id
// (0x0000_0000_0000_0001 to 0xFFFF_FFEF_FFFF_FFFF, Matter Core §2.5.5.1;
// matter.js NodeId.isOperationalNodeId).
func isOperationalNodeID(id uint64) bool { return id != 0 && id <= 0xFFFF_FFEF_FFFF_FFFF }

// atomicPeerOf derives the Atomic Writer of the request ctx belongs to —
// matter.js #derivePeerAddress (AtomicWriteHandler.ts:474-482): a remote
// actor whose subject is an operational node, on its fabric. A local
// caller (no Interaction Model request behind ctx), a group subject and a
// PASE session (subject node id 0) have none.
func atomicPeerOf(ctx context.Context) (atomicPeer, bool) {
	_, fabric, remote := im.LookupFabricFilter(ctx)
	if !remote {
		return atomicPeer{}, false
	}
	if _, group := im.GroupSubjectFromContext(ctx); group {
		return atomicPeer{}, false
	}
	nodeID, _ := im.SubjectFromContext(ctx)
	if !isOperationalNodeID(nodeID) {
		return atomicPeer{}, false
	}
	return atomicPeer{fabricIndex: fabric, nodeID: nodeID}, true
}

// atomicRequest normalises the AtomicRequest payload: the bridge decodes it
// through the generated definition.
func atomicRequest(fields any) (tdef.AtomicRequestRequest, error) {
	switch v := fields.(type) {
	case tdef.AtomicRequestRequest:
		return v, nil
	case *tdef.AtomicRequestRequest:
		if v != nil {
			return *v, nil
		}
	}
	return tdef.AtomicRequestRequest{}, spec.Errorf(im.StatusInvalidCommand, "thermostat: AtomicRequest carried %T", fields)
}

// handleAtomicRequest is matter.js ThermostatBaseServer.atomicRequest
// (ThermostatServer.ts:1602-1620): BeginWrite, CommitWrite or
// RollbackWrite.
func (s *ThermostatServer) handleAtomicRequest(ctx context.Context, fields any) (any, error) {
	req, err := atomicRequest(fields)
	if err != nil {
		return nil, err
	}
	switch req.RequestType {
	case atomicBeginWrite:
		return s.beginWrite(ctx, req)
	case atomicCommitWrite:
		return s.commitWrite(ctx, req)
	case atomicRollbackWrite:
		return s.rollbackWrite(ctx, req)
	default:
		// matter.js's request schema refuses a value outside
		// RequestTypeEnum before the handler runs (a ValidationError,
		// ConstraintError).
		return nil, spec.Errorf(im.StatusConstraintError, "thermostat: AtomicRequest RequestType %d is not a RequestTypeEnum value", req.RequestType)
	}
}

// atomicSupported reports whether attrID is an attribute of the server:
// served, or a global attribute (matter.js #validateRequest,
// AtomicWriteHandler.ts:111-114: conformant and present in the state).
func (s *ThermostatServer) atomicSupported(attrID uint32) bool {
	if _, ok := s.inst.ReadGlobal(attrID); ok || isGlobalAttribute(attrID) {
		return true
	}
	return s.inst.Serves(attrID)
}

// isGlobalAttribute is GeneratedCommandList … ClusterRevision.
func isGlobalAttribute(id uint32) bool { return id >= 0xFFF8 && id <= 0xFFFD }

// validateAtomicRequest is matter.js #validateRequest
// (AtomicWriteHandler.ts:97-122): the checks every request type shares and
// that fail the whole command — an operational peer, at least one
// attribute, every one supported and none twice.
func (s *ThermostatServer) validateAtomicRequest(ctx context.Context, req tdef.AtomicRequestRequest) (atomicPeer, error) {
	// #assertValidPeer (AtomicWriteHandler.ts:484-492): a remote actor on
	// an operational session.
	peer, ok := atomicPeerOf(ctx)
	if !ok || peer.fabricIndex == 0 {
		return atomicPeer{}, spec.Errorf(im.StatusInvalidCommand, "thermostat: AtomicRequest requires an operational session")
	}
	if len(req.AttributeRequests) == 0 {
		return atomicPeer{}, spec.Errorf(im.StatusInvalidCommand, "thermostat: no attribute requests provided")
	}
	seen := map[uint32]bool{}
	for _, attr := range req.AttributeRequests {
		if !s.atomicSupported(attr) {
			return atomicPeer{}, spec.Errorf(im.StatusInvalidCommand, "thermostat: attribute 0x%04X not supported by cluster", attr)
		}
		if seen[attr] {
			return atomicPeer{}, spec.Errorf(im.StatusInvalidCommand, "thermostat: duplicate attribute in attribute requests")
		}
		seen[attr] = true
	}
	return peer, nil
}

// existingAtomicState is matter.js #assertExistingState
// (AtomicWriteHandler.ts:127-150): the state a CommitWrite or RollbackWrite
// addresses, holding exactly the attributes the request names.
func (s *ThermostatServer) existingAtomicState(ctx context.Context, req tdef.AtomicRequestRequest) (*atomicWriteState, error) {
	peer, err := s.validateAtomicRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	st := s.atomic.forPeer(s.clk.Now(), peer)
	if st == nil {
		return nil, spec.Errorf(im.StatusInvalidInState, "thermostat: no atomic write in progress for this peer")
	}
	if len(st.attrs) != len(req.AttributeRequests) {
		return nil, spec.Errorf(im.StatusInvalidInState, "thermostat: attribute requests do not match existing atomic write")
	}
	for _, attr := range st.attrs {
		if !slices.Contains(req.AttributeRequests, attr) {
			return nil, spec.Errorf(im.StatusInvalidInState, "thermostat: attribute requests do not match existing atomic write")
		}
	}
	return st, nil
}

// beginWrite is matter.js AtomicWriteHandler.beginWrite
// (AtomicWriteHandler.ts:155-237).
func (s *ThermostatServer) beginWrite(ctx context.Context, req tdef.AtomicRequestRequest) (any, error) {
	peer, err := s.validateAtomicRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	now := s.clk.Now()
	// §7.15.6.4.1 step 1: a peer holding an atomic write on the cluster and
	// endpoint is refused at command level (:170-176).
	if s.atomic.forPeer(now, peer) != nil {
		return nil, spec.Errorf(im.StatusInvalidInState, "thermostat: an atomic write is already in progress for this peer on this cluster and endpoint")
	}
	if req.Timeout == nil {
		return nil, spec.Errorf(im.StatusInvalidCommand, "thermostat: timeout missing for BeginWrite request")
	}

	// The location of the access check: the endpoint the command runs at
	// (:187-193).
	path, _ := im.CommandPathFromContext(ctx)
	command := tdef.StatusSuccess
	statuses := make([]tdef.AtomicResponseAttributeStatusEntry, 0, len(req.AttributeRequests))
	for _, attr := range req.AttributeRequests {
		status := tdef.StatusSuccess
		a := tdef.Definition.Attribute(attr)
		switch {
		case !im.AuthorityAt(ctx, path.Endpoint, ThermostatClusterID, s.inst.MinWritePrivilege(attr)).IsSuccess():
			status = tdef.StatusUnsupportedAccess
		case a == nil || !a.Quality.Atomic:
			status = tdef.StatusInvalidCommand
		case s.atomic.forAttribute(now, attr) != nil:
			status = tdef.StatusBusy
		}
		if status != tdef.StatusSuccess {
			command = tdef.StatusFailure
		}
		statuses = append(statuses, tdef.AtomicResponseAttributeStatusEntry{AttributeId: attr, StatusCode: status})
	}
	if command != tdef.StatusSuccess {
		return tdef.AtomicResponse{StatusCode: command, AttributeStatus: statuses}, nil
	}

	st := newAtomicWriteState(peer, req.AttributeRequests, *req.Timeout)
	s.atomic.add(st)
	st.start(s.clk)
	timeout := uint16(st.timeout / time.Millisecond) //nolint:gosec // at most maxAtomicTimeout, 9000 ms
	return tdef.AtomicResponse{StatusCode: command, AttributeStatus: statuses, Timeout: &timeout}, nil
}

// rollbackWrite is matter.js AtomicWriteHandler.rollbackWrite
// (AtomicWriteHandler.ts:358-373): the state closes and its staged values
// are dropped.
func (s *ThermostatServer) rollbackWrite(ctx context.Context, req tdef.AtomicRequestRequest) (any, error) {
	st, err := s.existingAtomicState(ctx, req)
	if err != nil {
		return nil, err
	}
	st.close()
	statuses := make([]tdef.AtomicResponseAttributeStatusEntry, 0, len(st.attrs))
	for _, attr := range st.attrs {
		statuses = append(statuses, tdef.AtomicResponseAttributeStatusEntry{AttributeId: attr, StatusCode: tdef.StatusSuccess})
	}
	return tdef.AtomicResponse{StatusCode: tdef.StatusSuccess, AttributeStatus: statuses}, nil
}

// commitWrite is matter.js AtomicWriteHandler.commitWrite
// (AtomicWriteHandler.ts:271-339): every staged value is validated and
// applied as one unit, in the CommitWrite request's order, an attribute the
// peer did not write reporting SUCCESS; a failure discards all of them, and
// the command reports FAILURE with the reasons in the attributes'
// statuses. The state closes whatever the outcome.
//
// Presets is the one atomic attribute this server serves (Schedules is
// MatterScheduleConfiguration, which it does not), so the unit is at most
// that one value.
func (s *ThermostatServer) commitWrite(ctx context.Context, req tdef.AtomicRequestRequest) (any, error) {
	st, err := s.existingAtomicState(ctx, req)
	if err != nil {
		return nil, err
	}
	defer st.close()
	statuses := make([]tdef.AtomicResponseAttributeStatusEntry, 0, len(req.AttributeRequests))
	failed := false
	for _, attr := range req.AttributeRequests {
		status := tdef.StatusSuccess
		if v, ok := st.staged(attr); ok && attr == tdef.AttrPresets {
			if err := s.commitPresets(ctx, v.([]tdef.PresetStruct)); err != nil {
				status = tdef.Status(statusOf(err, "committing attribute Presets"))
			}
		}
		if status != tdef.StatusSuccess {
			failed = true
		}
		statuses = append(statuses, tdef.AtomicResponseAttributeStatusEntry{AttributeId: attr, StatusCode: status})
	}
	command := tdef.StatusSuccess
	if failed {
		command = tdef.StatusFailure
	}
	return tdef.AtomicResponse{StatusCode: command, AttributeStatus: statuses}, nil
}

// statusOf is the status an attribute reports for an error raised while
// committing it — matter.js #statusOf (AtomicWriteHandler.ts:345-353): a
// refusal carrying a Matter status reports it, any other error FAILURE.
func statusOf(err error, step string) im.StatusCode {
	if sc, ok := asStatus(err); ok {
		slog.Default().Debug("matter.thermostat.atomic_commit_failed", slog.String("step", step), slog.String("reason", err.Error()))
		return sc
	}
	slog.Default().Warn("matter.thermostat.atomic_commit_failed", slog.String("step", step), slog.String("reason", err.Error()))
	return im.StatusFailure
}

// writeAtomicAttribute is matter.js AtomicWriteHandler.writeAttribute
// (AtomicWriteHandler.ts:243-266) with #assertPendingWriteForAttributeAndPeer
// (:452-472): a controller's write of an atomic attribute stages its value
// in the atomic write that holds it, after validate checked it against
// the stored value — the attribute's $AtomicChanging. No atomic write
// holding the attribute, or a request without an Atomic Writer, is
// INVALID_IN_STATE; another peer's hold is BUSY (CHIP and TC-TSTAT-4.2
// step 15, :467-469).
func (s *ThermostatServer) writeAtomicAttribute(ctx context.Context, attrID uint32, v any, validate func(v any) error) error {
	now := s.clk.Now()
	st := s.atomic.forAttribute(now, attrID)
	if st == nil {
		return spec.Errorf(im.StatusInvalidInState, "thermostat: there is no atomic write in progress for this attribute")
	}
	peer, ok := atomicPeerOf(ctx)
	if !ok {
		return spec.Errorf(im.StatusInvalidInState, "thermostat: there is no atomic write in progress for this peer")
	}
	if st.peer != peer {
		return spec.Errorf(im.StatusBusy, "thermostat: attribute is part of an atomic write in progress for a different peer")
	}
	if err := validate(v); err != nil {
		return err
	}
	st.stage(attrID, v)
	return nil
}

// pendingAtomicValue is the value the peer of ctx staged for attrID, if it
// holds the attribute — matter.js pendingValueForAttributeAndPeer
// (AtomicWriteHandler.ts:425-450), which the Presets getter consults
// (ThermostatServer.ts:1680-1692).
func (s *ThermostatServer) pendingAtomicValue(ctx context.Context, attrID uint32) (any, bool) {
	peer, ok := atomicPeerOf(ctx)
	if !ok {
		return nil, false
	}
	st := s.atomic.forAttribute(s.clk.Now(), attrID)
	if st == nil || st.peer != peer {
		return nil, false
	}
	return st.staged(attrID)
}

// MatterFabricRemoved closes the atomic writes of fabricIndex's peers —
// matter.js AtomicWriteHandler observes FabricManager.events.deleting
// (AtomicWriteHandler.ts:75, :378-388). The bridge calls it for every
// cluster server of a device endpoint when a fabric is removed.
func (s *ThermostatServer) MatterFabricRemoved(fabricIndex uint8) { s.atomic.closeFabric(fabricIndex) }
