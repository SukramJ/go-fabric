// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import "slices"

// The host interfaces (brief CK-2, issue #293): the methods of a kit
// server that a host calls and the typed server does not reach — a
// validated host change with its own rule, a state machine input, a
// query of state no attribute carries. They are declared here, next to
// the ports, so that every cluster whose kit server has some gets a Host
// interface in its <cluster>def package and a Server.Host accessor, and
// the kit server implements the interface. What Set, Get, OnChanged,
// OnWrite, MarkChanged, Handle<Command> and Emit<Event> already reach
// stays out, as does the bridge's wiring (SetEndpoint). A Host may embed
// a contract interface the kit server implements for host inputs (the
// switch's contract.SwitchPositionSink and contract.SwitchEventEmitter):
// a method line naming the interface embeds it, and the device's decision
// table lists its methods as the Host's. Hand-written: which methods are
// host methods is the server's decision, not the schema's.

// hostDecl is a cluster's Host interface: its doc after the standard
// first line, its method lines (a line starting with "//" is the next
// method's doc), the types they need beyond the generated ones (rendered
// before the interface) and the packages they name.
type hostDecl struct {
	server  string // the kit server, as the doc names it
	doc     []string
	methods []string
	types   []string
	imports []string
}

// alarmBaseHost is the Host of every AlarmBase derivation (cluster/alarmbase).
var alarmBaseHost = hostDecl{
	server: "cluster/alarmbase Server",
	methods: []string{
		"// ResetLatchedAlarms clears alarms from State, latched or not —",
		"// chip's ResetLatchedAlarms; Set's State keeps a latched alarm. An",
		"// alarm outside Supported is refused.",
		"ResetLatchedAlarms(alarms uint32) error",
	},
}

// hosts are the clusters whose kit server has host methods.
var hosts = map[string]hostDecl{
	"SmokeCoAlarm": {
		server: "cluster/alarm Server",
		methods: []string{
			"// Refresh reads the StateSource and emits the events its change",
			"// since the previous Refresh calls for — for a source that is no",
			"// contract.ChangeNotifier.",
			"Refresh()",
		},
	},
	"RefrigeratorAlarm": alarmBaseHost,
	"DishwasherAlarm":   alarmBaseHost,
	"TemperatureAlarm":  alarmBaseHost,
	"BooleanStateConfiguration": {
		server: "cluster/boolcfg Server",
		methods: []string{
			"// SetSensorFault records a sensor fault: the SensorFault event is",
			"// emitted on every call when the event is in the EventList, and",
			"// the SensorFault attribute, when served, changes with it.",
			"SetSensorFault(fault SensorFaultBitmap)",
		},
	},
	"CameraAvSettingsUserLevelManagement": {
		server: "cluster/camera AvSettings",
		methods: []string{
			"// AddMoveCapableVideoStream adds an allocated stream with its",
			"// default viewport to DPTZStreams.",
			"AddMoveCapableVideoStream(videoStreamID uint16, viewport ViewportStruct)",
			"// UpdateMoveCapableVideoStream sets a known stream's viewport; an",
			"// unknown stream changes nothing.",
			"UpdateMoveCapableVideoStream(videoStreamID uint16, viewport ViewportStruct)",
			"// UpdateMoveCapableVideoStreams sets every stream's viewport, after",
			"// the device's default viewport changed.",
			"UpdateMoveCapableVideoStreams(viewport ViewportStruct)",
			"// RemoveMoveCapableVideoStream removes a deallocated stream; an",
			"// unknown one changes nothing.",
			"RemoveMoveCapableVideoStream(videoStreamID uint16)",
			"// PhysicalMovementComplete reports the end of the movement a",
			"// command started: on success (nil) the target becomes",
			"// MPTZPosition; either way MovementState returns to Idle.",
			"PhysicalMovementComplete(err error)",
		},
	},
	"Chime": {
		server: "cluster/camera Chime",
		methods: []string{
			"// SoundsChanged reports a change of the port's installed sounds:",
			"// InstalledChimeSounds advances its data version and is reported.",
			"SoundsChanged() error",
		},
	},
	"CameraAvStreamManagement": {
		server: "cluster/camera AvStream",
		methods: []string{
			"// UpdateVideoStreamRefCount counts the video stream's use by a",
			"// transport up or down; a stream in use is not deallocated.",
			"UpdateVideoStreamRefCount(videoStreamID uint16, increment bool) error",
			"// UpdateAudioStreamRefCount counts the audio stream's use up or",
			"// down.",
			"UpdateAudioStreamRefCount(audioStreamID uint16, increment bool) error",
			"// UpdateSnapshotStreamRefCount counts the snapshot stream's use up",
			"// or down.",
			"UpdateSnapshotStreamRefCount(snapshotStreamID uint16, increment bool) error",
			"// ReusableVideoStreamID is an allocated video stream a request",
			"// can reuse (chip's GetReusableVideoStreamId).",
			"ReusableVideoStreamID(req VideoStreamStruct) (uint16, bool)",
			"// ReusableSnapshotStreamID is an allocated snapshot stream a",
			"// request can reuse (chip's GetReusableSnapshotStreamId).",
			"ReusableSnapshotStreamID(req SnapshotStreamStruct) (uint16, bool)",
			"// ResourceAvailable reports whether a stream of the candidate's",
			"// encoded pixel rate still fits (chip's",
			"// IsResourceAvailableForStreamAllocation).",
			"ResourceAvailable(candidateEncodedPixelRate uint32, encoderRequired bool) bool",
		},
	},
	"ZoneManagement": {
		server: "cluster/camera Zones",
		methods: []string{
			"// ZoneTriggered emits the ZoneTriggered event the host reports for",
			"// a zone, under the zone's trigger control.",
			"ZoneTriggered(zoneID uint16, reason ZoneEventTriggeredReasonEnum) error",
			"// ZoneStopped emits the ZoneStopped event the host reports for a",
			"// zone.",
			"ZoneStopped(zoneID uint16, reason ZoneEventStoppedReasonEnum) error",
		},
	},
	"CommodityTariff": {
		server: "cluster/energy CommodityTariffServer",
		methods: []string{
			"// SyncTime is chip's time poll: a delayed tariff whose StartDate",
			"// has come is committed, and the current day, day entry and",
			"// tariff components are derived again at the server's clock. The",
			"// server runs it itself with a SyncInterval.",
			"SyncTime() error",
		},
	},
	"DeviceEnergyManagement": {
		server: "cluster/energy DeviceEnergyManagementServer",
		methods: []string{
			"// Close stops the adjustment and pause timers.",
			"Close()",
		},
	},
	"EnergyEvse": {
		server: "cluster/energy EvseServer",
		doc: []string{
			"The charger is a state machine the hardware's inputs drive; the",
			"attributes it computes from them are refused by Set.",
		},
		methods: []string{
			"// SetHardwareState is the hardware's plug and demand state",
			"// (chip's HwSetState): NotPluggedIn, PluggedInNoDemand and",
			"// PluggedInDemand drive the state machine, any other is FAILURE.",
			"SetHardwareState(next StateEnum) error",
			"// SetFault records a fault (chip's HwSetFault): Fault is emitted,",
			"// FaultState changes, the state machine enters or leaves Fault;",
			"// the same fault again is FAILURE.",
			"SetFault(fault FaultStateEnum) error",
			"// SetMaxHardwareChargeCurrent is the hardware's charge limit in mA",
			"// (chip's HwSetMaxHardwareChargeCurrentLimit).",
			"SetMaxHardwareChargeCurrent(mA int64) error",
			"// SetMaxHardwareDischargeCurrent is the hardware's discharge limit",
			"// in mA (chip's HwSetMaxHardwareDischargeCurrentLimit).",
			"SetMaxHardwareDischargeCurrent(mA int64) error",
			"// SetCableAssemblyLimit is the cable's limit in mA (chip's",
			"// HwSetCableAssemblyLimit).",
			"SetCableAssemblyLimit(mA int64) error",
			"// SetNominalMainsVoltage is the mains voltage in mV (chip's",
			"// HwSetNominalMainsVoltage); below 100 V is CONSTRAINT_ERROR.",
			"SetNominalMainsVoltage(mV int64) error",
			"// ReportRFID emits the Rfid event (chip's HwSetRFID).",
			"ReportRFID(uid []byte) error",
			"// DiagnosticsComplete returns from DisabledDiagnostics to Disabled",
			"// (chip's HwDiagnosticsComplete); in any other SupplyState it is",
			"// FAILURE.",
			"DiagnosticsComplete() error",
			"// HardwareState returns the state the hardware last reported.",
			"HardwareState() StateEnum",
			"// MaxHardwareChargeCurrent returns the hardware charge limit in mA.",
			"MaxHardwareChargeCurrent() int64",
			"// MaxHardwareDischargeCurrent returns the hardware discharge limit",
			"// in mA.",
			"MaxHardwareDischargeCurrent() int64",
			"// CableAssemblyLimit returns the cable's limit in mA.",
			"CableAssemblyLimit() int64",
			"// NominalMainsVoltage returns the mains voltage in mV.",
			"NominalMainsVoltage() int64",
			"// IsPluggedIn reports whether a vehicle is plugged in (chip's",
			"// IsEvsePluggedIn).",
			"IsPluggedIn() bool",
			"// Targets returns the stored charging target schedules.",
			"Targets() []ChargingTargetScheduleStruct",
			"// Close stops the enabled-until timer.",
			"Close()",
		},
	},
	"DoorLock": {
		server: "cluster/lock DoorLockServer",
		methods: []string{
			"// SetDoorState records the door's state (DPS) and emits",
			"// DoorStateChange; DoorOpen and DoorClosed count in DoorOpenEvents",
			"// and DoorClosedEvents when they are served.",
			"SetDoorState(state DoorStateEnum) error",
			"// Users returns a copy of the user database.",
			"Users() []store.DoorLockUser",
		},
		imports: []string{"github.com/SukramJ/go-fabric/store"},
	},
	"ThreadNetworkDirectory": {
		server: "cluster/netinfra ThreadDirectoryServer",
		methods: []string{
			"// ReloadNetworks re-reads the ThreadNetworkStore after the host",
			"// changed it outside the cluster and reports ThreadNetworks when",
			"// it moved; a preferred network the store no longer holds is",
			"// cleared.",
			"ReloadNetworks(ctx context.Context) error",
		},
	},
	"WiFiNetworkManagement": {
		server: "cluster/netinfra WiFiManagementServer",
		methods: []string{
			"// SetNetworkCredentials stores the network's SSID and passphrase:",
			"// a pair outside chip's limits changes nothing and is refused;",
			"// Ssid and PassphraseSurrogate change with it.",
			"SetNetworkCredentials(ssid, passphrase []byte) error",
			"// ClearNetworkCredentials drops the credentials: Ssid and",
			"// PassphraseSurrogate read null.",
			"ClearNetworkCredentials() error",
			"// Passphrase returns the stored passphrase, nil without",
			"// credentials.",
			"Passphrase() []byte",
		},
	},
	"ServiceArea": {
		server: "cluster/servicearea Server",
		methods: []string{
			"// RemoveSupportedAreasEntry removes an area and everything that",
			"// names it: it leaves SelectedAreas and Progress, and CurrentArea",
			"// becomes null when it was the area.",
			"RemoveSupportedAreasEntry(areaID uint32)",
			"// RemoveSupportedMapsEntry removes a map and the first area that",
			"// lies on it.",
			"RemoveSupportedMapsEntry(mapID uint32)",
		},
	},
	"Switch": {
		server: "cluster/switch Server",
		doc: []string{
			"The positions and presses come in through the server's",
			"contract.SwitchPositionSink and contract.SwitchEventEmitter, which",
			"Host embeds.",
		},
		methods: []string{
			"// SwitchPositionSink takes the raw and the decided position.",
			"contract.SwitchPositionSink",
			"// SwitchEventEmitter takes the presses of a momentary switch.",
			"contract.SwitchEventEmitter",
			"// RawPosition returns the last raw position reported, debounced",
			"// or not.",
			"RawPosition() uint8",
			"// SetDebounceDelay sets the debounce delay of a raw position; 0",
			"// decides one at once.",
			"SetDebounceDelay(d time.Duration)",
			"// SetLongPressDelay sets the hold that makes a press long; 0 is",
			"// the default.",
			"SetLongPressDelay(d time.Duration)",
			"// SetMultiPressDelay sets the release that ends a multi-press",
			"// sequence; 0 is the default.",
			"SetMultiPressDelay(d time.Duration)",
			"// ResetState starts a clean cycle: pending debounced positions,",
			"// timers and results not applied yet are dropped.",
			"ResetState()",
		},
		imports: []string{"time"},
	},
	"Binding": {
		server: "cluster/core Binding",
		methods: []string{
			"// Entries returns every fabric's entries, in list order.",
			"Entries() []TargetStruct",
			"// OnChange registers fn to learn, after each committed write, the",
			"// entries one fabric gained (added) and lost (removed), once per",
			"// fabric whose entries changed; it returns the function that",
			"// removes the registration.",
			"OnChange(fn func(fabricIndex uint8, added, removed []TargetStruct)) (stop func())",
		},
	},
	"WebRtcTransportRequestor": {
		server: "cluster/camera Requestor",
		types: []string{
			"// SessionKey identifies a tracked session. The session id alone is",
			"// not unique: each provider node allocates its own, so two peers, or",
			"// one peer on two fabrics, may use the same id at the same time.",
			"// Mirrors matter.js WebRtcTransportRequestorServer.ts:189-196",
			"// (SessionKey) and :185-187 (isSameSession).",
			"type SessionKey struct {",
			"\tID          uint16",
			"\tFabricIndex uint8",
			"\tPeerNodeID  uint64",
			"}",
			"",
			"// KeyOf is the key of a session.",
			"func KeyOf(s WebRTCSessionStruct) SessionKey {",
			"\treturn SessionKey{ID: s.Id, FabricIndex: s.FabricIndex, PeerNodeID: s.PeerNodeId}",
			"}",
		},
		methods: []string{
			"// UpsertSession registers or updates a session the host initiated",
			"// with a peer: a session must be registered before the peer's",
			"// signalling for it is accepted, and it appears in CurrentSessions.",
			"UpsertSession(session WebRTCSessionStruct) error",
			"// RemoveSession stops tracking a session, after the host tore its",
			"// stream down; a key no session has changes nothing.",
			"RemoveSession(key SessionKey)",
		},
	},
	"WebRtcTransportProvider": {
		server: "cluster/camera Provider",
		methods: []string{
			"// UpsertSession stores or replaces a session of CurrentSessions by",
			"// its id — the host's change of a session the provider signals",
			"// (connectedhomeip WebRTCTransportProviderCluster::UpsertSession).",
			"UpsertSession(session WebRTCSessionStruct) error",
			"// RemoveSession drops the session of the id from CurrentSessions;",
			"// an unknown id changes nothing (chip's RemoveSession).",
			"RemoveSession(sessionID uint16)",
		},
	},
	"PushAvStreamTransport": {
		server: "cluster/camera PushTransport",
		methods: []string{
			"// TransportStarted emits PushTransportBegin for a connection the",
			"// host started; an unknown connection is NOT_FOUND",
			"// (connectedhomeip NotifyTransportStarted).",
			"TransportStarted(connectionID uint16, triggerType TransportTriggerTypeEnum, activationReason *TriggerActivationReasonEnum) error",
			"// TransportStopped emits PushTransportEnd for a connection the",
			"// host stopped (chip's NotifyTransportStopped).",
			"TransportStopped(connectionID uint16) error",
		},
	},
	"TlsCertificateManagement": {
		server: "cluster/tls CertificateManagement",
		types: []string{
			"// DependencyChecker answers whether a certificate is still named by",
			"// a provisioned TLS endpoint, which keeps it from being removed",
			"// (INVALID_IN_STATE); the node's TlsClientManagement server is one.",
			"// Mirrors connectedhomeip Tls::CertificateDependencyChecker",
			"// (tls-certificate-management-server/CertificateTable.h:212-237).",
			"type DependencyChecker interface {",
			"\tRootCertificateInUse(fabricIndex uint8, caid uint16) bool",
			"\tClientCertificateInUse(fabricIndex uint8, ccdid uint16) bool",
			"}",
		},
		methods: []string{
			"// RootCertificate returns a fabric's root certificate of the id.",
			"RootCertificate(fabricIndex uint8, caid uint16) (TLSCertStruct, bool)",
			"// ClientCertificate returns a fabric's client certificate of the",
			"// id; ClientCertificate is null until it is provisioned.",
			"ClientCertificate(fabricIndex uint8, ccdid uint16) (TLSClientCertificateDetailStruct, bool)",
			"// SetDependencyChecker installs the check of a removal; without",
			"// one every certificate may be removed.",
			"SetDependencyChecker(checker DependencyChecker)",
		},
	},
	"TlsClientManagement": {
		server: "cluster/tls ClientManagement",
		methods: []string{
			"// ProvisionedEndpoint returns a fabric's provisioned TLS endpoint",
			"// (connectedhomeip FindProvisionedEndpointByID); the",
			"// PushAvStreamTransport server validates a TLSEndpointID with it.",
			"ProvisionedEndpoint(fabricIndex uint8, endpointID uint16) (TLSEndpointStruct, bool)",
			"// MutateEndpointReferenceCount counts a user of an endpoint up or",
			"// down; an endpoint in use is not removed (chip's",
			"// MutateEndpointReferenceCount).",
			"MutateEndpointReferenceCount(fabricIndex uint8, endpointID uint16, delta int8) error",
			"// RootCertificateInUse and ClientCertificateInUse are the",
			"// TlsCertificateManagement server's dependency check.",
			"RootCertificateInUse(fabricIndex uint8, caid uint16) bool",
			"ClientCertificateInUse(fabricIndex uint8, ccdid uint16) bool",
		},
	},
	"ThreadBorderRouterManagement": {
		server: "cluster/threadbr ManagementServer",
		methods: []string{
			"// Refresh reads the border router again and reports what moved;",
			"// the host schedules it.",
			"Refresh(ctx context.Context) error",
		},
	},
	"ThreadNetworkDiagnostics": {
		server: "cluster/threadbr DiagnosticsServer",
		methods: []string{
			"// Refresh reads the border router again and reports what moved.",
			"Refresh(ctx context.Context) error",
		},
	},
	"EcosystemInformation": {
		server: "cluster/core EcosystemInformation",
		methods: []string{
			"// DirectoryChanged tells the server the host's directory moved:",
			"// DeviceDirectory and LocationDirectory change.",
			"DirectoryChanged()",
		},
	},
}

// hostInterface renders a cluster's Host interface and the typed server's
// Host accessor; nothing for a cluster without a hosts entry.
func (g *generator) hostInterface(w *writer) {
	h, ok := hosts[g.c.Name]
	if !ok {
		return
	}
	w.p("")
	for _, line := range h.types {
		w.p("%s", line)
	}
	if len(h.types) > 0 {
		w.p("")
	}
	w.p("// Host is the %s kit server's host methods: the ones Set, Get and", g.c.Name)
	w.p("// the typed handlers do not reach (%s).", h.server)
	for _, line := range h.doc {
		w.p("// %s", line)
	}
	w.p("type Host interface {")
	for _, m := range h.methods {
		w.p("\t%s", m)
	}
	w.p("}")
	w.p("")
	w.p("// Host returns the mounted server's host methods when it implements")
	w.p("// Host — the kit's server does; nil for a server that does not, as the")
	w.p("// generated one and a host's own (Declare.HostServer) normally do not.")
	w.p("func (s *Server) Host() Host {")
	w.p("\th, _ := s.Underlying().(Host)")
	w.p("\treturn h")
	w.p("}")
}

// hostImports are the packages the cluster's ports and Host name beyond
// the generated ones, sorted, each once.
func hostImports(name string) []string {
	imps := slices.Clone(ports[name].imports)
	for _, imp := range hosts[name].imports {
		if !slices.Contains(imps, imp) {
			imps = append(imps, imp)
		}
	}
	slices.Sort(imps)
	return imps
}
