// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// scope names this host's single partition. The assembler garbage-collects
// persisted endpoint identities one scope at a time; a host with one
// partition may use any non-empty constant.
const scope = "demo"

// --- device 1: an on/off light ------------------------------------------

// demoLight is a hand-built stand-in for a real device: an on/off light
// whose state lives in this process. It implements
// [contract.EndpointSource], which is all the assembler needs to give it a
// bridged endpoint, and it hands out its own OnOff cluster server.
type demoLight struct {
	name string

	mu sync.RWMutex
	on bool
}

func newDemoLight(name string) *demoLight { return &demoLight{name: name} }

// MatterDeviceType implements [contract.EndpointSource].
func (d *demoLight) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffLight }

// MatterClusterServers implements [contract.EndpointSource]. The assembler
// adds Descriptor and BridgedDeviceBasicInformation itself; only the
// device-specific surface comes from here.
func (d *demoLight) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{&onOffServer{light: d}}
}

func (d *demoLight) set(on bool) {
	d.mu.Lock()
	d.on = on
	d.mu.Unlock()
}

func (d *demoLight) state() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.on
}

// onOffServer projects [demoLight] onto the OnOff cluster (0x0006).
//
// Ids, the feature bit and the revision all come from the module's onoff
// package rather than being written out here — the revision in particular is
// read from the generated matter.js snapshot, so a schema regeneration moves
// it without an edit on this side.
type onOffServer struct {
	light   *demoLight
	version contract.DataVersionTracker
}

// MatterClusterID implements [contract.ClusterServer].
func (s *onOffServer) MatterClusterID() uint32 { return onoff.ClusterID }

// MatterRead implements [contract.ClusterServer]. FeatureMap and
// ClusterRevision are answered here on purpose: the IM dispatcher
// synthesises the three list globals for a server that does not implement
// them, but never those two — a cluster that stays silent on 0xFFFC/0xFFFD
// answers a controller's very first read with UnsupportedAttribute.
func (s *onOffServer) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case onoff.AttrOnOff:
		return s.light.state(), true
	case cluster.AttrGlobalFeatureMap:
		// No LT: this light supports plain On/Off/Toggle and none of the
		// LT-gated timing attributes below.
		return uint32(0), true
	case cluster.AttrGlobalClusterRevision:
		return uint32(onoff.Revision()), true
	default:
		return nil, false
	}
}

// MatterWrite implements [contract.ClusterServer]. OnOff is command-driven;
// the attribute itself is read-only per Matter §1.5.6.
func (s *onOffServer) MatterWrite(context.Context, uint32, any) error {
	return errors.New("onoff: attribute is read-only")
}

// MatterInvoke implements [contract.ClusterServer]. All three commands are
// status-only, so the response is nil.
func (s *onOffServer) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	switch cmdID {
	case onoff.CmdOn:
		s.apply(true)
	case onoff.CmdOff:
		s.apply(false)
	case onoff.CmdToggle:
		s.apply(!s.light.state())
	default:
		return nil, fmt.Errorf("onoff: unsupported command %#x", cmdID)
	}
	return nil, nil
}

// apply drives the device and bumps the cluster's DataVersion, so a
// controller's DataVersionFilter misses and the next report carries the new
// value.
func (s *onOffServer) apply(on bool) {
	s.light.set(on)
	s.version.Bump()
	slog.Info("light.set", slog.String("device", s.light.name), slog.Bool("on", on))
}

// MatterReportable implements [contract.ClusterServer].
func (s *onOffServer) MatterReportable() []uint32 { return []uint32{onoff.AttrOnOff} }

// MatterAttributes implements [contract.ClusterAttributeLister] so a
// wildcard read enumerates OnOff rather than only the globals.
func (s *onOffServer) MatterAttributes() []uint32 { return []uint32{onoff.AttrOnOff} }

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *onOffServer) MatterAcceptedCommands() []uint32 {
	return []uint32{onoff.CmdOff, onoff.CmdOn, onoff.CmdToggle}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]. OnOff
// emits no response commands.
func (s *onOffServer) MatterGeneratedCommands() []uint32 { return []uint32{} }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *onOffServer) MatterDataVersion() uint32 { return s.version.Current() }

// --- device 2: a temperature sensor -------------------------------------

// demoThermometer is a measurement-only device. It implements
// [contract.FloatMeasurementSource] and nothing else: the assembler picks
// the cluster (TemperatureMeasurement 0x0402) and the standalone device type
// from the declared class, so no cluster server is written on this side.
type demoThermometer struct {
	mu      sync.RWMutex
	celsius float64
}

func newDemoThermometer(celsius float64) *demoThermometer {
	return &demoThermometer{celsius: celsius}
}

// MatterMeasurementClass implements [contract.MeasurementSource].
func (t *demoThermometer) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementTemperature
}

// MatterFloatValue implements [contract.FloatMeasurementSource]. The unit is
// the model's own — °C for temperature; the cluster server converts.
func (t *demoThermometer) MatterFloatValue() (float64, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.celsius, true
}

// --- the fleet ----------------------------------------------------------

// fleet is the hard-coded device list this daemon bridges, plus the
// assembler that turns it into a topology.
type fleet struct {
	light       *demoLight
	thermometer *demoThermometer
	assembler   *endpoint.Assembler
}

func newFleet(store endpoint.Store, cfg endpoint.Config, logger *slog.Logger) (*fleet, error) {
	asm, err := endpoint.New(store, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("endpoint assembler: %w", err)
	}
	return &fleet{
		light:       newDemoLight("Desk Lamp"),
		thermometer: newDemoThermometer(21.5),
		assembler:   asm,
	}, nil
}

// snapshotter is what the bridge calls at Start and on every Reassemble. It
// walks this host's model — here, two hard-coded devices — describes each as
// a flat [endpoint.Spec], and hands the assembled topology back.
//
// StableKey is the load-bearing field: it decides which endpoint number the
// device gets back after a restart, so it must render byte-for-byte
// identically for the same device across releases.
func (f *fleet) snapshotter(ctx context.Context) (*endpoint.Topology, error) {
	specs := []endpoint.Spec{
		{
			StableKey:      endpoint.StringKey("demo:light:1"),
			DeviceAddress:  "demo-light-1",
			ChannelAddress: "demo-light-1:0",
			DeviceType:     onoff.DeviceTypeOnOffLight,
			FriendlyName:   f.light.name,
			Source:         f.light,
		},
		{
			StableKey:      endpoint.StringKey("demo:thermometer:1"),
			DeviceAddress:  "demo-thermometer-1",
			ChannelAddress: "demo-thermometer-1:0",
			DeviceType:     contract.MeasurementClassDeviceType(contract.MeasurementTemperature),
			FriendlyName:   "Study Thermometer",
			Measurement:    f.thermometer,
		},
	}
	return f.assembler.Assemble(ctx, []endpoint.Snapshot{{
		Scope:     scope,
		Endpoints: specs,
		// The fleet is hard-coded, so it is authoritative from the first
		// call. A host whose model loads asynchronously must report false
		// until the load finishes, or the assembler's vanished-source
		// collection wipes every persisted endpoint number at boot.
		ModelComplete: true,
	}})
}
