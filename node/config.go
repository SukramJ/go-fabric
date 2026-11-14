// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	wifidiagdef "github.com/SukramJ/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/setup"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/surface"
)

// Advertiser is the type of [Config].Advertiser: what publishes the node
// over DNS-SD ([mdns.Advertiser]). An owned name for the seam; the two
// names are one type, so a value of either is the other.
type Advertiser = mdns.Advertiser

// Groups is the type of [Config].Groups and of what [Node.Groups]
// returns: the group state one node shares between its Groups,
// GroupKeyManagement and Groupcast servers and its group messaging
// ([groups.Manager]). An owned name for the seam, as [Advertiser] is.
type Groups = groups.Manager

// Config is the node's configuration, code-first (ADR 0018 decision 5).
// Its groups are the behaviour ids matter.js's ServerNode.Configuration
// keys its options by — `network`, `commissioning`, `productDescription`,
// `basicInformation` (packages/node/src/node/ServerNode.ts:107-127) — for
// the subset the reference daemon uses; the rest is what the daemon's
// bring-up needs beyond them (examples/reference-bridge/main.go:84-497 at b4ea1f96^).
type Config struct {
	// Network mirrors matter.js `network` (NetworkServer.ts:202-222).
	Network Network
	// Commissioning mirrors matter.js `commissioning`
	// (CommissioningServer.ts:400-410).
	Commissioning Commissioning
	// ProductDescription mirrors matter.js `productDescription`
	// (ProductDescriptionServer.ts:70-90): the identity the commissionable
	// advertisement carries.
	ProductDescription ProductDescription
	// BasicInformation mirrors matter.js `basicInformation`
	// (examples/device-bridge-onoff/src/BridgedDevicesNode.ts:29-65).
	BasicInformation BasicInformation
	// Store is where the node keeps its fabrics, credentials, ACL, groups,
	// subscriptions, settings and endpoint numbers.
	Store Store
	// Advertiser publishes the node over DNS-SD. nil is the node's own:
	// the module's mDNS responder (mdns.NewResponder), configured by
	// [Network].MDNS, or mdns.NewNoop() with [MDNS].Disabled. [Node.Stop]
	// closes it, the host's as the node's.
	Advertiser Advertiser
	// DeviceTypeValidation is the bridge's device-type judgement mode
	// (bridge.SetDeviceTypeValidation); the zero value is matter.js's
	// warn mode.
	DeviceTypeValidation DeviceTypeValidationMode
	// Attestation is the device attestation material; the zero value is
	// the CSA test chain and test Certification Declaration for the
	// configured vendor and product (attestation.BuildTestChain,
	// attestation.BuildTestCertificationDeclaration) — pairable with every
	// commissioner's test PAA, and not shippable.
	Attestation Attestation
	// OTA mounts the OTA Software Update Requestor on the root when set;
	// nil leaves it out (and the OtaRequestor device type with it).
	OTA *OTAConfig
	// ICD makes the node an intermittently connected device: ICD
	// Management on the root, Check-Ins to its registered clients, the
	// ICD advertisement ([ICDConfig], [Node.ICD]). nil, the default, is a
	// node that is no ICD: nothing mounts and nothing changes.
	ICD *ICDConfig
	// TLS mounts TlsCertificateManagement and TlsClientManagement on the
	// root and hands them to the topology's PushAvStreamTransport servers
	// ([TLSConfig], [RootClusters].TLSCertificateManagement); nil mounts
	// neither.
	TLS *TLSConfig
	// OTAProvider serves OTA images: the OTA Software Update Provider on an
	// endpoint of device type OtaProvider beside the Aggregator, its images
	// over BDX (ADR 0021). nil, or no ImageSource, mounts none. With
	// Topology set the node builds the provider ([Node.OTAProvider]) and
	// the host mounts it.
	OTAProvider *OTAProviderConfig
	// CommissionerControl mounts the CommissionerControl server on the
	// Aggregator (ADR 0020 stage 6); nil mounts none. The reference daemon
	// mounts none.
	CommissionerControl *CommissionerControlConfig
	// FixedLabels is the root FixedLabel list; nil serves an empty list.
	FixedLabels []fixedlabeldef.LabelStruct
	// Locales are the root LocalizationConfiguration's SupportedLocales,
	// the first of them its ActiveLocale; nil is en-US alone. matter.js
	// detects the active locale and lists it alone when the host states
	// neither (LocalizationConfigurationServer.ts:14-21 initialize); a host
	// states its own, as the reference daemon does (en-US, de-DE).
	Locales []string
	// Groups, when set, is the group state the node runs on, for a host
	// whose own assembler ([Config].Snapshotter) mounts the stack's Groups and
	// ScenesManagement servers over it (endpoint.Config.Groups): one
	// manager serves the endpoints' servers, the bridge's group-message
	// routing and the root's GroupKeyManagement and Groupcast, as matter.js
	// has one GroupKeyManagementServer the GroupsServers of every endpoint
	// write (GroupsServer.ts). The facade loads it (groups.Manager.Load)
	// and attaches it; nil builds one over the credential store, which
	// [Node.Groups] returns for a host that binds its assembler after New.
	// An advanced seam (docs/guides/advanced-seams.md): a host with the
	// facade's own assembler leaves it nil.
	Groups *Groups
	// Snapshotter, when set, is the host's own topology — a host that
	// assembles its endpoints itself (its own endpoint.Assembler over its
	// own device model, one endpoint.Snapshot per scope, its own label and
	// scene persistence) hands the bridge its Snapshotter here, as the
	// reference daemon did before the facade (main.go:188 at b4ea1f96^, bridge.New's
	// first argument). The facade then builds no assembler and no
	// [Aggregator] ([Node.Aggregator] is nil); the root and Aggregator
	// endpoints' servers, the PartsList providers and every port stay the
	// facade's. nil is the facade's [Aggregator] under one scope.
	Snapshotter Snapshotter
	// EndpointPersistence, when set, replaces the facade's own scene and
	// bridged-NodeLabel persistence ([NewEndpointPersistence] over the
	// node's store) in the facade's assembler and [Aggregator], for a host
	// that keeps that state itself; [Node.Persistence] returns it. nil is
	// the facade's own.
	EndpointPersistence EndpointPersistence
	// AggregatorServers are mounted on the Aggregator endpoint after
	// Identify, Descriptor, FixedLabel and UserLabel — an Actions server,
	// say (the daemon's buildAggregatorClusters extra, wiring.go:330-345 at b4ea1f96^).
	AggregatorServers []contract.ClusterServer
	// RootServers are mounted on the root endpoint after the root's own
	// servers — a PowerSource on a battery node's root, say: matter.js's
	// ServerNode.RootEndpoint.with(...) (ClientNodeTest.ts:3192-3205 at
	// 862ad75f puts PowerSourceServer.with("Battery") there). A cluster
	// the root already serves is refused.
	RootServers []contract.ClusterServer
	// specificationVersion is the SpecificationVersion the node reports,
	// 0 its own. TESTING ONLY, set through
	// internal/nodeseam.UseSpecificationVersion: a test stands in for a
	// node of an older release.
	specificationVersion uint32
	// Surface are the surface policies the node's devices are composed
	// under (package surface, ADR 0030). A device carries the digest of the
	// policies it was built with (device.Options.Surface), and
	// [Aggregator.Add] refuses one whose digest is not this one's
	// ([ErrPolicyMismatch]). The zero value is no policy.
	Surface surface.Policies
	// SupportedFabrics is OperationalCredentials' SupportedFabrics, the
	// number of fabrics the node admits (Matter §11.18.4.4: 1 to 254); 0
	// is 5, the facade's value before it was configurable. matter.js
	// defaults it to 254 when unset (OperationalCredentialsServer.ts:
	// 84-88 initialize), which cluster/core does too; the facade keeps 5.
	SupportedFabrics uint8
	// TimeSynchronization is the root TimeSynchronization cluster
	// (0x0038), mounted by default ([TimeSynchronization]); its Off
	// leaves it off the root.
	TimeSynchronization TimeSynchronization
	// GeneralDiagnosticsFeatures selects the root GeneralDiagnostics'
	// features (mattercore.NewGeneralDiagnosticsWith); nil keeps
	// matter.js's default selection, DataModelTest
	// (GeneralDiagnosticsServer.ts:51-52). Without DataModelTest a
	// MaxPathsPerInvoke above one fails [Node.Start]
	// (mattercore.GeneralDiagnostics.CheckDataModelTest).
	GeneralDiagnosticsFeatures *GeneralDiagnosticsFeatures
	// Faults is the host's port of the faults the device detects
	// (contract.FaultSource): the root GeneralDiagnostics serves
	// ActiveHardwareFaults, ActiveRadioFaults and ActiveNetworkFaults
	// from it and emits HardwareFaultChange, RadioFaultChange and
	// NetworkFaultChange with the current and the previous list when one
	// changes, as matter.js GeneralDiagnosticsServer's register…Fault and
	// clear…Fault do (GeneralDiagnosticsServer.ts:296-411 at db021da0).
	// nil serves neither the lists nor the events.
	Faults contract.FaultSource
	// MaxPathsPerInvoke is the most commands one InvokeRequest may carry:
	// the root BasicInformation advertises it, the PASE and CASE session
	// parameters carry it and the invoke path refuses a larger batch with
	// InvalidAction. 0 is im.DefaultMaxPathsPerInvoke (10), matter.js's
	// DEFAULT_MAX_PATHS_PER_INVOKE. Mirrors matter.js
	// basicInformation.maxPathsPerInvoke, which InteractionServer.ts:198
	// enforces.
	MaxPathsPerInvoke uint16
	// Sessions are the MRP intervals the node reports in its session
	// parameters and its operational DNS-SD record ([SessionIntervals]);
	// nil is the Matter defaults, 500 ms idle, 300 ms active and a 4000 ms
	// active threshold. An ICD's operational record carries the ICD's
	// active threshold and, while LIT, no idle interval
	// (IcdManagementServer.ts:482-495); its session parameters keep these.
	Sessions *SessionIntervals
	// CapabilityMinima is the root BasicInformation's CapabilityMinima:
	// a zero field takes matter.js's default (the Matter floor of 3 for
	// CaseSessionsPerFabric and SubscriptionsPerFabric, 20 for the four
	// revision-6 fields, BasicInformationServer.ts initialize), a value
	// the host sets wins (CapabilityMinimaTest.ts "caller-provided values
	// win").
	CapabilityMinima CapabilityMinimaStruct
	// Limits are the capacity limits a host may set ([Limits]); the zero
	// value keeps every default.
	Limits Limits
	// Client is the node's client stack ([ClientConfig]): the socket,
	// exchanges and peers its Binding entries reach other nodes through.
	// nil runs without one ([Node.Peers] is nil, a node Binding entry is
	// dropped).
	Client *ClientConfig
	// Health runs the node's health probe ([Health], [Node.Health]); nil
	// runs none.
	Health *Health
	// Observers are the host's callbacks on fabric and commissioning
	// events, each chained after the facade's own handler (observers.go);
	// the zero value observes nothing.
	Observers Observers
	// Metrics receives the node's named counters and gauges ([Metrics],
	// the Metric… constants); nil records nothing. node/metrics/expvar
	// publishes them on expvar.
	Metrics Metrics
	// Diagnostics sizes the node's diagnostic trace ([Diagnostics],
	// [Node.DiagnosticEvents]); the zero value keeps 256 events.
	Diagnostics Diagnostics
	// Logger is the node's logger; nil is slog.Default().
	// docs/guides/observability.md names the attribute keys and event
	// prefixes the module logs with.
	Logger *slog.Logger
	// Clock is the node's time source; nil is [clock.System]. The node
	// hands it to the bridge (every device-side interval: the ack pump,
	// MRP, subscriptions' timers, the commissioning window, PASE lockout),
	// the session table, the subscription engine, the PASE and CASE
	// providers, the operational resolver, an advertiser that takes a
	// clock ([mdns.Responder]), the group sender, the root's cluster
	// servers, OTA, the store's persist loop and the reopen poll. A test
	// passes a [github.com/SukramJ/go-fabric/clock/clocktest.Fake].
	Clock clock.Clock
}

// Network is matter.js `network` for the keys the daemon uses
// (NetworkServer.ts:202-222: port and listening address, ipv4, tcp).
type Network struct {
	// Listen is the UDP listen address; empty is ":5540".
	Listen string
	// PreferIPv4 forces an IPv4-only socket.
	PreferIPv4 bool
	// TCP selects the Matter TCP transport; the zero value is TCP off. A
	// TCP role is also what the node's PASE and CASE responders advertise
	// in their session parameters (SupportedTransports: tcpServer for
	// Incoming, tcpClient for Outgoing), so a peer learns it during the
	// handshake and may open TCP to us — matter.js
	// ServerNetworkRuntime.ts:355-363.
	TCP TCPConfig
	// BLE is the host's Bluetooth LE radio in the peripheral role: with
	// it an uncommissioned node is commissionable over BLE, and the
	// onboarding payload says so (the QR code's BLE discovery bit) —
	// matter.js `network.ble` and `discoveryCapabilities.ble`
	// (NetworkServer.ts:28-51 at db021da0). While the node has no fabric,
	// its own window (and a [Node.OpenBasicWindow] window) advertises over
	// BLE as well as over DNS-SD and serves PASE over BTP. The window's end
	// (its timeout, [Node.CloseWindow]) stops only the BLE advertisement
	// (BleAdvertisement.ts:69-71): the GATT service and a central stay, so a
	// commissioning over BLE completes over the same link. The node leaves
	// the radio once it is commissioned — the fail-safe committed at
	// CommissioningComplete (ServerNetworkRuntime.ts:294-308) — and at
	// [Node.Stop]. A commissioned node never touches the radio, not even
	// for a window an administrator opens (:255-275); when its last fabric
	// is removed, the own window that reopens is on BLE again. nil is no
	// BLE.
	BLE contract.BLEPeripheral
	// WiFi is the node's Wi-Fi interface: its Driver joins the network a
	// commissioner hands it (NetworkCommissioning with the Wi-Fi feature),
	// its Reporter answers WiFiNetworkDiagnostics (0x0036). Either or both;
	// a non-nil WiFi with neither is a configuration error. The node states
	// the RootNode device-type condition WiFi, under which RootNode lists
	// WiFiNetworkDiagnostics ("[WiFi]", device/root_node_gen.go), through
	// the assembler's RootDeviceConditions — with [Config].Snapshotter the
	// host's own assembler states it. EthernetNetworkDiagnostics stays
	// mounted beside it. nil is an Ethernet-attached node:
	// NetworkCommissioning with the Ethernet feature, no Wi-Fi cluster.
	WiFi *WiFi
	// MDNS configures the advertiser the node builds when
	// [Config].Advertiser is nil; the zero value advertises (the module's
	// mDNS responder, which announces on matter.js's broadcast schedule).
	MDNS MDNS
	// InterfaceCheck is how often the node compares the host's network
	// interfaces: the mDNS responder while it publishes, to say goodbye to
	// a lost address and announce the current ones, and GeneralDiagnostics
	// while NetworkInterfaces is observed, to report the change. Zero is
	// matter.js's 60 s (INTERFACE_CHECK_INTERVAL, Network.ts:91-95 at
	// db021da0); negative disables the periodic check — a host that
	// learns of changes itself calls [Node.TriggerReannounce], which
	// re-announces the mDNS records; GeneralDiagnostics' NetworkInterfaces
	// is then current on each read and reports no change of its own.
	InterfaceCheck time.Duration
}

// interfaceCheck is the interval [Network.InterfaceCheck] selects: the
// 60 s default for zero, a disabled check (0) for a negative value.
func (n Network) interfaceCheck() time.Duration {
	switch {
	case n.InterfaceCheck < 0:
		return 0
	case n.InterfaceCheck == 0:
		return netwatchDefaultInterval
	}
	return n.InterfaceCheck
}

// TimeSynchronization is the root TimeSynchronization cluster (0x0038),
// mounted by default as the facade always did; RootNode carries it under
// an optional conformance (device/root_node_gen.go, generated from the
// snapshot). A default-on sub-system: its switch is Off, inside its own
// configuration (ADR 0025 principle 2).
type TimeSynchronization struct {
	// Off leaves the cluster off the root. Without it GeneralDiagnostics
	// has no UTC clock: TimeSnapshot answers SystemTimeMs alone and
	// PosixTimeMs null, as matter.js GeneralDiagnosticsServer.timeSnapshot
	// does without a TimeSynchronizationBehavior
	// (GeneralDiagnosticsServer.ts:168-182).
	Off bool
	// Config selects the features TZ, NTPC, NTPS and TSC after
	// connectedhomeip ([TimeSynchronizationConfig]); nil serves UTCTime
	// and Granularity alone. The node persists their state in its store
	// and clears a removed fabric's trusted time source; the config's
	// Store is the node's. Off excludes it.
	//
	// The node's UTC — UTCTime, and GeneralDiagnostics' TimeSnapshot — is
	// [Config].Clock plus an offset, with or without Config: SetUTCTime
	// sets the offset, and so does the time client, which runs from Start
	// while the host clock is no time source (Config's PlatformTime): it
	// reads the trusted time source's node over the client stack
	// ([Config].Client) and falls back to Config's NTPClient asking
	// DefaultNTP, emitting TimeFailure when neither answers. The host
	// clock is never set, and the offset is not persisted: a restart
	// serves the host clock again.
	Config *TimeSynchronizationConfig
}

// WiFi is a Wi-Fi node's interface: its commissioning driver and its
// WiFiNetworkDiagnostics.
type WiFi struct {
	// Driver scans and joins the Wi-Fi networks a commissioner configures:
	// non-nil mounts NetworkCommissioning with the Wi-Fi feature in place
	// of the Ethernet one — the network list of one, the 10 s scan and
	// 20 s connect maxima of connectedhomeip's Linux driver
	// (NetworkCommissioningDriver.h:85-87 at 6170af84). The cluster keeps
	// the committed network in the node's store and rejoins it inside
	// [Node.Start], before the fabrics' operational records are published:
	// a network out of range holds Start for up to the connect timeout.
	// After a successful ConnectNetwork command the node re-announces its
	// DNS-SD records at once ([Node.TriggerReannounce]), as matter.js's
	// example does after its connect
	// (DummyWifiNetworkCommissioningServer.ts:132-133 at db021da0); after
	// the rejoin at Start the trigger finds nothing published yet, and the
	// start sequence publishes the records right after it.
	// wifi/wpasupplicant is the Linux driver; nil mounts the Ethernet
	// server.
	Driver contract.WiFiDriver
	// Reporter answers WiFiNetworkDiagnostics' reads and ResetCounts
	// (WiFiReporter); nil mounts no cluster.
	Reporter WiFiReporter
	// Features is the cluster's FeatureMap: wifidiagdef.FeaturePacketCounts
	// (PKTCNT) and wifidiagdef.FeatureErrorCounts (ERRCNT, which brings
	// ResetCounts).
	Features wifidiagdef.Feature
	// Attributes are the optional attributes served
	// (wifidiagdef.AttrCurrentMaxRate).
	Attributes []uint32
}

// conditionWiFi is the RootNode device-type condition a Wi-Fi node states
// (the Base condition "WiFi", schema/devicetype_requirements_gen.go).
const (
	conditionWiFi     = "WiFi"
	conditionEthernet = "Ethernet"
)

// rootDeviceConditions are the device-type conditions the node states for
// its root endpoint: WiFi when it has a Wi-Fi interface.
func (c *Config) rootDeviceConditions() []string {
	// The root always carries EthernetNetworkDiagnostics (buildRootOptional,
	// unknownEthernet), so it states the Base condition "Ethernet" its
	// `[Ethernet]` requirement names; a Wi-Fi interface adds "WiFi".
	out := []string{conditionEthernet}
	if c.Network.WiFi != nil {
		out = append(out, conditionWiFi)
	}
	// An ICD states the condition RootNode's IcdManagement requirement
	// names ("Sit | Lit", root-node.element.ts:91): LIT for a LIT-capable
	// ICD, SIT otherwise.
	if c.ICD != nil {
		if c.ICD.Mode == ICDModeLIT {
			out = append(out, conditionLIT)
		} else {
			out = append(out, conditionSIT)
		}
	}
	return out
}

// Commissioning is matter.js `commissioning` (CommissioningServer.ts:
// 370-380: passcode, discriminator) plus the SPAKE2+ PBKDF parameters the
// daemon configures (main.go:91-92 at b4ea1f96^).
type Commissioning struct {
	// Passcode is the setup passcode; it must pass setup.IsValidSetupPIN.
	Passcode uint32
	// Discriminator is the 12-bit commissioning discriminator.
	Discriminator uint16
	// PBKDFIterations is the SPAKE2+ PBKDF2 iteration count, within
	// [spake2.IterationsMin, spake2.IterationsMax].
	PBKDFIterations uint32
	// Salt is the SPAKE2+ salt, spake2.PBKDFMinSaltSize to
	// spake2.PBKDFMaxSaltSize bytes.
	Salt []byte
	// LocationCapability is GeneralCommissioning's LocationCapability
	// (Matter §11.10.5.3), the regulatory locations the node supports and
	// SetRegulatoryConfig admits (GeneralCommissioningServer.ts:172-191):
	// mattercore.RegulatoryIndoor (the zero value, the facade's default),
	// RegulatoryOutdoor or RegulatoryIndoorOutdoor.
	LocationCapability uint8
	// BasicCommissioning serves AdministratorCommissioning's Basic feature
	// (BC): an administrator's OpenBasicCommissioningWindow opens a
	// commissioning window against this Passcode under this Discriminator,
	// WindowStatus BasicWindowOpen, where the enhanced command carries its
	// own verifier. Off by default, as matter.js serves the feature only
	// through AdministratorCommissioningServer.with("Basic")
	// (AdministratorCommissioningServer.ts:55-57, :105-117 at 862ad75f);
	// a host that turns it on offers the weaker method knowingly (the
	// window's PASE runs against the fixed setup passcode).
	BasicCommissioning bool
	// TermsAndConditions serves GeneralCommissioning's TermsAndConditions
	// feature (TC): the terms a commissioner has to acknowledge with
	// SetTCAcknowledgements before CommissioningComplete succeeds, after
	// connectedhomeip (register row
	// BD-chip-GeneralCommissioning-TermsAndConditions; matter.js serves no
	// terms, GeneralCommissioningServer.ts:31 at 862ad75f). nil leaves the
	// feature off. The node keeps the acceptance in its store, reverts a
	// tentative one with the fail-safe and resets it with the last fabric.
	// The row is served behind [Config].Surface's Extensions (ADR 0030).
	TermsAndConditions *TermsAndConditions
}

// TermsAndConditions is the requirement [Commissioning].TermsAndConditions
// states: GeneralCommissioning's TCMinRequiredVersion, the acknowledgement
// bits every acceptance has to carry, and the optional TCUpdateDeadline
// (Matter epoch seconds; nil reads null).
type TermsAndConditions struct {
	Version          uint16
	Acknowledgements uint16
	UpdateDeadline   *uint32
}

// core converts the requirement to the server's configuration.
func (t *TermsAndConditions) core() *mattercore.TermsAndConditionsConfig {
	if t == nil {
		return nil
	}
	return &mattercore.TermsAndConditionsConfig{
		RequiredVersion: t.Version, RequiredAcknowledgements: t.Acknowledgements, UpdateDeadline: t.UpdateDeadline,
	}
}

// ProductDescription is matter.js `productDescription`
// (ProductDescriptionServer.ts:70-90).
type ProductDescription struct {
	// Name is the advertised device name; empty is
	// BasicInformation.NodeLabel, as the daemon advertises (main.go:285 at b4ea1f96^).
	Name string
	// DeviceType is the advertised device type; 0 is RootNode (0x0016),
	// as the daemon advertises (main.go:290 at b4ea1f96^).
	DeviceType uint16
	// VendorID and ProductID are the node's identity; both mandatory.
	VendorID, ProductID uint16
}

// BasicInformation is the root BasicInformation the daemon configures
// (examples/reference-bridge/wiring.go:105-116 at b4ea1f96^, buildRootClusters), plus
// ProductLabel from matter.js's example (BridgedDevicesNode.ts:29-65).
type BasicInformation struct {
	VendorName   string
	ProductName  string
	ProductLabel string
	NodeLabel    string // mandatory
	SerialNumber string
	// UniqueID pins BasicInformation.UniqueID (quality F, Matter
	// §11.1.5.13) to a value the host persisted itself; at most 32 UTF-8
	// bytes. Empty, the facade pins it: the first boot of a store derives
	// one (mattercore.DeriveUniqueID over the vendor, product, NodeLabel
	// and SerialNumber) and keeps it under store.SettingUniqueID, every
	// later boot serves the stored value — matter.js creates uniqueId
	// once when the state has none and persists it
	// (BasicInformationServer.ts:89-91), so a later edit to NodeLabel does
	// not re-identify the node under a controller's cache. A boot with
	// bootid rotation enabled serves a per-boot value and pins nothing, so
	// the next boot without rotation serves the value pinned before it.
	// Leave the field empty unless the host owns the node's identity: a
	// value here replaces the facade's pin, silently.
	UniqueID              string
	HardwareVersion       uint16
	HardwareVersionString string
	SoftwareVersion       uint32
	SoftwareVersionString string
}

// Store names the node's persistence. With DB the node applies both
// schemas (store.Apply, sqlitestore.Apply) and opens the two stores on it,
// as the daemon's openDB does (persist.go:95-110); the caller picks the
// SQLite driver and owns the handle. With Path the node opens that SQLite
// file itself and treats it as DB. Credentials and Endpoints override the
// stores built from DB or Path, or stand in for them when both are unset.
type Store struct {
	// Path is a SQLite file the node opens (creating it on first use)
	// with the pure-Go driver modernc.org/sqlite, foreign keys on and a
	// 5 s busy timeout; [Node.Stop] closes it. It excludes DB.
	Path        string
	DB          *sql.DB
	Credentials store.Port
	Endpoints   endpoint.Store
}

// Attestation is the device attestation material.
type Attestation struct {
	// Files names a vendor chain on disk; [New] loads it into Chain and
	// CertificationDeclaration, which it excludes. Files that name
	// nothing keep the CSA test chain; a chain that does not load fails
	// New.
	Files *AttestationFiles
	// BlockCommissioningOnError keeps a node whose Files do not load
	// running: it serves the test chain, refuses every commissioning
	// ([Node.SetCommissioningBlocked] with the load error) and reports the
	// error in [Node.AttestationError]. Controllers already paired keep
	// working — attestation is checked only while a controller
	// commissions. It needs Files.
	BlockCommissioningOnError bool
	// Chain is the DAC, its key and the PAI; nil is the CSA test chain. A
	// host with a chain of its own (an HSM-held key, say) sets it here.
	// Its DAC and PAI must name the ProductDescription's vendor and
	// product, as a chain Files loads must: another product's fails New
	// with attestation.ErrProductMismatch.
	Chain *AttestationChain
	// CertificationDeclaration is the CMS-signed CD; nil is the CSA test
	// CD for the vendor and product.
	CertificationDeclaration []byte
	// Provider supplies the chain and the CD when a commissioner first
	// asks for them — the first AttestationRequest, CertificateChainRequest
	// or CSRRequest — never at [New] or [Node.Start]; a nil chain or CD it
	// returns is the CSA test material, as above. A failed call fails that
	// request and is retried on the next; so does a chain of another
	// product than ProductDescription's (attestation.ErrProductMismatch). It excludes Files, Chain and
	// CertificationDeclaration. Mirrors matter.js
	// OperationalCredentialsServer.State.certification, which takes an
	// async provider (packages/protocol/src/certificate/
	// DeviceCertification.ts:181, built on first use by
	// OperationalCredentialsServer.ts:560-572).
	Provider func(ctx context.Context) (chain *AttestationChain, certificationDeclaration []byte, err error)
}

// OTAConfig mounts the OTA Software Update Requestor (ADR 0019).
type OTAConfig struct {
	// Dir receives the downloads; mandatory.
	Dir string
	// harness applies an image in place, reporting its version as running
	// without a restart. TESTING ONLY, set through
	// internal/nodeseam.UseOTAHarness — see the daemon's otaApplier.
	harness bool
	// Applier carries out an update; nil is the daemon's applier, which
	// validates the image and keeps it under Dir/applied.
	Applier OTAApplier
	// AnnouncedUpdateQueryDelay, when set, replaces the random 1-600 s wait
	// between an UpdateAvailable or Urgent announcement and the query it
	// schedules — matter.js's announcedUpdateQueryDelay
	// (OtaSoftwareUpdateRequestorServer.ts:410, :1323). Harness mode (a test's)
	// without it waits 250 ms, as the matter.js cert requestor does.
	AnnouncedUpdateQueryDelay *time.Duration
	// TransferProtocols are the download protocols the requestor announces
	// (mattercore.OTARequestorConfig.TransferProtocols); empty is BDX
	// alone. Add [OTADownloadHTTPS] only where the node reaches the image
	// hosts; a list without [OTADownloadBDXSynchronous] fails the build.
	TransferProtocols []OTADownloadProtocol
	// HTTPClient sends an HTTPS download's GET; nil is the module's client,
	// which follows a redirect only to an https:// target. A host's client
	// keeps its own redirect policy (mattercore.OTARequestorConfig).
	HTTPClient OTAHTTPClient
	// HTTPSMaxImageSize caps an HTTPS download, HTTPSIdleTimeout the time it
	// may receive nothing; 0 is 256 MiB and 60 seconds
	// (mattercore.OTARequestorConfig).
	HTTPSMaxImageSize int64
	HTTPSIdleTimeout  time.Duration
}

// ErrInvalidConfig wraps every [Config.Validate] failure.
var ErrInvalidConfig = errors.New("node: invalid configuration")

// setupPasscodeMax is the Matter §5.1.1.6 upper bound the daemon checks
// (main.go:121-125 at b4ea1f96^).
const setupPasscodeMax = 99999998

// Fabric-table bounds: Matter §11.18.4.4 limits SupportedFabrics to 1..254
// (matter.js: "FabricBuilder uses 254 as max",
// OperationalCredentialsServer.ts:86); the facade's default is 5.
const (
	maxSupportedFabrics     = 254
	defaultSupportedFabrics = 5
)

// maxUniqueIDBytes is the UniqueID attribute's length ceiling (Matter
// §11.1.5.13, 32 UTF-8 bytes), the bound mattercore.NewBasicInformation
// enforces too.
const maxUniqueIDBytes = 32

// validSetupPIN is the one passcode check: the Matter §5.1.1.6 range and
// the trivially-guessable list.
func validSetupPIN(pin uint32) bool { return pin <= setupPasscodeMax && setup.IsValidSetupPIN(pin) }

// errSetupPIN is the failure every passcode check reports. It names the
// rule and not the value: the text reaches the host's log, and a refused
// passcode is still the passcode of the device it was configured for —
// the rule secure/setup's errors follow (review II, R2-P0-sec-E04).
var errSetupPIN = errors.New("node: the setup passcode is invalid: zero, above 99999998 or on the trivially-guessable list")

// The checks [Config.Validate] and [Node.OpenBasicWindow] share, against
// the bounds above.
func checkDiscriminator(d uint16) error {
	if d > MaxDiscriminator {
		return fmt.Errorf("node: discriminator %d exceeds the 12-bit maximum %d", d, MaxDiscriminator)
	}
	return nil
}

func checkPBKDF(iterations int64, saltLen int) []error {
	var errs []error
	if iterations < PBKDFIterationsMin || iterations > PBKDFIterationsMax {
		errs = append(errs, fmt.Errorf("node: PBKDF iterations %d not in [%d, %d]", iterations, PBKDFIterationsMin, PBKDFIterationsMax))
	}
	if saltLen < PBKDFSaltMin || saltLen > PBKDFSaltMax {
		errs = append(errs, fmt.Errorf("node: PBKDF salt is %d bytes, want %d to %d", saltLen, PBKDFSaltMin, PBKDFSaltMax))
	}
	return errs
}

// Validate holds the checks the daemon applies to its flags
// (main.go:113-131 at b4ea1f96^): the discriminator's 12 bits, the passcode's range
// and the trivially-guessable list, the PBKDF iteration range and salt
// length the SPAKE2+ verifier needs, and the identity the bridge and the
// assembler refuse to start without. The bounds are this package's
// [MaxDiscriminator], [PBKDFIterationsMin], [PBKDFIterationsMax],
// [PBKDFSaltMin] and [PBKDFSaltMax]; the passcode check is
// [ValidateSetupPIN] — a host checks its own configuration against them
// before New.
func (c *Config) Validate() error {
	var errs []error
	cm := c.Commissioning
	if err := checkDiscriminator(cm.Discriminator); err != nil {
		errs = append(errs, err)
	}
	if !validSetupPIN(cm.Passcode) {
		errs = append(errs, errSetupPIN)
	}
	errs = append(errs, checkPBKDF(int64(cm.PBKDFIterations), len(cm.Salt))...)
	if c.ProductDescription.VendorID == 0 || c.ProductDescription.ProductID == 0 {
		errs = append(errs, errors.New("node: ProductDescription.VendorID and ProductID must be non-zero"))
	}
	if strings.TrimSpace(c.BasicInformation.NodeLabel) == "" {
		errs = append(errs, errors.New("node: BasicInformation.NodeLabel must be non-empty"))
	}
	switch {
	case c.Store.Path != "" && c.Store.DB != nil:
		errs = append(errs, errors.New("node: Store takes Path or DB, not both"))
	case c.Store.Path == "" && c.Store.DB == nil && (c.Store.Credentials == nil || c.Store.Endpoints == nil):
		errs = append(errs, errors.New("node: Store needs Path, DB, or both Credentials and Endpoints"))
	}
	// A mode that is none of warn, strict and off fails the node's
	// construction. Mirrors matter.js
	// packages/node/src/node/server/DeviceTypeConformanceService.ts:674-685
	// at 862ad75f (modeOf: an unknown endpoint.validation value throws).
	switch c.DeviceTypeValidation {
	case endpoint.DeviceTypeValidationWarn, endpoint.DeviceTypeValidationStrict, endpoint.DeviceTypeValidationOff:
	default:
		errs = append(errs, fmt.Errorf("node: DeviceTypeValidation %d is not a mode; it must be one of warn, strict, off", int(c.DeviceTypeValidation)))
	}
	if cm.LocationCapability > mattercore.RegulatoryIndoorOutdoor {
		errs = append(errs, fmt.Errorf("node: LocationCapability %d is not a RegulatoryLocationTypeEnum value", cm.LocationCapability))
	}
	if len(c.BasicInformation.UniqueID) > maxUniqueIDBytes {
		errs = append(errs, fmt.Errorf("node: BasicInformation.UniqueID is %d bytes, at most %d", len(c.BasicInformation.UniqueID), maxUniqueIDBytes))
	}
	if c.SupportedFabrics > maxSupportedFabrics {
		errs = append(errs, fmt.Errorf("node: SupportedFabrics %d exceeds %d", c.SupportedFabrics, maxSupportedFabrics))
	}
	errs = append(errs, c.Limits.validate()...)
	errs = append(errs, c.Sessions.validate()...)
	errs = append(errs, c.timeSynchronizationErrors()...)
	if at := c.Attestation; at.Provider != nil && (at.Files != nil || at.Chain != nil || at.CertificationDeclaration != nil) {
		errs = append(errs, errors.New("node: Attestation.Provider excludes Files, Chain and CertificationDeclaration"))
	} else if at.Files != nil && (at.Chain != nil || at.CertificationDeclaration != nil) {
		errs = append(errs, errors.New("node: Attestation.Files excludes Chain and CertificationDeclaration"))
	} else if at.Files == nil && at.BlockCommissioningOnError {
		errs = append(errs, errors.New("node: Attestation.BlockCommissioningOnError needs Files"))
	}
	errs = append(errs, c.optionalErrors()...)
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return nil
}

// optionalErrors checks the optional sub-systems a Config turns on: each
// one's own required fields.
func (c *Config) optionalErrors() []error {
	var errs []error
	if c.Health != nil && c.Health.Interval < 0 {
		errs = append(errs, fmt.Errorf("node: Health.Interval %v is negative", c.Health.Interval))
	}
	if w := c.Network.WiFi; w != nil && w.Driver == nil && w.Reporter == nil {
		errs = append(errs, errors.New("node: Network.WiFi needs its Driver, its Reporter or both"))
	}
	for i, name := range c.Network.MDNS.Interfaces {
		switch {
		case strings.TrimSpace(name) == "":
			errs = append(errs, fmt.Errorf("node: Network.MDNS.Interfaces[%d] is empty", i))
		case slices.Contains(c.Network.MDNS.Interfaces[:i], name):
			errs = append(errs, fmt.Errorf("node: Network.MDNS.Interfaces names %q twice", name))
		}
	}
	if c.TLS != nil && c.TLS.CertificatePort == nil {
		errs = append(errs, errNoCertificatePort)
	}
	if c.OTA != nil && c.OTA.Dir == "" {
		errs = append(errs, errors.New("node: OTA.Dir must be set"))
	}
	if c.ICD != nil {
		if err := c.ICD.validate(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// timeSynchronizationErrors refuses a TimeSynchronization Config beside
// its Off.
func (c *Config) timeSynchronizationErrors() []error {
	if c.TimeSynchronization.Off && c.TimeSynchronization.Config != nil {
		return []error{errors.New("node: TimeSynchronization.Config excludes TimeSynchronization.Off")}
	}
	return nil
}
