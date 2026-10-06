// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package chipdm

import (
	"slices"
	"strings"
)

// Class is why a difference between the snapshot and CHIP's data model is
// acknowledged. Class (iii), representation only, has no entries: those
// differences are normalized away in compare.go and counted per rule.
type Class string

const (
	// ClassMatterJS (i): matter.js is right, or deliberately different —
	// CHIP's XML is known to be wrong or to lag, or a matter.js override
	// (support/models/src/local/) states a reasoned divergence.
	ClassMatterJS Class = "i"

	// ClassCHIP (ii): CHIP's data model is right and the snapshot is wrong.
	// A certifiability risk wherever go-fabric's behaviour depends on the
	// snapshot's value; each entry says whether it does.
	ClassCHIP Class = "ii"

	// ClassHarness (iv): the certification harness's own data-model
	// parsing already corrects or excuses the difference.
	ClassHarness Class = "iv"
)

// Acknowledgement is one reviewed difference. It matches a Difference by
// property and both values exactly — a change on either side stops it
// matching, so the difference surfaces again — and by path, where a
// leading "*." stands for at least one enclosing element and a "*" segment
// for any one segment (matter.js by-design.ts matches, extended by the
// segment wildcard). Paths and values compare canonically.
type Acknowledgement struct {
	Class    Class
	Path     string
	Property string
	Chip     string
	Ours     string
	Reason   string

	// Source cites where the reason is established.
	Source string

	// Impact is, for class (ii), whether go-fabric's behaviour depends on
	// the snapshot's value and what was done about it.
	Impact string
}

const (
	byDesign      = "matter.js support/codegen/src/chipdm/by-design.ts KNOWN_DIFFERENCES"
	localOverride = "matter.js support/models/src/local/"
	specParsing   = "connectedhomeip src/python_testing/matter_testing_infrastructure/matter/testing/spec_parsing.py build_xml_clusters"
	okNotSuccess  = "The specification names the value Ok where the global status names it Success; spec enhancement filed"
	sentinel      = "We keep both alternatives the specification states; CHIP keeps the sentinel"
	unscaled      = "CHIP states the fallback of a percent100ths attribute unscaled; 0.01 is not a value of the type"
	critical      = "The specification requires CRITICAL for the states that matter and permits INFO otherwise, so " +
		"matter.js states CRITICAL; a DoorLock override records that deliberately"
	fieldSelected = "matter.js keeps the field the conformance selects; CHIP names only the command"
	modeStatus    = "matter.js names the status codes of the mode clusters ModeChangeStatus; CHIP types the field as the global status"
	respMax       = "matter.js resolves the RESP_MAX constant the specification defines; CHIP keeps the name"
	wncvTable     = "matter.js states the lift / tilt feature each covering type needs; the specification's enum table " +
		"has no conformance column, so CHIP marks every value M"
)

// Acknowledged is the reviewed table. Class (i) entries from matter.js's
// by-design.ts keep matter.js's reason verbatim.
var Acknowledged = []Acknowledgement{
	// --- (i) matter.js by-design.ts ---------------------------------------
	{ClassMatterJS, "ClosureDimension.Resolution", "default", "0.01", "1", unscaled, byDesign, ""},
	{ClassMatterJS, "ClosureDimension.StepValue", "default", "0.01", "1", unscaled, byDesign, ""},
	{ClassMatterJS, "GeneralCommissioning.ArmFailSafeResponse.ErrorCode", "default", "success", "0", okNotSuccess, byDesign, ""},
	{ClassMatterJS, "GeneralCommissioning.SetRegulatoryConfigResponse.ErrorCode", "default", "success", "0", okNotSuccess, byDesign, ""},
	{ClassMatterJS, "GeneralCommissioning.CommissioningCompleteResponse.ErrorCode", "default", "success", "0", okNotSuccess, byDesign, ""},
	{ClassMatterJS, "GeneralCommissioning.SetTcAcknowledgementsResponse.ErrorCode", "default", "success", "0", okNotSuccess, byDesign, ""},
	{ClassMatterJS, "OperationalCredentials.NocResponse.FabricIndex", "conformance", "statuscode==success,o", "statuscode==ok,o", okNotSuccess, byDesign, ""},
	{ClassMatterJS, "JointFabricAdministrator.IcaccsrResponse.Icaccsr", "conformance", "statuscode==success,o", "statuscode==ok,o", okNotSuccess, byDesign, ""},
	{ClassMatterJS, "DoorLock.DoorStateChange", "priority", "desc", "critical", critical, byDesign, ""},
	{ClassMatterJS, "DoorLock.LockOperation", "priority", "desc", "critical", critical, byDesign, ""},
	{ClassMatterJS, "DoorLock.LockOperationError", "priority", "desc", "critical", critical, byDesign, ""},
	{ClassMatterJS, "DoorLock.ClearWeekDaySchedule.WeekDayIndex", "constraint", "254", "1tonumberofweekdayschedulessupportedperuser,254", sentinel, byDesign, ""},
	{ClassMatterJS, "DoorLock.ClearYearDaySchedule.YearDayIndex", "constraint", "254", "1tonumberofyeardayschedulessupportedperuser,254", sentinel, byDesign, ""},
	{ClassMatterJS, "DoorLock.ClearHolidaySchedule.HolidayIndex", "constraint", "254", "1tonumberofholidayschedulessupported,254", sentinel, byDesign, ""},
	{ClassMatterJS, "DoorLock.ClearUser.UserIndex", "constraint", "65534", "1tonumberoftotaluserssupported,65534", sentinel, byDesign, ""},
	{
		ClassMatterJS, "IlluminanceMeasurement.MeasuredValue", "constraint", "0", "0,minmeasuredvaluetomaxmeasuredvalue",
		"We keep the range the specification states alongside the zero sentinel", byDesign, "",
	},
	{
		ClassMatterJS, "Thermostat.SetpointChangeAmount", "type", "int16s", "temperaturedifference",
		"We name the temperature type the specification defines; CHIP states a primitive", byDesign, "",
	},
	{
		ClassMatterJS, "ContentLauncher.ContentSearchStruct.ParameterList", "default", "0", "undefined",
		"CHIP states a numeric fallback for a list; the specification states none", byDesign, "",
	},
	{ClassMatterJS, "*.SolicitOfferResponse.VideoStreamId", "conformance", "solicitoffer,d", "solicitoffer.videostreamid,d", fieldSelected, byDesign, ""},
	{ClassMatterJS, "*.SolicitOfferResponse.AudioStreamId", "conformance", "solicitoffer,d", "solicitoffer.audiostreamid,d", fieldSelected, byDesign, ""},
	{ClassMatterJS, "*.ProvideOfferResponse.VideoStreamId", "conformance", "provideoffer,d", "provideoffer.videostreamid,d", fieldSelected, byDesign, ""},
	{ClassMatterJS, "*.ProvideOfferResponse.AudioStreamId", "conformance", "provideoffer,d", "provideoffer.audiostreamid,d", fieldSelected, byDesign, ""},
	{
		ClassMatterJS, "KeypadInput.CecKeyCodeEnum.Reserved", "field", "present", "absent",
		"We drop a value the specification names Reserved; CHIP keeps it", byDesign, "",
	},
	{
		ClassMatterJS, "Messages.MessageID", "datatype", "absent", "present",
		"The specification defines the type in the cluster; CHIP treats it as a built-in", byDesign, "",
	},
	{
		ClassMatterJS, "ScenesManagement.LogicalSceneTable", "datatype", "absent", "present",
		"The specification defines the type in the cluster; CHIP omits it", byDesign, "",
	},
	{ClassMatterJS, "*.ModeChangeStatus", "datatype", "absent", "present", modeStatus, byDesign, ""},
	{ClassMatterJS, "RvcCleanMode.StatusCodeEnum", "datatype", "present", "absent", "matter.js names the status codes of the mode clusters ModeChangeStatus", byDesign, ""},
	{ClassMatterJS, "RvcRunMode.StatusCodeEnum", "datatype", "present", "absent", "matter.js names the status codes of the mode clusters ModeChangeStatus", byDesign, ""},
	{
		ClassMatterJS, "Thermostat.MinSetpointDeadBand", "constraint", "0to127", "0to12.7°c",
		"We keep the temperature notation of the specification where CHIP states the encoded value", byDesign, "",
	},
	{ClassMatterJS, "OperationalCredentials.AttestationResponse.AttestationElements", "constraint", "maxresp_max", "max900", respMax, byDesign, ""},
	{ClassMatterJS, "OperationalCredentials.CsrResponse.NocsrElements", "constraint", "maxresp_max", "max900", respMax, byDesign, ""},
	{
		ClassMatterJS, "OtaSoftwareUpdateRequestor.AnnounceOtaProvider", "access fabric", "absent", "F",
		"The specification scopes the command to the accessing fabric; CHIP's data model XML does not record it", byDesign, "",
	},

	// --- (i) matter.js overrides (support/models/src/local/) ---------------
	{
		ClassMatterJS, "ColorControl.RemainingTime", "constraint", "max65534", "0to65535",
		"65535 stands for \"endless\" while a color loop runs", localOverride + "ColorControlOverrides.ts", "",
	},
	{
		ClassMatterJS, "ColorControl.ColorTemperatureMireds", "constraint", "max65279", "colortempphysicalminmiredstocolortempphysicalmaxmireds",
		"The physical bounds the specification's prose states, which validate a value against the device", localOverride + "ColorControlOverrides.ts", "",
	},
	{
		ClassMatterJS, "CommodityTariff.TariffComponentStruct.PeakPeriod", "default", "0", "undefined",
		"The specification states 0 as the default of a struct-typed field, which no struct can be", localOverride + "CommodityTariffOverrides.ts", "",
	},
	{
		ClassMatterJS, "CommodityTariff.TariffComponentStruct.PowerThreshold", "default", "0", "undefined",
		"The specification states 0 as the default of a struct-typed field, which no struct can be", localOverride + "CommodityTariffOverrides.ts", "",
	},
	{
		ClassMatterJS, "FanControl.FanModeEnum.Medium", "conformance", "[low]", "o",
		"\"[Low]\" names another enum value, which a value's conformance cannot be evaluated against", localOverride + "FanControl.ts", "",
	},
	{
		ClassMatterJS, "IlluminanceMeasurement.LightSensorType", "type", "lightsensortypeenum", "uint8",
		"The specification defines LightSensorTypeEnum but permits values outside it; uint8 is the same wire type",
		localOverride + "IlluminanceMeasurementOverrides.ts", "",
	},
	{
		ClassMatterJS, "JointFabricDatastore.DatastoreAccessControlEntryStruct.Subjects", "constraint", "maxsubjectsperaccesscontrolentry", "none",
		"The bound is an attribute of another cluster (Access Control), which a constraint cannot reach", localOverride + "JointFabricDatastoreOverrides.ts", "",
	},
	{
		ClassMatterJS, "JointFabricDatastore.DatastoreAccessControlEntryStruct.Targets", "constraint", "maxtargetsperaccesscontrolentry", "none",
		"The bound is an attribute of another cluster (Access Control), which a constraint cannot reach", localOverride + "JointFabricDatastoreOverrides.ts", "",
	},
	{
		ClassMatterJS, "LocalizationConfiguration.ActiveLocale", "constraint", "max35", "insupportedlocales",
		"Validates the value against SupportedLocales, whose entries the specification bounds to 35", localOverride + "LocalizationConfigurationOverrides.ts", "",
	},
	{
		ClassMatterJS, "OtaSoftwareUpdateRequestor.UpdateStateProgress", "quality", "nullable", "nullable quieter",
		"The specification asks nodes not to over-report progress; Q states that", localOverride + "OtaSoftwareUpdateRequestor.ts", "",
	},
	{
		ClassMatterJS, "UserLabel.LabelList", "constraint", "desc", "min0",
		"\"Minimum 4\" means a node supports at least four entries; an empty list is valid", localOverride + "UserLabelOverrides.ts", "",
	},
	{
		ClassMatterJS, "TlsClientManagement.FindEndpointResponse.Endpoint", "constraint", "0to65534", "none",
		"The specification bounds a TLSEndpointStruct by the range of TLSEndpointID; no comparison orders a struct against a number",
		localOverride + "TlsClientManagementOverrides.ts", "",
	},
	{
		ClassMatterJS, "DoorLock.OperatingModesBitmap.AlwaysSet", "field", "absent", "present",
		"The bitmap is inverse; the bits the specification leaves unnamed are always set", localOverride + "DoorLockOverrides.ts", "",
	},
	{
		ClassMatterJS, "Thermostat.HVACSystemTypeBitmap", "datatype", "absent", "present",
		"Types of the attributes Matter 1.5.1 removed, kept for clients until 1.7", localOverride + "ThermostatOverrides.ts", "",
	},
	{
		ClassMatterJS, "Thermostat.ProgrammingOperationModeBitmap", "datatype", "absent", "present",
		"Types of the attributes Matter 1.5.1 removed, kept for clients until 1.7", localOverride + "ThermostatOverrides.ts", "",
	},
	{ClassMatterJS, "WindowCovering.TypeEnum.*", "conformance", "m", "!tl&lf", wncvTable, localOverride + "WindowCoveringOverrides.ts", ""},
	{ClassMatterJS, "WindowCovering.TypeEnum.*", "conformance", "m", "!lf&tl", wncvTable, localOverride + "WindowCoveringOverrides.ts", ""},
	{ClassMatterJS, "WindowCovering.TypeEnum.*", "conformance", "m", "!lf&tl|!tl&lf", wncvTable, localOverride + "WindowCoveringOverrides.ts", ""},
	{ClassMatterJS, "WindowCovering.TypeEnum.*", "conformance", "m", "o", wncvTable, localOverride + "WindowCoveringOverrides.ts", ""},
	{ClassMatterJS, "WindowCovering.EndProductTypeEnum.*", "conformance", "m", "!tl&lf", wncvTable, localOverride + "WindowCoveringOverrides.ts", ""},
	{ClassMatterJS, "WindowCovering.EndProductTypeEnum.*", "conformance", "m", "!lf&tl", wncvTable, localOverride + "WindowCoveringOverrides.ts", ""},
	{ClassMatterJS, "WindowCovering.EndProductTypeEnum.*", "conformance", "m", "lf&tl", wncvTable, localOverride + "WindowCoveringOverrides.ts", ""},
	{ClassMatterJS, "WindowCovering.EndProductTypeEnum.*", "conformance", "m", "!lf&tl|!tl&lf", wncvTable, localOverride + "WindowCoveringOverrides.ts", ""},
	{
		ClassMatterJS, "WindowCovering.TypeEnum.TiltBlindLift", "field", "absent", "present",
		"Value 8 keeps its earlier name; CHIP names it TiltBlindLiftAndTilt (same value)", localOverride + "WindowCoveringOverrides.ts", "",
	},
	{
		ClassMatterJS, "WindowCovering.TypeEnum.TiltBlindLiftAndTilt", "field", "present", "absent",
		"Value 8 keeps its earlier name TiltBlindLift (same value)", localOverride + "WindowCoveringOverrides.ts", "",
	},
	{
		ClassMatterJS, "WindowCovering.GoToLiftPercentage.Ignored", "field", "absent", "present",
		"The specification defines a second field CHIP does not use; matter.js disallows it (X) to follow the de-facto standard",
		localOverride + "WindowCoveringOverrides.ts", "",
	},
	{
		ClassMatterJS, "WindowCovering.GoToTiltPercentage.Ignored", "field", "absent", "present",
		"The specification defines a second field CHIP does not use; matter.js disallows it (X) to follow the de-facto standard",
		localOverride + "WindowCoveringOverrides.ts", "",
	},
	{
		ClassMatterJS, "ModeSelect.StandardNamespace", "type", "enum16", "namespace",
		"The namespace type enumerates exactly the standard namespace ids, at the width of the semantic tag's " +
			"NamespaceID (enum8), which every standard namespace id fits; TLV carries the value at its own width",
		localOverride + "ModeSelectOverrides.ts", "",
	},
	{
		ClassMatterJS, "WindowCovering.MovementStatus", "datatype", "absent", "present",
		"The formal type of the OperationalStatus bit fields, which the specification describes in prose", localOverride + "WindowCoveringOverrides.ts", "",
	},

	// --- (ii) CHIP is right, the snapshot is wrong --------------------------
	{
		ClassCHIP, "GroupKeyManagement.GroupKeySetStruct.GroupKeyMulticastPolicy", "conformance", "d", "o",
		"Matter 1.6.1 deprecates the field (matter.js's own 1.6.1 scrape states D); a matter.js override written when " +
			"the specification said \"P, M\" forces O for every revision",
		localOverride + "GroupKeyManagementOverrides.ts; support/models/src/v1.6.1/spec.ts GroupKeySetStruct",
		"No certification case reads the field. go-fabric's KeySetReadResponse reports field 8 as matter.js's " +
			"GroupKeyManagementServer does while its model defines the field, which a D field still is, so the behaviour " +
			"stays; CHIP's server omits it. Finding recorded; upstream candidate (gate the override until 1.6.1).",
	},
	{
		ClassCHIP, "semtag.Label", "conformance", "mfgcode!=null,o", "o",
		"The specification makes Label mandatory when MfgCode is not null; matter.js relaxes it to O " +
			"(\"TODO we do not support MfgCode != null\")",
		localOverride + "semtag.ts",
		"go-fabric serves no Descriptor TagList (cluster/core/descriptor.go) and ModeSelect's SemanticTagStruct is a " +
			"different struct, so nothing depends on it. Finding recorded; known upstream TODO.",
	},

	// --- (iv) the harness excuses it -----------------------------------------
	{
		ClassHarness, "Thermostat.AtomicRequest", "command", "absent", "present",
		"CHIP defines AtomicRequest globally (globals/Commands.xml); the harness adds it to Thermostat from revision 8 " +
			"with conformance Presets | Schedules and Operate privilege, as matter.js models it there (PRES | MSCH, " +
			"the features those attributes require)",
		specParsing + " (\"Need automated parsing for atomic attributes\"); " + localOverride + "ThermostatOverrides.ts", "",
	},
	{
		ClassHarness, "Thermostat.AtomicResponse", "command", "absent", "present",
		"CHIP defines AtomicResponse globally (globals/Commands.xml); the harness adds it to Thermostat from revision 8 " +
			"with conformance Presets | Schedules and Operate privilege, as matter.js models it there (PRES | MSCH, " +
			"the features those attributes require)",
		specParsing + " (\"Need automated parsing for atomic attributes\"); " + localOverride + "ThermostatOverrides.ts", "",
	},
}

// matches is by-design.ts matches, with a "*" segment matching any one
// segment.
func (a *Acknowledgement) matches(d Difference) bool {
	if a.Property != d.Property || a.Chip != d.Chip || a.Ours != d.Ours {
		return false
	}
	pattern := a.Path
	wildcard := strings.HasPrefix(pattern, "*.")
	if wildcard {
		pattern = pattern[2:]
	}
	want := strings.Split(pattern, ".")
	got := strings.Split(d.Path, ".")
	if wildcard {
		// The wildcard stands for at least one enclosing element.
		if len(want) >= len(got) {
			return false
		}
		got = got[len(got)-len(want):]
	} else if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] != "*" && canonicalName(want[i]) != canonicalName(got[i]) {
			return false
		}
	}
	return true
}

// Reconciliation is a comparison held against the acknowledged table.
type Reconciliation struct {
	// Explained maps each acknowledgement to the differences it explains.
	Explained map[int][]Difference

	// Unexplained are differences no acknowledgement matches.
	Unexplained []Difference

	// Stale are acknowledgements that match no difference.
	Stale []Acknowledgement
}

// Reconcile holds differences against acknowledgements. Every difference
// must be explained and every acknowledgement must still explain one.
func Reconcile(diffs []Difference, acks []Acknowledgement) Reconciliation {
	r := Reconciliation{Explained: map[int][]Difference{}}
	for _, d := range diffs {
		found := false
		for i := range acks {
			if acks[i].matches(d) {
				r.Explained[i] = append(r.Explained[i], d)
				found = true
				break
			}
		}
		if !found {
			r.Unexplained = append(r.Unexplained, d)
		}
	}
	for i := range acks {
		if len(r.Explained[i]) == 0 {
			r.Stale = append(r.Stale, acks[i])
		}
	}
	return r
}

// CountByClass counts the explained differences per class.
func (r Reconciliation) CountByClass(acks []Acknowledgement) map[Class]int {
	out := map[Class]int{}
	keys := make([]int, 0, len(r.Explained))
	for i := range r.Explained {
		keys = append(keys, i)
	}
	slices.Sort(keys)
	for _, i := range keys {
		out[acks[i].Class] += len(r.Explained[i])
	}
	return out
}
