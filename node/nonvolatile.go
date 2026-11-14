// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	tnddef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// nonVolatileKeyRoot prefixes the settings key a nonvolatile attribute's
// value is kept under: "nonvolatile.", the endpoint's stable key (or "@"
// and its number, as bindingKey), the cluster and the attribute id in hex.
const nonVolatileKeyRoot = "nonvolatile."

// keptElsewhere are the nonvolatile writable attributes a store of their
// own keeps already: the root's NodeLabel and Location (restoreRoot), a
// bridged NodeLabel (OnNodeLabelWritten), Binding (bindingConfig),
// UserLabel (buildLabels, persistUserLabels), GroupKeyMap and
// DefaultOTAProviders (store), the Thermostat's Presets (a declared
// PresetStore) and Schedules (written in an atomic write that a command
// commits, not by the write itself), and the ThreadNetworkDirectory's
// PreferredExtendedPanId (a declared store).
var keptElsewhere = map[[2]uint32]bool{
	{basicdef.ClusterID, basicdef.AttrNodeLabel}:           true,
	{basicdef.ClusterID, basicdef.AttrLocation}:            true,
	{bdbidef.ClusterID, bdbidef.AttrNodeLabel}:             true,
	{bindingdef.ClusterID, bindingdef.AttrBinding}:         true,
	{userlabeldef.ClusterID, userlabeldef.AttrLabelList}:   true,
	{gkmdef.ClusterID, gkmdef.AttrGroupKeyMap}:             true,
	{otadef.ClusterID, otadef.AttrDefaultOtaProviders}:     true,
	{thermostatdef.ClusterID, thermostatdef.AttrPresets}:   true,
	{thermostatdef.ClusterID, thermostatdef.AttrSchedules}: true,
	{tnddef.ClusterID, tnddef.AttrPreferredExtendedPanId}:  true,
}

// attributeHolder is the endpoint source that says which attributes its
// own servers hold: device.Device.HoldsAttribute.
type attributeHolder interface {
	HoldsAttribute(clusterID, attrID uint32) bool
}

// nonVolatile is the node's one persister of the nonvolatile attributes
// the module holds (owner decision 38): every attribute whose generated
// definition carries the N quality and write access, not fabric-scoped
// and not kept elsewhere (keptElsewhere), of a server the module built
// and holds the state of — the root's own servers (not one the host
// mounts there, Config.RootServers), and on a bridged endpoint
// a server its device built itself and no host port takes the attribute's
// writes of (device.Device.HoldsAttribute). A controller's write is
// stored before the server takes it ([endpoint.WritePersister]); the
// stored value is restored before the bridge serves the endpoint, over
// the declaration's Initial. matter.js persists every nonvolatile
// attribute and every writable or fabric-scoped one
// (RootSupervisor.ts:160-175 at db021da0, persistentKeys).
type nonVolatile struct {
	n *Node
	// mu serialises a stored write: the store write, the server's write
	// and the undo of a refused one ([endpoint.WritePersister]).
	mu sync.Mutex
	// metMu guards met: the servers whose values were restored, so a
	// server met again in a reassembly is not restored over its newer
	// state (persistUserLabels does the same).
	metMu sync.Mutex
	met   map[contract.ClusterServer]bool
}

var _ endpoint.WritePersister = (*nonVolatile)(nil)

// key is the settings key of attrID of srv on ep, false for an attribute
// the persister does not keep.
func (v *nonVolatile) key(ep *endpoint.Endpoint, srv contract.ClusterServer, attrID uint32) (string, bool) {
	clusterID := srv.MatterClusterID()
	if !nonVolatileWritable(clusterID, attrID) {
		return "", false
	}
	// Nothing could restore a value the server takes no setter for.
	if _, ok := srv.(spec.AttributeSetter); !ok {
		return "", false
	}
	switch {
	case ep.IsRoot():
		// The node's own: a server the host mounts there is the host's.
		if !slices.Contains(v.n.root.servers, srv) || slices.Contains(v.n.cfg.RootServers, srv) {
			return "", false
		}
	case ep.IsAggregator():
		// The Aggregator's own servers hold no such attribute but its
		// UserLabel (buildLabels); the rest are the host's.
		return "", false
	default:
		h, ok := ep.Source.(attributeHolder)
		if !ok || !h.HoldsAttribute(clusterID, attrID) {
			return "", false
		}
	}
	return nonVolatileKey(ep, clusterID, attrID), true
}

// nonVolatileWritable reports an attribute of the generated definition
// with the N quality and write access, not fabric-scoped and not kept by
// a store of its own.
func nonVolatileWritable(clusterID, attrID uint32) bool {
	def := spec.Lookup(clusterID)
	if def == nil {
		return false
	}
	a := def.Attribute(attrID)
	if a == nil || !a.Quality.NonVolatile || !a.Writable() || a.Access.Fabric == "F" {
		return false
	}
	return !keptElsewhere[[2]uint32{clusterID, attrID}]
}

// nonVolatileKey is the settings key of clusterID's attrID on ep.
func nonVolatileKey(ep *endpoint.Endpoint, clusterID, attrID uint32) string {
	at, ok := keyString(ep.SourceKey)
	if !ok {
		at = "@" + strconv.FormatUint(uint64(ep.ID), 10)
	}
	return fmt.Sprintf("%s%s.%04x.%04x", nonVolatileKeyRoot, at, clusterID, attrID)
}

// PersistWrite implements [endpoint.WritePersister]: the value as TLV,
// hex-encoded, under the attribute's key; for a bridged endpoint's
// UserLabel list the JSON persistUserLabels restores.
func (v *nonVolatile) PersistWrite(ctx context.Context, ep *endpoint.Endpoint, srv contract.ClusterServer, attrID uint32, value im.AttributeValue) (func(applied bool), error) {
	key, stored, ok := v.storedForm(ep, srv, attrID, value)
	if !ok {
		return nil, nil
	}
	st := v.n.persisted.st
	ctx = context.WithoutCancel(ctx)
	v.mu.Lock()
	prev, had, err := st.GetSetting(ctx, key)
	if err == nil {
		err = st.SetSetting(ctx, key, stored)
	}
	if err != nil {
		v.mu.Unlock()
		v.n.logger.Warn("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
		return nil, fmt.Errorf("node: persist %s: %w", key, err)
	}
	return func(applied bool) {
		defer v.mu.Unlock()
		if applied {
			v.storeCommitted(ctx, srv, attrID, key, stored)
			return
		}
		// The server refused the value: the store holds what it held.
		// An absent key is put back as the empty value, which reads as
		// none.
		if !had {
			prev = ""
		}
		if err := st.SetSetting(ctx, key, prev); err != nil {
			v.n.logger.Warn("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
		}
	}, nil
}

// storeCommitted stores the value srv holds after it committed a write
// stored as stored under key, when it differs: a server may commit a
// normalised form of the written value (WindowCovering clears
// CalibrationMode under MaintenanceMode), and a restart serves what is
// stored. The write already answered, so a refused store write is logged;
// the store then keeps the written value. A bridged LabelList, kept in
// its own form, is not re-read.
func (v *nonVolatile) storeCommitted(ctx context.Context, srv contract.ClusterServer, attrID uint32, key, stored string) {
	if strings.HasPrefix(key, bridgedUserLabelKeyRoot) {
		return
	}
	val, ok := srv.MatterRead(attrID)
	if !ok {
		return
	}
	b, err := bridge.AttributeValueTLV(val)
	if err != nil {
		// A form the codec does not take: the written value stays.
		v.n.logger.Debug("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
		return
	}
	committed := hex.EncodeToString(b)
	if committed == stored {
		return
	}
	if err := v.n.persisted.st.SetSetting(ctx, key, committed); err != nil {
		v.n.logger.Warn("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
	}
}

// storedForm is the key and the settings value of a write the persister
// keeps; false for one it does not.
func (v *nonVolatile) storedForm(ep *endpoint.Endpoint, srv contract.ClusterServer, attrID uint32, value im.AttributeValue) (key, stored string, ok bool) {
	if srv.MatterClusterID() == userlabeldef.ClusterID && attrID == userlabeldef.AttrLabelList && !ep.IsRoot() && !ep.IsAggregator() {
		return v.bridgedUserLabel(ep, value)
	}
	key, ok = v.key(ep, srv, attrID)
	if !ok {
		return "", "", false
	}
	var val any
	if !value.IsNull {
		val = value.Value
	}
	b, err := bridge.AttributeValueTLV(val)
	if err != nil {
		v.n.logger.Warn("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
		return "", "", false
	}
	return key, hex.EncodeToString(b), true
}

// bridgedUserLabel is the key and value of a write of a bridged
// endpoint's LabelList, in the form persistUserLabels restores: stored
// before the write answers, as the root's list is (buildLabels).
func (v *nonVolatile) bridgedUserLabel(ep *endpoint.Endpoint, value im.AttributeValue) (key, stored string, ok bool) {
	k, keyed := keyString(ep.SourceKey)
	if !keyed {
		return "", "", false
	}
	b, err := json.Marshal(value.Value)
	if err != nil {
		// Not a list the server takes either; it refuses the write.
		return "", "", false
	}
	return bridgedUserLabelKeyRoot + k, string(b), true
}

// restore sets every attribute of srv on ep the persister keeps to the
// value stored for it, once per server.
func (v *nonVolatile) restore(ctx context.Context, ep *endpoint.Endpoint, srv contract.ClusterServer) {
	v.metMu.Lock()
	met := v.met[srv]
	if v.met == nil {
		v.met = map[contract.ClusterServer]bool{}
	}
	v.met[srv] = true
	v.metMu.Unlock()
	if met {
		return
	}
	def := spec.Lookup(srv.MatterClusterID())
	if def == nil {
		return
	}
	setter, ok := srv.(spec.AttributeSetter)
	if !ok {
		return
	}
	for i := range def.Attributes {
		a := &def.Attributes[i]
		key, kept := v.key(ep, srv, a.ID)
		if !kept {
			continue
		}
		raw, found, err := v.n.persisted.st.GetSetting(ctx, key)
		if err != nil {
			v.n.logger.Warn("nonvolatile.restore", slog.String("key", key), slog.Any("err", err))
			continue
		}
		if !found || raw == "" {
			continue
		}
		val, err := decodeNonVolatile(a, raw)
		if err == nil {
			err = setter.SetAttributes(map[uint32]any{a.ID: val})
		}
		if err != nil {
			v.n.logger.Warn("nonvolatile.restore", slog.String("key", key), slog.Any("err", err))
		}
	}
}

// persistNonVolatile makes t store a controller's write of a nonvolatile
// attribute the module holds before the server takes it, and restores the
// stored values of every server met for the first time before the bridge
// serves t: the root's (which the bridge mounts on t's root endpoint after
// this) and those of the bridged endpoints.
func (n *Node) persistNonVolatile(ctx context.Context, t *endpoint.Topology) {
	t.Persister = n.nonVolatile
	ctx = context.WithoutCancel(ctx)
	current := map[contract.ClusterServer]bool{}
	for _, ep := range t.Endpoints {
		var servers []contract.ClusterServer
		switch {
		case ep == nil || ep.IsAggregator():
		case ep.IsRoot():
			servers = n.root.servers
		default:
			servers = endpoint.ClusterServers(ep)
		}
		for _, srv := range servers {
			n.nonVolatile.restore(ctx, ep, srv)
			current[srv] = true
		}
	}
	// A server that left the topology is forgotten; one that comes back
	// is restored again, as a new one is.
	n.nonVolatile.metMu.Lock()
	maps.DeleteFunc(n.nonVolatile.met, func(srv contract.ClusterServer, _ bool) bool { return !current[srv] })
	n.nonVolatile.metMu.Unlock()
}

// decodeNonVolatile reads a stored value the way the bridge decodes a
// controller's write of it: a container through the attribute's generated
// decoder, a primitive as its wire type (bridge attributeValueReader).
func decodeNonVolatile(a *spec.Attribute, raw string) (any, error) {
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, err
	}
	el := n.El
	switch {
	case el.IsNull || el.Type == tlv.TypeNull:
		return nil, nil
	case el.IsContainer:
		if a.Decode == nil {
			return nil, fmt.Errorf("node: %s has no generated decoder", a.Name)
		}
		return a.Decode(n)
	}
	switch el.Type {
	case tlv.TypeBoolTrue:
		return true, nil
	case tlv.TypeBoolFalse:
		return false, nil
	case tlv.TypeUTF8Str1, tlv.TypeUTF8Str2, tlv.TypeUTF8Str4, tlv.TypeUTF8Str8:
		return el.String, nil
	case tlv.TypeOctetStr1, tlv.TypeOctetStr2, tlv.TypeOctetStr4, tlv.TypeOctetStr8:
		return slices.Clone(el.Octets), nil
	case tlv.TypeSignedInt1, tlv.TypeSignedInt2, tlv.TypeSignedInt4, tlv.TypeSignedInt8:
		return el.Int, nil
	case tlv.TypeFloat4, tlv.TypeFloat8:
		return el.Float, nil
	default:
		return el.Uint, nil
	}
}
