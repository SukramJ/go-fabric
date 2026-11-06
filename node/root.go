// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/clock"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	ethdiagdef "github.com/SukramJ/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/cluster/tls"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/internal/coreseam"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/store"
)

// Device types of the two system endpoints.
const (
	deviceTypeRootNode   uint32 = 0x0016 // Matter §10.3
	deviceTypeAggregator uint32 = 0x000E // Matter §13.2
)

// Settings keys of the root's and the Aggregator's UserLabel lists, the
// daemon's (root_optional.go:20-26 at b4ea1f96^), so a database the daemon wrote keeps
// its labels under the facade.
const (
	rootUserLabelsKey       = "user_label.root"
	aggregatorUserLabelsKey = "user_label.aggregator"
)

// deviceTypeRevision reads a device type's revision from the generated
// matter.js snapshot (schema.DeviceTypeRevision), so a regeneration
// propagates without an edit.
func deviceTypeRevision(id schema.DeviceTypeID) uint16 {
	rev, _ := schema.DeviceTypeRevision(id) // both system types are in every snapshot
	return rev
}

// RootClusters is endpoint 0's server set: matter.js's root endpoint
// composition (ServerNode.ts:274-309) as the reference daemon builds it
// (examples/reference-bridge/wiring.go:93-299 at b4ea1f96^, buildRootClusters, and
// root_optional.go). The fields are the servers a host may need a hook
// on; [RootClusters.Servers] is the mounted set in mount order.
type RootClusters struct {
	BasicInformation           *mattercore.BasicInformation
	AccessControl              *mattercore.AccessControl
	GeneralCommissioning       *mattercore.GeneralCommissioning
	AdministratorCommissioning *wire.AdministratorCommissioning
	// NetworkCommissioning is the Ethernet server; nil when
	// the Wi-Fi commissioning seam (internal/nodeseam.UseWiFiCommissioning) set a Driver.
	NetworkCommissioning *mattercore.NetworkCommissioning
	// WiFiNetworkCommissioning is the Wi-Fi server, nil unless
	// the Wi-Fi commissioning seam (internal/nodeseam.UseWiFiCommissioning) set a Driver (TESTING ONLY).
	WiFiNetworkCommissioning *mattercore.WiFiNetworkCommissioning
	GeneralDiagnostics       *mattercore.GeneralDiagnostics
	DiagnosticLogs           *mattercore.DiagnosticLogs
	// TimeSynchronization is nil when [Config].TimeSynchronization.Off
	// is set.
	TimeSynchronization    *mattercore.TimeSynchronization
	OperationalCredentials *mattercore.OperationalCredentials
	GroupKeyManagement     *mattercore.GroupKeyManagement
	Groupcast              *mattercore.Groupcast
	Descriptor             *mattercore.Descriptor
	// OTARequestor is nil unless [Config].OTA is set.
	OTARequestor *mattercore.OTASoftwareUpdateRequestor
	// otaApplier is the node's own OTA applier, nil with the host's.
	otaApplier *otaApplier
	// WiFiNetworkDiagnostics is nil unless [Network].WiFi has a Reporter.
	WiFiNetworkDiagnostics *mattercore.WiFiNetworkDiagnostics
	// PowerSourceConfiguration lists the node's PowerSource endpoints;
	// it starts empty, for the host to fill once the topology exists.
	PowerSourceConfiguration *mattercore.PowerSourceConfiguration
	// ICDManagement is nil unless [Config].ICD is set.
	ICDManagement *mattercore.ICDManagement
	// TLSCertificateManagement and TLSClientManagement are nil unless
	// [Config].TLS is set.
	TLSCertificateManagement *tls.CertificateManagement
	TLSClientManagement      *tls.ClientManagement

	servers []contract.ClusterServer
}

// Servers returns the mounted set, in mount order.
func (r *RootClusters) Servers() []contract.ClusterServer { return slices.Clone(r.servers) }

// rootDeps is what buildRoot needs from the node under construction.
type rootDeps struct {
	cfg               *Config
	logger            *slog.Logger
	bootReason        uint8
	st                store.Port
	groups            *groups.Manager
	connector         mattercore.OtaProviderConnector
	onFabricInstalled func(ctx context.Context, fabricIndex uint8, fabricID, nodeID uint64, rootPublicKey []byte)
	// onCommissioningComplete runs after the root's own
	// CommissioningComplete handling (the host's observer).
	onCommissioningComplete func(ctx context.Context, fabricIndex uint8)
	// onFailSafeExpired runs after the root's own fail-safe expiry:
	// the topology's fail-safe participants revert (failsafe.go).
	onFailSafeExpired func(ctx context.Context, fabricIndex uint8)
}

// buildRoot constructs endpoint 0's cluster surface and cross-wires the
// fail-safe hooks, as the daemon's buildRootClusters does
// (wiring.go:93-299 at b4ea1f96^).
func buildRoot(ctx context.Context, d rootDeps) (*RootClusters, error) { //nolint:funlen // one pass over the root's servers, as buildRootClusters
	cfg := d.cfg
	bi := cfg.BasicInformation
	pd := cfg.ProductDescription
	r := &RootClusters{}

	basicInfo, err := mattercore.NewBasicInformation(mattercore.Config{
		VendorName:         bi.VendorName,
		VendorID:           pd.VendorID,
		ProductName:        bi.ProductName,
		ProductID:          pd.ProductID,
		ProductLabel:       bi.ProductLabel,
		NodeLabel:          bi.NodeLabel,
		HardwareVersion:    bi.HardwareVersion,
		HardwareVersionStr: bi.HardwareVersionString,
		SoftwareVersion:    bi.SoftwareVersion,
		SoftwareVersionStr: bi.SoftwareVersionString,
		SerialNumber:       bi.SerialNumber,
		UniqueID:           bi.UniqueID,
		CapabilityMinima:   cfg.CapabilityMinima,
		MaxPathsPerInvoke:  cfg.MaxPathsPerInvoke,
		Logger:             d.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("basic information: %w", err)
	}
	// TESTING ONLY (internal/nodeseam.UseSpecificationVersion); 0, every
	// release node's value, leaves the server's own.
	coreseam.SetSpecificationVersion(basicInfo, cfg.specificationVersion)
	r.BasicInformation = basicInfo

	// RebootCount and the operational time carry over from the previous
	// boots (wiring.go:118-128 at b4ea1f96^).
	if f := cfg.GeneralDiagnosticsFeatures; f != nil {
		r.GeneralDiagnostics = mattercore.NewGeneralDiagnosticsWith(d.bootReason, *f)
	} else {
		r.GeneralDiagnostics = mattercore.NewGeneralDiagnostics(d.bootReason)
	}
	r.GeneralDiagnostics.SetClock(d.cfg.Clock)
	r.GeneralDiagnostics.SetLogger(d.logger)
	reboots, err := countBoot(ctx, d.st)
	if err != nil {
		return nil, fmt.Errorf("general diagnostics: %w", err)
	}
	opTime, err := loadOperationalTime(ctx, d.st)
	if err != nil {
		return nil, fmt.Errorf("general diagnostics: %w", err)
	}
	r.GeneralDiagnostics.SetPersistedCounters(reboots, 0)
	r.GeneralDiagnostics.SetPersistedOperationalTime(opTime)

	if r.GeneralCommissioning, err = newGeneralCommissioning(ctx, d); err != nil {
		return nil, err
	}
	generalCom := r.GeneralCommissioning

	supportedFabrics := cfg.SupportedFabrics
	if supportedFabrics == 0 {
		supportedFabrics = defaultSupportedFabrics
	}
	opcredsCfg := mattercore.OpcredsConfig{
		Logger:           d.logger,
		SupportedFabrics: supportedFabrics,
		// The CASE identity is rebuilt from the freshly written fabric
		// (wiring.go:166-170 at b4ea1f96^).
		OnFabricInstalled: d.onFabricInstalled,
		FailSafe:          generalCom.FailSafeFabric,
		// A successful AddNOC re-arms the fail-safe onto its fabric, Matter
		// §11.18.6.16 (wiring.go:172-178 at b4ea1f96^).
		RearmFailSafeForFabric: generalCom.SetCurrentFabric,
	}
	if err := withAttestation(cfg, &opcredsCfg); err != nil {
		return nil, err
	}
	r.OperationalCredentials, err = mattercore.NewOperationalCredentials(d.st, opcredsCfg)
	if err != nil {
		return nil, fmt.Errorf("operational credentials: %w", err)
	}
	opCreds := r.OperationalCredentials

	// The fail-safe hooks between the two clusters (wiring.go:184-200 at b4ea1f96^).
	generalCom.SetOnFailSafeArmed(func(context.Context, uint8) { opCreds.ClearPendingState() })
	// The topology's fail-safe participants revert after the root's own
	// rollback (failsafe.go).
	participants := d.onFailSafeExpired
	expired := func(ctx context.Context, fi uint8) {
		opCreds.OnFailSafeExpiry(ctx, fi)
		if participants != nil {
			participants(ctx, fi)
		}
	}
	generalCom.SetOnFailSafeExpired(expired)

	deviceTypes := []mattercore.DeviceTypeStruct{{DeviceType: deviceTypeRootNode, Revision: deviceTypeRevision(schema.DeviceTypeID(deviceTypeRootNode))}}
	if cfg.OTA != nil {
		r.OTARequestor, r.otaApplier, err = buildOtaRequestor(cfg, d.connector, basicInfo)
		if err != nil {
			return nil, fmt.Errorf("ota requestor: %w", err)
		}
		deviceTypes = append(deviceTypes, mattercore.OtaRequestorDeviceType())
	}
	otaRequestor := r.OTARequestor
	observer := d.onCommissioningComplete
	commissioned := func(ctx context.Context, fabricIndex uint8) {
		opCreds.ClearPendingState()
		if otaRequestor != nil {
			// A query 30 s after a commissioning (wiring.go:210-215 at b4ea1f96^).
			otaRequestor.Commissioned()
		}
		if observer != nil {
			observer(ctx, fabricIndex)
		}
	}
	generalCom.SetOnCommissioningComplete(commissioned)

	r.AccessControl, err = mattercore.NewAccessControl(d.st) //nolint:contextcheck // the constructor takes no context; it reads the store once
	if err != nil {
		return nil, fmt.Errorf("access control: %w", err)
	}
	r.AccessControl.SetLogger(d.logger)
	r.AccessControl.SetLimits(cfg.Limits.accessControl())
	r.GroupKeyManagement, err = mattercore.NewGroupKeyManagement(d.st, mattercore.GroupKeyMgmtConfig{
		MaxGroupsPerFabric:    cfg.Limits.MaxGroupsPerFabric,
		MaxGroupKeysPerFabric: cfg.Limits.MaxGroupKeysPerFabric,
		Groups:                d.groups,
	})
	if err != nil {
		return nil, fmt.Errorf("group key management: %w", err)
	}
	r.Groupcast, err = mattercore.NewGroupcast(mattercore.GroupcastConfig{ //nolint:contextcheck // the constructor takes no context
		Groups: d.groups, GroupKeyManagement: r.GroupKeyManagement, AccessControl: r.AccessControl,
		Clock: cfg.Clock,
	})
	if err != nil {
		return nil, fmt.Errorf("groupcast: %w", err)
	}
	r.Descriptor, err = mattercore.NewDescriptor(deviceTypes, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("root descriptor: %w", err)
	}
	r.AdministratorCommissioning = wire.NewAdministratorCommissioningWith(wire.AdministratorCommissioningConfig{BasicCommissioning: cfg.Commissioning.BasicCommissioning})
	if r.TimeSynchronization, err = d.timeSynchronization(cfg, r.GeneralDiagnostics); err != nil { //nolint:contextcheck // the constructor takes no context; it reads the store once
		return nil, err
	}
	r.DiagnosticLogs = mattercore.NewDiagnosticLogs()
	r.DiagnosticLogs.SetClock(cfg.Clock)
	var netComm contract.ClusterServer
	if wc := cfg.Network.wifiCommissioning; wc.Driver != nil {
		r.WiFiNetworkCommissioning, err = mattercore.NewWiFiNetworkCommissioning(mattercore.WiFiNetworkCommissioningConfig{ //nolint:contextcheck // the constructor takes no context; it reads the store once
			Driver: wc.Driver, FailSafe: generalCom.FailSafeFabric,
			// The committed network survives a restart, as chip's driver
			// keeps it (NetworkCommissioningWiFiDriver.cpp:135-167);
			// Node.Start reconnects to it.
			Store: d.st, Logger: d.logger,
			SetBreadcrumb: func(v uint64) {
				_ = generalCom.MatterWrite(context.WithoutCancel(ctx), gencommdef.AttrBreadcrumb, v)
			},
		})
		if err != nil {
			return nil, fmt.Errorf("wi-fi network commissioning: %w", err)
		}
		wifiNC := r.WiFiNetworkCommissioning
		// The network list is committed with the commissioning and reverted
		// with the failsafe (NetworkCommissioningCluster.cpp:984-1014).
		generalCom.SetOnFailSafeExpired(func(ctx context.Context, fi uint8) {
			expired(ctx, fi)
			wifiNC.OnFailSafeExpired()
		})
		generalCom.SetOnCommissioningComplete(func(ctx context.Context, fi uint8) {
			wifiNC.OnCommissioningComplete(ctx)
			commissioned(ctx, fi)
		})
		netComm = wifiNC
	} else {
		r.NetworkCommissioning = mattercore.NewNetworkCommissioning(mattercore.NetworkCommissioningConfig{})
		netComm = r.NetworkCommissioning
	}

	// IcdManagement is mounted only on an ICD ([Config].ICD): RootNode
	// requires it under "Sit | Lit" (root-node.element.ts:91), which an
	// always-on, mains-powered node is not, and matter.js mounts it on no
	// node that is not an ICD (it is in neither ServerNode.RootEndpoint
	// nor any non-ICD test app). A controller reads its presence as "this
	// node sleeps" (NodePhysicalProperties isIntermittentlyConnected).
	if cfg.ICD != nil {
		if r.ICDManagement, err = buildICDManagement(cfg, d.st, d.logger); err != nil {
			return nil, fmt.Errorf("root icd management: %w", err)
		}
	}

	if cfg.TLS != nil {
		if r.TLSCertificateManagement, r.TLSClientManagement, err = buildTLS(ctx, cfg.TLS, d.st); err != nil {
			return nil, fmt.Errorf("root %w", err)
		}
	}

	if r.WiFiNetworkDiagnostics, err = buildWiFiDiagnostics(cfg.Network.WiFi); err != nil {
		return nil, err
	}
	optional, err := buildRootOptional(ctx, d.st, cfg.FixedLabels, cfg.Locales, r.WiFiNetworkDiagnostics)
	if err != nil {
		return nil, fmt.Errorf("root labels and diagnostics: %w", err)
	}
	r.PowerSourceConfiguration, err = mattercore.NewPowerSourceConfiguration(nil)
	if err != nil {
		return nil, fmt.Errorf("root power source configuration: %w", err)
	}
	optional = append(optional, r.PowerSourceConfiguration)

	// The daemon's mount order (wiring.go:281-296 at b4ea1f96^).
	servers := []contract.ClusterServer{
		basicInfo,
		r.AccessControl,
		generalCom,
		r.AdministratorCommissioning,
		netComm,
		r.GeneralDiagnostics,
		r.DiagnosticLogs,
	}
	if r.TimeSynchronization != nil {
		servers = append(servers, r.TimeSynchronization)
	}
	servers = append(servers,
		opCreds,
		r.GroupKeyManagement,
		r.Groupcast,
		r.Descriptor,
	)
	servers = append(servers, r.configured()...)
	servers = append(servers, optional...)
	if servers, err = withRootServers(servers, cfg.RootServers); err != nil {
		return nil, err
	}
	r.servers = servers
	r.Descriptor.SetServerListProvider(clusterIDsOf(r.servers))
	return r, nil
}

// configured are the root servers the configuration adds, in mount
// order: the OTA requestor, ICD Management and the two TLS servers.
func (r *RootClusters) configured() []contract.ClusterServer {
	var out []contract.ClusterServer
	if r.OTARequestor != nil {
		out = append(out, r.OTARequestor)
	}
	if r.ICDManagement != nil {
		out = append(out, r.ICDManagement)
	}
	if r.TLSCertificateManagement != nil {
		out = append(out, r.TLSCertificateManagement, r.TLSClientManagement)
	}
	return out
}

// withAttestation puts the attestation material into opcredsCfg: with
// Attestation.Provider a provider the server calls on the commissioner's
// first ask, never here (OperationalCredentialsServer.ts:560-572), else the
// configured or test material.
func withAttestation(cfg *Config, opcredsCfg *mattercore.OpcredsConfig) error {
	if provider := cfg.Attestation.Provider; provider != nil {
		opcredsCfg.AttestationProvider = func(ctx context.Context) (mattercore.OpcredsAttestation, error) {
			chain, cd, err := provider(ctx)
			if err != nil {
				return mattercore.OpcredsAttestation{}, err
			}
			return attestationMaterialOf(cfg, chain, cd)
		}
		return nil
	}
	m, err := attestationMaterialOf(cfg, cfg.Attestation.Chain, cfg.Attestation.CertificationDeclaration)
	if err != nil {
		return err
	}
	opcredsCfg.DACPrivateKey, opcredsCfg.DAC, opcredsCfg.PAI = m.DACPrivateKey, m.DAC, m.PAI
	opcredsCfg.CertificationDeclaration = m.CertificationDeclaration
	return nil
}

// attestationMaterialOf returns chain and cd, the CSA test material for
// the vendor and product in place of a nil one, as the node's former
// wiring did before b4ea1f96.
func attestationMaterialOf(cfg *Config, chain *attestation.Chain, cd []byte) (mattercore.OpcredsAttestation, error) {
	pd := cfg.ProductDescription
	if chain == nil {
		var err error
		if chain, err = attestation.BuildTestChainAt(pd.VendorID, pd.ProductID, clock.Or(cfg.Clock).Now()); err != nil {
			return mattercore.OpcredsAttestation{}, fmt.Errorf("attestation chain: %w", err)
		}
	}
	if cd == nil {
		var err error
		if cd, err = attestation.BuildTestCertificationDeclaration(pd.VendorID, pd.ProductID); err != nil {
			return mattercore.OpcredsAttestation{}, fmt.Errorf("certification declaration: %w", err)
		}
	}
	return mattercore.OpcredsAttestation{DACPrivateKey: chain.DACKey, DAC: chain.DAC, PAI: chain.PAI, CertificationDeclaration: cd}, nil
}

// buildRootOptional constructs the root's optional label, localization
// and diagnostics clusters as the daemon does (root_optional.go:28-91 at b4ea1f96^):
// FixedLabel, UserLabel (persisted), LocalizationConfiguration (locales,
// nil is en-US alone),
// TimeFormatLocalization (24-hour, Gregorian), UnitLocalization,
// SoftwareDiagnostics (the Go heap), the host's WiFiNetworkDiagnostics when
// it has one, and EthernetNetworkDiagnostics (every value null).
func buildRootOptional(ctx context.Context, st store.Settings, fixed []fixedlabeldef.LabelStruct, locales []string,
	wifi *mattercore.WiFiNetworkDiagnostics,
) ([]contract.ClusterServer, error) {
	fixedLabel, userLabel, err := buildLabels(ctx, st, rootUserLabelsKey, fixed)
	if err != nil {
		return nil, err
	}
	if len(locales) == 0 {
		locales = []string{"en-US"}
	}
	localization, err := mattercore.NewLocalizationConfiguration(mattercore.LocalizationConfigurationConfig{
		ActiveLocale:     locales[0],
		SupportedLocales: slices.Clone(locales),
	})
	if err != nil {
		return nil, err
	}
	timeFormat, err := mattercore.NewTimeFormatLocalization(mattercore.TimeFormatLocalizationConfig{
		HourFormat:         tfldef.HourFormatV24Hr,
		ActiveCalendarType: tfldef.CalendarTypeGregorian,
	})
	if err != nil {
		return nil, err
	}
	units, err := mattercore.NewUnitLocalization(mattercore.UnitLocalizationConfig{})
	if err != nil {
		return nil, err
	}
	software, err := mattercore.NewSoftwareDiagnostics(mattercore.SoftwareDiagnosticsConfig{Heap: runtimeHeap{}})
	if err != nil {
		return nil, err
	}
	ethernet, err := mattercore.NewEthernetNetworkDiagnostics(mattercore.EthernetNetworkDiagnosticsConfig{
		Reporter:   unknownEthernet{},
		Attributes: []uint32{ethdiagdef.AttrPhyRate, ethdiagdef.AttrFullDuplex, ethdiagdef.AttrCarrierDetect},
	})
	if err != nil {
		return nil, err
	}
	servers := []contract.ClusterServer{fixedLabel, userLabel, localization, timeFormat, units, software}
	if wifi != nil {
		servers = append(servers, wifi)
	}
	return append(servers, ethernet), nil
}

// buildLabels constructs one endpoint's FixedLabel and UserLabel, the
// latter restored from and persisted under key (root_optional.go:93-127 at b4ea1f96^).
func buildLabels(ctx context.Context, st store.Settings, key string, fixed []fixedlabeldef.LabelStruct) (fixedLabel, userLabel contract.ClusterServer, err error) {
	fixedLabel, err = mattercore.NewFixedLabel(fixed)
	if err != nil {
		return nil, nil, err
	}
	var labels []userlabeldef.LabelStruct
	if v, ok, gerr := st.GetSetting(ctx, key); gerr == nil && ok {
		if json.Unmarshal([]byte(v), &labels) != nil {
			labels = nil
		}
	}
	userLabel, err = mattercore.NewUserLabel(mattercore.UserLabelConfig{
		Labels: labels,
		OnWrite: func(ctx context.Context, l []userlabeldef.LabelStruct) error {
			b, err := json.Marshal(l)
			if err != nil {
				return fmt.Errorf("user labels: %w", err)
			}
			return st.SetSetting(context.WithoutCancel(ctx), key, string(b))
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return fixedLabel, userLabel, nil
}

// buildAggregatorClusters constructs endpoint 1, the Aggregator: Identify,
// Descriptor, FixedLabel ("role" "aggregator"), UserLabel and the host's
// extra servers (wiring.go:332-345 at b4ea1f96^). Identify counts down on clk, the
// node's clock.
func buildAggregatorClusters(ctx context.Context, st store.Settings, extra []contract.ClusterServer, clk clock.Clock) ([]contract.ClusterServer, error) {
	descriptor, err := mattercore.NewDescriptor(
		[]mattercore.DeviceTypeStruct{{DeviceType: deviceTypeAggregator, Revision: deviceTypeRevision(schema.DeviceTypeID(deviceTypeAggregator))}},
		nil, nil, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("aggregator descriptor: %w", err)
	}
	fixedLabel, userLabel, err := buildLabels(ctx, st, aggregatorUserLabelsKey, []fixedlabeldef.LabelStruct{
		{Label: "role", Value: "aggregator"},
	})
	if err != nil {
		return nil, fmt.Errorf("aggregator labels: %w", err)
	}
	identify := mattercore.NewIdentify()
	identify.SetClock(clk)
	servers := append([]contract.ClusterServer{identify, descriptor, fixedLabel, userLabel}, extra...)
	descriptor.SetServerListProvider(clusterIDsOf(servers))
	return servers, nil
}

// withRootServers appends the host's own root servers ([Config].RootServers);
// a cluster the root serves already is refused.
func withRootServers(servers, extra []contract.ClusterServer) ([]contract.ClusterServer, error) {
	for _, srv := range extra {
		id := srv.MatterClusterID()
		if slices.ContainsFunc(servers, func(s contract.ClusterServer) bool { return s.MatterClusterID() == id }) {
			return nil, fmt.Errorf("node: root: Config.RootServers names cluster 0x%04x, which the root serves already", id)
		}
		servers = append(servers, srv)
	}
	return servers, nil
}

// clusterIDsOf returns a provider over the cluster ids of the mounted
// set, deduplicated and in mount order (wiring.go:348-363 at b4ea1f96^).
func clusterIDsOf(servers []contract.ClusterServer) func() []uint32 {
	return func() []uint32 {
		ids := make([]uint32, 0, len(servers))
		seen := make(map[uint32]struct{}, len(servers))
		for _, srv := range servers {
			id := srv.MatterClusterID()
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		return ids
	}
}

// attachPartsListProviders installs the two live PartsList providers,
// both full-family (wiring.go:301-327 at b4ea1f96^; matter.js DescriptorServer.ts
// #updatePartsList for the full-family RootNode and Aggregator).
func attachPartsListProviders(br *bridge.Bridge) error {
	if !br.AttachRootPartsListProvider(func() []uint16 {
		return endpointIDs(br, func(ep *endpoint.Endpoint) bool { return !ep.IsRoot() })
	}) {
		return errors.New("node: root Descriptor missing: PartsList provider could not be attached")
	}
	if !br.AttachAggregatorPartsListProvider(func() []uint16 {
		// The Aggregator's siblings — the OTA provider's endpoint — are
		// the root's parts, not its.
		return endpointIDs(br, func(ep *endpoint.Endpoint) bool { return !ep.IsRoot() && !ep.IsAggregator() && !isRootChild(ep) })
	}) {
		return errors.New("node: aggregator Descriptor missing: PartsList provider could not be attached")
	}
	return nil
}

// endpointIDs returns the ids of the live topology's endpoints that pass
// keep, ascending; nil before the first assembly.
func endpointIDs(br *bridge.Bridge, keep func(*endpoint.Endpoint) bool) []uint16 {
	topology := br.Topology()
	if topology == nil {
		return nil
	}
	ids := make([]uint16, 0, len(topology.Endpoints))
	for _, ep := range topology.Endpoints {
		if ep != nil && keep(ep) {
			ids = append(ids, ep.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// runtimeHeap reports the Go heap (root_optional.go:129-144 at b4ea1f96^).
type runtimeHeap struct{}

func (runtimeHeap) read() runtime.MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}

// CurrentHeapFree implements mattercore.HeapReporter.
func (h runtimeHeap) CurrentHeapFree() (uint64, bool) {
	m := h.read()
	return m.HeapSys - m.HeapReleased - m.HeapAlloc, true
}

// CurrentHeapUsed implements mattercore.HeapReporter.
func (h runtimeHeap) CurrentHeapUsed() (uint64, bool) { return h.read().HeapAlloc, true }

// unknownEthernet is an Ethernet reporter that cannot tell anything: the
// node binds every interface and knows no single link.
type unknownEthernet struct{}

func (unknownEthernet) PHYRate() (ethdiagdef.PHYRateEnum, bool) { return 0, false }
func (unknownEthernet) FullDuplex() (duplex, ok bool)           { return false, false }
func (unknownEthernet) CarrierDetect() (carrier, ok bool)       { return false, false }
func (unknownEthernet) TimeSinceReset() (uint64, bool)          { return 0, false }

// timeSynchronization builds the root TimeSynchronization: without
// features, or with [Config].TimeSynchronization.Config's on the node's store;
// nil with TimeSynchronization.Off.
// GeneralDiagnostics takes its UTC clock.
func (d rootDeps) timeSynchronization(cfg *Config, gd *mattercore.GeneralDiagnostics) (*mattercore.TimeSynchronization, error) {
	if cfg.TimeSynchronization.Off {
		return nil, nil
	}
	ts := mattercore.NewTimeSynchronization()
	if tc := cfg.TimeSynchronization.Config; tc != nil {
		c := *tc
		c.Store, c.Logger = d.st, cmp.Or(c.Logger, d.logger)
		var err error
		if ts, err = mattercore.NewTimeSynchronizationWith(c); err != nil { //nolint:contextcheck // the constructor takes no context; it reads the store once
			return nil, fmt.Errorf("time synchronization: %w", err)
		}
	}
	ts.SetClock(cfg.Clock)
	gd.SetUTCClock(ts.UTC)
	return ts, nil
}

// buildWiFiDiagnostics is the root's WiFiNetworkDiagnostics server of
// [Network].WiFi; nil without one.
func buildWiFiDiagnostics(wifi *WiFi) (*mattercore.WiFiNetworkDiagnostics, error) {
	if wifi == nil || wifi.Reporter == nil {
		return nil, nil //nolint:nilnil // no Wi-Fi interface, no server
	}
	srv, err := mattercore.NewWiFiNetworkDiagnostics(mattercore.WiFiNetworkDiagnosticsConfig{
		Reporter: wifi.Reporter, Features: wifi.Features, Attributes: wifi.Attributes,
	})
	if err != nil {
		return nil, fmt.Errorf("root wifi diagnostics: %w", err)
	}
	return srv, nil
}

// newGeneralCommissioning builds the root's GeneralCommissioning server
// and restores the committed terms-and-conditions acceptance from the
// node's store ([Commissioning].TermsAndConditions).
func newGeneralCommissioning(ctx context.Context, d rootDeps) (*mattercore.GeneralCommissioning, error) {
	cfg := d.cfg
	gc, err := mattercore.NewGeneralCommissioning(mattercore.GeneralCommissioningConfig{
		Logger:                       d.logger,
		LocationCapability:           cfg.Commissioning.LocationCapability,
		SupportsConcurrentConnection: true,
		TermsAndConditions:           cfg.Commissioning.TermsAndConditions.core(),
		Settings:                     d.st,
		Clock:                        cfg.Clock,
	})
	if err != nil {
		return nil, fmt.Errorf("general commissioning: %w", err)
	}
	if err := gc.RestoreTermsAcceptance(ctx); err != nil {
		// A malformed stored acceptance reads as none: the commissioner
		// acknowledges the terms again (TCAcknowledgementsRequired true);
		// the node starts.
		d.logger.Warn("node.terms.restore", slog.Any("err", err))
	}
	return gc, nil
}
