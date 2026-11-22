// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/fabrictest"
)

// TestRunIsBoundedWithoutADeadline (R2-P6-test-2511): Site.Run with a
// context that has no deadline ends with ErrWaited once Options.Wait has
// passed, as Eventually does, and the fn it gave up on sees its context
// end.
func TestRunIsBoundedWithoutADeadline(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{Wait: 50 * time.Millisecond})
	left := make(chan struct{})
	// Unbounded, Run does not return (go test's timeout).
	err := site.Run(context.Background(), func(ctx context.Context) error {
		defer close(left)
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, fabrictest.ErrWaited) {
		t.Fatalf("Run = %v, want ErrWaited", err)
	}
	<-left // the fn Run gave up on sees its context end
}
