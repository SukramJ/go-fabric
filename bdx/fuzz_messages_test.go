// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/bdx"
)

// FuzzDecode feeds every BDX message type arbitrary payloads: the decoder
// a responder runs on a peer's first message (the bridge hands it any
// *Init a peer sends, bdx.Responder.HandleExchange) must not panic, and an
// Init it accepts must re-encode to bytes it decodes the same way.
func FuzzDecode(f *testing.F) {
	initPayload, _ := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/f")}.Encode()
	f.Add(uint8(bdx.MessageReceiveInit), initPayload)
	f.Add(uint8(bdx.MessageSendAccept), []byte{0x10, 0x00, 0x04})
	f.Add(uint8(bdx.MessageReceiveAccept), []byte{0x20, 0x01, 0x00, 0x04, 0x10, 0x00, 0x00, 0x00})
	f.Add(uint8(bdx.MessageBlock), []byte{0, 0, 0, 0, 1, 2, 3})
	f.Add(uint8(bdx.MessageBlockQueryWithSkip), make([]byte, 12))
	f.Fuzz(func(t *testing.T, typ uint8, payload []byte) {
		v, err := bdx.Decode(bdx.MessageType(typ), payload)
		if err != nil {
			return
		}
		init, ok := v.(bdx.Init)
		if !ok {
			return
		}
		again, err := init.Encode()
		if err != nil {
			t.Fatalf("decoded Init %+v does not encode: %v", init, err)
		}
		back, err := bdx.DecodeInit(again)
		if err != nil {
			t.Fatalf("re-encoded Init does not decode: %v", err)
		}
		if !bytes.Equal(back.FileDesignator, init.FileDesignator) || back.MaxBlockSize != init.MaxBlockSize || back.TransferControl != init.TransferControl {
			t.Fatalf("round trip changed %+v into %+v", init, back)
		}
	})
}
