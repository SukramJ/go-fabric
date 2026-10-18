// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mrp

// A port of matter.js packages/protocol/test/protocol/MessageReceptionStateTest.ts
// at 862ad75f: the window vectors of the three reception states.
//
// matter.js intercepts calculateDiff and calculateMessageCounterBitmap; the
// Go surface is Window.diff (read before Accept, as updateMessageCounter
// computes it) and the bitmap after Accept. The states map to
// NewWindowNoRollover (EncryptedWithoutRollover), NewWindowEncryptedRollover
// (EncryptedWithRollover) and NewWindow (UnencryptedWithRollover).

import "testing"

const (
	mrsMax    = uint32(0xFFFFFFFF)
	mrs2pow31 = int64(1) << 31
)

// mrsUpdate is assertMessageWindowUpdate (MessageReceptionStateTest.ts:17-60).
func mrsUpdate(t *testing.T, w *Window, c, wantBitmap uint32, wantDup bool, wantDiff int64) {
	t.Helper()
	mrsDiff(t, w, c, wantDup, wantDiff)
	if w.bitmap != wantBitmap {
		t.Errorf("counter %d: bitmap = %#b, want %#b", c, w.bitmap, wantBitmap)
	}
}

// mrsDiff is assertMessageWindowDifference (MessageReceptionStateTest.ts:62-91).
func mrsDiff(t *testing.T, w *Window, c uint32, wantDup bool, wantDiff int64) {
	t.Helper()
	if d := w.diff(c); d != wantDiff {
		t.Errorf("counter %d: diff = %d, want %d", c, d, wantDiff)
	}
	if dup := !w.Accept(c); dup != wantDup {
		t.Errorf("counter %d: duplicate = %v, want %v", c, dup, wantDup)
	}
}

func mrsAccept(t *testing.T, w *Window, cs ...uint32) {
	t.Helper()
	for _, c := range cs {
		if !w.Accept(c) {
			t.Fatalf("counter %d rejected", c)
		}
	}
}

func mrsDup(t *testing.T, w *Window, cs ...uint32) {
	t.Helper()
	for _, c := range cs {
		if w.Accept(c) {
			t.Errorf("counter %d accepted, want duplicate", c)
		}
	}
}

// mrsShared are the cases the three states share verbatim.
func mrsShared(t *testing.T, newWindow func() *Window, firstBitmap uint32) {
	t.Helper()
	t.Run("state gets initialized by first counter value", func(t *testing.T) {
		t.Parallel()
		w := newWindow()
		mrsAccept(t, w, 0x1234)
		mrsUpdate(t, w, 0x1235, firstBitmap, false, 1)
	})
	t.Run("correct difference gets calculated and state gets updated after being initialized", func(t *testing.T) {
		t.Parallel()
		w := newWindow()
		mrsAccept(t, w, 0x1234)
		mrsDiff(t, w, 0x1235, false, 1)
		if w.max != 0x1235 {
			t.Errorf("max = %#x", w.max)
		}
	})
	t.Run("duplicate counter value (same value) within window is detected", func(t *testing.T) {
		t.Parallel()
		w := newWindow()
		mrsAccept(t, w, 0x1234)
		mrsDup(t, w, 0x1234)
	})
	t.Run("value tests within counter window", func(t *testing.T) {
		t.Parallel()
		w := newWindow() // MessageReceptionStateTest.ts:245-264
		mrsAccept(t, w, 1, 96)
		mrsUpdate(t, w, 100, 0b1000, false, 4)
		mrsUpdate(t, w, 97, 0b1100, false, -3)
		mrsUpdate(t, w, 101, 0b11001, false, 1)
		mrsUpdate(t, w, 103, 0b1100110, false, 2)
		mrsDup(t, w, 96)
		mrsUpdate(t, w, 95, 0b11100110, false, -8)
		mrsUpdate(t, w, 73, 0b100000000000000000000011100110, false, -30)
		mrsUpdate(t, w, 72, 0b1100000000000000000000011100110, false, -31)
		mrsUpdate(t, w, 71, 0b11100000000000000000000011100110, false, -32)
		mrsDup(t, w, 96, 101, 103)
		mrsAccept(t, w, 102)
	})
	t.Run("forward jump of exactly the window size clears stale bitmap bits", func(t *testing.T) {
		t.Parallel()
		w := newWindow() // MessageReceptionStateTest.ts:304-315
		mrsAccept(t, w, 1, 100)
		mrsUpdate(t, w, 94, 0b100000, false, -6)
		mrsUpdate(t, w, 132, 0b10000000000000000000000000000000, false, 32)
		mrsUpdate(t, w, 126, 0b10000000000000000000000000100000, false, -6)
	})
}

// mrsEncryptedShared are the cases both encrypted states share.
func mrsEncryptedShared(t *testing.T, newWindow func() *Window) {
	t.Helper()
	t.Run("duplicate counter value (value -1) within window is detected", func(t *testing.T) {
		t.Parallel()
		w := newWindow()
		mrsAccept(t, w, 0x1234, 0x1235)
		mrsDiff(t, w, 0x1234, true, -1)
	})
	t.Run("value tests on counter window edges", func(t *testing.T) {
		t.Parallel()
		w := newWindow() // MessageReceptionStateTest.ts:266-281
		mrsAccept(t, w, 1, 103)
		mrsUpdate(t, w, 100, 0b100, false, -3)
		mrsUpdate(t, w, 101, 0b110, false, -2)
		mrsUpdate(t, w, 96, 0b1000110, false, -7)
		mrsDup(t, w, 70)
		mrsUpdate(t, w, 71, 0b10000000000000000000000001000110, false, -32)
		mrsUpdate(t, w, 105, 0b100011010, false, 2)
		mrsDup(t, w, 70, 71, 72)
		mrsUpdate(t, w, 73, 0b10000000000000000000000100011010, false, -32)
	})
}

func TestMessageReceptionStateEncryptedWithoutRollover(t *testing.T) {
	t.Parallel()
	mrsShared(t, NewWindowNoRollover, 0b11111111111111111111111111111111)
	mrsEncryptedShared(t, NewWindowNoRollover)

	t.Run("value tests on range edges", func(t *testing.T) {
		t.Parallel()
		w := NewWindowNoRollover() // MessageReceptionStateTest.ts:283-291
		mrsAccept(t, w, 103)
		mrsDup(t, w, 0)
		mrsAccept(t, w, mrsMax)
		mrsDup(t, w, mrsMax-33)
		mrsAccept(t, w, mrsMax-32)
	})
	t.Run("accepts a peer counter of 0xFFFFFFFF (maximum valid value)", func(t *testing.T) {
		t.Parallel()
		mrsAccept(t, NewWindowNoRollover(), 0xffffffff)
	})
}

func TestMessageReceptionStateEncryptedWithRollover(t *testing.T) {
	t.Parallel()
	mrsShared(t, NewWindowEncryptedRollover, 0b11111111111111111111111111111111)
	mrsEncryptedShared(t, NewWindowEncryptedRollover)

	t.Run("value tests on edges and duplicate values with low max-number and rollovers", func(t *testing.T) {
		t.Parallel()
		w := NewWindowEncryptedRollover() // MessageReceptionStateTest.ts:476-498
		mrsAccept(t, w, 1, 103)
		mrsUpdate(t, w, 71, 0b10000000000000000000000000000000, false, -32)
		mrsDiff(t, w, 70, true, -33)
		mrsDiff(t, w, 0, true, -103)
		mrsDiff(t, w, mrsMax, true, -104)
		mrsDiff(t, w, uint32(103+mrs2pow31), true, -mrs2pow31)
	})
	t.Run("value tests on edges and duplicate values with high max-number and rollovers", func(t *testing.T) {
		t.Parallel()
		w := NewWindowEncryptedRollover() // MessageReceptionStateTest.ts:500-515
		highBase := int64(1) << 30
		mrsAccept(t, w, uint32(highBase))
		highBase2 := mrs2pow31 + 100_000
		mrsDiff(t, w, uint32(highBase2), false, highBase2-highBase)
		mrsDiff(t, w, uint32(highBase2-mrs2pow31), true, -mrs2pow31)
	})
	t.Run("value tests on edges and new values with low max-number and rollovers", func(t *testing.T) {
		t.Parallel()
		w := NewWindowEncryptedRollover() // MessageReceptionStateTest.ts:517-536
		mrsAccept(t, w, 1, 102)
		mrsUpdate(t, w, 103, 0b1, false, 1)
		mrsUpdate(t, w, uint32(103+mrs2pow31-1), 0b0, false, mrs2pow31-1)
	})
	t.Run("value tests on edges and new values with high max-number and rollovers", func(t *testing.T) {
		t.Parallel()
		w := NewWindowEncryptedRollover() // MessageReceptionStateTest.ts:538-553
		mrsAccept(t, w, uint32(103+mrs2pow31-1))
		highBase := mrs2pow31 + 100_000
		mrsDiff(t, w, uint32(highBase), false, 100_000-102)
		low := highBase - mrs2pow31 - 1
		mrsDiff(t, w, uint32(low), false, int64(mrsMax)-highBase+low+1)
	})
}

func TestMessageReceptionStateUnencryptedWithRollover(t *testing.T) {
	t.Parallel()
	mrsShared(t, NewWindow, 0b1)

	t.Run("duplicate counter value (value -1) within window is detected", func(t *testing.T) {
		t.Parallel()
		w := NewWindow()
		mrsAccept(t, w, 1, 0x1234, 0x1235)
		mrsDiff(t, w, 0x1234, true, -1)
	})
	t.Run("value tests on counter window edges", func(t *testing.T) {
		t.Parallel()
		w := NewWindow() // MessageReceptionStateTest.ts:707-717
		mrsAccept(t, w, 1, 103)
		mrsUpdate(t, w, 100, 0b100, false, -3)
		mrsUpdate(t, w, 101, 0b110, false, -2)
		mrsUpdate(t, w, 96, 0b1000110, false, -7)
		mrsUpdate(t, w, 71, 0b10000000000000000000000001000110, false, -32)
	})
	t.Run("value tests on edges and duplicate values with low max-number and rollovers", func(t *testing.T) {
		t.Parallel()
		w := NewWindow() // MessageReceptionStateTest.ts:719-729
		mrsAccept(t, w, 1, 103)
		mrsUpdate(t, w, 70, 0b0, false, int64(mrsMax)-32)
		mrsUpdate(t, w, 71, 0b1, false, 1)
		mrsUpdate(t, w, 105, 0b0, false, 34)
		mrsUpdate(t, w, 73, 0b10000000000000000000000000000000, false, -32)
	})
	t.Run("value tests on edges and duplicate values with high max-number and rollovers", func(t *testing.T) {
		t.Parallel()
		w := NewWindow() // MessageReceptionStateTest.ts:731-761
		mrsAccept(t, w, 1<<30, 10)
		mrsUpdate(t, w, mrsMax-10, 0b100000000000000000000, false, -21)
		mrsUpdate(t, w, mrsMax-21, 0b10000000000100000000000000000000, false, -32)
		mrsUpdate(t, w, mrsMax-22, 0b0, false, int64(mrsMax)-32)
	})
	t.Run("treats the exact counter rollover (MAX -> 0) as new and detects replays", func(t *testing.T) {
		t.Parallel()
		w := NewWindow() // MessageReceptionStateTest.ts:763-779
		mrsAccept(t, w, mrsMax-1, mrsMax)
		mrsDiff(t, w, 0, false, 1)
		mrsDup(t, w, 0, mrsMax)
		mrsDiff(t, w, 1, false, 1)
	})
	t.Run("accepts an in-window sub-max counter delivered right after the first counter", func(t *testing.T) {
		t.Parallel()
		w := NewWindow() // MessageReceptionStateTest.ts:791-800 (matter.js issue #4095)
		const af = uint32(0x0e8288af)
		mrsAccept(t, w, af, af-1)
		mrsDup(t, w, af-1)
	})
}
