// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"reflect"
	"slices"
	"sync"
	"testing"

	gendiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/contract"
)

// faultHost is a host's fault port with matter.js's convenience methods:
// register appends a fault not yet active, clear removes one
// (GeneralDiagnosticsServer.ts registerHardwareFault, clearHardwareFault
// and their radio and network siblings, :296-411 at db021da0), each
// followed by the notification a host fires after a change.
type faultHost struct {
	mu     sync.Mutex
	faults contract.Faults
	cbs    []func()
}

func (h *faultHost) MatterActiveFaults() contract.Faults {
	h.mu.Lock()
	defer h.mu.Unlock()
	return contract.Faults{Hardware: slices.Clone(h.faults.Hardware), Radio: slices.Clone(h.faults.Radio), Network: slices.Clone(h.faults.Network)}
}

func (h *faultHost) OnMatterValueChanged(cb func()) func() {
	h.mu.Lock()
	h.cbs = append(h.cbs, cb)
	h.mu.Unlock()
	return func() {}
}

// change edits one list and notifies.
func (h *faultHost) change(edit func(f *contract.Faults)) {
	h.mu.Lock()
	edit(&h.faults)
	cbs := slices.Clone(h.cbs)
	h.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

func register(list *[]uint8, v uint8) {
	if !slices.Contains(*list, v) {
		*list = append(*list, v)
	}
}

func clearFault(list *[]uint8, v uint8) {
	*list = slices.DeleteFunc(slices.Clone(*list), func(x uint8) bool { return x == v })
}

// faultEvents records the change events.
type faultEvents struct {
	mu   sync.Mutex
	seen []faultEmitted
}

type faultEmitted struct {
	event             string
	current, previous []uint8
}

func toU8[E ~uint8](in []E) []uint8 {
	out := make([]uint8, len(in))
	for i, v := range in {
		out[i] = uint8(v)
	}
	return out
}

func (e *faultEvents) MatterEmitEvent(_ uint16, clusterID, event uint32, data any, pri contract.EventPriority) {
	if clusterID != gendiagClusterID || pri != contract.EventPriorityCritical {
		return
	}
	var rec faultEmitted
	switch d := data.(type) {
	case gendiagdef.HardwareFaultChangeEvent:
		rec = faultEmitted{"hardware", toU8(d.Current), toU8(d.Previous)}
	case gendiagdef.RadioFaultChangeEvent:
		rec = faultEmitted{"radio", toU8(d.Current), toU8(d.Previous)}
	case gendiagdef.NetworkFaultChangeEvent:
		rec = faultEmitted{"network", toU8(d.Current), toU8(d.Previous)}
	default:
		return
	}
	_ = event
	e.mu.Lock()
	e.seen = append(e.seen, rec)
	e.mu.Unlock()
}

func (e *faultEvents) take() []faultEmitted {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.seen
	e.seen = nil
	return out
}

// newFaultDevice is the test's createFaultDevice (:421-436): the three
// lists enabled and empty, the change events recorded.
func newFaultDevice(t *testing.T) (*GeneralDiagnostics, *faultHost, *faultEvents) {
	t.Helper()
	g := NewGeneralDiagnostics(BootReasonPowerOnReboot)
	host := &faultHost{}
	events := &faultEvents{}
	g.SetMatterEventEmitter(events)
	g.SetFaultSource(host)
	return g, host, events
}

func readFaults(t *testing.T, g *GeneralDiagnostics, attr uint32) []uint8 {
	t.Helper()
	v, ok := g.MatterRead(attr)
	if !ok {
		t.Fatalf("attribute 0x%04X not served", attr)
	}
	l, _ := v.(faultList)
	return []uint8(l)
}

func wantEmitted(t *testing.T, got, want []faultEmitted) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events %+v, want %+v", got, want)
	}
}

// TestGeneralDiagnosticsFaultsParityMatterJS ports the five "faults"
// cases of matter.js packages/node/test/behaviors/general-diagnostics/
// GeneralDiagnosticsServerTest.ts at db021da0 (:420-534) by meaning: the
// host's register and clear become a FaultSource notification, and each
// change of a list emits one change event with the current and the
// previous list.
func TestGeneralDiagnosticsFaultsParityMatterJS(t *testing.T) {
	t.Parallel()
	const (
		hwRadio, hwSensor        = uint8(gendiagdef.HardwareFaultRadio), uint8(gendiagdef.HardwareFaultSensor)
		wifiFault, threadFault   = uint8(gendiagdef.RadioFaultWiFiFault), uint8(gendiagdef.RadioFaultThreadFault)
		jammed, connectionFailed = uint8(gendiagdef.NetworkFaultNetworkJammed), uint8(gendiagdef.NetworkFaultConnectionFailed)
	)

	// :442-464.
	t.Run("registers and clears hardware faults with one event per change", func(t *testing.T) {
		t.Parallel()
		g, host, events := newFaultDevice(t)
		host.change(func(f *contract.Faults) { register(&f.Hardware, hwRadio) })
		host.change(func(f *contract.Faults) { register(&f.Hardware, hwSensor) })
		host.change(func(f *contract.Faults) { clearFault(&f.Hardware, hwRadio) })
		if got := readFaults(t, g, gendiagAttrActiveHardwareFaults); !slices.Equal(got, []uint8{hwSensor}) {
			t.Fatalf("ActiveHardwareFaults %v", got)
		}
		wantEmitted(t, events.take(), []faultEmitted{
			{"hardware", []uint8{hwRadio}, []uint8{}},
			{"hardware", []uint8{hwRadio, hwSensor}, []uint8{hwRadio}},
			{"hardware", []uint8{hwSensor}, []uint8{hwRadio, hwSensor}},
		})
	})

	// :466-478.
	t.Run("registers and clears radio faults with one event per change", func(t *testing.T) {
		t.Parallel()
		g, host, events := newFaultDevice(t)
		host.change(func(f *contract.Faults) { register(&f.Radio, wifiFault) })
		host.change(func(f *contract.Faults) { clearFault(&f.Radio, wifiFault) })
		if got := readFaults(t, g, gendiagAttrActiveRadioFaults); len(got) != 0 {
			t.Fatalf("ActiveRadioFaults %v", got)
		}
		wantEmitted(t, events.take(), []faultEmitted{
			{"radio", []uint8{wifiFault}, []uint8{}},
			{"radio", []uint8{}, []uint8{wifiFault}},
		})
	})

	// :480-492.
	t.Run("registers and clears network faults with one event per change", func(t *testing.T) {
		t.Parallel()
		g, host, events := newFaultDevice(t)
		host.change(func(f *contract.Faults) { register(&f.Network, jammed) })
		host.change(func(f *contract.Faults) { clearFault(&f.Network, jammed) })
		if got := readFaults(t, g, gendiagAttrActiveNetworkFaults); len(got) != 0 {
			t.Fatalf("ActiveNetworkFaults %v", got)
		}
		wantEmitted(t, events.take(), []faultEmitted{
			{"network", []uint8{jammed}, []uint8{}},
			{"network", []uint8{}, []uint8{jammed}},
		})
	})

	// :494-519 (§11.12.6.6: one instance of a value per list).
	t.Run("emits nothing when an active fault is registered again", func(t *testing.T) {
		t.Parallel()
		g, host, events := newFaultDevice(t)
		host.change(func(f *contract.Faults) {
			register(&f.Hardware, hwRadio)
			register(&f.Hardware, hwSensor)
			register(&f.Radio, wifiFault)
			register(&f.Radio, threadFault)
			register(&f.Network, jammed)
			register(&f.Network, connectionFailed)
		})
		events.take()
		host.change(func(f *contract.Faults) { register(&f.Hardware, hwRadio) })
		host.change(func(f *contract.Faults) { register(&f.Radio, wifiFault) })
		host.change(func(f *contract.Faults) { register(&f.Network, jammed) })
		// A host that lists a value twice is served it once.
		host.change(func(f *contract.Faults) { f.Hardware = append(f.Hardware, hwRadio) })
		for attr, want := range map[uint32][]uint8{
			gendiagAttrActiveHardwareFaults: {hwRadio, hwSensor},
			gendiagAttrActiveRadioFaults:    {wifiFault, threadFault},
			gendiagAttrActiveNetworkFaults:  {jammed, connectionFailed},
		} {
			if got := readFaults(t, g, attr); !slices.Equal(got, want) {
				t.Errorf("attribute 0x%04X: %v, want %v", attr, got, want)
			}
		}
		wantEmitted(t, events.take(), nil)
	})

	// :521-533.
	t.Run("emits nothing when an absent fault is cleared", func(t *testing.T) {
		t.Parallel()
		_, host, events := newFaultDevice(t)
		host.change(func(f *contract.Faults) {
			clearFault(&f.Hardware, hwRadio)
			clearFault(&f.Radio, wifiFault)
			clearFault(&f.Network, jammed)
		})
		wantEmitted(t, events.take(), nil)
	})
}

// TestGeneralDiagnosticsFaultsAbsentWithoutPort: without a fault port the
// lists and their events stay off the cluster's surface, as before; with
// one they join the attribute and event lists.
func TestGeneralDiagnosticsFaultsAbsentWithoutPort(t *testing.T) {
	t.Parallel()
	g := NewGeneralDiagnostics(BootReasonPowerOnReboot)
	for _, a := range gendiagFaultAttrs {
		if _, ok := g.MatterRead(a); ok || slices.Contains(g.MatterAttributes(), a) {
			t.Fatalf("attribute 0x%04X served without a fault port", a)
		}
	}
	for _, e := range gendiagFaultEvents {
		if slices.Contains(g.MatterEvents(), e) {
			t.Fatalf("event 0x%02X listed without a fault port", e)
		}
	}
	g.SetFaultSource(&faultHost{})
	for _, a := range gendiagFaultAttrs {
		if _, ok := g.MatterRead(a); !ok || !slices.Contains(g.MatterAttributes(), a) {
			t.Fatalf("attribute 0x%04X not served with a fault port", a)
		}
	}
	for _, e := range gendiagFaultEvents {
		if !slices.Contains(g.MatterEvents(), e) {
			t.Fatalf("event 0x%02X not listed with a fault port", e)
		}
	}
	g.SetFaultSource(nil)
	if slices.Contains(g.MatterAttributes(), gendiagdef.AttrActiveHardwareFaults) {
		t.Fatal("the lists stay after the port is detached")
	}
}
