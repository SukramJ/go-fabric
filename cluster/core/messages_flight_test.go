// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	messagesdef "thingfabric.dev/go-fabric/cluster/spec/messages"
	"thingfabric.dev/go-fabric/im"
)

// flightPresenter blocks the PresentMessage of one id until released and
// records every CancelMessage.
type flightPresenter struct {
	mu       sync.Mutex
	block    []byte
	entered  chan struct{}
	release  chan struct{}
	canceled [][]byte
}

func (p *flightPresenter) PresentMessage(_ context.Context, msg messagesdef.MessageStruct) error {
	p.mu.Lock()
	block := p.block != nil && bytes.Equal(msg.MessageId, p.block)
	if block {
		p.block = nil
	}
	p.mu.Unlock()
	if block {
		close(p.entered)
		<-p.release
	}
	return nil
}

func (p *flightPresenter) CancelMessage(_ context.Context, id []byte) error {
	p.mu.Lock()
	p.canceled = append(p.canceled, slices.Clone(id))
	p.mu.Unlock()
	return nil
}

func (p *flightPresenter) canceledCount(id []byte) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.canceled {
		if bytes.Equal(c, id) {
			n++
		}
	}
	return n
}

func flightID(b byte) []byte { id := make([]byte, 16); id[0] = b; return id }

func flightReq(b byte) messagesdef.PresentMessagesRequestRequest {
	return messagesdef.PresentMessagesRequestRequest{
		MessageId: flightID(b), MessageText: "x",
		StartTime: spec.Nullable[uint32]{Null: true}, Duration: spec.Nullable[uint64]{Null: true},
	}
}

// fullQueueWithBlockedRepresent fills the queue with ids 1..8 and starts a
// re-present of id 1 whose presenter blocks; it answers the server, the
// presenter and the re-present's result.
func fullQueueWithBlockedRepresent(t *testing.T) (*Messages, *flightPresenter, <-chan error) {
	t.Helper()
	p := &flightPresenter{entered: make(chan struct{}), release: make(chan struct{})}
	m, err := NewMessages(MessagesConfig{Presenter: p})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := byte(1); i <= messagesMax; i++ {
		if _, err := m.MatterInvoke(ctx, messagesdef.CmdPresentMessagesRequest, flightReq(i)); err != nil {
			t.Fatalf("present %d: %v", i, err)
		}
	}
	p.mu.Lock()
	p.block = flightID(1)
	p.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := m.MatterInvoke(ctx, messagesdef.CmdPresentMessagesRequest, flightReq(1))
		done <- err
	}()
	<-p.entered
	return m, p, done
}

func queuedIDs(m *Messages) [][]byte {
	v, _ := m.MatterRead(messagesdef.AttrMessages)
	list := v.(spec.List[messagesdef.MessageStruct])
	ids := make([][]byte, 0, len(list))
	for _, msg := range list {
		ids = append(ids, msg.MessageId)
	}
	return ids
}

// TestMessagesCancelDuringRepresentWins (R2-C2-conc-1201): a cancel of the
// id that lands while its re-present is at the presenter wins — the
// cancelled message is not queued again, the presenter is told to take it
// down after the present returned, and the queue never exceeds its eight.
func TestMessagesCancelDuringRepresentWins(t *testing.T) {
	t.Parallel()
	m, p, done := fullQueueWithBlockedRepresent(t)
	ctx := context.Background()
	if _, err := m.MatterInvoke(ctx, messagesdef.CmdCancelMessagesRequest, messagesdef.CancelMessagesRequestRequest{MessageIds: [][]byte{flightID(1)}}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := m.MatterInvoke(ctx, messagesdef.CmdPresentMessagesRequest, flightReq(10)); err != nil {
		t.Fatalf("present 10: %v", err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatalf("re-present: %v", err)
	}
	ids := queuedIDs(m)
	if len(ids) > messagesMax {
		t.Errorf("queue holds %d messages, above its %d", len(ids), messagesMax)
	}
	if slices.ContainsFunc(ids, func(id []byte) bool { return bytes.Equal(id, flightID(1)) }) {
		t.Error("the cancelled message is queued again")
	}
	if n := p.canceledCount(flightID(1)); n < 2 {
		t.Errorf("CancelMessage of the id %d times, want the cancel's and the take-down after the re-present", n)
	}
}

// TestMessagesRepresentRechecksTheBound (R2-C2-conc-1201): a re-present
// whose message left the queue while the presenter had it (here the
// host's Complete) and whose slot another present took answers
// RESOURCE_EXHAUSTED and takes its message down, rather than queue a
// ninth.
func TestMessagesRepresentRechecksTheBound(t *testing.T) {
	t.Parallel()
	m, p, done := fullQueueWithBlockedRepresent(t)
	if !m.Complete(flightID(1), MessageOutcome{}) {
		t.Fatal("Complete of a queued id answered false")
	}
	if _, err := m.MatterInvoke(context.Background(), messagesdef.CmdPresentMessagesRequest, flightReq(10)); err != nil {
		t.Fatalf("present 10: %v", err)
	}
	close(p.release)
	err := <-done
	var sc im.StatusCodeError
	if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusResourceExhausted {
		t.Errorf("re-present: %v, want RESOURCE_EXHAUSTED", err)
	}
	if ids := queuedIDs(m); len(ids) > messagesMax {
		t.Errorf("queue holds %d messages, above its %d", len(ids), messagesMax)
	}
	if p.canceledCount(flightID(1)) != 1 {
		t.Error("the refused re-present was not taken down at the presenter")
	}
}
