// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"maps"
	"math"
	"slices"
	"sync"

	fabricdevice "github.com/SukramJ/go-fabric/device"
)

// UnknownDeviceType is the device type of an endpoint whose Descriptor has
// not named one — matter.js EndpointType.UNKNOWN_DEVICE_TYPE, DeviceTypeId(-1)
// (node/src/endpoint/type/EndpointType.ts:43 at 0ceb202d).
const UnknownDeviceType uint32 = math.MaxUint32

// rootNodeDeviceType is the RootNode device type the image's endpoint 0
// starts with (ClientNode.ts:401-404 over Node.CommonRootEndpoint,
// packages/node/src/endpoints/root.ts:314-319).
const rootNodeDeviceType uint32 = 0x0016

// Endpoint is one endpoint of the image — an endpoint of a matter.js
// ClientNode with its EndpointStructure (ClientStructure.ts:1607-1611). Its
// methods are safe for concurrent use.
type Endpoint struct {
	mu     *sync.RWMutex
	number uint16

	deviceType     uint32
	deviceRevision uint16
	hasRevision    bool
	classification string
	name           string

	owner, pendingOwner *Endpoint
	children            []*Endpoint
	installed           bool
	destroyed           bool
	clusters            map[uint32]*Cluster
}

// newEndpoint is the endpoint #endpointFor creates (ClientStructure.ts:1020-1033):
// "Unknown", of the unknown device type and revision, classified simple
// (EndpointType.ts:31).
func newEndpoint(mu *sync.RWMutex, number uint16) *Endpoint {
	return &Endpoint{mu: mu, number: number, deviceType: UnknownDeviceType, classification: "simple", name: "Unknown", clusters: map[uint32]*Cluster{}}
}

// Number is the endpoint number.
func (e *Endpoint) Number() uint16 { return e.number }

// DeviceType is the device type the Descriptor names — the first
// application device type, else the last other one
// (ClientStructure.ts:858-905); [UnknownDeviceType] while none is known.
func (e *Endpoint) DeviceType() uint32 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.deviceType
}

// Device is the generated device type ([fabricdevice.Type]) of the endpoint's
// device type — the one [Endpoint.DeviceType] reports — or nil for a
// device type device/ does not generate: the unknown device type, a
// manufacturer-specific or unknown id, and the snapshot types
// script/devicegen excludes (see [fabricdevice.Lookup]: the camera and media
// families and ThreadBorderRouter, NetworkInfrastructureManager,
// JointFabricAdministrator, OtaProvider).
//
// matter.js gives a client endpoint its type from the model
// (packages/node/src/node/client/ClientStructure.ts:871-897,
// this.#node.matter.deviceTypes(dt.deviceType)) and installs no generated
// device type on it; the Go view answers the generated one beside it, so a
// host reads the requirements the device layer mounts a server by. Its
// Revision is the snapshot's; the peer's is [Endpoint.DeviceRevision].
func (e *Endpoint) Device() *fabricdevice.Type {
	t, _ := fabricdevice.Lookup(e.DeviceType())
	return t
}

// DeviceRevision is the device type's revision; ok false while unknown
// (EndpointType.UNKNOWN_DEVICE_REVISION).
func (e *Endpoint) DeviceRevision() (revision uint16, ok bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.deviceRevision, e.hasRevision
}

// Classification is the device type's classification ("simple", "utility",
// "node", …).
func (e *Endpoint) Classification() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.classification
}

// Name is the device type's name, "Unknown#<hex4>" for a device type the
// schema does not know, "Unknown" while none is known.
func (e *Endpoint) Name() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.name
}

// Owner is the endpoint's parent; nil for endpoint 0.
func (e *Endpoint) Owner() *Endpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.owner
}

// Installed reports whether the endpoint is on the image's tree.
func (e *Endpoint) Installed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.installed
}

// Clusters are the endpoint's ready clusters in id order.
func (e *Endpoint) Clusters() []*Cluster {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*Cluster
	for _, id := range slices.Sorted(maps.Keys(e.clusters)) {
		if c := e.clusters[id]; c.injected != nil {
			out = append(out, c)
		}
	}
	return out
}

// Cluster is the endpoint's cluster id, nil when it has none ready.
func (e *Endpoint) Cluster(id uint32) *Cluster {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if c := e.clusters[id]; c != nil && c.injected != nil {
		return c
	}
	return nil
}

// Parts are the endpoint's installed children in number order.
func (e *Endpoint) Parts() []*Endpoint {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.partsLocked()
}

// partsLocked is Parts; the caller holds the lock.
func (e *Endpoint) partsLocked() []*Endpoint {
	out := slices.Clone(e.children)
	slices.SortFunc(out, func(a, b *Endpoint) int { return int(a.number) - int(b.number) })
	return out
}
