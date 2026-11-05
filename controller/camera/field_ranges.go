// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"

	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
)

// fieldRange is the inclusive range a numeric field takes. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraFieldRanges.ts:11-14
// at 0bd06f2.
type fieldRange struct {
	min, max int64
}

// fieldRangeOf is a numeric field's range from the model: its constraint,
// with the type's width where the constraint states no ceiling. A bound
// that names another field (MinFrameRate is "1 to maxFrameRate") is left
// to the device. A type outside uint8, enum8, uint16 and uint32 panics: the
// model is generated, so this is a build-time defect, as matterjs-server's
// InternalError at module load is. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraFieldRanges.ts:16-22, :46-55 at
// 0bd06f2.
func fieldRangeOf(f *spec.Field) fieldRange {
	var typeMax int64
	switch {
	case f.Type.Kind == spec.KindUint && (f.Type.Bits == 8 || f.Type.Bits == 16 || f.Type.Bits == 32),
		f.Type.Kind == spec.KindEnum && f.Type.Bits == 8:
		typeMax = int64(1)<<f.Type.Bits - 1
	default:
		panic(fmt.Sprintf("camera: the Matter model gives %s the unhandled scalar type %s", f.Name, f.Type.Name))
	}
	r := fieldRange{min: 0, max: typeMax}
	if b := f.Constraint.Min; b.IsInt() {
		r.min = b.Int
	}
	if b := f.Constraint.Max; b.IsInt() {
		r.max = b.Int
	}
	return r
}

// requireField is the field name of fields, panicking when the model
// states none. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraFieldRanges.ts:24-31 at 0bd06f2.
func requireField(fields []spec.Field, owner, name string) *spec.Field {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	panic(fmt.Sprintf("camera: the Matter model states no %s.%s", owner, name))
}

// commandFields are the fields of a request command of cluster, panicking
// when the model states no such command. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraFieldRanges.ts:57-63 at 0bd06f2.
func commandFields(cluster *spec.Cluster, name string) []spec.Field {
	for i := range cluster.Commands {
		if c := &cluster.Commands[i]; c.Name == name && c.Direction == spec.Request {
			return c.Fields
		}
	}
	panic(fmt.Sprintf("camera: the Matter model states no %s.%s", cluster.Name, name))
}

// maxOf is a field's length ceiling, panicking when the model states none.
// Mirrors matterjs-server packages/ws-controller/src/camera/cameraFieldRanges.ts:65-71
// at 0bd06f2.
func maxOf(f *spec.Field) int {
	if !f.Constraint.Max.IsInt() {
		panic("camera: the Matter model states no length ceiling for " + f.Name)
	}
	return int(f.Constraint.Max.Int)
}

// entryMaxOf is a list field's per-entry ceiling, panicking when the model
// states none. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraFieldRanges.ts:73-79 at 0bd06f2.
func entryMaxOf(f *spec.Field) int {
	if f.Constraint.Entry == nil || !f.Constraint.Entry.Max.IsInt() {
		panic("camera: the Matter model states no per-entry ceiling for " + f.Name)
	}
	return int(f.Constraint.Entry.Max.Int)
}

// cameraRanges are the ranges of the fields a camera request states, read
// from the model (§11.2.8.4 VideoStreamAllocate, §11.2.8.1
// AudioStreamAllocate, §11.2.6.7 VideoResolutionStruct). Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraFieldRanges.ts:87-105
// at 0bd06f2.
type cameraRanges struct {
	resolutionWidth, resolutionHeight fieldRange
	minFrameRate, maxFrameRate        fieldRange
	minBitRate, maxBitRate            fieldRange
	channelCount, sampleRate          fieldRange
	audioBitRate                      fieldRange
	videoStreamID, audioStreamID      fieldRange
	snapshotStreamID                  fieldRange
	webRTCSessionID                   fieldRange
}

var cameraFieldRanges = func() cameraRanges {
	avsm := csdef.Definition
	res := csdef.VideoResolutionStructDef.Fields
	field := func(command, name string) fieldRange {
		return fieldRangeOf(requireField(commandFields(avsm, command), command, name))
	}
	return cameraRanges{
		resolutionWidth:  fieldRangeOf(requireField(res, "VideoResolutionStruct", "Width")),
		resolutionHeight: fieldRangeOf(requireField(res, "VideoResolutionStruct", "Height")),
		minFrameRate:     field("VideoStreamAllocate", "MinFrameRate"),
		maxFrameRate:     field("VideoStreamAllocate", "MaxFrameRate"),
		minBitRate:       field("VideoStreamAllocate", "MinBitRate"),
		maxBitRate:       field("VideoStreamAllocate", "MaxBitRate"),
		channelCount:     field("AudioStreamAllocate", "ChannelCount"),
		sampleRate:       field("AudioStreamAllocate", "SampleRate"),
		audioBitRate:     field("AudioStreamAllocate", "BitRate"),
		videoStreamID:    field("VideoStreamDeallocate", "VideoStreamId"),
		audioStreamID:    field("AudioStreamDeallocate", "AudioStreamId"),
		snapshotStreamID: field("SnapshotStreamDeallocate", "SnapshotStreamId"),
		webRTCSessionID:  fieldRangeOf(requireField(commandFields(wpdef.Definition, "EndSession"), "EndSession", "WebRtcSessionId")),
	}
}()

// iceLimits are the bounds of an ICE server list (§11.4.5.3
// ICEServerStruct, §11.5.6.3 ProvideOffer); SolicitOffer states the same
// list ceiling, so one bound covers both commands. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraFieldRanges.ts:107-125 at 0bd06f2.
type iceLimits struct {
	maxServers, maxTransportPolicyLength int
	maxUrls, maxURLLength                int
	maxUsernameLength                    int
	maxCredentialLength                  int
	caid                                 fieldRange
}

var iceServerLimits = func() iceLimits {
	offer := commandFields(wpdef.Definition, "ProvideOffer")
	server := wpdef.ICEServerStructDef.Fields
	urls := requireField(server, "ICEServerStruct", "Urls")
	return iceLimits{
		maxServers:               maxOf(requireField(offer, "ProvideOffer", "IceServers")),
		maxTransportPolicyLength: maxOf(requireField(offer, "ProvideOffer", "IceTransportPolicy")),
		maxUrls:                  maxOf(urls),
		maxURLLength:             entryMaxOf(urls),
		maxUsernameLength:        maxOf(requireField(server, "ICEServerStruct", "Username")),
		maxCredentialLength:      maxOf(requireField(server, "ICEServerStruct", "Credential")),
		caid:                     fieldRangeOf(requireField(server, "ICEServerStruct", "Caid")),
	}
}()

// argumentError refuses a caller's argument: matterjs-server's
// ServerError.invalidArguments, error 8 on its wire. Its text is the
// refusal as matterjs-server words it.
type argumentError struct {
	msg string
}

func (e *argumentError) Error() string { return e.msg }

// Is makes every refusal of an argument [ErrInvalidArgument].
func (e *argumentError) Is(target error) bool { return target == ErrInvalidArgument }

// ErrInvalidArgument is matterjs-server's error 8: a caller's argument the
// controller refuses before anything reaches a camera. Every such refusal
// matches it with errors.Is; its text names the argument.
var ErrInvalidArgument = errors.New("camera: invalid argument")

// invalidArguments is an argumentError with a formatted text.
func invalidArguments(format string, args ...any) error {
	return &argumentError{msg: fmt.Sprintf(format, args...)}
}

// rangeText words a range for a refusal. Mirrors matterjs-server
// packages/ws-controller/src/camera/wireArgumentChecks.ts:29-31 at 0bd06f2.
func rangeText(r fieldRange) string {
	return "an integer between " + strconv.FormatInt(r.min, 10) + " and " + strconv.FormatInt(r.max, 10)
}

// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER.
const maxSafeInteger = 1<<53 - 1

// toRequiredNumber is a JSON value held to r: a safe integer, as
// matterjs-server's isInRange takes it, within the range. Mirrors
// matterjs-server packages/ws-controller/src/camera/wireArgumentChecks.ts:25-36
// at 0bd06f2.
func toRequiredNumber(value json.RawMessage, field string, r fieldRange) (int64, error) {
	var n float64
	if jsonKind(value) != '0' || json.Unmarshal(value, &n) != nil ||
		n != math.Trunc(n) || math.Abs(n) > maxSafeInteger || n < float64(r.min) || n > float64(r.max) {
		return 0, invalidArguments("%s must be %s", field, rangeText(r))
	}
	return int64(n), nil
}

// jsLength is a string's length as JavaScript counts it: UTF-16 code
// units.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// toBoundedString is a JSON string of 1 to maxLength characters. Mirrors
// matterjs-server packages/ws-controller/src/camera/wireArgumentChecks.ts:38-43
// at 0bd06f2.
func toBoundedString(value json.RawMessage, field string, maxLength int) (string, error) {
	var s string
	if jsonKind(value) != '"' || json.Unmarshal(value, &s) != nil || s == "" || jsLength(s) > maxLength {
		return "", invalidArguments("%s must be a string of 1 to %d characters", field, maxLength)
	}
	return s, nil
}

// jsonKind classifies a JSON value by its first byte: '{' object, '['
// array, '"' string, 't' or 'f' boolean, 'n' null, '0' number, and 0 for
// no value.
func jsonKind(value json.RawMessage) byte {
	for _, c := range value {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		case '{', '[', '"', 'n':
			return c
		case 't', 'f':
			return 't'
		default:
			return '0'
		}
	}
	return 0
}

// jsonMember is one key of a JSON object and its value.
type jsonMember struct {
	key   string
	value json.RawMessage
}

// jsonObject reads a JSON object's members in their order, ok false for
// anything that is not an object (isRecord, matterjs-server
// packages/ws-controller/src/camera/wireArgumentChecks.ts:11-13 at 0bd06f2).
// A key stated twice keeps its first position and its last value, as
// JSON.parse leaves it.
func jsonObject(value json.RawMessage) ([]jsonMember, bool) {
	if jsonKind(value) != '{' {
		return nil, false
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(value, &raw) != nil {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(value))
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	members := make([]jsonMember, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, _ := tok.(string)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return nil, false
		}
		if !seen[key] {
			seen[key] = true
			members = append(members, jsonMember{key: key, value: raw[key]})
		}
	}
	return members, true
}

// jsonArray reads a JSON array's entries, ok false for anything else.
func jsonArray(value json.RawMessage) ([]json.RawMessage, bool) {
	var entries []json.RawMessage
	if jsonKind(value) != '[' || json.Unmarshal(value, &entries) != nil {
		return nil, false
	}
	return entries, true
}
