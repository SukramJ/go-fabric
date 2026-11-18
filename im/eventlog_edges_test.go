// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import "testing"

// The edges of the event buffer's sizing and harvest that no other test
// reaches (R2-P5-tmut-18 rest).

// TestBufferConfigNormalized: an unset MinEventAllowance takes the
// default, and a MaxEventAllowance not above the minimum is raised past
// it, so a harvest always has something to drop.
func TestBufferConfigNormalized(t *testing.T) {
	t.Parallel()
	if got := (bufferConfig{}).normalized().MinEventAllowance; got != defaultMinEventAllowance {
		t.Errorf("unset MinEventAllowance normalised to %d, want %d", got, defaultMinEventAllowance)
	}
	if got := (bufferConfig{MinEventAllowance: 20, MaxEventAllowance: 20}).normalized().MaxEventAllowance; got <= 20 {
		t.Errorf("MaxEventAllowance equal to the minimum normalised to %d, want above 20", got)
	}
}

// countBy is the buffered records per priority.
func countBy(log *EventLog) map[EventPriority]int {
	out := map[EventPriority]int{}
	for _, r := range log.Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0) {
		out[r.Priority]++
	}
	return out
}

// TestEventLog_HarvestStartsPastTheMaximum: a buffer holding up to
// MaxEventAllowance records, and any number between the two allowances,
// is not harvested; one more brings it back to MinEventAllowance.
func TestEventLog_HarvestStartsPastTheMaximum(t *testing.T) {
	t.Parallel()
	log := harvestLog(20, 24, 0, 0)
	appendN(log, 21, EventPriorityInfo, 0x0039, 0x03)
	if got := countBy(log)[EventPriorityInfo]; got != 21 {
		t.Fatalf("21 records between the allowances: %d buffered, want 21", got)
	}
	appendN(log, 3, EventPriorityInfo, 0x0039, 0x03)
	if got := countBy(log)[EventPriorityInfo]; got != 24 {
		t.Fatalf("MaxEventAllowance records: %d buffered, want 24", got)
	}
	appendN(log, 1, EventPriorityInfo, 0x0039, 0x03)
	if got := countBy(log)[EventPriorityInfo]; got != 20 {
		t.Fatalf("one past MaxEventAllowance: %d buffered, want 20", got)
	}
}

// TestEventLog_HarvestFloorIsExact: the most recent MinInfoAllowance Info
// records are kept, and no more: older Info records go before Critical
// ones, the protected ones only after them.
func TestEventLog_HarvestFloorIsExact(t *testing.T) {
	t.Parallel()
	// 4 Critical, then 3 Info: the harvest at 7 > 6 drops 3; the 3 Info are
	// the floor, so 3 Critical go.
	log := harvestLog(4, 6, 3, 0)
	appendN(log, 4, EventPriorityCritical, 0x0028, 0x00)
	appendN(log, 3, EventPriorityInfo, 0x0039, 0x03)
	if got := countBy(log); got[EventPriorityInfo] != 3 || got[EventPriorityCritical] != 1 {
		t.Errorf("floor of 3 Info: %v, want 3 Info and 1 Critical", got)
	}
	// 4 Critical, then 4 Info: the harvest at 8 > 7 drops 4; the oldest Info
	// is above the floor and goes first, then 3 Critical.
	log = harvestLog(4, 7, 3, 0)
	appendN(log, 4, EventPriorityCritical, 0x0028, 0x00)
	appendN(log, 4, EventPriorityInfo, 0x0039, 0x03)
	if got := countBy(log); got[EventPriorityInfo] != 3 || got[EventPriorityCritical] != 1 {
		t.Errorf("4 Info over a floor of 3: %v, want 3 Info and 1 Critical", got)
	}
}

// TestEventLog_HarvestStopsAtTheFloor: when the floors leave fewer records
// to drop than the harvest wants, it drops what it may and keeps the rest,
// above MinEventAllowance.
func TestEventLog_HarvestStopsAtTheFloor(t *testing.T) {
	t.Parallel()
	log := harvestLog(2, 3, 3, 0)
	appendN(log, 4, EventPriorityInfo, 0x0039, 0x03)
	if got := countBy(log)[EventPriorityInfo]; got != 3 {
		t.Errorf("4 Info over a floor of 3 with room for 2: %d buffered, want 3", got)
	}
}
