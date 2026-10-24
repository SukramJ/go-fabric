// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/icd/IcdCheckInSuppressionTest.ts
// at 862ad75f: a monitored subject covered by a subscription's node id or by
// a CAT of a later version, and not covered otherwise. matter.js's
// catSubjects are subject ids (NodeId.fromCaseAuthenticatedTag); Go carries
// the session's CATs as the 32-bit tags themselves.
package icd_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/icd"
)

func TestIsMonitoredSubjectCovered(t *testing.T) {
	t.Parallel()
	t.Run("is covered when a subscription subject node id matches", func(t *testing.T) {
		t.Parallel()
		if !icd.MonitoredSubjectCovered(0x1234, []icd.SubscriptionSubject{{NodeID: 0x1234}}) {
			t.Fatal("not covered")
		}
	})
	t.Run("is covered when a subscription CAT matches the monitored CAT", func(t *testing.T) {
		t.Parallel()
		monitored := icd.CATSubject(0xabcd0001)
		if !icd.MonitoredSubjectCovered(monitored, []icd.SubscriptionSubject{{NodeID: 0x9, CATs: []uint32{0xabcd0002}}}) {
			t.Fatal("not covered")
		}
	})
	t.Run("is not covered when nothing matches", func(t *testing.T) {
		t.Parallel()
		if icd.MonitoredSubjectCovered(0x1234, []icd.SubscriptionSubject{{NodeID: 0x9}}) {
			t.Fatal("covered")
		}
	})
	t.Run("is not covered with no subscriptions", func(t *testing.T) {
		t.Parallel()
		if icd.MonitoredSubjectCovered(0x1234, nil) {
			t.Fatal("covered")
		}
	})
}

// TestSubjectMatches holds subjectMatches.ts:18-31 beyond the four cases: a
// CAT of an older version and a CAT of another identifier do not match, nor
// does a node id that looks like neither.
func TestSubjectMatches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		target, candidate uint64
		want              bool
	}{
		{icd.CATSubject(0xabcd0002), icd.CATSubject(0xabcd0001), false},
		{icd.CATSubject(0xabcd0001), icd.CATSubject(0xabce0001), false},
		{icd.CATSubject(0xabcd0001), 0xabcd0001, false},
		{0x42, 0x42, true},
	} {
		if got := icd.SubjectMatches(tc.target, tc.candidate); got != tc.want {
			t.Errorf("SubjectMatches(%#x, %#x) = %v", tc.target, tc.candidate, got)
		}
	}
}
