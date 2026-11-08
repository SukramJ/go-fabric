// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package brdiscovery is a passive registry of the Thread border routers
// on the LAN, discovered over DNS-SD: a port of matter.js
// packages/thread-br-client/src/discovery/BorderRouterRegistry.ts at
// db021da0 for the fields a network topology needs. It watches the
// `_meshcop._udp.local` and `_trel._udp.local` instances the controller's
// DNS-SD cache (client/discovery) admits, keys each border router by its
// extended address — the TXT `xa`, 8 bytes, or for a TREL instance the
// instance name (Thread §11.4.2) — and keeps its extended PAN id (`xp`),
// network name (`nn`), vendor (`vn`), model (`mn`), host name (the SRV
// target) and when it was last seen. controller/topology classifies a
// Thread neighbour by it; controller.Controller keeps one per DNS-SD cache
// (controller.Config.BorderRouters).
//
// Left out of matter.js's registry: the addresses and ports of a border
// router, the MeshCoP TXT fields beyond the five above (tv, sv, rv, the
// border-agent id, the state bitmap, the active timestamp, the partition
// id, the domain name), the instance-observer cap (the cache bounds what
// it admits) and the start/stop lifecycle (a [Registry] runs from [New]
// to [Registry.Close]). The registry joins no Thread network and speaks
// no MeshCoP; thread/otbr stays the border router's REST client.
package brdiscovery

import (
	"encoding/hex"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/mdns"
)

// StaleRetention is how long an entry no instance carries any more stays
// listed after it was last seen; pruning is lazy, on [Registry.List],
// [Registry.Get] and a newly discovered instance. Mirrors matter.js
// packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:28-33
// (STALE_RETENTION, 24 hours) at db021da0.
const StaleRetention = 24 * time.Hour

// maxEntries caps the registry; a new border router past it evicts the
// oldest stale entry, else the oldest live one. Mirrors matter.js
// packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:23
// (REGISTRY_MAX_ENTRIES) and :565-610 (#evictOldest) at db021da0.
const maxEntries = 256

// The service suffixes the registry watches. Mirrors matter.js
// packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:34-37 at
// db021da0.
const (
	meshcopType   = "_meshcop._udp.local"
	trelType      = "_trel._udp.local"
	meshcopSuffix = "." + meshcopType
	trelSuffix    = "." + trelType
)

// Entry is one border router — matter.js BorderRouterEntry
// (packages/thread-br-client/src/discovery/BorderRouterEntry.ts:15-27 at
// db021da0) for the fields a topology reads.
type Entry struct {
	// ExtAddress is the border router's 64-bit Thread extended address,
	// 16 upper-case hex digits: the key, the value a commissioned node's
	// neighbour table lists.
	ExtAddress string
	// ExtPanID is the extended PAN id, 16 upper-case hex digits; "" while
	// unknown.
	ExtPanID string
	// NetworkName, VendorName and ModelName are the `_meshcop` TXT nn, vn
	// and mn; "" while unknown.
	NetworkName, VendorName, ModelName string
	// HostName is the SRV target as cached ("kitchen.local"); "" while
	// unknown.
	HostName string
	// LastSeen is when an instance last installed or updated the entry.
	LastSeen time.Time
}

// EventKind is what happened to an entry.
type EventKind uint8

// The event kinds — matter.js's events.added, updated and removed
// (packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:73-77 at
// db021da0).
const (
	// EventAdded: a border router entered the registry.
	EventAdded EventKind = iota + 1
	// EventUpdated: an instance of a known border router was installed or
	// changed — fired on every such change, whether a field moved or not.
	EventUpdated
	// EventRemoved: the entry was pruned (stale past [StaleRetention]) or
	// evicted.
	EventRemoved
)

// Event is one change of the registry, with the entry as it stood.
type Event struct {
	Kind  EventKind
	Entry Entry
}

// Config configures a [Registry]; the zero value is the system clock and
// slog.Default().
type Config struct {
	// Clock stamps LastSeen and paces the stale cutoff; nil is the system
	// clock. The controller passes its own.
	Clock clock.Clock
	// Logger receives the registry's debug lines; nil is slog.Default().
	Logger *slog.Logger
}

// Registry is the passive border-router registry. Its methods are safe
// for concurrent use.
type Registry struct {
	clk  clock.Clock
	log  *slog.Logger
	stop func()

	mu        sync.Mutex
	entries   map[string]*record // by ExtAddress
	order     []string           // ExtAddress in insertion order
	instances map[string]*instance
	evicted   map[string]bool // instance qnames (lower) ignored until gone
	observers map[int]func(Event)
	nextObs   int
	pending   []Event
	emitting  bool
	closed    bool
}

// record is an entry with the instance sources that carry it.
type record struct {
	Entry
	sources []string // "meshcop" before "trel"
}

// instance is one watched `_meshcop` or `_trel` instance name.
type instance struct {
	source string
	xa     string // the entry it contributed to, "" before one
}

// watcher is what the registry needs of a DNS-SD cache: *discovery.Names.
type watcher interface {
	Watch(suffixes []string, fn func(discovery.NameState)) (stop func())
}

// New starts a registry on names: it watches the border routers'
// instances and asks the LAN once for both service types, as matter.js's
// start does (packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:121-136
// at db021da0). The cache must admit the two types — client/discovery
// admits them by default.
func New(names *discovery.Names, cfg Config) *Registry {
	r := newRegistry(names, cfg)
	for _, t := range []string{meshcopType, trelType} {
		names.Solicitor().Solicit(discovery.Solicitation{Name: names.Get(t), RecordTypes: []mdns.DNSType{discovery.TypePTR}})
	}
	return r
}

func newRegistry(w watcher, cfg Config) *Registry {
	r := &Registry{
		clk: clock.Or(cfg.Clock), log: cfg.Logger,
		entries: map[string]*record{}, instances: map[string]*instance{},
		evicted: map[string]bool{}, observers: map[int]func(Event){},
	}
	if r.log == nil {
		r.log = slog.Default()
	}
	r.stop = w.Watch([]string{meshcopSuffix, trelSuffix}, r.observe)
	return r
}

// Close stops watching and clears the registry; nothing is told
// afterwards. It is idempotent.
func (r *Registry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.entries, r.order = map[string]*record{}, nil
	clear(r.instances)
	clear(r.observers)
	r.pending = nil
	r.mu.Unlock()
	r.stop()
}

// List is every entry in the order it was first seen, after pruning the
// stale ones. Mirrors matter.js BorderRouterRegistry.ts:170-173.
func (r *Registry) List() []Entry {
	r.mu.Lock()
	r.pruneLocked()
	out := make([]Entry, 0, len(r.order))
	for _, xa := range r.order {
		out = append(out, r.entries[xa].Entry)
	}
	r.mu.Unlock()
	r.deliver()
	return out
}

// Get is the entry of extAddress, matched ignoring case, after pruning
// the stale ones. Mirrors matter.js BorderRouterRegistry.ts:175-179.
func (r *Registry) Get(extAddress string) (Entry, bool) {
	r.mu.Lock()
	r.pruneLocked()
	rec, ok := r.entries[strings.ToUpper(extAddress)]
	var e Entry
	if ok {
		e = rec.Entry
	}
	r.mu.Unlock()
	r.deliver()
	return e, ok
}

// OnChange calls fn with every event until the returned function is
// called. fn runs one call at a time, on the goroutine that caused the
// event — the DNS-SD cache's for an instance, a caller of List or Get for
// a prune; it may call List and Get, and must not block.
func (r *Registry) OnChange(fn func(Event)) (unsubscribe func()) {
	r.mu.Lock()
	id := r.nextObs
	r.nextObs++
	if !r.closed {
		r.observers[id] = fn
	}
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.observers, id)
			r.mu.Unlock()
		})
	}
}

// emitLocked queues an event for delivery.
func (r *Registry) emitLocked(kind EventKind, rec *record) {
	if len(r.observers) > 0 {
		r.pending = append(r.pending, Event{Kind: kind, Entry: rec.Entry})
	}
}

// deliver hands the queued events to the observers, one at a time; a
// delivery already running (an observer's own List) takes the rest.
func (r *Registry) deliver() {
	r.mu.Lock()
	if r.emitting {
		r.mu.Unlock()
		return
	}
	r.emitting = true
	for len(r.pending) > 0 {
		ev := r.pending[0]
		r.pending = r.pending[1:]
		ids := make([]int, 0, len(r.observers))
		for id := range r.observers {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		fns := make([]func(Event), 0, len(ids))
		for _, id := range ids {
			fns = append(fns, r.observers[id])
		}
		r.mu.Unlock()
		for _, fn := range fns {
			fn(ev)
		}
		r.mu.Lock()
	}
	r.emitting = false
	r.mu.Unlock()
}

// pruneLocked drops every entry no instance carries whose LastSeen is
// older than [StaleRetention]. Mirrors matter.js
// packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:181-193
// (#pruneExpired) at db021da0.
func (r *Registry) pruneLocked() {
	cutoff := r.clk.Now().Add(-StaleRetention)
	for _, xa := range slices.Clone(r.order) {
		rec := r.entries[xa]
		if len(rec.sources) == 0 && rec.LastSeen.Before(cutoff) {
			r.removeLocked(xa)
		}
	}
}

// removeLocked deletes an entry and tells the observers.
func (r *Registry) removeLocked(xa string) {
	rec := r.entries[xa]
	delete(r.entries, xa)
	r.order = slices.DeleteFunc(r.order, func(k string) bool { return k == xa })
	r.emitLocked(EventRemoved, rec)
}

// observe is the cache's report of one instance.
func (r *Registry) observe(st discovery.NameState) {
	r.mu.Lock()
	if !r.closed {
		r.observeLocked(st)
	}
	r.mu.Unlock()
	r.deliver()
}

func (r *Registry) observeLocked(st discovery.NameState) {
	key := strings.ToLower(st.QName)
	source := "meshcop"
	if strings.HasSuffix(key, trelSuffix) {
		source = "trel"
	}
	if !st.Discovered {
		r.goneLocked(key, source)
		return
	}
	if r.evicted[key] {
		return
	}
	inst, known := r.instances[key]
	if !known {
		// A newly discovered instance. Mirrors matter.js
		// BorderRouterRegistry.ts:204-240 (#onDiscovered).
		r.pruneLocked()
		inst = &instance{source: source}
		r.instances[key] = inst
	}
	r.upsertLocked(st, inst)
}

// goneLocked is an instance whose last record went: its source no longer
// carries the entry, which keeps its fields and its LastSeen. Mirrors
// matter.js packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:280-303
// (#onInstanceChanged) at db021da0.
func (r *Registry) goneLocked(key, source string) {
	delete(r.evicted, key)
	inst, ok := r.instances[key]
	if !ok {
		return
	}
	delete(r.instances, key)
	if rec, ok := r.entries[inst.xa]; ok && inst.xa != "" {
		rec.sources = slices.DeleteFunc(rec.sources, func(s string) bool { return s == source })
	}
}

// upsertLocked parses an instance into its entry. Mirrors matter.js
// packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:329-476
// (#parseAndUpsert) at db021da0 for the entry's fields: the key from xa
// (8 bytes) or a TREL instance's name (:335-346); xp (8 bytes) and the SRV
// target from the meshcop instance once one contributed (:391-401); nn,
// vn and mn from a meshcop instance only (:416-423); meshcop listed before
// trel (:451-457); LastSeen now (:458).
func (r *Registry) upsertLocked(st discovery.NameState, inst *instance) {
	xa := rawHex(st.Parameters, "xa", 8)
	if xa == "" && inst.source == "trel" {
		xa = xaOfTrelInstance(st.QName)
	}
	if xa == "" {
		r.log.Debug("brdiscovery: instance without a valid xa skipped", "qname", st.QName, "source", inst.source)
		return
	}
	rec, existing := r.entries[xa]
	if !existing {
		rec = &record{Entry: Entry{ExtAddress: xa}}
	}
	canOverwrite := inst.source == "meshcop" || !slices.Contains(rec.sources, "meshcop")
	if xp := rawHex(st.Parameters, "xp", 8); xp != "" && canOverwrite {
		rec.ExtPanID = xp
	}
	if target := st.SRVTarget(); target != "" && canOverwrite {
		rec.HostName = target
	}
	if inst.source == "meshcop" {
		if v, ok := st.Parameters.Get("nn"); ok {
			rec.NetworkName = v
		}
		if v, ok := st.Parameters.Get("vn"); ok {
			rec.VendorName = v
		}
		if v, ok := st.Parameters.Get("mn"); ok {
			rec.ModelName = v
		}
	}
	if !slices.Contains(rec.sources, inst.source) {
		if inst.source == "meshcop" {
			rec.sources = append([]string{inst.source}, rec.sources...)
		} else {
			rec.sources = append(rec.sources, inst.source)
		}
	}
	rec.LastSeen = r.clk.Now()
	if existing {
		r.emitLocked(EventUpdated, rec)
	} else {
		if len(r.entries) >= maxEntries {
			r.evictOldestLocked()
		}
		r.entries[xa] = rec
		r.order = append(r.order, xa)
		r.emitLocked(EventAdded, rec)
	}
	inst.xa = xa
}

// evictOldestLocked removes the oldest stale entry, else the oldest live
// one, and ignores its instances until they go and come back. Mirrors
// matter.js packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:565-610
// (#evictOldest) at db021da0, where the evicted entry's instance observers
// are released.
func (r *Registry) evictOldestLocked() {
	var stale, live string
	var staleSeen, liveSeen time.Time
	for _, xa := range r.order {
		rec := r.entries[xa]
		if len(rec.sources) == 0 {
			if stale == "" || rec.LastSeen.Before(staleSeen) {
				stale, staleSeen = xa, rec.LastSeen
			}
		} else if live == "" || rec.LastSeen.Before(liveSeen) {
			live, liveSeen = xa, rec.LastSeen
		}
	}
	victim := stale
	if victim == "" {
		victim = live
	}
	if victim == "" {
		return
	}
	r.removeLocked(victim)
	for key, inst := range r.instances {
		if inst.xa == victim {
			delete(r.instances, key)
			r.evicted[key] = true
		}
	}
}

// rawHex is key's raw TXT bytes as upper-case hex, "" when absent, empty
// or not n bytes long. Mirrors matter.js
// packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:630-634
// (rawHex) at db021da0.
func rawHex(p discovery.Parameters, key string, n int) string {
	b, ok := p.Raw(key)
	if !ok || len(b) == 0 || len(b) != n {
		return ""
	}
	return strings.ToUpper(hex.EncodeToString(b))
}

// xaOfTrelInstance is the extended address a TREL instance's name states:
// its first label when that is 16 hex digits, upper-cased. Mirrors
// matter.js packages/thread-br-client/src/discovery/BorderRouterRegistry.ts:642-647
// (deriveXaFromTrelInstance) at db021da0.
func xaOfTrelInstance(qname string) string {
	label, _, _ := strings.Cut(qname, ".")
	if len(label) != 16 {
		return ""
	}
	if _, err := hex.DecodeString(label); err != nil {
		return ""
	}
	return strings.ToUpper(label)
}
