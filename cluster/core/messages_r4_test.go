// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	messagesdef "thingfabric.dev/go-fabric/cluster/spec/messages"
	"thingfabric.dev/go-fabric/im"
)

// r4DisplayPresenter models the display: a present shows the id, a cancel
// takes it down. The cancel of blockCancel applies, then blocks until
// release; cancels counts every CancelMessage per id.
type r4DisplayPresenter struct {
	mu          sync.Mutex
	shown       map[string]bool
	cancels     map[string]int
	blockCancel []byte
	entered     chan struct{}
	release     chan struct{}
}

func newR4DisplayPresenter() *r4DisplayPresenter {
	return &r4DisplayPresenter{shown: map[string]bool{}, cancels: map[string]int{}, entered: make(chan struct{}), release: make(chan struct{})}
}

func (p *r4DisplayPresenter) PresentMessage(_ context.Context, msg messagesdef.MessageStruct) error {
	p.mu.Lock()
	p.shown[string(msg.MessageId)] = true
	p.mu.Unlock()
	return nil
}

func (p *r4DisplayPresenter) CancelMessage(_ context.Context, id []byte) error {
	p.mu.Lock()
	delete(p.shown, string(id))
	p.cancels[string(id)]++
	block := p.blockCancel != nil && bytes.Equal(id, p.blockCancel)
	if block {
		p.blockCancel = nil
	}
	p.mu.Unlock()
	if block {
		close(p.entered)
		<-p.release
	}
	return nil
}

func (p *r4DisplayPresenter) isShown(id []byte) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shown[string(id)]
}

func (p *r4DisplayPresenter) cancelsOf(id []byte) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancels[string(id)]
}

func isQueued(m *Messages, id []byte) bool {
	return slices.ContainsFunc(queuedIDs(m), func(q []byte) bool { return bytes.Equal(q, id) })
}

// TestMessagesRepresentWaitsForAVisibleCancel: a fabric's cancel of its
// queued id is at the presenter (the display is down) when the same fabric
// presents the id again: the present waits for the cancel, so it is shown
// and queued after it, and the queue and the display agree (review II,
// R4-conc-4101).
func TestMessagesRepresentWaitsForAVisibleCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newR4DisplayPresenter()
		m, err := NewMessages(MessagesConfig{Presenter: p})
		if err != nil {
			t.Fatal(err)
		}
		ctx := im.WithFabricFilter(context.Background(), true, 1)
		if _, err := m.MatterInvoke(ctx, messagesdef.CmdPresentMessagesRequest, flightReq(1)); err != nil {
			t.Fatal(err)
		}
		p.mu.Lock()
		p.blockCancel = flightID(1)
		p.mu.Unlock()
		cancelDone := make(chan error, 1)
		go func() {
			_, err := m.MatterInvoke(ctx, messagesdef.CmdCancelMessagesRequest, messagesdef.CancelMessagesRequestRequest{MessageIds: [][]byte{flightID(1)}})
			cancelDone <- err
		}()
		<-p.entered
		presentDone := make(chan error, 1)
		go func() {
			_, err := m.MatterInvoke(ctx, messagesdef.CmdPresentMessagesRequest, flightReq(1))
			presentDone <- err
		}()
		synctest.Wait()
		close(p.release)
		if err := <-cancelDone; err != nil {
			t.Errorf("cancel: %v", err)
		}
		if err := <-presentDone; err != nil {
			t.Errorf("re-present: %v", err)
		}
		queued, shown := isQueued(m, flightID(1)), p.isShown(flightID(1))
		if queued != shown || !queued {
			t.Errorf("after the cancel and the re-present: queued=%v shown=%v, want both true", queued, shown)
		}
	})
}

// TestMessagesRemoveFabricTakesItsMessagesDown: a removed fabric's queued
// messages, presented or not, are taken down at the presenter, each once;
// another fabric's stays (review II, R4-fsm-4103, decision 598). The
// take-down runs after the drop (decision 624): the bubble waits for it.
func TestMessagesRemoveFabricTakesItsMessagesDown(t *testing.T) {
	synctest.Test(t, testMessagesRemoveFabricTakesItsMessagesDown)
}

func testMessagesRemoveFabricTakesItsMessagesDown(t *testing.T) {
	p := newR4DisplayPresenter()
	m, err := NewMessages(MessagesConfig{Presenter: p})
	if err != nil {
		t.Fatal(err)
	}
	ctx1 := im.WithFabricFilter(context.Background(), true, 1)
	ctx2 := im.WithFabricFilter(context.Background(), true, 2)
	for _, c := range []struct {
		ctx context.Context
		id  byte
	}{{ctx1, 1}, {ctx1, 2}, {ctx2, 3}} {
		if _, err := m.MatterInvoke(c.ctx, messagesdef.CmdPresentMessagesRequest, flightReq(c.id)); err != nil {
			t.Fatal(err)
		}
	}
	m.Presented(flightID(1))
	if err := m.MatterDropFabric(context.Background(), 1); err != nil {
		t.Fatalf("drop: %v", err)
	}
	synctest.Wait()
	for _, id := range []byte{1, 2} {
		if isQueued(m, flightID(id)) || p.isShown(flightID(id)) {
			t.Errorf("message %d of the removed fabric: queued=%v shown=%v, want neither", id, isQueued(m, flightID(id)), p.isShown(flightID(id)))
		}
		if n := p.cancelsOf(flightID(id)); n != 1 {
			t.Errorf("message %d cancelled at the presenter %d times, want 1", id, n)
		}
	}
	if !isQueued(m, flightID(3)) || !p.isShown(flightID(3)) || p.cancelsOf(flightID(3)) != 0 {
		t.Error("the other fabric's message was taken down")
	}
	if err := m.MatterDropFabric(context.Background(), 1); err != nil {
		t.Fatalf("second drop: %v", err)
	}
	synctest.Wait()
	if n := p.cancelsOf(flightID(1)); n != 1 {
		t.Errorf("a second drop cancelled again: %d", n)
	}
}

// TestMessagesRemoveFabricHoldsTheIDsUntilTheCancelReturned: while the
// removal's cancel of an id is at the presenter, another fabric's present
// of that id is refused, so the cancel never takes that fabric's display
// down (as takeDown, R2-P6-sec-1201 rounds 2, 4).
func TestMessagesRemoveFabricHoldsTheIDsUntilTheCancelReturned(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newR4DisplayPresenter()
		m, err := NewMessages(MessagesConfig{Presenter: p})
		if err != nil {
			t.Fatal(err)
		}
		ctx1 := im.WithFabricFilter(context.Background(), true, 1)
		ctx2 := im.WithFabricFilter(context.Background(), true, 2)
		if _, err := m.MatterInvoke(ctx1, messagesdef.CmdPresentMessagesRequest, flightReq(1)); err != nil {
			t.Fatal(err)
		}
		p.mu.Lock()
		p.blockCancel = flightID(1)
		p.mu.Unlock()
		dropDone := make(chan error, 1)
		go func() { dropDone <- m.MatterDropFabric(context.Background(), 1) }()
		<-p.entered
		_, err = m.MatterInvoke(ctx2, messagesdef.CmdPresentMessagesRequest, flightReq(1))
		close(p.release)
		if err := <-dropDone; err != nil {
			t.Fatalf("drop: %v", err)
		}
		synctest.Wait() // the take-down runs after the drop (decision 624)
		if err == nil {
			t.Error("another fabric's present of the id answered SUCCESS while the removal's cancel ran")
		}
		if isQueued(m, flightID(1)) != p.isShown(flightID(1)) {
			t.Errorf("queue and display disagree: queued=%v shown=%v", isQueued(m, flightID(1)), p.isShown(flightID(1)))
		}
	})
}

// TestMessagesRemoveFabricDoesNotWaitForThePresenter: the removal's
// take-down runs after the drop, off the removal path — a presenter whose
// CancelMessage blocks delays neither RemoveFabric nor MatterDropFabric,
// so the fabric's teardown and the NOCResponse do not wait for the host's
// display; the id stays held against another fabric until the cancel
// returned, and the cancel still runs once (review II, R4-fsm-4103,
// fable-diff-h2.md N1, decision 624).
func TestMessagesRemoveFabricDoesNotWaitForThePresenter(t *testing.T) {
	for _, via := range []string{"MatterDropFabric", "RemoveFabric"} {
		t.Run(via, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newR4DisplayPresenter()
				m, err := NewMessages(MessagesConfig{Presenter: p})
				if err != nil {
					t.Fatal(err)
				}
				ctx1 := im.WithFabricFilter(context.Background(), true, 1)
				ctx2 := im.WithFabricFilter(context.Background(), true, 2)
				if _, err := m.MatterInvoke(ctx1, messagesdef.CmdPresentMessagesRequest, flightReq(1)); err != nil {
					t.Fatal(err)
				}
				p.mu.Lock()
				p.blockCancel = flightID(1)
				p.mu.Unlock()
				returned := make(chan error, 1)
				go func() {
					if via == "RemoveFabric" {
						m.RemoveFabric(1)
						returned <- nil
						return
					}
					returned <- m.MatterDropFabric(context.Background(), 1)
				}()
				<-p.entered
				synctest.Wait()
				select {
				case err := <-returned:
					if err != nil {
						t.Errorf("drop: %v", err)
					}
				default:
					close(p.release)
					t.Fatalf("%s has not returned while the presenter's cancel blocks; want it to return after the drop", via)
				}
				if isQueued(m, flightID(1)) {
					t.Error("the removed fabric's message is still queued after the drop returned")
				}
				if _, err := m.MatterInvoke(ctx2, messagesdef.CmdPresentMessagesRequest, flightReq(1)); err == nil {
					t.Error("another fabric's present of the id answered SUCCESS while the removal's cancel ran")
				}
				close(p.release)
				synctest.Wait()
				if n := p.cancelsOf(flightID(1)); n != 1 {
					t.Errorf("cancelled at the presenter %d times, want 1", n)
				}
				if _, err := m.MatterInvoke(ctx2, messagesdef.CmdPresentMessagesRequest, flightReq(1)); err != nil {
					t.Errorf("another fabric's present after the cancel returned: %v, want SUCCESS", err)
				}
			})
		})
	}
}
