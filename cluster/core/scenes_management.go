// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

// ScenesState is an endpoint's scene table and scene info: the server is
// rebuilt per dispatch, the state outlives it. Persist, when set, receives
// the table after every change (the host stores it); LoadScenesState
// restores one.
type ScenesState struct {
	mu        sync.Mutex
	table     []SceneEntry
	info      map[uint8]*SceneInfoStruct
	monitor   uint8 // fabric whose current scene is valid, 0 for none
	tableSize uint16
	persist   func([]byte)
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

// NewScenesState returns an empty state; persist may be nil.
func NewScenesState(persist func([]byte)) *ScenesState {
	return &ScenesState{info: map[uint8]*SceneInfoStruct{}, tableSize: scenesDefaultTableSize, persist: persist}
}

// LoadScenesState restores a state from data a Persist callback received.
func LoadScenesState(data []byte, persist func([]byte)) (*ScenesState, error) {
	s := NewScenesState(persist)
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.table); err != nil {
		return s, fmt.Errorf("matter: ScenesManagement: restore scene table: %w", err)
	}
	return s, nil
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

func (st *ScenesState) save() {
	if st.persist == nil {
		return
	}
	if data, err := json.Marshal(st.table); err == nil {
		st.persist(data)
	}
}

// addOrReplace is matter.js #addOrReplaceSceneEntry. Caller holds mu.
func (st *ScenesState) addOrReplace(e SceneEntry, existing int) im.StatusCode {
	if existing == -1 {
		if st.remainingCapacity(st.countFor(e.FabricIndex)) == 0 {
			return im.StatusResourceExhausted
		}
		st.table = append(st.table, e)
	} else {
		st.table[existing] = e
	}
	st.infoFor(e.FabricIndex)
	st.save()
	return im.StatusSuccess
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
func (st *ScenesState) RemoveScenesForGroup(fabric uint8, groupID uint16) {
	defer st.reportIfChanged(st.snapshot())
	st.mu.Lock()
	defer st.mu.Unlock()
	before := len(st.table)
	st.table = slices.DeleteFunc(st.table, func(e SceneEntry) bool { return e.FabricIndex == fabric && e.GroupID == groupID })
	if st.monitor == fabric {
		if inf := st.info[fabric]; inf != nil && inf.CurrentGroup == groupID && inf.SceneValid {
			inf.SceneValid = false
			st.monitor = 0
		}
	}
	st.infoFor(fabric)
	if len(st.table) != before {
		st.save()
	}
}

// RemoveScenesForFabric drops all of a fabric's scenes — matter.js
// removeScenesForAllGroupsForFabric (Groups.RemoveAllGroups) and the fabric
// removal cleanup.
func (st *ScenesState) RemoveScenesForFabric(fabric uint8) {
	defer st.reportIfChanged(st.snapshot())
	st.mu.Lock()
	defer st.mu.Unlock()
	before := len(st.table)
	st.table = slices.DeleteFunc(st.table, func(e SceneEntry) bool { return e.FabricIndex == fabric })
	if st.monitor == fabric {
		if inf := st.info[fabric]; inf != nil {
			inf.SceneValid = false
		}
		st.monitor = 0
	}
	st.infoFor(fabric)
	if len(st.table) != before {
		st.save()
	}
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
	// Fabrics lists the node's fabrics, for FabricSceneInfo.
	Fabrics func(ctx context.Context) []uint8
}

// ScenesManagement is the server.
type ScenesManagement struct {
	cfg ScenesConfig
}

// NewScenesManagement returns the server; cfg.State is required.
func NewScenesManagement(cfg ScenesConfig) (*ScenesManagement, error) {
	if cfg.State == nil {
		return nil, errors.New("matter: ScenesManagement: state is required")
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
)

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier]:
// FabricSceneInfo reports whenever a scene count, the current scene or its
// validity changes (matter.js fabricSceneInfo is behavior state). The
// listeners live on the endpoint's [ScenesState], which outlives the server.
func (s *ScenesManagement) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.cfg.State.changes.OnMatterAttributesChanged(cb)
}

// MatterClusterID implements [contract.ClusterServer].
func (s *ScenesManagement) MatterClusterID() uint32 { return ScenesManagementClusterID }

// MatterRead implements [contract.ClusterServer] (unfiltered).
func (s *ScenesManagement) MatterRead(attrID uint32) (any, bool) {
	return s.MatterReadFiltered(im.WithFabricFilter(context.Background(), false, 0), attrID)
}

// MatterReadFiltered implements [contract.FabricScopedReader]:
// FabricSceneInfo is fabric-scoped ("R F V").
func (s *ScenesManagement) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	st := s.cfg.State
	switch attrID {
	case scenesAttrSceneTableSize:
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.tableSize, true
	case scenesAttrFabricSceneInfo:
		filtered, fabric := im.FabricFilterFromContext(ctx)
		var fabrics []uint8
		if s.cfg.Fabrics != nil {
			fabrics = s.cfg.Fabrics(ctx)
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
	return refusedWrite{sentinel: fmt.Errorf("matter: ScenesManagement attribute 0x%04X is read-only", attrID), status: err}
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

func (e scenesStatusError) Error() string                   { return "matter: ScenesManagement: " + e.msg }
func (e scenesStatusError) MatterStatusCode() im.StatusCode { return e.status }

// MatterInvoke implements [contract.ClusterServer]. Every command is
// fabric-scoped.
func (s *ScenesManagement) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	_, fabric := im.FabricFilterFromContext(ctx)
	if fabric == 0 {
		return nil, scenesStatusError{im.StatusUnsupportedAccess, "command requires an accessing fabric"}
	}
	defer s.cfg.State.reportIfChanged(s.cfg.State.snapshot())
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
	return nil, im.UnsupportedCommandf("matter: ScenesManagement command 0x%02X not supported", cmdID)
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
	if req.SceneID > scenesMaxSceneID || req.TransitionTime > scenesMaxTransition {
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
	if len(utf16.Encode([]rune(name))) > scenesMaxNameLength {
		name = string(utf16.Decode(utf16.Encode([]rune(name))[:scenesMaxNameLength]))
	}
	st := s.cfg.State
	st.mu.Lock()
	defer st.mu.Unlock()
	resp.Status = st.addOrReplace(SceneEntry{
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
	i := st.indexOf(fabric, req.GroupID, req.SceneID)
	if i == -1 {
		resp.Status = im.StatusNotFound
		return resp
	}
	st.table = slices.Delete(st.table, i, i+1)
	if st.monitor == fabric {
		if inf := st.info[fabric]; inf != nil && inf.CurrentGroup == req.GroupID && inf.CurrentScene == req.SceneID && inf.SceneValid {
			inf.SceneValid = false
			st.monitor = 0
		}
	}
	st.infoFor(fabric)
	st.save()
	resp.Status = im.StatusSuccess
	return resp
}

func (s *ScenesManagement) removeAllScenes(ctx context.Context, fabric uint8, groupID uint16) RemoveAllScenesResponse {
	if !s.groupValid(ctx, fabric, groupID) {
		return RemoveAllScenesResponse{Status: im.StatusInvalidCommand, GroupID: groupID}
	}
	s.cfg.State.RemoveScenesForGroup(fabric, groupID)
	return RemoveAllScenesResponse{Status: im.StatusSuccess, GroupID: groupID}
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
	resp.Status = st.addOrReplace(entry, i)
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
			if status := st.addOrReplace(e, st.indexOf(fabric, req.GroupTo, e.SceneID)); status != im.StatusSuccess {
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
	resp.Status = st.addOrReplace(e, st.indexOf(fabric, req.GroupTo, req.SceneTo))
	return resp
}
