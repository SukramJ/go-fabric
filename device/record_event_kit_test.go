// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"errors"
	"log/slog"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/device"
)

type countingEmitter struct{ n int }

func (e *countingEmitter) MatterEmitEvent(uint16, uint32, uint32, any, contract.EventPriority) { e.n++ }

// TestRecordEventWorksOnEveryMountedKitServer (R2-P5-apispec-07,
// coordinator round 2 Q2a): every server the device kit builds with its
// defaults that lists an event, mounted as the bridge mounts it (its
// emitter, then its endpoint), emits through EmitEvent — the call the
// typed Record<Event> makes — on its endpoint: no server answers
// spec.ErrNoEvents (no EmitEvent) or spec.ErrNotMounted.
func TestRecordEventWorksOnEveryMountedKitServer(t *testing.T) {
	t.Parallel()
	checked, built := 0, 0
	for _, ty := range device.Types() {
		dev, err := device.New(ty, device.Options{StableKey: device.StringKey("rec:" + ty.Name), Name: ty.Name, Logger: slog.New(slog.DiscardHandler)}, device.InitialFor(ty.Name)...)
		if err != nil {
			continue // a type whose defaults need a feature selection or a port
		}
		built++
		for _, srv := range dev.Servers() {
			lister, ok := srv.(contract.ClusterEventLister)
			if !ok || len(lister.MatterEvents()) == 0 {
				continue
			}
			em := &countingEmitter{}
			if r, ok := srv.(contract.EventReceiver); ok {
				r.SetMatterEventEmitter(em)
			}
			if r, ok := srv.(contract.EndpointReceiver); ok {
				r.SetEndpoint(5)
			}
			checked++
			emitter, ok := srv.(interface {
				EmitEvent(eventID uint32, data any) error
			})
			if !ok {
				t.Errorf("%s: %T (cluster 0x%04X) has no EmitEvent: Record<Event> answers spec.ErrNoEvents", ty.Name, srv, srv.MatterClusterID())
				continue
			}
			if err := emitter.EmitEvent(lister.MatterEvents()[0], nil); errors.Is(err, spec.ErrNotMounted) || errors.Is(err, spec.ErrNoEvents) {
				t.Errorf("%s: %T (cluster 0x%04X) mounted: EmitEvent %v", ty.Name, srv, srv.MatterClusterID(), err)
			}
		}
	}
	if built < 60 || checked < 10 {
		t.Fatalf("the walk broke: %d device types built, %d event servers checked", built, checked)
	}
	t.Logf("%d event servers checked over %d device types", checked, built)
}
