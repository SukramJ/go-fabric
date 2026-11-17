// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
)

// closeCountingAdvertiser is a Noop advertiser that counts its Close calls.
type closeCountingAdvertiser struct {
	*mdns.Noop
	closed atomic.Int32
}

func (a *closeCountingAdvertiser) Close() error {
	a.closed.Add(1)
	return a.Noop.Close()
}

// quiesceCounter is a root cluster server that counts its quiesces.
type quiesceCounter struct{ quiesced atomic.Int32 }

func (*quiesceCounter) MatterClusterID() uint32                                { return 0xFFF1FC10 }
func (*quiesceCounter) MatterRead(uint32) (any, bool)                          { return nil, false }
func (*quiesceCounter) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*quiesceCounter) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*quiesceCounter) MatterReportable() []uint32                             { return nil }
func (*quiesceCounter) MatterAttributes() []uint32                             { return nil }
func (q *quiesceCounter) MatterQuiesce()                                       { q.quiesced.Add(1) }

// doneWatchingContext closes asked the first time its Done channel is
// asked for: a wait on the context has begun.
type doneWatchingContext struct {
	context.Context
	once  sync.Once
	asked chan struct{}
}

func (c *doneWatchingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.asked) })
	return c.Context.Done()
}

// withBridgedEndpoint is the empty fleet plus one bridged endpoint, 2.
func withBridgedEndpoint(ctx context.Context) (*endpoint.Topology, error) {
	topo, err := wbEmptySnapshotter(ctx)
	if err != nil {
		return nil, err
	}
	topo.Endpoints = append(topo.Endpoints, &endpoint.Endpoint{ID: 2})
	return topo, nil
}

// TestReassembleInstallsTheNewestSnapshot: two Reassembles overlap, the
// first holding its snapshot while the second is called. The topology live
// afterwards is the one the Snapshotter returned last — before, the second
// swapped in first and the first, older snapshot replaced it and stayed
// live (review II, R2-P6-conc-0603).
func TestReassembleInstallsTheNewestSnapshot(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	firstSnapped, releaseFirst := make(chan struct{}), make(chan struct{})
	var last atomic.Pointer[endpoint.Topology]
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		topo, err := wbEmptySnapshotter(ctx)
		last.Store(topo)
		if calls.Add(1) == 2 { // the first Reassemble; 1 is Start's
			close(firstSnapped)
			<-releaseFirst
		}
		return topo, err
	}
	b, err := New(snap, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })

	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- b.Reassemble(context.Background()) }()
	<-firstSnapped
	second := &doneWatchingContext{Context: context.Background(), asked: make(chan struct{})}
	go func() { secondDone <- b.Reassemble(second) }()
	// Serialised, the second waits for the first's assembly — it asks for
	// its context's Done channel to wait on; unserialised, it runs to its
	// end beside the first.
	select {
	case <-second.asked:
	case err := <-secondDone:
		secondDone <- err
	}
	close(releaseFirst)
	for _, done := range []chan error{firstDone, secondDone} {
		if err := <-done; err != nil {
			t.Fatalf("Reassemble: %v", err)
		}
	}
	if got, want := b.Topology(), last.Load(); got != want {
		t.Error("the live topology is not the snapshot taken last: an older snapshot replaced a newer one")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("snapshots taken = %d, want 3", got)
	}
}

// TestReassembleWaitsForTheAssemblyUntilItsContextEnds: a Reassemble that
// finds another in progress waits for it no longer than its context.
func TestReassembleWaitsForTheAssemblyUntilItsContextEnds(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	firstSnapped, releaseFirst := make(chan struct{}), make(chan struct{})
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		if calls.Add(1) == 2 {
			close(firstSnapped)
			<-releaseFirst
		}
		return wbEmptySnapshotter(ctx)
	}
	b, err := New(snap, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
	firstDone := make(chan error, 1)
	go func() { firstDone <- b.Reassemble(context.Background()) }()
	<-firstSnapped
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Reassemble(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Reassemble beside one in progress, context done: %v, want context.Canceled", err)
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Reassemble: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("snapshots taken = %d, want 2 (the refused Reassemble takes none)", got)
	}
}

// TestAttachedDescriptorGetsTheLivePartsList: the root's and the
// Aggregator's Descriptor serve the bridge's live PartsList whether the
// host attaches them before Start or after it — before, a Descriptor
// attached after Start kept its static, empty list (review II,
// R2-P6-api-0604) — and a provider the host attached stays.
func TestAttachedDescriptorGetsTheLivePartsList(t *testing.T) {
	t.Parallel()
	descriptor := func(t *testing.T, deviceType uint32) *mattercore.Descriptor {
		t.Helper()
		d, err := mattercore.NewDescriptor([]mattercore.DeviceTypeStruct{{DeviceType: deviceType, Revision: 1}}, []uint32{0x001D}, nil, nil)
		if err != nil {
			t.Fatalf("NewDescriptor: %v", err)
		}
		return d
	}
	type endpointCase struct {
		name       string
		deviceType uint32
		attach     func(*Bridge, []contract.ClusterServer)
		provide    func(*Bridge, func() []uint16) bool
		want       []uint16
	}
	endpoints := []endpointCase{
		{"root", 0x0016, (*Bridge).AttachRootClusters, (*Bridge).AttachRootPartsListProvider, []uint16{1, 2}},
		{"Aggregator", 0x000E, (*Bridge).AttachAggregatorClusters, (*Bridge).AttachAggregatorPartsListProvider, []uint16{2}},
	}
	for _, ep := range endpoints {
		for _, afterStart := range []bool{false, true} {
			for _, hostProvider := range []bool{false, true} {
				name := ep.name + "/attached before Start"
				if afterStart {
					name = ep.name + "/attached after Start"
				}
				if hostProvider {
					name += "/with the host's provider"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					b, err := New(withBridgedEndpoint, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
					if err != nil {
						t.Fatalf("New: %v", err)
					}
					d := descriptor(t, ep.deviceType)
					attach := func() {
						ep.attach(b, []contract.ClusterServer{d})
						if hostProvider && !ep.provide(b, func() []uint16 { return []uint16{42} }) {
							t.Fatal("the host's provider was not installed")
						}
					}
					if !afterStart {
						attach()
					}
					if err := b.Start(context.Background()); err != nil {
						t.Fatalf("Start: %v", err)
					}
					t.Cleanup(func() { _ = b.Stop(context.Background()) })
					if afterStart {
						attach()
					}
					want := ep.want
					if hostProvider {
						want = []uint16{42}
					}
					v, ok := d.MatterRead(0x0003)
					got, _ := v.([]uint16)
					if !ok || !slices.Equal(got, want) {
						t.Errorf("PartsList = %v (ok %v), want %v", v, ok, want)
					}
				})
			}
		}
	}
}

// TestStopOfAnUnstartedBridgeClosesWhatItHolds: Stop on a bridge never
// started, or whose Start failed, withdraws the records the host published
// on its advertiser, closes the advertiser once, and closes the
// commissioning window, which opens no more — before, it returned before
// any of that and the dead bridge kept a commissionable record on the link
// and an open window (review II, R2-P6-fsm-0605).
func TestStopOfAnUnstartedBridgeClosesWhatItHolds(t *testing.T) {
	t.Parallel()
	for _, failedStart := range []bool{false, true} {
		name := "never started"
		if failedStart {
			name = "Start failed"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			adv := &closeCountingAdvertiser{Noop: mdns.NewNoop()}
			cfg := Config{Listen: "127.0.0.1:0"}
			if failedStart {
				cfg.Listen = "999.999.999.999:5540"
			}
			b, err := New(wbEmptySnapshotter, adv, cfg, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			w := NewCommissioningWindow()
			b.AttachCommissioningWindow(w)
			ctx := context.Background()
			if err := b.AnnounceCommissioning(ctx, CommissioningAdvertisement{Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8000, CommissioningMode: 1}); err != nil {
				t.Fatalf("AnnounceCommissioning: %v", err)
			}
			if err := w.OpenOwnWindow(ctx, time.Hour); err != nil {
				t.Fatalf("OpenOwnWindow: %v", err)
			}
			if failedStart {
				if err := b.Start(ctx); err == nil {
					t.Fatal("Start on an unresolvable address succeeded")
				}
			}
			for range 2 {
				if err := b.Stop(ctx); err != nil {
					t.Fatalf("Stop: %v", err)
				}
			}
			if got := adv.closed.Load(); got != 1 {
				t.Errorf("advertiser Close calls after two Stops = %d, want 1", got)
			}
			if active := adv.Active(); len(active) != 0 {
				t.Errorf("records still published after Stop: %d", len(active))
			}
			if w.IsOpen() {
				t.Error("the commissioning window is open after Stop")
			}
			if err := w.OpenOwnWindow(ctx, time.Hour); !errors.Is(err, ErrStopped) {
				t.Errorf("OpenOwnWindow after Stop: %v, want ErrStopped", err)
			}
		})
	}
}

// TestFailedStartTakesItsAssemblyBack: a Start that fails after its initial
// assembly leaves no topology or dispatcher installed, quiesces the servers
// it assembled, and leaves the device-type validator with no record of the
// topology it never served, in the mode the host set — before, Topology()
// and Dispatcher() returned the assembled tree and the validator held its
// violations as reported (review II, R2-P6-api-0606).
func TestFailedStartTakesItsAssemblyBack(t *testing.T) {
	t.Parallel()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "999.999.999.999:5540"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	q := &quiesceCounter{}
	b.AttachRootClusters([]contract.ClusterServer{q})
	b.SetDeviceTypeValidation(endpoint.DeviceTypeValidationWarn)
	if err := b.Start(context.Background()); err == nil {
		t.Fatal("Start on an unresolvable address succeeded")
	}
	if b.Topology() != nil {
		t.Error("Topology() after a failed Start is not nil")
	}
	if b.Dispatcher() != nil {
		t.Error("Dispatcher() after a failed Start is not nil")
	}
	if got := b.DeviceTypeViolations(); len(got) != 0 {
		t.Errorf("DeviceTypeViolations after a failed Start = %d, want none recorded", len(got))
	}
	if mode := b.deviceTypeValidator().Mode(); mode != endpoint.DeviceTypeValidationWarn {
		t.Errorf("validation mode after a failed Start = %v, want warn", mode)
	}
	if got := q.quiesced.Load(); got == 0 {
		t.Error("a server of the failed Start's assembly was not quiesced")
	}
}

// TestStopStopsThePASECloseTimer: the PASE close a CommissioningComplete
// armed for after its grace is stopped by Stop, and none is armed after it
// — before, it ran on an untracked timer that outlived Stop (review II,
// R2-P6-fsm-0615).
func TestStopStopsThePASECloseTimer(t *testing.T) {
	t.Parallel()
	for _, armAfterStop := range []bool{false, true} {
		name := "armed before Stop"
		if armAfterStop {
			name = "response sent after Stop"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b, _, clk := paseCloseBridge(t)
			after := &im.AfterResponse{}
			ctx := im.WithAfterResponse(context.Background(), after)
			var closings atomic.Int32
			if ids := b.closePASESessions(ctx, paseCloseGrace, func([]uint16) { closings.Add(1) }); len(ids) != 1 {
				t.Fatalf("closePASESessions named %v, want one session", ids)
			}
			if !armAfterStop {
				after.Run()
				if got := clk.Pending(); got != 1 {
					t.Fatalf("timers armed after the response = %d, want 1", got)
				}
			}
			if err := b.Stop(context.Background()); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			if armAfterStop {
				after.Run()
			}
			if got := clk.Pending(); got != 0 {
				t.Errorf("timers armed after Stop = %d, want 0", got)
			}
			clk.Advance(paseCloseGrace)
			if got := closings.Load(); got != 0 {
				t.Errorf("PASE closes run after Stop = %d, want 0", got)
			}
		})
	}
}

// TestStrictValidationSetAfterStartJudgesEveryEndpointAsAdded pins what
// [Bridge.SetDeviceTypeValidation] states for a call after Start: the new
// validator knows no endpoint, so the next Reassemble judges every endpoint
// as an addition and strict refuses the violations of endpoints already
// served (review II, R2-P6-api-0611).
func TestStrictValidationSetAfterStartJudgesEveryEndpointAsAdded(t *testing.T) {
	t.Parallel()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
	if len(b.DeviceTypeViolations()) == 0 {
		t.Fatal("the served topology violates nothing: the scenario needs a violation on a served endpoint")
	}
	served := b.Topology()
	b.SetDeviceTypeValidation(endpoint.DeviceTypeValidationStrict)
	var refusal *endpoint.DeviceTypeConformanceError
	if err := b.Reassemble(context.Background()); !errors.As(err, &refusal) {
		t.Fatalf("Reassemble of the unchanged topology under strict set after Start: %v, want a DeviceTypeConformanceError", err)
	}
	if b.Topology() != served {
		t.Error("the refused topology replaced the served one")
	}
}

// TestReassembleQueuedAcrossStopTakesNoSnapshot: a Reassemble that waited
// for the assembly slot while a Stop ended the run returns ErrNotStarted
// without calling the host's Snapshotter (review II, R2-P6-conc-0603).
func TestReassembleQueuedAcrossStopTakesNoSnapshot(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	firstSnapped, releaseFirst := make(chan struct{}), make(chan struct{})
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		if calls.Add(1) == 2 { // the first Reassemble; 1 is Start's
			close(firstSnapped)
			<-releaseFirst
		}
		return wbEmptySnapshotter(ctx)
	}
	b, err := New(snap, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- b.Reassemble(context.Background()) }()
	<-firstSnapped
	second := &doneWatchingContext{Context: context.Background(), asked: make(chan struct{})}
	go func() { secondDone <- b.Reassemble(second) }()
	<-second.asked // waiting for the slot
	if err := b.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	close(releaseFirst)
	for i, done := range []chan error{firstDone, secondDone} {
		if err := <-done; !errors.Is(err, ErrNotStarted) {
			t.Errorf("Reassemble %d across Stop: %v, want ErrNotStarted", i+1, err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("snapshots taken = %d, want 2 (the queued Reassemble takes none)", got)
	}
}
