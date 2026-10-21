// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	messagesdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
)

// readingPresenter reads its server from inside every call, as a host's
// display that shows the queue it is asked to change would.
type readingPresenter struct {
	m     *mattercore.Messages
	reads int
}

func (p *readingPresenter) read() {
	done := make(chan struct{})
	go func() {
		p.m.MatterRead(messagesdef.AttrMessages)
		close(done)
	}()
	select {
	case <-done:
		p.reads++
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		panic("the Messages server holds its lock across the presenter call")
	}
}

func (p *readingPresenter) PresentMessage(context.Context, messagesdef.MessageStruct) error {
	p.read()
	return nil
}

func (p *readingPresenter) CancelMessage(context.Context, []byte) error {
	p.read()
	return nil
}

// TestMessagesPresenterRunsWithoutTheLock pins G1-16 (ADR 0026 P6): the
// presenter is host code and runs with the server's lock released, so a
// presenter that reads the server does not deadlock, and the queue it
// changed is the one applied.
func TestMessagesPresenterRunsWithoutTheLock(t *testing.T) {
	t.Parallel()
	p := &readingPresenter{}
	m, err := mattercore.NewMessages(mattercore.MessagesConfig{Presenter: p})
	if err != nil {
		t.Fatal(err)
	}
	p.m = m
	ctx := msgCtx(1, true)
	if err := decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(1), "hello")); err != nil {
		t.Fatal(err)
	}
	if err := decodeAndInvoke(t, m, ctx, messagesdef.CmdCancelMessagesRequest, messagesdef.CancelMessagesRequestRequest{MessageIds: [][]byte{messageID(1)}}); err != nil {
		t.Fatal(err)
	}
	if p.reads != 2 {
		t.Fatalf("presenter read the server %d times, want 2", p.reads)
	}
	if v, _ := m.MatterRead(messagesdef.AttrMessages); len(v.(spec.List[messagesdef.MessageStruct])) != 0 {
		t.Fatalf("Messages %v after the cancel, want empty", v)
	}
}
