// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

type handReq struct {
	Level uint8
	Mode  uint8
}

// handCluster is a hand-written definition (no clustergen): command 0
// states a refusal for a request it does not fit, command 1 states none.
func handCluster() *spec.Cluster {
	fields := []spec.Field{
		{ID: 0, Name: "Level", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory), Constraint: spec.Constraint{Text: "1 to 100", Min: &spec.Bound{Int: 1}, Max: &spec.Bound{Int: 100}}},
		{ID: 1, Name: "Mode", Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: modeEnum}, Conformance: op(spec.ConfMandatory)},
	}
	return &spec.Cluster{
		ID: 0xFFF1_FC0B, Name: "Hand", Revision: 1,
		Commands: []spec.Command{
			{ID: 0, Name: "Checked", Direction: spec.Request, Decode: decodesAs[handReq](), Response: "status", Conformance: op(spec.ConfMandatory), Access: spec.Access{Write: spec.PrivilegeOperate}, Fields: fields, Check: spec.RequestCheck{Refusal: im.StatusConstraintError}},
			{ID: 1, Name: "Logged", Direction: spec.Request, Decode: decodesAs[handReq](), Response: "status", Conformance: op(spec.ConfMandatory), Access: spec.Access{Write: spec.PrivilegeOperate}, Fields: fields},
		},
	}
}

// TestCheckRequestHoldsAHandWrittenDefinition: Instance.CheckRequest
// holds a request to the constraint and the enum values its definition
// states; the request gate ([spec.AdmitRequest], before the server)
// refuses a request it refuses before any handler for a command that
// states a refusal, and logs it and runs the handler
// for one that states none (decision 113, R2-P5-apispec-08, -09 as
// narrowed).
func TestCheckRequestHoldsAHandWrittenDefinition(t *testing.T) {
	def := handCluster()
	calls := 0
	h := func(context.Context, any) (any, error) { calls++; return nil, nil }
	srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{Handlers: map[uint32]spec.CommandHandler{0: h, 1: h}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		req  handReq
		want error
	}{
		{"fits", handReq{Level: 50}, nil},
		{"out of range", handReq{Level: 101}, nil},
		{"undefined enum", handReq{Level: 50, Mode: 7}, spec.ErrUnknownEnumValue},
	} {
		err := srv.CheckRequest(0, c.req, nil)
		switch {
		case c.name == "fits" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.name != "fits" && (err == nil || statusOf(err) != im.StatusConstraintError || c.want != nil && !errors.Is(err, c.want)):
			t.Errorf("%s: %v, want CONSTRAINT_ERROR", c.name, err)
		}
	}

	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(prev)

	if _, err := wireInvoke(context.Background(), srv, 0, handReq{Level: 101}); statusOf(err) != im.StatusConstraintError || calls != 0 {
		t.Errorf("a stated refusal: %v, %d handler calls; want CONSTRAINT_ERROR before the handler", err, calls)
	}
	if _, err := wireInvoke(context.Background(), srv, 1, handReq{Level: 101}); err != nil || calls != 1 {
		t.Errorf("no stated refusal: %v, %d handler calls; want the handler to run", err, calls)
	}
	if !strings.Contains(logged.String(), "Logged") {
		t.Errorf("the refused request of a command without a refusal is not logged: %q", logged.String())
	}
	if _, err := wireInvoke(context.Background(), srv, 0, handReq{Level: 1}); err != nil || calls != 2 {
		t.Errorf("a request that fits: %v, %d handler calls", err, calls)
	}
}
