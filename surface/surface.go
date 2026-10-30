// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/bits"
	"slices"
)

// Surface is the served surface of a node: the root endpoint and every
// other endpoint, ordered by Key.
type Surface struct {
	Root      Endpoint
	Endpoints []Endpoint
}

// Endpoint is one endpoint's surface.
type Endpoint struct {
	// Key is the endpoint's rendered StableKey; "" for the root.
	Key string
	// Number is the endpoint number, 0 until the endpoint is assembled. It
	// is not part of any digest.
	Number uint16
	// DeviceTypes are the device types in the order the Descriptor lists
	// them.
	DeviceTypes []DeviceTypeRef
	// Clusters are the server clusters, ordered by ID.
	Clusters []Cluster
}

// DeviceTypeRef is one entry of a Descriptor's DeviceTypeList.
type DeviceTypeRef struct {
	ID       uint32
	Revision uint16
}

// Cluster is one server cluster's surface: its ClusterRevision and
// FeatureMap, and its four lists, each sorted ascending. Attributes
// includes the global attributes as the dispatcher serves them.
type Cluster struct {
	ID         uint32
	Revision   uint16
	FeatureMap uint32
	Attributes []uint32
	Accepted   []uint32
	Generated  []uint32
	Events     []uint32
}

// Clone returns a deep copy of s.
func (s Surface) Clone() Surface {
	out := Surface{Root: s.Root.Clone()}
	if s.Endpoints != nil {
		out.Endpoints = make([]Endpoint, len(s.Endpoints))
		for i := range s.Endpoints {
			out.Endpoints[i] = s.Endpoints[i].Clone()
		}
	}
	return out
}

// Clone returns a deep copy of e.
func (e Endpoint) Clone() Endpoint {
	out := e
	out.DeviceTypes = slices.Clone(e.DeviceTypes)
	if e.Clusters != nil {
		out.Clusters = make([]Cluster, len(e.Clusters))
		for i, c := range e.Clusters {
			c.Attributes = slices.Clone(c.Attributes)
			c.Accepted = slices.Clone(c.Accepted)
			c.Generated = slices.Clone(c.Generated)
			c.Events = slices.Clone(c.Events)
			out.Clusters[i] = c
		}
	}
	return out
}

// Digest is the SHA-256 of the canonical JSON of s with every endpoint
// number left out: two surfaces a controller cannot tell apart have the
// same digest, wherever their endpoints are numbered.
func (s Surface) Digest() [32]byte {
	b, err := json.Marshal(s.wire(false))
	if err != nil {
		// The wire form holds nothing but strings, numbers and slices.
		panic(fmt.Sprintf("surface: digest: %v", err))
	}
	return sha256.Sum256(b)
}

// Digest is the SHA-256 of the canonical JSON of e, its number left out.
func (e Endpoint) Digest() [32]byte {
	b, err := json.Marshal(e.wire(false))
	if err != nil {
		panic(fmt.Sprintf("surface: digest: %v", err))
	}
	return sha256.Sum256(b)
}

// MarshalJSON implements [json.Marshaler]: the canonical encoding —
// object keys sorted, no insignificant whitespace, endpoints by key,
// clusters by ID, every id list ascending and present (empty as []),
// device types in Descriptor order, endpoint numbers only where assembled.
// Every id is a lower-case hex string with the 0x prefix: four digits for
// a device type and a cluster ("0x0201"), eight for an attribute, command
// or event and for the FeatureMap ("0x00000000"); revisions and endpoint
// numbers are decimal.
func (s Surface) MarshalJSON() ([]byte, error) { return json.Marshal(s.wire(true)) }

// UnmarshalJSON implements [json.Unmarshaler] for the encoding
// [Surface.MarshalJSON] writes; an id may also be a decimal number.
func (s *Surface) UnmarshalJSON(b []byte) error {
	var w wireSurface
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*s = w.value()
	return nil
}

// The wire forms: fields in the order of their JSON keys, so
// encoding/json writes the keys sorted.
type (
	wireSurface struct {
		Endpoints []wireEndpoint `json:"endpoints"`
		Root      wireEndpoint   `json:"root"`
	}
	wireEndpoint struct {
		Clusters    []wireCluster    `json:"clusters"`
		DeviceTypes []wireDeviceType `json:"deviceTypes"`
		Key         string           `json:"key"`
		Number      uint16           `json:"number,omitempty"`
	}
	wireDeviceType struct {
		ID       id16   `json:"id"`
		Revision uint16 `json:"revision"`
	}
	wireCluster struct {
		Accepted   []id32 `json:"accepted"`
		Attributes []id32 `json:"attributes"`
		Events     []id32 `json:"events"`
		FeatureMap id32   `json:"featureMap"`
		Generated  []id32 `json:"generated"`
		ID         id32   `json:"id"`
		Revision   uint16 `json:"revision"`
	}
)

func (s Surface) wire(numbers bool) wireSurface {
	w := wireSurface{Root: s.Root.wire(numbers), Endpoints: make([]wireEndpoint, 0, len(s.Endpoints))}
	for i := range s.Endpoints {
		w.Endpoints = append(w.Endpoints, s.Endpoints[i].wire(numbers))
	}
	slices.SortStableFunc(w.Endpoints, func(x, y wireEndpoint) int { return cmp.Compare(x.Key, y.Key) })
	return w
}

func (e Endpoint) wire(numbers bool) wireEndpoint {
	w := wireEndpoint{
		Key:         e.Key,
		DeviceTypes: make([]wireDeviceType, 0, len(e.DeviceTypes)),
		Clusters:    make([]wireCluster, 0, len(e.Clusters)),
	}
	if numbers {
		w.Number = e.Number
	}
	for _, dt := range e.DeviceTypes {
		w.DeviceTypes = append(w.DeviceTypes, wireDeviceType{ID: id16(dt.ID), Revision: dt.Revision})
	}
	for _, c := range e.Clusters {
		w.Clusters = append(w.Clusters, wireCluster{
			ID: id32(c.ID), Revision: c.Revision, FeatureMap: id32(c.FeatureMap),
			Attributes: sorted(c.Attributes), Accepted: sorted(c.Accepted),
			Generated: sorted(c.Generated), Events: sorted(c.Events),
		})
	}
	slices.SortStableFunc(w.Clusters, func(x, y wireCluster) int { return cmp.Compare(x.ID, y.ID) })
	return w
}

func (w wireSurface) value() Surface {
	s := Surface{Root: w.Root.value()}
	for _, e := range w.Endpoints {
		s.Endpoints = append(s.Endpoints, e.value())
	}
	return s
}

func (w wireEndpoint) value() Endpoint {
	e := Endpoint{Key: w.Key, Number: w.Number}
	for _, dt := range w.DeviceTypes {
		e.DeviceTypes = append(e.DeviceTypes, DeviceTypeRef{ID: uint32(dt.ID), Revision: dt.Revision})
	}
	for _, c := range w.Clusters {
		e.Clusters = append(e.Clusters, Cluster{
			ID: uint32(c.ID), Revision: c.Revision, FeatureMap: uint32(c.FeatureMap),
			Attributes: plain32(c.Attributes), Accepted: plain32(c.Accepted),
			Generated: plain32(c.Generated), Events: plain32(c.Events),
		})
	}
	return e
}

// sorted is an ascending copy of ids in their JSON form, [] for none.
func sorted(ids []uint32) []id32 {
	out := append(make([]uint32, 0, len(ids)), ids...)
	slices.Sort(out)
	return ids32(out)
}

// Change is one difference between two surfaces ([Diff]): the element of
// kind Kind with ID on cluster Cluster of the endpoint with key Endpoint,
// as it reads before and after. An absent element reads "".
//
// What Before and After say per kind: a device type and a cluster its
// revision ("revision 4"); a feature, ID its one-bit mask, "set"; an
// attribute and an event "served"; a command "accepted" or "generated",
// one Change per list.
type Change struct {
	Endpoint      string
	Cluster       uint32
	Kind          Kind
	ID            uint32
	Before, After string
}

// String renders c for a log line or a test failure.
func (c Change) String() string {
	return fmt.Sprintf("%s: %q → %q", Element{Endpoint: c.Endpoint, Cluster: c.Cluster, Kind: c.Kind, ID: c.ID}, c.Before, c.After)
}

// Diff returns every difference from a to b, endpoint by endpoint in key
// order (the root first), then device types, then clusters by ID; an empty
// slice when a controller could not tell them apart. Endpoint numbers are
// no difference. An endpoint or a cluster that only one side has is one
// Change per device type or one per cluster, not one per element.
func Diff(a, b Surface) []Change {
	out := []Change{}
	out = diffEndpoint(out, a.Root, b.Root)
	before := byKey(a.Endpoints)
	after := byKey(b.Endpoints)
	keys := make([]string, 0, len(before)+len(after))
	for k := range before {
		keys = append(keys, k)
	}
	for k := range after {
		if _, dup := before[k]; !dup {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		out = diffEndpoint(out, before[k], after[k])
	}
	return out
}

func byKey(eps []Endpoint) map[string]Endpoint {
	m := make(map[string]Endpoint, len(eps))
	for _, e := range eps {
		m[e.Key] = e
	}
	return m
}

func revision(r uint16) string { return fmt.Sprintf("revision %d", r) }

func diffEndpoint(out []Change, a, b Endpoint) []Change {
	key := cmp.Or(a.Key, b.Key)
	dtA := map[uint32]uint16{}
	for _, dt := range a.DeviceTypes {
		dtA[dt.ID] = dt.Revision
	}
	dtB := map[uint32]uint16{}
	for _, dt := range b.DeviceTypes {
		dtB[dt.ID] = dt.Revision
	}
	for _, id := range unionKeys(dtA, dtB) {
		ra, inA := dtA[id]
		rb, inB := dtB[id]
		if inA && inB && ra == rb {
			continue
		}
		ch := Change{Endpoint: key, Kind: KindDeviceType, ID: id}
		if inA {
			ch.Before = revision(ra)
		}
		if inB {
			ch.After = revision(rb)
		}
		out = append(out, ch)
	}
	cA := map[uint32]Cluster{}
	for _, c := range a.Clusters {
		cA[c.ID] = c
	}
	cB := map[uint32]Cluster{}
	for _, c := range b.Clusters {
		cB[c.ID] = c
	}
	for _, id := range unionKeys(cA, cB) {
		ca, inA := cA[id]
		cb, inB := cB[id]
		switch {
		case inA && inB:
			out = diffCluster(out, key, ca, cb)
		case inA:
			out = append(out, Change{Endpoint: key, Cluster: id, Kind: KindCluster, ID: id, Before: revision(ca.Revision)})
		default:
			out = append(out, Change{Endpoint: key, Cluster: id, Kind: KindCluster, ID: id, After: revision(cb.Revision)})
		}
	}
	return out
}

func diffCluster(out []Change, key string, a, b Cluster) []Change {
	if a.Revision != b.Revision {
		out = append(out, Change{Endpoint: key, Cluster: a.ID, Kind: KindCluster, ID: a.ID, Before: revision(a.Revision), After: revision(b.Revision)})
	}
	for x := a.FeatureMap ^ b.FeatureMap; x != 0; x &= x - 1 {
		bit := uint32(1) << bits.TrailingZeros32(x)
		ch := Change{Endpoint: key, Cluster: a.ID, Kind: KindFeature, ID: bit}
		if a.FeatureMap&bit != 0 {
			ch.Before = "set"
		} else {
			ch.After = "set"
		}
		out = append(out, ch)
	}
	out = diffList(out, key, a.ID, KindAttribute, "served", a.Attributes, b.Attributes)
	out = diffList(out, key, a.ID, KindCommand, "accepted", a.Accepted, b.Accepted)
	out = diffList(out, key, a.ID, KindCommand, "generated", a.Generated, b.Generated)
	return diffList(out, key, a.ID, KindEvent, "served", a.Events, b.Events)
}

func diffList(out []Change, key string, cluster uint32, kind Kind, present string, a, b []uint32) []Change {
	inA := map[uint32]struct{}{}
	for _, id := range a {
		inA[id] = struct{}{}
	}
	inB := map[uint32]struct{}{}
	for _, id := range b {
		inB[id] = struct{}{}
	}
	for _, id := range unionKeys(inA, inB) {
		_, ok := inA[id]
		_, okB := inB[id]
		if ok == okB {
			continue
		}
		ch := Change{Endpoint: key, Cluster: cluster, Kind: kind, ID: id}
		if ok {
			ch.Before = present
		} else {
			ch.After = present
		}
		out = append(out, ch)
	}
	return out
}

func unionKeys[V any](a, b map[uint32]V) []uint32 {
	ids := make([]uint32, 0, len(a)+len(b))
	for id := range a {
		ids = append(ids, id)
	}
	for id := range b {
		if _, dup := a[id]; !dup {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}
