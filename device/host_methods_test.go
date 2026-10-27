// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/cover"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/filter"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/lock"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/media"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/modeselect"
	"github.com/SukramJ/go-fabric/cluster/netinfra"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	h_login "github.com/SukramJ/go-fabric/cluster/spec/accountlogin"
	h_acf "github.com/SukramJ/go-fabric/cluster/spec/activatedcarbonfiltermonitoring"
	h_basic "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
	h_launcher "github.com/SukramJ/go-fabric/cluster/spec/applicationlauncher"
	h_output "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	h_binding "github.com/SukramJ/go-fabric/cluster/spec/binding"
	h_bs "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	h_bsc "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	h_av "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	h_cs "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	h_channel "github.com/SukramJ/go-fabric/cluster/spec/channel"
	h_chime "github.com/SukramJ/go-fabric/cluster/spec/chime"
	h_ccl "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	h_cd "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	h_color "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	h_meter "github.com/SukramJ/go-fabric/cluster/spec/commoditymetering"
	h_price "github.com/SukramJ/go-fabric/cluster/spec/commodityprice"
	h_tariff "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	h_content "github.com/SukramJ/go-fabric/cluster/spec/contentcontrol"
	h_launch "github.com/SukramJ/go-fabric/cluster/spec/contentlauncher"
	h_dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	h_demm "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagementmode"
	h_dwa "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	h_dwm "github.com/SukramJ/go-fabric/cluster/spec/dishwashermode"
	h_lock "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	h_eco "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	h_grid "github.com/SukramJ/go-fabric/cluster/spec/electricalgridconditions"
	h_evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	h_evsem "github.com/SukramJ/go-fabric/cluster/spec/energyevsemode"
	h_ep "github.com/SukramJ/go-fabric/cluster/spec/energypreference"
	h_fan "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	h_fixed "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	h_hepa "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	h_keypad "github.com/SukramJ/go-fabric/cluster/spec/keypadinput"
	h_ldc "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	h_washer "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
	h_lwm "github.com/SukramJ/go-fabric/cluster/spec/laundrywashermode"
	h_level "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	h_lowpower "github.com/SukramJ/go-fabric/cluster/spec/lowpower"
	h_input "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	h_mp "github.com/SukramJ/go-fabric/cluster/spec/mediaplayback"
	h_mwoc "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	h_mwom "github.com/SukramJ/go-fabric/cluster/spec/microwaveovenmode"
	h_ms "github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	h_onoff "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	h_op "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	h_oven "github.com/SukramJ/go-fabric/cluster/spec/ovencavityoperationalstate"
	h_ovenm "github.com/SukramJ/go-fabric/cluster/spec/ovenmode"
	h_pump "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	h_rfa "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	h_rtcc "github.com/SukramJ/go-fabric/cluster/spec/refrigeratorandtemperaturecontrolledcabinetmode"
	h_rvcc "github.com/SukramJ/go-fabric/cluster/spec/rvccleanmode"
	h_rvcop "github.com/SukramJ/go-fabric/cluster/spec/rvcoperationalstate"
	h_rvcr "github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	h_sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	h_smoke "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	h_switch "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	h_target "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	h_ta "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	h_tc "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	h_thermostat "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	h_tui "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	h_tbrm "github.com/SukramJ/go-fabric/cluster/spec/threadborderroutermanagement"
	h_tnd "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	h_tdir "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	h_user "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	h_valve "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	h_wol "github.com/SukramJ/go-fabric/cluster/spec/wakeonlan"
	h_whmg "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	h_whm "github.com/SukramJ/go-fabric/cluster/spec/waterheatermode"
	h_wr "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	h_wifi "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkmanagement"
	h_wc "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	h_zm "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	switchserver "github.com/SukramJ/go-fabric/cluster/switch"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/cluster/threadbr"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/contract"
)

// hostCase is one kit server of the device's defaults and the decision on
// each of its exported methods (brief CK-2): the ones in its cluster's
// Host, and covered — the ones Set, Get, MarkChanged, OnWrite,
// Handle<Command> or Emit<Event> reach, a derivation of what Get reads,
// or the kit's own wiring — and deferred, the host methods whose types
// live only in the kit's package so far. A method in none fails the test:
// a new kit method is decided before it ships.
type hostCase struct {
	kit      reflect.Type
	host     *hostSpec // the cluster's Host; nil without one
	covered  []string
	deferred []string
}

// hostSpec is a cluster's Host interface and its typed server's Host
// over a server.
type hostSpec struct {
	typ reflect.Type
	of  func(contract.ClusterServer) any
}

// hostOf is the hostSpec of the cluster whose typed server wrap builds.
func hostOf[H any, S interface{ Host() H }](wrap func(contract.ClusterServer) S) *hostSpec {
	return &hostSpec{typ: reflect.TypeFor[H](), of: func(srv contract.ClusterServer) any {
		return any(wrap(srv).Host()) // a nil Host is a nil any
	}}
}

// kitOf is the reflect type of a kit server.
func kitOf[T contract.ClusterServer]() reflect.Type { return reflect.TypeFor[T]() }

// Set covers every attribute setter and getter of these servers; the
// lists name the kit methods Set / Get reach or that are wiring.
var hostCases = map[uint32]hostCase{
	h_onoff.ClusterID: {kit: kitOf[*onoff.Server](), covered: []string{"OnOff", "Set", "SetScenes"}},
	h_level.ClusterID: {kit: kitOf[*levelcontrol.Server]()},
	// Binding: Persist and RemoveFabric are the node's wiring.
	h_binding.ClusterID: {kit: kitOf[*core.Binding](), host: hostOf[h_binding.Host](h_binding.Wrap), covered: []string{"Persist", "RemoveFabric"}},
	h_switch.ClusterID:  {kit: kitOf[*switchserver.Server](), host: hostOf[h_switch.Host](h_switch.Wrap), covered: []string{"CurrentPosition", "Endpoint"}},
	h_lock.ClusterID:    {kit: kitOf[*lock.DoorLockServer](), host: hostOf[h_lock.Host](h_lock.Wrap), covered: []string{"ReportAlarm", "SetLockState", "User"}},
	h_wc.ClusterID:      {kit: kitOf[*cover.WindowCoveringServer](), covered: []string{"SetCurrentPosition", "SetOperationalStatus", "SetTargetPosition"}},
	h_thermostat.ClusterID: {kit: kitOf[*thermo.ThermostatServer](), covered: []string{
		"ActivePresetHandle", "ActiveScheduleHandle", "Presets", "Schedules",
		"SetAbsoluteSetpointLimit", "SetActivePresetHandle", "SetActiveScheduleHandle", "SetLocalTemperature",
		"SetOccupied", "SetOccupiedCoolingSetpoint", "SetOccupiedHeatingSetpoint", "SetPresets", "SetSchedules",
		"SetSystemMode", "SetTemperatureSetpointHold", "SetThermostatRunningState",
	}},
	h_color.ClusterID:  {kit: kitOf[*light.ColorControlServer](), covered: []string{"Color", "Set", "SetColorWriter", "SetWriter", "SyncColorTemperatureWithLevel"}},
	h_eco.ClusterID:    {kit: kitOf[*core.EcosystemInformation](), host: hostOf[h_eco.Host](h_eco.Wrap)},
	h_fixed.ClusterID:  {kit: kitOf[*spec.Server]()},
	h_user.ClusterID:   {kit: kitOf[*spec.Server]()},
	h_fan.ClusterID:    {kit: kitOf[*fan.Server]()},
	h_wifi.ClusterID:   {kit: kitOf[*netinfra.WiFiManagementServer](), host: hostOf[h_wifi.Host](h_wifi.Wrap), covered: []string{"HasNetworkCredentials", "SSID"}},
	h_tdir.ClusterID:   {kit: kitOf[*netinfra.ThreadDirectoryServer](), host: hostOf[h_tdir.Host](h_tdir.Wrap), covered: []string{"SetPreferredExtendedPanID"}},
	h_washer.ClusterID: {kit: kitOf[*appliance.WasherServer](), covered: []string{"NumberOfRinses", "SetNumberOfRinses", "SetSpinSpeedCurrent", "SpinSpeedCurrent"}},
	h_rfa.ClusterID:    {kit: kitOf[*alarmbase.Server](), host: hostOf[h_rfa.Host](h_rfa.Wrap), covered: alarmBaseCovered},
	h_dwa.ClusterID:    {kit: kitOf[*alarmbase.Server](), host: hostOf[h_dwa.Host](h_dwa.Wrap), covered: alarmBaseCovered},
	h_ta.ClusterID:     {kit: kitOf[*alarmbase.Server](), host: hostOf[h_ta.Host](h_ta.Wrap), covered: alarmBaseCovered},
	h_hepa.ClusterID:   {kit: kitOf[*filter.Server](), covered: filterCovered},
	h_acf.ClusterID:    {kit: kitOf[*filter.Server](), covered: filterCovered},
	h_bsc.ClusterID:    {kit: kitOf[*boolcfg.Server](), host: hostOf[h_bsc.Host](h_bsc.Wrap), covered: []string{"ClearAllAlarms", "SetAlarmsActive", "SetAllEnabledAlarmsActive", "SetCurrentSensitivityLevel", "State"}},
	h_price.ClusterID:  {kit: kitOf[*energy.CommodityPriceServer](), covered: []string{"CurrentPrice", "SetCurrency", "SetCurrentPrice", "SetForecast", "SetTariffUnit"}},
	h_evse.ClusterID: {kit: kitOf[*energy.EvseServer](), host: hostOf[h_evse.Host](h_evse.Wrap), covered: []string{
		"BatteryCapacity", "CircuitCapacity", "FaultState", "MaximumChargeCurrent", "MaximumDischargeCurrent", "SessionID",
		"SetBatteryCapacity", "SetChargeSchedule", "SetCircuitCapacity", "SetStateOfCharge", "SetUserMaximumChargeCurrent",
		"SetVehicleID", "State", "StateOfCharge", "SupplyState", "UserMaximumChargeCurrent",
	}},
	h_grid.ClusterID:  {kit: kitOf[*energy.GridConditionsServer](), covered: []string{"SetCurrentConditions", "SetForecastConditions", "SetLocalGenerationAvailable"}},
	h_tui.ClusterID:   {kit: kitOf[*thermo.ThermostatUIServer]()},
	h_meter.ClusterID: {kit: kitOf[*energy.CommodityMeteringServer](), covered: []string{"MaximumMeteredQuantities", "MeteredQuantity", "MeteredQuantityTimestamp", "SetMaximumMeteredQuantities", "SetMeteredQuantity", "SetMeteredQuantityTimestamp", "SetTariffUnit", "TariffUnit"}},
	// The camera servers (cluster/camera).
	h_wr.ClusterID: {kit: kitOf[*camera.Requestor](), host: hostOf[h_wr.Host](h_wr.Wrap), covered: []string{"Sessions"}},
	// The Thread border router (cluster/threadbr): BorderRouter is the
	// host's own port, the fail-safe methods the node's wiring.
	h_tbrm.ClusterID: {kit: kitOf[*threadbr.ManagementServer](), host: hostOf[h_tbrm.Host](h_tbrm.Wrap), covered: []string{
		"AttachFailSafe", "BorderRouter", "OnCommissioningComplete", "OnFailSafeExpired",
	}},
	h_tnd.ClusterID:   {kit: kitOf[*threadbr.DiagnosticsServer](), host: hostOf[h_tnd.Host](h_tnd.Wrap)},
	h_chime.ClusterID: {kit: kitOf[*camera.Chime](), host: hostOf[h_chime.Host](h_chime.Wrap), covered: []string{"Enabled", "SelectedChime"}},
	h_av.ClusterID: {kit: kitOf[*camera.AvSettings](), host: hostOf[h_av.Host](h_av.Wrap), covered: []string{
		"MovementState", "NextPresetID", "Position", "Presets", "SetPanMax", "SetPanMin", "SetPosition", "SetTiltMax", "SetTiltMin", "SetZoomMax", "Streams",
	}},
	h_cs.ClusterID: {kit: kitOf[*camera.AvStream](), host: hostOf[h_cs.Host](h_cs.Wrap), covered: []string{"AudioStreams", "SnapshotStreams", "StreamUsagePriorities", "VideoStreams"}},
	h_zm.ClusterID: {kit: kitOf[*camera.Zones](), host: hostOf[h_zm.Host](h_zm.Wrap), covered: []string{"Sensitivity", "SetSensitivity", "TriggerFor", "TriggerList", "ZoneList"}},
	// The media servers: their event helpers are Emit<Event>'s.
	h_mp.ClusterID:       {kit: kitOf[*media.PlaybackServer](), covered: []string{"EmitStateChanged"}},
	h_keypad.ClusterID:   {kit: kitOf[*media.KeypadServer]()},
	h_launch.ClusterID:   {kit: kitOf[*media.ContentLauncherServer]()},
	h_launcher.ClusterID: {kit: kitOf[*media.AppLauncherServer]()},
	h_input.ClusterID:    {kit: kitOf[*media.InputsServer]()},
	h_output.ClusterID:   {kit: kitOf[*media.OutputsServer]()},
	h_channel.ClusterID:  {kit: kitOf[*media.ChannelsServer]()},
	h_target.ClusterID:   {kit: kitOf[*media.TargetsServer](), covered: []string{"EmitTargetUpdated"}},
	h_lowpower.ClusterID: {kit: kitOf[*media.LowPowerServer]()},
	h_login.ClusterID:    {kit: kitOf[*media.AccountLoginServer](), covered: []string{"EmitLoggedOut"}},
	h_wol.ClusterID:      {kit: kitOf[*media.WakeOnLanServer]()},
	h_basic.ClusterID:    {kit: kitOf[*media.AppBasicServer]()},
	h_content.ClusterID:  {kit: kitOf[*media.ContentControlServer](), covered: []string{"EmitEnteringBlockContentTimeWindow", "EmitRemainingScreenTimeExpired"}},
	h_pump.ClusterID:     {kit: kitOf[*pump.Server](), covered: []string{"Endpoint", "SupportsControlMode", "SupportsOperationMode"}},
	h_valve.ClusterID:    {kit: kitOf[*valve.Server]()},
	h_ms.ClusterID:       {kit: kitOf[*modeselect.Server]()},
	h_sa.ClusterID: {kit: kitOf[*servicearea.Server](), host: hostOf[h_sa.Host](h_sa.Wrap), covered: []string{
		"SetCurrentArea", "SetEstimatedEndTime", "SetProgress", "SetSelectedAreas", "SetSupportedAreas", "SetSupportedMaps",
	}},
	h_ccl.ClusterID:  {kit: kitOf[*closure.ControlServer](), covered: []string{"FeatureMap", "ReportError", "SetCurrentPosition", "SetEngaged", "SetErrorList", "SetMainState"}},
	h_cd.ClusterID:   {kit: kitOf[*closure.DimensionServer](), covered: []string{"CurrentState", "SetCurrentState", "SetLimitRange", "SetTargetState", "SetUnitRange", "TargetState"}},
	h_tc.ClusterID:   {kit: kitOf[*thermo.TemperatureControlServer](), covered: []string{"SetSelectedTemperatureLevel", "SetTemperatureSetpoint"}},
	h_mwoc.ClusterID: {kit: kitOf[*appliance.MicrowaveServer](), covered: []string{"CookTime", "MaxCookTime", "SetCookTime", "SetPowerSetting", "SetSelectedWattIndex"}},
	h_mwom.ClusterID: {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_whmg.ClusterID: {kit: kitOf[*energy.WaterHeaterManagementServer](), covered: []string{
		"BoostState", "EndBoost", "SetEstimatedHeatRequired", "SetHeatDemand", "SetTankPercentage", "SetTankVolume", "State",
	}},
	h_dem.ClusterID: {kit: kitOf[*energy.DeviceEnergyManagementServer](), host: hostOf[h_dem.Host](h_dem.Wrap), covered: []string{
		"AbsMaxPower", "AbsMinPower", "EsaState", "Forecast", "OptOutState", "PowerAdjustmentCapability",
		"SetAbsMaxPower", "SetAbsMinPower", "SetEsaState", "SetForecast", "SetOptOutState", "SetPowerAdjustmentCapability",
	}},
	h_ldc.ClusterID:    {kit: kitOf[*appliance.DryerServer](), covered: []string{"SelectedDrynessLevel", "SetSelectedDrynessLevel"}},
	h_smoke.ClusterID:  {kit: kitOf[*alarm.Server](), host: hostOf[h_smoke.Host](h_smoke.Wrap), covered: []string{"ExpressedStateOf"}},
	h_tariff.ClusterID: {kit: kitOf[*energy.CommodityTariffServer](), host: hostOf[h_tariff.Host](h_tariff.Wrap), covered: []string{"ClearTariff", "UpdateTariff"}},
	h_ep.ClusterID:     {kit: kitOf[*energy.PreferenceServer](), covered: []string{"CurrentEnergyBalance", "CurrentLowPowerModeSensitivity"}},
	h_bs.ClusterID:     {kit: kitOf[*measurement.HeldBooleanState]()},
	// The ModeBase derivations (cluster/modebase).
	h_lwm.ClusterID:   {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_dwm.ClusterID:   {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_rvcr.ClusterID:  {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_rvcc.ClusterID:  {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_ovenm.ClusterID: {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_rtcc.ClusterID:  {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_evsem.ClusterID: {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_whm.ClusterID:   {kit: kitOf[*modebase.Server](), covered: modeCovered},
	h_demm.ClusterID:  {kit: kitOf[*modebase.Server](), covered: modeCovered},
	// The OperationalState derivations (cluster/opstate).
	h_op.ClusterID:    {kit: kitOf[*opstate.Server](), covered: opstateCovered},
	h_rvcop.ClusterID: {kit: kitOf[*opstate.Server](), covered: opstateCovered},
	h_oven.ClusterID:  {kit: kitOf[*opstate.Server](), covered: opstateCovered},
}

var (
	alarmBaseCovered = []string{"Latch", "Mask", "SetMask", "SetState", "SetThresholds", "State", "Supported", "Thresholds"}
	filterCovered    = []string{"Notify", "SetState", "State"}
	modeCovered      = []string{"Revision", "SetCurrentMode"}
	opstateCovered   = []string{"EmitOperationCompletion", "SetCountdownTime", "SetCurrentPhase", "SetOperationalError", "SetOperationalState", "SetPhaseList"}
)

// typedLayer are the interfaces whose methods are the typed layer's and
// the contract's, never a Host's.
var typedLayer = []reflect.Type{
	reflect.TypeFor[contract.ClusterServer](),
	reflect.TypeFor[contract.FabricScopedReader](),
	reflect.TypeFor[contract.ClusterAttributeLister](),
	reflect.TypeFor[contract.ClusterCommandLister](),
	reflect.TypeFor[contract.ClusterDataVersion](),
	reflect.TypeFor[contract.ClusterEventLister](),
	reflect.TypeFor[contract.ClusterAttributeReadPrivilege](),
	reflect.TypeFor[contract.ClusterAttributeWritePrivilege](),
	reflect.TypeFor[contract.ClusterCommandInvokePrivilege](),
	reflect.TypeFor[contract.TimedCommander](),
	reflect.TypeFor[contract.ElectricalReadings](),
	reflect.TypeFor[contract.MeasurementSource](),
	reflect.TypeFor[contract.FloatMeasurementSource](),
	reflect.TypeFor[contract.BoolMeasurementSource](),
	reflect.TypeFor[contract.ChangeNotifier](),
	reflect.TypeFor[contract.AttributeChangeNotifier](),
	reflect.TypeFor[contract.AttributeReportNotifier](),
	reflect.TypeFor[contract.SelfReportedAttributeLister](),
	reflect.TypeFor[contract.ClusterQuiescer](),
	reflect.TypeFor[contract.EventEmitter](),
	reflect.TypeFor[contract.EventReceiver](),
	reflect.TypeFor[contract.EligibilitySource](),
	reflect.TypeFor[contract.SwitchEventEmitter](),
	reflect.TypeFor[contract.SwitchPositionSink](),
	reflect.TypeFor[contract.IdentifyHandler](),
	reflect.TypeFor[contract.EndpointSource](),
	reflect.TypeFor[spec.AttributeSetter](),
	reflect.TypeFor[spec.CommandRegistrar](),
	reflect.TypeFor[spec.EventEmitting](),
	reflect.TypeFor[spec.BeforeWriteSetter](),
	reflect.TypeFor[spec.ChangeMarker](),
}

// kitMethods are the exported methods of a kit server beyond the typed
// layer, the contract (but for an interface its Host embeds), the methods
// its embedded types bring, and the
// bridge's SetEndpoint.
func kitMethods(kit, host reflect.Type) []string {
	if kit == reflect.TypeFor[*spec.Server]() {
		return nil // the generated server, whose methods are the typed layer's
	}
	skip := map[string]bool{"SetEndpoint": true}
	for _, it := range typedLayer {
		if !kit.Implements(it) {
			continue
		}
		for i := range it.NumMethod() {
			name := it.Method(i).Name
			if host != nil {
				if _, inHost := host.MethodByName(name); inHost {
					continue // a contract interface the Host embeds is the Host's
				}
			}
			skip[name] = true
		}
	}
	if elem := kit.Elem(); elem.Kind() == reflect.Struct {
		for i := range elem.NumField() {
			f := elem.Field(i)
			if !f.Anonymous {
				continue
			}
			ft := f.Type
			if ft.Kind() != reflect.Pointer {
				ft = reflect.PointerTo(ft)
			}
			for j := range ft.NumMethod() {
				skip[ft.Method(j).Name] = true
			}
		}
	}
	var out []string
	for i := range kit.NumMethod() {
		if name := kit.Method(i).Name; !skip[name] && !strings.HasPrefix(name, "Matter") {
			out = append(out, name)
		}
	}
	return out
}

// TestEveryKitServerHasItsHostDecided holds every kit server of the
// device's defaults to the decision on its methods: each exported method
// beyond the typed layer and the contract is in its cluster's Host or
// named covered, and nothing else.
func TestEveryKitServerHasItsHostDecided(t *testing.T) {
	t.Parallel()
	for id := range selfHeldDefaults {
		if _, ok := hostCases[id]; !ok {
			t.Errorf("cluster 0x%04X: a kit default without a host decision", id)
		}
	}
	for id, c := range hostCases {
		if _, ok := selfHeldDefaults[id]; !ok {
			t.Errorf("cluster 0x%04X: decided but not a kit default", id)
		}
		var inHost []string
		if c.host != nil {
			hostType := c.host.typ
			if !c.kit.Implements(hostType) {
				t.Errorf("cluster 0x%04X: %v does not implement %v", id, c.kit, hostType)
			}
			for i := range hostType.NumMethod() {
				inHost = append(inHost, hostType.Method(i).Name)
			}
		}
		var host reflect.Type
		if c.host != nil {
			host = c.host.typ
		}
		got := kitMethods(c.kit, host)
		want := slices.Sorted(slices.Values(slices.Concat(inHost, c.covered, c.deferred)))
		if !slices.Equal(got, want) {
			t.Errorf("cluster 0x%04X (%v): methods %v, decided %v (Host %v)", id, c.kit, got, want, inHost)
		}
	}
}

// TestHostIsTheKitServer: a typed server's Host is the kit's server it
// wraps, and nil over a host's own server.
func TestHostIsTheKitServer(t *testing.T) {
	t.Parallel()
	for id, c := range hostCases {
		if c.host == nil {
			continue
		}
		kit := reflect.New(c.kit.Elem()).Interface().(contract.ClusterServer)
		if got := c.host.of(kit); got != any(kit) {
			t.Errorf("cluster 0x%04X: Host %v, want the kit's %T", id, got, kit)
		}
		if got := c.host.of(ownServer{id: id}); got != nil {
			t.Errorf("cluster 0x%04X: Host over a host's own server is %T", id, got)
		}
	}
}

// ownServer is a host's own server: a contract.ClusterServer and nothing
// else.
type ownServer struct{ id uint32 }

func (s ownServer) MatterClusterID() uint32                              { return s.id }
func (ownServer) MatterRead(uint32) (any, bool)                          { return nil, false }
func (ownServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (ownServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (ownServer) MatterReportable() []uint32                             { return nil }
