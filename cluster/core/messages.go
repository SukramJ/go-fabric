// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	messagesdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
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
	// CancelMessage takes a queued or presented message down.
	CancelMessage(ctx context.Context, messageID []byte) error
}

// MessagesConfig carries the construction parameters of a Messages server.
type MessagesConfig struct {
	// Presenter shows the messages; required.
	Presenter MessagePresenter
	// Features is the FeatureMap (CONF, RESP, RPLY, PROT).
	Features messagesdef.Feature
	// DataVersion is an optional host-owned tracker.
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
//   - A message whose MessageID is queued already replaces it: a presented
//     one completes first (examples/tv-app/tv-common/clusters/messages/
//     MessagesManager.cpp HandlePresentMessagesRequest), and the new one is
//     queued and MessageQueued emitted.
//   - CancelMessagesRequest completes a presented message and drops a
//     queued one; an unknown id is skipped (HandleCancelMessagesRequest).
//   - ActiveMessageIds lists the presented messages
//     (HandleGetActiveMessageIds).
//
// Beyond both: a queue holding its eight messages (the list's "max 8")
// answers a ninth RESOURCE_EXHAUSTED, and a cancel touches only the
// accessing fabric's messages, as only they are visible to it.
type Messages struct {
	*spec.Instance
	cluster.AttributeChanges

	presenter MessagePresenter
	embedded  cluster.DataVersionTracker
	ext       *cluster.DataVersionTracker

	mu       sync.Mutex
	messages []messagesdef.MessageStruct
	active   [][]byte
	emitter  contract.EventEmitter
	endpoint uint16
	// pending holds what a change under mu reports once mu is released.
	pendingAttrs  []uint32
	pendingEvents []messagesEvent
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
		return spec.List[messagesdef.MessageStruct](slices.Clone(m.messages)), true
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
// only; an unfiltered one, and a read without a request, every fabric's —
// matter.js's fabric-filtered list view (ListManager.ts
// FabricFilteredListProxyHandler), as [Binding.MatterReadFiltered] reads.
func (m *Messages) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	filtered, fabric, request := im.LookupFabricFilter(ctx)
	if attrID != messagesdef.AttrMessages || !request || !filtered {
		return m.MatterRead(attrID) //nolint:contextcheck // MatterRead is the cluster-interface read without a request; it takes no ctx by contract
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := spec.List[messagesdef.MessageStruct]{}
	for _, msg := range m.messages {
		if msg.FabricIndex == fabric {
			out = append(out, msg)
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
	msg := messagesdef.MessageStruct{
		MessageId: slices.Clone(req.MessageId), Priority: req.Priority, MessageControl: req.MessageControl,
		StartTime: req.StartTime, Duration: req.Duration, MessageText: req.MessageText,
		Responses: req.Responses, FabricIndex: fabric,
	}
	m.mu.Lock()
	defer m.unlock()
	i := m.indexLocked(msg.MessageId)
	if i < 0 && len(m.messages) >= messagesMax {
		return bindingStatusError{code: im.StatusResourceExhausted, msg: "core: Messages: the queue holds its eight messages"}
	}
	if err := m.presenter.PresentMessage(ctx, msg); err != nil {
		return messagesStatus(err)
	}
	if i >= 0 {
		m.removeLocked(i)
	}
	m.messages = append(m.messages, msg)
	m.changedLocked(messagesdef.AttrMessages, messagesdef.AttrActiveMessageIds)
	m.emitLocked(messagesdef.EventMessageQueued, messagesdef.MessageQueuedEvent{MessageId: slices.Clone(msg.MessageId), FabricIndex: fabric})
	return nil
}

// cancel takes the accessing fabric's messages with the given ids down.
func (m *Messages) cancel(ctx context.Context, fabric uint8, ids [][]byte) error {
	m.mu.Lock()
	defer m.unlock()
	changed := false
	for _, id := range ids {
		i := m.indexLocked(id)
		if i < 0 || m.messages[i].FabricIndex != fabric {
			continue
		}
		if err := m.presenter.CancelMessage(ctx, slices.Clone(id)); err != nil {
			return messagesStatus(err)
		}
		m.removeLocked(i)
		changed = true
	}
	if changed {
		m.changedLocked(messagesdef.AttrMessages, messagesdef.AttrActiveMessageIds)
	}
	return nil
}

// messagesStatus keeps the status of an [im.StatusCodeError] and answers
// any other error FAILURE.
func messagesStatus(err error) error {
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return err
	}
	return bindingStatusError{code: im.StatusFailure, msg: "core: Messages: " + err.Error()}
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
// (packages/node/src/behavior/cluster/FabricScopedDataHandler.ts:109-122;
// [Binding.RemoveFabric]). No event is emitted: the fabric that would
// read it is gone.
func (m *Messages) RemoveFabric(fabricIndex uint8) {
	m.mu.Lock()
	defer m.unlock()
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
		return
	}
	m.active = slices.DeleteFunc(m.active, func(id []byte) bool {
		return slices.ContainsFunc(gone, func(g []byte) bool { return bytes.Equal(g, id) })
	})
	m.changedLocked(messagesdef.AttrMessages, messagesdef.AttrActiveMessageIds)
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
