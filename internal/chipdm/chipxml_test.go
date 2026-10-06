// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"math"
	"strings"
	"testing"
)

// fixture is a small data model directory in CHIP's layout, written for
// these tests: one of each shape the reader handles.
var fixture = map[string]string{
	"clusters/WidgetBase.xml": `<?xml version="1.0"?>
<!-- a comment before the root -->
<cluster name="Widget Base Cluster" revision="3">
  <classification hierarchy="base" role="application" scope="Endpoint"/>
  <features>
    <feature bit="0" code="BASEF" name="BaseFeature"><optionalConform/></feature>
  </features>
  <dataTypes>
    <enum name="StatusEnum">
      <item value="0x00" name="Ok"><mandatoryConform/></item>
      <item value="1" name="Busy"/>
    </enum>
    <struct name="OptStruct">
      <field id="0" name="Label" type="string"><mandatoryConform/></field>
    </struct>
  </dataTypes>
  <attributes>
    <attribute id="0x0000" name="Mode" type="uint8">
      <access read="true" write="optional" readPrivilege="view" writePrivilege="manage"/>
      <mandatoryConform/>
    </attribute>
  </attributes>
  <commands>
    <command id="0x00" name="Reset" direction="commandToServer" response="Y">
      <access invokePrivilege="operate" timed="true"/>
      <mandatoryConform/>
    </command>
  </commands>
</cluster>
`,
	"clusters/Widget.xml": `<cluster id="0xFFF1" name="Widget Cluster" revision="2">
  <clusterIds><clusterId id="0xFFF1" name="Widget"/></clusterIds>
  <classification hierarchy="derived" baseCluster="Widget Base" role="application" scope="Endpoint"/>
  <features>
    <feature bit="1" code="DER" name="Derived"><mandatoryConform/></feature>
  </features>
  <attributes>
    <attribute id="0x0000" name="Mode"><disallowConform/></attribute>
  </attributes>
  <commands>
    <command id="0x00" name="Reset"><optionalConform/></command>
  </commands>
</cluster>
`,
	"clusters/Family.xml": `<cluster name="Thing Clusters" revision="1">
  <clusterIds>
    <clusterId id="0xFFF2" name="Alpha Thing"/>
    <clusterId id="0xFFF3" name="Beta Thing"><provisionalConform/></clusterId>
  </clusterIds>
  <classification hierarchy="base" role="utility" scope="Node"/>
</cluster>
`,
	"clusters/Gadget.xml": `<cluster id="0xFFF4" name="Gadget Cluster">
  <clusterIds>
    <clusterId name="Gadget Base"/>
    <clusterId id="0xFFF4" name="Gadget"/>
  </clusterIds>
  <classification role="utility" scope="Endpoint"/>
  <attributes>
    <attribute id="0x0001" name="Level" type="uint16" default="0x10">
      <access read="true" write="true" readPrivilege="view" writePrivilege="operate" fabricScoped="true"/>
      <quality nullable="true" scene="true" changeOmitted="true" quieterReporting="true" largeMessage="true" singleton="true" diagnostics="true" atomicWrite="true" persistence="fixed"/>
      <otherwiseConform>
        <mandatoryConform><feature name="LT"/></mandatoryConform>
        <optionalConform choice="a" more="true" min="1"/>
        <provisionalConform/>
      </otherwiseConform>
      <constraint><between><from value="1"/><to><attribute name="MaxLevel"/></to></between></constraint>
    </attribute>
    <attribute id="0x0002" name="Name" type="string" default="null">
      <access read="true"/>
      <quality persistence="nonVolatile"/>
      <optionalConform choice="b" max="2"><notTerm><feature name="LT"/></notTerm></optionalConform>
      <constraint><maxLength value="32"/></constraint>
      <constraint><maxCodePoints value="16"/></constraint>
    </attribute>
    <attribute id="0x0003" name="Flags" type="list" default="true">
      <access write="true" fabricSensitive="true"/>
      <mandatoryConform>
        <andTerm><feature name="A"/><feature name="B"/><condition name="Wi-Fi"/></andTerm>
      </mandatoryConform>
      <constraint><minCount value="1"/><maxCount value="4"/></constraint>
      <entry type="uint8"><constraint><allowed value="7"/></constraint></entry>
    </attribute>
    <attribute id="0x0004" name="Pick" type="StatusEnum" default="Busy">
      <mandatoryConform><equalTerm><field name="Status"/><status name="SUCCESS"/></equalTerm></mandatoryConform>
      <constraint><allowed value="1"/></constraint>
      <constraint><allowed value="2"/></constraint>
    </attribute>
    <attribute id="0x0005" name="Old" type="uint8">
      <mandatoryConform><greaterOrEqualTerm><revision value="current"/><revision value="3"/></greaterOrEqualTerm></mandatoryConform>
      <constraint><between value="0"><attribute name="Max"/></between></constraint>
    </attribute>
    <attribute id="0x0006" name="Pair" type="int8">
      <mandatoryConform><orTerm><literal value="0x02"/><number name="Named"/></orTerm></mandatoryConform>
      <constraint><between><compute><left value="1"/><operation>add</operation><right><compute><left><attribute name="A"/></left><operation>multiply</operation><right value="2"/></compute></right></compute><field name="B"><field name="C"/></field></between></constraint>
    </attribute>
    <attribute id="0x0007" name="Extremes" type="int8">
      <describedConform/>
      <constraint><min><maxOf><constant name="X"/><value value="10"/></maxOf></min><max><minOf><enum value="3"/><bitmap value="4"/></minOf></max></constraint>
      <constraint><desc/></constraint>
      <constraint><minLength value="2"/></constraint>
    </attribute>
    <attribute id="0x0008" name="Gone" type="uint8"><deprecateConform/></attribute>
    <attribute id="0x0009" name="Never" type="uint8"><obsoleteConform/></attribute>
    <attribute code="0x000A" name="Coded" type="uint8">
      <mandatoryConform><notEqualTerm><enum value="Red"/><value value="5"/></notEqualTerm></mandatoryConform>
    </attribute>
    <attribute id="0x000B" name="Compared" type="uint8">
      <mandatoryConform><orTerm>
        <greaterTerm><attribute name="A"/><literal value="1"/></greaterTerm>
        <lessTerm><attribute name="B"/><literal value="2"/></lessTerm>
        <lessOrEqualTerm><attribute name="C"/><literal value="3"/></lessOrEqualTerm>
        <greaterOrEqualTerm><attribute name="D"/><literal value="4"/></greaterOrEqualTerm>
      </orTerm></mandatoryConform>
      <constraint><min value="0"/></constraint>
      <constraint><max value="9"/></constraint>
    </attribute>
    <attribute id="0x000C" name="Kinds" type="uint8">
      <mandatoryConform><orTerm><command name="Go"/><event name="Went"/><cluster name="OnOff"/><deviceType name="Light"/></orTerm></mandatoryConform>
      <constraint><between><from><literal>  12  </literal></from><to value="13"/></between></constraint>
    </attribute>
  </attributes>
  <commands>
    <command id="0x01" name="Go" direction="commandToServer" response="GoResponse">
      <access invokePrivilege="admin"/>
      <mandatoryConform/>
      <field id="0" name="Speed" type="uint8"><mandatoryConform/></field>
    </command>
    <command id="0x01" name="GoResponse" direction="responseFromServer" response="N">
      <mandatoryConform/>
    </command>
  </commands>
  <events>
    <event id="0x00" name="Went" priority="info">
      <access readPrivilege="view"/>
      <mandatoryConform/>
      <field id="0" name="Where" type="uint8"/>
    </event>
  </events>
  <dataTypes>
    <bitmap name="Bits">
      <bitfield name="One" bit="0"><mandatoryConform/></bitfield>
      <bitfield name="Range" from="1" to="3"/>
    </bitmap>
    <number name="Num" type="uint32"/>
    <typedef name="Alias" type="uint16"/>
  </dataTypes>
</cluster>
`,
	"device_types/Base.xml": `<deviceType name="Base Device Type" revision="1">
  <clusters>
    <cluster id="0x001D" name="Descriptor" side="server"><mandatoryConform/></cluster>
  </clusters>
</deviceType>
`,
	"device_types/Light.xml": `<deviceType id="0xFFF0" name="Fancy Light" revision="2">
  <classification superset="Plain Light" class="simple" scope="endpoint"/>
  <conditions>
    <condition name="One"/>
    <condition name="Two"/>
  </conditions>
  <clusters>
    <cluster id="0xFFF1" name="Widget" side="server">
      <quality singleton="true"/>
      <mandatoryConform/>
      <features><feature code="DER"><mandatoryConform/></feature></features>
      <attributes><attribute code="0x0000" name="Mode"><optionalConform/></attribute></attributes>
    </cluster>
    <cluster id="0x0006" name="On/Off" side="client"><optionalConform/></cluster>
  </clusters>
</deviceType>
`,
	"device_types/Plain.xml": `<deviceType id="0xFFEF" name="Plain Light">
  <classification class="simple" scope="endpoint"/>
</deviceType>
`,
	"globals/Enums.xml":        `<enums><enum name="GlobalEnum"><item value="0" name="Zero"/></enum></enums>`,
	"globals/Commands.xml":     `<commands><command id="0xFE" name="AtomicRequest" direction="commandToServer"/></commands>`,
	"namespaces/Namespace.xml": `<namespace id="0x01" name="N"/>`,
	"spec_tag":                 "1.6.1-test",
	"clusters/notes.txt":       "not XML",
	"other/Ignored.xml":        "<ignored/>",
}

func fixtureFiles(t *testing.T, overrides map[string]string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for k, v := range fixture {
		files[k] = []byte(v)
	}
	for k, v := range overrides {
		if v == "" {
			delete(files, k)
			continue
		}
		files[k] = []byte(v)
	}
	return files
}

func loadFixture(t *testing.T) *Model {
	t.Helper()
	m, err := LoadFiles(fixtureFiles(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func clusterByID(m *Model, id uint32) *Cluster {
	for _, c := range m.Clusters {
		if *c.ID == id {
			return c
		}
	}
	return nil
}

func elementNamed(list []*Element, name string) *Element {
	for _, e := range list {
		if e.Name == name {
			return e
		}
	}
	return nil
}

func TestLoadFilesClusters(t *testing.T) {
	t.Parallel()
	m := loadFixture(t)

	if len(m.BaseClusters) != 1 || m.BaseClusters[0].Name != "Widget Base" || m.BaseClusters[0].Revision != 3 {
		t.Fatalf("base clusters = %+v", m.BaseClusters)
	}
	widget := clusterByID(m, 0xFFF1)
	if widget == nil || widget.Base != "Widget Base" || widget.Name != "Widget" || widget.Classification != "application" {
		t.Fatalf("widget = %+v", widget)
	}
	alpha, beta := clusterByID(m, 0xFFF2), clusterByID(m, 0xFFF3)
	if alpha == nil || beta == nil || alpha.Name != "Alpha Thing" || beta.Revision != 1 || alpha.Classification != "node" ||
		alpha.Provisional || !beta.Provisional {
		t.Fatalf("family = %+v %+v", alpha, beta)
	}
	gadget := clusterByID(m, 0xFFF4)
	if gadget == nil || gadget.Classification != "endpoint" || gadget.Revision != 1 {
		t.Fatalf("gadget = %+v", gadget)
	}

	wants := map[string]struct{ conf, constraint string }{
		"Level":    {"LT, O.a+, P", "1 to MaxLevel"},
		"Name":     {"[!LT].b2-", "max 32{16}"},
		"Flags":    {"A & B & Wi-Fi", "1 to 4[7]"},
		"Pick":     {"Status == Success", "1, 2"},
		"Old":      {"Rev >= v3", "0 to Max"},
		"Pair":     {"0x02 | Named", "1 + (A * 2) to B.C"},
		"Extremes": {"desc", "desc, min 2"}, // desc outranks the bounds it merged with (Constraint.ts serializeAtom)
		"Gone":     {"D", ""},
		"Never":    {"Z", ""},
		"Coded":    {"Red != 5", ""},
		"Compared": {"A > 1 | B < 2 | C <= 3 | D >= 4", "0 to 9"},
		"Kinds":    {"Go | Went | OnOff | Light", "12 to 13"},
	}
	for name, want := range wants {
		a := elementNamed(gadget.Attributes, name)
		if a == nil {
			t.Fatalf("attribute %s missing", name)
		}
		if got := a.Conformance.String(); got != want.conf {
			t.Errorf("%s conformance = %q, want %q", name, got, want.conf)
		}
		if a.Constraint != want.constraint {
			t.Errorf("%s constraint = %q, want %q", name, a.Constraint, want.constraint)
		}
	}

	level := elementNamed(gadget.Attributes, "Level")
	if level.Access == nil || *level.Access != (Access{Rw: "RW", ReadPriv: "V", WritePriv: "O", Fabric: "F"}) {
		t.Errorf("Level access = %+v", level.Access)
	}
	if got := strings.Join(level.Quality, " "); got != "atomic changesOmitted diagnostics fixed largeMessage nullable quieter scene singleton" {
		t.Errorf("Level quality = %q", got)
	}
	if level.Default == nil || *level.Default != (Value{Kind: "number", Text: "16"}) {
		t.Errorf("Level default = %+v", level.Default)
	}
	name := elementNamed(gadget.Attributes, "Name")
	if name.Default.Kind != "null" || strings.Join(name.Quality, " ") != "nonvolatile" || name.Access.Rw != "R" {
		t.Errorf("Name = %+v %+v", name.Default, name.Access)
	}
	flags := elementNamed(gadget.Attributes, "Flags")
	if flags.Access.Rw != "W" || flags.Access.Fabric != "S" || flags.EntryType != "uint8" || flags.Default.Kind != "bool" {
		t.Errorf("Flags = %+v entry %q default %+v", flags.Access, flags.EntryType, flags.Default)
	}
	if pick := elementNamed(gadget.Attributes, "Pick"); pick.Default.Kind != "reference" || pick.Default.Text != "Busy" {
		t.Errorf("Pick default = %+v", pick.Default)
	}
	if coded := elementNamed(gadget.Attributes, "Coded"); coded.ID == nil || *coded.ID != 0x0A {
		t.Errorf("Coded id = %v", coded.ID)
	}

	goCmd, goResp := gadget.Commands[0], gadget.Commands[1]
	if goCmd.Direction != "request" || goCmd.Response != "GoResponse" || goCmd.Access.ReadPriv != "A" || goCmd.Access.WritePriv != "A" ||
		len(goCmd.Fields) != 1 || goCmd.Fields[0].Name != "Speed" {
		t.Errorf("Go = %+v", goCmd)
	}
	if goResp.Direction != "response" || goResp.Response != "" {
		t.Errorf("GoResponse = %+v", goResp)
	}
	if ev := gadget.Events[0]; ev.Priority != "info" || ev.Access.ReadPriv != "V" || len(ev.Fields) != 1 {
		t.Errorf("Went = %+v", ev)
	}
	bits := elementNamed(gadget.Datatypes, "Bits")
	if bits.Kind != "bitmap" || bits.Fields[0].Constraint != "0" || bits.Fields[1].Constraint != "" {
		t.Errorf("Bits = %+v", bits)
	}
	if n := elementNamed(gadget.Datatypes, "Num"); n.Kind != "number" || n.Type != "uint32" {
		t.Errorf("Num = %+v", n)
	}
	if a := elementNamed(gadget.Datatypes, "Alias"); a.Kind != "typedef" {
		t.Errorf("Alias = %+v", a)
	}

	base := m.BaseClusters[0]
	if e := elementNamed(base.Datatypes, "StatusEnum"); e.Kind != "enum" || len(e.Fields) != 2 || *e.Fields[1].ID != 1 ||
		e.Fields[1].Conformance != nil {
		t.Errorf("StatusEnum = %+v", e)
	}
	if mode := base.Attributes[0]; mode.Access.Rw != "R[W]" || mode.Access.WritePriv != "M" {
		t.Errorf("base Mode access = %+v", mode.Access)
	}
	if reset := base.Commands[0]; !reset.Access.Timed || reset.Response != "status" {
		t.Errorf("base Reset = %+v", reset)
	}
	if f := base.Features[0]; f.Name != "BASEF" || f.Constraint != "0" || f.Conformance.String() != "O" {
		t.Errorf("base feature = %+v", f)
	}
}

func TestLoadFilesDeviceTypesAndGlobals(t *testing.T) {
	t.Parallel()
	m := loadFixture(t)
	if len(m.DeviceTypes) != 3 || m.DeviceTypes[0].ID != nil {
		t.Fatalf("device types = %+v (the id-less base sorts first)", m.DeviceTypes)
	}
	plain, light := m.DeviceTypes[1], m.DeviceTypes[2]
	if *plain.ID != 0xFFEF || plain.Revision != 1 || *light.ID != 0xFFF0 {
		t.Fatalf("order = %v %v", *plain.ID, *light.ID)
	}
	if light.Superset != "Plain Light" || light.Classification != "simple" || light.Revision != 2 || len(light.Requirements) != 2 {
		t.Errorf("light = %+v", light)
	}
	if r := light.Requirements[1]; r.Side != "client" || r.ID != 6 || r.Conformance.String() != "O" {
		t.Errorf("client requirement = %+v", r)
	}
	if len(m.Globals) != 1 || m.Globals[0].Name != "GlobalEnum" {
		t.Errorf("globals = %+v", m.Globals)
	}
	want := map[string]int{
		"device-type conditions": 2, "device-type element requirements": 2, "device-type cluster quality": 1,
		"global commands": 1, "semantic namespaces": 1,
	}
	for k, v := range want {
		if m.Uncompared[k] != v {
			t.Errorf("uncompared %s = %d, want %d", k, m.Uncompared[k], v)
		}
	}
}

func TestLoadFilesErrors(t *testing.T) {
	t.Parallel()
	cluster := func(body string) string {
		return `<cluster id="0x1" name="X"><clusterIds><clusterId id="0x1" name="X"/></clusterIds>` + body + `</cluster>`
	}
	attr := func(inner string) string {
		return cluster(`<attributes><attribute id="0" name="A" type="uint8">` + inner + `</attribute></attributes>`)
	}
	cases := map[string]map[string]string{
		"no XML":              {"clusters/WidgetBase.xml": "", "clusters/Widget.xml": "", "clusters/Family.xml": "", "clusters/Gadget.xml": "", "device_types/Base.xml": "", "device_types/Light.xml": "", "device_types/Plain.xml": "", "globals/Enums.xml": "", "globals/Commands.xml": ""},
		"malformed":           {"clusters/X.xml": "<cluster"},
		"trailing element":    {"clusters/X.xml": cluster("") + "<extra/>"},
		"trailing text":       {"clusters/X.xml": cluster("") + "text"},
		"wrong root":          {"clusters/X.xml": "<deviceType/>"},
		"wrong dt root":       {"device_types/X.xml": "<cluster/>"},
		"bad revision":        {"clusters/X.xml": `<cluster revision="x"/>`},
		"unnamed base":        {"clusters/X.xml": `<cluster/>`},
		"unnamed id":          {"clusters/X.xml": `<cluster><clusterIds><clusterId id="1"/></clusterIds></cluster>`},
		"unnamed base id":     {"clusters/X.xml": `<cluster><clusterIds><clusterId/></clusterIds></cluster>`},
		"bad cluster id":      {"clusters/X.xml": `<cluster><clusterIds><clusterId id="z"/></clusterIds></cluster>`},
		"unnamed feature":     {"clusters/X.xml": cluster(`<features><feature bit="0"/></features>`)},
		"bad feature bit":     {"clusters/X.xml": cluster(`<features><feature code="F" bit="b"/></features>`)},
		"bad feature conf":    {"clusters/X.xml": cluster(`<features><feature code="F"><bogusConform/><mandatoryConform/><optionalConform/></feature></features>`)},
		"unnamed attribute":   {"clusters/X.xml": cluster(`<attributes><attribute id="0"/></attributes>`)},
		"bad attribute id":    {"clusters/X.xml": cluster(`<attributes><attribute id="q" name="A"/></attributes>`)},
		"two conformances":    {"clusters/X.xml": attr(`<mandatoryConform/><optionalConform/>`)},
		"unknown conform":     {"clusters/X.xml": attr(`<otherwiseConform><weirdConform/></otherwiseConform>`)},
		"two expressions":     {"clusters/X.xml": attr(`<mandatoryConform><feature name="A"/><feature name="B"/></mandatoryConform>`)},
		"conditional flag":    {"clusters/X.xml": attr(`<provisionalConform><feature name="A"/></provisionalConform>`)},
		"choice range":        {"clusters/X.xml": attr(`<optionalConform choice="a" min="1" max="2"/>`)},
		"choice bad min":      {"clusters/X.xml": attr(`<optionalConform choice="a" min="x"/>`)},
		"choice bad max":      {"clusters/X.xml": attr(`<optionalConform choice="a" max="x"/>`)},
		"choice bad more":     {"clusters/X.xml": attr(`<optionalConform choice="a" more="maybe"/>`)},
		"one operand":         {"clusters/X.xml": attr(`<mandatoryConform><andTerm><feature name="A"/></andTerm></mandatoryConform>`)},
		"bad operand":         {"clusters/X.xml": attr(`<mandatoryConform><andTerm><feature name="A"/><bogus/></andTerm></mandatoryConform>`)},
		"bad first operand":   {"clusters/X.xml": attr(`<mandatoryConform><andTerm><bogus/><feature name="A"/></andTerm></mandatoryConform>`)},
		"not arity":           {"clusters/X.xml": attr(`<mandatoryConform><notTerm/></mandatoryConform>`)},
		"bad not":             {"clusters/X.xml": attr(`<mandatoryConform><notTerm><bogus/></notTerm></mandatoryConform>`)},
		"unnamed feature ref": {"clusters/X.xml": attr(`<mandatoryConform><feature/></mandatoryConform>`)},
		"unnamed status":      {"clusters/X.xml": attr(`<mandatoryConform><status/></mandatoryConform>`)},
		"valueless enum":      {"clusters/X.xml": attr(`<mandatoryConform><enum/></mandatoryConform>`)},
		"valueless literal":   {"clusters/X.xml": attr(`<mandatoryConform><literal/></mandatoryConform>`)},
		"bad optional expr":   {"clusters/X.xml": attr(`<optionalConform><bogus/></optionalConform>`)},
		"revision against":    {"clusters/X.xml": attr(`<mandatoryConform><greaterOrEqualTerm><revision value="old"/><revision value="1"/></greaterOrEqualTerm></mandatoryConform>`)},
		"revision value":      {"clusters/X.xml": attr(`<mandatoryConform><greaterOrEqualTerm><revision value="current"/><revision value="x"/></greaterOrEqualTerm></mandatoryConform>`)},
		"revision missing":    {"clusters/X.xml": attr(`<mandatoryConform><greaterOrEqualTerm><revision value="current"/><revision/></greaterOrEqualTerm></mandatoryConform>`)},
		"constraint element":  {"clusters/X.xml": attr(`<constraint><bogus/></constraint>`)},
		"between arity":       {"clusters/X.xml": attr(`<constraint><between><literal value="1"/></between></constraint>`)},
		"between lower":       {"clusters/X.xml": attr(`<constraint><between value="1"><bogus/></between></constraint>`)},
		"between first":       {"clusters/X.xml": attr(`<constraint><between><bogus/><literal value="1"/></between></constraint>`)},
		"between second":      {"clusters/X.xml": attr(`<constraint><between><literal value="1"/><bogus/></between></constraint>`)},
		"between from":        {"clusters/X.xml": attr(`<constraint><between><from/><to value="1"/></between></constraint>`)},
		"between to":          {"clusters/X.xml": attr(`<constraint><between><from value="1"/><to/></between></constraint>`)},
		"allowed bound":       {"clusters/X.xml": attr(`<constraint><allowed/></constraint>`)},
		"min bound":           {"clusters/X.xml": attr(`<constraint><min/></constraint>`)},
		"max bound":           {"clusters/X.xml": attr(`<constraint><max/></constraint>`)},
		"code points":         {"clusters/X.xml": attr(`<constraint><maxCodePoints value="x"/></constraint>`)},
		"entry constraint":    {"clusters/X.xml": attr(`<entry type="uint8"><constraint><bogus/></constraint></entry>`)},
		"unnamed ref":         {"clusters/X.xml": attr(`<constraint><max><attribute/></max></constraint>`)},
		"unnamed path":        {"clusters/X.xml": attr(`<constraint><max><attribute name="A"><field/></attribute></max></constraint>`)},
		"valueless bound":     {"clusters/X.xml": attr(`<constraint><max><literal/></max></constraint>`)},
		"operation":           {"clusters/X.xml": attr(`<constraint><max><compute><left value="1"/><operation>pow</operation><right value="2"/></compute></max></constraint>`)},
		"no operation":        {"clusters/X.xml": attr(`<constraint><max><compute><left value="1"/><right value="2"/></compute></max></constraint>`)},
		"no operand":          {"clusters/X.xml": attr(`<constraint><max><compute><left value="1"/><operation>add</operation></compute></max></constraint>`)},
		"left operand":        {"clusters/X.xml": attr(`<constraint><max><compute><left/><operation>add</operation><right value="1"/></compute></max></constraint>`)},
		"right operand":       {"clusters/X.xml": attr(`<constraint><max><compute><left value="1"/><operation>add</operation><right><bogus/></right></compute></max></constraint>`)},
		"maxOf operand":       {"clusters/X.xml": attr(`<constraint><max><maxOf><bogus/></maxOf></max></constraint>`)},
		"read access":         {"clusters/X.xml": attr(`<access read="maybe"/>`)},
		"write access":        {"clusters/X.xml": attr(`<access write="sometimes"/>`)},
		"sensitive access":    {"clusters/X.xml": attr(`<access fabricSensitive="x"/>`)},
		"scoped access":       {"clusters/X.xml": attr(`<access fabricScoped="x"/>`)},
		"invoke privilege":    {"clusters/X.xml": attr(`<access invokePrivilege="root"/>`)},
		"read privilege":      {"clusters/X.xml": attr(`<access readPrivilege="root"/>`)},
		"write privilege":     {"clusters/X.xml": attr(`<access writePrivilege="root"/>`)},
		"timed access":        {"clusters/X.xml": attr(`<access timed="x"/>`)},
		"quality flag":        {"clusters/X.xml": attr(`<quality nullable="x"/>`)},
		"persistence":         {"clusters/X.xml": attr(`<quality persistence="volatile"/>`)},
		"field":               {"clusters/X.xml": attr(`<field id="0"/>`)},
		"command":             {"clusters/X.xml": cluster(`<commands><command id="0"/></commands>`)},
		"event":               {"clusters/X.xml": cluster(`<events><event id="0"/></events>`)},
		"datatype kind":       {"clusters/X.xml": cluster(`<dataTypes><union name="U"/></dataTypes>`)},
		"enum":                {"clusters/X.xml": cluster(`<dataTypes><enum/></dataTypes>`)},
		"enum item":           {"clusters/X.xml": cluster(`<dataTypes><enum name="E"><item value="0"/></enum></dataTypes>`)},
		"enum value":          {"clusters/X.xml": cluster(`<dataTypes><enum name="E"><item name="I" value="z"/></enum></dataTypes>`)},
		"enum conformance":    {"clusters/X.xml": cluster(`<dataTypes><enum name="E"><item name="I" value="0"><bogusConform/><mandatoryConform/><optionalConform/></item></enum></dataTypes>`)},
		"bitmap":              {"clusters/X.xml": cluster(`<dataTypes><bitmap/></dataTypes>`)},
		"bitfield":            {"clusters/X.xml": cluster(`<dataTypes><bitmap name="B"><bitfield bit="0"/></bitmap></dataTypes>`)},
		"bitfield bit":        {"clusters/X.xml": cluster(`<dataTypes><bitmap name="B"><bitfield name="F" bit="x"/></bitmap></dataTypes>`)},
		"bitfield conf":       {"clusters/X.xml": cluster(`<dataTypes><bitmap name="B"><bitfield name="F" bit="0"><mandatoryConform/><optionalConform/></bitfield></bitmap></dataTypes>`)},
		"struct":              {"clusters/X.xml": cluster(`<dataTypes><struct/></dataTypes>`)},
		"global datatype":     {"globals/Enums.xml": `<enums><union name="U"/></enums>`},
		"dt name":             {"device_types/X.xml": `<deviceType id="1"/>`},
		"dt id":               {"device_types/X.xml": `<deviceType id="x" name="D"/>`},
		"dt revision":         {"device_types/X.xml": `<deviceType id="1" name="D" revision="x"/>`},
		"requirement name":    {"device_types/X.xml": `<deviceType id="1" name="D"><clusters><cluster id="1"/></clusters></deviceType>`},
		"requirement id":      {"device_types/X.xml": `<deviceType id="1" name="D"><clusters><cluster name="C"/></clusters></deviceType>`},
		"requirement bad id":  {"device_types/X.xml": `<deviceType id="1" name="D"><clusters><cluster id="z" name="C"/></clusters></deviceType>`},
		"requirement conf":    {"device_types/X.xml": `<deviceType id="1" name="D"><clusters><cluster id="1" name="C"><bogusConform/><mandatoryConform/><optionalConform/></cluster></clusters></deviceType>`},
	}
	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := LoadFiles(fixtureFiles(t, override)); err == nil {
				t.Errorf("LoadFiles accepted %v", override)
			}
		})
	}
}

func TestLoaderUnknownDirectory(t *testing.T) {
	t.Parallel()
	l := &xmlLoader{}
	if err := l.addFile("elsewhere", "X.xml", []byte("<x/>")); err == nil {
		t.Error("an unknown directory was accepted")
	}
}

func TestStatusName(t *testing.T) {
	t.Parallel()
	if got := statusName("UNSUPPORTED_ACCESS__X"); got != "UnsupportedAccessX" {
		t.Errorf("statusName = %q", got)
	}
}

func TestTranslateValue(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]Value{
		"null": {"null", "null"}, "true": {"bool", "true"}, "false": {"bool", "false"},
		"0x1F": {"number", "31"}, "-3": {"number", "-3"}, "0b101": {"number", "5"}, "1.50": {"number", "1.5"},
		"+7": {"number", "7"}, "-0": {"number", "0"}, "Busy": {"reference", "Busy"}, "1.": {"reference", "1."},
		"0xZZ": {"reference", "0xZZ"}, "0b2": {"reference", "0b2"}, "": {"reference", ""},
		"18446744073709551615": {"number", "18446744073709551615"}, "1.2.3": {"reference", "1.2.3"},
		"99999999999999999999": {"reference", "99999999999999999999"},
	} {
		if got := *translateValue(in); got != want {
			t.Errorf("translateValue(%q) = %+v, want %+v", in, got, want)
		}
	}
}

// asInt holds a number past the int32 range at the bound rather than letting
// it wrap on a platform whose int is 32 bits wide.
func TestAsIntIsBounded(t *testing.T) {
	t.Parallel()
	if got := asInt(7); got != 7 {
		t.Errorf("asInt(7) = %d", got)
	}
	if got := asInt(math.MaxUint32); got != math.MaxInt32 {
		t.Errorf("asInt(MaxUint32) = %d, want %d", got, math.MaxInt32)
	}
}
