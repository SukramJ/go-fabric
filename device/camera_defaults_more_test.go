// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/netinfra"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	tdirdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/device"
)

type requestorPort struct{}

func (requestorPort) Offer(context.Context, wrdef.WebRTCSessionStruct, wrdef.OfferRequest) {}
func (requestorPort) Answer(context.Context, wrdef.WebRTCSessionStruct, string)            {}
func (requestorPort) IceCandidates(context.Context, wrdef.WebRTCSessionStruct, []wrdef.ICECandidateStruct) {
}
func (requestorPort) End(context.Context, wrdef.WebRTCSessionStruct, uint8) {}

// TestCameraDefaultsFromTheDeclaration: the declared RequestorPort reaches
// the kit's requestor, the Chime's Initial Enabled is honoured, a
// CameraAvSettings declaration the server refuses keeps the generated
// server (ErrSelectionNotServed), and a ThreadNetworkDirectory without a
// store sizes its in-memory one from Initial.
func TestCameraDefaultsFromTheDeclaration(t *testing.T) {
	t.Parallel()
	d, err := device.NewCameraController(device.Options{StableKey: device.StringKey("ccp"), Name: "cc"},
		wrdef.Declare{RequestorPort: requestorPort{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.WebRtcTransportRequestor().Underlying().(*camera.Requestor); !ok {
		t.Fatalf("requestor with a port is %T", d.WebRtcTransportRequestor().Underlying())
	}

	off := false
	c, err := device.NewChime(device.Options{StableKey: device.StringKey("chime-off"), Name: "chime"},
		chimedef.Declare{Initial: chimedef.Patch{Enabled: &off}, ChimePort: chimePort{}})
	if err != nil {
		t.Fatal(err)
	}
	if st := c.Chime().Get(); st.Enabled {
		t.Fatalf("Enabled %v; want the Initial's false", st.Enabled)
	}

	// MechanicalPan without a MechanicalPTZ port: the server refuses the
	// configuration and the kit keeps the generated server.
	_, err = device.NewCamera(device.Options{StableKey: device.StringKey("ptz-none"), Name: "cam"},
		avdef.Declare{Features: avdef.FeatureMechanicalPan})
	if err != nil && !errors.Is(err, device.ErrSelectionNotServed) {
		t.Fatalf("camera without a PTZ port: %v", err)
	}

	size := uint8(4)
	n, err := device.NewNetworkInfrastructureManager(device.Options{StableKey: device.StringKey("nim"), Name: "nim"},
		tdirdef.Declare{Initial: tdirdef.Patch{ThreadNetworkTableSize: &size}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := n.ThreadNetworkDirectory().Underlying().(*netinfra.ThreadDirectoryServer); !ok {
		t.Fatalf("thread directory is %T", n.ThreadNetworkDirectory().Underlying())
	}
	if st := n.ThreadNetworkDirectory().Get(); st.ThreadNetworkTableSize != 4 {
		t.Fatalf("ThreadNetworkTableSize %d; want the Initial's 4", st.ThreadNetworkTableSize)
	}
}
