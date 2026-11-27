// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/internal/deepcopy"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
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

	// mu guards closed, deadline, onClosed, pending and timer.
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

// snapshot is a copy of the values staged so far. A commit judges and
// stores the snapshot it took when it began, so a value the peer stages
// while the commit runs reaches neither its checks nor its store
// (R2-C2-conc-1501).
func (a *atomicWriteState) snapshot() atomicStaged {
	a.mu.Lock()
	defer a.mu.Unlock()
	return maps.Clone(a.pending)
}

// atomicStaged is a commit's snapshot of a state's staged values.
type atomicStaged map[uint32]any

// staged is the value the snapshot holds for attrID, if any.
func (m atomicStaged) staged(attrID uint32) (any, bool) {
	v, ok := m[attrID]
	return v, ok
}

// holds reports whether the state holds attrID.
func (a *atomicWriteState) holds(attrID uint32) bool { return slices.Contains(a.attrs, attrID) }

// atomicWrites is the set of a server's atomic writes in progress —
// matter.js AtomicWriteHandler's #pendingWrites (AtomicWriteHandler.ts:72).
type atomicWrites struct {
	// mu guards states.
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

// forAttribute is the state that holds attrID, whoever its peer — matter.js
// #pendingWriteStateForAttribute (AtomicWriteHandler.ts:405-420). An
// attribute a state holds is answered BUSY to every other peer, so there
// is at most one.
func (w *atomicWrites) forAttribute(now time.Time, attrID uint32) *atomicWriteState {
	for _, s := range w.live(now) {
		if s.holds(attrID) {
			return s
		}
	}
	return nil
}

// closeFabric closes the states of fabricIndex's peers — matter.js
// #handleFabricRemoval (AtomicWriteHandler.ts:378-388).
func (w *atomicWrites) closeFabric(fabricIndex uint8, log *slog.Logger) {
	w.mu.Lock()
	all := slices.Clone(w.states)
	w.mu.Unlock()
	for _, s := range all {
		if s.peer.fabricIndex == fabricIndex {
			log.Debug("matter.thermostat.atomic_write_closed",
				slog.Int("fabric_index", int(fabricIndex)), slog.Uint64("node_id", s.peer.nodeID),
				slog.String("reason", "fabric removed"))
			s.close()
		}
	}
}

// closeAll closes every state — matter.js AtomicWriteHandler.close
// (AtomicWriteHandler.ts:84-89), which ThermostatServer's
// [Symbol.asyncDispose] runs (ThermostatServer.ts:1582-1585), both at
// db021da0.
func (w *atomicWrites) closeAll() {
	w.mu.Lock()
	all := slices.Clone(w.states)
	w.mu.Unlock()
	for _, s := range all {
		s.close()
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
	return tdef.AtomicRequestRequest{}, spec.Errorf(im.StatusInvalidCommand, "thermo: AtomicRequest carried %T", fields)
}

// handleAtomicRequest is matter.js ThermostatBaseServer.atomicRequest
// (ThermostatServer.ts:1588-1605 at db021da0): BeginWrite, CommitWrite or
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
		return nil, spec.Errorf(im.StatusConstraintError, "thermo: AtomicRequest RequestType %d is not a RequestTypeEnum value", req.RequestType)
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
		return atomicPeer{}, spec.Errorf(im.StatusInvalidCommand, "thermo: AtomicRequest requires an operational session")
	}
	if len(req.AttributeRequests) == 0 {
		return atomicPeer{}, spec.Errorf(im.StatusInvalidCommand, "thermo: no attribute requests provided")
	}
	seen := map[uint32]bool{}
	for _, attr := range req.AttributeRequests {
		if !s.atomicSupported(attr) {
			return atomicPeer{}, spec.Errorf(im.StatusInvalidCommand, "thermo: attribute 0x%04X not supported by cluster", attr)
		}
		if seen[attr] {
			return atomicPeer{}, spec.Errorf(im.StatusInvalidCommand, "thermo: duplicate attribute in attribute requests")
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
		return nil, spec.Errorf(im.StatusInvalidInState, "thermo: no atomic write in progress for this peer")
	}
	if len(st.attrs) != len(req.AttributeRequests) {
		return nil, spec.Errorf(im.StatusInvalidInState, "thermo: attribute requests do not match existing atomic write")
	}
	for _, attr := range st.attrs {
		if !slices.Contains(req.AttributeRequests, attr) {
			return nil, spec.Errorf(im.StatusInvalidInState, "thermo: attribute requests do not match existing atomic write")
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
		return nil, spec.Errorf(im.StatusInvalidInState, "thermo: an atomic write is already in progress for this peer on this cluster and endpoint")
	}
	if req.Timeout == nil {
		return nil, spec.Errorf(im.StatusInvalidCommand, "thermo: timeout missing for BeginWrite request")
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

	// Nothing is staged until the peer writes: matter.js keeps a pending
	// value only once written and commits only those
	// (AtomicWriteHandler.ts:272, :298-301 at b25ed960); the peer reads the
	// stored value until then (MatterReadFiltered).
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
// (AtomicWriteHandler.ts:279-346 at db91886c): every staged value is
// validated and applied as one unit, in the CommitWrite request's order,
// an attribute the peer did not write reporting SUCCESS; a failure
// discards all of them, and the command reports FAILURE with the reasons
// in the attributes' statuses. The state closes whatever the outcome.
//
// The unit runs in three passes, as matter.js's does: every staged
// attribute's $AtomicChanging and store (:298-312, [ThermostatServer.precommitAtomic]),
// then the $AtomicChanged checks of the ones that passed (:314-320,
// [ThermostatServer.atomicChanged]) — which read the other attributes of
// the same unit as staged — and, only when all passed, the commit
// ([ThermostatServer.commitAll], which undoes the committed attributes
// when a later host port refuses).
func (s *ThermostatServer) commitWrite(ctx context.Context, req tdef.AtomicRequestRequest) (any, error) {
	st, err := s.existingAtomicState(ctx, req)
	if err != nil {
		return nil, err
	}
	defer st.close()
	// Every pass reads the values staged when the commit began: the
	// peer's own Presets or Schedules write is not under s.op and could
	// otherwise replace a value between the checks and the store.
	unit := st.snapshot()
	statuses := make([]tdef.AtomicResponseAttributeStatusEntry, len(req.AttributeRequests))
	for i, attr := range req.AttributeRequests {
		statuses[i] = tdef.AtomicResponseAttributeStatusEntry{AttributeId: attr, StatusCode: tdef.StatusSuccess}
	}
	run := func(step string, fn func(attr uint32, v any) error) bool {
		ok := true
		for i, attr := range req.AttributeRequests {
			v, staged := unit.staged(attr)
			if !staged || statuses[i].StatusCode != tdef.StatusSuccess {
				continue
			}
			if err := fn(attr, v); err != nil {
				statuses[i].StatusCode = tdef.Status(statusOf(err, step+" attribute "+atomicAttributeName(attr), s.logger()))
				ok = false
			}
		}
		return ok
	}
	staging := run("staging", func(attr uint32, v any) error { return s.precommitAtomic(unit, attr, v) })
	// The Schedules of the unit's transaction: the staged ones once they
	// passed staging, the stored ones otherwise (persistedSchedules,
	// ThermostatServer.ts:2051).
	var unitSchedules []Schedule
	for i, attr := range req.AttributeRequests {
		if attr != tdef.AttrSchedules || statuses[i].StatusCode != tdef.StatusSuccess {
			continue
		}
		if v, staged := unit.staged(attr); staged {
			unitSchedules, _ = v.([]Schedule)
			if unitSchedules == nil {
				unitSchedules = []Schedule{}
			}
		}
	}
	changed := run("validating", func(attr uint32, v any) error { return s.atomicChanged(attr, v, unitSchedules) })
	done := staging && changed && s.commitAll(ctx, unit, req.AttributeRequests, statuses)
	if _, staged := unit.staged(tdef.AttrPresets); done && staged {
		// Once the whole unit committed: a reverted Presets commit leaves
		// the suggestions as they were, as matter.js's rolled-back
		// transaction does (ThermostatServer.ts:1792-1794 at b25ed960).
		s.reevaluateOnPresetsChange(ctx)
	}
	command := tdef.StatusSuccess
	if !done {
		command = tdef.StatusFailure
	}
	return tdef.AtomicResponse{StatusCode: command, AttributeStatus: statuses}, nil
}

// commitAll commits every staged attribute after all precommits passed,
// as one unit: a host port that refuses a later attribute (SavePresets,
// CommitSchedules) undoes the attributes already committed — their
// previous value is stored and handed to the port again — and its status
// stands for every attribute. Mirrors matter.js
// packages/node/src/behaviors/thermostat/AtomicWriteHandler.ts:322-334 at
// b25ed960: any failure rolls the one transaction back, and a failure
// while committing "cannot be traced to one attribute, so it stands for
// each of them" (AtomicWriteHandlerTest.ts:491-517 "discards a valid
// Presets write when the Schedules write fails"; W3-clua1-02). The
// caller holds s.op; unit is the commit's snapshot of the staged values.
func (s *ThermostatServer) commitAll(ctx context.Context, unit atomicStaged, attrs []uint32, statuses []tdef.AtomicResponseAttributeStatusEntry) bool {
	s.mu.RLock()
	prevPresets := deepcopy.Each(s.presets)
	prevSchedules := deepcopy.Each(s.schedules)
	s.mu.RUnlock()
	var committed []uint32
	for _, attr := range attrs {
		v, staged := unit.staged(attr)
		if !staged {
			continue
		}
		if err := s.commitAtomic(ctx, attr, v); err != nil {
			code := tdef.Status(statusOf(err, "committing attribute "+atomicAttributeName(attr), s.logger()))
			for i := range statuses {
				statuses[i].StatusCode = code
			}
			for _, done := range committed {
				s.revertAtomic(ctx, done, prevPresets, prevSchedules)
			}
			return false
		}
		committed = append(committed, attr)
	}
	return true
}

// revertAtomic restores an attribute commitAll already committed to the
// value it had before the commit: stored, handed to its host port again
// and reported. A port that refuses the restore is logged; the stored
// value is restored regardless, so the attribute reads what the
// controller was told is still in force.
func (s *ThermostatServer) revertAtomic(ctx context.Context, attr uint32, presets []Preset, schedules []Schedule) {
	var err error
	switch attr {
	case tdef.AttrPresets:
		s.lockPersist()
		s.mu.RLock()
		active := bytes.Clone(s.activePresetHandle)
		s.mu.RUnlock()
		err = s.savePresets(ctx, presets, active)
		if err != nil {
			// Memory is restored whatever the store answers; the pair is
			// behind and its next save retries (R3-store-3253, decision 570).
			s.presetsBehind = true
		}
		s.mu.Lock()
		s.presets = presets
		s.mu.Unlock()
		s.unlockPersist()
		s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrPresets)
	case tdef.AttrSchedules:
		if s.schedulePort != nil {
			err = s.schedulePort.CommitSchedules(ctx, deepcopy.Each(schedules))
		}
		s.mu.Lock()
		changed := !schedulesTLVEqual(s.schedules, schedules)
		s.schedules = schedules
		s.mu.Unlock()
		if changed {
			s.changes.NotifyFrom(contract.OriginFrom(ctx), tdef.AttrSchedules)
		}
	}
	if err != nil {
		s.logger().Warn("matter.thermostat.atomic_revert_failed",
			slog.String("attribute", atomicAttributeName(attr)), slog.String("reason", err.Error()))
	}
}

// atomicAttributeName names an atomic attribute for the log.
func atomicAttributeName(attr uint32) string {
	if a := tdef.Definition.Attribute(attr); a != nil {
		return a.Name
	}
	return fmt.Sprintf("0x%04X", attr)
}

// precommitAtomic is an attribute's $AtomicChanging and store at commit
// (AtomicWriteHandler.ts:298-312 at b25ed960): Presets validated as
// staged against the stored presets (presets$AtomicChanging,
// ThermostatServer.ts:1760-1762) and, on a normalised copy, refused when
// it removes a built-in or the active preset (#validatePresetCommit
// :2032-2080, which matter.js runs in presets$AtomicChanged, :1983-1985);
// Schedules held to the attribute's schema and validated again against
// the stored schedules and the peer's pending Presets (schedules$AtomicChanging,
// :2581-2591; the presets getter, :3196-3222), which are the Presets staged
// in st: a peer holds at most one atomic write on the cluster
// (AtomicWriteHandler.ts:178-185 at b25ed960, in beginWrite),
// so the pending value matter.js's getter finds for it
// (pendingValueForAttributeAndPeer, :433-458) is this unit's. unit is the
// commit's snapshot of the state's staged values.
func (s *ThermostatServer) precommitAtomic(unit atomicStaged, attr uint32, v any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch attr {
	case tdef.AttrPresets:
		staged, _ := v.([]Preset) // writePresets stages a []Preset
		next := deepcopy.Each(staged)
		issued := map[string]bool{}
		if err := s.presetRules().validatePresetWrite(next, s.presets, false, issued); err != nil {
			return err
		}
		return normalizePresetCommit(next, s.presets, false, issued, s.activePresetHandle)
	case tdef.AttrSchedules:
		staged, _ := v.([]Schedule) // writeSchedules stages a []Schedule
		if _, err := s.inst.CheckValue(tdef.AttrSchedules, spec.List[Schedule](staged), s.readLocked); err != nil {
			return spec.Errorf(im.StatusConstraintError, "thermo: staged Schedules: %v", err)
		}
		presets := s.presets
		if pv, ok := unit.staged(tdef.AttrPresets); ok {
			if pending, isList := pv.([]Preset); isList {
				presets = pending
			}
		}
		_, err := s.scheduleRules(presets).validateSchedules(s.schedules, staged)
		return err
	}
	return nil
}

// atomicChanged is an attribute's $AtomicChanged at commit, after every
// staged attribute of the unit was staged (AtomicWriteHandler.ts:314-320
// at b25ed960): with MSCH, Presets refused when it drops a preset a
// schedule of the unit's transaction names (#validatePresetCommit,
// ThermostatServer.ts:2032-2080; its schedules half :2048-2065) —
// schedules the unit's Schedules when they staged, nil for the stored
// ones; Schedules refused when it drops a built-in or the active schedule
// (schedules$AtomicChanged, :3009-3014, #validateScheduleCommit
// :3056-3086).
func (s *ThermostatServer) atomicChanged(attr uint32, v any, schedules []Schedule) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch attr {
	case tdef.AttrPresets:
		if schedules == nil {
			schedules = s.schedules
		}
		staged, _ := v.([]Preset)
		return s.presetsInUseBySchedules(s.presets, staged, schedules)
	case tdef.AttrSchedules:
		staged, _ := v.([]Schedule)
		return validateScheduleCommit(s.schedules, staged, s.activeScheduleHandle)
	}
	return nil
}

// commitAtomic applies a staged value after every precommit passed. The
// caller holds s.op.
func (s *ThermostatServer) commitAtomic(ctx context.Context, attr uint32, v any) error {
	switch attr {
	case tdef.AttrPresets:
		staged, _ := v.([]Preset)
		return s.commitPresets(ctx, staged)
	case tdef.AttrSchedules:
		staged, _ := v.([]Schedule)
		return s.commitSchedules(ctx, staged)
	}
	return nil
}

// statusOf is the status an attribute reports for an error raised while
// committing it — matter.js #statusOf (AtomicWriteHandler.ts:345-353): a
// refusal carrying a Matter status reports it, any other error FAILURE.
func statusOf(err error, step string, log *slog.Logger) im.StatusCode {
	if sc, ok := asStatus(err); ok {
		log.Debug("matter.thermostat.atomic_commit_failed", slog.String("step", step), slog.String("reason", err.Error()))
		return sc
	}
	log.Warn("matter.thermostat.atomic_commit_failed", slog.String("step", step), slog.String("reason", err.Error()))
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
	return s.stageAtomicAttribute(ctx, attrID, im.StatusBusy, func() (any, error) {
		if err := validate(v); err != nil {
			return nil, err
		}
		return v, nil
	})
}

// stageAtomicAttribute stages the value stage computes in the atomic write
// that holds attrID, for the peer holding it; another peer's write is
// answered otherPeer — BUSY for Presets, INVALID_IN_STATE for Schedules
// (matter.js answersBusyToOtherPeer, AtomicWriteHandler.ts:54-56, :474-479
// at b25ed960).
func (s *ThermostatServer) stageAtomicAttribute(ctx context.Context, attrID uint32, otherPeer im.StatusCode, stage func() (any, error)) error {
	now := s.clk.Now()
	st := s.atomic.forAttribute(now, attrID)
	if st == nil {
		return spec.Errorf(im.StatusInvalidInState, "thermo: there is no atomic write in progress for this attribute")
	}
	peer, ok := atomicPeerOf(ctx)
	if !ok {
		return spec.Errorf(im.StatusInvalidInState, "thermo: there is no atomic write in progress for this peer")
	}
	if st.peer != peer {
		return spec.Errorf(otherPeer, "thermo: attribute is part of an atomic write in progress for a different peer")
	}
	v, err := stage()
	if err != nil {
		return err
	}
	st.stage(attrID, v)
	return nil
}

// pendingAtomicValue is the value the peer of ctx staged for attrID, if it
// holds the attribute — matter.js pendingValueForAttributeAndPeer
// (AtomicWriteHandler.ts:433-458 at b25ed960), which the Presets and
// Schedules getters consult (ThermostatServer.ts:3196-3222, :3244-3270).
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
// (AtomicWriteHandler.ts:75, :378-388). The node reaches it through
// [ThermostatServer.MatterDropFabric], the [contract.FabricScopedState]
// port it calls on every server when a fabric is removed.
func (s *ThermostatServer) MatterFabricRemoved(fabricIndex uint8) {
	s.atomic.closeFabric(fabricIndex, s.logger())
}

// MatterDropFabric implements [contract.FabricScopedState]: the atomic
// writes of fabricIndex's peers close ([ThermostatServer.MatterFabricRemoved]).
// They live in memory only, so it never fails.
func (s *ThermostatServer) MatterDropFabric(_ context.Context, fabricIndex uint8) error {
	s.MatterFabricRemoved(fabricIndex)
	return nil
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// whose peers hold an atomic write open.
func (s *ThermostatServer) MatterHeldFabrics() []uint8 {
	var out []uint8
	for _, st := range s.atomic.live(s.clk.Now()) {
		if !slices.Contains(out, st.peer.fabricIndex) {
			out = append(out, st.peer.fabricIndex)
		}
	}
	return out
}
