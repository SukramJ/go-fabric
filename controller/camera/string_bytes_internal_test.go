// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
)

// overBytes is a string of max/2+1 "é" (two UTF-8 bytes, one UTF-16
// unit each): within max counted in UTF-16 code units, over it in bytes.
func overBytes(maxLength int) string { return strings.Repeat("é", maxLength/2+1) }

// TestStringBoundsCountBytes holds R3-err-3651 (decision 554, decision 294
// "UTF-8 bytes everywhere"): every Matter string bound the camera
// boundary checks counts UTF-8 bytes, as the generated decoder the request
// passes afterwards does (spec.StringLength), so a non-ASCII string over
// its byte bound is refused as an argument (ErrInvalidArgument) and never
// as an untyped decode error after the contract passed.
func TestStringBoundsCountBytes(t *testing.T) {
	t.Parallel()

	// The standing guard: every string field with a length ceiling of
	// every provider command and of the ICE candidate struct, by the
	// converter the contract builds for it.
	fields := make([]spec.Field, 0, len(wpdef.ICECandidateStructDef.Fields))
	for _, name := range providerCommandNames {
		fields = append(fields, requestFields(name)...)
	}
	fields = append(fields, wpdef.ICECandidateStructDef.Fields...)
	checked := 0
	for i := range fields {
		f := &fields[i]
		if f.Type.Kind != spec.KindString || !f.Constraint.Max.IsInt() {
			continue
		}
		checked++
		maxLength := int(f.Constraint.Max.Int)
		raw, _ := json.Marshal(overBytes(maxLength))
		if _, err := converterFor(f)(raw, f.Name); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s (max %d): %d bytes refused with %v, want ErrInvalidArgument", f.Name, maxLength, len(overBytes(maxLength)), err)
		}
		if within := strings.Repeat("é", maxLength/2); within != "" {
			raw, _ := json.Marshal(within)
			if _, err := converterFor(f)(raw, f.Name); err != nil {
				t.Errorf("%s (max %d): %d bytes refused: %v", f.Name, maxLength, len(within), err)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no bounded string field found: the guard checks nothing")
	}

	// The wire path end to end: the contract refuses, so request never
	// meets the decoder's byte bound.
	policy := overBytes(iceServerLimits.maxTransportPolicyLength)
	for cmd, members := range map[providerCommandName]map[string]any{
		cmdSolicitOffer: {"streamUsage": 3, "iceTransportPolicy": policy},
		cmdProvideOffer: {"webRtcSessionId": nil, "sdp": "v=0", "streamUsage": 3, "iceTransportPolicy": policy},
	} {
		payload, _ := json.Marshal(members)
		f, err := toProviderCommandFields(cmd, payload, "", "")
		if err == nil {
			_, err = f.request(1)
		}
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s iceTransportPolicy: %v, want ErrInvalidArgument", cmd, err)
		}
	}

	// ICEServerStruct, from the wire and typed.
	servers := map[string]wpdef.ICEServerStruct{
		"urls":       {Urls: []string{overBytes(iceServerLimits.maxURLLength)}},
		"username":   {Urls: []string{"stun:x"}, Username: ptr(overBytes(iceServerLimits.maxUsernameLength))},
		"credential": {Urls: []string{"stun:x"}, Credential: ptr(overBytes(iceServerLimits.maxCredentialLength))},
	}
	for name, s := range servers {
		members := map[string]any{"urls": s.Urls}
		if s.Username != nil {
			members["username"] = *s.Username
		}
		if s.Credential != nil {
			members["credential"] = *s.Credential
		}
		raw, _ := json.Marshal([]map[string]any{members})
		if _, err := toIceServers(raw, "ice_servers"); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("wire ice_servers %s: %v, want ErrInvalidArgument", name, err)
		}
		if err := validateICEServers([]wpdef.ICEServerStruct{s}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("typed ICEServers %s: %v, want ErrInvalidArgument", name, err)
		}
	}
	if _, err := cmdErrStart(t, func(r *StartRequest) {
		r.ICETransportPolicy = overBytes(iceServerLimits.maxTransportPolicyLength)
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("typed ICETransportPolicy: %v, want ErrInvalidArgument", err)
	}
}
