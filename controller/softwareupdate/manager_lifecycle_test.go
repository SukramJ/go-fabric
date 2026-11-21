// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/controller/client"
)

// lineRecorder keeps the messages of the lines logged.
type lineRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (l *lineRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (l *lineRecorder) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	l.msgs = append(l.msgs, r.Message)
	l.mu.Unlock()
	return nil
}
func (l *lineRecorder) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *lineRecorder) WithGroup(string) slog.Handler      { return l }

func (l *lineRecorder) has(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.msgs {
		if m == msg {
			return true
		}
	}
	return false
}

// startedManager is a manager started on a fake clock, its timers' check
// replaced by check.
func startedManager(t *testing.T, log *slog.Logger, check func(ctx context.Context) error) (*Manager, *clocktest.Fake) {
	t.Helper()
	clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	m, err := New(Config{Registry: &client.Registry{}, Images: NewImages(ImagesConfig{}), Clock: clk, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	m.check = check
	m.Start()
	return m, clk
}

// TestManagerCloseCancelsAndWaitsForTheTimersCheck (review II,
// R2-P6-conc-1907): an update check a timer started runs under a context
// Close cancels, and Close returns only after the check returned — so a
// download the check runs stores nothing after Close.
func TestManagerCloseCancelsAndWaitsForTheTimersCheck(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	var returned atomic.Bool
	var ctxErr atomic.Value
	m, clk := startedManager(t, slog.New(slog.DiscardHandler), func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		ctxErr.Store(ctx.Err())
		returned.Store(true)
		return ctx.Err()
	})
	clk.Advance(11 * time.Minute) // past the first check's 5 to 10 minutes
	<-entered
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	watchdog, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	select {
	case <-closed:
	case <-watchdog.Done():
		t.Fatal("Close did not return: the running check was not cancelled")
	}
	if !returned.Load() {
		t.Fatal("Close returned before the running check did")
	}
	if err, _ := ctxErr.Load().(error); !errors.Is(err, context.Canceled) {
		t.Fatalf("the check's context ended with %v, want context.Canceled", err)
	}
}

// TestManagerInitialCheckReportsItsFailures (review II, R2-P6-err-1912):
// the failures of the first update check reach its log line.
func TestManagerInitialCheckReportsItsFailures(t *testing.T) {
	t.Parallel()
	rec := &lineRecorder{}
	done := make(chan struct{})
	m, clk := startedManager(t, slog.New(rec), func(context.Context) error {
		defer close(done)
		return errors.New("product check failed")
	})
	clk.Advance(11 * time.Minute)
	<-done
	m.Close() // waits for the check's goroutine, the log line included
	if !rec.has("ota.manager.initial_check_failed") {
		t.Fatalf("lines %v, want ota.manager.initial_check_failed", rec.msgs)
	}
}

// TestManagerForgetsNodesThatLeft (review II, R2-P6-bound-1914): a check of
// every node drops what the manager keeps for a node the registry no
// longer holds; a node with a queued update keeps it.
func TestManagerForgetsNodesThatLeft(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	gone := Peer{FabricIndex: 1, NodeID: 0x22}
	r.queue(2, nil) // testPeer: queued
	r.m.mu.Lock()
	for _, p := range []Peer{gone, testPeer} {
		r.m.observed[p] = 1
		r.m.known[p] = SoftwareUpdateInfo{}
		r.m.startUpSuppress[p] = 7
	}
	r.m.consents[gone] = &consent{Target: Target{TargetSoftwareVersion: 2}, peer: gone}
	r.m.mu.Unlock()
	if err := r.m.CheckForUpdates(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	_, o := r.m.observed[gone]
	_, k := r.m.known[gone]
	_, s := r.m.startUpSuppress[gone]
	if _, ok := r.m.consents[gone]; ok {
		t.Fatal("a consent of a node that left stays")
	}
	if o || k || s {
		t.Fatalf("kept for a node that left: observed %v, known %v, startUpSuppress %v", o, k, s)
	}
	if _, ok := r.m.observed[testPeer]; !ok {
		t.Fatal("dropped the state of a node with a queued update")
	}
}

// TestManagerSettersReadUnderTheLock (review II, R2-P6-conc-1913) is the
// standing guard of the fields SetProvider and SetBDX write after New: no
// function of the package but the setters, their two accessors and the
// queue run's locked read (bdxSessionForLocked) reads
// them from the manager's Config, and nothing copies the whole Config
// (which reads them too). A new reader goes through provider() or
// bdxResponder().
func TestManagerSettersReadUnderTheLock(t *testing.T) {
	t.Parallel()
	// bdxSessionForLocked reads the responder with m.mu held: the queue
	// run calls it under the lock it would otherwise take again.
	allowed := map[string]bool{"SetProvider": true, "SetBDX": true, "provider": true, "bdxResponder": true, "bdxSessionForLocked": true}
	fields := map[string]bool{"ProviderNodeID": true, "ProviderEndpoint": true, "BDX": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || allowed[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				inner, ok := sel.X.(*ast.SelectorExpr)
				isCfg := sel.Sel.Name == "cfg"
				if (ok && inner.Sel.Name == "cfg" && fields[sel.Sel.Name]) || (isCfg && wholeCopy(fn.Body, sel)) {
					t.Errorf("%s: %s reads the manager's Config outside the lock", fset.Position(sel.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
}

// wholeCopy reports whether sel (an x.cfg) is the whole right-hand side
// of an assignment in body: a copy of the whole Config.
func wholeCopy(body *ast.BlockStmt, sel *ast.SelectorExpr) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for _, r := range as.Rhs {
				if r == sel {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// TestQueryUpdatesReturnsTheProductFailures (review II, R2-P6-err-1912,
// decision 217): QueryUpdates and CheckForUpdates return the joined
// failures of the products whose check failed beside
// the updates the other products found; nil only when all succeeded.
func TestQueryUpdatesReturnsTheProductFailures(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	first, second := errors.New("product fff1-8000 failed"), errors.New("product fff1-8001 failed")
	found := []PeerUpdate{{Peer: testPeer}}
	r.m.query = func(context.Context, *client.Node, bool) ([]PeerUpdate, bool, error) {
		return found, true, errors.Join(first, second)
	}
	got, err := r.m.QueryUpdates(context.Background(), nil, true)
	if !errors.Is(err, first) || !errors.Is(err, second) || len(got) != 1 {
		t.Fatalf("QueryUpdates = %v, %v; want the update found and both failures", got, err)
	}
	if err := r.m.CheckForUpdates(context.Background()); !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("CheckForUpdates = %v; want both failures", err)
	}
	r.m.query = func(context.Context, *client.Node, bool) ([]PeerUpdate, bool, error) { return found, true, nil }
	if _, err := r.m.QueryUpdates(context.Background(), nil, true); err != nil {
		t.Fatalf("QueryUpdates with every product checked: %v", err)
	}
}

// TestManagerNodeDeleted (review II, R2-P6-bound-1914, decision 219): a
// node that left the registry is forgotten at once — its consent and its
// queued update go, the update without an event (#forgetPeer,
// SoftwareUpdateManager.ts:312-341 at b25ed960; the contract :2303-2308),
// and nothing of it stays for a later node at the same address; another
// node keeps its state.
func TestManagerNodeDeleted(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	other := Peer{FabricIndex: 1, NodeID: 0x22}
	r.queue(2, statusPtr(mattercore.OTAUpdateDownloading))
	r.m.mu.Lock()
	for _, p := range []Peer{testPeer, other} {
		r.m.observed[p] = 1
		r.m.known[p] = SoftwareUpdateInfo{}
		r.m.startUpSuppress[p] = 7
	}
	r.m.consents[other] = &consent{Target: Target{TargetSoftwareVersion: 3}, peer: other}
	r.m.mu.Unlock()

	r.setNode(true, nil) // the registry removed it before its Deleted hook
	r.m.NodeDeleted(testPeer)
	if r.m.HasConsent(testPeer, 0) || len(r.m.QueuedUpdates()) != 0 {
		t.Fatal("the deleted node keeps a consent or its queued update")
	}
	if failed, _ := r.counts(); failed != 0 {
		t.Fatalf("%d updates reported failed, want none (the update ends without an event)", failed)
	}
	r.m.mu.Lock()
	_, o := r.m.observed[testPeer]
	_, k := r.m.known[testPeer]
	_, s := r.m.startUpSuppress[testPeer]
	_, otherKept := r.m.observed[other]
	r.m.mu.Unlock()
	if o || k || s {
		t.Fatalf("kept for the deleted node: observed %v, known %v, startUpSuppress %v", o, k, s)
	}
	if !otherKept || !r.m.HasConsent(other, 3) {
		t.Fatal("another node's state went with it")
	}
	r.m.NodeDeleted(Peer{FabricIndex: 9, NodeID: 9}) // never seen: no effect, no panic
}

// TestManagerNodeDeletedDisarmsWithoutAQueuedUpdate (review II,
// R2-P6-bound-1914): a node armed for its reboot is disarmed when it is
// deleted although no update of it is queued — its return deadline then
// recovers nothing.
func TestManagerNodeDeletedDisarmsWithoutAQueuedUpdate(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var lost atomic.Int32
	armer := peer.NewRebootResubscribeArmer(peer.RebootArmerContext{
		Clock: clk, PeerLost: func(exchange.PeerAddress) { lost.Add(1) },
		ClosePeerSessionsRegisteredBefore: func(exchange.PeerAddress, peer.ArmerSession) {}, CloseSubscriptions: func(exchange.PeerAddress) {},
		Logger: slog.New(slog.DiscardHandler),
	})
	m, err := New(Config{Registry: &client.Registry{}, Images: NewImages(ImagesConfig{}), Clock: clk, Armer: armer, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.mu.Lock()
	m.suppress = false // online: NodeDeleted does nothing while updates are suppressed (C-15)
	m.mu.Unlock()
	armer.Arm(testPeer, 0)
	m.NodeDeleted(testPeer)
	clk.Advance(peer.RebootReturnTimeout(0) + time.Minute)
	if n := lost.Load(); n != 0 {
		t.Fatalf("the deleted node's return deadline gave it up %d times; want it disarmed", n)
	}
}
