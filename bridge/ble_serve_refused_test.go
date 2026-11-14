// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/ble/bletest"
)

// refusingRadio is a radio whose platform refuses the GATT service.
type refusingRadio struct{ *bletest.Radio }

func (refusingRadio) Serve(context.Context, []byte, func(contract.BLEConnection)) error {
	return errors.New("bletest: service registration refused")
}

// TestStartBLEServiceRefusedStopsAdvertising (review II, R2-P1-req-06): a
// GATT service the platform refuses ends the advertisement StartBLE began,
// so the node does not announce itself as commissionable over BLE with no
// service behind it.
func TestStartBLEServiceRefusedStopsAdvertising(t *testing.T) {
	p := &bletest.Peripheral{Address: "C0:FF:EE:00:00:05"}
	h := newConnHarness(t, Config{Listen: "127.0.0.1:0", BLE: refusingRadio{p.Radio()}})
	if err := h.bridge.StartBLE(t.Context(), bleWindow); err != nil {
		t.Fatalf("StartBLE: %v", err)
	}
	h.bridge.ble.mu.Lock()
	advDone := h.bridge.ble.advDone
	h.bridge.ble.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := waitBLE(ctx, advDone); err != nil {
		t.Fatalf("the advertisement still runs after the service was refused: %v", err)
	}
	if _, advertising := p.Radio().Advertising(); advertising {
		t.Fatal("advertising after the service was refused")
	}
}
