// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// The host ports of the appliance, closure, energy and pump servers (brief
// BU-b): each is the port its server in cluster/<package> runs on, in the
// generated types, and the server package names it by alias — one form.
// The method lines carry their docs as comments.
var devicePorts = map[string]portSet{
	"PumpConfigurationAndControl": {
		ports: []portDecl{{
			name: "StateSource",
			doc: []string{
				"StateSource is the pump: PumpState reports its state (the",
				"limits come from the declaration's Initial, not from here),",
				"SetOperationMode applies a controller's OperationMode write. A",
				"source that also implements ControlModeSetter,",
				"RunningHoursSetter or EnergyConsumedSetter takes those writes",
				"(cluster/pump).",
			},
			methods: []string{
				"PumpState() State",
				"SetOperationMode(ctx context.Context, mode OperationModeEnum) error",
			},
		}},
		types: []string{
			"// ControlModeSetter is a StateSource that takes a controller's",
			"// ControlMode write; the optional ControlMode attribute needs it.",
			"type ControlModeSetter interface {",
			"\tSetControlMode(ctx context.Context, mode ControlModeEnum) error",
			"}",
			"",
			"// RunningHoursSetter is a StateSource that takes a controller's",
			"// LifetimeRunningHours write (nil for null).",
			"type RunningHoursSetter interface {",
			"\tSetLifetimeRunningHours(ctx context.Context, hours *uint32) error",
			"}",
			"",
			"// EnergyConsumedSetter is a StateSource that takes a controller's",
			"// LifetimeEnergyConsumed write (nil for null).",
			"type EnergyConsumedSetter interface {",
			"\tSetLifetimeEnergyConsumed(ctx context.Context, kWh *uint32) error",
			"}",
		},
	},
	"SmokeCoAlarm": {
		ports: []portDecl{{
			name: "StateSource",
			doc: []string{
				"StateSource is the alarm: SmokeCOState reports its state, read on",
				"every attribute read; the kit's server derives ExpressedState from",
				"it by the priority order — an ExpressedState of Inoperative is the",
				"host's \"the sensor cannot detect\" — and emits the events of each",
				"change when the source, a contract.ChangeNotifier, reports one. A",
				"source that also implements SelfTester takes SelfTestRequest, one",
				"that implements SensitivitySetter the SmokeSensitivityLevel write",
				"(cluster/alarm).",
			},
			methods: []string{"SmokeCOState() State"},
		}},
		types: []string{
			"// SelfTester is a StateSource that runs the self-test: SelfTest starts",
			"// it and returns; the source then reports TestInProgress until it ends.",
			"type SelfTester interface {",
			"\tSelfTest(ctx context.Context) error",
			"}",
			"",
			"// SensitivitySetter is a StateSource that takes a controller's",
			"// SmokeSensitivityLevel write.",
			"type SensitivitySetter interface {",
			"\tSetSmokeSensitivityLevel(ctx context.Context, level SensitivityEnum) error",
			"}",
		},
	},
	"ThermostatUserInterfaceConfiguration": {ports: []portDecl{{
		name: "SettingStore",
		doc: []string{
			"SettingStore persists KeypadLockout under the device's StableKey;",
			"the node's store (node.Node.Store) is one. Without one it stays in",
			"memory (cluster/thermo SettingStore).",
		},
		methods: []string{
			"GetSetting(ctx context.Context, key string) (value string, ok bool, err error)",
			"SetSetting(ctx context.Context, key, value string) error",
		},
	}}},
	"BooleanStateConfiguration": {ports: []portDecl{{
		name: "Delegate",
		doc: []string{
			"Delegate is told what a controller changed — CurrentSensitivityLevel",
			"(quality N, the host persists it), and what EnableDisableAlarm and",
			"SuppressAlarm did; an error answers FAILURE (cluster/boolcfg",
			"Delegate).",
		},
		methods: []string{
			"CurrentSensitivityLevelChanged(ctx context.Context, level uint8) error",
			"AlarmsEnabledChanged(ctx context.Context, enabled AlarmModeBitmap) error",
			"AlarmsActiveChanged(ctx context.Context, active AlarmModeBitmap) error",
			"AlarmsSuppressedChanged(ctx context.Context, suppressed AlarmModeBitmap) error",
		},
	}}},
	"ColorControl": {
		ports: []portDecl{{
			name: "ColorTemperatureWriter",
			doc: []string{
				"ColorTemperatureWriter drives the device's colour temperature",
				"(mireds) whenever the kit's ColorControl server changes it; an",
				"error refuses the change (cluster/light ColorTemperatureWriter).",
			},
			methods: []string{"SetColorTemperatureMireds(ctx context.Context, mireds uint16) error"},
		}, {
			name: "ColorWriter",
			doc: []string{
				"ColorWriter drives the device with the whole colour whenever the",
				"kit's ColorControl server changes it (not on a colour mode",
				"switch); an error refuses the change (cluster/light ColorWriter).",
			},
			methods: []string{"SetColor(ctx context.Context, c Color) error"},
		}},
		types: []string{
			"// Color is a colour in the attributes' Matter units: Mode says which",
			"// fields describe it — X / Y (CurrentX, CurrentY), Hue / Saturation",
			"// (CurrentHue, CurrentSaturation), EnhancedHue / Saturation, or",
			"// ColorTemperatureMireds; the others keep the last value of their mode.",
			"type Color struct {",
			"\tMode                   EnhancedColorModeEnum",
			"\tX, Y                   uint16",
			"\tHue                    uint8",
			"\tEnhancedHue            uint16",
			"\tSaturation             uint8",
			"\tColorTemperatureMireds uint16",
			"}",
		},
	},
	"CommodityTariff": {ports: []portDecl{{
		name: "TimeSource",
		doc: []string{
			"TimeSource is the time the kit's CommodityTariff server reads the",
			"current day and day entry from, in place of the node's clock — a",
			"host that runs the tariff on its own time, as chip's",
			"energy-gateway app runs it on its TestTimeManager. A TimeSource",
			"that is also a contract.ChangeNotifier makes the server derive",
			"the current attributes again whenever it notifies: a clock that",
			"jumped (cluster/energy).",
		},
		methods: []string{"Now() time.Time"},
	}}, imports: []string{"time"}},
	"ValveConfigurationAndControl": {ports: []portDecl{{
		name: "StateSource",
		doc: []string{
			"StateSource is the valve: the readers report its state, known",
			"false for null (quality X), and the command methods drive it",
			"after the kit's checks (cluster/valve).",
		},
		methods: []string{
			"// CurrentState reports the valve's observed state.",
			"CurrentState() (state ValveStateEnum, known bool)",
			"// TargetState reports the state the valve is moving towards.",
			"TargetState() (state ValveStateEnum, known bool)",
			"// OpenDuration reports the duration of the current opening, in seconds.",
			"OpenDuration() (seconds uint32, known bool)",
			"// RemainingDuration reports the time left before the valve closes.",
			"RemainingDuration() (seconds uint32, known bool)",
			"// DefaultOpenDuration reports the configured default open duration.",
			"DefaultOpenDuration() (seconds uint32, known bool)",
			"// SetDefaultOpenDuration persists a new default; nil for a null",
			"// write, which clears it.",
			"SetDefaultOpenDuration(ctx context.Context, seconds *uint32) error",
			"// Open drives the valve open: an absent OpenDuration means",
			"// DefaultOpenDuration, a null one stays open until closed.",
			"Open(ctx context.Context, req OpenRequest) error",
			"// Close drives the valve closed.",
			"Close(ctx context.Context) error",
		},
	}}},
	"ModeSelect": {ports: []portDecl{{
		name: "ModeSource",
		doc: []string{
			"ModeSource is the device's mode selection: the description,",
			"namespace and modes it offers, the mode it is in, and",
			"ChangeToMode for a mode the kit's server checked against",
			"SupportedModes (cluster/modeselect).",
		},
		methods: []string{
			"// ModeDescription is what this instance selects between.",
			"ModeDescription() string",
			"// ModeNamespace is the StandardNamespace; present false is null.",
			"ModeNamespace() (namespace uint8, present bool)",
			"// SupportedModes are the selectable modes, each Mode distinct.",
			"SupportedModes() []ModeOptionStruct",
			"// CurrentMode is the mode the device is in.",
			"CurrentMode() uint8",
			"// ChangeToMode applies a checked mode.",
			"ChangeToMode(ctx context.Context, newMode uint8) error",
		},
	}}},
	"ServiceArea": {ports: []portDecl{{
		name: "Selector",
		doc: []string{
			"Selector decides a SelectAreas request whose areas are all",
			"supported (areas without duplicates): a non-Success status is",
			"answered as is and changes nothing, an error answers FAILURE",
			"(cluster/servicearea).",
		},
		methods: []string{"SelectAreas(ctx context.Context, areas []uint32) (SelectAreasStatus, string, error)"},
	}, {
		name: "Skipper",
		doc: []string{
			"Skipper carries out SkipArea for a selected area and updates",
			"Progress itself; without one SkipArea is not accepted",
			"(cluster/servicearea).",
		},
		methods: []string{"SkipArea(ctx context.Context, area uint32) (SkipAreaStatus, string, error)"},
	}}},
	"ClosureControl": {ports: []portDecl{{
		name: "Mover",
		doc: []string{
			"Mover drives the closure after the kit's checks: MoveTo to a",
			"target position, Stop halts motion; an error answers FAILURE and",
			"leaves the target state (cluster/closure).",
		},
		methods: []string{
			"MoveTo(ctx context.Context, position TargetPositionEnum) error",
			"Stop(ctx context.Context) error",
		},
	}}},
	"ClosureDimension": {ports: []portDecl{{
		name: "DimensionHandler",
		doc: []string{
			"DimensionHandler carries out SetTarget and Step after the kit's",
			"checks and before TargetState changes; an error answers FAILURE",
			"(cluster/closure).",
		},
		methods: []string{
			"// SetTarget takes Position after the LimitRange clamp and the",
			"// Resolution rounding, Latch and Speed as sent.",
			"SetTarget(ctx context.Context, position *uint16, latch *bool, speed *ThreeLevelAutoEnum) error",
			"// Step takes the request's fields as sent.",
			"Step(ctx context.Context, direction StepDirectionEnum, numberOfSteps uint16, speed *ThreeLevelAutoEnum) error",
		},
	}}},
	"TemperatureControl": {ports: []portDecl{{
		name: "TemperatureSetter",
		doc: []string{
			"TemperatureSetter applies a SetTemperature the kit's server",
			"checked: target (TN, 0.01 °C) or level (TL, an index into",
			"SupportedTemperatureLevels). Nil error stores it; an error",
			"answers FAILURE, or INVALID_IN_STATE when it wraps",
			"thermo.ErrTemperatureRefused (cluster/thermo).",
		},
		methods: []string{"SetTemperature(ctx context.Context, target *int16, level *uint8) error"},
	}}},
	"MicrowaveOvenControl": {ports: []portDecl{{
		name: "Oven",
		doc: []string{
			"Oven is the oven behind the kit's MicrowaveOvenControl server:",
			"the first four answer from the endpoint's OperationalState and",
			"MicrowaveOvenMode, the last two carry out a command; an error",
			"wrapping an IM status answers it, any other FAILURE",
			"(cluster/appliance).",
		},
		methods: []string{
			"// OperationalState is the endpoint's current OperationalState.",
			"OperationalState() uint8",
			"// NormalMode is the MicrowaveOvenMode mode tagged Normal.",
			"NormalMode() (uint8, bool)",
			"// SupportedMode reports whether mode is a MicrowaveOvenMode mode.",
			"SupportedMode(mode uint8) bool",
			"// StartSupported reports whether OperationalState accepts Start.",
			"StartSupported() bool",
			"// SetCookingParameters applies checked parameters: exactly one",
			"// of power (PWRNUM) and wattIndex (WATTS) is set.",
			"SetCookingParameters(ctx context.Context, mode uint8, cookTime uint32, start bool, power, wattIndex *uint8) error",
			"// ModifyCookTime applies AddMoreTime's new CookTime.",
			"ModifyCookTime(ctx context.Context, cookTime uint32) error",
		},
	}}},
	"WaterHeaterManagement": {ports: []portDecl{{
		name: "Booster",
		doc: []string{
			"Booster starts and cancels a boost the kit's server admitted;",
			"the device ends it through the server (cluster/energy).",
		},
		methods: []string{
			"Boost(ctx context.Context, info WaterHeaterBoostInfoStruct) error",
			"CancelBoost(ctx context.Context) error",
		},
	}}},
	"DeviceEnergyManagement": {ports: []portDecl{{
		name: "DemManager",
		doc: []string{
			"DemManager is the device's energy management behind the kit's",
			"server: the hooks run after the server's checks; an error",
			"answers FAILURE and changes nothing (cluster/energy).",
		},
		methods: []string{
			"// ApproxEnergyDuringSession is PowerAdjustEnd's EnergyUse, in mWh.",
			"ApproxEnergyDuringSession() int64",
			"PowerAdjust(ctx context.Context, powerMw int64, durationS uint32, cause AdjustmentCauseEnum) error",
			"PowerAdjustCompleted() error",
			"CancelPowerAdjust(cause CauseEnum) error",
			"StartTimeAdjust(ctx context.Context, requestedStartTime uint32, cause AdjustmentCauseEnum) error",
			"Pause(ctx context.Context, durationS uint32, cause AdjustmentCauseEnum) error",
			"PauseCompleted() error",
			"CancelPause(cause CauseEnum) error",
			"CancelRequest(ctx context.Context) error",
			"ModifyForecast(ctx context.Context, forecastID uint32, adjustments []SlotAdjustmentStruct, cause AdjustmentCauseEnum) error",
			"RequestConstraintBasedForecast(ctx context.Context, constraints []ConstraintsStruct, cause AdjustmentCauseEnum) error",
		},
	}}},
}

// operationalCommands is the port of the three OperationalState
// derivations: the one engine in cluster/opstate serves them, each with its
// own generated ErrorStateStruct.
var operationalCommands = portSet{ports: []portDecl{{
	name: "CommandHandler",
	doc: []string{
		"CommandHandler performs a command the kit's server accepted and",
		"checked against the state (cluster/opstate): it moves",
		"OperationalState through the device's typed Set and returns the",
		"ErrorStateStruct the response reports, the zero value for",
		"NoError; an error answers FAILURE.",
	},
	methods: []string{"HandleOperationalCommand(ctx context.Context, cmdID uint32) (ErrorStateStruct, error)"},
}}}

// alarmDelegate is the port of the three AlarmBase derivations: the one
// engine in cluster/alarmbase asks it before a command changes Mask or
// State.
var alarmDelegate = portSet{ports: []portDecl{{
	name: "Delegate",
	doc: []string{
		"Delegate is asked before a command changes Mask (ModifyEnabledAlarms)",
		"or State (Reset); an error refuses the command and answers FAILURE.",
		"Without one every command is accepted (cluster/alarmbase).",
	},
	methods: []string{
		"ModifyEnabledAlarms(ctx context.Context, mask uint32) error",
		"ResetAlarms(ctx context.Context, alarms uint32) error",
	},
}}}

// resourceMonitoring is the port of the two ResourceMonitoring
// derivations (cluster/filter).
var resourceMonitoring = portSet{ports: []portDecl{{
	name: "Resetter",
	doc: []string{
		"Resetter resets the device's resource tracking for ResetCondition; on",
		"success the kit's server resets Condition and ChangeIndication. The",
		"ResetCondition command needs it (cluster/filter).",
	},
	methods: []string{"ResetCondition(ctx context.Context) error"},
}, {
	name: "LastChangedTimeWriter",
	doc: []string{
		"LastChangedTimeWriter is told a controller's LastChangedTime write",
		"(nil for null), so the host can persist it; an error refuses the",
		"write (cluster/filter).",
	},
	methods: []string{"WriteLastChangedTime(ctx context.Context, t *uint32) error"},
}}}

func init() {
	devicePorts["DishwasherAlarm"] = alarmDelegate
	devicePorts["RefrigeratorAlarm"] = alarmDelegate
	devicePorts["TemperatureAlarm"] = alarmDelegate
	devicePorts["HepaFilterMonitoring"] = resourceMonitoring
	devicePorts["ActivatedCarbonFilterMonitoring"] = resourceMonitoring
	devicePorts["OperationalState"] = operationalCommands
	devicePorts["RvcOperationalState"] = operationalCommands
	devicePorts["OvenCavityOperationalState"] = operationalCommands
	for name, ps := range devicePorts {
		ports[name] = ps
	}
}
