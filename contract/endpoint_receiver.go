// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

// EndpointReceiver is the optional capability of a [ClusterServer] that
// learns the endpoint it is mounted on. The bridge calls SetEndpoint when
// it mounts the server — together with
// [EventReceiver.SetMatterEventEmitter] — and again when a reassembly
// mounts it on another endpoint, so a server emits its events on its own
// endpoint instead of asking its host for the number.
type EndpointReceiver interface {
	SetEndpoint(endpoint uint16)
}
