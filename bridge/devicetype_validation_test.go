// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// singletonServer is a BasicInformation-id server: a RootNode singleton.
type singletonServer struct{}

func (singletonServer) MatterClusterID() uint32                                { return 0x0028 }
func (singletonServer) MatterRead(uint32) (any, bool)                          { return nil, false }
func (singletonServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (singletonServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (singletonServer) MatterReportable() []uint32                             { return nil }

func stopBridge(t *testing.T, b *bridge.Bridge) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = b.Stop(ctx)
	})
}

func aggregatorDescriptor(t *testing.T) contract.ClusterServer {
	t.Helper()
	d, err := mattercore.NewDescriptor([]mattercore.DeviceTypeStruct{{DeviceType: 0x000E, Revision: 2}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// In the default mode a non-conforming topology is installed and its
// violations recorded; a misplaced singleton refuses the reassembly and
// the bridge keeps serving the topology it had.
func TestDeviceTypeValidationWarnByDefault(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	stopBridge(t, b)
	if got := b.DeviceTypeViolations(); len(got) != 0 {
		t.Fatalf("violations before Start: %v", got)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start refused a topology in warn mode: %v", err)
	}
	// The root and the Aggregator carry no servers, so their mandatory
	// clusters are missing.
	if got := b.DeviceTypeViolations(); len(got) == 0 || got[0].Endpoint != 0 || got[0].Kind != endpoint.ViolationMissing {
		t.Errorf("recorded violations = %v", got)
	}
	installed := b.Topology()

	b.AttachAggregatorClusters([]contract.ClusterServer{aggregatorDescriptor(t), singletonServer{}})
	err := b.Reassemble(context.Background())
	var refusal *endpoint.DeviceTypeConformanceError
	if !errors.As(err, &refusal) || refusal.Violations[0].Kind != endpoint.ViolationSingletonMisplaced || refusal.Violations[0].Endpoint != 1 {
		t.Fatalf("Reassemble with a misplaced singleton = %v", err)
	}
	if b.Topology() != installed {
		t.Error("a refused topology was installed")
	}
}

// Strict refuses any new violation at Start; off judges nothing but
// singleton placement.
func TestDeviceTypeValidationModes(t *testing.T) {
	t.Parallel()
	strict := newTestBridge(t)
	stopBridge(t, strict)
	strict.SetDeviceTypeValidation(endpoint.DeviceTypeValidationStrict)
	var refusal *endpoint.DeviceTypeConformanceError
	if err := strict.Start(context.Background()); !errors.As(err, &refusal) {
		t.Errorf("strict Start = %v, want a refusal", err)
	}

	off := newTestBridge(t)
	stopBridge(t, off)
	off.SetDeviceTypeValidation(endpoint.DeviceTypeValidationOff)
	if err := off.Start(context.Background()); err != nil {
		t.Fatalf("off Start = %v", err)
	}
	if got := off.DeviceTypeViolations(); len(got) != 0 {
		t.Errorf("off recorded %v", got)
	}
}
