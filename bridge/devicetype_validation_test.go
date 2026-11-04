// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/mdns"
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

// levelRecorder keeps the message and level of every record, at every level.
type levelRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *levelRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *levelRecorder) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *levelRecorder) WithGroup(string) slog.Handler            { return h }
func (h *levelRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r.Clone())
	h.mu.Unlock()
	return nil
}

// requirements are the requirements the records with message msg name at
// level on endpoint ep.
func (h *levelRecorder) requirements(msg string, level slog.Level, ep int64) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for i := range h.records {
		r := &h.records[i]
		if r.Message != msg || r.Level != level {
			continue
		}
		var onEP bool
		var req string
		r.Attrs(func(a slog.Attr) bool {
			switch a.Key {
			case "endpoint":
				onEP = a.Value.Int64() == ep
			case "requirement":
				req = a.Value.String()
			}
			return true
		})
		if onEP {
			out = append(out, req)
		}
	}
	return out
}

// In warn mode the violations of the node endpoint that only a Root
// assertion raises (a bare test root leaves Groupcast so at the pin) are
// logged at debug, each a root-asserted violation the verdict holds, and
// none of them as a warning — matter.js DeviceTypeConformanceService.ts:
// 433-442 at 862ad75f.
func TestDeviceTypeValidationLogsRootAssertedAtDebug(t *testing.T) {
	t.Parallel()
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("light"), DeviceType: 0x0100, FriendlyName: "Light",
		}}}})
	}
	rec := &levelRecorder{}
	b, err := bridge.New(snap, mdns.NewNoop(), bridge.Config{Listen: ":0"}, slog.New(rec))
	if err != nil {
		t.Fatal(err)
	}
	stopBridge(t, b)
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start refused a topology in warn mode: %v", err)
	}
	debug := rec.requirements("matter.devicetype.root_asserted", slog.LevelDebug, 0)
	if len(debug) == 0 {
		t.Fatal("nothing root-asserted logged at debug on the root")
	}
	verdict := b.DeviceTypeViolations()
	for _, req := range debug {
		if !slices.ContainsFunc(verdict, func(v endpoint.DeviceTypeViolation) bool {
			return v.Endpoint == 0 && v.Requirement == req && v.AssertedAtRoot
		}) {
			t.Errorf("%s logged at debug is no root-asserted violation of the root: %+v", req, verdict)
		}
	}
	if warned := rec.requirements("matter.devicetype.violation", slog.LevelWarn, 0); slices.ContainsFunc(warned, func(r string) bool { return slices.Contains(debug, r) }) {
		t.Fatalf("a root-asserted requirement was warned of: %v (debug %v)", warned, debug)
	}
}
