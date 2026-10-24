// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import (
	"context"
	"log/slog"
	"net"

	"github.com/SukramJ/go-fabric/icd/checkin"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// SenderContext is what the Check-In sender needs from its device: the
// address of a registered client and an unsecured send. Mirrors matter.js
// IcdCheckInSender.ts:14-20 (IcdCheckInSenderContext); the crypto port is
// the codec's own.
type SenderContext struct {
	// ResolveAddress returns a client's operational address, nil when it
	// is unknown; best effort.
	ResolveAddress func(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (*net.UDPAddr, error)
	// SendUnsecured sends payload as an unsecured Secure Channel message
	// of opcode to addr, without MRP, and reports whether it went out.
	SendUnsecured func(ctx context.Context, addr *net.UDPAddr, opcode uint8, payload []byte) (bool, error)
	// Logger receives the debug lines of a failed delivery; nil is
	// slog.Default.
	Logger *slog.Logger
}

// CheckInRequest is one Check-In to send. Mirrors matter.js
// IcdCheckInSender.ts:22-29 (IcdCheckInRequest).
type CheckInRequest struct {
	FabricIndex uint8
	PeerNodeID  uint64
	Key         []byte
	Counter     uint32
	// ActiveModeThreshold is the device's threshold in milliseconds.
	ActiveModeThreshold uint16
}

// Sender builds and sends the device's Check-In messages (Secure Channel
// opcode 0x50), unsecured, to a registered client. Delivery is best effort:
// an unresolvable client or a failed send is false, never an error. An
// encoding failure is a defect, not a delivery failure, and is returned.
//
// Mirrors matter.js protocol/src/icd/IcdCheckInSender.ts:38-70
// (IcdCheckInSender).
type Sender struct {
	ctx SenderContext
}

// NewSender returns a sender over its context.
func NewSender(c SenderContext) *Sender {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return &Sender{ctx: c}
}

// Send sends one Check-In and reports whether it went out (IcdCheckInSender.ts:45-69).
func (s *Sender) Send(ctx context.Context, req CheckInRequest) (bool, error) {
	addr, err := s.ctx.ResolveAddress(ctx, req.FabricIndex, req.PeerNodeID)
	if err != nil {
		s.ctx.Logger.Debug("icd.checkin.resolve_failed",
			slog.Uint64("fabric_index", uint64(req.FabricIndex)),
			slog.Uint64("peer_node_id", req.PeerNodeID), slog.Any("err", err))
		return false, nil
	}
	if addr == nil {
		return false, nil
	}
	payload, err := checkin.EncodeICD(req.Key, req.Counter, req.ActiveModeThreshold)
	if err != nil {
		return false, err
	}
	ok, err := s.ctx.SendUnsecured(ctx, addr, mrp.SCOpcodeIcdCheckIn, payload)
	if err != nil {
		s.ctx.Logger.Debug("icd.checkin.send_failed",
			slog.Uint64("fabric_index", uint64(req.FabricIndex)),
			slog.Uint64("peer_node_id", req.PeerNodeID), slog.Any("err", err))
		return false, nil
	}
	return ok, nil
}
