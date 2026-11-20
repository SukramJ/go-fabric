// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
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
// at db021da0:
//
//   - a first message other than a ReportData is not a client operation:
//     the controller serves no Interaction Model server, so it is answered
//     InvalidAction as InteractionServerMessenger answers an unsupported
//     message (InteractionMessenger.ts:415-419);
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
//     (:129-138, processReports :155-188);
//   - in every case the liveness deadline restarts (:139-144).
//
// The chunks are acknowledged as readDataReports does
// (InteractionMessenger.ts:1012-1053).
func (r *Subscriptions) HandleExchange(x *exchange.Exchange, first *exchange.Message) {
	m := &messenger{x: x, maxReport: r.maxReport}
	ctx := r.readingCtx
	if s := r.serverFor(first.Opcode); s != nil {
		// A request for the endpoints this stack serves itself
		// ([Subscriptions.Serve]): matter.js's InteractionServer takes
		// every first message that is not a report
		// (InteractionServer.ts:240-249). Once new activity is blocked it
		// ignores anything newly incoming (:241-244 at db021da0), and an
		// exchange it serves is tracked as node activity (:257-260), so
		// the request is closed unanswered here and counted with the
		// report reads BlockNewActivity waits for (review II,
		// R2-P1-net-04).
		done, ok := r.beginReading()
		if !ok {
			r.log.Debug("matter.controller.interaction.served_request_blocked", slog.Int("opcode", int(first.Opcode)))
			x.Close()
			return
		}
		defer done()
		// Only a CASE session carries the fabric and subject the request
		// context is built from (server.requestContext): a request on any
		// other — a commissionee's PASE session, which has no fabric — is
		// refused before it is decoded or dispatched, as the access check
		// would refuse it for want of an accessing fabric (review II,
		// R2-P6-acl-2108).
		if x.Session().FabricIndex() == 0 {
			r.log.Debug("matter.controller.interaction.served_request_without_fabric", slog.Int("opcode", int(first.Opcode)))
			r.sendStatus(ctx, m, im.StatusUnsupportedAccess)
			x.Close()
			return
		}
		s.handle(ctx, x, first)
		return
	}
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
		r.log.Info("matter.controller.interaction.report_failed", slog.Any("err", err))
	}
}

// sendStatus answers with a StatusResponse — sendInvalid
// (ClientSubscriptionHandler.ts:150-156) and the InvalidAction answer.
func (r *Subscriptions) sendStatus(ctx context.Context, m *messenger, status im.StatusCode) {
	if err := m.sendStatus(context.WithoutCancel(ctx), status); err != nil {
		r.log.Debug("matter.controller.interaction.status_failed", slog.Any("err", err))
	}
}

// handleReport is ClientSubscriptionHandler.#handleExchange (:62-145).
//
// A report whose status the peer never acknowledged is handed to the
// subscription once the report ends, unless the registry is blocked
// (shutdown); a missing acknowledgement of a chunk's status, which ends
// the report, is no failure of it. Mirrors matter.js
// ClientSubscriptionHandler.ts:107 and :142-158 at b25ed960.
func (r *Subscriptions) handleReport(ctx context.Context, m *messenger, first *exchange.Message) (err error) {
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
		r.log.Info("matter.controller.interaction.report_for_unknown_subscription", slog.Uint64("subscription_id", uint64(initial.SubscriptionID)))
		r.sendStatus(ctx, m, im.StatusInvalidSubscription)
		return nil
	}
	reportNumber := sub.nextReportNumber()
	sub.mu.Lock()
	sub.isReading = true
	sub.mu.Unlock()
	defer func() {
		if err != nil && m.unacknowledgedReportStatus == reportStatusChunk && isAckMissing(err) {
			err = nil
		}
		sub.mu.Lock()
		sub.isReading = false
		sub.timeoutAt = time.Time{}
		sub.mu.Unlock()
		r.ResetTimer()
		if unacknowledged := m.unacknowledgedReportStatus; unacknowledged != "" {
			if r.IsBlocked() {
				r.log.Debug("matter.controller.interaction.report_status_unacknowledged_during_shutdown",
					slog.Uint64("subscription_id", uint64(initial.SubscriptionID)))
			} else {
				sub.reportStatusUnacknowledged(reportNumber, unacknowledged)
			}
		}
	}()
	r.announceReport(session)
	if len(initial.Reports) == 0 && len(initial.EventReports) == 0 {
		if sub.request.KeepaliveReceived != nil {
			sub.request.KeepaliveReceived()
		}
		chunk := 0
		return readDataReports(ctx, r.log, m, initial, first.Payload, func(*im.ReportData) error {
			chunk++
			if chunk > 1 {
				r.log.Warn("matter.controller.interaction.data_after_keepalive", slog.Uint64("subscription_id", uint64(initial.SubscriptionID)))
			}
			return nil
		})
	}
	a := newAssembler(r.log)
	a.report.Session = session
	return readDataReports(ctx, r.log, m, initial, first.Payload, func(rd *im.ReportData) error {
		if rd != initial && (!rd.HasSubscription || rd.SubscriptionID != initial.SubscriptionID) {
			r.log.Debug("matter.controller.interaction.report_for_another_subscription", slog.Uint64("subscription_id", uint64(rd.SubscriptionID)))
			r.sendStatus(ctx, m, im.StatusInvalidSubscription)
			return errStopReports
		}
		a.add(rd)
		if !rd.MoreChunkedMessages && sub.request.Updated != nil {
			// A failure of the consumer leaves the report unanswered and
			// ends its exchange — matter.js's subscription handler lets
			// updated's rejection out (ClientSubscriptionHandler.ts:130-131
			// at db021da0).
			return sub.request.Updated(a.report)
		}
		return nil
	})
}

// announceReport runs ReportStarted; a panic in it is logged and
// contained, so a broken observer does not abort the report being read.
// Mirrors matter.js
// packages/protocol/src/action/client/subscription/ClientSubscriptions.ts:156-158
// (the Observable's error handler logs the observer's error).
func (r *Subscriptions) announceReport(session *exchange.Session) {
	if r.reportStarted == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			r.log.Warn("matter.controller.interaction.report_started_panic", slog.String("panic", fmt.Sprint(p)))
		}
	}()
	r.reportStarted(session)
}

// beginReading tracks an in-flight report read or served request; it
// fails once the registry is blocked, the check and the tracking under one lock
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
