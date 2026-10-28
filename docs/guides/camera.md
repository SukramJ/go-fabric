# Camera

`cluster/camera` holds the module's servers of the camera surface. The
module holds the state and the rules. The host supplies the media: it
plays the chime, moves the camera, encodes the streams, takes the
snapshots, detects motion in the zones and runs the WebRTC peer
connection. No
media stack, no WebRTC and no TLS live in the module. Each server runs
on a port of its generated declaration (`<cluster>def.Declare`), as the
Thermostat's `SchedulePort` does.

| Cluster | Server | Source of the rules | Port |
| --- | --- | --- | --- |
| WebRtcTransportRequestor (`0x0554`) | `camera.Requestor` | matter.js `WebRtcTransportRequestorServer.ts` | `RequestorPort` (optional) |
| Chime (`0x0556`) | `camera.Chime` | connectedhomeip `ChimeCluster.cpp` (ahead of matter.js) | `ChimePort` |
| CameraAvSettingsUserLevelManagement (`0x0552`) | `camera.AvSettings` | connectedhomeip `CameraAvSettingsUserLevelManagementLogic.cpp` (ahead of matter.js) | `MechanicalPTZ`, `MechanicalPresets`, `DigitalPTZ` |
| CameraAvStreamManagement (`0x0551`) | `camera.AvStream` | connectedhomeip `CameraAVStreamManagementCluster.cpp` (ahead of matter.js) | `VideoStreamPort`, `AudioStreamPort`, `SnapshotStreamPort`, `SnapshotCapturePort` |
| ZoneManagement (`0x0550`) | `camera.Zones` | connectedhomeip `ZoneManagementCluster.cpp`, `zone-geometry.h` (ahead of matter.js) | `ZonePort` |
| WebRtcTransportProvider (`0x0553`) | `camera.Provider` | connectedhomeip `WebRTCTransportProviderCluster.cpp` (ahead of matter.js) | `ProviderPort` |
| PushAvStreamTransport (`0x0555`) | `camera.PushTransport` | connectedhomeip `PushAVStreamTransportLogic.cpp` (ahead of matter.js) | `PushTransportPort`, `TLSEndpoints`, `TLSCertificates` |
| TlsCertificateManagement (`0x0801`) | `tls.CertificateManagement` | connectedhomeip `TLSCertificateManagementCluster.cpp` (ahead of matter.js) | `CertificatePort`, `CertificateStore` |
| TlsClientManagement (`0x0802`) | `tls.ClientManagement` | connectedhomeip `TLSClientManagementCluster.cpp` (ahead of matter.js) | `EndpointStore` |

The device kit makes `camera.Requestor` the default of every device that
mounts WebRtcTransportRequestor, such as `device.NewCameraController`.
Chime, CameraAvSettingsUserLevelManagement, CameraAvStreamManagement and
ZoneManagement, WebRtcTransportProvider and PushAvStreamTransport get the
kit's server once the declaration carries their ports; the two
transports check against the device's CameraAvStreamManagement and
ZoneManagement servers. The TLS servers (`cluster/tls`) are node-scoped:
`node.Config.TLS` (`node.TLSConfig`: the `CertificatePort`, the maxima,
the stores) mounts them on endpoint 0, as connectedhomeip serves them,
and hands their lookups to every PushAvStreamTransport whose declaration
names no `TLSEndpoints` or `TLSCertificates` of its own
(`PushTransport.AttachTLS`). Without the port they keep the
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

A removed fabric's sessions stay in `CurrentSessions` until the host
removes them, as in matter.js, whose server has no fabric-removal hook.

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

## The stream model

CameraAvStreamManagement keeps the camera's allocated video, audio and
snapshot streams. The host's encoders allocate them; the server decides
whether a request may reach the encoder and keeps the tables a controller
reads.

| Feature | Requires | Accepted commands |
| --- | --- | --- |
| Video (VDO) | `VideoStreamPort` | VideoStreamAllocate, VideoStreamDeallocate; VideoStreamModify with WMARK or OSD |
| Audio (ADO) | `AudioStreamPort` | AudioStreamAllocate, AudioStreamDeallocate |
| Snapshot (SNP) | `SnapshotStreamPort`, `SnapshotCapturePort` | SnapshotStreamAllocate, SnapshotStreamDeallocate, CaptureSnapshot; SnapshotStreamModify with WMARK or OSD |
| — | — | SetStreamPriorities |

At least one of VDO, ADO and SNP is required. WMARK, OSD, ICTL and HDR
need VDO or SNP, and SPKR needs ADO. Of the optional attributes, the
server takes the eight connectedhomeip knows: HardPrivacyModeOn,
NightVisionIllum (needs NV), MicrophoneAGCEnabled (needs ADO),
ImageRotation, ImageFlipHorizontal and ImageFlipVertical (need ICTL, and
ICTL needs one of them), StatusLightEnabled and StatusLightBrightness.

**The capabilities.** The host states them in `Initial`: the encoders
and pixel rate, the sensor, the minimum viewport, the rate-distortion
points, the microphone and speaker, the snapshot capabilities, the
bandwidth and `SupportedStreamUsages`. `StreamUsagePriorities` starts
with the supported usages unless `Initial` names its own list. An
attribute that the definition gives no default starts at
connectedhomeip's value: NightVision On, the speaker and microphone
maximum 254, StatusLightBrightness Medium, the rest 0 or false.

**Allocation.** The server checks a request before the port sees it, in
connectedhomeip's order:

1. WatermarkEnabled is present exactly with WMARK, and OSDEnabled
   exactly with OSD; otherwise `INVALID_COMMAND`.
2. The usage is Recording, Analysis or LiveView, the codec is known, the
   frame rates, resolutions and bit rates form ranges, the key-frame
   interval is at most 65500, the channels are 1 to 8, the bit depth is
   8, 16, 24 or 32, the quality is 1 to 100; otherwise
   `CONSTRAINT_ERROR`.
3. A video or audio usage must be in `StreamUsagePriorities`; otherwise
   `INVALID_IN_STATE`.
4. A snapshot request must match a `SnapshotCapabilities` entry: the
   codec, a frame rate the entry reaches, the entry's resolution within
   the requested range. Otherwise `DYNAMIC_CONSTRAINT_ERROR`. The entry
   sets the stream's EncodedPixels and HardwareEncoder.

Then the port allocates and returns an id; its error is the answer (its
IM status, or `FAILURE`). A new id adds a stream. An id the server holds
already is a reuse: a video or snapshot stream narrows to the
intersection of both ranges, and an audio stream stays as it is. After a
video allocation the server calls `VideoStreamAllocated` with the stream
as allocated and the action: `StreamNewAllocation`, `StreamModification`
(the ranges narrowed) or `StreamReuse`. That is where the host starts or
restarts the encoder. A port that reuses streams finds the candidate with
`AvStream.ReusableVideoStreamID` or `ReusableSnapshotStreamID`, and
checks the encoder budget with `ResourceAvailable`.

**Modify and deallocate.** An unknown stream answers `NOT_FOUND`. A
stream with a reference count above 0 cannot be deallocated
(`INVALID_IN_STATE`). An Internal video or audio stream is the device's
own (`DYNAMIC_CONSTRAINT_ERROR`). A Modify needs WatermarkEnabled or
OSDEnabled, each with its feature (`INVALID_COMMAND`). A snapshot stream
that rests on a video stream (its capability's RequiresHardwareEncoder is
false), or that no capability matches, is not modifiable
(`INVALID_IN_STATE`).

**Reference counts.** A transport that uses a stream — a WebRTC session,
a push transport — counts it with `UpdateVideoStreamRefCount`,
`UpdateAudioStreamRefCount` or `UpdateSnapshotStreamRefCount`. An unknown
stream is `ErrStreamNotFound`, a count leaving 0 to 255
`ErrStreamRefCount`. Persisted streams start at 0, as in
connectedhomeip.

**Priorities.** `SetStreamPriorities` needs no stream allocated
(`INVALID_IN_STATE`). An unknown usage answers `INVALID_COMMAND`, an
unsupported one `DYNAMIC_CONSTRAINT_ERROR`, a repeated one
`ALREADY_EXISTS`.

**Snapshots.** `CaptureSnapshot` needs an allocated snapshot stream, and
the named one when the request names one (`NOT_FOUND`). The resolution is
at least 1×1 (`CONSTRAINT_ERROR`). Neither SoftLivestreamPrivacyModeEnabled
nor HardPrivacyModeOn may be on (`INVALID_IN_STATE`). An image larger
than `AvStreamConfig.MaxSnapshotSize` — by default the session's 64000
bytes less 100 — answers `RESOURCE_EXHAUSTED`.

**Settings.** A Viewport, written or set, lies between
MinViewportResolution and the sensor and has the sensor's aspect ratio
to two decimal places; otherwise `CONSTRAINT_ERROR`. The volume levels
lie between their minimum and maximum, and ImageRotation is at most 359,
as the definition says. The host sets its own changes with
`SetAttributes` (CurrentFrameRate, HardPrivacyModeOn, the levels'
bounds); the allocated streams are the server's.

## The zone model

ZoneManagement keeps the camera's zones and their triggers. The host's
motion detection runs on them and reports what it sees.

| Feature | Accepted commands |
| --- | --- |
| — | CreateOrUpdateTrigger, RemoveTrigger |
| UserDefined (needs TwoDimensionalCartesianZone) | RemoveZone; with TWODCART CreateTwoDCartesianZone, UpdateTwoDCartesianZone |

`ZonesConfig` carries the limits: MaxZones of at least 1,
MaxUserDefinedZones of at least 5 and at most MaxZones with USERDEFINED,
SensitivityMax of 2 to 10, and TwoDCartesianMax, the largest vertex. The
zones and triggers the host persisted arrive in `Zones` and `Triggers`.

**A zone** has a name of at most 32 characters, a known use, 3 to 12
vertices and an optional colour of 7 or 9 characters; otherwise
`CONSTRAINT_ERROR`. A Focus zone needs FOCUSZONES (`CONSTRAINT_ERROR`).
A new zone needs room among the user-defined zones
(`RESOURCE_EXHAUSTED`). Each vertex lies within TwoDCartesianMax
(`DYNAMIC_CONSTRAINT_ERROR`). A zone of the same use and vertices exists
already (`ALREADY_EXISTS`). The polygon must be simple: no repeated
vertex, no triangle of collinear vertices, no two crossing edges
(`DYNAMIC_CONSTRAINT_ERROR`; `camera.ZoneSelfIntersecting`). Then the
port stores it; `CreateTwoDCartesianZone` returns the id.

An update needs a known zone (`NOT_FOUND`), not the manufacturer's
(`CONSTRAINT_ERROR`), of the 2D Cartesian type
(`DYNAMIC_CONSTRAINT_ERROR`). A removal needs a known user-defined zone
without a trigger (`NOT_FOUND`, `CONSTRAINT_ERROR`, `INVALID_IN_STATE`).

**A trigger** has an InitialDuration of at least 1, an
AugmentationDuration of at most and a MaxDuration of at least the
initial one (`CONSTRAINT_ERROR`). It carries a Sensitivity exactly with
PerZoneSensitivity (`INVALID_COMMAND`), 1 to SensitivityMax
(`CONSTRAINT_ERROR`). Its zone must exist (`NOT_FOUND`) and, when it is a
2D zone, be a Motion zone (`CONSTRAINT_ERROR`). A new trigger goes to
`ZonePort.CreateTrigger`, a known one to `UpdateTrigger`. `RemoveTrigger`
needs a known trigger (`NOT_FOUND`).

**Sensitivity** (without PerZoneSensitivity) is written 1 to
SensitivityMax (`CONSTRAINT_ERROR`); the host sets it with
`Zones.SetSensitivity`.

**Events.** The host reports motion with `Zones.ZoneTriggered(zone,
reason)` and its end with `Zones.ZoneStopped(zone, reason)`.

## The transport model

Both transports check a request against the camera's stream state, a
`camera.StreamSource`. The device's `*camera.AvStream` is one. The rules
are the answers of connectedhomeip's camera app: a stream id must name an
allocated stream, a null id picks the first allocated stream of the
usage (WebRTC also takes another usage's stream when none matches), and
the privacy modes refuse a usage (`INVALID_IN_STATE`: hard privacy every
usage, soft livestream privacy LiveView, soft recording privacy
Recording and Analysis). The host counts the streams a transport uses
with the AvStream's `Update…StreamRefCount`.

**WebRTC offer and answer.** A controller either asks the camera for an
offer (`SolicitOffer`) or sends its own (`ProvideOffer`). For a new
session the server checks the constraints (at most 10 ICE servers of at
most 10 URLs, the lengths of the username, credential and transport
policy), the privacy modes, a usage in StreamUsagePriorities
(`DYNAMIC_CONSTRAINT_ERROR`), the stream lists or the deprecated stream
ids (not both, `INVALID_COMMAND`), each stream, and a `turns:` or
`stuns:` server against a known UTCTime (`ProviderConfig.UTCTimeKnown`).
It then allocates a session id from 0 and calls the `ProviderPort` with
the validated `OfferArgs`. The port returns the session to store in
CurrentSessions, and for a solicited offer whether the offer is
deferred. A `ProvideOffer` with a session id is a re-offer of the
invoker's session (`NOT_FOUND` for another peer's). `ProvideAnswer`,
`ProvideICECandidates` and `EndSession` name a session of the invoker's
node and fabric; `EndSession` removes the session whatever the port
answers. The host changes a session itself with `Provider.UpsertSession`
and `RemoveSession`.

**Push transports.** `AllocatePushTransport` checks, in chip's order: the
fabric's room (4 transports, `PushTransportConfig.MaxPerFabric`), the
transport options (the stream names and counts, the URL length, the
trigger's fields per type, the CMAF segment of 500 to 65500 ms and a
chunk of at most half of it), the TLS endpoint (a provisioned endpoint
of the fabric with a client certificate, `InvalidTLSEndpoint`), the
format (CMAF over CMAF ingest, `InvalidCombination`), the URL (`https`,
a host, a path that ends in `/`, no query or fragment, `InvalidURL`;
`camera.ValidPushURL`), the usage among SupportedStreamUsages
(`InvalidStreamUsage`), the motion zones (unique, within MaxZones, the
camera's zones, `InvalidZone`), the streams, the segment duration against
the key-frame interval (`InvalidOptions`), the bandwidth (the port) and
the pre-roll (`InvalidPreRollLength`). The port then allocates the
transport with its `TLSCredentials`, the endpoint and its DER
certificates; without the node's TLS servers or the declaration's
lookups every allocation is InvalidTLSEndpoint. The transport starts inactive; `SetTransportStatus`
activates it, `ManuallyTriggerTransport` triggers an active, not
continuous one. An ExpiryTime deallocates the transport after that many
seconds, as chip counts it. The host reports an upload's start and end
with `PushTransport.TransportStarted` and `TransportStopped`, the
PushTransportBegin and PushTransportEnd events.

**TLS provisioning.** A controller provisions a root certificate
(`ProvisionRootCertificate`), asks for a client CSR (`ClientCSR`; the
`CertificatePort` makes the key and signs the nonce), provisions the
signed client certificate and then a TLS endpoint naming both
(`ProvisionEndpoint`). The certificate server keeps per-fabric tables
with node-wide ids, refuses a duplicate (`ALREADY_EXISTS`), a full
fabric (`RESOURCE_EXHAUSTED`) and a certificate the port judges invalid
(`DYNAMIC_CONSTRAINT_ERROR`), and keeps a certificate an endpoint names
(`INVALID_IN_STATE`); an endpoint a user counts (`MutateEndpointReferenceCount`)
is not removed (EndpointInUse). Reads of the certificate lists serve the
full certificates, as matter.js's generated behaviour; chip's id-only
encoding outside a large-payload transport is not mirrored. The fingerprint of `Lookup…Certificate` is the
certificate's SHA-256. A removed fabric takes its entries along
(`MatterFabricRemoved`). The tables persist through any `store.Settings`
(the node's store) under the device's key.

## Persistence

connectedhomeip persists SelectedChime and Enabled, the camera's
position, presets and DPTZ streams, the allocated streams, the
priorities and the settings of the stream management, and the zone
management's Sensitivity (its zones and triggers are the delegate's).
`store.Port` has no record for them. Instead, every change is reported
through the server's change notification (the typed `OnChanged`). The
host persists the values and hands them back in the declaration's
`Initial` (or `ChimeConfig`, `AvSettingsConfig`, `AvStreamConfig`,
`ZonesConfig`). The push transports are the same: the host persists
CurrentConnections and hands them back in `PushTransportConfig.Connections`
(or the declaration's `Initial`). The TLS tables persist through the
servers' `Store`.

## The ecosystem gate

Concept v10 §D (owner decision, 2026-10-27), verbatim:

> **Ökosystem:** Kamera-Cluster erscheinen in keinem Host-Gerät, bevor ein
> Livetest mit Apple Home (das einzige Ökosystem mit Matter-Kamera-Support
> 2026) bestanden ist — dieselbe Regel wie MSCH.

In English: no host device carries a camera cluster until a live test
with Apple Home has passed. Apple Home is the only ecosystem with Matter
camera support in 2026. The same rule applies to MSCH. The reference
bridge composes none of these clusters.
