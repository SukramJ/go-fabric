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
	"unicode/utf8"

	"thingfabric.dev/go-fabric/bridge"
	"thingfabric.dev/go-fabric/cluster/spec"
	basicdef "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	bindingdef "thingfabric.dev/go-fabric/cluster/spec/binding"
	bdbidef "thingfabric.dev/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	gkmdef "thingfabric.dev/go-fabric/cluster/spec/groupkeymanagement"
	otadef "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdaterequestor"
	thermostatdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	tnddef "thingfabric.dev/go-fabric/cluster/spec/threadnetworkdirectory"
	userlabeldef "thingfabric.dev/go-fabric/cluster/spec/userlabel"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/schema"
	"thingfabric.dev/go-fabric/tlv"
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
	// restoring: restore sets the server's stored values; moved: a change
	// was reported meanwhile, which restore stores once all are set.
	restoring, moved bool
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
	// label: a bridged LabelList, kept in persistUserLabels' form;
	// labelSrv: the UserLabel server whose list an owed label key is
	// written from when the node stops (a change or a rollback the store
	// refused).
	label    bool
	labelSrv contract.ClusterServer
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
	return nonVolatileSlotAt(at, clusterID)
}

// nonVolatileSlotAt is clusterID's slot at at, an endpoint's stable key
// (or "@" and its number).
func nonVolatileSlotAt(at string, clusterID uint32) string {
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
	// server that holds it (R2-P2-rr-ctl-01). A label key is the server's
	// whose list persistUserLabels keeps.
	if k.label && !v.n.userLabels.tracks(srv) || !k.label && k.srv != srv {
		k.store.Unlock()
		k.write.Unlock()
		return nil, nil
	}
	prev, had, err := st.GetSetting(ctx, key)
	// A value the store holds already is not written again: the write
	// changes nothing stored (review II, R2-P5-write-04). Nor is one the
	// server holds under a key never stored: a write of the held value
	// changes nothing, which matter.js neither notes nor stores
	// (Datasource.ts:1446-1452, :1476-1481 at db021da0; F-db021da0-47) —
	// a restart serves the same value from the declaration.
	wrote := !had || prev != stored
	if err == nil && !had && !k.label {
		if held, ok := v.heldForm(k); ok && held == stored {
			wrote = false
		}
	}
	if err == nil && wrote {
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
			if k.restoring {
				// A server restore sets in the key's place since: it
				// stores the held value once all of its are set.
				k.moved = true
				return
			}
			v.storeHeld(ctx, k, key)
			return
		}
		if applied {
			return
		}
		// The server refused the value: the store holds what it held.
		// An absent key is put back as the empty value, which reads as
		// none; a value that was not written has nothing to put back.
		if !wrote {
			k.stored = prev
			return
		}
		if !had {
			prev = ""
		}
		if err := st.SetSetting(ctx, key, prev); err != nil {
			v.n.logger.Warn("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
			k.owed = true
			if k.label {
				k.labelSrv = srv
			}
			return
		}
		k.stored = prev
	}, nil
}

// heldForm is the stored form (hex of its TLV) of the value k's server
// holds; false without a server, for an attribute it serves no value of,
// and for a value the codec does not take (logged).
func (v *nonVolatile) heldForm(k *nvKey) (string, bool) {
	if k.srv == nil {
		return "", false
	}
	val, ok := k.srv.MatterRead(k.attrID)
	if !ok {
		return "", false
	}
	b, err := bridge.AttributeValueTLV(val)
	if err != nil {
		v.n.logger.Debug("nonvolatile.persist", slog.Uint64("attribute", uint64(k.attrID)), slog.Any("err", err))
		return "", false
	}
	return hex.EncodeToString(b), true
}

// storeHeld stores the value k's server holds under key when the store
// does not hold it already; a store that refuses it is logged and the key
// owed (written again at the next change and when the node stops). The
// caller holds k.store. A crash before it ran leaves the value stored
// before the change. A label key is [nonVolatile.storeLabel]'s.
func (v *nonVolatile) storeHeld(ctx context.Context, k *nvKey, key string) {
	if k.label {
		v.storeLabel(ctx, k, key)
		return
	}
	held, ok := v.heldForm(k)
	if !ok {
		return
	}
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

// labelChanged stores the list a bridged UserLabel server holds after a
// change of it (persistUserLabels' subscription) under key, under the
// key's store lock as every other nonvolatile key's change: a host's
// change and a controller's write of the list are stored one after the
// other, each as the list the server holds then, so the older list is
// never stored last; a refused store owes the key, written again at the
// next change and when the node stops (review II, R2-P6-store-2416). A
// server the node let go of meanwhile (a forgotten or replaced endpoint)
// stores nothing.
func (v *nonVolatile) labelChanged(ctx context.Context, key string, srv contract.ClusterServer) {
	k := v.keyState(key)
	k.store.Lock()
	defer k.store.Unlock()
	k.labelSrv = srv
	v.storeLabel(ctx, k, key)
}

// storeLabel writes a label key from the list its UserLabel server holds
// (k.labelSrv), while the node keeps that server's list; one it let go of
// (the endpoint left the topology) takes its debt with it, and the server
// in its place stores its own list. A refused store owes the key: a
// change ([nonVolatile.labelChanged]) or a refused rollback. The caller
// holds k.store.
func (v *nonVolatile) storeLabel(ctx context.Context, k *nvKey, key string) {
	srv := k.labelSrv
	if srv == nil {
		return
	}
	if !v.n.userLabels.tracks(srv) {
		k.owed, k.labelSrv = false, nil
		return
	}
	b, err := json.Marshal(userlabeldef.Wrap(srv).Get().LabelList)
	if err == nil {
		err = v.n.persisted.st.SetSetting(ctx, key, string(b))
	}
	if err != nil {
		v.n.logger.Warn("nonvolatile.persist", slog.String("key", key), slog.Any("err", err))
		k.owed = true
		return
	}
	k.stored, k.known, k.owed, k.labelSrv = string(b), true, false, nil
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
		switch {
		case a.k.restoring:
			a.k.moved = true
		case a.k.writing:
			a.k.dirty = true
		default:
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
		a.k.restoring = true
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
		val, cut, err := decodeNonVolatile(def.Attribute(id), raw)
		if cut > 0 {
			// The value is not logged: the generated model does not carry
			// matter.js's isSecret, so a secret container cannot be told
			// apart and redacted as matter.js redacts it (Datasource.ts:156-163).
			v.n.logger.Warn("nonvolatile.restore", slog.String("key", a.key),
				slog.String("what", "stored strings shortened to their bound in UTF-8 bytes"), slog.Int("count", cut))
		}
		if err != nil {
			v.n.logger.Warn("nonvolatile.restore", slog.String("key", a.key), slog.Any("err", err))
			continue
		}
		restored[id] = val
	}
	// Subscribed before the values are set, so a companion the restore
	// moves is stored as well — once every value is set: a value set on
	// the way, a companion moved before its own stored value is restored,
	// is never stored over that value (R2-P2-rr-ctl-04).
	unsubscribe := spec.ChangesOf(srv, func(c contract.Change) { v.changed(ctx, keys, c) })
	v.mu.Lock()
	stopped := v.stopped
	if !stopped {
		rec.unsubscribe = unsubscribe
	}
	v.mu.Unlock()
	defer v.restored(ctx, keys, stopped)
	if stopped {
		unsubscribe()
		return
	}
	v.setRestored(srv, keys, restored)
}

// setRestored sets each of restored on srv, in attribute id order.
func (v *nonVolatile) setRestored(srv contract.ClusterServer, keys map[uint32]nvAttr, restored map[uint32]any) {
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

// restored ends restore's setting of keys: each one a change was reported
// for meanwhile stores the value its server now holds, unless the node
// stopped.
func (v *nonVolatile) restored(ctx context.Context, keys map[uint32]nvAttr, stopped bool) {
	for _, id := range slices.Sorted(maps.Keys(keys)) {
		a := keys[id]
		a.k.store.Lock()
		moved := a.k.moved
		a.k.restoring, a.k.moved = false, false
		if moved && !stopped {
			v.storeHeld(ctx, a.k, a.key)
		}
		a.k.store.Unlock()
	}
}

// forget lets go of the servers met at the endpoint keys of a forgotten
// endpoint ([Aggregator.Forget]), which has left for good, and erases
// what the store keeps for those keys: the servers' change subscriptions
// and their UserLabel lists' end, the persister references them no
// longer, and every nonvolatile value and every setting kept under one of
// the keys ([keyedSettingRoots]: the NodeLabel, ConfigurationVersion,
// scene table, Binding list, UserLabel list and surface) is erased, so a
// device Added under such a key afterwards starts as a new one would —
// from its declaration's Initial, label and lists. matter.js erases the
// persisted data of an endpoint it deletes
// (packages/node/src/endpoint/Endpoint.ts:984-992 at db021da0). A write
// that reaches a released server is not stored. A removed endpoint
// ([Aggregator.Remove]) is not forgotten: its servers keep their state,
// subscription and stored values for the Add that brings it back. The
// error is the store's; a refused erasure leaves every value stored.
func (v *nonVolatile) forget(ctx context.Context, endpointKeys []string) error {
	ctx = context.WithoutCancel(ctx)
	labelKeys := make([]string, 0, len(endpointKeys))
	for _, key := range endpointKeys {
		labelKeys = append(labelKeys, bridgedUserLabelKeyRoot+key)
	}
	// Released before the keys are read: a label write that finds its
	// key's state only after this is not stored (PersistWrite).
	v.n.userLabels.release(labelKeys)
	v.mu.Lock()
	var unsubscribes []func()
	for slot, srv := range v.slots {
		if !slotOfAny(slot, endpointKeys) {
			continue
		}
		if rec := v.servers[srv]; rec != nil {
			unsubscribes = append(unsubscribes, rec.unsubscribe)
		}
		delete(v.servers, srv)
		delete(v.slots, slot)
	}
	known := map[string]*nvKey{}
	for key, k := range v.keys {
		if k.label && slices.Contains(labelKeys, key) || !k.label && keyOfAny(key, endpointKeys) {
			known[key] = k
		}
	}
	v.mu.Unlock()
	for _, unsubscribe := range unsubscribes {
		unsubscribe()
	}
	return v.erase(ctx, slices.Concat(nonVolatileCandidates(endpointKeys), keyedSettingKeys(endpointKeys)), known)
}

// erase deletes every key of candidates and of known in one store write
// (store.Settings DeleteSettings), reading none of them. known are the
// states of the forgotten keys the persister holds: each is held, and its
// server let go of, so no write or change stores it meanwhile.
func (v *nonVolatile) erase(ctx context.Context, candidates []string, known map[string]*nvKey) error {
	held := slices.Sorted(maps.Keys(known))
	for _, key := range held {
		k := known[key]
		k.store.Lock()
		k.srv, k.labelSrv, k.owed = nil, nil, false
	}
	defer func() {
		for _, key := range held {
			known[key].store.Unlock()
		}
	}()
	keys := slices.Compact(slices.Sorted(slices.Values(slices.Concat(candidates, held))))
	if err := v.n.persisted.st.DeleteSettings(ctx, keys...); err != nil {
		v.n.logger.Warn("nonvolatile.forget", slog.Int("keys", len(keys)), slog.Any("err", err))
		return err
	}
	for _, key := range held {
		known[key].stored, known[key].known = "", true
	}
	return nil
}

// nonVolatileCandidates are the settings keys the persister keeps a value
// under at endpointKeys: every attribute it keeps of every cluster this
// build defines (schema.ClusterNames with a registered definition): the
// keys an erasure deletes. A prefix delete would take the keys of a stable
// key that extends a forgotten one by a dot and a hex segment
// ([slotOfAny]), so the erasure names its keys.
func nonVolatileCandidates(endpointKeys []string) []string {
	var keys []string
	for _, clusterID := range slices.Sorted(maps.Keys(schema.ClusterNames)) {
		def := spec.Lookup(clusterID)
		if def == nil {
			continue
		}
		for i := range def.Attributes {
			if !nonVolatileWritable(clusterID, def.Attributes[i].ID) {
				continue
			}
			for _, at := range endpointKeys {
				keys = append(keys, fmt.Sprintf("%s.%04x", nonVolatileSlotAt(at, clusterID), def.Attributes[i].ID))
			}
		}
	}
	return keys
}

// keyOfAny reports whether key ([nonVolatileKey]) is an attribute's key
// in a cluster's slot at one of endpointKeys ([slotOfAny]).
func keyOfAny(key string, endpointKeys []string) bool {
	i := strings.LastIndexByte(key, '.')
	return i > 0 && slotOfAny(key[:i], endpointKeys)
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
// First every string in it longer than its numeric maximum in UTF-8 bytes
// is cut to it ([shortenStored]); cut counts them. The cut value is what
// the server is set to, and the store holds it once the restore or a later
// change stores the attribute's value; until then each restore cuts again.
func decodeNonVolatile(a *spec.Attribute, raw string) (val any, cut int, err error) {
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, 0, err
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, 0, err
	}
	n, cut = shortenStored(a.Type, a.Constraint, nil, n)
	el := n.El
	switch {
	case el.IsNull || el.Type == tlv.TypeNull:
		return nil, cut, nil
	case el.IsContainer:
		if a.Decode == nil {
			return nil, cut, fmt.Errorf("node: %s has no generated decoder", a.Name)
		}
		val, err = a.Decode(n)
		return val, cut, err
	}
	switch el.Type {
	case tlv.TypeBoolTrue:
		return true, cut, nil
	case tlv.TypeBoolFalse:
		return false, cut, nil
	case tlv.TypeUTF8Str1, tlv.TypeUTF8Str2, tlv.TypeUTF8Str4, tlv.TypeUTF8Str8:
		return el.String, cut, nil
	case tlv.TypeOctetStr1, tlv.TypeOctetStr2, tlv.TypeOctetStr4, tlv.TypeOctetStr8:
		return slices.Clone(el.Octets), cut, nil
	case tlv.TypeSignedInt1, tlv.TypeSignedInt2, tlv.TypeSignedInt4, tlv.TypeSignedInt8:
		return el.Int, cut, nil
	case tlv.TypeFloat4, tlv.TypeFloat8:
		return el.Float, cut, nil
	default:
		return el.Uint, cut, nil
	}
}

// shortenStored cuts every character string in n, a stored value of type
// t under constraint c, that is longer than its numeric maximum in UTF-8
// bytes to that maximum, between two runes, and returns n with them cut
// and their number. A string's maximum is the smaller of its own and the
// entry maximum of the list that holds it (listMax, "max 10[max 32]");
// strings in lists and structs are cut too; a bound that is not a number
// cuts nothing. A value stored while the bound counted UTF-16 code units
// can be longer than it now is. Mirrors matter.js
// packages/node/src/behavior/state/managed/Datasource.ts:124-220 at
// b25ed960 (shortenStoredStrings, Bytes.truncateUtf8, #4995), applied to
// stored values at load (:656-661).
func shortenStored(t spec.Type, c spec.Constraint, listMax *spec.Bound, n spec.Node) (out spec.Node, cut int) {
	switch t.Kind { //nolint:exhaustive // only strings are cut; lists and structs are walked for them
	case spec.KindString:
		if n.El.IsContainer || n.El.Type < tlv.TypeUTF8Str1 || n.El.Type > tlv.TypeUTF8Str8 {
			return n, 0
		}
		limit := -1
		for _, b := range []*spec.Bound{c.Max, listMax} {
			if b.IsInt() && (limit < 0 || int(b.Int) < limit) {
				limit = int(b.Int)
			}
		}
		if limit < 0 || len(n.El.String) <= limit {
			return n, 0
		}
		end := limit
		for end > 0 && !utf8.RuneStart(n.El.String[end]) {
			end--
		}
		n.El.String, n.El.RawLength = n.El.String[:end], 0
		return n, 1
	case spec.KindList:
		if t.Entry == nil || !n.El.IsContainer {
			return n, 0
		}
		var entryMax *spec.Bound
		if c.Entry != nil {
			entryMax = c.Entry.Max
		}
		return shortenChildren(n, func(spec.Node) (spec.Type, spec.Constraint, *spec.Bound, bool) {
			return t.Entry.Type, t.Entry.Constraint, entryMax, true
		})
	case spec.KindStruct:
		if t.Struct == nil || !n.El.IsContainer {
			return n, 0
		}
		return shortenChildren(n, func(ch spec.Node) (spec.Type, spec.Constraint, *spec.Bound, bool) {
			if ch.El.Tag.Kind != tlv.TagKindContext {
				return spec.Type{}, spec.Constraint{}, nil, false
			}
			for i := range t.Struct.Fields {
				if f := &t.Struct.Fields[i]; f.ID == ch.El.Tag.Number {
					return f.Type, f.Constraint, nil, true
				}
			}
			return spec.Type{}, spec.Constraint{}, nil, false
		})
	}
	return n, 0
}

// shortenChildren is [shortenStored] over a container's children, each
// under the type, constraint and list maximum of returns; n's children are
// copied before the first one changes.
func shortenChildren(n spec.Node, of func(spec.Node) (spec.Type, spec.Constraint, *spec.Bound, bool)) (out spec.Node, cut int) {
	total := 0
	for i, ch := range n.Children {
		t, c, listMax, ok := of(ch)
		if !ok {
			continue
		}
		short, k := shortenStored(t, c, listMax, ch)
		if k == 0 {
			continue
		}
		if total == 0 {
			n.Children = slices.Clone(n.Children)
		}
		n.Children[i] = short
		total += k
	}
	return n, total
}

// shortenStoredLabels is [shortenStored] for a UserLabel LabelList a
// store of its own keeps as JSON (buildLabels, Node.userLabel): a Label or
// Value stored while "max 16" counted UTF-16 code units is cut to 16 bytes
// in UTF-8, so the list restores — a list over its bound would fail the
// server's construction or be refused by its setter. A list within its
// bounds comes back as it was; a cut is logged without the values (see
// decodeNonVolatile's caller).
func shortenStoredLabels(logger *slog.Logger, key string, labels []userlabeldef.LabelStruct) []userlabeldef.LabelStruct {
	a := userlabeldef.Definition.Attribute(userlabeldef.AttrLabelList)
	b, err := bridge.AttributeValueTLV(spec.List[userlabeldef.LabelStruct](labels))
	if err != nil {
		return labels
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return labels
	}
	n, cut := shortenStored(a.Type, a.Constraint, nil, n)
	if cut == 0 {
		return labels
	}
	v, err := a.Decode(n)
	list, ok := v.(spec.List[userlabeldef.LabelStruct])
	if err != nil || !ok {
		return labels
	}
	logger.Warn("label.restore", slog.String("key", key),
		slog.String("what", "stored strings shortened to their bound in UTF-8 bytes"), slog.Int("count", cut))
	return list
}
