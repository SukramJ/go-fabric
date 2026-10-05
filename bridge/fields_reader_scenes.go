// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"fmt"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// ScenesManagement (0x0062) command payloads (scenes-management.element.ts).
// The server relaxes SceneId, TransitionTime and SceneName constraints and
// answers them in its responses (ScenesManagementServer.ts schema
// extension), so the decoders only check the wire types.

// scenesNode is one decoded element of a command payload, with its children
// when it is a container.
type scenesNode struct {
	el       tlv.Element
	children []scenesNode
}

// readScenesTree reads the members of the container whose opener was just
// consumed.
func readScenesTree(dec *tlv.Decoder) ([]scenesNode, error) {
	var out []scenesNode
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if el.IsEndContainer {
			return out, nil
		}
		n := scenesNode{el: el}
		if el.IsContainer {
			if n.children, err = readScenesTree(dec); err != nil {
				return nil, err
			}
		}
		out = append(out, n)
	}
}

// field returns the member with context tag t.
func field(nodes []scenesNode, t uint64) (scenesNode, bool) {
	for _, n := range nodes {
		if n.el.Tag.Kind == tlv.TagKindContext && uint64(n.el.Tag.Number) == t {
			return n, true
		}
	}
	return scenesNode{}, false
}

func scenesMissing(cmd, name string) error {
	return fieldInvalidCommandError{msg: fmt.Sprintf("%s: mandatory field %s missing", cmd, name), consumed: true}
}

func sceneUint(cmd, name string, nodes []scenesNode, t uint64, limit uint64) (uint64, error) {
	n, ok := field(nodes, t)
	if !ok {
		return 0, scenesMissing(cmd, name)
	}
	if n.el.IsContainer || n.el.Type > tlv.TypeUnsignedInt8 {
		return 0, fieldInvalidCommandError{msg: fmt.Sprintf("%s: %s is not a number", cmd, name), consumed: true}
	}
	if n.el.Uint > limit {
		return 0, fieldConstraintError{fmt.Sprintf("%s: %s %d exceeds %d", cmd, name, n.el.Uint, limit)}
	}
	return n.el.Uint, nil
}

// scenesFieldsReader decodes the ScenesManagement requests; ok is false for
// a command it does not know.
func scenesFieldsReader(path im.ConcreteCommandPath, dec *tlv.Decoder) (fields any, ok bool, err error) {
	if path.Cluster != mattercore.ScenesManagementClusterID {
		return nil, false, nil
	}
	nodes, err := readScenesTree(dec)
	if err != nil {
		return nil, true, err
	}
	cmdName := fmt.Sprintf("ScenesManagement command 0x%02X", path.Command)
	ref := func() (mattercore.SceneRef, error) {
		g, err := sceneUint(cmdName, "GroupId", nodes, 0, 0xFFFF)
		if err != nil {
			return mattercore.SceneRef{}, err
		}
		s, err := sceneUint(cmdName, "SceneId", nodes, 1, 0xFF)
		return mattercore.SceneRef{GroupID: uint16(g), SceneID: uint8(s)}, err //nolint:gosec // bounded above
	}
	switch path.Command {
	case 0x00: // AddScene
		r, err := ref()
		if err != nil {
			return nil, true, err
		}
		tt, err := sceneUint(cmdName, "TransitionTime", nodes, 2, 0xFFFFFFFF)
		if err != nil {
			return nil, true, err
		}
		nameNode, okName := field(nodes, 3)
		if !okName {
			return nil, true, scenesMissing(cmdName, "SceneName")
		}
		setsNode, okSets := field(nodes, 4)
		if !okSets {
			return nil, true, scenesMissing(cmdName, "ExtensionFieldSetStructs")
		}
		sets, err := decodeExtensionFieldSets(cmdName, setsNode)
		return mattercore.AddSceneRequest{
			GroupID: r.GroupID, SceneID: r.SceneID, TransitionTime: uint32(tt), //nolint:gosec // bounded above
			SceneName: nameNode.el.String, ExtensionFieldSets: sets,
		}, true, err
	case 0x01, 0x02, 0x04: // ViewScene, RemoveScene, StoreScene
		r, err := ref()
		return r, true, err
	case 0x03, 0x06: // RemoveAllScenes, GetSceneMembership
		g, err := sceneUint(cmdName, "GroupId", nodes, 0, 0xFFFF)
		return mattercore.SceneGroupRequest{GroupID: uint16(g)}, true, err //nolint:gosec // bounded above
	case 0x05: // RecallScene
		r, err := ref()
		if err != nil {
			return nil, true, err
		}
		req := mattercore.RecallSceneRequest{GroupID: r.GroupID, SceneID: r.SceneID}
		if n, has := field(nodes, 2); has && !n.el.IsNull {
			v := uint32(min(n.el.Uint, 0xFFFFFFFF)) //nolint:gosec // clamped
			req.TransitionTime = &v
		}
		return req, true, nil
	case 0x40: // CopyScene
		mode, err := sceneUint(cmdName, "Mode", nodes, 0, 0xFF)
		if err != nil {
			return nil, true, err
		}
		gf, err := sceneUint(cmdName, "GroupIdentifierFrom", nodes, 1, 0xFFFF)
		if err != nil {
			return nil, true, err
		}
		sf, err := sceneUint(cmdName, "SceneIdentifierFrom", nodes, 2, 0xFF)
		if err != nil {
			return nil, true, err
		}
		gt, err := sceneUint(cmdName, "GroupIdentifierTo", nodes, 3, 0xFFFF)
		if err != nil {
			return nil, true, err
		}
		st, err := sceneUint(cmdName, "SceneIdentifierTo", nodes, 4, 0xFF)
		return mattercore.CopySceneRequest{
			CopyAllScenes: mode&1 != 0, GroupFrom: uint16(gf), SceneFrom: uint8(sf), //nolint:gosec // bounded above
			GroupTo: uint16(gt), SceneTo: uint8(st), //nolint:gosec // bounded above
		}, true, err
	}
	return nil, false, nil
}

// decodeExtensionFieldSets reads list<ExtensionFieldSetStruct>.
func decodeExtensionFieldSets(cmd string, n scenesNode) ([]mattercore.ExtensionFieldSet, error) {
	var out []mattercore.ExtensionFieldSet
	for _, s := range n.children {
		cid, ok := field(s.children, 0)
		if !ok {
			return nil, scenesMissing(cmd, "ExtensionFieldSet.ClusterId")
		}
		list, ok := field(s.children, 1)
		if !ok {
			return nil, scenesMissing(cmd, "ExtensionFieldSet.AttributeValueList")
		}
		set := mattercore.ExtensionFieldSet{ClusterID: uint32(cid.el.Uint)} //nolint:gosec // cluster ids are 32-bit
		for _, p := range list.children {
			pair := mattercore.AttributeValuePair{}
			if a, has := field(p.children, 0); has {
				pair.AttributeID = uint32(a.el.Uint) //nolint:gosec // attribute ids are 32-bit
			} else {
				return nil, scenesMissing(cmd, "AttributeValuePair.AttributeId")
			}
			for _, v := range p.children {
				if v.el.Tag.Kind != tlv.TagKindContext || v.el.Tag.Number < 1 || v.el.Tag.Number > 8 {
					continue
				}
				pair.Fields++
				pair.Tag = uint8(v.el.Tag.Number) //nolint:gosec // 1..8
				pair.Unsigned, pair.Signed = v.el.Uint, v.el.Int
				if v.el.Tag.Number%2 == 0 { // signed variants
					pair.Unsigned = uint64(v.el.Int) //nolint:gosec // the raw bits
				}
			}
			set.AttributeValueList = append(set.AttributeValueList, pair)
		}
		out = append(out, set)
	}
	return out, nil
}

// encodeScenesResponse writes the ScenesManagement responses
// (scenes-management.element.ts field order). Returns false for a value
// that is no ScenesManagement response.
func encodeScenesResponse(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case mattercore.SceneStatusResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		enc.PutUint(tlv.ContextTag(1), uint64(x.GroupID))
		enc.PutUint(tlv.ContextTag(2), uint64(x.SceneID))
		_ = enc.EndContainer()
	case mattercore.ViewSceneResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		enc.PutUint(tlv.ContextTag(1), uint64(x.GroupID))
		enc.PutUint(tlv.ContextTag(2), uint64(x.SceneID))
		if x.Status == im.StatusSuccess {
			enc.PutUint(tlv.ContextTag(3), uint64(x.TransitionTime))
			enc.PutUTF8(tlv.ContextTag(4), x.SceneName)
			enc.StartArray(tlv.ContextTag(5))
			encodeExtensionFieldSetsTLV(enc, x.ExtensionFieldSets)
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case mattercore.RemoveAllScenesResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		enc.PutUint(tlv.ContextTag(1), uint64(x.GroupID))
		_ = enc.EndContainer()
	case mattercore.GetSceneMembershipResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		if x.Capacity == nil {
			enc.PutNull(tlv.ContextTag(1))
		} else {
			enc.PutUint(tlv.ContextTag(1), uint64(*x.Capacity))
		}
		enc.PutUint(tlv.ContextTag(2), uint64(x.GroupID))
		if x.HasList {
			enc.StartArray(tlv.ContextTag(3))
			for _, id := range x.SceneList {
				enc.PutUint(tlv.AnonymousTag(), uint64(id))
			}
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	case mattercore.CopySceneResponse:
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(x.Status))
		enc.PutUint(tlv.ContextTag(1), uint64(x.GroupFrom))
		enc.PutUint(tlv.ContextTag(2), uint64(x.SceneFrom))
		_ = enc.EndContainer()
	case []mattercore.SceneInfoStruct:
		enc.StartArray(tag)
		for _, s := range x {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(0), uint64(s.SceneCount))
			enc.PutUint(tlv.ContextTag(1), uint64(s.CurrentScene))
			enc.PutUint(tlv.ContextTag(2), uint64(s.CurrentGroup))
			enc.PutBool(tlv.ContextTag(3), s.SceneValid)
			enc.PutUint(tlv.ContextTag(4), uint64(s.RemainingCapacity))
			enc.PutUint(tlv.ContextTag(0xFE), uint64(s.FabricIndex))
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
	default:
		return false
	}
	return true
}

func encodeExtensionFieldSetsTLV(enc *tlv.Encoder, sets []mattercore.ExtensionFieldSet) {
	for _, s := range sets {
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(0), uint64(s.ClusterID))
		enc.StartArray(tlv.ContextTag(1))
		for _, p := range s.AttributeValueList {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutUint(tlv.ContextTag(0), uint64(p.AttributeID))
			enc.PutUint(tlv.ContextTag(uint8(p.Tag)), p.Unsigned)
			_ = enc.EndContainer()
		}
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
}
