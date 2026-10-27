// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/device"
)

// streamPorts is a camera's four stream ports.
type streamPorts struct{}

func (streamPorts) VideoStreamAllocate(context.Context, csdef.VideoStreamStruct) (uint16, error) {
	return 1, nil
}

func (streamPorts) VideoStreamAllocated(context.Context, csdef.VideoStreamStruct, csdef.StreamAllocationAction) {
}
func (streamPorts) VideoStreamModify(context.Context, uint16, *bool, *bool) error { return nil }
func (streamPorts) VideoStreamDeallocate(context.Context, uint16) error           { return nil }
func (streamPorts) AudioStreamAllocate(context.Context, csdef.AudioStreamStruct) (uint16, error) {
	return 1, nil
}
func (streamPorts) AudioStreamDeallocate(context.Context, uint16) error { return nil }
func (streamPorts) SnapshotStreamAllocate(context.Context, csdef.SnapshotStreamStruct) (uint16, error) {
	return 1, nil
}
func (streamPorts) SnapshotStreamModify(context.Context, uint16, *bool, *bool) error { return nil }
func (streamPorts) SnapshotStreamDeallocate(context.Context, uint16) error           { return nil }
func (streamPorts) CaptureSnapshot(context.Context, *uint16, csdef.VideoResolutionStruct) (csdef.CaptureSnapshotResponse, error) {
	return csdef.CaptureSnapshotResponse{}, nil
}

// TestCameraServesTheKitAvStream: a Camera (VDO, ADO, SNP) declared with
// its stream ports is the kit's CameraAvStreamManagement server, the
// priorities from Initial's SupportedStreamUsages; without the ports the
// cluster keeps the generated server.
func TestCameraServesTheKitAvStream(t *testing.T) {
	t.Parallel()
	p := streamPorts{}
	usages := []csdef.StreamUsageEnum{csdef.StreamUsageLiveView, csdef.StreamUsageRecording}
	d, err := device.NewCamera(device.Options{StableKey: device.StringKey("cam-streams"), Name: "cam"}, csdef.Declare{
		Initial:         csdef.Patch{SupportedStreamUsages: &usages},
		VideoStreamPort: p, AudioStreamPort: p, SnapshotStreamPort: p, SnapshotCapturePort: p,
	})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := d.Server(csdef.ClusterID).(*camera.AvStream)
	if !ok {
		t.Fatalf("CameraAvStreamManagement is %T", d.Server(csdef.ClusterID))
	}
	if got := a.StreamUsagePriorities(); len(got) != 2 || got[0] != csdef.StreamUsageLiveView {
		t.Fatalf("StreamUsagePriorities %v", got)
	}
	plain, err := device.NewCamera(device.Options{StableKey: device.StringKey("cam-plain"), Name: "cam"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.Server(csdef.ClusterID).(*spec.Server); !ok {
		t.Fatalf("CameraAvStreamManagement without ports is %T", plain.Server(csdef.ClusterID))
	}
}

// TestCameraServesTheKitZones: a Camera's optional ZoneManagement declared
// with a ZonePort is the kit's server, its limits from Initial.
func TestCameraServesTheKitZones(t *testing.T) {
	t.Parallel()
	maxZones, sensMax := uint8(3), uint8(4)
	d, err := device.NewCamera(device.Options{StableKey: device.StringKey("cam-zones"), Name: "cam"}, zmdef.Declare{
		Initial: zmdef.Patch{MaxZones: &maxZones, SensitivityMax: &sensMax}, ZonePort: zonePort{},
	})
	if err != nil {
		t.Fatal(err)
	}
	z, ok := d.Server(zmdef.ClusterID).(*camera.Zones)
	if !ok {
		t.Fatalf("ZoneManagement is %T", d.Server(zmdef.ClusterID))
	}
	if v, _ := z.MatterRead(zmdef.AttrMaxZones); v != uint8(3) {
		t.Fatalf("MaxZones %v", v)
	}
}

type zonePort struct{}

func (zonePort) CreateTwoDCartesianZone(context.Context, zmdef.TwoDCartesianZoneStruct) (uint16, error) {
	return 1, nil
}

func (zonePort) UpdateTwoDCartesianZone(context.Context, uint16, zmdef.TwoDCartesianZoneStruct) error {
	return nil
}
func (zonePort) RemoveZone(context.Context, uint16) error                            { return nil }
func (zonePort) CreateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error { return nil }
func (zonePort) UpdateTrigger(context.Context, zmdef.ZoneTriggerControlStruct) error { return nil }
func (zonePort) RemoveTrigger(context.Context, uint16) error                         { return nil }
