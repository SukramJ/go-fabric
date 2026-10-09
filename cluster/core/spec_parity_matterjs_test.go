// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	diaglogsdef "github.com/SukramJ/go-fabric/cluster/spec/diagnosticlogs"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	gendiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	timesyncdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/contract"
)

// listedGlobals hides the globals a server lists in its AttributeList
// itself (Descriptor does, for Apple Home's cache) from the conformance
// check, which judges the cluster's own attributes.
type listedGlobals struct {
	contract.ClusterServer
}

func (l listedGlobals) MatterAttributes() []uint32 {
	return slices.DeleteFunc(l.ClusterServer.(contract.ClusterAttributeLister).MatterAttributes(), func(id uint32) bool {
		return id >= spec.AttrGeneratedCommandList
	})
}

// TestDescriptorMatchesTheGeneratedDefinition holds Descriptor against
// matter.js descriptor.element.ts for the selection it serves (no
// feature: TagList stays out).
func TestDescriptorMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	d, err := NewDescriptor([]DeviceTypeStruct{{DeviceType: 0x0100, Revision: 3}}, []uint32{0x001D}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, listedGlobals{d}, descdef.Definition, 0)
	spectest.RoundTrip(t, DeviceTypeStruct{DeviceType: 0x0100, Revision: 3}, spec.DecodeStruct[DeviceTypeStruct])
}

// TestIdentifyMatchesTheGeneratedDefinition holds Identify against
// matter.js identify.element.ts (cluster/spec/identify, ADR 0013), and
// pins that the generated IdentifyRequest the bridge now decodes sets
// IdentifyTime.
func TestIdentifyMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	id := NewIdentify()
	defer id.Close()
	spectest.CheckServer(t, id, identifydef.Definition, 0)

	for _, fields := range []any{identifydef.IdentifyRequest{IdentifyTime: 30}, &identifydef.IdentifyRequest{IdentifyTime: 30}} {
		if _, err := id.MatterInvoke(context.Background(), identifydef.CmdIdentify, fields); err != nil {
			t.Fatalf("%T: %v", fields, err)
		}
		if v, _ := id.MatterRead(identifydef.AttrIdentifyTime); v.(uint16) < 29 {
			t.Errorf("%T: IdentifyTime = %v, want 30", fields, v)
		}
	}
	if _, err := id.MatterInvoke(context.Background(), identifydef.CmdIdentify, (*identifydef.IdentifyRequest)(nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := id.MatterInvoke(context.Background(), identifydef.CmdTriggerEffect, identifydef.TriggerEffectRequest{}); err != nil {
		t.Errorf("TriggerEffect: %v", err)
	}
}

// TestBasicInformationMatchesTheGeneratedDefinition holds BasicInformation
// against matter.js basic-information.element.ts, bare and with every
// optional string and ProductAppearance set, and round-trips the
// structures and event payloads it reports through the generated codecs.
func TestBasicInformationMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	cfg := Config{VendorID: 0x1234, ProductID: 0x5678, NodeLabel: "bridge", VendorName: "go-fabric", ProductName: "Bridge"}
	b, err := NewBasicInformation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, b, bidef.Definition, 0)
	cfg.ManufacturingDate, cfg.PartNumber, cfg.ProductURL, cfg.ProductLabel = "20260101", "PN", "https://example.org", "Label"
	cfg.ProductAppearance = ProductAppearanceStruct{Finish: 1, PrimaryColor: PrimaryColorAbsent}
	full, err := NewBasicInformation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, full, bidef.Definition, 0)

	v, _ := full.MatterRead(bidef.AttrProductAppearance)
	pa, err := spec.DecodeStruct[bidef.ProductAppearanceStruct](spectest.Decode(t, spectest.Encode(t, v.(ProductAppearanceStruct))))
	if err != nil || pa.Finish != 1 || !pa.PrimaryColor.Null {
		t.Errorf("ProductAppearance decoded as %+v (%v)", pa, err)
	}
	v, _ = full.MatterRead(bidef.AttrCapabilityMinima)
	cm, err := spec.DecodeStruct[bidef.CapabilityMinimaStruct](spectest.Decode(t, spectest.Encode(t, v.(CapabilityMinimaStruct))))
	if err != nil || cm.CaseSessionsPerFabric < 3 || cm.ReadPathsSupported == nil || *cm.ReadPathsSupported != 20 {
		t.Errorf("CapabilityMinima decoded as %+v (%v)", cm, err)
	}
	spectest.RoundTripEvent(t, bidef.Definition, bidef.EventStartUp, StartUpEvent{SoftwareVersion: 7})
	spectest.RoundTripEvent(t, bidef.Definition, bidef.EventShutDown, ShutDownEvent{})
	spectest.RoundTripEvent(t, bidef.Definition, bidef.EventLeave, LeaveEvent{FabricIndex: 2})
}

// TestBridgedDeviceBasicInformationMatchesTheGeneratedDefinition holds
// the bridged server against bridged-device-basic-information.element.ts,
// bare and with every optional attribute set.
func TestBridgedDeviceBasicInformationMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	bare, err := NewBridgedDeviceBasicInformation(BridgedConfig{NodeLabel: "Lamp", UniqueID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, bare, bdbidef.Definition, 0)
	full, err := NewBridgedDeviceBasicInformation(BridgedConfig{
		NodeLabel: "Lamp", UniqueID: "u1", VendorName: "V", VendorID: 0x1234, ProductName: "P", ProductID: 1,
		HardwareVersion: 1, HardwareVersionStr: "1", SoftwareVersion: 1, SoftwareVersionStr: "1",
		ManufacturingDate: "20260101", PartNumber: "PN", ProductURL: "https://example.org", ProductLabel: "L",
		SerialNumber: "S", ProductAppearance: ProductAppearanceStruct{Finish: 2, PrimaryColor: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, full, bdbidef.Definition, 0)
	spectest.RoundTripEvent(t, bdbidef.Definition, bdbidef.EventReachableChanged, ReachableChangedEvent{ReachableNewValue: true})
}

// TestGeneralDiagnosticsMatchesTheGeneratedDefinition holds
// GeneralDiagnostics against general-diagnostics.element.ts (DMTEST), and
// drives PayloadTestRequest and TimeSnapshot with the generated request
// and response payloads the bridge now decodes and encodes.
func TestGeneralDiagnosticsMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	g := NewGeneralDiagnostics(BootReasonPowerOnReboot)
	spectest.CheckServer(t, g, gendiagdef.Definition, uint32(gendiagdef.FeatureDataModelTest))

	key := []byte("0123456789abcdef")
	if err := g.EnableTestEventTriggers(key, func(context.Context, uint64) error { return nil }); err != nil {
		t.Fatal(err)
	}
	resp, err := g.MatterInvoke(context.Background(), gendiagdef.CmdPayloadTestRequest,
		gendiagdef.PayloadTestRequestRequest{EnableKey: key, Value: 0xAB, Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	spectest.RoundTripCommand(t, gendiagdef.Definition, gendiagdef.CmdPayloadTestResponse, spec.Response, resp.(PayloadTestResponse))
	if _, err := g.MatterInvoke(context.Background(), gendiagdef.CmdTestEventTrigger,
		TestEventTriggerRequest{EnableKey: key, EventTrigger: 1}); err != nil {
		t.Errorf("TestEventTrigger: %v", err)
	}
	resp, err = g.MatterInvoke(context.Background(), gendiagdef.CmdTimeSnapshot, gendiagdef.TimeSnapshotRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ts, err := gendiagdef.Definition.Command(gendiagdef.CmdTimeSnapshotResponse, spec.Response).Decode(spectest.Decode(t, spectest.Encode(t, resp.(TimeSnapshotResponse))))
	if r, _ := ts.(gendiagdef.TimeSnapshotResponse); err != nil || !r.PosixTimeMs.Null {
		t.Errorf("TimeSnapshotResponse decoded as %+v (%v), want a null PosixTimeMs without a UTC clock", ts, err)
	}
	if v, _ := g.MatterRead(gendiagdef.AttrNetworkInterfaces); v != nil {
		if _, err := gendiagdef.Definition.Attribute(gendiagdef.AttrNetworkInterfaces).Decode(spectest.Decode(t, spectest.Encode(t, NetworkInterfaceList(v.([]NetworkInterfaceStruct))))); err != nil {
			t.Errorf("NetworkInterfaces do not decode: %v", err)
		}
	}
	spectest.RoundTripEvent(t, gendiagdef.Definition, gendiagdef.EventBootReason, gendiagdef.BootReasonEvent{BootReason: 1})
}

// TestGeneralCommissioningMatchesTheGeneratedDefinition holds
// GeneralCommissioning against general-commissioning.element.ts for the
// selection it serves (no TC).
func TestGeneralCommissioningMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	g, err := NewGeneralCommissioning(GeneralCommissioningConfig{})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, g, gencommdef.Definition, 0)
}

// TestSmallSystemServersMatchTheGeneratedDefinitions holds TimeSynchronization,
// Binding, ICDManagement, NetworkCommissioning (Ethernet) and
// DiagnosticLogs against their matter.js elements, and round-trips the
// RetrieveLogsResponse the bridge now encodes through the generated codec.
func TestSmallSystemServersMatchTheGeneratedDefinitions(t *testing.T) {
	t.Parallel()
	spectest.CheckServer(t, NewTimeSynchronization(), timesyncdef.Definition, 0)
	spectest.CheckServer(t, NewBinding(), bindingdef.Definition, 0)
	spectest.CheckServer(t, NewICDManagement(), icddef.Definition, 0)
	spectest.CheckServer(t, NewNetworkCommissioning(NetworkCommissioningConfig{}), netcommdef.Definition, NetworkCommFeatureEthernet)
	d := NewDiagnosticLogs()
	spectest.CheckServer(t, d, diaglogsdef.Definition, 0)

	resp, err := d.MatterInvoke(context.Background(), diaglogsdef.CmdRetrieveLogsRequest,
		diaglogsdef.RetrieveLogsRequestRequest{Intent: diaglogsdef.IntentCrashLogs})
	if err != nil {
		t.Fatal(err)
	}
	r := resp.(RetrieveLogsResponse)
	got, err := diaglogsdef.Definition.Command(diaglogsdef.CmdRetrieveLogsResponse, spec.Response).Decode(spectest.Decode(t, spectest.Encode(t, r)))
	if g, _ := got.(diaglogsdef.RetrieveLogsResponse); err != nil || g.Status != diaglogsdef.StatusNoLogs || g.UtcTimeStamp == nil || g.TimeSinceBoot == nil {
		t.Errorf("RetrieveLogsResponse decoded as %+v (%v)", got, err)
	}
	if c, id := r.ResponseCommand(); c != diaglogsdef.ClusterID || id != diaglogsdef.CmdRetrieveLogsResponse {
		t.Errorf("RetrieveLogsResponse names 0x%04X/0x%02X", c, id)
	}
}

// TestOTARequestorMatchesTheGeneratedDefinition holds the OTA requestor
// against ota-software-update-requestor.element.ts. It lists no events,
// though matter.js makes StateTransition, VersionApplied and DownloadError
// mandatory — the server never updates, so it never emits them; ADR 0013
// records it as what remains.
func TestOTARequestorMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	spectest.CheckServer(t, NewOTASoftwareUpdateRequestor(), otadef.Definition, 0)
}
