// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package filter

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestMatterNow: outside a node LastChangedTime takes the clock's seconds
// since 2000-01-01 UTC, none before the epoch or at the null value
// (W3-club2-03); with the node's Matter time attached it takes that
// (issue 445).
func TestMatterNow(t *testing.T) {
	t.Parallel()
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at   time.Time
		want uint32
		ok   bool
	}{
		{epoch, 0, true},
		{epoch.Add(90 * time.Second), 90, true},
		{epoch.Add(-time.Second), 0, false},
		{epoch.Add((1<<32 - 1) * time.Second), 0, false},
	} {
		s := &Server{cfg: Config{Clock: clocktest.New(c.at)}}
		if got, ok := s.matterNow(); (ok && got != c.want) || ok != c.ok {
			t.Errorf("matterNow at %v = %d, %v; want %d, %v", c.at, got, ok, c.want, c.ok)
		}
	}
	s := &Server{cfg: Config{Clock: clocktest.New(epoch)}}
	s.AttachMatterTime(func() (uint32, bool) { return 777, true })
	if got, ok := s.matterNow(); got != 777 || !ok {
		t.Errorf("attached: %d, %v; want the node's 777", got, ok)
	}
	if s.RequiresMatterTime() {
		t.Error("ResourceMonitoring requires no time")
	}
}
