// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
)

// delayPipe is a pipe end that records the MRP delay set on it.
type delayPipe struct {
	*pipeEnd
	mu    sync.Mutex
	delay *time.Duration
}

func (p *delayPipe) SetPeerAdditionalMrpDelay(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.delay = &d
}

func (p *delayPipe) got() *time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.delay
}

// The responder sets the scope's additionalMrpDelay, else its own default,
// as the exchange's peerAdditionalMrpDelay before it answers the Init
// (matter.js BdxProtocol.ts:139-142 at 862ad75f); without either the
// exchange keeps its own.
func TestResponder_SetsTheExchangeMrpDelay(t *testing.T) {
	t.Parallel()
	scoped := 3 * time.Second
	for name, tc := range map[string]struct {
		def   time.Duration
		scope *time.Duration
		want  *time.Duration
	}{
		"none":              {-1, nil, nil},
		"responder default": {2 * time.Second, nil, new(2 * time.Second)},
		"scope wins":        {2 * time.Second, &scoped, &scoped},
		"zero default":      {0, nil, new(time.Duration(0))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r := bdx.NewResponder(nil)
			r.SetAdditionalMrpDelay(tc.def)
			if !r.EnablePeerForScope(testPeer, "ota", memStorage{"img": testData(10)}, bdx.ScopeConfig{
				PreferredDriverModes: []bdx.DriverMode{bdx.ReceiverDrive}, MessageTimeout: time.Second, AdditionalMrpDelay: tc.scope,
			}) {
				t.Fatal("enable refused")
			}
			a, b := newPipe(1178)
			ex := &delayPipe{pipeEnd: b}
			init := bdx.Init{TransferControl: bdx.TransferControl{ReceiverDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/img")}
			payload, _ := init.Encode()
			done := make(chan struct{})
			go func() {
				defer close(done)
				r.HandleExchange(ctx, testPeer, bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: payload}, ex)
			}()
			m := bdx.NewMessenger(a, time.Second)
			if _, _, err := m.Next(ctx, bdx.MessageReceiveAccept); err != nil {
				t.Fatalf("accept: %v", err)
			}
			got := ex.got()
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("delay %v set, want none", *got)
			case tc.want != nil && (got == nil || *got != *tc.want):
				t.Fatalf("delay %v, want %v", got, *tc.want)
			}
			_ = a.Close()
			cancel()
			<-done
		})
	}
}
