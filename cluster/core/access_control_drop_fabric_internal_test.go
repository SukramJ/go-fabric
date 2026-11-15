// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"
)

// extensionSettingProbe is a store that, when the Extension's setting is
// written, reports whether the server's write mutex is held.
type extensionSettingProbe struct {
	ACLStoreFacade
	a           *AccessControl
	calls       int
	heldAtWrite bool
}

func (p *extensionSettingProbe) GetSetting(context.Context, string) (value string, ok bool, err error) {
	return "", false, nil
}

func (p *extensionSettingProbe) SetSetting(context.Context, string, string) error {
	p.calls++
	if p.a.writeMu.TryLock() {
		p.a.writeMu.Unlock()
		return nil
	}
	p.heldAtWrite = true
	return nil
}

// TestDropFabricClearsTheExtensionUnderTheWriteMutex: a fabric's removal
// holds the write mutex a write of the Extension holds across its
// comparison, its setting and its entry in memory, so the removal is one
// step against such a write — it does not clear the setting between the
// write's setting and the write's entry in memory, which would leave the
// removed fabric's entry behind (review II, phase 4, R2-P4-fr-ver-05).
func TestDropFabricClearsTheExtensionUnderTheWriteMutex(t *testing.T) {
	t.Parallel()
	a := &AccessControl{extensions: map[uint8][]AccessControlExtensionEntry{1: {{FabricIndex: 1}}}}
	probe := &extensionSettingProbe{a: a}
	a.store = probe
	if err := a.MatterDropFabric(context.Background(), 1); err != nil {
		t.Fatalf("MatterDropFabric: %v", err)
	}
	if probe.calls != 1 {
		t.Fatalf("the Extension's setting was written %d times, want once", probe.calls)
	}
	if !probe.heldAtWrite {
		t.Error("the Extension's setting was cleared without the write mutex held")
	}
	if _, held := a.extensions[1]; held {
		t.Error("the entry stayed in memory after the removal")
	}
	if !a.writeMu.TryLock() {
		t.Fatal("the write mutex is still held after the removal")
	}
	a.writeMu.Unlock()
}
