// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"regexp"
	"time"
)

// This file carries no build tag on purpose: the family table is the
// source of docs/certifiability.md, and TestCertifiabilityDocument (which
// needs no harness) holds that document to it in every `go test ./...`.

// gapClass is the class of a certification case this module does not pass,
// exactly as docs/adr/0011-certifiability-is-a-goal.md names them.
type gapClass string

const (
	// classDefect: go-fabric misbehaves or lacks mandatory behaviour. The
	// reason names the open finding (notes/parity/matter_behaviour_findings.md,
	// "Certification harness" section, keyed by TC id).
	classDefect gapClass = "(a) defect"
	// classNotSupported: optional per spec for the device types exposed and
	// declared so in PICS; the reason names the PICS line.
	classNotSupported gapClass = "(b) not supported"
	// classHarness: the test case or the environment is at fault; the
	// reason cites the evidence.
	classHarness gapClass = "(c) harness"
	// classOutOfScope: Bluetooth, Thread, Wi-Fi commissioning, controller
	// role; the reason names the scope decision.
	classOutOfScope gapClass = "(d) out of scope"
)

// controllerRoleCases are matter.js's certification cases with its
// controller as the DUT (support/chip-testing/test/cert, 862ad75f) that the
// Go controller does not run as the DUT, each with its class and reason;
// the DD onboarding-payload cases, TC-ICDM-6.1, TC-ICDB-1.3 run since phase
// 29b-1 (gocert_dddut_test.go, gocert_icd_test.go), TC-BR-4 since 29b-2
// (gocert_br_test.go). A case the harness
// image lists in a family gets the reason on its row — a manual one
// through manualReason, a not-applicable one beside its PICS —; one no
// family lists is a row of the status page's "Controller role" section.
var controllerRoleCases = map[string]gap{
	// TC-TBRM-3.1.test.ts:133-140: chip's network-manager app as the TH.
	"TBRM/3.1":    {class: classNotSupported, reason: "controller role: the TH is a Thread border router (`--otbr` needs a live OTBR REST API; matter.js runs it against chip's network-manager app only, TC-TBRM-3.1.test.ts:133-140)"},
	"DA/1.9":      pythonDriven("TC-DA-1.9.test.ts:32 TC_DA_1_9.py"),
	"SC/3.5":      pythonDriven("TC-SC-3.5.test.ts:43 TC_SC_3_5.py"),
	"WEBRTCR/2.1": pythonDriven("TC-WEBRTCR-2.1.test.ts:18 TC_WEBRTCR_2_1.py"),
	"WEBRTCR/2.2": pythonDriven("TC-WEBRTCR-2.2.test.ts:18 TC_WEBRTCR_2_2.py"),
	"WEBRTCR/2.3": pythonDriven("TC-WEBRTCR-2.3.test.ts:11 TC_WEBRTCR_2_3.py"),
	"WEBRTCR/2.4": pythonDriven("TC-WEBRTCR-2.4.test.ts:11 TC_WEBRTCR_2_4.py"),
	"WEBRTCR/2.5": pythonDriven("TC-WEBRTCR-2.5.test.ts:30 TC_WEBRTCR_2_5.py"),
	"WEBRTCR/2.6": pythonDriven("TC-WEBRTCR-2.6.test.ts:19 TC_WEBRTCR_2_6.py"),
	"WEBRTCR/2.7": pythonDriven("TC-WEBRTCR-2.7.test.ts:18 TC_WEBRTCR_2_7.py"),
}

// pythonDriven is the row of a case matter.js runs as chip's own Python
// script, its controller answering the script's prompts
// (packages/testing/src/chip/cert/prompt-driven-python-test.ts:41,
// PromptDrivenPythonTest); script names the TS line and the script.
func pythonDriven(script string) gap {
	return gap{class: classNotSupported, reason: "controller role: matter.js drives chip's python script (prompt-driven-python-test.ts:41), the DUT answering its prompts — not ported (" + script + ")"}
}

// gap is one case of a family that is not run, with its class and reason.
type gap struct {
	class  gapClass
	reason string
	// selfSkip marks a case that still runs and must skip itself: its
	// PICS gate is off because the honest PICS says the element is not
	// there (class (b)). The run fails if the case executes anything —
	// the PICS and the case disagree — or does not skip.
	selfSkip bool
}

// edit patches a borrowed case file in the harness container before it runs,
// the way matter.js's chip.testFor(...).edit(...) does
// (support/chip-testing/test/core/*.test.ts). old must occur in the file.
// An edit is only for a case defect (class (c)) and its reason cites the
// matter.js edit or the upstream issue.
type edit struct {
	old, new string
	reason   string
}

// family is one matter.js-style family declaration.
type family struct {
	name string
	// deviceType selects the endpoint the family's cluster lives on: the
	// first endpoint advertising it. Zero targets the node as a whole (the
	// core families), runs with the endpoint-0 PICS slice and passes no
	// --endpoint.
	deviceType uint32
	// endpoints overrides deviceType for single cases (case → device type
	// or pseudo key, see pseudoEndpoints): such a case takes its endpoint
	// and that endpoint's PICS slice from the key (familyrun_test.go
	// deviceTypeOf). A case that needs elements the family's endpoint does
	// not serve is pointed at one that does: TC-S-2.2 at a light with
	// LevelControl and ColorControl, TC-DEM-2.6 at the ESA with SFR.
	endpoints map[string]uint32
	// ownEndpoints cases name every endpoint they touch through PIXIT
	// arguments and keep their own default endpoint (0) for the rest; they
	// run with the PICS slice of deviceType's endpoint but get no
	// --endpoint, which would retarget their root-node steps.
	ownEndpoints map[string]bool
	// exclude maps a case ("2.9", "10.*") to the gap it is.
	exclude map[string]gap
	// args are extra script arguments per case ("*" for every case).
	args map[string][]string
	// edits patch a case file before it runs.
	edits map[string][]edit
	// picsEdits patch a case's descriptor PICS expression, the one the
	// runner decides applicability with (skipNotApplicable), for a case
	// whose own gate names a code no PICS defines; the matching file edit
	// goes in edits. Same rules as edits: a case defect, cited.
	picsEdits map[string]edit //nolint:unused // read by the chiptool-tagged runner (familyrun_test.go)
	// uncommissioned cases get a factory-fresh daemon and commission it
	// themselves, with the daemon's discriminator and passcode.
	uncommissioned map[string]bool
	// multicast cases send group (IPv6 multicast) messages to the DUT; they
	// run only on a host where an up, multicast-capable interface has IPv6,
	// and otherwise skip naming the exact command that enables it.
	multicast map[string]bool
	// labPath cases run their non-CI path, the one a test lab runs
	// against a DUT the case did not start: the endpoint's PICS slice with
	// PICS_SDK_CI_ONLY=0 instead of the CI values, and, uncommissioned,
	// the runner leaves the commissioning to the case's own steps (the
	// daemon's --passcode and --discriminator go in args). For a case whose
	// CI path starts a CHIP app as the DUT and never reaches the daemon.
	labPath map[string]bool //nolint:unused // read by the chiptool-tagged runner (familyrun_test.go)
	// perEndpoint cases run once per endpoint of the daemon, each against
	// that endpoint's PICS slice (TC-IDM-10.4, the PICS checker).
	perEndpoint map[string]bool
	// knownProblems lets a case whose CHIP checker reports only recorded
	// problems pass, and only then (see knownProblems).
	knownProblems map[string]knownProblems
	// timeout raises the per-case budget (defaultCaseTimeout) for a case
	// that waits longer by design.
	timeout map[string]time.Duration
	// cases names the descriptor family whose cases this row runs, when
	// the row is a second run of that family (CNETWIFI runs CNET's); empty
	// is the row's own name. The run's case ids carry the row's name.
	cases string //nolint:unused // read by the chiptool-tagged runner (familyrun_test.go)
	// variant runs every case against the daemon with that composition
	// (daemonVariants: the flags it adds), with PICS generated from that
	// daemon and a commissioned snapshot of its own. Empty is the
	// certified default composition.
	variant string
}

// The Wi-Fi network of the CNETWIFI run: the defaults of the CNET cases'
// PIXIT.CNET.WIFI_1ST_ACCESSPOINT_SSID and _CREDENTIALS (chip 6170af84
// src/app/tests/suites/certification/Test_TC_CNET_4_5.yaml config,
// hex:7A6967626565686F6D65 and hex:70617373776f7264313233).
const (
	wifiSimSSID       = "zigbeehome"
	wifiSimPassphrase = "password123"
	// variantWiFiSim is the daemon with --wifi-sim (daemonVariants).
	variantWiFiSim = "wifi-sim"
)

// The second compositions of the reference daemon (phase 34), one variant
// each (daemonVariants): the families of the clusters only they serve run
// against them, with PICS generated from them (testdata/pics-<variant>/).
const (
	// variantTLS is the daemon with --tls-sim: TlsCertificateManagement
	// and TlsClientManagement on the root (examples/reference-bridge/tls.go).
	variantTLS = "tls-sim"
	// variantCamera is the daemon with --camera-sim and --tls-sim: a
	// Camera and a Chime after chip's camera-app
	// (examples/reference-bridge/camera.go).
	variantCamera = "camera-sim"
	// variantJointFabric is the daemon with --jointfabric-sim: a
	// JointFabricAdministrator endpoint
	// (examples/reference-bridge/jointfabric.go).
	variantJointFabric = "jointfabric-sim"
	// variantThread is the daemon with --otbr against the virtual OTBR
	// agent (script/otbr-virtual-test.sh): the ThreadBorderRouter endpoint
	// with ThreadNetworkDirectory and the root's ThreadNetworkDiagnostics
	// (examples/reference-bridge/otbr.go).
	variantThread = "thread"
)

// Device types only a variant's daemon has (variantDeviceTypes).
const (
	// dtCamera is the --camera-sim Camera: CameraAvStreamManagement,
	// WebRtcTransportProvider and Requestor, PushAvStreamTransport,
	// CameraAvSettingsUserLevelManagement and ZoneManagement.
	dtCamera = 0x0142
	// dtChime is the --camera-sim Chime.
	dtChime = 0x0146
	// dtJointFabricAdministrator is the --jointfabric-sim administrator:
	// JointFabricAdministrator and JointFabricDatastore.
	dtJointFabricAdministrator = 0x0130
	// dtThreadBorderRouter is the --otbr border router:
	// ThreadBorderRouterManagement, ThreadNetworkDiagnostics and
	// ThreadNetworkDirectory.
	dtThreadBorderRouter = 0x0091
)

// variantDeviceTypes are the device types the harness resolves to an
// endpoint of a variant's daemon besides endpointDeviceTypes; a family row
// of that variant may name them (TestFamilyDeviceTypesResolve).
var variantDeviceTypes = map[string][]uint32{
	variantCamera:      {dtCamera, dtChime},
	variantJointFabric: {dtJointFabricAdministrator},
	variantThread:      {dtThreadBorderRouter},
}

// wifiCaseArgs are a Python CNET case's network and endpoint: the network
// the TH commissioned the DUT onto (TC_CNET_4_1.py:37-40 usage), and the
// root's NetworkCommissioning for the cases' endpoint matchers. chip's
// runner keeps --wifi-ssid / --wifi-passphrase only with a ble-wifi or
// nfc-wifi commissioning method (matter/testing/runner.py
// populate_commissioning_args in the harness image), so the commissioned
// DUT is named as one an in-test ble-wifi commissioning would reach, with
// the onboarding values that method requires; nothing commissions before
// the case.
var wifiCaseArgs = []string{
	"--in-test-commissioning-method", "ble-wifi", "--discriminator", "{discriminator}", "--passcode", "{passcode}",
	"--wifi-ssid", wifiSimSSID, "--wifi-passphrase", wifiSimPassphrase, "--endpoint", "0",
}

// knownProblems is a case that fails only on recorded gaps (by_design.md
// or an open finding). The CHIP spec checkers report every problem they
// find, one "problem:" line each; such a case passes only when every
// problem matches a recorded one and every recorded one still occurs — so
// the rest of what the checker verifies keeps failing the case, and a
// fixed gap cannot linger in the table.
type knownProblems struct {
	class    gapClass
	reason   string
	patterns []*regexp.Regexp //nolint:unused // read by the chiptool-tagged runner (familyrun_test.go)
}

// startUpOnBridged classifies the cases that power-cycle the bridge and
// expect a bridged light's start-up attribute applied.
const startUpOnBridged = "the case reboots the DUT and expects the light's StartUpOnOff / StartUpColorTemperatureMireds applied; matter.js applies the start-up attributes only on an endpoint no Aggregator owns (OnOffServer.ts and ColorControlServer.ts initialize: !endpoint.ownerOfType(AggregatorEndpoint)) — a bridge restart is not the bridged device's power cycle — and every light here is bridged"

// Device types of the reference daemon's bridged endpoints
// (examples/reference-bridge/fleet*.go), named for the family table.
const (
	dtOnOffLight      = 0x0100
	dtColorTempLight  = 0x010C
	dtExtColorLight   = 0x010D
	dtSpeaker         = 0x0022
	dtTempSensor      = 0x0302
	dtWaterValve      = 0x0042
	dtModeSelect      = 0x0027
	dtFan             = 0x002B
	dtSmokeCOAlarm    = 0x0076
	dtPump            = 0x0303
	dtFlowSensor      = 0x0306
	dtLaundryWasher   = 0x0073
	dtRVC             = 0x0074
	dtThermostat      = 0x0301
	dtWindowCovering  = 0x0202
	dtDoorLock        = 0x000A
	dtHumiditySensor  = 0x0307
	dtOccupancySensor = 0x0107
	dtContactSensor   = 0x0015
	dtGenericSwitch   = 0x000F
	dtAirPurifier     = 0x002D
	dtClosure         = 0x0230
	// dtAirQualitySensor carries AirQuality and the ten concentration
	// clusters (examples/reference-bridge/fleet_sensors.go).
	dtAirQualitySensor = 0x002C
	// dtElectricalUtilityMeter is the reference daemon's electricity meter
	// (MeterIdentification, brief F).
	dtElectricalUtilityMeter = 0x0511
	// dtTempControlledCabinet is the reference fridge's cabinet part
	// (examples/reference-bridge/fleet_appliances.go): TemperatureControl
	// and RefrigeratorAndTemperatureControlledCabinetMode. The oven's
	// cavity advertises the same device type; the fridge's is the first
	// endpoint that does (see dtOvenCavity).
	dtTempControlledCabinet = 0x0071
	// dtWaterHeater is the reference water heater
	// (examples/reference-bridge/fleet_energy.go): WaterHeaterManagement
	// (EM, TP), WaterHeaterMode and a HEAT Thermostat.
	dtWaterHeater   = 0x050F
	dtRefrigerator  = 0x0070
	dtDishwasher    = 0x0075
	dtMicrowaveOven = 0x0079
	dtOven          = 0x007B
	// dtOvenCavity is no device type: it names the reference oven's
	// cavity part (examples/reference-bridge/fleet_kitchen.go), the
	// TemperatureControlledCabinet with OvenMode, OvenCavityOperationalState
	// and TemperatureControl. A real device type is 16 bits; the key is the
	// Oven's in the high half and the cabinet's in the low, so it collides
	// with none. It resolves to the first 0x0071 endpoint numbered above
	// the Oven's (bridgeProcess.endpointAfter).
	dtOvenCavity = dtOven<<16 | dtTempControlledCabinet
	// dtEnergyEvse is the reference EV charger
	// (examples/reference-bridge/fleet_evse.go): EnergyEvse and
	// EnergyEvseMode; dtDeviceEnergyManagement is its
	// DeviceEnergyManagement part (DeviceEnergyManagement with
	// PA|PFR|STA|PAU|FA|CON and DeviceEnergyManagementMode).
	dtEnergyEvse             = 0x050C
	dtDeviceEnergyManagement = 0x050D
	// dtStateForecastEsa is no device type: it names the reference
	// daemon's standalone DeviceEnergyManagement endpoint
	// (examples/reference-bridge/fleet_esa.go, FeatureMap 0x7C: SFR, STA,
	// PAU, FA, CON), which TC-DEM-2.6 and 2.8 need (TC_DEM_2_6.py:30,
	// TC_DEM_2_8.py:30 at the harness pin run chip's app with
	// --featureSet 0x7c). The charger's part is the first 0x050D endpoint;
	// the ESA is the first one numbered above the grid meter's
	// (bridgeProcess.endpointAfter), so the key is the meter's in the high
	// half and DeviceEnergyManagement's in the low, as dtOvenCavity is.
	dtStateForecastEsa = dtElectricalMeter<<16 | dtDeviceEnergyManagement
	// dtElectricalEnergyTariff is the reference daemon's energy tariff
	// (examples/reference-bridge/fleet_energy_market.go): CommodityPrice,
	// ElectricalGridConditions and CommodityTariff; dtElectricalMeter its
	// grid meter: ElectricalPowerMeasurement, ElectricalEnergyMeasurement
	// and CommodityMetering.
	dtElectricalEnergyTariff = 0x0513
	dtElectricalMeter        = 0x0514
	// dtElectricalSensor is the ElectricalSensor part of the reference
	// EV charger (examples/reference-bridge/fleet_evse.go) and of the grid
	// meter (fleet_energy_market.go): ElectricalPowerMeasurement,
	// ElectricalEnergyMeasurement and PowerTopology. The charger's is the
	// first endpoint advertising it.
	dtElectricalSensor = 0x0510
	// dtLaundryDryer is the reference dryer
	// (examples/reference-bridge/fleet_kitchen.go): LaundryDryerControls.
	dtLaundryDryer = 0x007C
	// dtClosurePanel is the reference daemon's closure panel
	// (examples/reference-bridge/fleet_closure.go): ClosureDimension.
	dtClosurePanel = 0x0231
	// dtSoilSensor is the reference daemon's soil moisture sensor
	// (examples/reference-bridge/fleet_sensors.go): SoilMeasurement.
	dtSoilSensor = 0x0045
	// dtCastingVideoPlayer and dtContentApp are the media pair the daemon
	// serves with --media (examples/reference-bridge/media.go, ADR 0022):
	// chip's tv-app endpoints 1 and 3, which the media YAML cases target
	// (config endpoint: 1, and 3 for ALOGIN-12.1 and APBSC-9.1).
	dtCastingVideoPlayer = 0x0023
	dtContentApp         = 0x0024
)

// endpointDeviceTypes are the device types the harness resolves to an
// endpoint of the reference daemon (familyrun_test.go devicePICS, the first
// endpoint advertising each). A family row or an endpoints entry naming a
// device type outside this list and pseudoEndpoints fails every case of the
// family with "no endpoint advertises device type" (briefs README, first
// wave); TestFamilyDeviceTypesResolve holds the table to it.
var endpointDeviceTypes = []uint32{
	dtOnOffLight, dtColorTempLight, dtExtColorLight, dtSpeaker, dtTempSensor, dtWaterValve, dtModeSelect, dtFan,
	dtSmokeCOAlarm, dtPump, dtFlowSensor, dtLaundryWasher, dtRVC, dtThermostat, dtWindowCovering,
	dtDoorLock, dtHumiditySensor, dtOccupancySensor, dtContactSensor, dtGenericSwitch, dtAirPurifier, dtClosure,
	dtTempControlledCabinet, dtAirQualitySensor, dtWaterHeater, dtElectricalUtilityMeter, dtRefrigerator, dtDishwasher, dtMicrowaveOven, dtOven, dtEnergyEvse, dtDeviceEnergyManagement, dtClosurePanel, dtSoilSensor, dtElectricalEnergyTariff, dtElectricalMeter,
	dtCastingVideoPlayer, dtContentApp, dtElectricalSensor, dtLaundryDryer,
}

// pseudoEndpoints are the keys that name an endpoint by its position: the
// first endpoint advertising the second device type that is numbered above
// the first endpoint advertising the first (bridgeProcess.endpointAfter).
var pseudoEndpoints = map[uint32][2]uint32{
	dtOvenCavity:       {dtOven, dtTempControlledCabinet},
	dtStateForecastEsa: {dtElectricalMeter, dtDeviceEnergyManagement},
}

// chipFamilies is this module's selection of CSA certification families,
// declared the way matter.js declares its own (support/chip-testing/test/
// core/*.test.ts, app-fast/, app-slow/, app-cc/): the whole family runs,
// and every case left out is a gap with its class and reason (ADR 0011).
// The reference set is matter.js's, extended by the family of every cluster
// server the reference daemon exposes; docs/certifiability.md lists the
// families neither runs and why.
var chipFamilies = []family{
	// --- core ---------------------------------------------------------------
	{name: "ACE", multicast: map[string]bool{"1.6": true}},
	{
		name: "ACL",
		// TC-ACL-2.6 reads the AccessControlEntryChanged event the
		// commissioning emitted. Events live in the DUT's memory, as in
		// chip; the harness's commissioned snapshot restarts the daemon,
		// so the case commissions the factory-fresh daemon itself, as the
		// CHIP CI runs it.
		uncommissioned: map[string]bool{"2.6": true},
	},
	{name: "BINFO"},
	{
		name: "BRBINFO", deviceType: dtOnOffLight,
		exclude: map[string]gap{
			"4.1": {classNotSupported, "TC-BRBINFO-4.1 exercises KeepActive of a bridged ICD (BridgedICDSupport, PICS BRBINFO.S.F00=0) against a LIT ICD test app the case starts itself (${LIT_ICD_APP}); the case carries no PICS gate, so it runs regardless (matter.js test/core/BRBINFO.test.ts excludes it for the same reason)", false},
		},
	},
	{name: "CADMIN"},
	{name: "CGEN"},
	{
		name: "CNET",
		// The Wi-Fi cases the descriptor gates on CNET.S.F00 are not
		// applicable through the PICS; these carry no gate at all
		// (matter.js test/core/CNET.test.ts excludes the same set).
		exclude: map[string]gap{
			"4.11": {classOutOfScope, "TC-CNET-4.11 verifies Wi-Fi ConnectNetwork; Wi-Fi commissioning is out of scope (ADR 0011 (d), docs/matterjs-comparison.md), and the case has no PICS gate (CNET.S.F00=0)", false},
			"4.25": {classOutOfScope, "TC-CNET-4.25 verifies Wi-Fi per-device credentials; Wi-Fi commissioning is out of scope and the case skips itself on a node without the Wi-Fi feature", true},
			"4.26": {classOutOfScope, "TC-CNET-4.26 verifies Wi-Fi QueryIdentity (per-device credentials); out of scope with Wi-Fi commissioning, and the case skips itself", true},
			"4.27": {classOutOfScope, "TC-CNET-4.27 verifies Wi-Fi network client identities; out of scope with Wi-Fi commissioning, and the case skips itself", true},
			"4.29": {classOutOfScope, "TC-CNET-4.29 verifies Wi-Fi ConnectNetwork with per-device credentials; out of scope with Wi-Fi commissioning, and the case skips itself once given the endpoint its matcher needs", true},
		},
		args: map[string][]string{
			// These cases' endpoint matchers need --endpoint before they
			// can skip themselves ("The --endpoint flag is required for
			// this test"; matter.js test/core/CNET.test.ts passes it to
			// 4.29). NetworkCommissioning lives on the root node.
			"4.25": {"--endpoint", "0"},
			"4.26": {"--endpoint", "0"},
			"4.27": {"--endpoint", "0"},
			"4.29": {"--endpoint", "0"},
		},
	},
	{
		// CNET a second time, against the daemon with --wifi-sim
		// (NetworkCommissioning with the Wi-Fi feature, CNET.S.F00=1, over
		// the simulated driver, examples/reference-bridge/wifisim.go):
		// brief BB item 4. The default run above keeps the certified
		// composition (Ethernet). The commissioned snapshots carry the
		// Wi-Fi network a TH commissioning over BLE-Wi-Fi would have
		// configured (provisionWiFiNetwork), and the Python cases get it as
		// --wifi-ssid / --wifi-passphrase.
		name: "CNETWIFI", cases: "CNET", variant: variantWiFiSim,
		exclude: map[string]gap{
			// The harness starts each case's daemon from the commissioned
			// snapshot; the network the snapshot configured survives the
			// restart and is rejoined (WiFiNetworkCommissioning.Start, as
			// chip's Linux driver keeps it: NetworkCommissioningWiFiDriver.cpp
			// :77-128, :135-167 at the harness pin), so 4.1, 4.4 and 4.9 run;
			// the simulated driver knows only --wifi-sim-ssid's network.
			"4.11": {classHarness, "TC-CNET-4.11 moves the DUT between two real access points and switches the TH host's own Wi-Fi with them (PIXIT.CNET.WIFI_2ND_ACCESSPOINT_SSID/_CREDENTIALS, TC_CNET_4_11.py:1107-1112, and its host connect steps); the simulated driver has no second network and the harness host no Wi-Fi to switch", false},
			"4.23": {classHarness, "TC-CNET-4.23 steps 0-15 pass on the simulated driver (a wrong SSID is NetworkNotFound, a wrong passphrase AuthFailure, examples/reference-bridge/wifisim.go); step 16 calls the TH's CloseBLEConnection (TC_CNET_4_23.py:498) before its CASE CommissioningComplete, which the harness image's controller, built without BLE, refuses (CHIP_ERROR_UNSUPPORTED_CHIP_FEATURE without CONFIG_NETWORK_LAYER_BLE, src/controller/python/ChipDeviceController-ScriptBinding.cpp:1520-1528): the case assumes a BLE-commissioned DUT", false},
			"4.25": {classNotSupported, "TC-CNET-4.25 verifies Wi-Fi per-device credentials (NetworkCommissioning PDC); the module serves no PDC, and the case skips itself on an endpoint without it", true},
			"4.26": {classNotSupported, "TC-CNET-4.26 verifies QueryIdentity (per-device credentials); no PDC, the case skips itself", true},
			"4.27": {classNotSupported, "TC-CNET-4.27 verifies Wi-Fi network client identities (per-device credentials); no PDC, the case skips itself", true},
			"4.29": {classNotSupported, "TC-CNET-4.29 verifies ConnectNetwork with per-device credentials; no PDC, the case skips itself", true},
		},
		args: map[string][]string{
			"1.4":  wifiCaseArgs,
			"4.1":  wifiCaseArgs,
			"4.3":  wifiCaseArgs,
			"4.4":  wifiCaseArgs,
			"4.9":  wifiCaseArgs,
			"4.11": wifiCaseArgs,
			"4.15": wifiCaseArgs,
			"4.23": wifiCaseArgs,
			"4.25": wifiCaseArgs,
			"4.26": wifiCaseArgs,
			"4.27": wifiCaseArgs,
			"4.29": wifiCaseArgs,
		},
	},
	{
		name: "DA",
		// As matter.js passes them (test/core/DA.test.ts): TC_DA_1_7
		// recommissions and needs the onboarding values; TC_DA_1_2 looks
		// for the CD signing certificates relative to its working directory.
		args: map[string][]string{"*": {
			"--passcode", "{passcode}", "--discriminator", "{discriminator}",
			"--string-arg=cd_cert_dir:/credentials/development/cd-certs",
		}},
	},
	{
		name: "DD",
		// TC_DD_1_16_17.py is a MatterTestUncommissionedDevice case: it
		// parses the onboarding payload and checks the commissionable
		// advertisement of a factory-new device, without commissioning
		// (TC_DD_1_16_17.py:75, :136-138; chip's CI runs it with
		// factory-reset: true and no commissioning method, header lines
		// 21-61). Against the harness's commissioned snapshot the device no
		// longer advertises and the case fails, so both run on a
		// factory-fresh daemon with its own QR and manual code, as TC-SC-7.1
		// does (matter.js test/core/SC.test.ts chip("SC/7.1").uncommissioned()).
		uncommissioned: map[string]bool{"1.16": true, "1.17": true},
	},
	{name: "DESC"},
	{
		name: "DGGEN",
		edits: map[string][]edit{
			// The YAML cases default PIXIT.DGGEN.ENABLEKEY to a key the
			// harness does not give the DUT; the Python cases get the
			// daemon's key through --hex-arg. matter.js aligns the YAML
			// default the same way (test/core/DGGEN.test.ts).
			"2.1": {{"hex:00112233445566778899aabbccddeeff", "hex:000102030405060708090a0b0c0d0e0f", "align the YAML TestEventTrigger key with the daemon's (matter.js DGGEN.test.ts)"}},
			"2.3": {{"hex:00112233445566778899aabbccddeeff", "hex:000102030405060708090a0b0c0d0e0f", "align the YAML TestEventTrigger key with the daemon's (matter.js DGGEN.test.ts)"}},
		},
		// TC-DGGEN-2.1 waits 2 h 5 min and then 1 h 5 min for
		// TotalOperationalHours to move (connectedhomeip#29580); its budget
		// covers both waits.
		timeout: map[string]time.Duration{"2.1": 3*time.Hour + 40*time.Minute},
	},
	{name: "DT"},
	{
		name: "G", deviceType: dtOnOffLight,
		exclude: map[string]gap{
			"2.2": {classHarness, "TC-G-2.2 step 7a (the Groups revision 4 path) writes MaxGroupsPerFabric+1 GroupKeyMap entries and expects Success; chip's own GroupDataProviderImpl::SetGroupKeyAt refuses the entry beyond MaxGroupsPerFabric at the image commit (src/credentials/GroupDataProviderImpl.cpp:1492, CHIP_ERROR_INVALID_LIST_LENGTH), as does matter.js GroupKeyManagementServer #validateGroupKeyMap (ResourceExhausted) and this module", false},
		},
		edits: map[string][]edit{
			// The case's two Groups endpoints are PIXITs; the defaults (1
			// and 2) name the aggregator here, which has no Groups server.
			// They are set in the case's config rather than on the command
			// line, where the YAML runner would take them as strings and
			// compare them with the integer endpoints the DUT reports.
			"2.4": {
				{"Groups.Endpoint1: 1\n", "Groups.Endpoint1: {ep:0x0100}\n", "PIXIT.G.ENDPOINT1: the on/off light"},
				{"Groups.Endpoint2: 2\n", "Groups.Endpoint2: {ep:0x010C}\n", "PIXIT.G.ENDPOINT2: the colour-temperature light"},
			},
		},
		// TC-G-2.4's KeySetWrite and GroupKeyMap steps address
		// GroupKeyManagement on the root node through the config default.
		ownEndpoints: map[string]bool{"2.4": true},
	},
	{name: "GC", multicast: map[string]bool{"2.8": true}},
	{name: "GRPKEY"},
	{
		name: "IDM",
		args: map[string][]string{
			// The bridge mounts Identify on every bridged endpoint, including
			// device types that do not list it; matter.js passes the same flag
			// for its own test apps (test/core/IDM.test.ts).
			"10.5": {"--bool-arg", "fail_on_extra_clusters:False"},
		},
		perEndpoint: map[string]bool{"10.4": true},
	},
	{name: "OPCREDS"},
	{name: "RR"},
	{
		name:      "SC",
		multicast: map[string]bool{"5.2": true, "5.3": true},
		// TC-SC-4.1 and TC-SC-4.3 follow the SRV target with a bare
		// host-name AAAA query; the daemon's mDNS responder answers it
		// (mdns/answer.go). Both run unconditionally: they failed
		// on an IPv6-capable runner because that answer was missing, not
		// because of the host.
		// TC_SC_7_1 checks the commissionable advertisement of a factory-new
		// device and commissions it (matter.js test/core/SC.test.ts
		// chip("SC/7.1").uncommissioned()).
		uncommissioned: map[string]bool{"7.1": true},
		edits: map[string][]edit{
			// TC-SC-7.1 rejects CHIP's default discriminator and passcode,
			// which the harness runs the daemon with; a product sets its
			// own (docs/certifiability.md, product obligations). matter.js
			// disables the same two checks (test/core/SC.test.ts).
			"7.1": {
				{", 3840,", ", 0000,", "the harness's daemon uses the default discriminator (matter.js SC.test.ts)"},
				{", 20202021,", ", 00000000,", "the harness's daemon uses the default passcode (matter.js SC.test.ts)"},
			},
		},
	},
	{name: "SM"},
	// The root's optional clusters. TimeSynchronization and DiagnosticLogs
	// are mounted; IcdManagement and the OTA Software Update Requestor are
	// not (examples/reference-bridge/wiring.go buildRootClusters says why),
	// so the ICDM and SU cases are not applicable through the device's own
	// PICS. Every DLOG case and the BIND cases are manual in the image's
	// descriptor; BIND tests the binding client (BIND.C), a role the module
	// does not take.
	{name: "TIMESYNC"},
	{name: "DLOG"},
	{name: "ICDM"},
	// SU and BDX: the software-update requestor cases take chip's OTA
	// provider app and a staged image (TC_SU_2_2.py:39-41 at the harness
	// pin: provider_app_path, ota_image, ota_provider_port), which the
	// runner prepares in the container for every case whose descriptor
	// arguments name them (otaPlumbing), with the case's own --timeout as
	// budget, and stops the provider and collects its logs after each
	// (otaCaseTimeout, otaCleanupCommand, otaProviderLogsScript). The
	// requestor is mounted on the root (brief N); the BDX cases at the pin
	// are YAML with every step disabled (Test_TC_BDX_*.yaml), manual in
	// the descriptor.
	{
		name: "SU",
		edits: map[string][]edit{
			// Step 3 guards the 180 s DelayedActionTime on the UpdateState
			// subscription and then waits for Downloading on the same
			// queue. The guard consumes every report until its full
			// duration has elapsed, the last 5 s unchecked (tolerance zone,
			// event_attribute_reporting.py:704-739 at the harness pin), and
			// the test anchors the window on *its* receipt of
			// DelayedOnQuery, after the DUT started its timer. A DUT that
			// re-queries at exactly DelayedActionTime, as matter.js
			// schedules it (OtaSoftwareUpdateRequestorServer.ts:676-680,
			// max(delayedActionTime, minimumQueryInterval)), reports
			// Downloading inside that zone, where the guard swallows it,
			// and Phase C waits for a report that already went by (run
			// 38149548973: report at 15:21:57.237, Phase C from
			// 15:21:57.237). The guard ends at the tolerance boundary
			// instead, so Phase C sees the report; the step's own elapsed
			// assertion already allows it (TC_SU_2_2.py:864-867,
			// `elapsed_s3 >= min_interval_s3 - tolerance_s3`).
			"2.2": {
				{"            duration_sec=min_interval_s3,", "            duration_sec=min_interval_s3 - tolerance_s3,", "end the step-3 guard at the tolerance boundary so the Downloading report reaches Phase C (TC_SU_2_2.py:846-850)"},
				{"            tolerance_sec=tolerance_s3,", "            tolerance_sec=0.0,", "the guard's tolerance zone is now outside its duration (TC_SU_2_2.py:846-850)"},
			},
		},
	},
	{name: "BDX"},
	{name: "BIND"},
	// The root's label, localization and diagnostics clusters (all
	// optional on RootNode), mounted by buildRootLabelsAndDiagnostics in
	// examples/reference-bridge/root_optional.go.
	{name: "FLABEL"},
	{name: "ULABEL"},
	{name: "LCFG"},
	{name: "LTIME"},
	{name: "LUNIT"},
	{name: "DGSW"},
	{name: "DGETH"},
	// WiFiNetworkDiagnostics on the root, from the daemon's simulated Wi-Fi
	// link (--wifi, examples/reference-bridge/wifi.go; brief AM), which the
	// suite starts every daemon with (startBridgeWith). TC-DGWIFI-2.2 is
	// manual in the descriptor (every step `disabled: true`); 3.2 is the
	// DUT-as-client case (DGWIFI.C, false in testdata/reference-bridge.pics).
	{name: "DGWIFI"},
	// The node clusters of brief J. Actions is on the Aggregator
	// (examples/reference-bridge/fleet_node.go); every step of
	// Test_TC_ACT_2_1/2_2/3_2.yaml is manual (`disabled: true`) at the
	// harness pin. PowerSourceConfiguration is on the root, where
	// Test_TC_PSCFG_2_1.yaml reads Sources (config endpoint: 0).
	{name: "ACT"},
	{name: "PSCFG"},
	{
		// EcosystemInformation is on the air quality sensor, but both
		// CHIP cases bring their own DUT: a fabric-sync admin and bridge
		// pair the runner starts itself (TC_ECOINFO_2_1.py and
		// TC_ECOINFO_2_2.py CI arguments, header lines 24-27 at the
		// harness pin: "app: examples/fabric-admin/scripts/fabric-sync-app.py").
		name: "ECOINFO",
		exclude: map[string]gap{
			"2.1": {classHarness, "TC_ECOINFO_2_1.py runs against the fabric-sync app it starts itself (CI arguments, header lines 24-27: app: examples/fabric-admin/scripts/fabric-sync-app.py), not against a commissioned DUT", false},
			"2.2": {classHarness, "TC_ECOINFO_2_2.py runs against the fabric-sync app it starts itself (CI arguments, header lines 24-27: app: examples/fabric-admin/scripts/fabric-sync-app.py), not against a commissioned DUT", false},
		},
	},
	// --- application clusters ----------------------------------------------
	{name: "ACFREMON", deviceType: dtAirPurifier},
	{name: "BOOL", deviceType: dtContactSensor},
	{name: "BOOLCFG", deviceType: dtContactSensor},
	// The concentration families, one case each (TC-<FAM>-2.1), all on
	// the reference AirQualitySensor. CDOCONC is CarbonDioxide and
	// CMOCONC CarbonMonoxide (connectedhomeip
	// src/app/tests/suites/certification/Test_TC_CDOCONC_2_1.yaml:29,
	// Test_TC_CMOCONC_2_1.yaml:22).
	{name: "CDOCONC", deviceType: dtAirQualitySensor},
	{name: "CMOCONC", deviceType: dtAirQualitySensor},
	// AIRQUAL, PMICONC (PM2.5) and PMKCONC (PM10): the AirQuality, Pm25 and
	// Pm10 clusters the air quality sensor also serves (connectedhomeip
	// Test_TC_AIRQUAL_2_1.yaml, Test_TC_PMICONC_2_1.yaml,
	// Test_TC_PMKCONC_2_1.yaml).
	{name: "AIRQUAL", deviceType: dtAirQualitySensor},
	{name: "PMICONC", deviceType: dtAirQualitySensor},
	{name: "PMKCONC", deviceType: dtAirQualitySensor},
	{name: "FLDCONC", deviceType: dtAirQualitySensor},
	{name: "NDOCONC", deviceType: dtAirQualitySensor},
	{name: "OZCONC", deviceType: dtAirQualitySensor},
	{name: "PMHCONC", deviceType: dtAirQualitySensor},
	{name: "RNCONC", deviceType: dtAirQualitySensor},
	{name: "TVOCCONC", deviceType: dtAirQualitySensor},
	// SOIL: SoilMeasurement on the soil sensor (connectedhomeip
	// TC_SOIL_2_1.py, TC_SOIL_2_2.py; 2.2 moves the reading through the
	// app pipe's SetSimulatedSoilMoisture).
	{name: "SOIL", deviceType: dtSoilSensor},
	{
		name: "CLCTRL", deviceType: dtClosure,
		// TC_CLCTRL_5_1 gates on "CLCTRL.S.C00", a code no PICS defines:
		// an accepted command's code is CLCTRL.S.C00.Rsp (CHIP's own
		// derivation, matter/testing/pics.py, and every other case of the
		// family). As written the case never runs against a DUT whose Stop
		// is accepted; the gate is corrected, the steps are unchanged.
		picsEdits: map[string]edit{
			"5.1": {"CLCTRL.S.C00", "CLCTRL.S.C00.Rsp", "TC_CLCTRL_5_1 gates on the undefined code CLCTRL.S.C00; Stop is CLCTRL.S.C00.Rsp"},
		},
		edits: map[string][]edit{
			"5.1": {{`"CLCTRL.S", "CLCTRL.S.C00"`, `"CLCTRL.S", "CLCTRL.S.C00.Rsp"`, "TC_CLCTRL_5_1 gates on the undefined code CLCTRL.S.C00; Stop is CLCTRL.S.C00.Rsp"}},
		},
	},
	// CLDIM sets no PIXIT and sends no test event trigger: the CHIP cases
	// at the harness pin (TC_CLDIM_*.py) name none; they run against the
	// closure panel, whose features are the closure-app panel's.
	{name: "CLDIM", deviceType: dtClosurePanel},
	{
		// The extended colour light serves every ColorControl feature (XY,
		// CT, HS, EHUE, CL), as matter.js's own CHIP test endpoint does
		// (support/chip-testing/src/devices/ExtendedColorLightEndpoint.ts),
		// so every CC case runs against the features it gates on; the
		// colour-temperature light keeps CT-only ColorControl under the
		// LVL family's coupling cases and TC-S-2.2 / 2.4.
		name: "CC", deviceType: dtExtColorLight,
		exclude: map[string]gap{
			// matter.js test/app-cc/CC.1.test.ts excludes the same three.
			"9.1": {classHarness, "TC-CC-9.1 asserts transition results more exactly than a conforming device must meet; matter.js excludes it (test/app-cc/CC.1.test.ts)", false},
			"9.2": {classHarness, "TC-CC-9.2 asserts transition results more exactly than a conforming device must meet; matter.js excludes it (test/app-cc/CC.1.test.ts)", false},
			"9.3": {classHarness, "TC-CC-9.3 asserts transition results more exactly than a conforming device must meet; matter.js excludes it (test/app-cc/CC.1.test.ts)", false},
			"6.5": {classHarness, startUpOnBridged, false},
		},
	},
	{
		name: "DRLK", deviceType: dtDoorLock,
		edits: map[string][]edit{
			// TC_DRLK_2_5.py and TC_DRLK_2_9.py address the lock as endpoint 1
			// (src/python_testing/TC_DRLK_2_5.py:184, TC_DRLK_2_9.py:363 at
			// the image commit) rather than the case's --endpoint; the
			// daemon's lock is a bridged endpoint.
			"2.5": {{"self.app_cluster_endpoint = 1\n", "self.app_cluster_endpoint = {ep:0x000A}\n", "the lock is the daemon's bridged door lock, not endpoint 1"}},
			"2.9": {{"self.app_cluster_endpoint = 1\n", "self.app_cluster_endpoint = {ep:0x000A}\n", "the lock is the daemon's bridged door lock, not endpoint 1"}},
		},
	},
	// EPREF runs on the thermostat, which serves EnergyPreference (BALA).
	{name: "EPREF", deviceType: dtThermostat},
	// EWATERHTR 2.2 and 2.3 drive the tank through the
	// WaterHeaterManagement test event triggers (0x0094...), which the
	// daemon's tank model answers as connectedhomeip's water-heater app
	// does (examples/reference-bridge/fleet_energy.go).
	{name: "EWATERHTR", deviceType: dtWaterHeater},
	// MTRID: MeterIdentification on the electricity meter (connectedhomeip
	// TC_MTRID_2_1.py, TC_MTRID_3_1.py).
	{name: "MTRID", deviceType: dtElectricalUtilityMeter},
	// DEM, DEMM, EEVSE and EEVSEM run on the EV charger and its
	// DeviceEnergyManagement part; TC_EEVSE_2_x and TC_DEM_2_x drive them
	// through the EnergyEvse (0x0099...) and DeviceEnergyManagement
	// (0x0098...) test event triggers, which the charger answers as
	// connectedhomeip's evse-app does (examples/reference-bridge/
	// fleet_evse.go).
	// SETRF and COMMTR run on the energy tariff and the grid meter;
	// TC_SETRF_2_2/2_3 and TC_COMMTR_3_1 drive them through the
	// CommodityTariff (0x0700...) and CommodityMetering (0x0b07...) test
	// event triggers, which the daemon answers as connectedhomeip's
	// energy-gateway app does (examples/reference-bridge/
	// fleet_energy_market.go).
	{name: "SETRF", deviceType: dtElectricalEnergyTariff},
	{name: "SEPR", deviceType: dtElectricalEnergyTariff},
	{name: "COMMTR", deviceType: dtElectricalMeter},
	// EEM and EPM run on the grid meter too; TC_EEM_2_2 and TC_EPM_2_2
	// drive its readings through the EnergyReporting (0x0091...) test
	// event triggers, which the meter answers as connectedhomeip's
	// energy-management app's FakeReadings does
	// (examples/reference-bridge/fleet_energy_market.go).
	{name: "EEM", deviceType: dtElectricalMeter},
	{name: "EPM", deviceType: dtElectricalMeter},
	{
		name: "PWRTL", deviceType: dtElectricalSensor,
		exclude: map[string]gap{
			"2.2": {classNotSupported, "TC-PWRTL-2.2 needs PowerTopology's ElectricalCircuit feature (has_feature(kElectricalCircuit), TC_PWRTL_2_2.py:76 at 6170af84), which the model at the matter.js pin does not define (parity/schema.json PowerTopology: NODE, TREE, SET, DYPF); the case skips itself (matter.js test/core/PWRTL.test.ts excludes it for the same reason)", true},
		},
	},
	// EGC: ElectricalGridConditions on the energy tariff
	// (fleet_energy_market.go, FORE); TC_EGC_2_2/2_3 drive it through the
	// 0x00A0... triggers (TC_EGCTestBase.py:36-37 at the harness pin),
	// which the daemon answers as the energy-gateway app does.
	{
		name: "EGC", deviceType: dtElectricalEnergyTariff,
		// TC_EGC_2_3.py:61-64 gates on "EGC.F00", a code no PICS defines:
		// a cluster feature's PICS code is <PICS>.S.F<bit>, as the case
		// family's sibling gates (TC_EGC_2_1.py:64 "EGC.S") and every
		// device-generated slice name it (testdata/pics), so the honest
		// PICS would leave the Forecasting case unrun on a device that
		// serves FORE.
		picsEdits: map[string]edit{
			"2.3": {"EGC.F00", "EGC.S.F00", "TC_EGC_2_3 gates on the undefined code EGC.F00 (TC_EGC_2_3.py:63); ElectricalGridConditions Forecasting is EGC.S.F00"},
		},
		edits: map[string][]edit{
			"2.3": {{`"EGC.F00"`, `"EGC.S.F00"`, "TC_EGC_2_3 gates on the undefined code EGC.F00 (TC_EGC_2_3.py:63); ElectricalGridConditions Forecasting is EGC.S.F00"}},
		},
	},
	// DEM/2.6 and 2.8 need StateForecastReporting (DEM.S.F02), which the
	// charger's part does not have; they run on the standalone ESA
	// (fleet_esa.go, FeatureMap 0x7C as TC_DEM_2_6.py:30 and
	// TC_DEM_2_8.py:30 start chip's app) with its own PICS slice. 2.9
	// (PFR & SFR) and 2.11 (F07) stay not applicable on both
	// (BD-chip-DeviceEnergyManagement-EnergyEvse-RulesInServer).
	{
		name: "DEM", deviceType: dtDeviceEnergyManagement,
		endpoints: map[string]uint32{"2.6": dtStateForecastEsa, "2.8": dtStateForecastEsa},
	},
	{name: "DEMM", deviceType: dtDeviceEnergyManagement},
	{
		name: "EEVSE", deviceType: dtEnergyEvse,
		edits: map[string][]edit{
			// Step 9 writes UserMaximumChargeCurrent to endpoint 1
			// (TC_EEVSE_2_2.py:298 at the harness pin) — the EVSE endpoint
			// of the energy-management app the case was written against
			// (its header runs with --endpoint 1, :35) — while every
			// other step uses --endpoint. On this daemon endpoint 1 is the
			// first bridged light, so the write lands on an endpoint
			// without the cluster (UNSUPPORTED_CLUSTER). matter.js's
			// chip-testing does not run the EEVSE family.
			"2.2": {{"self.write_user_max_charge(1, user_max_charge_current)", "self.write_user_max_charge(self.get_endpoint(), user_max_charge_current)", "TC-EEVSE-2.2 step 9 hard-codes endpoint 1 for the UserMaximumChargeCurrent write (TC_EEVSE_2_2.py:298); the case's own endpoint is the EVSE"}},
		},
	},
	{name: "EEVSEM", deviceType: dtEnergyEvse},
	{name: "FAN", deviceType: dtFan},
	{name: "HEPAFREMON", deviceType: dtAirPurifier},
	{
		name: "FLW", deviceType: dtFlowSensor,
		exclude: map[string]gap{
			"2.2": {classHarness, "TC-FLW-2.2 has an operator change the measured value between two reads (a UserPrompt under FLW.M.FlowChange); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in", false},
		},
	},
	{
		name: "I", deviceType: dtOnOffLight,
		exclude: map[string]gap{
			"2.4": {classHarness, "TC-I-2.4 checks the Q-quality reporting of IdentifyTime added in Matter 1.4.2 against an expectation chip has not merged yet (connectedhomeip#42128); matter.js excludes it until then (test/app-slow/I.test.ts)", false},
		},
	},
	{name: "LVL", deviceType: dtColorTempLight},
	{name: "LWM", deviceType: dtLaundryWasher},
	{
		name: "WASHERCTRL", deviceType: dtLaundryWasher,
		exclude: map[string]gap{
			"2.2": {classHarness, "TC-WASHERCTRL-2.2 step 6 writes NumberOfRinses 4, a value NumberOfRinsesEnum does not define, and expects INVALID_IN_STATE: chip decodes it to kUnknownEnumValue and refuses it as not a supported rinse (LaundryWasherControlsCluster.cpp:132-145 at 6170af84). matter.js refuses an undefined enum value as CONSTRAINT_ERROR before any server logic (UnknownEnumValueError, packages/protocol/src/action/errors.ts:99-103, thrown by conformance-compiler.ts:859-862 at 862ad75f), and this module answers as matter.js does", false},
		},
	},
	{name: "DRYERCTRL", deviceType: dtLaundryDryer},
	{
		name: "MOD", deviceType: dtModeSelect,
		edits: map[string][]edit{
			// The mode TC-MOD-2.1 changes to is a PIXIT whose default (4)
			// is a mode of CHIP's sample app; the daemon's selector offers
			// 0-2 (examples/reference-bridge/fleet.go newDemoSelector). Set
			// in the config, not on the command line, where the runner
			// would compare the string "2" with the integer CurrentMode.
			"2.1": {{"defaultValue: 4\n", "defaultValue: 2\n", "PIXIT NewMode: a mode of the daemon's selector"}},
		},
	},
	{name: "OCC", deviceType: dtOccupancySensor},
	{
		name: "OO", deviceType: dtOnOffLight,
		edits: map[string][]edit{
			// chip caps OffWaitTime at 215 where the specification sets no
			// such bound, and expects 30 s exactly; matter.js relaxes both
			// (test/app-slow/OO.test.ts).
			"2.3": {
				{"maxValue: 215", "maxValue: 300", "chip's OffWaitTime cap is not in the specification (matter.js OO.test.ts)"},
				{"value: 30000", "value: 30500", "a 30 s wait measured to the millisecond (matter.js OO.test.ts)"},
			},
		},
		exclude: map[string]gap{
			"2.4": {classHarness, startUpOnBridged, false},
			"2.8": {classHarness, "TC-OO-2.8 expects OnTime and OffWaitTime reported only on a change larger than 10 or to 0 — chip's OnOffLightingCluster.cpp SetOnTime/SetOffWaitTime (kValueDeltaReportTrigger), an SDK choice: Matter 1.6.1 gives neither attribute the Q quality (matter.js on-off.element.ts), and matter.js OnOffServer reports every countdown tick, as the daemon now does", false},
		},
	},
	{name: "OPSTATE", deviceType: dtLaundryWasher},
	{name: "PCC", deviceType: dtPump},
	{name: "PS", deviceType: dtSmokeCOAlarm},
	{
		name: "RH", deviceType: dtHumiditySensor,
		exclude: map[string]gap{
			"2.2": {classHarness, "TC-RH-2.2 has an operator change the measured value between two reads (a UserPrompt under RH.M.ManuallyControlled); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in", false},
		},
	},
	{
		name: "RVCCLEANM", deviceType: dtRVC,
		args: map[string][]string{
			// The clean modes are PIXITs; the case's CI values name the
			// rvc-app's (1-3). The daemon's vacuum offers Vacuum (0) and
			// Mop (1) (examples/reference-bridge/fleet_appliances.go).
			"2.1": {"--int-arg", "PIXIT.RVCCLEANM.MODE_CHANGE_OK:1", "--int-arg", "PIXIT.RVCCLEANM.MODE_CHANGE_FAIL:1"},
		},
	},
	{name: "RVCOPSTATE", deviceType: dtRVC},
	// TCCM and TCTL set no PIXIT: connectedhomeip TC_TCCM_1_2.py and
	// TC_TCTL_2_3.py (at the harness pin) and Test_TC_TCTL_*.yaml name none;
	// Test_TC_TCCM_2_1.yaml names PIXIT.TCCM.MODE_CHANGE_* only in its
	// steps' verification text, not in its config.
	{name: "TCCM", deviceType: dtTempControlledCabinet},
	{name: "TCTL", deviceType: dtTempControlledCabinet},
	// The kitchen appliances (examples/reference-bridge/fleet_kitchen.go,
	// fleet_appliances.go): DishwasherAlarm on the dishwasher,
	// RefrigeratorAlarm on the fridge, OvenCavityOperationalState and
	// OvenMode on the oven's cavity, MicrowaveOvenControl and
	// MicrowaveOvenMode on the microwave.
	{name: "DISHALM", deviceType: dtDishwasher},
	{name: "REFALM", deviceType: dtRefrigerator},
	{name: "OVENOPSTATE", deviceType: dtOvenCavity},
	{name: "OTCCM", deviceType: dtOvenCavity},
	{name: "MWOCTRL", deviceType: dtMicrowaveOven},
	{name: "MWOM", deviceType: dtMicrowaveOven},
	{name: "RVCRUNM", deviceType: dtRVC},
	{
		name: "S", deviceType: dtOnOffLight, multicast: map[string]bool{"2.3": true},
		args: map[string][]string{
			// TC-S-2.6 fills the scene table of three fabrics, one AddScene
			// per 1 s subscription report, inside the harness's 90 s default
			// test budget — sized for chip's 16-entry table. matter.js's
			// table, which the module mirrors, holds 128 (3 x 63 quota,
			// bounded by the table): ~165 s of reports. The budget is the
			// harness's, the assertions are unchanged.
			"2.6": {"--timeout", "600"},
		},
		// TC-S-2.2 and 2.4 configure a scene over every scene-capable
		// cluster of the endpoint (OnOff, LevelControl, ColorControl): the
		// colour-temperature light has all three, the on/off light only
		// OnOff.
		endpoints: map[string]uint32{"2.2": dtColorTempLight, "2.4": dtColorTempLight},
		edits: map[string][]edit{
			// matter.js test/app-fast/S.test.ts: chip expects the unset
			// values of a ScenesManagement server that no longer tracks
			// CurrentScene, CurrentGroup and SceneValid; revision 1, which
			// matter.js and this module implement, still tracks them.
			"2.2": {
				{"CurrentScene: 0xFF,", "CurrentScene: 0x01,", "revision 1 tracks CurrentScene (matter.js S.test.ts)"},
				{"CurrentGroup: 0x00,", "CurrentGroup: G1,", "revision 1 tracks CurrentGroup (matter.js S.test.ts)"},
				{"SceneValid: false,", "SceneValid: true,", "revision 1 tracks SceneValid (matter.js S.test.ts)"},
			},
		},
	},
	// SEAR sets no PIXIT: TC_SEAR_*.py at the harness pin name none; the
	// vacuum's ServiceArea serves MAPS and PROG (fleet_servicearea.go).
	{name: "SEAR", deviceType: dtRVC},
	{name: "SMOKECO", deviceType: dtSmokeCOAlarm},
	{
		name: "SWTCH", deviceType: dtGenericSwitch,
		exclude: map[string]gap{
			"2.2": {classNotSupported, "TC-SWTCH-2.2 runs only on a latching switch (PICS SWTCH.S.F00=0: the daemon's switch is momentary)", true},
			"2.6": {classNotSupported, "TC-SWTCH-2.6 runs only with ActionSwitch (PICS SWTCH.S.F05=0): the daemon's button serves MomentarySwitchRelease, which ActionSwitch excludes (MSR conformance \"[MS & !AS]\", switch.element.ts)", true},
		},
	},
	{
		name: "TMP", deviceType: dtTempSensor,
		exclude: map[string]gap{
			"2.2": {classHarness, "TC-TMP-2.2 has an operator change the measured value between two reads (a UserPrompt under TMP.M.ManuallyControlled); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in", false},
		},
	},
	{name: "TSTAT", deviceType: dtThermostat},
	{name: "TSUIC", deviceType: dtThermostat},
	{
		name: "VALCC", deviceType: dtWaterValve,
		exclude: map[string]gap{
			"3.2": {classNotSupported, "TC-VALCC-3.2 runs only with the Level feature (PICS VALCC.S.F01=0: the daemon's valve is open/closed)", true},
			"3.3": {classNotSupported, "TC-VALCC-3.3 runs only with DefaultOpenLevel (PICS VALCC.S.A0006=0)", true},
		},
	},
	{name: "WHM", deviceType: dtWaterHeater},
	{name: "WNCV", deviceType: dtWindowCovering},
	// --- media (ADR 0022) ---------------------------------------------------
	// The media families run against the daemon's --media pair
	// (examples/reference-bridge/media.go), a simulation of chip's tv-app:
	// the casting video player for every family but the content app's
	// ApplicationBasic and AccountLogin, whose cases target the tv-app's
	// content app (Test_TC_APBSC_9_1.yaml and Test_TC_ALOGIN_12_1.yaml,
	// config endpoint: 3). The image's descriptor lists the cases whose
	// every step is disabled as manual; the
	// Python cases gate on PICS codes past the model at the pin
	// (MEDIAPLAYBACK.S.A000b/A000c, CONTENTLAUNCHER.S.F05/F06/A0003,
	// ALOGIN.S.F00) and are not applicable.
	{name: "ALOGIN", deviceType: dtContentApp},
	{name: "APBSC", deviceType: dtContentApp},
	{
		name: "APPLAUNCHER", deviceType: dtCastingVideoPlayer,
		edits: map[string][]edit{
			// The ApplicationBasic endpoint the StopApp / HideApp steps read
			// is a config variable naming the tv-app's content app (3,
			// Test_TC_APPLAUNCHER_3_8.yaml:18 and _3_9.yaml:18); here the
			// content app is the daemon's ContentApp endpoint. Set in the
			// config, as TC-G-2.4's endpoints are.
			"3.8": {{"ApplicationBasic.Endpoint: 3\n", "ApplicationBasic.Endpoint: {ep:0x0024}\n", "PIXIT: the ApplicationBasic endpoint is the daemon's content app"}},
			"3.9": {{"ApplicationBasic.Endpoint: 3\n", "ApplicationBasic.Endpoint: {ep:0x0024}\n", "PIXIT: the ApplicationBasic endpoint is the daemon's content app"}},
		},
	},
	{name: "APPOBSERVER", deviceType: dtCastingVideoPlayer},
	{name: "AUDIOOUTPUT", deviceType: dtCastingVideoPlayer},
	{name: "CHANNEL", deviceType: dtCastingVideoPlayer},
	{name: "CONTENTLAUNCHER", deviceType: dtCastingVideoPlayer},
	{name: "KEYPADINPUT", deviceType: dtCastingVideoPlayer},
	{name: "LOWPOWER", deviceType: dtCastingVideoPlayer},
	{name: "MEDIAINPUT", deviceType: dtCastingVideoPlayer},
	{
		name: "MEDIAPLAYBACK", deviceType: dtCastingVideoPlayer,
		exclude: map[string]gap{
			"6.8": {classHarness, "TC-MEDIAPLAYBACK-6.8 step 1 (and the reads after DeactivateTextTrack) expects ActiveTextTrack to read a TrackStruct with an empty ID and a null TrackAttributes (Test_TC_MEDIAPLAYBACK_6_8.yaml:26-31), the tv-app's empty track under chip's revision 3; the model at the pin makes TrackAttributes mandatory and not nullable (matter.js media-playback.element.ts:206-208) and ActiveTextTrack itself nullable (:71-74), so the daemon reads null while no text track is active", false},
		},
	},
	{name: "TGTNAV", deviceType: dtCastingVideoPlayer},
	{name: "WAKEONLAN", deviceType: dtCastingVideoPlayer},
	// MESS: Messages (cluster/core) on the video player, after the
	// tv-app's endpoint 1 (tv-app.matter:4407-4424; TC_MESS_1_1.py CI
	// arguments: app ${TV_APP}, --endpoint 1).
	{
		name: "MESS", deviceType: dtCastingVideoPlayer,
		exclude: map[string]gap{
			"1.1": {classNotSupported, "TC-MESS-1.1 runs only on an endpoint with SpokenMessages or AudioMessages (TC_MESS_1_1.py:76, TC_MESSTestBase.py:42-50); the player's Messages, like the tv-app's (featureMap 0, tv-app.matter:4419), has neither, and the case skips itself", true},
		},
	},

	// --- The second compositions (phase 34) ---------------------------------
	//
	// TLSCERT and TLSCLIENT: the root's TlsCertificateManagement and
	// TlsClientManagement (node.Config.TLS) of the --tls-sim daemon
	// (examples/reference-bridge/tls.go); chip runs them against its
	// all-clusters app's root (TC_TLSCERT_2_1.py and TC_TLSCLIENT_2_1.py
	// CI arguments, --endpoint 0, at the harness pin).
	{
		name: "TLSCERT", variant: variantTLS,
		exclude: map[string]gap{
			// notes/parity/matter_behaviour_findings.md, "Certification
			// harness": TC-TLSCERT-2.2, TC-TLSCERT-2.9.
			"2.2": {classDefect, "TC-TLSCERT-2.2 step 5: the module's reads of ProvisionedRootCertificates and ProvisionedClientCertificates serve the full certificates on every transport (BD-chip-TlsCertificateManagement-RulesInServer, owner decision 2026-10-28, after matter.js's generated behaviour); chip encodes the ids alone outside a large-payload transport (TLSCertificateManagementCluster.cpp:136-166 at the harness pin), and the case reads over MRP expecting no certificate", false},
			"2.9": {classDefect, "TC-TLSCERT-2.9 step 13: the module's reads of ProvisionedRootCertificates and ProvisionedClientCertificates serve the full certificates on every transport (BD-chip-TlsCertificateManagement-RulesInServer, owner decision 2026-10-28, after matter.js's generated behaviour); chip encodes the ids alone outside a large-payload transport (TLSCertificateManagementCluster.cpp:136-166 at the harness pin), and the case reads over MRP expecting no certificate; two such client certificates exceed one MRP message, so the read answers RESOURCE_EXHAUSTED", false},
		},
	},
	{name: "TLSCLIENT", variant: variantTLS},
	// The camera families: the --camera-sim Camera and Chime after chip's
	// camera-app (examples/reference-bridge/camera.go), which chip's CI
	// runs them against (the cases' CI arguments: app ${CAMERA_APP},
	// --endpoint 1). The image's descriptor files the WEBRTCP cases under
	// two names, WEBRTCP and WebRTCP (the case titles' spelling); each is
	// a row. A case whose CI starts the camera-app with
	// --camera-deferred-offer or --camera-test-videosrc starts the daemon
	// with the same switch (daemonAppArgs).
	{
		name: "WEBRTCP", deviceType: dtCamera, variant: variantCamera,
		edits: map[string][]edit{
			// Step 2 sends its ProvideOffers to endpoint 1, step 3 to the
			// case's endpoint (TC_WEBRTCP_2_16.py:159, :173).
			"2.16": {{"cmd=provide_offer_cmd,\n                endpoint=1,", "cmd=provide_offer_cmd,\n                endpoint=endpoint,", "step 2 addresses endpoint 1, the camera-app's camera, where step 3 uses the case's endpoint (TC_WEBRTCP_2_16.py:159, :173); the daemon's camera is a bridged endpoint"}},
		},
	},
	{name: "WebRTCP", deviceType: dtCamera, variant: variantCamera},
	{name: "AVSM", deviceType: dtCamera, variant: variantCamera},
	{name: "AVSUM", deviceType: dtCamera, variant: variantCamera},
	{name: "ZONEMGMT", deviceType: dtCamera, variant: variantCamera},
	{
		name: "PAVST", deviceType: dtCamera, variant: variantCamera,
		exclude: map[string]gap{
			"2.3": {classHarness, "TC-PAVST-2.3 step 25 allocates a motion transport with MotionSensitivity 11 on a camera with PERZONESENS and expects chip's INVALID_COMMAND (TC_PAVST_2_3.py:435-443, PushAVStreamTransportLogic.cpp:573-574); the module validates the request's fields first, as matter.js does, and answers CONSTRAINT_ERROR — matter.js answers a constraint and a conformance violation alike with CONSTRAINT_ERROR (packages/protocol/src/action/errors.ts:77-118 at 862ad75f)", false},
			"2.6": {classHarness, "TC-PAVST-2.6 step 8 modifies the transport with empty VideoStreams and AudioStreams lists (TC_PAVST_2_6.py:283-294), which the fields' constraint refuses (\"1 to 16\", matter.js push-av-stream-transport.element.ts:266, :270 at 862ad75f; CONSTRAINT_ERROR); chip's server treats an empty list as absent (PushAVStreamTransportLogic.cpp:671-680)", false},
			"2.11": {classHarness, "TC-PAVST-2.11 step 21a allocates with an empty AudioStreams list (TC_PAVST_2_11.py:470-479), which the field's constraint refuses (\"1 to 16\", matter.js push-av-stream-transport.element.ts:270 at 862ad75f; CONSTRAINT_ERROR) before the stream check the case is after; chip's server treats an empty list as absent (PushAVStreamTransportLogic.cpp:671-680)", false},
			"2.15": {classNotSupported, "TC-PAVST-2.15 drives UpdateMotionZoneOptions (command 0x08), which the matter.js snapshot does not carry (push-av-stream-transport.element.ts at 862ad75f lists commands 0x00-0x07); the module's server answers UNSUPPORTED_COMMAND", false},
			"2.4": {classHarness, "TC-PAVST-2.4 step 5 modifies the transport with empty VideoStreams and AudioStreams lists (TC_PAVST_2_4.py:203-204), which the fields' constraint refuses (\"1 to 16\", matter.js push-av-stream-transport.element.ts:266, :270 at 862ad75f; CONSTRAINT_ERROR, as matter.js validates a request); chip's server treats an empty list as absent (PushAVStreamTransportLogic.cpp:671-680)", false},
		},
	},
	{name: "PAVSTI", deviceType: dtCamera, variant: variantCamera},
	// CHIME: run1 drives the camera-app's endpoint 1 (TC_CHIME_2_1.py CI
	// arguments); here the Chime is a device of its own.
	{name: "CHIME", deviceType: dtChime, variant: variantCamera},
	// JFADMIN and JFDS: the --jointfabric-sim administrator
	// (examples/reference-bridge/jointfabric.go). Every case starts its
	// administrator itself; see the reasons.
	{
		name: "JFADMIN", deviceType: dtJointFabricAdministrator, variant: variantJointFabric,
		// TC-JFADMIN-1.2 has a lab path without the RPC: the case commissions
		// the administrator it is given and drives its commands
		// (TC_JFADMIN_1_2.py:98-103 and the steps from :140).
		labPath:        map[string]bool{"1.2": true},
		uncommissioned: map[string]bool{"1.2": true},
		args:           map[string][]string{"1.2": {"--passcode", "{passcode}", "--discriminator", "{discriminator}"}},
		edits: map[string][]edit{
			"1.2": {{"_JOINT_FABRIC_ADMINISTRATOR_ENDPOINT = 1\n", "_JOINT_FABRIC_ADMINISTRATOR_ENDPOINT = {ep:0x0130}\n", "the administrator is the daemon's bridged JointFabricAdministrator, not endpoint 1 (TC_JFADMIN_1_2.py:57)"}},
		},
		exclude: map[string]gap{
			// notes/parity/matter_behaviour_findings.md, "Certification
			// harness": TC-JFADMIN-1.2. Steps 1-15 pass on the lab path.
			"1.2": {classDefect, "TC-JFADMIN-1.2 step 16: an ICAC whose Subject DN is a TLV structure instead of a list (TC_JFADMIN_1_2.py:318-323) passes the AddICAC chain check (secure/mattercert Decode), so the administrator answers InvalidPublicKey where chip's ValidateCert refuses the certificate first, InvalidICAC (joint-fabric-administrator-server.cpp:375-376 at the harness pin); the fix is in secure/mattercert, a security path", false},
			"1.1": {classHarness, "TC-JFADMIN-1.1 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFADMIN_1_1.py:131-162; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
			"1.4": {classHarness, "TC-JFADMIN-1.4 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFADMIN_1_4.py:194-212; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
			"2.1": {classHarness, "TC-JFADMIN-2.1 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFADMIN_2_1.py:155-170; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
			"2.2": {classHarness, "TC-JFADMIN-2.2 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFADMIN_2_2.py:259-270; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
		},
	},
	{
		name: "JFDS", deviceType: dtJointFabricAdministrator, variant: variantJointFabric,
		exclude: map[string]gap{
			"2.1": {classHarness, "TC-JFDS-2.1 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_1.py:95-130; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
			"2.2": {classHarness, "TC-JFDS-2.2 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_2.py:99-120; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
			"2.3": {classHarness, "TC-JFDS-2.3 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_3.py:99-120; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
			"2.4": {classHarness, "TC-JFDS-2.4 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_4.py:108-130; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
			"2.5": {classHarness, "TC-JFDS-2.5 drives the administrator through chip's jf-control-app and the administrator app's pigweed RPC JointFabric service (TC_JFDS_2_5.py:124-162; examples/common/pigweed/protos/joint_fabric_service.proto at the harness pin): with the CI PICS it starts chip's own jf-admin-app as the DUT and never reaches the daemon, without them it needs the DUT's RPC server (dut_rpc_server_ip/_port); the daemon serves no pigweed RPC, an SDK test interface outside Matter", false},
		},
	},
	// TBRM, DGTHREAD and THNETDIR: the --otbr border router against the
	// virtual OTBR agent (script/otbr-virtual-test.sh): ThreadBorderRouter-
	// Management, ThreadNetworkDiagnostics (the four counts) and
	// ThreadNetworkDirectory on the 0x0091 endpoint
	// (examples/reference-bridge/otbr.go). chip runs TBRM and THNETDIR
	// against its network-manager app's endpoint 1 and DGTHREAD against the
	// root of a Thread device (Test_TC_DGTHREAD_2_1.yaml config endpoint
	// 0); the daemon has no Thread radio (ADR 0027), so DGTHREAD reads the
	// border router's diagnostics. They run in the thread group, on
	// dispatch and weekly (.github/workflows/otbr-virtual.yml).
	{name: "TBRM", deviceType: dtThreadBorderRouter, variant: variantThread},
	{name: "DGTHREAD", deviceType: dtThreadBorderRouter, variant: variantThread},
	{name: "THNETDIR", deviceType: dtThreadBorderRouter, variant: variantThread},
}
