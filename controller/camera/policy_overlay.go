// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// overlayBounds are the overlay flags of a request or a stream. Mirrors
// matterjs-server packages/ws-controller/src/camera/overlayPolicy.ts:9-13 at
// 0bd06f2.
type overlayBounds struct {
	watermark overlayFlag
	osd       overlayFlag
}

// overlaySupport says whether the camera advertises each overlay feature:
// flagOn makes the allocate field mandatory, flagOff forbids it, and
// flagUnstated means the feature map has not arrived and nothing is gated on
// it. Mirrors matterjs-server
// packages/ws-controller/src/camera/overlayPolicy.ts:15-22 at 0bd06f2.
type overlaySupport struct {
	watermark overlayFlag
	osd       overlayFlag
}

// overlaySelection is resolveOverlays's answer: the overlays to send, or
// the feature a caller demanded that the camera lacks (a capability
// refusal, not a bound the ladder can relax). Mirrors matterjs-server
// packages/ws-controller/src/camera/overlayPolicy.ts:24-27 at 0bd06f2.
type overlaySelection struct {
	overlays overlayBounds
	// unsupported is the feature the camera does not advertise; 0 when the
	// selection holds.
	unsupported csdef.Feature
}

// featureMapStated reports whether the camera's FeatureMap has arrived:
// matter.js shows an empty map until the device reports it, and one of
// Audio, Video and Snapshot is mandatory (§11.2.5, O.a+). Mirrors
// matterjs-server packages/ws-controller/src/camera/devicePolicy.ts:13-19 at
// 0bd06f2.
func featureMapStated(features csdef.Feature) bool {
	return features&(csdef.FeatureAudio|csdef.FeatureVideo|csdef.FeatureSnapshot) != 0
}

// supportOf derives the overlay support from the camera's FeatureMap.
// Mirrors matterjs-server packages/ws-controller/src/camera/devicePolicy.ts:30-33
// at 0bd06f2.
func supportOf(features csdef.Feature) overlaySupport {
	if !featureMapStated(features) {
		return overlaySupport{}
	}
	return overlaySupport{
		watermark: flagOf(features&csdef.FeatureWatermark != 0),
		osd:       flagOf(features&csdef.FeatureOnScreenDisplay != 0),
	}
}

// overlayField is the flag sent for one overlay. Mirrors matterjs-server
// packages/ws-controller/src/camera/overlayPolicy.ts:29-34 at 0bd06f2.
func overlayField(stated, supported overlayFlag) overlayFlag {
	switch supported {
	case flagOn:
		if stated == flagUnstated {
			return flagOff
		}
		return stated
	case flagOff:
		// A stated on never reaches here: resolveOverlays refuses it.
		return flagUnstated
	default:
		return stated
	}
}

// resolveOverlays decides the overlay fields of an allocation:
// WatermarkEnabled and OSDEnabled must be present exactly when the feature
// is advertised (conformance WMARK / OSD, §11.2.8.4, §11.2.8.8), and the
// reference server answers INVALID_COMMAND otherwise. An unstated flag on a
// supporting camera is sent as off, the §11.2.6.11 fallback. Mirrors
// matterjs-server packages/ws-controller/src/camera/overlayPolicy.ts:36-52
// at 0bd06f2.
func resolveOverlays(stated overlayBounds, support overlaySupport) overlaySelection {
	if stated.watermark == flagOn && support.watermark == flagOff {
		return overlaySelection{unsupported: csdef.FeatureWatermark}
	}
	if stated.osd == flagOn && support.osd == flagOff {
		return overlaySelection{unsupported: csdef.FeatureOnScreenDisplay}
	}
	return overlaySelection{overlays: overlayBounds{
		watermark: overlayField(stated.watermark, support.watermark),
		osd:       overlayField(stated.osd, support.osd),
	}}
}

// overlaysMatch reports whether a stream carrying carried serves a request
// wanting wanted. An absent flag on the stream reads as off: the struct
// reports it only when the feature is advertised (§11.2.6.11, §11.2.6.13).
// Mirrors matterjs-server packages/ws-controller/src/camera/overlayPolicy.ts:54-64
// at 0bd06f2.
func overlaysMatch(carried, wanted overlayBounds) bool {
	on := func(f overlayFlag) bool { return f == flagOn }
	if wanted.watermark != flagUnstated && on(carried.watermark) != on(wanted.watermark) {
		return false
	}
	if wanted.osd != flagUnstated && on(carried.osd) != on(wanted.osd) {
		return false
	}
	return true
}
