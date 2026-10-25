// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/icd"
)

// TrustedEcosystemVendors are the administrators an ICD registration's
// single-admin guard ignores by default: Apple (0x1384, whose keychain
// does not subscribe), Samsung SmartThings (0x110a) and the Open Home
// Foundation / Home Assistant (0x134b). Mirrors matter.js
// packages/node/src/behavior/system/icd/IcdMultiAdminError.ts:37-41
// (TRUSTED_ECOSYSTEM_VENDORS).
var TrustedEcosystemVendors = []uint16{0x1384, 0x110a, 0x134b}

// ICDMultiAdminError refuses an ICD registration on a peer administered
// by more than one fabric of vendors not ignored: their Check-In clients
// would compete for the device's few slots. It is an [ErrICDOperation].
// Mirrors matter.js IcdMultiAdminError.ts:16-27.
type ICDMultiAdminError struct {
	// AdminVendorIDs are the counted vendors, each once.
	AdminVendorIDs []uint16
}

func (e *ICDMultiAdminError) Error() string {
	return fmt.Sprintf("client: ICD registration refused: peer has %d administrator fabrics from other vendors; "+
		"register with AllowMultiAdmin or add their vendor ids to IgnoredVendors", len(e.AdminVendorIDs))
}

// Unwrap makes the error an [ErrICDOperation], as matter.js's is an
// ImplementationError.
func (e *ICDMultiAdminError) Unwrap() error { return ErrICDOperation }

// AssertSingleAdmin refuses more than one administrator of vendors outside
// ignored unless allowMultiAdmin. Mirrors IcdMultiAdminError.ts:56-73.
func AssertSingleAdmin(vendorIDs, ignored []uint16, allowMultiAdmin bool) error {
	if allowMultiAdmin {
		return nil
	}
	var counted []uint16
	for _, v := range vendorIDs {
		if !slices.Contains(ignored, v) {
			counted = append(counted, v)
		}
	}
	if len(counted) <= 1 {
		return nil
	}
	var unique []uint16
	for _, v := range counted {
		if !slices.Contains(unique, v) {
			unique = append(unique, v)
		}
	}
	return &ICDMultiAdminError{AdminVendorIDs: unique}
}

// ICDPeerAsleepError is a held interaction whose LIT ICD peer did not wake
// in time. Mirrors matter.js IcdPeerAsleepError.ts:17-25.
type ICDPeerAsleepError struct {
	Peer    PeerAddress
	Timeout time.Duration
}

func (e *ICDPeerAsleepError) Error() string {
	return fmt.Sprintf("client: LIT ICD peer %s did not wake within %v", peerAddressString(e.Peer), e.Timeout)
}

// Errors of the ICD client.
var (
	// ErrICDOperation: an ICD client call its state refuses — matter.js's
	// ImplementationError of IcdClient (a registration exists, another
	// operation runs, no subscription, the peer offline or without CIP).
	ErrICDOperation = errors.New("client: ICD operation refused")
	// ErrICDAbandoned: an ICD operation another one, decommission or
	// shutdown abandoned — matter.js's AbortedError.
	ErrICDAbandoned = errors.New("client: ICD operation abandoned")
)

type icdAwaitKey struct{}

// icdAwait is the hold of one interaction: skip, or a timeout override.
type icdAwait struct {
	skip    bool
	timeout time.Duration
}

// WithICDAwaitTimeout bounds the hold of the Read, Write or Invoke ctx
// carries for a sleeping LIT ICD peer to d, in place of the time to the
// peer's next Check-In — matter.js's per-request icdAwaitTimeout
// (ClientRequest.ts:50).
func WithICDAwaitTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, icdAwaitKey{}, icdAwait{timeout: d})
}

// withoutICDHold sends the interaction ctx carries without a hold —
// matter.js's icdAwaitTimeout false (ClientInteraction.ts:194), for the
// ICD client's own interactions.
func withoutICDHold(ctx context.Context) context.Context {
	return context.WithValue(ctx, icdAwaitKey{}, icdAwait{skip: true})
}

// HoldUntilICDPeerAwake waits, for a peer that needs awaiting and sleeps,
// until w reports it awake: at most timeout (0: w's NextCheckInWithin,
// the time to the peer's next Check-In) on w's clock. A peer without a
// wakefulness, or awake, passes at once. A peer that does not wake in
// time is an [*ICDPeerAsleepError]; an ended ctx its error. Mirrors
// matter.js packages/node/src/behavior/system/icd/holdUntilIcdPeerAwake.ts:21-69.
func HoldUntilICDPeerAwake(ctx context.Context, w *icd.PeerWakefulness, peer PeerAddress, timeout time.Duration, log *slog.Logger) error {
	if w == nil || w.Awake() {
		return nil
	}
	if timeout <= 0 {
		timeout = w.NextCheckInWithin()
	}
	if log != nil {
		attrs := []any{slog.String("peer", peerAddressString(peer)), slog.Duration("timeout", timeout)}
		if due, ok := w.NextCheckInDue(); ok {
			attrs = append(attrs, slog.Time("next_check_in", due))
		}
		log.Info("matter.controller.client.icd_hold", attrs...)
	}
	switch w.AwaitAwake(ctx, timeout) {
	case icd.AwaitAborted:
		return ctx.Err()
	case icd.AwaitTimeout:
		return &ICDPeerAsleepError{Peer: peer, Timeout: timeout}
	}
	return nil
}
