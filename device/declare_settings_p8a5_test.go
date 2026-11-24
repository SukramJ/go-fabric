// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
)

// TestKitSettingsBesideHostServerOrWithoutPortRefused (issue #600,
// R5-err-5302 = R5-api-5503): a declaration's kit settings — the
// WebRtcTransportProvider session bounds — are the kit server's. Beside a
// HostServer they are ErrConfiguration, as a port is; without the
// ProviderPort a negative bound is refused as negative and a positive
// one as unserved, instead of both being accepted and ignored. A plain
// HostServer is still accepted.
func TestKitSettingsBesideHostServerOrWithoutPortRefused(t *testing.T) {
	t.Parallel()
	streams := csdef.Declare{
		VideoStreamPort: boundStreams{}, AudioStreamPort: boundStreams{},
		SnapshotStreamPort: boundStreams{}, SnapshotCapturePort: boundStreams{},
	}
	host := func(t *testing.T) *camera.Provider {
		t.Helper()
		srv, err := camera.NewProvider(camera.ProviderConfig{Port: boundProvider{}})
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	if _, err := newWith(CameraType(), testOptions("plain-host"), streams, wpdef.Declare{HostServer: host(t)}); err != nil {
		t.Fatalf("a plain HostServer: %v, want accepted", err)
	}
	for _, decl := range []wpdef.Declare{
		{HostServer: host(t), MaxSessions: 1},
		{HostServer: host(t), MaxSessionsPerFabric: 1},
	} {
		if _, err := newWith(CameraType(), testOptions("host-settings"), streams, decl); !errors.Is(err, ErrConfiguration) {
			t.Errorf("%+v: %v, want ErrConfiguration", decl, err)
		}
	}
	for _, decl := range []wpdef.Declare{{MaxSessions: -1}, {MaxSessionsPerFabric: -1}} {
		if _, err := newWith(CameraType(), testOptions("negative-no-port"), streams, decl); !errors.Is(err, camera.ErrNegativeMaxSessions) {
			t.Errorf("%+v: %v, want ErrNegativeMaxSessions", decl, err)
		}
	}
	for _, decl := range []wpdef.Declare{{MaxSessions: 1}, {MaxSessionsPerFabric: 1}} {
		if _, err := newWith(CameraType(), testOptions("bound-no-port"), streams, decl); err == nil {
			t.Errorf("%+v without a ProviderPort: accepted, want refused", decl)
		}
	}
}
