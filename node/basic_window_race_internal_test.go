// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"crypto/rand"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bridge"
	"thingfabric.dev/go-fabric/cluster/wire"
	"thingfabric.dev/go-fabric/mdns"
)

// lockedBuffer is a log sink several goroutines write.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestBasicWindowRefusedUnderTheInstallLock (review II, R2-P6-conc-2413):
// a basic window decides against an administrator's window under the
// lock the acceptor's install takes, so an administrator's window that
// opens while the basic one waits for that lock refuses the basic one —
// ErrAdministratorWindowOpen — before it installs an acceptor, which
// would replace the administrator's.
func TestBasicWindowRefusedUnderTheInstallLock(t *testing.T) {
	t.Parallel()
	n, _, _ := commissionedKeeperNode(t, mdns.NewNoop())
	ctx := t.Context()
	var logs lockedBuffer
	n.window.logger = slog.New(slog.NewTextHandler(&logs, nil))
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	creds := WindowCredentials{Passcode: 34567890, Discriminator: 0x123, PBKDFIterations: 1000, Salt: salt}

	n.window.installMu.Lock()
	done := make(chan error, 1)
	go func() { done <- n.OpenBasicWindow(ctx, creds) }()
	// The basic window waits for the install's lock meanwhile.
	waitBlockedIn(t, "installVerifierContext")
	if err := n.br.CommissioningWindow().OpenWindow(ctx, wire.OpenWindowParams{CommissioningTimeoutSeconds: 180, Discriminator: 100}); err != nil {
		n.window.installMu.Unlock()
		t.Fatalf("administrator OpenWindow: %v", err)
	}
	n.window.installMu.Unlock()
	if err := <-done; !errors.Is(err, bridge.ErrAdministratorWindowOpen) {
		t.Fatalf("OpenBasicWindow = %v, want ErrAdministratorWindowOpen", err)
	}
	if strings.Contains(logs.String(), "pase.window.verifier_installed") {
		t.Errorf("the refused basic window installed an acceptor:\n%s", logs.String())
	}
}

// TestBasicWindowRefusedAfterItsInstallKeepsTheAdministratorsAcceptor
// (review II, R3-conc-3751): an administrator's basic window that opens
// after a basic window installed its acceptor and before it recorded its
// window refuses the basic window (ErrAdministratorWindowOpen), and the
// refused window's restore leaves the administrator's window with the
// configured PASE acceptor, not with none.
func TestBasicWindowRefusedAfterItsInstallKeepsTheAdministratorsAcceptor(t *testing.T) {
	t.Parallel()
	n, _, _ := commissionedKeeperNode(t, mdns.NewNoop())
	ctx := t.Context()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	creds := WindowCredentials{Passcode: 34567890, Discriminator: 0x123, PBKDFIterations: 1000, Salt: salt}
	var adminErr error
	n.window.testAfterInstall = func() {
		// OpenWindow runs the window's transition before it returns.
		adminErr = n.br.CommissioningWindow().OpenWindow(ctx, wire.OpenWindowParams{CommissioningTimeoutSeconds: 180, IsBasicWindow: true})
	}
	if err := n.OpenBasicWindow(ctx, creds); !errors.Is(err, bridge.ErrAdministratorWindowOpen) {
		t.Fatalf("OpenBasicWindow = %v (administrator OpenWindow: %v), want ErrAdministratorWindowOpen", err, adminErr)
	}
	if adminErr != nil {
		t.Fatalf("administrator OpenWindow: %v", adminErr)
	}
	if w := n.br.CommissioningWindow(); !w.IsOpen() || w.IsOwnWindow() {
		t.Fatal("the administrator's basic window is not open")
	}
	n.window.installMu.Lock()
	acceptor, active, baseline := n.window.acceptor, n.window.active, n.window.baseline
	n.window.installMu.Unlock()
	if acceptor == nil || acceptor != baseline || active != nil {
		t.Errorf("administrator's basic window: acceptor %p (configured %p), window provider %p; want the configured acceptor", acceptor, baseline, active)
	}
}

// waitBlockedIn returns once a goroutine waits for a mutex inside fn,
// read from the goroutines' stacks: no wall-clock wait.
func waitBlockedIn(t *testing.T, fn string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); runtime.Gosched() {
		if blockedIn(fn) {
			return
		}
	}
	t.Fatalf("no goroutine waits for a lock in %s", fn)
}

// blockedIn reports whether a goroutine waits for a mutex inside fn.
func blockedIn(fn string) bool {
	buf := make([]byte, 1<<20)
	stacks := string(buf[:runtime.Stack(buf, true)])
	for _, g := range strings.Split(stacks, "\n\n") {
		if strings.Contains(g, "sync.(*Mutex).Lock") && strings.Contains(g, fn) {
			return true
		}
	}
	return false
}
