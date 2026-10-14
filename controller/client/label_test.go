// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
)

// noInvoke asserts the manager sent no InvokeRequest since the last
// message read.
func (sr *scriptedRegistry) noInvoke(t *testing.T) {
	t.Helper()
	time.Sleep(20 * time.Millisecond)
	for {
		select {
		case b := <-sr.conn.ch:
			hdr, hl, err := message.UnmarshalHeader(b)
			if err != nil {
				t.Fatal(err)
			}
			plain, _, err := sr.scripted.peer.Decrypt(&hdr, hdr.NonceSecurityFlags(), b[hl:])
			if err != nil {
				t.Fatal(err)
			}
			if proto, _, err := message.UnmarshalProtocolHeader(plain); err == nil && proto.ProtocolID == im.InteractionModelProtocolID && proto.Opcode == im.OpcodeInvokeRequest {
				t.Fatal("an invoke went out")
			}
		default:
			return
		}
	}
}

// expectLabel reads the next message: an UpdateFabricLabel with label.
func (sr *scriptedRegistry) expectLabel(t *testing.T, label string) msg {
	t.Helper()
	inv := sr.next(t)
	cmd, fields := invokeOf(t, inv)
	if el, ok := fieldOf(fields, 0); cmd != cmdUpdateLabel || !ok || el.String != label {
		t.Fatalf("command %d fields %+v", cmd, fields)
	}
	return inv
}

// TestLabelReconciliation: NetworkClient.#reconcileFabricLabel — once per
// start, on the first SubscriptionAlive: nothing when the peer's own entry
// (CurrentFabricIndex's) carries our label; one UpdateFabricLabel with our
// label when it differs, a non-Ok status or an error only noted; a second
// SubscriptionAlive does nothing; Stop and Start reconcile again.
func TestLabelReconciliation(t *testing.T) {
	t.Parallel()
	logs := &msgLog{}
	sr := newScriptedRegistry(t, seedCommissioned(3), func(c *client.RegistryConfig) { c.Logger = slog.New(logs) })
	n := sr.reg.Get("peer1")

	// The peer's label differs: one UpdateFabricLabel, refused.
	sr.subscribe(t, n, sr.imageWithOpCreds("stale"), 33)
	inv := sr.expectLabel(t, "ours")
	sr.nocResponse(t, inv, 10)
	client.WaitRegistry(sr.reg)
	if !logs.has("matter.controller.client.registry.label_not_reconciled") {
		t.Fatal("the refusal was not noted")
	}
	// Another SubscriptionAlive: nothing.
	sr.pushEvents(t, 33)
	waitUntil(t, "the second keepalive", func() bool { sr.hooks.mu.Lock(); defer sr.hooks.mu.Unlock(); return sr.hooks.alive >= 2 })
	client.WaitRegistry(sr.reg)
	sr.noInvoke(t)

	// Stop and Start: reconciled again; an error is noted.
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	sr.subscribe(t, n, nil, 34)
	inv = sr.expectLabel(t, "ours")
	sr.reply(t, inv, im.InteractionModelProtocolID, im.OpcodeStatusResponse, encode(t, func(e *tlv.Encoder) { im.StatusResponse{Status: im.StatusFailure}.MarshalTLV(e) }))
	client.WaitRegistry(sr.reg)
	if !logs.has("matter.controller.client.registry.label_reconcile_failed") {
		t.Fatal("the failure was not noted")
	}

	// The label the peer reports is ours: nothing.
	_ = n.Stop(context.Background())
	sr.subscribe(t, n, sr.opCredsImage("ours"), 35)
	client.WaitRegistry(sr.reg)
	sr.noInvoke(t)
}
