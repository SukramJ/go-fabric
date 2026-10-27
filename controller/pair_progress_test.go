// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// progressLog collects PairOptions.Progress reports.
type progressLog struct {
	mu    sync.Mutex
	steps []controller.PairingStep
}

func (l *progressLog) add(s controller.PairingStep) {
	l.mu.Lock()
	l.steps = append(l.steps, s)
	l.mu.Unlock()
}

func (l *progressLog) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.steps))
	for i, s := range l.steps {
		out[i] = fmt.Sprintf("%d.%d %s %s", s.Number, s.Sub, s.Name, s.Phase)
	}
	return out
}

// TestPairProgress: a paired device reports each step started, then
// succeeded or skipped, in execution order under matter.js's step names
// (ControllerCommissioningFlow.ts:536-656 at 862ad75f) — the discovery
// and PASE first, the module's own 2.1 and 2.2. An IP device has no
// network steps (:596-626); this device's regulatory configuration is
// skipped (:1185-1188), time synchronisation always is (:1200-1211), and
// so is the ACL step, whose standard entry is enough (:1516-1522).
func TestPairProgress(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	l := &progressLog{}
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{OnAttestationFailure: attestation.Accept(), Progress: l.add})
		return err
	}); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	want := []string{
		"2.1 Discovery started", "2.1 Discovery succeeded",
		"2.2 PASE started", "2.2 PASE succeeded",
		"0.1 GetInitialData started", "0.1 GetInitialData succeeded",
		"7.1 GeneralCommissioning.ArmFailsafe started", "7.1 GeneralCommissioning.ArmFailsafe succeeded",
		"8.1 GeneralCommissioning.ConfigureRegulatoryInformation started", "8.1 GeneralCommissioning.ConfigureRegulatoryInformation skipped",
		"8.2 TimeSynchronization.SynchronizeTime started", "8.2 TimeSynchronization.SynchronizeTime skipped",
		"10.1 OperationalCredentials.DeviceAttestation started", "10.1 OperationalCredentials.DeviceAttestation succeeded",
		"11.1 OperationalCredentials.Certificates started", "11.1 OperationalCredentials.Certificates succeeded",
		"15.1 AccessControl started", "15.1 AccessControl skipped",
		"18.1 Reconnect started", "18.1 Reconnect succeeded",
		"20.1 GeneralCommissioning.Complete started", "20.1 GeneralCommissioning.Complete succeeded",
		"98.1 OperationalCredentials.UpdateFabricLabel started", "98.1 OperationalCredentials.UpdateFabricLabel succeeded",
	}
	if got := l.lines(); !slices.Equal(got, want) {
		t.Fatalf("progress %q,\nwant %q", got, want)
	}
}

// TestPairProgressAttestationFailure: a device whose attestation the
// default policy rejects — the site's test Certification Declaration —
// reports OperationalCredentials.DeviceAttestation failed with the error
// Pair returns, and nothing after it (:438-462).
func TestPairProgressAttestationFailure(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	l := &progressLog{}
	var pairErr error
	_ = site.Run(ctx, func(ctx context.Context) error {
		_, pairErr = c.Pair(ctx, manualCode(t, dev), controller.PairOptions{Progress: l.add})
		return nil
	})
	if pairErr == nil {
		t.Fatal("Pair accepted a test attestation under the default policy")
	}
	l.mu.Lock()
	last := l.steps[len(l.steps)-1]
	l.mu.Unlock()
	if last.Number != 10 || last.Sub != 1 || last.Name != "OperationalCredentials.DeviceAttestation" || last.Phase != controller.PairingFailed {
		t.Fatalf("last report %+v, want 10.1 DeviceAttestation failed; all %q", last, l.lines())
	}
	if last.Err == nil || !errors.Is(pairErr, last.Err) {
		t.Errorf("failed step's error %v, Pair's %v", last.Err, pairErr)
	}
}

// TestPairProgressByAddress: with an address the discovery is skipped.
func TestPairProgressByAddress(t *testing.T) {
	t.Parallel()
	site, dev, c := siteController(t, fabrictest.Topology{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	l := &progressLog{}
	if err := site.Run(ctx, func(ctx context.Context) error {
		_, err := c.Pair(ctx, manualCode(t, dev), controller.PairOptions{Address: dev.Addr(), OnAttestationFailure: attestation.Accept(), Progress: l.add})
		return err
	}); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if got := l.lines(); len(got) < 2 || got[0] != "2.1 Discovery skipped" || got[1] != "2.2 PASE started" {
		t.Fatalf("progress %q, want the discovery skipped first", got)
	}
}
