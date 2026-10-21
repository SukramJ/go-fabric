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
// next one, unless the server was quiesced meanwhile.
func (s *CommodityTariffServer) syncTick() {
	_ = s.SyncTime() // chip logs what it returns; there is no caller to answer
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Reset(s.syncEvery)
	}
}

// MatterQuiesce implements [contract.ClusterQuiescer]: the time sync
// stops until the next tariff update starts it again.
func (s *CommodityTariffServer) MatterQuiesce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.syncTimer != nil {
		s.syncTimer.Stop()
		s.syncTimer = nil
	}
}
