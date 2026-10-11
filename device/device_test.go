// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	evsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	ptdef "github.com/SukramJ/go-fabric/cluster/spec/powertopology"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
)

// hostServer is a host's own cluster server.
type hostServer struct {
	id      uint32
	changed func()
}

func (h *hostServer) MatterClusterID() uint32                              { return h.id }
func (*hostServer) MatterRead(uint32) (any, bool)                          { return nil, false }
func (*hostServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*hostServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*hostServer) MatterReportable() []uint32                             { return []uint32{0, 1} }

func (h *hostServer) OnMatterValueChanged(cb func()) func() {
	h.changed = cb
	return func() { h.changed = nil }
}

var _ contract.ChangeNotifier = (*hostServer)(nil)

// TestFeatureSelection: a selection must pass the cluster's feature
// conformance and keep the features the device type mandates
// (OnOffServer.with("Lighting"), dimmable-light.ts:52).
func TestFeatureSelection(t *testing.T) {
	t.Parallel()
	opts := testOptions("lamp")
	// Leaving out the mandated Lighting feature is refused, naming it.
	_, err := NewDimmableLight(opts, WithFeatures(onoffdef.ClusterID, uint32(onoffdef.FeatureDeadFrontBehavior)))
	if !errors.Is(err, ErrConfiguration) || !strings.Contains(err.Error(), "OnOff feature LT") {
		t.Errorf("selection without LT: %v", err)
	}
	// A selection the cluster's conformance forbids is refused.
	_, err = NewDimmableLight(opts, WithFeatures(onoffdef.ClusterID, uint32(onoffdef.FeatureLighting|onoffdef.FeatureOffOnly)))
	if !errors.Is(err, spec.ErrFeatureSelection) {
		t.Errorf("LT|OFFONLY: %v", err)
	}
	// A legal superset is accepted and served.
	d, err := NewDimmableLight(opts, WithFeatures(onoffdef.ClusterID, uint32(onoffdef.FeatureLighting|onoffdef.FeatureDeadFrontBehavior)))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := d.OnOff().MatterRead(spec.AttrFeatureMap); v != uint32(onoffdef.FeatureLighting|onoffdef.FeatureDeadFrontBehavior) {
		t.Errorf("FeatureMap %v", v)
	}
	// The mandated features alone, by default.
	d, err = NewDimmableLight(opts)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := d.LevelControl().MatterRead(spec.AttrFeatureMap); v != uint32(levelcontroldef.FeatureOnOff|levelcontroldef.FeatureLighting) {
		t.Errorf("LevelControl FeatureMap %v", v)
	}
	// A mandatory-with-extension cluster takes the host's selection.
	if _, err := NewSmokeCoAlarm(testOptions("alarm"), WithFeatures(0x005C, 1)); err != nil {
		t.Errorf("SmokeCoAlarm with SMOKE: %v", err)
	}
}

// TestWithServerReplacesTheDefault: a host server takes the cluster's
// place; the typed accessor answers nil for a server of another type.
func TestWithServerReplacesTheDefault(t *testing.T) {
	t.Parallel()
	own := &hostServer{id: onoffdef.ClusterID}
	d, err := NewDimmableLight(testOptions("lamp"), WithServer(own))
	if err != nil {
		t.Fatal(err)
	}
	if d.Server(onoffdef.ClusterID) != own || d.OnOff() != nil {
		t.Error("the host's OnOff server is not mounted in place of the default")
	}
	if d.LevelControl() == nil {
		t.Error("the default LevelControl went missing")
	}
	if n := len(d.Servers()); n != 2 || !slices.Equal(serverIDs(d.MatterClusterServers()), []uint32{0x0006, 0x0008}) {
		t.Errorf("servers %v", serverIDs(d.Servers()))
	}
	// An optional cluster the host supplies is mounted.
	lc := &hostServer{id: levelcontroldef.ClusterID}
	l, err := NewOnOffLight(testOptions("l"), WithServer(lc))
	if err != nil || l.Server(levelcontroldef.ClusterID) != lc {
		t.Errorf("optional host LevelControl: %v", err)
	}
}

func serverIDs(servers []contract.ClusterServer) []uint32 {
	out := make([]uint32, 0, len(servers))
	for _, s := range servers {
		out = append(out, s.MatterClusterID())
	}
	return out
}

// TestOptionsRefused: options the type does not allow.
func TestOptionsRefused(t *testing.T) {
	t.Parallel()
	opts := testOptions("x")
	for name, tc := range map[string]struct {
		ty   *Type
		opts Options
		with []Option
		want string
	}{
		"nil type":         {nil, opts, nil, "no device type"},
		"no key":           {&OnOffLightType, Options{}, nil, "needs a StableKey"},
		"nil server":       {&OnOffLightType, opts, []Option{WithServer(nil)}, "WithServer(nil)"},
		"foreign cluster":  {&OnOffLightType, opts, []Option{WithOptional(0x0201)}, "no server cluster requirement for cluster 0x0201"},
		"assembler's":      {&OnOffLightType, opts, []Option{WithServer(&hostServer{id: 0x0003})}, "mounts Identify (0x0003) itself"},
		"features of host": {&OnOffLightType, opts, []Option{WithServer(&hostServer{id: 6}), WithFeatures(6, 1)}, "WithFeatures for cluster 0x0006"},
		"initial of host":  {&OnOffLightType, opts, []Option{WithServer(&hostServer{id: 6}), WithInitial(6, nil)}, "WithInitial for cluster 0x0006"},
		"bad initial":      {&OnOffLightType, opts, []Option{WithInitial(6, map[uint32]any{0: uint8(7)})}, "OnOff"},
		"no definition":    {&RootNodeType, opts, nil, "no generated definition"},
	} {
		_, err := New(tc.ty, tc.opts, tc.with...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	// A nil Option is ignored.
	if _, err := New(&OnOffLightType, opts, nil); err != nil {
		t.Error(err)
	}
}

// TestOptionalClusterWithItsAlterations: LevelControl is optional on an
// OnOffLight and not mounted by default (on-off-light.ts:111, 126-131);
// opted in, it carries the features the type mandates
// (on-off-light.ts:83-91) and the host's initial values.
func TestOptionalClusterWithItsAlterations(t *testing.T) {
	t.Parallel()
	d, err := NewOnOffLight(testOptions("l"))
	if err != nil {
		t.Fatal(err)
	}
	if d.LevelControl() != nil {
		t.Fatal("LevelControl mounted by default")
	}
	d, err = NewOnOffLight(testOptions("l"), WithOptional(levelcontroldef.ClusterID),
		WithInitial(levelcontroldef.ClusterID, map[uint32]any{levelcontroldef.AttrCurrentLevel: uint8(42)}))
	if err != nil {
		t.Fatal(err)
	}
	lc := d.LevelControl()
	for attr, want := range map[uint32]any{
		levelcontroldef.AttrCurrentLevel: uint8(42),
		spec.AttrFeatureMap:              uint32(levelcontroldef.FeatureOnOff | levelcontroldef.FeatureLighting),
	} {
		if v, _ := lc.MatterRead(attr); v != want {
			t.Errorf("attribute 0x%04X = %v (%T), want %v", attr, v, v, want)
		}
	}
	// MinLevel and MaxLevel ("Rev >= v7, O") are not served by the
	// generated server, which leaves a revision gate conditional
	// (cluster/spec/conformance.go); their stated defaults apply only to
	// a served attribute.
	if lc.Serves(levelcontroldef.AttrMinLevel) {
		t.Error("MinLevel served; the device type's default 1 should now be checked here")
	}
}

// TestElementAlterationsApply: an element a requirement makes mandatory is
// served, and its stated default is its initial value
// (ClusterRequirements.ts:151-176).
func TestElementAlterationsApply(t *testing.T) {
	t.Parallel()
	ty := &Type{ID: 0xFFF0, Name: "Synthetic", Servers: []ClusterRequirement{{
		ID: levelcontroldef.ClusterID, Name: "LevelControl", Mandatory: true, Features: 0x3, FeatureNames: []string{"OO", "LT"},
		Elements: []ElementRequirement{
			{Element: schema.RequirementAttribute, Name: "MinLevel", ID: levelcontroldef.AttrMinLevel, Presence: PresenceMandatory, HasDefault: true, Default: 1},
			{Element: schema.RequirementAttribute, Name: "MaxLevel", ID: levelcontroldef.AttrMaxLevel, Presence: PresenceMandatory, HasDefault: true, Default: 254},
			{Element: schema.RequirementCommand, Name: "MoveToClosestFrequency", ID: levelcontroldef.CmdMoveToClosestFrequency, Presence: PresenceOptional},
			{Element: schema.RequirementEvent, Name: "None", ID: 0, Presence: PresenceUnchanged},
		},
		Definition: levelcontroldef.Definition,
	}}}
	d, err := New(ty, testOptions("s"))
	if err != nil {
		t.Fatal(err)
	}
	lc := serverAs[*spec.Server](d, levelcontroldef.ClusterID)
	if v, _ := lc.Value(levelcontroldef.AttrMinLevel); v != uint8(1) {
		t.Errorf("MinLevel %v (%T)", v, v)
	}
	if v, _ := lc.Value(levelcontroldef.AttrMaxLevel); v != uint8(254) {
		t.Errorf("MaxLevel %v", v)
	}
	// A mandated element the feature selection disallows is refused.
	ty.Servers[0].Elements = append(ty.Servers[0].Elements,
		ElementRequirement{Element: schema.RequirementCommand, Name: "X", ID: 0x7F, Presence: PresenceMandatory},
		ElementRequirement{Element: schema.RequirementEvent, Name: "Y", ID: 0x7F, Presence: PresenceMandatory})
	if _, err := New(ty, testOptions("s")); !errors.Is(err, spec.ErrUnknownElement) {
		t.Errorf("unknown mandated command: %v", err)
	}
}

// TestOnChangedFiresOnSet: OnChanged reaches the server's own change
// notification (events.onOff.onOff$Changed in matter.js).
func TestOnChangedFiresOnSet(t *testing.T) {
	t.Parallel()
	d, err := NewOnOffLight(testOptions("l"))
	if err != nil {
		t.Fatal(err)
	}
	var got [][]uint32
	stop := d.OnChanged(onoffdef.ClusterID, func(ids []uint32) { got = append(got, ids) })
	if err := d.OnOff().Set(onoffdef.AttrOnOff, true); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !slices.Equal(got[0], []uint32{onoffdef.AttrOnOff}) {
		t.Errorf("notified %v", got)
	}
	stop()
	_ = d.OnOff().Set(onoffdef.AttrOnOff, false)
	if len(got) != 1 {
		t.Error("notified after unsubscribe")
	}
	// A ChangeNotifier server reports every attribute it reports.
	own := &hostServer{id: onoffdef.ClusterID}
	h, err := NewOnOffLight(testOptions("h"), WithServer(own))
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint32
	h.OnChanged(onoffdef.ClusterID, func(a []uint32) { ids = a })
	own.changed()
	if !slices.Equal(ids, []uint32{0, 1}) {
		t.Errorf("ChangeNotifier ids %v", ids)
	}
	// A cluster the device does not mount never fires.
	h.OnChanged(0x0201, func([]uint32) { t.Error("fired") })()
}

// TestParts: a part must be a component the type names; BridgedNode hosts
// any; cycles are refused; Spec carries parts recursively.
func TestParts(t *testing.T) {
	t.Parallel()
	light, _ := NewOnOffLight(testOptions("light"))
	ps, err := NewPowerSource(testOptions("ps"), WithFeatures(0x002F, 1))
	if err != nil {
		t.Fatal(err)
	}
	// A simple device type that states no component refuses a part.
	if err := light.AddPart(ps.Device); !errors.Is(err, ErrComposition) {
		t.Errorf("OnOffLight took a PowerSource part: %v", err)
	}
	alarm, err := NewSmokeCoAlarm(testOptions("alarm"), WithFeatures(0x005C, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := alarm.AddPart(ps.Device); err != nil {
		t.Fatal(err)
	}
	if err := alarm.AddPart(ps.Device); err != nil || len(alarm.Parts()) != 1 {
		t.Errorf("a second add: %v, %d parts", err, len(alarm.Parts()))
	}
	if err := alarm.AddPart(nil); !errors.Is(err, ErrComposition) {
		t.Error("nil part accepted")
	}
	if err := alarm.AddPart(alarm.Device); !errors.Is(err, ErrComposition) {
		t.Error("self as part accepted")
	}
	// DimmableLight derives from OnOffLight; neither names a component.
	dim, _ := NewDimmableLight(testOptions("dim"))
	if err := dim.AddPart(light.Device); !errors.Is(err, ErrComposition) {
		t.Error("DimmableLight took a part")
	}
	node, err := NewBridgedNode(testOptions("node"))
	if err != nil {
		t.Fatal(err)
	}
	if err := node.AddPart(light.Device); err != nil {
		t.Errorf("BridgedNode refused an application part: %v", err)
	}
	if err := light.AddPart(node.Device); !errors.Is(err, ErrComposition) {
		t.Error("a cycle was accepted")
	}

	s := alarm.Spec()
	if len(s.Parts) != 1 || s.Parts[0].DeviceType != PowerSourceID || s.Parts[0].Source != ps.Device {
		t.Errorf("parts %+v", s.Parts)
	}
}

// TestSpec: Options map onto endpoint.Spec, and the device is the
// endpoint's source.
func TestSpec(t *testing.T) {
	t.Parallel()
	reach := func() bool { return false }
	d, err := NewOnOffPlugInUnit(Options{
		StableKey: endpoint.StringKey("k"), Name: "Plug", Vendor: "ACME", DeviceAddress: "dev-1",
		ChannelAddress: "dev-1:3", Reachable: reach, Conditions: []string{"Active"}, NodeLabel: "Kitchen",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := d.Spec()
	if s.StableKey != endpoint.StringKey("k") || s.FriendlyName != "Plug" || s.VendorName != "ACME" ||
		s.DeviceAddress != "dev-1" || s.ChannelAddress != "dev-1:3" || s.Availability() || s.NodeLabel != "Kitchen" ||
		!slices.Equal(s.DeviceConditions, []string{"Active"}) || s.DeviceType != OnOffPlugInUnitID {
		t.Errorf("spec %+v", s)
	}
	if s.Source != d.Device || d.MatterDeviceType() != OnOffPlugInUnitID {
		t.Error("the device is not the spec's source")
	}
	topo := assemble(t, d.Device)
	if len(topo.Bridged()) != 1 || len(violationsOf(topo)) != 0 {
		t.Errorf("assembled %d endpoints, violations %v", len(topo.Bridged()), violationsOf(topo))
	}
}

// TestTypeLookups covers the registry and the requirement lookups.
func TestTypeLookups(t *testing.T) {
	t.Parallel()
	if ty, ok := TypeByID(DimmableLightID); !ok || ty != &DimmableLightType {
		t.Error("TypeByID(DimmableLight)")
	}
	if _, ok := TypeByID(0xFFFF); ok {
		t.Error("TypeByID(0xFFFF)")
	}
	if typeByName("Nope") != nil {
		t.Error("typeByName(Nope)")
	}
	r, ok := DimmableLightType.Server(levelcontroldef.ClusterID)
	if !ok {
		t.Fatal("no LevelControl requirement")
	}
	if _, ok := DimmableLightType.Server(0x0201); ok {
		t.Error("Thermostat requirement on a light")
	}
	if e, ok := r.Element(schema.RequirementAttribute, levelcontroldef.AttrMinLevel); !ok || e.Default != 1 {
		t.Errorf("MinLevel %+v", e)
	}
	if _, ok := r.Element(schema.RequirementCommand, levelcontroldef.AttrMinLevel); ok {
		t.Error("command 2 found")
	}
	if got := featureNames(&ClusterRequirement{FeatureNames: []string{"A"}}, 1); !slices.Equal(got, []string{"A"}) {
		t.Errorf("featureNames without definition %v", got)
	}
}

// TestTypedDefault covers each attribute kind a numeric default converts
// to.
func TestTypedDefault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind spec.Kind
		v    float64
		want any
		ok   bool
	}{
		{spec.KindUint, 3, uint64(3), true},
		{spec.KindUint, -1, nil, false},
		{spec.KindInt, -3, int64(-3), true},
		{spec.KindFloat64, 1.5, 1.5, true},
		{spec.KindBool, 1, nil, false},
	} {
		got, ok := typedDefault(&spec.Attribute{Type: spec.Type{Kind: tc.kind}}, tc.v)
		if got != tc.want || ok != tc.ok {
			t.Errorf("kind %d %v: %v %v", tc.kind, tc.v, got, ok)
		}
	}
	if _, ok := typedDefault(nil, 1); ok {
		t.Error("nil attribute")
	}
}

// TestFeatureMasksMatchTheDefinitions: every generated mandated-feature
// mask is the features named, through the definition's Feature list.
func TestFeatureMasksMatchTheDefinitions(t *testing.T) {
	t.Parallel()
	for _, ty := range Types() {
		for _, r := range ty.Servers {
			if r.Definition == nil {
				continue
			}
			var mask uint32
			for _, name := range r.FeatureNames {
				f := r.Definition.Feature(name)
				if f == nil {
					t.Errorf("%s %s: no feature %s", ty.Name, r.Name, name)
					continue
				}
				mask |= f.Mask()
			}
			if mask != r.Features {
				t.Errorf("%s %s: Features 0x%X, the names say 0x%X", ty.Name, r.Name, r.Features, mask)
			}
			if r.Definition.ID != r.ID {
				t.Errorf("%s %s: definition of 0x%04X", ty.Name, r.Name, r.Definition.ID)
			}
		}
	}
}

// descriptorClientList reads the assembled Descriptor ClientList of the
// endpoint whose source is d.
func descriptorClientList(t *testing.T, topo *endpoint.Topology, d *Device) []uint32 {
	t.Helper()
	for _, ep := range topo.Endpoints {
		if ep.Source != contract.EndpointSource(d) {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			if srv.MatterClusterID() == 0x001D {
				v, _ := srv.MatterRead(0x0002)
				l, _ := v.([]uint32)
				return l
			}
		}
	}
	t.Fatal("no Descriptor for the device")
	return nil
}

// TestClientListCarriesTheMandatoryClientClusters: the device type's
// mandatory client clusters reach the Descriptor's ClientList
// (MutableEndpoint.ts:53-61, DescriptorServer.ts:284-290) — OtaRequestor's
// OtaSoftwareUpdateProvider, Doorbell's Chime; a type with only optional
// clients declares none.
func TestClientListCarriesTheMandatoryClientClusters(t *testing.T) {
	t.Parallel()
	ota, err := NewOtaRequestor(testOptions("ota"), WithServer(&hostServer{id: 0x002A}))
	if err != nil {
		t.Fatal(err)
	}
	bell, err := NewDoorbell(testOptions("bell"), WithServer(core.NewBinding()))
	if err != nil {
		t.Fatal(err)
	}
	light, _ := NewOnOffLight(testOptions("light"))
	if got := ota.Spec().ClientClusters; !slices.Equal(got, []uint32{0x0029}) {
		t.Errorf("OtaRequestor ClientClusters %v", got)
	}
	if light.Spec().ClientClusters != nil {
		t.Error("OnOffLight declares a client cluster")
	}
	topo := assemble(t, ota.Device, bell.Device, light.Device)
	for d, want := range map[*Device][]uint32{ota.Device: {0x0029}, bell.Device: {0x0556}, light.Device: nil} {
		if got := descriptorClientList(t, topo, d); !slices.Equal(got, want) {
			t.Errorf("%s ClientList %v, want %v", d.Type().Name, got, want)
		}
	}
}

// TestDefaultSelectionKeepsTheClusterMandatedFeatures: EnergyEvse's PREF
// ("M" in the cluster) is in its default selection, so the type constructs
// with defaults.
func TestDefaultSelectionKeepsTheClusterMandatedFeatures(t *testing.T) {
	t.Parallel()
	d, err := NewEnergyEvse(testOptions("evse"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := d.EnergyEvse().MatterRead(spec.AttrFeatureMap); v != uint32(evsedef.FeatureChargingPreferences) {
		t.Errorf("EnergyEvse FeatureMap %v", v)
	}
}

// TestWithElements: optional attributes of a default server are served,
// the cluster is mounted; features in it, or a host server for the
// cluster, are refused.
func TestWithElements(t *testing.T) {
	t.Parallel()
	el := WithElements(epmdef.ClusterID, spec.Options{Attributes: []uint32{epmdef.AttrVoltage}})
	d, err := NewElectricalSensor(testOptions("s"), feat(0x009C, ptdef.FeatureNodeTopology), acPower, el,
		WithElements(epmdef.ClusterID, spec.Options{Attributes: []uint32{epmdef.AttrActiveCurrent}}))
	if err != nil {
		t.Fatal(err)
	}
	epm := d.ElectricalPowerMeasurement()
	if !epm.Serves(epmdef.AttrVoltage) || !epm.Serves(epmdef.AttrActiveCurrent) {
		t.Error("the elements opted in are not served")
	}
	if _, err := NewElectricalSensor(testOptions("s"), feat(0x009C, ptdef.FeatureNodeTopology),
		WithElements(epmdef.ClusterID, spec.Options{Features: 2})); !errors.Is(err, ErrConfiguration) {
		t.Errorf("features through WithElements: %v", err)
	}
	if _, err := NewElectricalSensor(testOptions("s"), feat(0x009C, ptdef.FeatureNodeTopology),
		WithServer(&hostServer{id: epmdef.ClusterID}), el); !errors.Is(err, ErrConfiguration) {
		t.Errorf("WithElements with a host server: %v", err)
	}
}
