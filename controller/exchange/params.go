// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// Fallbacks for session parameters a peer did not report. Mirrors matter.js
// packages/protocol/src/session/SessionIntervals.ts:45-49 (the intervals)
// and SessionParameters.ts:118-153 (the rest) at 0ceb202d.
const (
	FallbackIdleInterval             uint32 = 500  // ms, SessionIntervals.ts:46
	FallbackActiveInterval           uint32 = 300  // ms, SessionIntervals.ts:47
	FallbackActiveThreshold          uint16 = 4000 // ms, SessionIntervals.ts:48
	FallbackDataModelRevision        uint16 = 17   // SessionParameters.ts:125
	FallbackInteractionModelRevision uint16 = 11   // SessionParameters.ts:131
	FallbackMaxPathsPerInvoke        uint16 = 1    // SessionParameters.ts:145
)

// Our own revisions, the ones matter.js SessionParameters.defaults
// (SessionParameters.ts:158-164) takes from packages/model/src/common/Specification.ts.
const (
	// localDataModelRevision is Specification.ts:115 DATA_MODEL_REVISION.
	localDataModelRevision uint16 = 21
	// localSpecificationVersion is Specification.ts:110 SPECIFICATION_VERSION.
	localSpecificationVersion uint32 = 0x01060100
)

// DefaultLocalParameters is the session parameters a controller sends
// when its host chose none: matter.js SessionParameters.defaults
// (SessionParameters.ts:158-164), the fallbacks with this module's
// data-model revision, interaction-model revision and specification
// version.
func DefaultLocalParameters() sessionparams.Parameters {
	return sessionparams.Parameters{
		SessionIdleInterval:      FallbackIdleInterval,
		SessionActiveInterval:    FallbackActiveInterval,
		SessionActiveThreshold:   FallbackActiveThreshold,
		DataModelRevision:        localDataModelRevision,
		InteractionModelRevision: uint16(im.MatterInteractionModelRevision),
		SpecificationVersion:     localSpecificationVersion,
		MaxPathsPerInvoke:        FallbackMaxPathsPerInvoke,
	}
}

// PeerParameters is the parameters a session assumes for its peer: the
// fallbacks overlaid by what the peer reported (nil reported nothing), with
// TCP cleared for a peer below Matter 1.5, MaxTCPMessageSize only when TCP
// remains, and a MaxPathsPerInvoke of 0 taking the fallback. Mirrors
// matter.js SessionParameters.ts:42-81 (the SessionParameters factory).
func PeerParameters(reported *sessionparams.Parameters) sessionparams.Parameters {
	var r sessionparams.Parameters
	if reported != nil {
		r = *reported
	}
	p := sessionparams.Parameters{
		SessionIdleInterval:      orU32(r.SessionIdleInterval, FallbackIdleInterval),
		SessionActiveInterval:    orU32(r.SessionActiveInterval, FallbackActiveInterval),
		SessionActiveThreshold:   orU16(r.SessionActiveThreshold, FallbackActiveThreshold),
		DataModelRevision:        orU16(r.DataModelRevision, FallbackDataModelRevision),
		InteractionModelRevision: orU16(r.InteractionModelRevision, FallbackInteractionModelRevision),
		SpecificationVersion:     r.SpecificationVersion,
		MaxPathsPerInvoke:        orU16(r.MaxPathsPerInvoke, FallbackMaxPathsPerInvoke),
		SupportedTransports:      r.EffectiveTransports(),
	}
	p.MaxTCPMessageSize = r.EffectiveMaxTCPMessageSize()
	return p
}

// Overlay returns base with every field over states replacing it — the
// object spread `{...base, ...over}` matter.js uses to combine parameter
// sets (PaseClient.ts:168-171, CaseClient.ts:184-188).
func Overlay(base sessionparams.Parameters, over *sessionparams.Parameters) sessionparams.Parameters {
	if over == nil {
		return base
	}
	o := *over
	if o.SessionIdleInterval != 0 {
		base.SessionIdleInterval = o.SessionIdleInterval
	}
	if o.SessionActiveInterval != 0 {
		base.SessionActiveInterval = o.SessionActiveInterval
	}
	if o.SessionActiveThreshold != 0 {
		base.SessionActiveThreshold = o.SessionActiveThreshold
	}
	if o.DataModelRevision != 0 {
		base.DataModelRevision = o.DataModelRevision
	}
	if o.InteractionModelRevision != 0 {
		base.InteractionModelRevision = o.InteractionModelRevision
	}
	if o.SpecificationVersion != 0 {
		base.SpecificationVersion = o.SpecificationVersion
	}
	if o.MaxPathsPerInvoke != 0 {
		base.MaxPathsPerInvoke = o.MaxPathsPerInvoke
	}
	if o.SupportedTransports != 0 {
		base.SupportedTransports = o.SupportedTransports
	}
	if o.MaxTCPMessageSize != 0 {
		base.MaxTCPMessageSize = o.MaxTCPMessageSize
	}
	return base
}

// IntervalsOf is the MRP timing of p, the fallback for every interval p
// does not state (SessionIntervals.ts:36-42).
func IntervalsOf(p sessionparams.Parameters) Intervals {
	return Intervals{
		Idle:            time.Duration(orU32(p.SessionIdleInterval, FallbackIdleInterval)) * time.Millisecond,
		Active:          time.Duration(orU32(p.SessionActiveInterval, FallbackActiveInterval)) * time.Millisecond,
		ActiveThreshold: time.Duration(orU16(p.SessionActiveThreshold, FallbackActiveThreshold)) * time.Millisecond,
	}
}

func orU32(v, fallback uint32) uint32 {
	if v == 0 {
		return fallback
	}
	return v
}

func orU16(v, fallback uint16) uint16 {
	if v == 0 {
		return fallback
	}
	return v
}
