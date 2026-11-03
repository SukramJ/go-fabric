// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

// Further device types and semantic tags on a device, after matter.js's
// bridge test instance at 862ad75f
// (support/chip-testing/src/BridgeTestInstance.ts): its composed device
// declares deviceTypeList [BridgedNode rev 3, PowerSource 0x0011 rev 1]
// (:323-335), its two sensors CommonNumberTag One and Two through
// DescriptorServer.with("TagList") (:306-317). DescriptorServer.addDeviceTypes
// skips a listed device type (packages/node/src/behaviors/descriptor/
// DescriptorServer.ts:91-110); the TagList feature is on only with tags.

import (
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	descriptordef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
)

// descriptorOf is the Descriptor server of d's endpoint.
func descriptorOf(t *testing.T, topo *endpoint.Topology, d *Device) contract.ClusterServer {
	t.Helper()
	for _, ep := range topo.Endpoints {
		if ep.Source != contract.EndpointSource(d) {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			if srv.MatterClusterID() == descriptordef.ClusterID {
				return srv
			}
		}
	}
	t.Fatal("no Descriptor for the device")
	return nil
}

func deviceTypeList(t *testing.T, desc contract.ClusterServer) []mattercore.DeviceTypeStruct {
	t.Helper()
	v, _ := desc.MatterRead(descriptordef.AttrDeviceTypeList)
	l, ok := v.([]mattercore.DeviceTypeStruct)
	if !ok {
		t.Fatalf("DeviceTypeList is %T", v)
	}
	return l
}

func revisionOf(t *testing.T, id schema.DeviceTypeID) uint16 {
	t.Helper()
	r, ok := schema.DeviceTypeRevision(id)
	if !ok {
		t.Fatalf("device type 0x%04X not in the schema", uint32(id))
	}
	return r
}

func numberTag(n uint8, label string) SemanticTag {
	return SemanticTag{MfgCode: spec.Nullable[uint16]{Null: true}, NamespaceId: descriptordef.NamespaceCommonNumber, Tag: n, Label: &spec.Nullable[string]{Value: label}}
}

func TestDescriptorDeviceTypesAndTagsParity(t *testing.T) {
	t.Parallel()

	// :323-335 — the composed device lists Bridged Node, then Power
	// Source; a type listed twice, and Bridged Node itself, are listed once.
	t.Run("further device types after the own, deduplicated, with the schema's revision", func(t *testing.T) {
		t.Parallel()
		opts := testOptions("composed")
		opts.DeviceTypes = []schema.DeviceTypeID{PowerSourceID, PowerSourceID, BridgedNodeID}
		composed, err := NewBridgedNode(opts)
		if err != nil {
			t.Fatal(err)
		}
		got := deviceTypeList(t, descriptorOf(t, assemble(t, composed.Device), composed.Device))
		want := []mattercore.DeviceTypeStruct{
			{DeviceType: uint32(BridgedNodeID), Revision: revisionOf(t, BridgedNodeID)},
			{DeviceType: uint32(PowerSourceID), Revision: revisionOf(t, PowerSourceID)},
		}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("DeviceTypeList = %+v, want %+v", got, want)
		}
		if want[0].Revision != 3 || want[1].Revision != 1 {
			t.Errorf("revisions %d / %d, want BridgedNode 3 and PowerSource 1 (:330-335)", want[0].Revision, want[1].Revision)
		}
	})

	// A further type on a device whose own type is not Bridged Node goes
	// between the two, and its mandatory server cluster is required of the
	// endpoint: the validator reads the composition from the same list.
	t.Run("between the own type and Bridged Node; the listed type's clusters are required", func(t *testing.T) {
		t.Parallel()
		opts := testOptions("sensor")
		opts.DeviceTypes = []schema.DeviceTypeID{PowerSourceID}
		sensor, err := NewTemperatureSensor(opts)
		if err != nil {
			t.Fatal(err)
		}
		topo := assemble(t, sensor.Device)
		got := deviceTypeList(t, descriptorOf(t, topo, sensor.Device))
		if len(got) != 3 || got[0].DeviceType != uint32(TemperatureSensorID) || got[1].DeviceType != uint32(PowerSourceID) || got[2].DeviceType != uint32(BridgedNodeID) {
			t.Fatalf("DeviceTypeList = %+v, want TemperatureSensor, PowerSource, BridgedNode", got)
		}
		found := false
		for _, v := range violationsOf(topo) {
			if v.DeviceTypeID == uint32(PowerSourceID) && v.Requirement == "PowerSource" {
				found = true
			}
		}
		if !found {
			t.Errorf("no violation for Power Source without its PowerSource cluster: %v", violationsOf(topo))
		}
	})

	t.Run("an unknown device type is refused", func(t *testing.T) {
		t.Parallel()
		opts := testOptions("unknown")
		opts.DeviceTypes = []schema.DeviceTypeID{0xFFF1}
		if _, err := NewTemperatureSensor(opts); err == nil {
			t.Fatal("device type 0xFFF1 accepted")
		}
	})

	// :306-317 — a sensor with a tag serves TagList and the feature; one
	// without serves neither (the default, cluster/core Descriptor).
	t.Run("TagList and its feature only with tags", func(t *testing.T) {
		t.Parallel()
		tagged := testOptions("tagged")
		tagged.Tags = []SemanticTag{numberTag(1, "One")}
		one, err := NewTemperatureSensor(tagged)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := NewTemperatureSensor(testOptions("plain"))
		if err != nil {
			t.Fatal(err)
		}
		topo := assemble(t, one.Device, plain.Device)
		desc := descriptorOf(t, topo, one.Device)
		if fm, _ := desc.MatterRead(0xFFFC); fm.(uint32)&uint32(descriptordef.FeatureTagList) == 0 {
			t.Errorf("tagged FeatureMap %v without TAGLIST", fm)
		}
		v, ok := desc.MatterRead(descriptordef.AttrTagList)
		tags, _ := v.([]mattercore.SemanticTag)
		if !ok || len(tags) != 1 || tags[0].Tag != 1 || tags[0].Label == nil || tags[0].Label.Value != "One" {
			t.Errorf("TagList = %+v, want CommonNumberTag One", v)
		}
		bare := descriptorOf(t, topo, plain.Device)
		if fm, _ := bare.MatterRead(0xFFFC); fm.(uint32)&uint32(descriptordef.FeatureTagList) != 0 {
			t.Errorf("untagged FeatureMap %v with TAGLIST", fm)
		}
		if _, ok := bare.MatterRead(descriptordef.AttrTagList); ok {
			t.Error("untagged Descriptor serves TagList")
		}
	})
}
