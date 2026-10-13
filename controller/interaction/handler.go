// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/im"
)

// Register installs the registry's handler of the reports peers push on
// mgr: the first message of every IM exchange a peer opens on a secure
// session reaches [Subscriptions.HandleExchange] — matter.js
// ClientSubscriptionHandler (requiresSecureSession, ClientSubscriptionHandler.ts:23-31)
// as Peers.#configureInteractionServer installs it into the node's
// InteractionServer (packages/node/src/node/client/Peers.ts:546-563,
// packages/node/src/node/server/InteractionServer.ts:218-228).
func (r *Subscriptions) Register(mgr *exchange.Manager) {
	mgr.SetProtocolHandler(im.InteractionModelProtocolID, r.HandleExchange)
}

// HandleExchange handles an IM exchange a peer opened with first. Mirrors
// matter.js ClientSubscriptionHandler (ClientSubscriptionHandler.ts:33-145)
// at 0ceb202d:
//
//   - a first message other than a ReportData is not a client operation:
//     the controller serves no Interaction Model server, so it is answered
//     InvalidAction as InteractionServerMessenger answers an unsupported
//     message (InteractionMessenger.ts:357-361);
//   - while the registry is blocked a report is answered
//     InvalidSubscription (:33-43);
//   - a report without a subscription id is answered InvalidSubscription
//     (:76-86), one for an unknown (peer, id) logged and answered
//     InvalidSubscription (:88-104);
//   - otherwise the subscription is reading and ReportStarted runs (:108-112);
//     an empty report is a keepalive — KeepaliveReceived, then the report's
//     acknowledgement, further chunks read over (:114-128); a report with
//     data is assembled and handed to Updated, a later chunk naming no or
//     another subscription answered InvalidSubscription and ending it
//     (:129-138, processReports :163-196);
//   - in every case the liveness deadline restarts (:139-144).
//
// The chunks are acknowledged as readDataReports does
// (InteractionMessenger.ts:938-972).
func (r *Subscriptions) HandleExchange(x *exchange.Exchange, first *exchange.Message) {
	m := &messenger{x: x}
	ctx := r.readingCtx
	if first.Opcode != im.OpcodeReportData {
		r.log.Debug("matter.controller.interaction.unsupported_message", slog.Int("opcode", int(first.Opcode)))
		r.sendStatus(ctx, m, im.StatusInvalidAction)
		return
	}
	done, ok := r.beginReading()
	if !ok {
		r.sendStatus(ctx, m, im.StatusInvalidSubscription)
		return
	}
	defer done()
	if err := r.handleReport(ctx, m, first); err != nil {
		if ctx.Err() != nil && errors.Is(err, context.Canceled) {
			r.log.Debug("matter.controller.interaction.report_aborted")
			return
		}
		r.log.Info("matter.controller.interaction.report_failed", slog.String("err", err.Error()))
	}
}

// sendStatus answers with a StatusResponse — sendInvalid
// (ClientSubscriptionHandler.ts:150-156) and the InvalidAction answer.
func (r *Subscriptions) sendStatus(ctx context.Context, m *messenger, status im.StatusCode) {
	if err := m.sendStatus(context.WithoutCancel(ctx), status); err != nil {
		r.log.Debug("matter.controller.interaction.status_failed", slog.String("err", err.Error()))
	}
}

// handleReport is ClientSubscriptionHandler.#handleExchange (:62-145).
func (r *Subscriptions) handleReport(ctx context.Context, m *messenger, first *exchange.Message) error {
	initial, err := decodeReport(first.Payload)
	if err != nil {
		return err
	}
	if !initial.HasSubscription {
		r.log.Debug("matter.controller.interaction.report_without_subscription")
		r.sendStatus(ctx, m, im.StatusInvalidSubscription)
		return nil
	}
	session := m.x.Session()
	peer := exchange.PeerAddress{FabricIndex: session.FabricIndex(), NodeID: session.PeerNodeID()}
	sub := r.GetPeer(peer, initial.SubscriptionID)
	if sub == nil {
		r.log.Info("matter.controller.interaction.report_for_unknown_subscription", slog.Uint64("subscription", uint64(initial.SubscriptionID)))
		r.sendStatus(ctx, m, im.StatusInvalidSubscription)
		return nil
	}
	sub.mu.Lock()
	sub.isReading = true
	sub.mu.Unlock()
	defer func() {
		sub.mu.Lock()
		sub.isReading = false
		sub.timeoutAt = time.Time{}
		sub.mu.Unlock()
		r.ResetTimer()
	}()
	if r.reportStarted != nil {
		r.reportStarted(session)
	}
	if len(initial.Reports) == 0 && len(initial.EventReports) == 0 {
		if sub.request.KeepaliveReceived != nil {
			sub.request.KeepaliveReceived()
		}
		chunk := 0
		return readDataReports(ctx, r.log, m, initial, func(*im.ReportData) error {
			chunk++
			if chunk > 1 {
				r.log.Warn("matter.controller.interaction.data_after_keepalive", slog.Uint64("subscription", uint64(initial.SubscriptionID)))
			}
			return nil
		})
	}
	a := newAssembler(r.log)
	return readDataReports(ctx, r.log, m, initial, func(rd *im.ReportData) error {
		if rd != initial && (!rd.HasSubscription || rd.SubscriptionID != initial.SubscriptionID) {
			r.log.Debug("matter.controller.interaction.report_for_another_subscription", slog.Uint64("subscription", uint64(rd.SubscriptionID)))
			r.sendStatus(ctx, m, im.StatusInvalidSubscription)
			return errStopReports
		}
		a.add(rd)
		if !rd.MoreChunkedMessages && sub.request.Updated != nil {
			sub.request.Updated(a.report)
		}
		return nil
	})
}

// beginReading tracks an in-flight report read; it fails once the
// registry is blocked, the check and the tracking under one lock
// (beginReading, :97-123, with the isBlocked check of
// ClientSubscriptionHandler.ts:35-46).
func (r *Subscriptions) beginReading() (done func(), ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.blocked {
		return nil, false
	}
	r.inFlight++
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.inFlight--
		if r.inFlight == 0 && r.drained != nil {
			close(r.drained)
			r.drained = nil
		}
	}, true
}
