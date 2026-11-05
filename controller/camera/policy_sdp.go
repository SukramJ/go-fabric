// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
)

// ceiling is an upper bound an offer may state: a JavaScript number in
// matterjs-server, so a float64 here (H.265's max-fps divides by 100), and
// set only when the offer states it. 0 is a stated ceiling.
type ceiling struct {
	value float64
	set   bool
}

// stated is the ceiling v.
func stated(v float64) ceiling { return ceiling{value: v, set: true} }

// tighter keeps the smaller of two ceilings, either of which may be unset.
// Mirrors matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:194-198
// at 0bd06f2.
func (c ceiling) tighter(o ceiling) ceiling {
	switch {
	case !c.set:
		return o
	case !o.set:
		return c
	default:
		return stated(math.Min(c.value, o.value))
	}
}

// videoCodecLimits are an offer's decode ceilings for one codec. Several
// records for the same codec fold to the tighter value; within one record
// an explicit capability parameter replaces the level's value (RFC 6184
// §8.1 and RFC 7798 §7.1 define it as at or above the level). Mirrors
// matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:20-31
// at 0bd06f2.
type videoCodecLimits struct {
	maxPixels          ceiling
	maxPixelsPerSecond ceiling
	// maxFrameRate is frames per second, from max-fr (RFC 7741 §6.1) or
	// H.265's max-fps (per 100 s, RFC 7798 §7.1).
	maxFrameRate ceiling
	maxBitRate   ceiling
}

// selectedVideoCodecLimits carries the codec that stated the limits, so
// they cannot be applied to another codec. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:33-36 at 0bd06f2.
type selectedVideoCodecLimits struct {
	codec csdef.VideoCodecEnum
	videoCodecLimits
}

// dispositionState is what an offer says about one kind of media.
type dispositionState uint8

// The four dispositions: no section; refused (port 0, RFC 3264 §6); a live
// section the peer will not receive on; receiving (no direction reads as
// receiving, RFC 4566 §6).
const (
	mediaAbsent dispositionState = iota
	mediaRefused
	mediaNotReceiving
	mediaReceiving
)

// mediaDisposition is the offer's statement on one kind of media. codecs
// is nil, never empty, when the receiving sections have no a=rtpmap lines
// (static payload types only); direction is set for mediaNotReceiving
// only. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:38-48 at 0bd06f2.
type mediaDisposition struct {
	state     dispositionState
	direction string
	codecs    []string
}

// sendingAudioSection is a live audio section whose direction lets the
// peer send, i.e. asks the camera for talkback. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:50-56 at 0bd06f2.
type sendingAudioSection struct {
	// index is the position among all of the offer's m-sections, from 0.
	index int
	// direction is the direction in effect: stated by the section, by the
	// session, or the sendrecv default.
	direction string
}

// sdpVideoConstraints is what an SDP offer constrains. Mirrors
// matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:58-70
// at 0bd06f2.
type sdpVideoConstraints struct {
	video mediaDisposition
	audio mediaDisposition
	// sendingAudioSections is empty when the offer does not ask for
	// talkback.
	sendingAudioSections []sendingAudioSection
	// limitsByCodec are the per-codec fmtp limits, keyed by upper-cased
	// codec name; a codec absent here stated none.
	limitsByCodec map[string]videoCodecLimits
	// unreadableCeilingCodecs must not be selected; kept apart from the
	// codec list, because an empty list would read as "no codec stated".
	unreadableCeilingCodecs map[string]bool
	// notices are what matterjs-server logs as notices while reading the
	// offer, for the caller to log.
	notices []string
}

// mediaRefusal is the statement of an offer that forbids kind ("video" or
// "audio"), ok false when nothing forbids it. No offer (the controller
// solicits one) refuses nothing; an offer with no section of the kind
// refuses it, because the answer can only carry the offer's m-lines
// (RFC 3264 §6). Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:72-90 at 0bd06f2.
func mediaRefusal(offer *sdpVideoConstraints, kind string) (mediaDisposition, bool) {
	if offer == nil {
		return mediaDisposition{}, false
	}
	d := offer.audio
	if kind == "video" {
		d = offer.video
	}
	if d.state == mediaReceiving {
		return mediaDisposition{}, false
	}
	return d, true
}

// receivableCodecs is the codecs a receiving disposition names; nil also
// for a section that is not receiving, so check mediaRefusal first.
// Mirrors matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:92-95
// at 0bd06f2.
func receivableCodecs(d mediaDisposition) []string {
	if d.state != mediaReceiving {
		return nil
	}
	return d.codecs
}

// fmtpValue reads one parameter of an fmtp config, case-insensitively:
// media type parameter names are (RFC 8866 §6.15, RFC 6838 §4.3). Mirrors
// matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:97-101
// at 0bd06f2.
func fmtpValue(params, key string) (string, bool) {
	pattern, ok := fmtpPatterns[key]
	if !ok {
		pattern = fmtpPattern(key)
	}
	m := pattern.FindStringSubmatch(params)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// fmtpPattern matches the parameter key of an fmtp config.
func fmtpPattern(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|;)\s*` + regexp.QuoteMeta(key) + `=([^;\s]+)`)
}

// fmtpPatterns are the patterns of the parameters the policies read,
// compiled once.
var fmtpPatterns = func() map[string]*regexp.Regexp {
	keys := []string{"profile-level-id", "level-id", "max-fs", "max-mbps", "max-fr", "max-lps", "max-lsr", "max-fps", maxBitRateParameter}
	patterns := make(map[string]*regexp.Regexp, len(keys))
	for _, k := range keys {
		patterns[k] = fmtpPattern(k)
	}
	return patterns
}()

// fmtpReading is one fmtp number: absent, read, or unreadable, which must
// never be treated as absent. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:103-109 at 0bd06f2.
type fmtpReading struct {
	present    bool
	unreadable bool
	value      float64
	raw        string
}

var digitsPattern = regexp.MustCompile(`^\d+$`)

// fmtpNumber reads one numeric fmtp parameter. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:111-120 at 0bd06f2.
func fmtpNumber(params, key string) fmtpReading {
	value, ok := fmtpValue(params, key)
	if !ok {
		return fmtpReading{}
	}
	if !digitsPattern.MatchString(value) {
		return fmtpReading{present: true, unreadable: true, raw: value}
	}
	n, _ := strconv.ParseFloat(value, 64) // digits only; an overlong one reads as +Inf, as Number does
	return fmtpReading{present: true, value: n, raw: value}
}

// statedNumber is a read reading's value as a ceiling.
func (r fmtpReading) statedNumber() ceiling {
	if !r.present || r.unreadable {
		return ceiling{}
	}
	return stated(r.value)
}

// maxBitRateParameter is the bit-rate parameter of both RFC 6184 §8.1 and
// RFC 7798 §7.1. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:131 at 0bd06f2.
const maxBitRateParameter = "max-br"

// codecFmtpParameters are the fmtp parameter names of one codec
// (RFC 6184 §8.1, RFC 7798 §7.1): pixelsPerUnit converts frame-size and
// sample-rate units to pixels, frameRatePerUnit the frame-rate parameter
// to frames per second. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:133-144 at 0bd06f2.
type codecFmtpParameters struct {
	levelName        string
	readLevel        func(string) (videoCodecLimits, bool)
	maxFrameSize     string
	maxSampleRate    string
	maxFrameRate     string
	pixelsPerUnit    float64
	frameRatePerUnit float64
}

// defaultFmtpParameters are H.264's names and units, without a level, for
// a codec with no entry of its own. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:146-153 at 0bd06f2.
var defaultFmtpParameters = codecFmtpParameters{
	maxFrameSize:     "max-fs",
	maxSampleRate:    "max-mbps",
	maxFrameRate:     "max-fr",
	pixelsPerUnit:    pixelsPerMacroblock,
	frameRatePerUnit: 1,
}

// frameRatePeriodSeconds: H.265's max-fps counts frames over this period
// (RFC 7798 §7.1). Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:17-18 at 0bd06f2.
const frameRatePeriodSeconds = 100

// fmtpParametersOf are the per-codec fmtp names. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:155-174 at 0bd06f2.
func fmtpParametersOf(codec string) codecFmtpParameters {
	switch codec {
	case "H264":
		p := defaultFmtpParameters
		p.levelName, p.readLevel = "profile-level-id", h264ProfileLevelIDLimits
		return p
	case "H265":
		return codecFmtpParameters{
			levelName:        "level-id",
			readLevel:        h265LevelIDLimits,
			maxFrameSize:     "max-lps",
			maxSampleRate:    "max-lsr",
			maxFrameRate:     "max-fps",
			pixelsPerUnit:    1,
			frameRatePerUnit: 1.0 / frameRatePeriodSeconds,
		}
	default:
		return defaultFmtpParameters
	}
}

// levelStatement is the level a record states: none (also for codecs with
// no level table here), read, or unreadable. The RFC default for an absent
// level (H.264: 42000A, level 1) is not applied: it would bound 1080p below
// one frame per second. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:176-192 at 0bd06f2.
type levelStatement struct {
	read       bool
	unreadable bool
	limits     videoCodecLimits
	value      string
}

func statedLevel(p codecFmtpParameters, config string) levelStatement {
	if p.readLevel == nil {
		return levelStatement{}
	}
	value, ok := fmtpValue(config, p.levelName)
	if !ok {
		return levelStatement{}
	}
	limits, ok := p.readLevel(value)
	if !ok {
		return levelStatement{unreadable: true, value: value}
	}
	return levelStatement{read: true, limits: limits}
}

// tighten keeps the value both payload types can decode. Mirrors
// matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:200-213
// at 0bd06f2.
func tighten(into map[string]videoCodecLimits, codec string, limits videoCodecLimits) {
	known := into[codec]
	into[codec] = videoCodecLimits{
		maxPixels:          known.maxPixels.tighter(limits.maxPixels),
		maxPixelsPerSecond: known.maxPixelsPerSecond.tighter(limits.maxPixelsPerSecond),
		maxFrameRate:       known.maxFrameRate.tighter(limits.maxFrameRate),
		maxBitRate:         known.maxBitRate.tighter(limits.maxBitRate),
	}
}

// mediaSections gathers the sections of one kind. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:215-221 at 0bd06f2.
type mediaSections struct {
	receiving bool
	// notReceiving is the direction of the first live section the peer will
	// not receive on; reported only, it decides nothing.
	notReceiving string
	refused      bool
	codecs       []string
}

// disposition: one receiving section makes the kind receiving, whatever
// the other sections say. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:223-232 at 0bd06f2.
func (s *mediaSections) disposition() mediaDisposition {
	switch {
	case s.receiving:
		if len(s.codecs) == 0 {
			return mediaDisposition{state: mediaReceiving}
		}
		return mediaDisposition{state: mediaReceiving, codecs: s.codecs}
	case s.notReceiving != "":
		return mediaDisposition{state: mediaNotReceiving, direction: s.notReceiving}
	case s.refused:
		return mediaDisposition{state: mediaRefused}
	default:
		return mediaDisposition{state: mediaAbsent}
	}
}

// decodableVideoCodecs splits the offer's receivable video codecs into
// those whose ceilings it can read and those it cannot; ok false when the
// offer names no video codec to narrow by. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:234-243 at 0bd06f2.
func decodableVideoCodecs(sdp *sdpVideoConstraints) (decodable, unreadable []string, ok bool) {
	offered := receivableCodecs(sdp.video)
	if offered == nil {
		return nil, nil, false
	}
	decodable, unreadable = []string{}, []string{}
	for _, name := range offered {
		if sdp.unreadableCeilingCodecs[name] {
			unreadable = append(unreadable, name)
		} else {
			decodable = append(decodable, name)
		}
	}
	return decodable, unreadable, true
}

// videoCodecLimitsOf is the offer's limits on codec, read only from that
// codec's own a=fmtp records; sdp nil states none. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:245-249 at 0bd06f2.
func videoCodecLimitsOf(sdp *sdpVideoConstraints, codec csdef.VideoCodecEnum) selectedVideoCodecLimits {
	out := selectedVideoCodecLimits{codec: codec}
	if sdp != nil {
		out.videoCodecLimits = sdp.limitsByCodec[videoCodecName(codec)]
	}
	return out
}

// parseSdpVideoConstraints reads what an SDP offer constrains. Limits are
// upper bounds on what the caller can decode, not preferences. Mirrors
// matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:251-359
// at 0bd06f2; matterjs-server's catch of an unparseable offer has no Go
// counterpart, since parseSDP reads every input and an offer with no media
// line reads as absent for both kinds.
func parseSdpVideoConstraints(sdp string) sdpVideoConstraints {
	out := sdpVideoConstraints{
		limitsByCodec:           map[string]videoCodecLimits{},
		unreadableCeilingCodecs: map[string]bool{},
	}
	var video, audio mediaSections
	parsed := parseSDP(sdp)
	for index, media := range parsed.media {
		var sections *mediaSections
		switch media.mediaType {
		case "video":
			sections = &video
		case "audio":
			sections = &audio
		default:
			continue
		}
		// Port 0 with a=bundle-only is a live section carried on the bundle
		// transport (RFC 8843 §7.1.2).
		if media.port == "0" && !media.bundleOnly {
			sections.refused = true
			continue
		}
		// Session-level direction applies where a section states none
		// (RFC 4566 §5); the default is sendrecv (§6).
		direction := effectiveDirection(media.direction, parsed.direction)
		if media.mediaType == "audio" && (direction == "sendonly" || direction == "sendrecv") {
			out.sendingAudioSections = append(out.sendingAudioSections, sendingAudioSection{index: index, direction: direction})
		}
		if direction == "sendonly" || direction == "inactive" {
			if sections.notReceiving == "" {
				sections.notReceiving = direction
			}
			continue
		}
		sections.receiving = true
		codecsByPayload := map[string]string{}
		var order []string
		for _, entry := range media.rtp {
			if _, seen := codecsByPayload[entry.payload]; !seen {
				order = append(order, entry.payload)
			}
			codecsByPayload[entry.payload] = strings.ToUpper(entry.codec)
		}
		for _, payload := range order {
			if codec := codecsByPayload[payload]; !slices.Contains(sections.codecs, codec) {
				sections.codecs = append(sections.codecs, codec)
			}
		}
		if media.mediaType == "video" {
			out.readVideoFmtp(media.fmtp, codecsByPayload)
		}
	}
	out.video = video.disposition()
	out.audio = audio.disposition()
	return out
}

// readVideoFmtp folds the a=fmtp records of one receiving video section
// into the per-codec limits. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:299-348 at 0bd06f2.
func (out *sdpVideoConstraints) readVideoFmtp(records []sdpFmtp, codecsByPayload map[string]string) {
	for _, entry := range records {
		codec, ok := codecsByPayload[entry.payload]
		// No a=rtpmap for this payload type; every selectable codec is
		// dynamically mapped.
		if !ok {
			continue
		}
		p := fmtpParametersOf(codec)
		level := statedLevel(p, entry.config)
		if level.unreadable {
			out.unreadableCeilingCodecs[codec] = true
			out.notices = append(out.notices, "Offer states "+codec+" "+p.levelName+"="+level.value+
				", which is no level this server can bound a stream by; "+codec+" is not selectable for it")
			continue
		}
		names := []string{p.maxFrameSize, p.maxSampleRate, p.maxFrameRate, maxBitRateParameter}
		readings := make([]fmtpReading, len(names))
		for i, name := range names {
			readings[i] = fmtpNumber(entry.config, name)
		}
		if i := slices.IndexFunc(readings, func(r fmtpReading) bool { return r.unreadable }); i >= 0 {
			// No fallback to the level: an explicit parameter overrides it.
			out.unreadableCeilingCodecs[codec] = true
			out.notices = append(out.notices, "Offer states "+codec+" "+names[i]+"="+readings[i].raw+
				", which is no decode ceiling this server can read; "+codec+" is not selectable for it")
			continue
		}
		limits := videoCodecLimits{}
		if level.read {
			limits = level.limits
			limits.maxFrameRate = ceiling{}
		}
		if r := readings[0].statedNumber(); r.set {
			limits.maxPixels = stated(r.value * p.pixelsPerUnit)
		}
		if r := readings[1].statedNumber(); r.set {
			limits.maxPixelsPerSecond = stated(r.value * p.pixelsPerUnit)
		}
		if r := readings[2].statedNumber(); r.set {
			limits.maxFrameRate = stated(r.value * p.frameRatePerUnit)
		}
		if r := readings[3].statedNumber(); r.set {
			limits.maxBitRate = stated(r.value * bitsPerKilobit)
		}
		tighten(out.limitsByCodec, codec, limits)
	}
}

// effectiveDirection is a section's direction: its own line, else the
// session-level line, else sendrecv (RFC 4566 §5, §6).
func effectiveDirection(section, session string) string {
	switch {
	case section != "":
		return section
	case session != "":
		return session
	default:
		return "sendrecv"
	}
}

// directionLine matches the lines sdp-transform reads as a direction: the
// same prefix match as its grammar, so a rewrite hits exactly the lines the
// parser read. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:363-364 at 0bd06f2.
var directionLine = regexp.MustCompile(`^a=(?:sendrecv|sendonly|recvonly|inactive)`)

// talkbackRefused is the non-sending direction for each sending one.
// Mirrors matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:365-368
// at 0bd06f2.
var talkbackRefused = map[string]string{"sendrecv": "recvonly", "sendonly": "inactive"}

// withoutTalkback is the offer with each of sections set to a direction
// that does not send: sendrecv becomes recvonly, sendonly becomes inactive.
// Every other line is kept. sections must come from
// parseSdpVideoConstraints on the same sdp, which counts m-lines the same
// way. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:370-382 at 0bd06f2.
func withoutTalkback(sdp string, sections []sendingAudioSection) string {
	if len(sections) == 0 {
		return sdp
	}
	directions := make(map[int]string, len(sections))
	for _, s := range sections {
		directions[s.index] = talkbackRefused[s.direction]
	}
	return withSectionDirections(sdp, directions)
}

// sdpLine is one line of an SDP text and the line break after it.
type sdpLine struct {
	text, eol string
}

// splitSDPLines splits sdp at every \r\n, \r or \n, keeping each break:
// the parts of sdp.split(/(\r\n|\r|\n)/).
func splitSDPLines(sdp string) []sdpLine {
	var lines []sdpLine
	start := 0
	for i := 0; i < len(sdp); i++ {
		switch sdp[i] {
		case '\r':
			eol := "\r"
			if i+1 < len(sdp) && sdp[i+1] == '\n' {
				eol = "\r\n"
			}
			lines = append(lines, sdpLine{text: sdp[start:i], eol: eol})
			i += len(eol) - 1
			start = i + 1
		case '\n':
			lines = append(lines, sdpLine{text: sdp[start:i], eol: "\n"})
			start = i + 1
		}
	}
	return append(lines, sdpLine{text: sdp[start:]})
}

// withSectionDirections is sdp with each section in directions (by
// position, from 0) set to its direction. A section that states no
// direction gets a line, which overrides a session-level direction for that
// section only (RFC 4566 §5). Every other line is kept. Mirrors
// matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:384-422
// at 0bd06f2.
func withSectionDirections(sdp string, directions map[int]string) string {
	lines := splitSDPLines(sdp)
	var sectionStarts []int
	for i, l := range lines {
		if strings.HasPrefix(l.text, "m=") {
			sectionStarts = append(sectionStarts, i)
		}
	}
	indexes := make([]int, 0, len(directions))
	for index := range directions {
		indexes = append(indexes, index)
	}
	// Back to front, so an inserted line does not move a section still to
	// be rewritten.
	slices.Sort(indexes)
	slices.Reverse(indexes)
	for _, index := range indexes {
		if index < 0 || index >= len(sectionStarts) {
			continue
		}
		start := sectionStarts[index]
		end := len(lines)
		if index+1 < len(sectionStarts) {
			end = sectionStarts[index+1]
		}
		target := "a=" + directions[index]
		statedHere := false
		last := start
		for i := start; i < end; i++ {
			if lines[i].text != "" {
				last = i
			}
			if directionLine.MatchString(lines[i].text) {
				lines[i].text = target
				statedHere = true
			}
		}
		if statedHere {
			continue
		}
		afterEOL := lines[last].eol
		eol := afterEOL
		if eol == "" {
			eol = lines[start].eol
			if eol == "" {
				eol = "\r\n"
			}
		}
		lines = slices.Insert(lines, last+1, sdpLine{text: target, eol: afterEOL})
		lines[last].eol = eol
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.text)
		b.WriteString(l.eol)
	}
	return b.String()
}

// sends reports whether a direction sends. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:424-426 at 0bd06f2.
func sends(direction string) bool { return direction == "sendrecv" || direction == "sendonly" }

// receives reports whether a direction receives. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:428-430 at 0bd06f2.
func receives(direction string) bool { return direction == "sendrecv" || direction == "recvonly" }

// directionOf is the direction that sends and receives as stated. Mirrors
// matterjs-server packages/ws-controller/src/camera/sdpConstraints.ts:432-435
// at 0bd06f2.
func directionOf(send, receive bool) string {
	switch {
	case send && receive:
		return "sendrecv"
	case send:
		return "sendonly"
	case receive:
		return "recvonly"
	default:
		return "inactive"
	}
}

// answerWithinOfferedDirections is the answer with each section's direction
// narrowed to what the offer allows (RFC 3264 §6.1): the answerer sends
// only where the offer receives, and receives only where the offer sends.
// Some cameras answer a=sendrecv to a recvonly section, which the client's
// WebRTC stack rejects ([answerDirectionsNarrowed]). Sections pair by
// position (RFC 3264 §6); a section's direction is its own line, else the
// session-level line, else sendrecv, in offer and answer alike; a narrowed
// section gets a line of its own, and a section with no offered
// counterpart is kept. corrected lists the positions of the rewritten
// sections, from 0. Mirrors matterjs-server
// packages/ws-controller/src/camera/sdpConstraints.ts:437-467 at 0bd06f2.
func answerWithinOfferedDirections(answer, offer string) (sdp string, corrected []int) {
	offered := parseSDP(offer)
	answered := parseSDP(answer)
	narrowed := map[int]string{}
	corrected = []int{}
	for section, media := range answered.media {
		if section >= len(offered.media) {
			continue
		}
		offeredDirection := effectiveDirection(offered.media[section].direction, offered.direction)
		statedDirection := effectiveDirection(media.direction, answered.direction)
		allowed := directionOf(
			sends(statedDirection) && receives(offeredDirection),
			receives(statedDirection) && sends(offeredDirection),
		)
		if allowed != statedDirection {
			narrowed[section] = allowed
			corrected = append(corrected, section)
		}
	}
	if len(narrowed) == 0 {
		return answer, corrected
	}
	return withSectionDirections(answer, narrowed), corrected
}

// sdpRtp is one a=rtpmap record.
type sdpRtp struct {
	payload, codec string
}

// sdpFmtp is one a=fmtp record.
type sdpFmtp struct {
	payload, config string
}

// sdpMedia is one m-section as sdp-transform reads it.
type sdpMedia struct {
	// mediaType is "" when the m-line did not match the grammar.
	mediaType  string
	port       string
	direction  string
	bundleOnly bool
	rtp        []sdpRtp
	fmtp       []sdpFmtp
}

// sdpSession is an SDP text as sdp-transform reads it, the parts the
// camera policies use.
type sdpSession struct {
	direction string
	media     []sdpMedia
}

// The sdp-transform 3.0.0 grammar lines the policies read
// (lib/grammar.js), anchored as it anchors them.
var (
	sdpValidLine  = regexp.MustCompile(`^([a-z])=`)
	sdpMediaLine  = regexp.MustCompile(`^(\w*) (\d*) ([\w/]*)(?: (.*))?`)
	sdpRtpmapLine = regexp.MustCompile(`^rtpmap:(\d*) ([\w\-.]*)(?:\s*/(\d*)(?:\s*/(\S*))?)?`)
	// sdpFmtpLine is sdp-transform's ^fmtp:(\d*) ([\S| ]*), whose "|" the
	// class \S already holds.
	sdpFmtpLine   = regexp.MustCompile(`^fmtp:(\d*) ([\S ]*)`)
	sdpDirection  = regexp.MustCompile(`^(sendrecv|recvonly|sendonly|inactive)`)
	sdpBundleOnly = regexp.MustCompile(`^bundle-only`)
)

// parseSDP reads sdp the way sdp-transform's parse does
// (lib/parser.js of sdp-transform 3.0.0, the parser matterjs-server's
// sdpConstraints uses), for the attributes the camera policies read: every
// line of the form <letter>=<content>, an m-line opening a section, and
// a=rtpmap, a=fmtp, a direction and a=bundle-only going to the section (or,
// before the first m-line, to the session). Payload types are kept as
// written; sdp-transform turns a canonical decimal into a number, which
// compares the same.
func parseSDP(sdp string) sdpSession {
	var session sdpSession
	var current *sdpMedia
	for _, l := range splitSDPLines(sdp) {
		line := l.text
		if !sdpValidLine.MatchString(line) {
			continue
		}
		kind, content := line[0], line[2:]
		switch kind {
		case 'm':
			session.media = append(session.media, sdpMedia{})
			current = &session.media[len(session.media)-1]
			if m := sdpMediaLine.FindStringSubmatch(content); m != nil {
				current.mediaType, current.port = m[1], m[2]
			}
		case 'a':
			parseSDPAttribute(content, &session, current)
		}
	}
	return session
}

// parseSDPAttribute applies one a-line to the section, or to the session
// before the first m-line.
func parseSDPAttribute(content string, session *sdpSession, media *sdpMedia) {
	switch {
	case sdpRtpmapLine.MatchString(content):
		if media != nil {
			m := sdpRtpmapLine.FindStringSubmatch(content)
			media.rtp = append(media.rtp, sdpRtp{payload: m[1], codec: m[2]})
		}
	case sdpFmtpLine.MatchString(content):
		if media != nil {
			m := sdpFmtpLine.FindStringSubmatch(content)
			media.fmtp = append(media.fmtp, sdpFmtp{payload: m[1], config: m[2]})
		}
	case sdpDirection.MatchString(content):
		d := sdpDirection.FindStringSubmatch(content)[1]
		if media != nil {
			media.direction = d
		} else {
			session.direction = d
		}
	case sdpBundleOnly.MatchString(content):
		if media != nil {
			media.bundleOnly = true
		}
	}
}

// videoCodecNames are the wire names of the video codecs, written out
// because the enum member names (Hevc, …) differ from the SDP spelling
// (H265, …). Mirrors matterjs-server
// packages/ws-controller/src/camera/wireNames.ts:11-21 at 0bd06f2.
var videoCodecNames = map[csdef.VideoCodecEnum]string{
	csdef.VideoCodecH264: "H264",
	csdef.VideoCodecHevc: "H265",
	csdef.VideoCodecVvc:  "H266",
	csdef.VideoCodecAv1:  "AV1",
}

// audioCodecNames are the wire names of the audio codecs. Mirrors
// matterjs-server packages/ws-controller/src/camera/wireNames.ts:23-26 at
// 0bd06f2.
var audioCodecNames = map[csdef.AudioCodecEnum]string{
	csdef.AudioCodecOpus:  "OPUS",
	csdef.AudioCodecAacLc: "AAC",
}

// videoCodecName is a video codec's wire name, its number for one without a
// name. Mirrors matterjs-server packages/ws-controller/src/camera/wireNames.ts:44-58
// at 0bd06f2.
func videoCodecName(codec csdef.VideoCodecEnum) string {
	if name, ok := videoCodecNames[codec]; ok {
		return name
	}
	return strconv.Itoa(int(codec))
}

// audioCodecName is an audio codec's wire name, its number for one without
// a name. Mirrors matterjs-server packages/ws-controller/src/camera/wireNames.ts:60-62
// at 0bd06f2.
func audioCodecName(codec csdef.AudioCodecEnum) string {
	if name, ok := audioCodecNames[codec]; ok {
		return name
	}
	return strconv.Itoa(int(codec))
}
