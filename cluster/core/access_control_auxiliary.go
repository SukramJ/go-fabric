// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"log/slog"
	"maps"
	"reflect"
	"slices"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// aclStatusError is an AccessControl rejection carrying the IM status
// matter.js answers with.
type aclStatusError struct {
	code im.StatusCode
	msg  string
}

func (e aclStatusError) Error() string { return "core: AccessControl: " + e.msg }

// MatterStatusCode implements [im.StatusCodeError].
func (e aclStatusError) MatterStatusCode() im.StatusCode { return e.code }

var _ im.StatusCodeError = aclStatusError{}

// featureMap is EXTS, plus AUX once an auxiliary source is registered.
func (a *AccessControl) featureMap() uint32 {
	if a.auxiliaryEnabled() {
		return accessControlFeatureExtension | accessControlFeatureAuxiliary
	}
	return accessControlFeatureExtension
}

func (a *AccessControl) auxiliaryEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.auxSource != nil
}

// registerAuxiliaryProvider turns the Auxiliary feature on with src as the
// provider of its entries: the AuxiliaryAcl attribute and the
// AuxiliaryAccessUpdated event appear, and every change src signals is
// re-synchronised. The initial synchronisation emits no event. Mirrors
// matter.js AccessControlServer.registerAuxAclProvider
// ("Sync initial value without emitting events").
func (a *AccessControl) registerAuxiliaryProvider(ctx context.Context, src AuxiliaryACLSource) {
	a.mu.Lock()
	a.auxSource = src
	a.mu.Unlock()
	a.syncAuxiliary(ctx, false)
	src.OnGroupcastChanged(func(changeCtx context.Context, fabric uint8) {
		a.mu.Lock()
		batched := a.auxBatch > 0
		// Only a change a request made names a node: a change without one
		// — GroupKeyManagement's retry of a stale group state, a forget —
		// neither records a context nor displaces the one a request
		// recorded (review II, R2-P6-err-1701 R3-1). Nor does a request of
		// another fabric: GroupKeyManagement reloads every stale fabric
		// under the context of the command that runs the reload, and that
		// command's node is no administrator of the changed fabric (review
		// II, R2-P7-acl-01).
		if _, f, request := im.LookupFabricFilter(changeCtx); batched && request && f == fabric {
			if a.auxDeferred == nil {
				a.auxDeferred = make(map[uint8]context.Context)
			}
			a.auxDeferred[fabric] = changeCtx
		}
		a.mu.Unlock()
		if !batched {
			a.syncAuxiliary(changeCtx, true)
		}
	})
}

// beginAuxiliaryBatch defers the synchronisation of source changes until
// the matching endAuxiliaryBatch.
func (a *AccessControl) beginAuxiliaryBatch() {
	a.mu.Lock()
	a.auxBatch++
	a.mu.Unlock()
}

// endAuxiliaryBatch closes a batch and, once no batch is open,
// synchronises once: a change of the closing command's own fabric names
// the node of ctx, a change another source signalled meanwhile the node
// of its own context (auxDeferred).
func (a *AccessControl) endAuxiliaryBatch(ctx context.Context) {
	a.mu.Lock()
	if a.auxBatch > 0 {
		a.auxBatch--
	}
	open := a.auxBatch > 0
	var (
		deferred map[uint8]context.Context
		noted    map[uint8]uint64
	)
	if !open {
		deferred, a.auxDeferred = a.auxDeferred, nil
		// The drop epochs of the taken contexts, noted in the hold that
		// takes them: a drop before the synchronisation's own take is
		// seen there too (#619).
		noted = a.dropEpochsLocked(deferred)
	}
	a.mu.Unlock()
	if !open {
		a.syncAuxiliaryAs(ctx, ctx, true, deferred, noted)
	}
}

// syncAuxiliary recomputes the auxiliary entries and, for every fabric whose
// entries changed, bumps the DataVersion and — with emit — fires
// AuxiliaryAccessUpdated naming the administering node: the node of the
// change's own context a batch deferred (endAuxiliaryBatch, owed contexts),
// else the node of ctx when ctx is a
// request of that fabric, else none — a fabric's event never names a node
// of another fabric's request (review II, R2-P6-conc-1007). A fabric that
// no longer exists gets no event. Mirrors matter.js AccessControlServer
// #syncAuxAcl.
func (a *AccessControl) syncAuxiliary(ctx context.Context, emit bool) {
	a.syncAuxiliaryAs(ctx, ctx, emit, nil, nil)
}

// settleOwedAuxiliary runs the synchronisation a failed read of the source
// owes (auxOwed), before a read of the cluster serves its attributes
// (review II, R4-err-4055). The dispatcher takes a read's DataVersion
// before its value, so the read that settles reports the version from
// before the run with the value after it; the next read reports the
// version the run moved to. The reader made no change, so the run has no
// actor: each event names the node of its own failed change (auxOwedCtx)
// or none, never the reader's.
func (a *AccessControl) settleOwedAuxiliary(ctx context.Context) {
	a.mu.RLock()
	owed, emit := a.auxOwed, a.auxOwedEmit
	a.mu.RUnlock()
	if owed {
		a.syncAuxiliaryAs(ctx, nil, emit, nil, nil)
	}
}

// syncAuxiliaryAs is syncAuxiliary whose source reads run under ctx and
// whose events name the node of actor (nil: no actor). It first takes the
// synchronisation a failed read owes, with its contexts — a context in
// deferred is newer and wins; an owed context wins over actor for its
// fabric, so a later change by another administrator of that fabric is
// reported with the earlier failed change's node — and a read of the
// source that fails owes
// the whole synchronisation again, its contexts and actor's own fabric's
// kept, rather than dropping them (review II, R4-err-4055). noted is the
// drop epoch of each context in deferred as the caller took it (#619);
// it overrides the epoch noted at this run's own take. The failure is
// logged at Warn when nothing was owed before the run and at Debug when
// the run failed to settle a debt already owed (#621).
func (a *AccessControl) syncAuxiliaryAs(ctx, actor context.Context, emit bool, deferred map[uint8]context.Context, noted map[uint8]uint64) {
	a.mu.RLock()
	src := a.auxSource
	a.mu.RUnlock()
	if src == nil {
		return
	}
	a.auxSyncMu.Lock()
	a.mu.Lock()
	wasOwed := a.auxOwed
	deferred = mergeAuxContexts(a.auxOwedCtx, deferred)
	a.auxOwed, a.auxOwedEmit, a.auxOwedCtx = false, false, nil
	// The drop epoch of each fabric whose context the run holds, noted in
	// the hold of mu that takes the contexts: a fabric dropped while the
	// source is read gets no context back from a failure (#619).
	epochs := a.auxEpochsLocked(actor, deferred)
	maps.Copy(epochs, noted)
	a.mu.Unlock()
	entries, err := src.AuxiliaryACL(ctx, 0)
	if err != nil {
		if f, ok := requestFabricOf(actor); ok {
			deferred = mergeAuxContexts(map[uint8]context.Context{f: actor}, deferred)
		}
		a.mu.Lock()
		a.auxOwed = true
		a.auxOwedEmit = a.auxOwedEmit || emit
		a.auxOwedCtx = mergeAuxContexts(a.undroppedLocked(deferred, epochs), a.auxOwedCtx)
		a.mu.Unlock()
		a.auxSyncMu.Unlock()
		a.logger().Log(ctx, auxSyncFailLevel(wasOwed), "matter.access_control.auxiliary_sync", slog.Any("err", err))
		return
	}
	next := make(map[uint8][]store.ACLEntry)
	for _, e := range entries {
		next[e.FabricIndex] = append(next[e.FabricIndex], e)
	}
	a.mu.Lock()
	prev := a.auxApplied
	a.auxApplied = next
	emitter, endpoint := a.emitter, a.endpoint
	a.mu.Unlock()
	a.auxSyncMu.Unlock()

	var changed []uint8
	for fabric := range unionKeys(prev, next) {
		if !reflect.DeepEqual(prev[fabric], next[fabric]) {
			changed = append(changed, fabric)
		}
	}
	if len(changed) == 0 {
		return
	}
	slices.Sort(changed)
	a.dataVersion.Bump()
	if !emit || emitter == nil {
		return
	}
	known, err := src.Fabrics(ctx)
	if err != nil {
		return
	}
	var accessing uint8
	if actor != nil {
		_, accessing = im.FabricFilterFromContext(actor)
	}
	for _, fabric := range changed {
		if !slices.Contains(known, fabric) {
			continue
		}
		var admin *uint64
		switch own, ok := deferred[fabric]; {
		case ok:
			admin = adminNodeIDOf(own) //nolint:contextcheck // own is the deferred change's request context, read for its subject only
		case accessing == fabric:
			admin = adminNodeIDOf(actor)
		}
		emitter.MatterEmitEvent(endpoint, accessControlClusterID, accessControlEventAuxiliaryAccessUpd,
			AuxiliaryAccessUpdatedEvent{AdminNodeID: admin, FabricIndex: fabric}, contract.EventPriorityInfo)
	}
}

// auxSyncFailLevel is the level of a failed synchronisation's log: a debt
// that starts failing is a Warn; the same debt failing again — at every
// read of the cluster while the source stays down — is Debug, so a
// wildcard read or a subscriber's report does not warn per attribute
// (#621). A run that succeeds settles the debt, and the next failure warns
// again.
func auxSyncFailLevel(wasOwed bool) slog.Level {
	if wasOwed {
		return slog.LevelDebug
	}
	return slog.LevelWarn
}

// requestFabricOf is the fabric of the request behind actor; false for a
// nil actor, a context without a request and a session without a fabric.
func requestFabricOf(actor context.Context) (uint8, bool) {
	if actor == nil {
		return 0, false
	}
	_, f, request := im.LookupFabricFilter(actor)
	return f, request && f != 0
}

// auxEpochsLocked notes the drop epoch (aclDropEpoch) of every fabric
// deferred names and of actor's own fabric. Called with mu held.
func (a *AccessControl) auxEpochsLocked(actor context.Context, deferred map[uint8]context.Context) map[uint8]uint64 {
	out := a.dropEpochsLocked(deferred)
	if f, ok := requestFabricOf(actor); ok {
		out[f] = a.aclDropEpoch[f]
	}
	return out
}

// dropEpochsLocked notes the drop epoch (aclDropEpoch) of every fabric
// ctxs names. Called with mu held.
func (a *AccessControl) dropEpochsLocked(ctxs map[uint8]context.Context) map[uint8]uint64 {
	out := make(map[uint8]uint64, len(ctxs)+1)
	for f := range ctxs {
		out[f] = a.aclDropEpoch[f]
	}
	return out
}

// undroppedLocked is ctxs without the fabrics whose drop epoch moved since
// epochs was noted: a context of a fabric dropped meanwhile goes with the
// fabric, as dropFabric deletes it from auxOwedCtx. A fabric epochs does
// not name is dropped too. Called with mu held.
func (a *AccessControl) undroppedLocked(ctxs map[uint8]context.Context, epochs map[uint8]uint64) map[uint8]context.Context {
	var out map[uint8]context.Context
	for f, c := range ctxs {
		if e, ok := epochs[f]; !ok || a.aclDropEpoch[f] != e {
			continue
		}
		if out == nil {
			out = make(map[uint8]context.Context, len(ctxs))
		}
		out[f] = c
	}
	return out
}

// mergeAuxContexts is older with newer laid over it: a fabric newer names
// keeps newer's context. Nil when both are empty.
func mergeAuxContexts(older, newer map[uint8]context.Context) map[uint8]context.Context {
	if len(older) == 0 {
		return newer
	}
	out := make(map[uint8]context.Context, len(older)+len(newer))
	maps.Copy(out, older)
	maps.Copy(out, newer)
	return out
}

func unionKeys(a, b map[uint8][]store.ACLEntry) map[uint8]struct{} {
	out := make(map[uint8]struct{}, len(a)+len(b))
	for k := range a {
		out[k] = struct{}{}
	}
	for k := range b {
		out[k] = struct{}{}
	}
	return out
}

// adminNodeIDOf names the node behind a change: the CASE subject of the
// request, none for PASE or a change without a request. matter.js reports
// the accessing fabric's root node id (`#adminDataFromSession`); the
// subject of the CASE session is the node that acted, which this module
// can name without persisting the fabric's CaseAdminSubject.
func adminNodeIDOf(ctx context.Context) *uint64 {
	if im.IsPASEFromContext(ctx) {
		return nil
	}
	nodeID, _ := im.SubjectFromContext(ctx)
	if nodeID == 0 {
		return nil
	}
	return &nodeID
}

// auxiliaryACLRead serves the AuxiliaryAcl attribute: the auxiliary
// entries split to SubjectsPerAccessControlEntry and
// TargetsPerAccessControlEntry, each with AuxiliaryType Groupcast. A
// fabric-filtered read sees the accessing fabric's entries; an unfiltered
// one every fabric's, whole — the dispatcher withholds another fabric's
// fabric-sensitive fields from the request. local marks a read without an
// IM request behind it, which sees everything. Mirrors matter.js
// AccessControlServer #auxiliaryAclFor and the fabric-sensitive handling of
// a fabric-scoped list.
func (a *AccessControl) auxiliaryACLRead(ctx context.Context, filtered bool, fabricIndex uint8, local bool) (any, bool) {
	a.mu.RLock()
	src := a.auxSource
	a.mu.RUnlock()
	if src == nil {
		return nil, false
	}
	if filtered && fabricIndex == 0 && !local {
		return []AccessControlAuxiliaryEntryStruct{}, true
	}
	scope := uint8(0)
	if filtered && !local {
		scope = fabricIndex
	}
	entries, err := src.AuxiliaryACL(ctx, scope)
	if err != nil {
		return nil, false
	}
	// Split by the configured limits, the ones the capacity attributes
	// advertise. Mirrors matter.js db021da0 packages/node/src/behaviors/
	// access-control/AccessControlServer.ts:67-70 (auxiliaryEntryLimits from
	// the state) and :671, :681-682 (#auxiliaryAclFor).
	maxSubjects, maxTargets := int(a.subjectsPerEntry()), int(a.targetsPerEntry())
	out := []AccessControlAuxiliaryEntryStruct{}
	for _, e := range entries {
		for _, subjects := range chunked(e.Subjects, maxSubjects) {
			for _, targets := range chunked(e.Targets, maxTargets) {
				chunk := e
				chunk.Subjects, chunk.Targets = subjects, targets
				out = append(out, AccessControlAuxiliaryEntryStruct{
					Entry:         aclEntryStruct(chunk),
					AuxiliaryType: AccessControlAuxiliaryTypeGroupcast,
				})
			}
		}
	}
	if len(out) > accessControlAuxiliaryACLMaxEntries {
		out = out[:accessControlAuxiliaryACLMaxEntries]
	}
	return out, true
}

// chunked splits list into pieces of at most size; an empty (wildcard)
// list stays one empty piece. Mirrors matter.js AccessControlServer chunked.
func chunked[T any](list []T, size int) [][]T {
	if len(list) <= size {
		return [][]T{list}
	}
	var out [][]T
	for i := 0; i < len(list); i += size {
		out = append(out, list[i:min(i+size, len(list))])
	}
	return out
}

// aclEntryStruct converts a stored entry to its wire struct.
func aclEntryStruct(e store.ACLEntry) AccessControlEntryStruct {
	ace := AccessControlEntryStruct{
		Privilege:   uint8(e.Privilege),
		AuthMode:    uint8(e.AuthMode),
		Subjects:    append([]uint64(nil), e.Subjects...),
		FabricIndex: e.FabricIndex,
	}
	if len(e.Targets) > 0 {
		ace.Targets = make([]ACLTargetStruct, 0, len(e.Targets))
		for _, t := range e.Targets {
			ace.Targets = append(ace.Targets, ACLTargetStruct{Cluster: t.Cluster, Endpoint: t.Endpoint, DeviceType: t.DeviceType})
		}
	}
	return ace
}
