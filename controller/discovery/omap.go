// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import "slices"

// omap is a map that iterates in insertion order, as a JavaScript Map
// does. matter.js's discovery code depends on that order in several places
// (which TXT record is newest among equals, the order a scanner meets its
// cached devices), so the port keeps it. Setting an existing key keeps its
// position; deleting and setting again moves it to the end.
type omap[K comparable, V any] struct {
	keys []K
	m    map[K]V
}

func newOmap[K comparable, V any]() *omap[K, V] {
	return &omap[K, V]{m: make(map[K]V)}
}

func (o *omap[K, V]) get(k K) (V, bool) {
	v, ok := o.m[k]
	return v, ok
}

func (o *omap[K, V]) has(k K) bool {
	_, ok := o.m[k]
	return ok
}

func (o *omap[K, V]) set(k K, v V) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

func (o *omap[K, V]) del(k K) bool {
	if _, ok := o.m[k]; !ok {
		return false
	}
	delete(o.m, k)
	o.keys = slices.DeleteFunc(o.keys, func(x K) bool { return x == k })
	return true
}

func (o *omap[K, V]) len() int { return len(o.keys) }

func (o *omap[K, V]) clear() {
	o.keys = nil
	o.m = make(map[K]V)
}

// snapshotKeys returns the keys in order; safe to iterate while mutating.
func (o *omap[K, V]) snapshotKeys() []K { return slices.Clone(o.keys) }

// values returns the values in order, as a snapshot.
func (o *omap[K, V]) values() []V {
	out := make([]V, 0, len(o.keys))
	for _, k := range o.keys {
		out = append(out, o.m[k])
	}
	return out
}
