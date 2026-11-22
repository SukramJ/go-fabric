// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/secure/spake2"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// pbkdfRequestField is one member of a hand-built PBKDFParamRequest: put
// encodes it, nil leaves it out.
type pbkdfRequestField struct {
	tag uint8
	put func(enc *tlv.Encoder, tag tlv.Tag)
}

// pbkdfRequestWith encodes a PBKDFParamRequest from the well-formed
// members (InitiatorRandom, InitiatorSessionID 42, PasscodeID 0,
// HasPBKDFParameters false), with each override replacing the member of
// its tag, or removing it when its put is nil.
func pbkdfRequestWith(t *testing.T, overrides ...pbkdfRequestField) []byte {
	t.Helper()
	fields := []pbkdfRequestField{
		{1, func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.PutOctets(tag, bytes.Repeat([]byte{0x22}, spake2.PBKDFRandomSize))
		}},
		{2, func(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, 42) }},
		{3, func(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, 0) }},
		{4, func(enc *tlv.Encoder, tag tlv.Tag) { enc.PutBool(tag, false) }},
	}
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	for _, f := range fields {
		for _, o := range overrides {
			if o.tag == f.tag {
				f.put = o.put
			}
		}
		if f.put != nil {
			f.put(enc, tlv.ContextTag(f.tag))
		}
	}
	if err := enc.EndContainer(); err != nil {
		t.Fatal(err)
	}
	out, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestPaseAdapter_PBKDFParamRequestWithoutMandatoryFieldRejected: a
// PBKDFParamRequest that lacks one of its mandatory fields, or carries
// one of another type or width, is refused before a response is built,
// and the refusal reaches the PASE failure count. Mirrors matter.js at
// db021da0: TlvPbkdfParamRequest makes tags 1-4 TlvFields
// (packages/protocol/src/session/pase/PaseMessages.ts:70-73: ByteString of
// 32, UInt16, UInt16, Boolean); Schema.decode validates
// (packages/types/src/schema/Schema.ts:18-24), a missing field throws
// (packages/types/src/tlv/TlvObject.ts:252-261), an element of another
// type throws (TlvNumber.ts:124-131, TlvBoolean.ts:22-24), a UInt16 above
// 0xFFFF fails its bound (TlvNumber.ts:155); SecureChannelMessenger.decode
// rethrows it as UnexpectedDataError
// (packages/protocol/src/securechannel/SecureChannelMessenger.ts:123-137),
// read inside handlePairingRequest (PaseServer.ts:122, the read :143;
// PaseMessenger.ts:37), whose throw is counted and answered
// (PaseServer.ts:90-104). Before this check the request without
// PasscodeID and HasPBKDFParameters was answered with a
// PBKDFParamResponse: PasscodeID read as the default 0 and
// HasPBKDFParameters as false.
func TestPaseAdapter_PBKDFParamRequestWithoutMandatoryFieldRejected(t *testing.T) {
	t.Parallel()
	absent := func(tag uint8) pbkdfRequestField { return pbkdfRequestField{tag: tag} }
	putUint := func(tag uint8, v uint64) pbkdfRequestField {
		return pbkdfRequestField{tag, func(enc *tlv.Encoder, tg tlv.Tag) { enc.PutUint(tg, v) }}
	}
	putBool := func(tag uint8) pbkdfRequestField {
		return pbkdfRequestField{tag, func(enc *tlv.Encoder, tg tlv.Tag) { enc.PutBool(tg, false) }}
	}
	putInt := func(tag uint8) pbkdfRequestField {
		return pbkdfRequestField{tag, func(enc *tlv.Encoder, tg tlv.Tag) { enc.PutInt(tg, 1) }}
	}
	adapter := func() *PaseAdapter {
		a := NewPaseAdapterWithFactory(newVerifierFactory(t, nil))
		a.SetPBKDFParams(failureCountTestIterations, failureCountTestSalt(), 1)
		a.randomSource = func() [spake2.PBKDFRandomSize]byte { return [spake2.PBKDFRandomSize]byte{0x11} }
		return a
	}
	// The control: the well-formed request is answered.
	if op, body, err := adapter().ProcessPBKDFParamRequest(pbkdfRequestWith(t)); err != nil || op != mrp.SCOpcodePBKDFParamResponse || len(body) == 0 {
		t.Fatalf("well-formed request: opcode 0x%02X, %d bytes, err %v", op, len(body), err)
	}
	for _, c := range []struct {
		name      string
		overrides []pbkdfRequestField
	}{
		{"without InitiatorSessionID", []pbkdfRequestField{absent(2)}},
		{"without PasscodeID", []pbkdfRequestField{absent(3)}},
		{"without HasPBKDFParameters", []pbkdfRequestField{absent(4)}},
		{"without PasscodeID and HasPBKDFParameters", []pbkdfRequestField{absent(3), absent(4)}},
		{"an InitiatorSessionID above 0xFFFF", []pbkdfRequestField{putUint(2, 0x10000)}},
		{"a signed InitiatorSessionID", []pbkdfRequestField{putInt(2)}},
		{"a PasscodeID of boolean type", []pbkdfRequestField{putBool(3)}},
		{"a HasPBKDFParameters of integer type", []pbkdfRequestField{putUint(4, 0)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			op, body, err := adapter().ProcessPBKDFParamRequest(pbkdfRequestWith(t, c.overrides...))
			if !errors.Is(err, spake2.ErrWireMalformed) {
				t.Fatalf("err = %v, want ErrWireMalformed", err)
			}
			if op != 0 || body != nil {
				t.Fatalf("refused request returned opcode 0x%02X, %d bytes; want (0, nil) so handlePase answers it", op, len(body))
			}
		})
	}
	t.Run("counted through the router", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		b.AttachPaseHandler(adapter())
		req := pbkdfRequestWith(t, absent(3), absent(4))
		_ = b.dispatchSecureChannel(loopbackSrc(), scHdr(), scProto(mrp.SCOpcodePBKDFParamRequest, 7, false, 0), req)
		if got := b.failedAttempts.Load(); got != 1 {
			t.Fatalf("failedAttempts = %d, want 1", got)
		}
	})
}
