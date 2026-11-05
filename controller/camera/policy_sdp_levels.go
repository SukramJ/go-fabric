// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"regexp"
	"strconv"
)

// pixelsPerMacroblock: a macroblock is 16x16 luma samples, which is how
// H.264 states MaxFS and MaxMBPS. Mirrors matterjs-server
// packages/ws-controller/src/camera/codecLevels.ts:17-18 at 0bd06f2.
const pixelsPerMacroblock = 256

// bitsPerKilobit converts max-br and MaxBR, both in units of 1000 bits per
// second (RFC 6184 §8.1, RFC 7798 §7.1). Mirrors matterjs-server
// packages/ws-controller/src/camera/codecLevels.ts:19 at 0bd06f2.
const bitsPerKilobit = 1000

// h264Level is one row of ITU-T H.264 (V15, 08/2021) Annex A, Table A-1
// "Level limits": MaxMBPS (macroblocks per second), MaxFS (macroblocks) and
// MaxBR (kbit/s). MaxBR is the Baseline/Main/Extended value (cpbBrVclFactor
// 1000): the tighter bound, since High allows 1.25x.
type h264Level struct {
	maxMbps, maxFs, maxBr float64
}

// h264LevelLimits is Table A-1 by level name. Mirrors matterjs-server
// packages/ws-controller/src/camera/codecLevels.ts:21-47 at 0bd06f2.
var h264LevelLimits = map[string]h264Level{
	"1":   {1485, 99, 64},
	"1b":  {1485, 99, 128},
	"1.1": {3000, 396, 192},
	"1.2": {6000, 396, 384},
	"1.3": {11880, 396, 768},
	"2":   {11880, 396, 2000},
	"2.1": {19800, 792, 4000},
	"2.2": {20250, 1620, 4000},
	"3":   {40500, 1620, 10000},
	"3.1": {108000, 3600, 14000},
	"3.2": {216000, 5120, 20000},
	"4":   {245760, 8192, 20000},
	"4.1": {245760, 8192, 50000},
	"4.2": {522240, 8704, 50000},
	"5":   {589824, 22080, 135000},
	"5.1": {983040, 36864, 240000},
	"5.2": {2073600, 36864, 240000},
	"6":   {4177920, 139264, 240000},
	"6.1": {8355840, 139264, 480000},
	"6.2": {16711680, 139264, 800000},
}

// h264LevelIDCNames maps level_idc to its Table A-1 level. 11 is missing
// because it means 1.1 or 1b depending on the profile; 9 is the other
// spelling of 1b (H.264 §A.3.1). Mirrors matterjs-server
// packages/ws-controller/src/camera/codecLevels.ts:49-73 at 0bd06f2.
var h264LevelIDCNames = map[int64]string{
	9: "1b", 10: "1", 12: "1.2", 13: "1.3",
	20: "2", 21: "2.1", 22: "2.2",
	30: "3", 31: "3.1", 32: "3.2",
	40: "4", 41: "4.1", 42: "4.2",
	50: "5", 51: "5.1", 52: "5.2",
	60: "6", 61: "6.1", 62: "6.2",
}

// constraintSet3Flag is constraint_set3_flag, the fourth flag in the
// profile_iop byte (H.264 §7.3.2.1.1). Mirrors matterjs-server
// packages/ws-controller/src/camera/codecLevels.ts:75-76 at 0bd06f2.
const constraintSet3Flag = 0x10

// h264Level1bProfiles: for profile_idc 66 (Baseline), 77 (Main) and 88
// (Extended), level_idc 11 with constraint_set3_flag set is level 1b
// (RFC 6184 §8.1); for any other profile it is 1.1. Mirrors matterjs-server
// packages/ws-controller/src/camera/codecLevels.ts:78-82 at 0bd06f2.
var h264Level1bProfiles = map[int64]bool{66: true, 77: true, 88: true}

var (
	profileLevelIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)
	h265LevelIDPattern    = regexp.MustCompile(`^\d{1,3}$`)
)

// h264ProfileLevelIDLimits reads a profile-level-id (RFC 6184 §8.1) as its
// level's hard bounds; ok false means unreadable, not unlimited. Mirrors
// matterjs-server packages/ws-controller/src/camera/codecLevels.ts:84-103 at
// 0bd06f2.
func h264ProfileLevelIDLimits(profileLevelID string) (videoCodecLimits, bool) {
	if !profileLevelIDPattern.MatchString(profileLevelID) {
		return videoCodecLimits{}, false
	}
	profileIdc, _ := strconv.ParseInt(profileLevelID[0:2], 16, 64)
	profileIop, _ := strconv.ParseInt(profileLevelID[2:4], 16, 64)
	levelIdc, _ := strconv.ParseInt(profileLevelID[4:6], 16, 64)
	var level string
	if levelIdc == 11 {
		level = "1.1"
		if profileIop&constraintSet3Flag != 0 && h264Level1bProfiles[profileIdc] {
			level = "1b"
		}
	} else {
		level = h264LevelIDCNames[levelIdc]
	}
	limits, ok := h264LevelLimits[level]
	if !ok {
		return videoCodecLimits{}, false
	}
	return videoCodecLimits{
		maxPixels:          stated(limits.maxFs * pixelsPerMacroblock),
		maxPixelsPerSecond: stated(limits.maxMbps * pixelsPerMacroblock),
		maxBitRate:         stated(limits.maxBr * bitsPerKilobit),
	}, true
}

// h265Level is MaxLumaPs from ITU-T H.265 (V5, 02/2018) Table A.8 and
// MaxLumaSr from Table A.9. No bit rate: H.265 states MaxBR per tier, and
// offers often leave the tier out.
type h265Level struct {
	maxLumaPs, maxLumaSr float64
}

// h265LevelLimits is Tables A.8 and A.9 keyed by general_level_idc (thirty
// times the level number). Mirrors matterjs-server
// packages/ws-controller/src/camera/codecLevels.ts:105-124 at 0bd06f2.
var h265LevelLimits = map[int64]h265Level{
	30:  {36864, 552960},
	60:  {122880, 3686400},
	63:  {245760, 7372800},
	90:  {552960, 16588800},
	93:  {983040, 33177600},
	120: {2228224, 66846720},
	123: {2228224, 133693440},
	150: {8912896, 267386880},
	153: {8912896, 534773760},
	156: {8912896, 1069547520},
	180: {35651584, 1069547520},
	183: {35651584, 2139095040},
	186: {35651584, 4278190080},
}

// h265LevelIDLimits reads a level-id, which is general_level_idc
// (RFC 7798 §7.1); ok false means unreadable, not unlimited. Mirrors
// matterjs-server packages/ws-controller/src/camera/codecLevels.ts:126-132
// at 0bd06f2.
func h265LevelIDLimits(levelID string) (videoCodecLimits, bool) {
	if !h265LevelIDPattern.MatchString(levelID) {
		return videoCodecLimits{}, false
	}
	idc, _ := strconv.ParseInt(levelID, 10, 64)
	limits, ok := h265LevelLimits[idc]
	if !ok {
		return videoCodecLimits{}, false
	}
	return videoCodecLimits{maxPixels: stated(limits.maxLumaPs), maxPixelsPerSecond: stated(limits.maxLumaSr)}, true
}
