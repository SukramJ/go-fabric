// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

// TLVType is a MeshCoP TLV type identifier (Thread 1.4 spec §8.10 plus the
// types of Agners' reference parser).
//
// Mirrors matter.js packages/protocol/src/thread/meshcopTlvTypes.ts:14-58
// (MeshCopTlvType), in its order.
type TLVType uint8

// The MeshCoP TLV types; [TLVType.Name] gives each its matter.js name.
const (
	TLVChannel                 TLVType = 0
	TLVPANID                   TLVType = 1
	TLVExtPANID                TLVType = 2
	TLVNetworkName             TLVType = 3
	TLVPSKc                    TLVType = 4
	TLVNetworkKey              TLVType = 5
	TLVNetworkKeySequence      TLVType = 6
	TLVMeshLocalPrefix         TLVType = 7
	TLVSteeringData            TLVType = 8
	TLVBorderAgentLocator      TLVType = 9
	TLVCommissionerID          TLVType = 10
	TLVCommissionerSessionID   TLVType = 11
	TLVSecurityPolicy          TLVType = 12
	TLVGet                     TLVType = 13
	TLVActiveTimestamp         TLVType = 14
	TLVCommissionerUDPPort     TLVType = 15
	TLVState                   TLVType = 16
	TLVJoinerDTLSEncapsulation TLVType = 17
	TLVJoinerUDPPort           TLVType = 18
	TLVJoinerIID               TLVType = 19
	TLVJoinerRouterLocator     TLVType = 20
	TLVJoinerRouterKEK         TLVType = 21
	TLVDuration                TLVType = 23
	TLVProvisioningURL         TLVType = 32
	TLVVendorName              TLVType = 33
	TLVVendorModel             TLVType = 34
	TLVVendorSWVersion         TLVType = 35
	TLVVendorData              TLVType = 36
	TLVVendorStackVersion      TLVType = 37
	TLVUDPEncapsulation        TLVType = 48
	TLVIPv6Address             TLVType = 49
	TLVPendingTimestamp        TLVType = 51
	TLVDelayTimer              TLVType = 52
	TLVChannelMask             TLVType = 53
	TLVCount                   TLVType = 54
	TLVPeriod                  TLVType = 55
	TLVScanDuration            TLVType = 56
	TLVEnergyList              TLVType = 57
	TLVThreadDomainName        TLVType = 59
	TLVWakeupChannel           TLVType = 74
	TLVDiscoveryRequest        TLVType = 128
	TLVDiscoveryResponse       TLVType = 129
	TLVJoinerAdvertisement     TLVType = 241
)

// tlvTypes is every [TLVType] with its matter.js name, in the order of
// meshcopTlvTypes.ts:14-58.
var tlvTypes = []struct {
	typ  TLVType
	name string
}{
	{TLVChannel, "CHANNEL"},
	{TLVPANID, "PANID"},
	{TLVExtPANID, "EXTPANID"},
	{TLVNetworkName, "NETWORK_NAME"},
	{TLVPSKc, "PSKC"},
	{TLVNetworkKey, "NETWORK_KEY"},
	{TLVNetworkKeySequence, "NETWORK_KEY_SEQUENCE"},
	{TLVMeshLocalPrefix, "MESH_LOCAL_PREFIX"},
	{TLVSteeringData, "STEERING_DATA"},
	{TLVBorderAgentLocator, "BORDER_AGENT_LOCATOR"},
	{TLVCommissionerID, "COMMISSIONER_ID"},
	{TLVCommissionerSessionID, "COMMISSIONER_SESSION_ID"},
	{TLVSecurityPolicy, "SECURITY_POLICY"},
	{TLVGet, "GET"},
	{TLVActiveTimestamp, "ACTIVE_TIMESTAMP"},
	{TLVCommissionerUDPPort, "COMMISSIONER_UDP_PORT"},
	{TLVState, "STATE"},
	{TLVJoinerDTLSEncapsulation, "JOINER_DTLS_ENCAPSULATION"},
	{TLVJoinerUDPPort, "JOINER_UDP_PORT"},
	{TLVJoinerIID, "JOINER_IID"},
	{TLVJoinerRouterLocator, "JOINER_ROUTER_LOCATOR"},
	{TLVJoinerRouterKEK, "JOINER_ROUTER_KEK"},
	{TLVDuration, "DURATION"},
	{TLVProvisioningURL, "PROVISIONING_URL"},
	{TLVVendorName, "VENDOR_NAME"},
	{TLVVendorModel, "VENDOR_MODEL"},
	{TLVVendorSWVersion, "VENDOR_SW_VERSION"},
	{TLVVendorData, "VENDOR_DATA"},
	{TLVVendorStackVersion, "VENDOR_STACK_VERSION"},
	{TLVUDPEncapsulation, "UDP_ENCAPSULATION"},
	{TLVIPv6Address, "IPV6_ADDRESS"},
	{TLVPendingTimestamp, "PENDING_TIMESTAMP"},
	{TLVDelayTimer, "DELAY_TIMER"},
	{TLVChannelMask, "CHANNEL_MASK"},
	{TLVCount, "COUNT"},
	{TLVPeriod, "PERIOD"},
	{TLVScanDuration, "SCAN_DURATION"},
	{TLVEnergyList, "ENERGY_LIST"},
	{TLVThreadDomainName, "THREAD_DOMAIN_NAME"},
	{TLVWakeupChannel, "WAKEUP_CHANNEL"},
	{TLVDiscoveryRequest, "DISCOVERY_REQUEST"},
	{TLVDiscoveryResponse, "DISCOVERY_RESPONSE"},
	{TLVJoinerAdvertisement, "JOINER_ADVERTISEMENT"},
}

// tlvTypeNames is the reverse lookup of tlvTypes.
var tlvTypeNames = func() map[TLVType]string {
	out := make(map[TLVType]string, len(tlvTypes))
	for _, e := range tlvTypes {
		out[e.typ] = e.name
	}
	return out
}()

// Name is the matter.js name of a MeshCoP TLV type ("CHANNEL",
// "NETWORK_NAME", …), for diagnostics; ok is false for a type the table
// does not know.
//
// Mirrors matter.js packages/protocol/src/thread/meshcopTlvTypes.ts:63-69
// (MeshCopTlvTypeName).
func (t TLVType) Name() (name string, ok bool) {
	name, ok = tlvTypeNames[t]
	return name, ok
}
