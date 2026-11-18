// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/controller"
)

// countingImage counts the image reads of the service's report path.
type countingImage struct {
	imageReader
	reads *atomic.Int32
}

func (c countingImage) providerEndpoints(nodeID uint64) []uint16 {
	c.reads.Add(1)
	return c.imageReader.providerEndpoints(nodeID)
}

// reportsOn is a Reports installed n times on one configuration and
// attached to a service over the fixture whose image reads are counted;
// node "n" is the fixture's camera.
func reportsOn(t *testing.T, n int) (*Reports, controller.Config, *atomic.Int32, *Service) {
	t.Helper()
	f := newPSFixture()
	f.svc.ctrl = &controller.Controller{}
	reads := &atomic.Int32{}
	f.svc.image = countingImage{imageReader: f, reads: reads}
	r := &Reports{}
	var cfg controller.Config
	for range n {
		r.Install(&cfg)
	}
	if !r.claim() {
		t.Fatal("a fresh Reports refused its first service")
	}
	r.attach(f.svc)
	r.mu.Lock()
	r.known["n"] = psCamera
	r.mu.Unlock()
	return r, cfg, reads, f.svc
}

// TestReportsInstallIsIdempotent (review II, R2-P6-api-2208): a second
// Install on the same configuration wraps its hooks once, so a report
// reaches the service once. Before, each Install wrapped them again and
// every report reached the service once per Install.
func TestReportsInstallIsIdempotent(t *testing.T) {
	t.Parallel()
	_, once, onceReads, onceSvc := reportsOn(t, 1)
	once.HooksFor("n").SubscriptionAlive()
	onceSvc.w.wait()
	_, twice, twiceReads, twiceSvc := reportsOn(t, 2)
	twice.HooksFor("n").SubscriptionAlive()
	twiceSvc.w.wait()
	if onceReads.Load() == 0 || twiceReads.Load() != onceReads.Load() {
		t.Errorf("a report read the image %d times after two Installs, %d after one", twiceReads.Load(), onceReads.Load())
	}
}

// TestReportsForgetDeletedNode (review II, R2-P6-bound-2209): a node that
// leaves the registry leaves Reports' operational-id map too; a
// decommission alone keeps it, since Deleted follows and names the node
// by it. Before, the map kept every node ever seen.
func TestReportsForgetDeletedNode(t *testing.T) {
	t.Parallel()
	r, cfg, _, _ := reportsOn(t, 1)
	h := cfg.HooksFor("n")
	h.Decommissioned()
	r.mu.Lock()
	_, kept := r.known["n"]
	r.mu.Unlock()
	if !kept {
		t.Fatal("the decommission forgot the node's id before Deleted")
	}
	h.Deleted()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.known) != 0 {
		t.Errorf("known after Deleted = %v, want empty", r.known)
	}
}

// TestNewRefusesReportsInUse (review II, R2-P6-api-2208, decision 248):
// New refuses a Reports a live Service holds with ErrReportsInUse, which
// also matches ErrInvalidArgument as before; the refusal would otherwise
// have stopped the first Service's reports silently. Once that Service
// closed, the Reports serves a new one.
func TestNewRefusesReportsInUse(t *testing.T) {
	t.Parallel()
	d := newDevSite(t)
	d.startService()
	if svc, err := New(context.Background(), d.c, Config{Reports: d.rep}); !errors.Is(err, ErrReportsInUse) || !errors.Is(err, ErrInvalidArgument) {
		if svc != nil {
			d.run(svc.Close)
		}
		t.Fatalf("New with the Reports of a live Service = %v, want ErrReportsInUse and ErrInvalidArgument", err)
	}
	d.run(d.svc.Close)
	svc, err := New(context.Background(), d.c, Config{Reports: d.rep})
	if err != nil {
		t.Fatalf("New once the first Service closed: %v", err)
	}
	d.run(svc.Close)
}
