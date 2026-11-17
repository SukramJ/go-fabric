// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ble_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/ble"
)

// deviceFeed is a discovery transport that reports each device sent on it.
type deviceFeed chan discovery.Device

func (deviceFeed) Kind() discovery.TransportKind { return discovery.TransportBLE }

func (f deviceFeed) FindContinuously(ctx context.Context, _ discovery.Identifier, found func(discovery.Device)) error {
	for {
		select {
		case d := <-f:
			found(d)
		case <-ctx.Done():
			return nil
		}
	}
}

// TestDialMarksTheRaceAttemptAnsweredOnConnect: a race attempt whose
// peripheral accepted the GATT connection has answered — while it waits
// for the BTP handshake it is not ended for a candidate found later, so
// with every place held by such attempts the later candidates wait
// (N3-rwrace-2).
func TestDialMarksTheRaceAttemptAnsweredOnConnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		raceClock := clocktest.New(time.Unix(1_000_000, 0))
		// The handshake's own timeout never fires: the attempts stay in it.
		dialClock := clocktest.New(time.Unix(1_000_000, 0))
		feed := make(deviceFeed)
		var mu sync.Mutex
		ended := map[string]bool{}
		s := discovery.Race(t.Context(), []discovery.Transport{feed},
			discovery.SessionOptions{Clock: raceClock, Timeout: 10 * time.Minute},
			func(ctx context.Context, c discovery.Candidate, _ func() bool) (string, error) {
				central := &silentCentral{conn: &silent{gone: make(chan struct{})}}
				_, err := ble.Dial(ctx, central, ble.Peripheral{Address: c.Device.InstanceID}, ble.DialOptions{Clock: dialClock})
				mu.Lock()
				ended[c.Device.InstanceID] = true
				mu.Unlock()
				return "", err
			})
		// More devices than the race has places: the ones without a place
		// reach their ceiling while every place is held.
		for i := range 24 {
			feed <- discovery.Device{InstanceID: fmt.Sprintf("dev-%02d", i)}
			synctest.Wait()
		}
		for range 120 {
			raceClock.Advance(time.Second)
			synctest.Wait()
		}
		mu.Lock()
		n := len(ended)
		mu.Unlock()
		if n != 0 {
			t.Fatalf("%d attempts in the BTP handshake were ended for later candidates, want none", n)
		}
		s.Cancel(nil)
		for range 10 {
			raceClock.Advance(time.Second)
			synctest.Wait()
		}
	})
}
