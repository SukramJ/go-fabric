// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
)

// refusingLabels is an EndpointState whose NodeLabel saves fail while
// refuse is set.
type refusingLabels struct{ refuse atomic.Bool }

var errLabelStore = errors.New("label store refused")

func (r *refusingLabels) SaveNodeLabel(context.Context, device.SourceKey, string) error {
	if r.refuse.Load() {
		return errLabelStore
	}
	return nil
}

func (*refusingLabels) LoadNodeLabel(context.Context, device.SourceKey) (string, error) {
	return "", nil
}

func (*refusingLabels) LoadScenes(context.Context, endpoint.SourceKey) ([]byte, error) {
	return nil, nil
}

func (*refusingLabels) SaveScenes(context.Context, endpoint.SourceKey, []byte) error { return nil }

// TestDeviceRenameReturnsTheStoresRefusal (R2-P5-apidev-16, step 12): the
// node's refusal to store a device's label reaches Device.Rename — before
// the node assembled the endpoint and after — and a refused label is not
// served; a stored one is.
func TestDeviceRenameReturnsTheStoresRefusal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	labels := &refusingLabels{}
	labels.refuse.Store(true)
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.EndpointState = labels
	n := newNode(t, cfg)
	lamp, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("lamp"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, lamp); err != nil {
		t.Fatal(err)
	}
	if err := lamp.Rename(ctx, "Before"); !errors.Is(err, errLabelStore) {
		t.Fatalf("Rename before the assembly: %v, want the store's refusal", err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := lamp.Rename(ctx, "Desk"); !errors.Is(err, errLabelStore) {
		t.Fatalf("Rename of the assembled endpoint: %v, want the store's refusal", err)
	}
	if d := n.Topology().Devices; len(d) != 1 || d[0].Name != "Lamp" {
		t.Fatalf("after the refused renames the node serves %+v, want Lamp", d)
	}
	labels.refuse.Store(false)
	if err := lamp.Rename(ctx, "Desk"); err != nil {
		t.Fatalf("Rename once the store takes it: %v", err)
	}
	if d := n.Topology().Devices; len(d) != 1 || d[0].Name != "Desk" {
		t.Errorf("after the stored rename the node serves %+v, want Desk", d)
	}
}
