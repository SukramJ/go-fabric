// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/client/exchange"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// The OTA provider on the controller's own identity: the provider
// endpoint of the controller's tree (serve.go), served to the nodes of the
// fabric as RootNodeID — matter.js's OtaSoftwareUpdateProviderServer on
// the controller's ServerNode, whose announced providerNodeId is the
// fabric's root node id and whose endpoint is one of the controller's own
// (OtaAnnouncements.ts:47-58, :272-278 at 862ad75f). matterjs-server adds
// that endpoint (packages/ws-controller/src/controller/ControllerNode.ts:140-141
// at 0bd06f2). Until phase 37 the provider ran
// on a second node of its own (`node/controllernode`, `OTAConfig.StartNode`);
// the migration below retires that node's record.

// otaProvider is the provider server with its BDX responder, mounted at
// [OTAProviderEndpoint].
type otaProvider struct {
	server  *mattercore.OTAProvider
	bdx     *bdx.Responder
	restore func()
	// ctx bounds the transfers the responder serves; stop ends them.
	ctx  context.Context
	stop context.CancelFunc
	// receiveHook is a test seam (export_ota_test.go): when set, a BDX
	// exchange calls it on the flow's goroutine with every message it
	// hands the flow, before the flow takes it.
	receiveHook atomic.Pointer[func(context.Context, bdx.Message)]
}

// newOTAProvider composes node/'s OTA provider ([mattercore.NewOTAProvider])
// over the update manager's ports — image source, consent and apply
// policy — with the manager's scripted commands in front of it, as
// node/controllernode wrapped them for the own node.
func (c *Controller) newOTAProvider(ctx context.Context, mgr *softwareupdate.Manager, bdxDelay *time.Duration) (*otaProvider, error) {
	p := mgr.ProviderConfig()
	responder := bdx.NewResponder(c.log)
	responder.SetClock(c.clock)
	if bdxDelay != nil {
		responder.SetAdditionalMrpDelay(*bdxDelay)
	}
	ours := c.fabricIndex()
	srv, err := mattercore.NewOTAProvider(mattercore.OTAProviderConfig{
		ImageSource: p.ImageSource, Consent: p.Consent, ApplyPolicy: p.ApplyPolicy,
		BDX: responder, BDXMaxBlockSize: p.BDXMaxBlockSize,
		NodeID: func(fabricIndex uint8) (uint64, bool) {
			if fabricIndex != ours {
				return 0, false
			}
			return c.fabric.RootNodeID, true
		},
		OnStatusChange: p.OnStatusChange, RebootQuery: p.RebootQuery, TransferBlockSize: p.TransferBlockSize,
		Clock: c.clock, Logger: c.log,
	})
	if err != nil {
		return nil, err
	}
	o := &otaProvider{server: srv, bdx: responder}
	// The transfers outlive the constructor's context and end with
	// Close; the exchange manager's handler itself carries none.
	o.ctx, o.stop = context.WithCancel(context.WithoutCancel(ctx)) //nolint:contextcheck // bounded by the provider's lifetime, not the call's
	o.restore = wrapProviderCommands(srv, p.Commands)
	c.mgr.SetProtocolHandler(bdx.ProtocolID, serveBDX(o.ctx, responder, &o.receiveHook)) //nolint:contextcheck // the handler has no context of its own
	return o, nil
}

// serveBDX is the BDX protocol handler on the controller's exchange
// manager: a node that downloads an image opens a BDX exchange to the
// provider, and the responder serves it over that exchange — the device
// side's bridge.deliverRespondedBDX on the client stack's exchanges.
func serveBDX(ctx context.Context, r *bdx.Responder, hook *atomic.Pointer[func(context.Context, bdx.Message)]) exchange.ProtocolHandler {
	return func(x *exchange.Exchange, first *exchange.Message) {
		sess := x.Session()
		peer := bdx.Peer{FabricIndex: sess.FabricIndex(), NodeID: sess.PeerNodeID(), SessionID: sess.ID()}
		msg := bdx.Message{ProtocolID: first.ProtocolID, Opcode: first.Opcode, Payload: first.Payload}
		ex := &bdxExchange{x: x, hook: hook}
		x.SetReceiveNotifier(ex.arrived)
		defer func() { _ = ex.Close() }()
		r.HandleExchange(ctx, peer, msg, ex)
	}
}

// bdxExchange is a client-stack exchange as the BDX responder's
// [bdx.Exchange]: a StatusReport travels on the Secure Channel protocol by
// the exchange's own rule (exchange.Send), every other message on BDX. It
// is a [bdx.ReceiveNotifier] through the exchange's receive notifier
// ([exchange.Exchange.SetReceiveNotifier]), so the outbound flow finishes
// a transfer when its final BlockAckEof arrives, before the receive path
// decodes the requestor's ApplyUpdateRequest.
type bdxExchange struct {
	x *exchange.Exchange
	// notify is the flow's [bdx.ReceiveNotifier] callback.
	notify atomic.Pointer[func(bdx.Message)]
	// hook is the provider's receive seam ([otaProvider].receiveHook).
	hook *atomic.Pointer[func(context.Context, bdx.Message)]
}

// OnReceive implements [bdx.ReceiveNotifier].
func (e *bdxExchange) OnReceive(fn func(bdx.Message)) { e.notify.Store(&fn) }

// arrived is the exchange's receive notifier, on the receive goroutine:
// it passes the flow the messages Receive returns — a duplicate and a
// standalone acknowledgement are not among them (exchange.Next).
func (e *bdxExchange) arrived(m *exchange.Message, duplicate bool) {
	if duplicate || (m.ProtocolID == mrp.SecureChannelProtocolID && m.Opcode == mrp.StandaloneAckOpcode) {
		return
	}
	if fn := e.notify.Load(); fn != nil {
		(*fn)(bdx.Message{ProtocolID: m.ProtocolID, Opcode: m.Opcode, Payload: m.Payload})
	}
}

func (e *bdxExchange) Send(ctx context.Context, _ uint16, opcode uint8, payload []byte) error {
	return e.x.Send(ctx, opcode, payload, exchange.SendOptions{})
}

func (e *bdxExchange) Receive(ctx context.Context) (bdx.Message, error) {
	m, err := e.x.Next(ctx, exchange.ReceiveOptions{Timeout: bdx.TransferIdleTimeout})
	if err != nil {
		return bdx.Message{}, err
	}
	msg := bdx.Message{ProtocolID: m.ProtocolID, Opcode: m.Opcode, Payload: m.Payload}
	if e.hook != nil {
		if fn := e.hook.Load(); fn != nil {
			(*fn)(ctx, msg)
		}
	}
	return msg, nil
}

func (e *bdxExchange) MaxPayloadSize() int { return e.x.Session().MaxPayloadSize() }

func (e *bdxExchange) Close() error {
	e.x.Close()
	return nil
}

var _ bdx.ReceiveNotifier = (*bdxExchange)(nil)

// wrapProviderCommands puts the manager's scripted commands
// ([softwareupdate.ProviderCommands]) in front of the provider's own
// handlers: a script answers for the requestor it is written for, the
// provider for every other. The returned function restores the handlers.
func wrapProviderCommands(p *mattercore.OTAProvider, cmds *softwareupdate.ProviderCommands) (restore func()) {
	if p == nil || cmds == nil {
		return func() {}
	}
	if cmds.QueryImage != nil {
		_ = p.Handle(provdef.CmdQueryImage, func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.QueryImageRequest](fields)
			if err != nil {
				return nil, err
			}
			own := func(ctx context.Context) (provdef.QueryImageResponse, error) { return p.QueryImage(ctx, req) }
			peer, ok := requestorOf(ctx)
			if !ok {
				return own(ctx)
			}
			return cmds.QueryImage(ctx, peer, req, own)
		})
	}
	if cmds.ApplyUpdate != nil {
		_ = p.Handle(provdef.CmdApplyUpdateRequest, func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.ApplyUpdateRequestRequest](fields)
			if err != nil {
				return nil, err
			}
			own := func(ctx context.Context) (provdef.ApplyUpdateResponse, error) { return p.ApplyUpdateRequest(ctx, req) }
			peer, ok := requestorOf(ctx)
			if !ok {
				return own(ctx)
			}
			return cmds.ApplyUpdate(ctx, peer, req, own)
		})
	}
	if cmds.NotifyUpdateApplied != nil {
		_ = p.Handle(provdef.CmdNotifyUpdateApplied, func(ctx context.Context, fields any) (any, error) {
			req, err := fieldsAs[provdef.NotifyUpdateAppliedRequest](fields)
			if err != nil {
				return nil, err
			}
			own := func(ctx context.Context) error { return p.NotifyUpdateApplied(ctx, req) }
			peer, ok := requestorOf(ctx)
			if !ok {
				return nil, own(ctx)
			}
			return nil, cmds.NotifyUpdateApplied(ctx, peer, req, own)
		})
	}
	return func() {
		for _, cmd := range []uint32{provdef.CmdQueryImage, provdef.CmdApplyUpdateRequest, provdef.CmdNotifyUpdateApplied} {
			_ = p.Handle(cmd, nil)
		}
	}
}

// requestorOf is the requestor a provider command came from: the
// session's fabric and subject, which a CASE session carries.
func requestorOf(ctx context.Context) (mattercore.OTAPeer, bool) {
	_, fabric := im.FabricFilterFromContext(ctx)
	nodeID, _ := im.SubjectFromContext(ctx)
	if fabric == 0 || nodeID == 0 || im.IsPASEFromContext(ctx) {
		return mattercore.OTAPeer{}, false
	}
	return mattercore.OTAPeer{FabricIndex: fabric, NodeID: nodeID}, true
}

func fieldsAs[T any](fields any) (T, error) {
	switch f := fields.(type) {
	case *T:
		if f != nil {
			return *f, nil
		}
	case T:
		return f, nil
	}
	var zero T
	return zero, invalidCommand(fmt.Sprintf("controller: command fields %T", fields))
}

type invalidCommand string

func (e invalidCommand) Error() string { return string(e) }

func (invalidCommand) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

// ownNodeKey is the settings key the own node of releases before phase 37
// was kept under: its passcode, discriminator, salt and node id.
const ownNodeKey = "controller.ownnode"

// retireOwnNode is the migration of a database that ran the OTA provider
// on an own node: the node's record goes, and the registry forgets the
// node, which no longer exists — the controller is the provider now, and
// the next announcement round rewrites every node's DefaultOTAProviders
// (softwareupdate/announcements.go announceDefaultProvider, which writes
// our record unless the node holds the same one).
func (c *Controller) retireOwnNode(ctx context.Context, st *store.Store, log *slog.Logger) error {
	v, ok, err := st.GetSetting(ctx, ownNodeKey)
	if err != nil || !ok {
		return err
	}
	var rec struct {
		NodeID uint64 `json:"nodeId"`
	}
	if err := json.Unmarshal([]byte(v), &rec); err != nil {
		return fmt.Errorf("%s: %w", ownNodeKey, err)
	}
	if rec.NodeID != 0 {
		addr := c.fabric.AddressOf(rec.NodeID)
		if n := c.reg.GetByAddress(addr); n != nil {
			if err := c.reg.Forget(ctx, n); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("controller.ota.own_node.forget", slog.Any("err", err))
			}
		}
	}
	if err := st.DeleteSetting(ctx, ownNodeKey); err != nil {
		return err
	}
	log.Info("controller.ota.own_node.retired", slog.String("node", fmt.Sprintf("%016X", rec.NodeID)))
	return nil
}
