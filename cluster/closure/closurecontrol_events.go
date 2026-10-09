// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package closure

import (
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/tlv"
)

// The ClosureControl events (closure-control.element.ts at the schema pin):
//
//	0x00 OperationalError   M     critical  ErrorState list<ClosureErrorEnum> 1..10
//	0x01 MovementCompleted  !IS   info      —
//	0x02 EngageStateChanged MO    info      EngageValue bool
//	0x03 SecureStateChanged M     info      SecureValue bool
//
// matter.js's ClosureControlServer is an empty default that leaves every
// behaviour to the implementer, so the triggers below are the
// specification's (Matter 1.6.1 §5.4.7): OperationalError when the closure
// enters the Error state with the errors that caused it, MovementCompleted
// when a motion the server knew about ends at its target, SecureStateChanged
// when OverallCurrentState.SecureState changes value, EngageStateChanged when
// a manually operable closure is disengaged or engaged again.

// OperationalErrorEvent is the OperationalError payload.
type OperationalErrorEvent struct{ ErrorState wire.ClosureErrorList }

// EncodeTLV implements spec.Encodable.
func (e OperationalErrorEvent) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.StartArray(tlv.ContextTag(0))
	for _, v := range e.ErrorState {
		enc.PutUint(tlv.AnonymousTag(), uint64(v))
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
}

// MovementCompletedEvent is the field-less MovementCompleted payload,
// generated from the definition.
type MovementCompletedEvent = closuredef.MovementCompletedEvent

// EngageStateChangedEvent is the EngageStateChanged payload, generated from
// the definition.
type EngageStateChangedEvent = closuredef.EngageStateChangedEvent

// SecureStateChangedEvent is the SecureStateChanged payload, generated from
// the definition.
type SecureStateChangedEvent = closuredef.SecureStateChangedEvent

// MatterEvents implements [contract.ClusterEventLister]: the events the
// advertised feature set carries — OperationalError and SecureStateChanged
// (M), MovementCompleted (!IS), EngageStateChanged (MO).
func (s *ControlServer) MatterEvents() []uint32 { return s.inst.MatterEvents() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// MoveTo (M) and Stop (!IS); Calibrate (CL) is never served. Without it the
// dispatcher answered an empty AcceptedCommandList.
func (s *ControlServer) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister]: every
// ClosureControl command answers with a status.
func (s *ControlServer) MatterGeneratedCommands() []uint32 { return s.inst.MatterGeneratedCommands() }

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege]:
// Stop and MoveTo are Operate (MoveTo timed), Calibrate Manage.
func (s *ControlServer) MinInvokePrivilege(cmdID uint32) uint8 {
	return s.inst.MinInvokePrivilege(cmdID)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *ControlServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (s *ControlServer) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// pendingEvent is an event decided under the lock and emitted after it.
type pendingEvent struct {
	id       uint32
	data     any
	priority contract.EventPriority
}

// emit sends events outside the server lock.
func (s *ControlServer) emit(events []pendingEvent) {
	if len(events) == 0 {
		return
	}
	s.mu.RLock()
	emitter, endpoint := s.emitter, s.endpoint
	s.mu.RUnlock()
	if emitter == nil {
		return
	}
	for _, ev := range events {
		emitter.MatterEmitEvent(endpoint, closuredef.ClusterID, ev.id, ev.data, ev.priority)
	}
}

// ReportError puts the closure into the Error state with the errors that
// caused it (CurrentErrorList, at most ten) and emits OperationalError. An
// empty list is refused: the event carries at least one error.
func (s *ControlServer) ReportError(errs wire.ClosureErrorList) {
	if len(errs) == 0 {
		return
	}
	if len(errs) > wire.ClosureErrorListMax {
		errs = errs[:wire.ClosureErrorListMax]
	}
	list := make(wire.ClosureErrorList, len(errs))
	copy(list, errs)
	s.mu.Lock()
	s.errorList = list
	s.mainState = wire.ClosureMainStateError
	s.mu.Unlock()
	s.Notify(wire.ClosureControlAttrMainState, wire.ClosureControlAttrCurrentErrorList)
	s.emit([]pendingEvent{{closuredef.EventOperationalError, OperationalErrorEvent{ErrorState: list}, s.inst.EventPriority(closuredef.EventOperationalError)}})
}

// SetEngaged records whether a manually operable closure is engaged
// (MainState leaves or enters Disengaged) and emits EngageStateChanged on a
// change. Without the ManuallyOperable feature it does nothing.
func (s *ControlServer) SetEngaged(engaged bool) {
	if !s.inst.Emits(closuredef.EventEngageStateChanged) {
		return
	}
	s.mu.Lock()
	was := s.mainState != wire.ClosureMainStateDisengaged
	switch {
	case !engaged:
		s.mainState = wire.ClosureMainStateDisengaged
	case s.mainState == wire.ClosureMainStateDisengaged:
		s.mainState = wire.ClosureMainStateStopped
	}
	s.mu.Unlock()
	s.Notify(wire.ClosureControlAttrMainState)
	if was != engaged {
		s.emit([]pendingEvent{{closuredef.EventEngageStateChanged, EngageStateChangedEvent{EngageValue: engaged}, s.inst.EventPriority(closuredef.EventEngageStateChanged)}})
	}
}
