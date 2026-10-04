// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package operational_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestOpenFromSigmaAsInitiatorWithID_TalksToResponderSession: the same CASE
// keys installed once as initiator and once as responder must interoperate
// in both directions — the initiator encrypts with I2R, the responder
// decrypts with it (matter.js NodeSession `isInitiator` key split).
func TestOpenFromSigmaAsInitiatorWithID_TalksToResponderSession(t *testing.T) {
	t.Parallel()
	var keys sigma.SessionKeys
	for i := range keys.I2RKey {
		keys.I2RKey[i] = byte(i + 1)
		keys.R2IKey[i] = byte(0xA0 + i)
	}
	const deviceNode, controllerNode = uint64(0xD0D0), uint64(0xC0C0)

	device, _ := newTestManager()
	controller, _ := newTestManager()
	initEntry, err := device.OpenFromSigmaAsInitiatorWithID(0x100, 1, deviceNode, controllerNode, 0x200, nil, keys)
	if err != nil {
		t.Fatal(err)
	}
	respEntry, err := controller.OpenFromSigmaWithID(0x200, 1, controllerNode, deviceNode, 0x100, nil, keys)
	if err != nil {
		t.Fatal(err)
	}
	if got := initEntry.Session.PeerSessionID(); got != 0x200 {
		t.Fatalf("initiator peer session id = %#x", got)
	}

	roundTrip := func(from, to *channel.Session, payload []byte) {
		t.Helper()
		var hdr message.Header
		hdr.SessionID = 0x1
		out, err := from.Encrypt(&hdr, 0, payload)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := to.Decrypt(&hdr, 0, out.Ciphertext)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("got %q, want %q", got, payload)
		}
	}
	roundTrip(initEntry.Session, respEntry.Session, []byte("device → controller"))
	roundTrip(respEntry.Session, initEntry.Session, []byte("controller → device"))
}
