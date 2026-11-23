// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bdx"
	"thingfabric.dev/go-fabric/clock"
)

// TestResponder_SetClockBesideHandleExchange holds R2-C2-conc-1905: the
// clock a session is built with is read under the responder's lock, so
// SetClock beside an exchange being accepted is not a data race (run
// with -race).
func TestResponder_SetClockBesideHandleExchange(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := bdx.NewResponder(nil)
	if !r.EnablePeerForScope(testPeer, "ota", memStorage{"img": testData(10)}, bdx.ScopeConfig{
		PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}, MessageTimeout: time.Second,
	}) {
		t.Fatal("enable refused")
	}
	stop := make(chan struct{})
	setterDone := make(chan struct{})
	go func() {
		defer close(setterDone)
		for {
			select {
			case <-stop:
				return
			default:
				r.SetClock(clock.System{})
			}
		}
	}()
	for range 20 {
		a, b := newPipe(1178)
		init := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/img")}
		payload, _ := init.Encode()
		done := make(chan struct{})
		exCtx, exCancel := context.WithCancel(ctx)
		go func() {
			defer close(done)
			r.HandleExchange(exCtx, testPeer, bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: payload}, b)
		}()
		m := bdx.NewMessenger(a, time.Second)
		if _, _, err := m.Next(ctx, bdx.MessageReceiveAccept); err != nil {
			t.Fatalf("accept: %v", err)
		}
		_ = a.Close()
		exCancel()
		<-done
	}
	close(stop)
	<-setterDone
}
