// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package parity_test

import (
	"encoding/json"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
)

// The device-type layer of the snapshot: what matter.js's device type
// validation (packages/model/src/logic/device-types/DeviceTypeConformance.ts)
// reads of each device type, emitted by script/extract-from-matter-js.ts
// (deviceTypeEffective, requirementOut, referentOut). These tests hold the
// extractor's output to the matter.js element files at the pin
// (packages/model/src/standard/elements/*.element.ts).

type layerConformance struct {
	Text string          `json:"text"`
	AST  json.RawMessage `json:"ast"`
}

type layerRequirement struct {
	Element      string                 `json:"element"`
	Name         string                 `json:"name"`
	ID           *uint32                `json:"id"`
	Type         string                 `json:"type"`
	Instance     int                    `json:"instance"`
	Location     string                 `json:"location"`
	Conformance  *layerConformance      `json:"conformance"`
	Constraint   *struct{ Text string } `json:"constraint"`
	Quality      map[string]bool        `json:"quality"`
	Referent     *layerReferent         `json:"referent"`
	Requirements []layerRequirement     `json:"requirements"`
}

type layerReferent struct {
	ID          *uint32 `json:"id"`
	Name        string  `json:"name"`
	Bit         *int    `json:"bit"`
	Declarer    string  `json:"declarer"`
	Provisional bool    `json:"provisional"`
}

type layerEffective struct {
	Composition  string             `json:"composition"`
	Base         string             `json:"base"`
	Conditions   []string           `json:"conditions"`
	Requirements []layerRequirement `json:"requirements"`
}

type layerDeviceType struct {
	ID             uint32          `json:"id"`
	Name           string          `json:"name"`
	Classification string          `json:"classification"`
	Revision       int             `json:"revision"`
	Effective      *layerEffective `json:"effective"`
}

type layerFile struct {
	DeviceTypes     []layerDeviceType `json:"deviceTypes"`
	BaseDeviceTypes []layerDeviceType `json:"baseDeviceTypes"`
	Clusters        []struct {
		ID             uint32 `json:"id"`
		Name           string `json:"name"`
		Classification string `json:"classification"`
		Bindable       *bool  `json:"bindable"`
	} `json:"clusters"`
}

func loadLayer(t *testing.T) layerFile {
	t.Helper()
	var f layerFile
	if err := json.Unmarshal(parity.SchemaJSON(), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func deviceTypeNamed(t *testing.T, f layerFile, name string) layerDeviceType {
	t.Helper()
	for _, dt := range f.DeviceTypes {
		if dt.Name == name {
			return dt
		}
	}
	t.Fatalf("device type %s not in the snapshot", name)
	return layerDeviceType{}
}

func requirementNamed(t *testing.T, reqs []layerRequirement, element, name string) layerRequirement {
	t.Helper()
	for _, r := range reqs {
		if r.Element == element && r.Name == name {
			return r
		}
	}
	t.Fatalf("no %s requirement %s", element, name)
	return layerRequirement{}
}

// Every device type carries its effective layer, and every requirement in
// it resolves: the extract at the pin names what RequirementResolver
// resolves each one to.
func TestDeviceTypeLayerResolvesEveryRequirement(t *testing.T) {
	t.Parallel()
	f := loadLayer(t)
	if len(f.BaseDeviceTypes) != 1 || f.BaseDeviceTypes[0].Name != "Base" {
		t.Fatalf("baseDeviceTypes = %+v, want the Base device type", f.BaseDeviceTypes)
	}
	conditions := 0
	var walk func(dt string, rs []layerRequirement)
	walk = func(dt string, rs []layerRequirement) {
		for _, r := range rs {
			if r.Referent == nil {
				t.Errorf("%s: %s requirement %s resolves to nothing", dt, r.Element, r.Name)
			}
			if r.Element == "condition" && r.Location == "" {
				t.Errorf("%s: condition requirement %s states no location", dt, r.Name)
			}
			walk(dt, r.Requirements)
		}
	}
	for _, dt := range append(append([]layerDeviceType(nil), f.DeviceTypes...), f.BaseDeviceTypes...) {
		if dt.Effective == nil {
			t.Errorf("%s: no effective layer", dt.Name)
			continue
		}
		conditions += len(dt.Effective.Conditions)
		walk(dt.Name, dt.Effective.Requirements)
	}
	// The device types of matter.js's model declare 46 conditions; CHIP's
	// device_types/*.xml state the same number.
	if conditions != 46 {
		t.Errorf("conditions = %d, want 46", conditions)
	}
}

// OnOffLight (on-off-light.element.ts): OnOff requires LT, Identify the
// TriggerEffect command, ScenesManagement CopyScene; LevelControl's
// attributes carry constraints; the GroupcastListenerCond condition
// requirement asserts RootNode's condition on the root.
func TestDeviceTypeLayerOnOffLight(t *testing.T) {
	t.Parallel()
	dt := deviceTypeNamed(t, loadLayer(t), "OnOffLight")
	e := dt.Effective
	if e.Composition != "tree" || e.Base != "" {
		t.Errorf("composition %q base %q, want tree and none", e.Composition, e.Base)
	}
	onOff := requirementNamed(t, e.Requirements, "serverCluster", "OnOff")
	lt := requirementNamed(t, onOff.Requirements, "feature", "LT")
	if lt.Conformance == nil || lt.Conformance.Text != "M" || lt.Referent.Bit == nil || *lt.Referent.Bit != 0 {
		t.Errorf("OnOff.LT = %+v, want M on feature bit 0", lt)
	}
	identify := requirementNamed(t, e.Requirements, "serverCluster", "Identify")
	trigger := requirementNamed(t, identify.Requirements, "command", "TriggerEffect")
	if trigger.Referent.ID == nil || *trigger.Referent.ID != 0x40 {
		t.Errorf("Identify.TriggerEffect referent = %+v, want command 0x40", trigger.Referent)
	}
	level := requirementNamed(t, e.Requirements, "serverCluster", "LevelControl")
	current := requirementNamed(t, level.Requirements, "attribute", "CurrentLevel")
	if current.Constraint == nil || current.Constraint.Text != "1 to 254" || current.Conformance != nil {
		t.Errorf("LevelControl.CurrentLevel = %+v, want constraint 1 to 254 and no conformance", current)
	}
	cond := requirementNamed(t, e.Requirements, "condition", "GroupcastListenerCond")
	if cond.Location != "Root" || cond.Type != "RootNode.GroupcastListenerCond" ||
		cond.Referent.Declarer != "RootNode" || cond.Referent.Name != "GroupcastListenerCond" {
		t.Errorf("GroupcastListenerCond = %+v", cond)
	}
}

// RootNode (root-node.element.ts): full-family composition, its twelve
// conditions, singleton server clusters and condition-gated features.
func TestDeviceTypeLayerRootNode(t *testing.T) {
	t.Parallel()
	dt := deviceTypeNamed(t, loadLayer(t), "RootNode")
	e := dt.Effective
	if e.Composition != "full-family" {
		t.Errorf("composition = %q, want full-family", e.Composition)
	}
	if len(e.Conditions) != 12 || e.Conditions[0] != "CustomNetworkConfig" {
		t.Errorf("conditions = %v", e.Conditions)
	}
	acl := requirementNamed(t, e.Requirements, "serverCluster", "AccessControl")
	if !acl.Quality["singleton"] {
		t.Errorf("AccessControl quality = %v, want singleton", acl.Quality)
	}
	aux := requirementNamed(t, acl.Requirements, "feature", "AUX")
	if aux.Conformance == nil || aux.Conformance.Text != "GroupcastListenerCond" {
		t.Errorf("AccessControl.AUX = %+v", aux)
	}
	nc := requirementNamed(t, e.Requirements, "serverCluster", "NetworkCommissioning")
	if nc.Quality["singleton"] || nc.Conformance.Text != "!CustomNetworkConfig" {
		t.Errorf("NetworkCommissioning = %+v", nc)
	}
	ps := requirementNamed(t, e.Requirements, "deviceType", "PowerSource")
	if ps.Referent.ID == nil || *ps.Referent.ID != 0x11 {
		t.Errorf("PowerSource component referent = %+v", ps.Referent)
	}
}

// DimmableLight derives from OnOffLight (dimmable-light.element.ts type),
// and BatteryStorage numbers its two PowerSource components.
func TestDeviceTypeLayerBaseAndInstances(t *testing.T) {
	t.Parallel()
	f := loadLayer(t)
	if b := deviceTypeNamed(t, f, "DimmableLight").Effective.Base; b != "OnOffLight" {
		t.Errorf("DimmableLight base = %q, want OnOffLight", b)
	}
	instances := map[int]bool{}
	for _, r := range deviceTypeNamed(t, f, "BatteryStorage").Effective.Requirements {
		if r.Element == "deviceType" && r.Name == "PowerSource" {
			instances[r.Instance] = true
			if r.Constraint == nil || r.Constraint.Text != "min 2" {
				t.Errorf("PowerSource instance %d constraint = %+v, want min 2", r.Instance, r.Constraint)
			}
		}
	}
	if !instances[1] || !instances[2] {
		t.Errorf("BatteryStorage PowerSource instances = %v, want 1 and 2", instances)
	}
	base := f.BaseDeviceTypes[0].Effective
	desc := requirementNamed(t, base.Requirements, "serverCluster", "Descriptor")
	if tl := requirementNamed(t, desc.Requirements, "feature", "TAGLIST"); tl.Conformance.Text != "Duplicate" {
		t.Errorf("Base Descriptor.TAGLIST = %+v", tl)
	}
	if len(base.Conditions) != 23 {
		t.Errorf("Base conditions = %d, want 23", len(base.Conditions))
	}
}

// Cluster classification and bindability (ClusterModel.effectiveClassification
// and effectiveBindable): the inputs of Base's Server and Client conditions.
func TestDeviceTypeLayerClusterClassification(t *testing.T) {
	t.Parallel()
	f := loadLayer(t)
	want := map[uint32]string{0x0006: "application", 0x0028: "node", 0x001D: "endpoint"}
	unbindable := map[uint32]bool{}
	for _, c := range f.Clusters {
		if c.Classification == "" {
			t.Errorf("%s: no classification", c.Name)
		}
		if w, ok := want[c.ID]; ok && c.Classification != w {
			t.Errorf("%s classification = %q, want %q", c.Name, c.Classification, w)
		}
		if c.Bindable != nil {
			unbindable[c.ID] = !*c.Bindable
		}
	}
	if !unbindable[0x0029] {
		t.Error("OtaSoftwareUpdateProvider is not marked bindable: false")
	}
}
