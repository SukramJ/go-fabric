// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestParityMatterJS_IdentifyTimeBounds: an IdentifyTime write above
// 65535 or below 0 is refused with CONSTRAINT_ERROR and leaves the
// attribute as it was, where it used to be masked into another valid
// value; a value of another type is INVALID_ACTION — matter.js
// packages/types/src/tlv/TlvNumber.ts:145-156 and
// packages/types/src/common/ValidationError.ts:35-38 at 862ad75f
// (W3-clucore-04).
func TestParityMatterJS_IdentifyTimeBounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id := core.NewIdentify()
	t.Cleanup(id.Close)
	for _, c := range []struct {
		value any
		want  im.StatusCode
	}{
		{uint64(70000), im.StatusConstraintError},
		{uint32(65536), im.StatusConstraintError},
		{int64(-1), im.StatusConstraintError},
		{int(1 << 20), im.StatusConstraintError},
		{"bad", im.StatusInvalidAction},
	} {
		err := id.MatterWrite(ctx, 0x0000, c.value)
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != c.want {
			t.Errorf("IdentifyTime %T(%v): %v, want status 0x%02X", c.value, c.value, err, c.want)
		}
		if v, _ := id.MatterRead(0x0000); v != uint16(0) {
			t.Fatalf("IdentifyTime %v after the refused %v, want 0", v, c.value)
		}
	}
	if err := id.MatterWrite(ctx, 0x0000, uint64(65535)); err != nil {
		t.Fatalf("IdentifyTime 65535: %v", err)
	}
}

// TestParityMatterJS_IdentifyCountdownZeroReported: the countdown reaching
// zero is reported once and advances the DataVersion, the per-second ticks
// are not — matter.js IdentifyServer.ts:41-47 at 862ad75f (the quiet
// event emits at once for a change to 0; W3-clucore-05).
func TestParityMatterJS_IdentifyCountdownZeroReported(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	id := core.NewIdentify()
	id.SetClock(clk)
	t.Cleanup(id.Close)
	var (
		mu      sync.Mutex
		changes [][]uint32
	)
	id.OnAttributeChange(func(c contract.Change) {
		mu.Lock()
		changes = append(changes, c.Attributes)
		mu.Unlock()
	})
	if _, err := id.MatterInvoke(context.Background(), 0x00, uint16(3)); err != nil {
		t.Fatal(err)
	}
	version := id.MatterDataVersion()
	for want := uint16(2); ; want-- {
		for range 10_000 {
			if clk.Pending() > 0 {
				break
			}
			clk.Yield()
		}
		clk.Advance(time.Second)
		for range 10_000 {
			if v, _ := id.MatterRead(0x0000); v == want {
				break
			}
			clk.Yield()
		}
		if want == 0 {
			break
		}
	}
	for range 10_000 {
		mu.Lock()
		n := len(changes)
		mu.Unlock()
		if n > 0 {
			break
		}
		clk.Yield()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(changes) != 1 || len(changes[0]) != 1 || changes[0][0] != 0x0000 {
		t.Fatalf("changes %v, want IdentifyTime once, at zero", changes)
	}
	if id.MatterDataVersion() == version {
		t.Error("the DataVersion did not advance when the countdown reached zero")
	}
}
