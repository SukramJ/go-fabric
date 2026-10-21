// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	accountlogindef "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	actionsdef "github.com/SukramJ/go-fabric/cluster/spec/actions"
	activatedcarbonfiltermonitoringdef "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	administratorcommissioningdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	airqualitydef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	ambientcontextsensingdef "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing"
	applicationbasicdef "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
	applicationlauncherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	audiooutputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	basicinformationdef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	booleanstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	booleanstateconfigurationdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	bridgeddevicebasicinformationdef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	carbondioxideconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	carbonmonoxideconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/carbonmonoxideconcentrationmeasurement"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	closurecontroldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	closuredimensiondef "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	colorcontroldef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	commissionercontroldef "github.com/SukramJ/go-fabric/cluster/spec/commissionercontrol"
	commoditymeteringdef "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	commoditypricedef "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	commoditytariffdef "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	contentappobserverdef "github.com/SukramJ/go-fabric/cluster/spec/contentappobserver"
	contentcontroldef "github.com/SukramJ/go-fabric/cluster/spec/contentcontrol"
	contentlauncherdef "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	descriptordef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	deviceenergymanagementdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	deviceenergymanagementmodedef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagementmode"
	diagnosticlogsdef "github.com/SukramJ/go-fabric/cluster/spec/diagnosticlogs"
	dishwasheralarmdef "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	dishwashermodedef "github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	doorlockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	ecosysteminformationdef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	electricalenergymeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	electricalgridconditionsdef "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	electricalpowermeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	energyevsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	energyevsemodedef "github.com/SukramJ/go-fabric/cluster/spec/energyevsemode"
	energypreferencedef "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	ethernetnetworkdiagnosticsdef "github.com/SukramJ/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
	fancontroldef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	flowmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/flowmeasurement"
	formaldehydeconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/formaldehydeconcentrationmeasurement"
	generalcommissioningdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	generaldiagnosticsdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	groupcastdef "github.com/SukramJ/go-fabric/cluster/spec/groupcast"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	hepafiltermonitoringdef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	icdmanagementdef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	illuminancemeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/illuminancemeasurement"
	keypadinputdef "github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	laundrydryercontrolsdef "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	laundrywashercontrolsdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	laundrywashermodedef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	localizationconfigurationdef "github.com/SukramJ/go-fabric/cluster/spec/localizationconfiguration"
	lowpowerdef "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	mediainputdef "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	mediaplaybackdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	messagesdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
	meteridentificationdef "github.com/SukramJ/go-fabric/cluster/spec/meteridentification"
	microwaveovencontroldef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	microwaveovenmodedef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	modeselectdef "github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	networkcommissioningdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	nitrogendioxideconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/nitrogendioxideconcentrationmeasurement"
	occupancysensingdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	operationalstatedef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	otasoftwareupdateproviderdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	otasoftwareupdaterequestordef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	ovencavityoperationalstatedef "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate"
	ovenmodedef "github.com/SukramJ/go-fabric/cluster/spec/ovenmode"
	ozoneconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/ozoneconcentrationmeasurement"
	pm10concentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pm10concentrationmeasurement"
	pm1concentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pm1concentrationmeasurement"
	pm25concentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pm25concentrationmeasurement"
	powersourcedef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	powersourceconfigurationdef "github.com/SukramJ/go-fabric/cluster/spec/powersourceconfiguration"
	powertopologydef "github.com/SukramJ/go-fabric/cluster/spec/powertopology"
	pressuremeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pressuremeasurement"
	pumpconfigurationandcontroldef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	radonconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/radonconcentrationmeasurement"
	refrigeratoralarmdef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	refrigeratorandtemperaturecontrolledcabinetmodedef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	relativehumiditymeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/relativehumiditymeasurement"
	rvccleanmodedef "github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	rvcoperationalstatedef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	rvcrunmodedef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	scenesmanagementdef "github.com/SukramJ/go-fabric/cluster/spec/scenesmanagement"
	serviceareadef "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	smokecoalarmdef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	softwarediagnosticsdef "github.com/SukramJ/go-fabric/cluster/spec/softwarediagnostics"
	soilmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
	switchclusterdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	targetnavigatordef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	temperaturealarmdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	temperaturecontroldef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	temperaturemeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	thermostatuserinterfaceconfigurationdef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	timeformatlocalizationdef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	timesynchronizationdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	totalvolatileorganiccompoundsconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/totalvolatileorganiccompoundsconcentrationmeasurement"
	unitlocalizationdef "github.com/SukramJ/go-fabric/cluster/spec/unitlocalization"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	valveconfigurationandcontroldef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	wakeonlandef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	waterheatermanagementdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	waterheatermodedef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
	watertanklevelmonitoringdef "github.com/SukramJ/go-fabric/cluster/spec/watertanklevelmonitoring"
	wifinetworkdiagnosticsdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	wifinetworkmanagementdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkmanagement"
	windowcoveringdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
)

// hosted declares one of the fleet's own servers as its cluster's
// HostServer — the declaration's route for a server the host writes
// itself (device.WithServer takes only a cluster without a generated
// package). A stopgap of brief BU-a: the bridge moves to the declaration's
// own fields (Initial, the ports, the handlers) with BU-b and this table
// goes with it. A cluster the table does not list goes to device.WithServer.
func hosted(srv contract.ClusterServer) device.Declaration {
	if declare, ok := hostedDeclarations[srv.MatterClusterID()]; ok {
		return declare(srv)
	}
	return device.WithServer(srv)
}

// hostedDeclarations maps a cluster id to its declaration of a host server.
var hostedDeclarations = map[uint32]func(contract.ClusterServer) device.Declaration{
	identifydef.ClusterID:     func(srv contract.ClusterServer) device.Declaration { return identifydef.Declare{HostServer: srv} },
	groupsdef.ClusterID:       func(srv contract.ClusterServer) device.Declaration { return groupsdef.Declare{HostServer: srv} },
	onoffdef.ClusterID:        func(srv contract.ClusterServer) device.Declaration { return onoffdef.Declare{HostServer: srv} },
	levelcontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return levelcontroldef.Declare{HostServer: srv} },
	descriptordef.ClusterID:   func(srv contract.ClusterServer) device.Declaration { return descriptordef.Declare{HostServer: srv} },
	bindingdef.ClusterID:      func(srv contract.ClusterServer) device.Declaration { return bindingdef.Declare{HostServer: srv} },
	actionsdef.ClusterID:      func(srv contract.ClusterServer) device.Declaration { return actionsdef.Declare{HostServer: srv} },
	basicinformationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return basicinformationdef.Declare{HostServer: srv}
	},
	otasoftwareupdateproviderdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return otasoftwareupdateproviderdef.Declare{HostServer: srv}
	},
	otasoftwareupdaterequestordef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return otasoftwareupdaterequestordef.Declare{HostServer: srv}
	},
	localizationconfigurationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return localizationconfigurationdef.Declare{HostServer: srv}
	},
	timeformatlocalizationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return timeformatlocalizationdef.Declare{HostServer: srv}
	},
	unitlocalizationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return unitlocalizationdef.Declare{HostServer: srv}
	},
	powersourceconfigurationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return powersourceconfigurationdef.Declare{HostServer: srv}
	},
	powersourcedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return powersourcedef.Declare{HostServer: srv} },
	generalcommissioningdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return generalcommissioningdef.Declare{HostServer: srv}
	},
	networkcommissioningdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return networkcommissioningdef.Declare{HostServer: srv}
	},
	diagnosticlogsdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return diagnosticlogsdef.Declare{HostServer: srv} },
	generaldiagnosticsdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return generaldiagnosticsdef.Declare{HostServer: srv}
	},
	softwarediagnosticsdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return softwarediagnosticsdef.Declare{HostServer: srv}
	},
	wifinetworkdiagnosticsdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return wifinetworkdiagnosticsdef.Declare{HostServer: srv}
	},
	ethernetnetworkdiagnosticsdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return ethernetnetworkdiagnosticsdef.Declare{HostServer: srv}
	},
	timesynchronizationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return timesynchronizationdef.Declare{HostServer: srv}
	},
	bridgeddevicebasicinformationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return bridgeddevicebasicinformationdef.Declare{HostServer: srv}
	},
	switchclusterdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return switchclusterdef.Declare{HostServer: srv} },
	administratorcommissioningdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return administratorcommissioningdef.Declare{HostServer: srv}
	},
	fixedlabeldef.ClusterID:    func(srv contract.ClusterServer) device.Declaration { return fixedlabeldef.Declare{HostServer: srv} },
	userlabeldef.ClusterID:     func(srv contract.ClusterServer) device.Declaration { return userlabeldef.Declare{HostServer: srv} },
	booleanstatedef.ClusterID:  func(srv contract.ClusterServer) device.Declaration { return booleanstatedef.Declare{HostServer: srv} },
	icdmanagementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return icdmanagementdef.Declare{HostServer: srv} },
	ovencavityoperationalstatedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return ovencavityoperationalstatedef.Declare{HostServer: srv}
	},
	ovenmodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return ovenmodedef.Declare{HostServer: srv} },
	laundrydryercontrolsdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return laundrydryercontrolsdef.Declare{HostServer: srv}
	},
	modeselectdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return modeselectdef.Declare{HostServer: srv} },
	laundrywashermodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return laundrywashermodedef.Declare{HostServer: srv}
	},
	refrigeratorandtemperaturecontrolledcabinetmodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return refrigeratorandtemperaturecontrolledcabinetmodedef.Declare{HostServer: srv}
	},
	laundrywashercontrolsdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return laundrywashercontrolsdef.Declare{HostServer: srv}
	},
	rvcrunmodedef.ClusterID:   func(srv contract.ClusterServer) device.Declaration { return rvcrunmodedef.Declare{HostServer: srv} },
	rvccleanmodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return rvccleanmodedef.Declare{HostServer: srv} },
	temperaturecontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return temperaturecontroldef.Declare{HostServer: srv}
	},
	refrigeratoralarmdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return refrigeratoralarmdef.Declare{HostServer: srv}
	},
	dishwashermodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return dishwashermodedef.Declare{HostServer: srv} },
	airqualitydef.ClusterID:     func(srv contract.ClusterServer) device.Declaration { return airqualitydef.Declare{HostServer: srv} },
	smokecoalarmdef.ClusterID:   func(srv contract.ClusterServer) device.Declaration { return smokecoalarmdef.Declare{HostServer: srv} },
	dishwasheralarmdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return dishwasheralarmdef.Declare{HostServer: srv}
	},
	microwaveovenmodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return microwaveovenmodedef.Declare{HostServer: srv}
	},
	microwaveovencontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return microwaveovencontroldef.Declare{HostServer: srv}
	},
	operationalstatedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return operationalstatedef.Declare{HostServer: srv}
	},
	rvcoperationalstatedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return rvcoperationalstatedef.Declare{HostServer: srv}
	},
	scenesmanagementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return scenesmanagementdef.Declare{HostServer: srv}
	},
	temperaturealarmdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return temperaturealarmdef.Declare{HostServer: srv}
	},
	groupcastdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return groupcastdef.Declare{HostServer: srv} },
	hepafiltermonitoringdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return hepafiltermonitoringdef.Declare{HostServer: srv}
	},
	activatedcarbonfiltermonitoringdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return activatedcarbonfiltermonitoringdef.Declare{HostServer: srv}
	},
	watertanklevelmonitoringdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return watertanklevelmonitoringdef.Declare{HostServer: srv}
	},
	booleanstateconfigurationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return booleanstateconfigurationdef.Declare{HostServer: srv}
	},
	valveconfigurationandcontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return valveconfigurationandcontroldef.Declare{HostServer: srv}
	},
	electricalpowermeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return electricalpowermeasurementdef.Declare{HostServer: srv}
	},
	electricalenergymeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return electricalenergymeasurementdef.Declare{HostServer: srv}
	},
	waterheatermanagementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return waterheatermanagementdef.Declare{HostServer: srv}
	},
	commoditypricedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return commoditypricedef.Declare{HostServer: srv} },
	messagesdef.ClusterID:       func(srv contract.ClusterServer) device.Declaration { return messagesdef.Declare{HostServer: srv} },
	deviceenergymanagementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return deviceenergymanagementdef.Declare{HostServer: srv}
	},
	energyevsedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return energyevsedef.Declare{HostServer: srv} },
	energypreferencedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return energypreferencedef.Declare{HostServer: srv}
	},
	powertopologydef.ClusterID:  func(srv contract.ClusterServer) device.Declaration { return powertopologydef.Declare{HostServer: srv} },
	energyevsemodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return energyevsemodedef.Declare{HostServer: srv} },
	waterheatermodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return waterheatermodedef.Declare{HostServer: srv}
	},
	deviceenergymanagementmodedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return deviceenergymanagementmodedef.Declare{HostServer: srv}
	},
	electricalgridconditionsdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return electricalgridconditionsdef.Declare{HostServer: srv}
	},
	doorlockdef.ClusterID:       func(srv contract.ClusterServer) device.Declaration { return doorlockdef.Declare{HostServer: srv} },
	windowcoveringdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return windowcoveringdef.Declare{HostServer: srv} },
	closurecontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return closurecontroldef.Declare{HostServer: srv} },
	closuredimensiondef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return closuredimensiondef.Declare{HostServer: srv}
	},
	serviceareadef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return serviceareadef.Declare{HostServer: srv} },
	pumpconfigurationandcontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return pumpconfigurationandcontroldef.Declare{HostServer: srv}
	},
	thermostatdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return thermostatdef.Declare{HostServer: srv} },
	fancontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return fancontroldef.Declare{HostServer: srv} },
	thermostatuserinterfaceconfigurationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return thermostatuserinterfaceconfigurationdef.Declare{HostServer: srv}
	},
	colorcontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return colorcontroldef.Declare{HostServer: srv} },
	illuminancemeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return illuminancemeasurementdef.Declare{HostServer: srv}
	},
	temperaturemeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return temperaturemeasurementdef.Declare{HostServer: srv}
	},
	pressuremeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return pressuremeasurementdef.Declare{HostServer: srv}
	},
	flowmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return flowmeasurementdef.Declare{HostServer: srv}
	},
	relativehumiditymeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return relativehumiditymeasurementdef.Declare{HostServer: srv}
	},
	occupancysensingdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return occupancysensingdef.Declare{HostServer: srv}
	},
	carbonmonoxideconcentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return carbonmonoxideconcentrationmeasurementdef.Declare{HostServer: srv}
	},
	carbondioxideconcentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return carbondioxideconcentrationmeasurementdef.Declare{HostServer: srv}
	},
	nitrogendioxideconcentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return nitrogendioxideconcentrationmeasurementdef.Declare{HostServer: srv}
	},
	ozoneconcentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return ozoneconcentrationmeasurementdef.Declare{HostServer: srv}
	},
	pm25concentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return pm25concentrationmeasurementdef.Declare{HostServer: srv}
	},
	formaldehydeconcentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return formaldehydeconcentrationmeasurementdef.Declare{HostServer: srv}
	},
	pm1concentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return pm1concentrationmeasurementdef.Declare{HostServer: srv}
	},
	pm10concentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return pm10concentrationmeasurementdef.Declare{HostServer: srv}
	},
	totalvolatileorganiccompoundsconcentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return totalvolatileorganiccompoundsconcentrationmeasurementdef.Declare{HostServer: srv}
	},
	radonconcentrationmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return radonconcentrationmeasurementdef.Declare{HostServer: srv}
	},
	soilmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return soilmeasurementdef.Declare{HostServer: srv}
	},
	ambientcontextsensingdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return ambientcontextsensingdef.Declare{HostServer: srv}
	},
	wifinetworkmanagementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return wifinetworkmanagementdef.Declare{HostServer: srv}
	},
	wakeonlandef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return wakeonlandef.Declare{HostServer: srv} },
	channeldef.ClusterID:   func(srv contract.ClusterServer) device.Declaration { return channeldef.Declare{HostServer: srv} },
	targetnavigatordef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return targetnavigatordef.Declare{HostServer: srv}
	},
	mediaplaybackdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return mediaplaybackdef.Declare{HostServer: srv} },
	mediainputdef.ClusterID:    func(srv contract.ClusterServer) device.Declaration { return mediainputdef.Declare{HostServer: srv} },
	lowpowerdef.ClusterID:      func(srv contract.ClusterServer) device.Declaration { return lowpowerdef.Declare{HostServer: srv} },
	keypadinputdef.ClusterID:   func(srv contract.ClusterServer) device.Declaration { return keypadinputdef.Declare{HostServer: srv} },
	contentlauncherdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return contentlauncherdef.Declare{HostServer: srv}
	},
	audiooutputdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return audiooutputdef.Declare{HostServer: srv} },
	applicationlauncherdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return applicationlauncherdef.Declare{HostServer: srv}
	},
	applicationbasicdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return applicationbasicdef.Declare{HostServer: srv}
	},
	accountlogindef.ClusterID:   func(srv contract.ClusterServer) device.Declaration { return accountlogindef.Declare{HostServer: srv} },
	contentcontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return contentcontroldef.Declare{HostServer: srv} },
	contentappobserverdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return contentappobserverdef.Declare{HostServer: srv}
	},
	chimedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return chimedef.Declare{HostServer: srv} },
	commoditytariffdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return commoditytariffdef.Declare{HostServer: srv}
	},
	ecosysteminformationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return ecosysteminformationdef.Declare{HostServer: srv}
	},
	commissionercontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return commissionercontroldef.Declare{HostServer: srv}
	},
	meteridentificationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return meteridentificationdef.Declare{HostServer: srv}
	},
	commoditymeteringdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return commoditymeteringdef.Declare{HostServer: srv}
	},
}
