// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/events/EventsBehaviorTest.ts
// at 862ad75f. matter.js adds a device to a MockSite, reads the root's
// BasicInformation StartUp and ShutDown occurrences from its
// OccurrenceManager, and stops and starts the node; the port runs a
// fabrictest device, reads the same events from the node's event log
// (im.EventLog.Query, the OccurrenceManager's counterpart) and restarts it
// on its store (Device.Restart). The Go event store is volatile, as
// matter.js's is by default; "keeps the events of the former run … if
// nonvolatile" needs the option node.Config does not carry (the register
// names why).
package node_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

// basicInformationEvents is basicInformationEvents (:12-24): the root's
// BasicInformation (0x0028) StartUp (0x00) or ShutDown (0x01) records.
func basicInformationEvents(d *fabrictest.Device, eventID uint32) []im.EventRecord {
	return d.Bridge().EventLog().Query(0, 0x0028, eventID, 0)
}

const (
	evStartUp  uint32 = 0x00
	evShutDown uint32 = 0x01
)

// TestEventsBehaviorParityMatterJS ports the describe "EventsBehavior"
// (EventsBehaviorTest.ts:26-55).
func TestEventsBehaviorParityMatterJS(t *testing.T) {
	holdSite(t)

	// :31-43.
	t.Run("starts with no events of the former run after stop() and start()", func(t *testing.T) {
		site := quietSite(t)
		d := site.AddDevice(fabrictest.Topology{})
		first := basicInformationEvents(d, evStartUp)
		if len(first) != 1 {
			t.Fatalf("%d StartUp events before the restart, want 1", len(first))
		}
		if err := d.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if got := basicInformationEvents(d, evShutDown); len(got) != 0 {
			t.Errorf("%d ShutDown events after the restart, want 0", len(got))
		}
		startUps := basicInformationEvents(d, evStartUp)
		if len(startUps) != 1 {
			t.Fatalf("%d StartUp events after the restart, want 1", len(startUps))
		}
		if startUps[0].Number <= first[0].Number {
			t.Errorf("the new StartUp is number %d, not above the first's %d", startUps[0].Number, first[0].Number)
		}
	})
}
