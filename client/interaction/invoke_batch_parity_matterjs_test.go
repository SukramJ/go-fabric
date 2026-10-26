// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestInvokeBatchSplitsRefLessParity: a two-command invoke for a peer of
// MaxPathsPerInvoke 1 goes as two InvokeRequests of one command each,
// neither with a CommandRef, and each result comes back for its command;
// a stray CommandDataIB for no sent command is discarded
// (packages/protocol/test/action/client/ClientInteractionInvokeTest.ts:153-178,
// :354-394 at 862ad75f; ClientInteraction.ts:510-517, :654-700).
func TestInvokeBatchSplitsRefLessParity(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	on := im.ConcreteCommandPath{Endpoint: 1, Cluster: 0x0006, Command: 1, HasEndpoint: true, HasCluster: true, HasCommand: true}
	off := on
	off.Command = 0
	done := make(chan batchResult, 1)
	go func() {
		res, err := c.InvokeBatch(context.Background(), []interaction.CommandRequest{{Path: on}, {Path: off}}, 1, interaction.InvokeOptions{})
		done <- batchResult{res, err}
	}()
	for range 2 {
		req := s.next(t)
		parsed, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(req.payload), nil)
		if err != nil || len(parsed.Invokes) != 1 || parsed.Invokes[0].HasCommandRef {
			t.Fatalf("request %+v (%v)", parsed, err)
		}
		p := parsed.Invokes[0].Path
		status := im.StatusSuccess
		if p.Command == 0 {
			status = im.StatusBusy
		}
		s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false,
			im.InvokeResponseEntry{Path: p, IsStatus: true, Status: im.StatusIB{Status: status}}))
	}
	r := await(t, done)
	if r.err != nil || len(r.res) != 2 || r.res[0].Status != im.StatusSuccess || r.res[1].Status != im.StatusBusy {
		t.Fatalf("results %+v (%v)", r.res, r.err)
	}

	go func() {
		res, err := c.InvokeBatch(context.Background(), []interaction.CommandRequest{{Path: on}, {Path: off}}, 10, interaction.InvokeOptions{})
		done <- batchResult{res, err}
	}()
	req := s.next(t)
	resp := on
	resp.Command = 0x40
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false,
		im.InvokeResponseEntry{Path: on, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}, CommandRef: 0, HasCommandRef: true},
		im.InvokeResponseEntry{Path: off, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}, CommandRef: 1, HasCommandRef: true},
		im.InvokeResponseEntry{Path: resp, HasResponse: true, Response: gencomm.ArmFailSafeResponse{}, CommandRef: 99, HasCommandRef: true},
	))
	r = await(t, done)
	if r.err != nil || len(r.res) != 2 || r.res[0].Status != im.StatusSuccess || r.res[1].Status != im.StatusSuccess || r.res[0].HasFields {
		t.Fatalf("results %+v (%v)", r.res, r.err)
	}
}
