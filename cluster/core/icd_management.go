// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/store"
)

// ICDManagementClusterID is the ICD Management cluster (0x0046).
const ICDManagementClusterID = icddef.ClusterID

// ICDFastPollingInterval is the MRP backoff addend of a node operating as an
// ICD: an ICD in active mode polls for the acknowledgement only at its
// fast-poll cadence, so the sender pads its retransmission backoff by one
// such interval (Core §4.12.2.1). Mirrors matter.js
// IcdManagementServer.ts:53 (ICD_FAST_POLLING_INTERVAL).
const ICDFastPollingInterval = 200 * time.Millisecond

// The server's constants (IcdManagementServer.ts:89-113).
const (
	// minLITActiveModeThresholdMs is the shortest ActiveModeThreshold of
	// a LIT ICD (Core §9.15.1.6.2), MIN_LIT_ACTIVE_MODE_THRESHOLD.
	minLITActiveModeThresholdMs = 5000
	// stayActivePromiseFloor caps the guaranteed minimum of a
	// StayActiveRequest promise at min(30 s, requested) (Core
	// §9.16.7.5.1.1), STAY_ACTIVE_PROMISE_FLOOR.
	stayActivePromiseFloor = 30 * time.Second
)

// instructionRequiredTriggerHints are the UserActiveModeTriggerHint bits
// that need a non-empty instruction; the LightsBlink bits depend on it but
// may leave it empty (Core §9.16.6.7). Mirrors
// IcdManagementServer.ts:104-113 (INSTRUCTION_REQUIRED_TRIGGER_HINTS).
var instructionRequiredTriggerHints = []struct {
	bit  icddef.UserActiveModeTriggerBitmap
	name string
}{
	{icddef.UserActiveModeTriggerCustomInstruction, "customInstruction"},
	{icddef.UserActiveModeTriggerActuateSensorSeconds, "actuateSensorSeconds"},
	{icddef.UserActiveModeTriggerActuateSensorTimes, "actuateSensorTimes"},
	{icddef.UserActiveModeTriggerResetButtonSeconds, "resetButtonSeconds"},
	{icddef.UserActiveModeTriggerResetButtonTimes, "resetButtonTimes"},
	{icddef.UserActiveModeTriggerSetupButtonSeconds, "setupButtonSeconds"},
	{icddef.UserActiveModeTriggerSetupButtonTimes, "setupButtonTimes"},
	{icddef.UserActiveModeTriggerAppDefinedButton, "appDefinedButton"},
}

// ErrICDManagementConfig wraps every refused ICD Management configuration —
// matter.js's ImplementationError from initialize (IcdManagementServer.ts:184-229).
var ErrICDManagementConfig = errors.New("core: invalid ICD Management configuration")

// ErrICDOperatingMode reports a SetOperatingMode or
// WithdrawForcedOperatingMode the server's features do not allow
// (IcdManagementServer.ts:666-700).
var ErrICDOperatingMode = errors.New("core: ICD operating mode")

// ICDManagementStore is what the server persists to: the store.ICD area and
// the fabric list it prunes the registrations of departed fabrics against.
// store.Port satisfies it.
type ICDManagementStore interface {
	store.ICD
	ListFabrics(ctx context.Context) ([]store.FabricRecord, error)
}

// ICDManagementConfig configures [NewICDManagement]. A zero duration or
// count is the schema default (icd-management.element.ts:31-69):
// IdleModeDuration 1 s, ActiveModeDuration 300 ms, ActiveModeThreshold
// 300 ms, ClientsSupportedPerFabric 1, MaximumCheckInBackoff 1 s.
type ICDManagementConfig struct {
	// Features is the FeatureMap; zero is CheckInProtocolSupport alone,
	// matter.js's IcdManagementServer (IcdManagementServer.ts:838).
	Features icddef.Feature
	// IdleModeDuration is in seconds, 1 to 64800.
	IdleModeDuration uint32
	// ActiveModeDuration is in milliseconds.
	ActiveModeDuration uint32
	// ActiveModeThreshold is in milliseconds; a LIT ICD needs at least
	// 5000.
	ActiveModeThreshold uint16
	// ClientsSupportedPerFabric is the number of registrations a fabric
	// may hold.
	ClientsSupportedPerFabric uint16
	// MaximumCheckInBackoff is in seconds, at least IdleModeDuration.
	MaximumCheckInBackoff uint32
	// UserActiveModeTriggerHint and UserActiveModeTriggerInstruction are
	// served with the UserActiveModeTrigger feature.
	UserActiveModeTriggerHint        icddef.UserActiveModeTriggerBitmap
	UserActiveModeTriggerInstruction string
	// OperatingMode is the initial OperatingMode, mandatory with
	// LongIdleTimeSupport, which has no default for it; the server then
	// derives it from the registrations (or a forced DSLS mode).
	OperatingMode *icddef.OperatingModeEnum
	// Store persists the registrations and the counter; nil persists
	// nothing.
	Store ICDManagementStore
	// Clock times the active window; nil is the system clock.
	Clock clock.Clock
	// Logger receives the server's lines; nil is slog.Default.
	Logger *slog.Logger
}

func (c ICDManagementConfig) withDefaults() ICDManagementConfig {
	if c.Features == 0 {
		c.Features = icddef.FeatureCheckInProtocolSupport
	}
	c.IdleModeDuration = cmp.Or(c.IdleModeDuration, 1)
	c.ActiveModeDuration = cmp.Or(c.ActiveModeDuration, 300)
	c.ActiveModeThreshold = cmp.Or(c.ActiveModeThreshold, 300)
	c.ClientsSupportedPerFabric = cmp.Or(c.ClientsSupportedPerFabric, 1)
	c.MaximumCheckInBackoff = cmp.Or(c.MaximumCheckInBackoff, 1)
	return c
}

func (c ICDManagementConfig) has(f icddef.Feature) bool { return c.Features&f != 0 }

// Validate refuses what matter.js's initialize refuses
// (IcdManagementServer.ts:184-229), after the defaults: an idle duration
// shorter than the active one (Core §9.16.6.1), a maximum back-off shorter
// than the idle duration (§9.16.6.10), LongIdleTimeSupport without
// CheckInProtocolSupport, a LIT threshold under 5 s (§9.15.1.6.2), a LIT
// configuration without its OperatingMode, and a trigger hint that needs
// an instruction without one (§9.16.6.7-8). Every error wraps
// [ErrICDManagementConfig] and names the field.
func (c ICDManagementConfig) Validate() error {
	c = c.withDefaults()
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrICDManagementConfig}, args...)...))
	}
	if c.IdleModeDuration > 64800 {
		fail("IdleModeDuration (%d s) exceeds 64800 s", c.IdleModeDuration)
	}
	if uint64(c.IdleModeDuration)*1000 < uint64(c.ActiveModeDuration) {
		fail("IdleModeDuration (%d s) must be >= ActiveModeDuration (%d ms)", c.IdleModeDuration, c.ActiveModeDuration)
	}
	if c.MaximumCheckInBackoff < c.IdleModeDuration {
		fail("MaximumCheckInBackoff (%d s) must be >= IdleModeDuration (%d s)", c.MaximumCheckInBackoff, c.IdleModeDuration)
	}
	lits := c.has(icddef.FeatureLongIdleTimeSupport)
	if lits && !c.has(icddef.FeatureCheckInProtocolSupport) {
		fail("LongIdleTimeSupport requires the CheckInProtocolSupport feature")
	}
	if c.has(icddef.FeatureDynamicSitLitSupport) && !lits {
		fail("DynamicSitLitSupport requires the LongIdleTimeSupport feature")
	}
	if lits && c.ActiveModeThreshold < minLITActiveModeThresholdMs {
		fail("a LIT ICD requires ActiveModeThreshold (%d ms) >= %d ms", c.ActiveModeThreshold, minLITActiveModeThresholdMs)
	}
	if lits && c.OperatingMode == nil {
		fail("OperatingMode is mandatory with LongIdleTimeSupport")
	}
	if c.has(icddef.FeatureUserActiveModeTrigger) && c.UserActiveModeTriggerInstruction == "" {
		for _, h := range instructionRequiredTriggerHints {
			if c.UserActiveModeTriggerHint&h.bit != 0 {
				fail("UserActiveModeTriggerHint.%s requires a non-empty UserActiveModeTriggerInstruction", h.name)
				break
			}
		}
	}
	if len(c.UserActiveModeTriggerInstruction) > 128 {
		fail("UserActiveModeTriggerInstruction is %d bytes, at most 128", len(c.UserActiveModeTriggerInstruction))
	}
	return errors.Join(errs...)
}

// ICDModeEvent is one of the three mode events of the server
// (IcdManagementServer.ts:813-820, Events).
type ICDModeEvent uint8

// The three events.
const (
	// ICDActiveModeEntered: the initial power-up or an idle→active wake,
	// where a CIP ICD sends its Check-Ins.
	ICDActiveModeEntered ICDModeEvent = iota
	// ICDIdleModeEntered: the device went idle.
	ICDIdleModeEntered
	// ICDMayEnterIdleMode: the active window elapsed while quiet; the
	// host may put the device to sleep. No transition happens.
	ICDMayEnterIdleMode
)

// String is the matter.js event name.
func (e ICDModeEvent) String() string {
	switch e {
	case ICDActiveModeEntered:
		return "activeModeEntered"
	case ICDIdleModeEntered:
		return "idleModeEntered"
	default:
		return "mayEnterIdleMode"
	}
}

// ICDAdvertisement is what the operational DNS-SD record of an ICD carries:
// the ICD key, the threshold as SAT, and SII only for a SIT ICD (Core
// §9.15.1.6.2). Mirrors matter.js DeviceAdvertiser.ts:25-36
// (IcdAdvertisement) as IcdManagementServer.ts:482-495 builds it.
type ICDAdvertisement struct {
	// Mode is the ICD TXT key: the effective operating mode.
	Mode icddef.OperatingModeEnum
	// ActiveThreshold is ActiveModeThreshold, the SAT key.
	ActiveThreshold time.Duration
	// OmitIdleInterval is true for a LIT ICD, which SHOULD NOT advertise
	// SII.
	OmitIdleInterval bool
}

// icdClient is one RegisteredClients entry with its ICDToken — matter.js's
// MonitoringRegistration and its IcdKeyEntry (IcdManagementServer.ts:572-589).
type icdClient struct {
	fabricIndex      uint8
	checkInNodeID    uint64
	monitoredSubject uint64
	clientType       icddef.ClientTypeEnum
	key              []byte
}

// ICDManagement is the ICD Management server (0x0046) of an intermittently
// connected node: the timing attributes, the registered Check-In clients
// with their keys, the Check-In counter, the idle/active mode machine and
// the Check-In sender, and with LongIdleTimeSupport the SIT/LIT operating
// mode. The mode is driven from outside — a module node never truly
// sleeps: [ICDManagement.EnterIdleMode], [ICDManagement.RequestActiveMode],
// [ICDManagement.TriggerUserActiveMode], StayActiveRequest and inbound
// activity ([ICDManagement.NoteActivity]) move it. The node mounts it on
// the root only for a node configured as an ICD.
//
// Mirrors matter.js packages/node/src/behaviors/icd-management/IcdManagementServer.ts
// (IcdManagementBaseServer, :179-773): initialize is [NewICDManagement] and
// [ICDManagementConfig.Validate], #online is [ICDManagement.Start], the
// goingOffline reaction [ICDManagement.Stop]; the behavior's persistent
// state (registeredClients, icdCounter, icdKeys) is the store.ICD area,
// FabricIcd's device role (fabric.icd) one icd.Registrations per fabric.
type ICDManagement struct {
	cfg     ICDManagementConfig
	inst    *spec.Instance
	clock   clock.Clock
	logger  *slog.Logger
	mode    *icd.ModeState
	backOff *icd.DoublingBackOff

	// mu guards the fields below it. It is never held across the store,
	// the sender, a port or a callback.
	mu            sync.Mutex
	counter       *icd.Counter
	clients       []icdClient                  // registeredClients with icdKeys, in registration order
	fabrics       map[uint8]*icd.Registrations // fabric.icd, the operational view
	forced        *icddef.OperatingModeEnum
	operatingMode icddef.OperatingModeEnum
	started       bool
	send          func(context.Context, icd.CheckInRequest) (bool, error)
	subjects      icd.SubscriptionSubjects
	onModeChanged func()
	observers     map[uint64]func(ICDModeEvent)
	nextObserver  uint64
	life          context.Context
	cancel        context.CancelFunc

	// cmdMu serialises the commands and the fabric removal, each a
	// read-check-persist sequence over the registrations.
	cmdMu sync.Mutex

	// sending guards one Check-In pass at a time
	// (IcdManagementServer.ts:408-413, sendingCheckIns); passes waits
	// for the pass in flight on Stop.
	sending atomic.Bool
	passes  sync.WaitGroup

	changes cluster.AttributeChanges
}

var (
	_ contract.ClusterServer           = (*ICDManagement)(nil)
	_ contract.ClusterAttributeLister  = (*ICDManagement)(nil)
	_ contract.ClusterCommandLister    = (*ICDManagement)(nil)
	_ contract.FabricScopedReader      = (*ICDManagement)(nil)
	_ contract.AttributeChangeNotifier = (*ICDManagement)(nil)
)

// NewICDManagement returns the server of a validated configuration
// ([ICDManagementConfig.Validate]). It is stopped: [ICDManagement.Start]
// loads the persisted state and enters active mode.
func NewICDManagement(cfg ICDManagementConfig) (*ICDManagement, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.withDefaults()
	opts := spec.Options{Features: uint32(cfg.Features), Commands: []uint32{icddef.CmdStayActiveRequest}}
	if cfg.has(icddef.FeatureUserActiveModeTrigger) {
		opts.Attributes = []uint32{icddef.AttrUserActiveModeTriggerInstruction}
	}
	inst, err := spec.New(icddef.Definition, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrICDManagementConfig, err)
	}
	s := &ICDManagement{
		cfg:     cfg,
		inst:    inst,
		clock:   clock.Or(cfg.Clock),
		logger:  cmp.Or(cfg.Logger, slog.Default()),
		fabrics: map[uint8]*icd.Registrations{},
		backOff: icd.NewDoublingBackOff(time.Duration(cfg.IdleModeDuration)*time.Second, time.Duration(cfg.MaximumCheckInBackoff)*time.Second),
	}
	if cfg.OperatingMode != nil {
		s.operatingMode = *cfg.OperatingMode
	}
	s.mode = icd.NewModeState(icd.ModeOptions{
		ActiveModeDuration:  time.Duration(cfg.ActiveModeDuration) * time.Millisecond,
		ActiveModeThreshold: time.Duration(cfg.ActiveModeThreshold) * time.Millisecond,
		OnActiveEntered:     s.activeEntered,
		OnIdleEntered:       func() { s.emit(ICDIdleModeEntered) },
		OnMayEnterIdle:      func() { s.emit(ICDMayEnterIdleMode) },
		Clock:               s.clock,
	})
	return s, nil
}

// SetCheckInSender sets the send the Check-In pass uses — the node's
// icd.Sender over the bridge's unsecured send. nil sends nothing.
// Mirrors IcdManagementServer.ts:356-393 (createCheckInSender), which a
// test overrides the same way.
func (s *ICDManagement) SetCheckInSender(send func(context.Context, icd.CheckInRequest) (bool, error)) {
	s.mu.Lock()
	s.send = send
	s.mu.Unlock()
}

// SetSubscriptionSubjects sets the view of the active subscriptions per
// fabric that suppresses a covered Check-In (IcdCheckInSuppression.ts:39-53).
// nil suppresses nothing.
func (s *ICDManagement) SetSubscriptionSubjects(fn icd.SubscriptionSubjects) {
	s.mu.Lock()
	s.subjects = fn
	s.mu.Unlock()
}

// SetOnOperatingModeChanged sets the callback that re-advertises every
// fabric when the effective operating mode changes
// (IcdManagementServer.ts:518-523). It runs outside the server's lock.
func (s *ICDManagement) SetOnOperatingModeChanged(fn func()) {
	s.mu.Lock()
	s.onModeChanged = fn
	s.mu.Unlock()
}

// OnModeEvent registers fn for the three mode events; fn runs inline, on
// the goroutine of the transition (a command, a host call, the active
// window's timer), outside the server's lock. It returns the unsubscribe
// function.
func (s *ICDManagement) OnModeEvent(fn func(ICDModeEvent)) (unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observers == nil {
		s.observers = map[uint64]func(ICDModeEvent){}
	}
	id := s.nextObserver
	s.nextObserver++
	s.observers[id] = fn
	return func() {
		s.mu.Lock()
		delete(s.observers, id)
		s.mu.Unlock()
	}
}

func (s *ICDManagement) emit(e ICDModeEvent) {
	s.mu.Lock()
	obs := slices.Collect(maps.Values(s.observers))
	s.mu.Unlock()
	for _, fn := range obs {
		fn(e)
	}
}

// Start is matter.js's #online (IcdManagementServer.ts:252-343): the
// counter is created once from the persisted value (a random initial value
// for a factory-fresh device, Core §4.6.1.1) and the boot-bumped value
// persisted (§4.6.3); the registrations are loaded, those of fabrics that
// no longer exist pruned and the prune persisted; the operating mode is
// reconciled; and the mode machine starts in active mode, which sends the
// first Check-Ins. Idempotent while started.
func (s *ICDManagement) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	counter, err := s.loadCounter(ctx)
	if err != nil {
		return err
	}
	clients, err := s.loadClients(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.counter = counter
	s.clients = clients
	s.fabrics = map[uint8]*icd.Registrations{}
	for _, c := range clients {
		s.fabricLocked(c.fabricIndex).Set(c.registration())
	}
	s.life, s.cancel = context.WithCancel(context.WithoutCancel(ctx))
	s.started = true
	changed := s.updateOperatingModeLocked()
	s.mu.Unlock()
	s.operatingModeChanged(changed)

	s.mode.Start()
	return nil
}

// loadCounter seeds the counter (IcdManagementServer.ts:260-272).
func (s *ICDManagement) loadCounter(ctx context.Context) (*icd.Counter, error) {
	s.mu.Lock()
	existing := s.counter
	s.mu.Unlock()
	if existing != nil {
		return existing, nil // online re-fires across stop/start: once only (:259-260)
	}
	var persisted uint32
	if st := s.cfg.Store; st != nil {
		v, ok, err := st.LoadICDCounter(ctx)
		if err != nil {
			return nil, fmt.Errorf("core: icd management: load counter: %w", err)
		}
		if ok {
			persisted = v
		}
	}
	seed := persisted
	if seed == 0 {
		seed = icd.RandomInitialValue()
	}
	counter := icd.NewCounter(seed)
	if err := s.saveCounter(ctx, counter.Value()); err != nil {
		return nil, err
	}
	counter.OnChange(func(v uint32) {
		if err := s.saveCounter(context.Background(), v); err != nil {
			s.logger.Warn("icd.management.persist_counter", slog.Any("err", err))
		}
	})
	return counter, nil
}

func (s *ICDManagement) saveCounter(ctx context.Context, v uint32) error {
	if st := s.cfg.Store; st != nil {
		if err := st.SaveICDCounter(ctx, v); err != nil {
			return fmt.Errorf("core: icd management: save counter: %w", err)
		}
	}
	return nil
}

// loadClients reads the persisted registrations and prunes the keys of
// fabrics that no longer exist, so a stale ICDToken is never replayed
// (IcdManagementServer.ts:311-326).
func (s *ICDManagement) loadClients(ctx context.Context) ([]icdClient, error) {
	st := s.cfg.Store
	if st == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		return slices.Clone(s.clients), nil
	}
	regs, err := st.ListICDRegistrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("core: icd management: load registrations: %w", err)
	}
	fabrics, err := st.ListFabrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("core: icd management: list fabrics: %w", err)
	}
	live := map[uint8]bool{}
	for _, f := range fabrics {
		live[f.FabricIndex] = true
	}
	var out []icdClient
	pruned := map[uint8]bool{}
	for _, r := range regs {
		if !live[r.FabricIndex] {
			pruned[r.FabricIndex] = true
			continue
		}
		out = append(out, icdClient{
			fabricIndex: r.FabricIndex, checkInNodeID: r.CheckInNodeID, monitoredSubject: r.MonitoredSubject,
			clientType: icddef.ClientTypeEnum(r.ClientType), key: slices.Clone(r.Key),
		})
	}
	for f := range pruned {
		if err := st.ReplaceICDRegistrations(ctx, f, nil); err != nil {
			return nil, fmt.Errorf("core: icd management: prune registrations of fabric %d: %w", f, err)
		}
	}
	return out, nil
}

// Stop is matter.js's goingOffline reaction (IcdManagementServer.ts:345-347):
// the mode machine stops without a transition. It waits for the Check-In
// pass in flight. Idempotent.
func (s *ICDManagement) Stop() {
	s.mode.Stop()
	s.mu.Lock()
	cancel := s.cancel
	s.started = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.passes.Wait()
}

// Mode is the idle/active mode.
func (s *ICDManagement) Mode() icd.Mode { return s.mode.Mode() }

// Counter is the Check-In counter; nil before the first Start.
func (s *ICDManagement) Counter() *icd.Counter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counter
}

// Registrations is fabric.icd of fabricIndex: the registered clients with
// their keys, in registration order (FabricIcd.ts:45-47).
func (s *ICDManagement) Registrations(fabricIndex uint8) []icd.Registration {
	s.mu.Lock()
	r := s.fabrics[fabricIndex]
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.All()
}

// NoteActivity records inbound activity: it extends the active window, or
// wakes an idle device (IcdManagementServer.ts:300-306, the NodeActivity
// inactive observer).
func (s *ICDManagement) NoteActivity() { s.mode.NoteActivity() }

// RequestActiveMode wakes the device for a full active window — the
// idle→active transition at which a CIP ICD sends its Check-Ins. A no-op
// while stopped. Mirrors IcdManagementServer.ts:632-634.
func (s *ICDManagement) RequestActiveMode() {
	s.mode.RequestActive(time.Duration(s.cfg.ActiveModeDuration) * time.Millisecond)
}

// TriggerUserActiveMode is the device-physical User Active Mode Trigger:
// every client's back-off resets and the device wakes. UAT has no Matter
// command. Mirrors IcdManagementServer.ts:642-645.
func (s *ICDManagement) TriggerUserActiveMode() {
	s.backOff.ResetAll()
	s.RequestActiveMode()
}

// EnterIdleMode forces idle mode now. A no-op while stopped. Mirrors
// IcdManagementServer.ts:653-655.
func (s *ICDManagement) EnterIdleMode() { s.mode.EnterIdle() }

// stayActive extends the active window by requested and returns the
// duration promised: the real remaining window, never less than
// min(30 s, requested). Mirrors IcdManagementServer.ts:617-624.
func (s *ICDManagement) stayActive(requested time.Duration) time.Duration {
	floor := min(stayActivePromiseFloor, requested)
	return max(s.mode.RequestActive(requested), floor)
}

// OperatingMode is the OperatingMode attribute; SIT without
// LongIdleTimeSupport.
func (s *ICDManagement) OperatingMode() icddef.OperatingModeEnum {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effectiveModeLocked()
}

// SetOperatingMode switches the operating mode at run time. It needs
// LongIdleTimeSupport; without DynamicSitLitSupport the mode follows the
// registrations and mode must be that mode, with it the mode is forced
// until [ICDManagement.WithdrawForcedOperatingMode] or a restart (Core
// §9.15.1.6.4). Mirrors IcdManagementServer.ts:666-684.
func (s *ICDManagement) SetOperatingMode(mode icddef.OperatingModeEnum) error {
	if !s.cfg.has(icddef.FeatureLongIdleTimeSupport) {
		return fmt.Errorf("%w: setOperatingMode requires the LongIdleTimeSupport feature", ErrICDOperatingMode)
	}
	s.mu.Lock()
	if !s.cfg.has(icddef.FeatureDynamicSitLitSupport) {
		if mode != s.registrationModeLocked() {
			s.mu.Unlock()
			return fmt.Errorf("%w: without DynamicSitLitSupport the operating mode follows registration state and cannot be forced", ErrICDOperatingMode)
		}
	} else {
		s.forced = &mode
	}
	changed := s.updateOperatingModeLocked()
	s.mu.Unlock()
	s.operatingModeChanged(changed)
	return nil
}

// WithdrawForcedOperatingMode reverts a forced mode to the
// registration-driven one; it needs DynamicSitLitSupport. Mirrors
// IcdManagementServer.ts:694-700.
func (s *ICDManagement) WithdrawForcedOperatingMode() error {
	if !s.cfg.has(icddef.FeatureDynamicSitLitSupport) {
		return fmt.Errorf("%w: withdrawForcedOperatingMode requires the DynamicSitLitSupport feature", ErrICDOperatingMode)
	}
	s.mu.Lock()
	s.forced = nil
	changed := s.updateOperatingModeLocked()
	s.mu.Unlock()
	s.operatingModeChanged(changed)
	return nil
}

// Advertisement is the operational record's ICD part
// (IcdManagementServer.ts:482-495, #refreshIcdAdvertisementCache).
func (s *ICDManagement) Advertisement() ICDAdvertisement {
	s.mu.Lock()
	mode := s.effectiveModeLocked()
	s.mu.Unlock()
	return ICDAdvertisement{
		Mode:             mode,
		ActiveThreshold:  time.Duration(s.cfg.ActiveModeThreshold) * time.Millisecond,
		OmitIdleInterval: mode == icddef.OperatingModeLit,
	}
}

// registrationModeLocked is LIT when a client is registered.
func (s *ICDManagement) registrationModeLocked() icddef.OperatingModeEnum {
	if len(s.clients) > 0 {
		return icddef.OperatingModeLit
	}
	return icddef.OperatingModeSit
}

// effectiveModeLocked mirrors IcdManagementServer.ts:462-472 (#effectiveMode).
func (s *ICDManagement) effectiveModeLocked() icddef.OperatingModeEnum {
	if !s.cfg.has(icddef.FeatureLongIdleTimeSupport) {
		return icddef.OperatingModeSit
	}
	if s.cfg.has(icddef.FeatureDynamicSitLitSupport) && s.forced != nil {
		return *s.forced
	}
	return s.registrationModeLocked()
}

// updateOperatingModeLocked writes the effective mode into OperatingMode
// and reports whether it changed (IcdManagementServer.ts:502-511).
func (s *ICDManagement) updateOperatingModeLocked() bool {
	if !s.cfg.has(icddef.FeatureLongIdleTimeSupport) {
		return false
	}
	effective := s.effectiveModeLocked()
	if s.operatingMode == effective {
		return false
	}
	s.operatingMode = effective
	return true
}

// operatingModeChanged reports a changed OperatingMode and re-advertises
// (IcdManagementServer.ts:276, :518-523). Called without the lock.
func (s *ICDManagement) operatingModeChanged(changed bool) {
	if !changed {
		return
	}
	s.changes.Notify(icddef.AttrOperatingMode)
	s.mu.Lock()
	fn := s.onModeChanged
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *ICDManagement) fabricLocked(fabricIndex uint8) *icd.Registrations {
	r := s.fabrics[fabricIndex]
	if r == nil {
		r = &icd.Registrations{}
		s.fabrics[fabricIndex] = r
	}
	return r
}

func (c icdClient) registration() icd.Registration {
	return icd.Registration{CheckInNodeID: c.checkInNodeID, MonitoredSubject: c.monitoredSubject, Key: c.key, ClientType: icd.ClientType(c.clientType)}
}

// activeEntered emits ActiveModeEntered and starts a Check-In pass
// (IcdManagementServer.ts:287, :298).
func (s *ICDManagement) activeEntered() {
	s.emit(ICDActiveModeEntered)
	s.startCheckIns()
}

// startCheckIns runs one Check-In pass in the background unless one is in
// flight; the pass is counted work of the host (internal/inflight).
func (s *ICDManagement) startCheckIns() {
	if !s.cfg.has(icddef.FeatureCheckInProtocolSupport) || !s.sending.CompareAndSwap(false, true) {
		return
	}
	s.mu.Lock()
	ctx := s.life
	s.mu.Unlock()
	if ctx == nil {
		s.sending.Store(false)
		return
	}
	g := inflight.Of(s.clock)
	g.Enter(inflight.ICDCheckIns)
	s.passes.Add(1)
	go func() {
		defer s.passes.Done()
		defer g.Leave(inflight.ICDCheckIns)
		defer s.sending.Store(false)
		s.sendCheckIns(ctx)
	}()
}

// sendCheckIns sends a Check-In to every Permanent registration a
// subscription of its monitored subject does not cover, gated by the
// per-client back-off; the counter goes out post-increment, so the first
// Check-In carries an offset of at least 1 (Core §9.15.1, §9.16.5.3.2,
// §4.6.3). A failed send is logged, never returned.
//
// Mirrors matter.js IcdManagementServer.ts:404-445 (#sendCheckIns).
func (s *ICDManagement) sendCheckIns(ctx context.Context) {
	s.mu.Lock()
	send, subjects, counter := s.send, s.subjects, s.counter
	fabrics := slices.Sorted(maps.Keys(s.fabrics))
	views := make([]*icd.Registrations, len(fabrics))
	for i, f := range fabrics {
		views[i] = s.fabrics[f]
	}
	s.mu.Unlock()
	if send == nil || counter == nil {
		return
	}
	for i, fabricIndex := range fabrics {
		var subs []icd.SubscriptionSubject
		if subjects != nil {
			subs = subjects(fabricIndex)
		}
		for _, reg := range views[i].All() {
			if reg.ClientType != icd.ClientPermanent {
				continue
			}
			key := icd.ClientKey{FabricIndex: fabricIndex, CheckInNodeID: reg.CheckInNodeID}
			if icd.MonitoredSubjectCovered(reg.MonitoredSubject, subs) {
				s.backOff.RecordAnswered(key)
				continue
			}
			if !s.backOff.ShouldSend(key) {
				continue
			}
			s.backOff.RecordSent(key)
			if ctx.Err() != nil {
				return
			}
			ok, err := send(ctx, icd.CheckInRequest{
				FabricIndex: fabricIndex, PeerNodeID: reg.CheckInNodeID, Key: reg.Key,
				Counter: counter.Increment(), ActiveModeThreshold: s.cfg.ActiveModeThreshold,
			})
			switch {
			case err != nil:
				s.logger.Warn("icd.management.checkin_failed", slog.Uint64("fabric_index", uint64(fabricIndex)),
					slog.Uint64("check_in_node_id", reg.CheckInNodeID), slog.Any("err", err))
			case !ok:
				s.logger.Debug("icd.management.checkin_not_sent", slog.Uint64("fabric_index", uint64(fabricIndex)),
					slog.Uint64("check_in_node_id", reg.CheckInNodeID))
			}
		}
	}
}

// FabricRemoved drops the fabric's registrations, keys and back-off state
// (IcdManagementServer.ts:525-542, #onFabricDeleted); the store's fabric
// cascade has removed the rows already.
func (s *ICDManagement) FabricRemoved(ctx context.Context, fabricIndex uint8) {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()
	s.mu.Lock()
	before := len(s.clients)
	var gone []uint64
	s.clients = slices.DeleteFunc(s.clients, func(c icdClient) bool {
		if c.fabricIndex == fabricIndex {
			gone = append(gone, c.checkInNodeID)
			return true
		}
		return false
	})
	delete(s.fabrics, fabricIndex)
	changed := s.updateOperatingModeLocked()
	removed := len(s.clients) != before
	s.mu.Unlock()
	for _, n := range gone {
		s.backOff.Forget(icd.ClientKey{FabricIndex: fabricIndex, CheckInNodeID: n})
	}
	if st := s.cfg.Store; st != nil {
		if err := st.ReplaceICDRegistrations(ctx, fabricIndex, nil); err != nil {
			s.logger.Debug("icd.management.fabric_removed_persist", slog.Uint64("fabric_index", uint64(fabricIndex)), slog.Any("err", err))
		}
	}
	if removed {
		s.changes.Notify(icddef.AttrRegisteredClients)
	}
	s.operatingModeChanged(changed)
}

// MatterClusterID implements [contract.ClusterServer].
func (s *ICDManagement) MatterClusterID() uint32 { return ICDManagementClusterID }

// MatterRead implements [contract.ClusterServer]: a read without a request
// behind it sees every fabric's registrations whole.
func (s *ICDManagement) MatterRead(attrID uint32) (any, bool) {
	return s.read(attrID, func(icdClient) bool { return true })
}

// MatterReadFiltered implements [contract.FabricScopedReader]: a
// fabric-filtered read lists the accessing fabric's registrations; an
// unfiltered one lists every fabric's, another fabric's entry with its
// fabric-sensitive fields left out (Core §7.13.6).
func (s *ICDManagement) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	filtered, fabric, request := im.LookupFabricFilter(ctx)
	if attrID != icddef.AttrRegisteredClients || !request {
		return s.MatterRead(attrID) //nolint:contextcheck // MatterRead is the cluster-interface read without a request; it takes no ctx by contract
	}
	if filtered {
		return s.read(attrID, func(c icdClient) bool { return c.fabricIndex == fabric })
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(spec.List[icddef.MonitoringRegistrationStruct], 0, len(s.clients))
	for _, c := range s.clients {
		if c.fabricIndex != fabric {
			out = append(out, icddef.MonitoringRegistrationStruct{FabricIndex: c.fabricIndex})
			continue
		}
		out = append(out, c.wire())
	}
	return out, true
}

func (c icdClient) wire() icddef.MonitoringRegistrationStruct {
	return icddef.MonitoringRegistrationStruct{
		CheckInNodeId: c.checkInNodeID, MonitoredSubject: c.monitoredSubject,
		ClientType: c.clientType, FabricIndex: c.fabricIndex,
	}
}

func (s *ICDManagement) read(attrID uint32, keep func(icdClient) bool) (any, bool) {
	switch attrID {
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return s.inst.ReadGlobal(attrID)
	}
	if !s.inst.Serves(attrID) {
		return nil, false
	}
	c := s.cfg
	switch attrID {
	case icddef.AttrIdleModeDuration:
		return c.IdleModeDuration, true
	case icddef.AttrActiveModeDuration:
		return c.ActiveModeDuration, true
	case icddef.AttrActiveModeThreshold:
		return c.ActiveModeThreshold, true
	case icddef.AttrRegisteredClients:
		s.mu.Lock()
		defer s.mu.Unlock()
		out := spec.List[icddef.MonitoringRegistrationStruct]{}
		for _, cl := range s.clients {
			if keep(cl) {
				out = append(out, cl.wire())
			}
		}
		return out, true
	case icddef.AttrIcdCounter:
		if counter := s.Counter(); counter != nil {
			return counter.Value(), true
		}
		return uint32(0), true
	case icddef.AttrClientsSupportedPerFabric:
		return c.ClientsSupportedPerFabric, true
	case icddef.AttrUserActiveModeTriggerHint:
		return c.UserActiveModeTriggerHint, true
	case icddef.AttrUserActiveModeTriggerInstruction:
		return c.UserActiveModeTriggerInstruction, true
	case icddef.AttrOperatingMode:
		return s.OperatingMode(), true
	case icddef.AttrMaximumCheckInBackoff:
		return c.MaximumCheckInBackoff, true
	}
	return nil, false
}

// MatterWrite implements [contract.ClusterServer]: every attribute is
// read-only.
func (s *ICDManagement) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := s.inst.ValidateWrite(attrID, value, nil)
	return refusedWrite{sentinel: fmt.Errorf("core: ICDManagement attribute 0x%04X is read-only", attrID), status: err}
}

// MatterInvoke implements [contract.ClusterServer]: RegisterClient,
// UnregisterClient and StayActiveRequest.
func (s *ICDManagement) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if !s.inst.Accepts(cmdID) {
		return nil, im.UnsupportedCommandf("core: ICDManagement command 0x%02X not supported", cmdID)
	}
	switch cmdID {
	case icddef.CmdRegisterClient:
		req, ok := fields.(icddef.RegisterClientRequest)
		if !ok {
			return nil, spec.Errorf(im.StatusInvalidCommand, "core: RegisterClientRequest expected, got %T", fields)
		}
		return s.registerClient(ctx, req)
	case icddef.CmdUnregisterClient:
		req, ok := fields.(icddef.UnregisterClientRequest)
		if !ok {
			return nil, spec.Errorf(im.StatusInvalidCommand, "core: UnregisterClientRequest expected, got %T", fields)
		}
		return nil, s.unregisterClient(ctx, req)
	case icddef.CmdStayActiveRequest:
		req, ok := fields.(icddef.StayActiveRequestRequest)
		if !ok {
			return nil, spec.Errorf(im.StatusInvalidCommand, "core: StayActiveRequestRequest expected, got %T", fields)
		}
		promised := s.stayActive(time.Duration(req.StayActiveDuration) * time.Millisecond)
		return icddef.StayActiveResponse{PromisedActiveDuration: uint32(promised.Milliseconds())}, nil //nolint:gosec // G115: at most the requested uint32 milliseconds or the 30 s floor
	}
	return nil, im.UnsupportedCommandf("core: ICDManagement command 0x%02X not supported", cmdID)
}

// accessingFabric is the fabric of the command's session; a command
// without one is refused, as matter.js's assertRemoteActor and
// session.associatedFabric refuse it (IcdManagementServer.ts:554-555).
func accessingFabric(ctx context.Context) (uint8, error) {
	_, fabric := im.FabricFilterFromContext(ctx)
	if fabric == 0 {
		return 0, spec.Errorf(im.StatusUnsupportedAccess, "core: ICDManagement command requires an accessing fabric")
	}
	return fabric, nil
}

// registerClient mirrors IcdManagementServer.ts:551-602 (registerClient,
// Core §9.16.7.1): a new entry needs a free slot of the fabric
// (RESOURCE_EXHAUSTED), an existing one the caller's right to modify it
// (#assertMayModify); the entry and its key are stored and persisted, the
// fabric's operational view updated, the operating mode reconciled, and
// the response carries the counter.
func (s *ICDManagement) registerClient(ctx context.Context, req icddef.RegisterClientRequest) (icddef.RegisterClientResponse, error) {
	fabric, err := accessingFabric(ctx)
	if err != nil {
		return icddef.RegisterClientResponse{}, err
	}
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()

	s.mu.Lock()
	idx, n := -1, 0
	for i, c := range s.clients {
		if c.fabricIndex != fabric {
			continue
		}
		n++
		if c.checkInNodeID == req.CheckInNodeId {
			idx = i
		}
	}
	var stored []byte
	if idx >= 0 {
		stored = s.clients[idx].key
	}
	s.mu.Unlock()

	if idx < 0 {
		if n >= int(s.cfg.ClientsSupportedPerFabric) {
			return icddef.RegisterClientResponse{}, spec.Errorf(im.StatusResourceExhausted, "core: ICD client slots exhausted for fabric")
		}
	} else if err := assertMayModify(ctx, stored, req.VerificationKey); err != nil {
		return icddef.RegisterClientResponse{}, err
	}

	entry := icdClient{
		fabricIndex: fabric, checkInNodeID: req.CheckInNodeId, monitoredSubject: req.MonitoredSubject,
		clientType: req.ClientType, key: slices.Clone(req.Key),
	}
	s.mu.Lock()
	next := slices.Clone(s.clients)
	if idx < 0 {
		next = append(next, entry)
	} else {
		next[idx] = entry
	}
	s.mu.Unlock()
	if err := s.persistFabric(ctx, fabric, next); err != nil {
		return icddef.RegisterClientResponse{}, err
	}
	s.mu.Lock()
	s.clients = next
	s.fabricLocked(fabric).Set(entry.registration())
	changed := s.updateOperatingModeLocked()
	counter := s.counter
	s.mu.Unlock()

	s.changes.Notify(icddef.AttrRegisteredClients)
	s.operatingModeChanged(changed)
	var value uint32
	if counter != nil {
		value = counter.Value()
	}
	return icddef.RegisterClientResponse{IcdCounter: value}, nil
}

// unregisterClient mirrors IcdManagementServer.ts:705-730 (unregisterClient,
// Core §9.16.7.3): NOT_FOUND for an unknown client, the right to modify,
// then the entry, its key and its back-off state go.
func (s *ICDManagement) unregisterClient(ctx context.Context, req icddef.UnregisterClientRequest) error {
	fabric, err := accessingFabric(ctx)
	if err != nil {
		return err
	}
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()

	s.mu.Lock()
	idx := slices.IndexFunc(s.clients, func(c icdClient) bool {
		return c.fabricIndex == fabric && c.checkInNodeID == req.CheckInNodeId
	})
	var stored []byte
	if idx >= 0 {
		stored = s.clients[idx].key
	}
	s.mu.Unlock()
	if idx < 0 {
		return spec.Errorf(im.StatusNotFound, "core: no such ICD client registration")
	}
	if err := assertMayModify(ctx, stored, req.VerificationKey); err != nil {
		return err
	}
	s.mu.Lock()
	next := slices.Delete(slices.Clone(s.clients), idx, idx+1)
	s.mu.Unlock()
	if err := s.persistFabric(ctx, fabric, next); err != nil {
		return err
	}
	s.mu.Lock()
	s.clients = next
	s.fabricLocked(fabric).Delete(req.CheckInNodeId)
	changed := s.updateOperatingModeLocked()
	s.mu.Unlock()
	s.backOff.Forget(icd.ClientKey{FabricIndex: fabric, CheckInNodeID: req.CheckInNodeId})

	s.changes.Notify(icddef.AttrRegisteredClients)
	s.operatingModeChanged(changed)
	return nil
}

// persistFabric stores fabric's entries of clients.
func (s *ICDManagement) persistFabric(ctx context.Context, fabric uint8, clients []icdClient) error {
	st := s.cfg.Store
	if st == nil {
		return nil
	}
	var regs []store.ICDRegistration
	for _, c := range clients {
		if c.fabricIndex == fabric {
			regs = append(regs, store.ICDRegistration{
				FabricIndex: fabric, CheckInNodeID: c.checkInNodeID, MonitoredSubject: c.monitoredSubject,
				Key: c.key, ClientType: uint8(c.clientType),
			})
		}
	}
	if err := st.ReplaceICDRegistrations(ctx, fabric, regs); err != nil {
		return spec.Errorf(im.StatusFailure, "core: persist ICD registrations: %v", err)
	}
	return nil
}

// assertMayModify mirrors IcdManagementServer.ts:739-764 (#isAdministrator,
// #assertMayModify): an Administrator modifies any entry; a Manage caller
// must present the stored ICDToken as its VerificationKey, FAILURE
// otherwise (Core §9.16.7.1 step 2, §9.16.7.3 step 2).
func assertMayModify(ctx context.Context, stored []byte, verificationKey *[]byte) error {
	path, _ := im.CommandPathFromContext(ctx)
	if im.AuthorityAt(ctx, path.Endpoint, ICDManagementClusterID, uint8(spec.PrivilegeAdminister)).IsSuccess() {
		return nil
	}
	if verificationKey == nil || stored == nil || !bytes.Equal(*verificationKey, stored) {
		return spec.Errorf(im.StatusFailure, "core: VerificationKey mismatch")
	}
	return nil
}

// MatterReportable implements [contract.ClusterServer].
func (s *ICDManagement) MatterReportable() []uint32 { return s.inst.MatterReportable() }

// MatterAttributes implements [contract.ClusterAttributeLister].
func (s *ICDManagement) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *ICDManagement) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (s *ICDManagement) MatterGeneratedCommands() []uint32 { return s.inst.MatterGeneratedCommands() }

// OnAttributeChange implements [contract.AttributeChangeNotifier]:
// RegisteredClients and OperatingMode changes. The counter's quality "C"
// keeps its changes out of reports.
func (s *ICDManagement) OnAttributeChange(fn func(contract.Change)) (unsubscribe func()) {
	return s.changes.OnAttributeChange(fn)
}
