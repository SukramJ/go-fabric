// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import "strings"

// The declaration (concept v6 A.3 step 2, brief BU): server_gen.go ends
// with Declare, the one form in which a host states a cluster of a device —
// its initial state, its feature selection, the controller-write veto, the
// typed command handlers, and the host ports the kit's own server for the
// cluster runs on. device.New<Type> and device.FromSpec take it as a
// device.Declaration; the kit reads it through ClusterDeclaration
// (cluster/spec/declaration.go).
//
// The elements a declaration serves follow matter.js's rule: an optional
// attribute with a value is supported, an optional command with an
// implementation is. Mirrors matter.js
// packages/node/src/behavior/cluster/ValidatedElements.ts:251-252 and
// :270-278 (attributes), :301-318 (commands).

// transitionClusters are the clusters whose servers run their commands as
// transitions over time when the declaration asks (ADR 0014).
var transitionClusters = map[string]bool{"LevelControl": true, "ColorControl": true}

// portDecl is one host port a declaration carries: an interface declared
// in the cluster's package, in the generated request and state types, and
// the Declare field of that type.
type portDecl struct {
	name    string   // the interface and the Declare field
	doc     []string // the interface's doc (the field's names it)
	methods []string // the interface's method lines
}

// portSet is a cluster's host ports, with the types they need beyond the
// generated ones.
type portSet struct {
	ports []portDecl
	types []string
}

// modeChanger is the port of every ModeBase derivation that accepts
// ChangeToMode.
var modeChanger = portSet{ports: []portDecl{{
	name: "ModeChanger",
	doc: []string{
		"ModeChanger moves the device to a supported mode other than the",
		"current one: status 0 (Success) makes newMode the CurrentMode, any",
		"other ModeChangeStatus is the response's with text for the user; an",
		"error answers FAILURE. The kit's ModeBase server runs on it",
		"(cluster/modebase).",
	},
	methods: []string{"ChangeToMode(ctx context.Context, newMode uint8) (status uint8, text string, err error)"},
}}}

// ports are the host ports of the clusters whose kit server needs one to
// run (the ones brief BX1 listed as "needs a host port"); a declaration
// that carries the port makes that server the device's default. Hand-
// written: what a port is, is the server's decision, not the schema's.
var ports = map[string]portSet{
	"OnOff": {ports: []portDecl{{
		name: "Sink",
		doc: []string{
			"Sink switches the device: the kit's OnOff server calls it before it",
			"stores a change of OnOff a command makes, and an error leaves the",
			"state and answers FAILURE (cluster/onoff Sink).",
		},
		methods: []string{"SetOnOff(ctx context.Context, on bool) error"},
	}}},
	"LevelControl": {ports: []portDecl{{
		name: "LevelSource",
		doc: []string{
			"LevelSource is the device's level: the kit's LevelControl server",
			"reads it and hands it the eight commands, after its own checks",
			"(cluster/levelcontrol). A device that holds no level of its own",
			"needs none: the kit's server holds it.",
		},
		methods: []string{
			"CurrentLevel() (level uint8, known bool)",
			"Options() OptionsBitmap",
			"OnLevel() (level uint8, known bool)",
			"SetOptions(ctx context.Context, options OptionsBitmap) error",
			"SetOnLevel(ctx context.Context, level *uint8) error",
			"MoveToLevel(ctx context.Context, req MoveToLevelRequest) error",
			"MoveToLevelWithOnOff(ctx context.Context, req MoveToLevelWithOnOffRequest) error",
			"Move(ctx context.Context, req MoveRequest) error",
			"MoveWithOnOff(ctx context.Context, req MoveWithOnOffRequest) error",
			"Step(ctx context.Context, req StepRequest) error",
			"StepWithOnOff(ctx context.Context, req StepWithOnOffRequest) error",
			"Stop(ctx context.Context, req StopRequest) error",
			"StopWithOnOff(ctx context.Context, req StopWithOnOffRequest) error",
		},
	}}},
	"FanControl": {ports: []portDecl{{
		name: "StateSource",
		doc: []string{
			"StateSource is the fan: FanState reports its state (FanMode,",
			"PercentSetting, PercentCurrent, SpeedSetting, SpeedCurrent,",
			"RockSetting, WindSetting, AirflowDirection), and ApplyFanSettings",
			"applies what a controller's write or a Step resolved to — the",
			"fields the coupling rules determine, the others nil",
			"(cluster/fan).",
		},
		methods: []string{
			"FanState() State",
			"ApplyFanSettings(ctx context.Context, p Patch) error",
		},
	}, {
		name: "Stepper",
		doc: []string{
			"Stepper takes the Step command over; without one the kit's",
			"FanControl server steps by its own rule (cluster/fan Stepper).",
		},
		methods: []string{"Step(ctx context.Context, req StepRequest) error"},
	}}},
	"DoorLock": {ports: []portDecl{{
		name: "StateSource",
		doc: []string{
			"StateSource is the lock's actuator: it reports whether the bolt is",
			"jammed and locked, and LockInvoke runs a LockDoor or UnlockDoor",
			"(cmdID) the kit's DoorLock server admitted (cluster/lock",
			"StateSource). Without one the server keeps its own lock state.",
		},
		methods: []string{
			"IsJammed() bool",
			"IsLocked() (locked, observed bool)",
			"LockInvoke(ctx context.Context, cmdID uint32) error",
		},
	}}},
	"WindowCovering": {ports: []portDecl{{
		name: "Mover",
		doc: []string{
			"Mover moves the covering: GoTo to the targets in percent100ths",
			"(nil leaves an axis), Stop stops every movement. It returns once",
			"the device accepted the target; the position comes back through",
			"the server's Set (cluster/cover Mover). Without one a command",
			"moves the covering at once.",
		},
		methods: []string{
			"GoTo(ctx context.Context, lift, tilt *uint16) error",
			"Stop(ctx context.Context) error",
		},
	}}},
	"Thermostat": {
		ports: []portDecl{{
			name: "Sink",
			doc: []string{
				"Sink applies a SystemMode write and a setpoint change (a write",
				"or a SetpointRaiseLower) the kit's Thermostat server admitted,",
				"with the companions that move with it; an error answers FAILURE",
				"and stores nothing (cluster/thermo Sink).",
			},
			methods: []string{
				"SetSystemMode(ctx context.Context, mode uint8) error",
				"SetSetpoint(ctx context.Context, attrID uint32, value int16, companions []SetpointChange) error",
			},
		}},
		types: []string{
			"// SetpointChange is one setpoint a Sink's setpoint change moves with",
			"// it: the attribute and its new value.",
			"type SetpointChange struct {",
			"\tAttrID uint32",
			"\tValue  int16",
			"}",
		},
	},
	"LaundryWasherMode": modeChanger,
	"DishwasherMode":    modeChanger,
	"RvcRunMode":        modeChanger,
	"RvcCleanMode":      modeChanger,
	"OvenMode":          modeChanger,
	"RefrigeratorAndTemperatureControlledCabinetMode": modeChanger,
	"EnergyEvseMode":             modeChanger,
	"WaterHeaterMode":            modeChanger,
	"DeviceEnergyManagementMode": modeChanger,
}

// declaration renders the Declare section of server_gen.go.
//
//nolint:funlen // a template: the fields and methods of the generated type in order
func (g *generator) declaration(w *writer, cmds []hullCommand) {
	c := g.c
	ps := ports[c.Name]
	for _, line := range ps.types {
		w.p("%s", line)
	}
	if len(ps.types) > 0 {
		w.p("")
	}
	for _, p := range ps.ports {
		for _, line := range p.doc {
			w.p("// %s", line)
		}
		w.p("type %s interface {", p.name)
		for _, m := range p.methods {
			w.p("\t%s", m)
		}
		w.p("}")
		w.p("")
	}
	w.p("// Declare is the declaration of the %s cluster on a device:", c.Name)
	w.p("// device.New<Type> and device.FromSpec take it, and every field is")
	w.p("// optional — the zero value mounts the cluster (an optional one too)")
	w.p("// with the kit's default server and the device type's defaults. A")
	w.p("// device takes one Declare per cluster.")
	w.p("type Declare struct {")
	w.p("\t// Initial is the state the server starts with, over the device")
	w.p("\t// type's defaults; an optional attribute it sets is served.")
	w.p("\tInitial Patch")
	if len(c.Features) > 0 {
		w.p("\t// Features is the FeatureMap selection; zero is the device type's.")
		w.p("\tFeatures Feature")
	}
	if len(cmds) > 0 {
		w.p("\t// Commands are optional commands the server accepts beyond the")
		w.p("\t// ones the selection mandates: the kit's server carries them out")
		w.p("\t// (through its port), a Handle field below handles one in its place.")
		w.p("\tCommands []uint32")
	}
	if len(g.hullEvents()) > 0 {
		w.p("\t// Events are optional events the server emits beyond the ones the")
		w.p("\t// selection mandates (Server.Emit<Event>).")
		w.p("\tEvents []uint32")
	}
	w.p("\t// OnWrite vetoes a controller's write before it is stored")
	w.p("\t// (Server.OnWrite); it runs inline.")
	w.p("\tOnWrite func(ctx context.Context, next State, attrID uint32) error")
	if transitionClusters[c.Name] {
		w.p("\t// Transitions runs the commands as transitions over their")
		w.p("\t// TransitionTime (ADR 0014); false applies the target at once.")
		w.p("\tTransitions bool")
	}
	for _, hc := range cmds {
		w.p("\t// %s handles the %s command in place of the server", hc.method, strings.TrimPrefix(hc.cmdID, "Cmd"))
		w.p("\t// (Server.%s), inline; an optional command it handles is accepted.", hc.method)
		w.p("\t%s func(ctx context.Context, req %s) (%s, error)", hc.method, hc.request, hc.response)
	}
	for _, p := range ps.ports {
		w.p("\t// %s is the host port the kit's server runs on ([%s]).", p.name, p.name)
		w.p("\t%s %s", p.name, p.name)
	}
	w.p("\t// HostServer is the host's own server for the cluster, mounted in")
	w.p("\t// place of the kit's — also for a cluster the device type does not")
	w.p("\t// list. The hooks above go on it; Initial, Features and the ports are")
	w.p("\t// the kit's server's and are refused beside it.")
	w.p("\tHostServer contract.ClusterServer")
	w.p("}")
	w.p("")
	w.p("// ClusterID implements device.Declaration: the cluster declared.")
	w.p("func (Declare) ClusterID() uint32 { return ClusterID }")
	w.p("")
	w.p("// ClusterDeclaration implements device.Declaration: the declaration in")
	w.p("// the form the device kit applies it.")
	w.p("func (d Declare) ClusterDeclaration() spec.ClusterDeclaration {")
	w.p("\tdecl := spec.ClusterDeclaration{Cluster: ClusterID, Initial: spec.PatchValues(d.Initial), Server: d.HostServer, Ports: d}")
	w.p("\tif d.OnWrite != nil {")
	w.p("\t\tdecl.Attach = d.attach")
	w.p("\t}")
	if len(c.Features) > 0 {
		w.p("\tdecl.Features, decl.HasFeatures = uint32(d.Features), d.Features != 0")
	}
	if len(cmds) > 0 {
		w.p("\tdecl.Elements.Commands = append(decl.Elements.Commands, d.Commands...)")
	}
	if len(g.hullEvents()) > 0 {
		w.p("\tdecl.Elements.Events = append(decl.Elements.Events, d.Events...)")
	}
	if transitionClusters[c.Name] {
		w.p("\tdecl.Transitions = d.Transitions")
	}
	for _, hc := range cmds {
		w.p("\tif d.%s != nil {", hc.method)
		w.p("\t\tdecl.Elements.Commands = append(decl.Elements.Commands, %s)", hc.cmdID)
		w.p("\t\tdecl.Attach = d.attach")
		w.p("\t}")
	}
	w.p("\treturn decl")
	w.p("}")
	w.p("")
	w.p("// attach installs the declaration's veto and handlers on srv; the")
	w.p("// error names each one the server refused.")
	w.p("func (d Declare) attach(srv contract.ClusterServer) error {")
	w.p("\ts := Wrap(srv)")
	w.p("\tvar errs []error")
	w.p("\tif d.OnWrite != nil {")
	w.p("\t\terrs = append(errs, spec.HookError(Name, \"OnWrite\", s.OnWrite(d.OnWrite)))")
	w.p("\t}")
	for _, hc := range cmds {
		w.p("\tif d.%s != nil {", hc.method)
		w.p("\t\terrs = append(errs, spec.HookError(Name, %q, s.%s(d.%s)))", hc.method, hc.method, hc.method)
		w.p("\t}")
	}
	w.p("\treturn errors.Join(errs...)")
	w.p("}")
}
