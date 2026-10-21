// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/tlv"
)

// cache is one cluster's values and DataVersion — matter.js DatasourceCache
// (packages/node/src/storage/client/DatasourceCache.ts at 0ceb202d) without
// the local-write compensation (writes go to the peer, not through the
// cache). Its fields are guarded by the node's lock.
type cache struct {
	p       *persister
	ep      uint16
	cluster uint32

	values map[uint32]spec.Node
	// version is the cluster's DataVersion; hasVersion false is
	// Datasource.UNKNOWN_VERSION (DatasourceCache.ts:47-48).
	version    uint32
	hasVersion bool

	// dirty are the keys a buffered write has not flushed;
	// versionDirty the version.
	dirty        map[uint32]struct{}
	versionDirty bool
	erased       bool
}

// newCache is a cache seeded with stored values (DatasourceCache.ts:38-49).
func newCache(p *persister, ep uint16, cluster uint32, values map[uint32]spec.Node, version *uint32) *cache {
	c := &cache{p: p, ep: ep, cluster: cluster, values: values, dirty: map[uint32]struct{}{}}
	if c.values == nil {
		c.values = map[uint32]spec.Node{}
	}
	if version != nil {
		c.version, c.hasVersion = *version, true
	}
	return c
}

// externalSet applies what the peer reported — DatasourceCache.externalSet
// and #applyExternal (DatasourceCache.ts:132-174): a nil value deletes the
// key (EndpointStore.ts:104-105), a version replaces the cluster's
// (:151-154); buffered, the keys are marked dirty and the node's flusher
// armed (:156-160), else the store is written now (:161-163).
func (c *cache) externalSet(values map[uint32]*spec.Node, version *uint32) {
	if c.erased {
		return
	}
	if version != nil {
		c.version, c.hasVersion = *version, true
	}
	for k, v := range values {
		if v == nil {
			delete(c.values, k)
			continue
		}
		c.values[k] = *v
	}
	if c.p.interval > 0 {
		for k := range values {
			c.dirty[k] = struct{}{}
		}
		if version != nil {
			c.versionDirty = true
		}
		c.p.markDirty(c)
		return
	}
	c.p.save(c, slices.Collect(maps.Keys(values)))
}

// invalidateVersion forgets the version, so the next wildcard read fetches
// the whole cluster (DatasourceCache.ts:199-201).
func (c *cache) invalidateVersion() { c.hasVersion = false }

// versionPtr is the version, nil when unknown.
func (c *cache) versionPtr() *uint32 {
	if !c.hasVersion {
		return nil
	}
	v := c.version
	return &v
}

// discard disables the cache without touching the store
// (DatasourceCache.ts:210-214).
func (c *cache) discard() {
	c.erased = true
	clear(c.dirty)
	c.versionDirty = false
	c.p.remove(c)
}

// erase disables the cache and erases its cluster from the store
// (DatasourceCache.ts:219-222).
func (c *cache) erase() {
	c.discard()
	if err := c.p.store.EraseCluster(c.p.nodeID, c.ep, c.cluster); err != nil {
		c.p.log.Warn("matter.controller.client.store_erase_failed", slog.Int("endpoint", int(c.ep)), slog.Uint64("cluster", uint64(c.cluster)), slog.String("err", err.Error()))
	}
}

// encodeValue is a value's stored form: its TLV encoding, anonymous.
func encodeValue(n spec.Node) ([]byte, error) {
	enc := tlv.NewEncoder()
	interaction.Raw(n).EncodeTLV(enc, tlv.AnonymousTag())
	return enc.Bytes()
}

// decodeValue reads a stored value.
func decodeValue(b []byte) (spec.Node, error) {
	return spec.ReadElement(tlv.NewDecoder(b))
}

// persister writes a node's caches to its store — the node's half of
// matter.js ClientCacheBuffer (storage/client/ClientCacheBuffer.ts:21-174)
// and LocalWriter: with an interval, dirty caches are written by one timer
// per node in one pass; without, every change is written at once.
type persister struct {
	store    Store
	nodeID   string
	interval time.Duration
	clock    exchange.Clock
	log      *slog.Logger
	// image is the node's lock, which guards the caches.
	image sync.Locker

	// mu guards dirty, flushing, stopped and timer.
	mu      sync.Mutex
	dirty   []*cache
	timer   exchange.Timer
	stopped bool
	// flushing counts the timer's flushes armed or running, so stop can
	// wait for one that is writing — matter.js serializes its flushes
	// through a mutex that close drains (ClientCacheBuffer.ts:94-101).
	flushing sync.WaitGroup
}

// save writes keys of c and its version now; a store error is logged, never
// raised (DatasourceCache.ts:297-303). The caller holds the node's lock.
func (p *persister) save(c *cache, keys []uint32) {
	values, err := c.encode(keys)
	if err == nil {
		err = p.store.Save(p.nodeID, c.ep, c.cluster, values, c.versionPtr())
	}
	if err != nil {
		p.log.Warn("matter.controller.client.store_write_failed", slog.Int("endpoint", int(c.ep)), slog.Uint64("cluster", uint64(c.cluster)), slog.String("err", err.Error()))
	}
}

// encode is keys of c in stored form, nil for a deleted key.
func (c *cache) encode(keys []uint32) (map[uint32][]byte, error) {
	out := make(map[uint32][]byte, len(keys))
	for _, k := range keys {
		v, ok := c.values[k]
		if !ok {
			out[k] = nil
			continue
		}
		b, err := encodeValue(v)
		if err != nil {
			return nil, err
		}
		out[k] = b
	}
	return out, nil
}

// markDirty records a cache with unflushed changes and arms the flush timer
// (ClientCacheBuffer.markDirty, :64-66; the timer of #timerLoop,
// :159-173). The caller holds the node's lock.
func (p *persister) markDirty(c *cache) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.Contains(p.dirty, c) {
		p.dirty = append(p.dirty, c)
	}
	if p.timer == nil && !p.stopped {
		p.armLocked()
	}
}

// armLocked arms the flush timer and counts its flush. The caller holds
// mu.
func (p *persister) armLocked() {
	p.flushing.Add(1)
	p.timer = p.clock.AfterFunc(p.interval, func() {
		defer p.flushing.Done()
		p.flush()
	})
}

// flushJob is one cache's flush.
type flushJob struct {
	c       *cache
	keys    []uint32
	version bool
	values  map[uint32][]byte
	ver     *uint32
}

// flush writes every dirty cache in one pass — #doFlush
// (ClientCacheBuffer.ts:103-153) with DatasourceCache.flush (:243-306): a
// cache that fails keeps its dirty keys and is retried next cycle
// (:297-303, ClientCacheBuffer.ts:122-130).
func (p *persister) flush() {
	p.image.Lock()
	p.mu.Lock()
	snapshot := p.dirty
	p.dirty = nil
	p.timer = nil
	p.mu.Unlock()
	var jobs []flushJob
	for _, c := range snapshot {
		if c.erased || (len(c.dirty) == 0 && !c.versionDirty) {
			continue
		}
		job := flushJob{c: c, keys: slices.Sorted(maps.Keys(c.dirty)), version: c.versionDirty, ver: c.versionPtr()}
		values, err := c.encode(job.keys)
		if err != nil {
			p.log.Warn("matter.controller.client.store_write_failed", slog.Int("endpoint", int(c.ep)), slog.Uint64("cluster", uint64(c.cluster)), slog.String("err", err.Error()))
			continue
		}
		job.values = values
		clear(c.dirty)
		c.versionDirty = false
		jobs = append(jobs, job)
	}
	p.image.Unlock()

	var failed []flushJob
	for _, j := range jobs {
		if err := p.store.Save(p.nodeID, j.c.ep, j.c.cluster, j.values, j.ver); err != nil {
			p.log.Warn("matter.controller.client.store_flush_failed", slog.Int("endpoint", int(j.c.ep)), slog.Uint64("cluster", uint64(j.c.cluster)), slog.String("err", err.Error()))
			failed = append(failed, j)
		}
	}
	if len(failed) == 0 {
		return
	}
	p.image.Lock()
	defer p.image.Unlock()
	for _, j := range failed {
		if j.c.erased {
			continue
		}
		for _, k := range j.keys {
			j.c.dirty[k] = struct{}{}
		}
		j.c.versionDirty = j.c.versionDirty || j.version
		p.markDirty(j.c)
	}
}

// remove drops a cache from the dirty set (ClientCacheBuffer.removeDirty,
// :71-73). The caller holds the node's lock.
func (p *persister) remove(c *cache) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dirty = slices.DeleteFunc(p.dirty, func(x *cache) bool { return x == c })
}

// stop halts the timer, waits for a flush the timer already started and
// flushes what is dirty (ClientCacheBuffer.close, :94-101: the timer loop
// is awaited, then the final flush drains the mutex); restart re-arms on
// the next change. Without the wait a flush that had left the node's lock
// for its writes landed after the erase that followed stop, and the
// erased node was back in the store.
func (p *persister) stop() {
	p.mu.Lock()
	p.stopped = true
	if p.timer != nil {
		if p.timer.Stop() {
			p.flushing.Done()
		}
		p.timer = nil
	}
	p.mu.Unlock()
	p.flushing.Wait()
	p.flush()
}

// restart lets changes arm the timer again.
func (p *persister) restart() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = false
	if len(p.dirty) > 0 && p.timer == nil && p.interval > 0 {
		p.armLocked()
	}
}
