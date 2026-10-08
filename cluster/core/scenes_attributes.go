// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// The scene-able attributes ("S" quality) of the clusters this module
// serves, with the AttributeValuePair field each one travels in — matter.js
// DataTypeToSceneAttributeDataMap (bool, enum8 and uint8 in ValueUnsigned8,
// uint16 in ValueUnsigned16) — and the bounds matter.js validates a decoded
// value against (#decodeValueFromAttributeValuePair). A bound given as other
// attributes ("minLevel to maxLevel") is the type's.
type sceneAttr struct {
	id       uint32
	tag      uint8 // AttributeValuePair field: 1 ValueUnsigned8, 3 ValueUnsigned16
	boolean  bool
	nullable bool
	max      uint64
}

var sceneableAttributes = map[uint32][]sceneAttr{
	0x0006: { // OnOff
		{id: 0x0000, tag: 1, boolean: true},
	},
	0x0008: { // LevelControl
		{id: 0x0000, tag: 1, nullable: true, max: 254}, // CurrentLevel, nullable uint8
		{id: 0x0004, tag: 3, max: 0xFFFF},              // CurrentFrequency
	},
	0x0300: { // ColorControl
		{id: 0x0001, tag: 1, max: 254},    // CurrentSaturation
		{id: 0x0003, tag: 3, max: 65279},  // CurrentX
		{id: 0x0004, tag: 3, max: 65279},  // CurrentY
		{id: 0x0007, tag: 3, max: 65279},  // ColorTemperatureMireds
		{id: 0x4000, tag: 3, max: 0xFFFF}, // EnhancedCurrentHue
		{id: 0x4001, tag: 1, max: 0xFF},   // EnhancedColorMode
		{id: 0x4002, tag: 1, max: 1},      // ColorLoopActive
		{id: 0x4003, tag: 1, max: 0xFF},   // ColorLoopDirection
		{id: 0x4004, tag: 3, max: 0xFFFF}, // ColorLoopTime
	},
}

// endpointSceneAttributes returns the scene-able attributes the endpoint's
// sibling servers actually carry, per cluster (matter.js implementScenes
// registers only attributes present on the endpoint).
func (s *ScenesManagement) endpointSceneAttributes() map[uint32][]sceneAttr {
	out := map[uint32][]sceneAttr{}
	if s.cfg.Siblings == nil {
		return out
	}
	for _, srv := range s.cfg.Siblings() {
		attrs, ok := sceneableAttributes[srv.MatterClusterID()]
		if !ok {
			continue
		}
		listed := listedAttributes(srv)
		for _, a := range attrs {
			if listed[a.id] {
				out[srv.MatterClusterID()] = append(out[srv.MatterClusterID()], a)
			}
		}
	}
	return out
}

func listedAttributes(srv contract.ClusterServer) map[uint32]bool {
	out := map[uint32]bool{}
	var ids []uint32
	if l, ok := srv.(contract.ClusterAttributeLister); ok {
		ids = l.MatterAttributes()
	} else {
		ids = srv.MatterReportable()
	}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// decodeExtensionFieldSets is matter.js #decodeExtensionFieldSets: sets for
// clusters the endpoint does not have are skipped, a repeated cluster
// keeps its last set, an attribute that is not scene-able there or a pair
// that does not carry exactly one value of the attribute's type rejects the
// whole command (false → INVALID_COMMAND).
func (s *ScenesManagement) decodeExtensionFieldSets(sets []ExtensionFieldSet) ([]SceneClusterValues, bool) {
	known := s.endpointSceneAttributes()
	var out []SceneClusterValues
	for _, set := range sets {
		attrs, ok := known[set.ClusterID]
		if !ok {
			continue
		}
		out = slicesDeleteCluster(out, set.ClusterID)
		values := SceneClusterValues{ClusterID: set.ClusterID}
		for _, pair := range set.AttributeValueList {
			var def *sceneAttr
			for i := range attrs {
				if attrs[i].id == pair.AttributeID {
					def = &attrs[i]
				}
			}
			if def == nil || pair.Fields != 1 || pair.Tag != def.tag {
				return nil, false
			}
			values.Values = setSceneValue(values.Values, decodeSceneValue(*def, pair.Unsigned))
		}
		if len(values.Values) > 0 {
			out = append(out, values)
		}
	}
	return out, true
}

func slicesDeleteCluster(in []SceneClusterValues, clusterID uint32) []SceneClusterValues {
	out := in[:0]
	for _, v := range in {
		if v.ClusterID != clusterID {
			out = append(out, v)
		}
	}
	return out
}

func setSceneValue(in []SceneAttributeValue, v SceneAttributeValue) []SceneAttributeValue {
	for i := range in {
		if in[i].AttributeID == v.AttributeID {
			in[i] = v
			return in
		}
	}
	return append(in, v)
}

// decodeSceneValue applies matter.js's out-of-range rules: a boolean other
// than 0/1 is null when nullable and FALSE otherwise; a number above the
// bound is null when nullable and the closest valid value (the bound)
// otherwise.
func decodeSceneValue(def sceneAttr, raw uint64) SceneAttributeValue {
	v := SceneAttributeValue{AttributeID: def.id}
	switch {
	case def.boolean:
		switch {
		case raw <= 1:
			v.Unsigned = raw
		case def.nullable:
			v.Null = true
		default:
			v.Unsigned = 0
		}
	case raw > def.max:
		if def.nullable {
			v.Null = true
		} else {
			v.Unsigned = def.max
		}
	default:
		v.Unsigned = raw
	}
	return v
}

// encodeExtensionFieldSets is matter.js #encodeExtensionFieldSets: a null
// boolean travels as 0xFF, a null unsigned number as its type's maximum.
func (s *ScenesManagement) encodeExtensionFieldSets(values []SceneClusterValues) []ExtensionFieldSet {
	out := []ExtensionFieldSet{}
	for _, cv := range values {
		set := ExtensionFieldSet{ClusterID: cv.ClusterID}
		for _, v := range cv.Values {
			def, ok := sceneAttrDef(cv.ClusterID, v.AttributeID)
			if !ok {
				continue
			}
			pair := AttributeValuePair{AttributeID: v.AttributeID, Tag: def.tag, Unsigned: v.Unsigned, Fields: 1}
			if v.Null {
				pair.Unsigned = 0xFF
				if def.tag == 3 {
					pair.Unsigned = 0xFFFF
				}
			}
			set.AttributeValueList = append(set.AttributeValueList, pair)
		}
		if len(set.AttributeValueList) > 0 {
			out = append(out, set)
		}
	}
	return out
}

func sceneAttrDef(clusterID, attrID uint32) (sceneAttr, bool) {
	for _, a := range sceneableAttributes[clusterID] {
		if a.id == attrID {
			return a, true
		}
	}
	return sceneAttr{}, false
}

// captureValues is matter.js #collectSceneAttributeValues: the current
// value of every scene-able attribute of the endpoint.
func (s *ScenesManagement) captureValues() []SceneClusterValues {
	if s.cfg.Siblings == nil {
		return nil
	}
	known := s.endpointSceneAttributes()
	var out []SceneClusterValues
	for _, srv := range s.cfg.Siblings() {
		attrs := known[srv.MatterClusterID()]
		if len(attrs) == 0 {
			continue
		}
		cv := SceneClusterValues{ClusterID: srv.MatterClusterID()}
		for _, a := range attrs {
			raw, ok := srv.MatterRead(a.id)
			if !ok {
				continue
			}
			v := SceneAttributeValue{AttributeID: a.id}
			switch x := raw.(type) {
			case nil:
				v.Null = true
			case bool:
				if x {
					v.Unsigned = 1
				}
			default:
				n, isNum := asUint64(raw)
				if !isNum {
					continue
				}
				v.Unsigned = n
			}
			cv.Values = append(cv.Values, v)
		}
		if len(cv.Values) > 0 {
			out = append(out, cv)
		}
	}
	return out
}

func asUint64(v any) (uint64, bool) {
	switch x := v.(type) {
	case uint8:
		return uint64(x), true
	case uint16:
		return uint64(x), true
	case uint32:
		return uint64(x), true
	case uint64:
		return x, true
	case int:
		return uint64(max(x, 0)), true //nolint:gosec // clamped
	case *uint8:
		if x == nil {
			return 0, false
		}
		return uint64(*x), true
	case *uint16:
		if x == nil {
			return 0, false
		}
		return uint64(*x), true
	}
	return 0, false
}

// SceneValuesApplier is the optional capability of a scene-able cluster
// server that recalls a scene's values itself: matter.js's
// ScenesManagementServer.implementScenes(behavior, applyFunc), where each
// cluster registers its own #applySceneValues. values holds the scene's
// non-null values by attribute id (matter.js passes them as a struct and
// each apply function tests `typeof value === "number"`); transitionMs is
// the scene's transition in milliseconds. light.ColorControlServer
// implements it.
type SceneValuesApplier interface {
	MatterApplySceneValues(ctx context.Context, values map[uint32]uint64, transitionMs uint32)
}

// presentValues returns a scene's non-null values by attribute id.
func presentValues(cv SceneClusterValues) map[uint32]uint64 {
	out := make(map[uint32]uint64, len(cv.Values))
	for _, v := range cv.Values {
		if !v.Null {
			out[v.AttributeID] = v.Unsigned
		}
	}
	return out
}

// applyValues recalls a scene through the clusters' own commands, as
// matter.js's per-cluster apply functions do (OnOffServer, LevelControlServer
// and ColorControlServer #applySceneValues): OnOff On/Off, LevelControl
// MoveToLevel with ExecuteIfOff, ColorControl MoveToColorTemperature or
// MoveToHueAndSaturation for the stored color mode — unless the server
// recalls its values itself ([SceneValuesApplier]). transitionMs is the
// scene's transition in milliseconds; the commands take tenths of a second.
func (s *ScenesManagement) applyValues(ctx context.Context, values []SceneClusterValues, transitionMs uint32) {
	if s.cfg.Siblings == nil {
		return
	}
	servers := map[uint32]contract.ClusterServer{}
	for _, srv := range s.cfg.Siblings() {
		servers[srv.MatterClusterID()] = srv
	}
	tenths := uint16(min(transitionMs/100, 0xFFFE)) //nolint:gosec // clamped
	get := func(cv SceneClusterValues, id uint32) (SceneAttributeValue, bool) {
		for _, v := range cv.Values {
			if v.AttributeID == id {
				return v, true
			}
		}
		return SceneAttributeValue{}, false
	}
	for _, cv := range values {
		srv := servers[cv.ClusterID]
		if srv == nil {
			continue
		}
		if applier, ok := srv.(SceneValuesApplier); ok {
			applier.MatterApplySceneValues(ctx, presentValues(cv), transitionMs)
			continue
		}
		switch cv.ClusterID {
		case 0x0006:
			if v, ok := get(cv, 0x0000); ok && !v.Null {
				current, _ := srv.MatterRead(0x0000)
				if b, isBool := current.(bool); isBool && b == (v.Unsigned == 1) {
					continue
				}
				cmd := uint32(0x00) // Off
				if v.Unsigned == 1 {
					cmd = 0x01 // On
				}
				_, _ = srv.MatterInvoke(ctx, cmd, nil)
			}
		case 0x0008:
			if v, ok := get(cv, 0x0000); ok && !v.Null {
				tt := tenths
				_, _ = srv.MatterInvoke(ctx, 0x00, wire.MoveToLevelRequest{
					Level: uint8(v.Unsigned), TransitionTime: &tt, OptionsMask: 1, OptionsOverride: 1, //nolint:gosec // bounded by 254
				})
			}
		case 0x0300:
			mode, hasMode := get(cv, 0x4001)
			if ct, ok := get(cv, 0x0007); ok && !ct.Null && (!hasMode || mode.Unsigned == 2) {
				_, _ = srv.MatterInvoke(ctx, wire.ColorCtrlCmdMoveToColorTemperature, wire.MoveToColorTemperatureRequest{
					ColorTemperatureMireds: uint16(ct.Unsigned), TransitionTime: tenths, OptionsMask: 1, OptionsOverride: 1, //nolint:gosec // bounded by 65279
				})
				continue
			}
			hue, okH := get(cv, 0x4000)
			sat, okS := get(cv, 0x0001)
			if okH && okS && !hue.Null && !sat.Null && (!hasMode || mode.Unsigned == 0 || mode.Unsigned == 3) {
				_, _ = srv.MatterInvoke(ctx, wire.ColorCtrlCmdMoveToHueAndSaturation, wire.MoveToHueAndSaturationRequest{
					Hue: uint8(hue.Unsigned >> 8), Saturation: uint8(sat.Unsigned), TransitionTime: tenths, //nolint:gosec // bounded
					OptionsMask: 1, OptionsOverride: 1,
				})
			}
		}
	}
}
