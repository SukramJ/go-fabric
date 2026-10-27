// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"fmt"
	"maps"

	"github.com/SukramJ/go-fabric/cluster/camera"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/contract"
)

// The kit's camera servers (cluster/camera, brief 26-A1): the
// WebRtcTransportRequestor is the default of every device that mounts the
// cluster — matter.js's CameraControllerDevice with
// WebRtcTransportRequestorServer — and takes its RequestorPort when the
// declaration carries one; Chime and CameraAvSettingsUserLevelManagement
// run on the ports their declarations carry and keep the generated server
// without them. Each takes the values the host persisted from the
// declaration's Initial ([configFrom]). CameraAvStreamManagement and
// ZoneManagement (brief 26-A2) run on the ports their declarations carry
// — a stream port per feature, the ZonePort — and keep the generated
// server without them.

func init() {
	for id, f := range map[uint32]serverFactory{
		wrdef.ClusterID:    newRequestorServer,
		chimedef.ClusterID: newChimeServer,
		avdef.ClusterID:    newAvSettingsServer,
		csdef.ClusterID:    newAvStreamServer,
		zmdef.ClusterID:    newZonesServer,
	} {
		selfHeldDefaults[id] = f
	}
}

// newRequestorServer is the module's WebRtcTransportRequestor server
// ([camera.NewRequestor]); Initial.CurrentSessions registers sessions.
func newRequestorServer(b defaultBuild) (contract.ClusterServer, error) {
	var cfg camera.RequestorConfig
	if d, ok := b.ports.(wrdef.Declare); ok {
		cfg.Port = d.RequestorPort
	}
	cfg.DataVersion = b.cfg.DataVersion
	return camera.NewRequestor(cfg)
}

// newChimeServer is the module's Chime server ([camera.NewChime]) on the
// declared ChimePort; SelectedChime and Enabled start from Initial.
func newChimeServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(chimedef.Declare)
	if d.ChimePort == nil {
		return nil, notServed(fmt.Errorf("chime: %w", errNoPort))
	}
	cfg := camera.ChimeConfig{Port: d.ChimePort, DataVersion: b.cfg.DataVersion}
	if v, ok := b.initial[chimedef.AttrSelectedChime].(uint8); ok {
		cfg.SelectedChime = v
		delete(b.initial, chimedef.AttrSelectedChime)
	}
	if v, ok := b.initial[chimedef.AttrEnabled].(bool); ok {
		cfg.Enabled = &v
		delete(b.initial, chimedef.AttrEnabled)
	}
	return camera.NewChime(cfg)
}

// newAvSettingsServer is the module's CameraAvSettingsUserLevelManagement
// server ([camera.NewAvSettings]) on the declared MechanicalPTZ and
// DigitalPTZ, with the features of the selection; the limits, MaxPresets,
// the position, the presets and the streams start from Initial.
func newAvSettingsServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(avdef.Declare)
	cfg := camera.AvSettingsConfig{
		Features:   camera.AvFeature(b.opts.Features),
		Mechanical: d.MechanicalPTZ, Digital: d.DigitalPTZ,
		DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg, b.def, b.initial)
	srv, err := camera.NewAvSettings(cfg)
	if errors.Is(err, camera.ErrAvSettingsConfig) || errors.Is(err, camera.ErrNoMechanicalPTZ) || errors.Is(err, camera.ErrNoDigitalPTZ) {
		return nil, notServed(err)
	}
	return srv, err
}

// newAvStreamServer is the module's CameraAvStreamManagement server
// ([camera.NewAvStream]) on the declared stream ports, with the features
// and optional attributes of the selection; the persisted streams and
// priorities start from Initial, every other value of Initial is set as
// the server's SetAttributes sets it.
func newAvStreamServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(csdef.Declare)
	cfg := camera.AvStreamConfig{
		Features: camera.AvStreamFeature(b.opts.Features), Attributes: b.opts.Attributes,
		Video: d.VideoStreamPort, Audio: d.AudioStreamPort, Snapshot: d.SnapshotStreamPort, Capture: d.SnapshotCapturePort,
		DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg, b.def, b.initial)
	cfg.Initial = maps.Clone(b.initial)
	clear(b.initial)
	srv, err := camera.NewAvStream(cfg)
	if errors.Is(err, camera.ErrAvStreamConfig) || errors.Is(err, camera.ErrNoStreamPort) {
		return nil, notServed(err)
	}
	return srv, err
}

// newZonesServer is the module's ZoneManagement server ([camera.NewZones])
// on the declared ZonePort; the limits, Sensitivity, the zones and the
// triggers start from Initial.
func newZonesServer(b defaultBuild) (contract.ClusterServer, error) {
	d, _ := b.ports.(zmdef.Declare)
	if d.ZonePort == nil {
		return nil, notServed(fmt.Errorf("zone management: %w", errNoPort))
	}
	cfg := camera.ZonesConfig{Features: camera.ZoneFeature(b.opts.Features), Port: d.ZonePort, DataVersion: b.cfg.DataVersion}
	configFrom(&cfg, b.def, b.initial)
	srv, err := camera.NewZones(cfg)
	if errors.Is(err, camera.ErrZonesConfig) {
		return nil, notServed(err)
	}
	return srv, err
}
