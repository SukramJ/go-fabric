// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package surface

import (
	"fmt"
	"strconv"
)

// Kind is what an [Element] or a [Change] is about.
type Kind uint8

// Kinds. The zero value is no kind.
const (
	KindDeviceType Kind = iota + 1
	KindCluster
	KindFeature
	KindAttribute
	KindCommand
	KindEvent
)

var kindNames = map[Kind]string{
	KindDeviceType: "device-type", KindCluster: "cluster", KindFeature: "feature",
	KindAttribute: "attribute", KindCommand: "command", KindEvent: "event",
}

// String returns the kind's name ("device-type", "cluster", "feature",
// "attribute", "command", "event").
func (k Kind) String() string { return enumName(kindNames, k) }

// MarshalText implements [encoding.TextMarshaler].
func (k Kind) MarshalText() ([]byte, error) { return marshalEnum(kindNames, k) }

// UnmarshalText implements [encoding.TextUnmarshaler].
func (k *Kind) UnmarshalText(b []byte) error { return unmarshalEnum(kindNames, k, b) }

// Origin is why a candidate [Element] exists.
type Origin uint8

// Origins. The zero value is no origin.
const (
	// OriginClusterMandatory: the cluster's own conformance makes the
	// feature or element mandatory for the selected features.
	OriginClusterMandatory Origin = iota + 1
	// OriginDeviceType: the device type mandates the cluster, the feature
	// or the element — or is the element.
	OriginDeviceType
	// OriginHostDeclared: the host declared it — a feature selection, an
	// optional element or initial value, an opted-in optional cluster, a
	// host's own server.
	OriginHostDeclared
	// OriginKitDefault: a default server's own optional choice the device
	// layer makes. No candidate carries it in this phase (package doc).
	OriginKitDefault
)

var originNames = map[Origin]string{
	OriginClusterMandatory: "cluster-mandatory", OriginDeviceType: "device-type",
	OriginHostDeclared: "host-declared", OriginKitDefault: "kit-default",
}

// String returns the origin's name.
func (o Origin) String() string { return enumName(originNames, o) }

// MarshalText implements [encoding.TextMarshaler].
func (o Origin) MarshalText() ([]byte, error) { return marshalEnum(originNames, o) }

// UnmarshalText implements [encoding.TextUnmarshaler].
func (o *Origin) UnmarshalText(b []byte) error { return unmarshalEnum(originNames, o, b) }

// Reference is the reference an element is served against: matter.js's
// baseline, or an entry of the "Ahead of matter.js" register.
type Reference struct {
	// Ahead is the register ID; "" is the matter.js baseline. The device
	// layer and the node set it from the servers' [AheadSet] declarations;
	// the built-in reference policy refuses it unless
	// [Policies].Extensions enables the row.
	Ahead string
}

// Element is one candidate of an endpoint's surface.
type Element struct {
	// Endpoint is the endpoint's rendered StableKey; "" for the root.
	Endpoint string
	// Cluster is the cluster id; 0 for a device type.
	Cluster uint32
	Kind    Kind
	// ID is the device type id, the cluster id, the feature's one-bit
	// mask, or the attribute, command or event id.
	ID        uint32
	Origin    Origin
	Reference Reference
	// Mandatory: removing the element would make the composition invalid
	// at the pin — the device type mandates it, or the cluster's
	// conformance does for the selected features.
	Mandatory bool
}

// String names the element: "attribute 0x0001 of cluster 0x0006 on
// endpoint \"light\"".
func (e Element) String() string {
	where := fmt.Sprintf("endpoint %q", e.Endpoint)
	if e.Endpoint == "" {
		where = "the root endpoint"
	}
	switch e.Kind {
	case KindDeviceType:
		return fmt.Sprintf("device type 0x%04X on %s", e.ID, where)
	case KindCluster:
		return fmt.Sprintf("cluster 0x%04X on %s", e.ID, where)
	case KindFeature:
		return fmt.Sprintf("feature 0x%X of cluster 0x%04X on %s", e.ID, e.Cluster, where)
	default:
		return fmt.Sprintf("%s 0x%04X of cluster 0x%04X on %s", e.Kind, e.ID, e.Cluster, where)
	}
}

func enumName[E ~uint8](names map[E]string, v E) string {
	if n, ok := names[v]; ok {
		return n
	}
	return strconv.Itoa(int(v))
}

func marshalEnum[E ~uint8](names map[E]string, v E) ([]byte, error) {
	if n, ok := names[v]; ok {
		return []byte(n), nil
	}
	return nil, fmt.Errorf("surface: no name for %T %d", v, v)
}

func unmarshalEnum[E ~uint8](names map[E]string, v *E, b []byte) error {
	for k, n := range names {
		if n == string(b) {
			*v = k
			return nil
		}
	}
	return fmt.Errorf("surface: unknown %T %q", *v, b)
}
