// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"strings"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/controller/camera"
)

// TestWireNamesParityMatterjsServer holds the exported wire names against
// matterjs-server packages/ws-controller/src/camera/wireNames.ts at
// 0bd06f2: the codec tables (:16-31), the decimal spelling of a value
// without a name (wireName, :44-46), the case-insensitive byName
// (wireValue, :48-54) and imageCodecByName's decimal fallback (:68-73).
func TestWireNamesParityMatterjsServer(t *testing.T) {
	t.Parallel()

	video := map[csdef.VideoCodecEnum]string{ // the tables of the file above
		csdef.VideoCodecH264: "H264", csdef.VideoCodecHevc: "H265",
		csdef.VideoCodecVvc: "H266", csdef.VideoCodecAv1: "AV1",
	}
	audio := map[csdef.AudioCodecEnum]string{
		csdef.AudioCodecOpus: "OPUS", csdef.AudioCodecAacLc: "AAC",
	}
	image := map[csdef.ImageCodecEnum]string{
		csdef.ImageCodecJpeg: "JPEG", csdef.ImageCodecHeic: "HEIC",
	}

	t.Run("names", func(t *testing.T) {
		t.Parallel()
		for c, want := range video {
			if got := camera.VideoCodecName(c); got != want {
				t.Errorf("VideoCodecName(%d) = %q, want %q", c, got, want)
			}
		}
		for c, want := range audio {
			if got := camera.AudioCodecName(c); got != want {
				t.Errorf("AudioCodecName(%d) = %q, want %q", c, got, want)
			}
		}
		for c, want := range image {
			if got := camera.ImageCodecName(c); got != want {
				t.Errorf("ImageCodecName(%d) = %q, want %q", c, got, want)
			}
		}
		for u, want := range map[csdef.StreamUsageEnum]string{
			csdef.StreamUsageInternal: "Internal", csdef.StreamUsageRecording: "Recording",
			csdef.StreamUsageAnalysis: "Analysis", csdef.StreamUsageLiveView: "LiveView",
		} {
			if got := camera.StreamUsageName(u); got != want {
				t.Errorf("StreamUsageName(%d) = %q, want %q", u, got, want)
			}
		}
		for s, want := range map[csdef.TwoWayTalkSupportTypeEnum]string{
			csdef.TwoWayTalkSupportTypeNotSupported: "NotSupported",
			csdef.TwoWayTalkSupportTypeHalfDuplex:   "HalfDuplex",
			csdef.TwoWayTalkSupportTypeFullDuplex:   "FullDuplex",
		} {
			if got := camera.TwoWayTalkSupportName(s); got != want {
				t.Errorf("TwoWayTalkSupportName(%d) = %q, want %q", s, got, want)
			}
		}
	})

	t.Run("a value without a name is its decimal spelling", func(t *testing.T) {
		t.Parallel()
		for got, want := range map[string]string{
			camera.VideoCodecName(9):        "9",
			camera.AudioCodecName(7):        "7",
			camera.ImageCodecName(200):      "200",
			camera.StreamUsageName(5):       "5",
			camera.TwoWayTalkSupportName(3): "3",
		} {
			if got != want {
				t.Errorf("name %q, want %q", got, want)
			}
		}
	})

	t.Run("byName round-trips every name, ignoring case", func(t *testing.T) {
		t.Parallel()
		for c, name := range video {
			for _, spelled := range []string{name, strings.ToLower(name)} {
				if got, ok := camera.VideoCodecByName(spelled); !ok || got != c {
					t.Errorf("VideoCodecByName(%q) = %d, %v; want %d", spelled, got, ok, c)
				}
			}
		}
		for c, name := range audio {
			if got, ok := camera.AudioCodecByName(strings.ToLower(name)); !ok || got != c {
				t.Errorf("AudioCodecByName(%q) = %d, %v; want %d", name, got, ok, c)
			}
		}
		for c, name := range image {
			if got, ok := camera.ImageCodecByName(strings.ToLower(name)); !ok || got != c {
				t.Errorf("ImageCodecByName(%q) = %d, %v; want %d", name, got, ok, c)
			}
		}
		if got, ok := camera.StreamUsageByName("liveview"); !ok || got != csdef.StreamUsageLiveView {
			t.Errorf("StreamUsageByName(liveview) = %d, %v", got, ok)
		}
	})

	t.Run("the decimal spelling is accepted back for a codec", func(t *testing.T) {
		t.Parallel()
		if got, ok := camera.ImageCodecByName("7"); !ok || got != 7 { // :72
			t.Errorf("ImageCodecByName(7) = %d, %v", got, ok)
		}
		if got, ok := camera.ImageCodecByName(camera.ImageCodecName(200)); !ok || got != 200 {
			t.Errorf("ImageCodecByName of the decimal name = %d, %v", got, ok)
		}
		if got, ok := camera.VideoCodecByName("9"); !ok || got != 9 {
			t.Errorf("VideoCodecByName(9) = %d, %v", got, ok)
		}
		if got := camera.VideoCodecName(9); got != "9" {
			t.Errorf("VideoCodecName(9) = %q, want the decimal spelling VideoCodecByName parses back", got)
		}
		if got, ok := camera.AudioCodecByName("1"); !ok || got != csdef.AudioCodecAacLc {
			t.Errorf("AudioCodecByName(1) = %d, %v", got, ok)
		}
	})

	t.Run("an unknown name is refused", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"", "VP8", "Hevc", "-1", "+1", "1.0", "256", " 1"} {
			if got, ok := camera.VideoCodecByName(name); ok {
				t.Errorf("VideoCodecByName(%q) = %d, want refused", name, got)
			}
			if got, ok := camera.ImageCodecByName(name); ok {
				t.Errorf("ImageCodecByName(%q) = %d, want refused", name, got)
			}
		}
		// Only member names for a usage (wireNames.ts streamUsageByName).
		for _, name := range []string{"3", "Live", ""} {
			if got, ok := camera.StreamUsageByName(name); ok {
				t.Errorf("StreamUsageByName(%q) = %d, want refused", name, got)
			}
		}
	})
}
