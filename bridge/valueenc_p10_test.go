// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// malformedValueP10 writes one complete, balanced, anonymously tagged
// element whose inner tags break the container rules of tlv.Validate.
type malformedValueP10 struct{ kind string }

func (v malformedValueP10) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	switch v.kind {
	case "anonymous member of a Structure":
		enc.StartStruct(tag)
		enc.PutUint(tlv.AnonymousTag(), 1)
		_ = enc.EndContainer()
	case "context-tagged member of an Array":
		// Array { ctx(1) uint8 5 }
		_ = enc.PutRawElement(tag, []byte{0x16, 0x24, 0x01, 0x05, 0x18})
	}
}

// malformedServerP10 is failingServerW625 with attribute 0 a
// malformedValueP10.
type malformedServerP10 struct {
	failingServerW625
	kind string
}

func (s *malformedServerP10) MatterRead(attrID uint32) (any, bool) {
	if attrID == 0 {
		return malformedValueP10{kind: s.kind}, true
	}
	return s.failingServerW625.MatterRead(attrID)
}

// TestMalformedHostElementCostsItsPathOnReadP10 (P10-wire-6402): a Read
// of a host attribute whose EncodeTLV writes one balanced element with an
// anonymous member in a Structure, or a tagged member in an Array, is
// answered FAILURE for that path, the other attribute delivered, an ERROR
// log naming the path. Red at d1ab4e970: attribute 0 went out as data,
// and the ReportData broke tlv.Validate's tag rules.
func TestMalformedHostElementCostsItsPathOnReadP10(t *testing.T) {
	t.Parallel()
	for i, kind := range []string{"anonymous member of a Structure", "context-tagged member of an Array"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			b, logs := newLoggedBridgeW625(t, &malformedServerP10{kind: kind})
			peerConn, peerAddr := newSubscribeTestPeer(t)
			sent := answerAndDecodeW625(t, b, peerConn)
			hdr := &message.Header{SessionID: paseTestSessionID, MessageCounter: uint32(900 + i)}
			proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeReadRequest, ExchangeID: uint16(0x6402 + i), Initiator: true}
			req := im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{
				{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 0, HasAttribute: true},
				{Endpoint: 0, HasEndpoint: true, Cluster: 0x0006, HasCluster: true, Attribute: 1, HasAttribute: true},
			}}
			if err := b.dispatchReadRequest(context.Background(), peerAddr, hdr, proto, b.Dispatcher(), req); err != nil {
				t.Fatalf("dispatchReadRequest: %v", err)
			}
			rd := nextSentW625(t, sent, im.OpcodeReportData).report
			checkFailedAttributeW625(t, "read", rd.Reports)
			waitLogW625(t, logs, encodeFailedLogW625)
		})
	}
}
