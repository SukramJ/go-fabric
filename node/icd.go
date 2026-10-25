// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/store"
)

// ICDOperatingMode is an ICD's operating mode: a Short Idle Time or a Long
// Idle Time device (Core §9.15.1.5-6, OperatingModeEnum).
type ICDOperatingMode uint8

// The two operating modes.
const (
	// ICDModeSIT is a Short Idle Time ICD: the Check-In protocol, no LIT.
	ICDModeSIT ICDOperatingMode = 0
	// ICDModeLIT is a Long Idle Time capable ICD: it operates as LIT
	// while a client is registered (or while DynamicSITLIT forces it),
	// as SIT otherwise.
	ICDModeLIT ICDOperatingMode = 1
)

// String is the mode's name.
func (m ICDOperatingMode) String() string {
	if m == ICDModeLIT {
		return "LIT"
	}
	return "SIT"
}

// ICDConfig makes the node an intermittently connected device: the root
// endpoint serves ICD Management with the configured timings, the node
// counts and sends Check-Ins to its registered clients, advertises the ICD
// key, pads its MRP backoff by the fast-poll interval and keeps no
// subscription across a restart. The mode machine is the host's to drive
// ([Node.ICD]): a node of this module never truly sleeps.
//
// A zero timing is the schema default: IdleModeDuration 1 s,
// ActiveModeDuration 300 ms, ActiveModeThreshold 300 ms,
// ClientsSupportedPerFabric 1; a zero MaximumCheckInBackoff is
// IdleModeDuration (a Check-In every wake). A LIT ICD needs an
// ActiveModeThreshold of at least 5 s. Mirrors the configuration matter.js
// IcdManagementServer takes (IcdManagementServer.ts:184-229, the attribute
// defaults of icd-management.element.ts:31-69).
type ICDConfig struct {
	// Mode is SIT (Check-In protocol) or LIT (also LongIdleTimeSupport
	// and UserActiveModeTrigger, which LIT makes mandatory).
	Mode ICDOperatingMode
	// IdleModeDuration is the longest time the device is idle, whole
	// seconds, 1 s to 18 h.
	IdleModeDuration time.Duration
	// ActiveModeDuration is the shortest active window after a wake,
	// whole milliseconds, at most IdleModeDuration.
	ActiveModeDuration time.Duration
	// ActiveModeThreshold is how long the device stays active after
	// network activity, whole milliseconds, at most 65.535 s.
	ActiveModeThreshold time.Duration
	// MaximumCheckInBackoff is the longest gap between Check-Ins to a
	// client that does not answer, whole seconds, at least
	// IdleModeDuration.
	MaximumCheckInBackoff time.Duration
	// ClientsSupportedPerFabric is the number of Check-In clients a
	// fabric may register.
	ClientsSupportedPerFabric uint16
	// UserActiveModeTrigger, when set, states how a user wakes the
	// device; required for LIT (Core §9.16.6.7-8).
	UserActiveModeTrigger *ICDUserActiveModeTrigger
	// DynamicSITLIT lets the host switch a LIT device between SIT and LIT
	// at run time ([ICD.SetOperatingMode]); LIT only.
	DynamicSITLIT bool
}

// ICDUserActiveModeTrigger is the UserActiveModeTriggerHint bitmap and its
// instruction; a hint that names a count or a duration needs the
// instruction.
type ICDUserActiveModeTrigger struct {
	Hint        icddef.UserActiveModeTriggerBitmap
	Instruction string
}

// ICDModeEvent is a transition of the device's idle/active mode
// (matter.js IcdManagementServer.ts:813-820, Events).
type ICDModeEvent uint8

// The three events.
const (
	// ICDActiveModeEntered: the start or an idle→active wake — the node
	// sends its Check-Ins. Hook the hardware's power-up here.
	ICDActiveModeEntered ICDModeEvent = ICDModeEvent(mattercore.ICDActiveModeEntered)
	// ICDIdleModeEntered: the device went idle. Hook the power-down.
	ICDIdleModeEntered ICDModeEvent = ICDModeEvent(mattercore.ICDIdleModeEntered)
	// ICDMayEnterIdleMode: the active window elapsed while quiet; the host
	// may put the device to sleep ([ICD.EnterIdleMode]).
	ICDMayEnterIdleMode ICDModeEvent = ICDModeEvent(mattercore.ICDMayEnterIdleMode)
)

// String is the matter.js event name.
func (e ICDModeEvent) String() string { return mattercore.ICDModeEvent(e).String() }

// coreConfig is the ICD Management configuration of c, without the node's
// ports. A duration that is not whole in its unit, or out of the unit's
// range, is an error.
func (c *ICDConfig) coreConfig() (mattercore.ICDManagementConfig, error) {
	var errs []error
	whole := func(name string, d, unit time.Duration, maxUnits uint64) uint64 {
		if d < 0 || d%unit != 0 || uint64(d/unit) > maxUnits {
			errs = append(errs, fmt.Errorf("node: ICD.%s %v is not a whole number of %v up to %d", name, d, unit, maxUnits))
			return 0
		}
		return uint64(d / unit)
	}
	idle := whole("IdleModeDuration", c.IdleModeDuration, time.Second, 64800)
	backoff := whole("MaximumCheckInBackoff", c.MaximumCheckInBackoff, time.Second, 64800)
	if backoff == 0 {
		backoff = max(idle, 1)
	}
	out := mattercore.ICDManagementConfig{
		IdleModeDuration:          uint32(idle),                                                                            //nolint:gosec // G115: at most 64800
		ActiveModeDuration:        uint32(whole("ActiveModeDuration", c.ActiveModeDuration, time.Millisecond, 0xFFFFFFFF)), //nolint:gosec // G115: at most 0xFFFFFFFF
		ActiveModeThreshold:       uint16(whole("ActiveModeThreshold", c.ActiveModeThreshold, time.Millisecond, 0xFFFF)),   //nolint:gosec // G115: at most 0xFFFF
		MaximumCheckInBackoff:     uint32(backoff),                                                                         //nolint:gosec // G115: at most 64800
		ClientsSupportedPerFabric: c.ClientsSupportedPerFabric,
		Features:                  icddef.FeatureCheckInProtocolSupport,
	}
	if t := c.UserActiveModeTrigger; t != nil {
		out.Features |= icddef.FeatureUserActiveModeTrigger
		out.UserActiveModeTriggerHint, out.UserActiveModeTriggerInstruction = t.Hint, t.Instruction
	}
	switch c.Mode {
	case ICDModeSIT:
		if c.DynamicSITLIT {
			errs = append(errs, errors.New("node: ICD.DynamicSITLIT needs Mode LIT"))
		}
	case ICDModeLIT:
		out.Features |= icddef.FeatureLongIdleTimeSupport
		if c.UserActiveModeTrigger == nil {
			errs = append(errs, errors.New("node: ICD.UserActiveModeTrigger is mandatory for a LIT ICD"))
		}
		if c.DynamicSITLIT {
			out.Features |= icddef.FeatureDynamicSitLitSupport
		}
		sit := icddef.OperatingModeSit // registration-driven: SIT until a client registers
		out.OperatingMode = &sit
	default:
		errs = append(errs, fmt.Errorf("node: ICD.Mode %d is neither SIT nor LIT", c.Mode))
	}
	if err := errors.Join(errs...); err != nil {
		return out, err
	}
	return out, out.Validate()
}

// validate is the ICD part of [Config.Validate].
func (c *ICDConfig) validate() error {
	_, err := c.coreConfig()
	return err
}

// conditionSIT and conditionLIT are the RootNode device-type conditions
// under which RootNode lists IcdManagement ("Sit | Lit",
// root-node.element.ts:91).
const (
	conditionSIT = "Sit"
	conditionLIT = "Lit"
)

// buildICDManagement builds the root's ICD Management server of cfg.ICD.
func buildICDManagement(cfg *Config, st store.Port, logger *slog.Logger) (*mattercore.ICDManagement, error) {
	cc, err := cfg.ICD.coreConfig()
	if err != nil {
		return nil, err
	}
	cc.Store, cc.Clock, cc.Logger = st, cfg.Clock, logger
	return mattercore.NewICDManagement(cc)
}

// wireICD connects the ICD Management server to the bridge: the Check-In
// sender over the bridge's unsecured send and the cached or resolved peer
// address, the subscriptions that suppress a Check-In, inbound activity,
// the advertisement and its refresh, the MRP fast-poll pad, and no
// subscription persistence — the side effects of matter.js
// IcdManagementServer.ts:232-236 (persistence off), :257 (the MRP pad),
// :300-306 (activity), :356-393 (the sender), :482-495 (the advertisement)
// and :518-523 (the re-advertisement).
func (n *Node) wireICD() {
	srv := n.root.ICDManagement
	if srv == nil {
		return
	}
	br := n.br
	n.logger.Info("icd.subscription_persistence_disabled",
		slog.String("reason", "this node operates as an ICD; its controllers re-subscribe on a Check-In"))
	br.SetSubscriptionPersistence(false)
	br.SetLocalFixedMRPBackoff(mattercore.ICDFastPollingInterval)
	br.SetOnInboundActivity(srv.NoteActivity)
	br.SetICDAdvertisement(func() bridge.ICDAdvertisement {
		a := srv.Advertisement()
		return bridge.ICDAdvertisement{Mode: uint8(a.Mode), ActiveThreshold: a.ActiveThreshold, OmitIdleInterval: a.OmitIdleInterval}
	})
	srv.SetSubscriptionSubjects(br.ActiveSubscriptionSubjects)
	sender := icd.NewSender(icd.SenderContext{
		ResolveAddress: func(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (*net.UDPAddr, error) {
			if addr := br.PeerSessionAddress(fabricIndex, peerNodeID); addr != nil {
				return addr, nil
			}
			f, ok := n.caseIDs.forFabric(fabricIndex)
			if !ok {
				return nil, nil
			}
			return br.ResolveOperationalAddress(ctx, f.identity.CompressedFabricID, peerNodeID)
		},
		SendUnsecured: func(_ context.Context, addr *net.UDPAddr, opcode uint8, payload []byte) (bool, error) {
			if err := br.SendUnsecured(addr, opcode, payload); err != nil {
				return false, err
			}
			return true, nil
		},
		Logger: n.logger,
	})
	srv.SetCheckInSender(sender.Send)
	// A LIT-capable publisher grants its idle cycle as every
	// subscription's MaxInterval (ServerSubscription.ts:302-312).
	if cc, err := n.cfg.ICD.coreConfig(); err == nil && n.cfg.ICD.Mode == ICDModeLIT {
		n.sec.subs.SetICDIdleModeDuration(uint16(max(cc.IdleModeDuration, 1))) //nolint:gosec // G115: at most 64800
	}
	srv.SetOnOperatingModeChanged(func() { n.reannounceFabrics(n.life) })
}

// reannounceFabrics publishes every fabric's operational record again —
// matter.js DeviceAdvertiser.refreshOperationalAdvertisement per fabric.
func (n *Node) reannounceFabrics(ctx context.Context) {
	fabrics, err := n.st.ListFabrics(ctx)
	if err != nil {
		n.logger.Warn("icd.reannounce_list_failed", slog.Any("err", err))
		return
	}
	for _, f := range fabrics {
		n.br.AnnounceFabric(ctx, f.CompressedID, f.NodeID)
	}
}

// ICD is the host's handle on an ICD node's mode machine and operating
// mode ([Config].ICD). Every method is safe for concurrent use.
type ICD struct {
	n   *Node
	srv *mattercore.ICDManagement
}

// ICD returns the node's ICD handle; nil for a node that is no ICD.
func (n *Node) ICD() *ICD {
	if n.root == nil || n.root.ICDManagement == nil {
		return nil
	}
	return &ICD{n: n, srv: n.root.ICDManagement}
}

// RequestActiveMode wakes the device for a full active window — the
// idle→active transition at which the node sends its Check-Ins. A no-op
// before Start (IcdManagementServer.ts:632-634).
func (i *ICD) RequestActiveMode() { i.srv.RequestActiveMode() }

// EnterIdleMode forces idle mode now (IcdManagementServer.ts:653-655).
func (i *ICD) EnterIdleMode() { i.srv.EnterIdleMode() }

// TriggerUserActiveMode is the device-physical User Active Mode Trigger:
// the Check-In back-off resets and the device wakes
// (IcdManagementServer.ts:642-645).
func (i *ICD) TriggerUserActiveMode() { i.srv.TriggerUserActiveMode() }

// Active reports active mode.
func (i *ICD) Active() bool { return i.srv.Mode() == icd.ModeActive }

// Mode is the operating mode the node advertises: LIT for a LIT
// ICD with a registered client or a forced LIT, SIT otherwise.
func (i *ICD) Mode() ICDOperatingMode { return ICDOperatingMode(i.srv.OperatingMode()) }

// SetOperatingMode switches a LIT ICD between SIT and LIT at run time. With
// [ICDConfig].DynamicSITLIT the mode is forced until
// [ICD.WithdrawForcedOperatingMode] or the node's restart; without it the
// mode follows the registrations and mode must be that mode
// (IcdManagementServer.ts:666-684).
func (i *ICD) SetOperatingMode(mode ICDOperatingMode) error {
	return i.srv.SetOperatingMode(icddef.OperatingModeEnum(mode))
}

// WithdrawForcedOperatingMode returns a DynamicSITLIT ICD to the
// registration-driven mode (IcdManagementServer.ts:694-700).
func (i *ICD) WithdrawForcedOperatingMode() error { return i.srv.WithdrawForcedOperatingMode() }

// OnModeChange registers fn for every mode event. fn runs inline, on the
// goroutine of the transition — a command, a host call, the active
// window's timer — and must not block; [ICD.OnModeChangeAsync] queues it
// instead. It returns the unsubscribe function.
func (i *ICD) OnModeChange(fn func(ICDModeEvent)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	return i.srv.OnModeEvent(func(e mattercore.ICDModeEvent) { fn(ICDModeEvent(e)) })
}

// OnModeChangeAsync is [ICD.OnModeChange] with fn queued on the node's
// callback goroutine: fn runs later, one call after the other, while the
// node runs (between [Node.Start] and [Node.Stop]). fn must not stop the
// node.
func (i *ICD) OnModeChangeAsync(fn func(ICDModeEvent)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	q := i.n.callbacks
	return i.srv.OnModeEvent(func(e mattercore.ICDModeEvent) { q.Enqueue(func() { fn(ICDModeEvent(e)) }) })
}
