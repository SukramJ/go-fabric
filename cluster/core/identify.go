// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	identifydef "github.com/SukramJ/go-fabric/cluster/spec/identify"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/clockvar"
)

// Identify implements the Matter Identify cluster (0x0003) per Matter
// Application Cluster Specification §1.2. Mandatory on every endpoint
// other than Root and Network Commissioning (Spec §1.4) — Apple Home's
// HAP service rebuild step uses the cluster's presence as a structural
// gate; an endpoint that advertises a primary device type without
// Identify in its ServerList fails HAPErrorDomain Code=24 ("Failed to
// rebuild HAP services") and the pair aborts.
//
// matter.js places `IdentifyServer` in the mandatory cluster set of
// every device type definition under
// `packages/node/src/devices/*.ts` (e.g. `contact-sensor.ts`,
// `temperature-sensor.ts`, `on-off-plug-in-unit.ts`). Bridge endpoints
// inherit the same requirement.
//
// Bridge semantics: go-fabric does not drive any visual or audible
// indicator on the underlying CCU device, so Identify is implemented
// as a no-op surface that:
//
//   - reports IdentifyType = None (0x00) — "the device has no
//     identification mechanism", signalling clients should fall back
//     to a software cue rather than expect blinking lights;
//   - tracks IdentifyTime as a software-only countdown (set by the
//     Identify command, not driven by hardware);
//   - implements the Identify command as a successful no-op so
//     commissioner-side Identify flows complete without error.
//
// The TriggerEffect command (0x40) is optional per spec; we accept it
// and return Success without performing any effect — Apple's HAP
// mapper expects either a working implementation or a clean Success
// reply. Returning UnsupportedCommand here would surface as a HAP
// build warning that some firmware revisions escalate to Code=24.
type Identify struct {
	// clock paces the IdentifyTime countdown (see [Identify.SetClock]).
	clock clockvar.Var

	// handler is the host's [contract.IdentifyHandler]
	// ([Identify.SetHandler]); nil runs none.
	handler atomic.Pointer[identifyHandlerRef]

	// identifyTime is tracked atomically because reads (subscribe
	// reports), writes (clients setting it directly per spec §1.2.5.1),
	// and command dispatch (Identify) all touch the same field from
	// different goroutines.
	identifyTime atomic.Uint32 // logically uint16; high bits ignored

	// dataVersion tracks the per-cluster monotonic counter per Matter
	// §10.6.5. Bumped after every successful IdentifyTime write (via
	// MatterWrite or MatterInvoke) so DataVersionFilter evaluation works.
	// Satisfies [contract.ClusterDataVersion].
	dataVersion cluster.DataVersionTracker

	// Countdown-Timer state. Spec §1.2.5.1: "The IdentifyTime
	// attribute … SHALL decrement at a rate of 1 per second once
	// active". One goroutine per Identify cluster decrements every
	// second until either zero is reached or a write/command sets a
	// new non-zero value (in which case the existing goroutine
	// continues with the new value). The instance MUST therefore
	// outlive a single dispatch — a per-call instance both loses the
	// state and strands one ticker goroutine per invoke.
	//
	// timerMu guards timerActive and closed.
	timerMu     sync.Mutex
	timerActive bool
	closed      bool
	// done is closed by [Identify.Close] and terminates a running
	// countdown immediately, so an instance whose endpoint disappeared
	// cannot keep a ticker alive for up to 65535 s.
	done chan struct{}
	// countdown is the countdown goroutine, added under timerMu while
	// open and waited for by [Identify.Close].
	countdown sync.WaitGroup
}

// Cluster ID, revision, attribute and command ids: the generated
// definition's (cluster/spec/identify, ADR 0013, from matter.js
// identify.element.ts). The lists, FeatureMap and the write status of the
// read-only IdentifyType come from it as well; the countdown is this
// server's.
const (
	identifyClusterID       = identifydef.ClusterID
	identifyClusterRevision = identifydef.Revision

	identifyAttrTime = identifydef.AttrIdentifyTime // IdentifyTime  (uint16, RW, default 0)
	identifyAttrType = identifydef.AttrIdentifyType // IdentifyType  (enum8,  R, default 0/None)

	identifyCmdIdentify      = identifydef.CmdIdentify      // Identify(IdentifyTime)
	identifyCmdTriggerEffect = identifydef.CmdTriggerEffect // TriggerEffect(EffectIdentifier, EffectVariant)

	// identifyTypeNone is IdentifyTypeEnum None.
	identifyTypeNone = uint8(identifydef.IdentifyTypeNone)
)

// identifyInst is the definition bound to what the server serves: no
// feature, both attributes, and the optional TriggerEffect, accepted as a
// no-op.
var identifyInst = mustInstance(identifydef.Definition, spec.Options{Commands: []uint32{identifyCmdTriggerEffect}})

// NewIdentify constructs a fresh Identify cluster instance with
// IdentifyTime = 0 and IdentifyType = None.
//
// The returned instance is stateful (IdentifyTime + its countdown) and
// must be bound to the lifetime of the endpoint that hosts it. Call
// [Identify.Close] when that endpoint goes away.
func NewIdentify() *Identify {
	return &Identify{done: make(chan struct{})}
}

// SetClock sets the clock the IdentifyTime countdown ticks on; nil is
// [clock.System]. A countdown already running keeps its clock.
func (i *Identify) SetClock(c clock.Clock) { i.clock.Store(c) }

// identifyHandlerRef holds a handler for the atomic pointer.
type identifyHandlerRef struct{ h contract.IdentifyHandler }

// SetHandler sets the host's handler of the Identify and TriggerEffect
// commands, run before the server's own handling; an error it returns
// refuses the command. nil removes it. Mirrors matter.js's application
// override of IdentifyServer.identify and triggerEffect
// (packages/node/src/behaviors/identify/IdentifyServer.ts:129-135 at
// 862ad75f).
func (i *Identify) SetHandler(h contract.IdentifyHandler) {
	if h == nil {
		i.handler.Store(nil)
		return
	}
	i.handler.Store(&identifyHandlerRef{h})
}

func (i *Identify) hostHandler() contract.IdentifyHandler {
	if r := i.handler.Load(); r != nil {
		return r.h
	}
	return nil
}

// Close releases the cluster: a running countdown terminates at once —
// Close returns after its goroutine did — and no later write / command
// starts a new one. Idempotent and safe for concurrent use.
//
// Mirrors matter.js
// packages/node/src/behaviors/identify/IdentifyServer.ts
// [Symbol.asyncDispose], which stops the identify timer when the
// behavior — and with it the endpoint — is destroyed.
func (i *Identify) Close() {
	i.timerMu.Lock()
	if !i.closed {
		i.closed = true
		i.timerActive = false
		if i.done != nil {
			close(i.done)
		}
	}
	i.timerMu.Unlock()
	i.countdown.Wait()
}

// Compile-time assertions.
var (
	_ contract.ClusterServer        = (*Identify)(nil)
	_ contract.ClusterDataVersion   = (*Identify)(nil)
	_ contract.ClusterCommandLister = (*Identify)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (i *Identify) MatterClusterID() uint32 { return identifyClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
// Returns the current per-cluster monotonic counter bumped after every
// successful IdentifyTime write or Identify command dispatch.
// Mirrors matter.js IdentifyServer.ts DataVersion tracking.
func (i *Identify) MatterDataVersion() uint32 { return i.dataVersion.Current() }

// MatterRead implements [contract.ClusterServer].
func (i *Identify) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case identifyAttrTime:
		return uint16(i.identifyTime.Load() & 0xFFFF), true // value capped at uint16 by every writer
	case identifyAttrType:
		return identifyTypeNone, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return identifyInst.ReadGlobal(attrID)
	}
	return nil, false
}

// MatterWrite handles direct writes to IdentifyTime per Matter
// §1.2.5.1. Spec wording: "The IdentifyTime attribute SHALL be in
// units of seconds, and SHALL be writable." — clients drive Identify
// either via the command or by setting the attribute directly, and the
// host's handler sees both: matter.js reacts to identifyTime$Changed
// whichever wrote it (IdentifyServer.ts:57-58 at 862ad75f), so the write
// runs the handler as the Identify command does and a refusal refuses the
// write with its status.
func (i *Identify) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if attrID != identifyAttrTime {
		// IdentifyType is "R V": UNSUPPORTED_WRITE; any other id
		// UNSUPPORTED_ATTRIBUTE, as the definition judges them.
		_, err := identifyInst.ValidateWrite(attrID, value, nil)
		return err
	}
	t, err := coerceUint16(value)
	if err != nil {
		return fmt.Errorf("core: Identify.IdentifyTime: %w", err)
	}
	// Mirrors matter.js packages/node/src/behaviors/identify/
	// IdentifyServer.ts:57-58, :108-114 at 862ad75f: the attribute and the
	// command reach one reactor (#identifyTimeChangedHandler).
	if h := i.hostHandler(); h != nil {
		if err := h.MatterIdentify(ctx, t); err != nil {
			return err
		}
	}
	i.identifyTime.Store(uint32(t))
	i.maybeStartCountdown()
	// Bump DataVersion after a successful IdentifyTime write so
	// DataVersionFilter evaluation correctly detects the cluster changed.
	i.dataVersion.Bump()
	return nil
}

// MatterInvoke dispatches Identify and TriggerEffect commands.
func (i *Identify) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	switch cmdID {
	case identifyCmdIdentify:
		// Identify command argument is a struct {IdentifyTime: uint16}
		// per Matter §1.2.6.1. The IM cluster-fields decoder may pass
		// either the decoded struct or a raw value depending on the
		// commissioner's encoding; coerceUint16 handles both with a
		// best-effort fallback to 0 (= clear identify).
		t, _ := coerceUint16(fields)
		if h := i.hostHandler(); h != nil {
			if err := h.MatterIdentify(ctx, t); err != nil {
				return nil, err
			}
		}
		i.identifyTime.Store(uint32(t))
		i.maybeStartCountdown()
		// Bump DataVersion after a successful Identify command so
		// DataVersionFilter evaluation correctly detects the cluster changed.
		i.dataVersion.Bump()
		// Success path returns nil (status-only response).
		return nil, nil
	case identifyCmdTriggerEffect:
		// Without a host handler TriggerEffect is a no-op for the
		// bridge — we don't actuate any indicator. Return success so
		// Apple's HAP mapper does not flag the cluster as broken during
		// the topology probe. A host handler gets the effect.
		if h := i.hostHandler(); h != nil {
			effect, variant := triggerEffectOf(fields)
			if err := h.MatterTriggerEffect(ctx, effect, variant); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	return nil, im.UnsupportedCommandf("core: Identify command 0x%02X not supported", cmdID)
}

// MatterReportable returns the attributes that change over the
// subscription's lifetime. IdentifyTime ticks down once per second
// while a software-driven identify is in progress; clients subscribe
// to it to render the remaining-time indicator.
func (i *Identify) MatterReportable() []uint32 {
	return []uint32{identifyAttrTime}
}

// MatterAttributes implements
// [contract.ClusterAttributeLister] so wildcard subscribe /
// read enumerates the full Identify surface.
func (i *Identify) MatterAttributes() []uint32 { return identifyInst.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
// Returns the command IDs handled by MatterInvoke so the dispatcher
// populates AcceptedCommandList (0xFFF9) correctly. TriggerEffect (0x40)
// is optional per spec but accepted as a visual no-op; including it here
// prevents strict commissioners from flagging an AcceptedCommandList
// mismatch against the Identify element.ts command table.
func (i *Identify) MatterAcceptedCommands() []uint32 { return identifyInst.MatterAcceptedCommands() }

// MatterGeneratedCommands returns nil; Identify commands carry no
// response payload (the definition generates none).
func (i *Identify) MatterGeneratedCommands() []uint32 { return nil }

// maybeStartCountdown starts a background goroutine that decrements
// IdentifyTime once per second per Matter §1.2.5.1, if one is not
// already running. Concurrent callers are serialised by timerMu so
// only one goroutine is ever active at a time; the goroutine returns
// when the countdown reaches zero or [Identify.Close] is called.
func (i *Identify) maybeStartCountdown() {
	i.timerMu.Lock()
	if i.closed || i.timerActive || i.identifyTime.Load() == 0 {
		i.timerMu.Unlock()
		return
	}
	i.timerActive = true
	done := i.done
	i.countdown.Add(1)
	i.timerMu.Unlock()

	go func() {
		defer i.countdown.Done()
		ticker := i.clock.Load().NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C():
				v := i.identifyTime.Load()
				if v == 0 {
					i.timerMu.Lock()
					i.timerActive = false
					i.timerMu.Unlock()
					return
				}
				// A CompareAndSwap, so a write or command landing
				// between the load and the store is not overwritten.
				if !i.identifyTime.CompareAndSwap(v, v-1) || v != 1 {
					continue
				}
				// The countdown reached zero: the host is told to stop,
				// as matter.js's tick drives the reactor to
				// #stopIdentifying (IdentifyServer.ts:100-114, :121-127
				// at 862ad75f); 0 is the contract's "stops".
				if h := i.hostHandler(); h != nil {
					_ = h.MatterIdentify(context.Background(), 0) //nolint:errcheck // nothing to refuse: the countdown ended
				}
			}
		}
	}()
}

// triggerEffectOf reads TriggerEffect's EffectIdentifier and EffectVariant
// from the generated request the bridge decodes, or from the tag-keyed map
// of the generic path (tags 0 and 1); 0 for what it cannot read.
func triggerEffectOf(v any) (effect, variant uint8) {
	switch x := v.(type) {
	case identifydef.TriggerEffectRequest:
		return uint8(x.EffectIdentifier), uint8(x.EffectVariant)
	case *identifydef.TriggerEffectRequest:
		if x != nil {
			return uint8(x.EffectIdentifier), uint8(x.EffectVariant)
		}
	case map[uint8]any:
		e, _ := coerceUint16(x[0])
		w, _ := coerceUint16(x[1])
		return uint8(e), uint8(w) //nolint:gosec // 8-bit enums
	}
	return 0, 0
}

// identifyFieldIdentifyTime is the Identify command's IdentifyTime
// context tag (identify.element.ts, Identify command field id 0x0).
const identifyFieldIdentifyTime uint8 = 0

// coerceUint16 best-effort reads a uint16 out of TLV-decoded values.
// The bridge decodes a real Identify into the generated IdentifyRequest
// (a missing or out-of-range IdentifyTime is answered INVALID_COMMAND /
// CONSTRAINT_ERROR there, as matter.js's request schema answers it); the
// tag-keyed map of the generic salvage path (bridge/fields_reader.go
// decodeGenericTagMap) with IdentifyTime under tag 0 and a bare uint64 /
// uint32 / uint16 are what a host that decoded the payload itself hands
// over. Unrecognised shapes coerce to 0,
// which is the spec's "clear identify" semantic — safer than rejecting
// the command outright.
func coerceUint16(v any) (uint16, error) {
	switch x := v.(type) {
	case identifydef.IdentifyRequest:
		return x.IdentifyTime, nil
	case *identifydef.IdentifyRequest:
		if x == nil {
			return 0, nil
		}
		return x.IdentifyTime, nil
	case map[uint8]any:
		raw, ok := x[identifyFieldIdentifyTime]
		if !ok {
			return 0, errors.New("core: missing the IdentifyTime field")
		}
		return coerceUint16(raw)
	case uint16:
		return x, nil
	case uint32:
		return uint16(x & 0xFFFF), nil // spec-bound: IdentifyTime fits uint16
	case uint64:
		return uint16(x & 0xFFFF), nil // spec-bound: IdentifyTime fits uint16
	case int:
		return uint16(x), nil //nolint:gosec // spec-bound; see #20
	case int64:
		return uint16(x), nil //nolint:gosec // spec-bound; see #20
	case nil:
		return 0, nil
	}
	return 0, fmt.Errorf("core: unsupported IdentifyTime type %T", v)
}
