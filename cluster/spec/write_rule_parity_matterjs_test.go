// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/identify"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// identifyWithRule is an Identify server holding IdentifyTime 5, its own
// rule refusing 7 and counting its calls.
func identifyWithRule(t *testing.T, ruleCalls *int, snk *sink) *spec.Server {
	t.Helper()
	srv, err := spec.NewServer(identify.Definition, spec.Options{}, spec.ServerConfig{
		Initial:  map[uint32]any{identify.AttrIdentifyTime: uint16(5)},
		Handlers: map[uint32]spec.CommandHandler{identify.CmdIdentify: func(context.Context, any) (any, error) { return nil, nil }},
		Sink:     snk,
		WriteRule: func(_ context.Context, _ uint32, v any) error {
			*ruleCalls++
			if v == uint16(7) {
				return spec.Errorf(im.StatusConstraintError, "the author's rule refuses 7")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestWriteRuleAndVetoOrderParityMatterJS: the author's rule judges every
// write, the held value included (matter.js validates the assigned value
// with no comparison, StructManager.ts:273, :279-298 at db021da0); the
// host's veto is behind the held-value comparison — a write of the held
// value calls no veto, reaches no Sink, advances no data version and
// notifies nothing (Datasource.ts:1209-1212, :1406-1408, :1456-1457,
// :1476-1481) — and a host veto, set or removed, never removes the rule
// (R2-P5-apispec-01, -02).
func TestWriteRuleAndVetoOrderParityMatterJS(t *testing.T) {
	ctx := context.Background()
	var ruleCalls, vetoCalls int
	snk := &sink{}
	srv := identifyWithRule(t, &ruleCalls, snk)
	refuse := errors.New("host refuses")
	srv.SetBeforeWrite(func(context.Context, uint32, any) error { vetoCalls++; return refuse })
	notified := 0
	defer srv.OnAttributeChange(func(contract.Change) { notified++ })()
	before := srv.MatterDataVersion()

	changed, err := srv.MatterWriteChanged(ctx, identify.AttrIdentifyTime, uint64(5))
	if err != nil || changed {
		t.Fatalf("held value: changed %v err %v, want unchanged and accepted", changed, err)
	}
	if ruleCalls != 1 || vetoCalls != 0 || len(snk.writes) != 0 {
		t.Fatalf("held value: rule %d veto %d sink %d, want 1 0 0", ruleCalls, vetoCalls, len(snk.writes))
	}
	if v := srv.MatterDataVersion(); v != before || notified != 0 {
		t.Fatalf("held value: version %d → %d, %d notifications", before, v, notified)
	}

	if _, err := srv.MatterWriteChanged(ctx, identify.AttrIdentifyTime, uint64(6)); !errors.Is(err, refuse) {
		t.Fatalf("a change: %v, want the host veto's refusal", err)
	}
	if vetoCalls != 1 {
		t.Fatalf("a change: veto %d, want 1", vetoCalls)
	}

	for _, host := range []func(context.Context, uint32, any) error{
		func(context.Context, uint32, any) error { return nil },
		nil,
	} {
		srv.SetBeforeWrite(host)
		if st := statusOf(srv.MatterWrite(ctx, identify.AttrIdentifyTime, uint64(7))); st != im.StatusConstraintError {
			t.Fatalf("rule after SetBeforeWrite(%v): status %v, want ConstraintError", host != nil, st)
		}
	}
}
