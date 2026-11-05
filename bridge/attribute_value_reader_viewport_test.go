// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

type viewportPorts struct{}

func (viewportPorts) VideoStreamAllocate(context.Context, csdef.VideoStreamStruct) (uint16, error) {
	return 1, nil
}

func (viewportPorts) VideoStreamAllocated(context.Context, csdef.VideoStreamStruct, csdef.StreamAllocationAction) {
}
func (viewportPorts) VideoStreamModify(context.Context, uint16, *bool, *bool) error { return nil }
func (viewportPorts) VideoStreamDeallocate(context.Context, uint16) error           { return nil }

// TestAttributeValueReader_Viewport: a write of CameraAvStreamManagement's
// ViewportStruct decodes through the generated definition, so the server
// takes it (TC-AVSM-2.12 step 18 was answered CONSTRAINT_ERROR while the
// struct decoded to the primitive form).
func TestAttributeValueReader_Viewport(t *testing.T) {
	t.Parallel()
	path := im.ConcreteAttributePath{HasCluster: true, HasAttribute: true, Cluster: csdef.ClusterID, Attribute: csdef.AttrViewport}
	enc := tlv.NewEncoder()
	csdef.ViewportStruct{X1: 1, Y1: 1, X2: 1921, Y2: 1081}.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(b)
	av, err := attributeValueReader(path, advanceToContent(t, dec), dec)
	if err != nil {
		t.Fatal(err)
	}
	sensor := csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30}
	srv, err := camera.NewAvStream(camera.AvStreamConfig{
		Features: camera.FeatureVideo, Video: viewportPorts{},
		Initial: map[uint32]any{
			csdef.AttrVideoSensorParams:     sensor,
			csdef.AttrMinViewportResolution: csdef.VideoResolutionStruct{Width: 640, Height: 360},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(context.Background(), csdef.AttrViewport, av.Value); err != nil {
		t.Fatalf("the decoded Viewport %T was refused: %v", av.Value, err)
	}
}
