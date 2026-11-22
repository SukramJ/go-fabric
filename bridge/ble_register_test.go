// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/controller/ble/bletest"
)

// refusingGATT is a GATT peripheral whose platform refuses the Matter GATT
// service; it records every advertisement it is asked for.
type refusingGATT struct {
	*bletest.Radio
	refusal    error
	advertised int
}

func (r *refusingGATT) RegisterService(context.Context, []byte) (contract.GATTService, error) {
	return nil, r.refusal
}

func (r *refusingGATT) Advertise(ctx context.Context, adv contract.BLEAdvertisement) error {
	r.advertised++
	return r.Radio.Advertise(ctx, adv)
}

// TestStartBLERegistersBeforeItAdvertises (R2-P5-apidev-05, decision 114):
// with a contract.GATTPeripheral, StartBLE registers the Matter GATT
// service first — a refusal is StartBLE's error and nothing is advertised —
// and a service the platform accepts is served and advertised. Before, the
// registration ran inside Serve on a goroutine of its own: StartBLE
// returned nil, the refusal was a log line, and the advertisement started
// regardless.
func TestStartBLERegistersBeforeItAdvertises(t *testing.T) {
	t.Parallel()
	t.Run("refused", func(t *testing.T) {
		t.Parallel()
		p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:02", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
		refused := errors.New("the platform refuses the service")
		g := &refusingGATT{Radio: p.Radio(), refusal: refused}
		h := newConnHarness(t, Config{Listen: "127.0.0.1:0", GATT: g})
		if err := h.bridge.StartBLE(context.Background(), bleWindow); !errors.Is(err, refused) {
			t.Fatalf("StartBLE with a refused service = %v, want the refusal", err)
		}
		if g.advertised != 0 {
			t.Errorf("%d advertisements started for a service that is not there", g.advertised)
		}
		if _, advertising := p.Radio().Advertising(); advertising {
			t.Error("the radio advertises")
		}
	})
	t.Run("accepted", func(t *testing.T) {
		t.Parallel()
		p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:03", Discriminator: 3840, VendorID: 0xFFF1, ProductID: 0x8001}
		h := newConnHarness(t, Config{Listen: "127.0.0.1:0", GATT: p.Radio()})
		if err := h.bridge.StartBLE(context.Background(), bleWindow); err != nil {
			t.Fatalf("StartBLE: %v", err)
		}
		waitFor(t, "the GATT service served", func() bool { s, _ := p.Radio().Serving(); return s })
		waitFor(t, "advertised", func() bool { _, a := p.Radio().Advertising(); return a })
	})
	t.Run("both ports", func(t *testing.T) {
		t.Parallel()
		p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:04"}
		if _, err := New(wbEmptySnapshotter, newFakeAdvertiser(), Config{Listen: "127.0.0.1:0", GATT: p.Radio(), BLE: p.Radio()}, nil); err == nil {
			t.Error("New took GATT and the deprecated BLE together")
		}
	})
}
