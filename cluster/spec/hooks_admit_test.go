// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// hooksAdmitDefinition states one request whose mandatory field the
// gate holds, with a Refusal so a misfit is refused rather than logged.
func hooksAdmitDefinition() *spec.Cluster {
	return &spec.Cluster{
		ID: 0xFFF1FC0A, Name: "HooksAdmit", Revision: 1,
		Commands: []spec.Command{{
			ID: 1, Name: "Req", Direction: spec.Request, Response: "status", Conformance: op(spec.ConfMandatory),
			Fields: []spec.Field{{ID: 0, Name: "Enable", Type: spec.Type{Kind: spec.KindBool}, Conformance: op(spec.ConfMandatory)}},
			Check:  spec.RequestCheck{Refusal: im.StatusInvalidCommand},
		}},
	}
}

// TestHooksInvokeAdmitsTheRequestFirst holds a spec.Hooks server's command
// path to the request gate every other path runs: a request its
// definition refuses answers the command's Refusal before the host's
// handler runs, and — with no host handler — before the server's own
// logic (Invoke answers handled). Before, Hooks.Invoke ran the host's
// handler with no admitRequest, so a stated Refusal on any of the eight
// Hooks servers' clusters would have been ignored (review II, DR5b-c2-06,
// R2-P6-api-1307).
func TestHooksInvokeAdmitsTheRequestFirst(t *testing.T) {
	t.Parallel()
	inst, err := spec.New(hooksAdmitDefinition(), spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var h spec.Hooks
	h.Init(inst)
	yes := true

	// No host handler: a misfit is refused and handled; a fit request
	// leaves the server's own logic to run.
	resp, handled, err := h.Invoke(context.Background(), 1, ctReq{})
	var se *spec.StatusError
	if !handled || !errors.As(err, &se) || se.Status != im.StatusInvalidCommand || resp != nil {
		t.Errorf("misfit, no handler: (%v, %v, %v), want handled with INVALID_COMMAND", resp, handled, err)
	}
	if _, handled, err := h.Invoke(context.Background(), 1, ctReq{Enable: &yes}); handled || err != nil {
		t.Errorf("fit, no handler: handled %v, %v; want the server's own logic", handled, err)
	}

	// A host handler: it never sees the misfit.
	ran := 0
	if err := h.Handle(1, func(context.Context, any) (any, error) { ran++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, handled, err := h.Invoke(context.Background(), 1, ctReq{}); !handled || !errors.As(err, &se) || se.Status != im.StatusInvalidCommand {
		t.Errorf("misfit, host handler: handled %v, %v; want INVALID_COMMAND", handled, err)
	}
	if _, handled, err := h.Invoke(context.Background(), 1, ctReq{Enable: &yes}); !handled || err != nil {
		t.Errorf("fit, host handler: handled %v, %v", handled, err)
	}
	if ran != 1 {
		t.Errorf("the host handler ran %d times, want once (the fit request only)", ran)
	}
}
