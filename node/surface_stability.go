// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/surface"
)

// The settings keys an endpoint's effective surface is kept under, next to
// its ConfigurationVersion: a bridged endpoint (and a part) under
// "bridged_surface_digest." and its stable key, as
// "bridged_configuration_version." keys the version; an endpoint without
// one (the Aggregator, a host topology's unkeyed endpoint) under "@" and
// its number, as [bindingKey] keys its Binding list; the root under its
// own key, as rootConfigVersionKey keys the root's version.
const (
	endpointSurfaceKey = "bridged_surface_digest."
	rootSurfaceKey     = "basic_information.surface_digest"
)

// maxChanged bounds the announced changes [Node.Surface] reports.
const maxChanged = 100

// storedSurface is what the store keeps for an endpoint: the digest of its
// effective surface and the surface itself (the endpoint alone, without
// its number), so a later change can be told as a [surface.Diff].
type storedSurface struct {
	Digest  string          `json:"digest"`
	Surface surface.Surface `json:"surface"`
}

// assembledEndpoint is one endpoint of the assembled topology with its key
// in the surface report, its settings key and its effective surface.
type assembledEndpoint struct {
	ep       *endpoint.Endpoint
	key      string
	storeKey string
	surface  surface.Endpoint
}

// surfaceKeys returns the endpoint's key in the surface report and the
// settings key its surface is persisted under: "" and [rootSurfaceKey]
// for the root, [AggregatorSurfaceKey] for the Aggregator, the rendered
// stable key otherwise — "@" and the number for an endpoint without one.
func surfaceKeys(ep *endpoint.Endpoint) (key, storeKey string) {
	switch {
	case ep.IsRoot():
		return "", rootSurfaceKey
	case ep.IsAggregator():
		return AggregatorSurfaceKey, endpointSurfaceKey + "@" + strconv.FormatUint(uint64(ep.ID), 10)
	}
	if k, ok := keyString(ep.SourceKey); ok && k != "" {
		return k, endpointSurfaceKey + k
	}
	at := "@" + strconv.FormatUint(uint64(ep.ID), 10)
	return at, endpointSurfaceKey + at
}

// assembledSurface reads every endpoint of topo as a controller's wildcard
// read of it returns: the servers the endpoint assembler mounts
// (endpoint.ClusterServers) through endpoint.SurfaceEndpoint — the one
// function the wire and the snapshot share — with the device types the
// endpoint's Descriptor lists, and its number.
func assembledSurface(topo *endpoint.Topology) []assembledEndpoint {
	out := make([]assembledEndpoint, 0, len(topo.Endpoints))
	for _, ep := range topo.Endpoints {
		if ep == nil {
			continue
		}
		key, storeKey := surfaceKeys(ep)
		s := endpoint.SurfaceEndpoint(key, endpoint.ClusterServers(ep))
		s.Number = ep.ID
		out = append(out, assembledEndpoint{ep: ep, key: key, storeKey: storeKey, surface: s})
	}
	return out
}

// loadSurface returns the surface persisted under storeKey: its digest —
// recomputed from the stored surface when that decodes, so a change of
// the canonical encoding is no change of the surface — and the surface;
// ok is false when none is persisted (or what is cannot be read, which
// counts as none).
func (p persisted) loadSurface(ctx context.Context, storeKey string) (digest [32]byte, before *surface.Endpoint, ok bool) {
	v, found, err := p.st.GetSetting(ctx, storeKey)
	if err != nil || !found || v == "" {
		return digest, nil, false
	}
	var st storedSurface
	if err := json.Unmarshal([]byte(v), &st); err != nil {
		p.logger.Warn("surface.restore", slog.String("key", storeKey), slog.Any("err", err))
		return digest, nil, false
	}
	if len(st.Surface.Endpoints) == 1 {
		ep := st.Surface.Endpoints[0]
		return ep.Digest(), &ep, true
	}
	b, err := hex.DecodeString(st.Digest)
	if err != nil || len(b) != len(digest) {
		p.logger.Warn("surface.restore", slog.String("key", storeKey), slog.String("value", st.Digest))
		return digest, nil, false
	}
	copy(digest[:], b)
	return digest, nil, true
}

// storeSurface persists ep (its number left out) and its digest under
// storeKey.
func (p persisted) storeSurface(ctx context.Context, storeKey string, ep surface.Endpoint) {
	ep.Number = 0
	digest := ep.Digest()
	b, err := json.Marshal(storedSurface{Digest: hex.EncodeToString(digest[:]), Surface: surface.Surface{Endpoints: []surface.Endpoint{ep}}})
	if err == nil {
		err = p.st.SetSetting(ctx, storeKey, string(b))
	}
	if err != nil {
		p.logger.Warn("surface.persist", slog.String("key", storeKey), slog.Any("err", err))
	}
}

// forgetSurface drops the surface persisted for the bridged endpoint with
// the rendered key: a withheld endpoint serves nothing, and when it is
// served again its surface is a first assembly, not a change. Nothing is
// written when none is persisted.
func (p persisted) forgetSurface(ctx context.Context, key string) {
	storeKey := endpointSurfaceKey + key
	if v, found, err := p.st.GetSetting(ctx, storeKey); err != nil || !found || v == "" {
		return
	}
	if err := p.st.SetSetting(ctx, storeKey, ""); err != nil {
		p.logger.Warn("surface.persist", slog.String("key", storeKey), slog.Any("err", err))
	}
}

// surfaceDrift is one endpoint whose effective surface differs from the
// one persisted for it.
type surfaceDrift struct {
	assembledEndpoint
	before  [32]byte
	after   [32]byte
	changes []surface.Change
}

// announceSurface compares every endpoint of the bridge's topology with
// the surface persisted for it and announces a change the way the
// standard provides (ADR 0030 decision 6, concept v11 §C.9): in one
// [bridge.Bridge.IncreaseNodeConfigurationVersionWith] change it raises
// the BridgedDeviceBasicInformation ConfigurationVersion of every bridged
// endpoint that changed — for a part, of the bridged endpoint it belongs
// to — and the root's BasicInformation ConfigurationVersion once, then
// bumps the DataVersion of a changed bridged endpoint's Descriptor and of
// every cluster the [surface.Diff] names, persists the raised versions and
// the new surfaces, and logs one "surface changed" record per endpoint. A
// change of the root's or the Aggregator's surface raises the root's
// version alone. An endpoint without a persisted surface — its first
// assembly — is persisted without a raise; an unchanged one writes
// nothing.
//
// matter.js provides the raise and leaves the "when" to the application
// (packages/node/src/behaviors/basic-information/BasicInformationServer.ts:35-37
// at 862ad75f, "When the node's configuration changes in a way clients
// should detect, bump it with increaseConfigurationVersion"; the method at
// :132-138). Its only production caller at the pin is the bridged node's
// coupled raise
// (packages/node/src/behaviors/bridged-device-basic-information/BridgedDeviceBasicInformationServer.ts:86);
// raising it on a surface change is this module's own rule, ahead of
// matter.js.
func (n *Node) announceSurface(ctx context.Context) {
	topo := n.br.Topology()
	if topo == nil {
		return
	}
	n.announceMu.Lock()
	defer n.announceMu.Unlock()
	var drifts []surfaceDrift
	for _, e := range assembledSurface(topo) {
		after := e.surface.Digest()
		before, prev, ok := n.persisted.loadSurface(ctx, e.storeKey)
		switch {
		case !ok:
			n.persisted.storeSurface(ctx, e.storeKey, e.surface)
		case before != after:
			d := surfaceDrift{assembledEndpoint: e, before: before, after: after}
			if prev != nil {
				prev.Key = e.key
				d.changes = surface.Diff(surface.Surface{Endpoints: []surface.Endpoint{*prev}}, surface.Surface{Endpoints: []surface.Endpoint{e.surface}})
			}
			drifts = append(drifts, d)
		}
	}
	if len(drifts) == 0 {
		return
	}
	var raise []uint16
	for i := range drifts {
		if id, ok := configurationVersionCarrier(topo, drifts[i].ep); ok && !slices.Contains(raise, id) {
			raise = append(raise, id)
		}
	}
	versions := n.br.IncreaseNodeConfigurationVersionWith(func(c *bridge.ConfigurationChange) {
		for _, id := range raise {
			c.IncreaseEndpointConfigurationVersion(id)
		}
	})
	n.persisted.storeConfigVersions(ctx, topo, versions)
	at := n.cfg.Clock.Now()
	for i := range drifts {
		d := &drifts[i]
		if !d.ep.IsRoot() && !d.ep.IsAggregator() {
			// The root's and the Aggregator's servers keep their own
			// DataVersion, which follows their content (a Descriptor's
			// lists); a bridged endpoint's lives on the endpoint.
			d.ep.BumpClusterDataVersion(descriptorClusterID)
			for _, id := range changedClusters(d.changes) {
				if id != descriptorClusterID {
					d.ep.BumpClusterDataVersion(id)
				}
			}
		}
		n.persisted.storeSurface(ctx, d.storeKey, d.surface)
		n.recordChange(surface.SurfaceChange{Endpoint: d.key, Before: d.before, After: d.after, Changes: d.changes, At: at})
		lines := make([]string, 0, len(d.changes))
		for _, ch := range d.changes {
			lines = append(lines, ch.String())
		}
		n.logger.Info("surface changed",
			slog.String("key", d.key),
			slog.Int("endpoint", int(d.ep.ID)),
			slog.String("from", hex.EncodeToString(d.before[:])[:12]),
			slog.String("to", hex.EncodeToString(d.after[:])[:12]),
			slog.Any("changed", lines))
	}
}

// descriptorClusterID is the Descriptor cluster (descriptor.element.ts,
// 0x001D).
const descriptorClusterID uint32 = 0x001D

// configurationVersionCarrier returns the number of the bridged endpoint
// whose BridgedDeviceBasicInformation announces a change of ep: ep itself,
// or for a part the bridged endpoint it belongs to; false for the root,
// the Aggregator and a part under either, whose change the root's
// BasicInformation announces.
func configurationVersionCarrier(topo *endpoint.Topology, ep *endpoint.Endpoint) (uint16, bool) {
	for range len(topo.Endpoints) {
		if ep == nil || ep.IsRoot() || ep.IsAggregator() {
			return 0, false
		}
		if !ep.Part {
			return ep.ID, true
		}
		ep = topo.FindByID(ep.ParentEndpointID)
	}
	return 0, false
}

// changedClusters are the ids of the clusters changes name, ascending and
// once each: a cluster added, removed or changed in its revision,
// FeatureMap or one of its lists. A device type change names none.
func changedClusters(changes []surface.Change) []uint32 {
	var out []uint32
	for _, ch := range changes {
		if ch.Kind == surface.KindDeviceType || slices.Contains(out, ch.Cluster) {
			continue
		}
		out = append(out, ch.Cluster)
	}
	slices.Sort(out)
	return out
}

// recordChange keeps c for [Node.Surface], the last [maxChanged].
func (n *Node) recordChange(c surface.SurfaceChange) {
	n.changedMu.Lock()
	defer n.changedMu.Unlock()
	n.changed = append(n.changed, c)
	if over := len(n.changed) - maxChanged; over > 0 {
		n.changed = slices.Delete(n.changed, 0, over)
	}
}

// announcedChanges returns a copy of the changes announced so far.
func (n *Node) announcedChanges() []surface.SurfaceChange {
	n.changedMu.Lock()
	defer n.changedMu.Unlock()
	return surface.Report{Changed: n.changed}.Clone().Changed
}
