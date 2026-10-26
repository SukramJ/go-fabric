# Thermostat

The kit's Thermostat server (`cluster/thermo`, cluster `0x0201`) is a
semantic port of matter.js's `ThermostatServer`. A device declares it with
`thermostatdef.Declare` (or builds it with `thermo.New`), and the host
reaches it through one port, the `Sink`, and a set of inbound setters.

## Features

| Feature | Served | Note |
| --- | --- | --- |
| Heating (HEAT), Cooling (COOL) | yes | at least one; the device type's choice |
| AutoMode (AUTO) | yes | needs HEAT and COOL, cleared otherwise |
| LocalTemperatureNotExposed (LTNE) | yes | |
| Occupancy (OCC) | yes | the host reports occupancy with `SetOccupied` |
| Presets (PRES) | yes | presets and the atomic write, persisted through a `PresetStore` |
| MatterScheduleConfiguration (MSCH) | no | matter.js: "not implemented yet" |
| ScheduleConfiguration, Setback | no | deprecated; matter.js refuses them |
| Events (TEVT), ThermostatSuggestions | no | |

`thermo.New` refuses a selection naming a feature it does not serve
(`thermo.ErrFeatures`).

## Optional attributes on request

| Attribute | Served when | Controller write | Device change |
| --- | --- | --- | --- |
| `TemperatureSetpointHold` (0x0023) | `ThermostatConfig.TemperatureSetpointHold` is set, or the declaration's `Initial` sets it | reaches `Sink.SetTemperatureSetpointHold` before it is stored | `SetTemperatureSetpointHold` |
| `TemperatureSetpointHoldDuration` (0x0024) | with the hold; starts null | stored as written (null, or up to 1440 minutes); no Sink, no hold timer | the typed server's `Set` |
| `ThermostatRunningState` (0x0029) | `ThermostatConfig.ThermostatRunningState` is set, or the declaration's `Initial` sets it | read-only | `SetThermostatRunningState` |

The hold says whether the device follows its program
(`SetpointHoldOff`, 0) or holds the setpoint it has (`SetpointHoldOn`, 1).
The running state is a `RelayStateBitmap`: the relays the device runs
right now (Heat, Cool, Fan and their second and third stages).

`SetpointHoldExpiryTimestamp` is not served, and the server keeps no hold
timer: matter.js leaves both to the implementer. The hold reaching the host
is this module's addition. matter.js stores the hold and does not react to
it.

## The `Sink`

`Declare.Sink` (`thermo.ThermostatConfig.Sink`) is told about every change
a controller asks for, after the server validated it and before anything
is stored:

| Method | Called for |
| --- | --- |
| `SetSystemMode(ctx, mode)` | a SystemMode write |
| `SetSetpoint(ctx, attrID, value, companions)` | an occupied or unoccupied setpoint write, a SetpointRaiseLower, the setpoints a SetActivePresetRequest moves |
| `SetTemperatureSetpointHold(ctx, hold)` | a TemperatureSetpointHold write, when the hold is served |

An error refuses the change. The write or command answers with the
error's status when it carries one (an `im.StatusCodeError`), and with
FAILURE otherwise. Nothing is stored. A value equal to the stored one is
no change: the server answers SUCCESS without calling the Sink. Without a
Sink the server stores every change itself.

## Inbound setters

The device's own changes come back through `SetLocalTemperature`,
`SetSystemMode`, `SetOccupiedHeatingSetpoint`, `SetOccupiedCoolingSetpoint`,
`SetOccupied`, `SetAbsoluteSetpointLimit`, `SetPresets`,
`SetActivePresetHandle`, `SetTemperatureSetpointHold` and
`SetThermostatRunningState`, or through the typed server's `Set`. They do
not call the Sink. A change they store is reported as the host's change
(`contract.OriginHostSet`); LocalTemperature is reported by the
subscription's own reporting instead. `SetSystemMode`, the setpoint setters
and the hold and running-state setters log and ignore a value the
selection cannot serve (a setpoint outside its limits is clamped);
`SetAbsoluteSetpointLimit`, `SetPresets`, `SetActivePresetHandle` and `Set`
return the refusal.

*Source: `cluster/thermo/thermostat_server.go:82-83` (features),
`:128-211` (`ThermostatConfig`), `:261-288` (`Sink`), `:548-562` (the
optional attributes), `:602-683` and `:1460`, `:1480` (inbound setters),
`:1001-1018` (the hold write); `cluster/thermo/thermostat_typed.go:74`
(`SetAttributes`); `cluster/thermo/presets.go:394`, `:421`.*
