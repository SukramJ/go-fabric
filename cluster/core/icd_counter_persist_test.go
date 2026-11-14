// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// refusingCounterStore refuses the ICDCounter's writes while refuse is set.
type refusingCounterStore struct {
	*memstore.Store
	refuse atomic.Bool
}

func (s *refusingCounterStore) SaveICDCounter(ctx context.Context, v uint32) error {
	if s.refuse.Load() {
		return errors.New("counter refused")
	}
	return s.Store.SaveICDCounter(ctx, v)
}

// TestICDCheckInCounterCoveredByTheStore: a Check-In goes out only with a
// counter the stored value covers — one a restart's boot bump does not
// hand out again — so a counter that ran past it while the store refused
// its writes sends no Check-In until a write lands (R2-P2-store-11).
func TestICDCheckInCounterCoveredByTheStore(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := &refusingCounterStore{Store: icdStore(t)}
		rec := &icdRecorder{}
		cfg := core.ICDManagementConfig{
			IdleModeDuration: 4, MaximumCheckInBackoff: 4, ActiveModeDuration: 2000, ActiveModeThreshold: 1000,
			Store: st, Clock: inflight.WithGroup(clocktest.New(icdEpoch), inflight.New("icd-test")),
		}
		work := inflight.Of(cfg.Clock)
		s := newICDServer(t, cfg)
		s.SetCheckInSender(rec.send)
		if err := s.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		icdWaitFor(t, "the start-time Check-In pass", func() bool { return !work.Busy() })
		if _, err := s.MatterInvoke(icdInvokeCtx(1, privAdminister), icddef.CmdRegisterClient, icddef.RegisterClientRequest{
			CheckInNodeId: 0x10, MonitoredSubject: 0x10, Key: icdKey(1), ClientType: icddef.ClientTypePermanent,
		}); err != nil {
			t.Fatal(err)
		}
		// The counter runs past what a restart's bump covers while the
		// store refuses it.
		st.refuse.Store(true)
		if _, err := s.Counter().Advance(icd.BootBump + 1); err != nil {
			t.Fatal(err)
		}
		s.EnterIdleMode()
		s.RequestActiveMode()
		icdWaitFor(t, "the pass to finish", func() bool { return !work.Busy() })
		s.Stop()
		var last uint32
		for _, req := range rec.sent {
			last = max(last, req.Counter)
		}
		st.refuse.Store(false)
		again := newICDServer(t, cfg)
		if err := again.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if next := again.Counter().Value() + 1; next <= last {
			t.Errorf("after the restart the next Check-In carries %d; %d went out before it", next, last)
		}
	})
}
