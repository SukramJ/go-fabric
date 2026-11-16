// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	messagesdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
	tsdef "github.com/SukramJ/go-fabric/cluster/spec/timesynchronization"
	"github.com/SukramJ/go-fabric/im"
)

// TestReadsAreDeepCopies (review II, decision 142): a stateful server of
// the package hands out the generated state it holds through the
// generated Clone — a reader that changes what it got, down to a slice's
// bytes or a pointer's target, changes nothing the server serves. The
// standing guard of the class in this package: a server added to the
// table below that hands out a shallow copy fails here.
func TestReadsAreDeepCopies(t *testing.T) {
	t.Parallel()
	t.Run("Messages", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newMessages(t, messagesdef.FeatureReceivedConfirmation|messagesdef.FeatureConfirmationResponse)
		resp := []messagesdef.MessageResponseOptionStruct{{MessageResponseId: 1, Label: "yes"}}
		req := presentRequest(messageID(3), "hello")
		req.Responses = &resp
		if err := decodeAndInvoke(t, m, msgCtx(1, true), messagesdef.CmdPresentMessagesRequest, req); err != nil {
			t.Fatal(err)
		}
		for _, read := range []func() spec.List[messagesdef.MessageStruct]{
			func() spec.List[messagesdef.MessageStruct] {
				v, _ := m.MatterRead(messagesdef.AttrMessages)
				return v.(spec.List[messagesdef.MessageStruct])
			},
			func() spec.List[messagesdef.MessageStruct] {
				v, _ := m.MatterReadFiltered(msgCtx(1, true), messagesdef.AttrMessages)
				return v.(spec.List[messagesdef.MessageStruct])
			},
		} {
			got := read()
			got[0].MessageId[0] ^= 0xFF
			(*got[0].Responses)[0].Label = "no"
			again := read()
			if again[0].MessageId[0] != 3 || (*again[0].Responses)[0].Label != "yes" {
				t.Errorf("a reader's change reached the server: %+v", again[0])
			}
		}
	})
	t.Run("TimeSynchronization", func(t *testing.T) {
		t.Parallel()
		srv, _, _ := tsServer(t, core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeZone})
		ctx := im.WithFabricFilter(context.Background(), false, 1)
		if _, err := srv.MatterInvoke(ctx, tsdef.CmdSetTimeZone, tsdef.SetTimeZoneRequest{TimeZone: []tsdef.TimeZoneStruct{{Offset: 3600, Name: name("Europe/Berlin")}}}); err != nil {
			t.Fatal(err)
		}
		v, _ := srv.MatterRead(tsdef.AttrTimeZone)
		*v.(spec.List[tsdef.TimeZoneStruct])[0].Name = "changed"
		again, _ := srv.MatterRead(tsdef.AttrTimeZone)
		if n := again.(spec.List[tsdef.TimeZoneStruct])[0].Name; n == nil || *n != "Europe/Berlin" {
			t.Errorf("a reader's change reached TimeZone: %v", n)
		}
	})
}
