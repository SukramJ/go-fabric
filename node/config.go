// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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
	// mdns.Zeroconf with the subtype responder, configured by
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
	OTA *OtaConfig
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
	// whose own assembler ([Config].Topology) mounts the stack's Groups and
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
	// Topology, when set, is the host's own topology — a host that
	// assembles its endpoints itself (its own endpoint.Assembler over its
	// own device model, one endpoint.Snapshot per scope, its own label and
	// scene persistence) hands the bridge its Snapshotter here, as the
	// reference daemon did before the facade (main.go:188 at b4ea1f96^, bridge.New's
	// first argument). The facade then builds no assembler and no
	// [Aggregator] ([Node.Aggregator] is nil); the root and Aggregator
	// endpoints' servers, the PartsList providers and every port stay the
	// facade's. nil is the facade's [Aggregator] under one scope.
	Topology Snapshotter
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
	// SupportedFabrics is OperationalCredentials' SupportedFabrics, the
	// number of fabrics the node admits (Matter §11.18.4.4: 1 to 254); 0
	// is 5, the facade's value before it was configurable. matter.js
	// defaults it to 254 when unset (OperationalCredentialsServer.ts:
	// 84-88 initialize), which cluster/core does too; the facade keeps 5.
	SupportedFabrics uint8
	// DisableTimeSynchronization leaves the TimeSynchronization cluster
	// (0x0038) off the root; the zero value mounts it, as the facade
	// always did. RootNode carries it under an optional conformance
	// (device/root_node_gen.go, generated from the snapshot).
	// Without it GeneralDiagnostics has no UTC clock: TimeSnapshot
	// answers SystemTimeMs alone and PosixTimeMs null, as matter.js
	// GeneralDiagnosticsServer.timeSnapshot does without a
	// TimeSynchronizationBehavior (GeneralDiagnosticsServer.ts:168-182).
	DisableTimeSynchronization bool
	// TimeSynchronization selects the root TimeSynchronization's features
	// TZ, NTPC, NTPS and TSC after connectedhomeip
	// ([TimeSynchronizationConfig]); nil serves UTCTime and Granularity
	// alone, as before. The node persists their state in its store and
	// clears a removed fabric's trusted time source; the config's Store is
	// the node's. UTCTime stays the node's clock. It excludes
	// DisableTimeSynchronization.
	TimeSynchronization *TimeSynchronizationConfig
	// GeneralDiagnosticsFeatures selects the root GeneralDiagnostics'
	// features (mattercore.NewGeneralDiagnosticsWith); nil keeps
	// matter.js's default selection, DataModelTest
	// (GeneralDiagnosticsServer.ts:51-52). Without DataModelTest a
	// MaxPathsPerInvoke above one fails [Node.Start]
	// (mattercore.GeneralDiagnostics.CheckDataModelTest).
	GeneralDiagnosticsFeatures *GeneralDiagnosticsFeatures
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
	// Client is the node's client stack ([Client]): the socket, exchanges
	// and peers its Binding entries reach other nodes through. The zero
	// value runs without one.
	Client Client
	// BootID selects the development rotation of UniqueIDs ([BootID]); the
	// zero value keeps every UniqueID stable across restarts.
	BootID BootID
	// Health runs the node's health probe ([Health], [Node.Health]); the
	// zero value runs none.
	Health Health
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
	// clock ([mdns.Zeroconf]), the group sender, the root's cluster
	// servers, OTA, the store's persist loop and the reopen poll. A test
	// passes a [github.com/SukramJ/go-fabric/clock/clocktest.Fake].
	Clock clock.Clock
}

// BootID is the per-boot UniqueID rotation of package bootid, a
// development aid: with RotateUniqueIDs every UniqueID the module derives
// — the root's BasicInformation UniqueID and every bridged endpoint's —
// mixes in a salt drawn once per process, so a controller sees a new
// bridge on every start (bootid.EnableRotation). Rotation is process-wide
// and stays on for the life of the process, for every node in it; a boot
// under rotation pins nothing in the store, so the next boot without it
// serves the UniqueID pinned before. Never set it in production: Apple
// Home and Google Home recognise a bridge's accessories by their
// UniqueIDs across restarts.
type BootID struct {
	// RotateUniqueIDs enables the rotation; [New] enables it before it
	// derives the root's UniqueID.
	RotateUniqueIDs bool
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
	// WiFi mounts WiFiNetworkDiagnostics (0x0036) on the root for a node on
	// Wi-Fi; the zero value (no Reporter) mounts none. NetworkCommissioning
	// stays Ethernet-only either way (concept v2 §B.4): the node states the
	// RootNode device-type condition WiFi, under which RootNode lists the
	// cluster ("[WiFi]", device/root_node_gen.go), through the assembler's
	// RootDeviceConditions — with [Config].Topology the host's own
	// assembler states it. EthernetNetworkDiagnostics stays mounted beside
	// it.
	WiFi WiFi
	// WiFiCommissioning mounts NetworkCommissioning with the Wi-Fi feature
	// instead of Ethernet, its scan and connect done by Driver — what the
	// commissioner's network steps configure. TESTING ONLY: the module's
	// nodes are Ethernet-attached; the reference daemon's --wifi-sim is
	// the one user. The zero value (no Driver) keeps the Ethernet-only
	// server, and the composition is unchanged.
	WiFiCommissioning WiFiCommissioning
	// ReannounceInterval is the cadence at which the node re-publishes its
	// DNS-SD records when its Advertiser is an mdns.Reannouncer
	// (mdns.Zeroconf is): the loop starts on [Node.Start] and stops on
	// [Node.Stop]. 0 is [DefaultReannounceInterval]. The loop is this
	// module's own device, not matter.js's (mdns.Reannouncer).
	ReannounceInterval time.Duration
	// DisableReannounce keeps the re-announce loop off; [Node.TriggerReannounce]
	// still re-publishes on demand.
	DisableReannounce bool
	// MDNS configures the advertiser the node builds when
	// [Config].Advertiser is nil; the zero value advertises (Zeroconf with
	// the subtype responder).
	MDNS MDNS
}

// DefaultReannounceInterval is the re-announce cadence a zero
// [Network].ReannounceInterval selects: 30 minutes, under half the
// 75-minute TTL Apple's mDNSResponder caches the records for, so a cache
// sees the records again before they expire.
const DefaultReannounceInterval = 30 * time.Minute

// WiFiCommissioning is the Wi-Fi NetworkCommissioning configuration
// (mattercore.WiFiNetworkCommissioningConfig).
type WiFiCommissioning struct {
	// Driver scans and connects; nil mounts the Ethernet-only server.
	Driver contract.WiFiDriver
	// MaxNetworks is the size of the network list; 0 is 1.
	MaxNetworks uint8
	// ScanMaxTimeSeconds and ConnectMaxTimeSeconds; 0 are 10 and 20.
	ScanMaxTimeSeconds, ConnectMaxTimeSeconds uint8
}

// WiFi is a Wi-Fi node's WiFiNetworkDiagnostics configuration.
type WiFi struct {
	// Reporter answers the cluster's reads and ResetCounts
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
// its root endpoint: WiFi when it serves WiFiNetworkDiagnostics.
func (c *Config) rootDeviceConditions() []string {
	// The root always carries EthernetNetworkDiagnostics (buildRootOptional,
	// unknownEthernet), so it states the Base condition "Ethernet" its
	// `[Ethernet]` requirement names; a Wi-Fi reporter adds "WiFi".
	out := []string{conditionEthernet}
	if c.Network.WiFi.Reporter != nil {
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
	PBKDFIterations int
	// Salt is the SPAKE2+ salt, spake2.PBKDFMinSaltSize to
	// spake2.PBKDFMaxSaltSize bytes.
	Salt []byte
	// LocationCapability is GeneralCommissioning's LocationCapability
	// (Matter §11.10.5.3), the regulatory locations the node supports and
	// SetRegulatoryConfig admits (GeneralCommissioningServer.ts:172-191):
	// mattercore.RegulatoryIndoor (the zero value, the facade's default),
	// RegulatoryOutdoor or RegulatoryIndoorOutdoor.
	LocationCapability uint8
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
	Chain *AttestationChain
	// CertificationDeclaration is the CMS-signed CD; nil is the CSA test
	// CD for the vendor and product.
	CertificationDeclaration []byte
}

// OtaConfig mounts the OTA Software Update Requestor (ADR 0019).
type OtaConfig struct {
	// Dir receives the downloads; mandatory.
	Dir string
	// Harness applies an image in place, reporting its version as running
	// without a restart. TESTING ONLY — see the daemon's otaApplier.
	Harness bool
	// Applier carries out an update; nil is the daemon's applier, which
	// validates the image and keeps it under Dir/applied.
	Applier OTAApplier
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

// errSetupPIN is the failure every passcode check reports.
func errSetupPIN(pin uint32) error {
	return fmt.Errorf("node: passcode %d is invalid or on the trivially-guessable list", pin)
}

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
		errs = append(errs, errSetupPIN(cm.Passcode))
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
	if at := c.Attestation; at.Files != nil && (at.Chain != nil || at.CertificationDeclaration != nil) {
		errs = append(errs, errors.New("node: Attestation.Files excludes Chain and CertificationDeclaration"))
	} else if at.Files == nil && at.BlockCommissioningOnError {
		errs = append(errs, errors.New("node: Attestation.BlockCommissioningOnError needs Files"))
	}
	if c.Health.Interval < 0 {
		errs = append(errs, fmt.Errorf("node: Health.Interval %v is negative", c.Health.Interval))
	}
	if c.Network.ReannounceInterval < 0 {
		errs = append(errs, fmt.Errorf("node: Network.ReannounceInterval %v is negative", c.Network.ReannounceInterval))
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
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return nil
}

// timeSynchronizationErrors refuses TimeSynchronization beside
// DisableTimeSynchronization.
func (c *Config) timeSynchronizationErrors() []error {
	if c.DisableTimeSynchronization && c.TimeSynchronization != nil {
		return []error{errors.New("node: TimeSynchronization excludes DisableTimeSynchronization")}
	}
	return nil
}
