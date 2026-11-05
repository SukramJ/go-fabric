// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/im"
)

// The cases of matterjs-server's
// packages/ws-controller/test/WebRtcProviderSessionWatchTest.ts:77-559 at
// 0bd06f2, ported by meaning: MockTime is the fake clock, a microtask wait
// is wait() on the watch's sweeps, a logged message is a slog record
// (notice is Info).

const (
	wCamera     = uint64(5)
	wEndpoint   = uint16(1)
	wServer     = uint64(100)
	wController = uint64(200)
)

// watchFixture is the fixture of WebRtcProviderSessionWatchTest.ts:22-63,
// matterjs-server at 0bd06f2.
type watchFixture struct {
	mu          sync.Mutex
	sessions    map[uint16][]sessionPeer
	requestor   map[uint16]bool
	reports     []string
	ended       []uint16
	endAttempts int
	endFailure  error
	endGate     chan struct{}
	providers   func() []uint16
	onReport    func(ep uint16)
	lookups     int
}

func (f *watchFixture) localNodeID() uint64 { return wServer }

func (f *watchFixture) providerEndpoints(uint64) []uint16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	if f.providers != nil {
		return f.providers()
	}
	var out []uint16
	for ep := range f.sessions {
		out = append(out, ep)
	}
	slices.Sort(out)
	return out
}

func (f *watchFixture) currentSessions(_ uint64, ep uint16) ([]sessionPeer, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[ep]
	return slices.Clone(s), ok
}

func (f *watchFixture) requestorSessionIDs(uint64, uint16) map[uint16]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uint16]bool{}
	for id := range f.requestor {
		out[id] = true
	}
	return out
}

func (f *watchFixture) endSession(_ context.Context, _ uint64, _, id uint16) error {
	f.mu.Lock()
	f.endAttempts++
	gate := f.endGate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.endFailure != nil {
		return f.endFailure
	}
	f.ended = append(f.ended, id)
	return nil
}

func (f *watchFixture) reported(node uint64, ep uint16, present map[uint16]bool, restarted bool) {
	ids := make([]int, 0, len(present))
	for id := range present {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	f.mu.Lock()
	f.reports = append(f.reports, fmt.Sprintf("%d/%d [%s] restarted=%v", node, ep, strings.Join(parts, ","), restarted))
	onReport := f.onReport
	f.mu.Unlock()
	if onReport != nil {
		onReport(ep)
	}
}

func (f *watchFixture) set(ep uint16, s []sessionPeer) {
	f.mu.Lock()
	f.sessions[ep] = s
	f.mu.Unlock()
}

func (f *watchFixture) snapshot() (reports []string, ended []uint16, attempts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reports), slices.Clone(f.ended), f.endAttempts
}

func ours(ids ...uint16) []sessionPeer {
	out := make([]sessionPeer, len(ids))
	for i, id := range ids {
		out[i] = sessionPeer{id: id, peerNodeID: wServer}
	}
	return out
}

// recordingHandler keeps every slog record as "LEVEL msg session=…".
type recordingHandler struct {
	mu   *sync.Mutex
	logs *[]string
}

func (h recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h recordingHandler) Handle(_ context.Context, r slog.Record) error {
	line := r.Level.String() + " " + r.Message
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "session" || a.Key == "sweeps" {
			line += " " + a.Key + "=" + a.Value.String()
		}
		return true
	})
	h.mu.Lock()
	*h.logs = append(*h.logs, line)
	h.mu.Unlock()
	return nil
}

func (h recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h recordingHandler) WithGroup(string) slog.Handler { return h }

func newWatchFixture() (*watchFixture, *sessionWatch, *clocktest.Fake, func() []string) {
	f := &watchFixture{sessions: map[uint16][]sessionPeer{wEndpoint: {}}, requestor: map[uint16]bool{}}
	clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var mu sync.Mutex
	var logs []string
	w := newSessionWatch(context.Background(), f, clk, slog.New(recordingHandler{&mu, &logs}))
	return f, w, clk, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(logs)
	}
}

// pastFirstBatch: the first batch end of a run reports every provider
// endpoint (WebRtcProviderSessionWatchTest.ts:69-73, matterjs-server at
// 0bd06f2).
func pastFirstBatch(f *watchFixture, w *sessionWatch) {
	w.reportCompleted(wCamera)
	w.wait()
	f.mu.Lock()
	f.reports = nil
	f.mu.Unlock()
}

func wantReports(t *testing.T, f *watchFixture, want ...string) {
	t.Helper()
	got, _, _ := f.snapshot()
	if want == nil {
		want = []string{}
	}
	if got == nil {
		got = []string{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("reports = %q, want %q", got, want)
	}
}

func wantEnded(t *testing.T, f *watchFixture, want ...uint16) {
	t.Helper()
	_, got, _ := f.snapshot()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !slices.Equal(got, want) {
		t.Errorf("ended = %v, want %v", got, want)
	}
}

// TestSessionWatchParityMatterjsServer is describe("WebRtcProviderSessionWatch")
// (WebRtcProviderSessionWatchTest.ts:77-559, matterjs-server at 0bd06f2).
func TestSessionWatchParityMatterjsServer(t *testing.T) {
	t.Run("reports a changed CurrentSessions at the batch end with this server's ids only", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		pastFirstBatch(f, w)
		f.set(wEndpoint, append(ours(3), sessionPeer{id: 4, peerNodeID: wController}))
		w.currentSessionsChanged(wCamera, wEndpoint)
		wantReports(t, f)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [3] restarted=false")
	})
	t.Run("marks the report as a restart when the batch carried a StartUp event, in either order", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		pastFirstBatch(f, w)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.startedUp(wCamera)
		w.reportCompleted(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [] restarted=true", "5/1 [] restarted=false")
	})
	t.Run("marks an endpoint's first change report after the StartUp event as a restart, batches later", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		pastFirstBatch(f, w)
		w.startedUp(wCamera)
		w.reportCompleted(wCamera)
		w.reportCompleted(wCamera)
		w.reportCompleted(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [] restarted=true", "5/1 [] restarted=false")
	})
	t.Run("keeps the mark through a report built from node state in the StartUp batch", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.set(wEndpoint, ours(3))
		w.startedUp(wCamera)
		w.reportCompleted(wCamera)
		f.set(wEndpoint, nil)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [3] restarted=false", "5/1 [] restarted=true")
	})
	t.Run("keeps the mark through a report sweepAgain or endpointAdded builds from node state", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		pastFirstBatch(f, w)
		w.startedUp(wCamera)
		w.sweepAgain(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.endpointAdded(wCamera)
		w.reportCompleted(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [] restarted=false", "5/1 [] restarted=false", "5/1 [] restarted=true")
	})
	t.Run("forgets a StartUp event ${RESTART_MARK_TIMEOUT} ms after it arrived", func(t *testing.T) {
		f, w, clk, _ := newWatchFixture()
		pastFirstBatch(f, w)
		w.startedUp(wCamera)
		clk.Advance(restartMarkTimeout - time.Millisecond)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.startedUp(wCamera)
		clk.Advance(restartMarkTimeout)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [] restarted=true", "5/1 [] restarted=false")
	})
	t.Run("reports and sweeps every endpoint when handling one report throws", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.set(wEndpoint, ours(1))
		f.set(2, ours(2))
		f.onReport = func(ep uint16) {
			if ep == wEndpoint {
				panic("listener failed")
			}
		}
		w.startedUp(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.wait()
		wantReports(t, f, "5/1 [1] restarted=true", "5/2 [2] restarted=false", "5/1 [1] restarted=false")
		// Each endpoint's sweep runs on its own goroutine: the order across
		// endpoints is not the test's.
		_, ended, _ := f.snapshot()
		slices.Sort(ended)
		if !slices.Equal(ended, []uint16{1, 2}) {
			t.Errorf("ended = %v, want [1 2]", ended)
		}
	})
	t.Run("marks the first report of each endpoint after a StartUp event, in whichever batch it comes", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.set(2, nil)
		pastFirstBatch(f, w)
		w.startedUp(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.currentSessionsChanged(wCamera, 2)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [] restarted=true", "5/1 [] restarted=false", "5/2 [] restarted=true")
	})
	t.Run("reports a changed endpoint once: the batch end consumes its change", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		pastFirstBatch(f, w)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [] restarted=false")
	})
	t.Run("reports nothing for an endpoint whose provider state is gone", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		pastFirstBatch(f, w)
		f.mu.Lock()
		delete(f.sessions, wEndpoint)
		f.mu.Unlock()
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		wantReports(t, f)
	})
	t.Run("drops the StartUp event of a removed node", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		pastFirstBatch(f, w)
		w.startedUp(wCamera)
		w.nodeRemoved(wCamera)
		w.reportCompleted(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [] restarted=false", "5/1 [] restarted=false")
	})
	t.Run("keeps the sweep done for other nodes when a node is removed", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		w.reportCompleted(wCamera)
		w.reportCompleted(15)
		w.nodeRemoved(1)
		f.set(wEndpoint, ours(1))
		w.endpointAdded(wCamera)
		w.endpointAdded(15)
		w.reportCompleted(wCamera)
		w.reportCompleted(15)
		w.wait()
		wantEnded(t, f)
	})
	t.Run("sweeps an endpoint again after sweepAgain, ending what the requestor does not hold", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		w.reportCompleted(wCamera)
		f.set(wEndpoint, ours(4, 5))
		f.requestor[5] = true
		w.sweepAgain(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 4)
	})
	t.Run("keeps waiting with the sweep while a second establishing call on the endpoint runs", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.set(wEndpoint, ours(7))
		var finished [2]chan error
		gates := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
		started := make(chan struct{}, 2)
		for i := range gates {
			finished[i] = make(chan error, 1)
			go func() {
				_, err := establishingDo(w, tTarget5(), func() (struct{}, error) { started <- struct{}{}; <-gates[i]; return struct{}{}, nil })
				finished[i] <- err
			}()
		}
		<-started
		<-started
		close(gates[0])
		<-finished[0]
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f)
		close(gates[1])
		<-finished[1]
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 7)
	})
	t.Run("lets the sweep run again once an establishing call failed", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.set(wEndpoint, ours(7))
		if _, err := establishingDo(w, tTarget5(), func() (struct{}, error) { return struct{}{}, errors.New("refused") }); err == nil {
			t.Fatal("the establishing call did not fail")
		}
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 7)
	})
	t.Run("sweeps an endpoint added after the node's first batch end", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.mu.Lock()
		f.sessions = map[uint16][]sessionPeer{}
		f.mu.Unlock()
		w.reportCompleted(wCamera)
		f.set(wEndpoint, ours(1))
		w.endpointAdded(wCamera)
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 1)
	})
	t.Run("reports nothing for a batch without a CurrentSessions change once the first batch is done", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		w.reportCompleted(wCamera)
		w.reportCompleted(wCamera)
		wantReports(t, f, "5/1 [] restarted=false")
	})
	t.Run("looks up a node without a camera endpoint once, not at every batch end", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.providers = func() []uint16 { return nil }
		w.reportCompleted(wCamera)
		w.reportCompleted(wCamera)
		if f.lookups != 1 {
			t.Errorf("lookups = %d, want 1", f.lookups)
		}
	})
	t.Run("ends, once, every session the camera holds for this server that no local record names", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.set(wEndpoint, append(ours(1, 2), sessionPeer{id: 3, peerNodeID: wController}))
		f.requestor[2] = true
		w.reportCompleted(wCamera)
		w.currentSessionsChanged(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 1)
	})
	t.Run("waits with the sweep while a session is being established on that endpoint", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.set(wEndpoint, ours(7))
		gate, started, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			_, _ = establishingDo(w, tTarget5(), func() (struct{}, error) { close(started); <-gate; return struct{}{}, nil })
			close(done)
		}()
		<-started
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f)
		// The establishing call returns once the local requestor holds the
		// session.
		f.mu.Lock()
		f.requestor[7] = true
		f.mu.Unlock()
		close(gate)
		<-done
		f.set(wEndpoint, ours(7, 1))
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 1)
	})
	sweepOne := func(endFailure error) []string {
		f, w, _, logs := newWatchFixture()
		f.set(wEndpoint, ours(1))
		f.endFailure = endFailure
		w.reportCompleted(wCamera)
		w.wait()
		return logs()
	}
	t.Run("logs each leftover session it ended at notice level", func(t *testing.T) {
		if got := sweepOne(nil); !slices.Equal(got, []string{"INFO camera.sweep.ended session=1"}) {
			t.Errorf("logged %q", got)
		}
	})
	t.Run("treats a NotFound answer as the session being gone already, and warns on any other failure", func(t *testing.T) {
		if got := sweepOne(statusErr(im.StatusNotFound)); len(got) != 0 {
			t.Errorf("NotFound logged %q", got)
		}
		if got := sweepOne(statusErr(im.StatusBusy)); len(got) != 1 || !strings.HasPrefix(got[0], "WARN camera.sweep.end_failed session=1") {
			t.Errorf("Busy logged %q", got)
		}
	})
	t.Run("sweeps again after a failed EndSession, once the retry delay passed, against the current list", func(t *testing.T) {
		f, w, clk, _ := newWatchFixture()
		f.set(wEndpoint, ours(1))
		f.endFailure = statusErr(im.StatusBusy)
		w.reportCompleted(wCamera)
		w.wait()
		if _, _, n := f.snapshot(); n != 1 {
			t.Fatalf("attempts = %d", n)
		}
		f.mu.Lock()
		f.endFailure = nil
		f.mu.Unlock()
		f.set(wEndpoint, ours(1, 2))
		clk.Advance(leftoverSweepRetryDelay - time.Millisecond)
		w.reportCompleted(wCamera)
		w.wait()
		if _, _, n := f.snapshot(); n != 1 {
			t.Fatalf("attempts before the delay = %d", n)
		}
		clk.Advance(time.Millisecond)
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 1, 2)
		clk.Advance(leftoverSweepRetryDelay)
		w.reportCompleted(wCamera)
		w.wait()
		if _, _, n := f.snapshot(); n != 3 {
			t.Errorf("attempts = %d, want 3", n)
		}
	})
	t.Run("stops sweeping an endpoint after ${MAX_FAILED_LEFTOVER_SWEEPS} failed sweeps", func(t *testing.T) {
		f, w, clk, logs := newWatchFixture()
		f.set(wEndpoint, ours(1))
		f.endFailure = statusErr(im.StatusBusy)
		for range maxFailedLeftoverSweeps + 2 {
			w.reportCompleted(wCamera)
			w.wait()
			clk.Advance(leftoverSweepRetryDelay)
		}
		if _, _, n := f.snapshot(); n != maxFailedLeftoverSweeps {
			t.Fatalf("attempts = %d, want %d", n, maxFailedLeftoverSweeps)
		}
		if got := logs(); got[len(got)-1] != "WARN camera.sweep.given_up sweeps=3" {
			t.Errorf("last log %q", got[len(got)-1])
		}
		w.sweepAgain(wCamera, wEndpoint)
		w.reportCompleted(wCamera)
		w.wait()
		if _, _, n := f.snapshot(); n != maxFailedLeftoverSweeps+1 {
			t.Errorf("attempts after sweepAgain = %d", n)
		}
	})
	t.Run("lets sweepAgain during a failing EndSession sweep at the next batch end, without the retry delay", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		f.set(wEndpoint, ours(1))
		f.endFailure = statusErr(im.StatusBusy)
		gate := make(chan struct{})
		f.endGate = gate
		w.reportCompleted(wCamera)
		w.sweepAgain(wCamera, wEndpoint)
		close(gate)
		w.wait()
		f.mu.Lock()
		f.endGate, f.endFailure = nil, nil
		f.mu.Unlock()
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 1)
	})
	for _, c := range []struct {
		what  string
		reset func(*sessionWatch)
	}{
		{"sweepAgain", func(w *sessionWatch) { w.sweepAgain(wCamera, wEndpoint) }},
		{"a node removal", func(w *sessionWatch) { w.nodeRemoved(wCamera) }},
	} {
		t.Run("drops the retry delay of a failed sweep on ${what} ("+c.what+")", func(t *testing.T) {
			f, w, _, _ := newWatchFixture()
			f.set(wEndpoint, ours(1))
			f.endFailure = statusErr(im.StatusBusy)
			w.reportCompleted(wCamera)
			w.wait()
			f.mu.Lock()
			f.endFailure = nil
			f.mu.Unlock()
			c.reset(w)
			w.reportCompleted(wCamera)
			w.wait()
			wantEnded(t, f, 1)
		})
	}
	t.Run("sweeps a node again after it was removed and commissioned anew", func(t *testing.T) {
		f, w, _, _ := newWatchFixture()
		w.reportCompleted(wCamera)
		w.nodeRemoved(wCamera)
		f.set(wEndpoint, ours(1))
		w.reportCompleted(wCamera)
		w.wait()
		wantEnded(t, f, 1)
	})
}

func tTarget5() target { return target{wCamera, wEndpoint} }
