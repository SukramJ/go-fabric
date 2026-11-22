// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/im"
)

// hooksAdmitDefinition states one request whose mandatory field the
// gate holds, with a Refusal so a misfit is refused rather than logged.
func hooksAdmitDefinition() *spec.Cluster {
	return &spec.Cluster{
		ID: 0xFFF1FC0A, Name: "HooksAdmit", Revision: 1,
		Commands: []spec.Command{{
			ID: 1, Name: "Req", Direction: spec.Request, Decode: decodesAs[ctReq](), Response: "status", Conformance: op(spec.ConfMandatory),
			Fields: []spec.Field{{ID: 0, Name: "Enable", Type: spec.Type{Kind: spec.KindBool}, Conformance: op(spec.ConfMandatory)}},
			Check:  spec.RequestCheck{Refusal: im.StatusInvalidCommand},
		}},
	}
}

// TestHooksInvokeAdmitsTheRequestFirst: a spec.Hooks server's command is
// held by the one request gate in front of every server
// ([spec.AdmitRequest], decision 339): a request its definition refuses
// answers the command's Refusal there, so neither the host's handler nor
// the server's own logic runs; Hooks.Invoke itself is a local call and
// does not validate (matter.js validates only the commands its interaction
// layer invokes, ProtocolService.ts:347-350 at b25ed960). Before decision
// 339 the gate sat in Hooks.Invoke (review II, DR5b-c2-06, R2-P6-api-1307).
func TestHooksInvokeAdmitsTheRequestFirst(t *testing.T) {
	t.Parallel()
	def := hooksAdmitDefinition()
	inst, err := spec.New(def, spec.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var h spec.Hooks
	h.Init(inst)
	yes := true
	ctx := context.Background()

	// The gate: a misfit is refused with the Refusal, a fit request passes.
	var se *spec.StatusError
	if err := spec.AdmitRequest(ctx, def, inst.FeatureMap(), 1, ctReq{}, nil); !errors.As(err, &se) || se.Status != im.StatusInvalidCommand {
		t.Errorf("misfit at the gate: %v, want INVALID_COMMAND", err)
	}
	if err := spec.AdmitRequest(ctx, def, inst.FeatureMap(), 1, ctReq{Enable: &yes}, nil); err != nil {
		t.Errorf("fit at the gate: %v", err)
	}

	// Hooks.Invoke on its own: no host handler leaves the server's own
	// logic to run; a host handler runs, misfit or not (a local call).
	if _, handled, err := h.Invoke(ctx, 1, ctReq{}); handled || err != nil {
		t.Errorf("misfit, no handler: handled %v, %v; want the server's own logic", handled, err)
	}
	ran := 0
	if err := h.Handle(1, func(context.Context, any) (any, error) { ran++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, handled, err := h.Invoke(ctx, 1, ctReq{}); !handled || err != nil || ran != 1 {
		t.Errorf("misfit, host handler, local call: handled %v, %v, ran %d; want the handler", handled, err, ran)
	}
}
