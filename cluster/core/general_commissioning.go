// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// GeneralCommissioning implements the Matter GeneralCommissioning
// cluster (0x0030) per Matter Core Specification 1.5.1 §11.10.
// Mandatory on the Root endpoint.
//
// Lifecycle:
//
//   - During PASE the commissioner calls ArmFailSafe — the bridge
//     enters a fail-safe window during which configuration writes
//     (NOC install, network setup) are reversible if the window
//     expires without a CommissioningComplete.
//   - SetRegulatoryConfig records the country / regulatory locale
//     so the bridge picks the right radio profiles (no-op for an
//     Ethernet-only bridge).
//   - CommissioningComplete locks the configuration in.
//
// go-fabric ships an Ethernet-only bridge so the regulatory
// surface is purely informational; the FailSafe state machine
// reflects what the spec mandates.
type GeneralCommissioning struct {
	// mu guards the fields below (the breadcrumb, the fail-safe and the
	// regulatory values); clock and log are set once.
	mu sync.RWMutex

	// changes are the listeners [GeneralCommissioning.OnAttributeChange]
	// registers; a Breadcrumb another server writes is reported through it.
	changes cluster.AttributeChanges

	clock clock.Clock
	// log is [GeneralCommissioningConfig.Logger]; slog.Default() when nil.
	log *slog.Logger

	breadcrumb                   uint64
	regulatoryConfig             uint8
	locationCapability           uint8
	supportsConcurrentConnection bool

	// FailSafe state.
	failSafeArmed       bool
	failSafeExpiresAt   time.Time
	failSafeFabricIndex uint8
	// failSafeFabricAdded records that SetCurrentFabric re-stamped the
	// armed window with a fabric — AddNOC does, with the fabric it
	// installed (OpcredsConfig.RearmFailSafeForFabric) — so the window's
	// rollback removes failSafeFabricIndex. Reset by every fresh arm.
	failSafeFabricAdded bool
	// failSafeOverPASE records that the armed fail-safe was armed, from
	// the disarmed state, over a PASE session — the commissioner's own,
	// not an administrator's over CASE. Kept by a re-arm, cleared when the
	// fail-safe ends. RevokeCommissioning expires only such a fail-safe
	// ([GeneralCommissioning.ExpireFailSafeArmedOverPASE]).
	failSafeOverPASE bool
	// closed: Close ran; no watcher expires the fail-safe from here on.
	closed bool
	// failSafeWatch is the expiry watcher of the armed window, nil when
	// none runs: one at a time, replaced on a re-arm and stopped on a
	// disarm ([GeneralCommissioning.startFailSafeWatchLocked]).
	failSafeWatch *failSafeWatch
	// expiryRollbacks counts the expiry hooks running outside g.mu over a
	// fail-safe they disarmed; expiryRollbacksDone is closed when the
	// count returns to zero, nil while it is zero. An arm waits for it
	// (general_commissioning_rollback.go).
	expiryRollbacks     int
	expiryRollbacksDone chan struct{}
	// rollbackPaseCloses and rollbackFabricRemovals count the finished
	// expiry hooks by what they ended: every PASE session (the hook ran
	// onFailSafeRolledBack), or the fabric the window's AddNOC added. An
	// arm compares them across its settle to learn whether a rollback it
	// waited for ended its own session (review II, R2-P1-rw-05).
	rollbackPaseCloses     uint64
	rollbackFabricRemovals map[uint8]uint64

	// Cumulative FailSafe tracking: once the cumulative cap is hit the
	// bridge must refuse further ArmFailSafe calls with BusyWithOtherAdmin.
	// Mirrors chip src/app/FailSafeContext.cpp:106-133
	// HandleMaxCumulativeFailSafeTimer: a separate timer starts on the first
	// arm (non-zero ExpiryLengthSeconds) and cannot be reset by re-arms.
	failSafeCumulativeStarted  bool
	failSafeCumulativeDeadline time.Time

	// Static config.
	failSafeMaxSeconds       uint16
	cumulativeFailSafeMaxSec uint16

	// inst is the definition bound to the served features: gencommInst, or
	// gencommInstTC with the terms-and-conditions feature.
	inst *spec.Instance
	// The TermsAndConditions feature (general_commissioning_tc.go): the
	// requirement, the store the committed acceptance lives in, the
	// committed and the tentative acceptance, and whether the armed
	// fail-safe window set one (chip
	// FailSafeContext::UpdateTermsAndConditionsHasBeenInvoked).
	tc          *TermsAndConditionsConfig
	tcSettings  store.Settings
	tcCommitted *termsAcceptance
	tcTemporal  *termsAcceptance
	tcTouched   bool

	// dataVersion tracks the per-cluster monotonic counter per Matter
	// §10.6.5. Bumped after every successful ArmFailSafe, SetRegulatoryConfig,
	// and CommissioningComplete so DataVersionFilter evaluation correctly
	// detects that the cluster changed.
	// Satisfies [contract.ClusterDataVersion].
	// chip's general-commissioning-server uses ember dirty-marking for every
	// attribute write; matter.js behavior layer auto-tracks DataVersion for
	// state mutations. go-fabric mirrors this with an explicit
	// DataVersionTracker (same pattern as OperationalCredentials).
	// Mirrors chip src/app/clusters/general-commissioning-server/
	// general-commissioning-server.cpp MarkAttributeDirty and matter.js
	// packages/node/src/behaviors/general-commissioning/
	// GeneralCommissioningServer.ts state mutation.
	dataVersion cluster.DataVersionTracker

	// Hooks invoked when the fail-safe expires without a
	// CommissioningComplete. Optional.
	onFailSafeExpired func(ctx context.Context, fabricIndex uint8)
	// Hook invoked on a successful CommissioningComplete. chip's
	// `CommissioningWindowManager` revokes the open commissioning window
	// automatically at this point — go-fabric exposes the same hook
	// so the daemon-side wiring can call
	// `CommissioningWindow.RevokeWindow(ctx)`. Otherwise the window
	// stays open for the full duration (typ. 180 s) and a second,
	// unintended fabric can be admitted.
	onCommissioningComplete func(ctx context.Context, fabricIndex uint8)
	// failSafeCommit is CommissioningComplete's durable commit
	// ([GeneralCommissioning.SetFailSafeCommit]); nil commits the terms
	// acceptance only.
	failSafeCommit FailSafeCommitFunc
	// onCommissioned ends the commissioning window after a successful
	// CommissioningComplete; the bridge wires it (bridge.AttachRootClusters
	// / AttachCommissioningWindow), separately from the host's hook.
	onCommissioned func()
	// onFailSafeRolledBack runs after onFailSafeExpired, whenever that
	// would run; the bridge wires it (bridge.AttachRootClusters) to clear
	// the PASE sessions, separately from the host's hook.
	onFailSafeRolledBack func(ctx context.Context)
	// Hook invoked every time ArmFailSafe arms the FailSafe window from
	// the disarmed state — a re-arm extends the fail-safe in force and
	// keeps its state, as matter.js failsafeContext.extend does. The bridge
	// wires this to OperationalCredentials so the pending-NOC /
	// pending-trust-root state is reset for every new commissioning
	// attempt — Matter §11.10.6.2 + matter.js
	// OperationalCredentialsServer.ts model the FailSafeContext as
	// fabricIndex-scoped fresh state per arm. Without this hook Apple's
	// multi-admin "SystemCommissioner" flow (second CSRRequest +
	// AddTrustedRootCertificate + AddNOC on the same CASE session after
	// the iPhone fabric is installed) fails with CONSTRAINT_ERROR on
	// AddTrustedRootCertificate ("NOC command already invoked in this
	// FailSafe window") because the prior AddNOC's flags carry over.
	onFailSafeArmed func(ctx context.Context, fabricIndex uint8)

	// isCommissioningWindowOpen, when non-nil, reports whether an enhanced
	// commissioning window (PASE) is currently open. Used by ArmFailSafe to
	// detect and reject CASE-steal attempts per Matter §11.10.6.2.
	// Mirrors chip GeneralCommissioningCluster.cpp:419-427 which checks
	// IsCommissioningWindowOpen() alongside IsFailSafeArmed() to surface
	// BusyWithOtherAdmin. Wired by the daemon to
	// bridge.CommissioningWindowIsOpen after both clusters are built.
	// When nil the CASE-steal check is skipped (safe for test environments).
	isCommissioningWindowOpen func() bool
}

// RegulatoryLocationTypeEnum values per Matter §11.10.5.2.
const (
	RegulatoryIndoor        uint8 = 0
	RegulatoryOutdoor       uint8 = 1
	RegulatoryIndoorOutdoor uint8 = 2
)

// CommissioningErrorEnum values per Matter §11.10.5.1.
const (
	CommissioningErrorOK                            uint8 = 0
	CommissioningErrorValueOutsideRange             uint8 = 1
	CommissioningErrorInvalidAuthentication         uint8 = 2
	CommissioningErrorNoFailSafe                    uint8 = 3
	CommissioningErrorBusyWithOtherAdmin            uint8 = 4
	CommissioningErrorRequiredTCNotAccepted         uint8 = 5
	CommissioningErrorTCAcknowledgementsNotReceived uint8 = 6
	CommissioningErrorTCMinVersionNotMet            uint8 = 7
)

// Cluster ID, revision, attribute and command ids: the generated
// definition's (cluster/spec/generalcommissioning, ADR 0013, from matter.js
// general-commissioning.element.ts). The lists, the read, write and invoke
// privileges and the statuses of a refused write come from it as well. The
// requests keep the bridge's hand-written decoders into the request
// structs below (notes/parity/by_design.md
// BD-Matter-Commissioning-HandDecoders); the fail-safe is this server's.
const (
	gencommClusterID       = gencommdef.ClusterID
	gencommClusterRevision = gencommdef.Revision

	gencommAttrBreadcrumb                   = gencommdef.AttrBreadcrumb
	gencommAttrBasicCommissioningInfo       = gencommdef.AttrBasicCommissioningInfo
	gencommAttrRegulatoryConfig             = gencommdef.AttrRegulatoryConfig
	gencommAttrLocationCapability           = gencommdef.AttrLocationCapability
	gencommAttrSupportsConcurrentConnection = gencommdef.AttrSupportsConcurrentConnection

	gencommCmdArmFailSafe                   = gencommdef.CmdArmFailSafe
	gencommCmdArmFailSafeResponse           = gencommdef.CmdArmFailSafeResponse
	gencommCmdSetRegulatoryConfig           = gencommdef.CmdSetRegulatoryConfig
	gencommCmdSetRegulatoryConfigResponse   = gencommdef.CmdSetRegulatoryConfigResponse
	gencommCmdCommissioningComplete         = gencommdef.CmdCommissioningComplete
	gencommCmdCommissioningCompleteResponse = gencommdef.CmdCommissioningCompleteResponse
)

// gencommInst is the definition bound to what the server serves: no
// feature (TC, the terms-and-conditions flow, is not advertised, so
// SetTCAcknowledgements and the TC attributes stay out).
var gencommInst = mustInstance(gencommdef.Definition, spec.Options{})

// errGencommInvalidArg is returned for malformed command payloads.
var errGencommInvalidArg = errors.New("core: GeneralCommissioning invalid argument")

// GeneralCommissioningConfig drives [NewGeneralCommissioning].
type GeneralCommissioningConfig struct {
	// LocationCapability is the fixed regulatory capability (Indoor /
	// Outdoor / IndoorOutdoor) the bridge supports.
	LocationCapability uint8
	// SupportsConcurrentConnection reports whether the device can be
	// commissioned over one channel while keeping another active.
	// go-fabric answers "true" — the bridge runs over Ethernet.
	SupportsConcurrentConnection bool
	// FailSafeMaxSeconds bounds how long an ArmFailSafe window stays
	// active. Matter spec floor is 60 s; v1.1 defaults to 900 s
	// (15 min) to keep parity with the pairing-window default in
	// the UI concept.
	FailSafeMaxSeconds uint16
	// CumulativeFailSafeMaxSeconds bounds the *total* time a
	// commissioner may spend across multiple ArmFailSafe calls in
	// one commissioning attempt.
	CumulativeFailSafeMaxSeconds uint16
	// OnFailSafeExpired is called when the fail-safe window times out
	// without a CommissioningComplete. The implementation rolls back
	// commissioning state (NOC install, network config) — the
	// detailed action depends on what the commissioner had written.
	// May be nil for tests or stub setups.
	OnFailSafeExpired func(ctx context.Context, fabricIndex uint8)
	// OnCommissioningComplete fires on a successful
	// CommissioningComplete invoke. The bridge wires this to
	// `CommissioningWindow.RevokeWindow` so a successful pair-completion
	// auto-closes the open commissioning window.
	OnCommissioningComplete func(ctx context.Context, fabricIndex uint8)
	// OnFailSafeArmed fires after every successful ArmFailSafe arm /
	// re-arm. The bridge wires this to OperationalCredentials.
	// ClearPendingState so a fresh commissioning attempt does not see
	// pending NOC / trust-root flags from a prior attempt. May be nil
	// for tests that exercise the FailSafe machinery in isolation.
	OnFailSafeArmed func(ctx context.Context, fabricIndex uint8)
	// IsCommissioningWindowOpen, when non-nil, is queried by ArmFailSafe
	// to detect CASE-steal attempts. When a commissioning window is open
	// (PASE) and an armed fail-safe is owned by a different CASE fabric,
	// ArmFailSafe returns BusyWithOtherAdmin per Matter §11.10.6.2.
	// Wired by the daemon post-construction; nil skips the check.
	IsCommissioningWindowOpen func() bool
	// TermsAndConditions serves the TC feature: the terms a commissioner
	// acknowledges before CommissioningComplete succeeds
	// ([TermsAndConditionsConfig]); nil leaves the feature off.
	TermsAndConditions *TermsAndConditionsConfig
	// Settings keeps the committed terms acceptance across restarts
	// ([store.SettingTermsAcceptance]); nil keeps it in memory. Read by
	// [GeneralCommissioning.RestoreTermsAcceptance].
	Settings store.Settings
	// Clock is the fail-safe's clock: its expiry, the cumulative limit and
	// the expiry watcher. nil is [clock.System].
	Clock clock.Clock
	// Logger receives the server's diagnostics; nil is slog.Default().
	Logger *slog.Logger
}

// NewGeneralCommissioning constructs the cluster.
func NewGeneralCommissioning(cfg GeneralCommissioningConfig) (*GeneralCommissioning, error) {
	if cfg.LocationCapability > RegulatoryIndoorOutdoor {
		return nil, fmt.Errorf("core: GeneralCommissioning LocationCapability=%d", cfg.LocationCapability)
	}
	if cfg.FailSafeMaxSeconds < 900 {
		// Matter §11.10.5.2 ArmFailSafe ExpiryLengthSeconds field default is
		// 900 s per matter.js packages/model/src/standard/elements/
		// general-commissioning.element.ts:66. The spec floor is 60 s; using
		// 900 s (the field default) gives commissioners the full recommended
		// window without requiring them to send an explicit larger value.
		cfg.FailSafeMaxSeconds = 900
	}
	if cfg.CumulativeFailSafeMaxSeconds == 0 {
		// Matter §11.10.5.3: spec-recommended default is 900 s (15 min).
		// matter.js HEAD GeneralCommissioningServer.ts seeds this when
		// the daemon left the field at its zero value.
		cfg.CumulativeFailSafeMaxSeconds = 900
	}
	if cfg.CumulativeFailSafeMaxSeconds < cfg.FailSafeMaxSeconds {
		cfg.CumulativeFailSafeMaxSeconds = cfg.FailSafeMaxSeconds
	}
	inst := gencommInst
	if cfg.TermsAndConditions != nil {
		inst = gencommInstTC
	}
	return &GeneralCommissioning{
		inst:                         inst,
		tc:                           cfg.TermsAndConditions,
		tcSettings:                   cfg.Settings,
		log:                          cmp.Or(cfg.Logger, slog.Default()),
		regulatoryConfig:             cfg.LocationCapability,
		locationCapability:           cfg.LocationCapability,
		supportsConcurrentConnection: cfg.SupportsConcurrentConnection,
		failSafeMaxSeconds:           cfg.FailSafeMaxSeconds,
		clock:                        clock.Or(cfg.Clock),
		cumulativeFailSafeMaxSec:     cfg.CumulativeFailSafeMaxSeconds,
		onFailSafeExpired:            cfg.OnFailSafeExpired,
		onCommissioningComplete:      cfg.OnCommissioningComplete,
		onFailSafeArmed:              cfg.OnFailSafeArmed,
		isCommissioningWindowOpen:    cfg.IsCommissioningWindowOpen,
	}, nil
}

// SetOnFailSafeArmed wires the post-arm hook after construction. Used
// when the OperationalCredentials cluster the hook resets is built
// after GeneralCommissioning (avoids a constructor ordering cycle).
func (g *GeneralCommissioning) SetOnFailSafeArmed(fn func(ctx context.Context, fabricIndex uint8)) {
	g.mu.Lock()
	g.onFailSafeArmed = fn
	g.mu.Unlock()
}

// SetOnFailSafeExpired wires the FailSafe-expiry hook after construction.
// Same ordering motivation as SetOnFailSafeArmed: the OpCreds cluster
// the hook needs to invoke ClearPendingState on is built after
// GeneralCommissioning, so the daemon installs the hook post-build.
//
// Mirrors matter.js packages/node/src/behaviors/operational-credentials/
// OperationalCredentialsServer.ts:#handleFailsafeClosed which fires from
// fabricManager.events.failsafeClosed after the FailSafeContext rolls back.
func (g *GeneralCommissioning) SetOnFailSafeExpired(fn func(ctx context.Context, fabricIndex uint8)) {
	g.mu.Lock()
	g.onFailSafeExpired = fn
	g.mu.Unlock()
}

// SetOnFailSafeRolledBack wires what the owner of the sessions does when
// the fail-safe expires without a CommissioningComplete — its timer, an
// ArmFailSafe with expiry 0, [GeneralCommissioning.ExpireFailSafe]: it runs
// after the host's [GeneralCommissioning.SetOnFailSafeExpired] hook, and
// with or without one. The bridge wires it to clear the PASE sessions
// ([bridge.Bridge.AttachRootClusters]), as matter.js's fail-safe context
// does in its rollback (packages/protocol/src/common/FailsafeContext.ts:277-279
// at db021da0, "1. Terminate any open PASE secure session by clearing any
// associated Secure Session Context at the Server" → closePaseSession); a
// host leaves it alone. ctx is the expiring command's context, or — on the
// timer — the context of the ArmFailSafe that armed the fail-safe, whose
// interaction is over. Pass nil to detach.
func (g *GeneralCommissioning) SetOnFailSafeRolledBack(fn func(ctx context.Context)) {
	g.mu.Lock()
	g.onFailSafeRolledBack = fn
	g.mu.Unlock()
}

// failSafeExpiryHooksLocked returns what runs when the fail-safe expires:
// the host's hook, then the session owner's. Nil when neither is wired.
// Caller holds g.mu and calls the result without it.
func (g *GeneralCommissioning) failSafeExpiryHooksLocked() func(ctx context.Context, fabricIndex uint8) {
	host, rolledBack := g.onFailSafeExpired, g.onFailSafeRolledBack
	if host == nil && rolledBack == nil {
		return nil
	}
	return func(ctx context.Context, fabricIndex uint8) {
		if host != nil {
			host(ctx, fabricIndex)
		}
		if rolledBack != nil {
			rolledBack(ctx)
		}
	}
}

// SetOnCommissioned wires what ends the commissioning window after a
// successful CommissioningComplete — matter.js DeviceCommissioner listens on
// failsafeContext.commissioned and calls endCommissioning
// (DeviceCommissioner.ts:160). The bridge wires it to its
// CommissioningWindow; it fires after the host's OnCommissioningComplete
// hook. Pass nil to detach.
func (g *GeneralCommissioning) SetOnCommissioned(fn func()) {
	g.mu.Lock()
	g.onCommissioned = fn
	g.mu.Unlock()
}

// SetOnCommissioningComplete replaces the commissioning-complete hook after
// construction. It runs after a successful CommissioningComplete has
// committed ([GeneralCommissioning.SetFailSafeCommit]) and disarmed the
// fail-safe. Mirrors chip FailSafeContext::Reset() called from the success
// path in CommissioningWindowManager::OnCommissioningComplete.
func (g *GeneralCommissioning) SetOnCommissioningComplete(fn func(ctx context.Context, fabricIndex uint8)) {
	g.mu.Lock()
	g.onCommissioningComplete = fn
	g.mu.Unlock()
}

// SetIsCommissioningWindowOpen wires the commissioning-window predicate used
// by ArmFailSafe to detect and reject CASE-steal attempts. Pass nil to detach.
// Mirrors chip GeneralCommissioningCluster.cpp:419-427 which checks
// IsCommissioningWindowOpen() + the current fail-safe owner before returning
// BusyWithOtherAdmin on a conflicting ArmFailSafe.
func (g *GeneralCommissioning) SetIsCommissioningWindowOpen(fn func() bool) {
	g.mu.Lock()
	g.isCommissioningWindowOpen = fn
	g.mu.Unlock()
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*GeneralCommissioning)(nil)
	_ contract.ClusterCommandLister           = (*GeneralCommissioning)(nil)
	_ contract.ClusterDataVersion             = (*GeneralCommissioning)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*GeneralCommissioning)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*GeneralCommissioning)(nil)
	_ contract.AttributeChangeNotifier        = (*GeneralCommissioning)(nil)
)

// MatterDataVersion implements [contract.ClusterDataVersion].
// Mirrors chip src/app/clusters/general-commissioning-server/
// general-commissioning-server.cpp MarkAttributeDirty(Attribute::Breadcrumb)
// and matter.js packages/node/src/behaviors/general-commissioning/
// GeneralCommissioningServer.ts state.breadcrumb / state.regulatoryConfig
// mutations that bump the cluster-level DataVersion automatically.
func (g *GeneralCommissioning) MatterDataVersion() uint32 { return g.dataVersion.Current() }

// MatterClusterID implements [contract.ClusterServer].
func (g *GeneralCommissioning) MatterClusterID() uint32 { return gencommClusterID }

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege]:
// the definition's — ArmFailSafe, SetRegulatoryConfig and
// CommissioningComplete require Administer (access "A").
func (g *GeneralCommissioning) MinInvokePrivilege(cmdID uint32) uint8 {
	return g.inst.MinInvokePrivilege(cmdID)
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// the definition's — Breadcrumb requires Administer ("RW VA").
func (g *GeneralCommissioning) MinWritePrivilege(attrID uint32) uint8 {
	return g.inst.MinWritePrivilege(attrID)
}

// BasicCommissioningInfoStruct mirrors Matter §11.10.5.3.
type BasicCommissioningInfoStruct struct {
	FailSafeExpiryLengthSeconds  uint16
	MaxCumulativeFailsafeSeconds uint16
}

// MatterRead implements [contract.ClusterServer].
func (g *GeneralCommissioning) MatterRead(attrID uint32) (any, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	switch attrID {
	case gencommAttrBreadcrumb:
		return g.breadcrumb, true
	case gencommAttrBasicCommissioningInfo:
		return BasicCommissioningInfoStruct{
			FailSafeExpiryLengthSeconds:  g.failSafeMaxSeconds,
			MaxCumulativeFailsafeSeconds: g.cumulativeFailSafeMaxSec,
		}, true
	case gencommAttrRegulatoryConfig:
		return g.regulatoryConfig, true
	case gencommAttrLocationCapability:
		return g.locationCapability, true
	case gencommAttrSupportsConcurrentConnection:
		return g.supportsConcurrentConnection, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return g.inst.ReadGlobal(attrID)
	}
	return g.readTermsLocked(attrID)
}

// MatterWrite handles Breadcrumb writes (Matter §11.10.6.1). Other
// attributes are read-only.
func (g *GeneralCommissioning) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if attrID != gencommAttrBreadcrumb {
		// UNSUPPORTED_WRITE for a served read-only attribute,
		// UNSUPPORTED_ATTRIBUTE otherwise, as the definition judges them.
		_, err := g.inst.ValidateWrite(attrID, value, nil)
		return refusedWrite{sentinel: fmt.Errorf("core: GeneralCommissioning attribute 0x%04X is read-only", attrID), status: err}
	}
	v, ok := value.(uint64)
	if !ok {
		return fmt.Errorf("%w: Breadcrumb expected uint64, got %T", errGencommInvalidArg, value)
	}
	g.mu.Lock()
	changed := g.breadcrumb != v
	g.breadcrumb = v
	if changed {
		// A changed attribute advances the cluster's DataVersion
		// (matter.js Datasource.ts:1456-1457 at db021da0; review II,
		// R2-P1-core-02).
		g.dataVersion.Bump()
	}
	g.mu.Unlock()
	if changed {
		g.changes.NotifyFrom(contract.OriginFrom(ctx), gencommAttrBreadcrumb)
	}
	return nil
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]: a
// Breadcrumb written through [GeneralCommissioning.MatterWrite] is
// reported. That is how another cluster's command moves it — the Wi-Fi
// NetworkCommissioning commands write their Breadcrumb here (chip
// UpdateBreadcrumb, NetworkCommissioningCluster.cpp:589-593 at 6170af84) —
// and the bridge's invoke comparison sees only the invoked cluster; in
// matter.js the commissioning state is one behavior's Datasource, which
// reports every committed change (Datasource.ts:1473-1476). Found by
// TC-CNET-4.4 and 4.9, whose subscription cache missed the Breadcrumb a
// ScanNetworks wrote.
func (g *GeneralCommissioning) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return g.changes.OnAttributeChange(cb)
}

// ArmFailSafeRequest mirrors Matter §11.10.6.2.
type ArmFailSafeRequest struct {
	ExpiryLengthSeconds uint16
	Breadcrumb          uint64
}

// ArmFailSafeResponse mirrors Matter §11.10.6.3.
type ArmFailSafeResponse struct {
	ErrorCode uint8
	DebugText string
}

// SetRegulatoryConfigRequest mirrors Matter §11.10.6.4.
type SetRegulatoryConfigRequest struct {
	NewRegulatoryConfig uint8
	CountryCode         string
	Breadcrumb          uint64
}

// SetRegulatoryConfigResponse mirrors Matter §11.10.6.5.
type SetRegulatoryConfigResponse struct {
	ErrorCode uint8
	DebugText string
}

// CommissioningCompleteResponse mirrors Matter §11.10.6.7.
type CommissioningCompleteResponse struct {
	ErrorCode uint8
	DebugText string
}

// MatterInvoke implements [contract.ClusterServer].
func (g *GeneralCommissioning) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	cmdName := gencommCmdName(cmdID)
	g.logger().Info("matter.gencomm.cmd",
		slog.String("command_name", cmdName),
		slog.String("command", fmt.Sprintf("0x%02X", cmdID)))
	resp, err := g.dispatchCmd(ctx, cmdID, fields)
	if err != nil {
		g.logger().Warn("matter.gencomm.cmd_err",
			slog.String("command_name", cmdName),
			slog.Any("err", err))
	} else {
		g.logger().Info("matter.gencomm.cmd_ok",
			slog.String("command_name", cmdName),
			slog.String("response_type", fmt.Sprintf("%T", resp)))
	}
	return resp, err
}

func (g *GeneralCommissioning) dispatchCmd(ctx context.Context, cmdID uint32, fields any) (any, error) {
	switch cmdID {
	case gencommCmdArmFailSafe:
		return g.handleArmFailSafe(ctx, fields)
	case gencommCmdSetRegulatoryConfig:
		return g.handleSetRegulatoryConfig(fields)
	case gencommCmdCommissioningComplete:
		return g.handleCommissioningComplete(ctx)
	case gencommdef.CmdSetTcAcknowledgements:
		if g.tc != nil {
			return g.handleSetTCAcknowledgements(ctx, fields)
		}
	}
	return nil, im.UnsupportedCommandf("core: GeneralCommissioning command 0x%02X not supported", cmdID)
}

func gencommCmdName(cmdID uint32) string {
	switch cmdID {
	case gencommCmdArmFailSafe:
		return "ArmFailSafe"
	case gencommCmdSetRegulatoryConfig:
		return "SetRegulatoryConfig"
	case gencommCmdCommissioningComplete:
		return "CommissioningComplete"
	default:
		return fmt.Sprintf("Cmd0x%02X", cmdID)
	}
}

// MatterReportable lists the subscribe-able attributes.
func (g *GeneralCommissioning) MatterReportable() []uint32 {
	if g.tc != nil {
		return append([]uint32{gencommAttrBreadcrumb, gencommAttrRegulatoryConfig}, gencommTCAttrs...)
	}
	return []uint32{gencommAttrBreadcrumb, gencommAttrRegulatoryConfig}
}

// MatterAttributes implements [contract.ClusterAttributeLister]
// so wildcard subscribe enumerates the full cluster surface.
func (g *GeneralCommissioning) MatterAttributes() []uint32 { return g.inst.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// ArmFailSafe, SetRegulatoryConfig and CommissioningComplete.
// SetTCAcknowledgements (0x06) joins them with the TC feature
// (general_commissioning_tc.go).
func (g *GeneralCommissioning) MatterAcceptedCommands() []uint32 {
	return g.inst.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]: the
// three responses.
func (g *GeneralCommissioning) MatterGeneratedCommands() []uint32 {
	return g.inst.MatterGeneratedCommands()
}

func (g *GeneralCommissioning) handleArmFailSafe(ctx context.Context, fields any) (any, error) {
	req, ok := fields.(ArmFailSafeRequest)
	if !ok {
		return nil, fmt.Errorf("%w: ArmFailSafeRequest expected, got %T", errGencommInvalidArg, fields)
	}
	g.mu.Lock()
	// The disarm branch unlocks explicitly before firing the revert hook
	// (so the OpCreds rollback never runs under our mutex); this guard
	// keeps the deferred unlock a no-op in that case while every other
	// return path still unlocks normally.
	unlocked := false
	defer func() {
		if !unlocked {
			g.mu.Unlock()
		}
	}()

	// An arm judges the state an expired fail-safe's rollback left, never
	// the state before it (core2 item 1b). A disarm does not wait: it is
	// what a rollback itself may run. An arm whose own session a rollback
	// it settled ended — one it ran or one it waited for — arms nothing
	// (settleArmLocked).
	//
	// sessFabric is the requesting fabric (0 == PASE / commissioning
	// session). It gates the fail-safe ownership rules below.
	_, sessFabric := im.FabricFilterFromContext(ctx)
	if req.ExpiryLengthSeconds != 0 {
		if err := g.settleArmLocked(ctx, sessFabric); err != nil {
			return nil, err
		}
	}

	// (a) A CASE session may not arm the fail-safe while a commissioning
	// window is open for another admin and the fail-safe is not yet armed:
	// the short window is reserved for the PASE commissioner. Mirrors
	// matter.js GeneralCommissioningServer.ts:82-90 (`!isFailsafeArmed &&
	// windowStatus !== WindowNotOpen && !session.isPase` → BusyWithOtherAdmin).
	// sessFabric != 0 identifies a CASE session (a PASE invoke resolves to
	// fabric 0). The window hook is only consulted when wired; test paths
	// without it fall through (no window means no reservation to protect).
	if !g.failSafeArmed && sessFabric != 0 && g.isCommissioningWindowOpen != nil && g.isCommissioningWindowOpen() {
		return ArmFailSafeResponse{
			ErrorCode: CommissioningErrorBusyWithOtherAdmin,
			DebugText: "cannot arm fail-safe over CASE while a commissioning window is open",
		}, nil
	}

	// (b)+(c) Ownership: once the fail-safe is armed by a fabric, only that
	// same fabric may re-arm OR disarm it — regardless of window state. A
	// different fabric attempting either leaves the fail-safe unchanged and
	// is rejected with BusyWithOtherAdmin. This stops fabric B from
	// disarming fabric A's fail-safe mid-commissioning (which would roll
	// back A's pending NOC) or hijacking A's arm. Mirrors matter.js
	// FailsafeTimer.reArm (FailsafeTimer.ts:53-57): a fabricIndex mismatch
	// throws, which GeneralCommissioningServer maps to BusyWithOtherAdmin.
	if g.failSafeArmed && g.failSafeFabricIndex != sessFabric {
		return ArmFailSafeResponse{
			ErrorCode: CommissioningErrorBusyWithOtherAdmin,
			DebugText: fmt.Sprintf("fail-safe owned by fabric %d; requesting fabric %d rejected", g.failSafeFabricIndex, sessFabric),
		}, nil
	}

	if req.ExpiryLengthSeconds == 0 {
		// Spec: ExpiryLengthSeconds=0 disarms the fail-safe and resets the
		// cumulative cap so the next commissioning attempt starts fresh.
		// Mirrors chip FailSafeContext::Disarm() which stops both the
		// single-arm timer and the cumulative timer.
		//
		// A disarm before CommissioningComplete must run the SAME revert
		// path the timeout fires: chip's GeneralCommissioningCluster.cpp:429-432
		// (ForceFailSafeTimerExpiry) routes into FailSafeContext.cpp:66-76,
		// which performs the full cleanup (RevertPendingOpCerts + drop the
		// pending fabric). Without firing onFailSafeExpired here a disarm
		// would leak a half-installed NOC: AddNOC's pending fabric is never
		// rolled back, so a re-pair attempt collides with the orphaned entry.
		wasArmed := g.failSafeArmed
		expiredFabric := g.failSafeFabricIndex
		g.failSafeArmed = false
		g.failSafeOverPASE = false
		g.stopFailSafeWatchLocked()
		g.failSafeFabricIndex = 0
		g.failSafeCumulativeStarted = false
		// The request's Breadcrumb is not taken: an armed fail-safe expires,
		// and its expiry resets the Breadcrumb to 0; a fail-safe not armed
		// is left without side effect. Mirrors matter.js
		// packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:100-104
		// (`if (expiryLengthSeconds === 0) return SuccessResponse` when not
		// armed) and :92-97 → packages/protocol/src/common/FailsafeTimer.ts:61-65
		// (an armed fail-safe expires) → FailsafeContext.ts:314
		// restoreBreadcrumb → ServerNodeFailsafeContext.ts:84-91 (breadcrumb
		// 0); chip GeneralCommissioningCluster.cpp:429-435 ("Don't set the
		// breadcrumb, since expiring the failsafe should reset it anyway").
		if wasArmed {
			g.breadcrumb = 0
		}
		// A tentative terms acceptance of the window goes with it
		// (general_commissioning_tc.go revertTermsLocked).
		termsReverted := wasArmed && g.revertTermsLocked()
		// Bump DataVersion on disarm so Apple's MTRDevice sees a version
		// change and invalidates its cached DataVersion=0.
		// Mirrors chip MarkAttributeDirty(Breadcrumb).
		g.dataVersion.Bump()
		// Release the cluster lock before firing the hook so
		// OpCreds.OnFailSafeExpiry (which acquires its own lock to perform
		// the AddNOC rollback) runs without our mutex held — matching the
		// timeout path in watchFailSafeExpiry.
		var hook func(ctx context.Context, fabricIndex uint8)
		if wasArmed {
			hook = g.trackRollbackLocked(g.failSafeExpiryHooksLocked(), expiredFabric)
		}
		g.mu.Unlock()
		unlocked = true
		if termsReverted {
			g.changes.Notify(gencommTCAttrs...)
		}
		if hook != nil {
			hook(ctx, expiredFabric)
		}
		return ArmFailSafeResponse{ErrorCode: CommissioningErrorOK}, nil
	}
	// ExpiryLengthSeconds is not bounded by FailSafeMaxSeconds: matter.js
	// arms or extends with the value as sent
	// (GeneralCommissioningServer.ts:89-106 at 862ad75f) and chip arms it
	// as given (GeneralCommissioningCluster.cpp:439-441 at 6170af84,
	// FailSafeContext.cpp:112-119); both end the window at the cumulative
	// limit's own timer, as the clamp below does. The ValueOutsideRange
	// answer the server used to give is no reference's (review 2026-11
	// W3-clusec-05).
	// Cumulative-cap enforcement: once the total commissioning time across
	// all re-arms exceeds MaxCumulativeFailsafeSeconds the bridge must
	// refuse further arms. Mirrors chip src/app/FailSafeContext.cpp:106-133
	// HandleMaxCumulativeFailSafeTimer — the cumulative timer starts on the
	// first arm and runs independently; re-arms cannot reset it.
	now := g.clock.Now()
	if !g.failSafeCumulativeStarted {
		g.failSafeCumulativeStarted = true
		g.failSafeCumulativeDeadline = now.Add(time.Duration(g.cumulativeFailSafeMaxSec) * time.Second)
	} else if now.After(g.failSafeCumulativeDeadline) {
		// Cumulative cap exceeded: return BusyWithOtherAdmin per Matter §11.10.6.2.
		// chip returns FAILSAFE_REQUIRED_ERROR via StatusCode; matter.js
		// GeneralCommissioningServer.ts maps this to
		// CommissioningErrorBusyWithOtherAdmin (4).
		return ArmFailSafeResponse{
			ErrorCode: CommissioningErrorBusyWithOtherAdmin,
			DebugText: fmt.Sprintf("cumulative fail-safe cap of %d s exceeded", g.cumulativeFailSafeMaxSec),
		}, nil
	}
	// A re-arm extends the fail-safe in force; only an arm from the
	// disarmed state starts a new fail-safe context. matter.js
	// GeneralCommissioningServer #armFailSafe: failsafeContext.extend(...)
	// when isFailsafeArmed, a new ServerNodeFailsafeContext otherwise — so
	// a trusted root or NOC added under the fail-safe survives the re-arm
	// (TC-CGEN-2.2 step 34).
	extending := g.failSafeArmed
	g.failSafeFabricAdded = g.failSafeFabricAdded && extending
	g.failSafeOverPASE = extending && g.failSafeOverPASE || !extending && (sessFabric == 0 || im.IsPASEFromContext(ctx))
	g.failSafeArmed = true
	g.failSafeExpiresAt = now.Add(time.Duration(req.ExpiryLengthSeconds) * time.Second)
	// The cumulative limit is a timer of its own that expires the fail-safe,
	// so no re-arm carries the deadline past it. Mirrors matter.js
	// packages/protocol/src/common/FailsafeTimer.ts:33-35 (the max cumulative
	// timer, started with the context, calls expire()).
	if g.failSafeExpiresAt.After(g.failSafeCumulativeDeadline) {
		g.failSafeExpiresAt = g.failSafeCumulativeDeadline
	}
	g.breadcrumb = req.Breadcrumb
	// Capture the requesting fabric so CommissioningComplete can verify
	// that the *same* CASE fabric closes the window the PASE/CASE
	// session opened. PASE sessions surface as fabric==0, which is OK
	// here — only Complete is fabric-gated.
	g.failSafeFabricIndex = sessFabric
	// Bump DataVersion on successful arm so Apple's MTRDevice sees the cluster
	// changed and does not skip it with DataVersionFilter=0.
	// Mirrors chip src/app/clusters/general-commissioning-server/
	// general-commissioning-server.cpp MarkAttributeDirty.
	g.dataVersion.Bump()

	g.startFailSafeWatchLocked(ctx, g.failSafeFabricIndex, g.failSafeExpiresAt)
	// Capture the hook + fabric before releasing the cluster lock so the
	// callback runs without holding our mutex (OpCreds.ClearPendingState
	// acquires its own lock).
	armedHook := g.onFailSafeArmed
	armedFabric := g.failSafeFabricIndex
	// Release the cluster lock now, as the comment above promises: the
	// deferred unlock would otherwise hold it across the hook, and a hook
	// that reads this cluster (or arms through the bridge) would deadlock.
	g.mu.Unlock()
	unlocked = true
	if armedHook != nil && !extending {
		// Mirrors matter.js GeneralCommissioningServer.ts:90-100 at
		// 862ad75f: an ArmFailSafe while armed extends the fail-safe
		// context, one while disarmed creates a new
		// ServerNodeFailsafeContext (the hook runs only then), surfacing a
		// fresh `rootCertSet` / `fabricIndex` set so multi-admin pairing
		// (Apple's SystemCommissioner adds the iCloud-Heim fabric after
		// the iPhone fabric is installed) can run a clean CSRRequest +
		// AddTrustedRootCertificate + AddNOC sequence on the same
		// CASE session.
		armedHook(ctx, armedFabric)
	}
	return ArmFailSafeResponse{ErrorCode: CommissioningErrorOK}, nil
}

// failSafeWatch is one expiry watcher: its timer and the channel that
// ends it early.
type failSafeWatch struct {
	timer clock.Timer
	stop  chan struct{}
	// fabricIndex is what its expiry hook is called with.
	fabricIndex uint8
}

// startFailSafeWatchLocked replaces the expiry watcher by one for a
// window that ends at expiresAt. It runs with or without an expiry hook:
// the expiry has effects of the cluster's own — the window ends, the
// Breadcrumb resets, a tentative terms acceptance reverts — which the
// command paths judge by the armed flag the watcher clears (review II,
// R2-P1-core-05). One
// watcher serves the window: before, every arm and re-arm started a
// goroutine that slept to its own deadline, so a commissioner that
// re-armed every few seconds left one parked per re-arm until that
// deadline (review II, W2-conc-15). Caller holds g.mu.
func (g *GeneralCommissioning) startFailSafeWatchLocked(ctx context.Context, fabricIndex uint8, expiresAt time.Time) {
	g.stopFailSafeWatchLocked()
	w := &failSafeWatch{
		timer:       g.clock.NewTimer(clock.Until(g.clock, expiresAt)),
		stop:        make(chan struct{}),
		fabricIndex: fabricIndex,
	}
	g.failSafeWatch = w
	go g.watchFailSafeExpiry(ctx, fabricIndex, w)
}

// Close ends the fail-safe's expiry watcher and waits for every expiry
// rollback in flight — OperationalCredentials' rollback, the participants',
// the PASE close — until it returns or ctx ends, which is the error; from
// then on no deadline expires the fail-safe. The node calls it when it
// stops, before its store goes (review II, R2-P2-stop-06): a rollback that
// began before the stop no longer writes after it. What stays uncommitted
// is rolled back by the next start. It may be called again after an error.
func (g *GeneralCommissioning) Close(ctx context.Context) error {
	g.mu.Lock()
	g.closed = true
	g.stopFailSafeWatchLocked()
	for {
		done := g.expiryRollbacksDone
		g.mu.Unlock()
		if done == nil {
			return nil
		}
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("core: GeneralCommissioning: wait for the fail-safe's rollback: %w", ctx.Err())
		}
		g.mu.Lock()
	}
}

// stopFailSafeWatchLocked ends the expiry watcher, if one runs: the
// window it watched was re-armed or disarmed. Caller holds g.mu.
func (g *GeneralCommissioning) stopFailSafeWatchLocked() {
	w := g.failSafeWatch
	if w == nil {
		return
	}
	g.failSafeWatch = nil
	w.timer.Stop()
	close(w.stop)
}

// watchFailSafeExpiry blocks until w's timer fires (or w is stopped, or
// ctx is cancelled) and fires onFailSafeExpired iff the fail-safe is
// still armed and due at that moment. Runs in the one goroutine
// [GeneralCommissioning.startFailSafeWatchLocked] starts per window.
func (g *GeneralCommissioning) watchFailSafeExpiry(ctx context.Context, fabricIndex uint8, w *failSafeWatch) {
	defer w.timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-w.stop:
		return
	case <-w.timer.C():
	}
	g.mu.Lock()
	if g.failSafeWatch == w {
		g.failSafeWatch = nil
	}
	if g.closed || !g.failSafeArmed || g.failSafeExpiresAt.After(g.clock.Now()) {
		g.mu.Unlock()
		return
	}
	after := g.expireLocked(fabricIndex)
	g.mu.Unlock()
	after(ctx)
}

// expireLocked ends the armed fail-safe as its deadline does, and returns
// what then runs without g.mu: the change notifications and the expiry
// hook, recorded as a rollback in flight until it returns. Caller holds
// g.mu.
func (g *GeneralCommissioning) expireLocked(fabricIndex uint8) func(ctx context.Context) {
	hadBreadcrumb := g.breadcrumb != 0
	g.failSafeArmed = false
	g.failSafeOverPASE = false
	g.stopFailSafeWatchLocked()
	// chip's GeneralCommissioningCluster.cpp:146 (kFailSafeTimerExpired
	// branch) clears the Breadcrumb when the fail-safe expires —
	// Matter §11.10.6.1 "Breadcrumb SHALL be set to 0 on FailSafe
	// expiry". Apple's HomeKitDaemon reads Breadcrumb post-failsafe
	// and a stale non-zero value can confuse the commissioning-recovery
	// flow.
	g.breadcrumb = 0
	// Reset the cumulative cap so the next commissioning attempt
	// starts fresh. Mirrors chip FailSafeContext::HandleMaxCumulativeFailSafeTimer
	// which stops when the fail-safe expires.
	g.failSafeCumulativeStarted = false
	termsReverted := g.revertTermsLocked()
	// As the disarm, ExpireFailSafe and ArmFailSafeFor(0) paths do:
	// the expiry changed the cluster (review II, R2-P1-core-02).
	g.dataVersion.Bump()
	hook := g.trackRollbackLocked(g.failSafeExpiryHooksLocked(), g.failSafeFabricIndex)
	return func(ctx context.Context) {
		if termsReverted {
			g.changes.Notify(gencommTCAttrs...)
		}
		// The reset is reported like any other change of the attribute:
		// chip's kFailSafeTimerExpired branch calls SetBreadCrumb(0)
		// (GeneralCommissioningCluster.cpp:145-146 at 6170af84), which
		// notifies when the value moved (:377-381); no command of this
		// cluster runs here, so the bridge's invoke comparison cannot
		// see it.
		if hadBreadcrumb {
			g.changes.Notify(gencommAttrBreadcrumb)
		}
		if hook != nil {
			hook(ctx, fabricIndex)
		}
	}
}

func (g *GeneralCommissioning) handleSetRegulatoryConfig(fields any) (any, error) {
	req, ok := fields.(SetRegulatoryConfigRequest)
	if !ok {
		return nil, fmt.Errorf("%w: SetRegulatoryConfigRequest expected, got %T", errGencommInvalidArg, fields)
	}
	if req.NewRegulatoryConfig > RegulatoryIndoorOutdoor {
		return SetRegulatoryConfigResponse{
			ErrorCode: CommissioningErrorValueOutsideRange,
			DebugText: fmt.Sprintf("NewRegulatoryConfig=%d", req.NewRegulatoryConfig),
		}, nil
	}
	if req.CountryCode != "" && len(req.CountryCode) != 2 {
		return SetRegulatoryConfigResponse{
			ErrorCode: CommissioningErrorValueOutsideRange,
			DebugText: fmt.Sprintf("CountryCode length=%d (want 0 or 2)", len(req.CountryCode)),
		}, nil
	}
	// The new configuration must be one the LocationCapability allows:
	// Indoor or Outdoor alone, or any of the three for IndoorOutdoor.
	// Mirrors matter.js GeneralCommissioningServer.setRegulatoryConfig
	// (validValues per locationCapability → ValueOutsideRange);
	// TC-CGEN-2.4 step 19.
	g.mu.RLock()
	capability := g.locationCapability
	g.mu.RUnlock()
	if capability != RegulatoryIndoorOutdoor && req.NewRegulatoryConfig != capability {
		return SetRegulatoryConfigResponse{
			ErrorCode: CommissioningErrorValueOutsideRange,
			DebugText: fmt.Sprintf("NewRegulatoryConfig=%d not allowed by LocationCapability=%d", req.NewRegulatoryConfig, capability),
		}, nil
	}
	g.mu.Lock()
	g.regulatoryConfig = req.NewRegulatoryConfig
	g.breadcrumb = req.Breadcrumb
	// Bump DataVersion on successful config change.
	// Mirrors chip MarkAttributeDirty(RegulatoryConfig) and matter.js
	// GeneralCommissioningServer.ts state.regulatoryConfig mutation.
	g.dataVersion.Bump()
	g.mu.Unlock()
	return SetRegulatoryConfigResponse{ErrorCode: CommissioningErrorOK}, nil
}

func (g *GeneralCommissioning) handleCommissioningComplete(ctx context.Context) (any, error) {
	// The durable commit holds off what it must not interleave with — the
	// NOC commands, RemoveFabric, the fail-safe's rollback — before this
	// cluster's lock is taken, so the lock order is the commit's, then
	// g.mu (OperationalCredentials.CommitFailSafe).
	//
	// A window past its deadline whose watcher has not run yet is expired
	// first, its rollback run by this command, as an arm does
	// (settleExpiryLocked): CommissioningComplete judges the fail-safe as
	// FailSafeFabric reports it, armed and not due (review II,
	// R2-P2-comm-09). The settle runs before the commit's hold, which the
	// rollback takes itself.
	g.mu.Lock()
	if _, err := g.settleExpiryLocked(ctx); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	commitFn := g.failSafeCommit
	g.mu.Unlock()
	var (
		resp  any
		err   error
		hooks func()
	)
	complete := func(commit func(ctx context.Context) error) {
		resp, hooks, err = g.completeCommissioning(ctx, commit)
	}
	if commitFn != nil {
		commitFn(ctx, complete)
	} else {
		complete(nil)
	}
	if hooks != nil {
		hooks()
	}
	return resp, err
}

// completeCommissioning is CommissioningComplete under the durable
// commit's hold: the checks, then every store write in an order where the
// commit of the fabric row is the last (review II, R2-P2-comm-07):
//
//  1. a tentative terms acceptance is persisted — a failure answers
//     FAILURE and has written nothing;
//  2. commit — the Wi-Fi network list (R2-P2-store-01), then the fabric
//     row — a failure has undone its own writes; the terms acceptance is
//     written back to the committed one, and the command answers FAILURE;
//
// on a FAILURE the fail-safe stays armed and nothing in memory moved, so
// its end rolls the window back as if CommissioningComplete never came. A
// write-back that fails too is logged: the store then holds the window's
// acceptance while the fail-safe's end reverts it in memory — what a crash
// between the writes leaves as well. On success the acceptance is
// committed in memory and the fail-safe disarmed; hooks is what runs
// after the commit's hold and this cluster's lock are released.
func (g *GeneralCommissioning) completeCommissioning(ctx context.Context, commit func(ctx context.Context) error) (resp any, hooks func(), err error) {
	// Mirrors matter.js packages/node/src/behaviors/general-commissioning/
	// GeneralCommissioningServer.ts:216-265:
	//   1. Reject PASE (and Group) sessions with InvalidAuthentication.
	//   2. Reject when no fail-safe is armed → NoFailSafe.
	//   3. Reject when the requesting CASE fabric does not match the
	//      fabric that armed the fail-safe → InvalidAuthentication.
	//   4. On success: clear fail-safe state, reset Breadcrumb to 0
	//      (D-56), and report OK.
	//
	// The bridge stamps the active session's FabricIndex into the
	// invoke context via `WithFabricFilter`; FabricIndex==0 signals a
	// PASE session (or unsecured channel) — Apple Home, Google Home,
	// and chip-tool always send CommissioningComplete over CASE, so
	// the PASE branch is purely a defence-in-depth.
	_, sessFabric := im.FabricFilterFromContext(ctx)
	g.mu.Lock()
	// A PASE session adopted onto the fabric AddNOC installed has a
	// fabric and is still PASE (TC-CGEN-2.4 step kSendNOC).
	if sessFabric == 0 || im.IsPASEFromContext(ctx) {
		g.mu.Unlock()
		return CommissioningCompleteResponse{
			ErrorCode: CommissioningErrorInvalidAuthentication,
			DebugText: "Command must be executed over CASE session.",
		}, nil, nil
	}
	// Armed and not due: a deadline that passed between the settle and
	// this hold is not committed either; the watcher rolls it back.
	if !g.failSafeArmed || !g.failSafeExpiresAt.After(g.clock.Now()) {
		g.mu.Unlock()
		return CommissioningCompleteResponse{
			ErrorCode: CommissioningErrorNoFailSafe,
			DebugText: "fail-safe not armed",
		}, nil, nil
	}
	// Plain equality, as both references have it — chip
	// FailSafeContext.h:92-96 MatchesFabricIndex, matter.js
	// GeneralCommissioningServer.ts:242-247, where an undefined associated
	// fabric never matches.
	//
	// This used to read `g.failSafeFabricIndex != 0 && ...`, tolerating a
	// window armed over PASE (which stamps fabric 0) so that a first pairing
	// could complete over CASE. The cost was that ANY already-authorised
	// fabric could complete a window it did not open — aborting a peer
	// admin's commissioning and committing half-installed state without the
	// expiry rollback ever running.
	//
	// The references avoid that tolerance because AddNOC re-stamps the
	// context onto the fabric it installed. This module does that now too
	// (OpcredsConfig.RearmFailSafeForFabric), so equality is correct here and
	// a first pairing still completes: by the time CommissioningComplete
	// arrives, the window belongs to the fabric AddNOC created.
	if sessFabric != g.failSafeFabricIndex {
		g.mu.Unlock()
		return CommissioningCompleteResponse{
			ErrorCode: CommissioningErrorInvalidAuthentication,
			DebugText: fmt.Sprintf("session fabric %d != failsafe fabric %d", sessFabric, g.failSafeFabricIndex),
		}, nil, nil
	}
	// The terms-and-conditions gate, then the commit of a tentative
	// acceptance (chip GeneralCommissioningCluster.cpp:476-525 at 6170af84;
	// general_commissioning_tc.go).
	if code := g.termsGateLocked(); code != CommissioningErrorOK {
		g.mu.Unlock()
		return CommissioningCompleteResponse{ErrorCode: code, DebugText: "terms and conditions not accepted"}, nil, nil
	}
	termsWritten := false
	if g.tcTouched && g.tcTemporal != nil {
		if err := g.persistTermsLocked(ctx, g.tcTemporal); err != nil {
			// The window's flag stays: the acceptance is still tentative,
			// and the fail-safe's expiry reverts it (chip keeps
			// UpdateTermsAndConditionsHasBeenInvoked on a failed commit,
			// GeneralCommissioningCluster.cpp:512-526).
			g.mu.Unlock()
			return nil, nil, err
		}
		termsWritten = true
	}
	if commit != nil {
		if err := commit(ctx); err != nil {
			if termsWritten {
				if werr := g.persistTermsLocked(context.WithoutCancel(ctx), g.tcCommitted); werr != nil {
					g.logger().Error("matter.gencomm.terms.write_back_failed", slog.Any("err", werr))
				}
			}
			g.mu.Unlock()
			return nil, nil, fmt.Errorf("core: GeneralCommissioning: commit the commissioning: %w", err)
		}
	}
	if g.tcTouched {
		g.tcCommitted, g.tcTemporal, g.tcTouched = g.tcTemporal, nil, false
	}
	g.failSafeArmed = false
	g.failSafeOverPASE = false
	g.stopFailSafeWatchLocked()
	g.failSafeFabricIndex = 0
	g.breadcrumb = 0
	// Reset the cumulative cap on successful CommissioningComplete so the
	// next pair attempt starts with a fresh cumulative window.
	g.failSafeCumulativeStarted = false
	// Bump DataVersion on commissioning complete so subscribers see the
	// cluster-state flip. Mirrors chip MarkAttributeDirty(Breadcrumb) on the
	// success path and matter.js GeneralCommissioningServer.ts state mutation
	// on CommissioningComplete.
	g.dataVersion.Bump()
	hook := g.onCommissioningComplete
	commissioned := g.onCommissioned
	// The hooks run without the lock — they may take arbitrary locks
	// (commissioning window mutex, mDNS unpublish, …) — and after the
	// commit's hold is released.
	g.mu.Unlock()
	return CommissioningCompleteResponse{ErrorCode: CommissioningErrorOK}, func() {
		if hook != nil {
			// chip's CommissioningWindowManager auto-closes the open
			// enhanced-commissioning window on a successful
			// CommissioningComplete (Matter §11.20.6.1). Wire the same
			// behaviour via the daemon-supplied hook so the window does
			// not stay open until its full timeout (typ. 180s) and admit
			// a second, unintended fabric.
			hook(ctx, sessFabric)
		}
		if commissioned != nil {
			commissioned()
		}
	}, nil
}

// ArmFailSafeFor arms the fail-safe for seconds on behalf of fabricIndex
// (0: a PASE session), outside any ArmFailSafe command; seconds 0 disarms
// it without its rollback. Opening a commissioning window no longer calls
// it (owner decision 47).
func (g *GeneralCommissioning) ArmFailSafeFor(ctx context.Context, seconds uint32, fabricIndex uint8) error {
	return g.armFailSafeFor(ctx, seconds, fabricIndex, false)
}

// armFailSafeFor is ArmFailSafeFor; forSession says the arm is made for a
// session that exists now (the PASE auto-arm), whose fabricIndex the
// rollback of a due window, or one in flight it waits for, may end — then
// nothing is armed. An arm for a
// session to come (OpenCommissioningWindow's) arms either way.
func (g *GeneralCommissioning) armFailSafeFor(ctx context.Context, seconds uint32, fabricIndex uint8, forSession bool) error {
	if seconds == 0 {
		// A zero-second arm is a DISARM request, a pure state reset: it
		// does NOT run the arm logic below (now+0 is immediately expired,
		// so watchFailSafeExpiry would fire onFailSafeExpired at once) and
		// does NOT fire onFailSafeExpired — a caller inside that hook would
		// otherwise re-enter it without end. The wire
		// ArmFailSafe(ExpiryLengthSeconds=0) in handleArmFailSafe and
		// ExpireFailSafe are the disarms that roll the window back.
		g.mu.Lock()
		g.failSafeArmed = false
		g.failSafeOverPASE = false
		g.stopFailSafeWatchLocked()
		g.failSafeFabricIndex = 0
		g.failSafeCumulativeStarted = false
		// Matter §11.10.6.1: Breadcrumb SHALL be 0 once the fail-safe is no
		// longer armed. Bump DataVersion so Apple's MTRDevice invalidates its
		// cached snapshot, matching the wire-disarm and expiry paths.
		g.breadcrumb = 0
		termsReverted := g.revertTermsLocked()
		g.dataVersion.Bump()
		g.mu.Unlock()
		if termsReverted {
			g.changes.Notify(gencommTCAttrs...)
		}
		return nil
	}
	req := ArmFailSafeRequest{
		ExpiryLengthSeconds: uint16(seconds & 0xFFFF), // window timeout ≤ 900 s, fits uint16
		Breadcrumb:          0,                        // window open does not set Breadcrumb
	}
	// Temporarily inject the fabric index so handleArmFailSafe records it.
	// handleArmFailSafe re-derives it from ctx via FabricFilterFromContext;
	// override the context's fabric filter if needed, or set directly.
	g.mu.Lock()
	// An arm judges the state an expired fail-safe's rollback left: a due
	// window is expired first, a rollback in flight waited for (core2
	// item 1b). So a window armed now is not due, and fresh is "disarmed".
	var err error
	if forSession {
		err = g.settleArmLocked(ctx, fabricIndex)
	} else {
		_, err = g.settleExpiryLocked(ctx)
	}
	if err != nil {
		g.mu.Unlock()
		return err
	}
	// An arm for a session leaves a fail-safe another fabric armed alone,
	// in the same lock hold as its check (matter.js extend refuses another
	// fabric, FailsafeTimer.ts:53-57 at db021da0; review II,
	// R2-P2-comm-08).
	if forSession && g.failSafeArmed && g.failSafeFabricIndex != fabricIndex {
		g.mu.Unlock()
		return nil
	}
	fresh := !g.failSafeArmed
	g.failSafeFabricAdded = g.failSafeFabricAdded && !fresh
	if fresh {
		g.failSafeOverPASE = fabricIndex == 0
	}
	g.failSafeArmed = true
	g.failSafeExpiresAt = g.clock.Now().Add(time.Duration(seconds) * time.Second)
	// A re-arm stays within the cumulative limit, as the wire arm does.
	if !fresh && g.failSafeCumulativeStarted && g.failSafeExpiresAt.After(g.failSafeCumulativeDeadline) {
		g.failSafeExpiresAt = g.failSafeCumulativeDeadline
	}
	g.breadcrumb = req.Breadcrumb
	g.failSafeFabricIndex = fabricIndex
	g.dataVersion.Bump()
	g.startFailSafeWatchLocked(ctx, fabricIndex, g.failSafeExpiresAt)
	armedHook := g.onFailSafeArmed
	g.mu.Unlock()
	// An arm from the disarmed state starts a new fail-safe context — the
	// PASE auto-arm included, as matter.js arms a new
	// ServerNodeFailsafeContext when the PASE session is established — so
	// nothing an aborted earlier attempt left pending carries over
	// (TC-CGEN-2.4).
	if fresh && armedHook != nil {
		armedHook(ctx, fabricIndex)
	}
	return nil
}

// ExpireFailSafe expires an armed fail-safe at once, with the cleanup its
// timeout runs (onFailSafeExpired: the pending NOC and fabric are rolled
// back), and does nothing when none is armed. RevokeCommissioning uses it
// — Matter §11.19.8.3 step 1 expires the fail-safe, which matter.js
// AdministratorCommissioningServer.revokeCommissioning does with
// failsafeContext.close(), rolling back what the commissioning installed.
// Without the rollback a fabric an aborted commissioning had added over
// PASE survived the revoke and the next attempt failed FabricConflict
// (TC-CGEN-2.4). The fail-safe is disarmed before the hook runs, so a hook
// that revokes the window again finds nothing to expire.
func (g *GeneralCommissioning) ExpireFailSafe(ctx context.Context) {
	g.mu.Lock()
	if !g.failSafeArmed {
		g.mu.Unlock()
		return
	}
	g.expireNowLocked(ctx)
}

// ExpireFailSafeArmedOverPASE is [GeneralCommissioning.ExpireFailSafe] for
// a fail-safe armed over a PASE session only — the commissioner's own,
// whether or not its AddNOC re-stamped it since; one an administrator
// armed over CASE is left alone. RevokeCommissioning uses it (Matter
// §11.19.8.3 step 1). This is the owner's rule (decision 42), a deliberate
// divergence: matter.js closes whatever fail-safe context is open
// (AdministratorCommissioningServer.ts:143-148 at db021da0, under a
// comment that speaks of the "PASE-held fail-safe"), and connectedhomeip's
// ExpireFailSafeIfHeldByOpenPASESession
// (src/app/clusters/administrator-commissioning-server/AdministratorCommissioningLogic.cpp:113)
// expires whatever fail-safe is armed while a PASE session exists
// (src/app/server/CommissioningWindowManager.cpp:650-657 at 6170af84).
func (g *GeneralCommissioning) ExpireFailSafeArmedOverPASE(ctx context.Context) {
	g.mu.Lock()
	if !g.failSafeArmed || !g.failSafeOverPASE {
		g.mu.Unlock()
		return
	}
	g.expireNowLocked(ctx)
}

// expireNowLocked expires the armed fail-safe at once and runs its
// rollback; g.mu is held and released.
func (g *GeneralCommissioning) expireNowLocked(ctx context.Context) {
	fabric := g.failSafeFabricIndex
	g.failSafeArmed = false
	g.failSafeOverPASE = false
	g.stopFailSafeWatchLocked()
	g.failSafeFabricIndex = 0
	g.failSafeCumulativeStarted = false
	g.breadcrumb = 0
	termsReverted := g.revertTermsLocked()
	g.dataVersion.Bump()
	hook := g.trackRollbackLocked(g.failSafeExpiryHooksLocked(), fabric)
	g.mu.Unlock()
	if termsReverted {
		g.changes.Notify(gencommTCAttrs...)
	}
	if hook != nil {
		hook(ctx, fabric)
	}
}

// SetCurrentFabric records the fabric the active commissioning
// session belongs to. The bridge calls this from the IM dispatcher
// before invoking ArmFailSafe so the expiry hook knows which fabric
// to roll back.
func (g *GeneralCommissioning) SetCurrentFabric(idx uint8) {
	g.mu.Lock()
	g.failSafeFabricIndex = idx
	g.failSafeFabricAdded = g.failSafeArmed && idx != 0
	g.mu.Unlock()
}

// FailSafeArmed reports whether a fail-safe window is currently
// active. Used by the OperationalCredentials cluster (Stufe 4) to
// gate AddNOC / AddTrustedRootCertificate.
func (g *GeneralCommissioning) FailSafeArmed() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.failSafeArmed && g.failSafeExpiresAt.After(g.clock.Now())
}

// FailSafeFabric reports whether a fail-safe window is active and, when
// it is, the fabric it is armed for — 0 for a window armed over PASE
// before AddNOC re-stamped it. It is the node's fail-safe port: a server
// answers connectedhomeip's IsFailSafeArmed(accessingFabricIndex) with
// [FailSafeArmedFor] over it (src/app/FailSafeContext.h:76-79 at
// 6170af84), and a check that needs the armed state alone reads armed.
func (g *GeneralCommissioning) FailSafeFabric() (fabricIndex uint8, armed bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if !g.failSafeArmed || !g.failSafeExpiresAt.After(g.clock.Now()) {
		return 0, false
	}
	return g.failSafeFabricIndex, true
}

// FailSafeArmedFor is connectedhomeip's
// FailSafeContext::IsFailSafeArmed(accessingFabricIndex)
// (src/app/FailSafeContext.h:76-79, :92-96 at 6170af84) over a fail-safe
// port: armed, and armed for the accessing fabric (0 is a PASE session
// before AddNOC, which matches a window armed over PASE). A nil port is
// disarmed.
func FailSafeArmedFor(failSafe func() (fabricIndex uint8, armed bool), accessingFabricIndex uint8) bool {
	if failSafe == nil {
		return false
	}
	fabric, armed := failSafe()
	return armed && fabric == accessingFabricIndex
}

// PaseSessionAutoArmSeconds is the default fail-safe window duration
// the bridge arms automatically when a new PASE session is established
// and no ArmFailSafe has been sent yet.
const PaseSessionAutoArmSeconds uint16 = 60

// AutoArmOnPaseEstablished arms the fail-safe for 60 s when the bridge
// receives a new PASE session: a fresh fail-safe when none is armed, and
// one armed over PASE before is re-armed to 60 s from now, as matter.js's
// GeneralCommissioningServer does on every new PASE session
// (packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:55-66,
// #armFailSafe → failsafeContext.extend when armed, :89-94, at db021da0).
// A fail-safe another fabric armed is left as it is — matter.js's extend
// refuses another fabric (FailsafeTimer.ts:53-57). The check and the arm
// are one step (armFailSafeFor). Opening a commissioning window arms
// nothing (owner decision 47): this arm, or the commissioner's
// ArmFailSafe, is what arms the fail-safe of a commissioning.
//
// The daemon wires it from the PaseAdapter's onEstablished callback so the
// arm fires exactly once per successful Pake3 completion.
func (g *GeneralCommissioning) AutoArmOnPaseEstablished(ctx context.Context) {
	_ = g.armFailSafeFor(ctx, uint32(PaseSessionAutoArmSeconds), 0, true)
}

// logger is the server's logger ([GeneralCommissioningConfig.Logger]); a
// value built without its constructor logs through slog.Default()
// (ADR 0026 P9).
func (g *GeneralCommissioning) logger() *slog.Logger {
	if g.log != nil {
		return g.log
	}
	return slog.Default()
}
