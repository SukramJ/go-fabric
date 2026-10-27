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

// transitionClusters are the clusters whose servers run their commands
// over time when the declaration asks, with the doc of the Declare field:
// LevelControl and ColorControl as transitions (ADR 0014), WindowCovering
// as a travelling movement.
var transitionClusters = map[string][]string{
	"LevelControl": transitionDoc,
	"ColorControl": transitionDoc,
	"WindowCovering": {
		"Transitions makes a movement travel: the kit's server moves the",
		"axis in six steps, OperationalStatus Opening or Closing until it",
		"arrives — matter.js's CHIP test node (TestWindowCoveringServer.ts,",
		"six 950 ms steps); false moves at once. A Mover moves the device",
		"instead, and excludes it.",
	},
}

// transitionDoc is the Transitions field's doc of LevelControl and
// ColorControl.
var transitionDoc = []string{
	"Transitions runs the commands as transitions over their",
	"TransitionTime (ADR 0014); false applies the target at once.",
}

// portDecl is one host port a declaration carries: an interface declared
// in the cluster's package, in the generated request and state types, and
// the Declare field of that type.
type portDecl struct {
	name    string   // the interface and the Declare field
	doc     []string // the interface's doc (the field's names it)
	methods []string // the interface's method lines
	// field is the Declare field when it is not named as the interface;
	// fieldDoc, when set, replaces the field's standard doc line.
	field    string
	fieldDoc []string
	// optional marks an optional interface of another port, found on it by
	// type assertion: it gets no Declare field.
	optional bool
}

// fieldName is the Declare field that carries the port.
func (p portDecl) fieldName() string {
	if p.field != "" {
		return p.field
	}
	return p.name
}

// portSet is a cluster's host ports, with the types they need beyond the
// generated ones.
type portSet struct {
	ports []portDecl
	types []string
	// imports are the packages the ports name beyond the generated ones
	// (a store's record type).
	imports []string
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
	}, {
		name:  "NativeRamp",
		field: "Ramp",
		fieldDoc: []string{
			"Ramp is a device that ramps its level by itself ([NativeRamp]):",
			"the kit's server runs on cluster/levelcontrol's NativeSource over",
			"it, coupled under the OO feature to the endpoint's OnOff server, so",
			"the host implements no On/Off port; Options and OnLevel start from",
			"Initial. The device ramps by itself, so Ramp is refused beside",
			"LevelSource and beside Transitions.",
		},
		doc: []string{
			"NativeRamp is a device that ramps its level by itself — a dimmer",
			"with a ramp time, a player with a volume. It is all a host",
			"implements to take the hand-off path: the kit builds",
			"cluster/levelcontrol NewNativeSource on it, which keeps the Options",
			"and OnLevel attributes, applies the ExecuteIfOff gate, turns each of",
			"the eight commands into one target level and one duration, and",
			"couples the endpoint's On/Off state for the \"with On/Off\" commands,",
			"as matter.js LevelControlServer does on its own transitions. The",
			"optional interfaces [NativeMover], [NativeSwitcher] and",
			"[NativePersister] are found on it by type assertion. Under the OO",
			"feature the kit couples to the endpoint's OnOff server itself, so",
			"the host implements no On/Off port for it. A device declares a",
			"NativeRamp or a [LevelSource], not both, and a NativeRamp without",
			"Transitions.",
		},
		methods: []string{
			"// Level reports the device's current level; known=false is a level",
			"// the device has not reported, which reads as null. A level below the",
			"// server's MinLevel (a dimmer whose 0 means off) is reported as it is:",
			"// the source counts it as at the minimum for an owed switch-off, so a",
			"// host need not crop it.",
			"Level() (level uint8, known bool)",
			"// Ramp drives the device to level over d; d == 0 is at once. withOnOff",
			"// marks a \"with On/Off\" command: the source has already switched the",
			"// device on before a ramp away from the minimum and switches it off",
			"// once a ramp to the minimum has arrived, so a host needs it only when",
			"// its device couples the two itself.",
			"Ramp(ctx context.Context, level uint8, d time.Duration, withOnOff bool) error",
			"// Stop halts a running ramp where it is.",
			"Stop(ctx context.Context) error",
		},
	}, {
		name:     "NativeMover",
		optional: true,
		doc: []string{
			"NativeMover is an optional interface of a [NativeRamp]: a device that",
			"implements it replaces the source's Move logic, as an implementation",
			"overrides matter.js's \"default implementation of the Move commands\",",
			"moveLogic (LevelControlServer.ts:378-403). Move and MoveWithOnOff call",
			"MoveLevel after the ExecuteIfOff gate (which matter.js applies in move",
			"before moveLogic, :351-361) instead of ramping to the bound; the On/Off",
			"coupling of MoveWithOnOff is then the host's, as an override of",
			"moveLogic skips transition and its couple (:529-550). A device for",
			"which a jump to the bound is no reading of a continuous move — a",
			"speaker's volume — returns nil and keeps its level. Stop and",
			"StopWithOnOff are unaffected. The request is the generated",
			"[MoveRequest], the form a host's [LevelSource] takes too.",
		},
		methods: []string{"MoveLevel(ctx context.Context, req MoveRequest, withOnOff bool) error"},
	}, {
		name:     "NativeSwitcher",
		optional: true,
		doc: []string{
			"NativeSwitcher is an optional interface of a [NativeRamp] whose On/Off",
			"and level are written together — a player that takes \"start at volume",
			"X\" and \"stop\" as one write each. For a \"with On/Off\" command the source",
			"calls SwitchOn(level, d) instead of switching the endpoint on and then",
			"ramping when the command turns the device on, and SwitchOff(d) instead",
			"of ramping to the minimum and then switching off when it ends there.",
			"The source then sets nothing on the OnOff attribute itself: the host's",
			"OnOff reflects what the device did. matter.js has no counterpart — it",
			"couples On/Off and the transition in one transaction against local",
			"state (LevelControlServer.ts:529-550 transition, :597 #turnOnForLevel) —",
			"so the interface is a bridged host's affordance; a device without it",
			"keeps matter.js's order. A [NativeMover] still takes Move and",
			"MoveWithOnOff.",
		},
		methods: []string{
			"SwitchOn(ctx context.Context, level uint8, d time.Duration) error",
			"SwitchOff(ctx context.Context, d time.Duration) error",
		},
	}, {
		name:     "NativePersister",
		optional: true,
		doc: []string{
			"NativePersister is an optional interface of a [NativeRamp] that keeps",
			"the Options and OnLevel attributes across restarts: PersistLevel",
			"receives the values a write would leave in effect, before they take",
			"effect, and an error refuses the write and keeps the previous values",
			"(cluster/levelcontrol NativeConfig.Persist). They start from the",
			"declaration's Initial.",
		},
		methods: []string{"PersistLevel(ctx context.Context, options OptionsBitmap, onLevel *uint8) error"},
	}}, imports: []string{"time"}},
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
			"StateSource). The server reads LockState from it at every read and",
			"stores none: IsJammed true is NotFullyLocked, IsLocked's observed",
			"false (the lock's state is not known) is a null LockState, else",
			"locked is Locked or Unlocked; after LockInvoke the next read asks",
			"IsLocked again. Without one the server keeps its own lock state.",
		},
		methods: []string{
			"IsJammed() bool",
			"IsLocked() (locked, observed bool)",
			"LockInvoke(ctx context.Context, cmdID uint32) error",
		},
	}, {
		name: "AuthStore",
		doc: []string{
			"AuthStore persists the lock's users, credentials and schedules",
			"under the device's StableKey, and its credential key; the node's",
			"store (node.Node.Store) is one. Without one they stay in memory",
			"(cluster/lock AuthStore).",
		},
		methods: []string{
			"GetSetting(ctx context.Context, key string) (value string, ok bool, err error)",
			"SetSetting(ctx context.Context, key, value string) error",
			"LoadDoorLockAuth(ctx context.Context, endpointKey string) (auth store.DoorLockAuth, saved bool, err error)",
			"SaveDoorLockAuth(ctx context.Context, endpointKey string, auth store.DoorLockAuth) error",
		},
	}}, imports: []string{"github.com/SukramJ/go-fabric/store"}},
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
				"Sink applies a SystemMode write, a setpoint change (a write or",
				"a SetpointRaiseLower) with the companions that move with it,",
				"and a TemperatureSetpointHold write, each one the kit's",
				"Thermostat server admitted; an error answers FAILURE and",
				"stores nothing (cluster/thermo Sink).",
			},
			methods: []string{
				"SetSystemMode(ctx context.Context, mode uint8) error",
				"SetSetpoint(ctx context.Context, attrID uint32, value int16, companions []SetpointChange) error",
				"SetTemperatureSetpointHold(ctx context.Context, hold uint8) error",
			},
		}, {
			name: "PresetStore",
			doc: []string{
				"PresetStore persists the presets (their TLV encoding) and the",
				"active handle (nil for null); Load reports ok false when nothing",
				"was saved, and the kit's server starts with the declaration's",
				"Presets (cluster/thermo PresetStore).",
			},
			methods: []string{
				"LoadPresets(ctx context.Context) (presets, activeHandle []byte, ok bool, err error)",
				"SavePresets(ctx context.Context, presets, activeHandle []byte) error",
			},
		}, {
			name: "SchedulePort",
			doc: []string{
				"SchedulePort is the device's week program behind the",
				"MatterScheduleConfiguration feature: SetActiveSchedule switches",
				"to a stored schedule a SetActiveScheduleRequest named, and",
				"CommitSchedules stores the list an atomic write committed; an",
				"error refuses the change with its status, FAILURE without one",
				"(cluster/thermo SchedulePort).",
			},
			methods: []string{
				"SetActiveSchedule(ctx context.Context, handle []byte) error",
				"CommitSchedules(ctx context.Context, schedules []ScheduleStruct) error",
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
	"ThreadNetworkDirectory": {ports: []portDecl{{
		name: "ThreadNetworkStore",
		doc: []string{
			"ThreadNetworkStore persists the directory's operational datasets,",
			"each an opaque blob keyed by its Extended PAN ID, and the preferred",
			"network: Capacity is ThreadNetworkTableSize, LoadNetworks returns",
			"the stored datasets in a stable order, SaveNetwork adds or replaces",
			"one and RemoveNetwork drops one; LoadPreferred returns the saved",
			"PreferredExtendedPanID (ok false when none was saved) and",
			"SavePreferred stores it (nil or empty for null). An error answers",
			"the command or write FAILURE and changes nothing. Without one the",
			"kit's server keeps both in memory (cluster/netinfra",
			"ThreadNetworkStore).",
		},
		methods: []string{
			"Capacity() uint8",
			"LoadNetworks(ctx context.Context) (datasets [][]byte, err error)",
			"SaveNetwork(ctx context.Context, extendedPanID, dataset []byte) error",
			"RemoveNetwork(ctx context.Context, extendedPanID []byte) error",
			"LoadPreferred(ctx context.Context) (extendedPanID []byte, ok bool, err error)",
			"SavePreferred(ctx context.Context, extendedPanID []byte) error",
		},
	}}},
	"ThreadBorderRouterManagement": {ports: []portDecl{{
		name: "BorderRouter",
		doc: []string{
			"BorderRouter is the Thread Border Router the kit's",
			"ThreadBorderRouterManagement and ThreadNetworkDiagnostics servers",
			"manage and read, connectedhomeip's",
			"ThreadBorderRouterManagementDelegate in Go (cluster/threadbr",
			"BorderRouter; thread/otbr adapts an OpenThread Border Router's",
			"REST API to it). Dataset returns the active or the pending",
			"dataset, nil when there is none; SetActiveDataset returns once the",
			"border router is attached to the network or ctx ends, which the",
			"fail-safe's expiry does; CommitActiveDataset and",
			"RevertActiveDataset run at CommissioningComplete and at the",
			"fail-safe's expiry; Diagnostics is the node's diagnostics. Without",
			"one the generated servers serve both clusters.",
		},
		methods: []string{
			"BorderRouterName() string",
			"BorderAgentID(ctx context.Context) ([]byte, error)",
			"ThreadVersion(ctx context.Context) (uint16, error)",
			"InterfaceEnabled(ctx context.Context) (bool, error)",
			"Dataset(ctx context.Context, pending bool) (dataset []byte, err error)",
			"SetActiveDataset(ctx context.Context, dataset []byte) error",
			"CommitActiveDataset(ctx context.Context) error",
			"RevertActiveDataset(ctx context.Context) error",
			"SetPendingDataset(ctx context.Context, dataset []byte) error",
			"Diagnostics(ctx context.Context) (thread.NodeDiagnostics, error)",
		},
	}}, imports: []string{"github.com/SukramJ/go-fabric/thread"}},
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
	if doc, ok := transitionClusters[c.Name]; ok {
		for _, line := range doc {
			w.p("\t// %s", line)
		}
		w.p("\tTransitions bool")
	}
	for _, hc := range cmds {
		w.p("\t// %s handles the %s command in place of the server", hc.method, strings.TrimPrefix(hc.cmdID, "Cmd"))
		w.p("\t// (Server.%s), inline; an optional command it handles is accepted.", hc.method)
		w.p("\t%s func(ctx context.Context, req %s) (%s, error)", hc.method, hc.request, hc.response)
	}
	for _, p := range ps.ports {
		if p.optional {
			continue
		}
		if len(p.fieldDoc) > 0 {
			for _, line := range p.fieldDoc {
				w.p("\t// %s", line)
			}
		} else {
			w.p("\t// %s is the host port the kit's server runs on ([%s]).", p.fieldName(), p.name)
		}
		w.p("\t%s %s", p.fieldName(), p.name)
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
	w.p("\tdecl := spec.ClusterDeclaration{Cluster: ClusterID, Initial: d.Initial.AttributeValues(), Server: d.HostServer, Ports: d}")
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
	if _, ok := transitionClusters[c.Name]; ok {
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
