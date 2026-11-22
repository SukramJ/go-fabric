// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"sync/atomic"

	"thingfabric.dev/go-fabric/bdx"
)

// BDXExchange is an exchange of this stack as a [bdx.Exchange]: a BDX
// transfer's messages go over it, a StatusReport on the Secure Channel
// protocol by the exchange's own rule ([Exchange.Send]), every other message
// on the exchange's protocol. Open the exchange on [bdx.ProtocolID]
// ([Manager.Initiate]) for a transfer this node starts — the OTA
// requestor's download from its provider.
//
// It is a [bdx.ReceiveNotifier] through the exchange's queue notifier
// ([Exchange.SetQueueNotifier]): the notifier sees each message as Receive
// will return it. It reports Reliable for an exchange on a TCP session, the
// one on which an OTA requestor asks for its larger blocks
// (cluster/core.OtaProviderSession.OpenExchange).
type BDXExchange struct {
	x      *Exchange
	notify atomic.Pointer[func(bdx.Message)]
}

// NewBDXExchange wraps x; closing the result closes x.
func NewBDXExchange(x *Exchange) *BDXExchange {
	e := &BDXExchange{x: x}
	x.SetQueueNotifier(e.queued)
	return e
}

var (
	_ bdx.Exchange        = (*BDXExchange)(nil)
	_ bdx.ReceiveNotifier = (*BDXExchange)(nil)
)

// Send implements [bdx.Exchange].
func (e *BDXExchange) Send(ctx context.Context, _ uint16, opcode uint8, payload []byte) error {
	return e.x.Send(ctx, opcode, payload, SendOptions{})
}

// Receive implements [bdx.Exchange]: the peer's next message, waiting at
// most [bdx.TransferIdleTimeout] beyond ctx.
func (e *BDXExchange) Receive(ctx context.Context) (bdx.Message, error) {
	m, err := e.x.Next(ctx, ReceiveOptions{Timeout: bdx.TransferIdleTimeout})
	if err != nil {
		return bdx.Message{}, err
	}
	return bdx.Message{ProtocolID: m.ProtocolID, Opcode: m.Opcode, Payload: m.Payload}, nil
}

// MaxPayloadSize implements [bdx.Exchange]: the session's.
func (e *BDXExchange) MaxPayloadSize() int { return e.x.Session().MaxPayloadSize() }

// Close implements [bdx.Exchange].
func (e *BDXExchange) Close() error {
	e.x.Close()
	return nil
}

// Reliable reports whether the exchange runs on a TCP session.
func (e *BDXExchange) Reliable() bool { return e.x.Session().Transport() == TransportTCP }

// OnReceive implements [bdx.ReceiveNotifier].
func (e *BDXExchange) OnReceive(fn func(bdx.Message)) { e.notify.Store(&fn) }

// queued is the exchange's queue notifier, on the receive goroutine.
func (e *BDXExchange) queued(m *Message) {
	if fn := e.notify.Load(); fn != nil {
		(*fn)(bdx.Message{ProtocolID: m.ProtocolID, Opcode: m.Opcode, Payload: m.Payload})
	}
}
