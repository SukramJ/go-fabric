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
// writes of (device.Device.HoldsAttribute). Every change of such an
// attribute is stored, whatever made it: a controller's write before the
// server takes it ([endpoint.WritePersister]), a command, a companion the
// server moves or the host's Set after the server committed it, from the
// server's own change notification (spec.ChangesOf), as the value the
// server then holds. The stored value is restored before the bridge
// serves the endpoint, over the declaration's Initial, once per server.
// matter.js persists every nonvolatile attribute and every writable or
// fabric-scoped one (RootSupervisor.ts:160-175 at db021da0,
// persistentKeys), each change whoever made it (Datasource.ts:1442-1454,
// handed to store.set at :1261).
type nonVolatile struct {
	n *Node
	// mu guards the maps and stopped; it is never held across a store
	// call or a server's code.
	mu sync.Mutex
	// keys are the state of each settings key the persister wrote or
	// restored; an entry is never removed, so a hook keeps its pointer.
	keys map[string]*nvKey
	// servers are the servers met, each restored once: one met again
	// after it left the topology keeps its state and its subscription.
	servers map[contract.ClusterServer]*nvServer
	// slots are the server met last for each endpoint key and cluster
	// (nonVolatileSlot): another server in its place releases it.
	slots   map[string]contract.ClusterServer
	stopped bool
}

// nvKey is the persister's state of one settings key.
type nvKey struct {
	// write serialises a controller's writes of the attribute: the store
	// write, the server's write (the host's veto included) and the
	// settlement ([endpoint.WritePersister]); a write of another
	// attribute does not wait for it.
	write sync.Mutex
	// store guards the fields below and each store call of the key; it
	// is held across no server write and no host code.
	store sync.Mutex
	// writing: a controller's write is between PersistWrite and done;
	// dirty: a change was reported meanwhile, which done stores.
	writing, dirty bool
	// stored is what the store holds for the key, when known; owed: the
	// store refused the held value, written again at the next change
	// and when the node stops.
	stored      string
	known, owed bool
	// srv and attrID are where the held value is read: the server restore
	// met last in the key's place, nil once forgotten; only restore and
	// forget set them.
	srv    contract.ClusterServer
	attrID uint32
	// label: a bridged LabelList, kept in persistUserLabels' form.
	label bool
}

// nvServer is a server met: its change subscription.
type nvServer struct {
	unsubscribe func()
}

// keyState is key's state, made on first use.
func (v *nonVolatile) keyState(key string) *nvKey {
	v.mu.Lock()
	defer v.mu.Unlock()
	k := v.keys[key]
	if k == nil {
		if v.keys == nil {
			v.keys = map[string]*nvKey{}
		}
		k = &nvKey{label: strings.HasPrefix(key, bridgedUserLabelKeyRoot)}
		v.keys[key] = k
	}
	return k
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
	return fmt.Sprintf("%s.%04x", nonVolatileSlot(ep, clusterID), attrID)
}

// nonVolatileSlot is the part of clusterID's keys on ep before the
// attribute: one server holds it at a time.
func nonVolatileSlot(ep *endpoint.Endpoint, clusterID uint32) string {
	at, ok := keyString(ep.SourceKey)
	if !ok {
		at = "@" + strconv.FormatUint(uint64(ep.ID), 10)
	}
	return fmt.Sprintf("%s%s.%04x", nonVolatileKeyRoot, at, clusterID)
}

// PersistWrite implements [endpoint.WritePersister]: the value as TLV,
// hex-encoded, under the attribute's key; for a bridged endpoint's
// UserLabel list the JSON persistUserLabels restores. Only the writes of
// the same attribute wait for one another. After the server applied the
// write, done stores the value the server holds when it differs (a server
// may commit a normalised form: WindowCovering clears CalibrationMode
// under MaintenanceMode), or one a change reported meanwhile. A write
// that reaches a server no longer in the key's place is not stored.
func (v *nonVolatile) PersistWrite(ctx context.Context, ep *endpoint.Endpoint, srv contract.ClusterServer, attrID uint32, value im.AttributeValue) (func(applied bool), error) {
	key, stored, ok := v.storedForm(ep, srv, attrID, value)
	if !ok {
		return nil, nil
	}
	st := v.n.persisted.st
	ctx = context.WithoutCancel(ctx)
	k := v.keyState(key)
	k.write.Lock()
	k.store.Lock()
	// The key is the server's that restore met last in its place (k.srv,
	// nil once forgotten): a write that reaches a server released since —
	// a Device the host replaced or forgot, answering on the topology the
	// write was resolved on — is not stored, and the key stays with the
	// server that holds it (R2-P2-rr-ctl-01).
	if !k.label && k.srv != srv {
		k.store.Unlock()
		k.write.Unlock()
		return nil, nil
	}
	prev, had, err := st.GetSetting(ctx, key)
	if err == nil {
		err = st.SetSetting(ctx, key, stored)
	}
	if err != nil {
		k.store.Unlock()
		k.write.Unlock()
		v.n.logger.Warn("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
		return nil, fmt.Errorf("node: persist %s: %w", key, err)
	}
	k.stored, k.known, k.owed = stored, true, false
	k.writing, k.dirty = true, false
	k.store.Unlock()
	return func(applied bool) {
		defer k.write.Unlock()
		k.store.Lock()
		defer k.store.Unlock()
		dirty := k.dirty
		k.writing, k.dirty = false, false
		if !k.label && (applied || dirty) {
			v.storeHeld(ctx, k, key)
			return
		}
		if applied {
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
			k.owed = true
			return
		}
		k.stored = prev
	}, nil
}

// storeHeld stores the value k's server holds under key when the store
// does not hold it already; a store that refuses it is logged and the key
// owed (written again at the next change and when the node stops). The
// caller holds k.store. A crash before it ran leaves the value stored
// before the change.
func (v *nonVolatile) storeHeld(ctx context.Context, k *nvKey, key string) {
	if k.label || k.srv == nil {
		return
	}
	val, ok := k.srv.MatterRead(k.attrID)
	if !ok {
		return
	}
	b, err := bridge.AttributeValueTLV(val)
	if err != nil {
		// A form the codec does not take: what is stored stays.
		v.n.logger.Debug("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
		return
	}
	held := hex.EncodeToString(b)
	if k.known && !k.owed && held == k.stored {
		return
	}
	if err := v.n.persisted.st.SetSetting(ctx, key, held); err != nil {
		v.n.logger.Warn("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
		k.owed = true
		return
	}
	k.stored, k.known, k.owed = held, true, false
}

// changed stores each attribute of keys that c reports, from the server's
// change notification: a controller's write in progress of the attribute
// stores it in its done.
func (v *nonVolatile) changed(ctx context.Context, keys map[uint32]nvAttr, c contract.Change) {
	for _, id := range c.Attributes {
		a, ok := keys[id]
		if !ok {
			continue
		}
		a.k.store.Lock()
		if a.k.writing {
			a.k.dirty = true
		} else {
			v.storeHeld(ctx, a.k, a.key)
		}
		a.k.store.Unlock()
	}
}

// nvAttr is one attribute of a server met: its key and the key's state.
type nvAttr struct {
	key string
	k   *nvKey
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
// value stored for it and stores each later change of them, once per
// server: a server met before — also one that left the topology and came
// back — keeps the state it holds. A different server in the place of one
// met before (the same endpoint key and cluster) releases that one.
func (v *nonVolatile) restore(ctx context.Context, ep *endpoint.Endpoint, srv contract.ClusterServer) {
	def := spec.Lookup(srv.MatterClusterID())
	if def == nil {
		return
	}
	keys := map[uint32]nvAttr{}
	for i := range def.Attributes {
		if key, kept := v.key(ep, srv, def.Attributes[i].ID); kept {
			keys[def.Attributes[i].ID] = nvAttr{key: key, k: v.keyState(key)}
		}
	}
	if len(keys) == 0 {
		return
	}
	slot := nonVolatileSlot(ep, srv.MatterClusterID())
	v.mu.Lock()
	if _, met := v.servers[srv]; met || v.stopped {
		v.mu.Unlock()
		return
	}
	var release func()
	if old, ok := v.slots[slot]; ok {
		release = v.servers[old].unsubscribe
		delete(v.servers, old)
	}
	rec := &nvServer{unsubscribe: func() {}}
	if v.servers == nil {
		v.servers, v.slots = map[contract.ClusterServer]*nvServer{}, map[string]contract.ClusterServer{}
	}
	v.servers[srv], v.slots[slot] = rec, srv
	v.mu.Unlock()
	if release != nil {
		release()
	}
	restored := map[uint32]any{}
	for _, id := range slices.Sorted(maps.Keys(keys)) {
		a := keys[id]
		raw, found, err := v.n.persisted.st.GetSetting(ctx, a.key)
		a.k.store.Lock()
		a.k.srv, a.k.attrID = srv, id
		if err == nil {
			a.k.stored, a.k.known = raw, true
		}
		a.k.store.Unlock()
		if err != nil {
			v.n.logger.Warn("nonvolatile.restore", slog.String("key", a.key), slog.Any("err", err))
			continue
		}
		if !found || raw == "" {
			continue
		}
		val, err := decodeNonVolatile(def.Attribute(id), raw)
		if err != nil {
			v.n.logger.Warn("nonvolatile.restore", slog.String("key", a.key), slog.Any("err", err))
			continue
		}
		restored[id] = val
	}
	// Subscribed before the values are set: a companion the restore moves
	// is stored as well.
	unsubscribe := spec.ChangesOf(srv, func(c contract.Change) { v.changed(ctx, keys, c) })
	v.mu.Lock()
	stopped := v.stopped
	if !stopped {
		rec.unsubscribe = unsubscribe
	}
	v.mu.Unlock()
	if stopped {
		unsubscribe()
		return
	}
	setter, ok := srv.(spec.AttributeSetter)
	if !ok {
		return
	}
	for _, id := range slices.Sorted(maps.Keys(restored)) {
		if err := setter.SetAttributes(map[uint32]any{id: restored[id]}); err != nil {
			v.n.logger.Warn("nonvolatile.restore", slog.String("key", keys[id].key), slog.Any("err", err))
		}
	}
}

// forget lets go of the servers met at the endpoint keys of a forgotten
// endpoint ([Aggregator.Forget]), which has left for good: their change
// subscriptions end, a key the store refused a change of is written once
// more, and the persister references them no longer. A server met at
// such a key later is restored as a new one. A removed endpoint
// ([Aggregator.Remove]) is not forgotten: its servers keep their state
// and subscription for the Add that brings it back.
func (v *nonVolatile) forget(ctx context.Context, endpointKeys []string) {
	ctx = context.WithoutCancel(ctx)
	v.mu.Lock()
	var (
		released     []contract.ClusterServer
		unsubscribes []func()
	)
	for slot, srv := range v.slots {
		if !slotOfAny(slot, endpointKeys) {
			continue
		}
		if rec := v.servers[srv]; rec != nil {
			unsubscribes = append(unsubscribes, rec.unsubscribe)
		}
		delete(v.servers, srv)
		delete(v.slots, slot)
		released = append(released, srv)
	}
	var keys map[string]*nvKey
	if len(released) > 0 {
		keys = maps.Clone(v.keys)
	}
	v.mu.Unlock()
	for _, unsubscribe := range unsubscribes {
		unsubscribe()
	}
	for key, k := range keys {
		k.store.Lock()
		if k.srv != nil && slices.Contains(released, k.srv) {
			if k.owed {
				v.storeHeld(ctx, k, key)
			}
			k.srv = nil
		}
		k.store.Unlock()
	}
}

// slotOfAny reports whether slot ([nonVolatileSlot]) is a cluster's slot
// at one of endpointKeys: the key, a dot and the cluster id in hex, so a
// key that extends another by a dot is not taken for it.
func slotOfAny(slot string, endpointKeys []string) bool {
	for _, key := range endpointKeys {
		rest, ok := strings.CutPrefix(slot, nonVolatileKeyRoot+key+".")
		if !ok || rest == "" || strings.Contains(rest, ".") {
			continue
		}
		if _, err := strconv.ParseUint(rest, 16, 32); err == nil {
			return true
		}
	}
	return false
}

// stop ends the persister: no change is stored from here on, and every
// key the store refused a change of is written once more.
func (v *nonVolatile) stop(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	v.mu.Lock()
	v.stopped = true
	unsubscribes := make([]func(), 0, len(v.servers))
	for _, rec := range v.servers {
		unsubscribes = append(unsubscribes, rec.unsubscribe)
	}
	keys := maps.Clone(v.keys)
	v.mu.Unlock()
	for _, unsubscribe := range unsubscribes {
		unsubscribe()
	}
	for key, k := range keys {
		k.store.Lock()
		if k.owed {
			v.storeHeld(ctx, k, key)
		}
		k.store.Unlock()
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
		}
	}
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
