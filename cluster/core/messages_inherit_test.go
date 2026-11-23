// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	messagesdef "thingfabric.dev/go-fabric/cluster/spec/messages"
)

// refusingPresenter blocks the first two PresentMessage calls until
// released; the first then shows its message, the second refuses it.
type refusingPresenter struct {
	mu       sync.Mutex
	shown    map[string]bool
	presents int
	cancels  int
	in       [2]chan struct{}
	release  [2]chan struct{}
}

func newRefusingPresenter() *refusingPresenter {
	p := &refusingPresenter{shown: map[string]bool{}}
	for i := range p.in {
		p.in[i], p.release[i] = make(chan struct{}), make(chan struct{})
	}
	return p
}

func (p *refusingPresenter) PresentMessage(_ context.Context, msg messagesdef.MessageStruct) error {
	p.mu.Lock()
	p.presents++
	n := p.presents
	p.mu.Unlock()
	if n <= 2 {
		close(p.in[n-1])
		<-p.release[n-1]
	}
	if n == 2 {
		return errors.New("host refuses the message")
	}
	p.mu.Lock()
	p.shown[string(msg.MessageId)] = true
	p.mu.Unlock()
	return nil
}

func (p *refusingPresenter) CancelMessage(_ context.Context, id []byte) error {
	p.mu.Lock()
	p.cancels++
	delete(p.shown, string(id))
	p.mu.Unlock()
	return nil
}

func (p *refusingPresenter) state(id []byte) (shown bool, cancels int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shown[string(id)], p.cancels
}

// TestMessagesRefusedPresentInheritsTheTakeDown (R3-conc-3101/3102, the
// Fable diff read's B1): P1 at the presenter, its fabric cancels the id or
// is removed, P2 of the id under the same fabric index reaches the
// presenter; P1 returns and its take-down is skipped in favour of P2; P2 is
// then refused. P2 inherits the skipped take-down, so P1's message does not
// stay on the display while nothing queues it.
func TestMessagesRefusedPresentInheritsTheTakeDown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		trigger func(ctx context.Context, m *Messages, id []byte) error
		p1Fails bool
	}{
		{"cancel", func(ctx context.Context, m *Messages, id []byte) error {
			return m.cancel(ctx, 1, [][]byte{id})
		}, false},
		{"fabric removal", func(_ context.Context, m *Messages, _ []byte) error {
			m.RemoveFabric(1)
			return nil
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newRefusingPresenter()
			m, err := NewMessages(MessagesConfig{Presenter: p})
			if err != nil {
				t.Fatal(err)
			}
			id := bytes.Repeat([]byte{3}, 16)
			req := messagesdef.PresentMessagesRequestRequest{MessageId: id}
			ctx := context.Background()
			p1, p2 := make(chan error, 1), make(chan error, 1)
			go func() { p1 <- m.present(ctx, 1, req) }()
			<-p.in[0]
			if err := tc.trigger(ctx, m, id); err != nil {
				t.Fatalf("trigger: %v", err)
			}
			go func() { p2 <- m.present(ctx, 1, req) }()
			<-p.in[1]
			close(p.release[0])
			if err := <-p1; (err != nil) != tc.p1Fails {
				t.Fatalf("P1: %v, want failure %v", err, tc.p1Fails)
			}
			close(p.release[1])
			if err := <-p2; err == nil {
				t.Fatal("P2 refused at the presenter answered SUCCESS")
			}
			shown, cancels := p.state(id)
			if queued := messagesQueued(m, id); queued || shown {
				t.Errorf("queued=%v shown at the presenter=%v cancels=%d, want neither", queued, shown, cancels)
			}
		})
	}
}
