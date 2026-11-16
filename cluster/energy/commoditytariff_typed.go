// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ct "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	"github.com/SukramJ/go-fabric/contract"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.SelfHeld]; SetAttributes is a tariff
// update's.
var (
	_ spec.AttributeSetter     = (*CommodityTariffServer)(nil)
	_ spec.CommandRegistrar    = (*CommodityTariffServer)(nil)
	_ spec.EventEmitting       = (*CommodityTariffServer)(nil)
	_ spec.BeforeWriteSetter   = (*CommodityTariffServer)(nil)
	_ contract.ClusterQuiescer = (*CommodityTariffServer)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's tariff, as
// one [CommodityTariffServer.UpdateTariff] of the eleven tariff
// attributes the values carry — validated as a whole, refused as a whole.
// Every one of them null is [CommodityTariffServer.ClearTariff]. The
// current and next day, day entry and tariff components are derived from
// the tariff and the clock ([spec.ErrNotHeld]).
func (s *CommodityTariffServer) SetAttributes(values map[uint32]any) error {
	var t Tariff
	null := 0
	for id, raw := range values {
		v, err := s.CheckSet(id, raw, nil)
		if err != nil {
			return err
		}
		if v == nil {
			null++
		}
		if err := t.set(id, v); err != nil {
			return err
		}
	}
	if len(values) == 0 {
		return nil
	}
	if null == len(values) {
		return s.ClearTariff()
	}
	return s.UpdateTariff(t)
}

// set takes one checked tariff attribute value into t: the generated
// types are the Tariff's.
func (t *Tariff) set(id uint32, v any) error {
	switch id {
	case ct.AttrTariffUnit:
		if n, ok := v.(uint8); ok {
			u := TariffUnit(n)
			t.TariffUnit = &u
		}
	case ct.AttrStartDate:
		if n, ok := v.(uint32); ok {
			t.StartDate = &n
		}
	case ct.AttrDefaultRandomizationOffset:
		if n, ok := v.(int16); ok {
			t.DefaultRandomizationOffset = &n
		}
	case ct.AttrDefaultRandomizationType:
		if n, ok := v.(uint8); ok {
			r := DayEntryRandomizationType(n)
			t.DefaultRandomizationType = &r
		}
	case ct.AttrTariffInfo:
		if info, ok := v.(TariffInformation); ok {
			t.TariffInfo = &info
		}
	case ct.AttrDayEntries:
		t.DayEntries, _ = listOf[TariffDayEntry](v)
	case ct.AttrDayPatterns:
		t.DayPatterns, _ = listOf[TariffDayPattern](v)
	case ct.AttrTariffComponents:
		t.TariffComponents, _ = listOf[TariffComponent](v)
	case ct.AttrTariffPeriods:
		t.TariffPeriods, _ = listOf[TariffPeriod](v)
	case ct.AttrIndividualDays:
		t.IndividualDays, _ = listOf[TariffDay](v)
	case ct.AttrCalendarPeriods:
		t.CalendarPeriods, _ = listOf[TariffCalendarPeriod](v)
	default:
		return fmt.Errorf("%w: energy: CommodityTariff attribute 0x%04X is derived from the tariff", spec.ErrNotHeld, id)
	}
	return nil
}

// startSyncLocked arms the time sync ([CommodityTariffConfig.SyncInterval])
// unless it runs or is not configured.
func (s *CommodityTariffServer) startSyncLocked() {
	if s.syncEvery <= 0 || s.syncTimer != nil {
		return
	}
	s.syncTimer = s.clock.AfterFunc(s.syncEvery, s.syncTick)
}

// syncTick is one time sync: [CommodityTariffServer.SyncTime], then the
// next one — unless the server was quiesced meanwhile, which a tick
// already waiting on the lock sees before it stores anything (review II,
// R2-P6-conc-1621).
func (s *CommodityTariffServer) syncTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncTimer == nil {
		return
	}
	_ = s.syncTimeLocked() // chip logs what it returns; there is no caller to answer
	if s.syncTimer != nil {
		s.syncTimer.Reset(s.syncEvery)
	}
}

// MatterQuiesce implements [contract.ClusterQuiescer]: the time sync
// stops. The next tariff update starts it again, and so does the
// [CommodityTariffServer.SetEndpoint] of a reassembly that puts the server
// back.
func (s *CommodityTariffServer) MatterQuiesce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Stop()
		s.syncTimer = nil
		s.quiesced = true
	}
}

// SetEndpoint implements [contract.EndpointReceiver]; the bridge calls it
// at reassembly, for every server of the new topology. A server
// [CommodityTariffServer.MatterQuiesce] stopped comes back here: it runs
// one [CommodityTariffServer.SyncTime] at once — a delayed tariff whose
// StartDate passed while it was out activates, the current day and day
// entry are derived for now — and arms the sync again, as EvseServer
// re-arms its deadline (review II, R2-P6-fsm-1607).
func (s *CommodityTariffServer) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	if s.quiesced {
		s.quiesced = false
		_ = s.syncTimeLocked() // as a tick: there is no caller to answer
		s.startSyncLocked()
	}
	s.mu.Unlock()
	s.SelfHeld.SetEndpoint(endpoint)
}
