// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package deviceseam carries the one-way seam through which a node binds
// the devices its Aggregator holds: the lifecycle of device.Device
// (endpoint number, reachable, rename, the device's queue and clock)
// reaches the node without a second exported way to add a device.
//
// Direction of the edge: package device installs [Attach] from its init;
// package node calls it from Aggregator.Add and Replace, and the detach it
// returns from Remove, Replace and Node.Stop.
package deviceseam

import (
	"context"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/endpoint"
)

// Host is what a node gives a device it adds: its clock, and the bridge
// behind the device's endpoints, addressed by the endpoints' stable keys.
type Host interface {
	// Clock is the node's clock.
	Clock() clock.Clock
	// EndpointNumber is the endpoint number the node assembled the
	// endpoint with key under; false before the endpoint is assembled.
	EndpointNumber(key endpoint.SourceKey) (uint16, bool)
	// NotifyReachable tells the controllers that the endpoint with key
	// became reachable or unreachable (the ReachableChanged event and the
	// Reachable attribute); a no-op before the endpoint is assembled.
	NotifyReachable(ctx context.Context, key endpoint.SourceKey, reachable bool)
	// Rename sets the NodeLabel of the endpoint with key and reports it;
	// a no-op before the endpoint is assembled.
	Rename(ctx context.Context, key endpoint.SourceKey, name string)
	// HostName gives the endpoint with key the host's current name, served
	// as its NodeLabel while no label is written and persisted nowhere; a
	// no-op before the endpoint is assembled, whose assembly reads the
	// name from the device's Spec.
	HostName(ctx context.Context, key endpoint.SourceKey, name string)
}

// Attach binds ep — a device.Device, or a type that embeds one — to h:
// the device and its parts take h's clock (unless the host set one), start
// their queues and answer their lifecycle calls through h. It returns the
// detach function, which stops the queues — waiting for each to drain —
// and unbinds; ok false when ep is no device (any other node.Endpoint),
// and an error when the device is bound to a node already.
var Attach func(ep any, h Host) (detach func(), ok bool, err error)
