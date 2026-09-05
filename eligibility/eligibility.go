// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package eligibility answers two Matter-side questions that every host
// bridging to Matter has to answer the same way: what will Matter make
// of this source, and what will the ecosystem on the other end of the
// fabric make of the result.
//
// It does *not* classify sources itself — every DP exposes its verdict
// via [contract.EligibilitySource]. This package delegates and
// composes; the model is the single source of truth. Nothing here walks
// a device tree or reads a host enum: the walk that turns a model into
// candidate rows lives host-side, in
// [github.com/SukramJ/go-fabricadapter].
//
// Default verdict for sources that don't override
// [contract.EligibilitySource]: derived structurally from
// [contract.EndpointSource] / [contract.MeasurementSource]
// via [DeriveMatterEligibility]. Custom DPs with caveats (Siren tones,
// Effect light effect dispatch, FixedColorLight palette quantisation)
// implement the interface explicitly to surface
// MatterEligibilityPartial + a UX-readable reason.
//
// The second half — [Compat], [EcosystemForVendor] and [VendorName] —
// is CSA vendor-id knowledge: which controller commissioned a fabric,
// and which device types that controller silently drops.
package eligibility

import (
	"github.com/SukramJ/go-fabric/contract"
)

// Verdict is a re-export of [contract.EligibilityVerdict] for
// callers that only depend on this package.
type Verdict = contract.EligibilityVerdict

// State is a re-export of [contract.EligibilityState].
type State = contract.EligibilityState

// Re-exports of the state constants so callers don't need both
// imports.
const (
	StateUnmappable = contract.EligibilityUnmappable
	StateMappable   = contract.EligibilityMappable
	StatePartial    = contract.EligibilityPartial
)

// ReasonNoProjection is the verdict reason for a source that carries no
// Matter surface at all — neither an endpoint nor a measurement.
//
// It is exported because a caller listing candidates has to tell it
// apart from every other Unmappable reason: a source with a real reason
// is worth a row the operator can read, while an opaque one is noise.
// As an unexported literal it was compared by string in a second
// package, where a reworded reason would have silently stopped matching.
const ReasonNoProjection = "no Matter projection on source type"

// Classify returns the verdict for one source. It honours
// [contract.EligibilitySource] when present, otherwise falls
// back to [DeriveMatterEligibility] for the structural default.
func Classify(src any) Verdict {
	if src == nil {
		return Verdict{State: StateUnmappable, Reason: "nil source"}
	}
	if eligible, ok := src.(contract.EligibilitySource); ok {
		return eligible.MatterEligibility()
	}
	return DeriveMatterEligibility(src)
}

// DeriveMatterEligibility computes the default verdict for a source
// from the structural surface it already implements. Custom DPs that
// implement MatterEndpointSource get a Mappable verdict with the
// declared device type + cluster IDs. Generic / Calculated DPs that
// implement MatterMeasurementSource get a Mappable verdict with the
// measurement-class-derived device type + cluster ID. Anything else
// is Unmappable.
//
// Custom DPs with caveats (Siren tones, Effect dispatch,
// FixedColorLight palette quantisation) implement
// [contract.EligibilitySource] directly and override the
// derivation; they typically call DeriveMatterEligibility for the
// base verdict and then patch in `State = Partial` plus a reason.
func DeriveMatterEligibility(src any) Verdict {
	if ep, ok := src.(contract.EndpointSource); ok {
		dt := ep.MatterDeviceType()
		clusters := clusterIDs(ep.MatterClusterServers())
		if dt == 0 || len(clusters) == 0 {
			return Verdict{
				State:  StateUnmappable,
				Reason: "source advertises no Matter device type or cluster",
			}
		}
		return Verdict{
			State:      StateMappable,
			DeviceType: dt,
			Clusters:   clusters,
		}
	}
	if ms, ok := src.(contract.MeasurementSource); ok {
		class := ms.MatterMeasurementClass()
		if class == contract.MeasurementNone {
			return Verdict{State: StateUnmappable, Reason: "measurement class is None"}
		}
		dt := contract.MeasurementClassDeviceType(class)
		cl := contract.MeasurementClassClusterID(class)
		if cl == 0 {
			return Verdict{State: StateUnmappable, Reason: "measurement class has no cluster equivalent"}
		}
		return Verdict{
			State:      StateMappable,
			DeviceType: dt, // 0 for measurements that ride on a host endpoint
			Clusters:   []uint32{cl},
		}
	}
	return Verdict{State: StateUnmappable, Reason: ReasonNoProjection}
}

func clusterIDs(servers []contract.ClusterServer) []uint32 {
	out := make([]uint32, 0, len(servers))
	for _, s := range servers {
		if s == nil {
			continue
		}
		out = append(out, s.MatterClusterID())
	}
	return out
}
