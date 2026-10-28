# Ambient context sensing

`cluster/sensing` holds the module's AmbientContextSensing (`0x0431`)
server, `sensing.AmbientContext`. The cluster reports what a sensor
detects: human activity, identified objects and sounds, an object count
against a threshold, and predicted activity windows. matter.js serves it
with the generated behaviour only, so the rules follow connectedhomeip's
`AmbientContextSensingCluster.cpp` (ahead of matter.js,
`BD-chip-AmbientContextSensing-RulesInServer`). The server needs no port:
chip's delegate becomes the server's own state, and the device's clock
drives the hold time and the event timestamps.

| Cluster | Server | Source of the rules | Host methods |
| --- | --- | --- | --- |
| AmbientContextSensing (`0x0431`) | `sensing.AmbientContext` | connectedhomeip `AmbientContextSensingCluster.cpp` (ahead of matter.js) | `AddDetection`, `SetObjectCount`, `Close` (`acsdef.Host`) |

WaterTankLevelMonitoring (`0x0079`) keeps the generated server. No source
gives it logic (`BD-Matter-GeneratedServerSuffices`).

## Mounting it

No device type at the pin lists AmbientContextSensing as a server.
matter.js has it only as the thermostat's client. Build the server and
mount it on any device as the declaration's `HostServer`:

```go
acs, err := sensing.NewAmbientContext(sensing.AmbientContextConfig{
	Features: sensing.FeatureHumanActivity | sensing.FeatureObjectCounting | sensing.FeatureObjectIdentification,
	AmbientContextTypeSupported: supported, // the tags the sensor detects
	HoldTime: persistedHoldTime,            // nil: HoldTimeLimits' default
	Clock: clk,
})
d, err := device.NewContactSensor(opts, acsdef.Declare{HostServer: acs}) // acsdef: cluster/spec/ambientcontextsensing
```

The selection needs at least one of HumanActivity, ObjectCounting,
ObjectIdentification and SoundIdentification. The attributes are chip's
for that selection. Every element of the cluster is provisional, so the
model makes each one optional, and the server opts in what chip serves.
ObjectCount is served only with `ServeObjectCount`. Where a device type
lists the cluster, the kit's server is the default, and it takes its
configuration from the declaration's `Initial`.

## Detections and the hold time

`AddDetection` records one detection of one or two tags. Each tag must be
on AmbientContextTypeSupported, or the call fails with
`sensing.ErrNotSupported`. A new detection goes to the front of
AmbientContextType, is held for HoldTime seconds and emits
AmbientContextDetectStarted. When the hold time ends, the detection leaves
the list and AmbientContextDetectEnded carries its start, as posix-ms and
as milliseconds since the server was built. HumanActivityDetected,
ObjectIdentified and AudioContextDetected follow the namespaces of the
held detections.

The same detection again (the same namespaces and tags in the same order)
moves to the front and keeps its start. chip adds the hold to the start,
not to the current time, so a repeated detection does not outlast its first
hold. At SimultaneousDetectionLimit the oldest detections make room without
an end event.

`SetObjectCount` records a count of at least 1 (OC and OI). The count is
held for HoldTime. ObjectCountThresholdReached compares it with
ObjectCountConfig's threshold. AmbientContextDetectStarted carries the
counting tag, the flag and the count. Setting the same count again changes
nothing.

## The configuration attributes

The typed `Set` (`SetAttributes`) changes the configuration under chip's
rules. All values are checked first, then all are stored, or none:

- **HoldTimeLimits** is sanitised: the minimum is at least 1, the maximum
  at least the larger of 10 and the minimum, and the default lies between
  them. A HoldTime outside the new limits resets to the default.
- **HoldTime** must lie within the limits.
- **AmbientContextTypeSupported** takes 1 to 50 tags. Each tag must be in a
  namespace whose feature is selected, and no tag may appear twice.
- **ObjectCountConfig** needs a threshold of at least 1. A standard tag
  must be in the IdentifiedObject namespace and on the supported list. A
  manufacturer tag needs a label of at most 64 characters.
- **SimultaneousDetectionLimit** takes 1 to 10. A lower limit drops the
  oldest detections.
- **PredictedActivity** takes at most 20 windows in order, each start
  before its end and after the previous end. Tags must be in selected
  namespaces, and a crowd count must lie in 1 to 254 under OC.

The detections, the attributes derived from them and the object count
belong to the server. A `Set` of one of them fails with `spec.ErrNotHeld`.
A controller may write HoldTime and ObjectCountConfig. The same HoldTime
is a success that changes nothing.

chip's SensorFusion feature and its SensorFusionSupported attribute are not
in the matter.js model at the pin. The server has neither.

## Persistence

chip persists HoldTime and the ObjectCountConfig threshold. The host
persists them here, as it does for the camera servers: each change arrives
through the change notification (the typed `OnChanged`), and
`AmbientContextConfig.HoldTime` and `ObjectCountConfig` restore the values.
A persisted HoldTime outside the limits falls back to their default, as
chip's start-up replaces a stored value it refuses.
