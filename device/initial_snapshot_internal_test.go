// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"

	// Every generated cluster definition is linked, so the walk below
	// reads every struct attribute of the snapshot (spec.Lookup knows a
	// definition once its package is linked);
	// TestRefusedStructsOfTheSnapshot fails when one is missing here.
	_ "thingfabric.dev/go-fabric/cluster/spec/accesscontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/accountlogin"
	_ "thingfabric.dev/go-fabric/cluster/spec/actions"
	_ "thingfabric.dev/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	_ "thingfabric.dev/go-fabric/cluster/spec/administratorcommissioning"
	_ "thingfabric.dev/go-fabric/cluster/spec/airquality"
	_ "thingfabric.dev/go-fabric/cluster/spec/ambientcontextsensing"
	_ "thingfabric.dev/go-fabric/cluster/spec/applicationbasic"
	_ "thingfabric.dev/go-fabric/cluster/spec/applicationlauncher"
	_ "thingfabric.dev/go-fabric/cluster/spec/audiooutput"
	_ "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	_ "thingfabric.dev/go-fabric/cluster/spec/binding"
	_ "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
	_ "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
	_ "thingfabric.dev/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	_ "thingfabric.dev/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/carbonmonoxideconcentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/channel"
	_ "thingfabric.dev/go-fabric/cluster/spec/chime"
	_ "thingfabric.dev/go-fabric/cluster/spec/closurecontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/closuredimension"
	_ "thingfabric.dev/go-fabric/cluster/spec/colorcontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/commissionercontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/commoditymetering"
	_ "thingfabric.dev/go-fabric/cluster/spec/commodityprice"
	_ "thingfabric.dev/go-fabric/cluster/spec/commoditytariff"
	_ "thingfabric.dev/go-fabric/cluster/spec/contentappobserver"
	_ "thingfabric.dev/go-fabric/cluster/spec/contentcontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/contentlauncher"
	_ "thingfabric.dev/go-fabric/cluster/spec/descriptor"
	_ "thingfabric.dev/go-fabric/cluster/spec/deviceenergymanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/deviceenergymanagementmode"
	_ "thingfabric.dev/go-fabric/cluster/spec/diagnosticlogs"
	_ "thingfabric.dev/go-fabric/cluster/spec/dishwasheralarm"
	_ "thingfabric.dev/go-fabric/cluster/spec/dishwashermode"
	_ "thingfabric.dev/go-fabric/cluster/spec/doorlock"
	_ "thingfabric.dev/go-fabric/cluster/spec/ecosysteminformation"
	_ "thingfabric.dev/go-fabric/cluster/spec/electricalenergymeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/electricalgridconditions"
	_ "thingfabric.dev/go-fabric/cluster/spec/electricalpowermeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/energyevse"
	_ "thingfabric.dev/go-fabric/cluster/spec/energyevsemode"
	_ "thingfabric.dev/go-fabric/cluster/spec/energypreference"
	_ "thingfabric.dev/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
	_ "thingfabric.dev/go-fabric/cluster/spec/fancontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/fixedlabel"
	_ "thingfabric.dev/go-fabric/cluster/spec/flowmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/formaldehydeconcentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/generalcommissioning"
	_ "thingfabric.dev/go-fabric/cluster/spec/generaldiagnostics"
	_ "thingfabric.dev/go-fabric/cluster/spec/groupcast"
	_ "thingfabric.dev/go-fabric/cluster/spec/groupkeymanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/groups"
	_ "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
	_ "thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/identify"
	_ "thingfabric.dev/go-fabric/cluster/spec/illuminancemeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/jointfabricadministrator"
	_ "thingfabric.dev/go-fabric/cluster/spec/jointfabricdatastore"
	_ "thingfabric.dev/go-fabric/cluster/spec/keypadinput"
	_ "thingfabric.dev/go-fabric/cluster/spec/laundrydryercontrols"
	_ "thingfabric.dev/go-fabric/cluster/spec/laundrywashercontrols"
	_ "thingfabric.dev/go-fabric/cluster/spec/laundrywashermode"
	_ "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/localizationconfiguration"
	_ "thingfabric.dev/go-fabric/cluster/spec/lowpower"
	_ "thingfabric.dev/go-fabric/cluster/spec/mediainput"
	_ "thingfabric.dev/go-fabric/cluster/spec/mediaplayback"
	_ "thingfabric.dev/go-fabric/cluster/spec/messages"
	_ "thingfabric.dev/go-fabric/cluster/spec/meteridentification"
	_ "thingfabric.dev/go-fabric/cluster/spec/microwaveovencontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/microwaveovenmode"
	_ "thingfabric.dev/go-fabric/cluster/spec/modeselect"
	_ "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	_ "thingfabric.dev/go-fabric/cluster/spec/nitrogendioxideconcentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/occupancysensing"
	_ "thingfabric.dev/go-fabric/cluster/spec/onoff"
	_ "thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	_ "thingfabric.dev/go-fabric/cluster/spec/operationalstate"
	_ "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdateprovider"
	_ "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	_ "thingfabric.dev/go-fabric/cluster/spec/ovencavityoperationalstate"
	_ "thingfabric.dev/go-fabric/cluster/spec/ovenmode"
	_ "thingfabric.dev/go-fabric/cluster/spec/ozoneconcentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/pm10concentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/pm1concentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/pm25concentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/powersource"
	_ "thingfabric.dev/go-fabric/cluster/spec/powersourceconfiguration"
	_ "thingfabric.dev/go-fabric/cluster/spec/powertopology"
	_ "thingfabric.dev/go-fabric/cluster/spec/pressuremeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/pushavstreamtransport"
	_ "thingfabric.dev/go-fabric/cluster/spec/radonconcentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/refrigeratoralarm"
	_ "thingfabric.dev/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	_ "thingfabric.dev/go-fabric/cluster/spec/relativehumiditymeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/rvccleanmode"
	_ "thingfabric.dev/go-fabric/cluster/spec/rvcoperationalstate"
	_ "thingfabric.dev/go-fabric/cluster/spec/rvcrunmode"
	_ "thingfabric.dev/go-fabric/cluster/spec/scenesmanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/servicearea"
	_ "thingfabric.dev/go-fabric/cluster/spec/smokecoalarm"
	_ "thingfabric.dev/go-fabric/cluster/spec/softwarediagnostics"
	_ "thingfabric.dev/go-fabric/cluster/spec/soilmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/switchcluster"
	_ "thingfabric.dev/go-fabric/cluster/spec/targetnavigator"
	_ "thingfabric.dev/go-fabric/cluster/spec/temperaturealarm"
	_ "thingfabric.dev/go-fabric/cluster/spec/temperaturecontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/temperaturemeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	_ "thingfabric.dev/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	_ "thingfabric.dev/go-fabric/cluster/spec/threadborderroutermanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/threadnetworkdiagnostics"
	_ "thingfabric.dev/go-fabric/cluster/spec/threadnetworkdirectory"
	_ "thingfabric.dev/go-fabric/cluster/spec/timeformatlocalization"
	_ "thingfabric.dev/go-fabric/cluster/spec/timesynchronization"
	_ "thingfabric.dev/go-fabric/cluster/spec/tlscertificatemanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/tlsclientmanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/totalvolatileorganiccompoundsconcentrationmeasurement"
	_ "thingfabric.dev/go-fabric/cluster/spec/unitlocalization"
	_ "thingfabric.dev/go-fabric/cluster/spec/userlabel"
	_ "thingfabric.dev/go-fabric/cluster/spec/valveconfigurationandcontrol"
	_ "thingfabric.dev/go-fabric/cluster/spec/wakeonlan"
	_ "thingfabric.dev/go-fabric/cluster/spec/waterheatermanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/waterheatermode"
	_ "thingfabric.dev/go-fabric/cluster/spec/watertanklevelmonitoring"
	_ "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	_ "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	_ "thingfabric.dev/go-fabric/cluster/spec/wifinetworkdiagnostics"
	_ "thingfabric.dev/go-fabric/cluster/spec/wifinetworkmanagement"
	_ "thingfabric.dev/go-fabric/cluster/spec/windowcovering"
	_ "thingfabric.dev/go-fabric/cluster/spec/zonemanagement"
)

// refusedStructs are the struct attributes of the snapshot refusedAtInit
// refuses — not nullable, no stated default, a mandatory child matter.js
// leaves undefined when it builds the struct from its children's defaults
// (DefaultValue.ts:130-143, :153-179 at db021da0) — each with that child,
// as "<cluster>.<attribute>" → "<child path>". The set is exact in both
// directions.
var refusedStructs = map[string]string{
	"ApplicationBasic.Application":                    "CatalogVendorId",
	"BasicInformation.ProductAppearance":              "Finish",
	"BridgedDeviceBasicInformation.ProductAppearance": "Finish",
	"CameraAvStreamManagement.MicrophoneCapabilities": "MaxNumberOfChannels",
	"CameraAvStreamManagement.MinViewportResolution":  "Width",
	"CameraAvStreamManagement.SpeakerCapabilities":    "MaxNumberOfChannels",
	"CameraAvStreamManagement.VideoSensorParams":      "SensorWidth",
	"CameraAvStreamManagement.Viewport":               "X1",
	"ClosureDimension.LimitRange":                     "Min",
	"ElectricalEnergyMeasurement.Accuracy":            "MeasurementType",
	"GeneralCommissioning.BasicCommissioningInfo":     "FailSafeExpiryLengthSeconds",
	"GeneralDiagnostics.DeviceLoadStatus":             "CurrentSubscriptions",
	"JointFabricDatastore.Status":                     "State",
	"OccupancySensing.HoldTimeLimits":                 "HoldTimeMin",
	"SoilMeasurement.SoilMoistureMeasurementLimits":   "MeasurementType",
	"ZoneManagement.TwoDCartesianMax":                 "X",
}

// TestRefusedStructsOfTheSnapshot walks every struct attribute of every
// generated definition: the ones refusedAtInit refuses are exactly
// refusedStructs, and each named child is mandatory, not nullable, without
// a default, and neither a list that admits [] nor a struct — a member
// matter.js leaves undefined.
func TestRefusedStructsOfTheSnapshot(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../cluster/spec/*/definition_gen.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no generated definitions (%v)", err)
	}
	re := regexp.MustCompile(`ClusterID\s+uint32\s*=\s*0x([0-9A-Fa-f]+)`)
	got := map[string]bool{}
	structs := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m := re.FindSubmatch(b)
		if m == nil {
			t.Fatalf("%s states no ClusterID", f)
		}
		id, _ := strconv.ParseUint(string(m[1]), 16, 32)
		def := spec.Lookup(uint32(id))
		if def == nil {
			t.Errorf("%s (0x%04X) is not linked: add it to this file's imports", filepath.Dir(f), id)
			continue
		}
		for i := range def.Attributes {
			a := &def.Attributes[i]
			if a.Type.Kind != spec.KindStruct {
				continue
			}
			structs++
			if !refusedAtInit(a.Type, a.Quality, a.Constraint, a.Default) {
				continue
			}
			name := def.Name + "." + a.Name
			got[name] = true
			if child, ok := refusedStructs[name]; ok {
				if why := undefinedChild(a.Type.Struct, child); why != "" {
					t.Errorf("%s: child %s %s", name, child, why)
				}
			}
		}
	}
	if structs < 20 {
		t.Fatalf("the walk broke: %d struct attributes", structs)
	}
	var extra, missing []string
	for k := range got {
		if _, ok := refusedStructs[k]; !ok {
			extra = append(extra, k)
		}
	}
	for k := range refusedStructs {
		if !got[k] {
			missing = append(missing, k)
		}
	}
	slices.Sort(extra)
	slices.Sort(missing)
	if len(extra)+len(missing) > 0 {
		t.Errorf("refused struct attributes: not listed %q, listed but not refused %q (of %d struct attributes)", extra, missing, structs)
	}
	t.Logf("%d struct attributes, %d refused", structs, len(got))
}

// undefinedChild is "" when the child of st that path names (dotted
// through nested structs) is one matter.js leaves undefined: mandatory,
// not nullable, without a default, and neither a list that admits [] nor
// a struct; otherwise what it is instead.
func undefinedChild(st *spec.Struct, path string) string {
	name, rest, nested := strings.Cut(path, ".")
	if st == nil {
		return "is in a struct of no known shape"
	}
	for i := range st.Fields {
		f := &st.Fields[i]
		if f.Name != name {
			continue
		}
		switch {
		case !f.Mandatory():
			return "is not mandatory"
		case f.Quality.Nullable:
			return "is nullable"
		case f.Default != nil:
			return "has a default"
		case nested:
			return undefinedChild(f.Type.Struct, rest)
		case f.Type.Kind == spec.KindStruct:
			return "is a struct"
		case f.Type.Kind == spec.KindList && !excludesZero(f.Constraint):
			return "is a list that admits []"
		}
		return ""
	}
	return "is not a field"
}
