// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

type ecoDir struct {
	devices   []ecodef.EcosystemDeviceStruct
	locations []ecodef.EcosystemLocationStruct
}

func (d *ecoDir) EcosystemDevices(context.Context) []ecodef.EcosystemDeviceStruct { return d.devices }

func (d *ecoDir) EcosystemLocations(context.Context) []ecodef.EcosystemLocationStruct {
	return d.locations
}

func ecoPtr[T any](v T) *T { return &v }

func ecoDevice(name string, fabric uint8) ecodef.EcosystemDeviceStruct {
	return ecodef.EcosystemDeviceStruct{
		DeviceName:        ecoPtr(name),
		OriginalEndpoint:  ecoPtr(uint16(3)),
		BridgedEndpoint:   ecoPtr(uint16(5)),
		DeviceTypes:       []ecodef.DeviceTypeStruct{{DeviceType: 0x0100, Revision: 3}},
		UniqueLocationIds: []string{"kitchen"},
		FabricIndex:       fabric,
	}
}

func ecoLocation(id, name string, fabric uint8) ecodef.EcosystemLocationStruct {
	return ecodef.EcosystemLocationStruct{
		UniqueLocationId: id,
		LocationDescriptor: ecodef.Locationdesc{
			LocationName: name,
			FloorNumber:  spec.Nullable[int16]{Null: true},
			AreaType:     spec.Nullable[uint8]{Null: true},
		},
		FabricIndex: fabric,
	}
}

// ecoRead reads attrID through MatterReadFiltered, or MatterRead for a
// nil ctx.
func ecoRead(t *testing.T, e *EcosystemInformation, ctx context.Context, attrID uint32) spec.List[spec.Encodable] { //nolint:revive // t first
	t.Helper()
	var (
		v  any
		ok bool
	)
	if ctx == nil {
		v, ok = e.MatterRead(attrID)
	} else {
		v, ok = e.MatterReadFiltered(ctx, attrID)
	}
	l, isList := v.(spec.List[spec.Encodable])
	if !ok || !isList {
		t.Fatalf("read = %#v (%v)", v, ok)
	}
	return l
}

// TestEcosystemInformationFiltersByFabric: a filtered read sees the
// reading fabric's entries, an unfiltered one every fabric's with a
// foreign entry reduced to its FabricIndex (AttributeValueEncoder.h
// :96-107), a local read everything.
func TestEcosystemInformationFiltersByFabric(t *testing.T) {
	t.Parallel()
	dir := &ecoDir{
		devices:   []ecodef.EcosystemDeviceStruct{ecoDevice("Lamp", 1), ecoDevice("Plug", 2)},
		locations: []ecodef.EcosystemLocationStruct{ecoLocation("kitchen", "Kitchen", 2), ecoLocation("hall", "Hall", 1)},
	}
	e := NewEcosystemInformation(dir)

	filtered := im.WithFabricFilter(context.Background(), true, 1)
	devs := ecoRead(t, e, filtered, ecodef.AttrDeviceDirectory)
	if len(devs) != 1 || *devs[0].(ecodef.EcosystemDeviceStruct).DeviceName != "Lamp" {
		t.Errorf("filtered DeviceDirectory = %#v", devs)
	}
	locs := ecoRead(t, e, filtered, ecodef.AttrLocationDirectory)
	if len(locs) != 1 || locs[0].(ecodef.EcosystemLocationStruct).UniqueLocationId != "hall" {
		t.Errorf("filtered LocationDirectory = %#v", locs)
	}

	unfiltered := im.WithFabricFilter(context.Background(), false, 1)
	devs = ecoRead(t, e, unfiltered, ecodef.AttrDeviceDirectory)
	if len(devs) != 2 {
		t.Fatalf("unfiltered DeviceDirectory = %#v", devs)
	}
	if _, own := devs[0].(ecodef.EcosystemDeviceStruct); !own {
		t.Errorf("own entry redacted: %#v", devs[0])
	}
	if r, redacted := devs[1].(redactedEntry); !redacted || r.fabricIndex != 2 {
		t.Errorf("foreign entry = %#v, want FabricIndex 2 alone", devs[1])
	}
	// The redacted entry carries FabricIndex and nothing else: the full
	// struct's mandatory fields are missing from it.
	var full ecodef.EcosystemLocationStruct
	if err := full.DecodeTLV(spectest.Decode(t, spectest.Encode(t, devs[1]))); err == nil {
		t.Error("a redacted entry decoded as a full EcosystemLocationStruct")
	}

	// A session without a fabric sees nothing filtered and nothing whole.
	pase := im.WithFabricFilter(context.Background(), true, 0)
	if devs := ecoRead(t, e, pase, ecodef.AttrDeviceDirectory); len(devs) != 0 {
		t.Errorf("fabric-0 filtered read = %#v", devs)
	}
	pase = im.WithFabricFilter(context.Background(), false, 0)
	for _, d := range ecoRead(t, e, pase, ecodef.AttrDeviceDirectory) {
		if _, redacted := d.(redactedEntry); !redacted {
			t.Errorf("fabric-0 unfiltered read disclosed %#v", d)
		}
	}

	local := ecoRead(t, e, context.Background(), ecodef.AttrLocationDirectory)
	if len(local) != 2 || local[0].(ecodef.EcosystemLocationStruct).UniqueLocationId != "hall" {
		t.Errorf("local LocationDirectory = %#v, want both, ordered by UniqueLocationId", local)
	}
}

// TestEcosystemInformationAppliesChipBuilderRules leaves out the entries
// connectedhomeip's builders refuse and applies Encode's name rule.
func TestEcosystemInformationAppliesChipBuilderRules(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 65)
	noOrig := ecoDevice("a", 1)
	noOrig.OriginalEndpoint = nil
	invalidOrig := ecoDevice("a", 1)
	invalidOrig.OriginalEndpoint = ecoPtr(uint16(0xFFFF))
	noTypes := ecoDevice("a", 1)
	noTypes.DeviceTypes = nil
	longName := ecoDevice(long, 1)
	badFabric := ecoDevice("a", 0)
	tooManyIDs := ecoDevice("a", 1)
	tooManyIDs.UniqueLocationIds = make([]string, 65)
	longID := ecoDevice("a", 1)
	longID.UniqueLocationIds = []string{long}
	unnamed := ecoDevice("", 1)
	unnamed.DeviceNameLastEdit = ecoPtr(uint64(7))
	named := ecoDevice("ok", 1)

	dir := &ecoDir{
		devices: []ecodef.EcosystemDeviceStruct{noOrig, invalidOrig, noTypes, longName, badFabric, tooManyIDs, longID, unnamed, named},
		locations: []ecodef.EcosystemLocationStruct{
			ecoLocation("", "Empty id", 1),
			ecoLocation(long, "Long id", 1),
			ecoLocation("a", "", 1),
			ecoLocation("b", strings.Repeat("n", 129), 1),
			ecoLocation("c", "Bad fabric", 255),
			ecoLocation("d", "First", 1),
			ecoLocation("d", "Duplicate", 1),
			ecoLocation("d", "Other fabric", 2),
		},
	}
	e := NewEcosystemInformation(dir)
	devs := ecoRead(t, e, nil, ecodef.AttrDeviceDirectory)
	if len(devs) != 2 {
		t.Fatalf("DeviceDirectory = %#v, want the two valid entries", devs)
	}
	if d := devs[0].(ecodef.EcosystemDeviceStruct); d.DeviceName != nil || d.DeviceNameLastEdit != nil {
		t.Errorf("unnamed device sent a name or its edit time: %#v", d)
	}
	if d := devs[1].(ecodef.EcosystemDeviceStruct); d.DeviceNameLastEdit == nil || *d.DeviceNameLastEdit != 0 {
		t.Errorf("named device's DeviceNameLastEdit = %v, want 0", d.DeviceNameLastEdit)
	}
	locs := ecoRead(t, e, nil, ecodef.AttrLocationDirectory)
	if len(locs) != 2 || locs[0].(ecodef.EcosystemLocationStruct).LocationDescriptor.LocationName != "First" ||
		locs[1].(ecodef.EcosystemLocationStruct).FabricIndex != 2 {
		t.Errorf("LocationDirectory = %#v", locs)
	}
	// Every valid entry encodes and decodes through the generated codec.
	if _, err := ecodef.Definition.Attribute(ecodef.AttrDeviceDirectory).Decode(spectest.Decode(t, spectest.Encode(t, devs))); err != nil {
		t.Errorf("DeviceDirectory does not decode: %v", err)
	}
	if _, err := ecodef.Definition.Attribute(ecodef.AttrLocationDirectory).Decode(spectest.Decode(t, spectest.Encode(t, locs))); err != nil {
		t.Errorf("LocationDirectory does not decode: %v", err)
	}
}

func TestEcosystemInformationDirectoryChanged(t *testing.T) {
	t.Parallel()
	e := NewEcosystemInformation(&ecoDir{})
	var got []uint32
	e.OnAttributeChange(func(change contract.Change) { ids := change.Attributes; got = append(got, ids...) })
	before := e.MatterDataVersion()
	e.DirectoryChanged()
	if e.MatterDataVersion() == before {
		t.Error("data version did not move")
	}
	if len(got) != 2 {
		t.Errorf("changed attributes = %v", got)
	}
	if err := e.MatterWrite(context.Background(), ecodef.AttrDeviceDirectory, nil); err == nil {
		t.Error("write accepted")
	}
}
