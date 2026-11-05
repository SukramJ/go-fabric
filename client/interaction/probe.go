// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"log/slog"
	"net"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/im"
)

// ProbeOptions configure [Interaction.Probe] — matter.js
// ClientProbeOptions (packages/protocol/src/action/client/ClientInteraction.ts:1370-1385
// at 862ad75f). The abort is the ctx Probe takes.
type ProbeOptions struct {
	// Network names the network profile whose throttle the probe takes a
	// slot of; "" is the interaction's [Config].Network, then the peer's
	// medium profile (`options?.network ?? this.#network`,
	// ClientInteraction.ts:370; [ExchangeOptions].Network).
	// matter.js falls back to the interaction's own network (:370); a Go
	// Interaction has none, its provider choosing the peer's.
	Network string
	// AddressOverride sends the probe to this address instead of the
	// session's, without changing the session's address or any other
	// exchange on it (ClientRequest.ts:34-40); nil is the session's.
	AddressOverride *net.UDPAddr
	// SuppressPeerLoss waives the peer-loss report of a failed probe, so
	// the session stays alive.
	SuppressPeerLoss bool
	// MaxRetransmissions caps the probe's MRP transmissions; 0 is
	// [exchange.MaxTransmissions] ([exchange.SendOptions].MaxRetransmissions).
	MaxRetransmissions int
}

// Probe checks that the peer answers on an existing session with an empty
// read and reports whether it did. Mirrors matter.js ClientInteraction.probe
// (packages/protocol/src/action/client/ClientInteraction.ts:352-412 at
// 862ad75f): a closed interaction probes nothing (:361-363); the exchange
// requires an existing session — never a new connection — and goes to
// opts.AddressOverride when set (:369-374), a provider without a session
// failing the probe (:375-378); the read has no paths and no fabric filter
// (Read({fabricFilter: false}), :383), sent with opts.SuppressPeerLoss and
// opts.MaxRetransmissions (:384-387); its reports are drained as
// readDataReports reads them (:388); any error, and the end of ctx, is
// false (:397-407); the exchange is closed either way (:408-411).
//
// The reports are read by this package's readDataReports, the reader of
// every other read: client/peer's own probe (the address monitor's) sits
// below this package and cannot share it.
func (c *Interaction) Probe(ctx context.Context, opts ProbeOptions) bool {
	if ctx.Err() != nil {
		return false
	}
	m, ictx, err := c.begin(ctx, ExchangeOptions{
		RequireExistingSession: true, AddressOverride: opts.AddressOverride, Network: opts.Network,
	})
	if err != nil {
		return false
	}
	defer m.close()
	req, err := encodeReadingRequest(im.ReadRequest{FabricFiltered: false})
	if err != nil {
		return false
	}
	if err := m.x.Send(ictx, im.OpcodeReadRequest, req, exchange.SendOptions{
		SuppressPeerLoss: opts.SuppressPeerLoss, MaxRetransmissions: opts.MaxRetransmissions,
	}); err != nil {
		c.probeFailed(ictx, err)
		return false
	}
	if err := readDataReports(ictx, c.log, m, nil, func(*im.ReportData) error { return nil }); err != nil {
		c.probeFailed(ictx, err)
		return false
	}
	c.log.Info("matter.controller.interaction.probe", slog.String("result", "success"))
	return true
}

// probeFailed logs a failed probe unless it was aborted (:398-406).
func (c *Interaction) probeFailed(ctx context.Context, err error) {
	if ctx.Err() == nil {
		c.log.Info("matter.controller.interaction.probe", slog.String("result", "failed"), slog.Any("err", err))
	}
}
