// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/icd/IcdCheckInSenderTest.ts at
// 862ad75f: a decodable 0x50 to the resolved address, and false — never an
// error — for an unresolvable client, a failed send and a collaborator that
// fails (matter.js's throw is a returned error in Go).
package icd_test

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"testing"

	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/icd/checkin"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

var senderKey, _ = hex.DecodeString("5a6b7c8d9e0f00112233445566778899")

var resolved = func(context.Context, uint8, uint64) (*net.UDPAddr, error) {
	return &net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 5540}, nil
}

func TestIcdCheckInSender(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("sends a decodable 0x50 to the resolved address", func(t *testing.T) {
		t.Parallel()
		type sent struct {
			opcode  uint8
			payload []byte
		}
		var got []sent
		s := icd.NewSender(icd.SenderContext{
			ResolveAddress: resolved,
			SendUnsecured: func(_ context.Context, _ *net.UDPAddr, opcode uint8, payload []byte) (bool, error) {
				got = append(got, sent{opcode, payload})
				return true, nil
			},
		})
		ok, err := s.Send(ctx, icd.CheckInRequest{FabricIndex: 1, PeerNodeID: 0x1122334455667788, Key: senderKey, Counter: 42, ActiveModeThreshold: 4000})
		if err != nil || !ok {
			t.Fatalf("Send = %v, %v", ok, err)
		}
		if len(got) != 1 || got[0].opcode != mrp.SCOpcodeIcdCheckIn {
			t.Fatalf("sent %+v", got)
		}
		counter, threshold, err := checkin.DecodeICD(senderKey, got[0].payload)
		if err != nil || counter != 42 || threshold != 4000 {
			t.Fatalf("decoded %d %d %v", counter, threshold, err)
		}
	})
	t.Run("returns false and sends nothing when the address is unresolvable", func(t *testing.T) {
		t.Parallel()
		sends := 0
		s := icd.NewSender(icd.SenderContext{
			ResolveAddress: func(context.Context, uint8, uint64) (*net.UDPAddr, error) { return nil, nil },
			SendUnsecured: func(context.Context, *net.UDPAddr, uint8, []byte) (bool, error) {
				sends++
				return true, nil
			},
		})
		ok, err := s.Send(ctx, icd.CheckInRequest{FabricIndex: 1, PeerNodeID: 1, Key: senderKey, Counter: 1, ActiveModeThreshold: 4000})
		if ok || err != nil || sends != 0 {
			t.Fatalf("Send = %v, %v; %d sends", ok, err, sends)
		}
	})
	t.Run("returns false when the send fails", func(t *testing.T) {
		t.Parallel()
		s := icd.NewSender(icd.SenderContext{
			ResolveAddress: resolved,
			SendUnsecured:  func(context.Context, *net.UDPAddr, uint8, []byte) (bool, error) { return false, nil },
		})
		ok, err := s.Send(ctx, icd.CheckInRequest{FabricIndex: 1, PeerNodeID: 1, Key: senderKey, Counter: 1, ActiveModeThreshold: 4000})
		if ok || err != nil {
			t.Fatalf("Send = %v, %v", ok, err)
		}
	})
	t.Run("returns false (best-effort) when a collaborator throws", func(t *testing.T) {
		t.Parallel()
		s := icd.NewSender(icd.SenderContext{
			ResolveAddress: resolved,
			SendUnsecured: func(context.Context, *net.UDPAddr, uint8, []byte) (bool, error) {
				return false, errors.New("transport blew up")
			},
		})
		ok, err := s.Send(ctx, icd.CheckInRequest{FabricIndex: 1, PeerNodeID: 1, Key: senderKey, Counter: 1, ActiveModeThreshold: 4000})
		if ok || err != nil {
			t.Fatalf("Send = %v, %v", ok, err)
		}
		r := icd.NewSender(icd.SenderContext{
			ResolveAddress: func(context.Context, uint8, uint64) (*net.UDPAddr, error) { return nil, errors.New("resolver blew up") },
		})
		if ok, err := r.Send(ctx, icd.CheckInRequest{Key: senderKey}); ok || err != nil {
			t.Fatalf("Send with a failing resolver = %v, %v", ok, err)
		}
	})
}

// TestSenderEncodeError holds IcdCheckInSender.ts:59-61: an encoding failure
// is a defect and is returned, not swallowed.
func TestSenderEncodeError(t *testing.T) {
	t.Parallel()
	s := icd.NewSender(icd.SenderContext{ResolveAddress: resolved})
	if _, err := s.Send(context.Background(), icd.CheckInRequest{Key: []byte{1}}); err == nil {
		t.Fatal("no error for a one-byte key")
	}
}
