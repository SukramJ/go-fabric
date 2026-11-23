// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	tsuic "thingfabric.dev/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"thingfabric.dev/go-fabric/cluster/thermo"
)

// gatedSettings holds the first save, once written, until release
// closes, and signals the second save on second.
type gatedSettings struct {
	mu      sync.Mutex
	values  map[string]string
	calls   int
	first   chan struct{} // closed when the first save is entered
	second  chan struct{} // closed when the second save is entered
	release chan struct{}
}

func (g *gatedSettings) GetSetting(_ context.Context, key string) (string, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	v, ok := g.values[key]
	return v, ok, nil
}

func (g *gatedSettings) SetSetting(_ context.Context, key, value string) error {
	g.mu.Lock()
	g.calls++
	n := g.calls
	g.values[key] = value
	g.mu.Unlock()
	switch n {
	case 1: // saved; the caller's store waits
		close(g.first)
		<-g.release
	case 2:
		close(g.second)
	}
	return nil
}

// TestThermostatUIKeypadLockoutSaveAndStoreAreOneStep (R2-C2-store-1555):
// a controller's KeypadLockout write that lands while the host's Set is
// saving does not leave the Settings and the served value apart. Before
// the fix the host saved A, the controller saved and stored B, and the
// host stored A: the server served A while the Settings held B.
func TestThermostatUIKeypadLockoutSaveAndStoreAreOneStep(t *testing.T) {
	t.Parallel()
	settings := &gatedSettings{
		values: map[string]string{}, first: make(chan struct{}),
		second: make(chan struct{}), release: make(chan struct{}),
	}
	srv := newTSUIC(t, thermo.ThermostatUIConfig{Settings: settings})
	hostDone := make(chan error, 1)
	go func() { hostDone <- srv.Set(tsuic.AttrKeypadLockout, tsuic.KeypadLockoutLockout1) }()
	<-settings.first
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- srv.MatterWrite(context.Background(), tsuic.AttrKeypadLockout, uint64(tsuic.KeypadLockoutLockout2))
	}()
	// Before the fix the controller's write saves (and stores) at once;
	// after it, it waits for the host's call to finish.
	var writeErr error
	written := false
	select {
	case <-settings.second:
		writeErr, written = <-writeDone, true
	case <-time.After(200 * time.Millisecond):
	}
	close(settings.release)
	if err := <-hostDone; err != nil {
		t.Fatalf("host Set: %v", err)
	}
	if !written {
		writeErr = <-writeDone
	}
	if writeErr != nil {
		t.Fatalf("controller write: %v", writeErr)
	}
	v, _ := srv.Value(tsuic.AttrKeypadLockout)
	got, _, _ := settings.GetSetting(context.Background(), thermo.DefaultKeypadLockoutSetting)
	if fmt.Sprintf("%d", v) != got {
		t.Errorf("KeypadLockout serves %v while the Settings hold %q", v, got)
	}
}
