// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	messagesdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// display is a MessagePresenter that records what it was asked to show.
type display struct {
	shown     []messagesdef.MessageStruct
	cancelled [][]byte
	err       error
}

func (d *display) PresentMessage(_ context.Context, msg messagesdef.MessageStruct) error {
	if d.err != nil {
		return d.err
	}
	d.shown = append(d.shown, msg)
	return nil
}

func (d *display) CancelMessage(_ context.Context, id []byte) error {
	if d.err != nil {
		return d.err
	}
	d.cancelled = append(d.cancelled, id)
	return nil
}

// msgEvents records the events of a Messages server.
type msgEvents struct{ ids []uint32 }

func (e *msgEvents) MatterEmitEvent(_ uint16, _, event uint32, _ any, _ contract.EventPriority) {
	e.ids = append(e.ids, event)
}

func messageID(b byte) []byte { return bytes.Repeat([]byte{b}, 16) }

// fabricCtx is a request from fabric f.
func msgCtx(f uint8, filtered bool) context.Context {
	return im.WithFabricFilter(context.Background(), filtered, f)
}

func presentRequest(id []byte, text string) messagesdef.PresentMessagesRequestRequest {
	return messagesdef.PresentMessagesRequestRequest{
		MessageId: id, Priority: messagesdef.MessagePriorityMedium,
		StartTime: spec.Nullable[uint32]{Null: true}, Duration: spec.Nullable[uint64]{Null: true}, MessageText: text,
	}
}

// decodeAndInvoke sends a request through the generated decoder, as the
// bridge does.
func decodeAndInvoke(t *testing.T, m *mattercore.Messages, ctx context.Context, cmdID uint32, req spec.Encodable) error { //nolint:revive // ctx after the fixtures reads as the call it stands for
	t.Helper()
	fields, err := messagesdef.Definition.Command(cmdID, spec.Request).Decode(spectest.Decode(t, spectest.Encode(t, req)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.MatterInvoke(ctx, cmdID, fields)
	return err
}

func newMessages(t *testing.T, features messagesdef.Feature) (*mattercore.Messages, *display, *msgEvents) {
	t.Helper()
	d := &display{}
	m, err := mattercore.NewMessages(mattercore.MessagesConfig{Presenter: d, Features: features})
	if err != nil {
		t.Fatal(err)
	}
	ev := &msgEvents{}
	m.SetMatterEventEmitter(ev)
	m.SetEndpoint(1)
	return m, d, ev
}

// TestParityMatterJS_MessagesDefinition: the server serves the generated
// definition (messages.element.ts: cluster 0x97 revision 3, :19-21), the
// Messages list fabric-scoped (:31-34, "R F V"), both commands "F O"
// (:65-91), the three events mandatory (:39-63); spectest.CheckServer
// holds the selection to the conformance.
func TestParityMatterJS_MessagesDefinition(t *testing.T) {
	t.Parallel()
	spectest.CheckDefinition(t, messagesdef.Definition)
	m, _, _ := newMessages(t, messagesdef.FeatureReceivedConfirmation|messagesdef.FeatureConfirmationResponse)
	if m.MatterClusterID() != 0x0097 || messagesdef.Revision != 3 {
		t.Fatalf("cluster 0x%04X revision %d", m.MatterClusterID(), messagesdef.Revision)
	}
	spectest.CheckServer(t, m, messagesdef.Definition, uint32(messagesdef.FeatureReceivedConfirmation|messagesdef.FeatureConfirmationResponse))
	if a := messagesdef.Definition.Attribute(messagesdef.AttrMessages); a.Access.Fabric != "F" {
		t.Errorf("Messages access %+v, want fabric-scoped", a.Access)
	}
	if got := m.MatterEvents(); !slices.Equal(got, []uint32{0, 1, 2}) {
		t.Errorf("EventList %v", got)
	}
	// The generated server refuses the fabric-scoped list, which is why
	// this server is core-held.
	if _, err := spec.NewServer(messagesdef.Definition, spec.Options{}, spec.ServerConfig{}); !errors.Is(err, spec.ErrFabricScoped) {
		t.Errorf("spec.NewServer: %v, want ErrFabricScoped", err)
	}
}

// TestMessagesPerFabric: each fabric's messages are its own — a
// fabric-filtered read lists the reader's, an unfiltered one and a local
// read every fabric's — after Binding's per-fabric filtering.
func TestMessagesPerFabric(t *testing.T) {
	t.Parallel()
	m, d, ev := newMessages(t, 0)
	before := m.MatterDataVersion()
	got := []uint32{}
	stop := m.OnAttributeChange(func(change contract.Change) { ids := change.Attributes; got = append(got, ids...) })
	defer stop()
	if err := decodeAndInvoke(t, m, msgCtx(1, false), messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(1), "one")); err != nil {
		t.Fatal(err)
	}
	if err := decodeAndInvoke(t, m, msgCtx(2, false), messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(2), "two")); err != nil {
		t.Fatal(err)
	}
	if len(d.shown) != 2 || d.shown[0].FabricIndex != 1 || d.shown[1].FabricIndex != 2 {
		t.Fatalf("shown %+v", d.shown)
	}
	if m.MatterDataVersion() == before || !slices.Contains(got, messagesdef.AttrMessages) {
		t.Errorf("no change reported: version %d → %d, %v", before, m.MatterDataVersion(), got)
	}
	if !slices.Equal(ev.ids, []uint32{messagesdef.EventMessageQueued, messagesdef.EventMessageQueued}) {
		t.Errorf("events %v", ev.ids)
	}
	v, _ := m.MatterReadFiltered(msgCtx(2, true), messagesdef.AttrMessages)
	if l := v.(spec.List[messagesdef.MessageStruct]); len(l) != 1 || l[0].MessageText != "two" {
		t.Errorf("fabric 2 filtered read %+v", l)
	}
	v, _ = m.MatterReadFiltered(msgCtx(2, false), messagesdef.AttrMessages)
	if l := v.(spec.List[messagesdef.MessageStruct]); len(l) != 2 {
		t.Errorf("unfiltered read %+v", l)
	}
	spectest.RoundTripAttribute(t, messagesdef.Definition, messagesdef.AttrMessages, v.(spec.List[messagesdef.MessageStruct]))
	if v, _ := m.MatterRead(messagesdef.AttrMessages); len(v.(spec.List[messagesdef.MessageStruct])) != 2 {
		t.Errorf("local read %+v", v)
	}
	// Fabric 2 cannot cancel fabric 1's message.
	if err := decodeAndInvoke(t, m, msgCtx(2, false), messagesdef.CmdCancelMessagesRequest,
		messagesdef.CancelMessagesRequestRequest{MessageIds: [][]byte{messageID(1)}}); err != nil {
		t.Fatal(err)
	}
	if len(d.cancelled) != 0 {
		t.Errorf("cancelled %v", d.cancelled)
	}
	m.RemoveFabric(1)
	if v, _ := m.MatterRead(messagesdef.AttrMessages); len(v.(spec.List[messagesdef.MessageStruct])) != 1 {
		t.Errorf("after RemoveFabric(1): %+v", v)
	}
	m.RemoveFabric(9) // no message of that fabric: nothing changes
}

// TestMessagesLifecycle: a presented message joins ActiveMessageIds and
// completes with MessageComplete; a cancelled one leaves the queue; a
// repeated id replaces the queued message (MessagesManager.cpp).
func TestMessagesLifecycle(t *testing.T) {
	t.Parallel()
	m, d, ev := newMessages(t, messagesdef.FeatureReceivedConfirmation|messagesdef.FeatureConfirmationResponse|messagesdef.FeatureConfirmationReply)
	ctx := msgCtx(1, true)
	id := messageID(7)
	if err := decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, presentRequest(id, "hello")); err != nil {
		t.Fatal(err)
	}
	if !m.Presented(id) || m.Presented(id) || m.Presented(messageID(9)) {
		t.Fatal("Presented: want true once for a queued id")
	}
	if v, _ := m.MatterRead(messagesdef.AttrActiveMessageIds); len(v.([][]byte)) != 1 {
		t.Errorf("ActiveMessageIds %v", v)
	}
	// The same id again: the presented one completes, the new one queues.
	if err := decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, presentRequest(id, "hello again")); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.MatterRead(messagesdef.AttrMessages); len(v.(spec.List[messagesdef.MessageStruct])) != 1 || v.(spec.List[messagesdef.MessageStruct])[0].MessageText != "hello again" {
		t.Errorf("Messages %+v", v)
	}
	want := []uint32{messagesdef.EventMessageQueued, messagesdef.EventMessagePresented, messagesdef.EventMessageComplete, messagesdef.EventMessageQueued}
	if !slices.Equal(ev.ids, want) {
		t.Errorf("events %v, want %v", ev.ids, want)
	}
	m.Presented(id)
	resp, reply := uint32(1), "ok"
	pref := messagesdef.FutureMessagePreferenceAllowed
	if !m.Complete(id, mattercore.MessageOutcome{ResponseID: &resp, Reply: &reply, FutureMessagesPreference: &pref}) || m.Complete(id, mattercore.MessageOutcome{}) {
		t.Fatal("Complete: want true once")
	}
	if v, _ := m.MatterRead(messagesdef.AttrActiveMessageIds); len(v.([][]byte)) != 0 {
		t.Errorf("ActiveMessageIds %v after Complete", v)
	}
	// Cancel: a presented message completes, a queued one is dropped.
	_ = decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(1), "a"))
	_ = decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(2), "b"))
	m.Presented(messageID(1))
	n := len(ev.ids)
	if err := decodeAndInvoke(t, m, ctx, messagesdef.CmdCancelMessagesRequest,
		messagesdef.CancelMessagesRequestRequest{MessageIds: [][]byte{messageID(1), messageID(2), messageID(3)}}); err != nil {
		t.Fatal(err)
	}
	if len(d.cancelled) != 2 || !slices.Equal(ev.ids[n:], []uint32{messagesdef.EventMessageComplete}) {
		t.Errorf("cancelled %v, events %v", d.cancelled, ev.ids[n:])
	}
	if v, _ := m.MatterRead(messagesdef.AttrMessages); len(v.(spec.List[messagesdef.MessageStruct])) != 0 {
		t.Errorf("Messages %+v after the cancel", v)
	}
}

// TestMessagesRefusals: Responses without ConfirmationResponse is
// INVALID_COMMAND (messages-server.cpp:359-363), a full queue
// RESOURCE_EXHAUSTED, the presenter's failure FAILURE, a session without a
// fabric UNSUPPORTED_ACCESS, a write UNSUPPORTED_WRITE.
func TestMessagesRefusals(t *testing.T) {
	t.Parallel()
	m, d, _ := newMessages(t, 0)
	ctx := msgCtx(1, true)
	req := presentRequest(messageID(1), "x")
	req.Responses = &[]messagesdef.MessageResponseOptionStruct{{MessageResponseId: 1, Label: "OK"}}
	wantCoreStatus(t, decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, req), im.StatusInvalidCommand)
	for i := range 8 {
		if err := decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(byte(i+1)), "x")); err != nil {
			t.Fatal(err)
		}
	}
	wantCoreStatus(t, decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(9), "x")), im.StatusResourceExhausted)
	d.err = errors.New("display off")
	wantCoreStatus(t, decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(1), "x")), im.StatusFailure)
	wantCoreStatus(t, decodeAndInvoke(t, m, ctx, messagesdef.CmdCancelMessagesRequest,
		messagesdef.CancelMessagesRequestRequest{MessageIds: [][]byte{messageID(1)}}), im.StatusFailure)
	wantCoreStatus(t, decodeAndInvoke(t, m, msgCtx(0, false), messagesdef.CmdPresentMessagesRequest, presentRequest(messageID(1), "x")), im.StatusUnsupportedAccess)
	_, err := m.MatterInvoke(ctx, messagesdef.CmdPresentMessagesRequest, "garbage")
	wantCoreStatus(t, err, im.StatusInvalidCommand)
	_, err = m.MatterInvoke(ctx, messagesdef.CmdCancelMessagesRequest, 42)
	wantCoreStatus(t, err, im.StatusInvalidCommand)
	_, err = m.MatterInvoke(ctx, 0x05, nil)
	wantCoreStatus(t, err, im.StatusUnsupportedCommand)
	wantCoreStatus(t, m.MatterWrite(ctx, messagesdef.AttrActiveMessageIds, [][]byte{}), im.StatusUnsupportedWrite)
	if _, err := mattercore.NewMessages(mattercore.MessagesConfig{}); !errors.Is(err, mattercore.ErrNilPresenter) {
		t.Errorf("no presenter: %v", err)
	}
	if _, ok := m.MatterRead(0x0042); ok {
		t.Error("an undefined attribute read")
	}
}

func wantCoreStatus(t *testing.T, err error, code im.StatusCode) {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != code {
		t.Fatalf("error %v, want %s", err, code)
	}
}

// presentWithBareOption encodes a PresentMessagesRequest whose one
// response option carries a MessageResponseId and no Label.
type presentWithBareOption struct {
	messagesdef.PresentMessagesRequestRequest
}

func (p presentWithBareOption) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	spec.PutBytes(enc, tlv.ContextTag(0), p.MessageId)
	spec.PutUint[messagesdef.MessagePriorityEnum](enc, tlv.ContextTag(1), p.Priority)
	spec.PutUint[messagesdef.MessageControlBitmap](enc, tlv.ContextTag(2), p.MessageControl)
	spec.PutNullable(spec.PutUint[uint32])(enc, tlv.ContextTag(3), p.StartTime)
	spec.PutNullable(spec.PutUint[uint64])(enc, tlv.ContextTag(4), p.Duration)
	spec.PutString(enc, tlv.ContextTag(5), p.MessageText)
	enc.StartArray(tlv.ContextTag(6))
	enc.StartStruct(tlv.AnonymousTag())
	spec.PutUint[uint32](enc, tlv.ContextTag(0), 1)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
}

// TestMessagesResponseOptionRules pins where connectedhomeip's rules for
// the response options of PresentMessagesRequest are held: in the generated
// decoder, before the server sees the request (review II, W3-clucore-09).
// chip src/app/clusters/messages-server/messages-server.cpp at 6170af84:
// more than four options CONSTRAINT_ERROR (:365-367), an option without id
// or label INVALID_COMMAND (:375-377), a MessageResponseID below 1
// CONSTRAINT_ERROR (:379-381), a label above 32 CONSTRAINT_ERROR
// (:383-385). None of them reaches the presenter.
func TestMessagesResponseOptionRules(t *testing.T) {
	t.Parallel()
	m, d, _ := newMessages(t, messagesdef.FeatureReceivedConfirmation|messagesdef.FeatureConfirmationResponse)
	ctx := msgCtx(1, true)
	with := func(options ...messagesdef.MessageResponseOptionStruct) messagesdef.PresentMessagesRequestRequest {
		req := presentRequest(messageID(1), "x")
		req.Responses = &options
		return req
	}
	if err := decodeAndInvoke(t, m, ctx, messagesdef.CmdPresentMessagesRequest,
		with(messagesdef.MessageResponseOptionStruct{MessageResponseId: 1, Label: strings.Repeat("l", 32)})); err != nil {
		t.Fatalf("an option with id 1 and a 32-byte label: %v", err)
	}
	if len(d.shown) != 1 {
		t.Fatalf("presented %d messages, want 1", len(d.shown))
	}

	five := make([]messagesdef.MessageResponseOptionStruct, 5)
	for i := range five {
		five[i] = messagesdef.MessageResponseOptionStruct{MessageResponseId: uint32(i + 1), Label: "OK"} //nolint:gosec // 1..5
	}
	for _, tc := range []struct {
		name string
		req  spec.Encodable
		want im.StatusCode
	}{
		{"MessageResponseID 0", with(messagesdef.MessageResponseOptionStruct{MessageResponseId: 0, Label: "OK"}), im.StatusConstraintError},
		{"a 33-byte label", with(messagesdef.MessageResponseOptionStruct{MessageResponseId: 1, Label: strings.Repeat("l", 33)}), im.StatusConstraintError},
		{"five options", with(five...), im.StatusConstraintError},
		{"an option without a label", presentWithBareOption{presentRequest(messageID(2), "x")}, im.StatusInvalidCommand},
	} {
		_, err := messagesdef.Definition.Command(messagesdef.CmdPresentMessagesRequest, spec.Request).
			Decode(spectest.Decode(t, spectest.Encode(t, tc.req)))
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != tc.want {
			t.Errorf("%s: the decoder answers %v, want %s", tc.name, err, tc.want)
		}
	}
	if len(d.shown) != 1 {
		t.Errorf("presented %d messages, want the first only", len(d.shown))
	}
}
