// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// stalledStore is a vendor store whose Load answers once release is
// closed: the service's first build waits for it, as for a ledger that
// does not answer.
type stalledStore struct{ release chan struct{} }

func (s stalledStore) Load(ctx context.Context) ([]byte, error) {
	select {
	case <-s.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (stalledStore) Save(context.Context, []byte) error { return nil }

// TestCloseDoesNotWaitForAVendorInfoBuild (review II, R2-P6-err-2317): a
// first VendorInfo whose build does not finish — its store or ledger does
// not answer — holds neither Close
// nor a second call past their own contexts; the build that finishes after
// Close is refused. Before, VendorInfo held vendorMu across the refresh
// and Close waited for it.
func TestCloseDoesNotWaitForAVendorInfoBuild(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var once sync.Once
	answer := func() { once.Do(func() { close(release) }) }
	defer answer()
	_, _, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) {
		cfg.DCL.Offline = true
		cfg.DCL.VendorStore = stalledStore{release}
	})
	first := make(chan error, 1)
	go func() {
		_, err := c.VendorInfo(context.Background())
		first <- err
	}()
	// A second call waits for the first's build, within its own ctx.
	second, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := c.VendorInfo(second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a second VendorInfo during the first's refresh = %v, want its ctx's deadline", err)
	}
	ctx, cancelClose := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelClose()
	closed := make(chan error, 1)
	go func() { closed <- c.Close(ctx) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second): // a held lock never releases: this bound only names the failure
		t.Fatal("Close waits for a VendorInfo whose build does not finish")
	}
	answer()
	select {
	case err := <-first:
		if !errors.Is(err, controller.ErrConfiguration) {
			t.Fatalf("the first VendorInfo after Close = %v, want ErrConfiguration", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the first VendorInfo did not return after the ledger answered")
	}
}
