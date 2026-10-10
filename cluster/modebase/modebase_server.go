// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package modebase contains one server for the Matter ModeBase cluster
// family, instantiated for the derivations the appliance device types
// use: LaundryWasherMode (0x0051, LaundryWasher and LaundryDryer),
// RvcRunMode (0x0054, mandatory on RoboticVacuumCleaner), RvcCleanMode
// (0x0055), DishwasherMode (0x0059), OvenMode, MicrowaveOvenMode,
// RefrigeratorAndTemperatureControlledCabinetMode, EnergyEvseMode,
// WaterHeaterMode and DeviceEnergyManagementMode (ids from their
// generated definitions).
//
// ModeSelect (0x0050) is not a ModeBase cluster and stays in
// cluster/modeselect.
//
// The server holds CurrentMode, as matter.js's mode servers do
// (packages/node/src/behaviors/<derivation>-mode/*ModeServer.ts), and
// mirrors what they enforce:
//
//   - SupportedModes: no label and no mode value twice
//     (ModeUtils.assertSupportedModes), plus the derivation's required
//     tags — RvcRunMode at least one Idle and one Cleaning mode, Idle,
//     Cleaning and Mapping never together in one mode; RvcCleanMode at
//     least one Vacuum or Mop mode; LaundryWasherMode and DishwasherMode
//     at least one Normal mode; OvenMode at least one Bake mode;
//     RefrigeratorAndTemperatureControlledCabinetMode at least one Auto
//     mode; MicrowaveOvenMode exactly one Normal mode and Normal never
//     with Defrost; EnergyEvseMode a Manual mode without TimeOfUse and
//     SolarCharging; WaterHeaterMode a Manual and an Off mode, and Off,
//     Manual and Timed only in single-tag modes; DeviceEnergyManagementMode
//     a NoOptimization, a LocalOptimization and a GridOptimization mode,
//     and NoOptimization never with an optimization tag;
//   - CurrentMode is always a supported mode (ModeUtils.assertMode);
//   - ChangeToMode answers UnsupportedMode for a mode not in
//     SupportedModes and Success for the current one
//     (ModeUtils.assertModeChange), and otherwise asks the host's
//     [ModeChanger] — matter.js's default sets CurrentMode at once; a
//     device decides here, and may refuse with GenericFailure,
//     InvalidInMode or a derivation's own status (Stuck, BatteryLow,
//     CleaningInProgress, …). On Success CurrentMode becomes NewMode.
//
// StartUpMode and OnMode — and with OnMode the DEPONOFF feature — are
// disallowed ("X") by every derivation served here in Matter 1.6.1, so
// they are neither served nor configurable. MicrowaveOvenMode's
// definition also disallows ChangeToMode and its response: that server
// accepts no command and needs no [ModeChanger].
//
// The schema constraints matter.js validates the state against
// (SupportedModes "2 to 255", Label "max 64", ModeTags "1 to 8") hold at
// construction, as does the ModeBase specification text matter.js carries
// in mode-base.resource.ts but does not check: the tags of one mode are
// distinct, no two modes have the same tag set, and every mode has a
// standard (MfgCode-less) tag.
//
// Each derivation is built on its generated definition
// (cluster/spec/laundrywashermode, rvcrunmode, rvccleanmode,
// dishwashermode, ovenmode, refrigeratorandtemperaturecontrolledcabinetmode,
// microwaveovenmode, energyevsemode, waterheatermode,
// deviceenergymanagementmode; ADR 0013): the ids, the tag and status values, the
// attribute, command and event lists, FeatureMap, ClusterRevision, the
// feature check and the write answers come from it. The rules above are
// what this package adds, as matter.js's mode servers add them.
package modebase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagementmode"
	"github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	"github.com/SukramJ/go-fabric/cluster/spec/energyevsemode"
	"github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	"github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	"github.com/SukramJ/go-fabric/cluster/spec/ovenmode"
	rtcc "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	"github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Cluster ids (the generated definitions').
const (
	ClusterIDLaundryWasherMode = laundrywashermode.ClusterID
	ClusterIDRvcRunMode        = rvcrunmode.ClusterID
	ClusterIDRvcCleanMode      = rvccleanmode.ClusterID
	ClusterIDDishwasherMode    = dishwashermode.ClusterID

	ClusterIDOvenMode                                        = ovenmode.ClusterID
	ClusterIDRefrigeratorAndTemperatureControlledCabinetMode = rtcc.ClusterID
	ClusterIDMicrowaveOvenMode                               = microwaveovenmode.ClusterID
	ClusterIDEnergyEvseMode                                  = energyevsemode.ClusterID
	ClusterIDWaterHeaterMode                                 = waterheatermode.ClusterID
	ClusterIDDeviceEnergyManagementMode                      = deviceenergymanagementmode.ClusterID
)

// Attribute ids, ModeBase's and so every derivation's. StartUpMode
// (0x0002) and OnMode (0x0003) are "X" in every derivation served here.
const (
	AttrSupportedModes = rvcrunmode.AttrSupportedModes // M, list[ModeOptionStruct] 2 to 255, F
	AttrCurrentMode    = rvcrunmode.AttrCurrentMode    // M, uint8, N
)

// Command ids.
const (
	CmdChangeToMode         = rvcrunmode.CmdChangeToMode
	CmdChangeToModeResponse = rvcrunmode.CmdChangeToModeResponse
)

// Feature is a FeatureMap bit.
type Feature uint32

// FeatureDirectModeChange is RvcRunMode / RvcCleanMode DIRECTMODECH,
// constraint "20" (rvc-run-mode.element.ts:23,
// rvc-clean-mode.element.ts:23): the device changes modes while the RVC
// Run Mode is not Idle. The other derivations define no feature; DEPONOFF
// is "X" everywhere.
const FeatureDirectModeChange = Feature(rvcrunmode.FeatureDirectModeChange)

// Status is a ModeChangeStatus value.
type Status uint8

// ModeChangeStatus values: ModeBase's and the derivations' own, from the
// generated ModeChangeStatus enums.
const (
	StatusSuccess               = Status(rvcrunmode.ModeChangeStatusSuccess)
	StatusUnsupportedMode       = Status(rvcrunmode.ModeChangeStatusUnsupportedMode)
	StatusGenericFailure        = Status(rvcrunmode.ModeChangeStatusGenericFailure)
	StatusInvalidInMode         = Status(rvcrunmode.ModeChangeStatusInvalidInMode)
	StatusCleaningInProgress    = Status(rvccleanmode.ModeChangeStatusCleaningInProgress)  // RvcCleanMode
	StatusStuck                 = Status(rvcrunmode.ModeChangeStatusStuck)                 // RvcRunMode
	StatusDustBinMissing        = Status(rvcrunmode.ModeChangeStatusDustBinMissing)        // RvcRunMode
	StatusDustBinFull           = Status(rvcrunmode.ModeChangeStatusDustBinFull)           // RvcRunMode
	StatusWaterTankEmpty        = Status(rvcrunmode.ModeChangeStatusWaterTankEmpty)        // RvcRunMode
	StatusWaterTankMissing      = Status(rvcrunmode.ModeChangeStatusWaterTankMissing)      // RvcRunMode
	StatusWaterTankLidOpen      = Status(rvcrunmode.ModeChangeStatusWaterTankLidOpen)      // RvcRunMode
	StatusMopCleaningPadMissing = Status(rvcrunmode.ModeChangeStatusMopCleaningPadMissing) // RvcRunMode
	StatusBatteryLow            = Status(rvcrunmode.ModeChangeStatusBatteryLow)            // RvcRunMode
)

// ModeTag values: the common ModeBase tags and each derivation's own, from
// the generated ModeTag enums.
const (
	TagAuto      = uint16(rvcrunmode.ModeTagAuto)
	TagQuick     = uint16(rvcrunmode.ModeTagQuick)
	TagQuiet     = uint16(rvcrunmode.ModeTagQuiet)
	TagLowNoise  = uint16(rvcrunmode.ModeTagLowNoise)
	TagLowEnergy = uint16(rvcrunmode.ModeTagLowEnergy)
	TagVacation  = uint16(rvcrunmode.ModeTagVacation)
	TagMin       = uint16(rvcrunmode.ModeTagMin)
	TagMax       = uint16(rvcrunmode.ModeTagMax)
	TagNight     = uint16(rvcrunmode.ModeTagNight)
	TagDay       = uint16(rvcrunmode.ModeTagDay)

	LaundryTagNormal   = uint16(laundrywashermode.ModeTagNormal)
	LaundryTagDelicate = uint16(laundrywashermode.ModeTagDelicate)
	LaundryTagHeavy    = uint16(laundrywashermode.ModeTagHeavy)
	LaundryTagWhites   = uint16(laundrywashermode.ModeTagWhites)

	DishwasherTagNormal = uint16(dishwashermode.ModeTagNormal)
	DishwasherTagHeavy  = uint16(dishwashermode.ModeTagHeavy)
	DishwasherTagLight  = uint16(dishwashermode.ModeTagLight)

	RvcRunTagIdle     = uint16(rvcrunmode.ModeTagIdle)
	RvcRunTagCleaning = uint16(rvcrunmode.ModeTagCleaning)
	RvcRunTagMapping  = uint16(rvcrunmode.ModeTagMapping)

	RvcCleanTagDeepClean     = uint16(rvccleanmode.ModeTagDeepClean)
	RvcCleanTagVacuum        = uint16(rvccleanmode.ModeTagVacuum)
	RvcCleanTagMop           = uint16(rvccleanmode.ModeTagMop)
	RvcCleanTagVacuumThenMop = uint16(rvccleanmode.ModeTagVacuumThenMop)

	OvenTagBake            = uint16(ovenmode.ModeTagBake)
	OvenTagConvection      = uint16(ovenmode.ModeTagConvection)
	OvenTagGrill           = uint16(ovenmode.ModeTagGrill)
	OvenTagRoast           = uint16(ovenmode.ModeTagRoast)
	OvenTagClean           = uint16(ovenmode.ModeTagClean)
	OvenTagConvectionBake  = uint16(ovenmode.ModeTagConvectionBake)
	OvenTagConvectionRoast = uint16(ovenmode.ModeTagConvectionRoast)
	OvenTagWarming         = uint16(ovenmode.ModeTagWarming)
	OvenTagProofing        = uint16(ovenmode.ModeTagProofing)
	OvenTagSteam           = uint16(ovenmode.ModeTagSteam)
	OvenTagAirFry          = uint16(ovenmode.ModeTagAirFry)
	OvenTagAirSousVide     = uint16(ovenmode.ModeTagAirSousVide)
	OvenTagFrozenFood      = uint16(ovenmode.ModeTagFrozenFood)

	RefrigeratorTagRapidCool   = uint16(rtcc.ModeTagRapidCool)
	RefrigeratorTagRapidFreeze = uint16(rtcc.ModeTagRapidFreeze)

	MicrowaveTagNormal  = uint16(microwaveovenmode.ModeTagNormal)
	MicrowaveTagDefrost = uint16(microwaveovenmode.ModeTagDefrost)

	EvseTagManual        = uint16(energyevsemode.ModeTagManual)
	EvseTagTimeOfUse     = uint16(energyevsemode.ModeTagTimeOfUse)
	EvseTagSolarCharging = uint16(energyevsemode.ModeTagSolarCharging)
	EvseTagV2X           = uint16(energyevsemode.ModeTagV2X)

	WaterHeaterTagOff    = uint16(waterheatermode.ModeTagOff)
	WaterHeaterTagManual = uint16(waterheatermode.ModeTagManual)
	WaterHeaterTagTimed  = uint16(waterheatermode.ModeTagTimed)

	DemTagNoOptimization     = uint16(deviceenergymanagementmode.ModeTagNoOptimization)
	DemTagDeviceOptimization = uint16(deviceenergymanagementmode.ModeTagDeviceOptimization)
	DemTagLocalOptimization  = uint16(deviceenergymanagementmode.ModeTagLocalOptimization)
	DemTagGridOptimization   = uint16(deviceenergymanagementmode.ModeTagGridOptimization)
)

// Limits from the schema.
const (
	SupportedModesMin = 2 // SupportedModes "2 to 255"
	SupportedModesMax = 255
	LabelMaxBytes     = 64 // ModeOptionStruct Label, StatusText "max 64"
	ModeTagsMin       = 1  // the derivations' ModeTags "1 to 8"
	ModeTagsMax       = 8
)

// ModeTag is one tag of a mode. MfgCode, when set, makes it a
// manufacturer-specific tag in that vendor's namespace.
type ModeTag struct {
	MfgCode *uint16
	Value   uint16
}

// ModeOption is one SupportedModes entry.
type ModeOption struct {
	Label string
	Mode  uint8
	Tags  []ModeTag
}

// ModeChanger is the host port: it moves the device to a supported mode
// other than the current one. Success makes NewMode the CurrentMode; any
// other status is the response's — GenericFailure, InvalidInMode, a status
// the derivation defines, or a product-specific one (0x80 and above) —
// with text for the user. A Go error answers the invoke with FAILURE.
type ModeChanger interface {
	ChangeToMode(ctx context.Context, newMode uint8) (status Status, text string, err error)
}

// Config carries the construction parameters.
type Config struct {
	// Changer applies a ChangeToMode; required where the definition
	// accepts ChangeToMode (every derivation but MicrowaveOvenMode).
	Changer ModeChanger
	// SupportedModes is fixed for the server's life (quality F).
	SupportedModes []ModeOption
	// CurrentMode is the initial mode, a supported one.
	CurrentMode uint8
	// Features: FeatureDirectModeChange on RvcRunMode / RvcCleanMode.
	Features Feature
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Configuration and setter errors.
var (
	ErrNoChanger         = errors.New("modebase: a ModeChanger is required")
	ErrUnknownFeature    = errors.New("modebase: feature not defined for this cluster")
	ErrModeCount         = errors.New("modebase: SupportedModes needs 2 to 255 entries")
	ErrDuplicateLabel    = errors.New("modebase: duplicate label in supportedModes")
	ErrDuplicateMode     = errors.New("modebase: duplicate mode in supportedModes")
	ErrLabel             = errors.New("modebase: label exceeds 64 bytes")
	ErrTagCount          = errors.New("modebase: a mode needs 1 to 8 mode tags")
	ErrDuplicateTag      = errors.New("modebase: a mode lists a tag twice")
	ErrDuplicateTagSet   = errors.New("modebase: two modes have the same set of tags")
	ErrNoStandardTag     = errors.New("modebase: a mode needs at least one standard tag")
	ErrRequiredTag       = errors.New("modebase: supportedModes lacks a tag the cluster requires")
	ErrExclusiveTags     = errors.New("modebase: provided supportedModes must not have Idle, Cleaning and Mapping mode tags together in one mode")
	ErrTagCombination    = errors.New("modebase: supportedModes combine tags the cluster forbids together")
	ErrUnsupportedMode   = errors.New("modebase: can not use unsupported mode")
	ErrInvalidHostStatus = errors.New("modebase: the mode changer answered a reserved status")
)

// kind is one ModeBase derivation: its generated definition, its
// ModeChangeStatus enum (ModeBase's values and its own) and the
// SupportedModes rule its matter.js server adds.
type kind struct {
	def      *spec.Cluster
	statuses *spec.Enum
	check    func([]ModeOption) error
}

var (
	laundryWasherKind = kind{laundrywashermode.Definition, laundrywashermode.ModeChangeStatusDef, requireTag(LaundryTagNormal, "Normal")}
	dishwasherKind    = kind{dishwashermode.Definition, dishwashermode.ModeChangeStatusDef, requireTag(DishwasherTagNormal, "Normal")}
	rvcRunKind        = kind{rvcrunmode.Definition, rvcrunmode.ModeChangeStatusDef, checkRvcRun}
	rvcCleanKind      = kind{rvccleanmode.Definition, rvccleanmode.ModeChangeStatusDef, checkRvcClean}
	ovenKind          = kind{ovenmode.Definition, ovenmode.ModeChangeStatusDef, requireTag(OvenTagBake, "Bake")}
	refrigeratorKind  = kind{rtcc.Definition, rtcc.ModeChangeStatusDef, requireTag(uint16(rtcc.ModeTagAuto), "Auto")}
	microwaveKind     = kind{microwaveovenmode.Definition, microwaveovenmode.ModeChangeStatusDef, checkMicrowaveOven}
	evseKind          = kind{energyevsemode.Definition, energyevsemode.ModeChangeStatusDef, checkEnergyEvse}
	waterHeaterKind   = kind{waterheatermode.Definition, waterheatermode.ModeChangeStatusDef, checkWaterHeater}
	demKind           = kind{deviceenergymanagementmode.Definition, deviceenergymanagementmode.ModeChangeStatusDef, checkDeviceEnergyManagement}
)

func hasTag(m ModeOption, values ...uint16) bool {
	return slices.ContainsFunc(m.Tags, func(t ModeTag) bool { return slices.Contains(values, t.Value) })
}

// requireTag mirrors LaundryWasherModeServer / DishwasherModeServer
// #assertSupportedModes: "Provided supportedModes need to include at least
// Normal mode tag"; with Bake it mirrors OvenModeServer's, with Auto
// RefrigeratorAndTemperatureControlledCabinetModeServer's
// (packages/node/src/behaviors/<derivation>-mode/*ModeServer.ts).
func requireTag(value uint16, name string) func([]ModeOption) error {
	return func(modes []ModeOption) error {
		if !slices.ContainsFunc(modes, func(m ModeOption) bool { return hasTag(m, value) }) {
			return fmt.Errorf("%w: at least one %s mode", ErrRequiredTag, name)
		}
		return nil
	}
}

// checkRvcRun mirrors RvcRunModeServer #assertSupportedModes.
func checkRvcRun(modes []ModeOption) error {
	if err := requireTag(RvcRunTagIdle, "Idle")(modes); err != nil {
		return err
	}
	if err := requireTag(RvcRunTagCleaning, "Cleaning")(modes); err != nil {
		return err
	}
	for _, m := range modes {
		n := 0
		for _, v := range []uint16{RvcRunTagIdle, RvcRunTagCleaning, RvcRunTagMapping} {
			if hasTag(m, v) {
				n++
			}
		}
		if n > 1 {
			return fmt.Errorf("%w: mode %d", ErrExclusiveTags, m.Mode)
		}
	}
	return nil
}

// checkRvcClean mirrors RvcCleanModeServer #assertSupportedModes: "at
// least Vacuum or Mop mode tag".
func checkRvcClean(modes []ModeOption) error {
	if !slices.ContainsFunc(modes, func(m ModeOption) bool { return hasTag(m, RvcCleanTagVacuum, RvcCleanTagMop) }) {
		return fmt.Errorf("%w: at least one Vacuum or Mop mode", ErrRequiredTag)
	}
	return nil
}

// someMode reports whether a mode satisfies f.
func someMode(modes []ModeOption, f func(ModeOption) bool) bool { return slices.ContainsFunc(modes, f) }

// checkMicrowaveOven mirrors MicrowaveOvenModeServer #assertSupportedModes
// (packages/node/src/behaviors/microwave-oven-mode/MicrowaveOvenModeServer.ts):
// "exactly one Normal mode tag", and "must not have Normal and Defrost
// mode tags together in one mode".
func checkMicrowaveOven(modes []ModeOption) error {
	normal := 0
	for _, m := range modes {
		if hasTag(m, MicrowaveTagNormal) {
			normal++
		}
	}
	if normal != 1 {
		return fmt.Errorf("%w: exactly one Normal mode, have %d", ErrRequiredTag, normal)
	}
	if someMode(modes, func(m ModeOption) bool { return hasTag(m, MicrowaveTagNormal) && hasTag(m, MicrowaveTagDefrost) }) {
		return fmt.Errorf("%w: Normal and Defrost in one mode", ErrTagCombination)
	}
	return nil
}

// checkEnergyEvse mirrors EnergyEvseModeServer #assertSupportedModes
// (packages/node/src/behaviors/energy-evse-mode/EnergyEvseModeServer.ts):
// "Provided supportedModes need to include at least one Manual mode tag,
// but not together with TimeOfUse or SolarCharging".
func checkEnergyEvse(modes []ModeOption) error {
	if !someMode(modes, func(m ModeOption) bool {
		return hasTag(m, EvseTagManual) && !hasTag(m, EvseTagTimeOfUse, EvseTagSolarCharging)
	}) {
		return fmt.Errorf("%w: at least one Manual mode without TimeOfUse or SolarCharging", ErrRequiredTag)
	}
	return nil
}

// checkWaterHeater mirrors WaterHeaterModeServer #assertSupportedModes
// (packages/node/src/behaviors/water-heater-mode/WaterHeaterModeServer.ts)
// as its code reads, not its message: every one of Manual and Off occurs
// in some mode, and no mode with Off, Manual or Timed has more than one
// tag.
func checkWaterHeater(modes []ModeOption) error {
	for _, v := range []uint16{WaterHeaterTagManual, WaterHeaterTagOff} {
		if !someMode(modes, func(m ModeOption) bool { return hasTag(m, v) }) {
			return fmt.Errorf("%w: a Manual and an Off mode", ErrRequiredTag)
		}
	}
	for _, v := range []uint16{WaterHeaterTagOff, WaterHeaterTagManual, WaterHeaterTagTimed} {
		if someMode(modes, func(m ModeOption) bool { return hasTag(m, v) && len(m.Tags) > 1 }) {
			return fmt.Errorf("%w: Off, Manual or Timed in a mode with more than one tag", ErrTagCombination)
		}
	}
	return nil
}

// checkDeviceEnergyManagement mirrors DeviceEnergyManagementModeServer
// #assertSupportedModes
// (packages/node/src/behaviors/device-energy-management-mode/DeviceEnergyManagementModeServer.ts):
// each of NoOptimization, LocalOptimization and GridOptimization occurs in
// some mode, and none of DeviceOptimization, LocalOptimization and
// GridOptimization occurs in a mode that also has NoOptimization.
func checkDeviceEnergyManagement(modes []ModeOption) error {
	for _, v := range []uint16{DemTagNoOptimization, DemTagLocalOptimization, DemTagGridOptimization} {
		if !someMode(modes, func(m ModeOption) bool { return hasTag(m, v) }) {
			return fmt.Errorf("%w: a NoOptimization, a LocalOptimization and a GridOptimization mode", ErrRequiredTag)
		}
	}
	for _, v := range []uint16{DemTagDeviceOptimization, DemTagLocalOptimization, DemTagGridOptimization} {
		if someMode(modes, func(m ModeOption) bool { return hasTag(m, v) && hasTag(m, DemTagNoOptimization) }) {
			return fmt.Errorf("%w: NoOptimization with an optimization tag in one mode", ErrTagCombination)
		}
	}
	return nil
}

// Server implements [contract.ClusterServer] for one ModeBase derivation.
type Server struct {
	cluster.AttributeChanges

	k        kind
	inst     *spec.Instance
	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	changer  ModeChanger
	modes    []ModeOption

	mu      sync.Mutex
	current uint8
}

// Compile-time assertions.
var (
	_ contract.ClusterServer           = (*Server)(nil)
	_ contract.ClusterDataVersion      = (*Server)(nil)
	_ contract.ClusterAttributeLister  = (*Server)(nil)
	_ contract.ClusterCommandLister    = (*Server)(nil)
	_ contract.ClusterEventLister      = (*Server)(nil)
	_ contract.AttributeChangeNotifier = (*Server)(nil)
)

// NewLaundryWasherMode builds a LaundryWasherMode (0x0051) server.
func NewLaundryWasherMode(cfg Config) (*Server, error) { return newServer(laundryWasherKind, cfg) }

// NewDishwasherMode builds a DishwasherMode (0x0059) server.
func NewDishwasherMode(cfg Config) (*Server, error) { return newServer(dishwasherKind, cfg) }

// NewRvcRunMode builds an RvcRunMode (0x0054) server.
func NewRvcRunMode(cfg Config) (*Server, error) { return newServer(rvcRunKind, cfg) }

// NewRvcCleanMode builds an RvcCleanMode (0x0055) server.
func NewRvcCleanMode(cfg Config) (*Server, error) { return newServer(rvcCleanKind, cfg) }

// NewOvenMode builds an OvenMode server.
func NewOvenMode(cfg Config) (*Server, error) { return newServer(ovenKind, cfg) }

// NewRefrigeratorAndTemperatureControlledCabinetMode builds a
// RefrigeratorAndTemperatureControlledCabinetMode server.
func NewRefrigeratorAndTemperatureControlledCabinetMode(cfg Config) (*Server, error) {
	return newServer(refrigeratorKind, cfg)
}

// NewMicrowaveOvenMode builds a MicrowaveOvenMode server. Its definition
// disallows ChangeToMode, so Config.Changer is not used.
func NewMicrowaveOvenMode(cfg Config) (*Server, error) { return newServer(microwaveKind, cfg) }

// NewEnergyEvseMode builds an EnergyEvseMode server.
func NewEnergyEvseMode(cfg Config) (*Server, error) { return newServer(evseKind, cfg) }

// NewWaterHeaterMode builds a WaterHeaterMode server.
func NewWaterHeaterMode(cfg Config) (*Server, error) { return newServer(waterHeaterKind, cfg) }

// NewDeviceEnergyManagementMode builds a DeviceEnergyManagementMode server.
func NewDeviceEnergyManagementMode(cfg Config) (*Server, error) { return newServer(demKind, cfg) }

func newServer(k kind, cfg Config) (*Server, error) {
	// An undefined bit, or DEPONOFF — "X" in every derivation served here.
	inst, err := spec.New(k.def, spec.Options{Features: uint32(cfg.Features)})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnknownFeature, err)
	}
	if cfg.Changer == nil && inst.Accepts(CmdChangeToMode) {
		return nil, ErrNoChanger
	}
	if err := checkModes(cfg.SupportedModes); err != nil {
		return nil, err
	}
	if err := k.check(cfg.SupportedModes); err != nil {
		return nil, err
	}
	modes := make([]ModeOption, len(cfg.SupportedModes))
	for i, m := range cfg.SupportedModes {
		modes[i] = ModeOption{Label: m.Label, Mode: m.Mode, Tags: slices.Clone(m.Tags)}
	}
	s := &Server{k: k, inst: inst, ext: cfg.DataVersion, changer: cfg.Changer, modes: modes, current: cfg.CurrentMode}
	// The mode servers' initialize: ModeUtils.assertMode(supportedModes,
	// currentMode).
	if !s.supports(cfg.CurrentMode) {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedMode, cfg.CurrentMode)
	}
	return s, nil
}

// checkModes validates SupportedModes against ModeUtils.assertSupportedModes,
// the schema constraints and the ModeBase specification text.
func checkModes(modes []ModeOption) error {
	if len(modes) < SupportedModesMin || len(modes) > SupportedModesMax {
		return fmt.Errorf("%w: %d", ErrModeCount, len(modes))
	}
	labels := map[string]bool{}
	values := map[uint8]bool{}
	var tagSets [][]ModeTag
	for _, m := range modes {
		if labels[m.Label] {
			return fmt.Errorf("%w: %s", ErrDuplicateLabel, m.Label)
		}
		if values[m.Mode] {
			return fmt.Errorf("%w: %d", ErrDuplicateMode, m.Mode)
		}
		labels[m.Label], values[m.Mode] = true, true
		if len(m.Label) > LabelMaxBytes || !utf8.ValidString(m.Label) {
			return fmt.Errorf("%w: mode %d", ErrLabel, m.Mode)
		}
		if len(m.Tags) < ModeTagsMin || len(m.Tags) > ModeTagsMax {
			return fmt.Errorf("%w: mode %d has %d", ErrTagCount, m.Mode, len(m.Tags))
		}
		// "Each mode tag in this field shall be distinct from other mode
		// tags in this field" and "A mode option shall be associated with
		// at least one standard mode tag" (mode-base.resource.ts,
		// ModeOptionStruct.ModeTags).
		standard := false
		for i, t := range m.Tags {
			if slices.ContainsFunc(m.Tags[:i], func(u ModeTag) bool { return sameTag(t, u) }) {
				return fmt.Errorf("%w: mode %d", ErrDuplicateTag, m.Mode)
			}
			standard = standard || t.MfgCode == nil
		}
		if !standard {
			return fmt.Errorf("%w: mode %d", ErrNoStandardTag, m.Mode)
		}
		// "The set of ModeTags listed in each entry in this list shall be
		// distinct from the sets of ModeTags listed in the other entries"
		// (mode-base.resource.ts, SupportedModes).
		for _, other := range tagSets {
			if sameTagSet(m.Tags, other) {
				return fmt.Errorf("%w: mode %d", ErrDuplicateTagSet, m.Mode)
			}
		}
		tagSets = append(tagSets, m.Tags)
	}
	return nil
}

func sameTag(a, b ModeTag) bool {
	return a.Value == b.Value && (a.MfgCode == nil) == (b.MfgCode == nil) && (a.MfgCode == nil || *a.MfgCode == *b.MfgCode)
}

// sameTagSet compares two tag lists as sets; each list's tags are
// distinct.
func sameTagSet(a, b []ModeTag) bool {
	if len(a) != len(b) {
		return false
	}
	for _, t := range a {
		if !slices.ContainsFunc(b, func(u ModeTag) bool { return sameTag(t, u) }) {
			return false
		}
	}
	return true
}

func (s *Server) supports(mode uint8) bool {
	return slices.ContainsFunc(s.modes, func(m ModeOption) bool { return m.Mode == mode })
}

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// Revision returns the cluster revision of the generated definition.
func (s *Server) Revision() uint16 { return s.inst.Revision() }

// MatterClusterID returns the derivation's cluster id.
func (s *Server) MatterClusterID() uint32 { return s.inst.MatterClusterID() }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterAttributes implements [contract.ClusterAttributeLister].
func (s *Server) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterReportable lists CurrentMode; SupportedModes is fixed.
func (s *Server) MatterReportable() []uint32 { return s.inst.MatterReportable() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *Server) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (s *Server) MatterGeneratedCommands() []uint32 { return s.inst.MatterGeneratedCommands() }

// MatterEvents implements [contract.ClusterEventLister]: ModeBase defines
// no event.
func (s *Server) MatterEvents() []uint32 { return s.inst.MatterEvents() }

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	switch attrID {
	case AttrSupportedModes:
		out := make([]clusterwire.ModeOptionStruct, 0, len(s.modes))
		for _, m := range s.modes {
			tags := make([]clusterwire.ModeTagStruct, 0, len(m.Tags))
			for _, t := range m.Tags {
				tags = append(tags, clusterwire.ModeTagStruct{MfgCode: t.MfgCode, Value: t.Value})
			}
			out = append(out, clusterwire.ModeOptionStruct{Label: m.Label, Mode: m.Mode, ModeTags: tags})
		}
		return out, true
	case AttrCurrentMode:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.current, true
	}
	return nil, false
}

// MatterWrite refuses every write: SupportedModes and CurrentMode are
// read-only, StartUpMode and OnMode are not served. The definition
// answers each with its status.
func (s *Server) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := s.inst.ValidateWrite(attrID, value, nil)
	return err
}

// MatterInvoke answers ChangeToMode with a ChangeToModeResponse where the
// definition accepts it (MicrowaveOvenMode's does not).
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if cmdID != CmdChangeToMode || !s.inst.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("modebase: command 0x%02X is not supported", cmdID)
	}
	req, ok := changeToModeRequest(fields)
	if !ok {
		return nil, statusError{im.StatusInvalidCommand, fmt.Sprintf("modebase: ChangeToMode fields %T", fields)}
	}
	s.mu.Lock()
	current := s.current
	s.mu.Unlock()
	// ModeUtils.assertModeChange.
	if req.NewMode == current {
		return clusterwire.ChangeToModeResponse{Status: uint8(StatusSuccess)}, nil
	}
	if !s.supports(req.NewMode) {
		return clusterwire.ChangeToModeResponse{
			Status: uint8(StatusUnsupportedMode), StatusText: fmt.Sprintf("Unsupported mode: %d", req.NewMode),
		}, nil
	}
	status, text, err := s.changer.ChangeToMode(ctx, req.NewMode)
	if err != nil {
		return nil, fmt.Errorf("modebase: ChangeToMode %d: %w", req.NewMode, err)
	}
	if !s.validHostStatus(status) {
		return nil, statusError{im.StatusFailure, fmt.Sprintf("%v: 0x%02X", ErrInvalidHostStatus, uint8(status))}
	}
	if status == StatusSuccess {
		s.setCurrent(req.NewMode)
	}
	return clusterwire.ChangeToModeResponse{Status: uint8(status), StatusText: truncate(text, LabelMaxBytes)}, nil
}

// changeToModeRequest reads the fields the bridge hands over: cluster/wire's
// ChangeToModeRequest, which its hand-written reader decodes for
// LaundryWasherMode, RvcRunMode, RvcCleanMode and DishwasherMode, or a
// generated definition's, which spec.DecodeRequest decodes for the other
// derivations. The payload is ModeBase's in every derivation.
func changeToModeRequest(fields any) (clusterwire.ChangeToModeRequest, bool) {
	var mode uint8
	switch r := fields.(type) {
	case clusterwire.ChangeToModeRequest:
		return r, true
	case laundrywashermode.ChangeToModeRequest:
		mode = r.NewMode
	case rvcrunmode.ChangeToModeRequest:
		mode = r.NewMode
	case rvccleanmode.ChangeToModeRequest:
		mode = r.NewMode
	case dishwashermode.ChangeToModeRequest:
		mode = r.NewMode
	case ovenmode.ChangeToModeRequest:
		mode = r.NewMode
	case rtcc.ChangeToModeRequest:
		mode = r.NewMode
	case energyevsemode.ChangeToModeRequest:
		mode = r.NewMode
	case waterheatermode.ChangeToModeRequest:
		mode = r.NewMode
	case deviceenergymanagementmode.ChangeToModeRequest:
		mode = r.NewMode
	default:
		return clusterwire.ChangeToModeRequest{}, false
	}
	return clusterwire.ChangeToModeRequest{NewMode: mode}, true
}

// validHostStatus accepts what a device may answer: a value of the
// derivation's ModeChangeStatus enum — Success, GenericFailure,
// InvalidInMode or one the derivation defines — or a product-specific one
// (0x80 and above). UnsupportedMode is the server's own answer.
func (s *Server) validHostStatus(st Status) bool {
	if st == StatusUnsupportedMode {
		return false
	}
	return st >= 0x80 || slices.ContainsFunc(s.k.statuses.Values, func(v spec.EnumValue) bool { return v.Value == uint64(st) })
}

// truncate cuts text to at most n bytes on a rune boundary.
func truncate(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// SetCurrentMode records a mode change the device made on its own — "the
// value of this attribute may change at any time via an out-of-band
// interaction" (mode-base.resource.ts, CurrentMode). The mode must be a
// supported one, as the mode servers' currentMode$Changing reactor
// asserts (ModeUtils.assertMode).
func (s *Server) SetCurrentMode(mode uint8) error {
	if !s.supports(mode) {
		return fmt.Errorf("%w: %d", ErrUnsupportedMode, mode)
	}
	s.setCurrent(mode)
	return nil
}

func (s *Server) setCurrent(mode uint8) {
	s.mu.Lock()
	if s.current == mode {
		s.mu.Unlock()
		return
	}
	s.current = mode
	s.mu.Unlock()
	s.tracker().Bump()
	s.Notify(AttrCurrentMode)
}

// statusError carries an exact IM status to the dispatcher.
type statusError struct {
	status im.StatusCode
	msg    string
}

func (e statusError) Error() string                   { return e.msg }
func (e statusError) MatterStatusCode() im.StatusCode { return e.status }

var _ im.StatusCodeError = statusError{}
