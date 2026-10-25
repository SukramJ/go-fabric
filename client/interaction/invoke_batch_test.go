// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

type batchResult struct {
	res []interaction.Result
	err error
}

// TestInvokeBatchOneExchange: three commands within MaxPathsPerInvoke go
// in one InvokeRequest numbered by CommandRef; the answers are matched by
// it whatever their order, a response for no command of the batch is
// dropped, and a command without an answer is NoCommandResponse
// (ClientInteraction.ts:510-517, :616-646 at 862ad75f).
func TestInvokeBatchOneExchange(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	cmds := []interaction.CommandRequest{
		{Path: armPath, Fields: gencomm.ArmFailSafeRequest{ExpiryLengthSeconds: 60}},
		{Path: armPath},
		{Path: armPath},
	}
	done := make(chan batchResult, 1)
	go func() {
		res, err := c.InvokeBatch(context.Background(), cmds, 10, interaction.InvokeOptions{})
		done <- batchResult{res, err}
	}()
	req := s.next(t)
	parsed, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(req.payload), nil)
	if err != nil || len(parsed.Invokes) != 3 {
		t.Fatalf("request %+v (%v)", parsed, err)
	}
	for i, inv := range parsed.Invokes {
		if !inv.HasCommandRef || int(inv.CommandRef) != i {
			t.Fatalf("invoke %d: CommandRef %d (%v)", i, inv.CommandRef, inv.HasCommandRef)
		}
	}
	path := armPath
	path.HasEndpoint, path.HasCluster, path.HasCommand = true, true, true
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeInvokeResponse, invokeResponse(t, false,
		im.InvokeResponseEntry{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusBusy}, CommandRef: 1, HasCommandRef: true},
		im.InvokeResponseEntry{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}, CommandRef: 0, HasCommandRef: true},
		im.InvokeResponseEntry{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}, CommandRef: 9, HasCommandRef: true},
	))
	r := await(t, done)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.res) != 3 || r.res[0].Status != im.StatusSuccess || r.res[1].Status != im.StatusBusy || r.res[2].Status != im.StatusNoCommandResponse {
		t.Fatalf("results %+v", r.res)
	}
}

// TestInvokeBatchRefusals: no command, and a batch size below one.
func TestInvokeBatchRefusals(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	if _, err := c.InvokeBatch(context.Background(), nil, 1, interaction.InvokeOptions{}); !errors.Is(err, interaction.ErrNoCommands) {
		t.Fatalf("no command: %v", err)
	}
	if _, err := c.InvokeBatch(context.Background(), []interaction.CommandRequest{{Path: armPath}}, 0, interaction.InvokeOptions{}); err == nil {
		t.Fatal("a batch size of 0 was accepted")
	}
}
