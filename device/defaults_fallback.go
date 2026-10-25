// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"github.com/SukramJ/go-fabric/cluster/spec"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	smokedef "github.com/SukramJ/go-fabric/cluster/spec/smokecoalarm"
	"github.com/SukramJ/go-fabric/contract"
)

// The values of a generated default server that neither the device type,
// the host's Initial nor the definition gives — the attributes a wildcard
// read would otherwise answer UnsupportedAttribute for though the server
// lists them.
//
// matter.js starts the state of a behaviour at SelectDefaultValue for every
// attribute it supports (packages/node/src/behavior/cluster/ClusterBehaviorType.ts:314-317,
// packages/model/src/logic/SelectDefaultValue.ts:20-27 and
// defaultValueForMetatype :137-169 at 862ad75f): the stated default, else
// null for a nullable attribute, 0 for a number, false for a boolean, an
// empty list, and an empty bitmap; an enum, a string and bytes have none.
// Then the default server's initialize fills what the state still leaves
// unset ([initializeDefaults]).

// initializeDefaults are the values matter.js's default servers set in
// initialize when the state leaves them unset, by cluster, for the
// generated server the kit falls back to.
var initializeDefaults = map[uint32]func(features uint32, initial map[uint32]any){
	// FanControlServer.initialize (packages/node/src/behaviors/fan-control/FanControlServer.ts:14-18
	// at 862ad75f): FanMode Off.
	fandef.ClusterID: func(_ uint32, initial map[uint32]any) {
		setUnset(initial, fandef.AttrFanMode, fandef.FanModeOff)
	},
	// SmokeCoAlarmBaseServer.initialize (packages/node/src/behaviors/smoke-co-alarm/SmokeCoAlarmServer.ts:19-41
	// at 862ad75f): ExpressedState Normal, SmokeState and COState Normal
	// with their features, BatteryAlert Normal, TestInProgress and
	// HardwareFaultAlert false, EndOfServiceAlert Normal.
	smokedef.ClusterID: func(features uint32, initial map[uint32]any) {
		setUnset(initial, smokedef.AttrExpressedState, smokedef.ExpressedStateNormal)
		if features&uint32(smokedef.FeatureSmokeAlarm) != 0 {
			setUnset(initial, smokedef.AttrSmokeState, smokedef.AlarmStateNormal)
		}
		if features&uint32(smokedef.FeatureCoAlarm) != 0 {
			setUnset(initial, smokedef.AttrCoState, smokedef.AlarmStateNormal)
		}
		setUnset(initial, smokedef.AttrBatteryAlert, smokedef.AlarmStateNormal)
		setUnset(initial, smokedef.AttrTestInProgress, false)
		setUnset(initial, smokedef.AttrHardwareFaultAlert, false)
		setUnset(initial, smokedef.AttrEndOfServiceAlert, smokedef.EndOfServiceNormal)
	},
	// OccupancySensingServer.initialize (packages/node/src/behaviors/occupancy-sensing/OccupancySensingServer.ts:22-66
	// at 862ad75f): with a detector feature and the sensor type or its
	// bitmap unset, the bitmap from the features (PIR, Ultrasonic,
	// PhysicalContact) and the type from the bitmap — PirAndUltrasonic,
	// Pir, else PhysicalContact; Ultrasonic alone leaves the type unset,
	// as there.
	occdef.ClusterID: func(features uint32, initial map[uint32]any) {
		_, hasType := initial[occdef.AttrOccupancySensorType]
		bmp, hasBmp := initial[occdef.AttrOccupancySensorTypeBitmap]
		if features == 0 || (hasType && hasBmp && bmp != occdef.OccupancySensorTypeBitmap(0)) {
			return
		}
		var b occdef.OccupancySensorTypeBitmap
		if features&uint32(occdef.FeaturePassiveInfrared) != 0 {
			b |= occdef.OccupancySensorTypeBitmapPir
		}
		if features&uint32(occdef.FeatureUltrasonic) != 0 {
			b |= occdef.OccupancySensorTypeBitmapUltrasonic
		}
		if features&uint32(occdef.FeaturePhysicalContact) != 0 {
			b |= occdef.OccupancySensorTypeBitmapPhysicalContact
		}
		if b == 0 {
			return
		}
		initial[occdef.AttrOccupancySensorTypeBitmap] = b
		switch {
		case b&occdef.OccupancySensorTypeBitmapPir != 0 && b&occdef.OccupancySensorTypeBitmapUltrasonic != 0:
			initial[occdef.AttrOccupancySensorType] = occdef.OccupancySensorTypeEnumPirAndUltrasonic
		case b&occdef.OccupancySensorTypeBitmapPir != 0:
			initial[occdef.AttrOccupancySensorType] = occdef.OccupancySensorTypeEnumPir
		case b&occdef.OccupancySensorTypeBitmapPhysicalContact != 0:
			initial[occdef.AttrOccupancySensorType] = occdef.OccupancySensorTypeEnumPhysicalContact
		}
	},
}

func setUnset(initial map[uint32]any, id uint32, v any) {
	if _, ok := initial[id]; !ok {
		initial[id] = v
	}
}

// fillSelectDefaults gives every attribute srv lists and holds no value
// for its SelectDefaultValue fallback — null when nullable, 0 for a number
// or bitmap, false, an empty list — where the server takes it (a fallback
// its constraint refuses stays out, as matter.js's validation would refuse
// it). An enum, a string, bytes and a struct get none.
func fillSelectDefaults(def *spec.Cluster, srv contract.ClusterServer) {
	l, ok := srv.(contract.ClusterAttributeLister)
	setter, settable := srv.(spec.AttributeSetter)
	if !ok || !settable {
		return
	}
	for _, id := range l.MatterAttributes() {
		if _, held := srv.MatterRead(id); held {
			continue
		}
		a := def.Attribute(id)
		if a == nil {
			continue
		}
		v, ok := selectDefault(a)
		if !ok {
			continue
		}
		_ = setter.SetAttributes(map[uint32]any{id: v})
	}
}

// selectDefault is defaultValueForMetatype's fallback for a, in the form a
// generated server stores.
func selectDefault(a *spec.Attribute) (any, bool) {
	if a.Quality.Nullable {
		return nil, true
	}
	switch a.Type.Kind {
	case spec.KindUint, spec.KindBitmap, spec.KindInt, spec.KindFloat32, spec.KindFloat64:
		return typedDefault(a, 0)
	case spec.KindBool:
		return false, true
	case spec.KindList:
		return []any{}, true
	case spec.KindAny, spec.KindString, spec.KindBytes, spec.KindEnum, spec.KindStruct:
	}
	return nil, false
}
