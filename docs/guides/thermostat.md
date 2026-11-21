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
| MatterScheduleConfiguration (MSCH) | yes | matter.js's, behind its forward feature; here behind the extension `BD-chip-Thermostat-Schedules`; see [Week programs](#week-programs-msch) |
| ScheduleConfiguration, Setback | no | deprecated; matter.js refuses them |
| Events (TEVT) | yes | ahead of matter.js, after connectedhomeip; see [Events](#events-tevt) |
| ThermostatSuggestions | no | |

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

matter.js implements MatterScheduleConfiguration behind its forward
feature `thermostat-msch` (`ThermostatServer.ts:267-271` at `b25ed960`).
This server mirrors it — the write rules, the normalisation, the commit
checks and the ScheduleTypes start checks — and serves it behind the
extension `BD-chip-Thermostat-Schedules`, off by default
(`surface.Policies.Extensions`; the reference bridge's
`--thermostat-schedules`).

**Configuration.** `ThermostatConfig`, or the declaration's `Initial`
under the attribute names, holds:

- `ScheduleTypes`: one to three types, each a SystemMode with its number
  of schedules and its features (presets, setpoints, names, Off). They
  may name only Auto, Heat or Cool, none twice, each supporting presets
  or setpoints (`thermo.ErrSchedules`).
- `NumberOfSchedules`, `NumberOfScheduleTransitions` and
  `NumberOfScheduleTransitionPerDay`. A nil per-day count means no limit.
- `Schedules`: the device's own week programs, held to the content rules
  a controller's write is held to. A null handle gets a generated one. A
  null `BuiltIn` takes the stored schedule's value for that handle, else
  false: set `BuiltIn: true` on a program a controller
  may not remove.
- `ActiveScheduleHandle`: the schedule the device follows now.

The types and counts are configuration. The typed `Set` refuses them.

**`SchedulePort`.** The port is set through `Declare.SchedulePort` or
`ThermostatConfig.SchedulePort`.

| Method | Called for |
| --- | --- |
| `SetActiveSchedule(ctx, handle)` | a `SetActiveScheduleRequest` whose handle names a stored schedule. The server answers an unknown handle INVALID_COMMAND without calling the port. |
| `CommitSchedules(ctx, schedules)` | the `Schedules` list a controller committed in an atomic write. The list is validated, and every new schedule already has a 16-byte handle the server issued. |

An error refuses the change, as for the `Sink`. Without a port the server
keeps the schedules in memory. The device's own changes come back through
`SetSchedules` and `SetActiveScheduleHandle`, or through the typed `Set`.
These calls do not use the port. `SetSchedules` is the host's
authoritative replace: it is held to the content rules (types and counts,
names, presets, transitions, limits, deadband, unique handles) but not to
the lifecycle rules of a controller's write — a new handle, a changed or
dropped built-in schedule and a dropped active schedule are accepted, and
a dropped active schedule clears the active handle
(`BD-Thermostat-HostSchedules`). Its errors are IM statuses.

**The write.** A controller writes `Schedules` only inside an
`AtomicRequest` BeginWrite … CommitWrite.

- Any other write answers INVALID_IN_STATE. A write from another peer
  during an atomic write also answers INVALID_IN_STATE; Presets answers
  that case BUSY.
- BeginWrite stages nothing: until its first write the holder reads the
  stored list.
- Each write is validated with matter.js's write rules: the handle rules
  (a handle the device never issued is NOT_FOUND, a new schedule is not
  built in, BuiltIn does not change), the schedule type, the transitions
  per schedule, names and presets only where the type supports them, the
  transition rules (a SystemMode other than the schedule's, the setpoints
  within the limits and the deadband, the presets among the writing
  peer's pending or the stored ones), then the count limits.
- The peer that holds the write reads its pending list.
- CommitWrite validates the staged Schedules again against the Presets of
  the same atomic write. A built-in schedule the list drops is
  CONSTRAINT_ERROR, and an active schedule it drops is INVALID_IN_STATE.
- A Presets commit, or a local `SetPresets`, that drops a preset a
  schedule names is INVALID_IN_STATE.
- Only when every check passes does the commit hand the list to the port.

With the Events feature (TEVT), `SetActiveScheduleRequest` emits
`ActiveScheduleChange` with the previous and the new handle.


## Events (TEVT)

matter.js's server emits none of the cluster's eight events. The server
follows connectedhomeip (`ThermostatClusterEvents.cpp` and its call sites
at `6170af84`; ActiveScheduleChange's call site is in
`ThermostatClusterSchedules.cpp` at `645b267a`): every event needs TEVT, and the EventList carries the
events of the selection (LocalTemperatureChange without LTNE,
OccupancyChange with OCC, RunningModeChange with AUTO,
ActiveScheduleChange with MSCH, ActivePresetChange with PRES).

| Event | Emitted when | Payload |
| --- | --- | --- |
| SystemModeChange | SystemMode changes (a write, `SetSystemMode`, `SetAttributes`) | the previous and the current mode |
| LocalTemperatureChange | LocalTemperature changes (`SetLocalTemperature`, `SetAttributes`) | the current value, null included |
| SetpointChange | one of the four occupied and unoccupied setpoints changes, by a write, a command, a host setter or a companion the deadband moves | Heat or Cool, Occupied or no occupancy, the previous and the current setpoint; a user limit emits none |
| RunningStateChange | ThermostatRunningState changes | the previous and the current relays |
| RunningModeChange | ThermostatRunningMode changes (`SetThermostatRunningMode`) | the previous and the current mode |
| ActiveScheduleChange | `SetActiveScheduleRequest` succeeds | the previous and the new handle |
| ActivePresetChange | `SetActivePresetRequest` succeeds (the same handle again included), or a write of a setpoint in effect — or of a setpoint limit whose reconcile moves that setpoint — clears the active preset | the previous and the new handle |
| OccupancyChange | never: connectedhomeip does not emit it | |

With TEVT and AUTO the server serves `ThermostatRunningMode` (0x001E),
which the selection makes mandatory. The host sets it with
`SetThermostatRunningMode`: Off always, Cool with COOL, Heat with HEAT,
anything else `ConstraintError`, as chip's `SetRunningMode` does. The
events of a host change go to the endpoint the bridge stamped
(`SetEndpoint`); a command's go to its endpoint.

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
error's status when it carries one (a `contract.StatusError` or an
`im.StatusCodeError`), and with
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
`cluster/thermo/thermostat_server.go` (the `ThermostatFeature…`
constants, `ThermostatConfig`, `Sink`, `servedOptional`, the inbound
`Set…` methods, `writeHold`); `cluster/thermo/thermostat_typed.go`
(`SetAttributes`); `cluster/thermo/presets.go` (`SetPresets`,
`SetActivePresetHandle`).*
