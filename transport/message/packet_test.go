// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package message

import (
	"bytes"
	"errors"
	"testing"
)

// TestMarshalPacketGroupConstraint pins matter.js MessageCodec.ts:481-488:
// a group-session header encodes only with a destination group id and a
// source node id, and never with a destination node id.
func TestMarshalPacketGroupConstraint(t *testing.T) {
	t.Parallel()
	good := Header{
		SessionID: 0x1234, MessageCounter: 7, SessionType: SessionGroup, Privacy: true,
		HasSourceNodeID: true, SourceNodeID: 0x1122334455667788,
		DestSize: DestGroup, DestGroupID: 0x0101,
	}
	b, err := good.MarshalPacket()
	if err != nil {
		t.Fatalf("MarshalPacket: %v", err)
	}
	if !bytes.Equal(b, good.Marshal()) {
		t.Errorf("MarshalPacket % X != Marshal % X", b, good.Marshal())
	}
	// Message Flags S|DSIZ=group, Security Flags P|Group (matter.js
	// encodePacketHeader :490-495).
	if b[0] != 0x06 || b[3] != 0x81 || len(b) != 18 {
		t.Errorf("group header % X: flags %#x, security flags %#x, %d bytes", b, b[0], b[3], len(b))
	}

	for name, h := range map[string]Header{
		"no group id":       {SessionType: SessionGroup, HasSourceNodeID: true, SourceNodeID: 1},
		"no source node id": {SessionType: SessionGroup, DestSize: DestGroup, DestGroupID: 1},
		"destination node":  {SessionType: SessionGroup, HasSourceNodeID: true, DestSize: DestNodeID, DestNodeID: 9},
	} {
		if _, err := h.MarshalPacket(); !errors.Is(err, ErrGroupHeader) {
			t.Errorf("%s: %v, want ErrGroupHeader", name, err)
		}
	}

	// A unicast header encodes as Marshal does, with or without ids.
	uni := Header{SessionID: 3, MessageCounter: 1, DestSize: DestNodeID, DestNodeID: 5}
	if b, err := uni.MarshalPacket(); err != nil || !bytes.Equal(b, uni.Marshal()) {
		t.Errorf("unicast: % X, %v", b, err)
	}
}

// TestMarshalPacketRefusesExtensions pins MessageCodec.ts:441-443.
func TestMarshalPacketRefusesExtensions(t *testing.T) {
	t.Parallel()
	for _, h := range []Header{
		{HasExtension: true},
		{MessageExtension: []byte{1}},
	} {
		if _, err := h.MarshalPacket(); !errors.Is(err, ErrEncodeExtensions) {
			t.Errorf("%+v: %v, want ErrEncodeExtensions", h, err)
		}
	}
}
