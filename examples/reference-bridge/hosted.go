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
// goes with it. nil for a cluster the table does not list.
func hosted(srv contract.ClusterServer) device.Declaration {
	switch srv.MatterClusterID() {
	case levelcontroldef.ClusterID:
		return levelcontroldef.Declare{HostServer: srv}
	case fixedlabeldef.ClusterID:
		return fixedlabeldef.Declare{HostServer: srv}
	case booleanstatedef.ClusterID:
		return booleanstatedef.Declare{HostServer: srv}
	case ovencavityoperationalstatedef.ClusterID:
		return ovencavityoperationalstatedef.Declare{HostServer: srv}
	case ovenmodedef.ClusterID:
		return ovenmodedef.Declare{HostServer: srv}
	case laundrydryercontrolsdef.ClusterID:
		return laundrydryercontrolsdef.Declare{HostServer: srv}
	case modeselectdef.ClusterID:
		return modeselectdef.Declare{HostServer: srv}
	case laundrywashermodedef.ClusterID:
		return laundrywashermodedef.Declare{HostServer: srv}
	case refrigeratorandtemperaturecontrolledcabinetmodedef.ClusterID:
		return refrigeratorandtemperaturecontrolledcabinetmodedef.Declare{HostServer: srv}
	case laundrywashercontrolsdef.ClusterID:
		return laundrywashercontrolsdef.Declare{HostServer: srv}
	case rvcrunmodedef.ClusterID:
		return rvcrunmodedef.Declare{HostServer: srv}
	case rvccleanmodedef.ClusterID:
		return rvccleanmodedef.Declare{HostServer: srv}
	case temperaturecontroldef.ClusterID:
		return temperaturecontroldef.Declare{HostServer: srv}
	case refrigeratoralarmdef.ClusterID:
		return refrigeratoralarmdef.Declare{HostServer: srv}
	case airqualitydef.ClusterID:
		return airqualitydef.Declare{HostServer: srv}
	case smokecoalarmdef.ClusterID:
		return smokecoalarmdef.Declare{HostServer: srv}
	case dishwasheralarmdef.ClusterID:
		return dishwasheralarmdef.Declare{HostServer: srv}
	case microwaveovenmodedef.ClusterID:
		return microwaveovenmodedef.Declare{HostServer: srv}
	case microwaveovencontroldef.ClusterID:
		return microwaveovencontroldef.Declare{HostServer: srv}
	case operationalstatedef.ClusterID:
		return operationalstatedef.Declare{HostServer: srv}
	case rvcoperationalstatedef.ClusterID:
		return rvcoperationalstatedef.Declare{HostServer: srv}
	case hepafiltermonitoringdef.ClusterID:
		return hepafiltermonitoringdef.Declare{HostServer: srv}
	case activatedcarbonfiltermonitoringdef.ClusterID:
		return activatedcarbonfiltermonitoringdef.Declare{HostServer: srv}
	case booleanstateconfigurationdef.ClusterID:
		return booleanstateconfigurationdef.Declare{HostServer: srv}
	case valveconfigurationandcontroldef.ClusterID:
		return valveconfigurationandcontroldef.Declare{HostServer: srv}
	case electricalpowermeasurementdef.ClusterID:
		return electricalpowermeasurementdef.Declare{HostServer: srv}
	case electricalenergymeasurementdef.ClusterID:
		return electricalenergymeasurementdef.Declare{HostServer: srv}
	case waterheatermanagementdef.ClusterID:
		return waterheatermanagementdef.Declare{HostServer: srv}
	case commoditypricedef.ClusterID:
		return commoditypricedef.Declare{HostServer: srv}
	case deviceenergymanagementdef.ClusterID:
		return deviceenergymanagementdef.Declare{HostServer: srv}
	case energyevsedef.ClusterID:
		return energyevsedef.Declare{HostServer: srv}
	case energypreferencedef.ClusterID:
		return energypreferencedef.Declare{HostServer: srv}
	case energyevsemodedef.ClusterID:
		return energyevsemodedef.Declare{HostServer: srv}
	case waterheatermodedef.ClusterID:
		return waterheatermodedef.Declare{HostServer: srv}
	case deviceenergymanagementmodedef.ClusterID:
		return deviceenergymanagementmodedef.Declare{HostServer: srv}
	case electricalgridconditionsdef.ClusterID:
		return electricalgridconditionsdef.Declare{HostServer: srv}
	case doorlockdef.ClusterID:
		return doorlockdef.Declare{HostServer: srv}
	case windowcoveringdef.ClusterID:
		return windowcoveringdef.Declare{HostServer: srv}
	case closurecontroldef.ClusterID:
		return closurecontroldef.Declare{HostServer: srv}
	case closuredimensiondef.ClusterID:
		return closuredimensiondef.Declare{HostServer: srv}
	case serviceareadef.ClusterID:
		return serviceareadef.Declare{HostServer: srv}
	case pumpconfigurationandcontroldef.ClusterID:
		return pumpconfigurationandcontroldef.Declare{HostServer: srv}
	case thermostatdef.ClusterID:
		return thermostatdef.Declare{HostServer: srv}
	case fancontroldef.ClusterID:
		return fancontroldef.Declare{HostServer: srv}
	case thermostatuserinterfaceconfigurationdef.ClusterID:
		return thermostatuserinterfaceconfigurationdef.Declare{HostServer: srv}
	case colorcontroldef.ClusterID:
		return colorcontroldef.Declare{HostServer: srv}
	case flowmeasurementdef.ClusterID:
		return flowmeasurementdef.Declare{HostServer: srv}
	case carbonmonoxideconcentrationmeasurementdef.ClusterID:
		return carbonmonoxideconcentrationmeasurementdef.Declare{HostServer: srv}
	case carbondioxideconcentrationmeasurementdef.ClusterID:
		return carbondioxideconcentrationmeasurementdef.Declare{HostServer: srv}
	case nitrogendioxideconcentrationmeasurementdef.ClusterID:
		return nitrogendioxideconcentrationmeasurementdef.Declare{HostServer: srv}
	case ozoneconcentrationmeasurementdef.ClusterID:
		return ozoneconcentrationmeasurementdef.Declare{HostServer: srv}
	case pm25concentrationmeasurementdef.ClusterID:
		return pm25concentrationmeasurementdef.Declare{HostServer: srv}
	case formaldehydeconcentrationmeasurementdef.ClusterID:
		return formaldehydeconcentrationmeasurementdef.Declare{HostServer: srv}
	case pm1concentrationmeasurementdef.ClusterID:
		return pm1concentrationmeasurementdef.Declare{HostServer: srv}
	case pm10concentrationmeasurementdef.ClusterID:
		return pm10concentrationmeasurementdef.Declare{HostServer: srv}
	case totalvolatileorganiccompoundsconcentrationmeasurementdef.ClusterID:
		return totalvolatileorganiccompoundsconcentrationmeasurementdef.Declare{HostServer: srv}
	case radonconcentrationmeasurementdef.ClusterID:
		return radonconcentrationmeasurementdef.Declare{HostServer: srv}
	case wakeonlandef.ClusterID:
		return wakeonlandef.Declare{HostServer: srv}
	case channeldef.ClusterID:
		return channeldef.Declare{HostServer: srv}
	case targetnavigatordef.ClusterID:
		return targetnavigatordef.Declare{HostServer: srv}
	case mediaplaybackdef.ClusterID:
		return mediaplaybackdef.Declare{HostServer: srv}
	case mediainputdef.ClusterID:
		return mediainputdef.Declare{HostServer: srv}
	case lowpowerdef.ClusterID:
		return lowpowerdef.Declare{HostServer: srv}
	case keypadinputdef.ClusterID:
		return keypadinputdef.Declare{HostServer: srv}
	case contentlauncherdef.ClusterID:
		return contentlauncherdef.Declare{HostServer: srv}
	case audiooutputdef.ClusterID:
		return audiooutputdef.Declare{HostServer: srv}
	case applicationlauncherdef.ClusterID:
		return applicationlauncherdef.Declare{HostServer: srv}
	case commoditytariffdef.ClusterID:
		return commoditytariffdef.Declare{HostServer: srv}
	case ecosysteminformationdef.ClusterID:
		return ecosysteminformationdef.Declare{HostServer: srv}
	case meteridentificationdef.ClusterID:
		return meteridentificationdef.Declare{HostServer: srv}
	case commoditymeteringdef.ClusterID:
		return commoditymeteringdef.Declare{HostServer: srv}
	default:
		return device.WithServer(srv)
	}
}
