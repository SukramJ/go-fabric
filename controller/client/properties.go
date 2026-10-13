// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/tlv"
)

// Properties are a node's physical properties — matter.js
// PhysicalDeviceProperties (packages/protocol/src/peer/PhysicalDeviceProperties.ts:32-61
// at 0ceb202d) as NodePhysicalProperties computes them from the image
// (packages/node/src/node/NodePhysicalProperties.ts:30-194). A pointer
// field is nil while unknown.
type Properties struct {
	SupportsThread, SupportsWifi, SupportsEthernet bool
	// RootEndpointServerList is the root endpoint's ServerList.
	RootEndpointServerList []uint32
	MainsPowered           bool
	BatteryPowered         bool
	// IntermittentlyConnected: the root serves IcdManagement.
	IntermittentlyConnected bool
	// LongIdleTimeOperating: an ICD in Long Idle Time mode.
	LongIdleTimeOperating bool
	// IdleModeDuration is IcdManagement's IdleModeDuration.
	IdleModeDuration      *time.Duration
	ThreadSleepyEndDevice bool
	// SpecificationVersion is BasicInformation's SpecificationVersion.
	SpecificationVersion *uint32
	// DeviceTypes are the device types of every endpoint, in id order.
	DeviceTypes                              []uint32
	ThreadActive, WifiActive, EthernetActive *bool
	ThreadPan                                *uint64
	ThreadChannel                            *uint16
}

// Cluster and attribute ids the properties read. ThreadNetworkDiagnostics
// and WiFiNetworkDiagnostics have no generated definition; their ids are
// matter.js's (model/src/standard/elements/thread-network-diagnostics.element.ts:20,
// :31-38, :288 and wi-fi-network-diagnostics.element.ts:20, :28 at
// 0ceb202d), the clusters "ThreadNetworkDiagnostics" (0x0035) and
// "WiFiNetworkDiagnostics" (0x0036) of parity/schema.json.
const (
	threadDiagnostics            uint32 = 0x0035
	threadAttrChannel            uint32 = 0x0000
	threadAttrRoutingRole        uint32 = 0x0001
	threadAttrNetworkName        uint32 = 0x0002
	threadAttrPanID              uint32 = 0x0003
	threadAttrExtendedPanID      uint32 = 0x0004
	threadRoutingSleepyEndDevice        = 2
	wifiDiagnostics              uint32 = 0x0036
	wifiAttrBssid                uint32 = 0x0000

	aggregatorDeviceType    uint32 = 0x000E
	genericSwitchDeviceType uint32 = 0x000F
)

// computeProperties is NodePhysicalProperties (NodePhysicalProperties.ts:30-59).
// The caller holds the node's lock.
func (s *structure) computeProperties() Properties {
	root := s.root
	var p Properties
	if d := readyCluster(root, descriptor.ClusterID); d != nil {
		if v, ok := d.cache.values[descriptor.AttrServerList]; ok {
			p.RootEndpointServerList = idListInOrder(v)
		}
	}
	icd := readyCluster(root, icdmanagement.ClusterID)
	p.IntermittentlyConnected = slices.Contains(p.RootEndpointServerList, icdmanagement.ClusterID)
	if icd != nil {
		// The owner reads IcdClient.litSupported as the LITS feature bit
		// (NodePhysicalProperties.ts:33-35, :45).
		lits := icd.injected.featureMap&uint32(icdmanagement.FeatureLongIdleTimeSupport) != 0
		if mode, ok := uintOf(icd, icdmanagement.AttrOperatingMode); ok {
			p.LongIdleTimeOperating = lits && mode == uint64(icdmanagement.OperatingModeLit)
		}
		if idle, ok := uintOf(icd, icdmanagement.AttrIdleModeDuration); ok {
			d := time.Duration(idle) * time.Second //nolint:gosec // a uint32 attribute
			p.IdleModeDuration = &d
		}
	}
	if bi := readyCluster(root, basicinformation.ClusterID); bi != nil {
		if v, ok := uintOf(bi, basicinformation.AttrSpecificationVersion); ok && v <= math.MaxUint32 {
			sv := uint32(v)
			p.SpecificationVersion = &sv
		}
	}
	types := map[uint32]bool{}
	s.inspectEndpoint(root, &p, types, false)
	p.DeviceTypes = slices.Sorted(maps.Keys(types))
	if !p.SupportsWifi && !p.SupportsThread && !p.SupportsEthernet {
		inferNetworkMediumFromDiagnostics(root, &p)
	}
	return p
}

// inspectEndpoint is NodePhysicalProperties.ts:108-194: the device types of
// every endpoint; the network, power and Thread properties of the endpoints
// not behind an aggregator.
func (s *structure) inspectEndpoint(ep *Endpoint, p *Properties, types map[uint32]bool, behindAggregator bool) {
	if d := readyCluster(ep, descriptor.ClusterID); d != nil {
		if v, ok := d.cache.values[descriptor.AttrDeviceTypeList]; ok && isList(v) {
			for _, dt := range deviceTypeEntries(v) {
				types[dt.DeviceType] = true
			}
		}
	}
	if !behindAggregator {
		inspectNetwork(ep, p)
		inspectPower(ep, p)
		inspectThread(ep, p)
	}
	for _, part := range ep.partsLocked() {
		s.inspectEndpoint(part, p, types, behindAggregator || (part.number != 0 && part.deviceType == aggregatorDeviceType))
	}
}

// inspectNetwork: NetworkCommissioning's features (:116-128) and the
// operational interfaces of GeneralDiagnostics (:130-141).
func inspectNetwork(ep *Endpoint, p *Properties) {
	if nc := readyCluster(ep, networkcommissioning.ClusterID); nc != nil {
		f := nc.injected.featureMap
		p.SupportsWifi = p.SupportsWifi || f&uint32(networkcommissioning.FeatureWiFiNetworkInterface) != 0
		p.SupportsThread = p.SupportsThread || f&uint32(networkcommissioning.FeatureThreadNetworkInterface) != 0
		p.SupportsEthernet = p.SupportsEthernet || f&uint32(networkcommissioning.FeatureEthernetNetworkInterface) != 0
	}
	gd := readyCluster(ep, generaldiagnostics.ClusterID)
	if gd == nil {
		return
	}
	v, ok := gd.cache.values[generaldiagnostics.AttrNetworkInterfaces]
	if !ok {
		return
	}
	decoded, err := generaldiagnostics.Definition.Attribute(generaldiagnostics.AttrNetworkInterfaces).Decode(v)
	if err != nil {
		return
	}
	list, _ := decoded.(spec.List[generaldiagnostics.NetworkInterface])
	for _, ni := range list {
		switch ni.Type {
		case generaldiagnostics.InterfaceTypeWiFi:
			p.WifiActive = orBool(p.WifiActive, ni.IsOperational)
		case generaldiagnostics.InterfaceTypeEthernet:
			p.EthernetActive = orBool(p.EthernetActive, ni.IsOperational)
		case generaldiagnostics.InterfaceTypeUnspecified, generaldiagnostics.InterfaceTypeCellular, generaldiagnostics.InterfaceTypeThread:
		}
	}
}

// orBool is (*b ?? false) || v.
func orBool(b *bool, v bool) *bool {
	r := v || (b != nil && *b)
	return &r
}

// inspectPower: PowerSource — wired and Active is mains, otherwise Active
// or Unspecified is battery (:143-168; the else branch's condition holds
// whenever the source is not wired).
func inspectPower(ep *Endpoint, p *Properties) {
	ps := readyCluster(ep, powersource.ClusterID)
	if ps == nil {
		return
	}
	status, ok := uintOf(ps, powersource.AttrStatus)
	if ps.injected.featureMap&uint32(powersource.FeatureWired) != 0 {
		if ok && status == uint64(powersource.PowerSourceStatusActive) {
			p.MainsPowered = true
		}
		return
	}
	if ok && (status == uint64(powersource.PowerSourceStatusActive) || status == uint64(powersource.PowerSourceStatusUnspecified)) {
		p.BatteryPowered = true
	}
}

// inspectThread: ThreadNetworkDiagnostics — a sleepy end device
// (:174-176), an active network and its PAN and channel (:177-183).
func inspectThread(ep *Endpoint, p *Properties) {
	tnd := readyCluster(ep, threadDiagnostics)
	if tnd == nil {
		return
	}
	if role, ok := uintOf(tnd, threadAttrRoutingRole); ok && role == threadRoutingSleepyEndDevice {
		p.ThreadSleepyEndDevice = true
	}
	active := false
	if pan, ok := uintOf(tnd, threadAttrExtendedPanID); ok {
		active = true
		p.ThreadPan = &pan
		p.ThreadChannel = nil
		if ch, ok := uintOf(tnd, threadAttrChannel); ok && ch <= math.MaxUint16 {
			c := uint16(ch)
			p.ThreadChannel = &c
		}
	}
	p.ThreadActive = &active
}

// inferNetworkMediumFromDiagnostics is NodePhysicalProperties.ts:70-99: the
// root's WiFi diagnostics with a BSSID, or its Thread diagnostics with any
// network detail, name the medium; Thread as the only medium is active.
func inferNetworkMediumFromDiagnostics(root *Endpoint, p *Properties) {
	if w := readyCluster(root, wifiDiagnostics); w != nil && present(w, wifiAttrBssid) {
		p.SupportsWifi = true
	}
	tnd := readyCluster(root, threadDiagnostics)
	if tnd == nil {
		return
	}
	if !present(tnd, threadAttrExtendedPanID) && !present(tnd, threadAttrPanID) && !present(tnd, threadAttrChannel) && !present(tnd, threadAttrNetworkName) {
		return
	}
	p.SupportsThread = true
	if !p.SupportsWifi && p.WifiActive == nil && p.EthernetActive == nil {
		active := true
		p.ThreadActive = &active
		if p.ThreadChannel == nil {
			if ch, ok := uintOf(tnd, threadAttrChannel); ok && ch <= math.MaxUint16 {
				c := uint16(ch)
				p.ThreadChannel = &c
			}
		}
	}
}

// readyCluster is the endpoint's cluster when it has a shape — the
// behaviour maybeStateOf finds.
func readyCluster(ep *Endpoint, id uint32) *Cluster {
	if c := ep.clusters[id]; c != nil && c.injected != nil {
		return c
	}
	return nil
}

// present: the value is there and not null (:101-103).
func present(c *Cluster, attr uint32) bool {
	v, ok := c.cache.values[attr]
	return ok && v.El.Type != tlv.TypeNull
}

// uintOf is an unsigned value of the cluster; null and other types are
// absent.
func uintOf(c *Cluster, attr uint32) (uint64, bool) {
	v, ok := c.cache.values[attr]
	if !ok || !isUint(v) {
		return 0, false
	}
	return v.El.Uint, true
}

// idListInOrder is a list's numeric entries in list order.
func idListInOrder(n spec.Node) []uint32 {
	var out []uint32
	for _, e := range n.Children {
		if isUint(e) && e.El.Uint <= math.MaxUint32 {
			out = append(out, uint32(e.El.Uint))
		}
	}
	return out
}

// Subscription interval constants — PhysicalDeviceProperties.ts:13-30.
const (
	floorDefault        = 0               // DEFAULT_SUBSCRIPTION_FLOOR_DEFAULT = Instant
	floorThreadLegacy   = time.Second     // DEFAULT_SUBSCRIPTION_FLOOR_THREAD_LEGACY
	floorICD            = 0               // DEFAULT_SUBSCRIPTION_FLOOR_ICD = Instant
	specVersion13       = 0x0103_0000     // SPEC_VERSION_1_3
	ceilingWifi         = time.Minute     // DEFAULT_SUBSCRIPTION_CEILING_WIFI
	ceilingThread       = time.Minute     // DEFAULT_SUBSCRIPTION_CEILING_THREAD
	ceilingThreadSleepy = 3 * time.Minute // DEFAULT_SUBSCRIPTION_CEILING_THREAD_SLEEPY
	ceilingBattery      = 10 * time.Minute
	ceilingJitter       = 0.1              // SUBSCRIPTION_CEILING_JITTER
	ceilingJitterFloor  = 10 * time.Second // SUBSCRIPTION_CEILING_JITTER_MIN
	maxCeiling          = 65535 * time.Second
)

// intervalBounds is PhysicalDeviceProperties.subscriptionIntervalBoundsFor
// (PhysicalDeviceProperties.ts:64-164): an ICD's floor is 0, announced only
// when the caller gave one (:94-102); the default floor is 1 s for a
// Thread node before Matter 1.3 without a Generic Switch, else 0
// (:103-115); the default ceiling by the node's kind (:117-126); an ICD's
// or the caller's ceiling is kept, at least the floor (:137-144); a
// default one gets up to max(10 %, 10 s) of jitter, whole seconds
// (:146-157); at most 65535 s (:158).
func intervalBounds(p Properties, reqFloor, reqCeiling *time.Duration, rand func() float64, log *slog.Logger) (floor, ceiling time.Duration) {
	hasFloor := reqFloor != nil
	if hasFloor {
		floor = *reqFloor
	}
	if p.IntermittentlyConnected && (!hasFloor || floor != floorICD) {
		if hasFloor {
			log.Info("matter.controller.client.icd_floor", slog.Duration("floor", floorICD))
		}
		floor, hasFloor = floorICD, true
	}
	if !hasFloor {
		onThread := (p.ThreadActive != nil && *p.ThreadActive) || (p.ThreadActive == nil && (p.SupportsThread || p.ThreadSleepyEndDevice))
		preMatter13 := p.SpecificationVersion == nil || *p.SpecificationVersion < specVersion13
		floor = floorDefault
		if onThread && preMatter13 && !slices.Contains(p.DeviceTypes, genericSwitchDeviceType) {
			floor = floorThreadLegacy
		}
	}
	isICDCeiling := p.IntermittentlyConnected && p.IdleModeDuration != nil
	var def time.Duration
	switch {
	case isICDCeiling:
		def = min(*p.IdleModeDuration, maxCeiling)
	case p.BatteryPowered && !p.MainsPowered:
		def = ceilingBattery
	case p.ThreadSleepyEndDevice:
		def = ceilingThreadSleepy
	case p.SupportsThread:
		def = ceilingThread
	default:
		def = ceilingWifi
	}
	explicit := reqCeiling != nil
	ceiling = def
	if explicit {
		ceiling = *reqCeiling
	}
	if !isICDCeiling && ceiling < def {
		log.Debug("matter.controller.client.ceiling_below_default", slog.Duration("ideal", def), slog.Duration("ceiling", ceiling))
	}
	if isICDCeiling || explicit {
		ceiling = max(floor, ceiling)
	} else {
		maxJitterMs := max(float64(ceiling.Milliseconds())*ceilingJitter, float64(ceilingJitterFloor.Milliseconds()))
		jitter := time.Duration(math.Round(maxJitterMs*rand())) * time.Millisecond
		ceiling = max(floor, (ceiling + jitter).Truncate(time.Second))
	}
	return floor, min(ceiling, maxCeiling)
}
