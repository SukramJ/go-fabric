// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"math"

	"github.com/SukramJ/go-fabric/schema"
)

// WildcardPathFlags bits of an AttributePathIB (Matter Core §8.9.2.5): each
// asks a wildcard expansion to skip a class of paths. Mirrors matter.js
// packages/types/src/globals/WildcardPathFlags.ts:18-58 (bit 3 is
// doNotUse).
const (
	WildcardSkipRootNode                 uint32 = 1 << 0
	WildcardSkipGlobalAttributes         uint32 = 1 << 1
	WildcardSkipAttributeList            uint32 = 1 << 2
	WildcardSkipCommandLists             uint32 = 1 << 4
	WildcardSkipCustomElements           uint32 = 1 << 5
	WildcardSkipFixedAttributes          uint32 = 1 << 6
	WildcardSkipChangesOmittedAttributes uint32 = 1 << 7
	WildcardSkipDiagnosticsClusters      uint32 = 1 << 8
)

// The global attribute ids a WildcardPathFlags filter names.
const (
	globalGeneratedCommandList uint32 = 0xFFF8
	globalAcceptedCommandList  uint32 = 0xFFF9
	globalAttributeList        uint32 = 0xFFFB
	globalFeatureMap           uint32 = 0xFFFC
	globalClusterRevision      uint32 = 0xFFFD
)

// BasicInformation ConfigurationVersion (endpoint 0, cluster 0x28,
// attribute 0x18), the version a WildcardFilterConfigurationVersion is
// judged against.
const (
	basicInformationCluster           uint32 = 0x0028
	basicInfoConfigurationVersionAttr uint32 = 0x0018
)

// ElementQualities answers the model qualities a WildcardPathFlags filter
// judges and neither the path ids nor package schema carry: whether a
// cluster has the Diagnostics (K) quality and whether an attribute has the
// Fixed (F) quality. The IM layer knows no cluster model; the bridge
// supplies one through [WithElementQualities]. The Changes Omitted (C)
// quality comes from [schema.AttributeChangesOmitted].
type ElementQualities interface {
	DiagnosticsCluster(cluster uint32) bool
	AttributeFixed(cluster, attribute uint32) bool
}

type elementQualitiesCtxKey struct{}

// WithElementQualities attaches q to ctx for [ApplyWildcardPathFlags]. A
// context without one skips no Diagnostics cluster and, of the Fixed
// attributes, only the five global ones.
func WithElementQualities(ctx context.Context, q ElementQualities) context.Context {
	return context.WithValue(ctx, elementQualitiesCtxKey{}, q)
}

func elementQualitiesFrom(ctx context.Context) ElementQualities {
	q, _ := ctx.Value(elementQualitiesCtxKey{}).(ElementQualities)
	return q
}

// ApplyWildcardPathFlags drops from results — the expansion of the request
// path — every path its WildcardPathFlags skip. A concrete path, a path
// without flags and a path whose WildcardFilterConfigurationVersion is
// older than the node's ConfigurationVersion return results unchanged.
//
// Mirrors matter.js packages/protocol/src/action/server/
// AttributeReadResponse.ts:149-197 (addWildcard: the flags apply to a
// wildcard path only, and not once the configuration changed past the
// filter version — an omitted filter or 0xFFFFFFFF is current) with the
// per-level skips of readEndpointForWildcard (:321-324),
// readClusterForWildcard (:355-358) and readAttributeForWildcard
// (:403-405), and the flags each level carries from
// packages/node/src/node/integration/ProtocolService.ts:235 (endpoint 0),
// :428-431 (Diagnostics cluster, custom cluster) and :474-499 (the global
// lists, custom, Fixed and Changes Omitted attributes).
func ApplyWildcardPathFlags(ctx context.Context, d Dispatcher, path ConcreteAttributePath, results []ReadResult) []ReadResult {
	flags := effectiveWildcardPathFlags(ctx, d, path)
	if flags == 0 {
		return results
	}
	q := elementQualitiesFrom(ctx)
	out := results[:0:0]
	for _, r := range results {
		if wildcardPathSkipped(r.Path, flags, q) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// effectiveWildcardPathFlags returns the flags that apply to path's
// expansion: none for a concrete path, none once the node's
// ConfigurationVersion advanced past the filter version
// (AttributeReadResponse.ts:166-178).
func effectiveWildcardPathFlags(ctx context.Context, d Dispatcher, path ConcreteAttributePath) uint32 {
	if path.WildcardPathFlags == 0 || (path.HasEndpoint && path.HasCluster && path.HasAttribute) {
		return 0
	}
	if path.HasWildcardFilterConfigurationVersion && path.WildcardFilterConfigurationVersion != math.MaxUint32 {
		if current, ok := currentConfigurationVersion(ctx, d); ok && path.WildcardFilterConfigurationVersion < current {
			return 0
		}
	}
	return path.WildcardPathFlags
}

// currentConfigurationVersion reads BasicInformation ConfigurationVersion
// on endpoint 0, false when the node does not serve it
// (AttributeReadResponse.ts:137-144).
func currentConfigurationVersion(ctx context.Context, d Dispatcher) (uint32, bool) {
	for _, r := range d.Read(ctx, ConcreteAttributePath{
		Endpoint: 0, Cluster: basicInformationCluster, Attribute: basicInfoConfigurationVersionAttr,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}) {
		if r.Status != StatusSuccess {
			return 0, false
		}
		switch v := r.Value.Value.(type) {
		case uint32:
			return v, true
		case uint64:
			if v <= math.MaxUint32 {
				return uint32(v), true
			}
		case int:
			if v >= 0 && uint64(v) <= math.MaxUint32 {
				return uint32(v), true
			}
		}
	}
	return 0, false
}

// WildcardPathFlagsSkip reports whether the WildcardPathFlags of the
// request path skip the concrete path — the test [ApplyWildcardPathFlags]
// applies to each result, for a caller that holds a concrete path already
// (a subscription's changed attribute).
func WildcardPathFlagsSkip(ctx context.Context, d Dispatcher, request, concrete ConcreteAttributePath) bool {
	flags := effectiveWildcardPathFlags(ctx, d, request)
	return flags != 0 && wildcardPathSkipped(concrete, flags, elementQualitiesFrom(ctx))
}

// wildcardPathSkipped reports whether flags skip the concrete path p.
func wildcardPathSkipped(p ConcreteAttributePath, flags uint32, q ElementQualities) bool {
	// Endpoint level (ProtocolService.ts:235).
	if p.Endpoint == 0 && flags&WildcardSkipRootNode != 0 {
		return true
	}
	// Cluster level (ProtocolService.ts:428-431).
	if p.Cluster&0xFFFF0000 != 0 && flags&WildcardSkipCustomElements != 0 {
		return true
	}
	if flags&WildcardSkipDiagnosticsClusters != 0 && q != nil && q.DiagnosticsCluster(p.Cluster) {
		return true
	}
	return flags&attributeWildcardFlags(p.Cluster, p.Attribute, q) != 0
}

// attributeWildcardFlags returns the flags that skip one attribute
// (ProtocolService.ts:474-499). The five global attributes are Fixed in
// matter.js's model (packages/model/src/standard/elements/
// {cluster-revision,feature-map,attribute-list,accepted-command-list,
// generated-command-list}.element.ts, quality "F").
func attributeWildcardFlags(cluster, attribute uint32, q ElementQualities) uint32 {
	var flags uint32
	switch attribute {
	case globalGeneratedCommandList, globalAcceptedCommandList:
		flags = WildcardSkipGlobalAttributes | WildcardSkipCommandLists
	case globalAttributeList:
		flags = WildcardSkipGlobalAttributes | WildcardSkipAttributeList
	}
	if attribute&0xFFFF0000 != 0 {
		flags |= WildcardSkipCustomElements
	}
	var fixed bool
	switch attribute {
	case globalGeneratedCommandList, globalAcceptedCommandList, globalAttributeList, globalFeatureMap, globalClusterRevision:
		fixed = true
	default:
		fixed = q != nil && q.AttributeFixed(cluster, attribute)
	}
	if fixed {
		flags |= WildcardSkipFixedAttributes
	}
	if schema.AttributeChangesOmitted(cluster, attribute) {
		flags |= WildcardSkipChangesOmittedAttributes
	}
	return flags
}
