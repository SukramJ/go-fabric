// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"fmt"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The node's settings — matter.js NetworkClient.State's persisted fields
// (behavior/system/network/NetworkClient.ts:331-372 at 0ceb202d, all
// quality N) — are stored with the image, under a cluster id no cluster
// has, on endpoint 0: the place of matter.js's "network" behaviour context
// in ClientNodeStores.
const (
	settingsCluster uint32 = 0xFFFF_FFFF

	settingDisabled            uint32 = 0
	settingAutoSubscribe       uint32 = 1
	settingMaxEventNumber      uint32 = 2
	settingDefaultSubscription uint32 = 3
)

// settings are a node's persisted settings.
type settings struct {
	disabled            bool
	autoSubscribe       bool
	maxEventNumber      uint64
	defaultSubscription *SubscribeOptions
}

// encode is the settings' stored form.
func (s settings) encode() (map[uint32][]byte, error) {
	out := map[uint32][]byte{settingDefaultSubscription: nil}
	for id, put := range map[uint32]func(e *tlv.Encoder){
		settingDisabled:       func(e *tlv.Encoder) { e.PutBool(tlv.AnonymousTag(), s.disabled) },
		settingAutoSubscribe:  func(e *tlv.Encoder) { e.PutBool(tlv.AnonymousTag(), s.autoSubscribe) },
		settingMaxEventNumber: func(e *tlv.Encoder) { e.PutUint(tlv.AnonymousTag(), s.maxEventNumber) },
	} {
		e := tlv.NewEncoder()
		put(e)
		b, err := e.Bytes()
		if err != nil {
			return nil, err
		}
		out[id] = b
	}
	if s.defaultSubscription != nil {
		b, err := encodeSubscribeOptions(s.defaultSubscription)
		if err != nil {
			return nil, err
		}
		out[settingDefaultSubscription] = b
	}
	return out, nil
}

// decodeSettings reads stored settings; an absent value keeps its default.
func decodeSettings(values map[uint32][]byte) (settings, error) {
	var s settings
	for id, b := range values {
		n, err := decodeValue(b)
		if err != nil {
			return s, fmt.Errorf("setting %d: %w", id, err)
		}
		switch id {
		case settingDisabled:
			s.disabled, err = spec.DecodeBool(n)
		case settingAutoSubscribe:
			s.autoSubscribe, err = spec.DecodeBool(n)
		case settingMaxEventNumber:
			s.maxEventNumber, err = spec.DecodeUint[uint64](64)(n)
		case settingDefaultSubscription:
			s.defaultSubscription, err = decodeSubscribeOptions(n)
		}
		if err != nil {
			return s, fmt.Errorf("setting %d: %w", id, err)
		}
	}
	return s, nil
}

// The stored form of SubscribeOptions: a struct of 0 attribute paths, 1
// event paths (each list present when not nil), 2 noFabricFilter, 3
// keepSubscriptions, 4 and 5 the interval bounds in milliseconds (present
// when set). A path is a struct of 0 node, 1 endpoint, 2 cluster, 3
// attribute or event — each present when given — and, for an event, 4 urgent.

func encodeSubscribeOptions(o *SubscribeOptions) ([]byte, error) {
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	if o.Attributes != nil {
		e.StartArray(tlv.ContextTag(0))
		for _, p := range o.Attributes {
			putPath(e, p.HasNode, p.Node, p.HasEndpoint, p.Endpoint, p.HasCluster, p.Cluster, p.HasAttribute, p.Attribute)
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
	}
	if o.Events != nil {
		e.StartArray(tlv.ContextTag(1))
		for _, p := range o.Events {
			putPath(e, p.HasNode, p.Node, p.HasEndpoint, p.Endpoint, p.HasCluster, p.Cluster, p.HasEvent, p.Event)
			e.PutBool(tlv.ContextTag(4), p.IsUrgent)
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
	}
	e.PutBool(tlv.ContextTag(2), o.NoFabricFilter)
	e.PutBool(tlv.ContextTag(3), o.KeepSubscriptions)
	if o.MinIntervalFloor != nil {
		e.PutUint(tlv.ContextTag(4), uint64(o.MinIntervalFloor.Milliseconds())) //nolint:gosec // a bound is not negative
	}
	if o.MaxIntervalCeiling != nil {
		e.PutUint(tlv.ContextTag(5), uint64(o.MaxIntervalCeiling.Milliseconds())) //nolint:gosec // a bound is not negative
	}
	_ = e.EndContainer()
	return e.Bytes()
}

// putPath opens a path struct and writes its ids; the caller closes it.
func putPath(e *tlv.Encoder, hasNode bool, node uint64, hasEP bool, ep uint16, hasCluster bool, cluster uint32, hasElement bool, element uint32) {
	e.StartStruct(tlv.AnonymousTag())
	if hasNode {
		e.PutUint(tlv.ContextTag(0), node)
	}
	if hasEP {
		e.PutUint(tlv.ContextTag(1), uint64(ep))
	}
	if hasCluster {
		e.PutUint(tlv.ContextTag(2), uint64(cluster))
	}
	if hasElement {
		e.PutUint(tlv.ContextTag(3), uint64(element))
	}
}

func decodeSubscribeOptions(n spec.Node) (*SubscribeOptions, error) {
	o := &SubscribeOptions{}
	err := spec.DecodeFields(n, "defaultSubscription",
		spec.Bind(0, "attributes", false, &o.Attributes, spec.DecodeList(decodeAttributePath)),
		spec.Bind(1, "events", false, &o.Events, spec.DecodeList(decodeEventPath)),
		spec.Bind(2, "noFabricFilter", true, &o.NoFabricFilter, spec.DecodeBool),
		spec.Bind(3, "keepSubscriptions", true, &o.KeepSubscriptions, spec.DecodeBool),
		spec.Bind(4, "minIntervalFloor", false, &o.MinIntervalFloor, spec.DecodeOptional(decodeMillis)),
		spec.Bind(5, "maxIntervalCeiling", false, &o.MaxIntervalCeiling, spec.DecodeOptional(decodeMillis)),
	)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func decodeMillis(n spec.Node) (time.Duration, error) {
	ms, err := spec.DecodeUint[uint64](64)(n)
	return time.Duration(ms) * time.Millisecond, err //nolint:gosec // stored by encodeSubscribeOptions
}

// pathIDs reads a path struct's ids, each with its presence.
type pathIDs struct {
	node             *uint64
	ep               *uint16
	cluster, element *uint32
	urgent           bool
}

func decodePathIDs(n spec.Node) (pathIDs, error) {
	var p pathIDs
	err := spec.DecodeFields(n, "path",
		spec.Bind(0, "node", false, &p.node, spec.DecodeOptional(spec.DecodeUint[uint64](64))),
		spec.Bind(1, "endpoint", false, &p.ep, spec.DecodeOptional(spec.DecodeUint[uint16](16))),
		spec.Bind(2, "cluster", false, &p.cluster, spec.DecodeOptional(spec.DecodeUint[uint32](32))),
		spec.Bind(3, "element", false, &p.element, spec.DecodeOptional(spec.DecodeUint[uint32](32))),
		spec.Bind(4, "urgent", false, &p.urgent, spec.DecodeBool),
	)
	return p, err
}

func decodeAttributePath(n spec.Node) (im.ConcreteAttributePath, error) {
	ids, err := decodePathIDs(n)
	var p im.ConcreteAttributePath
	if ids.node != nil {
		p.Node, p.HasNode = *ids.node, true
	}
	if ids.ep != nil {
		p.Endpoint, p.HasEndpoint = *ids.ep, true
	}
	if ids.cluster != nil {
		p.Cluster, p.HasCluster = *ids.cluster, true
	}
	if ids.element != nil {
		p.Attribute, p.HasAttribute = *ids.element, true
	}
	return p, err
}

func decodeEventPath(n spec.Node) (im.ConcreteEventPath, error) {
	ids, err := decodePathIDs(n)
	p := im.ConcreteEventPath{IsUrgent: ids.urgent}
	if ids.node != nil {
		p.Node, p.HasNode = *ids.node, true
	}
	if ids.ep != nil {
		p.Endpoint, p.HasEndpoint = *ids.ep, true
	}
	if ids.cluster != nil {
		p.Cluster, p.HasCluster = *ids.cluster, true
	}
	if ids.element != nil {
		p.Event, p.HasEvent = *ids.element, true
	}
	return p, err
}

// clone copies the options.
func (o *SubscribeOptions) clone() *SubscribeOptions {
	if o == nil {
		return nil
	}
	c := *o
	if o.Attributes != nil {
		c.Attributes = slices.Clone(o.Attributes)
	}
	if o.Events != nil {
		c.Events = slices.Clone(o.Events)
	}
	return &c
}
