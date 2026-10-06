// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestGeneratedResponsePayload sends a generated response payload through
// the invoke path: the response names its own command id and encodes as
// matter.js does (application-wire-fixtures.json, mode_response_stuck).
func TestGeneratedResponsePayload(t *testing.T) {
	t.Parallel()
	resp := rvcrunmode.ChangeToModeResponse{Status: rvcrunmode.ModeChangeStatusStuck, StatusText: "Stuck under the sofa"}
	ent := im.InvokeResponseEntry{
		Path:     im.ConcreteCommandPath{Cluster: rvcrunmode.ClusterID, Command: rvcrunmode.CmdChangeToMode},
		Response: resp,
	}
	rewriteInvokeResponseCommand(&ent)
	if ent.Path.Command != rvcrunmode.CmdChangeToModeResponse {
		t.Errorf("response command 0x%02X", ent.Path.Command)
	}
	if got := encodeCommandResponse(t, resp); got != "152400412c0114537475636b20756e6465722074686520736f666118" {
		t.Errorf("encoded %s", got)
	}
}

// TestGeneratedRequestDecoding decodes a request of a cluster the bridge
// has no hand-written decoder for through the generated definition's
// registry entry, with the status matter.js answers a rejected payload
// with. RvcRunMode's ChangeToMode has a hand-written decoder, so the test
// registers the definition under a cluster id no switch case claims.
func TestGeneratedRequestDecoding(t *testing.T) {
	t.Parallel()
	alias := *rvcrunmode.Definition
	alias.ID = 0xFFF5_0054
	spec.Register(&alias)
	got := decodeCommandFields(t, alias.ID, rvcrunmode.CmdChangeToMode, "1524000718")
	if got != (rvcrunmode.ChangeToModeRequest{NewMode: 7}) {
		t.Errorf("decoded %#v", got)
	}
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), 300)
	_ = enc.EndContainer()
	b, _ := enc.Bytes()
	dec := tlv.NewDecoder(b)
	open, _ := dec.Next()
	if _, err := commandFieldsReader(im.ConcreteCommandPath{Cluster: alias.ID, Command: rvcrunmode.CmdChangeToMode}, dec, open); !isStatus(err, im.StatusConstraintError) {
		t.Errorf("NewMode 300: %v", err)
	}
}

func isStatus(err error, status im.StatusCode) bool {
	var sce im.StatusCodeError
	return errors.As(err, &sce) && sce.MatterStatusCode() == status
}
