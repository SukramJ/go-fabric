// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	ethdiagdef "github.com/SukramJ/go-fabric/cluster/spec/ethernetnetworkdiagnostics"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
	tfldef "github.com/SukramJ/go-fabric/cluster/spec/timeformatlocalization"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/groups"
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
// daemon's (root_optional.go:20-26), so a database the daemon wrote keeps
// its labels under the facade.
const (
	rootUserLabelsKey       = "user_label.root"
	aggregatorUserLabelsKey = "user_label.aggregator"
)

// deviceTypeRevision reads a device type's revision from the generated
// matter.js snapshot (schema.DeviceTypeRevision), so a regeneration
// propagates without an edit.
func deviceTypeRevision(id uint32) uint16 {
	rev, _ := schema.DeviceTypeRevision(id) // both system types are in every snapshot
	return rev
}

// RootClusters is endpoint 0's server set: matter.js's root endpoint
// composition (ServerNode.ts:264-298) as the reference daemon builds it
// (examples/reference-bridge/wiring.go:93-299, buildRootClusters, and
// root_optional.go). The fields are the servers a host may need a hook
// on; [RootClusters.Servers] is the mounted set in mount order.
type RootClusters struct {
	BasicInformation           *mattercore.BasicInformation
	AccessControl              *mattercore.AccessControl
	GeneralCommissioning       *mattercore.GeneralCommissioning
	AdministratorCommissioning *wire.AdministratorCommissioning
	NetworkCommissioning       *mattercore.NetworkCommissioning
	GeneralDiagnostics         *mattercore.GeneralDiagnostics
	DiagnosticLogs             *mattercore.DiagnosticLogs
	TimeSynchronization        *mattercore.TimeSynchronization
	OperationalCredentials     *mattercore.OperationalCredentials
	GroupKeyManagement         *mattercore.GroupKeyManagement
	Groupcast                  *mattercore.Groupcast
	Descriptor                 *mattercore.Descriptor
	// OTARequestor is nil unless [Config].OTA is set.
	OTARequestor *mattercore.OTASoftwareUpdateRequestor
	// PowerSourceConfiguration lists the node's PowerSource endpoints;
	// it starts empty, for the host to fill once the topology exists.
	PowerSourceConfiguration *mattercore.PowerSourceConfiguration

	servers []contract.ClusterServer
}

// Servers returns the mounted set, in mount order.
func (r *RootClusters) Servers() []contract.ClusterServer { return slices.Clone(r.servers) }

// rootDeps is what buildRoot needs from the node under construction.
type rootDeps struct {
	cfg               *Config
	bootReason        uint8
	st                *store.Store
	groups            *groups.Manager
	connector         mattercore.OtaProviderConnector
	onFabricInstalled func(ctx context.Context, fabricIndex uint8, fabricID, nodeID uint64, rootPublicKey []byte)
}

// buildRoot constructs endpoint 0's cluster surface and cross-wires the
// fail-safe hooks, as the daemon's buildRootClusters does
// (wiring.go:93-299).
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
	})
	if err != nil {
		return nil, fmt.Errorf("basic information: %w", err)
	}
	r.BasicInformation = basicInfo

	// RebootCount and the operational time carry over from the previous
	// boots (wiring.go:118-128).
	r.GeneralDiagnostics = mattercore.NewGeneralDiagnostics(d.bootReason)
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

	r.GeneralCommissioning, err = mattercore.NewGeneralCommissioning(mattercore.GeneralCommissioningConfig{
		LocationCapability:           mattercore.RegulatoryIndoor,
		SupportsConcurrentConnection: true,
	})
	if err != nil {
		return nil, fmt.Errorf("general commissioning: %w", err)
	}
	generalCom := r.GeneralCommissioning

	chain, cd, err := attestationMaterial(cfg)
	if err != nil {
		return nil, err
	}
	r.OperationalCredentials, err = mattercore.NewOperationalCredentials(d.st, mattercore.OpcredsConfig{
		SupportedFabrics:         5,
		DACPrivateKey:            chain.DACKey,
		DAC:                      chain.DAC,
		PAI:                      chain.PAI,
		CertificationDeclaration: cd,
		// The CASE identity is rebuilt from the freshly written fabric
		// (wiring.go:166-170).
		OnFabricInstalled: d.onFabricInstalled,
		IsFailSafeArmed:   generalCom.FailSafeArmed,
		// A successful AddNOC re-arms the fail-safe onto its fabric, Matter
		// §11.18.6.16 (wiring.go:172-178).
		RearmFailSafeForFabric: generalCom.SetCurrentFabric,
	})
	if err != nil {
		return nil, fmt.Errorf("operational credentials: %w", err)
	}
	opCreds := r.OperationalCredentials

	// The fail-safe hooks between the two clusters (wiring.go:184-200).
	generalCom.SetOnFailSafeArmed(func(context.Context, uint8) { opCreds.ClearPendingState() })
	generalCom.SetOnFailSafeExpired(opCreds.OnFailSafeExpiry)

	deviceTypes := []mattercore.DeviceTypeStruct{{DeviceType: deviceTypeRootNode, Revision: deviceTypeRevision(deviceTypeRootNode)}}
	if cfg.OTA != nil {
		r.OTARequestor, err = buildOtaRequestor(cfg, d.connector, basicInfo)
		if err != nil {
			return nil, fmt.Errorf("ota requestor: %w", err)
		}
		deviceTypes = append(deviceTypes, mattercore.OtaRequestorDeviceType())
	}
	otaRequestor := r.OTARequestor
	generalCom.SetOnCommissioningComplete(func(context.Context, uint8) {
		opCreds.ClearPendingState()
		if otaRequestor != nil {
			// A query 30 s after a commissioning (wiring.go:210-215).
			otaRequestor.Commissioned()
		}
	})

	r.AccessControl, err = mattercore.NewAccessControl(d.st) //nolint:contextcheck // the constructor takes no context; it reads the store once
	if err != nil {
		return nil, fmt.Errorf("access control: %w", err)
	}
	r.GroupKeyManagement, err = mattercore.NewGroupKeyManagement(d.st, mattercore.GroupKeyMgmtConfig{Groups: d.groups})
	if err != nil {
		return nil, fmt.Errorf("group key management: %w", err)
	}
	r.Groupcast, err = mattercore.NewGroupcast(mattercore.GroupcastConfig{ //nolint:contextcheck // the constructor takes no context
		Groups: d.groups, GroupKeyManagement: r.GroupKeyManagement, AccessControl: r.AccessControl,
	})
	if err != nil {
		return nil, fmt.Errorf("groupcast: %w", err)
	}
	r.Descriptor, err = mattercore.NewDescriptor(deviceTypes, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("root descriptor: %w", err)
	}
	r.AdministratorCommissioning = wire.NewAdministratorCommissioning()
	r.TimeSynchronization = mattercore.NewTimeSynchronization()
	r.GeneralDiagnostics.SetUTCClock(r.TimeSynchronization.UTC)
	r.DiagnosticLogs = mattercore.NewDiagnosticLogs()
	r.NetworkCommissioning = mattercore.NewNetworkCommissioning(mattercore.NetworkCommissioningConfig{})

	optional, err := buildRootOptional(ctx, d.st, cfg.FixedLabels, cfg.Locales)
	if err != nil {
		return nil, fmt.Errorf("root labels and diagnostics: %w", err)
	}
	r.PowerSourceConfiguration, err = mattercore.NewPowerSourceConfiguration(nil)
	if err != nil {
		return nil, fmt.Errorf("root power source configuration: %w", err)
	}
	optional = append(optional, r.PowerSourceConfiguration)

	// The daemon's mount order (wiring.go:281-296).
	servers := []contract.ClusterServer{
		basicInfo,
		r.AccessControl,
		generalCom,
		r.AdministratorCommissioning,
		r.NetworkCommissioning,
		r.GeneralDiagnostics,
		r.DiagnosticLogs,
		r.TimeSynchronization,
		opCreds,
		r.GroupKeyManagement,
		r.Groupcast,
		r.Descriptor,
	}
	if r.OTARequestor != nil {
		servers = append(servers, r.OTARequestor)
	}
	servers = append(servers, optional...)
	r.servers = servers
	r.Descriptor.SetServerListProvider(clusterIDsOf(r.servers))
	return r, nil
}

// attestationMaterial returns the configured chain and CD, or the CSA
// test material for the vendor and product (wiring.go:132-163).
func attestationMaterial(cfg *Config) (*attestation.Chain, []byte, error) {
	pd := cfg.ProductDescription
	chain := cfg.Attestation.Chain
	if chain == nil {
		var err error
		if chain, err = attestation.BuildTestChain(pd.VendorID, pd.ProductID); err != nil {
			return nil, nil, fmt.Errorf("attestation chain: %w", err)
		}
	}
	cd := cfg.Attestation.CertificationDeclaration
	if cd == nil {
		var err error
		if cd, err = attestation.BuildTestCertificationDeclaration(pd.VendorID, pd.ProductID); err != nil {
			return nil, nil, fmt.Errorf("certification declaration: %w", err)
		}
	}
	return chain, cd, nil
}

// buildRootOptional constructs the root's optional label, localization
// and diagnostics clusters as the daemon does (root_optional.go:28-91):
// FixedLabel, UserLabel (persisted), LocalizationConfiguration (locales,
// nil is en-US alone),
// TimeFormatLocalization (24-hour, Gregorian), UnitLocalization,
// SoftwareDiagnostics (the Go heap) and EthernetNetworkDiagnostics (every
// value null).
func buildRootOptional(ctx context.Context, st *store.Store, fixed []fixedlabeldef.LabelStruct, locales []string) ([]contract.ClusterServer, error) {
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
	return []contract.ClusterServer{fixedLabel, userLabel, localization, timeFormat, units, software, ethernet}, nil
}

// buildLabels constructs one endpoint's FixedLabel and UserLabel, the
// latter restored from and persisted under key (root_optional.go:93-127).
func buildLabels(ctx context.Context, st *store.Store, key string, fixed []fixedlabeldef.LabelStruct) (fixedLabel, userLabel contract.ClusterServer, err error) {
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
// extra servers (wiring.go:332-345).
func buildAggregatorClusters(ctx context.Context, st *store.Store, extra []contract.ClusterServer) ([]contract.ClusterServer, error) {
	descriptor, err := mattercore.NewDescriptor(
		[]mattercore.DeviceTypeStruct{{DeviceType: deviceTypeAggregator, Revision: deviceTypeRevision(deviceTypeAggregator)}},
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
	servers := append([]contract.ClusterServer{mattercore.NewIdentify(), descriptor, fixedLabel, userLabel}, extra...)
	descriptor.SetServerListProvider(clusterIDsOf(servers))
	return servers, nil
}

// clusterIDsOf returns a provider over the cluster ids of the mounted
// set, deduplicated and in mount order (wiring.go:348-363).
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
// both full-family (wiring.go:301-327; matter.js DescriptorServer.ts
// #updatePartsList for the full-family RootNode and Aggregator).
func attachPartsListProviders(br *bridge.Bridge) error {
	if !br.AttachRootPartsListProvider(func() []uint16 {
		return endpointIDs(br, func(ep *endpoint.Endpoint) bool { return !ep.IsRoot() })
	}) {
		return errors.New("node: root Descriptor missing: PartsList provider could not be attached")
	}
	if !br.AttachAggregatorPartsListProvider(func() []uint16 {
		return endpointIDs(br, func(ep *endpoint.Endpoint) bool { return !ep.IsRoot() && !ep.IsAggregator() })
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

// runtimeHeap reports the Go heap (root_optional.go:129-144).
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
