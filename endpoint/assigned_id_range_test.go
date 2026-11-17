// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"strings"
	"testing"
)

// TestAssignOrReuseIDRefusesANumberOutsideTheRecordRange (R2-P6-store-0448):
// the assembler holds a Store's answer to [Record]'s range [2, 65534], on
// the fresh allocation and on a stored record alike — 0 and 1 would be a
// second root or Aggregator in the topology, 65535 is reserved.
func TestAssignOrReuseIDRefusesANumberOutsideTheRecordRange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, id := range []uint16{0, 1, 65535} {
		// Fresh allocation answering id.
		fs := newInternalFakeStore()
		fs.nextID = id
		a := &Assembler{store: fs}
		if got, err := a.assignOrReuseID(ctx, "s", StringKey("fresh"), 0x0100); err == nil || !strings.Contains(err.Error(), "fresh") {
			t.Errorf("fresh allocation of %d: id %d, err %v — want an error naming the key", id, got, err)
		}
		// A stored record holding id.
		fs = newInternalFakeStore()
		fs.rows[StringKey("held")] = Record{Key: StringKey("held"), Scope: "s", DeviceType: 0x0100, EndpointID: id}
		a = &Assembler{store: fs}
		if got, err := a.assignOrReuseID(ctx, "s", StringKey("held"), 0x0100); err == nil || !strings.Contains(err.Error(), "held") {
			t.Errorf("stored %d: id %d, err %v — want an error naming the key", id, got, err)
		}
	}
	// The bounds themselves are numbers a store may answer.
	for _, id := range []uint16{2, 65534} {
		fs := newInternalFakeStore()
		fs.nextID = id
		a := &Assembler{store: fs}
		if got, err := a.assignOrReuseID(ctx, "s", StringKey("ok"), 0x0100); err != nil || got != id {
			t.Errorf("allocation of %d: id %d, err %v", id, got, err)
		}
	}
}
