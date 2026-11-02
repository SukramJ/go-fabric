// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package eligibility answers two Matter-side questions that every host
// bridging to Matter has to answer the same way: what will Matter make
// of this source, and what will the ecosystem on the other end of the
// fabric make of the result.
//
// It does *not* classify sources itself: a host source states its own
// verdict through [contract.EligibilitySource], and this package
// delegates and composes. Nothing here walks a device tree or reads a
// host's model; the walk that turns a host's model into candidate
// sources is the host's.
//
// A source that does not implement [contract.EligibilitySource] gets the
// verdict [DeriveMatterEligibility] derives from the structural surface
// it implements: [contract.EndpointSource] (a device type and its
// cluster servers) or [contract.MeasurementSource] (a measurement class
// and the device type and cluster it projects to). A source whose
// projection is lossy implements [contract.EligibilitySource] itself and
// reports [StatePartial] with a reason an operator reads.
//
// The second half — [Compat], [EcosystemForVendor] and [VendorName] —
// is CSA vendor-id knowledge: which controller commissioned a fabric,
// and which device types that controller is reported to drop (the rows
// of notes/parity/controller_profiles.md, "eligibility.Compat rules").
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
// from the structural surface it already implements. A
// [contract.EndpointSource] gets a Mappable verdict with its declared
// device type and cluster ids; a [contract.MeasurementSource] gets a
// Mappable verdict with the device type and cluster id its measurement
// class projects to. Anything else is Unmappable.
//
// A source whose projection is lossy implements
// [contract.EligibilitySource] and overrides the derivation; it
// typically calls DeriveMatterEligibility for the base verdict and then
// reports [StatePartial] with a reason.
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
