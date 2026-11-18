// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestControllerAddrDuringRestart (R2-P6-conc-2509): the site's resolver
// reads Controller.Addr from the devices' goroutines while a test restarts
// the controller; the controller is safe for concurrent use, so -race
// reports nothing.
func TestControllerAddrDuringRestart(t *testing.T) {
	t.Parallel()
	s := fabrictest.NewSite(t, fabrictest.Options{})
	c := s.AddController(fabrictest.ControllerOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = c.Addr()
			}
		}
	})
	if err := c.Restart(ctx); err != nil {
		t.Errorf("restart: %v", err)
	}
	close(stop)
	wg.Wait()
	if c.Addr() == nil {
		t.Error("no address after the restart")
	}
}
