// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// invoke sends the request payload req through the wire: encoded with the
// generated codec, decoded with the definition's decoder — the path the
// bridge takes — and handed to the server.
func invoke(t *testing.T, srv contract.ClusterServer, def *spec.Cluster, cmdID uint32, req spec.Encodable) (any, error) {
	t.Helper()
	cmd := def.Command(cmdID, spec.Request)
	if cmd == nil {
		t.Fatalf("%s: no request 0x%02X", def.Name, cmdID)
	}
	fields, err := cmd.Decode(spectest.Decode(t, spectest.Encode(t, req)))
	if err != nil {
		t.Fatalf("%s.%s: decode: %v", def.Name, cmd.Name, err)
	}
	return srv.MatterInvoke(context.Background(), cmdID, fields)
}

// wantStatus wants err to carry the IM status code.
func wantStatus(t *testing.T, err error, code im.StatusCode) {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != code {
		t.Fatalf("error %v, want status %s", err, code)
	}
}

// wantResponse wants resp to be want and to encode as the generated
// response command it names.
func wantResponse(t *testing.T, def *spec.Cluster, respID uint32, resp, want spec.Encodable) {
	t.Helper()
	if !reflect.DeepEqual(resp, want) {
		t.Fatalf("response %+v, want %+v", resp, want)
	}
	spectest.RoundTripCommand(t, def, respID, spec.Response, resp)
}

// read reads attrID or fails.
func read(t *testing.T, srv contract.ClusterServer, attrID uint32) any {
	t.Helper()
	v, ok := srv.MatterRead(attrID)
	if !ok {
		t.Fatalf("attribute 0x%04X absent", attrID)
	}
	return v
}

// changes records the attribute-change reports of srv.
func changes(t *testing.T, srv contract.ClusterServer) *[]uint32 {
	t.Helper()
	n, ok := srv.(contract.AttributeChangeNotifier)
	if !ok {
		t.Fatalf("%T reports no attribute changes", srv)
	}
	var got []uint32
	stop := n.OnAttributeChange(func(change contract.Change) { ids := change.Attributes; got = append(got, ids...) })
	t.Cleanup(stop)
	return &got
}

// errPort is a port failure without a status.
var errPort = errors.New("port failed")

// emitted records the events a server emits.
type emitted struct {
	events []emittedEvent
}

type emittedEvent struct {
	endpoint uint16
	cluster  uint32
	event    uint32
	data     any
	priority contract.EventPriority
}

func (e *emitted) MatterEmitEvent(endpoint uint16, cluster, event uint32, data any, priority contract.EventPriority) {
	e.events = append(e.events, emittedEvent{endpoint, cluster, event, data, priority})
}
