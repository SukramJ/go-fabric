// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
)

// heldReadServer answers attribute 0 with a boolean: once armed, its first
// read closes entered and waits for release before it answers the value
// the device had then (false); every later read answers true.
type heldReadServer struct {
	mu               sync.Mutex
	armed, held      bool
	entered, release chan struct{}
}

func (*heldReadServer) MatterClusterID() uint32 { return 0x0045 }

func (s *heldReadServer) MatterRead(uint32) (any, bool) {
	s.mu.Lock()
	first := s.armed && !s.held
	if first {
		s.held = true
	}
	armed := s.armed
	s.mu.Unlock()
	if first {
		close(s.entered)
		<-s.release
		return false, true
	}
	return armed, true
}

func (*heldReadServer) MatterWrite(context.Context, uint32, any) error { return nil }

func (*heldReadServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }

func (*heldReadServer) MatterReportable() []uint32 { return []uint32{0} }

// TestPathValuesKeepTheLatestReading (review II, R2-P1-req2-10): two
// notifications that overlap around a change false → true leave the
// value read last remembered. Read outside the lock, the older reading
// could be stored last: the bridge then remembered false for a device
// that is true, and the next real change to false went unreported.
func TestPathValuesKeepTheLatestReading(t *testing.T) {
	t.Parallel()
	srv := &heldReadServer{entered: make(chan struct{}), release: make(chan struct{})}
	ep := &endpointpkg.Endpoint{ID: 5}
	ep.PublishClusterServers([]contract.ClusterServer{srv})
	path := im.ConcreteAttributePath{Endpoint: 5, Cluster: 0x0045, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	v := newPathValues(ep, []im.ConcreteAttributePath{path}) // remembers false
	srv.mu.Lock()
	srv.armed = true
	srv.mu.Unlock()

	first := make(chan struct{})
	go func() {
		defer close(first)
		v.changed() // reads false, held
	}()
	<-srv.entered
	second := make(chan struct{})
	go func() {
		defer close(second)
		v.changed() // reads true
	}()
	// The second notification either runs to its end now — its reading
	// outside the lock — or waits for the first, which holds the lock while
	// it reads; a bounded wait tells the two apart, then the first goes on.
	wait, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	select {
	case <-second:
	case <-wait.Done():
	}
	close(srv.release)
	<-first
	<-second

	v.mu.Lock()
	got := v.last[path]
	v.mu.Unlock()
	if !got.equal(readingOf(im.AttributeValue{Value: true})) {
		t.Fatalf("the remembered value is %+v after the device moved to true; the older reading was stored last", got)
	}
}
