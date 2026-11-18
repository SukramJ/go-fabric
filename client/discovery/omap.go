// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

// omap is a map that iterates in insertion order, as a JavaScript Map
// does. matter.js's discovery code depends on that order in several places
// (which TXT record is newest among equals, the order a scanner meets its
// cached devices), so the port keeps it. Setting an existing key keeps its
// position; deleting and setting again moves it to the end. Every
// operation but the snapshots costs the same whatever the map's size: the
// order is a doubly linked list beside the index (N3-disc-2).
type omap[K comparable, V any] struct {
	m          map[K]*omapEntry[K, V]
	head, tail *omapEntry[K, V]
}

type omapEntry[K comparable, V any] struct {
	key        K
	val        V
	prev, next *omapEntry[K, V]
}

func newOmap[K comparable, V any]() *omap[K, V] {
	return &omap[K, V]{m: make(map[K]*omapEntry[K, V])}
}

func (o *omap[K, V]) get(k K) (V, bool) {
	if e, ok := o.m[k]; ok {
		return e.val, true
	}
	var zero V
	return zero, false
}

func (o *omap[K, V]) has(k K) bool {
	_, ok := o.m[k]
	return ok
}

func (o *omap[K, V]) set(k K, v V) {
	if e, ok := o.m[k]; ok {
		e.val = v
		return
	}
	e := &omapEntry[K, V]{key: k, val: v, prev: o.tail}
	if o.tail != nil {
		o.tail.next = e
	} else {
		o.head = e
	}
	o.tail = e
	o.m[k] = e
}

func (o *omap[K, V]) del(k K) bool {
	e, ok := o.m[k]
	if !ok {
		return false
	}
	delete(o.m, k)
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		o.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		o.tail = e.prev
	}
	e.prev, e.next = nil, nil
	return true
}

func (o *omap[K, V]) len() int { return len(o.m) }

// first returns the key set longest ago.
func (o *omap[K, V]) first() (K, bool) {
	if o.head == nil {
		var zero K
		return zero, false
	}
	return o.head.key, true
}

func (o *omap[K, V]) clear() {
	o.head, o.tail = nil, nil
	o.m = make(map[K]*omapEntry[K, V])
}

// snapshotKeys returns the keys in order; safe to iterate while mutating.
func (o *omap[K, V]) snapshotKeys() []K {
	out := make([]K, 0, len(o.m))
	for e := o.head; e != nil; e = e.next {
		out = append(out, e.key)
	}
	return out
}

// values returns the values in order, as a snapshot.
func (o *omap[K, V]) values() []V {
	out := make([]V, 0, len(o.m))
	for e := o.head; e != nil; e = e.next {
		out = append(out, e.val)
	}
	return out
}
