// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

// catSubjectPrefix marks a CASE Authenticated Tag in the subject-id space:
// 0xFFFF_FFFD_0000_0000 to 0xFFFF_FFFD_FFFF_FFFF (chip NodeId.h).
const (
	catSubjectPrefix uint64 = 0xFFFF_FFFD_0000_0000
	catSubjectMask   uint64 = 0xFFFF_FFFF_0000_0000
)

// CATSubject is the subject id of a CASE Authenticated Tag.
func CATSubject(cat uint32) uint64 { return catSubjectPrefix | uint64(cat) }

func isCATSubject(s uint64) bool { return s&catSubjectMask == catSubjectPrefix }

// SubjectMatches is the Access Control subject match: candidate satisfies
// target when they are equal, or when both are CASE Authenticated Tags of
// the same identifier and candidate's version is at least target's (Core
// §9.10.5.7.3.1).
//
// Mirrors matter.js protocol/src/interaction/subjectMatches.ts:18-31
// (subjectMatches), which Check-In suppression shares with the access
// control algorithm.
func SubjectMatches(target, candidate uint64) bool {
	if target == candidate {
		return true
	}
	if !isCATSubject(target) || !isCATSubject(candidate) {
		return false
	}
	t, c := uint32(target), uint32(candidate) //nolint:gosec // a CAT subject's low 32 bits are the CAT (isCATSubject)
	return t>>16 == c>>16 && c&0xFFFF >= t&0xFFFF
}

// SubscriptionSubject is the subject of a session that holds at least one
// subscription: its peer node id and the CASE Authenticated Tags of its
// certificate. Mirrors matter.js IcdCheckInSuppression.ts:13-16
// (SubscriptionSubject).
type SubscriptionSubject struct {
	NodeID uint64
	CATs   []uint32
}

// MonitoredSubjectCovered reports whether a subscription subject matches a
// registration's monitored subject, by node id or by CAT. A covered
// registration gets no Check-In: its client already hears from the device
// (Core §9.16.5.3.2).
//
// Mirrors matter.js IcdCheckInSuppression.ts:24-36 (isMonitoredSubjectCovered).
func MonitoredSubjectCovered(monitoredSubject uint64, subjects []SubscriptionSubject) bool {
	for _, s := range subjects {
		if SubjectMatches(monitoredSubject, s.NodeID) {
			return true
		}
		for _, cat := range s.CATs {
			if SubjectMatches(monitoredSubject, CATSubject(cat)) {
				return true
			}
		}
	}
	return false
}

// SubscriptionSubjects is the device's view of the CASE sessions on one
// fabric that hold an active subscription — matter.js's
// activeSubscriptionSubjects (IcdCheckInSuppression.ts:39-53) over its
// session manager. A closing session is left out by the implementation, so
// it cannot suppress a Check-In the client needs. The bridge implements it
// over its subscription manager.
type SubscriptionSubjects func(fabricIndex uint8) []SubscriptionSubject
