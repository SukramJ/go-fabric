// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// The composite device types (brief 27-B §4) pass the node's strict
// device-type validation with their components as parts — matter.js
// counts a simple component on an endpoint below the composite
// (DeviceTypeConformance.ts:596-613 at 862ad75f) — and fail it without
// them (StandardDeviceTypeCorpusTest.ts:328-358, "component endpoints are
// the developer's to add").
func TestCompositeDeviceTypesValidate(t *testing.T) {
	opts := func(key string) device.Options { return device.Options{StableKey: endpoint.StringKey(key), Name: key} }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	camera := func(key string) *device.Camera {
		c, err := device.NewCamera(opts(key))
		must(err)
		return c
	}
	doorbell, err := device.NewDoorbell(opts("doorbell"))
	must(err)
	vd, err := device.NewVideoDoorbell(opts("video-doorbell"), camera("vd-camera"), doorbell)
	must(err)
	light, err := device.NewOnOffLight(opts("flood-light"))
	must(err)
	fc, err := device.NewFloodlightCamera(opts("floodlight"), light, camera("fc-camera"))
	must(err)

	// The node's own root and the Camera's generated default servers carry
	// violations of their own; the composites' are what is judged here.
	compositeViolations := func(lg *bqLog) []bqRecord {
		var out []bqRecord
		for _, r := range violationsLogged(lg) {
			if dt := r.attrs["device_type"]; dt == "VideoDoorbell" || dt == "FloodlightCamera" {
				out = append(out, r)
			}
		}
		return out
	}
	site, lg := loggingSite(t)
	if d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{vd, fc}}); d.Node() == nil {
		t.Fatal("the node did not start")
	}
	if got := compositeViolations(lg); len(got) != 0 {
		t.Errorf("composites logged %v", got)
	}

	// The bare composite, without its components, carries the violations
	// matter.js pins for it.
	bare, err := device.FromSpec(device.Spec{Type: device.VideoDoorbellID, Options: opts("bare")})
	must(err)
	site2, lg2 := loggingSite(t)
	_ = site2.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{bare}})
	got := compositeViolations(lg2)
	if len(got) != 2 || got[0].attrs["kind"] != "instanceCount" {
		t.Errorf("a bare VideoDoorbell logged %v, want its two instanceCount violations", got)
	}
}
