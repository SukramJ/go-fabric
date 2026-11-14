// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/store"
)

// The TermsAndConditions (TC) feature of GeneralCommissioning — ahead of
// matter.js (register row BD-chip-GeneralCommissioning-TermsAndConditions,
// notes/parity/by_design.md): matter.js's GeneralCommissioningServer has
// no terms-and-conditions logic (`class GeneralCommissioningServer`,
// GeneralCommissioningServer.ts:31 at 862ad75f, 285 lines). The behaviour
// is connectedhomeip's, `src/app/clusters/general-commissioning-server/
// GeneralCommissioningCluster.cpp` at 6170af84 with its
// TermsAndConditionsProvider (`src/app/server/TermsAndConditionsProvider.h`,
// `DefaultTermsAndConditionsProvider.cpp`):
//
//   - the node states the required terms — a version and the
//     acknowledgement bits ([TermsAndConditionsConfig]); the feature is
//     served when it does, as chip serves it under
//     CHIP_CONFIG_TERMS_AND_CONDITIONS_REQUIRED;
//   - SetTCAcknowledgements validates the version (not below the required
//     one, TCMinVersionNotMet) and the bits (every required bit set,
//     RequiredTCNotAccepted) before it records the acceptance
//     (HandleSetTCAcknowledgements :617-650); the acceptance is tentative
//     while the fail-safe is armed and committed at CommissioningComplete,
//     committed at once otherwise (:663-672, DefaultTermsAndConditionsProvider
//     CommitAcceptance :153-164);
//   - CommissioningComplete refuses without an acceptance
//     (TCAcknowledgementsNotReceived), with one below the version
//     (TCMinVersionNotMet) or missing a bit (RequiredTCNotAccepted)
//     (:476-510), then commits a tentative one (:512-525);
//   - a fail-safe that expires reverts a tentative acceptance
//     (OnPlatformEventHandler :149-157, RevertAcceptance :243-246);
//   - the last fabric's removal resets the acceptance (OnFabricRemoved
//     :357-374, ResetAcceptance :234-240), a factory reset erases the
//     store it lives in;
//   - TCAcknowledgementsRequired reports whether the acceptance satisfies
//     the requirement (GetAcknowledgementsRequired :192-217);
//     TCUpdateDeadline is the configured deadline or null
//     (GeneralCommissioningCluster.cpp:221-231).

// TermsAndConditionsConfig states the terms and conditions a commissioner
// has to acknowledge before CommissioningComplete succeeds: the TC feature
// of GeneralCommissioning. A nil config leaves the feature off (the
// surface without it, SetTCAcknowledgements unsupported).
type TermsAndConditionsConfig struct {
	// RequiredVersion is TCMinRequiredVersion: an acceptance below it is
	// TCMinVersionNotMet.
	RequiredVersion uint16
	// RequiredAcknowledgements are the bits of TCAcknowledgements an
	// acceptance has to carry: a missing one is RequiredTCNotAccepted.
	RequiredAcknowledgements uint16
	// UpdateDeadline is TCUpdateDeadline, the Matter epoch second by which
	// an updated acceptance is due; nil reads null.
	UpdateDeadline *uint32
}

// termsAcceptance is an acceptance a commissioner set: the version it
// acknowledged and the bits.
type termsAcceptance struct {
	version uint16
	acks    uint16
}

// satisfies reports whether the acceptance meets the requirement:
// chip TermsAndConditions::Validate (TermsAndConditionsProvider.h:81-97).
func (a termsAcceptance) satisfies(req TermsAndConditionsConfig) bool {
	return a.version >= req.RequiredVersion && req.RequiredAcknowledgements&a.acks == req.RequiredAcknowledgements
}

// gencommFeatureTC is the TermsAndConditions feature bit.
const gencommFeatureTC = uint32(gencommdef.FeatureTermsAndConditions)

// gencommInstTC is the definition bound to the TC feature: the five TC
// attributes and SetTCAcknowledgements with its response.
var gencommInstTC = mustInstance(gencommdef.Definition, spec.Options{Features: gencommFeatureTC})

// gencommTCAttrs are the attributes the feature gates, the ones a change
// of the acceptance or the requirement reports.
var gencommTCAttrs = []uint32{
	gencommdef.AttrTcAcceptedVersion, gencommdef.AttrTcMinRequiredVersion, gencommdef.AttrTcAcknowledgements,
	gencommdef.AttrTcAcknowledgementsRequired, gencommdef.AttrTcUpdateDeadline,
}

// acceptanceLocked is the acceptance in force: a tentative one over the
// committed one (DefaultTermsAndConditionsProvider::GetAcceptance :167-189).
func (g *GeneralCommissioning) acceptanceLocked() *termsAcceptance {
	if g.tcTemporal != nil {
		return g.tcTemporal
	}
	return g.tcCommitted
}

// acknowledgementsRequiredLocked is TCAcknowledgementsRequired: no
// acceptance, or one that does not satisfy the requirement
// (GetAcknowledgementsRequired :192-217).
func (g *GeneralCommissioning) acknowledgementsRequiredLocked() bool {
	acc := g.acceptanceLocked()
	return acc == nil || !acc.satisfies(*g.tc)
}

// readTermsLocked serves the five TC attributes; ok is false for any other
// id or without the feature.
func (g *GeneralCommissioning) readTermsLocked(attrID uint32) (any, bool) {
	if g.tc == nil {
		return nil, false
	}
	acc := g.acceptanceLocked()
	switch attrID {
	case gencommdef.AttrTcAcceptedVersion:
		if acc == nil {
			return uint16(0), true
		}
		return acc.version, true
	case gencommdef.AttrTcMinRequiredVersion:
		return g.tc.RequiredVersion, true
	case gencommdef.AttrTcAcknowledgements:
		if acc == nil {
			return uint16(0), true
		}
		return acc.acks, true
	case gencommdef.AttrTcAcknowledgementsRequired:
		return g.acknowledgementsRequiredLocked(), true
	case gencommdef.AttrTcUpdateDeadline:
		if g.tc.UpdateDeadline == nil {
			return spec.Nullable[uint32]{Null: true}, true
		}
		return spec.Nullable[uint32]{Value: *g.tc.UpdateDeadline}, true
	}
	return nil, false
}

// handleSetTCAcknowledgements is SetTCAcknowledgements
// (HandleSetTCAcknowledgements :617-672): the version and the bits are
// judged against the requirement, a changed acceptance is recorded —
// tentatively while the fail-safe is armed, committed otherwise — and
// reported.
func (g *GeneralCommissioning) handleSetTCAcknowledgements(ctx context.Context, fields any) (any, error) {
	req, ok := fields.(gencommdef.SetTcAcknowledgementsRequest)
	if !ok {
		if p, isPtr := fields.(*gencommdef.SetTcAcknowledgementsRequest); isPtr && p != nil {
			req = *p
		} else {
			return nil, fmt.Errorf("%w: SetTcAcknowledgementsRequest expected, got %T", errGencommInvalidArg, fields)
		}
	}
	accepted := termsAcceptance{version: req.TcVersion, acks: req.TcUserResponse}
	g.mu.Lock()
	if g.tc == nil {
		g.mu.Unlock()
		return nil, errGencommNoTerms
	}
	if accepted.version < g.tc.RequiredVersion {
		g.mu.Unlock()
		return gencommdef.SetTcAcknowledgementsResponse{ErrorCode: gencommdef.CommissioningErrorTcMinVersionNotMet}, nil
	}
	if g.tc.RequiredAcknowledgements&accepted.acks != g.tc.RequiredAcknowledgements {
		g.mu.Unlock()
		return gencommdef.SetTcAcknowledgementsResponse{ErrorCode: gencommdef.CommissioningErrorRequiredTcNotAccepted}, nil
	}
	before := g.acceptanceLocked()
	if before != nil && *before == accepted {
		g.mu.Unlock()
		return gencommdef.SetTcAcknowledgementsResponse{ErrorCode: gencommdef.CommissioningErrorOk}, nil
	}
	prev := g.tcTemporal
	g.tcTemporal = &accepted
	if g.failSafeArmed {
		g.tcTouched = true
	} else if err := g.commitTermsLocked(ctx); err != nil {
		// Outside a fail-safe nothing would revert an acceptance the store
		// refused: it would stay in force unpersisted, pass
		// CommissioningComplete's gate and make a retry a no-op. The
		// command fails with the acceptance as it was (review II,
		// R2-P1-core-04).
		g.tcTemporal = prev
		g.mu.Unlock()
		return nil, err
	}
	g.dataVersion.Bump()
	g.mu.Unlock()
	g.changes.Notify(gencommdef.AttrTcAcceptedVersion, gencommdef.AttrTcAcknowledgements, gencommdef.AttrTcAcknowledgementsRequired)
	return gencommdef.SetTcAcknowledgementsResponse{ErrorCode: gencommdef.CommissioningErrorOk}, nil
}

// errGencommNoTerms is SetTCAcknowledgements without the feature — the
// dispatcher refuses the command as unsupported before this; it guards the
// direct call.
var errGencommNoTerms = errors.New("core: GeneralCommissioning serves no terms and conditions")

// termsGateLocked is CommissioningComplete's terms-and-conditions gate
// (:476-510): the error code that refuses the command, or
// CommissioningErrorOK.
func (g *GeneralCommissioning) termsGateLocked() uint8 {
	if g.tc == nil {
		return CommissioningErrorOK
	}
	acc := g.acceptanceLocked()
	switch {
	case acc == nil:
		return uint8(gencommdef.CommissioningErrorTcAcknowledgementsNotReceived)
	case acc.version < g.tc.RequiredVersion:
		return uint8(gencommdef.CommissioningErrorTcMinVersionNotMet)
	case g.tc.RequiredAcknowledgements&acc.acks != g.tc.RequiredAcknowledgements:
		return uint8(gencommdef.CommissioningErrorRequiredTcNotAccepted)
	}
	return CommissioningErrorOK
}

// commitTermsLocked commits a tentative acceptance: persisted when the
// server has a store, then the one in force (CommitAcceptance :153-164).
func (g *GeneralCommissioning) commitTermsLocked(ctx context.Context) error {
	if g.tcTemporal == nil {
		g.tcTouched = false
		return nil
	}
	if err := g.persistTermsLocked(ctx, g.tcTemporal); err != nil {
		return err
	}
	g.tcCommitted, g.tcTemporal, g.tcTouched = g.tcTemporal, nil, false
	return nil
}

// persistTermsLocked writes acc as the stored acceptance (nil: none) when
// the server has a store; it does not change the acceptance in memory.
func (g *GeneralCommissioning) persistTermsLocked(ctx context.Context, acc *termsAcceptance) error {
	if g.tcSettings == nil {
		return nil
	}
	v := ""
	if acc != nil {
		v = strconv.FormatUint(uint64(acc.version), 10) + ":" + strconv.FormatUint(uint64(acc.acks), 10)
	}
	if err := g.tcSettings.SetSetting(ctx, store.SettingTermsAcceptance, v); err != nil {
		return fmt.Errorf("core: GeneralCommissioning: persist the terms acceptance: %w", err)
	}
	return nil
}

// revertTermsLocked drops a tentative acceptance the expired fail-safe
// window set (RevertAcceptance :243-246; only after a
// SetTCAcknowledgements of that window, :149-157). It reports whether the
// attributes moved.
func (g *GeneralCommissioning) revertTermsLocked() bool {
	touched := g.tcTouched
	g.tcTouched = false
	if !touched || g.tcTemporal == nil {
		return false
	}
	g.tcTemporal = nil
	return true
}

// LastFabricRemoved resets the terms-and-conditions acceptance once the
// node's last fabric is gone — "the device SHALL delete all Matter
// related data on the node which was created since it was commissioned"
// (OnFabricRemoved :357-374, ResetAcceptance :234-240). The node calls it
// after the fabric's removal; it is a no-op without the feature.
func (g *GeneralCommissioning) LastFabricRemoved(ctx context.Context) error {
	g.mu.Lock()
	if g.tc == nil || (g.tcCommitted == nil && g.tcTemporal == nil) {
		g.mu.Unlock()
		return nil
	}
	g.tcCommitted, g.tcTemporal, g.tcTouched = nil, nil, false
	var err error
	if d, ok := g.tcSettings.(interface {
		DeleteSetting(ctx context.Context, key string) error
	}); ok && g.tcSettings != nil {
		err = d.DeleteSetting(ctx, store.SettingTermsAcceptance)
	} else if g.tcSettings != nil {
		err = g.tcSettings.SetSetting(ctx, store.SettingTermsAcceptance, "")
	}
	g.dataVersion.Bump()
	g.mu.Unlock()
	g.changes.Notify(gencommdef.AttrTcAcceptedVersion, gencommdef.AttrTcAcknowledgements, gencommdef.AttrTcAcknowledgementsRequired)
	if err != nil {
		return fmt.Errorf("core: GeneralCommissioning: reset the terms acceptance: %w", err)
	}
	return nil
}

// RestoreTermsAcceptance reads the committed acceptance from the server's
// store ([GeneralCommissioningConfig.Settings]) — what a restart comes back
// with (DefaultTermsAndConditionsStorageDelegate::Get :63-115). A missing
// or empty value is no acceptance; a malformed one is an error.
func (g *GeneralCommissioning) RestoreTermsAcceptance(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.tc == nil || g.tcSettings == nil {
		return nil
	}
	v, ok, err := g.tcSettings.GetSetting(ctx, store.SettingTermsAcceptance)
	if err != nil {
		return fmt.Errorf("core: GeneralCommissioning: read the terms acceptance: %w", err)
	}
	if !ok || v == "" {
		g.tcCommitted = nil
		return nil
	}
	version, acks, found := strings.Cut(v, ":")
	ver, err1 := strconv.ParseUint(version, 10, 16)
	ack, err2 := strconv.ParseUint(acks, 10, 16)
	if !found || err1 != nil || err2 != nil {
		return fmt.Errorf("core: GeneralCommissioning: terms acceptance %q: malformed", v)
	}
	g.tcCommitted = &termsAcceptance{version: uint16(ver), acks: uint16(ack)}
	return nil
}

// TermsAndConditions reports the served requirement; nil without the
// feature.
func (g *GeneralCommissioning) TermsAndConditions() *TermsAndConditionsConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.tc == nil {
		return nil
	}
	c := *g.tc
	return &c
}
