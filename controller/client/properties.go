// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"
	"maps"
	"math"
	"slices"
	"time"

	"thingfabric.dev/go-fabric/client/peer"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	"thingfabric.dev/go-fabric/cluster/spec/descriptor"
	"thingfabric.dev/go-fabric/cluster/spec/generaldiagnostics"
	"thingfabric.dev/go-fabric/cluster/spec/icdmanagement"
	"thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
	"thingfabric.dev/go-fabric/cluster/spec/powersource"
	"thingfabric.dev/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"thingfabric.dev/go-fabric/cluster/spec/wifinetworkdiagnostics"
	"thingfabric.dev/go-fabric/tlv"
)

// Properties are a node's physical properties — matter.js
// PhysicalDeviceProperties (packages/protocol/src/peer/PhysicalDeviceProperties.ts:32-61
// at db021da0) as NodePhysicalProperties computes them from the image
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

// Medium is the part of the properties a peer's network profile is
// selected by (client/peer.NetworkProfiles.ForPeer, NetworkProfile.ts:184-221
// at db91886c).
func (p Properties) Medium() peer.Medium {
	m := peer.Medium{
		SupportsThread: p.SupportsThread, SupportsWifi: p.SupportsWifi, SupportsEthernet: p.SupportsEthernet,
		RootEndpointServers: len(p.RootEndpointServerList),
		ThreadActive:        p.ThreadActive, WifiActive: p.WifiActive, EthernetActive: p.EthernetActive,
	}
	if p.ThreadChannel != nil {
		m.ThreadChannel = *p.ThreadChannel
	}
	return m
}

// Cluster and attribute ids the properties read: the generated
// definitions' (ADR 0013), so a regeneration carries them (review II,
// R2-P6-par-2360).
const (
	threadDiagnostics            = threadnetworkdiagnostics.ClusterID
	threadAttrChannel            = threadnetworkdiagnostics.AttrChannel
	threadAttrRoutingRole        = threadnetworkdiagnostics.AttrRoutingRole
	threadAttrNetworkName        = threadnetworkdiagnostics.AttrNetworkName
	threadAttrPanID              = threadnetworkdiagnostics.AttrPanId
	threadAttrExtendedPanID      = threadnetworkdiagnostics.AttrExtendedPanId
	threadRoutingSleepyEndDevice = uint64(threadnetworkdiagnostics.RoutingRoleSleepyEndDevice)
	wifiDiagnostics              = wifinetworkdiagnostics.ClusterID
	wifiAttrBssid                = wifinetworkdiagnostics.AttrBssid

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
		// isLongIdleTimeOperating is IcdClient.litSupported — the LITS
		// feature and specification 1.4.0 or later — and OperatingMode LIT
		// (NodePhysicalProperties.ts:33-35, :45; IcdClient.ts:967-974).
		lits := icd.injected.featureMap&uint32(icdmanagement.FeatureLongIdleTimeSupport) != 0
		if bi := readyCluster(root, basicinformation.ClusterID); lits && bi != nil {
			v, ok := uintOf(bi, basicinformation.AttrSpecificationVersion)
			lits = ok && v >= uint64(MinLITSpecificationVersion)
		} else {
			lits = false
		}
		if mode, ok := uintOf(icd, icdmanagement.AttrOperatingMode); ok {
			p.LongIdleTimeOperating = lits && mode == uint64(icdmanagement.OperatingModeLit)
		}
		if idle, ok := uintWithin(icd, icdmanagement.AttrIdleModeDuration, math.MaxUint32); ok {
			d := time.Duration(idle) * time.Second //nolint:gosec // uintWithin bounds it to the attribute's uint32
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

// uintWithin is uintOf for an attribute of an unsigned type no wider than
// limit: a value beyond its type is absent. The ICD timings are multiplied
// into a time.Duration, and a peer's value wider than the attribute's type
// would overflow it — a negative idle window, a subscription ceiling of 0
// (review II, R2-P6-bound-2352).
func uintWithin(c *Cluster, attr uint32, limit uint64) (uint64, bool) {
	v, ok := uintOf(c, attr)
	if !ok || v > limit {
		return 0, false
	}
	return v, true
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
// (PhysicalDeviceProperties.ts:64-177 at b25ed960): an ICD's floor is 0,
// announced only when the caller gave one (:107-115); the default floor is
// 1 s for a Thread node before Matter 1.3 without a Generic Switch, else 0
// (:116-128); the device ceiling by the node's kind (:130-137); an ICD's
// IdleModeDuration is the default ceiling only when it is at least that
// device ceiling (:138-140, doc :68-72) — a shorter one gets the device
// ceiling with its jitter; an ICD's or the caller's ceiling is kept, at least
// the floor (:151-157); a default one gets up to max(10 %, 10 s) of jitter,
// whole seconds (:158-170); at most 65535 s (:171).
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
	var deviceCeiling time.Duration
	switch {
	case p.BatteryPowered && !p.MainsPowered:
		deviceCeiling = ceilingBattery
	case p.ThreadSleepyEndDevice:
		deviceCeiling = ceilingThreadSleepy
	case p.SupportsThread:
		deviceCeiling = ceilingThread
	default:
		deviceCeiling = ceilingWifi
	}
	// Mirrors matter.js packages/protocol/src/peer/PhysicalDeviceProperties.ts:138-140
	// at db91886c (subscriptionIntervalBoundsFor, isIcdCeiling): an ICD's
	// IdleModeDuration below the device ceiling would only let it report more
	// often, so it gets the device ceiling instead.
	isICDCeiling := p.IntermittentlyConnected && p.IdleModeDuration != nil && *p.IdleModeDuration >= deviceCeiling
	def := deviceCeiling
	if isICDCeiling {
		def = min(*p.IdleModeDuration, maxCeiling)
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
