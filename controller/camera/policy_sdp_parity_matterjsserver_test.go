// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// The cases of matterjs-server's
// packages/ws-controller/test/SdpConstraintsTest.ts:313-1045 at 0bd06f2,
// ported by meaning with the offers of its fixtures (:21-311).

const (
	sdpH264 = csdef.VideoCodecH264
	sdpH265 = csdef.VideoCodecHevc
	sdpAV1  = csdef.VideoCodecAv1
)

// crlf joins SDP lines with CRLF.
func crlf(lines ...string) string { return strings.Join(lines, "\r\n") }

var sdpSessionHead = []string{"v=0", "o=- 1 1 IN IP4 127.0.0.1", "s=-", "t=0 0"}

// sdpOffer is the fixtures' session head, then lines, then a final break.
func sdpOffer(lines ...string) string {
	return crlf(append(append(slices.Clone(sdpSessionHead), lines...), "")...)
}

// offerWithFmtp is a receiving video section offering one codec, with params
// as its only a=fmtp line.
func offerWithFmtp(codec, params string) string {
	return sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=recvonly", "a=rtpmap:102 "+codec+"/90000", "a=fmtp:102 "+params)
}

var (
	offerH265ThenH264 = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 100 102", "c=IN IP4 0.0.0.0", "a=recvonly",
		"a=rtpmap:100 H265/90000", "a=fmtp:100 level-id=93",
		"a=rtpmap:102 H264/90000", "a=fmtp:102 profile-level-id=42e01f;max-fs=8160;max-mbps=245760;max-br=5000")
	offerTwoVideoLevelCaps = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 100 102", "c=IN IP4 0.0.0.0", "a=recvonly",
		"a=rtpmap:100 H265/90000", "a=fmtp:100 max-lps=2088960", "a=rtpmap:102 H264/90000", "a=fmtp:102 max-fs=3600")
	// RFC 6838 §4.3 makes media type parameter names case-insensitive.
	offerUpperCaseFmtp = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=recvonly",
		"a=rtpmap:102 H264/90000", "a=fmtp:102 profile-level-id=42e01f;MAX-FS=3600;Max-Mbps=108000;MAX-FR=15;Max-Br=2000")
	offerAudioSendrecv = sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=sendrecv", "a=rtpmap:111 opus/48000/2")
	offerAudioSendonly = sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=sendonly", "a=rtpmap:111 opus/48000/2")
	offerSameCodecTwo  = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 100 102", "c=IN IP4 0.0.0.0", "a=recvonly",
		"a=rtpmap:100 H264/90000", "a=fmtp:100 profile-level-id=42e01f;max-fs=3600",
		"a=rtpmap:102 H264/90000", "a=fmtp:102 profile-level-id=640c1f;max-fs=8160")
	offerVideoOnlyRejected = sdpOffer("m=video 0 UDP/TLS/RTP/SAVPF 100", "c=IN IP4 0.0.0.0", "a=rtpmap:100 H265/90000")
	offerRejectedSections  = sdpOffer(
		"m=video 0 UDP/TLS/RTP/SAVPF 100", "c=IN IP4 0.0.0.0", "a=recvonly", "a=rtpmap:100 AV1/90000", "a=fmtp:100 max-fs=900",
		"m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=recvonly", "a=rtpmap:102 H264/90000",
		"m=audio 0 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=sendrecv", "a=rtpmap:111 opus/48000/2")
	offerVideoSendonly    = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=sendonly", "a=rtpmap:102 H264/90000", "a=fmtp:102 max-fs=900")
	offerVideoInactive    = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=inactive", "a=rtpmap:102 H264/90000")
	offerVideoNoDirection = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=rtpmap:102 H264/90000", "a=fmtp:102 max-fs=3600")
	offerSessionInactive  = sdpOffer("a=inactive",
		"m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=rtpmap:102 H264/90000",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=recvonly", "a=rtpmap:111 opus/48000/2")
	offerVideoSendonlyAndRecvonly = sdpOffer(
		"m=video 9 UDP/TLS/RTP/SAVPF 100", "c=IN IP4 0.0.0.0", "a=sendonly", "a=rtpmap:100 AV1/90000", "a=fmtp:100 max-fs=900",
		"m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=recvonly", "a=rtpmap:102 H264/90000")
	offerAudioNoDirection = sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=rtpmap:111 opus/48000/2")
	offerSessionSendrecv  = sdpOffer("a=sendrecv",
		"m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=rtpmap:102 H264/90000",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=rtpmap:111 opus/48000/2")
	offerVideoSendrecv     = sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=sendrecv", "a=rtpmap:102 H264/90000")
	offerTwoAudioSections  = sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 8", "c=IN IP4 0.0.0.0", "a=inactive", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=sendrecv", "a=rtpmap:111 opus/48000/2")
	offerRejectedAndSendon = sdpOffer("m=video 0 UDP/TLS/RTP/SAVPF 100", "c=IN IP4 0.0.0.0", "a=rtpmap:100 AV1/90000",
		"m=video 9 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=sendonly", "a=rtpmap:102 H264/90000")
)

// bundledVideoOffer: port 0 with a=bundle-only is live on the bundle
// transport (RFC 8843); without it port 0 refuses the section.
func bundledVideoOffer(bundleOnly bool) string {
	lines := []string{
		"a=group:BUNDLE 0 1", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "c=IN IP4 0.0.0.0", "a=mid:0", "a=recvonly", "a=rtpmap:111 opus/48000/2",
		"m=video 0 UDP/TLS/RTP/SAVPF 102", "c=IN IP4 0.0.0.0", "a=mid:1",
	}
	if bundleOnly {
		lines = append(lines, "a=bundle-only")
	}
	return sdpOffer(append(lines, "a=recvonly", "a=rtpmap:102 H264/90000")...)
}

func parsed(sdp string) *sdpVideoConstraints {
	c := parseSdpVideoConstraints(sdp)
	return &c
}

// limitsOf is videoCodecLimits of an offer.
func limitsOf(sdp string, codec csdef.VideoCodecEnum) selectedVideoCodecLimits {
	return videoCodecLimitsOf(parsed(sdp), codec)
}

// want is a selected limit set; a negative argument is unset.
func want(codec csdef.VideoCodecEnum, maxPixels, maxPixelsPerSecond, maxFrameRate, maxBitRate float64) selectedVideoCodecLimits {
	opt := func(v float64) ceiling {
		if v < 0 {
			return ceiling{}
		}
		return stated(v)
	}
	return selectedVideoCodecLimits{codec: codec, videoCodecLimits: videoCodecLimits{
		maxPixels: opt(maxPixels), maxPixelsPerSecond: opt(maxPixelsPerSecond), maxFrameRate: opt(maxFrameRate), maxBitRate: opt(maxBitRate),
	}}
}

// TestParseSdpVideoConstraintsLimitsParityMatterjsServer is the limit cases
// of describe("parseSdpVideoConstraints") (SdpConstraintsTest.ts:315-511,
// :666-682, matterjs-server at 0bd06f2).
func TestParseSdpVideoConstraintsLimitsParityMatterjsServer(t *testing.T) {
	cases := []struct {
		name string
		got  selectedVideoCodecLimits
		want selectedVideoCodecLimits
	}{
		// max-fs counts 16x16 macroblocks: 8160 * 256 = 2088960 pixels.
		{"converts max-fs macroblocks to a pixel ceiling for the codec that stated it", limitsOf(offerH265ThenH264, sdpH264), want(sdpH264, 2088960, 245760*256, -1, 5000*1000)},
		{"converts max-mbps macroblocks per second to a pixel rate ceiling", limitsOf(offerH265ThenH264, sdpH264), want(sdpH264, 8160*256, 245760*256, -1, 5000*1000)},
		{"reads max-br as kilobits per second and reports bits per second", limitsOf(offerH265ThenH264, sdpH264), want(sdpH264, 8160*256, 245760*256, -1, 5000*1000)},
		{"reads an fmtp parameter name written in upper case", limitsOf(offerUpperCaseFmtp, sdpH264), want(sdpH264, 3600*256, 108000*256, 15, 2000*1000)},
		// H265 states only level-id=93; the max-fs=8160 beside it belongs to
		// H264.
		{"bounds a codec offered at a level alone by that level, not by another codec's max-fs", limitsOf(offerH265ThenH264, sdpH265), want(sdpH265, 983040, 33177600, -1, -1)},
		// H.265 states frame size in luma samples (max-lps), H.264 in
		// macroblocks (max-fs).
		{"keeps each codec's level cap apart instead of folding them into one", limitsOf(offerTwoVideoLevelCaps, sdpH265), want(sdpH265, 2088960, -1, -1, -1)},
		{"keeps each codec's level cap apart instead of folding them into one", limitsOf(offerTwoVideoLevelCaps, sdpH264), want(sdpH264, 3600*256, -1, -1, -1)},
		// The controller does not pick the profile the camera encodes in, so
		// only the tighter bound is safe.
		{"takes the tighter of two payload types of the same codec", limitsOf(offerSameCodecTwo, sdpH264), want(sdpH264, 3600*256, 108000*256, -1, 14000*1000)},
		// H.264 Table A-1, level 3.1: MaxFS 3600, MaxMBPS 108000, MaxBR 14000.
		{"reads an H.264 level as a frame size, pixel rate and bit rate ceiling", limitsOf(offerWithFmtp("H264", "profile-level-id=42e01f"), sdpH264), want(sdpH264, 3600*256, 108000*256, -1, 14000*1000)},
		// RFC 6184 §8.1: max-fs signals a capability at or above the
		// level's.
		{"lets an explicit max-fs raise the level's frame size", limitsOf(offerWithFmtp("H264", "profile-level-id=42e01f;max-fs=8160"), sdpH264), want(sdpH264, 8160*256, 108000*256, -1, 14000*1000)},
		// 0x4d = Main, 0x50 sets constraint_set3.
		{"reads constraint_set3_flag on a Baseline level_idc 11 as level 1b", limitsOf(offerWithFmtp("H264", "profile-level-id=4d500b"), sdpH264), want(sdpH264, 99*256, 1485*256, -1, 128*1000)},
		{"reads the same level_idc without that flag as level 1.1", limitsOf(offerWithFmtp("H264", "profile-level-id=4d400b"), sdpH264), want(sdpH264, 396*256, 3000*256, -1, 192*1000)},
		// 0x64 is High, which RFC 6184 §8.1 leaves out of the level 1b rule.
		{"reads constraint_set3_flag outside those three profiles as level 1.1", limitsOf(offerWithFmtp("H264", "profile-level-id=64100b"), sdpH264), want(sdpH264, 396*256, 3000*256, -1, 192*1000)},
		// H.264 §A.3.1 gives level 1b two spellings.
		{"reads level_idc 9 as level 1b, which is how the other profiles spell it", limitsOf(offerWithFmtp("H264", "profile-level-id=640009"), sdpH264), want(sdpH264, 99*256, 1485*256, -1, 128*1000)},
		// RFC 7798 §7.1 states max-fps in frames per 100 seconds.
		{"reads H.265 max-fps as frames per 100 seconds", limitsOf(offerWithFmtp("H265", "level-id=93;max-fps=1500"), sdpH265), want(sdpH265, 983040, 33177600, 15, -1)},
		{"states no limits for a codec the offer never mentioned", limitsOf(offerH265ThenH264, sdpH264+99), want(sdpH264+99, -1, -1, -1, -1)},
		// max-fs and max-fr are RFC 6184 / RFC 7741 names that H.265 does
		// not define.
		{"reads no H.264 parameter name on an H.265 record", limitsOf(offerWithFmtp("H265", "level-id=93;max-fs=1000000;max-fr=60"), sdpH265), want(sdpH265, 983040, 33177600, -1, -1)},
		// RFC 7798 §7.1 states both in luma samples, not macroblocks.
		{"reads H.265's own max-lps and max-lsr in luma samples", limitsOf(offerWithFmtp("H265", "level-id=93;max-lps=2228224;max-lsr=133693440"), sdpH265), want(sdpH265, 2228224, 133693440, -1, -1)},
		{"reads max-fr as a frame rate ceiling for the codec that stated it", limitsOf(crlf("v=0", "o=- 0 0 IN IP4 127.0.0.1", "s=-", "t=0 0", "m=video 9 UDP/TLS/RTP/SAVPF 96", "a=rtpmap:96 H264/90000", "a=fmtp:96 max-fs=8160;max-fr=10", ""), sdpH264), want(sdpH264, 8160*256, -1, 10, -1)},
		{"states no limits when there is no offer at all", videoCodecLimitsOf(nil, sdpH265), want(sdpH265, -1, -1, -1, -1)},
		{"ignores a rejected video section's codecs and limits while a live one stands", limitsOf(offerRejectedSections, sdpAV1), want(sdpAV1, -1, -1, -1, -1)},
		{"refuses our video for a sendonly section, with no codecs and no limits from it", limitsOf(offerVideoSendonly, sdpH264), want(sdpH264, -1, -1, -1, -1)},
		{"treats a section with no direction as sendrecv", limitsOf(offerVideoNoDirection, sdpH264), want(sdpH264, 3600*256, -1, -1, -1)},
		{"ignores a sendonly section's codecs and limits while a receiving one stands", limitsOf(offerVideoSendonlyAndRecvonly, sdpAV1), want(sdpAV1, -1, -1, -1, -1)},
		// Every codec the controller can select is dynamically mapped, so
		// this limit belongs to none of them.
		{"states no limit for an fmtp line whose payload type has no rtpmap", limitsOf(sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 96 98", "a=recvonly", "a=rtpmap:96 H265/90000", "a=fmtp:98 max-fs=900"), sdpH265), want(sdpH265, -1, -1, -1, -1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("limits = %+v, want %+v", c.got, c.want)
			}
		})
	}
}

// TestParseSdpVideoConstraintsDispositionParityMatterjsServer is the media
// and talkback cases of describe("parseSdpVideoConstraints")
// (SdpConstraintsTest.ts:315-327, :397-415, :513-682, matterjs-server at
// 0bd06f2).
func TestParseSdpVideoConstraintsDispositionParityMatterjsServer(t *testing.T) {
	receivingWith := func(codecs ...string) mediaDisposition {
		return mediaDisposition{state: mediaReceiving, codecs: codecs}
	}
	notReceiving := func(direction string) mediaDisposition {
		return mediaDisposition{state: mediaNotReceiving, direction: direction}
	}
	cases := []struct {
		name         string
		sdp          string
		video, audio *mediaDisposition
		// talkback: nil does not check, else whether sendingAudioSections
		// is non-empty.
		talkback *bool
	}{
		{"lists video codecs in m-line preference order", offerH265ThenH264, ptr(receivingWith("H265", "H264")), nil, nil},
		{"reports a video m-line and no audio m-line", offerH265ThenH264, ptr(receivingWith("H265", "H264")), &mediaDisposition{state: mediaAbsent}, nil},
		{"separates a refused video section from an offer that has none", offerVideoOnlyRejected, &mediaDisposition{state: mediaRefused}, nil, nil},
		{"separates a refused video section from an offer that has none", offerAudioSendrecv, &mediaDisposition{state: mediaAbsent}, nil, nil},
		{"counts a port 0 video section marked bundle-only as offered", bundledVideoOffer(true), ptr(receivingWith("H264")), nil, nil},
		{"still refuses a port 0 video section that is not bundle-only", bundledVideoOffer(false), &mediaDisposition{state: mediaRefused}, nil, nil},
		{"ignores a rejected video section's codecs and limits while a live one stands", offerRejectedSections, ptr(receivingWith("H264")), nil, nil},
		{"reports a rejected audio section as refused, with no codecs and no talkback", offerRejectedSections, nil, &mediaDisposition{state: mediaRefused}, ptr(false)},
		{"reports audio codecs and talkback intent from a sendrecv audio m-line", offerAudioSendrecv, nil, ptr(receivingWith("OPUS")), ptr(true)},
		// sdp-transform builds media.rtp from a=rtpmap lines only, so static
		// payload types name no codec.
		{"separates an offered section that names no codec from one that names some", crlf("v=0", "o=- 0 0 IN IP4 127.0.0.1", "s=-", "t=0 0", "m=audio 9 RTP/AVP 0 8", "a=recvonly", ""), nil, &mediaDisposition{state: mediaReceiving}, nil},
		{"reports talkback intent from a sendonly audio m-line", offerAudioSendonly, nil, nil, ptr(true)},
		{"reports no talkback for an offer with no audio section", offerH265ThenH264, nil, nil, ptr(false)},
		{"reports no talkback for a recvonly audio section", offerSessionInactive, nil, ptr(receivingWith("OPUS")), ptr(false)},
		// RFC 4566 §6 makes it sendrecv, and sendrecv offers to send.
		{"reads an audio section stating no direction as asking for talkback", offerAudioNoDirection, nil, ptr(receivingWith("OPUS")), ptr(true)},
		{"applies a permissive session-level direction to a section that restates none", offerSessionSendrecv, ptr(receivingWith("H264")), ptr(receivingWith("OPUS")), ptr(true)},
		{"treats an explicit sendrecv video section as receiving", offerVideoSendrecv, ptr(receivingWith("H264")), nil, nil},
		{"takes one receiving audio section among several of that kind", offerTwoAudioSections, &mediaDisposition{state: mediaAbsent}, ptr(receivingWith("OPUS")), ptr(true)},
		// Both forbid the track; the live section is the one still in the
		// negotiation.
		{"reports the live statement when one section is rejected and another will not receive", offerRejectedAndSendon, ptr(notReceiving("sendonly")), nil, nil},
		{"reports no constraints for an offer it cannot parse", "not an sdp", &mediaDisposition{state: mediaAbsent}, nil, nil},
		// A nonzero port is not permission to send: the peer will only send
		// on this section.
		{"refuses our video for a sendonly section, with no codecs and no limits from it", offerVideoSendonly, ptr(notReceiving("sendonly")), nil, nil},
		{"refuses our video for an inactive section", offerVideoInactive, ptr(notReceiving("inactive")), nil, nil},
		// RFC 4566 §6: a section with no direction attribute is sendrecv.
		{"treats a section with no direction as sendrecv", offerVideoNoDirection, ptr(receivingWith("H264")), nil, nil},
		{"treats a recvonly section as receiving", offerH265ThenH264, ptr(receivingWith("H265", "H264")), nil, nil},
		{"refuses our audio for a sendonly section while still reading its talkback request", offerAudioSendonly, nil, ptr(notReceiving("sendonly")), ptr(true)},
		// RFC 4566 §5: a session-level attribute applies to every section
		// that does not override it.
		{"applies a session-level direction to a section that restates none", offerSessionInactive, ptr(notReceiving("inactive")), ptr(receivingWith("OPUS")), nil},
		{"ignores a sendonly section's codecs and limits while a receiving one stands", offerVideoSendonlyAndRecvonly, ptr(receivingWith("H264")), nil, nil},
		{"states no limit for an fmtp line whose payload type has no rtpmap", sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 96 98", "a=recvonly", "a=rtpmap:96 H265/90000", "a=fmtp:98 max-fs=900"), ptr(receivingWith("H265")), nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parsed(c.sdp)
			if c.video != nil && !reflect.DeepEqual(got.video, *c.video) {
				t.Errorf("video = %+v, want %+v", got.video, *c.video)
			}
			if c.audio != nil && !reflect.DeepEqual(got.audio, *c.audio) {
				t.Errorf("audio = %+v, want %+v", got.audio, *c.audio)
			}
			if c.talkback != nil && (len(got.sendingAudioSections) > 0) != *c.talkback {
				t.Errorf("sendingAudioSections = %+v, want talkback %v", got.sendingAudioSections, *c.talkback)
			}
		})
	}
	unreadable := []struct{ name, codec, params string }{
		// The parameter overrides the level, so the level is not a ceiling
		// the peer still states.
		{"refuses the codec when an fmtp capability value is not whole digits", "H264", "profile-level-id=42e01f;max-fs=8160px"},
		// Reading the malformed value as absent would leave the codec
		// unbounded.
		{"refuses the codec when a malformed fmtp value stands beside no level at all", "H264", "max-fs=8160px"},
		{"refuses the codec for a malformed max-br, which no level states for H.265", "H265", "level-id=93;max-br=1e6"},
	}
	for _, c := range unreadable {
		t.Run(c.name, func(t *testing.T) {
			sdp := parsed(offerWithFmtp(c.codec, c.params))
			decodable, unreadable, ok := decodableVideoCodecs(sdp)
			if !ok || len(decodable) != 0 || !slices.Equal(unreadable, []string{c.codec}) {
				t.Errorf("decodable = %v, unreadable = %v, %v", decodable, unreadable, ok)
			}
			if l := videoCodecLimitsOf(sdp, sdpH264); l.maxPixels.set {
				t.Errorf("maxPixels = %v, want unset", l.maxPixels)
			}
		})
	}
}

// TestDecodableVideoCodecsParityMatterjsServer is
// describe("decodableVideoCodecs") (SdpConstraintsTest.ts:685-752,
// matterjs-server at 0bd06f2).
func TestDecodableVideoCodecsParityMatterjsServer(t *testing.T) {
	cases := []struct {
		name                  string
		sdp                   string
		decodable, unreadable []string
	}{
		// 0xff is not an H.264 Table A-1 level; reading it as "no limit"
		// could exceed the peer's ceiling.
		{"refuses a codec whose level_idc names no row of the level table", offerWithFmtp("H264", "profile-level-id=42e0ff"), nil, []string{"H264"}},
		{"refuses a profile-level-id that is not three bytes of base16", offerWithFmtp("H264", "profile-level-id=42e0"), nil, []string{"H264"}},
		// The third byte 0x1f is a valid level (3.1), so only the length
		// check refuses this one.
		{"refuses a profile-level-id that is not three bytes of base16", offerWithFmtp("H264", "profile-level-id=42e01f0"), nil, []string{"H264"}},
		{"refuses an H.265 level-id outside the level tables", offerWithFmtp("H265", "level-id=99"), nil, []string{"H265"}},
		// Number("93.0") is 93, a Table A.8 row, so only the digit check
		// refuses it.
		{"refuses a level-id that is not decimal digits, where Number would still read a level", offerWithFmtp("H265", "level-id=93.0"), nil, []string{"H265"}},
		{"keeps the codec whose level it can read beside the one it cannot", sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 100 102", "c=IN IP4 0.0.0.0", "a=recvonly",
			"a=rtpmap:100 H265/90000", "a=fmtp:100 level-id=93", "a=rtpmap:102 H264/90000", "a=fmtp:102 profile-level-id=42e0ff"), []string{"H265"}, []string{"H264"}},
		{"keeps a codec that states no level at all", offerWithFmtp("H264", "max-fs=3600"), []string{"H264"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			decodable, unreadable, ok := decodableVideoCodecs(parsed(c.sdp))
			if !ok || !slices.Equal(decodable, c.decodable) || !slices.Equal(unreadable, c.unreadable) {
				t.Errorf("decodable = %v, unreadable = %v, %v; want %v, %v", decodable, unreadable, ok, c.decodable, c.unreadable)
			}
		})
	}
	t.Run("refuses a codec whose level_idc names no row of the level table", func(t *testing.T) {
		if got := limitsOf(offerWithFmtp("H264", "profile-level-id=42e0ff"), sdpH264); got != want(sdpH264, -1, -1, -1, -1) {
			t.Errorf("limits = %+v, want none", got)
		}
	})
	// Static payload types name no codec; an empty decodable list would read
	// as "decodes nothing".
	t.Run("states nothing to narrow by when the section named no codec", func(t *testing.T) {
		if _, _, ok := decodableVideoCodecs(parsed(sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 34", "c=IN IP4 0.0.0.0", "a=recvonly"))); ok {
			t.Error("a section naming no codec states codecs to narrow by")
		}
	})
}

// TestMediaRefusalParityMatterjsServer is describe("mediaRefusal")
// (SdpConstraintsTest.ts:754-781, matterjs-server at 0bd06f2).
func TestMediaRefusalParityMatterjsServer(t *testing.T) {
	cases := []struct {
		name    string
		offer   *sdpVideoConstraints
		kind    string
		want    mediaDisposition
		refuses bool
	}{
		{"names the statement that forbids a track, and answers nothing for the one that does not", parsed(offerVideoOnlyRejected), "video", mediaDisposition{state: mediaRefused}, true},
		{"names the statement that forbids a track, and answers nothing for the one that does not", parsed(offerVideoSendonly), "video", mediaDisposition{state: mediaNotReceiving, direction: "sendonly"}, true},
		{"names the statement that forbids a track, and answers nothing for the one that does not", parsed(offerVideoInactive), "video", mediaDisposition{state: mediaNotReceiving, direction: "inactive"}, true},
		{"names the statement that forbids a track, and answers nothing for the one that does not", parsed(offerH265ThenH264), "video", mediaDisposition{}, false},
		{"refuses a kind the offer carries no section for", parsed(offerH265ThenH264), "audio", mediaDisposition{state: mediaAbsent}, true},
		{"refuses a kind the offer carries no section for", parsed("v=0"), "video", mediaDisposition{state: mediaAbsent}, true},
		{"refuses nothing when there is no offer to answer", nil, "video", mediaDisposition{}, false},
		{"refuses nothing when there is no offer to answer", nil, "audio", mediaDisposition{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, refuses := mediaRefusal(c.offer, c.kind)
			if refuses != c.refuses || !reflect.DeepEqual(got, c.want) {
				t.Errorf("mediaRefusal(%s) = %+v, %v; want %+v, %v", c.kind, got, refuses, c.want, c.refuses)
			}
		})
	}
}

// TestWithoutTalkbackParityMatterjsServer is describe("withoutTalkback")
// (SdpConstraintsTest.ts:784-957, matterjs-server at 0bd06f2).
func TestWithoutTalkbackParityMatterjsServer(t *testing.T) {
	rewritten := func(sdp string) string { return withoutTalkback(sdp, parsed(sdp).sendingAudioSections) }
	t.Run("names each sending audio section by its m-line position and the direction in effect", func(t *testing.T) {
		for _, sdp := range []string{offerTwoAudioSections, offerSessionSendrecv} {
			if got := parsed(sdp).sendingAudioSections; !slices.Equal(got, []sendingAudioSection{{index: 1, direction: "sendrecv"}}) {
				t.Errorf("sendingAudioSections = %+v", got)
			}
		}
	})
	head := slices.Clone(sdpSessionHead)
	cases := []struct {
		name, sdp, want string
	}{
		{
			"makes a sendrecv audio section recvonly and leaves a sendrecv video section alone",
			sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 96", "a=sendrecv", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=sendrecv", "a=rtpmap:111 opus/48000/2"),
			sdpOffer("m=video 9 UDP/TLS/RTP/SAVPF 96", "a=sendrecv", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly", "a=rtpmap:111 opus/48000/2"),
		},
		{
			"leaves an extmap line with a direction suffix alone",
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=extmap:2/recvonly urn:ietf:params:rtp-hdrext:csrc-audio-level", "a=sendrecv"),
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=extmap:2/recvonly urn:ietf:params:rtp-hdrext:csrc-audio-level", "a=recvonly"),
		},
		{"makes a sendonly audio section inactive", sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=sendonly"), sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=inactive")},
		{
			"adds recvonly at the end of an audio section that states no direction",
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=rtpmap:111 opus/48000/2", "m=video 9 UDP/TLS/RTP/SAVPF 96", "a=recvonly"),
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=rtpmap:111 opus/48000/2", "a=recvonly", "m=video 9 UDP/TLS/RTP/SAVPF 96", "a=recvonly"),
		},
		{
			"adds the direction to the last section of an offer with no final line break",
			crlf(append(slices.Clone(head), "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=rtpmap:111 opus/48000/2")...),
			crlf(append(slices.Clone(head), "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=rtpmap:111 opus/48000/2")...) + "\r\na=recvonly",
		},
		{
			"keeps the offer's own line breaks",
			strings.Join(append(slices.Clone(head), "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=sendrecv", ""), "\n"),
			strings.Join(append(slices.Clone(head), "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly", ""), "\n"),
		},
		{"keeps the offer's own line breaks", offerAudioNoDirection, offerAudioNoDirection + "a=recvonly\r\n"},
		{
			"rewrites every sending audio section and no other",
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=sendrecv", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly", "m=audio 0 UDP/TLS/RTP/SAVPF 111", "a=sendrecv", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=sendonly"),
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly", "m=audio 0 UDP/TLS/RTP/SAVPF 111", "a=sendrecv", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=inactive"),
		},
		{
			"adds the direction to each of several audio sections that state none",
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:0", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:1"),
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:0", "a=recvonly", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:1", "a=recvonly"),
		},
		{
			"rewrites every direction line of a section that states more than one",
			sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly", "a=sendrecv"), sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly", "a=recvonly"),
		},
		{
			"counts an application section when locating the audio section after it",
			sdpOffer("m=application 9 UDP/DTLS/SCTP webrtc-datachannel", "a=sendrecv", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=sendrecv"),
			sdpOffer("m=application 9 UDP/DTLS/SCTP webrtc-datachannel", "a=sendrecv", "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly"),
		},
		{
			"adds the direction after the section's last line, not after a blank line in it",
			strings.Join(append(slices.Clone(head), "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:0", "", "a=mid:1", ""), "\r"),
			strings.Join(append(slices.Clone(head), "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:0", "", "a=mid:1", "a=recvonly", ""), "\r"),
		},
		{
			"rewrites a bundle-only audio section, which is live although its port is 0",
			sdpOffer("m=audio 0 UDP/TLS/RTP/SAVPF 111", "a=bundle-only", "a=sendrecv"), sdpOffer("m=audio 0 UDP/TLS/RTP/SAVPF 111", "a=bundle-only", "a=recvonly"),
		},
		// The session line still applies to the video section, which states
		// none.
		{"overrides a sending session-level direction in the audio section only", offerSessionSendrecv, offerSessionSendrecv + "a=recvonly\r\n"},
		{"overrides a sending session-level direction in the audio section only", sdpOffer("a=sendonly", "m=audio 9 UDP/TLS/RTP/SAVPF 111"), sdpOffer("a=sendonly", "m=audio 9 UDP/TLS/RTP/SAVPF 111") + "a=inactive\r\n"},
		{"leaves an offer that asks for no talkback unchanged", sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly"), sdpOffer("m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=recvonly")},
		{"leaves an offer that asks for no talkback unchanged", "not an sdp", "not an sdp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rewritten(c.sdp); got != c.want {
				t.Errorf("rewritten =\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

// TestAnswerWithinOfferedDirectionsParityMatterjsServer is
// describe("answerWithinOfferedDirections") (SdpConstraintsTest.ts:959-1045,
// matterjs-server at 0bd06f2).
func TestAnswerWithinOfferedDirectionsParityMatterjsServer(t *testing.T) {
	const video, audio = "m=video 9 UDP/TLS/RTP/SAVPF 96", "m=audio 9 UDP/TLS/RTP/SAVPF 111"
	cases := []struct {
		name, answer, offer, want string
		corrected                 []int
	}{
		{
			"narrows sendrecv to sendonly in a section offered recvonly and keeps a sendrecv offer's answer",
			sdpOffer(video, "a=sendrecv", audio, "a=sendrecv"), sdpOffer(video, "a=recvonly", audio, "a=sendrecv"),
			sdpOffer(video, "a=sendonly", audio, "a=sendrecv"),
			[]int{0},
		},
		{
			"answers a section offered inactive with inactive and one offered sendonly with recvonly",
			sdpOffer(video, "a=sendrecv", audio, "a=sendrecv"), sdpOffer(video, "a=inactive", audio, "a=sendonly"),
			sdpOffer(video, "a=inactive", audio, "a=recvonly"),
			[]int{0, 1},
		},
		{
			"reads the offer's session-level direction for a section that states none",
			sdpOffer(video, "a=sendrecv"), sdpOffer("a=recvonly", video), sdpOffer(video, "a=sendonly"),
			[]int{0},
		},
		{
			"keeps the answer's session-level lines and a section with no offered counterpart",
			sdpOffer("a=sendrecv", video, "a=sendonly", audio, "a=sendrecv"), sdpOffer(video, "a=recvonly"),
			sdpOffer("a=sendrecv", video, "a=sendonly", audio, "a=sendrecv"),
			[]int{},
		},
		{"keeps a section where neither offer nor answer states a direction: both are sendrecv", sdpOffer(video), sdpOffer(video), sdpOffer(video), []int{}},
		{"reads a section without a direction in the offer as sendrecv", sdpOffer(video, "a=sendonly"), sdpOffer(video), sdpOffer(video, "a=sendonly"), []int{}},
		{
			"gives a section a direction of its own when the answer states none, read as sendrecv",
			sdpOffer(video, "a=rtpmap:96 H264/90000"), sdpOffer(video, "a=recvonly"), sdpOffer(video, "a=rtpmap:96 H264/90000", "a=sendonly"),
			[]int{0},
		},
		{
			"narrows a section that takes the answer's session-level direction",
			sdpOffer("a=sendrecv", video, audio), sdpOffer(video, "a=recvonly", audio, "a=sendrecv"), sdpOffer("a=sendrecv", video, "a=sendonly", audio),
			[]int{0},
		},
		{"keeps a section whose answer-level direction the offer allows", sdpOffer("a=sendonly", video), sdpOffer(video, "a=recvonly"), sdpOffer("a=sendonly", video), []int{}},
		{"keeps the answer's line endings", strings.Join([]string{"v=0", video, "a=sendrecv", ""}, "\n"), sdpOffer(video, "a=recvonly"), strings.Join([]string{"v=0", video, "a=sendonly", ""}, "\n"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, corrected := answerWithinOfferedDirections(c.answer, c.offer)
			if got != c.want {
				t.Errorf("sdp =\n%q\nwant\n%q", got, c.want)
			}
			if c.corrected != nil && !slices.Equal(corrected, c.corrected) {
				t.Errorf("corrected = %v, want %v", corrected, c.corrected)
			}
		})
	}
}
