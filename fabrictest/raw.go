// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// Exchange sends one Interaction Model message — opcode and its TLV
// payload — on a new exchange over the pair's session and returns the
// device's first answer. It is for the request the controller's own
// formulation refuses to send: a wildcard write carrying a DataVersion, a
// write path without its attribute, a request with a field the controller
// never sets — the raw requests matter.js's tests hand the server's
// response classes directly (AttributeWriteResponseTest.ts writeAttrRaw,
// readAttrRaw in read-helpers.ts). The exchange is closed when the answer
// has arrived; an answer that is itself a request for more (a chunked
// report) is the caller's to read no further.
func Exchange(ctx context.Context, p *Pair, opcode uint8, payload []byte) (*exchange.Message, error) {
	var msg *exchange.Message
	err := run(ctx, p, func(ctx context.Context) error {
		x, err := p.Interaction.Provider().InitiateExchange(ctx, interaction.ExchangeOptions{})
		if err != nil {
			return err
		}
		defer x.Close()
		if err := x.Send(ctx, opcode, payload, exchange.SendOptions{}); err != nil {
			return err
		}
		m, err := x.Next(ctx, exchange.ReceiveOptions{})
		if err != nil {
			return err
		}
		if m.ProtocolID != im.InteractionModelProtocolID {
			return interaction.ErrUnexpectedMessage
		}
		msg = m
		return nil
	})
	return msg, err
}

// Send sends one Interaction Model message on a new exchange over the
// pair's session and waits for no answer — a request the device answers
// with nothing (a SuppressResponse invoke or write). The exchange stays
// open for the message's acknowledgement and closes itself
// (exchange.Exchange.Close).
func Send(ctx context.Context, p *Pair, opcode uint8, payload []byte) error {
	return run(ctx, p, func(ctx context.Context) error {
		x, err := p.Interaction.Provider().InitiateExchange(ctx, interaction.ExchangeOptions{})
		if err != nil {
			return err
		}
		defer x.Close()
		return x.Send(ctx, opcode, payload, exchange.SendOptions{})
	})
}
