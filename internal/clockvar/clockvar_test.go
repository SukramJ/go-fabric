// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clockvar_test

import (
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/internal/clockvar"
)

func TestVar(t *testing.T) {
	t.Parallel()
	var v clockvar.Var
	if v.Load() != (clock.System{}) {
		t.Fatal("the zero Var is not System")
	}
	f := clocktest.New(time.Unix(0, 0))
	v.Store(f)
	if v.Load() != clock.Clock(f) {
		t.Fatal("Load is not the stored clock")
	}
	v.Store(nil)
	if v.Load() != (clock.System{}) {
		t.Fatal("Store(nil) is not System")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				v.Store(f)
				_ = v.Load().Now()
			}
		})
	}
	wg.Wait()
}
