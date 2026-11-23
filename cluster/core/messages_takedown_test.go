// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	messagesdef "thingfabric.dev/go-fabric/cluster/spec/messages"
)

// displayPresenter records the host's display (the ids it shows) and
// blocks the first PresentMessage, and optionally the first CancelMessage,
// until released.
type displayPresenter struct {
	mu            sync.Mutex
	shown         map[string]bool
	presents      int
	cancels       int
	presentIn     chan struct{}
	presentGo     chan struct{}
	cancelIn      chan struct{} // nil: CancelMessage does not block
	cancelGo      chan struct{}
	presentsAfter chan struct{} // closed by the second PresentMessage
}

func newDisplayPresenter(blockCancel bool) *displayPresenter {
	p := &displayPresenter{
		shown: map[string]bool{}, presentIn: make(chan struct{}), presentGo: make(chan struct{}),
		presentsAfter: make(chan struct{}),
	}
	if blockCancel {
		p.cancelIn, p.cancelGo = make(chan struct{}), make(chan struct{})
	}
	return p
}

func (p *displayPresenter) PresentMessage(_ context.Context, msg messagesdef.MessageStruct) error {
	p.mu.Lock()
	p.presents++
	n := p.presents
	p.mu.Unlock()
	switch n {
	case 1:
		close(p.presentIn)
		<-p.presentGo
	case 2:
		close(p.presentsAfter)
	}
	p.mu.Lock()
	p.shown[string(msg.MessageId)] = true
	p.mu.Unlock()
	return nil
}

func (p *displayPresenter) CancelMessage(_ context.Context, id []byte) error {
	p.mu.Lock()
	p.cancels++
	first := p.cancels == 1 && p.cancelIn != nil
	p.mu.Unlock()
	if first {
		close(p.cancelIn)
		<-p.cancelGo
	}
	p.mu.Lock()
	delete(p.shown, string(id))
	p.mu.Unlock()
	return nil
}

func (p *displayPresenter) isShown(id []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shown[string(id)]
}

func messagesQueued(m *Messages, id []byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.indexLocked(id) >= 0
}

// TestMessagesCancelledPresentLeavesALaterPresent (R3-conc-3101): P1 at
// the presenter, the same fabric cancels the id (SUCCESS), P2 of the same
// id by the same fabric is queued; P1's take-down then must not take P2's
// message down at the presenter. The C3 case — the cancel wins over the
// present it overtook — is TestMessagesCancelDuringRepresentWins.
func TestMessagesCancelledPresentLeavesALaterPresent(t *testing.T) {
	t.Parallel()
	p := newDisplayPresenter(false)
	m, err := NewMessages(MessagesConfig{Presenter: p})
	if err != nil {
		t.Fatal(err)
	}
	id := bytes.Repeat([]byte{7}, 16)
	req := messagesdef.PresentMessagesRequestRequest{MessageId: id}
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- m.present(ctx, 1, req) }()
	<-p.presentIn
	if err := m.cancel(ctx, 1, [][]byte{id}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := m.present(ctx, 1, req); err != nil {
		t.Fatalf("P2: %v", err)
	}
	close(p.presentGo)
	if err := <-done; err != nil {
		t.Fatalf("P1: %v", err)
	}
	if queued, shown := messagesQueued(m, id), p.isShown(id); queued != shown || !queued {
		t.Errorf("after P1, cancel, P2: queued=%v shown at the presenter=%v, want both true", queued, shown)
	}
}

// TestMessagesRemovedPresentLeavesAReusedIndexPresent (R3-conc-3102): P1
// of fabric index 1 at the presenter, fabric 1 removed, a fabric that then
// holds index 1 presents the same id and is queued; P1's take-down must not
// take that message down at the presenter.
func TestMessagesRemovedPresentLeavesAReusedIndexPresent(t *testing.T) {
	t.Parallel()
	p := newDisplayPresenter(false)
	m, err := NewMessages(MessagesConfig{Presenter: p})
	if err != nil {
		t.Fatal(err)
	}
	id := bytes.Repeat([]byte{9}, 16)
	req := messagesdef.PresentMessagesRequestRequest{MessageId: id}
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- m.present(ctx, 1, req) }()
	<-p.presentIn
	m.RemoveFabric(1)
	if err := m.present(ctx, 1, req); err != nil {
		t.Fatalf("P2 (index reused): %v", err)
	}
	close(p.presentGo)
	if err := <-done; err == nil {
		t.Error("P1 of the removed fabric answered SUCCESS")
	}
	if queued, shown := messagesQueued(m, id), p.isShown(id); queued != shown || !queued {
		t.Errorf("queued=%v shown at the presenter=%v, want both true", queued, shown)
	}
}

// TestMessagesPresentWaitsForATakeDown (R3-conc-3101): a present of the id
// that begins while a cancelled present's take-down runs at the presenter
// reaches the presenter only after that cancel returned, so the presenter
// keeps the new message.
func TestMessagesPresentWaitsForATakeDown(t *testing.T) {
	t.Parallel()
	p := newDisplayPresenter(true)
	m, err := NewMessages(MessagesConfig{Presenter: p})
	if err != nil {
		t.Fatal(err)
	}
	id := bytes.Repeat([]byte{5}, 16)
	req := messagesdef.PresentMessagesRequestRequest{MessageId: id}
	ctx := context.Background()
	p1 := make(chan error, 1)
	go func() { p1 <- m.present(ctx, 1, req) }()
	<-p.presentIn
	if err := m.cancel(ctx, 1, [][]byte{id}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	close(p.presentGo)
	<-p.cancelIn // P1's take-down is at the presenter
	p2 := make(chan error, 1)
	go func() { p2 <- m.present(ctx, 1, req) }()
	select {
	case <-p.presentsAfter:
		t.Error("P2 reached the presenter while P1's take-down ran there")
	case <-time.After(50 * time.Millisecond):
	}
	close(p.cancelGo)
	if err := <-p1; err != nil {
		t.Fatalf("P1: %v", err)
	}
	if err := <-p2; err != nil {
		t.Fatalf("P2: %v", err)
	}
	if queued, shown := messagesQueued(m, id), p.isShown(id); !queued || !shown {
		t.Errorf("queued=%v shown at the presenter=%v, want both true", queued, shown)
	}
}
