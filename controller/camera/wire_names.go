// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// The wire names of the camera cluster enums: every name the capabilities
// report must be accepted back by a start-stream or snapshot request, so a
// host that speaks matterjs-server's JSON names a codec, a usage or a talk
// support as these do. Mirrors matterjs-server
// packages/ws-controller/src/camera/wireNames.ts at 0bd06f2: the codec
// tables written out (:16-31), because the enum member names (Hevc, AacLc,
// …) differ from the SDP spelling (H265, AAC, …); wireName (:44-46), the
// decimal spelling for a value without a name; wireValue (:48-54), a
// case-insensitive match (wireValueByName here); the exported name and
// byName functions (:55-73).

// videoCodecNames are the video codecs' wire names. Mirrors
// matterjs-server packages/ws-controller/src/camera/wireNames.ts:16-21 at
// 0bd06f2.
var videoCodecNames = map[csdef.VideoCodecEnum]string{
	csdef.VideoCodecH264: "H264",
	csdef.VideoCodecHevc: "H265",
	csdef.VideoCodecVvc:  "H266",
	csdef.VideoCodecAv1:  "AV1",
}

// audioCodecNames are the audio codecs' wire names. Mirrors
// matterjs-server packages/ws-controller/src/camera/wireNames.ts:23-26 at
// 0bd06f2.
var audioCodecNames = map[csdef.AudioCodecEnum]string{
	csdef.AudioCodecOpus:  "OPUS",
	csdef.AudioCodecAacLc: "AAC",
}

// imageCodecNames are the image codecs' wire names. Mirrors
// matterjs-server packages/ws-controller/src/camera/wireNames.ts:28-31 at
// 0bd06f2.
var imageCodecNames = map[csdef.ImageCodecEnum]string{
	csdef.ImageCodecJpeg: "JPEG",
	csdef.ImageCodecHeic: "HEIC",
}

// streamUsageNames and twoWayTalkSupportNames are the enums' member names,
// read off the generated definitions as namesOfEnum reads matter.js's enum
// in matterjs-server packages/ws-controller/src/camera/wireNames.ts:33-42
// at 0bd06f2.
var (
	streamUsageNames       = namesOfEnum[csdef.StreamUsageEnum](csdef.StreamUsageEnumDef)
	twoWayTalkSupportNames = namesOfEnum[csdef.TwoWayTalkSupportTypeEnum](csdef.TwoWayTalkSupportTypeEnumDef)
)

func namesOfEnum[E ~uint8](def *spec.Enum) map[E]string {
	names := make(map[E]string, len(def.Values))
	for i := range def.Values {
		names[E(def.Values[i].Value)] = def.Values[i].Name
	}
	return names
}

// wireName is v's name, its decimal spelling for a value without one.
func wireName[E ~uint8](names map[E]string, v E) string {
	if name, ok := names[v]; ok {
		return name
	}
	return strconv.Itoa(int(v))
}

// wireValueByName is the value whose name matches name ignoring case.
func wireValueByName[E ~uint8](names map[E]string, name string) (E, bool) {
	for v, known := range names {
		if strings.EqualFold(known, name) {
			return v, true
		}
	}
	return 0, false
}

// byNameOrDecimal is wireValueByName, then the decimal spelling wireName
// reports for a value without a name, as imageCodecByName does in
// matterjs-server packages/ws-controller/src/camera/wireNames.ts:68-73 at
// 0bd06f2.
func byNameOrDecimal[E ~uint8](names map[E]string, name string) (E, bool) {
	if v, ok := wireValueByName(names, name); ok {
		return v, true
	}
	if name == "" || strings.TrimLeft(name, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseUint(name, 10, 8)
	if err != nil {
		return 0, false
	}
	return E(n), true
}

// VideoCodecName is a video codec's wire name (H264, H265, H266, AV1), its
// decimal spelling for a codec without a name.
func VideoCodecName(codec csdef.VideoCodecEnum) string { return wireName(videoCodecNames, codec) }

// VideoCodecByName is the video codec name names, ignoring case, or the
// codec its decimal spelling states — the spelling [VideoCodecName]
// reports for a codec this build does not name, so every name parses
// back. ok=true means the name parsed (a member name or a decimal), not
// that the device or this module knows the codec. matterjs-server has no
// video ByName (only imageCodecByName, wireNames.ts:68-73 at 0bd06f2);
// this one extends its table by the same rule.
func VideoCodecByName(name string) (csdef.VideoCodecEnum, bool) {
	return byNameOrDecimal(videoCodecNames, name)
}

// AudioCodecName is an audio codec's wire name (OPUS, AAC), its decimal
// spelling for a codec without a name.
func AudioCodecName(codec csdef.AudioCodecEnum) string { return wireName(audioCodecNames, codec) }

// AudioCodecByName is the audio codec name names, ignoring case, or the
// codec its decimal spelling states — the spelling [AudioCodecName]
// reports for a codec this build does not name, so every name parses
// back. ok=true means the name parsed (a member name or a decimal), not
// that the device or this module knows the codec. matterjs-server has no
// audio ByName (only imageCodecByName, wireNames.ts:68-73 at 0bd06f2);
// this one extends its table by the same rule.
func AudioCodecByName(name string) (csdef.AudioCodecEnum, bool) {
	return byNameOrDecimal(audioCodecNames, name)
}

// ImageCodecName is an image codec's wire name (JPEG, HEIC), its decimal
// spelling for a codec without a name.
func ImageCodecName(codec csdef.ImageCodecEnum) string { return wireName(imageCodecNames, codec) }

// ImageCodecByName is the image codec name names, ignoring case, or the
// codec its decimal spelling states — the spelling [ImageCodecName]
// reports for a codec this build does not name, as matterjs-server's
// imageCodecByName accepts it (wireNames.ts:68-73 at 0bd06f2). ok=true
// means the name parsed (a member name or a decimal), not that the device
// or this module knows the codec.
func ImageCodecByName(name string) (csdef.ImageCodecEnum, bool) {
	return byNameOrDecimal(imageCodecNames, name)
}

// StreamUsageName is a stream usage's member name (Internal, Recording,
// Analysis, LiveView), its decimal spelling for a usage without a name.
func StreamUsageName(usage csdef.StreamUsageEnum) string { return wireName(streamUsageNames, usage) }

// StreamUsageByName is the stream usage name names, ignoring case. Only
// member names are accepted, as matterjs-server's streamUsageByName does,
// so a usage the controller cannot check never reaches the device.
func StreamUsageByName(name string) (csdef.StreamUsageEnum, bool) {
	return wireValueByName(streamUsageNames, name)
}

// TwoWayTalkSupportName is a two-way talk support's member name
// (NotSupported, HalfDuplex, FullDuplex), its decimal spelling for one
// without a name.
func TwoWayTalkSupportName(support csdef.TwoWayTalkSupportTypeEnum) string {
	return wireName(twoWayTalkSupportNames, support)
}
