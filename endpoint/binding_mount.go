// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"slices"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/schema"
)

// bindingClusterID is the Binding cluster (binding.element.ts, 0x001E).
const bindingClusterID uint32 = 0x001E

// baseMandatesBinding reports whether the Base device type mandates the
// Binding server on an endpoint whose DeviceTypeList lists deviceTypes and
// that declares clients — the facts of the endpoint as its Descriptor
// states them, as matter.js's #addBaseServers judges them
// (DeviceTypeConformanceService.ts:361-369 at b25ed960, a pass over
// ServerEndpointFacts, whose device types are the DeviceTypeList's). Base
// states Binding as "Simple & Client", and the Client condition holds for
// a declared client application cluster a binding may direct — matter.js
// ResolvedEndpoint.hasBindableApplicationClient
// (packages/model/src/logic/device-types/ResolvedEndpoint.ts:120-139),
// which ownStructuralConditionsOf turns into the condition
// (ConditionAssertions.ts:600-602). The conditions are those the
// endpoint's own device types and client declarations decide (Node, App,
// Simple, Dynamic, Client); Base's requirement is evaluated against them,
// not hand-coded. A device type the schema does not define decides
// nothing; an endpoint listing none it defines is not judged.
func baseMandatesBinding(deviceTypes []uint16, clients []uint32) bool {
	l := deviceTypeLookups()
	conds := map[string]bool{}
	known := false
	for _, id := range deviceTypes {
		dt, ok := schema.DeviceTypeDefinitionOf(schema.DeviceTypeID(id))
		if !ok {
			continue
		}
		known = true
		switch dt.Classification {
		case "node":
			conds[condNode] = true
		case "application":
			conds[condApp] = true
		case "simple":
			conds[condApp], conds[condSimple] = true, true
		case "dynamic":
			conds[condApp], conds[condDynamic] = true, true
		default:
		}
	}
	if !known {
		return false
	}
	for _, c := range clients {
		if l.m.classification(c) == "application" && l.m.bindable(c) {
			conds[condClient] = true
		}
	}
	for _, base := range l.m.base {
		all, _ := l.knownNames(base, 0, false)
		for i := range base.Requirements {
			r := &base.Requirements[i]
			if r.Element != schema.RequirementServerCluster || !r.Referent.Resolved || r.Referent.ID != bindingClusterID {
				continue
			}
			if l.requirementApplicability(r, conds, all) == spec.ApplicabilityMandatory {
				return true
			}
		}
	}
	return false
}

// mountBinding places the endpoint's Binding server on an endpoint for
// which Base mandates Binding when the source supplied none: matter.js
// gives such an endpoint a BindingServer without its declaring one
// (packages/node/src/behaviors/binding/BindingServer.ts:54-56). The server
// is the endpoint identity's own ([endpointState.bindingServer]), so the
// list a controller wrote outlives the per-dispatch rebuild of the
// cluster set. A source that serves Binding itself (a device-layer device
// type that mandates it) keeps its own.
func mountBinding(ep *Endpoint, inner []contract.ClusterServer) []contract.ClusterServer {
	out := slices.Clone(inner)
	mounted := false
	for _, srv := range inner {
		if srv != nil && srv.MatterClusterID() == bindingClusterID {
			mounted = true
			break
		}
	}
	if !mounted && baseMandatesBinding(listedDeviceTypes(ep), ep.ClientClusters) {
		out = append(out, ep.endpointState().bindingServer())
	}
	// Every Binding server the endpoint serves — the stack's or one a
	// device supplies — checks a request's write against the node's
	// fabric list ([Config.Fabrics]; R2-P6-store-1010, decision 158).
	if ep.fabrics != nil {
		for _, srv := range out {
			if b, ok := srv.(*mattercore.Binding); ok {
				b.SetFabricSource(ep.fabrics)
			}
		}
	}
	return out
}

// bindingServer returns (lazily creating) the endpoint identity's Binding
// server, the one [mountBinding] mounts.
func (s *endpointState) bindingServer() *mattercore.Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding == nil {
		s.binding = mattercore.NewBinding()
	}
	return s.binding
}
