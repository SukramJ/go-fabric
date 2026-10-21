// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package lock contains the DoorLock cluster server (0x0101): a semantic
// port of matter.js DoorLockBaseServer (packages/node/src/behaviors/
// door-lock/DoorLockServer.ts, LockAuth.ts and LockSchedule.ts at
// 862ad75f).
//
// The cluster's identity is the generated definition (cluster/spec/
// doorlock, ADR 0013): ids, revision, the enums, the attribute, command
// and event lists of the feature selection ([DoorLockConfig].Features,
// matter.js DoorLockServer.with(...)), the event priorities, the write
// statuses and checks and the privileges. The behaviour is matter.js's,
// each part behind its feature bit:
//
//   - LockDoor, UnlockDoor, UnlockWithTimeout and UnboltDoor pass one
//     authorization (#authorize :1049-1080) before the lock moves: the
//     operating mode, the wrong-code lockout, the PIN a COTA lock may
//     require, the user the PIN identifies, that user's status, type and
//     schedules; then the hardware moves ([LockOperationHandler], matter.js
//     handleLockOperation :236) and the lock reports the operation.
//   - Users, credentials and the three schedule kinds (USR, PIN, RID, FGP,
//     WDSCH, YDSCH, HDSCH) with every status matter.js answers, the
//     LockUserChange events, auto-relock (AutoRelockTime, UnlockWithTimeout)
//     and ExpiringUser timeouts on [clock.Clock].
//   - The records persist through an [AuthStore] — *store.Store is one —
//     with every credential encrypted under the node's credential key.
//
// The lock state comes from one of two places. A server with a
// [StateSource] projects the host's lock (IsLocked, IsJammed) and moves it
// through LockInvoke, as the module's server did before it grew users; a
// server without one keeps LockState itself, as matter.js does, and moves
// the lock through [DoorLockConfig].Hardware.
package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// ClusterID is the DoorLock cluster id (0x0101).
const ClusterID = lockdef.ClusterID

// Feature is a DoorLock FeatureMap bit, the generated type.
type Feature = lockdef.Feature

// FeatureMap bits as [Feature] values for [DoorLockConfig].Features. The
// definition's conformance holds: USR needs PIN, RID or FGP. ALIRO, ALBU
// and FACE are not served.
const (
	FeaturePIN   = lockdef.FeaturePinCredential
	FeatureRID   = lockdef.FeatureRfidCredential
	FeatureFGP   = lockdef.FeatureFingerCredentials
	FeatureWDSCH = lockdef.FeatureWeekDayAccessSchedules
	FeatureDPS   = lockdef.FeatureDoorPositionSensor
	FeatureCOTA  = lockdef.FeatureCredentialOverTheAirAccess
	FeatureUSR   = lockdef.FeatureUser
	FeatureYDSCH = lockdef.FeatureYearDayAccessSchedules
	FeatureHDSCH = lockdef.FeatureHolidaySchedules
	FeatureUBOLT = lockdef.FeatureUnbolting
)

// servedFeatures are the bits this server implements.
const servedFeatures = FeaturePIN | FeatureRID | FeatureFGP | FeatureWDSCH | FeatureDPS |
	FeatureCOTA | FeatureUSR | FeatureYDSCH | FeatureHDSCH | FeatureUBOLT

// defaultSupportedOperatingModes is matter.js's default
// SupportedOperatingModes, { vacation, privacy, passage, alwaysSet: 2047 }
// (DoorLockServer.ts:117-119): a set bit marks a mode as NOT supported, so
// Normal and NoRemoteLockUnlock are the supported ones — 0xFFF6.
const defaultSupportedOperatingModes = lockdef.OperatingModesAlwaysSet | lockdef.OperatingModesVacation |
	lockdef.OperatingModesPrivacy | lockdef.OperatingModesPassage

// ErrConfig reports a configuration the cluster's model, or matter.js's
// initialize, refuses.
var ErrConfig = errors.New("lock: configuration outside the cluster's model")

// LockOperationEvent is the cluster-native payload for the DoorLock
// LockOperation event (id 0x02, priority critical) per Matter §5.2.10.3
// of a server without the User feature. Field set mirrors matter.js
// door-lock-cluster.element.ts:181-195; UserIndex, FabricIndex and
// SourceNode are nullable (nil pointer → TLV null). The Credentials field
// ([USR]-gated) is omitted entirely. A server with USR emits the
// generated [lockdef.LockOperationEvent], which carries it.
//
//nolint:revive // LockOperationEvent mirrors the Matter event name verbatim.
type LockOperationEvent struct {
	LockOperationType uint8
	OperationSource   uint8
	UserIndex         *uint16
	FabricIndex       *uint8
	SourceNode        *uint64
}

var errUnknownCommand = errors.New("lock: unknown command")

// StateSource is the read-side interface a model-layer lock DP must
// satisfy so DoorLockServer can project its Matter attribute surface.
//
// IsJammed returns true when ERROR_JAMMED is asserted.
// IsLocked returns (locked=true, observed=true) when the lock state is known
// and the lock is in the locked position.
// LockInvoke dispatches LockDoor / UnlockDoor / UnboltDoor commands to the
// CCU. cmdID is one of the [wire.DoorLockCmd*] constants; UnlockWithTimeout
// and an auto-relock arrive as UnlockDoor and LockDoor. A returned error
// fails the command: the server emits LockOperationError (the
// [LockOperationFailedError] reason, Unspecified otherwise) and answers the
// error's IM status if it carries one ([im.StatusCodeError]), FAILURE
// otherwise. It is not called for a request without an accessing fabric
// or one the authorization refuses. A source that also implements
// [LockOperationHandler] is called through HandleLockOperation instead. The
// implementation owns the southbound urgency of the write it performs:
// the cluster contract carries no priority, so a host whose command
// queue ranks by urgency names the value it wants inside LockInvoke.
type StateSource interface {
	IsJammed() bool
	IsLocked() (locked, observed bool)
	LockInvoke(ctx context.Context, cmdID uint32) error
}

// Actuation is the movement a lock operation asks of the hardware:
// matter.js DoorLockBaseServer.Actuation (DoorLockServer.ts:1525).
type Actuation uint8

// The actuations. Unbolting retracts the bolt without pulling the latch.
const (
	ActuationLock Actuation = iota
	ActuationUnlock
	ActuationUnbolt
)

// String names the actuation as matter.js does ("lock", "unlock",
// "unbolt").
func (a Actuation) String() string {
	switch a {
	case ActuationLock:
		return "lock"
	case ActuationUnlock:
		return "unlock"
	case ActuationUnbolt:
		return "unbolt"
	}
	return fmt.Sprintf("actuation(%d)", uint8(a))
}

// LockOperation is one movement of the lock hardware and who asked for
// it: matter.js DoorLockBaseServer.LockOperation (DoorLockServer.ts:1528-1536).
//
//nolint:revive // LockOperation mirrors matter.js DoorLockBaseServer.LockOperation.
type LockOperation struct {
	Actuation Actuation
	// Source is Remote for a command, Auto for auto-relock.
	Source lockdef.OperationSourceEnum
	// UserIndex is the user the PIN of a remote operation identifies; nil
	// for none.
	UserIndex *uint16
}

// LockOperationHandler moves the lock hardware: matter.js
// handleLockOperation (DoorLockServer.ts:218-236). The server calls it
// once a remote lock command is authorized, before it sets LockState and
// reports the operation, and for auto-relock; the calls never overlap. An
// error fails the operation: LockState stays as it was and
// LockOperationError reports the reason of a [LockOperationFailedError],
// Unspecified for any other error; a failed command is answered with the
// error's IM status if it carries one, FAILURE otherwise, and a failed
// auto-relock is not retried.
//
//nolint:revive // LockOperationHandler mirrors matter.js handleLockOperation.
type LockOperationHandler interface {
	HandleLockOperation(ctx context.Context, op LockOperation) error
}

// LockOperationFailedError is the error a [LockOperationHandler] returns
// to name the reason the LockOperationError event reports: matter.js
// LockOperationFailedError (DoorLockServer.ts:76-84).
//
//nolint:revive // LockOperationFailedError mirrors the matter.js error class.
type LockOperationFailedError struct {
	Reason  lockdef.OperationErrorEnum
	Message string
}

func (e *LockOperationFailedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("lock: lock operation failed: operation error %d", uint8(e.Reason))
}

// DoorLockConfig holds the construction parameters for [New]. The zero
// value of a field is the default its comment names; a count or a length
// the feature selection serves is the attribute's value, which the model
// gives no default.
type DoorLockConfig struct {
	// Source provides live lock state and command dispatch. nil makes the
	// server keep LockState itself and move the lock through Hardware.
	Source StateSource
	// DataVersion is an optional pointer to a [cluster.DataVersionTracker]
	// owned by the model layer. When non-nil, the server uses the caller's
	// tracker for [MatterDataVersion] and [MatterInvoke] bumps so the
	// counter survives server reconstruction across [MatterClusterServers]
	// calls. When nil, an embedded tracker is used (prior behaviour).
	DataVersion *cluster.DataVersionTracker

	// Features is the FeatureMap selection; zero is none, matter.js's
	// DoorLockServer without features. The server accepts
	// UnlockWithTimeout whatever the selection, as matter.js's does.
	Features Feature
	// Hardware moves the lock of a server without Source; nil does
	// nothing (matter.js's default handleLockOperation).
	Hardware LockOperationHandler
	// LockState is the LockState a server without Source starts with; nil
	// is null.
	LockState *lockdef.LockStateEnum
	// LockType is the attribute; zero is DeadBolt.
	LockType lockdef.LockTypeEnum
	// OperatingMode is the mode the lock starts in; a mode
	// SupportedOperatingModes does not support is switched to Normal
	// (DoorLockServer.ts:129-133).
	OperatingMode lockdef.OperatingModeEnum
	// SupportedOperatingModes is the attribute, a set bit marking a mode
	// NOT supported; zero is matter.js's default 0xFFF6 (vacation, privacy
	// and passage not supported). The alwaysSet bits must be set and
	// Normal supported (DoorLockServer.ts:117-128).
	SupportedOperatingModes lockdef.OperatingModesBitmap

	// The User feature.
	NumberOfTotalUsersSupported         uint16
	NumberOfCredentialsSupportedPerUser uint8
	CredentialRulesSupport              lockdef.CredentialRulesBitmap
	// ExpiringUserTimeout is the attribute in minutes ([USR], optional);
	// nil does not serve it, and an ExpiringUser is then denied.
	ExpiringUserTimeout *uint16

	// The PIN and RFID credential features.
	NumberOfPinUsersSupported  uint16
	MaxPinCodeLength           uint8
	MinPinCodeLength           uint8
	NumberOfRfidUsersSupported uint16
	MaxRfidCodeLength          uint8
	MinRfidCodeLength          uint8
	// WrongCodeEntryLimit and UserCodeTemporaryDisableTime (seconds) are
	// the lockout attributes of PIN or RID, both 1 to 255.
	WrongCodeEntryLimit          uint8
	UserCodeTemporaryDisableTime uint8
	// RequirePinForRemoteOperation is the attribute of COTA and PIN.
	RequirePinForRemoteOperation bool

	// The schedule features.
	NumberOfWeekDaySchedulesSupportedPerUser uint8
	NumberOfYearDaySchedulesSupportedPerUser uint8
	NumberOfHolidaySchedulesSupported        uint8

	// AutoRelockTime is the attribute in seconds (optional); nil does not
	// serve it.
	AutoRelockTime *uint32
	// DoorState is the DoorState a DPS lock starts with; nil is null.
	DoorState *lockdef.DoorStateEnum

	// Initial holds the users, credentials and schedules a lock starts
	// with when its AuthStore holds none for it, or without an AuthStore:
	// matter.js's state given at endpoint creation. Credential data is in
	// clear here; the server encrypts it.
	Initial store.DoorLockAuth
	// Auth persists the records; nil keeps them in memory. AuthKey names
	// this lock's records in it.
	Auth    AuthStore
	AuthKey string
	// Cipher encrypts the stored credential data; nil is AES-128-GCM under
	// the node's credential key ([store.SettingDoorLockCredentialKey], a
	// key in memory without Auth). matter.js DoorLockServer.cipher
	// (:245-250), which a server overrides for an HSM.
	Cipher Cipher

	// Clock runs the auto-relock, lockout and expiry timers; nil is
	// [clock.System].
	Clock clock.Clock
	// Location is the time zone schedules are read in; nil is time.Local,
	// the zone matter.js's localInstant reads a Date in
	// (LockSchedule.ts:97-112).
	Location *time.Location
}

// DoorLockServer implements [contract.ClusterServer] for the DoorLock
// cluster (0x0101) through the generated [spec.Server] it embeds: reads,
// writes and their checks, the element lists, the data version and the
// change notification. It adds matter.js's DoorLockServer behaviour.
//
// Locking. op is matter.js's behaviour lock: every command and every
// timer action runs under it, so lock operations never overlap
// (DoorLockServer.ts:221-222). mu guards the records and the counters for
// the accessors; events leave the server with mu released.
type DoorLockServer struct {
	*spec.Server

	src      StateSource
	hardware LockOperationHandler
	tracker  *cluster.DataVersionTracker
	clock    clock.Clock
	loc      *time.Location
	auth     AuthStore
	authKey  string
	cipher   Cipher

	usr, pin, rid, fgp, cota, wdsch, ydsch, hdsch, dps bool

	supportedModes lockdef.OperatingModesBitmap

	// op serialises lock operations end to end — the hardware move
	// included, as matter.js runs them under `{ lock: true }` — and is
	// taken before mu.
	op sync.Mutex

	// mu guards the fields below.
	mu       sync.Mutex
	endpoint uint16
	data     store.DoorLockAuth
	dirty    bool
	queue    []queuedEvent
	// base is the context of the timer actions, which no request carries.
	base context.Context

	// Internal (DoorLockServer.ts:1576-1583).
	wrongCodeCount int
	lockoutEndsAt  time.Time
	autoRelock     *serverTimer
	expiryTimers   map[uint16]*serverTimer
	// actions counts the timer actions running on goroutines of their own
	// (goTimerAction); idle, on mu, signals it reached zero, which
	// MatterQuiesce waits for.
	actions int
	idle    sync.Cond
}

// queuedEvent is an event decided under mu and emitted after it.
type queuedEvent struct {
	id      uint32
	payload any
}

// serverTimer is one armed timer; its identity tells a superseded run to
// do nothing (DoorLockServer.ts:969-972). stopped, guarded by the
// server's mu, tells a callback that fired as the timer was stopped not
// to start its action.
type serverTimer struct {
	t       clock.Timer
	stopped bool
}

// stop stops the timer; the caller holds the server's mu.
func (t *serverTimer) stop() {
	if t != nil {
		t.stopped = true
		if t.t != nil {
			t.t.Stop()
		}
	}
}

// goTimerAction runs fn, the action of t, on a goroutine the server owns
// — fn takes op, which a lock operation may hold, so the clock's callback
// does not wait for it — unless t was stopped meanwhile. MatterQuiesce
// waits for the running actions.
func (s *DoorLockServer) goTimerAction(t *serverTimer, fn func()) {
	s.mu.Lock()
	if t.stopped {
		s.mu.Unlock()
		return
	}
	s.actions++
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			s.actions--
			if s.actions == 0 {
				s.idle.Broadcast()
			}
			s.mu.Unlock()
		}()
		fn()
	}()
}

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*DoorLockServer)(nil)
	_ contract.ClusterDataVersion     = (*DoorLockServer)(nil)
	_ contract.ClusterAttributeLister = (*DoorLockServer)(nil)
	_ contract.ClusterCommandLister   = (*DoorLockServer)(nil)
	_ contract.ClusterEventLister     = (*DoorLockServer)(nil)
	_ contract.EventReceiver          = (*DoorLockServer)(nil)
	_ contract.ClusterQuiescer        = (*DoorLockServer)(nil)
)

// NewDoorLockServer is [New] for a configuration it cannot refuse — the
// former server's: a Source, an optional DataVersion and no feature
// selection. It panics on a configuration New refuses.
func NewDoorLockServer(cfg DoorLockConfig) *DoorLockServer {
	s, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return s
}

// New builds a DoorLock server. The feature selection is held to the
// definition's conformance and to the features this server serves, each
// attribute to its model, SupportedOperatingModes to matter.js's rules.
// The records come from the AuthStore when it saved some for AuthKey,
// from Initial otherwise; an ExpiringUser's pending deadline is armed
// again. Mirrors matter.js DoorLockServer.ts:114-180 (initialize).
func New(cfg DoorLockConfig) (*DoorLockServer, error) {
	features := cfg.Features
	if features&^servedFeatures != 0 {
		return nil, fmt.Errorf("%w: features 0x%X are not served", ErrConfig, uint32(features&^servedFeatures))
	}
	s := &DoorLockServer{
		src:          cfg.Source,
		hardware:     cfg.Hardware,
		tracker:      cfg.DataVersion,
		clock:        clock.Or(cfg.Clock),
		loc:          cfg.Location,
		auth:         cfg.Auth,
		authKey:      cfg.AuthKey,
		cipher:       cfg.Cipher,
		usr:          features&FeatureUSR != 0,
		pin:          features&FeaturePIN != 0,
		rid:          features&FeatureRID != 0,
		fgp:          features&FeatureFGP != 0,
		cota:         features&FeatureCOTA != 0,
		wdsch:        features&FeatureWDSCH != 0,
		ydsch:        features&FeatureYDSCH != 0,
		hdsch:        features&FeatureHDSCH != 0,
		dps:          features&FeatureDPS != 0,
		expiryTimers: map[uint16]*serverTimer{},
		base:         context.Background(),
	}
	s.idle.L = &s.mu
	if s.tracker == nil {
		s.tracker = &cluster.DataVersionTracker{}
	}
	if s.loc == nil {
		s.loc = time.Local
	}

	supported := cfg.SupportedOperatingModes
	switch {
	case supported == 0:
		supported = defaultSupportedOperatingModes
	case supported&lockdef.OperatingModesAlwaysSet != lockdef.OperatingModesAlwaysSet:
		// matter.js DoorLockServer.ts:120-125.
		return nil, fmt.Errorf("%w: the alwaysSet bits of SupportedOperatingModes must be set (the bits are inverted: a set bit marks a mode not supported)", ErrConfig)
	}
	if !modeSupported(supported, lockdef.OperatingModeNormal) {
		// matter.js DoorLockServer.ts:126-128.
		return nil, fmt.Errorf("%w: %s", ErrConfig, unsupportedOperatingMode(lockdef.OperatingModeNormal))
	}
	s.supportedModes = supported
	mode := cfg.OperatingMode
	if !modeSupported(supported, mode) {
		// A controller may have stored a mode a later SupportedOperatingModes
		// no longer supports (DoorLockServer.ts:129-133).
		mode = lockdef.OperatingModeNormal
	}

	opts := spec.Options{Features: uint32(features), Commands: []uint32{lockdef.CmdUnlockWithTimeout}}
	initial := s.initialAttributes(cfg, mode, supported, &opts)
	srvCfg := spec.ServerConfig{DataVersion: s.tracker, Initial: initial, Sink: s, Handlers: s.handlers()}
	if s.src != nil {
		srvCfg.Source = sourceAdapter{s.src}
	}
	srv, err := spec.NewServer(lockdef.Definition, opts, srvCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	s.Server = srv

	if err := s.loadRecords(s.base, cfg.Initial); err != nil {
		return nil, err
	}
	// Re-arm the deadline of every ExpiringUser that is still running
	// (DoorLockServer.ts:166-176).
	for _, u := range s.data.Users {
		if lockdef.UserTypeEnum(u.UserType) == lockdef.UserTypeExpiringUser &&
			lockdef.UserStatusEnum(u.UserStatus) != lockdef.UserStatusOccupiedDisabled &&
			u.ExpiringUserExpiresAt != nil {
			s.armExpiryTimer(u.UserIndex, *u.ExpiringUserExpiresAt)
		}
	}
	return s, nil
}

// initialAttributes are the values the server starts with, and the
// optional attributes the configuration serves.
func (s *DoorLockServer) initialAttributes(cfg DoorLockConfig, mode lockdef.OperatingModeEnum,
	supported lockdef.OperatingModesBitmap, opts *spec.Options,
) map[uint32]any {
	initial := map[uint32]any{
		lockdef.AttrLockType:                uint8(cfg.LockType),
		lockdef.AttrActuatorEnabled:         true,
		lockdef.AttrOperatingMode:           uint8(mode),
		lockdef.AttrSupportedOperatingModes: uint16(supported),
	}
	if s.src == nil {
		initial[lockdef.AttrLockState] = nullableEnum(cfg.LockState)
	}
	if cfg.AutoRelockTime != nil {
		opts.Attributes = append(opts.Attributes, lockdef.AttrAutoRelockTime)
		initial[lockdef.AttrAutoRelockTime] = *cfg.AutoRelockTime
	}
	if s.usr {
		initial[lockdef.AttrNumberOfTotalUsersSupported] = cfg.NumberOfTotalUsersSupported
		initial[lockdef.AttrNumberOfCredentialsSupportedPerUser] = cfg.NumberOfCredentialsSupportedPerUser
		initial[lockdef.AttrCredentialRulesSupport] = uint8(cfg.CredentialRulesSupport)
		if cfg.ExpiringUserTimeout != nil {
			opts.Attributes = append(opts.Attributes, lockdef.AttrExpiringUserTimeout)
			initial[lockdef.AttrExpiringUserTimeout] = *cfg.ExpiringUserTimeout
		}
	}
	if s.pin {
		initial[lockdef.AttrNumberOfPinUsersSupported] = cfg.NumberOfPinUsersSupported
		initial[lockdef.AttrMaxPinCodeLength] = cfg.MaxPinCodeLength
		initial[lockdef.AttrMinPinCodeLength] = cfg.MinPinCodeLength
	}
	if s.rid {
		initial[lockdef.AttrNumberOfRfidUsersSupported] = cfg.NumberOfRfidUsersSupported
		initial[lockdef.AttrMaxRfidCodeLength] = cfg.MaxRfidCodeLength
		initial[lockdef.AttrMinRfidCodeLength] = cfg.MinRfidCodeLength
	}
	if s.pin || s.rid {
		initial[lockdef.AttrWrongCodeEntryLimit] = cfg.WrongCodeEntryLimit
		initial[lockdef.AttrUserCodeTemporaryDisableTime] = cfg.UserCodeTemporaryDisableTime
	}
	if s.cota && s.pin {
		initial[lockdef.AttrRequirePinForRemoteOperation] = cfg.RequirePinForRemoteOperation
	}
	if s.wdsch {
		initial[lockdef.AttrNumberOfWeekDaySchedulesSupportedPerUser] = cfg.NumberOfWeekDaySchedulesSupportedPerUser
	}
	if s.ydsch {
		initial[lockdef.AttrNumberOfYearDaySchedulesSupportedPerUser] = cfg.NumberOfYearDaySchedulesSupportedPerUser
	}
	if s.hdsch {
		initial[lockdef.AttrNumberOfHolidaySchedulesSupported] = cfg.NumberOfHolidaySchedulesSupported
	}
	if s.dps {
		initial[lockdef.AttrDoorState] = nullableEnum(cfg.DoorState)
	}
	return initial
}

// loadRecords reads the credential key and the records: from the
// AuthStore when it saved some, else from initial with its credentials
// encrypted (DoorLockServer.ts:153-164).
func (s *DoorLockServer) loadRecords(ctx context.Context, initial store.DoorLockAuth) error {
	if s.cipher == nil {
		key, err := s.credentialKey(ctx)
		if err != nil {
			return err
		}
		if s.cipher, err = NewCredentialCipher(key); err != nil {
			return err
		}
	}
	if s.auth != nil {
		saved, ok, err := s.auth.LoadDoorLockAuth(ctx, s.authKey)
		if err != nil {
			return fmt.Errorf("lock: load records: %w", err)
		}
		if ok {
			s.data = saved
			return nil
		}
	}
	data := cloneAuth(initial)
	for i := range data.Credentials {
		enc, err := s.cipher.Encrypt(data.Credentials[i].Data)
		if err != nil {
			return fmt.Errorf("lock: encrypt an initial credential: %w", err)
		}
		data.Credentials[i].Data = enc
	}
	s.data = data
	return nil
}

// credentialKey returns the node's credential key, generating and storing
// it on first use; without an AuthStore it is a key of this server alone.
// matter.js DoorLockServer.ts:153-155 generates 16 random bytes once.
func (s *DoorLockServer) credentialKey(ctx context.Context) ([]byte, error) {
	if s.auth != nil {
		v, ok, err := s.auth.GetSetting(ctx, store.SettingDoorLockCredentialKey)
		if err != nil {
			return nil, fmt.Errorf("lock: read the credential key: %w", err)
		}
		if ok {
			key, err := hex.DecodeString(v)
			if err != nil || len(key) != credentialKeySize {
				return nil, fmt.Errorf("%w: the stored credential key is not %d hex-encoded bytes", ErrConfig, credentialKeySize)
			}
			return key, nil
		}
	}
	key := make([]byte, credentialKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("lock: generate the credential key: %w", err)
	}
	if s.auth != nil {
		if err := s.auth.SetSetting(ctx, store.SettingDoorLockCredentialKey, hex.EncodeToString(key)); err != nil {
			return nil, fmt.Errorf("lock: store the credential key: %w", err)
		}
	}
	return key, nil
}

// sourceAdapter answers LockState from a [StateSource]: NotFullyLocked
// while jammed, null while the state is unknown.
type sourceAdapter struct{ src StateSource }

func (a sourceAdapter) MatterAttribute(attrID uint32) (any, bool) {
	if attrID != lockdef.AttrLockState {
		return nil, false
	}
	if a.src.IsJammed() {
		return uint8(lockdef.LockStateNotFullyLocked), true
	}
	locked, observed := a.src.IsLocked()
	switch {
	case !observed:
		return nil, true
	case locked:
		return uint8(lockdef.LockStateLocked), true
	default:
		return uint8(lockdef.LockStateUnlocked), true
	}
}

// handlers are the command handlers, by request command id.
func (s *DoorLockServer) handlers() map[uint32]spec.CommandHandler {
	return map[uint32]spec.CommandHandler{
		lockdef.CmdLockDoor:             s.lockCommand(ActuationLock),
		lockdef.CmdUnlockDoor:           s.lockCommand(ActuationUnlock),
		lockdef.CmdUnlockWithTimeout:    s.lockCommand(ActuationUnlock),
		lockdef.CmdUnboltDoor:           s.lockCommand(ActuationUnbolt),
		lockdef.CmdSetUser:              s.handleSetUser,
		lockdef.CmdGetUser:              s.handleGetUser,
		lockdef.CmdClearUser:            s.handleClearUser,
		lockdef.CmdSetCredential:        s.handleSetCredential,
		lockdef.CmdGetCredentialStatus:  s.handleGetCredentialStatus,
		lockdef.CmdClearCredential:      s.handleClearCredential,
		lockdef.CmdSetWeekDaySchedule:   s.handleSetWeekDaySchedule,
		lockdef.CmdGetWeekDaySchedule:   s.handleGetWeekDaySchedule,
		lockdef.CmdClearWeekDaySchedule: s.handleClearWeekDaySchedule,
		lockdef.CmdSetYearDaySchedule:   s.handleSetYearDaySchedule,
		lockdef.CmdGetYearDaySchedule:   s.handleGetYearDaySchedule,
		lockdef.CmdClearYearDaySchedule: s.handleClearYearDaySchedule,
		lockdef.CmdSetHolidaySchedule:   s.handleSetHolidaySchedule,
		lockdef.CmdGetHolidaySchedule:   s.handleGetHolidaySchedule,
		lockdef.CmdClearHolidaySchedule: s.handleClearHolidaySchedule,
	}
}

// MatterClusterID returns 0x0101 (DoorLock).
func (*DoorLockServer) MatterClusterID() uint32 { return lockdef.ClusterID }

// MatterInvoke implements [contract.ClusterServer]: an accepted command
// goes to its handler, an unknown one is refused.
func (s *DoorLockServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if !s.Accepts(cmdID) {
		return nil, spec.Errorf(im.StatusUnsupportedCommand, "%w: 0x%02X", errUnknownCommand, cmdID)
	}
	return s.Server.MatterInvoke(ctx, cmdID, fields)
}

// MatterWriteAttribute implements [spec.Sink]: a write of an
// OperatingMode SupportedOperatingModes does not support is
// CONSTRAINT_ERROR. Mirrors matter.js DoorLockServer.ts:1197-1201
// (#assertOperatingModeSupported, the operatingMode$Changing reactor :134).
func (s *DoorLockServer) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	if attrID != lockdef.AttrOperatingMode {
		return nil
	}
	m, _ := value.(uint8)
	if !modeSupported(s.supportedModes, lockdef.OperatingModeEnum(m)) {
		return spec.Errorf(im.StatusConstraintError, "lock: %s", unsupportedOperatingMode(lockdef.OperatingModeEnum(m)))
	}
	return nil
}

// MatterReportable returns the attribute IDs that change on wire events.
func (*DoorLockServer) MatterReportable() []uint32 {
	return []uint32{wire.DoorLockAttrLockState}
}

// SetMatterEventEmitter implements [contract.EventReceiver].
// Called by the bridge during topology assembly so the server can fire
// its events without holding a bridge reference. Idempotent — re-wiring
// during topology rebuild replaces the emitter.
func (s *DoorLockServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.Server.SetMatterEventEmitter(emitter)
}

// SetEndpoint stamps the endpoint id this DoorLock server is mounted
// on. Matter events carry the (endpoint, cluster, event) triple; the
// endpoint is captured at assembly time because the server is built by
// the model layer before the bridge assigns endpoint ids.
func (s *DoorLockServer) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// MatterQuiesce implements [contract.ClusterQuiescer]: the auto-relock
// and expiry timers stop, as matter.js stops them on dispose
// (DoorLockServer.ts:182-189). A later lock command arms auto-relock
// again; an ExpiringUser's deadline stays stored and is checked on its
// next use. A timer action already running (an auto-relock moving the
// hardware) finishes first: MatterQuiesce returns once the cancelled timer
// action has returned. The wait has no bound of its own — the interface
// carries no context — and is bounded by the action, whose hardware call
// the host keeps short.
func (s *DoorLockServer) MatterQuiesce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopAutoRelockLocked()
	for idx, t := range s.expiryTimers {
		t.stop()
		delete(s.expiryTimers, idx)
	}
	for s.actions > 0 {
		s.idle.Wait() // releases mu: the action takes op, then mu
	}
}

// Users returns a copy of the user database, matter.js state.users.
func (s *DoorLockServer) Users() []store.DoorLockUser {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAuth(store.DoorLockAuth{Users: s.data.Users}).Users
}

// User returns the user at index, matter.js auth.findUser.
func (s *DoorLockServer) User(index uint16) (store.DoorLockUser, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.findUser(index); u != nil {
		return cloneUser(*u), true
	}
	return store.DoorLockUser{}, false
}

// SetLockState records a lock state the lock reached by itself (a server
// without Source): matter.js's application writing state.lockState.
func (s *DoorLockServer) SetLockState(state lockdef.LockStateEnum) error {
	return s.Set(lockdef.AttrLockState, uint8(state))
}

// SetDoorState records the door's state (DPS) and emits DoorStateChange;
// DoorOpen and DoorClosed count in DoorOpenEvents and DoorClosedEvents
// when they are served. Mirrors matter.js DoorLockServer.ts:1345-1361
// (#handleDoorStateChange, the doorState$Changed reactor :179).
func (s *DoorLockServer) SetDoorState(state lockdef.DoorStateEnum) error {
	if err := s.Set(lockdef.AttrDoorState, uint8(state)); err != nil {
		return err
	}
	s.emit([]queuedEvent{{lockdef.EventDoorStateChange, lockdef.DoorStateChangeEvent{DoorState: state}}})
	var counter uint32
	switch state {
	case lockdef.DoorStateDoorOpen:
		counter = lockdef.AttrDoorOpenEvents
	case lockdef.DoorStateDoorClosed:
		counter = lockdef.AttrDoorClosedEvents
	default:
		return nil
	}
	if v, ok := s.Value(counter); ok {
		n, _ := v.(uint32)
		return s.Set(counter, n+1)
	}
	return nil
}

// ReportAlarm emits DoorLockAlarm with code, the way a lock reports a jam
// (DoorLockServer.ts:228).
func (s *DoorLockServer) ReportAlarm(code lockdef.AlarmCodeEnum) {
	s.emit([]queuedEvent{{lockdef.EventDoorLockAlarm, lockdef.DoorLockAlarmEvent{AlarmCode: code}}})
}

// ── Core lock operations ────────────────────────────────────────────────

// lockCommand is LockDoor, UnlockDoor, UnlockWithTimeout or UnboltDoor:
// matter.js DoorLockServer.ts:193-216. The lock moves (#actuate), then a
// lock stops auto-relock and an unlock or unbolt arms it — with the
// request's timeout for UnlockWithTimeout.
func (s *DoorLockServer) lockCommand(actuation Actuation) spec.CommandHandler {
	return func(ctx context.Context, fields any) (any, error) {
		pinCode, timeout, err := lockRequest(fields)
		if err != nil {
			return nil, err
		}
		// PINCode is a field of COTA and PIN only ("[COTA & PIN]").
		if !s.cota || !s.pin {
			pinCode = nil
		}
		s.op.Lock()
		defer s.op.Unlock()
		if err := s.actuate(ctx, actuation, pinCode); err != nil {
			return nil, err
		}
		s.mu.Lock()
		if actuation == ActuationLock {
			s.stopAutoRelockLocked()
		} else {
			s.scheduleAutoRelockLocked(timeout)
		}
		s.mu.Unlock()
		return nil, nil
	}
}

// lockRequest reads the PINCode and the UnlockWithTimeout timeout of a
// lock command; a command without fields carries neither.
func lockRequest(fields any) (pinCode *[]byte, timeout *uint16, err error) {
	switch r := fields.(type) {
	case nil:
	case lockdef.LockDoorRequest:
		pinCode = r.PinCode
	case lockdef.UnlockDoorRequest:
		pinCode = r.PinCode
	case lockdef.UnboltDoorRequest:
		pinCode = r.PinCode
	case lockdef.UnlockWithTimeoutRequest:
		pinCode, timeout = r.PinCode, &r.Timeout
	case *lockdef.LockDoorRequest:
		pinCode = r.PinCode
	case *lockdef.UnlockDoorRequest:
		pinCode = r.PinCode
	case *lockdef.UnboltDoorRequest:
		pinCode = r.PinCode
	case *lockdef.UnlockWithTimeoutRequest:
		pinCode, timeout = r.PinCode, &r.Timeout
	default:
		return nil, nil, spec.Errorf(im.StatusInvalidCommand, "lock: lock command fields %T", fields)
	}
	return pinCode, timeout, nil
}

// requester is who asks for a lock operation, as far as the credential
// identifies them: matter.js LockRequester (DoorLockServer.ts:59-65).
type requester struct {
	user        *store.DoorLockUser
	credentials []lockdef.CredentialStruct
}

// anonymous is ANONYMOUS (DoorLockServer.ts:65).
var anonymous = requester{}

func (r requester) userIndex() *uint16 {
	if r.user == nil {
		return nil
	}
	idx := r.user.UserIndex
	return &idx
}

// actuate mirrors matter.js DoorLockServer.ts:1015-1042 (#actuate); the
// caller holds op.
func (s *DoorLockServer) actuate(ctx context.Context, actuation Actuation, pinCode *[]byte) error {
	// Events of a lock operation name the requesting fabric; without one
	// the hardware must not move (:1016-1022).
	if _, fabricIndex := im.FabricFilterFromContext(ctx); fabricIndex == 0 {
		return spec.Errorf(im.StatusUnsupportedAccess, "lock: lock commands need the fabric of a remote requester")
	}
	// UnboltDoor retracts the bolt without pulling the latch: the lock ends
	// up Unlocked and reports an Unlock (§ 5.2.10.25, § 5.2.11.3;
	// :1024-1025).
	locking := actuation == ActuationLock
	opType := lockdef.LockOperationTypeUnlock
	if locking {
		opType = lockdef.LockOperationTypeLock
	}

	s.mu.Lock()
	req, err := s.authorizeLocked(ctx, opType, pinCode)
	events := s.takeQueueLocked()
	s.mu.Unlock()
	s.emit(events)
	if err != nil {
		return err
	}

	op := LockOperation{Actuation: actuation, Source: lockdef.OperationSourceRemote, UserIndex: req.userIndex()}
	if err := s.moveHardware(ctx, op); err != nil {
		s.mu.Lock()
		s.emitLockOperationErrorLocked(ctx, opType, hardwareFailureReason(err), req)
		events := s.takeQueueLocked()
		s.mu.Unlock()
		s.emit(events)
		var sc im.StatusCodeError
		if errors.As(err, &sc) {
			return err
		}
		return spec.Errorf(im.StatusFailure, "lock: lock hardware failed to %s: %v", actuation, err)
	}

	if err := s.setActuated(locking); err != nil {
		return err
	}
	return s.commit(ctx, func() error {
		s.completeOperationLocked(ctx, opType, req)
		return nil
	})
}

// moveHardware runs one lock operation on the hardware: the configured
// handler, the source's, or the source's LockInvoke.
func (s *DoorLockServer) moveHardware(ctx context.Context, op LockOperation) error {
	if s.hardware != nil {
		return s.hardware.HandleLockOperation(ctx, op)
	}
	if s.src == nil {
		return nil
	}
	if h, ok := s.src.(LockOperationHandler); ok {
		return h.HandleLockOperation(ctx, op)
	}
	cmd := lockdef.CmdUnlockDoor
	switch op.Actuation {
	case ActuationLock:
		cmd = lockdef.CmdLockDoor
	case ActuationUnbolt:
		cmd = lockdef.CmdUnboltDoor
	case ActuationUnlock:
	}
	return s.src.LockInvoke(ctx, cmd)
}

// setActuated records the lock state an operation reached: a server
// without Source sets LockState (:1040); one with a Source bumps the data
// version and reports LockState, which the host's lock now answers.
func (s *DoorLockServer) setActuated(locked bool) error {
	if s.src != nil {
		s.tracker.Bump()
		s.NotifyFrom(contract.OriginCommand, lockdef.AttrLockState)
		return nil
	}
	state := lockdef.LockStateUnlocked
	if locked {
		state = lockdef.LockStateLocked
	}
	return s.Set(lockdef.AttrLockState, uint8(state))
}

// isLocked reports whether LockState is Locked.
func (s *DoorLockServer) isLocked() bool {
	v, _ := s.MatterRead(lockdef.AttrLockState)
	state, ok := v.(uint8)
	return ok && lockdef.LockStateEnum(state) == lockdef.LockStateLocked
}

// ── Auto-relock (§ 5.2.9.22) ────────────────────────────────────────────

// stopAutoRelockLocked mirrors matter.js DoorLockServer.ts:951-954.
func (s *DoorLockServer) stopAutoRelockLocked() {
	s.autoRelock.stop()
	s.autoRelock = nil
}

// scheduleAutoRelockLocked mirrors matter.js DoorLockServer.ts:956-967:
// the timeout of UnlockWithTimeout, else AutoRelockTime; none or zero
// arms nothing.
func (s *DoorLockServer) scheduleAutoRelockLocked(timeoutSeconds *uint16) {
	s.stopAutoRelockLocked()
	var seconds uint32
	switch {
	case timeoutSeconds != nil:
		seconds = uint32(*timeoutSeconds)
	default:
		v, ok := s.Value(lockdef.AttrAutoRelockTime)
		if !ok {
			return
		}
		seconds, _ = v.(uint32)
	}
	if seconds == 0 {
		return
	}
	t := &serverTimer{}
	// The action takes op, which a lock operation may hold: it runs in a
	// goroutine of its own, as matter.js's callback waits for the lock
	// (:167, `{ lock: true }`).
	t.t = s.clock.AfterFunc(time.Duration(seconds)*time.Second, func() { s.goTimerAction(t, func() { s.runAutoRelock(t) }) })
	s.autoRelock = t
}

// runAutoRelock mirrors matter.js DoorLockServer.ts:969-1011 (#autoRelock).
func (s *DoorLockServer) runAutoRelock(t *serverTimer) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	// A lock operation that ran while this relock waited for the lock
	// replaced or stopped its timer (:970-972).
	if t != s.autoRelock || s.isLocked() {
		s.mu.Unlock()
		return
	}
	s.autoRelock = nil
	s.mu.Unlock()

	ctx := s.base
	op := LockOperation{Actuation: ActuationLock, Source: lockdef.OperationSourceAuto}
	if err := s.moveHardware(ctx, op); err != nil {
		s.emitAuto(lockdef.EventLockOperationError, hardwareFailureReason(err))
		return
	}
	if s.setActuated(true) != nil {
		return
	}
	s.emitAuto(lockdef.EventLockOperation, 0)
}

// emitAuto emits the LockOperation or LockOperationError of an
// auto-relock: Lock, source Auto, every requester field null
// (DoorLockServer.ts:984-1010).
func (s *DoorLockServer) emitAuto(eventID uint32, operationError lockdef.OperationErrorEnum) {
	var payload any
	if eventID == lockdef.EventLockOperationError {
		ev := lockdef.LockOperationErrorEvent{
			LockOperationType: lockdef.LockOperationTypeLock,
			OperationSource:   lockdef.OperationSourceAuto,
			OperationError:    operationError,
			UserIndex:         spec.NullOf[uint16](),
			FabricIndex:       spec.NullOf[uint8](),
			SourceNode:        spec.NullOf[uint64](),
		}
		if s.usr {
			ev.Credentials = nullCredentials()
		}
		payload = ev
	} else {
		payload = s.lockOperationPayload(lockdef.LockOperationTypeLock, lockdef.OperationSourceAuto, nil, 0, nil, nil, false)
	}
	s.emit([]queuedEvent{{eventID, payload}})
}

// ── Authorization ───────────────────────────────────────────────────────

// authorizeLocked is the single decision every remote lock operation
// passes before the lock moves. Mirrors matter.js DoorLockServer.ts:1049-1080
// (#authorize); the caller holds op and mu.
func (s *DoorLockServer) authorizeLocked(ctx context.Context, opType lockdef.LockOperationTypeEnum, pinCode *[]byte) (requester, error) {
	mode := s.operatingMode()
	if mode == lockdef.OperatingModePrivacy || mode == lockdef.OperatingModeNoRemoteLockUnlock {
		s.emitLockOperationErrorLocked(ctx, opType, lockdef.OperationErrorUnspecified, anonymous)
		return anonymous, spec.Errorf(im.StatusFailure, "lock: remote lock operations are disabled in operating mode %d", mode)
	}
	if s.isLockedOutLocked() {
		return anonymous, spec.Errorf(im.StatusFailure, "lock: lock operations are ignored during the wrong code lockout")
	}
	if pinCode == nil {
		if s.requirePin() {
			return anonymous, s.refuseWrongCodeLocked(ctx, opType, "PIN required for remote operation")
		}
		return anonymous, nil
	}
	req, ok := s.identifyPinLocked(*pinCode)
	if !ok {
		return anonymous, s.refuseWrongCodeLocked(ctx, opType, "invalid PIN code")
	}
	if req.user != nil {
		if err := s.authorizeUserLocked(ctx, opType, req); err != nil {
			return anonymous, err
		}
	}
	return req, nil
}

// authorizeUserLocked mirrors matter.js DoorLockServer.ts:1082-1119
// (#authorizeUser).
func (s *DoorLockServer) authorizeUserLocked(ctx context.Context, opType lockdef.LockOperationTypeEnum, req requester) error {
	user := req.user
	expired := s.hasExpired(user)
	status := lockdef.UserStatusEnum(user.UserStatus)
	if expired && status != lockdef.UserStatusOccupiedDisabled {
		s.armExpiryTimer(user.UserIndex, *user.ExpiringUserExpiresAt)
	}
	if status == lockdef.UserStatusOccupiedDisabled || expired {
		s.emitLockOperationErrorLocked(ctx, opType, lockdef.OperationErrorDisabledUserDenied, req)
		return spec.Errorf(im.StatusFailure, "lock: user %d is disabled", user.UserIndex)
	}
	userType := lockdef.UserTypeEnum(user.UserType)
	if !IsAccessGranted(userType, user.UserIndex, s.data.WeekDaySchedules, s.data.YearDaySchedules, LocalInstantOf(s.clock.Now().In(s.loc))) {
		s.emitLockOperationErrorLocked(ctx, opType, lockdef.OperationErrorRestricted, req)
		return spec.Errorf(im.StatusFailure, "lock: access denied by schedule")
	}
	if userType == lockdef.UserTypeExpiringUser && !s.Serves(lockdef.AttrExpiringUserTimeout) {
		s.emitLockOperationErrorLocked(ctx, opType, lockdef.OperationErrorRestricted, req)
		return spec.Errorf(im.StatusFailure, "lock: an ExpiringUser cannot be granted access without ExpiringUserTimeout")
	}
	if userType == lockdef.UserTypeNonAccessUser {
		s.emitLockOperationLocked(ctx, lockdef.LockOperationTypeNonAccessUserEvent, req)
		return spec.Errorf(im.StatusFailure, "lock: a NonAccessUser cannot operate the lock")
	}
	return nil
}

// completeOperationLocked mirrors matter.js DoorLockServer.ts:1121-1143
// (#completeOperation).
func (s *DoorLockServer) completeOperationLocked(ctx context.Context, opType lockdef.LockOperationTypeEnum, req requester) {
	if req.credentials != nil {
		s.wrongCodeCount = 0
	}
	user := req.user
	if user != nil && lockdef.UserTypeEnum(user.UserType) == lockdef.UserTypeForcedUser {
		s.emitLockOperationLocked(ctx, lockdef.LockOperationTypeForcedUserEvent, req)
		s.queue = append(s.queue, queuedEvent{lockdef.EventDoorLockAlarm, lockdef.DoorLockAlarmEvent{AlarmCode: lockdef.AlarmCodeForcedUser}})
	} else {
		s.emitLockOperationLocked(ctx, opType, req)
	}
	if user == nil {
		return
	}
	// A DisposableUser may open the lock once; locking does not use that
	// up (:1138-1141).
	if lockdef.UserTypeEnum(user.UserType) == lockdef.UserTypeDisposableUser && opType == lockdef.LockOperationTypeUnlock {
		s.disableUserLocked(user.UserIndex)
	}
	s.armExpiringUserOnFirstUseLocked(*user)
}

// identifyPinLocked mirrors matter.js DoorLockServer.ts:1145-1167
// (#identifyPin): the first PIN credential whose data matches; one no
// user holds is a wrong code (§ 5.2.4.1, as in CHIP).
func (s *DoorLockServer) identifyPinLocked(pinCode []byte) (requester, bool) {
	for _, c := range s.data.Credentials {
		if lockdef.CredentialTypeEnum(c.Type) != lockdef.CredentialTypePin || !s.matches(c, pinCode) {
			continue
		}
		owner, ok := s.findUserIndexForCredential(c.Type, c.Index)
		if !ok {
			return anonymous, false
		}
		user := s.findUser(owner)
		if user == nil {
			return anonymous, false
		}
		u := cloneUser(*user)
		return requester{user: &u, credentials: []lockdef.CredentialStruct{{CredentialType: lockdef.CredentialTypePin, CredentialIndex: c.Index}}}, true
	}
	return anonymous, false
}

// refuseWrongCodeLocked counts a wrong or missing code and starts the
// lockout once WrongCodeEntryLimit is reached; both lockout attributes
// must be served for either to happen. Mirrors matter.js
// DoorLockServer.ts:1175-1190 (#refuseWrongCode).
func (s *DoorLockServer) refuseWrongCodeLocked(ctx context.Context, opType lockdef.LockOperationTypeEnum, message string) error {
	limit, okLimit := s.uint8Attr(lockdef.AttrWrongCodeEntryLimit)
	disable, okDisable := s.uint8Attr(lockdef.AttrUserCodeTemporaryDisableTime)
	if okLimit && okDisable {
		s.wrongCodeCount++
		if s.wrongCodeCount >= int(limit) {
			s.wrongCodeCount = 0
			s.lockoutEndsAt = s.clock.Now().Add(time.Duration(disable) * time.Second)
			s.queue = append(s.queue, queuedEvent{lockdef.EventDoorLockAlarm, lockdef.DoorLockAlarmEvent{AlarmCode: lockdef.AlarmCodeWrongCodeEntryLimit}})
		}
	}
	s.emitLockOperationErrorLocked(ctx, opType, lockdef.OperationErrorInvalidCredential, anonymous)
	return spec.Errorf(im.StatusFailure, "lock: %s", message)
}

// isLockedOutLocked mirrors matter.js DoorLockServer.ts:1192-1195. The
// deadline is read with the clock's monotonic reading where it has one
// (time.Time comparison), as matter.js measures it on Time.nowUs.
func (s *DoorLockServer) isLockedOutLocked() bool {
	return !s.lockoutEndsAt.IsZero() && s.clock.Now().Before(s.lockoutEndsAt)
}

// operatingMode returns OperatingMode.
func (s *DoorLockServer) operatingMode() lockdef.OperatingModeEnum {
	v, _ := s.uint8Attr(lockdef.AttrOperatingMode)
	return lockdef.OperatingModeEnum(v)
}

// requirePin returns RequirePINforRemoteOperation.
func (s *DoorLockServer) requirePin() bool {
	v, ok := s.Value(lockdef.AttrRequirePinForRemoteOperation)
	b, _ := v.(bool)
	return ok && b
}

// uint8Attr returns a served uint8 attribute.
func (s *DoorLockServer) uint8Attr(attrID uint32) (uint8, bool) {
	v, ok := s.Value(attrID)
	if !ok {
		return 0, false
	}
	n, ok := v.(uint8)
	return n, ok
}

// uint16Attr returns a served uint16 attribute.
func (s *DoorLockServer) uint16Attr(attrID uint32) (uint16, bool) {
	v, ok := s.Value(attrID)
	if !ok {
		return 0, false
	}
	n, ok := v.(uint16)
	return n, ok
}

// modeSupported reports whether SupportedOperatingModes supports mode: a
// set bit marks it as not supported (§ 5.2.9.25). Mirrors matter.js
// DoorLockServer.ts:1600-1603.
func modeSupported(supported lockdef.OperatingModesBitmap, mode lockdef.OperatingModeEnum) bool {
	if mode > lockdef.OperatingModePassage {
		return false
	}
	return supported&(1<<mode) == 0
}

func unsupportedOperatingMode(mode lockdef.OperatingModeEnum) string {
	return fmt.Sprintf("OperatingMode %d is marked unsupported (bit set) in SupportedOperatingModes", mode)
}

// hardwareFailureReason is the reason the error event reports for a
// failed hardware operation: the [LockOperationFailedError]'s, Unspecified
// for any other error. Mirrors matter.js DoorLockServer.ts:1605-1624.
func hardwareFailureReason(err error) lockdef.OperationErrorEnum {
	var failed *LockOperationFailedError
	if errors.As(err, &failed) {
		return failed.Reason
	}
	return lockdef.OperationErrorUnspecified
}

// ── Event emission ──────────────────────────────────────────────────────

// emitLockOperationLocked mirrors matter.js DoorLockServer.ts:1289-1301
// (#emitLockOperation): source Remote, the requester's user and
// credentials, FabricIndex and SourceNode from the session.
func (s *DoorLockServer) emitLockOperationLocked(ctx context.Context, opType lockdef.LockOperationTypeEnum, req requester) {
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	nodeID, _ := im.SubjectFromContext(ctx)
	s.queue = append(s.queue, queuedEvent{
		lockdef.EventLockOperation,
		s.lockOperationPayload(opType, lockdef.OperationSourceRemote, req.userIndex(), fabricIndex, &nodeID, req.credentials, true),
	})
}

// lockOperationPayload is the LockOperation payload: the generated event
// with Credentials for a server with USR, [LockOperationEvent] without.
func (s *DoorLockServer) lockOperationPayload(opType lockdef.LockOperationTypeEnum, source lockdef.OperationSourceEnum,
	userIndex *uint16, fabricIndex uint8, sourceNode *uint64, credentials []lockdef.CredentialStruct, remote bool,
) any {
	var fabric *uint8
	if remote && fabricIndex != 0 {
		fabric = &fabricIndex
	}
	var node *uint64
	if remote && sourceNode != nil && *sourceNode != 0 {
		node = sourceNode
	}
	if !s.usr {
		return LockOperationEvent{
			LockOperationType: uint8(opType), OperationSource: uint8(source),
			UserIndex: userIndex, FabricIndex: fabric, SourceNode: node,
		}
	}
	return lockdef.LockOperationEvent{
		LockOperationType: opType, OperationSource: source,
		UserIndex: nullable(userIndex), FabricIndex: nullable(fabric), SourceNode: nullable(node),
		Credentials: credentialsField(credentials),
	}
}

// emitLockOperationErrorLocked mirrors matter.js DoorLockServer.ts:1303-1320
// (#emitLockOperationError).
func (s *DoorLockServer) emitLockOperationErrorLocked(ctx context.Context, opType lockdef.LockOperationTypeEnum, operationError lockdef.OperationErrorEnum, req requester) {
	ev := lockdef.LockOperationErrorEvent{
		LockOperationType: opType,
		OperationSource:   lockdef.OperationSourceRemote,
		OperationError:    operationError,
		UserIndex:         nullable(req.userIndex()),
		FabricIndex:       spec.NullOf[uint8](),
		SourceNode:        spec.NullOf[uint64](),
	}
	if _, fabricIndex := im.FabricFilterFromContext(ctx); fabricIndex != 0 {
		ev.FabricIndex = spec.ValueOf(fabricIndex)
	}
	if nodeID, _ := im.SubjectFromContext(ctx); nodeID != 0 {
		ev.SourceNode = spec.ValueOf(nodeID)
	}
	if s.usr {
		ev.Credentials = credentialsField(req.credentials)
	}
	s.queue = append(s.queue, queuedEvent{lockdef.EventLockOperationError, ev})
}

// emitLockUserChangeLocked mirrors matter.js DoorLockServer.ts:1322-1341
// (#emitLockUserChange).
func (s *DoorLockServer) emitLockUserChangeLocked(ctx context.Context, dataType lockdef.LockDataTypeEnum, op lockdef.DataOperationTypeEnum, userIndex, dataIndex *uint16) {
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	ev := lockdef.LockUserChangeEvent{
		LockDataType:      dataType,
		DataOperationType: op,
		OperationSource:   lockdef.OperationSourceRemote,
		UserIndex:         nullable(userIndex),
		FabricIndex:       spec.ValueOf(fabricIndex),
		SourceNode:        spec.NullOf[uint64](),
		DataIndex:         nullable(dataIndex),
	}
	if nodeID, _ := im.SubjectFromContext(ctx); nodeID != 0 {
		ev.SourceNode = spec.ValueOf(nodeID)
	}
	s.queue = append(s.queue, queuedEvent{lockdef.EventLockUserChange, ev})
}

// takeQueueLocked hands over the queued events.
func (s *DoorLockServer) takeQueueLocked() []queuedEvent {
	q := s.queue
	s.queue = nil
	return q
}

// emit sends events in order to the bridge, with mu released.
func (s *DoorLockServer) emit(events []queuedEvent) {
	if len(events) == 0 {
		return
	}
	s.mu.Lock()
	endpoint := s.endpoint
	s.mu.Unlock()
	for _, e := range events {
		_ = s.Emit(endpoint, e.id, e.payload) // an event outside the selection's EventList is dropped
	}
}

// commit runs fn under mu as one transaction: the records it changed are
// saved, and when saving fails — or fn does — they are what they were
// before, as a matter.js transaction rolls back; the events fn queued
// leave either way, as matter.js's event observers have already run. The
// caller holds op.
func (s *DoorLockServer) commit(ctx context.Context, fn func() error) error {
	s.mu.Lock()
	before := cloneAuth(s.data)
	err := fn()
	dirty := s.dirty
	s.dirty = false
	if err != nil && dirty {
		s.data = before
		dirty = false
	}
	snapshot := s.data
	if dirty {
		snapshot = cloneAuth(s.data)
	}
	events := s.takeQueueLocked()
	s.mu.Unlock()
	if dirty && s.auth != nil {
		if perr := s.auth.SaveDoorLockAuth(ctx, s.authKey, snapshot); perr != nil {
			s.mu.Lock()
			s.data = before
			s.mu.Unlock()
			s.emit(events)
			return spec.Errorf(im.StatusFailure, "lock: save records: %v", perr)
		}
	}
	s.emit(events)
	return err
}

// nullable turns a pointer into a TLV nullable.
func nullable[T any](p *T) spec.Nullable[T] {
	if p == nil {
		return spec.NullOf[T]()
	}
	return spec.ValueOf(*p)
}

// nullableEnum is the stored form of a nullable enum attribute.
func nullableEnum[T ~uint8](p *T) any {
	if p == nil {
		return nil
	}
	return uint8(*p)
}

// credentialsField is the Credentials field of a USR event: the list, or
// null for none.
func credentialsField(c []lockdef.CredentialStruct) *spec.Nullable[[]lockdef.CredentialStruct] {
	if c == nil {
		return nullCredentials()
	}
	v := spec.ValueOf(c)
	return &v
}

func nullCredentials() *spec.Nullable[[]lockdef.CredentialStruct] {
	v := spec.NullOf[[]lockdef.CredentialStruct]()
	return &v
}

// OnMatterValueChanged implements [contract.ChangeNotifier] over the
// StateSource's: a change the host reports there moves the server's data
// version, then cb runs — the bridge hears a pull server's change through
// it. A StateSource that notifies nothing never calls cb.
func (s *DoorLockServer) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	n, ok := s.src.(contract.ChangeNotifier)
	if !ok || n == nil {
		return func() {}
	}
	return n.OnMatterValueChanged(func() {
		s.tracker.Bump()
		if cb != nil {
			cb()
		}
	})
}
