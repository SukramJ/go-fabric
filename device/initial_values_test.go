// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"github.com/SukramJ/go-fabric/cluster/spec"
	aqdef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	abdef "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	cccdef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	cddef "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	demdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	eemdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	evsemodedef "github.com/SukramJ/go-fabric/cluster/spec/energyevsemode"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	jfddef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	mpdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	mwocdef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	mwomodedef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	msdef "github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	ncdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	opstatedef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	rvcopdef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	rvcrundef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	soildef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
	tbrmdef "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	whmdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	whmodedef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
)

// The values a host states for the attributes New refuses a type without
// (Type.RequiredInitial, decision 17). Each is taken from the fixtures of
// matter.js packages/node/test/endpoint/validation/StandardDeviceTypeCorpusTest.ts
// at b25ed960, the line cited; a value matter.js gives none for — a server
// its corpus does not build (:366-384) — is the lowest valid value of the
// type or constraint, marked "test input, no reference value".

// corpusCorpus helpers name the corpus's shared state.
var (
	// FAN_CONTROL_STATE (:59-64).
	fanControlState = withInitial(fandef.ClusterID, map[uint32]any{fandef.AttrFanModeSequence: fandef.FanModeSequenceOffLowMedHigh})
	// THREAD_BORDER_ROUTER_STATE (:125-130).
	threadBorderRouterState = withInitial(tbrmdef.ClusterID, map[uint32]any{
		tbrmdef.AttrBorderRouterName: "Test Border Router",
		tbrmdef.AttrBorderAgentId:    []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
	})
	// CAMERA_MICROPHONE_CAPABILITIES (:132-137) in
	// CAMERA_AV_STREAM_MANAGEMENT_AUDIO_STATE (:148-155).
	cameraAudioState = withInitial(avdef.ClusterID, map[uint32]any{
		avdef.AttrMicrophoneCapabilities: avdef.AudioCapabilitiesStruct{
			MaxNumberOfChannels: 1, SupportedCodecs: []avdef.AudioCodecEnum{avdef.AudioCodecOpus},
			SupportedSampleRates: []uint32{48000}, SupportedBitDepths: []uint8{16},
		},
	})
	// CAMERA_AV_STREAM_MANAGEMENT_STATE (:157-169): the audio state, the
	// video sensor (:139-143), the minimum viewport and the viewport.
	cameraVideoState = withInitial(avdef.ClusterID, map[uint32]any{
		avdef.AttrVideoSensorParams:     avdef.VideoSensorParamsStruct{SensorWidth: 1920, SensorHeight: 1080, MaxFps: 30},
		avdef.AttrMinViewportResolution: avdef.VideoResolutionStruct{Width: 640, Height: 480},
		avdef.AttrViewport:              avdef.ViewportStruct{X1: 0, Y1: 0, X2: 1920, Y2: 1080},
	})
	// OPERATIONAL_STATE_WITH_ERROR (:66-69): the state Stopped.
	operationalStateStopped = withInitial(opstatedef.ClusterID, map[uint32]any{opstatedef.AttrOperationalState: opstatedef.OperationalStateStopped})
	// The electrical power measurement's power mode and accuracy: test
	// input, no reference value (matter.js's corpus builds no
	// ElectricalPowerMeasurement, :366-384).
	electricalPowerState = withInitial(epmdef.ClusterID, map[uint32]any{
		epmdef.AttrPowerMode: epmdef.PowerModeUnknown,
		epmdef.AttrAccuracy: spec.List[epmdef.MeasurementAccuracyStruct]{{
			MeasurementType: epmdef.MeasurementTypeVoltage, AccuracyRanges: []epmdef.MeasurementAccuracyRangeStruct{{PercentMax: ptrTo[uint16](0)}},
		}},
	})
	// The electrical energy measurement's accuracy: test input, no
	// reference value.
	electricalEnergyState = withInitial(eemdef.ClusterID, map[uint32]any{
		eemdef.AttrAccuracy: eemdef.MeasurementAccuracyStruct{
			MeasurementType: eemdef.MeasurementTypeElectricalEnergy, AccuracyRanges: []eemdef.MeasurementAccuracyRangeStruct{{PercentMax: ptrTo[uint16](0)}},
		},
	})
)

func ptrTo[T any](v T) *T { return &v }

// initialValues are the statements per type name.
var initialValues = map[string][]Declaration{
	"AirPurifier":        {fanControlState},                                                                                     // :175
	"AirQualitySensor":   {withInitial(aqdef.ClusterID, map[uint32]any{aqdef.AttrAirQuality: aqdef.AirQualityGood})},            // :176
	"AudioDoorbell":      {cameraAudioState},                                                                                    // :177
	"BasicVideoPlayer":   {withInitial(mpdef.ClusterID, map[uint32]any{mpdef.AttrCurrentState: mpdef.PlaybackStateNotPlaying})}, // :178
	"Camera":             {cameraAudioState, cameraVideoState},                                                                  // :179
	"CastingVideoPlayer": {withInitial(mpdef.ClusterID, map[uint32]any{mpdef.AttrCurrentState: mpdef.PlaybackStateNotPlaying})}, // :180
	"Chime": {withInitial(chimedef.ClusterID, map[uint32]any{ // :181
		chimedef.AttrInstalledChimeSounds: spec.List[chimedef.ChimeSoundStruct]{{ChimeId: 1, Name: "Default"}},
	})},
	// Test input, no reference value (the corpus states only a tag list
	// for Closure and ClosurePanel, :182-183).
	"Closure":      {withInitial(cccdef.ClusterID, map[uint32]any{cccdef.AttrMainState: cccdef.MainStateStopped})},
	"ClosurePanel": {withInitial(cddef.ClusterID, map[uint32]any{cddef.AttrTranslationDirection: cddef.TranslationDirectionDownward})},
	"ContentApp": {withInitial(abdef.ClusterID, map[uint32]any{ // :185-192
		abdef.AttrApplicationName:    "Test App",
		abdef.AttrApplication:        abdef.ApplicationStruct{CatalogVendorId: 0, ApplicationId: "test"},
		abdef.AttrStatus:             abdef.ApplicationStatusStopped,
		abdef.AttrApplicationVersion: "1",
	})},
	// Test input, no reference value (no DeviceEnergyManagement in the
	// corpus, :366-384).
	"DeviceEnergyManagement": {withInitial(demdef.ClusterID, map[uint32]any{demdef.AttrEsaType: demdef.ESATypeEvse, demdef.AttrEsaState: demdef.ESAStateOffline})},
	"Dishwasher":             {operationalStateStopped}, // :193
	"ElectricalMeter":        {electricalPowerState, electricalEnergyState},
	"ElectricalSensor":       {electricalPowerState, electricalEnergyState},
	"EnergyEvse": {withInitial(evsemodedef.ClusterID, map[uint32]any{ // :196-199, ENERGY_EVSE_MODE_STATE :84-89
		evsemodedef.AttrSupportedModes: spec.List[evsemodedef.ModeOptionStruct]{
			{Label: "Manual", Mode: 0, ModeTags: []evsemodedef.ModeTagStruct{{Value: evsemodedef.ModeTagManual}}},
			{Label: "Quick", Mode: 1, ModeTags: []evsemodedef.ModeTagStruct{{Value: evsemodedef.ModeTagTimeOfUse}}},
		},
	})},
	"ExtractorHood": {fanControlState},  // :201
	"Fan":           {fanControlState},  // :202
	"Intercom":      {cameraAudioState}, // :203
	"JointFabricAdministrator": {withInitial(jfddef.ClusterID, map[uint32]any{ // :204-247
		jfddef.AttrAnchorRootCa: []byte{0x00},
		jfddef.AttrFriendlyName: "Test Fabric",
		jfddef.AttrGroupKeySetList: spec.List[jfddef.DatastoreGroupKeySetStruct]{{
			GroupKeySetId: 0, GroupKeySecurityPolicy: jfddef.DatastoreGroupKeySecurityPolicyTrustFirst,
			EpochKey0: spec.Nullable[[]byte]{Null: true}, EpochStartTime0: spec.Nullable[uint64]{Null: true}, EpochKey1: spec.Nullable[[]byte]{Null: true},
			EpochStartTime1: spec.Nullable[uint64]{Null: true}, EpochKey2: spec.Nullable[[]byte]{Null: true}, EpochStartTime2: spec.Nullable[uint64]{Null: true},
		}},
		jfddef.AttrGroupList: spec.List[jfddef.DatastoreGroupInformationEntryStruct]{{
			GroupId: 1, FriendlyName: "Test Group", GroupKeySetId: spec.Nullable[uint16]{Null: true}, GroupCat: spec.Nullable[uint16]{Null: true},
			GroupCatVersion: spec.Nullable[uint16]{Null: true}, GroupPermission: jfddef.DatastoreAccessControlEntryPrivilegeView,
		}},
		jfddef.AttrAdminList: spec.List[jfddef.DatastoreAdministratorInformationEntryStruct]{{
			NodeId: 1, FriendlyName: "Test Admin", VendorId: 1, Icac: []byte{0x00},
		}},
		jfddef.AttrStatus: jfddef.DatastoreStatusEntryStruct{State: jfddef.DatastoreStateCommitted, UpdateTimestamp: 946684800, FailureCode: jfddef.StatusSuccess},
	})},
	"LaundryDryer":  {operationalStateStopped}, // :248
	"LaundryWasher": {operationalStateStopped}, // :249
	"MicrowaveOven": {
		operationalStateStopped, // :250-253, MICROWAVE_OVEN_MODE_STATE :91-96
		withInitial(mwomodedef.ClusterID, map[uint32]any{mwomodedef.AttrSupportedModes: spec.List[mwomodedef.ModeOptionStruct]{
			{Label: "Normal", Mode: 0, ModeTags: []mwomodedef.ModeTagStruct{{Value: mwomodedef.ModeTagNormal}}},
			{Label: "Defrost", Mode: 1, ModeTags: []mwomodedef.ModeTagStruct{{Value: mwomodedef.ModeTagDefrost}}},
		}}),
		// Test input, no reference value (the corpus builds no
		// MicrowaveOvenControl, :366-384): the constraints' minimum 1.
		withInitial(mwocdef.ClusterID, map[uint32]any{mwocdef.AttrCookTime: uint32(1), mwocdef.AttrMaxCookTime: uint32(1)}),
	},
	"ModeSelect":                   {withInitial(msdef.ClusterID, map[uint32]any{msdef.AttrDescription: "Test Mode"})}, // :254, MODE_SELECT_STATE :105-108
	"NetworkInfrastructureManager": {threadBorderRouterState},                                                          // :255
	// Test input, no reference value (no PumpConfigurationAndControl in
	// the corpus, :366-384).
	"Pump": {withInitial(pumpdef.ClusterID, map[uint32]any{
		pumpdef.AttrEffectiveOperationMode: pumpdef.OperationModeNormal, pumpdef.AttrEffectiveControlMode: pumpdef.ControlModeConstantSpeed,
	})},
	"RoboticVacuumCleaner": { // :256-259
		withInitial(rvcrundef.ClusterID, map[uint32]any{rvcrundef.AttrSupportedModes: spec.List[rvcrundef.ModeOptionStruct]{ // RVC_RUN_MODE_STATE :76-81
			{Label: "Idle", Mode: 0, ModeTags: []rvcrundef.ModeTagStruct{{Value: rvcrundef.ModeTagIdle}}},
			{Label: "Clean", Mode: 1, ModeTags: []rvcrundef.ModeTagStruct{{Value: rvcrundef.ModeTagCleaning}}},
		}}),
		withInitial(rvcopdef.ClusterID, map[uint32]any{ // RVC_OPERATIONAL_STATE_WITH_ERROR :71-74
			rvcopdef.AttrOperationalState:     rvcopdef.OperationalStateError,
			rvcopdef.AttrOperationalStateList: spec.List[rvcopdef.OperationalStateStruct]{{OperationalStateId: rvcopdef.OperationalStateError}},
		}),
	},
	// Test input, no reference value (the corpus builds no
	// NetworkCommissioning there, :366-384): the constraint's minimum 1.
	"SecondaryNetworkInterface": {withInitial(ncdef.ClusterID, map[uint32]any{ncdef.AttrMaxNetworks: uint8(1)})},
	"SoilSensor": {withInitial(soildef.ClusterID, map[uint32]any{ // :260, SOIL_MOISTURE_LIMITS :117-123
		soildef.AttrSoilMoistureMeasurementLimits: soildef.MeasurementAccuracyStruct{
			MeasurementType: soildef.MeasurementTypeSoilMoisture, Measured: true, MinMeasuredValue: 20, MaxMeasuredValue: 80,
			AccuracyRanges: []soildef.MeasurementAccuracyRangeStruct{{RangeMin: 20, RangeMax: 80, PercentMax: ptrTo[uint16](1000)}},
		},
	})},
	"ThreadBorderRouter": {threadBorderRouterState}, // :264
	"WaterHeater": { // :265-272
		withInitial(whmdef.ClusterID, map[uint32]any{whmdef.AttrBoostState: whmdef.BoostStateInactive}),
		withInitial(whmodedef.ClusterID, map[uint32]any{whmodedef.AttrSupportedModes: spec.List[whmodedef.ModeOptionStruct]{ // WATER_HEATER_MODE_STATE :98-103
			{Label: "Off", Mode: 0, ModeTags: []whmodedef.ModeTagStruct{{Value: whmodedef.ModeTagOff}}},
			{Label: "Manual", Mode: 1, ModeTags: []whmodedef.ModeTagStruct{{Value: whmodedef.ModeTagManual}}},
		}}),
	},
}

// initialFor returns the statements of the named type's required
// attributes ([initialValues]); none for a type that needs none.
func initialFor(name string) []Declaration { return initialValues[name] }

// newWith is New with the statements of the attributes New requires of
// ty (initialFor), after decls.
func newWith(ty *Type, opts Options, decls ...Declaration) (*Device, error) {
	return New(ty, opts, append(decls, initialFor(ty.Name)...)...)
}

// req is decls with the statements of the attributes New requires of the
// named type (initialFor).
func req(name string, decls ...Declaration) []Declaration {
	return append(decls, initialFor(name)...)
}
