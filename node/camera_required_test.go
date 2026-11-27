// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"

// cameraRequired is the CameraAvStreamManagement state device.New requires
// of a Camera (decision 17), with the values of matter.js's corpus
// fixtures (packages/node/test/endpoint/validation/StandardDeviceTypeCorpusTest.ts
// at db91886c: the microphone :132-137, the video sensor :139-143, the
// minimum viewport :162, the viewport :166).
func cameraRequired() csdef.Declare {
	return csdef.Declare{Initial: csdef.Patch{
		MicrophoneCapabilities: &csdef.AudioCapabilitiesStruct{
			MaxNumberOfChannels: 1, SupportedCodecs: []csdef.AudioCodecEnum{csdef.AudioCodecOpus},
			SupportedSampleRates: []uint32{48000}, SupportedBitDepths: []uint8{16},
		},
		VideoSensorParams:     &csdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30},
		MinViewportResolution: &csdef.VideoResolutionStruct{Width: 640, Height: 480},
		Viewport:              &csdef.ViewportStruct{X1: 0, Y1: 0, X2: 1920, Y2: 1080},
	}}
}
