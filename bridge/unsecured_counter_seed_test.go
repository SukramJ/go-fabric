// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import "testing"

// TestUnsecuredCounterAndExchangeIDStartRandom — the unsecured message
// counter and the device-initiated exchange ids start at a random value,
// as both references start them (matter.js
// packages/protocol/src/protocol/MessageCounter.ts:60
// `(crypto.randomUint32 >>> 4) + 1`, ExchangeManager.ts:794-808;
// connectedhomeip src/messaging/ExchangeMgr.cpp:67
// `mNextExchangeId = chip::Crypto::GetRandU16()`), not at 1 after every
// process start. Eight bridges all starting at 1 by chance is a 2^-112
// event for the exchange ids and less for the counter.
func TestUnsecuredCounterAndExchangeIDStartRandom(t *testing.T) {
	t.Parallel()
	const bridges = 8
	allOneCounter, allOneExchange := true, true
	for range bridges {
		b := newUnstartedBridge(t)
		c := b.nextUnsecuredCounter()
		if c >= 1<<28+2 {
			t.Fatalf("first unsecured counter %#x is above the 28-bit start range plus one", c)
		}
		if c != 1 {
			allOneCounter = false
		}
		if b.nextOutboundExchangeID() != 1 {
			allOneExchange = false
		}
	}
	if allOneCounter {
		t.Fatalf("the first unsecured counter of %d fresh bridges was 1 every time: the counter is not seeded", bridges)
	}
	if allOneExchange {
		t.Fatalf("the first exchange id of %d fresh bridges was 1 every time: the exchange ids are not seeded", bridges)
	}
}

// TestOutboundExchangeIDUsesTheFull16Bits — a device-initiated exchange
// id runs through the whole 16-bit space and wraps past 0xFFFF to 0, as
// matter.js ExchangeCounter.getIncrementedCounter does
// (packages/protocol/src/protocol/ExchangeManager.ts:801-807).
func TestOutboundExchangeIDUsesTheFull16Bits(t *testing.T) {
	t.Parallel()
	b := newUnstartedBridge(t)
	b.outboundExchangeID.Store(0x7FFE)
	for _, want := range []uint16{0x7FFF, 0x8000, 0x8001} {
		if got := b.nextOutboundExchangeID(); got != want {
			t.Fatalf("exchange id = %#04x, want %#04x", got, want)
		}
	}
	b.outboundExchangeID.Store(0xFFFE)
	for _, want := range []uint16{0xFFFF, 0x0000, 0x0001} {
		if got := b.nextOutboundExchangeID(); got != want {
			t.Fatalf("exchange id after the wrap = %#04x, want %#04x", got, want)
		}
	}
}
