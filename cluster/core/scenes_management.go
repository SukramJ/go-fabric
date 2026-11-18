// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"unicode/utf16"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	scenesdef "github.com/SukramJ/go-fabric/cluster/spec/scenesmanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ScenesManagement (0x0062) — a port of matter.js
// packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts.
//
// The scene table, the per-fabric scene info and the attribute capture and
// recall follow matter.js: SceneNames is the only feature, the table holds
// SceneTableSize (128) entries with a per-fabric capacity of
// min((SceneTableSize-1)/2, 253), a scene's group must be 0 or a group with
// a GroupKeyMap entry on the fabric, StoreScene captures the scene-able
// ("S" quality) attributes of the endpoint's other clusters, RecallScene
// applies them through those clusters' commands. The cluster servers are
// rebuilt per dispatch, so the table lives in a ScenesState the endpoint
// keeps.

// Cluster ID, revision, attributes and commands: the generated
// definition's (cluster/spec/scenesmanagement, ADR 0013, from matter.js
// scenes-management.element.ts). The attribute and command lists,
// FeatureMap, ClusterRevision, the invoke privileges and the statuses of a
// refused write come from it as well. The requests keep the bridge's
// hand-written decoders into the request structs below
// (notes/parity/by_design.md BD-Matter-Groups-HandDecoders); the scene
// table is this package's ScenesState.
const (
	ScenesManagementClusterID = scenesdef.ClusterID

	scenesClusterRevision   = scenesdef.Revision
	scenesFeatureSceneNames = uint32(scenesdef.FeatureSceneNames)

	scenesAttrSceneTableSize  = scenesdef.AttrSceneTableSize
	scenesAttrFabricSceneInfo = scenesdef.AttrFabricSceneInfo

	scenesCmdAddScene           = scenesdef.CmdAddScene
	scenesCmdViewScene          = scenesdef.CmdViewScene
	scenesCmdRemoveScene        = scenesdef.CmdRemoveScene
	scenesCmdRemoveAllScenes    = scenesdef.CmdRemoveAllScenes
	scenesCmdStoreScene         = scenesdef.CmdStoreScene
	scenesCmdRecallScene        = scenesdef.CmdRecallScene
	scenesCmdGetSceneMembership = scenesdef.CmdGetSceneMembership
	scenesCmdCopyScene          = scenesdef.CmdCopyScene

	// scenesDefaultTableSize is matter.js's default SceneTableSize
	// (ScenesManagementServer.ts initialize).
	scenesDefaultTableSize uint16 = 128
	// scenesUndefinedSceneID / scenesUndefinedGroup mark "no current scene".
	scenesUndefinedSceneID uint8  = 0xFF
	scenesMaxSceneID       uint8  = 254
	scenesMaxTransition    uint32 = 60000000
	scenesMaxNameLength           = 16
)

// SceneAttributeValue is one stored scene-able attribute value. Null marks
// a nullable attribute's null; otherwise Unsigned or Signed carries it.
type SceneAttributeValue struct {
	AttributeID uint32 `json:"a"`
	Null        bool   `json:"n,omitempty"`
	Signed      bool   `json:"s,omitempty"`
	Unsigned    uint64 `json:"u,omitempty"`
	Int         int64  `json:"i,omitempty"`
}

// SceneClusterValues is the stored values of one cluster.
type SceneClusterValues struct {
	ClusterID uint32                `json:"c"`
	Values    []SceneAttributeValue `json:"v"`
}

// SceneEntry is one scene table entry (matter.js ScenesTableEntry).
type SceneEntry struct {
	FabricIndex    uint8                `json:"f"`
	GroupID        uint16               `json:"g"`
	SceneID        uint8                `json:"s"`
	Name           string               `json:"name,omitempty"`
	TransitionTime uint32               `json:"t"`
	Values         []SceneClusterValues `json:"values"`
}

// SceneInfoStruct is one entry of FabricSceneInfo (§1.4.9.5).
type SceneInfoStruct struct {
	SceneCount        uint8
	CurrentScene      uint8
	CurrentGroup      uint16
	SceneValid        bool
	RemainingCapacity uint8
	FabricIndex       uint8
	// Redacted, when set, makes the bridge's writer send the entry
	// without CurrentScene, CurrentGroup and SceneValid.
	//
	// Deprecated: the dispatcher scopes every fabric-scoped attribute of
	// the model from the schema, whatever the server returns; no server of
	// this module sets it any more. Return the entry whole. Removable in
	// the next minor release.
	Redacted bool
}

// ScenesState is an endpoint's scene table and scene info: the server is
// rebuilt per dispatch, the state outlives it. A state built over a store
// ([NewPersistedScenesState]) loads the table from it and stores every
// change with the change: a change a controller's scene command makes is
// stored before it is served, and a refused store answers FAILURE with the
// previous table kept (store/doc.go, the store-error rule); a change that
// is the consequence of another — a group's or a fabric's removal, the
// global scene OffWithEffect stores — is kept and stored again with the
// next change. A table the store could not load stays unloaded: every
// scene command answers FAILURE and nothing is stored until a load
// succeeds, so a failed load never lets a save write over the stored
// table. [NewScenesState] and [LoadScenesState] build a state over a
// Persist callback that cannot refuse.
type ScenesState struct {
	// mu guards info, monitor, table, loaded, dirty and pending.
	mu        sync.Mutex
	table     []SceneEntry
	info      map[uint8]*SceneInfoStruct
	monitor   uint8 // fabric whose current scene is valid, 0 for none
	tableSize uint16
	// load reads the stored table, nil for a state built loaded; save
	// stores one, nil for none.
	load func(context.Context) ([]byte, error)
	save func(context.Context, []byte) error
	// loaded reports that the table is the stored one; dirty that a
	// consequence's store was refused and the next save owes it; pending
	// are the consequences that reached an unloaded table, applied after
	// its load.
	loaded  bool
	dirty   bool
	pending []scenesPending
	// changes carries FabricSceneInfo change reports: matter.js keeps
	// fabricSceneInfo as behavior state, so every change to a count, the
	// current scene or its validity reaches subscribers.
	changes cluster.AttributeChanges
}

// infoDigest is the FabricSceneInfo content of every fabric with an entry
// or a scene, as a comparable string. Caller holds mu.
func (st *ScenesState) infoDigest() string {
	type row struct {
		F    uint8
		Info SceneInfoStruct
		N    int
	}
	fabrics := map[uint8]bool{}
	for f := range st.info {
		fabrics[f] = true
	}
	for _, e := range st.table {
		fabrics[e.FabricIndex] = true
	}
	rows := make([]row, 0, len(fabrics))
	for f := range fabrics {
		r := row{F: f, N: st.countFor(f)}
		if inf := st.info[f]; inf != nil {
			r.Info = *inf
		}
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b row) int { return int(a.F) - int(b.F) })
	data, _ := json.Marshal(rows)
	return fmt.Sprintf("%d %s", len(st.table), data)
}

// snapshot returns the FabricSceneInfo digest; reportIfChanged reports
// FabricSceneInfo when the digest moved since before. Neither may be called
// with mu held.
func (st *ScenesState) snapshot() string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.infoDigest()
}

func (st *ScenesState) reportIfChanged(before string) {
	if st.snapshot() != before {
		st.changes.Notify(scenesAttrFabricSceneInfo)
	}
}

// logger is the state's logger: the default one, as the state carries no
// configuration of its own.
func (st *ScenesState) logger() *slog.Logger { return slog.Default() }

// scenesPending is a consequence waiting for the table to load. A keyed
// one replaces the earlier one of the same key — a global scene's store,
// which the next one overwrites whole — so repeated consequences while the
// load fails do not grow the list (review II, R2-P6-store-1202).
type scenesPending struct {
	key   string
	apply func()
}

// NewScenesState returns an empty state; persist may be nil.
func NewScenesState(persist func([]byte)) *ScenesState {
	st := &ScenesState{info: map[uint8]*SceneInfoStruct{}, tableSize: scenesDefaultTableSize, loaded: true}
	if persist != nil {
		st.save = func(_ context.Context, data []byte) error {
			persist(data)
			return nil
		}
	}
	return st
}

// LoadScenesState restores a state from data a Persist callback received.
func LoadScenesState(data []byte, persist func([]byte)) (*ScenesState, error) {
	s := NewScenesState(persist)
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.table); err != nil {
		return s, fmt.Errorf("core: ScenesManagement: restore scene table: %w", err)
	}
	s.table = s.validTable(s.table)
	return s, nil
}

// validTable holds a restored table to the rules a scene command is held
// to: an entry whose (fabric, group, scene) an earlier entry holds is
// dropped, the entries beyond SceneTableSize too, and a name longer than
// 16 UTF-16 units is cut to it. What it drops or cuts is logged (review
// II, R2-P6-store-1212).
func (st *ScenesState) validTable(table []SceneEntry) []SceneEntry {
	type key struct {
		f uint8
		g uint16
		s uint8
	}
	seen := make(map[key]bool, len(table))
	out := table[:0]
	duplicates, beyond, cut := 0, 0, 0
	for _, e := range table {
		k := key{e.FabricIndex, e.GroupID, e.SceneID}
		switch {
		case seen[k]:
			duplicates++
			continue
		case len(out) >= int(st.tableSize):
			beyond++
			continue
		}
		seen[k] = true
		if name := []rune(e.Name); len(utf16.Encode(name)) > scenesMaxNameLength {
			for len(utf16.Encode(name)) > scenesMaxNameLength {
				name = name[:len(name)-1]
			}
			e.Name = string(name)
			cut++
		}
		out = append(out, e)
	}
	if duplicates+beyond+cut > 0 {
		st.logger().Warn("core: ScenesManagement: stored scene table held to the rules",
			"duplicates_dropped", duplicates, "beyond_table_size_dropped", beyond, "names_cut", cut)
	}
	return out
}

// NewPersistedScenesState returns a state over a store: load reads the
// stored table (nil for none) and save stores one, each returning the
// store's error. The table is loaded before the first scene command,
// read or change needs it; one the store refuses to load is loaded again
// before the next, and until a load succeeds the state is unloaded (see
// [ScenesState]). A stored table that does not decode loads as empty, and
// the next change replaces it.
func NewPersistedScenesState(load func(context.Context) ([]byte, error), save func(context.Context, []byte) error) *ScenesState {
	return &ScenesState{info: map[uint8]*SceneInfoStruct{}, tableSize: scenesDefaultTableSize, load: load, save: save}
}

// ensureLoaded loads the stored table when the state is not loaded and
// reports whether it is loaded. The consequences that reached the
// unloaded table are applied to the loaded one and stored. Caller holds
// mu.
func (st *ScenesState) ensureLoaded(ctx context.Context) bool {
	if st.loaded {
		return true
	}
	if st.load == nil {
		st.loaded = true
		return true
	}
	data, err := st.load(context.WithoutCancel(ctx))
	if err != nil {
		return false
	}
	st.table = nil
	if len(data) > 0 {
		if err := json.Unmarshal(data, &st.table); err != nil {
			st.table = nil
		}
	}
	st.table = st.validTable(st.table)
	st.loaded = true
	if len(st.pending) > 0 {
		for _, p := range st.pending {
			p.apply()
		}
		st.pending = nil
		st.store(ctx, false)
	}
	return true
}

// ready loads the table when it is not loaded and reports whether it is.
func (st *ScenesState) ready(ctx context.Context) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.ensureLoaded(ctx)
}

// fabricCapacity is matter.js #fabricSceneCapacity.
func (st *ScenesState) fabricCapacity() int {
	return min((int(st.tableSize)-1)/2, 253)
}

func (st *ScenesState) indexOf(fabric uint8, groupID uint16, sceneID uint8) int {
	return slices.IndexFunc(st.table, func(e SceneEntry) bool {
		return e.FabricIndex == fabric && e.GroupID == groupID && e.SceneID == sceneID
	})
}

func (st *ScenesState) countFor(fabric uint8) int {
	n := 0
	for _, e := range st.table {
		if e.FabricIndex == fabric {
			n++
		}
	}
	return n
}

// infoFor returns (creating) the fabric's scene info, counts refreshed.
func (st *ScenesState) infoFor(fabric uint8) *SceneInfoStruct {
	inf := st.info[fabric]
	if inf == nil {
		inf = &SceneInfoStruct{CurrentScene: scenesUndefinedSceneID, FabricIndex: fabric}
		st.info[fabric] = inf
	}
	count := st.countFor(fabric)
	inf.SceneCount = uint8(min(count, 255))                    //nolint:gosec // bounded
	inf.RemainingCapacity = uint8(st.remainingCapacity(count)) //nolint:gosec // bounded by 253
	return inf
}

// remainingCapacity is the number of scenes fabric may still add: its own
// quota left, bounded by the free entries of the endpoint's table, which
// the other fabrics share — chip FabricTableImpl::GetRemainingCapacity
// (src/app/storage/FabricTableImpl.ipp), the reading TC-S-2.6 asserts.
// matter.js #countsForFabric counts the fabric quota only and so lets
// three fabrics store 3×63 scenes in a 128-entry table; this follows the
// spec's "may change … due to other clients associated with other
// fabrics" and chip (notes/parity/by_design.md, ScenesManagement
// RemainingCapacity). count is the fabric's own scene count.
func (st *ScenesState) remainingCapacity(count int) int {
	return max(min(st.fabricCapacity()-count, int(st.tableSize)-len(st.table)), 0)
}

// store stores the table and reports whether the store took it. A refused
// store marks the table owed (dirty) when keep is false — a consequence's
// change, kept and stored again with the next — and leaves the caller to
// restore the previous table when keep is true. Caller holds mu.
func (st *ScenesState) store(ctx context.Context, keep bool) bool {
	if st.save == nil {
		return true
	}
	data, err := json.Marshal(st.table)
	if err == nil {
		err = st.save(context.WithoutCancel(ctx), data)
	}
	switch {
	case err == nil:
		st.dirty = false
		return true
	case !keep:
		st.dirty = true
	}
	return false
}

// change applies mutate to the loaded table and stores it when mutate
// reports a change (or a refused store is owed). A command's
// change (command true) the store refuses is undone: the previous table
// comes back, with fabric's scene info and the fabric whose current scene
// is monitored as they were — a removal that invalidated the current
// scene leaves it valid — and the change answers FAILURE. A consequence's change
// (command false) is kept when the store refuses it, and reaches an
// unloaded table as a pending change. Caller holds mu.
func (st *ScenesState) change(ctx context.Context, command bool, fabric uint8, mutate func() (im.StatusCode, bool)) im.StatusCode {
	return st.changeKeyed(ctx, command, fabric, "", mutate)
}

// changeKeyed is [ScenesState.change] whose pending consequence, when key
// is not empty, replaces the pending one of the same key. Caller holds mu.
func (st *ScenesState) changeKeyed(ctx context.Context, command bool, fabric uint8, key string, mutate func() (im.StatusCode, bool)) im.StatusCode {
	if !st.ensureLoaded(ctx) {
		if command {
			return im.StatusFailure
		}
		if key != "" {
			st.pending = slices.DeleteFunc(st.pending, func(p scenesPending) bool { return p.key == key })
		}
		st.pending = append(st.pending, scenesPending{key: key, apply: func() { mutate() }})
		return im.StatusSuccess
	}
	prev := slices.Clone(st.table)
	prevMonitor := st.monitor
	prevInfo, hadInfo := SceneInfoStruct{}, false
	if inf := st.info[fabric]; inf != nil {
		prevInfo, hadInfo = *inf, true
	}
	status, changed := mutate()
	if status != im.StatusSuccess || !changed && !st.dirty {
		return status
	}
	if !st.store(ctx, command) && command {
		st.table, st.monitor = prev, prevMonitor
		// The fabric's entry as it was: restored and its counts
		// recomputed, or — one the refused command created — gone
		// (infoFor would create it again, so it is not called then).
		if hadInfo {
			*st.info[fabric] = prevInfo
			st.infoFor(fabric)
		} else {
			delete(st.info, fabric)
		}
		return im.StatusFailure
	}
	return im.StatusSuccess
}

// addOrReplace is matter.js #addOrReplaceSceneEntry, stored with the
// change ([ScenesState.change]). Caller holds mu.
func (st *ScenesState) addOrReplace(ctx context.Context, e SceneEntry, existing int) im.StatusCode {
	return st.change(ctx, true, e.FabricIndex, func() (im.StatusCode, bool) {
		if existing == -1 {
			if st.remainingCapacity(st.countFor(e.FabricIndex)) == 0 {
				return im.StatusResourceExhausted, false
			}
			st.table = append(st.table, e)
		} else {
			st.table[existing] = e
		}
		st.infoFor(e.FabricIndex)
		return im.StatusSuccess, true
	})
}

// activate is matter.js #activateSceneInFabricSceneInfo. Caller holds mu.
func (st *ScenesState) activate(fabric uint8, groupID uint16, sceneID uint8) {
	for f, inf := range st.info {
		if f != fabric {
			inf.SceneValid = false
		}
	}
	inf := st.infoFor(fabric)
	inf.CurrentGroup, inf.CurrentScene, inf.SceneValid = groupID, sceneID, true
	st.monitor = fabric
}

// RemoveScenesForGroup drops a fabric's scenes of one group — matter.js
// removeScenesForGroupOnFabric, called by Groups.RemoveGroup — and
// invalidates the current scene if it was one of them.
// A refused store keeps the removal and stores it with the next change.
// No request bounds the store work of this consequence.
func (st *ScenesState) RemoveScenesForGroup(fabric uint8, groupID uint16) {
	st.removeScenesForGroup(context.Background(), false, fabric, groupID)
}

// removeScenesForGroup is RemoveScenesForGroup; a command's removal
// (RemoveAllScenes) the store refuses answers FAILURE with the scenes
// kept.
func (st *ScenesState) removeScenesForGroup(ctx context.Context, command bool, fabric uint8, groupID uint16) im.StatusCode {
	defer st.reportIfChanged(st.snapshot())
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.change(ctx, command, fabric, func() (im.StatusCode, bool) {
		before := len(st.table)
		st.table = slices.DeleteFunc(st.table, func(e SceneEntry) bool { return e.FabricIndex == fabric && e.GroupID == groupID })
		if st.monitor == fabric {
			if inf := st.info[fabric]; inf != nil && inf.CurrentGroup == groupID && inf.SceneValid {
				inf.SceneValid = false
				st.monitor = 0
			}
		}
		st.infoFor(fabric)
		return im.StatusSuccess, len(st.table) != before
	})
}

// RemoveScenesForFabric drops all of a fabric's scenes — matter.js
// removeScenesForAllGroupsForFabric (Groups.RemoveAllGroups) and the fabric
// removal cleanup.
// A refused store keeps the removal and stores it with the next change;
// on a table not loaded yet it is applied once the load succeeds. No
// request bounds the store work of this consequence.
func (st *ScenesState) RemoveScenesForFabric(fabric uint8) {
	st.removeScenesForFabric(context.Background(), fabric)
}

// removeScenesForFabric is RemoveScenesForFabric with ctx for the store.
func (st *ScenesState) removeScenesForFabric(ctx context.Context, fabric uint8) {
	defer st.reportIfChanged(st.snapshot())
	st.mu.Lock()
	defer st.mu.Unlock()
	st.change(ctx, false, fabric, func() (im.StatusCode, bool) {
		before := len(st.table)
		st.table = slices.DeleteFunc(st.table, func(e SceneEntry) bool { return e.FabricIndex == fabric })
		if st.monitor == fabric {
			if inf := st.info[fabric]; inf != nil {
				inf.SceneValid = false
			}
			st.monitor = 0
		}
		st.infoFor(fabric)
		return im.StatusSuccess, len(st.table) != before
	})
}

// ForgetFabric drops everything the table holds for a removed fabric: its
// scenes ([ScenesState.RemoveScenesForFabric]) and its scene info entry,
// so a later fabric with the same index starts without a current scene.
func (st *ScenesState) ForgetFabric(fabric uint8) {
	st.forgetFabric(context.Background(), fabric)
}

// forgetFabric is ForgetFabric with ctx for the store.
func (st *ScenesState) forgetFabric(ctx context.Context, fabric uint8) {
	st.removeScenesForFabric(ctx, fabric)
	st.mu.Lock()
	delete(st.info, fabric)
	st.mu.Unlock()
}

// Fabrics lists the fabrics the table holds scenes or a scene info entry
// for, ascending.
//
// A table not loaded yet is loaded first, so the node's reconcile of a
// fabric removed while the endpoint was away sees the stored fabrics
// (review II, R2-P6-store-1212); while the store refuses the load, only
// the scene info entries are listed.
func (st *ScenesState) Fabrics() []uint8 {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.ensureLoaded(context.Background())
	out := fabricsOf(st.table, func(e SceneEntry) uint8 { return e.FabricIndex })
	for f := range st.info {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// InvalidateCurrentScene is matter.js makeAllFabricSceneInfoEntriesInvalid:
// a command that changes a scene-able attribute ends the current scene's
// validity.
func (st *ScenesState) InvalidateCurrentScene() {
	defer st.reportIfChanged(st.snapshot())
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.monitor == 0 {
		return
	}
	if inf := st.info[st.monitor]; inf != nil {
		inf.SceneValid = false
	}
	st.monitor = 0
}

// ScenesConfig wires a ScenesManagement server to its endpoint.
type ScenesConfig struct {
	// State is the endpoint's scene table; required.
	State *ScenesState
	// Siblings returns the endpoint's other cluster servers, whose
	// scene-able attributes a scene captures and recalls.
	Siblings func() []contract.ClusterServer
	// GroupKnown reports whether the fabric has a GroupKeyMap entry for
	// the group — matter.js #groupExistentInFabric. Nil accepts only group 0.
	GroupKnown func(ctx context.Context, fabric uint8, groupID uint16) bool
	// Fabrics lists the node's fabrics, for FabricSceneInfo; nil serves the
	// accessing fabric's entry alone.
	Fabrics func(ctx context.Context) []uint8
}

// ScenesManagement is the server.
type ScenesManagement struct {
	cfg ScenesConfig
}

// NewScenesManagement returns the server; cfg.State is required.
func NewScenesManagement(cfg ScenesConfig) (*ScenesManagement, error) {
	if cfg.State == nil {
		return nil, errors.New("core: ScenesManagement: state is required")
	}
	return &ScenesManagement{cfg: cfg}, nil
}

var (
	_ contract.ClusterServer                 = (*ScenesManagement)(nil)
	_ contract.ClusterAttributeLister        = (*ScenesManagement)(nil)
	_ contract.ClusterCommandLister          = (*ScenesManagement)(nil)
	_ contract.FabricScopedReader            = (*ScenesManagement)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*ScenesManagement)(nil)
	_ contract.AttributeChangeNotifier       = (*ScenesManagement)(nil)
	_ contract.FabricScopedState             = (*ScenesManagement)(nil)
)

// The global scene: matter.js GLOBAL_SCENE_ID in UNDEFINED_GROUP
// (ScenesManagementServer.ts:77-80), the scene OnOff's OffWithEffect
// stores and OnWithRecallGlobalScene recalls.
const (
	scenesGlobalSceneID   uint8  = 0
	scenesUndefinedGroup  uint16 = 0
	scenesGlobalSceneName        = "Global Scene"
)

// MakeAllFabricSceneInfoEntriesInvalid mirrors matter.js
// ScenesManagementServer.makeAllFabricSceneInfoEntriesInvalid
// (ScenesManagementServer.ts:966-975), the call OnOffServer makes when a
// command changes OnOff (OnOffServer.ts:114-117): the current scene is no
// longer valid. One of the three operations of the OnOff server's scenes
// port (cluster/onoff Scenes).
func (s *ScenesManagement) MakeAllFabricSceneInfoEntriesInvalid() {
	s.cfg.State.InvalidateCurrentScene()
}

// StoreGlobalScene mirrors matter.js storeGlobalScene
// (ScenesManagementServer.ts:563-582), called by OnOffServer.offWithEffect
// while GlobalSceneControl is true (OnOffServer.ts:159-165): the endpoint's
// scene-able values are stored as the fabric's global scene — a new entry
// named "Global Scene" with transition time 0, or the existing entry with
// its values replaced. The current scene does not change.
func (s *ScenesManagement) StoreGlobalScene(ctx context.Context, fabricIndex uint8) {
	st := s.cfg.State
	defer st.reportIfChanged(st.snapshot())
	values := s.captureValues()
	st.mu.Lock()
	defer st.mu.Unlock()
	// matter.js ignores the status of #addOrReplaceSceneEntry here: a
	// fabric at capacity stores no global scene. It is the consequence of
	// OffWithEffect, which a refused store does not fail: the scene is
	// kept and stored with the next change. The existing entry is looked
	// up when the change applies — on the loaded table, also when the
	// change waited for the load — never on a table not loaded yet, where
	// it would be missed and a second (fabric, 0, 0) entry appended
	// (review II, R2-P6-store-1202).
	_ = st.changeKeyed(ctx, false, fabricIndex, fmt.Sprintf("global/%d", fabricIndex), func() (im.StatusCode, bool) {
		entry := SceneEntry{
			FabricIndex: fabricIndex, GroupID: scenesUndefinedGroup, SceneID: scenesGlobalSceneID,
			Name: scenesGlobalSceneName, TransitionTime: 0, Values: values,
		}
		switch i := st.indexOf(fabricIndex, scenesUndefinedGroup, scenesGlobalSceneID); {
		case i != -1:
			st.table[i].Values = values
		case st.remainingCapacity(st.countFor(fabricIndex)) == 0:
			return im.StatusResourceExhausted, false
		default:
			st.table = append(st.table, entry)
		}
		st.infoFor(fabricIndex)
		return im.StatusSuccess, true
	})
}

// RecallGlobalScene mirrors matter.js recallGlobalScene
// (ScenesManagementServer.ts:552-561), called by
// OnOffServer.onWithRecallGlobalScene (OnOffServer.ts:175-184): the
// fabric's global scene, if stored, is applied with its transition time and
// becomes the fabric's current scene. ctx is passed on to the servers that
// apply the values.
func (s *ScenesManagement) RecallGlobalScene(ctx context.Context, fabricIndex uint8) {
	st := s.cfg.State
	defer st.reportIfChanged(st.snapshot())
	st.mu.Lock()
	// The table is loaded first: on one nothing has loaded since start the
	// stored global scene would be missed (review II, R2-P6-store-1203).
	if !st.ensureLoaded(ctx) {
		st.mu.Unlock()
		st.logger().Warn("core: ScenesManagement: OnWithRecallGlobalScene recalls nothing: the scene table does not load",
			"fabric", fabricIndex)
		return
	}
	i := st.indexOf(fabricIndex, scenesUndefinedGroup, scenesGlobalSceneID)
	if i == -1 {
		st.mu.Unlock()
		return
	}
	e := st.table[i]
	st.mu.Unlock()
	s.applyValues(ctx, e.Values, e.TransitionTime)
	st.mu.Lock()
	st.activate(fabricIndex, scenesUndefinedGroup, scenesGlobalSceneID)
	st.mu.Unlock()
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]:
// FabricSceneInfo reports whenever a scene count, the current scene or its
// validity changes (matter.js fabricSceneInfo is behavior state). The
// listeners live on the endpoint's [ScenesState], which outlives the server.
func (s *ScenesManagement) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.cfg.State.changes.OnAttributeChange(cb)
}

// MatterClusterID implements [contract.ClusterServer].
func (s *ScenesManagement) MatterClusterID() uint32 { return ScenesManagementClusterID }

// MatterDropFabric implements [contract.FabricScopedState]: the endpoint's
// table forgets the fabric ([ScenesState.ForgetFabric]); a removal the
// store refuses is kept and stored with the next change, and one that
// reaches a table not loaded yet is applied once it loads.
func (s *ScenesManagement) MatterDropFabric(ctx context.Context, fabricIndex uint8) error {
	s.cfg.State.forgetFabric(ctx, fabricIndex)
	return nil
}

// MatterHeldFabrics implements [contract.FabricScopedState]
// ([ScenesState.Fabrics]).
func (s *ScenesManagement) MatterHeldFabrics() []uint8 { return s.cfg.State.Fabrics() }

// MatterRead implements [contract.ClusterServer]: a read without a
// request behind it, which sees every fabric's entry whole.
func (s *ScenesManagement) MatterRead(attrID uint32) (any, bool) {
	return s.MatterReadFiltered(context.Background(), attrID)
}

// MatterReadFiltered implements [contract.FabricScopedReader]:
// FabricSceneInfo is fabric-scoped ("R F V"). A fabric-filtered read lists
// the accessing fabric's entry; an unfiltered one every fabric's, each
// whole — the dispatcher withholds the fabric-sensitive fields of another
// fabric's entry from a request, as it scopes every fabric-scoped
// attribute of the model.
func (s *ScenesManagement) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	st := s.cfg.State
	switch attrID {
	case scenesAttrSceneTableSize:
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.tableSize, true
	case scenesAttrFabricSceneInfo:
		// A table not loaded yet is loaded for the read; one the store
		// still refuses fails the read and stays unloaded — answered
		// FAILURE on a request ([contract.FabricScopedReader]'s (nil,
		// false)), not SceneCount 0 with the whole capacity free, which
		// the commands' FAILURE would contradict. Mirrors connectedhomeip
		// src/app/clusters/scenes-server/ScenesManagementCluster.cpp:425-436
		// at 6170af84 (the FabricSceneInfo encoder returns the scene
		// table's GetRemainingCapacity error) and
		// src/app/storage/FabricTableImpl.ipp:578-581, :596-597 (a storage
		// error is returned; only NOT_FOUND reads as an empty fabric).
		// matter.js models no unloaded table.
		if !st.ready(ctx) {
			return nil, false
		}
		filtered, fabric, _ := im.LookupFabricFilter(ctx)
		// Every fabric of the node has an entry, created on read
		// (infoFor), also one that never stored a scene. matter.js holds
		// one for each fabric known when the server initialises and adds
		// one only when a fabric's scene count is updated
		// (ScenesManagementServer.ts:978-999 #initializeFabricSceneInfo,
		// :1003-1023 #updateFabricSceneInfoCountsForFabric at db021da0;
		// a read creates none), so a fabric commissioned after start
		// reads an empty list until its first scene or a restart. This
		// module serves the entry from the start, on purpose (the
		// by_design entry for FabricSceneInfo). Without the fabric list
		// the accessing fabric still sees its own entry (W3-clucore-14).
		var fabrics []uint8
		if s.cfg.Fabrics != nil {
			fabrics = s.cfg.Fabrics(ctx)
		} else if fabric != 0 {
			fabrics = []uint8{fabric}
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		out := []SceneInfoStruct{}
		for _, f := range fabrics {
			if filtered && f != fabric {
				continue
			}
			out = append(out, *st.infoFor(f))
		}
		return out, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return scenesInst.ReadGlobal(attrID)
	}
	return nil, false
}

// scenesInst is the definition bound to SN (scene names) and the optional
// CopyScene, as the default ScenesManagementServer serves them.
var scenesInst = mustInstance(scenesdef.Definition, spec.Options{
	Features: scenesFeatureSceneNames,
	Commands: []uint32{scenesCmdCopyScene},
})

// MatterWrite implements [contract.ClusterServer]: nothing is writable.
func (s *ScenesManagement) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := scenesInst.ValidateWrite(attrID, value, nil)
	return refusedWrite{sentinel: fmt.Errorf("core: ScenesManagement attribute 0x%04X is read-only", attrID), status: err}
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// server stores no written value, so no write changes it.
func (s *ScenesManagement) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return false, s.MatterWrite(ctx, attrID, value)
}

// MatterReportable implements [contract.ClusterServer].
func (s *ScenesManagement) MatterReportable() []uint32 {
	return []uint32{scenesAttrSceneTableSize, scenesAttrFabricSceneInfo}
}

// MatterAttributes implements [contract.ClusterAttributeLister].
func (s *ScenesManagement) MatterAttributes() []uint32 { return scenesInst.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *ScenesManagement) MatterAcceptedCommands() []uint32 {
	return scenesInst.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]:
// every request but RecallScene has a response of its own id.
func (s *ScenesManagement) MatterGeneratedCommands() []uint32 {
	return scenesInst.MatterGeneratedCommands()
}

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege]:
// the definition's — Manage for the table-changing commands, Operate
// otherwise ("F M" / "F O").
func (s *ScenesManagement) MinInvokePrivilege(cmdID uint32) uint8 {
	return scenesInst.MinInvokePrivilege(cmdID)
}

// Command payloads (scenes-management.element.ts field ids).
type (
	// AttributeValuePair is one AttributeValuePairStruct: AttributeID plus
	// exactly one value field, named by its tag (1 ValueUnsigned8 … 8
	// ValueSigned64). Tag 0 means none was present.
	AttributeValuePair struct {
		AttributeID uint32
		Tag         uint8
		Unsigned    uint64
		Signed      int64
		Fields      int // value fields present; matter.js wants exactly one
	}
	// ExtensionFieldSet is one ExtensionFieldSetStruct.
	ExtensionFieldSet struct {
		ClusterID          uint32
		AttributeValueList []AttributeValuePair
	}
	// AddSceneRequest is AddScene (0x00).
	AddSceneRequest struct {
		GroupID            uint16
		SceneID            uint8
		TransitionTime     uint32
		SceneName          string
		ExtensionFieldSets []ExtensionFieldSet
	}
	// SceneRef is ViewScene / RemoveScene / StoreScene (group, scene).
	SceneRef struct {
		GroupID uint16
		SceneID uint8
	}
	// RecallSceneRequest is RecallScene (0x05); TransitionTime nil is null.
	RecallSceneRequest struct {
		GroupID        uint16
		SceneID        uint8
		TransitionTime *uint32
	}
	// SceneGroupRequest is RemoveAllScenes / GetSceneMembership (group).
	SceneGroupRequest struct {
		GroupID uint16
	}
	// CopySceneRequest is CopyScene (0x40).
	CopySceneRequest struct {
		CopyAllScenes bool
		GroupFrom     uint16
		SceneFrom     uint8
		GroupTo       uint16
		SceneTo       uint8
	}
	// SceneStatusResponse is AddScene/RemoveScene/StoreScene response.
	SceneStatusResponse struct {
		Command uint32
		Status  im.StatusCode
		GroupID uint16
		SceneID uint8
	}
	// ViewSceneResponse is ViewSceneResponse (0x01).
	ViewSceneResponse struct {
		Status             im.StatusCode
		GroupID            uint16
		SceneID            uint8
		TransitionTime     uint32
		SceneName          string
		ExtensionFieldSets []ExtensionFieldSet
	}
	// RemoveAllScenesResponse is RemoveAllScenesResponse (0x03).
	RemoveAllScenesResponse struct {
		Status  im.StatusCode
		GroupID uint16
	}
	// GetSceneMembershipResponse is GetSceneMembershipResponse (0x06);
	// Capacity nil is null.
	GetSceneMembershipResponse struct {
		Status    im.StatusCode
		Capacity  *uint8
		GroupID   uint16
		SceneList []uint8
		HasList   bool
	}
	// CopySceneResponse is CopySceneResponse (0x40).
	CopySceneResponse struct {
		Status    im.StatusCode
		GroupFrom uint16
		SceneFrom uint8
	}
)

// scenesStatusError answers a command with a plain status.
type scenesStatusError struct {
	status im.StatusCode
	msg    string
}

func (e scenesStatusError) Error() string                   { return "core: ScenesManagement: " + e.msg }
func (e scenesStatusError) MatterStatusCode() im.StatusCode { return e.status }

// MatterInvoke implements [contract.ClusterServer]. Every command is
// fabric-scoped.
func (s *ScenesManagement) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	_, fabric := im.FabricFilterFromContext(ctx)
	if fabric == 0 {
		return nil, scenesStatusError{im.StatusUnsupportedAccess, "command requires an accessing fabric"}
	}
	if err := groupZeroInGroupMessage(ctx, fields); err != nil {
		return nil, err
	}
	defer s.cfg.State.reportIfChanged(s.cfg.State.snapshot())
	// A table the store did not load answers every scene command with
	// FAILURE until a load succeeds: a command on the empty table would
	// answer NOT_FOUND for a stored scene, and its change would be stored
	// over the stored table.
	if !s.cfg.State.ready(ctx) {
		return nil, scenesStatusError{im.StatusFailure, "the scene table is not loaded from the store"}
	}
	bad := func() (any, error) {
		return nil, scenesStatusError{im.StatusInvalidCommand, fmt.Sprintf("command 0x%02X: unexpected fields %T", cmdID, fields)}
	}
	switch cmdID {
	case scenesCmdAddScene:
		req, ok := fields.(AddSceneRequest)
		if !ok {
			return bad()
		}
		return s.addScene(ctx, fabric, req), nil
	case scenesCmdViewScene:
		req, ok := fields.(SceneRef)
		if !ok {
			return bad()
		}
		return s.viewScene(ctx, fabric, req), nil
	case scenesCmdRemoveScene:
		req, ok := fields.(SceneRef)
		if !ok {
			return bad()
		}
		return s.removeScene(ctx, fabric, req), nil
	case scenesCmdRemoveAllScenes:
		req, ok := fields.(SceneGroupRequest)
		if !ok {
			return bad()
		}
		return s.removeAllScenes(ctx, fabric, req.GroupID), nil
	case scenesCmdStoreScene:
		req, ok := fields.(SceneRef)
		if !ok {
			return bad()
		}
		return s.storeScene(ctx, fabric, req), nil
	case scenesCmdRecallScene:
		req, ok := fields.(RecallSceneRequest)
		if !ok {
			return bad()
		}
		return nil, s.recallScene(ctx, fabric, req)
	case scenesCmdGetSceneMembership:
		req, ok := fields.(SceneGroupRequest)
		if !ok {
			return bad()
		}
		return s.getSceneMembership(ctx, fabric, req.GroupID), nil
	case scenesCmdCopyScene:
		req, ok := fields.(CopySceneRequest)
		if !ok {
			return bad()
		}
		return s.copyScene(ctx, fabric, req), nil
	}
	return nil, im.UnsupportedCommandf("core: ScenesManagement command 0x%02X not supported", cmdID)
}

// groupZeroInGroupMessage refuses a scene command that arrived as a group
// message with a GroupID of 0. Mirrors matter.js
// packages/node/src/behaviors/scenes-management/ScenesManagementServer.ts:229-234
// at db021da0 (#assertSceneCommandParameter, which every scene command
// calls, CopyScene for both of its groups, :491-499): `if (isGroupSession
// && groupIdToValidate === 0) throw new StatusResponse.InvalidCommandError`.
// The check runs before the command's own, on the request of any of the
// eight commands (a payload of another shape is left to the command's
// decoding); matter.js runs some of those
// first (AddScene's and RecallScene's range checks, :283-285, :435-440),
// which a group message, answered by no response, cannot tell apart —
// either way nothing changes.
func groupZeroInGroupMessage(ctx context.Context, fields any) error {
	if _, group := im.GroupSubjectFromContext(ctx); !group {
		return nil
	}
	var groupIDs []uint16
	switch req := fields.(type) {
	case AddSceneRequest:
		groupIDs = []uint16{req.GroupID}
	case SceneRef:
		groupIDs = []uint16{req.GroupID}
	case RecallSceneRequest:
		groupIDs = []uint16{req.GroupID}
	case SceneGroupRequest:
		groupIDs = []uint16{req.GroupID}
	case CopySceneRequest:
		groupIDs = []uint16{req.GroupFrom, req.GroupTo}
	}
	for _, id := range groupIDs {
		if id == 0 {
			return scenesStatusError{im.StatusInvalidCommand, "GroupID cannot be 0 in a group message"}
		}
	}
	return nil
}

// groupValid is matter.js #assertSceneCommandParameter's group check.
func (s *ScenesManagement) groupValid(ctx context.Context, fabric uint8, groupID uint16) bool {
	if groupID == 0 {
		return true
	}
	return s.cfg.GroupKnown != nil && s.cfg.GroupKnown(ctx, fabric, groupID)
}

func (s *ScenesManagement) addScene(ctx context.Context, fabric uint8, req AddSceneRequest) SceneStatusResponse {
	resp := SceneStatusResponse{Command: scenesCmdAddScene, GroupID: req.GroupID, SceneID: req.SceneID}
	// A SceneName over 16 is refused with the SceneID and TransitionTime
	// bounds, as connectedhomeip refuses it
	// (src/app/clusters/scenes-server/ScenesManagementCluster.cpp:535-541
	// at 6170af84); it used to be cut to 16 and stored with SUCCESS.
	// matter.js relaxes the request field so that its behaviour answers
	// the status (ScenesManagementServer.ts:129-147 at db021da0) but then
	// checks only SceneID and TransitionTime (:283); see
	// notes/parity/by_design.md BD-chip-ScenesManagement-SceneNameLength.
	// The length counts UTF-16 units, as matter.js's string constraint
	// counts them (constraint.ts:200-211).
	if req.SceneID > scenesMaxSceneID || req.TransitionTime > scenesMaxTransition || len(utf16.Encode([]rune(req.SceneName))) > scenesMaxNameLength {
		resp.Status = im.StatusConstraintError
		return resp
	}
	if !s.groupValid(ctx, fabric, req.GroupID) {
		resp.Status = im.StatusInvalidCommand
		return resp
	}
	values, ok := s.decodeExtensionFieldSets(req.ExtensionFieldSets)
	if !ok {
		resp.Status = im.StatusInvalidCommand
		return resp
	}
	name := req.SceneName
	st := s.cfg.State
	st.mu.Lock()
	defer st.mu.Unlock()
	resp.Status = st.addOrReplace(ctx, SceneEntry{
		FabricIndex: fabric, GroupID: req.GroupID, SceneID: req.SceneID, Name: name,
		TransitionTime: req.TransitionTime, Values: values,
	}, st.indexOf(fabric, req.GroupID, req.SceneID))
	return resp
}

func (s *ScenesManagement) viewScene(ctx context.Context, fabric uint8, req SceneRef) ViewSceneResponse {
	resp := ViewSceneResponse{GroupID: req.GroupID, SceneID: req.SceneID}
	if req.SceneID > scenesMaxSceneID {
		resp.Status = im.StatusConstraintError
		return resp
	}
	if !s.groupValid(ctx, fabric, req.GroupID) {
		resp.Status = im.StatusInvalidCommand
		return resp
	}
	st := s.cfg.State
	st.mu.Lock()
	defer st.mu.Unlock()
	i := st.indexOf(fabric, req.GroupID, req.SceneID)
	if i == -1 {
		resp.Status = im.StatusNotFound
		return resp
	}
	e := st.table[i]
	resp.Status, resp.TransitionTime, resp.SceneName = im.StatusSuccess, e.TransitionTime, e.Name
	resp.ExtensionFieldSets = s.encodeExtensionFieldSets(e.Values)
	return resp
}

func (s *ScenesManagement) removeScene(ctx context.Context, fabric uint8, req SceneRef) SceneStatusResponse {
	resp := SceneStatusResponse{Command: scenesCmdRemoveScene, GroupID: req.GroupID, SceneID: req.SceneID}
	if req.SceneID > scenesMaxSceneID {
		resp.Status = im.StatusConstraintError
		return resp
	}
	if !s.groupValid(ctx, fabric, req.GroupID) {
		resp.Status = im.StatusInvalidCommand
		return resp
	}
	st := s.cfg.State
	st.mu.Lock()
	defer st.mu.Unlock()
	resp.Status = st.change(ctx, true, fabric, func() (im.StatusCode, bool) {
		i := st.indexOf(fabric, req.GroupID, req.SceneID)
		if i == -1 {
			return im.StatusNotFound, false
		}
		st.table = slices.Delete(st.table, i, i+1)
		if st.monitor == fabric {
			if inf := st.info[fabric]; inf != nil && inf.CurrentGroup == req.GroupID && inf.CurrentScene == req.SceneID && inf.SceneValid {
				inf.SceneValid = false
				st.monitor = 0
			}
		}
		st.infoFor(fabric)
		return im.StatusSuccess, true
	})
	return resp
}

func (s *ScenesManagement) removeAllScenes(ctx context.Context, fabric uint8, groupID uint16) RemoveAllScenesResponse {
	if !s.groupValid(ctx, fabric, groupID) {
		return RemoveAllScenesResponse{Status: im.StatusInvalidCommand, GroupID: groupID}
	}
	return RemoveAllScenesResponse{Status: s.cfg.State.removeScenesForGroup(ctx, true, fabric, groupID), GroupID: groupID}
}

func (s *ScenesManagement) storeScene(ctx context.Context, fabric uint8, req SceneRef) SceneStatusResponse {
	resp := SceneStatusResponse{Command: scenesCmdStoreScene, GroupID: req.GroupID, SceneID: req.SceneID}
	if req.SceneID > scenesMaxSceneID {
		resp.Status = im.StatusConstraintError
		return resp
	}
	if !s.groupValid(ctx, fabric, req.GroupID) {
		resp.Status = im.StatusInvalidCommand
		return resp
	}
	values := s.captureValues()
	st := s.cfg.State
	st.mu.Lock()
	defer st.mu.Unlock()
	i := st.indexOf(fabric, req.GroupID, req.SceneID)
	entry := SceneEntry{FabricIndex: fabric, GroupID: req.GroupID, SceneID: req.SceneID, Values: values}
	if i != -1 {
		// An existing scene keeps its name and transition time; only its
		// values are replaced.
		entry = st.table[i]
		entry.Values = values
	}
	resp.Status = st.addOrReplace(ctx, entry, i)
	if resp.Status == im.StatusSuccess {
		st.activate(fabric, req.GroupID, req.SceneID)
	}
	return resp
}

func (s *ScenesManagement) recallScene(ctx context.Context, fabric uint8, req RecallSceneRequest) error {
	if req.SceneID > scenesMaxSceneID || (req.TransitionTime != nil && *req.TransitionTime > scenesMaxTransition) {
		return scenesStatusError{im.StatusConstraintError, "RecallScene out of range"}
	}
	if !s.groupValid(ctx, fabric, req.GroupID) {
		return scenesStatusError{im.StatusInvalidCommand, fmt.Sprintf("invalid group %d", req.GroupID)}
	}
	st := s.cfg.State
	st.mu.Lock()
	i := st.indexOf(fabric, req.GroupID, req.SceneID)
	if i == -1 {
		st.mu.Unlock()
		return scenesStatusError{im.StatusNotFound, fmt.Sprintf("scene %d in group %d not found", req.SceneID, req.GroupID)}
	}
	e := st.table[i]
	st.mu.Unlock()
	tt := e.TransitionTime
	if req.TransitionTime != nil {
		tt = *req.TransitionTime
	}
	s.applyValues(ctx, e.Values, tt)
	st.mu.Lock()
	st.activate(fabric, req.GroupID, req.SceneID)
	st.mu.Unlock()
	return nil
}

func (s *ScenesManagement) getSceneMembership(ctx context.Context, fabric uint8, groupID uint16) GetSceneMembershipResponse {
	if !s.groupValid(ctx, fabric, groupID) {
		return GetSceneMembershipResponse{Status: im.StatusInvalidCommand, GroupID: groupID}
	}
	st := s.cfg.State
	st.mu.Lock()
	defer st.mu.Unlock()
	capacity := uint8(min(st.remainingCapacity(st.countFor(fabric)), 0xFE)) //nolint:gosec // bounded
	list := []uint8{}
	for _, e := range st.table {
		if e.FabricIndex == fabric && e.GroupID == groupID {
			list = append(list, e.SceneID)
		}
	}
	return GetSceneMembershipResponse{Status: im.StatusSuccess, Capacity: &capacity, GroupID: groupID, SceneList: list, HasList: true}
}

func (s *ScenesManagement) copyScene(ctx context.Context, fabric uint8, req CopySceneRequest) CopySceneResponse {
	resp := CopySceneResponse{GroupFrom: req.GroupFrom, SceneFrom: req.SceneFrom}
	if !s.groupValid(ctx, fabric, req.GroupFrom) || !s.groupValid(ctx, fabric, req.GroupTo) {
		resp.Status = im.StatusInvalidCommand
		return resp
	}
	st := s.cfg.State
	st.mu.Lock()
	defer st.mu.Unlock()
	if req.CopyAllScenes {
		var from []SceneEntry
		for _, e := range st.table {
			if e.FabricIndex == fabric && e.GroupID == req.GroupFrom {
				from = append(from, e)
			}
		}
		for _, e := range from {
			e.GroupID = req.GroupTo
			if status := st.addOrReplace(ctx, e, st.indexOf(fabric, req.GroupTo, e.SceneID)); status != im.StatusSuccess {
				resp.Status = status
				return resp
			}
		}
		resp.Status = im.StatusSuccess
		return resp
	}
	if req.SceneTo > scenesMaxSceneID || req.SceneFrom > scenesMaxSceneID {
		resp.Status = im.StatusConstraintError
		return resp
	}
	i := st.indexOf(fabric, req.GroupFrom, req.SceneFrom)
	if i == -1 {
		resp.Status = im.StatusNotFound
		return resp
	}
	e := st.table[i]
	e.GroupID, e.SceneID = req.GroupTo, req.SceneTo
	resp.Status = st.addOrReplace(ctx, e, st.indexOf(fabric, req.GroupTo, req.SceneTo))
	return resp
}
