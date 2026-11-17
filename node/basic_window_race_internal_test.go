// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"crypto/rand"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/mdns"
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
	time.Sleep(50 * time.Millisecond)
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
