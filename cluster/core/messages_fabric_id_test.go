// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	messagesdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
	"github.com/SukramJ/go-fabric/im"
)

// TestMessagesRefusesAnotherFabricsID (review II, R2-P6-sec-1201, decision
// 129): a PresentMessagesRequest whose MessageID another fabric's message
// holds is refused ALREADY_EXISTS, and that message stays queued and
// presented — no MessageComplete for it, nothing shown. The accessing
// fabric's own id still replaces its own message.
func TestMessagesRefusesAnotherFabricsID(t *testing.T) {
	t.Parallel()
	m, d, ev := newMessages(t, 0)
	id := messageID(5)
	if err := decodeAndInvoke(t, m, msgCtx(1, true), messagesdef.CmdPresentMessagesRequest, presentRequest(id, "from fabric 1")); err != nil {
		t.Fatal(err)
	}
	if !m.Presented(id) {
		t.Fatal("Presented")
	}
	shown, events := len(d.shown), len(ev.ids)
	err := decodeAndInvoke(t, m, msgCtx(2, true), messagesdef.CmdPresentMessagesRequest, presentRequest(id, "from fabric 2"))
	wantCoreStatus(t, err, im.StatusAlreadyExists)
	if len(d.shown) != shown || len(ev.ids) != events {
		t.Errorf("refused present reached the presenter (%d) or emitted %v", len(d.shown)-shown, ev.ids[events:])
	}
	v, _ := m.MatterRead(messagesdef.AttrMessages)
	if l := v.(spec.List[messagesdef.MessageStruct]); len(l) != 1 || l[0].FabricIndex != 1 || l[0].MessageText != "from fabric 1" {
		t.Errorf("Messages %+v", l)
	}
	if a, _ := m.MatterRead(messagesdef.AttrActiveMessageIds); len(a.([][]byte)) != 1 {
		t.Errorf("ActiveMessageIds %v", a)
	}
	// The owner's own replace is unchanged.
	if err := decodeAndInvoke(t, m, msgCtx(1, true), messagesdef.CmdPresentMessagesRequest, presentRequest(id, "again")); err != nil {
		t.Fatal(err)
	}
	v, _ = m.MatterRead(messagesdef.AttrMessages)
	if l := v.(spec.List[messagesdef.MessageStruct]); len(l) != 1 || l[0].MessageText != "again" {
		t.Errorf("Messages %+v after the owner's replace", l)
	}
}

// blockingDisplay holds every PresentMessage until release is closed.
type blockingDisplay struct {
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	shown   [][]byte
}

func (b *blockingDisplay) PresentMessage(_ context.Context, msg messagesdef.MessageStruct) error {
	b.entered <- struct{}{}
	<-b.release
	b.mu.Lock()
	b.shown = append(b.shown, msg.MessageId)
	b.mu.Unlock()
	return nil
}

func (b *blockingDisplay) CancelMessage(context.Context, []byte) error { return nil }

// TestMessagesRefusesAnIDAnotherFabricIsPresenting (R2-P6-sec-1201): while
// fabric 1's present of an id runs at the presenter, fabric 2's present of
// the same id is refused before it reaches the presenter.
func TestMessagesRefusesAnIDAnotherFabricIsPresenting(t *testing.T) {
	t.Parallel()
	b := &blockingDisplay{entered: make(chan struct{}, 2), release: make(chan struct{})}
	m, err := core.NewMessages(core.MessagesConfig{Presenter: b})
	if err != nil {
		t.Fatal(err)
	}
	id := messageID(6)
	done := make(chan error, 1)
	go func() {
		done <- decodeAndInvoke(t, m, msgCtx(1, true), messagesdef.CmdPresentMessagesRequest, presentRequest(id, "one"))
	}()
	<-b.entered
	second := make(chan error, 1)
	go func() {
		second <- decodeAndInvoke(t, m, msgCtx(2, true), messagesdef.CmdPresentMessagesRequest, presentRequest(id, "two"))
	}()
	select {
	case err := <-second:
		wantCoreStatus(t, err, im.StatusAlreadyExists)
		close(b.release)
	case <-b.entered:
		t.Error("fabric 2's present reached the presenter")
		close(b.release)
		<-second
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.shown) != 1 || !bytes.Equal(b.shown[0], id) {
		t.Errorf("shown %v", b.shown)
	}
	v, _ := m.MatterRead(messagesdef.AttrMessages)
	if l := v.(spec.List[messagesdef.MessageStruct]); len(l) != 1 || l[0].FabricIndex != 1 || !slices.Equal(l[0].MessageId, id) {
		t.Errorf("Messages %+v", l)
	}
}
