// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The port of matter.js packages/node/test/behaviors/thermostat/preset-helpers.ts
// at 862ad75f and the fabrictest scaffolding the thermostat ports share.
// The ports on fabrictest run one after the other (no t.Parallel): a fake
// site clock moves only while the whole test process is quiet.
package thermo_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// deviceTypeThermostat is the Thermostat device type (thermostat.element.ts).
const deviceTypeThermostat = 0x0301

// presetsAttribute is preset-helpers.ts:18 PRESETS_ATTRIBUTE.
const presetsAttribute = tdef.AttrPresets

// newPreset is preset-helpers.ts:24-33: a preset the device has not seen —
// no handle, scenario Occupied, cooling 26.00 °C, heating 20.00 °C, not
// built in — with the case's changes.
func newPreset(edit ...func(*thermo.Preset)) thermo.Preset {
	cool, heat := int16(2600), int16(2000)
	p := thermo.Preset{
		PresetHandle:    spec.NullOf[[]byte](),
		PresetScenario:  tdef.PresetScenarioOccupied,
		CoolingSetpoint: &cool,
		HeatingSetpoint: &heat,
		BuiltIn:         spec.ValueOf(false),
	}
	for _, e := range edit {
		e(&p)
	}
	return p
}

// withHandle sets a preset's handle.
func withHandle(h []byte) func(*thermo.Preset) {
	return func(p *thermo.Preset) { p.PresetHandle = spec.ValueOf(h) }
}

// withName sets a preset's name.
func withName(n string) func(*thermo.Preset) {
	return func(p *thermo.Preset) { v := spec.ValueOf(n); p.Name = &v }
}

// filled is new Uint8Array(n).fill(b).
func filled(n int, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// thermostatConfig is preset-helpers.ts:35-55 on PresetsThermostatServer
// (:14, Heating, Cooling, AutoMode, Presets): CoolingAndHeating — the Go
// server derives ControlSequenceOfOperation from HEAT+COOL — SystemMode
// Auto (its default with AUTO), the occupied setpoints 20.00 / 26.00 °C,
// MinSetpointDeadBand 2.5 °C, numberOfPresets presets of one type,
// Occupied, without SupportsNames, no active preset and presets. The
// absolute limits are the defaults matter.js falls back on without them
// (ThermostatServer.ts:810-822 #heatDefaults / #coolDefaults).
func thermostatConfig(numberOfPresets uint8, presets []thermo.Preset) thermo.ThermostatConfig {
	deadband := int8(25)
	cfg := thermo.ThermostatConfig{
		Features:                thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeatureAUTO | thermo.ThermostatFeaturePRES,
		AbsMinHeatSetpointLimit: 700, AbsMaxHeatSetpointLimit: 3000,
		AbsMinCoolSetpointLimit: 1600, AbsMaxCoolSetpointLimit: 3200,
		InitialHeatingSetpoint: 2000, InitialCoolingSetpoint: 2600,
		MinSetpointDeadBand: &deadband,
		NumberOfPresets:     numberOfPresets,
		PresetTypes:         []thermo.PresetType{{PresetScenario: tdef.PresetScenarioOccupied, NumberOfPresets: numberOfPresets}},
		Presets:             presets,
	}
	return cfg
}

// newServer builds the server of cfg on the site's clock.
func newServer(t *testing.T, site *fabrictest.Site, cfg thermo.ThermostatConfig) *thermo.ThermostatServer {
	t.Helper()
	cfg.Clock = site.Clock()
	srv, err := thermo.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// thermostatEndpoint is the Thermostat endpoint of preset-helpers.ts:57-63
// (id "thermostat") serving srv.
func thermostatEndpoint(srv *thermo.ThermostatServer) node.Endpoint {
	return fabrictest.Endpoint("thermostat", deviceTypeThermostat, srv)
}

// changeRecorder is preset-helpers.ts:79-87 recordThermostatChanges: the
// attribute ids the server announces as changed — its
// OnMatterAttributesChanged, the input a subscription has for the
// thermostat's own changes.
type changeRecorder struct {
	mu            sync.Mutex
	announcements [][]uint32
}

func recordThermostatChanges(srv *thermo.ThermostatServer) *changeRecorder {
	r := &changeRecorder{}
	srv.OnMatterAttributesChanged(func(ids []uint32) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.announcements = append(r.announcements, slices.Clone(ids))
	})
	return r
}

// count is the number of announcements that name attrID.
func (r *changeRecorder) count(attrID uint32) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, a := range r.announcements {
		if slices.Contains(a, attrID) {
			n++
		}
	}
	return n
}

// parityCtx bounds a case.
func parityCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// thermostatPair is a commissioned pair whose device serves srv on the
// "thermostat" endpoint — MockSite.addCommissionedPair with
// presetsEndpoint (PresetsAtomicWriteTest.ts:15-27).
type thermostatPair struct {
	site *fabrictest.Site
	pair *fabrictest.Pair
	srv  *thermo.ThermostatServer
	ep   uint16
}

func commissionedThermostat(t *testing.T, cfg func(site *fabrictest.Site) thermo.ThermostatConfig) *thermostatPair {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	srv := newServer(t, site, cfg(site))
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{thermostatEndpoint(srv)}}})
	ep, ok := pair.Device.EndpointOf("thermostat")
	if !ok {
		t.Fatal("no thermostat endpoint")
	}
	return &thermostatPair{site: site, pair: pair, srv: srv, ep: ep}
}

// secondPeer commissions a second controller onto the device, on a fabric
// of its own: another peer of the device.
func (p *thermostatPair) secondPeer(t *testing.T) *fabrictest.Pair {
	t.Helper()
	ctx := parityCtx(t)
	if err := p.pair.Device.OpenWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	second := p.site.AddController(fabrictest.ControllerOptions{FabricID: 2, NodeID: 0x2222})
	q, err := second.Commission(ctx, p.pair.Device, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func (p *thermostatPair) commandPath(cmd uint32) im.ConcreteCommandPath {
	return im.ConcreteCommandPath{Endpoint: p.ep, Cluster: tdef.ClusterID, Command: cmd, HasEndpoint: true}
}

func (p *thermostatPair) attrPath(attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: p.ep, Cluster: tdef.ClusterID, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// timeoutMs is a BeginWrite timeout.
func timeoutMs(ms uint16) *uint16 { return &ms }

// atomicRequest invokes AtomicRequest from q.
func (p *thermostatPair) atomicRequest(ctx context.Context, q *fabrictest.Pair, requestType uint8, attrs []uint32, timeout *uint16) (interaction.Result, error) {
	return fabrictest.Invoke(ctx, q, p.commandPath(tdef.CmdAtomicRequest), tdef.AtomicRequestRequest{
		RequestType: requestType, AttributeRequests: attrs, Timeout: timeout,
	})
}

// The RequestTypeEnum values (globals).
const (
	beginWrite    uint8 = 0
	commitWrite   uint8 = 1
	rollbackWrite uint8 = 2
)

// decodeAtomicResponse is AtomicWriteHandlerTest.ts:131-137: the
// AtomicResponse of a command result.
func decodeAtomicResponse(t *testing.T, r interaction.Result) tdef.AtomicResponse {
	t.Helper()
	if !r.HasFields || r.Command != tdef.CmdAtomicResponse {
		t.Fatalf("no AtomicResponse in command result: %+v", r)
	}
	v, err := spec.DecodeStruct[tdef.AtomicResponse](r.Fields)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// wantAtomic compares an AtomicResponse with the expected status, entries
// and timeout (nil for none).
func wantAtomic(t *testing.T, got tdef.AtomicResponse, status tdef.Status, entries []tdef.AtomicResponseAttributeStatusEntry, timeout *uint16) {
	t.Helper()
	if got.StatusCode != status || !slices.Equal(got.AttributeStatus, entries) ||
		(got.Timeout == nil) != (timeout == nil) || (timeout != nil && *got.Timeout != *timeout) {
		t.Errorf("AtomicResponse {status %d, %+v, timeout %v}, want {status %d, %+v, timeout %v}", got.StatusCode, got.AttributeStatus, deref(got.Timeout), status, entries, deref(timeout))
	}
}

// wantStatus checks that err is a command or attribute status error with
// status.
func wantStatus(t *testing.T, err error, status im.StatusCode) {
	t.Helper()
	var se *interaction.StatusError
	if !errors.As(err, &se) || se.Status != status {
		t.Errorf("error %v, want status 0x%02X", err, status)
	}
}

// writePresets writes Presets from q: the controller's whole-list
// assignment, REPLACE-ALL and one append per preset (interaction.Write
// chunks lists by default).
func (p *thermostatPair) writePresets(ctx context.Context, q *fabrictest.Pair, presets []thermo.Preset) (interaction.WriteStatuses, error) {
	ia, err := fabrictest.Connect(ctx, q)
	if err != nil {
		return nil, err
	}
	var statuses interaction.WriteStatuses
	err = p.site.Run(ctx, func(ctx context.Context) error {
		var werr error
		statuses, werr = ia.Write(ctx, []interaction.WriteRequest{{Path: p.attrPath(presetsAttribute), Value: spec.List[thermo.Preset](presets)}}, interaction.WriteOptions{})
		return werr
	})
	return statuses, err
}

// decodePresets decodes a Presets value of a report.
func decodePresets(t *testing.T, n spec.Node) []thermo.Preset {
	t.Helper()
	v, err := tdef.Definition.Attribute(presetsAttribute).Decode(n)
	if err != nil {
		t.Fatal(err)
	}
	return []thermo.Preset(v.(spec.List[thermo.Preset]))
}

// readPresets reads Presets from q.
func (p *thermostatPair) readPresets(t *testing.T, ctx context.Context, q *fabrictest.Pair) []thermo.Preset {
	t.Helper()
	d, err := fabrictest.Read(ctx, q, p.attrPath(presetsAttribute))
	if err != nil {
		t.Fatal(err)
	}
	return decodePresets(t, d.Value)
}

// presetReports is a subscription's Presets reports — the cache of
// matter.js's subscribedPeer (PresetsAtomicWriteTest.ts:22, :48-50,
// :84): every Presets value the controller received, in order.
type presetReports struct {
	mu      sync.Mutex
	reports [][]thermo.Preset
}

func (r *presetReports) last() ([]thermo.Preset, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reports) == 0 {
		return nil, false
	}
	return r.reports[len(r.reports)-1], true
}

// subscribePresets subscribes to Presets from the pair's controller.
func (p *thermostatPair) subscribePresets(t *testing.T, ctx context.Context) *presetReports {
	t.Helper()
	r := &presetReports{}
	floor, ceiling := time.Duration(0), 60*time.Second
	_, err := fabrictest.Subscribe(ctx, p.pair, interaction.SubscribeRequest{
		Attributes:         []im.ConcreteAttributePath{p.attrPath(presetsAttribute)},
		MinIntervalFloor:   &floor,
		MaxIntervalCeiling: &ceiling,
		Updated: func(rep *interaction.Report) {
			for _, a := range rep.Attributes {
				if a.Path.Attribute != presetsAttribute {
					continue
				}
				v, err := tdef.Definition.Attribute(presetsAttribute).Decode(a.Value)
				if err != nil {
					continue
				}
				r.mu.Lock()
				r.reports = append(r.reports, []thermo.Preset(v.(spec.List[thermo.Preset])))
				r.mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// act runs fn on the device inside Device.Act: matter.js endpoint.act / a
// local state write.
func act(t *testing.T, d *fabrictest.Device, fn func(ctx context.Context) error) error {
	t.Helper()
	return d.Act(func(*node.Node) error { return fn(context.Background()) })
}

// wantStatusCode checks that err carries status.
func wantStatusCode(t *testing.T, err error, status im.StatusCode, text string) {
	t.Helper()
	var sc im.StatusCodeError
	if !errors.As(err, &sc) || sc.MatterStatusCode() != status {
		t.Errorf("error %v, want status 0x%02X", err, status)
		return
	}
	if text != "" && !strings.Contains(err.Error(), text) {
		t.Errorf("error %q, want it to contain %q", err, text)
	}
}

// deref is *v, or "none".
func deref(v *uint16) any {
	if v == nil {
		return "none"
	}
	return *v
}
