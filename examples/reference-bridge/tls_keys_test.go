// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/surface"
)

// TestSimKeysKeepAKeySetTheyCannotRead holds R2-C2-store-2605, the class of
// R2-P6-store-2652 beyond the camera: a TLS key set --tls-sim cannot read,
// decode or take whole is logged and left alone — a new CSR's key is not
// persisted over it — and --jointfabric-sim refuses the CSR rather than
// replace an administrator key it cannot read.
func TestSimKeysKeepAKeySetTheyCannotRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, st := range map[string]*flakySettings{
		"a read error": {readErr: errors.New("disk I/O error")},
		"corrupt JSON": {value: `{"1/1":`},
		"a bad key":    {value: `{"1/1":"bm90IGEga2V5"}`},
		"bad base64":   {value: `{"1/1":"!"}`},
	} {
		var logs bytes.Buffer
		k := newTLSKeys(slog.New(slog.NewTextHandler(&logs, nil)))
		k.attach(ctx, st)
		if _, _, err := k.GenerateCSR(ctx, 1, 2, false, []byte("nonce")); err != nil {
			t.Fatalf("%s: GenerateCSR: %v", name, err)
		}
		k.RemoveClientKey(ctx, 1, 2)
		if st.writes != 0 {
			t.Errorf("%s: %d writes over the kept key set", name, st.writes)
		}
		if !strings.Contains(logs.String(), "tlssim.load") {
			t.Errorf("%s: not logged:\n%s", name, logs.String())
		}
	}
	ok := &flakySettings{}
	k := newTLSKeys(slog.New(slog.DiscardHandler))
	k.attach(ctx, ok)
	if _, _, err := k.GenerateCSR(ctx, 1, 2, false, []byte("nonce")); err != nil || ok.writes != 1 {
		t.Fatalf("a readable key set: %d writes (want 1), %v", ok.writes, err)
	}

	for name, st := range map[string]*flakySettings{
		"a read error": {readErr: errors.New("disk I/O error")},
		"bad base64":   {value: "!"},
		"a bad key":    {value: "bm90IGEga2V5"},
	} {
		j := newDemoJointFabric(surface.Policies{}, slog.New(slog.DiscardHandler))
		j.attach(st)
		if _, err := j.IcacCsr(ctx); err == nil || !strings.Contains(err.Error(), jfKeySetting) {
			t.Errorf("%s: IcacCsr = %v, want a refusal naming %s", name, err, jfKeySetting)
		}
		if st.writes != 0 {
			t.Errorf("%s: %d writes over the kept administrator key", name, st.writes)
		}
	}
	none := &flakySettings{}
	j := newDemoJointFabric(surface.Policies{}, slog.New(slog.DiscardHandler))
	j.attach(none)
	if _, err := j.IcacCsr(ctx); err != nil || none.writes != 1 {
		t.Fatalf("no kept key: %d writes (want 1), %v", none.writes, err)
	}
}
