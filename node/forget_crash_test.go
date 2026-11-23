// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/endpoint"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// forgetPendingSetting is the setting a Forget records itself under while
// it runs (node/reserve.go forgetPendingKey).
const forgetPendingSetting = "aggregator.forget_pending"

var errCrashed = errors.New("the process crashed")

// crashPoint stops every write of the two stores once a Forget recorded
// itself as in progress — that record lands, nothing after it does — so
// the stores hold what a process that died right after the record left.
type crashPoint struct {
	armed, crashed atomic.Bool
}

func (c *crashPoint) write(key, value string) (land bool, err error) {
	if c.crashed.Load() {
		return false, errCrashed
	}
	if key == forgetPendingSetting && value != "" && c.armed.Load() {
		c.crashed.Store(true)
	}
	return true, nil
}

type crashingCreds struct {
	store.Port
	c *crashPoint
}

func (s crashingCreds) SetSetting(ctx context.Context, key, value string) error {
	if land, err := s.c.write(key, value); !land {
		return err
	}
	return s.Port.SetSetting(ctx, key, value)
}

func (s crashingCreds) SetSettings(ctx context.Context, values map[string]string) error {
	if s.c.crashed.Load() {
		return errCrashed
	}
	return s.Port.SetSettings(ctx, values)
}

func (s crashingCreds) DeleteSettings(ctx context.Context, keys ...string) error {
	if s.c.crashed.Load() {
		return errCrashed
	}
	return s.Port.DeleteSettings(ctx, keys...)
}

type crashingEndpoints struct {
	endpoint.Store
	c *crashPoint
}

func (s crashingEndpoints) UpsertEndpointAssigning(ctx context.Context, rec endpoint.Record) (uint16, error) {
	if s.c.crashed.Load() {
		return 0, errCrashed
	}
	return s.Store.UpsertEndpointAssigning(ctx, rec)
}

func (s crashingEndpoints) RemoveEndpoint(ctx context.Context, key endpoint.SourceKey) error {
	if s.c.crashed.Load() {
		return errCrashed
	}
	return s.Store.RemoveEndpoint(ctx, key)
}

// TestForgetCrashKeepsTheHeldEndpointsNumber (review II, R3-store-3702,
// decision 568): a Forget whose erasure reaches an endpoint the batch
// held back — here x, which a Replace in the batch gave the forgotten
// device's part — records the keys it keeps with the forget in progress.
// The process dies right after that record; the next New finishes the
// forget and spares the kept record, so the held endpoint keeps its
// number across the restart.
func TestForgetCrashKeepsTheHeldEndpointsNumber(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	ctx := t.Context()
	creds, eps := memstore.New(), epmem.New()
	crash := &crashPoint{}
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: crashingCreds{Port: creds, c: crash}, Endpoints: crashingEndpoints{Store: eps, c: crash}}
	n := newNode(t, cfg)
	if err := n.Aggregator().Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(ctx, bridgedWithParts(t, "x")); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	before, ok := servedNumberOf(n, partKey)
	if !ok {
		t.Fatalf("%s is not served before the Forget", partKey)
	}
	crash.armed.Store(true)
	// The batch fails on the dead stores; what matters is what they hold.
	_ = n.Aggregator().Batch(ctx, func(b *node.Batch) error {
		if err := b.Forget(endpoint.StringKey("owner")); err != nil {
			return err
		}
		return b.Replace(endpoint.StringKey("x"), bridgedWithParts(t, "x", partKey))
	})
	if !crash.crashed.Load() {
		t.Fatal("the batch recorded no forget in progress")
	}
	_ = n.Stop(ctx)

	cfg2 := minimalConfig(nil)
	cfg2.Store = node.Store{Credentials: creds, Endpoints: eps}
	again := newNode(t, cfg2)
	if v, ok, err := creds.GetSetting(ctx, forgetPendingSetting); err != nil || ok && v != "" {
		t.Errorf("forget record %q (found %v, %v) after the next New finished it, want none", v, ok, err)
	}
	if hasEndpointRecord(t, eps, "owner") {
		t.Error("the forgotten key owner kept its endpoint record across the finished forget")
	}
	if err := again.Aggregator().Add(ctx, bridgedWithParts(t, "x", partKey)); err != nil {
		t.Fatal(err)
	}
	if err := again.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if after, ok := servedNumberOf(again, partKey); !ok || after != before {
		t.Errorf("held endpoint %s at %d (served %v) after the crash and restart, want %d", partKey, after, ok, before)
	}
}

// TestForgetPendingRecordOfTheOldFormat (decision 568): a forget in
// progress recorded before the kept keys were — a bare list of keys —
// reads as before: the next New finishes it over every key, and the
// endpoint records of all of them go.
func TestForgetPendingRecordOfTheOldFormat(t *testing.T) {
	t.Parallel()
	const partKey = "owner/light"
	ctx := t.Context()
	creds, eps := memstore.New(), epmem.New()
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Credentials: creds, Endpoints: eps}
	n := newNode(t, cfg)
	if err := n.Aggregator().Add(ctx, bridgedWithParts(t, "owner", partKey)); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if !hasEndpointRecord(t, eps, partKey) {
		t.Fatalf("no record of %s after the first run", partKey)
	}
	if err := creds.SetSetting(ctx, forgetPendingSetting, `[["owner","owner/light"]]`); err != nil {
		t.Fatal(err)
	}
	cfg2 := minimalConfig(nil)
	cfg2.Store = node.Store{Credentials: creds, Endpoints: eps}
	newNode(t, cfg2)
	for _, k := range []string{"owner", partKey} {
		if hasEndpointRecord(t, eps, k) {
			t.Errorf("endpoint record of %s after the next New finished an old-format forget, want none", k)
		}
	}
	if v, ok, err := creds.GetSetting(ctx, forgetPendingSetting); err != nil || ok && v != "" {
		t.Errorf("forget record %q (found %v, %v) after it was finished, want none", v, ok, err)
	}
}
