// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"slices"
	"strings"
	"testing"
)

func diffStrings(res *Result) []string {
	out := make([]string, len(res.Differences))
	for i, d := range res.Differences {
		out[i] = d.String()
	}
	return out
}

// TestCompareSeededDifferences mutates one aspect of the real snapshot per
// case and requires the comparison to report exactly that. It needs CHIP's
// data model and skips without a checkout, as the cross-check does.
func TestCompareSeededDifferences(t *testing.T) {
	t.Parallel()
	x := chipModel(t)
	baseline := Compare(mustSnapshot(t), x.Model)

	cases := []struct {
		name   string
		mutate func(m *Model)
		want   string
	}{
		{"conformance", func(m *Model) {
			attr(m, "OnOff", "OnTime").Conformance = &Conf{Op: opOptional}
		}, `OnOff.OnTime conformance: chip "lt", ours "o"`},
		{"access", func(m *Model) {
			attr(m, "OnOff", "StartUpOnOff").Access.WritePriv = "O"
		}, `OnOff.StartUpOnOff access write privilege: chip "M", ours "O"`},
		{"quality", func(m *Model) {
			attr(m, "OnOff", "StartUpOnOff").Quality = []string{"nonvolatile"}
		}, `OnOff.StartUpOnOff quality: chip "nonvolatile nullable", ours "nonvolatile"`},
		{"type", func(m *Model) {
			a := attr(m, "OnOff", "OnTime")
			a.Type, a.Primitive = "uint32", "uint32"
		}, `OnOff.OnTime type: chip "uint16", ours "uint32"`},
		{"revision", func(m *Model) {
			cluster(m, "OnOff").Revision = 5
		}, `OnOff revision: chip "6", ours "5"`},
		{"feature bit", func(m *Model) {
			cluster(m, "OnOff").Features[0].Constraint = "7"
		}, `OnOff.LT constraint: chip "0", ours "7"`},
		{"missing attribute", func(m *Model) {
			c := cluster(m, "OnOff")
			c.Attributes = slices.DeleteFunc(c.Attributes, func(e *Element) bool { return e.Name == "OnTime" })
		}, `OnOff.OnTime attribute: chip "present", ours "absent"`},
		{"timed", func(m *Model) {
			cmd := elementNamed(cluster(m, "OnOff").Commands, "Off")
			cmd.Access.Timed = true
		}, `OnOff.Off access timed: chip "not timed", ours "T"`},
		{"requirement conformance", func(m *Model) {
			for _, dt := range m.DeviceTypes {
				if dt.Name == "OnOffLight" {
					for _, r := range dt.Requirements {
						if r.Name == "OnOff" && r.Side == "server" {
							r.Conformance = &Conf{Op: opOptional}
						}
					}
				}
			}
		}, `OnOffLight.OnOff (server) conformance: chip "m", ours "o"`},
		// A derived cluster: CHIP states RvcRunMode's SupportedModes only in
		// Mode Base, so the difference proves the base chain is resolved.
		{"derived cluster", func(m *Model) {
			attr(m, "RvcRunMode", "SupportedModes").Constraint = "1 to 255"
		}, `RvcRunMode.SupportedModes constraint: chip "2to255", ours "1to255"`},
		{"derived cluster quality", func(m *Model) {
			attr(m, "RvcRunMode", "SupportedModes").Quality = nil
		}, `RvcRunMode.SupportedModes quality: chip "fixed", ours "none"`},
		{"event priority", func(m *Model) {
			elementNamed(cluster(m, "BasicInformation").Events, "StartUp").Priority = "info"
		}, `BasicInformation.StartUp priority: chip "critical", ours "info"`},
		{"event field", func(m *Model) {
			ev := elementNamed(cluster(m, "BasicInformation").Events, "StartUp")
			ev.Fields[0].Type, ev.Fields[0].Primitive = "uint16", "uint16"
		}, `BasicInformation.StartUp.SoftwareVersion type: chip "uint32", ours "uint16"`},
		{"event access", func(m *Model) {
			elementNamed(cluster(m, "AccessControl").Events, "AccessControlEntryChanged").Access.ReadPriv = "M"
		}, `AccessControl.AccessControlEntryChanged access read privilege: chip "A", ours "M"`},
		{"command fabric scope", func(m *Model) {
			elementNamed(cluster(m, "GroupKeyManagement").Commands, "KeySetWrite").Access.Fabric = ""
		}, `GroupKeyManagement.KeySetWrite access fabric: chip "F", ours "absent"`},
		{"command field", func(m *Model) {
			cmd := elementNamed(cluster(m, "OnOff").Commands, "OnWithTimedOff")
			elementNamed(cmd.Fields, "OnTime").Constraint = "max 65535"
		}, `OnOff.OnWithTimedOff.OnTime constraint: chip "max65534", ours "max65535"`},
		{"enum item", func(m *Model) {
			dt := elementNamed(cluster(m, "OnOff").Datatypes, "StartUpOnOffEnum")
			elementNamed(dt.Fields, "Toggle").ID = u32(3)
		}, `OnOff.StartUpOnOffEnum.Toggle id: chip "0x2", ours "0x3"`},
		{"bitmap bit", func(m *Model) {
			dt := elementNamed(cluster(m, "OnOff").Datatypes, "OnOffControlBitmap")
			elementNamed(dt.Fields, "AcceptOnlyWhenOn").Constraint = "1"
		}, `OnOff.OnOffControlBitmap.AcceptOnlyWhenOn constraint: chip "0", ours "1"`},
		{"struct field fabric sensitivity", func(m *Model) {
			dt := elementNamed(cluster(m, "AccessControl").Datatypes, "AccessControlEntryStruct")
			elementNamed(dt.Fields, "Privilege").Access = &Access{}
		}, `AccessControl.AccessControlEntryStruct.Privilege access fabric: chip "S", ours "absent"`},
		// Conformance is an expression: the operands of an OR are a set, so
		// reordering them is no difference, but changing the operator is.
		{"conformance operand order", func(m *Model) {
			f := elementNamed(cluster(m, "OnOff").Features, "OFFONLY")
			or := f.Conformance.Args[0].Args[0]
			or.Args[0], or.Args[1] = or.Args[1], or.Args[0]
		}, ""},
		{"conformance operator", func(m *Model) {
			f := elementNamed(cluster(m, "OnOff").Features, "OFFONLY")
			f.Conformance.Args[0].Args[0].Op = opAnd
		}, `OnOff.OFFONLY conformance: chip "[!(df|lt)]", ours "[!(df&lt)]"`},
		{"device-type classification", func(m *Model) {
			for _, dt := range m.DeviceTypes {
				if dt.Name == "OnOffLight" {
					dt.Classification = "utility"
				}
			}
		}, `OnOffLight classification: chip "simple", ours "utility"`},
		{"device-type revision", func(m *Model) {
			for _, dt := range m.DeviceTypes {
				if dt.Name == "OnOffLight" {
					dt.Revision = 1
				}
			}
		}, `OnOffLight revision: chip "4", ours "1"`},
		// Element requirements, both directions.
		{"element requirement conformance", func(m *Model) {
			elementRequirement(m, "OnOffLight", "OnOff", "LT").Conformance = &Conf{Op: opOptional}
		}, `OnOffLight.OnOff (server).LT conformance: chip "m", ours "o"`},
		{"element requirement constraint", func(m *Model) {
			elementRequirement(m, "OnOffLight", "LevelControl", "CurrentLevel").Constraint = "1 to 253"
		}, `OnOffLight.LevelControl (server).CurrentLevel constraint: chip "1to254", ours "1to253"`},
		{"element requirement id", func(m *Model) {
			elementRequirement(m, "OnOffLight", "Identify", "TriggerEffect").ID = u32(0x41)
		}, `OnOffLight.Identify (server).TriggerEffect id: chip "0x40", ours "0x41"`},
		{"element requirement missing", func(m *Model) {
			r := clusterRequirement(m, "OnOffLight", "OnOff")
			r.Elements = nil
		}, `OnOffLight.OnOff (server).LT feature requirement: chip "present", ours "absent"`},
		{"element requirement extra", func(m *Model) {
			r := clusterRequirement(m, "OnOffLight", "OnOff")
			r.Elements = append(r.Elements, &ElementRequirement{Element: "feature", Name: "DF", Conformance: &Conf{Op: opDisallowed}})
		}, `OnOffLight.OnOff (server).DF feature requirement: chip "absent", ours "present"`},
		{"cluster requirement quality", func(m *Model) {
			clusterRequirement(m, "RootNode", "AccessControl").Quality = nil
		}, `RootNode.AccessControl (server) quality: chip "singleton", ours "none"`},
		{"base device type requirement", func(m *Model) {
			clusterRequirement(m, "Base", "Binding").Conformance = &Conf{Op: opMandatory}
		}, `Base.Binding (server) conformance: chip "client&simple", ours "m"`},
		// Conditions and condition requirements, both directions.
		{"condition missing", func(m *Model) {
			dt := deviceTypeNamed(m, "RootNode")
			dt.Conditions = slices.DeleteFunc(slices.Clone(dt.Conditions), func(c string) bool { return c == "CustomNetworkConfig" })
		}, `RootNode.CustomNetworkConfig condition: chip "present", ours "absent"`},
		{"condition extra", func(m *Model) {
			dt := deviceTypeNamed(m, "Aggregator")
			dt.Conditions = append(slices.Clone(dt.Conditions), "Federated")
		}, `Aggregator.Federated condition: chip "absent", ours "present"`},
		{"condition requirement conformance", func(m *Model) {
			conditionRequirement(m, "OnOffLight", "GroupcastListenerCond").Conformance = &Conf{Op: opOptional}
		}, `OnOffLight.RootNode.GroupcastListenerCond (condition) conformance: chip "m", ours "o"`},
		{"condition requirement missing", func(m *Model) {
			dt := deviceTypeNamed(m, "OnOffLight")
			dt.ConditionRequirements = nil
		}, `OnOffLight.Root Node.GroupcastListenerCond (condition) conditionRequirement: chip "present", ours "absent"`},
		{"condition requirement extra", func(m *Model) {
			dt := deviceTypeNamed(m, "Aggregator")
			dt.ConditionRequirements = append(dt.ConditionRequirements, &ConditionRequirement{DeviceType: "RootNode", Name: "TimeSyncCond", Conformance: &Conf{Op: opMandatory}})
		}, `Aggregator.RootNode.TimeSyncCond (condition) conditionRequirement: chip "absent", ours "present"`},
		{"cluster classification", func(m *Model) {
			cluster(m, "OnOff").Classification = "endpoint"
		}, `OnOff classification: chip "application", ours "endpoint"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ours := mustSnapshot(t)
			tc.mutate(ours)
			got := diffStrings(Compare(ours, x.Model))
			want := diffStrings(baseline)
			if tc.want != "" {
				want = append(want, tc.want)
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("seeded %s: got differences beyond the baseline\n%s\nwant the one\n%s",
					tc.name, strings.Join(added(got, diffStrings(baseline)), "\n"), tc.want)
			}
		})
	}
}

func added(got, baseline []string) []string {
	var out []string
	for _, g := range got {
		if !slices.Contains(baseline, g) {
			out = append(out, g)
		}
	}
	return out
}

func deviceTypeNamed(m *Model, name string) *DeviceType {
	for _, dt := range m.DeviceTypes {
		if dt.Name == name {
			return dt
		}
	}
	panic("no device type " + name)
}

func clusterRequirement(m *Model, deviceType, name string) *Requirement {
	for _, r := range deviceTypeNamed(m, deviceType).Requirements {
		if r.Name == name && r.Side == "server" {
			return r
		}
	}
	panic("no server requirement " + name)
}

func elementRequirement(m *Model, deviceType, clusterName, name string) *ElementRequirement {
	for _, e := range clusterRequirement(m, deviceType, clusterName).Elements {
		if e.Name == name {
			return e
		}
	}
	panic("no element requirement " + name)
}

func conditionRequirement(m *Model, deviceType, name string) *ConditionRequirement {
	for _, r := range deviceTypeNamed(m, deviceType).ConditionRequirements {
		if r.Name == name {
			return r
		}
	}
	panic("no condition requirement " + name)
}

// TestCompareBaseDeviceTypePairing: the Base device type pairs by having no
// id on both sides, and one present on a single side is reported.
func TestCompareBaseDeviceTypePairing(t *testing.T) {
	t.Parallel()
	base := func(name string) *DeviceType {
		return &DeviceType{Name: name, Revision: 3, Requirements: []*Requirement{{ID: 0x1D, Name: "Descriptor", Side: "server", Conformance: &Conf{Op: opMandatory}}}}
	}
	if got := diffStrings(Compare(&Model{DeviceTypes: []*DeviceType{base("Base")}}, &Model{DeviceTypes: []*DeviceType{base("Base Device Type")}})); len(got) != 0 {
		t.Errorf("paired Base differs: %v", got)
	}
	if got := diffStrings(Compare(&Model{DeviceTypes: []*DeviceType{base("Base")}}, &Model{})); !slices.Equal(got, []string{`Base deviceType: chip "absent", ours "present"`}) {
		t.Errorf("Base only ours: %v", got)
	}
	if got := diffStrings(Compare(&Model{}, &Model{DeviceTypes: []*DeviceType{base("Base Device Type")}})); !slices.Equal(got, []string{`Base Device Type deviceType: chip "present", ours "absent"`}) {
		t.Errorf("Base only CHIP's: %v", got)
	}
}

func mustSnapshot(t *testing.T) *Model {
	t.Helper()
	m, _ := loadSnapshot(t)
	return m
}

func cluster(m *Model, name string) *Cluster {
	for _, c := range m.Clusters {
		if c.Name == name {
			return c
		}
	}
	panic("no cluster " + name)
}

func attr(m *Model, clusterName, name string) *Element {
	e := elementNamed(cluster(m, clusterName).Attributes, name)
	if e == nil {
		panic("no attribute " + name)
	}
	return e
}

// TestCompareRules drives every normalization rule and every kind of
// reported difference through a pair of small models.
func TestCompareRules(t *testing.T) {
	t.Parallel()
	chip, err := LoadFiles(fixtureFiles(t, map[string]string{
		"device_types/Light.xml": `<deviceType id="0xFFF0" name="Fancy Light" revision="2">
  <classification superset="Plain Light" class="simple"/>
  <conditions><condition name="One"/><condition name="Two"/></conditions>
  <conditionRequirements>
    <deviceType id="0x0016" name="Root Node">
      <conditionRequirement name="Wi-Fi"><mandatoryConform/></conditionRequirement>
      <conditionRequirement name="Thread"><mandatoryConform/></conditionRequirement>
    </deviceType>
    <deviceType name="Base Device Type">
      <conditionRequirement name="Ethernet"><mandatoryConform/></conditionRequirement>
    </deviceType>
  </conditionRequirements>
  <clusters>
    <cluster id="0xFFF1" name="Widget" side="server">
      <quality singleton="true"/>
      <mandatoryConform/>
      <features><feature code="DER"><mandatoryConform/></feature><feature code="GONE"><optionalConform/></feature></features>
      <attributes><attribute code="0x0000" name="Mode"><constraint><between><from value="1"/><to value="4"/></between></constraint></attribute></attributes>
      <commands><command id="0x0040" name="Reset"><mandatoryConform/></command></commands>
    </cluster>
    <cluster id="0x0006" name="On/Off" side="client"><optionalConform/></cluster>
    <cluster id="0x0007" name="Missing" side="server"><optionalConform/></cluster>
  </clusters>
</deviceType>`,
		"device_types/Plain.xml": `<deviceType id="0xFFEF" name="Plain Light"><classification class="simple" superset="Fancy Light"/>
  <clusters><cluster id="0x0003" name="Identify" side="server"><mandatoryConform/></cluster></clusters></deviceType>`,
		"device_types/Gone.xml": `<deviceType id="0xFFEE" name="Gone"><classification class="simple" superset="Nobody"/></deviceType>`,
		"globals/Structs.xml": `<structs>
  <struct name="SemanticTagStruct"><field id="0" name="Label" type="string"><optionalConform/></field></struct>
  <struct name="SharedStruct"><field id="0" name="A" type="uint8"/></struct>
  <struct name="ScopedStruct"><field id="0" name="A" type="uint8"/></struct>
  <struct name="LostStruct"><field id="0" name="A" type="uint8"/></struct>
</structs>`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	id := u32
	ours := &Model{
		Clusters: []*Cluster{
			{
				ID: id(0xFFF1), Name: "Widget", Revision: 2, Base: "WidgetBase", Classification: "endpoint",
				Features: []*Element{
					{Name: "BASEF", Constraint: "0"},
					{Name: "DER", Constraint: "1", Conformance: &Conf{Op: opMandatory}},
					{Name: "EXTRA", Constraint: "2"},
				},
				Attributes: []*Element{
					{ID: id(0), Name: "Mode", Type: "uint8", Conformance: &Conf{Op: opDisallowed}, Access: &Access{Rw: "R[W]", ReadPriv: "V", WritePriv: "M"}},
					{ID: id(0xFFFD), Name: "ClusterRevision"},
					{ID: id(0xFE), Name: "FabricIndex"},
					{ID: id(0x99), Name: "Bonus"},
				},
				Commands: []*Element{
					{
						ID: id(0), Name: "Reset", Direction: "request", Response: "status", Conformance: &Conf{Op: opOptional},
						Access: &Access{ReadPriv: "O", WritePriv: "O", Timed: true},
						// CHIP states no field for Reset: not compared, counted.
						Fields: []*Element{{ID: id(0), Name: "Hard", Type: "bool"}},
					},
				},
				Datatypes: []*Element{
					{Name: "StatusEnum", Kind: "enum", Type: "enum8", Metatype: "enum", Primitive: "uint8", Fields: []*Element{
						{ID: id(0), Name: "Ok"},
						{ID: id(1), Name: "Busy"},
						{ID: id(2), Name: "Extra"},
						{Name: "FabricIndex"},
					}},
					{Name: "OptStruct", Kind: "struct", Metatype: "object", Primitive: "struct", Fields: []*Element{
						{ID: id(0), Name: "Label", Type: "string", Conformance: &Conf{Op: opMandatory}},
					}},
					{Name: "semtag", Kind: "struct"},
					{Name: "Mine", Kind: "struct"},
				},
			},
			{ID: id(0xFFF2), Name: "AlphaThing", Revision: 1},
			{ID: id(0xFFF3), Name: "BetaThing", Revision: 1, Datatypes: []*Element{{Name: "ScopedStruct", Fields: []*Element{{ID: id(0), Name: "A", Type: "uint8"}}}}},
			{ID: id(0xFFF5), Name: "OnlyOurs", Revision: 1},
		},
		DeviceTypes: []*DeviceType{
			{Name: "Base", Revision: 1, Classification: "base", Requirements: []*Requirement{
				{ID: 0x1D, Name: "Descriptor", Side: "server", Conformance: &Conf{Op: opMandatory}},
			}},
			{ID: id(0xFFF0), Name: "FancyLight", Revision: 2, Classification: "simple", Conditions: []string{"One", "Three"}, ConditionRequirements: []*ConditionRequirement{
				{DeviceType: "RootNode", Name: "WiFi", Conformance: &Conf{Op: opOptional}},
				{DeviceType: "Base", Name: "Ethernet", Conformance: &Conf{Op: opMandatory}},
				{DeviceType: "RootNode", Name: "GroupcastListenerCond", Conformance: &Conf{Op: opMandatory}},
			}, Requirements: []*Requirement{
				{ID: 0xFFF1, Name: "Widget", Side: "server", Conformance: &Conf{Op: opMandatory}, Elements: []*ElementRequirement{
					{Element: "feature", Name: "DER", Conformance: &Conf{Op: opOptional}},
					{Element: "attribute", ID: id(0), Name: "Mode", Constraint: "1 to 5"},
					{Element: "command", ID: id(0x41), Name: "Reset", Conformance: &Conf{Op: opMandatory}},
					{Element: "attribute", ID: id(1), Name: "Extra", Conformance: &Conf{Op: opDisallowed}},
				}},
				{ID: 6, Name: "OnOff", Side: "client", Conformance: &Conf{Op: opOptional}},
				{ID: 0x1D, Name: "Descriptor", Side: "server"},
				{ID: 0x11, Name: "PowerSource", Side: "deviceType"},
				{ID: 3, Name: "Identify", Side: "server"},
				{ID: 4, Name: "Groups", Side: "server"},
			}},
			{ID: id(0xFFEF), Name: "PlainLight", Revision: 1, Classification: "simple"},
			{ID: id(0xFFED), Name: "OnlyOursType", Revision: 1},
		},
		Globals: []*Element{
			{Name: "semtag", Kind: "struct", Fields: []*Element{{ID: id(0), Name: "Label", Type: "string", Conformance: &Conf{Op: opOptional}}}},
			{Name: "GlobalEnum", Kind: "enum", Fields: []*Element{{ID: id(0), Name: "Zero"}}},
			{Name: "status", Kind: "enum"},
			{Name: "Unmatched", Kind: "enum"},
		},
	}
	// The Gadget family member and a type only a definitions scope names.
	ours.Clusters = append(ours.Clusters, &Cluster{ID: id(0xFFF4), Name: "Gadget", Revision: 1, Commands: []*Element{
		{ID: id(9), Name: "Use", Direction: "request", Fields: []*Element{{Name: "S", Type: "Defs.SharedStruct", Entry: &Element{Type: "Defs.SharedStruct"}}}},
	}})

	res := Compare(ours, chip)
	got := diffStrings(res)
	for _, want := range []string{
		`Widget.EXTRA feature: chip "absent", ours "present"`,
		`Widget.Bonus attribute: chip "absent", ours "present"`,
		`Widget.StatusEnum.Extra field: chip "absent", ours "present"`,
		`Widget.Mine datatype: chip "absent", ours "present"`,
		`OnlyOurs cluster: chip "absent", ours "present"`,
		`OnlyOursType deviceType: chip "absent", ours "present"`,
		`Gone deviceType: chip "present", ours "absent"`,
		`FancyLight.Missing (server) requirement: chip "present", ours "absent"`,
		`FancyLight.Groups (server) requirement: chip "absent", ours "present"`,
		`Unmatched global datatype: chip "absent", ours "present"`,
		`LostStruct global datatype: chip "present", ours "absent"`,
		`FancyLight.Widget (server) quality: chip "singleton", ours "none"`,
		`FancyLight.Widget (server).DER conformance: chip "m", ours "o"`,
		`FancyLight.Widget (server).GONE feature requirement: chip "present", ours "absent"`,
		`FancyLight.Widget (server).Mode constraint: chip "1to4", ours "1to5"`,
		`FancyLight.Widget (server).Reset id: chip "0x40", ours "0x41"`,
		`FancyLight.Widget (server).Extra attribute requirement: chip "absent", ours "present"`,
		`FancyLight.Two condition: chip "present", ours "absent"`,
		`FancyLight.Three condition: chip "absent", ours "present"`,
		`FancyLight.RootNode.WiFi (condition) conformance: chip "m", ours "o"`,
		`FancyLight.Root Node.Thread (condition) conditionRequirement: chip "present", ours "absent"`,
		`FancyLight.RootNode.GroupcastListenerCond (condition) conditionRequirement: chip "absent", ours "present"`,
		`Widget classification: chip "application", ours "endpoint"`,
		`semtag.Label conformance: chip "o", ours "o"`,
	} {
		if want == `semtag.Label conformance: chip "o", ours "o"` {
			if slices.Contains(got, want) {
				t.Errorf("an equal conformance was reported: %s", want)
			}
			continue
		}
		if !slices.Contains(got, want) {
			t.Errorf("missing difference %s\ngot:\n%s", want, strings.Join(got, "\n"))
		}
	}
	for rule, n := range map[string]int{
		"global-attribute": 1, "fabric-index": 2, "descriptor-required": 1, "composed-device-type": 1,
		"superset-requirement": 1, "core-global": 1, "global-datatype": 2,
		"scoped-global": 1, "type-alias": 1, "value-table-m": 1, "base-device-type": 1,
	} {
		if res.Normalized[rule] != n {
			t.Errorf("rule %s applied %d times, want %d (%v)", rule, res.Normalized[rule], n, res.Normalized)
		}
	}
	if len(res.Provisional) != 1 || res.Provisional[0] != "BetaThing" {
		t.Errorf("provisional = %v", res.Provisional)
	}
	if res.NotCompared["definitions outside the snapshot"] != 1 || res.NotCompared["cluster classification"] != 0 ||
		res.NotCompared["members CHIP does not state"] != 1 || res.NotCompared["base device type"] != 0 {
		t.Errorf("not compared = %v", res.NotCompared)
	}
}

func TestCompareElementRules(t *testing.T) {
	t.Parallel()
	c := &comparer{
		res:  &Result{Normalized: map[string]int{}, Compared: map[string]int{}, NotCompared: map[string]int{}},
		ours: &Model{}, ourGlobal: map[string]*Element{},
	}
	c.cluster = &Cluster{Datatypes: []*Element{{Name: "ModeEnum", Fields: []*Element{{ID: u32(3), Name: "Auto"}}}}}
	c.ourGlobal["globalenum"] = &Element{Name: "GlobalEnum", Fields: []*Element{{ID: u32(4), Name: "Four"}}}
	path := []string{"C", "E"}
	cases := []struct {
		name       string
		chip, ours *Element
		ctx        elemCtx
		want       string // a difference, or "rule:<name>" / "nc:<aspect>"
	}{
		{"metabase enum", &Element{Type: "enum16"}, &Element{Type: "ModeEnum", Metatype: "enum", Primitive: "uint16"}, elemCtx{}, "rule:type-metabase"},
		{"metabase bitmap", &Element{Type: "map32"}, &Element{Type: "Bits", Metatype: "bitmap", Primitive: "uint32"}, elemCtx{}, "rule:type-metabase"},
		{"alias type", &Element{Type: "endpoint-id"}, &Element{Type: "endpoint-no"}, elemCtx{}, "rule:type-alias"},
		{"enum as integer", &Element{Type: "enum16"}, &Element{Type: "uint16", Metatype: "integer", Primitive: "uint16"}, elemCtx{}, "rule:enum-as-integer"},
		{"qualified type", &Element{Type: "Foo"}, &Element{Type: "Other.Foo"}, elemCtx{}, ""},
		{"type", &Element{Type: "uint8"}, &Element{Type: "uint16", Metatype: "integer", Primitive: "uint16"}, elemCtx{}, `C.E type: chip "uint8", ours "uint16"`},
		{"entry type", &Element{EntryType: "uint8"}, &Element{Entry: &Element{Type: "uint16"}}, elemCtx{}, `C.E entry type: chip "uint8", ours "uint16"`},
		{"response", &Element{Response: "GoResponse"}, &Element{Response: "status"}, elemCtx{}, `C.E response: chip "goresponse", ours "status"`},
		{"priority", &Element{Priority: "info"}, &Element{Priority: "debug"}, elemCtx{}, `C.E priority: chip "info", ours "debug"`},
		{"id", &Element{ID: u32(1)}, &Element{}, elemCtx{}, `C.E id: chip "0x1", ours "undefined"`},
		{"feature o", &Element{Conformance: &Conf{Op: opOptional}}, &Element{}, elemCtx{isFeature: true}, "rule:feature-o"},
		{"value table", &Element{Conformance: &Conf{Op: opMandatory}}, &Element{}, elemCtx{inValueTable: true}, "rule:value-table-m"},
		{"conformance", &Element{Conformance: &Conf{Op: opMandatory}}, &Element{}, elemCtx{}, `C.E conformance: chip "m", ours "undefined"`},
		{"powers", &Element{Constraint: "-4611686018427387904 to 4611686018427387904"}, &Element{Constraint: "-2^62 to 2^62"}, elemCtx{}, ""},
		{"percent", &Element{Constraint: "0 to 100"}, &Element{Constraint: "0% to 100%"}, elemCtx{}, ""},
		{"type bound", &Element{Constraint: "min 1"}, &Element{Type: "percent", Constraint: "1 to 100"}, elemCtx{}, "rule:type-bound"},
		{"constraint", &Element{Constraint: "min 1"}, &Element{Type: "uint8", Constraint: "1 to 100"}, elemCtx{}, `C.E constraint: chip "min1", ours "1to100"`},
		{"member access", &Element{Access: &Access{ReadPriv: "V"}}, &Element{}, elemCtx{isMember: true}, "rule:member-access"},
		{"datatype fabric", &Element{Access: &Access{Fabric: "F"}}, &Element{}, elemCtx{isDatatype: true}, "rule:datatype-fabric"},
		{"fabric", &Element{Access: &Access{Fabric: "S"}}, &Element{Access: &Access{}}, elemCtx{}, `C.E access fabric: chip "S", ours "absent"`},
		{"rw", &Element{Access: &Access{Rw: "RW"}}, &Element{}, elemCtx{}, `C.E access rw: chip "RW", ours "undefined"`},
		{"read privilege", &Element{Access: &Access{ReadPriv: "M"}}, &Element{Access: &Access{ReadPriv: "V"}}, elemCtx{}, `C.E access read privilege: chip "M", ours "V"`},
		{"reportable", &Element{Quality: []string{"nullable"}}, &Element{Quality: []string{"nullable", "reportable"}}, elemCtx{}, "rule:quality-reportable"},
		{"command quality", &Element{Quality: []string{"largeMessage"}}, &Element{}, elemCtx{kind: "command"}, "nc:command quality"},
		{"event quality", &Element{Quality: []string{"largeMessage"}}, &Element{}, elemCtx{kind: "event"}, "nc:event quality"},
		{"empty quality", &Element{Quality: []string{}}, &Element{Quality: []string{"fixed"}}, elemCtx{}, `C.E quality: chip "none", ours "fixed"`},
		{"null default", &Element{Default: &Value{"null", "null"}}, &Element{Quality: []string{"nullable"}}, elemCtx{}, "rule:null-default"},
		{"enum default", &Element{Default: &Value{"reference", "Auto"}}, &Element{Type: "ModeEnum", Default: &Value{"number", "3"}}, elemCtx{}, ""},
		{"global enum default", &Element{Default: &Value{"reference", "Four"}}, &Element{Type: "GlobalEnum", Default: &Value{"number", "4"}}, elemCtx{}, ""},
		{"own field default", &Element{Default: &Value{"reference", "X"}}, &Element{Fields: []*Element{{ID: u32(9), Name: "X"}}, Default: &Value{"number", "9"}}, elemCtx{}, ""},
		{"unknown reference", &Element{Default: &Value{"reference", "Nope"}}, &Element{Type: "Unknown", Default: &Value{"reference", "Nope"}}, elemCtx{}, ""},
		{"bool default", &Element{Default: &Value{"bool", "false"}}, &Element{Default: &Value{"bool", "true"}}, elemCtx{}, `C.E default: chip "0", ours "1"`},
		{"celsius", &Element{Default: &Value{"number", "700"}}, &Element{Type: "temperature", Default: &Value{"celsius", "7"}}, elemCtx{}, ""},
		{"celsius s8", &Element{Default: &Value{"number", "70"}}, &Element{Type: "SignedTemperature", Default: &Value{"celsius", "7"}}, elemCtx{}, ""},
		{"percent100ths", &Element{Default: &Value{"number", "100"}}, &Element{Type: "percent100ths", Default: &Value{"percent", "1"}}, elemCtx{}, ""},
		{"percent", &Element{Default: &Value{"number", "50"}}, &Element{Type: "percent", Default: &Value{"percent", "50"}}, elemCtx{}, ""},
		{"unscaled", &Element{Default: &Value{"number", "5"}}, &Element{Type: "uint8", Default: &Value{"celsius", "5"}}, elemCtx{}, ""},
		{"bad scale", &Element{Default: &Value{"number", "5"}}, &Element{Type: "percent", Default: &Value{"percent", "x"}}, elemCtx{}, `C.E default: chip "5", ours "x"`},
		{"empty list", &Element{Default: &Value{"reference", "empty"}}, &Element{Default: &Value{"list", "[]"}}, elemCtx{}, ""},
		{"string", &Element{Default: &Value{"reference", "XX"}}, &Element{Default: &Value{"string", "xx"}}, elemCtx{}, ""},
		{"undefined", &Element{Default: &Value{"number", "0"}}, &Element{}, elemCtx{}, `C.E default: chip "0", ours "undefined"`},
	}
	for _, tc := range cases {
		before := len(c.res.Differences)
		rules := map[string]int{}
		for k, v := range c.res.Normalized {
			rules[k] = v
		}
		nc := map[string]int{}
		for k, v := range c.res.NotCompared {
			nc[k] = v
		}
		if tc.chip.Name == "" {
			tc.chip.Name = "E"
		}
		if tc.ours.Name == "" {
			tc.ours.Name = "E"
		}
		c.element(path, tc.chip, tc.ours, tc.ctx)
		newDiffs := diffStringsOf(c.res.Differences[before:])
		switch {
		case strings.HasPrefix(tc.want, "rule:"):
			rule := strings.TrimPrefix(tc.want, "rule:")
			if c.res.Normalized[rule] != rules[rule]+1 || len(newDiffs) != 0 {
				t.Errorf("%s: want rule %s, got %v / %v", tc.name, rule, c.res.Normalized, newDiffs)
			}
		case strings.HasPrefix(tc.want, "nc:"):
			aspect := strings.TrimPrefix(tc.want, "nc:")
			if c.res.NotCompared[aspect] != nc[aspect]+1 || len(newDiffs) != 0 {
				t.Errorf("%s: want not-compared %s, got %v / %v", tc.name, aspect, c.res.NotCompared, newDiffs)
			}
		case tc.want == "":
			if len(newDiffs) != 0 {
				t.Errorf("%s: want no difference, got %v", tc.name, newDiffs)
			}
		default:
			if len(newDiffs) != 1 || newDiffs[0] != tc.want {
				t.Errorf("%s: want %s, got %v", tc.name, tc.want, newDiffs)
			}
		}
	}
}

func diffStringsOf(ds []Difference) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.String()
	}
	return out
}

func TestComparerPanicsOnUnknownNames(t *testing.T) {
	t.Parallel()
	c := &comparer{res: &Result{Normalized: map[string]int{}, NotCompared: map[string]int{}}}
	for _, f := range []func(){func() { c.normalize("nope") }, func() { c.notCompared("nope", 1) }} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("an unknown name did not panic")
				}
			}()
			f()
		}()
	}
}
