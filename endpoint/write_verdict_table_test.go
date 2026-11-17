// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/appliance"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	lightpkg "github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/netinfra"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/sensing"
	"github.com/SukramJ/go-fabric/cluster/spec"
	acldef "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	gcdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	ldc "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	ncdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	smokedef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	tdir "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// verdictCase is one writable kit server and a value its attribute does
// not hold when the server is built.
type verdictCase struct {
	// typ is the server's type as the source guard below keys it: its
	// directory under cluster/ and its name.
	typ   string
	root  bool // on the root endpoint, where the server keeps its own version
	build func(t *testing.T) contract.ClusterServer
	attr  uint32
	value any
}

// verdictCases are the hand-written writable kit servers (review II,
// R2-P4-fr-ver-02), and spec.Server, whose verdict every generated server
// and every server that delegates to one takes.
func verdictCases() []verdictCase {
	must := func(t *testing.T, srv contract.ClusterServer, err error) contract.ClusterServer {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}
	return []verdictCase{
		{"core.AccessControl", true, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewAccessControl(&memACL{})
			return must(t, s, err)
		}, acldef.AttrAcl, []mattercore.AccessControlEntryStruct{{Privilege: 5, AuthMode: 2, Subjects: []uint64{0x112233}}}},
		{"core.AccessControl", true, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewAccessControl(&memACL{})
			return must(t, s, err)
		}, acldef.AttrExtension, []mattercore.AccessControlExtensionEntry{{Data: []byte{0x17, 0x18}}}},
		{"core.GroupKeyManagement", true, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewGroupKeyManagement(&memGroupKeys{}, mattercore.GroupKeyMgmtConfig{})
			return must(t, s, err)
		}, gkmdef.AttrGroupKeyMap, []mattercore.GroupKeyMapStruct{{GroupID: 0x0100, GroupKeySetID: 1}}},
		{"core.OTASoftwareUpdateRequestor", true, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewOTASoftwareUpdateRequestor(mattercore.OTARequestorConfig{
				NodeInformation: func() mattercore.OtaNodeInformation { return mattercore.OtaNodeInformation{VendorID: 0xFFF1} },
				DownloadDir:     t.TempDir(),
				Logger:          slog.New(slog.DiscardHandler),
			})
			if err == nil {
				t.Cleanup(s.Stop)
			}
			return must(t, s, err)
		}, otadef.AttrDefaultOtaProviders, spec.List[otadef.ProviderLocation]{{ProviderNodeId: 0xAA}}},
		{"core.BasicInformation", true, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewBasicInformation(mattercore.Config{VendorID: 0x1234, ProductID: 0x5678, NodeLabel: "bridge", VendorName: "go-fabric", ProductName: "bridge"})
			return must(t, s, err)
		}, basicdef.AttrNodeLabel, "lamp"},
		{"core.GeneralCommissioning", true, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewGeneralCommissioning(mattercore.GeneralCommissioningConfig{})
			return must(t, s, err)
		}, gcdef.AttrBreadcrumb, uint64(7)},
		{"core.NetworkCommissioning", true, func(*testing.T) contract.ClusterServer {
			return mattercore.NewNetworkCommissioning(mattercore.NetworkCommissioningConfig{})
		}, ncdef.AttrInterfaceEnabled, false},
		{"core.WiFiNetworkCommissioning", true, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewWiFiNetworkCommissioning(mattercore.WiFiNetworkCommissioningConfig{Driver: switchingWiFi{}})
			return must(t, s, err)
		}, ncdef.AttrInterfaceEnabled, false},
		{"core.Identify", false, func(*testing.T) contract.ClusterServer { return mattercore.NewIdentify() }, identifydef.AttrIdentifyTime, uint16(600)},
		{"core.BridgedDeviceBasicInformation", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewBridgedDeviceBasicInformation(mattercore.BridgedConfig{UniqueID: "u-1", NodeLabel: "lamp"})
			return must(t, s, err)
		}, bdbidef.AttrNodeLabel, "desk"},
		{
			"core.Binding", false, func(*testing.T) contract.ClusterServer { return sourcedBinding() },
			bindingdef.AttrBinding,
			spec.List[bindingdef.TargetStruct]{{Node: ptr(uint64(0x1234)), Endpoint: ptr(uint16(1)), Cluster: ptr(uint32(6))}},
		},
		{"cover.WindowCoveringServer", false, func(*testing.T) contract.ClusterServer {
			return cover.NewWindowCoveringServer(cover.Config{FeatureMap: 0b101})
		}, wcdef.AttrMode, uint8(1)},
		{"light.ColorControlServer", false, func(*testing.T) contract.ClusterServer {
			return lightpkg.NewColorControlServer(lightpkg.ColorControlServerConfig{})
		}, ccdef.AttrOptions, uint8(1)},
		{"levelcontrol.Server", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := levelcontrol.New(levelcontrol.HeldConfig{Features: levelcontrol.FeatureOnOff})
			return must(t, s, err)
		}, levelcontrol.AttrOptions, uint8(1)},
		{"thermo.ThermostatServer", false, func(*testing.T) contract.ClusterServer {
			return thermo.NewThermostatServer(thermo.ThermostatConfig{
				Features: thermo.ThermostatFeatureHEAT, AbsMinHeatSetpointLimit: 700, AbsMaxHeatSetpointLimit: 3000, InitialHeatingSetpoint: 2000,
			})
		}, tdef.AttrOccupiedHeatingSetpoint, int16(2100)},
		{"valve.Server", false, func(*testing.T) contract.ClusterServer {
			return valve.NewServer(valve.Config{Source: &heldValve{}})
		}, valvedef.AttrDefaultOpenDuration, uint32(30)},
		{"fan.Server", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := fan.NewServer(fan.Config{Source: &heldFan{}, Sequence: fan.SequenceOffLowMedHigh, SpeedMax: 3})
			return must(t, s, err)
		}, fan.AttrFanMode, uint8(fan.FanModeHigh)},
		{"sensing.Occupancy", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := sensing.NewOccupancy(sensing.OccupancyConfig{Features: occdef.FeaturePassiveInfrared, Attributes: []uint32{occdef.AttrHoldTime}})
			return must(t, s, err)
		}, occdef.AttrHoldTime, uint16(5)},
		{"appliance.WasherServer", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := appliance.NewLaundryWasherControls(appliance.WasherConfig{
				Features:        uint32(lwc.FeatureRinse),
				SupportedRinses: []appliance.NumberOfRinses{lwc.NumberOfRinsesNormal, lwc.NumberOfRinsesExtra},
				NumberOfRinses:  lwc.NumberOfRinsesNormal,
			})
			return must(t, s, err)
		}, lwc.AttrNumberOfRinses, uint8(lwc.NumberOfRinsesExtra)},
		{"appliance.DryerServer", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := appliance.NewLaundryDryerControls(appliance.DryerConfig{SupportedDrynessLevels: []appliance.DrynessLevel{ldc.DrynessLevelLow, ldc.DrynessLevelExtra}})
			return must(t, s, err)
		}, ldc.AttrSelectedDrynessLevel, uint8(ldc.DrynessLevelExtra)},
		{"netinfra.ThreadDirectoryServer", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			mem := netinfra.NewMemoryThreadNetworkStore(0)
			if err := mem.SaveNetwork(context.Background(), threadPanID, threadDataset()); err != nil {
				t.Fatal(err)
			}
			s, err := netinfra.NewThreadDirectory(context.Background(), netinfra.ThreadDirectoryConfig{Store: mem})
			return must(t, s, err)
		}, tdir.AttrPreferredExtendedPanId, threadPanID},
		{"pump.Server", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			p := func(v int16) *int16 { return &v }
			u := func(v uint16) *uint16 { return &v }
			s, err := pump.NewServer(pump.Config{
				Source:   &heldPump{},
				Features: pump.FeatureConstantSpeed,
				Limits:   pump.Limits{MaxPressure: p(1000), MaxSpeed: u(3000), MinConstSpeed: u(500), MaxConstSpeed: u(2500)},
			})
			return must(t, s, err)
		}, pump.AttrOperationMode, uint8(pump.OperationMaximum)},
		{"alarm.Server", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := alarm.NewServer(alarm.Config{Source: &heldAlarm{}, Features: alarm.FeatureSmokeAlarm, Optional: alarm.OptionalSmokeSensitivityLevel})
			return must(t, s, err)
		}, alarm.AttrSmokeSensitivityLevel, uint8(alarm.SensitivityLow)},
		{"onoff.Server", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Bridged: true})
			return must(t, s, err)
		}, onoff.AttrOnTime, uint16(7)},
		{"spec.Server", false, func(t *testing.T) contract.ClusterServer {
			t.Helper()
			s, err := mattercore.NewUserLabel(mattercore.UserLabelConfig{})
			return must(t, s, err)
		}, userlabeldef.AttrLabelList, spec.List[userlabeldef.LabelStruct]{{Label: "a", Value: "v"}}},
	}
}

// TestKitWriteVerdictParity: every hand-written writable kit server, served
// through the dispatcher — on a bridged endpoint, whose cluster versions
// the endpoint keeps, or on the root, where the server keeps its own —
// answers a write of a value it does not hold as changed, advancing the
// served DataVersion once, and the same write again as unchanged, leaving
// it. matter.js advances a datasource's version only for a commit that
// changed a value (packages/node/src/behavior/state/managed/
// Datasource.ts:1456-1457 at db021da0) and holds an equal value as no
// change (:1476-1481, #noteIfChanged). A server that answered unchanged for
// every write would leave its cluster's version where it is for good and
// report none of its writes; this table is what fails then.
func TestKitWriteVerdictParity(t *testing.T) {
	t.Parallel()
	for _, c := range verdictCases() {
		t.Run(c.typ, func(t *testing.T) {
			t.Parallel()
			srv := c.build(t)
			if got := typeKey(srv); got != c.typ {
				t.Fatalf("the case builds %s, not %s", got, c.typ)
			}
			ep := uint16(2)
			eps := []*Endpoint{{ID: 0}, {ID: 1, Aggregator: true, DeviceType: 0x000E}, {ID: 2, Source: gateSource{srvs: []contract.ClusterServer{srv}}}}
			if c.root {
				ep = 0
				root := &Endpoint{ID: 0}
				root.PublishClusterServers([]contract.ClusterServer{srv})
				eps = []*Endpoint{root}
			}
			d := NewTopologyDispatcher(&Topology{Endpoints: eps, VendorID: 0xFFF1, ProductID: 0x8000})
			d.SetACLLister(fakeACLLister{entries: []store.ACLEntry{caseEntry(1, store.PrivilegeAdminister, nil)}})
			path := concreteAttrPath(ep, srv.MatterClusterID(), c.attr)
			for i, want := range []bool{true, false} {
				before, ok := d.CurrentDataVersion(context.Background(), ep, path.Cluster)
				if !ok {
					t.Fatalf("no DataVersion for 0x%04X on endpoint %d", path.Cluster, ep)
				}
				out := im.HandleWrite(caseCtx(false), d, im.WriteRequest{Writes: []im.AttributeWrite{writeOf(path, c.value)}})
				if len(out.Results) != 1 || out.Results[0].Status != im.StatusSuccess || out.Results[0].Commit == nil {
					t.Fatalf("write %d of %v: %+v", i+1, c.value, out.Results)
				}
				changed := len(out.Results[0].Commit.Changed) > 0
				after, _ := d.CurrentDataVersion(context.Background(), ep, path.Cluster)
				advances := after - before
				if wantAdv := map[bool]uint32{true: 1, false: 0}[want]; advances != wantAdv || changed != want {
					t.Errorf("write %d of %v: DataVersion %d → %d, Commit names %v; want changed %v (%d advance)",
						i+1, c.value, before, after, out.Results[0].Commit.Changed, want, wantAdv)
				}
			}
		})
	}
}

// The two reasons a type that declares MatterWriteChanged needs no verdict
// case. The guard below holds each exempt type's method body to its
// reason.
const (
	// exemptDelegates: the body is `return <recv>.srv.MatterWriteChanged(…)`
	// — the verdict of the generated spec.Server it wraps, which the table
	// holds.
	exemptDelegates = "delegates to its generated spec.Server"
	// exemptRefuses: the body is `return false, <recv>.MatterWrite(…)` —
	// the server stores no write (its MatterWrite refuses every attribute,
	// or accepts only the value it holds; review II fr-ver coverage (e)).
	exemptRefuses = "stores no write"
)

// verdictExempt are the types under cluster/ that declare
// MatterWriteChanged and are in no verdict case, each with its reason.
var verdictExempt = map[string]string{
	"alarmbase.Server":                    exemptDelegates,
	"appliance.MicrowaveServer":           exemptDelegates,
	"boolcfg.Server":                      exemptDelegates,
	"camera.AvSettings":                   exemptDelegates,
	"camera.AvStream":                     exemptDelegates,
	"camera.Chime":                        exemptDelegates,
	"camera.Provider":                     exemptDelegates,
	"camera.PushTransport":                exemptDelegates,
	"camera.Requestor":                    exemptDelegates,
	"camera.Zones":                        exemptDelegates,
	"closure.ControlServer":               exemptRefuses,
	"closure.DimensionServer":             exemptRefuses,
	"core.Descriptor":                     exemptRefuses,
	"core.DiagnosticLogs":                 exemptRefuses,
	"core.EcosystemInformation":           exemptRefuses,
	"core.GeneralDiagnostics":             exemptRefuses,
	"core.Groupcast":                      exemptRefuses,
	"core.Groups":                         exemptRefuses,
	"core.ICDManagement":                  exemptRefuses,
	"core.Messages":                       exemptRefuses,
	"core.ScenesManagement":               exemptRefuses,
	"core.ThreadNetworkCommissioning":     exemptRefuses,
	"core.TimeSynchronization":            exemptRefuses,
	"energy.CommodityPriceServer":         exemptDelegates,
	"energy.CommodityTariffServer":        exemptDelegates,
	"energy.DeviceEnergyManagementServer": exemptDelegates,
	"energy.EvseServer":                   exemptDelegates,
	"energy.GridConditionsServer":         exemptDelegates,
	"energy.PreferenceServer":             exemptDelegates,
	"energy.WaterHeaterManagementServer":  exemptDelegates,
	"filter.Server":                       exemptDelegates,
	"jointfabric.Administrator":           exemptDelegates,
	"jointfabric.Datastore":               exemptDelegates,
	"measurement.AirQualityServer":        exemptRefuses,
	"measurement.BooleanStateServer":      exemptRefuses,
	"measurement.ElectricalEnergyServer":  exemptRefuses,
	"measurement.ElectricalPowerServer":   exemptRefuses,
	"measurement.FlowServer":              exemptRefuses,
	"measurement.HumidityServer":          exemptRefuses,
	"measurement.IlluminanceServer":       exemptRefuses,
	"measurement.OccupancySensingServer":  exemptRefuses,
	"measurement.PowerSourceServer":       exemptRefuses,
	"measurement.PowerTopologyServer":     exemptRefuses,
	"measurement.PressureServer":          exemptRefuses,
	"measurement.SoilMeasurementServer":   exemptRefuses,
	"measurement.TemperatureServer":       exemptRefuses,
	"measurement.concentrationServer":     exemptRefuses,
	"modebase.Server":                     exemptRefuses,
	"modeselect.Server":                   exemptRefuses,
	"netinfra.WiFiManagementServer":       exemptDelegates,
	"opstate.Server":                      exemptRefuses,
	"sensing.AmbientContext":              exemptDelegates,
	"servicearea.Server":                  exemptRefuses,
	"thermo.TemperatureControlServer":     exemptRefuses,
	"threadbr.DiagnosticsServer":          exemptDelegates,
	"threadbr.ManagementServer":           exemptDelegates,
	"tls.CertificateManagement":           exemptDelegates,
	"tls.ClientManagement":                exemptDelegates,
	"wire.AdministratorCommissioning":     exemptRefuses,
	"wire.ScenesManagement":               exemptRefuses,
	"wire.SchedulesServer":                exemptRefuses,
}

// TestEveryWriteVerdictIsInTheTable: every type under cluster/ that
// declares MatterWriteChanged is a verdict case or is exempt with its
// reason, and every exempt type still declares it — so a server whose
// verdict no test checks cannot be added unnoticed. Types are keyed by
// their directory, as cluster/write_changed_guard_test.go keys them.
func TestEveryWriteVerdictIsInTheTable(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "cluster")
	declared := map[string]string{} // "dir.Type" → its body's shape
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != "MatterWriteChanged" {
				continue
			}
			typ := fn.Recv.List[0].Type
			if star, ok := typ.(*ast.StarExpr); ok {
				typ = star.X
			}
			if ident, ok := typ.(*ast.Ident); ok {
				declared[filepath.ToSlash(rel)+"."+ident.Name] = bodyShape(fn.Body)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cased := map[string]bool{}
	for _, c := range verdictCases() {
		cased[c.typ] = true
	}
	for typ, shape := range declared {
		reason, exempt := verdictExempt[typ]
		switch {
		case cased[typ] && exempt:
			t.Errorf("%s is a verdict case and exempt: remove it from verdictExempt", typ)
		case !cased[typ] && !exempt:
			t.Errorf("%s declares MatterWriteChanged and is in no verdict case and not exempt", typ)
		case exempt && shape != reason:
			t.Errorf("%s is exempt because it %s, and its MatterWriteChanged does not read so (%q): give it a verdict case", typ, reason, shape)
		}
	}
	for typ := range verdictExempt {
		if _, ok := declared[typ]; !ok {
			t.Errorf("%s is exempt and declares no MatterWriteChanged: remove the entry", typ)
		}
	}
	for typ := range cased {
		if _, ok := declared[typ]; !ok {
			t.Errorf("%s is a verdict case and declares no MatterWriteChanged", typ)
		}
	}
	if len(declared) < 80 {
		t.Fatalf("found %d types declaring MatterWriteChanged under cluster/, expected the kit's servers", len(declared))
	}
}

func ptr[T any](v T) *T { return &v }

// bodyShape names a MatterWriteChanged body that is one of the two exempt
// shapes, and "its own verdict" for any other.
func bodyShape(body *ast.BlockStmt) string {
	if body == nil || len(body.List) != 1 {
		return "its own verdict"
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok {
		return "its own verdict"
	}
	method := func(e ast.Expr) (recv ast.Expr, name string) {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return nil, ""
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil, ""
		}
		return sel.X, sel.Sel.Name
	}
	switch len(ret.Results) {
	case 1:
		recv, name := method(ret.Results[0])
		if sel, ok := recv.(*ast.SelectorExpr); ok && sel.Sel.Name == "srv" && name == "MatterWriteChanged" {
			return exemptDelegates
		}
	case 2:
		lit, ok := ret.Results[0].(*ast.Ident)
		if _, name := method(ret.Results[1]); ok && lit.Name == "false" && name == "MatterWrite" {
			return exemptRefuses
		}
	}
	return "its own verdict"
}

// typeKey is srv's type as the source guard keys it.
func typeKey(srv any) string {
	rt := reflect.TypeOf(srv)
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	return strings.TrimPrefix(rt.PkgPath(), "github.com/SukramJ/go-fabric/cluster/") + "." + rt.Name()
}

// memACL is a fabric's ACL held in memory.
type memACL struct {
	mu   sync.Mutex
	acls map[uint8][]store.ACLEntry
}

func (m *memACL) ListACL(_ context.Context, f uint8) ([]store.ACLEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]store.ACLEntry(nil), m.acls[f]...), nil
}

func (m *memACL) ReplaceACL(_ context.Context, f uint8, e []store.ACLEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.acls == nil {
		m.acls = map[uint8][]store.ACLEntry{}
	}
	m.acls[f] = append([]store.ACLEntry(nil), e...)
	return nil
}

// memGroupKeys holds a GroupKeyMap in memory; it keeps no key sets.
type memGroupKeys struct {
	mu   sync.Mutex
	maps []store.GroupKeyMapping
}

func (m *memGroupKeys) UpsertGroupKeySet(context.Context, store.GroupKeySet) error { return nil }
func (m *memGroupKeys) GetGroupKeySet(context.Context, uint8, uint16) (store.GroupKeySet, error) {
	return store.GroupKeySet{}, store.ErrGroupKeySetNotFound
}

func (m *memGroupKeys) ListGroupKeySets(context.Context, uint8) ([]store.GroupKeySet, error) {
	return nil, nil
}
func (m *memGroupKeys) RemoveGroupKeySet(context.Context, uint8, uint16) error { return nil }
func (m *memGroupKeys) SetGroupKeyMapping(_ context.Context, g store.GroupKeyMapping) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, h := range m.maps {
		if h.FabricIndex == g.FabricIndex && h.GroupID == g.GroupID {
			m.maps[i] = g
			return nil
		}
	}
	m.maps = append(m.maps, g)
	return nil
}

func (m *memGroupKeys) RemoveGroupKeyMapping(_ context.Context, f uint8, group uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, h := range m.maps {
		if h.FabricIndex == f && h.GroupID == group {
			m.maps = append(m.maps[:i], m.maps[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *memGroupKeys) ReplaceGroupKeyMappings(_ context.Context, f uint8, mappings []store.GroupKeyMapping) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.maps[:0]
	for _, h := range m.maps {
		if h.FabricIndex != f {
			kept = append(kept, h)
		}
	}
	for _, g := range mappings {
		g.FabricIndex = f
		kept = append(kept, g)
	}
	m.maps = kept
	return nil
}

func (m *memGroupKeys) ListGroupKeyMappings(_ context.Context, f uint8) ([]store.GroupKeyMapping, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.GroupKeyMapping
	for _, h := range m.maps {
		if h.FabricIndex == f {
			out = append(out, h)
		}
	}
	return out, nil
}

// switchingWiFi is a Wi-Fi driver that switches its interface.
type switchingWiFi struct{}

func (switchingWiFi) ScanNetworks(context.Context, []byte) (status uint8, debugText string, results []contract.WiFiScanResult) {
	return 0, "", nil
}

func (switchingWiFi) ConnectNetwork(context.Context, []byte, []byte) (status uint8, debugText string, errorValue int32) {
	return 0, "", 0
}
func (switchingWiFi) SetInterfaceEnabled(context.Context, bool) error { return nil }

// heldValve is a valve whose DefaultOpenDuration a write sets.
type heldValve struct {
	mu       sync.Mutex
	duration *uint32
}

func (*heldValve) CurrentState() (valvedef.ValveStateEnum, bool) {
	return valvedef.ValveStateClosed, true
}
func (*heldValve) TargetState() (valvedef.ValveStateEnum, bool) { return 0, false }
func (*heldValve) OpenDuration() (uint32, bool)                 { return 0, false }
func (*heldValve) RemainingDuration() (uint32, bool)            { return 0, false }
func (v *heldValve) DefaultOpenDuration() (uint32, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.duration == nil {
		return 0, false
	}
	return *v.duration, true
}

func (v *heldValve) SetDefaultOpenDuration(_ context.Context, s *uint32) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.duration = s
	return nil
}
func (*heldValve) Open(context.Context, valvedef.OpenRequest) error { return nil }
func (*heldValve) Close(context.Context) error                      { return nil }

// heldFan is a fan whose FanMode a write sets.
type heldFan struct {
	mu sync.Mutex
	st fandef.State
}

func (f *heldFan) FanState() fandef.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st
}

func (f *heldFan) ApplyFanSettings(_ context.Context, p fandef.Patch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.FanMode != nil {
		f.st.FanMode = *p.FanMode
	}
	return nil
}

// heldPump is a pump whose OperationMode a write sets.
type heldPump struct {
	mu sync.Mutex
	st pumpdef.State
}

func (p *heldPump) PumpState() pumpdef.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.st
}

func (p *heldPump) SetOperationMode(_ context.Context, m pumpdef.OperationModeEnum) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.st.OperationMode = m
	return nil
}

// heldAlarm is an alarm whose SmokeSensitivityLevel a write sets; it
// starts at Standard.
type heldAlarm struct {
	mu  sync.Mutex
	set bool
	st  smokedef.State
}

func (a *heldAlarm) SmokeCOState() smokedef.State {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.set {
		a.st.SmokeSensitivityLevel, a.set = alarm.SensitivityStandard, true
	}
	return a.st
}

func (a *heldAlarm) SetSmokeSensitivityLevel(_ context.Context, level alarm.Sensitivity) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.st.SmokeSensitivityLevel, a.set = level, true
	return nil
}

// threadPanID and threadDataset are a stored Thread network, the
// operational dataset connectedhomeip's ThreadNetworkDirectory test
// stores (cluster/netinfra threaddir_chip_test.go dataset).
var threadPanID = []byte{0x39, 0x75, 0x8e, 0xc8, 0x14, 0x4b, 0x07, 0xfb}

func threadDataset() []byte {
	out := []byte{0x0e, 0x08, 0, 0, 0, 0, 0, 0, 0, 1, 0x00, 0x03, 0x00, 0x00, 0x0f, 0x35, 0x04, 0x07, 0xff, 0xf8, 0x00, 0x02, 0x08}
	out = append(out, threadPanID...)
	return append(out,
		0x07, 0x08, 0xfd, 0xf1, 0xf1, 0xad, 0xd0, 0x79, 0x7d, 0xc0,
		0x05, 0x10, 0xf3, 0x66, 0xce, 0xc7, 0xa4, 0x46, 0xba, 0xb9, 0x78, 0xd9, 0x0d, 0x27, 0xab, 0xe3, 0x8f, 0x23,
		0x03, 0x0f, 0x4f, 0x70, 0x65, 0x6e, 0x54, 0x68, 0x72, 0x65, 0x61, 0x64, 0x2d, 0x35, 0x39, 0x33, 0x38,
		0x01, 0x02, 0x59, 0x38,
		0x04, 0x10, 0x3c, 0xa6, 0x7c, 0x96, 0x9e, 0xfb, 0x0d, 0x0c, 0x74, 0xa4, 0xd8, 0xee, 0x92, 0x3b, 0x57, 0x6c,
		0x0c, 0x04, 0x02, 0xa0, 0xf7, 0xf8,
	)
}
