// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestRunIsBoundedWithoutADeadline (R2-P6-test-2511): Site.Run with a
// context that has no deadline ends with ErrWaited once Options.Wait has
// passed, as Eventually does, and the fn it gave up on sees its context
// end.
func TestRunIsBoundedWithoutADeadline(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{Wait: 50 * time.Millisecond})
	left := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- site.Run(context.Background(), func(ctx context.Context) error {
			defer close(left)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	select {
	case err := <-result:
		if !errors.Is(err, fabrictest.ErrWaited) {
			t.Fatalf("Run = %v, want ErrWaited", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return: unbounded without a deadline")
	}
	select {
	case <-left:
	case <-time.After(10 * time.Second):
		t.Error("the fn Run gave up on did not see its context end")
	}
}
