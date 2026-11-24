// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	messagesdef "thingfabric.dev/go-fabric/cluster/spec/messages"
)

// cancelRefusingPresenter shows every message and refuses every cancel.
type cancelRefusingPresenter struct{}

func (cancelRefusingPresenter) PresentMessage(context.Context, messagesdef.MessageStruct) error {
	return nil
}

func (cancelRefusingPresenter) CancelMessage(context.Context, []byte) error {
	return errors.New("host refuses the cancel")
}

// lockedBuffer is a bytes.Buffer a slog handler writes from the server's
// take-down goroutine while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestMessagesSetLoggerReceivesTheTakeDownRefusal (review II, R4-fsm-4103
// follow-up H2c, decision 628): a logger set with SetLogger before the
// fabric's removal receives the presenter's refused take-down
// (matter.messages.take_down_refused), not slog.Default(). In a synctest
// bubble: synctest.Wait returns once the take-down goroutine is done.
func TestMessagesSetLoggerReceivesTheTakeDownRefusal(t *testing.T) {
	t.Parallel()
	synctest.Test(t, testMessagesSetLoggerReceivesTheTakeDownRefusal)
}

func testMessagesSetLoggerReceivesTheTakeDownRefusal(t *testing.T) {
	m, err := NewMessages(MessagesConfig{Presenter: cancelRefusingPresenter{}})
	if err != nil {
		t.Fatal(err)
	}
	var out lockedBuffer
	m.SetLogger(slog.New(slog.NewTextHandler(&out, nil)))
	id := bytes.Repeat([]byte{7}, 16)
	if err := m.present(context.Background(), 1, messagesdef.PresentMessagesRequestRequest{MessageId: id}); err != nil {
		t.Fatalf("present: %v", err)
	}
	m.RemoveFabric(1)
	synctest.Wait() // the take-down goroutine has run the refused cancel
	got := out.String()
	if !strings.Contains(got, "matter.messages.take_down_refused") || !strings.Contains(got, "fabric_index=1") {
		t.Errorf("the set logger got %q, want the take_down_refused line of fabric 1", got)
	}
}

// TestMessagesSetLoggerNilRestoresTheDefault (decision 628): SetLogger(nil)
// is slog.Default(), as AccessControl and GeneralDiagnostics have it.
func TestMessagesSetLoggerNilRestoresTheDefault(t *testing.T) {
	t.Parallel()
	m, err := NewMessages(MessagesConfig{Presenter: cancelRefusingPresenter{}})
	if err != nil {
		t.Fatal(err)
	}
	l := slog.New(slog.DiscardHandler)
	m.SetLogger(l)
	if m.logger() != l {
		t.Fatal("logger() is not the set logger")
	}
	m.SetLogger(nil)
	if m.logger() != slog.Default() {
		t.Error("SetLogger(nil) did not restore slog.Default()")
	}
}
