// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"log/slog"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// aclStatusError is an AccessControl rejection carrying the IM status
// matter.js answers with.
type aclStatusError struct {
	code im.StatusCode
	msg  string
}

func (e aclStatusError) Error() string { return "matter: AccessControl: " + e.msg }

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
	src.OnGroupcastChanged(func(changeCtx context.Context, _ uint8) {
		a.mu.RLock()
		batched := a.auxBatch > 0
		a.mu.RUnlock()
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
// synchronises once with ctx naming the administering node.
func (a *AccessControl) endAuxiliaryBatch(ctx context.Context) {
	a.mu.Lock()
	if a.auxBatch > 0 {
		a.auxBatch--
	}
	open := a.auxBatch > 0
	a.mu.Unlock()
	if !open {
		a.syncAuxiliary(ctx, true)
	}
}

// syncAuxiliary recomputes the auxiliary entries and, for every fabric whose
// entries changed, bumps the DataVersion and — with emit — fires
// AuxiliaryAccessUpdated naming the administering node of ctx. A fabric
// that no longer exists gets no event. Mirrors matter.js AccessControlServer
// #syncAuxAcl.
func (a *AccessControl) syncAuxiliary(ctx context.Context, emit bool) {
	a.mu.RLock()
	src := a.auxSource
	a.mu.RUnlock()
	if src == nil {
		return
	}
	entries, err := src.AuxiliaryACL(ctx, 0)
	if err != nil {
		slog.Default().Warn("matter.access_control.auxiliary_sync", slog.String("err", err.Error()))
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
	admin := adminNodeIDOf(ctx)
	for _, fabric := range changed {
		if !slices.Contains(known, fabric) {
			continue
		}
		emitter.MatterEmitEvent(endpoint, accessControlClusterID, accessControlEventAuxiliaryAccessUpd,
			AuxiliaryAccessUpdatedEvent{AdminNodeID: admin, FabricIndex: fabric}, contract.EventPriorityInfo)
	}
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
// one every fabric's, with another fabric's entries — or all of them for a
// session without a fabric — redacted to FabricIndex. local marks a read
// without an IM request behind it, which sees everything. Mirrors matter.js
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
	out := []AccessControlAuxiliaryEntryStruct{}
	for _, e := range entries {
		redacted := !local && e.FabricIndex != fabricIndex
		for _, subjects := range chunked(e.Subjects, int(accessControlSubjectsPerEntry)) {
			for _, targets := range chunked(e.Targets, int(accessControlTargetsPerEntry)) {
				chunk := e
				chunk.Subjects, chunk.Targets = subjects, targets
				out = append(out, AccessControlAuxiliaryEntryStruct{
					Entry:         aclEntryStruct(chunk),
					AuxiliaryType: AccessControlAuxiliaryTypeGroupcast,
					Redacted:      redacted,
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
