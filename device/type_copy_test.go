// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	airqualitydef "thingfabric.dev/go-fabric/cluster/spec/airquality"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
)

// TestRegistryUnreachable (R2-P5-apidev-03): every accessor hands out a
// copy — writing every field of what it returns leaves the next accessor
// call and the devices built afterwards as they were — and New copies a
// Type it is given, so a later change to the host's value changes nothing
// in the device.
func TestRegistryUnreachable(t *testing.T) {
	t.Parallel()
	opts := func(key string) device.Options { return device.Options{StableKey: endpoint.StringKey(key), Name: key} }
	before, err := device.NewOnOffLight(opts("before"))
	if err != nil {
		t.Fatal(err)
	}
	want := len(before.Servers())
	vandalise := func(ty *device.Type) {
		ty.Revision = 99
		ty.Name = "vandal"
		ty.Conditions = append(ty.Conditions, "vandal")
		for i := range ty.Servers {
			ty.Servers[i].Features = 0xFFFF
			if len(ty.Servers[i].Elements) > 0 {
				ty.Servers[i].Elements[0].Name = "vandal"
			}
		}
		ty.Servers = ty.Servers[:0]
	}
	vandalise(device.OnOffLightType())
	got, _ := device.Lookup(uint32(device.OnOffLightID))
	vandalise(got)
	for _, ty := range device.Types() {
		vandalise(ty)
	}
	vandalise(before.Type())
	again := device.OnOffLightType()
	if again.Revision == 99 || again.Name != "OnOffLight" || len(again.Servers) == 0 || slices.Contains(again.Conditions, "vandal") {
		t.Fatalf("the registry changed through a returned Type: %+v", again)
	}
	after, err := device.NewOnOffLight(opts("after"))
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Servers()) != want || len(before.Servers()) != want || before.Type().Name != "OnOffLight" {
		t.Errorf("servers %d and %d after the writes, want %d", len(after.Servers()), len(before.Servers()), want)
	}

	// A host-made Type is copied at New.
	host := device.OnOffLightType()
	d, err := device.New(host, opts("host"))
	if err != nil {
		t.Fatal(err)
	}
	vandalise(host)
	if d.Type().Name != "OnOffLight" || len(d.Type().Servers) == 0 {
		t.Errorf("the device's type followed the host's later write: %+v", d.Type())
	}
}

// TestFromSpecBuildsParts (R2-P5-apidev-11): Spec.Parts are built and added
// as parts; a composite takes its components from them and is refused, with
// the constructor's ErrComposition, when one is missing.
func TestFromSpecBuildsParts(t *testing.T) {
	t.Parallel()
	opts := func(key string) device.Options { return device.Options{StableKey: endpoint.StringKey(key), Name: key} }
	_, err := device.FromSpec(device.Spec{Type: device.FloodlightCameraID, Options: opts("flood")})
	if !errors.Is(err, device.ErrComposition) || !strings.Contains(err.Error(), "FloodlightCamera needs its components OnOffLight and Camera") {
		t.Fatalf("FromSpec of a composite without parts = %v, want the constructor's composition refusal", err)
	}
	_, ctorErr := device.NewFloodlightCamera(opts("flood"), nil, nil)
	if ctorErr == nil || ctorErr.Error() != err.Error() {
		t.Errorf("FromSpec refuses with %v, the constructor with %v", err, ctorErr)
	}
	_, err = device.FromSpec(device.Spec{Type: device.FloodlightCameraID, Options: opts("flood"), Parts: []device.Spec{
		{Type: device.CameraID, Options: opts("flood/camera"), Declarations: device.InitialFor("Camera")},
	}})
	if !errors.Is(err, device.ErrComposition) || !strings.Contains(err.Error(), "needs its components OnOffLight") {
		t.Errorf("FromSpec without the OnOffLight component = %v", err)
	}
	flood, err := device.FromSpec(device.Spec{Type: device.FloodlightCameraID, Options: opts("flood"), Parts: []device.Spec{
		{Type: device.OnOffLightID, Options: opts("flood/light")},
		{Type: device.CameraID, Options: opts("flood/camera"), Declarations: device.InitialFor("Camera")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctor, err := device.NewFloodlightCamera(opts("flood2"), mustLight(t, opts("flood2/light")), mustCamera(t, opts("flood2/camera")))
	if err != nil {
		t.Fatal(err)
	}
	fp, cp := flood.Parts(), ctor.Parts()
	if len(fp) != 2 || len(cp) != 2 || fp[0].Type().ID != cp[0].Type().ID || fp[1].Type().ID != cp[1].Type().ID {
		t.Errorf("FromSpec parts %v, the constructor's %v", fp, cp)
	}
}

// TestRequiredInitial (R2-P5-apidev-10): a type whose kit server lists an
// attribute without a value names it and the declaration that states it;
// a type whose defaults answer everything lists nothing.
func TestRequiredInitial(t *testing.T) {
	t.Parallel()
	req, err := device.AirQualitySensorType().RequiredInitial()
	if err != nil || len(req) != 1 || req[0].Cluster != airqualitydef.ClusterID || req[0].Attribute != airqualitydef.AttrAirQuality ||
		req[0].Declare != "airqualitydef.Declare{Initial: airqualitydef.Patch{AirQuality: …}}" {
		t.Fatalf("AirQualitySensor RequiredInitial = %+v, %v", req, err)
	}
	// OperationalError has a model default matter.js builds from the
	// struct's child default: not required (decision 17).
	if req, err := device.DishwasherType().RequiredInitial(); err != nil || len(req) != 1 || req[0].Attribute != 0x0004 {
		t.Errorf("Dishwasher RequiredInitial = %+v, %v; want OperationalState alone", req, err)
	}
	if _, err := device.NewAirQualitySensor(device.Options{StableKey: endpoint.StringKey("aq-bare"), Name: "aq"}); !errors.Is(err, device.ErrConfiguration) ||
		!strings.Contains(err.Error(), "airqualitydef.Declare{Initial: airqualitydef.Patch{AirQuality: …}}") {
		t.Errorf("a bare AirQualitySensor = %v, want the refusal naming the declaration", err)
	}
	if req, err := device.DimmableLightType().RequiredInitial(); err != nil || len(req) != 0 {
		t.Errorf("DimmableLight RequiredInitial = %+v, %v; want none", req, err)
	}
	// A type whose bare defaults do not build is not measured: an error,
	// not the empty list "nothing required" is (review II, phase 5b).
	if _, bareErr := device.New(device.GenericSwitchType(), device.Options{StableKey: endpoint.StringKey("sw-bare"), Name: "sw"}); bareErr == nil {
		t.Fatal("a bare GenericSwitch builds; pick a type whose defaults do not")
	}
	if req, err := device.GenericSwitchType().RequiredInitial(); err == nil || req != nil {
		t.Errorf("GenericSwitch RequiredInitial = %+v, %v; want no rows and the refusal", req, err)
	}
	q := airqualitydef.AirQualityGood
	d, err := device.NewAirQualitySensor(device.Options{StableKey: endpoint.StringKey("aq"), Name: "aq"}, required("AirQualitySensor",
		airqualitydef.Declare{Initial: airqualitydef.Patch{AirQuality: &q}})...)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := d.Server(airqualitydef.ClusterID).MatterRead(airqualitydef.AttrAirQuality); !ok || v == nil {
		t.Errorf("the declared AirQuality is not served: %v %v", v, ok)
	}
}

func mustLight(t *testing.T, o device.Options) *device.OnOffLight {
	t.Helper()
	d, err := device.NewOnOffLight(o)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustCamera(t *testing.T, o device.Options) *device.Camera {
	t.Helper()
	d, err := device.NewCamera(o, required("Camera")...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
