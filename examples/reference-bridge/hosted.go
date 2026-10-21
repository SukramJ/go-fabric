// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	activatedcarbonfiltermonitoringdef "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	airqualitydef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	applicationlauncherdef "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	audiooutputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	booleanstatedef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	booleanstateconfigurationdef "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	carbondioxideconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	carbonmonoxideconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/carbonmonoxideconcentrationmeasurement"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	closurecontroldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	closuredimensiondef "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	colorcontroldef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	commoditymeteringdef "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	commoditypricedef "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	commoditytariffdef "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	contentlauncherdef "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	deviceenergymanagementdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	deviceenergymanagementmodedef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagementmode"
	dishwasheralarmdef "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	doorlockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	ecosysteminformationdef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	electricalenergymeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	electricalgridconditionsdef "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	electricalpowermeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	energyevsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	energyevsemodedef "github.com/SukramJ/go-fabric/cluster/spec/energyevsemode"
	energypreferencedef "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	fancontroldef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	flowmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/flowmeasurement"
	formaldehydeconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/formaldehydeconcentrationmeasurement"
	hepafiltermonitoringdef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	keypadinputdef "github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	laundrydryercontrolsdef "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	laundrywashercontrolsdef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	laundrywashermodedef "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	lowpowerdef "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	mediainputdef "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	mediaplaybackdef "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	meteridentificationdef "github.com/SukramJ/go-fabric/cluster/spec/meteridentification"
	microwaveovencontroldef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	microwaveovenmodedef "github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	modeselectdef "github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	nitrogendioxideconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/nitrogendioxideconcentrationmeasurement"
	operationalstatedef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	ovencavityoperationalstatedef "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate"
	ovenmodedef "github.com/SukramJ/go-fabric/cluster/spec/ovenmode"
	ozoneconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/ozoneconcentrationmeasurement"
	pm10concentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pm10concentrationmeasurement"
	pm1concentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pm1concentrationmeasurement"
	pm25concentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/pm25concentrationmeasurement"
	pumpconfigurationandcontroldef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	radonconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/radonconcentrationmeasurement"
	refrigeratoralarmdef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	refrigeratorandtemperaturecontrolledcabinetmodedef "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	rvccleanmodedef "github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	rvcoperationalstatedef "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	rvcrunmodedef "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	serviceareadef "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	smokecoalarmdef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	targetnavigatordef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	temperaturecontroldef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	thermostatuserinterfaceconfigurationdef "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	totalvolatileorganiccompoundsconcentrationmeasurementdef "github.com/SukramJ/go-fabric/cluster/spec/totalvolatileorganiccompoundsconcentrationmeasurement"
	valveconfigurationandcontroldef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	wakeonlandef "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	waterheatermanagementdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	waterheatermodedef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
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
	levelcontroldef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return levelcontroldef.Declare{HostServer: srv} },
	fixedlabeldef.ClusterID:   func(srv contract.ClusterServer) device.Declaration { return fixedlabeldef.Declare{HostServer: srv} },
	booleanstatedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return booleanstatedef.Declare{HostServer: srv} },
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
	airqualitydef.ClusterID:   func(srv contract.ClusterServer) device.Declaration { return airqualitydef.Declare{HostServer: srv} },
	smokecoalarmdef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return smokecoalarmdef.Declare{HostServer: srv} },
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
	hepafiltermonitoringdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return hepafiltermonitoringdef.Declare{HostServer: srv}
	},
	activatedcarbonfiltermonitoringdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return activatedcarbonfiltermonitoringdef.Declare{HostServer: srv}
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
	deviceenergymanagementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return deviceenergymanagementdef.Declare{HostServer: srv}
	},
	energyevsedef.ClusterID: func(srv contract.ClusterServer) device.Declaration { return energyevsedef.Declare{HostServer: srv} },
	energypreferencedef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return energypreferencedef.Declare{HostServer: srv}
	},
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
	flowmeasurementdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return flowmeasurementdef.Declare{HostServer: srv}
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
	commoditytariffdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return commoditytariffdef.Declare{HostServer: srv}
	},
	ecosysteminformationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return ecosysteminformationdef.Declare{HostServer: srv}
	},
	meteridentificationdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return meteridentificationdef.Declare{HostServer: srv}
	},
	commoditymeteringdef.ClusterID: func(srv contract.ClusterServer) device.Declaration {
		return commoditymeteringdef.Declare{HostServer: srv}
	},
}
