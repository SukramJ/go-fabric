// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"thingfabric.dev/go-fabric/internal/deepcopy"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	messagesdef "thingfabric.dev/go-fabric/cluster/spec/messages"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// MessagePresenter is the host port of a Messages server: the display
// that shows a queued message and takes it down again.
type MessagePresenter interface {
	// PresentMessage queues msg for display — at once, or at its
	// StartTime. msg carries the accessing fabric's index. An error
	// refuses the command (a [im.StatusCodeError] with its status, any
	// other with FAILURE) and nothing is queued. The host reports what
	// becomes of the message with [Messages.Presented] and
	// [Messages.Complete].
	PresentMessage(ctx context.Context, msg messagesdef.MessageStruct) error
	// CancelMessage takes a queued or presented message down: at a
	// CancelMessagesRequest, and for every queued message of a fabric
	// after the fabric was removed ([Messages.RemoveFabric]). The
	// removal's calls run after the removal, one after the other on a
	// goroutine of the server's, so the fabric's teardown and the
	// command's answer do not wait for them; until a call returned, no
	// other fabric can present a message with that id. A refusal there
	// is logged.
	CancelMessage(ctx context.Context, messageID []byte) error
}

// MessagesConfig carries the construction parameters of a Messages server.
type MessagesConfig struct {
	// Presenter shows the messages; required.
	Presenter MessagePresenter
	// Features is the FeatureMap (CONF, RESP, RPLY, PROT).
	Features messagesdef.Feature
	// DataVersion is an optional host-owned tracker.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// ErrNilPresenter is the error of a Messages server built without its
// presenter.
var ErrNilPresenter = errors.New("core: Messages: a presenter is required")

// messagesMax is the capacity of the Messages list and of
// ActiveMessageIds: the constraint "max 8" (matter.js
// messages.element.ts:32, :36).
const messagesMax = 8

// Messages is a Messages (0x0097) server: the fabric-scoped queue of
// messages controllers present to the user, held by this package because
// the generated server refuses a fabric-scoped attribute
// ([spec.ErrFabricScoped]; Messages is "R F V", messages.element.ts:31-34)
// — Binding's shape, with per-fabric read filtering.
//
// Mirrors matter.js packages/node/src/behaviors/messages/MessagesServer.ts,
// the generated behaviour with no logic of its own. What the model does not
// say is connectedhomeip's at the harness pin
// 6170af8461b10b1766044122ac83332c6d00ab20, read for the elements of
// revision 3:
//
//   - PresentMessagesRequest with Responses but without
//     ConfirmationResponse is INVALID_COMMAND (src/app/clusters/
//     messages-server/messages-server.cpp:359-363); the host's refusal is
//     FAILURE (:392-400).
//   - A message whose MessageID the accessing fabric has queued already
//     replaces it: a presented one completes first (examples/tv-app/
//     tv-common/clusters/messages/MessagesManager.cpp
//     HandlePresentMessagesRequest), and the new one is queued and
//     MessageQueued emitted.
//   - CancelMessagesRequest completes a presented message and drops a
//     queued one; an unknown id is skipped (HandleCancelMessagesRequest).
//   - ActiveMessageIds lists the presented messages
//     (HandleGetActiveMessageIds).
//
// Beyond both: a queue holding its eight messages (the list's "max 8")
// answers a ninth RESOURCE_EXHAUSTED, and a cancel touches only the
// accessing fabric's messages, as only they are visible to it. A present
// whose MessageID another fabric's message holds — queued, or being
// presented — is refused ALREADY_EXISTS (the module's status; neither
// reference answers one here) and that message stays as it is:
// chip's example looks the id up across fabrics and replaces it
// (MessagesManager.cpp:73-87, FindCachedMessage(messageId)), so one
// fabric could take down another's presented message (review II,
// R2-P6-sec-1201, decision 129).
type Messages struct {
	*spec.Instance
	cluster.AttributeChanges

	presenter MessagePresenter
	embedded  cluster.DataVersionTracker
	ext       *cluster.DataVersionTracker
	// log is the logger [Messages.SetLogger] stored; nil is slog.Default().
	log atomic.Pointer[slog.Logger]

	// mu guards the fields below; it is released before a presenter call
	// (present, cancel).
	mu       sync.Mutex
	messages []messagesdef.MessageStruct
	active   [][]byte
	emitter  contract.EventEmitter
	endpoint uint16
	// presenting counts the new messages whose PresentMessage runs, held
	// against the queue's eight so two presents cannot both take its last
	// slot while the presenter has them.
	presenting int
	// inFlight holds the id and fabric of every present whose
	// PresentMessage runs and every cancel whose CancelMessage runs, so
	// another fabric's present of the same id is refused while the
	// presenter has it.
	inFlight []*messagesInFlight
	// pending holds what a change under mu reports once mu is released.
	pendingAttrs  []uint32
	pendingEvents []messagesEvent
	// takeDowns are the removals whose cancels wait for the presenter,
	// in removal order; takingDown is set while takeDownRemoved runs
	// them (removeFabric, R4-fsm-4103, decision 624).
	takeDowns  []messagesTakeDown
	takingDown bool
}

// messagesTakeDown is one removal's take-down at the presenter: the
// flights that hold its ids until each cancel returned.
type messagesTakeDown struct {
	ctx     context.Context
	fabric  uint8
	flights []*messagesInFlight
}

// messagesInFlight is one present or cancel at the presenter; removed
// marks a present whose fabric was removed meanwhile, cancelled a present
// whose id the same fabric cancelled meanwhile (R2-C2-conc-1201).
// takingDown is set, and closed once the presenter's cancel returned,
// while a present that queues nothing takes its message down
// (R3-conc-3101, R3-conc-3102).
type messagesInFlight struct {
	id        []byte
	fabric    uint8
	present   bool
	removed   bool
	cancelled bool
	// inherits: a take-down of the id under this fabric index was
	// skipped in favour of this present, so its refusal takes the
	// display down in that take-down's place (R3-conc-3101, R3-conc-3102).
	inherits   bool
	takingDown chan struct{}
}

// beginFlightLocked records an id at the presenter; endFlightLocked takes
// it back. Caller holds mu.
func (m *Messages) beginFlightLocked(id []byte, fabric uint8) *messagesInFlight {
	f := &messagesInFlight{id: id, fabric: fabric}
	m.inFlight = append(m.inFlight, f)
	return f
}

func (m *Messages) endFlightLocked(f *messagesInFlight) {
	m.inFlight = slices.DeleteFunc(m.inFlight, func(e *messagesInFlight) bool { return e == f })
}

// messagesEvent is an event a change emits once the lock is released.
type messagesEvent struct {
	id   uint32
	data any
}

// Compile-time assertions.
var (
	_ contract.ClusterServer           = (*Messages)(nil)
	_ contract.ClusterDataVersion      = (*Messages)(nil)
	_ contract.ClusterAttributeLister  = (*Messages)(nil)
	_ contract.ClusterCommandLister    = (*Messages)(nil)
	_ contract.ClusterEventLister      = (*Messages)(nil)
	_ contract.FabricScopedReader      = (*Messages)(nil)
	_ contract.AttributeChangeNotifier = (*Messages)(nil)
	_ contract.EventReceiver           = (*Messages)(nil)
	_ contract.FabricScopedState       = (*Messages)(nil)
)

// NewMessages builds a Messages server over cfg.Presenter with an empty
// queue.
func NewMessages(cfg MessagesConfig) (*Messages, error) {
	if cfg.Presenter == nil {
		return nil, ErrNilPresenter
	}
	inst, err := spec.New(messagesdef.Definition, spec.Options{Features: uint32(cfg.Features)})
	if err != nil {
		return nil, fmt.Errorf("core: Messages: %w", err)
	}
	return &Messages{Instance: inst, presenter: cfg.Presenter, ext: cfg.DataVersion}, nil
}

func (m *Messages) tracker() *cluster.DataVersionTracker {
	if m.ext != nil {
		return m.ext
	}
	return &m.embedded
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (m *Messages) MatterDataVersion() uint32 { return m.tracker().Current() }

// MatterRead implements [contract.ClusterServer]: every fabric's messages,
// as a read without a request behind it sees them.
func (m *Messages) MatterRead(attrID uint32) (any, bool) {
	if v, ok := m.ReadGlobal(attrID); ok {
		return v, true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch attrID {
	case messagesdef.AttrMessages:
		return spec.List[messagesdef.MessageStruct](deepcopy.Each(m.messages)), true
	case messagesdef.AttrActiveMessageIds:
		return activeIDs(m.active), true
	}
	return nil, false
}

// activeIDs copies the presented messages' ids; never nil, so an empty
// list encodes as one.
func activeIDs(active [][]byte) [][]byte {
	out := make([][]byte, 0, len(active))
	for _, id := range active {
		out = append(out, slices.Clone(id))
	}
	return out
}

// MatterReadFiltered implements [contract.FabricScopedReader]: a
// fabric-filtered read of Messages lists the accessing fabric's messages
// only — matter.js's fabric-filtered list view (ListManager.ts
// FabricFilteredListProxyHandler), as [Binding.MatterReadFiltered] reads;
// a read without a request every fabric's whole. An unfiltered read lists
// every fabric's, whole: the dispatcher withholds another fabric's
// fabric-sensitive fields from the request, as it scopes every
// fabric-scoped attribute of the model.
func (m *Messages) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	filtered, fabric, request := im.LookupFabricFilter(ctx)
	if attrID != messagesdef.AttrMessages || !request {
		return m.MatterRead(attrID) //nolint:contextcheck // MatterRead is the cluster-interface read without a request; it takes no ctx by contract
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := spec.List[messagesdef.MessageStruct]{}
	for _, msg := range m.messages {
		if !filtered || msg.FabricIndex == fabric {
			out = append(out, msg.Clone())
		}
	}
	return out, true
}

// MatterWrite implements [contract.ClusterServer]: both attributes are
// read-only, refused with the status the definition gives.
func (m *Messages) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := m.ValidateWrite(attrID, value, nil)
	return refusedWrite{sentinel: fmt.Errorf("core: Messages has no writable attribute 0x%04X", attrID), status: err}
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// server stores no written value, so no write changes it.
func (m *Messages) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return false, m.MatterWrite(ctx, attrID, value)
}

// MatterInvoke implements [contract.ClusterServer].
func (m *Messages) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if !m.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("core: Messages: command 0x%02X is not supported", cmdID)
	}
	_, fabric, request := im.LookupFabricFilter(ctx)
	if request && fabric == 0 {
		// The commands are fabric-scoped ("F O", messages.element.ts:67,
		// :84), as Binding refuses a write without a fabric.
		return nil, bindingStatusError{code: im.StatusUnsupportedAccess, msg: "core: Messages: the session has no fabric"}
	}
	switch cmdID {
	case messagesdef.CmdPresentMessagesRequest:
		req, ok := messagesRequest[messagesdef.PresentMessagesRequestRequest](fields)
		if !ok {
			return nil, bindingStatusError{code: im.StatusInvalidCommand, msg: fmt.Sprintf("core: Messages: fields %T", fields)}
		}
		return nil, m.present(ctx, fabric, req)
	case messagesdef.CmdCancelMessagesRequest:
		req, ok := messagesRequest[messagesdef.CancelMessagesRequestRequest](fields)
		if !ok {
			return nil, bindingStatusError{code: im.StatusInvalidCommand, msg: fmt.Sprintf("core: Messages: fields %T", fields)}
		}
		return nil, m.cancel(ctx, fabric, req.MessageIds)
	}
	return nil, im.UnsupportedCommandf("core: Messages: command 0x%02X is not supported", cmdID)
}

// messagesRequest takes the decoded payload as a value or a pointer.
func messagesRequest[T any](fields any) (T, bool) {
	switch v := fields.(type) {
	case *T:
		if v != nil {
			return *v, true
		}
	case T:
		return v, true
	}
	var zero T
	return zero, false
}

// present queues a message for the accessing fabric.
func (m *Messages) present(ctx context.Context, fabric uint8, req messagesdef.PresentMessagesRequestRequest) error {
	if req.Responses != nil && !m.HasFeature("RESP") {
		// messages-server.cpp:359-363.
		return bindingStatusError{code: im.StatusInvalidCommand, msg: "core: Messages: Responses without ConfirmationResponse"}
	}
	req = req.Clone() // the queue keeps its own copy (decision 142)
	msg := messagesdef.MessageStruct{
		MessageId: req.MessageId, Priority: req.Priority, MessageControl: req.MessageControl,
		StartTime: req.StartTime, Duration: req.Duration, MessageText: req.MessageText,
		Responses: req.Responses, FabricIndex: fabric,
	}
	// The presenter is host code: the queue is checked and a slot held
	// under mu, the presenter called without it, the result applied under
	// it again (ADR 0026 P6).
	m.mu.Lock()
	for {
		if m.heldByAnotherLocked(msg.MessageId, fabric) {
			m.unlock()
			return bindingStatusError{code: im.StatusAlreadyExists, msg: "core: Messages: another fabric's message holds the MessageID"}
		}
		// A take-down of the id under this fabric index runs at the
		// presenter: this present waits for it, so the presenter sees the
		// cancel before the present and never takes the new message down
		// (R3-conc-3101, R3-conc-3102).
		done := m.takeDownOfLocked(msg.MessageId, fabric)
		if done == nil {
			break
		}
		m.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return messagesStatus(ctx.Err())
		}
		m.mu.Lock()
	}
	reserved := m.indexLocked(msg.MessageId) < 0
	if reserved && len(m.messages)+m.presenting >= messagesMax {
		m.unlock()
		return bindingStatusError{code: im.StatusResourceExhausted, msg: "core: Messages: the queue holds its eight messages"}
	}
	if reserved {
		m.presenting++
	}
	flight := m.beginFlightLocked(msg.MessageId, fabric)
	flight.present = true
	m.mu.Unlock()
	err := m.presenter.PresentMessage(ctx, msg.Clone())
	m.mu.Lock()
	if reserved {
		m.presenting--
	}
	if err != nil {
		if flight.inherits {
			// A skipped take-down passed the display to this present: the
			// presenter refused it, so the earlier message it shows is
			// taken down here, unless a newer present owns the id by now.
			return m.takeDown(ctx, flight, messagesStatus(err))
		}
		m.endFlightLocked(flight)
		m.unlock()
		return messagesStatus(err)
	}
	switch {
	case flight.removed:
		// The fabric was removed while the presenter had the message: it
		// is not queued under the removed index, and the presenter takes
		// down what it showed. The id stays held until that cancel
		// returned, so another fabric's present of it is refused meanwhile
		// and not taken down by it (review II, R2-P6-sec-1201 rounds 2, 4).
		return m.takeDown(ctx, flight, bindingStatusError{code: im.StatusUnsupportedAccess, msg: fmt.Sprintf("core: Messages: fabric %d was removed while its message was presented", fabric)})
	case flight.cancelled:
		// The same fabric cancelled the id while the presenter had it: the
		// present came first, the cancel after it, so the message is not
		// queued and the presenter takes down what it showed — a cancel
		// answered SUCCESS is not undone by the present it overtook
		// (R2-C2-conc-1201).
		return m.takeDown(ctx, flight, nil)
	case !reserved && m.indexLocked(msg.MessageId) < 0 && len(m.messages)+m.presenting >= messagesMax:
		// A re-present took no slot: its message left the queue while the
		// presenter had it and the slot went to another present. The
		// bound is checked again, not exceeded (R2-C2-conc-1201).
		return m.takeDown(ctx, flight, bindingStatusError{code: im.StatusResourceExhausted, msg: "core: Messages: the queue holds its eight messages"})
	}
	m.endFlightLocked(flight)
	defer m.unlock()
	// A message with the same id replaces the queued one, looked up again:
	// it may have gone or come while the presenter ran. Only the accessing
	// fabric's can be there: another fabric's present of the id was
	// refused while this one was in flight.
	if i := m.indexLocked(msg.MessageId); i >= 0 {
		m.removeLocked(i)
	}
	m.messages = append(m.messages, msg)
	m.changedLocked(messagesdef.AttrMessages, messagesdef.AttrActiveMessageIds)
	m.emitLocked(messagesdef.EventMessageQueued, messagesdef.MessageQueuedEvent{MessageId: slices.Clone(msg.MessageId), FabricIndex: fabric})
	return nil
}

// takeDown answers a present whose message is not queued: the presenter
// takes down what it showed, the flight ending only once that cancel
// returned, and answer is the command's result unless the cancel failed.
// When the id is queued again under the flight's fabric index, or another
// present of it under that index runs at the presenter, the presenter's
// display of the id is that newer present's, and the take-down skips the
// cancel: the queue and the display stay one (R3-conc-3101, a cancelled
// present; R3-conc-3102, a present of a removed fabric whose index a new
// fabric holds). A running present so preferred inherits the take-down:
// should the presenter refuse it, its own take-down runs in this one's
// place (present). A present that begins while the cancel runs waits for
// it (present). Called with mu held; returns with it released.
func (m *Messages) takeDown(ctx context.Context, flight *messagesInFlight, answer error) error {
	if m.supersededLocked(flight) {
		m.endFlightLocked(flight)
		m.unlock()
		return answer
	}
	done := make(chan struct{})
	flight.takingDown = done
	m.unlock()
	cancelErr := m.presenter.CancelMessage(ctx, slices.Clone(flight.id))
	m.mu.Lock()
	m.endFlightLocked(flight)
	close(done)
	m.unlock()
	if cancelErr != nil {
		return messagesStatus(cancelErr)
	}
	return answer
}

// supersededLocked reports whether a newer present owns the presenter's
// display of flight's id: the id is queued under flight's fabric index, or
// another present of it under that index runs that is neither cancelled
// nor removed. Each such running present is marked to inherit the skipped
// take-down (takeDown). Caller holds mu.
func (m *Messages) supersededLocked(flight *messagesInFlight) bool {
	if i := m.indexLocked(flight.id); i >= 0 && m.messages[i].FabricIndex == flight.fabric {
		return true
	}
	superseded := false
	for _, e := range m.inFlight {
		if e != flight && e.present && !e.cancelled && !e.removed && e.takingDown == nil &&
			e.fabric == flight.fabric && bytes.Equal(e.id, flight.id) {
			e.inherits = true
			superseded = true
		}
	}
	return superseded
}

// takeDownOfLocked is the done channel of a take-down of id under fabric
// that runs at the presenter, nil when none does. Caller holds mu.
func (m *Messages) takeDownOfLocked(id []byte, fabric uint8) chan struct{} {
	for _, e := range m.inFlight {
		if e.takingDown != nil && e.fabric == fabric && bytes.Equal(e.id, id) {
			return e.takingDown
		}
	}
	return nil
}

// cancelPresentsLocked marks the accessing fabric's presents of id at the
// presenter as cancelled: each then queues nothing (R2-C2-conc-1201).
// Caller holds mu.
func (m *Messages) cancelPresentsLocked(id []byte, fabric uint8) {
	for _, f := range m.inFlight {
		if f.present && f.fabric == fabric && bytes.Equal(f.id, id) {
			f.cancelled = true
		}
	}
}

// cancel takes the accessing fabric's messages with the given ids down.
func (m *Messages) cancel(ctx context.Context, fabric uint8, ids [][]byte) error {
	// Each id is looked up under mu, cancelled at the presenter without it,
	// and removed under it again if it is still queued (ADR 0026 P6).
	changed := false
	defer func() {
		if changed {
			m.mu.Lock()
			m.changedLocked(messagesdef.AttrMessages, messagesdef.AttrActiveMessageIds)
			m.unlock()
		}
	}()
	for _, id := range ids {
		m.mu.Lock()
		i := m.indexLocked(id)
		visible := i >= 0 && m.messages[i].FabricIndex == fabric
		var flight *messagesInFlight
		var done chan struct{}
		if visible {
			// The id stays the accessing fabric's while the presenter
			// cancels it: another fabric's present of it is refused even
			// if the host completes the message meanwhile, so the cancel
			// never takes that fabric's display down (review II,
			// R2-P6-sec-1201 round 2). The flight is a take-down: the
			// same fabric's present of the id waits for it (present), so
			// the presenter shows that present after the cancel, and the
			// cancel's removal from the queue never undoes it (review II,
			// R4-conc-4101).
			flight = m.beginFlightLocked(id, fabric)
			done = make(chan struct{})
			flight.takingDown = done
		}
		if !visible {
			// Not queued: a present of it at the presenter is the one
			// the cancel takes down, once that present returns.
			m.cancelPresentsLocked(id, fabric)
		}
		m.mu.Unlock()
		if !visible {
			continue
		}
		err := m.presenter.CancelMessage(ctx, slices.Clone(id))
		// The flight ends, the cancel applies and the waiting presents are
		// released in one critical section (R4-conc-4101).
		m.mu.Lock()
		m.endFlightLocked(flight)
		if err == nil {
			// A re-present of the id that runs now must not queue it
			// again once it returns (R2-C2-conc-1201); marked only after
			// the presenter's cancel succeeded, so a refused cancel leaves
			// both as they were.
			m.cancelPresentsLocked(id, fabric)
			if i := m.indexLocked(id); i >= 0 && m.messages[i].FabricIndex == fabric {
				m.removeLocked(i)
				changed = true
			}
		}
		close(done)
		m.mu.Unlock() // the events queued go out with the attributes, below
		if err != nil {
			return messagesStatus(err)
		}
	}
	return nil
}

// messagesStatus is the answer to the presenter's refusal: the status a
// host's contract.StatusError or an im.StatusCodeError names, FAILURE for
// any other error (docs/guides/ports.md).
func messagesStatus(err error) error {
	if spec.CarriesStatus(err) {
		return err
	}
	return bindingStatusError{code: im.StatusFailure, msg: "core: Messages: " + err.Error()}
}

// heldByAnotherLocked reports whether a message of a fabric other than
// fabric holds id: queued, or at the presenter (R2-P6-sec-1201).
func (m *Messages) heldByAnotherLocked(id []byte, fabric uint8) bool {
	if i := m.indexLocked(id); i >= 0 && m.messages[i].FabricIndex != fabric {
		return true
	}
	return slices.ContainsFunc(m.inFlight, func(e *messagesInFlight) bool {
		return e.fabric != fabric && bytes.Equal(e.id, id)
	})
}

// indexLocked is the queue position of id, -1 when it is not queued.
func (m *Messages) indexLocked(id []byte) int {
	return slices.IndexFunc(m.messages, func(msg messagesdef.MessageStruct) bool { return bytes.Equal(msg.MessageId, id) })
}

// removeLocked drops the message at i; a presented one completes with
// every response field null.
func (m *Messages) removeLocked(i int) {
	msg := m.messages[i]
	m.messages = slices.Delete(m.messages, i, i+1)
	if a := slices.IndexFunc(m.active, func(id []byte) bool { return bytes.Equal(id, msg.MessageId) }); a >= 0 {
		m.active = slices.Delete(m.active, a, a+1)
		m.emitLocked(messagesdef.EventMessageComplete, messagesdef.MessageCompleteEvent{
			MessageId:                slices.Clone(msg.MessageId),
			ResponseId:               m.nullResponseID(),
			Reply:                    m.nullReply(),
			FutureMessagesPreference: spec.Nullable[messagesdef.FutureMessagePreferenceEnum]{Null: true},
			FabricIndex:              msg.FabricIndex,
		})
	}
}

// nullResponseID is the MessageComplete ResponseId of a message completed
// without a response: null with ConfirmationResponse (its conformance
// "RESP", messages.element.ts:53), absent without.
func (m *Messages) nullResponseID() *spec.Nullable[uint32] {
	if !m.HasFeature("RESP") {
		return nil
	}
	return &spec.Nullable[uint32]{Null: true}
}

// nullReply is the MessageComplete Reply of a message completed without a
// reply: null with ConfirmationReply ("RPLY", :54-57), absent without.
func (m *Messages) nullReply() *spec.Nullable[string] {
	if !m.HasFeature("RPLY") {
		return nil
	}
	return &spec.Nullable[string]{Null: true}
}

// changedLocked bumps the data version; attrIDs are reported once the
// lock is released (unlock).
func (m *Messages) changedLocked(attrIDs ...uint32) {
	m.tracker().Bump()
	for _, id := range attrIDs {
		if !slices.Contains(m.pendingAttrs, id) {
			m.pendingAttrs = append(m.pendingAttrs, id)
		}
	}
}

// emitLocked queues eventID; it is emitted, with the definition's
// priority, once the lock is released (unlock).
func (m *Messages) emitLocked(eventID uint32, data any) {
	m.pendingEvents = append(m.pendingEvents, messagesEvent{id: eventID, data: data})
}

// unlock releases mu, then reports the changes and emits the events the
// critical section queued, outside the lock as [spec.Server] does.
func (m *Messages) unlock() {
	attrs, evs := m.pendingAttrs, m.pendingEvents
	m.pendingAttrs, m.pendingEvents = nil, nil
	emitter, endpoint := m.emitter, m.endpoint
	m.mu.Unlock()
	slices.Sort(attrs)
	m.Notify(attrs...)
	if emitter == nil {
		return
	}
	for _, ev := range evs {
		emitter.MatterEmitEvent(endpoint, messagesdef.ClusterID, ev.id, ev.data, m.EventPriority(ev.id))
	}
}

// Presented records that the display shows the message id now: it joins
// ActiveMessageIds and MessagePresented is emitted. An id not queued, or
// presented already, changes nothing and answers false.
func (m *Messages) Presented(id []byte) bool {
	m.mu.Lock()
	defer m.unlock()
	i := m.indexLocked(id)
	if i < 0 || slices.ContainsFunc(m.active, func(a []byte) bool { return bytes.Equal(a, id) }) {
		return false
	}
	m.active = append(m.active, slices.Clone(id))
	m.changedLocked(messagesdef.AttrActiveMessageIds)
	m.emitLocked(messagesdef.EventMessagePresented, messagesdef.MessagePresentedEvent{MessageId: slices.Clone(id), FabricIndex: m.messages[i].FabricIndex})
	return true
}

// MessageOutcome is how a presented message ended: the user's response,
// reply and preference for future messages, each nil when not given.
type MessageOutcome struct {
	ResponseID               *uint32
	Reply                    *string
	FutureMessagesPreference *messagesdef.FutureMessagePreferenceEnum
}

// Complete records that the message id is done — confirmed, answered, or
// its Duration over: it leaves the queue and, when presented,
// MessageComplete is emitted with outcome. An id not queued answers false.
func (m *Messages) Complete(id []byte, outcome MessageOutcome) bool {
	m.mu.Lock()
	defer m.unlock()
	i := m.indexLocked(id)
	if i < 0 {
		return false
	}
	msg := m.messages[i]
	m.messages = slices.Delete(m.messages, i, i+1)
	if a := slices.IndexFunc(m.active, func(x []byte) bool { return bytes.Equal(x, id) }); a >= 0 {
		m.active = slices.Delete(m.active, a, a+1)
		ev := messagesdef.MessageCompleteEvent{
			MessageId: slices.Clone(id), ResponseId: m.nullResponseID(), Reply: m.nullReply(),
			FutureMessagesPreference: spec.Nullable[messagesdef.FutureMessagePreferenceEnum]{Null: true},
			FabricIndex:              msg.FabricIndex,
		}
		if outcome.ResponseID != nil && ev.ResponseId != nil {
			ev.ResponseId = &spec.Nullable[uint32]{Value: *outcome.ResponseID}
		}
		if outcome.Reply != nil && ev.Reply != nil {
			ev.Reply = &spec.Nullable[string]{Value: *outcome.Reply}
		}
		if outcome.FutureMessagesPreference != nil {
			ev.FutureMessagesPreference = spec.Nullable[messagesdef.FutureMessagePreferenceEnum]{Value: *outcome.FutureMessagesPreference}
		}
		m.emitLocked(messagesdef.EventMessageComplete, ev)
	}
	m.changedLocked(messagesdef.AttrMessages, messagesdef.AttrActiveMessageIds)
	return true
}

// RemoveFabric drops a removed fabric's messages, as matter.js strips a
// departed fabric's fabric-scoped attribute entries
// (packages/node/src/behavior/cluster/FabricScopedDataHandler.ts:41-73,
// the filter at :62-68, and :114-123;
// [Binding.RemoveFabric]). No event is emitted: the fabric that would
// read it is gone.
//
// The presenter takes each dropped message down ([MessagePresenter]
// CancelMessage, once per message), so the display and the queue agree as
// they do for a removed fabric's present still at the presenter. The
// cancels run after the drop, off the removal path: RemoveFabric returns
// once the queue dropped the messages, and a goroutine of the server's
// runs the removals' cancels one after the other, so a presenter that
// blocks stalls neither the fabric's teardown nor the command's answer
// (decision 624). Until a cancel returned its id stays held: another
// fabric's present of it is refused meanwhile, so the cancel never takes
// that fabric's display down (review II, R4-fsm-4103, decision 598).
func (m *Messages) RemoveFabric(fabricIndex uint8) {
	m.removeFabric(context.Background(), fabricIndex)
}

// removeFabric is RemoveFabric with ctx, without its cancellation, for
// the presenter's cancels (takeDownRemoved).
func (m *Messages) removeFabric(ctx context.Context, fabricIndex uint8) {
	m.mu.Lock()
	for _, f := range m.inFlight {
		if f.fabric == fabricIndex {
			f.removed = true
		}
	}
	before := len(m.messages)
	var gone [][]byte
	m.messages = slices.DeleteFunc(m.messages, func(msg messagesdef.MessageStruct) bool {
		if msg.FabricIndex == fabricIndex {
			gone = append(gone, msg.MessageId)
			return true
		}
		return false
	})
	if len(m.messages) == before {
		m.unlock()
		return
	}
	m.active = slices.DeleteFunc(m.active, func(id []byte) bool {
		return slices.ContainsFunc(gone, func(g []byte) bool { return bytes.Equal(g, id) })
	})
	m.changedLocked(messagesdef.AttrMessages, messagesdef.AttrActiveMessageIds)
	td := messagesTakeDown{ctx: context.WithoutCancel(ctx), fabric: fabricIndex, flights: make([]*messagesInFlight, len(gone))}
	for i, id := range gone {
		td.flights[i] = m.beginFlightLocked(id, fabricIndex)
		td.flights[i].takingDown = make(chan struct{})
	}
	m.takeDowns = append(m.takeDowns, td)
	start := !m.takingDown
	m.takingDown = true
	m.unlock()
	if start {
		go m.takeDownRemoved()
	}
}

// takeDownRemoved runs the queued removals' cancels at the presenter, one
// after the other, and ends each id's flight once its cancel returned; it
// returns when no removal waits. A refusal or a panic of the presenter is
// logged and ends the flight all the same: the queue's drop stands and
// nothing is held for a retry.
func (m *Messages) takeDownRemoved() {
	for {
		m.mu.Lock()
		if len(m.takeDowns) == 0 {
			m.takingDown = false
			m.mu.Unlock()
			return
		}
		td := m.takeDowns[0]
		m.takeDowns = slices.Delete(m.takeDowns, 0, 1)
		m.mu.Unlock()
		for _, f := range td.flights {
			if err := m.cancelRemoved(td.ctx, f.id); err != nil {
				m.logger().Warn("matter.messages.take_down_refused",
					slog.Int("fabric_index", int(td.fabric)), slog.String("message_id", hex.EncodeToString(f.id)), slog.Any("err", err))
			}
			m.mu.Lock()
			m.endFlightLocked(f)
			close(f.takingDown)
			m.mu.Unlock()
		}
	}
}

// SetLogger routes the server's diagnostics (the presenter's refused
// take-down of a removed fabric's message) to l; nil restores
// slog.Default(). Safe at any time (ADR 0026 P9, decision 628).
func (m *Messages) SetLogger(l *slog.Logger) { m.log.Store(l) }

// logger is the server's logger; a value without SetLogger logs through
// slog.Default().
func (m *Messages) logger() *slog.Logger {
	if l := m.log.Load(); l != nil {
		return l
	}
	return slog.Default()
}

// cancelRemoved is the presenter's CancelMessage of a removed fabric's
// message, a panic answered as an error.
func (m *Messages) cancelRemoved(ctx context.Context, id []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("core: Messages: presenter panicked: %v", r)
		}
	}()
	return m.presenter.CancelMessage(ctx, slices.Clone(id))
}

// MatterDropFabric implements [contract.FabricScopedState]
// ([Messages.RemoveFabric]): the queue is memory only, so the drop always
// lands and answers nil. The presenter's take-down runs after it.
func (m *Messages) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	m.removeFabric(ctx, fabricIndex)
	return nil
}

// MatterHeldFabrics implements [contract.FabricScopedState]: the fabrics
// of the queued messages.
func (m *Messages) MatterHeldFabrics() []uint8 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fabricsOf(m.messages, func(msg messagesdef.MessageStruct) uint8 { return msg.FabricIndex })
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (m *Messages) SetMatterEventEmitter(emitter contract.EventEmitter) {
	m.mu.Lock()
	m.emitter = emitter
	m.mu.Unlock()
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it when it mounts the server.
func (m *Messages) SetEndpoint(endpoint uint16) {
	m.mu.Lock()
	m.endpoint = endpoint
	m.mu.Unlock()
}
