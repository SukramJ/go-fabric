# Camera

`cluster/camera` holds the module's servers of the camera surface. The
module holds the state and the rules. The host supplies the media: it
plays the chime, moves the camera and runs the WebRTC peer connection. No
media stack, no WebRTC and no TLS live in the module. Each server runs
on a port of its generated declaration (`<cluster>def.Declare`), as the
Thermostat's `SchedulePort` does.

| Cluster | Server | Source of the rules | Port |
| --- | --- | --- | --- |
| WebRtcTransportRequestor (`0x0554`) | `camera.Requestor` | matter.js `WebRtcTransportRequestorServer.ts` | `RequestorPort` (optional) |
| Chime (`0x0556`) | `camera.Chime` | connectedhomeip `ChimeCluster.cpp` (ahead of matter.js) | `ChimePort` |
| CameraAvSettingsUserLevelManagement (`0x0552`) | `camera.AvSettings` | connectedhomeip `CameraAvSettingsUserLevelManagementLogic.cpp` (ahead of matter.js) | `MechanicalPTZ`, `MechanicalPresets`, `DigitalPTZ` |

The device kit makes `camera.Requestor` the default of every device that
mounts WebRtcTransportRequestor, such as `device.NewCameraController`.
Chime and CameraAvSettingsUserLevelManagement get the kit's server once
the declaration carries their port. Without the port they keep the
generated server, and the kit logs `device.default_server.generated`.

## The signalling model

A camera controller (the requestor) and a camera (the provider) set up a
WebRTC stream. The controller's host starts the stream and registers the
session. After that the camera signals the session on the controller's
WebRtcTransportRequestor:

1. The host calls `Requestor.UpsertSession` with the session it started.
   The session carries the id the provider allocated, the peer's node id
   and endpoint, the fabric, the stream usage and the stream ids. A
   session with the same id, fabric and peer node is replaced.
   `VideoStreamId` and `AudioStreamId` carry the first stream of each
   list, or null.
2. The camera invokes `Offer`, `Answer`, `IceCandidates` and `End`. Each
   command must name a session that the invoking peer holds: the same id,
   on the peer's fabric, from the peer's node. The session is looked up
   by all three, because each provider allocates its own ids.
3. A held session is handed to the `RequestorPort` with the signal:
   `Offer(session, request)`, `Answer(session, sdp)`,
   `IceCandidates(session, candidates)` or `End(session, reason)`. `End`
   removes the session from `CurrentSessions` first.
4. Any other session answers `NOT_FOUND`. A port that implements
   `RequestorRefusals` is told the signal, the id the peer named and the
   peer.
5. An empty candidate list answers `CONSTRAINT_ERROR`. A peer's empty
   list is refused by the request's schema already.
6. The host calls `Requestor.RemoveSession` when it tears a stream down
   itself.

The four commands are large messages, so a controller sends them over
TCP. `CurrentSessions` is fabric-sensitive: a peer reads its own fabric's
sessions only.

**The ACL entry.** The node installs the access the camera needs. At
`Start` and after each commissioning, on the first fabric, it adds one
Operate entry for CASE with no subject, targeting the requestor's
endpoint and cluster. It adds the entry only when the fabric's ACL does
not hold it yet. This follows matter.js `#nodeOnline`. The node emits no
AccessControlEntryChanged event for the entry, because matter.js emits
none for a change without a remote actor.

## Chime

`ChimePort.InstalledChimeSounds` lists the sounds. The list is read on
every read of `InstalledChimeSounds`, and an error fails the read. When
the list changes, call `Chime.SoundsChanged` to report it.

- **SelectedChime.** A write must name an installed sound, or it answers
  `NOT_FOUND`. The value starts at 0, as in connectedhomeip, and 0 need
  not name a sound.
- **Enabled.** It starts `true`. While it is `false`, `PlayChimeSound`
  succeeds and plays nothing.
- **PlayChimeSound.** A given `ChimeID` must be installed, or the command
  answers `NOT_FOUND`. Without one, `SelectedChime` plays. The port's
  error is the answer: its IM status, or `FAILURE` without one. A sound
  that started emits `ChimeStartedPlaying`.

A write that does not change a value is stored without a report.

## Pan, tilt and zoom

The features are MechanicalPan, MechanicalTilt and MechanicalZoom (MPAN,
MTILT, MZOOM), MechanicalPresets (MPRESETS) and DigitalPTZ (DPTZ). At
least one of MPAN, MTILT, MZOOM and DPTZ is required.

| Feature | Requires | Accepted commands |
| --- | --- | --- |
| MPAN, MTILT, MZOOM | `MechanicalPTZ` | MPTZSetPosition, MPTZRelativeMove |
| MPRESETS | a `MechanicalPTZ` that is a `MechanicalPresets` | MPTZMoveToPreset, MPTZSavePreset, MPTZRemovePreset |
| DPTZ | `DigitalPTZ` | DPTZSetViewport, DPTZRelativeMove |

**Mechanical movement.** The server validates the target first:

- A value needs its feature, or the command answers `INVALID_COMMAND`.
- A value must lie within PanMin…PanMax, TiltMin…TiltMax or 1…ZoomMax,
  or it answers `CONSTRAINT_ERROR`.
- A relative move is held to the limits.
- A camera that is still moving answers `BUSY`.

Then the port starts the movement and returns, and MovementState is
Moving. The host reports the end with `AvSettings.PhysicalMovementComplete`.
On success (`nil`), the target becomes MPTZPosition. Either way,
MovementState returns to Idle. A device that cannot move now refuses
with `BUSY`; this is connectedhomeip's `CanChangeMPTZ`.

**Presets.** Preset ids run from 1 to MaxPresets (default 5). An id
outside that range answers `CONSTRAINT_ERROR`, and an unknown id answers
`NOT_FOUND`. A new preset needs a free slot, or the command answers
`RESOURCE_EXHAUSTED`. A save without an id takes the next free one.

**Digital PTZ.** The host announces the streams that can move:
`AddMoveCapableVideoStream` when a stream is allocated,
`RemoveMoveCapableVideoStream` when it is deallocated, and
`UpdateMoveCapableVideoStreams` when the default viewport changes. A
command for any other stream answers `NOT_FOUND`. `DptzRelativeMove`
returns the viewport it reached, and the stream reports that viewport.

**Limits.** `SetPanMin`, `SetPanMax`, `SetTiltMin`, `SetTiltMax` and
`SetZoomMax` hold each limit to its constraint. `SetPosition` records a
position the device reached by itself.

## Persistence

connectedhomeip persists SelectedChime and Enabled, and the camera's
position, presets and DPTZ streams. `store.Port` has no record for them.
Instead, every change is reported through the server's change
notification (the typed `OnChanged`). The host persists the values and
hands them back in the declaration's `Initial` (or `ChimeConfig`,
`AvSettingsConfig`).

## The ecosystem gate

Concept v10 §D (owner decision, 2026-10-27), verbatim:

> **Ökosystem:** Kamera-Cluster erscheinen in keinem Host-Gerät, bevor ein
> Livetest mit Apple Home (das einzige Ökosystem mit Matter-Kamera-Support
> 2026) bestanden ist — dieselbe Regel wie MSCH.

In English: no host device carries a camera cluster until a live test
with Apple Home has passed. Apple Home is the only ecosystem with Matter
camera support in 2026. The same rule applies to MSCH. The reference
bridge composes none of these clusters.
