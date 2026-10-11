// SPDX-License-Identifier: MIT
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
	// caseDeviceType overrides deviceType for single cases: a core case
	// that exercises an application cluster on --endpoint (TC-ACE-1.4 reads
	// OnOff there) is pointed at a bridged endpoint that serves it.
	caseDeviceType map[string]uint32
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
	// perEndpoint cases run once per endpoint of the daemon, each against
	// that endpoint's PICS slice (TC-IDM-10.4, the PICS checker).
	perEndpoint map[string]bool
	// knownProblems lets a case whose CHIP checker reports only recorded
	// problems pass, and only then (see knownProblems).
	knownProblems map[string]knownProblems
	// timeout raises the per-case budget (defaultCaseTimeout) for a case
	// that waits longer by design.
	timeout map[string]time.Duration
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
	dtWaterHeater = 0x050F
	dtRefrigerator          = 0x0070
	dtDishwasher            = 0x0075
	dtMicrowaveOven         = 0x0079
	dtOven                  = 0x007B
	// dtOvenCavity is no device type: it names the reference oven's
	// cavity part (examples/reference-bridge/fleet_kitchen.go), the
	// TemperatureControlledCabinet with OvenMode, OvenCavityOperationalState
	// and TemperatureControl. A real device type is 16 bits; the key is the
	// Oven's in the high half and the cabinet's in the low, so it collides
	// with none. It resolves to the first 0x0071 endpoint numbered above
	// the Oven's (bridgeProcess.endpointAfter).
	dtOvenCavity = dtOven<<16 | dtTempControlledCabinet
)

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
		name: "DA",
		// As matter.js passes them (test/core/DA.test.ts): TC_DA_1_7
		// recommissions and needs the onboarding values; TC_DA_1_2 looks
		// for the CD signing certificates relative to its working directory.
		args: map[string][]string{"*": {
			"--passcode", "{passcode}", "--discriminator", "{discriminator}",
			"--string-arg=cd_cert_dir:/credentials/development/cd-certs",
		}},
	},
	{name: "DD"},
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
		// host-name AAAA query; the daemon's mDNS side-car answers it
		// (mdns/host_responder.go). Both run unconditionally: they failed
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
	{name: "SU"},
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
		exclude: map[string]gap{
			"2.6": {classHarness, "TC-DRLK-2.6 gates every step on the Year Day Schedule feature (PICS DRLK.S.F0a=0 here) except its final \"Cleanup the created user\" ClearUser, which has no PICS gate and fails on a lock without the User feature (DRLK.S.F08=0)", false},
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
		caseDeviceType: map[string]uint32{"2.2": dtColorTempLight, "2.4": dtColorTempLight},
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
	{name: "SMOKECO", deviceType: dtSmokeCOAlarm},
	{
		name: "SWTCH", deviceType: dtGenericSwitch,
		exclude: map[string]gap{
			"2.2": {classNotSupported, "TC-SWTCH-2.2 runs only on a latching switch (PICS SWTCH.S.F00=0: the daemon's switch is momentary)", true},
			"2.5": {classNotSupported, "TC-SWTCH-2.5 runs only with MomentarySwitchMultiPress (PICS SWTCH.S.F04=0)", true},
			"2.6": {classNotSupported, "TC-SWTCH-2.6 runs only with MomentarySwitchMultiPress and ActionSwitch (PICS SWTCH.S.F04=0, SWTCH.S.F05=0)", true},
		},
	},
	{
		name: "TMP", deviceType: dtTempSensor,
		exclude: map[string]gap{
			"2.2": {classHarness, "TC-TMP-2.2 has an operator change the measured value between two reads (a UserPrompt under TMP.M.ManuallyControlled); an unattended run has no operator, and the YAML case has no app-pipe step that would stand in", false},
		},
	},
	{name: "TSTAT", deviceType: dtThermostat},
	{
		name: "VALCC", deviceType: dtWaterValve,
		exclude: map[string]gap{
			"3.2": {classNotSupported, "TC-VALCC-3.2 runs only with the Level feature (PICS VALCC.S.F01=0: the daemon's valve is open/closed)", true},
			"3.3": {classNotSupported, "TC-VALCC-3.3 runs only with DefaultOpenLevel (PICS VALCC.S.A0006=0)", true},
		},
	},
	{name: "WHM", deviceType: dtWaterHeater},
	{name: "WNCV", deviceType: dtWindowCovering},
}
