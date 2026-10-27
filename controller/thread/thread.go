// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package thread forwards to [github.com/SukramJ/go-fabric/thread], where
// the Thread operational dataset codec moved (ADR 0027 decision 2, wave
// 26a): a cluster server may import it there, which it may not import
// from a role package (script/archcheck, servers below roles). Every
// identifier is the same in the new package; change the import path,
// nothing else.
//
// Deprecated: import github.com/SukramJ/go-fabric/thread. The package is
// removed in the next minor release (docs/guides/migrating-to-the-facade.md).
package thread

import "github.com/SukramJ/go-fabric/thread"

// TLV is [thread.TLV].
type TLV = thread.TLV

// TLVType is [thread.TLVType].
type TLVType = thread.TLVType

// SecurityPolicy is [thread.SecurityPolicy].
type SecurityPolicy = thread.SecurityPolicy

// OperationalDataset is [thread.OperationalDataset].
type OperationalDataset = thread.OperationalDataset

// The sentinel errors of [thread].
var (
	// ErrThreadTLV is [thread.ErrThreadTLV].
	ErrThreadTLV = thread.ErrThreadTLV
	// ErrThreadDataset is [thread.ErrThreadDataset].
	ErrThreadDataset = thread.ErrThreadDataset
	// ErrInvalidTLV is [thread.ErrInvalidTLV].
	ErrInvalidTLV = thread.ErrInvalidTLV
)

// WalkTLVs is [thread.WalkTLVs].
func WalkTLVs(blob []byte) ([]TLV, error) { return thread.WalkTLVs(blob) }

// EncodeTLVs is [thread.EncodeTLVs].
func EncodeTLVs(entries []TLV) ([]byte, error) { return thread.EncodeTLVs(entries) }

// DecodeSecurityPolicy is [thread.DecodeSecurityPolicy].
func DecodeSecurityPolicy(value []byte) (SecurityPolicy, error) {
	return thread.DecodeSecurityPolicy(value)
}

// DecodeDataset is [thread.DecodeDataset].
func DecodeDataset(blob []byte) (*OperationalDataset, error) { return thread.DecodeDataset(blob) }

// DecodeDatasetHex is [thread.DecodeDatasetHex].
func DecodeDatasetHex(s string) (*OperationalDataset, error) { return thread.DecodeDatasetHex(s) }

// The MeshCoP TLV types of [thread].
const (
	TLVChannel                 = thread.TLVChannel
	TLVPANID                   = thread.TLVPANID
	TLVExtPANID                = thread.TLVExtPANID
	TLVNetworkName             = thread.TLVNetworkName
	TLVPSKc                    = thread.TLVPSKc
	TLVNetworkKey              = thread.TLVNetworkKey
	TLVNetworkKeySequence      = thread.TLVNetworkKeySequence
	TLVMeshLocalPrefix         = thread.TLVMeshLocalPrefix
	TLVSteeringData            = thread.TLVSteeringData
	TLVBorderAgentLocator      = thread.TLVBorderAgentLocator
	TLVCommissionerID          = thread.TLVCommissionerID
	TLVCommissionerSessionID   = thread.TLVCommissionerSessionID
	TLVSecurityPolicy          = thread.TLVSecurityPolicy
	TLVGet                     = thread.TLVGet
	TLVActiveTimestamp         = thread.TLVActiveTimestamp
	TLVCommissionerUDPPort     = thread.TLVCommissionerUDPPort
	TLVState                   = thread.TLVState
	TLVJoinerDTLSEncapsulation = thread.TLVJoinerDTLSEncapsulation
	TLVJoinerUDPPort           = thread.TLVJoinerUDPPort
	TLVJoinerIID               = thread.TLVJoinerIID
	TLVJoinerRouterLocator     = thread.TLVJoinerRouterLocator
	TLVJoinerRouterKEK         = thread.TLVJoinerRouterKEK
	TLVDuration                = thread.TLVDuration
	TLVProvisioningURL         = thread.TLVProvisioningURL
	TLVVendorName              = thread.TLVVendorName
	TLVVendorModel             = thread.TLVVendorModel
	TLVVendorSWVersion         = thread.TLVVendorSWVersion
	TLVVendorData              = thread.TLVVendorData
	TLVVendorStackVersion      = thread.TLVVendorStackVersion
	TLVUDPEncapsulation        = thread.TLVUDPEncapsulation
	TLVIPv6Address             = thread.TLVIPv6Address
	TLVPendingTimestamp        = thread.TLVPendingTimestamp
	TLVDelayTimer              = thread.TLVDelayTimer
	TLVChannelMask             = thread.TLVChannelMask
	TLVCount                   = thread.TLVCount
	TLVPeriod                  = thread.TLVPeriod
	TLVScanDuration            = thread.TLVScanDuration
	TLVEnergyList              = thread.TLVEnergyList
	TLVThreadDomainName        = thread.TLVThreadDomainName
	TLVWakeupChannel           = thread.TLVWakeupChannel
	TLVDiscoveryRequest        = thread.TLVDiscoveryRequest
	TLVDiscoveryResponse       = thread.TLVDiscoveryResponse
	TLVJoinerAdvertisement     = thread.TLVJoinerAdvertisement
)
