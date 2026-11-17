// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package deviceseam carries the one-way seam through which a node binds
// the devices its Aggregator holds: the lifecycle of device.Device
// (endpoint number, reachable, rename, the device's queue and clock)
// reaches the node without a second exported way to add a device.
//
// Direction of the edge: package device installs [Attach] and
// [AheadRows] from its init; package node calls Attach from
// Aggregator.Add and Replace, and the detach it returns from Remove,
// Replace and Node.Stop, and AheadRows from node.AheadRows.
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
	// Running reports whether the node serves its devices: true from
	// before the bridge's start takes its first snapshot (set under
	// [Composing]) — a part bound to a running node does not move
	// (device.Device.AddPart).
	Running() bool
	// EndpointNumber is the endpoint number the node assembled the
	// endpoint with key under; false before the endpoint is assembled.
	EndpointNumber(key endpoint.SourceKey) (uint16, bool)
	// NotifyReachable tells the controllers that the endpoint with key
	// became reachable or unreachable (the ReachableChanged event and the
	// Reachable attribute); a no-op before the endpoint is assembled.
	NotifyReachable(ctx context.Context, key endpoint.SourceKey, reachable bool)
	// Rename stores name as the NodeLabel of the endpoint with key, then
	// serves and reports it; before the endpoint is assembled it stores
	// the label the assembly restores. An error is the store's refusal:
	// nothing is served (device.Device.Rename returns it).
	Rename(ctx context.Context, key endpoint.SourceKey, name string) error
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

// Composing runs fn while no device tree's structure changes: under the
// lock device.Device.AddPart holds from its checks to its last change.
// The node flips what [Host.Running] answers inside it, so an AddPart
// either completed before the flip or sees it. fn must not call a host
// or a device. Nil until package device installs it.
var Composing func(fn func())

// AheadRows returns the "Ahead of matter.js" register IDs the device
// layer's server declarations name, sorted; node.AheadRows unions them
// with the root servers' rows, the one answer a host reads (review
// 2026-11 W5-api-14).
var AheadRows func() []string
