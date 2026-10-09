// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package lock contains the standalone DoorLock cluster server for the
// Matter bridge. The server wraps a [StateSource] that a host's own lock
// type satisfies, keeping cluster logic separate from the domain model.
//
// The cluster's identity is the generated definition (cluster/spec/
// doorlock, ADR 0013): ids, revision, the enums the server reports, the
// attribute, command and event lists for its one feature (Unbolting), the
// event priorities, the write statuses and checks and the privileges.
// LockDoor, UnlockDoor and UnboltDoor decode through the definition; the
// server reads none of their fields (no PIN credential). The lock-state
// projection, the SupportedOperatingModes rule and the LockOperation event
// are the server's.
package lock

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// DoorLock FeatureMap: only Unbolting (UBOLT, bit 12) is advertised —
// the server serves UnboltDoor and nothing of PIN, RID, USR, DPS or the
// schedules.
const doorLockFeatureUnbolt = uint32(lockdef.FeatureUnbolting)

// instance is the definition bound to the one selection the server
// serves. UBOLT alone satisfies every feature conformance and nothing
// optional is declared, so spec.New cannot refuse it.
var instance, _ = spec.New(lockdef.Definition, spec.Options{Features: doorLockFeatureUnbolt})

// LockStateEnum values the server reports (door-lock-cluster.element.ts).
const (
	lockStateNotFullyLocked = uint8(lockdef.LockStateNotFullyLocked)
	lockStateLocked         = uint8(lockdef.LockStateLocked)
	lockStateUnlocked       = uint8(lockdef.LockStateUnlocked)
)

// LockType is DeadBolt.
const lockTypeDeadBolt = uint8(lockdef.LockTypeDeadBolt)

// LockOperationTypeEnum values of the LockOperation event: Lock, Unlock,
// Unlatch.
const (
	lockOperationTypeLock    = uint8(lockdef.LockOperationTypeLock)
	lockOperationTypeUnlock  = uint8(lockdef.LockOperationTypeUnlock)
	lockOperationTypeUnlatch = uint8(lockdef.LockOperationTypeUnlatch)
)

// operationSourceRemote is the OperationSourceEnum value for a
// controller-driven operation (Remote, the only conformance-M source).
const operationSourceRemote = uint8(lockdef.OperationSourceRemote)

// LockOperationEvent is the cluster-native payload for the DoorLock
// LockOperation event (id 0x02, priority critical) per Matter §5.2.10.3.
// Field set mirrors matter.js door-lock-cluster.element.ts:181-195;
// UserIndex, FabricIndex and SourceNode are nullable (nil pointer →
// TLV null). The Credentials field ([USR]-gated) is omitted entirely —
// this projection advertises no USR feature.
//
//nolint:revive // LockOperationEvent mirrors the Matter event name verbatim.
type LockOperationEvent struct {
	LockOperationType uint8
	OperationSource   uint8
	UserIndex         *uint16
	FabricIndex       *uint8
	SourceNode        *uint64
}

var errUnknownCommand = errors.New("doorlock: unknown command")

// StateSource is the read-side interface a model-layer lock DP must
// satisfy so DoorLockServer can project its Matter attribute surface.
//
// IsJammed returns true when ERROR_JAMMED is asserted.
// IsLocked returns (locked=true, observed=true) when the lock state is known
// and the lock is in the locked position.
// LockInvoke dispatches LockDoor / UnlockDoor / UnboltDoor commands to the
// CCU. cmdID is one of the [wire.DoorLockCmd*] constants. The
// implementation owns the southbound urgency of the write it performs:
// the cluster contract carries no priority, so a host whose command
// queue ranks by urgency names the value it wants inside LockInvoke.
type StateSource interface {
	IsJammed() bool
	IsLocked() (locked, observed bool)
	LockInvoke(ctx context.Context, cmdID uint32) error
}

// DoorLockConfig holds the construction parameters for [DoorLockServer].
type DoorLockConfig struct {
	// Source provides live lock state and command dispatch.
	Source StateSource
	// DataVersion is an optional pointer to a [cluster.DataVersionTracker]
	// owned by the model layer. When non-nil, the server uses the caller's
	// tracker for [MatterDataVersion] and [MatterInvoke] bumps so the
	// counter survives server reconstruction across [MatterClusterServers]
	// calls. When nil, an embedded tracker is used (prior behaviour).
	DataVersion *cluster.DataVersionTracker
}

// DoorLockServer implements [contract.ClusterServer] for the
// DoorLock cluster (0x0101). It wraps a [StateSource] from the model
// layer and projects the mandatory attribute surface Apple Home and other
// controllers expect on any Lock (0x000A) device type endpoint.
//
// LockType is fixed to DeadBolt (0); ActuatorEnabled is always true;
// OperatingMode is fixed to Normal (0); SupportedOperatingModes is 0xFFF6
// — all alwaysSet bits (2047 = bits 0-10) set plus bits for vacation,
// privacy, passage; Normal (bit 0) and NoRemoteLockUnlock (bit 3) are
// both clear = supported. Mirrors matter.js DoorLockServer.ts:69
// ({vacation:true,privacy:true,passage:true,alwaysSet:2047}) → 0xFFF6.
type DoorLockServer struct {
	embedded cluster.DataVersionTracker // used when cfg.DataVersion is nil
	ext      *cluster.DataVersionTracker
	src      StateSource

	mu       sync.Mutex
	emitter  contract.EventEmitter
	endpoint uint16
	// operatingMode is OperatingMode; Normal (0) until written.
	operatingMode uint8
}

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*DoorLockServer)(nil)
	_ contract.ClusterDataVersion     = (*DoorLockServer)(nil)
	_ contract.ClusterAttributeLister = (*DoorLockServer)(nil)
	_ contract.ClusterCommandLister   = (*DoorLockServer)(nil)
	_ contract.ClusterEventLister     = (*DoorLockServer)(nil)
	_ contract.EventReceiver          = (*DoorLockServer)(nil)
)

// NewDoorLockServer constructs a DoorLockServer with LockType=DeadBolt.
func NewDoorLockServer(cfg DoorLockConfig) *DoorLockServer {
	return &DoorLockServer{src: cfg.Source, ext: cfg.DataVersion}
}

// tracker returns the active DataVersionTracker — the caller-supplied
// external tracker when available, the embedded one otherwise.
func (s *DoorLockServer) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// MatterClusterID returns 0x0101 (DoorLock).
func (*DoorLockServer) MatterClusterID() uint32 { return lockdef.ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *DoorLockServer) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead implements [contract.ClusterServer].
func (s *DoorLockServer) MatterRead(attrID uint32) (any, bool) {
	if v, ok := instance.ReadGlobal(attrID); ok {
		return v, true
	}
	switch attrID {
	case wire.DoorLockAttrLockState:
		if s.src.IsJammed() {
			return lockStateNotFullyLocked, true
		}
		locked, observed := s.src.IsLocked()
		if !observed {
			return nil, true
		}
		if locked {
			return lockStateLocked, true
		}
		return lockStateUnlocked, true
	case wire.DoorLockAttrLockType:
		return lockTypeDeadBolt, true
	case wire.DoorLockAttrActuatorEnabled:
		return true, true
	case wire.DoorLockAttrOperatingMode:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.operatingMode, true
	case wire.DoorLockAttrSupportedOperatingModes:
		// 0xFFF6: alwaysSet bits (0x07FF, bits 0-10) | vacation (bit 1) |
		// privacy (bit 2) | passage (bit 4) — Normal (bit 0) and
		// NoRemoteLockUnlock (bit 3) clear = supported.
		// Mirrors matter.js DoorLockServer.ts:69.
		return supportedOperatingModes, true
	default:
		return nil, false
	}
}

// supportedOperatingModes is SupportedOperatingModes: 0xFFF6 — Normal (bit 0)
// and NoRemoteLockUnlock (bit 3) clear, i.e. supported (matter.js
// DoorLockServer.ts:69 default).
const supportedOperatingModes uint16 = 0xFFF6

// MatterWrite implements [contract.ClusterServer]. OperatingMode ("R[W] VM",
// door-lock-cluster.element.ts) is the one writable attribute the server
// serves: matter.js keeps it as plain writable state. The definition
// checks the write (matter.js AttributeWriteResponse + ValueValidator): a
// served read-only attribute is UNSUPPORTED_WRITE, an unserved one
// UNSUPPORTED_ATTRIBUTE, a value outside OperatingModeEnum CONSTRAINT_ERROR;
// a mode whose bit in SupportedOperatingModes is set (not supported — the
// bitmap inverts) is the server's CONSTRAINT_ERROR.
func (s *DoorLockServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	n, err := instance.ValidateWrite(attrID, value, nil)
	if err != nil {
		return err
	}
	m, _ := n.(uint64) // an enum8 per ValidateWrite, OperatingMode the one writable attribute
	v := uint8(m)      //nolint:gosec // an enum8 per ValidateWrite
	if supportedOperatingModes&(1<<v) != 0 {
		return doorLockConstraintErr{fmt.Sprintf("doorlock: OperatingMode %d is not a supported mode", v)}
	}
	s.mu.Lock()
	s.operatingMode = v
	s.mu.Unlock()
	s.tracker().Bump()
	return nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// OperatingMode is "R[W] VM".
func (*DoorLockServer) MinWritePrivilege(attrID uint32) uint8 {
	return instance.MinWritePrivilege(attrID)
}

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege].
func (*DoorLockServer) MinInvokePrivilege(cmdID uint32) uint8 {
	return instance.MinInvokePrivilege(cmdID)
}

// doorLockConstraintErr surfaces as CONSTRAINT_ERROR.
type doorLockConstraintErr struct{ msg string }

func (e doorLockConstraintErr) Error() string                 { return e.msg }
func (doorLockConstraintErr) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// MatterInvoke implements [contract.ClusterServer]. Dispatches
// LockDoor / UnlockDoor / UnboltDoor to the underlying source and fires
// the LockOperation event on success.
func (s *DoorLockServer) MatterInvoke(ctx context.Context, cmdID uint32, _ any) (any, error) {
	switch cmdID {
	case lockdef.CmdLockDoor, lockdef.CmdUnlockDoor, lockdef.CmdUnboltDoor:
		if err := s.src.LockInvoke(ctx, cmdID); err != nil {
			return nil, err
		}
		s.tracker().Bump()
		s.emitLockOperation(ctx, cmdID)
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: 0x%02X", errUnknownCommand, cmdID)
	}
}

// emitLockOperation fires the Matter §5.2.10.3 LockOperation event
// after a successful remote lock/unlock/unbolt. Mirrors matter.js
// DoorLockServer.ts:119-143 (each command handler ends in
// #emitLockOperation) and :911-939 (payload assembly:
// OperationSource=Remote, UserIndex/Credentials null without PIN
// credentials, FabricIndex + SourceNode from the invoking session).
// A PASE session carries fabric index 0 and no subject node — both
// encode as TLV null, matching matter.js's null fallbacks. No-op until
// the bridge wires an emitter via [SetMatterEventEmitter].
func (s *DoorLockServer) emitLockOperation(ctx context.Context, cmdID uint32) {
	s.mu.Lock()
	emitter := s.emitter
	endpoint := s.endpoint
	s.mu.Unlock()
	if emitter == nil {
		return
	}
	var opType uint8
	switch cmdID {
	case wire.DoorLockCmdLockDoor:
		opType = lockOperationTypeLock
	case wire.DoorLockCmdUnlockDoor:
		opType = lockOperationTypeUnlock
	case wire.DoorLockCmdUnboltDoor:
		// UnboltDoor reports Unlatch, not Unlock — matter.js
		// DoorLockServer.ts:140-142.
		opType = lockOperationTypeUnlatch
	}
	ev := LockOperationEvent{
		LockOperationType: opType,
		OperationSource:   operationSourceRemote,
	}
	if _, fabricIndex := im.FabricFilterFromContext(ctx); fabricIndex != 0 {
		ev.FabricIndex = &fabricIndex
	}
	if nodeID, _ := im.SubjectFromContext(ctx); nodeID != 0 {
		ev.SourceNode = &nodeID
	}
	emitter.MatterEmitEvent(endpoint, lockdef.ClusterID,
		lockdef.EventLockOperation, ev,
		instance.EventPriority(lockdef.EventLockOperation))
}

// SetMatterEventEmitter implements [contract.EventReceiver].
// Called by the bridge during topology assembly so [MatterInvoke] can
// fire the LockOperation event without holding a bridge reference.
// Idempotent — re-wiring during topology rebuild replaces the emitter.
func (s *DoorLockServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
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

// MatterReportable returns the attribute IDs that change on wire events.
func (*DoorLockServer) MatterReportable() []uint32 {
	return []uint32{wire.DoorLockAttrLockState}
}

// MatterAttributes implements [contract.ClusterAttributeLister]: the
// attributes UBOLT makes mandatory — LockState, LockType, ActuatorEnabled,
// OperatingMode, SupportedOperatingModes.
func (*DoorLockServer) MatterAttributes() []uint32 { return instance.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// LockDoor, UnlockDoor (M) and UnboltDoor (UBOLT).
func (*DoorLockServer) MatterAcceptedCommands() []uint32 { return instance.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// DoorLock commands produce status-only InvokeResponses; no generated command
// IDs are advertised.
func (*DoorLockServer) MatterGeneratedCommands() []uint32 { return instance.MatterGeneratedCommands() }

// MatterEvents implements [contract.ClusterEventLister]. The
// three conformance-M DoorLock events are advertised (DoorLockAlarm,
// LockOperation, LockOperationError); DoorStateChange (DPS) and
// LockUserChange (USR) are feature-gated and absent. The server emits
// LockOperation on successful remote operations; DoorLockAlarm and
// LockOperationError have no emission path without PIN-credential
// support, matching matter.js where they fire only from the wrong-code
// path (DoorLockServer.ts:889 / :941).
func (*DoorLockServer) MatterEvents() []uint32 { return instance.MatterEvents() }
