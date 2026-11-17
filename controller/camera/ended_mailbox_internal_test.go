// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/controller"
)

// pumpEnded reports whether the box's pump has ended.
func pumpEnded(b *mailbox) bool {
	select {
	case <-b.done:
		return true
	default:
		return false
	}
}

// TestEndedMailboxDiscardsUnreadSignals (review II, R2-P6-bound-2206): an
// ended session's box hands what it holds to a reader for
// endedSignalsGrace, then discards what is unread and its pump ends —
// also when the pump was already offering a signal when the session
// ended. A reader within the grace gets every signal, then the close.
// Before, the pump of an unread box waited for a reader until the
// service's life ended, holding its signals.
func TestEndedMailboxDiscardsUnreadSignals(t *testing.T) {
	t.Parallel()
	t.Run("unread signals go once the grace passed", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			b := newMailbox(context.Background(), nil)
			b.put(Signal{Kind: SignalAnswer, SDP: "x"})
			synctest.Wait() // the pump took the signal and offers it
			b.put(Signal{Kind: SignalEnd})
			b.close()
			synctest.Wait()
			if pumpEnded(b) {
				t.Fatal("the pump ended at the session's end, before the grace")
			}
			time.Sleep(endedSignalsGrace)
			synctest.Wait()
			if !pumpEnded(b) {
				t.Fatal("the pump still runs after the grace with unread signals")
			}
			if _, open := <-b.out; open {
				t.Error("Signals handed a signal after the grace")
			}
			b.mu.Lock()
			held := len(b.queue)
			b.mu.Unlock()
			if held != 0 {
				t.Errorf("the box still holds %d signals", held)
			}
		})
	})
	t.Run("a reader within the grace gets every signal", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			b := newMailbox(context.Background(), nil)
			b.put(Signal{Kind: SignalAnswer, SDP: "x"})
			b.put(Signal{Kind: SignalEnd})
			b.close()
			var kinds []controller.CameraSignalKind
			for sig := range b.out {
				kinds = append(kinds, sig.Kind)
			}
			if len(kinds) != 2 || kinds[1] != SignalEnd {
				t.Errorf("read %v, want the Answer and the End", kinds)
			}
			synctest.Wait()
			if !pumpEnded(b) {
				t.Error("the pump runs after Signals closed")
			}
		})
	})
}
