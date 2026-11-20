// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// readHeld runs commandFieldsReader over the fields struct body writes,
// with one more element after it, and returns the reader's answer and the
// element the outer decoder reads next.
func readHeld(t *testing.T, cluster, cmd uint32, body func(*tlv.Encoder)) (any, tlv.Element, error) {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	body(enc)
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(9), 77) // the element after the fields
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(raw)
	open, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	fields, rerr := commandFieldsReader(cmdPath(cluster, cmd), dec, open)
	_, fields = spec.SplitHandForm(fields) // the server's form
	if rerr != nil {
		var c im.FieldsContainerConsumed
		if !errors.As(rerr, &c) || !c.FieldsContainerConsumed() {
			t.Fatalf("reject %v does not say the container is consumed", rerr)
		}
	}
	next, err := dec.Next()
	if err != nil {
		t.Fatalf("after the fields: %v", err)
	}
	return fields, next, rerr
}

// TestHandDecodedRequestsAreHeldByTheirDefinition: a request the bridge
// decodes by hand is held to its definition's request check first, as
// matter.js validates every request against its schema before the server
// runs (packages/protocol/src/action/server/CommandInvokeResponse.ts:474-487
// at db021da0) — with the server's relaxations the definition carries
// (script/clustergen/relaxed.go) — and the outer decoder resumes after the
// fields container either way.
func TestHandDecodedRequestsAreHeldByTheirDefinition(t *testing.T) {
	t.Parallel()
	status := func(err error) im.StatusCode {
		var sce im.StatusCodeError
		if !errors.As(err, &sce) {
			return im.StatusSuccess
		}
		return sce.MatterStatusCode()
	}
	cases := []struct {
		name          string
		cluster, cmd  uint32
		body          func(*tlv.Encoder)
		want          im.StatusCode
		wantDecodedAs string
	}{
		{
			// Label "max 32" (operational-credentials.element.ts:124).
			name: "UpdateFabricLabel of 33 characters", cluster: 0x003E, cmd: 0x09,
			body: func(e *tlv.Encoder) { e.PutUTF8(tlv.ContextTag(0), strings.Repeat("a", 33)) },
			want: im.StatusConstraintError,
		},
		{
			// NocValue "max 400" (:96).
			name: "AddNOC with a NOC of 401 bytes", cluster: 0x003E, cmd: 0x06,
			body: func(e *tlv.Encoder) {
				e.PutOctets(tlv.ContextTag(0), make([]byte, 401))
				e.PutOctets(tlv.ContextTag(2), make([]byte, 16))
				e.PutUint(tlv.ContextTag(3), 1)
				e.PutUint(tlv.ContextTag(4), 0xFFF1)
			},
			want: im.StatusConstraintError,
		},
		{
			// A mandatory field missing: ValidationMandatoryFieldMissingError,
			// InvalidAction answered INVALID_COMMAND (:529-535).
			name: "AddNOC without its IPK", cluster: 0x003E, cmd: 0x06,
			body: func(e *tlv.Encoder) {
				e.PutOctets(tlv.ContextTag(0), []byte{1})
				e.PutUint(tlv.ContextTag(3), 1)
				e.PutUint(tlv.ContextTag(4), 0xFFF1)
			},
			want: im.StatusInvalidCommand,
		},
		{
			// A list over its count on a hand-written server: Endpoints
			// "1 to 20" (the Groupcast definition).
			name: "Groupcast LeaveGroup with 21 endpoints", cluster: 0x0065, cmd: 0x01,
			body: func(e *tlv.Encoder) {
				e.PutUint(tlv.ContextTag(0), 1)
				e.StartArray(tlv.ContextTag(1))
				for n := range 21 {
					e.PutUint(tlv.AnonymousTag(), uint64(n+1))
				}
				_ = e.EndContainer()
			},
			want: im.StatusConstraintError,
		},
		{
			// CsrNonce relaxed (OperationalCredentialsServer.ts:53-66): the
			// server answers the length.
			name: "CSRRequest with a nonce of 31 bytes", cluster: 0x003E, cmd: 0x04,
			body:          func(e *tlv.Encoder) { e.PutOctets(tlv.ContextTag(0), make([]byte, 31)) },
			wantDecodedAs: "core.CSRRequest",
		},
		{
			// GroupName relaxed (GroupsServer.ts:20-50).
			name: "AddGroup with a name of 17 characters", cluster: 0x0004, cmd: 0x00,
			body: func(e *tlv.Encoder) {
				e.PutUint(tlv.ContextTag(0), 1)
				e.PutUTF8(tlv.ContextTag(1), strings.Repeat("g", 17))
			},
			wantDecodedAs: "core.AddGroupRequest",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fields, next, err := readHeld(t, tc.cluster, tc.cmd, tc.body)
			if got := status(err); got != tc.want {
				t.Fatalf("status %v (err %v), want %v", got, err, tc.want)
			}
			if got := fmt.Sprintf("%T", fields); tc.wantDecodedAs != "" && got != tc.wantDecodedAs {
				t.Errorf("decoded as %s, want %s", got, tc.wantDecodedAs)
			}
			if next.Tag.Number != 9 || next.Uint != 77 {
				t.Errorf("the outer decoder resumes at %+v, want the element after the fields", next)
			}
		})
	}
}
