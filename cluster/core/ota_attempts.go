// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/clock"
)

// OTAUpdateAttempt is one update flow of a requestor: a QueryImage, the
// offer the provider makes under one update token, and how far the
// requestor gets with that offer. An ApplyUpdateRequest under an unknown
// update token starts one too.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:11-137 at b25ed960
// (OtaUpdateAttempt).
//
// The attempt itself is read-only to whoever holds it: its transitions —
// the offer, the apply right, a status, the end — are methods of the
// [OTAUpdateAttempts] it came from, which its owner, the [OTAUpdateManager],
// calls under its own lock. The provider holds the attempt of its query and
// calls only Touch, OnEnd and the getters, as matter.js's provider does
// (OtaSoftwareUpdateProviderServer.ts:281, :286-289, :296 at b25ed960); a
// small lock of the attempt's own keeps those calls, which arrive on BDX
// goroutines, from racing the owner's.
//
// An attempt comes from [OTAUpdateAttempts.Begin]. The zero value is an
// attempt of no requestor that never begins: its getters answer zero
// values and Touch and OnEnd are safe, on the system clock.
type OTAUpdateAttempt struct {
	peer  OTAPeer
	clock clock.Clock

	mu           sync.Mutex
	token        []byte
	version      uint32
	offered      bool
	applyAllowed bool
	status       OTAUpdateStatus
	lastActivity time.Time
	applyDelay   time.Duration
	answered     bool
	ended        bool
	disposers    []func()
}

// Peer is the requestor of the attempt.
func (a *OTAUpdateAttempt) Peer() OTAPeer { return a.peer }

// Token is the update token of the offer; nil while the query made no
// offer.
func (a *OTAUpdateAttempt) Token() []byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.token)
}

// Version is the offered version; false while the query made no offer.
func (a *OTAUpdateAttempt) Version() (uint32, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.version, a.offered
}

// ApplyAllowed reports whether the requestor may apply the offered version
// under the token (OtaUpdateAttempts.ts:46-49 at b25ed960).
func (a *OTAUpdateAttempt) ApplyAllowed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applyAllowed
}

// Status is the attempt's last status; Querying when it begins.
func (a *OTAUpdateAttempt) Status() OTAUpdateStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// LastActivity is the time of the last status change or transfer progress.
func (a *OTAUpdateAttempt) LastActivity() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastActivity
}

// ApplyDelay is, with Applying, the delay the requestor waits out before
// it applies; zero otherwise.
func (a *OTAUpdateAttempt) ApplyDelay() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.applyDelay
}

// Answered reports whether the provider answered the query with the
// offer; only an answered attempt moves a queued update.
func (a *OTAUpdateAttempt) Answered() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.answered
}

// Ended reports whether the attempt ended.
func (a *OTAUpdateAttempt) Ended() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ended
}

// now is the attempt's clock, the system clock for the zero value.
func (a *OTAUpdateAttempt) now() time.Time { return clock.Or(a.clock).Now() }

// makeOffer records the offer, once; it allows the apply
// ([OTAUpdateAttempts.MakeOffer]).
func (a *OTAUpdateAttempt) makeOffer(token []byte, version uint32) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.offered {
		return fmt.Errorf("core: %s already made an offer", a.stringLocked())
	}
	a.token = slices.Clone(token)
	if a.token == nil {
		a.token = []byte{}
	}
	a.version = version
	a.offered = true
	a.applyAllowed = true
	a.lastActivity = a.now()
	return nil
}

// disallowApply takes the apply right of the offer
// ([OTAUpdateAttempts.DisallowApply]).
func (a *OTAUpdateAttempt) disallowApply() {
	a.mu.Lock()
	a.applyAllowed = false
	a.mu.Unlock()
}

// answer records that the provider answered the query with the offer
// (OtaUpdateAttempts.ts:90-93 at b25ed960); [OTAUpdateAttempts.Answer] calls it.
func (a *OTAUpdateAttempt) answer() {
	a.mu.Lock()
	a.answered = true
	a.mu.Unlock()
}

// setStatus records a status ([OTAUpdateAttempts.SetStatus]).
func (a *OTAUpdateAttempt) setStatus(status OTAUpdateStatus, applyDelay time.Duration) {
	a.mu.Lock()
	a.status = status
	a.applyDelay = 0
	if status == OTAUpdateApplying {
		a.applyDelay = applyDelay
	}
	a.lastActivity = a.now()
	a.mu.Unlock()
}

// Touch records activity that is no status change, such as BDX transfer
// progress (OtaUpdateAttempts.ts:102-105 at b25ed960). The provider calls
// it from a transfer's progress observer.
func (a *OTAUpdateAttempt) Touch() {
	a.mu.Lock()
	a.lastActivity = a.now()
	a.mu.Unlock()
}

// IsStale reports whether the requestor showed no activity for the OTA
// progress timeout, plus the delay of an apply.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:107-110 at b25ed960
// (isStale).
func (a *OTAUpdateAttempt) IsStale(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastActivity.Add(a.applyDelay + otaProgressTimeout).Before(now)
}

// OnEnd runs fn when the attempt ends, at once when it already ended
// (OtaUpdateAttempts.ts:112-119 at b25ed960). fn runs where the end runs —
// under the owner's lock — so it must not call back into the owner.
func (a *OTAUpdateAttempt) OnEnd(fn func()) {
	a.mu.Lock()
	if a.ended {
		a.mu.Unlock()
		fn()
		return
	}
	a.disposers = append(a.disposers, fn)
	a.mu.Unlock()
}

// end ends the attempt and runs its disposers ([OTAUpdateAttempts.End]).
func (a *OTAUpdateAttempt) end() {
	a.mu.Lock()
	if a.ended {
		a.mu.Unlock()
		return
	}
	a.ended = true
	disposers := a.disposers
	a.disposers = nil
	a.mu.Unlock()
	for _, fn := range disposers {
		fn()
	}
}

func (a *OTAUpdateAttempt) String() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stringLocked()
}

func (a *OTAUpdateAttempt) stringLocked() string {
	s := "OTA update"
	if a.offered {
		s += fmt.Sprintf(" to version %d", a.version)
	}
	s += fmt.Sprintf(" of node %d on fabric %d", a.peer.NodeID, a.peer.FabricIndex)
	if a.offered {
		s += " (" + hex.EncodeToString(a.token) + ")"
	}
	return s
}

// OTAUpdateAttempts is the set of running [OTAUpdateAttempt]s of all
// requestors, with the rules matter.js's SoftwareUpdateManager decides on
// attempts alone: offer, answer and replace, the staleness of an attempt,
// the admission of a new query (rows 0, 1, 3, 4 and 5 of
// SoftwareUpdateManager.admitQuery) and the apply table
// (applyUpdate, #applyAllowed). It holds no queue: an owner with one —
// controller/softwareupdate's Manager — adds the queue's half around
// these calls, and the owner without one is [NewOTAStandaloneManager].
//
// It has no lock: every method is called with its owner's lock held, so a
// rule and the owner's queue change in one step, as matter.js changes both
// in one #mutate (SoftwareUpdateManager.ts:539-561 at b25ed960). A
// requestor has at most one answered
// attempt (OtaUpdateAttempts.ts:139-143 at b25ed960).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:139-181 at b25ed960
// (OtaUpdateAttempts).
type OTAUpdateAttempts struct {
	clock    clock.Clock
	attempts []*OTAUpdateAttempt
	// ended are the attempts a rule ended (EndAttempt) that the owner
	// has not taken yet (TakeEnded).
	ended []*OTAUpdateAttempt
}

// NewOTAUpdateAttempts returns an empty set on clk; nil is [clock.System],
// as for the zero value, an empty set on the system clock.
func NewOTAUpdateAttempts(clk clock.Clock) *OTAUpdateAttempts {
	return &OTAUpdateAttempts{clock: clock.Or(clk)}
}

// otaAttemptsPerFabric is the share of running attempts one fabric's
// requestors hold; Begin ends the attempt of the fabric active longest ago
// to make room. The number is the module's (decision 134, by_design
// BD-Matter-OTAProvider-InProgressPerFabric): matter.js keeps an attempt
// until the requestor's next query, the manager's queue or the requestor's
// removal ends it, without a bound.
const otaAttemptsPerFabric = 16

// Begin starts an attempt, for a QueryImage that just arrived or an apply
// under an unknown update token.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:146-152 at b25ed960
// (begin); the per-fabric share is the module's.
func (s *OTAUpdateAttempts) Begin(peer OTAPeer) *OTAUpdateAttempt {
	var held []*OTAUpdateAttempt
	for _, a := range s.attempts {
		if a.peer.FabricIndex == peer.FabricIndex {
			held = append(held, a)
		}
	}
	if len(held) >= otaAttemptsPerFabric {
		victim := held[0]
		for _, a := range held[1:] {
			if a.LastActivity().Before(victim.LastActivity()) {
				victim = a
			}
		}
		s.EndAttempt(victim)
	}
	a := &OTAUpdateAttempt{peer: peer, clock: s.clock, status: OTAUpdateQuerying}
	a.lastActivity = a.now()
	s.attempts = append(s.attempts, a)
	a.OnEnd(func() { s.attempts = slices.DeleteFunc(s.attempts, func(x *OTAUpdateAttempt) bool { return x == a }) })
	return a
}

// Answer marks the attempt answered and returns the requestor's other
// attempts with an offer, which it replaces; it does not end them
// (AnswerAttempt does).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:154-158 at b25ed960
// (answer).
func (s *OTAUpdateAttempts) Answer(a *OTAUpdateAttempt) []*OTAUpdateAttempt {
	a.answer()
	return slices.DeleteFunc(s.Offered(a.peer), func(o *OTAUpdateAttempt) bool { return o == a })
}

// Find is the requestor's running attempt with an offer under token, nil
// when none (OtaUpdateAttempts.ts:160-165 at b25ed960).
func (s *OTAUpdateAttempts) Find(peer OTAPeer, token []byte) *OTAUpdateAttempt {
	for _, a := range s.Of(peer) {
		a.mu.Lock()
		match := a.offered && bytes.Equal(a.token, token)
		a.mu.Unlock()
		if match {
			return a
		}
	}
	return nil
}

// Offered are the requestor's running attempts with an offer, oldest first
// (OtaUpdateAttempts.ts:167-170 at b25ed960).
func (s *OTAUpdateAttempts) Offered(peer OTAPeer) []*OTAUpdateAttempt {
	return slices.DeleteFunc(s.Of(peer), func(a *OTAUpdateAttempt) bool {
		_, offered := a.Version()
		return !offered
	})
}

// Of are the requestor's running attempts, also those whose query made no
// offer yet, oldest first (OtaUpdateAttempts.ts:172-175 at b25ed960).
func (s *OTAUpdateAttempts) Of(peer OTAPeer) []*OTAUpdateAttempt {
	var out []*OTAUpdateAttempt
	for _, a := range s.attempts {
		if a.peer == peer {
			out = append(out, a)
		}
	}
	return out
}

// All are the running attempts, oldest first (OtaUpdateAttempts.ts:177-180 at b25ed960).
func (s *OTAUpdateAttempts) All() []*OTAUpdateAttempt { return slices.Clone(s.attempts) }

// MakeOffer records the offer of a, once; it allows the apply. An error
// reports a second offer, where matter.js throws an InternalError.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:74-83 at b25ed960
// (makeOffer).
func (s *OTAUpdateAttempts) MakeOffer(a *OTAUpdateAttempt, token []byte, version uint32) error {
	return a.makeOffer(token, version)
}

// DisallowApply takes the apply right of the offer of a, as its consent
// was removed.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:85-88 at b25ed960
// (disallowApply).
func (s *OTAUpdateAttempts) DisallowApply(a *OTAUpdateAttempt) { a.disallowApply() }

// SetStatus records a status of a; applyDelay counts only with Applying.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:95-100 at b25ed960
// (setStatus).
func (s *OTAUpdateAttempts) SetStatus(a *OTAUpdateAttempt, status OTAUpdateStatus, applyDelay time.Duration) {
	a.setStatus(status, applyDelay)
}

// End ends a and runs its disposers, the set's removal of it among them;
// ending it again does nothing. This is matter.js's plain attempt.end(),
// which leaves a queued update linked to it as it is — EndAttempt is the
// end the owner's queue learns of.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/OtaUpdateAttempts.ts:121-130 at b25ed960
// (end).
func (s *OTAUpdateAttempts) End(a *OTAUpdateAttempt) { a.end() }

// EndAttempt ends the attempt as a rule does, and keeps it for the owner's
// TakeEnded: matter.js's #endAttempt ends it and resets the queued update
// linked to it, which is the owner's half.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1216-1227 at b25ed960
// (#endAttempt), without the queue.
func (s *OTAUpdateAttempts) EndAttempt(a *OTAUpdateAttempt) {
	if a.Ended() {
		return
	}
	a.end()
	s.ended = append(s.ended, a)
}

// TakeEnded returns the attempts the rules ended since the last call, in
// the order they ended, for the owner to reset the queued updates linked
// to them.
func (s *OTAUpdateAttempts) TakeEnded() []*OTAUpdateAttempt {
	out := s.ended
	s.ended = nil
	return out
}

// AnswerAttempt marks the attempt answered and ends the requestor's
// attempts it replaces.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:2052-2057 at b25ed960
// (#answerAttempt).
func (s *OTAUpdateAttempts) AnswerAttempt(a *OTAUpdateAttempt) {
	for _, replaced := range s.Answer(a) {
		s.EndAttempt(replaced)
	}
}

// EndQuery ends the query's attempt unless the provider answered the query
// with its offer.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1782-1791 at b25ed960
// (endQuery).
func (s *OTAUpdateAttempts) EndQuery(a *OTAUpdateAttempt) {
	if !a.Answered() {
		s.EndAttempt(a)
	}
}

// OfferUpdate records the offer of the query, false when the attempt
// ended meanwhile — the provider then answers NotAvailable.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1743-1761 at b25ed960
// (offerUpdate).
func (s *OTAUpdateAttempts) OfferUpdate(a *OTAUpdateAttempt, token []byte, version uint32) bool {
	if a.Ended() {
		return false
	}
	return a.makeOffer(token, version) == nil
}

// AppliedVersionOf is the version an ApplyUpdateRequest under token asks
// to apply: newVersion, or for 0 the version offered under the token —
// false when the token has no running offer, as version 0 then names none.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1883-1895 at b25ed960
// (appliedVersionOf).
func (s *OTAUpdateAttempts) AppliedVersionOf(peer OTAPeer, token []byte, newVersion uint32) (uint32, bool) {
	if newVersion != 0 {
		return newVersion, true
	}
	if a := s.Find(peer, token); a != nil {
		return a.Version()
	}
	return 0, false
}

// ApplyAllowed decides the apply of version under the known token of a:
// another version is refused; an attempt already Applying is allowed, as
// the request was resent; else the offer's apply right decides, which a
// removed consent takes (rows 3 to 6 of the apply table).
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1945-1956 at b25ed960
// (#applyAllowed).
func (s *OTAUpdateAttempts) ApplyAllowed(a *OTAUpdateAttempt, version uint32) bool {
	if v, _ := a.Version(); v != version {
		return false
	}
	return a.Status() == OTAUpdateApplying || a.ApplyAllowed()
}

// ApplyUpdate decides an ApplyUpdateRequest by the apply table and returns
// the attempt it concerns, nil for version 0 under an unknown token (row
// 1, refused). An unknown token with a version above 0 begins an answered
// attempt that offers it (row 2, allowed); a known token is decided by
// ApplyAllowed. The owner then reports Applying for an allowed apply, and
// Cancelled for a refused one with an attempt.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1897-1943 at b25ed960
// (applyUpdate), without the status report.
func (s *OTAUpdateAttempts) ApplyUpdate(peer OTAPeer, token []byte, newVersion uint32) (*OTAUpdateAttempt, bool) {
	version, ok := s.AppliedVersionOf(peer, token, newVersion)
	if !ok {
		return nil, false
	}
	a := s.Find(peer, token)
	if a == nil {
		a = s.Begin(peer)
		_ = a.makeOffer(token, version) // a fresh attempt has no offer yet
		s.AnswerAttempt(a)
		return a, true
	}
	return a, s.ApplyAllowed(a, version)
}

// AdmitQuery decides whether a QueryImage that arrived on sessionID may
// get an offer while the requestor has an attempt with an offer, by the
// rows of matter.js's admission table that read attempts alone:
//
//   - row 0: every offered attempt that is stale ends;
//   - row 1: with none left, the query is admitted;
//   - rows 2 and 3: without a running transfer, the first offered attempt
//     ends and the query is admitted — returned as running, so an owner
//     with a queue judges row 2 (an Applying attempt linked to its queued
//     update that the requestor queries below the update's target) on it;
//   - row 4: a transfer on the query's session, or one active as recently
//     as the query's session, keeps the attempt and refuses (Busy);
//   - row 5: a transfer on another session active before the query's is
//     the requestor's restart: restart is true, and the owner cancels the
//     transfer outside its lock, then ends running with EndAttempt (or
//     answers Busy when the cancel fails).
//
// transfer is the requestor's running "ota" transfer, nil when none;
// activity, when set, reports when the peer last sent on a session
// (matter.js compares the two sessions' activeTimestamp), else the
// transfer's last block stands in for its session and the query's session
// is active at now.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1802-1857 at b25ed960
// (admitQuery, the decision in its first #mutate).
func (s *OTAUpdateAttempts) AdmitQuery(peer OTAPeer, sessionID uint16, transfer interface {
	Peer() bdx.Peer
	LastActivity() time.Time
}, activity func(sessionID uint16) (time.Time, bool), now time.Time,
) (admitted bool, running *OTAUpdateAttempt, restart bool) {
	for _, a := range s.Offered(peer) {
		if a.IsStale(now) {
			s.EndAttempt(a)
		}
	}
	offered := s.Offered(peer)
	if len(offered) == 0 {
		return true, nil, false
	}
	running = offered[0]
	if transfer == nil {
		s.EndAttempt(running)
		return true, running, false
	}
	if transfer.Peer().SessionID == sessionID || !transferOlder(transfer, sessionID, activity, now) {
		return false, running, false
	}
	return false, running, true
}

// transferOlder reports whether the transfer's session was last active
// before the query's session.
func transferOlder(transfer interface {
	Peer() bdx.Peer
	LastActivity() time.Time
}, querySessionID uint16, activity func(uint16) (time.Time, bool), now time.Time,
) bool {
	if activity != nil {
		t, okT := activity(transfer.Peer().SessionID)
		q, okQ := activity(querySessionID)
		if okT && okQ {
			return t.Before(q)
		}
	}
	return transfer.LastActivity().Before(now)
}

// UpdateApplied records the NotifyUpdateApplied under token: the attempt
// of the token, when one runs, is Done and ends. It returns that attempt,
// nil when the token is unknown; an owner with a queue links it before it
// judges its queued update.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1970-1980 at b25ed960
// (#updateApplied, the attempt).
func (s *OTAUpdateAttempts) UpdateApplied(peer OTAPeer, token []byte) *OTAUpdateAttempt {
	a := s.Find(peer, token)
	if a == nil {
		return nil
	}
	a.setStatus(OTAUpdateDone, 0)
	a.end()
	return a
}

// RemoveConsent applies the removal of the requestor's consent to its
// attempts: an attempt without an offer ends (its query then answers
// NotAvailable), and an offer of version — of any version when all is
// set — loses its apply right.
//
// Mirrors matter.js packages/node/src/behavior/system/software-update/SoftwareUpdateManager.ts:1670-1692 at b25ed960
// (removeConsent, the attempts).
func (s *OTAUpdateAttempts) RemoveConsent(peer OTAPeer, version uint32, all bool) {
	for _, a := range s.Of(peer) {
		v, offered := a.Version()
		switch {
		case !offered:
			a.end()
		case all || v == version:
			a.disallowApply()
		}
	}
}

// DropFabric ends the attempts of a removed fabric's requestors and
// returns those requestors, ascending by node id, so the owner withdraws
// their transfer scope: a later fabric that receives the index inherits
// none of them (decision 167). matter.js ends nothing at a fabric's
// removal.
func (s *OTAUpdateAttempts) DropFabric(fabricIndex uint8) []OTAPeer {
	var peers []OTAPeer
	for _, a := range s.All() {
		if a.peer.FabricIndex == fabricIndex {
			peers = append(peers, a.peer)
			s.EndAttempt(a)
		}
	}
	slices.SortFunc(peers, func(a, b OTAPeer) int {
		switch {
		case a.NodeID < b.NodeID:
			return -1
		case a.NodeID > b.NodeID:
			return 1
		}
		return 0
	})
	return slices.Compact(peers)
}

// Fabrics are the fabrics of the running attempts, ascending.
func (s *OTAUpdateAttempts) Fabrics() []uint8 {
	out := make([]uint8, 0, len(s.attempts))
	for _, a := range s.attempts {
		out = append(out, a.peer.FabricIndex)
	}
	slices.Sort(out)
	return slices.Compact(out)
}
