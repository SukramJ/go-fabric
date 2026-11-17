// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"errors"
	"testing"
)

// TestUnparsablePayloadFailsItsCandidate (F-db021da0-35): a group message
// that authenticates under a mapped key but whose payload's protocol
// header does not parse fails that candidate, as matter.js
// GroupSession.ts:435-442 at db021da0. With no other candidate it ends as
// a [*DecodeError] naming the group and its fabric, and the reception
// window never took its counter: the same sender's parsable message under
// that counter is fresh, not a duplicate.
func TestUnparsablePayloadFailsItsCandidate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, _ := newTestManager(t)
	op, sid := opKeyOf(t, 0xA1)

	// Flags 0x00, opcode, exchange id: three bytes, no protocol id.
	broken := sealGroup(t, op, sid, 0x0101, 1, 7, false, []byte{0x00, 0x08, 0x01})
	_, err := m.Decode(ctx, broken)
	var rejected *DecodeError
	if !errors.As(err, &rejected) {
		t.Fatalf("an authenticated message whose payload header does not parse: err = %v, want *DecodeError", err)
	}
	if !rejected.HasGroupID || rejected.GroupID != 0x0101 || rejected.GroupFabricIndex != 1 {
		t.Fatalf("rejection %+v, want group 0x0101 on fabric 1", rejected)
	}

	m.receptionMu.Lock()
	windows := 0
	for _, perKey := range m.reception {
		windows += len(perKey)
	}
	m.receptionMu.Unlock()
	if windows != 0 {
		t.Fatalf("%d reception windows after a message no candidate parsed, want none", windows)
	}

	msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 1, 7, false, imPayload([]byte("toggle"))))
	if err != nil || msg.Duplicate {
		t.Fatalf("the parsable message under the same counter: err %v, duplicate %v", err, msg != nil && msg.Duplicate)
	}
}
