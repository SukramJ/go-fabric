# Thermostat

The kit's Thermostat server (`cluster/thermo`, cluster `0x0201`) is a
semantic port of matter.js's `ThermostatServer`. A device declares it with
`thermostatdef.Declare` (or builds it with `thermo.New`), and the host
reaches it through the `Sink` and a set of inbound setters. The week
programs (MatterScheduleConfiguration) add a second port, the
`SchedulePort`.

## Features

| Feature | Served | Note |
| --- | --- | --- |
| Heating (HEAT), Cooling (COOL) | yes | at least one; the device type's choice |
| AutoMode (AUTO) | yes | needs HEAT and COOL, cleared otherwise |
| LocalTemperatureNotExposed (LTNE) | yes | |
| Occupancy (OCC) | yes | the host reports occupancy with `SetOccupied` |
| Presets (PRES) | yes | presets and the atomic write, persisted through a `PresetStore` |
| MatterScheduleConfiguration (MSCH) | yes | ahead of matter.js ("not implemented yet"), after connectedhomeip; see [Week programs](#week-programs-msch) |
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

## Week programs (MSCH)

matter.js does not implement MatterScheduleConfiguration. This server
implements it after connectedhomeip's `ThermostatSchedules`, which is the
behavioural authority for this feature.

**Configuration.** `ThermostatConfig`, or the declaration's `Initial`
under the attribute names, holds:

- `ScheduleTypes`: one to three types, each a SystemMode with its number
  of schedules and its features (presets, setpoints, names, Off).
- `NumberOfSchedules`, `NumberOfScheduleTransitions` and
  `NumberOfScheduleTransitionPerDay`. A nil per-day count means no limit.
- `Schedules`: the device's own week programs. Each one needs a handle of
  its own. A null `BuiltIn` is taken as built in, so a controller cannot
  remove that schedule.
- `ActiveScheduleHandle`: the schedule the device follows now.

The types and counts are configuration. The typed `Set` refuses them.

**`SchedulePort`.** The port is set through `Declare.SchedulePort` or
`ThermostatConfig.SchedulePort`.

| Method | Called for |
| --- | --- |
| `SetActiveSchedule(ctx, handle)` | a `SetActiveScheduleRequest` whose handle names a stored schedule. The server answers an unknown handle INVALID_COMMAND without calling the port. |
| `CommitSchedules(ctx, schedules)` | the `Schedules` list a controller committed in an atomic write. The list is validated and precommitted, and every new schedule already has a 16-byte handle the server issued. |

An error refuses the change, as for the `Sink`. Without a port the server
keeps the schedules in memory. The device's own changes come back through
`SetSchedules` and `SetActiveScheduleHandle`, or through the typed `Set`.
These calls do not use the port. A schedule list that drops the active
schedule clears the active handle.

**The write.** A controller writes `Schedules` only inside an
`AtomicRequest` BeginWrite … CommitWrite.

- Any other write answers INVALID_IN_STATE. A write from another peer
  during an atomic write also answers INVALID_IN_STATE, as chip does.
  Presets answers that case BUSY.
- BeginWrite starts the pending list as the stored one.
- Each write is validated entry by entry, with chip's
  `AppendPendingSchedule` rules:
  - handle, name and preset-handle sizes;
  - a SystemMode of Auto, Cool or Heat that a schedule type supports;
  - BuiltIn kept for an existing handle;
  - names and presets only where the type supports them;
  - the transition rules: weekdays, a time up to 1439, no repeated time on
    a shared day, the setpoints the effective SystemMode needs within the
    limits and the deadband, presets resolved under PRES;
  - the per-day, total and per-type counts.
- The peer that holds the write reads the pending list.
- CommitWrite first checks every staged attribute. A built-in schedule the
  list drops is CONSTRAINT_ERROR, and an active schedule it drops is
  INVALID_IN_STATE.
- With MSCH, a Presets commit that drops a preset a schedule names is
  INVALID_IN_STATE.
- Only when every check passes does the commit hand the list to the port.

`SetActiveScheduleRequest` emits no `ActiveScheduleChange` event. The
event belongs to the Events feature (TEVT), which the server does not
serve.

**Ecosystems.** Hosts gate MSCH behind a flag, off by default, until a
live pairing is verified. From issue #273, verbatim:

> No source found that Apple Home, Google Home or Alexa render MSCH or
> `TemperatureSetpointHold`. Apple once aborted pairing on an endpoint
> that announced an unknown cluster (the draft Schedules cluster 0x0024;
> the reference host recorded it). MSCH is a feature bit of the known
> Thermostat cluster, but whether Apple tolerates the bit is unverified —
> a live pairing test before any host enables it.

The reference bridge serves MSCH only with `--thermostat-schedules`.

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
`SetThermostatRunningState`, the kit server's methods. The typed server's
`Set` reaches every one of them (a `Patch` with `Presets` goes through
`SetPresets`), so `thermostatdef` has no `Host`: a host on the facade uses
`Set`. They do not call the Sink. A change they store is reported as the host's change
(`contract.OriginHostSet`); LocalTemperature is reported by the
subscription's own reporting instead. `SetSystemMode`, the setpoint setters
and the hold and running-state setters log and ignore a value the
selection cannot serve (a setpoint outside its limits is clamped);
`SetAbsoluteSetpointLimit`, `SetPresets`, `SetActivePresetHandle` and `Set`
return the refusal.

*Source: `cluster/thermo/schedules.go`, `cluster/thermo/schedules_write.go`
(MSCH), `cluster/thermo/atomic.go` (`commitWrite`, `precommitAtomic`);
`cluster/thermo/thermostat_server.go:82-83` (features),
`:128-211` (`ThermostatConfig`), `:261-288` (`Sink`), `:548-562` (the
optional attributes), `:602-683` and `:1460`, `:1480` (inbound setters),
`:1001-1018` (the hold write); `cluster/thermo/thermostat_typed.go:74`
(`SetAttributes`); `cluster/thermo/presets.go:394`, `:421`.*
