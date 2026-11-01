// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema_test

import (
	"testing"

	accesscontroldef "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	administratorcommissioningdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	basicinformationdef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	booleanstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	bridgeddevicebasicinformationdef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	carbondioxideconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	colorcontroldef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	descriptordef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	diagnosticlogsdef "github.com/SukramJ/go-fabric/cluster/spec/diagnosticlogs"
	doorlockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	electricalenergymeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	electricalpowermeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	generalcommissioningdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	generaldiagnosticsdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	groupkeymanagementdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	icdmanagementdef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	illuminancemeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/illuminancemeasurement"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	networkcommissioningdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	occupancysensingdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	operationalcredentialsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	otasoftwareupdaterequestordef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	pm10concentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pm10concentrationmeasurement"
	pm25concentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pm25concentrationmeasurement"
	powersourcedef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	pressuremeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pressuremeasurement"
	relativehumiditymeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/relativehumiditymeasurement"
	scenesmanagementdef "github.com/SukramJ/go-fabric/cluster/spec/scenesmanagement"
	smokecoalarmdef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	switchclusterdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	temperaturemeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	thermostatuserinterfaceconfigurationdef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	timesynchronizationdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	windowcoveringdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityCodeMatchesGeneratedSchema holds the revision every
// spec-based server serves — the generated definition's Revision, which
// [spec.Instance.ReadGlobal] answers for 0xFFFD — against the schema the
// schema generator emitted from the same snapshot, so a definition and the
// schema tables that drift apart fail here, naming the cluster. The rows
// read the constants themselves; they used to be a hand-copied table that
// a drifted constant passed (W3-clugen-08). The few revisions a server
// still states by hand (OperationalCredentials, AccessControl,
// GroupKeyManagement) are held against matter.js by
// cluster/core/parity_matterjs_test.go, which reads those constants.
func TestParityCodeMatchesGeneratedSchema(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id      uint32
		name    string
		codeRev uint16
	}{
		{accesscontroldef.ClusterID, accesscontroldef.Name, accesscontroldef.Revision},
		{basicinformationdef.ClusterID, basicinformationdef.Name, basicinformationdef.Revision},
		{bindingdef.ClusterID, bindingdef.Name, bindingdef.Revision},
		{bridgeddevicebasicinformationdef.ClusterID, bridgeddevicebasicinformationdef.Name, bridgeddevicebasicinformationdef.Revision},
		{descriptordef.ClusterID, descriptordef.Name, descriptordef.Revision},
		{diagnosticlogsdef.ClusterID, diagnosticlogsdef.Name, diagnosticlogsdef.Revision},
		{generalcommissioningdef.ClusterID, generalcommissioningdef.Name, generalcommissioningdef.Revision},
		{generaldiagnosticsdef.ClusterID, generaldiagnosticsdef.Name, generaldiagnosticsdef.Revision},
		{groupkeymanagementdef.ClusterID, groupkeymanagementdef.Name, groupkeymanagementdef.Revision},
		{icdmanagementdef.ClusterID, icdmanagementdef.Name, icdmanagementdef.Revision},
		{identifydef.ClusterID, identifydef.Name, identifydef.Revision},
		{networkcommissioningdef.ClusterID, networkcommissioningdef.Name, networkcommissioningdef.Revision},
		{operationalcredentialsdef.ClusterID, operationalcredentialsdef.Name, operationalcredentialsdef.Revision},
		{otasoftwareupdaterequestordef.ClusterID, otasoftwareupdaterequestordef.Name, otasoftwareupdaterequestordef.Revision},
		{powersourcedef.ClusterID, powersourcedef.Name, powersourcedef.Revision},
		{timesynchronizationdef.ClusterID, timesynchronizationdef.Name, timesynchronizationdef.Revision},
		{temperaturemeasurementdef.ClusterID, temperaturemeasurementdef.Name, temperaturemeasurementdef.Revision},
		{relativehumiditymeasurementdef.ClusterID, relativehumiditymeasurementdef.Name, relativehumiditymeasurementdef.Revision},
		{illuminancemeasurementdef.ClusterID, illuminancemeasurementdef.Name, illuminancemeasurementdef.Revision},
		{pressuremeasurementdef.ClusterID, pressuremeasurementdef.Name, pressuremeasurementdef.Revision},
		{booleanstatedef.ClusterID, booleanstatedef.Name, booleanstatedef.Revision},
		{occupancysensingdef.ClusterID, occupancysensingdef.Name, occupancysensingdef.Revision},
		{carbondioxideconcentrationmeasurementdef.ClusterID, carbondioxideconcentrationmeasurementdef.Name, carbondioxideconcentrationmeasurementdef.Revision},
		{pm25concentrationmeasurementdef.ClusterID, pm25concentrationmeasurementdef.Name, pm25concentrationmeasurementdef.Revision},
		{pm10concentrationmeasurementdef.ClusterID, pm10concentrationmeasurementdef.Name, pm10concentrationmeasurementdef.Revision},
		{electricalpowermeasurementdef.ClusterID, electricalpowermeasurementdef.Name, electricalpowermeasurementdef.Revision},
		{electricalenergymeasurementdef.ClusterID, electricalenergymeasurementdef.Name, electricalenergymeasurementdef.Revision},
		{switchclusterdef.ClusterID, switchclusterdef.Name, switchclusterdef.Revision},
		{administratorcommissioningdef.ClusterID, administratorcommissioningdef.Name, administratorcommissioningdef.Revision},
		{onoffdef.ClusterID, onoffdef.Name, onoffdef.Revision},
		{groupsdef.ClusterID, groupsdef.Name, groupsdef.Revision},
		{levelcontroldef.ClusterID, levelcontroldef.Name, levelcontroldef.Revision},
		{colorcontroldef.ClusterID, colorcontroldef.Name, colorcontroldef.Revision},
		{thermostatdef.ClusterID, thermostatdef.Name, thermostatdef.Revision},
		{thermostatuserinterfaceconfigurationdef.ClusterID, thermostatuserinterfaceconfigurationdef.Name, thermostatuserinterfaceconfigurationdef.Revision},
		{windowcoveringdef.ClusterID, windowcoveringdef.Name, windowcoveringdef.Revision},
		{doorlockdef.ClusterID, doorlockdef.Name, doorlockdef.Revision},
		{smokecoalarmdef.ClusterID, smokecoalarmdef.Name, smokecoalarmdef.Revision},
		{scenesmanagementdef.ClusterID, scenesmanagementdef.Name, scenesmanagementdef.Revision},
	}
	for _, c := range cases {
		schemaRev, ok := schema.ClusterRevision(c.id)
		if !ok {
			t.Errorf("cluster 0x%04X (%s) not found in generated schema — refresh parity/schema.json and run `make generate-matter-schema`", c.id, c.name)
			continue
		}
		if c.codeRev != schemaRev {
			t.Errorf("cluster 0x%04X (%s): the definition's Revision %d != the schema's %d — regenerate both from one snapshot", c.id, c.name, c.codeRev, schemaRev)
		}
	}
	if rev, ok := schema.ClusterRevision(0x0024); ok && rev != wire.SchedulesClusterRevision {
		t.Errorf("Schedules (0x0024): wire.SchedulesClusterRevision %d != the schema's %d", wire.SchedulesClusterRevision, rev)
	}
}

// TestDeviceTypeRevisionLookup verifies the DeviceTypeRevision helper returns
// the expected revisions for the device types the bridge currently advertises.
// Mirrors the cases in cluster/core/parity_matterjs_test.go
// TestParityMatterJS_DeviceTypeRevisions.
func TestDeviceTypeRevisionLookup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id      uint32
		name    string
		wantRev uint16
	}{
		{0x0016, "RootNode", 5},
		{0x000E, "Aggregator", 2},
		{0x0013, "BridgedNode", 3},
		{0x0015, "ContactSensor", 2},
		{0x0043, "WaterLeakDetector", 2},
		{0x002C, "AirQualitySensor", 1},
		{0x0076, "SmokeCoAlarm", 2},
		{0x0106, "LightSensor", 4},
		{0x0107, "OccupancySensor", 4},
		{0x0302, "TemperatureSensor", 3},
		{0x0305, "PressureSensor", 3},
		{0x0307, "HumiditySensor", 3},
		{0x000F, "GenericSwitch", 3},
		{0x0100, "OnOffLight", 4},
		{0x0101, "DimmableLight", 4},
		{0x010A, "OnOffPlugInUnit", 5},
		{0x010C, "ColorTemperatureLight", 5},
		{0x010D, "ExtendedColorLight", 5},
		{0x0202, "WindowCovering", 7},
		{0x0301, "Thermostat", 7},
		{0x000A, "DoorLock", 4},
	}

	for _, c := range cases {
		rev, ok := schema.DeviceTypeRevision(schema.DeviceTypeID(c.id))
		if !ok {
			t.Errorf("device type 0x%04X (%s) not found in generated schema", c.id, c.name)
			continue
		}
		if rev != c.wantRev {
			t.Errorf("device type 0x%04X (%s): schema revision %d != expected %d",
				c.id, c.name, rev, c.wantRev)
		}
	}
}

// TestSchemaCompleteness verifies the generated maps are non-empty and
// contain the baseline cluster + device-type count from matter.js HEAD.
// A sudden drop here means the snapshot was truncated or the generator broke.
func TestSchemaCompleteness(t *testing.T) {
	t.Parallel()

	const (
		minClusters    = 100 // matter.js HEAD has 115; allow some slack for future removals
		minDeviceTypes = 60  // matter.js HEAD has 72
	)

	if got := len(schema.ClusterRevisions); got < minClusters {
		t.Errorf("ClusterRevisions has %d entries, want at least %d — regenerate schema", got, minClusters)
	}
	if got := len(schema.ClusterNames); got != len(schema.ClusterRevisions) {
		t.Errorf("ClusterNames (%d) and ClusterRevisions (%d) have different lengths", len(schema.ClusterNames), len(schema.ClusterRevisions))
	}
	if got := len(schema.DeviceTypeRevisions); got < minDeviceTypes {
		t.Errorf("DeviceTypeRevisions has %d entries, want at least %d — regenerate schema", got, minDeviceTypes)
	}
	if got := len(schema.DeviceTypeNames); got != len(schema.DeviceTypeRevisions) {
		t.Errorf("DeviceTypeNames (%d) and DeviceTypeRevisions (%d) have different lengths", len(schema.DeviceTypeNames), len(schema.DeviceTypeRevisions))
	}
}
