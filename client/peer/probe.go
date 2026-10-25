// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// probeMaxRetransmissions caps a liveness probe's transmissions — matter.js
// PROBE_MAX_RETRANSMISSIONS (packages/protocol/src/peer/PeerAddressMonitor.ts:26-27
// at 0ceb202d).
const probeMaxRetransmissions = 2

// Tags of ReportDataMessage the probe reads (Matter §10.7.3).
const (
	tagReportMoreChunked      = 3
	tagReportSuppressResponse = 4
)

// probe checks that s still reaches the peer with an empty read — matter.js
// ClientInteraction.probe (action/client/ClientInteraction.ts:300-360):
// an IM exchange on the existing session, never a new connection
// (requireExistingSession, :321), to override instead of the session's
// address when it is set (:320); a ReadRequest without paths and not
// fabric-filtered (Read({fabricFilter: false}), :331) sent with
// suppressed peer loss and at most probeMaxRetransmissions transmissions
// (:332-335); then the reports as InteractionMessenger.readDataReports
// reads them (interaction/InteractionMessenger.ts:938-972) — a
// StatusResponse Success after a chunk with more to come, and after the
// last one unless it suppresses the response, the last one not awaited.
// It reports true on the final report and false on any error or the end of
// ctx (:345-355); the exchange is closed either way (:356-359).
//
// With a network profile the probe takes a slot of it, released when the
// exchange closes, and pads the exchange with margin — the probe network
// PeerAddressMonitor passes (PeerAddressMonitor.ts:202-211 at 862ad75f)
// and the medium margin PeerExchangeProvider.initiateExchange applies
// (PeerExchangeProvider.ts:87-92, :136-141).
func probe(ctx context.Context, mgr *exchange.Manager, s *exchange.Session, override *net.UDPAddr, log *slog.Logger, network *NetworkProfile, margin *time.Duration) bool {
	if ctx.Err() != nil || s == nil || s.IsClosed() || s.IsPeerLost() {
		return false
	}
	opts := exchange.Options{AddressOverride: override, PeerAdditionalMrpDelay: margin}
	if network != nil {
		slot, err := network.Semaphore.Obtain(ctx)
		if err != nil {
			return false
		}
		opts.OnClosing = slot.Close
	}
	x, err := mgr.Initiate(s, im.InteractionModelProtocolID, opts)
	if err != nil {
		if opts.OnClosing != nil {
			opts.OnClosing()
		}
		return false
	}
	err = readEmpty(ctx, x)
	if err != nil {
		if ctx.Err() == nil {
			log.Info("matter.controller.peer.probe", slog.String("result", "failed"), slog.Any("err", err))
		}
		x.Close()
		return false
	}
	log.Info("matter.controller.peer.probe", slog.String("result", "success"))
	return true
}

// errNotAReport is a probe reply that is not a ReportData.
var errNotAReport = errors.New("peer: probe reply is not a ReportData")

// readEmpty runs the probe's read on x and closes x once its last message
// is out.
func readEmpty(ctx context.Context, x *exchange.Exchange) error {
	enc := tlv.NewEncoder()
	im.ReadRequest{FabricFiltered: false}.MarshalTLV(enc)
	req, err := enc.Bytes()
	if err != nil {
		return err
	}
	if err := x.Send(ctx, im.OpcodeReadRequest, req, exchange.SendOptions{SuppressPeerLoss: true, MaxRetransmissions: probeMaxRetransmissions}); err != nil {
		return err
	}
	success := statusSuccess()
	for {
		msg, err := x.Next(ctx, exchange.ReceiveOptions{})
		if err != nil {
			return err
		}
		if msg.ProtocolID != im.InteractionModelProtocolID || msg.Opcode != im.OpcodeReportData {
			return errNotAReport
		}
		more, suppress, err := reportFlags(msg.Payload)
		if err != nil {
			return err
		}
		if more {
			if err := x.Send(ctx, im.OpcodeStatusResponse, success, exchange.SendOptions{}); err != nil {
				return err
			}
			continue
		}
		if suppress {
			x.Close()
			return nil
		}
		// The final StatusResponse is tracked, not awaited (:950-966).
		go func() {
			_ = x.Send(context.WithoutCancel(ctx), im.OpcodeStatusResponse, success, exchange.SendOptions{})
			x.Close()
		}()
		return nil
	}
}

func statusSuccess() []byte {
	enc := tlv.NewEncoder()
	im.StatusResponse{Status: im.StatusSuccess}.MarshalTLV(enc)
	b, _ := enc.Bytes()
	return b
}

// reportFlags reads MoreChunkedMessages and SuppressResponse of a
// ReportDataMessage.
func reportFlags(payload []byte) (more, suppress bool, err error) {
	dec := tlv.NewDecoder(payload)
	top, err := dec.Next()
	if err != nil {
		return false, false, err
	}
	if !top.IsContainer || top.Type != tlv.TypeStructure {
		return false, false, errNotAReport
	}
	depth := 0
	for {
		el, err := dec.Next()
		if err != nil {
			return false, false, err
		}
		switch {
		case el.IsEndContainer && depth == 0:
			return more, suppress, nil
		case el.IsEndContainer:
			depth--
		case el.IsContainer:
			depth++
		case depth == 0 && el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == tagReportMoreChunked:
			more = el.Bool
		case depth == 0 && el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == tagReportSuppressResponse:
			suppress = el.Bool
		}
	}
}
